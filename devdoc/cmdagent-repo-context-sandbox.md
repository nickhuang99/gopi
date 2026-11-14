<!-- doc-class: historical -->
# Task Report: the `cmd/agent` suite sandboxes the REPO CONTEXT of every real-gopi spawner

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch: `fix/cmdagent-repo-context-sandbox` (worktree `~/workspace/gopi-repo-sandbox`, based on master `796b3333`).

## Completed

### The defect, corrected from the briefing
The epic named `ReclaimOwnFinishedWorktrees` as the deleting pass. Measurement
(and the report string) say otherwise: the line

```
worker: own-worktree-release: own registered worktree freed: /home/user/workspace/gopi-v1-clean
   (a worktree of this run's repo that no ledger row owned; branch detached untouched)
```

is emitted by the **ownership-independent** producer arm
`ReclaimSelfOwnRegisteredWorktrees` (`internal/tools/task_reclaim_self.go:363`,
marker at :389, line at :478), called from
`cmd/agent/cli_tasks_worker.go:172` (`workerRoundOwnWorktrees`, deferred at the
end of every worker round) and `cmd/agent/cli_tasks.go:1834`
(`gopi task run`'s close-out).

Its repo set is `reclaimSelfOwnRepos()`:

1. the ledger repos of the rows **this process** claims, and
2. **the repo of the process's own CWD** (`git -C <cwd> worktree list`, first
   porcelain record).

