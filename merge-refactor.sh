#!/bin/bash
# Merge interface-based refactoring into latest main

set -e

echo "=== Step 1: Add Engine interface to chatapps/types.go ==="
# Check if Engine interface already exists
if ! grep -q "type Engine interface" chatapps/types.go; then
    echo "Adding Engine interface..."
    # Add after imports
    cat >> chatapps/types.go.tmp << 'EOF'
package chatapps

import (
	"context"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/types"
)

// Engine abstracts the engine functionality for dependency inversion
type Engine interface {
	Execute(ctx context.Context, cfg *types.Config, prompt string, callback event.Callback) error
	GetSession(sessionID string) (Session, bool)
	Close() error
	GetSessionStats(sessionID string) *SessionStats
	ValidateConfig(cfg *types.Config) error
	StopSession(sessionID string, reason string) error
	ResetSessionProvider(sessionID string)
	SetDangerAllowPaths(paths []string)
	SetDangerBypassEnabled(token string, enabled bool) error
	SetAllowedTools(tools []string)
	SetDisallowedTools(tools []string)
	GetAllowedTools() []string
	GetDisallowedTools() []string
}

// Session abstracts session state and operations
type Session interface {
	ID() string
	Status() string
	CreatedAt() time.Time
}

// SessionStats holds session statistics
type SessionStats struct {
	SessionID     string
	Status        string
	TotalTokens   int64
	InputTokens   int64
	OutputTokens  int64
	CacheRead     int64
	CacheWrite    int64
	TotalCost     float64
	Duration      time.Duration
	ToolCallCount int
	ErrorCount    int
}

// Re-export interfaces from base
type (
	MessageOperations = base.MessageOperations
	SessionOperations = base.SessionOperations
)

EOF
    # Append rest of original file (skip package and imports)
    tail -n +13 chatapps/types.go >> chatapps/types.go.tmp
    mv chatapps/types.go.tmp chatapps/types.go
    echo "✓ Engine interface added"
else
    echo "✓ Engine interface already exists"
fi

echo "=== Step 2: Add MessageOperations to chatapps/base/types.go ==="
if ! grep -q "type MessageOperations interface" chatapps/base/types.go; then
    cat >> chatapps/base/types.go << 'EOF'

// MessageOperations defines platform-specific message operations
type MessageOperations interface {
	DeleteMessage(ctx context.Context, channelID, messageTS string) error
	AddReaction(ctx context.Context, reaction Reaction) error
	RemoveReaction(ctx context.Context, reaction Reaction) error
	UpdateMessage(ctx context.Context, channelID, messageTS string, msg *ChatMessage) error
}

// SessionOperations defines platform-specific session operations
type SessionOperations interface {
	GetSession(key string) (*Session, bool)
	FindSessionByUserAndChannel(userID, channelID string) *Session
}
EOF
    echo "✓ MessageOperations added"
else
    echo "✓ MessageOperations already exists"
fi

echo "=== Done ==="
