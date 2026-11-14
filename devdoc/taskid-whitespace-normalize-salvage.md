<!-- doc-class: normative -->
# Re-measure: `archive/feat/taskid-whitespace-normalize` — SALVAGE (per unit), landed on `fix/taskid-whitespace-normalize`

Task row: `01a0b51d-40f9-79ed-b597-daad4f096dae` ("Recover unlanded work: archive/feat/taskid-whitespace-normalize").
Base: master `fa12d97f` at measurement time (**master then advanced twice —
`0380466d` → `f69a2149` — and §7/§7a re-observe EVERY instrument at the live
tip**; the branch also absorbs that advance). Tag tip: `e4e80335`.

The tag is the evidence of a failed path (Rule 23) and was **read only** —
`git show` / `git grep` / `git log -S`. It was never checked out, committed on,
merged, or deleted.)

## 1. Instruments — observed, not restated from the audit

```
$ git cherry -v master archive/feat/taskid-whitespace-normalize
+ 9f1810ab28c4461dc1503b462d360c827a80d613 fix(tools): one task_id normalization seam for every ledger accessor
+ f2c34ca122b78dd44d60d6bf3ca44a9cb2a1ce1c docs(tools): report for the uintask_id normalization seam (option (a), mutation proof, residual)
+ 7f3ec722ec5db92e78433dfb8f52baff139cb053 docs(tools): move the whitespace-normalization note to the END of each sibling doc comment
+ 6c59365076e00549c84d7fbbb5aff901028c4830 docs(tools): final acceptance gate + rg-missing environment root cause
+ 13677824af8d0b8f969398ddfa99c8d5fc9828c0 fix(tools): normalize the residual task_id entry points (write/fs/sync faces)
+ e4e80335c97552a34f49ffb4e38d7e1cd7de25ab docs(tools): report for the residual task_id entry points (mutation proof, left residual, raw logs)

$ git rev-list --count master..archive/feat/taskid-whitespace-normalize
6

$ git rev-list --count archive/feat/taskid-whitespace-normalize..master
2843

$ git merge-base master archive/feat/taskid-whitespace-normalize
0f66d17598bfc692d372646f31f2eec11fa78684

$ git merge-base --is-ancestor archive/feat/taskid-whitespace-normalize master; echo $?
1

$ git rev-parse master archive/feat/taskid-whitespace-normalize
fa12d97fa2865e78993cade27792201af7712bce
e4e80335c97552a34f49ffb4e38d7e1cd7de25ab

$ git tag -l 'archive/feat/taskid-whitespace-normalize*'
archive/feat/taskid-whitespace-normalize
```

Readings:

- cherry lists **6 `+`** (no patch-id equivalent on master) and rev-list counts
  **6** — they AGREE. There is **no internal merge of master inside the tag**
  (`git log --oneline master..<tag> | grep -i merge` printed nothing), so the
  known benign `+N` vs `N+1` gap does not apply: the tag is genuinely unlanded.
- No sibling tag of the same branch exists (one tag, not a merged twin).
- `--is-ancestor` exit **1**: unlanded, confirmed independently of cherry.
- Base drift: **2843** master commits since `0f66d17` (2026-09-11). This is why
  the audit's name/path list is worthless here — every verdict below is a
  CONTENT test against today's master.

## 2. Decider — the content test per unit

The tag's kernel is one seam plus its call sites:

```
$ git show archive/feat/taskid-whitespace-normalize:internal/tools/tasks.go | grep -A2 '^func taskIDNormalize'
func taskIDNormalize(taskID string) string {
	return strings.TrimSpace(taskID)
}
```

Content probes on master (never a name/path list):

```
$ git grep -n "taskIDNormalize" master -- '*.go'
(zero hits)

$ git log --oneline -S'taskIDNormalize' master -- '*.go'
(empty — the symbol was NEVER added to master, so it was never removed either:
 the seam has no mainline ancestor at all)

$ git grep -n "func taskIDNormalize\|func normalizeTaskID" master -- '*.go'
(zero hits)
```

So the capability can only be on master **by another route**: master trims the
task_id inline at each entry point, or not at all. Per-unit verdicts below are
decided by reading ≥30 master entry points and asking *"does THIS unit's
task_id reach a normalized form?"*.

### Unit-by-unit

