package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

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

func appendInventoryStatusItems(report *Report, statuses []inventoryStatus) {
	for _, status := range statuses {
		item := Item{Target: status.path, Action: ActionNone, Outcome: OutcomeUnchanged, ReasonCode: ReasonUnchanged}
		switch {
		case status.conflict || status.err != nil:
			item.Outcome = OutcomeConflict
			item.ReasonCode = status.reason
		case status.missing:
			item.Action = ActionInstall
			item.Outcome = OutcomeDrift
			item.ReasonCode = ReasonMissingTarget
		}
		report.Items = append(report.Items, item)
	}
}

func appendInventoryPlanItems(report *Report, statuses []inventoryStatus) {
	for _, status := range statuses {
		item := Item{Target: status.path, Action: ActionNone, Outcome: OutcomeUnchanged, ReasonCode: ReasonUnchanged}
		if status.missing {
			item.Action = ActionInstall
			item.Outcome = OutcomeChanged
			item.ReasonCode = ReasonChanged
		}
		report.Items = append(report.Items, item)
	}
}

func appendInventoryChangeItems(report *Report, statuses []inventoryStatus) {
	for _, status := range statuses {
		item := Item{Target: status.path, Action: ActionNone, Outcome: OutcomeUnchanged, ReasonCode: ReasonUnchanged}
		if status.missing {
			item.Action = ActionInstall
			item.Outcome = OutcomeChanged
			item.ReasonCode = ReasonChanged
		}
		report.Items = append(report.Items, item)
	}
}

func appendInventoryBlockedItems(report *Report, targets []Target, manifests []builtin.PackageManifest) {
	for _, target := range targets {
		if target.ReasonCode == ReasonUnsupportedWorker {
			continue
		}
		for _, manifest := range manifests {
			identity, err := PackageTargetIdentity(target.CanonicalRoot, manifest.Name)
			if err != nil {
				report.Items = append(report.Items, Item{
					Target:        target.CanonicalRoot,
					WorkerAliases: cloneWorkerAliases(target.WorkerAliases),
					Action:        ActionInstall,
					Outcome:       OutcomeFailed,
					ReasonCode:    ReasonInvalidPackage,
				})
				continue
			}
			report.Items = append(report.Items, Item{
				Target:        identity,
				WorkerAliases: cloneWorkerAliases(target.WorkerAliases),
				Action:        ActionInstall,
				Outcome:       OutcomeConflict,
				ReasonCode:    ReasonInventoryBlocked,
			})
		}
	}
}

func appendInventoryConflictItems(report *Report, statuses []inventoryStatus) {
	for _, status := range statuses {
		if status.conflict || status.err != nil {
			report.Items = append(report.Items, Item{Target: status.path, Action: ActionNone, Outcome: OutcomeConflict, ReasonCode: status.reason})
		}
	}
}

func appendInventoryPublicationFailureItems(report *Report, before, after []inventoryStatus) {
	afterByPath := make(map[string]inventoryStatus, len(after))
	for _, status := range after {
		afterByPath[status.path] = status
	}
	for _, previous := range before {
		current, ok := afterByPath[previous.path]
		item := Item{Target: previous.path, Action: ActionInstall, Outcome: OutcomeFailed, ReasonCode: ReasonInventoryBlocked}
		if ok {
			switch {
			case current.conflict || current.err != nil:
				item.ReasonCode = current.reason
			case previous.missing && !current.missing:
				item.Outcome = OutcomeChanged
				item.ReasonCode = ReasonChanged
			case !previous.missing && !current.missing:
				item.Action = ActionNone
				item.Outcome = OutcomeUnchanged
				item.ReasonCode = ReasonUnchanged
			}
		}
		report.Items = append(report.Items, item)
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
	if err := r.validateInventoryTarget(parent); err != nil {
		return err
	}
	if err := r.fs.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if err := r.validateInventoryTarget(parent); err != nil {
		return err
	}
	stage, err := r.fs.MkdirTemp(parent, ".hotplex-inventory-stage-*")
	if err != nil {
		return err
	}
	owned := true
	defer func() {
		if owned {
			if cleanupErr := r.fs.RemoveAll(stage); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
				retErr = errors.Join(retErr, cleanupErr)
			}
		}
	}()
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
	if err := r.validateInventoryTarget(parent); err != nil {
		return err
	}
	if err := r.fs.Rename(stage, target); err != nil {
		return err
	}
	owned = false
	return r.fs.SyncDir(parent)
}

func (r *Reconciler) validateInventoryTarget(parent string) error {
	canonical, err := canonicalFSPath(r.fs, parent)
	if err != nil {
		return err
	}
	if !isWithin(r.paths.HotplexHome, canonical) || canonical != parent {
		return ErrInventoryOutsideHotplexHome
	}
	return nil
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
