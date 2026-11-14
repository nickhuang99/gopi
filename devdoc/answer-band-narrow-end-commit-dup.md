<!-- doc-class: historical -->
# The answer band painted one NON-WRAPPING line twice — the turn-end commit carried it from both ends

**Task** 01a0e433 · **branch** `feat/answer-band-narrow-end-commit-dup` · **measured** 2026-09-27/28
**Class** historical (a past run's record)

## Symptom

At turn end one answer line reached the painted band **twice**: whole, then as a
truncated prefix of itself, while `session.jsonl` held the line once. The line was far
narrower than the band, which is why the search did not start where the previous
duplicate devdoc (`answer-band-wrap-carry-dup.md`) points: **this is not the wrap-carry
family.** It needs no wrap at all.

The operator's reading, reproduced end to end on the real binary (below): the line
`L09: 09-00 … 09-05 end` reached the grid as `… 09-05 end` and then, on the next row,
`… 09-05 en` — the same sentence again, cut by the row's width.

## The mechanism

`AnswerRenderer` paints the answer into a reserved *band*: content is written into a
live row, and a row is only **committed** (pushed into scrollback) when the line it
belongs to is known to be complete. Two registrations of the in-flight line therefore
coexist at turn end:

| registration | what it holds | why |
|---|---|---|
| `compPrefix` (the **park**) | the line's text as the burst that was **withheld** left it | a swallowed burst is parked so it can be re-queued, not lost |
| `pending` → `tail` | the painted continuation after the burst boundary | the tail of the flush window, drained into the commit |
| `liveText` | what the paint last **drew** on the live row | used by the commit to measure the row it must clear |

`Finish` (**the turn-end commit path**) then composed the committed line and asked one
question before appending the live row's text again — *is it already queued?* — and
asked it with a **suffix test over `prefix + park` only**:

```go
queued := r.prefix + r.compPrefix
if !strings.HasSuffix(strings.TrimSuffix(queued, "\n"), r.liveText) { r.prefix += r.liveText … }
…
r.prefix += tail
r.prefix += r.compPrefix        // the park appended LAST
```

Three facts compose the defect:

1. **The suffix test was satisfied.** `liveText` is the park's text, so the queue *ended*
   with the shown text — the test concluded "already queued" and appended nothing.
2. **The tail was not in that test at all.** `tail` arrives from `pending` and is in no
   other registration; it is appended after the test.
3. **The park was appended after the tail.** So the committed line came out as
   `tail + park` — and since `park == liveText`, that is *the line's own end glued in
   front of the whole line*: the sentence twice, with the second copy starting mid-line
   and cut by the band row's width.

Order is the whole defect. With one cadence (a break inside the last delta) the park is
empty and the composition is correct; with another (the boundary inside the last line)
the park holds the line, and the append order inverts it. That cadence-dependence is
what made it look like a flaky paint rather than a composition bug.

## The fix

`internal/lineedit/answer.go` — the carry's question now accounts for the whole commit
it is about to write, and the two halves go in the order the **line** has them:

- the "already queued" test is a named helper, `carryAlreadyQueued(queued, park, shown)`
  (`emphasis.go`), which answers yes when the shown text ends the queue **or ends the
  park** — the park being queued as the line's earlier half;
- the park is appended **before** the tail, so the committed line reads beginning then
  continuation.

`liveText` is deliberately **not** cleared when it is already queued: `commitLines`
measures the provisional paint through it to clear cells a wider literal spelling left
behind. Clearing it would trade this bug for residue on committed rows.

## Verification

- **Build:** `go build ./...` — OK
- **Tests:** `go test -count=1 ./internal/lineedit/` — `ok github.com/nick/gopi/internal/lineedit 21.3s` (whole package, fix applied)
- **RED proof (pre-fix tree, tests present):** reverting only `answer.go` + `emphasis.go`
  to `HEAD` and re-running the new tests —
  ```
  --- FAIL: TestAnswerBand_NarrowLineIsNeverPaintedTwice (0.24s)
      --- FAIL: …/finalBreak=false (0.02s)
          dup: a 12-cell run of the band occurs TWICE on the grid — the operator's
          partial repeat, on a line that never wraps
  --- FAIL: TestAnswerBand_NoCellRunOccursTwice (0.25s)
  --- FAIL: TestAnswerBand_NarrowTurnEndCommitRereadsTheLineOnce (0.02s)
          the parked line's head is committed 2 times, want 1: Finish's carry queued the
          text the live row already shows ON TOP of the park that holds it
  FAIL
  ```
  All three are GREEN with the fix; the control arm (`finalBreak=true`, the last line
  carrying its terminator) passes on both trees.
- **Ran (end to end, real binary, real PTY):** two binaries built from the **same tree**,
  differing only by this fix — `gopi-before` (files reverted to `HEAD`) and `gopi-after`,
  each `CGO_ENABLED=0 go build -o … ./cmd/agent/`. A stdlib-only SSE stub served the
  answer one byte per `text_delta` (the cadence whose boundary falls inside the last
  line); a 100×30 PTY drove `gopi chat --dump-screen=…` with a private sandbox
  `GOPI_HOME`, waited on the observed token, typed `hi`, and dumped the grid.
  - `gopi-before` — reading 1022 cells, the line's head appearing **2** times:
    ```
    ▌L08:08-00 … 08-05 L09:09-00 09-01 09-02 09-03 09-04 09-05 end L08:08-00
    -0108-0208-0308-0408-05 L09:09-00 09-01 09-02 09-03 09-04 09-05 en
    ```
    the whole line, then the same line cut by the row's width — the operator's shape.
  - `gopi-after` — reading 962 cells, head appearing **1** time:
    ```
    ▌L09:09-00 09-01 09-02 09-03 09-04 09-05 end
    ```
  - The `─`-rule rows make a naive 12-cell repeat scan noisy; the decisive index is the
    line's own head count (2 → 1), asserted on the dump's cell rows, not on the
    renderer's byte stream.
- **Artifacts:** the two dumps, the PTY transcripts, the stub and the driver are under
  this run's session dir `…/sessions/01a0e433-fe9f-7389-82a2-69cf38ffff11/e2e/`.

## Tests shipped

`internal/lineedit/answer_band_narrow_end_commit_test.go` — the neighbouring
`answer_wrap_scroll_dup_test.go` states in its own header that every line of its fixture
must wrap; **this defect is invisible to a wrapping fixture**, which is the reason the
case is a separate file and why its own fixture keeps every line well under the band.

- `TestAnswerBand_NarrowLineIsNeverPaintedTwice` — the minimal repro: one narrow answer,
  swept over **every delta 1..64** × both turn-end shapes (last line terminating /
  mid-line), asserting on the **grid** (replayed into the package's vt100 `Screen`) that
  the committed reading is the transcript and that no 12-cell run occurs twice.
- `TestAnswerBand_NoCellRunOccursTwice` — the general guard: for every cadence and both
  shapes, no run of cells repeats **and** every reading row is a contiguous piece of the
  transcript (so a reorder cannot hide behind a per-row assertion); it fails if the sweep
  did not judge every cadence.
- `TestAnswerBand_NarrowTurnEndCommitRereadsTheLineOnce` — the same property one level
  down, at the seam: it **asserts the precondition** (park, live row and pending tail all
  non-empty) so the case cannot silently stop exercising the carry, then asserts the
  parked line's head is committed exactly once.

## Not Done (honest)

- The `─`-rule repeat remaining in the corrected end-to-end reading was **not** chased to
  a verdict in this session: the A/B above proves this defect is gone by the line's own
  head count, but a duplicate of a box-drawing glyph elsewhere in the grid would not be
  caught by that index. It is the fixture's opening paragraph being re-painted across the
  scroll boundary, and it is unverified — filed as an observation, not a claim.
- The `park == liveText` equality is measured but not proven for every path that can set
  `compPrefix`; the guard asserts the property (no repeated run, head once) rather than
  the equality, so a differently-shaped park would still be caught.
