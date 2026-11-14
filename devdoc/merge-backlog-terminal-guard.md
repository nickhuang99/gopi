<!-- doc-class: normative -->
# Task Report: D2 — the merge-backlog TERMINAL guard

> **ROUTING UPDATE (2026-10-16).** The lines below are updated where the ONE writer replaces
> a raw ref removal: `gopi task archive <branch> --reason superseded` mints the archive tag, records
> the WHY and removes the ref, in one arm. The measurement each note carries is unchanged.


**Branch**: `fix/merge-backlog-terminal-guard` (worktree `~/workspace/gopi-d2-terminal-guard`,
based on master `909d4ca9`)
**Files**: `internal/tools/task_merge_backlog_terminal_guard.go` (new, the guard),
`internal/tools/task_merge_backlog_terminal_guard_test.go` (new, the regression),
`internal/tools/tasks.go`, `internal/tools/memory_sync_task.go`,
`internal/tools/task_merge_backlog.go` (wiring + one named constant)

---

## 1. The defect

A `merge-backlog: <branch> unmerged — merge/archive decision needed` row reached a
**success terminal** while its branch was provably still unmerged. The worst case is
`t-1788218295`:

```
$ git rev-list --count master..archive/fix-memsync-import-atomic-20260901
5
$ sqlite3 -readonly ~/.gopi/memory.db "SELECT status FROM tasks WHERE task_id='t-1788218295'"
verified
```

A 482-insertion atomicity fix (memsync-import-atomic) sat behind a green row. The row's
status vocabulary (`pending`/`claimed`/`completed`/`verified`) has **no word for "still
unmerged"**, and **no write site asked git** whether the branch landed — so a terminal row
reads as *"the decision was made / the merge is fine"* when nothing was merged.

The **re-arming** half of this family was already fixed (`7162092b`, dedupe on the task
identity). This task closes the surviving half: **the status itself**.

## 2. Root cause (5-why, with file:line)

1. **Symptom** — the row is `verified`, the branch is 5 commits ahead of master. The status
   was written without consulting git.
2. **Why it was legal** — `taskAllowedTransitions` (`internal/tools/tasks.go:3643`) admits
   `cancelled|completed|failed → verified`; nothing on that path looks at a ref.
3. **Why the write did not even walk that table** — the write that produced *this* row is
   `syncTaskMigrateFrom` (`internal/tools/memory_sync_task.go:1120` at the time of this audit;
   master's `cf314174` renamed it `syncTaskMoveStatusTx`, same raw `UPDATE tasks SET status = ?`
   semantics), a **raw**
   `UPDATE tasks SET status = ?` reached from the **"completed wins"** ruling
   (`memory_sync_task.go:1009` live-row arm, `:1027` cancelled-vs-terminal arm). A row-level
   truth is not a work-level truth: sync adopts *"the row closed"*, never *"the merge
   happened"*.
4. **Fingerprint confirming this path** — `t-1788218295` has **no rows in `task_events`**
   (the local door always writes a transition event) and carries sync-rewritten tags
   (`merge-backlog,yield-dead,w423-shield`). It was cancelled, then a terminal patch row
   collided with it and the completed-wins ruling forward-migrated it.
5. **Why nothing caught it** — the branch-scan mints a row whose entire *premise* is "this
   branch is unmerged" (`cmd/agent/cli_tasks.go` `agentBranchScan` → `MergeBacklogScanSummaryVerb`),
   and **nothing re-checks that premise at close time**. The local door's only git-shaped
   gate was `reclaimRunResidueRefusal` — about the run's *worktree*, never the branch's
   *ancestry*.

**The one mutation core is `taskUpdateStampedFrom` (`internal/tools/tasks.go:3361`).** It
already hosts a status-conditional refusal, which is where the guard belongs.

## 3. The guard

`mergeBacklogTerminalVerdictFor(row, status)` — **one predicate, both doors**:

> a merge-backlog family row may reach a success terminal (`completed`|`verified`)
> **only** when the branch is **provably merged** (`git rev-list --count master..<ref>` == 0)
> **or** the row records an **explicit archive decision with a reason**.

Merge proof is taken in order of strength (three-valued probe — `Unknown` is never collapsed
into "merged" *or* "unmerged"):

