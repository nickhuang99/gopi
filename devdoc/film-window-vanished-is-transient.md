<!-- doc-class: normative -->
# devdoc: a filmed window that goes away mid-frame is a transient, not a fatality

**Class**: normative — the film's error contract for "the window is not there".

**Related**: `devdoc/film-terminal-selection.md` (a terminal is a registry row),
`devdoc/film-cjk-default-terminal.md` (why xfce4-terminal), task
`01a103f4-8ce5-7eb1-8dc3-2559f5ef5ff6`.

## The defect

Since the film's default terminal became `xfce4-terminal` (`01a10370`) a long
film could end with no deliverable at all:

```
[gopi film] terminal window 4194307 ("Xfce Terminal") area 1186x521+0+0
[gopi film] grab window 4194307 on :65 (grab 2 of 4):
            window 0x400003: GetGeometry: X11 error reply (code 9, sequence 1)
```

`internal/shot/terminalterm.go`'s `SettledGrab` treated that as fatal and
returned on the first refusal. The same file already treats its other transient
— "the surface has not settled yet" — as something it carries on through
(`settled=no` is reported, not raised), which is what made the old behaviour an
inconsistency rather than a policy.

## What was measured before anything was changed

The reproduction had to be REAL, not a story about a race (Rule 10.4/10.5).
Three stimuli were applied to a live `xfce4-terminal` under `Xvfb`, with a film
running:

| stimulus | effect on the grab | effect on the terminal |
|---|---|---|
| `xdotool windowunmap` (2 s) | **none** — frames kept landing (3 → 10 across the unmap window), no X11 error | survives, re-maps cleanly |
| `xdotool windowsize` | none | survives; the id is unchanged |
| `DestroyWindow` on the top-level | the client **dies** (destroying a toplevel removes the last X connection's window and the emulator exits) | dead |

So on our own displays, "unmap and map again" does **not** produce the field's
`s GetGeometry … code 9`, and a live terminal cannot be provoked into rebuilding
its toplevel by a client. What a **killed** terminal does produce is exactly the
field's error, on the very next grab — that is the deterministic end-to-end
stimulus the fix is demonstrated with, and it is honest about what it is: a
vanished window, not a rebuilt one.

## The fix

1. **The server's refusal is typed.** `readReply` is the ONE place an X11 error
   reply becomes an error, and it now returns `*shot.X11Error` carrying
   `Request`, `Code` and `Sequence`, with the protocol's own name for the code
   (`BadWindow` = 3, `BadDrawable` = 9). The message text keeps the code it
   always printed and gains the name, so nothing a reader had is lost.
   `IsWindowVanishedError(err)` is the ONE judgement of "the window is not on the
   server right now" (Rule 13/14) — it is `errors.As`-based, so a wrapped error
   still classifies.

2. **A vanished window is retried, and the retry goes through the selection
   rule.** `SettledGrab` now takes the `*FilmTerminal` (not a bare `uint32`), and
   on a vanished-window refusal it sleeps `filmGrabVanishedStep`, calls
   `t.ReResolveWindow()` — which is `windowForSpec`, the pid + title +
   measured-rect rule, with **no second matching logic anywhere** — and shoots
   again. The re-resolution is **announced** on the film's own stream, naming the
   old error, the new id, the terminal and the area, once per attempt.

3. **The retry is bounded and its exhaustion is loud.** `filmGrabVanishedRetries`
   = 3; when the bound is spent the frame is refused with
   `*WindowVanishedError`, which names the last window, the number of attempts
   and the server's cause, and says in as many words that **no frame was
   delivered**. A re-resolution that finds no window of the terminal spends a step
   of the same bound rather than ending the frame at once: "the terminal is
   momentarily between windows" and "the terminal is gone" are told apart by
   running out of steps, not by guessing.

4. **Only that refusal is retried.** Any other error — an unreachable display, an
   unclassified code — is still returned immediately and unchanged, so the bound
   cannot be spent hiding a different failure.

5. **The recovery is counted and kept.** `FilmGrab.Vanished` per frame lands in
   `frames.tsv` as a sixth column and the run prints a closing line with the
   total, and `internal/shot`'s terminal notices are also written to
   `terminal-notices.log` in the film's directory. A recovery that leaves no
   trace is a recovery nobody can check (Rule 15).

## What the reproduction shows

Same stimulus, two binaries, `gopi chat --replay <session> --film … --out …`
with the filmed terminal killed mid-shoot:

* **before** — `[gopi film] grab window 2097155 on :65 (grab 2 of 4): window
  0x200003: GetGeometry: X11 error reply (code 9, sequence 1)` and the run ends;
  no GIF, and the message never says whether anything was delivered.
* **after** — the same code now arrives with a name and a decision:
  `window 0x200003 was not on the display for 3 consecutive grab attempt(s) — no
  frame was delivered (the terminal was re-resolved by pid+title+measured-rect
  between attempts): window 0x200003: GetGeometry: X11 error reply (code 9,
  BadDrawable, sequence 1)`.

Both still fail the run, and deliberately so: when the terminal really is gone
there is nothing to photograph. What changed is that the refusal is **decided**,
**bounded**, **named**, and **says what it did with the frame**.

## Not a workaround

The fix does not touch the window-selection rule, the locale pinning
(`filmlocale.go`), the terminal registry, the replay ledger or the CJK rendering
path — it adds a recovery around one class of server refusal, at the layer that
already owned the "how long do we chase a surface" judgement.

## Recipe (reusable)

To reproduce any "the filmed window is not there" failure deterministically:

```bash
cd <worktree> && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
./bin/gopi chat --replay <session-id> --film <dir> --out <dir>.gif &
# wait for the "terminal window N" line, then kill THAT driver's own terminal:
TPID=$(ps -o pid= --ppid $! | while read p; do [ "$(ps -o comm= -p $p)" = xfce4-terminal ] && echo $p; done | head -1)
kill -9 "$TPID"
```

Kill the driver's **own child**, never "the first xfce4-terminal on the box" —
another film may be running, and killing its terminal answers a different
question (measured: it did exactly that once during this task).
