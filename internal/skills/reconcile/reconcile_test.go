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

func TestStatusACPStillReportsInventoryWithoutWriting(t *testing.T) {
	r, _ := newTestReconciler(t)
	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	inventoryTarget := filepath.Join(r.paths.InventoryDir, manifest.Version, manifest.Name)
	report, err := r.Status(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerACP}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	inventoryIndex, unsupportedIndex := -1, -1
	for index, item := range report.Items {
		if item.Target == inventoryTarget {
			inventoryIndex = index
			require.Equal(t, ReasonMissingTarget, item.ReasonCode)
		}
		if item.ReasonCode == ReasonUnsupportedWorker {
			unsupportedIndex = index
		}
	}
	require.GreaterOrEqual(t, inventoryIndex, 0)
	require.Greater(t, unsupportedIndex, inventoryIndex)
	_, err = os.Stat(r.paths.InventoryDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSyncACPDoesNotPublishInventory(t *testing.T) {
	fs := &recordingFS{FileSystem: osFS{}}
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	r, _ := newTestReconcilerWithFS(t, fs, userHome, hotplexHome)
	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerACP}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	require.Zero(t, fs.writeCalls())
	_, err = os.Stat(r.paths.InventoryDir)
	require.ErrorIs(t, err, os.ErrNotExist)
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
	first := ReceiptPath(state, mustPackageTargetIdentity(root, "hotplex-cli"))
	second := ReceiptPath(state, mustPackageTargetIdentity(equivalentRoot, "hotplex-cli"))
	require.Equal(t, first, second)
	require.NotEqual(t, first, ReceiptPath(state, mustPackageTargetIdentity(root, "hotplex-operator")))
}

func TestPackageTargetIdentityRejectsInvalidName(t *testing.T) {
	_, err := PackageTargetIdentity(t.TempDir(), "../escape")
	require.ErrorIs(t, err, ErrInvalidPackageName)
}

func TestReceiptParserRejectsMalformedOwnershipFields(t *testing.T) {
	r, _ := newTestReconciler(t)
	_, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	receipt, err := readReceipt(r.fs, ReceiptPath(r.paths.StateDir, mustPackageTargetIdentity(r.paths.NativeRoots[WorkerClaude], manifest.Name)))
	require.NoError(t, err)
	for name, mutate := range map[string]func(*Receipt){
		"relative target":         func(value *Receipt) { value.CanonicalTarget = "relative/target" },
		"uppercase manifest hash": func(value *Receipt) { value.ManifestSHA256 = strings.Repeat("A", 64) },
		"invalid version":         func(value *Receipt) { value.PackageVersion = "v-test" },
		"duplicate aliases":       func(value *Receipt) { value.WorkerAliases = []WorkerType{WorkerClaude, WorkerClaude} },
		"acp alias":               func(value *Receipt) { value.WorkerAliases = []WorkerType{WorkerACP} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := receipt
			mutate(&candidate)
			data, marshalErr := marshalReceipt(candidate)
			require.NoError(t, marshalErr)
			_, parseErr := parseReceipt(data)
			require.ErrorIs(t, parseErr, ErrInvalidReceipt)
		})
	}
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
	_, err = os.Stat(ReceiptPath(paths.StateDir, mustPackageTargetIdentity(paths.NativeRoots[WorkerClaude], manifest.Name)))
	require.NoError(t, err)
}

