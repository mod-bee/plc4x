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
import org.apache.plc4x.java.spi.buffers.api.ReadBuffer;
import org.apache.plc4x.java.spi.buffers.api.WithOption;
import org.apache.plc4x.java.spi.buffers.api.WriteBuffer;
import org.apache.plc4x.java.spi.buffers.api.exceptions.BufferException;

/**
 * Helpers the knxnetip model calls (STATIC_CALL) for what its mspec can't express itself.
 */
public class StaticHelper {

    private StaticHelper() {
    }

    /**
     * A KNX integer with a coefficient (KNX master data), read as its scaled value: 5.001 is 0..255 for
     * 0..100 %, 5.003 0..255 for 0..360 degrees. The value is raw * fullScale / largest, largest being the
     * largest raw value (255 for 8 bits unsigned), so the ends of the range come out exact.
     */
    public static float parseKnxScaled(ReadBuffer readBuffer, int width, boolean signed, double fullScale, double largest) throws BufferException {
        long raw = readBuffer.readUnsignedLong(width, WithOption.WithName("value"));
        if (signed && raw >= (1L << (width - 1))) {
            // Two's complement of the field's own width
            raw -= 1L << width;
        }
        return (float) (raw * fullScale / largest);
    }

    /**
     * The inverse of {@link #parseKnxScaled}: the nearest raw value, refused when the value is outside the
     * datapoint type's range.
     */
    public static void serializeKnxScaled(WriteBuffer writeBuffer, PlcValue value, int width, boolean signed, double fullScale, double largest) throws BufferException {
        double scaled = value.getDouble();
        long raw = Math.round(scaled * largest / fullScale);
        long min = signed ? -(1L << (width - 1)) : 0;
        long max = signed ? (1L << (width - 1)) - 1 : (1L << width) - 1;
        if (Double.isNaN(scaled) || raw < min || raw > max) {
            throw new BufferException(scaled + " is outside the range of this datapoint type (" +
                (min * fullScale / largest) + " to " + (max * fullScale / largest) + ")");
        }
        if (raw < 0) {
            // Two's complement of the field's own width
            raw += 1L << width;
        }
        writeBuffer.writeUnsignedLong(width, raw, WithOption.WithName("value"));
    }

}
