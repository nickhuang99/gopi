<!-- doc-class: historical -->
# Task Report: submit-time refusal for a row id that can never name a ground

Task: `01a12b5c617373e099183da01416ebc9` (branch `task/01a12b5c617373e099183da01416ebc9`).
Epic reframed from the raw briefing by `task(action=reframe)` — verdict DOABLE.

## The short version, first

The epic's *deliverable 1* landed: **one shared predicate** in `internal/tools`.
The epic's *deliverable 2* (the live `gopi task submit` refusal) did **NOT** land,
and this report says so plainly: **three independent budget runs measured that no
face-based discriminator exists**, because the tests drive the very same CLI verb
with the very same slug-id shape the operator types. The final commit therefore
ships the seam **without** the CLI arming it, and the remaining work is dispatched
as its own task (see `## Not Done`). The dispatch-time gate is untouched and keeps
its current behaviour, exactly as the epic required.

## Completed

- **ONE shared predicate** — `internal/tools/task_submit_ground_name.go`:
  - `TaskSubmitGroundNameDenied(taskID, repo) bool` — the dispatch gate's own two
    facts: `!fullid.IsUUID(id)` AND the row is repo-aware (`repo` non-empty).
    `fullid.IsUUID` is Rule 14's single definition of "this id can name a
    uuid-derived ground"; the file cites `TaskRunGroundFor`'s condition so the two
    cannot drift.
  - `TaskSubmitGroundNameRefusal(taskID, repo) error` — returns nil exactly when
    the predicate is false, otherwise wraps **`taskGroundNameRefusal`**, the
    dispatch gate's own sentence. The operator reads the same cause, consequence
    and door at submit time (Rules 13/14: one definition, two moments).
  - `TaskSubmitRefusalFace` (`"task_submit"`) — the shared spelling of the face.
- **The submit funnel has the seam, and it writes nothing when it fires**:
  `internal/tools/tasks.go` `TaskSubmitReturningID` consults the refusal
  immediately after the `--repo` resolution and `taskRepoVerifiable`, i.e.
  *before* the ledger-address gate, the id mint, the collision pre-check, the
  payload spill, the attachment copies, the session container and the INSERT.
- **`TaskSubmitOptions.RefuseUngroundableID`** carries the one fact the engine
  cannot measure — "a human typed this id" — and **defaults to false**. Only the
  predicate's own test sets it today; see `## Not Done` for why no production face
  does.
- **The dispatch-time gate is untouched** — `git diff internal/tools/task_run_ground.go`
  is empty (0 lines). `TaskRunGroundFor` keeps its exact condition and behaviour.
- **Tests** — `internal/tools/task_submit_ground_name_test.go`:
  - `TestSubmitGroundNamePredicateFiresOnlyOnDerivingRows`: the truth table,
    including the two negatives that keep the check additive (a full-uuid id; a
    repo-less row), that the wrapper is nil exactly when the predicate is false,
    and that the sentence names the row, the cause, the consequence and the door.
  - `TestSubmitRefusesANonUUIDRowIdBeforeWritingAnything`: the refusal driven
    through the **real** `TaskSubmit` funnel — no row, no session container, and
    a full-uuid id still queues.

## The measurement that stopped deliverable 2 (three budget runs)

