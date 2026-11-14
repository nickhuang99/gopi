<!-- doc-class: historical -->

> **RETIRED 2026-10-26 — this page describes machinery that NO LONGER EXISTS.**
> `gopi task sweep-zombies` and the automatic zombie disposer were deleted whole; a stuck
> row is now REPORTED and left exactly where it is. Read this page as history, not as
> instructions: any command below that names `sweep-zombies` exits 1 with a pointer.
> The current behaviour is `devdoc/zombie-sweep-removed.md`.

# Fix: `gopi task sweep-zombies --dry-run` must be side-effect free (notify half)

Branch: `fix/sweep-zombies-dryrun-notify` · 2026-09-12 · Rule 15 (no silent side
effect) class.

## Completed

- `--dry-run` now suppresses BOTH sweep side effects: the stale notify fan-out
  AND the stall re-arm bump (`updated_at`).
- Usage text (`gopi task sweep-zombies --help`) + `internal/config/help.go`
  corrected: the old text admitted the bug ("same output, no side effects
  besides the stale notify").
- 3 regression tests: byte-level sink guard (dry run), ledger-clock guard
  (dry run), and the non-dry delivery guard (the sweep is not muted).
- Test-harness hermeticity, two layers: the tools fixture pinned `GOPI_SETTINGS`;
  the shared suite entry point (`internal/testutil.PinSuiteNotifyEnv`) now
  sandboxes the notify environment for ALL 17 test suites (env vars dropped +
  `GOPI_SETTINGS` pointed at an empty per-run path) — see "Third bug" below,
  which is what re-verification of this very report exposed.
- The CLI E2E tests now resolve the binary LOUDLY (`sweepZombiesRequireBin` — a
  skip could hide the whole guard).
- Red proof on the pre-fix tree + a live A/B on both binaries.

## Commit

`66b48547` fix(sweep-zombies): --dry-run is side-effect free — gate the notify half too
`0850d2f0` test(sweep-zombies): red-proof the dry-run sink guard + record the A/B
`9e2fee78` test(sweep-zombies): make the dry-run guard loud (never skip) + assert the ledger clock
`c5269d06` test(suite): sandbox the notify env for every suite (the third bug below)

## Symptom

`gopi task sweep-zombies --dry-run --stall-sec 30` printed "7 candidate(s)"
and, while it ran, `~/.gopi/notify/task-events.jsonl` gained FRESH
`"event":"stale"` records (14:41:5x / 14:47:5x) carrying
`zombie={"confidence":"confirmed","pid_dead":true,…}`. A read-only preview was
publishing real operator alarms.

## Root cause

`cmd/agent/cli_tasks.go` `runTaskSweepZombies` gated ONLY the disposal half
with `--dry-run`:

```go
kill := opts.killZombies && !opts.dryRun      // gated
… tools.SweepZombies(tools.SweepZombiesOptions{StallSec:…, MaxRunSec:…, Kill: kill})  // Notify left nil
```

and `internal/tools/taskzombie_sweep.go` read the nil pointer as ON:

```go
if opts.Notify == nil || *opts.Notify { notifyStaleTask(t, staleSince, &ev) }
```

So a dry run fanned out a real `stale` event per candidate — and through the
configured `notify.cmd` sink whatever the operator's command does with it.

## Fix (two halves, both needed for the "dry" claim to be true)

1. **CLI** (`cmd/agent/cli_tasks.go`): `--dry-run` now mirrors its kill gate
   onto the notify half — `notify := !opts.dryRun; Notify: &notify`. Stdout
   output is unchanged (candidates are still listed).
2. **Tools** (`internal/tools/taskzombie_sweep.go`): the Notify switch is
   resolved ONCE into `notifyEnabled` and now also gates the **stall re-arm
   bump** (`taskZombieBumpSQL`, the `updated_at` touch). Reported by the sweep
   bug report as a suspicion ("synthetic row appearing at the same second as
   the probe bursts"); here it is a deliberate decision, not a discovered
   side effect: the bump is the OTHER half of the same notify side effect —
   without the event, a bump would silently postpone the operator's next REAL
   stale alarm by a full stall window. A pass that publishes nothing mutates
   nothing. This holds for every caller (the serve tick passes nil = ON, so
   its loop is unchanged — it bumps exactly when it notifies).

Not changed: the default (no `--dry-run`) report pass still notifies, and
`--kill` still disposes confirmed zombies; the notify gate is independent of
the kill gate (`Notify` is about the sink, `Kill` about the ledger state), so
the two flags never have to move together.

## Verification

Regression tests added (a bug fix ships a repro test):

- `internal/tools`: `TestSweepZombies_NotifyOffIsSideEffectFree` — with the
  sink re-armed to a temp log via `GOPI_TASK_NOTIFY_CMD`, a `Notify:&false`
  pass reports the candidate, delivers nothing, and leaves `updated_at`
  byte-identical (no re-arm bump). It would FAIL on pre-fix code, where the
  nil-vs-false distinction did not exist for the CLI path and the bump was
  unconditional.
- `cmd/agent`: `TestSweepZombiesCLI_DryRunTouchesNoNotifySink` — the
  acceptance test. A sidecar `notify.sh` appends its args to a sink file;
  the assertion is byte-level (sink size unchanged across the run) with
  stdout still listing the candidate.
- `cmd/agent`: `TestSweepZombiesCLI_ReportNotifiesOperator` — the non-dry
  default still delivers (proves the fix narrowed the dry run rather than
  muting the sweep).
- The pre-existing `TestSweepZombiesCLI_NotifiesOperator` asserted the OLD
  (buggy) behaviour (dry-run delivers) and was REPLACED by the two tests
  above — it was the shape of the bug, not a regression guard.

Verification commands (GOPI_BIN pinned to the worktree build so the test does
not pick up `bin/gopi` of the main repo):

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
GOPI_BIN=$PWD/bin/gopi go test -count=1 -run 'TestSweepZombies' ./internal/tools/ ./cmd/agent/
GOPI_BIN=$PWD/bin/gopi go test -count=1 ./internal/tools/ ./cmd/agent/ ./internal/config/ ./internal/standards/
```

Result: `ok internal/tools 199.5s`, `ok cmd/agent 100.3s`,
`ok internal/config 0.3s`, `ok internal/standards 5.5s`.

### Red proof (both new tests fail on PRE-FIX code)

New test files copied into a detached pre-fix worktree (`/tmp/gwv-prefix`,
HEAD 9c604ee8) and run there — both FAIL, for the two halves of the bug:

```
--- FAIL: TestSweepZombiesCLI_DryRunTouchesNoNotifySink (0.07s)
    cli_tasks_sweep_zombies_test.go:196: dry-run must not touch the notify
    sink: size 0 → 1, sink contents: …
--- FAIL: TestSweepZombies_NotifyOffIsSideEffectFree (0.05s)
    taskzombie_sweep_test.go:365: Notify-off pass must not bump updated_at
    (re-arm guard): 1.789195479e+09 → 1.789202679e+09
```

The CLI red proof was then re-run WITHOUT a second worktree, by pointing
`GOPI_BIN` at the main repo's pre-fix `bin/gopi` (master c0211ad4) — same
failure, both observables named:

```
$ GOPI_BIN=/home/user/work/GoPi/bin/gopi go test -count=1 -run 'TestSweepZombiesCLI' -v ./cmd/agent/
=== RUN   TestSweepZombiesCLI_DryRunTouchesNoNotifySink
    dry-run must not touch the notify sink: size 0 → 1, sink contents:
    dry-run must not re-arm the row (updated_at): 1.789195902e+09 → 1.789203102e+09
--- FAIL: TestSweepZombiesCLI_DryRunTouchesNoNotifySink (0.04s)
=== RUN   TestSweepZombiesCLI_ReportNotifiesOperator
--- PASS: TestSweepZombiesCLI_ReportNotifiesOperator (0.04s)
```

`TestSweepZombiesCLI_ReportNotifiesOperator` passing on BOTH trees is the
point: the fix narrowed the dry run, it did not mute the sweep.

A first cut of the CLI test PASSED on pre-fix code — a false green: it handed
the sidecar script PATH to the test helper, which wraps its argument in
`cat >> <path>`, so the sink was never a delivery target and the assertion
proved nothing. It now passes the notify CMD verbatim
(`sweepZombiesEnvNotifyCmd`). Lesson recorded: a green-on-pre-fix test is a
test bug, not a pass.

### Harness lesson: the test can "pass" against the WRONG binary

`internal/testutil.GopiBin` returns `$GOPI_BIN` if set, else `moduleRoot()/
bin/gopi`, and **SKIPS** when neither exists. Two ways that silently produced
a meaningless green while this fix was being verified (2026-09-12):

1. **A skip reads as a pass.** A forgotten `GOPI_BIN` + no `bin/gopi` in the
   tree under test = `--- SKIP` in verbose output, invisible in a `| tail`
   summary. `sweepZombiesRequireBin` (new, in the test file) now resolves the
   binary and `t.Fatalf`s when it cannot — and stamps the resolved ABSOLUTE
   path into `GOPI_BIN` so a later `testutil.RunGopi` call cannot re-resolve
   to a different tree's binary.
2. **`$(pwd)` is not the worktree.** The first hand-written acceptance script
   used `BIN=$1` but let the shell's CWD decide the rest; it was launched from
   the main repo, so `$(pwd)/bin/gopi` was PRE-FIX master while I believed I
   was testing the branch. The accidental A/B (PRE-FIX labelled output) is what
   exposed it — `strings bin/gopi | grep '<new help text>'` settled which tree
   a binary came from. Always pass an ABSOLUTE binary path to an acceptance
   script, and pin `GOPI_BIN` for the tests.

Manual end-to-end (the bug's own repro, hermetic — a sandbox home, never the
live `~/.gopi/notify/task-events.jsonl`): a re-run of the exact repro
against BOTH binaries (script: `/tmp/gwv-acceptance.sh`; pre-fix = detached
worktree at master) gave:

```
--- PRE-FIX : dry-run delivered 1 notify event(s)
sweep-zombies (dry-run): 1 candidate(s), stall grace 1800s, max-run 0s
  t-dryrun-repro confirmed (stalled) — heartbeat stop + claimed pid 999999 dead — confirmed zombie
--- POST-FIX: dry-run delivered 0 notify event(s)
sweep-zombies (dry-run): 1 candidate(s), stall grace 1800s, max-run 0s
  t-dryrun-repro confirmed (stalled) — heartbeat stop + claimed pid 999999 dead — confirmed zombie
```

Same stdout (the preview is intact), sink count 1 → 0. The full `gopi task
sweep-zombies --dry-run --stall-sec N` acceptance (sink byte-size unchanged
across the run, candidates still listed) is also asserted in
`TestSweepZombiesCLI_DryRunTouchesNoNotifySink` with a sidecar notify.cmd.

### Acceptance re-run on the live CLI (both binaries, one sandbox, absolute paths)

Fixture: sandbox home, schema-only clone of the real ledger (`.schema` piped
into a fresh DB — the real 83 MB DB is never copied or touched), one row
seeded by the real CLI (`gopi task submit "acc dryrun probe" --payload
acceptance`), then backdated to `in_progress` with a REAPED pid (sleep 3600 &
kill -9), `last_toolcall_at` NULL, and a sidecar `notify.sh` appending `$*` to
a sink file. Both A and B ran that same fixture, each preceded by the fixture
reset.

| step | PRE-FIX (`/home/user/work/GoPi/bin/gopi`, master c0211ad4) | POST-FIX (`gopi-dryrun-notify/bin/gopi`) |
|---|---|---|
| dry-run stdout | `1 candidate(s) … t-acc-dry2 confirmed (stalled)` | `1 candidate(s) … t-acc-dry2 confirmed (stalled)` |
| sink bytes across the dry run | 0 → **1** (one real event delivered) | 0 → **0** |
| row `updated_at` across the dry run | reset 1789195872 → **1789203073** (+7201s, re-armed) | reset 1789195873 → **1789195873** (unchanged) |
| a REAL pass immediately after | `no zombie rows` — the dry run had re-armed the row away | `1 candidate(s)` — the event still fires |

The last row is the user-visible damage of the bump: with the pre-fix binary,
running the dry run and then the real sweep made the real sweep report
NOTHING for the very row the preview had just shown. Post-fix, the dry run is
invisible to the next real pass.

Live-sink safety AT THE TIME OF WRITING: `~/.gopi/notify/task-events.jsonl`
mtime/size were checked before and after every step (47220336 bytes, 16:50:19)
— untouched; all fixture traffic went to the sandbox sink.

**That claim did not survive the re-verification, and the correction is the
third bug** (see below): the very `go test` run used to re-verify this report
delivered ~200 test task-ids into the same live sink at 16:57–16:59 (47,492,049
bytes by 17:00). The measurement was honest at the moment it was taken — the
per-test fixture was sandboxed — but the SUITE was not, so the next suite run
reopened the leak. "Untouched right now" is not "cannot be touched"; only the
suite-level pin makes it a property of the test tree.

## Second bug found while verifying (fixed here too)

`internal/tools/sweepFixture` unset `GOPI_TASK_NOTIFY_CMD`/`WEBHOOK` but
`GOPI_HOME` was unset, so the config layer fell back to the operator's real
`~/.gopi/settings.json` — whose notify.cmd is
`cat >> /home/user/.gopi/notify/task-events.jsonl`. A fixture kill therefore
ATTEMPTED delivery into the LIVE sink (observed: `[task-notify] cmd "cat >>
/home/user/.gopi/notify/task-events.jsonl": fork/exec /usr/bin/sh: resource
temporarily unavailable`, and the test took 119s instead of 4.5s as the fork
retried). Fix: the fixture now pins `GOPI_SETTINGS` at a temp path, so the
fallback has nothing to find. Verified: 0 live-sink mentions and the 119s
blocked-fork runtime collapsed to 4.5s.

## Third bug found while re-verifying (fixed here too, in testutil)

The fixture fix above repaired ONE test; the leak was suite-wide. Both suite
entry points (`internal/tools/testmain_test.go`, `cmd/agent/testmain_test.go`)
dropped the five `GOPI_TASK_NOTIFY_*` vars and called the suite hermetic — but
`config.ResolveNotify` resolves env **> settings.json**, and the durable layer
reads `ResolveSettingsPath()`: the ambient `GOPI_SETTINGS`, i.e. the operator's
real `~/.gopi/settings.json`. Dropping the env vars removed the OVERRIDE and
handed resolution back to that file. Measured 2026-09-12: one unrelated test
(`TestBidirEpic1SelfSubmittedAndTerminal`) delivered 655 bytes, and a full
`go test ./internal/tools ./cmd/agent` flooded ~200 test task-ids
(`t-cli-e2e`, `t-div`, `t-c1`, …) into the live sink within two minutes.

Fix (one place, every suite inherits it): `internal/testutil/suiteenv.go` —
`PinSuiteNotifyEnv` drops the five vars AND points `GOPI_SETTINGS` at an
uncreated per-run path inside the pinned scratch root, wired into
`PinSuiteForTestMain` and `RunSuiteMain`, so all 17 TestMains are covered
without repeating the loop. Unsetting `GOPI_SETTINGS` would have been WRONG:
resolution then falls back to `DefaultSettingsPath()` — the same operator file
under HOME — which is why the fix must point, not clear (the red proof covers
both shapes). Guards: `testutil.AssertSuiteNotifyEnvHermetic` +
`AssertSuiteHasNoNotifyTarget`, called as a one-line test by the three suites
that own the risk (`internal/tools`, `cmd/agent`, `internal/agent`);
`TestEveryTestMainSuiteIsNotifyHermetic` is the STATIC gate — every package
whose tests carry a `TestMain` must route it through `internal/testutil`, so a
new hand-rolled entry point cannot bypass the pin (Rule 21: the invariant has a
real reader, not just a documented intent). Finally,
`TestSuiteNotifyEnvRawSuiteFails` re-execs the binary in the
PRE-FIX entry-point shape (scratch pin only, ambient notify env) and fails if
the anchor stays green, so the guard is proven to bite on the defect shape and
not merely on a re-creation of it.

## Verification (this session's re-verification)

- `go build ./...`: OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`: OK.
- **Acceptance property (the one the bug is about)**: sink
  `~/.gopi/notify/task-events.jsonl` — **47,492,049 bytes /
  sha256`206691783b7673cd…` before AND after** every verification run below
  (including `go test ./...`, the previously-flooding
  `./internal/tools ./cmd/agent` pair, and the full-suite run).
- `GOPI_BIN=<worktree>/bin/gopi go test -count=1 ./cmd/agent/`: **ok** (87.4s) —
  includes `TestSweepZombiesCLI_DryRunTouchesNoNotifySink`,
  `_DryRunReportsNotKills`, `_ReportNotifiesOperator`, `_KillMarksFailed`,
  `_LiveRowUntouched`.
- `go test -count=1 ./internal/tools/`: **ok** (177.7s) — the suite that used to
  flood the sink.
- `go test -count=1 ./internal/standards/`: **ok** — Rule 16…21 all green after
  the two STALE permits were pruned.
- `go test -count=1 ./internal/hippocampus/`: **ok** — the doc-anchor gate
  (`TestHippocampusDocAnchorsResolve`, lives in `internal/agent`) green again.
- `go test -run TestSuiteNotifyEnv -v ./internal/testutil/`: PASS (anchor green
  under the pin; both RAW pre-fix children red with the actionable message).
- `TestEveryTestMainSuiteIsNotifyHermetic` + `TestEveryTempUsingSuiteIsPinned`: PASS.
- **Pre-existing failures, NOT introduced here** (each verified on the untouched
  base commit `c0211ad4` in a separate worktree — report faithfully):
  - `internal/testutil TestWorkerOwnershipPointerPresent`: six files lack the
    `WORKER-OWNERSHIP: OPERATOR-ONLY` pointer line. Identical on base.
  - `internal/stylegate TestStyleGate`: `test-skip` 1, `test-sleep` 3, and the
    swept `ignored-error` ceiling (182) already exceeded on base (measured 186
    test hits there; this branch measures **184** — the suite pin's own
    `os.Unsetenv` discards were accounted for, so the delta is −2, not +2). No
    new site was introduced; the rules were already red on `c0211ad4`.
  - `internal/agent` SIGSEGV (`signal arrived during cgo execution`,
    `getaddrinfo` inside a network dial): a flake of that suite, not a
    deterministic failure — the same suite passed green in the run before it
    (`/tmp/final.log`: `ok internal/agent 67.5s`), and every assertion of the
    rule under test there (`TestHippocampusDocAnchorsResolve`) passes in
    isolation. Recorded as observed, not fixed.

## Fourth wave: what the full-suite run turned red (all fixed here)

Landing the suite pin moves the tree's baseline, and the machine-checkable
gates are designed to notice — every one of the three was a real consequence,
not noise:

1. **Rule 17 baseline went STALE** (`internal/standards/baseline.go`): the two
   `os.Unsetenv` entries were `cmd/agent/testmain_test.go:30` and
   `internal/tools/testmain_test.go:43` — the very loops the pin replaced.
   Removed, and the gate's rendered per-gate reason re-measured against the
   new 89-entry list (63 `os.Unsetenv` / 26 `os.Setenv`; 26 markers on the
   offending line, 38 within ±3, every on-line marker on a `Setenv` line).
   A stale permit is a silent permission, so this deletion IS the ratchet.
2. **stylegate ignored-error, production, 1 hit** — `os.Unsetenv(env)` in the
   new `suiteenv.go`. Swept to zero by the constant's own sweep, so the fix is
   the code, not the ceiling: the error is now checked and reported to stderr
   (Rule 15b — a failed unset leaves the ambient leak this pin exists to close).
3. **`TestCovCrDetectStaleTasks_FlagsStaleAndGuardsLiveRows` failed** — and this
   is the most instructive: it PASSED on the pre-fix tree only because it read
   the operator's real settings.json, whose `notify.events` lists `stale`; the
   tools default set is `claimed,in_progress,completed,failed`
   (`taskNotifyEventsDefault`), so the event only arrives if the AMBIENT config
   says so. The test now pins `GOPI_TASK_NOTIFY_EVENTS` itself (naming the
   event through the newly-exported `tools.TaskHeartbeatNotifyEvent` — Rule 1b
   instead of a bare `"stale"`). This is the leak paying a second dividend:
   silencing it exposed a test whose delivery was never self-contained.
   A grep of the other 25 `os.Setenv`-class sites found no further
   settings-dependent delivery (all pin `NotifyEventsEnv` themselves).

The stylegate's 185-test-hit→ceiling-182 delta is NOT from these changes: the
pre-fix base commit `c0211ad4` measures 186 hits (test-sleep and test-skip hits
included) — the sweep's ratchet was already exceeded before this branch. No new
test-side ignored-error site was introduced (verified per site).

## End-to-end acceptance (REAL binary, isolated home, REAL notify.cmd sink)

Not only unit tests: the worktree binary was driven exactly as the briefing
specifies — an isolated `GOPI_HOME` + `GOPI_SETTINGS` whose `notify.cmd` is a
sidecar script appending to a sink file (the operator's shape), one seeded row
backdated to a stalled `in_progress` with a dead claim stamp.

| step | stdout | sink |
|---|---|---|
| before | — | 2 bytes |
| `gopi task sweep-zombies --dry-run --stall-sec 1` | `1 candidate(s) … t-acc-1 suspected (stalled)` | **2 bytes (unchanged)** |
| `gopi task sweep-zombies --stall-sec 1` (real pass) | same preview + `[task-notify] cmd ok` | 3 bytes (**+1, still delivers**) |

That is the acceptance criterion pair from the briefing, measured on the shipped
artifact: `--dry-run` lists the candidates and writes ZERO notify bytes; the
default report mode still fires the event.

## Not done (deliberately)

- `internal/tools.taskheartbeat.DetectStaleTasks` (the serve tick's detection
  path) still notifies and bumps unconditionally — it has no dry-run concept
  and must not gain one: it IS the operator's alarm. Only the CLI sweep verb
  gained the read-only mode.
- The `Notify *bool` shape is kept (nil = ON). Making it a plain `bool` would
  turn every existing caller that omits the field into "notify off" — a silent
  behaviour change on the serve path (Rule 15a: zero-value semantics). The
  tri-state is intentional.
- No change to the `--kill` semantics or to what a real pass does.

## Notes for the next agent

- The CLI E2E suite (`cmd/agent`) requires `GOPI_BIN` (or `bin/gopi` at the
  module root); `internal/testutil.GopiBin` SKIPS when neither exists, so a
  silently skipped E2E looks green. Always export `GOPI_BIN` explicitly —
  and to the WORKTREE build, else the test exercises another tree's code.
- The notify env is sandboxed by the shared suite entry point
  (`internal/testutil.PinSuiteNotifyEnv`, called from `TestMain`): the five
  `GOPI_TASK_NOTIFY_*` vars are dropped AND `GOPI_SETTINGS` points at an empty
  per-run sandbox path, so an unconfigured test resolves NO notify target. A
  fixture that wants delivery must re-arm `GOPI_TASK_NOTIFY_CMD` +
  `GOPI_TASK_NOTIFY_EVENTS` itself (as the new tests do) — and a test that
  builds a CHILD environment must pass `GOPI_SETTINGS` explicitly, because the
  child inherits the sandbox path, not the operator's settings file.
