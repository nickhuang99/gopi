<!-- doc-class: historical -->
# Task Report: Task-List Issuance Audit v2 — Fresh 50-Session Scan + Root-Cause Verification + Design Proposal

**Date:** 2026-08-28
**Branch:** `feat/tasklist-audit-v2` (worktree `~/workspace/gopi-tasklist-audit2`, shared with main repo)
**Prior art:** this is a re-verification of the audit in `feat/tasklist-audit` (devdoc/tasklist-audit-report.md, memory #1050). This v2 run: fresh session data (the last 50 sessions are *new sessions*, not the old 50), independent parser, deeper per-session root-cause classification, and a design proposal.

---

## TL;DR

1. **The ">30% of sessions never issue a task list" claim is NOT reproducible on fresh data.** Last-50 scan with a role-aware parser: **real-work no-list rate = 3/43 ≈ 7%** (raw 10/50 = 20% before filtering). Nothing close to 30%.
2. **But "never issues" is the wrong metric anyway.** The dominant real defect is **late first emission**: e.g. session `12c0a5a6` (30 assistant messages) emitted its ONLY list at message 30/30; `d8d80850` at 60/60; `dfa8cd38` at 91/91; `ec1d48a7` at 131/131; `0da1bef3` at 161/161 — every one of them emitted exactly at the final message, **never during the work**. 8 of 40 list-emitting sessions (20%) emitted only at the last message.
3. **Root cause is NOT LLM non-compliance.** The prompt itself permits/defers emission (R2 relaxation, commit `da28869`, 2026-08-22). Worse, the harness *rewards* late emission: the R3 "trusted completion" fast-exit requires the final round to carry an all-completed list — so the model rationally emits its one-and-only list at the very last message. **Prompt rule + exit logic form a coherent incentive to defer.**
4. **Confirmed live bug (memory #559):** `ParseTaskList` returns `nil, nil` for `[TASK_LIST_BEGIN]` with no `[END]` (or `END` before `BEGIN`) — **silent drop, no log, no error surfaced** (all three loop call sites use `tasks, _ :=`). Reproduced in the wild: session `68827372` dropped 2 full lists (truncated-JSON emission); another session (0a2a237c) had 2 false-positive prose mentions.
5. **The "system prompt enforces task list" premise is factually wrong:** the only hard prompt anchor ("end your response with BEGIN..END") lives in the one-shot Epic-framing user message (loop.go:1411); the *system prompt* rule says "emit ONLY on change, or every 3 responses — omit otherwise". The Epic message is never re-asserted after context truncation (R1 folds old turns), so the emission contract decays.
6. **No harness backstop for never-established lists.** Heartbeat `ShouldRemind` requires `CountProgressing(currentTasks) > 0` — a list that never existed never fires the nudge. This is exactly the class the original ">30%" report likely tripped over.

## Verified numbers (this run, role-aware parser)

| Metric | Value |
|---|---|
| Scanned | 50 most-recent session JSONL files |
| Real-work sessions (≥1 text msg AND ≥1 tool call) | 43 |
| No-list among real-work | **3 (7%)** |
| No-list among ALL 50 | 10 (20%) |
| Lists emitted ONLY at final message | 8/40 (20%) |
| BEGIN-without-END (true truncation drops) | 2 msgs in 1 session + 2 prose false-positives in another |
| Malformed-emission sessions | 2 |

The 3 real-work no-list sessions, classified by hand:
- `f5a23393` — **this audit session itself** (list not yet emitted at scan time; self-scan false positive).
- `03a6e23d` — Epic = "help"; 1 text msg, 2 tool calls, degenerate run (API/stall), no list warranted.
- `e4f36863` — Epic = "找到 assembleSystemPrompt... 只回答结果" — one-shot Q&A task; explicitly told to answer only the result; no list warranted by design.

**Zero genuine long-run no-list failures in the last 50.**

## Root causes (verified against source, all in `internal/agent/`)

### RC1 — R2 prompt relaxation is the instruction, not a regression of it
`loop.go:263-285`: `maxRoundsWithoutTaskList = 3`; `taskManagementRules` rule 2: "Emit ... ONLY on change ..., or every 3 responses — omit otherwise." Commit `da28869` (2026-08-22, "R2 — task list JSON only on change, not every round") — deliberate token-saving change. The model omitting the list on ordinary rounds is **compliant behavior**, not drift. The claim "system prompt enforces a task list every response" stopped being true on 2026- anyone measuring "rounds without a list" will see ~70%+ omission and misread it as failure.

### RC2 — The harness pays the model to emit late (R3 + R2 interaction)
The R3 "trusted completion" fast-exit requires the *final* round to carry an all-completed list + substantive answer. Combine with R2's "omit on ordinary rounds" and the rational strategy is exactly what we measure: **one list, at the end, all "completed"**. The 20% "only at final message" sessions are the model optimizing for the stated reward. This is an incentive-design bug, not a model bug. The list is meant to be a *plan*, not a *receipt*.

### RC3 — ParseTaskList silent-drop (memory #559, live in source today)
`tasklist.go:91-113`:
```go
endIdx := strings.LastIndex(response, TaskListEndMarker)
if endIdx < 0 || endIdx <= beginIdx {
    return nil, nil   // ← BEGIN present but no valid END → indistinguishable from "no list"
}
```
- All 3 loop call sites (loop.go:1612, 1626, 1703) discard the error with `tasks, _ :=`.
- Consequences: (a) truncated/malformed emissions vanish (68827372 case); (b) `TaskState.hasList` stays false → heartbeat can't nudge; (c) the loop's exit-with-progressing-tasks observability (loop.go:1612) never sees the dropped list — **defeats the unfinished-task exit protection**.
- Also: `LastIndex` for BEGIN + `LastIndex` for END independently — mis-nested multi-block text yields silent drop, not error.

### RC4 — "First turn" contract decays and is never re-asserted
The only strong emission anchor is in the one-shot Epic message (loop.go:1411): "Remember to end your response with [TASK_LIST_BEGIN]...[TASK_LIST_END]." The system prompt's rule 1 "First turn: initial task list from the Epic goal" is contradicted by rule 2's "omit otherwise" — models resolve the contradiction by deferring past turn 0 (2% emit at turn 0 per prior audit; this run: median first emission at msg ~5, and 20% only-at-end). After R1 context folding, the Epic message content may be folded away; **nothing re-asserts the emission contract mid-run**.

### RC5 — No backstop for never-established lists
Heartbeat fires only when `CountProgressing(currentTasks) > 0` (heartbeat.go:102-118). A session that never established a list never gets nudged — 25-60-turn list-less runs slip through (prior audit found these; this run's last-50 has none, but the class is open). Also `todoState.hasList` tracks establishment but nothing consumes it for nudging.

---

## 1. How the data was measured (reproducible recipe)

- Sessions: `~/.gopi/sessions/*.jsonl`, 50 newest by mtime.
- Role-aware counting is essential — naive grep inflates:
  - The **user Epic text contains** "[TASK_LIST_BEGIN]...[TASK_LIST_END]" (injected by loop.go:1411 into EVERY first user message) → naive grep sees every session as "has list".
  - Assistant **prose quoting** the marker (e.g. a prior audit session 0a2a237c discussing the marker in prose) counts as a false positive if you count strings not message roles.
- Counted only assistant-role messages: text blocks containing `[TASK_LIST_BEGIN]`; tracked pairs/bNe/eNb + first-emission position. Tool uses counted from `content[].type=="tool_use"` blocks.
- Filter real-work = ≥1 assistant text msg AND ≥1 tool call (excludes 0-msg/echo/test sessions).
- Script preserved at `/tmp/tasklist_audit.py` (add to repo if wanted).

**Measurement pitfall for the future:** metrics on session JSONL must parse roles and event types (`user`/`assistant`/`tool_result`/`request`/`session_start`), not raw grep.

## 2. Cross-harness design comparison (web research)

Method: web_search (bing/arxiv/merged) + arxiv 2604.14228 "Dive into Claude Code: The Design Space of Today's and Future AI Agent Systems" (Liu, Zhao, Shang, Shen; 2026). Key transferable points, mapped to gopi's situation:

| Harness | Plan-state mechanism | Enforcement | Lesson for gopi |
|---|---|---|---|
| Claude Code | `TodoWrite`/`TodoRead` structured **tool call** (JSON args = the list) | none — nudge-by-prompt ("keep the todo list updated"); plan-mode ask-permission flow | Structured tool-call plans are self-validating (schema-enforced) — gopi's prose-embedded markers are not
| OpenHands | Plan agent + `finish` tool; plan as conversation state | soft | Same as above |
| OpenClaw | gateway control-plane registration | perimeter-level | Harness-owned state beats prompt-asked state |
| Hermes Agent | pluggable memory backends | soft | State should live in the harness, not in prose |
| gopi (today) | prose-embedded `[TASK_LIST_BEGIN]` markers parsed post-hoc | none for establishment; silent-drop parser bug; prompt actively invites omission | — |

From the paper's design-space framing: "the same design questions produce different answers across contexts" — gopi answers "where does plan state live?" with **"in the model's prose, parsed after the fact"**. That choice is the root of RC3/RC5: prose-embedded state is (a) unsafe to parse (silent drop), (b) truncatable mid-block by `finish_reason=length`, (c) unreissuable by the harness, and (d) impossible to validate at write time. Modern harnesses converge on **structured tool-call plan state** (TodoWrite-style) or **harness-owned plan files**, both of which are schema-checkable at write time and re-injectable by the harness.

Also relevant: the paper's "reliable execution" philosophy — enforcement lives in the harness, not in the prompt. Gopi's current design puts the emission contract only in the prompt (RC1/RC4) with no harness backstop (RC5).

## 3. Proposal (prioritized)

### P0 — Fix the silent drop (small, mechanical, high value)
In `ParseTaskList`: when BEGIN exists but no valid END, do NOT return nil,nil. Instead:
1. Scan with paired scanning instead of two independent LastIndex calls;
2. return the partial (JSON slice from BEGIN to end of response) best-effort parsed; **plus** a `TaskListParseWarning` (new type or reuse error channel) so callers can log "list was truncated/malformed";
3. log a warning in the loop at all 3 call sites (stop discarding parse errors with `_`);
4. regression tests: BEGIN-no-END, END-before-BEGIN, prose-mention, truncated-JSON, multi-block. (`-run TestParseTaskList`)
- Effect: makes the 68827372-class failure visible; restores exit-protection semantics (a dropped progressing list currently defeats loop.go:1612's exit warning).

### P1 — Break the "emit-late-to-earn-exit" incentive
Decouple exit from list-issuance timing:
1. **Extend R3 trusted-completion to also accept: tasks==nil AND harness-side todoState.hasList==true with all-completed tracked state** — allow the fast exit without forcing a final-round emission;
2. Keep the final-list fast exit as-is for models that do emit;
3. Add prompt rule: "the task list is a plan, not a receipt: establish it on the first substantive turn; do not delay it to the final message."
- Effect: removes the reward for emitting only at the end. Expect first-emission position to move earlier; telemetry: `first_tasklist_msg_ratio = first_list_msg / total_msgs`.

### P2 — Harness nudge for never-established lists (closes the RC5 gap)
Extend heartbeat: if N=5 rounds elapsed with `hasList == false` AND ≥3 tool rounds done, inject: "No task list established yet — issue one from the Epic goal." This is the exact backstop the original report wanted. Zero conflict with R2 (R2 governs *update* cadence, this governs *establishment*).
- Also: when R1 context folding removes/weakens the Epic message, re-inject a one-line emission-contract reminder (the harness re-asserts what the model must keep doing).

### P3 — Mid-term: structured tool-call plan state (TodoWrite-style)
Replace prose-embedded markers with a `task_list` tool (args = the JSON list), schema-validated at write time, harness-owned state, re-injected into context by the harness each round (the harness can print current tasks in the tool result). This kills RC3 entirely (no prose parsing), makes establishment observable, and matches where modern harnesses converged (Claude Code TodoWrite, OpenHands plan state). Keep the prose markers for backward compat during transition (parse both, tool call wins). Per Rule 11: two real-task scenarios must prove LLM adoption before commit.

### P4 — Telemetry
Add to session JSONL (session_end event): `first_tasklist_msg_ratio`, `n_lists`, `n_malformed`, `list_ever_established` — so future audits run off a field, not a role-aware re-parse. Cheap, and makes the ">30%" style questions answerable by `jq`.

---

## 4. What I did NOT do (honest scope)
- Did not implement P0-P3 (this Epic = scan + root-cause + proposal; implementation would be a separate task).
- Did not re-derive all-time (523-session) stats — prior audit did that; this run re-verified the last-50 claim on fresh data.
- Web research was constrained (arxiv paper + generic search; deepseek method down with 401) — the comparison table cites the one directly relevant paper; the TodoWrite/plan-file claims are from harness knowledge, flagged as lower-confidence.

## 5. Verification

- Build: repo untouched (report-only change; no code changed, nothing to build).
- Tests: `TestParseTaskList*` unaffected (no code change).
- Commit: (to be filled after commit — see below)
- Parser script: `/tmp/tasklist_audit.py` (self-contained, rerunnable: `python3 /tmp/tasklist_audit.py 50`).

## 6. Deliverable declaration (Rule 6d)

- **Branch:** `feat/tasklist-audit-v2`
- **Recovery:** `git show feat/tasklist-audit-v2:devdoc/tasklist-audit-v2-report.md`
- **In main repo:** yes — worktree-shared; commit is on the branch in `~/work/GoPi`.
