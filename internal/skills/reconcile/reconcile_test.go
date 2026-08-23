package reconcile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hrygo/hotplex/internal/skills/builtin"

	"github.com/stretchr/testify/require"
)

func TestResolveTargetsDeduplicatesSharedAgentsRootAndKeepsAllAliases(t *testing.T) {
	userHome := t.TempDir()
	for _, selected := range []WorkerType{WorkerCodex, WorkerOpenCode} {
		targets, err := ResolveTargets(userHome, []WorkerType{selected})
		require.NoError(t, err)
		require.Len(t, targets, 1)
		require.Equal(t, []WorkerType{WorkerCodex, WorkerOpenCode}, targets[0].WorkerAliases)
		require.Equal(t, filepath.Join(userHome, ".agents", "skills"), targets[0].CanonicalRoot)
	}
}

func TestResolveTargetsRejectsEmptyListAndACPWithoutWriting(t *testing.T) {
	_, err := ResolveTargets(t.TempDir(), nil)
	require.ErrorIs(t, err, ErrNoWorkerTargets)
	targets, err := ResolveTargets(t.TempDir(), []WorkerType{WorkerACP})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, ReasonUnsupportedWorker, targets[0].ReasonCode)
	if targets[0].CanonicalRoot != "" {
		t.Fatalf("ACP unexpectedly has a write root: %q", targets[0].CanonicalRoot)
	}
}

func TestNativeRootSymlinkMustResolveInsideUserHomeAndApprovedBase(t *testing.T) {
	userHome := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(userHome, ".claude")))
	_, err := ResolveTargets(userHome, []WorkerType{WorkerClaude})
	require.ErrorIs(t, err, ErrRootOutsideHome)
}

func TestInventorySymlinkMustResolveInsideHotplexHome(t *testing.T) {
	userHome := t.TempDir()
	hotplexHome := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(hotplexHome, "skills")))
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	_, err = New(registry, paths, osFS{})
	require.ErrorIs(t, err, ErrInventoryOutsideHotplexHome)
}

func TestReceiptKeyUsesCanonicalTargetIdentity(t *testing.T) {
	state := t.TempDir()
	userHome := t.TempDir()
	root := filepath.Join(userHome, ".agents", "skills")
	equivalentRoot := filepath.Join(userHome, "nested", "..", ".agents", "skills")
	first := ReceiptPath(state, PackageTargetIdentity(root, "hotplex-cli"))
	second := ReceiptPath(state, PackageTargetIdentity(equivalentRoot, "hotplex-cli"))
	require.Equal(t, first, second)
	require.NotEqual(t, first, ReceiptPath(state, PackageTargetIdentity(root, "hotplex-operator")))
}

func TestReportErrIsBoundedForActionRequiredOutcomes(t *testing.T) {
	for _, outcome := range []string{OutcomeConflict, OutcomeDrift, OutcomeFailed} {
		report := Report{Items: []Item{{Outcome: outcome, ReasonCode: ReasonCollision}}}
		require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	}
	for _, outcome := range []string{OutcomeUnchanged, OutcomeChanged} {
		report := Report{Items: []Item{{Outcome: outcome}}}
		require.NoError(t, report.Err())
	}
}

func TestSyncInitialInstallWritesInventoryProjectionAndReceipt(t *testing.T) {
	r, paths := newTestReconciler(t)
	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.NoError(t, report.Err())
	require.NotEmpty(t, report.Items)

	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	target := filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name)
	_, err = os.Stat(target)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(paths.InventoryDir, manifest.Version, manifest.Name))
	require.NoError(t, err)
	_, err = os.Stat(ReceiptPath(paths.StateDir, PackageTargetIdentity(paths.NativeRoots[WorkerClaude], manifest.Name)))
	require.NoError(t, err)
}

func TestAnyInventoryConflictBlocksEveryNativeProjection(t *testing.T) {
	r, paths := newTestReconciler(t)
	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	conflictPath := filepath.Join(paths.InventoryDir, manifest.Version, manifest.Name)
	base := osFS{}
	require.NoError(t, base.MkdirAll(conflictPath, 0o755))
	require.NoError(t, base.WriteFile(filepath.Join(conflictPath, "SKILL.md"), []byte("modified"), 0o644))
	before := snapshotTree(t, paths.UserHome)

	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileOperator, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	require.NotEmpty(t, report.Items)
	require.Contains(t, itemReasons(report.Items), ReasonInventoryBlocked)
	require.Equal(t, before, snapshotTree(t, paths.UserHome))
}

func TestDryRunDoesNotCreateInventoryOrProjection(t *testing.T) {
	base := osFS{}
	fs := &recordingFS{FileSystem: base}
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	r, err := New(registry, paths, fs)
	require.NoError(t, err)

	beforeUser, beforeHotplex := snapshotTree(t, userHome), snapshotTree(t, hotplexHome)
	_, err = r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}, DryRun: true})
	require.NoError(t, err)
	require.Zero(t, fs.writeCalls())
	require.Equal(t, beforeUser, snapshotTree(t, userHome))
	require.Equal(t, beforeHotplex, snapshotTree(t, hotplexHome))
}

