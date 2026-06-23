package admin

import (
	"net/http"

	"github.com/hrygo/hotplex/internal/web"
)

// BotListerProvider abstracts bot registry access for the admin API.
type BotListerProvider interface {
	ListBots() []BotEntry
	GetBot(name string) (*BotEntry, bool)
}

// BotEntry is a read-only view of a registered bot.
type BotEntry struct {
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	BotID       string `json:"bot_id"`
	Status      string `json:"status"`
	ConnectedAt string `json:"connected_at,omitempty"`
	WorkerType  string `json:"worker_type,omitempty"`
}

// HandleListBots returns all registered bots.
//
// @Summary      List bots
// @Description  Returns all registered bots and their current connection status. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Success      200  {array}   BotEntry
// @Failure      403  {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Router       /admin/bots [get]
func (a *AdminAPI) HandleListBots(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}

	if a.botLister == nil {
		respondJSON(w, []BotEntry{})
		return
	}

	respondJSON(w, a.botLister.ListBots())
}

// HandleGetBot returns details for a single bot by name.
//
// @Summary      Get bot
// @Description  Returns details for a single registered bot. Requires admin:read scope.
// @Tags         Admin API
// @Produce      json
// @Security     AdminBearerAuth
// @Param        name  path      string  true  "Bot name"
// @Success      200   {object}  BotEntry
// @Failure      403   {object}  ErrorResponse  "Insufficient scope: need admin:read"
// @Failure      404   {object}  ErrorResponse  "Bot not found"
// @Router       /admin/bots/{name} [get]
func (a *AdminAPI) HandleGetBot(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, ScopeAdminRead) {
		return
	}

	name := r.PathValue("name")
	if name == "" {
		web.WriteAppError(w, http.StatusBadRequest, "BAD_REQUEST", "missing bot name")
		return
	}

	if a.botLister == nil {
		web.WriteAppError(w, http.StatusNotFound, "NOT_FOUND", "bot registry not available")
		return
	}

	entry, ok := a.botLister.GetBot(name)
	if !ok {
		web.WriteAppError(w, http.StatusNotFound, "NOT_FOUND", "bot not found")
		return
	}
	respondJSON(w, entry)
}
