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

import (
	"context"
	"testing"
)

func TestStaticHelperSplitsTheRawId(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		raw                     uint32
		id                      int32
		extended, remote, error bool
	}{
		{0x00000123, 0x123, false, false, false},
		{0x80000123, 0x123, true, false, false},
		{0x98FEF100, 0x18FEF100, true, false, false},
		{0x40000123, 0x123, false, true, false},
		{0x20000004, 0x004, false, false, true},
		// A standard frame keeps only its 11 bits, whatever else the word holds.
		{0x00001FFF, 0x7FF, false, false, false},
	} {
		raw := int32(c.raw)
		if got := ReadIdentifier(ctx, raw); got != c.id {
			t.Errorf("0x%08X: identifier 0x%X, want 0x%X", c.raw, got, c.id)
		}
		if IsExtended(ctx, raw) != c.extended || IsRemote(ctx, raw) != c.remote || IsError(ctx, raw) != c.error {
			t.Errorf("0x%08X: flags wrong", c.raw)
		}
	}
}

// A struct can_frame as a little-endian Linux host lays it out parses into the mspec's model and back unchanged.
func TestSocketCANFrameRoundTrip(t *testing.T) {
	wire := []byte{0x00, 0xF1, 0xFE, 0x98, 3, 0, 0, 0, 0xAA, 0xBB, 0xCC, 0, 0, 0, 0, 0}
	frame, err := SocketCANFrameParse(context.Background(), wire)
	if err != nil {
		t.Fatal(err)
	}
	if frame.GetIdentifier() != 0x18FEF100 || !frame.GetExtended() || len(frame.GetData()) != 3 || frame.GetData()[2] != 0xCC {
		t.Fatalf("parsed %s", frame)
	}
	back, err := frame.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(wire) {
		t.Fatalf("serialized % X, want % X", back, wire)
	}
}
