<!-- doc-class: normative -->
# devdoc: `round` vs `exchange` — one word, two surfaces, two quantities

> **ORTHODOXY RESTORED 2026-10-12 — this document's naming decision is REVOKED.**
> The operator's ruling: **round ≡ iteration ≡ one HTTP request/response (one JSONL
> `request` row)**, counting notify messages and human input alike. That is what the
> word ROUND names on every surface again; the operator-row quantity (what this
> document calls the human round) is the neighbouring quantity **TURNS**
> (`sessionevent.CounterUnitTurn`, `gopi session log <id> --turns`), and the
> `request`-row unit's own word went back from `exchange` to `round`
> (`sessionevent.CounterUnitRound`, `--rounds`). Read
> `devdoc/round-definition-restored.md` before implementing anything from the text
> below — the reasoning in this file is the HISTORY of the split, kept as a record
> of the path walked, not a description of master.


**Date**: 2026-10-11 · **Branch**: `task/01a12b69367c73e589d83dcf40cf2733` · **Base**: `a3a31da42`

## The defect

`gopi session log <id> --rounds` heads its table `SESSION ROUNDS` and its own
number column `ROUND`. `gopi session replay <id> --rounds` prints `rounds=N` and
per-step `round N/M`. On one log both printed **169** while the log's own shape
was:

| quantity | value on session `aa48a7eb-a5ab-4507-9c4e-b717bf3583cd` |
|---|---|
| JSONL `request` rows | 169 |
| operator (human) rows | 29 |
| content-carrying exchanges | 142 |
| agent-loop iterations (`session_end`) | 29 |

Two real quantities, one word. A reader comparing the two commands had no way to
learn they were not the same number — on THIS base they happened to agree
numerically, which is worse: an agreement that teaches the wrong habit and then
breaks the day the two definitions diverge.

## The correction, and the premise it refuted

**The briefing's premise was stale, and the tree says so.** It stated that the log
surface's ROUND column is "one HTTP exchange" while replay's `rounds` is the
**human round**, "where the clock injection is the only round boundary". Measured
on this base:

- `internal/sessionevent.IsHarnessRoundBoundary` and
  `SessionHumanRoundBounds` **do not exist** on `a3a31da42` — they live only on
  the unmerged sibling `task/01a12b40f6de72e0be7a6b739f42d5b1`
  (`git merge-base --is-ancestor` → not an ancestor; `git diff --stat` → 14 files).
