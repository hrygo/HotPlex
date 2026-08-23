package builtin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/internal/skills"
)

// PublicCatalog is the read-only Agent Skills view of the embedded canonical
// packages. It deliberately has no filesystem or reconciliation dependency:
// an embedded package is discoverable even before it has been synchronized to
// a Worker-native root.
type PublicCatalog interface {
	List(context.Context, string) ([]skills.Skill, error)
	Read(context.Context, string, string) (*skills.Detail, error)
}

// Catalog exposes canonical packages as ordinary Agent Skills metadata. The
// registry is validated when it is created; Catalog never reads inventory,
// receipts, or a host path.
type Catalog struct {
	registry *Registry
}

var _ PublicCatalog = (*Catalog)(nil)

// NewPublicCatalog creates a read-only catalog backed by registry.
func NewPublicCatalog(registry *Registry) *Catalog {
	return &Catalog{registry: registry}
}

var (
	errNilPublicRegistry = errors.New("builtin: nil public registry")
	// ErrSkillNotFound aliases the existing skills package sentinel so callers
	// can distinguish an unknown embedded name without a new HTTP contract.
	ErrSkillNotFound = skills.ErrSkillNotFound
)

// List returns canonical packages in stable name order. An empty profile lists
// all canonical packages; otherwise profile must be runtime or operator. The
// operator profile is the cumulative public view and currently includes both
// canonical packages.
func (c *Catalog) List(ctx context.Context, profile string) ([]skills.Skill, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if c == nil || c.registry == nil {
		return nil, errNilPublicRegistry
	}

	var manifests []PackageManifest
	var err error
	if strings.TrimSpace(profile) == "" {
		manifests = c.registry.Packages()
	} else {
		manifests, err = c.registry.PackagesForProfile(Profile(strings.TrimSpace(profile)))
		if err != nil {
			return nil, err
		}
	}

	result := make([]skills.Skill, 0, len(manifests))
	for _, manifest := range manifests {
		if err := contextErr(ctx); err != nil {
			return nil, err
		}
		metadata, err := c.metadata(manifest)
		if err != nil {
			return nil, err
		}
		result = append(result, metadata)
	}
	return result, nil
}

// Read returns a canonical Skill detail, including the embedded SKILL.md body
// and the complete manifest file list. For compatibility with both package
// oriented callers and profile/name callers, first is preferred as the package
// name; when it is not a package, second is treated as the package name. The
// second argument is otherwise ignored, so Read(ctx, "hotplex-cli", "") is
// the canonical form.
func (c *Catalog) Read(ctx context.Context, first, second string) (*skills.Detail, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if c == nil || c.registry == nil {
		return nil, errNilPublicRegistry
	}

	packageName := strings.TrimSpace(first)
	if _, ok := c.registry.Package(packageName); !ok {
		packageName = strings.TrimSpace(second)
	}
	manifest, ok := c.registry.Package(packageName)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, strings.TrimSpace(first))
	}

	body, err := c.registry.ReadFile(manifest.Name, "SKILL.md")
	if err != nil {
		return nil, fmt.Errorf("builtin: read %s/SKILL.md: %w", manifest.Name, err)
	}
	metadata, err := c.metadataFromBody(manifest, body)
	if err != nil {
		return nil, err
	}
	return &skills.Detail{
		Skill: metadata,
		Body:  string(body),
		Files: manifest.Paths(),
	}, nil
}

func (c *Catalog) metadata(manifest PackageManifest) (skills.Skill, error) {
	body, err := c.registry.ReadFile(manifest.Name, "SKILL.md")
	if err != nil {
		return skills.Skill{}, fmt.Errorf("builtin: read %s/SKILL.md: %w", manifest.Name, err)
	}
	return c.metadataFromBody(manifest, body)
}

func (c *Catalog) metadataFromBody(manifest PackageManifest, body []byte) (skills.Skill, error) {
	name, description, ok := parseEmbeddedFrontmatter(body)
	if !ok || name == "" {
		return skills.Skill{}, fmt.Errorf("builtin: %s has invalid SKILL.md frontmatter", manifest.Name)
	}
	if name != manifest.Name {
		return skills.Skill{}, fmt.Errorf("builtin: %s frontmatter name %q does not match package", manifest.Name, name)
	}
	return skills.Skill{
		Name:                  name,
		Description:           description,
		Source:                skills.SourceGlobal,
		Managed:               false,
		Builtin:               true,
		BuiltinPackageVersion: manifest.Version,
	}, nil
}

func parseEmbeddedFrontmatter(data []byte) (string, string, bool) {
	if !bytes.HasPrefix(data, []byte("---")) {
		return "", "", false
	}
	end := bytes.Index(data[3:], []byte("\n---"))
	if end < 0 {
		return "", "", false
	}
	var name, description string
	for _, line := range strings.Split(string(data[3:end+3]), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			name = strings.TrimSpace(value)
		case "description":
			description = strings.TrimSpace(value)
		}
	}
	description = skills.CollapseSpaces(strings.ReplaceAll(description, "\n", " "))
	if name == "" || description == "" {
		return "", "", false
	}
	return name, description, true
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
