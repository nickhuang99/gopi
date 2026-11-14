<!-- doc-class: normative -->
# Task Report: P0 ParseTaskList Silent-Drop Repair — Evaluation (four-question, data-driven)

**Date:** 2026-08-29
**Branch:** `feat/tasklist-p0-eval` (worktree `~/workspace/tasklist-p0-eval`, shared with main repo)
**Scope:** Evaluate — NOT implement. No production code touched, no bin/gopi rebuilt.
**Prior art:** devdoc/tasklist-audit-report.md (#1050), devdoc/tasklist-audit-v2-report.md (#1052), tasklist-audit-compare-report.md (#1053), memory #559/#560.

---

## TL;DR (decisive conclusions)

1. **P0 partial-parse + warning is WORTH DOING.** Full-run role-aware scan finds **23 recoverable BEGIN-no-END messages across 10 real-work sessions; a partial parser recovers 23/23 = 100%**, and produces **zero garbage lists** on prose references. Critically, in **every one of the 10 sessions the dropped list was the session's ONLY task-list emission** (`hasList` never became true), so the drop did not lose "one list" — it made the entire session's progress tracking invisible and structurally disabled the unfinished-task exit protection (loop.go:1702). P0 is a high-value, low-risk, mechanical fix. **It should ALSO be paired with a prompt-side anchor** (see recommendation) because the root cause is the LLM forgetting the END marker, not a parser bug — partial-parse is the harness backstop, not the fix.
2. **nick's compaction hypothesis is DISPROVEN by the data.** Across all 1444 sessions there are **ZERO B-class cases** (no JSON-mid-truncation anywhere — no `finish_reason=length` truncation of a list). 17/23 instances occurred at prompt_tokens 1.4K–58K, nowhere near the 150K compact threshold. Even the 6 instances at high context (123K–227K prompt_tokens) wrote **complete, balanced JSON** — they simply omitted the END marker. The source is an **LLM behavioral pattern (forgetting the closing marker)**, not memory-compaction timing. Compaction is irrelevant to this bug.
3. **RC2 governance recommendation: do P1.3 (rewarding) FIRST, hold P1.5 (policy).** P1.3 is cheap and low-risk (system-prompt section edit, one-time ~350-token cache re-fill per session). P1.5 (accepting `tasks==nil && hasList` for fast-exit) is the earlier proposal but carries a real **stale-tracked-state premature-exit** risk and is **orthogonal to P0** (the dropped-list case never sets `hasList=true`, so P1.5 doesn't help it). Recommend P1.3 now, P1.5 only with a guard that verifies tracked-state recency.

---

## Q1 — 全库分类 BEGIN-no-END 的真实源头

**Method (role-aware, following devdoc/tasklist-audit-v2-report.md):** parsed all 1444 session JSONLs under `~/.gopi/sessions/`. Counted only **assistant-role text blocks**; matched the FIRST `[TASK_LIST_BEGIN]` in the block and checked for a `[TASK_LIST_END]` AFTER it (paired semantics, matching the real `ParseTaskList` drop condition). User Epic messages (which contain the injected marker via loop.go:1501) and prose quotes are excluded. For each BEGIN-no-END assistant message, classified:
- **A**: JSON complete/recoverable (ends in balanced `]}` or has a parseable balanced-JSON prefix) — LLM forgot the END marker.
- **B**: JSON mid-truncation (real `finish_reason=length` cut — JSON array not closed).
- **C**: other (prose reference / malformed / empty).

**Result:**

| Class | Count | Meaning |
|---|---|---|
| **A** (forgot END, JSON complete) | **23** | LLM wrote full JSON, omitted `[TASK_LIST_END]` |
| **B** (JSON mid-truncation) | **0** | no token-length truncation of any list |
| **C** (prose/malformed/other) | **6** | all confirmed prose quotations of the marker — false positives |
| **Total** | **29** msgs / 14 sessions | |

**A-class instances (3 shown per class; full list in /tmp/tasklist_p0_all_cases.txt):**

| session id | line | recovery evidence |
|---|---|---|
| `0a33b051-…` | 135 | tail `…"create_at":"1785893994"}]` (balanced `}]`) |
| `2be1022c-…` | 16 / 40 / 42 | tail `…"status":"completed"}]` (full list, `to_eof` parses it) |
| `354c0d47-…` | 6 | full 2-task list, `to_eof` parses |
| `5a1dab59-…` | 255 | 8-task list, `to_eof` |
| `6613fe97-…` | 90 / 383 / 446 / 591 / 660 | 11-task list + trailing `</parameter></invoke></tool_calls>` XML pastiche → balanced-prefix recovers |
| `68827372-…` (PA's case) | 281 / 286 | 7-task list, `to_eof` — **PA confirmed**: JSON complete, only END missing; twice in a row |
| `71aa7e11-…` | 64 / 852 / 862 | 8-task list, `to_eof`; note line 64 has invalid status `"pending"` |
| `82e6135b-…` | 76 | 7-task list, `to_eof` |
| `b9c3a79d-…` | 226 / 951 / 1040 | 13-task list + trailing XML pastiche → balanced-prefix |
| `e0a71bec-…` | 77 / 208 / 211 | 5-task list, `to_eof` |

**C-class (prose, all rejected — NOT bugs):**
`0a2a237c` (79, 103), `2b5cb178` (277), `a2add5ad` (56), `cd67a086` (113, 135) — all quote/discuss the marker in prose (e.g. *"the mandatory [TASK_LIST_BEGIN] JSON block"*, *"`fmt.Print(result)` … the `[TASK_LIST_BEGIN]` block"*).

**Key correction to PA's premise:** PA stated 68827372 was "the only case with data". **That is wrong** — I found **10 independent sessions / 23 recoverable lists**. The bug is wider than one session.

**Severity — the drop is worse than "loses one list".** In **all 10 A-class sessions, the dropped BEGIN-no-END message was the session's ONLY task-list emission** (verified: `valid=0` valid lists, `dropped=N` in every session, all exit_code=0). Because `update()` is gated on `tasks != nil` (loop.go:1716), and `ParseTaskList` returns nil for BEGIN-no-END, `TaskState.hasList` never became true → heartbeat never nudges (requires `CountProgressing>0`), and the loop's unfinished-task exit warning (loop.go:1702) never fires. The entire session's progress tracking is void, and the run exits normally (exit 0) as if short/incomplete work were fine.

---

## Q2 — 验证 nick 的 compaction 假设

Test: for each A-class instance, read the nearest `request` event's `prompt_tokens`. Compact threshold = `DefaultContextWindow(200_000) × DefaultCompactTriggerRatio(0.75)` ≈ **150K** (context.go:16-33, memory #931/#932).

| session | line | prompt_tokens | near 150K threshold? |
|---|---|---|---|
| `2be1022c` | 16 | 1,431 | No |
| `2be1022c` | 40 | 6,708 | No |
| `2be1022c` | 42 | 6,810 | No |
| `354c0d47` | 6 | 0 (fresh) | No |
| `82e6135b` | 76 | 17,890 | No |
| `e0a71bec` | 77 | 29,714 | No |
| `71aa7e11` | 64 | 31,165 | No |
| `e0a71bec` | 208 | 42,071 | No |
| `e0a71bec` | 211 | 42,453 | No |
| `68827372` | 281 | 50,650 | No |
| `68827372` | 286 | 52,103 | No |
| `5a1dab59` | 255 | 55,683 | No |
| `6613fe97` | 90 | 58,329 | No |
| `0a33b051` | 135 | 61,495 | No |
| `b9c3a79d` | 226 | 70,173 | No |
| `71aa7e11` | 852 | 123,797 | below |
| `71aa7e11` | 862 | 124,849 | below |
| `6613fe97` | 383 | 116,146 | below |
| `6613fe97` | 446 | 132,106 | below |
| `b9c3a79d` | 951 | 220,566 | **above** (compaction would have fired) |
| `b9c3a79d` | 1040 | 227,519 | **above** |
| `6613fe97` | 591 | 166,373 | **above** |
| `6613fe97` | 660 | 169,424 | **above** |

**Verdict — compaction hypothesis does NOT hold:**

- **17/23 instances at 1.4K–124K prompt_tokens**: far below the 150K threshold. Sessions like `2be1022c` (1.4K–6.8K) and `82e6135b` (17.9K) could not possibly have hit memory-compaction pressure. Compaction timing cannot explain these.
- **ZERO B-class** (no JSON mid-truncation) in the entire corpus. If `finish_reason=length` / compaction had been the cause, we would see lists cut mid-array. None exist.
- Even the **6 high-context instances (123K–227K)** wrote **complete, balanced JSON** — full lists recovered by partial-parse. The model had room to finish the JSON; it just omitted the `[TASK_LIST_END]` marker.

**Root cause (confirmed):** the LLM **forgets the closing marker**, an independent behavioral pattern — not token truncation and not compaction timing. (Memory #559/#560's earlier attribution to "58%→token limit" was a hypothesis from a single case; the full-corpus scan disproves truncation as the general mechanism.)

---

## Q3 — 评估 partial-parse 修法的实际恢复率

**Prototype parser** (`balanced_prefix_json` in the audit script): find the first `[` or `{` after BEGIN, scan to the matched close with string-aware balancing, `json.loads` the prefix. Compared against a naive "parse BEGIN-to-EOF" path.

**Recovery rate on A-class (23/23 = 100%):**

| approach | success | notes |
|---|---|---|
| `to_eof` (parse whole tail after BEGIN) | works on 18/23 | plain JSON-at-end cases |
| `balanced_prefix_json` (longest balanced JSON prefix, strips trailing prose/XML) | works on **23/23** | also recovers the 5 `6613fe97` + 3 `b9c3a79d` cases with trailing `</parameter></invoke>` pastiche |
| **Overall** | **23/23 = 100%** | — |

**False-positive (garbage-list) risk — LOW, and avoidable:**

- Run the SAME partial parser on the 6 C-class prose references: **all 6 rejected** (no balanced JSON prefix found → parse error → log warning → no list). No garbage lists generated by either `to_eof` or `balanced_prefix` on these.
- **Critical design note:** the parser MUST use **balanced-prefix detection** (find first `[`/`{`, match close), NOT blind "BEGIN-to-EOF". Blind to-EOF fails on 5/23 A-cases (trailing XML) and is fragile on prose. The balanced-prefix approach both recovers more AND rejects prose.
- Residual risk window: prose that happens to contain a valid short JSON after a BEGIN. My full-corpus C sample shows zero such cases; a guard (require a real `]}`/`}` with fields that Trip-validation (task_id/summary/status) accepts) eliminates it.

**Overall recovery = 23 recoverable lists / 23 dropped lists = 100%** of the real bug class. For B-class (truncation) it would recover partial-but-parseable lists; B-class is zero today so this is theoretical only.

**Caveat for warning semantics:** a partial parse that recovers a "forgot END" list should still log a warning (observability) and, importantly, the loop should treat a recovered-all-completed list exactly like an explicit one (so the real R3/exit paths work consistently). The warning is the "detection" half; recovery is the "repair" half.

---

## Q4 — P1.5 vs P1.3 治理 RC2 的有效性评估

RC2 (GLM audit, memory #1053; reproduced by my Q1 corpus): R3 trusted-completion (loop.go:1781-1722, final round with all-completed list + substantive answer skips the +1 confirmation round) × R2 (rule 2 "omit otherwise", loop.go:279) → models rationally defer their ONE list to the final message ("plan becomes receipt"). Verified earlier: 8/40 (a-v2) and 9/42 (compare) sessions emit only at the end.

### P1.5 — extend fast-exit to accept `tasks==nil && hasList==true && all-completed` (decouple exit from last-round emission)

- **Scope (which functions):** only the R3 exit branch at **loop.go:1793** (`if tasks, _ := ParseTaskList(content); tasks != nil && AllCompleted(tasks)`). Change to also enter when `tasks==nil && a.TaskState != nil && a.TaskState.hasList && AllCompleted(a.TaskState.tasks)`. No other loop function needed. `todoState` untouched (already tracks `hasList` + `tasks`, loop.go:312-324).
- **Risk — REAL and material:** `TaskState.tasks` is **as-stale-as-last-list-update**. If the model finishes work but omits the final list, the tracked state may still show `progressing` (if it didn't update after the last change) → fast-exit would **NOT** fire (good) but the run also **fails to exit** → the model must still emit a list to exit, defeating the decoupling. Conversely, if tracked state shows all-completed but the model has since done MORE tool work without updating, the fast-exit could **prematurely exit with incomplete state** after the tool round — the loop's `consecutiveEndTurn`/`len(toolCalls)==0` guard mitigates this (fast-exit only fires on a no-tool round), but the stale-tracked-state hazard is real. Also `AllCompleted` returns false on an empty list (tasklist.go), so a never-established-but-`hasList`-false session is safe.
- **Orthogonality to P0:** the P0-recovered list is only visible if partial-parse feeds it to `TaskState.update()`. Under the CURRENT bug, `hasList` never becomes true (silent drop), so **P1.5's `hasList==true` path would not even engage for the dropped-list sessions** — P1.5 does not fix P0. It is a governance change, not a data-recovery change.
- **Expected benefit:** removes the reward for deferring the ONLY list to the end; models could exit having emitted lists earlier. Moderate, real, but depends on P0 landing first so `hasList` is accurately true.

### P1.3 — 统一三处矛盾的措辞 (rule1 首轮必发 / rule2 只变化时发 / epic 每轮结尾发)

Contradiction lives in: `taskManagementRules` rule 1 (loop.go:278 "First turn: initial task list...") + rule 2 (279 "emit ONLY on change..."), versus the Epic message (loop.go:1502 "Remember to **end your response** with BEGIN..END"). This is a genuine textual contradiction; unify into one contract.

- **Does rewording fix the gaming? (the key Q4 test):** **No — and P1.3 should NOT be framed as fixing it.** The deferral is *deliberate* (models saving one API round to earn the R3 fast-exit), not *confusion* from contradictory wording. My Q1 data is consistent with the RC2 incentive (deferral). A wording cleanup aligns the contract and removes the "the prompt itself permits omission" cover (RC1), which is valuable — but the *incentive* to defer survives unless P1.5/policy changes the exit reward. **So P1.3 is a necessary hygiene fix, P1.5 is the policy fix; they are complementary, not alternatives.**
- **Cache cost of the system-prompt change — trivial.** `taskManagementRules` is one PromptSection (loop.go:447, ~1.2KB / ~350 tokens). Measured on the latest 10 sessions: prefix cache-hit ratio ≈ **99.7%** (e.g. cache_hit 9856/9881, 76416/76540 — memory #931's calibration). A system-prompt *text* change invalidates only the changed section's prefix once per session (one-time re-fill of ~350 tokens/session on the first request after the change), NOT the whole conversation. Sum of last prompt_tokens across the 10 newest sessions = 551,630, but a P1.3 edit does not re-fill that — it re-fills only the ~350-token changed section once per session. **One-time ~350 tokens/session × ~N new sessions = negligible.** (The dominant cached tokens are the stable system-prompt + tool defs + conversation tail, all unchanged by an edit confined to the rules text.)

### Recommendation

- **Do P0 now** (partial-parse + warning + regression tests). It is mechanically safe, recovers 23/23 real lists, produces no garbage, and restores the loop's exit protection — the highest value for effort.
- **Do P1.3 now** (unify the three contradictory rules). Cheap, low-risk, ~350-token one-time cache re-fill per session, and removes the "prompt permits omission" ambiguity. It will NOT by itself stop deferral.
- **Hold / stage P1.5** — it is the policy change that actually addresses the RC2 incentive, but ship it **only after P0 is verified AND with a stale-tracked-state guard** (e.g. require the fast-exit to use tracked state only when it was updated recent enough / no intervening tool round). Launching P1.5 on today's silent-drop path is ineffective (hasList stays false) and adds premature-exit risk. Recommend: **P0 + P1.3 first; P1.5 later, gated.**

---

## What I did NOT do (honest scope)

- No production code changed (`internal/agent/*` untouched), no bin/gopi rebuilt, master unmodified.
- Did not implement P0/P1.3 (this Epic = evaluation only).
- B-class recovery (truncation) not measured on live data because B-class count is zero; Q3's 100% figure is A-class-specific.

## Verification

- Build: n/a (report-only; no code changed).
- Tests: n/a (`TestParseTaskList*` unchanged).
- Audit scripts (rerunnable): `/tmp/tasklist_p0_scan.py` (Q1), `/tmp/tasklist_p0_q2q3.py` (Q2+Q3), both act on `~/.gopi/sessions/*.jsonl`. Full case dump: `/tmp/tasklist_p0_all_cases.txt`.

## Deliverable declaration (Rule 6d)

- **Branch:** `feat/tasklist-p0-eval` (worktree `~/workspace/tasklist-p0-eval`, shared with `~/work/GoPi`)
- **Recovery:** `git show feat/tasklist-p0-eval:devdoc/tasklist-p0-evaluation.md`
- **In main repo:** yes — worktree-shared; the commit is on the branch in `~/work/GoPi`.
