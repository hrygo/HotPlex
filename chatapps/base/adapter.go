package base

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Config is the common configuration for all adapters
type Config struct {
	ServerAddr   string
	SystemPrompt string
}

// Session represents a user session in a chat platform
type Session struct {
	SessionID  string
	UserID     string
	Platform   string
	LastActive time.Time
}

// MetadataExtractor extracts platform-specific metadata from incoming requests
type MetadataExtractor func(update any) map[string]any

// MessageParser parses incoming requests into ChatMessage
type MessageParser func(body []byte, metadata map[string]any) (*ChatMessage, error)

// MessageSender sends messages to the platform
type MessageSender func(ctx context.Context, sessionID string, msg *ChatMessage) error

// Adapter is the base adapter implementing common functionality
type Adapter struct {
	config         Config
	logger         *slog.Logger
	server         *http.Server
	sessions       map[string]*Session
	mu             sync.RWMutex
	handler        MessageHandler
	running        bool
	runningMu      sync.Mutex // Protects running state
	sessionTimeout time.Duration
	cleanupDone    chan struct{}
	cleanupOnce    sync.Once // Prevents double-close of cleanupDone

	// Platform-specific implementations
	platformName    string
	metadataExtract MetadataExtractor
	messageParser   MessageParser
	messageSender   MessageSender
	httpHandlers    map[string]http.HandlerFunc

	// Server control
	disableServer bool

	// Session ID generator for deterministic session mapping
	sessionIDGenerator SessionIDGenerator

	// Secondary index for O(1) session lookup by user+channel
	sessionsByUserChannel map[string]*Session // key: "userID:channelID"
	indexMu               sync.RWMutex        // Protects sessionsByUserChannel
}

// NewAdapter creates a new base adapter
func NewAdapter(
	platform string,
	config Config,
	logger *slog.Logger,
	opts ...AdapterOption,
) *Adapter {
	if config.ServerAddr == "" {
		config.ServerAddr = ":8080"
	}

	a := &Adapter{
		config:                config,
		logger:                logger,
		sessions:              make(map[string]*Session),
		sessionsByUserChannel: make(map[string]*Session),
		sessionTimeout:        30 * time.Minute,
		cleanupDone:           make(chan struct{}),
		platformName:          platform,
		httpHandlers:          make(map[string]http.HandlerFunc),
		sessionIDGenerator:    NewUUID5Generator("hotplex"), // Default to UUID5 generator
	}

	for _, opt := range opts {
		opt(a)
	}

	return a
}

// AdapterOption configures the base adapter
type AdapterOption func(*Adapter)

// WithSessionTimeout sets the session timeout
func WithSessionTimeout(timeout time.Duration) AdapterOption {
	return func(a *Adapter) {
		a.sessionTimeout = timeout
	}
}

// WithMetadataExtractor sets the metadata extractor
func WithMetadataExtractor(extractor MetadataExtractor) AdapterOption {
	return func(a *Adapter) {
		a.metadataExtract = extractor
	}
}

// WithMessageParser sets the message parser
func WithMessageParser(parser MessageParser) AdapterOption {
	return func(a *Adapter) {
		a.messageParser = parser
	}
}

// WithMessageSender sets the message sender
func WithMessageSender(sender MessageSender) AdapterOption {
	return func(a *Adapter) {
		a.messageSender = sender
	}
}

// WithHTTPHandler adds an HTTP handler
func WithHTTPHandler(path string, handler http.HandlerFunc) AdapterOption {
	return func(a *Adapter) {
		a.httpHandlers[path] = handler
	}
}

// WithoutServer disables the embedded HTTP server
// Use this when running adapters under a unified server
func WithoutServer() AdapterOption {
	return func(a *Adapter) {
		a.disableServer = true
	}
}

// Platform returns the platform name
func (a *Adapter) Platform() string {
	return a.platformName
}

// SystemPrompt returns the system prompt
func (a *Adapter) SystemPrompt() string {
	return a.config.SystemPrompt
}

// SetHandler sets the message handler (thread-safe)
func (a *Adapter) SetHandler(handler MessageHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handler = handler
}

// Handler returns the message handler (thread-safe)
func (a *Adapter) Handler() MessageHandler {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.handler
}

// Logger returns the logger
func (a *Adapter) Logger() *slog.Logger {
	return a.logger
}

// SetLogger sets the logger
func (a *Adapter) SetLogger(logger *slog.Logger) {
	a.logger = logger
}

// WebhookPath returns the primary webhook path for this adapter
func (a *Adapter) WebhookPath() string {
	for path := range a.httpHandlers {
		return path
	}
	return ""
}

// WebhookHandler returns an http.Handler with all webhook endpoints registered
func (a *Adapter) WebhookHandler() http.Handler {
	mux := http.NewServeMux()
	for path, handler := range a.httpHandlers {
		mux.HandleFunc(path, handler)
	}
	return mux
}

// Start starts the adapter
func (a *Adapter) Start(ctx context.Context) error {
	a.runningMu.Lock()
	defer a.runningMu.Unlock()

	if a.running {
		return nil
	}

	// Serverless mode: skip HTTP server, just start session cleanup
	if a.disableServer {
		go a.cleanupSessions()
		a.running = true
		a.logger.Debug("Adapter started (serverless mode)", "platform", a.platformName)
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.handleHealth)

	for path, handler := range a.httpHandlers {
		mux.HandleFunc(path, handler)
	}

	a.server = &http.Server{
		Addr:    a.config.ServerAddr,
		Handler: mux,
	}

	go func() {
		a.logger.Info("Starting adapter", "platform", a.platformName, "addr", a.config.ServerAddr)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Error("Server error", "platform", a.platformName, "error", err)
		}
	}()

	// Start session cleanup goroutine
	go a.cleanupSessions()

	a.running = true
	return nil
}

