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

package slmp

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/slmp/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/errors"
	spiModel "github.com/apache/plc4x/plc4go/spi/model"
	"github.com/apache/plc4x/plc4go/spi/utils"
)

// deviceAddressing says how a supported device's address is spelled and what it holds.
type deviceAddressing struct {
	deviceCode readWriteModel.SlmpDeviceCode
	// base is the radix the device number is written in: MELSEC addresses X, Y, B, W, SB, SW, DX
	// and DY in hex and every other device in decimal (SH-080008 section 8.1).
	base int
	// bit is a bit device (one point is one bit): read in word units, sixteen points to a word, and
	// written in bit units. A word device's point is a 16-bit word.
	bit bool
}

// supportedDevices are the devices a tag address may name, by their MELSEC names.
var supportedDevices = map[string]deviceAddressing{
	"X":   {deviceCode: readWriteModel.SlmpDeviceCode_X, base: 16, bit: true},
	"Y":   {deviceCode: readWriteModel.SlmpDeviceCode_Y, base: 16, bit: true},
	"M":   {deviceCode: readWriteModel.SlmpDeviceCode_M, base: 10, bit: true},
	"L":   {deviceCode: readWriteModel.SlmpDeviceCode_L, base: 10, bit: true},
	"F":   {deviceCode: readWriteModel.SlmpDeviceCode_F, base: 10, bit: true},
	"V":   {deviceCode: readWriteModel.SlmpDeviceCode_V, base: 10, bit: true},
	"B":   {deviceCode: readWriteModel.SlmpDeviceCode_B, base: 16, bit: true},
	"SM":  {deviceCode: readWriteModel.SlmpDeviceCode_SM, base: 10, bit: true},
	"SB":  {deviceCode: readWriteModel.SlmpDeviceCode_SB, base: 16, bit: true},
	"DX":  {deviceCode: readWriteModel.SlmpDeviceCode_DX, base: 16, bit: true},
	"DY":  {deviceCode: readWriteModel.SlmpDeviceCode_DY, base: 16, bit: true},
	"TS":  {deviceCode: readWriteModel.SlmpDeviceCode_TS, base: 10, bit: true},
	"TC":  {deviceCode: readWriteModel.SlmpDeviceCode_TC, base: 10, bit: true},
	"STS": {deviceCode: readWriteModel.SlmpDeviceCode_STS, base: 10, bit: true},
	"STC": {deviceCode: readWriteModel.SlmpDeviceCode_STC, base: 10, bit: true},
	"CS":  {deviceCode: readWriteModel.SlmpDeviceCode_CS, base: 10, bit: true},
	"CC":  {deviceCode: readWriteModel.SlmpDeviceCode_CC, base: 10, bit: true},
	"D":   {deviceCode: readWriteModel.SlmpDeviceCode_D, base: 10},
	"W":   {deviceCode: readWriteModel.SlmpDeviceCode_W, base: 16},
	"R":   {deviceCode: readWriteModel.SlmpDeviceCode_R, base: 10},
	"ZR":  {deviceCode: readWriteModel.SlmpDeviceCode_ZR, base: 10},
	"SD":  {deviceCode: readWriteModel.SlmpDeviceCode_SD, base: 10},
	"SW":  {deviceCode: readWriteModel.SlmpDeviceCode_SW, base: 16},
	"TN":  {deviceCode: readWriteModel.SlmpDeviceCode_TN, base: 10},
	"STN": {deviceCode: readWriteModel.SlmpDeviceCode_STN, base: 10},
	"CN":  {deviceCode: readWriteModel.SlmpDeviceCode_CN, base: 10},
	"Z":   {deviceCode: readWriteModel.SlmpDeviceCode_Z, base: 10},
}

// deviceNamesLongestFirst is every device name, the longest first. A device is found by its name
// as a prefix of the address, and a hex address can start with a letter, so the shortest prefix
// that parses is not the device meant: SB1A is SB 0x1A, not a device "S" at 0xB1A; SD100 is SD
// 100, CC10 is CC 10. Trying the longest name first finds the device meant.
var deviceNamesLongestFirst = func() []string {
	names := make([]string, 0, len(supportedDevices))
	for name := range supportedDevices {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j]
	})
	return names
}()

