package storage

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ValidateMessage 验证消息必填字段
func ValidateMessage(msg *ChatAppMessage) error {
	if msg == nil {
		return NewStorageError("VALIDATE_NULL", "message cannot be nil", nil)
	}
	if msg.ChatSessionID == "" {
		return NewStorageError("VALIDATE_SESSION", "chat session ID is required", nil)
	}
	if msg.EngineSessionID == uuid.Nil {
		return NewStorageError("VALIDATE_ENGINE", "engine session ID is required", nil)
	}
	if msg.ProviderSessionID == "" {
		return NewStorageError("VALIDATE_PROVIDER", "provider session ID is required", nil)
	}
	return nil
}

// SanitizeContent 清理消息内容
func SanitizeContent(content string, maxLength int) string {
	if len(content) > maxLength {
		return content[:maxLength] + "..."
	}
	return content
}

// FormatTimestamp 格式化时间戳
func FormatTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// ParseMessageType 解析消息类型
func ParseMessageType(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "user_input", "userinput", "user-input":
		return "user_input"
	case "final_response", "finalresponse", "final-response", "response":
		return "final_response"
	case "tool_use", "tooluse", "tool-use":
		return "tool_use"
	case "tool_result", "toolresult", "tool-result":
		return "tool_result"
	case "error", "error_message":
		return "error"
	default:
		return "unknown"
	}
}

// BuildSessionID 构建会话ID
func BuildSessionID(platform, userID, channelID string) string {
	return fmt.Sprintf("%s_%s_%s", platform, userID, channelID)
}

// GenerateProviderSessionID 生成 Provider 会话ID
func GenerateProviderSessionID() string {
	return uuid.New().String()
}

// MaskSensitiveData 脱敏敏感数据
func MaskSensitiveData(data string) string {
	if len(data) <= 4 {
		return "****"
	}
	return data[:2] + "****" + data[len(data)-2:]
}

// TruncateForLog 截断日志内容
func TruncateForLog(content string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 200
	}
	content = strings.TrimSpace(content)
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "..."
}
