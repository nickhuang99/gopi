<!-- doc-class: historical -->
# The chat dump's freeze must wait for the paint stream

**Defect class**: a test observation taken from a *live* async sink, frozen at an
arbitrary moment. Every assertion in `cmd/agent/chat_dump_test.go` reads row indices
off the dump's grid, so a frame captured mid-paint makes those indices describe a
grid no run ever drew.

Measured 2026-09-26, whole-package run of `./cmd/agent/`:

```
--- FAIL: TestChatDump_FenceAndDiffMarkersStayLiteral
    a diff line inside a fence marks 4 bold cell(s), want 0
        row 10 = "you> + added **d**"
```

## The two mechanisms

**1. The freeze was taken while paints were in flight.** The sink is fed from the
EDITOR'S READ LOOP — `chatDumpWriter` tees the paint stream
(`cmd/agent/chat_dump.go:300`), so the probe's own `renderer.Write`/`Finish` calls
return long before those bytes reach the sink. `newChatDumpProbe*` then called
`dumper.Close()` immediately, and `Close` marks the sink closed and feeds nothing
further: the artifact banks whatever frame happened to be current.

The probe's `textRowOf` returns the **last** row matching a text, so a frame in which
the answer's tail landed a second time on the row the editor's prompt owns made the
assertion measure `you> + added **d**` — the prompt's own 4 bold cells — instead of
the band's `▌ + added **d**` (0 bold cells, inside a fence).

**2. A re-seed at an identical size discarded painted cells.**
`ScreenDumper.SetGeometry` re-seeded the grid (`d.screen =
lineedit.NewScreen(cols, rows)`) whenever the geometry SOURCE changed — including the
`fallback → layout` hand-off every probe makes, at an unchanged size. A fresh `Screen`
starts at row 0 with a blank grid, while the painter keeps writing from its own live
cursor position, so rows after the re-seed land displaced. `internal/uiclient/dump.go`
now moves the provenance and flushes the artifact without re-seeding when the SIZE is
unchanged; only a real size change re-seeds.

## The fix

`cmd/agent/chat_dump_test.go`: the one seam every probe uses now **stops the producer
and then waits** before it freezes.

```go
if !h.stopReadLoop() { t.Fatal(...) }   // the harness's own deterministic stop
waitForChatDumpFrame(t, d, deltas)      // then the content/stability gate
```

**Why stopping first is the load-bearing step.** A quiet-window wait alone cannot
close this window, and the measurement is unambiguous: this harness drives the answer
renderer off a **test clock whose word-flush window is 2s**
(`chatTestEpoch + chatWordFlushWindow`), so a 60ms quiet window is satisfied over and
over while the band is still being painted. A whole-package `-count=80` stress run
then froze `▌ a b` — the turn's last line, half written — and the probe's own
assertion reported `the dumped grid does not carry "a b c" — the answer never reached
the artifact`. Waiting cannot fix a moving target; stopping the producer can.

`chatDumpFrameSettled` then requires **two** more facts, both measured on the sink
itself — the only surface the assertions read:

1. **STABILITY** — the sink's grid has not changed for `chatDumpQuietWindow`
   (`2 × chatWordFlushWindow`, the renderer's own word-flush cadence, so a quiet
   window can never be shorter than one producer step). The sink is fed from the
   editor's read loop, so this says that loop has finished handing over the turn.
