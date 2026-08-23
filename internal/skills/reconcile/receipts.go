package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

const receiptSchemaVersion = 1

type Receipt struct {
	SchemaVersion       int             `json:"schema_version"`
	PackageVersion      string          `json:"package_version"`
	PackageName         string          `json:"package_name"`
	Profile             builtin.Profile `json:"profile"`
	CanonicalTarget     string          `json:"canonical_target"`
	WorkerAliases       []WorkerType    `json:"worker_aliases"`
	ManifestSHA256      string          `json:"manifest_sha256"`
	ProjectedTreeSHA256 string          `json:"projected_tree_sha256"`
}

var receiptSequence atomic.Uint64

func ReceiptPath(stateDir, canonicalTarget string) string {
	target := filepath.Clean(canonicalTarget)
	if abs, err := filepath.Abs(target); err == nil {
		target = abs
	}
	// Canonicalize the approved root/parent, but never follow the package leaf:
	// a package target that is a symlink must remain a conflict and must not
	// redirect receipt lookup to user-controlled content.
	if canonicalParent, err := canonicalOSPath(filepath.Dir(target)); err == nil {
		target = filepath.Join(canonicalParent, filepath.Base(target))
	}
	sum := sha256.Sum256([]byte(target))
	return filepath.Join(stateDir, hex.EncodeToString(sum[:])+".json")
}

func manifestHash(manifest builtin.PackageManifest) string {
	assets := append([]builtin.AssetManifest(nil), manifest.Assets...)
	sort.Slice(assets, func(i, j int) bool {
		if assets[i].Path != assets[j].Path {
			return assets[i].Path < assets[j].Path
		}
		if assets[i].Size != assets[j].Size {
			return assets[i].Size < assets[j].Size
		}
		return assets[i].SHA256 < assets[j].SHA256
	})
	h := sha256.New()
	_, _ = h.Write([]byte(manifest.Name))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(manifest.Version))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(manifest.Profile))
	_, _ = h.Write([]byte{0})
	for _, asset := range assets {
		_, _ = h.Write([]byte(asset.Path))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(strconv.FormatInt(asset.Size, 10)))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(asset.SHA256))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

var errSymlinkInTree = errors.New("skills: symlink in managed package tree")

func treeHash(fs FileSystem, root string) (string, error) {
	info, err := fs.Lstat(root)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errSymlinkInTree
	}
	if !info.IsDir() {
		return "", fmt.Errorf("skills: tree root is not a directory")
	}
	h := sha256.New()
	if err := hashTreeFiles(fs, root, "", h); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashTreeFiles(fs FileSystem, root, relative string, h interface{ Write([]byte) (int, error) }) error {
	directory := root
	if relative != "" {
		directory = filepath.Join(root, relative)
	}
	entries, err := fs.ReadDir(directory)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		rel := entry.Name()
		if relative != "" {
			rel = filepath.Join(relative, entry.Name())
		}
		full := filepath.Join(root, rel)
		info, lstatErr := fs.Lstat(full)
		if lstatErr != nil {
			return lstatErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errSymlinkInTree
		}
		if info.IsDir() {
			if err := hashTreeFiles(fs, root, rel, h); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("skills: unsupported file type at %s", rel)
		}
		data, readErr := fs.ReadFile(full)
		if readErr != nil {
			return readErr
		}
		_, _ = h.Write([]byte(filepath.ToSlash(rel)))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(strconv.FormatInt(info.Size(), 10)))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return nil
}

