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
	"context"
	"runtime/debug"
	"sync"

	"github.com/rs/zerolog"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/errors"
	spiModel "github.com/apache/plc4x/plc4go/spi/model"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/utils"
)

// Reader reads each tag of a request in turn:
//   - an SDO tag by an SDO upload, as plc4j's onRead (which takes one tag per request; Go takes any number, one
//     transfer after the other);
//   - a PDO tag, and a heartbeat tag naming a node, from the last PDO or heartbeat this connection received from it,
//     waiting for one until the request timeout when none has come yet. plc4j reads SDO tags only; a gateway that
//     polls has no other way to get a PDO's value or a node's state;
//   - an NMT tag (a command, not a value) answers UNSUPPORTED, and a heartbeat tag naming no node INVALID_ADDRESS.
type Reader struct {
	connection *Connection

	wg  sync.WaitGroup
	log zerolog.Logger
}

func NewReader(connection *Connection, _options ...options.WithOption) *Reader {
	return &Reader{connection: connection, log: options.ExtractCustomLoggerOrDefaultToGlobal(_options...)}
}

func (m *Reader) Read(ctx context.Context, readRequest apiModel.PlcReadRequest) <-chan apiModel.PlcReadRequestResult {
	result := make(chan apiModel.PlcReadRequestResult, 1)
	m.wg.Go(func() {
		defer func() {
			if err := recover(); err != nil {
				utils.DeliverResult(m.log, result, spiModel.NewDefaultPlcReadRequestResult(readRequest, nil, errors.Errorf("panic-ed %v. Stack: %s", err, debug.Stack())))
			}
		}()
		codes := map[string]apiModel.PlcResponseCode{}
		values := map[string]apiValues.PlcValue{}
		for _, tagName := range readRequest.GetTagNames() {
			code, value := m.read(ctx, readRequest.GetTag(tagName))
			codes[tagName] = code
			if value != nil {
				values[tagName] = value
			}
		}
		utils.DeliverResult(m.log, result, spiModel.NewDefaultPlcReadRequestResult(readRequest, spiModel.NewDefaultPlcReadResponse(readRequest, codes, values), nil))
	})
	return result
}

func (m *Reader) read(ctx context.Context, tag apiModel.PlcTag) (apiModel.PlcResponseCode, apiValues.PlcValue) {
	switch tag := tag.(type) {
	case SDOTag:
		data, err := m.connection.sdoUpload(ctx, tag)
		if err != nil {
			m.log.Debug().Err(err).Stringer("tag", tag).Msg("SDO upload failed")
			return responseCodeOf(err), nil
		}
		return m.decoded(ctx, tag, data, tag.DataType, tag.Elements)
	case PDOTag:
		data, ok := m.latest(ctx, cobIdOf(tag.Service, tag.NodeId))
		if !ok {
			return apiModel.PlcResponseCode_REQUEST_TIMEOUT, nil
		}
		return m.decoded(ctx, tag, data, tag.DataType, tag.Elements)
	case HeartbeatTag:
		if tag.NodeId == 0 {
			return apiModel.PlcResponseCode_INVALID_ADDRESS, nil
		}
		data, ok := m.latest(ctx, cobIdOf(readWriteModel.CANOpenService_HEARTBEAT, tag.NodeId))
		if !ok {
			return apiModel.PlcResponseCode_REQUEST_TIMEOUT, nil
		}
		return apiModel.PlcResponseCode_OK, stateStruct(data[0], tag.NodeId)
	case NMTTag:
		return apiModel.PlcResponseCode_UNSUPPORTED, nil
	}
	return apiModel.PlcResponseCode_INVALID_ADDRESS, nil
}

func (m *Reader) latest(ctx context.Context, cobId uint16) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(ctx, m.connection.configuration.requestTimeout)
	defer cancel()
	return m.connection.latestData(ctx, cobId)
}

func (m *Reader) decoded(ctx context.Context, tag apiModel.PlcTag, data []byte, dataType readWriteModel.CANOpenDataType, elements uint8) (apiModel.PlcResponseCode, apiValues.PlcValue) {
	value, err := decodeValue(ctx, data, dataType, elements)
	if err != nil {
		m.log.Debug().Err(err).Stringer("tag", tag).Msg("could not read the value")
		return apiModel.PlcResponseCode_INVALID_DATA, nil
	}
	return apiModel.PlcResponseCode_OK, value
}
