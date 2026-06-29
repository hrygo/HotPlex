package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/hrygo/hotplex/internal/agentconfig"
	"github.com/hrygo/hotplex/internal/web"
)

// HandleListBotConfigs returns all registered bot configurations.
//
// @Summary      List bot configs
// @Description  Returns full configuration for all registered bots. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Success      200  {array}   BotConfigEntry
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Failure      500  {object}  ErrorResponse  "Internal error"
// @Router       /admin/bots/config [get]
// GET /admin/bots/config
func (a *AdminAPI) HandleListBotConfigs(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.botConfig == nil {
		respondJSON(w, []BotConfigEntry{})
		return
	}
	result, err := a.botConfig.ListBotConfigs(r.Context())
	if err != nil {
		respondStoreError(w, a.log, "admin: list bot configs", err)
		return
	}
	respondJSON(w, result)
}

// HandleGetBotConfig returns the full configuration for a single bot.
//
// @Summary      Get bot config
// @Description  Returns the full configuration including agent config summary for a single bot. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        name  path      string  true  "Bot name"
// @Success      200   {object}  BotConfigEntry
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Failure      404   {object}  ErrorResponse  "Bot not found"
// @Router       /admin/bots/{name}/config [get]
// GET /admin/bots/{name}/config
func (a *AdminAPI) HandleGetBotConfig(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.botConfig == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "bot config provider not available")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "missing bot name")
		return
	}
	result, err := a.botConfig.GetBotConfig(r.Context(), name)
	if err != nil {
		respondStoreError(w, a.log, "admin: get bot config", err)
		return
	}
	respondJSON(w, result)
}

// HandleGetAgentConfigFile reads a single agent config file for a bot.
//
// @Summary      Get agent config file
// @Description  Returns the content of a single agent config file (SOUL.md, AGENTS.md, SKILLS.md, USER.md, MEMORY.md). Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        name  path      string  true  "Bot name"
// @Param        file  path      string  true  "Config file name"  Enums(SOUL.md,AGENTS.md,SKILLS.md,USER.md,MEMORY.md)
// @Success      200   {object}  AgentConfigFile
// @Failure      400   {object}  ErrorResponse  "Invalid config file name"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Failure      404   {object}  ErrorResponse  "File not found"
// @Router       /admin/bots/{name}/config/{file} [get]
// GET /admin/bots/{name}/config/{file}
func (a *AdminAPI) HandleGetAgentConfigFile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.botConfig == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "bot config provider not available")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "missing bot name")
		return
	}
	fileStr := r.PathValue("file")
	fileName := AgentConfigFileName(fileStr)
	if !ValidConfigFiles[fileName] {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("invalid config file %q", fileStr))
		return
	}
	result, err := a.botConfig.GetAgentConfigFile(r.Context(), name, fileName)
	if err != nil {
		respondStoreError(w, a.log, "admin: get agent config file", err)
		return
	}
	respondJSON(w, result)
}

// HandleSystemPromptPreview returns the assembled system prompt for a bot.
//
// @Summary      Preview system prompt
// @Description  Returns the fully assembled system prompt for a bot, combining all agent config files. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        name  path      string  true  "Bot name"
// @Success      200   {object}  SystemPromptPreviewResponse
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Failure      404   {object}  ErrorResponse  "Bot not found"
// @Router       /admin/bots/{name}/preview [get]
// GET /admin/bots/{name}/preview
func (a *AdminAPI) HandleSystemPromptPreview(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.botConfig == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "bot config provider not available")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "missing bot name")
		return
	}
	result, err := a.botConfig.GetSystemPromptPreview(r.Context(), name)
	if err != nil {
		respondStoreError(w, a.log, "admin: get system prompt preview", err)
		return
	}
	respondJSON(w, map[string]string{"preview": result})
}

// HandleUpdateBotConfig applies partial updates to an existing bot configuration.
//
// @Summary      Update bot config
// @Description  Partially updates an existing bot's configuration attributes. Requires admin:write scope.
// @Tags         Admin API
// @Accept       json
// @Security     AdminBearerAuth
// @Param        name  path  string          true  "Bot name"
// @Param        body  body  BotConfigAttrs  true  "Config fields to update"
// @Success      204   "Config updated"
// @Failure      400   {object}  ErrorResponse  "Invalid JSON or update failed"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Router       /admin/bots/{name} [patch]
// PATCH /admin/bots/{name}
func (a *AdminAPI) HandleUpdateBotConfig(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.botConfig == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "bot config provider not available")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "missing bot name")
		return
	}
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
		return
	}
	attrs := extractBotConfigAttrs(body)
	if err := a.botConfig.UpdateBotConfig(r.Context(), name, attrs); err != nil {
		a.log.Error("admin: update bot config", "err", err)
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "update failed")
		return
	}
	a.log.Info("admin: bot config updated", "bot", name, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusNoContent)
}

