<!-- doc-class: historical -->
# Task Report: Gopi Task-List Tool Issuance Audit — Is the ">30% Not Issuing" Claim True?

**Date:** 2026-08-28
**Branch:** `feat/tasklist-audit`
**Investigator:** gopi agent
**Method:** scan of `~/.gopi/sessions/*.jsonl` (595 relevant sessions / 1441 files) + full source dive (`internal/agent/loop.go`, `tasklist.go`, `heartbeat.go`) + git blame + web research.
**Scope of data:** the task asked for "last 50 sessions"; I analyzed last-50, last-100, and all-523 real-work sessions to separate recency from history.

---

## TL;DR — The headline claim is NOT reproducible as stated

| Metric | Last 25 | Last 50 | Last 100 | ALL real-work sessions |
|---|---|---|---|---|
| Sessions that **never** issue a task list | **4%** | **4%** | **6%** | **17%** |
| Sessions that issue a proper `[TASK_LIST_BEGIN]...[TASK_LIST_END]` pair | ~92% | ~92% | ~90% | ~79% |
| Sessions with `[BEGIN]` but missing `[END]` (silent-drop risk) | 1 | ~1 | ~3 | 10 (2%) |

**The claimed ">30% of gopi sessions never issue a task list" is NOT reproduced.** Recent sessions issue task lists in ~92% of cases, and all-time the "never issues" rate is 17% — still not 30%. Moreover the trend is **improving** (17% → 6% → 4%), so a 30% figure is stale or measured differently.

The "not issuing" cases do exist and are concentrated in two categories:
1. **Legitimate short/trivial sessions** (2-5 text turns, few tool rounds) where a formal task list would be overkill — and which the R2 relaxation (below) explicitly permits.
2. **A small set of genuinely long sessions** (25-60+ turns) that ran to completion with **no task list at all** — these are the real failures and deserve attention (currently a few percent).

---

## 1. How I measured (reproducible recipe)

Analysis script over session JSONL, filtering to **real work sessions** = those with ≥1 assistant *text* turn AND ≥1 tool call (excludes empty/failed/echo-only sessions that skew a naive "no-list" count up).

- **Presence:** does ANY assistant text turn contain `[TASK_LIST_BEGIN]`?
- **Placement:** which assistant-turn index first contains it?
- **Truncation:** sessions with `[BEGIN]` but no `[END]`.

