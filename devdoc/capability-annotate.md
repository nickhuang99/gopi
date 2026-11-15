<!-- doc-class: normative -->
# `gopi annotate` — drawing marks onto an existing PNG

**Status**: implemented (`cmd/agent/cli_annotate.go` + `internal/text2img/annotate.go`),
chain link 7/9 of the capture capability (`capdemo-6`).
**Spec**: `devdoc/capability-annotation-handoff.md` (written by `capdemo-5`).

## What it is, in one paragraph

`gopi annotate <in.png> --out <out.png> --mark …` draws rectangles, points,
arrows and text labels onto a PNG that **already exists**, and writes a **new**
file. The input is never modified. It is the compositing counterpart of
`gopi capture`: capture *records* what the screen showed, annotate *points at*
something in a record — so the two can never be one verb, and a captured record
that drew on itself would not be evidence. There is no new tool (Rule 3: the
toolset stays at the fixed 17); annotate is a CLI verb reached through `bash`,
the same shape `gopi capture` and `gopi text2img` have.

## The geometry, in one rule

**Geometry is in OUTPUT-IMAGE PIXELS**, spelled `WxH[+X+Y]` — the exact grammar
`gopi capture --area` accepts (`shot.ParseArea`), because two verbs that take a
rectangle must take it the same way. The one exception is the **cell anchor**
(§The cell anchor), which is the point of the feature for a terminal surface.

```
kind:geometry:label:anchor          # four colon-separated fields
rect|arrow|point                    # kind
WxH[+X+Y]                           # geometry, output pixels
free text, spaces allowed, or empty # label
fixed | <row>#<col>[-<row>#<col>]   # anchor ("fixed" and "" mean the same thing)
```

A field may not contain a `:`; free text with a colon in it is not expressible
today.

### The three kinds

| kind | what it draws | pixel-level definition |
|---|---|---|
| `rect` | a rectangle **outline** | `annotateStrokeThick` (2) nested outlines, each grown **outward** by 1px — an inward stroke would erase a 2px-tall box |
| `point` | a **filled** box | every pixel of the declared rectangle |
| `arrow` | a shaft + a two-barb head | the shaft is the integer Bresenham raster of the box's **top-left → bottom-right** diagonal (each raster column plus the cell below it, for the same 2px stroke); the head is two `annotateHeadPx` (8px) barbs at ±`annotateHeadAngleDeg` (25°) from the tip |

An arrow runs along its box's **diagonal**, so a flat arrow is spelled as a
1px-tall box: `arrow:120x1+40+90:shrinks:fixed`.

### The label, and where its chip goes

A label is rendered through `text2img`'s own font chain and glyph blit (see
§Why this lives in internal/text2img). The chip is **opaque white with a
mark-ink border**, sized from `Face.Cells` — the package's own cell
measurement — plus the named padding constants. The chip sits **`annotateLabelGap`
(6px) ABOVE the mark's box**, and drops **below** it when there is no room
above. Both placements are deliberate: a label drawn *on* the thing it
describes hides the evidence it exists to explain.

### Ink colour

The mark ink is one opaque colour (`MarkInk`, RGB 220/30/30) and the label text
is black. **The package's own `blend()` is NOT used.** `blend()` derives grey
from glyph coverage (`return color.RGBA{v, v, v, 255}`) because it composites
black ink over the white paper the prose path allocated — correct there, wrong
for a photograph. `annotateBlend` keeps the ink's RGB and scales only its
coverage. `TestAnnotateInkNotGrey` pins it: under a pure-blue ink, **zero** inked
pixels may have R==G==B.

## The refusal contract

Every one of these is a **non-zero exit** with the named reason on stderr, and
**nothing is written** — a refusal must not leave a file that looks like a
result.

