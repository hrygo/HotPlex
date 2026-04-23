package onboard

import (
	"crypto/rand"
	"encoding/base64"
)

func DefaultConfigYAML() string {
	return `gateway:
  addr: ":8888"
admin:
  addr: ":9999"
  enabled: true
db:
  path: "~/.hotplex/data/hotplex.db"
worker:
  type: "claude_code"
  execution_timeout: "30m"
session:
  retention: "24h"
  gc_scan_interval: "5m"
pool:
  max_size: 10
  max_idle_per_user: 2
log:
  level: "info"
  format: "json"
`
}

func GenerateSecret() string {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		// rand.Read should never fail on supported platforms,
		// but if it does, panic is appropriate for a setup wizard.
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return base64.StdEncoding.EncodeToString(b)
}
