package reconcile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hrygo/hotplex/internal/skills/builtin"
)

type WorkerType string

const (
	WorkerClaude   WorkerType = "claude_code"
	WorkerCodex    WorkerType = "codex_cli"
	WorkerOpenCode WorkerType = "opencode_server"
	WorkerACP      WorkerType = "acp"
)

const (
	ActionNone    = "none"
	ActionInstall = "install"
	ActionUpdate  = "update"
	ActionRemove  = "remove"

	OutcomeUnchanged = "unchanged"
	OutcomeChanged   = "changed"
	OutcomeConflict  = "conflict"
	OutcomeDrift     = "drift"
	OutcomeFailed    = "failed"
)

const (
	ReasonUnsupportedWorker  = "unsupported_worker"
	ReasonCollision          = "collision"
	ReasonDrift              = "drift"
	ReasonInvalidReceipt     = "invalid_receipt"
	ReasonRootOutsideHome    = "root_outside_home"
	ReasonReceiptWriteFailed = "receipt_write_failed"
	ReasonInventoryBlocked   = "inventory_blocked"
	ReasonRollbackFailed     = "rollback_failed"
	ReasonUnchanged          = "unchanged"
	ReasonChanged            = "changed"
	ReasonMissingReceipt     = "missing_receipt"
	ReasonMissingTarget      = "missing_target"
	ReasonInvalidPackage     = "invalid_package"
)

var (
	ErrNoWorkerTargets             = errors.New("skills: no worker targets; specify --worker")
	ErrUnknownWorker               = errors.New("skills: unknown worker")
	ErrUnknownProfile              = errors.New("skills: unknown profile")
	ErrRootOutsideHome             = errors.New("skills: native root outside approved home")
	ErrInventoryOutsideHotplexHome = errors.New("skills: inventory outside HotPlex home")
	ErrReportActionRequired        = errors.New("skills: reconciliation requires explicit action")
	ErrInvalidReceipt              = errors.New("skills: invalid receipt")
	ErrReceiptWriteFailed          = errors.New("skills: receipt write failed")
	ErrInvalidPackageName          = errors.New("skills: invalid package name")
	ErrDirSyncUnsupported          = errors.New("skills: directory sync unsupported on this platform")
)

type Target struct {
	CanonicalRoot string
	WorkerAliases []WorkerType
	ReasonCode    string
}

type Options struct {
	Profile     builtin.Profile
	WorkerTypes []WorkerType
	DryRun      bool
}

type Report struct {
	Profile builtin.Profile `json:"profile"`
	Items   []Item          `json:"items"`
}

type Item struct {
	Target        string       `json:"target"`
	WorkerAliases []WorkerType `json:"worker_aliases"`
	Action        string       `json:"action"`
	Outcome       string       `json:"outcome"`
	ReasonCode    string       `json:"reason_code"`
	BackupPath    string       `json:"backup_path,omitempty"`
}

// PackageTargetIdentity returns the canonical native-root/package identity
// used by native projection items and receipts. Invalid package names fail
// closed instead of being mapped to a fallback path.
func PackageTargetIdentity(canonicalNativeRoot, packageName string) (string, error) {
	if !validPackageName(packageName) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPackageName, packageName)
	}
	root, err := filepath.Abs(filepath.Clean(canonicalNativeRoot))
	if err != nil {
		return "", err
	}
	canonical, canonicalErr := canonicalOSPath(root)
	if canonicalErr != nil {
		return "", canonicalErr
	}
	root = canonical
	return filepath.Join(root, packageName), nil
}

func validPackageName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.ContainsAny(name, `/\\`) && filepath.Base(name) == name
}

type Paths struct {
	UserHome     string
	HotplexHome  string
	InventoryDir string
	StateDir     string
	NativeRoots  map[WorkerType]string
}

type FileSystem interface {
	Lstat(string) (os.FileInfo, error)
	ReadDir(string) ([]os.DirEntry, error)
	ReadFile(string) ([]byte, error)
	MkdirAll(string, os.FileMode) error
	WriteFile(string, []byte, os.FileMode) error
	Rename(string, string) error
	Remove(string) error
	RemoveAll(string) error
	EvalSymlinks(string) (string, error)
	SyncFile(string) error
	SyncDir(string) error
	MkdirTemp(string, string) (string, error)
	CreateTemp(string, string, []byte, os.FileMode) (string, error)
}

type Reconciler struct {
	registry *builtin.Registry
	paths    Paths
	fs       FileSystem
}

type Runner interface {
	Status(context.Context, Options) (Report, error)
	Sync(context.Context, Options) (Report, error)
	Remove(context.Context, Options) (Report, error)
}

func ParseWorkerType(value string) (WorkerType, error) {
	switch WorkerType(strings.TrimSpace(value)) {
	case WorkerClaude:
		return WorkerClaude, nil
	case WorkerCodex:
		return WorkerCodex, nil
	case WorkerOpenCode:
		return WorkerOpenCode, nil
	case WorkerACP:
		return WorkerACP, nil
	default:
		return "", errors.Join(ErrUnknownWorker, errors.New("invalid worker value"))
	}
}

func parseProfile(profile builtin.Profile) error {
	if profile != builtin.ProfileRuntime && profile != builtin.ProfileOperator {
		return errors.Join(ErrUnknownProfile, errors.New("invalid profile value"))
	}
	return nil
}

func (r Report) Err() error {
	for _, item := range r.Items {
		switch item.Outcome {
		case OutcomeConflict, OutcomeDrift, OutcomeFailed:
			return ErrReportActionRequired
		}
	}
	return nil
}

func cloneWorkerAliases(aliases []WorkerType) []WorkerType {
	return append([]WorkerType(nil), aliases...)
}
