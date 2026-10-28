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
	"regexp"
	"strconv"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/errors"
)

// The patterns are plc4j's (CANOpenTag, CANOpenSDOTag, CANOpenPDOTag, CANOpenNMTTag, CANOpenHeartbeatTag), each matched
// against the whole address as Matcher.matches() does, and tried in plc4j's order.
const (
	nodePattern    = `(?P<nodeId>\d+)`
	addressPattern = `(?:0[xX](?P<indexHex>[0-9a-fA-F]+)|(?P<index>\d+))/(?:0[xX](?P<subIndexHex>[0-9a-fA-F]+)|(?P<subIndex>\d+)):(?P<canDataType>\w+)(?:\[(?P<numberOfElements>\d)])?`
)

var (
	sdoPattern       = regexp.MustCompile(`^SDO:` + nodePattern + `(?:/(?P<answerNodeId>\d+))?:` + addressPattern + `$`)
	pdoPattern       = regexp.MustCompile(`^(?P<pdo>(?:RECEIVE|TRANSMIT)_PDO_[1-4]):` + nodePattern + `(?::(?P<byteOffset>\d+)(?:\.(?P<bitOffset>\d+))?)?:(?P<canDataType>\w+)(?:\[(?P<numberOfElements>\d)])?$`)
	nmtPattern       = regexp.MustCompile(`^(?:NMT|NMT:` + nodePattern + `)$`)
	heartbeatPattern = regexp.MustCompile(`^(?:HEARTBEAT|HEARTBEAT:` + nodePattern + `)$`)
)

type TagHandler struct{}

func NewTagHandler() TagHandler {
	return TagHandler{}
}

// ParseTag reads the four kinds of address plc4j's driver takes:
//
//	SDO:<node>[/<answerNode>]:<index>/<subIndex>:<type>[<n>]   index and subIndex decimal or 0x hex
//	<RECEIVE|TRANSMIT>_PDO_<1-4>:<node>[:<byte>[.<bit>]]:<type>[<n>]   byte and bit beyond plc4j (see PDOTag)
//	NMT[:<node>]
//	HEARTBEAT[:<node>]
//
// Where Go is stricter than plc4j: a node is a CANopen node-ID (1 to 127, or 0 for "any" in NMT and HEARTBEAT); an
// index takes all 16 bits (plc4j parses it into a signed short, so 0x8000 and up fail); and a type is one the dataIo
// can read (plc4j accepts TIME_OF_DAY and TIME_DIFFERENCE, then cannot read them).
func (TagHandler) ParseTag(tagAddress string) (apiModel.PlcTag, error) {
	if match := sdoPattern.FindStringSubmatch(tagAddress); match != nil {
		return parseSdoTag(tagAddress, match)
	}
	if match := pdoPattern.FindStringSubmatch(tagAddress); match != nil {
		return parsePdoTag(tagAddress, match)
	}
	if match := nmtPattern.FindStringSubmatch(tagAddress); match != nil {
		nodeId, err := optionalNode(tagAddress, match[nmtPattern.SubexpIndex("nodeId")])
		return NMTTag{NodeId: nodeId}, err
	}
	if match := heartbeatPattern.FindStringSubmatch(tagAddress); match != nil {
		nodeId, err := optionalNode(tagAddress, match[heartbeatPattern.SubexpIndex("nodeId")])
		return HeartbeatTag{NodeId: nodeId}, err
	}
	return nil, errors.Errorf("unable to parse address %q: a CANopen tag is SDO:node:index/subIndex:type, "+
		"RECEIVE_PDO_1:node:type (or TRANSMIT_, 1 to 4), NMT[:node] or HEARTBEAT[:node]", tagAddress)
}

func parseSdoTag(tagAddress string, match []string) (apiModel.PlcTag, error) {
	group := func(name string) string { return match[sdoPattern.SubexpIndex(name)] }
	nodeId, err := node(tagAddress, group("nodeId"))
	if err != nil {
		return nil, err
	}
	answerNodeId := nodeId
	if text := group("answerNodeId"); text != "" {
		if answerNodeId, err = node(tagAddress, text); err != nil {
			return nil, err
		}
	}
	index, err := number(group("indexHex"), group("index"), 16)
	if err != nil {
		return nil, errors.Errorf("%q: the index is 0 to 0xFFFF", tagAddress)
	}
	subIndex, err := number(group("subIndexHex"), group("subIndex"), 8)
	if err != nil {
		return nil, errors.Errorf("%q: the sub-index is 0 to 0xFF", tagAddress)
	}
	dataType, err := dataTypeOf(tagAddress, group("canDataType"))
	if err != nil {
		return nil, err
	}
	return SDOTag{
		NodeId:       nodeId,
		AnswerNodeId: answerNodeId,
		Index:        uint16(index),
		SubIndex:     uint8(subIndex),
		DataType:     dataType,
		Elements:     elementsOf(group("numberOfElements")),
	}, nil
}

