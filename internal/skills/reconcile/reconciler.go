package reconcile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

var operationSequence atomic.Uint64

func New(registry *builtin.Registry, paths Paths, fs FileSystem) (*Reconciler, error) {
	if registry == nil {
		return nil, errors.New("skills: nil builtin registry")
	}
	normalized, err := normalizePaths(fs, paths)
	if err != nil {
		return nil, err
	}
	return &Reconciler{registry: registry, paths: normalized, fs: fs}, nil
}

func (r *Reconciler) ListInventory(ctx context.Context, profile builtin.Profile) ([]builtin.InstalledPackage, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if err := parseProfile(profile); err != nil {
		return nil, err
	}
	manifests, err := r.registry.PackagesForProfile(profile)
	if err != nil {
		return nil, err
	}
	if err := r.validateInventoryPath(); err != nil {
		return nil, err
	}
	result := make([]builtin.InstalledPackage, 0, len(manifests))
	for _, manifest := range manifests {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		path := filepath.Join(r.paths.InventoryDir, manifest.Version, manifest.Name)
		status, statusErr := r.inspectInventory(manifest)
		if statusErr != nil {
			return nil, statusErr
		}
		if status.conflict || status.missing {
			continue
		}
		result = append(result, builtin.InstalledPackage{Manifest: manifest, InventoryPath: path})
	}
	return result, nil
}

func (r *Reconciler) Status(ctx context.Context, options Options) (Report, error) {
	if err := contextErr(ctx); err != nil {
		return Report{}, err
	}
	if err := parseProfile(options.Profile); err != nil {
		return Report{}, err
	}
	targets, err := r.selectedTargets(options.WorkerTypes)
	if err != nil {
		return Report{}, err
	}
	manifests, err := r.registry.PackagesForProfile(options.Profile)
	if err != nil {
		return Report{}, err
	}
	if err := r.validateInventoryPath(); err != nil {
		return Report{}, err
	}
	if err := r.validateStatePath(); err != nil {
		return Report{}, err
	}
	report := Report{Profile: options.Profile}
	r.appendDirectoryRemnantItems(&report, r.paths.InventoryDir, nil, 2)
	r.appendDirectoryRemnantItems(&report, r.paths.StateDir, nil, 1)
	for _, target := range targets {
		if target.ReasonCode == ReasonUnsupportedWorker {
			report.Items = append(report.Items, unsupportedItem(target))
		}
	}
	if hasNativeTarget(targets) {
		inventory, inventoryErr := r.inventoryPreflight(manifests)
		if inventoryErr != nil {
			return report, inventoryErr
		}
		if inventoryBlocked(inventory) {
			appendInventoryBlockedItems(&report, targets, manifests, inventory)
			return report, nil
		}
		for _, target := range targets {
			if target.ReasonCode == ReasonUnsupportedWorker {
				continue
			}
			r.appendRootRemnantItems(&report, target)
			for _, manifest := range manifests {
				item, inspectErr := r.inspectProjection(target, manifest)
				if inspectErr != nil {
					return report, inspectErr
				}
				report.Items = append(report.Items, item)
			}
		}
	}
	return report, nil
}

func (r *Reconciler) Sync(ctx context.Context, options Options) (Report, error) {
	if err := contextErr(ctx); err != nil {
		return Report{}, err
	}
	if err := parseProfile(options.Profile); err != nil {
		return Report{}, err
	}
	targets, err := r.selectedTargets(options.WorkerTypes)
	if err != nil {
		return Report{}, err
	}
	manifests, err := r.registry.PackagesForProfile(options.Profile)
	if err != nil {
		return Report{}, err
	}
	if err := r.validateInventoryPath(); err != nil {
		return Report{}, err
	}
	if err := r.validateStatePath(); err != nil {
		return Report{}, err
	}
	report := Report{Profile: options.Profile}
	for _, target := range targets {
		if target.ReasonCode == ReasonUnsupportedWorker {
			report.Items = append(report.Items, unsupportedItem(target))
		}
	}
	if !hasNativeTarget(targets) {
		return report, nil
	}
	inventory, inventoryErr := r.inventoryPreflight(manifests)
	if inventoryErr != nil {
		return report, inventoryErr
	}
	if inventoryBlocked(inventory) {
		appendInventoryBlockedItems(&report, targets, manifests, inventory)
		return report, nil
	}
	if options.DryRun {
		for _, target := range targets {
			if target.ReasonCode == ReasonUnsupportedWorker {
				continue
			}
			for _, manifest := range manifests {
				item, inspectErr := r.inspectProjection(target, manifest)
				if inspectErr != nil {
					return report, inspectErr
				}
				if item.Outcome == OutcomeChanged && item.Action == ActionInstall {
					item.ReasonCode = ReasonChanged
				}
				report.Items = append(report.Items, item)
			}
		}
		return report, nil
	}
	if err := r.publishMissingInventory(inventory); err != nil {
		appendInventoryFailureItems(&report, targets, manifests)
		return report, nil
	}
	var operationErrors []error
	for _, target := range targets {
		if target.ReasonCode == ReasonUnsupportedWorker {
			continue
		}
		for _, manifest := range manifests {
			if err := contextErr(ctx); err != nil {
				return report, err
			}
			item, operationErr := r.syncProjection(target, manifest)
			report.Items = append(report.Items, item)
			if operationErr != nil {
				operationErrors = append(operationErrors, operationErr)
			}
		}
	}
	return report, errors.Join(operationErrors...)
}

