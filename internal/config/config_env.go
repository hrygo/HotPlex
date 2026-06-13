package config

import (
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// aggregateNumberedEnv appends values from environment variables like PREFIX_1, PREFIX_2...
// to the existing slice, deduplicating them.  Supports project's secret rotation convention.
func aggregateNumberedEnv(existing []string, prefix string) []string {
	seen := make(map[string]bool)
	for _, v := range existing {
		seen[v] = true
	}

	for i := 1; ; i++ {
		key := fmt.Sprintf("%s%d", prefix, i)
		val := os.Getenv(key)
		if val == "" {
			break
		}
		if !seen[val] {
			existing = append(existing, val)
			seen[val] = true
		}
	}
	return existing
}

// resolveAPIKeyUsers builds a runtime map of expanded API key value → userID.
// Input map keys are resolved as env var names first, then as literal keys.
// Returns nil when no valid mappings are found (preserves "api_user" default).
func resolveAPIKeyUsers(raw map[string]string, expandedKeys []string) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	keySet := make(map[string]struct{}, len(expandedKeys))
	for _, k := range expandedKeys {
		keySet[k] = struct{}{}
	}
	result := make(map[string]string, len(raw))
	for mapKey, userID := range raw {
		if envVal := os.Getenv(mapKey); envVal != "" {
			result[envVal] = userID
			continue
		}
		if _, isKey := keySet[mapKey]; isKey {
			result[mapKey] = userID
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// applyMessagingEnv overrides messaging config from environment variables.
// This is needed because Viper's AutomaticEnv cannot map nested keys
// unless the viper instance has seen them from a config file or SetDefault.
func applyMessagingEnv(cfg *Config) {
	// Slack
	applyPlatformEnv(&cfg.Messaging.Slack,
		[]envMapping{
			{"HOTPLEX_MESSAGING_SLACK_BOT_TOKEN", "BotToken"},
			{"HOTPLEX_MESSAGING_SLACK_APP_TOKEN", "AppToken"},
			{"HOTPLEX_MESSAGING_SLACK_WORKER_TYPE", "WorkerType"},
			{"HOTPLEX_MESSAGING_SLACK_WORK_DIR", "WorkDir"},
			{"HOTPLEX_MESSAGING_SLACK_DM_POLICY", "DMPolicy"},
			{"HOTPLEX_MESSAGING_SLACK_GROUP_POLICY", "GroupPolicy"},
		},
		[]envMapping{
			{"HOTPLEX_MESSAGING_SLACK_ENABLED", "Enabled"},
			{"HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION", "RequireMention"},
		},
		[]envMapping{
			{"HOTPLEX_MESSAGING_SLACK_ALLOW_FROM", "AllowFrom"},
			{"HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM", "AllowDMFrom"},
			{"HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM", "AllowGroupFrom"},
		},
	)

	// Feishu
	applyPlatformEnv(&cfg.Messaging.Feishu,
		[]envMapping{
			{"HOTPLEX_MESSAGING_FEISHU_APP_ID", "AppID"},
			{"HOTPLEX_MESSAGING_FEISHU_APP_SECRET", "AppSecret"},
			{"HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE", "WorkerType"},
			{"HOTPLEX_MESSAGING_FEISHU_WORK_DIR", "WorkDir"},
			{"HOTPLEX_MESSAGING_FEISHU_DM_POLICY", "DMPolicy"},
			{"HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY", "GroupPolicy"},
		},
		[]envMapping{
			{"HOTPLEX_MESSAGING_FEISHU_ENABLED", "Enabled"},
			{"HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION", "RequireMention"},
		},
		[]envMapping{
			{"HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM", "AllowFrom"},
			{"HOTPLEX_MESSAGING_FEISHU_ALLOW_DM_FROM", "AllowDMFrom"},
			{"HOTPLEX_MESSAGING_FEISHU_ALLOW_GROUP_FROM", "AllowGroupFrom"},
		},
	)

	// Yuanxin
	applyPlatformEnv(&cfg.Messaging.Yuanxin,
		[]envMapping{
			{"HOTPLEX_MESSAGING_YUANXIN_APP_ID", "AppID"},
			{"HOTPLEX_MESSAGING_YUANXIN_PULSAR_URL", "PulsarURL"},
			{"HOTPLEX_MESSAGING_YUANXIN_TENANT", "Tenant"},
			{"HOTPLEX_MESSAGING_YUANXIN_NAMESPACE", "Namespace"},
			{"HOTPLEX_MESSAGING_YUANXIN_PRODUCER_TOPIC", "ProducerTopic"},
			{"HOTPLEX_MESSAGING_YUANXIN_WORKER_TYPE", "WorkerType"},
			{"HOTPLEX_MESSAGING_YUANXIN_WORK_DIR", "WorkDir"},
			{"HOTPLEX_MESSAGING_YUANXIN_DM_POLICY", "DMPolicy"},
			{"HOTPLEX_MESSAGING_YUANXIN_GROUP_POLICY", "GroupPolicy"},
		},
		[]envMapping{
			{"HOTPLEX_MESSAGING_YUANXIN_ENABLED", "Enabled"},
		},
		[]envMapping{
			{"HOTPLEX_MESSAGING_YUANXIN_ALLOW_FROM", "AllowFrom"},
			{"HOTPLEX_MESSAGING_YUANXIN_ALLOW_DM_FROM", "AllowDMFrom"},
			{"HOTPLEX_MESSAGING_YUANXIN_ALLOW_GROUP_FROM", "AllowGroupFrom"},
		},
	)

	// Global messaging env vars.
	if v := os.Getenv("HOTPLEX_MESSAGING_TURN_SUMMARY_ENABLED"); v != "" {
		cfg.Messaging.TurnSummaryEnabled = strings.EqualFold(v, "true")
	}

	// Messaging-level shared defaults (propagated to platforms by propagateMessagingDefaults).
	msgStrs := []envMapping{
		{"HOTPLEX_MESSAGING_WORKER_TYPE", "WorkerType"},
		{"HOTPLEX_MESSAGING_STT_PROVIDER", "Provider"},
		{"HOTPLEX_MESSAGING_STT_LOCAL_CMD", "LocalCmd"},
		{"HOTPLEX_MESSAGING_TTS_PROVIDER", "TTSProvider"},
		{"HOTPLEX_MESSAGING_TTS_VOICE", "Voice"},
		{"HOTPLEX_MESSAGING_TTS_MOSS_MODEL_DIR", "MossModelDir"},
		{"HOTPLEX_MESSAGING_TTS_MOSS_VOICE", "MossVoice"},
	}
	for _, m := range msgStrs {
		if v := os.Getenv(m.env); v != "" {
			if err := setField(&cfg.Messaging, m.field, v); err != nil {
				slog.Warn("config: env mapping skipped",
					"env", m.env,
					"field", m.field,
					"target", "config.Messaging",
					"error", err,
				)
			}
		}
	}
	// Int fields.
	if v := os.Getenv("HOTPLEX_MESSAGING_TTS_MAX_CHARS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Messaging.MaxChars = n
		}
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_TTS_MOSS_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Messaging.MossPort = n
		}
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_TTS_MOSS_CPU_THREADS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Messaging.MossCpuThreads = n
		}
	}
	// Duration fields.
	if v := os.Getenv("HOTPLEX_MESSAGING_STT_LOCAL_IDLE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Messaging.LocalIdleTTL = d
		}
	}
	if v := os.Getenv("HOTPLEX_MESSAGING_TTS_MOSS_IDLE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Messaging.MossIdleTimeout = d
		}
	}
	// Bool fields.
	if v := os.Getenv("HOTPLEX_MESSAGING_TTS_ENABLED"); v != "" {
		cfg.Messaging.TTSEnabled = strings.EqualFold(v, "true")
	}
}

