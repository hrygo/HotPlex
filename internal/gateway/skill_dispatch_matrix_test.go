package gateway

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/e2econtract"
	"github.com/hrygo/hotplex/internal/execution"
	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestSkillResolutionCoversThreeChannelsFourWorkers(t *testing.T) {
	t.Parallel()

	catalog := []skills.Skill{{Name: "oracle-dba", FilePath: "/workspace/.agents/skills/oracle-dba/SKILL.md"}}
	seen := make(map[string]struct{}, len(e2econtract.Combinations()))
	for _, combination := range e2econtract.Combinations() {
		combination := combination
		t.Run(combination.ID, func(t *testing.T) {
			t.Parallel()
			invocation, matched, err := resolveSkillInvocation("/oracle-dba 10.102.78.1", catalog)
			require.NoError(t, err)
			require.True(t, matched)
			require.Equal(t, "oracle-dba", invocation.Name)
			require.Equal(t, "10.102.78.1", invocation.Args)
			require.Equal(t, "/workspace/.agents/skills/oracle-dba/SKILL.md", invocation.Path)
			require.NotEmpty(t, combination.Platform)
			require.NotEmpty(t, combination.Worker)
		})
		seen[combination.ID] = struct{}{}
	}
	require.Len(t, seen, 12)
	require.Len(t, e2econtract.Combinations(), 12)

	for _, workerType := range []worker.WorkerType{
		worker.TypeClaudeCode,
		worker.TypeOpenCodeSrv,
		worker.TypeCodexCLI,
		worker.TypeACP,
	} {
		require.NotEmpty(t, worker.NativeModeForType(workerType))
	}
}

// matrixNativeWorker is a per-combo worker double whose Type() follows the
// matrix combination, advertising a native catalog and recording the resolved
// invocation through the shared dispatch path (worker.AsNativeCatalogProvider /
// AsNativeInvoker). It is deliberately NOT the contracttest WorkerProbe: the
// native-command matrix must route through the per-protocol invoker surface.
type matrixNativeWorker struct {
	mockWorkerForHandler
	workerType  worker.WorkerType
	descriptors []worker.NativeCommandDescriptor
	invocation  worker.NativeCommandInvocation
}

func (w *matrixNativeWorker) Type() worker.WorkerType { return w.workerType }

func (w *matrixNativeWorker) ListNativeCommands(context.Context, string) ([]worker.NativeCommandDescriptor, error) {
	return w.descriptors, nil
}

func (w *matrixNativeWorker) InvokeNativeCommand(_ context.Context, invocation worker.NativeCommandInvocation) error {
	w.invocation = invocation
	return nil
}

// TestNativeDispatchCoversThreeChannelsFourWorkers is the T13 native-dispatch
// extension of the platform×worker matrix: every combination must route an
// explicit "/worker <name>" and the short "/<name>" entry through the shared
// dispatch path into the worker's native invoker with the protocol-derived
// invocation mode, and reject unknown names with NOT_SUPPORTED.
func TestNativeDispatchCoversThreeChannelsFourWorkers(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, len(e2econtract.Combinations()))
	for _, combination := range e2econtract.Combinations() {
		combination := combination
		t.Run(combination.ID, func(t *testing.T) {
			t.Parallel()
			runMatrixNativeDispatch(t, combination)
		})
		seen[combination.ID] = struct{}{}
	}
	require.Len(t, seen, 12)
	require.Len(t, e2econtract.Combinations(), 12)
}

func runMatrixNativeDispatch(t *testing.T, combo e2econtract.Combination) {
	t.Helper()

	// Deliberately equal the filesystem locator path: provenance, not Path
	// shape, must keep an authoritative Worker descriptor callable.
	const skillPath = contractFSPath
	w := &matrixNativeWorker{
		workerType: combo.Worker,
		descriptors: []worker.NativeCommandDescriptor{{
			Name:        "oracle-dba",
			Description: "DBA helper",
			Kind:        worker.NativeCommandKindSkill,
			Mode:        worker.NativeModeForType(combo.Worker),
			StartsTurn:  true,
			AcceptsArgs: true,
			Path:        skillPath,
		}},
	}

	sm := new(mockInputSM)
	sm.On("Get", "s1").Return(sessionWithWorkDir("s1"), nil).Maybe()
	sm.On("GetWorker", "s1").Return(w).Maybe()

	hub := newTestHub(t)
	conn := &mockPlatformConn{}
	hub.JoinPlatformSession("s1", conn)

	h := &Handler{
		log:            testLogger(t),
		hub:            hub,
		sm:             sm,
		catalogStore:   newSessionCatalogStore(slog.Default(), fixedSkillsLocator{items: contractFSSkills()}),
		skillsLocator:  fixedSkillsLocator{items: contractFSSkills()},
		executionStore: &fakeExecutionStore{record: testExecutionRecord(execution.StatusAccepted)},
	}

	// Parser + invoke routing through the shared dispatch path: the explicit
	// "/worker oracle-dba 10.0.0.1" entry must resolve against the merged
	// catalog and land on the worker's native invoker.
	require.NoError(t, h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", contractSkillInput, nil)))
	require.Equal(t, "oracle-dba", w.invocation.Name, "explicit /worker name must reach the native invoker")
	require.Equal(t, contractSkillArgs, w.invocation.Args)
	require.Equal(t, skillPath, w.invocation.Path, "authoritative catalog path must win")
	require.Equal(t, worker.NativeModeForType(combo.Worker), w.invocation.Mode,
		"invocation mode must be derived from the combination's worker type")

	// The short "/oracle-dba 10.0.0.1" entry routes through the same path.
	require.NoError(t, h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", contractSlashInput, nil)))
	require.Equal(t, worker.NativeModeForType(combo.Worker), w.invocation.Mode,
		"short form must preserve the protocol invocation mode")

	// Unknown names resolve to NOT_SUPPORTED and never reach the invoker.
	err := h.handleInput(t.Context(), inputEnvelopeWithMetadata("s1", "/worker unknown-thing", nil))
	require.Error(t, err)
	require.ErrorContains(t, err, string(events.ErrCodeNotSupported))
}