// HandleCreateBot registers a new bot.
//
// @Summary      Create bot
// @Description  Registers a new bot with the specified platform configuration. Requires admin:write scope.
// @Tags         Admin API
// @Accept       json
// @Security     AdminBearerAuth
// @Param        body  body  CreateBotRequest  true  "Bot creation request"
// @Success      201   "Bot created"
// @Failure      400   {object}  ErrorResponse  "Invalid JSON or missing bot name"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Router       /admin/bots [post]
// POST /admin/bots
func (a *AdminAPI) HandleCreateBot(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.botConfig == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "bot config provider not available")
		return
	}
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
		return
	}
	name, _ := body["name"].(string)
	if name == "" {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "missing bot name")
		return
	}
	attrs := extractBotConfigAttrs(body)
	if err := a.botConfig.CreateBot(r.Context(), name, attrs); err != nil {
		a.log.Error("admin: create bot", "err", err)
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "create failed")
		return
	}
	a.log.Info("admin: bot created", "bot", name, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusCreated)
}

// HandleDeleteBot removes a bot registration.
//
// @Summary      Delete bot
// @Description  Removes a bot registration by name. Returns 409 if the bot is currently running. Requires admin:write scope.
// @Tags         Admin API
// @Security     AdminBearerAuth
// @Param        name  path  string  true  "Bot name"
// @Success      204   "Bot deleted"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Failure      404   {object}  ErrorResponse  "Bot not found"
// @Failure      409   {object}  ErrorResponse  "Bot is currently running"
// @Router       /admin/bots/{name} [delete]
// DELETE /admin/bots/{name}
func (a *AdminAPI) HandleDeleteBot(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.botConfig == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "bot config provider not available")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "missing bot name")
		return
	}
	if err := a.botConfig.DeleteBot(r.Context(), name); err != nil {
		if errors.Is(err, ErrBotRunning) {
			a.log.Error("admin: delete bot conflict", "bot", name, "error", err)
			web.WriteAppError(w, http.StatusConflict, "CONFLICT", "bot is currently running")
		} else {
			respondStoreError(w, a.log, "admin: delete bot", err)
		}
		return
	}
	a.log.Info("admin: bot deleted", "bot", name, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusNoContent)
}

// HandleWriteAgentConfigFile writes content to a single agent config file for a bot.
//
// @Summary      Write agent config file
// @Description  Writes content to a single agent config file (SOUL.md, AGENTS.md, SKILLS.md, USER.md, MEMORY.md). Requires admin:write scope.
// @Tags         Admin API
// @Accept       json
// @Security     AdminBearerAuth
// @Param        name  path  string                 true  "Bot name"
// @Param        file  path  string                 true  "Config file name"  Enums(SOUL.md,AGENTS.md,SKILLS.md,USER.md,MEMORY.md)
// @Param        body  body  WriteAgentConfigRequest  true  "File content"
// @Success      204   "File written"
// @Failure      400   {object}  ErrorResponse  "Invalid config file or write failed"
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Router       /admin/bots/{name}/config/{file} [put]
// PUT /admin/bots/{name}/config/{file}
func (a *AdminAPI) HandleWriteAgentConfigFile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.botConfig == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "bot config provider not available")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "missing bot name")
		return
	}
	fileStr := r.PathValue("file")
	fileName := AgentConfigFileName(fileStr)
	if !ValidConfigFiles[fileName] {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("invalid config file %q", fileStr))
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
		return
	}

	if err := a.botConfig.WriteAgentConfigFile(r.Context(), name, fileName, body.Content); err != nil {
		a.log.Error("admin: write agent config file", "err", err)
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "write failed")
		return
	}
	a.log.Info("admin: agent config file written", "bot", name, "file", fileStr, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusNoContent)
}

// HandleGetPlatformAgentConfigFile reads a single platform-level (channel team
// default) agent config file, identified by platform rather than a bot name.
//
// @Summary      Get channel default agent config file
// @Description  Returns the content of a single platform-level agent config file (channel team default). Used for platforms without a bot instance, e.g. webchat. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        platform  path  string  true  "Platform identifier"  Enums(webchat,slack,feishu)
// @Param        file      path  string  true  "Config file name"     Enums(SOUL.md,AGENTS.md,SKILLS.md,USER.md,MEMORY.md)
// @Success      200  {object}  AgentConfigFile
// @Failure      400  {object}  ErrorResponse  "Invalid platform or config file name"
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Router       /admin/bots/platform/{platform}/config/{file} [get]
// GET /admin/bots/platform/{platform}/config/{file}
func (a *AdminAPI) HandleGetPlatformAgentConfigFile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.botConfig == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "bot config provider not available")
		return
	}
	platform := r.PathValue("platform")
	if !agentconfig.IsValidPlatform(platform) {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("invalid platform %q", platform))
		return
	}
	fileStr := r.PathValue("file")
	fileName := AgentConfigFileName(fileStr)
	if !ValidConfigFiles[fileName] {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("invalid config file %q", fileStr))
		return
	}
	result, err := a.botConfig.GetPlatformAgentConfigFile(r.Context(), platform, fileName)
	if err != nil {
		respondStoreError(w, a.log, "admin: get platform agent config file", err)
		return
	}
	respondJSON(w, result)
}

