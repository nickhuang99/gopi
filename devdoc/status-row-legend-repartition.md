<!-- doc-class: historical -->
# Task Report: the status row's legend describes the re-partitioned row (ctx pair leads, hit rate its own named fact)

## Completed

**The legend now describes the row the product composes.** `cmd/agent/cli_chat.go`'s
`chatStatusRowLegend` claimed the OLD partition: it presented `hit 74.5%` as the
group's first fact and described `ctx 92.1k/200.0k≡` as the read-out shown
"in the group when the cache has not answered yet". After the re-partition that
made the context pair lead (commit `9316f3dd`), that sentence teaches an order the
row cannot produce — a reader of `gopi chat -h` looks for a fallback that never
appears and an order that is reversed.

The section was rewritten against the **measured** row (probe below), not against a
reading of the composers:

- the group is stated as **three** read-outs in the row's own order — `ctx …≡`
  FIRST, then the transport `↑12k↓4k`, then the named rate `hit 74.5%` riding it;
- the pair's entry states that it **LEADS and is never the fact that yields**
  ("where the cells run out, the label yields and the pair is shown whole");
- the rate's entry states it is measured from the two counters printed beside it,
  that it is **named** because a bare percentage beside a two-sided pair reads as
  either half of it, and that it is the **least permanent** fact — where the
  sub-cell cannot hold the pair and its rate together, the RATE yields and the pair
  is printed whole (acceptance (e), stated to the reader);
- the samples were corrected to what the formatters really print: the pair's sides
  are the row's **bounded ladder** ("92k", "1M"), not `"92.1k/200.0k"`, and the
  window is gopi's own trim threshold rather than a claim about the model.

