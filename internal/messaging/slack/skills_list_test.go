package slack

import (
	"fmt"
	"strings"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

// cellText extracts the raw text of a DataTableCell for assertions.
func cellText(cell slack.DataTableCell) string {
	if c, ok := cell.(*slack.DataTableRawTextCell); ok {
		return c.Text
	}
	return ""
}

// TestBuildSkillGroupTableStatusColumn verifies the Status column: header row
// becomes "Name | Description | Status" and each data row carries the correct
// status cell for callable/unavailable/discoverable entries. An empty status
// (absent on the wire) must be treated as "discoverable" per the AEP contract
// (pkg/events/events.go:582-584).
func TestBuildSkillGroupTableStatusColumn(t *testing.T) {
	t.Parallel()

	g := messaging.SkillGroup{
		Source: messaging.SourceGlobal,
		Entries: []events.SkillEntry{
			{Name: "search", Description: "web search", Source: messaging.SourceGlobal, Status: events.SkillStatusCallable},
			{Name: "im", Description: "team messaging", Source: messaging.SourceGlobal, Status: events.SkillStatusUnavailable},
			{Name: "docs", Description: "documentation tooling", Source: messaging.SourceGlobal, Status: events.SkillStatusDiscoverable},
			{Name: "legacy", Description: "no status on wire", Source: messaging.SourceGlobal},
		},
	}

	table := buildSkillGroupTable(g, "skills_global_0")
	require.NotNil(t, table)
	require.Len(t, table.Rows, len(g.Entries)+1, "header row + one row per entry")

	header := table.Rows[0]
	require.Len(t, header, 3, "header must have Name/Description/Status columns")
	require.Equal(t, "Name", cellText(header[0]))
	require.Equal(t, "Description", cellText(header[1]))
	require.Equal(t, "Status", cellText(header[2]))

	tests := []struct {
		name string
		row  []slack.DataTableCell
		want string
	}{
		{"callable entry", table.Rows[1], "✅ callable"},
		{"unavailable entry", table.Rows[2], "🚫 unavailable"},
		{"discoverable entry", table.Rows[3], "🔎 discoverable"},
		{"empty status treated as discoverable", table.Rows[4], "🔎 discoverable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Len(t, tt.row, 3, "data row must keep 3 cells for column alignment")
			require.Equal(t, tt.want, cellText(tt.row[2]))
		})
	}
}

// TestBuildSkillGroupTableRowCap verifies the row cap: a group with more
// entries than maxDataTableRows-1 is truncated with a notice row, and the
// total row count (header + data + truncation) never exceeds maxDataTableRows
// so the block passes validator.go's DataTableBlock constraint.
func TestBuildSkillGroupTableRowCap(t *testing.T) {
	t.Parallel()

	entries := make([]events.SkillEntry, 0, 150)
	for i := range 150 {
		entries = append(entries, events.SkillEntry{
			Name:        fmt.Sprintf("skill-%03d", i),
			Description: "description",
			Source:      messaging.SourceProject,
		})
	}
	g := messaging.SkillGroup{Source: messaging.SourceProject, Entries: entries}

	table := buildSkillGroupTable(g, "skills_project_0")
	require.NotNil(t, table)

	// Total rows must stay within the Slack DataTableBlock limit (validator.go:24).
	require.LessOrEqual(t, len(table.Rows), maxDataTableRows)
	// At most maxDataTableRows-1 data rows (excluding the header).
	require.LessOrEqual(t, len(table.Rows)-1, maxDataTableRows-1)

	last := table.Rows[len(table.Rows)-1]
	require.Len(t, last, 3, "truncation row must keep 3 cells for column alignment")
	require.Equal(t, "...", cellText(last[0]))
	require.Contains(t, cellText(last[1]), "more")
}

// TestFormatSkillsPlainTextStatusMarkers verifies the plain-text fallback
// includes a status marker for every entry (callable/discoverable/unavailable,
// with empty status falling back to discoverable).
func TestFormatSkillsPlainTextStatusMarkers(t *testing.T) {
	t.Parallel()

	page := []messaging.SkillGroup{{
		Source: messaging.SourceProject,
		Entries: []events.SkillEntry{
			{Name: "search", Description: "web search", Source: messaging.SourceProject, Status: events.SkillStatusCallable},
			{Name: "im", Description: "team messaging", Source: messaging.SourceProject, Status: events.SkillStatusUnavailable},
			{Name: "legacy", Description: "no status on wire", Source: messaging.SourceProject},
		},
	}}

	text := formatSkillsPlainText(page)

	require.Contains(t, text, "✅ callable")
	require.Contains(t, text, "🚫 unavailable")
	require.Contains(t, text, "🔎 discoverable")
	require.Contains(t, text, strings.TrimSpace("search"), "entry name retained")
}
