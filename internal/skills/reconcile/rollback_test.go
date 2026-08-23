package reconcile

import (
	"bytes"
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
	receipt.PackageVersion = "v1-" + strings.Repeat("0", 64)
	receipt.ManifestSHA256 = strings.Repeat("1", 64)
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

func TestStageCleanupFailureRemainsVisibleToStatus(t *testing.T) {
	base := osFS{}
	fs := &recordingFS{FileSystem: base, failWriteContains: ".hotplex-stage-", failRemoveAllContains: ".hotplex-stage-"}
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	r, _ := newTestReconcilerWithFS(t, fs, userHome, hotplexHome)
	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	var remnant string
	for _, item := range report.Items {
		if item.BackupPath != "" && strings.Contains(item.BackupPath, ".hotplex-stage-") {
			remnant = item.BackupPath
		}
	}
	require.NotEmpty(t, remnant)
	_, err = os.Stat(remnant)
	require.NoError(t, err)
	status, err := r.Status(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, status.Err(), ErrReportActionRequired)
	var found bool
	for _, item := range status.Items {
		if item.Target == remnant {
			found = true
		}
	}
	require.True(t, found)
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
	manifest, ok := registry.Package("hotplex-cli")
	require.True(t, ok)
	inventoryTarget := filepath.Join(r.paths.InventoryDir, manifest.Version, manifest.Name)
	foundInventoryFailure := false
	foundProjectionBlock := false
	for _, item := range report.Items {
		if item.Target == inventoryTarget {
			foundInventoryFailure = item.Outcome == OutcomeFailed
		}
		if item.ReasonCode == ReasonInventoryBlocked && item.Outcome == OutcomeConflict {
			foundProjectionBlock = true
		}
	}
	require.True(t, foundInventoryFailure)
	require.True(t, foundProjectionBlock)
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

func TestBackupCleanupFailureRetainsCommittedUpdate(t *testing.T) {
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
	receipt.PackageVersion = "v1-" + strings.Repeat("0", 64)
	receipt.ManifestSHA256 = strings.Repeat("1", 64)
	require.NoError(t, writeReceipt(r.fs, paths.StateDir, receipt.CanonicalTarget, receipt))
	before := snapshotTree(t, filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name))
	fixedBackup := filepath.Join(filepath.Dir(filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name)), "hotplex-cli.hotplex-backup")
	require.NoError(t, os.MkdirAll(fixedBackup, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fixedBackup, "sentinel"), []byte("keep"), 0o644))
	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	backupPaths := reportBackupPaths(report.Items)
	require.NotEmpty(t, backupPaths)
	for _, backupPath := range backupPaths {
		_, statErr := os.Stat(backupPath)
		require.NoError(t, statErr)
	}
	target := filepath.Join(r.paths.NativeRoots[WorkerClaude], manifest.Name)
	_, err = os.Stat(target)
	require.NoError(t, err)
	newReceipt, err := readReceipt(r.fs, receiptPath)
	require.NoError(t, err)
	require.Equal(t, manifest.Version, newReceipt.PackageVersion)
	require.Equal(t, manifestHash(manifest), newReceipt.ManifestSHA256)
	require.Equal(t, mustPackageTargetIdentity(r.paths.NativeRoots[WorkerClaude], manifest.Name), newReceipt.CanonicalTarget)
	require.Equal(t, manifest.Profile, newReceipt.Profile)
	actualHash, err := treeHash(r.fs, target)
	require.NoError(t, err)
	require.Equal(t, actualHash, newReceipt.ProjectedTreeSHA256)
	require.Equal(t, before, snapshotTree(t, target))
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
	target := filepath.Join(r.paths.NativeRoots[WorkerClaude], manifest.Name)
	receiptPath := ReceiptPath(r.paths.StateDir, mustPackageTargetIdentity(r.paths.NativeRoots[WorkerClaude], manifest.Name))
	_, targetErr := os.Stat(target)
	_, receiptErr := os.Stat(receiptPath)
	targetExists := targetErr == nil
	receiptExists := receiptErr == nil
	require.Equal(t, targetExists, receiptExists, "remove must leave either complete old state or complete removed state")
	if targetExists {
		receipt, readErr := readReceipt(r.fs, receiptPath)
		require.NoError(t, readErr)
		actualHash, hashErr := treeHash(r.fs, target)
		require.NoError(t, hashErr)
		require.True(t, receiptMatches(receipt, manifest, target, []WorkerType{WorkerClaude}, actualHash))
	} else {
		require.ErrorIs(t, targetErr, os.ErrNotExist)
		require.ErrorIs(t, receiptErr, os.ErrNotExist)
	}
}

