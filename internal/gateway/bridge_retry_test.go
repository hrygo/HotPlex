package gateway

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

type blockingRetryDispatchWorker struct {
	mockWorkerForHandler
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
}

func (w *blockingRetryDispatchWorker) Input(ctx context.Context, content string, metadata map[string]any) error {
	return w.input(ctx, content, metadata, nil)
}

func (w *blockingRetryDispatchWorker) InputWithDispatchAccepted(
	ctx context.Context,
	content string,
	metadata map[string]any,
	accepted func(),
) error {
	return w.input(ctx, content, metadata, accepted)
}

func (w *blockingRetryDispatchWorker) input(
	ctx context.Context,
	_ string,
	_ map[string]any,
	accepted func(),
) error {
	w.enteredOnce.Do(func() { close(w.entered) })
	if accepted != nil {
		accepted()
	}
	select {
	case <-w.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestAutoRetryReleasesEventAdmissionAfterDispatchAccepted(t *testing.T) {
	retryCtrl := NewLLMRetryController(config.AutoRetryConfig{Enabled: true}, slog.Default())
	retryCtrl.config.BaseDelay = time.Nanosecond
	retryCtrl.config.NotifyUser = false
	b := &Bridge{
		log:         slog.Default(),
		retryCtrl:   retryCtrl,
		retryCancel: make(map[string]chan struct{}),
	}
	lifecycle := newWorkerRunLifecycle(nil)
	w := &blockingRetryDispatchWorker{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	cancelCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.autoRetry(context.Background(), w, "retry-session", 1, cancelCh, lifecycle)
	}()
	defer func() {
		close(w.release)
		<-done
	}()

	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("retry input was not dispatched")
	}

	writeLockAcquired := make(chan struct{})
	go func() {
		lifecycle.eventMu.Lock()
		close(writeLockAcquired)
		lifecycle.eventMu.Unlock()
	}()
	select {
	case <-writeLockAcquired:
	case <-time.After(250 * time.Millisecond):
		require.FailNow(t, "stop event barrier remained blocked by the full retry turn")
	}
}
