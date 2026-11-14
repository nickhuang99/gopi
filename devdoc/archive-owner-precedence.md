<!-- doc-class: historical -->
# Task Report: the archive owner pick reads the SAFEST row (live > decided > archivable)

Branch: `task/01a1167e38c370e0be1cf6ed304fe851` (worktree
`/home/user/workspace/gopi-01a1167e38c370e0be1cf6ed304fe851`).

## The defect (measured, not hypothesised)

`gopi task archive <branch>` is the hand archive gate. It asks
`archiveBranchOwners()` (internal/tools/archive_reason.go) — one row per branch —
and `ArchiveDecide` turns that **one** row's status into a verdict. The pick's
rule was "an OPEN row supersedes a DECIDED one", where `open` meant
`!archiveOwnerDecided(status)` — i.e. **anything that is not completed/verified**.

`archiveOwnerDecided` is true only for completed/verified, so `cancelled` was
counted as the OPEN side. On peerbox 2026-10-07 (run `01a115c3`, branch
`task/01a113e467767eb9a4c33cb1df1cdfbb`) FOUR rows named that one branch —
among them a `cancelled` row (`01a114aa-eb19-…`), a `completed` row and an
`in_progress` row. The `cancelled` row came first and counted as open, so it
won, and `ArchiveDecide` read it through `archiveOwnerArchivable` →
**Proceed**: `gopi task archive <branch> --reason hand-mistake` exited **0** and
minted `archive/<branch>` with no override, over a branch whose run was still
building.

The near miss is not cosmetic: `archive/<branch>` is what
`repoSweepArchiveExempt` honours and what arms `ArchiveReclaimDecidedRefs`'s
head-deleting arm — a tag over a live ground is one tick away from a running
deliverable's ref being deleted.

**Why the existing guard did not catch it**: the rule was written for the
2026-09-21/22 incident (`fix/stash-guard-compound-chain`, a `completed` row
beating an `in_progress` one) and it only ever compared TWO classifications.
The status space is THREE readings, and the pair of predicates
(`archiveOwnerDecided` / "open") does not partition it — `cancelled` fell into
the gap and landed on the permissive side of a gate whose whole job is to be
permissive only when the ledger already decided AGAINST the work.

## Completed

### 1. Rank-based owner precedence (`internal/tools/archive_reason.go`)

- `archiveOwnerRank(status) int` — the ONE classifier, an ordinal three-place
  scale, most-cautious first (smaller rank wins the pick):
  `archiveOwnerRankLive` (0), `archiveOwnerRankDecided` (1),
  `archiveOwnerRankArchivable` (2). The bottom arm is the DEFAULT: every status
  the package does not classify (pending, claimed, in_progress, and any future
  or unknown spelling) is ranked LIVE. Default-deny is deliberate — a status
  nobody reasoned about must not become the permission to delete refs.
- `archiveOwnerSafer(a, b) bool` — the comparison used by the pick, with a
  **task-id tie-break** so equal ranks resolve identically across runs. That is
  not tidiness: the pre-fix pick was order-sensitive by construction (it decided
  on which row arrived first with which predicate), and the incident's shape is
  itself an order artifact. Two identical reads of one ledger may not disagree.
- `archiveBranchOwners()` now uses them; the doc comment carries the measured
  incident (the four rows, the exit 0, the tag) and the reason the earlier
  2026-09-21/22 note was insufficient.
- `ArchiveDecide`'s comment states which row it is deciding on ("this gate never
  sees the rows — it sees the ONE row archiveBranchOwners picked").

Two rows of the SAME rank now resolve by id, so the ledger-order dependence is
gone for every pair — including archivable-vs-archivable, which the old rule
left to insertion order.

Rules honoured: Rule 1b/13/14 — the rank constants and the classifier are the
one definition the gate, the mechanism (`repoSweepArchiveOwnerGate`), the
backlog check and the `gopi merge --status` renderer all read; no caller
restates an order.

### 2. Unit regression (`internal/tools/archive_owner_pick_test.go`)

