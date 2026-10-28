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
package org.apache.plc4x.java.canopen.conversation;

import org.apache.plc4x.java.api.value.PlcValue;
import org.apache.plc4x.java.canopen.readwrite.CANOpenDataType;
import org.apache.plc4x.java.canopen.readwrite.CANOpenFrame;
import org.apache.plc4x.java.canopen.readwrite.CANOpenSDOResponse;
import org.apache.plc4x.java.canopen.readwrite.CANOpenService;
import org.apache.plc4x.java.canopen.readwrite.IndexAddress;
import org.apache.plc4x.java.canopen.readwrite.SDOInitiateExpeditedUnsizedUploadResponse;
import org.apache.plc4x.java.canopen.readwrite.SDOInitiateUploadResponse;
import org.apache.plc4x.java.canopen.readwrite.SDOResponseCommand;
import org.junit.jupiter.api.Test;

import java.time.Duration;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import java.util.function.Predicate;

import static org.junit.jupiter.api.Assertions.assertEquals;

class SDOUploadConversationTest {

    /**
     * CiA 301 7.2.4.3.6: an initiate upload response with e=1 and s=0 is expedited without the size indicated; its four
     * data bytes carry the value, and the object's type says how many of them it takes.
     */
    @Test
    void expeditedUploadWithoutTheSizeIndicatedReadsTheFourBytes() throws Exception {
        IndexAddress address = new IndexAddress(0x1017, (short) 0);
        CANOpenFrame answer = new CANOpenFrame((short) 1, CANOpenService.TRANSMIT_SDO,
            new CANOpenSDOResponse(SDOResponseCommand.INITIATE_UPLOAD,
                new SDOInitiateUploadResponse(true, false, address,
                    new SDOInitiateExpeditedUnsizedUploadResponse(new byte[]{(byte) 0xE8, 0x03, 0x00, 0x00}))));
        CANConversation conversation = new CANConversation() {
            @Override
            public void sendToWire(CANOpenFrame frame) {
            }

            @Override
            public CompletableFuture<CANOpenFrame> expect(Predicate<CANOpenFrame> predicate, Duration timeout) {
                return CompletableFuture.completedFuture(answer);
            }
        };

        CompletableFuture<PlcValue> value = new CompletableFuture<>();
        new SDOUploadConversation(conversation, 1, 1, address, CANOpenDataType.UNSIGNED16, Duration.ofSeconds(1))
            .execute(value);
        assertEquals(1000, value.get(1, TimeUnit.SECONDS).getInt());
    }

}
