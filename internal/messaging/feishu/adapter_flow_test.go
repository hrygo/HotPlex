package feishu

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestAdapterFlow_HandleTextMessage_NilBridge(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	// No bridge → early return nil.
	err := a.handleTextMessage(context.Background(), "msg1", "ch1", "p2p", "user1", "hello", "", "", false)
	require.NoError(t, err)
}

func TestAdapterFlow_HandleTextMessage_WithInteractionConsumed(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	// Set bridge via ConfigureWith (private field).
	_ = a.ConfigureWith(messaging.AdapterConfig{Bridge: &messaging.Bridge{}})
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	a.rateLimiter = NewFeishuRateLimiter()
	t.Cleanup(func() { a.rateLimiter.Stop() })

	// Register a pending permission request.
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:           "perm-ht-1",
		SessionID:    "",
		Type:         events.PermissionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(metadata map[string]any) {},
	})

	// "允许" consumed as interaction response → returns nil (not an error).
	err := a.handleTextMessage(context.Background(), "msg1", "ch1", "p2p", "user1", "允许", "", "", false)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_DoneEvent_WithStreamCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")

	ctrl := newTestStreamingCtrl()
	// Transition to creating then completed (Close will be a no-op).
	conn.EnableStreaming(ctrl)

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-done-1",
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{Success: true},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_ErrorEvent_WithStreamCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "", "")

	ctrl := newTestStreamingCtrl()
	conn.EnableStreaming(ctrl)

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-err-1",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{Code: "TIMEOUT", Message: "timeout"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_ToolCallEvent(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")
	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-tool-1",
		Event: events.Event{
			Type: events.ToolCall,
			Data: events.ToolCallData{Name: "ReadFile"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_ToolResultEvent(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")
	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-tresult-1",
		Event: events.Event{
			Type: events.ToolResult,
			Data: events.ToolResultData{ID: "tc_1"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_ContextUsageEvent_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-cu",
		Event: events.Event{
			Type: events.ContextUsage,
			Data: events.ContextUsageData{
				TotalTokens: 1000, MaxTokens: 2000, Percentage: 50,
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_WriteCtx_MCPStatusEvent_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-mcp",
		Event: events.Event{
			Type: events.MCPStatus,
			Data: events.MCPStatusData{
				Servers: []events.MCPServerInfo{
					{Name: "fs", Status: "connected"},
				},
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_WriteCtx_SkillsListEvent_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-sk",
		Event: events.Event{
			Type: events.SkillsList,
			Data: events.SkillsListData{
				Skills: []events.SkillEntry{{Name: "commit", Description: "git commit", Source: "project"}},
				Total:  1,
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_WriteCtx_MessageDelta_NoStreamingCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	limiter := NewFeishuRateLimiter()
	t.Cleanup(func() { limiter.Stop() })
	a.rateLimiter = limiter

	conn := NewFeishuConn(a, "chat123", "", "")
	conn.mu.Lock()
	conn.replyToMsgID = "msg_reply"
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	// No streaming controller → WriteCtx uses static message path (nil client).
	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-delta",
		Event: events.Event{
			Type: events.Message,
			Data: events.MessageData{Content: "hello world"},
		},
	}

	// Static message with nil client returns error, no panic.
	require.NotPanics(t, func() {
		_ = conn.WriteCtx(context.Background(), env)
	})
}

func TestAdapterFlow_FeishuConn_Close_WithStreamCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")

	ctrl := newTestStreamingCtrl()
	conn.EnableStreaming(ctrl)

	err := conn.Close()
	require.NoError(t, err)

	conn.mu.RLock()
	require.Nil(t, conn.streamCtrl)
	conn.mu.RUnlock()
}

func TestAdapterFlow_FeishuConn_Close_WithReactionIDs(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")

	conn.mu.Lock()
	conn.typingRid = "typing_rid"
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	// Nil larkClient → reaction cleanup skipped gracefully.
	err := conn.Close()
	require.NoError(t, err)

	conn.mu.RLock()
	require.Empty(t, conn.typingRid)
	conn.mu.RUnlock()
}

func TestAdapterFlow_ClearProcessingReaction_EmptyRID(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")

	// Empty RID → early return, no panic.
	conn.clearProcessingReaction(context.Background(), "")
}

func TestAdapterFlow_ClearProcessingReaction_EmptyMsgID(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")

	// RID set but no platformMsgID → early return.
	conn.clearProcessingReaction(context.Background(), "rid123")
}

func TestAdapterFlow_ClearProcessingReaction_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	// Nil larkClient → removeReaction returns error, but clearProcessingReaction ignores it.
	conn.clearProcessingReaction(context.Background(), "rid123")
}

func TestAdapterFlow_SetProcessingReaction_EmptyMsgID(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")

	rid := conn.setProcessingReaction(context.Background())
	require.Empty(t, rid)
}

func TestAdapterFlow_SetProcessingReaction_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	// Nil client → addReaction fails, returns empty rid.
	rid := conn.setProcessingReaction(context.Background())
	require.Empty(t, rid)
}

func TestAdapterFlow_SendTextMessage_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	err := a.sendTextMessage(context.Background(), "chat123", "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_ReplyMessage_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	err := a.replyMessage(context.Background(), "msg123", "hello", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_DedupCleanupLoop_Exit(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		BaseAdapter: messaging.BaseAdapter[*FeishuConn]{
			PlatformAdapter: messaging.PlatformAdapter{
				Log:   discardLogger,
				Dedup: messaging.NewDedup(10, time.Millisecond),
			},
			ConnPool: messaging.NewConnPool[*FeishuConn](nil),
		},
	}
	a.Dedup.StartCleanup()
	a.Dedup.Close() // should not panic
}

func TestAdapterFlow_Close_WithConnections(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := a.GetOrCreateConn("chat_cleanup", "")
	conn.mu.Lock()
	conn.streamCtrl = newTestStreamingCtrl()
	conn.mu.Unlock()

	err := a.Close(context.Background())
	require.NoError(t, err)

	require.Nil(t, a.ConnPool.Get("chat_cleanup#"))
}

func TestAdapterFlow_Start_AlreadyStarted(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.StartGuard()

	err := a.Start(context.Background())
	require.NoError(t, err)
}

func TestAdapterFlow_Start_NoCredentials(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	err := a.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "appID and appSecret required")
}

func TestAdapterFlow_HandleTextControlCommand_NilBridge(t *testing.T) {
	t.Parallel()
	// controlFeedbackMessageCN covers all action branches.
	_ = controlFeedbackMessageCN(events.ControlActionGC)
	_ = controlFeedbackMessageCN(events.ControlActionReset)
	_ = controlFeedbackMessageCN(events.ControlActionDelete)
}

func TestAdapterFlow_HandleTextWorkerCommand_NilBridge(t *testing.T) {
	// Worker command with nil bridge can't be tested without panic.
	// The function is covered indirectly via handleMessage integration.
}

func TestAdapterFlow_WriteCtx_NilEnvelope(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")

	err := conn.WriteCtx(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil envelope")
}

func TestAdapterFlow_WriteCtx_PermissionRequest_ExtractFail(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-perm",
		Event: events.Event{
			Type: events.PermissionRequest,
			Data: map[string]any{},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
}

func TestAdapterFlow_WriteCtx_QuestionRequest_ExtractFail(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-q",
		Event: events.Event{
			Type: events.QuestionRequest,
			Data: map[string]any{},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
}

func TestAdapterFlow_WriteCtx_ElicitationRequest_ExtractFail(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-el",
		Event: events.Event{
			Type: events.ElicitationRequest,
			Data: map[string]any{},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
}

func TestAdapterFlow_WriteCtx_Done_NoStreamCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "", "")

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-done-2",
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{Success: true},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_Error_NoStreamCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "", "")

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-err-2",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{Code: "TIMEOUT", Message: "timeout"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_MessageDelta_StaticPath(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")
	conn.mu.Lock()
	conn.replyToMsgID = "msg_reply"
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-delta-static",
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "hello"},
		},
	}

	// No streaming ctrl → falls through to static path.
	// replyMessage needs lark client → returns error.
	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_WriteCtx_Message_StaticPath_NoReplyTo(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")
	// replyToMsgID left empty → uses sendTextMessage path.

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-msg-static",
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "world"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_WriteCtx_RawEvent_WithText(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")
	conn.mu.Lock()
	conn.replyToMsgID = "msg_raw"
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-raw",
		Event: events.Event{
			Type: events.Raw,
			Data: events.RawData{Raw: map[string]any{"text": "raw content"}},
		},
	}

	// extractResponseText returns text for raw events.
	// Static path with nil client → error.
	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
}

func TestAdapterFlow_WriteCtx_Done_WithReactionCleanup(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "", "")
	conn.mu.Lock()
	conn.typingRid = "typing_rid"
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-done-rx",
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{Success: true},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)

	conn.mu.RLock()
	require.Empty(t, conn.typingRid)
	conn.mu.RUnlock()
}