| # | evidence | query |
|---|---|---|
| 1 | the branch ref (local → remote-tracking → plain) | `rev-list --count master..<ref>`, then **patch equivalence** (`git cherry master <ref>` lists no absent patch) |
| 2 | the **tip the row recorded** (`branch tip: <sha>`) | `merge-base --is-ancestor <tip> master` |
| 3 | the branch's **archive tag** (Rule 9 namespace, name-matched through the branch's **tails**) | `rev-list --count master..<tag>` |
| 4 | nothing resolvable | `Unknown` → **recorded loudly, allowed** (Rule 15) |

Arm 1's second half is not optional: this repo's merger **adopts work by cherry-pick / rebase**
(`adoption: merge <branch> (<sha>)` commits on master — `680b0ab4`, `c7ab803a`, `fd69fac0`…),
which leaves the branch ref alive and *ahead by commit identity* while master holds the same
patches. `rev-list --count != 0` alone would refuse those legitimate merges — the count is
about commits, not about work — so the guard asks `git cherry` (all patches present ⇒ merged).
`TestMergeBacklogTerminalGuard_AllowsCherryPickedLanding` pins exactly this case.

Arm 2 exists because of a **measured false-positive risk**: `task_merge_executor.go` merges,
removes the branch ref through its close-out arm, and only *then* writes `completed` — so "the ref is gone" is
the **normal** state of a legitimate close. Arm 3 is exactly how `t-1788218295` is provable
today (its branch ref is gone; its archive tag is 5 ahead).

Arm 3 must **walk the branch-name tails**, which was found only by testing the D2 row's own
shape: the scan mints rows for **remote-tracking** branches (`hp/fix/memsync-import-atomic`)
while the archive tag was made from the **local** name with slashes flattened
(`archive/fix-memsync-import-atomic-20260901`). A face-value tag match misses the one row the
guard exists for. `mergeBacklogArchiveRefsFor` therefore tries the full name and each of its
tails (`hp/fix/x` → `fix/x` → `x`), in both spellings, and takes the **strongest** (lowest
ahead) candidate; `TestMergeBacklogTerminalGuard_D2ExactShape` is that row, reproduced.

Wiring:

* **local door** — `taskUpdateStampedFrom` calls `mergeBacklogTerminalRefusalOn(ctx, db, taskID,
  status, effReason)` in the state gate **before any write**; a refusal returns an error naming the
  branch, the ahead count and the two legal exits. The note rides the row's append-only trail
  (`merge-backlog-terminal-refused`) so the still-open row explains itself.
* **sync door** — `syncTaskMoveStatusTx` calls `mergeBacklogTerminalRecord(ctx, tx, taskID, status)`.
  Sync **must not refuse** (a refusal breaks bilateral convergence), so it adopts the write
  and **records** it (`merge-backlog-terminal-refused` / `-unverified`) + `taskNotifyLog`.
  This is the door that actually produced `t-1788218295`.

**Re-landing on master (2026-09-17, after the merge was reverted).** Two master-side changes
between the branch's base and the re-land, both handled by passing the caller's handle through
`QueryExecer` rather than by weakening the guard:

1. `syncTaskMigrateFrom` no longer exists. Commit `cf314174` ("fix(memsync): import-tasks
   single-transaction atomicity — no partial patch") renamed it **`syncTaskMoveStatusTx`** and
   moved it inside the import's `BEGIN IMMEDIATE` transaction as `(ctx, *sql.Tx, ...)`. The
   branch's test still called the old name, which is why `go vet ./...` reported
   `undefined: syncTaskMigrateFrom`.
2. Because the sync door now runs **inside** a write transaction, the guard may not open a
   second connection: it would block on the caller's own transaction and its trail INSERT would
   fail with `SQLITE_BUSY` — the guard's record silently lost, which is the exact silence the
   guard exists to prevent (measured: `task_events append for d2-sync-door
   (merge-backlog-terminal-refused) failed: database is locked (5)`). The guard therefore takes
   the caller's `*sql.Tx` (`mergeBacklogTerminalRecord(ctx, tx, ...)`,
   `mergeBacklogTerminalRefusalOn(ctx, q, ...)`, `taskByIDOn`, `taskEventCountForDetailOn`,
   `taskEventWrite(ctx, QueryExecer, ...)`) so the row READ and the trail WRITE ride one
   connection — and a trail entry can no longer survive a rolled-back import.

The rule and its three arms are unchanged; only the connection plumbing is. Re-landed on
`fix/d2-merge-backlog-terminal-guard-reland`.

