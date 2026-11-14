<!-- doc-class: historical -->
# Task Report: P0 ParseTaskList Silent-Drop Repair + P1.3 Rules Unification

**Date:** 2026-08-29
**Branch:** `feat/tasklist-p0-impl` (worktree `~/workspace/tasklist-p0-impl`, shared with `~/work/GoPi`) — commit `8937246`
**Prior art:** devdoc/tasklist-p0-evaluation.md (Q1-Q4 data-driven verdict), memory #1054/#1050/#1053, devdoc/tasklist-audit-*.md

---

## Design: warning mechanism (P0 requirement #3 — explained)

The brief asked me to choose the warning mechanism (error variant / separate return value / direct log) and justify it.

**Chosen: typed sentinel errors + a thin `parseTaskList(content) ([]TaskItem, string)` wrapper.** `ParseTaskList` keeps its existing `([]TaskItem, error)` signature; the recovery path returns two new sentinel errors that the caller matches with `errors.Is`:

- `ErrTaskListRecovered` — BEGIN-without-END but balanced-prefix JSON parse succeeded; **tasks is non-nil and valid**, err is this sentinel.
- `ErrTaskListUnparseable` — BEGIN-without-END with no parseable JSON prefix (prose ref / mid-JSON truncation / bare XML); **tasks is nil**, err is this sentinel.

