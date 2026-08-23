package reconcile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

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
	owned      bool
	treeHash   string
}

func (r *Reconciler) inspectProjection(target Target, manifest builtin.PackageManifest) (Item, error) {
	inspection, err := r.inspectProjectionState(target, manifest)
	return inspection.item, err
}

func (r *Reconciler) inspectProjectionState(target Target, manifest builtin.PackageManifest) (projectionInspection, error) {
	identity, err := PackageTargetIdentity(target.CanonicalRoot, manifest.Name)
	if err != nil {
		return projectionInspection{item: Item{WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionInstall, Outcome: OutcomeFailed, ReasonCode: ReasonInvalidPackage}}, err
	}
	item := Item{Target: identity, WorkerAliases: cloneWorkerAliases(target.WorkerAliases), Action: ActionInstall, Outcome: OutcomeChanged, ReasonCode: ReasonChanged}
	receiptPath := ReceiptPath(r.paths.StateDir, identity)
	receiptRaw, rawErr := r.fs.ReadFile(receiptPath)
	receipt, receiptErr := parseReceipt(receiptRaw)
	if errors.Is(rawErr, os.ErrNotExist) {
		receiptRaw = nil
		receiptErr = os.ErrNotExist
	} else if rawErr != nil {
		receiptErr = rawErr
	}
	info, lstatErr := r.fs.Lstat(identity)
	if errors.Is(lstatErr, os.ErrNotExist) {
		if errors.Is(receiptErr, ErrInvalidReceipt) {
			item.Outcome = OutcomeConflict
			item.ReasonCode = ReasonInvalidReceipt
		} else if rawErr == nil || receiptErr == nil {
			item.Outcome = OutcomeDrift
			item.ReasonCode = ReasonDrift
		} else {
			item.Action = ActionNone
			item.Outcome = OutcomeUnchanged
			item.ReasonCode = ReasonMissingTarget
		}
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw}, nil
	}
	if lstatErr != nil {
		return projectionInspection{}, lstatErr
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		item.Outcome = OutcomeConflict
		item.ReasonCode = ReasonCollision
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw}, nil
	}
	actualHash, hashErr := treeHash(r.fs, identity)
	if hashErr != nil {
		item.Outcome = OutcomeConflict
		item.ReasonCode = ReasonCollision
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw}, nil
	}
	if receiptErr != nil {
		item.Outcome = OutcomeConflict
		item.ReasonCode = ReasonInvalidReceipt
		if errors.Is(receiptErr, os.ErrNotExist) {
			item.ReasonCode = ReasonMissingReceipt
		}
		return projectionInspection{item: item, receiptRaw: receiptRaw, treeHash: actualHash}, nil
	}
	if receipt.CanonicalTarget != identity || receipt.PackageName != manifest.Name || receipt.Profile != manifest.Profile || !equalAliases(receipt.WorkerAliases, target.WorkerAliases) {
		item.Outcome = OutcomeConflict
		item.ReasonCode = ReasonInvalidReceipt
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, treeHash: actualHash}, nil
	}
	if receipt.ProjectedTreeSHA256 != actualHash {
		item.Action = ActionUpdate
		item.Outcome = OutcomeDrift
		item.ReasonCode = ReasonDrift
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, treeHash: actualHash}, nil
	}
	owned := true
	if receiptMatches(receipt, manifest, identity, target.WorkerAliases, actualHash) {
		item.Action = ActionNone
		item.Outcome = OutcomeUnchanged
		item.ReasonCode = ReasonUnchanged
		return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, owned: owned, treeHash: actualHash}, nil
	}
	item.Action = ActionUpdate
	item.Outcome = OutcomeChanged
	item.ReasonCode = ReasonChanged
	return projectionInspection{item: item, receipt: receipt, receiptRaw: receiptRaw, owned: owned, treeHash: actualHash}, nil
}
