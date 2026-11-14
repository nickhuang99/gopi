<!-- doc-class: historical -->
# The answer renderer's write ownership — one owner per instant, not a lock

Task `01a0e62d` (epic: "close the second half of the lineedit ANSWER-RENDERER
race family"). The predecessor `01a0e608` closed the FLOW seam
(`Editor.WriteLines` / `WriteBandRow`) and is documented in
`devdoc/lineedit-band-write-ownership.md`; this is the sibling seam — the
streamed ANSWER itself.

## What was wrong

`AnswerRenderer`'s state — the in-flight buffers (`pending`, `prefix`,
`partial`, `compPrefix`, `tableRows`), the live row and column (`liveText`,
`liveRow`, `liveCol`, `inFlight`), the marker/reasoning ledger (`marked`,
`ledged`, `thinking`, `thinkingWrote`), the caller-row queue (`notes`, `noting`),
the commit counter and the composer's fence — was mutated from TWO goroutines:

- the **turn's** goroutine: `chatTurnRenderer.Begin/BeginTurn/EndTurn/Write/
  Thinking/AnswerBoundary/Finish/ToolRow` (cmd/agent/chat_stream.go, driven from
  `cli_chat.go`'s turn callback and `internal/agent`'s tool-event emission);
- the **read loop**: `Editor.Run` → `repaint` → `paintMenu` →
  `AnswerRenderer.Tick` → `drawStatus`, and `repaint` → `drainBandFrames` →
  `drainNotes` → `writeRow`/`commitPrefix`/`finalizeLiveRow`.

Measured on the branch base (`a5fde636`, i.e. WITH the flow-seam fix), one run of
`go test -race ./cmd/agent/ -run 'TestChatSurfaceKey_' -count=1` produced **11
`WARNING: DATA RACE` blocks**; both cases FAILED. By symbol pair:

| pair | blocks |
|---|---|
| `drawStatus` ↔ `reanchorLiveRow` | 4 |
| `Note` ↔ `Noting` | 3 |
| `drainNotes` ↔ `writeRow` | 2 |
| `commitPrefix` ↔ `writeRow` | 1 |
| `LineComposer.inFence` ↔ `EndStream` | 1 |
| `writeRow` ↔ `writeRow` | 1 |

Artifacts: `~/.gopi/sessions/01a0e62d-3bfd-7ab8-b23b-a66a1632c60c/final/pre-renderer-baseline.log`.

## The fix: routing, and why not a lock

The package's invariant was already stated for the band — *the read loop is the
layout's only writer and the editing model's only reader*. This task extends it
to the renderer: **while a read loop runs, the LOOP owns the renderer.**

A turn-side call therefore PUBLISHES instead of mutating
(`internal/lineedit/answerqueue.go`):

1. `AnswerRenderer.routed(op)` asks whether this goroutine may touch renderer
   state right now. The identity test is the one the band seam already uses
   (`bandwait.go`): `Editor.loopGoid` (set by `Run` via `noteAudience()`, before
   the first repaint) compared for EQUALITY with the caller's `goid()`.
   - loop running **and the caller is not it** → queue an op and return;
   - **no loop running**, or the caller **is** the loop (the drain's own
     application) → perform the work here.
2. `queueAnswerOp` appends to `Editor.answers` (a queue + a level, guarded by its
   own mutex, exactly like `frameMu`/`band`/`bandActive`) and wakes the loop.
3. The loop applies the ops **in the producer's order**, through the renderer's
   own methods (`applyAnswerOp` → `beginNow`/`deltaNow`/`finishNow`/
   `beginTurnNow`/`endTurnNow`/`beginThinkingNow`/`endThinkingNow`/
   `answerBoundaryNow`/`noteNow`).

**A lock over the fields was rejected, and the reason is Rule 15, not taste**: a
mutex around renderer state would leave the two goroutines' passes just as
interleaved — a commit's cursor placement landing between another commit's
scroll — and merely stop the detector from seeing it. Routing removes the
interleaving instead: at any instant one goroutine owns every field.

**The drain is placed FIRST in `repaint`, before the plan is resolved.** The
first implementation drained at the END (beside `drainBandFrames`) and the
epic's own acceptance case failed: an answer op COMMITS rows into the band
(`writeRow` → `commitScroll`), the band's row count is half of what the plan
decides, so drawing the chrome first left the editing line and the menu on rows
the published layout no longer named — `chat_surface_help_menu_e2e_test.go:452`
read `activeMenuRow() == -1` while the same failure's dumped grid showed
`you> /h` with the menu rows two lines lower than the layout said.

**Ordering identity.** `drainAnswerOps()` applies ops through the same exported
methods a direct caller uses, so the drain is a *re-ordering across goroutines*,
never a change of sequence: whatever a direct caller would have done in order,
the loop does in the same order on its own goroutine. That is what keeps this
package's direct-drive test call sites byte-for-byte the same program — with no
loop running, `routed` is false and the caller takes the historic road.

## Supporting changes

- `Committed()` is now atomic (`committed atomic.Int64`): it is read back by a
  caller (`cmd/agent/chat_stream_test.go:359`) while the loop increments it in
  `finalizeLiveRow`. A plain int there is the HARNESS's own race, and a harness
  race hides the production one the test exists to find.
- `chatInput.SettleDump` (cmd/agent/lineedit_wire.go) waits
  (`Editor.WaitAnswerDrawn`) before `dumper.Settle()`: the turn's writes arrive
  on two roads now — the flow seam's band frames and the answer renderer's ops —
  and the dump must freeze a frame in which both have landed, or the artifact
  describes a screen the operator never saw.
- `AnswerRenderer.Composer()` exposes the body classifier so a caller that must
  ask the classifier a question asks the OBJECT (whose methods are the fence's
  own mutex), never a copy of its state.
- `WaitAnswerDrawn` mirrors the band terminator exactly, including its two
  exclusions: the loop's own call must not wait (it is the only applier ⇒
  deadlock), and with no loop running the caller already did the work. Reaching
  the bound is REPORTED on the editor's error channel
  (`reportAnswerWaitFailure`), never silent.

## Guard

`internal/lineedit/answer_writer_foreign_test.go`, the same shape as
`bandwriter_foreign_test.go`: the read loop is PARKED on the source's gate, the
turn-side call is made from a goroutine that is not the loop, and the observables
are the recorded byte count and the commit counter — a COUNTER, not a sampled
window, so the guard cannot flake.

- `TestAnswerWriter_ForeignGoroutineWritesNothing` — the foreign `Delta` must
  write ZERO chunks and commit ZERO lines, and the release must then show the
  loop drawing the text. Red-proof: on the pre-fix tree the routed methods do not
  exist, so the guard does not compile against the old API; the behavioural red
  is the baseline log above, whose 11 blocks are this exact pair.
- `TestAnswerWriter_GuardIsDeterministic` — 5 repetitions.
- `TestAnswerWriter_NoLoopMeansTheCallerOwnsIt` — the other road, pinned: with no
  loop the caller does the work (a design that refused it would be a regression
  of the package's own tests).

## Measured result

| run | WARNING: DATA RACE | outcome |
|---|---|---|
| pre-fix | 11 (one run) | both cases FAIL |
| post-fix, run 1 | **0** | both PASS, `goroutine sweep: parked repository goroutines: 0` |
| post-fix, 12 consecutive runs | **0 in every run** | both PASS in every run |

### The red proof (measured out-of-tree)

A detached worktree at the branch base `a5fde636`
(`/home/user/workspace/gopi-redproof-a5fde636`, removed afterwards) received ONLY
this guard file, and `go test -race ./internal/lineedit/ -run 'TestAnswerWriter_'
-count=1` there FAILED both tests — the foreign `Delta` wrote ITS OWN text from
its own goroutine while the read loop was parked:

```
--- FAIL: TestAnswerWriter_ForeignGoroutineWritesNothing (3.00s)
    answer_writer_foreign_test.go:198: the turn-side Delta wrote 2 chunk(s) while
    the read loop was parked: the renderer is mutated by two goroutines, ...
        --- chunk 3 ---
        <ESC>[6;1Hanswer-owner-marker
--- FAIL: TestAnswerWriter_GuardIsDeterministic (3.00s)
    answer_writer_foreign_test.go:234: rep 0: the turn-side Delta wrote 2 chunk(s)
```

So the guard is red by BEHAVIOUR on the pre-fix tree (not merely by failing to
compile against the old API), and green after — the red-then-green the epic asks
for. Log: `~/.gopi/sessions/01a0e62d-.../post/red-guard.log`.

## Not done / honest residue

- **`Renderer.Redraw` ↔ `Menu.draw`**: the epic listed this pair, and it is NOT
  reproducible through the answer seam. `Renderer.Redraw` and `Menu.draw` belong
  to the editing line and the completion menu, both of which are touched by the
  read loop alone; a turn-side call cannot reach them (the answer seam hands the
  loop an OP, and the loop schedules it). No block in the baseline's 11 names
  either symbol. It is recorded here as **not reproduced / not addressed**, not
  as fixed.
- **`Writes()`'s byte-interleaving bound is unchanged**: two goroutines still
  write to the paint stream (`write` is atomic per call and counted, but a cursor
  placement and the text it was for can interleave). This task removes the
  RENDERER-STATE races; the fd-level frame coordination is the same residual the
  predecessor's doc records, and it was not this task's acceptance criterion.
- The turn-side call still performs its pre-flight reads on the caller's
  goroutine (`r.out.Active()`, the `text != ""` early return, the string
  normalisation in `Delta`'s callers): those are `region` reads, not
  renderer-field reads, and no baseline block names them.
