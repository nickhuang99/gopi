<!-- doc-class: normative -->
# The annotation layer — the question answered by measurement, and the smallest shape that gives it

Task `capdemo-4` (link `[4/9]` of the capability-demo chain), branch
`task/capdemo4`, worktree `/home/user/workspace/gopi-capdemo4`.

This document is a **design**, not an implementation: no code was written for
the annotator. It records what was driven to answer "does gopi have this
today?", the three decisions the operator asked for, and a specification
precise enough that link `[5/9]` can build it without asking a question.

Every command below was run on this worktree's own built binary
(`CGO_ENABLED=0 go build -o /tmp/gopi-capdemo4 ./cmd/agent/`, exit 0) with
`DISPLAY=localhost:15.0`, `XDG_SESSION_TYPE=tty`. Evidence files live under
`/home/user/.gopi/sessions/capdemo-4/evidence/` (§ "Evidence index").

---

## 1. Does the capability exist today? — measured, not asserted

**No.** The claim is three independent negatives, each with the command that
produced it and its output.

### 1.1 Nothing in the tree composites one image onto another

```
$ grep -rn "image/draw" --include="*.go" . | wc -l
0
$ grep -rn "draw.Draw" --include="*.go" internal cmd | wc -l
0
$ grep -rn "draw.Over" --include="*.go" internal cmd | wc -l
0
```

The scan is over the whole tree (production **and** tests), not just
`internal/`+`cmd/` — so it is not narrowed into agreement. `draw.Draw` is
`image/draw`'s one entry point; zero hits in either direction means the
compositing primitive has never been imported here.

The vocabulary search widens the same negative:

```
$ grep -rn "AnnotateImage" --include="*.go" internal cmd | wc -l   -> 0
$ grep -rn "AddLabel"      --include="*.go" internal cmd | wc -l   -> 0
$ grep -rn "DrawArrow"     --include="*.go" internal cmd | wc -l   -> 0
$ grep -rn "overlay image" --include="*.go" internal cmd | wc -l   -> 0
$ grep -rn "composite"     --include="*.go" internal cmd | wc -l   -> 12
$ grep -rn "blit"          --include="*.go" internal cmd | wc -l   -> 5
```

The 12 `composite` and 5 `blit` hits are **not** image compositing; they are
the word used in other senses. Verbatim (`…/evidence/P.txt`):

```
internal/tools/reflow_extension_test.go:112:	// The composite tag string the two exporter patterns build must equal
internal/standards/db_scaffold_gate_test.go:793:// assigned to a `Status:` field of a composite literal inside SampleSeed —
internal/config/gopihome_nearmiss_test.go:4:// The composite defect (a second, nested profile that no reclaimer models) is
internal/text2img/render.go:4:// head/maxp/hhea/hmtx/loca/glyf, composite glyphs and TTC collections),
internal/text2img/render.go:1080:// rasterInto blits one already-rasterized glyph box onto img at
internal/text2img/render.go:1111:// blend composites glyph coverage a over the white page: full coverage is the
```

Two of them are `internal/text2img`'s own private helpers — and that is the
whole of what exists (§1.3). The rest are Go composite literals and TrueType
composite glyphs.

### 1.2 Nothing ever DECODES an image

This is the sharper negative: an annotator whose input is an existing PNG must
read it. Production code never does.

```
$ grep -rn "png.Decode\|image.Decode" --include="*.go" internal cmd | grep -v _test
exit=1
$ grep -rn "os.Open\|png.Decode\|image.Decode" --include="*.go" internal/text2img/render.go
exit=1
```

Every one of the eight hits is a `_test.go` file asserting on an encoder's
output (`internal/tools/text2img_ink_test.go:33`, `internal/text2img/render.go`'s
three test files, `internal/shot/x11_test.go:130`, `x11rect_test.go:321`).
`internal/text2img/render.go` never opens a file for reading — it only
`os.Create`s.

### 1.3 What DOES exist: a private painter, and a public canvas-maker

`internal/text2img/render.go`'s public API (verbatim from
`grep -n "^func [A-Z]\|^type [A-Z]"`):

```
87:type Options struct {
100:type Result struct {
131:func RenderFile(src, out string, o Options) (*Result, error) {
868:type GridOptions struct {
893:func RenderGrid(out string, rows, scrollback []string, o GridOptions) (*Result, error) {
912:func RenderGridBytes(rows, scrollback []string, o GridOptions) ([]byte, *Result, error) {
```

Three entry points, and **all three create their own canvas**: `RenderFile`
renders a *text file*, `RenderGrid`/`RenderGridBytes` render a *cell grid*, and
each allocates `image.NewRGBA` and fills it white inside its own body
(`internal/text2img/render.go:1252`, `internal/text2img/render.go:1003`). A caller cannot hand any of them an
image.

What *is* reusable is one level down, and it is **unexported**:

