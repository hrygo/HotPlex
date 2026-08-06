package worker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

// ─── fakes ──────────────────────────────────────────────────────────────────

// fakeWorkerBase implements worker.Worker with no optional capabilities.
type fakeWorkerBase struct {
	workerType worker.WorkerType
}

func (f *fakeWorkerBase) Type() worker.WorkerType                             { return f.workerType }
func (f *fakeWorkerBase) SupportsResume() bool                                { return true }
func (f *fakeWorkerBase) CanResumeTerminated() bool                           { return true }
func (f *fakeWorkerBase) SupportsStreaming() bool                             { return true }
func (f *fakeWorkerBase) SupportsTools() bool                                 { return true }
func (f *fakeWorkerBase) EnvBlocklist() []string                              { return nil }
func (f *fakeWorkerBase) SessionStoreDir() string                             { return "" }
func (f *fakeWorkerBase) MaxTurns() int                                       { return 0 }
func (f *fakeWorkerBase) Modalities() []string                                { return []string{"text"} }
func (f *fakeWorkerBase) Start(context.Context, worker.SessionInfo) error     { return nil }
func (f *fakeWorkerBase) Input(context.Context, string, map[string]any) error { return nil }
func (f *fakeWorkerBase) Resume(context.Context, worker.SessionInfo) error    { return nil }
func (f *fakeWorkerBase) Terminate(context.Context) error                     { return nil }
func (f *fakeWorkerBase) Kill() error                                         { return nil }
func (f *fakeWorkerBase) Wait() (int, error)                                  { return 0, nil }
func (f *fakeWorkerBase) Conn() worker.SessionConn                            { return nil }
func (f *fakeWorkerBase) Health() worker.WorkerHealth                         { return worker.WorkerHealth{} }
func (f *fakeWorkerBase) LastIO() time.Time                                   { return time.Time{} }
func (f *fakeWorkerBase) ResetContext(context.Context) (worker.ResetResult, error) {
	return worker.ResetResult{}, nil
}
func (f *fakeWorkerBase) StopCurrentTurn(context.Context) error { return nil }
func (f *fakeWorkerBase) IsStopped() bool                       { return false }

// legacyCatalogWorker implements ONLY worker.SkillCatalogProvider.
type legacyCatalogWorker struct {
	*fakeWorkerBase
	descriptors []worker.SkillDescriptor
	calls       int
}

func (w *legacyCatalogWorker) ListInvokableSkills(context.Context, string) ([]worker.SkillDescriptor, error) {
	w.calls++
	return w.descriptors, nil
}

// dualCatalogWorker implements BOTH the new and legacy catalog interfaces.
type dualCatalogWorker struct {
	*fakeWorkerBase
	nativeDescriptors []worker.NativeCommandDescriptor
	legacyDescriptors []worker.SkillDescriptor
	nativeCalls       int
	legacyCalls       int
}

func (w *dualCatalogWorker) ListNativeCommands(context.Context, string) ([]worker.NativeCommandDescriptor, error) {
	w.nativeCalls++
	return w.nativeDescriptors, nil
}

func (w *dualCatalogWorker) ListInvokableSkills(context.Context, string) ([]worker.SkillDescriptor, error) {
	w.legacyCalls++
	return w.legacyDescriptors, nil
}

// legacyInvokerWorker implements ONLY worker.SkillInvoker.
type legacyInvokerWorker struct {
	*fakeWorkerBase
	invoked worker.SkillInvocation
	calls   int
}

func (w *legacyInvokerWorker) InvokeSkill(_ context.Context, invocation worker.SkillInvocation) error {
	w.calls++
	w.invoked = invocation
	return nil
}

// dualInvokerWorker implements BOTH the new and legacy invoker interfaces.
type dualInvokerWorker struct {
	*fakeWorkerBase
	nativeInvoked worker.NativeCommandInvocation
	legacyInvoked worker.SkillInvocation
	nativeCalls   int
	legacyCalls   int
}

func (w *dualInvokerWorker) InvokeNativeCommand(_ context.Context, invocation worker.NativeCommandInvocation) error {
	w.nativeCalls++
	w.nativeInvoked = invocation
	return nil
}

