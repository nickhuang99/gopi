<!-- doc-class: historical -->
# The branch sweeper must REPORT, never ACT

**Epic:** sweep-report-not-act · **Branch:** `feat/repo-sweep-report-only` ·
**Base:** 21f4cc71 (`docs(brief): the branch sweeper must REPORT, never ACT`) ·
**Date:** 2026-09-19

## The incident this closes

A live session's branch `fix/conflict-branch-rename-recovery` and its worktree
were deleted **mid-session** by the per-repo sweep. Its tip was `1fb4ea4a` — a
delivery-point sync that had merged master *into* the working branch. A tip that
contains master **is an ancestor of master**, so the sweep's arm ① asked git one
question

```go
_, ancErr := repoSweepGit(repo, "merge-base", "--is-ancestor", branch, masterBranchName)
if ancErr == nil { removalaudit.BranchDelete(...); repoSweepGit(repo, "branch", "-d", branch) }
```

and on a **true** answer pruned the ref. The predicate was not wrong about the
fact — the tip really was integrated. It was wrong about the **disposition**:
nothing in it ever asked *who is using this branch*.

The operator's model, which this change adopts as the spec: **every branch has
exactly two destinations** — it merges and is deleted, or it cannot merge, a task
is filed, and it eventually merges and is deleted. Under that model no branch is
ever left ownerless in `refs/heads/`, so a mechanism whose job is "clean up
whatever the merger did not handle" has **no legitimate work**: on a healthy
system its only observable effect is deleting branches still in use.

Operator's instruction (2026-09-19): *先改成报告不要动作，发警告。立刻派单*.

## Completed

- **`internal/tools/repo_sweep_report.go` (new)** — the sweep's report arm.
  - `repoSweepWouldPrune(repo, branch)` — the ONE probe: resolve the tip
    (`rev-parse refs/heads/<b>`), ask git the *same* ancestry question the
    destructive arm asked, and collect **owner evidence** via `TaskForBranch`
    (the ONE branch→row loader, Rule 14 — never a second lookup). Git's own exit
    1 ("not an ancestor") is the ordinary not-spent answer; any other failure is
    returned as an error, so a wedged repo is never reported as either verdict
    (Rule 15).
  - `repoSweepOwnerEvidence` — splits the ledger rows that name the branch into
    **LIVE** / **terminal** / **foreign-repo** (a row naming the branch in
    another repo is *named*, not dropped — two hosts legitimately carry one
    branch name), and renders the honest fourth answer `no ledger row names this
    branch`. An unreadable ledger reads as UNKNOWN, never as "no owner".
  - `repoSweepReportSpentBranch` — reports the branch and, **only when a LIVE
    row names it**, raises the alarm on TWO channels: an always-on stderr warn
    (`[sweep][warn] …`) **and** the notify event
    `sweep-would-prune-live-branch` on the existing lane (`NotifySyntheticEvent`
    — the same as stale / build-failed / worktree-reaped). Both are needed: the
    notify half alone is not enough, because `NotifySyntheticEvent` **returns
    silently** when the event is outside the operator's allow-list and the
    shipped default list is `claimed,in_progress,completed,failed` — measured
    2026-09-19, this host's `settings.json` lists
    `…,stale,build-failed` and would still have dropped the alarm, leaving the
    epic's entire point invisible (Rule 15a). A terminal owner is the ordinary
    post-merge state and is deliberately not an alarm.
  - `RepoSweepPruneReport{Branch, Tip, Verdict, Owners, LiveRows, Detail}` +
    `RepoSweepPruneLines` — the ONE renderer of the report line.
