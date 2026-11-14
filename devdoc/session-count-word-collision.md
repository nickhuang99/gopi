<!-- doc-class: normative -->
# One Word, Two Numbers: the iteration/turn collision (2026-10-16)

> **PARTLY SUPERSEDED — read this first (2026-10-27).** The ROUND and PASS halves
> below still hold word for word: `iteration` means the `request` row, a loop
> pass is `session_end`'s `total_iterations`, and neither may borrow the other's
> word. **The TURN half is reversed.** This page says `turns` means the
> operator's own rows and that `chats.turn_count` must be published as
> `repl turns`; the operator's ruling of 2026-10-27 is that the pair has no
> subject — an external notify is an input and a self-driven turn is a turn — so
> `turns` IS `chats.turn_count` (one count, the bare word), and the person-only
> subset is the one that carries a qualifier: **`human turns`**. The wire key is
> `turns` again, with `repl_turns` demoted to a nested, dated alias.
> Where this page and the code disagree on turns, the code and
> `cmd/agent/session_show_turns_test.go` are right; the historical text below is
> left as it was written.

## The defect, measured

Session `5c6cd329-3e34-42c0-96b2-9a406961b1e1`, read off its own JSONL:

| quantity | value | producer |
|---|---|---|
| ROUND | **134** | JSONL `request` rows (`cmd/agent/cli_chat.go:716` → `LogRequest` per round) |
| LOOP PASS | **31** | `session_end.total_iterations` (`internal/agent/session.go:544`, fed by `iterCount++` in the chat REPL) |
| TURN | **16** | operator rows (`session.jsonl` `type:"user"` rows carrying text; `SessionTurnBounds`) |

The three are genuinely different quantities, and the numbers say why: the chat
REPL charges one LOOP PASS per turn it ran, including turns opened by a
**notify message** (an external message the person never typed) and turns whose
request **failed**. So passes ≥ turns, and a turn routinely takes many rounds.

Before this change the words did not respect that:

```
$ gopi session log <id>
COST:
  Iterations: 31  (loop iterations — session_end total_iterations)
  rounds: 134

$ gopi session log <id> --rounds      # headed a 134-row table
  ROUND = one JSONL `request` row = one HTTP request/response = one iteration

$ gopi session log <id> --turns       # headed a 16-row table
  ITERATIONS = 31 loop iterations (...)

$ gopi session list --mode chat
  ... (normal, model deepseek-flash, turns 31, ...)      ← 31 called "turns"
```

`iteration` meant **134** on one surface and **31** on another, in the same CLI;
`turns` meant **31** in `session list` and **16** in `--turns`. The operator's
"31" came from `session list`.

## What changed (naming only — not one value moved)

Three properties, applied at every surface:

1. **The word `iteration` names the ROUND and nothing else.** It is the
   `request`-row count, the same number `--rounds`, `replay --rounds` and
   `session stats` print.
2. **The loop's own count owns a word of its own: a PASS.** `session_end`'s
   `total_iterations` is a count of *REPL passes*, so it is spelled
   `PASSES` / `loop passes` and never `ITERATIONS`. Same number, same producer
   (`CounterSourceSessionEnd`), new word.
