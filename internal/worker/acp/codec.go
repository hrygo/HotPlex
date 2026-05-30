// Package acp implements a universal ACP (Agent Client Protocol) v1 worker
// that connects to any ACP-compatible agent via stdio (JSON-RPC 2.0 over NDJSON).
package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
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
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// ─── NDJSON Codec ────────────────────────────────────────────────────────────

// WriteMessage serializes a JSON-RPC message and writes it as a single NDJSON line.
func WriteMessage(w io.Writer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("acp codec: marshal: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("acp codec: write: %w", err)
	}
	if _, err := w.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("acp codec: write newline: %w", err)
	}
	return nil
}

// ReadMessage reads one NDJSON line and dispatches it as the correct JSON-RPC type.
// Returns *JSONRPCResponse (has id, no method), *JSONRPCRequest (has id + method),
// or *JSONRPCNotification (no id).
func ReadMessage(r *bufio.Reader) (any, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("acp codec: read line: %w", err)
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil, nil // skip blank lines
	}

	// Peek at the raw message to determine type without full unmarshal.
	var probe struct {
		ID     *json.RawMessage `json:"id"`
		Method *string          `json:"method"`
		Result json.RawMessage  `json:"result"`
		Error  *JSONRPCError    `json:"error"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return nil, fmt.Errorf("acp codec: parse json: %w", err)
	}

	switch {
	case probe.ID != nil && probe.Method != nil:
		// Request: has id + method (server-initiated, e.g. request_permission).
		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			return nil, fmt.Errorf("acp codec: unmarshal request: %w", err)
		}
		return &req, nil

	case probe.ID != nil:
		// Response: has id, no method.
		var resp JSONRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, fmt.Errorf("acp codec: unmarshal response: %w", err)
		}
		return &resp, nil

	case probe.Method != nil:
		// Notification: no id, has method.
		var notif JSONRPCNotification
		if err := json.Unmarshal(line, &notif); err != nil {
			return nil, fmt.Errorf("acp codec: unmarshal notification: %w", err)
		}
		return &notif, nil

	default:
		return nil, fmt.Errorf("acp codec: unrecognized message: %s", string(line))
	}
}
