<!-- doc-class: normative -->
# The flow seam PUBLISHES its band row — the read loop is the model's only reader

**Landed:** task 01a0e608, branch `task/01a0e608d67c7dc7b2f575365a14ff13`.
**Code:** `internal/lineedit/editor.go` (`bandFrame`, `queueBand`, `bandPending`,
`drainBand`, `drainBandFrames`, `WriteLines`, `WriteBandRow`), `bandgate.go`
(the read hook), `value.go` (`Text` reports the read),
`bandwriter_foreign_test.go` (the guard).

## The defect, measured before it was fixed

`go test -race ./cmd/agent/ -run 'TestChatSurfaceKey_' -count=1` on the
pre-fix tree — **26 `WARNING: DATA RACE` blocks in ONE run**, every one of them
the same pair:

```
WARNING: DATA RACE
Write at 0x00c0001202d0 by goroutine 27:
  lineedit.(*Value).Reset()            value.go:101
  lineedit.(*Editor).Run()             editor.go:895
  cmd/agent.(*chatInput).ReadLine()    lineedit_wire.go:165   <- the read loop
Previous read at 0x00c0001202d0 by goroutine 19:
  lineedit.(*Value).Text()             value.go:79
  lineedit.(*Value).RowsFor()          value.go:394
  lineedit.(*Editor).repaint()         editor.go:1389
  lineedit.(*Editor).WriteLines()      editor.go:470
  cmd/agent.(*chatInput).WriteLines()  lineedit_wire.go:347   <- the RUN LOOP
```

The seam was reached from the REPL's main goroutine (the turn's goroutine, and
the `/help` panel's own writer) while `Run` — on the reader goroutine started at
`cli_chat.go:2670` — did `e.val.Reset()` at the start of the next line.
`WriteBandRow` was the same defect through the flow seam's own reader goroutine:
it wrote the band AND called `e.repaint()`, and `repaint` reads
`clampedMenu → Value.RowsFor → Value.Text`.

## The invariant that was restored (not a lock)

The package already stated it: **the read loop is the layout's only writer and
the editing model's only reader.** The publish road existed to make it true for
the status row and for `Note` (`PublishStatus` raises a level, wakes the loop,
the loop draws) — the flow seam simply had not been moved onto it.

So the fix is routing, not locking:

- A foreign producer QUEUES a frame (`bandFrame{text, oneRow}`) — **data only**:
  no terminal byte, no `Value`, no region. `queueBand` raises the level
  (`bandActive`) and wakes the loop.
- The queue is guarded by `frameMu`, which protects **the queue and nothing
  else**. `Value`, the renderer and the region are still touched by one
  goroutine, which is why no mutex sits around the model — a lock there would
  have HIDDEN the race rather than removed it (Rule 15).
- The read loop drains it: `drainBand` writes each frame through
  `region.WriteLines` (the region's own two policies — a panel's trailing-break
  drop vs a single already-split flow row — are what `oneRow` selects), then
  `repaint()` draws the chrome the promoted band row displaced.
- The drain happens **inside `repaint`, after `render.Redraw` + `paintMenu`**,
  so the band row lands on the layout the chrome was just drawn for, and covers
  the one repaint that reads the model.

### Liveness: the level, not the notification

`bandPending()` is re-derived in **every** read outcome that can draw — the
`EventWake` arm, the `EventNone`/completion-poll arm, the idle-timeout tail, and
the post-keystroke flush gate. That is deliberate and is the lesson the package
already paid for once (`clampedMenu`'s own comment: the loop can be parked in an
UNBOUNDED read when a notification lands — measured `wakes=0`, every read
timeout `-1`). A queued frame is therefore drawn on the loop's next own wake at
worst: **late, never lost**.

### The fallback when no wake channel exists — announced

`queueBand` counts a frame queued with no enabled seam
(`Editor.BandNotifyMisses()`), so a caller can say "this row waited for the
loop's next wake" instead of leaving it invisible. Nothing is silent: production
installs the terminal's wake pipe, and a source without one is already announced
by `NotifyEnabled`/`StatusNotifyEnabled` at the REPL.

## What was rejected, and why

1. **A mutex around `Value`.** It removes the -race report while leaving two
   goroutines interleaving on the model — the lock would have to be held across a
   whole repaint (region write included), i.e. the terminal write and the model
   read would serialise anyway. The invariant is single-ownership; a lock is the
   opposite of it.