| # | condition | message carries |
|---|---|---|
| 1 | `--out` missing/empty | `--out is required: annotate writes a NEW png and never edits the input in place` |
| 2 | `--out` is the input | `is the input itself: annotating in place would destroy the evidence the mark points at` |
| 3 | missing / unreadable base | `cannot read the base image <PATH>: …` — **the path is named** |
| 4 | base is not decodable as PNG | `cannot decode the base image <PATH> as PNG: …` — the path is named |
| 5 | base is empty (0×0) | `is 0x0 — an empty canvas cannot carry a mark` |
| 6 | no `--mark` at all | `no --mark given: nothing to draw` |
| 7 | a mark is malformed (wrong field count, unknown kind, bad geometry, unknown flag, non-numeric size) | the offending spec is quoted back, and the grammar is printed |
| 8 | a mark's geometry is not **fully inside** the image | `<kind> <WxH+X+Y> does not fit the <W>x<H> image: the rectangle must be fully inside it, never clipped` |
| 9 | an anchor is a cell reference but no grid is resolvable | names the missing dump and the `--display` escape |
| 10 | the grid is a **FALLBACK** geometry | quotes `uiclient.GeometryFallbackNotice` |
| 11 | the grid carries no cells | `carries no cell grid (cols=… rows=… rows_of_cells=…)` |
| 12 | a cell reference is malformed / outside the grid / backwards | the reference is quoted and the grid's size is stated — **never clamped** |
| 13 | the label's chip fits nowhere in the image | the computed chip box and the image size |
| 14 | the font chain has no glyph for the label | `refusing to write a chip with no text in it` |
| 15 | N marks requested and **0 pixels changed** | `N mark(s) requested and 0 pixels changed — refusing to write <OUT>` |

### Why clipping was refused (and wrapping was refused too)

