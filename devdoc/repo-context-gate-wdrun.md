<!-- doc-class: historical -->
# Task Report: repo-context gate — the `wdRun` hole + the false positives it exposed

Branch: `fix/repo-context-gate-wdrun` (off master `32932668`; re-merged onto master
`f7bd2616` (`065e484d`), then onto the current master tip `5cdae25e` (`cbdef255`), then a
docs commit — **branch tip `16ffc9e7`**).
Worktree: created as `~/workspace/gopi-rcs-wdrun`; **that directory was FREED by a peer
run while this task was still open** (a merger take-over / its own-worktree-release pass —
the very class of hazard this branch's guard closes), and the re-created
`~/workspace/gopi-rcs-wdrun2` was freed the same way a second time, so the final close-out
run is from a third worktree, `~/workspace/gopi-rcs-final`, on the SAME branch (plain
`git worktree add <path> <branch>`, never `-b`). No work was lost either time: the branch
ref was never touched and every deliverable commit is on it.
Devdoc: `devdoc/repo-context-gate-wdrun.md` (same branch).

## Files
- `cmd/agent/repo_context_sandbox_guard_test.go` (gate: recognition set + predicate + floor + marker count)
- `cmd/agent/merger_restart_e2e_test.go` (`mreStartLoop` + plain-start/restart pins)
- `cmd/agent/merger_retired_flags_test.go`, `cmd/agent/merger_work_lock_e2e_test.go`,
  `cmd/agent/task_lifecycle_tick_test.go`, `cmd/agent/daemon_cgroup_e2e_test.go` (pins)
- `cmd/agent/daemon_test_leak_e2e_test.go`, `cmd/agent/worker_status_residue_e2e_test.go`
  (escapes re-worded to predicate notes)
- `devdoc/repo-context-gate-wdrun.md` (devdoc)

## Completed

- **Reproduced the hole by mutation first** (the mutation is the proof): adding
  `"wdRun": true` to `rcsLocalRunnerNames` made the gate fail with
  **21 function(s) … WITHOUT the repo-context sandbox** — the briefing's ~19, plus
  two more found by the audit. Full list under Evidence.
- **A file-diff/exhaustive audit of `cmd/agent`** (every `exec.Command`/
  `exec.CommandContext` whose first argument is not a string literal — 79 hits)
  found **two further real-binary runners the recognition set also missed**:
  `mdRun` (`merger_test_rig_test.go`, drives a bare `gopi merger` twice and a real
  `task merge --all`) and `mloopStart` (`merger_loop_e2e_test.go`, starts a
  resident `gopi merger --every` loop). Both are now in the recognition set.
- **The false positives are excluded BY THE PREDICATE, not by 21 pins** — the
  briefing's explicit requirement:
  - `rcsReadOnlyMergerControls` + `rcsRetiredMergerFlags` + `rcsMergerArgvIsControl`:
    `--stop` / `--restart` / `--status` and the RETIRED spellings return before any
    tick, so none reaches `config.ResolveMergerRepos`. A BARE `gopi merger` is
    deliberately still a lifecycle (it runs one tick — the hazard).
  - The **daemon arm**: a `--daemon` child gets its own working directory from
    production (`daemoncwd.Resolve`). The gate READS the production sources for that
    call and refuses to judge the daemon class if the resolver is gone
    (`rcsDaemonChildrenResolveTheirOwnDir`) — a premise that vanished fails loudly
    instead of widening the exclusion.
- **Genuine unpinned sites pinned** with `SandboxRepoContext(t)`, each placed AFTER
  the binary is resolved (the ordering trap): `mreStartLoop` (the one place a
  resident merger loop is born) + the plain-start/restart sites, `merger_retired_flags_test.go`
  ×3, `merger_work_lock_e2e_test.go` (`task merge`), `task_lifecycle_tick_test.go`
  (`gopi merger` cron), `daemon_cgroup_e2e_test.go` (a refusal the gate cannot see).
- **Two hardening fixes that are not about `wdRun`:**
  - the **escape-marker count must equal the escaped count** — admitting a runner
    silently un-covers the `repo-context-ok:` sites it described. It fired
    immediately on the real tree (2 orphaned escapes, re-worded as predicate notes).
  - the **read-floor 15 → 80** against a measured 85, so a future predicate
    regression cannot shrink the scan toward vacuity without failing.
  - variable-assigned argv resolution (`rcsFunctionArgvSlices`, `rcsSpreadArgElems`)
    so moving a table's argv into a variable cannot hide a lifecycle.
- **Mutation red-proofs** (both directions): removing `mreStartLoop`'s pin makes the
  gate name exactly `merger_restart_e2e_test.go: mreStartLoop()`; dropping `wdRun`
  from the recognition set makes the gate fail again.

## Not Done (honest)

- Control-face sites (`merger_status_cli_test.go`, the `--stop`/`--restart` face in
  `merger_restart_e2e_test.go`) got **no pin** — they are excluded by the predicate
  per the briefing (pinning them would be blind pinning).
- The gate still cannot see an entry point that reaches `exec.Command` **indirectly**;
  `mloopStart` is in the set only because this round's audit named it. The read-floor
  is the only tripwire for a future indirect runner.
- Whole-suite coverage: branch-scoped to the packages the diff touched (Rule 27 /
  smokegate), not tree-wide.
- The devdoc on the branch (`devdoc/repo-context-gate-wdrun.md`) is the same text as
  this report; this file is the run's report deliverable.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (exit 0).
- Merge: first `git merge master` (master's then-tip) → conflict in
  `cmd/agent/merger_restart_e2e_test.go` only (master's own `mreStartLoop` pin comment),
  resolved in favour of master's wording; merge commit `1f5e8594`. Re-merged onto the
  CURRENT master tip `f7bd2616` before close-out → clean, zero conflicts, merge commit
  `065e484d` (branch tip).
- Branch-scoped suite (the packages the diff touched):
  `go test -count=1 -run 'TestMerger|TestKilledWorkerDaemon|TestWorkerDaemon|TestWorkerStatusResidue|TestDaemonLeak|TestUserBusSeam|TestSpawnersPinTheRepoContext|TestRepoContextHazard' ./cmd/agent/`
  (result recorded below; the one failure seen in the FIRST run was
  `TestMergerTakeoverPromptIsEmbedded`, caused by that run's own tree changing under
  it — a merge landed while the test binary was already built; it passes in isolation
  on both master and this branch).
- Gate: `go test -count=1 -v -run 'TestSpawnersPinTheRepoContext|TestRepoContextHazard' ./cmd/agent/`
  → `ok github.com/nick/gopi/cmd/agent 8.909s`, both PASS. Re-run after the pre-close
  re-merge onto `f7bd2616`: `ok ... 2.460s`, both PASS (0.18s / 1.28s), same read-floor line.
- Read-floor line: `85 lifecycle spawn site(s) scanned, 69 pinned, 1 escaped, 15 daemon-start (own-dir) site(s), 1 escape marker(s)`.
- **Pre-merge re-verification onto the current master tip `5cdae25e`** (adoption: merge docs/req-test-infrastructure): `git merge master` → clean, zero conflicts, branch tip `cbdef255`. Then `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0; `go test -count=1 -v -run 'TestSpawnersPinTheRepoContext|TestRepoContextHazard' ./cmd/agent/` → both PASS (0.14s / 5.05s), `ok github.com/nick/gopi/cmd/agent 5.633s`, same read-floor line; `git status --porcelain` printed nothing (Rule 30). Run in worktree `/home/user/workspace/gopi-rcs-wdrun2` (the original `/home/user/workspace/gopi-rcs-wdrun` was released by a peer merger take-over while this task was open — see the incident note above).


1. **RED (the hole, reproduced by mutating the recognition set)** —
   `go test -count=1 -run TestSpawnersPinTheRepoContext ./cmd/agent/`:
   ```
   repo_context_sandbox_guard_test.go:187: 21 function(s) spawn a real gopi worker/merger WITHOUT the repo-context sandbox:
         daemon_cgroup_e2e_test.go:341: TestWorkerDaemon_RequireScopeIsHardWhenUnavailable()
         daemon_cgroup_e2e_test.go:377: TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation()
         daemon_cgroup_e2e_test.go:407: TestWorkerDaemon_RequireScopeWithoutDaemonIsRefused()
         daemon_userbus_seam_test.go:111: TestUserBusSeam_RequireScopeRefusalCarriesTheDiagnosis()
         daemon_userbus_seam_test.go:170: TestUserBusSeam_SilentWhenNothingToDiagnose()
         daemon_userbus_seam_test.go:52: TestUserBusSeam_DegradedStartPrintsTheMeasuredState()
         merger_restart_e2e_test.go:236: mreStopViaFace()
         merger_restart_e2e_test.go:281: mreStartLoop()
         merger_restart_e2e_test.go:351: TestMergerControlFlagListsAreOneDecision()
         merger_restart_e2e_test.go:396: TestMergerStopEndsALiveLoop()
         merger_restart_e2e_test.go:419: TestMergerStopWithNoLoopIsTheEndState()
         merger_restart_e2e_test.go:455: TestMergerRestartReplacesTheLoopWithoutOverlap()
         merger_restart_e2e_test.go:536: TestMergerRestartWithNoLoopIsAPlainStart()
         merger_restart_e2e_test.go:580: TestMergerRestartRefusesABadPeriodBeforeStopping()
         merger_retired_flags_test.go:122: TestMergerEveryIsAdmittedAndItsPeriodIsTheOnlyThingRefused()
         merger_retired_flags_test.go:55: TestMergerRetiredFlagsAreRefusedNotIgnored()
         merger_retired_flags_test.go:95: TestMergerBareRunIsTheSupportedSpelling()
         merger_status_cli_test.go:113: TestMergerStatus_FacesAreReadOnly()
         worker_daemon_singleton_e2e_test.go:109: wsStartDaemon()
         worker_foreground_console_e2e_test.go:226: TestDaemonWorkerKeepsTheTaskTextOutOfItsOwnStdio()
         worker_idle_drained_stop_e2e_test.go:77: wisStartDaemon()
   FAIL	github.com/nick/gopi/cmd/agent	5.847s
   ```
2. **GREEN (fixed tree)** —
   `go test -count=1 -v -run 'TestSpawnersPinTheRepoContext|TestRepoContextHazard' ./cmd/agent/`:
   ```
   repo_context_sandbox_guard_test.go:331: repo-context gate: escaped spawn site (documented): worker_daemon_singleton_e2e_test.go:63: wdForeground()
   repo_context_sandbox_guard_test.go:363: repo-context gate: 85 lifecycle spawn site(s) scanned, 69 pinned, 1 escaped, 15 daemon-start (own-dir) site(s), 1 escape marker(s)
   --- PASS: TestSpawnersPinTheRepoContext (0.14s)
   --- PASS: TestRepoContextHazard_TheReleasePassFreesAnUnownedWorktreeOfTheCwdRepo (7.92s)
   ok  	github.com/nick/gopi/cmd/agent	8.909s
   ```
3. **Mutation red-proof A (a pin is load-bearing)** — deleting the `SandboxRepoContext(t)`
   line from `mreStartLoop` made the gate name exactly that function:
   `merger_restart_e2e_test.go:287: mreStartLoop()` → FAIL.
4. **The escape-marker guard firing on the real tree** (before re-wording the two
   orphaned escapes):
   `the tree carries 3 "repo-context-ok:" marker(s) but this scan COUNTED only 1 escaped spawn site(s)` → FAIL.
5. **Master is ALSO red on this gate (a pre-existing, unfixed defect, measured)** —
   pristine `master` at `32932668` in a throwaway worktree:
   ```
   2 function(s) spawn a real gopi worker/merger WITHOUT the repo-context sandbox:
         merger_restart_e2e_test.go:281: mreStartLoop()
         merger_restart_e2e_test.go:455: TestMergerRestartReplacesTheLoopWithoutOverlap()
   FAIL	github.com/nick/gopi/cmd/agent	3.007s
   ```
   Both were introduced by `f234658c` ("feat(merger): a real stop/restart control
   surface for the resident loop") and survived `19e06bb2`.
   **Timestamped correction (observed, then superseded):** that measurement was taken
   while `master` was `32932668`, this branch's base. Master has since advanced, and a
   sibling task landed the same two pins independently (`f62d1314` "pin the repo context
   at the merger-restart e2e spawn sites", merged in `b5dde57e`). This branch was merged
   with master; the merge kept master's wording for `mreStartLoop`'s pin and dropped this
   branch's duplicate in `TestMergerRestartReplacesTheLoopWithoutOverlap`.
   Measured after that merge, master still carries NONE of the remaining pins this branch adds:
   `git show master:cmd/agent/merger_retired_flags_test.go | grep -c SandboxRepoContext` → `0`;
   same for `daemon_cgroup_e2e_test.go` → `0`; `merger_work_lock_e2e_test.go` → no match;
   and master's `TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound` (line 504-506)
   goes `bin := wdBin(t)` straight to `home := t.TempDir()` with no pin.
   **So on today's master the 21-site hole does not reproduce at 21** — it reproduces at
   19 minus the two already-pinned sites; the deliverable that is NOT on master is the
   recognition set (`wdRun`/`mdRun`/`mloopStart`), the control/daemon predicate, the
   escape-marker count and the raised read-floor. Master's guard still has
   `rcsScanFloor = 15` and no `wdRun` (measured: `git show master:cmd/agent/repo_context_sandbox_guard_test.go | grep -c wdRun` → `0`).
   **Pre-close re-merge:** master advanced again (`b5dde57e` → `f7bd2616`); this branch was
   re-merged onto `f7bd2616` clean, zero conflicts (`065e484d`), and the gate re-verified
   green with the same read-floor line (85/69/1/15/1).
**Final close-out re-verification ON THE TIP `16ffc9e7`** (fresh worktree
`/home/user/workspace/gopi-rcs-final`, because the peer release pass freed `/home/user/workspace/gopi-rcs-wdrun2`
too — incident #2 of the same class): `git rev-parse --abbrev-ref HEAD` →
`fix/repo-context-gate-wdrun`; `git log --oneline -1` → `16ffc9e7`; `git status --porcelain`
printed nothing BEFORE and AFTER the run. `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`
→ exit 0. `go test -count=1 -v -run 'TestSpawnersPinTheRepoContext|TestRepoContextHazard' ./cmd/agent/`:

```
=== RUN   TestSpawnersPinTheRepoContext
    repo_context_sandbox_guard_test.go:363: repo-context gate: 85 lifecycle spawn site(s) scanned, 69 pinned, 1 escaped, 15 daemon-start (own-dir) site(s), 1 escape marker(s)
--- PASS: TestSpawnersPinTheRepoContext (0.09s)
=== RUN   TestRepoContextHazard_TheReleasePassFreesAnUnownedWorktreeOfTheCwdRepo
--- PASS: TestRepoContextHazard_TheReleasePassFreesAnUnownedWorktreeOfTheCwdRepo (3.63s)
PASS
ok  	github.com/nick/gopi/cmd/agent	4.075s
```
