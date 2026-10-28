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
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/errors"
	spiModel "github.com/apache/plc4x/plc4go/spi/model"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/utils"
	spiValues "github.com/apache/plc4x/plc4go/spi/values"
)

// SubscriptionHandle is plc4j's CANOpenSubscriptionHandle: one tag of a subscription request.
type SubscriptionHandle struct {
	*spiModel.DefaultPlcSubscriptionHandle
	tagName string
	tag     apiModel.PlcTag
}

func newSubscriptionHandle(subscriber *Subscriber, tagName string, tag apiModel.PlcTag) *SubscriptionHandle {
	handle := &SubscriptionHandle{tagName: tagName, tag: tag}
	handle.DefaultPlcSubscriptionHandle = spiModel.NewDefaultPlcSubscriptionHandleWithHandleToRegister(subscriber, handle)
	return handle
}

// matches is whether a frame of service from nodeId is the handle's, as plc4j's matches: a PDO tag takes its node's
// PDO, a heartbeat tag its node's heartbeats (or any node's). An NMT command's COB-ID carries no node, so for NMT Go
// matches the node the command addresses (its second byte, 0 being every node), where plc4j compares the COB-ID's
// node-ID, which is always 0, so that plc4j's "NMT:1" never matches.
func (h *SubscriptionHandle) matches(service readWriteModel.CANOpenService, nodeId uint8, payload readWriteModel.CANOpenPayload) bool {
	switch tag := h.tag.(type) {
	case PDOTag:
		return service == tag.Service && nodeId == tag.NodeId
	case HeartbeatTag:
		return service == readWriteModel.CANOpenService_HEARTBEAT && (tag.NodeId == 0 || nodeId == tag.NodeId)
	case NMTTag:
		command, ok := payload.(readWriteModel.CANOpenNetworkPayload)
		return ok && (tag.NodeId == 0 || command.GetNode() == 0 || command.GetNode() == tag.NodeId)
	}
	return false
}

// SubscriptionEvent carries the value one frame gave a handle's tag.
type SubscriptionEvent struct {
	*spiModel.DefaultPlcSubscriptionEvent
	address string
}

// GetAddress is the address of the tag the event is for.
func (m SubscriptionEvent) GetAddress(_ string) string {
	return m.address
}

// Subscriber takes event subscriptions, which is all plc4j's driver takes: PDOs, NMT commands and heartbeats come
// when a node sends them.
type Subscriber struct {
	connection *Connection

	mutex     sync.RWMutex
	consumers map[*spiModel.DefaultPlcConsumerRegistration]apiModel.PlcSubscriptionEventConsumer
	handles   map[*SubscriptionHandle]struct{}

	wg       sync.WaitGroup
	log      zerolog.Logger
	_options []options.WithOption
}

func NewSubscriber(connection *Connection, _options ...options.WithOption) *Subscriber {
	return &Subscriber{
		connection: connection,
		consumers:  map[*spiModel.DefaultPlcConsumerRegistration]apiModel.PlcSubscriptionEventConsumer{},
		handles:    map[*SubscriptionHandle]struct{}{},
		log:        options.ExtractCustomLoggerOrDefaultToGlobal(_options...),
		_options:   slices.Clip(_options),
	}
}

