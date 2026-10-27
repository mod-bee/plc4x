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
package org.apache.plc4x.java.eip.base;

import org.apache.plc4x.java.eip.readwrite.PathSegment;
import org.apache.plc4x.java.eip.readwrite.PortSegment;
import org.apache.plc4x.java.eip.readwrite.PortSegmentExtended;
import org.apache.plc4x.java.eip.readwrite.PortSegmentNormal;
import org.junit.jupiter.api.Test;

import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

class CommunicationPathTest {

    private static PortSegmentNormal normal(PathSegment segment) {
        return assertInstanceOf(PortSegmentNormal.class, ((PortSegment) segment).getSegmentType());
    }

    @Test
    void theDefaultRouteTakesTheBackplanePortAndSlot() {
        List<PathSegment> segments = CommunicationPath.portSegments(null, 3, 2);
        assertEquals(1, segments.size());
        assertEquals(3, normal(segments.get(0)).getPort());
        assertEquals(2, normal(segments.get(0)).getLinkAddress());
    }

    @Test
    void aNumericLinkIsANodeAndAnAddressIsExtended() {
        List<PathSegment> segments = CommunicationPath.portSegments("1,4,2,192.168.0.1,2,5", 1, 0);
        assertEquals(3, segments.size());
        assertEquals(4, normal(segments.get(0)).getLinkAddress());
        PortSegmentExtended extended = assertInstanceOf(PortSegmentExtended.class, ((PortSegment) segments.get(1)).getSegmentType());
        assertEquals(2, extended.getPort());
        assertEquals(11, extended.getLinkAddressSize());
        assertEquals(2, normal(segments.get(2)).getPort());
        assertEquals(5, normal(segments.get(2)).getLinkAddress());
    }

    @Test
    void noneIsNoRoute() {
        assertTrue(CommunicationPath.portSegments("none", 1, 0).isEmpty());
    }

    @Test
    void aPathThatMakesNoRouteIsRefused() {
        for (String path : new String[]{"1", "1,x", "0,1", "15,1", "2,300", "1,0,2"}) {
            assertThrows(IllegalArgumentException.class, () -> CommunicationPath.portSegments(path, 1, 0), path);
        }
    }
}
