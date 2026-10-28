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
	"fmt"
	"runtime/debug"
	"slices"
	"sync"

	"github.com/rs/zerolog"

	plc4go "github.com/apache/plc4x/plc4go/pkg/api"
	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
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
	messageCodec  *MessageCodec
	options       map[string][]string

	connectionId string
	tracer       tracer.Tracer

	// latest is the last payload received for each identifier (for reads); latestChanged is closed, and replaced, each
	// time a frame arrives, to wake the reads waiting for one.
	latestMutex   sync.Mutex
	latest        map[uint32][]byte
	latestChanged chan struct{}

	subscribersMutex sync.RWMutex
	subscribers      []*Subscriber

	lifecycleMutex sync.Mutex
	workerCancel   context.CancelFunc
	wg             sync.WaitGroup

	log      zerolog.Logger
	_options []options.WithOption
}

func NewConnection(configuration Configuration, messageCodec *MessageCodec, connectionOptions map[string][]string, tagHandler spi.PlcTagHandler, _options ...options.WithOption) *Connection {
	customLogger := options.ExtractCustomLoggerOrDefaultToGlobal(_options...)
	_options = slices.Clip(_options)
	connection := &Connection{
		configuration: configuration,
		messageCodec:  messageCodec,
		options:       connectionOptions,
		connectionId:  utils.GenerateId(4),
		latest:        map[uint32][]byte{},
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

func (c *Connection) Connect(ctx context.Context) error {
	if err := c.DefaultConnection.Connect(ctx); err != nil {
		return errors.Wrap(err, "error connecting default connection")
	}
	c.startIncomingWorker()
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

func (c *Connection) startIncomingWorker() {
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
}

// handleFrame keeps the frame's payload for reads and hands it to the subscribers. Remote requests and error frames
// carry no data, and are passed over.
func (c *Connection) handleFrame(ctx context.Context, frame socketcanModel.SocketCANFrame) {
	if frame.GetRemote() || frame.GetError() {
		return
	}
	nodeId := uint32(frame.GetIdentifier())
	data := slices.Clone(frame.GetData())
	c.latestMutex.Lock()
	c.latest[nodeId] = data
	close(c.latestChanged)
	c.latestChanged = make(chan struct{})
	c.latestMutex.Unlock()
	for _, subscriber := range c.activeSubscribers() {
		subscriber.publish(ctx, nodeId, data)
	}
}

// latestFrame is the last payload received for nodeId, waiting for one until ctx ends when none has come yet.
func (c *Connection) latestFrame(ctx context.Context, nodeId uint32) ([]byte, bool) {
	for {
		c.latestMutex.Lock()
		data, ok := c.latest[nodeId]
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

// sendFrame puts one frame on the bus. An identifier above 0x7FF can only be an extended one, so it goes out with
// CAN_EFF_FLAG. plc4j hands the transport the bare identifier and leaves the extended flag off, which a CAN controller
// truncates to its low 11 bits; Go sets it.
func (c *Connection) sendFrame(ctx context.Context, nodeId uint32, data []byte) error {
	rawId := nodeId
	if nodeId > can.SffMask {
		rawId |= can.EffFlag
	}
	frame := socketcanModel.NewSocketCANFrame(int32(rawId), data)
	return c.messageCodec.Send(ctx, fmt.Sprintf("frame 0x%X", nodeId), frame)
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

// GetMetadata: reading (see Reader), writing and subscribing. plc4j's driver offers writing and subscribing.
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
	return fmt.Sprintf("genericcan.Connection{requestTimeout: %s}", c.configuration.requestTimeout)
}

// group splits tags into their identifiers, in the order each identifier first appears, keeping request order within
// each: the layout of one frame.
func group(tagNames []string, tagOf func(string) (Tag, bool), skipped func(string)) (order []uint32, names map[uint32][]string, tags map[uint32][]Tag) {
	names, tags = map[uint32][]string{}, map[uint32][]Tag{}
	for _, tagName := range tagNames {
		tag, ok := tagOf(tagName)
		if !ok {
			skipped(tagName)
			continue
		}
		if _, seen := tags[tag.NodeId]; !seen {
			order = append(order, tag.NodeId)
		}
		names[tag.NodeId] = append(names[tag.NodeId], tagName)
		tags[tag.NodeId] = append(tags[tag.NodeId], tag)
	}
	return order, names, tags
}
