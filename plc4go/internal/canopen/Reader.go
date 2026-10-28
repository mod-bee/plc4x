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
	"github.com/apache/plc4x/plc4go/spi/errors"
	spiModel "github.com/apache/plc4x/plc4go/spi/model"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/utils"
)

// Reader reads SDO tags, each by an SDO upload, as plc4j's onRead (which takes one tag per request; Go takes any
// number, one transfer after the other). Any other tag answers UNSUPPORTED: PDOs, NMT commands and heartbeats are
// subscribed to, as in plc4j.
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
	sdoTag, ok := tag.(SDOTag)
	if !ok {
		return apiModel.PlcResponseCode_UNSUPPORTED, nil
	}
	data, err := m.connection.sdoUpload(ctx, sdoTag)
	if err != nil {
		m.log.Debug().Err(err).Stringer("tag", sdoTag).Msg("SDO upload failed")
		return responseCodeOf(err), nil
	}
	value, err := decodeValue(ctx, data, sdoTag.DataType, sdoTag.Elements)
	if err != nil {
		m.log.Debug().Err(err).Stringer("tag", sdoTag).Msg("could not read the value")
		return apiModel.PlcResponseCode_INVALID_DATA, nil
	}
	return apiModel.PlcResponseCode_OK, value
}
