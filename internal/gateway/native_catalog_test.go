package gateway

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/skills"
	"github.com/hrygo/hotplex/internal/worker"
)

// nativeCatalogTestWorker is a minimal worker.Worker that also exposes the
// authoritative NativeCommandCatalogProvider surface with scripted behavior.
// It deliberately does NOT implement ControlRequester or WorkerCommander so
// capability-conditioned fixed commands can be tested for absence.
type nativeCatalogTestWorker struct {
	*fakeWorker
	descriptors []worker.NativeCommandDescriptor
	providerErr error
	blockCtx    bool
	calls       atomic.Int32
}

func (w *nativeCatalogTestWorker) ListNativeCommands(ctx context.Context, _ string) ([]worker.NativeCommandDescriptor, error) {
	w.calls.Add(1)
	if w.blockCtx {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if w.providerErr != nil {
		return nil, w.providerErr
	}
	return w.descriptors, nil
}

var _ worker.NativeCommandCatalogProvider = (*nativeCatalogTestWorker)(nil)

// nativeCatalogControlWorker adds the ControlRequester capability surface.
type nativeCatalogControlWorker struct {
	*nativeCatalogTestWorker
}

func (w *nativeCatalogControlWorker) SendControlRequest(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

var _ worker.ControlRequester = (*nativeCatalogControlWorker)(nil)

// nativeCatalogCommanderWorker adds the WorkerCommander capability surface.
type nativeCatalogCommanderWorker struct {
	*nativeCatalogTestWorker
}

func (w *nativeCatalogCommanderWorker) Compact(context.Context, map[string]any) error { return nil }
func (w *nativeCatalogCommanderWorker) Clear(context.Context) error                   { return nil }
func (w *nativeCatalogCommanderWorker) Rewind(context.Context, string) error          { return nil }

var _ worker.WorkerCommander = (*nativeCatalogCommanderWorker)(nil)

// nativeCatalogTestLocator is a scriptable SkillsLocator fake.
type nativeCatalogTestLocator struct {
	mu      sync.Mutex
	skills  []skills.Skill
	listErr error
}

func (l *nativeCatalogTestLocator) List(context.Context, string, string) ([]skills.Skill, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.listErr != nil {
		return nil, l.listErr
	}
	return l.skills, nil
}

func (l *nativeCatalogTestLocator) Close() {}

// newTestCatalogStore builds a store with a short bounded-query timeout so the
// 5s production bound (nativeCatalogQueryTimeout) is exercised in milliseconds.
func newTestCatalogStore(t *testing.T, locator SkillsLocator) *sessionCatalogStore {
	t.Helper()
	s := newSessionCatalogStore(slog.Default(), locator)
	s.queryTimeout = 50 * time.Millisecond
	return s
}

func descriptorsByName(descriptors []worker.NativeCommandDescriptor) map[string]worker.NativeCommandDescriptor {
	byName := make(map[string]worker.NativeCommandDescriptor, len(descriptors))
	for _, d := range descriptors {
		byName[d.Name] = d
	}
	return byName
}

func TestNativeCatalogMergePrecedence(t *testing.T) {
	t.Parallel()

	locator := &nativeCatalogTestLocator{skills: []skills.Skill{
		{Name: "reset", Description: "fs reset", FilePath: "/fs/reset"},
		{Name: "oracle-dba", Description: "fs oracle", FilePath: "/fs/oracle-dba/SKILL.md"},
	}}
	store := newTestCatalogStore(t, locator)

	w := &nativeCatalogTestWorker{
		fakeWorker: &fakeWorker{workerType: worker.TypeCodexCLI},
		descriptors: []worker.NativeCommandDescriptor{
			{Name: "reset", Description: "worker reset", Kind: worker.NativeCommandKindSkill, Mode: worker.SkillModeStructuredSkill, StartsTurn: true, Path: "/worker/reset"},
			{Name: "oracle-dba", Description: "worker oracle", Kind: worker.NativeCommandKindSkill, Mode: worker.SkillModeStructuredSkill, StartsTurn: true, Path: "/worker/oracle-dba"},
		},
	}

	merged, err := store.Lookup(context.Background(), "s1", "/work", w, 1)
	require.NoError(t, err)
	byName := descriptorsByName(merged)

	// Fixed Gateway command beats a same-named worker catalog entry: "reset"
	// stays a control command, never a Worker-resolvable skill.
	reset := byName["reset"]
	require.Equal(t, worker.NativeCommandKindControl, reset.Kind)
	require.False(t, reset.StartsTurn)
	require.Empty(t, reset.Path, "fixed commands carry no worker path")

	// Worker authoritative catalog beats the filesystem scan: "oracle-dba"
	// keeps the Worker's authoritative path, not the filesystem path.
	oracle := byName["oracle-dba"]
	require.Equal(t, "/worker/oracle-dba", oracle.Path)
	require.Equal(t, worker.NativeCommandKindSkill, oracle.Kind)
}

func TestNativeCatalogCapabilityConditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		worker  worker.Worker
		present []string
		absent  []string
	}{
		{
			name:    "plain worker keeps always-on commands only",
			worker:  &nativeCatalogTestWorker{fakeWorker: &fakeWorker{}},
			present: []string{"reset", "stop", "gc", "park", "new", "cd", "skills", "help"},
			absent:  []string{"context", "mcp", "model", "perm", "compact", "clear", "rewind", "effort", "commit"},
		},
		{
			name:    "control requester adds context/mcp/model/perm",
			worker:  &nativeCatalogControlWorker{nativeCatalogTestWorker: &nativeCatalogTestWorker{fakeWorker: &fakeWorker{}}},
			present: []string{"context", "mcp", "model", "perm"},
			absent:  []string{"compact", "clear", "rewind"},
		},
		{
			name:    "worker commander adds compact/clear/rewind",
			worker:  &nativeCatalogCommanderWorker{nativeCatalogTestWorker: &nativeCatalogTestWorker{fakeWorker: &fakeWorker{}}},
			present: []string{"compact", "clear", "rewind"},
			absent:  []string{"context", "mcp", "model", "perm"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := newTestCatalogStore(t, &nativeCatalogTestLocator{})
			merged, err := store.Lookup(context.Background(), "s1", "/work", tt.worker, 1)
			require.NoError(t, err)
			byName := descriptorsByName(merged)
			for _, present := range tt.present {
				require.Contains(t, byName, present, "expected %q in merged catalog", present)
			}
			for _, absent := range tt.absent {
				require.NotContains(t, byName, absent, "expected %q absent from merged catalog", absent)
			}
			require.Equal(t, worker.NativeCommandKindControl, byName["reset"].Kind)
			require.False(t, byName["reset"].StartsTurn)
		})
	}
}