- **`repo_sweep.go`** — the destructive arms are gone:
  - arm ① no longer runs `removalaudit.BranchDelete` + `branch -d`; it calls the
    report arm (verdict unchanged, disposition changed);
  - arm ② (the ff landing) still lands, but **no longer deletes the ref**; the
    durable rebuild mark (`MarkRebuildPending`) is kept, and its comment now says
    why a successor tick cannot re-derive the landing from git state (a reported
    branch is indistinguishable from one never landed);
  - arm ③ (`MergedCleanupSweep` every tick) is **retired from the tick** — no
    cleanup lane is left to run;
  - `RepoSweepResult` gained `Reported int`, `WouldPruneLive int`,
    `WouldPrune []RepoSweepPruneReport`; `Deleted` is kept declared but retired
    (never incremented); `MergedBranches` no longer includes prunes (a prune is
    not a landing);
  - the function-local `masterBranchName` uses became
    `repoSweepIntegrationBranchFn()` (a seam, not a tunable: the tests build
    fixture repos and the tick is a function of the repo it is pointed at);
  - `isGitExit1` is used as before — the two-answer split survives.
- **`internal/tools/task_merged_cleanup.go`** — the reaper's mutating arms are
  **retired in production** via `MergedCleanupOptions.ReportOnly`, which NAMES
  the pair it would have reaped (lane, branch, worktree, owner evidence) in the
  new `MergedCleanupReport.Reported`. The guard ladder (dirty, live-CWD, live
  row, off-tip, race-free merged recheck, foreign owner, no-work) is
  byte-for-byte unchanged; only the disposal is gone. The parameter (rather than
  an outright deletion of the code) is what keeps the ladder's own tests
  meaningful on their throwaway fixtures.
- **`cmd/agent/cli_tasks.go`** — both `serveMergedCleanupSweep` call sites pass
  `ReportOnly: true`; the report lines gained `would-reap …` and the summary line
  prints `would-reap=N`; the sweep status line prints `would-prune=N` /
  `would-prune-live=N` instead of `deleted=N`;
  `setFinishCleanupSweepOwnerRemoverWired` is **retired** — its only consumer was
  the reaper's mutating arm.
- **`cmd/agent/task_lifecycle_tick.go`** — no longer wires the (retired) owner
  remover; the release still clears the seam to nil, which is now the invariant.
- **`internal/removalaudit/audit.go`** — `ArmRepoSweepBranch` is **deleted**, with
  its comment recording the retirement. The reverse-direction gate
  (`TestRemovalAuditGateEveryRemovalSiteEmits`, floor 15 referenced arms) went RED
  the moment the sweep stopped emitting it — a declared-but-unemitted arm "silently
  stops covering its path". That is the gate doing its job, so the constant was
  retired rather than the gate relaxed (no baseline or floor was touched). After
  the deletion `internal/removalaudit` (ok 0.023s) and 15+ arms remain referenced,
  so the floor still holds.
- **`devdoc/DOC-INDEX.tsv`** — a row classifying this devdoc (the doc-class gate
  requires every file under `devdoc/` to carry class + reason). The 25 unclassified
  devdoc files and the one unbannered file the gate also reports are
  **pre-existing on master** (measured on the untouched baseline worktree: the
  identical two failures), so this branch adds no new doc-class debt.