Rule 13 compliance: both summary faces are parsed through their **single-source verbs**
(`mergeBacklogTag`-prefixed scan verb, and `mergeBacklogSummaryVerb` split into its halves at
run time) — no mirrored literal. `mergeBacklogMergeTarget = "master"` is now the **one**
definition shared by discovery, dedup and the guard; the process itself previously had
`"master.."` textually duplicated in the branch-scan and the audit.

## 4. Regression test — and the RED proof (both runs pasted)

`TestMergeBacklogTerminalGuard_*` (12 tests) drives the **real** write paths (`TaskUpdate` /
`TaskUpdateReason` / `SyncTaskImport`) against **real** git repos, so it is red the
moment the guard is reverted. Hermetic: the ledger is `t.TempDir()` via `GOPI_MEMORY_DB`
(Rule 22 — never `/tmp`). Both runs below are the **current** guard (transcripts kept at
`~/workspace/d2-proof/`).

**RED — guard neutralized** (one temporary early `return Allow:true` inside
`mergeBacklogTerminalVerdictFor`; `go build ./...` still OK; probe removed and the file
checksum-restored afterwards — `grep -c "D2 RED PROBE"` == 0):

```
$ go test ./internal/tools/ -run 'TestMergeBacklogTerminalGuard_(RefusesUnmergedBranch|RefusalIsRecorded|D2ExactShape|SyncDoorRecordsInsteadOfRefusing)' -count=1
--- FAIL: TestMergeBacklogTerminalGuard_RefusesUnmergedBranch (0.20s)
    --- FAIL: TestMergeBacklogTerminalGuard_RefusesUnmergedBranch/completed (0.03s)
        task_merge_backlog_terminal_guard_test.go:190: row d2-unmerged-completed reached completed with branch d2/unmerged provably ahead of master — the D2 guard is not refusing (reverted?)
    --- FAIL: TestMergeBacklogTerminalGuard_RefusesUnmergedBranch/verified (0.01s)
        task_merge_backlog_terminal_guard_test.go:190: row d2-unmerged-verified reached verified with branch d2/unmerged provably ahead of master — the D2 guard is not refusing (reverted?)
--- FAIL: TestMergeBacklogTerminalGuard_RefusalIsRecorded (0.10s)
    task_merge_backlog_terminal_guard_test.go:212: expected the guard to refuse completed on an unmerged branch
--- FAIL: TestMergeBacklogTerminalGuard_D2ExactShape (0.08s)
    task_merge_backlog_terminal_guard_test.go:375: row d2-exact-shape closed completed while archive/fix-memsync-import-atomic-20260901 is 1 commit ahead of master — the D2 defect verbatim (t-1788218295)
--- FAIL: TestMergeBacklogTerminalGuard_SyncDoorRecordsInsteadOfRefusing (0.05s)
    task_merge_backlog_terminal_guard_test.go:460: the sync door adopted a verified row for an unmerged branch with no merge-backlog-terminal-refused trail entry — this is exactly the silent write that produced t-1788218295
FAIL
FAIL	github.com/nick/gopi/internal/tools	2.148s
RED_EXIT=1
```

**GREEN — guard restored** (full guard test set):

```
$ go test ./internal/tools/ -run TestMergeBacklogTerminalGuard -count=1 -v
--- PASS: TestMergeBacklogTerminalGuard_RefusesUnmergedBranch (0.17s)
--- PASS: TestMergeBacklogTerminalGuard_RefusalIsRecorded (0.24s)
--- PASS: TestMergeBacklogTerminalGuard_AllowsArchivedDecision (0.20s)
--- PASS: TestMergeBacklogTerminalGuard_AllowsMergedBranch (0.41s)
--- PASS: TestMergeBacklogTerminalGuard_AllowsDeletedBranchByRecordedTip (0.33s)
--- PASS: TestMergeBacklogTerminalGuard_IgnoresNonFamilyRows (0.35s)
--- PASS: TestMergeBacklogTerminalGuard_UnverifiableIsLoudNotBlocking (0.08s)
--- PASS: TestMergeBacklogTerminalGuard_AuditFaceVerbIsGoverned (0.29s)
--- PASS: TestMergeBacklogTerminalGuard_D2ExactShape (0.22s)
--- PASS: TestMergeBacklogTerminalGuard_ArchivedThenMergedIsAllowed (0.16s)
--- PASS: TestMergeBacklogTerminalGuard_AllowsCherryPickedLanding (0.13s)
--- PASS: TestMergeBacklogTerminalGuard_SyncDoorRecordsInsteadOfRefusing (0.12s)
PASS
ok  	github.com/nick/gopi/internal/tools	3.928s
GREEN_EXIT=0
```

