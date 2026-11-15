<!-- doc-class: normative -->
# effort_values — settings.json per-provider effort whitelist (config-ization)

Branch: `feat/effort-values-settings` (commits `1200f7b`, `deaf20e`)
Worktree: `~/workspace/gopi-effort-values`

## Design decisions

- **effort_values list, not effort_map** (PA ruling): the provider's legal
  effort range is declared as DATA in settings.json — Go ships zero vendor
  mapping. No cross-protocol translation (deepseek `thinking.type` / claude
  `thinking` / vendor-proprietary effort tokens are never synthesized).
- **Listed → verbatim passthrough.** What you send is what goes. Match is
  trimmed + case-insensitive, the configured value is sent unchanged.
- **Unlisted → `minimal`, ok=false.** Fallback does NOT auto-enable thinking
  (same semantics as the old hardcoded switch: an invalid value does not
  imply intent). `off` is harness-only (thinking disabled) — always accepted,
  never a list member.
- **Nil list → DeepSeek baseline** `minimal|low|medium|high|max`,
  default `minimal` (pre-existing behavior, unchanged).
- **Runtime 4xx is the mapping's implementation:** a listed effort that the
  provider still rejects hits the existing minimal-core degradation ladder
  (`internal/deepseek/error_1214.go` rung 1 strips reasoning_effort and
  re-sends). Run-time reality beats declared data.
- **Transport:** `config.ApplyEnv` joins a non-empty `effort_values` into
  `GOPI_EFFORT_VALUES` (comma-separated) — single-source env channel; the
  agent loop parses it with `splitEffortValuesEnv`.

## Changed files

| file | change |
|---|---|
| `internal/config/config.go` | `EffortValues []string` field (`effort_values`) — commit 1200f7b |
| `internal/config/settings.go` | validation of the list (trim/case-insensitive dedup, sub-set check off by design), exported `ResolveReasoningEffort` wrapper |
| `internal/config/env.go` | `ApplyEnv` injects `GOPI_EFFORT_VALUES` |
| `internal/config/help.go` | `--reasoning-effort` help text |
| `internal/agent/loop.go` | `splitEffortValuesEnv` + effort block resolves via `config.ResolveReasoningEffort`; warn+fallback instead of hard fail |
| `internal/deepseek/error_1214.go` | comment tying rung-1 stripping to effort_values design |
| tests | `settings_effort_test.go` (resolve + ApplyEnv), `internal/agent/effort_test.go` (4 subtests) |

## Settings example

```json
{
  "effort_values": ["max", "high", "low"],
  "reasoning_effort": "high"
}
```

## Verification

- `go build ./...`, `go vet` — clean.
- `go test ./...` — 12/12 packages OK (471s, job-1 log `/tmp/effort-values-test.log`).
- New tests: `TestResolveReasoningEffort` (nil baseline / listed passthrough /
  unlisted fallback-no-auto-think / synthetic listed value), `TestApplyEnvEffortValues`,
  `TestNewAgent_EffortValuesListValidates` (listed-high-passthrough,
  unlisted-medium-fallback-no-auto-think, synthetic-listed-value-passthrough,
  nil-list-baseline-medium).
- E2E smoke on GLM (`glm-5.3-flash`, real API, raw log):
  - `effort_values [max,high,low]` + `reasoning_effort=high` → wire carries
    `"reasoning_effort":"high"` ×3 requests; thinking enabled.
  - `reasoning_effort=medium` (unlisted) → stderr warning
    `not in the provider's effort_values, using "minimal"` and minimal on the
    wire; thinking NOT auto-enabled.

## Not done / deferred

- No per-provider effort translation table (intentional — data not code).
- `off` remains harness-only; not wired into the effort_values list path.
