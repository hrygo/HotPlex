package provider

// StreamMessage represents a single event in the stream-json format emitted by CLI tools.
// This is a minimal copy for the provider package to avoid circular dependencies.
type StreamMessage struct {
	Message      *AssistantMessage `json:"message,omitempty"`
	Input        map[string]any    `json:"input,omitempty"`
	Type         string            `json:"type"`
	Timestamp    string            `json:"timestamp,omitempty"`
	SessionID    string            `json:"session_id,omitempty"`
	Role         string            `json:"role,omitempty"`
	Name         string            `json:"name,omitempty"`
	Output       string            `json:"output,omitempty"`
	Status       string            `json:"status,omitempty"`
	Error        string            `json:"error,omitempty"`
	Content      []ContentBlock    `json:"content,omitempty"`
	Duration     int               `json:"duration_ms,omitempty"`
	Subtype      string            `json:"subtype,omitempty"`
	IsError      bool              `json:"is_error,omitempty"`
	TotalCostUSD float64           `json:"total_cost_usd,omitempty"`
	Usage        *UsageStats       `json:"usage,omitempty"`
	Result       string            `json:"result,omitempty"`
}

// UsageStats represents the token consumption breakdown.
type UsageStats struct {
	InputTokens           int32 `json:"input_tokens"`
	OutputTokens          int32 `json:"output_tokens"`
	CacheWriteInputTokens int32 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens  int32 `json:"cache_read_input_tokens,omitempty"`
}

// GetContentBlocks returns the primary content blocks of the message.
func (m *StreamMessage) GetContentBlocks() []ContentBlock {
	if m.Message != nil && len(m.Message.Content) > 0 {
		return m.Message.Content
	}
	return m.Content
}

// AssistantMessage represents the structured message emitted by the model.
type AssistantMessage struct {
	ID      string         `json:"id,omitempty"`
	Type    string         `json:"type,omitempty"`
	Role    string         `json:"role,omitempty"`
	Content []ContentBlock `json:"content,omitempty"`
}

// ContentBlock represents an atomic unit of model output.
type ContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	Name      string         `json:"name,omitempty"`
	ID        string         `json:"id,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	Content   string         `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

// GetUnifiedToolID returns a tool identifier suitable for matching calls with results.
func (b *ContentBlock) GetUnifiedToolID() string {
	if b.ToolUseID != "" {
		return b.ToolUseID
	}
	return b.ID
}

// EventMeta contains detailed metadata for streaming events.
type EventMeta struct {
	DurationMs       int64  `json:"duration_ms,omitempty"`
	TotalDurationMs  int64  `json:"total_duration_ms,omitempty"`
	ToolName         string `json:"tool_name,omitempty"`
	ToolID           string `json:"tool_id,omitempty"`
	Status           string `json:"status,omitempty"`
	ErrorMsg         string `json:"error_msg,omitempty"`
	InputTokens      int32  `json:"input_tokens,omitempty"`
	OutputTokens     int32  `json:"output_tokens,omitempty"`
	CacheWriteTokens int32  `json:"cache_write_tokens,omitempty"`
	CacheReadTokens  int32  `json:"cache_read_tokens,omitempty"`
	InputSummary     string `json:"input_summary,omitempty"`
	OutputSummary    string `json:"output_summary,omitempty"`
	FilePath         string `json:"file_path,omitempty"`
	LineCount        int32  `json:"line_count,omitempty"`
	Progress         int32  `json:"progress,omitempty"`
	TotalSteps       int32  `json:"total_steps,omitempty"`
	CurrentStep      int32  `json:"current_step,omitempty"`
}

// EventWithMeta extends the basic event with metadata for observability.
type EventWithMeta struct {
	EventType string     `json:"event_type"`
	EventData string     `json:"event_data"`
	Meta      *EventMeta `json:"meta,omitempty"`
}

// NewEventWithMeta creates a new EventWithMeta with guaranteed non-nil Meta.
func NewEventWithMeta(eventType, eventData string, meta *EventMeta) *EventWithMeta {
	if meta == nil {
		meta = &EventMeta{}
	}
	return &EventWithMeta{
		EventType: eventType,
		EventData: eventData,
		Meta:      meta,
	}
}
