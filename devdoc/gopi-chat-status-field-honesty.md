<!-- doc-class: historical -->
# Task Report: the `gopi chat` status row — every reserved field states a fact

Task 01a0e16702197da6a543b72602f9fe56. The operator read this row at the end of a
turn of 5 iterations and 14 bash calls:

```
deepseek-flash ·  iter 5  ·  tools ·                   ·  ctx 15.4k/200.0k            0s · ready
                ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
                19 blank cells: the tool column, reserved and printed empty
```

and could not decode it: which block is the tool column, what `iter 5` counts, and
why a multi-minute turn says `0s`.

## Completed

### The diagnosis, per field (producer bug or consumer bug)

`status()` (`cmd/agent/chat_stream.go`) builds the row from SIX fields. Each one was
traced to its producer and to the layer that renders it. The blank column is a
producer defect; the other two complaints are one producer defect and one wording
defect.

| field | what it asserts | where the value comes from | verdict |
|---|---|---|---|
| **model** | the model this session talks to | `agent.Model` → `newChatStatusSource(ag.Model, …)` (`cli_chat.go:821`); short form `modelShort` | sound |
| **iter** | the turn counter: the RUNNING turn while a turn is in flight, the COMPLETED count at the prompt (`iterLabel`) | `chatStatusSource.iter` / `.live`, set by `update()`/`beginTurn()` | sound (the wording is what the operator could not decode — see below) |
| **mode** | whether this REPL session has the toolset armed | `sess.toolsEnabled` → `statusToolsHint` (`cli_chat.go:823`). It is the GATE, not the activity | sound, and the ambiguity the operator hit: a bare `tools` nests against the `⚙`/glyph activity field in the next column |
| **tool** | what tool activity is happening NOW (glyph + name + digest) | `Agent.OnToolEvents` → `setTool`/`setToolDone` (`cli_chat.go:1116`); cleared by `clearTool()` when the turn ends | **PRODUCER BUG** — the idle value was `statusFieldIndent`, TWO SPACES. The column is correctly reserved (16 cells, that is the alignment contract) but the producer emitted a hole, so the reader cannot tell "no tool ran" from "this field is broken" |
| **ctx** | the absolute context pair (used / window), labelled | `Agent.Ctx.EstimatedTokens()` + `Agent.ContextWindow` → `statusOpsFor`; `tokens` is the session cumulative total | sound — the pair, its label, and the no-window case are already covered by `chat_status_ctx_*_test.go` |
| **live hint (right block)** | the state word (`ready` / `thinking…` / …) plus the session's elapsed label | state word: `liveHintText()`; elapsed: `statusOps.elapsed` | **PRODUCER BUG, two parts** — see below |

**Bug 1 — the operational snapshot was never re-read on the live path.** The ops
read-out (`ctx`, cache, `elapsed`) is a *function* (`opsFn`), installed once at
`cli_chat.go:846`, and re-read only inside `update()` → `refreshOpsLocked()`. But the
live turn drives `chatTurnStatus.publish()`, which reached around `update()` (lock,
assign `iter`/`tokens`/`queued`, unlock), and the keepalive only advances the
animation frame. So `opsFn` was called exactly **once** per session — at REPL setup —
and the live row carried that frozen snapshot. Measured on the control path: an ops
source installed with a running clock still read `elapsed = 0s` a whole turn later,
which is exactly how a multi-minute turn prints `0s`.

**Bug 2 — the formatter rounded a measured duration down to a fake zero.**
`statusElapsedLabel` had no rung below one second (`default: "%ds"`), so every turn
shorter than a second — a tool round, a short answer, i.e. precisely the turns where
the row is the only thing on screen that could answer "is it working?" — printed
`0s`. `0s` is a number no clock reported, about a turn that was not zero, and it is
the failure mode the epic forbids.

Root causes, 5-why: (i) the row was written when every field was a *value* the REPL
already held; the ops fields arrived later as a *source*, and the publish path was
never taught to re-read it. (ii) The idle content of a reserved column was treated as
"nothing to say", and "nothing" was rendered as padding rather than as the fact
"no tool has run" — the layout's honesty clause stopped at the formatter.

### The fix (three parts, all in `cmd/agent/chat_stream.go`)

