<!-- doc-class: historical -->
# Per-turn Ctrl+C in `gopi chat` (interrupt the TURN, keep the session)

Task `01a0e4390ef0710ba9cfd9385b612172` · branch `task/01a0e4390ef0710ba9cfd9385b612172` ·
base master `f9b8cd6c` · verdict at briefing time: **DOABLE**.

## 1. The defect, as measured (not as described)

Three separate mechanisms each read Ctrl+C as "end the session", so an answer that
was streaming when the operator pressed the key could not be stopped at all:

| layer | what it did | why it could not stop a turn |
|---|---|---|
| line discipline | raw mode clears `ISIG` | Ctrl+C never becomes a SIGNAL — it arrives as the `0x03` **byte** |
| `internal/lineedit` key loop (`editor.go`, `KeyInterrupt`) | returned `Result{Interrupted: true}` | `cmd/agent/lineedit_wire.go` treats `res.EOF \|\| res.Interrupted` as terminal → the REPL exits |
| cooked path | `ag.Interrupt()` (session-scoped, process-wide, cancel-once) | it means "finalize and leave", and it is never re-armed |

So the feature is not "add a key handler": it is **one hardware key with two
meanings**, decided by the only party that knows whether a turn is in flight — the
REPL. A raw-mode terminal delivers the key, a cooked one (piped stdin, `--no-editor`,
or a terminal that could not enter raw mode) delivers a real `SIGINT`; both routes
have to reach the same decision.

## 2. Design: one seam, one claim, two transports

```
              ┌── 0x03 byte ──▶ Editor key loop ──▶ Editor.InterruptTurn() ─┐
Ctrl+C ───────┤                                                            ├──▶ claimTurnInterrupt()
              └── SIGINT ─────▶ Terminal.CaptureSignals / REPL watcher ────┘
```

* **`cmd/agent/lineedit_wire.go` — `chatInput.SetInterrupt(active, interrupt)`** installs
  the seam on BOTH transports in one call: `Editor.SetMinTurnActive` +
  `Editor.SetInterruptTurn` for the byte, `Terminal.SetInterruptHandler` for the
  signal. The `interrupt` closure is the same function object on both routes, so the
  two can never drift.
* **`cmd/agent/cli_chat.go` — `claimTurnInterrupt`** is the ONE decision point. It
  latches (`turnClaimed`, one claim per turn): on a terminal with `ISIG` on, one
  keystroke can arrive through both transports, and a second cancel would land on the
  context of the turn that started after the first — abandoning a turn the operator
  never asked to stop.
* **`beginTurn` / `endTurn`** bracket the window in which the key means "abandon the
  turn". The REPL states it; it is never derived from the agent, because "the agent
  has a run context" is true for a moment on either side of the turn.
* **`interruptTurn` returning false is NOT swallowed** (Rule 15): the editor then
  falls back to the prompt meaning, so a Ctrl+C that arrives as the turn ends still
  does something.

### The signal arm refuses what it does not own

`SIGTERM` also reaches the REPL's watcher. It is **not** a turn interrupt: its meaning
is "terminate this process" and nothing about it says which turn the operator was
reading. Only `sig == os.Interrupt` claims a turn; the closeout path is unchanged. The
watcher also loops, because a claimed interrupt ends a TURN and not the watcher —
without the loop the first Ctrl+C consumed the registration's only delivery and the
operator's next Ctrl+C at the prompt did nothing.

## 3. The agent side: a truncated answer is not an answer

`internal/agent` already had the run-scoped primitive (`InterruptRun`). Two things
were missing, and the second was found by the interrupt test itself:

1. **`errTurnInterrupted` / `IsTurnInterrupted`** — a sentinel distinct from
   `interruptErr`, so a carrier can tell "abandon this turn" from "end the session"
   without matching error text. `Chat`/`ChatOps` check it **before** the error and
   **unconditionally**.
2. **A cancelled SSE stream returns its partial text with a `NIL` error.** Measured:
   `Chat` returned `<nil>` after the run was cancelled, so an interrupt checked only
   on the error path was invisible in exactly the case it exists for — the operator
   stops the answer and the run reports a *triumphant completed turn* whose text stops
   mid-sentence. That truncated text would then be logged as the assistant's message,
   counted as a turn, and seed the next turn's context.

