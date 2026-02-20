package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hrygo/hotplex/pkg/hotplex"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// For MVP, allow all origins
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ClientRequest represents the JSON payload expected from the WebSocket client.
type ClientRequest struct {
	Type      string `json:"type"`       // "execute" or "stop"
	SessionID string `json:"session_id"` // Provide session_id to hot-multiplex
	Prompt    string `json:"prompt"`     // The user input (for "execute")
	WorkDir   string `json:"work_dir"`   // Working directory for CLI (for "execute")
	Reason    string `json:"reason"`     // Reason for stopping (for "stop")
}

// ServerResponse represents the JSON payload sent to the WebSocket client.
type ServerResponse struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// connWriter wraps a websocket.Conn with a mutex to prevent concurrent writes.
// gorilla/websocket does NOT support concurrent WriteMessage calls;
// this wrapper serializes all writes to the connection.
type connWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (cw *connWriter) writeJSON(event string, data any) {
	resp := ServerResponse{Event: event, Data: data}
	val, _ := json.Marshal(resp)
	cw.mu.Lock()
	defer cw.mu.Unlock()
	_ = cw.conn.WriteMessage(websocket.TextMessage, val)
}

// WebSocketHandler manages a WebSocket connection to a HotPlex Engine.
type WebSocketHandler struct {
	engine hotplex.HotPlexClient
	logger *slog.Logger
}

// NewWebSocketHandler creates a new handler.
func NewWebSocketHandler(engine hotplex.HotPlexClient, logger *slog.Logger) *WebSocketHandler {
	return &WebSocketHandler{
		engine: engine,
		logger: logger,
	}
}

// ServeHTTP upgrades the HTTP connection and starts the read loop.
func (h *WebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("Failed to upgrade websocket connection", "error", err)
		return
	}
	defer func() { _ = conn.Close() }()

	cw := &connWriter{conn: conn}

	h.logger.Info("Client connected via WebSocket", "addr", r.RemoteAddr)

	for {
		// Read message from client
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.logger.Error("WebSocket closed unexpectedly", "error", err)
			} else {
				h.logger.Info("WebSocket closed normally", "addr", r.RemoteAddr)
			}
			return
		}

		if messageType != websocket.TextMessage {
			h.logger.Warn("Ignoring non-text message type", "type", messageType)
			continue
		}

		var req ClientRequest
		if err := json.Unmarshal(p, &req); err != nil {
			cw.writeJSON("error", map[string]string{"message": "Invalid JSON payload: " + err.Error()})
			continue
		}

		switch req.Type {
		case "execute":
			h.handleExecute(cw, req)
		case "stop":
			h.handleStop(cw, req)
		default:
			cw.writeJSON("error", map[string]string{"message": "Unknown request type: " + req.Type})
		}
	}
}

func (h *WebSocketHandler) handleExecute(cw *connWriter, req ClientRequest) {
	if req.Prompt == "" {
		cw.writeJSON("error", map[string]string{"message": "prompt cannot be empty"})
		return
	}

	sessionID := req.SessionID
	if sessionID == "" {
		// Auto-generate session ID if not provided
		sessionID = uuid.New().String()
		h.logger.Debug("Auto-generated session ID", "session_id", sessionID)
	}

	workDir := req.WorkDir
	if workDir == "" {
		workDir = "/tmp/hotplex_sandbox" // Fallback MVP directory
	}

	cfg := &hotplex.Config{
		WorkDir:   workDir,
		SessionID: sessionID,
	}

	h.logger.Info("Handling execute request", "session_id", sessionID, "prompt_length", len(req.Prompt))

	// Define the callback that bridges HotPlex Engine events to WebSocket messages.
	// All writes to the connection go through connWriter, which is mutex-protected.
	cb := func(eventType string, data any) error {
		cw.writeJSON(eventType, data)
		return nil
	}

	// HotPlex execution
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	err := h.engine.Execute(ctx, cfg, req.Prompt, cb)
	if err != nil {
		cw.writeJSON("error", map[string]string{"message": "Execution failed: " + err.Error()})
		return
	}

	// Send completion signal
	cw.writeJSON("completed", map[string]string{"session_id": sessionID})
}

func (h *WebSocketHandler) handleStop(cw *connWriter, req ClientRequest) {
	if req.SessionID == "" {
		cw.writeJSON("error", map[string]string{"message": "session_id is required for stop"})
		return
	}

	reason := req.Reason
	if reason == "" {
		reason = "client requested stop"
	}

	h.logger.Info("Handling stop request", "session_id", req.SessionID, "reason", reason)

	// Access the underlying Engine to call StopSession
	if eng, ok := h.engine.(*hotplex.Engine); ok {
		if err := eng.StopSession(req.SessionID, reason); err != nil {
			cw.writeJSON("error", map[string]string{"message": "Stop failed: " + err.Error()})
			return
		}
	}

	cw.writeJSON("stopped", map[string]string{"session_id": req.SessionID})
}
