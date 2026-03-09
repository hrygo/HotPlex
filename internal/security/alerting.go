package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// ========================================
// Alert Event Types (mirrored from telemetry for self-containment)
// ========================================

// ThreatDetectionEvent contains details about an AI Guard threat detection.
type ThreatDetectionEvent struct {
	InputType string
	Category  string
	Score     float64
	Blocked   bool
	Verdict   string
	SessionID string
	Details   string
}

// DangerDetectionEvent contains details about a detected dangerous operation.
type DangerDetectionEvent struct {
	Operation      string
	Reason         string
	PatternMatched string
	Level          int
	Category       string
	BypassAllowed  bool
	SessionID      string
	UserID         string
	WorkspaceID    string
}

// BypassAttemptEvent contains details about a security bypass attempt.
type BypassAttemptEvent struct {
	TargetRule  string
	Success     bool
	AttemptedBy string
	SessionID   string
}

// PermissionDeniedEvent contains details about a permission denial.
type PermissionDeniedEvent struct {
	Resource  string
	Operation string
	Reason    string
	SessionID string
	UserID    string
}

// WorkspaceAccessEvent contains details about workspace access operations.
type WorkspaceAccessEvent struct {
	WorkspaceID string
	Operation   string
	Path        string
	Allowed     bool
	SessionID   string
	UserID      string
}

// LandlockEventType represents types of Landlock filesystem enforcement events.
type LandlockEventType string

const (
	LandlockEventAccessDenied  LandlockEventType = "access_denied"
	LandlockEventPathViolation LandlockEventType = "path_violation"
	LandlockEventRuleApplied   LandlockEventType = "rule_applied"
)

// LandlockEvent contains details about a Landlock enforcement event.
type LandlockEvent struct {
	EventType   LandlockEventType
	Operation   string
	Path        string
	Allowed     bool
	WorkspaceID string
	AccessMask  []string
}

// ========================================
// Alert Configuration
// ========================================

// AlertSeverity represents the severity level of an alert.
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityEmergency AlertSeverity = "emergency"
)

// AlertCategory represents the category of an alert.
type AlertCategory string

const (
	AlertCategoryThreatDetection  AlertCategory = "threat_detection"
	AlertCategoryDangerBlock      AlertCategory = "danger_block"
	AlertCategoryBypassAttempt    AlertCategory = "bypass_attempt"
	AlertCategoryAnomaly          AlertCategory = "anomaly"
	AlertCategoryPermission       AlertCategory = "permission"
	AlertCategoryWorkspace       AlertCategory = "workspace"
	AlertCategoryLandlock        AlertCategory = "landlock"
)

// Alert represents a security alert.
type Alert struct {
	ID          string                 `json:"id"`
	Severity    AlertSeverity          `json:"severity"`
	Category    AlertCategory          `json:"category"`
	Title       string                 `json:"title"`
	Message     string                 `json:"message"`
	Timestamp   time.Time              `json:"timestamp"`
	Source      string                 `json:"source"`
	SessionID   string                 `json:"session_id,omitempty"`
	UserID      string                 `json:"user_id,omitempty"`
	WorkspaceID string                 `json:"workspace_id,omitempty"`
	Metadata    map[string]any         `json:"metadata,omitempty"`
	Resolved    bool                   `json:"resolved"`
	ResolvedAt  *time.Time             `json:"resolved_at,omitempty"`
}

// AlertConfig holds configuration for the alerting system.
type AlertConfig struct {
	// WebhookURL is the URL to send alerts to.
	WebhookURL string

	// WebhookSecret is the secret for webhook authentication.
	WebhookSecret string

	// Enabled enables or disables alerting.
	Enabled bool

	// MinSeverity is the minimum severity level to trigger alerts.
	MinSeverity AlertSeverity

	// RateLimitDuration is the minimum time between repeated alerts of the same type.
	RateLimitDuration time.Duration

	// BufferSize is the size of the alert buffer.
	BufferSize int

	// Workers is the number of alert workers.
	Workers int

	// Logger is the logger instance.
	Logger *slog.Logger
}

