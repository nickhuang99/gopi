<!-- doc-class: historical -->
# The chat startup screen — a boxed logo with a side info panel

**Task** `01a0e7ed` (2026-09-28). **Branch** `task/01a0e7ed69337a83870fcb8291a09b21`.
**Files** `cmd/agent/chat_banner.go` (new), `cmd/agent/cli_chat.go`,
`cmd/agent/lineedit_wire.go`, `cmd/agent/chat_banner_test.go` (new),
`testsuite/chat_banner_pty_test.go` (new).

## What was asked

An attractive `gopi chat` startup screen: a boxed ASCII/Unicode logo on the left
and an info side panel on the right, modelled on the shape the reference agent
CLI opens with — but carrying gopi's OWN mark and gopi's OWN facts.

## The finding that decided the design

The REPL's startup banner was **one line**, printed straight to stderr
(`chatBannerLine`, `chat_stream.go`) before the editor took the terminal. The
obvious implementation — print twenty rows the same way — **does not work**, and
the reason is measurable rather than theoretical.

The editor installs a scroll region at row 1 (`region.go`: `\x1b[1;Nr`) and the
answer band owns the top of the screen. Rows printed to the terminal *before*
that install are pushed off the screen when the band paints. Measured on a real
pty (117×26, `script -qec`, `gopi chat` at HEAD `d1209fe0`):

```
 3|[gopi] loaded GOPI.md (67643 bytes)|
 …
 9|gopi chat — Ctrl+C interrupts the running turn, Ctrl+D or /quit to exit.|
24|you>|
25|deepseek-flash ·  iter 0  ·  tools off  ·  ctx 16k/1M≡  0.1s · ready|
```

— the banner is at rows 3–9 *before* the layout installs, and **gone from the
settled grid afterwards**. A twenty-row banner written the same way would be gone
the same way. So the banner is not a startup print at all: **it is the first
thing committed into the band**, which is the one surface whose rows go to
scrollback rather than being destroyed. (Scrollback is also where a startup
screen belongs — it stays readable after the first answer scrolls past.)

## The implementation

`cmd/agent/chat_banner.go` — a PURE renderer (`chatBannerRows(spec) []string`),
plus one composer (`chatComposeStartupScreen`) that gathers the facts and hands
the block to `input.WriteLines`, the REPL's own text path.

