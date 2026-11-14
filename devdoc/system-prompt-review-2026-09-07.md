<!-- doc-class: normative -->
# System Prompt Review — GoPi agent prompt vs Claude Code / Codex / leaks

- **Reviewed file**: `~/work/claude/nick/system_prompt_review.txt` (363 lines = agent prompt lines 1–80, then GOPI.md appended as project context)
- **Ground truth cross-checked against**: `internal/agent/loop.go` (`promptSections`, `claudeToolDefs`), `internal/agent/tasklist.go`
- **Comparison corpora**: `~/work/Downloads/claude-code-system-prompts/system-prompts/` (690 fragments, v2.1.238), `~/work/Downloads/system_prompts_leaks/` (Codex/Cursor/GLM/etc.)
- **Date**: 2026-09-07

## Completed
- Enumerated the real tool set from `claudeToolDefs` (23 tools) and diffed it against the prompt header and PROJECT.md Rule 3
- Read every prompt section at source (`toolRoutingRules`, `efficiencyRules`, `taskManagementRules`, `bashEfficiencyRules`, `jobRules`, `engineeringRules`, `conciseThinkingRules`, `memLayoutHint`, `tickClockRule`)
- Traced the task-list protocol end to end (markers → `ParseTaskList` → `todoState`) and confirmed the two-state status vocabulary
- Full census of `devdoc/` (389 files) with Rule 5 template-compliance measurement
- Extracted Claude Code's task/todo, routing, parallelism, verification, brevity, delegation, and memory protocols with verbatim quotes

## Not Done
- Did not modify the prompt or rewrite any code — this is review only
- Did not remediate the credential leak found in `devdoc/` (flagged below, needs owner decision)
- Did not benchmark other leak families beyond Codex in depth (Cursor/GLM skimmed only)

## Verification
- Build: n/a (docs-only review, no code touched)
- Tests: n/a
- Commit: (docs-only, uncommitted — author to commit)

---

# Part 1 — Verdict

The GoPi prompt is **dense, measured, and independently converged on several of Claude Code's best-validated rules** (parallel batching, act-when-ready, counter-example tool routing, prefer-edit-over-write, no-narration). That convergence is strong evidence the rules are real and not cargo-culted.

The weaknesses are **not** per-rule quality. They are:

1. **Internal contradictions between adjacent sections** — the same job is assigned to different tools in `[TOOL ROUTING]` vs `[BASH EFFICIENCY RULES]`, and the `todo` rule contradicts the `todo` tool's actual semantics.
2. **A task-list protocol that under-specifies its own vocabulary** — two statuses instead of three, no cardinality rule, a self-contradictory parent/child rule, and a "DONE" condition that omits the project's own build+test gate.
3. **Structural, not editorial**: everything lives in one 4,498-char monolith pressed against its own 4.5K density gate, while the competitors put their most prescriptive text in *tool descriptions* and *runtime-injected nags* — places that cost nothing from the system-prompt budget.

---

# Part 2 — Task workflow: the ambiguities (focus area)

### T1. Rule 7 tells the model to record progress with a read-only tool — `internal/agent/loop.go:590`

```
7. Use todo for progress — never narrate it in thinking.
```

The `todo` tool's own description says the opposite: *"Read-only and zero-cost … **you update tasks by emitting the TASK_LIST block in your response**."* The rule reads as if `todo` *writes* progress. A model following rule 7 literally calls `todo` and nothing changes.

PROJECT.md Rule 3 makes it worse by listing `todo` in the flat tool inventory with no hint it is read-only.

**Fix**: `todo = read-only view. To change status, re-emit the block.`

### T2. Two statuses, no `pending` — `internal/agent/tasklist.go:12-13`

```
TaskStatusProgressing = "progressing"
TaskStatusCompleted   = "completed"
```

Claude Code (`tool-description-todowrite-compact.md`) and Codex (`codex-full.md:501`) both use **three** states: `pending | in_progress | completed`. With two states:

- You cannot put planned-but-not-started work on the list without asserting it is already `progressing`.
- **The plan and the status collapse into one register.** The prompt's own rule 1 says the initial list *is* a PLAN, yet the only non-terminal status means "actively being worked."

### T3. Rule 4 is self-contradictory under rule 6

```
4. Complex task → decompose: parent "completed", children "progressing".
6. All completed = DONE — final summary, stop, memory_store.
```

