<!-- doc-class: historical -->
# Task Report: the editing line's buffer takes the system's gold (C1 → REVERSED)

**Task:** `01a0e439` · **Date:** 2026-09-27 · **Branch:** `task/01a0e439469c7d29aa0bc2f5d935e899`
**Epic:** reverse the C1 "the buffer is unstyled by design" verdict — the `gopi chat`
editing line's USER BUFFER rendered as the terminal's default foreground while the
status row was gold.

## Completed

- **`internal/lineedit/palette.go`** — one new field, `Palette.Buffer`, with one
  value per table: `BasicPalette` → `sgrBold + statusGold` (the named 8-colour gold
  the floor already pins), `Palette256` → `palette256FgBold(cubeGoldAccent)` (cube
  `#FFD700`, `38;5;220`), `HermesDarkPalette` → `truecolorFgBold(hermesDarkPrimary)`
  (`#FFD700`), `HermesLightPalette` → `truecolorFgBold(hermesLightPrim)` (`#8B6914`).
  `MonoPalette` stays the zero value, so the field emits nothing with colour off.
  **No new colour constant was introduced** — every value is an existing role
  referenced (Rule 13/14). The `Prompt` field's doc comment was corrected (it
  carried the overturned "never the buffer" clause).
- **`internal/lineedit/render.go`** — `editingRowSegs` replaces `promptEmphasis` at
  the one drawing site: it takes the prompt's row-local span and adds the buffer's
  span (`r.pal.Buffer`) for everything after it, **on every row**, so a wrapped
  continuation row is covered too (the case a prompt-only implementation would
  miss). A named constant `bufferRestRunes = math.MaxInt` is the "everything left on
  this row" span; `wrapSegs` clamps it (its `n > len(rest)` guard), so a second
  definition of the row's width was not needed. The header comment block is
  rewritten and keeps the old argument as the reason it was made, plus the C1
  reversal in full.
