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
package org.apache.plc4x.java.bacnetip.readwrite.utils;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;

/**
 * An enumerated (or other variable-length unsigned) payload is as many big-endian octets as its tag header announces:
 * zero is one octet 0x00, and a value with its top bit set carries no sign octet. Read back, the octets are unsigned.
 */
class StaticHelperVarUintTest {

    @Test
    void writesExactlyTheAnnouncedOctets() {
        assertArrayEquals(new byte[]{0x00}, StaticHelper.writeVarUint(0));
        assertArrayEquals(new byte[]{0x01}, StaticHelper.writeVarUint(1));
        assertArrayEquals(new byte[]{(byte) 0x80}, StaticHelper.writeVarUint(128));
        assertArrayEquals(new byte[]{(byte) 0xC8}, StaticHelper.writeVarUint(200));
        assertArrayEquals(new byte[]{0x01, 0x11, 0x70}, StaticHelper.writeVarUint(70000));
        assertArrayEquals(new byte[]{(byte) 0xFF, (byte) 0xFF, (byte) 0xFF, (byte) 0xFF}, StaticHelper.writeVarUint(0xFFFFFFFFL));
    }

    @Test
    void readsTheOctetsUnsigned() {
        assertEquals(200, StaticHelper.parseVarUint(new byte[]{(byte) 0xC8}));
        assertEquals(0xFFFFFFFFL, StaticHelper.parseVarUint(new byte[]{(byte) 0xFF, (byte) 0xFF, (byte) 0xFF, (byte) 0xFF}));
        assertEquals(70000, StaticHelper.parseVarUint(new byte[]{0x01, 0x11, 0x70}));
    }
}
