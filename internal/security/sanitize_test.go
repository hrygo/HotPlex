package security

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactSensitive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		want     string
		redacted bool
	}{
		{
			name:     "no sensitive data",
			input:    "Hello, world!",
			want:     "Hello, world!",
			redacted: false,
		},
		{
			name:     "api key",
			input:    `config: api_key=sk-abc123def456ghi789jkl012mno345`,
			want:     "config: [REDACTED]",
			redacted: true,
		},
		{
			name:     "aws access key",
			input:    "key=AKIAIOSFODNN7EXAMPLE",
			want:     "key=[REDACTED]",
			redacted: true,
		},
		{
			name:     "private key",
			input:    "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----",
			want:     "[REDACTED]\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----",
			redacted: true,
		},
		{
			name:     "jwt token",
			input:    "token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			want:     "token=[REDACTED]",
			redacted: true,
		},
		{
			name:     "internal ip 10.x",
			input:    "server at 10.0.1.25 is down",
			want:     "server at [REDACTED] is down",
			redacted: true,
		},
		{
			name:     "internal ip 192.168.x",
			input:    "connect to 192.168.1.100:8080",
			want:     "connect to [REDACTED]:8080",
			redacted: true,
		},
		{
			name:     "postgres connection string",
			input:    "postgres://admin:s3cret@db.example.com:5432/mydb",
			want:     "[REDACTED]",
			redacted: true,
		},
		{
			name:     "password in config",
			input:    `password = "supersecretpass123"`,
			want:     "[REDACTED]",
			redacted: true,
		},
		{
			name:     "public ip not redacted",
			input:    "server at 8.8.8.8 is up",
			want:     "server at 8.8.8.8 is up",
			redacted: false,
		},
		{
			name:     "multiple secrets",
			input:    "db=postgres://u:p@h/d key=AKIAIOSFODNN7EXAMPLE",
			want:     "db=[REDACTED] key=[REDACTED]",
			redacted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, redacted := RedactSensitive(tt.input)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.redacted, redacted)
		})
	}
}
