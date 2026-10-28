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
package org.apache.plc4x.java.canopen;

import org.apache.plc4x.java.canopen.readwrite.CANOpenEmergencyPayload;
import org.apache.plc4x.java.canopen.readwrite.CANOpenFrame;
import org.apache.plc4x.java.canopen.readwrite.CANOpenService;
import org.apache.plc4x.java.canopen.readwrite.CANOpenSyncPayload;
import org.apache.plc4x.java.spi.buffers.bytebased.ReadBufferByteBased;
import org.apache.plc4x.java.spi.transports.api.TransportInstance;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;
import org.mockito.Mockito;

import java.util.List;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertNull;

/**
 * CiA 301 7.3.5: SYNC is 0x080, the EMCY of node n 0x080 + n. CANOpenService gives EMCY the value 0b1101 only because
 * an enum needs one per constant; these tests pin that the value never reaches the bus, where 0b1101 would be COB-ID
 * 0x680 + n.
 */
class CANOpenMessageCodecTest {

    @Test
    void theNodeIdIsTheLowSevenBitsOfTheCobId() {
        assertEquals(0x080, CANOpenMessageCodec.functionBase(CANOpenService.SYNC));
        assertEquals(0x080, CANOpenMessageCodec.functionBase(CANOpenService.EMCY));
        assertEquals(0x180, CANOpenMessageCodec.functionBase(CANOpenService.TRANSMIT_PDO_1));
        assertEquals(0x700, CANOpenMessageCodec.functionBase(CANOpenService.HEARTBEAT));
    }

    @Test
    void emcyAndSyncAreSentAtTheirCobIds() throws Exception {
        TransportInstance<?> transport = Mockito.mock(TransportInstance.class);
        CANOpenMessageCodec codec = new CANOpenMessageCodec(transport, frame -> {
        });
        codec.sendFrame(new CANOpenFrame((short) 5, CANOpenService.EMCY,
            new CANOpenEmergencyPayload(0x2310, (short) 0x02, new byte[]{1, 2, 3, 4, 5})));
        codec.sendFrame(new CANOpenFrame((short) 0, CANOpenService.SYNC, new CANOpenSyncPayload()));

        ArgumentCaptor<byte[]> written = ArgumentCaptor.forClass(byte[].class);
        Mockito.verify(transport, Mockito.times(2)).write(written.capture());
        List<byte[]> frames = written.getAllValues();
        assertEquals(0x085, cobId(frames.get(0)));
        assertEquals(8, frames.get(0)[4]);
        assertArrayEquals(new byte[]{0x10, 0x23, 0x02, 1, 2, 3, 4, 5}, java.util.Arrays.copyOfRange(frames.get(0), 8, 16));
        assertEquals(0x080, cobId(frames.get(1)));
        assertEquals(0, frames.get(1)[4]);
    }

    @Test
    void emcyAndSyncAreReadFromTheirCobIds() throws Exception {
        CANOpenMessageCodec codec = new CANOpenMessageCodec(Mockito.mock(TransportInstance.class), frame -> {
        });

        CANOpenFrame emcy = codec.parseMessage(frame(0x085, 0x10, 0x23, 0x02, 1, 2, 3, 4, 5));
        assertEquals(CANOpenService.EMCY, emcy.getService());
        assertEquals(5, emcy.getNodeId());
        assertEquals(0x2310, ((CANOpenEmergencyPayload) emcy.getPayload()).getErrorCode());

        CANOpenFrame sync = codec.parseMessage(frame(0x080));
        assertEquals(CANOpenService.SYNC, sync.getService());
        assertEquals(0, sync.getNodeId());
        assertInstanceOf(CANOpenSyncPayload.class, sync.getPayload());

        // 0x685 is what 0b1101 would make of node 5: no predefined service, which the codec passes over.
        assertNull(codec.parseMessage(frame(0x685, 0x10, 0x23, 0x02, 1, 2, 3, 4, 5)));
    }

    private static int cobId(byte[] frame) {
        return ((frame[0] & 0xFF) << 24 | (frame[1] & 0xFF) << 16 | (frame[2] & 0xFF) << 8 | (frame[3] & 0xFF)) & 0x7FF;
    }

    private static ReadBufferByteBased frame(int cobId, int... data) {
        byte[] bytes = new byte[16];
        bytes[0] = (byte) (cobId >> 24);
        bytes[1] = (byte) (cobId >> 16);
        bytes[2] = (byte) (cobId >> 8);
        bytes[3] = (byte) cobId;
        bytes[4] = (byte) data.length;
        for (int i = 0; i < data.length; i++) {
            bytes[8 + i] = (byte) data[i];
        }
        return new ReadBufferByteBased(bytes, CANOpenConnection.LITTLE_ENDIAN_OPTIONS);
    }

}
