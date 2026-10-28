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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
)

// The COB-IDs of CiA 301's predefined connection set (7.3.5, table 72).
func TestCobIdsResolveToTheirServiceAndNode(t *testing.T) {
	tests := []struct {
		cobId   uint16
		service readWriteModel.CANOpenService
		nodeId  uint8
	}{
		{0x000, readWriteModel.CANOpenService_NMT, 0},
		{0x080, readWriteModel.CANOpenService_SYNC, 0},
		{0x081, readWriteModel.CANOpenService_EMCY, 1},
		{0x0FF, readWriteModel.CANOpenService_EMCY, 127},
		{0x100, readWriteModel.CANOpenService_TIME, 0},
		{0x185, readWriteModel.CANOpenService_TRANSMIT_PDO_1, 5},
		{0x205, readWriteModel.CANOpenService_RECEIVE_PDO_1, 5},
		{0x4FF, readWriteModel.CANOpenService_TRANSMIT_PDO_4, 127},
		{0x585, readWriteModel.CANOpenService_TRANSMIT_SDO, 5},
		{0x605, readWriteModel.CANOpenService_RECEIVE_SDO, 5},
		{0x705, readWriteModel.CANOpenService_HEARTBEAT, 5},
	}
	for _, test := range tests {
		service, nodeId, ok := resolve(test.cobId)
		require.True(t, ok, "0x%03X", test.cobId)
		assert.Equal(t, test.service, service, "0x%03X", test.cobId)
		assert.Equal(t, test.nodeId, nodeId, "0x%03X", test.cobId)
		assert.Equal(t, test.cobId, cobIdOf(service, nodeId), "0x%03X", test.cobId)
	}
	_, _, ok := resolve(0x7E5) // LSS (CiA 305), not a CiA 301 service
	assert.False(t, ok)
	// CANOpenService gives EMCY the value 0b1101 only because an enum needs one per constant. Were it ever put on the
	// bus, node 5's EMCY would be 0x685: no predefined service, which the driver passes over.
	_, _, ok = resolve(0x685)
	assert.False(t, ok)
}

// CiA 301 7.2.7.3.1: an EMCY's error code (here 0x2310, current at the output), error register and five
// manufacturer-specific bytes; and 7.2.5.3.1: a SYNC without a counter is empty.
func TestSyncAndEmergencyDecode(t *testing.T) {
	payload, err := readWriteModel.CANOpenPayloadParse[readWriteModel.CANOpenPayload](context.Background(),
		[]byte{0x10, 0x23, 0x02, 0x01, 0x02, 0x03, 0x04, 0x05}, readWriteModel.CANOpenService_EMCY)
	require.NoError(t, err)
	emergency, ok := payload.(readWriteModel.CANOpenEmergencyPayload)
	require.True(t, ok)
	assert.Equal(t, uint16(0x2310), emergency.GetErrorCode())
	assert.Equal(t, uint8(0x02), emergency.GetErrorRegister())
	assert.Equal(t, []byte{1, 2, 3, 4, 5}, emergency.GetManufacturerSpecificData())

	payload, err = readWriteModel.CANOpenPayloadParse[readWriteModel.CANOpenPayload](context.Background(), []byte{}, readWriteModel.CANOpenService_SYNC)
	require.NoError(t, err)
	_, ok = payload.(readWriteModel.CANOpenSyncPayload)
	assert.True(t, ok)
}
