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
	"slices"

	"github.com/rs/zerolog"

	socketcanModel "github.com/apache/plc4x/plc4go/protocols/socketcan/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi"
	_default "github.com/apache/plc4x/plc4go/spi/default"
	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/options"
	"github.com/apache/plc4x/plc4go/spi/transports"
	"github.com/apache/plc4x/plc4go/spi/transports/can"
	"github.com/apache/plc4x/plc4go/spi/utils"
)

// MessageCodec moves SocketCANFrames (the socketcan mspec's model of struct can_frame), which is what the CAN
// transports' stream is made of. plc4j's GenericCANMessageCodec picks its frames apart by hand; Go has the generated
// model for it.
type MessageCodec struct {
	_default.DefaultCodec

	log zerolog.Logger
}

func NewMessageCodec(transportInstance transports.TransportInstance, _options ...options.WithOption) *MessageCodec {
	codec := &MessageCodec{log: options.ExtractCustomLoggerOrDefaultToGlobal(_options...)}
	// Every frame is unsolicited: no request ever waits on an answer. A custom handler that declines every message keeps
	// the receive worker polling with no expectation outstanding, and leaves the frames to the connection's worker.
	codec.DefaultCodec = _default.NewDefaultCodec(codec, transportInstance,
		append(slices.Clip(_options), _default.WithCustomMessageHandler(declineMessage))...)
	return codec
}

func declineMessage(_ context.Context, _ _default.DefaultCodecRequirements, _ spi.Message) bool {
	return false
}

func (m *MessageCodec) GetCodec() spi.MessageCodec {
	return m
}

func (m *MessageCodec) Send(ctx context.Context, interactionInfo string, message spi.Message) error {
	frame, ok := message.(socketcanModel.SocketCANFrame)
	if !ok {
		return errors.Errorf("a CAN codec sends SocketCANFrames, not %T", message)
	}
	theBytes, err := frame.Serialize()
	if err != nil {
		return errors.Wrapf(err, "error serializing %s", interactionInfo)
	}
	if err := m.GetTransportInstance().Write(ctx, theBytes); err != nil {
		return errors.Wrapf(err, "error sending %s", interactionInfo)
	}
	return nil
}

// Receive takes the next whole frame off the stream, or nothing when none is there.
func (m *MessageCodec) Receive(ctx context.Context) (spi.Message, error) {
	transportInstance := m.GetTransportInstance()
	if !transportInstance.IsConnected() {
		return nil, errors.New("Transport instance not connected")
	}
	available, err := transportInstance.GetNumBytesAvailableInBuffer()
	if err != nil || available < can.FrameSize {
		return nil, nil
	}
	theBytes, err := transportInstance.Read(ctx, can.FrameSize)
	if err != nil {
		return nil, errors.Wrap(err, "error reading a CAN frame")
	}
	frame, err := socketcanModel.SocketCANFrameParse(ctx, theBytes)
	if err != nil {
		// The transports only ever hand over whole frames, so one that does not parse is dropped by itself.
		m.log.Debug().Err(err).Stringer("frame", utils.Base64Stringer(theBytes)).Msg("error parsing a CAN frame")
		return nil, nil
	}
	return frame, nil
}
