package hotplex

import (
	"io"
	"log/slog"
	"os"
	"testing"
)

// newTestLogger creates a logger for testing with optional debug output.
func newTestLogger() *slog.Logger {
	// Use discard handler by default for quiet tests
	// Set HOTPLEX_TEST_DEBUG=1 to enable debug output
	if os.Getenv("HOTPLEX_TEST_DEBUG") != "" {
		return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && contains(s[1:], substr) ||
		(len(s) >= len(substr) && s[:len(substr)] == substr))
}

// containsInSlice checks if slice contains item.
func containsInSlice[T comparable](slice []T, item T) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

// assertContains asserts that slice contains item, failing test if not.
func assertContains(t *testing.T, slice []string, item string) {
	t.Helper()
	if !containsInSlice(slice, item) {
		t.Errorf("Slice does not contain %q: %v", item, slice)
	}
}
