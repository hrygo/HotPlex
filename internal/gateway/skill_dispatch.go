package gateway

import (
	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// metadataSkillInvocationKey carries the Skill invocation resolved at buffer
// time through the pending-buffer replay path (cloneForReplay preserves
// envelope Metadata). It lets DeliverReplay keep native Skill semantics even
// when the filesystem catalog no longer matches (Skill removed/renamed while
// the input was buffered).
const metadataSkillInvocationKey = "skill_invocation"

// stashInvocation records a resolved invocation on the envelope so a buffered
// replay can fall back to it. The original content is recorded too: the stash
// is only honored when the replayed content is unchanged, so a merged
// multi-entry replay (numbered list) never replays only the Skill half.
func stashInvocation(env *events.Envelope, invocation worker.NativeCommandInvocation, content string) {
	if env == nil || invocation.Name == "" {
		return
	}
	if env.Metadata == nil {
		env.Metadata = make(map[string]any)
	}
	env.Metadata[metadataSkillInvocationKey] = map[string]any{
		"name":    invocation.Name,
		"args":    invocation.Args,
		"path":    invocation.Path,
		"mode":    string(invocation.Mode),
		"content": content,
	}
}

// invocationFromMetadata recovers a stashed invocation. ok is false when no
// valid stash exists or the replayed content diverged from the buffered one.
func invocationFromMetadata(md map[string]any, content string) (worker.NativeCommandInvocation, bool) {
	raw, _ := md[metadataSkillInvocationKey].(map[string]any)
	if raw == nil {
		return worker.NativeCommandInvocation{}, false
	}
	name, _ := raw["name"].(string)
	if name == "" {
		return worker.NativeCommandInvocation{}, false
	}
	if original, _ := raw["content"].(string); original != content {
		return worker.NativeCommandInvocation{}, false
	}
	args, _ := raw["args"].(string)
	path, _ := raw["path"].(string)
	mode, _ := raw["mode"].(string)
	return worker.NativeCommandInvocation{
		Name: name,
		Args: args,
		Path: path,
		Mode: worker.SkillInvocationMode(mode),
	}, true
}

func resolveSkillInvocation(content string, catalog []skills.Skill) (worker.NativeCommandInvocation, bool, error) {
	parsed, matched, err := skills.ParseInvocation(content, catalog)
	if err != nil || !matched {
		return worker.NativeCommandInvocation{}, matched, err
	}
	for _, skill := range catalog {
		if skill.Name == parsed.Name {
			return worker.NativeCommandInvocation{
				Name: parsed.Name,
				Args: parsed.Args,
				Path: skill.FilePath,
			}, true, nil
		}
	}
	return worker.NativeCommandInvocation{}, false, nil
}