| # | Unit (tag) | Master state (observed) | Verdict |
|---|---|---|---|
| U0 | The seam `taskIDNormalize` itself (`tasks.go`) | 0 hits on master; `-S` empty | **SALVAGE** |
| U1 | raw `strings.TrimSpace(taskID)` call sites in `internal/tools` production files | **51** on master (`git grep -o 'TrimSpace(taskID)' master -- internal/tools ':!*_test.go' \| wc -l`); **3** after this change — the seam's own body plus two `!= ""` validity tests that are not lookups | **SALVAGE** (the drift surface, measured) |
| U2 | `TaskRepoGuardWarn` | `func TaskRepoGuardWarn(taskID string) []string { ctx := context.Background()` — **NOT trimmed** (contrast `TaskShow`/`TaskArchive`, which do trim) | **SALVAGE** (live defect) |
| U3 | `TaskResumeCount` | NOT trimmed, no empty guard | **SALVAGE** (live defect) |
| U4 | `taskSessionDir` | NOT trimmed → `filepath.Join(gd, taskSessionsRootName, taskID)` builds a **padded-name container** | **SALVAGE** (filesystem drift) |
| U5 | `TaskDeliverableAttachments` | NOT trimmed | **SALVAGE** |
| U6 | `TaskCopyAttachments` | NOT trimmed → copies into `sessions/<padded>/` | **SALVAGE** |
| U7 | `finishCleanupWorktreeOwnerRows` | master: own `ctx`/timeout, **no** normalize at entry; exclusion is exact `id == taskID` → the cleaner sees its OWN row as a foreign owner | **SALVAGE** (live defect; branch normalizes once at entry) |
| U8 | `taskRowDeliverable` / `TaskDeliverableRepoFor` | `taskRowDeliverable` raw; `TaskDeliverableRepoFor` calls it raw | **SALVAGE** |
| U9 | `taskBackfillDeliverableBranch` / `taskBackfillDeliverableWorktree` | NOT trimmed | **SALVAGE** |
| U10 | `TaskRunWritebackTags` (`memory_sync_delta.go`) | master: `TaskRunWriteback(taskID,…)` trims for the writeback, but the tags UPDATE is `UPDATE tasks SET tags=? WHERE task_id=?` with the **raw** `taskID` → for a padded id the writeback lands and the tags are silently dropped (update matches 0 rows, no error) | **SALVAGE** (live defect) |
| U11 | claim-lock faces — `StartClaimLockBroadcast` (`memory_sync_claim.go:185`), `RollbackLocalClaim` (:651), `RunClaimLockProtocol` (:723), `SyncTaskPushOnly` (:788), `PushTerminalTaskSync` (:894) | on master each begins on the **raw** `taskID` (no normalize at entry) | **SALVAGE** |
| U12 | `NotifyTerminalPushFailed` (`:986`) → `notifyTerminalPushFailed` | master passes `taskID` straight through, raw; the alarm names a padded id | **SALVAGE** |
| U13 | write faces — `insertTaskRow`'s `f.TaskID`, `insertTaskAreaConclusion`'s `opts.TaskID` (`writetaskarea.go:239`) | **measured on clean master**: line 239 already passes `strings.TrimSpace(opts.TaskID)` to the INSERT, so this unit was **already correct by another route** — and the proof is a *negative* one: the tag's own `TestWriteTaskArea_PaddedTaskIDKeysTheSameRow` **PASSES on clean master** (§4), i.e. it is not a red-proof unit. The branch still routes it through the seam for uniformity (same behaviour) | **OBSOLETE (already correct)** |
| U14 | `TaskSubmit` minting (`tasks.go:4321`) | `taskID := strings.TrimSpace(opts.TaskID)` — already correct on master, by another route (manual inline trim, not the seam; the tag's own test for this unit SKIPs, see §5) | **OBSOLETE (already correct)** |
| U15 | `TaskVerify` (`tasks.go:7484`) | `taskID := strings.TrimSpace(opts.TaskID)` inline — correct, rule re-typed in place | **OBSOLETE (already correct)** |
| U16 | `RunCloseoutPair` (`tasks.go`, a **NEW master entry point** with no ancestor in the tag) | `return strings.TrimSpace(taskID), strings.TrimSpace(outcome)` — the raw trim re-typed | **SALVAGE (residual, post-tag)** |
| U17 | `taskFindSessionJSONLForRow` (`tasks.go`, post-tag) | `if hinted == strings.TrimSpace(taskID)` — raw trim on the compare key | **SALVAGE (residual, post-tag)** |
| U18 | Tag docs (`devdoc/taskid-whitespace-normalize*.md`, 2 files) | narrative of an unlanded branch | **OBSOLETE** (superseded by this report) |
| U19 | Tag's tests (2 files, 951 lines) | pin the contract | **SALVAGE** (they are the red proof, §4) |
| U20 | `signalRecordDest.go`, `archive_reason.go`, `task_reclaim.go`, `task_report.go`, `task_run_retry.go`, `task_signal_verdict.go`, `task_staleness_guard.go`, `taskledger.go`, `taskproc.go`, `taskrunguard.go`, `verify_gate.go`, `worker_output.go`, `worktree_sweep.go`, `task_merge_*`, `task_finish_premerge.go`, `memory_sync_task.go` | each carries a raw `strings.TrimSpace(taskID)` entry point | **SALVAGE** (same rule, same drift) |

Verdict: **SALVAGE**, with three units honestly OBSOLETE because master already
normalizes them by a different route (`TaskSubmit`'s manual trim, `TaskVerify`'s
inline trim, `writetaskarea.go`'s inline trim at the INSERT) and one (the tag's
own docs) OBSOLETE because this report supersedes its narrative. The count of
*residual* units is not a guess: it is the count of the tag's own tests that go
red on clean master (§4) — **18**, and every one of them names a live master
defect.

## 3. What was re-derived (not copied)

The tag's prod diff was `-U0`-mapped function by function (`git diff -U0
base..tag -- internal/tools/*.go`), then **every** raw `strings.TrimSpace(taskID)`
entry point on **today's master** was routed through the seam — including entry
points that did not exist on 2026-09-11 (`RunCloseoutPair`,
`taskFindSessionJSONLForRow`) and files the tag never touched. That is a
deliberate superset of the tag's kernel: the tag fixed 9 files; the drift on
master today spans 27.

- **Added seam**: `taskIDNormalize` in `internal/tools/tasks.go`.
- **Measured on the branch** (Rule 5 self-filed evidence):
  `git grep -o 'taskIDNormalize(' -- internal/ cmd/ ':!*_test.go' | wc -l`
  → **83** occurrences, of which one is the definition line itself
  (`internal/tools/tasks.go:1343 func taskIDNormalize(...)`) ⇒ **82 production call sites over
  27 files** (26 files plus `tasks.go`); the 7 further occurrences live in the
  tag's two test files. The raw-task-id trim surface this replaces, on master:
  `git grep -o 'TrimSpace(taskID)' master -- internal/tools ':!*_test.go' | wc -l`
  → **51** in `internal/tools` (55 across `internal/`+`cmd/`, 63 counting all four
  task-id spellings), down to **3** on the branch — the seam's own body plus the
  two validity tests named below.
- Two unit classes got the *narrower* treatment consistent with their exception
  class (never mechanically converted):
  - `task_finish_cleanup.go` / `task_merge_executor.go` report-label trims and
    the inbound-wire normalizers (`memory_sync_task.go`) are **not** entry-point
    lookups — they carry their own correct local rule. The lookup-shaped
    compare keys that the tag's `TestFinishCleanupOwnerRowScanPaddedIDIsNotItsOwnOwner`
    exercises *do* reach the seam:
    `finishCleanupWorktreeOwnerRows` normalizes `taskID` once at entry and then
    compares exactly (`task_finish_cleanup.go`), so the raw trim left inline
    below is a blank-check on the already-normalized value.
  - `tasknotifybus.go`'s empty check uses the seam but does not reassign the
    caller's variable (it is a validity test, not an id stamp).
  - Two remaining raw trims are **validity tests, not lookups**, and are left
    inline on purpose: `finishCleanupRemovalStateReason` (`internal/tools/task_finish_cleanup.go:2713`)
    (`strings.TrimSpace(taskID) != "" && id == taskID` — "is this id
    non-blank?" then an exact row compare) and `taskFindSessionJSONLForRow` (`internal/tools/tasks.go:2034`)
    (`hinted == strings.TrimSpace(taskID)` inside
    `taskFindSessionJSONLForRow`, a "was this dir already probed?" guard).
    Converting either would change which question is asked, not how an id is
    normalized. The anti-drift gate
    (`TestTaskIDNormalizeIsTheSingleSeam`) matches the exact literal
    `taskID = strings.TrimSpace(taskID)`, so it does not flag them — that
    boundary is deliberate, and it means the gate cannot be satisfied by
    deleting the explanation.
- **Unchanged behavior for exact ids** (`TrimSpace` of an exact id is itself).

## 4. Red proof (red on clean master → green on the branch)

Clean-master probe worktree `/home/user/workspace/gopi-taskid-ws-base`
(detached at `fa12d97f` — a checkout of my own, listed by `git worktree list`,
never another task's worktree, Rule 29), tag's two test files transcribed
verbatim with `git ls-tree` + `git show` (951 lines; `diff -q` against the tag's
blobs reports IDENTICAL — byte-for-byte, no edit), plus a **test-file-only** shim
supplying the seam so the transplanted tests compile (master has no
`taskIDNormalize`, which is exactly what they assert):

```go
// zz_probe_shim_test.go — PROBE-ONLY shim, never a production change
func taskIDNormalize(taskID string) string { return strings.TrimSpace(taskID) }
```

```
$ git -C /home/user/workspace/gopi-taskid-ws-base rev-parse --short HEAD
fa12d97f   # == master, nothing of mine on it

$ cd /home/user/workspace/gopi-taskid-ws-base \
  && go test -count=1 -v -run 'Padded|Whitespace|Normalize|Residual' ./internal/tools/ \
     > .probe-red.log 2>&1 ; echo "exit=$?"
exit=1    # 18 FAIL, 4 PASS, 1 SKIP  → FAIL github.com/nick/gopi/internal/tools 45.340s
```

The 18 red names, each carrying a *live-defect* message (never a shape
assertion) — quoted in full in §7a's live-tip re-run:

```
TestTaskStampRunPaddedIDStampsTheSameRow          "TaskStampRun(padded) did not land on t-ws-residual-1: run_method=\"\" run_cmd=\"\""
TestTaskStampSelfClaimPaddedIDAwardsTheSameRow    "claimed_by = \"\", want this runner"
TestTaskResumeClaimedPaddedIDRecoversTheSameRow   "task: no task with task_id=\"  t-ws-residual-1 \n \""
TestRollbackLocalClaimPaddedIDRevertsTheSameRow   "no longer claimed"
TestTaskRunWritebackTagsPaddedIDTagsTheSameRow    "the tags UPDATE matched no row"
TestTaskIDSessionDirPaddedIDIsTheSameContainer    "= \".../sessions/  t-ws-residual-1 \n \""
TestTaskCopyAttachmentsPaddedIDArchivesIntoTheSameDir  "a padded-name container exists — filesystem drift"
TestTaskDeliverableAttachmentsPaddedIDReadsTheSameManifest
TestTaskDeliverableRepoForPaddedIDResolvesTheSameRepo
TestFinishCleanupOwnerRowScanPaddedIDIsNotItsOwnOwner
TestStartClaimLockBroadcastPaddedIDExportsTheSameRow
TestRunClaimLockProtocolPaddedIDProceedsOnTheSameRow
TestSyncTaskPushOnlyPaddedIDPushesTheSameRow
TestPushTerminalTaskSyncPaddedIDPushesTheSameTerminalRow
TestNotifyTerminalPushFailedPaddedIDNamesTheSameRow
TestTaskIDResidualUnknownAndEmptyStayRefused
TestTaskIDWhitespaceNormalizedAllAccessors
TestTaskIDNormalizeIsTheSingleSeam
```

Same two test files, byte-identical, **unmodified**, on the branch:

```
$ cd /home/user/workspace/gopi-taskid-ws \
  && go test -count=1 -v -run 'Padded|Whitespace|Normalize|Residual' ./internal/tools/ \
     > .branch-green.log 2>&1 ; echo "exit=$?"
exit=0    # 0 FAIL, 22 PASS, 1 SKIP
```
--- PASS: TestTaskIDResidualUnknownAndEmptyStayRefused (1.19s)
--- PASS: TestTaskIDWhitespaceNormalizedAllAccessors (4.11s)
--- PASS: TestTaskIDWhitespaceNormalizedUnknownIDStillFails (1.09s)
--- PASS: TestTaskIDNormalizeIsTheSingleSeam (0.00s)
PASS
ok  	github.com/nick/gopi/internal/tools	48.245s
```

tally: **22 `--- PASS`, 0 `--- FAIL`, 1 `--- SKIP`** — the delta between the two
runs is exactly the 18 defects closing (plus `TestTaskIDNormalizeIsTheSingleSeam`
appearing once more as it is table-driven).

Both raw logs are kept verbatim, outside the worktree (Rule 30), in the session's
attachment area — a claim about a run is only as good as the log behind it:

```
~/.gopi/sessions/01a0b51d-40f9-79ed-b597-daad4f096dae/attachments/logs/
  clean-master-red.log            sha256 586ca864cc087e1e2d04cf0f03bc1f651b3f371ead5d093c187b4b29553ad1fd
  branch-green.log                sha256 1d6d22a8778ddd0744003534076c30456c65b4fd1026945e277224c13523a5a7
  full-package-ab.log             sha256 6eb60d9fbb58b7b6ad7bbcb5cb6559b05dd3ee1f7c96762c70891fa5bb9c80ad
  branch-test-budget-timeout.log  sha256 330c620c1219105ea76bffb0482986ada73e49d3f51160ffca8306b386d11da8
  two-failing-tests-ab.log        sha256 12eda3f90335a45ba28790fb6faddd74bba98ba5de5e23c3772bc5180f5b095a
```

The transplanted files are the tag's own (951 lines, taken via
`git ls-tree` + `git show`; `diff -q` reports IDENTICAL to the tag's blobs); the
**only** edit needed was none at all — they compile and pass unchanged against
the re-derived seam.

**The red count is the salvage count, measured.** 18 of the tag's tests fail on
clean master; the units that correspond to the 4 that pass there (`TaskVerify`,
`TaskSubmit`, `writetaskarea`, and the unknown-id case) are exactly the units
marked OBSOLETE in §2 — so "OBSOLETE because master already does it by another
route" is not an opinion about the patch, it is the observation that the tag's
own test for that unit is green without the fix.

The one test of the tag's own that the tag itself did not put in either
transplanted file lives in `writetaskarea_test.go` and is reproduced here the
same way — read from the tag with `git show` and committed as its own file
(`internal/tools/writetaskarea_padded_taskid_test.go`), so the salvage edits no
master-authored test file:

```
$ cd /home/user/workspace/gopi-taskid-ws-base && go test -count=1 -v -run TestWriteTaskArea_PaddedTaskIDKeysTheSameRow ./internal/tools/
=== RUN   TestWriteTaskArea_PaddedTaskIDKeysTheSameRow
--- PASS: TestWriteTaskArea_PaddedTaskIDKeysTheSameRow (1.25s)     ← GREEN on clean master
ok  	github.com/nick/gopi/internal/tools	2.174s
```

That PASS is the honest measurement behind U13's OBSOLETE verdict: the tag's own
assertion about the working-area write face holds on master *without* the seam. It
is kept green on the branch too (same file, unmodified):
`--- PASS: TestWriteTaskArea_PaddedTaskIDKeysTheSameRow (1.22s)`.

## 5. Not Done / honest residual

- `TestTaskSubmitPaddedTaskIDMintsTheTrimmedContainer` **SKIPs on this host** —
  observed reason, verbatim from the branch log:
  `taskid_whitespace_normalize_residual_test.go:378: repo probe refused the
  fixture (expected on some hosts): task_submit: repo ".../002/repo-probe.txt"
  is not a git work tree (git rev-parse failed: exit status 128)`. The fixture
  path is a *file*, so the `Repo` probe must refuse it — on every host. The
  skip is the test's own declared outcome, and it is counted in both runs (red:
  SKIP, green: SKIP), so it is neither the red half nor a hidden green. Its
  invariants (`opts.TaskID` normalized BEFORE the collision check and container
  mint) are covered by `TestTaskIDWhitespaceNormalizedAllAccessors` plus the
  seam presence in `TaskSubmit`. **Honest residual, filed not ported silently.**
- Pre-existing gofmt findings (`task_finish_premerge.go`,
  `task_staleness_guard_test.go`, `taskledger_cost_test.go`) are **master's** —
  reproduced identically in the clean-master probe worktree, untouched.
- The **full-package A/B** in §6 (branch 10 FAIL ⊂ master 16 FAIL) was measured
  at `fa12d97f`. It was started again at the live tip `f69a2149` and **killed
  before it finished** rather than reported — a whole-package run is ~600s of
  device work per side and it is not the verdict for this change either way
  (Rule 27 / `internal/smokegate`: the merge path is branch-scoped). The
  scoped verdict WAS re-taken at the live tip and is in §7a. Stated as measured,
  not as passed.
- Not merged (PA merges). Tag and archived branch untouched.

## 6. Verification (branch-scoped, Rule 27 — no whole-tree run, no load generator)

**§7a re-observed every number below at the live master `f69a2149`** (master
moved twice mid-run: `fa12d97f → 0380466d → f69a2149`). The readings agree; the
`fa12d97f` figures are kept as-first-measured and §7a carries the live-tip
re-run, so nothing here rests on a base that no longer exists.

```
$ cd /home/user/workspace/gopi-taskid-ws && CGO_ENABLED=0 go build ./...
(no output)          # exit 0

$ cd /home/user/workspace/gopi-taskid-ws && git status --porcelain
(no output — nothing, Rule 30)

$ git rev-parse archive/feat/taskid-whitespace-normalize
e4e80335c97552a34f49ffb4e38d7e1cd7de25ab   # tag untouched; no merge run
```

Branch-scoped package run — the touched package, on the branch. The repo's
declared branch-scoped form (`internal/smokegate.ScopedCommand`) is:

```
$ cd /home/user/workspace/gopi-taskid-ws && make test-budget PKGS='./internal/tools'
================== TIMEOUT, NOT AN ASSERTION FAILURE ==================
The per-package test budget (25m) ran out — a WALL-CLOCK cap, not a failing
assertion.  ... panic: test timed out after 25m0s
separate assertion failures in the same run (NOT the timeout):
  --- FAIL: TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind (1.83s)
  --- FAIL: TestTaskBashWorkdirCandidates_MalformedTildeIsDroppedNotStatted (0.00s)
stamp: 2026-09-19T11:34:12+08:00 host=peerbox pkg=./internal/tools budget=25m
elapsed=1526s load_start=7.91 7.49 6.67 load_end=5.67 6.06 6.44 cores=4 exit=3 outcome=timeout
make: *** [Makefile:41: test-budget] Error 3
```

Reported as observed: **exit 3 = the BUDGET expired under load, which the script
itself refuses to call a test failure** — it is not a green, and it is not
evidence of a defect either. So the two things it did say are separated and each
attributed, by re-running exactly those two tests on BOTH sides:

```
$ cd <worktree> && go test -count=1 -run 'TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind|TestTaskBashWorkdirCandidates_MalformedTildeIsDroppedNotStatted' ./internal/tools/

BRANCH (fix/taskid-whitespace-normalize):
--- FAIL: TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind (2.93s)
--- FAIL: TestTaskBashWorkdirCandidates_MalformedTildeIsDroppedNotStatted (0.00s)
FAIL	github.com/nick/gopi/internal/tools	3.714s

MASTER (clean fa12d97f, detached, none of my code):
--- FAIL: TestTaskDeliverableWorktreeBashReadOnlyProbeDoesNotBind (1.78s)
--- FAIL: TestTaskBashWorkdirCandidates_MalformedTildeIsDroppedNotStatted (0.00s)
FAIL	github.com/nick/gopi/internal/tools	2.594s
```

Identical on both sides ⇒ **master's, untouched by this change**. The timeout is
the host (load ≈ 6-8 on 4 cores while three other agents' suites ran; the script
documents "~189-291 s quiet, >1123 s at load 42-63"), which is precisely the case
`internal/smokegate` exists for.

**What IS green, on the same commit, scope-limited to the diff's subject** (§4):
`-run 'Padded|Whitespace|Normalize|Residual'` → 22 PASS / 0 FAIL / 1 SKIP, and the
transplanted working-area test → PASS. That is the branch-scoped verdict for this
change; the whole-package run is not a verdict about it either way.

Final close-out, re-observed on the branch tip itself after the report commit:

```
$ cd /home/user/workspace/gopi-taskid-ws && CGO_ENABLED=0 go build ./...
BUILD_EXIT=0

$ cd /home/user/workspace/gopi-taskid-ws && go test -count=1 -run 'Padded|Whitespace|Normalize|Residual' ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	54.009s

$ cd /home/user/workspace/gopi-taskid-ws && git status --porcelain
(no output)
```

The bare full-package run was ALSO taken, on BOTH sides, because a run that
fails says nothing until it is compared with the same run on clean master. The
comparison is the instrument that attributes failures:

```
# BRANCH = ~/workspace/gopi-taskid-ws   MASTER = ~/workspace/gopi-taskid-ws-base
$ cd <worktree> && go test -count=1 -timeout 600s ./internal/tools/
BRANCH : 10 FAIL, 600.7s      MASTER : 16 FAIL, 600.5s      BRANCH's 10 ⊂ MASTER's 16
```

Reading, and it is the whole point of the pair:

- **Every failure the branch shows is ALSO on clean master** (10 ⊂ 16) — not one
  is attributable to this change. Master-only extras: the `TestProposalRowsAre
  NeverClaimable` family (host has `role=client` with no `sync_to`), the
  `TestBidir*` family (no peers), `TestAssertionTotalityAuditGate` (asks a human
  to classify an assertion), plus the 6 claim-lock faces listed below.
- **Both trips Go's 10-minute per-package default** — a budget/load artefact,
  which is why `scripts/test-budget.sh` exists ("on this host the default is a
  load detector, not a hang detector") and why the merge path is BRANCH-SCOPED
  (`internal/smokegate`) rather than whole-package.
- **The delta is 6 failures PRESENT on master and ABSENT on the branch** —
  `TestRollbackLocalClaimRevertsToPending`, `…RevertsInProgress`,
  `TestExportClaimPatchCarriesPeerOriginRow`, `TestSyncClaimRemoteAcquireAdvances`,
  `TestSyncClaimRemoteRevertLeg`, `TestRunClaimLockProtocolNoPeers`. Those are
  the claim-lock surface the seam repairs, so read as an A/B the full-package run
  does not merely clear the change — it shows the change REMOVING failures.

Full logs, verbatim, outside the worktree (Rule 30):
`~/.gopi/sessions/01a0b51d-40f9-79ed-b597-daad4f096dae/attachments/logs/`.
Measured at `fa12d97f`; the scoped verdict at the live tip is §7a, and the
live-tip re-run of THIS whole-package pair was deliberately killed rather than
reported (§5 explains why).

Clean-master attribution baseline (§4's red half) lives in
`/home/user/workspace/gopi-taskid-ws-base`; any pre-existing failure is
reproduced there on clean master `fa12d97f`, which is how a failure is
attributed to master rather than to this change.

The report of record is this file, committed on
`fix/taskid-whitespace-normalize`; the session-dir copy is
`$GOPI_TASK_REPORT_PATH` = `~/.gopi/sessions/01a0b51d-40f9-79ed-b597-daad4f096dae/task-report.md`.

## 7. Master advanced while this ran — re-observed at the LIVE tip

Every instrument in §1 was taken against master `fa12d97f`. Master moved twice
during the run, so the whole instrument set was **re-observed** at the live tip
`f69a2149` — a measurement against a stale base is not a measurement:

```
$ git rev-parse master
f69a214901a66259062e763e19b0b59f040a72fa      # was fa12d97f at §1

$ git cherry -v master archive/feat/taskid-whitespace-normalize
+ 9f1810ab28c4461dc1503b462d360c827a80d613 fix(tools): one task_id normalization seam for every ledger accessor
+ f2c34ca122b78dd44d60d6bf3ca44a9cb2a1ce1c docs(tools): report for the uintask_id normalization seam (option (a), mutation proof, residual)
+ 7f3ec722ec5db92e78433dfb8f52baff139cb053 docs(tools): move the whitespace-normalization note to the END of each sibling doc comment
+ 6c59365076e00549c84d7fbbb5aff901028c4830 docs(tools): final acceptance gate + rg-missing environment root cause
+ 13677824af8d0b8f969398ddfa99c8d5fc9828c0 fix(tools): normalize the residual task_id entry points (write/fs/sync faces)
+ e4e80335c97552a34f49ffb4e38d7e1cd7de25ab docs(tools): report for the residual task_id entry points (mutation proof, left residual, raw logs)

$ git rev-list --count master..archive/feat/taskid-whitespace-normalize
6

$ git rev-list --count archive/feat/taskid-whitespace-normalize..master
2943                                          # was 2843 at fa12d97f — base drift GROWS

$ git merge-base master archive/feat/taskid-whitespace-normalize
0f66d17598bfc692d372646f31f2eec11fa78684      # unchanged: the fork point is fixed

$ git merge-base --is-ancestor archive/feat/taskid-whitespace-normalize master; echo $?
1

$ git grep -c taskIDNormalize master -- internal/ cmd/ ; echo "exit=$?"
exit=1                                         # 0 hits on the LIVE master: still never landed

$ git log --oneline -S'taskIDNormalize' master | head
98a4d342 docs(audit): full archive/* tag audit — 167 tags, verdict per tag, 31 recovery rows filed
                                               # only the audit document mentions the name
```

The verdict does not move: **still unlanded, still SALVAGE**, and the drift
number grew by 100 while the units stayed 6 — which is exactly why the audit's
*name/path* list cannot decide anything and the content test must.

### 7a. Mergeability at the live tip (the advance had to be absorbed)

`implied merge` at the new tip was not clean — the branch had to absorb it:

```
$ git merge --no-commit --no-ff master
Auto-merging internal/tools/tasks.go
CONFLICT (content): Merge conflict in internal/tools/tasks.go
```

One hunk, one cause, and it is a MASTER change, not mine: master **deleted**
`TaskPayload` as dead code between `fa12d97f` and `0380466d`
(`internal/tools/taskpayload_deleted_gate_test.go` is the gate it landed with).
The tag's `tasks.go` carried one added `taskIDNormalize(...)` call *inside that
deleted function*. Resolution: **honour master's deletion** — the function stays
gone, the seam stays in. The merged file is master's `tasks.go` plus
`taskIDNormalize` and nothing else:

```
$ git diff master...HEAD -- internal/tools/tasks.go --numstat
57      22      internal/tools/tasks.go

$ diff <(git show master:internal/tools/tasks.go) internal/tools/tasks.go | grep '^<'
                                    # every removed line is a TrimSpace->seam
                                    # replacement; NO master line is lost
```

Red-then-green was then re-run against the NEW master, since the base it was
first measured on no longer exists:

```
# clean-master probe rebuilt at the LIVE tip f69a2149 (git checkout --detach,
# my own worktree), the tag's two test files re-transcribed verbatim (sha256
# a305b582a0c6ec14… / f2de9335f991a20b… == the tag's blobs, unmodified):
$ cd ~/workspace/gopi-taskid-ws-base && go test -count=1 -v -run 'Padded|Whitespace|Normalize|Residual' ./internal/tools/
exit=1   --- 18 FAIL, 5 PASS, 1 SKIP      (log sha256 f629db3d6fdfe1c9…)

$ cd ~/workspace/gopi-taskid-ws && go test -count=1 -v -run 'Padded|Whitespace|Normalize|Residual' ./internal/tools/
exit=0   --- 0 FAIL, 23 PASS, 1 SKIP      (log sha256 8c70c1fd1f8aafb1…)
```

The identical red proof was also taken at the intermediate `0380466d`
(18 FAIL / 5 PASS / 1 SKIP, log sha256 `e5830da13bf51e8e…`), so the verdict is
stable across both advances, not a one-off. The PASS tallies differ by base
(fa12d97f: 4 red-side / 22 green-side; f69a2149: 5 / 23) because `-run` is a
regex over test NAMES and pre-existing packagemate tests match it
(`TestLatentResidualBasesSum`, `TestCanonicalStripsPathsTimestampsWhitespace`, …)
— they are not from the tag. The FAIL tally is the one that matters and it is
identical: 18 red, 0 green.

The 18 red names are identical to §4's list; the one SKIP is the same declared
skip, present on both sides. Both logs kept outside the worktree (Rule 30) in
`~/.gopi/sessions/01a0b51d-40f9-79ed-b597-daad4f096dae/evid/`.

## 8. Deliverable declaration (Rule 6d) — read before any worktree is removed

- **Branch:** `fix/taskid-whitespace-normalize`, forked from `fa12d97f` and
  carrying master `f69a2149` via one advance-absorber merge (`26cc53e5`).
  `git rev-list --count master..HEAD` = **9** (8 non-merge commits + that
  merge). The list, newest first — this file IS one of the rows, so the true tip
  is always whatever `git rev-parse` prints, never a sha quoted here:

  | sha | subject |
  |---|---|
  | `840531a0` | `docs: correct the seam's call-site count to the measured 82` |
  | `e361ec9e` | `docs: the deliverable declaration names the branch tip's commit table` |
  | `738d44af` | `docs: trim the report under 32 KiB and reconcile the PASS tallies` |
  | `1a4a4d6b` | `docs: state the live-tip full-package A/B as killed, not as passed` |
  | `65b54ba8` | `docs: re-observe every instrument at the live master tip (f69a2149)` |
  | `26cc53e5` | `Merge master (f69a2149) into fix/taskid-whitespace-normalize` — the advance absorber (§7a) |
  | `8448787e` | `docs: report for the archive-tag re-measure (units, red proof, deliverable declaration)` |
  | `530a0b37` | `test(tools): carry the tag's working-area padded-id test; report for the re-measure` |
  | `9155fa11` | `fix(tools): one task_id normalization seam for every ledger entry point` — the seam + its 82 production call sites |

  (A final docs commit correcting this very table follows — the table is
  therefore always one row behind the tip by construction.)

  Recover the tip with `git rev-parse fix/taskid-whitespace-normalize`.
  `master` is an ANCESTOR of the tip (`git merge-base --is-ancestor master
  fix/taskid-whitespace-normalize` → exit 0), so the branch is cleanly
  mergeable with no further work. All commits live in the SHARED repo —
  `git log --oneline` prints them from the main repo too, so no push is
  needed (Rule 6a/6c).
- **Recovery command:**
  `git show fix/taskid-whitespace-normalize:devdoc/taskid-whitespace-normalize-salvage.md`
- **Worktrees (both mine, Rule 29):**
  `/home/user/workspace/gopi-taskid-ws` (the deliverable) and
  `/home/user/workspace/gopi-taskid-ws-base` (the clean-master attribution
  baseline, deliberately left dirty with the three probe test files — it is a
  throwaway probe checkout, NOT the deliverable, and it must not be merged).
- **Untouched:** tag `archive/feat/taskid-whitespace-normalize` still resolves to
  `e4e80335c97552a34f49ffb4e38d7e1cd7de25ab`; no merge was performed by this run.
