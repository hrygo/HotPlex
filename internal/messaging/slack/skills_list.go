package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/slack-go/slack"

	"github.com/hrygo/hotplex/pkg/events"
)

func (c *SlackConn) sendSkillsList(ctx context.Context, env *events.Envelope) error {
	if c.adapter == nil || c.adapter.client == nil {
		return fmt.Errorf("slack: client not initialized")
	}

	var d events.SkillsListData
	switch v := env.Event.Data.(type) {
	case events.SkillsListData:
		d = v
	case map[string]any:
		raw, _ := json.Marshal(v)
		_ = json.Unmarshal(raw, &d)
	default:
		return nil
	}

	if len(d.Skills) == 0 {
		return c.postSkillsMessage(ctx, "⚡ No skills found.", nil)
	}

	plainText := fmt.Sprintf("⚡ Skills (%d)", d.Total)
	var blocks []slack.Block

	header := slack.NewTextBlockObject(slack.PlainTextType, plainText, false, false)
	blocks = append(blocks, slack.NewSectionBlock(header, nil, nil))

	var sb strings.Builder
	for _, s := range d.Skills {
		icon := "🌐"
		if s.Source == "project" {
			icon = "📁"
		}
		desc := s.Description
		if len([]rune(desc)) > 80 {
			desc = string([]rune(desc)[:77]) + "..."
		}
		fmt.Fprintf(&sb, "%s *%s* — %s\n", icon, s.Name, desc)
	}

	body := slack.NewTextBlockObject(slack.MarkdownType, sb.String(), false, false)
	blocks = append(blocks, slack.NewSectionBlock(body, nil, nil))

	if len(blocks) > 50 {
		blocks = blocks[:50]
	}

	return c.postSkillsMessage(ctx, plainText+"\n"+sb.String(), blocks)
}

func (c *SlackConn) postSkillsMessage(ctx context.Context, fallback string, blocks []slack.Block) error {
	opts := []slack.MsgOption{slack.MsgOptionText(fallback, false)}
	if len(blocks) > 0 {
		opts = append(opts, slack.MsgOptionBlocks(blocks...))
	}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}
	_, _, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	return err
}
