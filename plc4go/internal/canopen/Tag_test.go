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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
)

// The addresses and what they parse to are plc4j's tag tests' (CANOpenSDOTagTest, CANOpenTagSDOTest, CANOpenPDOTagTest,
// CANOpenNMTTagTest) and the addresses of its driver tests (CANOpenDriverSDOIT, CANOpenDriverNMTIT).
func TestTagsParseAsPlc4jParsesThem(t *testing.T) {
	tests := []struct {
		address string
		want    any
	}{
		{"SDO:20:0x30/40:BOOLEAN", SDOTag{NodeId: 20, AnswerNodeId: 20, Index: 0x30, SubIndex: 40, DataType: readWriteModel.CANOpenDataType_BOOLEAN}},
		{"SDO:20:0x10/0xAA:RECORD", SDOTag{NodeId: 20, AnswerNodeId: 20, Index: 0x10, SubIndex: 0xAA, DataType: readWriteModel.CANOpenDataType_RECORD}},
		{"SDO:20/22:0x10/0xAA:RECORD", SDOTag{NodeId: 20, AnswerNodeId: 22, Index: 0x10, SubIndex: 0xAA, DataType: readWriteModel.CANOpenDataType_RECORD}},
		{"SDO:1:1000/22:UNSIGNED32", SDOTag{NodeId: 1, AnswerNodeId: 1, Index: 1000, SubIndex: 22, DataType: readWriteModel.CANOpenDataType_UNSIGNED32}},
		{"SDO:1:9186/1:UNSIGNED8", SDOTag{NodeId: 1, AnswerNodeId: 1, Index: 9186, SubIndex: 1, DataType: readWriteModel.CANOpenDataType_UNSIGNED8}},
		{"SDO:1:0x6000/1:INTEGER16[4]", SDOTag{NodeId: 1, AnswerNodeId: 1, Index: 0x6000, SubIndex: 1, DataType: readWriteModel.CANOpenDataType_INTEGER16, Elements: 4}},
		{"RECEIVE_PDO_2:20:RECORD", PDOTag{NodeId: 20, Service: readWriteModel.CANOpenService_RECEIVE_PDO_2, DataType: readWriteModel.CANOpenDataType_RECORD}},
		{"TRANSMIT_PDO_1:5:UNSIGNED16", PDOTag{NodeId: 5, Service: readWriteModel.CANOpenService_TRANSMIT_PDO_1, DataType: readWriteModel.CANOpenDataType_UNSIGNED16}},
		{"NMT:20", NMTTag{NodeId: 20}},
		{"NMT:0", NMTTag{NodeId: 0}},
		{"NMT", NMTTag{NodeId: 0}},
		{"NMT:1", NMTTag{NodeId: 1}},
		{"HEARTBEAT", HeartbeatTag{NodeId: 0}},
		{"HEARTBEAT:5", HeartbeatTag{NodeId: 5}},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			tag, err := NewTagHandler().ParseTag(test.address)
			require.NoError(t, err)
			assert.Equal(t, test.want, tag)
		})
	}
}

func TestTagsPlc4jRefusesAreRefused(t *testing.T) {
	for _, address := range []string{"SDO:", "PDO:", "NMT:", "HEARTBEAT:", "SDO:1:1000/22", "TRANSMIT_PDO_5:1:UNSIGNED8", "SDO:1:1000/22:NOT_A_TYPE"} {
		_, err := NewTagHandler().ParseTag(address)
		assert.Error(t, err, address)
	}
}

func TestTagsGoRefusesWhatNoCANopenNodeHas(t *testing.T) {
	for _, address := range []string{
		"SDO:0:1000/22:UNSIGNED32",   // node-IDs are 1 to 127
		"SDO:128:1000/22:UNSIGNED32", //
		"SDO:1/0:1000/22:UNSIGNED32", //
		"TRANSMIT_PDO_1:0:UNSIGNED8", //
		"NMT:128",                    //
		"SDO:1:0x10000/0:UNSIGNED8",  // an index is 16 bits
		"SDO:1:0x1000/256:UNSIGNED8", // a sub-index 8
		"SDO:1:0x1000/0:TIME_OF_DAY", // the dataIo cannot read it
	} {
		_, err := NewTagHandler().ParseTag(address)
		assert.Error(t, err, address)
	}
}

// An index plc4j's signed short cannot hold is an ordinary one: device profiles start at 0x6000 and run to 0x9FFF.
func TestTagsTakeTheWholeIndexRange(t *testing.T) {
	tag, err := NewTagHandler().ParseTag("SDO:3:0x9FFF/0xFF:UNSIGNED8")
	require.NoError(t, err)
	assert.Equal(t, uint16(0x9FFF), tag.(SDOTag).Index)
}

func TestAddressStringsParseBackToTheSameTag(t *testing.T) {
	for _, address := range []string{
		"SDO:1:1000/22:UNSIGNED32", "SDO:1/2:1000/22:UNSIGNED32", "SDO:1:24576/1:INTEGER16[4]",
		"TRANSMIT_PDO_3:7:REAL32", "RECEIVE_PDO_4:7:UNSIGNED8[8]", "NMT", "NMT:9", "HEARTBEAT", "HEARTBEAT:9",
	} {
		tag, err := NewTagHandler().ParseTag(address)
		require.NoError(t, err, address)
		assert.Equal(t, address, tag.GetAddressString())
		again, err := NewTagHandler().ParseTag(tag.GetAddressString())
		require.NoError(t, err, address)
		assert.Equal(t, tag, again)
	}
}

func TestTagsReportTheTypeTheirValueIs(t *testing.T) {
	parse := func(address string) apiValues.PlcValueType {
		tag, err := NewTagHandler().ParseTag(address)
		require.NoError(t, err)
		return tag.GetValueType()
	}
	assert.Equal(t, apiValues.BOOL, parse("SDO:1:1/1:BOOLEAN"))
	assert.Equal(t, apiValues.UDINT, parse("SDO:1:1/1:UNSIGNED24"))
	assert.Equal(t, apiValues.RAW_BYTE_ARRAY, parse("SDO:1:1/1:RECORD"))
	assert.Equal(t, apiValues.STRING, parse("SDO:1:1/1:VISIBLE_STRING"))
	assert.Equal(t, apiValues.USINT, parse("NMT:1"))
	assert.Equal(t, apiValues.Struct, parse("HEARTBEAT:1"))
}
