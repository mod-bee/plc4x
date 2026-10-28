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

package genericcan

import (
	"reflect"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	apiValues "github.com/apache/plc4x/plc4go/pkg/api/values"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/genericcan/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/errors"
	"github.com/apache/plc4x/plc4go/spi/options"
	spiValues "github.com/apache/plc4x/plc4go/spi/values"
)

// ValueHandler is plc4j's DefaultPlcValueHandler, which the default handler is, plus RAW: a RAW tag's value is its
// payload, given as bytes ([]byte, or a list of numbers each a byte) and kept as a list of BYTEs, which is what the
// generated serializer walks.
type ValueHandler struct {
	spiValues.DefaultValueHandler
}

func NewValueHandler(_options ...options.WithOption) ValueHandler {
	return ValueHandler{spiValues.NewDefaultValueHandler(_options...)}
}

func (m ValueHandler) NewPlcValue(tag apiModel.PlcTag, value any) (apiValues.PlcValue, error) {
	canTag, ok := tag.(Tag)
	if !ok || canTag.DataType != readWriteModel.GenericCANDataType_RAW {
		return m.DefaultValueHandler.NewPlcValue(tag, value)
	}
	switch typed := value.(type) {
	case []byte:
		elements := make([]apiValues.PlcValue, len(typed))
		for i, b := range typed {
			elements[i] = spiValues.NewPlcBYTE(b)
		}
		return spiValues.NewPlcList(elements), nil
	case apiValues.PlcValue:
		if typed.IsRaw() {
			return m.NewPlcValue(tag, typed.GetRaw())
		}
		return typed, nil
	}
	list := reflect.ValueOf(value)
	if list.Kind() != reflect.Slice {
		return nil, errors.Errorf("a RAW value is bytes, not %T", value)
	}
	elements := make([]apiValues.PlcValue, list.Len())
	for i := range elements {
		b, err := byteOf(list.Index(i))
		if err != nil {
			return nil, errors.Wrapf(err, "byte %d of a RAW value", i)
		}
		elements[i] = spiValues.NewPlcBYTE(b)
	}
	return spiValues.NewPlcList(elements), nil
}

// byteOf is one element of a RAW value as a byte: any number from 0 to 255.
func byteOf(element reflect.Value) (byte, error) {
	if element.Kind() == reflect.Interface {
		element = element.Elem()
	}
	switch element.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n := element.Int(); n >= 0 && n <= 255 {
			return byte(n), nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n := element.Uint(); n <= 255 {
			return byte(n), nil
		}
	case reflect.Float32, reflect.Float64:
		if f := element.Float(); f >= 0 && f <= 255 && f == float64(int(f)) {
			return byte(f), nil
		}
	}
	return 0, errors.Errorf("%v is not a byte (0 to 255)", element)
}
