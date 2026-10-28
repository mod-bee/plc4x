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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/genericcan/readwrite/model"
	spiValues "github.com/apache/plc4x/plc4go/spi/values"
)

// plc4j's GenericCANTagTest and GenericCANTagHandlerTest, case for case.
func TestTagsParseAsPlc4jParsesThem(t *testing.T) {
	handler := NewTagHandler()

	parsed, err := handler.ParseTag("200:BYTE")
	require.NoError(t, err)
	tag := parsed.(Tag)
	assert.Equal(t, uint32(200), tag.NodeId)
	assert.Equal(t, readWriteModel.GenericCANDataType_BYTE, tag.DataType)
	assert.Zero(t, tag.ArraySize)
	assert.Equal(t, apiValues.BYTE, tag.GetValueType())
	assert.Equal(t, apiValues.BOOL, Tag{DataType: readWriteModel.GenericCANDataType_BOOLEAN}.GetValueType())
	assert.Equal(t, apiValues.UINT, Tag{DataType: readWriteModel.GenericCANDataType_UNSIGNED16}.GetValueType())

	parsed, err = handler.ParseTag("200:BYTE[8]")
	require.NoError(t, err)
	assert.Equal(t, uint16(8), parsed.(Tag).ArraySize)
	assert.Equal(t, apiValues.BYTE, parsed.GetValueType())

	_, err = handler.ParseTag("200:NOT_A_TYPE")
	assert.ErrorContains(t, err, "Could not create tag with data type NOT_A_TYPE")
	_, err = handler.ParseTag("not-a-can-tag")
	assert.Error(t, err)

	assert.Equal(t, "7:BYTE[4]", Tag{NodeId: 7, DataType: readWriteModel.GenericCANDataType_BYTE, ArraySize: 4}.GetAddressString())
	assert.Equal(t, "7:BYTE", Tag{NodeId: 7, DataType: readWriteModel.GenericCANDataType_BYTE, ArraySize: 1}.GetAddressString())
	assert.Len(t, Tag{NodeId: 7, DataType: readWriteModel.GenericCANDataType_BYTE, ArraySize: 4}.GetArrayInfo(), 1)
	assert.Empty(t, Tag{NodeId: 7, DataType: readWriteModel.GenericCANDataType_BYTE, ArraySize: 1}.GetArrayInfo())
	assert.Contains(t, Tag{NodeId: 5, DataType: readWriteModel.GenericCANDataType_BYTE, ArraySize: 3}.String(), "5:BYTE[3]")
}

func TestTagsGoRefuses(t *testing.T) {
	handler := NewTagHandler()
	for _, address := range []string{
		"536870912:BYTE", // one past 29 bits
		"0x123:BYTE",     // plc4j's node id is decimal
		"200:BYTE trailing",
		"200:byte", // data type names are upper case, as plc4j's enum's
		"200:BYTE[70000]",
	} {
		_, err := handler.ParseTag(address)
		assert.Error(t, err, address)
	}
	parsed, err := handler.ParseTag("419361024:RAW")
	require.NoError(t, err)
	assert.Equal(t, uint32(0x18FEF100), parsed.(Tag).NodeId)
	assert.Equal(t, apiValues.RAW_BYTE_ARRAY, parsed.GetValueType())
	assert.Empty(t, parsed.GetArrayInfo(), "RAW's size is its byte count, not an array")
}

func tagsOf(t *testing.T, addresses ...string) []Tag {
	t.Helper()
	tags := make([]Tag, len(addresses))
	for i, address := range addresses {
		parsed, err := NewTagHandler().ParseTag(address)
		require.NoError(t, err)
		tags[i] = parsed.(Tag)
	}
	return tags
}