2. **CONTENT** — `chatDumpFrameJustifies`: the line the turn last wrote is on the
   grid (in either spelling the layout legitimately draws it — raw, or resolved by
   lineedit's own tokenizer: `internal/lineedit`, never a second marker parser), and
   no row the PROMPT owns (anchored at column zero) **duplicates** a line already on
   the band.

Stability alone cannot be trusted (the read loop can pause longer than the window),
and content alone cannot either (a mid-paint frame already carries the text, on the
wrong row) — hence both. On expiry the gate fails BY NAME with the sink's whole grid,
never handing the assertion a frame the turn did not draw.

## The regression

`TestChatDumpFrameGateRefusesAMidPaintGrid` drives the criterion on a real sink:
a grid missing the turn's last line is refused; a grid that carries it but duplicates
a band line on the prompt's row is refused (the measured shape); a grid carrying it
with a clean prompt row is accepted; and `chatDumpFrameSettled` refuses a grid that
has not yet been quiet for the window, so the wait is pinned as a wait.

## Alternatives tried and rejected (recorded so they are not re-tried)

- **The harness's replayed reference screen as the oracle.** A beautifully exact
  idea — two readings of one recorded stream — and it does not hold: the replay is
  fed from the recording, which the sink's own reads grow (`scriptSource.holdAtEnd`),
  so the two screens are fed *different* byte sequences at different times. Measured
  divergences were permanent and not sink defects: `▌ a **b** c` (sink) vs
  `  a **b** c` (reference), i.e. a gutter-vs-band difference in how many frames the
  reference had replayed. An oracle that disagrees with the artifact about the bytes
  they were both given cannot decide when the artifact is right.
- **Block-count handshake with that recording.** `d.Frames() == h.out.count()` looked
  like the missing handshake, but the sink's read loop and `BeginTurn`'s editor paint
  deliberately bypass the per-block tee, so frame counts are not comparable — measured
  as 0 vs 22 and as a 5.00s hang on `TestChatDump_QuotedFenceMarkerKeepsTheProseProse`.
- **A blanket "nothing after the prompt on the prompt's row" leg.** Refuses a correct
  frame: `TestChatDump_QuotedFenceMarkerKeepsTheProseProse` legitimately settles with
  `you>  follows` and asserts on that very grid. The surviving leg is narrower and
  exact — the prompt's text must not be a *duplicate* of a band line.
- **Waiting on the artifact FILE** (a pre-existing pattern in this package). A no-op
  here: the sink's refresh is throttled (100ms) and the file is written by `Close`,
  so waiting on the file is waiting for a write about to happen anyway.
- **A dry-run/pre-flight call on the real probe** — the pre-fix code in
  `newChatDumpProbe` runs the deltas a SECOND time before the artifact is armed.
  Running the gate there (tried, then reverted) is worse than useless: the dry run's
  frame is bound to the discarded sink, so a correct dry run made `d.Close()` freeze
  the artifact before the real turn had painted. That pre-flight step is a separate
  smell, left alone here (it is why the artifact-wait pattern exists at all).

## Claim split — the gate is NOT what caught it

The gate is a SYNC fix: it stops the observation from being taken at the wrong moment.
The dump-content claims are unchanged, and each was verified green with the gate in
place. No new product behaviour is asserted here, which is why this change is scoped
"the test is deterministic", not "the dump renders correctly".

## The SECOND ORDER DEFECT, found after this gate landed (2026-09-26, same day)

The gate above stopped the artifact being frozen mid-paint. It did NOT stop the
artifact being frozen on a byte stream the terminal never received — and that is the
defect the `-count=120` stress runs then kept surfacing:

```
--- FAIL: TestChatDump_FenceAndDiffMarkersStayLiteral
    the dumped grid does not carry "a b c" — the answer never reached the artifact:
        ▌ ```
        ▌ a **b** c
        ▌ + added **d**
        ▌ ```
        ▌ a b            <-- the artifact's last row; the answer's tail is gone
```

### What it is NOT (the briefing's own hypothesis, refuted by measurement)

The task that followed this one described the freeze as fed by **two uncoordinated
writers** — the read-loop tee (`chatDumpWriter` → `io.MultiWriter`) and the input's
own direct writes (`newStreamHarnessDumped` sets `in.dumper`). That is not what the
code does and not what the instrument saw:

- `region.outFn`, `Editor.out`, the editor's `render` writer, `AnswerRenderer.out`
  (the region) and the dump tee are ONE writer chain (`lineedit.Open(in, paint)` /
  `NewChatEditor(term, paint, …)` in `newCmdChatInput`), so there is no second
  *chain*;
- `ScreenDumper.Write` holds `d.mu` across the whole `screen.Feed(p)`, and
  `Snapshot()` takes the same mutex — so the sink's grid cannot be torn by callers;
- the instrument recorded **0** prefix-loss events in 800 instrumented iterations,
  i.e. no single feed ever blanked a painted cell;
- every write in a 48-write trace came from ONE goroutine.

### What it IS: the fan-out's per-consumer ORDER

`chatDumpWriter` (with a sink armed) returned `io.MultiWriter(out, d)`. A
`MultiWriter` hands one chunk to each consumer **sequentially**, so two painters
reaching it concurrently can reach the two consumers in different orders:

```
A: terminal(pA) ─────────┐            B blocks on the lock, then overtakes both
B: terminal(pB)          │            artifacts (measured, 2 of 120 iterations):
B: dump(pB)              │              artifact  ▌ a b        / ▌ a   c
A: dump(pA)  <───────────┘              replay    ▌ a b c      (the same writes,
                                                         replayed in RECORD order)
```

The paint stream has exactly two painters — the turn goroutine's answer renderer
(`Begin`/`Delta`/`Finish` → `writeRow` → `region.commitScroll`) and the editor's read
loop (`Tick`'s status repaint, the menu) — and `io.MultiWriter` is a fan-out, not a
serializer. The sink's grid is the interpreted **effect** of its own byte order, so
the artifact described a screen no run drew. The harness's replayed screen, fed the
SAME bytes in the RECORDED order, held `▌ a b c` throughout — which is why the
failure was an *oracle* failure ("the answer never reached the artifact") on an
artifact that had received every byte.

Measured with a per-feed instrument over 120 iterations of the real probe scenario:
**prefix-loss 0, artifact-missing 1 (spy) + 1 (plain tee), gate-unsettled 0**, and a
third observation: a second goroutine was inside `ScreenDumper.Write` in **647 of 800**
iterations (the overlap is the precondition; only the rare *ordering* of the two legs
corrupts).

### The fix

```go
// cmd/agent/chat_dump.go
func chatDumpWriter(out io.Writer, d *uiclient.ScreenDumper) io.Writer {
	if d == nil {
		return out // the no-flag path is the SAME writer, byte-identical
	}
	return &chatPaintWriter{tee: io.MultiWriter(out, d)} // one paint WRITE at a time
}
```

`chatPaintWriter` holds a mutex for the duration of one fan-out, so the two consumers
always see the same order. It is the seam the run already owns, and with no dump armed
nothing changes at all (the nil path returns the caller's writer unchanged).

### The regression, and why it is deterministic

`cmd/agent/chat_dump_writer_order_test.go` constructs the interleaving instead of
sampling it (the shape `internal/lineedit/parkgate.go` already uses for its own hard
flake): the TERMINAL consumer writes the settled row to the recorder and then
**parks** — the fan-out is halfway through, the sink has not been handed that chunk —
while a second painter writes a two-cell blank over the same row. The park is after
the terminal leg, deliberately, and the gate holds no lock the other writer needs, so
the fan-out stays unsynchronized (the thing under test).

The assertion is the invariant the artifact lives on: **the sink's grid and the replay
of the recorded stream must describe the same screen.**

- RED (pre-fix writer, verified by reverting `chatDumpWriter` for one run):
  `5/5` iterations failed with `terminal row 1 = "a   c"` vs
  `artifact row 1 = "a b c"` — the exact measured asymmetry, produced deterministically.
- GREEN (the fix): `5/5` pass, in 0.05s each.

## Honest limits

- The content leg is a *spelling-aware* check, not a full grid comparison: it proves
  the turn's last line is present and that the prompt's row is not a duplicate, not
  that every row equals what the layout drew. A row-level oracle that can be trusted
  does not exist in this harness (see the first rejected alternative), so the gate
  stops at what the assertions actually depend on.
- The `d.Frames()` counter and `ScreenDumper.Write`'s read-loop bypass are recorded
  above as measured facts; a future reader tempted to build a count handshake should
  read that paragraph first.
- **The fan-out fix removes the ORDER divergence between the sink and the terminal,
  not the two-painter shape itself.** `AnswerRenderer`'s doc comment still carries the
  honest bound that a turn's answer bytes and the read loop's status repaint are
  emitted from two goroutines; what changed is that with a dump armed they can no
  longer be *fanned out* in two different orders. The `-race`-visible renderer state is
  a separate matter and unchanged here.
- The measured rates are from one machine inside a whole-package run; the overlap
  figure (647 of 800 instrumented iterations) is an upper bound observed WITH the
  instrument installed, not a production rate.

## The THIRD SHAPE: the live row was CACHED while the layout moved (2026-09-26/27)

The fan-out fix above removed the ORDER divergence. A residual flake survived it, and
it is a different defect — **one painter writing to a row the layout no longer
reserved for it.**

### What was measured, and how

1. **The class, isolated.** With the fan-out fix merged in, the residual rate is
   ≈1 hit per 300 iterations of a single test
   (`TestChatDump_QuotedFenceMarkerKeepsTheProseProse`: 3 hits in 600, and 2 of 3 in
   one 400-iteration batch) — measured, not estimated.
2. **The signature.** The failing artifact carries a row whose MIDDLE is blank. Two
   measured rows, character-counted (the row keeps its FULL 29-cell width, so the
   grid is not short of cells — a contiguous run inside it was erased):

   | artifact row | `the` | blanked | tail |
   |---|---|---|---|
   | `▌     bold conclusion follows` | head on another row | — | `bold conclusion follows` |
   | `▌ the                 follows` | `▌ the` (cols 0-4) | cols 5-20 (16 spaces) | `follows` (cols 21-27) |

   The strong form of the span is column-counted: markup `the **bold conclusion**
   follows` renders as `the ` (default), `bold conclusion` (bold), ` follows`
   (default) at **0:6, 6:15, 21:19** — and that row's attrs read
   `0:40=default`, i.e. the band row exists with a blank intermediate span and the
   other row carries no style at all.
3. **The bytes.** A tap on the paint stream (recording, for each chunk, the sink's
   grid as it stood just before that chunk) captured the transition:

   ```
   BEFORE[32895] grid holds row 9 = "▌     bold conclusion"     ← head painted one row ABOVE
   BEFORE[32896] grid holds row 9 = ""                          ← the editor's repaint erased it
   ```

   So the head is written, then erased, and the tail arrives later on the band row:
   the artifact keeps a row with a hole.
4. **The mechanism (the root cause).** The answer renderer drew through the layout
   the editor owns, and that layout MOVES while an answer is in flight: the editing
   line grows (the submitted message is laid out in full, so it needs more rows) and
   the band CONTRACTS by the same rows. The renderer had the band's live row in
   `r.liveRow`, read once per disk/predecessor call and then used for the rest of the
   burst. A stale live row is one row BELOW the band's new bottom — the editing
   line's first row — so the next write does exactly what its arithmetic says: it
   places the cursor there and paints the answer's head onto the editing line's row.
   The editor's next repaint erases that row (`ESC[<editorTop>;1H ESC[2K`), while the
   COMMIT that follows re-reads `LiveRow()` fresh, so the tail lands on the band.
   The result is the splattered row above — and the gate times out at 5s, because the
   band row can never carry the needle it waits for.

   The interleaving is a race: the window is "layout moves between the paint of the
   head and the paint of the continuation", which is why sampling it costs hundreds
   of iterations and why the regression guard CONSTRUCTS it instead.

### The fix

`internal/lineedit/answer.go`: every live-row write re-anchors first
(`reanchorLiveRow` / `refreshLiveRow`) — the row is taken from the layout that owns
it, and when it moved, the in-flight head is REPAINTED on the row the layout now
reserves (the rows it gave away belong to the editing line, whose repaint erases
them, so keeping only the tail would commit the line headless). The same refresh is
applied at the commit path's carry and at the live-row promotion, and the wrap width
rides the same layout (a resize that moved the live row moved the width with it).
`AnswerRenderer.LiveRow()`/`Rows()` expose both so a caller never needs a cached copy.

### The regression

`internal/lineedit/answer_liverow_test.go` — it does not sample the race, it builds
the interleaving: paint the head of a line, move the layout by exactly one editor row
(`SetEditorRows`, the same move `submit()` makes when the message is laid out in
full), then paint the continuation **without a line break** (a complete line goes
through the commit path, which re-reads the row anyway and would repair the split,
hiding the defect the guard exists to catch).

- RED (pre-fix renderer): the band row holds `▌ the` and the continuation is one row
  BELOW the band, i.e. on the editing line's row —
  `▌ the` / `      **bold conclusion** follows`.
- GREEN (fixed): one band row carries `the **bold conclusion** follows`.
- The guard's message names the property, not the row: "no band row 0..8 carries … —
  the continuation was written to a row the layout no longer reserved for it".

### HONEST LIMIT — this fix removes ONE mechanism, the class SURVIVES

The acceptance for this fix is `go test ./cmd/agent/ -count=120 -run
'TestChatDump|TestChatReportDump'`, three runs. Measured: **run 1 `ok` (0 hits), run 2
one hit, run 3 one hit.** Both hits are the same shape as above but a DIFFERENT
mechanism:

| artifact row | head | blanked | tail |
|---|---|---|---|
| `▌ alpha and      and gamma` | `▌ alpha and` (0-10) | `beta` (11-14) | ` and gamma` |
| `▌ alpha     beta and gamma` | `▌ alpha` (0-6) | ` and ` (7-11) | `beta and gamma` |

The row keeps its FULL width and a CONTIGUOUS interior run is blanked — twice exactly
over the **bold-marked span** (`beta` in the first, `and` in the second). The
`attrs` map on both rows reads `0:40=default`, i.e. the erased run's cells are not
bold and not marked; the row is simply missing the middle of an otherwise complete
line. So the stale-live-row defect fixed above is **not** the only writer that can
blank a band row's middle.

Refuted along the way (recorded so it is not re-tried): "the fix's re-anchor repaint
re-creates the hole, and a CARRIAGE RETURN instead of EL would land in the second
half of a double-width cell". The re-anchor repaint passes a WIDE (`Cols` = 44-cell)
region while the `attrs` maps show the mis-shaped rows only at cols 40 — and 40 is odd,
so a wide char cannot straddle the boundary at all.

**The strongest clue — the frozen frame's TEXT and its ATTRS disagree.** In the run-3
hit the row's attrs read
`0:2=default 2:5=bold 7:5=default 12:4=bold 16:5=default 21:5=bold 26:14=default`
— that is exactly the emphasis map of the COMPLETE line
`▌ alpha and beta and gamma` (2-cell prompt bar, then the three bold spans) — while the
row's TEXT is `▌ alpha     beta and gamma`, missing the 5-cell ` and ` at cols 7-11.
So the frozen frame's styles describe a line its own characters do not spell. Two
readings survive, and they point at DIFFERENT subsystems:

1. **The frozen frame is a SPLICE of two frames** — text from an earlier frame, attrs
   from the finished one — which would be a defect of the DUMPER's emission (or of a
   snapshot taken without a single lock across both halves), not of the answer
   renderer. Run 2's OTHER hit (`TestChatDump_HalfRowDecidesWhenTheBreakArrives`, row 8
   text `▌ a` with attrs `0:40=default`) is a row TORN mid-write, which supports this
   reading: torn frames demonstrably reach the artifact.
2. **The renderer emits a span's STYLE without its text** (an abbreviated emphasis
   write), leaving the cells spaces with correct attrs — a renderer defect, but not the
   stale-row one fixed above.

