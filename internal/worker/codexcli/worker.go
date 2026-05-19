package codexcli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/internal/worker/proc"
	"github.com/hrygo/hotplex/pkg/events"
)

var _ worker.Worker = (*ExecWorker)(nil)

func init() {
	worker.Register(worker.TypeCodexCLI, func() (worker.Worker, error) {
		cfg := GetConfig()
		if cfg.UseAppServer {
			s := GetSingleton()
			if s == nil {
				return nil, fmt.Errorf("codexcli: app-server singleton not initialized")
			}
			return &AppServerWorker{
				BaseWorker: base.NewBaseWorker(slog.Default(), nil),
				manager:    s,
			}, nil
		}
		return &ExecWorker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}, nil
	})
}

type ExecWorker struct {
	*base.BaseWorker

	cfg         Config
	mu          sync.Mutex
	started     bool
	sessionID   string
	projectDir  string
	origSession worker.SessionInfo

	threadID string

	parser *Parser
	mapper *Mapper
	cancel context.CancelFunc

	readLineFn func() (string, error)
	testConn   worker.SessionConn
}

type Config struct {
	Command        string
	Model          string
	Sandbox        string
	ApprovalMode   string
	Ephemeral      bool
	StartupTimeout time.Duration
}

func (w *ExecWorker) Type() worker.WorkerType { return worker.TypeCodexCLI }
func (w *ExecWorker) SupportsResume() bool    { return true }
func (w *ExecWorker) SupportsStreaming() bool { return true }
func (w *ExecWorker) SupportsTools() bool     { return true }
func (w *ExecWorker) EnvBlocklist() []string  { return EnvBlocklist }
func (w *ExecWorker) SessionStoreDir() string { return "" }
func (w *ExecWorker) MaxTurns() int           { return 0 }
func (w *ExecWorker) Modalities() []string    { return []string{"text", "code"} }

func (w *ExecWorker) Start(ctx context.Context, session worker.SessionInfo) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.startLocked(session)
}

func (w *ExecWorker) startLocked(session worker.SessionInfo) error {
	if w.started {
		return fmt.Errorf("codexcli: already started")
	}

	w.cfg = resolveConfig()
	w.sessionID = session.SessionID
	w.projectDir = session.ProjectDir

	if w.origSession.SessionID == "" {
		w.origSession = session
	}

	w.parser = NewParser()
	w.mapper = NewMapper(session.SessionID)

	w.started = true
	return nil
}

func resolveConfig() Config {
	gc := GetConfig()
	return Config{
		Command:        gc.Command,
		Model:          gc.Model,
		Sandbox:        gc.Sandbox,
		ApprovalMode:   gc.ApprovalMode,
		Ephemeral:      gc.Ephemeral,
		StartupTimeout: gc.StartupTimeout,
	}
}

func (w *ExecWorker) buildArgs(session worker.SessionInfo, prompt string) []string {
	args := []string{
		"exec", "--json",
		"--sandbox", w.cfg.Sandbox,
		"--ask-for-approval", w.cfg.ApprovalMode,
		"--cd", session.ProjectDir,
	}

	if w.cfg.Ephemeral {
		args = append(args, "--ephemeral")
	}

	if w.cfg.Model != "" {
		args = append(args, "-m", w.cfg.Model)
	}

	if session.ResumeSessionID != "" {
		args = append(args, "resume", session.ResumeSessionID)
	} else if w.threadID != "" {
		args = append(args, "resume", w.threadID)
	}

	if prompt != "" {
		args = append(args, prompt)
	}

	return args
}

