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
	"fmt"
	"time"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
	spiModel "github.com/apache/plc4x/plc4go/spi/model"
)

// SDOTag is plc4j's CANOpenSDOTag: one entry of a node's object dictionary, reached over the node's default SDO
// channel. AnswerNodeId is the node whose SDO responses answer the request; it is NodeId unless the address names
// another ("SDO:1/2:...").
type SDOTag struct {
	NodeId       uint8
	AnswerNodeId uint8
	Index        uint16
	SubIndex     uint8
	DataType     readWriteModel.CANOpenDataType
	// Elements is the address's [n]: n values of DataType, one after the other (plc4j parses it and ignores it).
	Elements uint8
}

// PDOTag is plc4j's CANOpenPDOTag: the data of one PDO of a node, read as DataType from its first byte - or, beyond
// plc4j, from the byte (and for BOOLEAN the bit) the address names before the type, as S7 names a byte and bit before
// its type (%DB1:4.2:BOOL): RECEIVE_PDO_1:5:2:INTEGER16, TRANSMIT_PDO_1:5:4.3:BOOLEAN.
type PDOTag struct {
	NodeId   uint8
	Service  readWriteModel.CANOpenService
	DataType readWriteModel.CANOpenDataType
	Elements uint8
	// Positioned: the address names the byte (0 to 7) the value starts at, and for BOOLEAN optionally its bit (0 to 7,
	// 0 the lowest). A write to a positioned tag changes only its bytes (or bits) of the PDO; see pdoPayload.
	Positioned bool
	ByteOffset uint8
	BitOffset  uint8
}

// NMTTag is plc4j's CANOpenNMTTag: the NMT commands addressed to a node, or to any (NodeId 0, the address "NMT").
type NMTTag struct {
	NodeId uint8
}

// HeartbeatTag is plc4j's CANOpenHeartbeatTag: the heartbeats (and boot-up) of a node, or of any (NodeId 0, the
// address "HEARTBEAT").
type HeartbeatTag struct {
	NodeId uint8
}

var (
	_ apiModel.PlcTag             = SDOTag{}
	_ apiModel.PlcSubscriptionTag = PDOTag{}
	_ apiModel.PlcSubscriptionTag = NMTTag{}
	_ apiModel.PlcSubscriptionTag = HeartbeatTag{}
)

// GetAddressString spells the tag as the tag handler parses it. plc4j's SDO tag writes ":/" before the answer node,
// which its own pattern does not read back, and its PDO tag leaves the node out; Go spells what it parses.
func (t SDOTag) GetAddressString() string {
	address := fmt.Sprintf("SDO:%d", t.NodeId)
	if t.AnswerNodeId != t.NodeId {
		address += fmt.Sprintf("/%d", t.AnswerNodeId)
	}
	return address + fmt.Sprintf(":%d/%d:%s", t.Index, t.SubIndex, t.DataType.String()) + elementsSuffix(t.Elements)
}

func (t PDOTag) GetAddressString() string {
	position := ""
	if t.Positioned {
		position = fmt.Sprintf(":%d", t.ByteOffset)
		if t.BitOffset > 0 {
			position += fmt.Sprintf(".%d", t.BitOffset)
		}
	}
	return fmt.Sprintf("%s:%d%s:%s", t.Service.String(), t.NodeId, position, t.DataType.String()) + elementsSuffix(t.Elements)
}

func (t NMTTag) GetAddressString() string {
	if t.NodeId == 0 {
		return "NMT"
	}
	return fmt.Sprintf("NMT:%d", t.NodeId)
}

func (t HeartbeatTag) GetAddressString() string {
	if t.NodeId == 0 {
		return "HEARTBEAT"
	}
	return fmt.Sprintf("HEARTBEAT:%d", t.NodeId)
}

func elementsSuffix(elements uint8) string {
	if elements > 1 {
		return fmt.Sprintf("[%d]", elements)
	}
	return ""
}

