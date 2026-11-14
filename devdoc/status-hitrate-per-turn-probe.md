<!-- doc-class: historical -->
# The chat status row's hit rate — the PER-TURN ("本轮") differential, measured


<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task row: `01a10f4e5bba7a43a7b852ae8241259e`, the A (evidence) half of the epic.
The question this document answers is narrow and mechanical: **is there a
"本轮" (this-turn) reading distinct from the cumulative one the row prints, and
is it built from the SAME pair of counters?** Nothing on the row was changed —
no geometry, no composer, no numerator/denominator definition. Every number
below is output of a throwaway probe
(`cmd/agent/zz_probe_test.go`, **deleted before the commit that lands this
document**: Rule 30, `git status --porcelain` prints nothing).

## 0. The answer, in three lines

1. **Same pair, both readings.** The row reads `↑/↓` and `hit%` out of exactly
   two fields — `Agent.Cost.TotalCacheHitTokens` / `TotalCacheMissTokens`
   (`statusOpsFor`, `cmd/agent/cli_chat.go`) — and those are the same two fields
   the run accumulates per request (`CostTracker.AddClaudeUsage`) and the same
   two the per-request differential subtracts (`loop.go`: `a.Cost.TotalCacheHitTokens
   - prevHit`). One pair of counters, read at two moments.
2. **The arithmetic.** With `C_i` the pair's value after the session's i-th
   request, `h_i := C_i.hit - C_{i-1}.hit` and `m_i := C_i.miss - C_{i-1}.miss`,
   the turn is `dh_turn = Σ_{i ∈ turn} h_i`, `dm_turn = Σ_{i ∈ turn} m_i`, and
   `本轮 hit% = dh_turn / (dh_turn + dm_turn)`. Summing telescopes:
   `dh_turn = C_last.hit - C_{before}.hit` — a difference of two readings of the
   pair. Per request `p_i = h_i + m_i` (the Claude transport's
   `PromptSideTokens() = input + cache_creation + cache_read`), so
   `dp_turn = dh_turn + dm_turn` and `Σ_i p_i = TotalPromptTokens`.
3. **They diverge, and by a lot.** Measured below: a warm session whose
   cumulative reading sits at `81.7%` prints `10.0%` for the turn that had just
   happened — a **-71.7 point** gap at the same moment on the same screen.

## 1. The probe, and what it drove

The probe drove the PRODUCTION path, not a re-derivation: three real
`Agent.ChatOps` turns against a scripted SSE transport, every reading taken
through the row's own source function (`statusOpsFor`), every displayed number
spelled by the row's own formatter (`statusHitRatePct`). Turn 1 was scripted to
cost **two** requests (a tool round and the answer round), which is what makes
the per-turn reading a SUM rather than one request's own number.

Scopes: `(a)` same pair of fields, `(b)` the arithmetic, `(c)` the divergence.
Raw output: session directory
`~/.gopi/sessions/01a10f4e-5bba-7a43-a7b8-52ae8241259e/artifacts/probe-per-turn-output.txt`.

### (a) One pair of counters, two readings

```
(a) the row's source: statusOpsFor -> Agent.Cost.{TotalCacheHitTokens,TotalCacheMissTokens}
(a) statusOpsFor(ag).cacheRead=14400 == ag.Cost.TotalCacheHitTokens=14400 : true
(a) statusOpsFor(ag).cacheWrite=7100 == ag.Cost.TotalCacheMissTokens=7100 : true
(a) the row's own rendering: pair="↑14k↓7k" rate="67.0%" | same pair through the production formatter: "67.0%" : true
(a) the JSONL rows carry that SAME pair (cumulative snapshots on this path):
(a)   row 0: hit=  9000 miss=  1000 prompt= 10000 | p_i == h_i+m_i: true
(a)   row 1: hit=  9300 miss=  1200 prompt= 10500 | p_i == h_i+m_i: true
(a)   row 2: hit=  9400 miss=  2100 prompt= 11500 | p_i == h_i+m_i: true
(a)   row 3: hit= 14400 miss=  7100 prompt= 21500 | p_i == h_i+m_i: true
(a) the reading taken at a turn boundary EQUALS the last row of that turn:
(a)   after turn 1: statusOpsFor=9300/1200/10500 | JSONL row 1 =9300/1200/10500 | equal: true
(a)   after turn 2: statusOpsFor=9400/2100/11500 | JSONL row 2 =9400/2100/11500 | equal: true
(a)   after turn 3: statusOpsFor=14400/7100/21500 | JSONL row 3 =14400/7100/21500 | equal: true
```

