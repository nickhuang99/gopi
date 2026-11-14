<!-- doc-class: historical -->
---
doc-class: historical
status: SNAPSHOT as of 2026-09-24 — the measurement this records is frozen; the two items it grounds (01a0d590-6537, 01a0d590-8472) were still running when it was written
---

# Self-aware UI: the sense exists, is honest about its limits, and the agent cannot see it

**Source material**: `~/work/claude/nick/ui_self_aware.txt` (318 lines, an external
analysis Nick supplied). This doc does three things: records what that analysis gets
right, corrects the one place where our measured system is *ahead* of its model, and
names the bottleneck it does not mention.

Everything below marked **MEASURED** was run on HP at master `216d3d41` with a rebuilt
binary on 2026-09-24. Nothing here is inferred from the analysis.

## 1. The analysis' central judgement is correct and worth keeping verbatim

Its own words:

> 不管是终端UI还是浏览器，**只要程序持有UI的状态模型，就不需要截图**；只有当Agent只能
> 拿到最终渲染输出（纯文本流/像素画面）时，才需要多模态OCR/截图识别。
> 你遇到的困惑根源：把「渲染输出」和「内部状态模型」混在一起。

And its answer to the sharpest question — *can a program know where the thing it just
printed ended up?*:

| | the analysis' answer |
|---|---|
| bare pty text stream | **no** — ANSI is characters + escapes; the terminal emulator owns the screen buffer; the program keeps no per-line coordinates |
| a component tree (BubbleTea Model) | **yes** — positions are computed in the in-memory model *before* rendering |

**Keep this distinction.** It is the reason vision is a fallback rather than the primary
sense anywhere in our stack, and it is independently why `[1/4]`'s sense verbs are
ordered the way they are (`ui_state` first, `capture`/`act` as fallback).

## 2. Where our system is AHEAD of the analysis — the third path it never considered

The analysis frames the choice as binary:

- read the **in-memory component tree** (requires wrapping every widget in a
  `Metadata()` exporter — "原生没有一键导出", "生态极小"),
- **or** use CDP + DOM/AX tree (mature, but needs a browser runtime).

It dismisses the pty stream outright (row 8: *"裸pty流不知道光标在哪、每个单词在屏幕坐标"*),
and on that basis recommends building the component-tree exporter from scratch
(方案A) or going to the browser (方案B).

**MEASURED — gopi does neither.** `gopi ui_state` re-interprets the pty byte stream
into an assertable cell-grid model. It reads the *rendered output*, and reconstructs
everything the analysis says a pty stream cannot carry:

```
$ ./bin/gopi ui_state --pid 1265828
#gopi-ui-state pid=1265828 target=http://127.0.0.1:8101 state=exited
#gopi-screen-dump v=1 cols=80 rows=24
cursor row=22 col=0
scroll_region top=0 bottom=23
attrs recorded=true
lines_begin count=24
000|gopi ui → http://127.0.0.1:8101  (tools on)
001|  · gopi ui — type a message and press Enter. Ctrl+X interrupts a turn, Ctrl+C
002|     quits.
```

So, contra row 87 (`裸pty文本流：不能`):

| the analysis says a pty stream cannot provide | what our dump carries |
|---|---|
| cursor position | `cursor row=22 col=0` |
| — | `scroll_region top=0 bottom=23` |
| — | `attrs recorded=true` (per-cell attributes) |
| — | an explicit `cols=80 rows=24` grid whose blank cells are distinguishable as *asserted blanks* |

And it is not naive about the result. The dump **self-discloses 8 blind spots** and,
when nothing was painted, refuses to imply an empty screen:

```
blind_spots_begin count=8
000|font|glyph shapes and metrics are the terminal emulator's; the grid holds code points…
001|ime_composition|an IME composes before the application receives bytes; preedit state never enters the grid
002|raw_bytes|the dump is the interpreted EFFECT of the byte stream, not the stream: escape-sequence order, framing and inter-frame timing are not kept
003|graphics_protocols|sixel / kitty graphics sequences are skipped…
004|private_modes|private modes … are parsed and not recorded
005|blank_vs_unpainted|a space cell and a never-painted cell are identical in the grid
006|interior_scroll_loss|a line pushed off an INTERIOR scroll-region margin is discarded by the terminal
007|frame_timing|a dump is a single instant; intermediate frames are not observable
```

> the dump sink never interpreted a render frame (frames=0): the grid below is an
> all-blank snapshot — the surface had not painted yet, or its panel was erased as it
> quit. **It is NOT evidence that the screen is blank.**

