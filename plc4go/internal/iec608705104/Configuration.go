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

package iec608705104

import (
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/apache/plc4x/plc4go/spi/errors"
	spiOptions "github.com/apache/plc4x/plc4go/spi/options"
)

const (
	// defaultRequestTimeout is plc4j's Iec608705014Configuration request-timeout-ms default of four
	// seconds. It bounds the two handshake round trips (test frame and start-data-transfer) which
	// are the only request/response interactions the protocol has - everything after them is
	// unsolicited.
	defaultRequestTimeout = 4000 * time.Millisecond
	// defaultAckThreshold is the IEC 60870-5-104 'w' parameter: after this many unacknowledged
	// I-format frames the controlling station has to send an S-format acknowledgement. The standard
	// recommends 8 (two thirds of the default k of 12) and plc4j's Iec60870Connection hard-codes
	// exactly that.
	defaultAckThreshold = 8
	// maxAckThreshold is the largest window the sequence number can express: the receive sequence
	// number is 15 bits wide.
	maxAckThreshold = 32767
	// defaultT1 is the standard's t1 (IEC 60870-5-104, 5.2.4): how long a sent test frame waits for
	// its confirmation before the connection is taken as broken.
	defaultT1 = 15 * time.Second
	// defaultT3 is the standard's t3: after this long without receiving any frame, the controlling
	// station sends a test frame to prove the link is still there. Without it a quiet but healthy
	// station is indistinguishable from a dead one, and a dead one from a quiet one.
	defaultT3 = 20 * time.Second
	// defaultInterrogationAsduAddress is the global common address: one interrogation reaches every
	// common address the station serves (IEC 60870-5-101, 7.2.4).
	defaultInterrogationAsduAddress = 0xFFFF
	// stationInterrogation is the qualifier of interrogation for a general (station) interrogation.
	stationInterrogation = 20
)

// Configuration is what an IEC 60870-5-104 connection string can say about the connection as a
// whole. plc4j's Iec608705014Configuration knows only request-timeout-ms; the acknowledgement window
// is added here because it is a real protocol parameter ('w') that plc4j buried in a constant, and
// a station which insists on a smaller window otherwise drops the connection.
type Configuration struct {
	// requestTimeout bounds how long each of the two handshake round trips waits for its
	// confirmation.
	requestTimeout time.Duration
	// ackThreshold is how many I-format frames may go unacknowledged before an S-format
	// acknowledgement is sent ('w' in the standard).
	ackThreshold uint16
	// t1 is how long a sent test frame waits for its confirmation before the link is taken as broken.
	t1 time.Duration
	// t3 is how long the link may stay silent before a test frame is sent. Zero sends none.
	t3 time.Duration
	// generalInterrogation sends a station interrogation (C_IC_NA_1, QOI 20) whenever a subscription
	// is added, so it starts with the current value of every point instead of waiting for each to
	// change - a breaker that never moves would otherwise never be reported at all.
	generalInterrogation bool
	// interrogationAsduAddress is the common address the interrogation is sent to.
	interrogationAsduAddress uint16
	// originatorAddress is this controlling station's originator address in the ASDUs it sends.
	originatorAddress uint8
}

// DefaultConfiguration is a connection without any options set.
func DefaultConfiguration() Configuration {
	return Configuration{
		requestTimeout:           defaultRequestTimeout,
		ackThreshold:             defaultAckThreshold,
		t1:                       defaultT1,
		t3:                       defaultT3,
		generalInterrogation:     true,
		interrogationAsduAddress: defaultInterrogationAsduAddress,
	}
}

// ParseFromOptions reads the connection options out of a parsed connection string. The timeout is
// spelled in milliseconds, the way plc4j's @IntDefaultValue(4000) does.
func ParseFromOptions(localLog zerolog.Logger, connectionOptions map[string][]string) (Configuration, error) {
	// Every option this driver reads goes through the reader, so the ones nothing read can be
	// reported rather than silently discarded. Deferred, so no return path can skip it.
	reader := spiOptions.NewOptionReader(localLog, connectionOptions)
	defer reader.ReportUnknown("iec-60870-5-104")

	configuration := DefaultConfiguration()

	if requestTimeoutString := reader.Get("request-timeout-ms"); requestTimeoutString != "" {
		parsedInt, err := strconv.ParseUint(requestTimeoutString, 10, 32)
		if err != nil {
			return Configuration{}, errors.Wrapf(err, "Error parsing request-timeout-ms %s", requestTimeoutString)
		}
		if parsedInt == 0 {
			return Configuration{}, errors.New("request-timeout-ms must be greater than zero")
		}
		configuration.requestTimeout = time.Duration(parsedInt) * time.Millisecond
	}

	if ackThresholdString := reader.Get("ack-threshold"); ackThresholdString != "" {
		parsedInt, err := strconv.ParseUint(ackThresholdString, 10, 32)
		if err != nil {
			return Configuration{}, errors.Wrapf(err, "Error parsing ack-threshold %s", ackThresholdString)
		}
		if parsedInt == 0 {
			return Configuration{}, errors.New("ack-threshold must be greater than zero")
		}
		if parsedInt > maxAckThreshold {
			return Configuration{}, errors.Errorf("ack-threshold may not be larger than %d. Was %d", maxAckThreshold, parsedInt)
		}
		configuration.ackThreshold = uint16(parsedInt)
	}

	for _, timer := range []struct {
		option string
		target *time.Duration
		zeroOk bool
	}{{"t1-ms", &configuration.t1, false}, {"t3-ms", &configuration.t3, true}} {
		if value := reader.Get(timer.option); value != "" {
			parsedInt, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return Configuration{}, errors.Wrapf(err, "Error parsing %s %s", timer.option, value)
			}
			if parsedInt == 0 && !timer.zeroOk {
				return Configuration{}, errors.Errorf("%s must be greater than zero", timer.option)
			}
			*timer.target = time.Duration(parsedInt) * time.Millisecond
		}
	}

	if value := reader.Get("general-interrogation"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return Configuration{}, errors.Wrapf(err, "Error parsing general-interrogation %s", value)
		}
		configuration.generalInterrogation = parsed
	}

	if value := reader.Get("interrogation-asdu-address"); value != "" {
		parsedInt, err := strconv.ParseUint(value, 10, 16)
		if err != nil {
			return Configuration{}, errors.Wrapf(err, "Error parsing interrogation-asdu-address %s", value)
		}
		configuration.interrogationAsduAddress = uint16(parsedInt)
	}

	if value := reader.Get("originator-address"); value != "" {
		parsedInt, err := strconv.ParseUint(value, 10, 8)
		if err != nil {
			return Configuration{}, errors.Wrapf(err, "Error parsing originator-address %s", value)
		}
		configuration.originatorAddress = uint8(parsedInt)
	}

	return configuration, nil
}
