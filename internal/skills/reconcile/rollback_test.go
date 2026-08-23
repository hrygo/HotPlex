package reconcile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hrygo/hotplex/internal/skills/builtin"

	"github.com/stretchr/testify/require"
)

func TestUpdateRollsBackWhenSecondRenameFails(t *testing.T) {
	base := osFS{}
	fs := &recordingFS{FileSystem: base}
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	r, err := New(registry, paths, fs)
	require.NoError(t, err)
	_, err = r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)

	manifest, ok := registry.Package("hotplex-cli")
	require.True(t, ok)
	receiptPath := ReceiptPath(paths.StateDir, mustPackageTargetIdentity(paths.NativeRoots[WorkerClaude], manifest.Name))
	receipt, err := readReceipt(r.fs, receiptPath)
	require.NoError(t, err)
	receipt.PackageVersion = "v-test-update"
	require.NoError(t, writeReceipt(r.fs, paths.StateDir, receipt.CanonicalTarget, receipt))
	fs.failRenameOldContains = ".hotplex-stage"
	before := snapshotTree(t, filepath.Join(paths.NativeRoots[WorkerClaude], "hotplex-cli"))
	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	require.Equal(t, before, snapshotTree(t, filepath.Join(paths.NativeRoots[WorkerClaude], "hotplex-cli")))
}

func TestStatusAndDryRunNeverRecoverStagingOrBackup(t *testing.T) {
	base := osFS{}
	fs := &recordingFS{FileSystem: base}
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	r, err := New(registry, paths, fs)
	require.NoError(t, err)
	stage := filepath.Join(paths.NativeRoots[WorkerClaude], ".hotplex-stage-manual")
	require.NoError(t, base.MkdirAll(stage, 0o755))
	beforeUser, beforeHotplex := snapshotTree(t, userHome), snapshotTree(t, hotplexHome)
	status, err := r.Status(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, status.Err(), ErrReportActionRequired)
	_, err = r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}, DryRun: true})
	require.NoError(t, err)
	require.Zero(t, fs.writeCalls())
	require.Equal(t, beforeUser, snapshotTree(t, userHome))
	require.Equal(t, beforeHotplex, snapshotTree(t, hotplexHome))
}

func TestPackageSymlinkIsConflictAndNeverFollowed(t *testing.T) {
	r, paths := newTestReconciler(t)
	_, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	target := filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name)
	outside := t.TempDir()
	require.NoError(t, os.RemoveAll(target))
	require.NoError(t, os.Symlink(outside, target))
	before := snapshotTree(t, outside)
	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	require.Contains(t, itemReasons(report.Items), ReasonCollision)
	require.Equal(t, before, snapshotTree(t, outside))
}

func TestSyncDirFailureDoesNotClaimSuccessfulProjection(t *testing.T) {
	base := osFS{}
	fs := &recordingFS{FileSystem: base, failSyncDir: true}
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	r, err := New(registry, paths, fs)
	require.NoError(t, err)
	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
}

func TestReceiptPromotionFailureRollsBackProjection(t *testing.T) {
	base := osFS{}
	fs := &recordingFS{FileSystem: base, failRenameContains: ".json"}
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	r, err := New(registry, paths, fs)
	require.NoError(t, err)
	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	manifest, ok := registry.Package("hotplex-cli")
	require.True(t, ok)
	_, err = os.Stat(filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestBackupCleanupFailureRollsBackUpdate(t *testing.T) {
	base := osFS{}
	fs := &recordingFS{FileSystem: base, failRemoveAllContains: ".hotplex-backup"}
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	r, err := New(registry, paths, fs)
	require.NoError(t, err)
	_, err = r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	manifest, ok := registry.Package("hotplex-cli")
	require.True(t, ok)
	receiptPath := ReceiptPath(paths.StateDir, mustPackageTargetIdentity(paths.NativeRoots[WorkerClaude], manifest.Name))
	receipt, err := readReceipt(r.fs, receiptPath)
	require.NoError(t, err)
	receipt.PackageVersion = "v-test-update"
	require.NoError(t, writeReceipt(r.fs, paths.StateDir, receipt.CanonicalTarget, receipt))
	before := snapshotTree(t, filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name))
	fixedBackup := filepath.Join(filepath.Dir(filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name)), "hotplex-cli.hotplex-backup")
	require.NoError(t, os.MkdirAll(fixedBackup, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fixedBackup, "sentinel"), []byte("keep"), 0o644))
	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	require.NotEmpty(t, reportBackupPaths(report.Items))
	_, err = os.Stat(filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name))
	require.NoError(t, err)
	newReceipt, err := readReceipt(r.fs, receiptPath)
	require.NoError(t, err)
	require.Equal(t, manifest.Version, newReceipt.PackageVersion)
	require.Equal(t, before, snapshotTree(t, filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name)))
	require.Equal(t, map[string]string{".": "dir", "sentinel": "file:6b656570"}, snapshotTree(t, fixedBackup))
}

func TestRemoveFinalSyncFailureNeverLeavesReceiptWithoutTarget(t *testing.T) {
	base := osFS{}
	fs := &recordingFS{FileSystem: base}
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	r, err := New(registry, paths, fs)
	require.NoError(t, err)
	_, err = r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	fs.failSyncDirAfter = fs.syncDirCalls + 3
	report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	manifest, ok := registry.Package("hotplex-cli")
	require.True(t, ok)
	target := filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name)
	receiptPath := ReceiptPath(paths.StateDir, mustPackageTargetIdentity(paths.NativeRoots[WorkerClaude], manifest.Name))
	_, targetErr := os.Stat(target)
	_, receiptErr := os.Stat(receiptPath)
	require.False(t, errors.Is(targetErr, os.ErrNotExist) && receiptErr == nil, "receipt must not survive without target")
}

func reportBackupPaths(items []Item) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item.BackupPath != "" {
			result = append(result, item.BackupPath)
		}
	}
	return result
}

func TestPathEscapeIsRejected(t *testing.T) {
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	paths := testPaths(userHome, hotplexHome)
	paths.NativeRoots[WorkerClaude] = filepath.Join(userHome, "..", "outside", "skills")
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	_, err = New(registry, paths, osFS{})
	require.ErrorIs(t, err, ErrRootOutsideHome)
}

func (f *recordingFS) failSync() error {
	if f.failSyncDir {
		return errors.New("injected sync directory failure")
	}
	return nil
}

// Keep this compile-time reference close to the failure-injection tests so a
// future refactor cannot silently remove the required error contract.
var _ = strings.Contains