func readReceipt(fs FileSystem, path string) (Receipt, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", ErrInvalidReceipt, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Receipt{}, ErrInvalidReceipt
		}
		return Receipt{}, fmt.Errorf("%w: %w", ErrInvalidReceipt, err)
	}
	if receipt.SchemaVersion != receiptSchemaVersion || receipt.PackageVersion == "" ||
		receipt.PackageName == "" || receipt.CanonicalTarget == "" ||
		receipt.ManifestSHA256 == "" || receipt.ProjectedTreeSHA256 == "" {
		return Receipt{}, ErrInvalidReceipt
	}
	if receipt.Profile != builtin.ProfileRuntime && receipt.Profile != builtin.ProfileOperator {
		return Receipt{}, ErrInvalidReceipt
	}
	if !validPackageName(receipt.PackageName) || !sortedAliases(receipt.WorkerAliases) {
		return Receipt{}, ErrInvalidReceipt
	}
	return receipt, nil
}

func sortedAliases(aliases []WorkerType) bool {
	if len(aliases) == 0 {
		return true
	}
	copyAliases := cloneWorkerAliases(aliases)
	sort.Slice(copyAliases, func(i, j int) bool { return copyAliases[i] < copyAliases[j] })
	for i := range aliases {
		if copyAliases[i] != aliases[i] {
			return false
		}
		if aliases[i] != WorkerClaude && aliases[i] != WorkerCodex && aliases[i] != WorkerOpenCode && aliases[i] != WorkerACP {
			return false
		}
	}
	return true
}

func writeReceipt(fs FileSystem, stateDir, target string, receipt Receipt) (retErr error) {
	if err := fs.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	finalPath := ReceiptPath(stateDir, target)
	tempPath := filepath.Join(stateDir, fmt.Sprintf(".%s.tmp-%d", filepath.Base(finalPath), receiptSequence.Add(1)))
	ownedTemp := true
	defer func() {
		if ownedTemp {
			if cleanupErr := fs.Remove(tempPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
				retErr = errors.Join(retErr, cleanupErr)
			}
		}
	}()
	if err := fs.WriteFile(tempPath, data, 0o600); err != nil {
		return fmt.Errorf("%w: %w", ErrReceiptWriteFailed, err)
	}
	if err := fs.SyncFile(tempPath); err != nil {
		return fmt.Errorf("%w: %w", ErrReceiptWriteFailed, err)
	}
	if err := atomicReplace(fs, tempPath, finalPath); err != nil {
		return fmt.Errorf("%w: %w", ErrReceiptWriteFailed, err)
	}
	ownedTemp = false
	if err := fs.SyncDir(stateDir); err != nil {
		return fmt.Errorf("%w: %w", ErrReceiptWriteFailed, err)
	}
	return nil
}

// atomicReplace handles POSIX rename-overwrite and Windows' two-step replace
// semantics without deleting a pre-existing, unrelated sibling backup.
func atomicReplace(fs FileSystem, tempPath, finalPath string) error {
	if err := fs.Rename(tempPath, finalPath); err == nil {
		return nil
	} else {
		firstErr := err
		if _, statErr := fs.Lstat(finalPath); statErr != nil {
			return firstErr
		}
		backup := uniqueSibling(fs, finalPath, ".hotplex-receipt-backup")
		if err := fs.Rename(finalPath, backup); err != nil {
			return errors.Join(firstErr, err)
		}
		if err := fs.Rename(tempPath, finalPath); err != nil {
			restoreErr := fs.Rename(backup, finalPath)
			return errors.Join(firstErr, err, restoreErr)
		}
		if err := fs.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Join(firstErr, err)
		}
		return nil
	}
}

func receiptMatches(receipt Receipt, manifest builtin.PackageManifest, target string, aliases []WorkerType, projectedHash string) bool {
	return receipt.SchemaVersion == receiptSchemaVersion &&
		receipt.PackageVersion == manifest.Version &&
		receipt.PackageName == manifest.Name &&
		receipt.Profile == manifest.Profile &&
		receipt.CanonicalTarget == target &&
		equalAliases(receipt.WorkerAliases, aliases) &&
		receipt.ManifestSHA256 == manifestHash(manifest) &&
		receipt.ProjectedTreeSHA256 == projectedHash
}

func equalAliases(left, right []WorkerType) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
