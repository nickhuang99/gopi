<!-- doc-class: normative -->

# The three HTTP numbers, and the turn count that was neither — measured

**Date:** 2026-11-01. **Session measured:** `e1e22e9f-eee0-4c33-a238-0fca04534ad9`
(the PA chat, alive throughout — see "the moving target" below).
**Verdict:** three separate quantities were being reported for one session and
one of them (`chats.turn_count`) was not what its name said at the time the
report was filed; a fourth, `other turns`, was real and unprinted.

---

## 1. The definitions this document uses

They are the operator's, and they are what every number below is checked against:

- **turn** — one *external* input, through to the final presented response.
  Tool calls inside it are detail, not turns.
- **round** — one HTTP request/response *inside* a turn. A turn routinely takes
  many rounds.

So `turn >= 1` and `round >= turn`, and any figure that violates the second is
mislabeled.

## 2. The four numbers, measured at one instant

Taken in a single snapshot (`evidence/readings.txt`), so the comparison is not
across a moving target:

```
$ grep -c '^→ REQUEST' e1e22e9f-….raw.log
1980
$ grep -c '"type":"request"' session.jsonl
1821
$ ./bin/gopi session stats e1e22e9f-… --json | .rounds
1821
```

| reading | value | what it actually counts |
|---|---|---|
| raw log `→ REQUEST` markers | **1980** | every HTTP body written to the wire, **streaming and non-streaming** |
| jsonl `request` rows | **1821** | one per round the agent *logged* — the streaming path's rounds |
| `session stats` `requests` | **1821** | the same number: `BuildStatsFromJSONL` counts jsonl request rows |
| `chats.turn_count` | **329→330** | turns, per the reader's seam predicate — see §3 |

**The 159 marker difference is explained exactly, not estimated.** Classifying
every marker by what followed it:

```
markers: 1980 {'stream': 1821, 'nonstream': 159}
```

1821 + 159 = 1980. The `nonstream` ones are the **compaction/distillation
requests** — a non-streaming `ClaudeChat` call that goes over the wire and
produces a response, but is not a conversation round, so it writes no jsonl
`request` row. Every one of them was inspected: they are all
`# Historical Distillation …` / `## Historical Task Snapshot …` bodies.

So the earlier reading of "1912 raw" vs "1732 stats" is **two correct counts of
two different things**, and the gap is the compaction calls. Neither is wrong;
the mistake was putting them in one column and reading the difference as an
error.

## 3. `turn_count` — what actually increments it

Two writers, one predicate, one ledger row:

- **the increment**: `internal/tools/chat.go:419`
  `turn_count = turn_count + 1` inside `recordTurnSQL`, reached through
  `ChatRecordTurnOutcome` (`chat.go:400`).
- **the callers**: `cmd/agent/term.go:417` and `cmd/agent/cli_web.go:1605`,
  each calling `ag.RecordChatTurn(...)` once per *completed* turn.
- **`opened`**: `ChatRecordTurnOutcome(sessionID, opened, …)` withholds the
  `+1` when the finished turn opened no session turn under
  `sessionevent.IsTurnOpeningRow` — the fix from task `01a17a37` for the
  ledger-ahead-of-reader defect. Withheld the +1, not the tokens: the model
  really spent them.
- **the read side**: `internal/gopilog/ledger_turns.go:79`
  `SessionLedgerTurnCount` renders it, and the reader's own seam count is
  `SessionTurnBounds` (`internal/gopilog/sessionround.go`).

**Measured, on the real session** (temporary probe, deleted before commit):

```
SessionTurnBounds=330  SessionHumanTurnBounds=83  diff=247
faces=map[human:83 notify:247]
```

The seams' recorded **faces** are `human` and `notify` **and nothing else**. That
pins the decomposition:

```
turns 330 = human turns 83 + other turns 247
```

