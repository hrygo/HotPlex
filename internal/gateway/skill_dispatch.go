package gateway

import (
	"fmt"
	"strings"

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

// nativeSkillCatalog adapts the merged session catalog to the existing
// compact/canonical slash parser. The descriptor slice is the source of
// truth; filesystem skills are already represented in it by
// sessionCatalogStore.assemble, so parsing cannot accidentally promote a
// filesystem-only entry into a callable invocation.
func nativeSkillCatalog(descriptors []worker.NativeCommandDescriptor) []skills.Skill {
	catalog := make([]skills.Skill, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.Kind != worker.NativeCommandKindSkill || descriptor.Name == "" {
			continue
		}
		catalog = append(catalog, skills.Skill{
			Name:        descriptor.Name,
			Description: descriptor.Description,
			FilePath:    descriptor.Path,
		})
	}
	return catalog
}

// resolveNativeSkillInvocation parses a slash input against the merged
// session catalog and returns the exact descriptor selected by the parser.
// The caller must still run classifyNativeSkillCallability before dispatch;
// this function only resolves syntax and name precedence.
func resolveNativeSkillInvocation(content string, descriptors []worker.NativeCommandDescriptor) (worker.NativeCommandInvocation, bool, error) {
	parsed, matched, err := skills.ParseInvocation(content, nativeSkillCatalog(descriptors))
	if err != nil || !matched {
		return worker.NativeCommandInvocation{}, matched, err
	}
	for _, descriptor := range descriptors {
		if descriptor.Kind == worker.NativeCommandKindSkill && descriptor.Name == parsed.Name {
			return worker.NativeCommandInvocation{
				Name: parsed.Name,
				Args: parsed.Args,
				Path: descriptor.Path,
				Mode: descriptor.Mode,
			}, true, nil
		}
	}
	return worker.NativeCommandInvocation{}, false, nil
}

// nativeSkillNotSupportedError is intentionally bounded: callers expose the
// worker-independent sentinel as NOT_SUPPORTED while retaining only the
// selected Skill name for remediation. Paths and catalog payloads never enter
// the user-facing error.
func nativeSkillNotSupportedError(name string) error {
	return fmt.Errorf("%w: Skill %q is discoverable but not callable by the current Worker", worker.ErrSkillNotSupported, strings.TrimSpace(name))
}

// revalidatedNativeInvocation checks a stashed/structured invocation against
// the current merged session catalog. Invocation metadata is only a
// correlation hint: the current descriptor supplies the authoritative path
// and mode, and filesystem-only entries remain discoverable-only. Both Skill
// entries and Worker-advertised starts-turn commands can be stashed by the
// explicit /worker path; Gateway fixed commands are never replayable here.
func revalidatedNativeInvocation(
	invocation worker.NativeCommandInvocation,
	descriptors []worker.NativeCommandDescriptor,
	fsSkills []skills.Skill,
	w worker.Worker,
	authoritativeOK bool,
) (worker.NativeCommandInvocation, error) {
	var descriptor *worker.NativeCommandDescriptor
	for i := range descriptors {
		if descriptors[i].Name == invocation.Name {
			descriptor = &descriptors[i]
			break
		}
	}
	if descriptor == nil {
		return worker.NativeCommandInvocation{}, nativeSkillNotSupportedError(invocation.Name)
	}

	if _, fixed := fixedCommandNamesFor(w)[descriptor.Name]; fixed {
		return worker.NativeCommandInvocation{}, nativeSkillNotSupportedError(descriptor.Name)
	}
	if descriptor.Kind == worker.NativeCommandKindSkill {
		fsByName := make(map[string]skills.Skill, len(fsSkills))
		for _, skill := range fsSkills {
			fsByName[skill.Name] = skill
		}
		fs, hasFS := fsByName[descriptor.Name]
		status := classifyNativeSkillCallability(*descriptor, fs, hasFS, w, authoritativeOK)
		if status != events.SkillStatusCallable {
			return worker.NativeCommandInvocation{}, nativeSkillNotSupportedError(descriptor.Name)
		}
	} else if descriptor.Kind != worker.NativeCommandKindControl || !authoritativeOK {
		return worker.NativeCommandInvocation{}, nativeSkillNotSupportedError(descriptor.Name)
	}

	resolved := invocation
	resolved.Name = descriptor.Name
	resolved.Path = descriptor.Path
	resolved.Mode = descriptor.Mode
	return resolved, nil
}
