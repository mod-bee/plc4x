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
	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/options"
	spiValues "github.com/apache/plc4x/plc4go/spi/values"
)

// ValueHandler is plc4j's DefaultPlcValueHandler, which the default handler is, plus what it cannot build: a
// RECORD's value is its bytes ([]byte, or a list of numbers each a byte), kept as a RAW_BYTE_ARRAY.
type ValueHandler struct {
	spiValues.DefaultValueHandler
}

func NewValueHandler(_options ...options.WithOption) ValueHandler {
	return ValueHandler{spiValues.NewDefaultValueHandler(_options...)}
}

func (m ValueHandler) NewPlcValue(tag apiModel.PlcTag, value any) (apiValues.PlcValue, error) {
	switch typed := tag.(type) {
	case SDOTag:
		if typed.DataType == readWriteModel.CANOpenDataType_RECORD {
			return rawValue(value)
		}
	case PDOTag:
		if typed.DataType == readWriteModel.CANOpenDataType_RECORD {
			return rawValue(value)
		}
	}
	return m.DefaultValueHandler.NewPlcValue(tag, value)
}

func rawValue(value any) (apiValues.PlcValue, error) {
	switch typed := value.(type) {
	case []byte:
		return spiValues.NewPlcRawByteArray(typed), nil
	case apiValues.PlcValue:
		if typed.IsRaw() {
			return typed, nil
		}
		if typed.IsList() {
			data := make([]byte, len(typed.GetList()))
			for i, element := range typed.GetList() {
				if !element.IsUint64() || element.GetUint64() > 255 {
					return nil, errors.Errorf("byte %d of a RECORD is not 0 to 255", i)
				}
				data[i] = byte(element.GetUint64())
			}
			return spiValues.NewPlcRawByteArray(data), nil
		}
	case []any:
		data := make([]byte, len(typed))
		for i, element := range typed {
			number, ok := element.(int)
			if !ok || number < 0 || number > 255 {
				return nil, errors.Errorf("byte %d of a RECORD is not 0 to 255", i)
			}
			data[i] = byte(number)
		}
		return spiValues.NewPlcRawByteArray(data), nil
	}
	return nil, errors.Errorf("a RECORD's value is bytes, not %T", value)
}