// Subscribe answers each tag as plc4j's onSubscribe: OK with a handle for a PDO, NMT or heartbeat tag, INVALID_ADDRESS
// for an SDO tag (an object is read, not pushed), and UNSUPPORTED for any subscription but an event.
func (s *Subscriber) Subscribe(ctx context.Context, subscriptionRequest apiModel.PlcSubscriptionRequest) <-chan apiModel.PlcSubscriptionRequestResult {
	result := make(chan apiModel.PlcSubscriptionRequestResult, 1)
	s.wg.Go(func() {
		defer func() {
			if err := recover(); err != nil {
				utils.DeliverResult(s.log, result, spiModel.NewDefaultPlcSubscriptionRequestResult(subscriptionRequest, nil, errors.Errorf("panic-ed %v. Stack: %s", err, debug.Stack())))
			}
		}()
		request, ok := subscriptionRequest.(*spiModel.DefaultPlcSubscriptionRequest)
		if !ok {
			utils.DeliverResult(s.log, result, spiModel.NewDefaultPlcSubscriptionRequestResult(subscriptionRequest, nil, errors.Errorf("unsupported subscription request type %T", subscriptionRequest)))
			return
		}
		if err := ctx.Err(); err != nil {
			utils.DeliverResult(s.log, result, spiModel.NewDefaultPlcSubscriptionRequestResult(subscriptionRequest, nil, err))
			return
		}
		codes := map[string]apiModel.PlcResponseCode{}
		handles := map[string]apiModel.PlcSubscriptionHandle{}
		var created []*SubscriptionHandle
		for _, tagName := range request.GetTagNames() {
			tag := request.GetTag(tagName)
			switch tag.(type) {
			case PDOTag, NMTTag, HeartbeatTag:
				if request.GetType(tagName) != apiModel.SubscriptionEvent {
					codes[tagName] = apiModel.PlcResponseCode_UNSUPPORTED
					continue
				}
				handle := newSubscriptionHandle(s, tagName, tag)
				created = append(created, handle)
				codes[tagName], handles[tagName] = apiModel.PlcResponseCode_OK, handle
			default:
				codes[tagName] = apiModel.PlcResponseCode_INVALID_ADDRESS
			}
		}
		if len(created) > 0 {
			s.mutex.Lock()
			for _, handle := range created {
				s.handles[handle] = struct{}{}
			}
			s.mutex.Unlock()
			s.connection.addSubscriber(s)
		}
		utils.DeliverResult(s.log, result, spiModel.NewDefaultPlcSubscriptionRequestResult(subscriptionRequest,
			spiModel.NewDefaultPlcSubscriptionResponse(subscriptionRequest, codes, handles, append(s._options, options.WithCustomLogger(s.log))...), nil))
	})
	return result
}

// Unsubscribe drops the handles; CANopen has nothing to tell the bus.
func (s *Subscriber) Unsubscribe(ctx context.Context, unsubscriptionRequest apiModel.PlcUnsubscriptionRequest) <-chan apiModel.PlcUnsubscriptionRequestResult {
	result := make(chan apiModel.PlcUnsubscriptionRequestResult, 1)
	request, ok := unsubscriptionRequest.(*spiModel.DefaultPlcUnsubscriptionRequest)
	if !ok {
		result <- spiModel.NewDefaultPlcUnsubscriptionRequestResult(unsubscriptionRequest, nil, errors.Errorf("unsupported unsubscription request type %T", unsubscriptionRequest))
		return result
	}
	s.mutex.Lock()
	for _, handle := range request.GetSubscriptionHandles() {
		if subscriptionHandle, ok := handle.(*SubscriptionHandle); ok {
			delete(s.handles, subscriptionHandle)
		}
	}
	nothingLeft := len(s.handles) == 0
	s.mutex.Unlock()
	if nothingLeft {
		s.connection.removeSubscriber(s)
	}
	result <- spiModel.NewDefaultPlcUnsubscriptionRequestResult(unsubscriptionRequest, spiModel.NewDefaultPlcUnsubscriptionResponse(unsubscriptionRequest), nil)
	return result
}

func (s *Subscriber) Register(consumer apiModel.PlcSubscriptionEventConsumer, handles []apiModel.PlcSubscriptionHandle) apiModel.PlcConsumerRegistration {
	registration := spiModel.NewDefaultPlcConsumerRegistration(s, consumer, handles...)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.consumers[registration.(*spiModel.DefaultPlcConsumerRegistration)] = consumer
	return registration
}

func (s *Subscriber) Unregister(registration apiModel.PlcConsumerRegistration) {
	if defaultRegistration, ok := registration.(*spiModel.DefaultPlcConsumerRegistration); ok {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		delete(s.consumers, defaultRegistration)
	}
}

func (s *Subscriber) Close() {
	s.mutex.Lock()
	s.consumers = map[*spiModel.DefaultPlcConsumerRegistration]apiModel.PlcSubscriptionEventConsumer{}
	s.handles = map[*SubscriptionHandle]struct{}{}
	s.mutex.Unlock()
	s.wg.Wait()
}

