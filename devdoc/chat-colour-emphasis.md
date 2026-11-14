<!-- doc-class: historical -->
# Chat Colour / Emphasis Layer

Status: implemented on `feat/chat-colour-palette` (based on `4aea3c76`).

## Summary

A single, named palette source (`internal/lineedit/palette.go`), one capability
decision (`internal/lineedit/colour.go`), and the emphasis wired into the chat
surfaces: the banner, the prompt, the status row (label vs live hint vs tool
activity), the answer body (including fenced code and diff lines), hints,
warnings and errors. With colour off the layer emits **zero bytes** — no reset,
no empty sequence — so a redirected run is byte-identical to the historic
output.

## Where the pieces live

| file | role |
|---|---|
| `internal/lineedit/palette.go` | the named SGR constants, `Style`, `MonoPalette` (the zero value), `BasicPalette`, `PaletteFor` |
| `internal/lineedit/colour.go` | **the single capability decision**: `ColourWanted` / `PaletteForEnv` / `FlagNoColor`, and the one terminal probe |
| `internal/lineedit/compose.go` | the `AnswerComposer` interface + `LineComposer` (fences, diff add/del), `LineStyle` → `Style` mapping |
| `internal/lineedit/styled.go` | `stripSGR`, `hasSGR`, `lineSegments` — the text utilities, no renderer state |
| `internal/lineedit/answer.go` | the answer band: per-line decision, emphasis applied **after** the cell cut |
| `internal/lineedit/render.go` | the editing line: the prompt in one style, the user's buffer in another (**both** styled since the C1 reversal, 2026-09-27) |
| `cmd/agent/lineedit_wire.go` | resolves the palette once and hands it to every surface |

## The decisions that mattered

### 1. MonoPalette is the zero value — and `Wrap` on it is the identity

`Palette{}` is `MonoPalette`: every field is an empty `Style`, `Style.Wrap`
returns its text unchanged, and `Open()`/`Close()` are empty strings. So an
unset palette cannot leak a byte, and `style.Wrap(text) == text` is the whole
off-path. The alternative (a `Reset()` on every off-path write) would have made
"colour off" a sequence of resets rather than the absence of bytes — and the
byte-exactness requirement is precisely about the absence.

### 2. The cut comes BEFORE the emphasis

The answer renderer cuts text into rows by **display cells**. Style is applied
to each piece *after* the cut (`writeRow(text, st)`): the cut is arithmetic on
characters, the emphasis wraps the halves. Had the text carried its own SGR (the
first design), every component that measures a row would have had to strip
sequences first, and one missed call is a row that shifts by the length of an
escape sequence. Keeping text and style apart until the last moment removes that
class of defect instead of testing for it.

### 3. The decision point is the line break, not the burst

`Delta` receives the model's output in arbitrary bursts; a line arrives split
mid-word. A fence marker is one *whole line*, so a line's classification cannot
be decided until its terminator arrives. The renderer therefore:

- collects a burst's complete lines and classifies each **once**, giving the
  composer the line's whole text (`compPrefix` carries the part an earlier burst
  already painted, so a fence split across two bursts is still a fence);
- paints the unfinished tail under `composer.Open()` — the style of the block the
  stream is currently *inside*. An answer's characters never wait for its
  emphasis (that would turn a streaming answer into a paragraph-at-a-time one),
  and what has already been painted is never repainted.

`Finish` appends its tail as **plain text** into `prefix` and lets the ordinary
commit path decide it — deciding it inline would ask the composer twice about
one line, and a fence asked twice closes itself.

### 4. One place reads the environment, the flag, and the terminal

`ColourWanted` has the precedence: **explicit caller setting → `NO_COLOR`
presence (any value, including empty) → `TERM=dumb` → the sink's own
terminal probe**. Only `colour.go` consults `os.Getenv`/`IsTerminal` for colour;
every surface is handed a `Palette`. `NO_COLOR`'s *presence* is the signal, which
is the case a `value != ""` check silently gets wrong — there is a test for it.

### 5. The status row's "thinking" state is a fact, not a wording

`Status` gained `Thinking bool`, set by the producer that knows a turn is
running. The alternative — matching the live hint's text (`"thinking…"`) — is a
bare string keyword (Rule 1b) and would silently lose the emphasis the day the
wording changed.

### 6. The prompt and the buffer are BOTH emphasised, in DIFFERENT styles

> **REVISED 2026-09-27 (task `01a0e439`).** This section read "the prompt is
> emphasised, the buffer is not" — "what the user typed is theirs". The operator
> reversed that ruling; the reason is kept in `render.go`'s header, and the short
> version is that SGR 39 is a colour too: leaving the input at the terminal's
> default was itself a colour decision, and it made the editing line the only
> uncoloured chrome on a screen whose status row is gold.

The emphasis covers the prompt (`Prompt`) and, in a second span, everything after
it on the row — the user's buffer (`Buffer`). It is applied after `layoutText`, so
the layout arithmetic never sees an SGR byte. `editingRowSegs` asks the layout how
many of the prompt's own runes land on each row (a wrap boundary cannot fall
differently inside the prompt because more text follows it) and hands the rest of
the row to the buffer's style, so the emphasis is stable across redraws — one
open/close pair per span per redraw, never accumulating — and a row the prompt
never reaches is buffer from its first cell.

The value each table gives the buffer is the SAME gold family the rest of the
interface is made of (the floor's named gold, the cube's `38;5;220`, Hermes'
`primary`), so nothing is JUDGED by it; `MonoPalette` keeps the zero value, which
is why colour-off output is byte-identical to the pre-change tree (pinned by
`TestEditorMonochromeBytesAreUnchanged`).

### 7. Monochrome rule — nothing is carried by colour alone

