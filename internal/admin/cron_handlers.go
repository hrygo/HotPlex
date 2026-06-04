package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// CronSchedulerProvider abstracts the cron scheduler for the admin API.
type CronSchedulerProvider interface {
	CreateJob(ctx context.Context, job any) error
	UpdateJob(ctx context.Context, id string, updates map[string]any) error
	DeleteJob(ctx context.Context, id string) error
	GetJob(ctx context.Context, id string) (any, error)
	ListJobs(ctx context.Context) (any, error)
	TriggerJob(ctx context.Context, id string) error
	RunHistory(ctx context.Context, id string) (any, error)
}

// HandleCronList returns all cron jobs.
func (a *AdminAPI) HandleCronList(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.cron == nil {
		respondJSON(w, []any{})
		return
	}
	result, err := a.cron.ListJobs(r.Context())
	if err != nil {
		respondStoreError(w, a.log, "admin: cron list jobs", err)
		return
	}
	respondJSON(w, result)
}

// HandleCronGet returns a single cron job.
func (a *AdminAPI) HandleCronGet(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	result, err := a.cron.GetJob(r.Context(), id)
	if err != nil {
		respondStoreError(w, a.log, "admin: cron get job", err)
		return
	}
	respondJSON(w, result)
}

// HandleCronCreate creates a new cron job.
func (a *AdminAPI) HandleCronCreate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := a.cron.CreateJob(r.Context(), body); err != nil {
		respondStoreError(w, a.log, "admin: create cron job", err)
		return
	}
	a.log.Info("admin: cron job created", "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusCreated)
}

// HandleCronUpdate updates an existing cron job.
func (a *AdminAPI) HandleCronUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := a.cron.UpdateJob(r.Context(), id, body); err != nil {
		respondStoreError(w, a.log, "admin: update cron job", err)
		return
	}
	a.log.Info("admin: cron job updated", "job_id", id, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusNoContent)
}

// HandleCronDelete deletes a cron job.
func (a *AdminAPI) HandleCronDelete(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := a.cron.DeleteJob(r.Context(), id); err != nil {
		respondStoreError(w, a.log, "admin: cron delete job", err)
		return
	}
	a.log.Info("admin: cron job deleted", "job_id", id, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusNoContent)
}

// HandleCronTrigger manually triggers a cron job run.
func (a *AdminAPI) HandleCronTrigger(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := a.cron.TriggerJob(r.Context(), id); err != nil {
		respondStoreError(w, a.log, "admin: cron trigger job", err)
		return
	}
	a.log.Info("admin: cron job triggered", "job_id", id, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusAccepted)
}

// HandleCronRunHistory returns the turn history for a cron job's latest run.
func (a *AdminAPI) HandleCronRunHistory(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.cron == nil {
		http.Error(w, "cron scheduler not enabled", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	result, err := a.cron.RunHistory(r.Context(), id)
	if err != nil {
		respondStoreError(w, a.log, "admin: cron run history", err)
		return
	}
	respondJSON(w, result)
}