func (r *Reconciler) Remove(ctx context.Context, options Options) (Report, error) {
	if err := contextErr(ctx); err != nil {
		return Report{}, err
	}
	if err := parseProfile(options.Profile); err != nil {
		return Report{}, err
	}
	targets, err := r.selectedTargets(options.WorkerTypes)
	if err != nil {
		return Report{}, err
	}
	manifests, err := r.registry.PackagesForProfile(options.Profile)
	if err != nil {
		return Report{}, err
	}
	if err := r.validateStatePath(); err != nil {
		return Report{}, err
	}
	report := Report{Profile: options.Profile}
	for _, target := range targets {
		if target.ReasonCode == ReasonUnsupportedWorker {
			report.Items = append(report.Items, unsupportedItem(target))
			continue
		}
		for _, manifest := range manifests {
			item, inspectErr := r.inspectProjection(target, manifest)
			if inspectErr != nil {
				return report, inspectErr
			}
			if item.Outcome != OutcomeUnchanged {
				item.Action = ActionRemove
				report.Items = append(report.Items, item)
				continue
			}
			if options.DryRun {
				item.Action = ActionRemove
				item.Outcome = OutcomeChanged
				item.ReasonCode = ReasonChanged
				report.Items = append(report.Items, item)
				continue
			}
			removed, removeErr := r.removeProjection(target, manifest)
			report.Items = append(report.Items, removed)
			if removeErr != nil {
				return report, removeErr
			}
		}
	}
	return report, nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (r *Reconciler) selectedTargets(workerTypes []WorkerType) ([]Target, error) {
	if len(workerTypes) == 0 {
		return nil, ErrNoWorkerTargets
	}
	seen := make(map[WorkerType]struct{}, len(workerTypes))
	for _, workerType := range workerTypes {
		parsed, err := ParseWorkerType(string(workerType))
		if err != nil {
			return nil, err
		}
		seen[parsed] = struct{}{}
	}
	targets := make([]Target, 0, len(seen))
	if _, ok := seen[WorkerClaude]; ok {
		root, err := r.validateNativeRoot(WorkerClaude)
		if err != nil {
			return nil, err
		}
		targets = append(targets, Target{CanonicalRoot: root, WorkerAliases: []WorkerType{WorkerClaude}})
	}
	if _, codex := seen[WorkerCodex]; codex {
		root, err := r.validateNativeRoot(WorkerCodex)
		if err != nil {
			return nil, err
		}
		targets = append(targets, Target{CanonicalRoot: root, WorkerAliases: []WorkerType{WorkerCodex, WorkerOpenCode}})
	} else if _, openCode := seen[WorkerOpenCode]; openCode {
		root, err := r.validateNativeRoot(WorkerOpenCode)
		if err != nil {
			return nil, err
		}
		targets = append(targets, Target{CanonicalRoot: root, WorkerAliases: []WorkerType{WorkerCodex, WorkerOpenCode}})
	}
	if _, ok := seen[WorkerACP]; ok {
		targets = append(targets, Target{WorkerAliases: []WorkerType{WorkerACP}, ReasonCode: ReasonUnsupportedWorker})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].CanonicalRoot < targets[j].CanonicalRoot })
	return targets, nil
}

