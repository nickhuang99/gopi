<!-- doc-class: historical -->
# ui_state `--compare`: the wrap join, and the prefix in the line's own space

Task `01a0e4f5f77e7483961ba465b3c6cf78`. Branch `task/01a0e4f5f77e7483961ba465b3c6cf78`.

## The reported defect, and what it turned out to be

Measured on the kept dump
`/home/user/.gopi/sessions/01a0e49e-6730-755a-a6ed-11dc0dfa5d96/` (a real 64x34 pty):

```
$ gopi ui_state --compare "<一个 102 汉字的答案行>" --target chat
#gopi-ui-state-compare ... outcome=disagreed match=false
  mismatch grid_row=-1 in_grid=false in_transcript=true damage=text_wrapped
  display_hits=0 geometry=layout cols=64 rows=34 frames=271 prefix=36
  reason="the line is on the grid WRAPPED (its whole text, 102 runes, is a
          continuation of what the first row opens with): it arrived, ..."   EXIT=3
```

Three facts were inside that ONE record, and they disagreed with each other:

1. **The status contradicted the reason.** The reason said the text arrived (wrapped);
   `status=mismatch` + exit 3 said a layer lost it.
2. **`prefix=36` was not a prefix of the line.** The line is 102 runes; the grid
   carries all 102, reassembled by the join from rows 10..12. 36 is the FIRST ROW's
   own text length (the row is `▌ ` + 34 runes of the line, 36 runes all told) — a
   length in the ROW's space, printed as if it were a measurement of the line.
3. **The two faces contradicted each other.** The SAME artifact's whole-answer audit
   (`gopi ui_state --all --target chat`) counted this line `rejoined=1` — arrived —
   with `damage=` empty, while `--compare` called it a loss.

## Root cause (5-why)

- *Why did `--compare` report a loss?* No single grid row carries the line, so
  `rowCarriesLine` did not fire and the verdict fell through to the loss branch.
- *Why did the loss branch not recognise the wrap?* The wrap question was never asked
  on that face.
- *Why not, when the ledger asks it?* The join instrument (`compareSequence`) was
  wired into `AuditAnswer` only. `classifyLostLine` — the ONE classifier both faces
  call — reached its `text_wrapped` answer through `wrapOnlyFits`, which is guarded
  by `displayWidth(line) >= grid.Cols`: **a width test standing in for a fact the grid
  can be asked directly**. On the reported dump the line came back `text_wrapped`
  (so the guard passed and the class was right), but the one-line face printed a
  MESSAGE that contradicted it.
- *Why was the prefix wrong?* `GridPrefixRunes` was filled from `sharedRunePrefix`,
  called on **the current row's text**: a measurement of the row, reported as if it
  were a measurement of the line. On a wrap the row the line opens on holds only the
  line's head and stops where the terminal wrapped, so the two spaces coincide only
  by accident of the wrap point.
- *Why did nobody notice?* Both numbers were plausible in isolation: 36 looks like
  "the band truncated it", and the class looked like an internal detail beside a
  status a script branches on.

## The fix

**One oracle, asked by both faces** (`internal/uiclient`).

- `CompareTranscriptLine`'s `case v.InTranscript` now ASKS `compareSequence` (the ONE
  wrap instrument, already used by `AuditAnswer`). When the grid reassembles the
  line's whole text from consecutive rows the verdict is `CompareMatch` with
  `Rejoined=true`, `WrapRows=[10 11 12]`, `GridRow=<first row>` and the reason
  `wrapArrivalReason(rows)`. When the join does not reproduce the text exactly the
  verdict stays a mismatch — **the loss direction is untouched**: the join must
  REPRODUCE the line, so a line the grid did not reassemble is still reported.
- `GridPrefixRunes` is now the longest prefix of the LINE the grid carries, in the
  line's own rune space: the best of two readings of the same grid —
  `joinedPrefixRunes(rows, start, line)` (the rows from here on, read the way the
  join reads them: `auditWrapRow`, i.e. marker column and the continuation marker
  column removed) and `sharedRunePrefix(rowText, line)` for the single-row case.
  Neither can exceed the line, because each is a prefix of it, so the diagnostic
  cannot over-claim: it reports a genuine truncation and reports a full line as full.
- `classifyLostLine` no longer answers the wrap question at all — it is a name for a
  MEASUREMENT, and the wrap has one: the grid's own answer. `wrapOnlyFits` is gone
  (its width guard was the proxy that produced the defect). `DamageTextWrapped`
  remains declared (the vocabulary a reader greps for) but is no longer emitted: a
  line the join reassembles is a MATCH, and a line it does not reassemble is a LOSS.
- The ledger mirrors: `AuditAnswer` counts `Rejoined` for a matched verdict with
  `Rejoined` set (and appends a thin ledger entry, so "the grid wrapped it" stays
  VISIBLE rather than inferable from an absence), and it no longer re-runs the join.
  Each audit face now reports one measurement.
- Rendering: `--compare` prints `rejoined=` and `wrap_rows=`;
  `internal/agent/chat_surface.go` renders `direction=both_wrapped` for a wrapped
  match (the `DamageTextWrapped` case there was unreachable once the wrap became a
  match).

## What was deliberately NOT widened

- A join that cannot reproduce the line exactly still leaves a mismatch: pinned by
  `TestCompareTranscriptLine_LostWideAsciiLineIsStillNotAMatch` (a 117-cell line
  nowhere on the grid → `wrap_artifact_or_loss`), by
  `TestCompareTranscriptLine_TruncatedRowIsStillPaintLoss` (a row carrying only the
  head → `paint_loss`), and by the new
  `TestUIStateCompare_LostLineStillExitsThree` (through the verb: exit 3).
- `DamageToRow` is unchanged: a row that displays the whole line plus something else
  is still recorded as damaged (it is), and it still exits 3 on the one-line face.

## Test contract moved (three existing cases)

These encoded the pre-fix behaviour and were updated to the stronger fact, not
weakened:

- `internal/uiclient/audit_test.go` (`TestAuditAnswer_WrapIsNotALoss`): the "RED
  direction" leg asserted that the one-line face did NOT match a wrapped line. That
  IS the defect; it now asserts the one-line face matches, is `Rejoined`, and carries
  `prefix == len(line)`, while `RowTailRunes != 0` still proves the row-exact rule
  did not fire (the join is what made it an arrival).
- `internal/uiclient/compare_carry_prefix_test.go`
  (`TestCompareTranscriptLine_WrappedRowOfExactlyGridWidthIsNotPaintLoss`): asserted
  "not a paint loss"; now asserts the arrival and the line-shaped prefix.
- `internal/uiclient/compare_carry_test.go` / `compare_carry_prefix_test.go`: the
  prefix assertions are unchanged and still green — this is why the single-row
  `sharedRunePrefix` reading was KEPT and OR-ed with the joined one rather than
  replaced.

## Verification recipe (reusable)

- The kept dump is the ground truth for this class:
  `gopi ui_state --compare "<line>" --target chat --session <sessions>/<id>/session.jsonl`.
- Build a mid-word-wrap fixture by painting ONE line wider than the band through the
  production band on a real pty (`auditFixture`), with an unbroken ASCII token longer
  than what is left of the row: the wrap then lands inside the word, which is the
  shape a row-exact rule cannot see.
- RED proof: run the same test file on a `git clone --shared --no-checkout` of this
  worktree checked out at `master` (a separate clone, never `git stash` in a shared
  repo). Measured there: exit 3, `damage=text_wrapped`, `prefix=86` of 103 runes.