2. **A synchronous `WaitBandWrite` seam (queue + block until drawn).** Built
   first, and it DEADLOCKED: `WriteLines` is also reached FROM the read loop's
   own goroutine (the submit path flushes its row while the loop reads the screen
   back), and the loop is the only drainer. `Editor.Note` already answers this
   question — **queue and return** — so the seam follows it. A caller that needs
   the row on screen waits with its own bounded wait (`waitDraw` in
   `cmd/agent/chat_surface_*`), which is where the assertion's subject lives.
3. **A fixed "wait always" or "never wait" terminator.** Removed with (2); one
   contract (`true` = the layout owns the screen and will draw it) is what the
   callers' `bool` already meant.

## The guard: deterministic, no `-race`, no sampling

`bandwriter_foreign_test.go` pins the invariant in the invariant's own terms:

1. `TestBandWriter_ForeignGoroutineDoesNotReadTheEditingModel` — the read loop is
   parked on the source's gate (so no loop-side read can fire the sentinel), a
   hook on `Value.Text` is armed, and `WriteLines` is called from a foreign
   goroutine. The call must return **and the sentinel must stay at zero**; the
   row must not be on the wire yet; and after the gate is released the loop must
   write it. **Red-proof:** restore `WriteLines`' pre-fix body
   (`e.region.WriteLines(text); e.repaint()`) and the same test fails —
   measured `the foreign WriteLines read the editing model 4 time(s)`.
2. `TestBandWriter_TheReadLoopWritesTheRow` — a write from the loop's own
   goroutine must return without blocking (the deadlock (2) above), the level
   must be raised, and the loop must be the one that writes the row.
3. `TestBandWriter_AWriteWithNoWakeSeamIsStillDrawn` — with the seam removed
   after the park, the row is still written on the loop's next own wake and the
   miss is COUNTED.
4. `TestBandWriter_GuardIsDeterministic` — the sequence repeated 5×: the barrier
   is a gate, the observable is a count, and a guard that flaked would be the
   thing that gets switched off.

`Value.Text` is the hook's site because it is the read the pre-fix stack shows at
the top (`Text → RowsFor → repaint`). Calling a hook from a read costs one nil
check in production, the same shape as `parkgate.go`'s acknowledge-window hook.

## The framing terminator: WHO waits for the draw (`bandwait.go`)

A frame is written by the read loop, and the seam is reached from two kinds of
caller, with OPPOSITE requirements:

- **A foreign goroutine** (the REPL's main goroutine, the `/help` panel's writer,
  cmd/agent's panel harness) keeps the historic contract: **the row is on the
  screen when the call returns.** Measured: dropping the wait made
  `TestChatHelpPanel_RendersInTheBandNotOverTheChrome` fail (`the caller's text
  never reached the reserved region`) — the panel asserted before the drain.
- **The read loop's own goroutine** (the submit path flushes the line's tail
  through the same seam; a nested producer reached from a region write) must
  **not** wait: it is the only drainer, so waiting is a deadlock. Measured during
  the build: a "always wait" seam blocked `WriteLines` called from the loop.

Told apart by **goroutine identity**, never by a flag the caller sets:
`Run` records `goid()` in `loopGoid` for the duration of the call and the
comparison is exact, so a goroutine whose id differs is not the read loop,
whatever it calls itself. A flag would be a claim; the id is a fact. `goid()`
parses the id off the stack header of the calling goroutine (the runtime exposes
no accessor) and **compares it for equality only** — never ordered, never stored
across sessions, never printed in production.

### Rule 35 (zero dependency): what this artifact is, said in the four terms

