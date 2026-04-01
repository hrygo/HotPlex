package claudecode

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"hotplex-worker/internal/aep"
	"hotplex-worker/internal/worker/base"
	"hotplex-worker/internal/worker/proc"

	"hotplex-worker/internal/worker"
	"hotplex-worker/pkg/events"
)

// Compile-time interface compliance checks.
var (
	_ worker.Worker = (*Worker)(nil)
)

// Env whitelist for Claude Code worker.
var claudeCodeEnvWhitelist = []string{
	"HOME", "USER", "SHELL", "PATH", "TERM",
	"LANG", "LC_ALL", "PWD",
	"CLAUDE_API_KEY", "CLAUDE_MODEL", "CLAUDE_BASE_URL",
	"CLAUDE_CODE_MODE", "CLAUDE_DISABLE_AUTO_PERMISSIONS",
}

// Default session store directory.
const defaultSessionStoreDir = ".claude/projects"

// Worker implements the Claude Code worker adapter.
type Worker struct {
	*base.BaseWorker
	sessionID string
	userID    string
	started   bool
}

// New creates a new Claude Code worker.
func New() *Worker {
	return &Worker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
	}
}

// ─── Capabilities ─────────────────────────────────────────────────────────────

func (w *Worker) Type() worker.WorkerType { return worker.TypeClaudeCode }

func (w *Worker) SupportsResume() bool    { return true }
func (w *Worker) SupportsStreaming() bool { return true }
func (w *Worker) SupportsTools() bool     { return true }
func (w *Worker) EnvWhitelist() []string  { return claudeCodeEnvWhitelist }
func (w *Worker) SessionStoreDir() string { return defaultSessionStoreDir }
func (w *Worker) MaxTurns() int           { return 0 }
func (w *Worker) Modalities() []string    { return []string{"text", "code", "image"} }

// ─── Worker ─────────────────────────────────────────────────────────────────

func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
	w.Mu.Lock()
	defer w.Mu.Unlock()

	if w.Proc != nil {
		return fmt.Errorf("claudecode: already started")
	}

	// Build command arguments.
	args := []string{
		"--print",
		"--session-id", session.SessionID,
	}
	if len(session.AllowedModels) > 0 {
		args = append(args, "--model", session.AllowedModels[0])
	}

	// Build environment.
	env := base.BuildEnv(session, claudeCodeEnvWhitelist, "claude-code")

	// Create process manager.
	w.Proc = proc.New(proc.Opts{
		Logger:       w.Log,
		AllowedTools: session.AllowedTools,
	})

	// Start the process.
	stdin, _, _, err := w.Proc.Start(ctx, "claude", args, env, session.ProjectDir)
	if err != nil {
		w.Proc = nil
		return fmt.Errorf("claudecode: start: %w", err)
	}

	// Create session connection (caller holds w.Mu).
	w.userID = session.UserID
	w.sessionID = session.SessionID
	w.SetConnLocked(base.NewConn(w.Log, stdin, session.UserID, session.SessionID))

	w.StartTime = time.Now()
	w.SetLastIO(w.StartTime)

	// Start output reader goroutine.
	go w.readOutput()

	w.started = true
	return nil
}

func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
	conn := w.BaseWorker.Conn()
	if conn == nil {
		return fmt.Errorf("claudecode: not started")
	}

	msg := events.NewEnvelope(
		aep.NewID(),
		w.sessionID,
		0, // seq assigned by hub
		events.Input,
		events.InputData{
			Content:  content,
			Metadata: metadata,
		},
	)

	if err := conn.Send(ctx, msg); err != nil {
		return fmt.Errorf("claudecode: input: %w", err)
	}

	w.SetLastIO(time.Now())
	return nil
}

func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
	w.Mu.Lock()
	defer w.Mu.Unlock()

	if w.Proc != nil {
		return fmt.Errorf("claudecode: already started")
	}

	// Build command arguments for resume.
	args := []string{
		"--print",
		"--resume",
		"--session-id", session.SessionID,
	}
	if len(session.AllowedModels) > 0 {
		args = append(args, "--model", session.AllowedModels[0])
	}

	// Build environment.
	env := base.BuildEnv(session, claudeCodeEnvWhitelist, "claude-code")

	// Create process manager.
	w.Proc = proc.New(proc.Opts{
		Logger:       w.Log,
		AllowedTools: session.AllowedTools,
	})

	// Start the process.
	stdin, _, _, err := w.Proc.Start(ctx, "claude", args, env, session.ProjectDir)
	if err != nil {
		w.Proc = nil
		return fmt.Errorf("claudecode: resume: %w", err)
	}

	// Create session connection.
	w.userID = session.UserID
	w.sessionID = session.SessionID
	w.SetConnLocked(base.NewConn(w.Log, stdin, session.UserID, session.SessionID))

	w.StartTime = time.Now()
	w.SetLastIO(w.StartTime)

	// Start output reader goroutine.
	go w.readOutput()

	w.started = true
	return nil
}

// Conn returns the session connection.
func (w *Worker) Conn() worker.SessionConn {
	return w.BaseWorker.Conn()
}

// Health returns a snapshot of the worker's runtime health.
func (w *Worker) Health() worker.WorkerHealth {
	return w.BaseWorker.Health(worker.TypeClaudeCode)
}

// LastIO returns the time of the last I/O activity.
func (w *Worker) LastIO() time.Time {
	return w.BaseWorker.LastIO()
}

// ─── Internal ────────────────────────────────────────────────────────────────

func (w *Worker) readOutput() {
	defer func() {
		if c := w.BaseWorker.Conn(); c != nil {
			c.Close()
		}
	}()

	w.Mu.Lock()
	proc := w.Proc
	w.Mu.Unlock()
	if proc == nil {
		return
	}

	for {
		line, err := proc.ReadLine()
		if err != nil {
			if err == io.EOF {
				return
			}
			w.Log.Error("claudecode: read line", "error", err)
			return
		}

		if line == "" {
			continue
		}

		env, err := aep.DecodeLine([]byte(line))
		if err != nil {
			w.Log.Warn("claudecode: decode line", "error", err, "line", line)
			continue
		}

		w.SetLastIO(time.Now())

		conn := w.BaseWorker.Conn()
		if conn == nil {
			return
		}

		if bc, ok := conn.(*base.Conn); ok {
			if !bc.TrySend(env) {
				w.Log.Warn("claudecode: recv channel full, dropping message")
			}
		}
	}
}

// ─── Init ────────────────────────────────────────────────────────────────────

func init() {
	worker.Register(worker.TypeClaudeCode, func() (worker.Worker, error) {
		return &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}, nil
	})
}
