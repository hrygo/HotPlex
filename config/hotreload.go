package config

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type HotReloader struct {
	path     string
	logger   *slog.Logger
	config   any
	mu       sync.RWMutex
	watcher  *fsnotify.Watcher
	onReload func(any)
}

func NewHotReloader(path string, initialConfig any, logger *slog.Logger) (*HotReloader, error) {
	if logger == nil {
		logger = slog.Default()
	}

	loader := &HotReloader{
		path:   path,
		logger: logger,
		config: initialConfig,
	}

	if err := loader.load(); err != nil {
		return nil, err
	}

	return loader, nil
}

func (h *HotReloader) load() error {
	data, err := os.ReadFile(h.path)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if err := json.Unmarshal(data, h.config); err != nil {
		return err
	}

	h.logger.Info("Config loaded", "path", h.path)
	return nil
}

func (h *HotReloader) Start(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	h.watcher = watcher

	if err := watcher.Add(h.path); err != nil {
		_ = watcher.Close()
		return err
	}

	go h.watchLoop(ctx)

	h.logger.Info("Hot reloader started", "path", h.path)
	return nil
}

func (h *HotReloader) watchLoop(ctx context.Context) {
	var debounceTimer *time.Timer
	debounceDelay := 100 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-h.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) {
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					if err := h.load(); err != nil {
						h.logger.Error("Failed to reload config", "error", err)
						return
					}
					h.mu.RLock()
					config := h.config
					h.mu.RUnlock()
					if h.onReload != nil {
						h.onReload(config)
					}
				})
			}
		case err, ok := <-h.watcher.Errors:
			if !ok {
				return
			}
			h.logger.Error("Watcher error", "error", err)
		}
	}
}

func (h *HotReloader) OnReload(fn func(any)) {
	h.onReload = fn
}

func (h *HotReloader) Get() any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.config
}

func (h *HotReloader) Close() error {
	if h.watcher != nil {
		return h.watcher.Close()
	}
	return nil
}