// publish hands one frame to the consumers of every handle it matches, outside the lock: a consumer may subscribe or
// unsubscribe from inside its callback.
func (s *Subscriber) publish(ctx context.Context, service readWriteModel.CANOpenService, nodeId uint8, data []byte, payload readWriteModel.CANOpenPayload) {
	type delivery struct {
		handle    *SubscriptionHandle
		consumers []apiModel.PlcSubscriptionEventConsumer
	}
	var deliveries []delivery
	s.mutex.RLock()
	for registration, consumer := range s.consumers {
		for _, handle := range registration.GetSubscriptionHandles() {
			subscriptionHandle, ok := handle.(*SubscriptionHandle)
			if !ok || !subscriptionHandle.matches(service, nodeId, payload) {
				continue
			}
			if _, active := s.handles[subscriptionHandle]; !active {
				continue
			}
			index := slices.IndexFunc(deliveries, func(d delivery) bool { return d.handle == subscriptionHandle })
			if index < 0 {
				deliveries = append(deliveries, delivery{handle: subscriptionHandle})
				index = len(deliveries) - 1
			}
			deliveries[index].consumers = append(deliveries[index].consumers, consumer)
		}
	}
	s.mutex.RUnlock()
	for _, d := range deliveries {
		event := s.event(ctx, d.handle, nodeId, data, payload)
		for _, consumer := range d.consumers {
			consumer(event)
		}
	}
}

// event is plc4j's buildEvent: a PDO tag's value read from the PDO's data (INVALID_DATA when it does not read), and
// for NMT and heartbeat tags the struct {state, node}: the command specifier and the node it addresses, or the node's
// NMT state and its node-ID.
func (s *Subscriber) event(ctx context.Context, handle *SubscriptionHandle, nodeId uint8, data []byte, payload readWriteModel.CANOpenPayload) *SubscriptionEvent {
	code, value := apiModel.PlcResponseCode_OK, apiValues.PlcValue(nil)
	switch tag := handle.tag.(type) {
	case PDOTag:
		decoded, err := decodeValue(ctx, data, tag.DataType, tag.Elements)
		if err != nil {
			s.log.Debug().Err(err).Stringer("tag", tag).Msg("could not read the PDO")
			code = apiModel.PlcResponseCode_INVALID_DATA
		} else {
			value = decoded
		}
	case HeartbeatTag:
		if heartbeat, ok := payload.(readWriteModel.CANOpenHeartbeatPayload); ok {
			value = stateStruct(uint8(heartbeat.GetState()), nodeId)
		}
	case NMTTag:
		if command, ok := payload.(readWriteModel.CANOpenNetworkPayload); ok {
			value = stateStruct(uint8(command.GetRequest()), command.GetNode())
		}
	}
	tagName := handle.tagName
	values := map[string]apiValues.PlcValue{}
	if value != nil {
		values[tagName] = value
	}
	event := &SubscriptionEvent{address: handle.tag.GetAddressString()}
	event.DefaultPlcSubscriptionEvent = spiModel.NewDefaultPlcSubscriptionEvent(event,
		map[string]apiModel.PlcTag{tagName: handle.tag},
		map[string]apiModel.PlcSubscriptionType{tagName: apiModel.SubscriptionEvent},
		map[string]time.Duration{tagName: 0},
		map[string]apiModel.PlcResponseCode{tagName: code},
		values,
		append(s._options, options.WithCustomLogger(s.log))...).(*spiModel.DefaultPlcSubscriptionEvent)
	return event
}

// stateStruct is the {state, node} of an NMT command or a heartbeat, in plc4j's order.
func stateStruct(state uint8, nodeId uint8) apiValues.PlcValue {
	return spiValues.NewPlcStructOrdered(map[string]apiValues.PlcValue{
		"state": spiValues.NewPlcUSINT(state),
		"node":  spiValues.NewPlcUSINT(nodeId),
	}, []string{"state", "node"})
}
