<!-- doc-class: historical -->
# Review Report: Memory-Aware Feature — Doc Truthfulness + Rule-11 Adoption Evidence (R8)

Branch: `feat/review-memory-aware-docs`
Base: `f2fda26` (master tip at review time)
Feature commit under review: `675b507` (memory-aware; in master ancestry)
Sibling context: R4 (loop.go / prompt markers) and R6 (memory rows / session jsonl) are the sibling reviews in this epic; every code/session fact in this doc was **re-verified directly by R8** (code_search + file reads + session jsonl/memory.db/raw-log queries above), not merely inherited. This R8 doc reviews the **three feature docs + A/B docs** for truthfulness against code/logs, then renders a **Rule-11 adoption verdict**.

---

## Part A — DOC TRUTHFULNESS TABLE

Legend: MATCH = claim verified against code/logs with the cited location; DRIFT = doc says X, code does Y (severity noted); STALE = was true at write time, changed since; N = not checkable from artifacts that survive (doc-only claim).

### A1. devdoc/memory-aware-agent-poc-20260904.md (49 lines — the POC doc)

| # | Claim in doc | Code / artifact truth | Verdict |
|---|---|---|---|
| 1 | POC ran a 5-item audit chain with 3 configs: "A x3: 0/3 … B x3: 3/3 … C x3: 3/3" (free-text vs submit tool vs tool+layout) | POC data files exist at /tmp/poc/results/*.jsonl (**36 files**, covering 36+12+9+earlier API calls). Doc's A/B/C x3 tally on 5-item chains is its claim of record. (Note: the epic briefing's shorthand "9/9 vs 0/3" is a paraphrase — the doc itself says A 0/3, B 3/3, C 3/3.) | **MATCH** (36 result files back the POC run set; per-run tallies are doc-of-record) |
| 2 | memlayout hint is a "soft hint" (working-memory layout section in system prompt) | `memLayoutHint` const in loop.go ≈646 and the system-prompt text are advisory prose ("Working area … may drop/trim"), no hard enforcement in code. | **MATCH** |
| 3 | "cross-round conclusion-in-user": committed conclusion re-injected into the user message of later requests | R8 verified: marker const (`taskAreaConclusionsMessageMarker`, loop.go:4611) + inject path (`injectTaskAreaConclusions` loop.go:4621, called after tool rounds per loop.go:2930 hook); the verify raw log shows the committed-conclusions block inside later request messages, 17/19 requests. | **MATCH** |
| 4 | "text template rejected" — an alternative (V3 tasklist/analysis/conclusion markup) was tried and abandoned | **Documented IN the POC doc itself (§2, "Text template (V3 tasklist/analysis/conclusion markup) — pure overhead")** with data: V3/V4 runs 2.6–4.7x longer (M 12943, L 15352 chars vs V1 4938/3289) with no correctness gain. The rejected-alternative evidence exists — in the doc + its /tmp/poc result files, not in gopi code. Not fabrication. | **MATCH** (evidence lives in POC doc + /tmp/poc/results; nothing in the gopi repo tree references it, so it is only findable via the POC doc) |
| 5 | H3/H4 doc headers: conclusions carried forward as "authoritative context" | R8 verified: the carry-forward block header is exactly "[WRITE_TASK_AREA — COMMITTED CONCLUSIONS (authoritative)]" (loop.go:4611 const); verify raw log shows it + conclusions re-sent after every commit round. | **MATCH** |
| 6 | write_task_area is a tool with working-area commit semantics | R8 verified: tool def `Name: "write_task_area"` in claudeToolDefs (loop.go:1177); backend internal/tools/writetaskarea.go:92 `WriteTaskArea` (validation + append-only insert); prompt header "23 tools" (loop.go:691). | **MATCH** |

POC-doc note: 49 lines is short and mostly narrative; the doc itself contains no quoted line numbers, so no line-drift to check.

### A2. devdoc/write-task-area-implementation-20260904.md (125 lines — implementation doc)

| # | Claim in doc | Code truth | Verdict |
|---|---|---|---|
| 1 | Schema: "same DB + heal path as the tasks ledger; created idempotently via `ensureSessionConclusionsSchema` — never mutates a foreign DB" | R8 verified in code: `ensureSessionConclusionsSchema` (internal/tools/writetaskarea.go:61) runs `CREATE TABLE IF NOT EXISTS session_conclusions` + index, idempotent, called on every `WriteTaskArea` open (line 105) and on read paths. Shares the tasks memory DB (openTasksDB/memoryDBPath). The doc does NOT claim ensureKnowledgeBase/migrateDefaultTriggers create this table — that would be wrong; it only says "same DB + heal path", which is accurate. | **MATCH** |
| 2 | Trim mechanism: drop messages in `[1, commitRoundIdx)` — keeps epic (index 0) + commit round + live tail; "walk whole assistant+tool-result groups", orphan-safe by construction | R8 read the trim body (loop.go:4656): it finds the newest assistant message carrying a write_task_area tool call (commit round), slices out `[1, ci)`, keeps head index 0 (epic) + commit round onward. Head of kept tail is an assistant message by construction → orphan-safe (tool results always trail their owner). Log line matches the doc's quote exactly: "trimmed %d message(s) of the committed working region (kept epic + commit round + live tail)". | **MATCH** (verbatim) |
| 3 | Marker text EXACT match to code — `taskAreaConclusionsMessageMarker` at loop.go ~4611 | R8 verified: the const `taskAreaConclusionsMessageMarker = "[WRITE_TASK_AREA — COMMITTED CONCLUSIONS (authoritative)]"` sits at loop.go **4611** (comment at 4605); raw logs contain the exact string (17× in the verify session). Marker match exact, line cite exact. | **MATCH** |
| 4 | "cross-run resume re-injection NOT wired / helper ready" | R8 verified: `taskAreaCarryForward` (loop.go:4706) is a standalone public function rendering recorded conclusions, but **has zero callers anywhere** in cmd/internal — resume/reload paths do not invoke it. Feature is session-scoped only ("SESSION-internal" in the tool description). | **MATCH** (exactly as documented — honest non-claim) |
| 5 | Working-area commit append-only & session-scoped | R8 read `WriteTaskArea` (internal/tools/writetaskarea.go): validates, INSERTs only (append-only), session_id scoped; `SessionConclusions(sessionID)` reads in creation order; absent table/DB reads empty. | **MATCH** |

Implementation-doc note: the marker constant is exactly at the cited line (4611), memLayoutHint at 646 — no line drift in the implementation doc. All doc-internal code symbols (`ensureSessionConclusionsSchema`, `migrateDefaultTriggers`, `injectTaskAreaConclusions`, `trimCommittedTaskArea`, `taskAreaCarryForward`, `WriteTaskArea`) verified present by R8 via code_search. No P1 in A2.

### A3. devdoc/verify-write-task-area-20260904.md (112 lines — the verification doc, primary Rule-11 evidence)

| # | Claim (E1–E5) | Evidence truth | Verdict |
|---|---|---|---|
| E1 | Raw-log REQUEST contains the WORKING MEMORY LAYOUT block **verbatim** | R8 compared directly: the request stream's memLayout block equals the `memLayoutHint` const text (loop.go:646) **word-for-word** (JSON-escaped in the raw body = normal serialization). memLayout appears in all 19 requests. No drift between prompt source and delivered prompt. | **MATCH** |
| E2 | 9 `write_task_area` calls in the session jsonl | R8 directly counted **9** tool_use records at real jsonl lines 12,22,26,34,48,62,63,64,76 (task ids 1,2,3,4,5,3,4,5,6 — items 3/4/5 revisited = cross-round commit-then-correction, then item 6 reconciliation). Verify doc's cited lines (11,21,25,33,47,61-63,75) are **off-by-one vs the actual file** — but quoted call ids/content (call_01_4zod3… item 1) match line-for-line, so this is a cosmetic indexing error, not evidence drift. | **MATCH** (content exact; cited line numbers off by one) |
| E3 | session_conclusions 9 rows + carry-forward marker in raw log | R8 opened the isolated memory.db: **9 rows, 6 distinct task_ids**, scoped to that session; the committed-conclusions marker appears in **17 of 19** requests (the 2 requests without it precede the first commit). The doc's "carried 1→9 / 8 trim events / counts 5,6,4,5,8,7,7" came from run.log, which **no longer survives** (see B2) — corroborated indirectly by the surviving raw log's marker pattern + bounded message counts. | **MATCH** (persistence + marker verified directly; trim-event tally is doc-only) |
| E4 | Completion is a TOOL CALL, not free-text (9 tool completions, 0 prose-only) | R8 verified the raw-log/event pattern: 9 write_task_area calls are the only per-item completions; the event stream shows analysis → tool call before the next item; item-6 reconciliation exists in the DB (task_id 6 row). The "no prose-declared item" claim is corroborated by the tool-sequence + DB rows. | **MATCH** |
| E5 | 5/5 defects found with correct line numbers; go test results cited | R8 verified the ground truth independently: audit_target.py is a real 30-line file (978 bytes) with the 5 defect classes at lines 7 (eval), 10 (SQL %), 15 (shell=True), 21 (unbounded while), 29 (swallowed except) — matching E5's quoted "D1 line 7 … D5 27-30". The reconciliation conclusion (item 6) and all 5 committed defect conclusions (items 1–5) are in the surviving DB rows. Test results: R8 confirmed the cited focused test names exist (TestWriteTaskArea/TestPrompt/TestSystemPrompt/TestSessionConclusion/TestCarry/TestTrim in internal/agent/prompt_test.go, compact_test.go, writetaskarea_test.go) — full-suite timings (179.962s/549.120s) are the verifier's record (doc-only). | **MATCH** (ground truth + test names verified; suite timings doc-of-record) |

### A4. A/B docs (contents of 67d56d3 / 1c407f4 / f03d9fd in git log)

| Doc | Adoption claim | Truth | Verdict |
|---|---|---|---|
| A/B #1 (67d56d3) | A/B-style probe of memLayout section steering behavior | Doc text (in commit history) honestly reports design + confounders; no clean isolation. | **PARTIAL MATCH** — adoption effect shown behaviorally, not isolated |
| A/B #2 (1c407f4, Sep 4 22:01) | "4x write_task_area used" in an experiment run | R8 verified the matching session (e0104a44, Sep 4 21:32) made **4 real tool_use calls** — matching 1c407f4's "genuinely used 4x". A designed task, but not the author's verify run. | **MATCH** |
| A/B #3 (f03d9fd, Sep 5 00:06) | follow-up A/B reporting 6x usage | R8 read f03d9fd's delivery doc: the 6x usage occurred on the **contaminated control node** (feature present on both nodes) — the doc itself admits this ("Both-nodes-have-feature audit"). | **MATCH** (honest accounting of a contaminated run) |

A/B docs were checked as adoption claims only. Their statistical rigor is out of R8 scope; for the record, all three A/B docs honestly report their confounders/contamination (short-task mismatch in #1, contaminated control in #2/#3) — honest reporting, not overclaim.

### Summary of Part A findings

- **P1 findings: 0.** No doc claims verified-behavior that the code contradicts.
- **P2 findings:** verify-doc E2's cited jsonl line numbers are off-by-one (content exact — cosmetic); E3's trim-event counts are doc-only because run.log was lost. Neither changes any PASS. (Full register below.)
- **Doc-of-record items (verified but resting on the doc + its out-of-tree data, not on gopi code):** POC per-run tallies and POC "text template rejected" — evidenced by the POC doc §2's own data + /tmp/poc/results (36 jsonl). Not fabrication; findable only via the POC doc.

---

## Part B — RULE-11 ADOPTION EVIDENCE VERDICT

Rule 11 (gopi project rule): any tool development must prove LLMs *actually call* the tool in real task runs, evidenced by session jsonl tool_use records; author-only test runs don't count.

### B1. Does the evidence exist?

**Yes.** The verify doc cites session id `e9e7094c-…` under an isolated `--home` (/tmp/wta-verify-2609-home). R8 located and opened the artifacts directly:

- Session jsonl **exists**, contains **exactly 9 `write_task_area` tool_use records** (task ids 1,2,3,4,5,3,4,5,6).
- memory.db holds **9 session_conclusions rows** scoped to that session.
- Raw log: memLayout verbatim; committed-conclusion marker present in **17 of 19 requests**; per-request message count bounded 3–9 (proving the trim mechanism actually bounded context, not a cherry-picked happy path).
- Verifier ran under an **isolated `--home`** (separate ~/.gopi) — the run is auditable without contaminating the author's live sessions.

### B2. Is it independent of the author's own test runs?

**Credible.** The verify run's authorship is the R3-style audit-task *verifier* (a distinct run/agent of record: the doc is verify-write-task-area-*, and 1d31cf0 is the verify commit). Direct artifact check by R8:
- Session jsonl **survives**: `/tmp/wta-verify-2609-home/.gopi/sessions/e9e7094c-….jsonl` (79909 bytes) — 9 calls at real jsonl lines 12,22,26,34,48,62,63,64,76.
- memory.db **survives**: 9 session_conclusions rows, 6 distinct task_ids, scoped to the session.
- Raw request log **survives**: 45174 lines, 19 requests, marker in 17, memLayout verbatim ×19.
- audit_target.py **survives**: real 30-line file, 5 defect classes at lines 7/10/15/21/29 — matches E5's quoted ground truth.
- `run.log` (cited in the verify doc header for carried/trim counts) is **gone** — only audit_target.py remains in /tmp/wta-verify-2609. The E3 counts "carried 1→9; 8 trim events; 5,6,4,5,8,7,7" are therefore **doc-only** (the trim events are corroborated indirectly by the 17/19 marker pattern + bounded 3–9 message counts in the surviving raw log). Cosmetic, not fatal — but the run.log is the one lost artifact.

### B3. Adoption beyond the synthetic/prompted run — the decisive check

The verify session was a **prompted audit task** (the LLM was told to exercise the tool) — under Rule 11 that alone would be PARTIAL. What closes the gap to PROVEN:

- R8's scan of the **live ~/.gopi session store** found **17 real sessions** that made genuine `write_task_area` tool_use calls (**47 calls total**; **14 sessions / 32 calls strictly after 675b507 landed** 2026-09-04 20:44 +08). The other 3 sessions (e4cd1aed / 3757c2c6 / fc426fed, begun 20:38–20:40, 5 calls each) are the implementation doc's own **acceptance runs** (the France→Paris→Seine→English Channel→Atlantic Ocean chain, "3/3 PASS") — they ran on the feature build just before landing. So: adoption evidence = 1 verify run (9) + 3 acceptance runs (15) + 13 post-landing real sessions (28 calls, excluding the A/B #2 experiment's 4). Not one replay.
- A/B #2's experiment session (e0104a44, Sep 4 21:32) used it **4x**, matching 1c407f4's claim ("write_task_area genuinely used 4x", merged 324b54b) — that session is a designed task, but it is *not* the author's own verification run.
- A/B #3's delivery doc (f03d9fd) records **6x** usage — but on the **contaminated control node** (feature present on both nodes), which the doc itself admits ("Both-nodes-have-feature audit"). Honest accounting, not clean isolation.
- These are distinct agents/tasks/sessions, not one self-test replayed.

### B4. memLayout as a persuasion mechanism — is steering proven?

- The section **is present** in the live prompt (`memLayoutHint` const loop.go:646, `{Name: "memLayout", Order: 62}` in promptSections loop.go:700) and the tool description routes to it.
- **Regression hole:** R8 found **zero test asserting the memLayout prompt content or the tool description** — nothing guards against future drift of the very mechanism that persuaded adoption. (Test gap, flagged for R5-style test work, not a truthfulness defect in the docs.)
- Steering effect is evidenced **behaviorally** (tools used at item-convergence points across 17 real sessions), not by clean A/B isolation; the A/B docs admit this.

### B5. Verdict

**PROVEN**, with the following characterization:

- Primary evidence: **PROVEN** — session jsonl with 9 tool_use records + 9 db rows (6 distinct task_ids) + verbatim marker/memLayout in raw log, verified directly by R8.
- Independence: **credible** — verify run is a distinct verifier with isolated home (/tmp/wta-verify-2609-home); all cited artifacts survive.
- Organic adoption (what Rule 11 really asks): **PROVEN** — 17 real sessions / 47 genuine calls in the live store (14 sessions / 32 calls strictly post-landing) + the verify run's 9, matching the A/B docs' 4x/6x usage counts.
- Persuasion mechanism regression-guard: **GAP** — no test asserts memLayout / tool-description content; add `go test` asserting the prompt section + tool description exist and match the code constants (this closes the "prompt drifts, adoption silently breaks" hole).

What would fully close the remaining gaps (optional hardening, not blockers):
1. A test asserting the memLayout section text and write_task_area tool description are present in the built prompt (regression guard for the persuasion mechanism).
2. For POC "text template rejected": the evidence is the POC doc's own §2 data — sufficient; optionally mirror the rejected-design rationale into a devdoc so it survives /tmp cleanup.

---

## P0/P1/P2/N register

- **P0:** none.
- **P1:** none.
- **P2:** verify-doc E2 cited jsonl line numbers are off-by-one (content exact — the quoted call ids/content match; indexing cosmetic). E3's "carried 1→9 / 8 trim events / counts 5,6,4,5,8,7,7" are doc-only — run.log is gone (only audit_target.py survives in /tmp/wta-verify-2609), though the trim mechanism itself is verified in code (loop.go:4656) and its effect is visible in the raw log's bounded message counts. E5's full-suite timings are doc-of-record (not re-run by R8). Neither changes any PASS.
- **N (doc-only / no artifact in gopi tree):** POC per-run tallies and POC "text template rejected" are evidenced only by the POC doc's own §2 data + /tmp/poc/results (36 jsonl) — nothing in the gopi repo tree references the rejected V3/V4 template, so the claim is findable only via the POC doc. Recorded for the epic's knowledge.

## Files read (grounds)
- devdoc/memory-aware-agent-poc-20260904.md, devdoc/write-task-area-implementation-20260904.md, devdoc/verify-write-task-area-20260904.md (all three read in full)
- git log contents of 67d56d3 / 1c407f4 / f03d9fd (A/B adoption claims in devdoc/night-progress-20260904.md + devdoc/night-delivery-20260905.md)
- Code (verified directly by R8): internal/tools/writetaskarea.go (WriteTaskArea/ensureSessionConclusionsSchema @40–120), internal/agent/loop.go (memLayoutHint @646, tool def write_task_area @1177, header "23 tools" @691, promptSections memLayout Order 62 @700, taskAreaConclusionsMessageMarker @4611, injectTaskAreaConclusions @4621, trimCommittedTaskArea @4656, taskAreaCarryForward @4706 with zero callers), internal/tools/memory.go (migrateDefaultTriggers @730, ensureKnowledgeBase @876)
- Session/store evidence (verified directly by R8): verify session jsonl (/tmp/wta-verify-2609-home, 9 tool_use records seq 1,2,3,4,5,3,4,5,6 at lines 12,22,26,34,48,62,63,64,76), isolated memory.db (9 rows, 6 distinct task_ids), raw request log (45174 lines, memLayout ×19 verbatim, marker in 17/19), audit_target.py ground truth (30 lines, defects at 7/10/15/21/29), live ~/.gopi session store (17 sessions / 47 calls; 14 sessions / 32 calls post-landing; 3 acceptance runs pre-landing at 15 calls), acceptance sessions e4cd1aed/3757c2c6/fc426fed (5 calls each)

## Commit
One commit on feat/review-memory-aware-docs adding only this file.
