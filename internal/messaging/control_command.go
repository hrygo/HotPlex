package messaging

import (
	"fmt"
	"strings"
	"sync"

	"github.com/hrygo/hotplex/pkg/events"
)

// ControlCommandResult holds the parsed control action and a human-readable label.
type ControlCommandResult struct {
	Action events.ControlAction
	Label  string // e.g. "gc" or "reset"
	Arg    string // optional argument, e.g. path for cd
}

// CommandMap is a generic slash + natural language command lookup table.
type CommandMap[T any] struct {
	slash   map[string]T
	natural map[string]T
}

// NewCommandMap creates a command map from slash and natural language entries.
func NewCommandMap[T any](slash, natural map[string]T) *CommandMap[T] {
	return &CommandMap[T]{slash: slash, natural: natural}
}

// Lookup finds a command by normalized text, checking slash entries first.
func (m *CommandMap[T]) Lookup(normalized string) (T, bool) {
	if v, ok := m.slash[normalized]; ok {
		return v, true
	}
	return m.LookupNatural(normalized)
}

// LookupSlash finds a command in the slash entries only.
func (m *CommandMap[T]) LookupSlash(normalized string) (T, bool) {
	v, ok := m.slash[normalized]
	return v, ok
}

// LookupNatural finds a command in the natural language entries only.
func (m *CommandMap[T]) LookupNatural(normalized string) (T, bool) {
	v, ok := m.natural[normalized]
	return v, ok
}

// controlCommands maps slash and natural language triggers to control actions.
var controlCommands = NewCommandMap(
	map[string]ControlCommandResult{
		"/gc":    {Action: events.ControlActionGC, Label: "gc"},
		"/park":  {Action: events.ControlActionGC, Label: "gc"},
		"/reset": {Action: events.ControlActionReset, Label: "reset"},
		"/new":   {Action: events.ControlActionReset, Label: "reset"},
		"/cd":    {Action: events.ControlActionCD, Label: "cd"},
	},
	map[string]ControlCommandResult{
		"$gc":    {Action: events.ControlActionGC, Label: "gc"},
		"$休眠":    {Action: events.ControlActionGC, Label: "gc"},
		"$挂起":    {Action: events.ControlActionGC, Label: "gc"},
		"$重置":    {Action: events.ControlActionReset, Label: "reset"},
		"$reset": {Action: events.ControlActionReset, Label: "reset"},
	},
)

// ParseControlCommand checks whether text is a control command.
// Returns nil if the text is not a control command.
// Matching: exact match after trim + lowercase + strip trailing punctuation.
// Special case: /cd and $cd/$切换目录 accept a path argument.
func ParseControlCommand(text string) *ControlCommandResult {
	t := strings.TrimSpace(text)
	tl := strings.ToLower(trimTrailingPunct(t))

	// cd commands: prefix match to extract path argument.
	if arg, ok := parseCdCommand(t, tl); ok {
		return &arg
	}

	if result, ok := controlCommands.Lookup(tl); ok {
		return &result
	}
	return nil
}

// cdPrefixes lists the slash and natural language triggers that accept a path argument.
var cdPrefixes = []struct {
	prefix string
	isCase bool // true = case-sensitive match on original text
}{
	{"/cd ", false},
	{"$cd ", false},
	{"$切换目录 ", true},
	{"$CD ", false},
}

func parseCdCommand(original, normalized string) (ControlCommandResult, bool) {
	for _, p := range cdPrefixes {
		src := normalized
		if p.isCase {
			src = original
		}
		if after, ok := strings.CutPrefix(src, p.prefix); ok {
			arg := strings.TrimSpace(after)
			return ControlCommandResult{Action: events.ControlActionCD, Label: "cd", Arg: arg}, true
		}
	}
	return ControlCommandResult{}, false
}

func parseWorkerSlashCommands(text string) (base, args string) {
	parts := strings.SplitN(text, " ", 2)
	base = parts[0]
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return base, args
}

// workerSlashCommandsWithArgs lists slash commands that accept arguments.
var workerSlashCommandsWithArgs = map[string]bool{
	"/model":  true,
	"/perm":   true,
	"/effort": true,
}

// workerCommands maps slash and natural language triggers to worker stdio commands.
var workerCommands = NewCommandMap(
	map[string]events.WorkerStdioCommand{
		"/context": events.StdioContextUsage,
		"/skills":  events.StdioSkills,
		"/mcp":     events.StdioMCPStatus,
		"/model":   events.StdioSetModel,
		"/perm":    events.StdioSetPermMode,
		"/compact": events.StdioCompact,
		"/clear":   events.StdioClear,
		"/effort":  events.StdioEffort,
		"/rewind":  events.StdioRewind,
		"/commit":  events.StdioCommit,
	},
	map[string]events.WorkerStdioCommand{
		"$context": events.StdioContextUsage,
		"$上下文":     events.StdioContextUsage,
		"$skills":  events.StdioSkills,
		"$技能":      events.StdioSkills,
		"$mcp":     events.StdioMCPStatus,
		"$model":   events.StdioSetModel,
		"$切换模型":    events.StdioSetModel,
		"$perm":    events.StdioSetPermMode,
		"$权限模式":    events.StdioSetPermMode,
		"$compact": events.StdioCompact,
		"$压缩":      events.StdioCompact,
		"$clear":   events.StdioClear,
		"$清空":      events.StdioClear,
		"$effort":  events.StdioEffort,
		"$rewind":  events.StdioRewind,
		"$回退":      events.StdioRewind,
		"$commit":  events.StdioCommit,
		"$提交":      events.StdioCommit,
	},
)

