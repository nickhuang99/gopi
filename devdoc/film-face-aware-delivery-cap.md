<!-- doc-class: historical -->
# Devdoc: the film's delivery cap comes from the terminal's own face

Task `01a10603-5319-7c55-b253-b2344335e7ba8` (2026-10-04). Class: measurement.

## The defect, reproduced with numbers

`gopi chat --replay S --film DIR --out F.gif` produced an animation the phone
conversation view (WeChat) refuses. Task `01a105d0` added the delivery contract:
a frame-width limit (`playableGIFMaxWidth` = 500 px) and a cell count derived from
it. The derivation used **xterm's built-in `fixed` face** (6×13 px per cell + a
4 px border), so the cap was 82 columns = 496 px.

This host's default filmed terminal is **xfce4-terminal**, whose face is
10×19 px per cell with a 16×27 px border. Measured on the same window:

| terminal | 82 columns | inside the 500 px limit? |
|---|---|---|
| xterm (reference face) | 496 px | yes |
| xfce4-terminal (the machine's default) | 836 px | **no** |

So the cap let through a canvas the composer then refused, and the parity half of
the same contract (the even-height crop) was never reached on a real session. The
limit is about the READER; the column count is a fact about the CAMERA, and
deriving one from the other through a face the film is not using is how 496
became 836.

## The fix: ask the terminal

`internal/shot/terminalface.go` reads the window's `WM_NORMAL_HINTS`
(`XA_WM_NORMAL_HINTS` = 40, a standard ICCCM atom — the same number a test checks
against `/usr/include/X11/Xatom.h`), and returns a `TerminalFace`:
`CellW/CellH` (the resize increment), `BorderW/BorderH` (the base size), plus the
window and display it was read from. `WidthForCols(c) = BorderW + CellW×c` is the
model, and it is confirmed against the server: a window of 836×521 at 82×26 cells
is `16+10×82` by `27+19×26`.

**The `PResizeInc` bit is NOT required** — and this is the measured part.
xfce4-terminal (VTE) publishes `flags=848` (`0x350`), which does **not** carry
`PResizeInc` (`0x40`), while filling the increment fields with `10, 19` that
describe its window exactly. A reader that insisted on the bit would call this
host's default terminal unmeasurable while its own numbers sat in the property.
So the rule is: a field is used when it is **plausible** (a cell of 1..64 px, chrome
of 0..512 px) — the plausibility bound is what makes reading an *undeclared* field
safe — and the end-to-end guard is that the model must **predict the window the
server actually mapped**.

`filmDeliveryFace` publishes the measured face into the composer's arithmetic, and
the help page's own table keeps its reference face through
`filmReferenceFace()` — one derivation, two inputs (Rules 13/14).

## The part that is not arithmetic: the ORDER

The face is only knowable **after the terminal has mapped a window**, and the
producer must be told the canvas the film is shot at. Two surfaces made that
awkward, both measured:

- The producer (`gopi chat --replay`) **refuses a terminal that disagrees with the
  canvas it was asked for** — verbatim: *"this terminal is 82x26 but the beat
  protocol was asked for 117x26"*. So shooting narrower means telling the surface,
  and the surface is already running by the time the face is known.
- The surface **detects its pty size itself**; a `--cols` the driver states is a
  second answer to a question the producer already answers.

So the driver does two things, in this order:

1. **`shot.FilmTerminal.Resize`** (`internal/shot/terminalresize.go`) moves the
   terminal window to the deliverable canvas, computed from the *same* face
   (`base + increment × cells`), with the X11 `ConfigureWindow` request written in
   one place. It is **verified, not assumed**: the request has no reply, so the
   caller reads the geometry back and a window that did not take the size is
   refused by name rather than photographed at a size nobody asked for. The
   surface inside the terminal owns its pty and re-reads its size, so ONE move
   leaves the picture and the delivered frames the same canvas.
2. **The producer is released with the decided canvas** through the guard script
   (`cmd/agent/chat_film_producer.go`): the terminal starts `/bin/sh` on a
   generated script that waits for a flag file, reads the argv from it (one word
   per line) and `exec`s it. The driver writes the flag *after* the resize, with
   the narrowed `--cols/--rows` in it. It is one file in this run's own scratch
   tree (Rule 22 / Rule 35: nothing installed, nothing resident, removed by
   `chatFilm.Close`). A guard that cannot be written is reported and the producer
   starts immediately at the resolved canvas — the ordinary case anyway.

## What a reader should be able to check

```bash
# the face and the cap it implies, on the terminal the film is shot on
DISPLAY=:99 bin/gopi chat --replay S --film D --out D/f.gif
#   [gopi film] the filmed terminal's face is 10x19 px per cell + 16x27 px border
#               — read from window 0x200003 on :65 — at the 500 px playable width
#               that is at most 48 column(s)
#   [gopi film] ... so this film is shot at 48x26 (496 px wide) instead
#   [gopi film] the producer was released at the canvas this run decided (… --cols 48 --rows 26)
#   [gopi film] animation D/f.gif: 12 frame(s) … 309187 byte(s)
```

A canvas the operator **stated** with `--cols/--rows` is shot as stated and still
refused by the composer if its frame is too wide — the refusal now names the face
and the count that would fit on it (`--cols 48` on xfce4-terminal), so the fix is
directly executable instead of "shoot it narrower":

```
[gopi film] compose the animation F.gif: F.gif is 836x520 px, over the 500 px
limit … this film was shot at a cell count whose frame is too wide on 10x19 px
per cell + 16x27 px border — read from window 0x200003 on :65, so no playable
animation can be written from it (shoot it narrower: --cols 48, which is 496 px
on that face)
```

## A trap for anyone writing a face probe (measured)

**xfce4-terminal is D-Bus single-instance.** Starting it while another instance is
alive hands the window to the *existing* process, so the new window's
`_NET_WM_PID` belongs to somebody else's terminal and the pid+title selection rule
correctly refuses it (observed twice while writing `internal/shot/terminalface_test.go`:
the probe window appeared under the *leftover* probe's pid). A test that probes a
face must therefore run on a display **it owns** — `shot.StartFilmDisplay`, the
same allocator the film driver uses — which is why
`TestFaceReadsTheTerminalsOwnModel` is gated on `GOPI_FACE_TEST_DISPLAY`.

## Files

- `internal/shot/terminalface.go` / `_test.go` — the face, its parser, the
  plausibility bounds, and the red/green pairs (VTE's shape among them).
- `internal/shot/terminalresize.go` / `_test.go` — the resize + verify, the wire
  layout pinned against the protocol.
- `cmd/agent/chat_film_producer.go` — the producer argv, the guard script, the
  release (a released producer is told the canvas this run decided).
- `cmd/agent/chat_film.go` — the order: start → read face → decide → resize →
  release.
- `cmd/agent/chat_film_gif.go` — the cap and the refusals, face-aware; the
  reference face is a named value.
- `cmd/agent/chat_film_face_test.go` — the defect reproduced as arithmetic
  (836 vs 496 at 82 columns) and the cap proved inside the limit on whatever face
  it was given.

## Not done / honest

- `cmd/agent/chat_film.go`'s background-size fallback (`shot.StartFilmDisplay`
  with a fixed `117x26` when the terminal cannot be started) is unchanged: a run
  whose terminal never starts has no face, and the composer's refusal states it.
- The help page's pixel table still prints the **reference** face on purpose; a
  reader of a real run should use the run's own notice, which names the face.
- `gopi chat --film` with `--film-terminal xterm` and `--cols` narrower than 82
  is shot as stated and refused if too wide — unchanged, and it is what the
  operator's own `--cols` means.