func (r *Reconciler) validateNativeRoot(workerType WorkerType) (string, error) {
	root := r.paths.NativeRoots[workerType]
	if root == "" {
		return "", fmt.Errorf("%w: missing root for %s", ErrRootOutsideHome, workerType)
	}
	canonicalRoot, err := canonicalFSPath(r.fs, root)
	if err != nil {
		return "", fmt.Errorf("resolve native root: %w", err)
	}
	baseName := ".agents"
	if workerType == WorkerClaude {
		baseName = ".claude"
	}
	base, err := canonicalFSPath(r.fs, filepath.Join(r.paths.UserHome, baseName))
	if err != nil {
		return "", fmt.Errorf("resolve native base: %w", err)
	}
	if !isWithin(r.paths.UserHome, base) || !isWithin(base, canonicalRoot) {
		return "", fmt.Errorf("%w: %s", ErrRootOutsideHome, root)
	}
	return canonicalRoot, nil
}

func (r *Reconciler) revalidateTarget(target Target) error {
	if len(target.WorkerAliases) == 0 {
		return ErrRootOutsideHome
	}
	workerType := target.WorkerAliases[0]
	if workerType == WorkerOpenCode {
		workerType = WorkerCodex
	}
	root, err := r.validateNativeRoot(workerType)
	if err != nil {
		return err
	}
	if root != target.CanonicalRoot {
		return fmt.Errorf("%w: native root changed", ErrRootOutsideHome)
	}
	return nil
}

func (r *Reconciler) validateInventoryPath() error {
	path, err := canonicalFSPath(r.fs, r.paths.InventoryDir)
	if err != nil {
		return err
	}
	if !isWithin(r.paths.HotplexHome, path) || path != r.paths.InventoryDir {
		return ErrInventoryOutsideHotplexHome
	}
	return nil
}

func (r *Reconciler) validateStatePath() error {
	path, err := canonicalFSPath(r.fs, r.paths.StateDir)
	if err != nil {
		return err
	}
	if !isWithin(r.paths.HotplexHome, path) || path != r.paths.StateDir {
		return ErrInventoryOutsideHotplexHome
	}
	return nil
}

func hasNativeTarget(targets []Target) bool {
	for _, target := range targets {
		if target.ReasonCode != ReasonUnsupportedWorker {
			return true
		}
	}
	return false
}

func unsupportedItem(target Target) Item {
	return Item{Target: string(WorkerACP), WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionNone, Outcome: OutcomeFailed, ReasonCode: ReasonUnsupportedWorker}
}

type inventoryStatus struct {
	manifest builtin.PackageManifest
	path     string
	missing  bool
	conflict bool
	reason   string
	err      error
}