```
$ grep -rn "drawChainGlyph\|rasterInto" --include="*.go" internal cmd
internal/text2img/render.go:1020:  ink += drawChainGlyph(img, chain, float64(o.Size), …)
internal/text2img/render.go:1055:// drawChainGlyph paints rune r with the FIRST font in the chain that can: …
internal/text2img/render.go:1065:func drawChainGlyph(img *image.RGBA, chain fontChain, size float64, r rune, penX, baseline float64, ink color.RGBA) int {
internal/text2img/render.go:1075:  return rasterInto(img, alpha, w, h, ox, oy, penX, baseline, ink)
internal/text2img/render.go:1080:// rasterInto blits one already-rasterized glyph box onto img at
internal/text2img/render.go:1083:func rasterInto(img *image.RGBA, alpha []byte, w, h, ox, oy int, penX, baseline float64, ink color.RGBA) int {
internal/text2img/render.go:1266:  ink += drawChainGlyph(img, chain, size, r, penX, baseline, black)
```

Two call sites, both inside the package; `fontChain`, `sfnt`, `loadChainFromPaths`
are likewise package-private. `drawChainGlyph(img *image.RGBA, …)` **already
takes the target image as a parameter** — it is exactly the primitive an
annotator needs, and it is unreachable from `cmd/agent`.

### 1.4 One more limit, smaller than it looks: `blend` discards the ink's RGB

```
1113:func blend(ink color.RGBA, a byte) color.RGBA {
1114:	if a == 255 { return ink }
1118:	return color.RGBA{v, v, v, 255}
```

Under glyph coverage the value it returns is grey derived from the alpha —
correct on the white paper it was written for, wrong as a compositing
operator on a photograph. This matters to §5.3 and is why the design does not
propose reusing `blend`.

### 1.5 The capture/grid surfaces, driven for real

```
$ gopi capture --window --out …/evidence/base-window.png
{"path":"…/base-window.png","display":"localhost:15.0","mode":"window","width":1353,"height":740,
 "x":343,"y":80,"depth":24,"bands":6,"bytes":148531,"sha256_16":"7e8207d73fcf50c8",
 "window":"0x2c0000a","window_route":"_NET_ACTIVE_WINDOW","hint":"…"}
exit=0

$ gopi capture --area 400x300+40+40 --out …/evidence/base-area.png
{"…","mode":"area","width":400,"height":300,"x":40,"y":40,"depth":24,"bands":3,
 "bytes":39601,"sha256_16":"1b9b0fa6212cc7ee","hint":"…"}
exit=0

$ file …/evidence/base-window.png …/evidence/base-area.png
…/base-window.png: PNG image data, 1353 x 740, 8-bit/color RGB, non-interlaced
…/base-area.png:   PNG image data, 400 x 300, 8-bit/color RGB, non-interlaced
```

Capture produces a PNG and **never annotates it** — it returns JSON naming the
file and its `sha256_16`, nothing more. The grid surfaces exist and were
driven by other links of this chain:

```
$ gopi ui_state --list
#gopi-ui-state-list scanned=31 roots=2
an interpreted cell grid (not bytes, not pixels): the escape sequences were already
interpreted by the terminal emulator, so this describes the CELLS of the screen
surf  pid      state      target                         started                dump
chat  4165135  exited     self:chat-tools                2026-09-26T12:36:17Z   /home/user/.gopi/sessions/e22f1375-…/chat-screen.dump
… (19 rows)
exit=0
```

### 1.6 No verb, and the name is free

```
$ gopi -h | grep -c "gopi annotate"      -> 0   (exit 1)
$ gopi completion bash | grep -c "annotate" -> 0
$ grep -rn "annotate" internal/config/cli_verb_guard.go   -> exit 1  (absent from PositionalVerbs)
$ grep -rn "annotate\|Annotate" --include=*.go internal/cliverbs/ internal/config/ cmd/agent/cli_verbs.go -> exit 1
```

**Why the verb list and not `gopi -h`.** `gopi`'s dispatcher is *open*: an
unrecognised first word is not refused, it is folded into the prompt as task
input (the documented "positional input" route, `internal/config/cli_verb_guard.go`).
Measured:

```
$ gopi annotate --help >/dev/null 2>&1 ; echo exit=$?
exit=0
$ gopi annotate -h 2>&1 | head -2
GoPi — a tool-using AI coding agent (DeepSeek / Anthropic-compatible endpoints).
$ gopi zznotaverb -h 2>&1 | head -3     # a name that certainly does not exist
GoPi — a tool-using AI coding agent (DeepSeek / Anthropic-compatible endpoints).
One run is one task: the model reads and edits files, runs shell commands, searches
```

A nonexistent name is accepted identically, so **"it did not error" is not
evidence**. The evidence is the enumerated surface: `gopi -h`'s command block
(51 `^  gopi ` rows at this commit), the completion list, and
`internal/config/PositionalVerbs` — none of which knows `annotate`.

