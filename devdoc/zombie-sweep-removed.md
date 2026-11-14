<!-- doc-class: normative -->
# The zombie sweeper is RETIRED (2026-10-26) — gopi reports a stuck row, it never rewrites one

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**What changed, in one sentence:** the mechanism that automatically moved a
"zombie" task row to a terminal status is deleted — code, CLI verb, config knobs
and the automatic leg on the lifecycle tick — and what survives is the stale
**report**, which was always the half that told a human something useful.

## Why it was removed

The disposer's premise was that a stuck row is RUBBISH to be cleared. It is the
opposite: a row stuck at `in_progress`/`claimed` whose holder is provably gone is
**evidence that a close-out never ran**. Something did not finish — a process died
mid-round, a `session_end` never wrote, a writeback path is broken. The disposer
converted that evidence into a tidy `failed` row with a `zombie` reason, and with
it went the only signal that the close-out path was defective.

So the mechanism traded a diagnosable defect for a clean ledger. The operator's
ruling is that gopi does not do that: a stuck row stays exactly where it is, the
lifecycle pass keeps reporting it (`stale reported=N`), and closing it out is a
human decision. That pressure — a visibly stuck row nobody swept — is what gets
the close-out path fixed instead of hidden.

## What is GONE (and must not come back)

| removed | was |
|---|---|
| `gopi task sweep-zombies` (all flags) | the manual verb: `--dry-run` / `--kill` / `--stall-sec` / `--max-run` |
| `autoDisposeConfirmedZombies` + its call in `taskLifecyclePass` | the AUTOMATIC leg: every worker round disposed confirmed zombies past a second grace |
| `internal/tools/taskzombie_extinct.go`, `taskzombie_sweep.go`, `taskzombie_unowned.go` (+ tests) | the disposal machinery: "extinct" rows, the sweep core, the unowned/never-started lane |
| `cmd/agent/cli_tasks_sweep_zombies_test.go`, `cli_tasks_zombie_dispose_test.go`, `task_lifecycle_litter_test.go`, `task_lifecycle_unowned_test.go` | their tests |
| `task_max_run_sec` / `GOPI_TASK_MAX_RUN_SEC` | the wallclock backstop — it only fed disposal |
| `task_dispose_sec` / `GOPI_TASK_DISPOSE_SEC` | the auto-disposal grace — it only fed disposal |
| `config.ResolveTaskGracePeriods` (triple) | replaced by `config.ResolveTaskStallGrace` (single) |

**The retired verb is not silent.** It stays declared in
`taskverbs.RemovedVerbs`, so `gopi task sweep-zombies` exits **1** with a sentence
naming the replacement and telling the operator what to do with a stuck row. It
does **not** fall through to the generic usage dump, and it does **not** exit 0 —
a 0 would tell an old script "nothing to sweep", which is a lie. This is the same
contract as the retired `gopi task run` (`worker_run_entry_test.go`).

## What SURVIVES — and why `taskzombie.go` was NOT deleted

The **stale report** (`DetectStaleTasks` → the notify sink) is untouched. It is
the whole point of keeping the ledger honest about a stuck row.

Its evidence chain also survives, in `internal/tools/taskzombie.go`:
`pidAlive`, `pidIsZombie`, `claimHolder`, `ClaimHolderPID`, `hostnameOrEmpty`,
`claimStampFreshness`, `sessionIdleSeconds`, `zombieVerdict`, `taskZombieEvidence`,
`taskZombieBumpSQL`. Rule 43 decides this by REFERENCE DISTRIBUTION, not by the
filename: 20+ files reference those symbols, and they are what lets a report say
*"the holder pid 12345 on host X is dead"* instead of *"cannot tell"*. The file is
named for the vocabulary of the old epic, but its contents are the report's
foundation. A tentative rename to `staleness_evidence.go` was made and **reverted**
— the name is still accurate enough, and 20+ documents cite the path.

The **finish-cleanup** leg of the same tick also survives, deliberately: it frees
a dead holder's WORKTREE (a directory), which is a resource leak rather than a
ledger verdict, and it rewrites no row's history.

## What happens to a stuck row NOW

1. The lifecycle pass reports it: `[<role>] [lifecycle] stale reported=N sink=…`
   (the `disposed=` column is gone with the disposer — a permanently-zero field
   would read as a live control surface).
2. If a claim stamp names a process that is verifiably dead, the report says so.
3. Nothing moves it. A human closes it out:
   `gopi task update <id> --status completed` / `--status failed`, or a new run of
   the row finishes it.

