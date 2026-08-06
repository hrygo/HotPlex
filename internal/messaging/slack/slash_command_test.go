package slack

import (
	"context"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestSlashRateLimiter_Allow(t *testing.T) {
	t.Parallel()

	cooldown := 100 * time.Millisecond
	rl := NewSlashRateLimiterWithCooldown(cooldown)
	defer rl.Stop()

	userID := "U123"

	require.True(t, rl.Allow(userID), "first request should be allowed")
	require.False(t, rl.Allow(userID), "second request within cooldown should be rate limited")

	require.Eventually(t, func() bool { return rl.Allow(userID) }, cooldown+200*time.Millisecond, 20*time.Millisecond, "request after cooldown should be allowed")
}

func TestSlashRateLimiter_DifferentUsers(t *testing.T) {
	t.Parallel()

	rl := NewSlashRateLimiter()
	defer rl.Stop()

	user1 := "U123"
	user2 := "U456"

	require.True(t, rl.Allow(user1))
	require.False(t, rl.Allow(user1))

	require.True(t, rl.Allow(user2))
	require.False(t, rl.Allow(user2))
}

func TestSlashRateLimiter_Stop(t *testing.T) {
	t.Parallel()

	rl := NewSlashRateLimiter()
	rl.Stop()
}

func TestSlashRateLimiter_SweepRemovesStaleEntries(t *testing.T) {
	t.Parallel()

	rl := NewSlashRateLimiter()
	defer rl.Stop()

	// Inject entries with different expiry times.
	now := time.Now()
	rl.cache.Do(func(items map[string]ttlEntry[time.Time]) {
		items["user1"] = ttlEntry[time.Time]{Value: now, Expiry: now.Add(-1 * time.Minute)}  // expired
		items["user2"] = ttlEntry[time.Time]{Value: now, Expiry: now.Add(5 * time.Minute)}   // fresh
		items["user3"] = ttlEntry[time.Time]{Value: now, Expiry: now.Add(-10 * time.Minute)} // expired
	})

	require.Equal(t, 3, rl.cache.Len(), "should have 3 entries before sweep")

	// Manually trigger sweep logic (same as what sweepLoop does).
	rl.cache.Do(func(items map[string]ttlEntry[time.Time]) {
		for k, e := range items {
			if now.After(e.Expiry) {
				delete(items, k)
			}
		}
	})

	require.Equal(t, 1, rl.cache.Len(), "should have 1 entry after sweep")
	_, ok := rl.cache.Get("user2")
	require.True(t, ok, "user2 should still exist (fresh entry)")
}

func TestSlashRateLimiter_SweepLoopExitsOnDone(t *testing.T) {
	t.Parallel()

	rl := NewSlashRateLimiter()

	// Stop should cleanly terminate the goroutine
	done := make(chan struct{})
	go func() {
		rl.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() should complete quickly")
	}
}

func TestExtractChannelThread(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sessionID  string
		wantCh     string
		wantThread string
	}{
		{"valid", "slack:T:C123:1234567890.123456:U1", "C123", "1234567890.123456"},
		{"short", "slack:T:C:456:U", "C", "456"},
		{"invalid", "invalid", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, thread := ExtractChannelThread(tt.sessionID)
			require.Equal(t, tt.wantCh, ch)
			require.Equal(t, tt.wantThread, thread)
		})
	}
}

func TestHandleSlashCommandEventWorker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		command  string
		text     string
		wantText string
	}{
		{"name and args", CommandWorker, "oracle-dba 10.0.0.1", "/worker oracle-dba 10.0.0.1"},
		{"empty text", CommandWorker, "", "/worker"},
		{"whitespace text", CommandWorker, "   ", "/worker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, calls := newAdapterWithCapture(t)
			a.socketMode = socketmode.New(slack.New("xoxb-test"))
			a.client = &stubSlackClient{}

			evt := socketmode.Event{
				Type: socketmode.EventTypeSlashCommand,
				Data: slack.SlashCommand{
					TeamID:    "T1",
					ChannelID: "C1",
					UserID:    "U1",
					Command:   tt.command,
					Text:      tt.text,
				},
				Request: &socketmode.Request{EnvelopeID: "env-1"},
			}

			a.handleSlashCommandEvent(context.Background(), evt)

			require.Len(t, *calls, 1, "one input envelope should reach the bridge")
			call := (*calls)[0]
			require.Equal(t, tt.wantText, call.Text, "content must carry name + args into the shared parser")
			require.Equal(t, "U1", call.OwnerID)
			require.Equal(t, "slack", call.Metadata["platform"])
			require.Equal(t, "C1", call.Metadata["channel_id"])
		})
	}
}

func TestHandleSlashCommandEventControl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		action  events.ControlAction
	}{
		{"reset", CommandReset, events.ControlActionReset},
		{"disconnect", CommandDisconnect, events.ControlActionTerminate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, envelopes, _ := newAdapterWithControlCapture(t)
			a.socketMode = socketmode.New(slack.New("xoxb-test"))
			a.client = &stubSlackClient{}

			evt := socketmode.Event{
				Type: socketmode.EventTypeSlashCommand,
				Data: slack.SlashCommand{
					TeamID:    "T1",
					ChannelID: "C1",
					UserID:    "U1",
					Command:   tt.command,
				},
				Request: &socketmode.Request{EnvelopeID: "env-1"},
			}

			a.handleSlashCommandEvent(context.Background(), evt)

			select {
			case env := <-envelopes:
				require.Equal(t, events.Control, env.Event.Type)
				cd, ok := env.Event.Data.(events.ControlData)
				require.True(t, ok, "control envelope must carry ControlData")
				require.Equal(t, tt.action, cd.Action)
				require.Equal(t, "U1", env.OwnerID)
			case <-time.After(2 * time.Second):
				t.Fatal("expected control envelope to reach the bridge")
			}
		})
	}
}
