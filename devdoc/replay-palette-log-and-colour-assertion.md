<!-- doc-class: historical -->
# Task Report: the assembly log said "colourful" about a colourless film — and the honest number to assert

Task `01a0fa54-33e5-709d-b6e5-f3b5979cea29` (epic reframed DOABLE). The epic's raw
briefing is in this session's log; the reframed goal is the acceptance anchor.

**READ THIS WITH ITS PREDECESSOR.** The colour LOSS itself — "颜色根本没流到画面里" —
was found and fixed by task `01a0fa3d` on the same branch
(`devdoc/replay-colour-lost-to-redirected-stderr.md`, commit `8e616fe9`, in this
history). The operator's briefing reached me with that fix already in the tree, and
this run's job is the half that fix did not touch: the assembly log that reads as the
OPPOSITE of the artifact, the colour assertion's own discriminating power, and the
honest measurement of both. Section 1 restates the located break so this document
stands alone; sections 2–5 are this run's work.

## 1. Where the colour was dropped — file:line, and the break

Three lines, in order. Each is quoted from the tree at this commit.

1. `scripts/replay/replay.sh:271` launches the replay with **stderr redirected to a
   regular file**:
   `--dump '$SCRATCH/chat-screen.dump' 2>'$SCRATCH/replay-stderr.log'`
2. `cmd/gopi-replay/main.go` asked the colour capability about **`os.Stderr`** — the
   descriptor the launcher had just pointed at that file. (Pre-fix spelling;
   `git show 8e616fe9^:cmd/gopi-replay/main.go`.)
3. `internal/lineedit/colour.go:94` is `ColourWanted`'s fourth rule —
   `if sink == nil || !IsTerminal(sink) { return ColourOff }`. The sink is not a
   terminal, the decision is `ColourOff`, and `PaletteFor(ColourOff)` returns
   `MonoPalette`, whose every field is the zero `Style`: **no SGR byte at all**.

So the film drew gopi's real surface — the production editor, the production layout —
with **no colour anywhere**, which is why the delivered GIF was monochrome while its
background still differed from a plain terminal's. The break is not in the renderer
and not in the palette tables; it is in *which descriptor the run asked about*.

The fix (`8e616fe9`, in this history) asks the descriptor the run actually draws to —
the first of stdout/stderr/stdin that IS a terminal — and refuses by name otherwise:

- `cmd/gopi-replay/main.go:677` `func colourSinkFor(candidates ...*os.File) *os.File`
- `cmd/gopi-replay/main.go:691` `out := colourSinkFor(os.Stdout, os.Stderr, os.Stdin)`
- `cmd/gopi-replay/main.go:697` the loud refusal,
  `fmt.Errorf("%s (no terminal on stdout, stderr or stdin)", statusNoTerminalFmt)`
- `cmd/gopi-replay/main.go:718` `pal := lineedit.PaletteForEnvCapability(false, out)`

No flag, no knob, default on: the depth decision still lives only in
`internal/lineedit/colour.go`, and only *the question's subject* changed.

## 2. The assembly log read as the opposite of the artifact (this run)

`tools/replay/assemble.py:266` prints the last line of `gopi gif`'s stdout, and
`internal/gifgen/gifgen.go`'s `Result.Summary()` produced exactly one palette number:

```
调色板=%d 色          →  r.PaletteColors, assigned as  len(q.colors)
```

`q.colors` is the palette's **slot count** — the size of the colour TABLE, capped at
`PaletteSize` = 256 (`quantizer.buildReduced` walks `for i := 0; i < PaletteSize`). It
is not a measurement of the film's colour, and it saturates. So the operator's
colourless film reported **253** while carrying no colour of gopi's at all — five
non-grey entries, every one of them the tinted background. A reader who had only that
line had no way to see the defect the artifact itself showed.

**The fix — name the table's size AND what it holds** (`internal/gifgen/gifgen.go`):

```
调色板槽位=%d 实际=%d 色 非灰阶=%d 饱和=%d 色 墨迹=%d 像素
```

