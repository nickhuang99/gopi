<!-- doc-class: normative -->
# devdoc: the dump artifact an OBSERVED run could not read (task 01a0e433)

**Class**: bug-fix · **Status**: fixed · **Date**: 2026-09-28

## The defect, in one paragraph

`gopi chat --dump-screen` writes its artifact on ONE trigger: a paint `Write`
that both landed outside `dumpMinInterval` and produced a body different from the
last written one. A chat REPL paints its answer from the turn's own goroutine and
then goes quiet — the next paint is the next keystroke. An armed run that was
merely OBSERVED therefore froze its artifact on whatever frame the throttle
happened to catch mid-turn, and the only way to make the answer appear was to
TYPE — and a typed repaint can be sampled mid-paint, which is where the artifact's
literal escape-sequence cells came from.

Measured on the pre-fix tree, an armed 117x26 run:

```
frames=35 dumps=28 … iter 0 · ready      # answer absent, byte-identical over 40
                                         # samples spread across 10 seconds
```

That is the defect: the handoff doc's §0 re-verification recipe
(`gopi chat --dump-screen` → `gopi ui_state --target chat`) was not executable
without an operator.

## The second defect, found while fixing the first

The first fix (settle at the turn boundary) made the artifact *current*, and the
answer STILL did not appear. The trace:

* the sink's grid never accumulated: `nonblank` stuck at 1 while the paint stream
  carried 144 frames (140 of them after `frames=8`);
* the SAME bytes replayed on one uninterrupted `lineedit.Screen` carried the
  answer **plus 47 lines of scrollback**;
* the sink's grid held region `0-25` (the whole screen) where the layout had sent
  `CSI 1;24r` → region `0-23`;
* removing that one sequence from the replayed bytes reproduced the blank grid
  exactly (the closure).

**Root cause.** `ScreenDumper.SetGeometry` (the layout-announce path) replaced the
grid with a NEW `lineedit.Screen`. A fresh screen is blank AND has no scrolling
region — and the region arrives in the paint stream, so it is already gone by the
time the geometry is announced. The failure is not "empty": the layout paints the
answer from a cursor at the band's bottom row (row 24 of 26, NOT the screen's last
row), so with no region every `\n` took the screen's own newline branch and the
answer crept DOWN the grid until each new row landed on the row the previous one
occupied. The artifact then held the editing line and none of the answer.

## The fix

1. **`lineedit.Screen.Resize(cols, rows)`** — a size change now MOVES the grid
   instead of replacing it: the cells and their styles are carried across, the
   cursor is carried, and the scrolling region is **re-clamped rather than
   reset**. It is a no-op when the size is unchanged.
2. **`ScreenDumper.SetGeometry`** calls `Resize` instead of `NewScreen`.
3. **`ScreenDumper.Settle` + `chatInput.SettleDump()`**, called at the turn
   boundary in `cmd/agent/cli_chat.go` (right after `flowSeam.Flush()`, before
   the dump is finalized). By that point every writer of the turn has committed
   its bytes synchronously on the turn's own goroutine, so the frame installed is
   the whole turn — with no timer, no input and no second observation path.
4. `blankCellRune` is a named constant because the resize PADS with it (a zero
   rune painted `\x00` cells, measured).

## Verification — the case, and the two ablations

**`cmd/agent/chat_turn_dump_grid_pty_test.go`** is the case the epic asks for: one
real 117x26 pty, the REAL chat layout, the REAL `--dump-screen` sink, a whole turn
driven with **no keyboard input** after the submitted line, and the T1/T2
assertions run against the artifact's own cell grid (band + scrollback).

Its legs, and why each is a criterion rather than a snapshot:

| leg | what it asserts |
|---|---|
| `carries` | band+scrollback carries the whole answer, in order |
| `no-dup` (T1) | no 10-cell run occurs twice — skipping single-character runs, so the layout's own rule lines cannot report as duplication |
| `no-literal` (T2) | no `**` survives into a cell; backticks balanced per row |
| `scrolls` | the grid holds the LAYOUT's region (not the whole screen) and saved a scrollback — without this the reading could be about a band that never scrolled |
| `style` (T2) | every cell of the emphasized wrapping line is painted bold, read from the grid's **whitespace-free** cell stream (the layout's gutter `▌ ` prefixes band rows and two spaces prefix continuation rows; the wrap may even split a token — measured `…16emph 17e` / `mph 18emph…`) |
| `no-fragment` | no SGR parameter run painted as literal cells — the mid-paint capture's signature |

