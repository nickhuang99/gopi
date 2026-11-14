<!-- doc-class: historical -->
# Task Report: repo-context gate re-run on current master (2026-09-16)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch: `fix/repo-context-gate-reverify` — worktree `~/workspace/gopi-rectx-verify`.
Base: master `0dac2ae7`, with `fix/cmdagent-repo-context-sandbox` (tip `5271631b`,
merge-base `796b3333`) merged in → HEAD `6bf2a37a`.

## Completed

- **Re-measured the gate** on `master + the fix branch` (not copied):
  `repo-context gate: 53 lifecycle spawn site(s) scanned, 50 pinned, 3 escaped`.
  The fix branch's published `46/43/3` was an **undercount**, and the gap is not
  master drift (master introduces no new spawn shape since `796b3333`) — it is two
  holes in the gate's own recognition set, both closed here.
- **Hole 1 — the package-local spawn helper was not a spawn entry point.**
  `rcsIsSpawnCall` knew `exec.Command*`/testutil runners, not `runGopiIn` — the
  helper master uses for real `gopi merger --stop` and `gopi worker --once` probes.
  Added `rcsLocalRunnerNames` (7 local runners), matched in both the qualified and
  the package-local call shape.
- **Hole 2 — a spread argv made a call read as verbless.** `…, tc.argv...` carries
  no verb elements at the call site, so the site was never counted. Added
  `rcsExprIsSpreadOfNamedSlice` (incl. the qualified `tc.argv...` shape) plus
  `rcsFunctionArgvNamesLifecycleVerb`, which reads the enclosing function's argv
  tables (`[]string{…}`, `argv = append(…)`) with the same adjacency rule exactly
  when the call's own argv is unresolvable. Two over-inclusions were then excluded
  on product semantics: `-h`/`--help` is the zero-side-effect usage query
  (`rcsArgvIsHelpQuery`, checked first), and `worker --status`/`--stop` stay
  read-only controls.
- **Pinned the 5 newly-revealed sites** (each measured unpinned — its child
  inherited the live repository):
  `merger_stop_force_modifier_test.go` `TestMergerStopForceAcceptedByCLI`,
  `status_action_conflict_e2e_test.go` `TestAliasDaemonStatusIsRefused`,
  `peak_guard_removal_test.go` `TestCostFreeVerbsRunInsideAPeakWindow`,
  `cli_tasks_worker_unknown_arg_test.go` `TestWorkerAcceptedArgumentsParse` and
  `TestWorkerUnknownArgumentIsRefused`.
- **Added the third pin shape** the two cases above actually need:
  `SandboxRepoContextInFixtureRepo` (shared `repoCtxPinTo` with
  `SandboxRepoContext`). Reason, measured: `gopi task submit` refuses a
  non-repository cwd, and the `task run` wrapper resolves the task's repo from its
  cwd (so a repo-less child makes the assertion pass vacuously). The fixture pin
  keeps the empty settings table + fresh ledger and points the cwd at a throwaway
  one-commit repo, **re-measured** to hold exactly one worktree (the main checkout);
  settings + ledger are written outside it. Documented escape markers: unchanged
  (3, all with stated reasons).
- **Ordering trap found and fixed**: `gopiTestBinPath` / `buildGopiForWorkerHelp` /
  `peakGuardProofBinary` build with a relative `go build . `, so the pin must come
  **after** the binary is resolved — pin-first makes the build fail, and in the
  resolver that skips, that is a silently green no-op. All three pinned tests now
  resolve the binary first.
- **Honest scope section in the devdoc**: the gate scans `cmd/agent/*_test.go`
  only; `runGopiInDir`'s new `dir` parameter is not yet read by
  `rcsIsRunnerWithDirArg`/`rcsFunctionSetsOwnWorkingDir`, which can only produce a
  false POSITIVE (no scanned call passes it a non-empty literal today).

### Branch sweep (newer than `796b3333`, touching `cmd/agent/*_test.go`)

Re-checked (present in the measured tree / read directly):

| branch | tip | own test commits | status |
| --- | --- | --- | --- |
| `fix/cmdagent-repo-context-sandbox` | `5271631b` | 3 | merged into HEAD → measured |
| master `fix/merge-rebuild-loop` | `0dac2ae7` | 1 (`569a5b0e`) | master → measured (no new spawn shape) |
| `fix/repo-context-gate-reverify` | `6bf2a37a` | 4 | this branch |

Read directly but **NOT** merged, therefore not re-checked by the gate run:

