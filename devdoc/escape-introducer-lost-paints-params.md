<!-- doc-class: historical -->
# Escape-sequence parameters reaching painted cells: the carry policy dropped the introducer

**Task**: `01a0e81b-dc17-7756-b9e3-ad7d4701260f` — "escape sequence fragment
`;61H24;73H5;230m` visible as text in the chat cell grid".
**Branch**: `task/01a0e81bdc177756b9e3ad7d4701260f`.

## The observation, and what it is

Session `884eca28`'s armed chat grid, dump row `002`, verbatim:

```
002|  行号/列偏移，并用 `see` 的 attrs 判断高亮 span 是从反引号;61H24;73H5;230m
```

`;61H24;73H5;230m` is three **placed sequences with every introducer gone**:

| bytes | sequence | first param omitted |
|---|---|---|
| `ESC [ ; 61 H` | cursor placement | yes — hence the run *starts* `;` |
| `ESC [ 24 ; 73 H` | cursor placement | a second one |
| `ESC [ 38 ; 5 ; 230 m` | SGR (palette index 230) | 230 is the grid's colour there |

`grep -c ';61H24;73H5;230m'` over the session's `session.jsonl` returns 11
records and over `chat-screen.dump` returns 9 — not a one-off.

## The instrument came first (and it held up)

Per memory #6439 (suspect the instrument before writing the defect down), two
checks were made **before** any product claim:

1. **The grid is not self-deceiving here.** `internal/lineedit/screen.go`'s many
   "anything else is skipped, never rendered as text" branches do not apply to
   this run: the dump's text (an un-escaped `;`-led parameter run at the end of
   the row) is only reachable through the `default:` arm of `Screen.Feed` — i.e.
   through bytes that were **genuinely not part of a sequence** when the
   interpreter read them. So the grid is reporting a real state of the byte
   stream it was fed.
2. **The 28 MB `.raw.log` is not a pty capture.** It is gopi's raw **HTTP** log
   (`# gopi raw HTTP log`); `d.count(b'\x1b')` is **0** by construction. Its
   `;61H` occurrences are the *transcript* repeating the fragment (the model
   quoting it back), so `grep -c 'ESC[;61H'` = 0 there proves nothing about the
   paint stream. The paint stream's own evidence is the dump and the artifact's
   attrs — this is the same reasoning `screen_chunk_carry_test.go` records.

## Root cause: one file, two policies, one mistake

`internal/lineedit/screen.go` — the vt100 cell grid that the dump sink
(`internal/uiclient/dump.go`, fed **one Write at a time**, i.e. one read
boundary per frame write) and the end-to-end pty tests both interpret the paint
stream with. A terminal is a byte **stream**: the split between two reads is the
OS's choice, never the producer's, so a sequence can be split across a boundary.

The parser holds an un-interpretable tail and re-offers it next call. **Both**
exits from that policy kept a fragment whose introducer had been thrown away:

1. **`holdPending`'s bound was truncating the wrong end.** Past
   `maxPendingEscape` (512) it kept the **last** 512 bytes —
   `tail = tail[len(tail)-maxPendingEscape:]` — which is precisely the half
   *without* the `ESC[`. The parser then re-offered the sequence's **parameters**
   as ordinary characters, and they painted as cells. This is the defect, and its
   shape is exactly `;61H24;73H5;230m`: run the policy over the artifact's three
   sequences and the leading `;` is what a dropped `ESC[` leaves behind.
2. **`Resize` discarded the held tail.** A read boundary leaving the parser
   holding a bare `ESC[` (a real chunk split point), followed by a re-measure
   before the next read, lost the introducer while the parameter bytes arrived
   in the next write.

`internal/sgr`'s `EscapeSeqLen` — "the ONE definition of which bytes draw no
cell" — is correct and was never at fault; it simply cannot classify bytes whose
`ESC` is no longer there. The defect is upstream of it.

### Why the existing tests did not catch it

`screen_chunk_carry_test.go` already asserts "the same stream paints the same
screen at every read size" over a ladder of 1, 2, 3, 7, 13, 64, 333, 4096 — but
its fixtures are short enough that **no** sequence ever exceeds the bound, so the
truncating arm was never reached. The bound's own test checked only that
`s.pending` stays ≤ the bound, never **what** was kept.

## The fix (one policy)

**The parser may hold a partial sequence or discard it; it must never keep a
fragment whose introducer it has thrown away.**

- `holdPending`: past the bound, drop the held tail **whole** (end the held
  state) instead of keeping its last bytes. Correct output has no third option:
  either the whole sequence is consumed, or none of it becomes cells — never its
  parameters as text.
- `Resize`: **re-interpret** the held tail against the new grid instead of
  discarding it (`defer s.Feed(pending)`, after the geometry is measured). It
  cannot double-apply: a tail is held only when it draws nothing yet, and
  re-feeding it either completes the sequence (whose cells were never drawn) or
  holds it again.

## Verification

### The deterministic repro (no pty)

`internal/lineedit/screen_introducer_lost_test.go`, two legs, one per policy,
both feeding **hand-built** frame sequences:

- `TestScreenResizeKeepsAHeldSequenceIntroducer` — 41-col grid, `Feed("ESC[")`,
  real `Resize(40, 4)`, then `Feed(";61H")` + the second placement + the SGR.
- `TestScreenFeedNeverPaintsAnOverlongHeldSequenceAsText` — an unterminated OSC
  (`ESC]11;?`, the query a bubbletea program really sends) carrying
  `maxPendingEscape * len("SEMI;9")` bytes of payload, then ordinary text.

