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

package genericcan

import (
	"context"
	"encoding/binary"
	"net/url"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plc4go "github.com/apache/plc4x/plc4go/pkg/api"
	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	"github.com/apache/plc4x/plc4go/spi"
	spiTransports "github.com/apache/plc4x/plc4go/spi/transports"
	"github.com/apache/plc4x/plc4go/spi/transports/can"
	"github.com/apache/plc4x/plc4go/spi/transports/socketcan"
	"github.com/apache/plc4x/plc4go/spi/transports/virtualcan"
)

func testLogger(t *testing.T) zerolog.Logger {
	return zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.WarnLevel)
}

// rig is a driver connection and a bare endpoint (the "device") on one virtual bus.
type rig struct {
	connection plc4go.PlcConnection
	device     spiTransports.TransportInstance
}

func newRig(t *testing.T, bus string, connectionOptions string) rig {
	t.Helper()
	manager := plc4go.NewPlcDriverManager()
	t.Cleanup(func() { _ = manager.Close() })
	manager.RegisterDriver(NewDriver())
	manager.(spi.TransportAware).RegisterTransport(virtualcan.NewTransport())
	manager.(spi.TransportAware).RegisterTransport(socketcan.NewTransport())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := manager.GetConnection(ctx, "genericcan:can-virtualcan://"+bus+connectionOptions)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })

	device, err := virtualcan.NewTransport().CreateTransportInstance(url.URL{Scheme: virtualcan.TransportCode, Host: bus}, nil)
	require.NoError(t, err)
	require.NoError(t, device.Connect(context.Background()))
	t.Cleanup(func() { _ = device.Close() })
	return rig{connection: connection, device: device}
}

func (r rig) send(t *testing.T, rawId uint32, data ...byte) {
	t.Helper()
	frame, err := can.EncodeFrame(rawId, data)
	require.NoError(t, err)
	require.NoError(t, r.device.Write(context.Background(), frame))
}

func (r rig) receive(t *testing.T) (uint32, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	frame, err := r.device.Read(ctx, can.FrameSize)
	require.NoError(t, err)
	return binary.LittleEndian.Uint32(frame), frame[8 : 8+frame[4]]
}

func TestTheConnectionStringPicksTheTransport(t *testing.T) {
	r := newRig(t, "metadata", "")
	metadata := r.connection.GetMetadata()
	assert.True(t, metadata.CanRead())
	assert.True(t, metadata.CanWrite())
	assert.True(t, metadata.CanSubscribe())
	assert.False(t, metadata.CanBrowse())

	manager := plc4go.NewPlcDriverManager()
	defer func() { _ = manager.Close() }()
	manager.RegisterDriver(NewDriver())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := manager.GetConnection(ctx, "genericcan:tcp://127.0.0.1:1")
	assert.Error(t, err)
}

func TestSubscriptionsGetEveryFrameOfTheirIdentifier(t *testing.T) {
	r := newRig(t, "subscribe", "")
	events := make(chan apiModel.PlcSubscriptionEvent, 10)
	request, err := r.connection.SubscriptionRequestBuilder().
		AddEventTagAddress("speed", "419361024:UNSIGNED16").
		AddEventTagAddress("gear", "419361024:INTEGER8").
		AddEventTagAddress("other", "291:BOOLEAN").
		AddPreRegisteredConsumer("speed", func(event apiModel.PlcSubscriptionEvent) { events <- event }).
		Build()
	require.NoError(t, err)
	result := <-request.Execute(context.Background())
	require.NoError(t, result.GetErr())
	assert.Equal(t, apiModel.PlcResponseCode_OK, result.GetResponse().GetResponseCode("speed"))

	r.send(t, 0x123, 1)                                 // another identifier: no event for this handle
	r.send(t, can.EffFlag|0x18FEF100, 0x10, 0x27, 0xFD) // 10000, -3
	r.send(t, can.EffFlag|0x18FEF100, 0x11, 0x27, 0x02)
	for _, want := range []struct {
		speed uint16
		gear  int8
	}{{10000, -3}, {10001, 2}} {
		select {
		case event := <-events:
			assert.Equal(t, apiModel.PlcResponseCode_OK, event.GetResponseCode("speed"))
			assert.Equal(t, want.speed, event.GetValue("speed").GetUint16())
			assert.Equal(t, want.gear, event.GetValue("gear").GetInt8())
		case <-time.After(2 * time.Second):
			t.Fatal("no event")
		}
	}
}

func TestReadsAnswerTheLastFrame(t *testing.T) {
	r := newRig(t, "read", "?request-timeout-ms=300")
	read := func() apiModel.PlcReadResponse {
		request, err := r.connection.ReadRequestBuilder().AddTagAddress("v", "291:UNSIGNED32").Build()
		require.NoError(t, err)
		result := <-request.Execute(context.Background())
		require.NoError(t, result.GetErr())
		return result.GetResponse()
	}
	started := time.Now()
	assert.Equal(t, apiModel.PlcResponseCode_REQUEST_TIMEOUT, read().GetResponseCode("v"), "nothing came with 0x123 yet")
	assert.Less(t, time.Since(started), 2*time.Second)

	r.send(t, 0x123, 0x78, 0x56, 0x34, 0x12)
	r.send(t, 0x124, 0xFF, 0xFF, 0xFF, 0xFF)
	require.Eventually(t, func() bool { return read().GetResponseCode("v") == apiModel.PlcResponseCode_OK }, 2*time.Second, 20*time.Millisecond)
	assert.Equal(t, uint32(0x12345678), read().GetValue("v").GetUint32())
}

func TestWritesSendOneFramePerIdentifier(t *testing.T) {
	r := newRig(t, "write", "")
	request, err := r.connection.WriteRequestBuilder().
		AddTagAddress("a", "291:UNSIGNED16", uint16(0xBEEF)).
		AddTagAddress("ext", "419361024:RAW", []byte{1, 2, 3, 4, 5, 6, 7, 8}).
		AddTagAddress("b", "291:BOOLEAN", true).
		Build()
	require.NoError(t, err)
	result := <-request.Execute(context.Background())
	require.NoError(t, result.GetErr())
	for _, name := range []string{"a", "ext", "b"} {
		assert.Equal(t, apiModel.PlcResponseCode_OK, result.GetResponse().GetResponseCode(name), name)
	}
	rawId, data := r.receive(t)
	assert.Equal(t, uint32(0x123), rawId, "a standard identifier")
	assert.Equal(t, []byte{0xEF, 0xBE, 0x80}, data, "0xBEEF, then the BOOLEAN in the next bit")
	rawId, data = r.receive(t)
	assert.Equal(t, can.EffFlag|0x18FEF100, rawId, "an identifier above 0x7FF goes out extended")
	assert.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7, 8}, data)

	// A frame that would not fit is not sent at all.
	request, err = r.connection.WriteRequestBuilder().
		AddTagAddress("long", "5:UNSIGNED64", uint64(1)).
		AddTagAddress("over", "5:UNSIGNED8", uint8(1)).
		Build()
	require.NoError(t, err)
	result = <-request.Execute(context.Background())
	require.NoError(t, result.GetErr())
	assert.Equal(t, apiModel.PlcResponseCode_INVALID_DATA, result.GetResponse().GetResponseCode("over"))
	available, err := r.device.GetNumBytesAvailableInBuffer()
	require.NoError(t, err)
	assert.Zero(t, available)
}

func TestClosingEndsTheConnection(t *testing.T) {
	r := newRig(t, "close", "")
	require.True(t, r.connection.IsConnected())
	require.NoError(t, r.connection.Close())
	assert.False(t, r.connection.IsConnected())
}
