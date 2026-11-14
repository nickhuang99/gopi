<!-- doc-class: historical -->
# The live answer band loses an answer's line break when the break arrives in its own frame

**The standing claim: master's live answer band violates the rule below, and the
defect is OPEN.** The fix is NOT in this branch; it is dispatched as
`01a0e54f-5bdc-7a70-bf90-45ea5b0c7cec`. This document is the authority `internal/lineedit/answer_linebreak_cadence_test.go`
cites — the gated RED repro whose skip message points here — so a future reader
starts from the mechanism, not from the symptom.

## The observation (render level, real pty — never OCR)

Measured by `testsuite/chat_render_redzone_pty_test.go` on a real pty (64×40) at two
streaming cadences, reading the painted cell grid (`--dump-screen` →
`lines_begin`/`attrs_begin`). At **1 byte per SSE frame**, five scripted answer lines
came back as one wrapped paragraph with the close and tail markers glued onto one row:

```
021|▌ RZ-NOWRAP one plain row that ends short.RZ-WRAP HEAD the empha
022|  sised run is wider than the band: ALPHA BRAVO CHARLIE DELTA EC
...
025|▌ <RZ-CLOSE<RZ-END>
```

At **4 bytes per frame** the same client, payload and terminal keep every break
(rows `020`..`025`, one line each). The cadence, not the payload, decides.

## The mechanism (read from the code, not inferred)

`AnswerRenderer.drain` (`internal/lineedit/answer.go`) flushes only when the cadence
window has elapsed — `answerFlushWindow = 30 * time.Millisecond`, or
`answerMaxBufferedBytes = 512` — and then **splits the buffer at its LAST break**:

```go
last := strings.LastIndexByte(text, '\n')
body := text[:last]      // routed + committed
tail := text[last+1:]    // painted live onto the CURRENT row
```

So the break does two jobs at once: it terminates the finalized half **and** it is
the only signal that commits the text already painted on the live row. When a delta
carries the break *together with* its line's text, the window fires on that delta
and the split lands correctly. When the break arrives **alone in its own frame**,
the flush window has already fired for the preceding text, that partial line is
sitting on the live row, and the break's frame splits to an **empty body** — nothing
is committed, and the next line's text is painted onto the same row. One row then
holds many lines.

`liveText` (the renderer's record of what the live row already shows) is the carry
that a later commit would use; the empty-body split is what stops it from being
reached. The wide cadence used by the merged harness
(`testsuite/chat_render_verdict_pty_test.go`, the legacy two-half `TextSSE`) never
lands a break in a frame of its own, which is why the defect was on no measured
screen before.

## The rate — it is a TIMING boundary, so it is intermittent

The frame boundary is decided by socket batching, so the shape is a coin toss the
cadence biases. Counted over every artifact the measuring session kept:

| cadence | artifacts showing the glued row | artifacts kept |
|---|---|---|
| 1 byte/frame, 15 ms apart | **3** | 9 |
| 4 bytes/frame, 10 ms apart | **0** | 9 |

An operator therefore sees it as an intermittent corrupt row — which is how the
original sighting arrived. **A green real-pty run does not mean the defect is
gone**; only the deterministic repro below decides it.

## The rule this violates

**The number of band rows an answer occupies is a property of the answer's own line
breaks, never of the rate the model chose to stream it at.** A line shorter than
the band must end its own row; the same payload must occupy the same rows at any
cadence.

## The minimal repro (RED on master, gated)

`internal/lineedit/answer_linebreak_cadence_test.go` →
`TestAnswerLineBreaks_SurviveEveryStreamingCadence`. The smallest sample that
separates the two regimes: `"AAA\nBBB\nCCC\n"` at 64 columns must occupy **3** band
rows. With each delta flushed on its own it occupies **1**.

```bash
GOPI_LINEBREAK_CADENCE_RED=1 go test ./internal/lineedit/ \
  -run TestAnswerLineBreaks_SurviveEveryStreamingCadence -v
```

The two subtests discriminate by construction, which is the proof the cadence alone
is the cause: `the_break_rides_with_its_line` (advance = a quarter window)
**PASSES**, `the_break_arrives_alone_in_its_frame` (advance = two windows)
**FAILS** with `1 band row(s): ["AAABBBCCC"]` (want 3). Ungated it SKIPs, so master
stays green while the repro stays runnable.

## Blind spots stated, not scored

- An answer whose **every** line wraps past the edge and loses a break is invisible
  to any spacing-insensitive comparison of the cells — only the short-line form is
  decidable at render level, so only it is asserted.
- The guard's artifact is the committed screen at teardown (throttled ≥100 ms): a
  partial that was wrong and then repainted correctly leaves no trace.
- One geometry (64×40), one payload, deltas 1 and 4; 5..16 are not swept.

## Related

- `devdoc/answer-band-wrap-carry-dup.md` — the carry/dup family on the same live row
- `devdoc/answer-row-cut-vocabulary.md` — the right-edge cut vocabulary
- `tasks/01a0e44e-render-redzone-report.md` — the measurement run's full report
