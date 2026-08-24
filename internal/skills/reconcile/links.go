package reconcile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

func (r *Reconciler) centralTarget(target Target) Target {
	return Target{
		CanonicalRoot: target.CanonicalRoot,
		WorkerAliases: []WorkerType{WorkerCodex, WorkerOpenCode},
	}
}

func aliasPackagePath(aliasRoot, packageName string) (string, error) {
	if !validPackageName(packageName) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPackageName, packageName)
	}
	root, err := filepath.Abs(filepath.Clean(aliasRoot))
	if err != nil {
		return "", err
	}
	return filepath.Join(root, packageName), nil
}

func (r *Reconciler) inspectLinkedProjectionState(target Target, manifest builtin.PackageManifest) (projectionInspection, error) {
	if len(target.AliasRoots) != 1 {
		return projectionInspection{}, fmt.Errorf("skills: expected one Claude alias root")
	}
	central := r.centralTarget(target)
	sourceInspection, err := r.inspectProjectionState(central, manifest)
	if err != nil {
		return projectionInspection{}, err
	}
	sourcePath, err := PackageTargetIdentity(target.CanonicalRoot, manifest.Name)
	if err != nil {
		return projectionInspection{}, err
	}
	linkPath, err := aliasPackagePath(target.AliasRoots[0], manifest.Name)
	if err != nil {
		return projectionInspection{}, err
	}
	item := Item{
		Target:        linkPath,
		WorkerAliases: cloneWorkerAliases(target.WorkerAliases),
		Action:        ActionInstall,
		Outcome:       OutcomeChanged,
		ReasonCode:    ReasonChanged,
	}
	inspection := projectionInspection{item: item, linkPath: linkPath, sourcePath: sourcePath}

	rootLinked, err := r.aliasRootLinked(target.AliasRoots[0], target.CanonicalRoot)
	if err != nil {
		return projectionInspection{}, err
	}
	if rootLinked {
		inspection.item.Action = ActionNone
		inspection.item.Outcome = OutcomeUnchanged
		inspection.item.ReasonCode = ReasonRootLinked
		return inspection, nil
	}

	sourceHash := sourceInspection.treeHash
	if sourceHash == "" {
		sourceHash, _ = treeHash(r.fs, sourcePath)
	}
	if sourceInspection.item.Outcome == OutcomeConflict || sourceInspection.item.Outcome == OutcomeFailed {
		inspection.item.Outcome = sourceInspection.item.Outcome
		inspection.item.ReasonCode = sourceInspection.item.ReasonCode
		return inspection, nil
	}
	if sourceInspection.item.ReasonCode == ReasonMissingTarget {
		inspection.item.Action = ActionNone
		inspection.item.Outcome = OutcomeUnchanged
		inspection.item.ReasonCode = ReasonMissingTarget
		return inspection, nil
	}
	if sourceHash == "" {
		inspection.item.Outcome = OutcomeConflict
		inspection.item.ReasonCode = ReasonCollision
		return inspection, nil
	}

	info, lstatErr := r.fs.Lstat(linkPath)
	if errors.Is(lstatErr, os.ErrNotExist) {
		inspection.item.ReasonCode = ReasonMissingLink
		return inspection, nil
	}
	if lstatErr != nil {
		return projectionInspection{}, lstatErr
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, resolveErr := canonicalFSPath(r.fs, linkPath)
		if resolveErr != nil || resolved != sourcePath {
			inspection.item.Outcome = OutcomeConflict
			inspection.item.ReasonCode = ReasonCollision
			return inspection, nil
		}
		receiptPath := ReceiptPath(r.paths.StateDir, linkPath)
		receiptRaw, receiptErr := r.fs.ReadFile(receiptPath)
		receipt, parseErr := parseReceipt(receiptRaw)
		if receiptErr != nil {
			parseErr = receiptErr
		}
		if parseErr != nil || !linkReceiptMatches(receipt, manifest, linkPath, sourcePath, sourceHash) {
			inspection.item.Outcome = OutcomeConflict
			inspection.item.ReasonCode = ReasonInvalidReceipt
			return inspection, nil
		}
		inspection.item.Action = ActionNone
		inspection.item.Outcome = OutcomeUnchanged
		inspection.item.ReasonCode = ReasonUnchanged
		inspection.owned = true
		inspection.receipt = receipt
		inspection.receiptRaw = receiptRaw
		inspection.treeHash = sourceHash
		return inspection, nil
	}

	if !info.IsDir() {
		inspection.item.Outcome = OutcomeConflict
		inspection.item.ReasonCode = ReasonCollision
		return inspection, nil
	}
	actualHash, hashErr := treeHash(r.fs, linkPath)
	if hashErr != nil {
		inspection.item.Outcome = OutcomeConflict
		inspection.item.ReasonCode = ReasonCollision
		return inspection, nil
	}
	receiptRaw, receiptErr := r.fs.ReadFile(ReceiptPath(r.paths.StateDir, linkPath))
	receipt, parseErr := parseReceipt(receiptRaw)
	if receiptErr != nil {
		parseErr = receiptErr
	}
	if parseErr == nil && receiptMatches(receipt, manifest, linkPath, []WorkerType{WorkerClaude}, actualHash) && actualHash == sourceHash {
		inspection.item.Action = ActionUpdate
		inspection.item.Outcome = OutcomeChanged
		inspection.item.ReasonCode = ReasonChanged
		inspection.owned = true
		inspection.receipt = receipt
		inspection.receiptRaw = receiptRaw
		inspection.treeHash = actualHash
		inspection.legacyTree = true
		return inspection, nil
	}
	inspection.item.Outcome = OutcomeConflict
	inspection.item.ReasonCode = ReasonCollision
	return inspection, nil
}