- `TestArchiveBranchOwnersPrefersTheLiveRowOverAnArchivableOne` — the incident's
  shape, driven **both ways round** (the archivable row seeded first with ids
  that also sort first, and the live row seeded first with ids that sort last),
  because the defect was order-sensitive. Each subtest asserts the pick AND the
  gate's own answer (`ArchiveDecide` refused, refusal says IN FLIGHT, names the
  live row), plus the fact that `--override-completed` does **not** clear a live
  owner: there is no flag that authorises burying work in flight.
- `TestArchiveBranchOwnersBreaksTiesByTaskID` — two same-rank rows, two
  identical reads, the same answer (determinism).

### 3. CLI regression through the REAL binary (`cmd/agent/cli_archive_test.go`)

`TestArchiveCLIRefusesALiveOwnerDespiteACancelledSibling`: temp git repo + temp
ledger (the file's existing `archCLIRepo` / `archCLISeedRow` / `runArchiveProbe`
pattern — the same one the epic's Rule 32 note points at), `cancelled` row seeded
**first**. It asserts exit != 0, the IN FLIGHT text, the LIVE row named, and
`git tag -l` empty — plus no reason record and the branch ref still present, and
that the decided-owner override arm (`feat/finished` + `--override-completed`)
still writes its tag.

## Not Done (honest)

- The mechanism face (`repoSweepArchiveOwnerGate`) is fixed **by construction**:
  it reads the same `archiveBranchOwners()`, so the rank now governs it too. It
  has no new test of its own in this change; its behaviour for the one-status
  cases is already covered by the existing archive tests.
- The live refusal is still keyed on the ledger row's status only. A branch whose
  last row reads `cancelled` while a process is *actually* building in it is not
  detected here — that question belongs to the claim/worktree layer, and the
  measured incident's live row read `in_progress` in the ledger. Not widened
  (Rule 35: no new machinery for an unmeasured failure).
- `archiveOwnerDecided` / `archiveOwnerArchivable` keep their exact meanings;
  the `gopi merge --status` renderer's "BURIED" marker is unchanged.
- **`make test-budget PKGS='./internal/tools ./cmd/agent'` is not green, and the
  three remaining failures are NOT this branch's**: `internal/tools` keeps
  `TestTaskRunGroundCutsTheBaseFromThePeerTip` /
  `…DegradesExplicitlyWhenThePeerIsUnreachable` / `…SkipsThePeerFetchOnTheServerRole`,
  each failing on a fixture that hands a non-uuid id (`t-ground-base-peer`) to a
  ground resolver that now demands a full uuid (`task_ground_baseline_test.go:163/216/272`).
  Reproduced with identical text from `git archive master` in this session (see
  above) — master's own mismatch, left alone deliberately (Rule 30a's sibling
  discipline: fixing it here would widen the diff into another task's subject).
  `cmd/agent` is fully green.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (also `go vet ./internal/tools/ ./cmd/agent/` → OK).
- Tests: `go test ./internal/tools/ ./cmd/agent/ -run 'TestArchive' -count=1` → both packages ok.
- **Budget run (final, after the delivery-point sync):**
  `make test-budget PKGS='./internal/tools ./cmd/agent'` →
  `cmd/agent` **PASSED**; `internal/tools` FAILED with 3 tests, all three
  **pre-existing on master** and reproduced there in this session (see the
  master-export evidence below). Stream:
  `/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791383666-505592.json`
  (`stamp: 2026-10-07T22:34:26+08:00 host=peerbox pkg=./internal/tools ./cmd/agent budget=25m elapsed=1205s exit=1 outcome=fail`).
- **Red, observed on master's source** (the file swapped back to
  `git show master:internal/tools/archive_reason.go`, tests unchanged):

  ```
  --- FAIL: TestArchiveBranchOwnersPrefersTheLiveRowOverAnArchivableOne
      --- FAIL: .../archivable_row_seeded_first
          got "cancelled" for row 01a114aa-eb19-cancelled, want "in_progress" for 01a113e4-6799-live
      --- FAIL: .../live_row_seeded_first
          got "cancelled" for row zzzz-cancelled, want "in_progress" for aaaa-live
  --- FAIL: TestArchiveBranchOwnersBreaksTiesByTaskID (0.04s)
      the tie-break must be the task id, got {taskID:t-tie-zzz status:superseded}
  --- FAIL: TestArchiveCLIRefusesALiveOwnerDespiteACancelledSibling (5.91s)
      a LIVE run's ground was archived by hand (exit 0):
      archived task/01a113e467767eb9a4c33cb1df1cdfbb as archive/task/… 
        owner:  task 01a114aa-eb19-dead (cancelled)
  ```
