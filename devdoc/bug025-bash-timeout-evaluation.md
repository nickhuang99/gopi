<!-- doc-class: historical -->
# Task Report: bug025 — bash timeout as a tool parameter (evaluation)

## Summary

Evaluation of `bugs/bug025-bash-timeout.txt` against the current codebase.
**Verdict: the approach is sound and worth implementing, but the proposal as
written cannot be applied verbatim.** It references a type that does not exist
(`BashTool`), has a correctness landmine at `timeout_sec=0` (immediate kill),
and leaves env-var precedence and idle-timeout behavior undefined. All gaps
below have concrete, minimal resolutions.

This is an **evaluation-only** task — no code was changed.

---

## Completed — findings

### 1. Current implementation (as-built)

- Tool schema lives in `internal/agent/loop.go` (`toolDefs`, `Name: "bash"`,
  line ~159). Description currently says `"...60s timeout, 100KB output limit."`
  and the schema has a single `command` parameter.
- Execution path: `loop.go` `case "bash":` → `tools.Bash(command)`.
- `internal/tools/bash.go` exposes a **package-level function**
  `Bash(command string) (string, error)`. **There is no `BashTool` type and no
  `Run()` method anywhere in the repo** (grep for `BashTool` returns nothing).
  The internal helper `bash(command, absTimeout, idleTimeout time.Duration)`
  already accepts timeouts and implements: absolute deadline via
  `context.WithTimeout`, idle-output monitor goroutine, process-group kill
  (Setpgid, bug011 fix), 100KB truncation, `exit=N` formatting.
- Defaults live in `internal/tools/timeout.go`: `DefaultBashTimeout = 60s`
  (override `GOPI_BASH_TIMEOUT_SEC`), `DefaultBashIdleTimeout = 30s` (override
  `GOPI_BASH_IDLE_SEC`), read exactly once in `init()` (Rule 2).
- Tool args are decoded with plain `json.Unmarshal` (no `UseNumber`), so a
  JSON integer arrives as `float64` — the proposal's `case float64` is correct.

### 2. Discrepancies / gaps in the proposal