## The knobs: a value on disk is IGNORED, not accepted

`task_max_run_sec` and `task_dispose_sec` lost their only readers, so they were
removed from the settings catalog, the resolver and `CONFIG.md` **in the same
commit**. A deployment that still exports the old spellings gets **no effect** —
not an error, and not a silent acceptance of a knob nothing consumes. Rule 21 is
the reason they could not stay behind as accepted-and-ignored keys: a knob that
reads as a working control surface while no code consumes it is worse than an
absent one, because an operator who sets `task_dispose_sec` would conclude a
disposal policy is in force.

`task_stall_sec` / `GOPI_TASK_STALL_SEC` (legacy `GOPI_TASK_STALE_SEC`) stays — it
is read by the surviving report.

## The tests, and why they were INVERTED rather than dropped

The disposal tests lost their subject, so keeping them would have been a test
against a deleted leg — the parser-shaped way a capability comes back (a test that
fails when the mechanism is removed, so the mechanism "must" stay).

The coverage inverted with the risk. While the disposer existed the danger was *"a
stuck row is never recovered"*; now the danger is *"a stuck row is silently
rewritten by something in the background"* — the exact behaviour removed here, and
the one a later well-meaning change would reintroduce. So
`TestTaskLifecycleTick_DoesNotDisposeAStaleConfirmedZombie` asserts the negative on
the strongest fixture available (a genuinely REAPED pid in the claim stamp, a
heartbeat an hour old): one full tick leaves the status unchanged, writes **no**
disposal event, still reports the row, and the operator line no longer carries
`disposed=`. Its own precondition is asserted first, so a seed that failed to
stick cannot make the test pass vacuously.

On the real binary, `TestTaskVerbRetiredSweepZombiesExitsNonZero` (help surface)
and `TestRetiredSweepZombiesVerbRefusesEveryShape` (parse layer) pin the pointed
refusal and the rejection of the retired flags.

## Rule 43 applied: what was MOVED out before the files were deleted

The four deleted cmd test files were not all mechanism. Their generic half is used
by tests whose subject survives (the stale read path, the merged-config path, the
config/env pairing tests), so it was moved to
`cmd/agent/task_cli_testhelpers_test.go` **in the same commit** — Rule 43's open
warning, that a delete which takes a shared symbol with it turns `go build ./...`
red, and that a red master is what the next workflow clones from:

| moved (renamed to drop the dead verb from its name) | used by |
|---|---|
| `sweepZombiesRequireBin` → `requireE2EGopiBin` (+ `buildE2EGopiBinary`, `trimBuildOutput`) | `cli_config_run_repo_test.go`, `ledger_settings_value_test.go`, `cli_tasks_list_sweep_test.go` |
| `sweepZombiesEnv` → `staleCLIEnv` | `cli_tasks_list_sweep_test.go` |
| `seedSweepZombieCLI` → `seedStaleRowCLI` | `cli_tasks_list_sweep_test.go` |
| `taskUpdatedAtRaw`, `memoryDBPathForTest` (unchanged) | the stale read-path E2E pair |
| `disposeFixture`, `taskStatusOfRow`, `zombieResultOf` | → re-homed as `seedStaleDeadHolderRow`, `zwTaskStatus`, `zwEventDetail` in `task_lifecycle_tick_test.go` |
| `zombieLegacyStallEnv` | `task_lifecycle_tick_test.go` |

**The reference instrument was the compiler, not a grep.** `go test -gcflags=-e`
printed every surviving reference in one pass (`taskUpdatedAtRaw` ×11,
`taskStatusOfRow` ×7, `requireE2EGopiBin` ×4, …). A first attempt at a shell symbol
census produced an empty-name regex that matched every identifier — recorded here
because the lesson is Rule 43's own: a count is not a judgement, and the compiler
is the honest census.

**Deliberately NOT renamed:** `zwPinSweepToSandbox`
(`cmd/agent/task_lifecycle_tick_test.go`). `repo_context_sandbox_guard_test.go`
names that identifier as one of the pin's three entry points
(`rcsLegacyPinFuncName`) and counts a call to it as a pinned spawn site — renaming
it would silently subtract every lifecycle test from that gate's verdict. The
"Sweep" in its name is now historical; the pin is not.

## Appendix — the report path, still intact

`serveStaleSweep` → `DetectStaleTasks` → the notify sink configured by
`notify.cmd` / `notify.events`. `stale` remains a subscribable event. Nothing about
delivery changed; only the disposition did.
