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

package utils

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The KNX 2-byte float (DPT 9) as the KNX Standard 3.7.2 lays it out, each value with its two
// bytes on the bus: 21.5 as a KNX device (knxd's bus, captured off the wire) sends it, the
// smallest steps, and both ends of the range.
var knxFloat16Cases = []struct {
	value float32
	bytes []byte
}{
	{0, []byte{0x00, 0x00}},
	{0.01, []byte{0x00, 0x01}},
	{-0.01, []byte{0x87, 0xFF}},
	{21.5, []byte{0x0C, 0x33}},
	{-2.25, []byte{0x87, 0x1F}},
	{20.48, []byte{0x0C, 0x00}},
	{-20.48, []byte{0x80, 0x00}},
	{670760.96, []byte{0x7F, 0xFF}},
	{-671088.64, []byte{0xF8, 0x00}},
}

func TestReadBuffer_ReadFloat32_knxFloat16(t *testing.T) {
	for _, c := range knxFloat16Cases {
		rb := NewReadBufferByteBased(append(c.bytes, 0xAA))
		got, err := rb.ReadFloat32("value", 16)
		require.NoError(t, err)
		assert.InDelta(t, c.value, got, 0.001, "% x", c.bytes)
		next, err := rb.ReadUint8("next", 8)
		require.NoError(t, err)
		assert.Equal(t, uint8(0xAA), next, "a 16 bit float is two bytes")
	}
}

func TestWriteBuffer_WriteFloat32_knxFloat16(t *testing.T) {
	for _, c := range knxFloat16Cases {
		wb := NewWriteBufferByteBased()
		require.NoError(t, wb.WriteFloat32("value", 16, c.value))
		assert.Equal(t, c.bytes, wb.GetBytes(), "%v", c.value)
	}
	for _, value := range []float32{680000, -680000, float32(math.NaN()), float32(math.Inf(1))} {
		assert.Error(t, NewWriteBufferByteBased().WriteFloat32("value", 16, value), "%v", value)
	}
}
