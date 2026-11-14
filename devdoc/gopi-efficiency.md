<!-- doc-class: historical -->
# Task Report: Make gopi as efficient as claude — parallel calls, act-when-ready (head-to-head)

## Design decision

**Where the fix lives: BUILTIN — a new `efficiencyRules` block in the kernel
system prompt (`internal/agent/loop.go`), plus a read-tool substitution note
embedded in the `bash` tool description.**

Why builtin, not GOPI.md / pluggable XXX.md:

1. **Same category as the existing rule blocks.** `toolRoutingRules` and
   `taskManagementRules` are both builtin consts marked "framework-level, not
   user-configurable". Efficiency behavior is the same kind of framework-level
   guidance — it should be on in every gopi run, by default.
2. **GOPI.md is per-project and optional.** A fix living there would only apply
   to projects that opt in, and the head-to-head would measure "project file
   present" rather than the framework. The goal is a permanent improvement to
   gopi itself.
3. **Pluggable adds machinery, not value.** A separate XXX.md needs loading
   code, precedence rules, and discovery — there is no consumer for
   per-config efficiency prompts today.
4. **Bloat is bounded.** The block is ~200 tokens; the kernel prompt was
   ~1.2k and sits in a 1M-token context. Acceptable.

The alternative that WAS rejected: putting the "read files with read, NOT
cat/head/tail" note in the system prompt. Claude Code embeds it in the **Bash
tool description** (tool-description-bash-alternative-read-files.md) — that is
where the model looks when deciding to `cat` a file, so we copied that
mechanism exactly.

## What the fix changes (commit 9c79e32)

1. **`efficiencyRules` block** appended to the system prompt (after routing
   rules, before task management):
   - CALL TOOLS IN PARALLEL: emit all independent tool calls in one response;
     the execution layer runs read/code_search/keyword_search batches
     concurrently. Sequential only when a later call depends on an earlier one.
   - ACT WHEN READY: don't re-derive established facts, re-read files, or
     narrate unpursued options; give a recommendation, not a survey.
   - NEVER sleep or poll: run checks directly, no sleeps before them.
   - BE CONCISE: lead with the result, no preamble/recap; correctness never
     traded for brevity.
2. **bash tool description** now reads "Read file contents with the read tool
   (NOT cat/head/tail)."

No execution-layer changes were needed: `executeToolBatch` already runs
all-read-only rounds concurrently with ordered results (5fbcb78), and the
streaming merge handles multi-call batches — the bottleneck was purely that
the model never emitted more than one tool call per round.

## Evidence the prompt was the bottleneck

In the Task F session (90dce872, 2026-08-20, 83 iterations, 19 min):
85/85 assistant rounds contained exactly **one** tool_use block. The parallel
batch executor was never exercised — it was dead code until the model learned
to batch. Tool usage in that run: 52 bash, 16 edit, 14 read, 0 code_search
(despite routing rules).

## Head-to-head method

- Task: **Task G — extend code_search to index JavaScript/TypeScript**
  (add .js/.jsx/.ts/.tsx/.mjs/.cjs to the ctags language list, extension set,
  kind map; extend tests; update docs). Deliberately comparable in shape and
  complexity to Task F ("extend code_search to multiple languages").
- Three isolated clones of the repo, same task text, same model
  (deepseek-v4-flash, same API key/endpoint):
  - `bench-before` @ feat/code-search HEAD (5c4994a) — gopi WITHOUT the fix
  - `bench-after`  @ feat/gopi-efficiency HEAD (9c79e32) — gopi WITH the fix
  - `bench-claude` @ feat/code-search HEAD (5c4994a) — Claude Code 2.1.204,
    same deepseek-v4-flash backend, effort max
- Project context parity: gopi reads GOPI.md (repo, unchanged); the claude
  clone got a CLAUDE.md with the identical rules (the repo has none).
- gopi metrics from its session JSONL (`session_end` event + `request` count);
  claude metrics from `claude -p --output-format stream-json --verbose`.

## Benchmark integrity note (important)

The first head-to-head attempt was INVALID and had to be redone. gopi follows
GOPI.md Rule 6: it clones `~/work/GoPi` and works in the clone. Both initial
"before" and "after" runs therefore cloned the main repo while its HEAD was
`feat/gopi-efficiency` (the fix commit) — **both runs actually ran WITH the
efficiency rules** (verified: both pushed branches whose parent commit is
9c79e32, the fix). The claude run was unaffected (its worktree was pinned at
5c4994a by checkout).

The true baseline run was redone with the main repo's HEAD pinned at
`feat/code-search` (5c4994a) for the duration of the run, so the agent's
Rule-6 clone checked out the pre-fix state (verified in the raw request log:
the baseline system prompt contains no EFFICIENCY RULES block). The two
initial "with fix" runs are still valid data — they are two independent
replicates of the with-fix condition and agree closely (44 and 46 iterations).