func (r *Reconciler) aliasRootLinked(aliasRoot, centralRoot string) (bool, error) {
	info, err := r.fs.Lstat(aliasRoot)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		if !info.IsDir() {
			return false, fmt.Errorf("%w: alias root is not a directory", ErrRootOutsideHome)
		}
		return false, nil
	}
	resolved, err := canonicalFSPath(r.fs, aliasRoot)
	if err != nil {
		return false, err
	}
	central, err := canonicalFSPath(r.fs, centralRoot)
	if err != nil {
		return false, err
	}
	if resolved == central {
		return true, nil
	}
	return false, fmt.Errorf("%w: alias root resolves outside central root", ErrRootOutsideHome)
}

func (r *Reconciler) syncLinkedProjection(target Target, manifest builtin.PackageManifest) Item {
	centralItem := r.syncProjection(r.centralTarget(target), manifest)
	item := Item{Target: filepath.Join(target.AliasRoots[0], manifest.Name), WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionInstall, Outcome: OutcomeChanged, ReasonCode: ReasonChanged}
	if centralItem.Outcome == OutcomeConflict || centralItem.Outcome == OutcomeFailed || centralItem.Outcome == OutcomeDrift {
		item.Outcome = centralItem.Outcome
		item.ReasonCode = centralItem.ReasonCode
		return item
	}
	inspection, err := r.inspectLinkedProjectionState(target, manifest)
	if err != nil {
		return failedItem(item, ReasonCollision)
	}
	if inspection.item.ReasonCode == ReasonRootLinked {
		inspection.item.Action = ActionNone
		inspection.item.Outcome = OutcomeUnchanged
		return inspection.item
	}
	if inspection.item.Outcome == OutcomeConflict {
		return inspection.item
	}
	sourceHash := inspection.treeHash
	if sourceHash == "" {
		sourceHash, err = treeHash(r.fs, inspection.sourcePath)
		if err != nil {
			return failedItem(item, ReasonCollision)
		}
	}
	return r.ensureAliasLink(target, manifest, inspection, sourceHash)
}

func (r *Reconciler) ensureAliasLink(target Target, manifest builtin.PackageManifest, inspection projectionInspection, sourceHash string) Item {
	linkPath := inspection.linkPath
	if err := r.ensureAliasDirectory(target.AliasRoots[0], target.CanonicalRoot); err != nil {
		return failedItem(inspection.item, ReasonRootOutsideHome)
	}
	info, err := r.fs.Lstat(linkPath)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		if !inspection.owned {
			if receiptErr := r.writeLinkReceipt(linkPath, inspection.sourcePath, manifest, sourceHash); receiptErr != nil {
				return failedItem(inspection.item, ReasonReceiptWriteFailed)
			}
		}
		inspection.item.Action = ActionNone
		inspection.item.Outcome = OutcomeUnchanged
		inspection.item.ReasonCode = ReasonUnchanged
		return inspection.item
	}
	if err == nil && !inspection.legacyTree {
		return failedItem(inspection.item, ReasonCollision)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return failedItem(inspection.item, ReasonCollision)
	}

	backupContainer, backupPath := "", ""
	if inspection.legacyTree {
		backupContainer, err = r.fs.MkdirTemp(filepath.Dir(linkPath), ".hotplex-link-backup-*")
		if err != nil {
			return failedItem(inspection.item, ReasonDrift)
		}
		backupPath = filepath.Join(backupContainer, filepath.Base(linkPath))
		if err := r.fs.Rename(linkPath, backupPath); err != nil {
			_ = r.fs.RemoveAll(backupContainer)
			return failedItem(inspection.item, ReasonDrift)
		}
	}
	if err := r.createAliasLink(filepath.Dir(linkPath), linkPath, inspection.sourcePath); err != nil {
		if backupPath != "" {
			_ = r.fs.Rename(backupPath, linkPath)
			_ = r.fs.RemoveAll(backupContainer)
		}
		return failedItem(inspection.item, ReasonReceiptWriteFailed)
	}
	if err := r.writeLinkReceipt(linkPath, inspection.sourcePath, manifest, sourceHash); err != nil {
		_ = r.fs.Remove(linkPath)
		if backupPath != "" {
			_ = r.fs.Rename(backupPath, linkPath)
		}
		if backupContainer != "" {
			_ = r.fs.RemoveAll(backupContainer)
		}
		return failedItem(inspection.item, ReasonReceiptWriteFailed)
	}
	if backupContainer != "" {
		if err := r.fs.RemoveAll(backupContainer); err != nil {
			inspection.item.BackupPath = backupContainer
			return failedItem(inspection.item, ReasonDrift)
		}
	}
	inspection.item.Action = ActionUpdate
	inspection.item.Outcome = OutcomeChanged
	inspection.item.ReasonCode = ReasonChanged
	return inspection.item
}

