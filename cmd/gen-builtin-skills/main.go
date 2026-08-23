package main

import (
	"crypto/sha256"
	"encoding/hex"
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
		"references/install-update.md",
		"references/service-lifecycle.md",
	},
}

func main() {
	var config generatorConfig
	flag.StringVar(&config.canonicalRoot, "canonical", "internal/skills/builtin", "canonical package root")
	flag.StringVar(&config.manifestOutput, "manifest-output", "internal/skills/builtin/manifest.generated.go", "generated manifest path")
	flag.StringVar(&config.mirrorRoot, "mirror", ".agents/skills/hotplex-cli", "runtime skill mirror path")
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
		})
	}
	if err := writeAtomic(manifestOutput, renderManifest(manifests)); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := mirrorPackage(filepath.Join(canonicalRoot, "hotplex-cli"), mirrorRoot); err != nil {
		return fmt.Errorf("mirror hotplex-cli: %w", err)
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

func renderManifest(packages []generatedPackage) []byte {
	var out strings.Builder
	out.WriteString("package builtin\n\n")
	out.WriteString("var generatedManifests = []PackageManifest{\n")
	for _, pkg := range packages {
		out.WriteString("\t{\n")
		out.WriteString("\t\tName:    ")
		out.WriteString(strconv.Quote(pkg.name))
		out.WriteString(",\n\t\tVersion: \"1\",\n\t\tProfile: Profile")
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

func mirrorPackage(sourceRoot, targetRoot string) error {
	parent := filepath.Dir(targetRoot)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".hotplex-skill-mirror-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()

	if err := copyTree(sourceRoot, stage); err != nil {
		return err
	}
	backup := targetRoot + ".backup"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(targetRoot); err == nil {
		if err := os.Rename(targetRoot, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(stage, targetRoot); err != nil {
		if _, backupErr := os.Stat(backup); backupErr == nil {
			_ = os.Rename(backup, targetRoot)
		}
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
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

func writeAtomic(targetPath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(targetPath), ".hotplex-generated-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, targetPath)
}