func (w *dualInvokerWorker) InvokeSkill(_ context.Context, invocation worker.SkillInvocation) error {
	w.legacyCalls++
	w.legacyInvoked = invocation
	return nil
}

// ─── AsNativeCatalogProvider ────────────────────────────────────────────────

func TestAsNativeCatalogProviderWrapsLegacySkillCatalog(t *testing.T) {
	t.Parallel()

	w := &legacyCatalogWorker{
		fakeWorkerBase: &fakeWorkerBase{workerType: worker.TypeClaudeCode},
		descriptors: []worker.SkillDescriptor{
			{Name: "oracle-dba", Description: "DBA helper", Path: "/workspace/.agents/skills/oracle-dba/SKILL.md"},
		},
	}

	provider, ok := worker.AsNativeCatalogProvider(w)
	require.True(t, ok)
	require.NotNil(t, provider)

	descriptors, err := provider.ListNativeCommands(context.Background(), "/workspace")
	require.NoError(t, err)
	require.Len(t, descriptors, 1)
	require.Equal(t, worker.NativeCommandDescriptor{
		Name:        "oracle-dba",
		Description: "DBA helper",
		Kind:        worker.NativeCommandKindSkill,
		Mode:        worker.SkillModeTextCommand,
		StartsTurn:  true,
		AcceptsArgs: true,
		Path:        "/workspace/.agents/skills/oracle-dba/SKILL.md",
	}, descriptors[0])
}

func TestAsNativeCatalogProviderModeFollowsWorkerType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		workerType worker.WorkerType
		wantMode   worker.SkillInvocationMode
	}{
		{name: "claude code", workerType: worker.TypeClaudeCode, wantMode: worker.SkillModeTextCommand},
		{name: "opencode server", workerType: worker.TypeOpenCodeSrv, wantMode: worker.SkillModeRPCCommand},
		{name: "codex cli", workerType: worker.TypeCodexCLI, wantMode: worker.SkillModeStructuredSkill},
		{name: "acp", workerType: worker.TypeACP, wantMode: worker.SkillModeAdvertisedCommand},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := &legacyCatalogWorker{
				fakeWorkerBase: &fakeWorkerBase{workerType: tt.workerType},
				descriptors:    []worker.SkillDescriptor{{Name: "oracle-dba"}},
			}
			provider, ok := worker.AsNativeCatalogProvider(w)
			require.True(t, ok)
			descriptors, err := provider.ListNativeCommands(context.Background(), "/workspace")
			require.NoError(t, err)
			require.Equal(t, tt.wantMode, descriptors[0].Mode)
		})
	}
}

func TestAsNativeCatalogProviderLegacyErrorPropagates(t *testing.T) {
	t.Parallel()

	w := &legacyCatalogWorker{fakeWorkerBase: &fakeWorkerBase{workerType: worker.TypeCodexCLI}}
	w.descriptors = nil
	// Force the catalog error through the fake.
	provider, ok := worker.AsNativeCatalogProvider(w)
	require.True(t, ok)
	// The wrapped provider surfaces the legacy error unchanged.
	_ = provider
	// A separate error fake keeps the single-path guarantee honest.
	errWorker := &errCatalogWorker{fakeWorkerBase: &fakeWorkerBase{workerType: worker.TypeCodexCLI}}
	ep, ok := worker.AsNativeCatalogProvider(errWorker)
	require.True(t, ok)
	_, err := ep.ListNativeCommands(context.Background(), "/workspace")
	require.ErrorIs(t, err, errCatalogBoom)
}

type errCatalogWorker struct {
	*fakeWorkerBase
}

var errCatalogBoom = errors.New("catalog unavailable")

func (w *errCatalogWorker) ListInvokableSkills(context.Context, string) ([]worker.SkillDescriptor, error) {
	return nil, errCatalogBoom
}

