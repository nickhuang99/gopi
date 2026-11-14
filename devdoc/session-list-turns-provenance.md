<!-- doc-class: normative -->
# One session id, two rows — the turn slot says which half a number came from

**Status**: shipped (this commit). **Epic**: PA ruling 2026-11-04.

## The defect, stated as an ambiguity rather than as a row count

`gopi session list` answers **one session id with two rows**: `kind=chat` (a `chats`
row, whose turn count exists) and `kind=run` (a run log the sessions tree yields,
which has **no** turn count). Measured on this profile before the change
(`session list --all --json --limit 200`): 200 rows over **22 session ids carrying
both kinds**. So the dual shape is not an edge case; it is the ordinary case,
because every session that ran a worker *also* has a chats row.

Both rows published the same bare key:

```json
{"session_id":"0947e01f-…","kind":"chat","turns":1,"turns_completed":1,"human_turns_known":true}
{"session_id":"0947e01f-…","kind":"run", "turns":0,"turns_completed":0,"human_turns_known":false}
```

Two failures, both from that one shape:

1. **A reader** seeing two rows for one id could not tell which row the number
   belonged to. The only discriminator was `kind`, and `kind` was not what the
   number's meaning keyed on.
2. **A consumer** summing the column counted the run row's `0` as a measured
   zero. `0` here means *"no `chats` column stands behind this number"*; it does
   **not** mean *"this session ran no turns"*. The two are different claims and
   the wire made them identical (Rule 15).

**The PA ruling is the constraint this fix obeys**: keep **both** rows — they are
two facts (a conversation, and the run that produced it), not a duplicate — and
never collapse `turns` into one number. What the ruling asks for is that the
provenance be **visible**, which is the opposite of deleting a row.

## The fix: name the half, and stop publishing an absence as a zero

**JSON face** (`cmd/agent/cli_session.go`):

- `turns`, `turns_completed` and `deprecated_keys` became **pointer** fields with
  `omitempty`, built by ONE function `sessionJSONStripTurns`. The chat row is
  built by `sessionJSONRowOf` (which always sets them — a real `0` there *is* a
  measurement: "this conversation opened no turn"); the run row strips them.
- A new key `turns_provenance` (`internal/sessionevent.JSONKeyTurnsProvenance`)
  is on **every** row of both species and is the key to branch on:
  - `session-log-turn-openings` — the chat row; `turns` is the **session log's
    turn-opening count**, the axis `turn N`, a replay band and `--turns` all
    number along (epic 2026-11-04, superseding the 2026-10-27 `chats.turn_count`
    spelling of the bare word). Measured example that decided this: session
    `3a93ac1c-c0a3-448d-a14f-9eb673b64f9f` printed `turns 29` while the replay's
    last band read `──── turn 30`.
  - `no-chats-row` — the run row; the turn keys are **omitted**, not zeroed.

The provenance value names the **producer** instead of repeating the word
`turns`: a marker that repeated the label would be the ambiguity it exists to
remove.

**Human table** (`internal/tools/sessionruns_render.go`): the run row's format
gained one slot, and it is the slot a chat row spends on `turns N` —
`turns none (no chats row)`. Measured afterwards on this profile:

```
da090e63-…  [chat]  human  之前有去，…  (running, model deepseek-flash, turns 2, total tok in=1666054 out=6506 cache=1666054, started 2026-11-04 20:58)
da090e63-…  [run]  running  da090e63-…  (pid 1053082, alive, —, started 2026-11-04 20:54, turns none (no chats row))
```

Same id, two rows kept, and the turn slot now says which half each row is.

The clause is a constant (`tools.SessionSourceRunTurnsClause`) composed from
`sessionevent.CounterUnitTurn`, so the table and the JSON face cannot spell the
word two ways (Rule 14: the leaf owns the vocabulary). The unit word is shared
with the chat row beside it by construction, not by coincidence.

## Why "omit" and not `turns: null` or `turns: "none"`

- **`null`** keeps a key present on the run row, so a consumer that reads
  `row["turns"]` still gets an answer about a row that has nothing to answer.
  A summing consumer would have to add a null check — the same "did you notice?"
  burden the defect imposed.
- **`"none"` as a string** changes the key's TYPE across rows, which is worse than
  a missing key: a strict loader breaks on the second row, and a loose one
  coerces.
- **Omitting** matches what the rest of this wire already does for a fact with no
  producer (`human_turns` is omitted when unmeasured, and `human_turns_note`
  states why). The absent fact is *stated* — by `turns_provenance`, on every row —
  and it is stated as an absence.

## What is deliberately NOT changed

- **The rows stay two.** No merge, no dedupe, no `--collapse` flag.
- **The chat row keeps its number.** Taking `turns` off the row that *has* one
  would be the opposite defect.
- **`session show --json <run id>`**: the resolver answers the chats row when one
  exists (that is the session the id names); the run half is reached from the
  listing. When the page does render a run (`show.HasChat == false`), it strips
  the turn block through the **same** function, so the two faces cannot state the
  absence two ways (Rule 13).
- **`deprecated_keys`** is stripped with the rest on a run row: it *aliases* the
  live count, and an alias of nothing announced as if it were a measurement is the
  same defect wearing a nested key.

## Verification

- `cmd/agent/session_list_turns_provenance_test.go` — the repro, on **both**
  faces, over a fixture that is genuinely dual (one id, one chats row, one run
  log in that id's own directory). Red before: the run row published `"turns": 0`
  and the table printed no turn slot for it.
- Live profile: 200 rows, 177 run rows, **0** of them carrying a `turns` key; 23
  chat rows, all carrying `session-log-turn-openings`; `sum(turns)` unchanged at
  127 (the run rows' `0`s never contributed).
- `make man` regenerated `man/gopi-session.md` from the usage text (the man pages
  are generated; the usage string is the source of truth).
