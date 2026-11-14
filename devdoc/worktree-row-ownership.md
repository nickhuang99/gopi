<!-- doc-class: historical -->
# Task Report: worktree row-ownership binding (finish-cleanup + merged sweep)

Branch: `fix/worktree-row-ownership` (linked worktree `/home/user/workspace/gopi-wtrowown`)
Base: master + the landed liveness guard (`fix/worktree-live-cwd-guard`, merged into this branch at `a20565a`).
Follow-up to: row `01a088ef` liveness incident (2026-09-10). This epic closes the **enabling** half.

## Completed

### 1. Ownership-binding gate on every removal path

One implementation, `finishCleanupOwnerRowGate` → two halves, both in
`internal/tools/task_finish_cleanup.go` (the merged sweep imports them, so the
rule has ONE spelling):

- **`finishCleanupOwnerDeclaredReason(declared, worktree)` — gate ①**
  declared-vs-resolved path identity, canonicalized (`finishCleanupSameWorktreePath`
  → `finishCleanupCanonicalWorktreePath`: absolute + `EvalSymlinks` + `Clean`, so a
  trailing-separator or symlinked spelling of the SAME dir matches, a
  SUBDIRECTORY does not). Empty on either side = allowed (nothing to bind).
- **`finishCleanupForeignOwnerReason(owners, worktree)` — gate ②**
  over a once-per-pass owner list (`finishCleanupWorktreeOwnerRows`: one SQL
  `SELECT task_id,status,claimed_by,deliverable FROM tasks WHERE deliverable LIKE
  '%"worktree"%'`, JSON decode as the authority, the row being cleaned excluded).
  Names the owner row id + status + holder liveness
  (`finishCleanupOwnerHolderSuffix` reusing `claimHolder`/`pidAlive`: a local live
  pid → "alive", a foreign host → "claimed by <host>", never "dead").

Wired at the three removal sites:

| site | gate | behaviour |
|---|---|---|
| `finishCleanupServerRecycle` (server arm) | ①② | `rep.Skipped` + `SkipReason`, no removal |
| `runFinishCleanupChainTarget` step ③ (client `--force`) | ① pre-push, ② post-push | `FinishCleanupStep{worktree, Detail}` |
| `MergedCleanupSweep` guard ⑤c | ② | `rep.Skipped` naming the owner |

**Design decisions worth keeping** (all three came from the root-cause work, not
from taste):

1. **Gate ① compares the row's record against GIT'S answer, not against the
   removal target.** The first draft compared `d.Worktree` with the resolved `wt`
   — structurally vacuous: the resolution is *defined* as "declared if non-empty,
   else git's probe", so the two can never differ and an A/B run showed the gate
   never fires on the real shape. The real shape is a DISAGREEMENT between the
   row's deliverable (`~/workspace/gopi-hippocampus-spec`, backfilled) and git's
   linked worktree for the branch (`~/workspace/gopi-hippocampus-reframe` — the
   re-attached directory). Both `linked` and `declared` are now resolved
   independently and compared.
2. **Gate ① runs before the push in the client chain** and refuses the whole
   close-out (`rep.Skipped`): the push hands the branch off as *this row's*
   deliverable, and gate ① firing means the row's ownership evidence is
   self-inconsistent. Nothing was done, nothing destroyed, both paths reported.
3. **Gate ② runs after the push, before step ③** (the `--force` removal): a
   refused removal must never cost the delivery — the branch is already on the
   server, step ④ is not reached, so the local branch also survives.
4. **The scan is best-effort** (Rule 15: it cannot be silent — the arm still
   reports its own steps): a DB error leaves gate ① as the only binding rather
   than blocking cleanup. The merged sweep is stricter: its ownership scan
   failure lands in `rep.Failed` naming "ownership scan: …" (that sweep's other
   guards already treat a ledger error as fail-closed).
5. **Known limit of gate ② on the merged sweep**: guard ⑤ (live-ledger) already
   protects a worktree whose OWNER row is live *in this repo*. ⑤c's added value
   is the re-attached-directory shape: the owner is a row in ANOTHER repo, or a
   terminal row (whose worktree is still its deliverable home).

### 2. Regression tests

`internal/tools/task_finish_cleanup_test.go` (+ `task_merged_cleanup_test.go`,
`task_deliverable_worktree_test.go`), all against real-git fixtures:

- `TestWorktreeOwnershipServerRecycleSkipsForeignOwnedWorktree` — gate ②, owner
  a LIVE row (this test process's own `host:pid` stamp, so the holder rendering
  is exercised for real): skip names the marker, `t-owner`, the worktree path and
  the live pid; directory + branch survive.