The latch is per-RUN, not per-process: `runInterrupted` is set by `InterruptRun` under
`runMu` (before the cancel) and cleared by `runScopedRoot` when the next run's scope
opens. It is deliberately NOT the `interrupted` flag `Interrupt()` sets — that one
means "leave the session" and its lifetime is the process.

## 4. What the operator sees, and what the file says

* **Banner** (`cmd/agent/chat_stream.go`): `chatBannerLine` = `chatBannerCtrlC + ", " +
  chatBannerExit` — the key's new meaning and its old one in one sentence, from one
  place.
* **Marker** (`chatInterruptMarker`): `[gopi] turn interrupted by ctrl-c (operator, N
  chars shown) — the session is still open; type the next prompt, /quit to exit.` It
  is written through the layout's own `WriteNotice`, not a bare `Fprintf`: with the
  reserved band installed, a flowed stderr write lands UNDER the band and the next
  repaint erases it. Order matters too — the partial answer is on screen first, and the
  settled screen dump CONTAINS the marker.
* **JSONL** (`internal/sessionevent` `turn_interrupted`): `reason`, `source`,
  `partial_chars`, written by `SessionLogger.LogTurnInterrupted` and flushed
  immediately (`Sync`), because the next event this session writes may be many turns
  away and a witness buffered until then would be missing from exactly the crash dump
  an operator opens after a hard kill.
* An interrupted turn **registers nothing**: `RegisterChatSession` /
  `RecordChatTurn` would bill a truncated turn as a whole one.

## 5. What deliberately did NOT change

* Ctrl+C **at the prompt** exits, exactly as before (`Result.Interrupted`), and a
  caller with no seam installed keeps the historic behaviour byte-for-byte.
* `/quit`, `Ctrl+D`, and the `session_end` contract.
* `SIGTERM` mid-turn: still the graceful closeout, still `session_end`.

## 6. Gate consequences (Rule 36 / doc anchors)

`internal/agent/docanchor_test.go` went red for 13 anchors in OTHER people's landed
records — my edits grew `internal/agent/loop.go` (+178), `cmd/agent/cli_chat.go` (+170)
and `internal/agent/session.go` (+19), so a doc's line reference landed on a blank
line or past EOF. Two rules decided the treatment:

* Where the doc's line was a **pure move** (the text at the old line is now at the new
  one), the reference was re-keyed, text-verified line by line — 4 anchors in
  `devdoc/cli-as-eyes-and-hands-feasibility.md`, `devdoc/requirements/99-summary.md`
  and `devdoc/rsi-chat-files-a-row-recipe.md`.
* Where the doc is a **dated measurement** (`look-without-fixing-bound.md` publishes
  its own drift table; `cov-agent-cr.md` and `chat-status-row-partition.md` carry
  historical numbers), re-keying would present an earlier measurement as today's, so
  the anchor is **recorded** in `internal/agent/testdata/docanchor-baseline.tsv` with
  this task's cause (17 rows), and the two permits my shifts made stale
  (`loop.go:2938`, `answer.go:1158`) were deleted from it.

Three anchors that the gate lists as pre-existing drift are **not** folded into my
permit list (their files are untouched by this diff): `palette.go:500`,
`memory_provenance_latent.go:143`, `task_verify_closure.go:345`. Current state: **0
new, 0 stale permits.**

## 7. Not Done (honest)

* **`gopi chat --turns` (the pipe-back driver) is not exercised by the new pty test.**
  Its sink (`chatOnceSink`, `chat_once.go`) receives `(result, runErr)`; the interrupt
  arm sets `runErr = nil` and does not call the sink, so the driver sees no finished
  turn it did not receive. I verified the code path by reading it and by the existing
  suite, not by running a `--turns` session with an interrupt — that is a gap.
* **No test drives the SIGTERM-mid-turn arm of the new `sig == os.Interrupt`
  condition.** `testsuite/term_once_signal_test.go` covers SIGTERM for the one-shot
  carrier and passes, but the chat-mode SIGTERM-mid-turn claim is guarded by the
  condition, not by an assertion of its own.
* **No coverage of the cooked-input (real SIGINT) interrupt route** in a pty test: the
  byte route is what a raw-mode terminal produces, and that is what the test drives.
  The signal route shares `claimTurnInterrupt`, which the unit tests cover.
