<!-- doc-class: normative -->
# The cost block belongs to the record, not to the log

**Ruling (operator, 2026-11-04), verbatim in intent:**

> `session show <id>` does not show the cost part, which is only visible at
> `session log <id>` at the end. This cost info is an important part. I don't
> know if cost is calculated data or part of what the session reported at the
> end by server or LLM — my point is that this cost part is directly output from
> the LLM as part of the raw log, **which is proved to be false**, i.e. it is
> calculated data which does NOT belong to the log. If shown in the log, a user
> might consider the LLM always reports such a summary at the end of a session,
> which is wrong. So it should be moved to `session show` output, which is part
> of the data field in the DB. Then there is no need to sum for each call.

## What was measured first

The claim "the cost block is written by the session" is refuted by the log
itself. On session `01a1a3eb-4dca-76b3-8b83-36326a6c37ae`:

```
$ grep -c 'COST' /home/user/.gopi/sessions/01a1a3eb-4dca-76b3-8b83-36326a6c37ae/01a1a3eb-4dca-76b3-8b83-36326a6c37ae.jsonl
0
$ grep -o '"type":"[a-z_]*"' …jsonl | sort | uniq -c
     68 "type":"assistant"
     27 "type":"request"
      1 "type":"session_end"
      1 "type":"session_start"
      5 "type":"text"
     24 "type":"thinking"
     78 "type":"tool_result"
     39 "type":"tool_use"
     35 "type":"user"
```

Zero occurrences of the block in the log file, and there is no `type` a human
or a model wrote at all. The text was produced at RENDER time by
`FormatSession` (`internal/gopilog/formatter.go`), which summed the parsed
`request` rows. A calculated artefact rendered inside a log is a category
error, and it is a live one: it reads as something the model said.

The one thing the session *did* record about its own cost is the
`session_end` event:

```
$ grep -n 'session_end' …jsonl | tail -1
171:{"elapsed_sec":191.40867957,"exit_code":0,"reason":"complete",
     "timestamp":"2026-11-04T08:48:14.546445023+08:00",
     "total_iterations":27,"total_tokens":2036246,"type":"session_end"}
```

That is a RECORDING, not a calculation — which is exactly the distinction the
ruling turns on.

## What changed

