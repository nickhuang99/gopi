<!-- doc-class: historical -->
# The chat palette decision: Hermes gold, one capability, zero bytes when off

Task 01a0db1f (epic "Restyle `gopi chat`'s terminal UI to a Hermes-derived warm
gold palette", child 1/3 — the PALETTE LAYER).

## What this child adds

| surface | before | after |
|---|---|---|
| answer BODY (`Palette.Answer`) | `Style{}` — deliberately unstyled ("the baseline, not a highlighted surface") | Hermes' own body colour: `#FFF8DC` on a dark terminal, `#3D2F13` on a light one (the light/dark tables) |
| the rest of the colour surfaces | 8/16-colour SGR (SGR 31/32/33/36/90/…) | 24-bit Hermes palette values (gold/amber family) |
| the status row | attributes only (SGR 1/3/4 + the default foreground) — HEAD `80a53890` | **later change**: `palette.go`'s `statusFg` / `palette256StatusFg` paint the row in the table's OWN gold (`#DAA520`/`#FFD700`/`#CC9B1F` on the 24-bit tables, `38;5;220` on `Palette256`, the named SGR 33 on the floor) with `RequiredDepth` still derived from the bytes — commit `4265324c`, whose guards are `palette_status_salience_test.go` and `palette_hermes_test.go`, verified against the live chat surface by task `01a0e2ed` |

The palette is ported from `/home/user/work/hermes-agent`'s
`ui-tui/src/theme.ts` (`DARK_THEME` / `LIGHT_THEME`): the DESIGN and the colour
VALUES cross the language boundary, the TypeScript/Node stack does not. Every
colour in `HermesDarkPalette` / `HermesLightPalette` names the Hermes constant it
comes from in a comment, so the next reader can check the port against the source
rather than trusting it.

## The decision: ONE function, and the gate it exists for

`internal/lineedit/colour.go` already owned "may colour be used" (`ColourWanted`,
precedence `--no-color` → `NO_COLOR` (presence) → `TERM=dumb` → the sink is not a
terminal). This child adds the second half of the same decision — **how much**
colour and **which** table:

```
CapabilityForEnv(noColor, sink)
  ├─ colour    = ColourWanted(...)                    (unchanged precedence)
  ├─ palette   = PaletteSelection()   ← GOPI_PALETTE  (this child)
  └─ truecolor = color ON && COLORTERM==truecolor     (this child)

PaletteForCapability(cap)
  ├─ colour off            → MonoPalette          (the zero table, zero bytes)
  ├─ gold asked, no 24-bit → BasicPalette         (THE FALLBACK)
  └─ otherwise             → the table asked for
```

**Why the truecolour gate is a hard condition and not a preference.** The gold
family (`#DAA520`, `#FFF8DC`) does not exist in the 16 named colours: SGR 33 asks
the user's TERM for whatever it calls "yellow", and SGR 90 is its "bright black"
— the two codes this row was already burned by. So the gold tables are 24-bit
(`38;2;r;g;b`), and `38;2;…` sent to a terminal that does not understand it is not
"a slightly wrong colour": the terminal prints the parameters as TEXT. The only
signal gopi has that 24 bits will be understood is `COLORTERM`, and it is read —
case-insensitively, value-exact — only when colour is already ON
(`internal/lineedit/colour.go`). No terminfo lookup and no probe: a probe writes
bytes to the terminal before the first frame, which is the same
before-you-know-if-you-may-write class of bug.

`Palette.NeedsTruecolor()` derives the property **from the table itself** (it
looks for the `38;2` marker `internal/sgr` composes) instead of a flag declared
next to the data, so a table that grows a truecolour field is gated automatically.

`GOPI_PALETTE` is environment-only, on purpose: a palette is a property of the
terminal the operator is SITTING AT, so it belongs in a shell profile next to
`NO_COLOR`/`TERM`/`COLORTERM` — not in a shared `settings.json` (which would
apply it to runs that are not at that terminal) and not on a per-invocation flag
(which makes it a thing to remember to type). An unrecognised value is reported
on stderr and the run continues on `basic` — never silent (`GOPI.md` Rule 15),
never obeyed.

## The measurements (this run's own numbers)

Screen-grid proof, `internal/lineedit`'s VT100 `Screen` — the instrument that
carries a per-cell attribute grid (`screenattr.go`), so "the palette moved
nothing" is measured on CELLS, not on a description:

| assertion | test | measured |
|---|---|---|
| the body colour on the wire | `TestHermesAnswerBodyCarriesTheGoldText` | `HermesDarkPalette.Answer.Open()` = `\x1b[38;2;255;248;220m` (0xFF,0xF8,0xDC) |
| the same CELLS under all four tables | `TestPaletteChangeNeverMovesACell` | Mono / Basic / HermesDark / HermesLight grids byte-identical (`Screen.Contents()`) |
| the gold only where advertised | `TestPaletteTruecolorIsGatedByTheCapability`, `TestNoCapabilityEverSelectsATruecolorTable` (168 combinations) | a truecolour table is returned iff `Capability.Truecolor()` |
| colour off ⇒ zero bytes, knob included | `TestColourOffEmitsNothingRegardlessOfTheSelectedPalette` | `hasSGR == false` for every palette name under flag / no-sink / `ColourOff` / `MonoPalette` |
| the status row survives the swap | `TestGoldTablesLeaveTheStatusRowThemeIndependent` + HEAD's `palette_status_salience_test.go` | no banned theme-dependent code, still emphasised, text identical |

End to end on the shipped binary (`bin/gopi build → testsuite`, `GOPI_BIN`):

| shape | command shape | measured |
|---|---|---|
| piped (no tty) | `gopi chat`, stdin scripted, `COLORTERM=truecolor`, `GOPI_PALETTE=hermes` | ZERO `0x1b` bytes on stdout and stderr (`TestChatPipedEmitsNoSGRUnderEveryPalette`) |
| pty + `COLORTERM=truecolor` + `GOPI_PALETTE=hermes` | real pty, typed turn | the stream carries `\x1b[38;2;255;248;220m` on the answer body |
| pty, no `COLORTERM` | same, `GOPI_PALETTE=hermes` | the gold sequence is ABSENT and the basic table is drawn (the fallback, measured) |
| pty + `TERM=dumb` | same | zero SGR **colour** sequences (`m`-final CSI) |

One measurement note that cost a round and is worth keeping: on a pty the
raw-mode editor ALWAYS emits CSI (`\x1b[5C`, `\x1b[23;1H`, `\x1b[2K`) — cursor
placement and line erasing are layout, not colour, and no palette decision governs
them. "Zero colour bytes" on a terminal therefore has to be stated as "no
`m`-final CSI sequence", which is what `hasSGRColour` in
`testsuite/chat_palette_e2e_test.go` counts. A zero-`0x1b` assertion is only
statable on the PIPED shape.

## What was deliberately NOT copied from Hermes

- **No theme detection by background luminance.** Hermes picks light/dark from
  `HERMES_TUI_LIGHT`, `HERMES_TUI_BACKGROUND` (hex → Rec.709 luma),
  `COLORFGBG` and a `TERM_PROGRAM` allow-list. That is four heuristics and a
  colour-space conversion, and every one of them can be wrong on a terminal that
  answers none of them — gopi instead has an explicit knob and a named light
  table, and the default is the dark table (as Hermes' own default is dark).
- **No per-token syntax highlighting, no box-drawing chrome.** Hermes is a
  full-screen React/Ink TUI; gopi's chat is a streaming line renderer whose
  answer band, editing line and status row are three writers over one terminal.
  Copying the layout would be a rewrite of the renderer, not a palette change
  (and the epic says so: the layout work is a sibling child).
- **No new dependency.** The 24-bit sequences are composed by `internal/sgr` —
  the package that already owns the wire spelling — so this child adds zero
  modules (`GOPI.md` Rule 10.7).

## Where the documentation lives

`CONFIG.md` §2f ("Environment-only knobs") carries the two new rows
(`GOPI_PALETTE`, `COLORTERM`) with their read-at anchors, because the catalog's
`EnvVars` table is `--help-env`'s root and carries only env ⇄ settings-key ⇄ flag
triples: a knob with neither a settings key nor a flag has no catalog shape to fit,
and the doc gate's env-name scan is exactly what keeps such a row from being an
invented name (`internal/standards/config_doc_gate_test.go`).
