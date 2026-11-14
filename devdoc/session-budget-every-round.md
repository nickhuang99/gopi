<!-- doc-class: historical -->
# Task Report: the session budget on EVERY request, not only at the commit beat

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a11f9e-1ad9-710b-892d-d4950ef585ee` · branch `task/01a11f9e1ad9710b892dd4950ef585ee`
· sibling (already on the baseline): `01a11f96-9336-7d9f-9c88-a1403bc88926` (`devdoc/chat-turn-clock.md`)

## Completed

1. **`internal/agent/clock.go` — the budget clause rides the per-request stamp.**
   `(a *Agent) turnStampLine()` now appends `a.sessionBudgetLine()` verbatim to the
   turn stamp it already minted, in ONE `LogUser` call. The string is not re-rendered
   from the numbers: the commit return's own line is *read* and appended, so the request
   tag and the commit return can never become two spellings of one measurement
   (Rules 1b/13/14), and the escalation arm — elapsed, remaining, the mark, the one
   available action — arrives with it unchanged once the run is past
   `sessionBudgetWarnPercent`. The measurement stays `sessionBudgetStatus()`; the model
   is never asked to do the arithmetic.

2. **`internal/agent/loop.go` — the worker task main loop is wired to the same point.**
   `runEngine`'s request build now reads `a.requestMessages(a.KeepThinking)` where it
   previously read `a.Ctx.WireMessages(a.KeepThinking)`. That is the *only* injection
   point (`requestMessages` → `turnStampLine` → `Clock.TurnStamp()`), the one the chat
   paths (`chat_ops.go`, the `Agent.Chat` single-turn path) already used, so all three
   surfaces carry one shape and there is no second mechanism. `requestMessages` with a
   nil `Clock` returns exactly `WireMessages` — the reason `Agent{}` literals in the
   suite still send the bytes they always sent.

3. **`internal/agent/loop.go` — `sessionBudgetPrefix`** is the one declaration of the
   string that opens all three rendered budget faces (`sessionBudgetReportFmt`,
   `sessionBudgetEscalationFmt`, `sessionBudgetRefusalFmt`), replacing three copies of
   the literal (`"session budget: "`). The request-path tests ask "does this text carry
   the budget" through that constant instead of re-typing it (Rules 1/13/14).

4. **`internal/agent/sessionbudget_everyround_test.go`** — nine tests, all asserted
   against the REQUESTS a mock model actually receives (never a copy of the sentence):
   every round of a real `Agent.Run` carries the tag; adjacent rounds advance in both
   numbers (tick and percent); a round with NO tool call carries it; the tag is
   byte-`HasSuffix`-equal to the framework's own `sessionBudgetReportFmt` rendering;
   past the mark it escalates in the existing wording and is strictly longer; unlimited
   (`0`) and nil-clock degrade silently with no empty marker; the increment is O(1) in
   the history; the commit return, the unlimited commit shape and `withTick` are
   unchanged.

## Not Done

- **The escalation clause is not length-gated.** Past the mark the tag is 360 bytes
  (the whole line 409 against 94). The epic's constraint was "信息量增加且措辞来自既有
  常量族", which is met, but a run past the mark pays ~85 tokens per request rather than
  the ~12 the plain arm costs. Trimming the escalation sentence belongs to whoever owns
  that wording (`sessionBudgetEscalationFmt` is also the commit return's line), not to
  this task — changing it here would rewrite a string another surface's tests pin.
- **`Agent.Chat` (the plain single-turn chat) is covered only indirectly.** The chat
  evidence below is `gopi chat --tools` (`ChatOps`); the plain path shares
  `requestMessages` and has its own regression test from the sibling task, but this run
  did not drive `gopi chat` without `--tools` through a mock to re-measure its bytes.
- **No worker-row run.** The task-path evidence is a real `gopi` run (`Run`/`runEngine`,
  the surface the diff changed) driven by the live provider; it is not a claimed ledger
  row, so `gopi worker`'s own loop was not exercised end to end. The request shape is
  identical, since `runEngine` is what both call.

## Verification

```
Build:  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → OK (65,017,274 bytes)
Tests:  go test ./internal/agent/ -run 'TestSessionBudgetTag|TestSessionBudgetEveryRound' → ok 4.7s
        go test ./internal/agent/ -run 'TestSessionBudgetLine|TestCommitTaskItemShapeUnchangedWithoutBudgetLine|TestTurnStamp|TestChatTurnClock|TestSessionBudgetGate|TestReframeTimeDimension|TestAgent_WithTick' → ok 5.1s
        go test ./internal/agent/ -run 'TestSystemPromptDensity|TestSystemPromptKernel|TestChatTurnClock_PromptBlockIsChatOnly' → ok 4.0s
        make test-budget PKGS='./internal/agent ./cmd/agent' → see the make output in the run's own log
