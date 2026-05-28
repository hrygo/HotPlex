package install

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveSourceBinary returns the absolute, symlink-resolved path of the
// currently running binary.
func ResolveSourceBinary() (string, error) {
	bin, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve binary: %w", err)
	}
	bin, err = filepath.EvalSymlinks(bin)
	if err != nil {
		return "", fmt.Errorf("resolve symlink: %w", err)
	}
	return bin, nil
}

// DefaultInstallDir returns the platform-specific default directory for
// installing the hotplex binary: ~/.local/bin on Unix, ~\hotplex\bin on Windows.
func DefaultInstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(home, ".hotplex", "bin"), nil
	}
	return filepath.Join(home, ".local", "bin"), nil
}

// ExeName returns the platform-specific executable name.
func ExeName() string {
	if runtime.GOOS == "windows" {
		return "hotplex.exe"
	}
	return "hotplex"
}

// CopyBinary atomically copies src to dst via a temp file in the same directory.
func CopyBinary(src, dst string) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), "hotplex-install-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()

	in, err := os.Open(src)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	defer func() { _ = in.Close() }()

	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	ok = true
	return nil
}

// SameContent reports whether two files have identical content (size + SHA256).
func SameContent(a, b string) bool {
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	if err1 != nil || err2 != nil || ai.Size() != bi.Size() {
		return false
	}
	ha, hb := fileSHA256(a), fileSHA256(b)
	return ha != nil && hb != nil && string(ha) == string(hb)
}

func fileSHA256(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil
	}
	return h.Sum(nil)
}

// IsInPATH reports whether dir is listed in the PATH environment variable.
// Uses filepath.Clean to normalize trailing slashes and redundant separators.
func IsInPATH(dir string) bool {
	dir = filepath.Clean(dir)
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == dir {
			return true
		}
	}
	return false
}

// AddToUserPath adds dir to the user's PATH permanently (shell RC on Unix,
// User environment variable on Windows).
func AddToUserPath(dir string) error {
	if runtime.GOOS == "windows" {
		// Escape single quotes for PowerShell: replace ' with ''
		escaped := strings.ReplaceAll(dir, "'", "''")
		// Use -split/-notcontains instead of -notlike to avoid
		// wildcard interpretation of [ and ] in paths.
		ps := fmt.Sprintf(
			`$p=[Environment]::GetEnvironmentVariable('Path','User');`+
				`if(-not ($p -split ';' -notcontains '%s')){`+
				`[Environment]::SetEnvironmentVariable('Path','%s;'+$p,'User')}`,
			escaped, escaped)
		return exec.Command("powershell", "-NoProfile", "-Command", ps).Run()
	}

	shellName := filepath.Base(os.Getenv("SHELL"))
	if shellName == "" || shellName == "." {
		return fmt.Errorf("SHELL environment variable not set; add %s to PATH manually", dir)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("determine home directory: %w", err)
	}
	// Escape double quotes and backslashes for shell export line
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(dir)
	exportLine := fmt.Sprintf(`export PATH="%s:$PATH"`, escaped)

	switch shellName {
	case "zsh":
		return appendToRC(filepath.Join(homeDir, ".zshrc"), exportLine)
	case "bash":
		return appendToRC(filepath.Join(homeDir, ".bashrc"), exportLine)
	case "fish":
		return exec.Command("fish", "-c", "fish_add_path "+shellEscape(dir)).Run()
	default:
		return fmt.Errorf("unsupported shell: %s, add %s to PATH manually", shellName, dir)
	}
}

// shellEscape quotes a string for safe use as a single shell argument.
func shellEscape(s string) string {
	if s == "" {
		return "''"
	}
	needsQuote := false
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') &&
			c != '-' && c != '_' && c != '.' && c != '/' && c != '@' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func appendToRC(path, line string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(data), line) {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = fmt.Fprintf(f, "\n# Added by hotplex\n%s\n", line)
	return err
}
