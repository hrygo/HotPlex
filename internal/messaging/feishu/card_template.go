package feishu

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

// Card header template color constants (Feishu CardKit v2).
const (
	headerBlue   = "blue"
	headerWathet = "wathet"
	headerGrey   = "grey"
	headerOrange = "orange"
	headerYellow = "yellow"
	headerViolet = "violet"
	headerRed    = "red"
)

// cardHeader defines a Card JSON 2.0 header component.
type cardHeader struct {
	Title    string    // Required.
	Subtitle string    // Optional.
	Template string    // Optional. Color theme (blue, wathet, grey, etc.).
	Tags     []cardTag // Optional. Up to 3 text_tag_list entries (server truncates excess).
}

// cardTag defines a text_tag_list entry in the card header.
type cardTag struct {
	Text  string
	Color string
}

// toMap converts cardHeader to a map for JSON serialization.
// Zero-value omission: Template empty -> omit; Tags nil/empty -> omit; Subtitle empty -> omit.
// Returns nil if Title is empty.
func (h cardHeader) toMap() map[string]any {
	if h.Title == "" {
		return nil
	}
	m := map[string]any{
		"title": map[string]any{"tag": "plain_text", "content": h.Title},
	}
	if h.Subtitle != "" {
		m["subtitle"] = map[string]any{"tag": "plain_text", "content": h.Subtitle}
	}
	if h.Template != "" {
		m["template"] = h.Template
	}
	if len(h.Tags) > 0 {
		tags := make([]map[string]any, 0, len(h.Tags))
		for _, t := range h.Tags {
			if t.Text == "" {
				continue
			}
			tag := map[string]any{
				"tag":  "text_tag",
				"text": map[string]any{"tag": "plain_text", "content": t.Text},
			}
			if t.Color != "" {
				tag["color"] = t.Color
			}
			tags = append(tags, tag)
		}
		if len(tags) > 0 {
			m["text_tag_list"] = tags
		}
	}
	return m
}

// buildCard constructs a standard CardKit v2 card (non-streaming) with optional header.
func buildCard(header cardHeader, config map[string]any, elements []map[string]any) string {
	card := map[string]any{
		"schema": "2.0",
		"config": config,
		"body":   map[string]any{"elements": elements},
	}
	if hm := header.toMap(); hm != nil {
		card["header"] = hm
	}
	return encodeCard(card)
}

// buildV1Card constructs a JSON 1.0 card (no schema field, elements at root level).
// Required for interactive elements like action + copy_text that are not supported in JSON 2.0.
func buildV1Card(header cardHeader, config map[string]any, elements []map[string]any) string {
	card := map[string]any{
		"config":   config,
		"elements": elements,
	}
	if hm := header.toMap(); hm != nil {
		card["header"] = hm
	}
	return encodeCard(card)
}

// toolActivityElementID is the element_id for the tool activity strip.
const toolActivityElementID = "tool_activity"

// buildStreamingCard constructs a streaming card with streaming_mode, element_id, summary, and optional header.
func buildStreamingCard(header cardHeader, summary, content, toolActivity string) string {
	elements := []any{
		map[string]any{
			"tag":        "markdown",
			"element_id": streamingElementID,
			"content":    content,
		},
		map[string]any{"tag": "hr"},
		map[string]any{
			"tag":        "markdown",
			"element_id": toolActivityElementID,
			"content":    toolActivity,
		},
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"streaming_mode": true,
			"summary":        map[string]any{"content": summary},
		},
		"body": map[string]any{"elements": elements},
	}
	if hm := header.toMap(); hm != nil {
		card["header"] = hm
	}
	return encodeCard(card)
}

// stringPtr returns a pointer to s. Used for SDK builder patterns.
func stringPtr(s string) *string { return &s }