**Sub-finding, and the one that surprises.** The `request` rows the session
JSONL carries are **not** deltas on the chat path: `chat_ops.go` writes the
CostTracker's cumulative totals and says so in its own comment ("Cumulative
totals … no per-request delta here"), while the *task* path
(`loop.go` `runEngine`, `LogRequest(i, p, h, m, comp)` against
`prevPrompt/prevHit/prevMiss/prevCompletion`) writes the **difference** of the
same fields. The two paths therefore write different quantities into the same
event shape. **A probe that assumed "the JSONL row IS h_i" would report a false
defect** — the probe's own first draft did exactly that (it summed the rows as
if each were a delta and got a sum that cannot equal the cumulative counter),
which is why the differential below is defined as a difference of consecutive
readings rather than as the row's own number. **Reading the JSONL rows as
deltas is a trap**; the honest reading of a chat-path row is "the pair as it
stood after that request".

### (b) The arithmetic, checked against the counters

```
(b) per-request differentials h_i := C_i - C_{i-1} (C_{-1} = 0, the session start):
(b)   request 0: h_i=  9000 m_i=  1000 p_i= 10000 | p_i == h_i+m_i: true | this request's own rate 90.0% | cumulative after it ↑9000↓1000 (90.0%)
(b)   request 1: h_i=   300 m_i=   200 p_i=   500 | p_i == h_i+m_i: true | this request's own rate 60.0% | cumulative after it ↑9300↓1200 (88.6%)
(b)   request 2: h_i=   100 m_i=   900 p_i=  1000 | p_i == h_i+m_i: true | this request's own rate 10.0% | cumulative after it ↑9400↓2100 (81.7%)
(b)   request 3: h_i=  5000 m_i=  5000 p_i= 10000 | p_i == h_i+m_i: true | this request's own rate 50.0% | cumulative after it ↑14400↓7100 (67.0%)
(b) sum_i h_i=14400 == TotalCacheHitTokens=14400 : true
(b) sum_i m_i=7100 == TotalCacheMissTokens=7100 : true
(b) sum_i p_i=21500 == TotalPromptTokens=21500 : true | == sum_i h_i + sum_i m_i = 21500 : true
(b) per-turn sums (the turn's own differential):
(b)   turn 1 (2 request(s), rows 0..1): dh_turn=  9300 dm_turn=  1200 dp_turn= 10500
(b)     telescopes onto the pair: dh_turn == after.hit-before.hit (9300-0) : true | dm_turn == after.miss-before.miss (1200-0) : true | dp_turn == after.prompt-before.prompt (10500-0) : true
(b)     dp_turn == dh_turn + dm_turn : true | the turn's own reading dh_turn/(dh_turn+dm_turn) = "88.6%"
(b)   turn 2 (1 request(s), rows 2..2): dh_turn=   100 dm_turn=   900 dp_turn=  1000
(b)     telescopes onto the pair: dh_turn == after.hit-before.hit (9400-9300) : true | dm_turn == after.miss-before.miss (2100-1200) : true | dp_turn == after.prompt-before.prompt (11500-10500) : true
(b)     dp_turn == dh_turn + dm_turn : true | the turn's own reading dh_turn/(dh_turn+dm_turn) = "10.0%"
(b)   turn 3 (1 request(s), rows 3..3): dh_turn=  5000 dm_turn=  5000 dp_turn= 10000
(b)     telescopes onto the pair: dh_turn == after.hit-before.hit (14400-9400) : true | dm_turn == after.miss-before.miss (7100-2100) : true | dp_turn == after.prompt-before.prompt (21500-11500) : true
(b)     dp_turn == dh_turn + dm_turn : true | the turn's own reading dh_turn/(dh_turn+dm_turn) = "50.0%"
```

So **`本轮` needs no new counter and no new field**: it is the pair's own
movement across the turn's request span, and on a single-request turn it is
simply `C_turn_end - C_turn_start` on the same two fields. The relation to
`TotalPromptTokens` is exact and is the same identity the transport guarantees
per request: `dp_turn = dh_turn + dm_turn`, and `Σ_turns dp_turn = TotalPromptTokens`.

### (c) The divergence — the same moment, two scopes, two numbers

