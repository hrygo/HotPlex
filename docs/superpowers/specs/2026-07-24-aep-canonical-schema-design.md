# AEP Canonical Schema & Cross-SDK Conformance Suite — #869

> **Issue**: #869 · **Status**: Draft · **Date**: 2026-07-24 · **Baseline**: v1.38.0

---

## 1. Problem

AEP types are maintained in four SDKs (Go gateway, TS, Python, Java) by hand.
Protocol changes compile in Go while an SDK silently drifts. Today's verified drift:

| Feature | Go | TS | Python | Java |
|---------|----|----|--------|------|
| 35 Kind constants | Yes | 29 (missing init, input.ack, runtime.*, internal_reset, control.stop) | No explicit registry | 32 (missing input.ack, runtime.*, internal_reset, control.stop) |
| InputAckData | Yes | No | No | No |
| RuntimeExecutionData | Yes | No | No | No |
| InternalResetData | Yes | No | No | No |
| ControlActionStop | Yes | No | Yes | No |

No mechanical check prevents this. A field rename, a dropped tag, or a new Kind
can land in Go without any SDK noticing.

## 2. Goal

One shared, machine-readable contract that every SDK validates against, plus a
CI gate that fails when Go types drift from the committed contract.

## 3. Design

### 3.1 Canonical Schema (`pkg/aep/schema/aep-v1.json`)

A single JSON file that is the **source of truth** for the wire contract. It contains:

- **Envelope**: required fields, types, JSON tags.
- **Kind registry**: every `events.Kind` constant string value, with direction (C->S / S->C / bidirectional) and a stability flag (`stable` / `additive`).
- **Kind -> Data mapping**: which Go struct backs each kind.
- **Metadata key registry**: standardized metadata keys (`trace_id`, `execution_id`, etc.).
- **Direction map**: client-to-server vs server-to-client.

This file is hand-maintained alongside `pkg/events/events.go` and committed to the
repo. It is NOT auto-generated from Go reflection because reflection-based JSON
Schema generation is fragile, produces noisy output, and obscures the human-readable
contract. Instead, the **Go test verifies consistency** (Section 3.3).

### 3.2 Golden Corpus (`pkg/aep/schema/corpus/`)

A directory of JSON fixture files, one per event kind, each containing a valid AEP
envelope. Plus edge-case fixtures:

- `00-init.json` through `NN-<kind>.json` — one per Kind.
- `compatibility-unknown-kind.json` — an envelope with an unknown `event.type` that
  must be safely ignorable by old clients.
- `compatibility-additive-fields.json` — extra unknown fields in `event.data` that
  must not break decoding (forward compatibility).
- `compatibility-missing-optional.json` — optional fields omitted.

Corpus naming convention: `NN-kind-name.json` (zero-padded sort order).

### 3.3 Go Schema-Diff & Corpus Tests (`pkg/aep/schema_test.go`)

Two test groups:

**Schema coverage test** — fails if Go and schema disagree:

1. Enumerate every `events.Kind` constant via reflection on the `Kind` type.
2. Assert every constant appears in `aep-v1.json` `kinds`.
3. Assert no schema kind is absent from Go (detects stale schema entries).
4. Assert the direction and stability flags are present for each kind.

**Corpus round-trip test** — fails if corpus fixtures are invalid:

1. Load every `.json` file in `corpus/`.
2. Decode via `aep.DecodeLine` (strict mode for known kinds).
3. For unknown-kind fixtures, decode via `aep.DecodeLineMinimal` and assert
   `event.type` is preserved but no panic occurs.
4. Re-encode via `aep.EncodeJSON` and assert the JSON is deterministic (stable key order).

### 3.4 SDK Conformance

Each SDK adds a test that loads the **same corpus directory** and validates every
fixture:

- **Go**: `pkg/aep/schema_test.go` (above).
- **TypeScript**: `examples/typescript-client/tests/conformance.test.ts` —
  load corpus JSON, parse with `Envelope<T>`, validate `event.type` matches.
