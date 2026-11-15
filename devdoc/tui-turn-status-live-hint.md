<!-- doc-class: normative -->
# devdoc: the chat status row's in-flight hint (Defect A) and the emphasised row cut (Defect B)

Two operator-reported `gopi chat` TUI defects, fixed in one session as two
commits. Both were reproduced first and both carry a test that fails on the
pre-fix code.

## Defect B — a bold span cut by the row wrap

**Symptom (operator):** an intra-line `**bold**` span rendered as a bold
*prefix* or *suffix* of a word instead of the whole word.

**Reproduction:** a line whose bold span is longer than one display row. The
defect is a **bookkeeping** one and the evidence is per-CELL: the assertion reads
the cell grid the editor's own bytes produce (`Screen.StyledLine`, i.e.
`screenattr.go`'s SGR parser) and asks of each cell whether it is bold.

```
internal/lineedit/emphasis_cut_test.go  (pre-fix)
row 9 col 11 ("x"): Bold = true, want false — the cell's emphasis does not follow the span it is in
… cols 11..39 …
```

`x` is the PLAIN run that follows the bold span: the cut had put a run of plain
characters under the span's emphasis.

**Root cause.** The row cutter (`writeRow`'s `displayCells`/`cutCols` arithmetic)
is arithmetic on the row's **plain characters**, but `answerRow.piecesFor` was
given only the row text and walked the row's spans from their FIRST byte — for
every row. After a cut the second row's text is a SUFFIX of the row text while
the spans still index the WHOLE text, so the walk re-emitted the span's head over
the next row's characters and then handed the remaining characters the following
span's style. The two effects are the operator's prefix (span's tail emphasised
over plain text) and suffix (the span's own characters painted plain).

**Fix.** `answerRow` carries `skip` — how many bytes of its own text earlier rows
have already drawn — and `writeRow` advances it by the `head` it cut off
(`skip += len(head)`). `piecesFor` starts its span walk at `skip`, so a span the
cut ended is not re-opened and a span the cut began is emphasised from its own
first byte. The field is zero for the first row and for every row with no spans,
so the unemphasised path is byte-for-byte what it was.

**What was NOT changed.** The cut arithmetic itself. The bug was never the cut;
it was that the pieces were generated as if no cut had happened.

## Defect A — the status row's live hint during a turn

**Symptom (operator):** after submitting a message the row kept reading the IDLE
value (`ready`) while the turn was in flight, so nothing on screen confirmed the
message had been sent; the only evidence the turn had finished was the answer
appearing.

**Reproduction (production wiring, no keystroke after Enter):** the pre-fix code
painted the row ONCE at turn start, with one in-flight word.

```
cmd/agent/chat_stream.go  (pre-fix, driven through the REPL's own path)
the ONE in-flight word, painted once per repaint opportunity
```

Two facts make that a defect rather than a cosmetic complaint:

1. a state painted only when it CHANGES sits on that value for the whole wait, so
   the operator gets no continuous assurance — one lost wake leaves the row on
   the PREVIOUS value (measured: the idle word while the turn was in flight);
2. one word cannot distinguish three different waits: the request just issued
   (nothing back yet), the model reasoning, the model answering.

The `chatStatusSource.status()` body also had a **bug of its own**, found while
reproducing: it computed the live hint and THEN appended the queued suffix, so
the row's live field was `hint` rather than `hint + StatusSep + queued` — the
queued note never reached the row. Removed.

**Fix, in three parts.**

1. **States, not a word.** `chatTurnPhase` (`chatTurnIdle` → `chatTurnSent` →
   `chatTurnThinking` → `chatTurnResponding`) and three state words
   (`sent` / `thinking` / `responding`), each tied to the reserved live column by
   `TestStatusLiveWordsFitTheirColumn` (the column is a FIXED width: a word that
   outgrew it would push the row's later columns sideways).
   The words carry a **no-break space** gap rather than the row's own separator:
   `StatusSep` is the field boundary the aligned layout and the narrow-terminal
   field drop both split on, so a field VALUE that carried one would be cut in
   half. That is also why the animation sits behind the gap instead of in front
   of it.

2. **A cadence, not a single paint.** `chatTurnStatus` owns the turn's phase and
   a `time.Ticker` (`statusLiveKeepalive`) that repaints the in-flight row; each
   frame ADVANCES an animation cell
   (`statusLiveAnimationFrames`), which is what makes the frame a *real* repaint
   — `AnswerRenderer.Tick` deduplicates a row whose composed text did not change,
   so a keepalive that did not move would be a wake that draws nothing. The phase
   changes are driven by the callbacks that already carry the events
   (`ag.OnThinking` → thinking, `ag.OnText` → responding) and by
   `chatTurnStatus.begin` → sent.

3. **One writer, still.** Every change is a `PublishStatus` (a flag plus a wake);
   nothing in the tracker touches the terminal. `end()` closes the ticker AND
   waits for the goroutine, so "the animation stopped with the turn" is a fact by
   the time the caller returns — an animation surviving its turn is the repaint
   storm the fix must not introduce.
   The OWNERSHIP of the state fields moved into `chatStatusSource`
   (`beginTurn`/`setPhase`/`endTurn`/`advanceFrame`): one owner for one set of
   fields, and the phase/frame can never be read against a later `live` count.

**Geometry.** The animation has TWO frames, not three: `responding` (10 cells) +
the field gap (1) + one animation cell = 12, exactly `statusLiveCol`. That is not
a coincidence to be re-derived by hand later — it is what
`TestStatusLiveWordsFitTheirColumn` asserts, and the value it forced is recorded
at the constant.

**The three tests.**

- `TestChatLiveHintWalksTheTurnStates` (cmd/agent) — the ROW, read off the grid
  the read loop painted, must walk sent → thinking → responding → idle, each
  in-flight phase repainted at least twice (the state's own draw plus a keepalive
  frame behind it), the phase rows actually differing from one another, and no
  repaint after the prompt returns.
- `TestChatTurnPhasesWriteNothingFromTheCaller` — the single-writer invariant at
  the REPL's seam: with the read loop STOPPED, the turn's own status calls write
  ZERO bytes and leave the publish flag set. Measured as a delta against the
  recording writer after `reset`, with the loop's exit waited for — not by
  snapshotting the cumulative byte count, which the loop's own startup traffic
  would have failed.
- `TestChatPTY_LiveHintShowsTheTurnInFlight` (testsuite) — the end-to-end surface
  on a real pseudo-terminal, driven the way the operator drives it and with NO
  keystroke after Enter. The mock model is HELD mid-stream
  (`testutil.Resp{Hold: true}`), so "the row says in-flight while the model has
  not answered" is deterministic instead of a race against a fast mock; the
  harness waits for the state word WITH an animation cell behind it, which is
  what proves the KEEPALIVE repainted the row.

## Evidence

The static grid captures are `gopi chat --dump-screen` artifacts and the
BEFORE/AFTER test output lives in the report's `## Verification`; the raw
pre-fix failure lines are quoted above.
