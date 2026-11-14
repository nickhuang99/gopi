<!-- doc-class: historical -->
# A band write with no read loop: the layout's direct drivers, and why one branch cannot serve them

## What broke

`cmd/agent`'s audit suite was RED on master at `7de8045c` — 10 cases, each taking
~4.3s:

```
[gopi] editor: a band write was not drawn within 4s (the read loop is stalled?) — the caller announced a screen it does not own
--- FAIL: TestUIStateAudit_TextFacePrintsTheSameCountersAsTheList (4.34s)
    the band refused the row "…": no reserved layout is active
--- FAIL: TestChatDump_GridFollowsTheLayoutGeometryOnARealPty (4.49s)
    the band refused the row: no reserved layout is active on the editor
```

The message was wrong on both halves: no read loop was *stalled* (none existed),
and the layout **was** active — `EnableRegion()` had returned true.

## Root cause (5-why)

1. **Symptom** — `WriteBandRow` returns false after 4s; the caller reports "no
   reserved layout is active".
2. **Mechanism** — `WriteBandRow` queues the row (`queueBand`) and waits
   (`waitBandWrite`) for a read loop to drain it. The fixtures never call
   `Editor.Run`, so nothing drains: 4000 × 1ms = `bandWaitBudget` (4s), then
   `false`.
3. **Why the wait exists** — `b3687371` / `de9f8c78` routed band writes through
   the read loop to close a real `-race` defect (26 `WARNING: DATA RACE` blocks in
   one run of `TestChatSurfaceKey_`): the region write + `e.repaint()` ran on the
   turn's goroutine while `Run` did `e.val.Reset()` on the same `Value`.
4. **Why the fixtures broke anyway** — `363aa34e` recognised exactly this shape
   and gave `WriteLines` a *"no drainer in sight"* arm (`loopGoid == 0` → return
   at once). **`WriteBandRow` — the sibling method on the same seam — never got
   one.** Verified from the commit: `git show 363aa34e -- internal/lineedit/editor.go`
   is one hunk, in one method.
5. **Root cause** — a fix applied to one of TWO methods that share the seam. The
   difference is invisible from either call site (they differ only in the frame's
   shape), so a caller cannot tell which arm it is owed.

## Why the obvious fix is wrong — the measurement that shaped the design

The first attempt was `363aa34e`'s arm plus a draw:

```go
if e.loopGoid.Load() == 0 { e.drainBand(); return e.BandActive() }
```

It went green on the acceptance filter and **broke two other cmd/agent cases**:

```
--- FAIL: TestChatStream_RepaintsStayLinearInTheAnswer
    chat_stream_test.go:360: committed lines = 39 for 40 finalized lines, want exactly 40 (one commit each)
--- FAIL: TestChatStatus_StaysOneScreenRowWithALongToolSummary
    lineedit_height_test.go:270: rows=4: editing row 2 = "you> type", want "typed" — the status row took it
```

Those harnesses DO run a read loop — but in a goroutine, while the test goroutine
writes band rows. In the boot window `loopGoid == 0` too, so the row was drawn by
the caller and the loop's **first repaint painted over it**.

A lifetime fact (`loopSeen`, set when `Run` is ever entered) was tried next and
measured: `TestChatStream_RepaintsStayLinearInTheAnswer` still failed **10/10**
with it, 7/10 without. The boot window is narrower but real, and it is not
separated by that flag either.

**The conclusion, and the reason this is a seam rather than a branch:**

> At the moment of the write, *"the read loop has not been scheduled yet"* and
> *"no read loop will ever come"* are **the same editor state** (`EnableRegion`
> done, nothing read yet). No per-call fact can tell them apart. A branch on the
> editor's state must therefore pick ONE answer for both, and both answers are
> measured broken: drawing costs a row the loop paints over (39 of 40); queueing
> costs an empty grid for the fixtures.

## The fix — two halves

**Half 1 — the missing no-wait arm** (`WriteBandRow`, internal/lineedit/editor.go).
With no loop running the call must not spend the bound:

```go
if e.loopGoid.Load() == 0 {
    return true   // the frame stays the LEVEL the loop's next read outcome drains
}
return e.waitBandWrite()
```