**RED on the pre-fix code, GREEN after** (production reverted to `HEAD`, the new
test kept — the revert-guard proof):

```
--- FAIL: TestScreenResizeKeepsAHeldSequenceIntroducer
    the grid carries a CSI PARAMETER run as literal cells: ";61H\n\n\n"
--- FAIL: TestScreenFeedNeverPaintsAnOverlongHeldSequenceAsText
    grid row 0 carries the held sequence's own payload as cells (";9SEMI;9SEMI;…")
```

Post-fix: both PASS, and `go test ./internal/lineedit/ ./internal/uiclient/
./internal/sgr/` is green (21.06s / 0.65s / 0.002s).

### The same criterion at the surface an agent reads back

`internal/uiclient/dump_introducer_lost_test.go` drives the **real**
`ScreenDumper` (the `--dump-screen` sink) with one Write per read, then reads the
**artifact file** back and asserts no cell run of the sequence's parameters. RED
on pre-fix production — the artifact's own row read
`000|afterEMI;9SEMI;9SEMI;9SEMI;9SEMI` — and GREEN after.

### Executed surfaces (Rule 31)

- `go test ./cmd/agent/ -run TestChatTurnDump_ReadsTheWholeAnswerOnARealPtyWithoutInput`
  — the real 117x26 pty, the real chat layout, the real dump sink:
  **PASS (4.32s)**, `ok github.com/nick/gopi/cmd/agent 4.742s`, child/goroutine
  sweeps clean. That test's fourth leg is exactly the acceptance criterion: the
  artifact must carry no `;[0-9]+H` / bare `;NNNm` run as cells.
- `go build ./...` — clean. `go vet ./internal/lineedit/ ./internal/uiclient/` —
  clean. `gofmt -l` — clean.
- `go test ./internal/standards/` — **RED, pre-existing on master `d1209fe0`**:
  `worktree_root_gate_test.go` names `internal/shot/x11.go:193` and
  `internal/memoryprefetch/standalone_test.go:110`, neither of which this branch
  touches (`git diff --name-only HEAD`). Rule 26's repair (a separate dispatch)
  is the right home for it, not this branch.

## Honest boundary

- **This fix is a proof, not a probability, for the two policies above.** The
  class it eliminates is exactly "a fragment kept without its introducer": the
  interpreter now has only two outcomes for a held tail. What it does **not**
  do is make the two policies unreachable — both still trigger, and a stream
  that never completes a sequence still ends its held state at the bound (that
  is the anti-growth guarantee, unchanged).
- **A sequence whose bytes are genuinely absent is out of scope.** Nothing here
  can recover an `ESC[` that a producer (or a lossy capture) never wrote; the
  interpreter only consumes what arrives.
- **The stale attribution is corrected in three places** — this devdoc plus the
  prose in `internal/uiclient/dump.go`, `cmd/agent/lineedit_wire.go` and
  `cmd/agent/chat_turn_dump_grid_pty_test.go`. Those comments had recorded "a
  frame captured mid-paint" as the cause of the literal cells. A frame boundary
  is where the defect is **reached**, not what produces it: the interpreter reads
  every escape of every frame it is fed. Leaving the old story in place would
  have sent the next reader after the wrong mechanism.
- **The armed-chat re-verification is by proxy here.** The original session
  `884eca28` is a live process, so its dump could not be re-shot post-fix without
  operator input; the criterion was re-run on a freshly built tree through the
  real pty path instead (`TestChatTurnDump_…`, PASS). The next armed chat's dump
  is the confirmation in situ.

## Re-landed 2026-09-29 (the fix had been orphaned)

**This document, its fix and its two tests were lost once and replayed here.** The
commit that carried them on `task/01a0e81b…` was `e06a2aed`; that branch was later
removed with `git branch -D`, which left the commit reachable from **no ref**
(`git branch -a --contains e06a2aed` → empty) and one `git prune` from permanent
loss — it survived only because a previous `gc` had aborted, leaving loose objects
on disk (`git status` warns "too many unreachable loose objects"). Nothing had
reached master by any other route: all 8 paths of `e06a2aed` differed from master.
Master meanwhile carried the defect's own **red** guard,
`TestScreenReconcilesAfterAnOverlongHold` (`internal/lineedit/screen_cell_reconcile_test.go:307`),
whose cause was in no branch.

It is re-landed on `fix/lineedit-escape-introducer-lost` (off master `14836a97`) as
commit `04a540ed`, **message preserved** from the orphan; the `Resize` arm was
re-applied by hand because master had diverged in that function. Semantic
compatibility with `14cde7c6`'s `canBeginRune` resync policy is untouched: that
function decides *whether to advance*, `holdPending` decides *what to keep*.
Replay fidelity was proved by diffing the two commits — identical file set, identical
message through line 35, per-file `+/-` equal except one added **comment** line, and
with comments stripped the `screen.go` hunks are byte-identical.

**The lesson (Rule 9).** A deleted branch leaves no record of the work it held.
`git tag archive/<name>-<date>` before the `-D` costs nothing and makes the work
permanently reachable; "the object is still there" is luck, not a guarantee.

## Files

- `internal/lineedit/screen.go` — `holdPending` (drop whole, never truncate) and
  `Resize` (re-interpret the held tail).
- `internal/lineedit/screen_introducer_lost_test.go` — new, the pty-free repro.
- `internal/uiclient/dump_introducer_lost_test.go` — new, the sink-level repro.
- `internal/uiclient/dump.go`, `cmd/agent/lineedit_wire.go`,
  `cmd/agent/chat_turn_dump_grid_pty_test.go` — corrected attribution prose.
