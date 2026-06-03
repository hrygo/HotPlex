package messaging

import (
	"strings"
	"sync"
)

// Source: OpenClaw abort-detect.ts (core triggers, covering English/Chinese/Japanese/Russian).
var abortTriggers = map[string]bool{
	"stop": true, "abort": true, "halt": true, "cancel": true,
	"wait": true, "exit": true, "interrupt": true,
	"please stop": true, "stop please": true,
	"停止": true, "取消": true, "中断": true, "等一下": true,
	"别说了": true, "停下来": true,
	"やめて": true, "止めて": true,
	"стоп": true,
}

var abortTriggersMu sync.RWMutex

func RegisterAbortTrigger(word string) {
	abortTriggersMu.Lock()
	abortTriggers[word] = true
	abortTriggersMu.Unlock()
}

// Normalization: trim → lowercase → strip trailing punctuation.
func IsAbortCommand(text string) bool {
	t := strings.TrimSpace(strings.ToLower(text))
	t = trimTrailingPunct(t)
	abortTriggersMu.RLock()
	ok := abortTriggers[t]
	abortTriggersMu.RUnlock()
	return ok
}

type CommandAction int

const (
	CmdNone CommandAction = iota
	CmdAbort
	CmdHelp
	CmdControl
	CmdWorker
	CmdPassthrough
	CmdGroupDiscuss
	CmdGroupStop
)

type CommandResult struct {
	Action       CommandAction
	Control      *ControlCommandResult
	Worker       *WorkerCommandResult
	GroupDiscuss *GroupDiscussCommand
}

// GroupDiscussCommand holds the parsed /discuss command result.
type GroupDiscussCommand struct {
	BotNames []string
	Topic    string
}

func DetectCommand(text string) CommandResult {
	if IsAbortCommand(text) {
		return CommandResult{Action: CmdAbort}
	}
	if IsHelpCommand(text) {
		return CommandResult{Action: CmdHelp}
	}
	if ctrl := ParseControlCommand(text); ctrl != nil {
		return CommandResult{Action: CmdControl, Control: ctrl}
	}
	if wc := ParseWorkerCommand(text); wc != nil {
		if wc.Command.IsPassthrough() {
			return CommandResult{Action: CmdPassthrough, Worker: wc}
		}
		return CommandResult{Action: CmdWorker, Worker: wc}
	}

	// Group chat commands: /discuss @bot1 @bot2 <topic> and /stop-collab.
	t := strings.TrimSpace(text)
	if strings.HasPrefix(strings.ToLower(t), "/discuss ") || strings.HasPrefix(t, "$讨论 ") {
		if gd := parseGroupDiscuss(t); gd != nil {
			return CommandResult{Action: CmdGroupDiscuss, GroupDiscuss: gd}
		}
	}
	if isGroupStopCollab(t) {
		return CommandResult{Action: CmdGroupStop}
	}

	return CommandResult{Action: CmdNone}
}

// parseGroupDiscuss extracts @mentions and topic from /discuss command text.
func parseGroupDiscuss(text string) *GroupDiscussCommand {
	rest := ""
	if strings.HasPrefix(strings.ToLower(text), "/discuss ") {
		rest = text[len("/discuss "):]
	} else if strings.HasPrefix(text, "$讨论 ") {
		rest = text[len("$讨论 "):]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil
	}

	var mentions []string
	fields := strings.Fields(rest)
	topicStart := -1
	for i, word := range fields {
		if strings.HasPrefix(word, "@") && len(word) > 1 {
			mentions = append(mentions, strings.TrimPrefix(word, "@"))
		} else {
			topicStart = i
			break
		}
	}
	if len(mentions) < 2 {
		return nil
	}
	topic := rest
	if topicStart >= 0 {
		topic = strings.Join(fields[topicStart:], " ")
	}
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return nil
	}
	return &GroupDiscussCommand{BotNames: mentions, Topic: topic}
}

// isGroupStopCollab checks for /stop-collab command variants.
func isGroupStopCollab(text string) bool {
	tl := strings.ToLower(strings.TrimSpace(text))
	tl = trimTrailingPunct(tl)
	return tl == "/stop-collab" || tl == "$停止讨论" || tl == "$stop-collab"
}
