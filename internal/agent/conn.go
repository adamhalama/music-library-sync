package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type Conn struct {
	in       io.Reader
	inCloser io.Closer
	out      *bufio.Writer

	writeMu  sync.Mutex
	stateMu  sync.Mutex
	pending  map[string]chan response
	incoming map[string]struct{}
	closed   bool
	closeErr error
	nextID   atomic.Uint64
}

func NewConn(in io.Reader, out io.Writer) *Conn {
	conn := &Conn{in: in, out: bufio.NewWriter(out), pending: map[string]chan response{}, incoming: map[string]struct{}{}}
	conn.inCloser, _ = in.(io.Closer)
	return conn
}

func (c *Conn) Serve(ctx context.Context, handler Handler) error {
	if c == nil || c.in == nil || c.out == nil {
		return errors.New("agent connection requires input and output")
	}
	scanner := bufio.NewScanner(c.in)
	scanner.Buffer(make([]byte, 64*1024), MaxFrameBytes)
	type scanResult struct {
		line []byte
		err  error
	}
	frames := make(chan scanResult, 1)
	go func() {
		for scanner.Scan() {
			select {
			case frames <- scanResult{line: append([]byte(nil), scanner.Bytes()...)}:
			case <-ctx.Done():
				return
			}
		}
		select {
		case frames <- scanResult{err: scanner.Err()}:
		case <-ctx.Done():
		}
	}()
	for {
		var frame scanResult
		select {
		case <-ctx.Done():
			if c.inCloser != nil {
				_ = c.inCloser.Close()
			}
			c.closePending(ctx.Err())
			return ctx.Err()
		case frame = <-frames:
		}
		if frame.line == nil {
			err := frame.err
			if err != nil && strings.Contains(err.Error(), "token too long") {
				err = NewRPCError(CodeFrameTooLarge, fmt.Sprintf("frame exceeds %d bytes", MaxFrameBytes), nil)
			}
			if err == nil {
				err = io.EOF
			}
			c.closePending(err)
			return err
		}
		line := frame.line
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var msg envelope
		if err := json.Unmarshal(line, &msg); err != nil {
			_ = c.writeEnvelope(envelope{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: NewRPCError(CodeParseError, "parse error", nil)})
			continue
		}
		if msg.JSONRPC != "2.0" {
			_ = c.writeEnvelope(envelope{JSONRPC: "2.0", ID: responseID(msg.ID), Error: NewRPCError(CodeInvalidRequest, "jsonrpc must be \"2.0\"", nil)})
			continue
		}
		if msg.Method == "" {
			c.deliverResponse(msg)
			continue
		}
		if len(msg.ID) > 0 && !validID(msg.ID) {
			_ = c.writeEnvelope(envelope{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: NewRPCError(CodeInvalidRequest, "request id must be a string or number", nil)})
			continue
		}
		if len(msg.ID) > 0 && !c.registerIncoming(msg.ID) {
			_ = c.writeEnvelope(envelope{JSONRPC: "2.0", ID: msg.ID, Error: NewRPCError(CodeRunConflict, "request id is already active", nil)})
			continue
		}
		go c.handleRequest(msg, handler)
	}
}

func (c *Conn) handleRequest(msg envelope, handler Handler) {
	if len(msg.ID) > 0 {
		defer c.clearIncoming(msg.ID)
	}
	if handler == nil {
		if len(msg.ID) > 0 {
			_ = c.writeEnvelope(envelope{JSONRPC: "2.0", ID: msg.ID, Error: NewRPCError(CodeMethodNotFound, "method not found", nil)})
		}
		return
	}
	result, rpcErr := invokeHandler(handler, msg.Method, msg.Params)
	var after func()
	if wrapped, ok := result.(AfterResponse); ok {
		result, after = wrapped.Result, wrapped.After
	}
	if len(msg.ID) == 0 {
		return
	}
	reply := envelope{JSONRPC: "2.0", ID: msg.ID, Error: rpcErr}
	if rpcErr == nil {
		payload, err := json.Marshal(result)
		if err != nil {
			reply.Error = NewRPCError(CodeInternalError, "encode response", nil)
		} else {
			reply.Result = payload
		}
	}
	_ = c.writeEnvelope(reply)
	if after != nil {
		after()
	}
}