func TestRemoveRecoveryReplacesWrongReceiptWhenSyncDirKeepsFailing(t *testing.T) {
	base := osFS{}
	fs := &wrongReceiptRecoveryFS{FileSystem: base}
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
	target := mustPackageTargetIdentity(paths.NativeRoots[WorkerClaude], manifest.Name)
	receiptPath := ReceiptPath(r.paths.StateDir, target)
	wantedReceipt, err := os.ReadFile(receiptPath)
	require.NoError(t, err)
	fs.stateDir = r.paths.StateDir
	fs.receiptPath = receiptPath
	fs.wrongReceipt = []byte("{\"wrong\":true}\n")
	fs.mutateOnStateSync = true
	fs.failSyncDirAfter = fs.syncDirCalls + 3
	report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	require.NoError(t, fs.mutationErr)
	actualReceipt, err := os.ReadFile(receiptPath)
	require.NoError(t, err)
	require.True(t, bytes.Equal(wantedReceipt, actualReceipt), "recovery must not accept the injected wrong receipt")
	actualHash, err := treeHash(r.fs, target)
	require.NoError(t, err)
	receipt, err := parseReceipt(actualReceipt)
	require.NoError(t, err)
	require.Equal(t, actualHash, receipt.ProjectedTreeSHA256)
}

func TestRemoveRecoveryPreservesReceiptWhenBackupRenameFails(t *testing.T) {
	base := osFS{}
	fs := &wrongReceiptRecoveryFS{FileSystem: base}
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
	target := mustPackageTargetIdentity(paths.NativeRoots[WorkerClaude], manifest.Name)
	receiptPath := ReceiptPath(r.paths.StateDir, target)
	beforeTarget := snapshotTree(t, target)
	fs.stateDir = r.paths.StateDir
	fs.receiptPath = receiptPath
	fs.wrongReceipt = []byte("{\"wrong\":true}\n")
	fs.mutateOnStateSync = true
	fs.failSyncDirAfter = fs.syncDirCalls + 3
	fs.failRenameContains = ".hotplex-receipt-recovery-backup-"
	report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	require.NoError(t, fs.mutationErr)
	actualReceipt, err := os.ReadFile(receiptPath)
	require.NoError(t, err)
	require.Equal(t, fs.wrongReceipt, actualReceipt, "failed backup rename must preserve the existing receipt")
	require.Equal(t, beforeTarget, snapshotTree(t, target))
	entries, err := os.ReadDir(paths.StateDir)
	require.NoError(t, err)
	for _, entry := range entries {
		require.NotContains(t, entry.Name(), ".hotplex-receipt-recovery-backup-")
		require.NotContains(t, entry.Name(), ".hotplex-recovery-receipt-")
	}
	status, err := r.Status(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, status.Err(), ErrReportActionRequired)
	require.Contains(t, reportReasons(status.Items), ReasonInvalidReceipt)
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

type wrongReceiptRecoveryFS struct {
	FileSystem
	stateDir           string
	receiptPath        string
	wrongReceipt       []byte
	mutateOnStateSync  bool
	mutationErr        error
	mutated            bool
	failSyncDirAfter   int
	syncDirCalls       int
	failRenameContains string
}

func (f *wrongReceiptRecoveryFS) SyncDir(path string) error {
	f.syncDirCalls++
	if f.mutateOnStateSync && !f.mutated && path == f.stateDir {
		f.mutationErr = f.WriteFile(f.receiptPath, f.wrongReceipt, 0o600)
		f.mutated = true
	}
	if f.failSyncDirAfter > 0 && f.syncDirCalls >= f.failSyncDirAfter {
		return errors.New("injected continuous directory sync failure")
	}
	return f.FileSystem.SyncDir(path)
}

func (f *wrongReceiptRecoveryFS) Rename(oldPath, newPath string) error {
	if f.failRenameContains != "" && strings.Contains(newPath, f.failRenameContains) {
		return errors.New("injected receipt backup rename failure")
	}
	return f.FileSystem.Rename(oldPath, newPath)
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
