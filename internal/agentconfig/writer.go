package agentconfig

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveFilePath returns the bot-level path for a config file and ensures
// the parent directory exists. It validates that botID contains no path
// separators to prevent directory traversal.
func ResolveFilePath(dir, platform, botID, fileName string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("agentconfig: empty dir")
	}
	if botID != "" && filepath.Base(botID) != botID {
		return "", fmt.Errorf("agentconfig: invalid botID %q: path separators not allowed", botID)
	}

	var parent string
	if botID != "" && platform != "" {
		parent = filepath.Join(dir, platform, botID)
	} else if platform != "" {
		parent = filepath.Join(dir, platform)
	} else {
		parent = dir
	}

	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("agentconfig: mkdir %s: %w", parent, err)
	}

	return filepath.Join(parent, fileName), nil
}

// WriteFile atomically writes content to a bot-level config file.
// It validates that content size does not exceed maxBytes, creates a temp
// file in the same directory, writes content, then renames to the target path.
func WriteFile(dir, platform, botID, fileName, content string, maxBytes int) error {
	if len(content) > maxBytes {
		return fmt.Errorf("agentconfig: content size %d exceeds limit %d", len(content), maxBytes)
	}

	target, err := ResolveFilePath(dir, platform, botID, fileName)
	if err != nil {
		return err
	}

	// Create temp file in the same directory to ensure atomic rename.
	tmp, err := os.CreateTemp(filepath.Dir(target), ".hotplex-*")
	if err != nil {
		return fmt.Errorf("agentconfig: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	if _, err := tmp.WriteString(content); err != nil {
		cleanup()
		return fmt.Errorf("agentconfig: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("agentconfig: close temp file: %w", err)
	}

	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("agentconfig: rename temp file: %w", err)
	}

	return nil
}

// ResolvedSource reports which level a config file resolves from by checking
// os.Stat at each level in priority order. Returns "bot", "platform", "global",
// or "" if the file is not found at any level.
func ResolvedSource(dir, platform, botID, fileName string) string {
	// 1. Bot-level
	if botID != "" && platform != "" {
		p := filepath.Join(dir, platform, botID, fileName)
		if _, err := os.Stat(p); err == nil {
			return "bot"
		}
	}
	// 2. Platform-level
	if platform != "" {
		p := filepath.Join(dir, platform, fileName)
		if _, err := os.Stat(p); err == nil {
			return "platform"
		}
	}
	// 3. Global-level
	p := filepath.Join(dir, fileName)
	if _, err := os.Stat(p); err == nil {
		return "global"
	}
	return ""
}
