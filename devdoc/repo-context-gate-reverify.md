<!-- doc-class: normative -->
# Re-run of the repo-context spawner gate on current master (2026-09-16)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch: `fix/repo-context-gate-reverify` (worktree `~/workspace/gopi-rectx-verify`)
Base: master `0dac2ae7` + `fix/cmdagent-repo-context-sandbox` merged in (merge commit `6bf2a37a`).

## Why this re-run existed

The fix branch's report ended with `repo-context gate: 46 lifecycle spawn site(s)
scanned, 43 pinned, 3 escaped`. A number in a report is a claim about the tree at
that moment, and the tree keeps moving: master gained commits after the fix
branch's merge-base (`796b3333`), and **14 live local branches** carry their own
`cmd/agent/*_test.go` commits on top of it. Publishing the count again without
re-measuring would have been a copy, not a measurement.

Measured now, on `master + fix/cmdagent-repo-context-sandbox`:

```
repo-context gate: 53 lifecycle spawn site(s) scanned, 50 pinned, 3 escaped
```

`46/43/3` was therefore an **undercount**, and the 7-site difference is *not*
master drift: master drift is zero (`git diff 796b3333..master -- 'cmd/agent/*_test.go'`
introduces no spawn shape; its two files are ledger-probe tests). The gap was in
the **gate's own recognition set** — two holes, both closed here.

## The two holes (both measured, not inferred)

### 1. The package-local spawn helpers were not spawn entry points

`rcsIsSpawnCall` recognized `exec.Command`, `exec.CommandContext`, the testutil
runners and the producer passes. It did **not** recognize `runGopiIn` — this
package's own helper, which ends in `exec.Command(bin, args...)` on the real gopi
binary. Master reaches real lifecycle verbs through it:

- `merger_stop_force_modifier_test.go:102` — `gopi merger --stop [--force]`
- `cli_tasks_worker_unknown_arg_test.go:224` — `gopi worker --once` (STARTS the loop)
- `cmd/agent/peak_guard_removal_test.go:267` — the peak A/B, including `task run` and `worker --once`
- `status_action_conflict_e2e_test.go:356` — the `--daemon/--status` collision family

Each of those children inherited the package directory — the live repository —
while the gate reported a clean, smaller tree. **A gate with an incomplete entry
point list does not report a safer tree, it reports a smaller number.**

Fix: `rcsLocalRunnerNames` (`runGopiIn`, `runGopiInDir`, `runGopiInWithPeakEnv`,
`runCLIContractProbeEnv`, `runGopiForVersion`, `runGopiVersionSandboxed`,
`runGopiForVersionHome`) is checked in `rcsIsSpawnCall` for both the qualified and
the package-local call shape.

### 2. A spread argv made the call read as verbless

`runGopiIn(t, bin, env, "", tc.argv...)` carries no verb *elements* at the call
site — the argv lives in the enclosing function's table. The call therefore
matched "spawn call" but never "lifecycle verb", and the site was not counted at
all. Fix: `rcsExprIsSpreadOfNamedSlice` (now including the qualified
`tc.argv...` shape) marks the argv as **statically unresolvable**, and in exactly
that case `rcsFunctionArgvNamesLifecycleVerb` reads the enclosing function's own
argv-building expressions (`[]string{…}`, `argv = append(argv, …)`) with the same
adjacency rule.

Two over-inclusions this produced were then excluded on **product semantics**, not
to make a number look good:

- `-h`/`--help` is the product's zero-side-effect usage query (`rcsArgvIsHelpQuery`,
  checked first): `worker -h`, `task run -h` start nothing, claim nothing, run no
  close-out — regardless of the verb beside them.
- `worker --status` / `worker --stop` (read-only controls) were already excluded;
  a pattern parameter (`argv = append(argv, tc.argv...)`) now inherits the
  argument's static elements, so `status_action_conflict_e2e_test.go`'s collision
  family is still measured as the collision it is.

## The five newly pinned sites

All five were confirmed unpinned by measurement (each ran with the process cwd =
the live repository), and each received the pin its own child needs:

| site | why the pin | pin chosen |
| --- | --- | --- |
| `merger_stop_force_modifier_test.go` `TestMergerStopForceAcceptedByCLI` | real merger child | `SandboxRepoContext` |
| `status_action_conflict_e2e_test.go` `TestAliasDaemonStatusIsRefused` | real merger child | `SandboxRepoContext` |
| `peak_guard_removal_test.go` `TestCostFreeVerbsRunInsideAPeakWindow` | real `task run` / `worker --once` | `SandboxRepoContext` + the CONTROL binary keeps an explicit package cwd |
| `cli_tasks_worker_unknown_arg_test.go` `TestWorkerAcceptedArgumentsParse` | `--once` STARTS the loop | fixture-repo pin |
| `cli_tasks_worker_unknown_arg_test.go` `TestWorkerUnknownArgumentIsRefused` | `task submit` seeds a row through the real binary | fixture-repo pin |

### And a third pin shape, because the first two are not enough

`SandboxRepoContext`'s cwd is a directory **inside no repository**. That is the
strictest sandbox, and it broke two genuinely different cases (measured):

1. `gopi task submit` **refuses** from a non-repository cwd — `task_submit: repo
   "…" is not a git work tree` — so any suite that seeds its rows through the
   shipped CLI fails outright under it;
2. the `task run` wrapper resolves the task's deliverable repo from its cwd, so a
   child handed no repository cannot exercise the path the test is about — the
   assertion would then pass **vacuously**.

`SandboxRepoContextInFixtureRepo` pins the other two inputs exactly as before
(empty settings repos table, fresh ledger) and points the cwd at a **throwaway
fixture repository this test owns**: one commit, and *re-measured* to hold exactly
one worktree registration (the main checkout) — asserted in
`repoCtxWorktreeRegistrations`, not assumed, because a fixture that silently grew
a second registration would hand the spawned pass a freeable one. The settings
file and the ledger are written **outside** the fixture repo, which stays
pristine — the child's cwd does not have to be repo-less, it only has to be a repo
whose entire worktree set is this test's own main checkout.

Why that is sufficient, stated as the rule rather than the example: the release
pass resolves its repo set from (a) the ledger rows this process claims and (b)
the repository of the process cwd. The fixture pin sets (a) to a fresh, absent
ledger, so the union is exactly the fixture repository — whose only registration
is the main checkout, which the pass never removes. The two inputs that could
widen that set (settings repos table, ledger) are pinned; the only residue is a
repo this test owns and empties at `t.TempDir()` teardown.

Both `SandboxRepoContext*` forms now share `repoCtxPinTo`, so the three pins can
never drift between them.

## Ordering trap worth knowing (cost me one red run)

`gopiTestBinPath` / `buildGopiForWorkerHelp` / `peakGuardProofBinary` all build
with a **relative** `go build -o … .`. Resolving the binary *after* the repo-context
pin chdirs out of the package directory, so the build fails:

```
peak_guard_removal_test.go:275: build gopi from this tree: go build: exit status 1
    go: go.mod file not found in current directory or any parent directory
```

and in the resolver that SKIPS instead of failing, that is a **silently green
no-op** — the worst outcome available. The pin therefore goes **after** the binary
is resolved in every pinned test.

## What the gate does NOT cover (honest scope)

- The scan is `cmd/agent/*_test.go` only: other packages can spawn gopi too, and a
  spawn site in `internal/**` is outside this gate's glob.
- `runGopiInDir`'s new `dir` parameter is read by neither `rcsIsRunnerWithDirArg`
  (which only knows `testutil.RunGopi`) nor `rcsFunctionSetsOwnWorkingDir` (which
  looks for `cmd.Dir = <non-empty>` in the function body). No scanned call passes
  it a non-empty literal today, so nothing is mis-pinned; a future call doing so
  would be reported as unpinned (a false positive, never a false negative).
- `internal/tools/task_reclaim_self.go` is unchanged by this branch.

## Branch sweep — newer than `796b3333`, touching `cmd/agent/*_test.go`

Exhaustive: **14 local heads** descend from `796b3333` and touch
`cmd/agent/*_test.go`. 4 re-checked (below), 10 named individually.

Re-checked (in the measured tree, or read directly):

| branch | tip | status |
| --- | --- | --- |
| `fix/cmdagent-repo-context-sandbox` | `5271631b` | merged into HEAD → measured |
| master (`fix/merge-rebuild-loop`) | `0dac2ae7` | master → measured; its `569a5b0e` adds two ledger-probe tests, **0** spawn-shape lines |
| `feat/child-stream-task-output` | `0dac2ae7` | the same commit as master → measured |
| `fix/repo-context-gate-reverify` | this branch | measured (the run above) |

**NOT merged → not re-checkable by a gate run** (each measured per branch:
spawn-shape lines = `\+.*(exec\.Command|runGopiIn|gopiTestBin|testutil\.[A-Za-z]*Run)`
in `git diff 796b3333..<b>` and `git diff master...<b>`):

| branch | tip | tip date | Δ vs master | spawn lines | finding |
| --- | --- | --- | --- | --- | --- |
| `fix/merger-busy-liveness` | `d7f24665` | 2026-09-16 | 228 | **3** | `merger_busy_recycle_red_test.go` has **2 unpinned** `runGopiIn(…, CliVerbMerger, …)` sites (`mergerControlStatus`, `mergerControlStop`); the third line is `exec.Command("flock", …)` — a lock holder, not a gopi spawn (out of scope). **Does not compile against master**: `tools.MergerNotInFlightWord`, `MergerState.ProcStart`, `MergerStateFile.WriterLive` are absent → see Not Done |
| `fix/heavy-tests-classification` | `5a159274` | 2026-09-16 | 48 | 0 | only `heavytest.Require(...)` lines |
| `docs/membase-sweep-disposition-decision` | `9ee389e6` | 2026-09-16 | 0 | 0 | its two files are identical on master — 0-line diff |
| `fix/loop-proof-script-scratch` | `f478baed` | 2026-09-16 | 0 | 0 | rig scratch routing; blob-identical files |
| `fix/rule17-journalreport-env-escape` | `f9311942` | 2026-09-16 | 0 | 0 | env escape; 0-line diff |
| `fix/rule17-journalreport-escape` | `c7123178` | 2026-09-16 | 0 | 0 | docs for the same; 0-line diff |
| `fix/stale-merge-audit-20260916` | `367928bb` | 2026-09-16 | 0 | 0 | audit doc |
| `fix/standards-baseline-rekey` | `c48f5436` | 2026-09-16 | 0 | 0 | baseline recipe |
| `fix/testutil-gates-green` | `fc1013bd` | 2026-09-16 | 0 | 0 | testutil scratch pin |
| `fix/worker-ownership-pointer-backfill-3` | `58499911` | 2026-09-16 | 0 | 0 | report only |

Not "newer than" the fix (they do NOT contain `796b3333`): **2 local heads** with
their own `cmd/agent/*_test.go` commits — `fix/ledger-firstopen-budget` (`6b846c64`)
and `fix/signalrec-verify-attribution` (`e9994f4e`) — recorded, not re-checked
(their copies of the scanned files are pre-fix). Remote-tracking refs (`hp/*`,
`peerbox/*`, `gitPushcheck/*`, `local/*`, `tmp/*`; 34 refs total head+remote without
`796b3333`) and `archive/*` tags are outside this ledger.

**Net finding of the sweep:** exactly ONE unmerged branch carries a spawn shape the
gate must judge (`fix/merger-busy-liveness`); the other nine are provably inert for
this hazard.

## Merge-time re-arm probe (measured, not reasoned)

A spawn site that is pinned on this branch can still be handed back UNPINNED if a
sibling branch's copy of the same file is taken at merge time. Measured with a
throwaway worktree (`~/workspace/gopi-merge-probe`):

| merge | result | pins after |
| --- | --- | --- |
| `fix/ledger-firstopen-budget` (base `e1229f85`) | clean auto-merge, no `cmd/agent` overlap | 60 (unchanged) |
| `fix/heavy-tests-classification` (base `0dac2ae7`) | **2 content CONFLICTS**, both ON PIN LINES | union resolution 60 |
| `fix/merger-busy-liveness` (base `0dac2ae7`) | clean auto-merge, no overlap with pinned files | 60 |

The conflicts are the important half. Both are the SAME 3-line shape — the
sibling inserts its `heavytest.Require(...)` call at exactly the offset where this
branch inserts `SandboxRepoContext(t)`, and git wraps its marker lines around it.
**The block below is quoted verbatim from that merge** — the marker lines are
git's own unmerged output, the evidence this section exists to carry, NOT residue
in this file. They are fenced for exactly that reason, and the repo-wide
conflict-marker gate (`internal/standards`, Rules 16-21 family) reads them as a
quote: deleting them to satisfy a scan would delete the evidence, not a defect.

```
<<<<<<< HEAD
	SandboxRepoContext(t) // repo-context sandbox: the worker child must not inherit this process's cwd (the live repo).
=======
	heavytest.Require(t, heavytest.CriterionDeviceExec)
>>>>>>> fix/heavy-tests-classification
```

in `agent_spawn_guard_test.go` (`TestAgentSpawnGuardAllowsOperatorTerminalInsideAgentSession`)
and `agent_spawn_guard_env_isolation_test.go`
(`TestAgentSpawnGuardOperatorStyleLaunchSurvivesInheritedDaemonHandshake`).

**The gate catches the wrong resolution** — that is the load-bearing fact, and it
was measured both ways on the real merge:

- resolution keeping BOTH sides (the correct union): gate PASSES, 60 pins,
  `ok github.com/nick/gopi/cmd/agent 0.875s`;
- resolution taking the sibling's side only (pin dropped — the mistake a conflict
  resolver makes by hand): gate **FAILS** and names exactly the two functions —

```
2 function(s) spawn a real gopi worker/merger WITHOUT the repo-context sandbox:
  agent_spawn_guard_env_isolation_test.go:126: TestAgentSpawnGuardOperatorStyleLaunchSurvivesInheritedDaemonHandshake()
  agent_spawn_guard_test.go:266: TestAgentSpawnGuardAllowsOperatorTerminalInsideAgentSession()
```

So this gate is the instrument that decides that conflict, and no merge of a
sibling copy can re-arm those two sites silently — the resolver is told. This is
why the sweep above is stated per branch and per file rather than as a count.
(`fix/heavy-tests-classification` itself cannot arm the hazard: it adds 0
spawn-shape lines. What it can do is collide with the pin lines, and that is a
merge-resolution event the gate now adjudicates.)

## Full-suite result (the epic asked for the targeted trio; this is the honest rest)

`go test -count=1 ./cmd/agent/` on this branch: **FAIL, 436.5s**, two failures,
NEITHER caused by the repo-context pin:

- `TestTmpDirValueProbeLeavesNothingBehind` — 3 abandoned probe directories under
  the temp root, all **11h44m–11h49m old**. A directory older than this session
  cannot have been created by it; pre-existing residue (Rule 22), not a regression.
- `TestWorkerDaemon_DetachesAndKeepsRunning` — `worker daemon pid N has SID=X, want
  its own pid (setsid did not take)`. **Reproduced on PURE MASTER** (`0dac2ae7`,
  fresh clean worktree, no pin, fresh build): identical failure. A pre-existing
  master failure about this host's session/scope reality (Rule 24 — `setsid` does
  not obtain the cgroup escape), not a regression from this branch.

## The hazard fired against THIS worktree (attributed, and its own instruction to check)

At t+1240s the worktree `~/workspace/gopi-rectx-verify` was **deleted while in
use** — mid-session, with a background suite running in it. `git worktree list`
lost the entry; the branch and every commit survived intact
(`git log fix/repo-context-gate-reverify` → `c48b94b7`), and the worktree was
re-created from the branch to finish this report.

Attribution, done with what is actually on this host: the briefing's
`scripts/attribute-victim.sh` and `scripts/killer-watch.sh` **do not exist in this
repo** (`ls scripts/` → neither; nothing under `scripts/` or `devdoc/` references
them), so no automatic attribution was possible. The directly observed context:
the background run I started was a **full `go test -count=1 ./cmd/agent/`** — and
that suite's own log shows the mechanism live, on my own sibling worktree:

```
worker: own-worktree-release: own registered worktree kept: /home/user/workspace/gopi-merge-probe — younger than the 5m0s floor (1m6s)
```

i.e. a spawned real gopi's own-worktree-release pass enumerating
`~/workspace/*` worktrees. `gopi-rectx-verify` was listed in that same
enumeration as "held by a live process" (my own shell) in that pass; the deletion
happened after it, when the holder condition no longer read as live. The honest
conclusion: I ran the full suite myself, and my own run is the demonstrated
mechanism by which a clean, unowned worktree under `~/workspace` disappears. That
is exactly the hazard this epic pins against — and it is why the epic's targeted
trio, not a full-suite sweep, is the acceptance instrument. No victim of mine was
lost (branch intact, worktree recreated); `gopi-merge-probe` (younger than the
floor) survived and will be removed by me, not left for the pass.

## Verification

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     # OK, 22791168 bytes
$ go test -count=1 -run TestSpawnersPinTheRepoContext ./cmd/agent/
    repo-context gate: 53 lifecycle spawn site(s) scanned, 50 pinned, 3 escaped
    ok  github.com/nick/gopi/cmd/agent
$ go test -count=1 -run 'TestSpawnersPinTheRepoContext|TestRepoContextHazard|TestWorkerWaitsAndPicksUpNewTaskWithoutRestart' ./cmd/agent/
    ok  github.com/nick/gopi/cmd/agent  4.392s
```

Mutation proof that the hardened recognition is load-bearing: with the 5 pins
reverted the gate FAILS and names exactly those 5 functions; restored, it PASSES
(53/50/3).

## Deliverable

- Branch: `fix/repo-context-gate-reverify` — worktree `~/workspace/gopi-rectx-verify`
  (base: master `0dac2ae7` + merged `fix/cmdagent-repo-context-sandbox` = `6bf2a37a`).
- Commits: `158db3fe` (gate widening + fixture pin), `e0d42b0a` (the five pins),
  `75bf9d0f` + `6ba0346a` (docs/report).
- Recovery: `git -C /home/user/work/GoPi show fix/repo-context-gate-reverify:devdoc/repo-context-gate-reverify.md`
  (and `…:cmd/agent/repo_context_sandbox_guard_test.go`).
- The worktree shares the main repo's refs, so the commit is already on the branch
  in `/home/user/work/GoPi` — no push needed.