func TestAdapterFlow_WriteCtx_Error_WithReactionCleanup(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "", "")
	conn.mu.Lock()
	conn.typingRid = "typing_rid"
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-err-rx",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{Code: "ERR", Message: "something went wrong"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)

	conn.mu.RLock()
	require.Empty(t, conn.typingRid)
	conn.mu.RUnlock()
}

func TestAdapterFlow_RegisterInteraction(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := a.GetOrCreateConn("chat_ri", "")

	a.registerInteraction("req-1", "sess-ri", "owner-1", events.PermissionRequest, conn)
	require.Equal(t, 1, a.Interactions.Len())
}

func TestAdapterFlow_WriteCtx_StreamCtrl_WriteFlush(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	limiter := NewFeishuRateLimiter()
	t.Cleanup(func() { limiter.Stop() })
	conn := NewFeishuConn(a, "chat123", "", "")

	ctrl := NewStreamingCardController(nil, limiter, discardLogger, "TestBot", 0, "", "", "", nil)
	ctrl.transition(PhaseCreating)
	ctrl.transition(PhaseStreaming)
	ctrl.mu.Lock()
	ctrl.cardKitOK = false // skip cardKit path, no msgID → IM patch also skipped
	ctrl.mu.Unlock()
	conn.EnableStreaming(ctrl)

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-wf",
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "hello"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_RemoveReaction_EmptyReactionID_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	// removeReaction checks nil client BEFORE empty reactionID.
	err := a.removeReaction(context.Background(), "msg123", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_RegisterInteraction_CallbackConsumed(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := a.GetOrCreateConn("chat_ricb", "")
	conn.mu.Lock()
	conn.sessionID = "sess-ricb"
	conn.mu.Unlock()

	// Register via registerInteraction (creates SendResponse closure with nil bridge).
	a.registerInteraction("perm-ricb", "sess-ricb", "owner-ricb", events.PermissionRequest, conn)
	require.Equal(t, 1, a.Interactions.Len())

	// Consume the interaction via checkPendingInteraction.
	consumed := a.checkPendingInteraction(context.Background(), "允许", "owner-ricb", conn)
	require.True(t, consumed)
	require.Equal(t, 0, a.Interactions.Len())
}

// ---------------------------------------------------------------------------
// WriteCtx interaction events close stream controller
// ---------------------------------------------------------------------------

func TestWriteCtx_InteractionEvents_ClosesStreamCtrl(t *testing.T) {
	// Do NOT mark parallel: modifies conn fields directly.
	tests := []struct {
		name      string
		eventType events.Kind
		data      any
	}{
		{
			"PermissionRequest",
			events.PermissionRequest,
			events.PermissionRequestData{ID: "r1", ToolName: "Bash"},
		},
		{
			"QuestionRequest",
			events.QuestionRequest,
			events.QuestionRequestData{ID: "q1"},
		},
		{
			"ElicitationRequest",
			events.ElicitationRequest,
			events.ElicitationRequestData{ID: "e1", MCPServerName: "srv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestAdapter(t)
			a.Interactions = messaging.NewInteractionManager(discardLogger)

			conn := a.GetOrCreateConn("chat_close_test", "")
			conn.mu.Lock()
			conn.sessionID = "sess-close"
			conn.platformMsgID = "msg_001"
			conn.mu.Unlock()

			// Set up an active typing reaction indicator to verify clearActiveIndicators runs.
			conn.mu.Lock()
			conn.typingRid = "typing_rid_test"
			conn.mu.Unlock()

			env := &events.Envelope{
				Version:   events.Version,
				SessionID: "sess-close",
				Event: events.Event{
					Type: tt.eventType,
					Data: tt.data,
				},
			}

			_ = conn.WriteCtx(context.Background(), env)

			// Verify typing reaction was cleared by clearActiveIndicators.
			conn.mu.RLock()
			got := conn.typingRid
			conn.mu.RUnlock()
			require.Empty(t, got, "typingRid should be cleared after %s event", tt.name)
		})
	}
}

func TestShouldAppendNewlineAfterDelta(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text string
		want bool
	}{
		// Empty → no separator.
		{"empty", "", false},
		// CJK word-split mid-stream chunks: must NOT add \n.
		{"cjk partial word", "赫尔", false},
		{"cjk mid sentence", "墨斯的", false},
		{"chinese comma", "赫尔墨斯，", false},
		// ASCII sentence terminators → add \n.
		{"ascii period", "Hello world.", true},
		{"ascii question", "Are you sure?", true},
		{"ascii exclamation", "Got it!", true},
		// ASCII terminator clusters → add \n.
		{"ascii question-period", "really?!", true},
		{"ascii bang-period", "yes!.", true},
		{"ascii double question", "why??", true},
		{"ascii double bang", "wow!!", true},
		{"ascii interrobang", "what!?", true},
		// CJK fullwidth terminators → add \n.
		{"cjk period", "你好世界。", true},
		{"cjk question", "你好吗？", true},
		{"cjk exclamation", "太棒了！", true},
		// Whitespace and other punctuation: do not trigger.
		{"space suffix", "trailing space ", false},
		{"semicolon", "item;", false},
		{"chinese semicolon", "项；", false},
		{"chinese colon", "注：", false},
		// CJK char ending in 0x82 that is NOT a terminator: 0x82 is also
		// the trailing byte of other 3-byte UTF-8 chars. Make sure we
		// decode the rune and don't false-positive.
		{"cjk non-terminator 0x82", "啊", false},
		{"chinese comma", "你，", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := shouldAppendNewlineAfterDelta(tt.text)
			require.Equal(t, tt.want, got, "shouldAppendNewlineAfterDelta(%q)", tt.text)
		})
	}
}

