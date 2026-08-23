package builtin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

type Profile string

const (
	ProfileRuntime  Profile = "runtime"
	ProfileOperator Profile = "operator"
)

var profilePackageSets = map[Profile][]string{
	ProfileRuntime:  {"hotplex-cli"},
	ProfileOperator: {"hotplex-cli", "hotplex-operator"},
}

// ProfilePackageSet returns a copy so callers cannot mutate the package policy.
func ProfilePackageSet(profile Profile) []string {
	return append([]string(nil), profilePackageSets[profile]...)
}

type AssetManifest struct {
	Path   string
	Size   int64
	SHA256 string
}

type PackageManifest struct {
	Name    string
	Version string
	Profile Profile
	Assets  []AssetManifest
}

func (m PackageManifest) Paths() []string {
	paths := make([]string, 0, len(m.Assets))
	for _, asset := range m.Assets {
		paths = append(paths, asset.Path)
	}
	sort.Strings(paths)
	return paths
}

type InstalledPackage struct {
	Manifest      PackageManifest
	InventoryPath string
}

type Registry struct {
	manifests map[string]PackageManifest
}

func NewRegistry() (*Registry, error) {
	manifests := make(map[string]PackageManifest, len(generatedManifests))
	for _, manifest := range generatedManifests {
		if _, exists := manifests[manifest.Name]; exists {
			return nil, fmt.Errorf("builtin: duplicate package %q", manifest.Name)
		}
		manifests[manifest.Name] = cloneManifest(manifest)
	}
	if err := validateManifests(manifests); err != nil {
		return nil, err
	}
	return &Registry{manifests: manifests}, nil
}

func (r *Registry) Packages() []PackageManifest {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.manifests))
	for name := range r.manifests {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]PackageManifest, 0, len(names))
	for _, name := range names {
		out = append(out, cloneManifest(r.manifests[name]))
	}
	return out
}

func (r *Registry) Package(name string) (PackageManifest, bool) {
	if r == nil {
		return PackageManifest{}, false
	}
	manifest, ok := r.manifests[name]
	if !ok {
		return PackageManifest{}, false
	}
	return cloneManifest(manifest), true
}

func (r *Registry) PackagesForProfile(profile Profile) ([]PackageManifest, error) {
	names := ProfilePackageSet(profile)
	if len(names) == 0 {
		return nil, fmt.Errorf("builtin: unsupported profile %q", profile)
	}
	out := make([]PackageManifest, 0, len(names))
	for _, name := range names {
		manifest, ok := r.Package(name)
		if !ok {
			return nil, fmt.Errorf("builtin: profile %q references missing package %q", profile, name)
		}
		out = append(out, manifest)
	}
	return out, nil
}

func (r *Registry) ReadFile(packageName, relativePath string) ([]byte, error) {
	if r == nil {
		return nil, errors.New("builtin: nil registry")
	}
	if _, ok := r.manifests[packageName]; !ok {
		return nil, fmt.Errorf("builtin: unknown package %q", packageName)
	}
	if !validAssetPath(relativePath) {
		return nil, fmt.Errorf("builtin: invalid asset path %q", relativePath)
	}
	return readEmbeddedFile(packageName, path.Clean(relativePath))
}

func validateManifests(manifests map[string]PackageManifest) error {
	if len(manifests) != len(profilePackageSets) {
		return fmt.Errorf("builtin: generated manifest package count %d, want %d", len(manifests), len(profilePackageSets))
	}

	expectedProfiles := map[string]Profile{
		"hotplex-cli":      ProfileRuntime,
		"hotplex-operator": ProfileOperator,
	}
	for name, profile := range expectedProfiles {
		manifest, ok := manifests[name]
		if !ok {
			return fmt.Errorf("builtin: missing package %q", name)
		}
		if manifest.Profile != profile {
			return fmt.Errorf("builtin: package %q has profile %q, want %q", name, manifest.Profile, profile)
		}
		if manifest.Version == "" {
			return fmt.Errorf("builtin: package %q has empty version", name)
		}

		embeddedPaths, err := embeddedPackagePaths(name)
		if err != nil {
			return fmt.Errorf("builtin: list package %q: %w", name, err)
		}
		manifestPaths := manifest.Paths()
		sort.Strings(manifestPaths)
		if strings.Join(embeddedPaths, "\x00") != strings.Join(manifestPaths, "\x00") {
			return fmt.Errorf("builtin: package %q manifest paths differ from embedded paths", name)
		}

		seen := make(map[string]struct{}, len(manifest.Assets))
		for _, asset := range manifest.Assets {
			if !validAssetPath(asset.Path) {
				return fmt.Errorf("builtin: package %q has invalid asset path %q", name, asset.Path)
			}
			if _, exists := seen[asset.Path]; exists {
				return fmt.Errorf("builtin: package %q repeats asset %q", name, asset.Path)
			}
			seen[asset.Path] = struct{}{}
			data, err := readEmbeddedFile(name, asset.Path)
			if err != nil {
				return fmt.Errorf("builtin: read %s/%s: %w", name, asset.Path, err)
			}
			sum := sha256.Sum256(data)
			if asset.Size != int64(len(data)) {
				return fmt.Errorf("builtin: %s/%s size %d, want %d", name, asset.Path, asset.Size, len(data))
			}
			if asset.SHA256 != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("builtin: %s/%s hash mismatch", name, asset.Path)
			}
		}
	}
	return nil
}

func cloneManifest(manifest PackageManifest) PackageManifest {
	manifest.Assets = append([]AssetManifest(nil), manifest.Assets...)
	return manifest
}
