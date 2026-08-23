//go:build windows

package reconcile

// Windows has no portable directory-fsync primitive in the Go standard
// library. File contents are still flushed before rename; directory sync is
// a best-effort no-op so ordinary reconciliation does not fail on Windows.
func (osFS) SyncDir(string) error { return nil }