### 1.7 The pixel-level statement of the gap, on real files

The only image comparison gopi can do today is a **whole-file hash over a
fresh capture**:

```
$ gopi capture --window --out …/evidence/pair-a.png   -> "sha256_16":"21dcdd9c7b2105c3"
$ gopi capture --window --out …/evidence/pair-b.png   -> "sha256_16":"21dcdd9c7b2105c3"
```

`sha256_16` answers *did the screen change?* — a change detector, not a
localizer, and it exists only inside `gopi capture`'s own JSON. There is no
substrate for "these two pixels, on this file" and therefore none for IoU
either.

**Verdict.** gopi has (a) capture to a fresh PNG, (b) text into a fresh PNG,
(c) a cell grid as text with attributes. It has **no path that takes an
existing PNG and adds a label, an arrow or a rectangle** — not in production
code, not in a test helper, not behind an unadvertised flag.

---

## 2. The three decisions

### Decision 1 — Is the capability present now?

**NO.** Evidence: §1.1 (zero `image/draw` in the tree), §1.2 (zero production
image *decode*), §1.3 (`internal/text2img`'s three public entry points each
create their own canvas; the reusable painter is unexported), §1.4 (`blend`
discards the ink's RGB), §1.6 (no verb, and the name is free in
`PositionalVerbs`). The two of these that decide it are §1.2 and §1.3: an
annotator's input is an existing PNG, and **nothing in this repo has ever read
one**.

### Decision 2 — What is the smallest thing that gives it?

**One CLI verb, `gopi annotate`, that composites marks onto an existing PNG
using code that already exists, with at most one narrow export added to
`internal/text2img` — no new module, no new dependency, no new artifact type.**

The shape, in one sentence: `gopi annotate <in.png> --out <out.png> --mark
"arrow:rect:label"` reads the PNG, draws a rectangle, an arrow from the
rectangle's edge to the target, and a text label, and writes a new PNG.

Precision about "no new module" (a false claim here would be exactly the
overstatement Rule 26 forbids):

- **Zero new packages**: the verb lands in `cmd/agent/cli_annotate.go`, beside
  `cli_capture.go` (245 lines) and `cli_text2img.go`. The drawing geometry
  lands in a new file *inside* `internal/text2img/` (`annotate.go`), because
  that is where the font chain, the rasterizer and the glyph blit already
  live — a second package would have to re-export all three.
- **At most one narrow export is added** to `internal/text2img`: a labelled
  ink primitive, §5.4. This is *not* "consume text2img as it exists" —
  `drawChainGlyph` is unexported and `blend` throws the colour away, so
  reusing the rasterizer **requires an export**. Saying otherwise would be a
  design that does not implement.

### Decision 3 — A deck of annotated stills, or an annotated walkthrough?

**A deck of annotated stills.** The moving-image option is refused on
measurement, not on taste:

```
$ grep -rn "image/gif"  --include="*.go" internal cmd | grep -v _test | wc -l  -> 5
$ grep -rn "gif.Encode" --include="*.go" internal cmd | wc -l                   -> 0
$ grep -rn "mp4"        --include="*.go" internal cmd | wc -l                   -> 0
$ grep -rn "ffmpeg"     --include="*.go" internal cmd | wc -l                   -> 0
```

The five `image/gif` production hits are **media-type strings** for
`vision_analyze`'s input allow-list and the web uploader's accept list
(`internal/tools/vision.go:46`, `internal/deepseek/claude_client.go:176`,
`cmd/agent/cli_web.go:1920`, `:1938`, `:2217`) — gopi can *receive* a GIF and
has never *written* one; `gif.Encode` appears zero times. And the word
"annotated walkthrough" in this repo is already taken: it names the web UI's
**live step-by-step streaming record** (`docs/archive/demo/external/` ships a
walkthrough page fed by SSE frames), not an image sequence. A "walkthrough of
stills" would therefore collide with an existing artifact's meaning.

The deck is also the shape the house style already uses for exactly this
purpose — a small set of PNGs each carrying its verbatim producing command:
`docs/archive/demo/external/stills/` ships four stills
(`01-gopi-self-webui.png` … `04-gopi-shot-url.png`), each with provenance in
`stills/CAPTIONS.md`, each produced by a one-line `gopi` verb. **The
annotation layer is what those captions cannot do**: `03-text2img-cjk.png` is
a *rendering* of the CJK claim, where the demo's real subject is a **defect
being pointed at**. A deck of 4–6 stills, each `gopi capture`-produced and
then annotated, is a new artifact of a kind the repo already has.

**Cost of a wrong pick is asymmetric**: a still is one PNG with a label; a
walkthrough needs a frame source, a timeline, a player and a hosting
decision — three artifacts gopi does not have, bought for a demo whose entire
argument is "look at this cell, this row, this button".

