<!-- doc-class: historical -->
# Task Report: the close-out must also read the MAIN repo (clean + HEAD on the integration branch)

Epic: `01a0cd9c-a313-71ea-bac8-8cbd1c037a68` — "close-out must read the MAIN repo:
clean + HEAD on integration branch (two checklist lines, no subsystem)".
Branch: `task/01a0cd9ca31371eabac88cbd1c037a68`
Worktree: `/home/user/workspace/gopi-01a0cd9ca31371eabac88cbd1c037a68`

## Completed

### The defect

`taskFinishCleanupChain` read cleanliness through ONE reader
(`finishCleanupWorktreeDirtyReason` / `finishCleanupProbeClean`) whose subject was
the RUN'S OWN worktree. Nothing in the close-out path asked the symmetric question
about the MAIN checkout. Measured 2026-09-23 on `~/work/GoPi` itself: a verify child
whose ground was `~/workspace/gopi-ccp-verify-doc` left an untracked
`tasks/rework-chat-colour-palette-union-proof.txt` and `.g3rep/gopi/memory.db` in the
main checkout; the child closed `completed`, its own worktree was clean, every
existing arm reported "clean", and the residue stayed.

### The two lines, and where they are wired

`finishCleanupMainRepoGate` (`internal/tools/task_finish_cleanup.go`) is called from
THREE places, all sharing the report's own `mainRepoChecked` so the two git readings
happen **once per close-out** (Rules 13/14):

1. `taskFinishCleanup`'s entry, immediately after the existing ①a
   `FinishCleanupRestoreMainCheckout` call and **before the server/client mode
   split** — because there are two chain bodies and every guard above this point
   returns early, and each of those returns is a run that can still have left its
   by-products in the checkout it was launched from (the same argument ①a is
   placed there for).
2. `finishCleanupServerRecycle` (server/single-node arm).
3. `runFinishCleanupChainTarget` (client arm) — the seam the tests inject a
   deliverable into, so an injected fixture is guarded too.

The check itself, `finishCleanupMainRepoReason`, is **two git commands** in the MAIN
checkout (`finishCleanupMainRepoDir(d.Repo)` resolves it through the merge
executor's own `--git-common-dir` convention):

```
git -C <main> status --porcelain          → through finishCleanupWorktreeDirtyReason (the ONE dirty reader)
git -C <main> symbolic-ref --short HEAD   → compared with IntegrationBranchForRepo(<main>)
```

- **(1) clean** is decided by `finishCleanupWorktreeDirtyReason` — the reader the
  three removal arms already share — so the main checkout's two dirty states keep the
  two names that reader gives them (`…tracked changes:` vs `…carries ONLY UNTRACKED
  files…`). **No second reading of `git status` exists anywhere in the change.** The
  reader is passed `nil` for the report because its untracked-only note is a step
  against `rep.Worktree`, i.e. a *different* directory; the gate records its own
  main-repo step instead, so nothing is mis-attributed.
- **(2) HEAD on the integration branch** is `git -C <main> symbolic-ref --short HEAD`
  vs `IntegrationBranchForRepo(<main>)` — the same resolution the ①a restore uses,
  so the two cannot disagree about which branch a landing needs.

A violation is a **refusal that stops the chain**, in the chain's existing
`SkipReason` shape, carrying the repo path, the offending bytes (git's own porcelain
lines, or both branch names) and the operator repair, e.g.

```
close-out refused: MAIN repo dirty (the checkout the run was launched from, not this
run's worktree) — /…/main: worktree dirty (live bond protected) — carries ONLY
UNTRACKED files (build output / scratch residue), a run's own hygiene, not uncommitted
work: ?? tasks-residue.txt — inspect it by hand; this close-out refuses rather than
cleaning (no git clean / checkout -- / stash / commit is run here)

close-out refused: MAIN repo HEAD is not on the integration branch — /…/main: HEAD is
on fix/wrong-branch, expected master — a landing fast-forwards whatever this checkout
holds, so put it back: git -C <main> checkout master
```

Nothing is repaired. No `git clean`, no `checkout --`, no `stash`, no auto-commit —
the guard reports and stops (Rules 15 + 30-repair-direction).

### Honest scope: when the check applies

`finishCleanupMainRepoCheckApplies` asks it when the MAIN checkout the repo argument
belongs to is **not** the chain's own GROUND (recorded `deliverable.worktree`, else
the repo argument). Both real shapes qualify:

- `repo=<main>, worktree=<linked wt>` — the ticket's measured shape;
- `repo=<linked wt>, worktree=absent` — Rule 6a's executor backfill (this very run's
  row has `deliverable.repo=/home/user/work/GoPi` while its ground is the worktree).

When the main checkout IS the run's ground (a manual run inside it, or a row recording
it as its own worktree) the chain's existing dirty reading already covers that
directory, and a second refusal over the same bytes would replace a report the
operator knows how to read with a menu of two reasons. On that shape, and on a shape
where no main repo resolves, the report carries
`MainRepoCheckSkipped="MAIN repo clean + HEAD on the integration branch (not this
shape's question: the repo argument IS the run's own ground, which the chain's own
dirty gate reads)"` — an explicit forward-reading statement, never a silent pass
(Rule 15).

### Visibility

Every main-repo outcome is reportable: `FinishCleanupSkipIsReportable` now consumes
`finishCleanupMainRepoMarkers` (dirt / parked HEAD / either unreadable read), so the
refusal reaches the sweep log instead of staying quiet. Verified on the real verb:

```
finish-cleanup (APPLY): candidates=1 removed=0 settled=0 refused=1
```

### Not weakened

No existing arm changed tolerance or ordering: the dirty gates, the
`finishCleanupWorktreeInUse` liveness gate, `finishCleanupTargetLeavesTestSpace`, the
push→removal→branch ordering, `finishCleanupOwnerDeclaredReason`, the ledger-liveness
gates and `reclaimGroundChecklistVerdict` are untouched. The guard only ever *adds* a
stop, and only before any step of the chain has run.

## Not Done (honest)

1. **The residue's producer is not fixed here.** The ticket's "Not this ticket"
   section says so explicitly: a task's by-product belongs in its own
   `~/.gopi/sessions/<task_id>/` container, which does not exist yet. This change
   makes the writer STOP (the close-out refuses) rather than making the residue
   invisible; the container question stays its own ticket.
2. **The sweep paths that do not go through this chain.** The check is wired into the
   finish-cleanup chain (all three entry seams). The reclaim arms
   (`task_reclaim*.go`, `worktree_sweep.go`) and `task_merged_cleanup.go` free
   worktrees without running this chain; they were deliberately NOT changed — the
   ticket's boundary is the close-out checklist, and adding a main-repo refusal to
   the reclaim sweep would be a different (and much wider) change of behaviour.
3. **`gopi merge --fast`'s own landing path** already repairs a parked main checkout
   (`FinishCleanupMainCheckoutRestore`, repo_sweep.go:1259 / merge_run.go:1336) and
   was left as is: this change refuses at CLOSE-OUT, it does not add a second repair.
4. **Not verified on a second host.** The Rule 31 run was on peerbox against a purpose
   built fixture; a client-node (HP) end-to-end run of the same verb was out of this
   session's reach.

## Files

- `internal/tools/task_finish_cleanup.go` — the check, its vocabulary, the wiring at
  the three seams, the `MainRepoCheckSkipped` report field and the reportable-marker
  list (+337/−11).
- `internal/tools/task_finish_cleanup_mainrepo_test.go` — NEW, the RED-first suite
  (6 tests, all over a real git fixture).
- `devdoc/closeout-main-repo-check.md` — this report's repo copy.

## Verification

- **Build:** `go build ./...` → no output (OK); `CGO_ENABLED=0 go build -o bin/gopi
  ./cmd/agent/` → OK, `./bin/gopi --version` → `dev`.