func TestFreshStatusReportsInventoryAndProjectionDrift(t *testing.T) {
	r, paths := newTestReconciler(t)
	report, err := r.Status(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	require.Contains(t, reportReasons(report.Items), ReasonMissingTarget)
	require.Contains(t, reportActions(report.Items), ActionInstall)
	require.Contains(t, reportOutcomes(report.Items), OutcomeDrift)
	nativeTarget := filepath.Join(r.paths.NativeRoots[WorkerClaude], "hotplex-cli")
	var nativeMissing Item
	for _, item := range report.Items {
		if item.Target == nativeTarget {
			nativeMissing = item
			break
		}
	}
	require.Equal(t, ActionInstall, nativeMissing.Action)
	require.Equal(t, OutcomeDrift, nativeMissing.Outcome)
	require.Equal(t, ReasonMissingTarget, nativeMissing.ReasonCode)
	_, err = os.Stat(paths.InventoryDir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSyncDryRunReportsInventoryAndProjectionPlanWithoutWrites(t *testing.T) {
	base := osFS{}
	fs := &recordingFS{FileSystem: base}
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	r, err := New(registry, paths, fs)
	require.NoError(t, err)
	userBefore, hotplexBefore := snapshotTree(t, userHome), snapshotTree(t, hotplexHome)
	report, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}, DryRun: true})
	require.NoError(t, err)
	require.NoError(t, report.Err())
	require.Contains(t, reportReasons(report.Items), ReasonChanged)
	require.Contains(t, reportActions(report.Items), ActionInstall)
	require.Contains(t, reportOutcomes(report.Items), OutcomeChanged)
	require.Zero(t, fs.writeCalls())
	require.Equal(t, userBefore, snapshotTree(t, userHome))
	require.Equal(t, hotplexBefore, snapshotTree(t, hotplexHome))
}

func TestRemoveMissingTargetAndReceiptIsIdempotent(t *testing.T) {
	r, _ := newTestReconciler(t)
	first, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.NoError(t, first.Err())
	require.Contains(t, reportReasons(first.Items), ReasonMissingTarget)
	require.Contains(t, reportActions(first.Items), ActionNone)
	require.Contains(t, reportOutcomes(first.Items), OutcomeUnchanged)
	second, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.NoError(t, second.Err())
	require.Equal(t, reportReasons(first.Items), reportReasons(second.Items))
}

func TestRemoveRefusesModifiedTree(t *testing.T) {
	r, paths := newTestReconciler(t)
	_, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	target := filepath.Join(paths.NativeRoots[WorkerClaude], manifest.Name)
	require.NoError(t, os.WriteFile(filepath.Join(target, "user-change.txt"), []byte("keep"), 0o644))
	report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.ErrorIs(t, report.Err(), ErrReportActionRequired)
	require.Contains(t, reportOutcomes(report.Items), OutcomeDrift)
	_, err = os.Stat(filepath.Join(target, "user-change.txt"))
	require.NoError(t, err)
}

func TestSyncIsIdempotentWhenManifestAndTreeMatch(t *testing.T) {
	r, _ := newTestReconciler(t)
	first, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.NoError(t, first.Err())
	second, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.NoError(t, second.Err())
	require.Contains(t, reportOutcomes(second.Items), OutcomeUnchanged)
}

