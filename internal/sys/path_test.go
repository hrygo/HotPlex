package sys

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Skipping test: could not determine home directory")
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "Home directory only",
			path: "~",
			want: home,
		},
		{
			name: "Path starting with tilde",
			path: "~/foo/bar",
			want: filepath.Join(home, "foo/bar"),
		},
		{
			name: "Absolute path",
			path: "/abs/path",
			want: "/abs/path",
		},
		{
			name: "Relative path",
			path: "./rel/path",
			want: "./rel/path",
		},
		{
			name: "Path with tilde in the middle",
			path: "/path/with/~/tilde",
			want: "/path/with/~/tilde",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandPath(tt.path)
			if got != tt.want {
				t.Errorf("ExpandPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
