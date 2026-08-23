package reconcile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

// syncProjection performs one explicit native-root transaction. The durable
// commit point is the successful parent sync after the new target and its
// matching receipt have both been promoted. Before that point rollback is
// allowed; after it, cleanup failures leave the new state in place and report
// the retained backup instead of risking data loss.
func (r *Reconciler) syncProjection(target Target, manifest builtin.PackageManifest) Item {
	inspection, err := r.inspectProjectionState(target, manifest)
	if err != nil {
		identity, identityErr := PackageTargetIdentity(target.CanonicalRoot, manifest.Name)
		if identityErr != nil {
			return Item{WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionInstall, Outcome: OutcomeFailed, ReasonCode: ReasonInvalidPackage}
		}
		return failedItem(Item{Target: identity, WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionInstall}, ReasonCollision)
	}
	if inspection.item.ReasonCode == ReasonMissingTarget && inspection.item.Outcome == OutcomeUnchanged {
		inspection.item.Action = ActionInstall
		inspection.item.Outcome = OutcomeChanged
		inspection.item.ReasonCode = ReasonChanged
	}
	if inspection.item.Outcome != OutcomeChanged {
		return inspection.item
	}
	identity := inspection.item.Target
	parent := filepath.Dir(identity)
	if err := r.revalidateTarget(target); err != nil {
		return failedItem(inspection.item, ReasonRootOutsideHome)
	}
	if err := r.fs.MkdirAll(parent, 0o755); err != nil {
		return failedItem(inspection.item, ReasonReceiptWriteFailed)
	}
	if err := r.revalidateTarget(target); err != nil {
		return failedItem(inspection.item, ReasonRootOutsideHome)
	}
	stage, err := r.stageProjection(manifest, parent)
	if err != nil {
		return failedItem(inspection.item, ReasonReceiptWriteFailed)
	}
	stageOwned := true
	cleanupStage := func() error {
		if !stageOwned {
			return nil
		}
		stageOwned = false
		if cleanupErr := r.fs.RemoveAll(stage); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			return cleanupErr
		}
		return nil
	}
	oldReceipt := inspection.receiptRaw
	backupContainer, backupPath := "", ""
	if inspection.item.Action == ActionUpdate {
		current, currentErr := r.inspectProjectionState(target, manifest)
		if currentErr != nil {
			_ = cleanupStage()
			return failedItem(inspection.item, ReasonDrift)
		}
		if current.item.Outcome != OutcomeChanged || current.item.Action != ActionUpdate || !bytes.Equal(current.receiptRaw, inspection.receiptRaw) {
			_ = cleanupStage()
			return failedItem(inspection.item, ReasonDrift)
		}
		backupContainer, backupPath, err = r.moveProjectionBackup(parent, identity)
		if err != nil {
			_ = cleanupStage()
			return failedItemWithBackup(inspection.item, retainedPath(r.fs, backupContainer), err)
		}
	}
	if err := r.revalidateTarget(target); err != nil {
		remnant, rollbackErr := r.restoreProjection(identity, backupPath, backupContainer, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, remnant, errors.Join(err, rollbackErr))
	}
	if inspection.item.Action == ActionInstall {
		if _, statErr := r.fs.Lstat(identity); statErr == nil {
			_ = cleanupStage()
			return failedItem(inspection.item, ReasonCollision)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			_ = cleanupStage()
			return failedItem(inspection.item, ReasonCollision)
		}
	}
	if err := r.fs.Rename(stage, identity); err != nil {
		remnant, rollbackErr := r.restoreProjection(identity, backupPath, backupContainer, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, remnant, errors.Join(err, rollbackErr))
	}
	stageOwned = false
	if err := r.fs.SyncDir(parent); err != nil {
		remnant, rollbackErr := r.restoreProjection(identity, backupPath, backupContainer, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, remnant, errors.Join(err, rollbackErr))
	}
	projectedHash, err := treeHash(r.fs, identity)
	if err != nil {
		remnant, rollbackErr := r.restoreProjection(identity, backupPath, backupContainer, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, remnant, errors.Join(err, rollbackErr))
	}
	if err := r.validateStatePath(); err != nil {
		remnant, rollbackErr := r.restoreProjection(identity, backupPath, backupContainer, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, remnant, errors.Join(err, rollbackErr))
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
	data, err := marshalReceipt(receipt)
	if err != nil {
		remnant, rollbackErr := r.restoreProjection(identity, backupPath, backupContainer, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, remnant, errors.Join(err, rollbackErr))
	}
	receiptResult, receiptErr := writeReceiptBytes(r.fs, r.paths.StateDir, ReceiptPath(r.paths.StateDir, identity), data)
	if receiptErr != nil {
		if receiptResult.committed {
			item := failedItem(inspection.item, ReasonDrift)
			item.BackupPath = receiptResult.backup
			return item
		}
		remnant, rollbackErr := r.restoreProjection(identity, backupPath, backupContainer, stage, ReceiptPath(r.paths.StateDir, identity), oldReceipt)
		return failedItemWithBackup(inspection.item, remnant, errors.Join(receiptErr, rollbackErr))
	}
	// Commit point: target and receipt are durable. Never roll either back.
	if backupContainer != "" {
		if err := r.fs.RemoveAll(backupContainer); err != nil {
			item := failedItem(inspection.item, ReasonDrift)
			item.BackupPath = backupContainer
			return item
		}
		if err := r.fs.SyncDir(parent); err != nil {
			return failedItem(inspection.item, ReasonDrift)
		}
	}
	item := inspection.item
	item.Outcome = OutcomeChanged
	item.ReasonCode = ReasonChanged
	item.BackupPath = ""
	return item
}