- **Tests** — the epic's acceptance is an observation:
    - `TestMergedCleanupProductionCallersAllReportOnly` — the mechanical half of
      the "no production caller passes `ReportOnly: false`" claim below. It walks
      `cmd/` + `internal/` (production files only), finds every
      `MergedCleanupOptions{` composite literal and REFUSES one that does not set
      `ReportOnly: true` — written against the VALUE, because the zero value
      (`false`) is the destructive direction. Read-floor included. Measured: 2
      production sites, both report-only.
  - `repo_sweep_report_only_test.go` (**new, the red-proof pair**):
    - `TestRepoSweepWouldPruneReportsLiveBranchAndMutatesNothing` reconstructs the
      incident exactly — a branch whose tip IS an ancestor of master (the
      predicate measured directly and asserted true), still present, still
      checked out, named by a LIVE row — and asserts (a) it is REPORTED with
      branch/tip/verdict/owner evidence, (b) the `sweep-would-prune-live-branch`
      alarm was delivered naming the branch, the tip and the live row, and (c)
      the ref, its tip and master are **exactly where they were**.
    - `TestRepoSweepReportPathHasNoMutatingGitVerb` — the mechanical guard: the
      report path's source is read and refused if it carries `branch -d`/`-D`/`-m`,
      `tag -d`, `worktree remove`/`prune`, `update-ref -d`, with a read-floor and
      an in-place RED/GREEN probe of the matcher (it fires on the retired arm's
      own text and stays quiet on `merge --ff-only`).
  - **Converted, not deleted** (the old assertions were "the branch is gone"):
    `repo_sweep_test.go` (landing keeps the ref; already-integrated is reported,
    not pruned), `repo_sweep_notff_test.go` (landed ref survives at its own tip),
    `repo_sweep_ref_guard_test.go`, `task_merged_cleanup_test.go`,
    `task_merged_cleanup_orphan_test.go`, `wip_worktree_repro_test.go`,
    `removal_audit_record_test.go`, `cmd/agent/cli_tasks_merged_cleanup_test.go`.

## Not Done (honest)

- **`task_merge.go`'s own `branch -d` (arm ② of the *manual* merge path)** is
  **untouched**. `TaskMerge(branches, all=true)` still prunes an
  already-integrated branch (`mergeActionDeleted`, `task_merge.go:516`) and that
  is reachable from `gopi task merge` (`cli_tasks_merge.go`). This epic's scope
  was the **sweep** path (the reported incident); the manual verb is an operator
  action, taken deliberately, and changing it is its own decision. It is the
  same judgment in a different place and should be dispatched separately if the
  operator wants the rule applied there too.
- **The disposal code is retired by parameter, not deleted.** `MergedCleanupSweep`
  can still remove a worktree and delete a branch if a caller passes
  `ReportOnly: false`. No production caller does (the gate is that every
  production call site passes true — grep `MergedCleanupOptions{`), but the
  capability exists so the guard ladder's tests stay honest.
- **The `worktree-reaped` notify event** now never fires from the production
  disposition (nothing is reaped). Its test is converted to assert the silence
  plus the survival of the ground, rather than being deleted, because the event
  contract still exists for the disposal path.
- **The report is emitted on the sweep status line and through the notify
  event**, not as a new operator CLI verb. `gopi task sweep`-style rendering of
  `WouldPrune` was not added — the field is exported and ready for it.

## Verification