func (r *Reconciler) inventoryPreflight(manifests []builtin.PackageManifest) ([]inventoryStatus, error) {
	statuses := make([]inventoryStatus, 0, len(manifests))
	for _, manifest := range manifests {
		status, err := r.inspectInventory(manifest)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (r *Reconciler) inspectInventory(manifest builtin.PackageManifest) (inventoryStatus, error) {
	path := filepath.Join(r.paths.InventoryDir, manifest.Version, manifest.Name)
	status := inventoryStatus{manifest: manifest, path: path}
	info, err := r.fs.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		status.missing = true
		return status, nil
	}
	if err != nil {
		status.conflict = true
		status.reason = ReasonCollision
		status.err = err
		return status, nil
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		status.conflict = true
		status.reason = ReasonCollision
		return status, nil
	}
	actual, hashErr := treeHash(r.fs, path)
	if hashErr != nil {
		status.conflict = true
		status.reason = ReasonCollision
		status.err = hashErr
		return status, nil
	}
	expected, expectedErr := r.expectedManifestTreeHash(manifest)
	if expectedErr != nil {
		return status, expectedErr
	}
	if actual != expected {
		status.conflict = true
		status.reason = ReasonCollision
		return status, nil
	}
	return status, nil
}

func inventoryBlocked(statuses []inventoryStatus) bool {
	for _, status := range statuses {
		if status.conflict || status.err != nil {
			return true
		}
	}
	return false
}

func appendInventoryBlockedItems(report *Report, targets []Target, manifests []builtin.PackageManifest, statuses []inventoryStatus) {
	for _, status := range statuses {
		if status.conflict || status.err != nil {
			report.Items = append(report.Items, Item{Target: status.path, Action: ActionNone, Outcome: OutcomeConflict, ReasonCode: status.reason})
		}
	}
	for _, target := range targets {
		if target.ReasonCode == ReasonUnsupportedWorker {
			continue
		}
		for _, manifest := range manifests {
			report.Items = append(report.Items, Item{
				Target:        PackageTargetIdentity(target.CanonicalRoot, manifest.Name),
				WorkerAliases: cloneWorkerAliases(target.WorkerAliases),
				Action:        ActionInstall,
				Outcome:       OutcomeConflict,
				ReasonCode:    ReasonInventoryBlocked,
			})
		}
	}
}

func appendInventoryFailureItems(report *Report, targets []Target, manifests []builtin.PackageManifest) {
	for _, target := range targets {
		if target.ReasonCode == ReasonUnsupportedWorker {
			continue
		}
		for _, manifest := range manifests {
			report.Items = append(report.Items, Item{Target: PackageTargetIdentity(target.CanonicalRoot, manifest.Name), WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionInstall, Outcome: OutcomeFailed, ReasonCode: ReasonInventoryBlocked})
		}
	}
}

func (r *Reconciler) publishMissingInventory(statuses []inventoryStatus) error {
	for _, status := range statuses {
		if !status.missing {
			continue
		}
		if err := r.publishInventory(status.manifest, status.path); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) publishInventory(manifest builtin.PackageManifest, target string) (retErr error) {
	if !validPackageName(manifest.Name) {
		return fmt.Errorf("%w: %s", ErrInventoryOutsideHotplexHome, manifest.Name)
	}
	if err := r.validateInventoryPath(); err != nil {
		return err
	}
	parent := filepath.Dir(target)
	if err := r.fs.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage := uniqueSibling(r.fs, target, ".hotplex-inventory-stage")
	owned := true
	defer func() {
		if owned {
			if cleanupErr := r.fs.RemoveAll(stage); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
				retErr = errors.Join(retErr, cleanupErr)
			}
		}
	}()
	if err := r.fs.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	for _, asset := range manifest.Assets {
		if !validAssetPath(asset.Path) {
			return fmt.Errorf("invalid asset path %q", asset.Path)
		}
		data, err := r.registry.ReadFile(manifest.Name, asset.Path)
		if err != nil {
			return err
		}
		path := filepath.Join(stage, filepath.FromSlash(asset.Path))
		if err := r.fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := r.fs.WriteFile(path, data, 0o644); err != nil {
			return err
		}
		if err := r.fs.SyncFile(path); err != nil {
			return err
		}
	}
	actualHash, err := treeHash(r.fs, stage)
	if err != nil {
		return err
	}
	expectedHash, err := r.expectedManifestTreeHash(manifest)
	if err != nil {
		return err
	}
	if actualHash != expectedHash {
		return fmt.Errorf("inventory tree hash mismatch")
	}
	if err := r.fs.SyncDir(stage); err != nil {
		return err
	}
	if err := r.validateInventoryPath(); err != nil {
		return err
	}
	if err := r.fs.Rename(stage, target); err != nil {
		return err
	}
	owned = false
	return r.fs.SyncDir(parent)
}

func validAssetPath(name string) bool {
	if name == "" || filepath.IsAbs(filepath.FromSlash(name)) || strings.ContainsRune(name, '\\') {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	return clean == name && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func (r *Reconciler) expectedManifestTreeHash(manifest builtin.PackageManifest) (string, error) {
	assets := append([]builtin.AssetManifest(nil), manifest.Assets...)
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })
	h := sha256.New()
	for _, asset := range assets {
		if !validAssetPath(asset.Path) {
			return "", fmt.Errorf("invalid asset path %q", asset.Path)
		}
		data, err := r.registry.ReadFile(manifest.Name, asset.Path)
		if err != nil {
			return "", err
		}
		_, _ = h.Write([]byte(filepath.ToSlash(asset.Path)))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(strconv.FormatInt(asset.Size, 10)))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func uniqueSibling(fs FileSystem, target, suffix string) string {
	parent := filepath.Dir(target)
	base := filepath.Base(target)
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("%s%s", base, suffix)
		if i > 0 {
			name = fmt.Sprintf("%s%s-%d", base, suffix, i)
		}
		candidate := filepath.Join(parent, name)
		if _, err := fs.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
	return filepath.Join(parent, fmt.Sprintf("%s%s-%d", base, suffix, operationSequence.Add(1)))
}

func (r *Reconciler) appendRootRemnantItems(report *Report, target Target) {
	entries, err := r.fs.ReadDir(target.CanonicalRoot)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.Contains(name, ".hotplex-") {
			continue
		}
		if !strings.Contains(name, "stage") && !strings.Contains(name, "backup") && !strings.Contains(name, "tombstone") {
			continue
		}
		item := Item{Target: filepath.Join(target.CanonicalRoot, name), WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionNone, Outcome: OutcomeDrift, ReasonCode: ReasonDrift}
		if strings.Contains(name, "backup") {
			item.BackupPath = item.Target
		}
		report.Items = append(report.Items, item)
	}
}

func (r *Reconciler) appendDirectoryRemnantItems(report *Report, root string, aliases []WorkerType, depth int) {
	if depth < 0 {
		return
	}
	entries, err := r.fs.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if strings.Contains(entry.Name(), ".hotplex-") {
			item := Item{Target: path, WorkerAliases: cloneWorkerAliases(aliases), Action: ActionNone, Outcome: OutcomeDrift, ReasonCode: ReasonDrift}
			if strings.Contains(entry.Name(), "backup") || strings.Contains(entry.Name(), "tombstone") {
				item.BackupPath = path
			}
			report.Items = append(report.Items, item)
		}
		info, lstatErr := r.fs.Lstat(path)
		if lstatErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			r.appendDirectoryRemnantItems(report, path, aliases, depth-1)
		}
	}
}

