package slack

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestBuildPermissionFallbackText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data *events.PermissionRequestData
		want []string // substrings that must appear
	}{
		{
			name: "basic",
			data: &events.PermissionRequestData{ID: "req1", ToolName: "Bash"},
			want: []string{"Tool Approval Required", "Bash", "allow req1", "deny req1"},
		},
		{
			name: "with description",
			data: &events.PermissionRequestData{ID: "req2", ToolName: "Write", Description: "write a file"},
			want: []string{"write a file", "Write"},
		},
		{
			name: "with args",
			data: &events.PermissionRequestData{ID: "req3", ToolName: "Bash", Args: []string{"ls -la"}},
			want: []string{"Args: ls -la"},
		},
		{
			name: "empty args skipped",
			data: &events.PermissionRequestData{ID: "req4", ToolName: "Read", Args: []string{"{}"}},
			want: []string{"Read"},
		},
		{
			name: "long args truncated",
			data: &events.PermissionRequestData{ID: "req5", ToolName: "Edit", Args: []string{string(make([]byte, 600))}},
			want: []string{"..."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildPermissionFallbackText(tt.data)
			for _, s := range tt.want {
				require.Contains(t, got, s)
			}
		})
	}
}

func TestBuildQuestionFallbackText(t *testing.T) {
	t.Parallel()

	data := &events.QuestionRequestData{
		ID: "q1",
		Questions: []events.Question{
			{
				Header:   "Choose option",
				Question: "Which framework?",
				Options: []events.QuestionOption{
					{Label: "React", Description: "Frontend library"},
					{Label: "Vue"},
				},
			},
		},
	}
	got := buildQuestionFallbackText(data)
	require.Contains(t, got, "Choose option")
	require.Contains(t, got, "Which framework?")
	require.Contains(t, got, "React — Frontend library")
	require.Contains(t, got, "Vue")
	require.Contains(t, got, "q1")
}

func TestBuildQuestionFallbackText_EmptyHeader(t *testing.T) {
	t.Parallel()

	data := &events.QuestionRequestData{
		ID: "q2",
		Questions: []events.Question{
			{Question: "What?"},
		},
	}
	got := buildQuestionFallbackText(data)
	require.Contains(t, got, "Question 1")
}

func TestBuildElicitationFallbackText(t *testing.T) {
	t.Parallel()

	data := &events.ElicitationRequestData{
		ID:            "e1",
		MCPServerName: "my-server",
		Message:       "Please confirm",
	}
	got := buildElicitationFallbackText(data)
	require.Contains(t, got, "my-server")
	require.Contains(t, got, "Please confirm")
	require.Contains(t, got, "accept e1")
	require.Contains(t, got, "decline e1")
}

func TestBuildElicitationFallbackText_WithURL(t *testing.T) {
	t.Parallel()

	data := &events.ElicitationRequestData{
		ID:            "e2",
		MCPServerName: "srv",
		Message:       "msg",
		URL:           "https://example.com/form",
	}
	got := buildElicitationFallbackText(data)
	require.Contains(t, got, "https://example.com/form")
}

// ---------------------------------------------------------------------------
// checkPendingInteraction tests
// ---------------------------------------------------------------------------

func newTestInteractionAdapter() *Adapter {
	return &Adapter{
		BaseAdapter: messaging.BaseAdapter[*SlackConn]{
			PlatformAdapter: messaging.PlatformAdapter{
				Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			},
		},
		client: slack.New("x-test-token"),
	}
}

type recordingSlackAPI struct {
	*slack.Client
	postCount int
}

func (c *recordingSlackAPI) PostMessageContext(_ context.Context, _ string, _ ...slack.MsgOption) (string, string, error) {
	c.postCount++
	return "", "", nil
}

func TestCheckPendingInteraction_NoInteractions(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	consumed := a.checkPendingInteraction(context.Background(), "allow abc-123", "C1", "123.456", "U1")
	require.False(t, consumed)
}

