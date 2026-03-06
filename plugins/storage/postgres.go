package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// PostgreSQLConfig PostgreSQL 配置
type PostgreSQLConfig struct {
	Host         string
	Port         int
	User         string
	Password     string
	Database     string
	SSLMode      string
	MaxOpenConns int
	MaxIdleConns int
	MaxLifetime  time.Duration
}

// PostgreStorage PostgreSQL 存储实现 (Level 2: Partitioned for 100M+ rows)
type PostgreStorage struct {
	db       *sql.DB
	config   PluginConfig
	strategy StorageStrategy
}

// PostgreFactory PostgreSQL 工厂
type PostgreFactory struct{}

// Compile-time interface compliance checks
var (
	_ ChatAppMessageStore = (*PostgreStorage)(nil)
	_ PluginFactory       = (*PostgreFactory)(nil)
)

func (f *PostgreFactory) Create(config PluginConfig) (ChatAppMessageStore, error) {
	pgConfig := getPostgreConfig(config)
	return NewPostgreStorage(pgConfig, config)
}

// NewPostgreStorage 创建 PostgreSQL 存储
func NewPostgreStorage(pgConfig PostgreSQLConfig, pluginConfig PluginConfig) (*PostgreStorage, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		pgConfig.Host, pgConfig.Port, pgConfig.User, pgConfig.Password, pgConfig.Database, pgConfig.SSLMode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 设置连接池参数
	db.SetMaxOpenConns(pgConfig.MaxOpenConns)
	db.SetMaxIdleConns(pgConfig.MaxIdleConns)
	db.SetConnMaxLifetime(pgConfig.MaxLifetime)

	// 验证连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgreStorage{
		db:       db,
		config:   pluginConfig,
		strategy: NewDefaultStrategy(),
	}, nil
}

// getPostgreConfig 从 PluginConfig 提取 PostgreSQL 配置
func getPostgreConfig(config PluginConfig) PostgreSQLConfig {
	getString := func(key string, def string) string {
		if v, ok := config[key].(string); ok {
			return v
		}
		return def
	}
	getInt := func(key string, def int) int {
		if v, ok := config[key].(int); ok {
			return v
		}
		return def
	}

	return PostgreSQLConfig{
		Host:         getString("host", "localhost"),
		Port:         getInt("port", 5432),
		User:         getString("user", "hotplex"),
		Password:     getString("password", ""),
		Database:     getString("database", "hotplex"),
		SSLMode:      getString("ssl_mode", "disable"),
		MaxOpenConns: getInt("max_open_conns", 25),
		MaxIdleConns: getInt("max_idle_conns", 5),
		MaxLifetime:  time.Duration(getInt("max_lifetime", 300)) * time.Second,
	}
}

// Initialize 初始化数据库表结构
func (p *PostgreStorage) Initialize(ctx context.Context) error {
	// 创建消息表 (支持分区)
	schema := `
	-- 消息表 (按时间范围分区)
	CREATE TABLE IF NOT EXISTS messages (
		id VARCHAR(64) NOT NULL,
		chat_session_id VARCHAR(128) NOT NULL,
		chat_platform VARCHAR(32) NOT NULL,
		chat_user_id VARCHAR(128) NOT NULL,
		chat_bot_user_id VARCHAR(128),
		chat_channel_id VARCHAR(128),
		chat_thread_id VARCHAR(128),
		engine_session_id UUID NOT NULL,
		engine_namespace VARCHAR(128),
		provider_session_id VARCHAR(128),
		provider_type VARCHAR(32),
		message_type VARCHAR(32) NOT NULL,
		from_user_id VARCHAR(128),
		from_user_name VARCHAR(256),
		to_user_id VARCHAR(128),
		content TEXT,
		metadata JSONB,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		deleted BOOLEAN DEFAULT FALSE,
		deleted_at TIMESTAMPTZ,
		PRIMARY KEY (id, created_at)
	) PARTITION BY RANGE (created_at);

	-- 创建索引
	CREATE INDEX IF NOT EXISTS idx_messages_session ON messages (chat_session_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_messages_user ON messages (chat_platform, chat_user_id);
	CREATE INDEX IF NOT EXISTS idx_messages_engine ON messages (engine_session_id);
	CREATE INDEX IF NOT EXISTS idx_messages_provider ON messages (provider_session_id);
	CREATE INDEX IF NOT EXISTS idx_messages_type ON messages (message_type);
	CREATE INDEX IF NOT EXISTS idx_messages_metadata ON messages USING GIN (metadata);

	-- 会话元数据表
	CREATE TABLE IF NOT EXISTS session_meta (
		chat_session_id VARCHAR(128) PRIMARY KEY,
		chat_platform VARCHAR(32) NOT NULL,
		chat_user_id VARCHAR(128) NOT NULL,
		last_message_id VARCHAR(64),
		last_message_at TIMESTAMPTZ,
		message_count BIGINT DEFAULT 0,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_session_meta_user ON session_meta (chat_platform, chat_user_id);
	`
	_, err := p.db.ExecContext(ctx, schema)
	return err
}

