package base

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidSchema is returned when the interaction response payload does not match the expected schema.
var ErrInvalidSchema = errors.New("invalid interaction response schema")

// MetadataHandler dispatches control responses extracted from Input metadata.
// Both ClaudeCode (stdin control) and OCS (HTTP POST) adapters implement this
// to unify the metadata type-switch pattern.
type MetadataHandler interface {
	HandlePermissionResponse(ctx context.Context, reqID string, allowed bool, reason string) error
	HandleQuestionResponse(ctx context.Context, reqID string, answers map[string]string) error
	HandleElicitationResponse(ctx context.Context, reqID string, action string, content map[string]any) error
}

// OrderedQuestionResponseHandler is implemented by workers whose native
// protocol needs the original question order in addition to the standard
// question-text-to-answer map.
type OrderedQuestionResponseHandler interface {
	HandleQuestionResponseWithOrder(ctx context.Context, reqID string, answers map[string]string, questionOrder []string) error
}

// MultiAnswerQuestionResponseHandler preserves multiple selected values per
// question for native protocols that support them. Workers without this
// optional interface receive a deterministic comma-joined compatibility map.
type MultiAnswerQuestionResponseHandler interface {
	HandleQuestionResponseOptions(ctx context.Context, reqID string, answers map[string][]string, questionOrder []string) error
}

// DispatchMetadata checks metadata for control response keys and dispatches
// to the handler. Returns (true, nil) if handled, (false, nil) if no match,
// or (true, err) on dispatch failure.
func DispatchMetadata(ctx context.Context, metadata map[string]any, h MetadataHandler) (bool, error) {
	if metadata == nil {
		return false, nil
	}
	if permResp, ok := metadata["permission_response"].(map[string]any); ok {
		reqID, _ := permResp["id"].(string)
		if reqID == "" {
			reqID, _ = permResp["request_id"].(string)
		}
		allowed, _ := permResp["allowed"].(bool)
		reason, _ := permResp["reason"].(string)
		return true, h.HandlePermissionResponse(ctx, reqID, allowed, reason)
	}
	if qResp, ok := metadata["question_response"].(map[string]any); ok {
		reqID, _ := qResp["id"].(string)
		var answerOptions map[string][]string
		if answersRaw := qResp["answers"]; answersRaw != nil {
			var err error
			answerOptions, err = parseAnswerOptions(answersRaw)
			if err != nil {
				return true, fmt.Errorf("%w: %w", ErrInvalidSchema, err)
			}
		}
		questionOrder := parseQuestionOrder(qResp["question_order"])
		if multi, ok := h.(MultiAnswerQuestionResponseHandler); ok {
			return true, multi.HandleQuestionResponseOptions(ctx, reqID, answerOptions, questionOrder)
		}
		answers := flattenAnswerOptions(answerOptions)
		if ordered, ok := h.(OrderedQuestionResponseHandler); ok {
			return true, ordered.HandleQuestionResponseWithOrder(ctx, reqID, answers, questionOrder)
		}
		return true, h.HandleQuestionResponse(ctx, reqID, answers)
	}
	if eResp, ok := metadata["elicitation_response"].(map[string]any); ok {
		reqID, _ := eResp["id"].(string)
		action, _ := eResp["action"].(string)
		if action != "accept" && action != "decline" && action != "cancel" {
			return true, fmt.Errorf("%w: invalid elicitation action: %q", ErrInvalidSchema, action)
		}
		var content map[string]any
		if contentRaw := eResp["content"]; contentRaw != nil {
			var ok bool
			content, ok = contentRaw.(map[string]any)
			if !ok {
				return true, fmt.Errorf("%w: elicitation content must be a map", ErrInvalidSchema)
			}
		}
		return true, h.HandleElicitationResponse(ctx, reqID, action, content)
	}
	return false, nil
}

func parseQuestionOrder(value any) []string {
	switch order := value.(type) {
	case []string:
		return order
	case []any:
		result := make([]string, 0, len(order))
		for _, item := range order {
			if question, ok := item.(string); ok && question != "" {
				result = append(result, question)
			}
		}
		return result
	default:
		return nil
	}
}

func parseAnswerOptions(answersRaw any) (map[string][]string, error) {
	if answersRaw == nil {
		return nil, nil
	}
	if mStr, ok := answersRaw.(map[string]string); ok {
		result := make(map[string][]string, len(mStr))
		for key, value := range mStr {
			result[key] = []string{value}
		}
		return result, nil
	}
	if mSlice, ok := answersRaw.(map[string][]string); ok {
		return mSlice, nil
	}
	mAny, ok := answersRaw.(map[string]any)
	if !ok {
		return nil, errors.New("answers must be a map")
	}
	res := make(map[string][]string, len(mAny))
	for k, v := range mAny {
		switch value := v.(type) {
		case string:
			res[k] = []string{value}
		case []string:
			res[k] = value
		case []any:
			values := make([]string, 0, len(value))
			for _, item := range value {
				text, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("value for key %q must contain only strings", k)
				}
				values = append(values, text)
			}
			res[k] = values
		default:
			return nil, fmt.Errorf("value for key %q must be a string or string array", k)
		}
	}
	return res, nil
}

func flattenAnswerOptions(answers map[string][]string) map[string]string {
	if answers == nil {
		return nil
	}
	result := make(map[string]string, len(answers))
	for key, values := range answers {
		result[key] = strings.Join(values, ", ")
	}
	return result
}
