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
	"fmt"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/apache/plc4x/plc4go/spi/errors"
	spiOptions "github.com/apache/plc4x/plc4go/spi/options"
)

// Configuration is what an slmp connection string can say about the connection. Ported from plc4j's
// SlmpConfiguration.
type Configuration struct {
	// monitoringTimer is the SLMP monitoring timer written into every 3E request frame. Zero means
	// "wait infinitely" as far as the device is concerned; the client-side bound is requestTimeout.
	// It is an unsigned 16-bit field on the wire, which is why it is typed as one here - plc4j
	// carries it as an int and has to range-check it by hand in SlmpConnection.validateConfiguration.
	monitoringTimer uint16
	// requestTimeout bounds every single read and write.
	requestTimeout time.Duration
	// route is the 3E frame's access route: which station the connected one passes the request on to.
	route Route
}

// Route is a 3E frame's access route (SH-080008 chapter 6). The connected station itself is the
// default: network 0, station 0xFF, module I/O 0x03FF, multidrop station 0.
type Route struct {
	// Network is the network number of the station addressed (0 = the connected station's own).
	Network uint8
	// Station is the station number on that network (0xFF = the connected station).
	Station uint8
	// ModuleIo is the request destination module I/O number: 0x03FF is the CPU of the connected
	// station; 0x03E0-0x03E3 are CPUs No. 1-4 of a multiple-CPU system; others reach a multidrop
	// link through the module at that I/O number.
	ModuleIo uint16
	// MultidropStation is the multidrop station number when ModuleIo names a multidrop link (else 0).
	MultidropStation uint8
}

const (
	// defaultMonitoringTimer is plc4j's @IntDefaultValue(0x0000) for the monitoring-timer option.
	defaultMonitoringTimer = uint16(0x0000)
	// defaultRequestTimeout is plc4j's @IntDefaultValue(5_000) for the request-timeout-ms option.
	defaultRequestTimeout = 5 * time.Second
)

// ownStation is the access route of the connected station itself.
var ownStation = Route{Network: 0x00, Station: 0xFF, ModuleIo: 0x03FF, MultidropStation: 0x00}

// DefaultConfiguration is a connection without any options set.
func DefaultConfiguration() Configuration {
	return Configuration{
		monitoringTimer: defaultMonitoringTimer,
		requestTimeout:  defaultRequestTimeout,
		route:           ownStation,
	}
}

// ParseFromOptions reads the connection options out of a parsed connection string. The timeout is
// spelled in milliseconds, the way plc4j's @IntDefaultValue(5_000) does.
//
// plc4j defers both range checks to SlmpConnection.validateConfiguration, because its configuration
// object is populated by field injection and its setters are bypassed. Here the parse is the only
// way in, so both checks live at the parse.
func ParseFromOptions(localLog zerolog.Logger, connectionOptions map[string][]string) (Configuration, error) {
	// Every option this driver reads goes through the reader, so the ones nothing read can be
	// reported rather than silently discarded. Deferred, so no return path can skip it.
	reader := spiOptions.NewOptionReader(localLog, connectionOptions)
	defer reader.ReportUnknown("slmp")

	configuration := DefaultConfiguration()

	if monitoringTimerString := reader.Get("monitoring-timer"); monitoringTimerString != "" {
		parsedInt, err := strconv.ParseUint(monitoringTimerString, 10, 16)
		if err != nil {
			return Configuration{}, errors.Wrapf(err, "Error parsing monitoring-timer %s (it is an unsigned 16-bit field in the 3E frame, so it has to be in [0, 65535])", monitoringTimerString)
		}
		configuration.monitoringTimer = uint16(parsedInt)
	}

	if requestTimeoutString := reader.Get("request-timeout-ms"); requestTimeoutString != "" {
		parsedInt, err := strconv.ParseUint(requestTimeoutString, 10, 32)
		if err != nil {
			return Configuration{}, errors.Wrapf(err, "Error parsing request-timeout-ms %s", requestTimeoutString)
		}
		if parsedInt == 0 {
			// plc4j rejects this at connect time with the same reasoning: a non-positive timeout
			// would time out every request immediately.
			return Configuration{}, errors.New("request-timeout-ms must be greater than zero")
		}
		configuration.requestTimeout = time.Duration(parsedInt) * time.Millisecond
	}

	for _, field := range []struct {
		name string
		bits int
		set  func(uint64)
	}{
		{"network-number", 8, func(v uint64) { configuration.route.Network = uint8(v) }},
		{"station-number", 8, func(v uint64) { configuration.route.Station = uint8(v) }},
		{"module-io-number", 16, func(v uint64) { configuration.route.ModuleIo = uint16(v) }},
		{"multidrop-station-number", 8, func(v uint64) { configuration.route.MultidropStation = uint8(v) }},
	} {
		if text := reader.Get(field.name); text != "" {
			// Base 0: decimal, or hex with 0x - the manual spells the module I/O numbers in hex.
			parsed, err := strconv.ParseUint(text, 0, field.bits)
			if err != nil {
				return Configuration{}, errors.Wrapf(err, "Error parsing %s %s (an unsigned %d-bit field of the 3E access route)", field.name, text, field.bits)
			}
			field.set(parsed)
		}
	}

	return configuration, nil
}

func (c Configuration) String() string {
	return fmt.Sprintf("slmp.Configuration{monitoringTimer: 0x%04X, requestTimeout: %s, route: network %d station 0x%02X module I/O 0x%04X multidrop %d}",
		c.monitoringTimer, c.requestTimeout, c.route.Network, c.route.Station, c.route.ModuleIo, c.route.MultidropStation)
}
