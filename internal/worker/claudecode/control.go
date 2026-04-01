package claudecode

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// ControlRequest represents a control request from Claude Code.
type ControlRequest struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Response  json.RawMessage `json:"response"`
}

// ControlResponse represents a response to Claude Code.
type ControlResponse struct {
	Type     string          `json:"type"`
	Response ResponsePayload `json:"response"`
}

// ResponsePayload represents the response payload.
type ResponsePayload struct {
	Subtype   string         `json:"subtype"`
	RequestID string         `json:"request_id"`
	Response  map[string]any `json:"response,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// ControlHandler handles bidirectional control protocol.
type ControlHandler struct {
	mu    sync.Mutex
	log   *slog.Logger
	stdin io.Writer // CLI stdin
}

// NewControlHandler creates a new ControlHandler instance.
func NewControlHandler(log *slog.Logger, stdin io.Writer) *ControlHandler {
	return &ControlHandler{
		log:   log,
		stdin: stdin,
	}
}

// HandleRequest processes a control_request from Claude Code.
// Returns WorkerEvent for gateway-forwarded requests (can_use_tool).
// Returns nil for internally-handled requests (interrupt, set_*, mcp_*).
func (h *ControlHandler) HandleRequest(req *ControlRequest) (*WorkerEvent, error) {
	// Parse the response payload to determine subtype
	var payload struct {
		Subtype  string          `json:"subtype"`
		ToolName string          `json:"tool_name,omitempty"`
		Input    json.RawMessage `json:"input,omitempty"`
	}
	if err := json.Unmarshal(req.Response, &payload); err != nil {
		return nil, fmt.Errorf("control: unmarshal response: %w", err)
	}

	switch payload.Subtype {
	case string(ControlCanUseTool):
		// Forward to client as permission_request
		var input map[string]any
		if len(payload.Input) > 0 {
			_ = json.Unmarshal(payload.Input, &input)
		}

		return &WorkerEvent{
			Type: EventControl,
			Payload: &PermissionRequestPayload{
				RequestID: req.RequestID,
				ToolName:  payload.ToolName,
				Input:     input,
			},
		}, nil

	case string(ControlInterrupt):
		// Internal interrupt signal (don't forward to client)
		h.log.Debug("control: received interrupt signal")
		return nil, nil

	case string(ControlSetPermissionMode), string(ControlSetModel), string(ControlSetMaxThinkingTokens):
		// Auto-approve configuration changes
		resp := &ControlResponse{
			Type: "control_response",
			Response: ResponsePayload{
				Subtype:   "success",
				RequestID: req.RequestID,
				Response:  map[string]any{"status": "ok"},
			},
		}
		return nil, h.SendResponse(resp)

	case string(ControlMCPStatus), string(ControlMCPSetServers), string(ControlMCPMessage):
		// Auto-approve MCP requests (P1: could add more sophisticated handling)
		resp := &ControlResponse{
			Type: "control_response",
			Response: ResponsePayload{
				Subtype:   "success",
				RequestID: req.RequestID,
				Response:  map[string]any{"status": "ok"},
			},
		}
		return nil, h.SendResponse(resp)

	default:
		// Ignore unknown control requests
		h.log.Warn("control: unknown request subtype", "subtype", payload.Subtype)
		return nil, nil
	}
}

// SendResponse sends a control_response to Claude Code via stdin.
func (h *ControlHandler) SendResponse(resp *ControlResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("control: marshal response: %w", err)
	}
	data = append(data, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err = h.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("control: write response: %w", err)
	}

	h.log.Debug("control: sent response", "request_id", resp.Response.RequestID)
	return nil
}

// SendPermissionResponse sends a user's permission decision back to Claude Code.
// This is called when the client responds to a permission_request.
func (h *ControlHandler) SendPermissionResponse(reqID string, allowed bool, reason string) error {
	resp := &ControlResponse{
		Type: "control_response",
		Response: ResponsePayload{
			Subtype:   "success",
			RequestID: reqID,
			Response: map[string]any{
				"allowed": allowed,
				"reason":  reason,
			},
		},
	}
	return h.SendResponse(resp)
}
