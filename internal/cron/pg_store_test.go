package cron

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/dbutil"
)

func newCronPGMock(t *testing.T) (*pgStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)

	db := &dbutil.DB{DB: mockDB}
	store := &pgStore{
		db:      db,
		dialect: dbutil.DialectPostgres,
		log:     slog.Default().With("component", "pg_cron_store"),
	}
	return store, mock, func() {
		require.NoError(t, mock.ExpectationsWereMet())
		mockDB.Close()
	}
}

func cronJobColumns() []string {
	return []string{
		"id", "name", "description", "enabled",
		"schedule_kind", "schedule_data", "payload_kind", "payload_data",
		"work_dir", "bot_id", "owner_id", "platform", "platform_key",
		"timeout_sec", "delete_after_run", "silent", "max_retries", "max_runs", "expires_at",
		"state", "created_at", "updated_at",
	}
}

func TestPGStore_CreateJob(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newCronPGMock(t)
	defer cleanup()

	now := time.Now().UnixMilli()
	job := &CronJob{
		ID:          "job-1",
		Name:        "test-job",
		Description: "desc",
		Enabled:     true,
		Schedule:    CronSchedule{Kind: ScheduleEvery, EveryMs: 60000},
		Payload:     CronPayload{Kind: PayloadIsolatedSession, Message: "hello"},
		BotID:       "bot-1",
		OwnerID:     "user-1",
		Platform:    "slack",
		MaxRuns:     10,
		ExpiresAt:   "2027-01-01T00:00:00Z",
		CreatedAtMs: now,
		UpdatedAtMs: now,
	}

	schedJSON, _ := json.Marshal(job.Schedule)
	payloadJSON, _ := json.Marshal(job.Payload)
	platformKeyJSON, _ := json.Marshal(job.PlatformKey)
	stateJSON, _ := json.Marshal(job.State)

	createSQL := dbutil.DialectPostgres.Rebind(
		`INSERT INTO cron_jobs (` + jobColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)

	mock.ExpectExec(regexp.QuoteMeta(createSQL)).
		WithArgs(
			job.ID, job.Name, job.Description, true,
			string(job.Schedule.Kind), string(schedJSON),
			string(job.Payload.Kind), string(payloadJSON),
			job.WorkDir, job.BotID, job.OwnerID, job.Platform, string(platformKeyJSON),
			job.TimeoutSec, false, false,
			job.MaxRetries, job.MaxRuns, job.ExpiresAt,
			string(stateJSON), job.CreatedAtMs, job.UpdatedAtMs,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := store.Create(context.Background(), job)
	require.NoError(t, err)
}

func TestPGStore_GetJob(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newCronPGMock(t)
	defer cleanup()

	getSQL := dbutil.DialectPostgres.Rebind(
		`SELECT ` + jobColumns + ` FROM cron_jobs WHERE id = ?`)

	now := time.Now().UnixMilli()
	state := CronJobState{NextRunAtMs: 3000}
	stateJSON, _ := json.Marshal(state)
	schedJSON, _ := json.Marshal(CronSchedule{Kind: ScheduleEvery, EveryMs: 60000})
	payloadJSON, _ := json.Marshal(CronPayload{Kind: PayloadIsolatedSession, Message: "hello"})

	rows := sqlmock.NewRows(cronJobColumns()).
		AddRow("job-1", "test-job", "desc", 1,
			"every", string(schedJSON), "isolated_session", string(payloadJSON),
			"/work", "bot-1", "user-1", "slack", "null",
			30, 0, 0, 3, 10, "2027-01-01T00:00:00Z",
			string(stateJSON), now, now)

	mock.ExpectQuery(regexp.QuoteMeta(getSQL)).WithArgs("job-1").WillReturnRows(rows)

	result, err := store.Get(context.Background(), "job-1")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "job-1", result.ID)
	require.Equal(t, "test-job", result.Name)
	require.Equal(t, "desc", result.Description)
	require.True(t, result.Enabled)
	require.Equal(t, ScheduleEvery, result.Schedule.Kind)
	require.Equal(t, int64(60000), result.Schedule.EveryMs)
	require.Equal(t, PayloadIsolatedSession, result.Payload.Kind)
	require.Equal(t, "hello", result.Payload.Message)
	require.Equal(t, "/work", result.WorkDir)
	require.Equal(t, "bot-1", result.BotID)
	require.Equal(t, "user-1", result.OwnerID)
	require.Equal(t, "slack", result.Platform)
	require.Equal(t, int64(3000), result.State.NextRunAtMs)
}

func TestPGStore_GetJob_NotFound(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newCronPGMock(t)
	defer cleanup()

	getSQL := dbutil.DialectPostgres.Rebind(
		`SELECT ` + jobColumns + ` FROM cron_jobs WHERE id = ?`)

	mock.ExpectQuery(regexp.QuoteMeta(getSQL)).WithArgs("nonexistent").
		WillReturnError(ErrJobNotFound)

	result, err := store.Get(context.Background(), "nonexistent")
	require.ErrorIs(t, err, ErrJobNotFound)
	require.Nil(t, result)
}

func TestPGStore_ListJobs(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newCronPGMock(t)
	defer cleanup()

	listSQL := dbutil.DialectPostgres.Rebind(
		`SELECT ` + jobColumns + ` FROM cron_jobs ORDER BY created_at`)

	now := time.Now().UnixMilli()
	stateJSON, _ := json.Marshal(CronJobState{})
	schedJSON, _ := json.Marshal(CronSchedule{Kind: ScheduleEvery, EveryMs: 60000})
	payloadJSON, _ := json.Marshal(CronPayload{Kind: PayloadIsolatedSession, Message: "hello"})

	rows := sqlmock.NewRows(cronJobColumns()).
		AddRow("job-1", "job-a", "", 1,
			"every", string(schedJSON), "isolated_session", string(payloadJSON),
			"", "bot-1", "user-1", "slack", "null",
			0, 0, 0, 0, 0, "",
			string(stateJSON), now, now).
		AddRow("job-2", "job-b", "", 0,
			"cron", `{"kind":"cron","expr":"0 9 * * *"}`, "isolated_session", string(payloadJSON),
			"", "bot-2", "user-2", "feishu", "null",
			0, 0, 0, 0, 5, "2027-01-01T00:00:00Z",
			string(stateJSON), now, now)

	mock.ExpectQuery(regexp.QuoteMeta(listSQL)).WillReturnRows(rows)

	results, err := store.List(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "job-1", results[0].ID)
	require.True(t, results[0].Enabled)
	require.Equal(t, "job-2", results[1].ID)
	require.False(t, results[1].Enabled)
}

func TestPGStore_DeleteJob(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newCronPGMock(t)
	defer cleanup()

	deleteSQL := dbutil.DialectPostgres.Rebind(`DELETE FROM cron_jobs WHERE id = ?`)

	mock.ExpectExec(regexp.QuoteMeta(deleteSQL)).WithArgs("job-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.Delete(context.Background(), "job-1")
	require.NoError(t, err)
}

func TestPGStore_DeleteJob_NotFound(t *testing.T) {
	t.Parallel()
	store, mock, cleanup := newCronPGMock(t)
	defer cleanup()

	deleteSQL := dbutil.DialectPostgres.Rebind(`DELETE FROM cron_jobs WHERE id = ?`)

	mock.ExpectExec(regexp.QuoteMeta(deleteSQL)).WithArgs("nonexistent").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := store.Delete(context.Background(), "nonexistent")
	require.ErrorIs(t, err, ErrJobNotFound)
}
