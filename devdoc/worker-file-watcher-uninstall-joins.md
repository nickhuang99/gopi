<!-- doc-class: historical -->
# The file watcher's uninstall joins the poll — a stop that returns before the thing it stopped

Task: `01a1343d-d9c2-793c-a5e2-29538e340c09` · branch `task/01a1343dd9c2793ca5e229538e340c09`
Base: `b888461f2` · Commit: `2b102c979`

## The defect, and why a GREEN `-run` set exited 1

`workerDaemonCtx.installFileWatcher` (cmd/agent/cli_worker_control.go) installs the
durable-request poll and returns its teardown. The teardown was:

```go
return func() { close(stopped) }
```

`close(stopped)` makes the loop **RUNNABLE** — it does not make it **gone**. The
caller here is not a production process (which exits and lets the kernel reclaim
the goroutine); it is `cmd/agent`'s own suite, which drives the worker loop
**in-process** (`TestRunTaskWorker_OnceSweepsZombieFirst`,
`TestRunTaskWorker_SurvivesFailedRun`). Its `TestMain` close-out sweep
(`reportSuiteGoroutinesAtCloseOut`, suite_stall_watchdog_test.go) samples the
stack, and it samples it on **the instruction after the deferred teardown** —
with no settling window of any kind. A poll that was merely signalled can
therefore still be on the stack at that instant, get counted as a parked
repository goroutine, and turn `parked > 0 && code == 0` into `code 1`: **every
test PASSES, the package still exits 1.**

