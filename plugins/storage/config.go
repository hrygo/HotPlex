package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ConfigLoader 配置加载器
type ConfigLoader struct {
	path string
}

// NewConfigLoader 创建配置加载器
func NewConfigLoader(path string) *ConfigLoader {
	return &ConfigLoader{path: path}
}

// LoadStorageConfig 从 YAML 文件加载存储配置
func (l *ConfigLoader) LoadStorageConfig() (*StorageConfig, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config StorageConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

// StorageConfig 存储配置
type StorageConfig struct {
	Enabled    bool             `json:"enabled"`
	Type       string           `json:"type"`
	SQLite     SQLiteConfig     `json:"sqlite"`
	PostgreSQL PostgreSQLConfig `json:"postgres"`
	Strategy   string           `json:"strategy"`
	Streaming  StreamingConfig  `json:"streaming"`
}

// SQLiteConfig SQLite 配置
type SQLiteConfig struct {
	Path      string `json:"path"`
	MaxSizeMB int    `json:"max_size_mb"`
}

// StreamingConfig 流式配置
type StreamingConfig struct {
	Enabled       bool   `json:"enabled"`
	BufferSize    int    `json:"buffer_size"`
	TimeoutSec    int    `json:"timeout_seconds"`
	StoragePolicy string `json:"storage_policy"`
}

// ExportToJSON 将消息导出为 JSON
func ExportToJSON(store ChatAppMessageStore, outputPath string, query *MessageQuery) error {
	ctx := context.Background()
	messages, err := store.List(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal messages: %w", err)
	}

	return os.WriteFile(outputPath, data, 0644)
}

// ImportFromJSON 从 JSON 导入消息
func ImportFromJSON(store ChatAppMessageStore, inputPath string) (int, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read file: %w", err)
	}

	var messages []*ChatAppMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return 0, fmt.Errorf("failed to unmarshal messages: %w", err)
	}

	ctx := context.Background()
	imported := 0
	for _, msg := range messages {
		if err := store.StoreUserMessage(ctx, msg); err != nil {
			continue
		}
		imported++
	}

	return imported, nil
}

// BackupStorage 备份存储
func BackupStorage(store ChatAppMessageStore, backupPath string) error {
	ctx := context.Background()

	// Get all messages
	messages, err := store.List(ctx, &MessageQuery{Limit: 100000})
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	// Get all session metadata
	sessions, err := store.ListUserSessions(ctx, "", "")
	if err != nil {
		// May not be implemented
		sessions = []string{}
	}

	backup := struct {
		Timestamp time.Time         `json:"timestamp"`
		Messages  []*ChatAppMessage `json:"messages"`
		Sessions  []string          `json:"sessions"`
	}{
		Timestamp: time.Now(),
		Messages:  messages,
		Sessions:  sessions,
	}

	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal backup: %w", err)
	}

	return os.WriteFile(backupPath, data, 0644)
}