- **Python**: `examples/python-client/tests/test_conformance.py` —
  load corpus JSON, decode, validate structure.
- **Java**: `examples/java-client/src/test/java/.../ConformanceTest.java` —
  load corpus JSON, deserialize with Jackson, validate `EventKind`.

SDKs that are missing a Kind found in the corpus will fail their conformance test,
making drift visible at CI time.

### 3.5 Schema-Diff Classifier

A Go test (`pkg/aep/schema_diff_test.go`) that classifies changes between the
committed schema and a freshly generated snapshot:

1. Build a snapshot of current Go Kind constants and their Data struct field
   lists (via reflection).
2. Compare to the committed `aep-v1.json`.
3. Classify each delta:
   - **Additive** (new Kind, new optional field with `omitempty`): safe, prints a reminder to update SDKs.
   - **Breaking** (removed Kind, removed field, changed JSON tag, changed required->optional without omitempty): fails CI.
4. The committed schema is the baseline; a dirty diff means the PR author either
   forgot to update it (breaking) or added types without updating it (additive reminder).

### 3.6 CI Integration

Add a new job to `.github/workflows/ci.yml`:

```yaml
  aep-conformance:
    name: AEP Schema & SDK Conformance
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: ./.github/actions/setup-go-webchat
        with:
          build-webchat: 'false'
      - run: go test ./pkg/aep/... -run Schema -count=1 -v
      - run: go test ./pkg/aep/... -run Corpus -count=1 -v
      - run: go test ./pkg/aep/... -run SchemaDiff -count=1 -v
      # SDK conformance
      - run: cd examples/typescript-client && npm ci && npx vitest run tests/conformance
      - run: cd examples/python-client && pip install -e . && pytest tests/test_conformance.py
      - uses: actions/setup-java@v4
        with: { java-version: '21', distribution: 'temurin' }
      - run: cd examples/java-client && ./gradlew test --tests '*Conformance*'
```

## 4. Acceptance Criteria Mapping

| AC | Implementation |
|----|----------------|
| One corpus consumed by Go/TS/Python/Java | Section 3.2 + 3.4 |
| Runtime events from #849 covered | Corpus fixtures for runtime.execution.* |
| Unknown additive kinds safely ignorable | compatibility-unknown-kind.json fixture + DecodeLineMinimal test |
| Field/tag drift fails CI | Schema coverage test (3.3) + Schema-diff (3.5) |
| Generation deterministic | Corpus re-encode comparison with stable key order |
| Protocol and SDK docs updated | aep-protocol.md + events.md + SDK README sections |

## 5. Non-Goals

- No AEP v2.
- No forced rewrite of all SDK implementations (SDKs add missing types only where
  needed to pass conformance; the corpus makes gaps visible).
- No runtime schema registry.
- No auto-generation of SDK code from the schema.

## 6. Scope for First Cut

- Canonical schema file with Kind registry + envelope structure.
- Corpus fixtures for all 35 Go Kind constants + 3 edge cases = 38 fixtures.
- Go schema coverage + corpus round-trip + schema-diff tests.
- SDK conformance tests (TS/Python/Java) — each SDK may need to add missing types.
- CI job.
- Docs: `docs/reference/aep-protocol.md` + `docs/reference/events.md` + SDK READMEs.

## 7. Risk

| Risk | Mitigation |
|------|-----------|
| SDK conformance tests require SDK fixes (adding missing types) | Fix is additive; each SDK gets the missing Kind constant + Data type. Keep PR scoped: schema + corpus + tests + minimal SDK fixes. |
| Schema-diff classifier false positives on cosmetic changes | Only compare Kind registry + field name + JSON tag + omitempty flag; ignore comments and ordering. |
| Java Gradle setup adds CI time | Java test is a lightweight Jackson round-trip; cache Gradle deps. |
| Python `pytest` version differences | Use stdlib `json` + `unittest` if pytest is unavailable in the SDK's CI path. |