// valueTypes is the PLC4X type each data type's value is: the case the mspec's dataIo yields for it, and RAW_BYTE_ARRAY
// for RECORD, whose bytes Go hands over as they came.
//
// plc4j's tags report the enum's plcValueName instead, which names BOOLEAN's "BIT" (no PlcValueType) and the 24 to 56
// bit integers RAW_BYTE_ARRAY, where its dataIo yields a UDINT, ULINT, DINT or LINT; Go reports what the value is.
var valueTypes = map[readWriteModel.CANOpenDataType]apiValues.PlcValueType{
	readWriteModel.CANOpenDataType_BOOLEAN:        apiValues.BOOL,
	readWriteModel.CANOpenDataType_UNSIGNED8:      apiValues.USINT,
	readWriteModel.CANOpenDataType_UNSIGNED16:     apiValues.UINT,
	readWriteModel.CANOpenDataType_UNSIGNED24:     apiValues.UDINT,
	readWriteModel.CANOpenDataType_UNSIGNED32:     apiValues.UDINT,
	readWriteModel.CANOpenDataType_UNSIGNED40:     apiValues.ULINT,
	readWriteModel.CANOpenDataType_UNSIGNED48:     apiValues.ULINT,
	readWriteModel.CANOpenDataType_UNSIGNED56:     apiValues.ULINT,
	readWriteModel.CANOpenDataType_UNSIGNED64:     apiValues.ULINT,
	readWriteModel.CANOpenDataType_INTEGER8:       apiValues.SINT,
	readWriteModel.CANOpenDataType_INTEGER16:      apiValues.INT,
	readWriteModel.CANOpenDataType_INTEGER24:      apiValues.DINT,
	readWriteModel.CANOpenDataType_INTEGER32:      apiValues.DINT,
	readWriteModel.CANOpenDataType_INTEGER40:      apiValues.LINT,
	readWriteModel.CANOpenDataType_INTEGER48:      apiValues.LINT,
	readWriteModel.CANOpenDataType_INTEGER56:      apiValues.LINT,
	readWriteModel.CANOpenDataType_INTEGER64:      apiValues.LINT,
	readWriteModel.CANOpenDataType_REAL32:         apiValues.REAL,
	readWriteModel.CANOpenDataType_REAL64:         apiValues.LREAL,
	readWriteModel.CANOpenDataType_RECORD:         apiValues.RAW_BYTE_ARRAY,
	readWriteModel.CANOpenDataType_OCTET_STRING:   apiValues.STRING,
	readWriteModel.CANOpenDataType_VISIBLE_STRING: apiValues.STRING,
	readWriteModel.CANOpenDataType_UNICODE_STRING: apiValues.STRING,
}

func valueTypeOf(dataType readWriteModel.CANOpenDataType) apiValues.PlcValueType {
	if valueType, ok := valueTypes[dataType]; ok {
		return valueType
	}
	return apiValues.NULL
}

func (t SDOTag) GetValueType() apiValues.PlcValueType { return valueTypeOf(t.DataType) }
func (t PDOTag) GetValueType() apiValues.PlcValueType { return valueTypeOf(t.DataType) }

// GetValueType of an NMT tag is the command specifier (NMTStateRequest), which is what a write sends and an event
// carries as "state".
func (t NMTTag) GetValueType() apiValues.PlcValueType { return apiValues.USINT }

// GetValueType of a heartbeat tag is its event's {state, node}; plc4j reports NULL.
func (t HeartbeatTag) GetValueType() apiValues.PlcValueType { return apiValues.Struct }

func arrayInfo(elements uint8) []apiModel.ArrayInfo {
	if elements > 1 {
		return []apiModel.ArrayInfo{&spiModel.DefaultArrayInfo{LowerBound: 0, UpperBound: uint32(elements) - 1}}
	}
	return []apiModel.ArrayInfo{}
}

func (t SDOTag) GetArrayInfo() []apiModel.ArrayInfo       { return arrayInfo(t.Elements) }
func (t PDOTag) GetArrayInfo() []apiModel.ArrayInfo       { return arrayInfo(t.Elements) }
func (t NMTTag) GetArrayInfo() []apiModel.ArrayInfo       { return []apiModel.ArrayInfo{} }
func (t HeartbeatTag) GetArrayInfo() []apiModel.ArrayInfo { return []apiModel.ArrayInfo{} }

// GetPlcSubscriptionType: PDOs, NMT commands and heartbeats come when a node sends them, so a tag is an event
// subscription; it is the only kind the driver takes, as plc4j's.
func (t PDOTag) GetPlcSubscriptionType() apiModel.PlcSubscriptionType {
	return apiModel.SubscriptionEvent
}
func (t NMTTag) GetPlcSubscriptionType() apiModel.PlcSubscriptionType {
	return apiModel.SubscriptionEvent
}
func (t HeartbeatTag) GetPlcSubscriptionType() apiModel.PlcSubscriptionType {
	return apiModel.SubscriptionEvent
}

func (t PDOTag) GetDuration() time.Duration       { return 0 }
func (t NMTTag) GetDuration() time.Duration       { return 0 }
func (t HeartbeatTag) GetDuration() time.Duration { return 0 }

func (t SDOTag) String() string       { return "CANOpenSDOTag(" + t.GetAddressString() + ")" }
func (t PDOTag) String() string       { return "CANOpenPDOTag(" + t.GetAddressString() + ")" }
func (t NMTTag) String() string       { return "CANOpenNMTTag(" + t.GetAddressString() + ")" }
func (t HeartbeatTag) String() string { return "CANOpenHeartbeatTag(" + t.GetAddressString() + ")" }
