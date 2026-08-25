package checkers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestSlackCreds_NoTokens(t *testing.T) {
	// Reads the package-level configPath and falls back to process env;
	// must remain serial and pin the env to stay hermetic (a half-set
	// SLACK_BOT_TOKEN in the developer shell would otherwise flip
	// enabled=true and fail the check).

	t.Setenv("HOTPLEX_MESSAGING_SLACK_BOT_TOKEN", "")
	t.Setenv("HOTPLEX_MESSAGING_SLACK_APP_TOKEN", "")
	t.Setenv("SLACK_BOT_TOKEN", "")
	t.Setenv("SLACK_APP_TOKEN", "")

	c := slackCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "messaging.slack_creds", d.Name)
	require.Equal(t, "messaging", d.Category)
	require.Equal(t, cli.StatusPass, d.Status)
}

func TestSlackCreds_ValidTokens(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel in Go 1.26

	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test-token")
	t.Setenv("SLACK_APP_TOKEN", "xapp-test-token")

	c := slackCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestSlackCreds_InvalidPrefix(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel in Go 1.26

	t.Setenv("SLACK_BOT_TOKEN", "invalid-token")

	c := slackCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
}

func TestFeishuCreds_NoCreds(t *testing.T) {
	// Reads the package-level configPath and falls back to process env;
	// must remain serial and pin the env to stay hermetic (a half-set
	// FEISHU_APP_SECRET in the developer shell would otherwise flip
	// enabled=true and fail the check).

	t.Setenv("HOTPLEX_MESSAGING_FEISHU_APP_ID", "")
	t.Setenv("HOTPLEX_MESSAGING_FEISHU_APP_SECRET", "")
	t.Setenv("FEISHU_APP_ID", "")
	t.Setenv("FEISHU_APP_SECRET", "")

	c := feishuCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "messaging.feishu_creds", d.Name)
	require.Equal(t, "messaging", d.Category)
	require.Equal(t, cli.StatusPass, d.Status)
}

func TestFeishuCreds_ValidCreds(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel in Go 1.26

	t.Setenv("HOTPLEX_MESSAGING_FEISHU_APP_ID", "")
	t.Setenv("HOTPLEX_MESSAGING_FEISHU_APP_SECRET", "")
	t.Setenv("FEISHU_APP_ID", "cli_test123")
	t.Setenv("FEISHU_APP_SECRET", "secret123")

	c := feishuCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestFeishuCreds_UsesEffectiveConfig(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel in Go 1.26

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
messaging:
  feishu:
    enabled: true
`), 0o644))
	withConfigPath(t, path)
	t.Setenv("HOTPLEX_MESSAGING_FEISHU_APP_ID", "cli_effective")
	t.Setenv("HOTPLEX_MESSAGING_FEISHU_APP_SECRET", "effective-secret")
	t.Setenv("FEISHU_APP_ID", "")
	t.Setenv("FEISHU_APP_SECRET", "")

	d := (feishuCredsChecker{}).Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "present")
}

func TestFeishuCreds_EnabledWithoutEffectiveCredentials(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel in Go 1.26

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
messaging:
  feishu:
    enabled: true
`), 0o644))
	withConfigPath(t, path)
	t.Setenv("HOTPLEX_MESSAGING_FEISHU_APP_ID", "")
	t.Setenv("HOTPLEX_MESSAGING_FEISHU_APP_SECRET", "")
	t.Setenv("FEISHU_APP_ID", "")
	t.Setenv("FEISHU_APP_SECRET", "")

	d := (feishuCredsChecker{}).Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, "missing")
}

func TestFeishuCreds_WhitespaceOnly(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel in Go 1.26

	t.Setenv("FEISHU_APP_ID", "   ")

	c := feishuCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
}

func TestMultiBotConfig_NoConfigPath(t *testing.T) {
	// Writes the package-level configPath; must remain serial (see
	// withConfigPath in config_test.go).

	orig := getConfigPath()
	SetConfigPath("")
	defer func() { SetConfigPath(orig) }()

	c := multiBotConfigChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusPass, d.Status)
}

func TestMultiBotConfig_ValidSingleBot(t *testing.T) {
	dir := t.TempDir()
	withConfigPath(t, dir+"/config.yaml")
	require.NoError(t, os.WriteFile(getConfigPath(), []byte(`
messaging:
  slack:
    enabled: true
    bots:
      - name: default
        bot_token: xoxb-test
        app_token: xapp-test
`), 0o644))

	c := multiBotConfigChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "1 bot")
}

func TestMultiBotConfig_DuplicateName(t *testing.T) {
	dir := t.TempDir()
	withConfigPath(t, dir+"/config.yaml")
	require.NoError(t, os.WriteFile(getConfigPath(), []byte(`
messaging:
  slack:
    enabled: true
    bots:
      - name: bot1
        bot_token: xoxb-aaa
        app_token: xapp-aaa
      - name: bot1
        bot_token: xoxb-bbb
        app_token: xapp-bbb
`), 0o644))

	c := multiBotConfigChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, `duplicate bot name "bot1"`)
}

func TestMultiBotConfig_MissingCredentials(t *testing.T) {
	dir := t.TempDir()
	withConfigPath(t, dir+"/config.yaml")
	require.NoError(t, os.WriteFile(getConfigPath(), []byte(`
messaging:
  feishu:
    enabled: true
    bots:
      - name: empty-bot
        app_id: ""
        app_secret: ""
`), 0o644))

	c := multiBotConfigChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, `no credentials`)
}

func TestMultiBotConfig_ExceedsLimit(t *testing.T) {
	dir := t.TempDir()
	withConfigPath(t, dir+"/config.yaml")

	var botLines strings.Builder
	for i := range 11 {
		fmt.Fprintf(&botLines, "      - name: bot-%d\n        bot_token: xoxb-%d\n        app_token: xapp-%d\n", i, i, i)
	}
	require.NoError(t, os.WriteFile(getConfigPath(), []byte("messaging:\n  slack:\n    enabled: true\n    bots:\n"+botLines.String()), 0o644))

	c := multiBotConfigChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, "exceed limit")
}
