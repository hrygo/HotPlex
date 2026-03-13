package rules

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hrygo/hotplex/internal/security"
)

// ========================================
// MemoryRuleSource Tests
// ========================================

func TestMemoryRuleSource(t *testing.T) {
	rules := []security.SecurityRule{
		&security.SafePatternRule{
			Pattern:     regexp.MustCompile(`^ls`),
			Description: "List files",
			Category:   "utilities",
		},
	}
	
	mrs := NewMemoryRuleSource("test-memory", rules)
	
	// Test Name
	if mrs.Name() != "test-memory" {
		t.Errorf("Expected name 'test-memory', got '%s'", mrs.Name())
	}
	
	// Test LoadRules
	loaded, err := mrs.LoadRules(context.Background())
	if err != nil {
		t.Errorf("LoadRules failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(loaded))
	}
	
	// Test AddRule
	mrs.AddRule(&security.SafePatternRule{
		Pattern:     regexp.MustCompile(`^cd`),
		Description: "Change directory",
		Category:   "utilities",
	})
	
	loaded, _ = mrs.LoadRules(context.Background())
	if len(loaded) != 2 {
		t.Errorf("Expected 2 rules after AddRule, got %d", len(loaded))
	}
}

func TestMemoryRuleSource_ConcurrentAccess(t *testing.T) {
	rules := []security.SecurityRule{
		&security.SafePatternRule{
			Pattern:     regexp.MustCompile(`^ls`),
			Description: "List files",
			Category:   "utilities",
		},
	}
	
	mrs := NewMemoryRuleSource("concurrent-test", rules)
	
	// Concurrent read
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				mrs.LoadRules(context.Background())
				mrs.Name()
			}
			done <- true
		}()
	}
	
	// Concurrent write
	go func() {
		for j := 0; j < 100; j++ {
			mrs.AddRule(&security.SafePatternRule{
				Pattern:     regexp.MustCompile(`^test`),
				Description: "test",
				Category:   "test",
			})
		}
		done <- true
	}()
	
	<-done
	<-done
}

func TestDefaultDevelopToolsRules(t *testing.T) {
	rules := DefaultDevelopToolsRules()
	
	if len(rules) == 0 {
		t.Error("DefaultDevelopToolsRules should return at least one rule")
	}
	
	// Test some known patterns (note: regex requires specific spacing)
	testCases := []struct {
		input    string
		expected bool
	}{
		{"go build ./...", true},
		{"npm install", true},
		{"git status", true},
		{"docker ps ", true},  // docker ps requires trailing space to match
		{"ls -la", true},
		{"rm -rf /", false}, // Not in safe patterns
	}
	
	for _, tc := range testCases {
		found := false
		for _, rule := range rules {
			if rule.Evaluate(tc.input) != nil {
				found = true
				break
			}
		}
		if found != tc.expected {
			t.Errorf("For input '%s', expected found=%v, got %v", tc.input, tc.expected, found)
		}
	}
}

// ========================================
// FileRuleSource Tests
// ========================================

func TestFileRuleSource_Name(t *testing.T) {
	f := NewFileRuleSource("/tmp/test.rules")
	if f.Name() != "file:/tmp/test.rules" {
		t.Errorf("Expected 'file:/tmp/test.rules', got '%s'", f.Name())
	}
}

