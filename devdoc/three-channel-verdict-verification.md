<!-- doc-class: historical -->
# Verifying "the display swallowed characters": which channel to trust, and how

**Question this answers.** An operator reads a `gopi chat` answer on screen, copies
part of it with the mouse, and the pasted text is visibly wrong (`显示吞字少字`).
Which layer lost the characters — the renderer, or the copy?

**Short answer.** In the case measured (2026-09-26, task `01a0ddac`), the painted
grid carried the disputed line **byte-exact** and the mouse copy did not. The
renderer was faithful; the copy was lossy. But the method below is the real
deliverable — the verdict is only as good as the channel it was taken on.

## 1. The three channels, and which one is allowed to be evidence

| channel | what it is | independent of the operator's copy? |
|---|---|---|
| **A** transcript (`gopi log <id> --response --text`) | what the agent's text was | **NO — once the operator pastes his copy back into the conversation, his paste IS in A** |
| **B** painted grid (`gopi chat --dump-screen`, read by `gopi ui_state`) | the cells the terminal emulator believes were drawn | yes |
| **C** the operator's mouse copy | a human action in a terminal | it is the thing under test |

**The trap, measured:** in the disputed session `A[474] == "dump 里:  " + C[0]` →
True. The operator pasted his copy back, so A contains C. A diff "A vs C" then
looks like it measures the renderer while actually measuring the copy against
itself. **Compare C against B, never against an A that contains C.**

## 2. The method (all four legs run, not proposed)

```bash
export GOPI_HOME=<the profile whose session you mean>

# 1. which dump is on hand (never refuses; resolves an ambiguity)
gopi ui_state --target chat --list

# 2. is the line you copied on the PAINTED grid?  — the one-call check
gopi ui_state --compare "<the line you copied>" --session <session.jsonl> --target chat
#    match    -> painted
#    mismatch -> the `reason=` names which layer lost it
#    refused  -> the grid may not be read as a screen (never a pass)
#    exit 3   -> the two sides disagree (a RESULT, not a failure to run)

# 3. the whole answer, one line of shell
gopi ui_state --all --session <session.jsonl> --target chat
#    exit 3 = a line disagreed; exit 1 = the audit COULD NOT RUN

# 4. before blaming ANY layer: can the dump be read as the paint at all?
GOPI_REPLAY_RAW=<pty capture> GOPI_REPLAY_COLS=117 GOPI_REPLAY_ROWS=26 \
  go test ./internal/lineedit/ -run TestScreenReplayAgreesAtEveryChunkSize -v
```

### Read `paint_loss` with the scroll-off caveat

`--all` on a healthy run reported `matched=0 disagreed=10`, including
`damage=paint_loss` for a line as short as `结尾一行：句末。`. Those lines were not
lost — the answer had **scrolled off** the 26-row visible grid before the dump was
read, and the tool's own `reason` says so ("… or it has scrolled out of the visible
grid"). The tool is honest; the reader must be. **A single dump is one instant,
rewritten at every repaint** (`frame_timing` is one of its own blind spots), so a
dump is weak evidence of ABSENCE. Prefer the answer's own frame series, or a frame
in which the line was still on screen.

## 3. What the falsification hunt tried, and what it found

Attacks aimed at finding a genuine paint loss: the operator's own narrow geometry
(117x26), the exact line the copy mangles, the double space and the trailing CJK,
the marker cells, four dump frames across 73 frames of animation, and a re-read of
the row de-padded through the **cell model** (each double-width rune consumes its
cell plus its filler). **No paint loss reproduced.** `go test
TestScreenReplayAgreesAtEveryChunkSize` on the project's own capture: PASS
(identical grids and scrollback over 8 read sizes).

**What did surface (a warning about evidence, not about the renderer):** a
published dump header is **not** a durable artifact. The header above one audit's
channel-B evidence named `pid=3792954 … frames=687`, while the frame files saved as
that evidence carried `pid=3793253 … frames=65` and `pid=3798917 … frames=64`; no
file on the host carried the published pid. **Quote the frame you saved, or quote
nothing** — a header typed into a report can describe a run that no longer exists.

**A shape worth naming (hypothesis, not a cause):** the raw stream writes some
answer rows in more than one write — a row head, then its tail re-indented by
three spaces (`▌ ### 她错的部分：` then a continuation with a leading indent).
A copy taken between those two writes has exactly the shape the operator's copy
has: a partial row with an offset head.

## 4. The C-side shape, stated as measurement only

From the operator's real `/tmp/dump_text.txt` (4945 B, 90 lines): all 64 non-blank
bodies appear verbatim in the transcript, **0/64** order disagreements (a first,
hand-rolled de-padding produced a bogus "reorder" reading — the corrected model is
per-row **head loss with order preserved**), mean coverage 24.6 cells against the
file's own shortest non-blank line of 32 cells, and 57 `**` pairs where the paint
stream emits 2 — i.e. **C carries the literal emphasis markers the renderer
consumes**. The mechanism that produces the head loss is **not established**.

## 5. What to reach for next time

1. `gopi ui_state --compare` first: it is one call, it refuses rather than guesses,
   and it names the layer.
2. If a dump is not armed, arm it (`--dump-screen`) **before** the turn you care
   about — a dump that was never armed cannot be reconstructed afterwards.
3. Never let a copy be its own reference: if the operator pasted it back, A is
   contaminated and B is the only independent leg.
4. Read a header as a claim, not as proof; the frame file is the artifact.
