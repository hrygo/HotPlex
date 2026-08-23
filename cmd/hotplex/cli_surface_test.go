package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestRenderPublicCLISurfaceFiltersHiddenAndSensitiveValues(t *testing.T) {
	t.Parallel()

	root := newRootCmd()
	root.AddCommand(&cobra.Command{Use: "hidden-internal", Hidden: true})
	root.PersistentFlags().String("secret-token", "example-secret", "secret value")

	rendered, err := renderPublicCLISurface(root)
	require.NoError(t, err)
	text := string(rendered)
	require.Contains(t, text, "hotplex cron create")
	require.Contains(t, text, "--schedule")
	require.Contains(t, text, "hotplex config validate")
	require.Contains(t, text, "--config <string>")
	require.Contains(t, text, "hotplex slack upload-file")
	require.Contains(t, text, "--file <string>")
	require.Contains(t, text, "hotplex slack download-file")
	require.Contains(t, text, "--output <string>")
	require.Contains(t, text, "--secret-token <string>")
	require.NotContains(t, text, "hidden-internal")
	require.NotContains(t, text, "example-secret")
	require.NotContains(t, text, "v1.41.0")
	require.NotContains(t, text, "~/.hotplex")
	require.NotContains(t, text, "/Users/")
	require.NotContains(t, text, "GATEWAY_")
}

func TestRenderPublicCLISurfaceIsDeterministicAndPreservesPublicFlagNames(t *testing.T) {
	t.Parallel()

	root := newRootCmd()
	first, err := renderPublicCLISurface(root)
	require.NoError(t, err)
	second, err := renderPublicCLISurface(root)
	require.NoError(t, err)
	require.Equal(t, first, second)
	rendered := string(first)
	require.Contains(t, rendered, "--config <string>")
	require.Contains(t, rendered, "--file <string>")
	require.Contains(t, rendered, "--output <string>")
	require.Contains(t, rendered, "--path <string>")
	require.True(t, bytes.HasSuffix(first, []byte("\n")))
	require.False(t, bytes.HasSuffix(first, []byte("\n\n")))
	require.True(t, strings.Index(rendered, "hotplex cron create") < strings.Index(rendered, "hotplex slack"))
}

func TestInternalCLISurfaceGenerationWritesOnlyRequestedTempFile(t *testing.T) {
	t.Parallel()

	output := filepath.Join(t.TempDir(), "cli-surface.generated.md")
	handled, err := runInternalCLISurface([]string{
		internalCLISurfaceFlag,
		"--output",
		output,
	})
	require.True(t, handled)
	require.NoError(t, err)
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Contains(t, string(data), "hotplex cron create")
}
