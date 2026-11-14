<!-- doc-class: normative -->
# Thinking Budget: what it bounds, why thinking gets paused, and what the numbers should be (2026-09-15)

## The operator's question

> `[gopi] ⚠️ thinking exceeded the budget 2 rounds in a row — disabling thinking for the rest of the session`
> What does this mean? Is the thinking budget the Anthropic `budget_tokens`? Does it affect long
> tasks? Why disable thinking for the whole session? Is the budget per session or per round?

Short answers, then the evidence:

| Question | Answer |
|---|---|
| Per session or per round? | **Per API request (per round).** Every round sends its own `thinking.budget_tokens`; nothing accumulates across the session. |
| Is it the Anthropic `budget_tokens`? | It is *sent* as `budget_tokens`, but **the endpoint ignores that field** — so the session was actually enforced by gopi's own harness. |
| Does the pause affect long tasks? | Yes — that was the bug. One runaway round near the start used to cost **every later round** its reasoning (the latch was never cleared). Fixed in this branch. |
| Why pause at all? | An over-budget round costs **two full prefills** (the aborted attempt + the re-issue). Two in a row is the runaway signal; pausing bounds the waste. |

## 1. Mechanism (source-verified)

- The budget is resolved once at agent construction: `settings.json thinking_budget` →
  `GOPI_THINKING_BUDGET` → `Thinking.BudgetTokens` (`internal/agent/loop.go`, `NewAgent`).
- **Every request** carries it (`BuildClaudeRequest` → `ClaudeThinking.BudgetTokens`), so the budget is
  per-round. The API requires `budget_tokens < max_tokens`, so the builder clamps it to
  `max_tokens/2` (`deepseek.EffectiveThinkingBudget`).
- **The endpoint does not enforce it.** Live probe 2026-08-21 and re-measured 2026-09-15 against
  `deepseek-v4-flash` (`/anthropic/messages`):

  | request | streamed thinking | text | stop |
  |---|---|---|---|
  | `budget_tokens=128` | 6,148 chars (~1,537 tok) | *(none)* | `max_tokens` |
  | `budget_tokens=1024` | 6,526 chars (~1,631 tok) | *(none)* | `max_tokens` |
  | `budget_tokens=2048` | ~15k tok | *(none)* | `max_tokens` |
  | `reasoning_effort=minimal` | 29,016 chars (~7,254 tok) | *(none)* | `max_tokens` |
  | `reasoning_effort=high` | 27,092 chars (~6,773 tok) | *(none)* | `max_tokens` |
  | omitted | 23,834 chars (~5,958 tok) | *(none)* | `max_tokens` |

  Three budgets three orders of magnitude apart produced the same runaway, and `reasoning_effort` is
  ignored too. So **neither knob is a cap on this endpoint**; only `thinking: disabled` is honored
  (0 thinking, full text). The harness must do the bounding — and it does:
  - streaming path: the stream is **aborted** once streamed thinking ≥ `budget × 4` chars
    (`thinkingCharsPerToken = 4`), the round is re-issued with thinking disabled;
  - non-streaming path: the over-budget response is discarded post-hoc and re-issued.
- Probe recipe (reusable, ~2 min, run detached): `/dev/shm/gopi-probe/probe.sh` — direct `curl` to the
  endpoint with the key from settings.json. **Do not** trust a single budget for this: the point is
  that the field is ignored, which only shows up across several values.

## 2. How often it fires (census, 14 days)

Source: `~/.gopi/sessions/<sid>/*.jsonl` — every assistant round's thinking text is stored.
804 session files with thinking, 75,574 thinking rounds.

| thinking chars/round | p50 | p75 | p90 | p95 | p99 | max |
|---|---|---|---|---|---|---|
| chars | 876 | 2,417 | 6,173 | 10,526 | 21,611 | 32,527 |

Applying the harness abort rule (abort at `budget × 4` chars):

| budget | rounds aborted | sessions that hit a streak-2 pause |
|---|---|---|
| 1024 (shipped default) | 11,861 (15.7%) | 557 / 804 (**69%**) |
| 2048 | 7.1% | 396 / 804 (49%) |
| 4096 | 2.2% | 163 / 804 (20%) |
| 8192 (the operator's effective value today) | **0.0%** | 0 / 804 |

**Raw-log census (uncensored source).** `~/.gopi/raw/gopi-raw-*.log` is captured stderr, so aborted rounds
ARE visible there (unlike JSONL): over 2026-08-21 → 2026-09-05, **29 of 1433 raw logs carry the banner,
2,126 banner lines in total** (worst files: 330, 271, 198, 134, 110 lines). So this is not a rare corner —
on a day of heavy use the harness aborts thinking dozens of times, and each abort is a second prefill.

**Methodological caveat (important):** an aborted round returns nothing and is **never written to the
session JSONL**, so JSONL-only counts cannot observe aborts — the real frequency has to come from the
banner lines in `~/.gopi/logs/worker.log`. Those do exist in production: 16 genuine
"re-issued with thinking disabled" lines and 4 genuine streak-2 lines in the current log file
(the remaining grep matches are agents' own commands quoting the banner — filter for a leading
`[gopi]`). Filtering matters: `grep` over session logs self-matches.

**Independent sweep of the session raw logs** (604 files under `~/.gopi/sessions/*/*.raw.log`, which
capture stderr and therefore DO see aborts): only 4 files contain the banner string, and two of them
are the investigating session itself (2026-09-15/16 — its own `grep` commands quote the string, i.e.
the self-match trap again). The uncontaminated historical instances are **2026-09-09 (40 banner
lines)** and **2026-09-10 (119 banner lines)** — two different sessions that each re-issued
thinking-disabled rounds dozens of times in a single run. So the phenomenon is real and recurrent,
but it clusters in specific sessions rather than being background noise.

## 3. What was wrong (three defects, all fixed here)

1. **The banner lied about the scope.** It said "for the rest of the session"; the latch is an
   in-memory `Agent` field — it never survives the process, and the accurate unit is *this run*.
   Fixed wording: "pausing thinking for this run (retried once every 8 rounds; this is per-run state,
   not a session setting)".