func (w *ExecWorker) spawn(_ context.Context, prompt string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	session := w.origSession
	if session.SessionID == "" {
		session.SessionID = w.sessionID
		session.ProjectDir = w.projectDir
	}

	args := w.buildArgs(session, prompt)

	w.Proc = proc.New(proc.Opts{
		Logger: w.Log,
	})
	w.Proc.SetPIDKey(session.SessionID)

	env := base.BuildEnv(session, w.EnvBlocklist(), "codex-cli")

	bgCtx := context.Background()
	stdin, stdout, _, err := w.Proc.Start(bgCtx, w.cfg.Command, args, env, session.ProjectDir)
	if err != nil {
		w.Proc = nil
		return fmt.Errorf("codexcli: start: %w", err)
	}

	childCtx, cancel := context.WithCancel(bgCtx)
	w.cancel = cancel

	if w.readLineFn == nil {
		w.readLineFn = w.Proc.ReadLine
	}

	conn := base.NewConn(w.Log, stdin, session.UserID, session.SessionID)
	w.SetConnLocked(conn)

	w.StartTime = time.Now()
	w.SetLastIO(w.StartTime)

	go w.readOutput(childCtx, stdout, conn)
	return nil
}

func (w *ExecWorker) Input(ctx context.Context, content string, metadata map[string]any) error {
	handled, err := base.DispatchMetadata(ctx, metadata, w)
	if err != nil {
		return err
	}
	if handled {
		w.SetLastIO(time.Now())
		return nil
	}

	w.mu.Lock()
	alreadyRunning := w.Proc != nil
	w.mu.Unlock()

	if alreadyRunning {
		return fmt.Errorf("%w: codex exec is one-shot per process; use Resume for follow-up",
			worker.ErrNotImplemented)
	}

	return w.spawn(ctx, content)
}

func (w *ExecWorker) Resume(ctx context.Context, session worker.SessionInfo) error {
	w.mu.Lock()
	w.started = false
	w.threadID = ""
	w.mu.Unlock()

	if err := w.BaseWorker.Terminate(ctx); err != nil {
		w.Log.Warn("codexcli: resume terminate", "error", err)
	}

	return w.Start(ctx, session)
}

func (w *ExecWorker) ResetContext(ctx context.Context) error {
	if err := w.BaseWorker.Terminate(ctx); err != nil {
		w.Log.Warn("codexcli: reset terminate", "error", err)
	}
	w.mu.Lock()
	w.started = false
	w.threadID = ""
	w.readLineFn = nil
	w.mu.Unlock()
	return nil
}

func (w *ExecWorker) Terminate(ctx context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	return w.BaseWorker.Terminate(ctx)
}

func (w *ExecWorker) Conn() worker.SessionConn {
	if w.testConn != nil {
		return w.testConn
	}
	return w.BaseWorker.Conn()
}

func (w *ExecWorker) Health() worker.WorkerHealth {
	return w.BaseWorker.Health(worker.TypeCodexCLI)
}

func (w *ExecWorker) LastIO() time.Time {
	return w.BaseWorker.LastIO()
}

