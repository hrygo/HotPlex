//go:build !windows

package main

import "os"

func replaceRestartFile(source, target string) error {
	return os.Rename(source, target)
}