// envMapping maps an environment variable to a struct field name.
type envMapping struct{ env, field string }

// applyPlatformEnv applies string, bool, and slice env-var mappings to a target struct.
func applyPlatformEnv(target any, strs, bools, slices []envMapping) {
	for _, m := range strs {
		if v := os.Getenv(m.env); v != "" {
			if err := setField(target, m.field, v); err != nil {
				slog.Warn("config: env mapping skipped",
					"env", m.env,
					"field", m.field,
					"target", fmt.Sprintf("%T", target),
					"error", err,
				)
			}
		}
	}
	for _, m := range bools {
		if v := os.Getenv(m.env); v != "" {
			if err := setBoolField(target, m.field, strings.EqualFold(v, "true")); err != nil {
				slog.Warn("config: env mapping skipped",
					"env", m.env,
					"field", m.field,
					"target", fmt.Sprintf("%T", target),
					"error", err,
				)
			}
		}
	}
	for _, m := range slices {
		if v := os.Getenv(m.env); v != "" {
			if err := setSliceField(target, m.field, v); err != nil {
				slog.Warn("config: env mapping skipped",
					"env", m.env,
					"field", m.field,
					"target", fmt.Sprintf("%T", target),
					"error", err,
				)
			}
		}
	}
}

// setField sets a string field on a struct by name using reflection.
func setField(target any, field, value string) error {
	v := reflect.ValueOf(target).Elem()
	f := v.FieldByName(field)
	if !f.IsValid() {
		return fmt.Errorf("config: setField: no such field %q on %T", field, target)
	}
	f.SetString(value)
	return nil
}

// setBoolField sets a bool field on a struct by name using reflection.
func setBoolField(target any, field string, value bool) error {
	v := reflect.ValueOf(target).Elem()
	f := v.FieldByName(field)
	if !f.IsValid() {
		return fmt.Errorf("config: setBoolField: no such field %q on %T", field, target)
	}
	f.SetBool(value)
	return nil
}

// setSliceField sets a []string field on a struct by name using reflection.
func setSliceField(target any, field, value string) error {
	v := reflect.ValueOf(target).Elem()
	f := v.FieldByName(field)
	if !f.IsValid() {
		return fmt.Errorf("config: setSliceField: no such field %q on %T", field, target)
	}
	parts := strings.Split(value, ",")
	slice := make([]string, len(parts))
	for i, p := range parts {
		slice[i] = strings.TrimSpace(p)
	}
	f.Set(reflect.ValueOf(slice))
	return nil
}
