package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type generatorConfig struct {
	canonicalRoot  string
	manifestOutput string
	mirrorRoot     string
}

type packageSpec struct {
	name    string
	profile string
}

type assetRecord struct {
	path   string
	size   int64
	sha256 string
}

var packageSpecs = []packageSpec{
	{name: "hotplex-cli", profile: "runtime"},
	{name: "hotplex-operator", profile: "operator"},
}

var expectedPackageAssets = map[string][]string{
	"hotplex-cli": {
		"SKILL.md",
		"references/cli-surface.generated.md",
		"references/cron.md",
		"references/diagnostics.md",
		"references/slack.md",
	},
	"hotplex-operator": {
		"SKILL.md",
		"references/admin-audit.md",
		"references/configuration.md",
		"references/initialization.md",
		"references/install-update.md",
		"references/service-lifecycle.md",
	},
}

func main() {
	var config generatorConfig
	flag.StringVar(&config.canonicalRoot, "canonical", "internal/skills/builtin", "canonical package root")
	flag.StringVar(&config.manifestOutput, "manifest-output", "internal/skills/builtin/manifest.generated.go", "generated manifest path")
	flag.StringVar(&config.mirrorRoot, "mirror", ".agents/skills", "repository skill mirror parent")
	flag.Parse()

	if err := generate(config); err != nil {
		fmt.Fprintln(os.Stderr, "generate built-in skills:", err)
		os.Exit(1)
	}
}

func generate(config generatorConfig) error {
	canonicalRoot, err := filepath.Abs(config.canonicalRoot)
	if err != nil {
		return fmt.Errorf("resolve canonical root: %w", err)
	}
	manifestOutput, err := filepath.Abs(config.manifestOutput)
	if err != nil {
		return fmt.Errorf("resolve manifest output: %w", err)
	}
	mirrorRoot, err := filepath.Abs(config.mirrorRoot)
	if err != nil {
		return fmt.Errorf("resolve mirror root: %w", err)
	}
	if err := validateCanonicalDirectories(canonicalRoot); err != nil {
		return err
	}

	manifests := make([]generatedPackage, 0, len(packageSpecs))
	for _, spec := range packageSpecs {
		assets, err := scanPackage(filepath.Join(canonicalRoot, spec.name))
		if err != nil {
			return fmt.Errorf("scan %s: %w", spec.name, err)
		}
		if err := validatePackageAssets(spec.name, assets); err != nil {
			return err
		}
		manifests = append(manifests, generatedPackage{
			name:    spec.name,
			profile: spec.profile,
			assets:  assets,
			version: packageVersion(assets),
		})
	}
	if err := writeAtomic(manifestOutput, renderManifest(manifests)); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	for _, spec := range packageSpecs {
		sourceRoot := filepath.Join(canonicalRoot, spec.name)
		targetRoot := filepath.Join(mirrorRoot, spec.name)
		if err := mirrorPackage(sourceRoot, targetRoot); err != nil {
			return fmt.Errorf("mirror %s: %w", spec.name, err)
		}
	}
	return nil
}

func validateCanonicalDirectories(canonicalRoot string) error {
	entries, err := os.ReadDir(canonicalRoot)
	if err != nil {
		return fmt.Errorf("read canonical root: %w", err)
	}
	allowed := make(map[string]struct{}, len(packageSpecs))
	for _, spec := range packageSpecs {
		allowed[spec.name] = struct{}{}
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, ok := allowed[entry.Name()]; !ok {
			return fmt.Errorf("unexpected canonical package directory %q", entry.Name())
		}
	}
	return nil
}

func validatePackageAssets(packageName string, assets []assetRecord) error {
	expected, ok := expectedPackageAssets[packageName]
	if !ok {
		return fmt.Errorf("package %s has no canonical asset policy", packageName)
	}
	expected = append([]string(nil), expected...)
	actual := make([]string, 0, len(assets))
	for _, asset := range assets {
		actual = append(actual, asset.path)
	}
	sort.Strings(expected)
	sort.Strings(actual)
	if len(expected) != len(actual) {
		return fmt.Errorf("package %s has %d assets, want %d", packageName, len(actual), len(expected))
	}
	for index := range expected {
		if expected[index] != actual[index] {
			return fmt.Errorf("package %s asset %q does not match canonical asset %q", packageName, actual[index], expected[index])
		}
	}
	return nil
}