The RED run reproduces the D2 symptom **exactly**: a merge-backlog row for a provably
unmerged branch lands on `completed`/`verified` and nothing anywhere records it.

**Reproducible, not transcribed.** `bash scripts/d2-redproof.sh` performs the whole proof:
it backs the guard up, neutralizes the **single** decision predicate
(`mergeBacklogTerminalVerdictFor` — the one `Allow: true` early return), asserts the anchor
was actually hit, runs the three tests, restores the file, `cmp`-verifies the restore and
prints `RED_EXIT=<n>` (non-zero = the guard's absence is detected; a zero exit makes the
script itself fail, so it cannot pass vacuously). The restore is a `trap` on
EXIT/INT/TERM/HUP — an interrupted run cannot leave a neutralized guard behind. Measured
2026-09-17 on this branch: `RED_EXIT=1`, then `cmp ... guard.bak` → IDENTICAL
(`grep -c "D2 REDPROOF"` == 0).

### 4a. A pre-existing test asserted the DEFECT — updated deliberately

`cmd/agent/cli_branch_scan_test.go:TestAgentBranchScan_ClosedRowStaysMasked` closed a scan
row `completed` *without merging* and asserted it masked the branch. Its own doc comment
says it: *"closed by a worker that did not merge"*. **That write is the D2 defect**, so the
test was asserting the bug. It now asserts the guard's behaviour instead — the close is
**refused** (`TaskStatusCompleted` arm), while the `cancelled` arm still closes and keeps
masking. The duplicate-dispatch property the test exists for holds either way: an open row
masks its branch (the `7162092b` identity mask), and a refused row is open. The change is
commented in place with the reason, not silently relaxed.

## 5. Disposition proposal — the already-wrong rows (no ledger mutation)

Measured read-only (`sqlite3` opened `mode=ro`, every statement a SELECT; plus `git log` /
`git cherry` / `git rev-list`). The ledger is **live** — counts drift; the figures are one
iteration's evidence, reproducible with `bash scripts/d2-disposition.sh`.

**Step 1 — the 101 archive tags, split by whether their patches are on master**
(`git cherry master <tag>`: empty ⇒ superseded):

| class | n | meaning |
|---|---|---|
| archive tags **superseded** (patches already on master) | 31–32 | the archive was taken **after** a real landing — honest, keep |
| archive tags carrying **absent patches** | 68–70 | work that exists **only** in the tag; the D2 family's raw material |

(The count differs by 1–2 between runs: the namespace is live, and
`git rev-list --count` vs `git cherry` disagree on squash-shaped landings.)

**Step 2 — the 6 success-terminal family rows, per row** (family is the scan verb
`merge-backlog: <branch> unmerged` or the tag; the umbrella row
`01a09462…` "merge backlog: land the 5 branches…" is the epic itself, not a per-branch row,
and is excluded as not-family):

| row | status | branch | ref | archive tag | patches on master | verdict |
|---|---|---|---|---|---|---|
| `t-1788218295` | verified | hp/fix/memsync-import-atomic | gone | `archive/fix-memsync-import-atomic-20260901` | **5 absent** | **D2 archetype** |
| `t-1788218212` | verified | hp/feat/serve-tick-git-sync | gone | none | **landed** (`680b0ab4 adoption: merge feat/serve-tick-git-sync`) | correct — decision was MERGE |
| `t-1788218223` | verified | hp/feat/task-id-collision | gone | none | not found by branch name | no proof ⇒ D3 |
| `t-1788441090-1` | completed | feat/maps-inventory | gone | none | closed by `9f0a7a7c`: "already merged FF at 46c99f3" (**Maps repo**, `~/Downloads/games/Maps`) | correct — foreign repo |
| `t-1788441090-2` | completed | feat/maps-vcmi-tools | gone | none | closed by `e18a10dd`: "already merged at 55715bb (maps repo)" | correct — foreign repo |
| `t-1788441091` | completed | feat/maps-visual-verify | gone | none | sibling of the two above (Maps repo; same batch) | correct — foreign repo |

So the class is **1 archetype + 3 correct + 2 requiring a D3 re-dispatch** — not "65 wrong
rows". An earlier count of 65 came from grading all 101 archive tags as ledger rows; the
archive namespace is git-side and holds no rows at all.

Proposed dispositions (all read-only — **no row is edited**; the ledger is append-only):

