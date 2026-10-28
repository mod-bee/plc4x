/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package canopen

import (
	"context"
	"encoding/binary"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	spiTransports "github.com/apache/plc4x/plc4go/spi/transports"
	"github.com/apache/plc4x/plc4go/spi/transports/can"
	"github.com/apache/plc4x/plc4go/spi/transports/virtualcan"
)

// testNode is a CANopen node on a virtual bus, written from CiA 301 alone - its frames are bytes laid out as the
// specification lays them out, not the driver's (nor the mspec's) - so that a driver test checks the driver against
// the protocol rather than against itself.
//
// It is an SDO server (7.2.4.3: expedited and segmented upload and download, abort) over an object dictionary, and it
// keeps every other frame it receives for the test to look at.
type testNode struct {
	t        *testing.T
	endpoint spiTransports.TransportInstance

	// requestId is the node-ID whose SDO requests (0x600 + requestId) the node answers, answerId the node-ID it answers
	// as (0x580 + answerId); they differ for the driver's "SDO:1/2:..." address.
	requestId, answerId uint8

	mutex      sync.Mutex
	dictionary map[objectKey][]byte
	readOnly   map[objectKey]bool
	silent     map[objectKey]bool   // objects the node never answers about
	abortWith  map[objectKey]uint32 // objects the node aborts with this code
	badToggle  bool                 // answer upload segments with the wrong toggle bit
	upload     *transfer
	download   *transfer
	aborts     []uint32 // the abort codes the client sent
	others     chan frame
}

type objectKey struct {
	index    uint16
	subIndex uint8
}

type transfer struct {
	key    objectKey
	data   []byte
	size   int
	toggle byte
}

type frame struct {
	cobId uint16
	data  []byte
}

func newTestNode(t *testing.T, bus string, requestId, answerId uint8) *testNode {
	t.Helper()
	endpoint, err := virtualcan.NewTransport().CreateTransportInstance(url.URL{Scheme: virtualcan.TransportCode, Host: bus}, nil)
	require.NoError(t, err)
	require.NoError(t, endpoint.Connect(context.Background()))
	node := &testNode{
		t:          t,
		endpoint:   endpoint,
		requestId:  requestId,
		answerId:   answerId,
		dictionary: map[objectKey][]byte{},
		readOnly:   map[objectKey]bool{},
		silent:     map[objectKey]bool{},
		abortWith:  map[objectKey]uint32{},
		others:     make(chan frame, 64),
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { node.serve(ctx) })
	t.Cleanup(func() {
		cancel()
		wg.Wait()
		_ = endpoint.Close()
	})
	return node
}

func (n *testNode) set(index uint16, subIndex uint8, data ...byte) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	n.dictionary[objectKey{index, subIndex}] = data
}

func (n *testNode) get(index uint16, subIndex uint8) []byte {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	return n.dictionary[objectKey{index, subIndex}]
}

func (n *testNode) abortCodes() []uint32 {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	return append([]uint32(nil), n.aborts...)
}

// send puts an 11-bit frame on the bus as this node (or anyone: a PDO, a heartbeat, another master's NMT command).
func (n *testNode) send(cobId uint16, data ...byte) {
	frame, err := can.EncodeFrame(uint32(cobId), data)
	require.NoError(n.t, err)
	require.NoError(n.t, n.endpoint.Write(context.Background(), frame))
}

// next is the next frame the node received that was not an SDO request to it.
func (n *testNode) next(t *testing.T) frame {
	t.Helper()
	select {
	case received := <-n.others:
		return received
	case <-time.After(2 * time.Second):
		t.Fatal("the node received no frame")
		return frame{}
	}
}

func (n *testNode) serve(ctx context.Context) {
	for ctx.Err() == nil {
		available, err := n.endpoint.GetNumBytesAvailableInBuffer()
		if err != nil || available < can.FrameSize {
			time.Sleep(time.Millisecond)
			continue
		}
		raw, err := n.endpoint.Read(ctx, can.FrameSize)
		if err != nil {
			continue
		}
		received := frame{cobId: uint16(binary.LittleEndian.Uint32(raw) & can.SffMask), data: raw[8 : 8+raw[4]]}
		if received.cobId == 0x600+uint16(n.requestId) && len(received.data) == 8 {
			n.serveSdo(received.data)
			continue
		}
		n.others <- received
	}
}

