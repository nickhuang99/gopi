<!-- doc-class: historical -->
# The 256-colour tier: the terminal landscape is three-tiered, the palette layer was binary

Task 01a0dcd4 (epic: the palette layer's middle tier). Companion to
`devdoc/chat-palette-hermes-gold.md`, which recorded the 24-bit half of the same
decision.

## The defect, as the operator met it

The palette layer had exactly two answers: the 16 named colours, or 38;2 24-bit
gold gated on `COLORTERM=truecolor`. Terminals have three tiers. On this host —
`TERM=xterm-256color`, `COLORTERM` unset — the run therefore landed on
`BasicPalette`: SGR 32/33/36/90, the same bytes `ls --color` emits. The operator's
report of the result is the specification for the fix:

> any color is better than default color!!! … if your display interface has no
> difference from default console, people would think you spend no development
> effort, or your product is very coarse or low … change some color, make it
> eye-catching!

`internal/sgr` had supported `38;5;n` all along (`ColorModePalette` → `FgParams`
→ `extendedColorParams`); nothing in `internal/lineedit` referenced it. The middle
tier was one `strings.Contains` away.

## The design, and why each piece is where it is

**One decision, one more input.** `CapabilityForEnv` (`internal/lineedit/colour.go`)
already read `COLORTERM`; it now also reads the terminal's `TERM` for the
256-colour marker. Both reads sit in that single function, so the tier and the
table can never be resolved against different environment states.

**The marker is a substring, case-insensitive, and that is the whole detection.**
The TI/terminfo convention puts the capability in the NAME (`xterm-256color`,
`tmux-256color`, `screen-256color`), and terminals put their own prefix in front of
it — a prefix match over a list of known names would miss the next emulator's.
A terminfo database read is a new dependency for one bit of information the
terminal already states, and a capability PROBE would have to write bytes before
permission is known. So: `strings.Contains(strings.ToLower(TERM), "256color")`.

**Tiers are SPELLED, not derived from constant order.** `Capability.AtLeast(tier)`
is a switch, not `c.depth >= tier`: a future tier inserted into the enum would
otherwise silently make a basic terminal satisfy it. `Depth()` exists for
reporting, never for table selection.

**`Palette.RequiredDepth()` is derived from the table's own bytes.**
`NeedsTruecolor()` used to scan for the `38;2` marker; it is now the head of a
ladder that scans for `38;2` (24-bit) then `38;5` (256) and reports `DepthBasic`
otherwise. A table that grows a deeper colour is gated the moment it is written —
the property cannot drift away from the data (Rule 13). The order matters:
`38;2` and `38;5` share the `38;` prefix.

**The default is settled by the capability, not by a name.**
`PaletteSelection()` returns the EMPTY string for an unset knob — the sentinel is
the empty selection rather than a word like `auto`, because a word would be a
legal value of `GOPI_PALETTE` that `PaletteByName` could not honestly resolve (the
answer is the capability's, and it is not known where the knob is read). The
`auto` spelling is refused with the same notice an unknown name gets, and the test
pins that. `PaletteForCapability` then turns "nobody chose" into the deepest table
the terminal can draw: gold 24-bit → `palette256` → `basic`.

**A downgrade falls back to the deepest drawable table, never to the floor.**
`PaletteForCapability` resolves the fallback by re-entering itself with the
capability's palette cleared, so a downgrade lands on exactly the table an unset
knob would have drawn at that depth — one notion of "the default for this
terminal", not two. This is the row the old collapse got wrong: `hermes` on a
256-colour terminal used to draw `basic`; it now draws `Palette256`.

**The 256 table is the gold family, not a fancier spelling of the 16.** The cube
is a real colour system: entry 220 is `#FFD700` on every terminal, because the
xterm cube is standardised rather than resolved by the user's theme — which is
exactly the property the 8-colour codes lack (see `BasicPalette`'s header for the
measured 4.39:1 status-row defect the theme remapping caused).
`cubeGoldAccent = 220` is Hermes' own `DARK_THEME.primary`; `cubeGoldBody = 230`
is `#FFFFD7`, the cube's nearest neighbour of cornsilk `#FFF8DC`. The test
reconstructs the cube arithmetic from the xterm standard (steps 1..5 = 40n+55) and
asserts each index decodes to the colour the table claims, so an index is only
"the promised colour" if the standard says so.

**One stderr line, and it names the enabler.** A named table that cannot be drawn
prints exactly one line: the table asked for, the depth it needs, the observed
`TERM`/`COLORTERM`, the table actually drawn, and `COLORTERM=truecolor` as what
would enable it. Two shapes stay deliberately quiet: an unset knob (nobody asked
for a table they are not getting — the "no silent upgrade" twin of the ban on a
silent downgrade), and a drawable request. A colour-OFF run is quiet too: a higher
decision already said no, and every redirected run would otherwise print a palette
notice onto a captured transcript. The unknown-name message was rewritten for the
same discipline: it no longer promises `basic`, which it could not know — the
default depends on the depth — it says the run follows the terminal instead.

**The status row carries the cube gold in the 256 table as well** — `38;5;220`, the
same #FFD700 Hermes calls primary — for the measured reason recorded in
`internal/lineedit/palette_status_salience_test.go` (SGR 90/2 are theme-remapped,
and the default foreground is the one colour the operator reported as missing).
This doc's original title for this section said "stays attribute-only": that was a
misreading of the salience rule, corrected by task `01a0e2ed` when commit
`4265324c` gave every tier its own gold (`palette256StatusFg`), and the table is
still in that guard's own table list plus the cell-grid identity test's.

## What the change had to repair in an existing test

`TestChatOnAPTYCarriesTheGoldAnswerBodyOnlyWhenTruecolourIsAdvertised` waited for
the answer's own text on the pty. The editor ECHOES the typed line, so the wait
returned the instant the echo appeared — before the answer band was drawn — and
the assertion that the stream contains the gold answer-body sequence passed
vacuously (the gold it matched was the PROMPT's, painted `Prompt: truecolorFgBold`
with the same cornsilk). It now waits for a marker that appears only in the answer
(`paletteE2EAnswerTail`) and requires the body's sequence to be immediately
followed by the answer text. The new E2E test for the 256 default uses the same
tail, and reads the downgrade notice out of the BOOT window, because the notice is
written while the capability is resolved — before the first frame.

## Where a future agent should look

| question | file |
|---|---|
| which table does this terminal get? | `internal/lineedit/colour.go` — `CapabilityForEnv`, `PaletteForCapability` |
| what does the 256 table paint? | `internal/lineedit/palette.go` — `Palette256`, the `cube*` constants |
| why is the status row not coloured? | `internal/lineedit/palette_status_salience_test.go` (measured 2026-09-26, task 01a0dacf) |
| how is the tier measured end to end? | `testsuite/chat_palette_e2e_test.go` (a real pty, the shipped binary) |
| the operator-facing knob | `CONFIG.md` §2f, plus `gopi chat -h` |
