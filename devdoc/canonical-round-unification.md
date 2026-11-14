<!-- doc-class: normative -->
# devdoc: canonical round — one definition of "round" across log / replay / stats

> **Naming update (2026-10-11, `task/01a12b69367c73e589d83dcf40cf2733`).** The
> definition below is still the live one — one `request` row = one HTTP
> request/response — but it is now printed under the name **exchange**, not
> "round": `session log --rounds` heads its table `SESSION EXCHANGES` and its
> column `EXCHANGE`, and `sessionevent.CounterUnitRequestEvent` is renamed
> `CounterUnitExchange`. The word **round** is reserved for the HUMAN round
> (the operator's own row) that `gopi session replay --rounds` numbers. Where
> this document says "round" for the `request`-row count, read "exchange".
> See `devdoc/round-vs-exchange-naming.md`.

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


**Date**: 2026-10-11 · **Branch**: `task/01a12825712079dda6123f721cd7f9d0` · **Commit**: `19c7fd82c`

## The defect

One session answered "how many rounds?" with **four different numbers on four
surfaces**, because the word "round" was being counted four ways:

| surface | old value (session `62a7079b`) | what it was actually counting |
|---|---|---|
| `session replay --rounds` | 257 | content-carrying exchanges (`len(Iterations)`) |
| `session log --rounds` | 257 | the same reader field, so the same number |
| `session stats --rounds` | 348 | the raw HTTP log's `→ REQUEST` markers |
| (agent loop, in-session) | 72 | `TotalIterations`, the loop's own counter |

A fifth spelling was hiding inside `stats`: the printed row number came from the
raw log's **`iteration` field**, which **restarts at 0 inside one session** (one
`iteration` per prompt), so two exchanges carried the same number and the last
row read "28".

## The definition (operator's)

> one HTTP request/response = one round = one `request` line of the session JSONL.

That is the ONLY definition, and it lives in exactly one place: `SessionRounds`
in `internal/gopilog` (Rule 14). The old meaning did not disappear — it was
demoted to the **named neighbour** `SessionContentRounds`, beside the
`sessionevent.CounterUnit*` constants, so a reader cannot pick up the wrong unit
without reading its name (Rule 15).

## What changed

- `SessionRounds(s)` = `s.RequestEvents`. `SessionContentRounds` keeps the old value.
- The reader stamps `Iteration.RequestOrdinal` / `ToolCall.RequestOrdinal` — the
  ONE mapping from "round N" to "that exchange's content". No surface computes an
  offset of its own. A trailing blob with no `request` row to belong to gets
  ordinal **0** (missing is missing, never a fabricated number).
- `session log --rounds` prints one row per `request` row. A **no-content exchange
  keeps its own row** and says so: skipping it renumbers the rounds, and then
  `--round N` would name a different exchange on each surface.
- `session stats --rounds` is built from the JSONL (`BuildStatsFromJSONL`). The raw
  log contributes only the wire bodies it alone holds, joined by the **token
  pairing** (`AttachRawTraffic` / `pairRawRounds`) — never by ordinal. New named
  field `WireLogDetail` states which corpus each half came from.
- `replay` lays out one step per canonical round, including a no-content round
  (`replayplay.RoundNoContent`), for the same renumbering reason.
- `--toolcall N` (`gopi session log|replay <id> --toolcall N`) is the separate
  address for "the Nth tool call", sharing one renderer and one resolver, and it
  **names the round the call ran in**. It exists so that "tool call" never has to
  borrow the word "round".

## Deliberately NOT changed

`session raw --rounds` still reports the raw log's own `→ REQUEST` count (348).
That is a different quantity — every send that actually happened on the wire — and
it has its own unit constant (`CounterUnitRawRequest`). Collapsing it into the
canonical number would hide exactly the discrepancy that produced this bug.

## Verification

Frozen input, session `62a7079b-3f59-4ce6-a0d5-ad5bef1a948e`:
`request` rows **321** (independently counted from the JSONL), content-carrying
exchanges 257, raw `→ REQUEST` 348, tool calls 348.

After the change all three round surfaces report **321** on that session; `session
log <id> 3` prints the usage fields byte-identical to the 3rd `request` row
(prompt 116,305 / cache_hit 80,128 / cache_miss 36,177 / completion 456), which is
the proof that "round 3" IS the 3rd HTTP exchange. Out-of-range refusals
(`--round 5000`, `--round -5000`, `--toolcall 9999`) all exit 1 and name the total
— no clamping.

