package builtin

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed hotplex-cli hotplex-operator
var embeddedAssets embed.FS

//go:generate go run ../../../cmd/hotplex --internal-generate-cli-surface --output hotplex-cli/references/cli-surface.generated.md
//go:generate go run ../../../cmd/gen-builtin-skills --canonical . --manifest-output manifest.generated.go --mirror ../../../.agents/skills

func readEmbeddedFile(packageName, relativePath string) ([]byte, error) {
	return fs.ReadFile(embeddedAssets, path.Join(packageName, relativePath))
}

func embeddedPackagePaths(packageName string) ([]string, error) {
	var paths []string
	err := fs.WalkDir(embeddedAssets, packageName, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relativePath := strings.TrimPrefix(name, packageName+"/")
		paths = append(paths, relativePath)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func validAssetPath(relativePath string) bool {
	if relativePath == "" || path.IsAbs(relativePath) || strings.ContainsRune(relativePath, '\\') {
		return false
	}
	clean := path.Clean(relativePath)
	return clean == relativePath && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}
