package pidutil

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/hrygo/hotplex/internal/config"
)

// GatewayState represents the gateway process state persisted to disk.
type GatewayState struct {
	PID        int    `json:"pid"`
	ConfigPath string `json:"config,omitempty"`
	DevMode    bool   `json:"dev,omitempty"`
}

// PIDPath returns the path to the gateway PID file.
func PIDPath() string {
	return filepath.Join(config.HotplexHome(), ".pids", "gateway.pid")
}

// ReadState reads and parses the gateway PID file.
// Returns the parsed state, or an error if the file cannot be read or parsed.
func ReadState() (*GatewayState, error) {
	data, err := os.ReadFile(PIDPath())
	if err != nil {
		return nil, err
	}
	var s GatewayState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
