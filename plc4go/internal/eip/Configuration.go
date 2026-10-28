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

package eip

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/apache/plc4x/plc4go/spi/errors"
	spiOptions "github.com/apache/plc4x/plc4go/spi/options"
)

type Configuration struct {
	backplane                 int8
	slot                      int8
	bigEndian                 bool
	forceUnconnectedOperation bool
	communicationPath         string
	connectionSerialNumber    uint16 // 0 = pick a random one per connection (test support option)
}

func ParseFromOptions(localLogger zerolog.Logger, options map[string][]string) (Configuration, error) {
	// Every option this driver reads goes through the reader, so the ones nothing read can be
	// reported rather than silently discarded. Deferred, so no return path can skip it.
	reader := spiOptions.NewOptionReader(localLogger, options)
	defer reader.ReportUnknown("eip")

	configuration := Configuration{
		backplane: 1,
		slot:      0,
		bigEndian: true,
	}
	if localRackString := reader.Get("backplane"); localRackString != "" {
		parsedBackplane, err := strconv.ParseInt(localRackString, 10, 8)
		if err != nil {
			return Configuration{}, errors.Wrap(err, "Error parsing backplane")
		}
		configuration.backplane = int8(parsedBackplane)
	}
	if localSlotString := reader.Get("slot"); localSlotString != "" {
		parsedSlot, err := strconv.ParseInt(localSlotString, 10, 8)
		if err != nil {
			return Configuration{}, errors.Wrap(err, "Error parsing slot")
		}
		configuration.slot = int8(parsedSlot)
	}
	if bigEndianString := getFromOptionsAliases(reader, "bigEndian", "big-endian"); bigEndianString != "" {
		parsedBigEndian, err := strconv.ParseBool(bigEndianString)
		if err != nil {
			return Configuration{}, errors.Wrap(err, "Error parsing bigEndian")
		}
		configuration.bigEndian = parsedBigEndian
	}
	if forceUnconnectedString := getFromOptionsAliases(reader, "forceUnconnectedOperation", "force-unconnected-operation"); forceUnconnectedString != "" {
		parsedForceUnconnected, err := strconv.ParseBool(forceUnconnectedString)
		if err != nil {
			return Configuration{}, errors.Wrap(err, "Error parsing forceUnconnectedOperation")
		}
		configuration.forceUnconnectedOperation = parsedForceUnconnected
	}
	configuration.communicationPath = getFromOptionsAliases(reader, "communicationPath", "communication-path")
	if _, err := parseCommunicationPath(configuration.communicationPath); err != nil {
		return Configuration{}, err
	}
	if serialNumberString := getFromOptionsAliases(reader, "connectionSerialNumber", "connection-serial-number"); serialNumberString != "" {
		parsedSerialNumber, err := strconv.ParseUint(serialNumberString, 10, 16)
		if err != nil {
			return Configuration{}, errors.Wrap(err, "Error parsing connectionSerialNumber")
		}
		configuration.connectionSerialNumber = uint16(parsedSerialNumber)
	}
	return configuration, nil
}

func getFromOptionsAliases(reader *spiOptions.OptionReader, keys ...string) string {
	for _, key := range keys {
		if value := reader.Get(key); value != "" {
			return value
		}
	}
	return ""
}

// communicationPathNone is the communication path of a controller reached on its own port, with no route at all
// (a Micro800): the connection path is then the Message Router alone.
const communicationPathNone = "none"

// routeHop is one hop of a communication path: a port, and the link on it - a slot or node number, or an IP address.
type routeHop struct {
	port    uint8
	link    uint8
	address string
}

// ipv4Link is a link given as an IPv4 address (the Ethernet port of a communication module).
var ipv4Link = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)

// parseCommunicationPath reads a communication path in the Logix form: pairs of a port and its link, e.g.
// "1,4,2,192.168.0.1,1,0" - backplane slot 4 (an Ethernet module), out of its port 2 to 192.168.0.1, then that
// chassis' slot 0. A port is 1-14: 1 the backplane, 2 (or 3, ...) a module's network port. A link is a slot or node
// number (0-255: a backplane slot, a ControlNet or DH+ node) or an IPv4 address. "none" is no route (nil hops, no
// error); an empty path is no path given (nil hops, no error).
func parseCommunicationPath(path string) ([]routeHop, error) {
	path = strings.TrimSpace(path)
	if path == "" || strings.EqualFold(path, communicationPathNone) {
		return nil, nil
	}
	parts := strings.Split(path, ",")
	if len(parts)%2 != 0 {
		return nil, errors.Errorf("communication path %q: pairs of a port and its link, e.g. 1,0", path)
	}
	hops := make([]routeHop, 0, len(parts)/2)
	for i := 0; i < len(parts); i += 2 {
		portText, linkText := strings.TrimSpace(parts[i]), strings.TrimSpace(parts[i+1])
		port, err := strconv.ParseUint(portText, 10, 8)
		if err != nil || port < 1 || port > 14 {
			return nil, errors.Errorf("communication path %q: port %q is not 1-14", path, portText)
		}
		if ipv4Link.MatchString(linkText) {
			hops = append(hops, routeHop{port: uint8(port), address: linkText})
			continue
		}
		link, err := strconv.ParseUint(linkText, 10, 8)
		if err != nil {
			return nil, errors.Errorf("communication path %q: link %q is not a slot or node number (0-255) or an IPv4 address", path, linkText)
		}
		hops = append(hops, routeHop{port: uint8(port), link: uint8(link)})
	}
	return hops, nil
}
