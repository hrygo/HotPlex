//go:build !windows

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
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
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() error {
		unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
		return errors.Join(unlockErr, file.Close())
	}, nil
}