// answer sends an SDO response: eight bytes, as every SDO frame is (7.2.4.3).
func (n *testNode) answer(data [8]byte) {
	n.send(0x580+uint16(n.answerId), data[:]...)
}

// abort sends an SDO abort transfer (7.2.4.3.17): cs 0x80, the multiplexer, the abort code.
func (n *testNode) abort(key objectKey, code uint32) {
	var response [8]byte
	response[0] = 0x80
	binary.LittleEndian.PutUint16(response[1:], key.index)
	response[3] = key.subIndex
	binary.LittleEndian.PutUint32(response[4:], code)
	n.answer(response)
}

func (n *testNode) serveSdo(request []byte) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	key := objectKey{binary.LittleEndian.Uint16(request[1:]), request[3]}
	ccs := request[0] >> 5
	switch ccs {
	case 2: // initiate upload (7.2.4.3.4)
		if n.silent[key] {
			return
		}
		if code, ok := n.abortWith[key]; ok {
			n.abort(key, code)
			return
		}
		data, ok := n.dictionary[key]
		if !ok {
			n.abort(key, 0x06020000) // object does not exist
			return
		}
		response := [8]byte{0, request[1], request[2], request[3]}
		if len(data) <= 4 { // expedited: scs 2, n = unused bytes, e = 1, s = 1
			response[0] = 0x40 | byte(4-len(data))<<2 | 0x02 | 0x01
			copy(response[4:], data)
		} else { // segmented, size indicated: scs 2, e = 0, s = 1, the size in bytes 4 to 7
			response[0] = 0x40 | 0x01
			binary.LittleEndian.PutUint32(response[4:], uint32(len(data)))
			n.upload = &transfer{key: key, data: data}
		}
		n.answer(response)
	case 3: // upload segment (7.2.4.3.5)
		if n.upload == nil {
			n.abort(key, 0x05040001) // command specifier not valid
			return
		}
		toggle := request[0] >> 4 & 1
		if toggle != n.upload.toggle {
			n.abort(n.upload.key, 0x05030000)
			n.upload = nil
			return
		}
		segment := n.upload.data[:min(7, len(n.upload.data))]
		n.upload.data = n.upload.data[len(segment):]
		last := len(n.upload.data) == 0
		answered := toggle
		if n.badToggle {
			answered ^= 1
		}
		var response [8]byte // scs 0, t, n = unused bytes, c = last
		response[0] = answered<<4 | byte(7-len(segment))<<1
		if last {
			response[0] |= 1
			n.upload = nil
		} else {
			n.upload.toggle ^= 1
		}
		copy(response[1:], segment)
		n.answer(response)
	case 1: // initiate download (7.2.4.3.2 and 7.2.4.3.3)
		if n.silent[key] {
			return
		}
		if n.readOnly[key] {
			n.abort(key, 0x06010002) // attempt to write a read only object
			return
		}
		expedited, indicated, unused := request[0]>>1&1 == 1, request[0]&1 == 1, int(request[0]>>2&3)
		if expedited {
			size := 4
			if indicated {
				size = 4 - unused
			}
			n.dictionary[key] = append([]byte(nil), request[4:4+size]...)
		} else {
			n.download = &transfer{key: key, size: int(binary.LittleEndian.Uint32(request[4:]))}
		}
		n.answer([8]byte{0x60, request[1], request[2], request[3]}) // scs 3
	case 0: // download segment (7.2.4.3.2)
		if n.download == nil {
			n.abort(key, 0x05040001)
			return
		}
		toggle, unused, last := request[0]>>4&1, int(request[0]>>1&7), request[0]&1 == 1
		if toggle != n.download.toggle {
			n.abort(n.download.key, 0x05030000)
			n.download = nil
			return
		}
		n.download.data = append(n.download.data, request[1:8-unused]...)
		n.download.toggle ^= 1
		n.answer([8]byte{0x20 | toggle<<4}) // scs 1
		if last {
			if len(n.download.data) == n.download.size {
				n.dictionary[n.download.key] = n.download.data
			}
			n.download = nil
		}
	case 4: // abort (7.2.4.3.17)
		n.aborts = append(n.aborts, binary.LittleEndian.Uint32(request[4:]))
		n.upload, n.download = nil, nil
	}
}
