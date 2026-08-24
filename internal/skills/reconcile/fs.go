package reconcile

import (
	"errors"
	"io"
	"os"
)

type osFS struct{}

// OSFileSystem exposes the production filesystem implementation to the CLI
// dependency seam while keeping all reconciliation operations injectable in
// tests.
type OSFileSystem = osFS

func NewOSFileSystem() FileSystem { return osFS{} }

func (osFS) Lstat(name string) (os.FileInfo, error) { return os.Lstat(name) }

func (osFS) ReadDir(name string) ([]os.DirEntry, error) { return os.ReadDir(name) }

func (osFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

func (osFS) MkdirAll(name string, mode os.FileMode) error { return os.MkdirAll(name, mode) }

func (osFS) WriteFile(name string, data []byte, mode os.FileMode) error {
	return os.WriteFile(name, data, mode)
}

func (osFS) Rename(oldName, newName string) error { return os.Rename(oldName, newName) }

func (osFS) Remove(name string) error { return os.Remove(name) }

func (osFS) RemoveAll(name string) error { return os.RemoveAll(name) }

func (osFS) Symlink(oldname, newname string) error { return os.Symlink(oldname, newname) }

func (osFS) EvalSymlinks(name string) (string, error) { return filepathEvalSymlinks(name) }

func (osFS) SyncFile(name string) error {
	f, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}

func (osFS) MkdirTemp(dir, pattern string) (string, error) {
	return os.MkdirTemp(dir, pattern)
}

func (osFS) CreateTemp(dir, pattern string, data []byte, mode os.FileMode) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	cleanup := func(closeErr error) (string, error) {
		if removeErr := os.Remove(name); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			closeErr = errors.Join(closeErr, removeErr)
		}
		return "", closeErr
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return cleanup(err)
	}
	if n, err := f.Write(data); err != nil {
		_ = f.Close()
		return cleanup(err)
	} else if n != len(data) {
		_ = f.Close()
		return cleanup(io.ErrShortWrite)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return cleanup(err)
	}
	if err := f.Close(); err != nil {
		return cleanup(err)
	}
	return name, nil
}