// HandleWritePlatformAgentConfigFile writes content to a single platform-level
// (channel team default) agent config file.
//
// @Summary      Write channel default agent config file
// @Description  Writes content to a single platform-level agent config file (channel team default). Used for platforms without a bot instance, e.g. webchat. Requires admin:write scope.
// @Tags         Admin API
// @Accept       json
// @Security     AdminBearerAuth
// @Param        platform  path  string                   true  "Platform identifier"  Enums(webchat,slack,feishu)
// @Param        file      path  string                   true  "Config file name"     Enums(SOUL.md,AGENTS.md,SKILLS.md,USER.md,MEMORY.md)
// @Param        body      body  WriteAgentConfigRequest  true  "File content"
// @Success      204  "File written"
// @Failure      400  {object}  ErrorResponse  "Invalid platform/config file or write failed"
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:write"
// @Router       /admin/bots/platform/{platform}/config/{file} [put]
// PUT /admin/bots/platform/{platform}/config/{file}
func (a *AdminAPI) HandleWritePlatformAgentConfigFile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.botConfig == nil {
		web.WriteAppError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "bot config provider not available")
		return
	}
	platform := r.PathValue("platform")
	if !agentconfig.IsValidPlatform(platform) {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("invalid platform %q", platform))
		return
	}
	fileStr := r.PathValue("file")
	fileName := AgentConfigFileName(fileStr)
	if !ValidConfigFiles[fileName] {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("invalid config file %q", fileStr))
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
		return
	}

	if err := a.botConfig.WritePlatformAgentConfigFile(r.Context(), platform, fileName, body.Content); err != nil {
		a.log.Error("admin: write platform agent config file", "err", err)
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "write failed")
		return
	}
	a.log.Info("admin: platform agent config file written", "platform", platform, "file", fileStr, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusNoContent)
}

// extractBotConfigAttrs builds BotConfigAttrs from a raw JSON map.
func extractBotConfigAttrs(body map[string]any) *BotConfigAttrs {
	attrs := &BotConfigAttrs{}
	if v, ok := body["platform"].(string); ok {
		attrs.Platform = v
	}
	if v, ok := body["worker_type"].(string); ok {
		attrs.WorkerType = v
	}
	if v, ok := body["work_dir"].(string); ok {
		attrs.WorkDir = v
	}
	if v, ok := body["dm_policy"].(string); ok {
		attrs.DMPolicy = v
	}
	if v, ok := body["group_policy"].(string); ok {
		attrs.GroupPolicy = v
	}
	if v, ok := body["require_mention"].(bool); ok {
		attrs.RequireMention = v
	}
	if v, ok := body["allow_from"].([]any); ok {
		attrs.AllowFrom = toStringSlice(v)
	}
	if v, ok := body["allow_dm_from"].([]any); ok {
		attrs.AllowDMFrom = toStringSlice(v)
	}
	if v, ok := body["allow_group_from"].([]any); ok {
		attrs.AllowGroupFrom = toStringSlice(v)
	}
	if stt, ok := body["stt"].(map[string]any); ok {
		if p, ok := stt["provider"].(string); ok && p != "" {
			attrs.STT = &STTAttrs{Provider: p}
		}
	}
	if tts, ok := body["tts"].(map[string]any); ok {
		ttsAttrs := &TTSAttrs{}
		if p, ok := tts["provider"].(string); ok {
			ttsAttrs.Provider = p
		}
		if v, ok := tts["voice"].(string); ok {
			ttsAttrs.Voice = v
		}
		if ttsAttrs.Provider != "" || ttsAttrs.Voice != "" {
			attrs.TTS = ttsAttrs
		}
	}
	// Credentials
	if v, ok := body["bot_token"].(string); ok {
		attrs.BotToken = v
	}
	if v, ok := body["app_token"].(string); ok {
		attrs.AppToken = v
	}
	if v, ok := body["app_id"].(string); ok {
		attrs.AppID = v
	}
	if v, ok := body["app_secret"].(string); ok {
		attrs.AppSecret = v
	}
	return attrs
}

// toStringSlice converts []any to []string.
func toStringSlice(vals []any) []string {
	result := make([]string, 0, len(vals))
	for _, v := range vals {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
