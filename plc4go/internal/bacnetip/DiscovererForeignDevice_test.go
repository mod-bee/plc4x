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

package bacnetip

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	"github.com/apache/plc4x/plc4go/spi/options"
)

// fakeBBMD answers a foreign-device registration (accepting it, or with a NAK) and a Distribute-Broadcast-To-Network
// with the IAm of device 3001 at 10.1.2.3:47808, forwarded as a Forwarded-NPDU — what a BBMD relays from its subnet.
func fakeBBMD(t *testing.T, accept bool) (string, *sync.Map) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	seen := &sync.Map{}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n < 4 || buf[0] != 0x81 {
				continue
			}
			seen.Store(buf[1], true)
			switch buf[1] {
			case 0x05: // Register-Foreign-Device
				code := []byte{0x00, 0x00}
				if !accept {
					code = []byte{0x00, 0x30} // Register-Foreign-Device NAK
				}
				_, _ = conn.WriteToUDP(append([]byte{0x81, 0x00, 0x00, 0x06}, code...), from)
			case 0x09: // Distribute-Broadcast-To-Network
				npdu := []byte{0x01, 0x00, 0x10, 0x00, 0xc4, 0x02, 0x00, 0x0b, 0xb9, 0x22, 0x05, 0xc4, 0x91, 0x00, 0x22, 0x03, 0xe7}
				frame := append([]byte{0x81, 0x04, 0x00, byte(4 + 6 + len(npdu)), 10, 1, 2, 3, 0xba, 0xc0}, npdu...)
				_, _ = conn.WriteToUDP(frame, from)
			}
		}
	}()
	return conn.LocalAddr().String(), seen
}

func discoverVia(t *testing.T, bbmd string) ([]apiModel.PlcDiscoveryItem, error) {
	d := NewDiscoverer()
	d.SetDiscoveryTimeout(700 * time.Millisecond)
	var mu sync.Mutex
	var found []apiModel.PlcDiscoveryItem
	err := d.Discover(context.Background(), func(item apiModel.PlcDiscoveryItem) {
		mu.Lock()
		defer mu.Unlock()
		found = append(found, item)
	}, options.WithDiscoveryOptionProtocolSpecific("foreign-device-bbmd", bbmd), options.WithDiscoveryOptionProtocolSpecific("foreign-device-ttl", 30))
	mu.Lock()
	defer mu.Unlock()
	return found, err
}

func TestDiscoverThroughBBMD_RegistersThenFindsTheForwardedDevice(t *testing.T) {
	bbmd, seen := fakeBBMD(t, true)
	found, err := discoverVia(t, bbmd)
	require.NoError(t, err)
	_, registered := seen.Load(byte(0x05))
	_, distributed := seen.Load(byte(0x09))
	assert.True(t, registered && distributed, "registered %v, distributed %v", registered, distributed)
	require.Len(t, found, 1)
	u := found[0].GetTransportUrl()
	assert.Equal(t, "10.1.2.3:47808", u.Host, "the device's own address, not the BBMD's")
	assert.Contains(t, found[0].GetName(), "3001")
	assert.Equal(t, []string{bbmd}, found[0].GetOptions()["via-bbmd"])
}

func TestDiscoverThroughBBMD_SaysWhenTheBBMDRefuses(t *testing.T) {
	bbmd, _ := fakeBBMD(t, false)
	_, err := discoverVia(t, bbmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refused the foreign-device registration")
}

func TestDiscoverThroughBBMD_SaysWhenNothingAnswers(t *testing.T) {
	quiet, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer func() { _ = quiet.Close() }()
	_, err = discoverVia(t, quiet.LocalAddr().String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not answer the foreign-device registration")
}