```
(c) boundary CUMULATIVE (the row's reading now) PER-TURN (the turn's differential) divergence
(c) turn 1   88.6% = ↑9k↓1k                 88.6% = ↑9k↓1k                 +0.0 points  (cumulative tokens ↑9300↓1200 | this turn's ↑9300↓1200)
(c) turn 2   81.7% = ↑9k↓2k                 10.0% = ↑100↓900               -71.7 points  (cumulative tokens ↑9400↓2100 | this turn's ↑100↓900)
(c) turn 3   67.0% = ↑14k↓7k                50.0% = ↑5k↓5k                 -17.0 points  (cumulative tokens ↑14400↓7100 | this turn's ↑5000↓5000)
```

Turn 1 is the degenerate case that makes the point sharpest: on the FIRST turn
of a session the two scopes are equal, because the cumulative reading has
nothing behind it yet (`88.6%` both). From turn 2 on they are two different
numbers, and turn 2's is not a rounding difference — the row would have said
`81.7%` while the turn that just ran was `10.0%`. **This is the funding case for
a second read-out**: not "the algorithm is wrong" but "the number is answering a
different question than a reader assumes".

### The same arithmetic on a REAL session of this machine

No script, no synthetic counters — `~/.gopi/sessions/01a0fa45-f7f2-7e3f-8e5f-674013692f2c/session-284.jsonl`
(70 request rows, `deepseek-flash`, 2026-10-02, a `gopi worker` run):

```
(real) p_i == h_i + m_i for every row: true | the pair is non-decreasing (cumulative snapshots): true
(real)   row   0 iter=0: h_i=   1024 m_i=  31662 p_i=  32686 | this request alone 3.1% | cumulative ↑1024↓31662 = 3.1%
(real)   row   1 iter=1: h_i=  32640 m_i=    646 p_i=  33286 | this request alone 98.1% | cumulative ↑33664↓32308 = 51.0%
(real)   row   2 iter=2: h_i=  33408 m_i=    189 p_i=  33597 | this request alone 99.4% | cumulative ↑67072↓32497 = 67.4%
(real)   row   3 iter=3: h_i=  33664 m_i=    250 p_i=  33914 | this request alone 99.3% | cumulative ↑100736↓32747 = 75.5%
(real)   row   4 iter=4: h_i=  33792 m_i=   1815 p_i=  35607 | this request alone 94.9% | cumulative ↑134528↓34562 = 79.6%
(real)   row   5 iter=5: h_i=  35712 m_i=   1100 p_i=  36812 | this request alone 97.0% | cumulative ↑170240↓35662 = 82.7%
(real)   row  69 iter=3: h_i= 118528 m_i=   1394 p_i= 119922 | this request alone 98.8% | cumulative ↑5036800↓106175 = 97.9%
(real) last turn = rows 66..69 | dh_turn=470400 dm_turn=2487 dp_turn=472887 | the turn's own reading 99.5% vs the session's cumulative 97.9% | divergence +1.5 points
```

On this real session the divergence is small (`+1.5` points) *because* the
session is long and uniformly warm — 70 requests of which the first six climb
from `3.1%` to `82.7%` and the rest sit near `98%`. That is the honest other
side of the finding: **the per-turn reading is not always dramatically
different; it is different whenever the turn is unlike the session's average**,
and the warm-up rows are where the two scopes are furthest apart (row 0: a
cumulative `3.1%`, the very next request `98.1%`). A reader doubting a `97.9%`
that "feels too good" is asking for the wrong instrument; the row's number is a
correct average over a session that really did warm up.

## 2. What was NOT done (this is the A half, and the boundary held)

Nothing on the row changed. Concretely, none of the following exists in this
branch, and each is a display decision this run did not make:

1. **No per-turn read-out on the row.** `statusOps` still carries the pair and
   nothing else; there is no `dh_turn`/`dm_turn` field, no composer, and no
   legend sentence naming the scope. The row prints what it printed before.
2. **No geometry touched.** `statusPinCols == statusColumnsAtCols ==
   statusLayoutBudgetCols == 100`, `statusPinSlackCells == 0`, and every
   reservation is still exactly drawn — the previous measurement
   (`devdoc/status-hitrate-scope-probe.md` §3) stands: no top-level column is a
   donor and the metrics group's seam `statusMetricsSep` (2 cells) is the only
   fundable cell range, against a ~10-cell need.
3. **No numerator/denominator definition changed.** `hit%` is still
   `read/(read+write)`; the per-turn reading measured here is the SAME quotient
   applied to the pair's movement, spelled through the same formatter
   (`statusHitRatePct`) — measurement, not a new definition.
