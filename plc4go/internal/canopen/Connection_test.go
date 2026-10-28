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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plc4go "github.com/apache/plc4x/plc4go/pkg/api"
	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi"
	"github.com/apache/plc4x/plc4go/spi/transports/socketcan"
	"github.com/apache/plc4x/plc4go/spi/transports/virtualcan"
)

func connect(t *testing.T, bus string, connectionOptions string) plc4go.PlcConnection {
	t.Helper()
	manager := plc4go.NewPlcDriverManager()
	t.Cleanup(func() { _ = manager.Close() })
	manager.RegisterDriver(NewDriver())
	manager.(spi.TransportAware).RegisterTransport(virtualcan.NewTransport())
	manager.(spi.TransportAware).RegisterTransport(socketcan.NewTransport())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := manager.GetConnection(ctx, "canopen:can-virtualcan://"+bus+connectionOptions)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	return connection
}

func read(t *testing.T, connection plc4go.PlcConnection, address string) (apiModel.PlcResponseCode, apiValues.PlcValue) {
	t.Helper()
	request, err := connection.ReadRequestBuilder().AddTagAddress("tag", address).Build()
	require.NoError(t, err)
	result := <-request.Execute(context.Background())
	require.NoError(t, result.GetErr())
	return result.GetResponse().GetResponseCode("tag"), result.GetResponse().GetValue("tag")
}

func write(t *testing.T, connection plc4go.PlcConnection, address string, value any) apiModel.PlcResponseCode {
	t.Helper()
	request, err := connection.WriteRequestBuilder().AddTagAddress("tag", address, value).Build()
	require.NoError(t, err)
	result := <-request.Execute(context.Background())
	require.NoError(t, result.GetErr())
	return result.GetResponse().GetResponseCode("tag")
}

func subscribe(t *testing.T, connection plc4go.PlcConnection, addresses map[string]string) chan apiModel.PlcSubscriptionEvent {
	t.Helper()
	events := make(chan apiModel.PlcSubscriptionEvent, 16)
	builder := connection.SubscriptionRequestBuilder()
	for name, address := range addresses {
		builder.AddEventTagAddress(name, address).AddPreRegisteredConsumer(name, func(event apiModel.PlcSubscriptionEvent) { events <- event })
	}
	request, err := builder.Build()
	require.NoError(t, err)
	result := <-request.Execute(context.Background())
	require.NoError(t, result.GetErr())
	for name := range addresses {
		require.Equal(t, apiModel.PlcResponseCode_OK, result.GetResponse().GetResponseCode(name), name)
	}
	return events
}

func nextEvent(t *testing.T, events chan apiModel.PlcSubscriptionEvent) apiModel.PlcSubscriptionEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("no event came")
		return nil
	}
}

func noEvent(t *testing.T, events chan apiModel.PlcSubscriptionEvent) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("an event came that should not have: %v", event)
	case <-time.After(100 * time.Millisecond):
	}
}

// The cases of plc4j's CANOpenDriverSDOIT: the node holds "asdf" (0x61 0x73 0x64 0x66) at 1000/22 and 9186/1, and
// "asdfasdf" at 2000/44, which is longer than an expedited transfer carries.
func TestSdoReadsAsPlc4jsDriverTestDescribes(t *testing.T) {
	connection := connect(t, "sdo-read", "?node-id=15&request-timeout-ms=1000")
	node := newTestNode(t, "sdo-read", 1, 1)
	node.set(1000, 22, 0x61, 0x73, 0x64, 0x66)
	node.set(9186, 1, 0x61, 0x73, 0x64, 0x66)
	node.set(2000, 44, []byte("asdfasdf")...)

	code, value := read(t, connection, "SDO:1:1000/22:UNSIGNED32")
	require.Equal(t, apiModel.PlcResponseCode_OK, code)
	assert.Equal(t, uint32(0x66647361), value.GetUint32(), "expedited, little-endian")

	code, value = read(t, connection, "SDO:1:9186/1:UNSIGNED8")
	require.Equal(t, apiModel.PlcResponseCode_OK, code)
	assert.Equal(t, uint8(97), value.GetUint8(), "expedited, the first byte")

	code, value = read(t, connection, "SDO:1:2000/44:RECORD")
	require.Equal(t, apiModel.PlcResponseCode_OK, code)
	assert.Equal(t, []byte("asdfasdf"), value.GetRaw(), "segmented")
}