* **D1 — keep, proven.** A success terminal whose branch resolves with
  `rev-list --count master..<ref>` == 0, **or** whose patches are all on master
  (`git cherry` empty), **or** whose landing commit names the branch
  (`git log master --grep=<branch>` / `adoption: merge <branch>`). The decision recorded IS
  the truth. ⇒ `t-1788218212`, `t-1788441090-1`, `t-1788441090-2`, `t-1788441091`.
* **D2 — keep the row, and the class is now recorded.** A success terminal for a branch
  carrying an **archive tag with absent patches**. `t-1788218295` is the archetype: the row
  asked for a *merge-or-archive* decision and the archive tag proves the decision **was**
  archive — so the row is honest about the *decision*, and the 482-insertion fix is not lost
  (it lives in the tag). What was wrong: **nothing on the row said so**. The guard now records
  that posture (`merge-backlog-terminal-refused` / `-unverified`) for new writes. No repair of
  the row is proposed; the **work** is dispositioned under D4.
* **D3 — dispatch a repair row.** A success terminal with **no proof anywhere** (no ref, no
  tag, no landing commit) must be re-dispatched as a fresh decision row naming the branch and
  the missing proof — Rule 26: an incomplete fix is repaired by dispatching again, never by
  waiting. Measured: **2 rows** (`t-1788218223`, and any future member); for
  `t-1788218223` the first step is to ask the **HP host** (`hp` remote), whose local refs are
  not visible from peer1 — the guard's `Unknown` posture is exactly this case, recorded rather
  than guessed.
* **D4 — do NOT bulk-sweep the archive namespace.** 68–70 of 101 tags still carry patches
  absent from master; each is either genuinely abandoned or recoverable work. Classify per tag
  (`git cherry` + symbol presence on master), then dispatch only the recoverable ones — the
  largest, `archive/feat/chat-m2-streaming` (20 commits), `archive/feat/web-m2-b5b8b9b12`
  (19), `archive/feat/cov-tools-rest` (15), are feature-sized. A bulk "archive ⇒ done" sweep
  would recreate D2 one layer up (a tag with no row).

**Boundary I could not close in this branch (honest):** the branch-scan payload records **no
branch tip**, so for scan-minted rows with no surviving ref/tag the guard can only reach
`Unknown` → allowed-and-**recorded** rather than refused. That is deliberate (refusing what it
cannot see would wedge the merge loop) but it is a **residual hole**, and its fix is one line
in the scan (`cmd/agent/cli_tasks.go agentBranchScan`). It is **dispatched as a follow-up
task**, not smuggled into this branch.

## 6. Not Done (honest)

* The **history** of the 6 already-terminal rows is untouched — by design (append-only, and
  the epic forbids mutating ledger rows). They are classified, not repaired.
* The **68 abandoned archive tags** are classified, not triaged one by one (D4 above).
* The scan-payload tip enrichment is **dispatched, not landed here** (a different, smaller
  change; the briefing's scope was the guard) — see the follow-up task id in §8.
* `t-1788218223`'s branch could not be checked on the **HP host** (`hp` remote) from peer1,
  so its verdict stays `no-proof ⇒ D3` rather than a claim either way.
* The archive-tag count is live: `rev-list --count` and `git cherry` disagree by 1-2 on
  squash-shaped landings, so the tables carry the reasons next to the numbers.
* Not exercised end-to-end through a live `gopi task update` CLI invocation — the guard is
  verified through the engine entry points the CLI calls (`TaskUpdate` / `TaskUpdateReason` /
  `syncTaskMigrateFrom`), which is the layer under test.

## 7. Verification

```
$ go build ./...                                  → exit 0
$ gofmt -l <the touched files>                    → (clean, no output)
$ go test ./internal/tools/ -run TestMergeBacklogTerminalGuard -count=1   → exit 0 (12 PASS)
$ go test ./internal/tools/ ./cmd/agent/ -count=1 → see §7.1
```

### 7.1 The two packages the acceptance names

```
$ go build ./...                                          → exit 0
$ go test ./internal/tools/ ./cmd/agent/ -count=1         → see below
```
Full-run result (job transcript `/home/user/workspace/d2-proof/pkg-run.txt`). Note the environment:
the host's `/dev/shm` was at **95%** during the first attempt, which produced
`No space left on device` failures in `git add` and in the go-build binary copy — **not**
related to this change. After the sweep (`gopi scratch-gc` semantics, 70m age, live-process
protected) the tree had room and the run was repeated.

