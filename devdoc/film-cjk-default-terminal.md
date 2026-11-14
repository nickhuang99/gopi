<!-- doc-class: normative -->
# The film's default terminal renders Chinese — xterm does not

**Status**: implemented and measured, 2026-10-04, on `peerbox` (xfce4 desktop).
**Change**: `FilmTerminalDefault` moves `xterm` → `xfce4-terminal`. `xterm` stays a
selectable name with its historical measurements intact.
**Code**: `internal/shot/terminalregistry.go` (the constant and its rationale),
`internal/shot/terminalregistry_test.go` (the pin), `internal/config/config.go`,
`cmd/agent/chat_film.go`, `CONFIG.md`.

## The ruling

The operator's instruction (2026-10-04):

> never xterm, use gnome-terminal or xfce4-terminal — both go through fontconfig
> with TrueType + font fallback and render Chinese.

Both named terminals render CJK; only one can be **used by this driver**.
`gnome-terminal` is the registry's KNOWN-BUT-UNUSABLE row — its binary does not draw
the window (a D-Bus-activated `org.gnome.Terminal` service does, on the display *it*
inherits), and its `--geometry` is not the client's to set. A default must be a
terminal a film can actually run inside, so the default is `xfce4-terminal`, and the
operator's intent — *never xterm for a CJK surface* — is what the constant now
encodes.

## Why xterm cannot draw the surface (root cause)

xterm's default font is the X11 **core bitmap** `fixed`, 6x13. Its charset is
ISO8859-1 and nothing else — measured on this host with `xlsfonts -ll fixed`:

```
CHARSET_REGISTRY      ISO8859
CHARSET_ENCODING      1
```