Every emphasis wraps characters that are already distinguishable without it: the
tool row already carries the tool glyph, the live hint already says
`thinking…`/`ready`, a diff line already starts with `+`/`-`, a code block is
already fenced. The tests assert `stripSGR(coloured) == stripSGR(mono)` on the
answer band and on the status row, so the claim is checked rather than intended.

## Evidence

### Byte-exact, both directions

`go test ./internal/lineedit/` — the palette tests, all green:

- `TestColourWantedDecisionMatrix` — flag / `NO_COLOR` / `TERM=dumb` / non-TTY /
  TTY, each asserted on **emitted bytes**.
- `TestEditorWithColourOffEmitsNoSGR`, `TestAnswerRendererColourOffEmitsNoSGRThroughEveryPath`
  — zero SGR through every renderer path (Delta, Tick, Finish, the wrap path).
- `TestColourOnReachesATtyCapture` — a real `/dev/ptmx` sink gets SGR with the
  decision on, none with the flag.
- `TestMonoPaletteEmitsNothing` — every field of the zero table.

### Monochrome rule

`TestAnswerBodyEmphasisIsNotCarriedByColourAlone`,
`TestStatusRowMonochromeKeepsTheRowIdentical`,
`TestAnswerBodyCarriesCodeAndDiffEmphasis` — the character stream after
`stripSGR` is identical under both capabilities.

### Sizing untouched

`TestSGRBytesAreNeverCountedAsCells` replays both captures through the vt100 cell
grid (`Screen`) and requires the **same grid contents**: an SGR byte measured as
a column would place different characters in different cells.

### Rendered capture (pixel truth)

`scripts/ui/capture_emphasis.sh <out-dir>` emits the real renderers' bytes
(`GOPI_PALETTE_CAPTURE_DIR`), plays them under xterm on a private Xvfb display,
and screenshots the window. Images committed under `devdoc/images/`:

| image | what it shows |
|---|---|
| `chat-emphasis-colour.png` | the answer band: cyan fences, green added line, red removed line |
| `chat-emphasis-mono.png` | the same bytes with the capability off — **plain grey, no SGR at all** |
| `chat-emphasis-chrome.png` | banner, hint, warning, error, status row (grey label, blue tool row, italic-yellow live hint), editing line (green prompt, plain buffer — **that capture predates the 2026-09-27 reversal, which styles the buffer too**) |

Measured on the committed images at close-out (786432 px each, saturation =
`max(r,g,b)-min(r,g,b) > 60`): `chat-emphasis-mono.png` has **0** saturated
pixels while `chat-emphasis-colour.png` has 4814 — the monochrome image is grey
by measurement, not by intention. The script also dumps
`chat-emphasis-attributes.txt`, the SGR spans read back out of the *rendered
bytes* (not from the palette's constants), so the emphasis is legible as data
and not only as an image.

Two capture defects found and fixed while producing the images, both in the
script rather than the renderer: xterm needs `-u8` **and** a UTF-8 `LC_ALL`
(otherwise the em-dash and the tool glyph are mojibake), and the chrome capture
had to be laid out sequentially because a screen region addresses *absolute*
rows — an image of a band under absolute addressing shows whatever the previous
run left there.

### Command surface

`--no-color` is parsed in `cmd/agent/cli_chat.go` (constant
`lineedit.FlagNoColor`) and documented in the `gopi chat --help` block.
`newChatInput`/`newChatInputWithCompleter` resolve the palette once
(`lineedit.PaletteForEnv(noColor, os.Stderr)`) and install it on both the
`Editor` and the banner.

### `cmd/agent` — the failing set is a SUBSET of master's

The same whole-package run was done in both worktrees on the same host, each
piped through `grep -E '^--- FAIL' | sort -u`:

```
master  (20 rows) /home/user/work/GoPi
branch  (11 rows) this worktree
comm -13 master.names branch.names    → (empty)
comm -23 branch.names master.names    → (empty)
```

**Every failure this branch shows also fails on master** — the branch's failing
set is a strict subset (11 of 20), with nothing branch-only. The nine
master-only rows are the session-dir/worker cohort
(`TestBareRunKeepsAllItsArtifactsInOneSessionDir`,
`TestWorkerServesEachTaskInItsOwnRepoAcrossConsecutiveRounds`, …), which are
environment-flaky on this host rather than stable. Notably
`TestEveryHelpConstantFitsTheWrapBudget` and
`TestTopLevelHelpCoversEveryDispatchedSubcommand` fail on **master** as well, so
the `--no-color` lines this branch adds did not introduce a help-width defect
(the pre-existing one is `cli_chat.go`'s `--no-editor` line, 143 cols).

### The defect this acceptance caught in the layer itself

The answer renderer consumed each line's `\n` in `strings.Split` **before**
`writeLive` saw the text, so the composer never received a terminated line and
every answer line rendered plain. `TestAnswerBodyCarriesCodeAndDiffEmphasis` was
red until the per-line decision moved to the commit path (where whole lines are
handed over). Recorded because it is the trap the next agent walks into.

## Not Done (honest)

- **The `gopi chat` LLM-adoption proof of Rule 11** is not exercised: colour is
  a rendering capability, not a tool the model routes to, so there is no
  `tool_use` record to cite. The flag and the env knobs are covered by tests and
  the capture script.
- **`--no-color` is chat-only.** Other CLI surfaces (task list, merge output) are
  untouched — the epic scoped the chat surfaces.
- **The region grid test for the status row's absolute row** is unchanged; the
  emphasis does not alter which row is addressed (`styledStatusLine` is a pure
  function of `statusLine`'s output).
- **Windows/`TERM` beyond `dumb`** is not probed specially: the probe is "is this
  fd a terminal", which is the property the epic asked for.
