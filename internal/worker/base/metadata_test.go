package base

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type richMetadataHandler struct {
	basicCalled bool
	richCalled  bool
	requestID   string
	answers     map[string][]string
	order       []string
}

func (h *richMetadataHandler) HandlePermissionResponse(context.Context, string, bool, string) error {
	return nil
}

func (h *richMetadataHandler) HandleQuestionResponse(_ context.Context, _ string, _ map[string]string) error {
	h.basicCalled = true
	return nil
}

func (h *richMetadataHandler) HandleQuestionResponseOptions(_ context.Context, requestID string, answers map[string][]string, order []string) error {
	h.richCalled = true
	h.requestID = requestID
	h.answers = answers
	h.order = order
	return nil
}

func (h *richMetadataHandler) HandleElicitationResponse(context.Context, string, string, map[string]any) error {
	return nil
}

func TestDispatchMetadata_PreservesMultiAnswerValues(t *testing.T) {
	t.Parallel()

	handler := &richMetadataHandler{}
	handled, err := DispatchMetadata(context.Background(), map[string]any{
		"question_response": map[string]any{
			"id": "question-1",
			"answers": map[string]any{
				"environment": "Staging",
				"checks":      []any{"Unit", "Race"},
			},
			"question_order": []any{"environment", "checks"},
		},
	}, handler)

	require.NoError(t, err)
	require.True(t, handled)
	require.True(t, handler.richCalled)
	require.False(t, handler.basicCalled)
	require.Equal(t, "question-1", handler.requestID)
	require.Equal(t, []string{"Staging"}, handler.answers["environment"])
	require.Equal(t, []string{"Unit", "Race"}, handler.answers["checks"])
	require.Equal(t, []string{"environment", "checks"}, handler.order)
}

// ─── all-rounder handler for full-coverage tests ──────────────────────────

type fullHandler struct {
	permReqID   string
	permAllowed bool
	permReason  string

	questReqID  string
	questMap    map[string]string
	questOrder  []string
	questCalled string // "basic", "ordered", "multi"

	elicReqID   string
	elicAction  string
	elicContent map[string]any
}

func (h *fullHandler) HandlePermissionResponse(_ context.Context, reqID string, allowed bool, reason string) error {
	h.permReqID = reqID
	h.permAllowed = allowed
	h.permReason = reason
	return nil
}

func (h *fullHandler) HandleQuestionResponse(_ context.Context, reqID string, answers map[string]string) error {
	h.questReqID = reqID
	h.questMap = answers
	h.questCalled = "basic"
	return nil
}

func (h *fullHandler) HandleElicitationResponse(_ context.Context, reqID, action string, content map[string]any) error {
	h.elicReqID = reqID
	h.elicAction = action
	h.elicContent = content
	return nil
}

// Implement OrderedQuestionResponseHandler but NOT MultiAnswerQuestionResponseHandler.
type orderedHandler struct {
	fullHandler
}

func (h *orderedHandler) HandleQuestionResponseWithOrder(_ context.Context, reqID string, answers map[string]string, order []string) error {
	h.questReqID = reqID
	h.questMap = answers
	h.questOrder = order
	h.questCalled = "ordered"
	return nil
}

func TestDispatchMetadata_Permission(t *testing.T) {
	t.Parallel()

	t.Run("allow_with_reason", func(t *testing.T) {
		t.Parallel()
		h := &fullHandler{}
		handled, err := DispatchMetadata(context.Background(), map[string]any{
			"permission_response": map[string]any{
				"id":      "perm-1",
				"allowed": true,
				"reason":  "looks good",
			},
		}, h)
		require.NoError(t, err)
		require.True(t, handled)
		require.Equal(t, "perm-1", h.permReqID)
		require.True(t, h.permAllowed)
		require.Equal(t, "looks good", h.permReason)
	})

	t.Run("deny_without_reason", func(t *testing.T) {
		t.Parallel()
		h := &fullHandler{}
		handled, err := DispatchMetadata(context.Background(), map[string]any{
			"permission_response": map[string]any{
				"id":      "perm-2",
				"allowed": false,
			},
		}, h)
		require.NoError(t, err)
		require.True(t, handled)
		require.Equal(t, "perm-2", h.permReqID)
		require.False(t, h.permAllowed)
		require.Empty(t, h.permReason)
	})

	t.Run("request_id_fallback", func(t *testing.T) {
		t.Parallel()
		h := &fullHandler{}
		handled, err := DispatchMetadata(context.Background(), map[string]any{
			"permission_response": map[string]any{
				"request_id": "perm-3",
				"allowed":    true,
			},
		}, h)
		require.NoError(t, err)
		require.True(t, handled)
		require.Equal(t, "perm-3", h.permReqID)
	})
}

