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
package org.apache.plc4x.java.iec608705104;

import org.apache.plc4x.java.iec608705104.readwrite.APDU;
import org.apache.plc4x.java.iec608705104.readwrite.APDUIFormat;
import org.apache.plc4x.java.iec608705104.readwrite.ASDU;
import org.apache.plc4x.java.iec608705104.readwrite.InformationObjectWithoutTime_DOUBLE_POINT_INFORMATION;
import org.apache.plc4x.java.spi.buffers.bytebased.ReadBufferByteBased;
import org.junit.jupiter.api.Test;

import java.util.HexFormat;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * A sequence of objects (structure qualifier set) carries the first object's address only; the
 * objects after it are at the addresses after it. These are the bytes a lib60870 (c104) station sent
 * in answer to a general interrogation: two double points at ASDU 48, addresses 22 (OFF) and 23 (ON).
 */
class Iec60870SequenceOfObjectsTest {

    @Test
    void numbersTheObjectsOnFromTheFirstAddress() throws Exception {
        byte[] frame = HexFormat.of().parseHex("680f020002000382140030001600000102");
        APDU apdu = APDU.staticParse(new ReadBufferByteBased(frame));
        ASDU asdu = ((APDUIFormat) apdu).getAsdu();
        assertTrue(asdu.getStructureQualifier());
        assertEquals(2, asdu.getInformationObjects().size());
        for (int index = 0; index < 2; index++) {
            assertEquals(22 + index, Iec60870Connection.objectAddress(asdu, asdu.getInformationObjects().get(index), index));
            InformationObjectWithoutTime_DOUBLE_POINT_INFORMATION point =
                (InformationObjectWithoutTime_DOUBLE_POINT_INFORMATION) asdu.getInformationObjects().get(index);
            assertEquals(index + 1, point.getDiq().getDpiCode());
        }
    }
}
