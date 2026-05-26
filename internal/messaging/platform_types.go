package messaging

// PlatformType identifies the messaging platform.
type PlatformType string

const (
	PlatformSlack  PlatformType = "slack"
	PlatformFeishu PlatformType = "feishu"
)

// ExtractPlatformKeys pulls platform-specific fields from generic metadata.
func (p PlatformType) ExtractPlatformKeys(md map[string]any) map[string]string {
	pk := make(map[string]string)
	switch p {
	case PlatformFeishu:
		if v, ok := md["chat_id"].(string); ok && v != "" {
			pk["chat_id"] = v
		}
		if v, ok := md["thread_ts"].(string); ok {
			pk["thread_ts"] = v
		}
		if v, ok := md["user_id"].(string); ok && v != "" {
			pk["user_id"] = v
		}
	case PlatformSlack:
		if v, ok := md["team_id"].(string); ok && v != "" {
			pk["team_id"] = v
		}
		if v, ok := md["channel_id"].(string); ok && v != "" {
			pk["channel_id"] = v
		}
		if v, ok := md["thread_ts"].(string); ok {
			pk["thread_ts"] = v
		}
		if v, ok := md["user_id"].(string); ok && v != "" {
			pk["user_id"] = v
		}
	}
	// bot_id is platform-agnostic — extracted for all platform types.
	if v, ok := md["bot_id"].(string); ok && v != "" {
		pk["bot_id"] = v
	}
	return pk
}
