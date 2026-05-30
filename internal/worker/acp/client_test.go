package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ─── NewACPClient ─────────────────────────────────────────────────────────────

func TestNewACPClient(t *testing.T) {
	t.Parallel()

	c := NewACPClient(nil, nil, nil)
	require.NotNil(t, c)
	require.NotNil(t, c.pending)
	require.NotNil(t, c.NotificationCh)
	require.NotNil(t, c.RequestCh)
	require.NotNil(t, c.done)
}

func TestNewACPClient_DefaultLogger(t *testing.T) {
	t.Parallel()

	c := NewACPClient(nil, nil, nil)
	require.Equal(t, slog.Default(), c.log)
}

// ─── RespondPermission ─────────────────────────────────────────────────────────

func TestRespondPermission(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	c := &ACPClient{
		stdin:   &buf,
		pending: make(map[string]chan *JSONRPCResponse),
	}

	err := c.RespondPermission(context.Background(), mustMarshal(42), map[string]any{
		"outcome":  "selected",
		"optionId": "opt1",
	})
	require.NoError(t, err)

	// Verify the written message.
	line, err := buf.ReadString('\n')
	require.NoError(t, err)

	var resp JSONRPCResponse
	require.NoError(t, json.Unmarshal([]byte(line[:len(line)-1]), &resp))
	require.Equal(t, "2.0", resp.JSONRPC)
	require.Equal(t, `42`, string(resp.ID))

	var result map[string]any
	require.NoError(t, json.Unmarshal(resp.Result, &result))
	require.Equal(t, "selected", result["outcome"])
}

// ─── readLoop ──────────────────────────────────────────────────────────────────

func TestReadLoop_EOF(t *testing.T) {
	t.Parallel()

	r, w := io.Pipe()
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })

	c := NewACPClient(io.Discard, bufio.NewReader(r), slog.Default())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go c.readLoop(ctx)

	// Close write end → reader gets EOF.
	require.NoError(t, w.Close())

	select {
	case <-c.Done():
		// Success: readLoop exited cleanly.
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit on EOF")
	}
}

func TestReadLoop_Cancel(t *testing.T) {
	t.Parallel()

	c := NewACPClient(io.Discard, bufio.NewReader(bytes.NewReader(nil)), slog.Default())
	ctx, cancel := context.WithCancel(t.Context())

	go c.readLoop(ctx)

	cancel()

	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit on cancel")
	}
}

func TestReadLoop_Notification(t *testing.T) {
	t.Parallel()

	notif := `{"jsonrpc":"2.0","method":"session/update","params":{}}` + "\n"
	r := bytes.NewReader([]byte(notif))

	c := NewACPClient(io.Discard, bufio.NewReader(r), slog.Default())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go c.readLoop(ctx)

	select {
	case n := <-c.NotificationCh:
		require.Equal(t, "session/update", n.Method)
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive notification")
	}

	// Wait for readLoop to exit (EOF from finite reader).
	cancel()
	<-c.Done()
}

func TestReadLoop_Request(t *testing.T) {
	t.Parallel()

	req := `{"jsonrpc":"2.0","id":1,"method":"session/request_permission","params":{}}` + "\n"
	r := bytes.NewReader([]byte(req))

	c := NewACPClient(io.Discard, bufio.NewReader(r), slog.Default())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go c.readLoop(ctx)

	select {
	case r := <-c.RequestCh:
		require.Equal(t, "session/request_permission", r.Method)
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive request")
	}

	// Wait for readLoop to exit (EOF from finite reader).
	cancel()
	<-c.Done()
}

// ─── call + dispatchResponse ───────────────────────────────────────────────────