**A regression gate ties the sentence to the row** (the real deliverable — the
defect was invisible to every existing gate, because each resolved SYMBOLS from
constants and none read the legend's prose):
`TestStatusRowLegendEntryOrderMatchesTheRow`
(`cmd/agent/chat_status_symbols_legend_test.go`). It finds each fact's **entry** in
the reader-visible help — a line OPENING with the row's own label constant, which is
how a reader scans the section — and fails when the rate's entry precedes the
pair's. **Red proof, observed** (pre-fix legend restored, test run):

```
--- FAIL: TestStatusRowLegendEntryOrderMatchesTheRow (0.00s)
    chat_status_symbols_legend_test.go:267: the legend presents "ctx" at offset 23371 and "hit" at 22106 — the RATE first.
```

Green after the rewrite. The same gate also asserts the rate's entry EXISTS — the
legend cannot go back to describing it as an arm the reader waits for.

## Not Done

- **No code geometry changed by this run.** The re-partition itself (the three
  coupled edits: `statusColumnSepTotalN`, `statusMetricsFormCol`, the rate's funded
  home) had already been implemented on another branch by task `01a0f8c9`
  (`9316f3dd`) and was **merged into master by the merger DURING this run**
  (observed: my worktree's `master` moved from `8374e6af` to `e2a4ef48`, and
  `git merge-base --is-ancestor 9316f3dd master` flipped NO→YES between two calls
  ~5 minutes apart). My branch was therefore synced to master and this run's own
  deliverable is the legend half plus the gate above.
- **`statusLayoutBudgetCols` is untouched, as the constraint requires** — verified
  by diffing `chat_stream.go` against master, not by assertion.
- **Not covered by this run**: whether `%`-bearing strings elsewhere in the
  `gopi chat -h` text (the unaligned composition's percentage) also need updating —
  that path is outside the aligned row this task is about.

## Verification

- Build: `CGO_ENABLED=0 go build ./cmd/agent/`: OK;
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`: OK (binary rebuilt before the
  pty run — the pty test SKIPs with "gopi binary not found" otherwise, observed).
- Tests: `go test ./cmd/agent/ -run TestChatStatus -count=1`: **ok** (PASS).
- Tests: `go test ./cmd/agent/ -run TestStatusRowSymbols|TestStatusCompactionLegend|TestStatusRowLegend -count=1 -v`:
  **4/4 PASS** (`TestStatusRowSymbolsHaveAReaderVisibleDefinition`,
  `TestStatusRowLegendQuotesTheRealCompactionRecord`,
  `TestStatusCompactionLegendIsNotPrintedAsZero`,
  `TestStatusRowLegendEntryOrderMatchesTheRow`).
- Tests: `go test ./cmd/agent/ -run 'TestStatus.*Arithmetic|TestStatus.*Pin|TestStatusMetrics|TestStatusRowColumns|TestStatusLadder' -count=1`: **ok** (the pin/budget guards).
- Ran: `go test ./testsuite/ -run TestChatPTY_ContextDenominatorIsQualifiedOnScreen -count=1 -v`
  — **PASS** in 13.31s:
  `chat_pty_ctx_qualifier_test.go:461: pty stream carried "92k/1M≡"`, `--- PASS`.
  (This is the guard the briefing records as RED on master; on the synced branch it
  is green — the real pty surface was executed, not simulated.)
- Ran: a throwaway probe (`cmd/agent/zz_probe_ctxpair_test.go`, **deleted before
  commit**) printing the composed row at the pin for the states the legend
  describes. The two states that fix the legend's sentences:
  ```
  cache answered      | deepseek-flash ·  iter 12 ·  ⚙ keyword_search l… ·  ctx 42k/64k≡ ·↑12k↓4k hit 74.5%    ⧉2 · ready
  cache NOT answered  | deepseek-flash ·  iter 12 ·  ⚙ keyword_search l… ·  ctx 42k/64k≡                       ⧉2 · ready
  deployed window     | deepseek-flash ·  iter 12 ·  ⚙ keyword_search l… ·  ctx 92k/1M≡ ·↑12k↓4k hit 74.5%     ⧉2 · ready
  ```
  The pair LEADS and the rate rides the transport in every state where a window is
  measured; the pre-fix legend's "shown when the cache has not answered yet" is
  refuted on the screen by the first line.
- Ran: bounded-formatter probe, which is why the legend no longer says `200.0k`:
  `formatCountBounded(200000) = "1M"`, `formatCountBounded(92100) = "92k"`, and
  `statusCountSideTextWidest = 3` (the cap the pair's sides are written under).
- Ran: rate-budget probe — the worst measured pair leaves the rate inside its span
  budget, which is acceptance (e) measured rather than argued:
  `statusCacheCol=8  statusCacheHitRateCells=11  statusHitRateSpanBudget=19`;
  `pair="↑1G↓1G"(6) → field="↑1G↓1G hit 50.0%"(16)`; `pair="↑12k↓4k"(7) → "… hit 74.5%"(17)`.
- Result: the reader-visible legend and the composed row now state the same order,
  the gate that closes the defect is red on the old text and green on the new, and
  the pty guard is green on the executable surface.
- Commit: the commit carrying THIS report, on `task/01a0fa8115587e3d99b9512d13c72c57`
  in `/home/user/workspace/gopi-01a0fa8115587e3d99b9512d13c72c57` (parent `885ac84e`;
  the report, the legend and the gate are its content, so it cannot name its own hash).

## Evidence (self-filed)

- `git diff master -- cmd/agent/chat_stream.go | grep -E '^[-+].*statusLayoutBudgetCols ='`
  → **no output** = the budget constant is NOT widened (constraint satisfied by
  measurement, not by claim).
- Red→green of the new gate: pre-fix legend rendered with
  `git show master:cmd/agent/cli_chat.go > cmd/agent/cli_chat.go` (a file copy —
  `git stash` is refused in this repo's shared worktree set), test run → the FAIL
  line quoted above; file restored from the session dir, test run → `ok`.
- The probe file `zz_probe_ctxpair_test.go` was deleted before the commit
  (`git status --porcelain` prints nothing at close-out, Rule 30).
- Evidence artifacts (probe output, the pre-fix legend copy) live under
  `/home/user/.gopi/sessions/01a0fa81-1558-7e3d-99b9-512d13c72c57/`, never inside
  the worktree.
