package checkers

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

type slackCredsChecker struct{}

func (c slackCredsChecker) Name() string     { return "messaging.slack_creds" }
func (c slackCredsChecker) Category() string { return "messaging" }
func (c slackCredsChecker) Check(ctx context.Context) cli.Diagnostic {
	platform, err := loadMessagingPlatform("slack")
	if err != nil {
		return configLoadDiagnostic(c.Name(), c.Category(), err)
	}
	if !platform.enabled {
		return platformDisabledDiagnostic(c.Name(), c.Category(), "Slack")
	}

	issues := validateCredentialEntries("Slack", platform.bots, func(b botCheck) []string {
		var issues []string
		if b.Cred1 != "" && !strings.HasPrefix(b.Cred1, "xoxb-") {
			issues = append(issues, "bot token has invalid prefix (expected xoxb-)")
		}
		if b.Cred2 != "" && !strings.HasPrefix(b.Cred2, "xapp-") {
			issues = append(issues, "app token has invalid prefix (expected xapp-)")
		}
		return issues
	})
	if len(issues) > 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Invalid Slack credentials: " + strings.Join(issues, "; "),
			FixHint:  "Check effective Slack credentials in the config or its adjacent .env file",
		}
	}

	return cli.Diagnostic{Name: c.Name(), Category: c.Category(), Status: cli.StatusPass, Message: "Slack credentials present and valid"}
}

type feishuCredsChecker struct{}

func (c feishuCredsChecker) Name() string     { return "messaging.feishu_creds" }
func (c feishuCredsChecker) Category() string { return "messaging" }
func (c feishuCredsChecker) Check(ctx context.Context) cli.Diagnostic {
	platform, err := loadMessagingPlatform("feishu")
	if err != nil {
		return configLoadDiagnostic(c.Name(), c.Category(), err)
	}
	if !platform.enabled {
		return platformDisabledDiagnostic(c.Name(), c.Category(), "Feishu")
	}

	issues := validateCredentialEntries("Feishu", platform.bots, nil)
	if len(issues) > 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Invalid Feishu credentials: " + strings.Join(issues, "; "),
			FixHint:  "Check effective Feishu credentials in the config or its adjacent .env file",
		}
	}

	return cli.Diagnostic{Name: c.Name(), Category: c.Category(), Status: cli.StatusPass, Message: "Feishu credentials present and valid"}
}

type messagingPlatform struct {
	enabled bool
	bots    []botCheck
}

