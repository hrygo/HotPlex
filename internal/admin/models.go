package admin

// ErrorResponse is the standard error envelope returned by all endpoints.
type ErrorResponse struct {
	Error string `json:"error" example:"not found"`
}

// StatusResponse is returned for simple ok/status operations.
type StatusResponse struct {
	Status string `json:"status" example:"ok"`
}

// SessionListResponse is returned by GET /admin/sessions.
type SessionListResponse struct {
	Sessions []any `json:"sessions"`
	Limit    int   `json:"limit" example:"100"`
	Offset   int   `json:"offset" example:"0"`
}

// CreateSessionResponse is returned by POST /admin/sessions.
type CreateSessionResponse struct {
	SessionID string `json:"session_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// GatewaySessionListResponse is returned by GET /api/sessions.
type GatewaySessionListResponse struct {
	Sessions []any  `json:"sessions"`
	Limit    int    `json:"limit" example:"100"`
	Offset   int    `json:"offset" example:"0"`
	Platform string `json:"platform" example:"webchat"`
}

// GatewayCreateSessionResponse is returned by POST /api/sessions.
type GatewayCreateSessionResponse struct {
	SessionID string `json:"session_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// SwitchWorkDirRequest is the request body for POST /api/sessions/{id}/cd.
type SwitchWorkDirRequest struct {
	WorkDir string `json:"work_dir" example:"/home/user/project"`
}

// SwitchWorkDirResponse is returned by POST /api/sessions/{id}/cd.
type SwitchWorkDirResponse struct {
	OldSessionID string `json:"old_session_id"`
	NewSessionID string `json:"new_session_id"`
	WorkDir      string `json:"work_dir"`
}

// HistoryResponse is returned by GET /api/sessions/{id}/history.
type HistoryResponse struct {
	Records []any `json:"records"`
	HasMore bool  `json:"has_more"`
}

// EventsResponse is returned by GET /api/sessions/{id}/events.
type EventsResponse struct {
	Events    []any `json:"events"`
	OldestSeq int64 `json:"oldest_seq"`
	NewestSeq int64 `json:"newest_seq"`
	HasOlder  bool  `json:"has_older"`
}

// GatewayStatsDetail holds per-component gateway statistics.
type GatewayStatsDetail struct {
	UptimeSeconds        int `json:"uptime_seconds"`
	WebsocketConnections int `json:"websocket_connections"`
	SessionsActive       int `json:"sessions_active"`
	SessionsTotal        int `json:"sessions_total"`
}

// DatabaseStatsDetail holds database statistics.
type DatabaseStatsDetail struct {
	SessionsCount int `json:"sessions_count"`
	DBSizeMB      int `json:"db_size_mb"`
}

// StatsResponse is returned by GET /admin/stats.
type StatsResponse struct {
	Gateway  GatewayStatsDetail     `json:"gateway"`
	Workers  map[string]interface{} `json:"workers"`
	Database DatabaseStatsDetail    `json:"database"`
}

// HealthResponse is returned by GET /admin/health.
type HealthResponse struct {
	Status  string                 `json:"status" example:"healthy"`
	Checks  map[string]interface{} `json:"checks"`
	Version string                 `json:"version"`
}

// WorkerHealthResponse is returned by GET /admin/health/workers.
type WorkerHealthResponse struct {
	Status    string        `json:"status" example:"ok"`
	Workers   []interface{} `json:"workers"`
	CheckedAt string        `json:"checked_at" example:"2024-01-01T00:00:00Z"`
}

// LogEntry is a single log record returned by GET /admin/logs.
type LogEntry struct {
	Time      string `json:"time"`
	Level     string `json:"level"`
	Msg       string `json:"msg"`
	SessionID string `json:"session_id,omitempty"`
}

// LogsResponse is returned by GET /admin/logs.
type LogsResponse struct {
	Logs  []LogEntry `json:"logs"`
	Total int        `json:"total"`
	Limit int        `json:"limit" example:"100"`
}

// PoolStatsResponse is returned by GET /admin/sessions/pool.
type PoolStatsResponse struct {
	Total int `json:"total"`
	Max   int `json:"max"`
	Users int `json:"users"`
}

// ConfigValidateRequest is the request body for POST /admin/config/validate.
type ConfigValidateRequest struct {
	Gateway  *ConfigValidateGateway  `json:"gateway,omitempty"`
	DB       *ConfigValidateDB       `json:"db,omitempty"`
	Worker   *ConfigValidateWorker   `json:"worker,omitempty"`
	Security *ConfigValidateSecurity `json:"security,omitempty"`
	Session  *ConfigValidateSession  `json:"session,omitempty"`
	Pool     *ConfigValidatePool     `json:"pool,omitempty"`
}

// ConfigValidateGateway holds gateway sub-config for validation.
type ConfigValidateGateway struct {
	Addr               string `json:"addr,omitempty"`
	ReadBufferSize     int    `json:"read_buffer_size,omitempty"`
	WriteBufferSize    int    `json:"write_buffer_size,omitempty"`
	BroadcastQueueSize int    `json:"broadcast_queue_size,omitempty"`
}

// ConfigValidateDB holds DB sub-config for validation.
type ConfigValidateDB struct {
	Path string `json:"path,omitempty"`
}

// ConfigValidateWorker holds worker sub-config for validation.
type ConfigValidateWorker struct {
	IdleTimeout      string `json:"idle_timeout,omitempty"`
	ExecutionTimeout string `json:"execution_timeout,omitempty"`
}

// ConfigValidateSecurity holds security sub-config for validation.
type ConfigValidateSecurity struct {
	TLSEnabled bool `json:"tls_enabled,omitempty"`
}

// ConfigValidateSession holds session sub-config for validation.
type ConfigValidateSession struct {
	RetentionPeriod string `json:"retention_period,omitempty"`
	GCScanInterval  string `json:"gc_scan_interval,omitempty"`
}

// ConfigValidatePool holds pool sub-config for validation.
type ConfigValidatePool struct {
	MaxSize int `json:"max_size,omitempty"`
}

// ConfigValidateResponse is returned by POST /admin/config/validate.
type ConfigValidateResponse struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// RollbackRequest is the request body for POST /admin/config/rollback.
type RollbackRequest struct {
	Version int `json:"version" example:"3"`
}

// RollbackResponse is returned by POST /admin/config/rollback.
type RollbackResponse struct {
	OK           bool `json:"ok"`
	RolledBack   int  `json:"rolled_back"`
	HistoryIndex int  `json:"history_index"`
}

// DebugSessionResponse is returned by GET /admin/debug/sessions/{id}.
type DebugSessionResponse struct {
	Session map[string]interface{} `json:"session"`
	Debug   DebugInfo              `json:"debug"`
}

// DebugInfo holds debug details for a session.
type DebugInfo struct {
	Available    bool        `json:"available"`
	HasWorker    bool        `json:"has_worker"`
	TurnCount    int         `json:"turn_count"`
	LastSeqSent  interface{} `json:"last_seq_sent"`
	WorkerHealth interface{} `json:"worker_health"`
}

// RestartResponse is returned by POST /admin/restart.
type RestartResponse struct {
	Status string `json:"status" example:"restarting"`
}

// CreateAPIKeyRequest is the request body for POST /admin/api-keys.
type CreateAPIKeyRequest struct {
	UserID      string `json:"user_id" example:"alice"`
	Description string `json:"description,omitempty" example:"CI bot token"`
}

// SystemPromptPreviewResponse is returned by GET /admin/bots/{name}/preview.
type SystemPromptPreviewResponse struct {
	Preview string `json:"preview"`
}

// WriteAgentConfigRequest is the request body for PUT /admin/bots/{name}/config/{file}.
type WriteAgentConfigRequest struct {
	Content string `json:"content" example:"You are a helpful assistant."`
}

// CreateBotRequest is the request body for POST /admin/bots.
type CreateBotRequest struct {
	Name string `json:"name" example:"my-bot"`
	BotConfigAttrs
}
