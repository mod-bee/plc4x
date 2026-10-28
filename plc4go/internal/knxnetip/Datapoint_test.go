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

package knxnetip

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	driverModel "github.com/apache/plc4x/plc4go/protocols/knxnetip/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/utils"
	spiValues "github.com/apache/plc4x/plc4go/spi/values"
)

// Composite datapoint types as the KNX Standard 3.7.2 lays them out, each with its group-value
// payload (the APDU's six-bit byte first, empty for these, then the data bytes). Each field
// used to be stored under the one key "Struct", so only the last survived, and writing one
// serialized the struct itself as every field.
var knxStructCases = []struct {
	name     string
	dpt      driverModel.KnxDatapointType
	payload  []byte
	fields   map[string]uint8
	booleans map[string]bool
}{
	{
		// 11.001: 28 October 2026 - day, month, year (two digits, 2000 + below 90)
		name: "DPT_Date", dpt: driverModel.KnxDatapointType_DPT_Date,
		payload: []byte{0x00, 0x1C, 0x0A, 0x1A},
		fields:  map[string]uint8{"dayOfMonth": 28, "month": 10, "year": 26},
	},
	{
		// 10.001: Wednesday (3) 22:35:10 - day in the top three bits of the hour's byte
		name: "DPT_TimeOfDay", dpt: driverModel.KnxDatapointType_DPT_TimeOfDay,
		payload: []byte{0x00, 0x76, 0x23, 0x0A},
		fields:  map[string]uint8{"day": 3, "hour": 22, "minutes": 35, "seconds": 10},
	},
	{
		// 232.600: an RGB colour, a byte each
		name: "DPT_Colour_RGB", dpt: driverModel.KnxDatapointType_DPT_Colour_RGB,
		payload: []byte{0x00, 0xFF, 0x80, 0x01},
		fields:  map[string]uint8{"r": 255, "g": 128, "b": 1},
	},
}

func TestKnxDatapoint_structsKeepEveryField(t *testing.T) {
	for _, c := range knxStructCases {
		t.Run(c.name, func(t *testing.T) {
			value, err := driverModel.KnxDatapointParseWithBuffer(context.Background(), utils.NewReadBufferByteBased(c.payload), c.dpt)
			require.NoError(t, err)
			require.True(t, value.IsStruct())
			for name, want := range c.fields {
				field := value.GetValue(name)
				if assert.NotNil(t, field, name) {
					assert.Equal(t, want, field.GetUint8(), name)
				}
			}

			fields := map[string]apiValues.PlcValue{}
			for name, v := range c.fields {
				fields[name] = spiValues.NewPlcUSINT(v)
			}
			serialized, err := driverModel.KnxDatapointSerialize(spiValues.NewPlcStruct(fields), c.dpt)
			require.NoError(t, err)
			assert.Equal(t, c.payload, serialized)
		})
	}
}

// TestKnxDatapoint_structFieldsWithACoefficientAreScaled: a field of a composite type with a
// coefficient in the KNX master data is its scaled value, as a single one is (KNX Standard 3.7.2:
// 232.601 HSV is hue 0..360 degrees and saturation and value 0..100 %, 251.600 RGBW four levels
// 0..100 % and a validity bit each). They used to be the raw byte.
func TestKnxDatapoint_structFieldsWithACoefficientAreScaled(t *testing.T) {
	for _, c := range []struct {
		name    string
		dpt     driverModel.KnxDatapointType
		payload []byte
		scaled  map[string]float64
		bits    map[string]bool
	}{
		{
			name: "DPT_Colour_HSV", dpt: driverModel.KnxDatapointType_DPT_Colour_HSV,
			payload: []byte{0x00, 0x33, 0x33, 0xFF},
			scaled:  map[string]float64{"h": 72, "s": 20, "v": 100},
		},
		{
			name: "DPT_Colour_RGBW", dpt: driverModel.KnxDatapointType_DPT_Colour_RGBW,
			payload: []byte{0x00, 0xFF, 0x33, 0x00, 0x66, 0x00, 0x0B},
			scaled:  map[string]float64{"colourLevelRed": 100, "colourLevelGreen": 20, "colourLevelBlue": 0, "colourLevelWhite": 40},
			bits:    map[string]bool{"mr": true, "mg": false, "mb": true, "mw": true},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			value, err := driverModel.KnxDatapointParseWithBuffer(context.Background(), utils.NewReadBufferByteBased(c.payload), c.dpt)
			require.NoError(t, err)
			fields := map[string]apiValues.PlcValue{}
			for name, want := range c.scaled {
				field := value.GetValue(name)
				require.NotNil(t, field, name)
				assert.InDelta(t, want, field.GetFloat64(), 0.001, name)
				fields[name] = spiValues.NewPlcLREAL(want)
			}
			for name, want := range c.bits {
				assert.Equal(t, want, value.GetValue(name).GetBool(), name)
				fields[name] = spiValues.NewPlcBOOL(want)
			}
			serialized, err := driverModel.KnxDatapointSerialize(spiValues.NewPlcStruct(fields), c.dpt)
			require.NoError(t, err)
			assert.Equal(t, c.payload, serialized)
		})
	}
	_, err := driverModel.KnxDatapointSerialize(spiValues.NewPlcStruct(map[string]apiValues.PlcValue{
		"h": spiValues.NewPlcLREAL(361), "s": spiValues.NewPlcLREAL(0), "v": spiValues.NewPlcLREAL(0),
	}), driverModel.KnxDatapointType_DPT_Colour_HSV)
	assert.Error(t, err, "a hue of 361 degrees is out of range")
}
