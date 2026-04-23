package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// Sensitive URL query parameter keys redacted in logs: access_key, conn_id,
// device_id, fpid, service_id, ticket. Keep in sync with sensitiveParamRe.

// sdkLogFilter rewrites Feishu SDK log messages to be more readable and
// removes connection noise that carries no actionable information.
func sdkLogFilter(msg string) string {
	// Silent noisy routine messages (ping/pong/heartbeat cycles).
	for _, sub := range sdkDebugSilent {
		if strings.Contains(msg, sub) {
			return ""
		}
	}
	// Silent verbose reconnection chatter.
	for _, prefix := range sdkReconnectSilent {
		if strings.HasPrefix(msg, prefix) {
			return ""
		}
	}
	// Improve "receive message failed" error readability.
	if strings.Contains(msg, "receive message failed") {
		// Strip the raw TCP errno from the user-facing log.
		msg = strings.Split(msg, ", err:")[0] + " (connection reset by peer)"
	}
	return msg
}

// sensitiveParamRe matches sensitive=VALUE patterns and captures the key=
// prefix for replacement. Values are terminated by &, whitespace, [, or ].
var sensitiveParamRe = regexp.MustCompile(
	`((?:access_key|conn_id|device_id|fpid|service_id|ticket)=)[^&\s\[\]]+`,
)

// redactURL replaces sensitive query parameter values with "***" in any string.
// Works on full URLs, embedded URLs (e.g. "connected to wss://..."), and
// standalone parameter patterns like [conn_id=...].
func redactURL(s string) string {
	return sensitiveParamRe.ReplaceAllString(s, "${1}***")
}

// sdkDebugSilent lists Feishu SDK Debug log message substrings that are
// silenced during normal operation (every ~2 min heartbeat cycle). These are
// routine ping/pong keep-alive messages that don't carry actionable info.
// Failures still surface via Warn/Error level.
var sdkDebugSilent = []string{
	"ping success",
	"receive pong",
}

// sdkReconnectSilent removes verbose reconnection-related log prefixes
// that carry no actionable information and are part of the SDK's automatic
// reconnect loop. "connected to wss://" is NOT silenced so that reconnection
// success is observable in logs.
var sdkReconnectSilent = []string{
	"disconnected to wss://",
	"trying to reconnect:",
}

// SlogLogger implements larkcore.Logger, wrapping slog.Logger.
// This ensures all Feishu SDK logs use the same JSON format and level
// as the application logs, with sensitive URL params redacted.
// Normal heartbeat messages (ping success, receive pong) are silenced
// to reduce log noise — failures still surface via Warn/Error level.
type SlogLogger struct{ *slog.Logger }

func (s SlogLogger) Debug(_ context.Context, args ...any) {
	msg := sdkLogFilter(redactURL(fmt.Sprint(args...)))
	if msg == "" {
		return
	}
	s.Logger.Log(context.Background(), slog.LevelDebug, msg)
}
func (s SlogLogger) Info(_ context.Context, args ...any) {
	msg := sdkLogFilter(redactURL(fmt.Sprint(args...)))
	if msg == "" {
		return
	}
	s.Logger.Log(context.Background(), slog.LevelInfo, msg)
}
func (s SlogLogger) Warn(_ context.Context, args ...any) {
	msg := sdkLogFilter(redactURL(fmt.Sprint(args...)))
	if msg == "" {
		return
	}
	s.Logger.Log(context.Background(), slog.LevelWarn, msg)
}
func (s SlogLogger) Error(_ context.Context, args ...any) {
	msg := sdkLogFilter(redactURL(fmt.Sprint(args...)))
	if msg == "" {
		return
	}
	s.Logger.Log(context.Background(), slog.LevelError, msg)
}
