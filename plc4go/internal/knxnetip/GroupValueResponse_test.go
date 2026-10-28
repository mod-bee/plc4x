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

package knxnetip

import (
	"context"
	"encoding/hex"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	driverModel "github.com/apache/plc4x/plc4go/protocols/knxnetip/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/testutils"
)

// knxdResponse114 is a GroupValueResponse knxd 0.14 sent a tunneling client (channel 1),
// captured off the wire: the device 1.1.10 answering a read of 1/1/4 with -1234 (DPT 8.001).
const knxdResponse114 = "061004200017040101002900bcd0110a0904030040fb2e"

func knxdTunnelingRequest(t *testing.T, frame string) driverModel.TunnelingRequest {
	t.Helper()
	data, err := hex.DecodeString(frame)
	require.NoError(t, err)
	message, err := driverModel.KnxNetIpMessageParse[driverModel.KnxNetIpMessage](context.Background(), data)
	require.NoError(t, err)
	tunnelingRequest, ok := message.(driverModel.TunnelingRequest)
	require.True(t, ok)
	return tunnelingRequest
}

// Test_Connection_aGroupValueResponseOnTheBusUpdatesCacheAndSubscribers: a response to another
// client's read carries the group address's value as a write does. It used to be dropped, so a
// subscriber never saw it and the cache kept the value from before.
func Test_Connection_aGroupValueResponseOnTheBusUpdatesCacheAndSubscribers(t *testing.T) {
	connection := newSubscriberTestConnection(t)
	connection.CommunicationChannelId = 1
	subscriber := NewSubscriber(connection, options.WithCustomLogger(testutils.ProduceTestingLogger(t)))
	connection.addSubscriber(subscriber)
	handles := subscribe(t, connection, subscriber, apiModel.SubscriptionEvent, map[string]string{"count": "1/1/4:INT"})
	var seen atomic.Int64
	handles["count"].Register(func(event apiModel.PlcSubscriptionEvent) { seen.Store(event.GetValue("count").GetInt64()) })

	connection.handleIncomingTunnelingRequest(t.Context(), knxdTunnelingRequest(t, knxdResponse114))
	connection.wg.Wait()

	assert.Equal(t, int64(-1234), seen.Load())
	assert.Equal(t, []byte{0x00, 0xfb, 0x2e}, connection.valueCache[1<<11|1<<8|4])
}
