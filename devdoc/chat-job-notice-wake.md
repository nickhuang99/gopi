<!-- doc-class: historical -->
# A Job Notice Reaching an Idle Prompt Mints a New Turn

**Epic:** `01a0eeea` (flagship 5). **Branch:** `task/01a0eeea9329708bbd27ef72f1e805d6`.
**Commit:** `b31913bb`.

## The defect, stated as a cycle

`tools.TakeJobNotices()` — the inbox that carries "background job finished" — has
exactly ONE take point: the chat turn's round boundary in
`internal/agent/chat_ops.go`, under the comment *"background-job completion
notices enter the inbox BEFORE the request is built"*. That boundary runs inside
a turn. A turn started when the operator typed a line. The REPL's idle wait was

```go
select {
case <-interruptCh:
case lr = <-lineCh:
}
```

So a notice could only ever be delivered to a turn that was already running for
some other reason. A job whose completion nobody was waiting on could not come
back at all: the console parked on `lineCh` and nothing but a keystroke could
release it. The cycle is the defect — not a missing feature.

## What was added, and what was deliberately NOT

| piece | file | why it is the smallest thing that works |
|---|---|---|
| LEVEL + EDGE | `internal/tools/job_notice_wake.go` | `JobNoticePending()` asks the inbox itself ("is a take going to return something?"); `JobNoticeWake()` is a cap-1 channel the reaper signals after the append. The channel carries **no payload** — a payload would be a second take point. |
| the third arm | `cmd/agent/cli_chat.go` (both idle selects) | `case <-tools.JobNoticeWake()`, then the LEVEL decides. Asked **before** the park too, so a notice that arrived earlier still becomes a turn. |
| the mint | `cmd/agent/cli_chat.go` (turn boundary) + `cmd/agent/chat_notice_turn.go` | the read carries `noticeTurn` and **no text**; the message is composed by the agent's own `agent.ChatJobNoticeOpenLine`, so the carrier never decides what a notice says. |
| the take point | unchanged — `internal/agent/chat_ops.go` | the carrier **peeks** (`tools.PeekJobNotices`) to name the jobs; the round boundary of the very turn it opened injects them, exactly as before. |

**Reuse:** no new top-level tool, no new script, no state file, no long-lived
dependency (Rule 35). The wake source is the in-process inbox that already
existed; the only new object is one package-global channel, created by the
package's own initialisation, never written to disk, needing no cleanup.