func loadMessagingPlatform(platform string) (messagingPlatform, error) {
	cfg, err := loadConfig()
	if err != nil {
		return messagingPlatform{}, err
	}
	if cfg == nil {
		// Standalone checker calls have no config path. Keep the legacy fallback
		// for compatibility, while all doctor/onboard calls use effective config.
		if platform == "slack" {
			botToken := firstNonEmptyEnv("HOTPLEX_MESSAGING_SLACK_BOT_TOKEN", "SLACK_BOT_TOKEN")
			appToken := firstNonEmptyEnv("HOTPLEX_MESSAGING_SLACK_APP_TOKEN", "SLACK_APP_TOKEN")
			return messagingPlatform{enabled: botToken != "" || appToken != "", bots: []botCheck{{Name: "slack", Cred1: botToken, Cred2: appToken}}}, nil
		}
		appID := firstNonEmptyEnv("HOTPLEX_MESSAGING_FEISHU_APP_ID", "FEISHU_APP_ID")
		appSecret := firstNonEmptyEnv("HOTPLEX_MESSAGING_FEISHU_APP_SECRET", "FEISHU_APP_SECRET")
		return messagingPlatform{enabled: appID != "" || appSecret != "", bots: []botCheck{{Name: "feishu", Cred1: appID, Cred2: appSecret}}}, nil
	}

	switch platform {
	case "slack":
		return messagingPlatform{enabled: cfg.Messaging.Slack.Enabled, bots: mapSlackBots(cfg.Messaging.Slack.Bots)}, nil
	case "feishu":
		return messagingPlatform{enabled: cfg.Messaging.Feishu.Enabled, bots: mapFeishuBots(cfg.Messaging.Feishu.Bots)}, nil
	default:
		return messagingPlatform{}, fmt.Errorf("unsupported messaging platform %q", platform)
	}
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func validateCredentialEntries(platform string, bots []botCheck, formatFn func(botCheck) []string) []string {
	if len(bots) == 0 {
		return []string{strings.ToLower(platform) + " credentials are missing"}
	}
	firstName, secondName := "primary credential", "secondary credential"
	if strings.EqualFold(platform, "slack") {
		firstName, secondName = "bot_token", "app_token"
	} else if strings.EqualFold(platform, "feishu") {
		firstName, secondName = "app_id", "app_secret"
	}
	var issues []string
	for _, bot := range bots {
		prefix := strings.ToLower(platform)
		if bot.Name != "" {
			prefix += " bot " + fmt.Sprintf("%q", bot.Name)
		}
		if strings.TrimSpace(bot.Cred1) == "" {
			issues = append(issues, prefix+" "+firstName+" is missing")
		}
		if strings.TrimSpace(bot.Cred2) == "" {
			issues = append(issues, prefix+" "+secondName+" is missing")
		}
		if formatFn != nil {
			for _, issue := range formatFn(bot) {
				issues = append(issues, prefix+": "+issue)
			}
		}
	}
	return issues
}

func configLoadDiagnostic(name, category string, err error) cli.Diagnostic {
	return cli.Diagnostic{
		Name: name, Category: category, Status: cli.StatusWarn,
		Message: "Cannot load effective config", Detail: err.Error(),
		FixHint: "Fix config syntax errors first",
	}
}

func platformDisabledDiagnostic(name, category, platform string) cli.Diagnostic {
	return cli.Diagnostic{Name: name, Category: category, Status: cli.StatusPass, Message: platform + " disabled in effective config"}
}

func init() {
	cli.DefaultRegistry.Register(slackCredsChecker{})
	cli.DefaultRegistry.Register(feishuCredsChecker{})
	cli.DefaultRegistry.Register(multiBotConfigChecker{})
}

// ─── messaging.multi_bot_config ─────────────────────────────────────────────

type multiBotConfigChecker struct{}

func (c multiBotConfigChecker) Name() string     { return "messaging.multi_bot_config" }
func (c multiBotConfigChecker) Category() string { return "messaging" }
func (c multiBotConfigChecker) Check(ctx context.Context) cli.Diagnostic {
	cfg, err := loadConfig()
	if cfg == nil && err == nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Config path not set, skipping multi-bot check",
		}
	}
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Cannot load config: " + err.Error(),
			FixHint:  "Fix config syntax errors first",
		}
	}

	var issues []string
	issues = append(issues, checkBotEntries("slack", mapSlackBots(cfg.Messaging.Slack.Bots))...)
	issues = append(issues, checkBotEntries("feishu", mapFeishuBots(cfg.Messaging.Feishu.Bots))...)

	if len(issues) == 0 {
		totalBots := len(cfg.Messaging.Slack.Bots) + len(cfg.Messaging.Feishu.Bots)
		if totalBots == 0 {
			return cli.Diagnostic{
				Name:     c.Name(),
				Category: c.Category(),
				Status:   cli.StatusPass,
				Message:  "No bots configured",
			}
		}
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  fmt.Sprintf("Multi-bot config valid (%d bot(s))", totalBots),
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusFail,
		Message:  strings.Join(issues, "; "),
		FixHint:  "Fix multi-bot config: ensure unique names, non-empty credentials, max 10 bots per platform",
	}
}

type botCheck struct {
	Name  string
	Cred1 string
	Cred2 string
}

func checkBotEntries(platform string, bots []botCheck) []string {
	var issues []string
	if len(bots) > config.MaxBotsPerPlatform {
		issues = append(issues, fmt.Sprintf("%s: %d bots exceed limit (max %d)", platform, len(bots), config.MaxBotsPerPlatform))
	}
	seen := make(map[string]bool, len(bots))
	for _, b := range bots {
		if b.Name == "" {
			issues = append(issues, fmt.Sprintf("%s: bot missing name", platform))
			continue
		}
		if seen[b.Name] {
			issues = append(issues, fmt.Sprintf("%s: duplicate bot name %q", platform, b.Name))
		}
		seen[b.Name] = true
		if strings.TrimSpace(b.Cred1) == "" && strings.TrimSpace(b.Cred2) == "" {
			issues = append(issues, fmt.Sprintf("%s: bot %q has no credentials", platform, b.Name))
		}
	}
	return issues
}

func mapSlackBots(bots []config.SlackBotConfig) []botCheck {
	result := make([]botCheck, len(bots))
	for i, b := range bots {
		result[i] = botCheck{Name: b.Name, Cred1: b.BotToken, Cred2: b.AppToken}
	}
	return result
}

func mapFeishuBots(bots []config.FeishuBotConfig) []botCheck {
	result := make([]botCheck, len(bots))
	for i, b := range bots {
		result[i] = botCheck{Name: b.Name, Cred1: b.AppID, Cred2: b.AppSecret}
	}
	return result
}