Key JSONL parsing notes (mirrors memory #1009): `session_start` key is `sessionId`; tool_use names/inputs may be `name`/`Name` and `input`; some "empty-looking" sessions are test/echo runs and must be excluded.

---

## 2. Root-cause investigation

### 2a. The system prompt actively TELLS the model it may omit the list — this is the core design tension

`internal/agent/loop.go` `taskManagementRules` (lines 276-285), rule 2 (verbatim from source):

> **2. Emit [TASK_LIST_BEGIN]...[TASK_LIST_END] JSON ONLY on change (new task/status), or every %d responses — omit otherwise.**

where `%d = maxRoundsWithoutTaskList = 3` (line 270).

**This is the single most important finding.** The instruction that the "system prompt enforces LLM to issue a task list" was **intentionally relaxed** in commit `da28869e` (feat(perf): R2 — task list JSON only on change, not every round, 2026-08-22) to save 150-500 output tokens/round of bookkeeping. Context from the perf-vs-claude-fix-plan and the commit message: the old rule demanded the full task-list JSON in EVERY response; the new rule says **emit only on change, or every 3 responses — omit on ordinary tool rounds.**

**So the model is not "failing" when it withholds the list on a given round — it is following instructions.** Any metric that counts rounds/turns where the list is absent will see a majority omission rate and misattribute it as non-compliance. The "First turn: initial task list" rule (rule 1) is in direct tension with rule 2's "omit otherwise" for the very first round, and rule 2 generally wins in practice.

### 2b. The "first turn" is being honored as "first SUBSTANTIVE turn", not turn 0

The report implies the first response should carry the list ("Remember to end your response with [TASK_LIST_BEGIN]...[TASK_LIST_END]" — loop.go:1412). Measured reality across 432 list-emitting sessions:

| First `[TASK_LIST_BEGIN]` position | Share |
|---|---|
| Assistant turn 0 | **2%** |
| Assistant turn 1 | 10% |
| Assistant turn ≥2 | **88%** |

And separately: **93% of sessions emit the list AFTER at least one tool round** (memory_search / code_search / read first), and 48 sessions open with a tool-only turn. So the model's behavior is: **explore/read first, then commit to a plan**. This is a reasonable LLM pattern (plan after seeing context), and matches the memory-first habit, but it means the "first turn" promise is honored ~never literally. Whether that matters depends on intent: the todo tool / heartbeat only *need* a list once task tracking is meaningful.

### 2c. Conflicting prompt instructions compound the ambiguity

There are **three overlapping instructions** telling the model different things about *when* to emit:
- Rule 1: "First turn: initial task list from the Epic goal" (emit immediately).
- Rule 2 (R2-relaxed): "ONLY on change, or every 3 responses — omit otherwise" (may hold off).
- Epic framing: "Remember to end your response with [TASK_LIST_BEGIN]...[TASK_LIST_END]" (implies end of each response).

The model resolves the conflict by **holding off until it has something to report**, which is the rational reading. This ambiguity is the root cause of *delayed/omitted* issuance — it's **a prompt-design regression (an unintended side effect of the R2 token optimization), not a code bug and not a hard LLM failure.**

### 2d. Secondary real edge cases (from session data)

1. **`[BEGIN]` with no `[END]` → silent drop (memory #559).** `ParseTaskList` (tasklist.go:91-110):
   ```go
   endIdx := strings.LastIndex(response, TaskListEndMarker)
   if endIdx < 0 || endIdx <= beginIdx { return nil, nil }  // silent!
   ```
   Found in **10 sessions (2%)**. When this happens on the FIRST turn, no list is ever established and none of the later nudge/heartbeat logic fetches it — the session runs task-less. This is the **data-loss-grade exit in the loop's unfinished-task protection** (loop.go:608-626) not firing. Real instigator seen in session `6613fe97`: the model emitted a `[TASK_LIST_BEGIN]` whose JSON contained an invalid status **`"pending"`** and ended with a `</invoke>` (it merged tool-call XML and task-list JSON), so END was never found.
2. **Invalid task status tokens.** `ValidateTaskList` only accepts `progressing`/`completed`. Sessions using `"pending"`/`"done"` fail validation; combined with a lost END marker, the list can vanish entirely.
3. **Long sessions that never establish a list.** A few 25-60-turn sessions ran to completion with zero `[TASK_LIST_BEGIN]`. Because the R2 relaxation plus the heartbeat *tolerates* omitted lists indefinitely (only fires a nudge after `staleRounds` of an *existing* unchanged list; it never nudges for "no list yet" once tasks progress), these sessions slip through. `loop.go` only logs "exit with N progressing tasks" when a list EXISTS.

---

## 3. Bug / regression / LLM behavior — classification

| Finding | Classification |
|---|---|
| >30% never issuing a list | **NOT reproduced.** Real rate 4-17% (recency-dependent), improving over time. |
| Model omits the list on most rounds | **Intended behavior**, introduced by regression commit `da28869e` (R2 token optimization) relaxing "every round" → "on change/every 3". |
| First-turn list | **LLM behavior** — model explores first (93% list after ≥1 tool round). See 2b. |
| `[BEGIN]`-no-`[END]` silent drop | **Real bug**, low-frequency (2%), severity P0/P1 (data-loss + defeated exit protection). Pre-existing (documented in #559), independent of R2. |
| Long sessions with no list ever | **Latent design hole** — no harness enforcement pushes a list-only "no list yet triggers a nudge" path; R2 made omission legitimate, so nothing corrects a never-reported plan. |

**Net:** this is primarily a **prompt-design regression (the R2 relaxation), plus a latent low-frequency parsing/data-loss bug (#559)** — NOT a 30% LLM-compliance failure. The observed data does not support the epic's premise.

---

## 4. Web research on how other harnesses handle task planning

Direct search was unproductive (Google reCAPTCHA-blocked, Bing returned generic/unrelated LLM articles, arxiv empty). The most relevant comparison is the repo's own devdoc `perf-vs-claude-fix-plan.md`, which names the same trade-off gopi hit: Claude Code CLI keeps a structured task/plan track and bind it to tool results, avoiding per-round full-JSON re-emission by **maintaining the plan as a harness-owned artifact the model references**, rather than re-emitting bookkeeping JSON each turn.

Best-practice heuristics that emerged (from the docs + general agent-harness literature my searches surfaced and the in-repo R2 analysis):
- **Separate "plan" from "bookkeeping":** keep the canonical task list as harness state; let the LLM issue *deltas* (which task, new status) instead of re-emitting the whole JSON — preserves plan-tracking data the harness can rely on without the 150-500 token/round cost.
- **Structured/typed task data beats adversarial text markers:** a self-correction of failing the plan the harness can parse robustly (survives truncated responses) is more reliable than BEGIN/END markers that silently fail (the #559 class).
- **One deterministic enforcement point:** a single rule for "when the harness must have seen a task list before it lets a long task finish," rather than three overlapping prompt statements that compete (rule 1 vs rule 2 vs epic framing).

---

## 5. Proposal (actionable)

### P0 — Harden the silent-drop path (bug, independent of R2)
1. **Fix `ParseTaskList` to not silently drop BEGIN-only responses** (memory #559 fix 1): if `[TASK_LIST_BEGIN]` found but no later `[TASK_LIST_END]`, parse the JSON up to EOF and return the partial list + a warning, so a truncated/invalid list still establishes task state instead of vanishing.
   - Add: if the JSON tail/before-list contains `"pending"`/`"done"` statuses, normalize or report.
   - Add `go test -run TestParseTaskList` regression tests: (a) BEGIN with no END, (b) valid JSON + invalid status, (c) JSON merged with a `</invoke>` tool tail.
2. **Log a warning when a response contains `[TASK_LIST_BEGIN]` but `tasks == nil`** (loop.go:488 path) so the silent-drop becomes visible at debug time (fix 3).

### P1 — Resolve the conflicting "when to emit" instructions
3. **Reconcile rules 1+2 with the epic framing into one deterministic emission contract.** Propose:
   - Rule 1 stays: "First turn: establish the initial task list from the Epic goal" — but change the epic framing line from "end your response with [TASK_LIST...]" to "establish your task list before your second tool round" (or drop the epic reminder and rely on the kernel prompt), removing the "must appear in turn 0" ambiguity.
   - Keep rule 2's "on change, or every 3 responses" to preserve the R2 token savings — but **add a harness backstop**: `maxRoundsWithoutTaskList` alone is passive; see P2.
4. **Add a "no-list-yet" nudge that fires when tasks are progressing but no list was ever established** — currently the heartbeat only nudges when a list exists and is stale. A long, list-less session should get a reminder after N rounds just like a stalled one.

### P2 — Long-list-less-session backstop
5. **Treat "has never emitted a list before round N" like a stalled heartbeat**: fire a reminder "You have not yet emitted a task list — establish one" after `maxRoundsWithoutTaskList` rounds with no `[TASK_LIST_BEGIN]` ever seen (only when tools are still being called / text still flowing), converting the rare 25-60-turn list-less runs into nudged ones.

### P3 — Telemetry visibility
6. The A6 / trajectory pipeline already exists (memory #9, #1009): add a per-session stat `first_tasklist_round` (largest `-1` if never) to `internal/agent/trajectory.go` so the "never issues" rate and "first-turn vs deferred" behavior become measurable over time instead of requiring an ad-hoc JSONL sweep. This converts the "is it >30%?" question from a manual audit into a tracked metric.

### Acceptance criteria (Rule 11 — LLM-adoption proof not applicable since no new tool is added; but bug fixes must carry tests + real-session evidence):
- `go build ./...` clean; every bug fix ships a `-run TestPath` regression test.
- After P0: re-run the session sweep — BEGIN-without-END count drops to ~0 for newly run sessions.
- After P1/P2: run 2+ real sessions; confirm the first-task-list round is consistently ≤2 and list-less long sessions get a nudge (JSONL evidence).

---

## 6. Not Done / Deferred
- Root-causing the *specific* 30%-claim methodology (no report text / metric definition provided — could not reproduce the exact numerator/denominator). Recommend the reporter share their counting method; the recency-corrected "never issues" rate they'd see is ~4% (last-50).
- Web research had to rely on in-repo comparison (Claude Code devdoc) because general web sources were junk/blocked.
- No code changes shipped in this audit (report-only); the proposal's fixes P0/P1/P2/P3 are ready to implement.

## 7. Verification
- Session sweep: Python JSONL analysis across 595 relevant sessions / 523 real-work sessions (scripts in `/tmp/analyze*.py`; reproducible).
- Source: `internal/agent/loop.go` `taskManagementRules`, `maxRoundsWithoutTaskList=3`, `tasklist.go` `ParseTaskList`, `heartbeat.go` — read in full; git blame on lines 263-285 confirms `da28869e` introduced the R2 relaxation.
- Commit: this report on branch `feat/tasklist-audit` in worktree `~/workspace/gopi-tasklist-audit`. No production code changed.

---

## Deliverable declaration (Rule 6d)
- **Branch:** `feat/tasklist-audit`
- **Recovery:** `git show feat/tasklist-audit:devdoc/tasklist-audit-report.md`
- **Confirmation:** committed on the worktree branch in the main repo (`~/work/GoPi`); worktree shares the object store, branch already visible on `master`'s repo.
