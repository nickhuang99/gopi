<!-- doc-class: normative -->
# The durable-request watcher is uninstallable — a leak the suite's close-out sweep caught

**Status:** fixed on `task/01a133af49917a358b0b27c5ce9a8d28` (2026-10-13).
**Symptom:** `make test-budget PKGS='./cmd/agent'` ended `FAIL` with **zero failing
tests** — a package-level verdict.
**Cause:** `workerDaemonCtx.installFileWatcher` started a poll goroutine with no
exit path, and the package's close-out goroutine sweep counted it.

## The measurement

The acceptance run of the worker-help-surface epic (13:49:39 → 14:10:00, 1221s,
4,374 tests, `0` `Action=fail` events) closed like this:

```
suite goroutine sweep: parked repository goroutines: 6
  goroutine 34616 [chan receive]:
  github.com/the operator/gopi/cmd/agent.workerDaemonCtx.installFileWatcher.func1()
  	…/cmd/agent/cli_worker_control.go:2164 +0xc5
  created by …installFileWatcher in goroutine 34611
  	…/cmd/agent/cli_worker_control.go:2161 +0x99
…
FAIL	github.com/the operator/gopi/cmd/agent	1221.139s
```

Six blocks, three goroutine ids each (`34616/17426/30677/8178/8011/17440`), all the
same frame. **The tests all passed; the run failed anyway**, because
`testmain_test.go` turns the count into the exit code:

```go
if parked := reportSuiteGoroutinesAtCloseOut(); parked > 0 && code == 0 {
    code = 1
}
```

So "zero failing tests" and "outcome=fail" are consistent here, and the goroutine
sweep — not the four named help-surface tests — is what the acceptance criterion
was actually gated on.

## Root cause

`installFileWatcher` (cli_worker_control.go) started a goroutine whose loop had **no
way to end**:

```go
for range t.C {
    c.consumeDrainRequests()
}
```

Correct for production — the worker process `os.Exit`s when the loop returns, so the
kernel reclaims it — and wrong for a harness that drives the worker **in-process**.
Its own doc comment said as much ("The watcher stops with the process"), which is
exactly the property a test binary does not have.

`c0f29000f fix(worker): --stop/--restart/--status address ONE worker (--pid /
--branch)` (2026-10-13 09:11) widened **when** the watcher installs, from
`c.writer != nil` to `!c.consumesRequests()` (i.e. `c.dir != ""`):

```diff
 func (c workerDaemonCtx) installFileWatcher() {
-	if c.writer == nil {
+	if !c.consumesRequests() {
 		return
 	}
```

That is the epic's own point — a **foreground** worker is a stop target too, and the
durable file is its proof of address. Before it, the foreground face did not install
a watcher at all; after it, every in-process foreground run parks one, and the suite
runs the loop in-process twice.

## Minimal reproduction

The whole defect is two cases (each leaks one goroutine, and `2 != 6` because the
package's other in-process faces reach the same install):

```bash
GOPI_TEST_SCRATCH_HOME=<off-/ dir> go test ./cmd/agent/ -count=1 \
  -run 'TestRunTaskWorker_OnceSweepsZombieFirst|TestRunTaskWorker_SurvivesFailedRun'
```

Before the fix (red):

```
suite goroutine sweep: parked repository goroutines: 2
  …installFileWatcher.func1()
FAIL	github.com/the operator/gopi/cmd/agent	9.659s
```

Control, same binary, a set that does not drive the loop in-process (green):

```
GOPI_TEST_SCRATCH_HOME=<off-/ dir> go test ./cmd/agent/ -count=1 \
  -run 'TestManSourcesMatchTheirHelpPage|TestPublishedHelpPagesStayWithinTheirUsageBudget'
ok  	github.com/the operator/gopi/cmd/agent	7.611s
```

The control is the point: the leak is not "this machine" or "a loaded host", it is
**reaching the loop in-process**.

## The fix

`installFileWatcher` now returns its own uninstall — the shape the package already
uses for the signal handler (`installWorkerStopSignals`):

```go
func (c workerDaemonCtx) installFileWatcher() (uninstall func()) {
	if !c.consumesRequests() {
		return func() {}
	}
	stopped := make(chan struct{})
	go func() {
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
	return func() { close(stopped) }
}
```

Both faces defer it, beside the request cleanup they already deferred:

```go
defer workerCtx.cleanupRequests()
defer workerCtx.installFileWatcher()()
```

`runTaskWorkerExplicit` and `runTaskWorkerLoop` both keep the install exactly where
it was — the capability the epic added (foreground workers ARE addressable) is
unchanged; only the teardown was added.

## Why stop-before-start, and not `context.WithCancel`

A `ctx cancel` version (`installFileWatcher() (context.Context, func())`) was
rejected: it lengthens the call and invites a correct-but-incomplete install, while
`defer install()()` makes "the install and its stop" a single expression the reader
cannot separate. The cost is one trap worth naming for the next reader: **when the
deferred call is itself the value-producing call, a returned teardown must be
non-nil on EVERY path** — `defer f()()` panics at function exit if `f` yields nil.
The early return therefore returns `func() {}`, not `nil`.

## Regression guard

`cmd/agent/cli_worker_control_watcher_test.go` —
`TestInstallFileWatcherIsUninstallable`. It counts goroutines carrying the
`workerDaemonCtx.installFileWatcher` frame before the install, after it (must have
started), and after the uninstall (must be gone), with the poll's own tick
(`workerFileWatchInterval`, 2s) named as the reason it counts a goroutine rather
than waiting for a tick.

Red proof: replacing the returned teardown body with `return func() {}` makes it
fail (`the poll is still parked after its uninstall ran`), and the suite sweep
independently reports the same single parked goroutine. The production file was
restored and the restore verified by blob identity, not by an index-relative diff
(Rule 5):

```
test "$(git hash-object cmd/agent/cli_worker_control.go)" \
   = "$(git hash-object $SESSION/artifacts/cli_worker_control.go.fixed)"   # → RESTORE OK
```

## What this is not

It is not a fix to the four help-surface tests: those were already green on this
tree (see `devdoc/worker-help-surface-reds-already-fixed.md` — the derivation is
byte-identical and the budget is 136). This leak was a **different** reason the
acceptance command failed, found only because the acceptance run was actually
executed (Rule 31) rather than inferred from the four tests passing.
