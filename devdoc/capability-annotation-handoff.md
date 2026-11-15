<!-- doc-class: normative -->
# The annotation handoff — how to build `gopi annotate` without reading any other document

**Status**: implementation-ready handoff spec. Link `[5/9]` (`capdemo-5`) wrote it;
link `[6/9]` (`capdemo-6`) implements it. The design it hands off is
`devdoc/capability-annotation-design.md` (link `[4/9]`); **that document is not a
prerequisite** — everything an implementer needs is restated here, corrected, and
where this document and the design disagree, the disagreement is called out and
this document wins (§4.3, §8.2).

**Class**: `normative` (a contract, not a survey) — see the row's own class field
in `devdoc/DOC-INDEX.tsv`, written by the generator.

---

## 1. The contract, in five lines

1. There is **one new CLI verb**, `gopi annotate <in.png> --out <out.png>
   --mark …`, and **no new top-level tool** (Rule 3: the toolset is fixed at 17).
2. It **decodes** the input PNG, **composites** rectangles, arrows, points and
   text labels onto it, and writes a new PNG. It never modifies `<in.png>`.
3. Its marks are in **output-image pixels** — the same rectangle grammar
   `gopi capture --area` already uses.
4. It reuses `internal/text2img`'s rasteriser through **exactly one new export**
   (§6) — no new package, no new dependency, no new binary, no new file on disk
   beyond the `--out` PNG the caller names.
5. Its output is verified by **pixel comparison against an independent ground
   truth** (§10) — never by OCR, never by a vision model.

**This document does not implement the verb.** `[6/9]` does; §10 is the gate that
says when it is done, and §11 is the wiring that makes it a real verb.

---

## 2. The measured starting point (why this is not "reuse, as it exists")

Every claim below is a command and its output, measured at the head this
specification was written at — commit `b08185c2` on branch `task/capdemo5` — and
**re-measured after the document's own commit** (§14.2), where every verdict
was unchanged. The raw transcripts of both passes are
`/home/user/.gopi/sessions/capdemo-5/probes-v2.txt` and `probes-v3.txt`. An
implementer can re-run all of them in one minute.

```bash
$ grep -rn "image/draw" --include=*.go internal/ cmd/ | wc -l
0
$ grep -rn "png.Decode\|image.Decode" --include=*.go internal/ cmd/ | grep -v _test | wc -l
0
```

**Zero compositing, and zero production decodes.** Nothing in this repository has
ever *read* an image in production code — the annotator's input is an existing
file, so this is the decisive negative: the capability is absent, not merely
unreachable.

The one thing that *is* reusable is a painter that already takes its target as a
parameter, and it is package-private:

```go
// internal/text2img/render.go:1083
func rasterInto(img *image.RGBA, alpha []byte, w, h, ox, oy int, penX, baseline float64, ink color.RGBA)
// internal/text2img/render.go:1065
func drawChainGlyph(img *image.RGBA, chain fontChain, size float64, r rune, penX, baseline float64)
```

Both are called only from inside `internal/text2img` (`internal/text2img/render.go:1065`,
`:1266`). **So "reuse the existing rasteriser" is a one-export change, not a
no-change one**, and the export is specified exactly in §6.

### 2.1 Two traps, both measured, both of which change the test plan

```bash
$ gopi -h | grep -c "gopi annotate"          # 0
$ gopi completion bash | grep -c annotate    # 0
$ grep -c annotate internal/config/cli_verb_guard.go   # 0
$ gopi annotate --help ; echo "exit=$?"
GoPi — a tool-using AI coding agent (DeepSeek / Anthropic-compatible endpoints).
exit=0
```

**Trap 1 — the dispatcher is open.** A word that is not a verb is *not refused*:
it is folded into the agent prompt as task input, and exits 0 printing the
top-level help. So a test that asserts "`gopi annotate …` exits non-zero before
the verb exists" is **wrong**, and a red-state test must assert on the
**artifact** (a PNG that is not a copy of the input, a JSON result on stdout),
not on the exit code.

**Trap 2 — the help budget was one line, and it has since been raised.** When
this plan was written the top-level usage was `159` lines against
`topLevelUsageMaxLines = 160` (`cmd/agent/usage_test.go:153`), and the budget
was not to be raised. **That is no longer the state of master**: on 2026-10-02
the cap was raised to `176` with the measurement recorded at the constant,
because the text already measured exactly `160` and the annotate verb's
advertised row did not fit. The instruction below is therefore a RECORD of the
constraint this plan was written under, not a live budget: a new verb's
advertised row must fit inside the current cap, whose value is stated at the
constant and nowhere else.

---

## 3. The verb — CLI surface (verbatim; treat as the contract)