// shortenModel produces a compact model name for tag display.
// "claude-sonnet-4-20250514" -> "claude-4"; "gpt-4o" -> "gpt-4o".
func shortenModel(name string) string {
	if i := strings.Index(name, "-20"); i > 0 {
		name = name[:i]
	}
	if i := strings.Index(name, "-preview"); i > 0 {
		name = name[:i]
	}
	// Strip provider prefix: "anthropic/claude-4" -> "claude-4"
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// shortenDir extracts the last path segment for tag display.
// "/home/user/project" -> "project"; "" -> "".
func shortenDir(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Base(dir)
}

// turnTags builds text_tag_list from turn metadata (max 3 tags, server truncates excess).
// Order: [#N] neutral, [model] turquoise, [dir·branch] green.
func turnTags(turnNum int, model, branch, workDir string) []cardTag {
	var tags []cardTag
	if turnNum > 0 {
		tags = append(tags, cardTag{Text: fmt.Sprintf("#%d", turnNum)})
	}
	if model != "" {
		tags = append(tags, cardTag{Text: shortenModel(model), Color: "turquoise"})
	}
	// Combine workdir and branch into one tag to stay within 3-tag limit.
	dir := shortenDir(workDir)
	if dir != "" && branch != "" {
		if len(branch) > 24 {
			branch = branch[:24]
		}
		tags = append(tags, cardTag{Text: dir + "·" + branch, Color: "green"})
	} else if dir != "" {
		tags = append(tags, cardTag{Text: dir, Color: "green"})
	} else if branch != "" {
		if len(branch) > 24 {
			branch = branch[:24]
		}
		tags = append(tags, cardTag{Text: branch, Color: "indigo"})
	}
	return tags
}

// Deprecated: buildQuestionElements is retained only for test backward compatibility.
// New code should use buildQuestionCardWithButtons.
func buildQuestionElements(questions []events.Question) []map[string]any {
	var elements []map[string]any
	for _, q := range questions {
		headerLabel := messaging.SanitizeText(q.Header)
		if headerLabel == "" {
			headerLabel = "Question"
		}
		type sanitizedOpt struct {
			Label, Desc string
		}
		opts := make([]sanitizedOpt, len(q.Options))
		for i, opt := range q.Options {
			opts[i] = sanitizedOpt{
				Label: messaging.SanitizeText(opt.Label),
				Desc:  messaging.SanitizeText(opt.Description),
			}
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "**%s**\n%s", headerLabel, messaging.SanitizeText(q.Question))
		if q.MultiSelect {
			sb.WriteString("\n*（可多选）*")
		}
		if len(opts) > 0 {
			sb.WriteString("\n\n")
			for i, opt := range opts {
				if opt.Desc != "" {
					fmt.Fprintf(&sb, "%d. **%s** — %s\n", i+1, opt.Label, opt.Desc)
				} else {
					fmt.Fprintf(&sb, "%d. **%s**\n", i+1, opt.Label)
				}
			}
		}
		elements = append(elements, map[string]any{
			"tag": "markdown", "content": sb.String(),
		})
		if len(opts) > 0 {
			buttons := make([]map[string]any, 0, len(opts))
			for _, opt := range opts {
				buttons = append(buttons, map[string]any{
					"tag":  "button",
					"text": map[string]any{"tag": "plain_text", "content": opt.Label},
					"type": "default",
					"click": map[string]any{
						"tag":   "copy_text",
						"value": opt.Label,
					},
				})
			}
			elements = append(elements, map[string]any{
				"tag": "action", "actions": buttons,
			})
		}
	}
	return elements
}

// Deprecated: questionFooterHint is retained only for test backward compatibility.
// New code should use the inline hint in buildQuestionCardWithButtons.
func questionFooterHint(questions []events.Question) string {
	for _, q := range questions {
		if q.MultiSelect {
			return "💬 点击按钮复制选项文本，可一次发送多个选项（用空格或逗号分隔）\n也可直接回复选项文本或自定义答案"
		}
	}
	return "💬 点击按钮复制选项文本，粘贴发送即可响应\n也可直接回复选项文本或自定义答案"
}

func technicalDetails(content string) map[string]any {
	return map[string]any{
		"tag":      "collapsible_panel",
		"expanded": false,
		"header": map[string]any{
			"title": map[string]any{"tag": "plain_text", "content": "技术详情"},
		},
		"elements": []map[string]any{
			{"tag": "markdown", "content": truncateCardText(content, 3000)},
		},
	}
}

func truncateCardText(content string, maxBytes int) string {
	content = messaging.SanitizeText(content)
	if len(content) <= maxBytes {
		return content
	}
	var out strings.Builder
	for _, r := range content {
		if out.Len()+len(string(r))+len("…") > maxBytes {
			break
		}
		out.WriteRune(r)
	}
	out.WriteString("…")
	return out.String()
}

func permissionActionSummary(toolName string) string {
	name := strings.ToLower(toolName)
	switch {
	case strings.Contains(name, "write"), strings.Contains(name, "patch"), strings.Contains(name, "edit"):
		return "修改文件或配置"
	case strings.Contains(name, "read"), strings.Contains(name, "search"):
		return "读取项目内容"
	case strings.Contains(name, "command"), strings.Contains(name, "shell"), strings.Contains(name, "bash"), strings.Contains(name, "exec"):
		return "执行命令"
	default:
		return "执行工具操作"
	}
}

func elicitationDataScope(data *events.ElicitationRequestData) string {
	if len(data.RequestedSchema) == 0 {
		return "数据范围未说明。仅在你确认后，Agent 才会继续向该服务提交所需信息。"
	}
	return "请求的数据结构已在技术详情中列出；仅在你确认后，Agent 才会继续提交所需信息。"
}

func buildPermissionCardWithButtons(data *events.PermissionRequestData) string {
	header := cardHeader{
		Title:    permissionActionSummary(data.ToolName),
		Subtitle: "待你确认",
		Template: headerOrange,
		Tags:     []cardTag{{Text: "需要授权", Color: "orange"}},
	}

	purpose := truncateCardText(data.Description, 600)
	if purpose == "" || purpose == truncateCardText(data.ToolName, 600) {
		purpose = "Agent 请求执行一项工具操作。"
	}
	var details strings.Builder
	fmt.Fprintf(&details, "**工具**\n%s", truncateCardText(data.ToolName, 200))
	if len(data.Args) > 0 {
		details.WriteString("\n\n**参数**")
		for _, arg := range data.Args {
			if details.Len() >= 2000 {
				details.WriteString("\n…参数已截断")
				break
			}
			details.WriteString("\n" + truncateCardText(arg, 600))
		}
	}
	details.WriteString("\n\n**请求 ID**\n" + messaging.SanitizeText(data.ID))

	summary := permissionActionSummary(data.ToolName)

	valAllow := map[string]any{"action": "allow", "request_id": data.ID, "summary": summary}
	valDeny := map[string]any{"action": "deny", "request_id": data.ID, "summary": summary}
	elements := []map[string]any{
		{"tag": "markdown", "content": "**目的**\n" + purpose},
		{"tag": "markdown", "content": "**影响范围**\n操作详情会影响当前会话或其关联资源；请在技术详情中核对具体参数。"},
		technicalDetails(details.String()),
		{
			"tag":  "input",
			"name": "reason",
			"placeholder": map[string]any{
				"tag":     "plain_text",
				"content": "可选：填写拒绝理由或补充说明",
			},
		},
		{
			"tag":       "interactive_container",
			"direction": "horizontal",
			"elements": []map[string]any{
				{
					"tag":   "button",
					"text":  map[string]any{"tag": "plain_text", "content": "允许并继续"},
					"type":  "primary",
					"value": valAllow,
				},
				{
					"tag":   "button",
					"text":  map[string]any{"tag": "plain_text", "content": "拒绝"},
					"type":  "danger",
					"value": valDeny,
				},
			},
		},
	}

	return buildCard(header, nil, elements)
}

func buildQuestionCardWithButtons(data *events.QuestionRequestData) string {
	header := cardHeader{
		Title:    "需要你的输入",
		Subtitle: "待你回答",
		Template: headerYellow,
		Tags:     []cardTag{{Text: "待回答", Color: "yellow"}},
	}

	useForm := len(data.Questions) > 1
	for _, question := range data.Questions {
		if question.MultiSelect {
			useForm = true
			break
		}
	}
	var elements []map[string]any
	formElements := make([]map[string]any, 0, len(data.Questions)+2)
	questionKeys := make(map[string]any, len(data.Questions))
	questionOrder := make([]string, 0, len(data.Questions))
	for index, q := range data.Questions {
		questionOrder = append(questionOrder, truncateCardText(q.Question, 600))
		headerLabel := messaging.SanitizeText(q.Header)
		if headerLabel == "" {
			headerLabel = "Question"
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "**%s**\n%s", headerLabel, messaging.SanitizeText(q.Question))
		if q.MultiSelect {
			sb.WriteString("\n*可多选*")
		}
		for _, opt := range q.Options {
			if desc := truncateCardText(opt.Description, 180); desc != "" {
				fmt.Fprintf(&sb, "\n- **%s**：%s", truncateCardText(opt.Label, 75), desc)
			}
		}
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": sb.String(),
		})

		if useForm {
			name := fmt.Sprintf("answer_%d", index)
			questionKeys[name] = truncateCardText(q.Question, 600)
			if len(q.Options) == 0 {
				formElements = append(formElements, map[string]any{
					"tag": "input", "name": name,
					"placeholder": map[string]any{"tag": "plain_text", "content": "输入你的答案"},
				})
				continue
			}
			options := make([]map[string]any, 0, len(q.Options))
			for _, opt := range q.Options {
				label := truncateCardText(opt.Label, 75)
				options = append(options, map[string]any{"text": map[string]any{"tag": "plain_text", "content": label}, "value": label})
			}
			tag := "select_static"
			if q.MultiSelect {
				tag = "multi_select_static"
			}
			formElements = append(formElements, map[string]any{
				"tag": tag, "name": name, "width": "fill", "required": false,
				"placeholder": map[string]any{"tag": "plain_text", "content": "请选择"}, "options": options,
			})
		} else if len(q.Options) > 0 {
			buttons := make([]map[string]any, 0, len(q.Options))
			for _, opt := range q.Options {
				sanitized := messaging.SanitizeText(opt.Label)
				display := sanitized
				if len([]rune(display)) > 75 {
					display = string([]rune(display)[:75])
				}
				summary := "已选择回答"
				buttons = append(buttons, map[string]any{
					"tag":  "button",
					"text": map[string]any{"tag": "plain_text", "content": display},
					"type": "primary",
					"value": map[string]any{
						"action":         "answer",
						"request_id":     data.ID,
						"answer":         sanitized,
						"label":          display,
						"question":       truncateCardText(q.Question, 600),
						"question_order": []string{truncateCardText(q.Question, 600)},
						"summary":        summary,
					},
				})
			}
			elements = append(elements, map[string]any{
				"tag":       "interactive_container",
				"direction": "horizontal",
				"elements":  buttons,
			})
		}
	}
	if useForm {
		formElements = append(formElements, map[string]any{
			"tag": "button", "text": map[string]any{"tag": "plain_text", "content": "提交答案"}, "type": "primary",
			"complex_interaction": true, "action_type": "form_submit", "name": "submit_question",
			"value": map[string]any{"action": "answer", "request_id": data.ID, "summary": "已提交回答", "question_keys": questionKeys, "question_order": questionOrder},
		})
		elements = append(elements, map[string]any{"tag": "form", "name": "question_answers", "elements": formElements})
		return buildCard(header, nil, elements)
	}

	elements = append(elements, map[string]any{
		"tag":  "input",
		"name": "custom_answer",
		"placeholder": map[string]any{
			"tag":     "plain_text",
			"content": "或者输入自定义答案",
		},
	})

	elements = append(elements, map[string]any{
		"tag":       "interactive_container",
		"direction": "horizontal",
		"elements": []map[string]any{
			{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": "提交自定义答案"},
				"type": "primary",
				"value": map[string]any{
					"action":     "answer",
					"request_id": data.ID,
					"summary":    "已提交自定义回答",
				},
			},
		},
	})

	return buildCard(header, nil, elements)
}