// DefaultAlertConfig returns a default alert configuration.
func DefaultAlertConfig() AlertConfig {
	return AlertConfig{
		Enabled:          false,
		MinSeverity:     AlertSeverityWarning,
		RateLimitDuration: 5 * time.Minute,
		BufferSize:      100,
		Workers:         2,
		Logger:          slog.Default(),
	}
}

// ========================================
// Alerting Engine
// ========================================

// AlertingEngine manages security alerts and notifications.
type AlertingEngine struct {
	config  AlertConfig
	logger  *slog.Logger
	buffer  chan *Alert
	workers []*alertWorker
	mu      sync.RWMutex
	history map[string]*Alert // Keyed by alert fingerprint
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// alertWorker processes alerts from the buffer.
type alertWorker struct {
	id      int
	engine  *AlertingEngine
	buffer  chan *Alert
	stopCh  chan struct{}
}

// NewAlertingEngine creates a new alerting engine.
func NewAlertingEngine(config AlertConfig) *AlertingEngine {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	engine := &AlertingEngine{
		config:  config,
		logger:  config.Logger,
		buffer:  make(chan *Alert, config.BufferSize),
		history: make(map[string]*Alert),
		stopCh:  make(chan struct{}),
	}

	// Create workers
	for i := 0; i < config.Workers; i++ {
		engine.workers = append(engine.workers, &alertWorker{
			id:     i,
			engine: engine,
			buffer: make(chan *Alert, config.BufferSize/config.Workers),
			stopCh: make(chan struct{}),
		})
	}

	return engine
}

// Start starts the alerting engine workers.
func (e *AlertingEngine) Start(ctx context.Context) {
	if !e.config.Enabled {
		e.logger.Info("Alerting engine disabled")
		return
	}

	e.logger.Info("Starting alerting engine", "workers", e.config.Workers)

	// Start workers
	for _, w := range e.workers {
		e.wg.Add(1)
		go w.run(ctx)
	}

	// Start buffer pump
	e.wg.Add(1)
	go e.runPump(ctx)
}

// Stop stops the alerting engine.
func (e *AlertingEngine) Stop() {
	e.logger.Info("Stopping alerting engine")

	close(e.stopCh)
	e.wg.Wait()

	e.logger.Info("Alerting engine stopped")
}

// runPump pumps alerts from main buffer to worker buffers.
func (e *AlertingEngine) runPump(ctx context.Context) {
	defer e.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case alert := <-e.buffer:
			// Distribute to workers
			idx := int(hashFingerprint(alert.fingerprint())) % len(e.workers)
			select {
			case e.workers[idx].buffer <- alert:
			default:
				e.logger.Warn("Alert buffer full, dropping alert", "alert_id", alert.ID)
			}
		}
	}
}

// run processes alerts from the worker buffer.
func (w *alertWorker) run(ctx context.Context) {
	defer w.engine.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case alert := <-w.buffer:
			w.processAlert(alert)
		}
	}
}

// processAlert processes a single alert.
func (w *alertWorker) processAlert(alert *Alert) {
	w.engine.mu.Lock()
	defer w.engine.mu.Unlock()

	// Check rate limiting
	fingerprint := alert.fingerprint()
	if existing, ok := w.engine.history[fingerprint]; ok {
		if time.Since(existing.Timestamp) < w.engine.config.RateLimitDuration {
			w.engine.logger.Debug("Alert rate limited", "alert_id", alert.ID, "fingerprint", fingerprint)
			return
		}
	}

	// Send webhook notification
	if w.engine.config.WebhookURL != "" {
		if err := w.sendWebhook(alert); err != nil {
			w.engine.logger.Error("Failed to send alert webhook", "alert_id", alert.ID, "error", err)
		}
	}

	// Store in history
	w.engine.history[fingerprint] = alert
}