---

## 3. The design — `gopi annotate`

### 3.1 Interface (verbatim spelling; treat as the contract)

```
gopi annotate <in.png> --out <out.png> [--mark SPEC]… [flags]

  --out PATH      Output PNG. REQUIRED; must differ from <in.png>
                  (refused by name otherwise — never an in-place overwrite).
  --mark SPEC     One mark, repeatable. SPEC is four colon-separated fields:
  --size INT      Label font size, px. Default 20 (= text2img.DefaultSize).
  --lh INT        Label line height, px. Default 32 (= text2img.DefaultLh).
  --font PATH     Font path. Default: text2img's autodetected CJK chain.
  --display PATH  Read the cell grid from this dump and resolve cell marks
                  against it. Default: DISCOVERY — the newest `*-screen.dump`
                  whose recorded pid is alive (the SAME resolution
                  `gopi ui_state` performs, one implementation).
  --json          Emit the result as JSON (the default).
```

The `--mark` grammar (WHY it is a string, not five flags: one mark is one
`--mark`, so a still with four marks is four flags, not twenty):

```
--mark <kind>:<geometry>:<label>:<anchor>

  kind      rect | arrow | point
  geometry  "WxH+X+Y" — PNG pixels, top-left origin of THE OUTPUT IMAGE.
            Identical grammar to `gopi capture --area` (rule "two verbs that
            take a rectangle take it the same way").
  label     free text; may contain spaces, no colon; may be EMPTY
  anchor    fixed | <row>#<col>[-<row>#<col>]
              fixed        (default) the geometry IS the output rectangle
              <r>#<c>      one 0-based cell of the dump's grid
              <r>#<c>-<r>#<c>  a cell rectangle
```

Ordering: `rect` is composited first (all rects), then `point`, then `arrow`,
then every label on top. Deterministic given the same arguments — a fixed
order, never "the order the map iterated".

### 3.2 Geometry units — one rule, stated plainly

**Everything inside a `--mark` is PIXELS of the OUTPUT PNG, unless the `anchor`
field is a cell reference, in which case the geometry is CELLS of the dump's
grid.** One mark is one unit system; mixing inside a mark is refused.

The conversion, and it is one line, not a fitting exercise:

```
x_px = k·logical_col                y_px = j·logical_row
```