func TestCheckPendingInteraction_PermissionAllow(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-perm-allow",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.PermissionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "allow req-perm-allow", "C1", "123.456", "U1")
	require.True(t, consumed)
	pr := capturedMetadata["permission_response"].(map[string]any)
	require.True(t, pr["allowed"].(bool))
	require.Equal(t, "req-perm-allow", pr["request_id"])
	require.Equal(t, 0, a.Interactions.Len())
}

func TestCheckPendingInteraction_DeliveryFailureRemainsRetryable(t *testing.T) {
	t.Parallel()

	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	attempts := 0
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-retry",
		SessionID: "sess-retry",
		OwnerID:   "U1",
		Type:      events.PermissionRequest,
		Timeout:   5 * time.Minute,
		SendResponseSync: func(_ context.Context, metadata map[string]any) error {
			attempts++
			if attempts == 1 {
				return fmt.Errorf("worker unavailable")
			}
			return nil
		},
	})

	require.True(t, a.checkPendingInteraction(context.Background(), "allow req-retry", "C1", "", "U1"))
	require.Equal(t, 1, a.Interactions.Len(), "failed delivery must remain pending for retry")
	_, ok := a.Interactions.Get("req-retry")
	require.True(t, ok)

	require.True(t, a.checkPendingInteraction(context.Background(), "allow req-retry", "C1", "", "U1"))
	require.Equal(t, 2, attempts)
	require.Zero(t, a.Interactions.Len(), "successful retry must complete the interaction")
}

func TestCheckPendingInteraction_PermissionDeny(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-perm-deny",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.PermissionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "deny req-perm-deny", "C1", "123.456", "U1")
	require.True(t, consumed)
	pr := capturedMetadata["permission_response"].(map[string]any)
	require.False(t, pr["allowed"].(bool))
	require.Equal(t, "user denied", pr["reason"])
}

func TestCheckPendingInteraction_ElicitationAccept(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-elic-accept",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.ElicitationRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "accept req-elic-accept", "C1", "123.456", "U1")
	require.True(t, consumed)
	er := capturedMetadata["elicitation_response"].(map[string]any)
	require.Equal(t, "accept", er["action"])
	require.Equal(t, "req-elic-accept", er["id"])
}

func TestCheckPendingInteraction_ElicitationDecline(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-elic-decline",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.ElicitationRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "decline req-elic-decline", "C1", "123.456", "U1")
	require.True(t, consumed)
	er := capturedMetadata["elicitation_response"].(map[string]any)
	require.Equal(t, "decline", er["action"])
}

func TestCheckPendingInteraction_QuestionRawText(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-question",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.QuestionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	// Single-word input triggers raw text path (no action keyword).
	consumed := a.checkPendingInteraction(context.Background(), "yes", "C1", "123.456", "U1")
	require.True(t, consumed)
	qr := capturedMetadata["question_response"].(map[string]any)
	require.Equal(t, "req-question", qr["id"])
	answers := qr["answers"].(map[string][]string)
	require.Equal(t, []string{"yes"}, answers["_"])
}

func TestCheckPendingInteraction_QuestionMultiWord(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var capturedMetadata map[string]any
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-question-multi-word",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.QuestionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	answer := "use PostgreSQL for storage"
	consumed := a.checkPendingInteraction(context.Background(), answer, "C1", "123.456", "U1")
	require.True(t, consumed)
	qr := capturedMetadata["question_response"].(map[string]any)
	answers := qr["answers"].(map[string][]string)
	require.Equal(t, []string{answer}, answers["_"])
}

