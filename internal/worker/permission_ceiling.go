package worker

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	// ErrPermissionEscalation signals a runtime permission request above the
	// immutable ceiling captured when the Worker session starts.
	ErrPermissionEscalation = errors.New("permission mode exceeds session ceiling")
	// ErrPermissionCeilingUnset signals a Worker implementation bug: runtime
	// permission changes must not be processed before startup captures a ceiling.
	ErrPermissionCeilingUnset = errors.New("permission ceiling not initialized")
)

// NormalizeRuntimePermissionMode converts public tiers and compatibility aliases
// to the unified permission modes used between Gateway and Workers.
func NormalizeRuntimePermissionMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case PermissionModeReadOnly, "readonly", "read_only", "plan":
		return PermissionModeReadOnly, nil
	case PermissionModeWorkspace, "accept-edits", "acceptedits", "default":
		return PermissionModeWorkspace, nil
	case PermissionModeAutoEdit, "autoedit", "auto_edit", "auto", "auto-accept", "autoaccept":
		return PermissionModeAutoEdit, nil
	case PermissionModeBypass, "bypasspermissions", "dangerously-skip-permissions":
		return PermissionModeBypass, nil
	default:
		return "", fmt.Errorf("%w: unsupported runtime mode", ErrInvalidPermissionMode)
	}
}

// PermissionRejectionReason returns a bounded reason code suitable for logs.
// It deliberately never includes caller-controlled permission mode text.
func PermissionRejectionReason(err error) string {
	switch {
	case errors.Is(err, ErrPermissionEscalation):
		return "ceiling_exceeded"
	case errors.Is(err, ErrPermissionCeilingUnset):
		return "ceiling_unset"
	case errors.Is(err, ErrInvalidPermissionMode):
		return "invalid_mode"
	default:
		return "permission_rejected"
	}
}

func permissionModeRank(mode string) int {
	switch mode {
	case PermissionModeReadOnly:
		return 0
	case PermissionModeWorkspace:
		return 1
	case PermissionModeAutoEdit:
		return 2
	case PermissionModeBypass:
		return 3
	default:
		return -1
	}
}

// ResolvePermissionMode returns the effective permission tier for a session.
// An empty session mode (platform/cron sessions) falls back to the
// operator-configured default; a non-empty session mode is then clamped to
// never exceed the operator tier, which is a permissiveness ceiling (mirrors
// codex's codexSandboxRank/codexApprovalRank clamp). Empty results keep the
// legacy per-worker default semantics and are interpreted by callers.
func ResolvePermissionMode(sessionMode, operatorMode string) string {
	effective := sessionMode
	if effective == "" {
		effective = operatorMode
	}
	if operatorMode != "" && permissionModeRank(effective) > permissionModeRank(operatorMode) {
		effective = operatorMode
	}
	return effective
}

// PermissionCeiling stores a Worker session's immutable maximum permission tier.
// Its zero value is ready for use.
type PermissionCeiling struct {
	mu          sync.RWMutex
	mode        string
	initialized bool
}

// Capture records the ceiling on the first call. Later calls validate their
// input but never replace the original ceiling, which keeps reset/restart paths
// from widening a live Worker session.
func (c *PermissionCeiling) Capture(mode string) error {
	canonical, err := NormalizeRuntimePermissionMode(mode)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.initialized {
		c.mode = canonical
		c.initialized = true
	}
	return nil
}

// Check normalizes a requested mode and rejects it when it exceeds the captured
// ceiling. A more restrictive target may later return to the original ceiling.
func (c *PermissionCeiling) Check(requested string) (string, error) {
	canonical, err := NormalizeRuntimePermissionMode(requested)
	if err != nil {
		return "", err
	}

	c.mu.RLock()
	ceiling := c.mode
	initialized := c.initialized
	c.mu.RUnlock()
	if !initialized {
		return "", ErrPermissionCeilingUnset
	}
	if permissionModeRank(canonical) > permissionModeRank(ceiling) {
		return "", fmt.Errorf("%w: requested %q, ceiling %q", ErrPermissionEscalation, canonical, ceiling)
	}
	return canonical, nil
}

// Mode returns the captured canonical ceiling.
func (c *PermissionCeiling) Mode() (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mode, c.initialized
}