func TestCall_ResponseRoundtrip(t *testing.T) {
	t.Parallel()

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	t.Cleanup(func() {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
	})

	c := NewACPClient(stdinW, bufio.NewReader(stdoutR), slog.Default())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go c.readLoop(ctx)

	var wg sync.WaitGroup
	var callErr error
	var callResult *JSONRPCResponse
	wg.Add(1)
	go func() {
		defer wg.Done()
		callResult, callErr = c.call(ctx, "test/method", map[string]any{"key": "val"})
	}()

	// Read the outgoing request from stdin.
	line, err := bufio.NewReader(stdinR).ReadString('\n')
	require.NoError(t, err)

	var req JSONRPCRequest
	require.NoError(t, json.Unmarshal([]byte(line[:len(line)-1]), &req))
	require.Equal(t, "test/method", req.Method)

	// Write back a matching response via stdout, then close to unblock readLoop.
	resp := `{"jsonrpc":"2.0","id":` + string(req.ID) + `,"result":{"ok":true}}` + "\n"
	_, err = stdoutW.Write([]byte(resp))
	require.NoError(t, err)

	wg.Wait()
	require.NoError(t, callErr)
	require.NotNil(t, callResult)

	var result struct {
		OK bool `json:"ok"`
	}
	require.NoError(t, json.Unmarshal(callResult.Result, &result))
	require.True(t, result.OK)

	// Close stdout write-end so readLoop receives EOF and exits.
	require.NoError(t, stdoutW.Close())
	<-c.Done()
}

func TestCall_ContextCancel(t *testing.T) {
	t.Parallel()

	c := NewACPClient(io.Discard, bufio.NewReader(bytes.NewReader(nil)), slog.Default())
	ctx, cancel := context.WithCancel(t.Context())
	go c.readLoop(ctx)

	// Cancel before any response arrives.
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	_, err := c.call(ctx, "test/method", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))

	// Wait for readLoop to exit.
	<-c.Done()
}

func TestCall_JSONRPCError(t *testing.T) {
	t.Parallel()

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	t.Cleanup(func() {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
	})

	c := NewACPClient(stdinW, bufio.NewReader(stdoutR), slog.Default())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go c.readLoop(ctx)

	var wg sync.WaitGroup
	var callErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, callErr = c.call(ctx, "test/fail", nil)
	}()

	// Read the request.
	line, err := bufio.NewReader(stdinR).ReadString('\n')
	require.NoError(t, err)

	var req JSONRPCRequest
	require.NoError(t, json.Unmarshal([]byte(line[:len(line)-1]), &req))

	// Write back an error response.
	errResp := `{"jsonrpc":"2.0","id":` + string(req.ID) + `,"error":{"code":-32600,"message":"bad"}}` + "\n"
	_, err = stdoutW.Write([]byte(errResp))
	require.NoError(t, err)

	wg.Wait()
	require.Error(t, callErr)

	var rpcErr *JSONRPCError
	require.True(t, errors.As(callErr, &rpcErr))
	require.Equal(t, -32600, rpcErr.Code)

	// Close stdout write-end so readLoop receives EOF and exits.
	require.NoError(t, stdoutW.Close())
	<-c.Done()
}

func TestDispatchResponse_Unmatched(t *testing.T) {
	t.Parallel()

	c := NewACPClient(io.Discard, bufio.NewReader(bytes.NewReader(nil)), slog.Default())

	// Dispatch a response for an unknown ID — should not panic.
	c.dispatchResponse(&JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      mustMarshal(999),
		Result:  mustMarshal(map[string]any{}),
	})
}

// ─── StartReadLoop + Done ──────────────────────────────────────────────────────

func TestStartReadLoop_AndDone(t *testing.T) {
	t.Parallel()

	c := NewACPClient(io.Discard, bufio.NewReader(bytes.NewReader(nil)), slog.Default())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c.StartReadLoop(ctx)

	// Cancel should cause Done to close.
	cancel()

	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done channel not closed after cancel")
	}
}

// ─── JSONRPCError ──────────────────────────────────────────────────────────────

func TestJSONRPCError_Error(t *testing.T) {
	t.Parallel()

	e := &JSONRPCError{Code: -32600, Message: "Invalid Request"}
	require.Contains(t, e.Error(), "-32600")
	require.Contains(t, e.Error(), "Invalid Request")
}

// ─── mustMarshal ───────────────────────────────────────────────────────────────

func TestMustMarshal(t *testing.T) {
	t.Parallel()

	raw := mustMarshal(map[string]string{"key": "val"})
	require.JSONEq(t, `{"key":"val"}`, string(raw))
}

func TestMustMarshal_Panic(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		mustMarshal(make(chan int)) // channels can't be marshaled
	})
}
