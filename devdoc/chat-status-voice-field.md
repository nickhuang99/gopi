<!-- doc-class: historical -->
# Task Report: the `/voice` phase on the chat status row (task 01a116b0)

## Completed

The chat status row now says what `/voice` is doing, on the row the operator is
already watching, and stops saying it the moment the mode ends.

- **A phase field, drawn by the mode's own state.** `chat_voice.go` gained the
  mode's live-status vocabulary (`voiceChatLiveRecordingWord` "rec" /
  `voiceChatLiveTranscribingWord` "stt", `voiceChatLiveWordCells`,
  `voiceChatLivePrefixCells`) and the reader that composes it
  (`voiceChatIndicatorFor`). The words are defined ONCE, in the file that owns the
  mode; the row's composer never spells one.
- **The phase is real state, set by the loop that owns the microphone.**
  `voiceChatMode.livePhase()` is the row's read-out, `markLive()` is the ONE place
  the phase moves (recording before the capture window opens, transcribing after it
  closes, both behind the existing `stale(entry)` generation check), and
  `publishStatus()` wakes the read loop so the change is painted. `enter` sets the
  phase to idle and publishes; `shutdown` clears it and publishes — so the field
  cannot outlive the mode.
- **The REPL wires the seam**: `cli_chat.go` calls `status.setVoiceFn(voiceMode.livePhase)`
  before the read loop starts, so the row reads the mode's state rather than a copy
  of it (Rule 3: one owner).
- **The row composer ranks the field.** `statusLiveFieldVoice` weighs the field
  first (it reports what the operator is doing NOW; the elapsed/compaction counter
  reports something that already happened), then the state word in the block's
  existing ladder, and it never cuts the field — a form that does not fit is left
  off whole. `statusLiveField` survives as a one-line wrapper handing the composer
  the idle phase, so every existing caller and test composes through the same code.
- **No geometry constant moved.** The field was sized to the room the live block
  already reserves (see "Measurements" below), so `statusPinCols` is still 100,
  `statusPinSlackCells` is still 0, and every reservation is the number it was.
- **Tests** (`chat_status_voice_test.go`, `chat_status_voice_pty_test.go`, plus the
  widened probe in `chat_status_way_breadth_funding_test.go`): the row is asserted
  as the ROW (composition at the pin), and end to end on a real pty judged on the
  vt100 cell grid — the field appears, follows the phase, and is gone when the mode
  ends, with the row cell-for-cell what it was before.

## Measurements that decided the design (all re-derived by tests)

The task started from the plan "a new field on the live block, funded by widening
the block out of the tool column's retired mode cells". Three measurements refused
that plan, and each refusal is now asserted rather than narrated:

1. **A block's reservation must equal the widest form it DRAWS.**
   `TestStatusReservationsAreFullyDrawn` measures every column's widest form against
   its reservation. A wider block would have pulled the row's tail LEFT while the
   mode was on and pushed it back when the mode ended — the row visibly jumps — and
   it would have moved `statusPinCols`, i.e. which terminal gets the aligned layout.
2. **A marker-prefixed field does not fit the thinking state.** A one-cell marker
   plus a gap makes the field 5 cells; 5 + the block's separator (3) + `think`+gap
   (6) is 14, one past the block's 13. The field would have to vanish exactly while
   the model is thinking — when knowing the microphone is open matters most.
   Dropping the marker makes the sentence 12 cells.
3. **Three cells is the widest word that keeps every state.** `rec` · `stt` are 3
   cells each, so the field's width does not move when the phase does, and every
   state word the block can draw still fits beside it. Asserted in the width test,
   which also asserts the glyph spelling would NOT fit (so the note cannot go stale).

## Not Done

- **The field is not shown on the unaligned (narrow-terminal) fallback path.** It
  rides the live block, which is the aligned row's right-hand reservation; the
  degradation ladder's label path is untouched. This is a deliberate scope line
  rather than an oversight, but it IS an honest gap: on a terminal narrower than the
  ladder's aligned threshold, `/voice` says which phase it is in only in the mode's
  own notes, not on the status row.
- **No live `gopi chat` run against real weights.** The pty case drives the
  production carrier and the real editor read loop with a scripted recorder standing
  in for the microphone; it does not run the local STT engine (this host's weights
  are not part of the harness). The phase transitions themselves are exercised at
  the state level, and the recorder seam is the existing one the other voice cases
  use.
- **The way-marker breadth (`statusToolColWayBreadth`) is still 0**, i.e. the tool
  column's two-segment way marker gets its four cells in no re-partition here. The
  voice field did not consume that budget (the plan that would have) — it is simply
  still unspent, and giving it back is a separate re-partition.

## Verification

- Build: `go build ./...` — OK (exit 0)
- Tests: `go test ./cmd/agent -run 'Status|Voice|Width|Budget' -count=1` — ok, 54.1s.
  New cases all pass:
  `TestStatusRowShowsTheVoiceFieldWhileTheModeIsOn`,
  `TestStatusVoiceFieldWidthIsTheReservation`,
  `TestStatusVoiceFieldFitsBesideEveryStateWord`,
  `TestStatusVoiceFieldOutranksTheCounterAndSaysSo`,
  `TestStatusVoiceFieldOnAPtyShowsThePhaseAndVanishesWhenTheModeEnds`,
  `TestStatusReservationsAreFullyDrawn`.