type generatedPackage struct {
	name    string
	profile string
	assets  []assetRecord
	version string
}

func scanPackage(packageRoot string) ([]assetRecord, error) {
	info, err := os.Stat(packageRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory")
	}

	var assets []assetRecord
	err = filepath.WalkDir(packageRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed: %s", filePath)
		}
		relativePath, err := filepath.Rel(packageRoot, filePath)
		if err != nil {
			return err
		}
		relativePath = filepath.ToSlash(relativePath)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		assets = append(assets, assetRecord{
			path:   relativePath,
			size:   int64(len(data)),
			sha256: hex.EncodeToString(sum[:]),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("package has no files")
	}
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].path < assets[j].path
	})
	return assets, nil
}

func packageVersion(assets []assetRecord) string {
	sortedAssets := append([]assetRecord(nil), assets...)
	sort.Slice(sortedAssets, func(i, j int) bool {
		if sortedAssets[i].path != sortedAssets[j].path {
			return sortedAssets[i].path < sortedAssets[j].path
		}
		if sortedAssets[i].size != sortedAssets[j].size {
			return sortedAssets[i].size < sortedAssets[j].size
		}
		return sortedAssets[i].sha256 < sortedAssets[j].sha256
	})

	hasher := sha256.New()
	for _, asset := range sortedAssets {
		_, _ = hasher.Write([]byte(asset.path))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(strconv.FormatInt(asset.size, 10)))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(asset.sha256))
		_, _ = hasher.Write([]byte{0})
	}
	return "v1-" + hex.EncodeToString(hasher.Sum(nil))
}

func renderManifest(packages []generatedPackage) []byte {
	var out strings.Builder
	out.WriteString("package builtin\n\n")
	out.WriteString("var generatedManifests = []PackageManifest{\n")
	for _, pkg := range packages {
		out.WriteString("\t{\n")
		out.WriteString("\t\tName:    ")
		out.WriteString(strconv.Quote(pkg.name))
		out.WriteString(",\n\t\tVersion: ")
		out.WriteString(strconv.Quote(pkg.version))
		out.WriteString(",\n\t\tProfile: Profile")
		if pkg.profile == "runtime" {
			out.WriteString("Runtime")
		} else {
			out.WriteString("Operator")
		}
		out.WriteString(",\n\t\tAssets: []AssetManifest{\n")
		for _, asset := range pkg.assets {
			out.WriteString("\t\t\t{Path: ")
			out.WriteString(strconv.Quote(asset.path))
			out.WriteString(", Size: ")
			out.WriteString(strconv.FormatInt(asset.size, 10))
			out.WriteString(", SHA256: ")
			out.WriteString(strconv.Quote(asset.sha256))
			out.WriteString("},\n")
		}
		out.WriteString("\t\t},\n\t},\n")
	}
	out.WriteString("}\n")
	return []byte(out.String())
}

type fileOps struct {
	rename    func(string, string) error
	remove    func(string) error
	removeAll func(string) error
}

var fsOps = fileOps{
	rename:    os.Rename,
	remove:    os.Remove,
	removeAll: os.RemoveAll,
}

func mirrorPackage(sourceRoot, targetRoot string) (err error) {
	parent := filepath.Dir(targetRoot)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".hotplex-skill-mirror-*")
	if err != nil {
		return err
	}
	defer func() {
		if cleanupErr := removeIfExists(stage); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("cleanup mirror stage %q: %w", stage, cleanupErr))
		}
	}()

	if err := copyTree(sourceRoot, stage); err != nil {
		return err
	}
	return replacePath(stage, targetRoot)
}

