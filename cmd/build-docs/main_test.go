package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHasAnchorID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		frag string
		want bool
	}{
		{"ascii id match", `<h2 id="authentication">`, "authentication", true},
		{"heading with surrounding tags", `<html><body><h3 id="oauth-config">X</h3></body></html>`, "oauth-config", true},
		{"no matching id", `<h2 id="other">`, "authentication", false},
		{"data-id must not be matched as anchor", `<div data-id="section">`, "section", false},
		{"percent-encoded cjk fragment decodes to non-ascii, no ascii id matches", `<h2 id="auth">`, "%E8%AE%A4%E8%AF%81", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, hasAnchorID([]byte(tt.data), tt.frag))
		})
	}
}

func TestValidateLinksDetectsBrokenAnchorsAndFiles(t *testing.T) {
	t.Parallel()
	dest := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dest, "target.html"),
		[]byte(`<html><body><h2 id="section">Section</h2></body></html>`), 0o644))
	// page.html: one valid anchor, one broken anchor, one broken file, one external link.
	require.NoError(t, os.WriteFile(filepath.Join(dest, "page.html"),
		[]byte(`<html><body>`+
			`<a href="target.html#section">ok</a>`+
			`<a href="target.html#missing">broken anchor</a>`+
			`<a href="missing.html">broken file</a>`+
			`<a href="https://example.com">external</a>`+
			`</body></html>`), 0o644))

	err := validateLinks(dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "target.html#missing")
	require.Contains(t, err.Error(), "missing.html")
	require.NotContains(t, err.Error(), "target.html#section", "valid anchor must not be reported")
}

func TestValidateLinksPassesOnAllValid(t *testing.T) {
	t.Parallel()
	dest := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dest, "target.html"),
		[]byte(`<html><body><h2 id="section">Section</h2></body></html>`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "page.html"),
		[]byte(`<html><body><a href="target.html#section">ok</a></body></html>`), 0o644))

	require.NoError(t, validateLinks(dest))
}
