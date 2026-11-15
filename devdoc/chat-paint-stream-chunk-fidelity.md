<!-- doc-class: normative -->
# The chat paint stream: which layer loses text — a three-channel comparison

**Verdict: A = B ≠ C.** The transcript (A) and the painted screen (B) agree; the
operator's mouse copy (C, `/tmp/dump_text.txt`) does not. **But the investigation
found a real, reproducible defect in a fourth place — the dump's own byte-stream
interpreter** — and that defect is fixed here with a RED→GREEN proof. It is the
reason an agent reading the dump back could not tell a fabricated loss from a real
one.

Task: `01a0dd26-a578-7e81-a81e-f751dcf993eb`, branch
`docs/chat-screen-three-channel-diff`.

## The three channels, on one known input

| | channel | how it was obtained |
|---|---|---|
| **A** | the authoritative transcript | the assistant text block of session `8c836970-…`, i.e. exactly `gopi log 8c836970-… --response --text` (705 lines) |
| **B** | a real `gopi chat --dump-screen` grid | the fix's own build, **real pty at 117x26**, seeded by a local SSE stub that streams the known input |
| **C** | the operator's mouse copy | `/tmp/dump_text.txt` (89 lines) |

B's provenance, quoted from the dump's own header (Rule 31 — the surface was
executed, not reasoned about):

```
#gopi-chat-dump pid=3792954 target=self:chat-tools tools=on terminal=117x26 model=117x26 grid=117x26 geometry=layout mode=ansi frames=687 dumps=45 …
```

`terminal=117x26 model=117x26 grid=117x26` is the header's own statement that the
layout geometry and the dump geometry are the same number — the "grid=80x24 while
the layout was 117x26" failure a previous run hit (`--dump-screen` now defaults the
dump geometry to the terminal's, so it no longer needs `--cols/--rows`).

### A|B|C, the operator's five lines

```
A: ## Doubao 说对了一半 —— 但关键那一半是错的，代码可以证明
B: ▌ ## Doubao 说对了一半 —— 但关键那一半是错的，代码可以证明
C:  证明oubao 说对了一半 —— 但关键那一半是错的，代码可以

A: bubbletea **确实**有内存驻留的数据结构，而且它确实比"浏览器引擎开放 API"更简单、更少 bug。这条我实测同意：
B: ▌ bubbletea 确实有内存驻留的数据结构，而且它确实比"浏览器引擎开放 API"更简单、更少 bug。这条我实测同意：
C: ▌ "更简单、更少 bug。这条我实测同意： 构，而且它确实比"浏览器引擎开放 API

A: | 布局状态在哪 | `tea.Model` 的 `View()` 返回**一个字符串**，renderer 写进 `tea.WithOutput` 的 io.Writer | DOM / layout tree / compositor 分层 |
B:   | 布局状态在哪 | `tea.Model` 的 `View()` 返回一个字符串，renderer 写进 `tea.WithOutput` 的 io.Writer | DOM / layout tr
C: ▌ | 布局状态在哪 | `tea.Model` 的 `View()` 返回**一个字符串**，renderer 写进 `tea.WithOutput` 的 io.Writer | DOM / la
```