- Tests, the wider sweep — **INCOMPLETE, stated honestly (Rule 26)**: `make test-budget
  PKGS='./cmd/agent ./internal/lineedit ./internal/standards'` was started twice and
  did NOT finish inside this session (the second run was still going at 14 minutes; the
  first was killed as a duplicate — the known two-sets-in-one-run pattern, Rule 27's
  note). What that leaves unverified is exactly this: the two untouched packages were
  not swept as a set, so a cross-package regression outside the branch-scoped runs
  below is not excluded by this run.
  - `go test ./internal/lineedit -run 'Status' -count=1` — ok (0.24s), the package the
    composer's separator constant comes from.
  - `go test ./internal/standards -run 'Gate|Rule1|Rule13|RepoHygiene|Census|Doc' -count=1`
    — **FAIL, and NOT this branch's**: `TestDocClassBannerMatchesManifest` reports
    `devdoc/sync-window-leg-column-aliases.md: carries the "historical" banner but
    devdoc/DOC-INDEX.tsv classes it "normative"`. Both halves are identical on master
    (read off it with `git show`), this branch's diff to the index only ADDS its own
    row, and the failure does not mention any file this branch touches. Filed as its
    own bug — task `01a11781-38f9-71af-ba02-be4762d96c55` (parked behind this row, so
    it lands after this branch merges) rather than mixed into a feature diff.
- **Red proof (Rule 10.5 — the regression guard is tested).** With the seam
  severed inside `chatStatusSource.status` (the voice read-out forced to idle), the
  pty case fails `timed out waiting for the capturing field on the row` and the
  composition case fails `the row does not show the capturing field "rec"`; with the
  line restored both pass. Green → red → green.
- Ran: `go test ./cmd/agent -run 'TestStatusVoiceFieldOnAPty' -count=1 -v` — the
  real pty carrier at 100×20 with the production status row and the production editor
  read loop as the only writer; the phase is driven through the same function the
  REPL installs (`setVoiceFn`), and each assertion reads the grid back. PASS.
- Result: the composed row at the pin reads
  `… · no metrics    <block>` with the block `1h02m · ready` when idle and
  `rec · think\u00a0` while capturing (12 cells, inside the block's 13), the field
  disappears with the mode, and the row after equals the row before cell for cell.
- Commit: the branch head — `git log -1 --format='%h %s'` on this task's branch
  (subject `feat(chat): show the /voice phase on the status row`). The hash is not
  written here on purpose: this report is committed in the same change, so any
  literal would be one amend out of date.

## Evidence (self-filed)

**The frozen geometry, read off the compiled constants** (throwaway probe, since
deleted):

```
pin=100 budget=100 slack=0 tool=19 live=13 seps=3
```

`seps=3` is the load-bearing one: the field needed no top-level column, so the
separator count did not move from bug #5's 3 — the whole feature is INSIDE the
live block.

**The block's widest form, with the field off and on** (same probe):

```
OFF widest          "1h02m · ready"     = 13 cells (reservation 13)
rec + ready         "rec · ready"       = 11
rec + sent          "rec · sent "       = 11
rec + thinking      "rec · think "      = 12
rec + responding    "rec · resp "       = 11
WIDEST(rec)         "rec · think ."     = 13   ← exactly the reservation
WIDEST(stt)         "stt · think ."     = 13   ← exactly the reservation
```

The reservation is earned BOTH ways: 13 with the field off and 13 at its widest
with the field on. That is what "the extra cell would have been air" means
concretely — the block is dense in both states, not merely non-overflowing in one.
This is also the number that killed the wider block: at `statusLiveCol = 14` the
same audit reports the block drawing 13 of 14 reserved, i.e. one cell nothing would
ever draw, and `statusVoiceCol` was deleted rather than kept.

**The composed row at the pin** (production source, voice hook live):

```
ROW(98) = "deepseek-flash ·  iter0   ·  tools off           ·  no metrics                         rec · ready"
```

98 is the row's CONTENT width (the layout gives back the live block's trailing pad
when the block is shorter than its reservation), and the prefix before the block is
identical with the field on and off — asserted, not eyeballed, in
`TestStatusRowShowsTheVoiceFieldWhileTheModeIsOn`.

- `statusPinCols=100`, `statusPinSlackCells=0`, `statusToolCol=19`, `statusLiveCol=13`
  — read from the compiled constants (probe output above).
- Red proof: with the seam severed inside `chatStatusSource.status`, the pty case
  fails `timed out waiting for the capturing field on the row` and the composition
  case fails `the row does not show the capturing field "rec"`; restored → both pass
  (green → red → green).
- `git status --porcelain` in the worktree is empty at hand-over (Rule 30).

## Files

- `cmd/agent/chat_voice.go` — the vocabulary, `voiceChatIndicatorFor`, `livePhase`,
  `markLive`, `publishStatus`; phase set in `loop`, cleared in `enter`/`shutdown`.
- `cmd/agent/chat_stream.go` — the voice seam on the status source, the composer's
  voiced branch (`statusLiveFieldVoice`, `statusLiveVoiceForm`,
  `statusLiveAnimationForms`), and the geometry notes (no constant changed).
- `cmd/agent/cli_chat.go` — the REPL's `status.setVoiceFn(voiceMode.livePhase)`.
- `cmd/agent/chat_status_voice_test.go` — composition/width/rank assertions.
- `cmd/agent/chat_status_voice_pty_test.go` — the pty cell-grid case.
- `cmd/agent/chat_status_way_breadth_funding_test.go` — the audit now drives the
  voiced phases, so the block's reservation is measured against what the field draws.
