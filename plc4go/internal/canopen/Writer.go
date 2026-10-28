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

// Writer writes each tag of a request in turn:
//   - an SDO tag by an SDO download, as plc4j's writeSdo;
//   - a PDO tag by sending the PDO with the value as its data, as plc4j's writePdo;
//   - an NMT tag by sending the NMT command its value names (CiA 301 7.2.8.3.1) to its node, or to every node for
//     "NMT". plc4j's driver does not write NMT tags; its NMT driver test describes this write.
//
// plc4j takes one tag per request; Go takes any number. A value that does not encode answers INVALID_DATA, a frame the
// transport fails to send REMOTE_ERROR, and a heartbeat tag (nothing to write) UNSUPPORTED.
type Writer struct {
	connection *Connection

	wg  sync.WaitGroup
	log zerolog.Logger
}

func NewWriter(connection *Connection, _options ...options.WithOption) *Writer {
	return &Writer{connection: connection, log: options.ExtractCustomLoggerOrDefaultToGlobal(_options...)}
}

func (m *Writer) Write(ctx context.Context, writeRequest apiModel.PlcWriteRequest) <-chan apiModel.PlcWriteRequestResult {
	result := make(chan apiModel.PlcWriteRequestResult, 1)
	m.wg.Go(func() {
		defer func() {
			if err := recover(); err != nil {
				utils.DeliverResult(m.log, result, spiModel.NewDefaultPlcWriteRequestResult(writeRequest, nil, errors.Errorf("panic-ed %v. Stack: %s", err, debug.Stack())))
			}
		}()
		codes := map[string]apiModel.PlcResponseCode{}
		for _, tagName := range writeRequest.GetTagNames() {
			codes[tagName] = m.write(ctx, writeRequest.GetTag(tagName), writeRequest.GetValue(tagName))
		}
		utils.DeliverResult(m.log, result, spiModel.NewDefaultPlcWriteRequestResult(writeRequest, spiModel.NewDefaultPlcWriteResponse(writeRequest, codes), nil))
	})
	return result
}

func (m *Writer) write(ctx context.Context, tag apiModel.PlcTag, value apiValues.PlcValue) apiModel.PlcResponseCode {
	switch tag := tag.(type) {
	case SDOTag:
		data, err := encodeValue(ctx, value, tag.DataType, tag.Elements)
		if err != nil {
			m.log.Debug().Err(err).Stringer("tag", tag).Msg("could not encode the value")
			return apiModel.PlcResponseCode_INVALID_DATA
		}
		if err := m.connection.sdoDownload(ctx, tag, data); err != nil {
			m.log.Debug().Err(err).Stringer("tag", tag).Msg("SDO download failed")
			return responseCodeOf(err)
		}
		return apiModel.PlcResponseCode_OK
	case PDOTag:
		return m.sent(m.connection.sendPdo(ctx, tag, value))
	case NMTTag:
		if value == nil || !value.IsUint8() {
			return apiModel.PlcResponseCode_INVALID_DATA
		}
		request, ok := readWriteModel.NMTStateRequestByValue(value.GetUint8())
		if !ok {
			return apiModel.PlcResponseCode_INVALID_DATA
		}
		return m.sent(m.connection.sendPayload(ctx, readWriteModel.CANOpenService_NMT, 0,
			readWriteModel.NewCANOpenNetworkPayload(request, tag.NodeId)))
	}
	return apiModel.PlcResponseCode_UNSUPPORTED
}

func (m *Writer) sent(err error) apiModel.PlcResponseCode {
	if errors.Is(err, errPdoValue) {
		m.log.Debug().Err(err).Msg("the value does not encode into the PDO")
		return apiModel.PlcResponseCode_INVALID_DATA
	}
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to send CANopen frame")
		return apiModel.PlcResponseCode_REMOTE_ERROR
	}
	return apiModel.PlcResponseCode_OK
}