// sendWebhook sends an alert to the configured webhook URL.
func (w *alertWorker) sendWebhook(alert *Alert) error {
	body, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, w.engine.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if w.engine.config.WebhookSecret != "" {
		req.Header.Set("X-Webhook-Secret", w.engine.config.WebhookSecret)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// SendAlert sends an alert to the alerting engine.
func (e *AlertingEngine) SendAlert(alert *Alert) {
	if !e.config.Enabled {
		return
	}

	// Check minimum severity
	if !alert.meetsMinSeverity(e.config.MinSeverity) {
		return
	}

	alert.Timestamp = time.Now()
	if alert.ID == "" {
		alert.ID = fmt.Sprintf("alert-%d-%s", alert.Timestamp.UnixMilli(), generateAlertID())
	}

	select {
	case e.buffer <- alert:
	default:
		e.logger.Warn("Alert buffer full, dropping alert", "alert_id", alert.ID)
	}
}

// ========================================
// Alert Methods
// ========================================

// fingerprint returns a unique identifier for rate limiting.
func (a *Alert) fingerprint() string {
	return fmt.Sprintf("%s:%s:%s", a.Category, a.Title, a.Source)
}

// meetsMinSeverity returns true if the alert meets the minimum severity.
func (a *Alert) meetsMinSeverity(min AlertSeverity) bool {
	severityOrder := map[AlertSeverity]int{
		AlertSeverityInfo:      0,
		AlertSeverityWarning:  1,
		AlertSeverityCritical: 2,
		AlertSeverityEmergency: 3,
	}

	aSev, aOk := severityOrder[a.Severity]
	mSev, mOk := severityOrder[min]
	if !aOk || !mOk {
		return true
	}

	return aSev >= mSev
}

// Resolve marks an alert as resolved.
func (a *Alert) Resolve() {
	now := time.Now()
	a.Resolved = true
	a.ResolvedAt = &now
}

// ========================================
// Alert Factory Methods
// ========================================

// NewThreatDetectedAlert creates a new threat detection alert.
func NewThreatDetectedAlert(detection *ThreatDetectionEvent) *Alert {
	severity := AlertSeverityWarning
	if detection.Score > 0.8 {
		severity = AlertSeverityCritical
	}

	return &Alert{
		Severity: severity,
		Category: AlertCategoryThreatDetection,
		Title:    "Threat Detected",
		Message:  fmt.Sprintf("AI Guard detected potential threat: %s (score: %.2f)", detection.Category, detection.Score),
		Source:   "ai_guard",
		SessionID: detection.SessionID,
		Metadata: map[string]any{
			"input_type": detection.InputType,
			"category":  detection.Category,
			"score":      detection.Score,
			"blocked":    detection.Blocked,
			"verdict":    detection.Verdict,
		},
	}
}

// NewDangerBlockAlert creates a new danger block alert.
func NewDangerBlockAlert(detection *DangerDetectionEvent) *Alert {
	severity := AlertSeverityInfo
	switch detection.Level {
	case 0: // Critical
		severity = AlertSeverityCritical
	case 1: // High
		severity = AlertSeverityWarning
	}

	return &Alert{
		Severity:    severity,
		Category:    AlertCategoryDangerBlock,
		Title:       "Dangerous Command Blocked",
		Message:     fmt.Sprintf("Blocked dangerous operation: %s - %s", detection.Operation, detection.Reason),
		Source:      "detector",
		SessionID:   detection.SessionID,
		UserID:      detection.UserID,
		WorkspaceID: detection.WorkspaceID,
		Metadata: map[string]any{
			"operation":       detection.Operation,
			"reason":          detection.Reason,
			"pattern_matched":  detection.PatternMatched,
			"level":           detection.Level,
			"category":        detection.Category,
			"bypass_allowed":  detection.BypassAllowed,
		},
	}
}

// NewBypassAttemptAlert creates a new bypass attempt alert.
func NewBypassAttemptAlert(event *BypassAttemptEvent) *Alert {
	severity := AlertSeverityWarning
	if event.Success {
		severity = AlertSeverityCritical
	}

	return &Alert{
		Severity:  severity,
		Category:  AlertCategoryBypassAttempt,
		Title:     "Security Bypass Attempt",
		Message:   fmt.Sprintf("Bypass attempt on rule '%s' by %s", event.TargetRule, event.AttemptedBy),
		Source:    "detector",
		SessionID: event.SessionID,
		Metadata: map[string]any{
			"target_rule": event.TargetRule,
			"success":     event.Success,
			"attempted_by": event.AttemptedBy,
		},
	}
}

// NewAnomalyAlert creates a new anomaly detection alert.
func NewAnomalyAlert(anomalyType, message string, metadata map[string]any) *Alert {
	return &Alert{
		Severity: AlertSeverityWarning,
		Category: AlertCategoryAnomaly,
		Title:    "Anomaly Detected",
		Message:  message,
		Source:   "anomaly_detector",
		Metadata: metadata,
	}
}

// NewPermissionDeniedAlert creates a new permission denied alert.
func NewPermissionDeniedAlert(event *PermissionDeniedEvent) *Alert {
	return &Alert{
		Severity:  AlertSeverityInfo,
		Category:  AlertCategoryPermission,
		Title:     "Permission Denied",
		Message:   fmt.Sprintf("Permission denied: %s - %s", event.Resource, event.Reason),
		Source:    "permission_manager",
		SessionID: event.SessionID,
		UserID:    event.UserID,
		Metadata: map[string]any{
			"resource":  event.Resource,
			"operation": event.Operation,
			"reason":    event.Reason,
		},
	}
}

// NewWorkspaceAccessDeniedAlert creates a new workspace access denied alert.
func NewWorkspaceAccessDeniedAlert(event *WorkspaceAccessEvent) *Alert {
	return &Alert{
		Severity:    AlertSeverityWarning,
		Category:    AlertCategoryWorkspace,
		Title:       "Workspace Access Denied",
		Message:     fmt.Sprintf("Workspace access denied: %s - %s", event.WorkspaceID, event.Operation),
		Source:      "workspace_isolation",
		SessionID:   event.SessionID,
		UserID:      event.UserID,
		WorkspaceID: event.WorkspaceID,
		Metadata: map[string]any{
			"workspace_id": event.WorkspaceID,
			"operation":    event.Operation,
			"path":         event.Path,
		},
	}
}

// NewLandlockViolationAlert creates a new Landlock violation alert.
func NewLandlockViolationAlert(event *LandlockEvent) *Alert {
	severity := AlertSeverityInfo
	if event.EventType == "access_denied" {
		severity = AlertSeverityWarning
	}

	return &Alert{
		Severity:    severity,
		Category:    AlertCategoryLandlock,
		Title:       "Landlock Violation",
		Message:     fmt.Sprintf("Landlock %s: %s on %s", event.EventType, event.Operation, event.Path),
		Source:      "landlock_enforcer",
		WorkspaceID: event.WorkspaceID,
		Metadata: map[string]any{
			"event_type": event.EventType,
			"operation":  event.Operation,
			"path":       event.Path,
			"allowed":    event.Allowed,
		},
	}
}

// ========================================
// Alert Query Methods
// ========================================

// GetAlerts returns alerts matching the given criteria.
func (e *AlertingEngine) GetAlerts(filter AlertFilter) []*Alert {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []*Alert
	for _, alert := range e.history {
		if filter.Category != "" && alert.Category != filter.Category {
			continue
		}
		if filter.Severity != "" && alert.Severity != filter.Severity {
			continue
		}
		if filter.Resolved && !alert.Resolved {
			continue
		}
		result = append(result, alert)
	}

	return result
}

// GetActiveAlerts returns all unresolved alerts.
func (e *AlertingEngine) GetActiveAlerts() []*Alert {
	return e.GetAlerts(AlertFilter{Resolved: false})
}

// GetCriticalAlerts returns all critical alerts.
func (e *AlertingEngine) GetCriticalAlerts() []*Alert {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []*Alert
	for _, alert := range e.history {
		if alert.Severity == AlertSeverityCritical && !alert.Resolved {
			result = append(result, alert)
		}
	}
	return result
}

// AlertFilter contains filter criteria for querying alerts.
type AlertFilter struct {
	Category  AlertCategory
	Severity  AlertSeverity
	Resolved  bool
	StartTime time.Time
	EndTime   time.Time
}

// ========================================
// Helper Functions
// ========================================

func hashFingerprint(s string) uint64 {
	// Simple hash for fingerprinting
	var hash uint64
	for i, c := range s {
		hash = hash*31 + uint64(c)*uint64(i)
	}
	return hash
}

func generateAlertID() string {
	// Generate a short random ID
	return fmt.Sprintf("%x", time.Now().UnixNano()&0xFFFFFF)
}
