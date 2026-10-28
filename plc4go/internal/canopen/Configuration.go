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

// Package canopen is the CANopen driver, as plc4j's canopen (drivers/canopen): CiA 301 over the CAN transports.
//
//   - read: an SDO upload (expedited or segmented) from a node's object dictionary, as plc4j does; and, beyond plc4j,
//     the last PDO or heartbeat a node sent;
//   - write: an SDO download (expedited or segmented) or a PDO, as plc4j does; and an NMT command (the node control
//     plc4j's NMT driver test describes);
//   - subscribe: PDOs, NMT commands and heartbeats, as plc4j does.
//
// Connection strings: canopen:can-socketcan://can0 (Linux) and canopen:can-virtualcan://test (in process).
package canopen

import (
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/apache/plc4x/plc4go/spi/errors"
	spiOptions "github.com/apache/plc4x/plc4go/spi/options"
)

// maxNodeId is the largest CANopen node-ID (CiA 301 7.3.2.1: 1 to 127).
const maxNodeId = 127

// heartbeatPeriod is how often the heartbeat producer reports OPERATIONAL, as plc4j's (10 s).
const heartbeatPeriod = 10 * time.Second

// Configuration is plc4j's CANOpenConfiguration.
type Configuration struct {
	// nodeId is this connection's own node-ID: the heartbeat producer sends as it.
	nodeId uint8
	// heartbeat makes the connection a heartbeat producer: BOOTED_UP once, then OPERATIONAL every heartbeatPeriod.
	heartbeat bool
	// requestTimeout is how long an SDO transfer waits for each answer of the node.
	requestTimeout time.Duration
}

// DefaultConfiguration is plc4j's defaults: node-id 0, heartbeat false, request-timeout-ms 1000.
func DefaultConfiguration() Configuration {
	return Configuration{requestTimeout: time.Second}
}

// ParseFromOptions reads node-id, heartbeat and request-timeout-ms, as plc4j names them.
//
// plc4j takes any int for node-id; Go takes a CANopen node-ID (0 to 127), and refuses heartbeat without one, as the
// heartbeat of node 0 would go out on 0x700, which no node-ID maps to.
func ParseFromOptions(localLog zerolog.Logger, connectionOptions map[string][]string) (Configuration, error) {
	reader := spiOptions.NewOptionReader(localLog, connectionOptions)
	defer reader.ReportUnknown("canopen")

	configuration := DefaultConfiguration()
	if text := reader.Get("node-id"); text != "" {
		parsed, err := strconv.ParseUint(text, 10, 8)
		if err != nil || parsed > maxNodeId {
			return Configuration{}, errors.Errorf("node-id %s is not a CANopen node-ID (0 to %d)", text, maxNodeId)
		}
		configuration.nodeId = uint8(parsed)
	}
	if text := reader.Get("heartbeat"); text != "" {
		parsed, err := strconv.ParseBool(text)
		if err != nil {
			return Configuration{}, errors.Wrapf(err, "Error parsing heartbeat %s", text)
		}
		configuration.heartbeat = parsed
	}
	if configuration.heartbeat && configuration.nodeId == 0 {
		return Configuration{}, errors.New("heartbeat needs a node-id (1 to 127) to send it as")
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
