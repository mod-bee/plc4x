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
	"fmt"
	"time"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/genericcan/readwrite/model"
	spiModel "github.com/apache/plc4x/plc4go/spi/model"
)

// Tag is plc4j's GenericCANTag: a frame identifier (11-bit, or 29-bit when it is above 0x7FF), a data type, and an
// optional array size.
type Tag struct {
	NodeId    uint32
	DataType  readWriteModel.GenericCANDataType
	ArraySize uint16
}

var _ apiModel.PlcSubscriptionTag = Tag{}

// elements is how many values of DataType the tag holds. RAW is one value whatever its size: its size is its length in
// bytes (see decodeTags).
func (t Tag) elements() int {
	if t.DataType == readWriteModel.GenericCANDataType_RAW || t.ArraySize <= 1 {
		return 1
	}
	return int(t.ArraySize)
}

// GetAddressString spells the tag as the tag handler parses it. plc4j appends [arraySize] for any size but 1, so it
// spells a tag without one as "200:BYTE[0]", which its own pattern then reads as an array of none; Go appends it for an
// array only.
func (t Tag) GetAddressString() string {
	address := fmt.Sprintf("%d:%s", t.NodeId, t.DataType.String())
	if t.ArraySize > 1 {
		address += fmt.Sprintf("[%d]", t.ArraySize)
	}
	return address
}

// valueTypes is the PLC4X type each data type's value is: the case the mspec's dataIo yields for it.
//
// plc4j's tag reports the enum's plcValueName instead, which disagrees with what its own dataIo yields (WORD where the
// value is a UINT, RAW_BYTE_ARRAY where it is a UDINT) and names BOOLEAN's as "BIT", which is no PlcValueType at all, so
// plc4j cannot build a BOOLEAN value to write. Go reports the dataIo's.
var valueTypes = map[readWriteModel.GenericCANDataType]apiValues.PlcValueType{
	readWriteModel.GenericCANDataType_BYTE:       apiValues.BYTE,
	readWriteModel.GenericCANDataType_BOOLEAN:    apiValues.BOOL,
	readWriteModel.GenericCANDataType_UNSIGNED8:  apiValues.USINT,
	readWriteModel.GenericCANDataType_UNSIGNED16: apiValues.UINT,
	readWriteModel.GenericCANDataType_UNSIGNED24: apiValues.UDINT,
	readWriteModel.GenericCANDataType_UNSIGNED32: apiValues.UDINT,
	readWriteModel.GenericCANDataType_UNSIGNED40: apiValues.ULINT,
	readWriteModel.GenericCANDataType_UNSIGNED48: apiValues.ULINT,
	readWriteModel.GenericCANDataType_UNSIGNED56: apiValues.ULINT,
	readWriteModel.GenericCANDataType_UNSIGNED64: apiValues.ULINT,
	readWriteModel.GenericCANDataType_INTEGER8:   apiValues.SINT,
	readWriteModel.GenericCANDataType_INTEGER16:  apiValues.INT,
	readWriteModel.GenericCANDataType_INTEGER24:  apiValues.DINT,
	readWriteModel.GenericCANDataType_INTEGER32:  apiValues.DINT,
	readWriteModel.GenericCANDataType_INTEGER40:  apiValues.LINT,
	readWriteModel.GenericCANDataType_INTEGER48:  apiValues.LINT,
	readWriteModel.GenericCANDataType_INTEGER56:  apiValues.LINT,
	readWriteModel.GenericCANDataType_INTEGER64:  apiValues.LINT,
	readWriteModel.GenericCANDataType_REAL32:     apiValues.REAL,
	readWriteModel.GenericCANDataType_REAL64:     apiValues.LREAL,
	readWriteModel.GenericCANDataType_RAW:        apiValues.RAW_BYTE_ARRAY,
}

// GetValueType is the PLC4X type of the tag's value (see valueTypes).
func (t Tag) GetValueType() apiValues.PlcValueType {
	if valueType, ok := valueTypes[t.DataType]; ok {
		return valueType
	}
	return apiValues.NULL
}

func (t Tag) GetArrayInfo() []apiModel.ArrayInfo {
	if t.elements() > 1 {
		return []apiModel.ArrayInfo{&spiModel.DefaultArrayInfo{LowerBound: 0, UpperBound: uint32(t.ArraySize) - 1}}
	}
	return []apiModel.ArrayInfo{}
}

// GetPlcSubscriptionType: CAN frames are pushed, so a tag is an event subscription; it is the only kind the driver
// takes, as plc4j's.
func (t Tag) GetPlcSubscriptionType() apiModel.PlcSubscriptionType {
	return apiModel.SubscriptionEvent
}

func (t Tag) GetDuration() time.Duration {
	return 0
}

func (t Tag) String() string {
	return "GenericCANTag(" + t.GetAddressString() + ")"
}
