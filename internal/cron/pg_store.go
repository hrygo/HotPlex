package cron

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/dbutil"
)

// pgStore implements Store using PostgreSQL.
type pgStore struct {
	db      *dbutil.DB
	dialect dbutil.Dialect
	log     *slog.Logger
}

// NewPGStore creates a new cron store backed by PostgreSQL.
// Returns an unexported type; external packages use the Store interface.
func NewPGStore(db *dbutil.DB, log *slog.Logger) Store {
	return &pgStore{
		db:      db,
		dialect: db.Dialect(),
		log:     log.With("component", "pg_cron_store"),
	}
}

func (s *pgStore) Create(ctx context.Context, job *CronJob) error {
	if job.ID == "" {
		job.ID = GenerateJobID()
	}

	ctx, cancel := withTimeout(ctx)
	defer cancel()

	schedData, payloadData, platformKeyData, stateData, err := marshalJobColumns(job)
	if err != nil {
		return err
	}

	query := s.dialect.Rebind(`INSERT INTO cron_jobs (` + jobColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)

	_, err = s.db.ExecContext(ctx, query,
		job.ID, job.Name, job.Description, s.dialect.BoolValue(job.Enabled),
		job.Schedule.Kind, schedData, job.Payload.Kind, payloadData,
		job.WorkDir, job.BotID, job.OwnerID, job.Platform, platformKeyData,
		job.TimeoutSec, s.dialect.BoolValue(job.DeleteAfterRun), s.dialect.BoolValue(job.Silent),
		job.MaxRetries, job.MaxRuns, job.ExpiresAt,
		stateData, job.CreatedAtMs, job.UpdatedAtMs,
	)
	if err != nil {
		return fmt.Errorf("cron store: create job: %w", err)
	}
	return nil
}

func (s *pgStore) Update(ctx context.Context, job *CronJob) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	schedData, payloadData, platformKeyData, stateData, err := marshalJobColumns(job)
	if err != nil {
		return err
	}

	query := s.dialect.Rebind(`UPDATE cron_jobs SET
		name = ?, description = ?, enabled = ?,
		schedule_kind = ?, schedule_data = ?, payload_kind = ?, payload_data = ?,
		work_dir = ?, bot_id = ?, owner_id = ?, platform = ?, platform_key = ?,
		timeout_sec = ?, delete_after_run = ?, silent = ?, max_retries = ?,
		max_runs = ?, expires_at = ?,
		state = ?, updated_at = ?
		WHERE id = ?`)

	res, err := s.db.ExecContext(ctx, query,
		job.Name, job.Description, s.dialect.BoolValue(job.Enabled),
		job.Schedule.Kind, schedData, job.Payload.Kind, payloadData,
		job.WorkDir, job.BotID, job.OwnerID, job.Platform, platformKeyData,
		job.TimeoutSec, s.dialect.BoolValue(job.DeleteAfterRun), s.dialect.BoolValue(job.Silent),
		job.MaxRetries, job.MaxRuns, job.ExpiresAt,
		stateData, job.UpdatedAtMs,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("cron store: update job: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrJobNotFound, job.ID)
	}
	return nil
}

