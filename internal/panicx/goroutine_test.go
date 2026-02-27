package panicx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSafeGoCatchesPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	done := make(chan bool, 1)
	SafeGo(logger, func() {
		defer func() { done <- true }()
		panic("test panic")
	})

	select {
	case <-done:
		// Good - goroutine completed
	case <-time.After(time.Second):
		t.Fatal("Goroutine did not complete - panic not recovered")
	}

	// Small delay to ensure logger writes
	time.Sleep(50 * time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, "test panic") {
		t.Errorf("Panic was not logged. Output: %s", output)
	}
	if !strings.Contains(output, "SafeGo") {
		t.Errorf("Context was not logged. Output: %s", output)
	}
}

func TestSafeGoWithContextCatchesPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	ctx := context.Background()
	done := make(chan bool, 1)
	SafeGoWithContext(ctx, logger, func(ctx context.Context) {
		defer func() { done <- true }()
		panic("context test panic")
	})

	select {
	case <-done:
		// Good
	case <-time.After(time.Second):
		t.Fatal("Goroutine did not complete")
	}

	time.Sleep(50 * time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, "context test panic") {
		t.Errorf("Panic was not logged. Output: %s", output)
	}
}

func TestSafeGoWithPolicyLogAndContinue(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	done := make(chan bool, 1)
	SafeGoWithPolicy(logger, PolicyLogAndContinue, "test_policy", func() {
		defer func() { done <- true }()
		panic("policy test panic")
	})

	select {
	case <-done:
		// Good
	case <-time.After(time.Second):
		t.Fatal("Goroutine did not complete")
	}

	time.Sleep(50 * time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, "log_and_continue") {
		t.Errorf("Policy was not logged. Output: %s", output)
	}
}

func TestRecoverInDefer(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	go func() {
		defer Recover(logger, "testRecover")
		panic("defer test panic")
	}()

	// Wait for goroutine to complete
	time.Sleep(100 * time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, "defer test panic") {
		t.Errorf("Panic was not recovered. Output: %s", output)
	}
}

func TestRecoverWithCallback(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	var callbackPanic any
	var callbackStack []byte
	var callbackMu sync.Mutex

	go func() {
		defer RecoverWithCallback(logger, "testCallback", func(p any, s []byte) {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			callbackPanic = p
			callbackStack = s
		})
		panic("callback test panic")
	}()

	// Wait for goroutine to complete
	time.Sleep(100 * time.Millisecond)

	callbackMu.Lock()
	defer callbackMu.Unlock()

	if callbackPanic == nil {
		t.Error("Callback was not called")
	}
	if callbackStack == nil {
		t.Error("Stack trace was not passed to callback")
	}
	if callbackPanic != nil && callbackPanic.(string) != "callback test panic" {
		t.Errorf("Wrong panic value: %v", callbackPanic)
	}
}

func TestSafeGoWithNilLogger(t *testing.T) {
	// Should not panic even with nil logger
	done := make(chan bool, 1)
	SafeGo(nil, func() {
		defer func() { done <- true }()
		panic("nil logger test")
	})

	select {
	case <-done:
		// Good - recovered without crashing
	case <-time.After(time.Second):
		t.Fatal("Goroutine did not complete")
	}
}

func TestSafeGoNormalExecution(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	result := 0
	done := make(chan bool, 1)
	SafeGo(logger, func() {
		result = 42
		done <- true
	})

	select {
	case <-done:
		// Good
	case <-time.After(time.Second):
		t.Fatal("Goroutine did not complete")
	}

	if result != 42 {
		t.Errorf("Function did not execute properly. Got: %d", result)
	}

	// Should not log anything for normal execution
	if buf.String() != "" {
		t.Errorf("Logger should be empty for normal execution: %s", buf.String())
	}
}

func TestPolicyString(t *testing.T) {
	tests := []struct {
		policy   RecoveryPolicy
		expected string
	}{
		{PolicyLogAndContinue, "log_and_continue"},
		{PolicyLogAndRestart, "log_and_restart"},
		{PolicyLogAndShutdown, "log_and_shutdown"},
		{RecoveryPolicy(99), "unknown"},
	}

	for _, tt := range tests {
		got := policyString(tt.policy)
		if got != tt.expected {
			t.Errorf("policyString(%v) = %s, want %s", tt.policy, got, tt.expected)
		}
	}
}

func TestConcurrentPanics(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		SafeGo(logger, func() {
			defer wg.Done()
			panic("concurrent panic")
		})
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for goroutines")
	}

	// Allow time for all logs to be written
	time.Sleep(100 * time.Millisecond)

	// Should have logged all panics
	output := buf.String()
	count := strings.Count(output, "concurrent panic")
	if count != numGoroutines {
		t.Errorf("Expected %d panic logs, got %d", numGoroutines, count)
	}
}
