package builtin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseEmbeddedFrontmatterSupportsQuotedColon(t *testing.T) {
	name, description, ok := parseEmbeddedFrontmatter([]byte("---\nname: \"demo-skill\"\ndescription: \"Does: useful things\"\n---\n"))
	require.True(t, ok)
	require.Equal(t, "demo-skill", name)
	require.Equal(t, "Does: useful things", description)
}

func TestParseEmbeddedFrontmatterSupportsFoldedDescription(t *testing.T) {
	name, description, ok := parseEmbeddedFrontmatter([]byte("---\nname: demo-skill\ndescription: >\n  first line\n  second line\n---\n"))
	require.True(t, ok)
	require.Equal(t, "demo-skill", name)
	require.Equal(t, "first line second line", description)
}