**Why sentinel errors rather than a separate return value:** it keeps the two-line `tasks, err := ParseTaskList(...)` contract identical, so the existing `if tasks != nil` downstream logic (TaskState.update, R3 fast-exit, exit protection) is **completely untouched** — a recovered list carries non-nil tasks and flows through every normal path exactly like a closed-block list. It is **not a second-class citizen** (P0 requirement #1 "恢复的清单必须与正常清单走完全相同的下游路径"). The sentinel is purely an observability signal the wrapper turns into a log line.

**Why a wrapper rather than logging inside ParseTaskList:** the parser should stay pure/unit-testable (no Logger dependency); the Agent owns session context (`a.SessionLog.SessionID()`). `parseTaskList` returns `(tasks, warnString)` and the call site logs the warning once per round. This also centralizes the `tasks, _ :=`-silent-drop elimination at all three call sites.

**Why not a direct log in the loop:** the loop has three call sites that would each need the `errors.Is` switch — the wrapper DRYs it into one place and guarantees identical session-label formatting.

**Recovery algorithm (P0 requirement #1):** `parseTaskListBalancedPrefix(tail)` — after `[TASK_LIST_BEGIN]`, skip whitespace, require the first non-space char to be `[` or `{`, then walk forward tracking bracket depth while skipping over string literals (so `[`/`{`/`]`/`}` inside quoted summaries don't unbalance the scan). When depth returns to 0, `json.Unmarshal` exactly that prefix. This strips trailing prose/XML (the `</parameter>` pastiche) that a blind BEGIN-to-EOF parse cannot — matching the eval's Q3 finding (balanced-prefix recovers 23/23, blind to-EOF only 18/23). Reaching EOF with depth>0 → truncation → Unparseable. Non-JSON first char or closed-but-invalid-prefix → Unparseable. **No garbage list is ever fabricated.**

---

## P0 changes (tasklist.go)

1. **Added two sentinel errors** (`ErrTaskListRecovered`, `ErrTaskListUnparseable`) near the marker constants, with doc comments. Added `errors` import.
2. **Rewrote the BEGIN-without-END branch** of `ParseTaskList`: previously `endIdx<0 || endIdx<=beginIdx` returned `nil, nil` (the silent drop). Now it delegates to `parseTaskListBalancedPrefix(tail)`.
3. **Added `parseTaskListBalancedPrefix`** — the string-aware balanced-prefix scanner (described above). Complete-JSON-no-END → `(tasks, ErrTaskListRecovered)`; untruncatable/preclose → `(nil, ErrTaskListUnparseable)`.
4. **Preserved all existing semantics**: no markers → `(nil, nil)`; closed-block malformed JSON → `fmt.Errorf("JSON parse error")`; END-before-BEGIN → recovery path; multiple blocks → LastIndex as before; empty block `[]` with END → `(nil, nil)` (empty list, unchanged).

## P0 changes (loop.go — three call sites)

Previously all three were `tasks, _ := ParseTaskList(content)` (silent drop). Now:
1. **Parse once per round**: `tasks, warn := a.parseTaskList(content)` at the top of the exit-check section; `if warn != "" { a.Logger.Printf("[gopi] %s", warn) }`.
2. The **exit-check**, **TaskState.update**, and **R3 all-completed** branches now use this same `tasks` (reusing the single parse, no re-parse).
3. Added two methods: `parseTaskList(content) ([]TaskItem, string)` (the wrapper that maps sentinel→warning) and `sessionLabel() string` (returns `a.SessionLog.SessionID()` for session-identifiable WARN lines, `"?"` when no session log).

The WARN line format: `[gopi] ⚠️ recovered task list from BEGIN-without-END (session <uuid>): N task(s); LLM likely forgot [TASK_LIST_END]` and `[gopi] ⚠️ task list dropped — BEGIN-without-END with no parseable JSON (session <uuid>); no garbage list produced`.

## P1.3 changes (loop.go — rules + epic message)

**Unified the three contradictory wordings into one contract** (density kept under the 4500-char system-prompt gate — see verification):

- **Rule 1** (was "First turn: initial task list from the Epic goal") → **"Initial list = PLAN (not receipt): build on the first substantive round (Epic ≠ task)."** resolves the rule1/rule2 contradiction (plan is established on the first *substantive* round, not literally turn 1).
- **Rule 2** retains the R2 omission rule ("ONLY on change or every %d responses (omit otherwise)") AND gains the anti-recurrence anchor: **"ALWAYS close with [TASK_LIST_END] (auto-recovers+warns)"** — telling the model the harness recovers a forgotten END but warns (the P0 backstop is framed as a warning, not a crutch).
- **Rules 3-7 preserved** (NEVER delete; decompose; keep working; DONE→memory_store; todo-not-narrate), lightly tightened to net-fit the density gate.
- **Epic message** (was "Remember to end your response with [TASK_LIST_BEGIN]...[TASK_LIST_END]") → **"establish an initial task list as a PLAN (first substantive round): emit it as [TASK_LIST_BEGIN]...[TASK_LIST_END], and re-emit only when it changes."** — now consistent with the rules (no "end your response with a list" mandate that contradicted R2's omit-otherwise).

**Density note:** the system-prompt gate is <4500 chars; baseline was 4479 (21 headroom). The contract additions were folded in by tightening rule wording (not by relaxing the gate — `TestSystemPromptDensity` still enforces <4500 and passes at 4499).

---

## Regression tests (tasklist_test.go)

A/B/C classes + boundaries, using **real session fixtures**:

| Class | Test | Fixture | Expectation |
|---|---|---|---|
| A | `TestParseTaskList_ClassA_Recover_RealFixture68` | `68827372-…` line 281 (7-task list, complete JSON, no END) | `(7 tasks, ErrTaskListRecovered)`, task[6]=completed |
| A (XML tail) | `TestParseTaskList_ClassA_Recover_StripsTrailingXML` | `6613fe97-…` line 591 (11-task list + `</parameter></invoke>` tail) | `(11 tasks, ErrTaskListRecovered)`, trailing XML stripped |
| B | `TestParseTaskList_ClassB_TruncatedJSON_NoPanic` | synthetic BEGIN + unclosed JSON array | `(nil, ErrTaskListUnparseable)`, no panic |
| C | `TestParseTaskList_ClassC_ProseReference_ZeroGarbage` | `0a2a237c-…` line 103 (prose quoting the marker) | `(nil, ErrTaskListUnparseable)`, zero garbage |
| boundary | `TestParseTaskList_Recovered_FeedsTaskState_ActivatesExitProtection` | recovered 7-list → `newTodoState().update()` | `hasList==true`, `len==7`, `AllCompleted==true` |
| boundary | `TestParseTaskList_Boundary_EndBeforeBegin` | END before BEGIN, no JSON | `(nil, ErrTaskListUnparseable)` |
| boundary | `TestParseTaskList_Boundary_NormalClosedPair` | normal BEGIN+END | `(1 task, nil)` — strict path, no warning |
| existing→updated | `TestParseTaskList_MissingEndMarker_PartialRecovery` | (was `MissingEndMarker`, asserted buggy nil) | now asserts recovery |
| existing→updated | `TestParseTaskList_OnlyBeginMarker` | `[TASK_LIST_BEGIN]` alone | now asserts `(nil, ErrTaskListUnparseable)` |

The two updated tests previously asserted the BUGGY behavior (silent nil); they now assert the correct recovery/drop semantics.

---

## Verification

- **Build:** `go build ./...` — OK
- **Vet:** `go vet ./...` — clean (0 findings)
- **Binary:** `CGO_ENABLED=0 go build -o bin/gopi-test ./cmd/agent` — OK (in worktree `~/workspace/tasklist-p0-impl/bin/gopi-test`; the main repo's `bin/gopi` is untouched)
- **Density gate:** `TestSystemPromptDensity` PASS (system prompt 4499 chars < 4500) — the P1.3 contract additions fit without exceeding the gate.
- **Tests:** `go test ./...` (full, on final source) — **all 9 packages PASS, exit 0**:

```
ok  	github.com/nick/gopi/cmd/agent	12.294s
ok  	github.com/nick/gopi/internal/agent	36.120s
ok  	github.com/nick/gopi/internal/checkpoint	0.015s
ok  	github.com/nick/gopi/internal/config	0.007s
ok  	github.com/nick/gopi/internal/deepseek	0.016s
ok  	github.com/nick/gopi/internal/gopilog	0.429s
ok  	github.com/nick/gopi/internal/proxy	0.018s
ok  	github.com/nick/gopi/internal/text2img	0.503s
ok  	github.com/nick/gopi/internal/tools	104.284s
```

**No FAIL, no flakes reproduced.** The pre-existing `TestMemorySearchNonBlocking` SQLITE_BUSY flake did NOT occur on this clean serialized run (note: an earlier concurrent run, `go test ./...` while another agent was running the identical `internal/tools` suite against the same SQLite DB, took 183s but still passed exit 0 — the contention slowed it but did not fail).

## Not Done (honest scope)

- **B-class recovery on live data** not re-measured: B-class (mid-JSON truncation) is zero in the full corpus (eval Q1), so its balanced-prefix recovery is exercised only by the synthetic B-fixture unit test. No live B data exists to test against.
- **P1.5 (decouple exit from final-round emission)** deliberately NOT implemented — the eval (Q4) recommends holding it until P0 is verified, and this epic's scope is P0 + P1.3 only.
- **Did not change the density-test threshold or the 4500 gate** — the contract additions were fit by tightening wording, and `TestSystemPromptDensity` still enforces <4500.
- **No change to master, no overwrite of main-repo `bin/gopi`, no force-push.**

## E2E smoke test (new binary `./bin/gopi-test`)

Command: `echo "explain semaphore in one paragraph" | ./bin/gopi-test`

Result: **exit 0, 4 iterations, 13s.** Task list parsed normally on both rounds — the model emitted an initial `progressing` list AND a final `completed` list, each with full `[TASK_LIST_END]` closure (the normal closed-block path, not recovery — correct happy-path behavior). 3 tool calls (memory_feedback 2, memory_search 1). Session JSONL written to `~/.gopi/sessions/03b02c6b-….jsonl`: 768 lines with `session_start`(1) + `user`(1) + `assistant`(371) + `request`(195) + `tool_result`(200) events all correctly recorded. The task-list parse + session writeback path is confirmed end-to-end.

Verified task list framing in the E2E output:
```
[TASK_LIST_BEGIN]
[{"task_id":"1","summary":"Explain semaphore in one paragraph","status":"progressing"}]
[TASK_LIST_END]
```
and the completion:
```
[TASK_LIST_BEGIN]
[{"task_id":"1","summary":"Explain semaphore in one paragraph","status":"completed"}]
[TASK_LIST_END]
```

---

## Deliverable declaration (GOPI.md Rule 6d)

- **Branch:** `feat/tasklist-p0-impl` — commit `502a6f2` (in worktree `~/workspace/tasklist-p0-impl`, shared with the main repo `~/work/GoPi`).
- **Recovery command:** `git show feat/tasklist-p0-impl:devdoc/tasklist-p0-impl-report.md` (or `git checkout feat/tasklist-p0-impl`).
- **Confirmed in the MAIN repo:** yes — `cd ~/work/GoPi && git log --oneline -1 feat/tasklist-p0-impl` → `502a6f2`. The worktree shares the main repo's refs, so the commit is already on the branch in `~/work/GoPi`; **no push needed**.
- **master untouched:** `3baae43`; main-repo `bin/gopi` not overwritten (deliverable binary is `bin/gopi-test` inside the worktree only).
- **Scope honored:** no force-push, no merge performed (left to the dispatcher per Rule 6e/9).