func buildElicitationCardWithButtons(data *events.ElicitationRequestData) string {
	header := cardHeader{
		Title:    "外部服务请求",
		Subtitle: "待你确认",
		Template: headerViolet,
		Tags:     []cardTag{{Text: "信息收集", Color: "purple"}},
	}

	var details strings.Builder
	fmt.Fprintf(&details, "**服务**\n%s\n\n**请求内容**\n%s", truncateCardText(data.MCPServerName, 200), truncateCardText(data.Message, 1200))
	if data.URL != "" {
		// Validate URL scheme to prevent javascript: or data: injection in Feishu markdown links.
		// Only http:// and https:// are safe in Feishu card markdown.
		display := data.URL
		if !strings.HasPrefix(data.URL, "http://") && !strings.HasPrefix(data.URL, "https://") {
			display = truncateCardText(data.URL, 500)
			fmt.Fprintf(&details, "\n\n**外部链接**\n%s", display)
		} else {
			fmt.Fprintf(&details, "\n\n**外部链接**\n[%s](%s)", messaging.SanitizeText(display), display)
		}
	}
	if len(data.RequestedSchema) > 0 {
		if schema, err := json.Marshal(data.RequestedSchema); err == nil {
			details.WriteString("\n\n**请求数据结构**\n" + truncateCardText(string(schema), 800))
		}
	}
	details.WriteString("\n\n**请求 ID**\n" + messaging.SanitizeText(data.ID))

	summary := "外部服务信息收集"
	valAccept := map[string]any{"action": "accept", "request_id": data.ID, "summary": summary}
	valDecline := map[string]any{"action": "decline", "request_id": data.ID, "summary": summary}
	elements := []map[string]any{
		{"tag": "markdown", "content": "**请求来源**\n" + truncateCardText(data.MCPServerName, 200)},
		{"tag": "markdown", "content": "**目的**\n" + truncateCardText(data.Message, 600)},
		{"tag": "markdown", "content": "**数据与影响**\n" + elicitationDataScope(data)},
		technicalDetails(details.String()),
		{
			"tag":  "input",
			"name": "comment",
			"placeholder": map[string]any{
				"tag":     "plain_text",
				"content": "可选：补充说明或自定义输入",
			},
		},
		{
			"tag":       "interactive_container",
			"direction": "horizontal",
			"elements": []map[string]any{
				{
					"tag":   "button",
					"text":  map[string]any{"tag": "plain_text", "content": "接受并继续"},
					"type":  "primary",
					"value": valAccept,
				},
				{
					"tag":   "button",
					"text":  map[string]any{"tag": "plain_text", "content": "拒绝"},
					"type":  "danger",
					"value": valDecline,
				},
			},
		},
	}

	return buildCard(header, nil, elements)
}

