<!-- doc-class: normative -->
# The answer band loses a line break when the break arrives alone in its frame

**Rework task 01a0e57d** · branch `rework/answer-linebreak-cadence-pty-guard` ·
recovered from the deleted branch `fix/answer-linebreak-cadence` (tip `e206b5ed`,
task 01a0e54f), whose CODE FIX was superseded by master and is not reintroduced
here.

This document is the record of THREE runs, in order: the fix branch that
measured the defect and wrote the first pty guard, the rework that kept the pty
guard and the record when master landed a different fix for a neighbouring
defect, and **the fix itself** (task `01a0e706`, branch
`task/01a0e7060fb87b6fab7298729a0b2086`), which is the one that closed the defect
the first two left open and is recorded in the sections marked `01a0e706`. Where
the earlier runs disagree with it, the disagreement is named and kept rather than
edited away. The
standing claim about master lives in `devdoc/line-break-lost-at-launch-cadence.md`
and in `internal/lineedit/answer_linebreak_cadence_test.go` (which cites it); this
file records the observation, the measurements and the parts that did not carry
over.

## The defect, in one sentence

`AnswerRenderer.drain` (`internal/lineedit/answer.go`) splits its buffer at the
**last** line break and commits only what is **before** it; when a delta frame
*is* the break (the frame's whole content is `"\n"`, the ordinary shape of a
1-byte stream — every character is a frame of its own), the committed body is
empty, `commitPrefix` returns at its first line (`if r.prefix == "" { return }`),
nothing is promoted, and the line the live row is already showing stays **live**.
The next line's text is then appended to it (`writeLive` appends at `liveCol`), so
several answer lines end up in ONE band row.

The reader therefore sees a different answer — a paragraph instead of a list —
decided by nothing but the carrier's chunk size.

## Measured, cadence by cadence (the fix branch's measurement)

