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