The discriminator between them is cheap and was NOT run here (budget): freeze a frame
and compare the artifact's own `lines` block against its own `attrs` block for a TURN
THAT IS KNOWN COMPLETE — a splice leaves several rows inconsistent; an abbreviated
write leaves exactly one.

The remainder is DISPATCHED, not left to this page: task
`01a0debe-c985-7ec3-b9bd-fa03fd948728` (pending, tag `bug`, priority 1) carries this
section's two readings, the cheap discriminator and the three leads below. Read the row
with `gopi task show 01a0debe-c985-7ec3-b9bd-fa03fd948728`.

Remaining leads for whoever takes the remainder (this is a dispatch, not a shrug):
1. A band write that occupies fewer cells than the row previously held, where the
   ERASE leaves the row's later cells intact — a blank run bounded by the old and new
   lengths of the SAME span.
2. The per-column emphasis write path (`requireMarkedSpans` compares the finished grid
   per column): a span written column-by-column can leave the middle unwritten when
   the write is abbreviated.
3. The `dumper`'s replay of the byte stream — the paint is timed (a flush window), so a
   frame captured mid-burst is a candidate for a row with a hole; the gate's quiet
   window is what decides whether such a frame is ever frozen.

### Post-merge re-measurement (same day, fix + master merged)

