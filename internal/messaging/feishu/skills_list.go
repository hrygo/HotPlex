package feishu

import (
	"context"
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func (c *FeishuConn) sendSkillsList(ctx context.Context, env *events.Envelope) error {
	d, err := messaging.ExtractSkillsListData(env)
	if err != nil {
		return err
	}
	if len(d.Skills) == 0 {
		return c.sendSkillsText(ctx, messaging.FormatEmptySkillsMsg(d.Filter))
	}

	groups := messaging.GroupSkillsBySource(d.Skills)
	pages := messaging.PaginateSkillGroups(groups, messaging.SkillsPerPage)

	for i, page := range pages {
		header := messaging.SkillsHeader(d, i+1, len(pages))

		var sb strings.Builder
		sb.WriteString(header)
		sb.WriteByte('\n')

		for _, g := range page {
			for _, s := range g.Entries {
				sb.WriteString(formatSkillLine(s))
				sb.WriteByte('\n')
			}
		}

		if err := c.sendSkillsText(ctx, sb.String()); err != nil {
			return err
		}
	}
	return nil
}

// formatSkillLine renders one skill entry as "{marker} *{name}* — {desc} ({suffixes})".
// An empty/unknown status is treated as discoverable per pkg/events.SkillStatus wire semantics.
func formatSkillLine(s events.SkillEntry) string {
	marker, statusSuffix := "", ""
	switch s.Status {
	case events.SkillStatusCallable:
		marker = "✅"
	case events.SkillStatusDiscoverable:
		marker, statusSuffix = "🔍", "仅可发现"
	case events.SkillStatusUnavailable:
		marker, statusSuffix = "🚫", "不可用"
	default:
		marker, statusSuffix = "🔍", "仅可发现"
	}

	desc := messaging.TruncateDesc(s.Description)

	var suffixes []string
	if statusSuffix != "" {
		suffixes = append(suffixes, statusSuffix)
	}
	if s.Source == messaging.SourceProject || s.Source == messaging.SourceGlobal {
		suffixes = append(suffixes, s.Source)
	}

	line := fmt.Sprintf("%s *%s* — %s", marker, s.Name, desc)
	if len(suffixes) > 0 {
		line += " (" + strings.Join(suffixes, ", ") + ")"
	}
	return line
}

func (c *FeishuConn) sendSkillsText(ctx context.Context, text string) error {
	return c.sendOrReply(ctx, text)
}