## Results (real runs, same task, same model — deepseek-v4-flash)

Task: **Task G — extend code_search to index JavaScript/TypeScript**. All four
runs COMPLETED the task: code changes committed, full `go build` + `go test
./...` pass on every branch (re-verified independently).

| Condition | LLM round-trips | Wall time | Avg round gap | Multi-call rounds | Total tokens |
|---|---|---|---|---|---|
| **gopi BEFORE fix** (5c4994a prompt) | **39** | **1850s (30.8m)** | 48.4s | 9 of 37 (24%) | 2,035,540 |
| gopi WITH fix, run 1 | 46 | 2123s (35.4m) | 47.0s | 10 of 43 (23%) | 1,443,952 |
| gopi WITH fix, run 2 | 44 | 2017s (33.6m) | 46.5s | 5 of 41 (12%) | 929,739 |
| **claude** (Code CLI 2.1.204, same backend) | **66 turns** (65 tool turns) | **258s (4.3m)** | 3.9s | 0 | — |

Deliverable commits: baseline → `69c58d3` (in /tmp/bench-before, branch
feat/code-search); with-fix → `5f58f11` + `87eddde` (branch
feat/code-search-js-ts) and `aaf350f` (branch feat/g-js-ts-index); claude →
`f410ae5` + `f52f239` (branch feat/task-g-js-ts). All three solutions are
functionally equivalent and high quality — all three independently discovered
that universal-ctags ≤ 5.9 does not map `.cjs`/`.tsx` by default and added
`--map-JavaScript=+.cjs --map-TypeScript=+.tsx` (the two gopi branches even
caught that comma-joined `--map-*` options only apply the first mapping).

## Verdict — did gopi close the gap? NO, and the premise was partly wrong.

**1. The iteration-count premise did not reproduce.** The task brief claimed
gopi runs 68 iterations vs claude's 20-30. On this task: gopi-before ran 39
iterations, claude ran 66 turns — gopi used FEWER round-trips than claude.
The 68-83-iteration pattern in the historical logs came from bigger tasks
(Task F: 83 iters) under an older prompt state (pre-code_search routing) and
a different reasoning config.

**2. The prompt fix had no measurable effect on iterations or wall time.**
With-fix runs (44/46 iters, 2017/2123s) were marginally WORSE than the
baseline (39 iters, 1850s) — within run-to-run noise, and partly explained by
the with-fix runs spending rounds on the GOPI.md Rule-6 clone/push/cleanup
dance while the baseline recognized its CWD as the assigned worktree and
skipped it. The efficiency rules did visibly reduce output volume (930k
tokens in with-fix #2 vs 2.04M baseline — the concise-output rule) and
batching rates were similar everywhere, but neither moved wall time.

**3. The real bottleneck is per-round latency, not round count: ~46-48s per
gopi round vs ~3.9s per claude turn — an 11-13x gap on the SAME model.** Tool
execution is µs-ms; the entire gap is LLM generation time. The driver is
gopi's default per-request reasoning config: `thinking: enabled` +
`reasoning_effort: max` on EVERY call (NewAgent defaults). Historical
control: Task F ran at 13.6s/iteration with `GOPI_THINKING=disabled` (raw log
20260820-232712) — thinking+max-effort multiplies per-round latency ~3.4x.
Claude's requests to the same DeepSeek endpoint are lighter (its own
thinking budget; the anthropic endpoint serves the same model at a fraction
of the per-call cost). Secondary structural overheads in gopi: a mandatory
task-list JSON block every round, the bug022 double end-turn confirmation
(+1-2 rounds), and no context trimming below 90% of the 1M window.

## Recommendation

Keep the prompt fix (cheap, reduces token volume, likely helps on larger
wandering tasks). The gap-closing lever is the reasoning config: default
`reasoning_effort` to a lower tier (or thinking off) for ordinary rounds, or
make effort adaptive. Expected effect from the historical control: ~3x
wall-time reduction, which would bring gopi from ~31 min to ~10 min on this
task — still 2x claude, with the remainder being structural per-round
overhead. Follow-up: re-run the head-to-head with `GOPI_REASONING_EFFORT=low`
or `GOPI_THINKING=disabled` to confirm.

## Provenance

- Session JSONLs (all metrics extracted from them): `~/.gopi/sessions/`
  7af3d35a (baseline), e0a71bec + 5eb70927 (with-fix), claude run captured
  in `/tmp/bench/claude.out` (stream-json, `num_turns: 66`,
  `duration_ms: 257807`).
- Benchmark harness: `/tmp/bench/` (task text, analyzer script, run logs);
  clones `/tmp/bench-{before,after,claude}`; main repo restored to
  `feat/gopi-efficiency` after the baseline run.
