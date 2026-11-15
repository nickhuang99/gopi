<!-- doc-class: normative -->
# `gopi notify --session` delivered an IDLE-wake message TWICE

**Task:** `01a12b21-c5b5-7b81-a355-8fd6fc88d611` (branch
`task/01a12b21-c5b5-7b81-a355-8fd6fc88d611`).
**Symptom reported:** one task event, delivered by the configured `notify.cmd`,
appeared in a chat console as **two identical messages**, while the anchor file
`<profile>/notify/task-events.jsonl` recorded the event **once** — so the sink
script ran once and the duplication was downstream of it.

## Root cause — the round boundary restated the turn's own opening line

The duplicate was in the **agent's round boundary**, not in `notify.sh`, not in
`gopi notify`, and not in the inbox.

`internal/tools/job.go`'s queue (`theJobs.notices`) is the ONE take point for
both kinds of notice: a background-job completion, and an external message
delivered by `gopi notify --session <id> --message <text>`. A console parked at
its prompt mints its next turn **from that queue directly**
(`cmd/agent/cli_chat.go`, the idle-wake arm), and that turn's **opening message
is the notice statement itself** — `agent.ChatJobNoticeOpenLine`, which for an
external message returns `sessionMessageFact` verbatim. So the statement was the
turn's user message, written to the session record by `ChatOps`.

That turn's **own round boundary** then ran, merged the same delivery (the
delivered marker is written at that boundary, `SessionInboxLoad`), drained the
queue with `tools.TakeJobNotices()` and injected **the identical sentence** as a
second user message (`injectJobNotice` → `jobNoticeInjectionMessage`, which for
an external message is `sessionMessageFact` with no tail).

Measured, on HEAD, before any fix — the session record of a real pty console:

```
$ go test ./cmd/agent/ -run TestNotifyDeliverStatesTheMessageOnceInTheSession -count=1
    cli_notify_duplicate_test.go:165: the session record carries 2 user message(s)
    with the delivered text, want exactly 1 — the operator would read this
    delivery 2 time(s)
```

and at the seam (a turn whose opening line IS the notice):

```
$ go test ./internal/agent/ -run TestSessionMessageIsInjectedOnceWhenTheTurnOpensWithIt -count=1
    session_message_notice_once_test.go:110: the request states the delivery
    2 time(s), want exactly 1
```

**Why it looked like "notify.sh ran twice".** The anchor is written by
`notify.sh` step 4, the delivery by step 3, and the two messages are byte-
identical, so the natural reading is a duplicated delivery. The anchor was
right: ONE delivery, two statements — one by the wake arm, one by the boundary.

## The fix — the boundary injects what the conversation does not already say

`internal/agent/chat_job_notice_turn.go` gains `noticesToInject(pending)`: it
keeps only the notices whose **injection message** the conversation does not
already carry, matched by containment over the user-message texts of the very
slice the request is built from (`Context.GetMessages()`).

Both boundaries (`internal/agent/chat_ops.go` — the chat turn; `loop.go` — the
task loop **and** the no-tools chat turn) now call
`a.noticesToInject(tools.TakeJobNotices())`.

**The take point is unchanged, and that is deliberate.**
`TestSessionInboxLoadHasOneTakePoint` (internal/agent/session_inbox_test.go)
pins the `TakeJobNotices(` call sites to `chat_ops.go` and `loop.go`; the fix
therefore filters at the boundary rather than draining somewhere else. The first
draft put the drain in a helper and was refused by that gate — the gate was
right, and the shipped shape keeps one queue, one take point, one decision.

**Why the test is the CONVERSATION and not the notice's kind.** The two kinds of
notice are not injected the same way: a job's injection is
`fact + " Call job(action=output, …)"` while its opener is the bare fact, so for
a job the boundary's sentence is NOT in the history and is still injected
(unchanged behaviour, and it is what keeps the model from re-running the job).
For an external message the injection is the fact alone — identical to the
opener. Matching on the injection message therefore draws the line exactly where
the duplication is, and needs no second rule.

This also keeps the **mid-turn** delivery working: a message that arrives while
the console is turning has no opener and no history row, so it is injected
exactly as before. A kind-based rule ("never inject external messages") would
have silently dropped it — the withhold would cover a delivery nothing ever
stated.

**Nothing is lost.** The withheld notice's text IS in the conversation (the
message that stated it) and its durable row is in the session record; the
delivered marker is written by `SessionInboxLoad` at the same boundary, so the
message still counts as taken and the sender is not told otherwise.

## Was the duplicate also there under the older `cat >>` configuration?

**Yes — the duplicate never involved the sink at all.** The two statements are
produced by the chat process, from its own queue, after `notify.cmd` has already
exited: the sink's only job is to call `gopi notify --session … --message …`
once, which it does (the anchor proves it). `notify.cmd` = `cat >> …` (the
operator's current settings) simply means **no chat delivery happens at all** —
the event is only anchored, and nothing is stated twice because nothing is
stated once. The duplicate therefore appears exactly when a chat console is
registered in the profile's slot registry, i.e. **whenever the sample sink
delivers** — and that is independent of which `notify.cmd` spelling is
configured.

## Verification

- Build: `go build ./...` — clean.
- Red → green, end to end: `go test ./cmd/agent/ -run TestNotifyDeliverStatesTheMessageOnceInTheSession`
  → **2 user rows** on the pre-fix tree, **1** after.
- Red → green, at the seam: `go test ./internal/agent/ -run TestSessionMessageIsInjectedOnceWhenTheTurnOpensWithIt`
  → statement count **2** before, **1** after.
- The neighbouring acceptance still passes (the delivery must still ARRIVE):
  `go test ./cmd/agent/ -run TestNotifyDeliverWakesAnIdleChatSessionWithNoInput`
  → ok.
- Budgeted suites: see the task report.

## Pre-existing red, recorded and NOT caused by this change

`testsuite/chat_notice_turn_pty_test.go` (both cases) is **red on HEAD**, before
any edit in this branch — measured by restoring the four files from `HEAD` and
re-running:

```
--- FAIL: TestChatNoticeTurn_JobCompletionOpensATurnWithNoInput
    session recorded 6 user turn(s) [..., "[system] Background job job-1
    finished: …", "[system] Background job job-1 finished: … Call
    job(action=output, …)", …], want exactly 2
```

The six rows are: the operator's line, two clock rows (`[system] [t+…s] — …`,
from the per-request session tick, `internal/agent/clock.go`), the **job-notice
opener**, the boundary's job injection — which for a JOB is `fact + instruction`
by design, so it is not a duplicate of the opener and the case's "want exactly
2" is a stale expectation of an older request shape — and one more clock row.
The case was failing before this task; it is left as it is (Rule 30/31 honesty)
rather than adjusted inside a notify bug fix, and it is reported here so the
next reader does not read it as damage from this change.

## Files

- `internal/agent/chat_job_notice_turn.go` — `noticesToInject`,
  `statedContains`, `conversationUserTexts`, and the reason on
  `ChatJobNoticeOpenLine` / `TakeJobNotices`.
- `internal/agent/chat_ops.go`, `internal/agent/loop.go` — the two boundaries
  (three call sites) filter what they inject.
- `internal/tools/job.go` — `TakeJobNotices`' doc states that the filter is the
  caller's decision.
- `internal/agent/session_message_notice_once_test.go` (new) — the seam-level
  regression case (red before, green after).
- `cmd/agent/cli_notify_duplicate_test.go` (new) — the two-process acceptance:
  one delivery, one user row, one request.
