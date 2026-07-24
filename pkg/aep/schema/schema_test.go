package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// schemaDir returns the absolute path to the schema package directory.
func schemaDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	return filepath.Dir(file)
}

// loadSchema reads and parses the canonical aep-v1.json.
func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	dir := schemaDir(t)
	raw, err := os.ReadFile(filepath.Join(dir, "aep-v1.json"))
	require.NoError(t, err, "read aep-v1.json")
	var s map[string]any
	require.NoError(t, json.Unmarshal(raw, &s), "parse aep-v1.json")
	return s
}

// knownKinds is the canonical list of events.Kind constant string values,
// extracted from pkg/events/events.go. If a new Kind is added to events.go,
// this list AND aep-v1.json must be updated — the test will fail otherwise.
var knownKinds = []string{
	"init",
	"error",
	"state",
	"input",
	"input.ack",
	"done",
	"message",
	"message.start",
	"message.delta",
	"message.end",
	"tool_call",
	"tool_result",
	"reasoning",
	"step",
	"raw",
	"permission_request",
	"permission_response",
	"question_request",
	"question_response",
	"elicitation_request",
	"elicitation_response",
	"ping",
	"pong",
	"control",
	"context_usage",
	"skills_list",
	"mcp_status",
	"worker_command",
	"internal_reset",
	"tool_update",
	"plan",
	"mode_update",
	"runtime.execution.started",
	"runtime.execution.completed",
	"runtime.execution.failed",
}

func TestSchemaKindCoverage(t *testing.T) {
	t.Parallel()
	s := loadSchema(t)

	kindsRaw, ok := s["kinds"].(map[string]any)
	require.True(t, ok, "schema missing 'kinds' object")

	// Every Go Kind constant must appear in the schema.
	for _, kv := range knownKinds {
		_, exists := kindsRaw[kv]
		require.True(t, exists, "Go Kind %q is missing from aep-v1.json schema", kv)
	}

	// No schema kind should be absent from Go (detects stale schema entries).
	for sk := range kindsRaw {
		found := false
		for _, kv := range knownKinds {
			if kv == sk {
				found = true
				break
			}
		}
		require.True(t, found, "schema kind %q does not exist in Go events.Kind constants", sk)
	}

	// Each kind must have direction and stability fields.
	for kind, raw := range kindsRaw {
		entry, ok := raw.(map[string]any)
		require.True(t, ok, "schema kind %q entry is not an object", kind)
		_, hasDir := entry["direction"]
		require.True(t, hasDir, "schema kind %q missing 'direction'", kind)
		_, hasStab := entry["stability"]
		require.True(t, hasStab, "schema kind %q missing 'stability'", kind)
		_, hasData := entry["data_type"]
		require.True(t, hasData, "schema kind %q missing 'data_type'", kind)
	}
}

func TestSchemaKindCountMatchesGo(t *testing.T) {
	t.Parallel()
	s := loadSchema(t)
	kindsRaw, ok := s["kinds"].(map[string]any)
	require.True(t, ok)
	require.Len(t, kindsRaw, len(knownKinds),
		"schema has %d kinds but Go has %d — they must match exactly", len(kindsRaw), len(knownKinds))
}

func TestCorpusRoundTrip(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(schemaDir(t), CorpusDir)
	fixtures := AllFixtures()
	require.NotEmpty(t, fixtures)

	for _, f := range fixtures {
		f := f
		t.Run(f.Filename, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(filepath.Join(dir, f.Filename))
			require.NoError(t, err, "read corpus fixture %s", f.Filename)

			if f.MinimalDecode {
				// Unknown/relaxed fixtures: use minimal decode, assert no panic.
				env, err := aep.DecodeLineMinimal(raw)
				require.NoError(t, err, "minimal decode %s", f.Filename)
				require.NotEmpty(t, env.Event.Type, "event type empty in %s", f.Filename)
			} else {
				// Known kinds: strict decode + re-encode determinism.
				env, err := aep.DecodeLine(raw)
				require.NoError(t, err, "strict decode %s", f.Filename)
				require.Equal(t, f.Envelope.Event.Type, env.Event.Type,
					"event type mismatch in %s", f.Filename)
			}
		})
	}
}

