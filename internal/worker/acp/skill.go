package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hrygo/hotplex/internal/worker"
)

func parseAvailableCommands(raw json.RawMessage) []worker.SkillDescriptor {
	var params struct {
		Update struct {
			SessionUpdate     string `json:"sessionUpdate"`
			AvailableCommands []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"availableCommands"`
		} `json:"update"`
	}
	if err := json.Unmarshal(raw, &params); err != nil || params.Update.SessionUpdate != "available_commands_update" {
		return nil
	}
	commands := make([]worker.SkillDescriptor, 0, len(params.Update.AvailableCommands))
	for _, command := range params.Update.AvailableCommands {
		name := strings.TrimSpace(command.Name)
		if name == "" {
			continue
		}
		commands = append(commands, worker.SkillDescriptor{Name: name, Description: command.Description})
	}
	return commands
}

func (w *Worker) updateAvailableCommands(raw json.RawMessage) {
	var marker struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
		} `json:"update"`
	}
	if err := json.Unmarshal(raw, &marker); err != nil || marker.Update.SessionUpdate != "available_commands_update" {
		return
	}
	commands := parseAvailableCommands(raw)
	w.skillMu.Lock()
	defer w.skillMu.Unlock()
	if w.availableCommands == nil {
		w.availableCommands = make(map[string]worker.SkillDescriptor)
	}
	clear(w.availableCommands)
	for _, command := range commands {
		w.availableCommands[command.Name] = command
	}
}

// ListInvokableSkills returns the current ACP session command advertisement.
// The workDir argument is part of the common capability interface; ACP v1
// scopes the list to the already-created session, so it is intentionally not
// used here.
func (w *Worker) ListInvokableSkills(_ context.Context, _ string) ([]worker.SkillDescriptor, error) {
	w.skillMu.RLock()
	commands := make([]worker.SkillDescriptor, 0, len(w.availableCommands))
	for _, command := range w.availableCommands {
		commands = append(commands, command)
	}
	w.skillMu.RUnlock()
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return commands, nil
}

// InvokeSkill sends an ACP slash command only when the current Agent has
// advertised that command. Unknown commands deliberately never fall through
// to the LLM as ordinary user text.
func (w *Worker) InvokeSkill(ctx context.Context, invocation worker.SkillInvocation) error {
	return w.invokeSkill(ctx, invocation, nil)
}

func (w *Worker) InvokeNativeCommandWithDispatchAccepted(
	ctx context.Context,
	invocation worker.NativeCommandInvocation,
	accepted func(),
) error {
	return w.invokeSkill(ctx, worker.SkillInvocation(invocation), accepted)
}

func (w *Worker) invokeSkill(ctx context.Context, invocation worker.SkillInvocation, accepted func()) error {
	w.skillMu.RLock()
	_, advertised := w.availableCommands[invocation.Name]
	w.skillMu.RUnlock()
	if !advertised {
		return fmt.Errorf("%w: %s", worker.ErrSkillNotSupported, invocation.Name)
	}
	command := "/" + strings.TrimSpace(invocation.Name)
	if args := strings.TrimSpace(invocation.Args); args != "" {
		command += " " + args
	}
	// Keep explicit invocation payloads intact. ACP's compatibility rules are
	// only sent with ordinary user text because a prefix could prevent an agent
	// from recognizing the advertised slash command.
	return w.input(ctx, command, nil, false, accepted)
}
