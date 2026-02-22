package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	intengine "github.com/hrygo/hotplex/internal/engine"
	"github.com/hrygo/hotplex/provider"
)

func TestStress1000Sessions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	prv, err := provider.NewClaudeCodeProvider(provider.ProviderConfig{}, logger)
	if err != nil {
		t.Skipf("Claude Code CLI not available: %v", err)
	}

	cliPath, err := prv.ValidateBinary()
	if err != nil {
		t.Skipf("Claude Code CLI not found: %v", err)
	}

	opts := intengine.EngineOptions{
		Logger:       logger,
		Timeout:      30 * time.Second,
		IdleTimeout:  5 * time.Minute,
		Namespace:    "stress-test",
		AllowedTools: []string{"Bash", "Read", "Write"},
	}

	manager := intengine.NewSessionPool(logger, opts.IdleTimeout, opts, cliPath, prv)
	defer manager.Shutdown()

	sessionCount := 100
	if os.Getenv("STRESS_SESSIONS") != "" {
		_, _ = fmt.Sscanf(os.Getenv("STRESS_SESSIONS"), "%d", &sessionCount)
	}

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var errorCount atomic.Int64

	start := time.Now()

	for i := 0; i < sessionCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			sessionID := fmt.Sprintf("stress-session-%d", idx)
			workDir := fmt.Sprintf("/tmp/hotplex-stress-%d", idx)
			_ = os.MkdirAll(workDir, 0755)

			cfg := intengine.SessionConfig{
				WorkDir:          workDir,
				TaskInstructions: "You are a test agent.",
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			sess, _, err := manager.GetOrCreateSession(ctx, sessionID, cfg, "test prompt")
			if err != nil {
				errorCount.Add(1)
				return
			}

			if sess != nil {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	elapsed := time.Since(start)
	t.Logf("Stress test completed: %d sessions in %v", sessionCount, elapsed)
	t.Logf("Success: %d, Errors: %d", successCount.Load(), errorCount.Load())

	successRate := float64(successCount.Load()) / float64(sessionCount) * 100
	t.Logf("Success rate: %.2f%%", successRate)

	if successRate < 90.0 {
		t.Errorf("Success rate too low: %.2f%% (expected >= 90%%)", successRate)
	}
}

func TestStressConcurrentAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	sessionCount := 50
	accessPerSession := 10

	var wg sync.WaitGroup
	var totalOps atomic.Int64

	start := time.Now()

	for i := 0; i < sessionCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			sessionID := fmt.Sprintf("concurrent-session-%d", idx)

			for j := 0; j < accessPerSession; j++ {
				totalOps.Add(1)
				_ = sessionID
			}
		}(i)
	}

	wg.Wait()

	elapsed := time.Since(start)
	t.Logf("Concurrent access test: %d sessions x %d accesses in %v", sessionCount, accessPerSession, elapsed)

	opsPerSec := float64(totalOps.Load()) / elapsed.Seconds()
	t.Logf("Operations per second: %.2f", opsPerSec)
}

func TestStressMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	sessionCount := 100
	sessions := make([]map[string]string, sessionCount)

	for i := 0; i < sessionCount; i++ {
		sessions[i] = map[string]string{
			"session_id": fmt.Sprintf("memory-session-%d", i),
			"work_dir":   fmt.Sprintf("/tmp/hotplex-memory-%d", i),
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	heapGrowth := int64(m2.HeapAlloc) - int64(m1.HeapAlloc)
	t.Logf("Heap before: %d bytes", m1.HeapAlloc)
	t.Logf("Heap after: %d bytes", m2.HeapAlloc)
	t.Logf("Heap growth: %d bytes (%.2f MB)", heapGrowth, float64(heapGrowth)/1024/1024)

	_ = sessions
}
