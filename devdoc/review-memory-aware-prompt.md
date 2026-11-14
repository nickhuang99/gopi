<!-- doc-class: historical -->
# REVIEW: memory-aware R4 — memLayout system-prompt section + density/marker tests

**Reviewer:** independent agent (child R4 of the memory-aware epic)
**Date:** 2026-09-05
**Commit under review:** `675b507` ("feat(write-task-area): session-internal subtask commit tool + working-memory soft hint")
**Review base:** `d8c4472` (master at review time; 675b507 is an ancestor)
**Branch:** `feat/review-memory-aware-prompt`
**Epic plan:** `devdoc/review-memory-aware-EPIC-PLAN.md` (cross-cutting lenses: comment↔code drift, Rule 11 LLM-adoption evidence, density gate)

Scope per briefing: `internal/agent/loop.go` memLayout region, `promptSections` registry,
`assembleSystemPrompt`, `prompt_test.go` marker/density coverage, 4500-char budget, Rule 11
adoption evidence, tool-count consistency, comment↔code drift.

**Verdict: 0 P0, 0 P1, 4 P2, 1 P3.** The feature is sound and the Rule-11 adoption claims are
real (verified against request-level logs, not implementer prose). The one genuine gap is a
**regression hole**: no test asserts the memLayout section's presence or content, so a future
edit could silently drop the entire feature from the system prompt without any test failing.

---

## Evidence basis (fresh measurements, not implementer prose)

All numeric claims below were re-derived during this review:

| Measurement | Value | How |
|---|---|---|
| Current assembled prompt length | **4497 chars** (headroom 3) | `go test -run TestZZDensityProbe` probe in worktree |
| 675b507~1 (baseline) assembled length | **4498 chars** (headroom 2) | section-length sum of parent consts |
| memLayoutHint section text | **231 chars** (Go source 227 visible + em-dash/arrow multibyte) | `len()` probe |
| tickClockRule | 128 chars | `len()` probe |
| Discipline-block totals | 675b507~1: 3458 → 675b507: 3455 (net **−3** with memLayout added) | python const extraction at both commits |
| write_task_area Description | 1106 chars (concatenated string literals) | python Go-string-concat eval |
| Tool defs in `claudeToolDefs` | **23** | `Name:` defs, lines 734–1262 band |
| Density test markers asserted | 10 content checks (see §4) | read of prompt_test.go |

The 675b507 commit-message claim ("compressed existing routing/efficiency/job/engineering
blocks to honor the 4500-char gate; baseline had 2 chars headroom") is **verified accurate**:
baseline 4498, current 4497, memLayout squeezed in by real compression (−192 toolRouting, −9
job, −29 engineering; efficiency untouched).

---

## Findings

### P0 — none

### P1 — none

### P2-1: NO test asserts the memLayout section's presence or content — regression hole
**File:** `internal/agent/prompt_test.go:70-86` (`TestSystemPromptDensity`)
**Severity:** P2 (test-coverage gap, not a live defect)

`TestSystemPromptDensity` asserts section markers for `TOOL ROUTING`, `EFFICIENCY RULES`,
`TASK MANAGEMENT RULES`, `BASH EFFICIENCY RULES`, `JOB RULES`, `THINKING RULES` — plus 4
memory-habit lines, the CLOCK-TICK lines, and the MUST-style guard — **but not** the memLayout
section. The test comment says "Every block must actually be present (nothing silently
dropped)" — yet the memLayout entry in the registry (`loop.go:699`) has no corresponding
marker assert. None of the following strings is asserted anywhere in a test:
`WORKING MEMORY LAYOUT`, `memLayout`, `write_task_area`, `CONCLUSION`, `BOUNDED`,
`do NOT re-derive` (verified: grep across `internal/agent/*_test.go` finds zero memLayout
content asserts; the only `write_task_area` test refs are the writetaskarea_test.go
tool-call flow tests, which exercise the tool handler, not the prompt text).

