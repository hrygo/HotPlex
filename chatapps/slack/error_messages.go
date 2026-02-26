package slack

// SlackErrorMessages maps Slack API error codes to user-friendly Chinese messages
var SlackErrorMessages = map[string]string{
	// Rate limiting
	"rate_limited": "服务器繁忙，请稍后重试",
	"ratelimited":  "服务器繁忙，请稍后重试",
	"rate_limit":   "服务器繁忙，请稍后重试",

	// Authentication errors
	"not_authed":             "认证失败，请重新配置 Bot Token",
	"invalid_auth":           "认证信息无效，请检查 Token",
	"token_revoked":          "访问令牌已撤销，请重新授权",
	"token_expired":          "访问令牌已过期，请重新授权",
	"account_inactive":       "账户已被禁用，请联系管理员",
	"not_allowed_token_type": "令牌类型不正确",
	"unauthorized":           "未授权，请检查权限",
	"forbidden":              "禁止访问，权限不足",

	// Scope errors
	"missing_scope": "权限不足，请添加必要的作用域",
	"access_denied": "访问被拒绝，权限不足",
	"no_permission": "没有执行此操作的权限",

	// Channel errors
	"channel_not_found":       "找不到指定的频道",
	"not_in_channel":          "Bot 不在该频道中，请先邀请 Bot 加入",
	"is_archived":             "频道已被归档",
	"channel_is_archived":     "频道已被归档",
	"duplicate_channel_found": "已存在同名频道",
	"channel_limit_exceeded":  "频道数量已达上限",
	"read_only":               "频道为只读模式",

	// Message errors
	"message_not_found":            "消息不存在或已被删除",
	"cant_update_message":          "无法更新消息",
	"cant_delete_message":          "无法删除消息",
	"invalid_timestamp":            "消息时间戳无效",
	"compliance_exceeds_retention": "消息超过保留期限",
	"cannot_send_message_to_user":  "无法向该用户发送消息",

	// User errors
	"user_not_found":     "找不到指定的用户",
	"user_is_restricted": "用户受限，无法发送消息",
	"user_is_bot":        "无法向 Bot 用户发送消息",

	// File errors
	"file_not_found":    "文件不存在或已被删除",
	"comment_not_found": "评论不存在或已被删除",

	// Group errors
	"group_not_found":      "群组不存在",
	"user_group_not_found": "用户组不存在",

	// General errors
	"invalid_arguments":  "参数无效",
	"invalid_arg_name":   "参数名无效",
	"invalid_array_json": "JSON 数组格式无效",
	"invalid_cursor":     "分页游标无效",
	"invalid_post_type":  "发布类型无效",
	"invalid_request":    "请求无效",
	"invalid_trigger":    "触发器无效",
	"missing_argument":   "缺少必要参数",
	"validation_error":   "验证失败",
	"not_allowed_type":   "不允许的类型",
}

// MapSlackErrorToUserMessage converts a Slack API error code to a user-friendly Chinese message
func MapSlackErrorToUserMessage(errorCode string) string {
	if msg, ok := SlackErrorMessages[errorCode]; ok {
		return msg
	}
	// Try case-insensitive lookup
	errorCodeLower := toLowerSnakeCase(errorCode)
	if msg, ok := SlackErrorMessages[errorCodeLower]; ok {
		return msg
	}
	// Return original error if no mapping found
	return errorCode
}

// MapSlackErrorToUserMessageWithRawError returns a user-friendly error message with the original error code
func MapSlackErrorToUserMessageWithRawError(errorCode string) string {
	userMsg := MapSlackErrorToUserMessage(errorCode)
	if userMsg == errorCode {
		return "Slack API 错误: " + errorCode
	}
	return userMsg
}

// toLowerSnakeCase converts a string to lower snake_case for case-insensitive matching
func toLowerSnakeCase(s string) string {
	// Convert common Slack error codes to their standard form
	standardForms := map[string]string{
		"ratelimited":                 "rate_limited",
		"rate-limit":                  "rate_limited",
		"notauthed":                   "not_authed",
		"invalidauth":                 "invalid_auth",
		"tokenrevoked":                "token_revoked",
		"tokenexpired":                "token_expired",
		"accountinactive":             "account_inactive",
		"notallowedtokentype":         "not_allowed_token_type",
		"missingscope":                "missing_scope",
		"accessdenied":                "access_denied",
		"nopermission":                "no_permission",
		"channelnotfound":             "channel_not_found",
		"notinchannel":                "not_in_channel",
		"isarchived":                  "is_archived",
		"channelisarchived":           "channel_is_archived",
		"duplicatechannelfound":       "duplicate_channel_found",
		"channellimitexceeded":        "channel_limit_exceeded",
		"messagenotfound":             "message_not_found",
		"cantupdatemessage":           "cant_update_message",
		"cantdeletemessage":           "cant_delete_message",
		"invalidtimestamp":            "invalid_timestamp",
		"cannot_send_message_to_user": "cannot_send_message_to_user",
		"user_not_found":              "user_not_found",
		"userisrestricted":            "user_is_restricted",
		"userisbot":                   "user_is_bot",
		"filenotfound":                "file_not_found",
		"commentnotfound":             "comment_not_found",
		"groupnotfound":               "group_not_found",
		"usergroupnotfound":           "user_group_not_found",
		"invalidarguments":            "invalid_arguments",
		"invalidargname":              "invalid_arg_name",
		"invalidarrayjson":            "invalid_array_json",
		"invalidcursor":               "invalid_cursor",
		"invalidposttype":             "invalid_post_type",
		"invalidrequest":              "invalid_request",
		"invalidtrigger":              "invalid_trigger",
		"missingargument":             "missing_argument",
		"validationerror":             "validation_error",
		"notallowedtype":              "not_allowed_type",
	}

	if standard, ok := standardForms[s]; ok {
		return standard
	}

	// Fallback: simple lowercase conversion
	result := make([]byte, 0, len(s))
	for i, b := range s {
		if b >= 'A' && b <= 'Z' {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, byte(b+32))
		} else {
			result = append(result, byte(b))
		}
	}
	return string(result)
}
