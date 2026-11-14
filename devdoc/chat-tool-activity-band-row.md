<!-- doc-class: historical -->
# The durable tool-activity row: chat's transcript now says what the agent DID

Task 01a0dd89 (epic "hermes ui dump and gopi chat can learn", child 1/N — the
first measured gap against the reference TUI dump).

## The gap, measured against the reference

The briefing's attachment is a Hermes TUI dump. Reading it as a *reference for
what gopi chat lacks* (not for what to copy), one gap is structural rather than
cosmetic: **Hermes keeps one durable row per tool call in the transcript**

```
  ┊ 💻 $         R=~/work/GoPi + 3 commands  0.6s
  ┊ ✍️  write     /tmp/pa_loop_probe.py  0.3s
  ┊ 🧠 memory    ?  2.6s
```

and gopi chat kept **none**. `cmd/agent/cli_chat.go` reports a running tool on
the STATUS row (`status.setTool`, `cmd/agent/chat_stream.go`'s `termToolLine`),
and `clearTool` is called when the turn ends. So:

- the transcript held no record that a tool had ever run;
- a reader could not tell which command produced the answer;
- a scrolled-away status row lost the activity entirely.

That is the one-line difference the reference makes obvious: **gopi answered
"what is happening NOW" and never "what happened".**

## What this change adds

One new surface — a *committed* band row — and one rule about who writes it.

| layer | addition |
|---|---|
| `internal/lineedit/answer.go` | `AnswerRenderer.Note(text, st)` queues caller rows; `Noting()` is the read loop's drain condition; `drainNotes()` commits them into the band |
| `internal/lineedit/editor.go` | `Editor.Note(text, st) bool` — the editor seam; **false** when no reserved layout owns the screen |
| `internal/lineedit/palette.go` | `Palette.ToolCall` on all four tables (Basic = attributes, gold/256 = `cubeDeepGold` 178, Mono = zero) |
| `cmd/agent/chat_stream.go` | `chatTurnRenderer.ToolRow(line)` — band row only, `region == nil` returns |
| `cmd/agent/lineedit_wire.go` | `chatInput.WriteToolRow(line)` — the REPL's seam |
| `cmd/agent/cli_chat.go` | `ag.OnToolEvents`: on `ToolEventKindCall`, `WriteToolRow(termToolLine(ev))` |

`ToolCall` is a **field of its own**, not a reuse of `ToolRow`. The two surfaces
answer different questions and the measured colour policy differs: the status
row is CHROME — it carries the table's own gold on every field and nothing else
(`palette.go` `statusFg` / `palette256StatusFg` / `statusGold`) — while a
committed band row is *transcript* — part of the answer's own reading matter, so
it carries the gold family exactly as the body does.

*(Corrected 2026-09-27, task `01a0e2ed`: this paragraph said the status row was
"attribute-only (the reason recorded on `BasicPalette`)". That was true of
`80a53890` and became false at commit `4265324c`, which gave the row its own gold;
the contrast between the two surfaces stands, but the reason given for it was
stale.)*

## The three decisions that mattered

**1. The producer writes ZERO bytes; the READ LOOP writes the row.**
`Note()` only appends to `r.notes` and calls `PublishStatus()`. The terminal has
exactly one writer (`drainStatus`'s note in `editor.go` says why: interleaved
cursor placement and repaint from two goroutines is unobservable corruption). A
tool call arrives from the turn goroutine; a band row is terminal I/O. So the
caller *queues*, and the read loop's `EventWake` / `EventNone` arms drain it.
The wake is what makes the row appear immediately rather than at the next
keystroke.

**2. `drainNotes` must ASK the layout for the live row — the root cause of the
first empty band.** The first implementation inherited `r.liveRow`. A tool call
before the turn's first delta arrives *before* `BeginTurn` → `clearLiveRow`, so
`liveRow` was still `0` — the screen's first row — and the band's commit scroll
pushed the freshly written row off the top margin. Measured symptom: the note was
written, `drainNotes` returned true, and **the grid stayed empty**. The fix is one
line, `r.liveRow = r.out.LiveRow()`, and its comment records the measurement.
The lesson generalises: *the live row is a property of the LAYOUT at the moment
of the write, never of the renderer's cached field*.

**3. The in-flight answer row is promoted FIRST.** A note must not land on the
line currently being written. `drainNotes` folds `liveText + pending` into
`prefix`, commits it, and only then writes the note's rows — so the note appears
BELOW the text the user can already see, in the order the terminal shows, and no
character is lost.

## Verification — pixel truth, not "it looked right"

The instrument is `devdoc/evidence/sgr-grid/capture.sh` (PTY bytes → a cell grid
that records per-cell SGR, plus an Xvfb/xterm PNG). Both runs drive the SAME
scripted fake model at the SAME geometry; only the checkout differs.

```
capture.sh before2 <tip 5710f683>    # the branch point
capture.sh after2  <tip 2a7d6416>    # this commit
```

| | BEFORE (`before-ui-3-answer-done.grid.txt`) | AFTER (`after2-ui-3b-after-turn.grid.txt`) |
|---|---|---|
| band rows | *no tool row anywhere in the band* | row 9: `⚙ call read /home/user/work/GoPi/devdoc/chat-mode-report.md` |
| the call's only record | row 29, the STATUS row — `deepseek-flash · iter 1 · tools · ⚙ read /home/ni… · … responding` | row 9 (durable) **and** the status row |
| attributes of that row | n/a | `0-58 fg=178 bold`, `59-99 default` — goldenrod, the gold family |

`fg=178` is `cubeDeepGold = 0xD7AF00` = Hermes' `goldenrod`. The answer body on
the same grid is `fg=230` (`#FFF8DC`), so the tool row is distinguishable from the
body by colour, as a transcript row must be.

The BEFORE capture is the falsifiable half: the same script, same model, same
geometry, and the band has no tool row at all.

## Not done (honest)

- **Only `ToolEventKindCall` is rendered.** Tool *results*, durations and exit
  codes have no durable row; the reference shows a duration (`0.6s`) and a
  result line. The seam (`Note` is a general caller-row API) already carries
  them; the REPL does not call it for them yet.
- **The glyph is gopi's own (`⚙`), not the reference's `┊ 💻 $`.** Deliberate:
  the marker column is a per-row property here (`lineGutter`), and a `┊` gutter
  would need the gutter machinery, not just a string.
- **Markdown tables in the band are still laid out glyph-by-glyph** (no column
  alignment) and a turn-closing rule does not exist. Both are visible in the
  same reference dump and are *separate* gaps, not addressed here.
- The copy/paste colour loss the briefing mentions (golden on screen, plain in
  the pasted text) is a **terminal** behaviour about the SELECTION, not about
  the band; this change does not touch it.