Rule 4 marks the **parent** `completed` while its children are still `progressing`. Rule 6 defines DONE as *all* completed. A parent that is `completed` above unfinished children either violates rule 6's semantics or invites premature termination. Neither rule defines what "parent" means or how parent/child ids relate — the JSON is flat, so there is no nesting representation at all.

**Fix**: invert it. Parent stays `progressing` until every child is `completed`.

### T4. "every 3 responses" is undefined

```
2. Re-emit ONLY on change or every 3 responses
```

Three of what — assistant messages, tool rounds, responses including the final one? Source is `maxRoundsWithoutTaskList = 3` (`internal/agent/loop.go:567`) counted in rounds, but the prompt says "responses," which a model will read differently. And the interaction with rule 6 is unspecified: the harness does an exit-time contract check via `todoState.view()` (the P1.5 path), but the prompt never says whether the *final* message must carry a closing list. Rule 1 says the initial list must *never* be in the final message — a model can reasonably conclude the final message must never contain a list, which defeats the exit check.

### T5. "NEVER delete task_ids" is unenforceable and undefined

Rule 3 forbids deletion, but the protocol is **full-list re-emission** — the model rewrites the entire array each time, so nothing structurally prevents dropping an id, and "delete" is never defined (removed from the array? marked complete?). There is no sanctioned way to retire a task that turned out to be unnecessary.

### T6. "Epic ≠ task" is undefined jargon — `internal/agent/loop.go:672`

`Establish the [TASK_LIST_BEGIN] plan by your second tool round — never the final message (Epic ≠ task).` "Epic" is a dispatcher/PA concept. The model has no other context and cannot tell what this forbids.

### T7. No threshold for using the list at all

Claude Code is explicit: *"Complex multi-step tasks — When a task requires 3 or more distinct steps"* and *"...the task is trivial ... Skip using this tool"*. GoPi mandates a plan by the second tool round for **every** task, including a one-line lookup. The code comment at `internal/agent/loop.go:751`, `internal/agent/loop.go:762` itself complains that re-emitting costs 150–500 output tokens/round of pure bookkeeping — the missing trivial-task exemption is a direct contributor.

### T8. The DONE condition omits the project's own build+test gate

```
6. All completed = DONE — final summary, stop, memory_store.
```

PROJECT.md Rule 5 defines done as build → test → commit → **report**, and says *"No document = task not finished."* None of that reaches the agent prompt. The agent's terminal condition is purely bookkeeping-state. This is the single highest-value fix in the block.

### T9. `task_submit` and `task_evaluate` are in the 23-tool set but appear **0 times** in the system prompt

Verified by grep against lines 1–80. Every other tool has routing guidance; these two have none. `write_task_area` appears only in `[WORKING MEMORY LAYOUT]`. Rule 11's own standard is *"工具的价值 = LLM 真的用它"* — these two cannot be used well because nothing tells the model when they apply.

### T10. `memory_feedback` enum is mis-taught — `internal/agent/loop.go:533`

```
d. vote memory_feedback(useful/unused) IMMEDIATELY after each memory_search result; contradicts → memory_feedback(useless)+memory_update
```

The real enum is three values (`useful | useless | unused`, confirmed in the tool schema). The prompt presents `(useful/unused)` as if it were the complete pair, then introduces `useless` separately. A model will plausibly conclude there are two vote modes and never use the neutral one.

### T11. `取不到→酿造` instructs an action the model cannot perform — `internal/agent/loop.go:529`

Rule (a): `start each task with memory_search FIRST (取不到→酿造)`. `memory_brew` exists in `internal/tools/` but is **not** in `claudeToolDefs` — verified. The model has no tool to "brew" with and no definition of what brewing means.

---

# Part 3 — Other ambiguities and contradictions

### A1. `[BASH EFFICIENCY]` rule 2 directly contradicts `[TOOL ROUTING]` — the worst defect in the prompt

```
[TOOL ROUTING]      - read: file contents. Use read — not shell cat/head/sed.
[BASH EFFICIENCY] 2. Never cat a large file whole — sed -n 'A,Bp' / head / tail.
```

Two adjacent sections prescribe opposite tools for the same job. The intended distinction (read = the default for *seeing* a file; sed/head only inside a larger compound command for byte-precise extraction) is never stated. A model cannot resolve this; it will pick one at random per round.

