<!-- doc-class: normative -->
# Task Report: per-cell SGR attributes for the vt100 Screen (V1)

A past run's record, frozen by nature: what this branch implemented, how it was
checked, and what it deliberately left out. The claim it makes about the code is
held by the tests named here — run them, do not trust the prose.

## Completed

- **`internal/sgr` (new leaf package).** The terminal's SGR vocabulary in one
  place (rules 13/14): parameter numbers (`Reset`, `Bold`, `Dim`, `Italic`,
  `Underline`, `Reverse`, `FgBase/FgLast`, `BgBase/BgLast`, `ExtendedFg/Bg`,
  `DefaultFg/Bg`, `BrightFg/BgBase/Last`), the colour spaces (`Palette16`,
  `Palette256`, `ChannelMax`, `ColorModePalette/Truecolor`, `DefaultColor`,
  `NoRGB`), the named palette (`Black`..`White`, `BrightBlack`..`BrightWhite`),
  the CSI punctuation and final bytes (`CSI`, `CSIStart`, `ParamSeparator`,
  `ParamDigitMin/Max`, `FinalByteMin/Max`, `FinalSGR/FinalCUP/FinalCUU/…`), the
  sequence builders, the **`Style`** value, and the **parameter interpreter**
  (`ApplyParam`). Stdlib only, imports nothing from the project.
- **`internal/lineedit/screen.go` — the grid records SGR.** A `Style` per cell
  (a second grid parallel to the runes) plus a `pen` — the style in force that
  new cells are written with. `case 'm'` no longer discards: the CSI parser's
  parameter slots are folded through `sgr.ApplyParam`. Every parser primitive it
  compares bytes against now comes from `internal/sgr`.
- **`internal/lineedit/screenattr.go` (new) — the read API.** `Cell(row,col)`,
  `Attr(row,col)`, `StyledLine`, `StyledContents`, `CellOf`/`CellsOf`, `CellKind`.
  Plain `Line`/`Contents`/`Scrollback`/`ScrollRegion` signatures are unchanged.
- **Wide-rune column model.** A wide (East Asian wide/fullwidth) rune occupies
  TWO columns: the rune in the first, a continuation marker in the second, both
  carrying the pen's style; the cursor advances two. `Line` skips the
  continuation column, so the row's TEXT is identical to before.