// CiA 301 7.2.4.3.6: an expedited response may leave the size unindicated (e=1, s=0); the four bytes are the data.
func TestSdoReadsAnExpeditedAnswerWithoutTheSize(t *testing.T) {
	connection := connect(t, "sdo-unsized", "")
	node := newTestNode(t, "sdo-unsized", 4, 4)
	node.set(0x1017, 0, 0xE8, 0x03)
	node.unsized[objectKey{0x1017, 0}] = true

	code, value := read(t, connection, "SDO:4:0x1017/0:UNSIGNED16")
	require.Equal(t, apiModel.PlcResponseCode_OK, code)
	assert.Equal(t, uint16(1000), value.GetUint16())
}

func TestSdoReadsTheAnswerOfTheNodeTheAddressNames(t *testing.T) {
	connection := connect(t, "sdo-answer", "")
	node := newTestNode(t, "sdo-answer", 1, 2) // asked as node 1, answers as node 2
	node.set(1000, 22, 0x61, 0x73, 0x64, 0x66)

	code, value := read(t, connection, "SDO:1/2:1000/22:UNSIGNED32")
	require.Equal(t, apiModel.PlcResponseCode_OK, code)
	assert.Equal(t, uint32(0x66647361), value.GetUint32())

	code, _ = read(t, connection, "SDO:1:1000/22:UNSIGNED32")
	assert.Equal(t, apiModel.PlcResponseCode_REQUEST_TIMEOUT, code, "node 1 never answers as itself")
}

func TestSdoReadsEveryTypeAsCiA301LaysItOut(t *testing.T) {
	connection := connect(t, "sdo-types", "")
	node := newTestNode(t, "sdo-types", 5, 5)
	node.set(0x6000, 1, 0xFE, 0xFF)
	node.set(0x6000, 2, 0x56, 0x34, 0x12)
	node.set(0x6000, 3, 0x00, 0x00, 0xC0, 0x3F)
	node.set(0x6000, 4, 0x01)
	node.set(0x6000, 5, 0x01, 0x00, 0xFF, 0xFF, 0x00, 0x80)
	node.set(0x1008, 0, []byte("ModBee CANopen test node")...)
	node.set(0x1009, 0, 0x41, 0x00, 0xE9, 0x00, 0x3A, 0x26)
	node.set(0x6001, 0, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11)

	tests := []struct {
		address string
		check   func(apiValues.PlcValue)
	}{
		{"SDO:5:0x6000/1:INTEGER16", func(v apiValues.PlcValue) { assert.Equal(t, int16(-2), v.GetInt16()) }},
		{"SDO:5:0x6000/2:UNSIGNED24", func(v apiValues.PlcValue) { assert.Equal(t, uint32(0x123456), v.GetUint32()) }},
		{"SDO:5:0x6000/3:REAL32", func(v apiValues.PlcValue) { assert.Equal(t, float32(1.5), v.GetFloat32()) }},
		{"SDO:5:0x6000/4:BOOLEAN", func(v apiValues.PlcValue) { assert.True(t, v.GetBool()) }},
		{"SDO:5:0x6000/5:INTEGER16[3]", func(v apiValues.PlcValue) {
			require.True(t, v.IsList())
			assert.Equal(t, []int16{1, -1, -32768}, []int16{v.GetIndex(0).GetInt16(), v.GetIndex(1).GetInt16(), v.GetIndex(2).GetInt16()})
		}},
		{"SDO:5:0x1008/0:VISIBLE_STRING", func(v apiValues.PlcValue) { assert.Equal(t, "ModBee CANopen test node", v.GetString()) }},
		{"SDO:5:0x1009/0:UNICODE_STRING", func(v apiValues.PlcValue) { assert.Equal(t, "Aé☺", v.GetString()) }},
		{"SDO:5:0x6001/0:UNSIGNED64", func(v apiValues.PlcValue) { assert.Equal(t, uint64(0x1122334455667788), v.GetUint64()) }},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			code, value := read(t, connection, test.address)
			require.Equal(t, apiModel.PlcResponseCode_OK, code)
			test.check(value)
		})
	}
}

