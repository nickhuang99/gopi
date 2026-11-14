<!-- doc-class: historical -->
# Task Report: the own-worktree-release pass sweeps a repository only when its ledger covers it

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch: `fix/own-release-cwd-authority` (worktree `~/workspace/gopi-cwd-authority`, base `master` 812f9a60)

## The defect

`ReclaimSelfOwnRegisteredWorktrees` (`internal/tools/task_reclaim_self.go`) is the
**ownership-independent** producer arm of the worktree release: it frees the linked
worktrees a run created whether or not any ledger row records them, which is its
legitimate purpose (a round may spin up a scratch checkout no row ever owns).

Its repo set, `reclaimSelfOwnRepos()`, was **unconditional**:

```
repos = (repos of the rows THIS process claims) ∪ (the repo of the process CWD)
```

When the ledger is not the authority for the repository the process stands in — the
classic `gopi worker <task-id>` launched from `~/work/GoPi` with a sandboxed
`GOPI_HOME`/`GOPI_MEMORY_DB` — the claim half is empty/foreign and the **CWD half
alone** enrolled the live repository. The pass then enumerated that repository's
registered worktrees and `git worktree remove`d every one that was clean, held by no
live process and older than `reclaimSelfOwnMinAge` (5 min) — including another task's
live, unmerged work. Only the directory goes (`branch detached untouched`), which is
why the loss looked random rather than catastrophic.

## Completed

- **The guard** (`internal/tools/task_reclaim_self.go`):
  - `reclaimSelfOwnRepos()` — the CWD half is now **conditional on ledger authority**:
    a repository is enrolled by the cwd only when the ledger this process reads names
    at least one row carrying that repo. Nothing else changed: a repo already enrolled
    by the claim-stamp half is swept exactly as before.
  - `reclaimSelfOwnLedgerNamedRepos()` — the authority evidence: the repos named by
    any row's `deliverable.repo` in the ledger this process actually reads
    (`memoryDBPath()`), so a sandbox ledger names nothing.
  - `reclaimSelfOwnRepoNamed()` — the comparison, on **canonical paths**
    (`scratch.CanonicalPath`, the shared renderer — Rule 14), so a prefix or a
    spelling difference can never authorise a sweep.
  - **Every direction is NAMED** (Rule 15): the withheld repo is reported on the
    operator sink with its evidence, an unreadable ledger reports the cwd refusal
    too, and the ledger-scan-failure path keeps its original report. Neither branch
    returns silently.
- **The regression test** (`internal/tools/task_reclaim_self_cwd_authority_test.go`,
  4 cases):
  1. the predicate — an exact canonical match, and the three approximations (empty,
     other repo, parent directory) that must never authorise;
  2. the repo set — a foreign ledger + a cwd inside a repository yields **no** repo,
     and the refusal is named on stderr;
  3. **the epic's regression** — a decoy (clean, detached, aged registration nothing
     owns) **survives** a full `ReclaimSelfOwnRegisteredWorktrees()` run with a
     sandbox ledger and the process standing in that repository;
  4. purpose preserved — with a ledger naming the repo and a row this process does not
     claim (so the claim half stays empty and the cwd half is what enrolls), the decoy
     is freed exactly as before.
- **The red proof** — mutation, not a second code path (the shipped code carries no
  test-only knob; the only switch is `ReclaimSelfEnv`, an operator brake). With the
  guard line replaced by the pre-fix behaviour (`add(repo)` unconditionally), cases 2
  and 3 turn **red** and the decoy is really freed; restoring the guard returns them
  **green**. Both directions are transcribed under Evidence.
- **Live end-to-end A/B through the real binary** (`gopi worker --once` with a real
  bounded round, sandbox ledger naming only the run's own fixture repo, process
  standing in a live repo holding a clean aged decoy): pre-fix binary **DECOY_FREED**,
  guarded binary **DECOY_KEPT** with the named refusal.
