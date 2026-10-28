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
	"context"
	"fmt"
	"time"

	apiModel "github.com/apache/plc4x/plc4go/pkg/api/model"
	readWriteModel "github.com/apache/plc4x/plc4go/protocols/canopen/readwrite/model"
	"github.com/apache/plc4x/plc4go/spi/errors"
)

// CiA 301 7.2.4.3.17, the SDO abort codes the driver sends.
const (
	abortToggleNotAlternated = 0x05030000
	abortTimedOut            = 0x05040000
	abortOutOfMemory         = 0x05040005
)

// maxSegmentedUpload bounds what one segmented upload collects, as plc4j's (16 MiB).
const maxSegmentedUpload = 16 * 1024 * 1024

// sdoAbortError is an SDO transfer the node aborted (or the driver did, local), with its CiA 301 abort code.
type sdoAbortError struct {
	code  uint32
	local bool
}

func (e sdoAbortError) Error() string {
	if e.local {
		return fmt.Sprintf("SDO transfer aborted here with code 0x%08X", e.code)
	}
	return fmt.Sprintf("SDO transfer aborted by the node with code 0x%08X", e.code)
}

var errSdoTimeout = errors.New("timed out waiting for the node's SDO response")

// responseCodeOf is the response code an SDO transfer that failed with err answers: REMOTE_ERROR, as plc4j's read
// answers for any failure.
func responseCodeOf(_ error) apiModel.PlcResponseCode {
	return apiModel.PlcResponseCode_REMOTE_ERROR
}

// sdoChannel is the SDO transfer in progress: the node whose responses it takes, and where the worker puts them.
type sdoChannel struct {
	answerNodeId uint8
	responses    chan readWriteModel.SDOResponse
}

// openSdo starts an SDO transfer with answerNodeId. A client has one SDO channel per server and a channel carries one
// transfer at a time (CiA 301 7.2.4.3), so transfers take turns; the returned function ends this one.
func (c *Connection) openSdo(answerNodeId uint8) (<-chan readWriteModel.SDOResponse, func()) {
	c.sdoMutex.Lock()
	channel := &sdoChannel{answerNodeId: answerNodeId, responses: make(chan readWriteModel.SDOResponse, 8)}
	c.sdoChannel.Store(channel)
	return channel.responses, func() {
		c.sdoChannel.Store(nil)
		c.sdoMutex.Unlock()
	}
}

// deliverSdo hands an SDO response of nodeId to the transfer waiting for it, if one is.
func (c *Connection) deliverSdo(nodeId uint8, response readWriteModel.SDOResponse) {
	channel := c.sdoChannel.Load()
	if channel == nil || channel.answerNodeId != nodeId {
		return
	}
	select {
	case channel.responses <- response:
	default:
		c.log.Debug().Uint8("nodeId", nodeId).Msg("dropping an SDO response nobody waits for")
	}
}