**Composed as styled spans, not strings.** The box's hierarchy is carried by
colour (chrome secondary, the title and mark emphasised, the panel's values as
reading matter), and the column arithmetic must run on the PLAIN text: a width
measured over a string that already carries an SGR sequence counts the
sequence's own cells — the defect `cutCols`' header records ("counting `\x1b[2m`
as four cells shortened every row after it"). So `bannerRow.add` accumulates
`lineedit.DisplayWidth` over plain runs and `render()` applies the styles at the
end. With the monochrome table (the zero `Palette`) every `Wrap` emits nothing,
so the colour-off path is plain text.

**The panel's width is derived from what it must say — not a fixed share.** The
first version used a 34% share of the inner width. That looked reasonable and was
wrong: at 100 columns the share gave the panel 31 cells, and the panel then
CROPPED the one line that is not decoration —

```
│ Ctrl+C interrupts the running … │        ← the 34% share, at 100 columns
```

`chatBannerCtrlCTip` is the statement the interrupt e2e case reads off the screen
(`testsuite/chat_pty_interrupt_test.go`), so cropping it is not a cosmetic loss.
The panel's width now starts at that sentence's own width
(`chatBannerPanelColsMin()`), grows to the widest line it will print
(`chatBannerPanelCols`), and is capped so the mark column keeps its floor. At 100
columns the same row now reads `│ Ctrl+C interrupts the running turn  │`. **The
minimum terminal width is derived too** (`chatBannerMinCols()` = gutters + both
columns' floors), so the floors and the width they describe cannot drift apart.

**The row budget, and the order content gives way.** `spec.maxRows` is the band's
own height (from the live layout, via `chatLayoutGeometry`) less the chrome it
reserves below. A box taller than its carrier would be pushed into scrollback by
the block that draws it — the screen would never be seen. Content is therefore
reduced **by priority**, never cropped to fit: the tips shed first, then the
least load-bearing facts, and the **mark last** — it is the thing the operator
asked for. `TestChatBannerGivesWayByPriority` pins that order.

**Facts are omitted, never faked.** A session with no ground prints no `dir` row
rather than a blank one; an over-long path is shortened from the HEAD
(`…ItsSidePanel657611157/002`) so the tail that identifies it survives, and the
cut is marked (`…`), because "the value ends here" and "the value continues" are
different claims.

## The one-liner is PRINTED, not replaced — the defect that proved it

The first cut made the screen an ALTERNATIVE to the historic one-liner: print the
box *or* print the line. That is wrong, and the surface's own e2e is what proved
it — `TestChatPTY_CtrlCDuringATurnInterruptsTheTurnNotTheSession` reads the pty
stream for the exit sentence, then for the prompt (`you>`). Both statements must
be **before** the prompt, because `waitFor` consumes everything up to its marker
and drops the surplus: a marker that arrives after the prompt is eaten by the
prompt's own wait and never read.

The box cannot satisfy that. It is committed into the band, and the band is
**below** the prompt row — so the box's rows are painted after `you>` and the
case timed out at 65s waiting for a sentence that was on the screen the whole
time. The repair is not to move the box: it is that the two are not alternatives.
**The one-liner is always printed** (before the layout, first in the stream —
exactly where it has always been), and the screen is drawn **in addition**, into
the band below. They answer different readers: the stream carries the contract,
the screen carries it where a human is looking, and the layout's first paint
pushes the one-liner's rows off the screen so nobody reads it twice.

A second, smaller ordering defect fell out of the same case: the panel listed the
exit sentence BEFORE the interrupt sentence, so a reader that stops at the exit
marker (the e2e's own reading) got a block whose interrupt line was still
unpainted — `the start-up banner does not say what Ctrl+C does`. The panel now
states the two sentences in the SAME ORDER the historic line does
(`chatBannerLine`: Ctrl+C first), which is also why the sentences themselves are
now single-sourced from `chat_stream.go` rather than re-spelled in the panel.

## What the paths that cannot carry the box get

They get the historic one-liner **byte for byte** — because that is ALL they get:
no layout means nothing is drawn, and the one-liner is not a fallback but the
baseline. That covers the cooked/redirected run (`--no-editor`, a pipe), a
terminal narrower than `chatBannerMinCols()`, and a layout that never installed.
Verified:

```
$ printf '/quit\n' | ./bin/gopi chat --no-editor
gopi chat — Ctrl+C interrupts the running turn, Ctrl+D or /quit to exit.
```

and the pinned byte-exact capture (`internal/lineedit/palette_capture_test.go`)
still passes. `chatBannerRows` returns `nil` rather than a degraded half-box — a
box at half the width claims a layout the terminal cannot hold.

## Verification (Rule 31 — the surface, executed)

**Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.

**Tests**:
- `go test ./cmd/agent/ -run TestChatBanner` → **PASS** (7 tests: width invariant
  at 8 widths, monochrome is plain text, the historic contract survives, declines
  what it cannot draw, the priority order, facts omitted not faked, crop marks
  its cut, dir keeps its tail).
- `GOPI_TEST_BANNER_EVIDENCE=<dir> go test ./testsuite/ -run TestChatPTY_StartupScreen -count=1`
  → **PASS** (6.1s).
- `go test ./internal/lineedit/` → **PASS** (24.3s — the pinned capture is
  untouched).
- `go test ./testsuite/ -run 'ChatPTY|ChatRender'` → **PASS** (the interrupt e2e
  and the whole render family).

**Ran** (the grid is the authority, not the source):

```
GOPI_TEST_BANNER_EVIDENCE=<dir> go test ./testsuite/ -run TestChatPTY_StartupScreen ...
```

which launches the REAL `bin/gopi chat` on a real pty (100×30, `TIOCSWINSZ`),
arms `--dump-screen`, and reads the artifact back through `uiclient.ReadDump` —
the same reader `gopi ui_state` uses. The dumped cell grid:

```
009|╭── gopi v0.1.0 ───────────────────────────────────────────────────────────────────────────────────╮
010|│                                                            │ Tips for getting started            │
011|│                       Welcome back!                        │ /help lists every command           │
012|│                                                            │ Ctrl+D or /quit to exit             │
013|│                          ▄▄▄▄▄▄▄▄                          │ Ctrl+C interrupts the running turn  │
014|│                         ▟████████▙                         │ /queue <prompt> parks the next turn │
015|│                       ▐███▛▘  ▝▜███▌                       │                                     │
016|│                      ▐███   ▄▄  ███▌                       │ ─────────────────────────────────── │
017|│                      ▐███▄  ▀▀  ███▌                       │                                     │
018|│                         ▜████████▛                         │ This session                        │
019|│                          ▝▀▀▀▀▀▀▘                          │ model    deepseek-v4-flash          │
020|│                                                            │ dir      …ItsSidePanel657611157/002 │
021|│             interactive chat, in this terminal             │ version  v0.1.0                     │
022|│                                                            │ tools    tools off                  │
023|│                     deepseek-v4-flash                      │                                     │
024|│ …PTY_StartupScreenShowsTheLogoAndItsSidePanel657611157/002 │                                     │
025|╰──────────────────────────────────────────────────────────────────────────────────────────────────╯
026|  (blank)
028|you>
029|deepseek-v4-f… ·  iter 0  ·  tools off  ·  no metrics  0.1s · ready
```

The assertion is the artifact, the reader is the tool's own, and the case checks
three things a source reading cannot: the box reached the screen at the pty's own
width, the mark is LEFT of the panel on the shared rows, and the chrome (prompt
row, status row) is still below the box in that order.

**The paint nudge, and why it is needed.** The dump is a SAMPLED artifact (100 ms
throttle) and the chat layout paints only when the screen CHANGES (`chat_dump.go`'s
own all-blank notice states the measurement). A reader that polls an idle screen
keeps seeing a frame from BEFORE the banner was committed — measured: the first
version of this case timed out with an all-blank grid while the same run's pty
stream carried the box. The case types one character (the editor echoes — a
change, so the sampler takes a frame that necessarily contains the startup
screen) and erases it, restoring the empty prompt.

## Limits (honest)

- **The box is drawn only when a reserved layout exists.** A `--no-editor` run and
  a piped run do not get it — they get the historic one-liner. That is a design
  boundary (`chat_banner.go`'s header), not a gap: those runs have no band to
  carry a screen and no terminal to read it.
- **`node` comes from `config.NodeIdentityLine`.** On a host whose topology was
  never declared the panel prints the `undeclared` spelling rather than a guess.
- **The mark is drawn only if the WHOLE art fits** the column; a narrower terminal
  gets the box without the logo rather than a clipped mark. Below
  `chatBannerMinCols()` there is no box at all.
- **Not verified here**: an actual human looking at a real terminal. The
  instrument is the dump; the claim is about the cell grid it reports.
