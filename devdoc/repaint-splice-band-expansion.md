<!-- doc-class: historical -->
# The repaint splice: the band's expansion had no erase, so an answer landed on the editing line's old row

## What the operator saw

Dump line 114 (`/tmp/error.txt`, `gopi chat`, 2026-09-27), verbatim:

```
113| ▌ 另外提醒一件无关但有风险的事：fetch 时 git 警告 `There are too many unreachable loose objects` 且 `.git/gc.log` 存
114| ▌ 在 —— 自动 gc 已被禁用，仓库会持续膨胀。要我跑一次 `自动 gc 已被禁用**，仓库会持续膨胀。要我跑一次 `git prune` / gc
115|   吗？
```

One sentence laid down twice, its tail spliced over different content, leaving
3 opening backticks and 1 closing, and an unpaired `**`.

## The layer, and how it was named

The deliverable asked for a three-channel comparison (transcript / dump / screen
copy) and a statement of which channel is wrong. The evidence, in the order it
was obtained:

1. **The transcript is NOT wrong — and the dump is NOT a generic "dump writer"
   either.** The agent's answer text is one string in the session log; the dump
   renders that string. A corrupted dump row whose corruption is *positional*
   (a splice, not a text change) cannot have been produced by rendering the
   string correctly.
2. **The renderer IS the writer.** `internal/lineedit` is the only component that
   emits cursor placements, and its own model of the screen (`Screen`) is
   deterministic — replayed byte for byte, it reproduces the operator's shape
   exactly (see the RED output below, row 20).
3. **A brute-force search of the LIVE path found zero duplicates** (the
   operator's own paragraph, at 78/80/117 columns, over every burst cut and delta
   size from 1 byte to whole-flush: 0 spliced rows). So the defect is NOT in the
   streaming/live composition — it is in the LAYOUT TRANSITION.
4. **The transition that fires it: the band EXPANDS.** `region.layout()`
   (`internal/lineedit/region.go`) moves the band's bottom edge for two different
   reasons — the editing line grows (band SHRINKS) or shrinks (band EXPANDS) —
   and only ONE direction carried its erase:

   - **SHINKS** — already fixed (2026-09-26, session 01a0dd28): one LF at the
     band's live row promotes every committed row up, so the rows given away are
     left blank. The code says why, in a comment.
   - **EXPANDS** — nothing. The band's live row MOVES DOWN onto a row the
     editing line was using, and that row still holds the editing line's last
     paint. The editing line erases each of its rows with a WHOLE-ROW erase
     before drawing it (`render.go`: `escEraseLine` then the text), so a retained
     row can hold text across its **entire width**.

   The answer's next burst then writes onto that row. An answer write APPENDS at
   the row's live column, so it REPLACES the head of what is there and stops
   where its own characters stop — the old run's tail survives and the two texts
   read as one line. The old run was the editing line's own text, inline markup
   and all, which is why the reader sees a sentence twice with unbalanced markup.

The operator's own sequence is the ordinary one: the message is submitted, the
input is empty, the editing line's row wish drops from 2 back to 1, the band
takes the row back — and the answer begins streaming onto it.

## The fix

`internal/lineedit/region.go`, `layout()`: the band's expansion is now the exact
counterpart of its shrink carry.

```go
if gained := p.EditorTop - was.EditorTop; gained > 0 && was.EditorRows > 0 {
    r.emitTo(r.clearRowsSeq(was.EditorTop, gained))
}
```

- **The rows are the arithmetic, not a guess.** `EditorTop == BandTop + bandRows`,
  so the editing line's own movement IS the band's growth: the rows handed over
  are exactly `was.EditorTop` .. `p.EditorTop-1`.
- **The erase is the whole row** (`escEraseLine`), never `escEraseToEnd`. This
  package bans erase-to-EOL for display code (it leaks under `patch_stdout`), and
  a whole-row erase is also what makes the pad-width invariant hold for *every*
  replaced width at once: the cleared span is at least as wide as anything the
  previous owner of that row could have drawn.
- **One write.** `clearRowsSeq` composes each placement together with the erase it
  positions, because the read loop repaints the editing line between any two
  writes (the seam every emission in this package is composed to avoid).
- **`was.EditorRows > 0`** excludes the degenerate plan the region holds before its
  first `Enable` (`EditorRows` 0, `EditorTop` 0): there the "handed over" rows are
  the whole screen, which the caller has not drawn yet and this function must not
  wipe.

The "pad width must equal the width of the text being replaced" rule now holds for
this writer *by construction*: the pad covers the whole row.

## The tests (`internal/lineedit/answer_repaint_splice_test.go`)

| test | claim | RED on master |
|---|---|---|
| `TestRepaint_BandExpansionDoesNotSpliceTheOldRow` | the reproduction: the settled frame holds the answer, not the spliced line | yes — row 20 `"我在 —— … 仓库会持续膨胀。 之后它那一行归了 band TAILMARKER"` |
| `TestRepaint_BandExpansionClearsTheWholeReplacedWidth` | the INVARIANT: for every replaced width (1..84 cells) a *shorter* replacement leaves nothing of the old text | yes — all 6 widths |
| `TestRepaint_BandExpansionNoIntermediateFrameSplices` | FRAME-LEVEL: no intermediate frame holds a row carrying both texts | yes |
| `TestRepaint_BandExpansionKeepsOneTurnMarkerPerRow` | no row ends up with the turn marker twice (`▌ ▌`) | (passes pre-fix — a guard for the shared symptom, see below) |

The frame-level test is the one the brief insisted on: it walks **every write** the
renderer emits, replays the screen after each one, and fails on the FIRST frame
whose grid carries the replaced text and the replacing text on one row. A
final-state-only assertion passes while the splice is still visible to a human
watching the screen; this one cannot.

## Verdict on the duplicate `▌ ` markdown-table sighting (`answer.go:1858`)

**NOT this root cause.** That sighting is already closed and its mechanism is
different: `writeGutter` records it as the gutter being carried into the row's
BODY and thus replayed as body cells — a `liveText` carry bug that was fixed by
keeping the gutter out of `liveText`. A duplicate glyph on a row is *content
written twice*; the splice here is *stale content never cleared* — one is a
double-write, the other a missing erase. The guard is shared, though: the
`KeepsOneTurnMarkerPerRow` assertion in this file now fails if the expansion
regression ever makes the marker visible twice on one row.

Evidence that they are separate: on master, with the fix reverted, the marker
assertion is **green** while all three splice assertions are **red** — one root
cause would move them together.

## Honest limits (## Not Done)

- The `gopi chat` transcript for the operator's exact session was not located on
  this host, so the transcript-vs-dump comparison is answered by mechanism and
  measurement rather than by that one file: the search of the live path found
  zero duplicates, and the layout transition reproduces the shape deterministically.
  What is proven is *which layer writes it* — the renderer's layout transition.
- The end-to-end path (`cmd/agent` chat → real pty → dump) was not driven with a
  rebuilt binary: the session budget did not leave room, and the defect is fully
  reproduced and fixed at the package boundary that owns the bytes.
- The fix clears rows the band acquires. It does not (and cannot) recover text that
  a *previous* such transition already destroyed on a running screen.

## Verification

- Build: `go build ./...` → OK
- Tests: `go test ./internal/lineedit/ -count=1` → ok (8.0s);
  `go test ./internal/standards/ -count=1` → ok (24.5s)
- RED: `go test ./internal/lineedit/ -run 'TestRepaint_' -v` on master
  (region.go reverted only) → FAIL, 3 tests + 6 subtests; output archived at
  `$SESS/red-on-master.txt`
- GREEN: same command with the fix → `ok`; archived at `$SESS/green-after-fix.txt`
