package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/pkg/events"
)

const (
	acpQuestionRequestMethod    = "session/request_question"
	acpElicitationRequestMethod = "session/request_elicitation"
	acpInvalidParamsErrorCode   = -32602
	defaultQuestionToolName     = "question"
)

// acpQuestionParams accepts both the single-question and the multi-question
// request shapes used by ACP-compatible agents. The raw question fields let us
// reject a wrong JSON type instead of silently turning it into an empty AEP
// request.
type acpQuestionParams struct {
	ToolName           string                  `json:"toolName"`
	ToolNameSnake      string                  `json:"tool_name"`
	Question           json.RawMessage         `json:"question"`
	Header             string                  `json:"header"`
	Options            []events.QuestionOption `json:"options"`
	MultiSelect        *bool                   `json:"multiSelect"`
	MultiSelectSnake   *bool                   `json:"multi_select"`
	IsMultiSelect      *bool                   `json:"isMultiSelect"`
	IsMultiSelectSnake *bool                   `json:"is_multi_select"`
	Multiple           *bool                   `json:"multiple"`
	Questions          json.RawMessage         `json:"questions"`
}

type acpQuestion struct {
	ID                 string                  `json:"id"`
	Question           string                  `json:"question"`
	Header             string                  `json:"header"`
	Options            []events.QuestionOption `json:"options"`
	MultiSelect        *bool                   `json:"multiSelect"`
	MultiSelectSnake   *bool                   `json:"multi_select"`
	IsMultiSelect      *bool                   `json:"isMultiSelect"`
	IsMultiSelectSnake *bool                   `json:"is_multi_select"`
	Multiple           *bool                   `json:"multiple"`
}

type acpElicitationParams struct {
	MCPServerName        string          `json:"mcpServerName"`
	MCPServerNameSnake   string          `json:"mcp_server_name"`
	Message              string          `json:"message"`
	Mode                 string          `json:"mode"`
	URL                  string          `json:"url"`
	ElicitationID        string          `json:"elicitationId"`
	ElicitationIDSnake   string          `json:"elicitation_id"`
	RequestedSchema      json.RawMessage `json:"requestedSchema"`
	RequestedSchemaSnake json.RawMessage `json:"requested_schema"`
	Schema               json.RawMessage `json:"schema"`
}

// mapQuestionRequest converts a known ACP question request into the existing
// AEP question_request payload. JSON-RPC IDs remain opaque strings here so the
// worker can use the same value as the pending request key and write the exact
// original raw ID back in its response.
func mapQuestionRequest(mapper *ACPMapper, req *JSONRPCRequest) (*events.Envelope, error) {
	if req == nil {
		return nil, invalidInteractionParams(acpQuestionRequestMethod, "request is nil")
	}
	var params acpQuestionParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, invalidInteractionParams(acpQuestionRequestMethod, "invalid JSON: %v", err)
	}
	if len(params.Question) > 0 && len(params.Questions) > 0 {
		return nil, invalidInteractionParams(acpQuestionRequestMethod, "question and questions are mutually exclusive")
	}

	toolName := params.ToolName
	if toolName == "" {
		toolName = params.ToolNameSnake
	}
	if toolName == "" {
		toolName = defaultQuestionToolName
	}

	var questions []events.Question
	switch {
	case len(params.Questions) > 0:
		var rawQuestions []acpQuestion
		if err := json.Unmarshal(params.Questions, &rawQuestions); err != nil {
			return nil, invalidInteractionParams(acpQuestionRequestMethod, "questions must be an array: %v", err)
		}
		if len(rawQuestions) == 0 {
			return nil, invalidInteractionParams(acpQuestionRequestMethod, "questions must not be empty")
		}
		questions = make([]events.Question, 0, len(rawQuestions))
		for i, rawQuestion := range rawQuestions {
			question, err := rawQuestion.toEventQuestion()
			if err != nil {
				return nil, invalidInteractionParams(acpQuestionRequestMethod, "questions[%d]: %v", i, err)
			}
			questions = append(questions, question)
		}
	case len(params.Question) > 0:
		var questionText string
		if err := json.Unmarshal(params.Question, &questionText); err != nil {
			return nil, invalidInteractionParams(acpQuestionRequestMethod, "question must be a string: %v", err)
		}
		if strings.TrimSpace(questionText) == "" {
			return nil, invalidInteractionParams(acpQuestionRequestMethod, "question must not be empty")
		}
		questions = []events.Question{{
			Question:    questionText,
			Header:      params.Header,
			Options:     params.Options,
			MultiSelect: firstBool(params.MultiSelect, params.MultiSelectSnake, params.IsMultiSelect, params.IsMultiSelectSnake, params.Multiple),
		}}
	default:
		return nil, invalidInteractionParams(acpQuestionRequestMethod, "question or questions is required")
	}

	return mapper.newEnvelope(events.QuestionRequest, events.QuestionRequestData{
		ID:        string(req.ID),
		ToolName:  toolName,
		Questions: questions,
	}), nil
}