(attachment `xterm-font-charset.txt` in the task's session directory.)

Two consequences, and **both** were reproduced at pixel level:

1. **No CJK glyph exists in the font** → the Chinese bytes are drawn as whatever
   Latin-1/CP1252 characters those byte values happen to be. Measured below:
   `中文测试` came out as `ä æ æ µ è ¬ …`, an empty box glyph below the first
   character.
2. **The advance width is wrong** → one cell per BYTE, so a Chinese word
   mis-measures and shoves the neighbouring ASCII along. Measured: the same line
   `中文测试ABC` advances **11.17** cell-widths in `xfce4-terminal` (four
   double-width CJK glyphs + `ABC` = 11) and **15.41** in xterm (one Latin glyph
   per UTF-8 byte: 12 + 3 = 15).

This is the entire difference between a film that is a picture of the chat surface
and a film that is a picture of mojibake.

## The pixel criterion (Rule 10.2 — measured, not read off a score)

Scripts and their raw output live in the task's session directory
(`~/.gopi/sessions/01a10370-415c-794c-8c22-e74b06577ab1/evidence/`), never in the
worktree (Rule 30/39).

### 1. The controlled probe — the same string in both terminals

`evidence/probe/one2.sh` starts one private `Xvfb` per terminal, runs
`printf '中文测试ABC\n'` inside it, and photographs the display with
`gopi capture`. `evidence/probe/advanceprobe.py` then measures the line on the
pixels: it takes the window, cuts its text band into glyph tokens, self-calibrates
**one ASCII advance** from the trailing `ABC`, and reports the run's total advance
in those units.

| terminal | font source | window bg | tokens | advances for `中文测试ABC` | verdict |
|---|---|---|---|---|---|
| `xfce4-terminal` (LANG=C.UTF-8) | VTE + fontconfig (`Noto Sans Mono CJK SC`) | `#131926` | 7 | **11.17** | CJK glyphs (4 x double-width + 3) |
| `xfce4-terminal` (LANG=C) | same | `#131926` | 15 | **15.41** | one Latin glyph per UTF-8 byte (the SAME mis-measure xterm shows) |
| `xterm` (any locale) | core bitmap `fixed` 6x13 | `#FFFFFF` | 11 | see below | Latin-1 mojibake (see below) |

The xterm row's `11 tokens` is NOT an advance measurement of the run: at a 6 px
cell the three letters of `ABC` are separated by gaps wide enough to split into
three tokens, so the trailing `ABC` is not a single token and the probe's
self-calibration (which reads the LAST token) is invalid on that image. The
run/advance column therefore says "see below" rather than a number, and xterm's
failure is named by the two measurements that ARE valid on it: the host's font
charset (`xlsfonts -ll fixed`) and the CP1252 transcription of the line's own
pixels.

Two facts fall out of this table and both matter:

- **the terminal is not the only variable — the LOCALE is one too.** Under
  `LANG=C`, `xfce4-terminal` is byte-wise as broken as xterm: VTE decodes the pty
  stream in the locale's charset, and a film run must therefore pin a UTF-8 locale
  on its own child rather than inherit the caller's.
- **the box glyph is a locale-independent failure.** Even in the `LANG=C` case the
  `▯` boxes show, because the bytes really are being decoded to *some* charset and
  a glyph is missing for at least one of them.

### 2. A read of the actual xterm line (corroboration, not the criterion)

`vision_analyze` on a 15x nearest-neighbour blow-up of that one xterm text line
(`evidence/probe/xterm-line-smooth.png`) transcribes it as:

```
ä,-ææµè¬ABC
    ▯
```

i.e. exactly the CP1252/Latin-1 reading of the bytes of `中文测试` (`ä æ æ µ è ¬`),
with a `▯` empty box under the first character. Vision is used here **only** to name
the mojibake; the verdict rests on the advance measurement above (a vision score
cannot quantify render quality — Rule 10.2).

### 3. The film itself — no terminal knob

```
[gopi film] display :63 (server pid 2097538), terminal xfce4-terminal (the built-in default) at 117x26 cells on it
[gopi film] xfce4-terminal offers no session-log switch, so the filmed command's own output cannot be captured to …/producer.log — it reaches that terminal's pty only (xterm keeps its own -l/-lf channel). …
[gopi film] terminal window 2097155 ("gopi film film") area 1186x521+0+0
[gopi film] 1 frame(s) never reached the settled criterion (two consecutive grabs were never equal) — they are delivered and marked settled=no in frames.tsv; a still surface and a lost repaint must not be the same number
[gopi film] beats.tsv: …/default/film/beats.tsv
[gopi film] animation …/default/x.gif: 162 frame(s) at 25cs (4.0 frame(s)/s), 8056510 byte(s) — 162 ledger row(s), 162 frame(s) photographed
```

- the resolution channel announces itself as **the built-in default** — the line a
  film run prints when nobody named a terminal, and the proof that the change is
  what a real run reaches;
- **162/162 frames from 162 ledger rows** — the same frame-per-beat arithmetic the
  xterm baseline recorded for this replay (162/162, `devdoc/film-terminal-selection.md`);
- the window is **1186x521** — 117x26 cells at the VTE font's cell, over the same
  #131926 ground the palette is designed for;
- **95 of the 162 beats carry CJK** (decoded from the run's own `beats.tsv`), so
  the frames are full of Chinese rather than incidentally touching it.

`evidence/probe/cellprobe.py` cuts a frame into its 117x26 cell grid and measures,
per inked cell, how much of its ink lies strictly inside the ink bounding box
(a hollow fallback box is *entirely* border) and how many connected components it
has. On `frame-0008.png` (beat 8 = a Chinese answer):

```
canvas=1186x521 grid=117x26 cell=10.14x20.04px ground=(19,25,38) ink_px=103994 inked_cells=1713
interior%: min=0.0 median=52.8 max=85.2
components/cell: {1: 945, 2: 425, 3: 231, 4: 87, 5: 21, 6: 4}
box-like cells (interior%>=90 and 1 component): 0/1713 = 0.0%
```

**The interior% criterion is INCONCLUSIVE on its own, and this document says so
rather than reading a number as a proof.** At 10.14x20.04 px per cell the 1-px
border of a glyph's ink bounding box is a large share of that glyph's ink, so
`interior%` cannot separate a hollow box from a dense CJK glyph: the same
"box-like = 0" figure comes out of the xterm control frame AND out of a frame of
xterm's blank startup screen. What the numbers DO support is stated narrowly —
nothing in the frame looks like a field of identical rectangles, and the
components histogram is typesetting-shaped (multi-part glyphs) rather than
box-shaped. **The proof that the default renders Chinese is the controlled
probe's advance measurement above** (11.17 units where byte-wise drawing would
give 15.41), taken with the same string in both terminals.

## What did NOT change

- **xterm is still a name you can ask for** (`--film-terminal xterm`), its registry
  row is untouched, and it appears in the usable list the refusals print. The
  ruling demoted it from the default; it did not remove it.
- its historical measurements remain the pipeline's baseline: the settled grab, the
  frame-per-beat ledger, the legibility floor, and the **706x342 = 117x26 x 6.034
  px/cell** geometry — which is xterm's cell at its own font, and therefore a fact
  about *that terminal*, not a constant of the pipeline.
- the four-channel knob, its ONE reader and the refusals are unchanged.

## Honest caveats

- **The pixel size of a film is the terminal's font's.** 706x342 is xterm's, and
  1186x521 is xfce4-terminal's at the same 117x26 cells. The 706x342/6.034px/cell
  figure is quoted in the ruling text as a *historical baseline of the xterm row*,
  and no constant in the tree asserts it — the pipeline's invariant is the CELL
  grid (117x26) and the frame-per-beat arithmetic, both of which hold.
- **VTE needs a UTF-8 locale on the child.** The driver inherits the caller's
  environment; under `LANG=C` the default terminal mis-measures the same way xterm
  does. Reported here rather than fixed: pinning a locale on the film child is a
  separate change with its own measurement, and it touches every child this driver
  starts.
- **`gnome-terminal` remains unusable** (its own registry refusal unchanged): the
  operator named it, but its window is drawn by a D-Bus service on another display
  and its geometry is not the client's to set.

## References

- `internal/shot/terminalregistry.go` — the constant, its rationale, the registry.
- `devdoc/film-terminal-selection.md` — the registry/knob this builds on, and the
  2026-10-03 measurements it recorded (including the earlier xfce4 film).
- attachment `xterm-font-charset.txt` (this task's session directory).
