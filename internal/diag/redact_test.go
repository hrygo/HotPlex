package diag

import (
	"strings"
	"testing"
)

func TestRedactAPIKey(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "api_key=sk-1234567890abcdef1234567890abcdef",
			expected: "api_key=[REDACTED_API_KEY]",
		},
		{
			input:    "API_KEY: \"supersecretkey12345678\"",
			expected: "[REDACTED_API_KEY]",
		},
		{
			input:    "apikey = mylongapikey123456789",
			expected: "[REDACTED_API_KEY]",
		},
	}

	for _, tt := range tests {
		result := redactor.Redact(tt.input)
		if !strings.Contains(result, "[REDACTED") {
			t.Errorf("expected redaction for input %q, got %q", tt.input, result)
		}
	}
}

func TestRedactSlackToken(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	// Use a fake token pattern that matches the regex
	input := "token=xoxb-FAKE-TOKEN-FOR-TESTING-NOT-REAL"
	result := redactor.Redact(input)

	if strings.Contains(result, "xoxb-") {
		t.Errorf("slack token not redacted: %s", result)
	}
	if !strings.Contains(result, "[REDACTED_SLACK_TOKEN]") {
		t.Errorf("expected [REDACTED_SLACK_TOKEN], got: %s", result)
	}
}

func TestRedactGitHubToken(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	// Use a fake token pattern that matches the regex
	input := "GITHUB_TOKEN=ghp_FAKEFORTOKENFORTOKENFORTOKEN1234"
	result := redactor.Redact(input)

	if strings.Contains(result, "ghp_") {
		t.Errorf("github token not redacted: %s", result)
	}
}

func TestRedactAnthropicKey(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	input := "ANTHROPIC_API_KEY=sk-ant-api03-1234567890abcdef"
	result := redactor.Redact(input)

	if strings.Contains(result, "sk-ant-") {
		t.Errorf("anthropic key not redacted: %s", result)
	}
}

func TestRedactPassword(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	tests := []struct {
		input    string
		contains string
	}{
		{
			input:    "password=mysecretpass123",
			contains: "[REDACTED_SECRET]",
		},
		{
			input:    "SECRET=\"supersecretvalue\"",
			contains: "[REDACTED_SECRET]",
		},
	}

	for _, tt := range tests {
		result := redactor.Redact(tt.input)
		if !strings.Contains(result, tt.contains) {
			t.Errorf("expected %q in result for %q, got %q", tt.contains, tt.input, result)
		}
	}
}

func TestRedactPrivateKey(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	input := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy0AHB7MbzYLdZ7ZvVy7F7V
some more key data here
-----END RSA PRIVATE KEY-----`

	result := redactor.Redact(input)

	if strings.Contains(result, "PRIVATE KEY") {
		t.Errorf("private key not redacted")
	}
	if !strings.Contains(result, "[REDACTED_PRIVATE_KEY]") {
		t.Errorf("expected [REDACTED_PRIVATE_KEY]")
	}
}

func TestRedactJWT(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	result := redactor.Redact(input)

	if strings.Contains(result, "eyJ") {
		t.Errorf("JWT not redacted: %s", result)
	}
}

func TestRedactEmail(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	input := "Contact: user@example.com for support"
	result := redactor.Redact(input)

	if strings.Contains(result, "user@example.com") {
		t.Errorf("email not redacted: %s", result)
	}
	if !strings.Contains(result, "[REDACTED_EMAIL]") {
		t.Errorf("expected [REDACTED_EMAIL]")
	}
}

func TestRedactConnectionStrings(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	tests := []string{
		"postgres://admin:secretpassword@localhost:5432/mydb",
		"mysql://root:password123@db.example.com:3306/production",
		"mongodb://user:pass@cluster.mongodb.net/db",
	}

	for _, input := range tests {
		result := redactor.Redact(input)
		if strings.Contains(result, "secretpassword") || strings.Contains(result, "password123") || strings.Contains(result, ":pass@") {
			t.Errorf("connection string password not redacted: %s", result)
		}
	}
}

func TestRedactAggressive(t *testing.T) {
	redactor := NewRedactor(RedactAggressive)

	tests := []struct {
		input    string
		contains string
	}{
		{"Server running on 10.0.0.1:8080", "[REDACTED_IP]"},
		{"Internal API at 172.16.0.1", "[REDACTED_IP]"},
		{"Local network 192.168.1.100", "[REDACTED_IP]"},
		{"Connecting to localhost:3000", "[REDACTED_HOST]"},
	}

	for _, tt := range tests {
		result := redactor.Redact(tt.input)
		if !strings.Contains(result, tt.contains) {
			t.Errorf("expected %q in %q, got %q", tt.contains, tt.input, result)
		}
	}
}

func TestRedactMapValues(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	input := map[string]any{
		"username": "john",
		"password": "secret123",
		"config": map[string]any{
			"api_key": "mykey123",
			"timeout": 30,
		},
	}

	result := redactor.RedactMapValues(input)

	if result["password"] != "[REDACTED]" {
		t.Errorf("password not redacted in map")
	}

	config := result["config"].(map[string]any)
	if config["api_key"] != "[REDACTED]" {
		t.Errorf("api_key not redacted in nested map")
	}
	if config["timeout"] != 30 {
		t.Errorf("timeout should remain unchanged")
	}
}

func TestRedactPreservesNonSensitive(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	input := "Server started on port 8080 with timeout 30s"
	result := redactor.Redact(input)

	if result != input {
		t.Errorf("non-sensitive content should be preserved: %s", result)
	}
}

func TestRedactEmpty(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	if result := redactor.Redact(""); result != "" {
		t.Errorf("empty string should remain empty: %q", result)
	}
}

func TestDefaultRedactor(t *testing.T) {
	// Test convenience functions - use a longer key that matches the pattern
	input := "api_key=sk-1234567890abcdefghij1234567890ab"
	result := Redact(input)

	if strings.Contains(result, "sk-1234567890abcdefghij1234567890ab") {
		t.Errorf("default redactor did not redact: %s", result)
	}
}

func TestRedactBytes(t *testing.T) {
	input := []byte("password=mysecretpassword123")
	result := RedactBytes(input)

	if strings.Contains(string(result), "mysecretpassword123") {
		t.Errorf("bytes not redacted: %s", result)
	}
}

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		{"password", true},
		{"api_key", true},
		{"secret", true},
		{"token", true},
		{"username", false},
		{"timeout", false},
		{"max_retries", false},
		{"auth_token", true},
		{"database_url", false},
	}

	for _, tt := range tests {
		result := isSensitiveKey(tt.key)
		if result != tt.expected {
			t.Errorf("isSensitiveKey(%q) = %v, expected %v", tt.key, result, tt.expected)
		}
	}
}

func TestRedactAWSKeys(t *testing.T) {
	redactor := NewRedactor(RedactStandard)

	// AWS Access Key ID
	input1 := "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"
	result1 := redactor.Redact(input1)
	if strings.Contains(result1, "AKIA") {
		t.Errorf("AWS access key not redacted: %s", result1)
	}

	// AWS Secret Key
	input2 := "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	result2 := redactor.Redact(input2)
	if strings.Contains(result2, "wJalrXUtnFEMI") {
		t.Errorf("AWS secret not redacted: %s", result2)
	}
}
