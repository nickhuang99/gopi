<!-- doc-class: historical -->
# Task Report: memory-aware R7 — test adequacy (behavior coverage matrix + gaps)

Child task R7 of the memory-aware review epic (commit 675b507, master ancestry).
Scope: test files `internal/tools/writetaskarea_test.go` (4 tests) and
`internal/agent/writetaskarea_test.go` (3 tests) + any other test exercising
write_task_area / SessionConclusions / session_conclusions / memLayout.
Findings P0/P1/P2/N. Zero source changes — review only.

## Coverage matrix

For each behavior: TESTED (test + what it proves) / WEAKLY-TESTED / UNTESTED (gap).

### 1. Validation rejections (both layers; multi-error precedence)

| Sub-behavior | Verdict | Evidence |
|---|---|---|
| Tools layer: empty session_id / task_id / status≠completed / empty conclusion | **TESTED** | `TestWriteTaskArea_RequiresConclusionAndCompleted` (4 subtests) drives `tools.WriteTaskArea` with each single bad field, asserts err contains the field name. Tools-layer order session→task→status→conclusion is the code's check order (writetaskarea.go:86-103). |
| Agent pre-dispatch layer: `validateToolArgs` for write_task_area (task_id/status/conclusion non-empty, before dispatch) | **UNTESTED** | `TestExecuteTool_MissingRequiredArgs` (loop_test.go:280) lists many tools but NOT write_task_area (verified: zero `write_task_area` occurrences in loop_test.go / any agent test other than writetaskarea_test.go). |
| Dispatch case `executeToolDefault` → WriteTaskArea → latch on success / error string on failure | **UNTESTED** | No test drives the `case "write_task_area"` dispatch (~4200). R2 finding #6 stands unguarded. |
| Multi-error precedence (e.g. empty session_id AND empty conclusion → which reported first) | **UNTESTED** (N) | All 4 tools-layer subtests inject exactly one bad field. Code order is deterministic (session first); a combined test would pin it but risk is trivial. |
| Whitespace-only conclusion / task_id (TrimSpace vs raw) | **WEAKLY** | Tools layer trims (r1's P2: insert stores TrimSpace(SessionID) at line 111 but `sessionConclusionCount` (121) + read query (178) use raw → padded id writes a row its own count/read can't see). No test uses whitespace-padded ids; benign only because the framework UUID is clean. |

### 2. Insert + read-back; append-only; ordering; session scoping

