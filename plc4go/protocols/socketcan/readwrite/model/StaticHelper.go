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

package model

import "context"

// The flags Linux keeps in the top three bits of a struct can_frame's can_id (linux/can.h). The mspec reads that
// word as rawId and leaves splitting it to these helpers, which generation only calls.
const (
	canEffFlag = uint32(0x80000000) // CAN_EFF_FLAG: a 29-bit (extended) identifier
	canRtrFlag = uint32(0x40000000) // CAN_RTR_FLAG: a remote transmission request
	canErrFlag = uint32(0x20000000) // CAN_ERR_FLAG: an error message frame
	canSffMask = uint32(0x000007FF) // CAN_SFF_MASK
	canEffMask = uint32(0x1FFFFFFF) // CAN_EFF_MASK
)

// ReadIdentifier is the frame's identifier without its flags: 29 bits for an extended frame, 11 otherwise.
func ReadIdentifier(_ context.Context, rawId int32) int32 {
	if uint32(rawId)&canEffFlag != 0 {
		return int32(uint32(rawId) & canEffMask)
	}
	return int32(uint32(rawId) & canSffMask)
}

// IsExtended says whether the frame carries a 29-bit identifier.
func IsExtended(_ context.Context, rawId int32) bool {
	return uint32(rawId)&canEffFlag != 0
}

// IsRemote says whether the frame is a remote transmission request.
func IsRemote(_ context.Context, rawId int32) bool {
	return uint32(rawId)&canRtrFlag != 0
}

// IsError says whether the frame is an error message frame.
func IsError(_ context.Context, rawId int32) bool {
	return uint32(rawId)&canErrFlag != 0
}
