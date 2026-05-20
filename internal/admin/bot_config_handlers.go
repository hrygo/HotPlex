package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// HandleListBotConfigs returns all registered bot configurations.
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, result)
}

// HandleGetBotConfig returns the full configuration for a single bot.
// GET /admin/bots/{name}/config
func (a *AdminAPI) HandleGetBotConfig(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}
	result, err := a.botConfig.GetBotConfig(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	respondJSON(w, result)
}

// HandleGetAgentConfigFile reads a single agent config file for a bot.
// GET /admin/bots/{name}/config/{file}
func (a *AdminAPI) HandleGetAgentConfigFile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}
	fileStr := r.PathValue("file")
	fileName := AgentConfigFileName(fileStr)
	if !ValidConfigFiles[fileName] {
		http.Error(w, fmt.Sprintf("invalid config file %q", fileStr), http.StatusBadRequest)
		return
	}
	result, err := a.botConfig.GetAgentConfigFile(r.Context(), name, fileName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	respondJSON(w, result)
}

// HandleSystemPromptPreview returns the assembled system prompt for a bot.
// GET /admin/bots/{name}/preview
func (a *AdminAPI) HandleSystemPromptPreview(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}
	result, err := a.botConfig.GetSystemPromptPreview(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	respondJSON(w, map[string]string{"preview": result})
}

// HandleUpdateBotConfig applies partial updates to an existing bot configuration.
// PATCH /admin/bots/{name}
func (a *AdminAPI) HandleUpdateBotConfig(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	attrs := extractBotConfigAttrs(body)
	if err := a.botConfig.UpdateBotConfig(r.Context(), name, attrs); err != nil {
		http.Error(w, fmt.Sprintf("update bot config: %s", err), http.StatusBadRequest)
		return
	}
	a.log.Info("admin: bot config updated", "bot", name, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusNoContent)
}

// HandleCreateBot registers a new bot.
// POST /admin/bots
func (a *AdminAPI) HandleCreateBot(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	name, _ := body["name"].(string)
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}
	attrs := extractBotConfigAttrs(body)
	if err := a.botConfig.CreateBot(r.Context(), name, attrs); err != nil {
		http.Error(w, fmt.Sprintf("create bot: %s", err), http.StatusBadRequest)
		return
	}
	a.log.Info("admin: bot created", "bot", name, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusCreated)
}

// HandleDeleteBot removes a bot registration.
// DELETE /admin/bots/{name}
func (a *AdminAPI) HandleDeleteBot(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}
	if err := a.botConfig.DeleteBot(r.Context(), name); err != nil {
		status := http.StatusNotFound
		if isConflictError(err) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	a.log.Info("admin: bot deleted", "bot", name, "admin", adminKeyPrefix(r))
	w.WriteHeader(http.StatusNoContent)
}

// HandleWriteAgentConfigFile writes content to a single agent config file for a bot.
// PUT /admin/bots/{name}/config/{file}
func (a *AdminAPI) HandleWriteAgentConfigFile(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminWrite) {
		return
	}
	if a.botConfig == nil {
		http.Error(w, "bot config provider not available", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing bot name", http.StatusBadRequest)
		return
	}
	fileStr := r.PathValue("file")
	fileName := AgentConfigFileName(fileStr)
	if !ValidConfigFiles[fileName] {
		http.Error(w, fmt.Sprintf("invalid config file %q", fileStr), http.StatusBadRequest)
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if err := a.botConfig.WriteAgentConfigFile(r.Context(), name, fileName, body.Content); err != nil {
		http.Error(w, fmt.Sprintf("write agent config file: %s", err), http.StatusBadRequest)
		return
	}
	a.log.Info("admin: agent config file written", "bot", name, "file", fileStr, "admin", adminKeyPrefix(r))
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

// isConflictError checks whether the error indicates a conflict
// (e.g., trying to delete a running bot).
func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "running") || strings.Contains(msg, "conflict")
}
