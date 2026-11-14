<!-- doc-class: normative -->
# The answer body IS gold — task 01a0e2e6-5742 was a misreading, not a bug

**Task:** `01a0e2eb-ffaf-72bb-a021-ee3cc6855bd5` (CORRECTION) · **closed row:**
`01a0e2e6-5742-76cf-8d49-fe445497912a` · **baseline verified:** master `9f22bcda`

## The correction (operator, 2026-09-27, verbatim)

> "originally I complained about font color not in golden, it is only my input text,
> gopi response texts are indeed in golden color which is beautiful. make sure this is
> not touched."

Two different surfaces were collapsed into one ticket:

| surface | colour | who owns the decision |
|---|---|---|
| **gopi's REPLY body** (the answer band) | **GOLD — must stay untouched** | the palette tables |
| **the user's own typed buffer** | **unstyled by design** | `render.go`'s documented rule |
| the status row (chrome) | gold since `4265324c` (its own task) | `palette.go` status fields |

## What commit `80a53890` actually changed — and what it did NOT

`git show --stat 80a53890` lists `internal/lineedit/palette.go` and **not**
`internal/lineedit/render.go`. The diff is four status-row lines:

```
- StatusLabel: Style{open: sgrGray},      + StatusLabel: Style{open: sgrBold},
- StatusLive:  Style{open: sgrDim},       + StatusLive:  Style{open: sgrBold + sgrItalic},
- Thinking:    Style{open: sgrItalic + sgrYellow},
+ Thinking:    Style{open: sgrBold + sgrItalic + sgrUnder},
- ToolRow:     Style{open: sgrBlue},      + ToolRow:     Style{open: sgrBold + sgrUnder},
```

The answer body was not in that commit. **`80a53890` is not to be reverted** — its
measured reason was that SGR 90 (bright black) and SGR 2 (dim) are *remapped by the
user's theme* (4.39:1 on the operator's background), not that colour is bad. The
status row's gold came back separately in `4265324c` as 24-bit/256/`SGR 33` values the
terminal cannot remap.

## The gold, read from source (Rule 13 — cited, not paraphrased)

`internal/lineedit/palette.go`:

| table | field | value | line |
|---|---|---|---|
| `HermesDarkPalette` | `Answer` | `truecolorFg(hermesDarkText)` = `38;2;255;248;220` — `#FFF8DC` "cornsilk", `hermesDarkText = 0xFFF8DC` | `internal/lineedit/palette.go:460` (constant `:398`) |
| `HermesLightPalette` | `Answer` | `truecolorFg(hermesLightText)` — `0x3D2F13` | `internal/lineedit/palette.go:507` (constant `:408`) |
| `Palette256` | `Answer` | `palette256Fg(cubeGoldBody)` = `38;5;230` (`#FFFFD7`, the cube's nearest cornsilk) | `internal/lineedit/palette.go:631` |
| `BasicPalette` | `Answer` | `Style{}` — the **documented unstyled baseline** (the fallback table is the baseline, not a regression) | `internal/lineedit/palette.go:324` |

## The buffer-unstyled rule is DOCUMENTED, not accidental

> **OVERTURNED 2026-09-27 — READ THIS FIRST (task `01a0e439`).** The buffer is
> styled now. The section below is kept as the record of what the rule WAS and
> why it was written; **it is not the current behaviour.** The operator reversed
> the C1 verdict after reading the live screen twice, so the editing line's
> buffer (`you> …` → the `…`) takes the palette's own gold through the SAME
> machinery as the status row (`Buffer`, one value per table; `MonoPalette`
> still the zero value). Three things in the text below are now false and are
> corrected in place: the render.go quote, the `promptEmphasis` citation, and
> the test name. See `devdoc/handoff-20260927-chat-render-and-infobar-defects.md`
> (C1 → **REVERSED**) and `devdoc/chat-colour-emphasis.md` §6.

`internal/lineedit/render.go` (the header block — rewritten in 2026-09-27):

> ~~THE PROMPT IS EMPHASISED, THE BUFFER IS NOT. The palette styles the prompt only:
> what the user typed is theirs, and colouring it would put the palette's judgement on
> their input.~~

The header now states the reversal and keeps that argument as the REASON IT WAS
MADE (`THE EDITING LINE IS PAINTED: THE PROMPT IN ONE STYLE, THE USER'S BUFFER IN
ANOTHER`), because the old argument was answering the question "may the palette
judge this text?" — not "should this text be painted?".

It was enforced by `(*Renderer).promptEmphasis`, which styles the prompt's runes
only; the editing line's spans now come from `(*Renderer).editingRowSegs`, which
adds the buffer's span (`r.pal.Buffer`) for everything after the prompt's runes —
on EVERY row, so a wrapped continuation row is covered too.

So `you>` was styled and `hello` was left at the terminal's default colour —
**by design, and that design was overturned** (the operator's screen read
`0:5=fg=colour-186 bold 5:112=default`; the same row now reads
`0:5=fg=colour-186 bold 5:22=fg=colour-220 bold`).

## How it was verified (observation, not claim)

1. **Source**: the table lines above, read on this worktree's master `9f22bcda`.
2. **Guards, observed green**:
   `go test ./internal/lineedit/ -run 'TestPaletteChangeNeverMovesACell|TestHermesAnswerBodyCarriesTheGoldText' -v`
   → `--- PASS: TestHermesAnswerBodyCarriesTheGoldText`, `--- PASS: TestPaletteChangeNeverMovesACell`,
   `ok github.com/nick/gopi/internal/lineedit 2.482s` (whole package `ok 9.792s`;
   `TestEditorPromptIsEmphasisedAndBufferIsNot` PASS).
3. **Live PTY, real binary, mock model** (`devdoc/evidence/sgr-grid/capture.sh`):
   - default capture (`TERM=xterm-256color`, no `COLORTERM`) → the answer body is
     `fg=230` (`38;5;230`, the 256 gold) — the tier downgrade, not "no colour":
     `correction-ui-3-answer-done.grid.attr.txt`.
   - `COLORTERM=truecolor GOPI_PALETTE=hermes` → the body is
     `fg=rgb:16775388` = **`0xFFF8DC`**, the Hermes cornsilk, per cell:
     `correction-truecolor-ui-3-answer-done.grid.attr.txt`.
   Both captures show the SAME cells with the same characters — the gold is an
   attribute, never a character (Rule 10.2's cell truth).

## The lesson worth keeping

A palette is not one surface. Before filing "the colour regressed", name WHICH surface:
the answer body, the user's buffer, the status row, the banner and the prompt each have
their own field and their own rule. `TestPaletteChangeNeverMovesACell` exists precisely
so a table change cannot silently move a cell, and it was green throughout — a report
that reads a palette table-change as a body-colour regression is contradicted by the
guard that was already passing.
