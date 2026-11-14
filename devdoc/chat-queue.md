<!-- doc-class: historical -->
# `/queue` — the chat REPL's own prompt queue

**Task:** `01a0e439-1e37-72cb-8ba5-19b45ebfd618`
**Status:** implemented, unit + end-to-end tested.

## The problem

`gopi chat` has ONE next-turn seam, and the **agent** owns it:
`internal/agent.Agent.ChatSurfaceTakePending` returns a single pending line that
the model typed for itself through the `chat_surface type` action, bounded by
`chatSurfaceLoopBound`. The REPL loop consumes it at the top of each iteration
(`cmd/agent/cli_chat.go`, `runChatWithInput`).

There was no seam for the **operator**. A prompt typed while a turn was in flight
had nowhere to go: it waited as a raw input line and nothing on screen said so,
and the operator had to hold the question in their head until the turn ended.

## The design — one seam, two sources, one consumer

`/queue` is a multi-slot FIFO the **REPL** owns (`cmd/agent/chat_queue.go`), and
the SAME loop consumes it at the SAME boundary:

1. the agent's pending line is taken **first** (`ChatSurfaceTakePending`) — the
   model's own turn keeps its precedence;
2. otherwise, a line the operator has **already typed** wins (`select` with a
   `default`);
3. otherwise, the queue hands the loop its next line;
4. otherwise, the loop blocks on `select { interruptCh, lineCh }` as before.

No second next-turn mechanism was invented, and no second bound either.

### Why the typed line wins over a parked one (measured, not stylistic)

