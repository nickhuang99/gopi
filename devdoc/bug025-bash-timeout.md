<!-- doc-class: historical -->
# Task Report: bug025 — bash timeout as an LLM tool parameter

## Summary

Made the bash tool's timeout an **LLM decision** instead of a fixed framework
constant. The tool schema now exposes an optional `timeout_sec` parameter
(default 120s; `0` = unlimited absolute time), and the idle-output safety net
was raised to 60s. The model can now say `timeout_sec=300` for a long build
instead of being killed at a hard 60s cap.

## Completed

### `internal/tools/timeout.go`
- `DefaultBashTimeout` 60s → **120s** (single per-call default, Rule 2).
- `DefaultBashIdleTimeout` 30s → **60s** (no-output safety net).
- Env overrides (`GOPI_BASH_TIMEOUT_SEC`, `GOPI_BASH_IDLE_SEC`) unchanged and
  still read exactly once in `init()`.

### `internal/tools/bash.go`
- Named constants `bashDefaultTimeoutSec = 120` and `bashNoOutputTimeoutSec = 60`
  (seconds, mirroring the duration constants in `timeout.go`).
- New `BashWithTimeout(command string, timeoutSec int) (string, error)`:
  - `timeoutSec > 0` → absolute kill after that many seconds; idle timeout
    capped at `min(DefaultBashIdleTimeout, timeoutSec)` so the requested budget
    stays a strict upper bound.
  - `timeoutSec == 0` → **unlimited** absolute time (no deadline); the idle
    safety net (60s of silence) still applies.
  - `timeoutSec < 0` → invalid; falls back to the default `BashTimeout()`.
  - Precedence (Rule 2): `timeout_sec` arg > `GOPI_BASH_TIMEOUT_SEC` env >
    `DefaultBashTimeout` (120s).
- `bash()` special-cases `absTimeout == 0`: builds a plain cancellable context
  instead of `context.WithTimeout(ctx, 0)` (which fires immediately and would
  SIGKILL the process group at t≈0 — the correctness landmine flagged in the
  evaluation). The step-6b deadline watcher is naturally a no-op (no deadline →
  `ctx.Err()` is never `DeadlineExceeded`). Idle monitor unchanged.
- `Bash()` remains the backward-compatible default entry point (used by tests
  and by the loop when `timeout_sec` is omitted).
- Doc comments refreshed (120s/60s wording, `timeout_sec` semantics).

### `internal/agent/loop.go`
- bash tool schema: added `timeout_sec` property (integer, default 120,
  "0 = unlimited absolute time, still subject to the 60s no-output safety
  net"); description no longer advertises "60s timeout" — now points at
  `timeout_sec` for long-running commands.
- `case "bash":` parses `args["timeout_sec"]` (float64, as JSON integers decode
  via plain `json.Unmarshal`); omitted → `tools.Bash` (default), present →
  `tools.BashWithTimeout`. Negative/invalid falls back to the default inside
  `BashWithTimeout`.

### Tests
- `timeout_test.go`: `TestTimeoutConstants_Defaults` updated to 120s/60s and
  now asserts the second-based constants mirror the duration constants.
- `bash_test.go`: 5 new tests —
  - `TestBashWithTimeout_PositiveKills` (`timeout_sec=1` kills `sleep 2`),
  - `TestBashWithTimeout_ZeroUnlimited` (`timeout_sec=0` lets `sleep 2` finish),
  - `TestBashWithTimeout_NegativeFallsBackToDefault`,
  - `TestBashWithTimeout_IdleKillUnderUnlimited` (idle kill still fires with
    abs=0, exercised at internal level with a 1s idle),
  - `TestBashWithTimeout_UnlimitedFiniteCommand` (abs=0 normal completion).
- `loop_test.go`: `TestExecuteTool_BashTimeoutSec` — dispatch-level proof that
  `timeout_sec` is parsed (float64) and enforced for positive, zero, and
  omitted cases.

### Docs
- `devdoc/bug025-bash-timeout.md` (this report).
- `devdoc/tool-timeout-deadlock-fix.md`: added a pointer note that the constants
  changed in bug025 (historical doc showed stale 60s/30s values).

## Not Done
- Did NOT lift the idle-output kill under `timeout_sec=0`: 60s of silence is
  still treated as a stuck process (deliberate — the bug file mandates the
  no-output kill as a safety net; documented in the schema description).
- No change to the 100KB output limit (unchanged, per constraints).
- No new top-level tool added; the fix reuses the existing functional
  `Bash`/`bash` style (Rule 3) rather than inventing a `BashTool` struct.

## Verification
- Build: `go build ./...` → **OK**
- Vet: `go vet ./internal/tools/ ./internal/agent/` → **OK**
- Tests: `go test ./... -count=1` → **3/3 packages PASS** (agent, deepseek,
  tools). New tests all pass; pre-existing environment-sensitive
  `TestBashDaemonSpawn_DoesNotLeakProcesses` passes on this machine.
- Commit: `2c5e8c0` — `feat(tools): make bash timeout an LLM-driven timeout_sec parameter`

## References
- `bugs/bug025-bash-timeout.txt` (the proposal)
- `devdoc/bug025-bash-timeout-evaluation.md` (evaluation + recommended patch shape)
- `internal/tools/bash.go`, `internal/tools/timeout.go`, `internal/agent/loop.go`
