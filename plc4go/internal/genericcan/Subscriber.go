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
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	"github.com/apache/plc4x/plc4go/spi/errors"
	spiModel "github.com/apache/plc4x/plc4go/spi/model"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/utils"
)

// SubscriptionHandle is plc4j's GenericCANSubscriptionHandle: the tags of one subscription request that name one
// identifier, in request order, read together from each of its frames.
type SubscriptionHandle struct {
	*spiModel.DefaultPlcSubscriptionHandle
	nodeId   uint32
	tagNames []string
	tags     []Tag
}

func newSubscriptionHandle(subscriber *Subscriber, nodeId uint32) *SubscriptionHandle {
	handle := &SubscriptionHandle{nodeId: nodeId}
	handle.DefaultPlcSubscriptionHandle = spiModel.NewDefaultPlcSubscriptionHandleWithHandleToRegister(subscriber, handle)
	return handle
}

// SubscriptionEvent carries the values one frame gave a handle's tags.
type SubscriptionEvent struct {
	*spiModel.DefaultPlcSubscriptionEvent
	address string
}

// GetAddress is the identifier of the frame the event came from.
func (m SubscriptionEvent) GetAddress(_ string) string {
	return m.address
}

// Subscriber takes event subscriptions, which is all plc4j's driver takes: CAN frames come when their sender sends
// them.
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
		byNode := map[uint32]*SubscriptionHandle{}
		for _, tagName := range request.GetTagNames() {
			tag, isTag := request.GetTag(tagName).(Tag)
			switch {
			case request.GetType(tagName) != apiModel.SubscriptionEvent:
				codes[tagName] = apiModel.PlcResponseCode_UNSUPPORTED
			case !isTag:
				codes[tagName] = apiModel.PlcResponseCode_INVALID_ADDRESS
			default:
				handle := byNode[tag.NodeId]
				if handle == nil {
					handle = newSubscriptionHandle(s, tag.NodeId)
					byNode[tag.NodeId] = handle
				}
				handle.tagNames = append(handle.tagNames, tagName)
				handle.tags = append(handle.tags, tag)
				codes[tagName], handles[tagName] = apiModel.PlcResponseCode_OK, handle
			}
		}
		if len(byNode) > 0 {
			s.mutex.Lock()
			for _, handle := range byNode {
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

// Unsubscribe drops the handles; CAN has nothing to tell the bus.
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

// publish hands one frame to the consumers of every handle for its identifier, outside the lock: a consumer may
// subscribe or unsubscribe from inside its callback.
func (s *Subscriber) publish(ctx context.Context, nodeId uint32, data []byte) {
	type delivery struct {
		handle    *SubscriptionHandle
		consumers []apiModel.PlcSubscriptionEventConsumer
	}
	var deliveries []delivery
	s.mutex.RLock()
	for registration, consumer := range s.consumers {
		for _, handle := range registration.GetSubscriptionHandles() {
			subscriptionHandle, ok := handle.(*SubscriptionHandle)
			if !ok || subscriptionHandle.nodeId != nodeId {
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
		event := s.event(ctx, d.handle, data)
		for _, consumer := range d.consumers {
			consumer(event)
		}
	}
}

func (s *Subscriber) event(ctx context.Context, handle *SubscriptionHandle, data []byte) *SubscriptionEvent {
	codes, values := decodeTags(ctx, data, handle.tags)
	tags := map[string]apiModel.PlcTag{}
	types := map[string]apiModel.PlcSubscriptionType{}
	intervals := map[string]time.Duration{}
	responseCodes := map[string]apiModel.PlcResponseCode{}
	plcValues := map[string]apiValues.PlcValue{}
	for i, tagName := range handle.tagNames {
		tags[tagName], types[tagName], intervals[tagName] = handle.tags[i], apiModel.SubscriptionEvent, 0
		responseCodes[tagName], plcValues[tagName] = codes[i], values[i]
	}
	event := &SubscriptionEvent{address: strconv.FormatUint(uint64(handle.nodeId), 10)}
	event.DefaultPlcSubscriptionEvent = spiModel.NewDefaultPlcSubscriptionEvent(event, tags, types, intervals, responseCodes, plcValues,
		append(s._options, options.WithCustomLogger(s.log))...).(*spiModel.DefaultPlcSubscriptionEvent)
	return event
}