// awaitSdo is the next response accept takes; responses it does not take are passed over, as plc4j's driver test
// expects for an upload response about another object. Each answer is waited for up to the request timeout.
func (c *Connection) awaitSdo(ctx context.Context, responses <-chan readWriteModel.SDOResponse, accept func(readWriteModel.SDOResponse) bool) (readWriteModel.SDOResponse, error) {
	timer := time.NewTimer(c.configuration.requestTimeout)
	defer timer.Stop()
	for {
		select {
		case response := <-responses:
			if accept(response) {
				return response, nil
			}
		case <-timer.C:
			return nil, errSdoTimeout
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (c *Connection) sendSdoRequest(ctx context.Context, nodeId uint8, command readWriteModel.SDORequestCommand, request readWriteModel.SDORequest) error {
	return c.sendPayload(ctx, readWriteModel.CANOpenService_RECEIVE_SDO, nodeId, readWriteModel.NewCANOpenSDORequest(command, request))
}

// abortSdo tells the node the driver gave up on the transfer (CiA 301 7.2.4.3.17), and is the error to return.
func (c *Connection) abortSdo(ctx context.Context, nodeId uint8, address readWriteModel.IndexAddress, code uint32) error {
	if err := c.sendSdoRequest(ctx, nodeId, readWriteModel.SDORequestCommand_ABORT,
		readWriteModel.NewSDOAbortRequest(readWriteModel.NewSDOAbort(address, code))); err != nil {
		c.log.Debug().Err(err).Msg("could not send the SDO abort")
	}
	return sdoAbortError{code: code, local: true}
}

func sameAddress(a, b readWriteModel.IndexAddress) bool {
	return a.GetIndex() == b.GetIndex() && a.GetSubindex() == b.GetSubindex()
}

// abortFor takes the node's abort of the transfer about address.
func abortFor(address readWriteModel.IndexAddress, response readWriteModel.SDOResponse) (sdoAbortError, bool) {
	abort, ok := response.(readWriteModel.SDOAbortResponse)
	if !ok || !sameAddress(abort.GetAbort().GetAddress(), address) {
		return sdoAbortError{}, false
	}
	return sdoAbortError{code: abort.GetAbort().GetCode()}, true
}

// sdoUpload reads one object of a node's object dictionary: CiA 301 7.2.4.3.4 (initiate) and 7.2.4.3.5 (segments),
// as plc4j's SDOUploadConversation. Beyond plc4j, Go also takes a segmented upload whose size the node does not
// indicate (e=0, s=0). It does not take block upload, as plc4j does not.
func (c *Connection) sdoUpload(ctx context.Context, tag SDOTag) ([]byte, error) {
	responses, done := c.openSdo(tag.AnswerNodeId)
	defer done()
	address := readWriteModel.NewIndexAddress(tag.Index, tag.SubIndex)
	if err := c.sendSdoRequest(ctx, tag.NodeId, readWriteModel.SDORequestCommand_INITIATE_UPLOAD,
		readWriteModel.NewSDOInitiateUploadRequest(address)); err != nil {
		return nil, err
	}
	response, err := c.awaitSdo(ctx, responses, func(response readWriteModel.SDOResponse) bool {
		if _, aborted := abortFor(address, response); aborted {
			return true
		}
		initiate, ok := response.(readWriteModel.SDOInitiateUploadResponse)
		return ok && sameAddress(initiate.GetAddress(), address)
	})
	if err != nil {
		if errors.Is(err, errSdoTimeout) {
			_ = c.abortSdo(ctx, tag.NodeId, address, abortTimedOut)
		}
		return nil, err
	}
	if abort, aborted := abortFor(address, response); aborted {
		return nil, abort
	}
	size, sizeKnown := uint32(0), false
	switch payload := response.(readWriteModel.SDOInitiateUploadResponse).GetPayload().(type) {
	case readWriteModel.SDOInitiateExpeditedUploadResponse:
		return payload.GetData(), nil
	case readWriteModel.SDOInitiateSegmentedUploadResponse:
		size, sizeKnown = payload.GetBytes(), true
		if size > maxSegmentedUpload {
			return nil, c.abortSdo(ctx, tag.NodeId, address, abortOutOfMemory)
		}
	}
	var data []byte
	toggle := false
	for {
		if err := c.sendSdoRequest(ctx, tag.NodeId, readWriteModel.SDORequestCommand_SEGMENT_UPLOAD,
			readWriteModel.NewSDOSegmentUploadRequest(toggle)); err != nil {
			return nil, err
		}
		response, err := c.awaitSdo(ctx, responses, func(response readWriteModel.SDOResponse) bool {
			_, aborted := abortFor(address, response)
			_, segment := response.(readWriteModel.SDOSegmentUploadResponse)
			return aborted || segment
		})
		if err != nil {
			if errors.Is(err, errSdoTimeout) {
				_ = c.abortSdo(ctx, tag.NodeId, address, abortTimedOut)
			}
			return nil, err
		}
		if abort, aborted := abortFor(address, response); aborted {
			return nil, abort
		}
		segment := response.(readWriteModel.SDOSegmentUploadResponse)
		if segment.GetToggle() != toggle {
			return nil, c.abortSdo(ctx, tag.NodeId, address, abortToggleNotAlternated)
		}
		data = append(data, segment.GetData()...)
		if len(data) > maxSegmentedUpload {
			return nil, c.abortSdo(ctx, tag.NodeId, address, abortOutOfMemory)
		}
		if segment.GetLast() {
			break
		}
		toggle = !toggle
	}
	if sizeKnown && uint32(len(data)) != size {
		return nil, errors.Errorf("the node announced %d bytes and sent %d", size, len(data))
	}
	return data, nil
}

// sdoDownload writes one object of a node's object dictionary: expedited when the data fits the initiate request's
// four bytes (CiA 301 7.2.4.3.3), segmented otherwise (7.2.4.3.2), as plc4j's SDODownloadConversation. It does not do
// block download, as plc4j does not.
func (c *Connection) sdoDownload(ctx context.Context, tag SDOTag, data []byte) error {
	responses, done := c.openSdo(tag.AnswerNodeId)
	defer done()
	address := readWriteModel.NewIndexAddress(tag.Index, tag.SubIndex)
	expedited := len(data) > 0 && len(data) <= 4
	var payload readWriteModel.SDOInitiateUploadResponsePayload
	if expedited {
		payload = readWriteModel.NewSDOInitiateExpeditedUploadResponse(data)
	} else {
		payload = readWriteModel.NewSDOInitiateSegmentedUploadResponse(uint32(len(data)))
	}
	if err := c.sendSdoRequest(ctx, tag.NodeId, readWriteModel.SDORequestCommand_INITIATE_DOWNLOAD,
		readWriteModel.NewSDOInitiateDownloadRequest(expedited, true, address, payload)); err != nil {
		return err
	}
	response, err := c.awaitSdo(ctx, responses, func(response readWriteModel.SDOResponse) bool {
		if _, aborted := abortFor(address, response); aborted {
			return true
		}
		initiate, ok := response.(readWriteModel.SDOInitiateDownloadResponse)
		return ok && sameAddress(initiate.GetAddress(), address)
	})
	if err != nil {
		if errors.Is(err, errSdoTimeout) {
			_ = c.abortSdo(ctx, tag.NodeId, address, abortTimedOut)
		}
		return err
	}
	if abort, aborted := abortFor(address, response); aborted {
		return abort
	}
	if expedited {
		return nil
	}
	toggle := false
	for offset := 0; ; {
		end := min(offset+7, len(data))
		last := end == len(data)
		if err := c.sendSdoRequest(ctx, tag.NodeId, readWriteModel.SDORequestCommand_SEGMENT_DOWNLOAD,
			readWriteModel.NewSDOSegmentDownloadRequest(toggle, last, data[offset:end])); err != nil {
			return err
		}
		response, err := c.awaitSdo(ctx, responses, func(response readWriteModel.SDOResponse) bool {
			_, aborted := abortFor(address, response)
			_, segment := response.(readWriteModel.SDOSegmentDownloadResponse)
			return aborted || segment
		})
		if err != nil {
			if errors.Is(err, errSdoTimeout) {
				_ = c.abortSdo(ctx, tag.NodeId, address, abortTimedOut)
			}
			return err
		}
		if abort, aborted := abortFor(address, response); aborted {
			return abort
		}
		if response.(readWriteModel.SDOSegmentDownloadResponse).GetToggle() != toggle {
			return c.abortSdo(ctx, tag.NodeId, address, abortToggleNotAlternated)
		}
		if last {
			return nil
		}
		offset, toggle = end, !toggle
	}
}