| field | constant/assignment | what it is |
|---|---|---|
| `调色板槽位` = `PaletteColors` | `gifgen.go:470` `len(q.colors)` | the table's SIZE (≤ 256), unchanged semantics |
| `实际` = `PaletteUsed` | `gifgen.go:473` `paletteUsed(pm)` | entries the encoded frames actually REFERENCE — scanned, not inferred |
| `非灰阶` = `PaletteNonGrey` | `gifgen.go:471` `paletteNonGrey(pal)` | entries with `r!=g!=b` — the table's colour content |
| `饱和` = `PaletteSaturated` | `gifgen.go:472` `paletteSaturated(pal)` | entries that are saturated AND bright — **the number that cannot be misread** |

⚠️ **`非灰阶` alone would still read as colourful, and that is the trap this run
found.** Measured, the colourless film's line now reads `非灰阶=202 色` — a big,
chromatic-looking number for a film with no colour of gopi's in it. Every one of
those 202 entries is an **antialiasing blend of white glyph toward the blue-tinted
background (19,25,38)**; their chroma (`max−min`) tops out at **19/255**. So the
summary also carries `饱和`, whose floors are the *same pair* `tools/replay/verify.py`
enforces (`paletteSatMin = 0.40`, `paletteValMin = 0.50`), and the same command
separates the two artifacts with no ambiguity:

| film | 调色板槽位 | 非灰阶 | **饱和** |
|---|---|---|---|
| the operator's colourless film | 208 | 202 | **0** |
| the film this same code produces after the fix | 31 | 31 | **9** |

(The `paletteSaturated` ratio is taken against `paletteFullScale = 65535`, not 255:
the palette is read through `color.Color.RGBA()`, which scales to 16 bits. A bare
`255` there is a different — wrong — fraction by a factor of 257. Named for Rule 1.)

## 3. The assertion: which threshold can actually tell truth from appearance

The epic asked for a machine-decidable "非灰阶色种类数" assertion. **As literally
stated that threshold is a trap**, and this is measured, not argued:

| measurement, on the operator's colourless film | value |
|---|---|
| distinct RGB kinds in its delivered frames | **208** |
| distinct RGB kinds with `r!=g!=b` ("非灰阶色种类数") | **202** |
| **maximum chroma among those 202** | **19 / 255** |
| kinds within tolerance 16 of ANY `PALETTE_RGB` anchor | **0** |

A threshold of "非灰阶色种类数 ≥ 10" — or ≥ 100 — **PASSES a film that carries no
gopi colour at all**, because antialiasing keeps a hue. That is the briefing's
background trap one level up: the blended edge of white text inherits the backdrop's
blue cast, so counting kinds counts the wallpaper.

`tools/replay/verify.py`'s claim 5 already solved this correctly and is UNCHANGED in
its discriminating power: `film_hues()` keeps only pixels with `s >= COLOUR_MIN_SAT
(0.40)` and `v >= COLOUR_MIN_VALUE (0.50)` — which excludes the background (v≈0.15)
*and* the blends (chroma 19 ⇒ s≈0.5 but v depends on the glyph; the saturated-and-
bright set is empty) — then requires `MIN_HUE_BUCKETS = 1` and, since the previous
run, `MIN_PALETTE_ROLES = 1` anchored on `PALETTE_RGB`.

**What this run added to the assertion** (`tools/replay/verify.py`):

1. `HUE_BUCKETS`/`MIN_HUE_BUCKETS`/`MIN_PALETTE_ROLES` now carry the SEPARATION as
   their reason — 0 roles before vs 2 roles after, measured — so the floor cannot be
   relaxed to a number both sides clear without contradicting the comment.
2. `film_hues()` now also returns the count of distinct non-grey RGB kinds, and
   claim 5 prints it: the epic's requested "非灰阶色种类数" is REPORTED, alongside the
   number that decides, so a reader can see the trap's own value and why it is
   excluded.

