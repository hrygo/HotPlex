package acp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestNewACPConn(t *testing.T) {
	t.Parallel()

	c := newACPConn("user1", "sess1")
	require.Equal(t, "user1", c.UserID())
	require.Equal(t, "sess1", c.SessionID())
}

func TestACPConn_Send_NotImplemented(t *testing.T) {
	t.Parallel()

	c := newACPConn("u", "s")
	err := c.Send(context.Background(), &events.Envelope{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not implemented")
}

func TestACPConn_Recv(t *testing.T) {
	t.Parallel()

	c := newACPConn("u", "s")
	ch := c.Recv()
	require.NotNil(t, ch)

	// Send an envelope and receive it.
	env := &events.Envelope{Version: events.Version}
	c.recvCh <- env
	received := <-ch
	require.Same(t, env, received)
}

func TestACPConn_TrySend_Droppable(t *testing.T) {
	t.Parallel()

	c := newACPConn("u", "s")

	// Droppable event succeeds on empty channel.
	require.True(t, c.TrySend(&events.Envelope{
		Event: events.Event{Type: events.MessageDelta},
	}))

	// Drain to empty the channel for the next test.
	<-c.recvCh

	// Fill the channel to capacity (now empty, cap=256).
	for range cap(c.recvCh) {
		c.recvCh <- &events.Envelope{}
	}
	// Droppable event on full channel — should be silently dropped.
	require.False(t, c.TrySend(&events.Envelope{
		Event: events.Event{Type: events.MessageDelta},
	}))
}

func TestACPConn_TrySend_Critical(t *testing.T) {
	t.Parallel()

	c := newACPConn("u", "s")

	// Critical event blocks until channel has room.
	filled := make(chan struct{}) // signaled when goroutine has filled the channel
	done := make(chan struct{})
	go func() {
		// Fill the channel to capacity (256).
		for range 256 {
			c.recvCh <- &events.Envelope{}
		}
		close(filled)
		// Critical send — blocks until room is available.
		c.TrySend(&events.Envelope{
			Event: events.Event{Type: events.Done},
		})
		close(done)
	}()

	// Wait until the goroutine has filled the channel, then drain one.
	<-filled
	<-c.recvCh

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("critical TrySend did not unblock after drain")
	}
}

func TestACPConn_TrySend_CriticalAfterClose(t *testing.T) {
	t.Parallel()

	c := newACPConn("u", "s")
	require.NoError(t, c.Close())

	// Critical event after close — should return false.
	require.False(t, c.TrySend(&events.Envelope{
		Event: events.Event{Type: events.Done},
	}))
}

func TestACPConn_SafeSend_AfterClose(t *testing.T) {
	t.Parallel()

	c := newACPConn("u", "s")
	require.NoError(t, c.Close())

	// safeSend should not panic on closed channel.
	result := c.safeSend(&events.Envelope{})
	require.False(t, result)
}

func TestACPConn_Close_Idempotent(t *testing.T) {
	t.Parallel()

	c := newACPConn("u", "s")
	require.NoError(t, c.Close())
	require.NoError(t, c.Close()) // second close is no-op
}

func TestIsDroppable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind     events.Kind
		expected bool
	}{
		{events.MessageDelta, true},
		{events.Raw, true},
		{events.State, false},
		{events.Done, false},
		{events.Error, false},
		{events.MessageStart, false},
		{events.MessageEnd, false},
		{events.ToolCall, false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.expected, isDroppable(tt.kind), "isDroppable(%s)", tt.kind)
	}
}
