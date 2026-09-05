package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestHandleServerRequest_QuestionNormalizesToAEP(t *testing.T) {
	t.Parallel()

	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil), mapper: newTestMapper()}
	conn := newACPConn("user-1", "session-1", nil)
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`7`),
		Method:  "session/request_question",
		Params: mustMarshal(map[string]any{
			"sessionId": "acp-session-1",
			"question":  "Choose a runtime",
			"header":    "Runtime",
			"options": []map[string]string{
				{"label": "Go", "description": "Use Go"},
			},
		}),
	}

	w.handleServerRequest(context.Background(), req, conn)
	env := <-conn.Recv()
	require.Equal(t, events.QuestionRequest, env.Event.Type)

	data, ok := env.Event.Data.(events.QuestionRequestData)
	require.True(t, ok)
	require.Equal(t, "7", data.ID)
	require.Len(t, data.Questions, 1)
	require.Equal(t, "Choose a runtime", data.Questions[0].Question)
	pending, ok := w.pendingRequests.Load(data.ID)
	require.True(t, ok)
	require.Same(t, req, pending)
}

func TestHandleServerRequest_QuestionArrayPreservesLiteralAndID(t *testing.T) {
	t.Parallel()

	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil), mapper: newTestMapper()}
	conn := newACPConn("user-1", "session-1", nil)
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"question-array"`),
		Method:  acpQuestionRequestMethod,
		Params: mustMarshal(map[string]any{
			"questions": []map[string]any{
				{
					"id":          "first",
					"question":    "  keep this literal  ",
					"header":      "First",
					"multiSelect": true,
					"options":     []map[string]string{{"label": "A"}},
				},
				{
					"question": "Second question",
					"header":   "Second",
				},
			},
		}),
	}

	w.handleServerRequest(context.Background(), req, conn)
	env := <-conn.Recv()
	require.Equal(t, events.QuestionRequest, env.Event.Type)
	data, ok := env.Event.Data.(events.QuestionRequestData)
	require.True(t, ok)
	require.Equal(t, string(req.ID), data.ID)
	require.Len(t, data.Questions, 2)
	require.Equal(t, "  keep this literal  ", data.Questions[0].Question)
	require.True(t, data.Questions[0].MultiSelect)
	require.Equal(t, "Second question", data.Questions[1].Question)
}

func TestHandleServerRequest_ElicitationNormalizesSchema(t *testing.T) {
	t.Parallel()

	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil), mapper: newTestMapper()}
	conn := newACPConn("user-1", "session-1", nil)
	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"elicitation-1"`),
		Method:  acpElicitationRequestMethod,
		Params: mustMarshal(map[string]any{
			"mcpServerName": "forms",
			"message":       "Please provide details",
			"requestedSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
		}),
	}

	w.handleServerRequest(context.Background(), req, conn)
	env := <-conn.Recv()
	require.Equal(t, events.ElicitationRequest, env.Event.Type)
	data, ok := env.Event.Data.(events.ElicitationRequestData)
	require.True(t, ok)
	require.Equal(t, string(req.ID), data.ID)
	require.Equal(t, "forms", data.MCPServerName)
	require.Equal(t, "Please provide details", data.Message)
	require.Equal(t, "form", data.Mode)
	require.Equal(t, "object", data.RequestedSchema["type"])
}

func TestMapElicitationRequest_ModeValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		params     map[string]any
		wantMode   string
		wantSchema bool
		wantErr    bool
	}{
		{
			name:       "schema defaults to form",
			params:     map[string]any{"message": "fill", "requestedSchema": map[string]any{"type": "object"}},
			wantMode:   "form",
			wantSchema: true,
		},
		{
			name:     "url defaults to url",
			params:   map[string]any{"message": "open", "url": "https://example.test/form"},
			wantMode: "url",
		},
		{
			name:       "explicit form",
			params:     map[string]any{"message": "fill", "mode": "form", "requestedSchema": map[string]any{}},
			wantMode:   "form",
			wantSchema: true,
		},
		{
			name:    "form requires schema",
			params:  map[string]any{"message": "fill", "mode": "form"},
			wantErr: true,
		},
		{
			name:    "url requires url",
			params:  map[string]any{"message": "open", "mode": "url"},
			wantErr: true,
		},
		{
			name:    "unknown mode",
			params:  map[string]any{"message": "fill", "mode": "other", "requestedSchema": map[string]any{}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := &JSONRPCRequest{ID: json.RawMessage(`11`), Params: mustMarshal(tt.params)}
			env, err := mapElicitationRequest(newTestMapper(), req)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			data := env.Event.Data.(events.ElicitationRequestData)
			require.Equal(t, tt.wantMode, data.Mode)
			require.Equal(t, tt.wantSchema, data.RequestedSchema != nil)
		})
	}
}

func TestHandleServerRequest_InvalidKnownInteractionRejectsWithoutPending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		params map[string]any
	}{
		{
			name:   "question must be string",
			method: acpQuestionRequestMethod,
			params: map[string]any{"question": 42},
		},
		{
			name:   "elicitation mode must be known",
			method: acpElicitationRequestMethod,
			params: map[string]any{"message": "fill", "mode": "other", "requestedSchema": map[string]any{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			w := &Worker{
				BaseWorker: base.NewBaseWorker(nil, nil),
				client:     NewACPClient(&output, strings.NewReader(""), nil),
				mapper:     newTestMapper(),
			}
			conn := newACPConn("user-1", "session-1", nil)
			req := &JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`"invalid"`),
				Method:  tt.method,
				Params:  mustMarshal(tt.params),
			}

			w.handleServerRequest(context.Background(), req, conn)
			_, pending := w.pendingRequests.Load(string(req.ID))
			require.False(t, pending)

			var response JSONRPCResponse
			require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
			require.Equal(t, string(req.ID), string(response.ID))
			require.NotNil(t, response.Error)
			require.Equal(t, acpInvalidParamsErrorCode, response.Error.Code)
		})
	}
}