```
gopi annotate <in.png> --out <out.png> [--mark SPEC]… [flags]

  --out PATH      Output PNG. REQUIRED. Must differ from <in.png>.
  --mark SPEC     One mark. Repeatable. Four colon-separated fields (see below).
  --size INT      Label font size, px. Default 20.
  --lh INT        Label line height, px. Default 32.
  --font PATH     Font path. Default: text2img's autodetected CJK chain.
  --display PATH  Read the cell grid from this dump and resolve cell anchors
                  against it. Default: discovery — the newest `*-screen.dump`
                  whose recorded pid is alive (the resolution `gopi ui_state`
                  already performs; ONE implementation, never a second one).
  --json          Emit the result as JSON (the default output shape).
```

`--mark` grammar, and the reason it is one repeatable string rather than five
flags: one mark is one flag, so a still with four marks is four arguments rather
than twenty.

```
--mark <kind>:<geometry>:<label>:<anchor>

  kind      rect | arrow | point
  geometry  WxH+X+Y  — pixels of the OUTPUT image, top-left origin
  label     free text; spaces allowed; no colon; may be EMPTY
  anchor    fixed (default) | <row>#<col> | <row>#<col>-<row>#<col>

  fixed              the geometry IS the output rectangle
  <r>#<c>            one 0-based cell of the dump's grid
  <r>#<c>-<r>#<c>    a cell rectangle
```

The geometry letters are the same letters `gopi capture --area` accepts
(`WxH[+X+Y]`, offsets optional and read as `+X+Y`), because two verbs that take a
rectangle take it the same way.

`--mark` is the **only** grammar; there is no `--marks @file.json` alternative
(one input shape, one parser, one error message — see §8.2).

---

## 4. Geometry units

### 4.1 One rule

**Everything in a `--mark` is PIXELS of the output image, unless the `anchor`
field is a cell reference — in which case the geometry is CELLS of the dump's
grid.** One mark is one unit system; a mark that mixes them is refused (§8).

### 4.2 The conversion, and the calibration finding

```
x_px = k · logical_col        y_px = j · logical_row
```

`k` = pixel width of a logical column, `j` = pixel height of a logical row.
`internal/text2img` already parameterises both and exposes them publicly —
`GridOptions.CellW` ("单元格宽(px)") and `GridOptions.Lh` — so a cell mark
converts through the *same* fields the grid renderer uses, and the converted
rectangle is then drawn by the **same rectangle code** as a `fixed` mark. One
drawing path, two ways to name one rectangle.

**This conversion is NOT yet measured end to end, and that is the first work
item of `[6/9]`.** `.gopi-chat-dump`'s record line is the cell authority and
carries *no* cell→pixel field at all — verbatim, from a real artifact:

```
#gopi-chat-dump pid=1724010 target:self:chat-tools tools=on terminal=117x26 model=117x26 grid=117x26 geometry=layout mode=ansi frames=32945 dumps=4467 started=2026-09-29T00:45:01Z
#gopi-screen-dump v=1 cols=117 rows=26
```

Every number in it is **cells**; the pixel half comes from the terminal (XTerm /
the profile), i.e. it is CONFIGURED, not a font metric. So the implementer runs
the calibration first (§10, item 6): annotate one cell you can read, measure the
offset between the mark's ink and the ground truth's ink, and record it.

- **Measured offset = an exact multiple of the logical cell size** → the
  conversion above is correct and `k`, `j` are the terminal's own numbers.
- **Measured offset is not such a multiple** → the design's premise is wrong and
  the anchor must become a **calibrated affine per host** (`x_px = a·col + b`),
  with `a`, `b` recorded in the test fixture. This is the single finding that
  sends the design back; it is measured *before* the implementation is called
  done, never after.

### 4.3 What the cell channel cannot do (stated, not hidden)

`gopi capture` is **pixels and no grid**; `gopi ui --dump-screen` is a **grid and
no pixels**. A mark on a live-desktop still therefore has no cell grid to be
anchored to, and `--mark …:…:…:fixed` is the shape for that still. Cell anchors
require the dump's surface to be the subject. The two specimen stills in §12 are
one of each, which is why they are a pair.

---

## 5. Data model

```go
// kind: named constants, never bare strings (Rule 1).
type MarkKind int   // MarkRect | MarkArrow | MarkPoint

type Mark struct {
    Kind   MarkKind         // rect | arrow | point
    Rect   image.Rectangle  // OUTPUT-IMAGE PIXELS, always, after resolution
    Label  string           // "" = no label
    Anchor string           // "" (fixed) or the cell reference, kept for provenance
}

type Result struct {
    Path, Input        string
    Width, Height      int   // of the OUTPUT image
    Marks              int   // marks requested
    InkPixels          int   // MEASURED: pixels this run changed
    AnchorSource       string // "fixed" | "cell" | "mixed" | "none"
    GridCols, GridRows int    // 0,0 when a cell anchor was used and no dump was found
    GridPath           string
}
```