func (r *Reconciler) stageProjection(manifest builtin.PackageManifest, parent string) (string, error) {
	if !validPackageName(manifest.Name) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPackageName, manifest.Name)
	}
	stage, err := r.fs.MkdirTemp(parent, ".hotplex-stage-*")
	if err != nil {
		return "", err
	}
	cleanup := func(primary error) (string, error) {
		if cleanupErr := r.fs.RemoveAll(stage); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			primary = errors.Join(primary, cleanupErr)
		}
		return "", primary
	}
	for _, asset := range manifest.Assets {
		if !validAssetPath(asset.Path) {
			return cleanup(fmt.Errorf("invalid asset path %q", asset.Path))
		}
		data, err := r.registry.ReadFile(manifest.Name, asset.Path)
		if err != nil {
			return cleanup(err)
		}
		path := filepath.Join(stage, filepath.FromSlash(asset.Path))
		if err := r.fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return cleanup(err)
		}
		if err := r.fs.WriteFile(path, data, 0o644); err != nil {
			return cleanup(err)
		}
		if err := r.fs.SyncFile(path); err != nil {
			return cleanup(err)
		}
	}
	actualHash, err := treeHash(r.fs, stage)
	if err != nil {
		return cleanup(err)
	}
	expectedHash, err := r.expectedManifestTreeHash(manifest)
	if err != nil {
		return cleanup(err)
	}
	if actualHash != expectedHash {
		return cleanup(errors.New("projection tree hash mismatch"))
	}
	if err := r.fs.SyncDir(stage); err != nil {
		return cleanup(err)
	}
	return stage, nil
}

func (r *Reconciler) moveProjectionBackup(parent, target string) (container, backupPath string, retErr error) {
	container, err := r.fs.MkdirTemp(parent, ".hotplex-backup-*")
	if err != nil {
		return "", "", err
	}
	backupPath = filepath.Join(container, filepath.Base(target))
	if err := r.fs.Rename(target, backupPath); err != nil {
		return container, backupPath, errors.Join(err, r.fs.RemoveAll(container))
	}
	if err := r.fs.SyncDir(parent); err != nil {
		restoreErr := r.fs.Rename(backupPath, target)
		if restoreErr == nil {
			restoreErr = r.fs.SyncDir(parent)
		}
		cleanupErr := r.fs.RemoveAll(container)
		return container, backupPath, errors.Join(err, restoreErr, cleanupErr)
	}
	return container, backupPath, nil
}
