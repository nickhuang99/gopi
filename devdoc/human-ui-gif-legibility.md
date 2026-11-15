<!-- doc-class: historical -->
# The chat-loop film was unreadable, and the criterion that let it ship

**Task**: `01a0f0de-869e-78a8-9500-a3520faf32fa` · **Branch**: `feat/human-ui-gif-legibility`
**Supersedes the film of**: `docs/archive/demo/human/` (built by task `01a0efbd-8a83-7d83-b9be-ccd0debced09`)

## The symptom, and why it was invisible

`docs/archive/demo/human/chat-loop.gif` was 960x456. The terminal it recorded is 120
columns wide, so the film gave every character **8.00 px** — and the text in it
could not be read. The stills beside it were fine: `chat.png`, `ui.png` and
`hero.png` were all 2284x1084, i.e. **19.03 px/char**, at the capture's own
resolution.

One capture, two delivery paths:

```
capture  2284x1084                       px/char = 2284/120 = 19.033
stills   -> crop to the cell band        px/char = 19.033   (unchanged, no scale)
film     -> crop, then resize to 960     px/char =  8.000   (--gif-width 960)
```

Nothing in the pipeline was broken. `assemble.py` did exactly what it was asked
to; the ask was wrong.

## 5-why: the ask, not the encoder

1. **Why is the text unreadable?** Each glyph is 8px wide, and the material is a
   terminal at 13pt rasterised at 19.03 px/char — over half the horizontal detail
   is resampled away.
2. **Why is the film 960px wide?** `assemble.py --gif-width` defaulted to 960 and
   `build.sh` passed it through.
3. **Why 960?** The acceptance criterion for the film was "**width >= 800px**"
   (`wide_enough_800` in `manifest.json`). 960 cleared it, so nothing complained.
4. **Why is a width criterion the wrong instrument?** For a terminal screenshot,
   legibility is a per-*character* property: `width / terminal columns`. A total
   width can be met while every glyph is a smudge — an 800px-wide film of a
   200-column terminal is *worse* than a 700px film of an 80-column one, and the
   recorded flag cannot tell them apart.
5. **Why did no measurement catch it?** Every downstream check measured the
   film's *file* — size, frame count, per-frame pixel diffs, containment of the
   redaction mask. All of them passed on the 960px film, and all of them would
   pass on a film of grey noise. **No check anywhere in the chain measured the
   resolution against the geometry that produced it.**

The last point is the actual defect: the criterion was a proxy for legibility
that nothing tied back to the terminal, and both the stills and the film come out
of the same `assemble.py` — so one artefact could satisfy the criterion while its
twin, twenty lines later in the same file, did not.

## The fix

Two assertion sites, because the two numbers can disagree only if the geometry
moved:

- **`scripts/human-ui/build.sh`** — a `MIN_PX_PER_CHAR=16` constant (Rule 1), and
  `drive_into_window` computes `WIDTH / TERM_COLS` from the window the display
  actually returned and refuses the run when it is below the floor. This is the
  only place the failure is still fixable: downstream, every figure is computed
  from the picture's own width, so a crop of unreadable glyphs reports a clean
  pass.
- **`tools/human-ui/assemble.py`** — the width target flag is gone
  (`--gif-width` deleted, `--hero-width` defaults to `0` = native), replaced by
  `--gif-min-px-per-char 16` and ONE function that picks the delivered scale:

```python
def film_scale(capture_w, cols, min_px_per_char):
    native = capture_w / float(cols)
    if native + 1e-9 >= min_px_per_char:
        return 1.0, native          # deliver the capture's own pixels
    return min_px_per_char / native, min_px_per_char
```

  The manifest now records `px_per_char`, `min_px_per_char`, `legible`,
  `frame_scale` and `px_per_char_if_not_scaled`, for the film and for the stills
  it sits beside; a film below the floor exits non-zero rather than shipping.

## The measurement that changed the design: downscaling made the file BIGGER

The obvious worry was the 2.5 MB ceiling — "19.03 px/char for 41 frames will not
fit". It fits, and the intuition that a smaller frame is a smaller file is
**false on this material**. Same 41 frames, PIL, adaptive 256-colour palette,
`optimize=True`:

| delivered width | px/char | bytes |
|---|---|---|
| 2284 (native) | 19.03 | **3,546,990** |
| 2056 | 17.13 | 6,139,157 |
| 1964 | 16.37 | 5,777,246 |

