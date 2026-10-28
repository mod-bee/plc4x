/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */
package org.apache.plc4x.java.knxnetip.readwrite.utils;

import org.apache.plc4x.java.api.value.PlcValue;
import org.apache.plc4x.java.knxnetip.readwrite.KnxDatapoint;
import org.apache.plc4x.java.knxnetip.readwrite.KnxDatapointType;
import org.apache.plc4x.java.spi.buffers.api.WithOption;
import org.apache.plc4x.java.spi.buffers.api.exceptions.BufferException;
import org.apache.plc4x.java.spi.buffers.bytebased.ReadBufferByteBased;
import org.apache.plc4x.java.spi.buffers.bytebased.WriteBufferByteBased;
import org.apache.plc4x.java.spi.values.PlcLREAL;
import org.junit.jupiter.api.Test;

import java.util.Arrays;

import static org.junit.jupiter.api.Assertions.*;

/**
 * An integer with a coefficient in the KNX master data is its scaled value (KNX Standard 3.7.2: 5.001 is
 * 0..255 for 0..100 %, 5.003 0..255 for 0..360 degrees, 8.010 hundredths of a percent). The payloads are
 * the group values as they are on the bus: the APDU's six-bit byte (empty) first, then the data.
 */
class StaticHelperTest {

    private static final WithOption[] OPTIONS = new WithOption[]{
        // As the driver reads the bus (KnxNetIpMessageCodec)
        WithOption.WithUnsignedIntegerEncoding("unsigned-binary"),
        WithOption.WithSignedIntegerEncoding("twos-complement"),
        WithOption.WithFloatEncoding("IEEE754")
    };

    private static PlcValue parse(KnxDatapointType type, byte... payload) throws BufferException {
        return KnxDatapoint.staticParse(new ReadBufferByteBased(payload, OPTIONS), type);
    }

    private static byte[] serialize(KnxDatapointType type, double value) throws BufferException {
        WriteBufferByteBased wb = new WriteBufferByteBased(new byte[8], OPTIONS);
        KnxDatapoint.staticSerialize(wb, new PlcLREAL(value), type);
        return Arrays.copyOf(wb.getBytes(), (wb.getPositionInBits() + 7) / 8);
    }

    @Test
    void scalingIsAPercentage() throws Exception {
        assertEquals(100.0, parse(KnxDatapointType.DPT_Scaling, (byte) 0x00, (byte) 0xFF).getDouble(), 0.001);
        assertEquals(50.196, parse(KnxDatapointType.DPT_Scaling, (byte) 0x00, (byte) 0x80).getDouble(), 0.001);
        assertArrayEquals(new byte[]{0x00, (byte) 0x80}, serialize(KnxDatapointType.DPT_Scaling, 50));
        assertArrayEquals(new byte[]{0x00, (byte) 0xFF}, serialize(KnxDatapointType.DPT_Scaling, 100));
        assertThrows(Exception.class, () -> serialize(KnxDatapointType.DPT_Scaling, 101));
    }

    @Test
    void angleIsDegrees() throws Exception {
        assertEquals(360.0, parse(KnxDatapointType.DPT_Angle, (byte) 0x00, (byte) 0xFF).getDouble(), 0.001);
        assertArrayEquals(new byte[]{0x00, (byte) 0xFF}, serialize(KnxDatapointType.DPT_Angle, 360));
    }

    @Test
    void signedPercentageIsInHundredths() throws Exception {
        assertEquals(-2.0, parse(KnxDatapointType.DPT_Percent_V16, (byte) 0x00, (byte) 0xFF, (byte) 0x38).getDouble(), 0.001);
        assertArrayEquals(new byte[]{0x00, (byte) 0xFF, (byte) 0x38}, serialize(KnxDatapointType.DPT_Percent_V16, -2));
    }
}