func copyTree(sourceRoot, targetRoot string) error {
	return filepath.WalkDir(sourceRoot, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relativePath, err := filepath.Rel(sourceRoot, sourcePath)
		if err != nil {
			return err
		}
		targetPath := targetRoot
		if relativePath != "." {
			targetPath = filepath.Join(targetRoot, relativePath)
		}
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed: %s", sourcePath)
		}
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o644)
	})
}

func writeAtomic(targetPath string, data []byte) (err error) {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(targetPath), ".hotplex-generated-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		if cleanupErr := removeIfExists(tempName); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("cleanup generated file %q: %w", tempName, cleanupErr))
		}
	}()
	if _, err := temp.Write(data); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := temp.Chmod(0o644); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replacePath(tempName, targetPath)
}

func replacePath(stagedPath, targetPath string) error {
	targetExists, err := pathExists(targetPath)
	if err != nil {
		return err
	}
	if !targetExists {
		return fsOps.rename(stagedPath, targetPath)
	}

	parent := filepath.Dir(targetPath)
	backupPath, err := createSiblingPlaceholder(parent, ".hotplex-backup-"+filepath.Base(targetPath)+"-")
	if err != nil {
		return fmt.Errorf("create unique backup for %q: %w", targetPath, err)
	}
	if err := fsOps.rename(targetPath, backupPath); err != nil {
		return fmt.Errorf("move existing %q to backup: %w", targetPath, err)
	}

	if err := fsOps.rename(stagedPath, targetPath); err != nil {
		rollbackErr := fsOps.rename(backupPath, targetPath)
		if rollbackErr != nil {
			return errors.Join(
				fmt.Errorf("promote staged %q: %w", stagedPath, err),
				fmt.Errorf("rollback %q: %w", targetPath, rollbackErr),
			)
		}
		return fmt.Errorf("promote staged %q: %w", stagedPath, err)
	}

	if err := removeIfExists(backupPath); err != nil {
		return errors.Join(
			fmt.Errorf("cleanup backup %q: %w", backupPath, err),
			rollbackReplacement(targetPath, backupPath),
		)
	}
	return nil
}

func rollbackReplacement(targetPath, backupPath string) error {
	displacedPath, err := createSiblingPlaceholder(filepath.Dir(targetPath), ".hotplex-displaced-"+filepath.Base(targetPath)+"-")
	if err != nil {
		return fmt.Errorf("stage promoted %q for rollback: %w", targetPath, err)
	}
	if err := fsOps.rename(targetPath, displacedPath); err != nil {
		return fmt.Errorf("move promoted %q for rollback: %w", targetPath, err)
	}
	if err := fsOps.rename(backupPath, targetPath); err != nil {
		restoreNewErr := fsOps.rename(displacedPath, targetPath)
		rollbackErrors := []error{fmt.Errorf("restore backup %q: %w", backupPath, err)}
		if restoreNewErr != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore promoted target %q: %w", targetPath, restoreNewErr))
		}
		return errors.Join(rollbackErrors...)
	}
	if err := removeIfExists(displacedPath); err != nil {
		return fmt.Errorf("cleanup displaced target %q: %w", displacedPath, err)
	}
	return nil
}

func createSiblingPlaceholder(parent, prefix string) (string, error) {
	placeholder, err := os.CreateTemp(parent, prefix)
	if err != nil {
		return "", err
	}
	placeholderPath := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		removeErr := fsOps.remove(placeholderPath)
		closeErrors := []error{fmt.Errorf("close placeholder %q: %w", placeholderPath, err)}
		if removeErr != nil {
			closeErrors = append(closeErrors, fmt.Errorf("cleanup placeholder %q: %w", placeholderPath, removeErr))
		}
		return "", errors.Join(closeErrors...)
	}
	if err := fsOps.remove(placeholderPath); err != nil {
		return "", fmt.Errorf("remove placeholder %q: %w", placeholderPath, err)
	}
	return placeholderPath, nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

func removeIfExists(path string) error {
	err := fsOps.removeAll(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
