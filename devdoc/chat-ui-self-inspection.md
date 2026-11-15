<!-- doc-class: normative -->
# Chat UI self-inspection — how to SEE and MEASURE `gopi chat`'s own screen

Audience: an agent (or a human) who has changed the chat chrome — the status
row, the banner, the prompt, an emphasis colour — and needs to know what the
screen actually became. Read this before claiming "it looks better".

The instrument is TWO files, already in the repo:

| file | what it is |
|---|---|
| `scripts/ui/chat-ui-observe.sh` | boots `gopi chat` in a private terminal on a private display, grabs the screen with the shipped `gopi capture` verb, and measures every text band |
| `scripts/ui/chat-ui-measure.py` | the measurement itself (WCAG contrast + core-pixel fraction per band, per field). Zero dependencies beyond PIL |

## The one command

```bash
# BEFORE — the binary you want to compare against
scripts/ui/chat-ui-observe.sh --bin /path/to/gopi-before --out /path/before.png

# …land your change, rebuild…

# AFTER
scripts/ui/chat-ui-observe.sh --bin ./bin/gopi --out /path/after.png
```

Real output from this repo (2026-09-26, task `01a0dacf`):

```
chat-ui-observe: png=/path/before.png sha256_16=236a3a49bf7b8e25 display=:97 geometry=120x34
# /path/before.png  1076x642  bands=7
      rows               bg        brightest  ratio           median  med_r  core%
     1-14     (48, 10, 36)  (255, 255, 255)  17.58  (180, 167, 176)   7.61   23.0
    ...
   625-641     (48, 10, 36)  (255, 255, 255)  17.58  (129, 106, 122)   3.58   14.3   ← the status row
```

Four things come out, and they are the four a UI claim needs:

1. **png** — the artifact. Attach it to the report.
2. **sha256_16** — capture identity, from `gopi capture` itself. Capture before
   and after a change: the hashes say whether the screen moved at all (equal
   hashes after a "fix" means the fix never reached the screen).
3. **bg** — the dominant background colour, computed from the capture, not
   assumed. Contrast is meaningless without it.