// addressingOf is how a device code's address is spelled, for rendering a tag back.
func addressingOf(deviceCode readWriteModel.SlmpDeviceCode) (deviceAddressing, bool) {
	for _, addressing := range supportedDevices {
		if addressing.deviceCode == deviceCode {
			return addressing, true
		}
	}
	return deviceAddressing{}, false
}

// splitDevice finds the device an address names and the digits of its number, the longest device
// name first (see deviceNamesLongestFirst). The digits may carry a 0x prefix.
func splitDevice(deviceAndNumber string) (string, deviceAddressing, string, bool) {
	upper := strings.ToUpper(deviceAndNumber)
	for _, name := range deviceNamesLongestFirst {
		if !strings.HasPrefix(upper, name) {
			continue
		}
		digits := deviceAndNumber[len(name):]
		if numberPattern.MatchString(digits) {
			return name, supportedDevices[name], digits, true
		}
	}
	return "", deviceAddressing{}, "", false
}

// numberPattern is a device number's digits: hex digits, optionally after 0x (the radix is the
// device's, checked once the device is known).
var numberPattern = regexp.MustCompile(`^(0[xX])?[0-9A-Fa-f]+$`)

// TagHandler parses slmp tag addresses of the form
//
//	<device><address>[selection][:<datatype>]
//
// e.g. D350, R200[0..3]:REAL, W1A, W0x1A, M100, X1F[0..7] or SB1A. Ported from plc4j's
// SlmpTag.ADDRESS_PATTERN.
//
// The device is found by name, the longest name first (see deviceNamesLongestFirst), rather than
// by a letters-then-digits split: plc4j matches the device as a greedy [A-Za-z]+, so on a W address
// whose first two digits are letters (WAB) the match swallows them into the device token.
type TagHandler struct {
	addressPattern *regexp.Regexp
}

func NewTagHandler() TagHandler {
	return TagHandler{
		// The selection sits between the address and the type, as it does in plc4j's
		// SlmpTag.ADDRESS_PATTERN: "D100[0..3]:INT".
		addressPattern: regexp.MustCompile(`^(?P<deviceAndNumber>[A-Za-z][0-9A-Za-z]*)` + spiModel.ArrayGroupPattern + `(:(?P<datatype>[A-Za-z_]+))?$`),
	}
}