type projectionInspection struct {
	item       Item
	receipt    Receipt
	receiptRaw []byte
	receiptOK  bool
	treeHash   string
}

func (r *Reconciler) inspectProjection(target Target, manifest builtin.PackageManifest) (Item, error) {
	inspection, err := r.inspectProjectionState(target, manifest)
	return inspection.item, err
}

func (r *Reconciler) inspectProjectionState(target Target, manifest builtin.PackageManifest) (projectionInspection, error) {
	identity := PackageTargetIdentity(target.CanonicalRoot, manifest.Name)
	item := Item{Target: identity, WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionInstall, Outcome: OutcomeChanged, ReasonCode: ReasonChanged}
	receiptPath := ReceiptPath(r.paths.StateDir, identity)
	receiptRaw, rawErr := r.fs.ReadFile(receiptPath)
	receipt, receiptErr := readReceipt(r.fs, receiptPath)
	if errors.Is(rawErr, os.ErrNotExist) {
		receiptRaw = nil
	}
	info, lstatErr := r.fs.Lstat(identity)
	if errors.Is(lstatErr, os.ErrNotExist) {
		if rawErr == nil || receiptErr == nil {
			item.Outcome = OutcomeDrift
			item.ReasonCode = ReasonDrift
		}
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, receiptOK: receiptErr == nil}, nil
	}
	if lstatErr != nil {
		return projectionInspection{}, lstatErr
	}
	if info.Mode()&os.ModeSymlink != 0 {
		item.Outcome = OutcomeConflict
		item.ReasonCode = ReasonCollision
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, receiptOK: receiptErr == nil}, nil
	}
	if !info.IsDir() {
		item.Outcome = OutcomeConflict
		item.ReasonCode = ReasonCollision
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, receiptOK: receiptErr == nil}, nil
	}
	actualHash, hashErr := treeHash(r.fs, identity)
	if hashErr != nil {
		item.Outcome = OutcomeConflict
		item.ReasonCode = ReasonCollision
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, receiptOK: receiptErr == nil}, nil
	}
	if receiptErr != nil {
		item.Outcome = OutcomeConflict
		item.ReasonCode = ReasonInvalidReceipt
		if errors.Is(receiptErr, os.ErrNotExist) {
			item.ReasonCode = ReasonMissingReceipt
		}
		return projectionInspection{item: item, receiptRaw: receiptRaw, treeHash: actualHash}, nil
	}
	if receipt.CanonicalTarget != identity || receipt.PackageName != manifest.Name || !equalAliases(receipt.WorkerAliases, target.WorkerAliases) {
		item.Outcome = OutcomeConflict
		item.ReasonCode = ReasonInvalidReceipt
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, receiptOK: true, treeHash: actualHash}, nil
	}
	if receipt.ProjectedTreeSHA256 != actualHash {
		item.Action = ActionUpdate
		item.Outcome = OutcomeDrift
		item.ReasonCode = ReasonDrift
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, receiptOK: true, treeHash: actualHash}, nil
	}
	if receiptMatches(receipt, manifest, identity, target.WorkerAliases, actualHash) {
		item.Action = ActionNone
		item.Outcome = OutcomeUnchanged
		item.ReasonCode = ReasonUnchanged
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, receiptOK: true, treeHash: actualHash}, nil
	}
	item.Action = ActionUpdate
	item.Outcome = OutcomeChanged
	item.ReasonCode = ReasonChanged
	return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, receiptOK: true, treeHash: actualHash}, nil
}

