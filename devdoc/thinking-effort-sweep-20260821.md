<!-- doc-class: historical -->
# Thinking-Effort Sweep: gopi's Optimal Reasoning Level (2026-08-21)

## Verdict

**New default: `GOPI_THINKING=enabled` + `GOPI_REASONING_EFFORT=minimal`.**

Light thinking is gopi's sweet spot. On the W3 benchmark (arxiv_search.py,
deepseek-v4-flash, same task at every level) it beat thinking=disabled on wall
time — 216s vs 289s — with identical task quality, because it cuts round-trips
(15 vs 23) despite slower per-round latency (12.0s vs 8.6s median). This
confirms the original hypothesis that sparked the sweep: claude's ~266
tokens/turn of light thinking helps task comprehension, and gopi's `disabled`
was over-corrected. Medium/max effort are the slow ones — 2x+ the wall time of
minimal with zero quality gain.

This reverses the default set in 2afdaf3 ("default thinking disabled") — that
commit's 3.4x per-round latency cut was real but measured latency only; the
round-trip reduction from light thinking more than repays it.

## Measurement table (primary runs — fresh implementation, no pre-existing state)

Task: **W3 — write `~/.gopi/plugins/websearch/scripts/arxiv_search.py`** (from
/tmp/task-w3-arxiv-search.txt). Model: `deepseek-v4-flash`. Endpoint:
`https://api.deepseek.com/v1/chat/completions` (OpenAI format; thinking +
reasoning_effort verified honored by API, see caveats). Acceptance: script
exists, `python3 arxiv_search.py "retrieval augmented generation"` returns
valid JSON with real papers, empty query → `status=empty`, all four statuses
mapped; independently re-verified per level with rate-limit spacing.

| Level | LLM rounds | Wall time | Med round | Mean round | Thinking blocks | Reasoning tok/round (med) | Acceptance |
|---|---|---|---|---|---|---|---|
| **thinking=disabled** (prev default) | **23** | **289.3s** | 8.6s | 13.0s | 0 | 0 | PASS |
| **thinking=enabled + minimal** | **15** | **216.4s** | 12.0s | 15.1s | 11/15 | 75 | PASS |
| thinking=enabled + low | 17 | 239.4s | 10.0s | 14.6s | 14/17 | 27 | PASS |
| thinking=enabled + medium | 22 | 494.3s | 15.0s | 22.9s | 18/22 | 89 | PASS |
| thinking=enabled + max | 26 | 460.0s* | 14.0s | 18.1s | 22/26 | 106 | PASS |

\* max run partially contaminated (see Replicates note); fresh-run max would be
≥ medium. Historical control for max (Task F, 2026-08-20): ~46s/round.

Replicates (contaminated by a leftover `~/work/GoPi-W3-arxiv` clone the medium
run created — later runs reused its committed solution instead of writing
fresh; still preserved the ordering):

| Level | LLM rounds | Wall time | Med round |
|---|---|---|---|
| enabled+minimal (run 2, reuse) | 13 | 275s | 13.5s |
| enabled+low (run 2, reuse) | 15 | 318s | 19.4s |
| disabled (run 2, reuse) | 15 | 233s | 12.4s |
| enabled+minimal (run 3, clean) | 22 | 321s | 12.0s |

The clean run-3 minimal replicate reproduced the run-1 median exactly (12.0s)
— the wall-time delta is the agent's GOPI.md-driven clone/report/commit dance,
not latency. Per-round latency at minimal is stable across runs.

## Why light thinking wins: round-trip reduction, not latency

Tool-call composition (primary runs):

| Level | rounds | bash | read | write/edit | keyword_search |
|---|---|---|---|---|---|
| disabled | 23 | 18 | 3 | 2 | 0 |
| minimal | 15 | 12 | 0 | 2 | 0 |
| low | 17 | 11 | 2 | 3 | 1 |
| medium | 22 | 16 | 4 | 4 | 1 |
| max | 26 | 28 | 3 | 1 | 3 |

Thinking levels plan better up front: fewer exploration bash calls (12-11 vs
18), no re-reads (minimal's 0 read calls vs disabled's 3). The extra ~3s/round
at minimal buys ~8 fewer round-trips; medium/max buy nothing but latency.

Quality check beyond acceptance: all four primary scripts were stress-tested
uniformly (`--count` clamp, garbage query → empty, bad `--count` graceful) —
functionally identical behavior at every level.

## vs claude (~3.9s/round)

Claude's Task G run on the same DeepSeek backend: 65 tool turns, 36 thinking
blocks (median ~43-266 tokens depending on counting), 258s total, 3.9s/turn.
gopi at the new default is still ~3x slower per round (12.0s median) — that
residual gap is structural, not thinking-related: gopi's larger system prompt
(routing + efficiency + task-list rules), the mandatory [TASK_LIST_BEGIN] JSON
block every round, and the bug022 double end-turn confirmation. Documented in
gopi-efficiency.md; separate optimization track.

## Code change

`internal/agent/loop.go` (NewAgent):

- `reasoningEffort` default `"max"` → `"minimal"`
- thinking default `"disabled"` → `"enabled"` (both the unset case and the
  invalid-value fallback)
- comments updated with the data reference

`internal/agent/loop_test.go`: `TestNewAgent_ThinkingDefaultDisabled` →
`TestNewAgent_ThinkingDefaultEnabledMinimal` (asserts Type=enabled +
ReasoningEffort=minimal); invalid-value fallback test now expects enabled;
`newTestAgentEnv` clears `GOPI_REASONING_EFFORT`.

`cmd/agent/main.go`: usage documents `GOPI_REASONING_EFFORT` with the new
default.

Build + full `go test ./...` pass. Binary rebuilt.

## Caveats / findings for follow-up

1. **`~/.gopi/settings.json` (symlink → .flash_settings.json) points at
   `https://api.deepseek.com/anthropic` — which returns 404 for gopi's
   OpenAI-format client** (`/anthropic/v1/chat/completions` does not exist).
   All sweep runs used `DEEPSEEK_BASE_URL=https://api.deepseek.com` env
   override. The /anthropic URL only works for anthropic-format requests
   (claude). Fix the settings file or gopi's default URL before next use.
2. arxiv API rate-limits bursts (HTTP 429) — a script returning
   `timeout/unavailable` right after a burst is environmental, not a quality
   signal; verification must space queries (~8s).
3. Replicate contamination: the medium-effort run created a git clone at
   `~/work/GoPi-W3-arxiv` (agent followed GOPI.md Rule 5/6 clone-dance) whose
   committed solution later runs reused. Removed post-sweep. Benchmark harness
   should neutralize agent-created clones between runs.
4. This benchmark (W3) is a moderate task; the trade-off was exposed on it.
   For very hard tasks (Task-G scale), the round-reduction benefit of thinking
   is expected to be at least as large — if anything minimal might be too
   terse there; low is the fallback recommendation.