So a ledger-only pin is half a sandbox, and the empty-`repos`-settings trick is
not sufficient either: ② always contributes the live repository, whatever the
settings table says. The guard ladder is: age ≥ `reclaimSelfOwnMinAge` (5 min,
measured from the registration's own `gitdir`/`HEAD` creation record), no live
holder (`scratch.HeldByProcesses`), a clean two-state reading, no live ledger row
naming it, and containment inside a declared worktree root (`<home>/workspace` by
default; `worktreeroots` env override wins with no settings file).

The logged culprit's shape is confirmed: `serve_worker_split_test.go`
`TestWorkerWaitsAndPicksUpNewTaskWithoutRestart` calls `runTaskWorker(false)`
**in-process**, so the round's release pass ran with the test process's cwd — the
live repository.

### What landed
1. **`cmd/agent/repo_context_sandbox_test.go`** — the pin. `SandboxRepoContext(t)`
   pins **all three** of the pass's inputs and re-measures each of them:
   - an empty `{"repos":{}}` settings file (`config.SettingsEnv`), key from
     `config.ReposSettingsKey`;
   - a fresh (absent) ledger file (`config.MemoryDBEnv`, name from
     `config.MemoryDBFileName`) — **measured necessary**: with the settings and
     cwd pinned but the ledger ambient, the pass resolved **six** of the host's
     registrations from the operator's own ledger;
   - the process's cwd moved into a fresh, **non-git** sandbox directory under
     `testutil.SuiteTempRoot()` (off `/`, Rule 22), undone by `t.Cleanup` — a
     `defer` would have restored the live repo the moment the helper returned
     (this was a real bug I hit and fixed);
     and it **fails loudly** if the cwd still resolves to a repo or the settings
     table is non-empty (Rule 15, no silent degradation).
   Plus `AssertProducerPassResolvesNoRepo` (runs the product's own pass and
   requires no repo resolved / nothing acted on), and the hazard fixture helpers
   `PlantUnownedWorktree` / `AgeWorktreeForRelease` / `WorktreeRegistered`.
2. **The pin applied at 43 lifecycle spawn sites in 23 pre-existing test files**
   (the gate scans 46 sites over 25 files, the two added files included) — including
   the logged culprit, the merger daemon/singleton/lock/restart E2Es, the
   worker daemon E2Es, the `task run` E2Es, the agent-spawn-guard E2Es and the
   lifecycle tick suite. Two sites were fixed by a **directory pin** rather than
   a chdir (their children need their own repo as `cmd.Dir`), and three
   spawn-helper functions whose env is built by an already-pinned caller carry a
   reviewed `// repo-context-ok:` escape (they keep appearing in the gate's log
   line rather than disappearing into a suppression).
3. **`cmd/agent/repo_context_sandbox_guard_test.go`** — the **update clause**, so
   the class cannot come back: a textual/AST scan of `*_test.go` flags any
   function that spawns the real gopi with a worker/merger/`task run` verb
   without one of the three pin shapes, with a read-floor (≥15 sites) so a
   drifted glob cannot pass vacuously. Current verdict: **36 sites scanned, 33
   pinned, 3 documented escapes**.
4. **`TestRepoContextHazard_TheReleasePassFreesAnUnownedWorktreeOfTheCwdRepo`** —
   the regression guard, two halves through the product's own code path:
   red (settings+ledger pinned, cwd a repo) the pass **frees** a planted clean,
   unheld, 10-minute-old unowned worktree; green (cwd sandboxed) the same plant
   **survives**. The red half self-skips with a loud message if the plant stops
   reproducing, so a drift in the hazard's shape cannot turn this into a vacuous
   pass.

### Attribution method (why the acceptance evidence is trustworthy)
Three failed attempts to demonstrate "the suite deletes a live worktree" taught
the crucial fact: **`/home/user/workspace` is a shared, actively-hostile environment**.
Per-process tracing (`/proc/*/fd` on every running gopi, plus a `git worktree
remove` exec watcher) showed the victim being deleted by **another agent's**
branches — the removals were executed by `agent.test` processes whose parent
chain was `go test -json ./...` under `/home/user/workspace/gopi-heavy-tests`, i.e. a
sibling task's UNPINNED suite, not mine. That sibling also deleted two of my own
throwaway worktrees. So the correct acceptance question is **not** "does the
victim survive on this host" (nothing survives on this host) but **"does MY
suite's execution reach out of its sandbox"** — answered directly: with the
exec watcher running, the ONLY removal my suite performed was
`git worktree remove /dev/shm/gopi-test-scratch/.../TestAcceptance_.../wt`, a
fixture inside its own temp tree. Nothing of the developer's.

### Red proof on the real culprit (the strongest evidence)
The epic's own culprit test was mutated: with its `SandboxRepoContext(t)` line
removed, `TestWorkerWaitsAndPicksUpNewTaskWithoutRestart` ran on a planted clean
detached `/home/user/workspace/gopi-victim-acceptance` (master tip, back-dated 10 minutes)
and **deleted it**; with the pin restored the same test left it in place
(`git worktree list` still named it). That is the pre-fix loss reproduced on the
live hazard and the fix demonstrated on the same code path — the ideal the epic
asked for. The same mutation was applied to the new pin itself (cwd half
removed): the hazard test fails, restored it passes.

### Test-suite status vs master (no regressions)
`go test -count=1 ./cmd/agent/` on this branch: 9 failures,
`TestCLIContractTaskServeAliasPrintsOneLinePointer`,
`TestTasksCLIListHeaderCountMatchesRows`, `TestTmpDirValueProbeLeavesNothingBehind`,
`TestWorkerDaemon_{DetachesAndKeepsRunning,LeavesTheLoginSessionScope,RequireScopeIsHardWhenUnavailable}`,
`TestMergerDaemon_LeavesTheLoginSessionScope`, `TestMergerFlock_DaemonLockStopsWithTheDaemon`,
`TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis` — **the same set fails
at master** (measured in a clean master worktree, `BASE_EXIT=1`). They are
host/environment failures (cgroup-scope and user-bus probes on this box, with
other agents' gopi daemons running concurrently), not regressions.
The one regression this change DID introduce — `TestTaskRunChildSIGKILLClosesWrapperRowE2E`,
whose child resolves the task's deliverable repo from its cwd — was found by that
comparison, root-caused (the row's `deliverable.repo` is what the wrapper hands
the child, so a submit from the live repository re-points the child there), and
fixed by submitting the row from a fixture repo (commit `0655b3e8`); the test
passes again.

## Not Done (honest)

- The pass's **count** semantics are untouched by design: this is a test-hermeticity
  fix, not a change to what a finished run may free (the epic's non-goal).
- The sweep arm (`worktreeSweepRepos`) also unions the cwd repo; it is covered
  here only insofar as the same pin removes the cwd repo from the child's reach.
  No sweep-specific test was added.
- `go test ./...` (the whole repository) was **not** run: this host currently
  runs several sibling agents' suites that fail pre-existing environmental tests,
  so a repo-wide run would neither be attributable nor cheap. The scoped
  evidence above (package suite + baseline diff + exec-watcher attribution) is
  what this report claims.
- The hazard remains live for **other** worktrees on this host: the sibling
  branches (`gopi-heavy-tests`, `gopi-worker-ownership3`, …) run the UNPINNED
  code, so their suites keep deleting the same class of worktree. That is a
  dispatch item, not something this branch can fix (their code is their own).
- The `/home/user/workspace` victims used for the mutation proof were themselves removed
  by those sibling suites mid-run; the proof was still obtained (the attribution
  watcher recorded which process did it, which is how the sibling was found).

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (needed
  because this package's tests assert the binary is fresh).
- **Gate**: `go test -count=1 -run TestSpawnersPinTheRepoContext ./cmd/agent/`
  → PASS (`repo-context gate: 46 lifecycle spawn site(s) scanned, 43 pinned, 3 escaped`).
- **Hazard red/green**: `go test -count=1 -run TestRepoContextHazard ./cmd/agent/`
  → PASS. **Mutation red proof**: with the cwd pin removed, the same test FAILS
  (`the producer pass acted on 7 registration(s) …`); restored, it PASSES.
- **End-to-end acceptance, mutation-proved**: a clean detached worktree
  `~/workspace/gopi-victim-acceptance` (master tip, back-dated 10 minutes) was
  planted, then `TestWorkerWaitsAndPicksUpNewTaskWithoutRestart` was run with its
  `SandboxRepoContext(t)` line **removed** → the test PASSED and the **victim was
  deleted** (`ls` → No such file or directory). With the pin restored → the test
  PASSES and the victim **survives** (`git worktree list` still names it). That is
  the pre-fix loss reproduced and the fix demonstrated on the live hazard.
- **Live hazard, observed**: during this very task a resident `gopi worker` freed
  the author's worktree (`own registered worktree freed: /home/user/workspace/gopi-wis-redproof`),
  the same defect class, mid-fix.
- **gofmt**: touched files clean (three files were already unformatted on master
  and were left alone).

## Commit

Branch `fix/cmdagent-repo-context-sandbox`, tip `fd5a9d6b` (5 commits on master
`796b3333`):

- `c6df765a` fix(cmd/agent): sandbox the repo context of every real-gopi spawner
- `fdc5eb38` docs(cmd/agent): report on the repo-context sandbox (mechanism correction, recipe, red proof)
- `11d087bd` fix(cmd/agent): extend the repo-context pin to the IN-PROCESS lifecycle drivers
- `0655b3e8` fix(cmd/agent): the task-run E2E hands its child a fixture repo, not the live one
- `fd5a9d6b` docs(cmd/agent): report the repo-context sandbox; add the attribution scripts

Shared-repo worktree (`~/workspace/gopi-repo-sandbox`), so the commits are
already on the branch — no push needed (Rule 6c).

## Follow-up: the two merger-restart spawn sites (task 01a0b453, 2026-09-18)

Master `812f9a60` was RED again on this gate — two functions the FIRST adoption
round did not reach, both in `cmd/agent/merger_restart_e2e_test.go`:

```
2 function(s) spawn a real gopi worker/merger WITHOUT the repo-context sandbox:
      merger_restart_e2e_test.go:281: mreStartLoop()
      merger_restart_e2e_test.go:455: TestMergerRestartReplacesTheLoopWithoutOverlap()
```

**Why the gate caught them and the earlier audit did not.** `mreStartLoop` is a
package-local helper: the gate's `rcsLocalRunnerNames` recognizes `runGopiIn`
and its siblings, but this rig spawns with a raw `exec.Command` whose argv is
built at the call site — so the SPAWN is visible, and the missing pin is visible,
but only because the helper was scanned as a spawn site. Its three callers (C2,
C4, C6) inherit the pin from the helper; C4 needed its own because the loop a
`--restart` installs is spawned by the CONTROL FACE (a second real merger child,
never this test's child).

**The pin, applied:** `SandboxRepoContext(t)` inside `mreStartLoop`, and in C4.
`mreProfile` already pinned the settings table and the ledger; the cwd was the
only missing input. Mutation-proved in both directions (remove the helper pin →
`mreStartLoop` is the single offender; remove C4's pin with the helper intact →
C4 is the single offender).

**Live A/B (the hazard, read from `/proc`).** While the pinned suite ran, its
merger child stood in the sandbox:

```
pid=444615 cwd=…/suite-cmd-agent-run-688882007/gopi-repo-ctx-3830607525  argv=…/gopi merger --every 30s
```

At the same moment a FOREIGN, UNPINNED run of this very test sat in a live
worktree — the hazard as an observation, not a hypothesis:

```
pid=2592429 cwd=/home/user/workspace/gopi-arch-recover-4/cmd/agent  argv=…/merger-restart-bin/gopi merger --every 30s
```

**The gate still has an entry-point hole — measured, deliberately NOT closed
here** (Rule 26: a narrow fix is the right shape). Adding `wdRun` — this rig's
real-binary runner, used with merger verbs by C5 — to `rcsLocalRunnerNames` turns
the gate RED with **19 offenders**: `daemon_cgroup_e2e_test.go` (3),
`daemon_userbus_seam_test.go` (3), `merger_restart_e2e_test.go` (6, incl.
`mreStopViaFace`), `merger_retired_flags_test.go` (3), `merger_status_cli_test.go`
(1), `worker_daemon_singleton_e2e_test.go` (`wsStartDaemon`),
`worker_foreground_console_e2e_test.go` (1). That is the historical holed-gate
failure ("a gate with an incomplete entry point list reports a smaller, cleaner
number, not a safer tree") at a third entry point, and it belongs in its own task
with its own read-floor and false-positive audit — not smuggled into a two-site
pin. The probe was reverted; this branch changes exactly one test file.

**C2 (`TestMergerStopEndsALiveLoop`) is red in this environment, and it is not
this change.** It PASSES alone (2.25s) and fails in a 4-case batch at the
product's 60s `mergerStopDeliverTimeout` (`pid <n> is still running after 1m0s`) —
a merger answers a stop only at its tick boundary and its FIRST tick is a full
merger pass. Identical to the pre-existing red master already recorded in
`devdoc/merger-stop-takeover-contract.md` (3/3 FAIL on base `9ce8e159`, host
contention, filed separately) and owned by ledger row `01a0b343`.