// Close 关闭数据库连接
func (p *PostgreStorage) Close() error {
	return p.db.Close()
}

// Name 返回存储名称
func (p *PostgreStorage) Name() string {
	return "postgresql"
}

// Version 返回版本
func (p *PostgreStorage) Version() string {
	return "1.0.0"
}

// Get 根据 ID 获取消息
func (p *PostgreStorage) Get(ctx context.Context, messageID string) (*ChatAppMessage, error) {
	query := `
		SELECT id, chat_session_id, chat_platform, chat_user_id, chat_bot_user_id,
		       chat_channel_id, chat_thread_id, engine_session_id, engine_namespace,
		       provider_session_id, provider_type, message_type, from_user_id,
		       from_user_name, to_user_id, content, metadata, created_at,
		       updated_at, deleted, deleted_at
		FROM messages
		WHERE id = $1 AND (deleted = FALSE OR deleted IS NULL)
		ORDER BY created_at DESC
		LIMIT 1
	`
	row := p.db.QueryRowContext(ctx, query, messageID)
	return p.scanMessage(row)
}

// List 查询消息列表
func (p *PostgreStorage) List(ctx context.Context, query *MessageQuery) ([]*ChatAppMessage, error) {
	sqlQuery, args := p.buildListQuery(query)

	rows, err := p.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var messages []*ChatAppMessage
	for rows.Next() {
		msg, err := p.scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// Count 统计消息数量
func (p *PostgreStorage) Count(ctx context.Context, query *MessageQuery) (int64, error) {
	sqlQuery, args := p.buildCountQuery(query)

	var count int64
	err := p.db.QueryRowContext(ctx, sqlQuery, args...).Scan(&count)
	return count, err
}

// StoreUserMessage 存储用户消息
func (p *PostgreStorage) StoreUserMessage(ctx context.Context, msg *ChatAppMessage) error {
	if p.strategy != nil && !p.strategy.ShouldStore(msg) {
		return nil
	}
	return p.storeMessage(ctx, msg)
}

// StoreBotResponse 存储机器人响应
func (p *PostgreStorage) StoreBotResponse(ctx context.Context, msg *ChatAppMessage) error {
	if p.strategy != nil && !p.strategy.ShouldStore(msg) {
		return nil
	}
	return p.storeMessage(ctx, msg)
}

// storeMessage 存储消息
func (p *PostgreStorage) storeMessage(ctx context.Context, msg *ChatAppMessage) error {
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	now := time.Now()
	msg.CreatedAt = now
	msg.UpdatedAt = now

	query := `
		INSERT INTO messages (
			id, chat_session_id, chat_platform, chat_user_id, chat_bot_user_id,
			chat_channel_id, chat_thread_id, engine_session_id, engine_namespace,
			provider_session_id, provider_type, message_type, from_user_id,
			from_user_name, to_user_id, content, metadata, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
		)
	`

	_, err := p.db.ExecContext(ctx, query,
		msg.ID, msg.ChatSessionID, msg.ChatPlatform, msg.ChatUserID, msg.ChatBotUserID,
		msg.ChatChannelID, msg.ChatThreadID, msg.EngineSessionID, msg.EngineNamespace,
		msg.ProviderSessionID, msg.ProviderType, string(msg.MessageType), msg.FromUserID,
		msg.FromUserName, msg.ToUserID, msg.Content, msg.Metadata, msg.CreatedAt, msg.UpdatedAt,
	)

	if err == nil {
		p.updateSessionMeta(ctx, msg)
	}

	return err
}

// GetSessionMeta 获取会话元数据
func (p *PostgreStorage) GetSessionMeta(ctx context.Context, chatSessionID string) (*SessionMeta, error) {
	query := `SELECT chat_session_id, chat_platform, chat_user_id, last_message_id, 
	          last_message_at, message_count, updated_at 
	          FROM session_meta WHERE chat_session_id = $1`

	row := p.db.QueryRowContext(ctx, query, chatSessionID)
	var meta SessionMeta
	var lastMessageID sql.NullString
	var lastMessageAt sql.NullTime

	err := row.Scan(&meta.ChatSessionID, &meta.ChatPlatform, &meta.ChatUserID,
		&lastMessageID, &lastMessageAt, &meta.MessageCount, &meta.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found: %s", chatSessionID)
	}
	if err != nil {
		return nil, err
	}

	meta.LastMessageID = lastMessageID.String
	meta.LastMessageAt = lastMessageAt.Time
	return &meta, nil
}

// ListUserSessions 列出用户的所有会话
func (p *PostgreStorage) ListUserSessions(ctx context.Context, platform, userID string) ([]string, error) {
	query := `SELECT chat_session_id FROM session_meta 
	          WHERE chat_platform = $1 AND chat_user_id = $2
	          ORDER BY last_message_at DESC`

	rows, err := p.db.QueryContext(ctx, query, platform, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var sessions []string
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return nil, err
		}
		sessions = append(sessions, sessionID)
	}
	return sessions, rows.Err()
}

// DeleteSession 删除会话
func (p *PostgreStorage) DeleteSession(ctx context.Context, chatSessionID string) error {
	// 软删除消息
	query := `UPDATE messages SET deleted = TRUE, deleted_at = $1 WHERE chat_session_id = $2`
	_, err := p.db.ExecContext(ctx, query, time.Now(), chatSessionID)
	if err != nil {
		return err
	}

	// 删除会话元数据
	_, err = p.db.ExecContext(ctx, `DELETE FROM session_meta WHERE chat_session_id = $1`, chatSessionID)
	return err
}

// updateSessionMeta 更新会话元数据
func (p *PostgreStorage) updateSessionMeta(ctx context.Context, msg *ChatAppMessage) {
	query := `
		INSERT INTO session_meta (chat_session_id, chat_platform, chat_user_id, last_message_id, last_message_at, message_count, updated_at)
		VALUES ($1, $2, $3, $4, $5, 1, $6)
		ON CONFLICT (chat_session_id) DO UPDATE SET
			last_message_id = EXCLUDED.last_message_id,
			last_message_at = EXCLUDED.last_message_at,
			message_count = session_meta.message_count + 1,
			updated_at = EXCLUDED.updated_at
	`
	_, err := p.db.ExecContext(ctx, query,
		msg.ChatSessionID, msg.ChatPlatform, msg.ChatUserID,
		msg.ID, msg.CreatedAt, msg.UpdatedAt,
	)
	// 忽略错误，确保主流程不受影响
	_ = err
}

// buildListQuery 构建列表查询
func (p *PostgreStorage) buildListQuery(query *MessageQuery) (string, []interface{}) {
	var conditions []string
	var args []interface{}
	argNum := 1

	if query.ChatSessionID != "" {
		conditions = append(conditions, fmt.Sprintf("chat_session_id = $%d", argNum))
		args = append(args, query.ChatSessionID)
		argNum++
	}
	if query.EngineSessionID != uuid.Nil {
		conditions = append(conditions, fmt.Sprintf("engine_session_id = $%d", argNum))
		args = append(args, query.EngineSessionID)
		argNum++
	}
	if query.ProviderSessionID != "" {
		conditions = append(conditions, fmt.Sprintf("provider_session_id = $%d", argNum))
		args = append(args, query.ProviderSessionID)
		argNum++
	}
	if len(query.MessageTypes) > 0 {
		placeholders := make([]string, len(query.MessageTypes))
		for i, mt := range query.MessageTypes {
			placeholders[i] = fmt.Sprintf("$%d", argNum)
			args = append(args, string(mt))
			argNum++
		}
		conditions = append(conditions, fmt.Sprintf("message_type IN (%s)", strings.Join(placeholders, ",")))
	}
	if query.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argNum))
		args = append(args, *query.StartTime)
	}
	if query.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argNum))
		args = append(args, *query.EndTime)
	}
	if !query.IncludeDeleted {
		conditions = append(conditions, "(deleted = FALSE OR deleted IS NULL)")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	orderClause := "ORDER BY created_at DESC"
	if query.Ascending {
		orderClause = "ORDER BY created_at ASC"
	}

	limitClause := ""
	if query.Limit > 0 {
		limitClause = fmt.Sprintf("LIMIT %d", query.Limit)
	} else {
		limitClause = "LIMIT 100"
	}

	sql := fmt.Sprintf(`
		SELECT id, chat_session_id, chat_platform, chat_user_id, chat_bot_user_id,
		       chat_channel_id, chat_thread_id, engine_session_id, engine_namespace,
		       provider_session_id, provider_type, message_type, from_user_id,
		       from_user_name, to_user_id, content, metadata, created_at,
		       updated_at, deleted, deleted_at
		FROM messages
		%s %s %s
	`, whereClause, orderClause, limitClause)

	return sql, args
}

