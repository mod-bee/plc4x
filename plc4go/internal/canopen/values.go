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
		return decodeBits(data, 0, max(int(elements), 1))
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
		bits, err := boolsOf(value, elements)
		if err != nil {
			return nil, err
		}
		data := make([]byte, (len(bits)+7)/8)
		setBits(data, 0, bits)
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

// decodeBits reads count BOOLEANs from bit start on (bit 0 the lowest of byte 0), as CiA 301 packs them.
func decodeBits(data []byte, start, count int) (apiValues.PlcValue, error) {
	if len(data)*8 < start+count {
		return nil, errors.Errorf("%d x BOOLEAN from bit %d needs %d bytes, %d came", count, start, (start+count+7)/8, len(data))
	}
	values := make([]apiValues.PlcValue, count)
	for i := range values {
		bit := start + i
		values[i] = spiValues.NewPlcBOOL(data[bit/8]>>(bit%8)&1 == 1)
	}
	if count == 1 {
		return values[0], nil
	}
	return spiValues.NewPlcList(values), nil
}

// boolsOf is the BOOLEANs a value gives a tag of elements of them.
func boolsOf(value apiValues.PlcValue, elements uint8) ([]bool, error) {
	if value == nil {
		return nil, errors.New("no value")
	}
	values := []apiValues.PlcValue{value}
	if elements > 1 {
		if !value.IsList() || len(value.GetList()) != int(elements) {
			return nil, errors.Errorf("%d x BOOLEAN takes a list of %d values", elements, elements)
		}
		values = value.GetList()
	}
	bits := make([]bool, len(values))
	for i, element := range values {
		if !element.IsBool() {
			return nil, errors.Errorf("a BOOLEAN is not %s", element.GetPlcValueType())
		}
		bits[i] = element.GetBool()
	}
	return bits, nil
}

// setBits sets (or clears) bits from bit start on, leaving every other bit of data as it is.
func setBits(data []byte, start int, bits []bool) {
	for i, on := range bits {
		bit := start + i
		if on {
			data[bit/8] |= 1 << (bit % 8)
		} else {
			data[bit/8] &^= 1 << (bit % 8)
		}
	}
}

// pdoValue reads a PDO tag's value from a PDO's data: from the tag's byte (and bit) on. A PDO shorter than that is an
// error, not a made-up value.
func pdoValue(ctx context.Context, data []byte, tag PDOTag) (apiValues.PlcValue, error) {
	if int(tag.ByteOffset) >= len(data) && !(len(data) == 0 && tag.ByteOffset == 0) {
		return nil, errors.Errorf("the PDO carries %d bytes; %s starts at byte %d", len(data), tag, tag.ByteOffset)
	}
	if tag.DataType == readWriteModel.CANOpenDataType_BOOLEAN {
		return decodeBits(data, int(tag.ByteOffset)*8+int(tag.BitOffset), max(int(tag.Elements), 1))
	}
	return decodeValue(ctx, data[tag.ByteOffset:], tag.DataType, tag.Elements)
}

// pdoPayload is the data of the PDO a write to tag sends.
//
// A tag that names no position is the whole PDO, as in plc4j: its value is the data. A positioned tag is part of a PDO
// whose other values it must not change, so its value is laid over base - the data this connection last sent or
// received with the PDO's COB-ID - with zeros where base has none: a fixed-size value replaces its own bytes (a
// BOOLEAN its own bits), and the PDO is as long as base or as the value's end, whichever is longer; a RECORD or string
// runs to the PDO's end, so it replaces everything from its byte on. A value that would run past 8 bytes is refused.
func pdoPayload(ctx context.Context, base []byte, tag PDOTag, value apiValues.PlcValue) ([]byte, error) {
	if !tag.Positioned {
		data, err := encodeValue(ctx, value, tag.DataType, tag.Elements)
		if err == nil && len(data) > pdoBytes {
			err = errors.Errorf("%d bytes do not fit a PDO", len(data))
		}
		return data, err
	}
	start := int(tag.ByteOffset)
	out := slices.Clone(base)
	if tag.DataType == readWriteModel.CANOpenDataType_BOOLEAN {
		bits, err := boolsOf(value, tag.Elements)
		if err != nil {
			return nil, err
		}
		first := start*8 + int(tag.BitOffset)
		if end := (first + len(bits) + 7) / 8; len(out) < end {
			out = append(out, make([]byte, end-len(out))...)
		}
		setBits(out, first, bits)
		return out, nil
	}
	data, err := encodeValue(ctx, value, tag.DataType, tag.Elements)
	if err != nil {
		return nil, err
	}
	if len(out) < start {
		out = append(out, make([]byte, start-len(out))...)
	}
	if _, fixed := pdoBits(tag); fixed {
		if end := start + len(data); len(out) < end {
			out = append(out, make([]byte, end-len(out))...)
		}
		copy(out[start:], data)
	} else {
		out = append(out[:start], data...)
	}
	if len(out) > pdoBytes {
		return nil, errors.Errorf("%d bytes from byte %d run past the PDO's %d", len(data), start, pdoBytes)
	}
	return out, nil
}
