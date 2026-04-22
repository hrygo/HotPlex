package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewWatcher(t *testing.T) {
	t.Parallel()

	t.Run("with nil logger and store", func(t *testing.T) {
		t.Parallel()
		w := NewWatcher(nil, "/tmp/test.yaml", nil, nil, nil, nil)
		require.NotNil(t, w)
		require.NotNil(t, w.log)
		require.Equal(t, "/tmp/test.yaml", w.path)
		require.Nil(t, w.store)
	})

	t.Run("with custom params", func(t *testing.T) {
		t.Parallel()
		logger := slog.Default()
		sp := NewEnvSecretsProvider()
		store := NewConfigStore(Default(), logger)
		w := NewWatcher(logger, "/tmp/test.yaml", sp, store, nil, nil)
		require.NotNil(t, w)
		require.Equal(t, logger, w.log)
		require.Equal(t, store, w.store)
	})
}

func TestWatcher_Reload_And_ConfigStore(t *testing.T) {
	t.Parallel()

	tmpFile := createTempConfigFile(t)
	cfg := Default()
	cfg.Gateway.Addr = "127.0.0.1:8888"
	cfg.Pool.MaxSize = 100

	// Save initial config to file
	content := "gateway:\n  addr: 127.0.0.1:8888\npool:\n  max_size: 100\n"
	require.NoError(t, os.WriteFile(tmpFile, []byte(content), 0644))

	logger := slog.Default()
	store := NewConfigStore(cfg, logger)

	var reloaded bool
	var mu sync.Mutex
	store.RegisterFunc(func(prev, next *Config) {
		mu.Lock()
		reloaded = true
		mu.Unlock()
	})

	w := NewWatcher(logger, tmpFile, nil, store, nil, nil)
	w.SetInitial(cfg)

	// Modify file
	newContent := "gateway:\n  addr: 127.0.0.1:9999\npool:\n  max_size: 200\n"
	require.NoError(t, os.WriteFile(tmpFile, []byte(newContent), 0644))

	// Trigger reload
	w.reload()

	// Verify history
	require.Equal(t, "127.0.0.1:9999", w.Latest().Gateway.Addr)
	require.Equal(t, 200, w.Latest().Pool.MaxSize)

	// Verify ConfigStore was updated
	require.Equal(t, "127.0.0.1:9999", store.Load().Gateway.Addr)

	// Verify observers notified
	time.Sleep(100 * time.Millisecond) // Observers run in goroutines
	mu.Lock()
	require.True(t, reloaded)
	mu.Unlock()
}

func TestDiffConfigs_Precision(t *testing.T) {
	t.Parallel()

	cfg1 := Default()
	cfg1.Pool.MaxSize = 100
	cfg1.Gateway.Addr = "127.0.0.1:8888"

	cfg2 := Default()
	cfg2.Pool.MaxSize = 200              // Hot change
	cfg2.Gateway.Addr = "127.0.0.1:9999" // Static change

	changes := diffConfigs(cfg1, cfg2)

	foundPool := false
	foundAddr := false
	for _, c := range changes {
		if c.Field == "pool.max_size" {
			foundPool = true
			require.True(t, c.Hot)
			require.Equal(t, "100", c.OldValue)
			require.Equal(t, "200", c.NewValue)
		}
		if c.Field == "gateway.addr" {
			foundAddr = true
			require.False(t, c.Hot)
		}
	}
	require.True(t, foundPool, "should have found pool.max_size change")
	require.True(t, foundAddr, "should have found gateway.addr change")
}

func TestWatcher_Rollback_Triggers_Store(t *testing.T) {
	t.Parallel()

	cfg1 := Default()
	cfg1.Gateway.Addr = "127.0.0.1:8081"
	cfg2 := Default()
	cfg2.Gateway.Addr = "127.0.0.1:8082"

	store := NewConfigStore(cfg2, nil)
	w := NewWatcher(nil, "/tmp/test.yaml", nil, store, nil, nil)

	w.muHistory.Lock()
	w.history = []*Config{cfg1, cfg2}
	w.latestIdx = 1
	w.muHistory.Unlock()

	var rolledBack bool
	var mu sync.Mutex
	store.RegisterFunc(func(prev, next *Config) {
		mu.Lock()
		rolledBack = true
		mu.Unlock()
	})

	_, _, err := w.Rollback(1)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8081", store.Load().Gateway.Addr)

	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	require.True(t, rolledBack)
	mu.Unlock()
}

func TestConfigStore_Concurrency(t *testing.T) {
	t.Parallel()

	cfg := Default()
	store := NewConfigStore(cfg, nil)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = store.Load()
				newCfg := Default()
				newCfg.Pool.MaxSize = n*100 + j
				store.Swap(newCfg)
			}
		}(i)
	}
	wg.Wait()
}

// ─── Helper Functions ───────────────────────────────────────────────────────

func createTempConfigFile(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")

	content := "gateway:\n  addr: 127.0.0.1:8888\npool:\n  max_size: 100\n"
	err := os.WriteFile(tmpFile, []byte(content), 0644)
	require.NoError(t, err)

	return tmpFile
}
