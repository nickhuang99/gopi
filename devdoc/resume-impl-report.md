<!-- doc-class: historical -->
# Resume Feature — Implementation Report (feat/resume-arm-trim)

> Branch: `feat/resume-arm-trim` (A/B arm: thinking-trim enabled — the trim
> arm of the resume competition). Worktree: `~/work/gopi-resume-arm-trim`.
> Builds on `devdoc/resume-feasibility.md` (Tier B byte-exact, recommended).

## What was implemented

The **minimal usable resume closed loop** (feasibility Tier B, Phase 1+2):

1. **`internal/checkpoint/`** — raw-log checkpoint extraction (read-only,
   offline, zero hot-path risk):
   - `parse.go`: marker-driven `Scan()` over `→ REQUEST` / `← CHUNK` /
     `← RESPONSE` entries. Multi-line SSE chunk payloads handled correctly
     (feasibility §4.1 update); payloads byte-exact (LogFunc newline trimmed).
   - `index.go`: `IndexDir()` registry — per-raw-log last-checkpoint metadata
     (model, msg count, bytes, sha256 dedupe, last message), newest first,
     corrupt logs reported without breaking the list.
   - `LastRequest()`: the resume point — last `→ REQUEST`, parsed to
     `deepseek.ClaudeRequest` (system blocks + full history with thinking
     signatures + wire params).

2. **`internal/agent/resume.go`** — bootstrap:
   - `Resume(req, continuation)`: seeds `Ctx` from the checkpoint verbatim
     (system blocks from checkpoint — cwd GOPI.md drift cannot corrupt the
     resumed context; history inverted via `messagesFromClaudeRequest` with
     thinking text + signatures + tool_use args preserved), appends the
     continuation **unframed** (no `[EPIC GOAL]` re-wrap, no task-start
     memory_search checkpoint), then runs the **shared loop**.
   - `seedFromCheckpoint`: replays wire params (model, thinking,
     reasoning_effort, max_tokens) from the checkpoint; forces `KeepThinking`
     so the replayed thinking blocks (with signatures) reach the wire.
   - `runLoop()`: extracted from `Run()` — Run and Resume share the entire
     ReAct loop body; the only differences are the entry message and the
     one-shot task-start checkpoint, both handled before the shared call.

3. **`cmd/agent/cli_checkpoint.go`** — CLI:
   - `gopi checkpoint list [--dir DIR]` — registry of every raw log's last
     checkpoint.
   - `gopi checkpoint export <log>` — print the snapshot JSON.
   - `gopi resume <log> [continuation...]` — continue from the checkpoint.
     `<log>` accepts a path, bare filename, or `gopi-raw-<timestamp>` prefix.
     Continuation from args, piped stdin, or default
     "Continue from where you left off." Wired into `main.go` before config
     resolution (same pattern as `log`/`memory` subcommands).

4. **`internal/deepseek/claude_client.go`** — `ClaudeMessage.UnmarshalJSON`
   tolerant of both wire shapes: current array content (BuildClaudeRequest)
   AND legacy string content (pre-2026-08 raw logs) — old logs stay
   resumable. Only the checkpoint parser unmarshals this, so no hot-path risk.

## Verification

- **Unit tests** (all `go test -count=1` green):
  - `internal/checkpoint/parse_test.go` (9 tests): Scan entry/byte-exact/
    multi-line-chunk/no-marker, LastRequest pick-last/no-request/missing-file/
    full-message-preservation, marker-text-in-chunk guard, IndexDir registry
    incl. corrupt-log resilience.
  - `internal/agent/resume_test.go` (3 tests): **no-op clone invariant** —
    checkpoint → seed → rebuild wire == checkpoint (thinking text + signature
    + tool_use args all survive); nil-safe; end-to-end `Resume()` via httptest
    asserting the first resumed request carries full history + verbatim
    continuation and NO epic re-framing / task-start checkpoint.
  - `cmd/agent/cli_checkpoint_test.go` (5 tests): resolveRawLog (path / bare
    name / prefix / missing-exit) + truncate.
- **Full package suite**: agent, checkpoint, cmd/agent, deepseek, config,
  gopilog all `ok` (internal/tools has a pre-existing `-count=1` run flake
  unrelated to this branch; all 78 tools tests pass individually).
- **Live end-to-end (real CLI, real LLM)**:
  1. Ran a 5-step task with `timeout 7` → interrupted (exit 124) mid-task
     after "phase1" created.
  2. `gopi resume <rawlog> "finish steps 3-5..."` → completed steps 3–5
     (phase2/phase3 appended), did NOT redo step 1, 4 rounds / 4.9s.
  3. **Prefix cache: 98.5% hit** (25856/26263 tokens) on the first resumed
     request — byte-exact checkpoint replay confirmed live.
  4. Resumed request verified: original epic at history index 0 + verbatim
     continuation, no `[EPIC GOAL]` re-wrap, no task-start search.

## Not done (honest)

- **Tier B §4.6 byte-splice** (append continuation by splicing the raw
  `messages` array bounds, zero re-serialization): NOT implemented — the
  continuation message is appended after `messagesFromClaudeRequest` +
  `BuildClaudeRequest` re-marshal. Prefix cache still hit 98.5% in the live
  test because the history portion is re-marshaled identically (map-key
  sorting is deterministic for the same data); the residual ~1.5% miss is the
  re-marshaled request envelope vs the parent's exact bytes. A future
  byte-splice would make it 100%, but the minimal loop is proven usable.
- **Cache-hit assertion test** (feasibility §7 "first response reports
  cache_hit ≈ parent's last prompt_tokens"): verified live manually (98.5%),
  not as an automated test.
- **Session-JSONL resume (Tier A)** and `clone --detached` sub-task mode
  (design doc §4.7): out of scope for the minimal loop.
- **`checkpoint export`** prints the re-marshaled pretty JSON (readable
  export), not a byte-exact copy of the raw payload line.

## Files

| Path | Role |
|---|---|
| `internal/checkpoint/parse.go` | marker-driven parser + LastRequest |
| `internal/checkpoint/index.go` | checkpoint registry |
| `internal/checkpoint/parse_test.go` | parser/index tests |
| `internal/agent/resume.go` | Resume + seedFromCheckpoint + wire inversion |
| `internal/agent/resume_test.go` | clone-invariant + end-to-end tests |
| `internal/agent/loop.go` | runLoop extraction (Run/Resume shared body) |
| `cmd/agent/cli_checkpoint.go` | checkpoint list/export + resume CLI |
| `cmd/agent/cli_checkpoint_test.go` | CLI tests |
| `cmd/agent/main.go` | subcommand dispatch wiring |
| `internal/deepseek/claude_client.go` | ClaudeMessage dual-shape unmarshal |
