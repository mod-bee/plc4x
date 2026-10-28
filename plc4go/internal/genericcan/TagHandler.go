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
	"regexp"
	"strconv"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/genericcan/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/errors"
)

// addressPattern is plc4j's GenericCANTag.ADDRESS_PATTERN, matched against the whole address as Matcher.matches() does.
var addressPattern = regexp.MustCompile(`^(?P<nodeId>\d+):(?P<dataType>\w+)(?:\[(?P<arraySize>\d+)])?$`)

// maxIdentifier is the largest CAN identifier there is (29 bits). plc4j takes any int and lets the bus truncate it; Go
// refuses what no frame can carry.
const maxIdentifier = 0x1FFFFFFF

type TagHandler struct{}

func NewTagHandler() TagHandler {
	return TagHandler{}
}

func (TagHandler) ParseTag(tagAddress string) (apiModel.PlcTag, error) {
	match := addressPattern.FindStringSubmatch(tagAddress)
	if match == nil {
		return nil, errors.Errorf("unable to parse %q: a Generic CAN tag is nodeId:type[arraySize], e.g. 200:UNSIGNED16", tagAddress)
	}
	nodeId, err := strconv.ParseUint(match[addressPattern.SubexpIndex("nodeId")], 10, 32)
	if err != nil || nodeId > maxIdentifier {
		return nil, errors.Errorf("%q: the node id is a CAN identifier, 0 to %d", tagAddress, maxIdentifier)
	}
	typeName := match[addressPattern.SubexpIndex("dataType")]
	dataType, ok := readWriteModel.GenericCANDataTypeByName(typeName)
	if !ok {
		return nil, errors.Errorf("Could not create tag with data type %s", typeName)
	}
	var arraySize uint64
	if text := match[addressPattern.SubexpIndex("arraySize")]; text != "" {
		if arraySize, err = strconv.ParseUint(text, 10, 16); err != nil {
			return nil, errors.Errorf("%q: the array size is 0 to 65535", tagAddress)
		}
	}
	return Tag{NodeId: uint32(nodeId), DataType: dataType, ArraySize: uint16(arraySize)}, nil
}

func (TagHandler) ParseQuery(_ string) (apiModel.PlcQuery, error) {
	return nil, errors.New("This driver doesn't support browsing")
}
