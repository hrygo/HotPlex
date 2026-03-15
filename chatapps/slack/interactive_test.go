package slack

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
)

func newTestAdapter() *Adapter {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewAdapter(&Config{
		BotToken:      "xoxb-test",
		SigningSecret: "test-secret",
		Mode:          "http",
	}, logger, base.WithoutServer())
}

func TestHandleBlockActions(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:  "block_actions",
		User:  CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
		Actions: []SlackAction{
			{ActionID: "test_action", BlockID: "block1", Value: "test_value"},
		},
	}

	w := httptest.NewRecorder()
	adapter.handleBlockActions(callback, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleBlockActions_Permission(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:  "block_actions",
		User:  CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
		Actions: []SlackAction{
			{ActionID: "perm_allow:session123:msg123", Value: "allow"},
		},
	}

	w := httptest.NewRecorder()
	adapter.handleBlockActions(callback, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleBlockActions_PlanMode(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:  "block_actions",
		User:  CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
		Actions: []SlackAction{
			{ActionID: "plan_approve", Value: "approved"},
		},
	}

	w := httptest.NewRecorder()
	adapter.handleBlockActions(callback, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleBlockActions_Danger(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:  "block_actions",
		User:  CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
		Actions: []SlackAction{
			{ActionID: "danger_confirm", Value: "confirm"},
		},
	}

	w := httptest.NewRecorder()
	adapter.handleBlockActions(callback, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleBlockActions_Question(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:  "block_actions",
		User:  CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
		Actions: []SlackAction{
			{ActionID: "question_option_yes", Value: "yes"},
		},
	}

	w := httptest.NewRecorder()
	adapter.handleBlockActions(callback, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleBlockActions_Unhandled(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:  "block_actions",
		User:  CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
		Actions: []SlackAction{
			{ActionID: "unknown_action", Value: "value"},
		},
	}

	w := httptest.NewRecorder()
	adapter.handleBlockActions(callback, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandlePermissionCallback(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:    "block_actions",
		User:    CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
	}

	action := SlackAction{
		ActionID: "perm_allow:session123:msg123",
		Value:    "allow",
	}

	w := httptest.NewRecorder()
	adapter.handlePermissionCallback(callback, action, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandlePermissionCallback_InvalidFormat(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:    "block_actions",
		User:    CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
	}

	action := SlackAction{
		ActionID: "invalid",
		Value:    "allow",
	}

	w := httptest.NewRecorder()
	adapter.handlePermissionCallback(callback, action, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandlePlanModeCallback(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:    "block_actions",
		User:    CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
	}

	action := SlackAction{
		ActionID: "plan_approve",
		Value:    "approved",
	}

	w := httptest.NewRecorder()
	adapter.handlePlanModeCallback(callback, action, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandlePlanModeCallback_Modify(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:    "block_actions",
		User:    CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
	}

	action := SlackAction{
		ActionID: "plan_modify",
		Value:    "modify",
	}

	w := httptest.NewRecorder()
	adapter.handlePlanModeCallback(callback, action, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandlePlanModeCallback_Cancel(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:    "block_actions",
		User:    CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
	}

	action := SlackAction{
		ActionID: "plan_cancel",
		Value:    "cancel",
	}

	w := httptest.NewRecorder()
	adapter.handlePlanModeCallback(callback, action, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleDangerBlockCallback(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:    "block_actions",
		User:    CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
	}

	action := SlackAction{
		ActionID: "danger_confirm",
		Value:    "confirm",
	}

	w := httptest.NewRecorder()
	adapter.handleDangerBlockCallback(callback, action, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleDangerBlockCallback_Cancel(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:    "block_actions",
		User:    CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
	}

	action := SlackAction{
		ActionID: "danger_cancel",
		Value:    "cancel",
	}

	w := httptest.NewRecorder()
	adapter.handleDangerBlockCallback(callback, action, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleViewSubmission(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type: "view_submission",
		View: &slack.View{
			ID: "V123",
		},
	}

	w := httptest.NewRecorder()
	adapter.handleViewSubmission(callback, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleViewClosed(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type: "view_closed",
		View: &slack.View{
			ID: "V123",
		},
	}

	w := httptest.NewRecorder()
	adapter.handleViewClosed(callback, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAskUserQuestionCallback(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:    "block_actions",
		User:    CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
	}

	action := SlackAction{
		ActionID: "question_option_yes",
		Value:    "yes",
	}

	w := httptest.NewRecorder()
	adapter.handleAskUserQuestionCallback(callback, action, w)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleAskUserQuestionCallback_No(t *testing.T) {
	adapter := newTestAdapter()

	callback := &SlackInteractionCallback{
		Type:    "block_actions",
		User:    CallbackUser{ID: "U123"},
		Channel: CallbackChannel{ID: "C123"},
	}

	action := SlackAction{
		ActionID: "question_option_no",
		Value:    "no",
	}

	w := httptest.NewRecorder()
	adapter.handleAskUserQuestionCallback(callback, action, w)

	assert.Equal(t, http.StatusOK, w.Code)
}
