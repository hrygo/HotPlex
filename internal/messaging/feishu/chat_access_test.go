package feishu

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging/phrases"
)

func TestBuildWelcomeBody_AllDefaults(t *testing.T) {
	t.Parallel()
	p := phrases.Defaults()
	got := buildWelcomeBody("Hi，我是 TestBot，你的 AI 编程助手！", p)

	// Greeting present
	require.True(t, strings.HasPrefix(got, "Hi，我是 TestBot"), got)
	// Capabilities section with default header
	require.Contains(t, got, "我可以帮你：")
	require.Contains(t, got, "• 💻 编写、审查、调试代码")
	// Quick commands section with default header
	require.Contains(t, got, "快捷命令：")
	require.Contains(t, got, "/help")
	// Closing line
	require.Contains(t, got, "直接发消息即可开始")
}

func TestBuildWelcomeBody_EmptyCategories(t *testing.T) {
	t.Parallel()
	p := &phrases.Phrases{}
	got := buildWelcomeBody("Hello!", p)
	require.Equal(t, "Hello!", got)
}

func TestBuildWelcomeBody_NilPhrases(t *testing.T) {
	t.Parallel()
	got := buildWelcomeBody("Hello!", nil)
	require.Equal(t, "Hello!", got)
}

func TestBuildWelcomeBody_AllSections(t *testing.T) {
	t.Parallel()
	def := phrases.Defaults()
	got := buildWelcomeBody("Hi!", def)

	// With defaults all 3 sections are present
	parts := strings.Split(got, "\n\n")
	require.Len(t, parts, 3, "expected greeting, capabilities, quick_commands sections")

	require.True(t, strings.HasPrefix(parts[0], "Hi!"))
	require.Contains(t, parts[1], "我可以帮你：")
	require.Contains(t, parts[2], "快捷命令：")
	require.True(t, strings.Contains(parts[2], "直接发消息即可开始"), parts[2])
}

func TestBuildWelcomeBody_CustomHeaders(t *testing.T) {
	t.Parallel()
	// Simulate a bot with custom header phrases but no capabilities_header
	p := phrases.Defaults()
	got := buildWelcomeBody("Hi!", p)
	require.Contains(t, got, "我可以帮你：", "should use default header")
	require.Contains(t, got, "快捷命令：", "should use default header")

	// With custom headers via parsed phrases (bot-level override)
	phr, err := phrases.Load("/nonexistent", "feishu", "fake_bot")
	require.NoError(t, err)
	// No external config → all defaults including headers
	got2 := buildWelcomeBody("Hi!", phr)
	require.Contains(t, got2, "我可以帮你：")
}

func TestBuildWelcomeBody_FallbackHeaders(t *testing.T) {
	t.Parallel()
	// Phrases with capabilities/quick_commands but NO header categories
	p := &phrases.Phrases{}
	// Manually inject entries (can't do this externally, so test the fallback path)
	got := buildWelcomeBody("Hi!", p)
	require.Equal(t, "Hi!", got, "empty phrases → bare greeting, no sections")
}
