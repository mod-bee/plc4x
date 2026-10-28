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

// Package genericcan is the Generic CAN driver, as plc4j's genericcan (drivers/can): raw CAN frames with no
// application protocol of its own. A tag is a frame identifier and a data type, nodeId:type[arraySize]; the tags of one
// request (or one subscription) that name the same identifier are laid out one after the other, in request order and
// little-endian, in that identifier's frame.
//
//   - subscribe: every frame with the identifier, as it arrives, as plc4j does;
//   - write: one frame per identifier, carrying the request's values for it, as plc4j does;
//   - read: the last frame with the identifier this connection received (plc4j has no read; see Reader).
//
// Connection strings: genericcan:can-socketcan://can0 (Linux) and genericcan:can-virtualcan://test (in process).
package genericcan

import (
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/apache/plc4x/plc4go/spi/errors"
	spiOptions "github.com/apache/plc4x/plc4go/spi/options"
)

// Configuration is plc4j's GenericCANConfiguration.
type Configuration struct {
	// nodeId is plc4j's node-id, which its driver accepts and never uses either: a tag names its frame itself.
	nodeId         uint32
	requestTimeout time.Duration
}

// DefaultConfiguration is plc4j's defaults: request-timeout-ms 1000.
func DefaultConfiguration() Configuration {
	return Configuration{requestTimeout: time.Second}
}

func ParseFromOptions(localLog zerolog.Logger, connectionOptions map[string][]string) (Configuration, error) {
	reader := spiOptions.NewOptionReader(localLog, connectionOptions)
	defer reader.ReportUnknown("genericcan")

	configuration := DefaultConfiguration()
	if text := reader.Get("node-id"); text != "" {
		parsed, err := strconv.ParseUint(text, 10, 32)
		if err != nil {
			return Configuration{}, errors.Wrapf(err, "Error parsing node-id %s", text)
		}
		configuration.nodeId = uint32(parsed)
	}
	if text := reader.Get("request-timeout-ms"); text != "" {
		parsed, err := strconv.ParseUint(text, 10, 32)
		if err != nil {
			return Configuration{}, errors.Wrapf(err, "Error parsing request-timeout-ms %s", text)
		}
		if parsed == 0 {
			return Configuration{}, errors.New("request-timeout-ms must be greater than zero")
		}
		configuration.requestTimeout = time.Duration(parsed) * time.Millisecond
	}
	return configuration, nil
}