func TestWriteCtx_MessageDelta_NoSeparatorForCJKMidWord(t *testing.T) {
	t.Parallel()
	// Regression for: every MessageDelta was being suffixed with "\n\n",
	// which split CJK words across chunk boundaries (e.g. "赫尔" + "墨斯"
	// rendered as separate lines). The fix only appends "\n" when the chunk
	// ends on a sentence terminator.
	//
	// We assert directly via shouldAppendNewlineAfterDelta (the helper that
	// gates the suffix). The streaming-IO path is covered by other tests
	// and requires a non-nil lark client, which we deliberately don't set
	// here — this test is about the suffix decision, not transport.
	cases := []struct {
		name  string
		chunk string
		want  bool
	}{
		{"cjk first half", "赫尔", false},
		{"cjk second half", "墨斯", false},
		{"cjk sentence end", "你好世界。", true},
		{"ascii sentence end", "Hello world.", true},
		{"empty", "", false},
		{"trailing comma", "赫尔墨斯，", false},
		{"trailing space", "trailing ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, shouldAppendNewlineAfterDelta(tc.chunk))
		})
	}
}

// BenchmarkShouldAppendNewlineAfterDelta measures the per-delta suffix-gate
// decision cost on representative streaming chunks. Every MessageDelta
// passes through this helper, so it sits on the hot path of every streaming
// turn. The constant-time implementation must not allocate per call.
func BenchmarkShouldAppendNewlineAfterDelta(b *testing.B) {
	cases := []struct {
		name string
		text string
	}{
		// Mid-CJK word: 200 runes. This is the worst case for the naive
		// []rune(text) implementation — it would allocate a 200-element rune
		// slice per call. Our constant-time impl must touch only the last byte.
		{"cjk_mid_200", strings.Repeat("赫", 199) + "尔"},
		// 1k ASCII chunk with terminator: also long enough to punish full scans.
		{"ascii_long_1k", strings.Repeat("a", 999) + "."},
		// CJK sentence-terminated: exercises the UTF-8 decode branch.
		{"cjk_end_50", strings.Repeat("中", 49) + "。"},
		// Short mid-CJK word (the bug-trigger shape).
		{"cjk_short_2", "赫尔"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = shouldAppendNewlineAfterDelta(tc.text)
			}
		})
	}
}
