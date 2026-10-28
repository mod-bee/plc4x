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
package org.apache.plc4x.java.slmp.config;

import org.apache.plc4x.java.spi.config.Configuration;
import org.apache.plc4x.java.spi.config.annotations.ConfigurationParameter;
import org.apache.plc4x.java.spi.config.annotations.Description;
import org.apache.plc4x.java.spi.config.annotations.defaults.IntDefaultValue;

public class SlmpConfiguration implements Configuration {

    // No "-ms": this is not a duration in milliseconds but a field of the 3E request frame, in
    // SLMP's own units, where 0 means "wait infinitely". The name is the protocol's. Renaming it
    // to monitoring-timer-ms would state a unit it does not have.
    @ConfigurationParameter("monitoring-timer")
    @IntDefaultValue(0x0000)
    @Description("SLMP monitoring timer written into each 3E request frame (0 = wait infinitely).")
    private int monitoringTimer;

    @ConfigurationParameter("request-timeout-ms")
    @IntDefaultValue(5_000)
    @Description("Client-side timeout in milliseconds awaiting a response.")
    private int requestTimeout;

    // The 3E frame's access route (SH-080008 chapter 6). The defaults address the connected station
    // itself: network 0, station 0xFF, module I/O 0x03FF, multidrop station 0. The initializers
    // say the same for a configuration built in code rather than from a connection string.
    @ConfigurationParameter("network-number")
    @IntDefaultValue(0x00)
    @Description("Access route: network number of the station addressed (0 = the connected station's own network).")
    private int networkNumber;

    @ConfigurationParameter("station-number")
    @IntDefaultValue(0xFF)
    @Description("Access route: station number on that network (255 = the connected station).")
    private int stationNumber = 0xFF;

    @ConfigurationParameter("module-io-number")
    @IntDefaultValue(0x03FF)
    @Description("Access route: request destination module I/O number (1023 = 0x03FF, the connected station's CPU; 992-995 = CPU No. 1-4 of a multiple-CPU system).")
    private int moduleIoNumber = 0x03FF;

    @ConfigurationParameter("multidrop-station-number")
    @IntDefaultValue(0x00)
    @Description("Access route: request destination multidrop station number (0 unless the module I/O number names a multidrop link).")
    private int multidropStationNumber;

    public int getMonitoringTimer() {
        return monitoringTimer;
    }

    public void setMonitoringTimer(int monitoringTimer) {
        this.monitoringTimer = monitoringTimer;
    }

    public int getRequestTimeout() {
        return requestTimeout;
    }

    public void setRequestTimeout(int requestTimeout) {
        this.requestTimeout = requestTimeout;
    }

    public int getNetworkNumber() {
        return networkNumber;
    }

    public void setNetworkNumber(int networkNumber) {
        this.networkNumber = networkNumber;
    }

    public int getStationNumber() {
        return stationNumber;
    }

    public void setStationNumber(int stationNumber) {
        this.stationNumber = stationNumber;
    }

    public int getModuleIoNumber() {
        return moduleIoNumber;
    }

    public void setModuleIoNumber(int moduleIoNumber) {
        this.moduleIoNumber = moduleIoNumber;
    }

    public int getMultidropStationNumber() {
        return multidropStationNumber;
    }

    public void setMultidropStationNumber(int multidropStationNumber) {
        this.multidropStationNumber = multidropStationNumber;
    }

    @Override
    public String toString() {
        return "SlmpConfiguration{monitoringTimer=" + monitoringTimer
            + ", requestTimeout=" + requestTimeout
            + ", networkNumber=" + networkNumber
            + ", stationNumber=" + stationNumber
            + ", moduleIoNumber=" + moduleIoNumber
            + ", multidropStationNumber=" + multidropStationNumber + '}';
    }
}
