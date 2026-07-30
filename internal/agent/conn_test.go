package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConnRequestResponseAndServerCall(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	server := NewConn(serverSide, serverSide)
	client := NewConn(clientSide, clientSide)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errs := make(chan error, 2)
	go func() {
		errs <- server.Serve(ctx, func(method string, params json.RawMessage) (any, *RPCError) {
			if method != "echo" {
				return nil, NewRPCError(CodeMethodNotFound, "method not found", nil)
			}
			var value map[string]string
			if err := json.Unmarshal(params, &value); err != nil {
				return nil, NewRPCError(CodeInvalidParams, "invalid params", nil)
			}
			return value, nil
		})
	}()
	go func() {
		errs <- client.Serve(ctx, func(method string, params json.RawMessage) (any, *RPCError) {
			return map[string]bool{"confirmed": method == "ui.confirm"}, nil
		})
	}()

	var echo map[string]string
	if err := client.Call(ctx, "echo", map[string]string{"value": "ok"}, &echo); err != nil {
		t.Fatal(err)
	}
	if echo["value"] != "ok" {
		t.Fatalf("unexpected echo result: %+v", echo)
	}
	var confirm struct {
		Confirmed bool `json:"confirmed"`
	}
	if err := server.Call(ctx, "ui.confirm", map[string]string{"prompt": "Continue?"}, &confirm); err != nil {
		t.Fatal(err)
	}
	if !confirm.Confirmed {
		t.Fatal("expected server-to-client call result")
	}
}

func TestConnCorrelatesOutOfOrderResponses(t *testing.T) {
	local, peer := net.Pipe()
	defer local.Close()
	defer peer.Close()
	conn := NewConn(local, local)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = conn.Serve(ctx, nil) }()

	go func() {
		scanner := bufio.NewScanner(peer)
		requests := make([]envelope, 0, 2)
		for scanner.Scan() {
			var msg envelope
			if json.Unmarshal(scanner.Bytes(), &msg) == nil {
				requests = append(requests, msg)
			}
			if len(requests) == 2 {
				encoder := json.NewEncoder(peer)
				_ = encoder.Encode(envelope{JSONRPC: "2.0", ID: requests[1].ID, Result: json.RawMessage(`{"value":"second"}`)})
				_ = encoder.Encode(envelope{JSONRPC: "2.0", ID: requests[0].ID, Result: json.RawMessage(`{"value":"first"}`)})
				return
			}
		}
	}()

	type result struct {
		Value string `json:"value"`
	}
	results := make([]result, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); errs[0] = conn.Call(ctx, "first", nil, &results[0]) }()
	time.Sleep(time.Millisecond)
	go func() { defer wg.Done(); errs[1] = conn.Call(ctx, "second", nil, &results[1]) }()
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("call errors: %v %v", errs[0], errs[1])
	}
	if results[0].Value != "first" || results[1].Value != "second" {
		t.Fatalf("responses were miscorrelated: %+v", results)
	}
}

func TestConnConcurrentWritesRemainNDJSON(t *testing.T) {
	var out bytes.Buffer
	conn := NewConn(strings.NewReader(""), &out)
	const count = 100
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if err := conn.Notify("log", map[string]int{"index": index}); err != nil {
				t.Errorf("notify: %v", err)
			}
		}(i)
	}
	wg.Wait()
	scanner := bufio.NewScanner(bytes.NewReader(out.Bytes()))
	lines := 0
	for scanner.Scan() {
		lines++
		var msg envelope
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			t.Fatalf("interleaved JSON line %d: %v\n%s", lines, err, scanner.Bytes())
		}
	}
	if lines != count {
		t.Fatalf("got %d frames, want %d", lines, count)
	}
}

func TestConnMalformedFrameReturnsParseError(t *testing.T) {
	var out bytes.Buffer
	conn := NewConn(strings.NewReader("{bad json}\n"), &out)
	err := conn.Serve(context.Background(), nil)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
	var reply envelope
	if decodeErr := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &reply); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if reply.Error == nil || reply.Error.Code != CodeParseError || string(reply.ID) != "null" {
		t.Fatalf("unexpected parse error reply: %+v", reply)
	}
}

func TestConnRejectsOversizedInboundFrame(t *testing.T) {
	input := strings.Repeat("x", MaxFrameBytes+1) + "\n"
	conn := NewConn(strings.NewReader(input), io.Discard)
	err := conn.Serve(context.Background(), nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != CodeFrameTooLarge {
		t.Fatalf("expected frame-too-large error, got %T %v", err, err)
	}
}

func TestConnDisconnectFailsPendingCall(t *testing.T) {
	local, peer := net.Pipe()
	conn := NewConn(local, local)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { _ = conn.Serve(ctx, nil) }()
	go func() {
		scanner := bufio.NewScanner(peer)
		if scanner.Scan() {
			_ = peer.Close()
		}
	}()
	err := conn.Call(ctx, "waiting", nil, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != CodeDisconnected {
		t.Fatalf("expected disconnected RPC error, got %T %v", err, err)
	}
}

func TestConnRejectsDuplicateActiveRequestID(t *testing.T) {
	serverSide, peer := net.Pipe()
	defer serverSide.Close()
	defer peer.Close()
	conn := NewConn(serverSide, serverSide)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	block := make(chan struct{})
	go func() {
		_ = conn.Serve(ctx, func(string, json.RawMessage) (any, *RPCError) {
			<-block
			return map[string]bool{"ok": true}, nil
		})
	}()
	encoder := json.NewEncoder(peer)
	request := envelope{JSONRPC: "2.0", ID: json.RawMessage(`"same"`), Method: "wait"}
	if err := encoder.Encode(request); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Encode(request); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(peer)
	if !scanner.Scan() {
		t.Fatal("expected duplicate-id response")
	}
	var duplicate envelope
	if err := json.Unmarshal(scanner.Bytes(), &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.Error == nil || duplicate.Error.Code != CodeRunConflict {
		t.Fatalf("unexpected duplicate response: %+v", duplicate)
	}
	close(block)
	if !scanner.Scan() {
		t.Fatal("expected original request response")
	}
	var original envelope
	if err := json.Unmarshal(scanner.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	if original.Error != nil || len(original.Result) == 0 {
		t.Fatalf("unexpected original response: %+v", original)
	}
}

func TestConnConvertsHandlerPanicToProtocolError(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	server := NewConn(serverSide, serverSide)
	client := NewConn(clientSide, clientSide)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = server.Serve(ctx, func(string, json.RawMessage) (any, *RPCError) {
			panic("secret panic detail")
		})
	}()
	go func() { _ = client.Serve(ctx, nil) }()
	err := client.Call(ctx, "panic", nil, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != CodeInternalError {
		t.Fatalf("expected internal protocol error, got %T %v", err, err)
	}
	if strings.Contains(rpcErr.Message, "secret panic detail") {
		t.Fatalf("panic detail leaked into protocol error: %q", rpcErr.Message)
	}
}