func TestRemoveRequiresMatchingReceiptAndTarget(t *testing.T) {
	t.Run("target exists without receipt", func(t *testing.T) {
		r, _ := newTestReconciler(t)
		_, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		require.NoError(t, err)
		manifest, ok := r.registry.Package("hotplex-cli")
		require.True(t, ok)
		target := filepath.Join(r.paths.NativeRoots[WorkerClaude], manifest.Name)
		receiptPath := ReceiptPath(r.paths.StateDir, mustPackageTargetIdentity(r.paths.NativeRoots[WorkerClaude], manifest.Name))
		require.NoError(t, os.Remove(receiptPath))
		report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		require.NoError(t, err)
		require.ErrorIs(t, report.Err(), ErrReportActionRequired)
		require.Contains(t, reportReasons(report.Items), ReasonMissingReceipt)
		_, err = os.Stat(target)
		require.NoError(t, err)
	})

	t.Run("target missing with receipt", func(t *testing.T) {
		r, _ := newTestReconciler(t)
		_, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		require.NoError(t, err)
		manifest, ok := r.registry.Package("hotplex-cli")
		require.True(t, ok)
		target := filepath.Join(r.paths.NativeRoots[WorkerClaude], manifest.Name)
		require.NoError(t, os.RemoveAll(target))
		report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		require.NoError(t, err)
		require.ErrorIs(t, report.Err(), ErrReportActionRequired)
		require.Contains(t, reportReasons(report.Items), ReasonDrift)
	})

	t.Run("invalid receipt", func(t *testing.T) {
		r, _ := newTestReconciler(t)
		_, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		require.NoError(t, err)
		manifest, ok := r.registry.Package("hotplex-cli")
		require.True(t, ok)
		receiptPath := ReceiptPath(r.paths.StateDir, mustPackageTargetIdentity(r.paths.NativeRoots[WorkerClaude], manifest.Name))
		require.NoError(t, os.WriteFile(receiptPath, []byte("{}\n"), 0o600))
		report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		require.NoError(t, err)
		require.ErrorIs(t, report.Err(), ErrReportActionRequired)
		require.Contains(t, reportReasons(report.Items), ReasonInvalidReceipt)
	})

	t.Run("missing target with invalid receipt", func(t *testing.T) {
		r, _ := newTestReconciler(t)
		_, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		require.NoError(t, err)
		manifest, ok := r.registry.Package("hotplex-cli")
		require.True(t, ok)
		identity := mustPackageTargetIdentity(r.paths.NativeRoots[WorkerClaude], manifest.Name)
		require.NoError(t, os.RemoveAll(identity))
		receiptPath := ReceiptPath(r.paths.StateDir, identity)
		require.NoError(t, os.WriteFile(receiptPath, []byte("{}\n"), 0o600))
		report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		require.NoError(t, err)
		require.ErrorIs(t, report.Err(), ErrReportActionRequired)
		require.Contains(t, reportReasons(report.Items), ReasonInvalidReceipt)
	})

	t.Run("profile mismatch", func(t *testing.T) {
		r, _ := newTestReconciler(t)
		_, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		require.NoError(t, err)
		manifest, ok := r.registry.Package("hotplex-cli")
		require.True(t, ok)
		identity := mustPackageTargetIdentity(r.paths.NativeRoots[WorkerClaude], manifest.Name)
		receiptPath := ReceiptPath(r.paths.StateDir, identity)
		receipt, err := readReceipt(r.fs, receiptPath)
		require.NoError(t, err)
		receipt.Profile = builtin.ProfileOperator
		require.NoError(t, writeReceipt(r.fs, r.paths.StateDir, identity, receipt))
		report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		require.NoError(t, err)
		require.ErrorIs(t, report.Err(), ErrReportActionRequired)
		require.Contains(t, reportReasons(report.Items), ReasonInvalidReceipt)
	})
}