**Clipping, refusal #8.** The precedent is `gopi capture --area`, whose
off-screen rectangle is refused by name (`internal/shot/x11rect.go`: *"the
rectangle runs past the root window: %s does not fit %dx%d"*). The reason is the
one this whole capability is about: **an annotation is evidence.** A silently
clipped box does not say "I meant a bigger box" — it says something false about
where the thing is.

**Wrapping, refusal #13.** The label arm of the spec allowed *refused-or-wrapped*,
pick one and justify it. **Refused** was chosen: wrapping silently changes the
text from what the caller typed (inserting line breaks the caller did not write)
and changes the chip's height, which moves the geometry the caller *did* write.
A refusal leaves the caller's own command line as the single fix, which is the
same contract as every other refusal here. The message names the chip's computed
box, so the fix is arithmetic the caller can do, not a guess.

## Why this lives in `internal/text2img`

Every element of an annotation except integer pixel writes is a **glyph** — the
label — and this package already owns a glyph rasteriser (the font chain, the 4×
supersampled SFNT rasteriser, and the blit that takes its target as a
parameter). So the annotator is a new file in the package that owns the painter,
never a second rasteriser and never a new dependency.

Two things had to be **exported**, and only two:

- `Ink` + `DrawText(img *image.RGBA, text string, penX, baseline float64, sizePx int, font string, ink Ink) int`
  — the blit. It has to be exported because the package's public entry points
  (`RenderFile`, `RenderGrid`) each **allocate their own image**, so no caller
  can hand them one; annotating an existing PNG needs a painter that takes the
  canvas. `DrawText` returns the pixel count it inked (never zero on success —
  the package's own measured-ink convention).
- `GridCellRect` — the cell→pixel derivation, exported so it can live **beside
  `gridImage`**, the renderer whose layout it describes (see §The cell anchor).

Everything else (`drawChainGlyph`, `rasterInto`, `loadChainFromPaths`,
`loadFontChain`) stays unexported.

**Rule 35 — the four statements.** *What breaks without it*: n/a, nothing was
created. *What persistent dependency was added*: **none.** No module
(`go.mod` untouched), no new external command, no committed font or binary blob
(the label's font is the system font `text2img` already autodetects), no state
file, no daemon, no cron line, no config key. The verb reads PNG in, writes PNG
out, and the whole feature is two Go files plus tests. `git status --porcelain`
is empty at hand-over and `go.mod` has no new line.

## The cell anchor

A terminal screenshot is a grid of cells. `--mark …:12#4-12#30` says "cells 4 to
30 of row 12", and the verb converts it to output pixels. **`--display PATH`
names the dump** whose cell grid the anchor is measured against; with no
`--display`, the verb performs the same discovery `gopi ui_state` does (the
newest `*-screen.dump` whose recorded pid is alive) and reports the dump it used
as `grid_path` in its result.

### The conversion is the renderer's own layout — this was a real defect, not a claim

`text2img.GridCellRect(row, col, rowEnd, colEnd, scrollbackRows, GridOptions)`
is the ONE cell→pixel derivation, and it lives **beside `gridImage`** so the
arithmetic that *places* a mark and the arithmetic that *lays a grid out*
cannot drift apart. Its four terms are the renderer's own: the page **margin**,
the **row pitch** (`Lh`, defaulting to `size × lineHeightRatio`), the **cell
advance** (`CellW`, defaulting to `size / defaultCellWidthDivisor`), and the
**scrollback band** (the history rows **plus the one rule row that closes
them**).

**The first implementation of this verb omitted the page margin.** It computed
`col*cellW` / `row*cellH` and put every cell mark `DefaultMargin` (12px) up and
left of the cell it named. The defect was found by the calibration test, then
fixed at the source, and the test that found it stays:

```
# RED — GridCellRect with the margin term dropped:
--- FAIL: TestAnnotateCellAnchorHonoursThePageMargin
    cli_annotate_test.go:143: cell 0#0 resolved to (0,0) — the page margin 12 is missing from the conversion
--- FAIL: TestAnnotateCellAnchorE2E
    cli_annotate_test.go:467: the anchored cell (20,32)-(30,64) contains only 0 of 75 glyph pixels (floor 0.80)

# GREEN — margin restored:
ok  github.com/the operator/gopi/cmd/agent
```

### The scrollback band is declared, never inferred

`--scrollback-rows N` (default `0`) says how many history rows the **image**
carries above the screen. It cannot be read off the PNG, and it must not be read
off the dump: the dump describes the *sink's buffer*, which may have moved on
since the image was made. The safe default is `0` — "the image starts at the
screen's first row", which is what a `gopi text2img` grid render or a plain
`gopi capture` produces. `render.go`'s band is `len(scrollback)` rows closed by
**one rule row**; `--scrollback-rows 4` therefore moves a cell anchor down by
`5 × Lh`.

## Verification: pixel-level, never OCR

A render claim is settled by **comparing pixels against an independently
computed ground truth**, never by OCR and never by a vision model — neither can
measure whether a stroke landed where it was declared (Rule 10.2,
`devdoc/text2img.md`).

**The ground truth is built in the test, sharing no code with the annotator.**
`annotateRefRect` / `annotateRefLine` (in `internal/text2img/annotate_test.go`)
are plain integer loops over a fresh image; the comparison is an **IoU over ink
masks** with the floor named as the exported constant
`text2img.AnnotateIoUFloor = 0.95` — one definition, quoted by this document and
asserted by the test (Rules 13/14).

**The cell anchor is calibrated against the RENDERER, not against arithmetic.**
`TestAnnotateCellAnchorE2E` renders a fixture grid through the real
`text2img.RenderGrid`, anchors a mark to the one cell that carries a glyph, and
asserts four things about the resulting image:

1. the mark's ink box **equals** `GridCellRect` for that cell, grown by the
   stroke's inner ring;
2. the anchored cell contains ≥ `annotateAnchorCoverFloor` (0.8) of the glyph's
   dark pixels — measured on this fixture: **68 of 75**, the 7 outliers being
   the `X`'s outermost columns spilling past the 10px cell;
3. **every rival placement is strictly worse**, and the pre-fix
   (margin-shifted) placement contains **0** — a conversion error cannot pass
   by luck;
4. the mark changed no pixel outside its own cell.

A glyph box is up to one cell wide plus its bearings, which is why (3) is not
just "worse" but *strictly* worse: an exact-coverage assertion would be false
and a loose one unfalsifiable.

## Commands

Without `--json` the verb prints a one-line summary (`✅ in.png → out.png  WxH
marks=N labels=N 墨迹=N 像素 anchor=…`); `--json` is the machine-readable shape.
The default is OFF for `--json`, deliberately: the one-line form is what a
walkthrough reads, and the JSON carries the fields a script asserts on.

```bash
# fixed marks on a real still
gopi annotate still.png --out marked.png \
  --mark "rect:200x40+14+13:status row:fixed" \
  --mark "arrow:120x1+300+120:shrinks like this:fixed" \
  --mark "point:8x8+640+360::fixed"

# cell anchors against a named dump (repeatable --mark; --json for the result)
gopi annotate base-grid.png --out marked-grid.png \
  --display ~/.gopi/sessions/<id>/chat-screen.dump --scrollback-rows 0 \
  --mark "rect:1x1+0+0:the header row:0#0-0#116" \
  --mark "point:1x1+0+0::3#10" --json
```

```json
{ "tool": "annotate", "path": "marked-grid.png", "input": "base-grid.png",
  "width": 2406, "height": 888, "marks": 2, "labels": 1, "ink_pixels": 8034,
  "anchor_source": "cell", "grid_cols": 117, "grid_rows": 26,
  "grid_path": "…/chat-screen.dump", "grid_geometry": "layout",
  "cell_w": 10, "cell_h": 32 }
```

`ink_pixels` is **measured** (the count of pixels this run changed, against the
input bytes), never claimed — the same shape as `text2img.Result.Ink`. It is
what makes refusal #15 possible, and it is the field a caller checks to know
that the file it just got is not a copy of its input.

## Tests, and where the red/green evidence is

```bash
go test ./internal/text2img/ -run TestAnnotate -count=1     # engine: 10 tests
go test ./cmd/agent/ -run TestAnnotate -count=1             # CLI contract + E2E
go test ./internal/standards/ -count=1                      # all six gates
```

The engine tests carry: the export's ink measurement, the ink-not-grey
regression, IoU-vs-ground-truth for rect/point/arrow, the
changed-pixels-are-declared property, the chip-refusal, the off-image refusal
**with an in-image control**, the refusal table, the non-PNG base, determinism
(byte-identical over three runs), and the label path. The CLI tests carry the
grammar table, the malformed-spec table, the band knob, the grid refusals, and
the end-to-end calibration.

### Close-out verification: this verb's own wiring was fixed on 2026-09-29

The first landing of this verb passed its engine and pixel tests but left four
WIRING contracts red, and the full-suite run that would have caught them was
never collected. They are fixed on the same branch now, and each one is the
reason a new verb is not done until the whole surface agrees:

- `topLevelCommandNames` (`cmd/agent/cli_completion.go`) — `gopi <TAB>` did not
  offer `annotate`, which `TestTopLevelHelpCoversEveryDispatchedSubcommand`
  reports as "completion would suggest a verb the help does not explain".
- `helpWordOfPredicate` (`cmd/agent/usage_test.go`) — the dispatch predicate
  `AnnotateSubcommand` had no help mapping.
- the cliVerbs constant list in `cmd/agent/cli_contracts_test.go` — the contract
  map's `annotate` key was not enumerated as a constant.
- `devdoc/gopi-verb-census.md` — the census had no `annotate` row at all, and
  the row now reads `a:test`: `TestAnnotateVerbRunsARealArgvEndToEnd` hands the
  verb an argv literal whose first element is the verb word and calls its run
  function, the `capture` precedent. The row's counters come from the generator
  (`scripts/g7-verb-census.sh ./bin/gopi`), never from hand arithmetic.

Measured on master at the same moment (detached worktree, 9cdf65a3): six
`cmd/agent` failures are PRE-EXISTING and unrelated to this verb —
`TestToolsMaintenanceHelpSurface{InventoryMatchesDerivation,EveryFormIsTestedOrBlockedWithAReason}`
(the text2img `--size 28` form; filed as bug task `01a0ebf9-172c-7411-a8f5-9ce4f8b41b7f`),
`TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer`,
`TestLedgerOpenersUseProductionDSNContract`,
`TestSlashMenuOneCandidateStatesWhyTheArrowCannotMove` and
`TestMergeEveryKilledLoopLeavesNoResidueInTheProfile`. Both the branch and
master fail them identically, which is what "pre-existing" has to mean to be
worth writing down.

## Not done / known limits (honest)

- **A `:` cannot appear inside a label** (the `--mark` grammar is
  colon-separated with 4 fields). Escaping was not added; the refusal quotes the
  spec so the caller can see why.
- **The arrow head is a visual approximation.** Its barbs are drawn with the
  same line routine, and the tests assert the shaft exactly and the head's ink
  as a *bounded extra* (≤ 4 × 8 × 2 px) — not a barb-by-barb IoU. An exact head
  model was not specified, so nothing exact was claimed.
- **A labelled mark's chip lies outside the mark's own rectangle** (above, or
  below when there is no room above), which means "every changed pixel is inside
  the declared mark" is only asserted for label-free marks. For labelled marks
  the chip is separately bounded by refusals #13 and #14.
- **The cell anchor is calibrated against a `text2img` grid render**
  (`CellW = size/2`, `Lh = size×1.6`, `Margin = 12`). A capture rendered with
  different `GridOptions` would need those options passed; today the verb exposes
  `--size`/`--lh` (which reach `GridCellRect`) but **not** `CellW`/`Margin`, so a
  grid rendered at a non-default cell width or margin cannot be anchored
  correctly. Declaring a band and a margin is the obvious next step and is *not*
  in this link.
- **No `--mark` from a file / stdin**, and no JSON input for marks. Repeatable
  `--mark` is the only channel.
- **The pre-existing red on master is untouched**: `TestToolsMaintenanceHelpSurface*`
  fails because `gopi text2img report.txt --out out.png --size 28` is advertised
  by `text2img -h` but absent from the g6 inventory. It was red before this
  branch and is red after it; `annotate` was added to the inventory derivation
  cleanly (`TestToolsMaintenanceHelpSurface`'s two failures name *text2img*
  forms, not the annotate row). Fixing it would mean re-running
  `scripts/g6-tools-maintenance-help-surface.sh` — a different surface, and not
  this link's deliverable.