func TestCheckPendingInteraction_ReservedWordQuestionAnswers(t *testing.T) {
	t.Parallel()

	for _, answer := range []string{"allow", "deny", "accept", "decline"} {
		answer := answer
		t.Run(answer, func(t *testing.T) {
			t.Parallel()
			a := newTestInteractionAdapter()
			a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
			a.client = &recordingSlackAPI{Client: slack.New("x-test-token")}

			var capturedMetadata map[string]any
			a.Interactions.Register(&messaging.PendingInteraction{
				ID:        "req-question-" + answer,
				SessionID: "sess-question",
				OwnerID:   "U1",
				Type:      events.QuestionRequest,
				Timeout:   5 * time.Minute,
				SendResponse: func(metadata map[string]any) {
					capturedMetadata = metadata
				},
			})

			consumed := a.checkPendingInteraction(context.Background(), answer, "C1", "123.456", "U1")
			require.True(t, consumed)
			require.NotNil(t, capturedMetadata)
			qr := capturedMetadata["question_response"].(map[string]any)
			answers := qr["answers"].(map[string][]string)
			require.Equal(t, []string{answer}, answers["_"])
		})
	}
}

func TestCheckPendingInteraction_MixedCaseRequestID(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var targetCalls, otherCalls int
	now := time.Now()
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-MixedCase",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.PermissionRequest,
		CreatedAt: now.Add(-time.Minute),
		Timeout:   5 * time.Minute,
		SendResponse: func(map[string]any) {
			targetCalls++
		},
	})
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-other",
		SessionID: "sess-2",
		OwnerID:   "U1",
		Type:      events.PermissionRequest,
		CreatedAt: now,
		Timeout:   5 * time.Minute,
		SendResponse: func(map[string]any) {
			otherCalls++
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "ALLOW req-MixedCase", "C1", "123.456", "U1")
	require.True(t, consumed)
	require.Equal(t, 1, targetCalls)
	require.Zero(t, otherCalls)
	require.Equal(t, 1, a.Interactions.Len())
}

func TestSlackQuestionAnswers_MultiQuestionAndMultiSelect(t *testing.T) {
	t.Parallel()

	questions := []events.Question{
		{ID: "environment", Question: "Where?"},
		{ID: "checks", Question: "Which checks?", MultiSelect: true},
		{ID: "notes", Question: "Notes?"},
	}
	state := &slack.BlockActionStates{Values: map[string]map[string]slack.BlockAction{
		"question_answer_0": {
			"answer_0": {SelectedOption: slack.OptionBlockObject{Value: "Staging"}},
		},
		"question_answer_1": {
			"answer_1": {SelectedOptions: []slack.OptionBlockObject{{Value: "Unit"}, {Value: "Race"}}},
		},
		"question_answer_2": {
			"answer_2": {Value: "Run before merge"},
		},
	}}

	answers, order := slackQuestionAnswers(questions, state, "submit")
	require.Equal(t, []string{"environment", "checks", "notes"}, order)
	require.Equal(t, []string{"Staging"}, answers["environment"])
	require.Equal(t, []string{"Unit", "Race"}, answers["checks"])
	require.Equal(t, []string{"Run before merge"}, answers["notes"])
}

func TestCheckPendingInteraction_OwnerMismatch(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	a.Interactions.Register(&messaging.PendingInteraction{
		ID:           "req-owner",
		SessionID:    "sess-1",
		OwnerID:      "U-correct",
		Type:         events.PermissionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(metadata map[string]any) {},
	})

	consumed := a.checkPendingInteraction(context.Background(), "allow req-owner", "C1", "123.456", "U-wrong")
	require.False(t, consumed)
	require.Equal(t, 1, a.Interactions.Len())
}

func TestCheckPendingInteraction_UnknownRequestIDDoesNotFallback(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	api := &recordingSlackAPI{Client: slack.New("x-test-token")}
	a.client = api

	var firstCalls, secondCalls int
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-fallback-old",
		SessionID: "sess-1",
		OwnerID:   "U1",
		Type:      events.PermissionRequest,
		CreatedAt: time.Now().Add(-time.Minute),
		Timeout:   5 * time.Minute,
		SendResponse: func(map[string]any) {
			firstCalls++
		},
	})
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-fallback-new",
		SessionID: "sess-2",
		OwnerID:   "U1",
		Type:      events.PermissionRequest,
		CreatedAt: time.Now(),
		Timeout:   5 * time.Minute,
		SendResponse: func(map[string]any) {
			secondCalls++
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "allow nonexistent-id", "C1", "123.456", "U1")
	require.True(t, consumed)
	require.Zero(t, firstCalls)
	require.Zero(t, secondCalls)
	require.Equal(t, 2, a.Interactions.Len(), "unknown explicit IDs must preserve every pending request")
	require.Equal(t, 1, api.postCount, "unknown explicit IDs must be consumed with a warning")
}