The delivery-point merge landed `d30f1341` (`master` into this branch, 32 commits, zero
conflicts) and the SAME acceptance was re-run on the merged tree:
`go test ./cmd/agent/ -count=120 -run 'TestChatDump|TestChatReportDump'` → `FAIL 99.145s`,
**one hit**, `TestChatDump_QuotedFenceMarkerKeepsTheProseProse`. The frozen sink holds

```
008|▌ the                 follows        ← in BOTH the lines block and the grid block
attrs 008|0:40=default                   ← the whole row is default: no bold anywhere
```

So this hit's row loses the contiguous `bold conclusion ` run (cols 6-21) **and** carries
no emphasis at all, where the run-3 hit's attrs described the COMPLETE line. Two
attrs/text relations are therefore on record — (a) attrs complete / text incomplete, (b)
attrs default / text incomplete — and they are two entry points of the same
"a span written in pieces" family rather than one mechanism: (b) cannot be a splice of two
finished frames (a finished frame would carry the bold span), so at least one instance is
the renderer's own abbreviated emphasis write. That is an argument FOR dispatch, not a
reason to re-open this fix: the row this fix owns (the live row a burst was painting on)
is not the row this hit blanks, and the guard's interleaving is deterministic.

Transcript: `<session>/acceptance-postmerge.txt`.