and the 247 is not a mystery quantity — it is `gopi notify` deliveries (§4.12 of
the machine's GOPI.md is the mechanism: a message on the inbox opens a turn).

`chats.turn_count` read 329 in the same window: **one behind, by construction** —
the turn in flight has not finished, and the ledger is bumped by the *finished*
turn. Same count, one lap of lag. Not a disagreement.

## 4. The defect this document repairs, and its fix

`gopi session stats` printed, before the fix:

```
turns        329
human turns   83
```

Two rows, two numbers, and **no arithmetic on screen**: nothing said where the
other 246 went. A reader has no choice but to read two numbers under one word as
a disagreement — which is exactly the shape the operator reported ("314 is
neither a turn nor a round"). Every number was right; the *block* was not
self-accounting.

**The fix is one derived row** (`internal/gopilog/stats.go`), and no new counter:

```
turns        330
human turns   83
other turns  247
requests    1821
```

- the residual is **derived** (`TurnCount − HumanTurns`), never stored, so it can
  never claim a number of its own;
- it is printed **only when the total was measured**, so it cannot be subtracted
  from an unknown (Rule 15);
- the words are constants in `internal/sessionevent/counters.go`
  (`CounterUnitOtherTurn`), beside the two it now accounts against (Rule 1/13/14).

Real run, this rev, on the live session:

```
gopi session stats e1e22e9f-eee0-4c33-a238-0fca04534ad9
│ turns          330
│ human turns     83
│ other turns    247
│ requests      1821
```

**Red proof** (Rule 10.5): with the residual row removed and nothing else
changed, `TestStatsAccountsForEveryTurnItPrints` fails; with it restored, green.
The fixture asserts its own premise (`TurnCount != HumanTurns`) so a fixture that
could not distinguish the answers fails loudly instead of passing blind.

## 5. The moving target — why an earlier number can never be re-read

The measured session is **still running**: pid `891338`, `gopi chat --tools`,
`ELAPSED 2-04:42:18` at the time of writing. Its log therefore grows while it is
being measured.

```
size 21263157 -> 21263157   (over 3 s — no write in this window, but:)
raw markers: 1971 -> 1976 -> 1980   across this task's own reads
turn_count:   329 ->  330
```

**Consequence, and it is the methodological point:** a figure quoted from a live
session is a reading at a time, not a property of the session. Two readings taken
minutes apart differ and neither is wrong. Any comparison must be one snapshot
(the readings in §2 were taken in one command), and any "the numbers don't add
up" claim must say *when* it read.

## 6. Sibling defect found in the same window — NOT fixed here (filed, not hidden)

`gopi session show`'s `tok in/out` reads `chats.input_tokens`, which
`ChatRecordTurnOutcome` folds as **`MAX(input_tokens, ?)`** (non-decreasing peak),
while `gopi session stats` prints the **sum** over requests. Both are printed as
"tokens" for one session. Measured on the smallest sample (`0ccb5f28`, 42
requests, 3 person inputs):

```
session stats  prompt tokens  9,030   (sum over 42 requests)
session show   tok in=420 out=294    (the ledger's MAX — the peak request)
ledger row     input=420 output=294
jsonl rows     rows 42  sum 9030  max 420
```

`9,030` and `420` are both true and are not the same quantity. The `MAX` is
deliberate (its comment says "so a duplicate or out-of-order call can never
shrink the row") and is fine as a monotonic guard — but the column is then a
**peak**, not a total, and `session show` labels it "tok in". That is a
mislabel of the same family as §4 and is left to its own task so this change
stays one thing.

## 7. What this document deliberately does not claim

- It does not claim the ledger and the reader agree to the turn **at every
  instant** — they are one lap apart by design (§3).
- It does not claim `1980` is a stable figure for that session; it is that
  snapshot's reading of a live file (§5).
- It does not touch the raw-vs-jsonl gap. That gap is 1821 streaming + 159
  compaction = 1980 and is **correct behaviour**, measured per marker (§2). The
  fix is not to make the numbers equal; it is to make the labels say which
  quantity each one is.
