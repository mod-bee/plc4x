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
	"context"
	"encoding/binary"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/genericcan/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/transports/can"
	"github.com/apache/plc4x/plc4go/spi/utils"
	spiValues "github.com/apache/plc4x/plc4go/spi/values"
)

// decodeTags reads tags one after the other from one frame's payload, little-endian, as plc4j's connection does for a
// subscription. A tag that does not fit what is left of the payload is INVALID_DATA.
//
// Where Go differs from plc4j, and why:
//   - an array tag (200:UNSIGNED8[4]) reads its arraySize elements as a list; plc4j reads one element for it, although
//     its tag reports the array;
//   - RAW reads what is left of the payload (or arraySize bytes, when given); plc4j hands it the whole frame's length
//     whatever came before it, which overruns the payload whenever RAW is not the first tag.
func decodeTags(ctx context.Context, data []byte, tags []Tag) ([]apiModel.PlcResponseCode, []apiValues.PlcValue) {
	readBuffer := utils.NewReadBufferByteBased(data, utils.WithByteOrderForReadBufferByteBased(binary.LittleEndian))
	codes := make([]apiModel.PlcResponseCode, len(tags))
	values := make([]apiValues.PlcValue, len(tags))
	for i, tag := range tags {
		value, err := decodeTag(ctx, readBuffer, data, tag)
		if err != nil {
			codes[i] = apiModel.PlcResponseCode_INVALID_DATA
			continue
		}
		codes[i], values[i] = apiModel.PlcResponseCode_OK, value
	}
	return codes, values
}

func decodeTag(ctx context.Context, readBuffer utils.ReadBufferByteBased, data []byte, tag Tag) (apiValues.PlcValue, error) {
	if tag.DataType == readWriteModel.GenericCANDataType_RAW {
		size := len(data) - int(readBuffer.GetPos())
		if tag.ArraySize > 0 {
			if int(tag.ArraySize) > size {
				return nil, errors.Errorf("%s wants %d bytes, %d are left", tag, tag.ArraySize, size)
			}
			size = int(tag.ArraySize)
		}
		return readWriteModel.DataItemParseWithBuffer(ctx, readBuffer, tag.DataType, int32(size))
	}
	elements := make([]apiValues.PlcValue, tag.elements())
	for i := range elements {
		value, err := readWriteModel.DataItemParseWithBuffer(ctx, readBuffer, tag.DataType, int32(len(data)))
		if err != nil {
			return nil, err
		}
		elements[i] = value
	}
	if len(elements) == 1 {
		return elements[0], nil
	}
	return spiValues.NewPlcList(elements), nil
}

// encodeTags lays out the values of tags one after the other, little-endian, into one frame's payload, as plc4j's
// connection does for a write. A tag whose value does not encode, or does not fit the eight bytes a frame carries, is
// INVALID_DATA, and then no frame is sent for any of them (ok is false), as plc4j discards the frame.
//
// The payload is as long as what was written (BOOLEAN bits rounded up to a byte); plc4j always sends eight bytes,
// zero-padded. A CAN frame's length is part of the message - a receiver checking it would refuse a padded one - so Go
// sends what the tags say.
func encodeTags(ctx context.Context, tags []Tag, values []apiValues.PlcValue) (payload []byte, codes []apiModel.PlcResponseCode, ok bool) {
	writeBuffer := utils.NewWriteBufferByteBased(utils.WithByteOrderForByteBasedBuffer(binary.LittleEndian))
	codes = make([]apiModel.PlcResponseCode, len(tags))
	ok = true
	for i, tag := range tags {
		codes[i] = apiModel.PlcResponseCode_OK
		if err := encodeTag(ctx, writeBuffer, tag, values[i]); err != nil || writeBuffer.GetTotalBytes() > can.MaxDataLength {
			codes[i] = apiModel.PlcResponseCode_INVALID_DATA
			ok = false
		}
	}
	return writeBuffer.GetBytes(), codes, ok
}

func encodeTag(ctx context.Context, writeBuffer utils.WriteBufferByteBased, tag Tag, value apiValues.PlcValue) error {
	if value == nil {
		return errors.Errorf("no value for %s", tag)
	}
	if tag.DataType == readWriteModel.GenericCANDataType_RAW {
		return readWriteModel.DataItemSerializeWithWriteBuffer(ctx, writeBuffer, value, tag.DataType, int32(value.GetLength()))
	}
	elements := []apiValues.PlcValue{value}
	if tag.elements() > 1 {
		if !value.IsList() || len(value.GetList()) != tag.elements() {
			return errors.Errorf("%s takes a list of %d values", tag, tag.elements())
		}
		elements = value.GetList()
	}
	for _, element := range elements {
		if err := readWriteModel.DataItemSerializeWithWriteBuffer(ctx, writeBuffer, element, tag.DataType, 1); err != nil {
			return err
		}
	}
	return nil
}