**The one message formatter.** `jobNoticeInjectionMessage` (the round boundary's
injection) and `ChatJobNoticeOpenLine` (the turn's opening line) now share
`jobNoticeFact`, so there is ONE statement of what a notice says. The injection's
bytes are unchanged.

## The busy case — an explicit A/B decision

**Option A (rejected): queue the notice into the running turn.** It would place a
notice between a tool result and the assistant message, or rewrite the turn in
flight — the two things the epic forbids outright ("a notice may only appear as a
new turn while idle; never between a tool result and an assistant message; never
rewrite past context"). It also has no consumer: a notice carries no text the
model must answer, so a turn opened mid-conversation would have no input to be
about.

**Option B (chosen): the notice waits in the inbox and becomes the NEXT turn.**
Nothing is dropped (the level stays true and the slice keeps every notice until
it is drained — the take point is unchanged), nothing is rewritten, and the
notice becomes the next turn's reason the moment the current turn ends, because
`wasTurn` is then set and the level is still true. This is the behaviour the
system already had for a notice arriving during a turn; the epic did not change
it, and the guard below pins it.

## Tests, each proven RED on the half it guards

| test | file | proves | red-proof |
|---|---|---|---|
| `TestJobNoticeWake_LevelAndEdgeAgreeWithTheInbox` | `internal/tools/job_notice_wake_test.go` | the edge fires on append, the level agrees, and **peek does not take** | removed the `NotifyJobNoticeWaiters()` call → FAIL (10.0s), restored → PASS |
| `TestJobNoticeWake_ProducesWithNoReaderAndNeverBlocks` | same | 3 jobs with **no reader**: the non-blocking send never parks a producer, and a token survives | same removal → FAIL (10.4s), restored → PASS |
| `TestChatNoticeTurn_NoticeDuringATurnDoesNotInterruptOrRewrite` | `cmd/agent/chat_notice_turn_busy_test.go` | with the first request HELD, the notice lands mid-turn: no second request (turn not abandoned, no in-flight turn), roles strictly alternate, the operator's line appears exactly once, byte-identical | `chatNoticeTurnOwed() → false` → FAIL (32.9s), restored → PASS (0.57s) |
| `TestChatNoticeTurn_JobCompletionOpensATurnWithNoInput` | `testsuite/chat_notice_turn_pty_test.go` | real pty, real binary, ONE typed line: a turn opens by itself, the model is asked a question nobody typed | `chatNoticeTurnOwed() → false` → FAIL (71.2s, timed out waiting for the announcement), restored → PASS |
| `TestChatNoticeTurn_WakesAParkedConsole` | same | the **EDGE** specifically: the job finishes while the console is already parked (its command sleeps first) | removed the edge signal → FAIL (56.1s), restored → PASS |

The first four red-proofs kill the two halves of the primitive (level/edge) and
the wiring; the last one is why the edge cannot be dropped even though the level
alone satisfies `TestChatNoticeTurn_JobCompletionOpensATurnWithNoInput` — in that
case the job finishes while its own turn still runs, so the loop reaches its next
idle check with the level already true.

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` → exit 0, no output.
- **Vet:** `go vet ./cmd/agent/ ./internal/tools/ ./internal/agent/ ./internal/testutil/ ./testsuite/` → exit 0.
- **Tests (branch-scoped smoke):** `go test ./internal/tools/ ./internal/agent/ ./cmd/agent/ ./testsuite/ -count=1` — see `## Evidence`.
- **Ran:** `/home/user/.gopi/sessions/01a0eeea-9329-708b-bd27-ef72f1e805d6/chat_notice_turn_run.py` — the script drives the REAL `bin/gopi chat --tools` on a REAL pty (typed key by key, 40ms apart) against a mock model server, with the session home in `run-home/`.
- **Result (the headline evidence, wall-clock from the pty transcript `chat-notice-turn.stamps.txt`):**

```
# OPERATOR KEYSTROKES (the only bytes a human sent)
[t+  3.20s] KEY <typed start the slow job>
[t+  4.12s] KEY <Enter>
[t+ 22.26s] KEY <typed /quit>
[t+ 22.66s] KEY <Enter>

[t+  8.40s] OUT [gopi] job-1 — a background job finished; opening a turn to report it (no input
[t+  8.55s] OUT iter 2   ← the console is running the turn it opened for itself
```

**Fourteen seconds separate the last operator keystroke (`Enter`, t+4.12s) from
the console opening a turn of its own (t+8.40s), and the next keystroke is
t+22.26s.** Model requests: 3 — `notice=False / notice=False / notice=True`, i.e.
only the third request carries the agent's notice message.

The session record (`run-home/.gopi/sessions/c7162c7f-…/session.jsonl`) reads:

```
user      | start the slow job
assistant | [tool_use job {"action":"start", ...}]
tool_result | job started: job-1 ...
assistant | I started the job; its completion will be reported when it lands. ...
user      | [system] Background job job-1 finished: status=exited code=0 (command: sleep 3; ...)
assistant | ...
SEQ: user -> assistant -> tool_result -> assistant -> user -> assistant
```

Roles strictly alternate across the whole conversation, the first user message is
still `start the slow job` byte-for-byte, and the notice is the ONE new user
message — it did not rewrite or duplicate anything.

- **`git status --porcelain`:** empty in the worktree after the commit (see `## Evidence`).
- **Commit:** `b31913bb` — `feat(chat): a job notice reaching an idle prompt mints a new turn`.

## Evidence (self-filed)

- `go build ./...` → exit 0, no output.
- `go test ./internal/tools/ -run TestJobNoticeWake -count=1` → `ok github.com/nick/gopi/internal/tools 0.513s`; after deleting the edge signal → `--- FAIL: TestJobNoticeWake_LevelAndEdgeAgreeWithTheInbox (10.01s) job_notice_wake_test.go:38: no wake after the job finished` and `--- FAIL: TestJobNoticeWake_ProducesWithNoReaderAndNeverBlocks (10.44s)`; restored → `ok … 2.747s`.
- `go test ./cmd/agent/ -run TestChatNoticeTurn_NoticeDuringATurnDoesNotInterruptOrRewrite -count=1` → `ok … 0.569s`; with `chatNoticeTurnOwed() → false` → `--- FAIL … the model served 1 request(s), want 2 within 30s` (32.936s); restored → PASS.
- `go test ./testsuite/ -run TestChatNoticeTurn -count=1` → `ok … 27.750s`; with `chatNoticeTurnOwed() → false` → `--- FAIL: TestChatNoticeTurn_JobCompletionOpensATurnWithNoInput (71.15s) timed out waiting for "opening a turn to report it"`; with the edge signal removed → `--- FAIL: TestChatNoticeTurn_WakesAParkedConsole (56.09s) timed out waiting for "opening a turn to report it"`; restored → PASS.
- Real run: `python3 …/chat_notice_turn_run.py <session-dir> <worktree>` → `model requests: 3`, `request 3: notice=True operator_line=True`, `exit code: 0`, `screen contains b'opening a turn to report it': True`.

## Not Done (honest)

- The **interrupt path** (`Ctrl+C` while parked) was not given a notice-specific
  case: the arm is unchanged and the interrupt arm still precedes it in the same
  `select`, so the behaviour is the one that already existed. Not verified by a
  test here.
- **`--no-editor` / cooked-stdin sessions** are a different reader with no idle
  park; the notice therefore opens no turn there by construction. This is stated,
  not tested — a cooked session consumes one line per push and exits at EOF.
- The wake is **process-local**: a notice raised by another gopi process on the
  same host does not signal this channel. Nothing in this epic asked for that,
  and the inbox is in-process by construction.
