<!-- doc-class: normative -->
# The replay's marker rows state the counts, never the keys

> Class: normative (a rule about one surface's screen, with the red-proof recipe
> that established it). Established 2026-10-22, task `01a162f5`.

## The defect, measured

`gopi chat --replay <session>` re-runs a recorded session on the real chat
surface. The watcher's keys are the control layer's (`cmd/agent/chat_replay_control.go`):
**any** typed key toggles the pause, Esc leaves, and the surface prints exactly one
bare state word per transition (`PAUSED` / `RESUMED` / `LEAVING`).

It still printed this, on every frame the driver typed:

```
… PgUp/PgDn scroll · Alt+↑/↓ one row
```

Raw pty evidence (117×26, one replay, keys SPACE/PGUP/PGDN/UP/A/SPACE/ESC):

| binary | `PgUp/PgDn scroll` occurrences | of them after the first key press |
|---|---|---|
| pre-fix (`cb14fadf4`) | 103 | 33 |
| post-fix | **0** | **0** |

## Where it came from — and where it did NOT

The line is `windowScrollKeys` (`internal/lineedit/window.go`), composed into a
MARKER ROW by `windowMarkerRow`, drawn by `renderWindow` whenever the editing line
hides buffer rows. It is not produced by the replay control layer at all: the
control layer's OSD vocabulary is asserted clean by `chat_replay_control_test.go`,
which is why three previous passes over that file could not find it.

The reason it appeared *during a replay* rather than only when the line overflows
is the surface itself: the replay types a whole recorded turn into the editing
line, so the buffer outgrows the window on every turn, and the end-of-turn `you>`
row with nothing hidden below is drawn as **the key statement alone**.

Two facts make the sentence false there:

1. The control layer claims **every typed key** at `lineedit.KeyGate` before the
   editor sees it, so `PgUp`/`PgDn`/`Alt+↑` never reach the window.
2. The market: the replay's surface may not print help text at all (the operator's
   rule for this column, `chat_replay_control.go`'s OSD block).

## The fix

One switch, on the owner side, defaulting to the historic behaviour:

- `Renderer.SetWindowKeyRow(bool)` / `Renderer.WindowKeyRow()` (reader+writer pair,
  the `SetInputLocked`/`InputLocked` precedent) — implemented as the flag
  `noWindowKeys`, so the ZERO VALUE keeps the keys on screen for every other
  editor.
- `Editor.SetWindowKeyRow` / `WindowKeyRow` forwards to it.
- `renderWindow(..., pinned, keys bool)` takes the resolved answer;
  `windowMarkerRow` receives it.
- `chatReplayControl.install` calls `ed.SetWindowKeyRow(false)` beside
  `SetInputLocked(true)` / `SetKeyGate(c)`, with the reason in place.

**What is NOT turned off: the counts.** `… 8 lines above` states a fact about the
user's own text. Hiding it would make the surface lie about the buffer instead of
about a key. And when nothing is hidden below *and* no key statement is due, the
marker row is not drawn at all — a chrome row carrying nothing is a row of the
user's text given away.

## Red/green recipe (reusable, ~3 minutes per run)

1. Build the binary: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`.
2. Drive it on a REAL pty (a pipe is refused by the verb) — `ptyreplay.py` in this
   task's session directory: `pty.openpty` + `TIOCSWINSZ 117x26` + `start_new_session`,
   wait for `[gopi replay] playback:`, then press `SPACE / PGUP / PGDN / UP / A /
   SPACE / ESC` with ~0.9 s between keys, recording every byte.
3. Assert ABSENCE on the raw bytes, not on a screen scrape (_the state line is
   absolutely positioned; copying the screen folds two rows into mojibake —
   memory #8156_): `grep -c 'PgUp/PgDn scroll' fix.raw` → 0, and the six state
   words still present.
4. The red proof is the same command against a binary built from the base commit.

## Pitfall found while fixing it

`renderWindow`'s `markers >= editorChromeRows` arm called
`windowMarkerRow(-1, below, ...)` and its `below == 0` branch returned the key
statement ALONE — so threading the flag through the two call sites changed nothing
on a real pty (measured: 103 hits before and after the first attempt). The branch
that ignores its argument is the defect: it must be reached only when the row has
something to say.