### A2. "ONE dense command per round" vs "PARALLEL: all independent tool calls in ONE response"

`[BASH EFFICIENCY] 1` says one command per round; `[EFFICIENCY] 1` says batch all independent calls into one response. Is a batch of four bash calls a violation? Unresolved. Also note rule 1's example is Go-specific (`go build ./...`) in a prompt presented as generic.

### A3. PROJECT.md Rule 3 says **18 tools**; the real count is **23**

`GOPI.md:46` lists 18 and says *"Do NOT add new top-level tools."* `claudeToolDefs` registers 23. Five tools (`git_blame`, `context_trim`, `write_task_area`, `task_submit`, `task_evaluate`) postdate the rule and contradict it. GOPI.md is read at startup as a *"HARD CONSTRAINT"* — so the project's own context file currently asserts a false inventory and forbids tools it already has.

### A4. The header names only 3 of 23 tools

`You are GoPi. 23 tools: vision_analyze (OCR), text2img (PNG), git_blame (git history).` The routing section covers 12. Eight or more tools have no coverage at all.

### A5. `[WORKING MEMORY LAYOUT — soft]` — the only section with a softness label, containing an imperative

`Item converges → write_task_area a CONCLUSION; committed, do NOT re-derive.` What does "soft" license? Skip when inconvenient? No other section has such a label.

### A6. The tick clock is a signal with no decision attached

`A ticking clock runs from session start; every tool result carries an elapsed tick [t+NNNs] — sense time: cost, stalls, waits.` "Sense time" is not actionable. No threshold, no behavior. Compare the engineering rules, which are all if-then. Either give it a trigger or it costs tokens for nothing.

### A7. No blocked/ambiguity path

`ACT WHEN READY` pushes toward acting on partial information (correct for an autonomous agent), but there is no counterpart for *cannot* proceed. Rule 6 handles "all completed"; nothing handles "blocked." Claude Code's autonomous guideline has an explicit detection rule (*"if your last paragraph is a plan … do that work now"*).

### A8. Mixed Chinese/English is an unstated policy

Chinese glosses appear in `[ENGINEERING RULES]` and `[TOOL ROUTING]` memory items only. Fine if deliberate (and devdoc shows Chinese reports are normal here), but it is applied inconsistently and is never declared as a convention.

---

# Part 4 — devdoc/ report

**Census**: 389 files, ~3.55 MB; 352 at top level; 382 `.md`, 3 `.txt`, 2 `.sh`, 1 `.go` (archived), 1 `.png`.

### 4.1 — SECURITY: plaintext credentials in a git-tracked file

`devdoc/claude_network_investigate.txt` is tracked by git (`git ls-files` confirms) and contains, verbatim on line 1:

> the gateway of local network is at 10.0.0.1 with user/pass as **[REDACTED]** … two router HRC-RC3000 at 10.0.0.7 and 10.0.0.9 both with browser login pass as : **[REDACTED]**

Line 45 additionally records that *"login password = WiFi password on both devices."* This is a home-network credential set committed to a repo, in a file whose purpose (a pasted CLI transcript) is unrelated to GoPi. Regardless of how the rest of devdoc is organized, this file should be removed from history and the credentials rotated. Note the irony: it is itself a transcript of a `claude -p --dangerously-skip-permissions` run. **[2026-09-07: file purged from git history via filter-repo; credentials rotated — nick to confirm.]**

### 4.2 — No index

There is no `README`, `INDEX`, or table of contents anywhere under `devdoc/`. PROJECT.md Rule 7 says *"Future agents have no memory of this session — devdocs are their only window"*, yet there is no way for a future agent to discover what is in the 389 files without listing them. This is the highest-leverage structural fix in the repo.

### 4.3 — Rule 5 template compliance is only 43%

Across 380 top-level `.md`: `## Completed` 161 (42%), "Not Done" 224 (59%), `## Verification` 258 (68%), **all three 163 (43%)**, none of the three 84 (22%).

The split is systematic, not random:

| Family | Compliant? |
|---|---|
| `bugNNN-*`, `bugfix-*`, `m4*` implementation reports | **Yes** — `bug025-bash-timeout.md` is textbook (Completed / Not Done / Verification + commit `2c5e8c0`) |
| `review*`, `code-review-*`, `self-review-*`, night reports, handoffs | **No** — findings-list format, no Verification section |

