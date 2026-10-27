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
package bacnetip

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	spiValues "github.com/apache/plc4x/plc4go/spi/values"
)

// An enumerated value goes out in as many octets as its tag header announces - at least one: inactive (0) is 91 00,
// not a header announcing an octet the payload leaves out, which a device rejects (and which wrote every binary
// object's present value except "active").
func TestPlcValueToApplicationTag_EnumeratedOctets(t *testing.T) {
	for _, testCase := range []struct {
		name string
		tag  func() (any, error)
		want []byte
	}{
		{"inactive", func() (any, error) { return plcValueToApplicationTag(spiValues.NewPlcBOOL(false), hintEnumerated) }, []byte{0x91, 0x00}},
		{"active", func() (any, error) { return plcValueToApplicationTag(spiValues.NewPlcBOOL(true), hintEnumerated) }, []byte{0x91, 0x01}},
		{"0", func() (any, error) { return plcValueToApplicationTag(spiValues.NewPlcUDINT(0), hintEnumerated) }, []byte{0x91, 0x00}},
		{"200", func() (any, error) { return plcValueToApplicationTag(spiValues.NewPlcUDINT(200), hintEnumerated) }, []byte{0x91, 0xC8}},
		{"70000", func() (any, error) { return plcValueToApplicationTag(spiValues.NewPlcUDINT(70000), hintEnumerated) }, []byte{0x93, 0x01, 0x11, 0x70}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tag, err := testCase.tag()
			require.NoError(t, err)
			serialized, err := tag.(interface{ Serialize() ([]byte, error) }).Serialize()
			require.NoError(t, err)
			assert.Equal(t, testCase.want, serialized)
		})
	}
}