| branch | tip | own test commits | finding |
| --- | --- | --- | --- |
| `fix/heavy-tests-classification` | `5a159274` | 1 | adds ONLY `heavytest.Require(...)` lines — no spawn shape (measured, 0 spawn-shape lines in its diff) → cannot arm the hazard |
| `fix/merger-busy-liveness` | `d7f24665` | 1 | adds `merger_busy_recycle_red_test.go` with 2 unpinned `runGopiIn(..., CliVerbMerger, …)` sites; the file does **not compile** against current master (`tools.MergerNotInFlightWord`, `MergerState.ProcStart`, `MergerStateFile.WriterLive` are absent), so it cannot be built or run — see "Not Done" |

Older (`NOT-descendant` of `796b3333`, i.e. not "newer than it" in the epic's
sense): 15 refs, incl. `hp/*`, `archive/*`, `safety/pre-rebase-hip`,
`fix/signalrec-verify-attribution` (`e9994f4e`) — recorded, not re-checked, since
their merge-bases predate the fix.

## Not Done (honest)

- **A measured gate run on `fix/merger-busy-liveness` could not be produced.** Its
  only `cmd/agent` test commit adds a file that fails to compile against current
  master (three undefined `internal/tools` symbols — the branch is written against
  an unmerged sibling's API). `go test` therefore cannot build an instrumented tree
  for it here. Its two merger spawn sites are unpinned as written; once that branch
  (or the API it needs) lands, the hardened gate will name them.
- **No mutation A/B was run for the whole suite** — only the targeted
  acceptance runs below. A full `go test ./cmd/agent/` was started as a background
  job during this task and had not finished within the session's budget; the
  three acceptance tests the epic names were run directly, with `-count=1`, against
  a freshly built `bin/gopi`.
- **`fix/heavy-tests-classification` was read, not run** (it is not merged; its
  diff adds no spawn shape, so there is nothing for this gate to measure).

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
  (22791168 bytes). `go build ./...` → OK.
- **Gate (measured)**: `go test -count=1 -run TestSpawnersPinTheRepoContext ./cmd/agent/`
  → PASS, printing
  `repo-context gate: 53 lifecycle spawn site(s) scanned, 50 pinned, 3 escaped`
  (escapes logged individually:
  `daemon_scope_restart_e2e_test.go:37 dsRunRestart()`,
  `daemon_test_leak_e2e_test.go:109 runDaemonLeakSpawner()`,
  `worker_daemon_singleton_e2e_test.go:61 wdForeground()` — all three carry a
  `// repo-context-ok:` reason).
- **Acceptance trio**: `go test -count=1 -run
  'TestSpawnersPinTheRepoContext|TestRepoContextHazard|TestWorkerWaitsAndPicksUpNewTaskWithoutRestart' ./cmd/agent/`
  → `ok github.com/nick/gopi/cmd/agent 4.392s` (individual: the gate PASS 0.07s,
  `TestRepoContextHazard_…` PASS 0.42s, `TestWorkerWaitsAndPicksUpNewTaskWithoutRestart`
  PASS 3.08s).
- **Newly pinned tests individually**: `TestMergerStopForceAcceptedByCLI` PASS,
  `TestModifierForceIsNotASecondAction` PASS, `TestAliasDaemonStatusIsRefused` PASS,
  `TestCostFreeVerbsRunInsideAPeakWindow` PASS, `TestWorkerAcceptedArgumentsParse`
  PASS (incl. both help subtests), `TestWorkerUnknownArgumentIsRefused` PASS,
  `TestWorkerStatusCLIRefusesGhostRecordsAndBoundsTheResidue` PASS.
- **Mutation (red) proof of the hardened recognition**: with the five pins
  reverted, the gate FAILS and names exactly those five functions
  (`agent_spawn_guard_test.go:181`, `cli_tasks_worker_unknown_arg_test.go:224`,
  `merger_stop_force_modifier_test.go:102`, `peak_guard_removal_test.go:283`,
  `status_action_conflict_e2e_test.go:356`); restored, 53/50/3 and green.
- **Hazard-check**: `git worktree list` before and after the runs listed the same
  11 worktrees — no victim vanished during this session's suite runs (Rule 27 was
  never approached: no load/stress test of any kind was written or run).
- **Commit**: `158db3fe` (gate widening + fixture pin), `e0d42b0a` (the five pins), `75bf9d0f` (docs) — tip `75bf9d0f` on `fix/repo-context-gate-reverify`.

## Deliverable

- Branch: `fix/repo-context-gate-reverify` (worktree `~/workspace/gopi-rectx-verify`).
- Recovery: `git -C /home/user/work/GoPi show fix/repo-context-gate-reverify:devdoc/repo-context-gate-reverify.md`
  (and `…:cmd/agent/repo_context_sandbox_guard_test.go`).
- The worktree shares the main repo's refs, so the commit is already on the branch
  in `/home/user/work/GoPi` — no push needed.
