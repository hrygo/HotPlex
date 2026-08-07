package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/slack-go/slack"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func (c *SlackConn) sendSkillsList(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractSkillsListData(env)
	if err != nil {
		return err
	}
	if len(d.Skills) == 0 {
		return c.postSkillsMessage(ctx, messaging.FormatEmptySkillsMsg(d.Filter), nil)
	}

	groups := messaging.GroupSkillsBySource(d.Skills)
	// page=1, total=1: non-paginated display, suppresses "Part X/Y" suffix.
	header := messaging.SkillsHeader(d, 1, 1)

	// Build DataTableBlocks — one table per skill group.
	var blocks []slack.Block
	var shown int
	blocks = append(blocks, slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.PlainTextType, header, false, false), nil, nil))

	for i, g := range groups {
		// Reserve 1 slot for the header SectionBlock above.
		if len(blocks) >= maxBlocksPerMessage-1 {
			break
		}
		blocks = append(blocks, buildSkillGroupTable(g, fmt.Sprintf("skills_%s_%d", g.Source, i)))
		shown = i + 1
	}
	// Append truncation notice if some groups were omitted (very rare: requires 99+ sources).
	if shown < len(groups) {
		remaining := len(groups) - shown
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.PlainTextType,
				fmt.Sprintf("… and %d more group(s) — use `$skills` for full list", remaining), false, false),
			nil, nil))
	}

	fallback := header + "\n" + formatSkillsPlainText(groups)
	return c.postSkillsMessage(ctx, fallback, blocks)
}

// skillStatusText maps a SkillStatus to a human-readable status marker.
// An empty status (absent on the wire) means the Worker could not confirm
// invokability and must be treated as "discoverable" per the AEP contract.
func skillStatusText(s events.SkillStatus) string {
	switch s {
	case events.SkillStatusCallable:
		return "✅ callable"
	case events.SkillStatusUnavailable:
		return "🚫 unavailable"
	default:
		return "🔎 discoverable"
	}
}

// buildSkillGroupTable creates a DataTableBlock for a single skill group.
func buildSkillGroupTable(g messaging.SkillGroup, blockID string) *slack.DataTableBlock {
	emoji := messaging.SourceEmoji(g.Source)
	caption := fmt.Sprintf("%s %s (%d)", emoji, g.Source, len(g.Entries))

	table := slack.NewDataTableBlock(caption, slack.DataTableBlockOptionBlockID(blockID))

	// Header row.
	table.AddRow(dataTableCell("Name"), dataTableCell("Description"), dataTableCell("Status"))

	// Data rows. Cap at maxDataTableRows-1 (excluding header) to prevent Slack
	// rejection; when overflowing, reserve the last slot for the truncation
	// notice so header + rows stay within maxDataTableRows (validator.go:24).
	maxRows := maxDataTableRows - 1
	if len(g.Entries) > maxRows {
		maxRows--
	}
	for i, s := range g.Entries {
		if i >= maxRows {
			table.AddRow(dataTableCell("..."),
				dataTableCell(fmt.Sprintf("and %d more", len(g.Entries)-maxRows)),
				dataTableCell(""))
			break
		}
		table.AddRow(dataTableCell(s.Name),
			dataTableCell(messaging.TruncateDesc(s.Description)),
			dataTableCell(skillStatusText(s.Status)))
	}

	return table
}

// postSkillsMessageFallback sends skills as plain text when blocks are rejected.
func (c *SlackConn) postSkillsMessageFallback(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractSkillsListData(env)
	if err != nil {
		return err
	}
	if len(d.Skills) == 0 {
		return c.postSkillsMessage(ctx, messaging.FormatEmptySkillsMsg(d.Filter), nil)
	}

	groups := messaging.GroupSkillsBySource(d.Skills)
	pages := messaging.PaginateSkillGroups(groups, messaging.SkillsPerPage)

	for i, page := range pages {
		header := messaging.SkillsHeader(d, i+1, len(pages))
		text := header + "\n" + formatSkillsPlainText(page)
		if err := c.postSkillsMessage(ctx, text, nil); err != nil {
			return err
		}
	}
	return nil
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

func formatSkillsPlainText(page []messaging.SkillGroup) string {
	var sb strings.Builder
	for _, g := range page {
		emoji := messaging.SourceEmoji(g.Source)
		fmt.Fprintf(&sb, "\n*%s %s (%d)*\n", emoji, g.Source, len(g.Entries))
		for _, s := range g.Entries {
			desc := messaging.TruncateDesc(s.Description)
			fmt.Fprintf(&sb, "• %s — %s — %s\n", s.Name, desc, skillStatusText(s.Status))
		}
	}
	return sb.String()
}