- Build: `go build ./...` — clean (exit 0); `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK.
- Vet: `go vet ./internal/tools/ ./cmd/agent/ ./internal/removalaudit/` — clean (exit 0, no output).
- gofmt: `gofmt -l` on every touched file — clean (the pre-existing offenders
  `internal/tools/task_finish_premerge.go`, `task_staleness_guard_test.go`,
  `taskledger_cost_test.go` are present on master too and are untouched by this
  branch).
- Red-proof pair + caller gate: `go test -run 'TestRepoSweepWouldPruneReportsLiveBranchAndMutatesNothing|TestRepoSweepReportPathHasNoMutatingGitVerb|TestMergedCleanupProductionCallersAllReportOnly|TestRepoSweepNotFF' ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools 8.210s`.
- Broad scoped sweep/cleanup suite (`RepoSweep|MergedCleanup|WIP|Removal|Deliverable|MergerRepo|Merge|Sweep|RefGuard|ReportOnly`, `internal/tools`):
  → `FAIL github.com/nick/gopi/internal/tools 934.525s`, whose ONLY failing test is
  `TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind` — the same test fails
  on the untouched `master` baseline worktree (job-10, `fa12d97f`), so it is
  pre-existing and not this branch's.
- `cmd/agent` scoped suite (`MergedCleanup|Sweep|Cleanup|RepoMerge`, with
  `GOPI_BIN` pointed at THIS tree's build): → `ok github.com/nick/gopi/cmd/agent 98.894s` (exit 0).
  `TestSweepZombiesCLI` (the two E2E tests) → `ok … 17.976s`; they FAIL without
  `GOPI_BIN` set, which is an environment requirement, not a code defect.
- `internal/removalaudit`: → `ok … 0.023s`; `internal/standards`
  `TestFlakeRegistryMatchesTheTestsItNames` → `ok … 7.245s`.
- Standards gates (Rules 16–21): `TestRemovalAuditGateEveryRemovalSiteEmits` is
  GREEN after `ArmRepoSweepBranch` was deleted with its emit (it was RED while the
  arm was still declared — that is the gate working, and no floor was lowered).
  The two doc-class failures (`TestDocManifestCoversEveryDevdocFile`,
  `TestDocClassBannerMatchesManifest`) are **pre-existing on master**: the
  baseline worktree reports the identical two, and the missing-row count is 25 on
  both sides. This branch adds a DOC-INDEX row and removes none.
- Worktree cleanliness: `git status --porcelain` prints nothing after the three
  commits (Rule 30).

## Evidence (self-filed)

| claim | observation |
|---|---|
| report arm reports + alarms + mutates nothing | `go test -run 'TestRepoSweepWouldPruneReportsLiveBranchAndMutatesNothing\|TestRepoSweepReportPathHasNoMutatingGitVerb' ./internal/tools/` → `ok github.com/nick/gopi/internal/tools 7.486s` (exit 0) |
| no production caller can re-enable destruction | `go test -run TestMergedCleanupProductionCallersAllReportOnly -v ./internal/tools/` → `PASS`, log line `production MergedCleanupOptions sites: 2, all report-only` (exit 0) |
| legacy "branch is gone" assertions converted | `grep -rn 'assertBranchGone' internal/tools/` → exactly ONE hit, a comment in `task_merged_cleanup_test.go:148`; no call site remains |
| no mutating git verb on the report path | `grep -n '"branch"\|"tag"\|"worktree"\|update-ref' internal/tools/repo_sweep_report.go` → exactly ONE hit, the JSON tag `json:"branch"` at line 128; the same grep over the whole sweep path (`repo_sweep.go`, `repo_sweep_ref_guard.go`) returns 0 for all seven banned verb pairs |
| `go build` + `go vet` | `go build ./...` exit 0; `go vet ./internal/tools/ ./cmd/agent/ ./internal/removalaudit/` exit 0 (no output) |
| the one broad-suite failure is not this branch's | baseline worktree `~/workspace/gopi-sweep-base` at `fa12d97f` (master), same `-run` filter → `--- FAIL: TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind`, identical assertion text |
| `cmd/agent` scoped suite green | `GOPI_BIN=$PWD/bin/gopi go test -run 'MergedCleanup\|Sweep\|Cleanup\|RepoMerge' ./cmd/agent/` → `ok github.com/nick/gopi/cmd/agent 98.894s` (exit 0) |
| worktree clean at close-out | `git status --porcelain` → no output (exit 0) after commits `7f8a4cff`, `0f0c3933`, `6ef91cce` |

## Mechanism notes (for the next reader)

1. **The incident's shape is legal git state, not a bug in git.** A delivery-point
   sync (`merge master → branch`) makes the branch tip an ancestor of master while
   the branch is very much in use. Any code that treats "ancestor of the
   integration branch" as "spent" without consulting ownership reproduces the
   incident; the fix is not a better predicate but a **report instead of a
   disposition**.
2. **Two answers must stay two.** `merge-base --is-ancestor` exits 1 for "not an
   ancestor" and non-zero-other for "cannot answer". Collapsing them into one
   error arm silently disables the landing path (measured while writing this
   change: 9 tests went red because every real branch read as `error`). The
   original `isGitExit1` split is preserved.
3. **Retiring by parameter, not by deletion.** The reaper's guard ladder is the
   most valuable part of it and has ~20 tests; deleting the disposal outright
   would have deleted the ladder's test coverage with it. `ReportOnly` moves the
   *disposition* while leaving the *reasoning* measurable.
