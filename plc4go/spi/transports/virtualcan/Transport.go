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

// Package virtualcan is the can-virtualcan transport, as plc4j's: an in-process CAN bus, for tests and for wiring
// drivers to each other without hardware. Every instance connected to a bus of the same name (can-virtualcan://test)
// receives the frames every other instance on it writes - not its own, as a CAN controller does not.
package virtualcan

import (
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/transports"
	"github.com/apache/plc4x/plc4go/spi/transports/can"
)

// TransportCode is how a connection string names this transport.
const TransportCode = "can-virtualcan"

// busNameOption names the bus when the address does not.
const busNameOption = TransportCode + ".bus-name"

// queueDepth is how many frames a member holds unread before the oldest is dropped (a CAN controller's receive
// buffer overflows the same way).
const queueDepth = 1024

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
	return "Virtual CAN"
}

func (m *Transport) CreateTransportInstance(transportUrl url.URL, transportOptions map[string][]string, _options ...options.WithOption) (transports.TransportInstance, error) {
	name := transportUrl.Host
	if name == "" {
		name = strings.TrimPrefix(transportUrl.Opaque, "//")
	}
	if name == "" {
		if values := transportOptions[busNameOption]; len(values) > 0 {
			name = values[0]
		}
	}
	if name = strings.TrimSpace(name); name == "" {
		return nil, errors.New("virtual CAN bus name must be specified (can-virtualcan://test)")
	}
	return can.NewTransportInstance(TransportCode+"://"+name, func() (can.FrameConn, error) { return join(name), nil }, _options...), nil
}

func (m *Transport) Close() error {
	return nil
}

func (m *Transport) String() string {
	return m.GetTransportCode() + "(" + m.GetTransportName() + ")"
}

var (
	busesMutex sync.Mutex
	buses      = map[string]map[*member]struct{}{}
)

// member is one instance's place on a bus.
type member struct {
	bus      string
	frames   chan []byte
	closed   chan struct{}
	once     sync.Once
	mutex    sync.Mutex
	deadline time.Time
}

func join(bus string) *member {
	m := &member{bus: bus, frames: make(chan []byte, queueDepth), closed: make(chan struct{})}
	busesMutex.Lock()
	defer busesMutex.Unlock()
	if buses[bus] == nil {
		buses[bus] = map[*member]struct{}{}
	}
	buses[bus][m] = struct{}{}
	return m
}

func (m *member) ReadFrame(frame []byte) error {
	m.mutex.Lock()
	deadline := m.deadline
	m.mutex.Unlock()
	var timeout <-chan time.Time
	if !deadline.IsZero() {
		wait := time.Until(deadline)
		if wait <= 0 {
			select {
			case received := <-m.frames:
				copy(frame, received)
				return nil
			default:
				return os.ErrDeadlineExceeded
			}
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case received := <-m.frames:
		copy(frame, received)
		return nil
	case <-m.closed:
		return os.ErrClosed
	case <-timeout:
		return os.ErrDeadlineExceeded
	}
}

// WriteFrame hands the frame to every other member of the bus, synchronously, as plc4j's virtual bus does.
func (m *member) WriteFrame(frame []byte) error {
	select {
	case <-m.closed:
		return os.ErrClosed
	default:
	}
	copied := append([]byte(nil), frame[:can.FrameSize]...)
	busesMutex.Lock()
	defer busesMutex.Unlock()
	for other := range buses[m.bus] {
		if other == m {
			continue
		}
		for {
			select {
			case other.frames <- copied:
			default:
				// Full: drop the member's oldest frame and try again.
				select {
				case <-other.frames:
				default:
				}
				continue
			}
			break
		}
	}
	return nil
}

func (m *member) SetReadDeadline(deadline time.Time) error {
	m.mutex.Lock()
	m.deadline = deadline
	m.mutex.Unlock()
	return nil
}

func (m *member) Close() error {
	m.once.Do(func() {
		busesMutex.Lock()
		delete(buses[m.bus], m)
		if len(buses[m.bus]) == 0 {
			delete(buses, m.bus)
		}
		busesMutex.Unlock()
		close(m.closed)
	})
	return nil
}
