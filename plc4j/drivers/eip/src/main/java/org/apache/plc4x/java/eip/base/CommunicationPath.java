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

import java.util.ArrayList;
import java.util.List;
import java.util.regex.Pattern;

/**
 * A communication path in the Logix form: pairs of a port and its link, e.g. "1,4,2,192.168.0.1,1,0" - backplane slot
 * 4 (an Ethernet module), out of its port 2 to 192.168.0.1, then that chassis' slot 0. A port is 1-14: 1 the
 * backplane, 2 (or 3, ...) a module's network port. A link is a slot or node number (0-255: a backplane slot, a
 * ControlNet or DH+ node), carried as a normal port segment, or an IPv4 address, carried as an extended one.
 * {@value #NONE} is no route at all, for a controller reached on its own port (a Micro800).
 */
public final class CommunicationPath {

    public static final String NONE = "none";

    private static final Pattern IPV4 = Pattern.compile("^\\d{1,3}(\\.\\d{1,3}){3}$");

    private CommunicationPath() {
    }

    /**
     * The port segments of a route: those of the communication path when one is given, none for {@value #NONE}, and
     * the configured backplane port and slot otherwise.
     *
     * @throws IllegalArgumentException for a path that does not make a route
     */
    public static List<PathSegment> portSegments(String path, int backplane, int slot) {
        List<PathSegment> segments = new ArrayList<>();
        if (path == null || path.isBlank()) {
            segments.add(new PortSegment(new PortSegmentNormal((byte) backplane, (short) slot)));
            return segments;
        }
        if (NONE.equalsIgnoreCase(path.trim())) {
            return segments;
        }
        String[] parts = path.split(",");
        if (parts.length % 2 != 0) {
            throw new IllegalArgumentException("communication path \"" + path + "\": pairs of a port and its link, e.g. 1,0");
        }
        for (int i = 0; i < parts.length; i += 2) {
            String portText = parts[i].trim();
            String linkText = parts[i + 1].trim();
            int port;
            try {
                port = Integer.parseInt(portText);
            } catch (NumberFormatException e) {
                port = -1;
            }
            if (port < 1 || port > 14) {
                throw new IllegalArgumentException("communication path \"" + path + "\": port \"" + portText + "\" is not 1-14");
            }
            if (IPV4.matcher(linkText).matches()) {
                String address = linkText.length() % 2 != 0 ? linkText + "\0" : linkText;
                segments.add(new PortSegment(new PortSegmentExtended((byte) port, (short) linkText.length(), address)));
                continue;
            }
            int link;
            try {
                link = Integer.parseInt(linkText);
            } catch (NumberFormatException e) {
                link = -1;
            }
            if (link < 0 || link > 255) {
                throw new IllegalArgumentException("communication path \"" + path + "\": link \"" + linkText
                    + "\" is not a slot or node number (0-255) or an IPv4 address");
            }
            segments.add(new PortSegment(new PortSegmentNormal((byte) port, (short) link)));
        }
        return segments;
    }
}
