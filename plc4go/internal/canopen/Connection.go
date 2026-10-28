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
	"fmt"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/apache/plc4x/plc4go/internal/genericcan"
	plc4go "github.com/apache/plc4x/plc4go/pkg/api"
	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
	socketcanModel "github.com/apache/plc4x/plc4go/protocols/socketcan/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi"
	_default "github.com/apache/plc4x/plc4go/spi/default"
	"github.com/apache/plc4x/plc4go/spi/errors"
	spiModel "github.com/apache/plc4x/plc4go/spi/model"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/tracer"
	"github.com/apache/plc4x/plc4go/spi/transports/can"
	"github.com/apache/plc4x/plc4go/spi/utils"
)

type Connection struct {
	_default.DefaultConnection

	configuration Configuration
	messageCodec  *genericcan.MessageCodec
	options       map[string][]string

	connectionId string
	tracer       tracer.Tracer

	// sdoMutex lets one SDO transfer run at a time; sdoChannel is the one running (see openSdo).
	sdoMutex   sync.Mutex
	sdoChannel atomic.Pointer[sdoChannel]

	// latest is the last data received with each COB-ID the driver reads (PDOs and heartbeats); latestChanged is
	// closed, and replaced, each time one arrives, to wake the reads waiting for one.
	latestMutex   sync.Mutex
	latest        map[uint16][]byte
	latestChanged chan struct{}

	subscribersMutex sync.RWMutex
	subscribers      []*Subscriber

	lifecycleMutex sync.Mutex
	workerCancel   context.CancelFunc
	wg             sync.WaitGroup

	log      zerolog.Logger
	_options []options.WithOption
}

func NewConnection(configuration Configuration, messageCodec *genericcan.MessageCodec, connectionOptions map[string][]string, tagHandler spi.PlcTagHandler, _options ...options.WithOption) *Connection {
	customLogger := options.ExtractCustomLoggerOrDefaultToGlobal(_options...)
	_options = slices.Clip(_options)
	connection := &Connection{
		configuration: configuration,
		messageCodec:  messageCodec,
		options:       connectionOptions,
		connectionId:  utils.GenerateId(4),
		latest:        map[uint16][]byte{},
		latestChanged: make(chan struct{}),
		log:           customLogger,
		_options:      _options,
	}
	if traceEnabledOption, ok := connectionOptions["traceEnabled"]; ok && len(traceEnabledOption) == 1 {
		connection.tracer = tracer.NewTracer(connection.connectionId, _options...)
	}
	connection.DefaultConnection = _default.NewDefaultConnection(connection,
		append(_options,
			_default.WithPlcTagHandler(tagHandler),
			_default.WithPlcValueHandler(NewValueHandler(_options...)),
		)...,
	)
	return connection
}

func (c *Connection) GetConnectionId() string {
	return c.connectionId
}

func (c *Connection) IsTraceEnabled() bool {
	return c.tracer != nil
}

func (c *Connection) GetTracer() tracer.Tracer {
	return c.tracer
}

func (c *Connection) GetConnection() plc4go.PlcConnection {
	return c
}

func (c *Connection) GetMessageCodec() spi.MessageCodec {
	return c.messageCodec
}

// IsConnected is also false once the codec stopped, which it does when the interface went away (a fatal transport
// error): nothing more will arrive on this connection.
func (c *Connection) IsConnected() bool {
	return c.DefaultConnection.IsConnected() && c.messageCodec.IsRunning()
}

// Connect starts taking frames and, with heartbeat set, producing heartbeats: BOOTED_UP once (CiA 301 7.2.8.3.2.1, the
// boot-up message) and OPERATIONAL every 10 s after, as plc4j's connection does.
func (c *Connection) Connect(ctx context.Context) error {
	if err := c.DefaultConnection.Connect(ctx); err != nil {
		return errors.Wrap(err, "error connecting default connection")
	}
	workerCtx := c.startIncomingWorker()
	if c.configuration.heartbeat {
		if err := c.sendHeartbeat(ctx, readWriteModel.NMTState_BOOTED_UP); err != nil {
			_ = c.Close()
			return errors.Wrap(err, "Failed to send the CANopen boot-up message")
		}
		c.wg.Go(func() { c.produceHeartbeats(workerCtx) })
	}
	return nil
}