`k` = cell width in px (the terminal's logical columns), `j` = cell height in px
(the terminal's logical rows). These are CONFIGURED (XTerm / the profile), not
font-metric guesses, and text2img already parameterises both: `GridOptions.CellW`
("单元格宽(px); <=0 → 从字体实测算出终端的等宽前进量", `internal/text2img/render.go:870`) and
`GridOptions.Lh`. The anchor's cell rectangle converts to a pixel rectangle and
is then drawn **by the same rectangle code** as a `fixed` mark — one drawing
path, two ways to name the same rectangle.

### 3.3 Data model (nothing new is invented)

```
Mark{
  Kind   MarkKind   // rect | arrow | point  (enum, named constants)
  Rect   image.Rectangle   // OUTPUT-IMAGE PIXELS — always, after resolution
  Label  string
  Anchor string     // "" (fixed) or the cell reference, kept for provenance
}
Deck / Result{
  Path, Input        string
  Width, Height      int
  Marks              int
  InkPixels          int    // MEASURED: pixels this verb changed
  AnchorSource       string // "fixed" | "cell" | "mixed" | "none"
  GridCols, GridRows int    // 0,0 when a cell anchor was used but no dump found
  GridPath           string
}
```

`InkPixels` is the anti-false-success field, and it has a precedent in the
package: `text2img.Result.Ink` is a *measured* count ("实测落笔像素数…; 0 = 空白页",
`internal/text2img/render.go:109`), and both render paths **refuse to write** a page whose ink is
0 while the input had content ("拒绝写出空白捕获", `internal/text2img/render.go:1040`; the prose
path's twin at `internal/text2img/render.go:1233`). `gopi annotate` inherits the rule: N marks
requested and 0 pixels changed ⇒ **refuse to write, exit 1** (Rule 15).

### 3.4 What is reused, and the one thing that must be exported

| Need | Source | Status |
|---|---|---|
| Read the input PNG | `image/png` (std) | new import, stdlib only |
| The target canvas | the input's own `*image.RGBA` | new |
| The font chain + CJK fallback | `internal/text2img`'s `loadChainFromPaths`/`fontSearchPaths` | **unexported → one export needed** |
| Glyph rasterisation (4× supersampled, no CGO) | `sfnt.rasterGlyph` | **unexported → one export needed** |
| Glyph blit onto an arbitrary `*image.RGBA` | `rasterInto` (already takes `img`) | **unexported → one export needed** |
| Cell width / line height | `GridOptions.CellW` / `Lh` | public |
| Dump discovery + identity | `internal/uiclient`'s dump reader | public (already used by `gopi ui_state`) |

**The single export** (name and shape are the contract; land it in
`internal/text2img/annotate.go`, not by widening `Options`/`Result`):

```go
// Ink is an opaque ink (any RGB) for compositing onto an EXISTING image.
// It exists because the internal blend() derives grey from alpha and is only
// correct on the white paper it was written for.
type Ink struct{ R, G, B uint8 }

// DrawText draws text onto img at penX/baseline with ink, returns pixels inked.
// It reuses the package's font chain, rasteriser and glyph blit; it does NOT
// create a canvas (contrast RenderFile/RenderGrid, which do).
func DrawText(img *image.RGBA, text string, penX, baseline float64,
              sizePx int, font string, ink Ink) int

// FontScale returns the package's own line-height rule for a size:
// int(float64(sizePx)*lineHeightRatio + 0.5) — the same arithmetic render.go
// already applies, never a second copy of the 1.6 (Rule 1/13/14).
func FontScale(sizePx int) (lineHeight int)
```

Rectangles, arrows and points are **not** exports: they are integer
`image.RGBA` writes (`SetRGBA` on a small box loop, and a Bresenham line), ~60
lines, and they belong to the annotator. Reusing one glyph-blit path for the
label is the point; inventing a second rasteriser is not.

### 3.5 Edge cases and refusals (each one a test, §6)

- `--out` missing, or equal to `<in.png>` → refuse, exit 1, naming the path.
  An in-place annotation destroys the evidence the annotation exists to point
  at.
- Input not a PNG, or not decodable → refuse, exit 1, `%w`-wrapped.
- `--mark` malformed (wrong field count, unknown kind, unparsable geometry,
  geometry outside the image) → refuse, exit 1, naming the spec verbatim.
- Geometry partially outside the image: **clip and count only inked pixels**,
  and report `clipped: true` in the JSON. This is the opposite choice from
  `gopi capture --area` (which refuses an off-screen rectangle, "never
  silently clipped", `capture -h`) and is deliberate: a *mark* that runs 4px
  past the edge is an ordinary annotation, while a *capture* that silently
  changed size is a lie about the screen.
- Cell anchor given, **no dump found / dump pid dead / dump geometry=fallback**
  → refuse, exit 1, naming what is missing. A fallback geometry is exactly the
  case `internal/uiclient` already flags loudly
  (`GeometryFallbackNotice`: "do not read wraps or truncation off it as if
  they were the screen's"); anchoring a mark on it would be the same silent
  failure one layer up.
- Zero marks inked → refuse to write (§3.3).

---

## 4. The anchor: how a mark agrees with the cell grid (渲染真值)

The requirement: `ui_state` / `gopi ui --dump-screen` and the PNG must agree
about **which cell a mark points at**, judged at pixel level, not OCR.

**What exists.** The dump is an *interpreted cell grid*, and it is rigorously
self-describing. Verbatim from a real artifact
(`…/sessions/3f57a116-…/ui-screen.dump`, `…/c5f1189a-…/chat-screen.dump`):

```
#gopi-chat-dump pid=1724010 target=self:chat-tools tools=on terminal=117x26 model=117x26
 grid=117x26 geometry=layout mode=ansi frames=32945 dumps=4467
 recorded=2026-09-29T02:38:23Z started=2026-09-29T00:45:01Z
```

`grid=117x26` is the geometry, and `screenattr.go`'s `Cell{rune, kind, style}`
is one entry per grid column. It says so: "an interpreted cell grid (not bytes,
**not pixels**)". There is no cell→pixel field anywhere in it (searched: the
dump's fields are geometry, cursor, scroll region, lines, grid, attr runs,
scrollback, blind spots — plus `model_geometry`/`terminal_geometry`, both in
CELLS).

**So the reconciliation has exactly two requirements, both mechanical:**

1. **The dump is the cell authority; the configuration is the pixel authority.**
   The dump's `grid=CxR` is the *reference frame*; `k` (px per column) and `j`
   (px per row) come from the terminal that owns the pty. Neither file invents
   the other's half.
2. **One mark, one truth.** A mark drawn with `--mark "rect:200x40+14+13:label:9#10-9#30"`
   is *defined* at cells (9,10)→(9,30) → `x=14px` becomes `14/11` … (see §3.2);
   the PNG is then a *rendering* of the anchor, not a second opinion about it.

**How "they agree" is judged — pixel level, never vision.** The test is a
numeric comparison against the PNG's own pixels, with **no OCR and no vision
model in the loop**:

1. Render the same screen onto a fresh synthetic canvas with
   `text2img.RenderGridBytes` **using the dump's `Grid` rows and its
   `GridOptions`** — `CellW=k`, `Lh=j`, same `--size`, same font chain.
2. Draw the mark at its cell rectangle on that ground truth, exactly as the
   annotator does.
3. Compare the annotator's PNG against that ground truth **cell by cell**:
   for the mark's rectangle, the fraction of pixels whose ink matches; for the
   whole image, the IoU of the two ink masks.
4. Assert IoU ≥ `annotateIoUFloor` (a named constant, proposed **0.95**: the
   renderers are the same code with the same arguments, so anything below it is
   drift, not anti-aliasing).

This is the repo's own "渲染真值" rule applied literally: the render is judged
by measuring pixels, and the layer that could disagree (the cell grid) is
pinned by construction because `RenderGrid` takes the dump's own rows.

**Why the test is decisive and not circular.** A wrong conversion is caught
automatically, because the ground truth is rendered at the *dump's* geometry
while the annotator's PNG is composed against the *same* numbers — a 1-cell
error moves the mark by `k` px, far outside a 0.95 IoU. And since `k` is
rendered identically on both sides, the only free variable is the conversion.
A first pass may CALIBRATE rather than check: annotate a cell you can read,
measure the offset, and record it. **If the measured offset is not an exact
multiple of the logical cell size, the design's `logical → physical` premise is
wrong and the anchor must become a calibrated affine per host** — that is the
one finding that would send this design back, so it is measured before the
implementation is called done.

**Known blind spot, stated rather than hidden.** A pixel-level mark on a *live
desktop* capture (a window with real desktop chrome, X11) has no cell grid at
all — `gopi capture` is pixels and no grid, `gopi ui --dump-screen` is a grid
and no pixels. So `--mark …:…:…:fixed` is the shape for desktop stills, and
cell anchors require the dump's surface to be the subject. This is a property
of the two observation channels, not a defect of the design; the deck's
captions must say which of the two each still is.

---

## 5. Who consumes its output

- **Link `[5/9]` of this chain** — the annotation handoff spec and the specimen
  stills: it builds the verb, runs it on `gopi capture` output, and ships the
  annotated PNGs as the demo's stills.
- **The external edition's stills bundle** (`docs/archive/demo/external/stills/`),
  which today ships four *unannotated* PNGs whose captions have to describe in
  prose what the reader is meant to look at. A caption that says "the status
  row, third line" becomes an arrow.
- **`gopi capture`'s own computer-use loop**: `capture → act → capture` is a
  change detector (`sha256_16`); the annotator is what turns a delta into a
  *finding* a human or a vision model can read in one glance.
- **A defect report**: the operator's §1.6 argument — pointing at a bug is
  cheaper and more exact than a paragraph about it.
- **Not** the agent's tool surface: Rule 3 fixes the top-level toolset at 17,
  and this is a CLI verb (`gopi annotate`, reachable through `bash`) — the
  same relationship `gopi text2img` and `gopi capture` already have to their
  same-named tools.

---

## 6. Rule 35 — the four answers

**1. What breaks without it?** A demo claim degrades into prose, and the
demonstration of the *defect* class has no instrument. Concretely (measured,
not hypothetical): `docs/archive/demo/external/stills/` ships
`03-text2img-cjk.png` — a still produced by a one-line verb
(`gopi text2img /home/user/.gopi/sessions/capdemo-2/still-03-sample.txt
docs/archive/demo/external/stills/03-text2img-cjk.png`, read from its captions file) —
and its caption must *tell* the reader what the picture shows. There is no
observed failure today only because the demo has not yet had to point at a
*defect*; the first still that does ("this cell is blank, and it should not
be") has no way to say so except in words. The operator's §1.6 is the
requirement this answers.

**2. Why can it not be fixed in-process?** Because the failure IS the absence
of a code path, not a misbehaviour of one. There is no existing `gopi` process
to fix: §1.2 measured that no production code in this repo has ever *decoded*
an image, so there is nothing to make behave differently. The in-process
alternative — have `gopi capture` annotate as it captures — is **refused on
purpose**: a capture must stay a faithful, unmodified record of the screen, or
the annotation contaminates the evidence it points at, and shot-vs-annotated
can no longer be compared. Two verbs, so the artifact stays the evidence.

**3. Who owns its lifecycle — and how does it get cleaned up?** The verb owns
nothing persistent. It creates exactly one thing: the `--out` PNG the caller
names, which is a **user deliverable by construction** (the caller chose the
path), not an artifact the tool leaves behind. It writes **no** state file, no
cache, no lock, no directory, no config key, no env var, no daemon, no cron
entry, no scratch tree of its own (`--out` is required precisely so the tool
never invents a path). When the reading dump is resolved by discovery, the
verb **reads** and never writes it — `gopi ui --dump-screen`'s own sink owns
that file, and its reclaimer is unchanged. There is consequently nothing to
reclaim and no lifecycle to own. **Measured against the artifact question**:
`git status --porcelain` after a run shows only the caller's PNG — no new
tracked file, no new directory.

**4. What does the next person who finds it running and does not know why
see?** `gopi -h` lists it under *Tools (the agent's own tools, from the
shell)* with a one-line description, beside `gopi capture`/`gopi text2img`;
`gopi annotate -h` carries the whole contract (the `--mark` grammar, the
pixel/cell unit rule, the refusals, the JSON result); it exits when its one
image is written — nothing of it is resident, so there is no "running" state to
find. The single risk this rule cares about (an artifact nobody can identify)
does not arise: the verb leaves no artifact of its own.

### 6.1 The checklist, stated as the rule requires

- **One verb, no new artifact** — ✓, as specified in §3.1/§6.3. The Rule 35
  verdict here is exactly the planner's expectation, so nothing has to be
  argued back up the chain.
- **No new persistent dependency, no new config key, no new env var** — ✓.
  The only new *surface* is one advertised help form, and §6.2 says what that
  costs.
- **Refuse by default** — ✓: `--out` required and `≠ input`; the dump anchor
  refused rather than guessed at a fallback geometry (§3.5).
- **A test that fails if the silent form comes back** — ✓: §6.2's two gates.

### 6.2 The collateral a new advertised verb costs (measured, so it is budgeted)

Adding `gopi annotate` is **not** free in bookkeeping, and each item is named
with the file that carries it:

1. `internal/cliverbs/cliverbs.go` — a new `Annotate = "annotate"` const
   (the package's stated Rule-14 purpose).
2. `internal/config/help.go:~605` — one row in the *Tools* group beside
   `{"gopi capture", "Capture the LIVE X11 desktop to a PNG (--screen/--window/--area)."}`.
3. **The top-level help is at 159 lines against a 160-line budget**
   (measured: `gopi -h | wc -l -> 159`; `topLevelUsageMaxLines = 160`,
   `cmd/agent/usage_test.go:144`). One more row may spill it — the implementer
   must fold the row into `capture`'s line or trim elsewhere, and the budget
   may NOT be raised (weakening the gate is not the fix).
4. **The G6 inventory gate**: a newly derived advertised form must be *typed*
   into `g6Cases` or *declared blocked* with a reason
   (`cmd/agent/cli_tools_maintenance_help_surface_basic_test.go:834` asserts
   `len(g6Cases) >= 85 && len(g6BlockedForms) >= 28`), and the inventory doc
   must be regenerated — never hand-edited.
5. `internal/config/cli_verb_guard.go`'s `PositionalVerbs` — worth an explicit
   decision: the guard exists so a *file name* that is also a verb word
   (`task.txt`) cannot be dispatched as a verb. `annotate` is not a plausible
   file stem, so leaving it out is defensible; if it is added, the reason goes
   in the same commit.
6. `cmd/agent/cli_completion.go`'s verb list, and the `cli_contracts_test.go`
   fingerprint + `usage_test.go` coverage entries.

Each of these is a *test-visible* surface, which is the point: the cost of a
new verb is paid in gates, not in dependencies.

### 6.3 The scored ledger (so the claim is falsifiable)

| Proposal | Persists past the process? | Verdict |
|---|---|---|
| `gopi annotate <in> --out <out> --mark …` | no — one user-named PNG | **allowed** |
| New package `internal/annotate` | no, but a second home for the font chain + rasteriser | **refused** (the export in §3.4 instead) |
| An `annotate` *tool* in the agent's toolset | n/a | **refused** (Rule 3: the toolset is closed; this is a verb) |
| Annotating inside `gopi capture` | n/a | **refused** (contaminates the evidence, §6 answer 2) |
| A GIF/RTP/MP4 walkthrough renderer | yes — a new encoder path | **refused** (Decision 3; measured in §2) |
| A wrapper script beside `gopi` | **yes — shell script** | **refused** (Rule 35's anti-pattern; the capability belongs in Go) |

---

## 7. Test plan (what link `[5/9]` must land)

Unit (`internal/text2img/annotate_test.go`, `cmd/agent/cli_annotate_test.go`):

1. **The export draws, and measures.** `DrawText` onto an existing
   `image.NewRGBA` returns `> 0` inked pixels and leaves pixels OUTSIDE the
   text box untouched (the "does not create a canvas" contract).
2. **Ink colour is honoured.** A red `Ink` does not produce grey — the direct
   regression test for `blend`'s grey-derivation (§1.4).
3. **Geometry parsing**, table-driven: the `WxH+X+Y` letters, each malformed
   shape refused with the spec verbatim in the error.
4. **The refusals** of §3.5, one test each: `--out` missing, `--out == --in`,
   non-PNG input, mark outside the image (clipped + `clipped:true`), zero ink
   changed → no file written.
5. **Cell anchor, no dump** → exit 1 naming what is missing; **fallback
   geometry** → exit 1 (the `GeometryFallbackNotice` case).
6. **The anchor reconciliation, pixel-level (the headline test).** Render the
   dump's own `Grid` rows with `text2img.RenderGridBytes` at `CellW=k,
   Lh=j`; draw the mark at its cell rectangle on that ground truth; composite
   the same mark onto the real capture; assert **IoU ≥ `annotateIoUFloor`**
   between the two ink masks. No OCR, no vision model. Runs against a committed
   fixture dump (a small copy of a real `chat-screen.dump`, ~5.7KB — the host
   has 19 of them), so the test needs no live terminal.
7. **Determinism**: the same arguments produce byte-identical PNGs (the fixed
   composite order of §3.1).

E2E (`cmd/agent/cli_annotate_e2e_test.go`, in the repo's E2E idiom — a real
binary run on a real PNG):

8. `gopi capture --window --out a.png` then `gopi annotate a.png --out b.png
   --mark "rect:200x40+14+13:status row:9#10-9#30"` → exit 0; `b.png` differs
   from `a.png` in `sha256`; the JSON's `ink_pixels > 0`; pixels OUTSIDE the
   mark's rectangle are byte-identical between `a.png` and `b.png`.
9. The same with `--mark …:fixed`, and with four marks on one still.

Gate (the anti-regression half, per §6.2):

10. Extend the G6 help-surface case list with the new form; regenerate the
    inventory; assert `gopi -h | wc -l <= topLevelUsageMaxLines`.

---

## 8. Not done (honest)

- **No implementation.** No annotator code was written — this link's
  deliverable is the measurement and the design (§ Deliberately, per the
  briefing "WRITE (not implement)").
- **The anchor premise is NOT measured end to end.** §4's conversion
  `x_px = k·logical_col` is derived from the surfaces as they are (the dump
  carries no cell→pixel mapping; `XTerm`'s cell size is textual), and the
  ground-truth test in §7.6 is the measurement that falsifies it if it is
  wrong. Link `[5/9]` must run that first. This is stated so nobody reads
  §4 as a measurement it is not.
- **No specimen stills.** Decision 3 picks the deck; producing it is link
  `[5/9]`'s deliverable, not this link's.
- **`--mark` grammar is unversioned.** If a second mark kind arrives
  (`highlight`, `blur`, `numbered bubble`), the field count changes and old
  command lines break. The alternative considered — a JSON `--marks @file.json` —
  was refused as a second artifact and a worse failure mode, but the trade is
  recorded here so the decision is visible rather than accidental.

---

## Evidence index

All under `/home/user/.gopi/sessions/capdemo-4/evidence/`:

| File | What it holds |
|---|---|
| `A-B-C.txt` | `image/draw` = 0 over the whole tree; the annotate/arrow/overlay symbol scan and its (unrelated) hits |
| `N-O.txt`, `P.txt` | `draw.Draw`/`draw.Over` = 0; the `composite`/`blit` hits, shown to be non-image senses |
| `R.txt` | the decisive negative: zero production `png.Decode`/`image.Decode` |
| `K.txt`, `M.txt`, `AA-AB.txt` | `internal/text2img`'s public API and its unexported painter; `blend`'s grey derivation; both call sites of `drawChainGlyph` |
| `E-F-G.txt` | `gopi -h`, `gopi capture -h`, `gopi text2img -h` verbatim |
| `BA-BB.txt`, `BA2-BE.txt`, `BG.txt` | the absent verb, the open-dispatcher probe, `PositionalVerbs`, the G6 hooks |
| `BC-BD.txt` | the verb list and the 159/160 help budget |
| `Q.txt` | `gopi capture --window` and `--area`, driven, with `file(1)` on both PNGs |
| `U.txt` | `gopi text2img` driven (ink=1969 px), and the `--out` flag gap re-checked |
| `V.txt`, `Y-Z.txt` | dump discovery, the real record line (`grid=117x26 geometry=layout`), the record's fields |
| `J.txt`, `H-I.txt` | the dump's JSON twin fields and the `--dump-screen` emitters |
| `W-X.txt` | the moving-image negative (`gif.Encode`=0, `mp4`/`ffmpeg`=0) |
| `BH-BJ.txt` | the change-detector measurement (`sha256_16` on two captures) and the PNG digests |
| `AK-AL.txt`, `AF-AJ.txt` | the external edition's stills/CAPTIONS provenance, help.go's rows, the dispatcher |

PNG digests (`sha256sum`, read back from the files):

```
d169ba27f536aacda9f466d321727c890ad08b9c85975b96f5e4a382f56dde54  probe-t2i.png
7e8207d73fcf50c84d5788fbf2099007dfc9cce1343050d111dff0a4965dac13  base-window.png
1b9b0fa6212cc7ee3ba347cae71e69e9fbdffdd83f9a5dbac0726cf1ac032c33  base-area.png
```

None of these files is in the tree: the worktree stays clean (Rule 30), and
the stills are link `[5/9]`'s deliverable.