22 dated reports have no Verification section at all. The reviews can't comply (read-only work has no build/test/commit), but PROJECT.md Rule 5 doesn't say so — the deviation is undocumented, so each new author re-decides it.

### 4.4 — At least 12 near-duplicate report clusters

The review → rereview → merge-report → v2 → second-opinion workflow spawns one file per artifact:

- `branch-scan-archive-filter.md` / `-merge-report.md` / `-rereview-report.md`
- `tasklist-audit-report.md` / `-compare-report.md` / `-v2-report.md`
- `log-last-degenerate-fix.md` / `fix-log-last-degenerate.md` / `merge-fix-log-last-degenerate.md`
- `review-cli-config-options.md` vs `review2-cli-config-options.md`
- `code-review-2026-08-28.md` vs `code-review-2026-08-28-buddy.md`
- `web-align-p0.md` / `-brief.md` / `web-align-p1.md`
- `tasks-auto-register-feasibility-{ds,glm,hy4}.md` (same question, three models)

### 4.5 — Naming drift

~150 files fit no prefix family. Two competing date formats coexist for the same window (`20260902`, 34 files vs `2026-09-02`, 15 files). Six ALL-CAPS shout-style names. Hash-pair (`full-review-632c4c4-229387e.md`) and epoch (`hook-test-probe-merge-report-2830183.md`) suffixes.

### 4.6 — Stale and stub files

- `optimization-report.md` (2025-07-17) and `hermes-memory-go-reevaluation.md` (2025-08-04) are ~13 months old; the former is *self-declared OUTDATED* by `gopi_design_concern.txt`.
- Four stubs under 130 bytes: `verify-final.md` (21 B), `v3-drain-test.md` (22 B), `ff-case-test.md` (119 B), `hp-channel-test.md` (120 B).

### 4.7 — Non-document files in a docs directory

