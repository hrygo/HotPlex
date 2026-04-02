// Package aep provides the gateway's AEP v1 codec implementation.
// This package re-exports from pkg/aep for backward compatibility.
// Gateway-specific codec logic should be added here.
package aep

import (
	"hotplex-worker/pkg/aep"
)

// Re-export only the symbols actually used by gateway/worker code.
// Dead re-exports were removed after codebase-wide grep confirmed zero external usage.
var (
	DecodeLine   = aep.DecodeLine
	Encode       = aep.Encode
	EncodeJSON   = aep.EncodeJSON
	NewID        = aep.NewID
	NewSessionID = aep.NewSessionID
)
