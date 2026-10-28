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

package virtualcan

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apache/plc4x/plc4go/spi/transports"
	"github.com/apache/plc4x/plc4go/spi/transports/can"
)

func connect(t *testing.T, address string, transportOptions map[string][]string) transports.TransportInstance {
	t.Helper()
	u, err := url.Parse(address)
	require.NoError(t, err)
	instance, err := NewTransport().CreateTransportInstance(*u, transportOptions)
	require.NoError(t, err)
	require.NoError(t, instance.Connect(context.Background()))
	t.Cleanup(func() { _ = instance.Close() })
	return instance
}

func TestFramesReachEveryOtherMemberOfTheBus(t *testing.T) {
	a := connect(t, "can-virtualcan://frames", nil)
	b := connect(t, "can-virtualcan://frames", nil)
	other := connect(t, "can-virtualcan://another-bus", nil)

	frame, err := can.EncodeFrame(can.EffFlag|0x18FEF100, []byte{1, 2, 3})
	require.NoError(t, err)
	require.NoError(t, a.Write(context.Background(), frame))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	received, err := b.Read(ctx, can.FrameSize)
	require.NoError(t, err)
	assert.Equal(t, frame, received)

	// Not back to the writer, and not to another bus.
	for _, silent := range []transports.TransportInstance{a, other} {
		available, err := silent.GetNumBytesAvailableInBuffer()
		require.NoError(t, err)
		assert.Zero(t, available)
	}
}

func TestReadsHonourTheContextDeadline(t *testing.T) {
	a := connect(t, "can-virtualcan://quiet", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := a.PeekReadableBytes(ctx, 1)
	require.Error(t, err)
	assert.Less(t, time.Since(started), time.Second)
	assert.Equal(t, transports.TransportErrorRetryable, a.ClassifyError(err))
}

func TestWritesAreWholeClassicFrames(t *testing.T) {
	a := connect(t, "can-virtualcan://shapes", nil)
	assert.Error(t, a.Write(context.Background(), make([]byte, can.FrameSize-1)))
	tooLong := make([]byte, can.FrameSize)
	tooLong[4] = 9
	assert.Error(t, a.Write(context.Background(), tooLong))
	_, err := can.EncodeFrame(0x123, make([]byte, 9))
	assert.Error(t, err)
}

func TestTheBusIsNamedByAddressOrOption(t *testing.T) {
	a := connect(t, "can-virtualcan://", map[string][]string{busNameOption: {"by-option"}})
	b := connect(t, "can-virtualcan://by-option", nil)
	frame, _ := can.EncodeFrame(0x123, []byte{9})
	require.NoError(t, a.Write(context.Background(), frame))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	received, err := b.Read(ctx, can.FrameSize)
	require.NoError(t, err)
	assert.Equal(t, frame, received)

	_, err = NewTransport().CreateTransportInstance(url.URL{Scheme: TransportCode}, nil)
	assert.Error(t, err)
}

func TestClosingEndsTheInstance(t *testing.T) {
	a := connect(t, "can-virtualcan://closing", nil)
	require.NoError(t, a.Close())
	assert.False(t, a.IsConnected())
	frame, _ := can.EncodeFrame(0x123, nil)
	assert.Error(t, a.Write(context.Background(), frame))
	require.NoError(t, a.Close())
}
