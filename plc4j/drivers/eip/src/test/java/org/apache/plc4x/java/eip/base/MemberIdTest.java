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
package org.apache.plc4x.java.eip.base;

import org.apache.plc4x.java.eip.base.tag.EipTag;
import org.apache.plc4x.java.eip.readwrite.LogicalSegment;
import org.apache.plc4x.java.spi.buffers.api.WithOption;
import org.apache.plc4x.java.spi.buffers.bytebased.WithByteBasedOption;
import org.apache.plc4x.java.spi.buffers.bytebased.WriteBufferByteBased;
import org.junit.jupiter.api.Test;

import java.util.HexFormat;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;

/**
 * An element index takes the member segment it fits - 8 bits up to 255, 16 up to 65535, 32 past that, after a pad
 * octet, little-endian - and a program-scoped tag's program is one symbol.
 */
class MemberIdTest {

    private static String bytesOf(long index, String byteOrder) throws Exception {
        LogicalSegment segment = new LogicalSegment(EipTcpConnection.memberId(index));
        WriteBufferByteBased buffer = new WriteBufferByteBased(new byte[segment.getLengthInBytes()],
            WithByteBasedOption.WithByteOrder(byteOrder), WithOption.WithUnsignedIntegerEncoding("unsigned-binary"));
        segment.serialize(buffer);
        return HexFormat.of().formatHex(buffer.getBytes());
    }

    private static String bytesOf(long index) throws Exception {
        return bytesOf(index, "LITTLE_ENDIAN");
    }

    @Test
    void theCipPathIsLittleEndianWhateverTheConnectionsByteOrder() throws Exception {
        // The connection's buffers are big-endian by default (big-endian=true); the path is little-endian all the same.
        assertEquals("910161002900" + "0001",
            HexFormat.of().formatHex(EipTcpConnection.cipPath(EipTag.of("a[256]:DINT").getPathElements())));
    }

    @Test
    void theMemberSegmentWidens() throws Exception {
        assertEquals("28ff", bytesOf(255));
        assertEquals("29000001", bytesOf(256));
        assertEquals("2900e803", bytesOf(1000));
        assertEquals("2a0070110100", bytesOf(70000));
    }

    @Test
    void aProgramIsOneSymbol() {
        EipTag tag = EipTag.of("Program:MainProgram.Level[300]:DINT");
        assertEquals(List.of(new EipTag.SymbolElement("Program:MainProgram"), new EipTag.SymbolElement("Level"),
            new EipTag.MemberElement(300)), tag.getPathElements());
    }
}
