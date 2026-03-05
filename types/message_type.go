package types

// MessageType 统一消息类型定义 (全项目共享，DRY 原则)
type MessageType string

const (
	// ✅ 可存储类型 (白名单)
	MessageTypeUserInput     MessageType = "user_input"
	MessageTypeFinalResponse MessageType = "final_response"

	// ❌ 不可存储类型 (中间过程，自动过滤)
	MessageTypeThinking            MessageType = "thinking"
	MessageTypeAction              MessageType = "action"
	MessageTypeToolUse             MessageType = "tool_use"
	MessageTypeToolResult          MessageType = "tool_result"
	MessageTypeStatus              MessageType = "status"
	MessageTypeError               MessageType = "error"
	MessageTypePlanMode            MessageType = "plan_mode"
	MessageTypeExitPlanMode        MessageType = "exit_plan_mode"
	MessageTypeAskUserQuestion     MessageType = "ask_user_question"
	MessageTypeDangerBlock         MessageType = "danger_block"
	MessageTypeSessionStats        MessageType = "session_stats"
	MessageTypeCommandProgress     MessageType = "command_progress"
	MessageTypeCommandComplete     MessageType = "command_complete"
	MessageTypeSystem              MessageType = "system"
	MessageTypeUser                MessageType = "user"
	MessageTypeStepStart           MessageType = "step_start"
	MessageTypeStepFinish          MessageType = "step_finish"
	MessageTypeRaw                 MessageType = "raw"
	MessageTypeSessionStart        MessageType = "session_start"
	MessageTypeEngineStarting      MessageType = "engine_starting"
	MessageTypeUserMessageReceived MessageType = "user_message_received"
	MessageTypePermissionRequest   MessageType = "permission_request"
	MessageTypeAnswer              MessageType = "answer"
)

// IsStorable 判断消息类型是否可存储 (单一事实来源)
func (t MessageType) IsStorable() bool {
	return t == MessageTypeUserInput || t == MessageTypeFinalResponse
}

// IsIntermediate 判断是否为中间过程消息
func (t MessageType) IsIntermediate() bool {
	return !t.IsStorable()
}