**Consequence:** the density test gates only TOTAL length (4497 < 4500). A future edit that
deleted the memLayout registry entry would shorten the prompt by 231 chars, INCREASING
headroom — every test still passes. The 4500 gate actively rewards dropping the section.
This is precisely the "density squeeze" hazard the epic lens warns about.

**Why the gate lets it through:** the density budget has 3 chars of headroom. The previous
compressions (675b507) already took toolRouting from 1501→1309 chars; the next density
emergency has no obvious fat left except engineering/taskManagement — or the newest, least
"owned" block (memLayout, added in the same commit as the write_task_area feature).

**Fix (recommended, not applied — review-only):** add to the density test's marker loop:
```go
for _, name := range []string{"TOOL ROUTING", ..., "WORKING MEMORY LAYOUT"} {
```
plus content asserts `strings.Contains(p, "write_task_area")` and
`strings.Contains(p, "do NOT re-derive")`. Mirrors the existing CLOCK-TICK guard pattern.

### P2-2: memLayoutHint const has no sibling-style explanatory comment — future readers won't know WHY it exists or that it must stay under budget
**File:** `internal/agent/loop.go:646-648`
**Severity:** P2 (comment↔code / maintainability)

Every sibling const carries a substantive doc comment: `conciseThinkingRules`
(loop.go:650-656, references competition #3/#4 data + devdoc/thinking-budget-fix.md),
`tickClockRule` (loop.go:661-665, explains clock-tick rationale + "Kept terse (~30 tokens)
to fit the 4500-char density gate"). `memLayoutHint` (added in 675b507) sits between them
with **zero comment**. Blame confirms: 675b507 (gopi author) added lines 646-648 with no
comment; the em-dash "— soft" in the header title is the only hint that this is a "soft"
(volitional, prompt-only) section vs the hard guarantees elsewhere.

The briefing's own question ("does 'committed, do NOT re-derive' interact safely with the
todo/task-list rules?") has a subtle answer that a comment should record: memLayout is
**soft/volitional** while taskManagementRules' re-emission duty is **structural** (todo
tool + TASK_LIST re-emission). Both live in the same prompt at Orders 30 and 62. The model
must hold "committed items are done" AND "the live task list still gets re-emitted every 3
responses" simultaneously — the memLayout hint gives no guidance on the boundary (commit
item 3, then re-emit the list showing 3 done, 4-6 pending). A comment capturing this
intended interaction would prevent a future edit from tightening memLayout into "never
re-emit" — which WOULD conflict with taskManagementRules.

### P2-3: toolRouting compression (1501→1309) removed routing nuance that other tools still need — not a live bug, but the -192 cut is the thinnest ice for future density squeezes
**File:** `internal/agent/loop.go` toolRoutingRules (675b507 diff)
**Severity:** P2 (latent — content the -192 compression dropped)

