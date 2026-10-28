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
	"net/url"
	"slices"

	"github.com/rs/zerolog"

	plc4go "github.com/apache/plc4x/plc4go/pkg/api"
	_default "github.com/apache/plc4x/plc4go/spi/default"
	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/transports"
	"github.com/apache/plc4x/plc4go/spi/transports/socketcan"
	"github.com/apache/plc4x/plc4go/spi/transports/virtualcan"
)

// supportedTransportCodes are plc4j GenericCANDriver.getSupportedTransportCodes but its "test": SocketCAN (the default)
// and the in-process virtual bus. Go's test transport is a byte pipe with no notion of frames; the virtual bus is what
// the tests use.
var supportedTransportCodes = []string{socketcan.TransportCode, virtualcan.TransportCode}

type Driver struct {
	_default.DefaultDriver

	log      zerolog.Logger
	_options []options.WithOption
}

func NewDriver(_options ...options.WithOption) plc4go.PlcDriver {
	driver := &Driver{
		log:      options.ExtractCustomLoggerOrDefaultToGlobal(_options...),
		_options: _options,
	}
	driver.DefaultDriver = _default.NewDefaultDriver(driver, "genericcan", "Generic CAN", socketcan.TransportCode, NewTagHandler())
	return driver
}

// GetSupportedTransportCodes is the list of transports a Generic CAN connection string may name.
func (d *Driver) GetSupportedTransportCodes() []string {
	return slices.Clone(supportedTransportCodes)
}

func (d *Driver) GetConnection(ctx context.Context, transportUrl url.URL, transports map[string]transports.Transport, driverOptions map[string][]string) (plc4go.PlcConnection, error) {
	connectionLog := d.log.With().Ctx(ctx).Str("transportUrl", transportUrl.String()).Logger()
	configuration, err := ParseFromOptions(connectionLog, driverOptions)
	if err != nil {
		return nil, errors.Wrap(err, "can't parse config")
	}
	if !slices.Contains(supportedTransportCodes, transportUrl.Scheme) {
		return nil, errors.Errorf("the genericcan driver doesn't support the transport %s, it supports %v", transportUrl.Scheme, supportedTransportCodes)
	}
	transport, ok := transports[transportUrl.Scheme]
	if !ok {
		return nil, errors.Errorf("couldn't find transport for given transport url %#v", transportUrl)
	}
	transportInstance, err := transport.CreateTransportInstance(transportUrl, driverOptions, append(d._options, options.WithCustomLogger(connectionLog))...)
	if err != nil {
		return nil, errors.Wrapf(err, "couldn't initialize transport configuration for given transport url %s", transportUrl.String())
	}
	codec := NewMessageCodec(transportInstance, append(d._options, options.WithCustomLogger(connectionLog))...)
	connection := NewConnection(configuration, codec, driverOptions, d.GetPlcTagHandler(), append(d._options, options.WithCustomLogger(connectionLog))...)
	if err := connection.Connect(ctx); err != nil {
		return nil, errors.Wrap(err, "Error connecting connection")
	}
	return connection, nil
}

// SupportsDiscovery: there is nothing on a CAN bus to discover without an application protocol.
func (d *Driver) SupportsDiscovery() bool {
	return false
}
