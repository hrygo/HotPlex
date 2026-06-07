package cron

import (
	"context"
	"time"
)

// backoffDurations defines exponential backoff intervals for consecutive failures.
// Used for job execution retry (up to 1h); see delivery.retryBackoff for
// platform delivery retry (capped at 5m).
var backoffDurations = []time.Duration{
	30 * time.Second,
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	1 * time.Hour,
}

// backoff returns the backoff duration for the given consecutive error count.
// After exhausting the list, it returns 1 hour.
func backoff(consecutiveErrs int) time.Duration {
	if consecutiveErrs <= 0 {
		return 0
	}
	if consecutiveErrs >= len(backoffDurations) {
		return backoffDurations[len(backoffDurations)-1]
	}
	// First error (consecutiveErrs=1) should get the shortest backoff (30s).
	// Subtract 1 so the index maps: 1→0 (30s), 2→1 (1m), 3→2 (5m), etc.
	return backoffDurations[consecutiveErrs-1]
}

// maxRetries returns the effective max retries for a job (default 3).
func maxRetries(job *CronJob) int {
	if job.MaxRetries > 0 {
		return job.MaxRetries
	}
	return 3
}

// scheduleRetry advances the job's next_run to a backoff time for retry.
func (s *Scheduler) scheduleRetry(ctx context.Context, job *CronJob) {
	delay := backoff(job.State.ConsecutiveErrs)
	nextRun := time.Now().Add(delay)
	job.State.NextRunAtMs = nextRun.UnixMilli()
	job.State.RetryCount++
	if err := s.store.UpdateState(ctx, job.ID, job.State); err != nil {
		s.log.Error("cron: persist retry state", "job_id", job.ID, "err", err)
	}
	s.mergeJobState(job.ID, job.State, false)

	s.log.Info("cron: retry scheduled",
		"job_id", job.ID, "name", job.Name,
		"retry", job.State.RetryCount, "delay", delay, "next_run", nextRun.Format(time.RFC3339))
}

// resetRetry resets retry state after a successful execution.
func resetRetry(job *CronJob) {
	job.State.RetryCount = 0
}