1. **What breaks without it.** Two measured failures, one on each side. Without
   a wait, the panel's rows are asserted before the drain (`TestChatHelpPanel_*`,
   above). With an unconditional wait, the loop's own seam call blocks forever
   (the `TestBandWriter_TheReadLoopWritesTheRow` shape: *a band write from the
   read loop's own goroutine BLOCKED*).
2. **Why the failure cannot be fixed in-process without it.** The alternative is
   a caller-passed boolean ("am I the loop?"), which is the same information as a
   CLAIM rather than a fact — one stale call site and the deadlock is back. The
   other alternative is to make drains synchronous under one mutex held across
   the region write; that serialises the loop against every producer and is the
   lock-hides-the-race shape this whole change exists to avoid.
3. **Who owns its lifecycle, and how it is cleaned up.** Nothing persistent is
   created: no process, no file, no socket, no config key, no new environment
   variable. The only external surface is the Go runtime's stack-header format,
   read read-only, per call, and **anti-regressed by a test**:
   `TestBandWriter_TheReadLoopWritesTheRow` fails if the identity read breaks
   (the loop's own call would then be treated as foreign and block).
4. **What the next person sees.** `bandwait.go`'s file comment states the two
   callers, the two measured failures, why identity and not a flag, and the
   equality-only restriction — a reader who finds `goid()` in a stack trace lands
   on that reasoning, not on a mystery.

### The wait applies only where a DRAINER exists (the arm the first cut missed)

Waiting is meaningful only when *somebody can drain*. Two shapes have nobody:

- **No wake seam** (`queueBand` returned false): the frame waits for the loop's
  own next wake, so the seam keeps the historic `true` — a `false` would make the
  caller ALSO emit the historic flow bytes and the row would appear twice. The
  degraded cadence is what `BandNotifyMisses` counts.
- **No read loop running** (`loopGoid == 0` — between two lines, EOF, teardown):
  nothing drains until the next `Run`, so waiting spends the whole bound and then
  reports a failure that is not one.

The second shape was found by running it, exactly as Rule 31 says it would be:
the `cmd/agent` pty case burned `bandWaitBudget` (4s) on a **shutdown-path**
write, after which the caller fell back to the historic flow bytes mid-session.
Both arms now return the historic `true` (the layout still owns the screen; the
frame is a LEVEL the next repaint drains — late, never lost) and the failure
report is reserved for the case it actually describes: a read loop that exists
and did not draw. `bandWaitBudget` names the bound once, so the report and a
reader comparing a frame's own cost (one region write, microseconds) against it
read the same number (Rule 1).

## NOT DONE — the residual race family (a DIFFERENT pair, measured)

This change removes the flow-seam pair completely: across **12 consecutive
post-fix runs** of `go test -race ./cmd/agent/ -run 'TestChatSurfaceKey_'`, no
block names `WriteLines`, and no block names `Value.Reset` (pre-fix: 19 of 26
blocks named `WriteLines`, 5 named `Reset`).

The same runs still report other pairs, which this change did not touch because
they are not the flow seam:

| pair (writer <-> reader) | blocks (12 runs) | what it is |
|---|---|---|
| `AnswerRenderer.reanchorLiveRow` <-> `drawStatus` | 41 + 18 | the answer renderer's live-row state, written by the turn goroutine (`Delta`/`BeginTurn`), read by the loop's `Tick` |
| `AnswerRenderer.Note` <-> `Noting`/`drainNotes` | 22 + 15 + 6 + 3 | the notes SLICE: appended from the turn goroutine, drained by the loop |
| `drainNotes`/`commitPrefix` <-> `writeRow` | 9 + 9 | the in-flight row's own bookkeeping, read by both |
| `LineComposer.inFence` <-> `EndStream` | 9 | the composer's fence state, turn goroutine vs loop |
| `writeRow` <-> `writeRow` | 9 | two writers on one row |
| `Renderer.Redraw` <-> `Redraw` / `Menu.draw` | 4 + 3 | the renderer reached from both |

They are the SAME shape one layer further in: the answer renderer's streaming
state is mutated by the turn goroutine while the read loop repaints it. Closing
them is the same move as this one — make `Delta`/`Note` a PUBLISH the loop drains,
so the band's writer and the renderer's state have one owner — and it is a
separate piece of work with its own guard. Until it lands, the epic's acceptance
("ZERO `WARNING: DATA RACE` lines over >=12 runs") is **not** satisfied, and this
document says so rather than reporting a green that is not there.

**It is DISPATCHED, not merely noted**: task `01a0e630-8d54-7c88-bd1f-692d00068e49`
("the ANSWER RENDERER's streaming state still races the read loop"), priority 1,
carrying this table, the exact repro command and the fix shape above (Rule 26 —
an incomplete fix is repaired by dispatching again, with the reason and the
evidence in the briefing). One further pair a later run named is NOT production
code and belongs with it as a fixture defect:
`Terminal.Size` (raw.go:382) vs `chat_pty_fixture_test.go:247` `closeQuietly` —
the fixture closes the pty at teardown while the read loop still polls it.