**Ablation 1 — restore the pre-fix `NewScreen` in `SetGeometry`** (the API stays,
so the case still compiles):

```
--- FAIL: TestChatTurnDump_ReadsTheWholeAnswerOnARealPtyWithoutInput
    the dump's band+scrollback does not carry "01plain".
```

**Ablation 2 — remove the turn-boundary `input.SettleDump()` call**: the pty case
still PASSES. This is reported rather than hidden, and it is why the settle has
its OWN guard.

**`internal/uiclient/dump_settle_test.go`** puts the settle leg under a clock the
test controls (`ScreenDumper.now` held still), so every paint write falls inside
the interval — the producer's measured state, made deterministic. It pins the
whole contract: a held frame is installed by `Settle`, a `Settle` after `Close`
does not rewrite the frozen artifact, and the throttle really did hold the write
(the `Writes()` assertions are marked `//screen-ok:` — the awaited fact IS the
install count, asserted synchronously after the call returned).

Ablated (`Settle` returns `nil` early):

```
--- FAIL: TestScreenDumperSettlesAFrameTheThrottleHeld
    Settle did not install the held frame — the artifact still reads:
```

## Why the pty case is not enough for the settle

Its clock is the real one. On a fast host the throttle's own writes land past the
turn's tail, so the artifact is correct with or without a settle — the case proves
the FRAME is right and cannot prove the settle is load-bearing. The seam's own
test holds the clock; together they cover both halves, and neither alone does.

## A pre-existing test this change turned RED — and why it was wrong

`TestUIStateAudit_OverWideLineIsNotAccusedOfBeingLost` asserted exit 3 (a
disagreement) on a premise its own comment stated outright: *"the band CUTS a row
wider than itself — it does not wrap it"*. That premise was **false**, and the
case was green because of the very defect fixed here: with the grid re-seeded and
its scrolling region gone, the rows after an over-wide one landed on the rows the
previous ones occupied, which made a WRAPPED line read as a truncated one.

The fix makes the band wrap honestly, so the case's fixture now reports
`status=agreed … rejoined=1` with the line arriving across rows `[20 21 22]`. The
case was updated to assert what is measured now — the line arrives, the audit says
so, and it names the rejoining — and it keeps the hard leg unchanged: an over-wide
line is NEVER accused of `paint_loss`.

## Honest limits

* **The CLI-level run could not use the production provider** (this sandbox has no
  reachable model), so `gopi chat` was driven end to end with a local stub
  speaking the Anthropic streaming envelope. What that exercised for real:
  the chat REPL on a real 117x26 pty, `--dump-screen` armed by the CLI, the
  reserved layout installed, a turn submitted with **no further input**, and the
  artifact inspected afterwards. It did NOT exercise the production provider's
  streaming cadence.
* **row-082's whole-sentence duplication is NOT established by this work.** The
  T1 leg here is a fresh fixture on the real pty/dump path, and it does not
  reproduce row-082; the mechanism is still open and should be its own task.
* `frames=0` when a `--dump-screen` run has no reserved layout is a separate,
  pre-existing behaviour (the sink says so on its own stderr line) and was left
  alone.

## The reusable recipe

1. **A dump artifact written only on CHANGE cannot be read by an observer** —
   a producer that finishes a frame and stops needs a SETTLE at the moment it
   knows the screen is final, not a longer sampling loop.
2. **Check that a reader's grid still holds the producer's REGION.** A blank grid
   whose bytes replay correctly elsewhere means parser state was dropped, not
   that the bytes were wrong. `region=<top>-<bottom>` in the artifact header is
   the cheapest tell, and one sequence's removal is the cheapest closure.
3. **A resize must move a grid, never replace it** — the region, cursor and cells
   are the paint stream's, and the stream is already underway.