func TestCheckPendingInteraction_ActionWithoutIDRejectsAmbiguousCandidates(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	api := &recordingSlackAPI{Client: slack.New("x-test-token")}
	a.client = api

	var calls int
	for _, id := range []string{"req-ambiguous-one", "req-ambiguous-two"} {
		a.Interactions.Register(&messaging.PendingInteraction{
			ID:        id,
			SessionID: id,
			OwnerID:   "U1",
			Type:      events.PermissionRequest,
			Timeout:   5 * time.Minute,
			SendResponse: func(map[string]any) {
				calls++
			},
		})
	}

	consumed := a.checkPendingInteraction(context.Background(), "allow", "C1", "123.456", "U1")
	require.True(t, consumed)
	require.Zero(t, calls)
	require.Equal(t, 2, a.Interactions.Len())
	require.Equal(t, 1, api.postCount, "ambiguous action must be consumed with a warning")
}

func TestCheckPendingInteraction_ActionWithoutIDDoesNotSelectOtherSession(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	api := &recordingSlackAPI{Client: slack.New("x-test-token")}
	a.client = api

	calls := 0
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        "req-other-session",
		SessionID: "sess-other",
		OwnerID:   "U1",
		Type:      events.PermissionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(map[string]any) {
			calls++
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "allow", "C-current", "123.456", "U1")
	require.True(t, consumed)
	require.Zero(t, calls, "an ID-less action must not select a request from another session")
	require.Equal(t, 1, a.Interactions.Len())
	require.Equal(t, 1, api.postCount, "an ID-less action must be consumed with a warning")
}

func TestCheckPendingInteraction_WrongActionForType(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	a.Interactions.Register(&messaging.PendingInteraction{
		ID:           "req-wrong-action",
		SessionID:    "sess-1",
		OwnerID:      "U1",
		Type:         events.ElicitationRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(metadata map[string]any) {},
	})

	// "allow" is not valid for ElicitationRequest (needs accept/decline).
	consumed := a.checkPendingInteraction(context.Background(), "allow req-wrong-action", "C1", "123.456", "U1")
	require.False(t, consumed)
}

func TestCheckPendingInteraction_RawTextNotQuestion(t *testing.T) {
	t.Parallel()
	a := newTestInteractionAdapter()
	a.Interactions = messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil)))

	a.Interactions.Register(&messaging.PendingInteraction{
		ID:           "req-not-question",
		SessionID:    "sess-1",
		OwnerID:      "U1",
		Type:         events.PermissionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(metadata map[string]any) {},
	})

	// Raw text (no action keyword) only matches QuestionRequest.
	consumed := a.checkPendingInteraction(context.Background(), "some random text", "C1", "123.456", "U1")
	require.False(t, consumed)
}

// ---------------------------------------------------------------------------
// Args preview backtick stripping
// ---------------------------------------------------------------------------

func TestArgsPreview_BacktickStripping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args string
		want string
	}{
		{"plain text", "hello world", "hello world"},
		{"nested backticks", "plan with ```code``` inside", "plan with code inside"},
		{"multiple blocks", "```a``` and ```b```", "a and b"},
		{"triple at boundaries", "```start end```", "start end"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			preview := tt.args
			if len(preview) > 500 {
				preview = preview[:500] + "..."
			}
			preview = strings.ReplaceAll(preview, "```", "")
			require.Equal(t, tt.want, preview)
		})
	}
}
