<!-- doc-class: historical -->
# Task Report: the status row's legend, re-adjudicated — TWO sub-cells carrying THREE read-outs, in the row's measured forms

Rework run `01a0faf0273670e7b71153d3e037febe`, worktree
`/home/user/workspace/gopi-01a0faf0273670e7b71153d3e037febe`, base master `1d062545`
(which contains `0edd6cea`, the landed legend rewrite of task `01a0fa81`).

## Completed

### 1. The two competing designs were adjudicated by MEASUREMENT, not by prose

Two branches rewrote the SAME block (`chatStatusRowLegend`, the THE STATUS ROW
section of `gopi chat -h`) for the same row:

| design | sample it taught | how it counted the group |
|---|---|---|
| master (`0edd6cea`) | `ctx  92k/1M≡` | "three read-outs … in this order" (a per-fact glossary: ctx, transport, rate) |
| `task/01a0faa7…` | `ctx  12k/200k≡` (and prose claiming gopi trims "at the budget above") | "two facts" (2 entries: ctx; `↑12k↓4k hit 74.5%`) |

Measured with a throwaway probe (deleted before the commit; raw output in this
run's session dir as `probe-row-forms.txt`), calling the PRODUCTION composers:

```
formatCountBounded(200000) = "1M"      ← the deployed DefaultContextWindow
statusMetricsField(deployed ops) = "ctx 92k/1M≡ ·↑12k↓4k hit 74.1%"
statusMetricsSubCellWidths()     = [12 8]   statusMetricsSubCells = 2
statusMetricsField(empty ops)    = "no metrics"
```

- **The row can never print `200k`.** The denominator is `Agent.ContextWindow`
  (`agent.DefaultContextWindow` = `200000`) routed through the row's own bounded
  ladder, which renders it `1M` (three whole digits climb a rung). The branch's
  headline sample was a form the composer cannot emit — the same defect class the
  landed commit removed when it retired `92.1k/200.0k`. Independently pinned on a
  real pty by `testsuite/chat_pty_ctx_qualifier_test.go:611`. **Master's form is
  the measured one.**
- **The group draws TWO sub-cells and THREE read-outs.** `statusMetricsField`'s
  literal list is `{statusCtxField, statusCacheField}`; the hit rate is not a
  sub-cell — it rides inside the cache value (`statusCacheField` →
  `statusCacheHitRateAppend`), which is why the pair and the rate are shed
  together and the rate never leaves alone. So master's "three read-outs" was true
  at the level the legend describes, and the branch's "two facts" was true only at
  the sub-cell level — while the branch's own block listed THREE entries and the
  merged tip listed FOUR.

**Landed: master's sample and glossary SHAPE, the branch's cache-sub-cell
account (pair+rate as one drawn value), plus the branch's `no metrics` entry —
and each count now names the level it counts at** ("TWO sub-cells … carrying
three read-outs"), which is what the merged tip's contradiction ("two/three
facts", four entries) came from.

### 2. The legend now states the group's empty state, which nothing documented

`statusMetricsUnmeasured` (`no metrics`, `chat_stream.go`) is a real output of the
row — a session whose first prompt has not answered prints it in the metrics cells
— and it was documented nowhere. The reader met a word the product never defined
in that column: the same defect class as the undefined `⧉` glyph, one column to
the left. It now has an entry that names it as a state word rather than a value.

### 3. A regression gate for that word (Rule 10.5)

`TestStatusRowLegendDefinesTheGroupsUnmeasuredWord`
(`cmd/agent/chat_status_symbols_legend_test.go`) resolves the word from the ROW's
constant (never retyped, Rule 13), proves the composer really emits it for
unmeasured ops and never for a measured group, and then holds the help to it.
**Red proof, observed** (the legend entry removed, test run):

```
--- FAIL: TestStatusRowLegendDefinesTheGroupsUnmeasuredWord (0.00s)
    chat_status_symbols_legend_test.go:300: the status row prints "no metrics" and NOTHING in the product defines it.
```

Green with the entry restored.

### 4. What was NOT taken from the archived branch, and why

The archived diff's prose was NOT applied (the briefing forbids it, and it is
stale in three measurable ways): its `200k` samples, its "two facts" header, and
its claim that trimming happens "at the budget above" — the budget is whatever
`ContextWindow` is, and the legend's own master wording ("gopi's own trim
threshold … never as the model's limit") is the accurate one. No `git apply`, no
cherry-pick: the surviving text was written directly against the measured row.

## Files

- `cmd/agent/cli_chat.go` — `chatStatusRowLegend`: the group's header, the cache
  entry, the hit-rate entry, and the new `no metrics` entry.
- `cmd/agent/chat_status_symbols_legend_test.go` — the new gate.
- `devdoc/status-row-ctx-pair-leads-legend.md` — this record.
- `devdoc/DOC-INDEX.tsv` — its classification row.

## Not Done

- The ORDER claim of `TestStatusRowLegendEntryOrderMatchesTheRow` is asserted on
  the legend only (it cannot see the row). That was already true before this run
  and is not made worse by it; the row-side order is asserted by
  `TestStatusRowKeepsBothThePairAndTheRate` and the pty case.
- The legend's numeric samples are still not recomposed by a test: they were
  verified by measurement at authoring time (the probe above). A gate that
  recomposes each sample from the composers is the honest follow-up and was not
  invented here (Rule 3: no new machinery alongside a text fix).
- **Not verified here**: that the `≡` glyph's own help-gate
  (`TestStatusRowSymbolsHaveAReaderVisibleDefinition`) still resolves against the
  reworked section on a session that has compacted — it is green, but its
  compaction arm was not exercised on a live pty by this run.
- No geometry changed by this run: `statusLayoutBudgetCols` and every
  `statusMetrics*Col` are untouched (`git diff master -- cmd/agent/chat_stream.go`
  is empty), so `statusPinSlackCells` is still 0 and the pin 100.

## Verification

- **Build**: `go build ./...` → OK. `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (`bin/gopi version` → `v0.1.0`).
- **Vet**: `go vet ./cmd/... ./internal/...` → clean (no output).
- **Tests**: `go test ./cmd/agent/ -run 'TestStatus|TestChatStatus'` → `ok github.com/nick/gopi/cmd/agent 5.446s`
  (re-run after the delivery-point sync with master: `ok … 4.873s`).
  Plus the new gate alone: `-run TestStatusRowLegend` → 3/3 PASS
  (`TestStatusRowLegendQuotesTheRealCompactionRecord`,
  `TestStatusRowLegendEntryOrderMatchesTheRow`,
  `TestStatusRowLegendDefinesTheGroupsUnmeasuredWord`), and the ptys that render the
  real row: `go test ./testsuite/ -run 'TestChatStatusRowLadder|TestChatPTY_Context'`
  → `ok github.com/nick/gopi/testsuite 15.107s`. `go test ./internal/standards/` →
  `ok … 44.023s` (the doc/corpus gates this commit's devdoc row answers to).
- **Ran** (Rule 31): `bin/gopi chat -h` — the changed surface, the real verb, help
  printed to stdout (exit 0). Its THE STATUS ROW section prints, verbatim:
  `right-hand group is TWO sub-cells — the context pair, then the prompt cache's — carrying three read-outs about THIS session, in this order:`,
  then the `ctx  92k/1M≡` entry (FIRST), the `↑12k↓4k` cache-sub-cell entry
  (SECOND), the `hit 74.5%` entry riding it, and `no metrics` before the tools
  fact. Captured at
  `/home/user/.gopi/sessions/01a0faf0-2736-70e7-b711-53d3e037febe/chat-h.txt`; the
  same section captured again after the delivery-point sync
  (`chat-h-merged.txt`) is byte-identical, and the string `200k` appears nowhere in it.
- **Result**: the printed section matches the row the probe measured, entry for
  entry: the pair leads, the cache sub-cell follows and carries the rate, the
  empty state is named, and no sample states a form the composer cannot print
  (`200k` appears nowhere in the section).
- **Commit**: see the row's branch tip.

## Evidence (self-filed)

- Probe output (throwaway `cmd/agent/zz_probe_test.go`, deleted before the commit):
  `probe-row-forms.txt` in this run's session dir — the `formatCountBounded` table,
  the composed metrics field for the deployed window, the sub-cell widths `[12 8]`,
  and the cache composer's forms (`cacheRead=12000, cacheWrite=4100 → "↑12k↓4k hit 74.5%"`).
- `bin/gopi chat -h` output: `chat-h.txt` (same directory) — the changed surface, run for real.
- `go vet ./cmd/... ./internal/...`: empty output, exit 0.
- `go test ./cmd/agent/ -run 'TestStatus|TestChatStatus'`: `ok … 5.446s` (exit 0).
- Red-then-green for the new gate, both runs observed (quoted above).
- `git status --porcelain` in the worktree at close-out: see the run's own record.
