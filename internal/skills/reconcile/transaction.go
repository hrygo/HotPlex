package reconcile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

// restoreProjection is used only before the sync commit point. A receipt is
// restored only after the old target has been restored (or, for an install,
// after the new target has been removed), preventing a receipt-without-target
// state when rollback itself is interrupted.
func (r *Reconciler) restoreProjection(identity, backupPath, backupContainer, stage, receiptPath string, oldReceipt []byte) (string, error) {
	var errs []error
	targetRestored := false
	if info, err := r.fs.Lstat(identity); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			errs = append(errs, errors.New("managed target became non-directory"))
		} else if removeErr := r.fs.RemoveAll(identity); removeErr != nil {
			errs = append(errs, removeErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		if backupPath != "" {
			if _, err := r.fs.Lstat(backupPath); err == nil {
				backupOK := false
				if receipt, receiptErr := parseReceipt(oldReceipt); receiptErr == nil {
					if actualHash, hashErr := treeHash(r.fs, backupPath); hashErr == nil && actualHash == receipt.ProjectedTreeSHA256 {
						backupOK = true
					}
				}
				if !backupOK {
					errs = append(errs, errors.New("backup tree hash mismatch"))
				} else if err := r.fs.Rename(backupPath, identity); err != nil {
					errs = append(errs, err)
				} else if err := r.fs.SyncDir(filepath.Dir(identity)); err != nil {
					errs = append(errs, err)
				} else {
					targetRestored = true
				}
			} else if errors.Is(err, os.ErrNotExist) {
				errs = append(errs, os.ErrNotExist)
			} else {
				errs = append(errs, err)
			}
		} else {
			targetRestored = true
		}
	}
	if len(errs) == 0 && targetRestored {
		if len(oldReceipt) > 0 {
			if err := writeRawReceipt(r.fs, filepath.Dir(receiptPath), receiptPath, oldReceipt); err != nil {
				errs = append(errs, err)
			}
		} else if err := r.fs.Remove(receiptPath); err != nil && !errors.Is(err, os.ErrNotExist) {
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
	remnant := ""
	if backupContainer != "" {
		if err := r.fs.RemoveAll(backupContainer); err != nil && !errors.Is(err, os.ErrNotExist) {
			remnant = backupContainer
			errs = append(errs, err)
		}
	}
	return remnant, errors.Join(errs...)
}

func writeRawReceipt(fs FileSystem, stateDir, finalPath string, data []byte) error {
	_, err := writeReceiptBytes(fs, stateDir, finalPath, data)
	return err
}

func (r *Reconciler) removeProjection(target Target, manifest builtin.PackageManifest) Item {
	inspection, err := r.inspectProjectionState(target, manifest)
	if err != nil {
		identity, identityErr := PackageTargetIdentity(target.CanonicalRoot, manifest.Name)
		if identityErr != nil {
			return Item{WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionRemove, Outcome: OutcomeFailed, ReasonCode: ReasonInvalidPackage}
		}
		return failedItem(Item{Target: identity, WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionRemove}, ReasonCollision)
	}
	item := inspection.item
	item.Action = ActionRemove
	if inspection.item.ReasonCode == ReasonMissingTarget && inspection.item.Outcome == OutcomeUnchanged {
		item.Action = ActionNone
		return item
	}
	if inspection.item.Outcome != OutcomeUnchanged {
		return item
	}
	identity := item.Target
	receiptPath := ReceiptPath(r.paths.StateDir, identity)
	parent := filepath.Dir(identity)
	if err := r.revalidateTarget(target); err != nil {
		return failedItem(item, ReasonRootOutsideHome)
	}
	current, currentErr := r.inspectProjectionState(target, manifest)
	if currentErr != nil || current.item.Outcome != OutcomeUnchanged || !bytes.Equal(current.receiptRaw, inspection.receiptRaw) {
		return failedItem(item, ReasonDrift)
	}
	backupContainer, backupPath, err := r.moveProjectionBackup(parent, identity)
	if err != nil {
		return failedItemWithBackup(item, retainedPath(r.fs, backupContainer), err)
	}
	if err := r.revalidateTarget(target); err != nil {
		remnant, rollbackErr := r.restoreRemoved(identity, backupPath, backupContainer, receiptPath, inspection.receiptRaw, "", "")
		return failedItemWithBackup(item, remnant, errors.Join(err, rollbackErr))
	}
	if err := r.validateStatePath(); err != nil {
		remnant, rollbackErr := r.restoreRemoved(identity, backupPath, backupContainer, receiptPath, inspection.receiptRaw, "", "")
		return failedItemWithBackup(item, remnant, errors.Join(err, rollbackErr))
	}
	tombContainer, tombPath, err := r.moveReceiptToTombstone(receiptPath)
	if err != nil {
		remnant, rollbackErr := r.restoreRemoved(identity, backupPath, backupContainer, receiptPath, inspection.receiptRaw, tombContainer, tombPath)
		return failedItemWithBackup(item, remnant, errors.Join(err, rollbackErr))
	}
	if err := r.fs.SyncDir(r.paths.StateDir); err != nil {
		remnant, rollbackErr := r.restoreRemoved(identity, backupPath, backupContainer, receiptPath, inspection.receiptRaw, tombContainer, tombPath)
		return failedItemWithBackup(item, remnant, errors.Join(err, rollbackErr))
	}
	if err := r.fs.Remove(tombPath); err != nil {
		remnant, rollbackErr := r.restoreRemoved(identity, backupPath, backupContainer, receiptPath, inspection.receiptRaw, tombContainer, tombPath)
		return failedItemWithBackup(item, remnant, errors.Join(err, rollbackErr))
	}
	if err := r.fs.RemoveAll(tombContainer); err != nil {
		remnant, rollbackErr := r.restoreRemoved(identity, backupPath, backupContainer, receiptPath, inspection.receiptRaw, tombContainer, tombPath)
		return failedItemWithBackup(item, remnant, errors.Join(err, rollbackErr))
	}
	if err := r.fs.SyncDir(r.paths.StateDir); err != nil {
		// The receipt removal is ambiguous but the complete backup still gives
		// us a safe pre-commit rollback path.
		remnant, rollbackErr := r.restoreRemoved(identity, backupPath, backupContainer, receiptPath, inspection.receiptRaw, "", "")
		return failedItemWithBackup(item, remnant, errors.Join(err, rollbackErr))
	}
	// Commit point: target and receipt are durably absent. From here on a
	// failure must not recreate a receipt without a target.
	if err := r.fs.RemoveAll(backupContainer); err != nil {
		if restoreErr := r.restoreRemovedAfterCommit(identity, backupPath, backupContainer, receiptPath, inspection.receiptRaw); restoreErr == nil {
			return failedItem(item, ReasonDrift)
		}
		item = failedItem(item, ReasonDrift)
		item.BackupPath = backupContainer
		return item
	}
	if err := r.fs.SyncDir(parent); err != nil {
		return failedItem(item, ReasonDrift)
	}
	item.Outcome = OutcomeChanged
	item.ReasonCode = ReasonChanged
	item.BackupPath = ""
	return item
}

func retainedPath(fs FileSystem, path string) string {
	if path == "" {
		return ""
	}
	if _, err := fs.Lstat(path); err == nil {
		return path
	}
	return ""
}

func (r *Reconciler) moveReceiptToTombstone(receiptPath string) (container, tombPath string, retErr error) {
	stateDir := filepath.Dir(receiptPath)
	container, err := r.fs.MkdirTemp(stateDir, ".hotplex-tombstone-*")
	if err != nil {
		return "", "", err
	}
	tombPath = filepath.Join(container, "receipt.json")
	if err := r.fs.Rename(receiptPath, tombPath); err != nil {
		return container, tombPath, errors.Join(err, r.fs.RemoveAll(container))
	}
	return container, tombPath, nil
}

func (r *Reconciler) restoreRemoved(identity, backupPath, backupContainer, receiptPath string, receiptRaw []byte, tombContainer, tombPath string) (string, error) {
	var errs []error
	if tombPath != "" {
		if _, err := r.fs.Lstat(tombPath); err == nil {
			if err := r.fs.Rename(tombPath, receiptPath); err != nil {
				errs = append(errs, err)
			} else if err := r.fs.SyncDir(filepath.Dir(receiptPath)); err != nil {
				errs = append(errs, err)
			}
		}
	}
	targetRestored := false
	if info, err := r.fs.Lstat(identity); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			errs = append(errs, errors.New("managed target became non-directory"))
		} else if receipt, receiptErr := parseReceipt(receiptRaw); receiptErr != nil {
			errs = append(errs, receiptErr)
		} else if actualHash, hashErr := treeHash(r.fs, identity); hashErr != nil || actualHash != receipt.ProjectedTreeSHA256 {
			if hashErr != nil {
				errs = append(errs, hashErr)
			} else {
				errs = append(errs, errors.New("managed target hash mismatch"))
			}
		} else {
			targetRestored = true
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if _, backupErr := r.fs.Lstat(backupPath); backupErr == nil {
			backupOK := false
			if receipt, receiptErr := parseReceipt(receiptRaw); receiptErr == nil {
				if actualHash, hashErr := treeHash(r.fs, backupPath); hashErr == nil && actualHash == receipt.ProjectedTreeSHA256 {
					backupOK = true
				}
			}
			if !backupOK {
				errs = append(errs, errors.New("backup tree hash mismatch"))
			} else if err := r.fs.Rename(backupPath, identity); err != nil {
				errs = append(errs, err)
			} else if err := r.fs.SyncDir(filepath.Dir(identity)); err != nil {
				errs = append(errs, err)
			} else {
				targetRestored = true
			}
		} else {
			errs = append(errs, backupErr)
		}
	} else {
		errs = append(errs, err)
	}
	if targetRestored && len(receiptRaw) > 0 {
		if _, err := r.fs.Lstat(receiptPath); errors.Is(err, os.ErrNotExist) {
			if err := writeRawReceipt(r.fs, filepath.Dir(receiptPath), receiptPath, receiptRaw); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if tombContainer != "" {
		if err := r.fs.RemoveAll(tombContainer); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	remnant := ""
	if backupContainer != "" {
		if err := r.fs.RemoveAll(backupContainer); err != nil && !errors.Is(err, os.ErrNotExist) {
			remnant = backupContainer
			errs = append(errs, err)
		}
	}
	return remnant, errors.Join(errs...)
}

func (r *Reconciler) restoreRemovedAfterCommit(identity, backupPath, backupContainer, receiptPath string, receiptRaw []byte) error {
	if _, err := r.fs.Lstat(backupPath); err != nil {
		return err
	}
	receipt, err := parseReceipt(receiptRaw)
	if err != nil {
		return err
	}
	actualHash, err := treeHash(r.fs, backupPath)
	if err != nil {
		return err
	}
	if actualHash != receipt.ProjectedTreeSHA256 {
		return errors.New("backup tree hash mismatch")
	}
	if _, err := r.fs.Lstat(backupPath); err != nil {
		return err
	}
	if err := r.fs.Rename(backupPath, identity); err != nil {
		return err
	}
	if err := r.fs.SyncDir(filepath.Dir(identity)); err != nil {
		return err
	}
	if err := writeRawReceipt(r.fs, filepath.Dir(receiptPath), receiptPath, receiptRaw); err != nil {
		return err
	}
	if err := r.fs.RemoveAll(backupContainer); err != nil {
		return err
	}
	return nil
}
