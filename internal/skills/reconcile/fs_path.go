package reconcile

import "path/filepath"

func filepathEvalSymlinks(name string) (string, error) {
	return filepath.EvalSymlinks(name)
}
