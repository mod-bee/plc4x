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

// Package can is what the CAN transports (can-socketcan, can-virtualcan) share: a byte-stream TransportInstance over an
// endpoint that moves whole CAN frames.
//
// The stream is a sequence of 16-byte frames in the layout of the socketcan mspec's SocketCANFrame, which is Linux's
// struct can_frame: the raw can_id as a little-endian 32-bit word (with CAN_EFF_FLAG, CAN_RTR_FLAG and CAN_ERR_FLAG in
// its top bits), the data length, three reserved bytes and eight data bytes. A driver reads it with the generated
// model, and writes it the same way.
//
// plc4j's CAN transports hand their codecs a 16-byte frame of their own instead (a big-endian identifier without
// flags); Go uses the mspec's layout so the frame is parsed by generated code rather than by hand, and so an extended
// identifier is told from a standard one by its flag rather than by its value.
package can

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/transports"
	transportUtils "github.com/apache/plc4x/plc4go/spi/transports/utils"
)

// FrameSize is the length of one frame on the stream (sizeof(struct can_frame)).
const FrameSize = 16

// MaxDataLength is the payload a classic CAN frame carries.
const MaxDataLength = 8

// The flags in the top bits of the raw identifier (linux/can.h).
const (
	EffFlag = uint32(0x80000000)
	RtrFlag = uint32(0x40000000)
	ErrFlag = uint32(0x20000000)
	SffMask = uint32(0x000007FF)
	EffMask = uint32(0x1FFFFFFF)
)

// FrameConn is one open CAN endpoint. Every frame it reads or writes is FrameSize bytes in the stream's layout.
type FrameConn interface {
	// ReadFrame blocks until a frame arrives, the read deadline passes (os.ErrDeadlineExceeded) or the endpoint
	// closes, and fills frame with it.
	ReadFrame(frame []byte) error
	// WriteFrame puts one frame on the bus.
	WriteFrame(frame []byte) error
	// SetReadDeadline bounds the ReadFrame calls that follow; the zero time removes the bound.
	SetReadDeadline(deadline time.Time) error
	io.Closer
}

// Opener opens the endpoint when the transport instance connects.
type Opener func() (FrameConn, error)

// EncodeFrame lays out one frame in the stream's layout.
func EncodeFrame(rawId uint32, data []byte) ([]byte, error) {
	if len(data) > MaxDataLength {
		return nil, errors.Errorf("a CAN frame carries at most %d data bytes, not %d", MaxDataLength, len(data))
	}
	frame := make([]byte, FrameSize)
	binary.LittleEndian.PutUint32(frame, rawId)
	frame[4] = byte(len(data))
	copy(frame[8:], data)
	return frame, nil
}

// TransportInstance is a byte-stream transports.TransportInstance over a FrameConn.
type TransportInstance struct {
	name string
	open Opener

	conn         FrameConn
	reader       *bufio.Reader
	connected    atomic.Bool
	stateMutex   sync.RWMutex
	writeMutex   sync.Mutex
	maxFrameSize uint32

	log zerolog.Logger
}

var _ transports.TransportInstance = (*TransportInstance)(nil)

// NewTransportInstance makes an instance which opens its endpoint with open on Connect; name is what String says.
func NewTransportInstance(name string, open Opener, _options ...options.WithOption) *TransportInstance {
	return &TransportInstance{
		name:         name,
		open:         open,
		maxFrameSize: transportUtils.ExtractMaxFrameSize(_options...),
		log:          options.ExtractCustomLoggerOrDefaultToGlobal(_options...),
	}
}

func (m *TransportInstance) Connect(_ context.Context) error {
	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()
	if m.connected.Load() {
		return errors.New("already connected")
	}
	conn, err := m.open()
	if err != nil {
		return errors.Wrapf(err, "error opening %s", m.name)
	}
	m.conn = conn
	// Each frame is one read of the endpoint; the buffered reader turns them into the byte stream the codecs peek at.
	m.reader = bufio.NewReaderSize(&frameReader{conn: conn}, 64*FrameSize)
	m.connected.Store(true)
	return nil
}

func (m *TransportInstance) Close() error {
	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()
	if !m.connected.Swap(false) {
		return nil
	}
	if err := m.conn.Close(); err != nil {
		return errors.Wrapf(err, "error closing %s", m.name)
	}
	return nil
}

func (m *TransportInstance) IsConnected() bool {
	return m.connected.Load()
}

// Reset is a no-op: CAN is frame-framed, so nothing buffered can be half a message.
func (m *TransportInstance) Reset() {}

