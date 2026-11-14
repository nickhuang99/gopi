<!-- doc-class: historical -->
# Which channels lose chat text — a four-channel verdict with the measuring stick's own error

Task `01a0ddf7-e068-7338-bf60-dc1b74eca422`. Settles a conflicting pair of verdicts about
**where** text is lost: sibling `01a0dd26` ("A(transcript) == B(painted) != C(mouse copy)") versus a
later session `a5e1c3e9` ("`gopi log <id> --response --text` = 4727 bytes vs 3831 chars — a FOURTH
lossy channel, truncating mid-clause"). Investigation only: no product code changed.

## 0. Verdict in one line

**Channel D (`gopi log <id> --response --text`) is byte-exact and width-independent — the "fourth
lossy channel" is a unit mix-up (bytes vs runes) plus a measuring stick that cuts BYTES while
believing it cuts characters.** The sibling's `A == B != C` verdict stands **for whole lines**.
The only replay-layer loss that IS real in the `gopi log` family is in the **default** `gopi log <id>`
form (`FormatSession`), which drops 4 of 5 assistant blocks and caps the fifth — 94.4% of the text,
at `internal/gopilog/formatter.go:106` and `:108`. That is a human-readability summary, never the
channel `--response --text` uses. **And the paint is not wholly innocent: §10.1 measures a
mid-line stitch in the sibling's own frame series that exists in no source line — found after this
document's first commit; §10 carries the correction to §7 and the new finding.**

## 1. The reproduction, channel by channel (session `a5e1c3e9-fcfb-450c-a0a8-abcea5688f25`)

The session's assistant text blocks, summed independently from `session.jsonl` (channel **D**):

| block | runes | UTF-8 bytes |
|---|---|---|
| 0 | 1080 | 1089 |
| 1 | 1887 | 1913 |
| 2 | 864 | 1722 |
| **first 3** | **3831** | **4724** |
| 3 | 985 | 2018 |
| 4 | 1246 | 2392 |
| **all 5** | **6062** | **9134** |

```
gopi log a5e1c3e9... --response --text | wc -c   -> 9139      (sum bytes 9134 + 5 wrapper newlines)
gopi log a5e1c3e9... --response --text | wc -l   -> 86
gopi log a5e1c3e9... --json | cmp - session.jsonl -> IDENTICAL
cmp A_final.txt D_recompute.txt                   -> IDENTICAL, diff line count = 0
```

**The reported delta is the same three blocks in two units.** `sum runes(first 3) = 3831` is exactly
the reported "3831 chars"; `sum bytes(first 3) + 3 wrapper newlines = 4724 + 3 = 4727` is exactly the
reported "4727 bytes"; and 44 is the line count of those same 3 blocks. One rune is 3 UTF-8 bytes in
this text — the gap is the unit, not missing characters.

Attributed layer of the `+1` per block: `internal/gopilog/formatter.go:334` `FormatResponse` writes
`text` then `"\n"` — one newline per block, nothing else.

## 2. Width dependence — two explicit widths, IDENTICAL

```
COLUMNS=40  gopi log <id> --response --text -> 9139 bytes / 86 lines
COLUMNS=200 gopi log <id> --response --text -> 9139 bytes / 86 lines
cmp w40.txt w200.txt -> IDENTICAL
```
`--response --text` never consults the terminal: it is a pure file->stdout path. A loss that changes
with width cannot live there; the width-sensitive layer is the **paint** (channel B).

## 3. The SHAPE of the loss in the DEFAULT form, and its layer

Real, measured, and never reported before this task:

```
gopi log a5e1c3e9... > def_final.txt   -> 33927 bytes / 308 lines
assistant region of that output        -> lines 289..299
per block, matched by the block's own opening against the printed output:
  block 0 (1080 runes): opening ABSENT   -> verbatim delivered    23 runes
  block 1 (1887 runes): opening ABSENT   -> verbatim delivered    18 runes
  block 2 ( 864 runes): opening ABSENT   -> verbatim delivered     1 rune   (block ABSENT)
  block 3 ( 985 runes): opening ABSENT   -> verbatim delivered     2 runes  (block ABSENT)
  block 4 (1246 runes): opening on line 289 -> verbatim delivered 297 runes + "..."
TOTAL addressable 6062 -> delivered 341 -> LOST 5721 runes (94.4%)
```

The counts above are the per-block ones; the earlier draft's 300/95.1% came from treating the
opening-probe match as whole-block presence. Both name the same two layers:
4 of 5 blocks never reach the file at all, and the fifth is capped at 300 runes.

- **Layer 1 — the guard.** `internal/gopilog/formatter.go:106` `if len(iter.ToolCalls) == 0` prints a
  text block ONLY for iterations with no tool call. Every assistant block that accompanied a tool
  call is invisible in this form. (Shape: whole-block, head-of-iteration.) This is the dominant loss.
- **Layer 2 — the cap.** `internal/gopilog/formatter.go:108` prints
  `truncateRunes(text, formatTextMaxLen)` with `formatTextMaxLen = 300` (`:39`); `truncateRunes`
  (`:49`) appends `"..."`. Printed evidence in situ:

  ```
  def_final.txt:298:- **§0 一句话** —— dump 不是眼睛，是记忆；记忆错误无法被察觉。...
  ```
  (the block is 1246 runes; the output carries 297 + `...`). **Shape: TAIL truncation of one block.**

- **Scope of that loss.** The same two lines exist in `FormatChat` (`:886`) but there they do NOT
  drop text: an iteration with tool calls prints its thinking block and its tool calls separately, so
  the TUI shows every turn. The 94.4% applies to the CLI summary form `FormatSession` — the one a
  reader or agent reaches for by default.

## 4. The "cut mid-clause" claim — the measuring stick, not the data

`cut -c1-200` cuts **BYTES** when `LC_ALL=C` (this box's `LC_ALL` is literally `C`, `LC_CTYPE` empty
— the half-initialised locale: `LANG` set, `LC_CTYPE` unset). On a CJK line 1 rune is up to 3 bytes:

```
longest_cjk.txt : runes=223  bytes=543
LANG=C cut -c1-200 | tail -1  ->  82 runes / 200 bytes   tail='），而是**补出那个用户级文件机制**，再把 per'
python3 s[:200] (200 runes)   -> 200 runes / 478 bytes   tail='*的真实运行证据，并且两条 chat 路径都要各验一'
```

The byte-cut ends mid-CLAUSE, in the middle of an English word — exactly the "truncated mid-sentence"
signature reported. **The stick that reported the "truncation" was slicing 200 bytes of a 223-rune
line and calling the result 200 characters.** With the rune-correct stick the same line is intact.

## 5. Reconciliation with the sibling (`01a0dd26`, B channel at 117x26)

- Sibling: "A == B != C — only the mouse copy loses text." **Stands.** Nothing in this task's
  evidence contradicts it: the `--response --text` channel is byte-exact (§1), and its width
  independence §2 is what a faithful grid-level channel must show.
- The sibling's channel **B** is a *grid*, and a grid is a **window**, not a transcript. Re-measured
  here: its headline artifact `runFull/chat-screen.dump` reports **frames=687**, and the frame it
  preserves is post-teardown — every row above the prompt is blank (`ui_state --pid 3792954 --json`
  -> rows 0..23 empty, row 24 `you>`, row 25 the status bar). So "B == A" is a claim about the *text
  the grid still holds at the captured moment*, not about the transcript as a whole. The sibling's
  own `THREE_CHANNEL_DIFF.md` compares B's rows against a 291-char **region** of A, and its own
  comparison harness shows what a window does: painted rows are per-line pieces carrying the `▌`
  fence marker (43 of them in `grid-fixed-chunk64.txt`), and A's rows wider than the 117-column grid
  — **97 of them** measured, one at display width 284 — have **no matching grid row at all**
  ("grid rows containing its first 40 chars: 0"). A grid re-wraps by columns and scrolls; A does not.
  This is a **shape** difference (continuation-row re-flow, plus the fence marker), not a character
  loss — and it is why `A == B` cannot be read as "the paint preserves every byte of A".
- The `--all` audit on the sibling's own transcript says the same thing in the tool's vocabulary:
  `transcript_lines=64 audited=51 skipped=13 matched=0 disagreed=51`, damages `paint_loss`,
  `wrap_artifact_or_loss`, `cjk_wrap`. Its own reason string names the three possibilities — "the
  paint lost it, re-wrapped it, or it has scrolled out of the visible grid".

**Both verdicts are true of different objects**: A == D (byte-faithful); B is faithful *as a window
onto a re-wrapped grid*; C (the mouse copy) is the lossy one.

## 6. `01a0ddac`'s premise — it does not stand (and its own verdict is now incomplete)

`01a0ddac-61f6-7970-8fba-c2352bcf6ca8` exists to **verify** the sibling's three-channel verdict
because that verdict "contradicts the operator's own reading" (我已经发现它的显示吞字少字). Row
status: **completed**. Its premise was "the sibling says the DISPLAY is faithful — check it". This
task's evidence shows the premise was a **category error in the framing**, not a false finding: the
sibling measured a *text grid* (cells), the operator was reading a *screen*. Both are "the display".

**And its own bottom line — "RENDERER-FAITHFUL (no paint loss found), no bug task filed" — is
incomplete**, not wrong: it is true for the *whole lines* it measured (its own run, own pty, own
header, its own falsification hunt, all of which this task respects), while §10.1 measures a
**mid-line** stitch in the sibling's own frame series that no whole-line test can see. Its parenthetical
warning was right in the other direction too: the report itself notes the channel-B header published
in the sibling's evidence belongs to a **different pid** than the frames saved with it — a second
reason a single artifact must never be trusted alone.

## 7. `01a0ddcd-2b52`'s status — completed, correct, and MERGED (corrected in §10.2)

`01a0ddcd-2b52-771f-8fea-2c3dc42b5b56` (the sibling's own bug report for this same replay path) is
**completed** and reached the **same conclusion as §1, independently and earlier**:
`devdoc/log-response-text-count-fidelity.md` on branch `task/01a0ddcd2b52771f8fea2c3dc42b5b56` —
"does NOT lose text — the delta is a bytes-vs-runes unit mix-up". Its change is three tests
(`TestResponseTextCountRelationshipMatchesTranscript`,
`TestResponseTextBytesExceedRunesForCJK`, `TestResponseTextPreservesEveryBlockWhole`) +
the devdoc + its DOC-INDEX row.

**This section's first version said "NOT merged" and that was WRONG — see §10.2 for the
measurement and the timing that caused it.** The branch landed on master at `23:32:12`
(`73052c0d u task/01a0ddcd2b52771f8fea2c3dc42b5b56`, parents `78767c5b` + `18898c72`), one minute
after this document's own commit `9a2930b1` at `23:31:09`: the claim was true when written and
false by the time it was read. This task adds **no** product code, so there is no duplication.

## 8. Instrument reachability — `ui_state` by default

- **`--pid`-less `ui_state` refuses.** `gopi ui_state` with no selector -> exit **2**, listing
  **12** exited gopi surfaces (4 with `chat-screen.dump` in a session dir, 8 under the sibling's run
  dirs). This is the sanctioned ambiguity refusal, and it is not weakened here.
- **`--list` is the way through**, and it works: `gopi ui_state --list` -> `scanned=12 roots=2` + the
  full table, exit 0.
- **A `--compare` without `--pid` is refused for the same reason** (exit 2) — the ambiguity rule
  fires before the transcript is read. That is the friction point: reading the screen needs an
  explicit selector even when the session id is already unambiguous.
- **Session resolution is `gopilog.ResolveSessionDir`** (`cmd/agent/cli_uistate_compare.go:155-186`):
  a plain session id is looked up by `gopilog.FindSession` in the directory the *installed binary's*
  settings name — which is **not** the calling task's `GOPI_HOME`. Passing another task's session id
  yields `gopi ui_state: cannot read the transcript : open : no such file or directory`, exit 1, an
  **empty path in the message** (the id-as-directory case). Passing the explicit `.jsonl` **path**
  works: `ui_state --session <path> --compare "<line>" --pid 3801942` ->
  `transcript_lines=64 match=false ... in_transcript=true reason="the transcript has this line but
  the grid does not ..."`, exit **3** (compare ran and disagreed). A path that does not exist gives
  the same exit 1, with the path named.
- Exit codes observed, consistent with the usage text: `0` read/agreed, `1` no dump / could not run,
  `2` ambiguous (no `--pid`), `3` compare/audit ran and disagreed.

## 9. What a future reader should do with this

1. Trust `gopi log <id> --response --text` (and `--json`) as the **verbatim** channel; never the
   default `gopi log <id>` form for a text-fidelity claim — it is a summary, losing 94.4% here.
2. Never compare a byte count against a rune count (`wc -c` vs a `len()` over CJK differs ~3x).
3. Never use `cut -c`/`head -c` as a *character* measuring stick unless `LC_ALL` names a UTF-8
   locale — on this box `LC_ALL=C`, so `cut -c` IS `cut -b`.
4. When a grid/channel B is compared against a transcript, compare **shapes** (re-wrap, fence marker,
   scroll window) as well as bytes; "no matching row" is not the same as "lost character".

---

# 10. Post-verdict corrections and a new finding (same session, after the first commit)

Everything below was measured **after** commit `9a2930b1`. Sections 1–6 and 8–9 stand as written;
two claims in §7/§8 are corrected here, and §10.1 is a **loss the first pass had not found**.

## 10.1 A mid-line loss in channel B — evidence IN the sibling's own artifact

The first pass reported the sibling's B as "faithful as a window". That is true of *whole lines*;
it is **not** true of a row at a **mid-answer moment**. The sibling kept a frame series
(`frames/frame-00000,00003,00010`), and row 023 moves between two consecutive frames:

```
source  A_transcript.txt:393  "## Doubao 说对了一半 —— 但关键那一半是错的，代码可以证明"
source  A_transcript.txt:395  "### 她说对的部分"

row 023 @ frames=65   frame-00003.txt :  ▌ ## Doubao 说对了一半 —— 但关键那一半是错的，代码可以证明
row 023 @ frames=73   frame-00010.txt :  ▌ ### 她说对的部分一半 —— 但关键那一半是错的，代码可以证明
```

The string `她说对的部分一半` **occurs in no source line** (`grep -c` on the transcript = **0**).
It is the new, shorter line "`### 她说对的部分`" (16 cells) written **over** the old, longer line
(56 cells) in place, with the tail of the old line left in the cells the new one does not cover.
Shape: **MID-LINE** (a boundary that exists in neither source line) — the operator's
"吞字少字" signature, on the grid, at a streaming moment, not in the transcript.

**Attributed layer — a wire write sequence, not a repo line.** The session's raw PTY stream for
that turn (`runB/pty.raw`, the same bytes the sibling's grid came from) holds:

```
…\x1b[2Kyou>\r\x1b[5C\xe2\x96\x8c \x1b[24;3H### 她说对的部分\x1b[24;1H\n…
       ^erase-to-EOL on the LIVE row only   ^cursor to col 3, then the new text, NO erase
```

So the band's in-place rewrite writes the new text at column 3 with **no erase-to-end-of-line**,
and the cells right of the new text keep the previous frame's characters. The same three-line
sequence appears in **every** capture the sibling kept (`runB`, `runB2`, `runB3`, `runFinal`), so
it is the renderer's steady behaviour and not a torn byte stream — but the *stitch itself* is
observable only where the shorter line lands on a longer predecessor, which is why the sibling's
chosen evidence (`runFull`, taken at a later frame) does not show it. This is a **candidate
mechanism with a reproduction**, offered for the fix brief; the tool that would name it — 
`gopi ui_state --compare`'s mid-line shape — is reachable (§10.3).

## 10.2 CORRECTION: `01a0ddcd-2b52` IS merged (the §7 claim was true for one minute)

`git merge-base --is-ancestor 18898c72 master` -> **ancestor**. The branch landed as
`73052c0d u task/01a0ddcd2b52771f8fea2c3dc42b5b56` (parents `78767c5b` + `18898c72`) at
**2026-09-26 23:32:12**, while this document was committed at **23:31:09** — 63 seconds earlier.
Both artifacts are on master (`internal/gopilog/response_text_fidelity_test.go`,
`devdoc/log-response-text-count-fidelity.md`), and the three tests it adds PASS:

```
git show master:internal/gopilog/response_text_fidelity_test.go > <pkg>/ && go test ./internal/gopilog/ -run 'ResponseText' -v
--- PASS: TestResponseTextCountRelationshipMatchesTranscript (0.00s)
--- PASS: TestResponseTextBytesExceedRunesForCJK (0.00s)
--- PASS: TestResponseTextPreservesEveryBlockWhole (0.00s)
```

(The file is not in *this* branch's tree, so it must be materialised from master to run there; the
package sources are identical — `git diff HEAD master -- internal/gopilog/` differs only by that
one added file.)

**And the correct lesson is not "check again before reporting".** The claim was measured, correct
when measured, and stale when read: a *merge state* is not a fact an agent can fix in its own
report — it is the ledger's and the merger's to report, and a document that restates it will be
wrong at some future read. A report should name the observation and its timestamp
(`23:31:09: absent from master`), which is what this correction supplies.

## 10.3 Instrument reachability, refined (the `--compare` and `--session` legs)

| invocation | exit | observation |
|---|---|---|
| `gopi ui_state` (no selector) | **2** | `12 exited gopi surfaces could be the target; refusing to guess — pass --pid (or --list)` |
| `gopi ui_state --list` | 0 | `#gopi-ui-state-list scanned=12 roots=6` + the full table (12 rows, each with its dump path) |
| `gopi ui_state --pid <N>` | 0 | the grid of that dump; `state=exited`, `the dump is the LAST frame that surface painted` |
| `gopi ui_state --pid <N> --compare "<line>"` | **1** | `--compare needs the authoritative transcript: pass --session <session-id\|session.jsonl>` — the usage text's "authority is DERIVED from the dump itself" is **not** what the shipped build does |
| `... --session <bare-id>` | 3 | resolves — **but only for a session in the installed binary's own profile**: `a5e1c3e9` resolved, `01a0dd26`/`01a0ddf7` did not (`cannot read the transcript : open : no such file or directory`, **empty path in the message**, exit 1) |
| `... --session <path.jsonl>` | 3 | resolves for ANY session, including another task's; the message names the resolved path |
| `gopi ui_state --all --pid <N>` (no session) | 1 | refused, same reason |
| `--all`, dump audited against its OWN transcript | 3 | `transcript_lines=64 audited=51 skipped=13 matched=0 disagreed=51` (`paint_loss` 26, `wrap_artifact_or_loss` 24, `cjk_wrap` 1) |
| `--all`, same dump against an UNRELATED session | 3 | `transcript_lines=86 … audited=61 matched=0 disagreed=61` — **it does not refuse**: two different authority transcripts produce two different damage ledgers on one grid, with no ownership check |

**The two friction points, stated for the fix brief.** (1) A bare session id is resolved against
the *installed* profile, so a caller in a task sandbox cannot name its own session by id — only by
absolute path; the failure prints an empty path. (2) Neither the audit nor the compare checks that
the dump and the transcript are the same session — measured above: the same dump yields
`61 disagreed / 86 transcript lines` against one session and `51 / 64` against another, both exit 3.
The `--compare` refusal for several matching surfaces is **not** weakened anywhere by this task.

## 10.4 Verdict on the epic's standing question, restated with the new finding

| channel | loss? | which text | shape |
|---|---|---|---|
| **A** `--response --text` | **none** (byte-exact) | — | — |
| **D** `session.jsonl` (authority) | none by definition | — | — |
| **--json** | none (`cmp` identical to D) | — | — |
| **B** (grid) | **YES, at a mid-answer moment** | the tail of a line the band rewrote in place | **mid-line**, streaming-time |
| **B** (grid, whole lines) | window, not a transcript | rows wider than 117 cells (97 of them) have no row | re-wrap + scroll |
| **C** the mouse copy | YES (operator's own file) | per-row head offset | head-of-row |
| **default `gopi log <id>`** | **YES — 94.4%** | 4 of 5 blocks invisible; the 5th capped at 300 runes | whole-block + tail |

**A == D; B is faithful only as a window onto a rewritten grid; C and the default log form lose
text.** The sibling's conclusion is right about the *transcript*, wrong about the *paint*: the paint
can carry a stitch that exists in no source line.
