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

package socketcan

import (
	"context"
	"net"
	"net/url"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apache/plc4x/plc4go/spi/transports/can"
)

func TestTheInterfaceIsNamedByAddressOrOption(t *testing.T) {
	name, err := InterfaceName(url.URL{Scheme: TransportCode, Host: "can0"}, map[string][]string{interfaceNameOption: {"can1"}})
	require.NoError(t, err)
	assert.Equal(t, "can0", name, "the address wins")
	name, err = InterfaceName(url.URL{Scheme: TransportCode}, map[string][]string{interfaceNameOption: {"vcan0"}})
	require.NoError(t, err)
	assert.Equal(t, "vcan0", name)
	for _, bad := range []string{"", "a-name-far-too-long-for-linux", "can 0"} {
		_, err = InterfaceName(url.URL{Scheme: TransportCode}, map[string][]string{interfaceNameOption: {bad}})
		assert.Error(t, err, bad)
	}
}

func TestAMissingInterfaceIsRefusedOnConnect(t *testing.T) {
	instance, err := NewTransport().CreateTransportInstance(url.URL{Scheme: TransportCode, Host: "nocan9"}, nil)
	require.NoError(t, err)
	err = instance.Connect(context.Background())
	require.Error(t, err)
	if runtime.GOOS != "linux" {
		assert.Contains(t, err.Error(), "Linux-only")
	}
}

// Two sockets on vcan0 see each other's frames. It needs the interface (edge rig: ip link add dev vcan0 type vcan),
// and is skipped where there is none.
func TestSocketsOnOneInterfaceSeeEachOther(t *testing.T) {
	if _, err := net.InterfaceByName("vcan0"); err != nil {
		t.Skip("no vcan0 here")
	}
	connect := func() *can.TransportInstance {
		instance, err := NewTransport().CreateTransportInstance(url.URL{Scheme: TransportCode, Host: "vcan0"}, nil)
		require.NoError(t, err)
		require.NoError(t, instance.Connect(context.Background()))
		t.Cleanup(func() { _ = instance.Close() })
		return instance.(*can.TransportInstance)
	}
	a, b := connect(), connect()
	for _, rawId := range []uint32{0x123, can.EffFlag | 0x18FEF100} {
		frame, _ := can.EncodeFrame(rawId, []byte{0xDE, 0xAD})
		require.NoError(t, a.Write(context.Background(), frame))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		var received []byte
		var err error
		// Skip whatever else is on the bus (a running simulator) until our frame comes by.
		for received == nil || string(received) != string(frame) {
			received, err = b.Read(ctx, can.FrameSize)
			require.NoError(t, err)
		}
		cancel()
	}
}
