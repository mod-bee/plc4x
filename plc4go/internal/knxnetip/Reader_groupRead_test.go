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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	driverModel "github.com/apache/plc4x/plc4go/protocols/knxnetip/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi"
	"github.com/apache/plc4x/plc4go/spi/errors"
)

// Frames knxd 0.14 sent a tunneling client (channel 1) after it put a GroupValueRead for 1/1/4
// onto the bus, captured off the wire: the device 1.1.10 answering with a GroupValueResponse
// (DPT 8.001, -1234), and the same device sending a GroupValueWrite to 1/1/5 in between.
const (
	knxdGroupValueResponse114 = "061004200017040101002900bcd0110a0904030040fb2e"
	knxdGroupValueWrite115    = "061004200019040102002900bcd0110a090505008000000142"
)

func knxdFrame(t *testing.T, frame string) spi.Message {
	t.Helper()
	data, err := hex.DecodeString(frame)
	require.NoError(t, err)
	message, err := driverModel.KnxNetIpMessageParse[driverModel.KnxNetIpMessage](context.Background(), data)
	require.NoError(t, err)
	return message
}

// Test_Connection_ReadGroupAddress_takesTheResponseForTheGroupAddress: a GroupValueResponse is
// addressed TO the group address and comes FROM the device answering. The response used to be
// matched by its source address, so no group read ever got its answer.
func Test_Connection_ReadGroupAddress_takesTheResponseForTheGroupAddress(t *testing.T) {
	connection, _ := newWriterConnection(t, nil, func(_ *writerCodec, _ spi.Message, handleMessage spi.HandleMessage, _ spi.HandleError) error {
		// Another telegram first, then the answer, as they came on the bus.
		if err := handleMessage(knxdFrame(t, knxdGroupValueWrite115)); err == nil {
			return errors.New("a GroupValueWrite to another group address was taken as the response")
		}
		return handleMessage(knxdFrame(t, knxdGroupValueResponse114))
	})
	connection.CommunicationChannelId = 1

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	datapointType := driverModel.KnxDatapointType_DPT_Value_2_Count
	select {
	case result := <-connection.ReadGroupAddress(ctx, []byte{0x09, 0x04}, &datapointType):
		require.NoError(t, result.err)
		assert.Equal(t, int64(-1234), result.value.GetInt64())
	case <-ctx.Done():
		require.FailNow(t, "the group read never completed")
	}
}

// Test_Connection_ReadGroupAddress_ignoresResponsesForOtherGroupAddresses: the answer for 1/1/4
// is not the answer for 1/1/3.
func Test_Connection_ReadGroupAddress_ignoresResponsesForOtherGroupAddresses(t *testing.T) {
	connection, _ := newWriterConnection(t, nil, func(_ *writerCodec, _ spi.Message, handleMessage spi.HandleMessage, _ spi.HandleError) error {
		assert.Error(t, handleMessage(knxdFrame(t, knxdGroupValueResponse114)))
		return errors.New("no answer")
	})
	connection.CommunicationChannelId = 1

	datapointType := driverModel.KnxDatapointType_DPT_Value_2_Count
	result := <-connection.ReadGroupAddress(context.Background(), []byte{0x09, 0x03}, &datapointType)
	assert.Error(t, result.err)
}
