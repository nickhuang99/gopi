<!-- doc-class: historical -->
# Epic Plan: Full Code Review of the memory-aware Feature (write_task_area + session working area + memLayout)

Feature commit: **675b507** `feat(write-task-area): session-internal subtask commit tool + working-memory soft hint` (in master ancestry; master tip f74aa7f).
Related commits: acbf958 (POC results doc), 1d31cf0 + 876090d (independent verification), f03d9fd/67d56d3/1c407f4 (A/B adoption data).
POC memory: devdoc/memory-aware-agent-poc-20260904.md · devdoc/write-task-area-implementation-20260904.md · devdoc/verify-write-task-area-20260904.md

This epic is **planning + task-issuance only**. Child review tasks are issued via gopi task_submit and execute in later runs (divide-and-conquer). Every child writes a detailed report to `devdoc/review-memory-aware-<area>.md`, shares findings via memory_store, and NEVER submits further child tasks (anti-recursion guard).

## What the feature is

An in-session "subtask working area" — the memory-aware analogue of the cross-session task ledger:

- **Tool**: `write_task_area` (session-internal commit: task_id, status="completed" only, conclusion REQUIRED, compressed_analysis optional). One item per call; commit on convergence. Validation double-layered (validateToolArgs in loop.go + WriteTaskArea in tools).
- **Persistence**: append-only `session_conclusions` table in the shared gopi memory DB (schema heal via ensureSessionConclusionsSchema; the file header claims it rides the ensureKnowledgeBase/migrateDefaultTriggers heal path — VERIFY this claim), scoped by session_id.
- **Framework signals** (runEngine hook, loop.go ~2919): after a commit round, (1) inject the FULL recorded conclusion set as ONE authoritative user message (`[WRITE_TASK_AREA — COMMITTED CONCLUSIONS (authoritative)]`), (2) trim the committed working region (messages [1, commit-round)) keeping epic + commit round + live tail, orphan-safe.
- **System prompt**: `[WORKING MEMORY LAYOUT — soft]` section (memLayoutHint, promptSections Order 62) telling the model analysis ≠ conclusion, commit and don't re-derive.
- **Not wired** (per commit msg): cross-run resume re-injection — `taskAreaCarryForward` helper exists "ready" but nothing calls it. injectTaskAreaConclusions' comment claims "resume/reload paths can re-inject it" — VERIFY whether that's drift.

## Review surface (files & docs)

### Core new files (feature 675b507)
| # | File | Notes |
|---|------|-------|
| F1 | internal/tools/writetaskarea.go (192) | WriteTaskArea validation+insert, SessionConclusions read, schema table+index, taskAreaDBTimeout=5s |
| F2 | internal/tools/writetaskarea_test.go (108) | tool-side tests |
| F3 | internal/agent/writetaskarea_test.go (152) | agent-side tests |

### Core changed file
| # | File | Feature anchors (loop.go, 4723 total) |
|---|------|-------|
| F4 | internal/agent/loop.go | memLayoutHint ~646; promptSections Order-62 entry ~699; write_task_area toolDef ~1177; Agent fields taskAreaActive/taskAreaDirty ~1335/1342; runEngine hook ~2919-2931; executeToolDefault dispatch ~4200-4223; validateToolArgs ~4392-4399; marker const ~4611; injectTaskAreaConclusions 4621-4642; trimCommittedTaskArea 4656-4698; taskAreaCarryForward 4706-4722; sessionIDForTasks 4304 |

### Supporting (pre-existing, now integrated — review the integration)
| # | File | Why in scope |
|---|------|-------------|
| F5 | internal/tools/memory.go | openTasksDB(882 in tasks.go), memoryDBPath(353), tableExists(543), ensureKnowledgeBase(876), migrateDefaultTriggers(730) — the "same heal path / foreign DB untouched" claim |
| F6 | internal/tools/tasks.go | openTasksDB 882; ledger conventions write_task_area mirrors |
| F7 | internal/agent/prompt_test.go | TestSystemPromptDensity 4500-char gate + section-marker asserts (is memLayout asserted?) |
| F8 | internal/agent/session.go, resume.go | session UUID source (sessionIDForTasks), resume/reload paths (does anything re-inject?) |