- The clock-tick boundary was **tried and refuted** on that sibling: its own
  devdoc records that the tick is "每请求一次" (128 ticks against 128 `request`
  rows on session `5af45c3e`), so tick-segmentation reproduced the very numbers it
  was meant to replace, and commit `04e146111` ("a human round is the OPERATOR's
  row, not a harness clock tick") replaced it with the operator-row definition.
- The live record is memory **#8279**: the sibling's final definition is the
  **operator row**, `sessionevent.IsHumanRoundBoundary` = `!IsHarnessInjected`.

So the briefing's "时钟注入是唯一的轮界" is a **superseded** statement, and this
devdoc exists so the next reader does not implement against it. The tick symbol
is gone from the sibling too (`a67ee35b9`: "the tick symbol is gone").

**On this base, therefore, replay did not yet count human rounds** — its
`rounds=` was still the request-row count, so it and `log --rounds` printed the
same 169 under the same word. This change is what makes the two surfaces
impossible to misread **before** and **after** the sibling lands.

## The change

The quantity `--rounds` tabulates is an **exchange**: one JSONL `request` row,
one HTTP request/response. It now carries that name on every surface, and the
word **round** is reserved for the human round.

- `internal/sessionevent/counters.go` — `CounterUnitRequestEvent` →
  **`CounterUnitExchange`**, value `"exchanges"`. ONE definition (Rule 14),
  referenced by every printer (Rule 13).
- **`CounterUnitHumanRound`** added: `"human rounds (operator rows)"` — the name
  the replay's number carries, so both units are declared in the one place even
  though only one of them is printed on this base.
- `internal/gopilog/sessionround.go` — the table's banner
  `SESSION ROUNDS` → `SESSION EXCHANGES`, its column `ROUND` → `EXCHANGE`, its
  unit line `ROUND = …` → `EXCHANGE = …`, and the single-exchange header
  `ROUND N of M` → `EXCHANGE N of M`. The two spellings are named constants
  (`sessionExchangeTableWord` / `sessionExchangeColumnWord`) with exported
  accessors, so `cmd/agent` asserts against the constant, never a literal.
- A new header line names the other surface explicitly — the trap stated where
  the reader is standing, **and stating its CURRENT behaviour rather than
  assuming its future one**: `NOT what gopi session replay <id> --rounds
  numbers: that surface's unit is the human rounds (operator rows) — and where
  it still counts 'request' rows under 'rounds=', that is THIS table's number,
  not the round`. The first draft of this line said the replay "prints it as
  `exchanges=`" — a statement about the SIBLING's branch read as if it were
  this base's behaviour, and false here: this base's playback prints
  `rounds=169` (measured), and it is the sibling's change that moves it. The
  line was corrected to say what this tree does. The numeric equality
  (`169` = `169` here) is not the proof of anything — it is the TRAP: the two
  surfaces agree only while the playback still counts request rows, and they
  part the moment it adopts the human round elsewhere.
- `cmd/agent/cli_log_round.go` — refusals now say "exchange" ("exchange 5 does
  not exist … has 169 exchange(s)").
- The `--rounds` **verb is deliberately unchanged**: the CLI grammar is not this
  change's to move, and the flag's cost (every script and doc that spells it)
  buys nothing — the defect was never in the flag's spelling, it was in the
  column's.

## The option that was rejected: renaming the API

Renaming `SessionRounds`/`SessionCanonicalRounds`/`FormatSessionRoundTable` to
`Exchanges` was the other candidate. Rejected because that API is being
**simultaneously reworked on the unmerged sibling**, which deliberately kept
`SessionRounds` as the exchange count and renamed the replay's own field to
`ExchangeCount` (`git diff` of `internal/replayplay/material.go`). A whole-symbol
rename here would collide at merge in exactly the region the sibling just rewrote
— and the sibling's own report already lists **my** half as its Not Done item:
"`gopi session log <id> --rounds` 的表头仍写着 `SESSION ROUNDS … ROUND = request
events`". The two changes are complementary by construction: it renamed the
number it prints (`exchanges=`, **on the sibling's branch — this base's playback
still prints `rounds=` and is unchanged by this commit**), this one renames the
number the log table prints.
Vocabulary agrees — **exchange** on both.

## Verification

- Build: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Tests: `make test-budget PKGS='./internal/gopilog ./internal/sessionevent ./cmd/agent'` — see the task report for the run stamp.
- Ran: `./bin/gopi session log aa48a7eb-a5ab-4507-9c4e-b717bf3583cd --rounds`
- Result: banner `SESSION EXCHANGES — aa48a7eb-… (169 exchange(s))`, unit line
  `EXCHANGE = exchanges`, column header `EXCHANGE      RAW  ELAPSED …`, and the
  cross-surface line naming the replay's human rounds.
- Ran: `./bin/gopi session replay aa48a7eb-a5ab-4507-9c4e-b717bf3583cd --rounds`
- Result: `steps=169 rounds=169` — unchanged on this base (the sibling's change
  is what moves it to the operator-row count). The point proven here is the
  **word**: the log surface no longer lends "round" to a number the replay also
  calls "round".
- Golden drift was exactly the unit lines: `git diff --stat tests/fixtures/…` →
  2 files, 2 insertions, 2 deletions, both `request events: 9` →
  `exchanges: 9`. Nothing else moved — a surgical rename, checked rather than
  assumed.
- Regression guard: `TestSessionReplayStatic_RoundSelectorMatchesTheLogSurface`
  now asserts the log surface prints `EXCHANGES`/`EXCHANGE` and does **not**
  print `SESSION ROUNDS` — the conflation cannot come back silently. Its numeric
  half (the two surfaces agree on the ordinal) is unchanged and still passes.
