package config

import "sort"

// EnabledWorkerTypes returns the effective worker types referenced by enabled
// messaging platforms and their normalized bot configurations. It intentionally
// stays at the string contract boundary: internal/config cannot import the
// worker registry without creating an import cycle, and WebChat preferences do
// not belong to this static messaging configuration.
func (c *Config) EnabledWorkerTypes() []string {
	workers := make(map[string]struct{})
	if c == nil {
		return []string{}
	}

	add := func(platform string, enabled bool, botNames []string) {
		if !enabled {
			return
		}
		if len(botNames) == 0 {
			workers[c.ResolveWorkerType(platform, "")] = struct{}{}
			return
		}
		for _, botName := range botNames {
			workers[c.ResolveWorkerType(platform, botName)] = struct{}{}
		}
	}

	slackBots := make([]string, 0, len(c.Messaging.Slack.Bots))
	for _, bot := range c.Messaging.Slack.Bots {
		slackBots = append(slackBots, bot.Name)
	}
	add("slack", c.Messaging.Slack.Enabled, slackBots)

	feishuBots := make([]string, 0, len(c.Messaging.Feishu.Bots))
	for _, bot := range c.Messaging.Feishu.Bots {
		feishuBots = append(feishuBots, bot.Name)
	}
	add("feishu", c.Messaging.Feishu.Enabled, feishuBots)

	add("yuanxin", c.Messaging.Yuanxin.Enabled, nil)

	result := make([]string, 0, len(workers))
	for workerType := range workers {
		result = append(result, workerType)
	}
	sort.Strings(result)
	return result
}