| # | Issue | Severity | Resolution |
|---|-------|----------|------------|
| 1 | **`BashTool` type does not exist.** The proposal's Step 2 is written against a struct that isn't in the codebase. | High | Keep the functional style (Rule 3 — tools are minimal). Add `timeoutSec` to the existing entry point: either variadic `Bash(command string, timeoutSec ...int)` (backward compatible with the one production caller and test helpers) or a new `BashWithTimeout(command string, timeoutSec int)`. Plumb `args["timeout_sec"]` in `loop.go` `case "bash":` into it. |
| 2 | **`timeout_sec=0` (unlimited) would kill instantly.** `context.WithTimeout(ctx, 0)` fires immediately → `ctx.Err() == DeadlineExceeded` → the step-6b kill goroutine SIGKILLs the process group at t≈0. The proposal's `timeout = 0 // unlimited` comment is dangerous as-is. | High (correctness) | Special-case 0: build the context with `context.Background()` (no deadline) and skip creating the deadline watcher goroutine (or make the watcher no-op when the context has no deadline). Only the idle monitor remains as the safety net. |
| 3 | **Env-var precedence undefined.** Current behavior: `GOPI_BASH_TIMEOUT_SEC` overrides the 60s default via `init()`. The proposal ignores env vars in the snippet, which would silently break existing deployments that set them. | Medium | Define precedence: `timeout_sec` arg (LLM) > `GOPI_BASH_TIMEOUT_SEC` (env, read only in `timeout.go` init — Rule 2) > `bashDefaultTimeoutSec` constant. Keep `init()` as the single env-var read site; fall back to `BashTimeout()` when the arg is absent. |
| 4 | **Two "defaults" for the same concept.** Proposal adds `bashDefaultTimeoutSec = 120` while `timeout.go` still has `DefaultBashTimeout = 60s`. Drift-prone and confusing. | Medium | Consolidate: change `DefaultBashTimeout` to `120 * time.Second` (or repurpose it as the named constant) so there is exactly one per-call default. The bug file's Step 2 snippet reads `bashDefaultTimeoutSec * time.Second` but never shows where the env override plugs in — see #3. |
| 5 | **Idle timeout 30s → 60s is a silent behavior change.** Proposal introduces `bashNoOutputTimeoutSec = 60` as the new default idle kill. Current default is 30s (`DefaultBashIdleTimeout`), asserted by `TestTimeoutConstants_Defaults`. | Medium | Deliberate decision + update the constant and the test. Also decide the interaction: with `timeout_sec=0`, a silent-but-progressing command (e.g. a quiet `go build`) is still killed after 60s of silence — "unlimited" is only unlimited while output flows. Consider making idle timeout per-call too, or `idle = min(60s, requested)` so `timeout_sec` stays an upper bound. At minimum, document that 0 = unlimited *absolute* time but still subject to the idle safety net. |
| 6 | **Schema/code default drift (Rule 2 nuance).** The JSON schema hardcodes `"default": 120`; the code constant lives in `internal/tools`. They can diverge. | Low | Acceptable (JSON schema can't reference Go constants). Add a cross-reference comment in both places. Note the schema `default` is informational for the LLM only — the real default is enforced in code, which matches (omitting the arg yields 120s). |
| 7 | **Stale description text.** Step 1's new description drops the old "60s timeout" wording — must also update the existing line so it stops advertising 60s. | Low | Covered by the proposal; just make sure the old string in `loop.go` is fully replaced (it contains `60s timeout, 100KB output limit.`). |
| 8 | **No test plan in the proposal.** | Medium | Add tools-level tests: omitted arg → default; `timeout_sec=1` kills `sleep 2`; `timeout_sec=0` runs past the default (e.g. `sleep 2` completes with no timeout error); negative/invalid value falls back to default; idle kill still fires under `timeout_sec=0`. Existing `TestBashCommandTimeout`/`TestBashIdleTimeout_*` already exercise the internal `bash()` and stay valid. |

### 3. What the proposal gets right

- LLM-driven timeout is the correct architectural call ("pull, don't push") and
  matches the codebase's Rule 2 philosophy (config in one place, explicit).
- Keeping the idle-output kill as a safety net is right — it is the only guard
  against infinite-output and silent-stuck commands, and it is orthogonal to a
  longer absolute budget.
- `case float64` type switch matches the actual JSON decoding path.
- `v >= 0` guard correctly rejects negative values (falls back to default).
- 100KB output limit untouched — no interaction with timeouts.

### 4. Recommended minimal patch shape (for the implementer)

1. `internal/tools/timeout.go`: change `DefaultBashTimeout` to `120 * time.Second`
   (single named default, keep env override reading in `init()`).
2. `internal/tools/bash.go`:
   - Add `BashWithTimeout(command string, timeoutSec int) (string, error)` (or
     variadic `Bash`). Internally: if `timeoutSec == 0` → no absolute deadline
     (skip `context.WithTimeout` and the deadline watcher); else reuse existing
     `bash(command, abs, idle)` path. Negative → default.
   - Keep `DefaultBashIdleTimeout` (30s) or rename to
     `bashNoOutputTimeoutSec = 60s` — pick one, update the constant test.
3. `internal/agent/loop.go`:
   - toolDefs: add `timeout_sec` property (integer, default 120, "0 = unlimited"),
     update description (drop "60s timeout", mention `timeout_sec`).
   - `case "bash":` parse `args["timeout_sec"]` (`float64` → int, guard
     negatives), pass through; omitted → default.
4. Tests + this devdoc's verification section; then commit on a feature branch
   per Rule 6.

---

## Not Done

- No code changes made (evaluation-only epic).
- Did not resolve the `GOPI_BASH_TIMEOUT_SEC` env override wording in GOPI.md
  (no GOPI.md mention of the bash env vars exists; only code comments in
  `bash.go`/`timeout.go` reference them — those comments should be refreshed
  during implementation).
- Did not settle whether `timeout_sec=0` should also lift the idle kill; this
  is a product decision for the implementer (recommendation in gap #5).

---

## Verification

- Build: `go build ./...` → **OK** (baseline, unmodified tree).
- Vet: `go vet ./internal/tools/` → **OK**.
- Tests: `go test ./internal/tools/` → **14/15 PASS** (all bash idle/timeout,
  truncation, and timeout-constant tests pass).
  - Pre-existing environmental failure, unrelated to bug025:
    `TestBashDaemonSpawn_DoesNotLeakProcesses` — a `sleep 300` background
    process survives the process-group kill check on this machine (timing /
    environment-sensitive; reproduces without any changes).
- Commit: none (no changes made — evaluation only).

## References

- `bugs/bug025-bash-timeout.txt` (the proposal under review)
- `internal/agent/loop.go` — toolDefs `bash` entry (line ~159), dispatch
  `case "bash":` (line ~917), `validateToolArgs`
- `internal/tools/bash.go` — `Bash()` / `bash()` / kill-watcher / idle monitor
- `internal/tools/timeout.go` — constants + single env-var read site
- `internal/tools/bash_test.go`, `bash_daemon_test.go`, `timeout_test.go`
