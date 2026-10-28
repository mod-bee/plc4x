/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package model

import (
	"context"
	"math"

	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/utils"
)

// ParseKnxScaled reads a KNX integer with a coefficient (KNX master data) as its scaled value:
// 5.001 is 0..255 for 0..100 %, 5.003 0..255 for 0..360 degrees. The value is
// raw * fullScale / largest, largest being the largest raw value (255 for 8 bits unsigned), so
// the ends of the range come out exact.
func ParseKnxScaled(_ context.Context, readBuffer utils.ReadBuffer, width uint8, signed bool, fullScale float64, largest float64) (float32, error) {
	v, err := readBuffer.ReadUint64("value", width)
	if err != nil {
		return 0, errors.Wrap(err, "error reading a scaled value")
	}
	raw := float64(v)
	if signed && v >= 1<<(width-1) {
		// Two's complement of the field's own width
		raw -= math.Exp2(float64(width))
	}
	return float32(raw * fullScale / largest), nil
}

// SerializeKnxScaled is the inverse of ParseKnxScaled: the nearest raw value, refused when the
// value is outside the datapoint type's range.
func SerializeKnxScaled(_ context.Context, writeBuffer utils.WriteBuffer, value apiValues.PlcValue, width uint8, signed bool, fullScale float64, largest float64) error {
	if value == nil {
		return errors.New("no value to write")
	}
	scaled := value.GetFloat64()
	raw := math.Round(scaled * largest / fullScale)
	lowest, highest := 0.0, math.Exp2(float64(width))-1
	if signed {
		lowest, highest = -math.Exp2(float64(width-1)), math.Exp2(float64(width-1))-1
	}
	if math.IsNaN(raw) || raw < lowest || raw > highest {
		return errors.Errorf("%v is outside the range of this datapoint type (%v to %v)", scaled,
			lowest*fullScale/largest, highest*fullScale/largest)
	}
	if raw < 0 {
		// Two's complement of the field's own width
		raw += math.Exp2(float64(width))
	}
	return writeBuffer.WriteUint64("value", width, uint64(raw))
}
