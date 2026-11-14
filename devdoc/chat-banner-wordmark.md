<!-- doc-class: historical -->
# The chat startup screen's wordmark — a 3D "GO" / "GOPI" logo

**Task** `01a0ff92` (2026-10-03). **Branch** `task/01a0ff9223fa7853901719bab6a75a6c`.
**Files** `cmd/agent/chat_banner.go`, `cmd/agent/chat_banner_test.go`,
`testsuite/chat_banner_pty_test.go`.

## What was asked

The operator pointed at the reference agent CLI's startup banner — an ANSI-Shadow
style 3D wordmark of that product's own name — and asked for a designed 3D mark
for gopi in that spirit: *"maybe we can design a 3D GO logo"*.

## What was there, and what it cost to change it

The startup screen (`chat_banner.go`, task `01a0e7ed`) drew a **flat** mark: a
g-block inside a rounded cell, 15 cells wide, 7 rows tall, using the block and
half-block vocabulary the chat chrome already uses (`▄ █ ▛ ▘ ▐ ▀`).

Two constraints decided the whole design, and both are measurable rather than
stylistic:

1. **The mark column's floor is derived from the art.**
   `chatBannerLeftColMin = 22` is documented as *"the widest row of the logo art
   plus a cell of headroom on each side"* — and the art it was derived from was
   15 cells wide. A new mark WIDER than 20 cells silently breaks that relation:
   the constant would still say 22 while the art demanded 24, and on the
   narrowest box that yields a box the geometry claims to support, drawing a mark
   two cells too wide.
2. **The mark may never be cropped, and every art row costs a screen row.**
   `chatBannerArtFits` drops the WHOLE art when the column cannot hold it, and
   the row budget is spent by the mark (`chatBannerArtRowsMax = 7`). A taller or
   wider mark is not decoration plus decoration: it changes what survives on a
   short terminal.

So the first delivered drop-in candidate (a 19-cell two-letter mark in the
classic box-drawing family, ` ██████╗  ██╔═══██╗ …`) was **rejected on
geometry** before any aesthetics were discussed: at 19 cells it is fine, but the
same construction spelled "GOPI" measures **35 cells**, i.e. it cannot share the
column with the panel on any terminal that draws the box at all
(`inner = cols - 7`; the panel takes 46-71 of it).

## What the final mark is

Two variants of ONE design, in the block vocabulary this interface already uses,
chosen by the room the column has (`chatBannerLogoVariant`):

- **short — "GO", 20 cells wide × 7 rows** — the floor the two-column box is
  built on. `chatBannerLeftColMin = 22` stays true without being touched: 20 +
  one cell of headroom per side.
- **wide — "GOPI", 33 cells × 7 rows** — the same construction spelled in full,
  drawn when the mark column can hold it (measured: it first appears at
  `cols = 109`; a run with an identity fact narrows the panel's slack, so the
  cutover moves with the panel, which is why the test MEASURES it rather than
  computing it).

The 3D reading comes from two properties, and the second is the one that took
the work:

- **the face is the full block `█`** — the letter is solid matter;
- **the shadow is the medium shade `▓`, cast one cell down-right and MASKED TO
  THE OUTSIDE OF THE LETTER.** The mask is what stops the shadow from crossing a
  counter (the hole in the "O", the mouth of the "G"), so the letters stay
  legible at a glance. Without the mask the cast lands inside the letter and the
  glyph turns to noise — which is what the first generator attempts produced.

The shade (not the darker `▒`) is deliberate: the block vocabulary already spends
the darkest cells on the face, so a `▓` cast against a `█` face is a grey plate
standing off a black one — the contrast the terminal can actually render, and the
same reason the reference banner uses a line-art shadow against a solid body.

## How the art was produced (and why the generator is not in the repo)

The art is a **committed literal** (`chatBannerLogoArt` /
`chatBannerLogoArtWide`), not a runtime render: no new dependency, no font, no
script in the tree (Rule 35). The generator that produced it (bitmap per letter →
masked down-right cast shadow → cell grid) ran from the run's session evidence
directory; the recipe is recorded here so the art can be regenerated:

- letter bitmaps: 8×6 cells ("G", "O", "P"), 4×6 ("I"), body `#`;
- cast: shadow = body shifted (+1 row, +1 col), minus the body itself, kept only
  where the cell is reachable from the image border through non-body cells (a
  flood fill) — that is the "masked to the outside" rule;
- placement: letters joined with 2 cells between them for "GO", 1 cell for
  "GOPI", so each variant comes out 20 and 33 cells wide at the same 7 rows.

## Verification

See the task report (`~/.gopi/sessions/01a0ff92-…/task-report.md`) for the
build/test/run/result block. The screen evidence is the run's own:
`screen-100.txt`, `screen-117.txt`, `screen-133.txt` and their PNG renders, in
that session directory — captured from the REAL binary on a real pty at three
terminal widths, and read back with a cell-grid assertion (the image, not the
byte stream).

## Limits (honest)

- **The wide variant is unreachable on the operator's own terminal.** Measured:
  117 columns draws the SHORT mark (the wide one needs 109 columns *for the mark
  column*, which a real 117-column terminal does not have once the panel takes
  its own 46-71 cells). The wide variant earns its place on 130+ column
  terminals, where the short mark would strand a half-empty column.
- **The e2e case `TestChatPTY_StartupScreenShowsTheLogoAndItsSidePanel`** uses
  the mark's art row as its on-screen marker; that literal was updated with the
  art. The pty dump path (`--dump-screen`) had a recorded defect (`01a0ff97`:
  the startup screen does not reach the dump's cell model); this run did not
  re-litigate that, and used the raw pty stream plus a cell-grid read as its
  evidence instead.