// The last case of plc4j's CANOpenDriverSDOIT aborts with 0x05030000 and expects REMOTE_ERROR; an abort whose code
// says what went wrong answers the response code that names it.
func TestSdoAbortsAnswerWhatTheAbortCodeSays(t *testing.T) {
	connection := connect(t, "sdo-abort", "")
	node := newTestNode(t, "sdo-abort", 1, 1)
	node.abortWith[objectKey{1000, 22}] = 0x05030000
	node.set(0x1000, 0, 0x91, 0x01, 0x0F, 0x00)
	node.readOnly[objectKey{0x1000, 0}] = true

	code, _ := read(t, connection, "SDO:1:1000/22:UNSIGNED32")
	assert.Equal(t, apiModel.PlcResponseCode_REMOTE_ERROR, code)
	code, _ = read(t, connection, "SDO:1:0x2000/0:UNSIGNED32")
	assert.Equal(t, apiModel.PlcResponseCode_NOT_FOUND, code, "0x06020000, object does not exist")
	assert.Equal(t, apiModel.PlcResponseCode_ACCESS_DENIED, write(t, connection, "SDO:1:0x1000/0:UNSIGNED32", uint32(1)),
		"0x06010002, attempt to write a read only object")
}

// A node that does not answer is told so (abort 0x05040000, SDO protocol timed out), and the read answers
// REQUEST_TIMEOUT; a node that answers a segment with the wrong toggle bit is aborted with 0x05030000.
func TestSdoTransfersAbortWhatGoesWrong(t *testing.T) {
	connection := connect(t, "sdo-wrong", "?request-timeout-ms=100")
	node := newTestNode(t, "sdo-wrong", 1, 1)
	node.silent[objectKey{0x2000, 1}] = true
	node.set(0x2000, 2, []byte("more than four")...)

	code, _ := read(t, connection, "SDO:1:0x2000/1:UNSIGNED32")
	assert.Equal(t, apiModel.PlcResponseCode_REQUEST_TIMEOUT, code)
	require.Eventually(t, func() bool { return len(node.abortCodes()) == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, uint32(0x05040000), node.abortCodes()[0])

	node.mutex.Lock()
	node.badToggle = true
	node.mutex.Unlock()
	code, _ = read(t, connection, "SDO:1:0x2000/2:VISIBLE_STRING")
	assert.Equal(t, apiModel.PlcResponseCode_REMOTE_ERROR, code)
	require.Eventually(t, func() bool { return len(node.abortCodes()) == 2 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, uint32(0x05030000), node.abortCodes()[1])
}

func TestSdoWritesExpeditedAndSegmented(t *testing.T) {
	connection := connect(t, "sdo-write", "")
	node := newTestNode(t, "sdo-write", 3, 3)

	assert.Equal(t, apiModel.PlcResponseCode_OK, write(t, connection, "SDO:3:0x2000/1:UNSIGNED16", uint16(0xBEEF)))
	assert.Equal(t, []byte{0xEF, 0xBE}, node.get(0x2000, 1), "expedited, two bytes indicated")

	assert.Equal(t, apiModel.PlcResponseCode_OK, write(t, connection, "SDO:3:0x2000/2:INTEGER32", int32(-2)))
	assert.Equal(t, []byte{0xFE, 0xFF, 0xFF, 0xFF}, node.get(0x2000, 2))

	text := "a string longer than one segment, and than two"
	assert.Equal(t, apiModel.PlcResponseCode_OK, write(t, connection, "SDO:3:0x2000/3:VISIBLE_STRING", text))
	assert.Equal(t, []byte(text), node.get(0x2000, 3), "segmented")

	assert.Equal(t, apiModel.PlcResponseCode_OK, write(t, connection, "SDO:3:0x2000/4:RECORD", []byte{1, 2, 3, 4, 5, 6, 7}))
	assert.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7}, node.get(0x2000, 4), "seven bytes: one full segment")

	assert.Equal(t, apiModel.PlcResponseCode_OK, write(t, connection, "SDO:3:0x2000/5:REAL64", 2.5))
	assert.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0x04, 0x40}, node.get(0x2000, 5))

	assert.Equal(t, apiModel.PlcResponseCode_OK, write(t, connection, "SDO:3:0x2000/6:BOOLEAN", true))
	assert.Equal(t, []byte{0x01}, node.get(0x2000, 6), "TRUE is 1 (CiA 301 7.1.4.2)")

	assert.Equal(t, apiModel.PlcResponseCode_OK, write(t, connection, "SDO:3:0x2000/7:BOOLEAN[3]", []bool{true, false, true}))
	assert.Equal(t, []byte{0x05}, node.get(0x2000, 7), "bits 0 to 2")

	code, value := read(t, connection, "SDO:3:0x2000/3:VISIBLE_STRING")
	require.Equal(t, apiModel.PlcResponseCode_OK, code)
	assert.Equal(t, text, value.GetString(), "what was written reads back")
}