- **Green with the fix**: same three tests pass; `TestArchive…` across both
  packages → `ok`.

### Ran: (Rule 31 — the surface changed, executed with real argv)

Two binaries were built from ONE worktree, differing only in
`internal/tools/archive_reason.go` (master's version vs the fixed version), and
each was driven through the operator's own command. Script:
`~/.gopi/sessions/01a1167e-38c3-70e0-be1c-f6ed304fe851/artifacts/real-verb-check.sh`;
transcripts `real-verb-PREFIX.txt` / `real-verb-FIXED.txt` beside it. Fixture:
temp git repo (`master` + the incident's branch name + `feat/finished`), temp
`GOPI_HOME`, temp ledger, rows written through `gopi task submit` /
`gopi task update` (legal state machine: pending → cancelled, pending →
in_progress), the deliverable's branch stamped in the executor's JSON shape.

```
# PRE-FIX binary
$ gopi task archive task/01a113e467767eb9a4c33cb1df1cdfbb --reason hand-mistake
archived task/01a113e467767eb9a4c33cb1df1cdfbb as archive/task/01a113e467767eb9a4c33cb1df1cdfbb
  reason: hand-mistake
  owner:  task 01a114aa-eb19-7ce3-8333-d29df8e65e55 (cancelled)      ← the incident
### exit=0
### git tag -l 'archive/…' -> [archive/task/01a113e467767eb9a4c33cb1df1cdfbb]   ← the live ground tagged

# POST-FIX binary (same repo, same fixture, same command)
$ gopi task archive task/01a113e467767eb9a4c33cb1df1cdfbb --reason hand-mistake
archive refused: task 01a113e4-6799-70e0-be1c-f6ed304fe851 owns this branch and is
"in_progress" — the work is IN FLIGHT. Archiving it buries a running deliverable;
wait for the row to conclude (or cancel it deliberately).
### exit=1
### git tag -l 'archive/task/…' -> []                                       ← empty
### reason record -> [fatal: Not a valid object name refs/gopi/archive-reason/…]
### branch refs -> [feat/finished, master, task/01a113e467767eb9a4c33cb1df1cdfbb]  ← ref intact

# POST-FIX control arm (a decided owner + the override — must still work)
$ gopi task archive feat/finished --reason superseded --override-completed
archived feat/finished as archive/feat/finished
  owner:  task t-finished (completed)
### exit=0
### tags now -> [archive/feat/finished]
```

## Evidence (self-filed)

- Red on master's source: `go test ./internal/tools/ -run 'TestArchiveBranchOwners' -count=1`
  → `FAIL github.com/nick/gopi/internal/tools` with the three lines quoted above (the file was
  restored from `git show master:internal/tools/archive_reason.go`; the test files were untouched).
- Red through the binary on master's source: `.../artifacts/gopi-prefix task archive <branch> --reason hand-mistake`
  → `exit=0`, owner line read `(cancelled)`, `git tag -l` printed the tag.
- Green through the binary on the fixed source: same command → `exit=1`, IN FLIGHT line,
  `git tag -l` empty, no reason record, branch ref intact.
- Green tests: `go test ./internal/tools/ ./cmd/agent/ -run 'TestArchive' -count=1` → `ok … 5.140s`, `ok … 13.298s`.
- Work dir device: `stat -c %d` on the evidence dir → 2052, `/` → 2051 (Rule 22: the fixture is off the `/` partition).
- No ledger semantics changed: `archiveOwnerDecided` / `archiveOwnerArchivable` are byte-identical,
  and the status predicates/constants they read (`TaskStatus*`, `taskSchemaStatusCHECK`) are untouched.

### The budget run, and the failures it reported (delivery-point sync included)

The first `make test-budget PKGS='./internal/tools ./cmd/agent'` run (stream
`run-1791381553-333514.json`, `exit=1`) reported 31 failing tests. **None of them
is in the changed code**, and two classes explain all but zero of them:

1. **The run raced my own edits.** I re-edited `archive_reason.go` (comment
   wording) while the suite was running, so eleven e2e tests that assert the
   tested binary is NEWER than the tree's sources failed by construction —
   `task_ground_e2e_test.go:64` / `task_executor_repo_cwd_e2e_test.go:106` /
   `rawprompt_worktree_release_e2e_test.go:106` /
   `task_session_cwd_e2e_test.go:126,298` / `write_tool_ownership_e2e_test.go:171`,
   each quoting `gopi (mtime 21:59:15) is OLDER than …/archive_reason.go (mtime 22:10:0X)`.
   That gate is working correctly; the fault was mine for editing mid-run.
2. **`TestSyncBarePeersTestCoversEveryChangedFile`** reads the branch's
   UNCOMMITTED diff and requires every changed file under `cmd/agent/` to be in
   its coverage lists. It fired on `cli_archive_test.go`, which is a file this
   branch legitimately changed for an unrelated reason (the epic's own CLI
   regression). It is a development-time ratchet that retires at the merge, so
   the fix is the commit, not a coverage-list edit — and after the commit the
   gate passes (re-run below).
3. The remaining failures (`TestSyncTaskIDRefusedOnEverySessionFace`,
   `TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses`, the three
   `TestTaskRunGround…` cases, `TestMergePromptCoversEveryRequiredSection`,
   `TestWorkerResumeKeepsTheGuardsIntact`, …) were measured on **master's own
   tree** in this same session: `git archive master | tar -x -C <session>/artifacts/master-export`
   then `go test ./internal/tools/ -run 'TestTaskRunGround'` there → the SAME
   three failures with identical text (`the task id is not a full uuid …`), and
   `go test ./cmd/agent/ -run 'TestSyncTaskIDRefusedOnEverySessionFace|TestChatCLIVocabularyIsClosed…'`
   in that export → `ok`. So the sync-face ones were the racy-binary window
   above, and the `TestTaskRunGround…` three are **pre-existing on master**
   (`ground_baseline_test.go` still passes non-uuid fixture ids to a ground
   resolver that now requires a full uuid — a master-side mismatch, not this
   branch's).
4. **The branch was 12 commits behind master** when the first run started
   (`git rev-list --count HEAD..master` → 12), so the failures also include
   master's own mid-flight state. The delivery-point sync was run
   (`git merge master`, no conflicts, `MERGE_EXIT=0`) and the budget re-run
   afterwards: **`cmd/agent` went fully green**, `internal/tools` kept exactly
   the three `TestTaskRunGround…` failures that the master export reproduces
   with identical text — i.e. the delta this branch introduces is **0 failures**,
   and what remains is master's own.

### The master-export measurement (the delta, not an absolute)

```
git archive master | tar -x -C ~/.gopi/sessions/01a1167e-…/artifacts/master-export
cd <master-export> && go test ./internal/tools/ -run 'TestTaskRunGround' -count=1
--- FAIL: TestTaskRunGroundCutsTheBaseFromThePeerTip (0.08s)
    task_ground_baseline_test.go:163: TaskRunGroundFor: cannot resolve the ground for task
    t-ground-base-peer … the task id is not a full uuid   ← SAME text, master's own tree
--- FAIL: TestTaskRunGroundDegradesExplicitlyWhenThePeerIsUnreachable
--- FAIL: TestTaskRunGroundSkipsThePeerFetchOnTheServerRole
FAIL github.com/nick/gopi/internal/tools  0.750s
cd <master-export> && go test ./cmd/agent/ -run 'TestSyncTaskIDRefusedOnEverySessionFace|TestChatCLIVocabularyIsClosed…'
ok  github.com/nick/gopi/cmd/agent  4.368s
```

## Files

- `internal/tools/archive_reason.go` — `archiveOwnerRank` + `archiveOwnerSafer` + the ranked pick and its measurement.
- `internal/tools/archive_owner_pick_test.go` — the two new regression tests (+ the shared `newArchiveOwnerLedger` helper).
- `cmd/agent/cli_archive_test.go` — `TestArchiveCLIRefusesALiveOwnerDespiteACancelledSibling`.
- `devdoc/archive-owner-precedence.md` — this report.
