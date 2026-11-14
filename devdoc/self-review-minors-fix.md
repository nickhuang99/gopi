<!-- doc-class: historical -->
# Task Report: Self-Review 3 Minor Bugs Fixed (feat/fix-3minor)

Date: 2026-08-30 · Branch: `feat/fix-3minor` (HP worktree ~/work/GoPi, based on
peerbox/master cb7a675) · Source findings: devdoc/self-review-20260830.md tickets
t-1788053264-1 / -2 / -3.

## Completed

### ① loop.go exit-check progressing warning fired EVERY round (t-1788053264-1)
- **Was:** the "Exit-check" warning sat on the every-round path (before the
  finish_reason switch) with no exit-state guard, so any round that re-emitted
  a progressing list logged `[gopi] exit with N progressing tasks` — 3× in a
  normal 4-round run (proven by repro), misleading wording on non-exit rounds.
- **Fix:** deleted the unguarded block. New `Agent.warnExitingWithProgressing`
  is called ONLY from the real exit paths: (a) R3/trusted fast-exit return,
  (b) bug022 confirmation-exhausted return, (c) max-iterations bail.
  - Completion view = this round's list when the final message re-emits one,
    else the harness-tracked state (`todoState.view()`, new accessor) — so the
    P1.5 case (final message legitimately omits the list) is still covered.
  - Wording corrected to the exit fact: `[gopi] run exiting with %d
    progressing task(s): [ids]`.
  - Nil-Logger safe (observability must never crash the exit path).

### ② isNonZeroExit hard-coded exit-code list missed 3/42/134/143 (t-1788053264-2)
- **Was:** substring enumeration {1,2,128,127,126,255,11,137} — exit=3 (git),
  42 (curl), 134 (SIGABRT), 143 (SIGTERM) slipped past the dead-end nudge, and
  the substring match fired on embedded text ("the pexit=1 prefix", repro-proven).
- **Fix:** parse the FIRST `exit=` line's numeric value: `N != 0` = dead end;
  non-numeric (`exit=foo`) or no exit line = not a dead end. Match is anchored
  to a real bash exit line (line start, optional `[t+NNNs] ` tick prefix
  stripped first), so embedded text no longer false-positives. Semantics of
  the `deadEndMarkers` `"exit="` branch unchanged (only non-zero triggers).
- Every non-zero code now counts — no list to maintain.

### ③ deepseek/retry.go debugLog init snapshot + `!= ""` truthiness (t-1788053264-3)
- **Was:** `var debugLog = os.Getenv("GOPI_DEBUG") != ""` — (a) the canonical
  `off` ApplyEnv force-writes counted as ON (the exact regression agent's
  boolEnvTrue fixed on 2026-08-29, missed in deepseek); (b) package vars
  evaluate before main, so settings.json `"debug": true` (ApplyEnv writes
  GOPI_DEBUG=1 at runtime) never reached it.
- **Fix:** `debugLogEnabled()` reads LAZILY via **`config.EnvBool`** — the
  truthy parser ("1", "on", "yes", "true", "enabled") promoted from agent's
  boolEnvTrue into internal/config/env.go as the SINGLE source of truth
  (GOPI.md Rule 2); `agent.boolEnvTrue` now delegates to it. deepseek already
  imported internal/config — no new dependency edge. logRetry's existing
  short-circuit keeps the hot path clean (read happens only when formatting).

## Not Done / honest gaps
- Repro test B1 asserts warning count (==1) via the new wording substring;
  it does not pin the exact sentence (wording may evolve without test churn).
- The max-iterations bail path's warning is exercised only by unit composition
  (helper + view()), not by a dedicated multi-round max-iter integration test.
- The `[t+…]` tick prefix stripping in isNonZeroExit is belt-and-braces for
  stamped results; no dedicated test pins the tick-prefix case (cheap to add
  if it ever regresses).
- Historical devdoc mentions of "exit with N progressing" (tasklist-audit*.md,
  perf-fix) left as-is — historical record, not live docs.

## Verification
- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0)
- Repro-first: RED before fix (B1 fired 3×; B2 exit=97 false + pexit=1 true;
  B3 build-failed on undefined debugLogEnabled), GREEN after fix — all three
  repro tests pass.
- Full suite: `go test ./... -count=1` (detached, /tmp/fix3minor-fulltest.log):
  internal/config, internal/deepseek, internal/checkpoint, internal/gopilog,
  internal/proxy, internal/testutil, internal/text2img all ok; internal/agent
  green except 2 failures proven PRE-EXISTING via `git stash -u` on clean
  cb7a675 (TestAgent_Run_ContextTrimReplaceSlimming, TestAgent_Run_ToolResultPayloadOnce
  — filed as bug ticket t-1788063844, not introduced here); cmd/agent
  code_search family fails on this box's known-broken ctags DB ("Unknown
  language Rust/TypeScript") + missing rg — environment, documented in
  self-review-20260830.md §5. All 3 new repro tests + boolEnvTrue table +
  tasklist/trusted-completion family green.
- Vet: `go vet ./internal/agent/ ./internal/deepseek/ ./internal/config/` → clean.
- Commit: `git log -1` on feat/fix-3minor (fix + tests + devdoc in one commit;
  hash recorded in the session's final report).

## Tests added
- internal/agent/selfreview_minors_repro_test.go
  - TestReproExitCheckWarnsOnlyOnExit (4-round mock run, warns == 1)
  - TestReproIsNonZeroExitParsesCode (16 cases incl. 3/42/134/143/97, 0-exempt,
    non-numeric, embedded-word)
- internal/deepseek/retry_debuglog_repro_test.go
  - TestReproDebugLogTruthy (11 truthy/falsy values incl. off/OFF)
  - TestReproDebugLogUnset (unset = off)
  - TestReproDebugLogSettingsPassthrough (ApplyEnv(settings debug=true) → on)
- internal/config: EnvBool promoted; agent.boolEnvTrue delegates (behavior
  unchanged — loop_test.go:1645 truthiness table still green via full suite).
