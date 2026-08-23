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
	return parseReceipt(data)
}

func parseReceipt(data []byte) (Receipt, error) {
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
	if !validReceipt(receipt) {
		return Receipt{}, ErrInvalidReceipt
	}
	return receipt, nil
}

func validReceipt(receipt Receipt) bool {
	return receipt.SchemaVersion == receiptSchemaVersion &&
		validPackageVersion(receipt.PackageVersion) &&
		validPackageName(receipt.PackageName) &&
		filepath.IsAbs(receipt.CanonicalTarget) &&
		filepath.Clean(receipt.CanonicalTarget) == receipt.CanonicalTarget &&
		validSHA256Hex(receipt.ManifestSHA256) &&
		validSHA256Hex(receipt.ProjectedTreeSHA256) &&
		(receipt.Profile == builtin.ProfileRuntime || receipt.Profile == builtin.ProfileOperator) &&
		validReceiptAliases(receipt.WorkerAliases)
}

func validPackageVersion(version string) bool {
	return len(version) == len("v1-")+64 && strings.HasPrefix(version, "v1-") && validLowerHex(version[3:])
}

func validSHA256Hex(value string) bool {
	return len(value) == 64 && validLowerHex(value)
}

func validLowerHex(value string) bool {
	for i := 0; i < len(value); i++ {
		char := value[i]
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validReceiptAliases(aliases []WorkerType) bool {
	if len(aliases) == 1 {
		return aliases[0] == WorkerClaude
	}
	return len(aliases) == 2 && aliases[0] == WorkerCodex && aliases[1] == WorkerOpenCode
}

type receiptWriteResult struct {
	committed bool
	backup    string
}

func writeReceipt(fs FileSystem, stateDir, target string, receipt Receipt) error {
	if !validReceipt(receipt) {
		return ErrInvalidReceipt
	}
	data, err := marshalReceipt(receipt)
	if err != nil {
		return err
	}
	finalPath := ReceiptPath(stateDir, target)
	_, err = writeReceiptBytes(fs, stateDir, finalPath, data)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReceiptWriteFailed, err)
	}
	return nil
}

func marshalReceipt(receipt Receipt) ([]byte, error) {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writeReceiptBytes(fs FileSystem, stateDir, finalPath string, data []byte) (result receiptWriteResult, retErr error) {
	if err := fs.MkdirAll(stateDir, 0o755); err != nil {
		return result, err
	}
	tempPath, err := fs.CreateTemp(stateDir, ".hotplex-receipt-*", data, 0o600)
	if err != nil {
		return result, err
	}
	tempOwned := true
	defer func() {
		if tempOwned {
			if cleanupErr := fs.Remove(tempPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
				retErr = errors.Join(retErr, cleanupErr)
			}
		}
	}()
	if err := fs.SyncFile(tempPath); err != nil {
		return result, err
	}
	oldExists := false
	if info, statErr := fs.Lstat(finalPath); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return result, ErrInvalidReceipt
		}
		oldExists = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return result, statErr
	}
	backupContainer := ""
	backupPath := ""
	if oldExists {
		backupContainer, err = fs.MkdirTemp(stateDir, ".hotplex-receipt-backup-*")
		if err != nil {
			return result, err
		}
		backupPath = filepath.Join(backupContainer, "receipt.json")
		if err := fs.Rename(finalPath, backupPath); err != nil {
			cleanupErr := fs.RemoveAll(backupContainer)
			return result, errors.Join(err, cleanupErr)
		}
		if err := fs.SyncDir(stateDir); err != nil {
			restoreErr := fs.Rename(backupPath, finalPath)
			if restoreErr == nil {
				restoreErr = fs.SyncDir(stateDir)
			}
			cleanupErr := fs.RemoveAll(backupContainer)
			return result, errors.Join(err, restoreErr, cleanupErr)
		}
	}
	if err := fs.Rename(tempPath, finalPath); err != nil {
		var restoreErr error
		if oldExists {
			restoreErr = fs.Rename(backupPath, finalPath)
			if restoreErr == nil {
				restoreErr = fs.SyncDir(stateDir)
			}
		}
		cleanupErr := error(nil)
		if backupContainer != "" {
			cleanupErr = fs.RemoveAll(backupContainer)
		}
		return result, errors.Join(err, restoreErr, cleanupErr)
	}
	tempOwned = false
	if err := fs.SyncDir(stateDir); err != nil {
		removeErr := fs.Remove(finalPath)
		var restoreErr error
		if oldExists {
			restoreErr = fs.Rename(backupPath, finalPath)
		}
		syncErr := fs.SyncDir(stateDir)
		cleanupErr := error(nil)
		if backupContainer != "" {
			cleanupErr = fs.RemoveAll(backupContainer)
		}
		return result, errors.Join(err, removeErr, restoreErr, syncErr, cleanupErr)
	}
	result.committed = true
	result.backup = backupContainer
	if backupContainer != "" {
		if err := fs.RemoveAll(backupContainer); err != nil {
			result.backup = retainedPath(fs, backupContainer)
			return result, err
		}
		result.backup = ""
		if err := fs.SyncDir(stateDir); err != nil {
			return result, err
		}
	}
	return result, nil
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
