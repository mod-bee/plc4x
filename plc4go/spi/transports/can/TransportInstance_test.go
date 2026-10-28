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

package can

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apache/plc4x/plc4go/spi/transports"
)

// failing is an endpoint whose next read fails with err (once; then it reads nothing, as a socket does).
type failing struct {
	err error
}

func (f *failing) ReadFrame([]byte) error {
	if err := f.err; err != nil {
		f.err = nil
		return err
	}
	return os.ErrDeadlineExceeded
}
func (f *failing) WriteFrame([]byte) error         { return nil }
func (f *failing) SetReadDeadline(time.Time) error { return nil }
func (f *failing) Close() error                    { return nil }

// An interface that goes away reports it once; the poll passes that on, and it is fatal, so the codec stops instead of
// polling a dead socket for ever.
func TestAnEndpointFailureReachesThePoll(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.ENODEV, syscall.ENETDOWN} {
		instance := NewTransportInstance("fake", func() (FrameConn, error) { return &failing{err: errno}, nil })
		require.NoError(t, instance.Connect(context.Background()))
		_, err := instance.GetNumBytesAvailableInBuffer()
		require.ErrorIs(t, err, errno)
		assert.Equal(t, transports.TransportErrorFatal, instance.ClassifyError(err))
		available, err := instance.GetNumBytesAvailableInBuffer()
		require.NoError(t, err, "a quiet poll is no error")
		assert.Zero(t, available)
	}
}