1. **`internal/gopilog/cost.go`** — new. It carries the ruling, the block's
   words (Rule 1), `RecordedCost` (the block's input) and two renderers:
   - `FormatRecordedCost(RecordedCost)` — the block itself;
   - `CostPointerLine()` — the single line a log now prints instead.

   `RecordedCost` deliberately has **no per-request field**. A caller that could
   pass one would be free to re-sum the rows, which is the defect this move
   removes; the type makes the wrong thing impossible to express.

2. **`internal/rundir/rundir_segments_scan.go`** — `SegmentStamp` grew
   `EndElapsedSec` / `EndPasses` / `EndTokens` / `HasEndTotals`, filled by the
   existing whole-file scan. **They are SUMMED over segments, never
   last-wins.** A resumed log holds one `session_end` per run segment and each
   reports only its own run, so the segment that ran no turn writes
   `total_iterations: 0` and a last-wins reading would erase the passes an
   earlier segment recorded. This is the same defect `internal/gopilog`'s reader
   already repaired (task `01a17ae2`) — the two surfaces must not disagree about
   one session's recorded cost, so they use the same span.

3. **`internal/tools/sessionruns_show.go`** — `SessionShow` carries those four
   fields, read once from the same scan.

4. **`cmd/agent/cli_session.go`** — `sessionCostBlock(show)` renders the block on
   the show page for BOTH species (a chat row and a task run), and
   `sessionShowJSON` gained `recorded_cost_passes` / `recorded_cost_tokens` /
   `recorded_cost_elapsed_sec` / `recorded_cost_recorded` so `--json` publishes
   the same recording (Rule 13: one decision, two renderings). The three values
   are **pointers**, omitted when no verdict was read: an omitted field and a
   measured `0` are different facts, and a plain `int` cannot tell them apart
   (Rule 15a).

5. **`internal/gopilog/formatter.go`** — `FormatSession` prints
   `CostPointerLine()` where the block was. `FormatChat`'s `COST SUMMARY` block
   is untouched: `--chat` is a different render, and its counters are pinned by
   `counter_units_test.go`.

6. **`internal/cliverbs/cliverbs.go`** — `Show` / `SessionShowAddress`, so the
   pointer's verb comes from the constants (Rule 13) and a rename of the session
   family moves the instruction with it.

## The pointer, and why not silence

Silence would be indistinguishable from "this session recorded no cost", and a
reader who remembers the block would think the log lost it. One line, naming
the verb that carries the numbers:

```
cost is not part of this log — it is the session's RECORDED totals: gopi session show <id>
```

## Absences are rendered, not filled

- **No `session_end`** (a live run, or one killed before its end hook): the
  block states `not recorded — this session wrote no `session_end``. Not `0`:
  zero passes is a measurement, and nothing took one (Rule 15).
- **No per-direction split**: `session_end` records ONE total, so a run's line
  says `no per-direction split is recorded for a run — `session_end` records one
  total` rather than splitting a number the rows could be made to yield. A chat
  session's row DOES hold `in`/`out`/`cache`, and those are printed as the
  measured columns they are.

## Verified by running it

```
$ ./bin/gopi session show 01a1a3eb-4dca-76b3-8b83-36326a6c37ae | tail -6
COST:
  the session's RECORDED totals (`session_end`), not a per-request sum
  Passes   27  — session_end total_iterations
  Total    2,036,246 tokens
  Elapsed  191.4s
  Tokens   no per-direction split is recorded for a run — `session_end` records one total

$ ./bin/gopi session log 01a1a3eb-4dca-76b3-8b83-36326a6c37ae | tail -4
cost is not part of this log — it is the session's RECORDED totals: gopi session show <id>

EXIT: SUCCESS (exit=0)
```

The chat species, on `399b9c7f-b992-43dd-96c7-747759c838bf`:

```
COST:
  the session's RECORDED totals (`session_end`), not a per-request sum
  Passes   not recorded — this session wrote no `session_end`
  Tokens   in=5,896,844 out=22,329 cache=5,896,844 tokens  (the session's recorded columns)
```

The numbers match the index row's own line for that session
(`turns 12, total tok in=5896844 out=22329 cache=5896844`) — one recording, two
renderings.

## Tests

- `internal/gopilog/cost_test.go` — five arms: the recorded totals render; an
  unrecorded quantity is STATED and never `0`; the split renders only where a
  column records one; the pointer's verb comes from `internal/cliverbs` and is
  not truncated; and the rendered LOG carries no `COST:` block and no
  `Prompt:`/`Completion:`/`Total:`/`Iterations:` quantity, but does carry the
  pointer.
- `internal/rundir/rundir_segments_scan_test.go` — two arms: the totals are
  SUMMED over segments (a two-segment fixture whose second segment ran no turn
  must still report the first segment's 7 passes — the last-wins reading reports
  0), and a log with no verdict reports `HasEndTotals: false`.
- `internal/gopilog/formatter_test.go` / `TestFormatSession` — retargeted: the
  block's absence and the pointer's presence, in place of the old
  `strings.Contains(out, "COST:")`.
- `cmd/agent/session_log_turns_test.go` — the turn-count epic's two arms moved
  to the page that now carries the counters (`session show`), and the first arm
  gained the assertion that the LOG no longer renders the block. Retargeted, not
  deleted: deleting them would drop the only proof that an unmeasured count is
  STATED rather than rendered as `0`.
- `tests/fixtures/log-stats-raw/golden/log-session.txt` — the recorded golden
  moved with the code (the golden test exists to drift loudly when a rendered
  log changes; this change IS a rendered-log change, so the golden is the place
  the change is visible).

## What was NOT done

- `FormatChat`'s `COST SUMMARY` still sums the parsed rows. It is a different
  face (`gopi session log --chat` renders a conversation, not a run log), its
  counters are asserted by `counter_units_test.go` together with their unit
  declarations, and the ruling named the run log's tail block. Changing it here
  would have retargeted a test suite whose subject is the counter VOCABULARY,
  not the storage location of the numbers — a separate decision.
- `gopi session stats` is untouched. It is the money/pricing face and answers a
  different question (what the session COST in 元), which needs the per-request
  price data the recorded totals do not carry.