func (c *Connection) Close() error {
	c.lifecycleMutex.Lock()
	workerCancel := c.workerCancel
	c.workerCancel = nil
	c.lifecycleMutex.Unlock()
	if workerCancel != nil {
		workerCancel()
	}
	err := c.DefaultConnection.Close()
	c.wg.Wait()
	c.subscribersMutex.Lock()
	subscribers := c.subscribers
	c.subscribers = nil
	c.subscribersMutex.Unlock()
	for _, subscriber := range subscribers {
		subscriber.Close()
	}
	return err
}

func (c *Connection) startIncomingWorker() context.Context {
	workerCtx, workerCancel := context.WithCancel(context.Background())
	c.lifecycleMutex.Lock()
	c.workerCancel = workerCancel
	c.lifecycleMutex.Unlock()
	incoming := c.messageCodec.GetDefaultIncomingMessageChannel()
	c.wg.Go(func() {
		defer func() {
			if err := recover(); err != nil {
				c.log.Error().Str("stack", string(debug.Stack())).Interface("err", err).Msg("panic-ed")
			}
		}()
		for {
			select {
			case <-workerCtx.Done():
				return
			case message := <-incoming:
				if frame, ok := message.(socketcanModel.SocketCANFrame); ok {
					c.handleFrame(workerCtx, frame)
				}
			}
		}
	})
	return workerCtx
}

func (c *Connection) produceHeartbeats(ctx context.Context) {
	ticker := time.NewTicker(heartbeatPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.sendHeartbeat(ctx, readWriteModel.NMTState_OPERATIONAL); err != nil {
				c.log.Warn().Err(err).Msg("Failed to send CANopen heartbeat")
			}
		}
	}
}

func (c *Connection) sendHeartbeat(ctx context.Context, state readWriteModel.NMTState) error {
	return c.sendPayload(ctx, readWriteModel.CANOpenService_HEARTBEAT, c.configuration.nodeId, readWriteModel.NewCANOpenHeartbeatPayload(state))
}

// handleFrame dispatches one frame by its COB-ID: SDO responses to the transfer waiting for them, and PDOs, NMT
// commands and heartbeats to the subscribers (PDOs and heartbeats are also kept for reads). SYNC and EMCY are decoded
// and logged: plc4j's tag syntax has no tag for them, so nothing subscribes to them. CANopen uses 11-bit
// identifiers (CiA 301 7.3.3); remote requests, error frames and extended frames are passed over, and so is a frame
// whose data does not parse as its service's.
//
// plc4j also passes over every frame whose node-ID is the connection's own, which with the default node-id 0 drops
// every NMT command; the CAN transports do not hand a connection its own frames, so Go needs no such filter.
func (c *Connection) handleFrame(ctx context.Context, frame socketcanModel.SocketCANFrame) {
	if frame.GetRemote() || frame.GetError() || frame.GetExtended() {
		return
	}
	cobId := uint16(uint32(frame.GetIdentifier()) & can.SffMask)
	service, nodeId, ok := resolve(cobId)
	if !ok {
		return
	}
	data := slices.Clone(frame.GetData())
	if service.GetPdo() {
		c.keepLatest(cobId, data)
		c.publish(ctx, service, nodeId, data, nil)
		return
	}
	switch service {
	case readWriteModel.CANOpenService_TRANSMIT_SDO, readWriteModel.CANOpenService_HEARTBEAT, readWriteModel.CANOpenService_NMT,
		readWriteModel.CANOpenService_SYNC, readWriteModel.CANOpenService_EMCY:
	default:
		return
	}
	payload, err := readWriteModel.CANOpenPayloadParse[readWriteModel.CANOpenPayload](ctx, data, service)
	if err != nil {
		c.log.Debug().Err(err).Stringer("service", service).Uint8("nodeId", nodeId).Msg("passing over a frame that does not parse")
		return
	}
	switch typed := payload.(type) {
	case readWriteModel.CANOpenSDOResponse:
		c.deliverSdo(nodeId, typed.GetResponse())
	case readWriteModel.CANOpenHeartbeatPayload:
		c.keepLatest(cobId, data)
		c.publish(ctx, service, nodeId, data, typed)
	case readWriteModel.CANOpenNetworkPayload:
		c.publish(ctx, service, nodeId, data, typed)
	case readWriteModel.CANOpenEmergencyPayload:
		c.log.Debug().Uint8("nodeId", nodeId).Str("errorCode", fmt.Sprintf("0x%04X", typed.GetErrorCode())).
			Str("errorRegister", fmt.Sprintf("0x%02X", typed.GetErrorRegister())).Msg("CANopen emergency")
	}
}