### Docs (devdoc)
| # | Doc | Lines |
|---|-----|-------|
| D1 | devdoc/memory-aware-agent-poc-20260904.md | 49 (POC: 9/9 vs 0/3 decisive, memLayout soft hint, H3/H4) |
| D2 | devdoc/write-task-area-implementation-20260904.md | 125 (implementation report) |
| D3 | devdoc/verify-write-task-area-20260904.md | 112 (E1-E5 request-level evidence) |

## Child task inventory (all issued via task_submit)

| Task | Area | Scope | Report file |
|------|------|-------|-------------|
| R1 | tools/writetaskarea.go + F2 tests | F1+F2 line-by-line: validation, insert, read side, schema, error swallowing | devdoc/review-memory-aware-tool.md |
| R2 | agent wiring + tests | F3 + loop.go dispatch/fields/validateToolArgs/sessionIDForTasks | devdoc/review-memory-aware-agent-wiring.md |
| R3 | message-array machinery | injectTaskAreaConclusions + trimCommittedTaskArea + carryForward + runEngine hook — orphan safety, H3/H4, duplication growth, context_trim interplay | devdoc/review-memory-aware-machinery.md |
| R4 | system-prompt memLayout | memLayoutHint, Order 62, assembly, density gate, marker tests | devdoc/review-memory-aware-prompt.md |
| R5 | DB heal-path integration | F5+F6 claim check, foreign-DB non-mutation, row lifecycle, unbounded growth, ordering/append-only | devdoc/review-memory-aware-db.md |
| R6 | resume / cross-run carry | taskAreaCarryForward not-wired drift vs comments; session.go/resume.go | devdoc/review-memory-aware-resume.md |
| R7 | tests adequacy | coverage vs behavior matrix; missing edge tests | devdoc/review-memory-aware-tests.md |
| R8 | docs truthfulness + Rule-11 adoption | D1-D3 + A/B docs vs code reality; is the tool genuinely LLM-used (session jsonl)? | devdoc/review-memory-aware-docs.md |

## Cross-cutting review lenses (apply in EVERY child)

1. **Rule 1/2 (GOPI.md)**: named constants for timeouts/limits; single source of truth for deadlines/env overrides.
2. **Comment↔code drift**: doc claims vs reality (the file-header heal-path claim, the resume re-inject claim, "append-only", "orphan-safe by design"). Each claimed invariant must be traced to a code path or test.
3. **Error-swallowing audit**: every `return nil, nil` / `seq = -1` / ignored error — justified or hiding a bug?
4. **Context-cost growth**: carried-block re-statement is O(n) per commit round; count total bytes over a 20-commit chain. Trims offset it? Bound it?
5. **State consistency**: taskAreaDirty/Active latches — set only on success? cleared? single-threaded claim?
6. **Orphan safety / pairing discipline**: message-array surgery must never orphan a tool_result; interaction with other trimmers (context_trim, TrimToFit, RemoveToolPair).
7. **Persistence semantics**: append-only honored (no UPDATE path)? created_at REAL ordering ties (ORDER BY created_at,id)? CHECK(status) vs hardcoded insert?
8. **Line-by-line evidence**: every finding cites file:line; use git_blame to see 675b507 authorship + rationale in commit message; check later commits touching the same lines.
9. **Severity rubric**: P0 = context corruption / data loss / crash / session divergence; P1 = wrong behavior on a realistic path or violates a doc'd invariant; P2 = robustness/edge/hygiene; N = design note.

## Child-task execution contract (each briefing repeats this)

- Work in the SHARED repo /home/user/work/GoPi via your OWN worktree: `git worktree add -b feat/review-memory-aware-<area> ~/workspace/gopi-review-memaware-<area>` (FRESH path — never reuse an existing worktree dir; stale-ref trap). All read/edit/bash paths inside that worktree. NEVER touch master or another branch.
- Method per file: read (line-by-line) → go build → targeted `go test` (package-scoped; full `go test ./...` dies at the 120s net → job_start to /tmp log) → git log/git_blame on the feature lines → compare against doc'd claims → findings.
- memory_search FIRST for past review experience on this feature; vote memory_feedback on hits immediately; memory_store discoveries mid-task and at the end (recipes/pitfalls, not transcripts).
- Deliverable: report `devdoc/review-memory-aware-<area>.md` (Completed / Not Done / Verification with build+test+commit hash / findings table P0-P2 with file:line). Commit with `git add <file>` one-by-one (NEVER git add -A). No merge — PA merges after review.
- ANTI-RECURSION: do NOT submit further child tasks; this epic has already decomposed the work.
- Final message declares branch name + recovery command per Rule 6d.
