package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hrygo/hotplex/internal/panicx"
)

// SocketModeConfig holds configuration for Socket Mode connection
type SocketModeConfig struct {
	AppToken string // xapp-* token
	BotToken string // xoxb-* token
}

// SocketModeConnection manages a WebSocket connection to Slack's Socket Mode
type SocketModeConnection struct {
	mu              sync.RWMutex
	conn            *websocket.Conn
	config          SocketModeConfig
	logger          *slog.Logger
	ctx             context.Context
	cancel          context.CancelFunc
	reconnects      int
	maxReconnects   int
	connected       bool
	handlers        map[string]EventHandler
	onReconnect     func()      // Called when successfully reconnected
	onReconnectFail func(error) // Called when reconnection fails
}

// EventHandler handles incoming Slack events
type EventHandler func(eventType string, data json.RawMessage)

// SocketModeURL is the Slack Socket Mode WebSocket endpoint
const SocketModeURL = "wss://wss.slack.com/ws"

// NewSocketModeConnection creates a new Socket Mode connection
func NewSocketModeConnection(config SocketModeConfig, logger *slog.Logger) *SocketModeConnection {
	ctx, cancel := context.WithCancel(context.Background())
	return &SocketModeConnection{
		config:        config,
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		reconnects:    0,
		maxReconnects: 5,
		handlers:      make(map[string]EventHandler),
	}
}

// RegisterHandler registers an event handler for a specific event type
func (s *SocketModeConnection) RegisterHandler(eventType string, handler EventHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[eventType] = handler
}

// SetReconnectCallbacks sets callbacks for reconnection events
func (s *SocketModeConnection) SetReconnectCallbacks(onReconnect func(), onReconnectFail func(error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onReconnect = onReconnect
	s.onReconnectFail = onReconnectFail
}

// Start begins the Socket Mode connection
func (s *SocketModeConnection) Start(ctx context.Context) error {
	s.mu.Lock()
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()

	return s.connect()
}

// Stop closes the Socket Mode connection
func (s *SocketModeConnection) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	if s.conn != nil {
		return s.conn.Close()
	}

	return nil
}

// IsConnected returns true if the connection is active
func (s *SocketModeConnection) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

// connect establishes a WebSocket connection to Slack via apps.connections.open
func (s *SocketModeConnection) connect() error {
	s.logger.Info("Opening Slack Socket Mode connection", "has_app_token", s.config.AppToken != "")

	// Step 1: Call apps.connections.open to get WebSocket URL
	wsURL, err := s.getWebSocketURL()
	if err != nil {
		return fmt.Errorf("failed to get WebSocket URL: %w", err)
	}

	s.logger.Info("Connecting to Slack WebSocket", "url", wsURL)

	// Step 2: Connect to the WebSocket URL
	header := http.Header{}
	header.Set("Authorization", "Bearer "+s.config.AppToken)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		s.logger.Error("Failed to connect to Slack WebSocket", "error", err, "status_code", func() int {
			if resp != nil {
				return resp.StatusCode
			}
			return 0
		}())
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	s.mu.Lock()
	s.conn = conn
	s.connected = true
	s.reconnects = 0
	s.mu.Unlock()

	s.logger.Info("Connected to Slack Socket Mode")

	// Start read loop
	go s.readLoop()

	return nil
}