## The seam the row-text/row-attrs disagreement actually lives on (2026-09-27)

The briefing for this follow-up framed the choice as `a SPLICE of two frames` vs
`an ABBREVIATED emphasis write`, and proposed deciding it by comparing the frozen
artifact's own `lines` block against its own `attrs` block. **That comparison
cannot decide it.** Both readings fit it: attrs-complete/text-incomplete is what
a whole-row emphasis write over a stale row produces, AND what a splice produces
when the emphasis map was written and the cells were not. The artifact was read,
and it stayed ambiguous.

**What decided it: tapping the paint stream.** The bytes were logged as they
reached the pty (`<session>/evidence/tap2-1500.txt`), by chunk index, and the
damaged row's neighbourhood was read directly. Chunks 592-604, verbatim
(`ESC` rendered as `CSI`):

```
│ ▌                        <- the gutter cell, chunk 592
│ CSI 10;3H                <- place at row 9, col 2
│ CSI 1m alpha CSI 0m      <- the first span's text
│ CSI 10;8H
│  and
│ CSI 10;13H               <- place at row 9, col 12  …… and NOTHING
│ CSI 11;1H CSI 2K CSI 1m CSI 32m "you>" CSI 0m CR CSI 5C    <- the READ LOOP repainting the editing line
│ CSI 1m beta CSI 0m       <- the span's TEXT, on the EDITING LINE's row
│ CSI 10;17H
│  and
│ CSI 10;22H
│ CSI 1m gamma CSI 0m
```

