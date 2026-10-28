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

// Reader answers a read from the last frame this connection received with each tag's identifier, laid out as for a
// subscription. plc4j's Generic CAN has no read ("the driver only writes and subscribes"): CAN has no request for a
// value, only frames a node sends. Go offers the last of them, because a caller that polls - a gateway's scan - has no
// other way to get one; a read for an identifier no frame has come with yet waits for one until the request timeout,
// and then answers REQUEST_TIMEOUT.
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
		ctx, cancel := context.WithTimeout(ctx, m.connection.configuration.requestTimeout)
		defer cancel()
		codes := map[string]apiModel.PlcResponseCode{}
		values := map[string]apiValues.PlcValue{}
		order, names, tags := group(readRequest.GetTagNames(), func(tagName string) (Tag, bool) {
			tag, ok := readRequest.GetTag(tagName).(Tag)
			return tag, ok
		}, func(tagName string) { codes[tagName] = apiModel.PlcResponseCode_INVALID_ADDRESS })
		for _, nodeId := range order {
			data, ok := m.connection.latestFrame(ctx, nodeId)
			if !ok {
				for _, tagName := range names[nodeId] {
					codes[tagName] = apiModel.PlcResponseCode_REQUEST_TIMEOUT
				}
				continue
			}
			nodeCodes, nodeValues := decodeTags(ctx, data, tags[nodeId])
			for i, tagName := range names[nodeId] {
				codes[tagName] = nodeCodes[i]
				if nodeValues[i] != nil {
					values[tagName] = nodeValues[i]
				}
			}
		}
		utils.DeliverResult(m.log, result, spiModel.NewDefaultPlcReadRequestResult(readRequest, spiModel.NewDefaultPlcReadResponse(readRequest, codes, values), nil))
	})
	return result
}