2. **A silent clamp (Rule 15).** `settings.json thinking_budget=16384` with the default
   `max_tokens=16384` yields an **effective 8192**, because the builder halves it. The operator could
   not see that — and the clamp is also the abort point. Now reported once per run:
   `ℹ️ thinking budget 16384 clamped to 8192 (max_tokens/2 = 16384/2) … Raise GOPI_MAX_TOKENS …`.
3. **The downgrade was permanent — the real defect.** `thinkingForceDisabled` was set and never
   cleared, so **one runaway round cost thinking for the entire run**. This is what makes the banner
   alarming for long tasks, and it is why the fix is a *pause with a recovery probe*, not just
   wording:
   - two consecutive over-budget rounds pause thinking;
   - every `thinkingProbeSpacing = 8` rounds, **one thinking-enabled probe round** goes out;
   - a probe that answers within budget **clears the pause** (thinking returns for good);
   - a probe that blows the budget **re-engages the pause immediately** — the round is still
     re-issued thinking-disabled, so no second double-prefill is paid.

   Measured red proof: with the old permanent behaviour the census-style test shows rounds 3–19 all
   `disabled` with no probe at any point (`TestAgent_Run_ThinkingRecoveryProbeRestoresThinking` fails
   pre-fix, passes post-fix); the new tests also pin the re-latch and the probe cadence.

## 4. Config re-estimate for deepseek-v4.1-flash

- Do **not** rely on `budget_tokens`/`reasoning_effort` to bound thinking — the endpoint ignores both.
  The budget value you set is a *harness abort threshold*, nothing more.
- The shipped default `DefaultClaudeThinkingBudget = 1024` is a booby trap **for runs that enable
  thinking without setting a budget**: 69% of the censused sessions would have hit a pause. It is only
  reachable when thinking is explicitly enabled (thinking is disabled by default), so the exposure is
  opt-in.
- The operator's `thinking_budget=16384` is the right order of magnitude: its effective 8192 made
  aborts vanish from the census (0/804). If you want the *configured* number to be what is enforced,
  raise `max_tokens` (`GOPI_MAX_TOKENS`) to at least `2 × budget`.
- We deliberately did **not** change the default budget in this branch: the census that condemns 1024
  is censored (aborted rounds are invisible in JSONL), so the tradeoff — a low budget wastes less
  generation on a runaway round but aborts more often — is not resolved by the data. What *is*
  resolved: pausing must be recoverable (fixed), and the clamp must be visible (fixed).

## 5. Constants (Rule 1b/13/14)

The thinking wire values were inlined at ~18 production sites across `agent`, `deepseek`, `config`,
`proxy` and `tools`, plus a mirror constant (`hippoThinkingDisabled`). They now live in the leaf
package **`internal/thinkmode`** (`Enabled`/`Disabled`), every site references it, the mirror constant
is deleted, and `TestThinkingTypeLiteralGate` fails any production file that re-inlines the value at
a thinking-type site (baseline-free, comments excluded).

## Verification

- `CGO_ENABLED=0 go build ./...` → OK
- `go test ./internal/agent/ ./internal/deepseek/ ./internal/thinkmode/ ./internal/proxy/ ./internal/config/ ./internal/tools/ ./internal/standards/` → see report; new tests:
  `TestAgent_Run_ThinkingRecoveryProbeRestoresThinking`, `TestAgent_Run_ThinkingProbeReLatch`,
  `TestAgent_ReportEffectiveThinkingBudget`, `TestAgent_ThinkingPausedForRound`,
  `TestThinkingTypeLiteralGate` (+ the pre-existing `TestAgent_Run_ThinkingDowngradeLatch`).

## Not done (honest)

- The shipped default `DefaultClaudeThinkingBudget = 1024` is **unchanged** (see §4) — flagged for an
  operator decision, not silently tuned.
- `GOPI_THINKING_BUDGET` still cannot make the *endpoint* stop thinking (verified again 2026-09-15);
  only `thinking: disabled` is honored. A future protocol that honors `budget_tokens` would let the
  harness cap be retired.
- The census covers this host's 14-day window only; the abort counts rest on the worker log (JSONL is
  censored for aborted rounds).