`apply-flagfix.sh`, `memsync-e2e-sim.sh` (belongs in `scripts/`), `archive/taskingest-cas-test-archived.go`, and `assets/bm-pricing-20260830.png` (1.06 MB — 30% of the entire tree's bytes).

---

# Part 5 — Comparison with Claude Code and Codex

## 5.1 — Where GoPi already matches or beats them

| Rule | GoPi | Claude Code | Verdict |
|---|---|---|---|
| Parallel batching | `PARALLEL: all independent tool calls in ONE response` | *"make all independent tool calls in parallel … if some tool calls depend on previous calls … sequentially"* | **Converged** |
| Act when ready | `ACT WHEN READY: act on enough info — no re-deriving` | *"When you have enough information to act, act. Do not re-derive facts already established"* | **Converged** |
| Counter-example routing | `Use read — not shell cat/head/sed` | *"Use ${READ_TOOL_NAME} (NOT cat/head/tail)"* | **Converged** — GoPi's one-line-per-tool form is *more* token-efficient than Claude Code's per-file bullets |
| Prefer edit over write | `Use edit — not write` | *"ALWAYS prefer editing existing files… NEVER write new files unless explicitly required"* | Converged (GoPi's is terser) |
| Don't narrate | `never narrate it in thinking` | *"Don't narrate your internal deliberation"* | Converged |
| **Cross-task memory** | `memory_search/store/feedback/update` with an explicit judge loop | CLAUDE.md is user-authored and static; auto-memory is a recent add with no feedback loop | **GoPi is ahead** — the `memory_feedback`/`memory_update` closed loop has no Claude Code equivalent |
| **Time salience** | tick clock `[t+NNNs]` | nothing comparable | **GoPi is ahead** (but currently unactionable, see A6) |
| **Context trimming** | `context_trim` with verbatim `tool_use_id` copy | relies on compaction, no model-invoked trim | **GoPi is ahead** |
| Token economy | 4,498 chars total | Claude Code's composite is several× larger (the TodoWrite tool description alone is longer than GoPi's entire task block) | **GoPi is ahead** on per-turn input cost |

This is the headline: **GoPi independently derived most of Claude Code's validated per-rule content in a fraction of the tokens.** The remaining gap is architectural, not editorial.

## 5.2 — Where Claude Code is ahead

### C1. Detail lives in tool descriptions and runtime nags, not the system prompt — the core structural difference

Claude Code's most prescriptive text is not in the system prompt at all. Its strongest routing line is in the Grep *tool description*:

> ALWAYS use ${GREP_TOOL_NAME} for search tasks. NEVER invoke `grep` or `rg` as a ${BASH_TOOL_NAME} command. The tool has been optimized for correct permissions and access.

And the full TodoWrite protocol (statuses, cardinality, completion gate, "3 or more steps" trigger) lives in the tool description, where it is scoped to the call site rather than competing for system-prompt budget. GoPi's tool descriptions are already good — `git_blame`'s is excellent and follows the same pattern — but the system prompt **duplicates and competes with** them instead of delegating. That is why GoPi is pinned at 4,498/4,500 characters with no room to fix T1–T11.

**This is the main unlock**: move protocol detail into tool descriptions and GoPi gets budget back for free.

### C2. Reactive nags — GoPi has the data and doesn't use it

Claude Code injects at runtime when a tool is under-used:

> The TodoWrite tool hasn't been used recently. If you're working on tasks that would benefit from tracking progress, consider using the TodoWrite tool.

GoPi already tracks `tasklist_issued_at` (`touchTasklistIssuedAt()`, `internal/agent/loop.go:638`) and warns at **exit** via `todoState.view()`. That is the same signal, delivered too late to matter. A round-N nudge would convert GoPi's most expensive failure mode — silent bookkeeping decay — from a prompt-engineering problem into a harness one. Rule 11's thesis (*"prompt 是工具的唯一说服机制"*) is exactly what nags obsolete; the audit numbers GoPi cites (`git_blame` 0/21435, `code_search` 0.3%) are the failure mode nags exist to fix.

### C3. Three-state vocabulary with a cardinality rule

> Each todo has `status` ("pending" | "in_progress" | "completed") … Exactly ONE task must be in_progress at any time (not less, not more)

vs GoPi's two states, no cardinality, and a self-contradictory parent/child rule (T2/T3).

### C4. A completion gate tied to test state

> ONLY mark a task as completed when you have FULLY accomplished it … Never mark a task as completed if: Tests are failing / Implementation is partial

GoPi has this idea in PROJECT.md Rule 5 but it never reaches the agent prompt (T8).

### C5. An explicit anti-fraud / truthful-reporting rule

> Report outcomes faithfully: if tests fail, say so with the output; if a step was skipped, say that; when something is done and verified, state it plainly without hedging.

And for delegated work: *"an agent's summary describes what it intended to do, not necessarily what it did."* GoPi's `BE CONCISE: lead with the result; failing tests keep full content` is a partial proxy, but nothing forbids claiming success. In a system where PA reads agent reports to decide merges, this is high value per token.

### C6. Delegation and context-protection guidance

Claude Code has an explicit inline-vs-delegate policy with positive tests and anti-patterns (*"Do not spawn a subagent to review, re-verify, or double-check work you can verify inline"*; *"Never delegate understanding"*). GoPi has `job_start` (process-level) and `context_trim` (context-level) but no subagent layer, so it cannot protect its context window by delegating. Not a defect so much as a capability GoPi hasn't built — `context_trim` is the substitute.

### C7. Codex's plan mode

`plan_mode.md` separates *non-mutating exploration* from *mutation* ("You must not perform mutating actions") and explicitly disambiguates it from the `update_plan` checklist tool. GoPi's worktree isolation (PROJECT.md Rule 6) covers the blast-radius half but has no explore-first phase for solo work. Low priority given Rule 6.

---

# Part 6 — Concrete recommendations

## 6.1 — Rewrite `[TASK MANAGEMENT RULES]` (token-neutral, fixes T1–T8)

Current block is 8 lines / ~640 chars. Proposed replacement:

```
[TASK MANAGEMENT]
1. SCOPE: list multi-step work only (>=3 real steps). Skip for one-shot lookups.
2. PLAN by 2nd tool round, never the final message:
   [TASK_LIST_BEGIN][{"task_id":"1","summary":"<verb+object>","status":"pending"}][TASK_LIST_END]
3. STATUS: pending -> progressing -> completed. Exactly ONE progressing.
   completed ONLY when verified (build+test green). Parent stays progressing
   until every child is completed.
4. UPDATE: re-emit the full block only when it changes; otherwise omit. At
   least every 3 tool rounds.
5. NEVER drop a task_id. Obsolete -> completed with summary "(dropped: why)".
6. todo = READ-ONLY view of your list. Emit the block to change it.
7. DONE = every id completed AND build+test green AND report written in
   devdoc/. Then final summary + memory_store. Never report done without the report.
```

Fixes: read-only `todo` (T1), three states (T2), inverted parent/child (T3), "tool rounds" (T4), drop semantics (T5), drops the undefined "Epic" jargon (T6), trivial-task exemption (T7), and the Rule 5 gate (T8). Requires a one-line harness change in `tasklist.go:12` to accept `pending` (treat as not-completed — `IsCompleted()` already returns false for anything else, so it may be a zero-change add).

## 6.2 — Resolve the read-vs-sed contradiction (A1)

State the distinction once:

```
- read: see a file — NOT cat/head/sed. sed/head/tail only INSIDE a compound
  bash command for byte-precise extraction, never as your primary reader.
```

## 6.3 — Sync PROJECT.md Rule 3 to the real tool set (A3)

Replace the 18-tool list with the 23-tool list from `claudeToolDefs`, and note `todo` and `context_trim` are read-only/context-scoped. Better: derive it from the code so it cannot drift again — the list is already enumerable from `claudeToolDefs`.

## 6.4 — Move protocol detail into tool descriptions (C1)

Highest-leverage structural change. Specifically: move the full task-list emission protocol into the `todo` tool description; move the `memory_feedback` three-value enum explanation into that tool's description; keep only the one-line trigger in the system prompt. This buys back ~300–400 chars against a 4,500-char ceiling — enough to fund every other fix on this list.

## 6.5 — Add a round-N task-list nag (C2)

GoPi already records `tasklist_issued_at`. Emit a `system-reminder`-style line into the next tool result when N rounds pass without a block:

```
[REMINDER] No TASK_LIST block in 3 rounds. Re-emit it, or mark tasks completed.
```

This converts rule 11's manual prompt-iteration loop into a runtime mechanism, and directly targets the measured failure (19 tool calls, zero `todo` calls, progress narrated in thinking).

## 6.6 — Add a truthful-reporting line (C5)

One line, high value:

```
- Report outcomes faithfully: failing tests keep full output; skipped steps
  say so. Never claim done without the verification output.
```

## 6.7 — Give the tick clock a decision (A6)

Either attach a threshold or delete it:

```
- [t+NNNs]: a single tool call exceeding t+300s means the wrong tool —
  switch to job_start or cut scope.
```

## 6.8 — Cover `task_submit` / `task_evaluate` (T9)

One routing line each, or drop them from the tool set. Under Rule 11's own standard, tools the prompt does not argue for are expected to go unused.

## 6.9 — devdoc remediation (Part 4)

Priority order:

1. **Remove `devdoc/claude_network_investigate.txt` from git history and rotate the router/gateway credentials.** Do not just `git rm` — the creds remain in prior commits.
2. Generate `devdoc/INDEX.md` from filename families (task*, review*, bug*, mem*, log*, m4*) so Rule 7's "only window" is actually navigable. Scriptable; PROJECT.md Rule 12 already establishes that repos declare their own automation.
3. Add one sentence to PROJECT.md Rule 5: reviews/audits/handoffs are exempt from the Verification section but must state "docs-only" explicitly. This legalizes the 43% deviation instead of leaving each author to guess.
4. Pick one date format (`YYYYMMDD`, the majority style, 34 vs 15) and note it in Rule 5.
5. Move `memsync-e2e-sim.sh` to `scripts/`, delete the four <130-byte stubs.
6. Require every devdoc file to open with a status line (`Status: current | superseded-by:<file> | outdated`) so the 2025 documents self-identify.

---

## Verification

- Build: n/a — docs-only review, no source modified
- Tests: n/a
- Commit: (uncommitted — author to commit)
- Claim-by-claim provenance: tool count from `claudeToolDefs`; status vocabulary from `tasklist.go:12-13`; `todo` semantics from its tool description; `memory_brew` non-exposure and `task_submit`/`task_evaluate` absence verified by grep; `memory_feedback` enum from the tool schema; credential leak confirmed by direct read + `git ls-files`; devdoc counts and Rule 5 compliance measured across 380 files; Claude Code quotes extracted from the 690-file corpus with source filenames.