| Sub-behavior | Verdict | Evidence |
|---|---|---|
| Insert + read-back roundtrip (conclusion + optional analysis persisted) | **TESTED** | `TestWriteTaskArea_RecordsAppendOnlyScoped`: 2 commits sessA, read-back asserts TaskID, Conclusion content, and `Analysis == "compressible reasoning"`. |
| Append-only, distinct items | **TESTED** | Same test: sessA has task "1" + "2" → 2 rows, both returned. |
| Append-only, SAME item committed twice (task "1" twice → 2 rows, no overwrite) | **UNTESTED** | No test re-commits the same task_id; the no-overwrite property is only implied by distinct-task writes. |
| Ordering by created_at,id | **TESTED** (creation order) | Read-back asserts got[0].TaskID=="1", got[1]=="2" in write order. Weaker than a same-timestamp tie-break test (created_at is float64 UnixNano/1e9, ties practically impossible) — adequate. |
| Session scoping (A's rows invisible to B; never-wrote → 0) | **TESTED** | Same test: sessB sees only its own row; `SessionConclusions("never-wrote")` → 0. |

### 3. Schema / heal paths

| Sub-behavior | Verdict | Evidence |
|---|---|---|
| Table+index creation on a FRESH DB (ensureSessionConclusionsSchema on first write) | **TESTED** (implicit) | Every tools-layer write runs against a fresh `t.TempDir()` DB and succeeds — table+index must have been created (writetaskarea.go:105). |
| Schema idempotence (double call) | **TESTED** (implicit) | `RecordsAppendOnlyScoped` runs `ensureSessionConclusionsSchema` on every write (3× same DB) with no error. |
| SessionConclusions on a gopi-owned DB WITHOUT the table → nil,nil | **TESTED** | `TestSessionConclusions_MissingDBReturnsEmpty`: env points at a nonexistent file → `openTasksDB` → `ensureKnowledgeBase` builds a fresh gopi DB (distilled_qa + memory_feedback + tasks, NO session_conclusions) → tableExists=false → nil,nil, no error. This is exactly the "pre-feature DB read" path. |
| Pre-feature gopi DB gains the table on first WriteTaskArea (heal-on-write) | **TESTED** (implicit) | Every fresh temp DB starts without the table; the first write creates it and succeeds. (R1's comment-drift caveat — the table does NOT ride ensureKnowledgeBase/migrateDefaultTriggers — is code-claim, not test, territory; the actual lazy-create is what is exercised.) |
| Foreign DB (has no distilled_qa): WriteTaskArea errors, SessionConclusions → nil,nil without error, DB never mutated | **UNTESTED** | No test pre-creates a foreign SQLite file (arbitrary table) and asserts WriteTaskArea's "not a distilled knowledge base" error / SessionConclusions' silent nil. tasks_repo_guard_test.go covers the ledger's foreign rejection, not this table. |

### 4. taskAreaDirty / taskAreaActive latch mechanics

| Sub-behavior | Verdict | Evidence |
|---|---|---|
| Latch set on success only (failed write does NOT latch) | **UNTESTED** | Dispatch err-path (4208-4217 returns before 4221-4222) never driven by a test. |
| Latch cleared by the runEngine hook | **UNTESTED** | Hook region (2927-2931) never exercised — tests call the private methods directly, bypassing runEngine (which needs a live model). |
| Hook fires exactly once per dirty round | **UNTESTED** | — |
| INJECT-THEN-TRIM order in the same round (carry block present BEFORE trim) | **UNTESTED** | H4 red-line order exists only structurally inside runEngine (2929 then 2930). `TestInjectTaskAreaConclusions` calls inject alone; `TestTrim…` calls trim alone. No test asserts the carry block survives a same-round trim. |
| Failed write_task_area does not leave dirty set | **UNTESTED** | Covered only if dispatch were driven (see above). |

**This is the feature's biggest test hole**: the entire latch→hook→inject→trim
wiring — the POC H3/H4 mechanism that IS the feature — has zero end-to-end
test. All 3 agent tests poke the two private leaf methods with the latches
hand-set (`ag.taskAreaDirty = true` at writetaskarea_test.go:136).

### 5. injectTaskAreaConclusions

| Sub-behavior | Verdict | Evidence |
|---|---|---|
| Injects ONE user message with marker + all recorded facts when conclusions exist | **TESTED** | `TestInjectTaskAreaConclusions_CarriesRecordedFacts`: 2 real DB commits, dirty=true, asserts last message is user, contains `taskAreaConclusionsMessageMarker` + both facts. |
| NO-OP when no conclusions (no stray marker / no message appended) | **UNTESTED** | Early return at 4626 (`len(concs)==0`) unguarded. |
| Error path (DB down) → silent no-op, no panic | **UNTESTED** | SessionConclusions swallows open/table errors → nil,nil (R1 finding #3), inject returns. Not exercised. |
| Empty session guard (sessionIDForTasks == "") | **UNTESTED** | — |
| Inject fires once per round even after multiple commits (read-all covers both) | **UNTESTED** | R3 proved the read-all semantics in code; no test does 2 commits + 1 inject and asserts BOTH facts in ONE block — the current inject test does exactly that (2 commits → 1 block, both facts) so this is actually **TESTED** at method level (only the hook wiring to fire it is untested). |

### 6. trimCommittedTaskArea

| Sub-behavior | Verdict | Evidence |
|---|---|---|
| [1,ci) drop keeps epic (idx0) + commit round + live tail | **TESTED** | `TestTrimCommittedTaskArea_DropsAnalysisKeepsEpicAndCommit` (epic + commit round survive, analysis gone) + `TestTrimCommittedTaskArea_KeepsLaterLiveWork` (item-2 analysis + result survive). |
| Orphan-safety (no tool_result at head after trim) | **TESTED** (simple layout) | `KeepsLaterLiveWork` asserts `Messages[1].Role != "tool"`. The general structural proof (results always trail their owner assistant) is R3's code verdict; the test only covers the single-analysis-round shape. |
| Early returns: Messages<3, ci<=1 (commit adjacent to epic) | **UNTESTED** | Both guards at 4657/4690 unexercised (empty ctx, commit right after epic). |
| Commit round carrying OTHER tools in the same assistant message (their results at >ci stay; nothing of theirs dropped) | **UNTESTED** | Fixture commit round has ONLY the write_task_area call; the multi-tool-commit-round shape R3 proved is unguarded. |
| Multi-round chain: commit item1 → round trims → commit item2 keeps item1's carry block | **UNTESTED** | Only a single trim round is ever run. |

### 7. memLayout prompt regression guard

| Sub-behavior | Verdict | Evidence |
|---|---|---|
| Density gate passes WITH the memLayout text | **TESTED** | `TestSystemPromptDensity` (prompt_test.go:66) — currently green at 4497 chars with memLayoutHint (231 chars) in the assembled prompt. |
| memLayout section PRESENCE asserted (someone deletes memLayoutHint / the Order-62 registry entry → test fails) | **UNTESTED** | **Confirmed by R4 (8456ae7, P2-1):** `TestSystemPromptDensity` asserts TOOL ROUTING / EFFICIENCY / TASK MANAGEMENT / BASH / JOB / THINKING markers + memory/clock lines but NOT `WORKING MEMORY LAYOUT` / `memLayout` / `write_task_area` / `do NOT re-derive`. Deleting the entry SHORTENS the prompt → density gate passes MORE easily → nothing fails. Regression hole (R4 P2-1, not yet fixed since R4 is review-only). Overlap with R4's territory — noted, not re-scored. |

### 8. Absent tests (briefing-listed)

| Scenario | Verdict |
|---|---|
| 2 concurrent sessions same DB | **UNTESTED** — no goroutine test. (Schema/table race handled by WAL + waitForDistilledSchema elsewhere.) |
| Cross-run resume re-injection (`taskAreaCarryForward`) | **UNTESTED** — and `taskAreaCarryForward` (4706) has ZERO callers (R3 P2: unwired). resume_test.go has no taskArea mention. |
| write_task_area commit in the FINAL round before max-iterations exit (dirty latch never cleared — leak?) | **UNTESTED** — runEngine's bail path (~2935+) never exercises the hook. |
| Multiple write_task_area calls in ONE round | **UNTESTED** — bool latch + read-all inject covers both per R3, but no test issues 2 calls in one round. |

## Test isolation audit

**PASS.** No shared-DB flakiness, no ~/.gopi reliance:
- All 4 tools tests `t.Setenv(memoryDBEnv → t.TempDir()/test-gopi.db)`; `DoesNotTouchRealDB` explicitly asserts the env changed from the real path before writing.
- Agent inject test uses `tempAgentMemoryDB` (GOPI_MEMORY_DB → t.TempDir); the tools package reads the same env var at call time (same test process).
- The 2 trim tests touch no DB at all (pure message-array) and correctly set no env.
- No `t.Parallel()` — sequential within each package binary; tools and agent compile to separate test binaries. No cross-test DB contamination.

## Tests testing implementation rather than behavior

None cross the line. All asserts are behavioral (epic survives / analysis gone /
carry block is a user message with the marker + facts). Caveats worth stating:
- All 3 agent tests call the **private leaf methods directly** with hand-built
  message arrays / hand-set latches — white-box unit tests of a private
  surface (acceptable since runEngine needs a live model), which is exactly
  why the latch/hook wiring (item 4) has no coverage: nothing is wrong with
  these tests, they simply cannot see the wiring.
- `addAnalysisWorkloads` fabricates a contiguous assistant→result sequence per
  round, which mirrors reality well enough for the leaf methods but not for
  hook-order or multi-round claims.

## Gap list + suggested missing test names (all P2 unless noted)

1. **P2 (highest)** Latch→hook wiring + inject-then-trim order: `TestRunEngineHook_TaskAreaDirtyInjectThenTrimSameRound` — drive the hook with a stubbed `executeToolFn`/model fn: successful write_task_area → dirty cleared → carry block present AND trim ran (analysis gone) after the same round; failed write → neither. Second variant asserts hook does NOT fire when dirty=false. Covers item 4 wholesale.
2. **P2** Dispatch validation: add write_task_area cases to `TestExecuteTool_MissingRequiredArgs` (task_id/status/conclusion missing → "Missing required parameter"); plus `TestExecuteTool_WriteTaskAreaDispatchSuccessLatches` — real dispatch success sets both latches, err path leaves them false.
3. **P2** memLayout presence (R4 P2-1): add `"WORKING MEMORY LAYOUT"` (+ one content marker like `do NOT re-derive`) to `TestSystemPromptDensity`'s marker loop (prompt_test.go:70).
4. **P2** Trim edge shapes: `TestTrimCommittedTaskArea_EarlyReturns` (empty Ctx, Messages<3, ci<=1); `TestTrimCommittedTaskArea_MultiToolCommitRound` (commit assistant also calls read → its result survives); `TestTrimCommittedTaskArea_TwoRoundChain` (item1 committed+trimmed, item2 commit keeps item1's carry block).
5. **P2** Inject no-op paths: `TestInjectTaskAreaConclusions_NoOpWhenNone` (no dirty commit → no message/marker appended); `TestInjectTaskAreaConclusions_EmptySessionNoOp`.
6. **P2** Foreign DB / heal: `TestWriteTaskArea_RejectsForeignDB` (pre-create sqlite file with an arbitrary table → WriteTaskArea errors "not a distilled knowledge base"; SessionConclusions → nil,nil; file unmutated — assert the arbitrary table still exists and session_conclusions does NOT).
7. **P2** Same-item append-only: `TestWriteTaskArea_SameItemTwiceIsAppendOnly` (task "1" committed twice → 2 rows, both conclusions readable).
8. **N** Multi-error precedence: `TestWriteTaskArea_MultiErrorSessionFirst` (empty session + empty conclusion → err names session_id).
9. **N** Whitespace-padded SessionID trim-mismatch (R1 P2): `TestWriteTaskArea_WhitespaceSessionID` — write with `" s1 "`, assert read-back via raw `"s1"` and receipt count agree (currently they would NOT — pins the R1 trim bug as a known-behavior test or a fix).

Later-commit note: `git log` shows 675b507 is the ONLY commit ever touching both
test files — no regression coverage has been added since the feature landed.

## Verification
- Build: not applicable (no source change).
- Tests run: `go test ./internal/tools/ -run 'WriteTaskArea|SessionConclusion' -v` → 4/4 PASS (1.65s). `go test ./internal/agent/ -run 'TaskArea|WriteTaskArea' -v` → 3/3 PASS (0.96s).
- Full `go test ./internal/tools/ ./internal/agent/` could NOT be completed in this session: the suites stalled at 0:00 CPU for 8+ min, traced to sibling gopi processes running identical `go test ./internal/tools/` concurrently in the main repo (CPU contention, not a code issue). Compile of both packages verified (`go test -run ZZZNoSuchTest` → `ok` both). Isolation/regression claim rests on: (a) the 3 test files touch only t.TempDir DBs, (b) targeted suites green, (c) git log shows 675b507 is the only commit ever touching the two reviewed test files.
- Commit: cefd3b5 docs(review): R7 test-adequacy review ... (branch feat/review-memory-aware-tests)
- Report: devdoc/review-memory-aware-tests.md on branch feat/review-memory-aware-tests.