func TestNativeCatalogCacheInvalidation(t *testing.T) {
	t.Parallel()

	store := newTestCatalogStore(t, &nativeCatalogTestLocator{})
	w1 := &nativeCatalogTestWorker{fakeWorker: &fakeWorker{}}
	w2 := &nativeCatalogTestWorker{fakeWorker: &fakeWorker{}}

	ctx := context.Background()

	_, err := store.Lookup(ctx, "s1", "/work", w1, 1)
	require.NoError(t, err)
	require.Equal(t, int32(1), w1.calls.Load(), "first lookup assembles fresh")

	// Same worker instance + same generation → cache hit, no refetch.
	_, err = store.Lookup(ctx, "s1", "/work", w1, 1)
	require.NoError(t, err)
	require.Equal(t, int32(1), w1.calls.Load(), "cache hit must not refetch")

	// Generation bump → refetch.
	_, err = store.Lookup(ctx, "s1", "/work", w1, 2)
	require.NoError(t, err)
	require.Equal(t, int32(2), w1.calls.Load(), "generation bump must refetch")

	// Different worker instance (same generation) → refetch.
	_, err = store.Lookup(ctx, "s1", "/work", w2, 2)
	require.NoError(t, err)
	require.Equal(t, int32(1), w2.calls.Load(), "worker instance change must refetch")

	// Invalidate drops the session entry → refetch.
	store.Invalidate("s1")
	_, err = store.Lookup(ctx, "s1", "/work", w2, 2)
	require.NoError(t, err)
	require.Equal(t, int32(2), w2.calls.Load(), "invalidate must force refetch")

	// Invalidation is session-scoped: an untouched session keeps its cache.
	_, err = store.Lookup(ctx, "s2", "/work", w2, 2)
	require.NoError(t, err)
	require.Equal(t, int32(3), w2.calls.Load(), "s2 lookup assembles fresh")
	store.Invalidate("s1")
	_, err = store.Lookup(ctx, "s2", "/work", w2, 2)
	require.NoError(t, err)
	require.Equal(t, int32(3), w2.calls.Load(), "invalidating s1 must not evict s2")
}

func TestNativeCatalogQueryIsBounded(t *testing.T) {
	t.Parallel()

	require.Equal(t, 5*time.Second, nativeCatalogQueryTimeout, "production bound must stay 5s (spec §8.2)")

	store := newTestCatalogStore(t, &nativeCatalogTestLocator{})
	w := &nativeCatalogTestWorker{fakeWorker: &fakeWorker{}, blockCtx: true}

	start := time.Now()
	_, err := store.Lookup(context.Background(), "s1", "/work", w, 1)
	elapsed := time.Since(start)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded, "provider blocking past the bound must surface a deadline error")
	require.Less(t, elapsed, time.Second, "bounded query must not run unbounded")
}