func invokeHandler(handler Handler, method string, params json.RawMessage) (result any, rpcErr *RPCError) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			rpcErr = NewRPCError(CodeInternalError, "request handler panicked", nil)
		}
	}()
	return handler(method, params)
}

func (c *Conn) registerIncoming(id json.RawMessage) bool {
	key := string(bytes.TrimSpace(id))
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if _, exists := c.incoming[key]; exists {
		return false
	}
	c.incoming[key] = struct{}{}
	return true
}

func (c *Conn) clearIncoming(id json.RawMessage) {
	key := string(bytes.TrimSpace(id))
	c.stateMu.Lock()
	delete(c.incoming, key)
	c.stateMu.Unlock()
}

func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	if strings.TrimSpace(method) == "" {
		return errors.New("JSON-RPC method must be set")
	}
	id := "s-" + strconv.FormatUint(c.nextID.Add(1), 10)
	idPayload, _ := json.Marshal(id)
	paramsPayload, err := marshalOptional(params)
	if err != nil {
		return fmt.Errorf("encode %s params: %w", method, err)
	}
	ch := make(chan response, 1)
	key := string(idPayload)
	c.stateMu.Lock()
	if c.closed {
		err := c.closeErr
		c.stateMu.Unlock()
		if err == nil {
			err = io.EOF
		}
		return err
	}
	c.pending[key] = ch
	c.stateMu.Unlock()

	if err := c.writeEnvelope(envelope{JSONRPC: "2.0", ID: idPayload, Method: method, Params: paramsPayload}); err != nil {
		c.removePending(key)
		return err
	}
	select {
	case reply := <-ch:
		if reply.err != nil {
			return reply.err
		}
		if result == nil || len(reply.result) == 0 || bytes.Equal(reply.result, []byte("null")) {
			return nil
		}
		if err := json.Unmarshal(reply.result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		c.removePending(key)
		return ctx.Err()
	}
}

func (c *Conn) Notify(method string, params any) error {
	if strings.TrimSpace(method) == "" {
		return errors.New("JSON-RPC method must be set")
	}
	payload, err := marshalOptional(params)
	if err != nil {
		return fmt.Errorf("encode %s params: %w", method, err)
	}
	return c.writeEnvelope(envelope{JSONRPC: "2.0", Method: method, Params: payload})
}

func (c *Conn) deliverResponse(msg envelope) {
	if !validID(msg.ID) {
		return
	}
	key := string(bytes.TrimSpace(msg.ID))
	c.stateMu.Lock()
	ch := c.pending[key]
	delete(c.pending, key)
	c.stateMu.Unlock()
	if ch != nil {
		ch <- response{result: msg.Result, err: msg.Error}
	}
}

func (c *Conn) writeEnvelope(msg envelope) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.stateMu.Lock()
	closed, closeErr := c.closed, c.closeErr
	c.stateMu.Unlock()
	if closed {
		if closeErr != nil {
			return closeErr
		}
		return io.EOF
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(payload) > MaxFrameBytes {
		return NewRPCError(CodeFrameTooLarge, fmt.Sprintf("frame exceeds %d bytes", MaxFrameBytes), nil)
	}
	if _, err := c.out.Write(payload); err != nil {
		c.closePending(err)
		return err
	}
	if err := c.out.WriteByte('\n'); err != nil {
		c.closePending(err)
		return err
	}
	if err := c.out.Flush(); err != nil {
		c.closePending(err)
		return err
	}
	return nil
}

func (c *Conn) closePending(err error) {
	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		return
	}
	c.closed, c.closeErr = true, err
	pending := c.pending
	c.pending = map[string]chan response{}
	c.stateMu.Unlock()
	rpcErr := NewRPCError(CodeDisconnected, "connection closed", nil)
	for _, ch := range pending {
		ch <- response{err: rpcErr}
	}
}

func (c *Conn) removePending(key string) {
	c.stateMu.Lock()
	delete(c.pending, key)
	c.stateMu.Unlock()
}

func marshalOptional(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func validID(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return false
	}
	var value any
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return false
	}
	switch value.(type) {
	case string, float64:
		return true
	default:
		return false
	}
}

func responseID(raw json.RawMessage) json.RawMessage {
	if validID(raw) {
		return raw
	}
	return json.RawMessage("null")
}
