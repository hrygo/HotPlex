package cron

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// RequiredPlatformKey maps each platform to the PlatformKey field required
// for CLI-based result delivery. Used by ValidateJob, HasCLIDelivery, and
// buildDeliverySuffix to avoid duplicating platform→key mappings.
var RequiredPlatformKey = map[string]string{
	"feishu": "chat_id",
	"slack":  "channel_id",
}

var threatPatterns = []string{
	"ignore previous instructions",
	"ignore all above",
	"ignore all instructions",
	"forget your instructions",
	"disregard your training",
	"system prompt override",
	"new instructions:",
	"override previous",
	"jailbreak",
}

// stripInvisible removes zero-width characters, control chars, and homoglyph
// noise from a string to harden injection detection against evasion.
func stripInvisible(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			continue
		}
		// Skip common zero-width and formatting codepoints.
		if unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ValidateJobPrompt scans for obvious prompt injection patterns.
func ValidateJobPrompt(prompt string) error {
	if prompt == "" {
		return errors.New("cron: prompt must not be empty")
	}
	if len(prompt) > 4096 {
		return fmt.Errorf("cron: prompt exceeds 4KB limit (%d bytes)", len(prompt))
	}
	cleaned := strings.ToLower(stripInvisible(prompt))
	for _, pat := range threatPatterns {
		if strings.Contains(cleaned, pat) {
			return fmt.Errorf("cron: potential prompt injection detected")
		}
	}
	return nil
}

// SanitizeJobName strips control characters and newlines from a job name
// to prevent injection when the name is embedded in worker prompts.
func SanitizeJobName(name string) string {
	// stripInvisible handles Unicode Cf and most control chars but preserves
	// \t and \n. We additionally strip those since newlines in job names
	// would break prompt formatting.
	return stripNewlinesTabs(stripInvisible(name))
}

// stripNewlinesTabs removes \n and \t from a string.
func stripNewlinesTabs(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// SanitizePrompt strips invisible characters from a prompt and returns the
// cleaned version suitable for storage. Called after ValidateJobPrompt passes
// to ensure the persisted text matches what was validated.
func SanitizePrompt(prompt string) string {
	return stripInvisible(prompt)
}

// formatJobPrompt builds the standard cron prompt with job metadata and timestamp.
func formatJobPrompt(job *CronJob, scheduledAt time.Time) string {
	return fmt.Sprintf("[cron:%s %s] %s\n%s",
		job.ID, SanitizeJobName(job.Name),
		job.Payload.Message, scheduledAt.Format(time.RFC3339))
}

// ValidateJob performs full validation on a CronJob before creation/update.
func ValidateJob(job *CronJob) error {
	if job.Name == "" {
		return errors.New("cron: name is required")
	}
	if len(job.Name) > 128 {
		return fmt.Errorf("cron: name exceeds 128 character limit (%d chars)", len(job.Name))
	}
	// Reject names with control characters or newlines that could enable
	// prompt injection when the name is embedded in worker prompts.
	for _, r := range job.Name {
		if unicode.IsControl(r) {
			return errors.New("cron: name contains control characters")
		}
	}
	if job.OwnerID == "" {
		return errors.New("cron: owner_id is required")
	}
	if job.BotID == "" {
		return errors.New("cron: bot_id is required")
	}
	if err := ValidateSchedule(job.Schedule); err != nil {
		return err
	}
	if err := ValidateJobPrompt(job.Payload.Message); err != nil {
		return err
	}
	// Attached session validation.
	if job.Payload.Kind == PayloadAttachedSession {
		if job.Payload.TargetSessionID == "" {
			return errors.New("cron: target_session_id is required for attached_session")
		}
		if job.Schedule.Kind == ScheduleCron {
			return errors.New("cron: attached_session does not support cron expression schedules")
		}
	}
	// Platform delivery validation: each platform requires a specific key in platform_key.
	if key, ok := RequiredPlatformKey[job.Platform]; ok {
		if job.PlatformKey[key] == "" {
			return fmt.Errorf("cron: %s platform requires %s in platform_key", job.Platform, key)
		}
	}
	// Recurring jobs must have lifecycle constraints to prevent infinite execution.
	if job.Schedule.Kind != ScheduleAt {
		if job.MaxRuns <= 0 {
			return errors.New("cron: max_runs is required for recurring jobs (every/cron)")
		}
		if job.ExpiresAt == "" {
			return errors.New("cron: expires_at is required for recurring jobs (every/cron)")
		}
		if _, err := time.Parse(time.RFC3339, job.ExpiresAt); err != nil {
			return fmt.Errorf("cron: invalid expires_at: %w", err)
		}
	}
	return nil
}