- **`cmd/agent/repo_context_sandbox_test.go` restructured** (necessary, see below).
- **Pre-existing red dispatched** (not caused here): task `01a0b453-77e0-7587-a64a-24bb9e6ba851`.
  That task's fix has since **landed on master** (`f62d1314`, merged via `45551c83`), touching only
  `cmd/agent/merger_restart_e2e_test.go` + `devdoc/cmdagent-repo-context-sandbox.md` — **it shares
  no file with this branch**, so the two are independent (confirmed by a conflict-free merge oracle
  against current master, and by `git log master..` over this branch's four paths).

## Not Done (honest)

- **`TestSpawnersPinTheRepoContext` is red on master** — `merger_restart_e2e_test.go`
  (`mreStartLoop`, `TestMergerRestartReplacesTheLoopWithoutOverlap`) spawn a real
  `gopi merger` without the repo-context sandbox. Measured **identical on base master**
  (`~/workspace/gopi-base-cwd-auth`, 812f9a60, same two offenders), i.e. **not
  introduced by this branch and not fixed here**: it is a distinct defect on another
  area's surface, so it is dispatched with its repro per Rule 26 rather than widened
  into this diff.
- **The guard is authority-scoped, not ownership-scoped.** A registration that no row
  owns but whose repo the ledger *does* name is still freed. That is deliberate (it is
  the pass's entire purpose), and rungs ①–⑤ continue to protect it — but the residual
  risk is unchanged and worth stating: the pass's safety still rests on those rungs,
  so a future widening of the repo set must keep this authority requirement.
- **No CLI verb drives this pass**, so the live round used `gopi worker --once` with
  `GOPI_TASK_CHILD_FAIL_TEST=1` (the existing bounded-round escape).

## Verification

Branch-scoped smoke (Rule 27 / `internal/smokegate`: `make test` semantics are the
declared `scripts/test-budget.sh`; never the whole tree on this path):

- `CGO_ENABLED=0 go build ./...` — exit 0. `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — exit 0.
- `gofmt -l` over the three touched files — no output. `go vet ./internal/tools/` — exit 0.
- `go test -count=1 -v -run '<the four new tests>' ./internal/tools/` — **4/4 PASS** (9.26s).
- `go test -count=1 -run '<touched surface>' ./internal/tools/ ./internal/stylegate/ ./cmd/agent/`
  — the new tests pass; every OTHER failure in those packages reproduces **identically on
  base `812f9a60`** (see Evidence; a separate detached worktree was used, never the live
  main repo — Rule 29).
- A full-package run of `./internal/tools/` and `./cmd/agent/` **exceeded the declared 25m
  per-package budget on a host at load 18.55 on 4 cores**: the harness reported
  `outcome=timeout`, exit 3, and explicitly "NOT an assertion failure". This is a HOST
  condition, not a verdict — the branch-scoped targeted runs above are what the
  acceptance rests on.
- Red→green observed on the new tests (mutation = the pre-fix `add(repo)`), and on the
  live binary A/B.

## Evidence (self-filed)

### Re-measured in the verifying session (2026-09-18, master `32932668`)

Independent re-run of both directions, so neither rests on a single session's transcript.

RED live — a base binary built at master `32932668` (`git worktree add --detach
~/workspace/gopi-base-cwd-auth3 master`, `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`),
driven through the same script (a real `gopi worker --once` round, cwd = a live repo, sandbox
ledger naming only the run's own fixture repo, a clean aged detached decoy):

```
$ sh ~/workspace/_cwd-auth-evid/live-cwd-proof.sh ~/workspace/gopi-base-cwd-auth3/bin/gopi prefix3
--- [prefix3] bin=.../gopi-base-cwd-auth3/bin/gopi  cwd=.../live-cwd-prefix3/live-repo (live repo)
              ledger=.../live-cwd-prefix3/ledger/memory.db (names .../live-cwd-prefix3/own-repo)
              decoy=.../live-cwd-prefix3/decoy
    worker exit=0 (round drove the close-out)
    worker: own-worktree-release: own registered worktree freed: .../live-cwd-prefix3/decoy
      (a worktree of this run's repo that no ledger row owned; branch detached untouched)
RESULT [prefix3]: DECOY_FREED .../live-cwd-prefix3/decoy
```

GREEN live — the SAME script, the same fixture shape, this branch's binary:

```
$ sh ~/workspace/_cwd-auth-evid/live-cwd-proof.sh /home/user/workspace/gopi-cwd-authority/bin/gopi guard3
--- [guard3] bin=.../gopi-cwd-authority/bin/gopi  cwd=.../live-cwd-guard3/live-repo (live repo)
             ledger=.../live-cwd-guard3/ledger/memory.db (names .../live-cwd-guard3/own-repo)
    worker exit=0 (round drove the close-out)
    [task-notify] own registered worktree kept: cwd repo .../live-cwd-guard3/live-repo not enrolled —
      the ledger this process reads names no row carrying it (1 repo(s) named), so this process
      cannot prove it owns it
RESULT [guard3]: DECOY_KEPT  .../live-cwd-guard3/decoy
```

RED unit — the guard-clause mutation re-applied in its own detached worktree
(`git worktree add --detach ~/workspace/gopi-cwd-auth-red 07fa8362`, the guard line replaced by
`if false { // MUTATION: the pre-fix unconditional cwd half`):

```
$ go test -count=1 -v -run 'TestReclaimSelfOwnReposRefusesTheCwdRepoOfAForeignLedger|TestReclaimSelfOwnRegisteredWorktreesKeepsTheDecoyOfAForeignLedger' ./internal/tools/
    the cwd repo .../001/repo was enrolled by a ledger that names it nowhere ([.../001/repo])
      — a spawned round would free every clean worktree of the live repository
--- FAIL: TestReclaimSelfOwnReposRefusesTheCwdRepoOfAForeignLedger (5.15s)
    the DECOY .../001/wt was freed by a pass that cannot prove it owns .../001/repo — this is the
      defect (pass lines: [own registered worktree freed: .../001/wt (a worktree of this run's repo
      that no ledger row owned; branch feat/x untouched)])
--- FAIL: TestReclaimSelfOwnRegisteredWorktreesKeepsTheDecoyOfAForeignLedger (5.38s)
FAIL	github.com/nick/gopi/internal/tools	11.313s
```

GREEN unit — the unmutated branch, same four tests, same session:

```
$ go test -count=1 -v -run 'TestReclaimSelfOwnRepoNamedRequiresAnExactCanonicalMatch|TestReclaimSelfOwnReposRefusesTheCwdRepoOfAForeignLedger|TestReclaimSelfOwnRegisteredWorktreesKeepsTheDecoyOfAForeignLedger|TestReclaimSelfOwnRegisteredWorktreesFreesTheDecoyWhenTheLedgerNamesTheRepo' ./internal/tools/
--- PASS: TestReclaimSelfOwnRepoNamedRequiresAnExactCanonicalMatch (0.00s)
--- PASS: TestReclaimSelfOwnReposRefusesTheCwdRepoOfAForeignLedger (1.37s)
--- PASS: TestReclaimSelfOwnRegisteredWorktreesKeepsTheDecoyOfAForeignLedger (1.06s)
--- PASS: TestReclaimSelfOwnRegisteredWorktreesFreesTheDecoyWhenTheLedgerNamesTheRepo (2.39s)
ok  	github.com/nick/gopi/internal/tools	7.105s

$ go test -count=1 -v -run 'TestRepoContextHazard_TheReleasePassFreesAnUnownedWorktreeOfTheCwdRepo' ./cmd/agent/
--- PASS: TestRepoContextHazard_TheReleasePassFreesAnUnownedWorktreeOfTheCwdRepo (2.67s)
ok  	github.com/nick/gopi/cmd/agent	6.341s
```

`CGO_ENABLED=0 go build ./...` on the branch — exit 0.

The `TestSpawnersPinTheRepoContext` red was re-measured on base `32932668` in
`~/workspace/gopi-base-cwd-auth3` this session — the SAME two offenders
(`merger_restart_e2e_test.go:281 mreStartLoop()`, `:455
TestMergerRestartReplacesTheLoopWithoutOverlap()`) as on the branch, so it is pre-existing and
not branch-introduced (dispatched, per Rule 26).

### Merge oracle against current master

`~/workspace/gopi-oracle2` (detached at then-current master `b5dde57e`) merged this branch with
no conflict:

```
$ git merge --no-commit --no-ff fix/own-release-cwd-authority
Automatic merge went well; stopped before committing as requested
--- conflicts: (none)
$ CGO_ENABLED=0 go build ./...
ORACLE2_BUILD_OK
```

The same oracle re-run and COMMITTED on top of the master current at verification time
(`a97cfdd4`) — conflict-free, and both touched packages green under the merge:

```
a97cfdd4 Merge branch 'fix/own-release-cwd-authority' into HEAD
ok  	github.com/nick/gopi/internal/tools	31.475s
ok  	github.com/nick/gopi/cmd/agent	10.864s
```

### First session's evidence

RED — the mutation is the pre-fix guard clause (unconditional `add(repo)` in
`reclaimSelfOwnRepos`), run in this worktree; the RED evidence is the decoy actually
being deleted:

```
$ go test -count=1 -v -run 'TestReclaimSelfOwnReposRefuses|TestReclaimSelfOwnRegisteredWorktreesKeeps' ./internal/tools/
--- FAIL: TestReclaimSelfOwnReposRefusesTheCwdRepoOfAForeignLedger (0.69s)
    task_reclaim_self_cwd_authority_test.go:161: the cwd repo .../TestReclaimSelfOwnReposRefusesTheCwdRepoOfAForeignLedger1020497104/001/repo
      was enrolled by a ledger that names it nowhere ([.../001/repo]) — a spawned round
      would free every clean worktree of the live repository
--- FAIL: TestReclaimSelfOwnRegisteredWorktreesKeepsTheDecoyOfAForeignLedger (1.68s)
    task_reclaim_self_cwd_authority_test.go:195: the DECOY .../TestReclaimSelfOwnRegisteredWorktreesKeepsTheDecoyOfAForeignLedger1067364137/001/wt
      was freed by a pass that cannot prove it owns .../001/repo — this is the defect (pass lines:
      [own registered worktree freed: .../001/wt (a worktree of this run's repo that no ledger row owned; branch feat/x untouched)])
```

WHY THAT MUTATION AND NOT A LOOSER ONE — a predicate-only mutation
(`return len(named) > 0`) reddens ① only (`a ledger naming only …/other must not
authorise …/repo`) and leaves ②③ GREEN, because the sandbox ledger in those fixtures
names no repo at all — so it does NOT reproduce the defect and cannot stand as the red
proof. The guard-clause mutation is the pre-fix code; only it deletes the decoy.

GREEN — guard restored, same worktree, same tests:

```
--- PASS: TestReclaimSelfOwnRepoNamedRequiresAnExactCanonicalMatch (0.00s)
--- PASS: TestReclaimSelfOwnReposRefusesTheCwdRepoOfAForeignLedger (0.77s)
--- PASS: TestReclaimSelfOwnRegisteredWorktreesKeepsTheDecoyOfAForeignLedger (1.39s)
--- PASS: TestReclaimSelfOwnRegisteredWorktreesFreesTheDecoyWhenTheLedgerNamesTheRepo (4.54s)
ok  	github.com/nick/gopi/internal/tools	9.258s
```

LIVE A/B, re-run by this session against a base binary built at `812f9a60` (script
`~/workspace/_cwd-auth-evid/live-cwd-proof.sh`; a real `gopi worker --once` round whose
CWD is a live repository holding a clean, detached, aged decoy, with a sandboxed ledger
naming only the run's OWN fixture repo):

```
--- [prefix2] bin=.../gopi-base-cwd-auth2/bin/gopi  cwd=.../live-cwd-prefix2/live-repo (live repo)
              ledger=.../live-cwd-prefix2/ledger/memory.db (names .../live-cwd-prefix2/own-repo)
              decoy=.../live-cwd-prefix2/decoy
    worker exit=0 (round drove the close-out)
    worker: own-worktree-release: own registered worktree freed: .../live-cwd-prefix2/decoy
      (a worktree of this run's repo that no ledger row owned; branch detached untouched)
RESULT [prefix2]: DECOY_FREED .../live-cwd-prefix2/decoy

--- [guard2]  bin=~/workspace/gopi-cwd-authority/bin/gopi  cwd=.../live-cwd-guard2/live-repo (live repo)
              ledger=.../live-cwd-guard2/ledger/memory.db (names .../live-cwd-guard2/own-repo)
    worker exit=0 (round drove the close-out)
    [task-notify] own registered worktree kept: cwd repo .../live-cwd-guard2/live-repo not enrolled —
      the ledger this process reads names no row carrying it (1 repo(s) named), so this process
      cannot prove it owns it
RESULT [guard2]: DECOY_KEPT  .../live-cwd-guard2/decoy
```

PRE-EXISTING RED — each reproduced on base `812f9a60` in a separate detached worktree
(`~/workspace/gopi-base-cwd-auth2`), same failures and same offenders, so none is
branch-introduced:

```
$ go test -count=1 -run 'FirstOpenSyncModeDecisionTable|MergeBuildGate_GoCacheLeavesNoScratchCache' ./internal/tools/
--- FAIL: TestFirstOpenSyncModeDecisionTable (0.00s)              # base AND branch
    --- FAIL: TestFirstOpenSyncModeDecisionTable/memory-filesystem-ledger
--- FAIL: TestMergeBuildGate_GoCacheLeavesNoScratchCache (0.00s)  # base AND branch

$ go test -count=1 -run TestSpawnersPinTheRepoContext ./cmd/agent/
--- FAIL: TestSpawnersPinTheRepoContext (0.35s)                   # base AND branch, same two offenders
    2 function(s) spawn a real gopi worker/merger WITHOUT the repo-context sandbox:
      merger_restart_e2e_test.go:281: mreStartLoop()
      merger_restart_e2e_test.go:455: TestMergerRestartReplacesTheLoopWithoutOverlap()

$ go test -count=1 -run TestStyleGate ./internal/stylegate/       # byte-identical gate lines on base AND branch
    gate_test.go:139: undecidable sites (reported, never guessed): 150
    gate_test.go:174: stylegate test-sleep: 2 test hit(s), ceiling 0
    gate_test.go:174: stylegate test-skip: 1 test hit(s), ceiling 0
    gate_test.go:174: stylegate errorf-verb: 8 production hit(s), ceiling 7
    gate_test.go:174: stylegate ignored-error: 12 production hit(s), ceiling 0
    gate_test.go:174: stylegate ignored-error: 197 test hit(s), ceiling 163
(grep of that output for the three files this branch touches: 0 hits)
```

CONTROL — the touched cmd/agent guard passes on this branch, i.e. the restraint this
branch adds holds on the in-process spawner path too:

```
$ go test -count=1 -v -run 'TestRepoContextHazard_TheReleasePassFreesAnUnownedWorktreeOfTheCwdRepo' ./cmd/agent/
--- PASS: TestRepoContextHazard_TheReleasePassFreesAnUnownedWorktreeOfTheCwdRepo (4.48s)
```

