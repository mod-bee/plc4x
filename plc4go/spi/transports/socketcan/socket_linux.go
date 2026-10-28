//go:build linux

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

package socketcan

import (
	"encoding/binary"
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"

	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/transports/can"
)

// open binds a raw CAN socket to the interface. The socket is non-blocking and handed to the runtime's poller
// (os.NewFile), so reads honour deadlines and closing the file wakes a blocked read.
func open(name string) (can.FrameConn, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, errors.Wrapf(err, "no CAN interface %q", name)
	}
	fd, err := unix.Socket(unix.AF_CAN, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, unix.CAN_RAW)
	if err != nil {
		return nil, errors.Wrap(err, "error opening a CAN_RAW socket (is the can module loaded?)")
	}
	if err := unix.Bind(fd, &unix.SockaddrCAN{Ifindex: iface.Index}); err != nil {
		_ = unix.Close(fd)
		return nil, errors.Wrapf(err, "error binding to CAN interface %q", name)
	}
	return &socket{file: os.NewFile(uintptr(fd), TransportCode+":"+name)}, nil
}

// socket is one raw CAN socket. The kernel's struct can_frame holds can_id in host order; the stream's layout is
// little-endian, so the identifier word is converted on its way in and out (a no-op on little-endian hosts).
type socket struct {
	file *os.File
}

func (s *socket) ReadFrame(frame []byte) error {
	n, err := s.file.Read(frame[:can.FrameSize])
	if err != nil {
		return err
	}
	if n != can.FrameSize {
		return errors.Errorf("read a %d-byte CAN frame, expected %d", n, can.FrameSize)
	}
	binary.LittleEndian.PutUint32(frame, binary.NativeEndian.Uint32(frame))
	return nil
}

func (s *socket) WriteFrame(frame []byte) error {
	native := make([]byte, can.FrameSize)
	copy(native, frame)
	binary.NativeEndian.PutUint32(native, binary.LittleEndian.Uint32(frame))
	n, err := s.file.Write(native)
	if err != nil {
		return err
	}
	if n != can.FrameSize {
		return errors.Errorf("wrote %d bytes of a %d-byte CAN frame", n, can.FrameSize)
	}
	return nil
}

func (s *socket) SetReadDeadline(deadline time.Time) error {
	return s.file.SetReadDeadline(deadline)
}

func (s *socket) Close() error {
	return s.file.Close()
}