func (r *Reconciler) ensureAliasDirectory(aliasRoot, centralRoot string) error {
	linked, err := r.aliasRootLinked(aliasRoot, centralRoot)
	if err != nil {
		return err
	}
	if linked {
		return nil
	}
	return r.fs.MkdirAll(aliasRoot, 0o755)
}

func (r *Reconciler) createAliasLink(parent, linkPath, sourcePath string) error {
	stage, err := r.fs.MkdirTemp(parent, ".hotplex-link-*")
	if err != nil {
		return err
	}
	if err := r.fs.RemoveAll(stage); err != nil {
		return err
	}
	if err := r.fs.Symlink(sourcePath, stage); err != nil {
		return err
	}
	if err := r.fs.Rename(stage, linkPath); err != nil {
		_ = r.fs.Remove(stage)
		return err
	}
	return r.fs.SyncDir(parent)
}

func (r *Reconciler) writeLinkReceipt(linkPath, sourcePath string, manifest builtin.PackageManifest, sourceHash string) error {
	return writeReceipt(r.fs, r.paths.StateDir, linkPath, Receipt{
		SchemaVersion:       receiptSchemaVersion,
		PackageVersion:      manifest.Version,
		PackageName:         manifest.Name,
		Profile:             manifest.Profile,
		CanonicalTarget:     linkPath,
		WorkerAliases:       []WorkerType{WorkerClaude},
		ManifestSHA256:      manifestHash(manifest),
		ProjectedTreeSHA256: sourceHash,
		ProjectionType:      "link",
		LinkTarget:          sourcePath,
	})
}

func (r *Reconciler) removeLinkedProjection(target Target, manifest builtin.PackageManifest) Item {
	inspection, err := r.inspectLinkedProjectionState(target, manifest)
	if err != nil {
		return failedItem(Item{Target: filepath.Join(target.AliasRoots[0], manifest.Name), WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionRemove}, ReasonCollision)
	}
	if inspection.item.ReasonCode == ReasonRootLinked || inspection.item.ReasonCode == ReasonMissingLink {
		return inspection.item
	}
	if !inspection.owned || inspection.item.Outcome != OutcomeUnchanged {
		inspection.item.Action = ActionRemove
		return inspection.item
	}
	parent := filepath.Dir(inspection.linkPath)
	backupContainer, err := r.fs.MkdirTemp(parent, ".hotplex-link-tombstone-*")
	if err != nil {
		return failedItem(inspection.item, ReasonDrift)
	}
	backupPath := filepath.Join(backupContainer, filepath.Base(inspection.linkPath))
	if err := r.fs.Rename(inspection.linkPath, backupPath); err != nil {
		_ = r.fs.RemoveAll(backupContainer)
		return failedItem(inspection.item, ReasonDrift)
	}
	receiptPath := ReceiptPath(r.paths.StateDir, inspection.linkPath)
	if err := r.fs.Remove(receiptPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = r.fs.Rename(backupPath, inspection.linkPath)
		_ = r.fs.RemoveAll(backupContainer)
		return failedItem(inspection.item, ReasonDrift)
	}
	if err := r.fs.RemoveAll(backupContainer); err != nil {
		inspection.item.BackupPath = backupContainer
		return failedItem(inspection.item, ReasonDrift)
	}
	inspection.item.Action = ActionRemove
	inspection.item.Outcome = OutcomeChanged
	inspection.item.ReasonCode = ReasonChanged
	return inspection.item
}