675b507's diff shows the toolRouting cuts included:
- `git_blame` line: dropped `"call git_blame(file_path[,start,end]), not bash git blame"` → the routing rule still says use git_blame but no longer carries the call-signature reminder (the tool's own Description carries it, so this is double-write redundancy trimmed — acceptable).
- The code_search "Reviewing code? resolve each unknown identifier" guidance was dropped/merged.
- Rule-4 "BE CONCISE" line trimmed.

None of the cuts removed a string that any test asserts (verified: the 10 density-test
markers + 4 memory habit + 2 clock lines all still present). The engineering cut removed
only "(OCR can't see quality loss)" — a parenthetical, also unasserted. **No marker was lost.**

The P2 is forward-looking: with headroom at 3 chars, toolRouting (1309) and engineering
(584) are the only compressible blocks left, and both are where routing nuance lives. Any
future section addition (R4's own report aside) will cut here again — the density test
cannot tell a routing-nuance cut from a marker cut.

### P2-4: promptSections comment says blocks "follow in the order they were previously concatenated" — no longer accurate after the order-sorted refactor
**File:** `internal/agent/loop.go:686-689`
**Severity:** P2 (comment drift)

The comment (dating to ccae5da, "P0-2 — system prompt sections with order-sorted assembly")
says "the discipline blocks follow in the order they were previously concatenated." But
assembly is now order-sorted ascending (`assembleSystemPrompt` uses `sort.SliceStable`),
and the registry orders (10/20/30/40/50/55/60/62/65) do NOT match any "previously
concatenated" literal order — 55 (engineering) and 62 (memLayout) are insertions between
the 50/60/65 landmarks. The comment describes pre-refactor history, not the current
registry. Minor, but it is exactly the class of comment↔code drift the epic lens targets.
(engineering at Order 55 was added by c5ac23c; memLayout at 62 by 675b507.)

### P3-1: Order 62 placement is defensible but the section sits at the BOTTOM of a top-down-read prompt — soft-steering reach is the weakest of all sections
**File:** `internal/agent/loop.go:699`
**Severity:** P3 (design observation, no action required)

Section order in the assembled prompt (top-down as the model reads it): header(0) →
toolRouting(10) → efficiency(20) → taskManagement(30) → bashEfficiency(40) → job(50) →
engineering(55) → conciseThinking(60) → **memLayout(62)** → tickClock(65).

The briefing asks whether 62 is the right slot given the model reads top-down. Analysis:
- The memLayout instruction is about the model's OWN working-memory behavior and is only
  actionable mid-task (after items start converging), so late placement is defensible —
  it is nearest the "now" of execution, adjacent to conciseThinking (60) and right before
  the tickClock (65), which is the last thing read before tool rounds begin.
- taskManagement (30) is structurally earlier and its TASK_LIST re-emission duty is the
  only rule memLayout could appear to contradict; both being present, the model resolves
  them contextually (commit item → re-emit list with that item's status). No conflict was
  observed in the adoption evidence (see §6).
- The genuine softness: at 231 chars and position 8-of-10, memLayout is the least
  prominent behavioral instruction in the prompt. The POC itself concluded "the discipline
  a template is meant to force is better delivered by a tool call, not by markup the model
  must reproduce" (devdoc/memory-aware-agent-poc-20260904.md:22) — i.e. the section is a
  nudge; the write_task_area tool is the enforcement. Order 62 is fine because the section
  is not where the guarantee lives.

---

## Section-by-section answers to the briefing

### 1. memLayoutHint wording vs what the model can DO (premature-commit risk)
**Real but contained.** The hint says "Item converges → write_task_area a CONCLUSION;
committed, do NOT re-derive." The convergence decision is entirely the model's — nothing in
the tool or the hint verifies the item actually converged before commit. The 675b507
verification session is direct evidence: the model **committed item 1's conclusion before
reading the file** (fabricated line numbers from prior knowledge), then had to repair via
3 CORRECTION + 1 RECONCILIATION records (9 records for 7 distinct task_ids; 44% churn).
The verify report (devdoc/verify-write-task-area-20260904.md, "Notable observation")
frames this as "the loop at work" — correct-and-repair works BECAUSE conclusions are
append-only and authoritative-but-correctable. So the premature-commit risk is real but the
design absorbs it: the hint's "do NOT re-derive" applies to committed truth, and the
correction path (re-commit with CORRECTION) is exercised naturally. A stricter hint
("only commit after verification") would fight the POC finding that the tool call — not
prompt discipline — is the reliable fix. **No change recommended**; the wording is honest
about being soft ("— soft" in the title).

### 2. memLayout vs taskManagementRules interaction (conflicting-signal audit)
**No conflict in practice.** taskManagementRules (Order 30) mandates TASK_LIST re-emission
every 3 responses; memLayout (Order 62) says committed items are authoritative and not to
re-derive. These are orthogonal: re-emission is a *status* duty (list shows item done), not
a *re-derivation* duty. The model in the adoption session committed 6 items AND produced a
final summary — both duties discharged. memLayout cannot override the re-emission
obligation because (a) it is soft and (b) the todo tool + task-list framing are
harness-visible structures, not prompt-only suggestions. The one genuine gap is the missing
guidance on the commit-then-re-emit boundary (P2-2 above). No other section contradicts
memLayout: efficiency/bashEfficiency/job/engineering are all tool-behavior rules that
memLayout (a memory-management rule) does not touch.

### 3. assembleSystemPrompt (empty-drop, density)
**Correct as implemented.** Empty sections dropped (`if s.Text == ""`), ascending order via
`sort.SliceStable`, blank-line join. Three unit tests cover order, empty-drop, and stable
order — all PASS. Density: 4497/4500 (headroom 3). The "density target <4500" comment
(citing dsh's 4.1K) is accurate.

### 4. Test coverage of the PROMPT section — THE GAP (P2-1)
Coverage matrix of the density test's asserts:

| What could silently drop | Asserted? |
|---|---|
| toolRouting | ✅ `TOOL ROUTING` |
| efficiency | ✅ `EFFICIENCY RULES` |
| taskManagement | ✅ `TASK MANAGEMENT RULES` |
| bashEfficiency | ✅ `BASH EFFICIENCY RULES` |
| job | ✅ `JOB RULES` |
| conciseThinking | ✅ `THINKING RULES` |
| **memLayout** | ❌ **nothing** |
| tickClock | ✅ `ticking clock…` + `[t+NNNs]` |
| engineering | ❌ no header assert (but engineering is a discipline block whose rules overlap others; lower risk) |
| memory habits | ✅ 4 asserts |
| MUST-style | ✅ negative guard |

memLayout is the ONLY behavioral section with zero presence/content assert. Because the
density test only gates total length and dropping memLayout *helps* the gate, this is a
true regression hole (P2-1). No test asserts memLayoutHint's exact content ("write_task_area"
/ "CONCLUSION" / "BOUNDED" — all unasserted).

### 5. The 4500-char budget — current state
Verified: 4497 chars, **3 chars headroom**. memLayout's 231 chars did squeeze the budget
(compressions −230 net of the +231 addition at baseline 4498). Which sections were
compressed in 675b507 and what they lost:

| Section | 675b507~1 | 675b507 | Cut | Marker lost? |
|---|---|---|---|---|
| toolRouting | 1501 | 1309 | −192 | no |
| job | 296 | 287 | −9 | no |
| engineering | 613 | 584 | −29 | no |
| memLayout | — | 227 | +227 | n/a |
| efficiency | 268 | 268 | 0 | no |

**No test-required marker string was lost** (verified by diff + grep of asserted markers).
But at 3 chars headroom, the budget is effectively exhausted — the next section addition
requires another compression round, and memLayout (231 chars, unasserted, added last) is
the most likely victim. This is the concrete mechanism behind P2-1.

### 6. Rule 11 (LLM-adoption evidence) — CLAIMS VERIFIED REAL
The two devdocs' adoption claims were re-verified against the actual evidence artifacts
(not implementer prose):

- **Session JSONL exists and contains the claimed records:** `/tmp/wta-verify-2609-home/.gopi/sessions/e9e7094c-24e0-40f5-92c7-176d63689704.jsonl` — grep confirms **9 `write_task_area` tool_use records** in the exact sequence the verify doc's E2 table claims (task_ids 1,2,3,4,5, then 3,4,5 CORRECTION, then 6 RECONCILIATION; all status="completed").
- **Raw request log exists and carries the memLayout section:** `/tmp/wta-verify-2609-home/.gopi/raw/gopi-raw-20260904-204710.log` (2.36 MB) — **19 `WORKING MEMORY LAYOUT` occurrences** (the section renders in the system block of every request round) + 28 `write_task_area` mentions. Baseline comparison in the verify doc (pre-feature prompt: 0 markers) is credible.
- **Correction-loop evidence:** the 3 CORRECTION + 1 RECONCILIATION records are visible in the JSONL itself — the "loop at work" claim is not verifier embellishment.

**Tool-description length (write_task_area = 1106 chars):** justified. It is a
session-internal tool whose entire persuasion contract lives in the description (CALL ONCE
PER ITEM / never batch / never free-text / schema semantics / what COMMITTED means /
session-internal vs task-ledger distinction). It is long but structurally comparable to
memory_feedback (1081), memory_search (1070), memory_store (1038), code_search (919),
context_trim (888) — the "behaviorally rich" tools all run 750-1100 chars. Only the trivial
tools (task_evaluate 192, job_kill 217, write 465) are short. Length is consistent with
sibling tools, not an outlier.

**"23 tools" header consistency:** verified — `claudeToolDefs` holds exactly 23 `Name:`
defs (read, git_blame, write, edit, bash, code_search, keyword_search, web_search,
web_fetch, memory_search, memory_store, memory_feedback, memory_update, context_trim,
task_evaluate, task_submit, job_start, write_task_area, job_output, job_kill, todo,
vision_analyze, text2img), matching the header literal at loop.go:691. Note (cross-ref R2
report devdoc/review-memory-aware-agent-wiring.md R2-5): the count was PRE-EXISTING drift —
header said "20" from 3612bfa (when task_evaluate/task_submit were added) until 675b507
fixed it to 23 for the feature's own +1. The header is NOW correct. No parity test guards
header-count == len(claudeToolDefs) — the same class of drift the R2 report flagged;
recommendation stands (P2-class, tracked in R2).

### 7. Comment↔code drift in the memLayout region
Audited every comment in the region:

| Comment | Lines | Verdict |
|---|---|---|
| conciseThinkingRules header (competition #3 root cause: DeepSeek ignores budget_tokens; thinking ran ~7.5k tokens/round) | 650-653 | **Plausible** — devdoc/thinking-budget-fix.md EXISTS (5693 bytes) and covers exactly this; consistent with the "harness hard-caps thinking" claim (chatOnce downgrade). No contradiction found. |
| conciseThinkingRules header (competition #4 gap: gopi median ~430 chars vs claude ~280) | 654-656 | Plausible — matches the competition-#4 framing used throughout the epic; the density test itself cites "competition #4" for the 4.5K target. Not independently re-derivable here, but internally consistent. |
| tickClockRule comment ("Kept terse ~30 tokens to fit the 4500-char gate") | 661-665 | **Verified** — tickClockRule is 128 chars ≈ 30 tokens; density gate confirmed at 4500. |
| PromptSection struct comment (P0-2, dsh renderPrompt semantics) | 676-683 | Accurate — matches TestAssembleSystemPromptEmptySectionDropped. |
| promptSections comment ("follow in the order they were previously concatenated") | 686-689 | **DRIFTED (P2-4)** — order-sorted assembly + insertions (55, 62) make this historical, not current. |
| assembleSystemPrompt comment ("Density target <4.5K; dsh measured 4.1K") | 704-707 | Verified — actual 4497 < 4500. |
| memLayoutHint | 646-648 | **NO comment at all** (P2-2). |

No fabricated citation found: every referenced devdoc/mechanism in the region exists and is
consistent with the code it annotates. The two real drift items are P2-2 (missing comment)
and P2-4 (stale ordering comment).

---

## Verification

- **Build:** `go build ./...` — not re-run (review-only, no code change); worktree at d8c4472 builds clean by construction.
- **Tests:** `go test ./internal/agent/ -run 'TestSystemPromptDensity|TestAssembleSystemPrompt'` → all PASS (`TestSystemPromptDensity`, `TestAssembleSystemPromptOrder`, `TestAssembleSystemPromptEmptySectionDropped`, `TestAssembleSystemPromptStableOrder`). Density probe (temp test, removed) confirmed len=4497, all memLayout markers present in the LIVE assembled prompt.
- **Commit:** see below (docs-only review report).

## Not Done
- No code changes made (review-only per epic contract — R-reports do not patch; findings are for the epic's fix phase).
- The density-test regression-hole fix (P2-1) and comment additions (P2-2/P2-4) are RECOMMENDED, not applied.
- Sibling R2's tool-count-parity recommendation is cross-referenced, not re-derived.

## Report location
`devdoc/review-memory-aware-prompt.md` (this file), on branch `feat/review-memory-aware-prompt`.