func TestDispatchMetadata_Elicitation(t *testing.T) {
	t.Parallel()

	validActions := []string{"accept", "decline", "cancel"}
	for _, action := range validActions {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			h := &fullHandler{}
			handled, err := DispatchMetadata(context.Background(), map[string]any{
				"elicitation_response": map[string]any{
					"id":      "elic-1",
					"action":  action,
					"content": map[string]any{"root": "/tmp"},
				},
			}, h)
			require.NoError(t, err)
			require.True(t, handled)
			require.Equal(t, "elic-1", h.elicReqID)
			require.Equal(t, action, h.elicAction)
			require.Equal(t, map[string]any{"root": "/tmp"}, h.elicContent)
		})
	}

	t.Run("nil_content", func(t *testing.T) {
		t.Parallel()
		h := &fullHandler{}
		handled, err := DispatchMetadata(context.Background(), map[string]any{
			"elicitation_response": map[string]any{
				"id":     "elic-2",
				"action": "decline",
			},
		}, h)
		require.NoError(t, err)
		require.True(t, handled)
		require.Nil(t, h.elicContent)
	})

	t.Run("invalid_action", func(t *testing.T) {
		t.Parallel()
		h := &fullHandler{}
		_, err := DispatchMetadata(context.Background(), map[string]any{
			"elicitation_response": map[string]any{
				"id":     "elic-3",
				"action": "maybe",
			},
		}, h)
		require.ErrorIs(t, err, ErrInvalidSchema)
		require.Contains(t, err.Error(), "invalid elicitation action")
	})

	t.Run("non_map_content", func(t *testing.T) {
		t.Parallel()
		h := &fullHandler{}
		_, err := DispatchMetadata(context.Background(), map[string]any{
			"elicitation_response": map[string]any{
				"id":      "elic-4",
				"action":  "accept",
				"content": "not-a-map",
			},
		}, h)
		require.ErrorIs(t, err, ErrInvalidSchema)
		require.Contains(t, err.Error(), "elicitation content must be a map")
	})
}

func TestDispatchMetadata_QuestionOrderedFallback(t *testing.T) {
	t.Parallel()

	h := &orderedHandler{}
	handled, err := DispatchMetadata(context.Background(), map[string]any{
		"question_response": map[string]any{
			"id": "q-ord",
			"answers": map[string]any{
				"q1": "a1",
				"q2": []any{"x", "y"},
			},
			"question_order": []any{"q1", "q2"},
		},
	}, h)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "ordered", h.questCalled)
	require.Equal(t, "q-ord", h.questReqID)
	// Multi-select values are comma-joined in the ordered fallback.
	require.Equal(t, map[string]string{"q1": "a1", "q2": "x, y"}, h.questMap)
	require.Equal(t, []string{"q1", "q2"}, h.questOrder)
}

func TestDispatchMetadata_QuestionBasicFallback(t *testing.T) {
	t.Parallel()

	h := &fullHandler{}
	handled, err := DispatchMetadata(context.Background(), map[string]any{
		"question_response": map[string]any{
			"id": "q-basic",
			"answers": map[string]any{
				"env": "Staging",
			},
		},
	}, h)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "basic", h.questCalled)
	require.Equal(t, "q-basic", h.questReqID)
	require.Equal(t, map[string]string{"env": "Staging"}, h.questMap)
}

func TestDispatchMetadata_NoMatch(t *testing.T) {
	t.Parallel()

	h := &fullHandler{}
	handled, err := DispatchMetadata(context.Background(), map[string]any{
		"some_other_key": "value",
	}, h)
	require.NoError(t, err)
	require.False(t, handled)
}

func TestDispatchMetadata_Nil(t *testing.T) {
	t.Parallel()

	h := &fullHandler{}
	handled, err := DispatchMetadata(context.Background(), nil, h)
	require.NoError(t, err)
	require.False(t, handled)
}

func TestDispatchMetadata_QuestionInvalidAnswers(t *testing.T) {
	t.Parallel()

	h := &richMetadataHandler{}
	_, err := DispatchMetadata(context.Background(), map[string]any{
		"question_response": map[string]any{
			"id":      "q-bad",
			"answers": "not-a-map",
		},
	}, h)
	require.ErrorIs(t, err, ErrInvalidSchema)
	require.Contains(t, err.Error(), "answers must be a map")
}

func TestDispatchMetadata_QuestionInvalidAnswerElement(t *testing.T) {
	t.Parallel()

	h := &richMetadataHandler{}
	_, err := DispatchMetadata(context.Background(), map[string]any{
		"question_response": map[string]any{
			"id": "q-bad-elem",
			"answers": map[string]any{
				"q1": []any{"ok", 42},
			},
		},
	}, h)
	require.ErrorIs(t, err, ErrInvalidSchema)
	require.Contains(t, err.Error(), "must contain only strings")
}