func TestRemoveAcceptsOwnedProjectionWithOlderManifestReceipt(t *testing.T) {
	r, _ := newTestReconciler(t)
	_, err := r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	identity := mustPackageTargetIdentity(r.paths.NativeRoots[WorkerClaude], manifest.Name)
	receiptPath := ReceiptPath(r.paths.StateDir, identity)
	receipt, err := readReceipt(r.fs, receiptPath)
	require.NoError(t, err)
	receipt.PackageVersion = "v1-" + strings.Repeat("0", 64)
	receipt.ManifestSHA256 = strings.Repeat("1", 64)
	require.NoError(t, writeReceipt(r.fs, r.paths.StateDir, identity, receipt))
	report, err := r.Remove(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
	require.NoError(t, err)
	require.NoError(t, report.Err())
	require.Contains(t, reportOutcomes(report.Items), OutcomeChanged)
	_, err = os.Stat(identity)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(receiptPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAnyInventoryConflictBlocksEveryNativeProjection(t *testing.T) {
	r, paths := newTestReconciler(t)
	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	conflictPath := filepath.Join(r.paths.InventoryDir, manifest.Version, manifest.Name)
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

func TestInventoryConflictIsReportedOncePerOperation(t *testing.T) {
	r, _ := newTestReconciler(t)
	manifest, ok := r.registry.Package("hotplex-cli")
	require.True(t, ok)
	conflictPath := filepath.Join(r.paths.InventoryDir, manifest.Version, manifest.Name)
	base := osFS{}
	require.NoError(t, base.MkdirAll(conflictPath, 0o755))
	require.NoError(t, base.WriteFile(filepath.Join(conflictPath, "SKILL.md"), []byte("modified"), 0o644))
	for _, operation := range []func() (Report, error){
		func() (Report, error) {
			return r.Status(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		},
		func() (Report, error) {
			return r.Sync(t.Context(), Options{Profile: builtin.ProfileRuntime, WorkerTypes: []WorkerType{WorkerClaude}})
		},
	} {
		report, err := operation()
		require.NoError(t, err)
		count := 0
		for _, item := range report.Items {
			if item.Target == conflictPath {
				count++
			}
		}
		require.Equal(t, 1, count)
	}
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
	receipt, err := readReceipt(r.fs, ReceiptPath(paths.StateDir, mustPackageTargetIdentity(paths.NativeRoots[WorkerClaude], manifest.Name)))
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
	receipt, err := readReceipt(r.fs, ReceiptPath(paths.StateDir, mustPackageTargetIdentity(paths.NativeRoots[WorkerCodex], manifest.Name)))
	require.NoError(t, err)
	require.Equal(t, []WorkerType{WorkerCodex, WorkerOpenCode}, receipt.WorkerAliases)
	for _, item := range report.Items {
		if item.Target == mustPackageTargetIdentity(paths.NativeRoots[WorkerCodex], manifest.Name) {
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
	_, err = os.Stat(ReceiptPath(paths.StateDir, mustPackageTargetIdentity(paths.NativeRoots[WorkerClaude], manifest.Name)))
	require.NoError(t, err)
}

func newTestReconciler(t *testing.T) (*Reconciler, Paths) {
	t.Helper()
	userHome, hotplexHome := t.TempDir(), t.TempDir()
	return newTestReconcilerWithFS(t, osFS{}, userHome, hotplexHome)
}

func newTestReconcilerWithFS(t *testing.T, fs FileSystem, userHome, hotplexHome string) (*Reconciler, Paths) {
	t.Helper()
	paths := testPaths(userHome, hotplexHome)
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	r, err := New(registry, paths, fs)
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

func reportReasons(items []Item) []string { return itemReasons(items) }

func reportActions(items []Item) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Action)
	}
	return result
}

func reportOutcomes(items []Item) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Outcome)
	}
	return result
}

func mustPackageTargetIdentity(root, packageName string) string {
	identity, err := PackageTargetIdentity(root, packageName)
	if err != nil {
		panic(err)
	}
	return identity
}

type recordingFS struct {
	FileSystem
	failRemoveContains    string
	failRenameContains    string
	failRenameOldContains string
	failWriteContains     string
	failSyncDir           bool
	failRemoveAllContains string
	failSyncDirAfter      int
	syncDirCalls          int
	writes                int
}

func (f *recordingFS) MkdirAll(path string, mode os.FileMode) error {
	f.writes++
	return f.FileSystem.MkdirAll(path, mode)
}

func (f *recordingFS) WriteFile(path string, data []byte, mode os.FileMode) error {
	f.writes++
	if f.failWriteContains != "" && strings.Contains(path, f.failWriteContains) {
		return errors.New("injected write failure")
	}
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
	f.syncDirCalls++
	if f.failSyncDirAfter > 0 && f.syncDirCalls >= f.failSyncDirAfter {
		return errors.New("injected sync directory failure after commit")
	}
	if err := f.failSync(); err != nil {
		return err
	}
	return f.FileSystem.SyncDir(path)
}

func (f *recordingFS) MkdirTemp(path, pattern string) (string, error) {
	f.writes++
	return f.FileSystem.MkdirTemp(path, pattern)
}

func (f *recordingFS) CreateTemp(path, pattern string, data []byte, mode os.FileMode) (string, error) {
	f.writes++
	return f.FileSystem.CreateTemp(path, pattern, data, mode)
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
