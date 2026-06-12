# Prompt Injection Mitigation for search-mcp-server

**Jira:** [ACM-32466](https://redhat.atlassian.net/browse/ACM-32466)
**Status:** Implemented ([PR #55](https://github.com/stolostron/search-mcp-server/pull/55))
**Plan date:** 2026-06-08
**Last updated:** 2026-06-08

---

## Problem

Resource metadata from Kubernetes managed clusters (annotations, status messages, labels,
ConfigMap data) flows into the LLM context window via the `find_resources` tool. An attacker
who can create resources on any managed cluster can craft adversarial metadata to manipulate
LLM behavior — causing data exfiltration, unintended tool invocations, or user deception.

### Vulnerable Fields (by risk)

| Field (index key) | Risk | Reason |
|---|---|---|
| `status` | **HIGH** | Arbitrary controller messages; rendered directly in Markdown list output |
| `annotation` values | **HIGH** | Arbitrary UTF-8 including multiline prompts (Search index key is `annotation`, singular) |
| Other JSONB string fields | **HIGH** | Unknown keys default to recursive string sanitization (covers ConfigMap-like content) |
| `label` values | **MEDIUM** | Constrained format but still injectable |
| Resource names | **LOW** | DNS-constrained `[a-z0-9-.]`; skipped via `PolicySkip` |
| Namespaces | **LOW** | Same DNS constraints; skipped via `PolicySkip` |

### Data Path (current implementation)

```
PostgreSQL JSONB (search.resources.data)
  └─> core.go: process*Mode() → dataMap (row[2])
        └─> sanitizer.SanitizeResourceDataMap(dataMap)   ← prompt injection defense
              ├─> PolicySkip: name, namespace, kind, cluster, created, _uid, _hubClusterName
              ├─> PolicySanitizeStrings: status, annotation, label (recursive)
              └─> default PolicySanitizeStrings: any other string field in dataMap

        └─> ResourceResult fields extracted from sanitized dataMap
              ├─> Status  → formatters.go Markdown table (list mode)
              ├─> Name / Namespace / Cluster / Age → Markdown table
              ├─> Labels  → on struct; not rendered in list Markdown today
              └─> Data    → full sanitized dataMap on ResourceResult

formatters.go:escapeMarkdown() → escapes Markdown syntax only (|, *, _, `, #)
                                  does NOT detect injection; sanitization runs earlier
```

**Primary attack vector:** The `status` field in **list** output mode. It is the main
free-text value rendered into the MCP Markdown response.

**Secondary surfaces:** Count mode (`groupBy=status` or `groupBy=label:key`) and health mode
(`Details`, `TopIssues`) also consume `status` / label values — these paths are sanitized
before aggregation. Annotations and labels are sanitized on `ResourceResult.Data` even though
the list formatter does not render them today.

---

## Solution: `internal/sanitize/` Package

A sanitization layer runs at the data extraction point in `FindResourcesCore`, immediately
after `dataMap` is parsed from each query row and before `ResourceResult` construction or
aggregation.

**Behavior:** Detected injection patterns are **always redacted** (no runtime toggle).
Matching string values are replaced with:

`[REDACTED: potential prompt injection detected]`

Detections are logged via `log.Printf` with the field name.

---

## Implementation Status

### Completed

#### `internal/sanitize/` package

| File | Purpose |
|---|---|
| `internal/sanitize/config.go` | `FieldPolicy`, `Config`, `DefaultConfig()` |
| `internal/sanitize/patterns.go` | Compiled regex pattern library, `InjectionDetected()` |
| `internal/sanitize/sanitize.go` | `Sanitizer`, `SanitizeString()`, `SanitizeResourceDataMap()` |
| `internal/sanitize/sanitize_test.go` | Unit tests per pattern category + false-positive cases |

**`DefaultConfig()` field policies:**

| Key | Policy |
|---|---|
| `name`, `namespace`, `kind`, `cluster`, `created` | `PolicySkip` |
| `_uid`, `_hubClusterName` | `PolicySkip` |
| `status`, `annotation`, `label` | `PolicySanitizeStrings` |
| *(any other key)* | `PolicySanitizeStrings` (default) |

**Pattern categories implemented** (see `patterns.go` and `sanitize_test.go`):

1. **Direct role/instruction overrides** — e.g. `ignore previous instructions`, `disregard all prior`, `forget your instructions`, `new instructions:`, `system prompt:`, `override instructions`
2. **Persona / role-play injection** — e.g. `you are now`, `you are a`, `act as`, `act like`, `pretend you are`, `roleplay as`, `from now on`
3. **Delimiter / special-token attacks** — e.g. `[SYSTEM]`, `[INST]`, `[USER]`, `[ASSISTANT]`, `<|im_start|>`, `<|system|>`, `### System`, `<system>`
4. **Tool invocation injection** — e.g. `call tool …`, `invoke the … tool`, `use the find_resources tool`

**Deferred / not implemented:** encoding evasion (Base64, homoglyphs, leetspeak), data-exfiltration URL patterns, `DAN` / `jailbreak` keywords — high false-positive risk.

#### Wiring in `internal/findresources/core.go`

```go
type FindResourcesCore struct {
    dbQueries *database.DatabaseQueries
    sanitizer *sanitize.Sanitizer
}

func NewFindResourcesCore(dbQueries *database.DatabaseQueries) *FindResourcesCore {
    return &FindResourcesCore{
        dbQueries: dbQueries,
        sanitizer: sanitize.New(sanitize.DefaultConfig()),
    }
}
```

Sanitization is applied in:

| Processor | Why |
|---|---|
| `processListMode` | Primary LLM output path (`status` in Markdown table) |
| `processCountMode` | `groupBy=status` or `groupBy=label:key` uses sanitized values |
| `processHealthMode` | `Details` / `TopIssues` aggregate sanitized `status` |

**Not sanitized:** `processSummaryMode` — only reads `kind`, `namespace`, and `cluster`
(DNS-safe / fixed vocabulary). No change required today.

Instantiation: `internal/server/server.go` calls `findresources.NewFindResourcesCore(dbQueries)`.
The formatter remains on `PostgresMCPServer`, not on `FindResourcesCore`.

#### Tool description (`internal/server/tools.go`)

The `find_resources` description includes a security note that free-text metadata fields
are inspected for prompt injection patterns and matching values are replaced with a
redaction marker.

#### Tests

| Location | Coverage |
|---|---|
| `internal/sanitize/sanitize_test.go` | Pattern detection, `SanitizeString`, `SanitizeResourceDataMap`, false positives, nested maps/slices, defensive config copy |
| `internal/findresources/core_test.go` | `processListMode` / `processHealthMode` adversarial status and annotation cases |

---

### Deferred (not shipped)

| Item | Original plan | Current state |
|---|---|---|
| `plans/injection-patterns.md` | Standalone pattern taxonomy doc | Patterns documented in `patterns.go` comments + unit tests |
| `MCP_SANITIZATION_MODE` env var | `allow` / `warn` / `block` rollout modes | Not implemented; behavior is always redact-on-detect |
| `pkg/config/config.go` sanitization setting | Configurable mode | `pkg/config` has DB/SQL settings only; no sanitization field |
| Integration tests for injection | `test/integration/findresources_integration_test.go` | Covered by unit tests in `core_test.go` and `sanitize_test.go` instead |

These may be revisited if operators need a `warn`-only rollout period before enforcing
redaction in production.

---

## Files Changed (as shipped)

| File | Change |
|---|---|
| `internal/sanitize/sanitize.go` | Core sanitizer |
| `internal/sanitize/patterns.go` | Pattern library |
| `internal/sanitize/config.go` | Field policies |
| `internal/sanitize/sanitize_test.go` | Unit tests |
| `internal/findresources/core.go` | `sanitizer` field; apply in list, count, and health processors |
| `internal/findresources/core_test.go` | Sanitization integration tests via `processListMode` / `processHealthMode` |
| `internal/server/tools.go` | `find_resources` description security note |
| `internal/server/server.go` | Constructs `FindResourcesCore` with default sanitizer |

**Not changed:** `pkg/config/config.go`, `cmd/server/main.go`, `test/integration/findresources_integration_test.go`

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| **False positives** — legit controller messages match patterns | Unit tests include common K8s statuses (`CrashLoopBackOff`, `Evicted`, `OOMKilled`, etc.); monitor `[sanitize]` log lines in production; tune `patterns.go` as needed |
| **Pattern evasion** — encoded or obfuscated payloads bypass regex | Defense-in-depth only; not a complete prevention layer |
| **Performance** — per-row regex on string values | Patterns compiled at package init; cost is sub-millisecond per resource at typical limits |
| **New output surfaces** — future modes serialize `ResourceResult.Data` | Sanitizer is on `FindResourcesCore`; any processor that sanitizes `dataMap` before use inherits protection |
| **`processSummaryMode` unsanitized** | Acceptable today — aggregates only DNS-safe / vocabulary fields |

---

## Acceptance Criteria

- [x] `internal/sanitize/` package implemented with unit tests covering all pattern categories
- [x] `processListMode`, `processCountMode`, and `processHealthMode` sanitize `dataMap` before result construction
- [x] Adversarial status and annotation values redacted in unit tests (`core_test.go`, `sanitize_test.go`)
- [x] Clean status messages pass through unchanged in unit tests
- [x] `find_resources` tool description updated to mention sanitization
- [x] PR reviewed and merged ([#55](https://github.com/stolostron/search-mcp-server/pull/55))
- [ ] `MCP_SANITIZATION_MODE` env var (`allow` / `warn` / `block`) — **deferred**
- [ ] Integration tests in `test/integration/findresources_integration_test.go` — **deferred** (unit tests provide coverage)

---

## Design Notes (plan vs. implementation)

The original plan proposed three rollout modes (`allow`, `warn`, `block`) and wiring through
`pkg/config`. The shipped implementation simplifies this:

- **Always block** on pattern match (simplest secure default).
- **No env var** — reduces misconfiguration risk; tuning is done in code + log review.
- **Unknown JSONB fields** default to `PolicySanitizeStrings` rather than kind-specific
  ConfigMap/Secret logic — functionally covers arbitrary indexed properties.
- **Search index field names** use `annotation` and `label` (singular), matching the ACM
  Search collector schema, not Kubernetes API plural forms.
