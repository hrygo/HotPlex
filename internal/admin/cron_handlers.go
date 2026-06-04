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
//
// @Summary      List cron jobs
// @Description  Returns all configured cron jobs. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Success      200  {array}   object
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Failure      500  {object}  ErrorResponse  "Internal error"
// @Router       /admin/cron/jobs [get]
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
//
// @Summary      Get cron job
// @Description  Returns details for a single cron job by ID. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        id   path      string  true  "Cron job ID"
// @Success      200  {object}  CronJobResponse
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Failure      404  {object}  ErrorResponse  "Job not found"
// @Router       /admin/cron/jobs/{id} [get]
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
//
// @Summary      Create cron job
// @Description  Creates a new scheduled cron job. Requires admin:write scope.
// @Tags         Admin API
// @Accept       json
// @Security     AdminBearerAuth
// @Param        body  body  CronJobCreateRequest  true  "Cron job definition"
// @Success      201   "Job created"
// @Failure      400   {object}  ErrorResponse  "Invalid JSON"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Failure      500   {object}  ErrorResponse  "Internal error"
// @Router       /admin/cron/jobs [post]
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
//
// @Summary      Update cron job
// @Description  Partially updates an existing cron job. Requires admin:write scope.
// @Tags         Admin API
// @Accept       json
// @Security     AdminBearerAuth
// @Param        id    path  string  true  "Cron job ID"
// @Param        body  body  CronJobUpdateRequest  true  "Fields to update"
// @Success      204   "Job updated"
// @Failure      400   {object}  ErrorResponse  "Invalid JSON"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Failure      404   {object}  ErrorResponse  "Job not found"
// @Router       /admin/cron/jobs/{id} [patch]
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
//
// @Summary      Delete cron job
// @Description  Permanently deletes a cron job. Requires admin:write scope.
// @Tags         Admin API
// @Security     AdminBearerAuth
// @Param        id   path  string  true  "Cron job ID"
// @Success      204  "Job deleted"
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Failure      404  {object}  ErrorResponse  "Job not found"
// @Router       /admin/cron/jobs/{id} [delete]
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
//
// @Summary      Trigger cron job
// @Description  Manually triggers an immediate run of a cron job. Requires admin:write scope.
// @Tags         Admin API
// @Security     AdminBearerAuth
// @Param        id   path  string  true  "Cron job ID"
// @Success      202  "Job triggered"
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Failure      404  {object}  ErrorResponse  "Job not found"
// @Router       /admin/cron/jobs/{id}/run [post]
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

// HandleCronRunHistory returns the run history for a cron job.
//
// @Summary      Get cron job run history
// @Description  Returns the execution history for a cron job. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        id   path      string  true  "Cron job ID"
// @Success      200  {array}   object
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Failure      404  {object}  ErrorResponse  "Job not found"
// @Router       /admin/cron/jobs/{id}/runs [get]
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