func TestNativeCatalogFetchFailureIsDiscoverableOnly(t *testing.T) {
	t.Parallel()

	locator := &nativeCatalogTestLocator{skills: []skills.Skill{
		{Name: "oracle-dba", Description: "fs oracle", FilePath: "/fs/oracle-dba/SKILL.md"},
	}}
	store := newTestCatalogStore(t, locator)

	sentinel := errors.New("provider unreachable")
	w := &nativeCatalogTestWorker{
		fakeWorker:  &fakeWorker{},
		providerErr: sentinel,
	}

	merged, err := store.Lookup(context.Background(), "s1", "/work", w, 1)
	require.Error(t, err, "fetch failure must surface so no caller marks entries callable")
	require.ErrorIs(t, err, sentinel)
	require.NotNil(t, merged, "degraded result still carries discoverable entries")
	byName := descriptorsByName(merged)

	// Fixed Gateway commands survive the degradation.
	require.Contains(t, byName, "reset")
	require.Equal(t, worker.NativeCommandKindControl, byName["reset"].Kind)

	// Filesystem entries survive as discoverable-only skill entries.
	fsEntry := byName["oracle-dba"]
	require.Equal(t, worker.NativeCommandKindSkill, fsEntry.Kind)
	require.True(t, fsEntry.StartsTurn)
	require.Equal(t, "/fs/oracle-dba/SKILL.md", fsEntry.Path)
}

func TestNativeCatalogACPAdvertisedCommands(t *testing.T) {
	t.Parallel()

	// The ACP session advertises commands that never exist on the filesystem.
	locator := &nativeCatalogTestLocator{skills: []skills.Skill{
		{Name: "oracle-dba", FilePath: "/fs/oracle-dba"},
	}}
	store := newTestCatalogStore(t, locator)

	w := &nativeCatalogTestWorker{
		fakeWorker: &fakeWorker{workerType: worker.TypeACP},
		descriptors: []worker.NativeCommandDescriptor{
			{Name: "tools", Description: "tool usage", Kind: worker.NativeCommandKindControl, Mode: worker.SkillModeAdvertisedCommand},
			{Name: "queue", Description: "queue status", Kind: worker.NativeCommandKindSkill, Mode: worker.SkillModeAdvertisedCommand, StartsTurn: true},
		},
	}

	merged, err := store.Lookup(context.Background(), "s_acp", "/work", w, 1)
	require.NoError(t, err)
	byName := descriptorsByName(merged)

	// ACP session-scoped commands appear as catalog entries (G3 fix).
	require.Contains(t, byName, "tools")
	require.Contains(t, byName, "queue")
	require.Equal(t, worker.SkillModeAdvertisedCommand, byName["tools"].Mode)
	require.Equal(t, worker.SkillModeAdvertisedCommand, byName["queue"].Mode)
}

func TestNativeCatalogACPSessionScopedNeverReused(t *testing.T) {
	t.Parallel()

	store := newTestCatalogStore(t, &nativeCatalogTestLocator{})
	ctx := context.Background()

	// First ACP session advertises commands.
	w1 := &nativeCatalogTestWorker{
		fakeWorker: &fakeWorker{workerType: worker.TypeACP},
		descriptors: []worker.NativeCommandDescriptor{
			{Name: "queue", Kind: worker.NativeCommandKindControl, Mode: worker.SkillModeAdvertisedCommand},
		},
	}
	merged1, err := store.Lookup(ctx, "s_acp", "/work", w1, 1)
	require.NoError(t, err)
	require.Contains(t, descriptorsByName(merged1), "queue")

	// A fresh worker instance (simulating ResetContext clearing the ACP
	// session) advertises nothing. The catalog must refetch and the old ads
	// must not survive into the new session (spec §5.2).
	w2 := &nativeCatalogTestWorker{fakeWorker: &fakeWorker{workerType: worker.TypeACP}}
	merged2, err := store.Lookup(ctx, "s_acp", "/work", w2, 2)
	require.NoError(t, err)
	require.Equal(t, int32(1), w2.calls.Load(), "fresh worker instance must refetch")
	byName := descriptorsByName(merged2)
	require.NotContains(t, byName, "queue", "ACP ads must never cross sessions")
	require.Contains(t, byName, "reset", "fixed commands still present after refresh")
}