The first draft popped the queue eagerly — before reading the next line. The
acceptance case for `/quit` with a pending item then **could not be written**: the
queue drained at the next loop iteration, so `chatQueueQuitNote` ("N parked
prompt(s) … will be DROPPED") was **unreachable**, and with it the operator's
ability to cancel a parked prompt at all. An honest sentence nothing can reach is
worse than no sentence. With the `default:` order, `/queue X` then `/quit` typed
while a turn is in flight ends the session with X still parked and the session
saying so.

It costs nothing the epic asked for: the parked prompt still becomes the next turn
with **no extra Enter** — it is taken the instant nothing has been typed.

### The two budgets are deliberately different

| seam | owner | bound | spent by |
|---|---|---|---|
| self-driven (`chat_surface type`) | the **agent** | `chatSurfaceLoopBound` (8) | `ChatSurfaceTakePending`, which the tool refuses past |
| `/queue` | the **operator** | `chatQueueBound` (8) | **nothing** |

A queued line spends no loop turn. Charging it to the agent's counter would spend
the model's budget on a person's keystroke, and would refuse that person a prompt
typed for themselves. The two constants hold the same value and are **not**
shared: one number serving both would let a change to either silently retune the
other.

## The contract

- `/queue <prompt>` parks a prompt (trailing/leading whitespace trimmed).
- bare `/queue` (or `/queue list`) lists the queue in order: a header naming
  **depth of bound**, then one row per prompt with its 1-based position.
- `/queue clear` empties it and names how many prompts were dropped.
- Every mutation writes a **note row** (`input.WriteNotice`) — `queued #N of M`,
  `queue cleared — N dropped`, the empty-queue sentence, the pop note. Nothing
  about the queue is inferred from what the next turn turns out to be.
- **Bounded.** The `(chatQueueBound+1)`-th add is REFUSED with a sentence naming
  the bound, the command, and the way out. Nothing accepted is ever dropped to
  make room — asserted by the unit case, which checks all 8 accepted prompts are
  still in order after the refusal.
- **Shells refuse, they do not pretend.** On a run whose input layer is not the
  interactive editor (piped stdin, `--no-editor`, one-shot) there is no
  loop-boundary seam, so `/queue` is refused by name
  (`chatQueueSeamRefusal`). Accepting a prompt nothing can consume is exactly the
  silent drop the feature exists to remove.
- `/quit` (and `/exit`, Ctrl+D, Ctrl+C — the ONE `leaveChat` closeout) **states**
  what happens to anything still parked, before the layout is taken down.

## Files

| file | what |
|---|---|
| `cmd/agent/chat_queue.go` | the type, its vocabulary/bound/notes constants, the seam predicate, the usage block |
| `cmd/agent/chat_queue_test.go` | the FIFO contract: order, bound + refusal naming, empty-prompt refusal, clear, list rendering, seam predicate |
| `cmd/agent/cli_chat.go` | `chatSession.queue`, the `/queue` dispatch case, the loop's ordered boundary, `chatQueueAnnounceDrop` in `leaveChat`, the `/help` block |
| `cmd/agent/commands.go` | `chatNativeUsage` answers `/help queue` from the same const block |
| `cmd/agent/lineedit_wire.go` | the command-menu hint and the completion vocabulary |
| `testsuite/chat_queue_pty_test.go` | the three acceptance facts end to end on a real pty + the bound |

## The end-to-end case, and why it must be a pty

`/queue`'s value is a **loop** behaviour, so a unit test cannot observe it. The
case drives the real binary on a real pty, and makes the turn-in-flight state
**deterministic rather than raced**: the mock holds request 1 open mid-stream
(`testutil.Resp{Hold: true}`), so the turn really is running while the `/queue`
lines arrive.

The ground truth is the **request bodies**, not the screen: request 2 must carry
the parked prompt. A screen-only reading cannot tell "the queue ran as a turn"
from "my text was echoed back at me".

## Not done (honest)

- No persistence: the queue is in-memory REPL state and does **not** survive the
  session. That is stated by `/quit`, not hidden — but a session that dies
  (SIGKILL) says nothing, and cannot.
- The queue count is **not** added to the status row. The epic allowed it "if it
  costs one field"; the status row's field set is a recently-reworked constants
  area, and a queue note already lands on every mutation, so a second surface
  would have been scope for no new information.
- `gopi chat --once` / the cooked path cannot queue; they refuse by name (by
  design, not by omission).

## Verification

- Build: `go build ./...` — OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK.
- Unit: `go test ./cmd/agent/ -run TestChatQueue -count=1` — `ok github.com/nick/gopi/cmd/agent`.
- End to end (real binary on a real pty, `GOPI_BIN=bin/gopi` resolver):
  `go test ./testsuite/ -run TestChatQueue -count=1 -v -timeout 900s` —
  `PASS ok github.com/nick/gopi/testsuite 88.134s`, all three cases green.

### Four defects this feature's own tests found (all fixed in this branch)

Each one is the same shape — a parked prompt could be silently lost, which is the
single outcome the epic forbids — and none was visible without the end-to-end
case:

1. **The eager pop.** Taking the queue before reading a typed line made
   `/quit`-with-a-pending-item unreachable (no sentence could describe an
   already-drained queue). Fixed by ordering the boundary: typed line, then
   queue, then block.
2. **The discarded take.** `if queued, ok := Take(); ok && wasTurn` **evaluates**
   `Take()` — which MUTATES — and then throws the value away. Measured by the
   bound case: `/queue fill-01` … `/queue fill-02` typed in a row left the queue
   one prompt short each time, so the operator read `queued #1 of 1` and the
   ceiling was unreachable. Fixed by skipping the take entirely when the
   iteration did not follow a turn.
3. **The stray `continue`.** The boundary's `continue` discarded the line it had
   just selected: the park and pop notes appeared (the reader path worked) while
   the parked prompt never ran. Fixed by branching — the boundary path continues,
   the non-boundary path blocks for input.
4. **The reader parking at an idle prompt.** The reader goroutine is the only
   place a line is seen mid-turn, but it was parking `/queue` lines at an idle
   prompt too, where the loop was about to dispatch them itself — the poll
   case filled to 1 and stopped. Fixed by gating the reader on `turnRunning`.

### Design note the code carries

The reader goroutine may not write to the console — under the reserved layout the
editor's read loop is the only writer (`internal/lineedit/region.go`), and a
second writer tears the layout. So the reader **records** the park's note row and
the loop **replays** it at the turn boundary, which is why `/queue <prompt>` is
the only form the reader answers: bare `/queue`, `list` and `clear` all answer
with a console write, so they are handed back to the loop.
