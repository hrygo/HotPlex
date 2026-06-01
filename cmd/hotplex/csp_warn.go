package main

import (
	"log/slog"

	"github.com/hrygo/hotplex/internal/config"
)

// warnCSPLogPolicy emits a startup warning when the resolved Content-Security-Policy
// for a given service is either the package-level default (no operator override)
// or is itself permissive (any-host sources). The intent is to make the
// security trade-off visible at boot, not to gate the process — remote /
// zero-config deployments remain first-class.
func warnCSPLogPolicy(log *slog.Logger, service, configured string) {
	if log == nil {
		return
	}
	if configured == "" {
		log.Warn("csp: using permissive default policy (any http/https/ws/wss host allowed); "+
			"set security.csp to restrict in production",
			"service", service)
		return
	}
	if config.IsPermissiveCSP(configured) {
		log.Warn("csp: configured policy contains wide-open sources (* or scheme-only); "+
			"review security.csp",
			"service", service)
	}
}