// WorkerCommandResult holds the parsed worker stdio command and its arguments.
type WorkerCommandResult struct {
	Command events.WorkerStdioCommand
	Label   string
	Args    string
	Extra   map[string]any
}

// ParseWorkerCommand checks whether text is a worker stdio command.
// Returns nil if the text is not a worker command.
//
// Supported formats:
//   - Slash commands: /context, /mcp, /model sonnet-4, /perm bypassPermissions, /effort high
//   - Natural language: $上下文, $MCP状态, $切换模型, etc. (require $ prefix)
func ParseWorkerCommand(text string) *WorkerCommandResult {
	t := strings.TrimSpace(strings.ToLower(text))
	t = trimTrailingPunct(t)

	base, args := parseWorkerSlashCommands(t)
	if cmd, ok := workerCommands.LookupSlash(base); ok {
		label := base
		if !workerSlashCommandsWithArgs[base] {
			args = ""
		}
		return &WorkerCommandResult{
			Command: cmd,
			Label:   label,
			Args:    args,
		}
	}

	if cmd, ok := workerCommands.LookupNatural(t); ok {
		return &WorkerCommandResult{
			Command: cmd,
			Label:   t,
		}
	}

	return nil
}

// IsHelpCommand checks whether the text is a help request.
// Recognizes: /help, $help, ?, ？
func IsHelpCommand(text string) bool {
	t := strings.TrimSpace(text)
	tl := strings.ToLower(t)
	return tl == "/help" || tl == "$help" || t == "?" || t == "？"
}

// helpSection groups related commands for display.
type helpSection struct {
	Title   string
	Emoji   string
	Entries []helpEntry
}

// helpEntry describes a single command line.
type helpEntry struct {
	Commands []string
	Args     string
	Desc     string
}

var (
	helpText     string
	helpTextOnce sync.Once
)

// HelpText returns the cached help message (generated once).
func HelpText() string {
	helpTextOnce.Do(func() {
		sections := []helpSection{
			{
				Title: "会话控制", Emoji: "🔧",
				Entries: []helpEntry{
					{Commands: []string{"/gc", "/park"}, Desc: "休眠会话（停止 Worker，保留会话）"},
					{Commands: []string{"/reset", "/new"}, Desc: "重置上下文（全新开始）"},
					{Commands: []string{"/cd"}, Args: "<目录>", Desc: "切换工作目录（创建新会话）"},
				},
			},
			{
				Title: "信息与状态", Emoji: "📊",
				Entries: []helpEntry{
					{Commands: []string{"/context"}, Desc: "查看上下文窗口使用量"},
					{Commands: []string{"/skills"}, Desc: "查看已加载的技能列表"},
					{Commands: []string{"/mcp"}, Desc: "查看 MCP 服务器状态"},
				},
			},
			{
				Title: "配置", Emoji: "⚙️",
				Entries: []helpEntry{
					{Commands: []string{"/model"}, Args: "<名称>", Desc: "切换 AI 模型"},
					{Commands: []string{"/perm"}, Args: "<模式>", Desc: "设置权限模式"},
					{Commands: []string{"/effort"}, Args: "<级别>", Desc: "设置推理力度"},
				},
			},
			{
				Title: "对话", Emoji: "💬",
				Entries: []helpEntry{
					{Commands: []string{"/compact"}, Desc: "压缩对话历史"},
					{Commands: []string{"/clear"}, Desc: "清空对话"},
					{Commands: []string{"/rewind"}, Desc: "撤销上一轮对话"},
					{Commands: []string{"/commit"}, Desc: "创建 Git 提交"},
				},
			},
			{
				Title: "提示", Emoji: "💡",
				Entries: []helpEntry{
					{Commands: []string{"?"}, Desc: "显示此帮助"},
					{Commands: []string{"$"}, Args: "前缀", Desc: "自然语言触发（如 $上下文、$休眠）"},
				},
			},
		}
		helpText = formatHelpSections(sections)
	})
	return helpText
}

func formatHelpSections(sections []helpSection) string {
	var b strings.Builder
	b.WriteString("📖 *命令帮助*\n\n")
	for _, s := range sections {
		fmt.Fprintf(&b, "*%s %s*\n", s.Emoji, s.Title)
		for _, e := range s.Entries {
			b.WriteString("• ")
			b.WriteString(formatCommands(e))
			if e.Desc != "" {
				b.WriteString(" — ")
				b.WriteString(e.Desc)
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func formatCommands(e helpEntry) string {
	parts := make([]string, len(e.Commands))
	for i, c := range e.Commands {
		parts[i] = "`" + c + "`"
	}
	s := strings.Join(parts, " ")
	if e.Args != "" {
		s += " `" + e.Args + "`"
	}
	return s
}

// trimTrailingPunct strips trailing punctuation (same character set as slack/abort.go).
func trimTrailingPunct(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool {
		switch r {
		case '.', '!', '?', ',', ';', ':', '"', '\'', ')', ']',
			'…', '，', '。', '；', '：', '！', '？', '、':
			return true
		}
		return false
	})
}
