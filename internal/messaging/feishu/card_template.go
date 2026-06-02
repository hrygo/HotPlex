package feishu

import (
	"fmt"
	"path/filepath"
	"strings"

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

func buildPermissionCardWithButtons(data *events.PermissionRequestData) string {
	header := cardHeader{
		Title:    data.ToolName,
		Subtitle: messaging.SanitizeText(data.Description),
		Template: headerOrange,
		Tags:     []cardTag{{Text: "pending", Color: "orange"}},
	}

	var content strings.Builder
	fmt.Fprintf(&content, "**%s**\n%s", data.ToolName, messaging.SanitizeText(data.Description))
	if len(data.Args) > 0 {
		content.WriteString("\n\n参数：")
		const maxArgBytes = 500
		truncated := false
		for i, arg := range data.Args {
			if i > 0 {
				content.WriteString(", ")
			}
			sanitized := messaging.SanitizeText(arg)
			// Truncate at 500 bytes to avoid oversized card payload (Feishu card 30KB limit).
			// Stacked args (e.g., base64 file contents) can blow past this easily.
			if content.Len()+len(sanitized) > maxArgBytes {
				if !truncated {
					content.WriteString("...")
					truncated = true
				}
				continue
			}
			content.WriteString(sanitized)
		}
	}

	valAllow := map[string]any{"action": "allow", "request_id": data.ID}
	valDeny := map[string]any{"action": "deny", "request_id": data.ID}
	elements := []map[string]any{
		{"tag": "markdown", "content": content.String()},
		{
			"tag": "action",
			"actions": []map[string]any{
				{
					"tag":   "button",
					"text":  map[string]any{"tag": "plain_text", "content": "✅ 允许"},
					"type":  "primary",
					"value": valAllow,
				},
				{
					"tag":   "button",
					"text":  map[string]any{"tag": "plain_text", "content": "❌ 拒绝"},
					"type":  "danger",
					"value": valDeny,
				},
			},
		},
	}

	return buildCard(header, map[string]any{"wide_screen_mode": true}, elements)
}

func buildQuestionCardWithButtons(data *events.QuestionRequestData) string {
	title := data.ToolName
	if title == "" {
		title = "用户输入请求"
	}
	header := cardHeader{
		Title:    title,
		Template: headerYellow,
	}

	var elements []map[string]any
	for _, q := range data.Questions {
		headerLabel := messaging.SanitizeText(q.Header)
		if headerLabel == "" {
			headerLabel = "Question"
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "**%s**\n%s", headerLabel, messaging.SanitizeText(q.Question))
		if q.MultiSelect {
			sb.WriteString("\n*（可多选）*")
		}
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": sb.String(),
		})

		if len(q.Options) > 0 {
			buttons := make([]map[string]any, 0, len(q.Options))
			for _, opt := range q.Options {
				sanitized := messaging.SanitizeText(opt.Label)
				display := sanitized
				if len([]rune(display)) > 75 {
					display = string([]rune(display)[:75])
				}
				buttons = append(buttons, map[string]any{
					"tag":  "button",
					"text": map[string]any{"tag": "plain_text", "content": display},
					"type": "primary",
					"value": map[string]any{
						"action":     "answer",
						"request_id": data.ID,
						"answer":     sanitized,
						"label":      display,
					},
				})
			}
			elements = append(elements, map[string]any{
				"tag":     "action",
				"actions": buttons,
			})
		}
	}

	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": "💬 点击按钮直接选择，或直接回复自定义答案",
	})

	return buildCard(header, map[string]any{"wide_screen_mode": true}, elements)
}

func buildElicitationCardWithButtons(data *events.ElicitationRequestData) string {
	header := cardHeader{
		Title:    data.MCPServerName,
		Subtitle: messaging.SanitizeText(data.Message),
		Template: headerViolet,
	}

	var content strings.Builder
	fmt.Fprintf(&content, "**%s**\n%s", data.MCPServerName, messaging.SanitizeText(data.Message))
	if data.URL != "" {
		// Validate URL scheme to prevent javascript: or data: injection in Feishu markdown links.
		// Only http:// and https:// are safe in Feishu card markdown.
		display := data.URL
		if !strings.HasPrefix(data.URL, "http://") && !strings.HasPrefix(data.URL, "https://") {
			display = messaging.SanitizeText(data.URL)
			fmt.Fprintf(&content, "\n\n📋 %s", display)
		} else {
			fmt.Fprintf(&content, "\n\n[🔗 %s](%s)", messaging.SanitizeText(display), display)
		}
	}

	valAccept := map[string]any{"action": "accept", "request_id": data.ID}
	valDecline := map[string]any{"action": "decline", "request_id": data.ID}
	elements := []map[string]any{
		{"tag": "markdown", "content": content.String()},
		{
			"tag": "action",
			"actions": []map[string]any{
				{
					"tag":   "button",
					"text":  map[string]any{"tag": "plain_text", "content": "✅ 接受"},
					"type":  "primary",
					"value": valAccept,
				},
				{
					"tag":   "button",
					"text":  map[string]any{"tag": "plain_text", "content": "❌ 拒绝"},
					"type":  "danger",
					"value": valDecline,
				},
			},
		},
	}

	return buildCard(header, map[string]any{"wide_screen_mode": true}, elements)
}

func buildResolvedCard(action, label, color string) map[string]any {
	if color == "" {
		switch action {
		case "allow", "answer", "accept":
			color = "green"
		case "deny", "decline":
			color = "red"
		}
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title":    map[string]any{"tag": "plain_text", "content": label},
			"template": color,
		},
	}
}
