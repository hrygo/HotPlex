package base

import (
	"context"
	"errors"
	"fmt"
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
		var answers map[string]string
		if answersRaw := qResp["answers"]; answersRaw != nil {
			var err error
			answers, err = parseAnswers(answersRaw)
			if err != nil {
				return true, fmt.Errorf("%w: %w", ErrInvalidSchema, err)
			}
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

func parseAnswers(answersRaw any) (map[string]string, error) {
	if answersRaw == nil {
		return nil, nil
	}
	if mStr, ok := answersRaw.(map[string]string); ok {
		return mStr, nil
	}
	mAny, ok := answersRaw.(map[string]any)
	if !ok {
		return nil, errors.New("answers must be a map")
	}
	res := make(map[string]string, len(mAny))
	for k, v := range mAny {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("value for key %q must be a string", k)
		}
		res[k] = s
	}
	return res, nil
}