Mechanism: a live terminal grab is antialiased/dithered, and LANCZOS resampling
mixes intermediate greys into the glyph edges. Those mid-tones are *new distinct
colours*, and they cost the palette encoder more bits than the discarded pixels
save. So the criterion and the ceiling pull in the **same** direction here, and
the archive keeps the evidence that the old cut was a loss on both counts.

(The delivered film came in at 1,830,260 bytes / 1.75 MiB through `gopi gif`,
i.e. `internal/gifgen` — its own quantiser beats PIL's on this material by ~1.9x.
No new dependency was added: `gopi gif` is the recorder the README already
documents.)

## Was this worth the fidelity of a 4x supersample? (checked, not assumed)

`gopi gif` is lossy, so the fix is only real if the *delivered* film still shows
what the PNG frames show. Measured on the encoded GIF against its own source
frame, ink mask at Otsu threshold, IoU plus mean absolute luminance error:

| encoder | frame 20 | frame 40 |
|---|---|---|
| `gopi gif` (delivered) | IoU **0.9993**, MAE 0.21 | (same run) |
| PIL 256-colour, no dither | IoU **1.0000**, MAE 0.00 | — |

0.9993 is the quantiser moving a glyph edge by a pixel in three cases per
thousand; it is not a legibility question. Handing the delivered frames to an
independent vision reader is the check a reader can repeat themselves, and the
next section is what came back.

## The mechanical evidence that the small text is readable NOW

The GIF's own frames are delivered beside it in `docs/archive/demo/human/frames/` at
exactly the film's frame size, so the film's legibility is a property of the
delivered PNGs and can be measured without decoding a GIF. `vision_analyze` on
the **final frame** (whole frame, nothing cropped for the reader's benefit):

> ■ 它由 `gopi` 命令行驱动：`gopi chat` 是交互式控制台（即当前这个界面），`gopi task submit` 把工作排入任务账本。
> ■ 任务由 worker 在各自独立的 git worktree/分支里执行，merger 负责合并与回收，全程可审计。
> ■ 简单说：一个把"派活、干活、验收、合并"流程化并自我校验的编程代理框架。
> [gopi] chat table: chat row ensured: ██████████████████████████████████████████████████ (mode=chat)
> · - 不是一个"看起来对齐就算完成"的系统——没有执行证据就不算交付，未记录即丢失（Rule 5/31、Rule 30a）。
> you>
> deepseek-flash · iter 2 · tools off · hit 49% · ↑20k ↓20k 225 · ready █

Two details in that output are the evidence, and neither is a model error:

- the redaction mask is legible **as a mask** (`██████…█ (mode=chat)`): the
  substituted cells are readable as cells, which is what the mask's own design
  asks for;
- the status row shows `hit 49% · ↑20k ↓20k 225 · ready` where a later frame shows
  `21s` — a number that is *changing as the frame is painted*. The line is
  mid-repaint, not misread: the film is a capture of a live screen.

## Honest limits

- **The delivered frame's exact wording of the terminal title bar is not part of
  this claim.** The check above is about the *chat transcript* text — the answers,
  the rule references, the status row — which is the text the film exists to
  show.
- **The film is 41 frames, not 46.** The frame reduction drops frames that repeat
  their predecessor (40 dropped this run) and a live run does not repeat
  identically, so the count is the run's own, not a constant. `manifest.json`
  carries it; `pxdiff.tsv` has one row per adjacent pair (40 for 41 frames), with
  no zero-change row.
- **The hero and the film now share a px/char figure by construction**, but only
  because both are delivered at native resolution. A future run that changes
  `TERM_COLS` or the font size moves both, and the assertions in `build.sh` and
  `assemble.py` are what keep them from drifting apart again.
- **`docs/archive/chat-loop-960x456-20260930.gif` is kept, not deleted.** It is
  the before-picture for the defect; deleting it would leave the report asserting
  a difference a reader cannot inspect.

## Verification

See the task report: `devdoc/`-adjacent evidence lives in
`~/.gopi/sessions/01a0f0de-869e-78a8-9500-a3520faf32fa/task-report.md`, including
the verbatim `Ran:` invocations (the full `build.sh` re-run, `pxdiff.py`,
`check-clean.sh`, `gopi gif`), the byte counts and the vision output above.
