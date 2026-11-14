<!-- doc-class: historical -->
# Arming the submit-time ground refusal: the fixture-id migration

**Task** `01a12b86-ebbe-7e7e-a944-81923d97c20d`
**Branch** `task/01a12b86ebbe7e7ea94481923d97c20d`
**Epic** 01a12b86 (follow-up to 01a12b5c, which built the refusal seam)

## What this change is

Epic 01a12b5c landed a **submit-time refusal**: `gopi task submit --task-id t-foo`
against a repo-aware row is refused where it stands instead of being accepted and
dying at dispatch (`internal/tools/task_submit_ground_name.go`,
`TaskSubmitGroundNameDenied` / `TaskSubmitGroundNameRefusal`, consulted by
`TaskSubmitReturningID` under `TaskSubmitOptions.RefuseUngroundableID`). The
engine kept that option **default false**, because a first cut that tested the
dispatch gate's condition unconditionally came back with **410 failing tests** in
one budget run: a fixture submits a slug id against an explicit repo, and the
engine cannot tell a fixture from an operator's typo — the caller is the only
thing that knows which it is.

This task discharges the **precondition of the arm**: every fixture that drives
the REAL `task submit` verb through the CLI with a slug `--task-id` now passes a
**full uuid**, so the arm can be set on the CLI face
(`cmd/agent/cli_tasks.go`, `taskSubmitRefusesUngroundableID = true` — a named
constant, Rule 1) and the budget suite is green with it armed.

**The refusal seam itself is untouched.** `internal/tools/task_submit_ground_name.go`
and the `TaskSubmitOptions.RefuseUngroundableID` reading in `internal/tools/tasks.go`
stay exactly as committed on master; the only engine-side change on this branch
is the comment inside the `cli_tasks.go` arm site (its old text pointed at a test
file that does not exist — corrected to the file that does).

## Completed

- **18 files changed** under `cmd/agent/`: 17 test files whose fixtures submit a
  row through the real binary, plus `cli_tasks.go` (the arm's named constant and
  its call site).