**The red/green proof is a REGRESSION IN THE REPOSITORY, not a transcript.**
`tools/replay/test_verify.py` case 5 (`case_colour`) builds two films **inside the
test** — a grey one (white antialiased toward this terminal's own background
`(19,25,38)`, i.e. the defect's painting, blends and all) and a coloured one
(`V.PALETTE_RGB[2]` filled generously enough to clear the pixel floor as a real film
does) — and asserts that the SAME `check_colour` the delivered checker runs gives
`fail=1` on the first and `fail=0` on the second. Its third assertion pins the trap by
measurement: the colourless film clears a floor of ten non-grey kinds (`113` measured
in the test's own film, `202` on the operator's), so a kinds-based threshold is
**executably shown** to be the wrong one. `V.BG_RGB` was added to the checker as a
named constant (Rule 1) because both the value floor and the test's synthetic film are
calibrated against that measured value.

```
$ python3 tools/replay/test_verify.py
5. the colour claim's discriminating power (non-grey kind count is a TRAP)
  ok   the colour claim REJECTS a film drawn without the palette
  ok   the colour claim ACCEPTS a film carrying a declared role
  ok   the epic's kind-count criterion would have PASSED the colourless film (measured, 113 non-grey kind(s) > 10) — hence the anchored-role floor
all pass        (exit 0 — cases 1..4 unchanged and green)
```

**The separation, red and green, is measured below in `## Verification`.**

Both proofs below are taken on the FINAL `tools/replay/verify.py`, with the same
`check_colour` the delivered checker runs, against two artifacts the SAME assembly
command produced from two sets of frames:

- **RED** — a film assembled from the operator's own colourless frames:
  ```
  [verify]    the film's 7 frame(s) carry 0 distinct hue bucket(s) of saturated, bright ink (0 such pixels; 0 distinct saturated bright RGB kind(s); floors: saturation >= 0.40, value >= 0.50)
  [verify] FAIL: the film has 0 saturated bright pixel(s) in 0 hue bucket(s) (floors 1000 / 1): the surface's palette did not reach the artifact — this is a grey-scale film of a colour product
  fail counter after check_colour on the colourless film: 1
  ```
- **GREEN** — a film assembled from the fixed film's frames:
  ```
  [verify]    the film's 12 frame(s) carry 1 distinct hue bucket(s) of saturated, bright ink (1571788 such pixels; 9 distinct saturated bright RGB kind(s); floors: saturation >= 0.40, value >= 0.50)
  [verify]    palette roles present: answer emphasis, colour-178 cubeDeepGold (#D7AF00), editing line, colour-220 (#FFD700)
  [verify]    OK: gopi's own palette is on the artifact, not only in the renderer
  fail counter after check_colour on the coloured film: 0
  ```

The discriminating quantity moves **0 → 9** (saturated bright kinds) and **0 → 2**
(anchored palette roles) between the two artifacts, so the claim's floor sits inside a
measured gap rather than at a value both sides can clear.

**Both proofs re-taken on the COMMITTED tree (`9ea34186`), on the delivered artifacts
rather than on freshly assembled ones** — the script is
`…/01a0fa54-…/colour-red-green.py`, which `import verify` and calls the repository's own
`check_colour`, so no floor is restated and no second copy of the rule exists:

```
$ python3 …/colour-red-green.py     # RED first, GREEN second
### RED — the film delivered at commit 42745614 (the colourless arm)
[verify]    the film's 121 frame(s) carry 0 distinct hue bucket(s) of saturated, bright ink (0 such pixels; 0 distinct saturated bright RGB kind(s); floors: saturation >= 0.40, value >= 0.50)
[verify] FAIL: the film has 0 saturated bright pixel(s) in 0 hue bucket(s) (floors 1000 / 1): … this is a grey-scale film of a colour product …
fail counter after check_colour: 1

### GREEN — the film produced through the fixed default path
[verify]    the film's 202 frame(s) carry 1 distinct hue bucket(s) of saturated, bright ink (27121676 such pixels; 9 distinct saturated bright RGB kind(s); floors: saturation >= 0.40, value >= 0.50)
[verify]    palette roles present: answer emphasis, colour-178 cubeDeepGold (#D7AF00), editing line, colour-220 (#FFD700)
[verify]    OK: gopi's own palette is on the artifact, not only in the renderer
fail counter after check_colour: 0
```

**Two green proofs above, and they are DIFFERENT FILMS — read them separately.** The
SHORT one (12 frames, 1,571,788 saturated-bright px) is the delivered arm's sampled
frame directory, the one `--allow-sampled-frames` is about; the re-taken one (202 frames,
27,121,676 px) is the full film from the fixed path. The pixel count scales with the
frame count (×12.96 against ×16.83 frames) as it must for a per-frame-measured quantity,
and both clear every floor — which is the point: the claim does not depend on how many
frames the caller happened to hand it.

## 4. The real colour inventory (deliverable 5)

Full-frame scan of the **delivered GIF** (PIL, every frame for the anchor table,
sampled for the inventory), tolerance 16 against `PALETTE_RGB`:

| measurement, on the DELIVERED GIF | before (operator's film, `42745614`) | after (fix `8e616fe9`) |
|---|---|---|
| distinct RGB kinds | 15 | 31 |
| non-grey kinds (`r!=g!=b`) | 12 — **all** white→blue-background blends | 31 |
| max chroma | 19/255 | 254/255 |
| kinds with chroma ≥ 32 | **0** | 14 |
| kinds that are saturated AND bright | **0** | **9** |
| `PALETTE_RGB` anchors hit | **0 of 4** | **2 of 4** |

The same measurement on the RENDERER's own PNG frames (before GIF quantisation)
separates the two even further — 208 distinct kinds before (max chroma 19/255) against
917 after — which is where the "非灰阶色种类数 = 202" trap figure comes from: on the
PNG side alone, a kinds-based threshold is most misleading of all.

The anchors that appear after the fix are gopi's own (counted on the delivered GIF):

- `(218,165,32)` — 1,214,766 px on the full-film scan — the 256-tinted goldenrod
- `(255,248,220)` — 524,252 px — cornsilk, the 24-bit answer body (`38;2;255,248,220`)
- `(255,215,0)` — 39,290 px — `colour-220`, the editing line (an exact `PALETTE_RGB`
  entry, hit on the sampled GIF scan too)
- `(215,175,0)` — 222 px — `colour-178` `cubeDeepGold`, an exact anchor, on the GIF scan

**Honest note on the tier:** this terminal declares `COLORTERM=truecolor`, so the
default resolves the 24-bit table (`#FFF8DC` cornsilk), not `Palette256`'s
`colour-230` `#FFFFD7`. Both are gopi's real palette and the gold family is the same;
forcing the 256 table would be the knob the epic forbids. `colour-220` (a 256-table
role) *is* present on the film.

## 5. Not Done (honest)

- **Terminal selection, and the half that stays limited.** The epic names
  `gnome-terminal` 48s as over budget. That is real and unchanged:
  `tools/termemu/termemu.py:104` sets `PROBE_LAUNCH_TIMEOUT_SEC_GNOME = 45`, and with
  `PROBE_SLICE_SLACK_SEC = 15` its slice is **60s** (`termemu.py:236` block budget =
  `launch_budget_sec + PROBE_SLICE_SLACK_SEC`); the 48s figure the epic quotes is the
  same shape as the fix note at `termemu.py:393`. `xfce4-terminal` is what wins —
  `launch_budget_sec = 12`, no window manager, no session bus, and it is a VTE
  terminal, so it sets `TERM=xterm-256color` for its child. **What is therefore still
  limited:** the delivered film is photographed in an `xfce4-terminal` under bare
  `Xvfb`, not in the operator's own GNOME terminal. The two share the VTE core, so the
  *palette decision* is the same by construction — but **font rendering, the exact
  `px_per_char` and the chrome the terminal itself draws are xfce4's, not GNOME's.**
  I did not attempt a live GNOME-hosted arm: it needs a session bus
  (`needs_bus: True`) and a window manager (`needs_wm: True`) inside the private
  display, and a probe that eats 60s cannot be run inside this session's budget
  without crowding out the run itself. **Who can run it:** an operator with a real
  GNOME session (or a spare 60s slice) can execute
  `bash scripts/replay/replay.sh --bin bin --session 4ca8cd29-… --emu gnome-terminal`.
- **I did not re-shoot the full 3-minute film in this run.** The green artifact is the
  one the previous run shot with the same launcher after the fix
  (`/home/user/workspace/_tmp/replay-1790903767/out-after/arm-after/`, 202 frames); I
  verified it, measured it, and re-assembled from its frames. The end-to-end shoot
  costs ~3 minutes of terminal + Xvfb time and buys nothing this run changed — the
  surface I changed is `gopi gif` and `verify.py`, and both are exercised directly
  (Rule 31) below. **Who can re-shoot:** any operator, with the command above.
- **The GIF is not re-delivered to `docs/archive/demo/replay/`.** The committed arm
  (`42745614`) is left exactly as the operator produced it — it is the RED evidence
  and must not be overwritten. Delivery of a new film is a separate decision.
- **Pre-existing traceability FAILs** (`beat 170`/`beat 158`, named in the
  predecessor's report) are untouched: they are text-provenance checks over the
  ledger, independent of the colour claim, and this run changed neither.
  **Consequence a reader must not misread: the whole `verify.py` still ends
  `RESULT: FAILED`, EXIT=1** on the fixed arm — because of those two, not because of
  colour. In the same run the colour claim printed
  `1 distinct hue bucket(s) … palette roles present: … colour-178 cubeDeepGold …, colour-220`
  and `OK: gopi's own palette is on the artifact`. Legibility (`19.017 px per character`,
  floor 16.0), motion (202 frames, 11 differing pairs) and withholding (185 masked runs
  replaced) were green too. The exit status is the file's, not the claim's — the claim's
  own contract is the `fail counter` taken in §3 and in
  `…/01a0fa54-…/red-green-committed.txt`. Full run: `…/green-full-defaultpath.txt`.
- **A pre-existing FAIL on master, found while smoke-running the merged branch (NOT
  mine, NOT fixed here).** `go test ./cmd/agent/ -run TestPtyMarkerColumnDump` fails —
  "body row 39 opens at cell 0, want the block's column 2". Attribution, three
  measured facts: (a) my diff (now `git diff --stat master...HEAD`) touches only
  `cmd/agent/cli_gif.go`, `internal/gifgen/*`, `tools/replay/verify.py`,
  `tools/replay/test_verify.py` and two devdoc files — none of
  the marker path; (b) the test file is **byte-identical** between my base and master
  (`git rev-parse 8b94e7da:cmd/agent/pty_marker_column_dump_test.go` =
  `8374e6af:…` = `15db0d5b…`); (c) my branch's `internal/lineedit` is identical to
  master's (`git diff --stat 8374e6af HEAD -- internal/lineedit` empty). Master's own
  `7e92b038` ("a code block's rows carry no turn gutter") changed
  `internal/lineedit/answer.go` and added `marker_column_narrow_test.go`, but did not
  update `cmd/agent/pty_marker_column_dump_test.go` — so that test contradicts the
  merged fix on master itself. **My own surface's tests pass on the merged branch**
  (`go test ./cmd/agent/ -run Gif` → ok; `go test ./internal/gifgen/` → ok). Filed as
  a separate bug task rather than fixed here: it is another owner's surface.
- **A second pre-existing failure the branch-scoped smoke surfaced, and the shape of it
  matters** (`TestPrefetchGoroutineSettleGapDetectsALiveLeak`): it fails inside the full
  `./cmd/agent` package run and **passes when run alone** (`-run
  TestPrefetchGoroutineSettleGapDetectsALiveLeak` → `ok cmd/agent 0.692s`, and the test
  itself is byte-identical to master at `3c740f3b…`). So the honest report is
  "load/order-sensitive pre-existing flake", NOT "green": I record it as a FAIL the smoke
  saw and I do not claim the package is green. The other `go`-level gates of this run's own
  surface are green (quoted in `## Verification`). Whether that flake deserves its own bug
  task is a judgement I did not make here — it is one test, it flaked under the smoke's own
  concurrent load (the run reported `load 7.15 10.76 10.73` on a 4-core box), and I neither
  re-ran it in a loop to characterise it nor touched its surface. Naming it beats hiding it.

## Verification

- Build: `CGO_ENABLED=0 go build ./internal/gifgen/ ./cmd/agent/ ./cmd/gopi-replay/`
  → OK (no output). Close-out re-check: `CGO_ENABLED=0 go build ./...` → exit 0; and the
  declared build `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD OK 9ea34186`.
- Tests: `CGO_ENABLED=0 go test ./internal/gifgen/ ./cmd/agent/ -run 'Gif|Summary|Palette'`
  → `ok github.com/nick/gopi/internal/gifgen 5.962s`, `ok github.com/nick/gopi/cmd/agent 7.575s`.
  Plus the new regression guard, verbatim:
  `go test ./internal/gifgen/ -run TestGifSummaryReportsPaletteContentNotItsSize -v`
  → `--- PASS: TestGifSummaryReportsPaletteContentNotItsSize (0.14s)`.
  And the checker's own suite, now including the colour case:
  `python3 tools/replay/test_verify.py` → cases 1–4 unchanged + case 5 (`case_colour`)
  → `all pass`, exit 0.
- Tests (branch-scoped smoke at close-out, through the repository's own entry point):
  `make test-budget PKGS='./internal/gifgen ./cmd/agent'` → `packages: 2 finished, 1 with a
  failure`; `./internal/gifgen` green; `./cmd/agent` red on **two** tests, neither on this
  run's surface, each attributed by measurement:
  (i) `TestPtyMarkerColumnDump` — FAILS in ISOLATION too
  (`go test -count=1 ./cmd/agent/ -run TestPtyMarkerColumnDump` → `FAIL`), file
  byte-identical to master (`15db0d5b…` on both), and master's own `7e92b038` changed
  `internal/lineedit/answer.go` without updating it: pre-existing.
  (ii) `TestPrefetchGoroutineSettleGapDetectsALiveLeak` — FAILS under the full package
  run, **PASSES in isolation** (`go test -count=1 ./cmd/agent/ -run
  TestPrefetchGoroutineSettleGapDetectsALiveLeak` → `ok cmd/agent 0.692s`); file
  byte-identical to master (`3c740f3b…` on both): a load/order-sensitive pre-existing
  flake, not a code change. The smoke's own NOTE about `bin/gopi` being absent is quoted
  and was answered: I rebuilt `bin/gopi` before the isolated re-runs below.
- Tests (this run's own surface, re-run after that rebuild, `GOPI_BIN` set):
  `GOPI_BIN=$PWD/bin/gopi go test -count=1 ./internal/gifgen/ -run 'Gif|Palette|Summary'`
  → `ok internal/gifgen 2.857s`; `GOPI_BIN=$PWD/bin/gopi go test -count=1 ./cmd/agent/ -run 'Gif'`
  → `ok cmd/agent 2.125s`.

- Ran: `bin/gopi gif --frames <before 7 frames>,… --out …/log-before.gif --delay 45`
  → `✅ 7 帧 → … 2225x1432 … 调色板槽位=208 实际=208 色 非灰阶=202 饱和=0 色 墨迹=0 像素 字节=1020967`
- Ran: `bin/gopi gif --frames <after 12 frames>,… --out …/log-after.gif --delay 45`
  → `✅ 12 帧 → … 2225x1432 … 调色板槽位=31 实际=31 色 非灰阶=31 饱和=9 色 墨迹=0 像素 字节=1135387`
- Ran: `python3 tools/replay/verify.py --out docs/archive/demo/replay/arm-sess-4ca8cd29-20261002
  --scratch … --session … --allow-sampled-frames` → **RED**, verbatim:
  `the film's 121 frame(s) carry 0 distinct hue bucket(s) of saturated, bright ink (0 such pixels…)`
  → `FAIL: the film has 0 saturated bright pixel(s) in 0 hue bucket(s) (floors 1000 / 1)`
  → `RESULT: FAILED`, exit 1. (That run also reports two FAILs for the ledger/redaction
  files under the scratch I passed — I assembled the check standalone, so the
  provenance claims had no ledger; they are not the colour claim and not this run's
  subject. The colour claim's own FAIL is the one quoted.)
- Ran: `check_colour` (the delivered checker's own function, imported and called
  directly) on the film assembled from the operator's colourless frames, and on the
  film assembled from the fixed film's frames → `fail counter … 1` / `fail counter … 0`,
  the RED and GREEN runs quoted in §3. Both on the FINAL `verify.py`.
- Ran (close-out, on the COMMITTED tree `9ea34186`, artifacts not reconstructions):
  `python3 /home/user/.gopi/sessions/01a0fa54-33e5-709d-b6e5-f3b5979cea29/colour-red-green.py`
  → RED on the DELIVERED arm's GIF (`docs/archive/demo/replay/arm-sess-4ca8cd29-20261002/chat-replay.gif`,
  121 frames, commit `42745614`): `0 distinct hue bucket(s)` / `fail counter after check_colour: 1`;
  → GREEN on the full film from the fixed default path (202 frames): `1 distinct hue bucket(s)`,
  `27121676 such pixels`, `9 distinct saturated bright RGB kind(s)`, roles
  `colour-178 cubeDeepGold (#D7AF00)` + `colour-220 (#FFD700)` / `fail counter … 0`.
  Output filed verbatim at `…/01a0fa54-…/red-green-committed.txt`.
- Ran (close-out, whole checker, exit status measured): `time python3 tools/replay/verify.py
  --out /home/user/workspace/_tmp/replay-1790903767/out-after/arm-after
  --scratch /home/user/workspace/_tmp/replay-1790903767/scr-after2
  --session /home/user/.gopi/sessions/4ca8cd29-019c-4dda-a000-6dafd782bf91 --allow-sampled-frames`
  → colour claim GREEN (`1 hue bucket`, `2 palette roles present`), legibility GREEN
  (`19.017 px per character`, floor 16.0), motion GREEN (202 frames, 11 differing pairs),
  withholding GREEN (185 masked runs replaced) — **`RESULT: FAILED`, EXIT=1**, because the
  two PRE-EXISTING traceability FAILs (beat 170, beat 158) are still there; they are text
  provenance, unchanged by this run, and are named in `## Not Done`. Full output:
  `…/01a0fa54-…/green-full-defaultpath.txt` (2m14s wall).

## Evidence (self-filed)

- The break, quoted from the tree: `scripts/replay/replay.sh:271` stderr redirect;
  `internal/lineedit/colour.go:94` `if sink == nil || !IsTerminal(sink) { return ColourOff }`.
- The log defect, quoted: `internal/gifgen/gifgen.go:470` `res.PaletteColors = len(q.colors)`
  (exposed by `tools/replay/assemble.py:266`).
- The trap, measured on the operator's colourless film: 208 distinct RGB kinds, 202
  non-grey, **max chroma 19/255**, **0** kinds with chroma ≥ 32, **0 of 4**
  `PALETTE_RGB` anchors. (Method: PIL scan of
  `docs/archive/demo/replay/arm-sess-4ca8cd29-20261002/chat-replay.gif`, all 121 frames for the
  anchor table, 7 sampled frames for the inventory. Raw output filed at
  `/home/user/.gopi/sessions/01a0fa54-33e5-709d-b6e5-f3b5979cea29/colour-inventory.txt`.)
- The two `gopi gif` invocation lines above, verbatim, and the assembled contrast they
  produce: 非灰阶=202/饱和=0 against 非灰阶=31/饱和=9.
- The RED and GREEN claim-5 runs, verbatim, filed at
  `/home/user/.gopi/sessions/01a0fa54-33e5-709d-b6e5-f3b5979cea29/red-green.txt`
  and, re-taken on the committed tree, at `…/red-green-committed.txt`.
- The regression's own run: `python3 tools/replay/test_verify.py` → `all pass`,
  exit 0 (case 5 output quoted in §3 above).
- The every-frame GIF inventory of both arms (before: 121 frames / 15 kinds / 12
  non-grey / 0 anchors; after: 202 frames / 31 kinds / 31 non-grey / 9 saturated+bright
  / 2 of 4 anchors, `(215,175,0)` 3956 px and `(255,215,0)` 665233 px), produced by
  `python3 colour-inventory-gif.py <label> <gif>` twice in one job, filed at
  `…/01a0fa54-…/colour-inventory-gif.txt`.
- The terminal budgets, quoted: `tools/termemu/termemu.py:104`
  `PROBE_LAUNCH_TIMEOUT_SEC_GNOME = 45`, `termemu.py:117` `PROBE_SLICE_SLACK_SEC = 15`,
  `termemu.py:183-184` gnome-terminal's `needs_bus`/`needs_wm` both `True`.
- `git status --porcelain` in this worktree: the four intended files, committed below.
