<!-- doc-class: normative -->
# The word `turn` has one meaning, and every surface prints one of two positions in it

This page is the definition task 01a17a37 exists to settle: a chat session's turn
count was printed by four surfaces, and the operator could see three different
numbers for one conversation. What follows is what the numbers ARE, which surface
prints which, and the two mechanisms that made them disagree.

## 1. The unit

**One TURN = one COMPLETE INPUT to the next.** The person typed it, `gopi notify`
delivered it, or the surface typed it for itself (`chat_surface action=type`).
Every round and tool call the agent ran in between belongs to that one turn.

The predicate that decides it is ONE function, `sessionevent.IsTurnOpeningRow` over
the row's **text** and its recorded **face**, and it is what `gopilog.SessionTurns`
counts. A row that opens no turn is not a turn however it arrived: a background-job
notice the harness writes from the human face is such a row, and so is any other
framework chatter.

Three things a turn is **not**:

| not a turn | what it is | where it is printed |
|---|---|---|
| a ROUND | one JSONL `request` row = one HTTP request/response = one iteration | `session log <id> --rounds`, `session replay --rounds` |
| a PASS | one loop iteration inside one turn (`session_end total_iterations`; on a resumed log the session's figure is the **sum over its run segments**, see §3b) | `--turns` header `PASSES`, `--chat` `Passes` |
| a HUMAN TURN | the SUBSET of turns opened at a row the person typed | `session show` `human turns`, `--turns` header |

A human turn is a subset, so `human turns <= turns` always. A round and a pass are
both strictly larger than the turn count in any real session (measured on
4a0957a3: 13 human turns, 29 turns, 186 rounds, 29 passes).

## 2. The two positions in the turn sequence

The remaining confusion was not a second quantity — it was the SAME sequence read
at two positions, printed under the same word with no way to tell which:

- **the completed count** — how many turns this session HOLDS. `gopi session show
  <id>` prints it as `turns`; `chats.turn_count` holds it; the resume notice prints
  it as "this session already holds N turn(s)"; the live status bar prints it
  (`chatTurnDisplay(operatorTurns)`).
- **the ordinal** — which turn the next one is, i.e. the completed count + 1. The
  resume notice prints it as "the next turn is turn N+1"; the body heading prints
  it (`chatTurnDisplay(operatorTurns)+1` when the turn opens a session turn).

Both come from `chatTurnDisplay`, so neither can drift from the other; its
docblock states the two positions, and
`cmd/agent/chat_turn_display_unit_test.go:TestChatTurnDisplaySemanticsAreWrittenDown`
pins the bar to the completed count, the heading to the ordinal, and both against
the resume notice's own format string. **The subject under test is which NUMBER a
surface prints, not a string.** A surface that prints the bare word `turn` must
print one of these two, never a third thing.

## 3. The two defects this page closes

### 3a. The ledger charged turns the log does not open

`chats.turn_count` was incremented by EVERY completed chat turn
(`tools.ChatRecordTurn`), while the session log opens a turn only at a row
`IsTurnOpeningRow` accepts. The REPL decided "does this turn count" from the FACE
alone (`chatCountsAsOperatorTurn`), so a harness-chatter row written from the human
face charged both the display and the ledger and opened nothing in the log.

Measured 2026-10-27 on session `80c07a7d-bb6b-48b4-be66-ea1867f403f2`: the ledger
held **18**, `gopilog.SessionTurns` over the same file counted **11**.

The fix has two halves, and both are needed:

- `cmd/agent/cli_chat.go:chatOpensSessionTurn(message, face)` — the REPL's
  predicate is now the READER's (`sessionevent.IsTurnOpeningRow`) AND the face
  whitelist. Neither alone is enough: the text predicate alone would claim a face
  this process cannot name, and the whitelist alone is the defect.
- `internal/tools/chat.go:ChatRecordTurnOutcome(sessionID, opened, …)` — the caller
  answers the turn question. `opened == false` folds the TOKENS and charges no
  TURN: the model really spent them, and making cost depend on a labelling
  decision would be a second wrong answer.

### 3b. `PASSES` collapsed to 0 after a resume — FIXED (task `01a17ae2`)

Filed separately as task `01a17ae2-2f8b-74cf-a759-a5f662310a3f` (a
`internal/gopilog` counter, not the turn counter) and repaired in this branch.
The defect: `TotalIterations` was a last-writer-wins assignment, so the
`resume → EOF` run that wrote a second `session_end` with `total_iterations: 0`
made the header print `PASSES = 0` beside `turns = 29` (measured on session
`4a0957a3-bb4e-4337-a761-20606c87d1fd`: line 868 records 29, line 871 records 0).

**The rule it left behind: a session-level counter counts the WHOLE LOG, never
its last segment.** A log holds one `session_end` per run segment, and each one
reports only ITS OWN passes — the REPL's pass counter is per process (see §2's
`turnBaseline` note), so a resume that runs no turn legitimately writes 0. The
session's figure is therefore the **SUM** over segments, over the same span
`SessionTurns` counts turns, and the caption says so
(`sessionevent.CounterSourceSessionEndAccumulated`) whenever the log holds more
than one segment (`Session.PassSegments > 1`).

All three readers that make the assignment are covered — the full parse
(`reader.go`), the `--last` summary walk (`LoadAllSummaries`) and `session
stats` (`ScanSessionUsage`) — because a fix applied to one of them is how the
three surfaces would drift apart.

## 4. What a future change must not do

- Must not make a surface decide "is this a turn" with anything other than
  `sessionevent.IsTurnOpeningRow` over the row it will actually log. A second
  predicate is how the numbers diverged the first time.
- Must not fix a disagreement by changing the READER to match a writer. The reader
  counts the log; the log is the record. A writer that disagrees is the writer's
  bug.
- Must not weaken `harness.go`'s line assertions or the existing replay turn
  assertions to make a number agree.
- Must not stop folding tokens for a row that opens no turn.