Red→green regression guard: `internal/gopilog/canonical_round_test.go` builds a
session where the two candidate answers **differ** (a contentless exchange between
two content-carrying ones). Reverting `SessionRounds` to `len(s.Iterations)` makes
it fail (`SessionRounds = 2, want 3`); restoring it makes it pass.

## Follow-up: the tail the round-split left behind (task `01a12847`, 2026-10-11)

The change above landed and was green in `internal/gopilog`, and it left three
kinds of tail in the packages that render it. This branch (`task/01a12847e23770428a9eca9842837bcf`,
which cherry-picks `19c7fd82c` + its two doc commits) clears them.

**(A) A fixture that wrote the `request` row FIRST — the defect, not a golden
drift.** `cmd/agent/cli_session_replay_test.go`'s `replayTestTimeline` put
`{"type":"request","iteration":1}` *before* the assistant row it describes. That
is not the writer's order: `internal/agent` logs the assistant content, then the
`request` row carrying the usage of the call that just finished. Under canonical
rounds the fixture therefore attributed each exchange's content to the NEXT one,
and `--round 1` reported `kind=no-content` for a round that plainly had content —
the test was right and the fixture was wrong. It is now the real shape (user,
assistant, request, tool_result, assistant, request), and the two tests that read
it (`TestSessionReplayStatic_RoundRendersPlainText`,
`TestSessionReplayKeyboard_DrivesTheTransportOverPty`) pass unmodified.

**(B) Six goldens + their assertions followed the round source.** The fixture
snapshots in `tests/fixtures/log-stats-raw/golden/` were recorded when
`session stats` read the RAW log, so they carried the raw corpus's numbers under
canonical headings. Re-recorded against the canonical build:

| golden | was | is |
|---|---|---|
| `stats-overview.txt` | box 120 wide, `source … (no raw log)` | 120 wide filled with the wire-bodies label, `rounds 9` |
| `stats-rounds.txt` / `stats-traffic.txt` / `stats-bill.txt` | `(raw round ordinal, 1-based)` | `(canonical round, 1-based …)` |
| `stats-tools.txt` | `6 calls / 3 kinds` | `8 calls / 3 kinds` |
| `stats-json.json` | `"source": "raw"` | `"source": "jsonl"` + `"wire_log"` |
| `stats-cost.txt` | `0.0120 元` (5 raw rounds) | `0.0170 元` (9 canonical rounds) |

`internal/gopilog/stats_golden_test.go`'s `fixtureStats` now builds the block the
CLI builds — `BuildStatsFromJSONL` + `AttachRawTraffic` — because
`TestStatsCLIOverFixture` asserts the SAME snapshots; a helper that kept reading
the raw log would have been asserting one thing while the CLI did another. The
raw parse did not lose its coverage: it is pinned by
`TestBuildStatsFromRoundsNumbers`, `TestBuildStatsFromRoundsMatchesJSONL`,
`TestGoldenRawRoundIndex` and `TestGoldenRawRequestIsByteExact`.

**(C) The G4 inventory was short one row.** `gopi session log <session-id>
--toolcall N` is a NEW advertised surface (`cmd/agent/cli_log_toolcall.go`), so
the derivation reads 87 while the doc's table still carried 86. Fixed in the three
places the gate requires to agree: `g4ExpectedRows` (the pinned expectation), the
inventory's re-derived listing + table (`devdoc/gopi-session-help-surface-inventory.md`,
row 8, everything below renumbered) and the case table's assertion
(`TOOL CALL 2 of 8` — deliberately the CALL count against the same session's 9
rounds, which is the distinction the flag exists to keep).

**Verification on the real session** (`62a7079b-3f59-4ce6-a0d5-ad5bef1a948e`):
`session log --rounds` / `session replay --rounds` / `session stats --rounds` all
report **321**; `session log <id> --toolcall 1` reports `TOOL CALL 1 of 348` and
`it ran in round 1 of 321`; `session raw --rounds` still reports its own **348**
(deliberately a different quantity — see "Deliberately NOT changed").


The player already went through the 2026-10-03 merge that retired `gopi replay`;
rewriting it would move a surface the user has already learned, and would
invalidate every golden. The defect was never in the player — it was **one word
carrying four units**. 17 files changed + 3 new, +1105 / −157, every existing
counter-unit and golden test still green.
