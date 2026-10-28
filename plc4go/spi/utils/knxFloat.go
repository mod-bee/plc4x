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

	"github.com/apache/plc4x/plc4go/spi/errors"
)

// A 16 bit float is the KNX 2-byte float (KNX Standard 3.7.2, DPT 9 - the "KNXFloat" encoding of
// the knxnetip mspec): MEEEEMMM MMMMMMMM, value = 0.01 * M * 2^E, with E a 4 bit exponent (0..15)
// and M a 12 bit two's complement mantissa whose sign is the top bit.

// decodeKnxFloat16 is the value of a KNX 2-byte float.
func decodeKnxFloat16(raw uint16) float32 {
	mantissa := int32(raw & 0x07FF)
	if raw&0x8000 != 0 {
		mantissa -= 0x0800
	}
	exponent := (raw >> 11) & 0x0F
	return float32(0.01 * float64(mantissa) * float64(uint32(1)<<exponent))
}

// encodeKnxFloat16 is a value as a KNX 2-byte float: the smallest exponent which fits the
// mantissa, so it keeps the most precision. -671088.64 to 670760.96 can be encoded.
func encodeKnxFloat16(value float32) (uint16, error) {
	if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
		return 0, errors.Errorf("%v can't be encoded as a KNX 2-byte float", value)
	}
	hundredths := float64(value) * 100
	for exponent := uint16(0); exponent <= 15; exponent++ {
		mantissa := math.Round(hundredths / float64(uint32(1)<<exponent))
		if mantissa >= -2048 && mantissa <= 2047 {
			twelveBits := uint16(int16(mantissa)) & 0x0FFF
			return (twelveBits&0x0800)<<4 | exponent<<11 | twelveBits&0x07FF, nil
		}
	}
	return 0, errors.Errorf("%v is out of the range of a KNX 2-byte float (-671088.64 to 670760.96)", value)
}