// SDO transfers to two nodes asked at once take turns on the client's SDO channel and each gets its own answer.
func TestSdoTransfersAtOnceEachGetTheirAnswer(t *testing.T) {
	connection := connect(t, "sdo-parallel", "")
	first := newTestNode(t, "sdo-parallel", 1, 1)
	second := newTestNode(t, "sdo-parallel", 2, 2)
	first.set(0x1018, 1, 0x11, 0x11, 0x00, 0x00)
	second.set(0x1018, 1, 0x22, 0x22, 0x00, 0x00)
	first.set(0x1008, 0, []byte("first node's name")...)
	second.set(0x1008, 0, []byte("second node's name")...)

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			code, value := read(t, connection, "SDO:1:0x1018/1:UNSIGNED32")
			assert.Equal(t, apiModel.PlcResponseCode_OK, code)
			assert.Equal(t, uint32(0x1111), value.GetUint32())
		})
		wg.Go(func() {
			code, value := read(t, connection, "SDO:2:0x1008/0:VISIBLE_STRING")
			assert.Equal(t, apiModel.PlcResponseCode_OK, code)
			assert.Equal(t, "second node's name", value.GetString())
		})
	}
	wg.Wait()
}

// plc4j's CANOpenDriverNMTIT: writing PRE_OPERATIONAL (128) to NMT:1 sends the NMT command 0x80 for node 1 on COB-ID
// 0 (CiA 301 7.2.8.3.1).
func TestNmtWritesSendTheCommandAsPlc4jsDriverTestDescribes(t *testing.T) {
	connection := connect(t, "nmt-write", "?node-id=15")
	node := newTestNode(t, "nmt-write", 1, 1)

	assert.Equal(t, apiModel.PlcResponseCode_OK, write(t, connection, "NMT:1", uint8(128)))
	assert.Equal(t, frame{cobId: 0x000, data: []byte{0x80, 0x01}}, node.next(t))

	assert.Equal(t, apiModel.PlcResponseCode_OK, write(t, connection, "NMT", "START"))
	assert.Equal(t, frame{cobId: 0x000, data: []byte{0x01, 0x00}}, node.next(t), "node 0 is every node")

	assert.Equal(t, apiModel.PlcResponseCode_INVALID_DATA, write(t, connection, "NMT:1", uint8(3)), "3 is no NMT command")
}