func (s *pgStore) Delete(ctx context.Context, id string) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	res, err := s.db.ExecContext(ctx,
		s.dialect.Rebind(`DELETE FROM cron_jobs WHERE id = ?`), id)
	if err != nil {
		return fmt.Errorf("cron store: delete job: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	return nil
}

func (s *pgStore) Get(ctx context.Context, id string) (*CronJob, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	row := s.db.QueryRowContext(ctx,
		s.dialect.Rebind(`SELECT `+jobColumns+` FROM cron_jobs WHERE id = ?`), id)
	return scanJobPG(row)
}

func (s *pgStore) GetByName(ctx context.Context, name string) (*CronJob, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	row := s.db.QueryRowContext(ctx,
		s.dialect.Rebind(`SELECT `+jobColumns+` FROM cron_jobs WHERE name = ?`), name)
	return scanJobPG(row)
}

func (s *pgStore) List(ctx context.Context, enabledOnly bool) ([]*CronJob, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	q := `SELECT ` + jobColumns + ` FROM cron_jobs`
	var args []any
	if enabledOnly {
		q += ` WHERE enabled = ?`
		args = append(args, s.dialect.BoolValue(true))
	}
	q += ` ORDER BY created_at`

	rows, err := s.db.QueryContext(ctx, s.dialect.Rebind(q), args...)
	if err != nil {
		return nil, fmt.Errorf("cron store: list jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var jobs []*CronJob
	for rows.Next() {
		job, err := scanJobRowPG(rows)
		if err != nil {
			return nil, fmt.Errorf("cron store: scan job row: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *pgStore) UpdateState(ctx context.Context, id string, state CronJobState) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("cron store: marshal state: %w", err)
	}

	now := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx,
		s.dialect.Rebind(`UPDATE cron_jobs SET state = ?, updated_at = ? WHERE id = ?`),
		string(data), now, id,
	)
	if err != nil {
		return fmt.Errorf("cron store: update state: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	return nil
}

// SetEnabled updates the enabled flag for a job without touching other fields.
func (s *pgStore) SetEnabled(ctx context.Context, id string, enabled bool) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	now := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx,
		s.dialect.Rebind(`UPDATE cron_jobs SET enabled = ?, updated_at = ? WHERE id = ?`),
		s.dialect.BoolValue(enabled), now, id,
	)
	if err != nil {
		return fmt.Errorf("cron store: set enabled: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	return nil
}

// UpsertByName inserts or updates a job by name (idempotent for YAML import).
// Uses PostgreSQL's INSERT ON CONFLICT to atomically insert or update,
// preserving runtime state (ID, State, CreatedAtMs) on conflict.
func (s *pgStore) UpsertByName(ctx context.Context, job *CronJob) error {
	if job.ID == "" {
		job.ID = GenerateJobID()
	}

	ctx, cancel := withTimeout(ctx)
	defer cancel()

	schedData, payloadData, platformKeyData, stateData, err := marshalJobColumns(job)
	if err != nil {
		return err
	}

	job.UpdatedAtMs = time.Now().UnixMilli()

	query := s.dialect.Rebind(`INSERT INTO cron_jobs (` + jobColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			enabled = EXCLUDED.enabled,
			schedule_kind = EXCLUDED.schedule_kind,
			schedule_data = EXCLUDED.schedule_data,
			payload_kind = EXCLUDED.payload_kind,
			payload_data = EXCLUDED.payload_data,
			work_dir = EXCLUDED.work_dir,
			bot_id = EXCLUDED.bot_id,
			owner_id = EXCLUDED.owner_id,
			platform = EXCLUDED.platform,
			platform_key = EXCLUDED.platform_key,
			timeout_sec = EXCLUDED.timeout_sec,
			delete_after_run = EXCLUDED.delete_after_run,
			silent = EXCLUDED.silent,
			max_retries = EXCLUDED.max_retries,
			max_runs = EXCLUDED.max_runs,
			expires_at = EXCLUDED.expires_at,
			state = cron_jobs.state,
			created_at = cron_jobs.created_at,
			updated_at = EXCLUDED.updated_at`)

	_, err = s.db.ExecContext(ctx, query,
		job.ID, job.Name, job.Description, s.dialect.BoolValue(job.Enabled),
		job.Schedule.Kind, schedData, job.Payload.Kind, payloadData,
		job.WorkDir, job.BotID, job.OwnerID, job.Platform, platformKeyData,
		job.TimeoutSec, s.dialect.BoolValue(job.DeleteAfterRun), s.dialect.BoolValue(job.Silent),
		job.MaxRetries, job.MaxRuns, job.ExpiresAt,
		stateData, job.CreatedAtMs, job.UpdatedAtMs,
	)
	if err != nil {
		return fmt.Errorf("cron store: upsert job: %w", err)
	}
	return nil
}

// scanJobPG scans a single job row using PG-appropriate types (BOOLEAN → bool).
func scanJobPG(row *sql.Row) (*CronJob, error) {
	job, err := scanJobRowPG(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrJobNotFound
	}
	return job, err
}

// scanJobRowPG scans BOOLEAN columns into bool variables for PostgreSQL compatibility.
// pgx returns Go bool for BOOLEAN columns; scanning into *int causes runtime errors.
func scanJobRowPG(s scanner) (*CronJob, error) {
	job := &CronJob{}
	var enabled, deleteAfterRun, silent bool
	var schedData, payloadData, platformKeyData, stateData string

	err := s.Scan(
		&job.ID, &job.Name, &job.Description, &enabled,
		&job.Schedule.Kind, &schedData, &job.Payload.Kind, &payloadData,
		&job.WorkDir, &job.BotID, &job.OwnerID, &job.Platform, &platformKeyData,
		&job.TimeoutSec, &deleteAfterRun, &silent, &job.MaxRetries, &job.MaxRuns, &job.ExpiresAt,
		&stateData, &job.CreatedAtMs, &job.UpdatedAtMs,
	)
	if err != nil {
		return nil, fmt.Errorf("cron store: scan job: %w", err)
	}
	if err := decodeJobFields(job, b2i(enabled), b2i(deleteAfterRun), b2i(silent), schedData, payloadData, platformKeyData, stateData); err != nil {
		return nil, fmt.Errorf("cron store: decode job: %w", err)
	}
	return job, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// compile-time interface check
var _ Store = (*pgStore)(nil)
