//go:build !linux

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
	"runtime"

	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/transports/can"
)

// open fails: SocketCAN is the Linux kernel's CAN stack (AF_CAN sockets), which no other operating system has. plc4j
// refuses the same way, at the transport instance.
func open(name string) (can.FrameConn, error) {
	return nil, errors.Errorf("SocketCAN is Linux-only: %s cannot open %q on %s (can-virtualcan is an in-process bus that works everywhere)", TransportCode, name, runtime.GOOS)
}