**Why this matters beyond "we did it differently":** it inverts the analysis' own
biggest stated risk. The analysis' §三 lists *"感知与执行耦合"* as a reason the
self-iteration loop breaks:

> Agent 用来感知UI的组件树/CDP DOM，本身就是UI代码的产物。如果Agent修改了UI框架底层，
> **感知接口本身会被破坏**。也就是Agent改坏了UI，它就失去了观察世界的窗口，闭环断裂。

That coupling is **inherent** to reading an in-memory component tree (the sense is a
product of the very code being edited) and to CDP (the browser process must survive).
It is **weaker** for a rendered-output re-interpretation: as long as the surface emits
*any* bytes, there is something to interpret, and the interpreter lives outside the
code under edit. The analysis treats the coupling as unavoidable; our shape reduces it.

## 3. The bottleneck the analysis does not mention

The analysis asks whether the agent *can* perceive its UI, and answers yes (via a
component tree it would have to build). **MEASURED: the bottleneck is not capability,
and not even the missing exporter — it is that the agent does not know the sense
exists.**

```
$ grep -rn "selfKnowledge\|SelfKnowledge" --include=*.go internal/ cmd/     →  zero hits
$ grep -rn "ui_state\|dump-screen" --include=*.go internal/agent/          →  zero hits
```

`gopi ui_state` is built, wired, works end-to-end, refuses honestly on ambiguity
(`2 live gopi surfaces could be the target; refusing to guess — pass --pid (or --list)`),
refuses honestly when there is nothing to read (`no ui-screen.dump dump found in any
scanned location`), and yet: **`internal/agent/` has no mention of it.** The eye is
complete and the thing that would use it has never been told it has one.

This is the same failure family we have hit repeatedly — a capability shipped on one
path and never connected to the consumer" — and it is why the two open items exist:

| item | what it settles |
|---|---|
| `01a0d590-6537` **[1/4]** | publish the senses (`selfKnowledge` prompt section) so the agent knows it *is* gopi and can sense itself |
| `01a0d590-8472` **[2/4]** | measure whether a `--tools` chat turn discovers `ui_state` **on its own** |

`[2/4]` is the experiment that decides whether `[1/4]` is *needed* or merely *cheaper*:
if a natural chat turn already reaches for `ui_state`, the sense is discoverable; if it
does not, `[1/4]` is the fix and `[2/4]` is its evidence. **Written so either outcome is
a valid result** — a negative here is worth more than a rehearsed positive.

## 4. The analysis' honest concession, which we should keep

它的 §三 Level 1 结论：

> **公开开源项目里，没有端到端完整跑通这个闭环的生产级例子。**

and it separates the levels correctly: **Self-Harness** (2026, Jiaxin Zhang) is a real
reproducible loop, but it edits *the agent's execution rules* — tool limits, retry
policy, stop conditions, prompt templates — **not UI rendering**. Hermes' auto-generated
skills are behavior-layer, not UI-layer. Its §三 also names why UI self-iteration is
rare: regression burden explodes, perception/execution coupling, and low priority.

**We should accept this as the honest state of the art and not overclaim.** Our `[3/4]`
(census of exercised verbs) and `[4/4]` (file a real improvement row from a chat turn)
are steps toward Level 1, not a demonstration of it. What is genuinely ahead of the
public state of the art is narrow and should be stated narrowly: *a UI whose rendered
output is re-interpretable as an assertable grid, plus a CLI verb that exposes it, plus
a blind-spot disclosure that says what it cannot see.* Not "an agent that maintains its
own UI."

## 5. Practical constraints confirmed by the analysis

- **Markdown as the single source of truth, with two projections, is sound** (§249-267):
  agent reads the parsed structure; humans read TUI/ANSI or web. Same model, two views.
  No vectors, no custom fonts needed at prototype stage.
- **Do not chase terminal graphics** (§276-285): Sixel/Kitty are bitmap, terminal-dependent,
  fail over ssh, and give the agent nothing. Unicode block art is the ceiling that matters.
- **Console has no native vector and no custom fonts** — a terminal is a character-grid
  device. This is a protocol limit, not an implementation gap.

## 6. What this doc is for

`[1/4]` and `[2/4]` both need a stated basis for *why* they exist. This is it:

1. structured perception beats screenshot perception (§1) — the reason `ui_state` is the
   primary sense and `capture` a fallback;
2. the sense already exists in a form better than the analysis assumed, and is
   **decoupled** from the code an agent might edit (§2) — the reason the loop is worth
   attempting at all here;
3. the actual blocker is publication, not capability (§3) — the reason the two items are
   a prompt section and a measurement rather than an implementation.
