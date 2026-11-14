<!-- doc-class: historical -->
# Devdoc: Agent-Loop Stuck-Net + Session Wallclock Cap

Date: 2026-09-02 · Commit: `0ab164d` · Files: `internal/agent/loop.go`,
`internal/agent/loop_stuck_test.go`, `internal/tools/taskheartbeat.go`

## Incident chain (why)

**t-1788173679** (2026-09-01): a gopi task ran **4276 iterations** for
**12h+**, each ~20s, emitting the identical plain text "已完成：HEAD" — no
tool_use, no exit. The agent loop had NO bottom guard: Rule 7's stop relied
purely on LLM volition, and bug022's end-turn exit (2 consecutive no-tool
rounds) never fired because **truncated (finish_reason=length) rounds reset
`consecutiveEndTurn` and `continue`** — the loop treated every truncated
round as "keep going". A second 12h loop the same day (M4.22 lock-model
eating its own task) confirmed the class: the loop needs a mechanical net.

## ① Stuck detection

- `maxStuckRounds = 5`, `stuckTextPrefixChars = 50` (both consts, loop.go).
- `stuckState.observe(text, hasToolUse)`: tool_use → **reset**; no-tool
  round whose first ≤50 runes (after TrimSpace) equal the previous
  qualifying round's prefix → **streak++**; divergent text → streak=1;
  empty-text rounds → no signal either way.
- **ONE feed point**: immediately after `content/toolCalls` extraction,
  BEFORE the finish_reason switch — so every continue path (truncated
  rounds, end-turn nudges, completion-store checkpoints) passes through it.
  An earlier placement after the switch left the length branch bypassing
  the net (found by the failing repro test — the mock had to stream
  `max_tokens` to reproduce the incident shape).
- Tripped → stderr ⛔ log + `notifyLoopStuck` + error return
  ("agent loop stuck: N consecutive rounds …").

### Notify

`tools.NotifySyntheticEvent(event, sessionID, model, summary)` — session-level
alarm delivery through the existing GOPI_TASK_NOTIFY_WEBHOOK/_CMD pipeline
without a ledger row (sibling of `stale`). Honors the events filter;
`GOPI_TASK_NOTIFY_EVENTS=loop-stuck` (or `all`) subscribes. Sync/async per
`GOPI_TASK_NOTIFY_ASYNC`.

## ② Session wallclock cap

- `GOPI_SESSION_MAX_WALLCLOCK` — unset = 8h default (`defaultSessionWallclock`),
  `0` = unlimited, positive seconds = override, invalid = warn + default
  (`sessionWallclockCap()`, the single env reader).
- The deadline is folded into the run ctx (`WithDeadline`) alongside
  GOPI_TIMEOUT_SEC; the shorter deadline wins and `wallclockExceeded`
  disambiguates at the boundary.
- Graceful finish, not abort: a cap hit **at a round boundary** (ctx select)
  or **mid-LLM-call** (chatOnce error path guarded by
  `!time.Now().Before(deadlineTime) && ctx.Err() != nil`) returns the last
  substantive assistant text (`lastSubstantiveText`) with **nil error** —
  the normal end-of-run defer then writes session_end, closes the ledger row
  as completed, and fires `event=session-wallclock-exceeded`. A hard
  GOPI_TIMEOUT_SEC hit (deadline earlier than the cap) keeps the historical
  error path — a timeout IS a failure.

## Verification

- `go build ./...` OK · `go vet` OK.
- `go test ./internal/agent/`: full package **ok** (after the fix; the
  intermediate state had 14 fails from a duplicated observe — triple-counted
  streaks; removed the two later copies, kept the single top feed point).
- New tests (`loop_stuck_test.go`, all PASS):
  `TestAgent_StuckLoop_TerminatesAndNotifies` (mock LLM streams identical
  truncated text → terminates ≤6 rounds + loop-stuck lands in the
  GOPI_TASK_NOTIFY_CMD capture file), `TestAgent_StuckLoop_ToolUseResets`
  (tool rounds interleaved → never trips), `TestAgent_StuckLoop_DivergentTextResets`,
  `TestSessionWallclockCap` (default/0/override/invalid),
  `TestAgent_WallclockCap_GracefulFinish` (1s cap + endless tool rounds →
  nil error, non-empty result, wallclock-exceeded notify captured),
  `TestAgent_WallclockCap_ZeroUnlimited`.
- `internal/tools` notify/stale family PASS (`TestSyncClaimRemoteAcquireAdvances`
  fails on master WITHOUT this change too — pre-existing, claim-probe related).

## Not done / deferred

- Review sub-tasks **t-1788242708 / t-1788242718** queued (stuck-observe
  ordering + truncated-round counting semantics) — superseded by this
  implementation (single feed point already counts truncated rounds); a
  runner may close them after verification.
- No restart-after-cap auto-resume: the ledger row carries the resume hint
  (failed rows) but completed-by-cap rows simply end; operators re-dispatch.

## Ops notes

- Subscribe: `GOPI_TASK_NOTIFY_EVENTS=loop-stuck,session-wallclock-exceeded,stale`
- Tune: `GOPI_SESSION_MAX_WALLCLOCK=3600` (1h), `0` to disable.
- A loop-stuck alarm means the LLM was genuinely repeating itself 5 rounds
  straight — inspect the session JSONL tail before re-dispatching.