| arm | carrier frames | unfixed tree | fixed tree |
|---|---|---|---|
| `slow-1` | 1 byte per frame, 40 ms apart | **1 row**: `AAALINEBBBLINECCCLINE` | **3 rows**: `▌ AAALINE` / `▌ BBBLINE` / `▌ CCCLINE` |
| `fast-4` | 4 bytes per frame, 40 ms apart | 3 rows (control — the break rides in a frame with the next line's first characters, so the commit lands) | 3 rows |

Both arms were measured with the shipped binary on a real pty at 64×24, driven by
a local Anthropic-SSE carrier that emits one `text_delta` per frame:

```
gopi chat --dump-screen=<evidence>/drv/chat-screen.dump        # 64x24 pty, TIOCSWINSZ
# delta=1:  unfixed 007|▌ AAALINEBBBLINECCCLINE
#           fixed   007|▌ AAALINE  008|▌ BBBLINE  009|▌ CCCLINE
# delta=4:  unfixed 007|▌ AAALINE  008|▌ BBBLINE  009|▌ CCCLINE (the control arm)
```

The same defect, from the other side of the family, was measured earlier by the
render-verdict harness (`testsuite/chat_render_redzone_pty_test.go`, task
01a0e44e) on a five-line answer: at delta=1 five lines came back as one wrapped
paragraph with the close and tail markers glued onto one row, while delta=4 kept
every break — which is what put this defect on a measured screen in the first
place, and what `devdoc/line-break-lost-at-launch-cadence.md` records as the
standing claim.

## The fix the branch made — and why it is NOT in this branch

The branch's fix promoted the row a break terminates, from inside `drain`:

```go
if body == "" && r.liveText != "" && !r.tableHolding() {
    body = r.liveText
}
r.routeBody(body)
r.commitPrefix()
```

**That condition is deliberately absent from master and from this rework.** Master
landed a DIFFERENT fix for a neighbouring defect on the same seam — the turn-end
carry, `carryAlreadyQueued` (`internal/lineedit/emphasis.go`), reached from
`finishNow` — whose subject is the *partial repeat*: an in-flight line that
reaches Finish in two registrations (the park `compPrefix` and the painted tail)
and is committed twice, as `tail + park`. Master's mechanism settles what the
turn-end commit assembles; it does not promote a row whose break arrived alone.
The two are not interchangeable, and reintroducing the branch's promotion would be
a second, competing owner of the same decision — the reason the rework brief
forbade applying the branch's diff.

The consequence is visible in the rework's own measurement (below): **the defect
is still open on master.** The pty guard therefore carries the same RED gate the
in-package guard carries, so the suite stays green on a tree where the defect is
live, and both guards go ungated in the commit that fixes it.

## Status on master, MEASURED by the rework (2026-09-28)

The rework did not take the epic's premise on trust: it re-measured the cadence
through the surface. Master's `drain` still splits at its last break and still has
no promotion for an empty body (there is no `body == ""` condition in
`internal/lineedit/answer.go`), and `carryAlreadyQueued` is reached only from
`finishNow` — the TURN-END carry, not the per-delta commit. The defect is
therefore **OPEN on master**, and the rework's own pty run says so:

```
$ GOPI_LINEBREAK_CADENCE_RED=1 GOPI_LINEBREAK_CADENCE_EVIDENCE=<session>/evidence \
      go test ./testsuite/ -run TestChatPTYAnswerLineBreaks_SurviveEveryStreamingCadence -v -count=1
--- FAIL: …/delta=1 (11.71s)
    cells: ["▌ AAALINEBBBLINECCCLINE"]
--- PASS: …/delta=4 (12.55s)
```

All three lines reached the terminal in ONE band row (`▌ AAALINEBBBLINECCCLINE`)
while the 4-byte control arm drew one row per line — the cadence, and nothing
else, decided the row structure. Run on the branch tip (after the delivery-point
sync, master `56f919d9`); an earlier run of the same guard on the same tree glued
two of the three (`["▌ AAALINEBBBLINE" "▌ CCCLINE"]`), because the frame boundary
is a timing boundary — which is the branch's own recorded finding below. That is
the branch's measurement reproduced on master's current `answer.go`, by the very
instrument this file is.

**So both guards carry the gate, and neither is ungated by this commit.** The
in-package case (master's, the authority) skips with it unset and fails its
`advance = two windows` arm with it set; this pty case skips with it unset and
fails its `delta=1` arm with it set. They go ungated together, in the commit that
fixes `drain`. A guard that failed on the tree it landed on would make the suite
red without pinning anything — which is why the gate exists, and why removing it
is part of the fix rather than part of this rework.

**THAT COMMIT IS `01a0e706` (2026-09-28), and this section is its record.** The
fix is in `drain` (`internal/lineedit/answer.go`): the row a line break TERMINATES
becomes the commit's body when the split leaves the body empty —
`if body == "" && r.liveText != "" && !r.tableHolding() { body = r.liveText }` —
so the finished line travels the ordinary `routeBody` + `commitPrefix` path, the
composer is asked about the whole line, and `headIsInRow` prefixes nothing
because the row's decided text already opens with the head. The guard against
`looksPipedRow(r.compPrefix)` is a HOLD, not a decision: a parked pipe row is the
unfinished first half of a line whose fate the row after it decides, and
promoting that paint is the table defect this package already has a test for.
Both gates (`GOPI_LINEBREAK_CADENCE_RED` in this package's guard, and the copy of
the name the pty guard carried) are **removed**; the pty guard also takes the name
the epic's acceptance names, `TestChatLinebreakCadencePty`.

The measured transition, through the surface (Rule 31):

```
# BEFORE — HEAD's answer.go, binary built from it:
$ GOPI_BIN=<session>/bin/gopi-red GOPI_LINEBREAK_CADENCE_EVIDENCE=<session>/evidence/red \
      go test ./testsuite/ -run TestChatLinebreakCadencePty -count=1
    --- FAIL: …/delta=1 (11.69s)
        delta=1: the answer occupies 1 dump row(s), want 3 (one per line):
            cells: ["▌ AAALINEBBBLINECCCLINE"]
    --- PASS: …/delta=4 (12.50s)

# AFTER — the fix restored, binary rebuilt:
$ GOPI_BIN=<session>/bin/gopi-fixed GOPI_LINEBREAK_CADENCE_EVIDENCE=<session>/evidence/green \
      go test ./testsuite/ -run TestChatLinebreakCadencePty -count=1
    --- PASS: …/delta=1 (13.20s)
    --- PASS: …/delta=4 (12.50s)
```

and the same transition on the dump itself, which is the screen the reader saw:

```
red/cadence-delta1-screen.dump    019|▌ AAALINEBBBLINECCCLINE
green/cadence-delta1-screen.dump  016|▌ AAALINE   017|▌ BBBLINE   018|▌ CCCLINE
red|green  cadence-delta4-screen.dump  016|▌ AAALINE   017|▌ BBBLINE   018|▌ CCCLINE
```

## What the rework kept, and in what shape

| branch artifact | rework decision | why |
|---|---|---|
| `internal/lineedit/answer.go` forced promotion | **dropped** | superseded by master's `carryAlreadyQueued` path; two owners of one decision |
| `internal/lineedit/answer_linebreak_cadence_test.go` (branch's copy) | **dropped** | master already has the file under the same name — it IS the authority, and a copy would have replaced it |
| `internal/lineedit/answer_band_narrow_end_commit_test.go` (20-line precondition) | **dropped in the rework — applied by the fix (`01a0e706`)** | the rework's reasoning was that master's mechanism "does not [commit the row at its break], so the precondition still holds: the case PASSES unmodified". That was true of master and is FALSE of the fixed tree, which is exactly the same reason the branch gave for editing it — measured: with the fix in, the case fails on its own precondition (`park="L09：09-00 … 09-05 尾" live="L09：09-00 … 09-05 尾" pending=""`, i.e. the pending tail is gone because the break committed the line). The edit removes the third term (`r.pending.Len() == 0`) and keeps the two the carry's question needs; the assertions below it are unchanged, and the case passes with it and fails without it. The rework's conclusion was right about the tree it measured and wrong about the tree the fix produces — recorded rather than quietly overwritten, because the difference is the whole finding |
| `testsuite/chat_linebreak_cadence_pty_test.go` | **kept and re-authored** | the surface instrument, and the only artifact here that measures the defect end to end. Symbols renamed (`lblCadence*`); the private carrier replaced by the shared `testutil.NewMockLLM` cadence (`Resp.TextDeltaBytes`/`TextDeltaDelay`), so the file adds no second SSE writer and the streamed sizes are the stub's own record |
| `devdoc/DOC-INDEX.tsv` row | **kept (union)** | written by the corpus's own classifier, not by hand (Rule 36) |

## The regression guards, and the run that proves each one

| guard | command | observed |
|---|---|---|
| in-package (master's, AUTHORITY) | `go test ./internal/lineedit/ -run TestAnswerLineBreaks_SurviveEveryStreamingCadence -count=1` | **UNGATED** by `01a0e706`: both arms PASS. Before the fix, on master, the same case behind `GOPI_LINEBREAK_CADENCE_RED=1` FAILED its "break arrives alone in its frame" arm — `1 band row(s): ["AAABBBCCC"] → dense "AAABBBCCC" (want 3 row(s), "AAABBBCCC")` — while the "break rides with its line" arm PASSED; ungated it skipped, because the tree was unfixed |
| real surface, both cadences (this file) | `GOPI_LINEBREAK_CADENCE_EVIDENCE=<dir> go test ./testsuite/ -run TestChatLinebreakCadencePty -v -count=1` | **UNGATED** by `01a0e706`: delta=1 and delta=4 both PASS, 3 rows each. The RED half is reproduced by running the same guard against a binary built from `answer.go` without the promotion — see the measured transition above |
| the narrow turn-end case, precondition re-shaped by the fix | `go test ./internal/lineedit/ -run TestAnswerBand_NarrowTurnEndCommitRereadsTheLineOnce -count=1` | `--- PASS`; with the fix in and the `r.pending.Len() == 0` term left in place it FAILS on its own precondition (the pending tail no longer exists — the break committed the line) |

## Pitfalls paid for by the branch's run (for the next runner)

1. **A private pty harness must DRAIN the pty continuously.** A reader that stops
   once its marker arrives leaves the master's buffer full, the child then blocks
   on write, and it does not exit when asked (measured: `chat did not exit within
   60s`, answer half painted). The rework deletes the private harness for this
   reason — `testsuite/chat_pty_test.go`'s `ptySession` already drains on its own
   goroutine.
2. **Back-to-back bytes are a PASTE to the editor.** Writing a question as one
   `WriteString` suppresses the submit Enter that follows
   (`lineedit.PasteBurstCharInterval`): the turn never starts and the wait for the
   answer times out against a healthy binary. Use the harness's `writeGapped`
   (spaced beyond the burst window) and pause before the Enter.
3. **The prompt marker is already in the buffer** when the second wait starts, so
   a substring wait returns at once and proves nothing — wait on an OCCURRENCE
   COUNT (the re-rendered prompt is the second one).
4. **A row's text is not guaranteed contiguous in the pty byte stream** (each
   styled span is written with its own cursor placement), so a pty-side assertion
   must strip escape sequences first; the dump is the better instrument, and the
   one this guard asserts on.
5. **The dump is the artifact of a specific SURFACE** — `gopi chat` writes
   `chat-screen.dump` (`uiclient.ChatDumpFileName`) and the tool's own reader
   refuses a file whose name belongs to no surface. Name it; do not invent one.
6. **The pty geometry must be re-checked through the kernel after it is set**
   (`lineedit.TerminalCols`): a case that asserts on a grid whose width it only
   *asked* for measures the default geometry when the ioctl silently fails.