- `TestWorktreeOwnershipServerRecycleSkipsDeclaredMismatch` — gate ①: row
  declares `…/some-other-worktree`, git says the branch lives at the fixture
  worktree → skip naming BOTH paths, directory survives.
- `TestWorktreeOwnershipServerRecycleMatchingDeclaredStillRecycles` — regression:
  a declared value EQUAL to git's answer recycles exactly as before, even with an
  unrelated foreign-owned row in the ledger.
- `TestWorktreeOwnershipServerRecycleSelfRowNotForeign` — the row being cleaned is
  never its own foreign owner.
- `TestWorktreeOwnershipChainSkipsForeignOwnedWorktree` — client chain: push still
  OK, step ③ reports the foreign owner, directory + branch survive.
- `TestWorktreeOwnershipChainSkipsDeclaredMismatch` — client chain: whole close-out
  skipped (no step runs), both paths named.
- `TestFinishCleanupOwnerRowGateUnit` / `TestFinishCleanupOwnerHolderSuffix` —
  decision edges (empty target, empty declared, trailing separator, subdirectory
  refused, unreadable-ledger allowance; holder renderings).
- `TestMergedCleanupForeignOwnedWorktreeSkipped` — sweep ⑤c: a TERMINAL owner row
  in another repo → skip with owner named, removed=0, directory + branch survive.

### 3. Backfill-gap closure — the ENABLING root cause (this is the real find)