4. **No writer/reader pair added**, so the Rule 14 hazard the previous probe
   recorded (a second reading needs its suffix constant shared by composer and
   `chat_status_hitrate_emphasis.go`, or the tier colouring fails silently) is
   still unopened — it belongs to whoever funds the cells.
5. **The probe file is gone.** It existed to produce §1 and was deleted before
   this document's commit; `git status --porcelain` prints nothing.
6. **Where a term would come from, if the epic continues** — stated because the
   next run would otherwise re-derive it: the turn boundary the differential
   needs is `statusOpsFor`'s own snapshot at the `cli_chat.go` turn end
   (`totalTokens = ag.Cost.TotalPromptTokens + TotalCompletionTokens` is read
   there, on the same counters), and the natural (but unimplemented) shape of a
   turn-start snapshot is a `Δsince` taken from that pair at the same place the
   cumulative reading is taken. Nothing about that shape was designed here.

## Verification

- Build: `go build ./...` → OK (exit 0, no output).
- Tests: `go test ./cmd/agent/ -run 'TestZZProbePerTurnCostDelta' -v` →
  `--- PASS: TestZZProbePerTurnCostDelta (0.03s)`, `ok github.com/nick/gopi/cmd/agent`
  (the probe file is deleted before this document's commit; the output is
  archived under the session directory).
- Ran: `GOPI_PROBE_REAL_SESSION=/home/user/.gopi/sessions/01a0fa45-f7f2-7e3f-8e5f-674013692f2c/session-284.jsonl go test ./cmd/agent/ -run 'TestZZProbePerTurnCostDelta' -v`
  — the exact invocation; its output is verbatim in §1 (three real `ChatOps`
  turns through a scripted SSE transport + one real 70-request session JSONL).
- Result: `(a)` `statusOpsFor(ag).cacheRead/cacheWrite ==
  Cost.CostTracker.TotalCacheHitTokens/TotalCacheMissTokens` → `true/true`, and
  the reading at each turn boundary equals that turn's last JSONL row
  (`true/true/true`); `(b)` `Σ_i h_i = 14400 == TotalCacheHitTokens`,
  `Σ_i m_i = 7100 == TotalCacheMissTokens`, `Σ_i p_i = 21500 ==
  TotalPromptTokens`, and every turn's per-request sum telescopes onto the
  pair's difference across that turn (`dp_turn == dh_turn + dm_turn` too) —
  all `true`; `(c)` divergences `+0.0 / -71.7 / -17.0` points (scripted) and
  `+1.5` points (real session).
- Commit: the commit that lands this document — `docs(status-hitrate): measure
  the per-turn differential and prove it shares the cumulative pair` (its hash,
  and the run's own close-out record, are in the session directory's
  `task-report.md`).

## Evidence (self-filed)

- Probe run (real session): `go test ./cmd/agent/ -run
  'TestZZProbePerTurnCostDelta' -v` with `GOPI_PROBE_REAL_SESSION=…/session-284.jsonl`
  → `--- PASS` and the `(a)/(b)/(c)/(real)` lines quoted in §1. Raw capture:
  `~/.gopi/sessions/01a10f4e-5bba-7a43-a7b8-52ae8241259e/artifacts/probe-per-turn-output.txt`
  (7630 bytes, 49 probe log lines).
- The JSONL-rows-are-cumulative claim: measured, not read off the source. The
  probe's own first draft treated a chat-path row as if it were that request's
  own delta, and its reconciliation against the cumulative counter failed (the
  summed "deltas" did not equal the counter they were supposed to be the parts
  of); the corrected form is the readings-vs-rows comparison in §1(a)
  (`equal: true/true/true`) plus the `Σ_i h_i == TotalCacheHitTokens`
  identity in §1(b) (`true`).
- Worktree cleanliness: `git status --porcelain` in
  `/home/user/workspace/gopi-01a10f4e5bba7a43a7b852ae8241259e` after removing
  the probe — the run's own close-out record is authoritative for this.

## Not Done (honest — Rule 26)

- **No display change was made** — by design (the epic's boundary): the
  implementation half (a second read-out, its cells, its legend, its
  composer/reader pair) is a separate task, and §2 lists the pieces it needs.
- **The probe measured three turns of a scripted session and one real session**
  (70 rows). It does not measure a turn that spans a *tool-heavy* round set
  beyond the two rounds scripted here, and it does not measure an interrupted
  turn; both are places the differential would need a definition (does an
  abandoned turn's partial usage count toward 本轮?), and neither is decided
  here.
- **The real-session figure comes from one session**; it is evidence of the
  arithmetic on real counters, not a distribution.
