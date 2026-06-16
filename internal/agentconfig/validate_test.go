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
		{
			name:    "total exceeds MaxTotalChars",
			raw:     `{"SOUL.md":"` + strings.Repeat("a", MaxTotalChars+1) + `"}`,
			wantErr: ErrConfigTooLarge,
		},
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