The 01a088ef row's deliverable was `{"repo":…}` only; the epic asked whether the
landed branch backfill (memory #1501) + the creation-time worktree seam (memory
#1375) actually record `deliverable.worktree` for the main-repo-launch + inline
`git worktree add` shape. **They do not — and the reason is a one-line bug:**

- Live evidence: session `01a08919-8791-731b-bbd5-d4a9efc1f775`
  (`83d40041-…jsonl` line 123/383) ran
  `cd ~/work/GoPi && git worktree add ~/workspace/gopi-hippocampus-spec -b feat/hippocampus-spec`.
  Its JSONL `session_start` says `workdir: /home/user/work/GoPi` → the tilde is
  NEVER expanded anywhere in the child's own commands, and the executor truly
  commits in the worktree. Its wrapper row kept `{"branch":"feat/hippocampus-spec","repo":"/home/user/work/GoPi"}`
  — no worktree — and `/home/user/workspace/gopi-hippocampus-spec` is **gone from
  disk**, while the SIBLING row `01a08913-d4eb-…` (same session) had recorded
  `worktree: /home/user/workspace/gopi-hippocampus-reframe` earlier and that
  directory is also gone. Two rows, one session, two worktrees, zero protection.
- Root cause: `taskBashWorkdirCandidates`'s `add()` did `filepath.Abs(p)` then
  `os.Stat()` on the RAW token. A `~`-spelled path resolves against the process
  CWD (`~/workspace/...` → `<cwd>/~/workspace/...`), the stat fails, and the
  candidate is silently dropped → the creation-time seam never detects the
  worktree → no `deliverable.worktree` → the ownership gate has nothing to bind.
- Fix: `expandHome(p)` (the SAME routine read/write/edit use — one home-expansion
  spelling) before `filepath.Abs`. Surgical, 3 lines.
- Guard: `TestTaskBashWorkdirCandidatesExpandsTilde` +
  `TestTaskDeliverableWorktreeBashTildeWorktreeAddMoment` (the end-to-end creation
  round with `HOME` pointed at the fixture root so `~/workspace/<name>` IS the
  linked worktree). **A/B proof**: with the `expandHome` call removed both tests
  fail (`worktree="" branch=""`, candidate list `[<cwd>]`); restored, both pass.
- Cross-check inside the test: the stamped value equals
  `finishCleanupBranchWorktree(...)` — i.e. the creation-time record and the
  cleanup-time resolution now name the same directory.

### 4. Item (3) of the briefing — the optional ownership LOCK: explicit NON-GOAL

Not implemented, and the reason is a real hole-closure argument, not a budget:

- Gates ①② already bind a removal to the row's own record + the whole ledger, on
  every path, with zero write-time cost. A lock file adds no case these two
  cannot express.
- A lock's lock-in costs are real: a lock at `worktree add` time is only as
  trustworthy as the arbiter, and the only arbiter is the ledger itself; the file
  is a second source of truth that both the `git worktree add <path>` re-attach
  case and a crashed holder would desynchronise.
- The residual hole gates ①② cannot see is the **same row detached/re-attached
  to a second directory** (a row's record is a single path). That is bounded by
  the liveness guard (a live holder skips) plus the chain's push-before-remove
  order — the branch is delivered before anything is deleted. A lock would not
  close it either (it would be the same row's own lock, i.e. name the same owner).

Recorded, deliberately, as a non-goal.

### 5. Acceptance mapping (the briefing's (a)/(b)/(c))

- **(a)** resolved worktree ≠ the row's record → SKIP + reason naming both paths,
  directory on disk, branch preserved:
  `TestWorktreeOwnershipServerRecycleSkipsDeclaredMismatch`,
  `TestWorktreeOwnershipChainSkipsDeclaredMismatch`.
- **(b)** matching record → still recycled, existing suites green:
  `TestWorktreeOwnershipServerRecycleMatchingDeclaredStillRecycles` + the whole
  pre-existing `FinishCleanup*` / `ServerRecycle*` / `MergedCleanup*` set.
- **(c)** another live row claims the worktree → skip with the holder named:
  `TestWorktreeOwnershipServerRecycleSkipsForeignOwnedWorktree` (owner = live row
  stamped with THIS process's `host:pid`; the reason names the row id, its status
  and the live pid) and `TestWorktreeOwnershipChainSkipsForeignOwnedWorktree`.
  The briefing phrased (c) as a *lock file* test; the guarantee is met through the
  LEDGER (the row's own `deliverable.worktree` + `claimed_by`), which is the
  single source of truth for ownership — a lock file would be a second one
  (Rule 14's "never mirror" spirit), so the optional lock stays a non-goal with
  the identical observable behaviour.

## Not Done

- No ownership lock (see item 4 above — explicit non-goal with the reason).
- The two lingering orphaned worktrees of row 01a08913-d4eb's session were NOT
  removed as part of this task (they are already gone from disk; only the ledger
  trace remains). No cleanup beyond the code change was attempted.
- `gofmt -l` reports pre-existing unformatted files in `internal/tools/`,
  `internal/agent/`, `cmd/agent/` (e.g. `chat.go`, `memory_sync.go`,
  `cli_memory.go`) — none of them mine; every file this task touched is
  gofmt-clean and untouched by the formatter.
- Gate ② cannot resolve a worktree recorded by a row whose `deliverable` is
  non-JSON (the decode is the authority) — such a row contributes no ownership
  claim. Pre-existing shape, unchanged.

## Merge of the guard branch (base discipline)

The epic's base (master + `fix/worktree-live-cwd-guard`) landed on master as
`902ee49` mid-task, so this branch merged master at `053fc5a`. ONE overlap: the
guard branch had extracted the git worktree verb into named constants
(`finishCleanupWorktreeVerb` / `ListVerb` / `RemoveVerb` / `ListFlag`, Rule 1)
while this branch had introduced a separate deliverable-JSON-key constant with
the SAME value `"worktree"` — two constants, one value, i.e. exactly the mirror
Rule 14 forbids. Resolution: the SQL `LIKE` pre-filter now reuses
`finishCleanupWorktreeVerb`; the extra constant is gone. The gates themselves
were untouched by the merge, and the two guards now sit in sequence in the same
chain — liveness (③) first (a live holder is the strongest reason to stop),
ownership immediately after.

## Verification

- Build: `go build ./...` → OK; `go vet ./internal/tools/` → OK
- Acceptance selection: `go test ./internal/tools/ -run 'FinishCleanup|ServerRecycle|MergedCleanup|WorktreeOwnership' -count=1` → **ok, 37 test functions** (22.8s)
- Backfill: `go test ./internal/tools/ -run 'TaskBashWorkdirCandidates|TaskDeliverableWorktree' -count=1` → ok (34.8s)
- A/B (the fix's own claim): the tilde tests FAIL with `expandHome` removed, PASS with it restored.
- cmd/agent (worker recycle e2e + resume-chdir): `go test ./cmd/agent/ -run 'WorkerServerDelivery|WorkerRecycle|ResumeChdirToWorktree' -count=1` → ok
- Full suites, `-timeout 25m`:
  - `go test ./internal/tools/ -count=1` → **ok, 936.2s, TOOLS_EXIT=0**
  - `go test ./cmd/agent/ -count=1` → **ok, 111.8s, CMD_EXIT=0**
  - (a first full run without `-timeout` died at the default 10m package timeout on
    PRE-EXISTING slow tests — `TestReproServeClaimsEmptyPayloadRow` /
    `TestReproLegacyMigrationLosesFTSRows`; the 25m rerun of the same tree is green,
    so the timeout was the harness default, not a regression from this change.)
- gofmt: every touched file clean.
- Commit: `2712d90` on `fix/worktree-row-ownership` in the linked worktree
  `/home/user/workspace/gopi-wtrowown` (no self-merge).

## Recovery

```
git show fix/worktree-row-ownership:devdoc/worktree-row-ownership.md
```
