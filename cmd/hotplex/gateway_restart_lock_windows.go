//go:build windows

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func acquireRestartLeaseFileLock(path string) (func() error, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() error {
		unlockErr := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
		return errors.Join(unlockErr, file.Close())
	}, nil
}
