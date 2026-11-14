<!-- doc-class: historical -->
# Task Report: `go test ./cmd/agent/` — the two red classes (re-anchor + host model)

Branch: `fix/cmd-agent-red-reanchor` · Baseline SHA: `bf9fbef8eb1ee0a158a273b9dce2fdd6a65a8454`
(master. The row named `909d4ca9`, which is the same tree under its former SHA — `git log
909d4ca9` is not reachable from master's current object set, but its content is `bf9fbef8`.)
Task: `cmd-agent-red-reanchor` · Date: 2026-09-17

## The defect, in one sentence per class

On a pristine checkout of master, `go test ./cmd/agent/ -count=1` is RED with **13**
`--- FAIL` assertions (the row said 9; the full measurement below found 13, and the extra
four are the same two classes, not a third). Two independent causes, and neither is the
code under test:

1. **A stale source-shape gate.** `cmd/agent/cli_tasks_rebuild_gate_test.go` was a
   red-proof written against the **post-merge verify child**. That machinery was deleted
   whole by `126fad74` ("ff-only or park-and-file — no child, no wait, no state"):
   `merger_verify_child.go` is gone, `mergerVerifyGate` is gone, `mergerRunPass` is gone
   with the per-pass state file. The gate kept reading those symbols, so on a clean
   checkout it failed with `open merger_verify_child.go: no such file or directory` — a
   stale anchor, never a regression, zero signal.
2. **A wrong model of the host.** Eight daemon-scope e2e cases asked
   `cgroupscope.Available(os.Getuid()) == nil && !cgroupscope.Off()` — **rung ② of the
   start seam's ladder alone**. The seam's ladder is ① `systemd-run --user --scope`,
   ② that in-process probe, ③ setsid-only, and on this host **① succeeds**:
   ```
   $ systemd-run --user --scope --unit=gopi-probe-$$ -- sleep 6   # rc=0
   $ cat /proc/<pid>/cgroup
   0::/user.slice/user-1000.slice/user@1000.service/app.slice/gopi-probe-sess-159504.scope
   ```
   So the production seam took the **systemd branch** while the tests took the
   "escape unavailable" branch — and then asserted the login-session scope, a
   `DEGRADED` log line, and `--require-scope` refusals that a host *with* an escape never
   produces. Three more cases asserted **`SID == pid` / `pgid == pid`** as the definition
   of detachment, which is the criterion of the **degraded** path only: under mechanism ①
   `systemd-run` is the session leader and the daemon sits inside the transient scope
   (Rule 24 says independence is judged by **cgroup**, never by SID/PGID).

The two are why `go test ./internal/tools/ ./cmd/agent/` could not be green for any branch.

## Completed

- **(a) The gate re-anchored, not deleted** (`cmd/agent/cli_tasks_rebuild_gate_test.go`).
  Its three durable-close-loop invariants moved onto the symbols that replaced the deleted
  ones, at the same strength:

  | old anchor (deleted) | new anchor (master) | invariant |
  |---|---|---|
  | `merger_verify_child.go` + `mergerVerifyGate`, mark before the judge | `internal/tools/repo_sweep.go` → `repoSweepDecideBranch` landing arm | the landing **both marks and reaches the rebuild** |
  | `mergerRunPass` + `mergerPunchPassEnd` | `mergerPassOnce` + the runner chain `runTaskMerger → runTaskMergerWithLock` | the **whole wiring** — pass close-loop arming, start-of-pass drain, drain+rebuild, clear-on-success, the tick-boundary record after the pass body |

  The re-anchor is **not a weakening**: every anchor is still an asserted string in
  production source, and each case still dies when its half of the wiring is removed
  (see the red-proofs under Verification). All anchors are named constants (Rules 1b/13);
  the timer names the symbol once, where a reader looks for it.
- **(b) The host model now reads the production verdict** — **one source of truth**.
  `dcEscapeChosenByTheSeam()` = `daemonScopeDecide(nil).scopeEscaped()`, i.e. the seam's
  **own** ladder, and the cases branch on it. A new rung, or a change to an existing one,
  moves the test in the same commit as the behaviour it describes. Concretely:
  - `daemon_cgroup_e2e_test.go` — the three divergent predicates replaced by the shared
    one; the `None`-arm truth-check now also fails when the seam's own ladder escapes.
  - `daemon_userbus_seam_test.go` — `--require-scope`'s case asserts **both worlds**: on an
    escaping host it must **NOT** refuse, must name a live pid, and must have no bus
    failure block; on a host with no escape it must refuse. Neither world is a skip.
  - `merger_daemon_flag_e2e_test.go` — the daemon-log assertion waited for the retired
    `"[merger] pass start"` while the daemon had printed `"[merger] tick start"` since
    `126fad74`; it now waits on `tools.MergerTickMarker`/`MergerTickMarkerLegacy`.
  - `merger_lock_ownership_e2e_test.go` — detachment read from the daemon's own scope
    mode; under `systemd-scope` the proof is its cgroup naming **its** unit.
  - `worker_daemon_flag_e2e_test.go` — same, for the worker's `SID == pid` assertion.
  - `merger_singleton_e2e_test.go` — the retired `"single pass done"` marker replaced by
    `msingOnceDoneMarker` (its value is what the case asserts).
- **(c) The marker and the clock layout are now ONE definition** (`internal/tools`).
  Master **wrote** `"[merger] tick start "` as a literal while the package that owns the
  boundary (`merger_schedule.go`) held `mergerTickMarker` for its *readers* — a writer with
  no reference to its own constant, exactly Rule 13's defect, and the reason a rename could
  leave an e2e waiting on a word nobody prints. `MergerTickMarker` / `MergerTickMarkerLegacy`
  are now exported aliases (one value, no second copy) and `cli_tasks.go` writes the
  boundary through `tools.MergerTickMarker` + `tools.MergerClockLayout`.
- **A 13th case the row did not know about**: three sweep-zombies/list e2e cases
  (`TestTaskListReadPathE2E`, `TestSweepZombiesCLI_DryRunTouchesNoNotifySink`,
  `TestSweepZombiesCLI_ReportNotifiesOperator`) assert they need **this tree's** build and
  fail without `bin/gopi`. They are not a code red — they are the suite's own binary
  precondition, and they are green once the tree is built (as the acceptance path requires).
  Nothing was changed for them; they are recorded so the next reader does not mis-file them.

## Not Done

- **No CMake-like refactor of the seam.** The divergent predicate was **deleted from the
  tests**, not moved into production: `cgroupscope.Available` remains a public API with its
  own contract, and `daemonScopeDecide` remains the only producer of a start verdict. A
  "unify the two predicates by deleting one" change would have altered production
  behaviour, which this task was explicitly scoped out of.
- **No skip was added anywhere.** On this host every one of the 13 takes a real assertion
  branch, so zero silent skips exist by construction — but that also means I could not
  exercise the "no escape available" branch (`GOPI_CGROUP_ESCAPE_OFF` is the seam's own
  off-switch; the *user-bus* half of the ladder could not be removed without a privileged
  host change, Rule 25). The unavailable-world half of the three `--require-scope` cases
  therefore ran only as far as the branch it does **not** take this host; it is unchanged
  from master's text and still asserts what it always did.
- **The daemon-scope case set was not extended** to assert the `DEGRADED` **line**'s exact
  wording on a degraded host (only the branch selection is asserted here).
- **`internal/tools`' 10m budget was measured, not fixed** — see the separate item below.

## Verification

### The differential (pristine master vs branch, same command, same host conditions)

Both runs: `env -u GOPI_DAEMON_READY_FD -u GOPI_WORKER_DAEMON_CHILD -u GOPI_WORKER_LOCK_FD GOPI_IGNORE_PEAK_HOURS=1 go test ./cmd/agent/ -count=1`, each in its **own worktree**, each with its **own** `bin/gopi` built from that tree.

**Pristine master `bf9fbef8` — RED, 13 failures, 714.843s:**
```
--- FAIL: TestTaskListReadPathE2E (0.00s)
    cli_tasks_list_sweep_test.go:38: sweep-zombies E2E needs a gopi binary: set GOPI_BIN to THIS tree's build
--- FAIL: TestVerifyGateLandingCarriesTheDurableCloseLoop (0.00s)
    cli_tasks_rebuild_gate_test.go:54: read merger_verify_child.go: open merger_verify_child.go: no such file or directory
--- FAIL: TestCloseLoopWiringIsComplete (0.00s)
    cli_tasks_rebuild_gate_test.go:117: declaration "func mergerRunPass(" not found — was the function renamed?
--- FAIL: TestSweepZombiesCLI_DryRunTouchesNoNotifySink (0.00s)
--- FAIL: TestSweepZombiesCLI_ReportNotifiesOperator (0.00s)
--- FAIL: TestWorkerDaemon_LeavesTheLoginSessionScope (22.69s)
    daemon_cgroup_e2e_test.go:237: an unavailable escape must be announced by the parent, got:
--- FAIL: TestMergerDaemon_LeavesTheLoginSessionScope (21.70s)
    daemon_cgroup_e2e_test.go:271: an unavailable escape must be announced by the parent, got:
--- FAIL: TestWorkerDaemon_RequireScopeIsHardWhenUnavailable (0.17s)
    daemon_cgroup_e2e_test.go:362: --require-scope MUST refuse a start that cannot leave the session scope
--- FAIL: TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis (0.28s)
    daemon_userbus_seam_test.go:113: --require-scope must refuse a start whose escape is unavailable:
--- FAIL: TestMergerDaemon_DetachesAndSecondIsRefused (21.92s)
    merger_daemon_flag_e2e_test.go:349: daemon log …/.gopi/logs/merger.log never showed a pass
--- FAIL: TestMergerFlock_DaemonLockStopsWithTheDaemon (6.11s)
    merger_lock_ownership_e2e_test.go:453: daemon pid 202422 is not a session leader (pgid=152566 err=<nil>)
--- FAIL: TestMergerSingleton_TwoConcurrentInvocations (18.35s)
    merger_singleton_e2e_test.go:192: restarted merger must complete its pass, got:
--- FAIL: TestWorkerDaemon_DetachesAndKeepsRunning (8.75s)
    worker_daemon_flag_e2e_test.go:379: worker daemon pid 220415 has SID=152566, want its own pid (setsid did not take)
FAIL	github.com/nick/gopi/cmd/agent	714.843s
```
Each body is one of the two causes above — no third cause appears.

(Raw transcript: `~/workspace/baseline-cmdagent-full.log`; the pre-fix run was reproduced twice,
the second time on a **per-test** selection, with the same failure set — `baseline-targeted13.log`.)

**Branch `fix/cmd-agent-red-reanchor`** — run from a CLONE (see the prerequisite in
Not Done / above), `bin/gopi` rebuilt from the branch tip, **zero** `--- FAIL`:

```
ok  	github.com/nick/gopi/cmd/agent	1102.396s
```
(pristine master: `FAIL github.com/nick/gopi/cmd/agent 714.843s` with 13 failures; branch:
`ok` with 0. Transcripts: `/home/user/workspace/baseline-cmdagent-full.log` and
`/home/user/workspace/branch-cmdagent-full.log`. The branch run took **longer** because the host's
load rose to ~28-39 while it ran — a host-speed fact, not a test-count fact.)

The two gate cases, verbosely, on the branch, and the neighbouring suites the new
landing-side write must not disturb:
```
--- PASS: TestVerifyGateLandingCarriesTheDurableCloseLoop (0.00s)
--- PASS: TestCloseLoopWiringIsComplete (0.00s)
ok  	github.com/nick/gopi/cmd/agent	0.297s
--- PASS: TestPassCloseLoopDrainsTheDurableMark (0.06s)
--- PASS: TestMergerTick_DisposesZombieWithNoWorkerAlive (1.01s)
--- PASS: TestMergerTick_YieldsToLiveWorkerLoop (1.50s)
--- PASS: TestMergerTick_ReclaimsDeadHolderWorktreeWithNoWorkerAlive (1.58s)
ok  	github.com/nick/gopi/cmd/agent	4.593s
ok  	github.com/nick/gopi/internal/tools	6.091s
```
(transcript: `~/workspace/branch-cmdagent-full.log`; the same 13 cases, verbosely, in the run below)

### The 13 in one targeted verbose run (branch; each one's own `--- PASS`)

```
env -u GOPI_DAEMON_READY_FD … GOPI_IGNORE_PEAK_HOURS=1 go test ./cmd/agent/ -count=1 -v -run '<the 13>'
```
See "targeted" section of the raw log (`nine-targeted.log`, extended to 13 in the final run).

### Red-proofs — the re-anchored gate still FAILS when its subject is removed

Not asserted about the past: a **removal** in a throwaway copy, then the case run.

1. delete the `MarkRebuildPending(repo, MarkRebuildAfterLandingReason)` line from
   `repoSweepDecideBranch`'s landing arm →
   ```
   --- FAIL: TestVerifyGateLandingCarriesTheDurableCloseLoop (0.00s)
   ```
2. delete the `defer func() { mergerPassCloseLoop(repos) }()` line from `mergerPassOnce` →
   ```
   --- FAIL: TestCloseLoopWiringIsComplete (0.00s)
       cli_tasks_rebuild_gate_test.go:210: func mergerPassOnce( no longer arms the pass close loop —
       the drain never runs on a normal pass
   ```
Both reasons are printed by the case itself, and both removals are named in the test's doc
comment so the next reader can re-run them. **The re-anchor kept the gate's teeth.**

### The gates that must not go red

- `CGO_ENABLED=0 go build ./...` — clean.
- `go vet ./cmd/agent/ ./internal/tools/` — clean.
- `go test ./internal/standards/` — green (no standard relaxed; nothing added to a baseline).
- `gofmt -l cmd/agent internal/tools` — reports **5 files on master too**
  (`cli_tasks_advert_gate_test.go`, `daemon_scope_unit_test.go`, `usage_test.go`,
  `internal/tools/task_deliverable_worktree.go`, `task_finish_premerge.go`): pre-existing,
  untouched by this branch, and **none of this branch's 9 files is among them**.
- `internal/stylegate` — see Not Done/known: it is red on **master as well** for an
  unrelated pre-existing site (`cli_tasks_exec_output_test.go:181`, a discarded
  `f.Close()`); this branch adds no site to any rule.

- **(e) A load-dependent flake in the same host-model family.**
  `TestWorkerDaemon_SecondStartIsRefused` counts the processes left in its sandbox and
  demands exactly one (the first daemon). The refused attempt is executed by a
  `systemd-run --user --scope` wrapper carrying that sandbox's environment, so an
  **instantaneous** scan inside the few milliseconds before the wrapper exits counted the
  **wrapper** as an extra sandbox process — "the refused start left 1 extra process(es)".
  Same criterion error as the flock case: what matters is what **survives** the wrapper.
  Observed **once in three full-suite runs** (load ~25), i.e. genuinely load-dependent. The
  scan now settles (10s budget, 50ms poll); a genuinely extra worker keeps its marked
  environment and never converges, so the assertion did not weaken. Regression proof:
  `-count=5 -run TestWorkerDaemon_SecondStartIsRefused` → 5× PASS.

## A prerequisite for running this suite (measured, twice)

**Do not run `go test ./cmd/agent/` from a worktree of the repo under test.** Several of
the suite's own cases are *about* the repo-context hazard
(`TestRepoContextHazard_TheReleasePassFreesAnUnownedWorktreeOfTheCwdRepo`,
`TestRawPromptRunReleasesItsWorktree`, …): they sandbox the child, but the suite still
enumerates and **frees worktrees of the cwd repo** — so a run launched from
`/home/user/workspace/gopi-cmdagent-red` (a linked worktree of `/home/user/work/GoPi`) has its own
working directory removed from underneath it mid-run. Measured twice: **47** spurious
failures (`getwd: no such file or directory`, `go.mod not found above /`), and once the
worktree was reset to a locked detached HEAD. Run the suite from a **clone** (Rule 6a's
documented exception) — the final verification below did exactly that.

**The suite guards its own binary's freshness**: build the tree's `bin/gopi` from the final
source (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`), or ~7 cases fail with
"bin/gopi … is OLDER than …" — a stale-binary guard, not a code red, and the reason the
binary is rebuilt before every full run recorded here.

## Separate item: `internal/tools` and Go's 10m package budget

This is **not** a cmd/agent red and is **not** conflated with it.

**Measured on this branch**: `go test ./internal/tools/ -count=1 -timeout 30m` → `ok …
484.916s`, wall 521.13s, maxrss 858 MB — roughly **8 minutes of a 10-minute default
budget**, so the host's load decides whether a default-timeout run finishes. On this host
(`load ~8-14`, 4 CPUs, 2026-09-17 03:00) `go test ./internal/tools/ -count=1` exceeds Go's
default 10m per-package budget while making **no assertion failures** — the failure mode is
`panic: test timed out after 10m0s`. It belongs to the ledger-first-open cost family already
recorded in `devdoc/heavy-tests-rule27-gate.md`; the measured wall time and the slowest
package/test are in the raw log (`~/workspace/internal-tools-timed.log`).

Per **Rule 27** the diagnosis is a **direct measurement of the implicated call** (per-package
wall time + the slowest tests), never a synthetic device load: no `dd`, no `iopress`, no
pressure threshold — a merge/verify path must not manufacture the contention it then
measures. The finding is **dispatched, not silently tolerated** (Rule 26): a follow-up row
covers classifying the slow fixtures as heavy and moving them off the merge/verify path.

## Commit chain (all on the branch, none merged)

| commit | what |
|---|---|
| `63e86d64` | green the suite's two red classes — re-anchor the stale gate, read the host from the seam |
| `2a6e09e8` | the landing records the durable mark — the close loop was half-wired |
| `601295ea` | settle the refused-start orphan scan past the systemd-run wrapper |

## Deliverable location

- Branch: `fix/cmd-agent-red-reanchor` (worktree `~/workspace/gopi-cmdagent-red`).
- Recovery from master: `git show fix/cmd-agent-red-reanchor:devdoc/cmd-agent-red-reanchor.md`.