func (w *ExecWorker) readOutput(ctx context.Context, stdout io.Reader, entryConn *base.Conn) {
	defer func() {
		if r := recover(); r != nil {
			w.Log.Error("codexcli: readOutput panic",
				"session_id", w.sessionID, "panic", r)
		}
		if entryConn != nil {
			_ = entryConn.Close()
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	var turnFailed bool

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		event, err := w.parser.ParseLine(line)
		if err != nil {
			w.Log.Warn("codexcli: parse error", "line", line, "error", err)
			continue
		}

		if event.Type == EventThreadStarted {
			w.mu.Lock()
			w.threadID = event.ThreadID
			w.mu.Unlock()
		}

		envelopes := w.mapper.Map(event)
		for _, env := range envelopes {
			if env == nil {
				continue
			}
			w.trySend(env)
		}

		switch event.Type {
		case EventTurnFailed:
			turnFailed = true
		case EventTurnCompleted:
			return
		case EventError:
			if !turnFailed {
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		w.Log.Error("codexcli: stdout read error", "error", err)
	}
}

func (w *ExecWorker) trySend(env *events.Envelope) {
	conn := w.Conn()
	if conn == nil {
		return
	}
	switch env.Event.Type {
	case events.Done, events.Error, events.State:
		_ = conn.Send(context.Background(), env)
	default:
		ts, ok := conn.(interface{ TrySend(*events.Envelope) bool })
		if !ok {
			return
		}
		if !ts.TrySend(env) {
			w.Log.Warn("codexcli: recv channel full, dropping",
				"session_id", w.sessionID, "event_type", env.Event.Type)
		}
	}
}

func (w *ExecWorker) HandlePermissionResponse(_ context.Context, reqID string, allowed bool, reason string) error {
	return fmt.Errorf("codexcli: permission responses not supported in one-shot mode")
}

func (w *ExecWorker) HandleQuestionResponse(_ context.Context, reqID string, answers map[string]string) error {
	return fmt.Errorf("codexcli: question responses not supported in one-shot mode")
}

func (w *ExecWorker) HandleElicitationResponse(_ context.Context, reqID, action string, content map[string]any) error {
	return fmt.Errorf("codexcli: elicitation responses not supported in one-shot mode")
}

func (w *ExecWorker) SetTestConn(c worker.SessionConn) {
	w.testConn = c
}

func (w *ExecWorker) SetReadLineFn(fn func() (string, error)) {
	w.readLineFn = fn
}

// ─── AppServerWorker (v2 persistent mode) ───────────────────────────────

var _ worker.Worker = (*AppServerWorker)(nil)

type AppServerWorker struct {
	*base.BaseWorker

	manager     *CodexAppServerManager
	threadID    string
	userID      string
	releaseOnce sync.Once
	crashSub    <-chan struct{}
	mu          sync.Mutex
	recvCh      chan *events.Envelope
	commands    *ServerCommander
	closed      bool
	sessionID   string
	conn        *appConn
}

// appConn implements worker.SessionConn for the app-server mode.
type appConn struct {
	userID    string
	sessionID string
	recvCh    chan *events.Envelope
	mu        sync.Mutex
	closed    bool
	manager   *CodexAppServerManager
}

// Send returns ErrNotImplemented because in app-server mode the manager
// handles all communication via JSON-RPC. Writing AEP envelopes directly
// to stdin would bypass the JSON-RPC protocol and break the codex process.
func (c *appConn) Send(ctx context.Context, msg *events.Envelope) error {
	return worker.ErrNotImplemented
}
func (c *appConn) Recv() <-chan *events.Envelope { return c.recvCh }
func (c *appConn) TrySend(env *events.Envelope) bool {
	select {
	case c.recvCh <- env:
		return true
	default:
		return false
	}
}
func (c *appConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.recvCh)
	return nil
}
func (c *appConn) UserID() string    { return c.userID }
func (c *appConn) SessionID() string { return c.sessionID }

func (w *AppServerWorker) Type() worker.WorkerType { return worker.TypeCodexCLI }
func (w *AppServerWorker) SupportsResume() bool    { return true }
func (w *AppServerWorker) SupportsStreaming() bool { return true }
func (w *AppServerWorker) SupportsTools() bool     { return true }
func (w *AppServerWorker) EnvBlocklist() []string  { return EnvBlocklist }
func (w *AppServerWorker) SessionStoreDir() string { return "" }
func (w *AppServerWorker) MaxTurns() int           { return 0 }
func (w *AppServerWorker) Modalities() []string    { return []string{"text", "code"} }

func (w *AppServerWorker) Start(ctx context.Context, session worker.SessionInfo) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.recvCh != nil {
		return fmt.Errorf("codexcli: app-server already started")
	}

	crashCh, err := w.manager.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("codexcli: acquire: %w", err)
	}
	w.crashSub = crashCh

	cfg := resolveConfig()
	params := map[string]any{
		"cwd":         session.ProjectDir,
		"sandbox":     cfg.Sandbox,
		"personality": "friendly",
	}
	if cfg.Model != "" {
		params["model"] = cfg.Model
	}
	if cfg.Ephemeral {
		params["ephemeral"] = true
	}

	resp, err := w.manager.Call("thread/start", params)
	if err != nil {
		w.manager.Release()
		return fmt.Errorf("codexcli: thread/start: %w", err)
	}

	var result ThreadStartResult
	if err := json.Unmarshal(resp, &result); err != nil {
		w.manager.Release()
		return fmt.Errorf("codexcli: parse thread/start: %w", err)
	}

	w.threadID = result.Thread.ID
	w.sessionID = session.SessionID
	w.userID = session.UserID

	w.recvCh = w.manager.Subscribe(w.threadID, w.sessionID)
	w.commands = NewServerCommander(w.manager, w.threadID)
	w.conn = &appConn{
		userID:    w.userID,
		sessionID: w.sessionID,
		recvCh:    w.recvCh,
		manager:   w.manager,
	}
	w.StartTime = time.Now()
	w.SetLastIO(w.StartTime)

	return nil
}

