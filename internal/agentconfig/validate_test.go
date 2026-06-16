package agentconfig

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateOverrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantErr error // sentinel; nil expects success
		want    map[string]string
	}{
		{
			name: "empty raw returns nil map",
			raw:  "",
			want: nil,
		},
		{
			name: "valid flat map with subset of files",
			raw:  `{"SOUL.md":"persona","USER.md":"profile"}`,
			want: map[string]string{"SOUL.md": "persona", "USER.md": "profile"},
		},
		{
			name:    "invalid JSON",
			raw:     `{not json`,
			wantErr: ErrInvalidConfigJSON,
		},
		{
			name:    "unknown key rejected",
			raw:     `{"META-COGNITION.md":"x"}`,
			wantErr: ErrUnknownConfigFile,
		},
		{
			name:    "unknown arbitrary key rejected",
			raw:     `{"foo.md":"x"}`,
			wantErr: ErrUnknownConfigFile,
		},
		{
			name:    "non-string value rejected",
			raw:     `{"SOUL.md":123}`,
			wantErr: ErrInvalidConfigValue,
		},
		{
			name:    "single file exceeds MaxFileChars",
			raw:     `{"SOUL.md":"` + strings.Repeat("a", MaxFileChars+1) + `"}`,
			wantErr: ErrConfigTooLarge,
		},
		// The total-limit branch (sum > MaxTotalChars) is unreachable under the current
		// 5-file whitelist with MaxFileChars=8000: max sum = 5*8000 = 40000 = MaxTotalChars,
		// never exceeds. The check in validate.go is defense-in-depth (matches loader.go's
		// Load total budget) and activates only if configFiles grows or MaxFileChars rises.
		// The per-file case above covers the oversized-input rejection path.
		{
			name: "empty object clears overrides",
			raw:  `{}`,
			want: map[string]string{},
		},
		{
			name: "all five known files accepted",
			raw:  `{"SOUL.md":"s","AGENTS.md":"a","SKILLS.md":"k","USER.md":"u","MEMORY.md":"m"}`,
			want: map[string]string{"SOUL.md": "s", "AGENTS.md": "a", "SKILLS.md": "k", "USER.md": "u", "MEMORY.md": "m"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateOverrides(tt.raw)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