This is the substance of the run, and it is a **negative** result about the epic's
own premise. The premise was that scoping the refusal to "the operator/LLM faces"
avoids the fixture blast radius (memory #7578). Two scopes were tried, and the
budget run refuted both:

**Cut 1 — unconditional at the engine** (the gate's own condition). Theory: a
fixture naming no repo can never reach it.
Result: `make test-budget PKGS='./internal/tools ./cmd/agent'` →
`exit=1 outcome=fail`, **410 distinct affected tests in each of two independent
runs** (streams `run-1791729502-685952.json`, `run-1791729581-714348.json`). The
sentence names the fixtures:

```
archive_owner_pick_test.go:237: submit t-tie-zzz: … repo /home/user/work/GoPi …
branch_attribution_test.go:140:  submit t-attrib-live-runner: … repo /home/user/work/GoPi …
claim_holder_freshness_test.go:86: submit t-start-stamp: … repo /home/user/work/GoPi …
```

A fixture seeds a **slug id** with an **explicit repo** (the main checkout) and
never resolves a ground — it just reads the row back. So a repo-aware slug-id row
is the *normal* fixture shape, not an exotic one.

**Cut 2 — the epic's prescription: arm it only at the CLI operator face.**
Result: `run-1791730529-918212.json` → `exit=1`, 112 failing tests, of which
**67 carry the refusal sentence** — and they are driven through *this very verb*:

```
cli_tasks_help_surface_basic_test.go:1044: gopi task submit … --task-id t-g1-surface …
cli_tasks_submit_payload_test.go:45:       gopi task submit … --task-id t-positional-submit …
cli_tasks_source_gate_e2e_test.go:189:     task submit --source agent … t-gate-role-agent …
```

**That is the finding**: `gopi task submit --task-id t-…` *is* how the fixtures
give a row a deterministic id, so the operator's typo and a test's seed row are
**the same command with the same shapes**. There is no face, flag, environment or
verb left to discriminate on — the epic's constraint ("scope it to the operator
faces to avoid the fixture blast radius") cannot be satisfied as literally stated,
because the two are indistinguishable at every seam this code can see.

What the flag *can* do is let a caller **declare** which it is — which is exactly
what `RefuseUngroundableID` is, and why it defaults to false rather than to a
guess. It is a real seam, mechanically tested through the real funnel; what it is
not is *armed*, because arming it turns 67 working tests red.

## Not Done (honest)

- **The live `gopi task submit` refusal is NOT enabled** — acceptance criterion
  (b) of the epic is **NOT met**. `cmd/agent/cli_tasks.go` deliberately does *not*
  set `RefuseUngroundableID`, with the measured reason recorded in a comment at
  the call site. The defect the epic describes therefore **still reproduces**:
  `gopi task submit --task-id t-foo` exits 0 and prints "Task queued". I did not
  claim otherwise, and the pre-reframe red proof is not evidence of a green fix.
- **`cmd/agent/cli_tasks_submit_ground_refusal_test.go` was written, passed in
  isolation, and then DELETED** when the CLI arm was reverted: a test asserting a
  refusal the shipped binary does not perform is worse than no test. Its content
  is preserved in the dispatched task's payload.
- **The `task_submit` tool face has no task-id parameter at all**
  (`internal/agent/loop.go`: summary/payload/tags/parent_task_id/repo/priority/
  attachments), so that half of deliverable 2 is moot — there is no tool face that
  can spell a bad row id.
- **The precondition for landing this is a fixture migration**: the slug `--task-id`
  call sites in `cmd/agent` and `internal/tools` must become full uuids before any
  face can arm the refusal. That is a self-contained task and has been **dispatched
  as `01a12b86-ebbe-7e7e-a944-81923d97c20d`** (parented to this row, so it is
  parked until this row reaches completed/verified — Rule 33). Its payload carries
  the measured method (arm the flag, run the budget run, read the failing names out
  of the `-json` stream, migrate, re-run) and the acceptance criterion this report
  could not meet.
- **This is a hosta-side (client) fix on the submit seam** (Rule 28); it changes
  no ground-naming behaviour — it only reports, earlier, a verdict the gate
  already gives.

## Verification

- Build: `go build ./...` → OK (`BUILD_OK`).
- Tests:
  `go test ./internal/tools/ -run 'TestSubmitGroundName|TestSubmitRefusesANonUUIDRowId' -count=1`
  → `ok github.com/nick/gopi/internal/tools 0.554s`;
  the three CLI fixtures that cut 2 broke, re-run after the revert →
  `go test ./cmd/agent/ -run 'TestTaskVerbSubmitRunsAsAdvertised|TestCLIPositionalSubmitIsDispatchable|TestTaskSubmitSourceMachineRolesRoundTrip' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 4.054s`.
- Ran (the surface, for real, on an isolated profile — this is what proves the
  refusal *works* and, honestly, that it is not *armed*):
  `CGO_ENABLED=0 go build -o $SCR/gopi ./cmd/agent/`, then
  `GOPI_HOME=$SCR/home $SCR/gopi task submit "probe" --task-id t-foo --repo $SCR/repo`.
- Result: with the CLI arm **present** (intermediate commit `98887f13e`) the
  command exited **1** with the ground sentence and `task show t-foo` exited 1
  (no row); with the arm **reverted** (the shipped state) it exits **0** and
  queues the row. Both readings are stated because both are true of this run's
  path, and the shipped one is the second.
- Commits: `356b29250` (predicate + engine seam), `98887f13e` (the armed CLI, and
  the measurement that refuted it), plus the final revert commit on this branch.

## Evidence (self-filed)

- `go build ./...` → `BUILD_OK` (exit 0) at each of the three cuts.
- **Cut 1, red**: `exit=1 outcome=fail`, 410 distinct affected tests, twice
  (`run-1791729502-685952.json`, `run-1791729581-714348.json`), counted from the
  streams by matching the refusal sentence.
- **Cut 2, red**: `run-1791730529-918212.json`, `exit=1`, 112 failing tests, 67
  carrying the sentence — including `TestTaskVerbSubmitRunsAsAdvertised`
  (`exit 1 (want 0)`), `TestCLIPositionalSubmitIsDispatchable` (`code=1`),
  `TestTaskSubmitSourceMachineRolesRoundTrip/agent` (`exit=1 want 0`).
- **Green after the revert**: `ok github.com/nick/gopi/cmd/agent 4.054s` for the
  three representative fixtures above.
- `grep -rn '"task", "submit"' cmd/agent/*_test.go | wc -l` → `48`; there are
  **145** `--task-id` occurrences across `cmd/agent/*_test.go` — the population
  that must be migrated before the refusal can be armed. This is a count of
  *occurrences of a flag*, several of which belong to `task sync`/`task pull`
  rather than `task submit`; the exact submit-only subset was **not** established
  and is not claimed.
- The census in the intermediate commit's message (228 `TaskSubmitOptions`
  literals with an explicit `TaskID`, 26 also setting `Repo`, 4 with a literal
  `t-*` id) is a **proxy** and the budget runs refuted it as a predictor — it
  counts call sites, not the runtime values of variables. Reported as a proxy, not
  as evidence.

## The budget run's failing set is ENVIRONMENTAL — measured against master

The touched-package budget run (`make test-budget PKGS='./internal/tools ./cmd/agent'`,
stream `run-1791731649-1198378.json`) reports 14 failing tests in
`internal/tools`. **None of them is this branch's.** The instrument that shows it is
master itself:

```bash
# the same tests, on the SHARED master checkout, nothing of this branch applied
cd /home/user/work/GoPi && git rev-parse --abbrev-ref HEAD   # → master
go test -count=1 ./internal/tools/ -run 'TestQueryDefinitionSelfHealsUnsearchableIndex|TestCodeSearchSurvivesEOFProbe|TestMergeBuildGate_RefusesTheIncidentShape|TestMergesetGate_GatesLegIsScopedToTheMergesDiff|TestMergerRepoSweep_BuildRefusalArchivesOnceAndFreesTheQueue|TestPostMergeVetGate_UnparsableTimeoutAbstains'
# → --- FAIL: (all six) ... FAIL github.com/nick/gopi/internal/tools
```

Two host properties explain all 14, and neither is a code defect this branch could
have introduced:

- **`ctags` is not on PATH on this host** (`which ctags` → empty on hosta). The
  `code_search` tests (`TestQueryDefinitionSelfHealsUnsearchableIndex`,
  `TestCodeSearchSurvivesEOFProbe`, `TestCodeSearchWithTimeout_ShortTimeoutNormal`,
  `TestSearchWithTimeout_NoMatchReturnsEmptyNotError`) assert against a real ctags
  binary: their message is literally
  `code_search: ctags: exec: "ctags": executable file not found in $PATH`.
- **The merger build-gate has no usable DISK-backed shared Go build cache**, so it
  ABSTAINS by design (`[task-notify] merger: BUILD gate ABSTAINING … RAM-backed
  storage (tmpfs)`) — `merge_build_gate_test.go:244: the non-compiling branch was
  NOT refused`. The remaining ten failures are all in that family
  (`TestMergeBuildGate_*` ×4, `TestMergesetGate_*` ×3, `TestMergerRepoSweep_*`,
  `TestPostMergeVetGate_*`, `TestTaskMerge_BuildRefusedArchivesAndNotifies`); the
  abstention is the documented 2026-09-15 tmpfs incident's own guard firing.

Both are properties of the machine, not of the diff — which is why the branch's own
tests are the evidence that matters here, and they are green in the same run:
`TestSubmitGroundName*` (`internal/tools`) and the three CLI fixtures named above
(`cmd/agent`, `ok … 4.054s`). The honest reading of the budget run is therefore
**green on everything this branch touches, red on 14 environmental tests that are
equally red on master** — stated because a bare "budget run: 14 failures" would
misread as this branch's damage in exactly the way Rule 5 warns about.
