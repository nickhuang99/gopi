<!-- doc-class: historical -->
# Task Report: Workflow Emission Contract (P1) + R3 Exit Decoupling (P1.5)

**Epic:** pa-q-p3-wf-contract
**Branch:** `feat/wf-contract` (worktree `~/workspace/gopi-wf-contract`, shared object store with `~/work/GoPi`)
**Date:** 2026-08-28
**Scope source:** `devdoc/tasklist-audit-compare-report.md` §9 final dispatch priority, after P0
(`feat/tasklist-p0-impl`), P1.4/P2 nudge + P3/P4 telemetry (`feat/tasklist-nudge-impl`) already landed on master.

---

## Completed

### P1 — Single-source emission contract (audit compare-report §9, "unify rule1+rule2+epic framing")

Both prior audits found three independent statements of "when to emit" (kernel rule 1 "first
substantive round", rule 2's omit-cadence, Epic framing "first substantive round") that could
drift and pushed deferred emissions.

- **New constant `emissionContract`** (`internal/agent/loop.go`, next to
  `maxRoundsWithoutTaskList`): *"Establish the [TASK_LIST_BEGIN]...[TASK_LIST_END] list as a
  PLAN (not receipt) on or before your second tool round — never hold it for the final message."*
  This is now the single source of truth for BOTH the establishment deadline and the
  never-as-receipt rule.
- **Rule 1** interpolates it (replacing the ambiguous "first substantive round") and now states
  the harness guarantee that makes omission safe: an omitted update never wipes tracked state.
- **Rule 2** unchanged in cadence (R2 token savings preserved, `maxRoundsWithoutTaskList=3`).
- **Epic framing** (first-turn message) interpolates the same constant — it no longer carries a
  competing deadline sentence.
- Old text remains greppable nowhere: `first substantive round` is gone from both surfaces.

### P1.5 — Decouple R3 trusted-completion from end-of-run list emission (compare-report §9, "解耦 exit 与清单发放时机", verified 21% plan-tailing class)

Before: the R3 fast-exit fired only on `tasks != nil && AllCompleted(tasks)` — the round itself
had to re-emit an all-completed list, so a model following the contract's "don't re-emit a
receipt" was punished with the bug022 confirmation round, and the rational strategy became
holding the only list for the final message (RC2's 21% plan-tailing).

- **New `func (a *Agent) trustedCompletion() bool`**: harness-tracked state
  (`TaskState.hasList && AllCompleted(TaskState.tasks)`).
- **R3 exit condition** is now `(tasks != nil && AllCompleted(tasks)) || a.trustedCompletion()` —
  legacy same-round list path kept as the compatible path, per the compare-report's proposal.
- `nil`/never-established/progressing states return false — the bug022 confirmation round and
  the P1.4/P2 no-list nudge protection stay intact for list-less sessions.
- Task-end `memory_store` enforcement path unchanged (fires on either completion signal).

## Tests (regression guard — all new, file `internal/agent/wfcontract_test.go`)

| Test | Pins |
|---|---|
| `TestEmissionContract_SingleSource` | rules block interpolates the constant; ambiguous phrase gone; harness-state rationale present |
| `TestEmissionContract_InEpicFraming` | real `Run()` request 1: Epic framing interpolates the constant, no competing deadline |
| `TestR3TrustedCompletion_FromTrackedState` | completed run whose final text omits the list still fast-exits (2 API calls), final message carries NO list block |
| `TestR3TrustedCompletion_NoListStillUntrusted` | hasList==false → confirmation round stays (3 API calls) |
| `TestTrustedCompletion_Table` | nil state / no list / progressing / tracked all-completed |

## Not Done

- **P3 structured `task_list` tool** (Rule-11 double-scenario adoption proof) — explicitly
  mid-term in the dispatch list, not part of this epic's wf-contract scope.
- **Rule-11 live-session adoption evidence**: no new tool was added; the changed surfaces are
  prompt text + exit logic, verified by harness-level tests, not by LLM tool-use telemetry.
- No `first_tasklist_round` field-name change: P3/P4 telemetry already landed as
  `FirstTasklistMsgRatio` in `trajectory.go` (kept as-is).

## Verification

- Build: `go build ./...` — OK (exit 0)
- Targeted: `go test ./internal/agent/ -run "TestEmissionContract|TestR3TrustedCompletion|TestTrustedCompletion_Table"` — 5/5 PASS
- Full suite: `go test ./...` run detached (log `/tmp/wfcontract-test.log`) — see commit message for final tally
- Regression: pre-existing R3 tests (`TestAgent_Run_KeepsConfirmationForTerseCompletedList`,
  trusted-completed R3 + store round) still PASS (legacy same-round path preserved)

## Design notes (Rule 7)

- The contract sentence deliberately bundles three things the audits proved must travel
  together: the deadline ("on or before your second tool round" — measurable, matches the 93%
  turn-1 behavior), the plan-not-receipt rule, and the anti-hold warning ("never hold it for
  the final message") — the last one only became safe to say once P1.5 removed the exit penalty
  for omitting the receipt list. Contract and incentive were fixed together on purpose; fixing
  only the prompt would have re-created the RC2 trap.
- `git add -f` is required for `internal/agent/wfcontract_test.go`: `.gitignore` carries a bare
  `agent` pattern that silently ignores everything under `internal/agent/` for NEW files
  (tracked files are unaffected). Worth a follow-up fix on master (would have hidden future
  test files).
- Concurrency note: another agent was simultaneously editing `plugins/websearch/scripts/web_fetch.py`
  in this worktree; that file is NOT part of this commit (explicit-file staging per Rule 8).

## Deliverable declaration (Rule 6d)

- **Branch:** `feat/wf-contract`
- **Recovery command:** `git show feat/wf-contract:devdoc/wf-contract.md` (code:
  `git show feat/wf-contract:internal/agent/loop.go`, `git show feat/wf-contract:internal/agent/wfcontract_test.go`)
- **Confirmation:** committed in worktree `~/workspace/gopi-wf-contract`; worktree shares the
  main repo's refs, so the commit is already on `feat/wf-contract` in `~/work/GoPi` (no push
  needed). Master untouched; live binary untouched; no force-push.