func (r *Reconciler) syncProjection(target Target, manifest builtin.PackageManifest) (item Item, retErr error) {
	inspection, err := r.inspectProjectionState(target, manifest)
	if err != nil {
		return Item{Target: PackageTargetIdentity(target.CanonicalRoot, manifest.Name), WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionInstall, Outcome: OutcomeFailed, ReasonCode: ReasonCollision}, err
	}
	if inspection.item.Outcome != OutcomeChanged {
		return inspection.item, nil
	}
	identity := inspection.item.Target
	parent := filepath.Dir(identity)
	if err := r.revalidateTarget(target); err != nil {
		return failedItem(inspection.item, ReasonRootOutsideHome), nil
	}
	stage := uniqueSibling(r.fs, identity, ".hotplex-stage")
	if err := r.stageProjection(manifest, stage); err != nil {
		return failedItem(inspection.item, ReasonReceiptWriteFailed), err
	}
	ownedStage := true
	defer func() {
		if ownedStage {
			if cleanupErr := r.fs.RemoveAll(stage); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
				retErr = errors.Join(retErr, cleanupErr)
			}
		}
	}()
	if err := r.fs.MkdirAll(filepath.Dir(identity), 0o755); err != nil {
		return failedItem(inspection.item, ReasonReceiptWriteFailed), err
	}
	backup := ""
	oldReceipt := inspection.receiptRaw
	if inspection.item.Action == ActionUpdate {
		if err := r.revalidateTarget(target); err != nil {
			return failedItem(inspection.item, ReasonRootOutsideHome), nil
		}
		current, currentErr := r.inspectProjectionState(target, manifest)
		if currentErr != nil {
			return failedItem(inspection.item, ReasonDrift), currentErr
		}
		if current.item.Outcome != OutcomeChanged || current.item.Action != ActionUpdate || !bytes.Equal(current.receiptRaw, inspection.receiptRaw) {
			return failedItem(inspection.item, ReasonDrift), nil
		}
		backup = uniqueSibling(r.fs, identity, ".hotplex-backup")
		if err := r.fs.Rename(identity, backup); err != nil {
			return failedItem(inspection.item, ReasonCollision), err
		}
		if err := r.fs.SyncDir(parent); err != nil {
			rollbackErr := r.restoreProjection(identity, backup, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
			return failedItemWithBackup(inspection.item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
		}
	}
	if err := r.revalidateTarget(target); err != nil {
		return failedItemWithBackup(inspection.item, backup, nil), nil
	}
	if inspection.item.Action == ActionInstall {
		if _, err := r.fs.Lstat(identity); err == nil {
			return failedItem(inspection.item, ReasonCollision), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return failedItem(inspection.item, ReasonCollision), err
		}
	}
	if err := r.fs.Rename(stage, identity); err != nil {
		rollbackErr := r.restoreProjection(identity, backup, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	ownedStage = false
	if err := r.fs.SyncDir(parent); err != nil {
		rollbackErr := r.restoreProjection(identity, backup, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	projectedHash, hashErr := treeHash(r.fs, identity)
	if hashErr != nil {
		rollbackErr := r.restoreProjection(identity, backup, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, backup, rollbackErr), rollbackOutcome(hashErr, rollbackErr)
	}
	receipt := Receipt{
		SchemaVersion:       receiptSchemaVersion,
		PackageVersion:      manifest.Version,
		PackageName:         manifest.Name,
		Profile:             manifest.Profile,
		CanonicalTarget:     identity,
		WorkerAliases:       cloneWorkerAliases(target.WorkerAliases),
		ManifestSHA256:      manifestHash(manifest),
		ProjectedTreeSHA256: projectedHash,
	}
	if err := r.validateStatePath(); err != nil {
		rollbackErr := r.restoreProjection(identity, backup, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	if err := writeReceipt(r.fs, r.paths.StateDir, identity, receipt); err != nil {
		rollbackErr := r.restoreProjection(identity, backup, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	if backup != "" {
		if err := r.fs.RemoveAll(backup); err != nil {
			rollbackErr := r.restoreProjection(identity, backup, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
			return failedItemWithBackup(inspection.item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
		}
		if err := r.fs.SyncDir(parent); err != nil {
			rollbackErr := r.restoreProjection(identity, backup, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
			return failedItemWithBackup(inspection.item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
		}
	}
	item = inspection.item
	item.Outcome = OutcomeChanged
	item.ReasonCode = ReasonChanged
	item.BackupPath = backup
	return item, nil
}

func (r *Reconciler) stageProjection(manifest builtin.PackageManifest, stage string) error {
	if !validPackageName(manifest.Name) {
		return fmt.Errorf("invalid package name %q", manifest.Name)
	}
	if err := r.fs.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	for _, asset := range manifest.Assets {
		if !validAssetPath(asset.Path) {
			return fmt.Errorf("invalid asset path %q", asset.Path)
		}
		data, err := r.registry.ReadFile(manifest.Name, asset.Path)
		if err != nil {
			return err
		}
		path := filepath.Join(stage, filepath.FromSlash(asset.Path))
		if err := r.fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := r.fs.WriteFile(path, data, 0o644); err != nil {
			return err
		}
		if err := r.fs.SyncFile(path); err != nil {
			return err
		}
	}
	actualHash, err := treeHash(r.fs, stage)
	if err != nil {
		return err
	}
	expectedHash, err := r.expectedManifestTreeHash(manifest)
	if err != nil {
		return err
	}
	if actualHash != expectedHash {
		return fmt.Errorf("projection tree hash mismatch")
	}
	return r.fs.SyncDir(stage)
}

func (r *Reconciler) restoreProjection(identity, backup, stage, receiptPath string, oldReceipt []byte) error {
	var errs []error
	if info, err := r.fs.Lstat(identity); err == nil && info.IsDir() {
		if removeErr := r.fs.RemoveAll(identity); removeErr != nil {
			errs = append(errs, removeErr)
		}
	}
	if backup != "" {
		if _, err := r.fs.Lstat(backup); err == nil {
			if err := r.fs.Rename(backup, identity); err != nil {
				errs = append(errs, err)
			} else if err := r.fs.SyncDir(filepath.Dir(identity)); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(oldReceipt) > 0 {
		if err := writeRawReceipt(r.fs, filepath.Dir(receiptPath), receiptPath, oldReceipt); err != nil {
			errs = append(errs, err)
		}
	} else {
		if err := r.fs.Remove(receiptPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		} else if err := r.fs.SyncDir(filepath.Dir(receiptPath)); err != nil {
			errs = append(errs, err)
		}
	}
	if stage != "" {
		if err := r.fs.RemoveAll(stage); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func writeRawReceipt(fs FileSystem, stateDir, finalPath string, data []byte) (retErr error) {
	if err := fs.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	temp := filepath.Join(stateDir, fmt.Sprintf(".%s.restore-%d", filepath.Base(finalPath), operationSequence.Add(1)))
	owned := true
	defer func() {
		if owned {
			if cleanupErr := fs.Remove(temp); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
				retErr = errors.Join(retErr, cleanupErr)
			}
		}
	}()
	if err := fs.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	if err := fs.SyncFile(temp); err != nil {
		return err
	}
	if err := atomicReplace(fs, temp, finalPath); err != nil {
		return err
	}
	owned = false
	return fs.SyncDir(stateDir)
}

func (r *Reconciler) removeProjection(target Target, manifest builtin.PackageManifest) (Item, error) {
	inspection, err := r.inspectProjectionState(target, manifest)
	if err != nil {
		return Item{Target: PackageTargetIdentity(target.CanonicalRoot, manifest.Name), WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionRemove, Outcome: OutcomeFailed, ReasonCode: ReasonCollision}, err
	}
	item := inspection.item
	item.Action = ActionRemove
	if inspection.item.Outcome != OutcomeUnchanged {
		return item, nil
	}
	identity := item.Target
	receiptPath := ReceiptPath(r.paths.StateDir, identity)
	parent := filepath.Dir(identity)
	if err := r.revalidateTarget(target); err != nil {
		return failedItem(item, ReasonRootOutsideHome), nil
	}
	current, currentErr := r.inspectProjectionState(target, manifest)
	if currentErr != nil {
		return failedItem(item, ReasonDrift), currentErr
	}
	if current.item.Outcome != OutcomeUnchanged || !bytes.Equal(current.receiptRaw, inspection.receiptRaw) {
		return failedItem(item, ReasonDrift), nil
	}
	backup := uniqueSibling(r.fs, identity, ".hotplex-backup")
	if err := r.fs.Rename(identity, backup); err != nil {
		return failedItemWithBackup(item, backup, nil), nil
	}
	if err := r.fs.SyncDir(parent); err != nil {
		rollbackErr := r.restoreRemoved(identity, backup, receiptPath, inspection.receiptRaw, "")
		return failedItemWithBackup(item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	tombstone := uniqueSibling(r.fs, receiptPath, ".hotplex-tombstone")
	if err := r.validateStatePath(); err != nil {
		rollbackErr := r.restoreRemoved(identity, backup, receiptPath, inspection.receiptRaw, "")
		return failedItemWithBackup(item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	if err := r.fs.Rename(receiptPath, tombstone); err != nil {
		rollbackErr := r.restoreRemoved(identity, backup, receiptPath, inspection.receiptRaw, tombstone)
		return failedItemWithBackup(item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	if err := r.fs.SyncDir(r.paths.StateDir); err != nil {
		rollbackErr := r.restoreRemoved(identity, backup, receiptPath, inspection.receiptRaw, tombstone)
		return failedItemWithBackup(item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	if err := r.fs.Remove(tombstone); err != nil {
		rollbackErr := r.restoreRemoved(identity, backup, receiptPath, inspection.receiptRaw, tombstone)
		return failedItemWithBackup(item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	if err := r.fs.SyncDir(r.paths.StateDir); err != nil {
		rollbackErr := r.restoreRemoved(identity, backup, receiptPath, inspection.receiptRaw, "")
		return failedItemWithBackup(item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	if err := r.fs.RemoveAll(backup); err != nil {
		rollbackErr := r.restoreRemoved(identity, backup, receiptPath, inspection.receiptRaw, "")
		return failedItemWithBackup(item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	if err := r.fs.SyncDir(parent); err != nil {
		rollbackErr := r.restoreRemoved(identity, backup, receiptPath, inspection.receiptRaw, "")
		return failedItemWithBackup(item, backup, rollbackErr), rollbackOutcome(err, rollbackErr)
	}
	item.Outcome = OutcomeChanged
	item.ReasonCode = ReasonChanged
	item.BackupPath = backup
	return item, nil
}

func (r *Reconciler) restoreRemoved(identity, backup, receiptPath string, receiptRaw []byte, tombstone string) error {
	var errs []error
	if tombstone != "" {
		if _, err := r.fs.Lstat(tombstone); err == nil {
			if err := r.fs.Rename(tombstone, receiptPath); err != nil {
				errs = append(errs, err)
			} else if err := r.fs.SyncDir(filepath.Dir(receiptPath)); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if _, err := r.fs.Lstat(identity); errors.Is(err, os.ErrNotExist) {
		if _, backupErr := r.fs.Lstat(backup); backupErr == nil {
			if err := r.fs.Rename(backup, identity); err != nil {
				errs = append(errs, err)
			} else if err := r.fs.SyncDir(filepath.Dir(identity)); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(receiptRaw) > 0 {
		if _, err := r.fs.Lstat(receiptPath); errors.Is(err, os.ErrNotExist) {
			if err := writeRawReceipt(r.fs, filepath.Dir(receiptPath), receiptPath, receiptRaw); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func failedItem(item Item, reason string) Item {
	item.Outcome = OutcomeFailed
	item.ReasonCode = reason
	return item
}

func failedItemWithBackup(item Item, backup string, rollbackErr error) Item {
	item = failedItem(item, ReasonReceiptWriteFailed)
	item.BackupPath = backup
	if rollbackErr != nil {
		item.ReasonCode = ReasonRollbackFailed
	}
	return item
}

func rollbackOutcome(primary, rollbackErr error) error {
	if rollbackErr == nil {
		return nil
	}
	return errors.Join(primary, rollbackErr)
}
