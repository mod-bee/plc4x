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

package ads

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apache/plc4x/plc4go/internal/ads/model"
	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	spiModel "github.com/apache/plc4x/plc4go/spi/model"
	"github.com/apache/plc4x/plc4go/spi/utils"
	spiValues "github.com/apache/plc4x/plc4go/spi/values"
)

// TwinCAT uploads an array type with its element type as the secondary name ("ARRAY [0..9] OF DINT"
// → "DINT"). Reading the element type out of the secondary name's " OF " found none on such a table,
// fell back to the array type itself, and every symbolic array read failed against a real PLC.
func TestArrayDecode_theElementTypeIsTheSecondaryName(t *testing.T) {
	c := connectionWithArraySymbol()
	array := c.driverContext.dataTypeTable["ARRAY [0..9] OF DINT"]
	data := make([]byte, 40)
	for i := range 10 {
		binary.LittleEndian.PutUint32(data[i*4:], uint32(i*11))
	}

	value, err := c.parsePlcValue(array, array.GetArrayInfo(), utils.NewReadBufferByteBased(data, utils.WithByteOrderForReadBufferByteBased(binary.LittleEndian)))

	require.NoError(t, err)
	require.True(t, value.IsList())
	require.Len(t, value.GetList(), 10)
	assert.Equal(t, int64(33), value.GetList()[3].GetInt64())
}

// Writing one element (MAIN.arr[3]) is one value of the element's type at that element's place. The
// value handler and the writer used the declared shape, so they demanded a list for the whole array
// and the write was refused ("couldn't cast value to []any").
func TestArrayElementWrite_oneElementIsOneValue(t *testing.T) {
	c := connectionWithArraySymbol()
	vh := NewValueHandlerWithDriverContext(c.driverContext, NewTagHandlerWithDriverContext(c.driverContext))
	element := []apiModel.ArrayInfo{&spiModel.DefaultArrayInfo{LowerBound: 3, UpperBound: 3}}

	value, err := vh.NewPlcValue(model.SymbolicPlcTag{SymbolicAddress: "MAIN.arr", PlcTag: model.PlcTag{ArrayInfo: element}}, int32(-7))
	require.NoError(t, err)
	assert.False(t, value.IsList(), "one element is one value")

	tag, err := resolve(t, "MAIN.arr", element)
	require.NoError(t, err)
	wb := utils.NewWriteBufferByteBased(utils.WithByteOrderForByteBasedBuffer(binary.LittleEndian))
	require.NoError(t, c.serializePlcValue(tag.DataType, tag.EncodeArrayInfo(), spiValues.NewPlcDINT(-7), wb))
	assert.Equal(t, uint32(112), tag.IndexOffset, "the fourth element")
	assert.Len(t, wb.GetBytes(), 4, "one DINT, not the array")
	assert.Equal(t, int32(-7), int32(binary.LittleEndian.Uint32(wb.GetBytes())))
}

// A written range is laid out as the range, not as the whole array.
func TestArrayElementWrite_aRangeIsWrittenAsTheRange(t *testing.T) {
	c := connectionWithArraySymbol()
	tag, err := resolve(t, "MAIN.arr", []apiModel.ArrayInfo{&spiModel.DefaultArrayInfo{LowerBound: 1, UpperBound: 2, Range: true}})
	require.NoError(t, err)
	wb := utils.NewWriteBufferByteBased(utils.WithByteOrderForByteBasedBuffer(binary.LittleEndian))
	list := spiValues.NewPlcList([]apiValues.PlcValue{spiValues.NewPlcDINT(5), spiValues.NewPlcDINT(6)})

	require.NoError(t, c.serializePlcValue(tag.DataType, tag.EncodeArrayInfo(), list, wb))
	assert.Len(t, wb.GetBytes(), 8)
}