func parsePdoTag(tagAddress string, match []string) (apiModel.PlcTag, error) {
	group := func(name string) string { return match[pdoPattern.SubexpIndex(name)] }
	service, ok := readWriteModel.CANOpenServiceByName(group("pdo"))
	if !ok {
		return nil, errors.Errorf("%q: unknown PDO %s", tagAddress, group("pdo"))
	}
	nodeId, err := node(tagAddress, group("nodeId"))
	if err != nil {
		return nil, err
	}
	dataType, err := dataTypeOf(tagAddress, group("canDataType"))
	if err != nil {
		return nil, err
	}
	tag := PDOTag{NodeId: nodeId, Service: service, DataType: dataType, Elements: elementsOf(group("numberOfElements"))}
	if text := group("byteOffset"); text != "" {
		byteOffset, err := strconv.ParseUint(text, 10, 8)
		if err != nil || byteOffset >= pdoBytes {
			return nil, errors.Errorf("%q: a PDO carries 8 bytes, 0 to 7", tagAddress)
		}
		tag.Positioned, tag.ByteOffset = true, uint8(byteOffset)
	}
	if text := group("bitOffset"); text != "" {
		bitOffset, err := strconv.ParseUint(text, 10, 8)
		if err != nil || bitOffset > 7 {
			return nil, errors.Errorf("%q: a byte's bits are 0 to 7", tagAddress)
		}
		if dataType != readWriteModel.CANOpenDataType_BOOLEAN {
			return nil, errors.Errorf("%q: only a BOOLEAN names a bit", tagAddress)
		}
		tag.BitOffset = uint8(bitOffset)
	}
	if bits, fixed := pdoBits(tag); fixed && int(tag.ByteOffset)*8+int(tag.BitOffset)+bits > pdoBytes*8 {
		return nil, errors.Errorf("%q: from byte %d the value runs past the PDO's 8 bytes", tagAddress, tag.ByteOffset)
	}
	return tag, nil
}

// pdoBytes is what one PDO carries (a CAN frame's data).
const pdoBytes = 8

// pdoBits is how many bits a PDO tag's value takes, when its type fixes it (not RECORD or a string: those run to the
// PDO's end). A BOOLEAN takes one bit, as a PDO maps it (CiA 301 7.4.8.2); elsewhere whole bytes.
func pdoBits(tag PDOTag) (int, bool) {
	count := max(int(tag.Elements), 1)
	switch tag.DataType {
	case readWriteModel.CANOpenDataType_RECORD, readWriteModel.CANOpenDataType_OCTET_STRING,
		readWriteModel.CANOpenDataType_VISIBLE_STRING, readWriteModel.CANOpenDataType_UNICODE_STRING:
		return 0, false
	case readWriteModel.CANOpenDataType_BOOLEAN:
		return count, true
	}
	return int(tag.DataType.NumBits()) * count, true
}

func node(tagAddress, text string) (uint8, error) {
	parsed, err := strconv.ParseUint(text, 10, 8)
	if err != nil || parsed < 1 || parsed > maxNodeId {
		return 0, errors.Errorf("%q: %s is not a CANopen node-ID (1 to %d)", tagAddress, text, maxNodeId)
	}
	return uint8(parsed), nil
}

func optionalNode(tagAddress, text string) (uint8, error) {
	if text == "" || text == "0" {
		return 0, nil
	}
	return node(tagAddress, text)
}

func number(hex, decimal string, bits int) (uint64, error) {
	if hex != "" {
		return strconv.ParseUint(hex, 16, bits)
	}
	return strconv.ParseUint(decimal, 10, bits)
}

func elementsOf(text string) uint8 {
	if text == "" {
		return 0
	}
	parsed, _ := strconv.ParseUint(text, 10, 8) // the pattern takes one digit
	return uint8(parsed)
}

func dataTypeOf(tagAddress, name string) (readWriteModel.CANOpenDataType, error) {
	dataType, ok := readWriteModel.CANOpenDataTypeByName(name)
	if !ok {
		return 0, errors.Errorf("%q: unknown CANopen data type %s", tagAddress, name)
	}
	if _, readable := valueTypes[dataType]; !readable {
		return 0, errors.Errorf("%q: the CANopen data type %s is not supported", tagAddress, name)
	}
	return dataType, nil
}

func (TagHandler) ParseQuery(_ string) (apiModel.PlcQuery, error) {
	return nil, errors.New("This driver doesn't support browsing")
}