// getWebSocketURL calls Slack's apps.connections.open API to get the WebSocket URL
func (s *SocketModeConnection) getWebSocketURL() (string, error) {
	// Use App-Level Token (xapp-*) for the API call - NOT Bot Token
	if s.config.AppToken == "" {
		return "", fmt.Errorf("app token is required for apps.connections.open")
	}

	req, err := http.NewRequestWithContext(s.ctx, "POST",
		"https://slack.com/api/apps.connections.open", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Use App-Level Token (xapp-*), not Bot Token
	req.Header.Set("Authorization", "Bearer "+s.config.AppToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("apps.connections.open returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		OK    bool   `json:"ok"`
		URL   string `json:"url"`
		Error string `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.OK {
		return "", fmt.Errorf("apps.connections.open failed: %s", result.Error)
	}

	if result.URL == "" {
		return "", fmt.Errorf("apps.connections.open returned empty URL")
	}

	return result.URL, nil
}

// reconnect attempts to reconnect with permanent retry and exponential backoff
// This implements the fix for issue #33 - Slack Socket Mode connection recovery
func (s *SocketModeConnection) reconnect() {
	s.mu.Lock()
	// Check if already reconnecting to prevent duplicate reconnect attempts
	if s.reconnects > 0 && s.reconnects >= s.maxReconnects {
		s.mu.Unlock()
		s.logger.Warn("Max reconnection attempts reached, giving up")
		return
	}
	s.reconnects++
	reconnectCount := s.reconnects
	onReconnect := s.onReconnect
	onReconnectFail := s.onReconnectFail
	s.mu.Unlock()

	s.logger.Info("Starting reconnection process", "attempt", reconnectCount)

	// Permanent retry loop with exponential backoff
	for {
		select {
		case <-s.ctx.Done():
			s.logger.Info("Reconnection cancelled via context")
			return
		default:
		}

		// Exponential backoff: 1s, 2s, 4s, 8s, 16s, 32s (max 30s)
		delay := time.Duration(1<<uint(min(reconnectCount-1, 4))) * time.Second
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}

		s.logger.Info("Attempting to reconnect", "attempt", reconnectCount, "delay", delay)

		select {
		case <-s.ctx.Done():
			return
		case <-time.After(delay):
		}

		// Clean up old connection before attempting new connection
		s.cleanupConnection()

		if err := s.connect(); err != nil {
			s.logger.Error("Reconnection failed", "error", err, "attempt", reconnectCount)
			// Notify adapter layer about reconnection failure
			if onReconnectFail != nil {
				onReconnectFail(err)
			}
			reconnectCount++
			continue // Keep retrying - this is the key fix for issue #33
		}

		// Successfully reconnected
		s.logger.Info("Reconnection successful")

		// Notify adapter layer about successful reconnection
		if onReconnect != nil {
			onReconnect()
		}
		return
	}
}

// cleanupConnection properly closes and cleans up the WebSocket connection
func (s *SocketModeConnection) cleanupConnection() {
	s.mu.Lock()
	conn := s.conn
	s.conn = nil
	s.connected = false
	s.mu.Unlock()

	if conn != nil {
		// Set a short deadline for close to avoid hanging
		_ = conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
		_ = conn.Close()
		s.logger.Debug("Old connection cleaned up")
	}
}

// readLoop continuously reads messages from the WebSocket
func (s *SocketModeConnection) readLoop() {
	// Recover from panics to prevent process crash
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic recovered in readLoop", "recover", r)
		}

		s.mu.Lock()
		s.connected = false
		s.conn = nil // Clear the connection on exit
		s.mu.Unlock()

		s.logger.Info("WebSocket read loop stopped")
	}()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		s.mu.RLock()
		conn := s.conn
		s.mu.RUnlock()

		if conn == nil {
			s.logger.Debug("Connection is nil, exiting readLoop")
			return
		}

		// Set read deadline to prevent blocking forever
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		_, message, err := conn.ReadMessage()
		if err != nil {
			// Check if it's a WebSocket close error or timeout
			isCloseError := websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure,
				websocket.CloseNoStatusReceived)

			s.logger.Debug("WebSocket read error", "error", err, "is_close_error", isCloseError)

			s.mu.RLock()
			connected := s.connected
			s.mu.RUnlock()

			if connected {
				// Connection lost, attempt reconnect with delay
				// Add delay to allow old connection to fully close
				s.logger.Info("Connection lost, scheduling reconnect with delay")
				panicx.SafeGo(s.logger, func() {
					time.Sleep(1 * time.Second) // Wait for old connection to close
					s.reconnect()
				})
			}
			return
		}

		s.handleMessage(message)
	}
}

// handleMessage processes incoming WebSocket messages
func (s *SocketModeConnection) handleMessage(data []byte) {
	var msg struct {
		Type       string          `json:"type"`
		EnvelopeID string          `json:"envelope_id,omitempty"`
		Payload    json.RawMessage `json:"payload,omitempty"`
		Body       json.RawMessage `json:"body,omitempty"` // fallback for some message types
	}

	if err := json.Unmarshal(data, &msg); err != nil {
		s.logger.Error("Failed to parse message", "error", err)
		return
	}

	// Log all incoming messages for debugging
	s.logger.Debug("[SLACK_WS_MESSAGE] Received WebSocket message",
		"type", msg.Type,
		"envelope_id", msg.EnvelopeID,
		"payload_len", len(msg.Payload),
		"body_len", len(msg.Body))

	switch msg.Type {
	case "hello":
		s.logger.Debug("Received hello from Slack")

	case "disconnect":
		s.logger.Warn("Received disconnect from Slack")
		s.mu.Lock()
		s.connected = false
		s.mu.Unlock()
		panicx.SafeGo(s.logger, func() {
			s.reconnect()
		})

	case "events_api":
		// Socket Mode uses "events_api" with "payload" field
		// payload contains the full event_callback structure
		s.logger.Debug("[SLACK_EVENTS_API] events_api received",
			"envelope_id", msg.EnvelopeID,
			"payload_len", len(msg.Payload))
		if len(msg.Payload) > 0 {
			s.handleEventsAPI(msg.Payload, msg.EnvelopeID)
		} else {
			s.logger.Warn("events_api with empty payload")
		}

	case "slash_commands":
		// Slash commands are sent as direct message type in Socket Mode
		s.logger.Debug("[SLACK_SLASH_COMMAND] slash_commands received",
			"envelope_id", msg.EnvelopeID)
		if len(msg.Payload) > 0 {
			s.handleSlashCommands(msg.Payload, msg.EnvelopeID)
		} else {
			s.logger.Warn("slash_commands with empty payload")
		}
	case "event_callback":
		// Fallback for HTTP webhook compatibility
		s.handleEventCallback(msg.Body)

	case "ping":
		_ = s.sendPong()

	case "pong":
		// Keep-alive acknowledged

	default:
		s.logger.Warn("Unknown message type", "type", msg.Type)
	}
}

// handleEventCallback processes event_callback messages
func (s *SocketModeConnection) handleEventCallback(body json.RawMessage) {
	var eventCallback struct {
		Type   string          `json:"type"`
		Event  json.RawMessage `json:"event,omitempty"`
		Hidden bool            `json:"hidden,omitempty"`
	}

	if err := json.Unmarshal(body, &eventCallback); err != nil {
		s.logger.Error("Failed to parse event callback", "error", err)
		return
	}

	if eventCallback.Event == nil {
		return
	}

	// Parse the inner event to get its type
	var innerEvent struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(eventCallback.Event, &innerEvent); err != nil {
		s.logger.Error("Failed to parse inner event", "error", err)
		return
	}

	// Call registered handler if exists, using the inner event type
	s.mu.RLock()
	handler, exists := s.handlers[innerEvent.Type]
	s.mu.RUnlock()

	if exists && !eventCallback.Hidden {
		handler(innerEvent.Type, eventCallback.Event)
	}
}

// handleEventsAPI processes events_api messages (Socket Mode format)
// The payload contains the event_callback structure directly
func (s *SocketModeConnection) handleEventsAPI(payload json.RawMessage, envelopeID string) {
	if len(payload) == 0 {
		s.logger.Warn("Empty payload in events_api message")
		return
	}

	// Send ACK to Slack to confirm receipt of the event
	// Slack expects a response with the envelope_id within 3 seconds
	if envelopeID != "" {
		if err := s.sendACKWithRetry(envelopeID, 3, 1*time.Second); err != nil {
			s.logger.Error("Failed to send ACK after retries", "envelope_id", envelopeID, "error", err)
		} else {
			s.logger.Debug("Sent ACK for envelope", "envelope_id", envelopeID)
		}
	}

	// The payload IS the event_callback structure, pass it directly
	s.handleEventCallback(payload)
}

// handleSlashCommands processes slash_commands messages from Socket Mode
func (s *SocketModeConnection) handleSlashCommands(payload json.RawMessage, envelopeID string) {
	// Send ACK to Slack
	if envelopeID != "" {
		if err := s.sendACKWithRetry(envelopeID, 3, 1*time.Second); err != nil {
			s.logger.Error("Failed to send ACK for slash command", "envelope_id", envelopeID, "error", err)
		}
	}

	// Parse slash command payload
	var slashData map[string]any
	if err := json.Unmarshal(payload, &slashData); err != nil {
		s.logger.Error("Failed to parse slash command", "error", err)
		return
	}

	s.logger.Debug("Slash command parsed", "command", slashData["command"])

	// Call registered handler if exists
	s.mu.RLock()
	handler, exists := s.handlers["slash_commands"]
	s.mu.RUnlock()

	if exists {
		// Pass the raw payload to the handler
		handler("slash_commands", payload)
	} else {
		s.logger.Warn("No handler registered for slash_commands")
	}
}

// sendACKWithRetry sends an ACK with exponential backoff retry
// maxRetries: maximum number of retry attempts
// baseDelay: initial delay between retries
func (s *SocketModeConnection) sendACKWithRetry(envelopeID string, maxRetries int, baseDelay time.Duration) error {
	ack := map[string]any{
		"envelope_id": envelopeID,
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: delay = baseDelay * 2^(attempt-1)
			delay := baseDelay * time.Duration(1<<(attempt-1))
			s.logger.Debug("Retrying ACK send", "envelope_id", envelopeID, "attempt", attempt, "delay", delay)
			time.Sleep(delay)
		}

		if err := s.Send(ack); err != nil {
			lastErr = err
			s.logger.Warn("ACK send attempt failed", "envelope_id", envelopeID, "attempt", attempt+1, "maxRetries", maxRetries+1, "error", err)
			continue
		}
		return nil // Success
	}

	return fmt.Errorf("ACK send failed after %d attempts: %w", maxRetries+1, lastErr)
}

// sendPong sends a pong response to keep the connection alive
func (s *SocketModeConnection) sendPong() error {
	pong := map[string]string{
		"type": "pong",
	}

	data, err := json.Marshal(pong)
	if err != nil {
		return err
	}

	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()

	if conn != nil {
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	return nil
}

// Send sends a message over the WebSocket connection
func (s *SocketModeConnection) Send(data map[string]any) error {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	msg, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return conn.WriteMessage(websocket.TextMessage, msg)
}