func TestTagsOfOneFrameAreLaidOutInOrderLittleEndian(t *testing.T) {
	ctx := context.Background()
	data := []byte{0x34, 0x12, 0xFF, 0x00, 0x00, 0x28, 0x42, 0x07}
	codes, values := decodeTags(ctx, data, tagsOf(t, "1:UNSIGNED16", "1:INTEGER8", "1:REAL32", "1:UNSIGNED8"))
	assert.Equal(t, []apiModel.PlcResponseCode{apiModel.PlcResponseCode_OK, apiModel.PlcResponseCode_OK, apiModel.PlcResponseCode_OK, apiModel.PlcResponseCode_OK}, codes)
	assert.Equal(t, uint16(0x1234), values[0].GetUint16())
	assert.Equal(t, int8(-1), values[1].GetInt8())
	assert.Equal(t, float32(42), values[2].GetFloat32())
	assert.Equal(t, uint8(7), values[3].GetUint8())

	// An array reads its elements; RAW reads what is left; a tag past the payload is INVALID_DATA.
	codes, values = decodeTags(ctx, data, tagsOf(t, "1:UNSIGNED8[2]", "1:RAW", "1:UNSIGNED8"))
	assert.Equal(t, apiModel.PlcResponseCode_OK, codes[0])
	require.True(t, values[0].IsList())
	assert.Equal(t, uint8(0x12), values[0].GetList()[1].GetUint8())
	assert.Equal(t, apiModel.PlcResponseCode_OK, codes[1])
	assert.Equal(t, data[2:], values[1].GetList()[0].GetRaw())
	assert.Equal(t, apiModel.PlcResponseCode_INVALID_DATA, codes[2])
	assert.Nil(t, values[2])
}

func TestValuesOfOneFrameAreLaidOutInOrderLittleEndian(t *testing.T) {
	ctx := context.Background()
	payload, codes, ok := encodeTags(ctx, tagsOf(t, "1:UNSIGNED16", "1:INTEGER8"),
		[]apiValues.PlcValue{spiValues.NewPlcUINT(0x1234), spiValues.NewPlcSINT(-2)})
	require.True(t, ok)
	assert.Equal(t, []apiModel.PlcResponseCode{apiModel.PlcResponseCode_OK, apiModel.PlcResponseCode_OK}, codes)
	assert.Equal(t, []byte{0x34, 0x12, 0xFE}, payload, "as long as what was written")

	raw, err := NewValueHandler().NewPlcValue(tagsOf(t, "1:RAW")[0], []any{1, 2.0, uint8(3)})
	require.NoError(t, err)
	payload, _, ok = encodeTags(ctx, tagsOf(t, "1:RAW"), []apiValues.PlcValue{raw})
	require.True(t, ok)
	assert.Equal(t, []byte{1, 2, 3}, payload)
	_, err = NewValueHandler().NewPlcValue(tagsOf(t, "1:RAW")[0], []any{256})
	assert.Error(t, err)

	// Nine bytes do not fit a frame: the one that overflows is INVALID_DATA and nothing is sent.
	_, codes, ok = encodeTags(ctx, tagsOf(t, "1:UNSIGNED64", "1:UNSIGNED8"),
		[]apiValues.PlcValue{spiValues.NewPlcULINT(1), spiValues.NewPlcUSINT(1)})
	assert.False(t, ok)
	assert.Equal(t, []apiModel.PlcResponseCode{apiModel.PlcResponseCode_OK, apiModel.PlcResponseCode_INVALID_DATA}, codes)
}

func TestOptionsAreParsedAsPlc4jNamesThem(t *testing.T) {
	configuration, err := ParseFromOptions(testLogger(t), map[string][]string{"request-timeout-ms": {"250"}, "node-id": {"5"}})
	require.NoError(t, err)
	assert.Equal(t, uint32(5), configuration.nodeId)
	assert.Equal(t, int64(250), configuration.requestTimeout.Milliseconds())
	_, err = ParseFromOptions(testLogger(t), map[string][]string{"request-timeout-ms": {"0"}})
	assert.Error(t, err)
}
