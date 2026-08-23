//go:build !windows

package reconcile

import "os"

func (osFS) SyncDir(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}