func TestFileRuleSource_LoadRules_JSON(t *testing.T) {
	// Create a temporary JSON rules file
	content := `[
		{"pattern": "^ls", "description": "List files", "level": "safe", "category": "utilities"},
		{"pattern": "rm\\s+-rf\\s+/", "description": "Recursive delete", "level": "critical", "category": "destructive"}
	]`
	
	tmpFile, err := os.CreateTemp("", "rules-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()
	
	f := NewFileRuleSource(tmpFile.Name())
	rules, err := f.LoadRules(context.Background())
	if err != nil {
		t.Errorf("LoadRules failed: %v", err)
	}
	
	// Should load 2 rules (both valid patterns)
	if len(rules) != 2 {
		t.Errorf("Expected 2 rules, got %d", len(rules))
	}
	
	// Test rule evaluation
	if rules[0].Evaluate("ls -la") == nil {
		t.Error("Safe pattern should match 'ls -la'")
	}
}

func TestFileRuleSource_LoadRules_LineBased(t *testing.T) {
	// Create a temporary line-based rules file
	content := `# Comment line
^ls|List files|safe|utilities
^cd|Change directory|safe|utilities
rm\\s+-rf\\s+/|Recursive delete|critical|destructive
`
	
	tmpFile, err := os.CreateTemp("", "rules-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()
	
	f := NewFileRuleSource(tmpFile.Name())
	rules, err := f.LoadRules(context.Background())
	if err != nil {
		t.Errorf("LoadRules failed: %v", err)
	}
	
	// Should load 3 rules (skipping comment and empty lines)
	if len(rules) != 3 {
		t.Errorf("Expected 3 rules, got %d", len(rules))
	}
}

func TestFileRuleSource_LoadRules_FileNotFound(t *testing.T) {
	f := NewFileRuleSource("/nonexistent/path/rules.txt")
	_, err := f.LoadRules(context.Background())
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestFileRuleSource_InvalidJSON(t *testing.T) {
	// Create a temp file with invalid JSON (falls back to line-based)
	content := `not valid json`
	
	tmpFile, err := os.CreateTemp("", "rules-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()
	
	f := NewFileRuleSource(tmpFile.Name())
	rules, err := f.LoadRules(context.Background())
	if err != nil {
		t.Errorf("LoadRules should not fail for invalid content: %v", err)
	}
	
	// Should return empty rules (line-based parsing finds nothing valid)
	if len(rules) != 0 {
		t.Errorf("Expected 0 rules for invalid content, got %d", len(rules))
	}
}

// ========================================
// Helper Functions Tests
// ========================================

func TestSplitLineRule(t *testing.T) {
	tests := []struct {
		line     string
		expected int
	}{
		{"pattern|desc|level|cat", 4},
		{"pattern|desc|level|cat|type", 5},
		{"a|b|c", 3},
		{"", 0},
	}
	
	for _, tt := range tests {
		result := splitLineRule(tt.line)
		if len(result) != tt.expected {
			t.Errorf("splitLineRule('%s'): expected %d parts, got %d", tt.line, tt.expected, len(result))
		}
	}
}

func TestSplitLineRule_EscapedPipe(t *testing.T) {
	result := splitLineRule("pattern\\|with\\|pipes|desc|level|cat")
	// Should split into 4 parts, not 6 (escaped pipes are not delimiters)
	if len(result) != 4 {
		t.Errorf("Expected 4 parts with escaped pipes, got %d: %v", len(result), result)
	}
}

func TestRemoveComment(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"# comment", ""},
		{"no comment", "no comment"},
		{"code # inline comment", "code "},
		{"", ""},
		{"   # leading spaces then comment", "   "},
	}
	
	for _, tt := range tests {
		result := removeComment(tt.input)
		if result != tt.expected {
			t.Errorf("removeComment('%s'): expected '%s', got '%s'", tt.input, tt.expected, result)
		}
	}
}

func TestParseLineRule(t *testing.T) {
	tests := []struct {
		line       string
		wantErr    bool
		wantPattern string
	}{
		{"pattern|description|level|category", false, "pattern"},
		{"pattern|description|level|category|type", false, "pattern"},
		{"a|b|c|d", false, "a"},
		{"a|b|c", true, ""}, // Less than 4 parts should error
		{"a|b", true, ""}, // Less than 4 parts should error
		{"", true, ""},
	}
	
	for _, tt := range tests {
		result, err := parseLineRule(tt.line)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseLineRule('%s'): error = %v, wantErr %v", tt.line, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && result.Pattern != tt.wantPattern {
			t.Errorf("parseLineRule('%s'): pattern = %s, want %s", tt.line, result.Pattern, tt.wantPattern)
		}
	}
}

// ========================================
// Integration Tests
// ========================================

func TestFileRuleSource_AbsolutePath(t *testing.T) {
	// Test with absolute path
	absPath, err := filepath.Abs("/tmp/test.rules")
	if err != nil {
		t.Skip("Cannot resolve absolute path")
	}
	
	f := NewFileRuleSource(absPath)
	if f.Name() != "file:"+absPath {
		t.Errorf("Name should include absolute path")
	}
}

func TestMemoryRuleSource_Empty(t *testing.T) {
	mrs := NewMemoryRuleSource("empty", nil)
	rules, err := mrs.LoadRules(context.Background())
	if err != nil {
		t.Errorf("LoadRules failed: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("Expected 0 rules for empty source, got %d", len(rules))
	}
}
