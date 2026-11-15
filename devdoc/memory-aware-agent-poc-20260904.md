<!-- doc-class: normative -->
# Memory-Aware Agent — POC Results & Design Conclusion (2026-09-04)

Source: ~/work/claude/the operator/memory-aware-agent.txt (the operator's design, 556 lines) + systematic POC (pure API prompt tests, no gopi in the loop).
Data: /tmp/poc/results/*.jsonl (36 + 12 + 9 + earlier calls, deepseek-v4-flash, thinking disabled).
Companion implementation ticket: write_task_area + memlayout prompt injection (gopi).

## The design under test
Give the agent *memory awareness*: it knows its working memory is bounded (~effective window, not nominal), is told the context layout (read-only anchors vs the working area it manages), and is *forced* (via a submit tool) to work one item at a time — analysis first, then a stable conclusion; the framework records the conclusion and clears the working frame (function-stack semantics). Framework = token accounting + partition isolation + a submit tool; LLM = decides when a step converges, what the conclusion is, what analysis to keep.

## POC findings (n>=3, DS v4-flash, thinking:disabled mandatory)

### 1. Layout hint (V2 "memlayout") — value is task-dependent
| task | V1 baseline chars/hit | V2 chars/hit | read |
|---|---|---|---|
| S simple (config diag) | 2881 / 100% | **2025 / 100%** | -30% length, same quality (drop filler) |
| M code review | 4938 / 100% | 4926 / 100% | flat |
| L long multi-step | 3289 / **90%** | 9330 / **100%** | V1 was *short because it skipped detail*; V2 expands per-step analysis+conclusion = more complete |

Conclusion: a soft layout/duty hint ("manage your working area; per step produce a compact conclusion; separate analysis from conclusion") is a *free win*: compact on easy tasks, complete on hard ones. Keep it as a base system layer. Percent/qualitative signals, NOT hard token offsets (offset is fake precision; LLM acts on "working area 70% full" and section semantics, not "offset 532").

### 2. Text template (V3 tasklist/analysis/conclusion markup) — pure overhead
V3/V4 (template / combo) systematically 2.6–4.7x longer (M 12943, L 15352 chars vs V1 4938/3289) with NO correctness gain. The *discipline* a template is meant to force is better delivered by a tool call (below), not by markup the model must reproduce.

### 3. Submit tool (write_task_area analog) — DECISIVE
5-item audit chain, A=free text, B=submit tool (one call per item), C=tool+layout hint:
- A x3: 0/3 completed within 8 rounds — model free-flows, asks for "new instruction", or buries "E ✅ DONE" in prose; no parseable per-item completion.
- B x3: 3/3 — exactly A->B->C->D->E, one submit call per round, clean completion + summary.
- C x3: 3/3 (tool dominates; layout hint adds nothing beyond the tool).
Conclusion: the submit tool FORCES one-item-at-a-time execution — this is the reliable fix for the "perfunctory all-complete" failure of free-text task lists. Framework-visible completion (a tool call) beats prompt-asked completion every time.

### 4. Cross-round context dropping — protocol allows it; HOW you hand off matters
Anthropic-style protocol only constrains tool_use/tool_result pairing WITHIN a round. Across rounds you may rebuild `messages` freely (drop old tool rounds entirely). Handoff comparison (round-2 question needing round-1 specs):
- H1 full history / H2 conclusion-only assistant msg / H3 conclusion embedded in the NEW USER msg: all correct.
- H3 (embedded in user) is cheapest and most robust — the model treats it as authoritative fact.
- H2 (isolated assistant "CONCLUSION:" history line) works but the model can treat it as an ordinary, question-able turn.
- H4 (nothing handed off; question says "use the earlier analysis") — model HALLUCINATES specs (invented delta=8GB/checkout=4GB). Safety red line: never let the model "know there was a conclusion it cannot see" — a missing conclusion turns into fabricated facts. Carry conclusions in the user/system context of the next round, never "implied but absent".

## Design conclusion (what gopi should implement)
1. **write_task_area tool** (per-subtask submit: task_id, status=completed, conclusion, optional compressed_analysis) — framework appends to an append-only task area, clears the working frame (via stub-folding per message-array constraints, or full drop across rounds — cross-round drop is protocol-legal). Hard discipline, replaces the text-template idea.
2. **memlayout soft hint** in the system prompt — layout map (read-only anchors: system/task; managed: working area), duty statement, per-step compact-conclusion instruction. Percent/fullness signals, no token offsets.
3. **Cross-round handoff = conclusions embedded in next user/system context** (not kept as isolated assistant history; never implied-but-missing).
4. Text templates (V3-style) NOT used for output structure.
5. Framework = scheduler (a pending queue the LLM cannot "declare all done" past), token/partition accounting, submit-tool enforcement. LLM = convergence judgment + conclusion authorship + keep/drop analysis decision.

## Open items (more testing, phase C)
- GLM-5.3-flash comparison (HP) — same matrix.
- Harder reasoning-type handoff (round-1 = analysis requiring synthesis, not spec extraction).
- Multi-round cumulative pressure (working area growth across many subtasks, where compaction timing matters).
- The framework-side scheduler interaction with gopi's existing task ledger (worker claim/run/writeback) — write_task_area is the in-session analog.