func (q acpQuestion) toEventQuestion() (events.Question, error) {
	if strings.TrimSpace(q.Question) == "" {
		return events.Question{}, fmt.Errorf("question must not be empty")
	}
	return events.Question{
		ID:          q.ID,
		Question:    q.Question,
		Header:      q.Header,
		Options:     q.Options,
		MultiSelect: firstBool(q.MultiSelect, q.MultiSelectSnake, q.IsMultiSelect, q.IsMultiSelectSnake, q.Multiple),
	}, nil
}

// mapElicitationRequest converts an ACP elicitation request into the existing
// AEP elicitation_request payload. Form mode carries requestedSchema; URL mode
// may carry a URL instead, matching MCP's two elicitation forms.
func mapElicitationRequest(mapper *ACPMapper, req *JSONRPCRequest) (*events.Envelope, error) {
	if req == nil {
		return nil, invalidInteractionParams(acpElicitationRequestMethod, "request is nil")
	}
	var params acpElicitationParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, invalidInteractionParams(acpElicitationRequestMethod, "invalid JSON: %v", err)
	}
	if strings.TrimSpace(params.Message) == "" {
		return nil, invalidInteractionParams(acpElicitationRequestMethod, "message must not be empty")
	}

	mcpServerName := params.MCPServerName
	if mcpServerName == "" {
		mcpServerName = params.MCPServerNameSnake
	}
	elicitationID := params.ElicitationID
	if elicitationID == "" {
		elicitationID = params.ElicitationIDSnake
	}
	requestedSchema, err := decodeRequestedSchema(params.RequestedSchema, params.RequestedSchemaSnake, params.Schema)
	if err != nil {
		return nil, invalidInteractionParams(acpElicitationRequestMethod, "%v", err)
	}
	mode := strings.ToLower(strings.TrimSpace(params.Mode))
	switch mode {
	case "":
		if requestedSchema != nil {
			mode = "form"
		} else if strings.TrimSpace(params.URL) != "" {
			mode = "url"
		} else {
			return nil, invalidInteractionParams(acpElicitationRequestMethod, "requestedSchema or url is required")
		}
	case "form":
		if requestedSchema == nil {
			return nil, invalidInteractionParams(acpElicitationRequestMethod, "requestedSchema is required for form mode")
		}
	case "url":
		if strings.TrimSpace(params.URL) == "" {
			return nil, invalidInteractionParams(acpElicitationRequestMethod, "url is required for url mode")
		}
	default:
		return nil, invalidInteractionParams(acpElicitationRequestMethod, "mode must be form or url")
	}

	return mapper.newEnvelope(events.ElicitationRequest, events.ElicitationRequestData{
		ID:              string(req.ID),
		MCPServerName:   mcpServerName,
		Message:         params.Message,
		Mode:            mode,
		URL:             params.URL,
		ElicitationID:   elicitationID,
		RequestedSchema: requestedSchema,
	}), nil
}

func decodeRequestedSchema(camel, snake, generic json.RawMessage) (map[string]any, error) {
	raw := camel
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = snake
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = generic
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("requestedSchema must be an object: %w", err)
	}
	if schema == nil {
		return nil, fmt.Errorf("requestedSchema must be an object")
	}
	return schema, nil
}

func firstBool(values ...*bool) bool {
	for _, value := range values {
		if value != nil {
			return *value
		}
	}
	return false
}

func invalidInteractionParams(method, format string, args ...any) error {
	return fmt.Errorf("acp: invalid %s params: %s", method, fmt.Sprintf(format, args...))
}

type interactionRequestMapper func(*ACPMapper, *JSONRPCRequest) (*events.Envelope, error)

// handleKnownInteractionRequest validates and forwards a known interaction.
// Pending state is installed only after validation succeeds, so malformed
// requests cannot create an entry that no response can safely resolve.
func (w *Worker) handleKnownInteractionRequest(
	ctx context.Context,
	req *JSONRPCRequest,
	conn *acpConn,
	mapRequest interactionRequestMapper,
) {
	env, err := mapRequest(w.mapper, req)
	if err != nil {
		if w.Log != nil {
			w.Log.Warn("acp: invalid interaction request", "method", req.Method, "id", string(req.ID), "err", err)
		}
		if w.client == nil {
			return
		}
		if writeErr := w.client.respondRequestError(ctx, req.ID, acpInvalidParamsErrorCode, err.Error(), nil); writeErr != nil && w.Log != nil {
			w.Log.Warn("acp: failed to reject invalid interaction request", "method", req.Method, "id", string(req.ID), "err", writeErr)
		}
		return
	}
	w.pendingRequests.Store(string(req.ID), req)
	conn.TrySend(env)
}