`true` is the historic answer (`false` means "no layout, write the flow bytes
yourself") and the frame is *late, never lost* — the same arm `WriteLines` keeps
for this state.

**Half 2 — a settle seam for the caller that KNOWS it owns the screen**
(`Editor.SettleBand`):

```go
// SettleBand writes whatever band frames are QUEUED, on the caller's goroutine,
// and reports whether the layout still owns the screen.
func (e *Editor) SettleBand() bool { e.drainBand(); return e.BandActive() }
```

A caller that never starts a loop — the pty fixtures, which read the grid back on
their own goroutine and have no next wake at all, so "late" means "never" — says
so explicitly. Both affected fixtures now call it:
`cmd/agent/cli_uistate_audit_test.go` (`auditFixture`) and
`cmd/agent/chat_dump_geometry_pty_test.go`.

**Ownership is untouched.** A direct driver is by definition the only goroutine
holding the editor; `drainBand` writes frames and reads **no** editing model
(`repaint()`, which reads `Value` and produced the 26 race blocks, stays the
loop's own and is not called here). `TestBandWriter_ForeignGoroutineDoesNotReadTheEditingModel`
— which parks a real loop and asserts a foreign write reads no model — still
passes, and the `-race` band family is clean.

## Minimal repro test

`internal/lineedit/bandwriter_noloop_test.go`, three cases through the REAL editor
(`NewChatEditor` + `EnableRegion`, a wake seam installed, no `Run`), asserting the
RENDERED CELL GRID replayed from the editor's own paint stream — never a return
value alone:

| case | asserts |
|---|---|
| `TestBandWriter_NoReadLoopWriteDoesNotSpendTheBound` | the call returns without spending `bandWaitBudget`, reports true, and QUEUED the frame |
| `TestBandWriter_TheDirectDriverSettlesTheRowsOntoTheGrid` | both rows are on the grid after the settle, in order, and `bandPending()` is false |
| `TestBandWriter_TheDirectDriverKeepsTheHistoricFalseWhenTheLayoutIsGone` | layout taken down ⇒ `false` from both, nothing drawn |

**RED proof — both halves, measured separately** (the new test file needs
`SettleBand`, so a whole-file revert cannot compile; each half was reverted on its
own):

| reverted | observed |
|---|---|
| the `loopGoid == 0` arm in `WriteBandRow` | `--- FAIL: TestBandWriter_NoReadLoopWriteDoesNotSpendTheBound (4.30s)` — `WriteBandRow spent 4.298384484s with no read loop to drain the queue` |
| the fixtures' `SettleBand()` call | `--- FAIL: TestChatDump_GridFollowsTheLayoutGeometryOnARealPty` — `the grid is ALL-BLANK`; `--- FAIL: TestUIStateAudit_TextFacePrintsTheSameCountersAsTheList` — `the fixture's band did not leave "the answ" on the grid it claims to have painted` |

Both restored → the acceptance filter is `ok … 1.270s`.

## Sibling audit — every caller of the seam, by whether a loop exists

| caller | read loop? | before the fix |
|---|---|---|
| `cmd/agent/cli_uistate_audit_test.go` `auditFixture` | no | **RED** (9 `TestUIStateAudit_*` cases) |
| `cmd/agent/chat_dump_geometry_pty_test.go` | no | **RED** (`TestChatDump_GridFollowsTheLayoutGeometryOnARealPty`) |
| `cmd/agent/lineedit_wire.go:468` (`chatInput.writeBandRow`) | yes — production REPL | unaffected (the loop arm is taken first) |
| `internal/lineedit/bandwriter_foreign_test.go` harness | yes (parked `Run`) | unaffected |
| `cmd/agent/lineedit_wire.go:360/391`, `cli_chat.go:784/1245/1525/1795/1806/1824` | via `WriteLines`, which already had its arm | unaffected |
| `internal/lineedit/bandwriter_noloop_test.go` (new) | no | was RED, green after |

## Pre-existing reds, named so they are not attributed to this change

Measured by reverting the source in this same worktree and re-running:

- `internal/lineedit`: `TestEmphasisSpanAfterAWrapSpaceKeepsItsOwnCells`,
  `TestEmphasisWrapSpaceLedgerHoldsAtEveryWrapCount`,
  `TestScreenReconcilesAfterAnOverlongHold`, `TestScreenReconcilesAcrossAReMeasure`,
  `TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow` — all five fail on the
  unmodified tree.
- `cmd/agent`: `TestChatTurnHeader_PaintsAMultiRowCountedHeaderOnARealPty`,
  `TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer`,
  `TestLedgerOpenersUseProductionDSNContract`,
  `TestSlashMenuOneCandidateStatesWhyTheArrowCannotMove` — fail pre-fix too.
- `TestChatStream_RepaintsStayLinearInTheAnswer` and
  `TestChatStatus_StaysOneScreenRowWithALongToolSummary` are **flaky in both
  states** (`-count=10`: 7 and 6 pre-fix vs 0 and 0 post-fix in the same batches;
  a later `-count=20` gave 16). Not worsened here, not fixed here, not skipped.

## Verification

See `task-report.md`.
