//go:build integration
// +build integration

package hotplex

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEngine_Concurrency(t *testing.T) {
	// Create a dummy "claude" script that follows the protocol
	tmpDir, err := os.MkdirTemp("", "hotplex-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	dummyPath := filepath.Join(tmpDir, "claude")
	script := `#!/bin/sh
# 1. Signal readiness immediately
echo '{"type":"ready"}'
# 2. Read turn-based prompts from stdin and output result
while read line; do
  # Simulate some processing
  echo '{"type":"thinking"}'
  sleep 0.1
  echo '{"type":"result", "status":"success"}'
done
`
	if err := os.WriteFile(dummyPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	opts := EngineOptions{
		Namespace:   "test-concurrency",
		Logger:      logger,
		IdleTimeout: 5 * time.Minute,
		Timeout:     5 * time.Minute,
	}

	// Manually construct engine to use dummyPath
	engine := &Engine{
		opts:           opts,
		cliPath:        dummyPath,
		logger:         logger,
		manager:        NewSessionPool(logger, opts.IdleTimeout, opts, dummyPath),
		dangerDetector: NewDetector(logger),
	}
	defer engine.manager.Shutdown()

	const concurrentSessions = 3
	const executionsPerSession = 4
	var wg sync.WaitGroup

	start := time.Now()

	// Launch parallel sessions
	for i := 0; i < concurrentSessions; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("bench-session-%d", id)

			for j := 0; j < executionsPerSession; j++ {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

				err := engine.Execute(ctx, &Config{
					SessionID: sessionID,
					WorkDir:   tmpDir,
				}, "test-prompt", func(eventType string, data any) error {
					return nil
				})

				cancel()
				if err != nil {
					t.Errorf("Execute failed for %s turn %d: %v", sessionID, j, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)
	t.Logf("Full concurrency test (3 sessions, 4 turns each) finished in %v", duration)

	// Expectations:
	// Total turns = 12
	// Each turn has 0.1s sleep in script.
	// Since 3 sessions run in parallel, 4 turns per session should take ~0.4s (plus cold start).
	if duration > 5*time.Second {
		t.Errorf("Test took too long (%v), expected ~1s", duration)
	}
}
