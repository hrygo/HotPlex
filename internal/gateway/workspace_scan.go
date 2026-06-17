package gateway

import (
	"context"
	"time"

	"log/slog"

	"github.com/hrygo/hotplex/internal/agentconfig"
)

// ScanWorkspaceOverrides performs a one-time validation sweep of all active
// workspaces' agent_config_overrides at gateway startup (#749). It logs a
// Warn for each workspace whose overrides fail validation — surfacing stale
// data written before spec ② write-time validation existed, without blocking
// startup. No data is modified; the runtime degrade path remains unchanged.
func ScanWorkspaceOverrides(ctx context.Context, store WorkspaceOverridesReader, log *slog.Logger) {
	if store == nil {
		return
	}
	scanCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	workspaces, err := store.ListAllWorkspaces(scanCtx)
	if err != nil {
		log.Warn("gateway: startup workspace overrides scan failed", "err", err)
		return
	}

	var dirty int
	for _, ws := range workspaces {
		if ws.AgentConfigOverrides == "" {
			continue
		}
		if _, err := agentconfig.ValidateOverrides(ws.AgentConfigOverrides); err != nil {
			dirty++
			log.Warn("gateway: workspace has invalid agent_config_overrides (will degrade to team defaults at runtime)",
				"workspace_id", ws.ID,
				"workspace_name", ws.Name,
				"owner", ws.OwnerUserID,
				"err", err)
		}
	}
	if dirty > 0 {
		log.Warn("gateway: startup scan complete, invalid agent_config_overrides detected",
			"dirty_count", dirty, "total_scanned", len(workspaces),
			"hint", "PATCH the affected workspace(s) with valid JSON to resolve")
	} else {
		log.Debug("gateway: startup scan complete, all workspace overrides valid",
			"total_scanned", len(workspaces))
	}
}