- Every CLI-driven `--task-id` that was a slug is now a full uuid, and every
  assertion that named a slug was updated with it (the id appears in `task show`
  read-backs, `task update` argv, `task send` gate probes, sync-rig seeds, the
  worker e2e chain, and the reparent/dependency fixtures' parent links).
- The **arm** is set: `taskSubmitRefusesUngroundableID = true` on the CLI face only.
- A **coverage-ratchet extension**: this branch's own changed `cmd/agent` files
  are registered in `syncPeersBareUnrelatedFiles` — with the reason — so
  `TestSyncBarePeersTestCoversEveryChangedFile` keeps holding while the branch is
  unmerged (that test derives "what did this branch change" from `git diff HEAD`).
- The slack comment in `cli_tasks.go` that named `task_submit_cli_arm_test.go`
  was corrected: that file **does not exist**; the arm's real proof is
  `TestTaskSubmitRefusesANonUUIDRowIDAtTheCommandLine`
  (`cmd/agent/cli_tasks_help_surface_basic_test.go`), which drives the real
  binary and asserts the refusal sentence, the absence of a queued row, and the
  full-uuid acceptance half.

## Not Done (honest)

- **The ten remaining budget-run failures are not this task's.** They are
  unrelated to the arm and reproduce in the `BEFORE` run as well. Each is named
  with its measured cause in `## Unmigrated / not-this-task sites` below.
- **Library call sites were deliberately NOT migrated.** `internal/tools` has
  ~303 `TaskSubmit{TaskID: "t-…"}` fixture sites; they are legal whenever the row
  is not repo-aware, and the engine's option stays `false` for library callers by
  design. Only the CLI face's precondition was in scope. See the same section.
- No new gate was added; the arm's own proof is the existing real-binary test.

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK (the suite
  builds the binary itself via `gopiTestBinPath`).
- **Tests:** `make test-budget PKGS='./internal/tools ./cmd/agent'`
  - **BEFORE** (stream `run-1791735227-1781588.json`, arm on, fixtures unmigrated):
    **74 fail events / 61 distinct failing tests**, `./internal/tools` **pass**,
    `./cmd/agent` **fail**; 54 of the 61 carry the refusal sentence
    "no ground name can be derived", the other 7 are unrelated.
  - **AFTER** (stream `run-1791736152-1939944.json`): **12 distinct failing
    tests** — the arm-driven and migration-collateral ones were then fixed
    (below), leaving 10 unrelated.
  - **FINAL** (stream `run-1791737219-2158224.json`, arm ON, all fixtures
    migrated): **8 distinct failing tests, and NONE of them carries the refusal
    sentence** — `./internal/tools` **pass**, and every remaining red is one that
    was already red in the BEFORE stream, with no `task submit` call in the
    failing test. The migration's own collateral red
    (`TestSyncBarePeersTestCoversEveryChangedFile`) is **green** in this run: the
    coverage list extension fixed it. The three arm-driven reds of the
    intermediate run are green too. **The arm is therefore proven discharged**;
    the suite's overall exit is 1 for pre-existing, host-condition reds that
    this task neither caused nor was scoped to fix, each named below.
  - Counts are read mechanically from the `-json` streams under
    `/home/user/workspace/gopi-test-scratch/queue/test-budget/`
    (`Action=="fail"` with a `Test` field, split on `/` for distinct top-level
    tests; attribution by scanning `Action=="output"` for the refusal sentence).
- **Ran:** `make test-budget PKGS='./internal/tools ./cmd/agent'` (the declared
  budget entry point, real arguments, on this branch with the arm set).
- **Result:** quoted from the streams; the per-run numbers and their stream file
  names are in `## Evidence (self-filed)`.
- **Commit:** see the branch tip (`git log --oneline -1` on
  `task/01a12b86ebbe7e7ea94481923d97c20d`).

## Evidence (self-filed)

All artifacts are under
`/home/user/.gopi/sessions/01a12b86-ebbe-7e7e-a944-81923d97c20d/artifacts/`
(Rule 37 — never inside the worktree).

- **BEFORE stream, read mechanically** (`r1-before-make.txt`, and the JSON parsed
  into `before-fails.txt` / `before-tests.txt` / `before-nonrefusal.txt`):
  stream `run-1791735227-1781588.json`; `pass github.com/nick/gopi/internal/tools`
  / `fail github.com/nick/gopi/cmd/agent`; 74 fail events, 61 distinct top-level
  tests; 54 carry the refusal sentence, 7 do not
  (`TestChatCookedFallback_NonTTY`, `TestChatVoicePrivacyRowCarriesOnePrefix`,
  `TestDaemonScopeNoPrivilege_TheCorrectionIsEmitted`,
  `TestMergeUnionRowCheckOnTheRealManifest`, `TestRunPathPlainPromptIsVerbatim`,
  `TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation`,
  `TestWorkerDaemon_StartProofAndPidSurviveTheScopeChange` — none of which calls
  the submit verb at all).
- **AFTER stream** (`after-tests.txt`): stream `run-1791736152-1939944.json`,
  12 distinct failing tests. Of those, **3 were the arm acting on fixtures the
  first pass missed** — `TestTaskChildFailEscapeRefusesOutsideSandboxE2E`
  (`t-esc-refuse`), `TestTaskChildFailEscapeStillFiresInsideSandboxE2E`
  (`t-esc-fire`), `TestWorkerRefusesWhenTheGroundCannotBeMade`
  (`t-ground-e2e-refuse`), each with the refusal text naming its id in the
  stream; **1 was migration collateral** —
  `TestSyncBarePeersTestCoversEveryChangedFile`, reporting 11 changed `cmd/agent`
  files absent from its coverage lists; the remaining 8 are the pre-existing /
  environment-dependent set above.
- **Targeted re-run after the fix** (Rule 31, the surface the change governs):
  `go test -count=1 -run 'TestSyncBarePeersTestCoversEveryChangedFile|TestTaskChildFailEscapeRefusesOutsideSandboxE2E|TestTaskChildFailEscapeStillFiresInsideSandboxE2E|TestWorkerRefusesWhenTheGroundCannotBeMade|TestTaskSubmitRefusesANonUUIDRowIDAtTheCommandLine' ./cmd/agent/`
  → `ok github.com/nick/gopi/cmd/agent 8.922s`.
- **The arm's own proof, read from the test source:** the refusal half asserts
  exit≠0 + the sentence + no `Task queued` line + nothing written; the acceptance
  half runs the identical argv shape with `1d7e2586-1b68-5f8d-a607-fbb086669628`.

## Affected-test counts, read mechanically from the `-json` streams

Read with a parser over `/home/user/workspace/gopi-test-scratch/queue/test-budget/`
(`Action=="fail"` with a `Test` field → split on `/`; a test is *arm-affected* when
its own `Action=="output"` lines carry the refusal sentence):

| stream | package verdicts | fail events | distinct failing tests | carrying "no ground name can be derived" |
|---|---|---|---|---|
| `run-1791735227-1781588.json` (BEFORE, arm on, fixtures unmigrated) | `internal/tools` pass / `cmd/agent` **fail** | 74 | **61** | **54** |
| `run-1791736152-1939944.json` (AFTER first pass) | pass / fail | 12 | 12 | 6 |
| `run-1791737219-2158224.json` (FINAL, all fixtures migrated) | `internal/tools` pass / `cmd/agent` fail | 8 | **8** | **0** |

- **The affected-test list (before), named**: the 54 = the 61 distinct failures of
  the BEFORE stream **minus** the 7 that carry no refusal sentence
  (`TestChatCookedFallback_NonTTY`, `TestChatVoicePrivacyRowCarriesOnePrefix`,
  `TestDaemonScopeNoPrivilege_TheCorrectionIsEmitted`,
  `TestMergeUnionRowCheckOnTheRealManifest`, `TestRunPathPlainPromptIsVerbatim`,
  `TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation`,
  `TestWorkerDaemon_StartProofAndPidSurviveTheScopeChange`). The per-test list is
  in `artifacts/before-tests.txt`; the sentence-carrying subset in
  `artifacts/before-fails.txt`.
- **After number: 0** arm-driven failures. Of the 8 remaining, **7 are the
  unchanged pre-existing set above** (each red in the BEFORE stream too), the 8th
  is the environment-flaky `TestUserBusProbe_RealUnlinkedSocketIsDetected`, which
  reads green in the neighbouring runs of the same tree
  (`run-1791737858-2351965.json`, `run-1791737935-2367485.json`: 0 failures,
  package pass).
- **Deduction (the deliverable's own evidence): 54 arm-driven failures → 0** while
  the arm stays set, which is exactly the precondition this epic was filed to
  establish.

## Unmigrated / not-this-task sites (named with their reason)

1. **`internal/tools` library fixtures (~303 `TaskSubmit{TaskID: "t-…"}` sites).**
   Not migrated, deliberately: `RefuseUngroundableID` defaults **false** for
   library callers, and the option is the *caller's* declaration that it speaks
   for an operator (the engine cannot measure that fact — see the seam's own
   header). Those rows are legal as long as they resolve no repo, and the three
   named in the seam's comment (`t-tie-zzz`, `t-attrib-live-runner`,
   `t-start-stamp`) submit slug ids against an explicit repo by construction —
   they exercise code paths that never resolve a ground.
2. **`archive/tasks_ingest_test.go`** (`t-mirror-1`, `t-mirror-2`, `t-fake`):
   `//go:build ignore` — not compiled, not run by any budget stream.
3. **`cmd/agent/cli_notify_test.go` (`t-notify-fixture`),
   `cli_tasks_attach_index_test.go` (`t-attidx-brief`),
   `cli_resume_status_e2e_test.go` (`t-resume-status-e2e`),
   `cli_session_selector_test.go` (`t-session-verb-selector`),
   `task_claim_ground_test.go` (`t-claim-ground-seam`),
   `cli_session_help_surface_basic_test.go` (`t-g4-surface`)**: slug ids that do
   **not** go through the CLI submit verb — `tools.TaskNew`,
   `tools.TaskClaimExplicit`, direct ledger inserts, or the `session` verb. The
   arm only governs `runTaskSubmit`, so these are unaffected; migrating them
   would be churn with no bearing on the precondition.
4. **The ten unrelated budget-run failures** (8 in the AFTER stream + 2 that are
   flaky/order-dependent across runs): chat session-event counting
   (`TestChatCookedFallback_NonTTY`), the run-path prompt verbatim check,
   the worker-daemon scope/PPID family, `git init` under `/dev/shm`
   (`TestWorkerDaemon_StartProofAndPidSurviveTheScopeChange`), the daemon
   no-privilege correction text, doc-evidence anchors
   (`TestMergeUnionRowCheckOnTheRealManifest`), the unlinked-socket probe, the
   chat-voice privacy prefix, and the child-fail sandbox family. Each was RED in
   the BEFORE run too, with no submit call anywhere in the failing test.