- **Tests.** The C1 test was **re-derived, not deleted**: `TestEditorPromptIs
  EmphasisedAndBufferIsNot` → `TestEditorPromptAndBufferAreDifferentlyStyled` (its
  positive half kept, its negative half inverted and strengthened). New:
  `TestEditorBufferGoldIsOnTheGrid` (cell grid, four tiers), `TestEditorMonochrome
  BytesAreUnchanged` (byte identity vs a golden measured on the pre-change tree),
  `TestEditingRowCarriesNoThemeResolvedColour` (the status row's salience invariant
  applied to the editing row's buffer, four tiers). Coverage maps in
  `palette_test.go` gained `buffer`.
- **Docs updated (the overturned verdicts):** `handoff-20260927-chat-render-and-
  infobar-defects.md` (C1 → **REVERSED**, with an addendum), `chat-colour-emphasis.md`
  §6 (revised) + its file table + the image caption, `chat-answer-gold-is-not-a-
  regression.md` (the "buffer-unstyled rule is DOCUMENTED" section marked OVERTURNED
  and corrected in place).

## Not Done

- **No committed image/pixel artifact.** The delivery is verified on the CELL GRID
  (the epic's own acceptance: "cell-grid truth, not 'we wrote SGR bytes'"), and on
  the live dump's `attr_runs`. The repo's older capture pipeline
  (`GOPI_PALETTE_CAPTURE_DIR` → Xvfb → screenshot) was not re-run for this change:
  the json/raw captures this run did produce are in the session directory, and the
  committed `devdoc/images/chat-emphasis-chrome.png` still shows the PRE-change
  editing line (its caption now says so). A future task could re-shoot that image.
- **The wrapped-continuation row was verified in tests, not on a live screen.** The
  live pty runs typed a one-row buffer; the wrapped case is covered by
  `TestEditorMonochromeBytesAreUnchanged/wrapped-buffer` (byte identity) and by
  `TestEditorBufferGoldIsOnTheGrid` (cell grid at 40 cols). Honest gap: no cell-grid
  assertion was written for a buffer that wraps at a *narrow* terminal — the
  mechanism (`editingRowSegs` covers every row) is the same one the non-wrapped case
  exercises.
- **`gopi ui_state --compare` was not run** as the audit arm (`--all`); the
  acceptance was read from the dump's own `attr_runs`, which is the same
  interpreted cell grid `--compare` audits against.

## Verification

- **Build:** `go build ./...` → `BUILD_OK` (exit 0); `go vet ./internal/lineedit/` → clean.
- **Tests:** `go test -count=1 ./internal/lineedit/` → `ok github.com/nick/gopi/internal/lineedit`
  (packages also run: `./internal/standards/`, `./internal/uiclient/`, `./internal/sgr/`, `./cmd/agent/`).
  Red proof (the new assertion really catches the defect): with `editingRowSegs`
  short-circuited to return only the prompt's span, `TestEditorBufferGoldIsOnTheGrid`
  fails on **all four tiers** with `buffer cell 0 (row 8 col 5) carries the terminal's
  DEFAULT foreground — this is the reported defect (screen truth: '5:112=default')`,
  and `TestEditorPromptAndBufferAreDifferentlyStyled` fails with `the buffer is not
  drawn under the palette's buffer style`. Restored → green.
- **Ran (Rule 31 — the real surface, a real terminal, the operator's own tier):**

  ```sh
  # build the branch's binary, run it in a PRIVATE SANDBOX (Rule 34) over a real pty,
  # with the operator's own arming variable and the same 80x24 geometry
  SB=$(gopi sandbox create --label c1verify --task-id 01a0e439 | tail -1)
  python3 c1-pty-driver.py /tmp/c1-p256.txt palette256 "why did the row shear?"
  # driver: GOPI_HOME=$SB GOPI_CHAT_DUMP=<path> GOPI_PALETTE=<tier> TERM=xterm-256color \
  #         gopi chat, in a pty (24x80), text typed AFTER the surface painted
  gopi ui_state --target chat --json          # read the live dump's attr_runs
  ```

- **Result (live screen truth, each read from the dump's own `attr_runs` on the
  editing row) — the operator's defect and its fix, in the operator's own vocabulary:**

  | run | row `you> why did the row shear?` | verdict |
  |---|---|---|
  | `palette256` (the operator's tier) | `0:5=fg=colour-186 bold` · **`5:22=fg=colour-220 bold`** | was `5:112=default` — **gold now** |
  | `basic` | `0:5=fg=green bold` · **`5:22=fg=yellow bold`** | floor's named gold |
  | `hermes` + `COLORTERM=truecolor` | `0:5=fg=rgb:fff8dc bold` · **`5:22=fg=rgb:ffd700 bold`** | `primary` |
  | `hermes-light` + `COLORTERM=truecolor` | `0:5=fg=rgb:3d2f13 bold` · **`5:22=fg=rgb:8b6914 bold`** | light primary |
  | `NO_COLOR=1` | `0:80=default` (one run, whole row) | colour-off: not one attribute |

  Note the `hermes`/`hermes-light` rows: without `COLORTERM` the pty advertised no
  24-bit support and the resolver correctly downgraded to the 256 tier — the tiers
  were resolved to *prove each table*, not asserted.
- **Byte identity (measured, not claimed):** the same three monochrome shapes
  (prompt-only, single-row buffer, wrapped buffer) were drawn on a **detached
  worktree at the pre-change commit `10dcd490`** and on this branch — the byte strings
  are identical, and those strings are what `TestEditorMonochromeBytesAreUnchanged`
  pins. The 14-byte SGR run in every string (`\x1b[1;8r`) is the region's own setup
  and predates the change.
- **Commit:** `64232afa` (the change) + `36b14165` (a comment correction in the new
  test file, in the same run).

## Evidence (self-filed)

- `go test ./internal/lineedit/ -run TestEditorBufferGoldIsOnTheGrid -v` → `--- PASS`
  for `basic` / `palette256` / `hermes-dark` / `hermes-light` (each subtest).
- Red proof, observed: with the buffer span removed, the same test printed
  `palette_test.go:283: <tier>: buffer cell 0 (row 8 col 5) carries the terminal's
  DEFAULT foreground — this is the reported defect (screen truth: '5:112=default')`
  (exit 1); `git restore` reverted the probe.
- Live runs, observed: `/tmp/c1-p256.txt.json` → row 22, `attr_runs` =
  `[{"col":0,"cells":5,"style":"fg=colour-186 bold"},{"col":5,"cells":22,"style":"fg=colour-220 bold"},{"col":27,"cells":53,"style":"default"}]`.
  Copies of all six dumps + the pty driver are in the session directory
  (`~/.gopi/sessions/01a0e439-469c-7d29-aa0b-c2f5d935e899/c1-evidence/`), i.e.
  outside the worktree (Rule 30).
- `gopi doccheck` → `the devdoc corpus is consistent` (exit 0).
- `gofmt -l internal/lineedit/` → no output.
- `git status --porcelain` in the worktree at close-out: **empty**. The probe test
  file `zz_golden_printer_test.go` was deleted before commit, and the pre-change
  fixture worktree (`git worktree add --detach … 10dcd490`) was removed; both probes
  are named here because "the tree is clean" is only meaningful next to what was
  taken out of it.

## Design notes (Rule 7)

- **Why the value answers the old objection.** C1's argument was "colouring the
  input puts the palette's judgement on it". That is a reason to refuse a
  *judgement*; it is not a reason to choose SGR 39, which is also a colour (whatever
  the theme declares for plain text). The fix keeps the *argument* and drops the
  *default colour*: the buffer takes the same gold family as the rest of the
  interface, so the colour says "editing line", nothing about the input's worth.
- **Why the span is `math.MaxInt` and not "the rest of the row".** The buffer's
  length is what remains after the prompt's runes on that row — a number the row
  knows. Computing it in `editingRowSegs` would re-derive the layout; naming it
  would be a second definition of the row's width. `wrapSegs` already clamps a span
  to what is left, so an explicit "everything" needs no length and the drawing loop
  is unchanged.
- **Why the tier resolver was not touched.** `PaletteForCapability` gates each table
  by what the terminal can draw; a `hermes` request on a terminal that declares no
  24-bit support downgrades to the 256 table *with a notice* — observed live in this
  run, and it is why the hermes rows above needed `COLORTERM=truecolor`.
