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
	"encoding/binary"
	"slices"
	"unicode/utf16"

	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/utils"
	spiValues "github.com/apache/plc4x/plc4go/spi/values"
)

// decodeValue reads the value of a tag of dataType from data (an SDO upload's bytes, or a PDO's), little-endian, as
// plc4j does with the mspec's dataIo.
//
// Where Go differs from plc4j, and why:
//   - RECORD is the bytes as they came (a RAW_BYTE_ARRAY), which is what plc4j's driver test expects; the dataIo
//     wraps them in a list;
//   - UNICODE_STRING is UTF-16LE, as CiA 301 (7.1.6.4) defines it; the dataIo reads it as UTF-8;
//   - BOOLEAN is bit 0 of its byte, and BOOLEAN[n] bits 0 to n-1, as CiA 301 packs them (7.1.4.2, and a PDO's mapped
//     bits, 7.4.8.2); the dataIo reads the byte's top bit;
//   - [n] reads n values of dataType one after the other (plc4j parses it and reads one).
func decodeValue(ctx context.Context, data []byte, dataType readWriteModel.CANOpenDataType, elements uint8) (apiValues.PlcValue, error) {
	switch dataType {
	case readWriteModel.CANOpenDataType_RECORD:
		return spiValues.NewPlcRawByteArray(slices.Clone(data)), nil
	case readWriteModel.CANOpenDataType_UNICODE_STRING:
		if len(data)%2 != 0 {
			return nil, errors.Errorf("a UNICODE_STRING is 16-bit code units, %d bytes are not", len(data))
		}
		units := make([]uint16, len(data)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(data[2*i:])
		}
		if end := slices.Index(units, 0); end >= 0 {
			units = units[:end]
		}
		return spiValues.NewPlcSTRING(string(utf16.Decode(units))), nil
	case readWriteModel.CANOpenDataType_BOOLEAN:
		count := max(int(elements), 1)
		if len(data)*8 < count {
			return nil, errors.Errorf("%d x BOOLEAN needs %d bits, %d bytes came", count, count, len(data))
		}
		values := make([]apiValues.PlcValue, count)
		for i := range values {
			values[i] = spiValues.NewPlcBOOL(data[i/8]>>(i%8)&1 == 1)
		}
		if count == 1 {
			return values[0], nil
		}
		return spiValues.NewPlcList(values), nil
	}
	readBuffer := utils.NewReadBufferByteBased(data, utils.WithByteOrderForReadBufferByteBased(binary.LittleEndian))
	if dataType == readWriteModel.CANOpenDataType_OCTET_STRING || dataType == readWriteModel.CANOpenDataType_VISIBLE_STRING {
		return readWriteModel.DataItemParseWithBuffer(ctx, readBuffer, dataType, int32(len(data)*8))
	}
	count := max(int(elements), 1)
	if need := (int(dataType.NumBits())*count + 7) / 8; len(data) < need {
		return nil, errors.Errorf("%d x %s needs %d bytes, %d came", count, dataType, need, len(data))
	}
	values := make([]apiValues.PlcValue, count)
	for i := range values {
		value, err := readWriteModel.DataItemParseWithBuffer(ctx, readBuffer, dataType, 0)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	if count == 1 {
		return values[0], nil
	}
	return spiValues.NewPlcList(values), nil
}

// encodeValue lays out value as dataType's bytes, little-endian: what an SDO download or a PDO carries. The exceptions
// to the dataIo are decodeValue's.
func encodeValue(ctx context.Context, value apiValues.PlcValue, dataType readWriteModel.CANOpenDataType, elements uint8) ([]byte, error) {
	if value == nil {
		return nil, errors.New("no value")
	}
	switch dataType {
	case readWriteModel.CANOpenDataType_RECORD:
		if !value.IsRaw() {
			return nil, errors.Errorf("a RECORD is bytes, not %s", value.GetPlcValueType())
		}
		return slices.Clone(value.GetRaw()), nil
	case readWriteModel.CANOpenDataType_UNICODE_STRING:
		units := utf16.Encode([]rune(value.GetString()))
		data := make([]byte, 2*len(units))
		for i, unit := range units {
			binary.LittleEndian.PutUint16(data[2*i:], unit)
		}
		return data, nil
	case readWriteModel.CANOpenDataType_BOOLEAN:
		values := []apiValues.PlcValue{value}
		if elements > 1 {
			if !value.IsList() || len(value.GetList()) != int(elements) {
				return nil, errors.Errorf("%d x BOOLEAN takes a list of %d values", elements, elements)
			}
			values = value.GetList()
		}
		data := make([]byte, (len(values)+7)/8)
		for i, element := range values {
			if !element.IsBool() {
				return nil, errors.Errorf("a BOOLEAN is not %s", element.GetPlcValueType())
			}
			if element.GetBool() {
				data[i/8] |= 1 << (i % 8)
			}
		}
		return data, nil
	}
	writeBuffer := utils.NewWriteBufferByteBased(utils.WithByteOrderForByteBasedBuffer(binary.LittleEndian))
	if dataType == readWriteModel.CANOpenDataType_OCTET_STRING || dataType == readWriteModel.CANOpenDataType_VISIBLE_STRING {
		if err := readWriteModel.DataItemSerializeWithWriteBuffer(ctx, writeBuffer, value, dataType, int32(len(value.GetString())*8)); err != nil {
			return nil, err
		}
		return writeBuffer.GetBytes(), nil
	}
	values := []apiValues.PlcValue{value}
	if elements > 1 {
		if !value.IsList() || len(value.GetList()) != int(elements) {
			return nil, errors.Errorf("%d x %s takes a list of %d values", elements, dataType, elements)
		}
		values = value.GetList()
	}
	for _, element := range values {
		if err := readWriteModel.DataItemSerializeWithWriteBuffer(ctx, writeBuffer, element, dataType, 0); err != nil {
			return nil, err
		}
	}
	return writeBuffer.GetBytes(), nil
}