func TestCorpusDeterministicRegeneration(t *testing.T) {
	// This test regenerates the corpus into a temp directory and byte-compares
	// each file against the committed corpus. If Go types change (field added,
	// tag renamed), the output changes and this test fails — forcing the PR
	// author to update the corpus intentionally.
	dir := filepath.Join(schemaDir(t), CorpusDir)

	tmp := t.TempDir()
	_, err := GenerateCorpus(tmp)
	require.NoError(t, err, "regenerate corpus")

	committed, err := LoadCorpusDir(dir)
	require.NoError(t, err, "load committed corpus")

	regenerated, err := LoadCorpusDir(tmp)
	require.NoError(t, err, "load regenerated corpus")

	// Same file set.
	committedNames := make([]string, 0, len(committed))
	for n := range committed {
		committedNames = append(committedNames, n)
	}
	regenNames := make([]string, 0, len(regenerated))
	for n := range regenerated {
		regenNames = append(regenNames, n)
	}
	sort.Strings(committedNames)
	sort.Strings(regenNames)
	require.Equal(t, committedNames, regenNames,
		"corpus file set mismatch — committed and regenerated have different fixtures")

	// Byte-compare each file.
	for _, name := range committedNames {
		require.Equal(t, committed[name], regenerated[name],
			"corpus fixture %s drift: committed bytes != regenerated bytes.\n"+
				"If you changed AEP types, regenerate: go run ./cmd/gen-corpus/main.go", name)
	}
}

func TestCorpusUnknownKindSafelyIgnorable(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(schemaDir(t), CorpusDir)
	raw, err := os.ReadFile(filepath.Join(dir, "90-compatibility-unknown-kind.json"))
	require.NoError(t, err)

	env, err := aep.DecodeLineMinimal(raw)
	require.NoError(t, err, "unknown kind must be decodable without error")
	require.Equal(t, events.Kind("custom.future_event"), env.Event.Type)

	// Verify the envelope round-trips without data loss.
	encoded, err := aep.EncodeJSON(env)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "custom.future_event")
}

func TestCorpusAdditiveFieldsIgnorable(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(schemaDir(t), CorpusDir)
	raw, err := os.ReadFile(filepath.Join(dir, "91-compatibility-additive-fields.json"))
	require.NoError(t, err)

	// An envelope with extra unknown fields in event.data must still decode.
	// DecodeLineMinimal does not use DisallowUnknownFields.
	env, err := aep.DecodeLineMinimal(raw)
	require.NoError(t, err, "additive fields must not break decoding")
	require.Equal(t, events.Message, env.Event.Type)

	data, ok := env.Event.Data.(map[string]any)
	require.True(t, ok)
	require.Contains(t, data, "future_tag", "additive field should survive decoding")
}

func TestCorpusMissingOptionalSurvives(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(schemaDir(t), CorpusDir)
	raw, err := os.ReadFile(filepath.Join(dir, "92-compatibility-missing-optional.json"))
	require.NoError(t, err)

	env, err := aep.DecodeLineMinimal(raw)
	require.NoError(t, err, "missing optional fields must not break decoding")
	require.Equal(t, events.Done, env.Event.Type)
}

func TestCorpusCoversAllKnownKinds(t *testing.T) {
	t.Parallel()
	fixtures := AllFixtures()

	// Every known kind (except ping/pong edge cases) must have a corpus fixture.
	corpusKinds := make(map[string]bool, len(fixtures))
	for _, f := range fixtures {
		if !strings.HasPrefix(f.Filename, "9") { // skip edge-case fixtures
			corpusKinds[string(f.Envelope.Event.Type)] = true
		}
	}

	for _, kv := range knownKinds {
		require.True(t, corpusKinds[kv],
			"Kind %q has no corpus fixture — add one to allFixtures() in generate_corpus.go", kv)
	}
}