This is the SAME defect, and the same fix, as `startSuiteStallWatchdog`'s stop
(suite_stall_watchdog_test.go, measured 2026-09-25: a fully green run failed on
`parked repository goroutines: 1` naming the suite's own instrument). That fix
added a `done` channel the goroutine closes as it leaves and the stop waits on
it, with the comment *"a stop function that returns before the thing it stopped is
not a stop function: it waits for the goroutine to leave."* The watcher was fixed
for installability by `5c5cdb631` but kept the signal-only shape.

## The fix

```go
stopped := make(chan struct{})
exited := make(chan struct{})
var once sync.Once
go func() {
        defer close(exited)
        t := time.NewTicker(workerFileWatchInterval)
        defer t.Stop()
        for {
                select {
                case <-t.C:
                        c.consumeDrainRequests()
                case <-stopped:
                        return
                }
        }
}()
return func() {
        once.Do(func() { close(stopped) })
        <-exited
}
```

Two properties, both deliberate:

- **`once` wraps only the close.** The ordinary caller shape is a deferred stop
  *plus* an explicit one (an early-return path that wants the poll gone before it
  does something else), and a bare `close(stopped)` panics on the second call
  (`close of closed channel`). The **join sits OUTSIDE the once**: the close
  happens once, but every caller waits for the goroutine to leave.
- **The callers are untouched.** Both faces already hold the install return and
  defer it beside the request cleanup
  (`defer workerCtx.installFileWatcher()()`, cli_tasks_worker.go:331 and :420) —
  the install-then-defer-stop shape the fix needs is the shape that was already
  there. No caller change was required, and none was made.

## The regression test — and why the FIRST version of it was worthless

`TestInstallFileWatcherIsUninstallable` already existed (added by `5c5cdb631`)
and it **passed on the broken code**. Its teardown step was:

```go
uninstall()
for countFileWatcherGoroutines() > before && time.Now().Before(deadline) {
        time.Sleep(fileWatcherSettleInterval)   // 10ms, up to 2s
}
```

That **settle window is exactly the difference** between this test and the sweep
it stands in for. A window lets a merely-runnable goroutine leave the stack
before the count; the sweep has no window and samples the next instruction. The
guard was therefore green on the defect it was written to catch — a guard that
reproduced the symptom's *shape* but not its *timing*.

The tightened version:

- the count is taken **the instant `uninstall()` returns** — no sleep, no retry;
- it is an **exact equality** (`!= before`), not `> before`: this test's own
  install must take the number back to where it started;
- the only wait left is **before** the teardown (the wait for the poll to reach
  the stack — a goroutine that never started cannot be observed stopping), and
  it is a 1ms poll against a 2s deadline;
- a **second** `uninstall()` call is pinned as a no-op.

## The measurement, and a second, unrelated leak found on the way

Criterion = the SAME command, the SAME `-run` set, before vs after; and because
the parked count floats with timing, the run was repeated. Scope note: the
`-run` set used for the red/green arms is the **eight worker faces** — the set
whose installs are the subject of this fix — not the ten-name set from the
archived briefing (two of those names, `TestReproWatcherBackspaceDoesNotEditTheLine`
and `TestChatBoundarySignalWatcherIsJoinedOnExit`, are a REPL-line watcher and a
chat-boundary watcher, not `installFileWatcher`; keeping them would have made the
criterion test a different watcher's lifecycle than the one changed here).
Adding `TestInstallFileWatcherIsUninstallable` to the arm set would also corrupt
the measurement: that test's own join changes the very count being measured.

**ARM B — `HEAD~1` (signal-only uninstall), 5 consecutive runs:**

```
B1 parked=1 watcherFrames=1 nonTestFails=1 EXIT=1
B2 parked=1 watcherFrames=1 nonTestFails=1 EXIT=1
B3 parked=1 watcherFrames=1 nonTestFails=1 EXIT=1
B4 parked=1 watcherFrames=1 nonTestFails=1 EXIT=1
B5 parked=1 watcherFrames=1 nonTestFails=1 EXIT=1
```

all eleven tests PASS in every run (`"Action":"fail"` with a `"Test"` field: 0);
the frame named is `workerDaemonCtx.installFileWatcher.func1`. The 1-frame count
is the single worker face that lands its deferred stop inside the sweep's own
sampling instant — an intermittent frame, which is why the run is repeated and
why `watcherFrames=1` (the frame appears in the dump) is the stable signal.

**ARM A — `HEAD` (joined uninstall), 5 consecutive runs:** see the report's
`## Verification` (measured in the same job, B after A so that both arms ran
under the same host conditions).

### The second leak

Three of the post-change runs above (`post-change-2/3/4.json`, same 11-name set)
went **0 / 0 / 1**, and the run-4 frame was NOT the watcher:

```
suite goroutine sweep: parked repository goroutines: 1
  goroutine 233 [runnable]:
  github.com/nick/gopi/internal/scratch.ScanHoldersWithin(...)
  github.com/nick/gopi/cmd/agent.scratchGCTick(...)
      cmd/agent/scratch_gc_tick.go:237 +0xbcc
```

`scratchGCSweepAsync` (scratch_gc_tick.go) runs the resident scratch-GC sweep on
a **fire-and-forget goroutine** (`go func(){ … scratchGCSweepBody(role) }()`) with
a `running` flag for single-flight but **nothing that waits for it to finish**.
On this host that sweep is a CPU-bound scan of every live process's
`/proc/<pid>/maps` (the file itself records 12–18s per firing, measured
2026-10-03), so a sweep that is still walking `/proc` at close-out is counted by
the same sweep and fails an otherwise green run — the identical shape of defect,
in a different producer. It is **out of scope for this task** (its own file, its
own caller, and its own decision about who joins a background hygiene sweep), and
it is filed as its own bug row (Rule 41) rather than papered over: the sweep's
exclusion list is untouched, and no frame name was added to it.

## Verification

- Build: `go build ./...` → exit 0. `go vet ./cmd/agent/` → exit 0.
- Tests: see the report (`## Verification`), same command/set A-vs-B plus the
  tightened guard's red proof.
- Red proof: production file reverted to `HEAD~1` →
  `go test -run '^TestInstallFileWatcherIsUninstallable$' ./cmd/agent/` exits 1,
  failing BOTH ways — the join assertion (`uninstall() returned with the poll
  still on the stack`) and the second-call `panic: close of closed channel` at
  `cli_worker_control.go:2184`, reached from the test's own second `uninstall()`
  at line 107. Restore verified by `git hash-object` against the HEAD blob. Ran:
  `go test -count=1 -timeout 300s -json -run '^TestInstallFileWatcherIsUninstallable$' ./cmd/agent/`
- Result: arm A 5/5 clean, arm B 5/5 red with `watcherFrames=1`; the tight guard
  fails in 0.01s on the reverted file and passes on HEAD.
- Commit: `2b102c979`.

## Not Done (honest)

- The `scratchGCTick` flight frame is **not** fixed here (filed as its own bug).
  A run of the 11-name set can therefore still exit 1 on that frame; the
  eight-name worker-face set is clean.
- The archived briefing's `b2366a62a` branch could not be merged or cherry-picked
  — the object still exists but **no branch or tag references it** (verified:
  `git branch -a --contains b2366a62a` → empty), and `master` (`60cf0a4d5`) still
  carries the pre-`5c5cdb631` form of the function. The fix was therefore redone
  from this baseline, per the briefing's own fallback.

## Reconciliation — this document re-applied onto master (rework run 01a134b9)

This file arrived on master by a **rework** of branch
`task/01a1343dd9c2793ca5e229538e340c09` (tip `ba1c7682`), which no merge path could
land: the real `git merge --no-commit --no-ff` probe reported two UU conflicts and
the ref was deleted. Re-measurement on the delivered tree (commit `951ec6f8c`,
worktree `/home/user/workspace/gopi-01a134b956ce70ff86a1a10557ca5497`) corrects the
two stale claims above — **by measurement, not by deletion**.

**1. "`b2366a62a` could not be merged … `master` (`60cf0a4d5`) still carries the
pre-`5c5cdb631` form" — STALE.** Master has since landed exactly that work:
`e9643ec0a  Merge branch 'task/01a133e324' (tip b2366a62) — file-watcher exit channel,
re-landed`, whose file is `cmd/agent/worker_file_watcher_lifecycle_test.go`. Master
at the time of this rework is `e9643ec0a`. Measured:
`git log --oneline c4f8784..master` → `e9643ec0a`, `b2366a62a`.

**2. The fix's own revision (`2b102c979` + `ee463c24`) is not what this branch
delivers — master's later revision is a strict SUPERSET of it.** Master's
`ba7592966` ("both stops JOIN — the watcher's uninstall and the resident sweep's
flight") fixed the same defect and carries the branch's whole contract
(`exited`/`done`, `sync.Once` around the close, the join OUTSIDE it, a named
uninstall) plus things the branch's revision does not have (the named
`noopWorkerFileWatcherStop`, `TestWorkerLoopLeavesNoWatcherPollBehind` driving the
real loop, `TestFileWatcherIsResidentUntilStopped` pinning the resident semantics,
and the sibling `scratchGCTick` flight join). Measured per-file line-set diff of
the two revisions: `cli_worker_control.go` has exactly **4** branch-unique
stripped lines, and they are precisely the superseded probe shape
(`stopped := make(chan struct{})`, `case <-stopped:`, `return func() {}`,
`once.Do(func() { close(stopped) })`); the watcher test has **39** branch-unique
lines and **all 39 are comments**, zero code. So this rework resolves both
conflicts by ADOPTION and reverts nothing: the two files are byte-identical to
master (`git diff --numstat master..HEAD -- <either file>` is empty, and
`git hash-object <path>` equals `git rev-parse master:<path>` —
`9ba840f52b75f9a3caa29b357f6d4df75f3024ef` and
`6ff77893c7bc237918069bd379562810dffea48d`). What this branch **adds** to master
is this document, its `devdoc/DOC-INDEX.tsv` row, and the originating run's report
`tasks/01a1343d-d9c2-793c-a5e2-29538e340c09-report.md`.

**3. The guard's numbers are re-measured, not restated.** `fileWatcherJoinProbeCycles = 12`
and the `runtime.GOMAXPROCS(1)` lever are master's (`ba7592966`) values and hold on
the delivered tree; the regression test runs green there (evidence:
`~/.gopi/sessions/01a134b9-56ce-70ff-86a1-a10557ca5497/evidence/regression-watcher-join.txt`).

**4. Bounded claim.** The red proof in `## Verification` above measured the
branch's own revision against its own baseline (`HEAD~1` at `b888461f2`); it was
**not** re-run by this rework, because the revision it proves is not the revision
that landed (master's `ba7592966` is). This rework's own rule-31 surface is the
executed regression test on the merged tree, reported in the rework run's report.
