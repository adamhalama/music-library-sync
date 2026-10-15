// Package agent implements the frontend-neutral JSON-RPC backend used by the
// native macOS application.
package agent

import (
	"encoding/json"
	"fmt"
)

const (
	ProtocolVersion = 2
	MaxFrameBytes   = 8 << 20

	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	CodeNotInitialized = -32001
	CodeRunNotFound    = -32002
	CodeRunConflict    = -32003
	CodeCanceled       = -32004
	CodeFrameTooLarge  = -32005
	CodeDisconnected   = -32006
)

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

func NewRPCError(code int, message string, data any) *RPCError {
	err := &RPCError{Code: code, Message: message}
	if data != nil {
		if payload, marshalErr := json.Marshal(data); marshalErr == nil {
			err.Data = payload
		}
	}
	return err
}

type Handler func(method string, params json.RawMessage) (any, *RPCError)

// AfterResponse runs only after the matching response frame has been flushed.
// It is used for lifecycle operations such as session.shutdown.
type AfterResponse struct {
	Result any
	After  func()
}

type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type response struct {
	result json.RawMessage
	err    *RPCError
}