- **Tests.** `internal/lineedit/screenattr_test.go` (12 tests, including the
  epic's `\x1b[31mred\x1b[0m plain` probe asserted as attributes),
  `internal/sgr/sgr_test.go` (11 tests: wire-format pinning, style round-trip,
  incremental/truncated parameter semantics, RGB packing).
- **This report + its `devdoc/DOC-INDEX.tsv` row** (the corpus gate requires
  every devdoc file to be classified).

## Design decisions worth writing down (rule 7)

1. **`Style` lives in `internal/sgr`, not `internal/lineedit`.** SGR is a
   protocol with two sides: something EMITS it and the grid PARSES it. A number
   or spelling typed on both sides is two sources of truth for one wire format —
   the emitter writes `2` and the parser reads `1`, and nothing fails until a
   terminal shows the wrong colour. One package holds the number, its meaning,
   the sequence and the state.
2. **The wide-rune column model is a change in the GRID's cursor arithmetic —
   deliberate, and it removes a disagreement rather than adding one.** The
   editor's own layout already counts a CJK character as two columns
   (`layoutText` → `runeCells`, and `TestLineEditorRenderUnicodeCursorWidth`
   asserts the cursor landing on column 9 after `you> 知知`). The grid stored one
   cell per rune, so it advanced one column for a rune the editor moved two for:
   a colour assertion would have reported "the cell the user sees" as blank, and
   `CurCol` disagreed with the renderer's own arithmetic. The grid now models the
   columns the renderer draws. The existing render tests are the detector if some
   draw path ever disagrees.
3. **`Line` skips the continuation column** instead of storing the rune twice or
   padding with a real space: the text is unchanged for every row that has no
   wide rune (the common case), and a row with one still reads as ONE rune.
   `Scrollback` uses `Line`, so its content is unchanged too.
4. **`Style.Seq()` is absolute** — a reset first, then one group per attribute
   and both colour families. The parser is stateful (that is what a terminal's
   pen is), so a sequence that set only its differences would inherit the
   previous style and the round-trip test would pass while a real terminal showed
   something else.
5. **A truncated extended colour consumes the tail and applies nothing.**
   `38;2;1;2` must not be read as "dim, bold" — those are attributes no terminal
   applied, and inventing them is worse than dropping them.
6. **Erase clears a cell's style and keeps the pen** — a blank cell carries no
   colour, and the pen survives an erase, which is what a terminal does. Tested.

## Not Done (honest)

- **No emission/drawing change**: the renderer does not WRITE colour or emphasis.
  `Attr` can be read but not yet drawn — that is V2 by the epic's own split.
- **`internal/agent/loop.go`'s `ansiDim`/`ansiReset` are still bare literals**
  (`"\x1b[2m"`, `"\x1b[0m"`). They are the SAME bytes `internal/sgr` now names,
  so this is a real Rule 13 gap — left in place because loop.go pulls the
  deepseek decoder into the import graph of the shared `agent` package, and
  because V2 is where the emitter is written with this vocabulary anyway.
- **No font work; no new Go dependencies** (offline box, CGO_ENABLED=0).
- **Pre-existing, untouched:** `devdoc/DOC-INDEX.tsv` has six files with no row
  and one mislabelled banner; none of them is this task's file.

## Verification

```
CGO_ENABLED=0 go build ./...                                    # OK
CGO_ENABLED=0 go test ./internal/sgr/ -count=1                  # ok
CGO_ENABLED=0 go test ./internal/lineedit/ -count=1             # ok
CGO_ENABLED=0 go test ./internal/lineedit/ -run 'Screen|Render|Layout' -count=1   # ok
CGO_ENABLED=0 go test ./cmd/agent/ -run 'Screen|Stream|Height|Completion|LineEdit' -count=1  # ok
```

Red-before / green-after proof (`go build -overlay`, the working tree untouched):
the SGR branch of `internal/lineedit/screen.go` was replaced at build time with
master's `// SGR changes colour, not cells.`; the new suite then reports **9
failing top-level tests** (exit 1) with cells reading `default`, and passes with
the branch restored. The overlay file (scratch, outside this worktree) is:

```json
{"Replace":{"<worktree>/internal/lineedit/screen.go":"<scratch>/screen.go"}}
```

## Independent red-proof (2026-09-20, verification run — two forms)

The overlay proof above was re-derived here in two stronger forms, both in
SEPARATE detached worktrees (this branch was not mutated):

1. **The API does not exist on the pre-change tree.** The new test file was
   copied onto pristine master (`92d14684`, plus `internal/sgr/` so the import
   resolves) — it cannot compile, `exit 1`:
   `internal/lineedit/screenattr_test.go:43:14: sc.Cell undefined (type *Screen
   has no field or method Cell)`, and the same for `Attr`.
2. **Colour-blind mutation.** On the branch, `s.sgr`'s `sgr.ApplyParam(&s.pen, …)`
   loop was replaced by `break` — SGR parsed, pen never updated, i.e. exactly the
   pre-change "colour, not cells" behaviour, with the whole API present. Result:
   15 failing test entries, `-- FAIL` for `TestScreenRecordsPerCellAttributes`,
   `TestScreenAttrRoundTripThroughFeed` (all 12 sub-cases), `TestScreenSGRReset*`,
   `TestScreenExtendedColors`, e.g.
   `cell (0,0) = 'r' rune, want the probe's red fg=red` and
   `the red cell compares equal to a default cell: attributes are not recorded`,
   with cells reporting `style = default` where a style was set. The 15 pre-change
   render/layout tests (`TestLineEditorRender*`, `TestLineEditorScreenGrid`,
   `TestStatusRow_*`, …) stay PASS on that colour-blind tree — the detector for
   the change is the attribute suite, which is the point.

## Files

`internal/sgr/sgr.go`, `internal/sgr/sgr_test.go`,
`internal/lineedit/screen.go`, `internal/lineedit/screenattr.go`,
`internal/lineedit/screenattr_test.go`, `devdoc/DOC-INDEX.tsv`, this file.

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./...` → exit 0, no output.
- `CGO_ENABLED=0 go test ./internal/sgr/ -count=1` → `ok github.com/nick/gopi/internal/sgr 0.003s`.
- `CGO_ENABLED=0 go test ./internal/lineedit/ -count=1` → `ok github.com/nick/gopi/internal/lineedit 7.880s`.
- `CGO_ENABLED=0 go test ./internal/lineedit/ -run 'Screen|Render|Layout' -count=1` → `ok github.com/nick/gopi/internal/lineedit 1.343s`.
- Red-before: `go test -overlay=<scratch>/overlay.json ./internal/lineedit/ -run 'Attr|Wide|Styled|SGR|Erase|Scroll' -count=1` → exit 1; the log names nine `--- FAIL:` tests, e.g. `TestScreenRecordsPerCellAttributes`: `cell (0,0) = 'r' rune, want the probe's red fg=red` and `the red cell compares equal to a default cell: attributes are not recorded`.
- `git status --porcelain` in the worktree after the commit → prints nothing.
- Independent red-proof #1 (pre-change tree, `~/workspace/gopi-sgr-red` at
  `92d14684` + the new test file + `internal/sgr/`):
  `go test ./internal/lineedit/ -run TestScreenRecordsPerCellAttributes -count=1`
  → exit 1, `sc.Cell undefined … sc.Attr undefined`.
- Independent red-proof #2 (colour-blind mutant, `~/workspace/gopi-sgr-mut`,
  `ApplyParam(&s.pen, …)` → `break`):
  `go test ./internal/lineedit/ -run 'Screen|Render|Layout' -count=1 -v`
  → 15 `--- FAIL` entries from `screenattr_test.go` (probe, 12 round-trip
  sub-cases, reset ×2, extended colours) while 15 pre-change tests PASS; on the
  unmutated tree the same command is `ok … 0.685s` with 25 PASS.
- `CGO_ENABLED=0 go test ./cmd/agent/ -run 'Screen|Stream|Height|Completion|LineEdit' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 11.250s` (a plain `Line`/`Contents`
  consumer is unaffected).
- `CGO_ENABLED=0 go test ./internal/standards/ ./internal/stylegate/ -count=1` on
  this branch and on pristine master `92d14684` → the SAME three failures on both
  (`TestDocManifestCoversEveryDevdocFile`, `TestDocClassBannerMatchesManifest`,
  `TestStyleGate`); diff of the two failure sets is empty, so this branch adds no
  gate red. (Pre-existing local reds; `fix-master-red-standards-stylegate-*` owns
  them.) `devdoc/DOC-INDEX.tsv` carries this report's row (line 2099).