`InkPixels` is the anti-false-success field, and it has a precedent inside the
package it reuses: `text2img.Result.Ink` is a *measured* count and both render
paths **refuse to write** a page whose ink is 0 while the input had content
("拒绝写出空白捕获"). `gopi annotate` inherits the rule: **N marks requested and
0 pixels changed ⇒ refuse to write, exit 1** (Rule 15 — no silent failure).

---

## 6. The label rendering path — the one export, named

| Need | Source | Status |
|---|---|---|
| Read the input PNG | `image/png` (stdlib) | new import, stdlib only |
| The target canvas | the input's own `*image.RGBA` | new (one allocation, the decode) |
| Font chain + CJK fallback | `internal/text2img`'s `loadChainFromPaths` / `fontSearchPaths` | unexported → reached through the export below |
| Glyph rasterisation (4× supersampled, no CGO) | `sfnt.rasterGlyph` | unexported → reached through the export below |
| Glyph blit onto an arbitrary `*image.RGBA` | `rasterInto` (already takes `img`) | unexported → reached through the export below |
| Cell width / line height | `GridOptions.CellW` / `.Lh` | public |
| Dump discovery + identity | `internal/uiclient`'s dump reader (`gopi ui_state`'s own) | public |

**Exactly one export lands, in a new file `internal/text2img/annotate.go`.** Do
not widen `Options` or `Result`, and do not export `drawChainGlyph` /
`rasterInto` / `loadChainFromPaths` — their signatures are internal layout
detail, and exporting them freezes that detail for every future caller.

```go
// Ink is an opaque ink (any RGB) for compositing onto an EXISTING image.
// It exists because the package's own blend() derives grey from alpha
// (render.go:1113: `return color.RGBA{v, v, v, 255}`) — correct on the white
// paper it was written for, wrong as a compositing operator over a photograph.
type Ink struct{ R, G, B uint8 }

// DrawText draws text onto img at penX/baseline in ink, and returns the number
// of pixels it inked. It reuses the package's font chain, rasteriser and glyph
// blit; it does NOT create a canvas (contrast RenderFile/RenderGrid, which do).
// A label containing a newline is laid out on successive lines spaced by the
// package's own line-height rule for sizePx.
func DrawText(img *image.RGBA, text string, penX, baseline float64,
              sizePx int, font string, ink Ink) int

// FontScale returns the package's line-height rule for a size:
// int(float64(sizePx)*lineHeightRatio + 0.5) — the same arithmetic the package
// already applies, never a second copy of the ratio (Rules 1, 13, 14).
func FontScale(sizePx int) int
```

**Why the blit is an export and the geometry is not.** A rectangle, a point and
an arrow are integer `image.RGBA` writes — ~60 lines with no font, no
antialiasing and no metric (their exact algorithms are §7). The label is the only
element whose rendering *is* a font rasteriser, and reusing one glyph-blit path
is the entire point of this link; writing a second rasteriser is not on the
table. So: **`DrawText` is the export, `SetRGBA` loops are the annotator's own
code.**

Marks are drawn in a **fixed composite order, all of one kind before the next**:
`rect`, then `point`, then `arrow`, then every label on top. Never "the order a
map iterated" — determinism is a tested property (§10, item 8).

---

## 7. The primitive algorithms (exact, and chosen so one stroke width looks even)

The mark ink is a single colour, `MarkInk = color.RGBA{220, 30, 30, 255}`
(named constant; contrast against both a dark terminal and white paper).

**Stroke**: `THICK = 2` px, applied by these two rules and no third one:

- **Rectangle** (also the label chip's border): `THICK` nested outlines, the
  `i`-th grown **outward** by `i` px on every side, each edge filled over its
  full grown length.
- **Line** (the arrow shaft and its barbs): the integer **Bresenham** raster of
  the segment, plus, for each raster cell, the cell one row **below** it
  (clamped to the image) — i.e. `THICK` consecutive raster rows per column.

**Arrowhead**: `HEAD = 8` px. Two barbs, drawn with the line rule, from the tip
`(x1,y1)` back at `ang ± 25°`, where `ang = atan2(y1-y0, x1-x0)` and the barb
direction is `ang + 180° ± 25°`. Draw the shaft first, then the barbs.

These numbers are not taste: they are the geometry the specimen stills in §12
were composed with, and §10's IoU fixture asserts that an implementation of these
rules reproduces them (§12.3 reports the measured agreement).

---

## 8. Refusals — every one of them a test

Each refusal is exit 1, names what is wrong verbatim, and writes **no** output
file (Rule 15: an error is never swallowed, and a half-written artifact is worse
than none).

### 8.1 The list

| Condition | Behaviour |
|---|---|
| `--out` missing | refuse, naming the flag |
| `--out` equal to `<in.png>` | refuse, naming the path — an in-place annotation destroys the evidence it exists to point at |
| input not a PNG / not decodable | refuse, `%w`-wrapped (Rule 16) |
| `--mark` malformed: wrong field count, unknown kind, unparsable geometry | refuse, quoting the spec back verbatim |
| **mark geometry outside the image** | **REFUSE** — see §8.2 |
| label chip outside the image | refuse, naming the chip's computed box |
| cell anchor given, no dump / dump pid dead / `geometry=fallback` | refuse, naming what is missing (§8.3) |
| N marks requested, 0 pixels changed | refuse to write (§5) |

### 8.2 The off-image decision — a deliberate reversal of the design's §3.5

The design proposed to **clip** an overhanging rectangle and report
`"clipped": true`. **This specification refuses instead**, and the reason is the
repository's own precedent, quoted from the surface it comes from:

```bash
$ gopi capture --area 400x300+99999+10 --out /tmp/x.png ; echo "exit=$?"
gopi capture: screen_capture: area 400x300+99999+10 on a 1920x1080 display: the
rectangle runs past the root window: 400x300+99999+10 does not fit 1920x1080
exit=1
```

*(This first block is the verbatim run whose display numbers the spec quoted at
draft time; the block below is the same command re-measured at link `[5/9]`'s
head. Both are real runs on this host; the display size differs between them
because the session's X display did.)*

> "the rectangle must FIT the display — one that runs off the screen is refused,
> never silently clipped" — `gopi capture -h`

Re-measured at link `[5/9]`'s head, together with its **control** — the same flag
on a rectangle that fits. Both were run twice at this head: once with the ambient
display (`DISPLAY` unset → `:0`, 1920×1080, the numbers printed immediately above
*and* by the fresh
`go build` binary), and once against the session's own live display, which the
earlier probe recorded at 1600×900. **The refusal is size-independent and only
the message's numbers move** — which is itself the point for the implementer:
the check is "does this rectangle fit THIS surface", and the surface is an input,
never a constant.

```bash
# (a) ambient display at this head — the same shape, 1920x1080
$ gopi capture --area 400x300+99999+10 --out …
gopi capture: screen_capture: area 400x300+99999+10 on a 1920x1080 display: the
rectangle runs past the root window: 400x300+99999+10 does not fit 1920x1080
exit=1

# (b) the session's live display at this head — 1600x900, same verdict
$ DISPLAY=:1 gopi capture --area 400x300+99999+10 --out …
gopi capture: screen_capture: area 400x300+99999+10 on a 1600x900 display: the
rectangle runs past the root window: 400x300+99999+10 does not fit 1600x900
exit=1
$ DISPLAY=:1 gopi capture --area 400x300+10+10 --out …
exit=0   # JSON result; sha256_16 cb3a983a8106c9e4 — the rectangle was written
```

The control matters for the implementer: the refusal is about the rectangle, not
about `--area`, so `annotate`'s off-image refusal must likewise leave every
in-image mark working.

A verb that takes `WxH+X+Y` and silently delivers a different rectangle than the
one asked for is the same defect in both places, and an annotation is *evidence*:
a clipped box changes what it claims to point at. So: **a mark whose declared
geometry is not fully inside the output image is refused**, and the error names
both the mark and the image bounds. Consistency between the two rectangle-taking
verbs is worth more than the convenience of clipping, and the decision is
recorded here rather than inherited silently.

### 8.3 The fallback-geometry refusal

`internal/uiclient` documents a dump whose geometry it could not establish
(`GeometryFallbackNotice`: *do not read wraps or truncation off it as if they
were the screen's*). Cell anchors resolved against such a dump are refused by
name — anchoring a mark on a fallback grid is the same silent failure one layer
up.

---

## 9. Result shape

Default (and only) output on success is one JSON object on stdout, so the surface
is machine-readable without a parser per caller:

```json
{"path":"…/b.png","input":"…/a.png","width":1353,"height":740,"marks":2,
 "ink_pixels":48211,"anchor_source":"cell","grid_cols":117,"grid_rows":26,
 "grid_path":"…/chat-screen.dump"}
```

`annotateIoUFloor = 0.95` is the named constant §10 asserts against (Rule 1).

---

## 10. Test plan — red first, then green, and every render claim by pixels

Two packages, plus the E2E and the wiring gate. **`go test` is not a substitute
for the E2E** (Rule 31): items 8–9 run the real binary on real PNGs.

`[6/9]` must land each item **after watching it fail for the right reason**
(red-then-green), and record both observations.

1. **The export draws, and measures.** `DrawText` onto an existing
   `image.NewRGBA` returns `> 0`, and leaves every pixel outside the text's ink
   untouched (the "does not create a canvas" contract). *Red*: before the export
   exists the test does not compile — that is the expected red, recorded as such.
2. **Ink colour is honoured.** A red `Ink` produces red pixels, not grey. This is
   the direct regression test for `blend` (render.go:1113): *red* = the grey
   derivation reused by accident, *green* = a real ink operator.
3. **Geometry parsing**, table-driven: every accepted spelling, and each malformed
   shape refused with the spec quoted back.
4. **Refusals of §8.1**, one test each — including the off-image box, which is
   the deliberate reversal and therefore gets its own named red
   (`--mark "rect:400x300+99999+10:…"` on a 1353×740 still ⇒ exit 1, no file
   written; pre-fix behaviour, if implemented per the design's clip rule, exits 0
   with a partial box).
5. **Cell anchor with no dump** ⇒ exit 1 naming what is missing; **fallback
   geometry** ⇒ exit 1.
6. **The anchor reconciliation, measured at pixel level — the headline test.**
   Against a committed fixture dump (a small real `chat-screen.dump`; the host
   carries 23 of them and the smallest is **4443 bytes**, so the fixture is
   cheap):
   - render the dump's own grid rows with `text2img.RenderGridBytes` at
     `CellW = k`, `Lh = j`;
   - draw the same mark at its cell rectangle on that ground truth;
   - composite that mark onto a real capture;
   - assert **IoU(ground-truth ink, annotator ink) ≥ `annotateIoUFloor`**.
   **No OCR, no vision model** (渲染真值). A one-cell conversion error moves the
   mark by `k` px and blows through the floor — which is what makes this test
   decisive rather than decorative. This is also the **calibration** run of §4.2:
   report the measured offset, and record whether it is an exact multiple of the
   cell size.
7. **Determinism**: the same arguments produce byte-identical PNGs (the fixed
   composite order of §6).
8. **E2E on a real capture**: `gopi capture --window --out a.png`, then
   `gopi annotate a.png --out b.png --mark "rect:200x40+14+13:status row:9#10-9#30"`
   ⇒ exit 0, `b.png`'s `sha256` differs from `a.png`'s, the JSON's
   `ink_pixels > 0`, and **every pixel outside the declared marks is
   byte-identical between `a.png` and `b.png`**. *Red state, per §2.1 Trap 1*:
   before the verb exists this command exits **0** too (the dispatcher folds it
   into a prompt) — so the red is asserted on the **missing artifact**, never on
   a non-zero exit.
9. **Four marks on one still**, and the `fixed` spelling, both E2E.
10. **Wiring gate**: extend the G6 case list with the new advertised form,
    regenerate the inventory, and assert `gopi -h | wc -l` stays within
    `topLevelUsageMaxLines` (raised `160 -> 176` on 2026-10-02; the current value
    lives at the constant in `cmd/agent/usage_test.go:153`).

---

## 11. Wiring the verb (Rule 3's boundary, done the repo's way)

The implementer must do all four; a verb that runs but is not registered is not
delivered:

1. **`internal/config/cli_verb_guard.go` → `PositionalVerbs`** — add the word
   (kept alphabetical; the mirror test compares both directions by value).
   Measured absent today: `grep -c annotate internal/config/cli_verb_guard.go`
   → `0`.
2. **The top-level help row** — it must fit the remaining line of the 159/160
   budget (§2.1).
3. **`gopi completion bash`** — measured `0` hits for the word today.
4. **The G6 help-surface case list** (`g6Cases`; floors
   `len(g6Cases) >= 85 && len(g6BlockedForms) >= 28`,
   `cmd/agent/cli_tools_maintenance_help_surface_basic_test.go:834`) — a newly
   *advertised* form must be **typed into the list** or declared blocked, or the
   gate reds.

**That gate is RED on this branch and on master before you write a line**
(§14.2 records the exact two failures and proves they are not this link's):
`gopi text2img report.txt --out out.png --size 28` is advertised but neither in
the inventory table nor in `g6Cases`, left behind by `852ff9c0`. So do not read
its red as your own damage — and do not "fix" it by deleting the form from the
case list: the form is real and advertised.

Rule 3 is satisfied by construction: no tool is added, and the verb is reachable
through `bash` exactly as `gopi capture` and `gopi text2img` already are.

---

## 12. The specimen stills — what they were, and what is there now

> **HISTORICAL (added by link `[8/9]`, 2026-09-29).** This section records the
> **specimen** stills link `[5/9]` produced and the measurements that backed
> them. Those two PNGs
> (`05-specimen-annotated-cjk-render.png`, `06-specimen-annotated-live-window.png`)
> **were replaced by the verb's real output** at link `[8/9]`:
> `05-annotate-cjk-render.png` and `06-annotate-live-window.png`, produced by
> `gopi annotate` at commit `30f655b1` — they are §8's verb, delivered by link
> `[6/9]`, and `stills/CAPTIONS.md` carries their provenance and pixel-level
> measurement. **No specimen ships any more** (§2's absence is false today: the
> compositor exists). The paragraphs below are kept because they are the record
> of what was measured while the verb did not exist, and because §12.3's method —
> compare two rasterisers of one declared geometry by pixel IoU — is the method
> link `[8/9]` re-used to measure the verb's output.

Two specimens shipped beside the other stills, as links `[5/9]`–`[7/9]` left
them (the names are recorded here as **history**, not as paths that exist today —
link `[8/9]` replaced the files, and `scripts/capdemo-external-check.sh` flags any
*tracked doc that cites a bundle file which does not exist*):
`05-specimen-annotated-cjk-render.png` and `06-specimen-annotated-live-window.png`,
each with its provenance row in `stills/CAPTIONS.md`.

**They are specimens, not verb output.** gopi has no compositor today (§2), so
these were **hand-composed** by a driver script kept outside the tree (Rule 30 /
Rule 37: the driver is session evidence, not a repo artifact). Every one of their
**label glyphs** was rendered by the **real `gopi text2img` binary** and pasted
in, so no glyph in a specimen comes from the composer — and that is **measured,
not asserted**: comparing each specimen's chip region against the `gopi text2img`
PNG that produced it gives a glyph-ink **IoU of exactly 1.0000** (1404 / 1404 and
2111 / 2111 inked pixels, identical masks — `specimen-glyphcheck.json`), while the
same region's border pixels differ because the border is the composer's. The
provenance of a specimen is therefore split and each half is measured: **glyphs =
gopi's (IoU 1.0), geometry strokes = the composer's (§12.3's 0.34–0.74 per
primitive; 0.69 / 0.64 overall)**. Each
caption, and each image, says so in-line — a reader who opens the PNG without the
caption still sees `SPECIMEN` in it.

**What they point at** — each specimen names a defect that was actually found by
an earlier link, and the base still it annotates:

| specimen | base still | the defect it points at |
|---|---|---|
| `05` | `03-text2img-cjk.png` | the renderer's own two-line output: the second line ends **89 px** short of the first (declared geometry: L1 `y13..35`, L2 `y45..66`), and this base still is the one whose CLI shape carried link `[3/9]`'s `--out` gap (`gopi text2img: unknown flag --out`, exit 1) |
| `06` | `02-live-desktop-window.png` | the base still's own caption already had to explain in prose what the reader should look at — and this still is a **pixel-only surface with no cell grid** (§4.3), which is why its mark is `fixed` and cannot be a cell anchor |

**12.3 Measured agreement (the honest numbers).** The declared geometry was
re-rendered independently — integer rectangle + integer Bresenham + arrowhead in
plain numpy, no imaging library — and compared against the composed specimen by
pixel IoU, per declared primitive, over that primitive's region. The two sides
differ in rasteriser on purpose and the caption above is the record of it: the
**ground-truth** side is numpy, the **specimen** side is `compose.py` (PIL
`ImageDraw`), so what the table below measures is exactly "two rasterisers of one
declared geometry", which is why its numbers are the size they are:

| specimen | rects | arrow | label chip | overall |
|---|---|---|---|---|
| `05` | 0.661, 0.684 | 0.551 | 0.741 | **0.69** |
| `06` | 0.696 | 0.340 | 0.700 | **0.64** |

**These are below the 0.95 floor the verb must meet, and that is the point.** The
disagreement is anti-aliasing and stroke-rounding between two rasterisers of the
same geometry, not a geometric disagreement — the specimen's ink never leaves a
declared mark's region (`specimen_mark_px_outside_declared_regions = 0`, the
measured key), and every pixel outside the declared marks is **byte-identical**
to the base still (`changed_pixels_outside_declared_regions = 0`, also measured).
Note
the direction, because the opposite sentence is the easy mistake: the base's own
rectangle is **NOT** byte-identical as a whole — the marks are drawn *on* the
base, which is the entire purpose of an annotation. Measured in
`specimen-basecheck.json`: `base_rect_matches_base_still_bytewise = false`,
`max_abs_diff = 225` (`05`) / `221` (`06`), with every differing pixel confined to
the declared marks' bounding boxes. The
specimen is a *shape demonstration*; the 0.95 floor belongs to the verb, whose
two sides of the comparison are the same code with the same arguments. **A
specimen's IoU is not evidence about the verb, and must never be quoted as if it
were.**

**The composition method, exactly** (so no reader mistakes a specimen for the
renderer's output): decode the base still → paste a caption band below it whose
height is the label chip's own measured height → draw each mark with the §7
algorithms → paste each chip (rendered by `gopi text2img`) on top → border each
chip with the §7 rectangle rule → write. The base still is pasted at its own
origin and is **not** otherwise altered: every pixel of the base's rectangle
outside the declared marks is byte-identical to the base still, and every pixel
that differs lies inside a declared mark's bounding box (measured; §12.3).

**The composer is not gopi, and it is named.** The driver is a Python script
(`compose.py`) using **Pillow (`PIL.ImageDraw`)** for the rectangle outlines, the
line, the arrowhead and the pastes — so the *geometry* half of a specimen is the
work of an external tool, while the *glyph* half is `gopi text2img`'s output
(IoU 1.0, above) and the *base* is gopi's own capture or render. That split is
exactly why §12.3's ground-truth comparison is two rasterisers and not one code
path run twice, and why its numbers (0.34–0.74) are below the verb's floor by
construction. **Nothing here is gopi's compositor**; a reader who takes a
specimen's strokes as evidence about the verb has been told, in the caption, in
this paragraph, and by the `SPECIMEN` header baked into the PNG itself, not to.

---

## 13. Rule 35 — the four answers, restated for the implementer

1. **What breaks without it.** A claim about a defect degrades into prose: today
   the bundle's captions must *tell* the reader which cell to look at. There is
   no observed failure yet only because the demo has not had to point at a defect
   in an image; the first still that must ("this cell is blank and should not
   be") has no way to say so except in words.
2. **Why it cannot be fixed in-process.** The failure **is** the absence of a
   code path (§2), not a misbehaviour of one — there is no gopi process to fix.
   The alternative ("let `capture` annotate as it captures") is refused on
   purpose: a capture must stay a faithful, unmodified record of the screen, or
   the annotation contaminates the evidence it points at and shot-vs-annotated
   can no longer be compared.
3. **Who owns its lifecycle.** The verb owns nothing. It creates exactly one
   thing — the `--out` PNG the caller named, which is a user deliverable by
   construction. No state file, no lock, no temp path, no cache, no config key,
   no background process. Nothing to reclaim, because nothing persists.
4. **What the next person sees.** Nothing running and nothing left behind: an
   extra verb in `gopi -h`, one help page, and one export in a package they can
   read. A file that appears is a file the caller asked for by name.

Rule 27 applies to the implementer's tests: no load may be generated to make a
number move, and no test may saturate the device, CPU or network.

---

## 14. What this link measured, and what it did NOT

**Measured here** (commands in §2, §8.2, §12.3): the absence at this head; the
open dispatcher; the help budget; the off-screen refusal precedent, verbatim; the
cell authority's lack of a pixel field; the composition and the pixel-level
agreement of the two specimens; and the wiring facts of §11.

**NOT done here, and named so nobody reads it as done:**

- **The verb is not implemented** — `[6/9]` builds it.
- **The anchor conversion is not measured end to end.** §4.2's calibration is the
  first work item of `[6/9]`. Until it runs, `x_px = k·col` is a derivation from
  the surfaces as they exist, not a measurement.
- **`--mark` is unversioned.** A third mark kind would change the field count and
  break old command lines. The JSON alternative was considered and refused as a
  second artifact; the trade is stated so it is a decision, not an accident.
- **The specimens are hand-composed** and their IoU numbers are below the verb's
  floor by construction (§12.3). They show the shape; they are not the verb.

### 14.1 How to re-derive every number in this document in four commands

The point of a number here is that it is the *output of a command*, not a claim —
so each one is re-runnable from the repository root, with no instrument to trust:

```bash
grep -rc "image/draw" --include=*.go internal/ cmd/ | awk -F: '{s+=$2} END{print s}'   # 0
grep -rn "png.Decode\|image.Decode" --include=*.go internal/ cmd/ | grep -vc _test     # 0
gopi -h | grep -c annotate ; gopi -h | wc -l          # 0, then 159 (budget 160)
DISPLAY=:1 gopi capture --area 400x300+99999+10 --out /dev/null ; echo $?   # 1: "does not fit"
```

(Measure a display the way `capture` reports it: it names the live displays on
this host when the one you asked for does not answer. The **control** — the same
verb with a rectangle that fits — is what proves the refusal is about the
rectangle and not about `--area`.)

The specimen numbers are re-derived with the composer's own ground truth, which
needs no imaging library beyond PIL for the driver:

```bash
python3 /home/user/.gopi/sessions/capdemo-5/gt_iou.py    # reproduces §12.3 exactly
```

**Run at this link's head, it reproduced the published figures unchanged**
(`overall` IoU `0.694613` / `0.641183`; byte-identical JSON to the committed
`specimen-iou.json`). That re-run is the reason §12.3's table can be quoted: the
measurement is not one run's word, it is a script's output.

### 14.2 The close-out re-measurement, after the commit

Every claim above was made against a tree that was still being edited; a claim
about a *commit* has to be taken at the commit. So the whole set was re-run after
`ece1d70e`, against a binary **built from this branch's own head** (not the
installed one):

```bash
CGO_ENABLED=0 go build -o $SESS/gopi-capdemo5 ./cmd/agent/   # exit 0
$SESS/gopi-capdemo5 -h | wc -l                    # 159   (budget 160)
$SESS/gopi-capdemo5 -h | grep -c annotate         # 0
$SESS/gopi-capdemo5 annotate --help ; echo $?     # 0     — Trap 1, still open
$SESS/gopi-capdemo5 doccheck ; echo $?            # "the devdoc corpus is consistent", exit 0
$SESS/gopi-capdemo5 capture --area 400x300+99999+10 --out …  # exit 1, "does not fit 1920x1080"
git status --porcelain                            # (prints nothing)
```

Verdicts unchanged at the commit: the absence counts are still `0` (`image/draw`,
production `png.Decode`), the help budget is still 159/160, no `annotate` row is
advertised, the dispatcher still folds the unknown word in with exit `0`, the
off-image rectangle is still **refused** (exit 1), and the corpus is consistent.
Transcript: `probes-v3.txt` in the session directory (§15).

> **SUPERSEDED at link `[8/9]`** (2026-09-29, commit `30f655b1`): the verb now
> exists, so all four of those state-of-the-world verdicts are **false today** —
> `image/draw` is referenced (this section's own implementation), `gopi -h`
> advertises an `annotate` row, and the dispatcher no longer folds the word
> (`gopi annotate -h` renders the verb's page). They were true when measured and
> they are kept as the pre-implementation record; `stills/CAPTIONS.md` and
> `docs/archive/demo/external/README.md` carry the post-implementation measurements, and
> `bash scripts/capdemo-external-check.sh` fails if the published bundle
> re-asserts the superseded ones.

**One red the implementer will meet, and it is NOT from this link — check it
yourself before blaming your own diff.**

```bash
$ go test ./cmd/agent/ -run 'TestToolsMaintenanceHelpSurface' -count=1
--- FAIL: TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation
    `gopi text2img report.txt --out out.png --size 28` is advertised by the help
    (text2img) but the inventory does not list it — re-run
    scripts/g6-tools-maintenance-help-surface.sh ./bin/gopi and regenerate the table
--- FAIL: TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason
    "gopi text2img report.txt --out out.png --size 28" (text2img) is advertised
    and NEITHER tested NOR declared blocked
```

This is **link `[3/9]`'s `--out` fix leaving the G6 inventory behind**, and it is
red on **master**, not on this branch: the three files that decide it
(`cmd/agent/cli_text2img.go`, `devdoc/gopi-tools-maintenance-help-surface-inventory.md`,
`cmd/agent/cli_tools_maintenance_help_surface_basic_test.go`) are **byte-identical
at `master`, at this link's start head `b08185c2`, and at this link's commit
`ece1d70e`** — verified with `git rev-parse <rev>:<path>` on all three, and this
link's commit touches **no `.go` file at all** (`git show --stat ece1d70e`). The
advertised form was added by `852ff9c0` (`fix(text2img): accept --out …`) while
the inventory was last regenerated at `11637728` (2026-09-27), i.e. two days
earlier. **This link does not fix it**: the repair is a `cmd/agent` + inventory
change, outside a documentation link's scope, and it is named here so `[6/9]`
starts from a known-red tree instead of mistaking it for its own damage. The
repair, when someone takes it, is the one the gate prints: re-run
`scripts/g6-tools-maintenance-help-surface.sh ./bin/gopi`, regenerate the table,
and either type the new form into `g6Cases` or declare it blocked with a reason.

---

## 15. Evidence index

Session directory `/home/user/.gopi/sessions/capdemo-5/` (outside the tree):

| File | What it holds |
|---|---|
| `L4-design.md` | link `[4/9]`'s design, as handed over |
| `bin-gopi` | the binary every probe and every label render used |
| `compose.py`, `make_specimens.py` | the hand-composer (§7's algorithms) and its driver |
| `gt_iou.py` | the independent ground truth and the IoU measurement of §12.3 |
| `specimen-receipts.json` | per-specimen geometry, digests, pixel counts, the base still each one annotates, and the composition canvas |
| `specimen-iou.json` | the §12.3 numbers: geometry IoU per primitive, overall, and the outside-the-marks byte identity |
| `specimen-basecheck.json` | the base-rectangle measurement that corrected §12.3's wording (marks are drawn *on* the base) |
| `specimen-glyphcheck.json` | the glyph half: each chip region vs the `gopi text2img` PNG that produced it — ink IoU **1.0** |
| `probes-v2.txt` | the verbatim transcript of §2 / §8.2 / §14.1 at this link's head (digests, counts, exit statuses) |
| `probes-v3.txt` | the **post-commit** re-measurement (after `ece1d70e`): the absence counts, the open dispatcher, the off-image refusal *and its control*, `gopi doccheck` exit 0, `git status --porcelain` empty, and both specimens' `sha256` — i.e. the evidence for §14.2 |
| `specimen-iou-rerun.json` | `gt_iou.py`'s output on a second run, byte-identical to `specimen-iou.json` (§14.1) |
| `gopi-capdemo5` | the binary built by §14.2's `go build` from this branch's head |
| `calib-window.png`, `*‑canvas.png` | the captures and composition canvases |
| `*-chip*.txt`, `*-chip*.png` | the label text and the `gopi text2img` renders of it |
