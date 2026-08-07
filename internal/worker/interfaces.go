package worker

import "context"

// SkillInvocationMode identifies the wire-level mechanism a Worker uses for
// an explicitly resolved Skill invocation.
type SkillInvocationMode string

const (
	SkillModeTextCommand       SkillInvocationMode = "text_command"
	SkillModeRPCCommand        SkillInvocationMode = "rpc_command"
	SkillModeStructuredSkill   SkillInvocationMode = "structured_skill"
	SkillModeAdvertisedCommand SkillInvocationMode = "advertised_command"
)

// SkillInvocation is a canonical Skill name plus user arguments. Path is
// optional for Workers whose native protocol resolves Skills by name.
type SkillInvocation struct {
	Name string
	Args string
	Path string
	Mode SkillInvocationMode
}

// InputReplay is the durable shape of the most recent primary input used by
// crash recovery. Skill carries the native command invocation when the Worker
// did not receive the input through the ordinary text path.
type InputReplay struct {
	Content string
	Skill   *NativeCommandInvocation
}

// InputReplayRecoverer is optional. It extends InputRecoverer for Workers
// whose native Skill protocol cannot be reconstructed from plain text.
type InputReplayRecoverer interface {
	LastInputReplay() InputReplay
}

// SkillInvoker is optional: Workers that do not implement a native or
// explicitly advertised Skill path must not receive the invocation as an
// ordinary prompt.
type SkillInvoker interface {
	InvokeSkill(ctx context.Context, invocation SkillInvocation) error
}

// SkillCatalogProvider is optional and lets a Worker expose its authoritative
// Skill catalog. The Gateway may use it to reject a filesystem Skill that the
// Worker cannot actually resolve.
type SkillCatalogProvider interface {
	ListInvokableSkills(ctx context.Context, workDir string) ([]SkillDescriptor, error)
}

// SkillDescriptor identifies a Worker-resolvable Skill without exposing its
// contents on the wire.
type SkillDescriptor struct {
	Name        string
	Description string
	Path        string
}

// ControlRequester is implemented by workers that support structured control queries.
type ControlRequester interface {
	SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error)
}

// WorkerCommander is implemented by workers that support worker-level commands
// beyond the basic Input() passthrough.
type WorkerCommander interface {
	Compact(ctx context.Context, args map[string]any) error
	Clear(ctx context.Context) error
	Rewind(ctx context.Context, targetID string) error
}