func (w *AppServerWorker) Input(ctx context.Context, content string, metadata map[string]any) error {
	handled, err := base.DispatchMetadata(ctx, metadata, w)
	if err != nil {
		return err
	}
	if handled {
		w.SetLastIO(time.Now())
		return nil
	}

	w.mu.Lock()
	tid := w.threadID
	w.mu.Unlock()
	if tid == "" {
		return fmt.Errorf("codexcli: app-server not started")
	}

	params := TurnStartParams{
		ThreadID: tid,
		Input: []TurnInputItem{
			{Type: "text", Text: content},
		},
	}

	_, err = w.manager.Call("turn/start", params)
	if err != nil {
		return fmt.Errorf("codexcli: turn/start: %w", err)
	}

	w.SetLastIO(time.Now())
	return nil
}

func (w *AppServerWorker) Resume(ctx context.Context, session worker.SessionInfo) error {
	return w.Start(ctx, session)
}

func (w *AppServerWorker) Terminate(ctx context.Context) error {
	w.release()
	return nil
}

func (w *AppServerWorker) Kill() error {
	w.release()
	return nil
}

func (w *AppServerWorker) Wait() (int, error) {
	if w.crashSub == nil {
		return 0, nil
	}
	select {
	case <-w.crashSub:
		return 1, nil
	case <-time.After(2 * time.Second):
		return 0, nil
	}
}

func (w *AppServerWorker) release() {
	w.releaseOnce.Do(func() {
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return
		}
		w.closed = true
		tid := w.threadID
		w.mu.Unlock()

		if w.manager != nil && tid != "" {
			_ = w.manager.Notify("thread/unsubscribe", ThreadUnsubscribeParams{
				ThreadID: tid,
			})
			w.manager.Unsubscribe(tid)
			w.manager.Release()
		}
	})
}

func (w *AppServerWorker) ResetContext(ctx context.Context) error {
	if err := w.Terminate(ctx); err != nil {
		w.Log.Warn("codexcli: reset terminate", "error", err)
	}
	w.mu.Lock()
	w.threadID = ""
	w.recvCh = nil
	w.closed = false
	w.mu.Unlock()
	return nil
}

func (w *AppServerWorker) Conn() worker.SessionConn {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn
}

func (w *AppServerWorker) Health() worker.WorkerHealth {
	return w.BaseWorker.Health(worker.TypeCodexCLI)
}

func (w *AppServerWorker) LastIO() time.Time {
	return w.BaseWorker.LastIO()
}

func (w *AppServerWorker) HandlePermissionResponse(ctx context.Context, reqID string, allowed bool, reason string) error {
	return fmt.Errorf("codexcli: permission responses not supported in app-server mode yet")
}

func (w *AppServerWorker) HandleQuestionResponse(ctx context.Context, reqID string, answers map[string]string) error {
	return fmt.Errorf("codexcli: question responses not supported in app-server mode yet")
}

func (w *AppServerWorker) HandleElicitationResponse(ctx context.Context, reqID, action string, content map[string]any) error {
	return fmt.Errorf("codexcli: elicitation responses not supported in app-server mode yet")
}