func TestRespondRequestError_PreservesJSONRPCID(t *testing.T) {
	t.Parallel()

	for _, id := range []json.RawMessage{json.RawMessage(`7`), json.RawMessage(`"question-7"`)} {
		id := id
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			client := NewACPClient(&output, strings.NewReader(""), nil)
			require.NoError(t, client.respondRequestError(context.Background(), id, acpInvalidParamsErrorCode, "bad params", nil))

			var response JSONRPCResponse
			require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
			require.Equal(t, string(id), string(response.ID))
			require.Equal(t, acpInvalidParamsErrorCode, response.Error.Code)
		})
	}
}

func TestRespondToServerRequest_WriteFailureKeepsPendingForRetry(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	require.NoError(t, writer.Close())
	requestID := json.RawMessage(`"retry-question"`)
	w := &Worker{
		BaseWorker: base.NewBaseWorker(nil, nil),
		client:     NewACPClient(writer, strings.NewReader(""), nil),
	}
	w.pendingRequests.Store(string(requestID), &JSONRPCRequest{ID: requestID, Method: acpQuestionRequestMethod})

	err := w.HandleQuestionResponse(context.Background(), string(requestID), map[string]string{"Choose": "Go"})
	require.Error(t, err)
	_, ok := w.pendingRequests.Load(string(requestID))
	require.True(t, ok)

	var output bytes.Buffer
	w.client = NewACPClient(&output, strings.NewReader(""), nil)
	require.NoError(t, w.HandleQuestionResponse(context.Background(), string(requestID), map[string]string{"Choose": "Go"}))
	_, ok = w.pendingRequests.Load(string(requestID))
	require.False(t, ok)
	var response JSONRPCResponse
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
	require.Equal(t, string(requestID), string(response.ID))
	_ = reader.Close()
}

func TestKnownInteractionRequest_RoundTripUsesTypedEventID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		request  *JSONRPCRequest
		wantKind events.Kind
		respond  func(context.Context, *Worker, string) error
		wantJSON string
	}{
		{
			name: "numeric question",
			request: &JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`7`),
				Method:  acpQuestionRequestMethod,
				Params: mustMarshal(map[string]any{
					"question": "Choose a runtime?",
					"options":  []map[string]string{{"label": "Go"}},
				}),
			},
			wantKind: events.QuestionRequest,
			respond: func(ctx context.Context, w *Worker, id string) error {
				return w.HandleQuestionResponseOptions(ctx, id, map[string][]string{
					"Choose a runtime?": {"Go"},
				}, []string{"Choose a runtime?"})
			},
			wantJSON: `{"Choose a runtime?":["Go"]}`,
		},
		{
			name: "string multi question preserves whitespace",
			request: &JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`"question-array"`),
				Method:  acpQuestionRequestMethod,
				Params: mustMarshal(map[string]any{
					"questions": []map[string]any{
						{
							"question":    "  keep this literal  ",
							"multiSelect": true,
							"options":     []map[string]string{{"label": "A"}, {"label": "B"}},
						},
					},
				}),
			},
			wantKind: events.QuestionRequest,
			respond: func(ctx context.Context, w *Worker, id string) error {
				return w.HandleQuestionResponseOptions(ctx, id, map[string][]string{
					"  keep this literal  ": {"A", "B"},
				}, []string{"  keep this literal  "})
			},
			wantJSON: `{"  keep this literal  ":["A","B"]}`,
		},
		{
			name: "string elicitation accept with typed content",
			request: &JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`"elicitation-1"`),
				Method:  acpElicitationRequestMethod,
				Params: mustMarshal(map[string]any{
					"mcpServerName":   "forms",
					"message":         "Provide details",
					"requestedSchema": map[string]any{"type": "object"},
				}),
			},
			wantKind: events.ElicitationRequest,
			respond: func(ctx context.Context, w *Worker, id string) error {
				return w.HandleElicitationResponse(ctx, id, "accept", map[string]any{
					"name":  "Ada",
					"count": float64(2),
				})
			},
			wantJSON: `{"action":"accept","content":{"count":2,"name":"Ada"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			w := &Worker{
				BaseWorker: base.NewBaseWorker(nil, nil),
				client:     NewACPClient(&output, strings.NewReader(""), nil),
				mapper:     newTestMapper(),
			}
			conn := newACPConn("user-1", "session-1", nil)

			w.handleServerRequest(context.Background(), tt.request, conn)
			env := <-conn.Recv()
			require.Equal(t, tt.wantKind, env.Event.Type)

			var eventID string
			switch data := env.Event.Data.(type) {
			case events.QuestionRequestData:
				eventID = data.ID
			case events.ElicitationRequestData:
				eventID = data.ID
			default:
				t.Fatalf("unexpected interaction data type %T", env.Event.Data)
			}
			require.Equal(t, string(tt.request.ID), eventID)
			_, pending := w.pendingRequests.Load(eventID)
			require.True(t, pending)

			require.NoError(t, tt.respond(context.Background(), w, eventID))
			_, pending = w.pendingRequests.Load(eventID)
			require.False(t, pending)

			var response JSONRPCResponse
			require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response))
			require.Equal(t, "2.0", response.JSONRPC)
			require.Equal(t, string(tt.request.ID), string(response.ID))
			require.Nil(t, response.Error)
			require.JSONEq(t, tt.wantJSON, string(response.Result))
		})
	}
}
