package base

import (
	"os"
	"strings"

	"hotplex-worker/internal/security"
	"hotplex-worker/internal/worker"
)

// BuildEnv constructs the environment variables for a CLI worker process.
// It whitelist-filters os.Environ(), adds HOTPLEX_* vars, merges session.Env,
// and strips nested agent configuration.
func BuildEnv(session worker.SessionInfo, whitelist []string, workerTypeLabel string) []string {
	env := make([]string, 0, len(os.Environ()))

	// Build whitelist set from provided list.
	whitelistSet := make(map[string]bool)
	for _, k := range whitelist {
		whitelistSet[k] = true
	}

	// Iterate os.Environ(), keep if in whitelist OR in session.Env.
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]

		// Check if key is in whitelist.
		if whitelistSet[key] {
			env = append(env, e)
			continue
		}

		// Check if key is in session env.
		if _, ok := session.Env[key]; ok {
			env = append(env, e)
		}
	}

	// Add HOTPLEX session vars.
	env = append(env,
		"HOTPLEX_SESSION_ID="+session.SessionID,
		"HOTPLEX_WORKER_TYPE="+workerTypeLabel,
	)

	// Add session-specific env vars (skip if in whitelist).
	for k, v := range session.Env {
		if k != "" && !whitelistSet[k] {
			env = append(env, k+"="+v)
		}
	}

	// Strip nested agent config (CLAUDECODE=).
	env = security.StripNestedAgent(env)

	return env
}
