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

package slmp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apache/plc4x/plc4go/spi/testutils"
)

func TestDefaultConfiguration(t *testing.T) {
	configuration := DefaultConfiguration()
	// plc4j's SlmpConfiguration: @IntDefaultValue(0x0000) and @IntDefaultValue(5_000).
	assert.Equal(t, uint16(0x0000), configuration.monitoringTimer)
	assert.Equal(t, 5*time.Second, configuration.requestTimeout)
}

func TestParseFromOptions(t *testing.T) {
	tests := []struct {
		name              string
		connectionOptions map[string][]string
		want              Configuration
		wantErr           bool
	}{
		{
			name:              "no options at all is the default",
			connectionOptions: map[string][]string{},
			want:              DefaultConfiguration(),
		},
		{
			name:              "both options",
			connectionOptions: map[string][]string{"monitoring-timer": {"250"}, "request-timeout-ms": {"1500"}},
			want:              Configuration{monitoringTimer: 250, requestTimeout: 1500 * time.Millisecond, route: ownStation},
		},
		{
			name:              "the request timeout is spelled in milliseconds",
			connectionOptions: map[string][]string{"request-timeout-ms": {"250"}},
			want:              Configuration{monitoringTimer: defaultMonitoringTimer, requestTimeout: 250 * time.Millisecond, route: ownStation},
		},
		{
			name:              "the largest monitoring timer the 3E field can carry",
			connectionOptions: map[string][]string{"monitoring-timer": {"65535"}},
			want:              Configuration{monitoringTimer: 0xFFFF, requestTimeout: defaultRequestTimeout, route: ownStation},
		},
		{
			// plc4j checks this at connect time, with the same reasoning: the field is an unsigned
			// 16-bit one in the 3E frame.
			name:              "a monitoring timer beyond the 16-bit field is refused",
			connectionOptions: map[string][]string{"monitoring-timer": {"65536"}},
			wantErr:           true,
		},
		{
			name:              "a negative monitoring timer is refused",
			connectionOptions: map[string][]string{"monitoring-timer": {"-1"}},
			wantErr:           true,
		},
		{
			name:              "a non-numeric monitoring timer is refused",
			connectionOptions: map[string][]string{"monitoring-timer": {"soon"}},
			wantErr:           true,
		},
		{
			// A zero timeout would time out every request immediately, which is never what anyone
			// meant. plc4j rejects it at connect time.
			name:              "a zero request timeout is refused",
			connectionOptions: map[string][]string{"request-timeout-ms": {"0"}},
			wantErr:           true,
		},
		{
			name:              "a non-numeric request timeout is refused",
			connectionOptions: map[string][]string{"request-timeout-ms": {"later"}},
			wantErr:           true,
		},
		{
			name:              "an empty option value falls back to the default",
			connectionOptions: map[string][]string{"monitoring-timer": {}},
			want:              DefaultConfiguration(),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			configuration, err := ParseFromOptions(testutils.ProduceTestingLogger(t), testCase.connectionOptions)
			if testCase.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.want, configuration)
		})
	}
}

func TestConfiguration_String(t *testing.T) {
	assert.Equal(t, "slmp.Configuration{monitoringTimer: 0x0000, requestTimeout: 5s, route: network 0 station 0xFF module I/O 0x03FF multidrop 0}",
		DefaultConfiguration().String())
}

// The access route: decimal or 0x hex, each in its field's width; the connected station by default.
func TestParseFromOptions_TheAccessRoute(t *testing.T) {
	configuration, err := ParseFromOptions(testutils.ProduceTestingLogger(t), map[string][]string{
		"network-number": {"2"}, "station-number": {"3"}, "module-io-number": {"0x03E1"}, "multidrop-station-number": {"0"},
	})
	require.NoError(t, err)
	assert.Equal(t, Route{Network: 2, Station: 3, ModuleIo: 0x03E1, MultidropStation: 0}, configuration.route)

	defaults, err := ParseFromOptions(testutils.ProduceTestingLogger(t), map[string][]string{})
	require.NoError(t, err)
	assert.Equal(t, ownStation, defaults.route)

	for name, value := range map[string]string{"network-number": "256", "station-number": "-1", "module-io-number": "0x10000", "multidrop-station-number": "x"} {
		_, err := ParseFromOptions(testutils.ProduceTestingLogger(t), map[string][]string{name: {value}})
		assert.Error(t, err, "%s=%s", name, value)
	}
}
