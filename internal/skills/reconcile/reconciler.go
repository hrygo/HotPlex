package reconcile

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

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
		status, statusErr := r.inspectInventory(manifest)
		if statusErr != nil {
			return nil, statusErr
		}
		if status.conflict || status.missing {
			continue
		}
		result = append(result, builtin.InstalledPackage{Manifest: manifest, InventoryPath: status.path})
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
	inventory, inventoryErr := r.inventoryPreflight(manifests)
	if inventoryErr != nil {
		return report, inventoryErr
	}
	appendInventoryStatusItems(&report, inventory)
	for _, target := range targets {
		if target.ReasonCode == ReasonUnsupportedWorker {
			report.Items = append(report.Items, unsupportedItem(target))
		}
	}
	if !hasNativeTarget(targets) {
		return report, nil
	}
	if inventoryBlocked(inventory) {
		appendInventoryBlockedItems(&report, targets, manifests)
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
			if item.ReasonCode == ReasonMissingTarget && item.Outcome == OutcomeUnchanged {
				item.Action = ActionInstall
				item.Outcome = OutcomeDrift
			}
			if item.Action == ActionUpdate && item.Outcome == OutcomeChanged {
				item.Outcome = OutcomeDrift
				item.ReasonCode = ReasonDrift
			}
			report.Items = append(report.Items, item)
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
		appendInventoryConflictItems(&report, inventory)
		appendInventoryBlockedItems(&report, targets, manifests)
		return report, nil
	}
	if options.DryRun {
		appendInventoryPlanItems(&report, inventory)
		for _, target := range targets {
			if target.ReasonCode == ReasonUnsupportedWorker {
				continue
			}
			for _, manifest := range manifests {
				item, inspectErr := r.inspectProjection(target, manifest)
				if inspectErr != nil {
					return report, inspectErr
				}
				if item.ReasonCode == ReasonMissingTarget && item.Outcome == OutcomeUnchanged {
					item.Action = ActionInstall
					item.Outcome = OutcomeChanged
					item.ReasonCode = ReasonChanged
				}
				report.Items = append(report.Items, item)
			}
		}
		return report, nil
	}
	if err := r.publishMissingInventory(inventory); err != nil {
		finalInventory, finalErr := r.inventoryPreflight(manifests)
		if finalErr != nil {
			finalInventory = inventory
		}
		appendInventoryPublicationFailureItems(&report, inventory, finalInventory)
		appendInventoryBlockedItems(&report, targets, manifests)
		return report, nil
	}
	appendInventoryChangeItems(&report, inventory)
	for _, target := range targets {
		if target.ReasonCode == ReasonUnsupportedWorker {
			continue
		}
		for _, manifest := range manifests {
			if err := contextErr(ctx); err != nil {
				return report, err
			}
			item := r.syncProjection(target, manifest)
			report.Items = append(report.Items, item)
		}
	}
	return report, nil
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
			var inspection projectionInspection
			var inspectErr error
			if len(target.AliasRoots) > 0 {
				inspection, inspectErr = r.inspectLinkedProjectionState(target, manifest)
			} else {
				inspection, inspectErr = r.inspectProjectionState(target, manifest)
			}
			if inspectErr != nil {
				return report, inspectErr
			}
			item := inspection.item
			if item.ReasonCode == ReasonMissingTarget && item.Outcome == OutcomeUnchanged {
				report.Items = append(report.Items, item)
				continue
			}
			if item.Outcome == OutcomeChanged && item.Action == ActionUpdate && !inspection.owned {
				item.Action = ActionRemove
				item.Outcome = OutcomeConflict
				item.ReasonCode = ReasonInvalidReceipt
				report.Items = append(report.Items, item)
				continue
			}
			if item.Outcome != OutcomeUnchanged && !inspection.owned {
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
			removed := r.removeProjection(target, manifest)
			report.Items = append(report.Items, removed)
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
	appendTarget := func(target Target) {
		for i := range targets {
			if targets[i].CanonicalRoot != target.CanonicalRoot || targets[i].ReasonCode != target.ReasonCode {
				continue
			}
			targets[i].WorkerAliases = appendUniqueWorkers(targets[i].WorkerAliases, target.WorkerAliases...)
			targets[i].AliasRoots = appendUniqueStrings(targets[i].AliasRoots, target.AliasRoots...)
			return
		}
		targets = append(targets, target)
	}
	if _, ok := seen[WorkerClaude]; ok {
		root, err := r.validateNativeRoot(WorkerClaude)
		if err != nil {
			return nil, err
		}
		target := Target{CanonicalRoot: root, WorkerAliases: []WorkerType{WorkerClaude}}
		if aliasRoot, aliasErr := r.validateAliasRoot(WorkerClaude); aliasErr == nil {
			target.AliasRoots = []string{aliasRoot}
		}
		appendTarget(target)
	}
	if _, ok := seen[WorkerCodex]; ok {
		root, err := r.validateNativeRoot(WorkerCodex)
		if err != nil {
			return nil, err
		}
		appendTarget(Target{CanonicalRoot: root, WorkerAliases: []WorkerType{WorkerCodex, WorkerOpenCode}})
	} else if _, ok := seen[WorkerOpenCode]; ok {
		root, err := r.validateNativeRoot(WorkerOpenCode)
		if err != nil {
			return nil, err
		}
		appendTarget(Target{CanonicalRoot: root, WorkerAliases: []WorkerType{WorkerCodex, WorkerOpenCode}})
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
	base, err := canonicalFSPath(r.fs, filepath.Join(r.paths.UserHome, baseName))
	if err != nil {
		return "", fmt.Errorf("resolve native base: %w", err)
	}
	if !isWithin(r.paths.UserHome, base) || !isWithin(base, canonicalRoot) {
		return "", fmt.Errorf("%w: %s", ErrRootOutsideHome, root)
	}
	return canonicalRoot, nil
}

func (r *Reconciler) validateAliasRoot(workerType WorkerType) (string, error) {
	root := r.paths.AliasRoots[workerType]
	if root == "" {
		return "", fmt.Errorf("%w: missing alias root for %s", ErrRootOutsideHome, workerType)
	}
	canonicalRoot, err := canonicalFSPath(r.fs, root)
	if err != nil {
		return "", fmt.Errorf("resolve alias root: %w", err)
	}
	base, err := canonicalFSPath(r.fs, filepath.Join(r.paths.UserHome, ".claude"))
	if err != nil {
		return "", fmt.Errorf("resolve alias base: %w", err)
	}
	centralRoot, err := r.validateNativeRoot(WorkerCodex)
	if err != nil {
		return "", err
	}
	if !isWithin(r.paths.UserHome, canonicalRoot) ||
		(!isWithin(base, canonicalRoot) && canonicalRoot != centralRoot) {
		return "", fmt.Errorf("%w: %s", ErrRootOutsideHome, root)
	}
	return filepath.Clean(root), nil
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