func TestPdoWritesSendTheValueAsThePdosData(t *testing.T) {
	connection := connect(t, "pdo-write", "")
	node := newTestNode(t, "pdo-write", 3, 3)

	assert.Equal(t, apiModel.PlcResponseCode_OK, write(t, connection, "RECEIVE_PDO_1:3:INTEGER16", int16(-2)))
	assert.Equal(t, frame{cobId: 0x203, data: []byte{0xFE, 0xFF}}, node.next(t))

	assert.Equal(t, apiModel.PlcResponseCode_OK, write(t, connection, "RECEIVE_PDO_4:3:UNSIGNED16[4]", []uint16{1, 2, 3, 4}))
	assert.Equal(t, frame{cobId: 0x503, data: []byte{1, 0, 2, 0, 3, 0, 4, 0}}, node.next(t))

	assert.Equal(t, apiModel.PlcResponseCode_INVALID_DATA, write(t, connection, "RECEIVE_PDO_1:3:RECORD", make([]byte, 9)),
		"a PDO carries eight bytes")
}

func TestPdoSubscriptionsAndReadsGetTheNodesPdo(t *testing.T) {
	connection := connect(t, "pdo-subscribe", "")
	node := newTestNode(t, "pdo-subscribe", 7, 7)
	events := subscribe(t, connection, map[string]string{"speed": "TRANSMIT_PDO_1:7:UNSIGNED16"})

	code, _ := read(t, connection, "TRANSMIT_PDO_1:7:UNSIGNED16")
	assert.Equal(t, apiModel.PlcResponseCode_REQUEST_TIMEOUT, code, "no PDO came yet")

	node.send(0x187, 0x34, 0x12, 0xFF)
	event := nextEvent(t, events)
	require.Equal(t, apiModel.PlcResponseCode_OK, event.GetResponseCode("speed"))
	assert.Equal(t, uint16(0x1234), event.GetValue("speed").GetUint16())

	node.send(0x188, 0x00, 0x00) // node 8's
	node.send(0x287, 0x00, 0x00) // node 7's TPDO2
	noEvent(t, events)

	code, value := read(t, connection, "TRANSMIT_PDO_1:7:UNSIGNED16")
	require.Equal(t, apiModel.PlcResponseCode_OK, code)
	assert.Equal(t, uint16(0x1234), value.GetUint16(), "the last PDO")

	node.send(0x187, 0x01)
	assert.Equal(t, apiModel.PlcResponseCode_INVALID_DATA, nextEvent(t, events).GetResponseCode("speed"), "one byte is no UNSIGNED16")
}

func TestHeartbeatSubscriptionsAndReadsGetTheNodesState(t *testing.T) {
	connection := connect(t, "heartbeat", "")
	node := newTestNode(t, "heartbeat", 9, 9)
	events := subscribe(t, connection, map[string]string{"node9": "HEARTBEAT:9", "any": "HEARTBEAT"})

	node.send(0x709, 0x00) // boot-up (7.2.8.3.2.1)
	node.send(0x709, 0x05) // OPERATIONAL (7.2.8.3.2.2)
	node.send(0x70A, 0x7F) // node 10, PRE-OPERATIONAL

	got := map[string][]string{}
	for range 5 {
		event := nextEvent(t, events)
		for _, name := range event.GetTagNames() {
			value := event.GetValue(name)
			got[name] = append(got[name], value.GetStruct()["state"].GetString()+"@"+value.GetStruct()["node"].GetString())
		}
	}
	assert.Equal(t, []string{"0@9", "5@9"}, got["node9"])
	assert.Equal(t, []string{"0@9", "5@9", "127@10"}, got["any"])

	code, value := read(t, connection, "HEARTBEAT:9")
	require.Equal(t, apiModel.PlcResponseCode_OK, code)
	assert.Equal(t, uint8(5), value.GetStruct()["state"].GetUint8())
}