func TestAsNativeCatalogProviderNativeWinsSinglePath(t *testing.T) {
	t.Parallel()

	w := &dualCatalogWorker{
		fakeWorkerBase:    &fakeWorkerBase{workerType: worker.TypeOpenCodeSrv},
		nativeDescriptors: []worker.NativeCommandDescriptor{{Name: "native-cmd", Kind: worker.NativeCommandKindControl}},
		legacyDescriptors: []worker.SkillDescriptor{{Name: "legacy-skill"}},
	}

	provider, ok := worker.AsNativeCatalogProvider(w)
	require.True(t, ok)
	descriptors, err := provider.ListNativeCommands(context.Background(), "/workspace")
	require.NoError(t, err)
	require.Len(t, descriptors, 1)
	require.Equal(t, "native-cmd", descriptors[0].Name)
	require.Equal(t, 1, w.nativeCalls, "native catalog must be the only source consulted")
	require.Zero(t, w.legacyCalls, "legacy catalog must never be consulted when native exists")
}

func TestAsNativeCatalogProviderPlainWorkerNil(t *testing.T) {
	t.Parallel()

	w := &fakeWorkerBase{workerType: worker.TypeClaudeCode}
	provider, ok := worker.AsNativeCatalogProvider(w)
	require.False(t, ok)
	require.Nil(t, provider)
}

// ─── AsNativeInvoker ────────────────────────────────────────────────────────

func TestAsNativeInvokerWrapsLegacySkillInvoker(t *testing.T) {
	t.Parallel()

	w := &legacyInvokerWorker{fakeWorkerBase: &fakeWorkerBase{workerType: worker.TypeCodexCLI}}

	invoker, ok := worker.AsNativeInvoker(w)
	require.True(t, ok)
	require.NotNil(t, invoker)

	invocation := worker.NativeCommandInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
		Mode: worker.SkillModeStructuredSkill,
	}
	err := invoker.InvokeNativeCommand(context.Background(), invocation)
	require.NoError(t, err)
	require.Equal(t, 1, w.calls)
	require.Equal(t, worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/.agents/skills/oracle-dba/SKILL.md",
		Mode: worker.SkillModeStructuredSkill,
	}, w.invoked)
}

func TestAsNativeInvokerNativeWinsSinglePath(t *testing.T) {
	t.Parallel()

	w := &dualInvokerWorker{fakeWorkerBase: &fakeWorkerBase{workerType: worker.TypeACP}}

	invoker, ok := worker.AsNativeInvoker(w)
	require.True(t, ok)
	invocation := worker.NativeCommandInvocation{Name: "context", Args: "on"}
	err := invoker.InvokeNativeCommand(context.Background(), invocation)
	require.NoError(t, err)
	require.Equal(t, 1, w.nativeCalls, "native invoker must be the only path consulted")
	require.Zero(t, w.legacyCalls, "legacy SkillInvoker must never be consulted when native exists")
	require.Equal(t, invocation, w.nativeInvoked)
}

func TestAsNativeInvokerPlainWorkerNil(t *testing.T) {
	t.Parallel()

	w := &fakeWorkerBase{workerType: worker.TypeOpenCodeSrv}
	invoker, ok := worker.AsNativeInvoker(w)
	require.False(t, ok)
	require.Nil(t, invoker)
}

// ─── NativeModeForType ──────────────────────────────────────────────────────

func TestNativeModeForType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		workerType worker.WorkerType
		want       worker.SkillInvocationMode
	}{
		{name: "claude code", workerType: worker.TypeClaudeCode, want: worker.SkillModeTextCommand},
		{name: "opencode server", workerType: worker.TypeOpenCodeSrv, want: worker.SkillModeRPCCommand},
		{name: "codex cli", workerType: worker.TypeCodexCLI, want: worker.SkillModeStructuredSkill},
		{name: "acp", workerType: worker.TypeACP, want: worker.SkillModeAdvertisedCommand},
		{name: "unknown", workerType: worker.TypeUnknown, want: ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, worker.NativeModeForType(tt.workerType))
		})
	}
}

// ─── NativeInvocationFromSkill ──────────────────────────────────────────────

func TestNativeInvocationFromSkill(t *testing.T) {
	t.Parallel()

	inv := worker.NativeInvocationFromSkill(worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/SKILL.md",
		Mode: worker.SkillModeTextCommand,
	})
	require.Equal(t, worker.NativeCommandInvocation{
		Name: "oracle-dba",
		Args: "10.102.78.1",
		Path: "/workspace/SKILL.md",
		Mode: worker.SkillModeTextCommand,
	}, inv)
}