**B carries every cell A has** (it differs from A only by the `▌` turn marker the
layout prepends, and by the emphasis markers that `--no-color`-style stripping
removes — the dump's per-cell attribute channel is where bold lives, by design).
**C does not**: line 1 lost the first three characters of `## Doubao` down to
`证明`; line 2 is scrambled — a fragment of line 2 (`构，而且它确实比…`) sits
*after* the end of line 2 (`…这条我实测同意：`), and a fragment of line 3
(`"更简单、更少 bug。…`) sits before it; and C's line cell counts are 27 where B
paints 115.

So the losing layer is **C**, and specifically a **reorder inside the copy**: the
substrings C holds are all present in A/B, in the wrong ORDER, which is what a
copy that assembles a scrollback from terminal rows the reader did not ask for
produces — not a loss inside gopi.

## The real defect this comparison exposed (fixed here)

The three-channel diff above is what the channel `--compare` check does. To make B
*provable* rather than asserted, B was recorded twice: once as the dump's grid, and
once as the raw pty byte stream. Replaying **the same byte stream** through the
interpreter the dump sink uses (`internal/lineedit.Screen`) at several read sizes
produced — before the fix — **different screens**:

```
# same 16592 bytes, same 117x26 grid, read 64 bytes at a time
BEFORE: 000|[gopi] no GOPI.md found — searched: /home/user/.gopi/…        002|, /
        006|004h                                     022|H                024|86myou>
AFTER:  000|  我 这 次 真 跑 过 ：                                          
        001|▌ - `gopi ui_state --list` → 扫  2 个 根 ， 0 个  dump …
        017|▌   易 反 复 被 误 传 的 点 （ Doubao 就 传 错 了 ） …
scrollback lines: BEFORE 0, AFTER 74
```

Row `004h`, `H`, `86myou>` are the smoking gun: those are **pieces of escape
sequences rendered as text**, i.e. the parser threw away the head of a sequence
and printed its tail into cells. Two root causes, both in `Screen.Feed`:

1. **A chunk that ended mid-sequence was dropped.** `consumeEscape` returned "the
   rest is not here", and `Feed` returned — discarding the tail. The next read
   then began in the middle of a sequence, so `ESC[24;1H` split after `ESC[24;`
   left `1H` to be painted as text and the cursor was never positioned. Impact is
   not cosmetic: on the recorded stream the whole answer and **all 74 lines of
   scrollback** disappeared (the layout's region scrolled the answer off the
   screen, and the read that should have written the rows had thrown away the
   positioning escape).
2. **A CSI whose parameters were split was accumulated twice.** `consumeEscape`
   returned -1 but had already stored `1` and `3` in the parser's scratch; the
   retry parsed the same digits *over* them → `ESC[1;3r` became `ESC[11;33r`. On a
   3-row grid that installs a scroll region of rows 11..33 instead of 1..3 — a
   layout that then discards the rows it was meant to keep.
3. **An abandoned OSC string ate the next sequence's introducer.** `oscLen`
   treated any `ESC` inside an OSC payload as an ST and consumed the following
   byte, so an unterminated `ESC]11;?` followed by `ESC[1;1Hok` left `1;1H` to be
   painted as text (`TestScreenOSCSplitAcrossChunks`, a pre-existing test, is the
   RED witness for this one). A terminal aborts the string at that ESC and reads
   it as the next sequence's start.

A terminal is a **byte stream**, and the split point between two reads is the
*reader's* choice (a tty buffer, a `select()` return), never a fact about the
paint. So the criterion is the property, not a byte offset:

> **The same stream must paint the same screen at every read size.**

## The fix

`internal/lineedit/screen.go`:

- `Screen.pending` carries an un-interpretable tail (a truncated escape, or the
  first bytes of a multi-byte rune) into the next `Feed` instead of dropping it,
  bounded by `maxPendingEscape` so a stream that never completes a sequence
  cannot grow the parser's state without limit.
- `consumeEscape` clears the parameter scratch **before** parsing, so a sequence
  re-offered after a split cannot read its own digits twice.
- `oscLen` aborts an OSC string at an `ESC` that is not the ST introducer
  (`oscSTFinal`), instead of consuming the next sequence's first byte.

`Screen.Feed`'s own doc comment states the criterion, so the next reader of that
function learns the invariant rather than rediscovering it.

## The self-verifiable path (no operator, no new tooling)

`internal/lineedit/screen_replay_selfcheck_test.go` —
`TestScreenReplayAgreesAtEveryChunkSize`. It replays one recorded pty capture at
eight read sizes (1, 2, 3, 7, 13, 64, 333, 4096) and asserts one screen and one
scrollback. Exact command:

```bash
GOPI_REPLAY_RAW=<capture.raw> GOPI_REPLAY_COLS=117 GOPI_REPLAY_ROWS=26 \
  go test ./internal/lineedit/ -run TestScreenReplayAgreesAtEveryChunkSize -v
```

Observed output: `replayed … (16592 bytes) at 117x26 over 8 read sizes: identical
grids and scrollback`. The geometry is taken from the dump header
(`terminal=117x26 grid=117x26`), and the check **skips with a named reason** when
`GOPI_REPLAY_RAW` is unset — a green that measured nothing would be worse than a
skip.

The same paragraph is in the `gopi ui_state -h` page, next to the other two
channel checks (`--compare`, and `gopi log <id> --response --text`), so an agent
asking "which layer lost this text?" meets all three in one place.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (see the close-out run).
- Tests: `go test ./internal/lineedit/` green; `cmd/agent -run TestUIState` green;
  `internal/standards/` green.
- Ran (Rule 31): the surfaces this change touches were executed for real:
  - `bin/gopi chat --dump-screen=…` under a real pty at 117x26, twice (once per
    build), seeded by a local SSE stub — the header quoted above is that run's
    own output.
  - `./bin/gopi ui_state --compare "<line>" --session … --pid 3792954` → a real
    verdict line (`match=false … reason="neither the transcript nor the grid has
    this line verbatim"` for a line C holds but no layer painted — the C
    corruption, reported honestly rather than blamed on a gopi layer).
  - the replay self-check above, with `GOPI_REPLAY_RAW` pointing at the recorded
    capture.
- Result: RED→GREEN on the SAME input and the SAME channel (see below).

### The fixed build on the real surface (B, live)

Same command as above, `bin/gopi` rebuilt from the fixed tree, seeded with the
same known text. The mid-turn snapshot (the frame the operator would be reading)
shows the answer band with the dump header's own numbers:

```
#gopi-chat-dump pid=3801942 target=self:chat tools=off terminal=117x26 model=117x26 grid=117x26 geometry=layout mode=ansi frames=79 dumps=8 …
grid 022|▌ se GOPI_MAX_TOKENS to keep a larger thinking budget.
grid 023|▌ bubbletea 确 实 有 内 存 驻 留 的 数 据 结 构 ， 而 且 它 确 实 比 "浏 览 器 引 擎 开
grid 024|you>
grid 025|—— 但 关 键 那 一 半 是 错 的 ， 代 码 可 以 证 明
```

(The two halves of the heading are on rows 023 and 025 because the live answer
band and the prompt row interleave while a turn is still streaming — the same
`frame_timing` property the dump's `blind_spots` list names.)

### RED → GREEN (same input bytes, same channel)

`internal/lineedit/screen_chunk_carry_test.go` is the regression test.
Two independent RED proofs were taken on the real recorded capture:

1. **Pre-fix parser, 64-byte reads:** `scrollback_lines=0`, grid rows are the
   escape-sequence fragments quoted above. Artifact: `grid-head-chunk64.txt`
   (5928 bytes) vs `grid-fixed-chunk64.txt` (14457 bytes).
2. **Carry present but the pre-clear removed** (isolating cause 2):
   `region-split: step=1 paints a different screen` and
   `step=1 installed region 2..2, want 0..2 (split parameters were accumulated
   twice)` for steps 1, 2, 3 — then green with the pre-clear restored.
3. **Both fixes reverted, synthetic cases:** all three tests fail
   (`region-split: step=1/2/3 paints a different screen`,
   `installed region 2..2, want 0..2`); with them, `ok`.

## Not Done (honest)

- **C's own mechanism is not proven.** What is proven is that C's content is a
  reordered subset of A/B (every fragment C holds exists in A/B; the order is
  wrong; C's line lengths are 27 vs B's 115 cells). *Why* the reader's copy
  assembled that way — terminal scrollback selection under a full-screen app, or
  the clipboard path — was not instrumented: it lives outside gopi's process
  tree, and `/tmp/dump_text.txt` carries no provenance. The verdict names C as the
  lossy layer; it does not claim to have reproduced the copy.
- **The dump is a single instant.** The `--dump-screen` artifact is rewritten at
  the end of a turn, so the frame the operator was reading is only preserved
  because this investigation snapshotted it mid-turn. Emission is not changed here
  (`blind_spots` already lists `frame_timing` out loud); a follow-up could dump
  every frame.
- **The `▌` turn marker and the missing `**` are not losses** and are not "fixed":
  the marker is the layout's, and bold lives in the dump's attribute channel
  rather than in the text — the same shape the SGR-grid work established.
- **The chat paint stream is only replayed, not re-emitted** with the fix in a
  same-turn A/B (the pre-fix binary no longer exists). The RED proof is therefore
  taken where it is exact and reproducible: on the recorded byte stream through
  the two parser revisions.

## Files

- `internal/lineedit/screen.go` — the fix (carry buffer + pre-clear) and the
  invariant in `Feed`'s doc comment.
- `internal/lineedit/screen_chunk_carry_test.go` — the regression tests
  (chunk-boundary tail, split-CSI parameters, bounded carry, whole-frame
  non-regression, a recorded-frame-shape case).
- `internal/lineedit/screen_replay_selfcheck_test.go` — the self-verifiable
  replay check (env-gated, named skip).
- `cmd/agent/cli_uistate.go` — the `-h` page now names the third channel and the
  command that checks it.

## Evidence (self-filed)

- Build: `CGO_ENABLED=0 go build ./...` → `BUILD_OK`; `bin/gopi` rebuilt → binary
  dated 2026-09-26T18:18Z.
- Header proof for B:
  `#gopi-chat-dump … terminal=117x26 model=117x26 grid=117x26 geometry=layout
  mode=ansi frames=687 dumps=45 …` (from `runFull/chat-screen.dump`).
- RED (real capture, 64-byte reads, HEAD parser): `scrollback_lines=0`;
  artifact 5928 bytes vs GREEN 14457 bytes.
- GREEN (same capture, same reads, fixed parser): `scrollback_lines=74`,
  `grid-head-chunk64` vs `grid-fixed-chunk64` diff = the quoted rows.
- Self-check: `replayed … (16592 bytes) at 117x26 over 8 read sizes: identical
  grids and scrollback`.
- `gopi ui_state --compare` exit 0 with a per-line verdict and a blind-spot list
  (the call was made; the verdict line is quoted in `## Verification`).
- `git status --porcelain` in the worktree: **empty** (measured after the commit; the
  three changed/added files are committed on this branch (Rule 30).