3. ~~**`turns` means the operator's own rows, everywhere.**~~ **REVERSED
   2026-10-27** — see the banner at the top. `turns` is `chats.turn_count` (the
   sessions's turns, every input face equal); the operator-only quantity prints
   as **`human turns`** (`sessionevent.CounterUnitHumanTurn`).

Every surface that prints one of the three also prints which neighbour it is
**not** (`CounterUnitRoundIsNot` / `CounterUnitLoopPassIsNot` /
`CounterUnitTurnIsNot`, `internal/sessionevent/counters.go`) — a reader who has to
infer which quantity they are looking at is the reader this defect was written
for.

### Surfaces touched

| surface | before | after |
|---|---|---|
| `session log <id>` COST | `Iterations: 31`, `rounds: 134` | `Iterations: 134 (rounds)`, `Turns: 16`, `Passes: 31`, each with its NOT-neighbour line |
| `session log <id> --rounds` banner | `ITERATIONS = 31 loop iterations` | `PASSES = 31 loop passes` + NOT-line; `ROUND = rounds …` |
| `session log <id> --turns` banner | same `ITERATIONS = …` | `PASSES = …` + NOT-line; TURNS/ROUNDS named |
| `session replay <id> --rounds` | `rounds=134 turns=16` (words undefined) | `rounds=134 the ROUND — …`, `turns=16 the TURN — …`, each with its NOT-line |
| `session log --last N` summary | column head `ITERS`, `%4d iter` | head `PASSES`, `%Nd PASSES` (width from the word) |
| `session list` / `session show` | `turns 31` | **2026-10-27**: `turns 200` (the ledger's count) + `human turns 66` (the subset), never a pair of totals |
| `gopi task show` run line | `31 turns` | `31 loop passes` |
| `gopi web` identity probe | `turns=31` | **2026-10-27**: `turns=31` — the same word as the ledger's |

The COST block and the summary block come from **one** function
(`costCounterLines`, `internal/gopilog/formatter.go`): the two used to spell
their own strings in two places, which is how the same word ended up carrying
two numbers with neither site able to see the other (Rule 14).

## Verified as a fixed point

The three counts themselves are **untouched** — `session_end`'s value, the
`request` row count and the operator-row count are the same numbers as before.
Only the words and the cross-references moved. The golden fixtures
(`tests/fixtures/log-stats-raw/golden/`) show exactly that: 12 lines changed,
**no number changed** (see the diff — every changed line is a label, a unit or
a NOT-clause).

## The regression guard, and its red proof

`internal/gopilog/counter_word_collision_test.go`
(`TestCounterWordsNeverCarryTwoNumbers`) pins the **allowed pairing of (word,
number)** rather than a rendered string, so a future surface cannot reintroduce
the collision without failing it. Its fixture makes the three counts
**differ** (4 request rows / 6 loop passes / 2 operator rows) — on a fixture
where they coincide the collision is invisible, and a guard that cannot see the
defect is not a guard. It asserts: the iteration line carries the ROUND's number
and **not** the loop's; the turn line carries neither the round's nor the pass's;
each table tabulates its own count and not its neighbour's; and no surface
prints the retired spelling `"loop iterations"`.

Red proof (the mutation and the restore, with the restore anchored to a
revision rather than the index):

```
# mutated: the Iterations line prints the PASS count — the original defect
$ go test ./internal/gopilog/ -run TestCounterWordsNeverCarryTwoNumbers
--- FAIL: TestCounterWordsNeverCarryTwoNumbers
    counter_word_collision_test.go:69: FormatChat's iteration line is not the ROUND count 4 …
    counter_word_collision_test.go:74: FormatChat prints the LOOP PASS count 6 as the iteration count …

# restored, verified by CONTENT IDENTITY against the STAGED blob (a revision —
# an index-relative diff would have been written back from the same index)
$ test "$(git hash-object internal/gopilog/formatter.go)" = "$(git rev-parse :internal/gopilog/formatter.go)"
RESTORE_OK hash=8b1fb23818d9d9cae1f6b120037ede8da9a8cc82
```


## Not done (honest)

- The live status bar's WORD is still `turns` while `cmd/agent/chat_stream.go`
  spells the same column `turn` — two spellings of one quantity, left alone
  because the column is a 7-cell reservation whose arithmetic (`statusTurnCol`)
  is asserted by several layout tests and renaming it is a separate change with
  its own width budget.
- **The bar's NUMBER is no longer left alone** (2026-10-27, superseding the
  paragraph that used to stand here): the status line counted the turns THIS
  surface ran, from 1, so a resumed session showed `turns 1` beside
  `session log`'s 16 and `session show`'s 16 — the operator reported it as
  "重启之后这个数字又归零了". `uiclient.Config.TurnBaseline` now seeds the counter
  from the LEDGER (`cmd/agent/ui_turn_baseline.go` reads `chats.turn_count`), so
  the bar starts where the other surfaces are and advances from there
  (`internal/uiclient/model_turn_baseline_test.go` pins both halves).
- Two `docs`-level mentions (`devdoc/session-counter-units.md`,
  `round-definition-restored.md`) still quote the old rendered strings in prose
  as the historical record of *their* round; they are not rewritten, because
  they are the record of a different measurement.

## 2026-10-27 — the log and `stats` joined the same number

The three surfaces this page named as agreeing (`session list`, `session show`,
`gopi web` identity) already printed the ledger's `turns`; `session log` and
`session stats` printed only `human turns`. The import cycle
(`internal/tools` → `internal/gopilog`) meant gopilog could not read the column,
so it printed the subset and a pointer. It now DECLARES a reader and
`cmd/agent` INJECTS it (`internal/gopilog/ledger_turns.go`,
`tools.ChatLedgerTurnCount`), the same manoeuvre this tree already uses in the
other direction for the subset. Measured on `a60787b0-9081-451f-bd5a-9e396cfc048b`:

```
before:  human turns: 16  (no `turns` line at all)
after:   turns: 16        ← the ledger's chats.turn_count
         human turns: 8   ← the person-only subset, its own word
```

`cmd/agent/session_log_turns_test.go` pins the three-surface agreement as
NUMBERS (log, show page, index row), and `internal/gopilog/ledger_turns.go`
prints an explicit unmeasured sentence — never the subset under the bare word —
when no reader is installed. That unmeasured branch is what the golden fixtures
render, because `go test ./internal/gopilog` runs without the binary's entry
point; the fixtures therefore document the honest fallback, not the shipped
number.