func buildResolvedCard(action, label, color, summary, operatorID, reason string) map[string]any {
	if color == "" {
		switch action {
		case "allow", "answer", "accept":
			color = "green"
		case "deny", "decline":
			color = "red"
		}
	}

	var elements []map[string]any
	if summary != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**原请求：**\n" + summary,
		})
	}
	if reason != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**反馈/理由：**\n" + reason,
		})
	}

	// Add context: operator and time
	var ctxContent string
	if operatorID != "" {
		ctxContent = fmt.Sprintf("操作人: <at id=%s></at>  |  时间: %s", operatorID, time.Now().Format("2006-01-02 15:04:05"))
	} else {
		ctxContent = fmt.Sprintf("时间: %s", time.Now().Format("2006-01-02 15:04:05"))
	}

	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": ctxContent,
	})

	return map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": label},
			"template": color,
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
}

// buildRetryCard keeps a failed interaction actionable without claiming it was
// accepted by the worker. The original callback value is preserved so retry
// follows the same permission, answer, or elicitation route.
func buildRetryCard(value map[string]any, summary, failure string) map[string]any {
	retryValue := make(map[string]any, len(value))
	for key, item := range value {
		retryValue[key] = item
	}

	elements := []map[string]any{
		{
			"tag":     "markdown",
			"content": "响应暂未提交给 Agent。请重试；如问题持续，请稍后重新发起请求。",
		},
	}
	if failure != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**失败原因**\n" + truncateCardText(failure, 240),
		})
	}
	if summary != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**请求摘要**\n" + messaging.SanitizeText(summary),
		})
	}
	elements = append(elements, map[string]any{
		"tag":       "interactive_container",
		"direction": "horizontal",
		"elements": []map[string]any{
			{
				"tag":   "button",
				"text":  map[string]any{"tag": "plain_text", "content": "重试提交"},
				"type":  "primary",
				"value": retryValue,
			},
		},
	})

	return map[string]any{
		"schema": "2.0",
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": "提交失败，可重试"},
			"template": headerRed,
		},
		"body": map[string]any{"elements": elements},
	}
}