func (c *Connection) keepLatest(cobId uint16, data []byte) {
	c.latestMutex.Lock()
	defer c.latestMutex.Unlock()
	c.latest[cobId] = data
	close(c.latestChanged)
	c.latestChanged = make(chan struct{})
}

// latestData is the last data received with cobId, waiting for some until ctx ends when none has come yet.
func (c *Connection) latestData(ctx context.Context, cobId uint16) ([]byte, bool) {
	for {
		c.latestMutex.Lock()
		data, ok := c.latest[cobId]
		changed := c.latestChanged
		c.latestMutex.Unlock()
		if ok {
			return data, true
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, false
		}
	}
}

func (c *Connection) publish(ctx context.Context, service readWriteModel.CANOpenService, nodeId uint8, data []byte, payload readWriteModel.CANOpenPayload) {
	for _, subscriber := range c.activeSubscribers() {
		subscriber.publish(ctx, service, nodeId, data, payload)
	}
}

// sendPayload puts payload on the bus as service's frame for nodeId.
func (c *Connection) sendPayload(ctx context.Context, service readWriteModel.CANOpenService, nodeId uint8, payload readWriteModel.CANOpenPayload) error {
	data, err := payload.Serialize()
	if err != nil {
		return errors.Wrapf(err, "error serializing the %s payload", service)
	}
	return c.sendData(ctx, cobIdOf(service, nodeId), data)
}

// sendData puts one 11-bit frame on the bus. plc4j's codec sends the payload's first eight bytes; Go refuses more.
func (c *Connection) sendData(ctx context.Context, cobId uint16, data []byte) error {
	if len(data) > can.MaxDataLength {
		return errors.Errorf("%d bytes do not fit a CAN frame", len(data))
	}
	frame := socketcanModel.NewSocketCANFrame(int32(cobId), data)
	return c.messageCodec.Send(ctx, fmt.Sprintf("frame 0x%03X", cobId), frame)
}

func (c *Connection) addSubscriber(subscriber *Subscriber) {
	c.subscribersMutex.Lock()
	defer c.subscribersMutex.Unlock()
	if !slices.Contains(c.subscribers, subscriber) {
		c.subscribers = append(c.subscribers, subscriber)
	}
}

func (c *Connection) removeSubscriber(subscriber *Subscriber) {
	c.subscribersMutex.Lock()
	defer c.subscribersMutex.Unlock()
	c.subscribers = slices.DeleteFunc(c.subscribers, func(existing *Subscriber) bool { return existing == subscriber })
}

func (c *Connection) activeSubscribers() []*Subscriber {
	c.subscribersMutex.RLock()
	defer c.subscribersMutex.RUnlock()
	return slices.Clone(c.subscribers)
}

// GetMetadata: reading, writing and subscribing, as plc4j's driver.
func (c *Connection) GetMetadata() apiModel.PlcConnectionMetadata {
	return &_default.DefaultConnectionMetadata{
		ProvidesReading:     true,
		ProvidesWriting:     true,
		ProvidesSubscribing: true,
		ProvidesBrowsing:    false,
	}
}

func (c *Connection) ReadRequestBuilder() apiModel.PlcReadRequestBuilder {
	return spiModel.NewDefaultPlcReadRequestBuilder(c.GetPlcTagHandler(), NewReader(c, append(c._options, options.WithCustomLogger(c.log))...))
}

func (c *Connection) WriteRequestBuilder() apiModel.PlcWriteRequestBuilder {
	return spiModel.NewDefaultPlcWriteRequestBuilder(c.GetPlcTagHandler(), c.GetPlcValueHandler(), NewWriter(c, append(c._options, options.WithCustomLogger(c.log))...))
}

func (c *Connection) SubscriptionRequestBuilder() apiModel.PlcSubscriptionRequestBuilder {
	return spiModel.NewDefaultPlcSubscriptionRequestBuilder(c.GetPlcTagHandler(), c.GetPlcValueHandler(), NewSubscriber(c, append(c._options, options.WithCustomLogger(c.log))...))
}

func (c *Connection) UnsubscriptionRequestBuilder() apiModel.PlcUnsubscriptionRequestBuilder {
	return spiModel.NewDefaultPlcUnsubscriptionRequestBuilder()
}

func (c *Connection) String() string {
	return fmt.Sprintf("canopen.Connection{nodeId: %d, heartbeat: %t, requestTimeout: %s}",
		c.configuration.nodeId, c.configuration.heartbeat, c.configuration.requestTimeout)
}
