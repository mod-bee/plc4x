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

package canopen

import (
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
)

// services are the CiA 301 services of the predefined connection set (7.3.5) the driver speaks, and the COB-IDs each
// takes: CANOpenService's min to max, as plc4j's codec walks the enum. SYNC (0x080) and EMCY (0x081 to 0x0FF) share a
// function code and are told apart by the node-ID.
var services = []readWriteModel.CANOpenService{
	readWriteModel.CANOpenService_NMT,
	readWriteModel.CANOpenService_SYNC,
	readWriteModel.CANOpenService_EMCY,
	readWriteModel.CANOpenService_TIME,
	readWriteModel.CANOpenService_TRANSMIT_PDO_1,
	readWriteModel.CANOpenService_RECEIVE_PDO_1,
	readWriteModel.CANOpenService_TRANSMIT_PDO_2,
	readWriteModel.CANOpenService_RECEIVE_PDO_2,
	readWriteModel.CANOpenService_TRANSMIT_PDO_3,
	readWriteModel.CANOpenService_RECEIVE_PDO_3,
	readWriteModel.CANOpenService_TRANSMIT_PDO_4,
	readWriteModel.CANOpenService_RECEIVE_PDO_4,
	readWriteModel.CANOpenService_TRANSMIT_SDO,
	readWriteModel.CANOpenService_RECEIVE_SDO,
	readWriteModel.CANOpenService_HEARTBEAT,
}

// resolve is the service and node-ID a COB-ID carries, if it is one the driver speaks.
func resolve(cobId uint16) (readWriteModel.CANOpenService, uint8, bool) {
	for _, service := range services {
		if cobId >= service.GetMin() && cobId <= service.GetMax() {
			return service, uint8(cobId - functionBase(service)), true
		}
	}
	return 0, 0, false
}

// cobIdOf is the COB-ID of service for node nodeId.
func cobIdOf(service readWriteModel.CANOpenService, nodeId uint8) uint16 {
	return functionBase(service) + uint16(nodeId)
}

// functionBase is the COB-ID of service's function code with node-ID 0: a COB-ID is the function code (its top four
// bits) and the node-ID (its low seven), CiA 301 7.3.3. plc4j's codec takes min as that base, which is the same for
// every service but EMCY, whose range starts at 0x081 as node-ID 0 would be SYNC; Go masks the node-ID bits off min.
func functionBase(service readWriteModel.CANOpenService) uint16 {
	return service.GetMin() &^ 0x7F
}
