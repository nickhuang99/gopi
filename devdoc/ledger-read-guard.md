<!-- doc-class: historical -->
# Task Report: the ledger a PURE READ was building — `TaskForBranch`'s opener created a knowledge base

**Task:** `01a0dc077cab7c22bfb8260eb6968c62` — branch `task/01a0dc077cab7c22bfb8260eb6968c62`
**Host:** peerbox (Rule 28: system layer) · **Date:** 2026-09-26

## Completed

1. **Root cause, with the mechanism MEASURED** (not inferred) — §"Root cause".
   The blocked chain the epic named is real, and its bottom is one frame deeper
   than the briefing guessed: the parked syscall is inside `enableWALWithSync`
   (the schema build's journal commit), not `sql.DB.Close`.
2. **The fix** — `internal/tools/ledger_read_guard.go` (new file): a pure-read
   ledger lookup answers from an ABSENT ledger without opening — and therefore
   without CREATING — it. `TaskForBranch` consults it before any opener.
3. **Regression tests, red before / green after, at both ends of the path** —
   `internal/tools/task_for_branch_test.go` (the lookup) and
   `cmd/agent/merge_ledger_read_cost_test.go` (new: the merge pass). The cost
   bound is ONE exported constant (`tools.TaskForBranchReadOnlyLookupBudget`),
   referenced by both packages (Rule 14) — never a second copy of the number.
4. **Rule 31 — the changed surface was executed through the real CLI**, with a
   before/after anyone can repeat: see `## Verification` → `Ran`.
5. **A regression this change INTRODUCED was caught by the suite and fixed**,
   with its own guard — §"The regression this change introduced".

## Root cause

`TaskForBranch` (`internal/tools/task_for_branch.go`) documents itself as
*"a PURE READ of the ledger: the SQL is a single SELECT, the whole scan runs
inside one read transaction, and no statement in this file writes"*. Its opener
broke that promise:

```
TaskForBranch → openTasksDB → ensureKnowledgeBase → initKnowledgeBaseOnce
  → initKnowledgeBase → enableWALWithSync   ← the parked syscall
  → ensureSchema + ensureFeedbackSchema + ensureTasksSchema
```

`openTasksDB` sees a MISSING file and initializes a whole knowledge base. So the
merge pass's question — *"does a live task still own this branch?"* — paid for a
full schema build and then read nothing from it.

`cmd/agent` reaches it per branch per merge pass:
`TestMergeTakesTheSingleInstanceLock → runMergePass → mergeSweepRepo →
mergeLiveOwnerParks → TaskForBranch(branch, "")`.

### The measured evidence

**(a) The stack, from a SIGQUIT dump of a hung run** (`run.log:5060+`) — the
briefing's chain, resolved one frame further:

```
modernc.org/libc.Xfsync → _unixSync → _syncJournal → *PagerCommitPhaseOne
← enableWALWithSync (internal/tools/memory.go:684, the PRAGMA loop)
← initKnowledgeBase ← initKnowledgeBaseOnce ← ensureKnowledgeBase
← openTasksDB (internal/tools/tasks.go:1271)
← TaskForBranch (task_for_branch.go:194)
← mergeLiveOwnerParks (cmd/agent/merge_run.go:954)
← mergeSweepRepo ← runMergePass ← ... ← TestMergeFast... (merge_fast_test.go)
```

**(b) The cost, by device, with the live merger + 5 workers running** —
`gopi task list --db <fresh path>` drives the same first-open path:

| first open of a FRESH ledger | mean (20 runs) | max |
|---|---|---|
| ext4 `/home` (device 2052 — the suite's own scratch device) | **839 ms** | **2211 ms** |
| `/dev/shm` (memory fs) | 18 ms | 22 ms |
| that same ext4 ledger, re-opened once built | 9 ms | — |

Identical work in every row; only the device differs. Each DDL commit pays a
durability cost on the **shared** journal — the mechanism
`internal/tools.firstOpenSyncMode` already documents (`jbd2_log_wait_commit`, a
queue every writer on the device shares) — so a ~0.5 s cost on an idle host
stretches without bound while the merger and workers commit.

**(c) The waiting, observed live on a running suite** (`live-evidence.txt`):

```
tid=3130947 state=D wchan=jbd2_log_wait_commit
tid=3121940 state=D wchan=jbd2_log_wait_commit
```

Two threads in uninterruptible `D` state, parked on the journal-commit queue —
sampled twice, minutes apart, while the merger ran.

### The test-scoped half (Rule 32)

With an empty `dbPath`, the resolution falls back through `memoryDBPath()` to
the operator's own `~/.gopi/memory.db` — an APPEND-ONLY file. `mergeLiveOwnerParks`
passes `""` by design (the tools layer resolves the ledger), so the suite's
protection is its fixture pin, not the empty argument. The defect is two-sided
and **both sides are now closed**: a read no longer creates the file at
whatever path it resolved (the fix), and that path is the suite's own scaffold
pin (Rule 32) rather than the operator's ledger.

### What was NOT the cause (checked, so nobody re-follows it)

The briefing's `sql.DB.Close (blocked ~8min)` frame is not where the cost is:
`Close` appears in the dump only as an ordinary defer below the parked frame.
The block is the `Xfsync` inside the PRAGMA loop. A fix aimed at `Close` would
have addressed nothing.

## The regression this change introduced (and how it was caught)

The suite caught my first cut **by failing**, which is the point of running it:

```
cli_tasks_help_surface_basic_test.go:574:
  an unreadable ledger: exit = 4, want 1 (never the no-owner answer)
```

The first cut answered "no owner" for a path that is *fundamentally
unreadable* — its parent is a regular FILE (`<file>/memory.db` can neither be
opened nor created). That collapsed two facts Rule 15 keeps apart: "there is no
ledger" (empty answer) and "I could not look" (refusal). The probe CLI's own
exit-code vocabulary already named the distinction (`TaskForBranchExitNoOwner`
vs exit 1).

The fix narrows the guard to the exact property that separates them, and the
deciding predicate is now `ledgerParentIsNotYetADirectory`:

| the ledger file | its parent | answer |
|---|---|---|
| absent | existing directory | **empty** — "not built yet"; never build it |
| absent | does not exist yet | **empty** — and this is the safer direction: falling through would let a READ create a directory tree |
| absent | a regular file | **fall through** to the opener → its error is the honest report |
| absent | stat fails for any other reason | **fall through** — only `fs.ErrNotExist` earns the empty answer |
| exists | — | the opener decides (unchanged behaviour) |

`TestLedgerReadGuardIsTheDecidingSeam` now pins the blocker case directly, with
a fixture that asserts the path is ABSOLUTE — the second cut of this guard got
it wrong by treating an absolute path as proof of readability, which is why the
case is pinned rather than reasoned about.

## Not Done (honest)

- **Acceptance (b) is NOT met on this host, and this report does not claim it
  is.** The epic asks for the package green "well under 2 minutes" while a live
  merger + worker run. Measured with the fix in place and the merger + workers
  live: **1404 test entries, 647.3 s of in-test time, and the package hit its
  10-minute timeout** (`panic: test timed out after 10m0s`, with
  `TestVoiceFileModeTranscribesViaStubEngine` (21 s) in flight and 845 tests
  already green, 0 failures). The package's own work sums to more than the
  budget before any overhead, so the timeout is structural on this host, not a
  hang: this change removes a real per-branch cost but it is not the package's
  dominant cost.
  - The dominant costs are child-process and binary-building tests:
    `TestToolsMaintenanceHelpSurfaceFormsAreTyped` 28.2 s,
    `TestVerbCensusMatchesTheDispatch` 15.3 s,
    `TestMemorySyncStalePeerUpgradeHint` 13.6 s,
    `TestManualRunKeepsWorktreeHeldByForeignProcess` 10.6 s,
    `TestMemorySyncE2EBidirectional` 10.3 s.
  - **Honest statement: the read-path defect is fixed and proven; the package's
    wall clock under load is a larger, separate problem**, filed with these
    numbers as task `01a0dc30-…` (see below). A change that "fixes" the timeout
    by raising it, or by skipping the slow tests, is not a fix (Rule 27/30).
- **A second, independent hang was measured in the same package and is filed
  separately** (`task 01a0dc30-fa1f-7e35-9419-08309147dd32`):
  `TestMemorySyncEveryLoopE2EConvergesAcrossRounds` once timed out at 100 s while
  blocked in `os/exec` `Waitid` on a child — NOT on the ledger. In a clean run it
  passed in 9.6 s, so that timeout was load-induced; the task is filed so the
  mechanism is found rather than guessed.
- **One measurement is void and is reported as void, not as a result:** an
  earlier post-fix full run (exit 143) was killed by my own `kill` of a process
  I had mistakenly taken for an orphan. It is not counted anywhere.
- The `Close` frame named in the epic brief was not reachable as a *cause*; if a
  blocking `Close` exists on some other path, this run found no evidence of it.

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; the
  freshness comparison the suite itself makes
  (`find cmd internal -name '*.go' ! -name '*_test.go' -newer bin/gopi`) prints
  nothing, i.e. the binary post-dates every production source.
- **Tests:**
  - `go test ./internal/tools/ -run 'TestTaskForBranch|TestLedgerReadGuard' -count=1`
    → `ok github.com/nick/gopi/internal/tools 14.666s`.
  - `go test ./cmd/agent/ -run 'TestTaskVerbForBranchRunsAsAdvertised' -count=1`
    → `ok` (this is the test whose failure found the regression of §"The
    regression…"; it drives the BUILT binary through `exec.Command`).
  - `go test ./cmd/agent/ -run 'TestMergePass|TestMergeLiveOwner|TestMergeUnreadable|TestMergeStatusReports|TestMergeFast' -count=1`
    → `ok github.com/nick/gopi/cmd/agent 19.774s`.
  - Neighbourhood, no weakening: `go test ./cmd/agent/ -run 'TestMergeLiveOwner|TestMergeUnreadableLedger|TestMergeStatusReports' -count=1`
    → `ok 23.782s`.
- **RED → GREEN (the fix's own proof).** With the guard's deciding branch
  forced off (`return false // RED-PROOF ONLY`), the new tests fail on the
  defect itself:
  - `TestTaskForBranchDoesNotCreateTheLedgerItReads` → *"the READ path CREATED
    …/memory.db"*.
  - `TestTaskForBranchAnswersAnAbsentLedgerUnderHardTimeBound` → *970.25 ms
    (budget 150 ms)*.
  - `TestMergePassWithNoLedgerDoesNotBuildOne` → *created the ledger* + *1.154 s*.
  - `TestMergeLiveOwnerParksAnswersWithoutBuildingTheLedger` → same, under a
    live merger + worker.
  With the guard restored: green (log: `redgreen.log`).
- **Ran:** the surface itself — `gopi task for-branch`, whose handler calls
  `TaskForBranch` (`cmd/agent/cli_tasks.go:4604`) — verbatim, with real
  arguments:

```
# PRE-FIX binary (13:06, built before the patch):
$ bin/gopi task for-branch feat/nobody-at-all --db <sandbox>/absent/memory.db
no owner: no ledger row names branch feat/nobody-at-all
  (0 rows carry a branch; none names this one)
LEDGER CREATED: YES            ← a pure READ built a whole knowledge base

# POST-FIX binary (rebuilt after the patch):
$ bin/gopi task for-branch feat/nobody-at-all --db <sandbox>/absent2/memory.db
no owner: no ledger row names branch feat/nobody-at-all
  (0 rows carry a branch; none names this one)
LEDGER CREATED: NO             ← the same answer, and no write

# POST-FIX, a REAL one-row ledger — the write path still creates ledgers:
$ bin/gopi task submit "cli probe row" --db <sandbox>/memory.db
Task queued: 01a0dc2f-a0df-7158-b4c7-d6ca16833a6f (status=pending, source=agent)
$ bin/gopi task for-branch feat/nobody-at-all --db <sandbox>/memory.db
no owner: no ledger row names branch feat/nobody-at-all     (exit 0)
```

- **Result:** the identical answer (`no owner`, exit 0) with the file no longer
  created, and the lookup's cost down from a measured 839 ms-mean / 2211 ms-max
  schema build (18 ms in `/dev/shm`, i.e. device-bound) to a file-existence
  check — the merge-pass guard's RED measured 1.15 s and its GREEN is inside the
  150 ms budget on the same loaded host.
- **Commit:** the branch tip that carries this report.

## Evidence (self-filed)

- `D-state threads, live`: `for t in /proc/<suite>/task/*; do s=$(awk '{print $3}' $t/stat); [ "$s" = D ] && echo "$(basename $t) $s $(cat $t/wchan)"; done`
  → `tid=3130947 state=D wchan=jbd2_log_wait_commit` (twice, minutes apart);
  archived in `live-evidence.txt`.
- `First-open cost by device, 20 runs each, live merger + 5 workers`:
  `ext4 total=16776 ms over 20 opens, max=2211 ms` ·
  `tmpfs total=355 ms over 20 opens, max=22 ms`.
- `Device ids (Rule 22 — measured, not a path spelling)`:
  `stat -c '%d %n'` → `2052 /home/user/workspace/gopi-test-scratch`, `2051 /`.
- `RED phase failures`: `redgreen.log` (quoted in `## Verification`).
- `GREEN phase`: `ok github.com/nick/gopi/internal/tools 14.666s` ·
  `ok github.com/nick/gopi/cmd/agent 19.774s` (merge-pass + fence neighbours).
- `The regression the suite caught`: `--- FAIL: TestTaskVerbForBranchRunsAsAdvertised`,
  `cli_tasks_help_surface_basic_test.go:574` — text quoted above.
- `Full-package run on this host, live merger + workers, the wall-clock claim
  itself`: `go test ./cmd/agent/ -count=1 -timeout 10m -v` →
  `panic: test timed out after 10m0s`, `running tests:
  TestVoiceFileModeTranscribesViaStubEngine (21s)`, 845 tests green before it,
  **0 failures**; the run's own figures: **1404 test entries, 647.3 s of in-test
  time**. This is the unfiltered number §"Not Done" reports — the package's work
  exceeds the budget with no hang in it.
- `Rule 30`: `git status --porcelain` prints nothing in this worktree at
  close-out (observed: empty output before the commit and after it).
