//go:build windows

package reconcile

func (osFS) SyncDir(string) error { return ErrDirSyncUnsupported }
