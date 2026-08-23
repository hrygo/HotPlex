package main

import (
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
	root.PersistentFlags().String("secret-token", "", "secret value")

	rendered, err := renderPublicCLISurface(root)
	require.NoError(t, err)
	text := string(rendered)
	require.Contains(t, text, "hotplex cron create")
	require.Contains(t, text, "--schedule")
	require.NotContains(t, text, "hidden-internal")
	require.NotContains(t, text, "secret-token")
	require.NotContains(t, text, "v1.41.0")
	require.NotContains(t, text, "~/.hotplex")
	require.NotContains(t, text, "/Users/")
	require.NotContains(t, text, "GATEWAY_")
}

func TestRenderPublicCLISurfaceIsDeterministicAndSkipsPathFlags(t *testing.T) {
	t.Parallel()

	root := newRootCmd()
	first, err := renderPublicCLISurface(root)
	require.NoError(t, err)
	second, err := renderPublicCLISurface(root)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.NotContains(t, first, "--config")
	require.NotContains(t, first, "--file")
	require.NotContains(t, first, "--output")
	require.NotContains(t, first, "--path")
	require.NotContains(t, first, string(filepath.Separator))
	rendered := string(first)
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