- **Vet:** `go vet ./...` → no output (OK).
- **Tests (targeted, the diff's own surface):**
  `go test ./internal/tools/ -run 'TestFinishCleanup|TestTaskFinishCleanup|TestMainCheckout' -count=1`
  → `ok github.com/nick/gopi/internal/tools 56.143s`.
  `go test ./internal/tools/ -run 'FinishCleanup|Reclaim|MergedCleanup|WorktreeSweep|GroundChecklist|WorktreeRemoval|MainCheckout|TaskMerge' -count=1`
  → `ok github.com/nick/gopi/internal/tools 89.398s` (the reclaim/sweep/checklist
  neighbours, i.e. the arms this change must not weaken).
  Full `./internal/tools/ ./cmd/agent/` run: see `## Evidence`.
- **`internal/standards/`:** RED, and **pre-existing on the merge base** — the suite
  reports `Rule 17 (test env isolation): NEW violation … at
  internal/tools/tasks_test.go:1109 os.Setenv/os.Unsetenv … + STALE baseline entry
  internal/tools/tasks_test.go:1133`, which is the *unrelated* open bug row
  `01a0cd7e` ("Rule 17 standards gate is RED on pristine master 56f665a5 —
  internal/tools/tasks_test.go:1109 os.Unsetenv + stale baseline entry :1133",
  status=failed) verbatim. My diff touches neither line; `git status --porcelain`
  in the worktree lists only the two files above.

### Ran / Result (Rule 31 — the real surface, not `-h`)

Fixture: a purpose-built two-node world off the source tree
(`/home/user/workspace/gopi-r31-mainrepo`: bare `server.git` + main checkout `main`
on `master` + linked worktree `wt` on `feat/deliverable`), its own ledger
(`--db …/scratch/memory.db`) and settings, row driven `claimed → in_progress →
completed`, deliverable = `{repo: …/main, branch: feat/deliverable, worktree: …/wt}`.

**Ran** (dirty main checkout, run's own worktree clean):

```
cd /home/user/workspace/gopi-r31-mainrepo/main
git status --porcelain                 # (clean)
echo RESIDUE > tasks-residue.txt       # an untracked by-product in the MAIN repo
…/bin/gopi task finish-cleanup --apply --task 01a0cdae-4650-741e-a209-acd03f520ae0 \
    --db /home/user/workspace/gopi-r31-mainrepo/scratch/memory.db
```

**Result:**

```
01a0cdae-4650-741e-a209-acd03f520ae0 skipped: close-out refused: MAIN repo dirty (the
checkout the run was launched from, not this run's worktree) —
/home/user/workspace/gopi-r31-mainrepo/main: worktree dirty (live bond protected) —
carries ONLY UNTRACKED files (build output / scratch residue), a run's own hygiene,
not uncommitted work: ?? tasks-residue.txt — inspect it by hand; this close-out
refuses rather than cleaning (no git clean / checkout -- / stash / commit is run here)
finish-cleanup (APPLY): candidates=1 removed=0 settled=0 refused=1
```

Queried afterwards: `ls …/main/tasks-residue.txt` → present (the guard did not clean);
`ls -d …/wt` → present and `git -C …/wt status --porcelain` → empty (the run's own
worktree is clean — the exact asymmetry of the ticket, and nothing was removed).

**Ran** (same row, main checkout parked off the integration branch):

```
cd …/main && git worktree add -q ../wt feat/deliverable && git checkout -q -b fix/wrong-branch
git symbolic-ref --short HEAD          → fix/wrong-branch
…/bin/gopi task finish-cleanup --apply --task 01a0cdae-4650-741e-a209-acd03f520ae0 --db …
```

**Result:**

```
01a0cdae-4650-741e-a209-acd03f520ae0 skipped: close-out refused: MAIN repo HEAD is not
on the integration branch — /home/user/workspace/gopi-r31-mainrepo/main: HEAD is on
fix/wrong-branch, expected master — a landing fast-forwards whatever this checkout
holds, so put it back: git -C <main> checkout master
finish-cleanup (APPLY): candidates=1 removed=0 settled=0 refused=1
```

**Ran** (both repairs done by hand — the repair the refusal names — then the same verb):

```
git checkout master in …/main, rm …/main/tasks-residue.txt
…/bin/gopi task finish-cleanup --apply --task 01a0cdae-4650-741e-a209-acd03f520ae0 --db …
```

**Result:** `01a0cdae-… branch=feat/deliverable done=[align-before-delivery,pre-merge,worktree]`
/ `finish-cleanup (APPLY): candidates=1 removed=1 settled=0 refused=0` — **no false
refusal** on the clean, correctly-parked main repo, and the chain did its ordinary job.

**Ran** (dry run, same dirty fixture):

```
echo x > plan-residue.txt
…/bin/gopi task finish-cleanup --dry-run --task 01a0cdae-4650-741e-a209-acd03f520ae0 --db …
```

**Result:** the plan prints the same refusal —
`close-out refused: MAIN repo dirty … ?? plan-residue.txt …` /
`finish-cleanup (dry-run): candidates=1 would-run=0 settled=0 refused=1` — so a dry
run cannot promise a removal `--apply` refuses.

## Evidence (self-filed)

- **RED before the fix** (the guard's two call sites replaced by `_ =
  finishCleanupMainRepoGate`, i.e. the pre-fix tree; nothing else changed —
  `go test ./internal/tools/ -run
  'TestFinishCleanupRefusesDirtyMainRepo|TestFinishCleanupRefusesMainRepoHeadOffIntegrationBranch|TestFinishCleanupCleanMainRepoDoesNotRefuse' -count=1`):
  ```
  --- FAIL: TestFinishCleanupRefusesDirtyMainRepo (0.52s)
      task_finish_cleanup_mainrepo_test.go:80: a dirty MAIN repo must refuse the close-out, got &{… Skipped:false …
      Steps:[{Step:push … OK:true …} {Step:worktree … OK:true Detail:} {Step:branch … OK:true}] …}
  --- FAIL: TestFinishCleanupRefusesMainRepoHeadOffIntegrationBranch (0.27s)
      task_finish_cleanup_mainrepo_test.go:128: a MAIN repo parked off the integration branch must refuse the close-out, got &{… Skipped:false …}
  FAIL
  FAIL	github.com/nick/gopi/internal/tools	1.531s
  ```
  The RED is the DEFECT itself: with no main-repo guard the chain pushed the branch,
  removed the worktree and deleted the branch while the main checkout carried the
  residue / was parked on `fix/ground-e2e-ledger-dsn`.
- **GREEN after** (`go test ./internal/tools/ -run 'TestFinishCleanupMainRepo|TestFinishCleanupRefuses|TestFinishCleanupCleanMainRepo' -count=1`):
  `ok github.com/nick/gopi/internal/tools 3.029s`; then the widened surface
  `-run 'TestFinishCleanup' -count=1` → `ok … 39.068s`.
- **Reused reader, not a second one** (Rule 14): `grep -n 'git status\|statusVerb'
  internal/tools/task_finish_cleanup.go` shows exactly one `finishCleanupStatusVerb`
  call path for the main repo, whose porcelain is handed to
  `finishCleanupWorktreeDirtyReason` — the same bytes-to-name mapping the worktree arm
  uses. `TestFinishCleanupMainRepoGateReadsThroughTheOneDirtyReader` pins both names
  (`…carries ONLY UNTRACKED files…` / `…tracked changes: …`) coming out of that reader
  for the main checkout.
- **No auto-clean** (measured, not asserted): the residue file was still on disk after
  the refusal (`ls …/main/tasks-residue.txt` → present) and the run's own worktree was
  untouched and clean.
- **Reportable is a union, not a replacement**: `FinishCleanupSkipIsReportable` keeps
  its three original markers and appends `finishCleanupMainRepoMarkers`.