Ran:    # the task path (Run/runEngine — the surface the diff changed), real provider, sandbox profile
        SB=$(./bin/gopi tool sandbox create --label everyround --task-id 01a11f9e1ad9710b892dd4950ef585ee | tail -1)
        GOPI_HOME="$SB" GOPI_RAW_LOG=on GOPI_SESSION_BUDGET=30 \
          bash -c 'printf "Answer with exactly: TASKPATH-OK. Do not use any tool.\n" | ./bin/gopi'
        # the chat path over a REAL interval, budget 1 minute so the tag crosses the mark
        GOPI_HOME="$SB" GOPI_RAW_LOG=on GOPI_SESSION_BUDGET=1 \
          bash -c '{ printf "Reply with exactly: ONE-OK\n"; sleep 65; printf "Reply with exactly: TWO-OK\n"; sleep 3; } | ./bin/gopi chat --tools'
        # five chat turns, one per second, to measure the increment and its growth
        GOPI_HOME="$SB" GOPI_RAW_LOG=on GOPI_SESSION_BUDGET=30 \
          bash -c '{ for i in 1 2 3 4 5; do printf "Reply with exactly: TURN-%d-OK\n" $i; sleep 1.2; done; } | ./bin/gopi chat --tools'
Result: see "Raw-log evidence" below — the last user message of each of the 9 requests
        parsed out of the → REQUEST checkpoint line of the run's own raw log.
Commit: 211f7dcec feat(budget): carry the session-budget progress tag on every request, …
```

## Evidence (self-filed)

**Raw-log evidence (Rule 31, no commit anywhere in these runs).** Parsed from the last
`→ REQUEST` payload of each run's own raw log
(`<sandbox>/.gopi/sessions/<id>/<id>.raw.log`; the script is in this run's session
directory):

| run | request | messages | tool_results | last user line |
|---|---|---|---|---|
| chat, `GOPI_SESSION_BUDGET=1` | 1 | 1 | 0 | `[system] [t+0s] — first request of this session. session budget: 0s elapsed of 1m0s (0% used).` |
| | 2 | 3 | 0 | `[system] [t+64s] — +64s since your previous request. session budget: 1m5s elapsed, 0s remaining of 1m0s (108% used, past the 75% mark) — … task(action=reframe, verdict=TOO_BIG) with a roadmap.` |
| chat, 5 turns, `GOPI_SESSION_BUDGET=30` | 1..5 | 1,3,5,7,9 | 0 | ticks `0,1,2,3,4`; tag `0s…(0%)`, `2s…(0%)`, `2s…(0%)`, `4s…(0%)`, `5s…(0%)` |
| task path (`gopi` bare), `GOPI_SESSION_BUDGET=30` | 1 | 1 | 0 | `[system] [t+0s] — first request of this session. session budget: 1s elapsed of 30m0s (0% used).` |
| | 2 | 3 | 0 | `[system] [t+1s] — +1s since your previous request. session budget: 2s elapsed of 30m0s (0% used).` |

- **①** the tag is on EVERY request and increases: chat ticks `0→1→2→3→4`, percentages
  `0→0→0→0→0` on a 30-minute budget, and `0→108` when a 65-second interval crosses a
  1-minute budget — both numbers advance, on a run in which `task(action=commit)` was
  never called (the runs contain no tool call at all).
- **②** pure conversation rounds: `tool_results=0` on all nine requests, and the mock in
  `TestSessionBudgetEveryRound_PlainConversationRoundCarriesTheTag` fails if any request
  it served carried a tool_result — so the tag on a no-tool round is proven at the wire
  in both directions.
- **③** measured per-request increment: whole line **94 → 409 bytes** (+315) when it
  escalates; **95, 97, 97, 97, 97** across five turns — +2 on the first gap, then
  **+0, +0, +0** while the history grew 1 → 3 → 5 → 7 → 9 messages. The tag does not grow
  with the session; the only drift is the digits of the elapsed counter and the percent
  (`budgetTagMaxChars = 400` bounds the escalation arm, whose template fills to 365 at a
  1h0m0s budget).
- **④** the escalated tag is the existing clause, byte-identical: the request's line
  ends with `a.sessionBudgetLine()` (`HasSuffix`, pinned in
  `TestSessionBudgetTag_EscalatesPastTheMark`), i.e. the very string the commit return
  carries, with `sessionBudgetWarnPercent` and `taskverdict.TooBig` as its only sources.
- **⑤** regressions green: `TestSessionBudgetLine`,
  `TestCommitTaskItemReturnCarriesBudgetLine`,
  `TestCommitTaskItemShapeUnchangedWithoutBudgetLine`,
  `TestSessionBudgetGateUnlimitedAndNilClockByteIdentical`, `TestReframeTimeDimension`,
  `TestAgent_WithTick`, `TestChatTurnClock_*` (including the nil-clock and O(1) guards)
  — all pass; the unlimited path renders `""` and the commit return is untouched.
- **⑦** `git status --porcelain` in this worktree prints nothing (the commit carries the
  three files).

**Not measured here** (Rule 27 — no load was generated anywhere in this run): nothing in
this task needed a saturated resource, and no timing number above is used as a
performance claim; the two intervals (65 s, 1.2 s) exist so a counter has something to
advance by.

## Why the injection point is the request, not the tool result

The sibling task's measurement holds and is the reason this one is a one-line wiring
change rather than a second mechanism: `withTick` rides tool RESULTS, so a round that
calls nothing carries no time signal — and the budget had the same shape, riding only the
`task(action=commit)` return, which a run that investigates for twenty rounds never
reaches. Both signals are properties of *now* rather than of the conversation, so both
belong at the assembly point, appended as the last user message, and neither may be
stored in `Ctx.Messages` (a stored copy goes stale on the next request and accumulates
one per round forever — pinned by `TestSessionBudgetTag_IsTheFrameworksOwnMeasurement`).