Read it off the stream: cols 7-11 (`beta`) were **never painted on the band row**,
and the bytes carrying them went to row 10 — the editing line — where the read
loop's own repaint (`CSI 2K` on that row) erased them. So the frozen artifact held
`▌ alpha     beta and gamma` with attrs
(`0:2=default 2:5=bold 7:5=default 12:4=bold 16:5=default 21:5=bold 26:14=default`)
spelling the **complete** line — the placements had all been written, the emphasis
map came from them, and the cells of one run had not.

**Verdict: it is a SPLICE — of two WRITERS' chunk boundaries inside ONE frame.**
Neither reading in the briefing was exactly right, and the attrs block could not
tell them apart, which is why the earlier pass concluded (b) and was wrong.

### Root cause and the seam fix

The answer renderer, the `region` and the `Renderer` all share **one** byte stream
with the editor's read loop, which repaints the editing line and the status row
between any two `write(2)` calls. Every `place(...)` emitted as its **own** write
was therefore a window the loop could land in, and the text it was positioning
then went to whatever row the repaint left the cursor on. Not one call site — the
row head, the gutter, the live-row clear, the status row, the menu rows, the band
scroll carry.

The fix is at the **seam**, never at the arithmetic: the placement and the bytes it
positions ride ONE write.

| layer | helper | file |
|---|---|---|
| answer renderer | `placeAndWrite` / `placeAndWritePrefixed` | `internal/lineedit/answer.go` |
| region | `emitAt` | `internal/lineedit/region.go` |
| renderer | `ControlAt` | `internal/lineedit/render.go` |

