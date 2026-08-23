package worker

import "context"

// NativeCommandKind classifies a native Worker command. A skill starts a
// durable turn through the ordinary input pipeline; a control command is a
// bounded Worker-side control request that does not create an execution turn.
type NativeCommandKind string

const (
	// NativeCommandKindSkill identifies a Worker-resolvable skill. These are
	// dispatched through the durable accept → ACK → delivery path and may be
	// buffered/replayed on busy sessions.
	NativeCommandKindSkill NativeCommandKind = "skill"

	// NativeCommandKindControl identifies a Worker control command that does
	// not start a turn (bounded control request; no execution record).
	NativeCommandKindControl NativeCommandKind = "control"
)

// CatalogOrigin identifies which trusted catalog tier contributed a merged
// descriptor. It is an internal Gateway evidence marker, not a wire field.
// The zero value is intentionally unknown and must fail closed.
type CatalogOrigin string

const (
	CatalogOriginUnknown    CatalogOrigin = ""
	CatalogOriginGateway    CatalogOrigin = "gateway"
	CatalogOriginWorker     CatalogOrigin = "worker"
	CatalogOriginFilesystem CatalogOrigin = "filesystem"
)

// NativeCommandDescriptor is the unified Worker-side description of a native
// command. It is the canonical catalog entry consumed by the Gateway router
// and the platform command menus; it never exposes Worker internals on the
// wire. Path is trusted only when it comes from the Worker's authoritative
// catalog or a HotPlex-resolved Skill root — never from client input.
type NativeCommandDescriptor struct {
	Name        string
	Description string
	Kind        NativeCommandKind
	Mode        SkillInvocationMode
	StartsTurn  bool
	AcceptsArgs bool
	Path        string
	// CatalogOrigin is stamped by the Gateway while merging tiers. Provider
	// values are never trusted as evidence; the field is deliberately omitted
	// from all external event/AEP models.
	CatalogOrigin CatalogOrigin `json:"-"`
}

// NativeCommandInvocation is the canonical invocation for a resolved native
// command. It carries the same field shape as the legacy SkillInvocation so
// adapters and the Gateway can convert between them without semantic loss.
type NativeCommandInvocation struct {
	Name string
	Args string
	Path string
	Mode SkillInvocationMode
}

// NativeCommandCatalogProvider is the unified catalog surface for a Worker.
// The Gateway queries it to validate that a requested command is actually
// resolvable by the Worker before dispatching. A query failure means "cannot
// confirm invokability", never a green light.
type NativeCommandCatalogProvider interface {
	ListNativeCommands(ctx context.Context, workDir string) ([]NativeCommandDescriptor, error)
}

// NativeCommandInvoker is the unified dispatch surface for a Worker. Workers
// that cannot execute a native command must not receive the invocation as an
// ordinary prompt.
type NativeCommandInvoker interface {
	InvokeNativeCommand(ctx context.Context, invocation NativeCommandInvocation) error
}

// NativeModeForType maps a WorkerType to the native SkillInvocationMode it
// uses for an explicitly resolved command.
func NativeModeForType(t WorkerType) SkillInvocationMode {
	switch t {
	case TypeClaudeCode:
		return SkillModeTextCommand
	case TypeOpenCodeSrv:
		return SkillModeRPCCommand
	case TypeCodexCLI:
		return SkillModeStructuredSkill
	case TypeACP:
		return SkillModeAdvertisedCommand
	default:
		return ""
	}
}

// NativeInvocationFromSkill converts a legacy SkillInvocation to the canonical
// NativeCommandInvocation shape used for crash-replay persistence.
func NativeInvocationFromSkill(inv SkillInvocation) NativeCommandInvocation {
	return NativeCommandInvocation(inv)
}

// AsNativeCatalogProvider returns the Worker's unified catalog capability.
//
// A Worker implementing NativeCommandCatalogProvider is authoritative. A
// Worker implementing only the legacy SkillCatalogProvider is wrapped into a
// NativeCommand catalog whose entries are all skill-kind, StartsTurn,
// args-accepting commands resolved through NativeModeForType. A plain Worker
// returns (nil, false). The single-path guarantee holds: Gateway code must
// never consult the underlying legacy interface directly once the worker is
// wrapped here.
func AsNativeCatalogProvider(w Worker) (NativeCommandCatalogProvider, bool) {
	if native, ok := w.(NativeCommandCatalogProvider); ok {
		return native, true
	}
	legacy, ok := w.(SkillCatalogProvider)
	if !ok {
		return nil, false
	}
	return nativeCatalogCompat{worker: w, legacy: legacy}, true
}

// nativeCatalogCompat adapts a legacy SkillCatalogProvider to the unified
// NativeCommandCatalogProvider surface.
type nativeCatalogCompat struct {
	worker Worker
	legacy SkillCatalogProvider
}

func (c nativeCatalogCompat) ListNativeCommands(ctx context.Context, workDir string) ([]NativeCommandDescriptor, error) {
	descriptors, err := c.legacy.ListInvokableSkills(ctx, workDir)
	if err != nil {
		return nil, err
	}
	mode := NativeModeForType(c.worker.Type())
	out := make([]NativeCommandDescriptor, 0, len(descriptors))
	for _, d := range descriptors {
		out = append(out, NativeCommandDescriptor{
			Name:        d.Name,
			Description: d.Description,
			Kind:        NativeCommandKindSkill,
			Mode:        mode,
			StartsTurn:  true,
			AcceptsArgs: true,
			Path:        d.Path,
		})
	}
	return out, nil
}

// AsNativeInvoker returns the Worker's unified invocation surface.
//
// A Worker implementing NativeCommandInvoker is authoritative. A Worker
// implementing only the legacy SkillInvoker is wrapped so a
// NativeCommandInvocation is converted back to the SkillInvocation shape and
// forwarded. A plain Worker returns (nil, false). The single-path guarantee
// holds: Gateway code must never call the legacy InvokeSkill directly once
// the worker is wrapped here.
func AsNativeInvoker(w Worker) (NativeCommandInvoker, bool) {
	if native, ok := w.(NativeCommandInvoker); ok {
		return native, true
	}
	legacy, ok := w.(SkillInvoker)
	if !ok {
		return nil, false
	}
	return nativeInvokerCompat{legacy: legacy}, true
}

// nativeInvokerCompat adapts a legacy SkillInvoker to the unified
// NativeCommandInvoker surface.
type nativeInvokerCompat struct {
	legacy SkillInvoker
}

func (i nativeInvokerCompat) InvokeNativeCommand(ctx context.Context, invocation NativeCommandInvocation) error {
	return i.legacy.InvokeSkill(ctx, SkillInvocation(invocation))
}
