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

package iec608705104

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// with is the default configuration with some fields changed.
func with(change func(*Configuration)) Configuration {
	configuration := DefaultConfiguration()
	change(&configuration)
	return configuration
}

func TestParseFromOptions(t *testing.T) {
	tests := []struct {
		name              string
		connectionOptions map[string][]string
		want              Configuration
		wantErr           bool
	}{
		{
			name:              "the defaults",
			connectionOptions: map[string][]string{},
			want:              with(func(c *Configuration) { c.requestTimeout = 4 * time.Second; c.ackThreshold = 8 }),
		},
		{
			name:              "a request timeout in milliseconds",
			connectionOptions: map[string][]string{"request-timeout-ms": {"1500"}},
			want:              with(func(c *Configuration) { c.requestTimeout = 1500 * time.Millisecond; c.ackThreshold = 8 }),
		},
		{
			name:              "a smaller acknowledgement window",
			connectionOptions: map[string][]string{"ack-threshold": {"4"}},
			want:              with(func(c *Configuration) { c.requestTimeout = 4 * time.Second; c.ackThreshold = 4 }),
		},
		{
			name:              "the largest window a sequence number can express",
			connectionOptions: map[string][]string{"ack-threshold": {"32767"}},
			want:              with(func(c *Configuration) { c.requestTimeout = 4 * time.Second; c.ackThreshold = 32767 }),
		},
		{
			name:              "an option given twice takes the first",
			connectionOptions: map[string][]string{"ack-threshold": {"4", "6"}},
			want:              with(func(c *Configuration) { c.requestTimeout = 4 * time.Second; c.ackThreshold = 4 }),
		},
		{
			name:              "an empty option list is no option at all",
			connectionOptions: map[string][]string{"ack-threshold": {}},
			want:              with(func(c *Configuration) { c.requestTimeout = 4 * time.Second; c.ackThreshold = 8 }),
		},
		{
			name:              "the keep-alive timers, in milliseconds",
			connectionOptions: map[string][]string{"t1-ms": {"5000"}, "t3-ms": {"10000"}},
			want:              with(func(c *Configuration) { c.t1 = 5 * time.Second; c.t3 = 10 * time.Second }),
		},
		{
			name:              "no keep-alive",
			connectionOptions: map[string][]string{"t3-ms": {"0"}},
			want:              with(func(c *Configuration) { c.t3 = 0 }),
		},
		{
			name:              "no general interrogation, to one common address, from originator 3",
			connectionOptions: map[string][]string{"general-interrogation": {"false"}, "interrogation-asdu-address": {"47"}, "originator-address": {"3"}},
			want: with(func(c *Configuration) {
				c.generalInterrogation = false
				c.interrogationAsduAddress = 47
				c.originatorAddress = 3
			}),
		},
		{name: "a t1 of zero", connectionOptions: map[string][]string{"t1-ms": {"0"}}, wantErr: true},
		{name: "a general interrogation which isn't a boolean", connectionOptions: map[string][]string{"general-interrogation": {"sometimes"}}, wantErr: true},
		{name: "a common address past 16 bits", connectionOptions: map[string][]string{"interrogation-asdu-address": {"65536"}}, wantErr: true},
		{name: "an originator address past 8 bits", connectionOptions: map[string][]string{"originator-address": {"256"}}, wantErr: true},
		{name: "a request timeout which isn't a number", connectionOptions: map[string][]string{"request-timeout-ms": {"soon"}}, wantErr: true},
		{name: "a request timeout of zero", connectionOptions: map[string][]string{"request-timeout-ms": {"0"}}, wantErr: true},
		{name: "a window which isn't a number", connectionOptions: map[string][]string{"ack-threshold": {"lots"}}, wantErr: true},
		{name: "a window of zero", connectionOptions: map[string][]string{"ack-threshold": {"0"}}, wantErr: true},
		{name: "a window past the sequence number", connectionOptions: map[string][]string{"ack-threshold": {"32768"}}, wantErr: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			configuration, err := ParseFromOptions(zerolog.Nop(), testCase.connectionOptions)
			if testCase.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.want, configuration)
		})
	}
}