// buildCountQuery 构建计数查询
func (p *PostgreStorage) buildCountQuery(query *MessageQuery) (string, []interface{}) {
	conditions := []string{}
	var args []interface{}

	if query.ChatSessionID != "" {
		conditions = append(conditions, fmt.Sprintf("chat_session_id = $%d", 1))
		args = append(args, query.ChatSessionID)
	}
	if !query.IncludeDeleted {
		conditions = append(conditions, "(deleted = FALSE OR deleted IS NULL)")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	return fmt.Sprintf("SELECT COUNT(*) FROM messages %s", whereClause), args
}

// scanMessage 扫描消息行
func (p *PostgreStorage) scanMessage(rows interface{ Scan(...interface{}) error }) (*ChatAppMessage, error) {
	var msg ChatAppMessage
	var content sql.NullString
	var metadata []byte
	var deleted sql.NullBool
	var deletedAt sql.NullTime

	err := rows.Scan(
		&msg.ID, &msg.ChatSessionID, &msg.ChatPlatform, &msg.ChatUserID, &msg.ChatBotUserID,
		&msg.ChatChannelID, &msg.ChatThreadID, &msg.EngineSessionID, &msg.EngineNamespace,
		&msg.ProviderSessionID, &msg.ProviderType, &msg.MessageType, &msg.FromUserID,
		&msg.FromUserName, &msg.ToUserID, &content, &metadata, &msg.CreatedAt,
		&msg.UpdatedAt, &deleted, &deletedAt,
	)
	if err != nil {
		return nil, err
	}

	msg.Content = content.String
	if len(metadata) > 0 {
		// TODO: 解析 JSONB 元数据
		// 实际实现需要 json.Unmarshal 到 map[string]any
		_ = metadata // 避免编译器警告
	}

	msg.Deleted = deleted.Bool
	if deletedAt.Valid {
		msg.DeletedAt = &deletedAt.Time
	}

	return &msg, nil
}

// GetStrategy 获取存储策略
func (p *PostgreStorage) GetStrategy() StorageStrategy {
	return p.strategy
}

// SetStrategy 设置存储策略
func (p *PostgreStorage) SetStrategy(s StorageStrategy) error {
	p.strategy = s
	return nil
}