func (m *TransportInstance) GetNumBytesAvailableInBuffer() (uint32, error) {
	if !m.IsConnected() {
		return 0, errors.New("working on a unconnected connection")
	}
	// A short deadline for this poll, as the UDP transport does: a deadline a request set earlier may have passed, and
	// none at all would block the codec's receive worker until the bus speaks.
	if m.reader.Buffered() == 0 {
		_ = m.conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		_, _ = m.reader.Peek(1)
	}
	return uint32(m.reader.Buffered()), nil
}

func (m *TransportInstance) FillBuffer(ctx context.Context, until func(pos uint, currentByte byte, reader transports.ExtendedReader) (keepGoing bool)) error {
	if !m.IsConnected() {
		return errors.New("working on a unconnected connection")
	}
	nBytes := uint32(1)
	for ctx.Err() == nil {
		bytes, err := m.PeekReadableBytes(ctx, nBytes)
		if err != nil {
			return errors.Wrap(err, "Error while peeking")
		}
		if keepGoing := until(uint(nBytes-1), bytes[len(bytes)-1], m.reader); !keepGoing {
			return nil
		}
		nBytes++
	}
	return errors.Wrap(ctx.Err(), "Timeout while filling buffer")
}

func (m *TransportInstance) setDeadline(ctx context.Context) error {
	deadline, _ := ctx.Deadline()
	return m.conn.SetReadDeadline(deadline)
}

func (m *TransportInstance) PeekReadableBytes(ctx context.Context, numBytes uint32) ([]byte, error) {
	if !m.IsConnected() {
		return nil, errors.New("working on a unconnected connection")
	}
	if err := m.setDeadline(ctx); err != nil {
		return nil, errors.Wrap(err, "error setting read deadline")
	}
	return m.reader.Peek(int(numBytes))
}

func (m *TransportInstance) Read(ctx context.Context, numBytes uint32) ([]byte, error) {
	if !m.IsConnected() {
		return nil, errors.New("working on a unconnected connection")
	}
	maxFrameSize := m.maxFrameSize
	if maxFrameSize == 0 {
		maxFrameSize = transportUtils.DefaultMaxFrameSize
	}
	if numBytes > maxFrameSize {
		return nil, errors.Errorf("requested %d bytes exceeds the maximum frame size of %d bytes", numBytes, maxFrameSize)
	}
	if err := m.setDeadline(ctx); err != nil {
		return nil, errors.Wrap(err, "error setting read deadline")
	}
	data := make([]byte, numBytes)
	if _, err := io.ReadFull(m.reader, data); err != nil {
		return nil, errors.Wrap(err, "error reading")
	}
	return data, nil
}

// Write puts each FrameSize bytes of data on the bus as one frame.
func (m *TransportInstance) Write(_ context.Context, data []byte) error {
	if !m.IsConnected() {
		return errors.New("working on a unconnected connection")
	}
	if len(data) == 0 || len(data)%FrameSize != 0 {
		return errors.Errorf("a CAN transport writes whole %d-byte frames, not %d bytes", FrameSize, len(data))
	}
	m.writeMutex.Lock()
	defer m.writeMutex.Unlock()
	for offset := 0; offset < len(data); offset += FrameSize {
		frame := data[offset : offset+FrameSize]
		if frame[4] > MaxDataLength {
			return errors.Errorf("a CAN frame carries at most %d data bytes, not %d", MaxDataLength, frame[4])
		}
		if err := m.conn.WriteFrame(frame); err != nil {
			return errors.Wrapf(err, "error writing to %s", m.name)
		}
	}
	return nil
}

func (m *TransportInstance) String() string {
	return m.name
}

func (m *TransportInstance) ClassifyError(err error) transports.TransportErrorKind {
	if err == nil {
		return transports.TransportErrorUnknown
	}
	if transports.ErrorIs(err, os.ErrDeadlineExceeded) {
		return transports.TransportErrorRetryable
	}
	if transports.ErrorIs(err, io.EOF) || transports.ErrorIs(err, os.ErrClosed) || transports.ErrorIs(err, net.ErrClosed) {
		return transports.TransportErrorFatal
	}
	if transports.IsTransientSyscallError(err) {
		return transports.TransportErrorTransient
	}
	var errno syscall.Errno
	if transports.ErrorAs(err, &errno) && errno == syscall.ETIMEDOUT {
		return transports.TransportErrorRetryable
	}
	// ENETDOWN (the interface was taken down), ENODEV (it went away) and anything else: the endpoint is gone.
	return transports.TransportErrorFatal
}

// frameReader turns an endpoint's frames into a stream of bytes.
type frameReader struct {
	conn    FrameConn
	pending []byte
	frame   [FrameSize]byte
}

func (r *frameReader) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		if err := r.conn.ReadFrame(r.frame[:]); err != nil {
			return 0, err
		}
		r.pending = r.frame[:]
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *frameReader) String() string {
	return fmt.Sprintf("frameReader(%d pending)", len(r.pending))
}
