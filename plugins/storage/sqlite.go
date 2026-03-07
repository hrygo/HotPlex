package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/hrygo/hotplex/internal/sys"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteStorage SQLite 存储实现
type SQLiteStorage struct {
	db       *sql.DB
	config   PluginConfig
	strategy StorageStrategy
}

type SQLiteFactory struct{}

// Compile-time interface compliance checks
var (
	_ ChatAppMessageStore = (*SQLiteStorage)(nil)
	_ PluginFactory       = (*SQLiteFactory)(nil)
)

func (f *SQLiteFactory) Create(config PluginConfig) (ChatAppMessageStore, error) {
	return &SQLiteStorage{config: config, strategy: NewDefaultStrategy()}, nil
}

func (s *SQLiteStorage) Initialize(ctx context.Context) error {
	path, _ := s.config["path"].(string)
	if path == "" {
		path = "~/.hotplex/chatapp_messages.db"
	}
	// Expand home directory tilde
	path = sys.ExpandPath(path)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return err
	}
	s.db = db
	return s.createTables(ctx)
}

func (s *SQLiteStorage) createTables(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY, chat_session_id TEXT NOT NULL, chat_platform TEXT NOT NULL,
			chat_user_id TEXT NOT NULL, chat_bot_user_id TEXT, chat_channel_id TEXT,
			chat_thread_id TEXT, engine_session_id TEXT NOT NULL, engine_namespace TEXT DEFAULT 'hotplex',
			provider_session_id TEXT NOT NULL, provider_type TEXT NOT NULL, message_type TEXT,
			from_user_id TEXT, from_user_name TEXT, to_user_id TEXT, content TEXT NOT NULL,
			metadata TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP, deleted INTEGER DEFAULT 0, deleted_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_chat_session ON messages(chat_session_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS session_metadata (
			chat_session_id TEXT PRIMARY KEY, chat_platform TEXT, chat_user_id TEXT,
			last_message_id TEXT, last_message_at DATETIME, message_count INTEGER DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, q := range queries {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStorage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *SQLiteStorage) Name() string    { return "sqlite" }
func (s *SQLiteStorage) Version() string { return "1.0.0" }

func (s *SQLiteStorage) Get(ctx context.Context, messageID string) (*ChatAppMessage, error) {
	query := `SELECT id, chat_session_id, chat_platform, chat_user_id, content, message_type, created_at
		FROM messages WHERE id = ? AND deleted = 0`
	row := s.db.QueryRowContext(ctx, query, messageID)
	var msg ChatAppMessage
	err := row.Scan(&msg.ID, &msg.ChatSessionID, &msg.ChatPlatform, &msg.ChatUserID, &msg.Content, &msg.MessageType, &msg.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *SQLiteStorage) List(ctx context.Context, query *MessageQuery) ([]*ChatAppMessage, error) {
	sqlQuery := `SELECT id, chat_session_id, chat_user_id, content, message_type, created_at FROM messages WHERE deleted = 0`
	var args []any
	if query.ChatSessionID != "" {
		sqlQuery += " AND chat_session_id = ?"
		args = append(args, query.ChatSessionID)
	}
	if query.ChatUserID != "" {
		sqlQuery += " AND chat_user_id = ?"
		args = append(args, query.ChatUserID)
	}
	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var results []*ChatAppMessage
	for rows.Next() {
		var msg ChatAppMessage
		_ = rows.Scan(&msg.ID, &msg.ChatSessionID, &msg.ChatUserID, &msg.Content, &msg.MessageType, &msg.CreatedAt)
		results = append(results, &msg)
	}
	return results, nil
}

func (s *SQLiteStorage) Count(ctx context.Context, query *MessageQuery) (int64, error) {
	sqlQuery := `SELECT COUNT(*) FROM messages WHERE deleted = 0`
	var args []any
	if query.ChatSessionID != "" {
		sqlQuery += " AND chat_session_id = ?"
		args = append(args, query.ChatSessionID)
	}
	if query.ChatUserID != "" {
		sqlQuery += " AND chat_user_id = ?"
		args = append(args, query.ChatUserID)
	}
	var count int64
	err := s.db.QueryRowContext(ctx, sqlQuery, args...).Scan(&count)
	return count, err
}

func (s *SQLiteStorage) StoreUserMessage(ctx context.Context, msg *ChatAppMessage) error {
	if s.strategy != nil && !s.strategy.ShouldStore(msg) {
		return nil
	}
	return s.storeMessage(ctx, msg)
}

func (s *SQLiteStorage) StoreBotResponse(ctx context.Context, msg *ChatAppMessage) error {
	if s.strategy != nil && !s.strategy.ShouldStore(msg) {
		return nil
	}
	return s.storeMessage(ctx, msg)
}

func (s *SQLiteStorage) storeMessage(ctx context.Context, msg *ChatAppMessage) error {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	now := time.Now()
	msg.CreatedAt = now
	msg.UpdatedAt = now
	metadataJSON, _ := json.Marshal(msg.Metadata)
	query := `INSERT OR REPLACE INTO messages (id, chat_session_id, chat_platform, chat_user_id, content, message_type, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, msg.ID, msg.ChatSessionID, msg.ChatPlatform, msg.ChatUserID, msg.Content, string(msg.MessageType), string(metadataJSON), msg.CreatedAt, msg.UpdatedAt)
	return err
}

func (s *SQLiteStorage) GetSessionMeta(ctx context.Context, chatSessionID string) (*SessionMeta, error) {
	query := `SELECT chat_session_id, chat_platform, chat_user_id, message_count FROM session_metadata WHERE chat_session_id = ?`
	row := s.db.QueryRowContext(ctx, query, chatSessionID)
	var meta SessionMeta
	err := row.Scan(&meta.ChatSessionID, &meta.ChatPlatform, &meta.ChatUserID, &meta.MessageCount)
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *SQLiteStorage) ListUserSessions(ctx context.Context, platform, userID string) ([]string, error) {
	query := `SELECT chat_session_id FROM session_metadata WHERE chat_platform = ? AND chat_user_id = ?`
	rows, err := s.db.QueryContext(ctx, query, platform, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var sessions []string
	for rows.Next() {
		var sessionID string
		_ = rows.Scan(&sessionID)
		sessions = append(sessions, sessionID)
	}
	return sessions, nil
}

func (s *SQLiteStorage) DeleteSession(ctx context.Context, chatSessionID string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET deleted = 1, deleted_at = ? WHERE chat_session_id = ?`, now, chatSessionID)
	return err
}

func (s *SQLiteStorage) GetStrategy() StorageStrategy         { return s.strategy }
func (s *SQLiteStorage) SetStrategy(st StorageStrategy) error { s.strategy = st; return nil }