// An NMT command's COB-ID carries no node: the node it addresses is its second byte, and 0 addresses every node.
func TestNmtSubscriptionsGetTheCommandsForTheirNode(t *testing.T) {
	connection := connect(t, "nmt-subscribe", "")
	master := newTestNode(t, "nmt-subscribe", 1, 1)
	events := subscribe(t, connection, map[string]string{"node5": "NMT:5"})

	master.send(0x000, 0x02, 0x05) // stop node 5
	event := nextEvent(t, events)
	assert.Equal(t, uint8(2), event.GetValue("node5").GetStruct()["state"].GetUint8())
	assert.Equal(t, uint8(5), event.GetValue("node5").GetStruct()["node"].GetUint8())

	master.send(0x000, 0x01, 0x06) // start node 6
	noEvent(t, events)

	master.send(0x000, 0x81, 0x00) // reset every node
	assert.Equal(t, uint8(0x81), nextEvent(t, events).GetValue("node5").GetStruct()["state"].GetUint8())
}

// CiA 301 7.3.5: the EMCY of node n goes out at 0x080 + n and SYNC at 0x080. CANOpenService's value for EMCY (0b1101,
// which would be 0x680 + n) never reaches the bus.
func TestSyncAndEmergencyGoOutAtTheirCobIds(t *testing.T) {
	connection := connect(t, "sync-emcy", "")
	node := newTestNode(t, "sync-emcy", 1, 1)
	driver, ok := connection.(*Connection)
	require.True(t, ok)

	require.NoError(t, driver.sendPayload(context.Background(), readWriteModel.CANOpenService_EMCY, 5,
		readWriteModel.NewCANOpenEmergencyPayload(0x2310, 0x02, []byte{1, 2, 3, 4, 5})))
	assert.Equal(t, frame{cobId: 0x085, data: []byte{0x10, 0x23, 0x02, 1, 2, 3, 4, 5}}, node.next(t))

	require.NoError(t, driver.sendPayload(context.Background(), readWriteModel.CANOpenService_SYNC, 0,
		readWriteModel.NewCANOpenSyncPayload()))
	assert.Equal(t, frame{cobId: 0x080, data: []byte{}}, node.next(t))
}

func TestTheHeartbeatProducerAnnouncesTheConnection(t *testing.T) {
	node := newTestNode(t, "producer", 1, 1)
	connect(t, "producer", "?node-id=10&heartbeat=true")
	assert.Equal(t, frame{cobId: 0x70A, data: []byte{0x00}}, node.next(t), "the boot-up message")
}

func TestTheConfigurationIsPlc4jsAndNoMore(t *testing.T) {
	manager := plc4go.NewPlcDriverManager()
	defer func() { _ = manager.Close() }()
	manager.RegisterDriver(NewDriver())
	manager.(spi.TransportAware).RegisterTransport(virtualcan.NewTransport())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, options := range []string{"?heartbeat=true", "?node-id=128", "?request-timeout-ms=0"} {
		_, err := manager.GetConnection(ctx, "canopen:can-virtualcan://config"+options)
		assert.Error(t, err, options)
	}
	_, err := manager.GetConnection(ctx, "canopen:tcp://127.0.0.1:1")
	assert.Error(t, err)

	connection := connect(t, "config", "?node-id=127&heartbeat=false&request-timeout-ms=250")
	metadata := connection.GetMetadata()
	assert.True(t, metadata.CanRead())
	assert.True(t, metadata.CanWrite())
	assert.True(t, metadata.CanSubscribe())
	assert.False(t, metadata.CanBrowse())
}