1. **The tool column states its idle fact.** `indentStatusToolField` returns
   `statusNoToolLabel` ("no tool") instead of `statusFieldIndent` ("  "). The old
   constant is **deleted**, not kept as a bypassed fallback (Rule 13). The column is
   still held — `statusToolCol` (16 cells) is unchanged, and the new label is 7
   cells, so it fits inside cells the geometry already reserves and no column widens.
2. **The elapsed label has a sub-second rung.** Below one second the label is stated
   in tenths (`0.4s`), with `statusElapsedTenth` (100ms) named and a floor of
   `statusElapsedMin` ("0.1s") so a positive duration can never collapse back into
   the zero-looking label. `0s` now means only what it says: the clock reported no
   time (or went backwards).
3. **The ops snapshot is re-read everywhere the row can change.** `refreshOpsLocked()`
   is now called from `setTool`/`setToolDone` (the first moment a turn has something
   to spend), from `chatTurnStatus.begin` (the turn's own goroutine), and
   `chatTurnStatus.publish` now goes through `update()` — the ONE way the REPL's
   goroutine touches the source — instead of duplicating three of its assignments.

### Tests (new file `cmd/agent/chat_status_field_honesty_test.go`)

Two behavioural tests plus one idle-case test, all driven through the production
wiring and asserted on the row as drawn. The file names no constant the fix
introduces, so it is byte-identical on the red tree and the green one:

- `TestStatusRow_NoReservedFieldIsPrintedBlank` — drives a turn with a running tool,
  then its end, and requires every reserved column to carry a value or a WORD (the
  cells are located from the row's own geometry `statusRowColumns` and named from
  `statusColumnNames`, so the instrument cannot drift from the layout).
- `TestStatusRow_SubSecondTurnIsNotRoundedToZero` — a measured 400ms turn may not
  print the zero-looking label; the no-clock case must not print it either.
- `TestStatusRow_IdleFactIsAWordNotPadding` — the idle case (the operator's own final
  row): every column states a fact, and the tool column says it in words that fit the
  reservation.

## Not Done (honest)

- **The `mode` wording is unlabelled and unchanged.** The MODE column prints a bare
  `tools`, which is what the operator could not decode ("the bare word `tools`" in
  the briefing). It is a *sound* value from a *sound* source — it is the session's
  gate, not tool activity — so it was left alone rather than reworded: any renaming
  ("tools on"/"off") is a wording decision about a field that already states a fact,
  and this task's mandate is the fields that print nothing or print a fake number.
  `no tool` in the activity column now reads against `tools` in the mode column, which
  makes the pair decodable without renaming either.
- **The `iter` semantics are unchanged.** `iter 5` is the completed-turn count at the
  prompt and the running turn in flight (`iterLabel`). The epic asks for provenance,
  which is in the table above; changing *which* count the prompt shows is a product
  decision, not a defect, and is not made here.
- **The ops snapshot is still refreshed by hand at the known entry points**, not by a
  single choke point: the read-loop path that calls `turnStatus.status()` directly
  (the compose path) does not re-read it. It is correct today because every writer of
  the counters publishes first; a future writer that forgets would re-open Bug 1.
  Making the composer self-refreshing would mean calling `opsFn` from the editor's
  read loop (a second goroutine touching the agent's counters), which is the race the
  current design deliberately avoids — so this stays a stated residual, not a fix.
- **`ctx` was not re-measured against a live API response** in the evidence run (the
  turn was short); its provenance is the code path above plus the existing
  `chat_status_ctx_provenance_test.go`.

## Verification

- Build: `go build ./...` → OK (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK)
- Tests: `go test ./cmd/agent/ -run 'TestStatusRow_(NoReservedFieldIsPrintedBlank|SubSecondTurnIsNotRoundedToZero|IdleFactIsAWordNotPadding)' -v`
  → 3/3 PASS after the fix; the same file FAILS on unmodified master (failure pasted below)
- Ran: `printf 'Use the bash tool to run: echo hello\r' … | COLUMNS=117 LINES=26 script -qefc "./bin/gopi chat --tools --dump-screen=/tmp/chat-status-final.dump" /dev/null`
  (verbatim, with the feeder's timings: `{ sleep 12; printf 'Use the bash tool to run: echo hello\r'; sleep 1; printf '\r'; sleep 60; printf '/quit\r'; sleep 1; printf '\r'; sleep 12; }`)
- Result: a real `gopi chat` turn that called the `bash` tool, with the tool field,
  the elapsed label and the state word all observed moving; the rows are quoted in
  `## Evidence (self-filed)`
- Ran (the ctx/ladder correction below, re-measured after the first draft):
  `printf '/quit\r' | script -qefc "stty cols 120 rows 30; ./bin/gopi chat --tools --dump-screen=/tmp/ctx-wide2.dump" /dev/null`
  and the same line with `stty cols 80 rows 24`
- Result: 80 cols → `'deepseek-flash ·  iter 0  ·  tools ·  no tool                      0.1s · ready'`
  (79 cells, the metrics column shed by the ladder); 120 cols →
  `'deepseek-flash ·  iter 0  ·  tools ·  no tool          ·  ctx 15.4k/200.0k            0.1s · ready'`
  (98 cells, both sub-cells of the group present with the pair measured). Exit code 124
  both times is the `timeout` wrapper reaping the pty after `/quit` — the dump is
  complete (header line + 24 rows) in both.
- Commit: see the branch `fix/chat-status-field-honesty`

### The RED failure (this exact test file, on unmodified master)

```
=== RUN   TestStatusRow_NoReservedFieldIsPrintedBlank
    chat_status_field_honesty_test.go:157: the turn finished: the "tool" column is reserved and printed BLANK (its cells are "                ") — a field that reserves space and prints nothing is worse than an absent field
        row = "deepseek-flash ·  iter 5  ·  tools ·                   ·  ctx 15.4k/200.0k            4m00s · ready"
--- FAIL: TestStatusRow_NoReservedFieldIsPrintedBlank (0.00s)
=== RUN   TestStatusRow_SubSecondTurnIsNotRoundedToZero
    chat_status_field_honesty_test.go:179: the elapsed formatter rounds a MEASURED 400ms to "0s" — the zero-looking label is the one thing this row must not print for a non-zero turn
        elapsed label = "0s"
        row = "deepseek-flash ·  iter 0  ·  tools ·  ⚙ bash           ·  ctx 15.4k/200.0k            0s · ready"
--- FAIL: TestStatusRow_SubSecondTurnIsNotRoundedToZero (0.00s)
=== RUN   TestStatusRow_IdleFactIsAWordNotPadding
    chat_status_field_honesty_test.go:219: the prompt: the "tool" column is reserved and printed BLANK (its cells are "                ") — a field that reserves space and prints nothing is worse than an absent field
        row = "deepseek-flash ·  iter 5  ·  tools ·                   ·  ctx 15.4k/200.0k            4m00s · ready"
--- FAIL: TestStatusRow_IdleFactIsAWordNotPadding (0.00s)
FAIL	github.com/nick/gopi/cmd/agent	0.568s
```

The reproduced row is the operator's own shape (19 blank cells where the tool column
is), and the second failure is the epic's headline: a measured 400ms printed `0s`.

**Re-measured once more at close-out, against the master this branch was finally
synced with** — `git archive master | tar -x -C $SCRATCH` (master tip `78d0aa83`,
scratch on `/home`, dev 2052 ≠ `/` dev 2051), the ONE test file copied in, nothing
else changed:

```
$ go test ./cmd/agent/ -run 'TestStatusRow_(NoReservedFieldIsPrintedBlank|SubSecondTurnIsNotRoundedToZero|IdleFactIsAWordNotPadding)' -count=1
--- FAIL: TestStatusRow_NoReservedFieldIsPrintedBlank (0.00s)
    chat_status_field_honesty_test.go:157: the turn finished: the "tool" column is reserved and printed BLANK (its cells are "                ")
        row = "deepseek-flash ·  iter 5  ·  tools ·                   ·  ctx 15.4k/200.0k            4m00s · ready"
--- FAIL: TestStatusRow_SubSecondTurnIsNotRoundedToZero (0.00s)
    chat_status_field_honesty_test.go:179: the elapsed formatter rounds a MEASURED 400ms to "0s"
        elapsed label = "0s"
        row = "deepseek-flash ·  iter 0  ·  tools ·  ⚙ bash           ·  ctx 15.4k/200.0k            0s · ready"
--- FAIL: TestStatusRow_IdleFactIsAWordNotPadding (0.00s)
    chat_status_field_honesty_test.go:219: the prompt: the "tool" column is reserved and printed BLANK (its cells are "                ")
    chat_status_field_honesty_test.go:226: the prompt's tool column carries no word (its cells are "                "): the reader cannot tell "no tool ran" from "this field is broken"
    chat_status_field_honesty_test.go:238: idle row = "deepseek-flash ·  iter 5  ·  tools ·                   ·  ctx 15.4k/200.0k            4m00s · ready"
FAIL	github.com/nick/gopi/cmd/agent	0.543s
```

The three failures are the same three, on the current master tip, with the same
19 blank tool cells and the same `0s` for a measured 400ms — the defect is on
master now, not only on the master this branch started from.

### GREEN after the fix

```
=== RUN   TestStatusRow_NoReservedFieldIsPrintedBlank
--- PASS: TestStatusRow_NoReservedFieldIsPrintedBlank (0.00s)
=== RUN   TestStatusRow_SubSecondTurnIsNotRoundedToZero
--- PASS: TestStatusRow_SubSecondTurnIsNotRoundedToZero (0.00s)
=== RUN   TestStatusRow_IdleFactIsAWordNotPadding
    chat_status_field_honesty_test.go:238: idle row = "deepseek-flash ·  iter 5  ·  tools ·  no tool          ·  ctx 15.4k/200.0k            4m00s · ready"
--- PASS: TestStatusRow_IdleFactIsAWordNotPadding (0.00s)
ok  	github.com/nick/gopi/cmd/agent	0.887s
```

Branch-scoped smoke (the packages this diff touches, the status/turn/stream/panel
cases): `go test ./cmd/agent/ -run 'StatusRow|StatusLadder|StatusMetrics|StatusColumns|
StatusDegrad|StatusCtx|TurnLayout|TestChatStream_|TestChatStatus_Tool|TestChatEditingLine_|
TestChatTurnPhases|TestChatLiveHint|TestStatusLiveWords|TestChatStatus_Long|TestChatStatus_Stays|
TestRecordingWriter'` → `ok`, and `go test ./internal/lineedit/` → `ok` (cached).

## Evidence (self-filed)

- **RED**: `go test ./cmd/agent/ -run 'TestStatusRow_(…)' -v` on master →
  `--- FAIL: TestStatusRow_NoReservedFieldIsPrintedBlank (0.00s)` with
  `the "tool" column is reserved and printed BLANK (its cells are "                ")`,
  and `the elapsed formatter rounds a MEASURED 400ms to "0s"`. Output pasted above.
- **GREEN**: the same three tests, same file, after the fix → 3/3 PASS, with the idle
  row printed by the test itself:
  `deepseek-flash ·  iter 5  ·  tools ·  no tool          ·  ctx 15.4k/200.0k            4m00s · ready`.
- **Build**: `go build ./...` → exit 0; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0.
- **Real turn, with a tool call** — the rows below are quoted verbatim from the paint
  stream of the run whose invocation is in `## Verification` (ANSI stripped, nothing
  else edited). The tool DID run (`⚙ call bash echo hello` is the answer band's row for
  it), and every field moved:

```
deepseek-flash ·  iter 0  ·  tools ·  no tool                      0.1s · ready
──── turn 1 · Use the bash tool to run: echo hello ─────────────────────────────
deepseek-flash ·  iter 1  ·  tools ·  no tool                      7s · sent
⚙ call bash echo hello
deepseek-flash ·  iter 1  ·  tools ·  ⚙ bash echo hel…             9s · sent
deepseek-flash ·  iter 1  ·  tools ·  ⚙ bash echo hel…             responding
deepseek-flash ·  iter 1  ·  tools ·  no tool                      10s · ready
```

  Read against the operator's own row, field by field: the tool column is never
  blank (`no tool` at the prompt, `⚙ bash echo hel…` while the tool runs), the
  elapsed label is a measurement at every rung including the sub-second one
  (`0.1s` a moment after launch, then `7s`/`9s`/`10s`), and the state word
  (`sent` → `responding` → `ready`) is a fact the tracker owns, not a decoration.
  The turn ended and the tool column returned to `no tool` — the activity is
  cleared with the turn, as designed.
- **The RED, re-measured on a REAL master checkout** (independent of this run's branch,
  so no worktree was borrowed — `git archive master | tar -x -C $SCRATCH`, the test file
  copied in, `go test ./cmd/agent/ -run 'TestStatusRow_…' -count=1` inside it):
  `--- FAIL: TestStatusRow_NoReservedFieldIsPrintedBlank`, `--- FAIL:
  TestStatusRow_SubSecondTurnIsNotRoundedToZero`, `--- FAIL:
  TestStatusRow_IdleFactIsAWordNotPadding`, `FAIL github.com/nick/gopi/cmd/agent 1.123s`,
  with the same two rows quoted above (19 blank tool cells; a measured 400ms → `"0s"`).
  The scratch tree carried **only** this change — master's own tree, plus the test file.
- **The screen dump itself**: `#gopi-chat-dump-report path=/tmp/chat-status-final.dump
  json=… bytes=5665 pid=3149941 grid=80x24 geometry=layout dumps=14 frames=89`
  (the run's own line, quoted). The dump is the *interpreted* grid, so it carries no
  ANSI — the rows above are the same rows as the paint stream, which is the layer the
  dump samples.
- **Derived-width fact, measured**: the new idle label is
  `lineedit.DisplayWidth("no tool") = 7` cells against `statusToolCol = 16`, so it
  fits the reservation the geometry already holds and `statusColumnsAtCols` /
  `statusPinCols` are untouched (`statusLayoutBudgetCols` was not widened).
- **Branch-scoped smoke**: `go test ./internal/standards/ -run 'Gate|Rule'` → `ok`
  (22.3s); `go test ./internal/lineedit/` → `ok`; `./bin/gopi doccheck` → "the devdoc
  corpus is consistent — every file under devdoc/ has a well-formed row".
- **Honest limit of the evidence**: the `ctx` field's provenance is the code audit
  above plus `chat_status_ctx_provenance_test.go`, NOT this run — the run's turn is
  too short to grow a context (the pair reports the total alone when no window is
  known, and nothing when nothing is measured — the two cases the existing ctx tests
  cover).
- **CORRECTION (measured 2026-09-27, after the report was first written — the earlier
  wording of this bullet called the dumped row's empty tail "ctx cells it cannot fill
  from a measurement", which is causally wrong twice).** The screen dump this report
  ships is `grid=80x24`, and at 80 columns the row does not DROP the context: it
  drops the whole metrics COLUMN (ctx + cache together) by the ladder, because the
  full form measures 100 cells and `statusColumnsForWidth(80, full)` takes the first
  form that fits. So the empty tail is the metrics column legitimately shed — a
  CONSUMER decision about width, not a producer that measured nothing — and it is
  `ctx`'s whole reservation (17) plus the cache sub-cell and the group's seam, not
  `ctx` alone. Measured on the real CLI, same session shape, two widths:

  ```
  stty cols 80  →   'deepseek-flash ·  iter 0  ·  tools ·  no tool                      0.1s · ready'  (79 cells, metrics shed)
  stty cols 120 →   'deepseek-flash ·  iter 0  ·  tools ·  no tool          ·  ctx 15.4k/200.0k            0.1s · ready'  (98 cells)
  ```

  At ≥100 columns the column is present and carries a real pair (`ctx 15.4k/200.0k`,
  the same numerator and denominator as the operator's own row), so the ctx producer
  IS sound at the width the operator dumped at — which is also why the operator's
  19 blank cells were the TOOL column and not this one (19 ≈ `statusToolCol` 16, not
  30). The `grid=80x24` in this report's own dump header is the other half of the
  same fact: the evidence turn was captured at a width where the ladder sheds the
  group by design.
  **Not fixed here, and deliberately**: making the metrics column survive 80 columns
  means widening `statusLayoutBudgetCols` (forbidden by this epic's constraints) or
  re-cutting the ladder's priorities — a geometry decision, not a field-honesty
  defect, and one the ladder's own arithmetic (each form strictly narrower than the
  last) already governs.
