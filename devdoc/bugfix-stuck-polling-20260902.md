<!-- doc-class: historical -->
# Task Report: bugfix-stuck-polling-exempt (bug055 follow-up)

## Summary

bug055's toolStreak stuck detector (same tool + same args hash × N → terminate)
could not distinguish a genuinely stuck loop from a legitimate POLLING WAIT.
On 2026-09-02 three tasks died as "agent loop stuck: 10 consecutive rounds with
identical tool calls (tool=job_output)" while legitimately waiting on a
background job (t-1788306421, t-1788307556, t-1788307557). This fix exempts
polling tools from the pure fingerprint signal and replaces it with a
**state-change signal**: a poll whose RESULT changes (elapsed ticking, status
flip, new job id) proves the Bond waits on a live, finite external change →
alive; a byte-identical RESULT N times proves the polled target is frozen →
genuinely stuck → still terminated.

## Root Cause

bug055 (built from the Bond 718319 incident: a bare `bash sleep 115; ssh …`
monitor loop, identical args 272× / 10h15m) over-generalized
"identical tool+args = stuck". A polling tool's contract is literally
"same args, check external state, return current state" — identical tool_use
is BY DESIGN while waiting. The detector needed a polling-tool exemption,
not a general relaxation.

## Fix (chosen: Epic direction 2 — state-change signal, narrow exemption)

- `pollingTools` set (internal/agent/loop.go): `job_output`, `job_start`,
  `job_kill` — the job family whose contract is poll/wait/check-progress on
  an external state. NOT bash/read/etc: the 718319 bare bash monitor loop
  keeps the pure fingerprint signal.
- `toolStreakState.observe` (fingerprint feed, pre-switch): polling rounds
  now only record their fingerprint and bump `pollChain` — the streak is
  deferred to the result signal.
- NEW `toolStreakState.observeResults(toolCalls, results)`: fed once per
  round right after `executeToolBatch`. For polling rounds, accumulates only
  when the round's result hash equals the previous poll's (frozen state);
  a changed result resets to 1. Chain guard: `pollChain >= 2` (consecutive
  polls only — intervening real work breaks the chain) + `hasPollResult`.
- Non-polling rounds: unchanged fingerprint accumulation; a real-work round
  resets `pollChain` so a frozen-poll streak never survives intervening work.
- Trip path: identical terminate + `loop-stuck-toolful` notify, reused from
  the fingerprint trip (message wording distinguishes "identical poll results").

Key observations that made result-hashing safe:
- A real `JobOutput` on a STILL-RUNNING job includes `elapsed=%.1fs` which
  changes every round (LLM rounds are seconds apart, far above 0.1s
  resolution) → running-job waits never accumulate.
- A real `JobOutput` on a COMPLETED job freezes elapsed + output → identical
  result → accumulates → trips (stuck remains detectable, acceptance c).
- `job_start` returns a unique job id each call → result always changes →
  never trips (its args may be identical but the external state moved).

## Regression Tests (internal/agent/loop_stuck_test.go)

(a) `TestAgent_PollingJobOutput_RunningJobNotKilled` — Bond polls job_output
    on a still-running job (result changes per round) for 25 rounds, runs to
    the iteration budget, no `loop-stuck-toolful` notify, no stuck error.
(b) `TestAgent_StuckToolfulLoop_TerminatesAndNotifies` (existing) — the
    718319 bare bash monitor loop STILL terminates. Unchanged and green.
(c) `TestAgent_PollingJobOutput_FrozenJobKilled` — job_output poll on a
    completed/unchanging job (byte-identical result) terminates after
    maxStuckToolfulRounds with the `loop-stuck-toolful` error + notify.
Plus `TestToolStreak_PollingStateUnit`: changing results never accumulate
(25 polls), frozen results trip at threshold, intervening real work breaks
the poll chain, job_start fresh-ids never trip, bash non-poll still trips.

## Verification

- Build: `go build ./...` OK
- Tests: `go test ./internal/agent/ ./internal/tools/` green (see acceptance 2)
- Targeted: `go test ./internal/agent/ -run 'TestToolStreak|TestAgent_StuckToolful|TestAgent_Polling'` 7/7 PASS
- Commit: see final message

## Not Done / Honest Limits

- A TRUNCATED (finish_reason=length) polling round never executes → never
  reaches observeResults → contributes no poll signal. A truncation loop of
  polls is not caught by the poll path (pre-existing corner; the first
  no-tool counter still catches identical truncated text).
- `job_start` repeated with the same command never trips even if the Bond
  is arguably churning (each start = a new external state). Accepted: the
  Epic names job_start in the whitelist direction.
- Exemption is keyed on tool NAME (semantic contract), not on argument
  shape — a future poll-like tool must be added to `pollingTools` explicitly.