func (m TagHandler) ParseTag(tagAddress string) (apiModel.PlcTag, error) {
	match := utils.GetSubgroupMatches(m.addressPattern, tagAddress)
	if match == nil {
		// "D100:INT[4]" - the count after the type - no longer parses, so name the form to
		// write rather than reporting only that nothing matched.
		return nil, spiModel.InvalidAddressError(tagAddress,
			"{device}{address}[selection]:{TYPE} - for example D100[0..3]:INT")
	}

	deviceToken, device, digits, ok := splitDevice(match["deviceAndNumber"])
	if !ok {
		return nil, errors.Errorf("no supported device with a device number in '%s' (devices: %s): %s",
			match["deviceAndNumber"], strings.Join(deviceNamesLongestFirst, ", "), tagAddress)
	}

	// The 0x prefix only says "read the following digits as hex", so on a device that is addressed
	// in decimal it can only be a mistake.
	hexPrefixed := len(digits) > 1 && (digits[:2] == "0x" || digits[:2] == "0X")
	if hexPrefixed && device.base != 16 {
		return nil, errors.Errorf("0x prefix is only valid for hex devices (X, Y, B, W, SB, SW, DX, DY), not %s: %s", deviceToken, tagAddress)
	}
	if hexPrefixed {
		digits = digits[2:]
	}

	// Parsed as 64 bit so that an address beyond the 24-bit field is reported as out of range
	// rather than as an unparsable one.
	deviceNumber, err := strconv.ParseUint(digits, device.base, 64)
	if err != nil {
		radixName := "decimal"
		if device.base == 16 {
			radixName = "hex"
		}
		return nil, errors.Wrapf(err, "Invalid %s device number in: %s", radixName, tagAddress)
	}
	if deviceNumber > maxDeviceNumber {
		return nil, errors.Errorf("device number %d exceeds the 24-bit SLMP device-address range [0..%d]: %s",
			deviceNumber, maxDeviceNumber, tagAddress)
	}

	// A bit device's point is a bit, a word device's a word: that is what a tag without a type reads.
	dataType := DataTypeWORD
	if device.bit {
		dataType = DataTypeBOOL
	}
	if datatypeToken := match["datatype"]; datatypeToken != "" {
		if dataType, ok = DataTypeByName(strings.ToUpper(datatypeToken)); !ok {
			return nil, errors.Errorf("Unsupported SLMP data type '%s' (supported: %s)",
				datatypeToken, strings.Join(SupportedDataTypeNames, ", "))
		}
	}
	if dataType == DataTypeBOOL && !device.bit {
		return nil, errors.Errorf("%s is a word device: read it as WORD (or another word type) and take the bit from the value: %s", deviceToken, tagAddress)
	}

	// The selection's offset moves the device number and its size is how many devices are read,
	// so "D100[4..7]" is the same read as "D104[0..3]". An SLMP Batch Read covers one contiguous
	// run of devices, so nothing deeper than a single dimension fits.
	quantity := uint64(1)
	explicitRange := false
	if expression := match["array"]; expression != "" {
		dimensions, err := spiModel.ParseArrayExpression(expression, tagAddress, spiModel.SingleDimension)
		if err != nil {
			return nil, err
		}
		dimension := dimensions[0]
		// The offset counts elements; a device number counts 16-bit words. They coincide only for
		// a one-word type, so D100[4]:DINT would otherwise land on D104 instead of D108. This is
		// the same scale the point count below applies, so offset and length cannot disagree.
		// A bit device read as words holds sixteen points (device numbers) in every word.
		pointsPerElement := uint64(dataType.WordsPerElement())
		if device.bit && dataType != DataTypeBOOL {
			pointsPerElement *= 16
		}
		deviceNumber += uint64(dimension.GetLowerBound()-dimension.GetBase()) * pointsPerElement
		if deviceNumber > maxDeviceNumber {
			return nil, errors.Errorf("device number %d exceeds the 24-bit SLMP device-address range [0..%d]: %s",
				deviceNumber, maxDeviceNumber, tagAddress)
		}
		quantity = uint64(dimension.GetSize())
		// A range is an array even when it spans one element, which the count cannot say.
		explicitRange = dimension.IsRange()
	}

	if dataType == DataTypeBOOL {
		// Bits are written in bit units, so their count is what has to stay inside that ceiling;
		// read in word units, 7168 bits are at most 449 words, well inside the word ceiling.
		if quantity > maxBitPoints {
			return nil, errors.Errorf("requested %d bits exceeds the single-frame bit-unit Batch Write ceiling of %d: %s",
				quantity, maxBitPoints, tagAddress)
		}
		return NewTagWithShape(device.deviceCode, uint32(deviceNumber), dataType, uint16(quantity), explicitRange), nil
	}
	if device.bit && deviceNumber%16 != 0 {
		// In word units a bit device is read and written sixteen points to a word, from a device
		// number that is a multiple of 16 (SH-080008 section 8.2).
		return nil, errors.Errorf("a bit device read as words starts at a multiple of 16 (%s%s, not %s%s): %s",
			deviceToken, formatDeviceNumber(device.base, deviceNumber&^15), deviceToken, formatDeviceNumber(device.base, deviceNumber), tagAddress)
	}

	// The point count is what the frame asks for, so it - not the quantity - is what has to stay
	// inside the single-frame ceiling. There is no request optimizer to split a bigger transfer.
	numberOfPoints := quantity * uint64(dataType.WordsPerElement())
	if numberOfPoints > maxPoints {
		return nil, errors.Errorf("requested %d words exceeds the single-frame Batch Read/Write ceiling of %d (no optimizer to split): %s",
			numberOfPoints, maxPoints, tagAddress)
	}

	return NewTagWithShape(device.deviceCode, uint32(deviceNumber), dataType, uint16(quantity), explicitRange), nil
}

// ParseQuery is not supported: neither this driver nor plc4j's browses an SLMP device. plc4j's
// SlmpTagHandler.parseQuery throws UnsupportedOperationException for the same reason.
func (m TagHandler) ParseQuery(_ string) (apiModel.PlcQuery, error) {
	return nil, errors.New("SLMP does not support queries")
}

// formatDeviceNumber spells a device number in the device's radix, the way an address writes it.
func formatDeviceNumber(base int, deviceNumber uint64) string {
	return strings.ToUpper(strconv.FormatUint(deviceNumber, base))
}