**A trap worth recording:** the byte order inside `placeAndWritePrefixed` is
`PLACE, prefix, text` — not `prefix, PLACE, text`. A prefix-first composition makes
the erase clear *whatever row the cursor happened to be on*; it broke four existing
tests (`TestA5B_ScrollRegionNeverMovesTheChrome`,
`TestA5B_CommittedLineSurvivesInScrollback`, `TestA5B_ChromeHoldsUnderALongAnswer`,
`TestEditorCloseOutClearsTheReservedLayoutAndParksTheCursor` — the editing line lost
its `you> ` prompt) and was caught by them, not by the new guards.

### The guard is CONSTRUCTED, not sampled

The defect is a race: sampling it costs about **1 hit per 360 iterations**
(measured post-`697f30c3`: 3×120 → ok 109.6s, ok 116.4s, FAIL 121.6s,
`TestChatDump_ArtifactCarriesTheBandAndItsIdentity`). A flake-rate drop is therefore
not evidence. `internal/lineedit/answer_placement_test.go` constructs the
interleaving instead:

- `TestNoAnswerWriteIsABarePlacement` — a writer wrapper HOLDS every bare
  placement; when the next write is unplaced cell-drawing text (the production
  window), it delivers the held placement, runs the read loop's repaint, then the
  text. The assertion is that **zero** windows exist. RED: 13. GREEN: 0.
- `TestAnswerWriteIsAtomicWithItsPlacement` — splices a real `ed.repaint()` at every
  window and asserts no band row is broken and the editing line does not carry the
  answer. RED: the span's text on the editing line's row. GREEN: pass.

Both were observed RED before the fix and GREEN after, in one session, by patching
exactly the emission seam (placement written on its own) and reverting it.
