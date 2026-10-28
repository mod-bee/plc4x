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

// Package socketcan is the can-socketcan transport: a Linux CAN interface (can0, vcan0) through a raw AF_CAN socket,
// as plc4j's can-socketcan transport is.
//
// The interface is named in the address (can-socketcan://can0) or, for a caller that builds the connection string out
// of options alone, by can-socketcan.interface-name; the address wins when both name one, as in plc4j. The interface's
// bitrate is the operating system's (ip link set can0 type can bitrate 250000): a raw socket cannot set it.
//
// Not ported from plc4j: reuse-interface (every Go instance opens a socket of its own, which on Linux sees every frame
// anyway), read-timeout-ms (reads are bounded by each request's context) and the userspace id filters.
package socketcan

import (
	"net/url"
	"strings"

	"github.com/rs/zerolog"

	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/transports"
	"github.com/apache/plc4x/plc4go/spi/transports/can"
)

// TransportCode is how a connection string names this transport.
const TransportCode = "can-socketcan"

// interfaceNameOption names the interface when the address does not.
const interfaceNameOption = TransportCode + ".interface-name"

// maxInterfaceName is IFNAMSIZ less its terminating NUL.
const maxInterfaceName = 15

type Transport struct {
	log zerolog.Logger
}

var _ transports.Transport = (*Transport)(nil)

func NewTransport(_options ...options.WithOption) *Transport {
	return &Transport{log: options.ExtractCustomLoggerOrDefaultToGlobal(_options...)}
}

func (m *Transport) GetTransportCode() string {
	return TransportCode
}

func (m *Transport) GetTransportName() string {
	return "SocketCAN"
}

func (m *Transport) CreateTransportInstance(transportUrl url.URL, transportOptions map[string][]string, _options ...options.WithOption) (transports.TransportInstance, error) {
	name, err := InterfaceName(transportUrl, transportOptions)
	if err != nil {
		return nil, err
	}
	return can.NewTransportInstance(TransportCode+"://"+name, func() (can.FrameConn, error) { return open(name) }, _options...), nil
}

// InterfaceName is the CAN interface a transport URL and its options name.
func InterfaceName(transportUrl url.URL, transportOptions map[string][]string) (string, error) {
	name := transportUrl.Host
	if name == "" {
		name = strings.TrimPrefix(transportUrl.Opaque, "//")
	}
	if name == "" {
		if values := transportOptions[interfaceNameOption]; len(values) > 0 {
			name = values[0]
		}
	}
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", errors.New("CAN interface name must be specified (can-socketcan://can0)")
	case len(name) > maxInterfaceName || strings.ContainsAny(name, "/: \t"):
		return "", errors.Errorf("%q is not a network interface name", name)
	}
	return name, nil
}

func (m *Transport) Close() error {
	return nil
}

func (m *Transport) String() string {
	return m.GetTransportCode() + "(" + m.GetTransportName() + ")"
}
