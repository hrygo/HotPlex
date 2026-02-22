package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/hrygo/hotplex"
	"github.com/hrygo/hotplex/event"
)

// OpenCodeHTTPHandler implements a server compatible with OpenCode's HTTP/SSE protocol.
type OpenCodeHTTPHandler struct {
	engine     hotplex.HotPlexClient
	controller *ExecutionController
	logger     *slog.Logger
	cors       *SecurityConfig

	// SSE broadcasting
	subscribers sync.Map // map[string]chan string
}

// NewOpenCodeHTTPHandler creates a new OpenCodeHTTPHandler instance.
func NewOpenCodeHTTPHandler(engine hotplex.HotPlexClient, logger *slog.Logger, cors *SecurityConfig) *OpenCodeHTTPHandler {
	return &OpenCodeHTTPHandler{
		engine:     engine,
		controller: NewExecutionController(engine, logger),
		logger:     logger,
		cors:       cors,
	}
}

// withAuth wraps an HTTP handler with authentication check.
func (s *OpenCodeHTTPHandler) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if authentication is enabled
		if s.cors != nil && s.cors.IsAPIKeyEnabled() {
			if !s.cors.validateAPIKey(r) {
				s.logger.Warn("OpenCode HTTP request rejected: invalid or missing API key",
					"remote_addr", r.RemoteAddr,
					"path", r.URL.Path)
				http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

// RegisterRoutes registers the OpenCode compatibility routes to a router.
func (s *OpenCodeHTTPHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/global/event", s.withAuth(s.handleGlobalEvent)).Methods("GET")
	r.HandleFunc("/session", s.withAuth(s.handleCreateSession)).Methods("POST")
	r.HandleFunc("/session/{id}/message", s.withAuth(s.handlePrompt)).Methods("POST")
	r.HandleFunc("/session/{id}/prompt_async", s.withAuth(s.handlePromptAsync)).Methods("POST")
	r.HandleFunc("/config", s.handleConfig).Methods("GET") // Public endpoint
}

func (s *OpenCodeHTTPHandler) handleGlobalEvent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	id := uuid.New().String()
	ch := make(chan string, 10)
	s.subscribers.Store(id, ch)
	defer s.subscribers.Delete(id)

	s.logger.Info("New OpenCode SSE subscriber", "id", id)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial connected event
	s.broadcastRaw(id, "server.connected", map[string]any{})

	for {
		select {
		case msg := <-ch:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *OpenCodeHTTPHandler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	// Simple session creation
	id := uuid.New().String()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"info": map[string]any{
			"id":        id,
			"projectID": "default",
			"directory": "/tmp/hotplex", // Default for now
			"title":     "New Session",
			"time": map[string]any{
				"created": time.Now().UnixMilli(),
			},
		},
	}); err != nil {
		s.logger.Error("Failed to encode session response", "error", err)
	}
}

func (s *OpenCodeHTTPHandler) handlePrompt(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["id"]

	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.logger.Info("OpenCode prompt request", "session_id", sessionID)

	// In OpenCode protocol, the response of this POST is usually empty or session info,
	// while the actual content flows through the SSE channel.
	go s.executeEngineTask(sessionID, req.Prompt, "", "")

	w.WriteHeader(http.StatusAccepted)
}

func (s *OpenCodeHTTPHandler) handlePromptAsync(w http.ResponseWriter, r *http.Request) {
	s.handlePrompt(w, r)
}

func (s *OpenCodeHTTPHandler) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"version":   "1.0.0",
		"providers": []string{"openai", "anthropic", "siliconflow"},
	}); err != nil {
		s.logger.Error("Failed to encode config response", "error", err)
	}
}

func (s *OpenCodeHTTPHandler) executeEngineTask(sessionID string, prompt string, agent string, model string) {
	ctx := context.Background()

	messageID := uuid.New().String()

	cb := func(eventType string, data any) error {
		pevt, ok := data.(*event.EventWithMeta)
		if !ok {
			return nil
		}

		// Map HotPlex events to OpenCode Parts
		part := s.mapToOpenCodePart(pevt, sessionID, messageID)
		if part != nil {
			s.broadcastEvent("message.part.updated", map[string]any{
				"part": part,
			})
		}
		return nil
	}

	execReq := ExecutionRequest{
		SessionID: sessionID,
		Prompt:    prompt,
		WorkDir:   "/tmp/hotplex", // Default workspace
		Timeout:   15 * time.Minute,
	}

	err := s.controller.Execute(ctx, execReq, cb)
	if err != nil {
		s.logger.Error("Engine execution failed", "error", err)
	}
}

func (s *OpenCodeHTTPHandler) mapToOpenCodePart(pevt *event.EventWithMeta, sessionID, messageID string) map[string]any {
	base := map[string]any{
		"id":        uuid.New().String(),
		"sessionID": sessionID,
		"messageID": messageID,
	}

	switch pevt.EventType {
	case "answer":
		base["type"] = "text"
		base["text"] = pevt.EventData
	case "thinking":
		base["type"] = "reasoning"
		base["text"] = pevt.EventData
	case "tool_use":
		base["type"] = "tool"
		base["tool"] = pevt.EventData
		base["state"] = map[string]any{
			"status": "running",
			"input":  pevt.Meta.InputSummary, // Use InputSummary for tool input
			"time": map[string]any{
				"start": time.Now().UnixMilli(),
			},
		}
	case "tool_result":
		base["type"] = "tool"
		base["state"] = map[string]any{
			"status": "completed",
			"output": pevt.EventData,
			"time": map[string]any{
				"end": time.Now().UnixMilli(),
			},
		}
	default:
		return nil
	}

	return base
}

func (s *OpenCodeHTTPHandler) broadcastEvent(typ string, properties any) {
	payload := map[string]any{
		"type":       typ,
		"properties": properties,
	}
	globalEvent := map[string]any{
		"directory": "/tmp/hotplex",
		"payload":   payload,
	}
	val, _ := json.Marshal(globalEvent)
	s.subscribers.Range(func(key, value any) bool {
		ch := value.(chan string)
		select {
		case ch <- string(val):
		default:
			// Full buffer, drop event or handle accordingly
		}
		return true
	})
}

func (s *OpenCodeHTTPHandler) broadcastRaw(subID string, typ string, properties any) {
	payload := map[string]any{
		"type":       typ,
		"properties": properties,
	}
	globalEvent := map[string]any{
		"directory": "/tmp/hotplex",
		"payload":   payload,
	}
	val, _ := json.Marshal(globalEvent)
	if ch, ok := s.subscribers.Load(subID); ok {
		ch.(chan string) <- string(val)
	}
}