func TestOperatorSyncDoesNotReclassifyCliReceiptForRuntimeStatus(t *testing.T) {
	r, paths := newTestReconciler(t)
	_, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	_, err = r.Sync(t.Context(), Options{Profile: builtin.ProfileOperator, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)

	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	receipt, err := readReceipt(r.fs, ReceiptPath(paths.StateDir, PackageTargetIdentity(paths.NativeRoots[WorkerClaude], manifest.Name)))
	require.NoError(t, err)
	require.Equal(t, builtin.ProfileRuntime, receipt.Profile)
	status, err := r.Status(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.NoError(t, status.Err())
}

func TestSharedRootReceiptAndReportKeepAllWorkerAliases(t *testing.T) {
	r, paths := newTestReconciler(t)
	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerCodex}})
	require.NoError(t, err)
	require.NoError(t, report.Err())
	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	receipt, err := readReceipt(r.fs, ReceiptPath(paths.StateDir, PackageTargetIdentity(paths.NativeRoots[WorkerCodex], manifest.Name)))
	require.NoError(t, err)
	require.Equal(t, []WorkerType{WorkerCodex, WorkerOpenCode}, receipt.WorkerAliases)
	for _, item := range report.Items {
		if item.Target == PackageTargetIdentity(paths.NativeRoots[WorkerCodex], manifest.Name) {
			require.Equal(t, []WorkerType{WorkerCodex, WorkerOpenCode}, item.WorkerAliases)
		}
	}
}

func TestRemovePreservesImmutableInventory(t *testing.T) {
	r, paths := newTestReconciler(t)
	_, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	inventory := filepath.Join(paths.InventoryDir, manifest.Version, manifest.Name)
	beforeInventory := snapshotTree(t, inventory)

	report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.NoError(t, report.Err())
	require.Equal(t, beforeInventory, snapshotTree(t, inventory))
	_, err = os.Stat(filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRemoveRollsBackWhenReceiptDeleteFails(t *testing.T) {
	base := osFS{}
	fs := &recordingFS{FileSystem: base, failRemoveContains: "tombstone"}
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	r, err := New(registry, paths, fs)
	require.NoError(t, err)
	_, err = r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	manifest, ok := registry.Package("hotplex-cli")
	require.True(t, ok)
	_, err = os.Stat(filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name))
	require.NoError(t, err)
	_, err = os.Stat(ReceiptPath(paths.StateDir, PackageTargetIdentity(paths.NativeRoots[WorkerClaude], manifest.Name)))
	require.NoError(t, err)
}

func newTestReconciler(t *testing.T) (*Reconciler, Paths) {
	t.Helper()
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	r, err := New(registry, paths, osFS{})
	require.NoError(t, err)
	return r, paths
}

func testPaths(userHome, hotplexHome string) Paths {
	return Paths{
		UserHome:     userHome,
		HotplexHome:  hotplexHome,
		InventoryDir: filepath.Join(hotplexHome, "skills", "builtin"),
		StateDir:     filepath.Join(hotplexHome, "state", "skills"),
		NativeRoots: map[WorkerType]string{
			WorkerClaude:   filepath.Join(userHome, ".claude", "skills"),
			WorkerCodex:    filepath.Join(userHome, ".agents", "skills"),
			WorkerOpenCode: filepath.Join(userHome, ".agents", "skills"),
		},
	}
}

func itemReasons(items []Item) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.ReasonCode)
	}
	return result
}

type recordingFS struct {
	FileSystem
	failRemoveContains    string
	failRenameContains    string
	failRenameOldContains string
	failSyncDir           bool
	failRemoveAllContains string
	writes                int
}

func (f *recordingFS) MkdirAll(path string, mode os.FileMode) error {
	f.writes++
	return f.FileSystem.MkdirAll(path, mode)
}

func (f *recordingFS) WriteFile(path string, data []byte, mode os.FileMode) error {
	f.writes++
	return f.FileSystem.WriteFile(path, data, mode)
}

func (f *recordingFS) Rename(oldPath, newPath string) error {
	f.writes++
	if (f.failRenameContains != "" && strings.Contains(newPath, f.failRenameContains)) ||
		(f.failRenameOldContains != "" && strings.Contains(oldPath, f.failRenameOldContains)) {
		return errors.New("injected rename failure")
	}
	return f.FileSystem.Rename(oldPath, newPath)
}

func (f *recordingFS) Remove(path string) error {
	f.writes++
	if f.failRemoveContains != "" && strings.Contains(path, f.failRemoveContains) {
		return errors.New("injected remove failure")
	}
	return f.FileSystem.Remove(path)
}

func (f *recordingFS) RemoveAll(path string) error {
	f.writes++
	if f.failRemoveAllContains != "" && strings.Contains(path, f.failRemoveAllContains) {
		return errors.New("injected remove-all failure")
	}
	return f.FileSystem.RemoveAll(path)
}

func (f *recordingFS) SyncFile(path string) error {
	f.writes++
	return f.FileSystem.SyncFile(path)
}

func (f *recordingFS) SyncDir(path string) error {
	f.writes++
	if err := f.failSync(); err != nil {
		return err
	}
	return f.FileSystem.SyncDir(path)
}

func (f *recordingFS) writeCalls() int { return f.writes }

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return result
	}
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		require.NoError(t, err)
		rel, relErr := filepath.Rel(root, path)
		require.NoError(t, relErr)
		if info.IsDir() {
			result[rel] = "dir"
			return nil
		}
		data, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		result[rel] = fmt.Sprintf("file:%x", data)
		return nil
	}))
	return result
}
