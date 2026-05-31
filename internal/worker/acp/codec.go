// Package acp implements a universal ACP (Agent Client Protocol) v1 worker
// that connects to any ACP-compatible agent via stdio (JSON-RPC 2.0 over NDJSON).
package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ─── JSON-RPC 2.0 Message Types ──────────────────────────────────────────────

// JSONRPCRequest is a JSON-RPC 2.0 request (has id + method).
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response (has id + result or error).
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCNotification is a JSON-RPC 2.0 notification (no id, has method).
type JSONRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCError is a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("JSON-RPC error %d: %s: %s", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// ─── NDJSON Codec ────────────────────────────────────────────────────────────

// Per-line size limits for bufio.Scanner (matching proc/manager pattern).
const (
	scannerInitSize = 64 * 1024        // 64 KB initial capacity
	scannerMaxSize  = 10 * 1024 * 1024 // 10 MB hard cap
)

// NewScanner creates a bufio.Scanner with the standard ACP size limits.
func NewScanner(r io.Reader) *bufio.Scanner {
	scan := bufio.NewScanner(r)
	buf := make([]byte, scannerInitSize)
	scan.Buffer(buf, scannerMaxSize)
	return scan
}

// WriteMessage serializes a JSON-RPC message and writes it as a single NDJSON line.
func WriteMessage(w io.Writer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("acp codec: marshal: %w", err)
	}
	// Allocate independent slice to avoid mutating json.Marshal's backing array,
	// which may be shared with json.RawMessage fields in the marshaled struct.
	line := make([]byte, len(data)+1)
	copy(line, data)
	line[len(data)] = '\n'
	if _, err := w.Write(line); err != nil {
		return fmt.Errorf("acp codec: write: %w", err)
	}
	return nil
}

// ReadMessage reads one NDJSON line and dispatches it as the correct JSON-RPC type.
// Returns *JSONRPCResponse (has id, no method), *JSONRPCRequest (has id + method),
// or *JSONRPCNotification (no id).
func ReadMessage(scan *bufio.Scanner) (any, error) {
	if !scan.Scan() {
		if err := scan.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				return nil, fmt.Errorf("acp codec: line exceeds %d byte limit", scannerMaxSize)
			}
			return nil, fmt.Errorf("acp codec: read line: %w", err)
		}
		return nil, io.EOF
	}

	line := bytes.TrimSpace(scan.Bytes())
	if len(line) == 0 {
		return nil, nil // skip blank lines
	}

	// Single-pass unmarshal: extract discriminator fields alongside full payload.
	var raw struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  *string         `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *JSONRPCError   `json:"error,omitempty"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, fmt.Errorf("acp codec: parse json: %w", err)
	}

	// Validate JSON-RPC version field.
	if raw.JSONRPC != "2.0" {
		return nil, fmt.Errorf("acp codec: invalid jsonrpc version %q, want %q", raw.JSONRPC, "2.0")
	}

	switch {
	case raw.ID != nil && raw.Method != nil:
		// Request: has id + method (server-initiated, e.g. request_permission).
		return &JSONRPCRequest{
			JSONRPC: raw.JSONRPC,
			ID:      raw.ID,
			Method:  *raw.Method,
			Params:  raw.Params,
		}, nil

	case raw.ID != nil:
		// Response: has id, no method.
		return &JSONRPCResponse{
			JSONRPC: raw.JSONRPC,
			ID:      raw.ID,
			Result:  raw.Result,
			Error:   raw.Error,
		}, nil

	case raw.Method != nil:
		// Notification: no id, has method.
		return &JSONRPCNotification{
			JSONRPC: raw.JSONRPC,
			Method:  *raw.Method,
			Params:  raw.Params,
		}, nil

	default:
		return nil, fmt.Errorf("acp codec: unrecognized message: %s", string(line))
	}
}
