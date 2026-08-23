package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	croncli "github.com/hrygo/hotplex/internal/cli/cron"
)

func TestCronExamplesMatchCurrentCobraValidators(t *testing.T) {
	t.Parallel()

	cronRoot := newCronCmd()
	create, _, err := cronRoot.Find([]string{"create"})
	require.NoError(t, err)
	require.NotNil(t, create)

	examples := []struct {
		name     string
		args     []string
		attached bool
	}{
		{
			name: "isolated recurring every",
			args: []string{
				"--name", "health-check",
				"--message", "Read the current gateway health and report actionable failures.",
				"--schedule", "every:30m",
				"--bot-id", "<BOT_ID>",
				"--owner-id", "<OWNER_ID>",
				"--max-runs", "10",
				"--expires-at", "2030-01-01T00:00:00Z",
				"--timeout", "120",
				"--allowed-tools", "status,logs",
				"--silent",
			},
		},
		{
			name: "isolated cron expression",
			args: []string{
				"--name", "weekday-review",
				"--message", "Review the pending work.",
				"--schedule", "cron:0 9 * * 1-5",
				"--bot-id", "<BOT_ID>",
				"--owner-id", "<OWNER_ID>",
				"--max-runs", "10",
				"--expires-at", "2030-01-01T00:00:00Z",
			},
		},
		{
			name: "isolated absolute at",
			args: []string{
				"--name", "release-reminder",
				"--message", "Read the release status.",
				"--schedule", "at:2030-01-01T00:00:00Z",
				"--bot-id", "<BOT_ID>",
				"--owner-id", "<OWNER_ID>",
				"--delete-after-run",
				"--max-retries", "2",
				"--platform", "cron",
				"--platform-key", `{"channel_id":"<CHANNEL_ID>"}`,
			},
		},
		{
			name: "isolated relative at",
			args: []string{
				"--name", "one-shot-health-check",
				"--message", "Read the current gateway health and report the result.",
				"--schedule", "at:+10m",
				"--bot-id", "<BOT_ID>",
				"--owner-id", "<OWNER_ID>",
				"--platform", "cron",
				"--delete-after-run",
			},
		},
		{
			name:     "attached recurring",
			attached: true,
			args: []string{
				"--name", "attached-health-check",
				"--message", "Read the current gateway health.",
				"--schedule", "every:30m",
				"--max-runs", "3",
				"--expires-at", "2030-01-01T00:00:00Z",
				"--attach",
			},
		},
	}

	for _, example := range examples {
		example := example
		t.Run(example.name, func(t *testing.T) {
			t.Parallel()

			cmd := newCronCreateCmd()
			require.NoError(t, cmd.ParseFlags(example.args))
			require.NoError(t, cmd.ValidateRequiredFlags())

			schedule, err := cmd.Flags().GetString("schedule")
			require.NoError(t, err)
			parsed, err := croncli.ParseSchedule(schedule)
			require.NoError(t, err)
			require.NotEmpty(t, parsed.Kind)

			name, err := cmd.Flags().GetString("name")
			require.NoError(t, err)
			message, err := cmd.Flags().GetString("message")
			require.NoError(t, err)
			botID, err := cmd.Flags().GetString("bot-id")
			require.NoError(t, err)
			ownerID, err := cmd.Flags().GetString("owner-id")
			require.NoError(t, err)
			if example.attached {
				// The command resolves these fields from GATEWAY_* at execution
				// time; the parser-only test supplies safe placeholders so the
				// pure job validator can check the attached form as well.
				botID = "<BOT_ID>"
				ownerID = "<OWNER_ID>"
			}
			allowedToolsRaw, err := cmd.Flags().GetString("allowed-tools")
			require.NoError(t, err)
			var allowedTools []string
			if allowedToolsRaw != "" {
				allowedTools = strings.Split(allowedToolsRaw, ",")
			}

			platformKeyRaw, err := cmd.Flags().GetString("platform-key")
			require.NoError(t, err)
			var platformKey map[string]string
			if platformKeyRaw != "" {
				require.NoError(t, json.Unmarshal([]byte(platformKeyRaw), &platformKey))
			}
			maxRuns, err := cmd.Flags().GetInt("max-runs")
			require.NoError(t, err)
			expiresAt, err := cmd.Flags().GetString("expires-at")
			require.NoError(t, err)
			deleteAfterRun, err := cmd.Flags().GetBool("delete-after-run")
			require.NoError(t, err)
			silent, err := cmd.Flags().GetBool("silent")
			require.NoError(t, err)
			maxRetries, err := cmd.Flags().GetInt("max-retries")
			require.NoError(t, err)
			timeout, err := cmd.Flags().GetInt("timeout")
			require.NoError(t, err)
			platform, err := cmd.Flags().GetString("platform")
			require.NoError(t, err)
			workerType, err := cmd.Flags().GetString("worker-type")
			require.NoError(t, err)

			attach, err := cmd.Flags().GetBool("attach")
			require.NoError(t, err)
			require.Equal(t, example.attached, attach)
			job, err := croncli.PrepareJobForCreate(name, schedule, message, "", "", botID, "", ownerID, timeout, allowedTools, croncli.JobCreateOptions{
				DeleteAfterRun:  deleteAfterRun,
				Silent:          silent,
				MaxRetries:      maxRetries,
				MaxRuns:         maxRuns,
				ExpiresAt:       expiresAt,
				Platform:        platform,
				PlatformKey:     platformKey,
				WorkerType:      workerType,
				Attach:          attach,
				TargetSessionID: "<SESSION_ID>",
			})
			require.NoError(t, err)
			require.Equal(t, name, job.Name)
			require.Equal(t, parsed.Kind, job.Schedule.Kind)
		})
	}
}

func TestCronCreateExamplesHaveIndependentJSONReadback(t *testing.T) {
	t.Parallel()

	for _, relativePath := range []string{
		"internal/skills/builtin/hotplex-cli/references/cron.md",
		"docs/tutorials/cron-scheduled-tasks.md",
	} {
		data, err := os.ReadFile(filepath.Join(skillExamplesRepositoryRoot(t), filepath.FromSlash(relativePath)))
		require.NoError(t, err, relativePath)
		text := string(data)
		lines := strings.Split(text, "\n")
		var createLines []int
		for index, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "hotplex cron create \\") {
				createLines = append(createLines, index)
			}
		}
		require.NotEmpty(t, createLines, relativePath)
		getPattern := regexp.MustCompile(`hotplex cron get\s+<[^>\n]+>\s+--json`)
		for index, line := range createLines {
			next := len(lines)
			if index+1 < len(createLines) {
				next = createLines[index+1]
			}
			segment := strings.Join(lines[line:next], "\n")
			require.Regexp(t, getPattern, segment, relativePath)
		}
		require.Contains(t, text, "--json", relativePath)
	}
}

func skillExamplesRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}