// Stop stops the adapter
func (a *Adapter) Stop() error {
	a.runningMu.Lock()
	defer a.runningMu.Unlock()

	if !a.running {
		return nil
	}

	// Signal cleanup goroutine to stop (use Once to prevent double-close)
	a.cleanupOnce.Do(func() {
		close(a.cleanupDone)
	})

	// Skip server shutdown in serverless mode
	if a.disableServer {
		a.running = false
		a.logger.Info("Adapter stopped (serverless mode)", "platform", a.platformName)
		return nil
	}

	if a.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := a.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
	}

	a.running = false
	a.logger.Info("Adapter stopped", "platform", a.platformName)
	return nil
}

// SendMessage sends a message (requires messageSender to be set)
func (a *Adapter) SendMessage(ctx context.Context, sessionID string, msg *ChatMessage) error {
	if a.messageSender == nil {
		return fmt.Errorf("message sender not configured")
	}
	return a.messageSender(ctx, sessionID, msg)
}

// HandleMessage handles incoming message (stub for interface compliance)
func (a *Adapter) HandleMessage(ctx context.Context, msg *ChatMessage) error {
	return nil
}

// GetSession retrieves a session by key
func (a *Adapter) GetSession(key string) (*Session, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	session, ok := a.sessions[key]
	return session, ok
}

// GetOrCreateSession gets or creates a session using deterministic session ID generation
// Parameters:
//   - userID: the user's ID on the platform
//   - botUserID: the bot's user ID (for multi-bot scenarios, empty for single bot)
//   - channelID: the channel/room ID (empty for DM)
//
// Returns the generated session ID (deterministic based on inputs)
func (a *Adapter) GetOrCreateSession(userID, botUserID, channelID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Generate deterministic key from all components
	key := fmt.Sprintf("%s:%s:%s:%s", a.platformName, userID, botUserID, channelID)

	if session, ok := a.sessions[key]; ok {
		session.LastActive = time.Now()
		return session.SessionID
	}

	// Generate deterministic session ID using UUID5
	sessionID := a.sessionIDGenerator.Generate(a.platformName, userID, botUserID, channelID)

	session := &Session{
		SessionID:  sessionID,
		UserID:     userID,
		Platform:   a.platformName,
		LastActive: time.Now(),
	}
	a.sessions[key] = session

	// Update secondary index for O(1) lookup by user+channel
	userChannelKey := userID + ":" + channelID
	a.indexMu.Lock()
	a.sessionsByUserChannel[userChannelKey] = session
	a.indexMu.Unlock()

	a.logger.Info("Session created",
		"session", sessionID,
		"user", userID,
		"bot", botUserID,
		"channel", channelID)
	return sessionID
}

// cleanupSessions periodically removes expired sessions
func (a *Adapter) cleanupSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-a.cleanupDone:
			a.logger.Info("Session cleanup stopped", "platform", a.platformName)
			return
		case <-ticker.C:
			a.mu.Lock()
			now := time.Now()
			for key, session := range a.sessions {
				if now.Sub(session.LastActive) > a.sessionTimeout {
					delete(a.sessions, key)
					a.logger.Debug("Session removed", "session", session.SessionID, "inactive", now.Sub(session.LastActive))
				}
			}
			a.mu.Unlock()
		}
	}
}

func (a *Adapter) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "OK")
}

func ReadBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

func RespondWithError(w http.ResponseWriter, code int, message string) {
	http.Error(w, message, code)
}

func RespondWithJSON(w http.ResponseWriter, code int, data any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	return json.NewEncoder(w).Encode(data)
}

func RespondWithText(w http.ResponseWriter, code int, text string) {
	w.WriteHeader(code)
	_, _ = fmt.Fprint(w, text)
}

// FindSessionByUserAndChannel finds a session by matching user_id and channel_id
// This is useful for slash commands where we don't have the exact key
// Performance: O(1) using secondary index
func (a *Adapter) FindSessionByUserAndChannel(userID, channelID string) *Session {
	// Use secondary index for O(1) lookup
	userChannelKey := userID + ":" + channelID

	a.indexMu.RLock()
	defer a.indexMu.RUnlock()

	session, exists := a.sessionsByUserChannel[userChannelKey]
	if exists {
		return session
	}
	return nil
}

// Compile-time interface compliance checks
var (
	_ ChatAdapter       = (*Adapter)(nil)
	_ MessageOperations = (*Adapter)(nil)
	_ SessionOperations = (*Adapter)(nil)
)

// MessageOperations default implementations (no-op for platforms that don't support them)

// DeleteMessage is a no-op by default, overridden by platforms that support it
func (a *Adapter) DeleteMessage(ctx context.Context, channelID, messageTS string) error {
	return nil
}

// AddReaction is a no-op by default, overridden by platforms that support it
func (a *Adapter) AddReaction(ctx context.Context, reaction Reaction) error {
	return nil
}

// RemoveReaction is a no-op by default, overridden by platforms that support it
func (a *Adapter) RemoveReaction(ctx context.Context, reaction Reaction) error {
	return nil
}

// UpdateMessage is a no-op by default, overridden by platforms that support it
func (a *Adapter) UpdateMessage(ctx context.Context, channelID, messageTS string, msg *ChatMessage) error {
	return nil
}

// EngineSupport defines optional interface for adapters that need engine integration
type EngineSupport interface {
	SetEngine(eng any)
}
