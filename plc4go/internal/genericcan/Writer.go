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
	"context"
	"runtime/debug"
	"sync"

	"github.com/rs/zerolog"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	"github.com/apache/plc4x/plc4go/spi/errors"
	spiModel "github.com/apache/plc4x/plc4go/spi/model"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/utils"
)

// Writer sends one frame per identifier, carrying the request's values for it in request order, as plc4j's
// GenericCANConnection.onWrite does: a frame one of whose values fails to encode is not sent (each of its tags answers
// INVALID_DATA or OK as it encoded), one the transport fails to send answers REMOTE_ERROR, and a tag that is not a
// Generic CAN tag answers UNSUPPORTED.
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
		ctx, cancel := context.WithTimeout(ctx, m.connection.configuration.requestTimeout)
		defer cancel()
		codes := map[string]apiModel.PlcResponseCode{}
		order, names, tags := group(writeRequest.GetTagNames(), func(tagName string) (Tag, bool) {
			tag, ok := writeRequest.GetTag(tagName).(Tag)
			return tag, ok
		}, func(tagName string) { codes[tagName] = apiModel.PlcResponseCode_UNSUPPORTED })
		for _, nodeId := range order {
			values := make([]apiValues.PlcValue, len(names[nodeId]))
			for i, tagName := range names[nodeId] {
				values[i] = writeRequest.GetValue(tagName)
			}
			payload, nodeCodes, ok := encodeTags(ctx, tags[nodeId], values)
			for i, tagName := range names[nodeId] {
				codes[tagName] = nodeCodes[i]
			}
			if !ok {
				m.log.Info().Uint32("nodeId", nodeId).Msg("Discarding frame: a tag failed to serialize")
				continue
			}
			if err := m.connection.sendFrame(ctx, nodeId, payload); err != nil {
				m.log.Error().Err(err).Uint32("nodeId", nodeId).Msg("Failed to send CAN frame")
				for _, tagName := range names[nodeId] {
					codes[tagName] = apiModel.PlcResponseCode_REMOTE_ERROR
				}
			}
		}
		utils.DeliverResult(m.log, result, spiModel.NewDefaultPlcWriteRequestResult(writeRequest, spiModel.NewDefaultPlcWriteResponse(writeRequest, codes), nil))
	})
	return result
}