4. **per-band rows** — for EVERY text band: `brightest` + `ratio` (WCAG contrast
   of the glyph's stroke centre against `bg`), `median` + `med_r` (the typical
   text pixel — for anti-aliased text this is what a reader's eye integrates),
   and **core%** = the share of text pixels at ≥90% of that band's own maximum
   contrast. The last text band is the status row.

Add `--fields` to split a band into its space-separated FIELDS (the status row is
several fields on one line and they can carry different emphasis):

```
# per-field (each run of text columns inside a band, split on 6+ blank columns)
      rows    x0    x1               bg        brightest  ratio           median  med_r  core%
   625-641     0   123     (48, 10, 36)  (255, 255, 255)  17.58  (233, 229, 232)  14.10   43.5   ← the label field
   625-641   153   157     (48, 10, 36)  (255, 255, 255)  17.58  (129, 106, 122)   3.58   22.2   ← a separator
   625-641   781   835     (48, 10, 36)  (255, 255, 255)  17.58  (255, 255, 255)  17.58   60.5   ← the live hint
```

Useful flags: `--bands N` (last N bands), `--seconds N` (how long the chat runs
before the grab — raise it if the row is still "starting up"), `--geometry`,
`--font-size`, `--chat-arg --tools` (pass a flag through to `gopi chat`).

## How to read the numbers

- **`ratio` ≥ 7:1** — good. **4.5:1** — the body-text floor. **below 3:1** —
  plainly too dim. This is the WCAG relative-luminance formula, computed from a
  real pixel of a real glyph, not from the colour the code intended.
- **`ratio` can be fine while the text still reads faint.** Anti-aliasing means
  most glyph pixels are NOT the stroke centre: a light face can have a 17:1
  `brightest` and a `med_r` near 4:1 (a thin face) — while a heavier face with
  the same colour has both near 14:1. That is what **core%** separates: ~20%
  core is a sparse, faint-looking stroke; ~45% is a solid one.
- **Compare LIKE with LIKE.** The same text at the same colour but a different
  font weight is a different measurement; so is a different terminal theme (see
  Blindness below). A before/after claim names the geometry, the font size and
  the theme, and both captures use the same ones — `chat-ui-observe.sh` fixes all
  three, and prints the geometry with the result.

## Blindness — where this instrument CANNOT see

State this in any report that uses it; a measurement whose limits are unstated is
a claim of omniscience.

1. **It measures STYLE, not MEANING.** It reports brightness and stroke density.
   It cannot tell whether the words are the right words, whether the layout is
   sensible, or whether the status row is even the row you think it is — for
   that use `gopi ui_state --target chat` (the surface's own cell dump) or
   `vision_analyze` on the PNG. **Use vision to READ text; use pixels to
   JUDGE style.** A vision model reporting "the text looks bright" is not a
   measurement (it is also non-deterministic — the same image can draw a
   different answer twice).
2. **It sees one TERMINAL THEME.** SGR colour codes are resolved by the user's
   terminal. `chat-ui-observe.sh` pins `-bg '#300A24' -fg '#FFFFFF'` (this
   machine's gnome-terminal theme), which is a deliberate choice: the numbers
   are only comparable within one theme. If the operator's theme changes, the
   same bytes measure differently — that is a property of SGR, not a defect of
   the instrument. In particular **SGR 2 (`dim`) is a blend with the
   BACKGROUND**, so "dim" on a light theme is a different colour than here.
3. **It sees PIXELS, not the composition.** It cannot tell a text field from a
   scrollbar; the crop that finds the terminal interior is heuristic (most common
   non-black colour). A capture of a window that is mostly non-terminal will
   measure whatever is on it.
4. **Frequency-domain blindness.** A band whose glyphs are thinner than the
   capture's pixel grid, or a capture taken at the wrong scale, measures the
   rasterisation, not the design. Measure at the geometry the complaint came
   from.
5. **It says nothing about what the user MEANT.** "Hard to read" can be contrast,
   stroke density, theme, font, or simply the wrong row being painted. The
   instrument can separate the first two; the last needs `ui_state` and the code.

## The complete recipe (what a report must carry)

```bash
# 1. BEFORE: capture the shipped binary
CGO_ENABLED=0 go build -o /tmp/scratch/gopi-before ./cmd/agent/
scripts/ui/chat-ui-observe.sh --bin /tmp/scratch/gopi-before --out /tmp/scratch/before.png --fields --bands 1

# 2. make the change, rebuild
CGO_ENABLED=0 go build -o /tmp/scratch/gopi-after ./cmd/agent/
scripts/ui/chat-ui-observe.sh --bin /tmp/scratch/gopi-after  --out /tmp/scratch/after.png  --fields --bands 1

# 3. the BYTE proof: the same run with colour off must be identical, and the
#    coloured run must differ only in SGR bytes (never in cells)
h=$(mktemp -d ~/workspace/_tmp/raw.XXXXXX)
env HOME=$h GOPI_HOME=$h/.gopi GOPI_SETTINGS=$h/settings.json \
  script -qec "cd $h && <bin> chat" /dev/null > /tmp/scratch/before.ansi
```

Then report: the two PNG paths, both `sha256_16` values, `bg`, the per-field
`ratio`/`med_r`/`core%` before and after, and the geometry+theme they were taken
at. A "more readable" sentence without those numbers is not a measurement of
anything.

### The colour-off invariant (one command, byte-exact)

```bash
python3 - <<'PY'
import re
def row(p):
    d=open(p,"rb").read().decode("utf-8","replace")
    m=list(re.finditer(r"iter 0", d))[-1]
    return d[m.start()-30:m.end()+40]
b=row("before-mono.raw"); a=row("after-mono.raw")
print("identical:", b==a); print(repr(a))
PY
```

Under `--no-color` the two rows must be byte-identical — emphasis is additive,
and a palette change may never move a cell.

## Where the numbers in this repo's report came from

`devdoc/evidence/chat-ui-salience/` holds the artifacts: the operator's own live
window captured before the fix (`operator-live-before.png`), the controlled
before/after pair (`status-row-before.png` / `status-row-after.png`),
`per-field-before-after.txt` (the measurement output), and
`raw-before.ansi` / `raw-after.ansi` (the raw byte streams, so the SGR codes
can be read out of the wire rather than out of the source).
