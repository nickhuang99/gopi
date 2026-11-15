<!-- doc-class: normative -->
<!-- STATUS 2026-09-19 (epic merger-delete-dead-impl): THE GUARD TESTS NAMED
BELOW ARE GONE — cmd/agent/merger_takeover_lock.go and the whole merger take-over
launcher were deleted with the retired resident daemon, so
`mergerTakeoverLockAdoptAt`, `detachInheritedFDWrapper` and the four exit sites
that applied it no longer exist. The MECHANISM this document found is general and
outlives its subject: `os.NewFile` installs `runtime.SetFinalizer(f,
(*file).close)`, so a wrapper around a descriptor this process did not open is
closed by the finalizer and the kernel recycles the number onto an unrelated live
handle (fd 7 = a TempDir directory; strace: the close came from
runtime.runfinq). SEVEN os.NewFile sites survive the deletion —
cmd/agent/cli_worker_daemon.go:564,699 · cmd/agent/cli_daemon_spawn.go:627,667 ·
internal/daemonlifetime/daemonlifetime.go:169 · internal/testutil/lifetimeprobe.go:122
— each still able to hit it if the wrapper is dropped instead of retained or
detached. The registry row was retired with its test (see testdata/flakes.tsv,
where the mechanism is carried forward) and the surviving risk is filed as a
Rule-26 dispatch. -->

# A borrowed fd closed by a GC finalizer — the `cmd/agent` EBADF flake

Root cause, the two shapes it takes, and the repair. Pinned 2026-09-19 on master
`a9a96cd7`; task `01a0b5cd`.

## Symptom

`cmd/agent` failed intermittently, only under a combined `-run` set:

```
CGO_ENABLED=0 go test ./cmd/agent/ \
  -run 'TestMergerRetired|TestMergerTakeover|TestMergerWorkLock' -count=1
```

two failure sites, ~1 run in 4 (measured 1/16 on a quiet host, then 2/4 with a
pre-compiled binary):

```
--- FAIL: TestMergerTakeoverEscapeDegradesLoudly (0.00s)
    testing.go:1267: TempDir RemoveAll cleanup: readdirent 001: bad file descriptor
--- FAIL: TestMergerTakeoverEscapeSelectsRungOne (0.42s)
    testing.go:1267: TempDir RemoveAll cleanup: unlinkat …: bad file descriptor
```

Single-test and family-only runs were green, so the trigger is **cross-test**.

## The mechanism

`main()` adopts the single-instance lock hand-off before argument parsing
(`cmd/agent/merger_takeover_lock.go`, DELETED — see the STATUS banner above). `mergerTakeoverLockAdoptAt` wraps
`mergerLockInheritedFD` in `os.NewFile` and REFUSES it when it is not a regular
file — the ordinary outcome on a normal start, and the line the failing runs
printed:

```
gopi-merger-takeover: the inherited single-instance lock could NOT be adopted:
  fd 7 is not a regular file (drwxrwxr-x) — this take-over runs UNGUARDED
```

`os.NewFile` installs `runtime.SetFinalizer(f.file, (*file).close)`
(`GOROOT/src/os/file_unix.go:243`). Dropping the refused wrapper therefore armed
a **close of somebody else's descriptor**, executed later by `runfinq`:

```
> runtime.runfinq+0x3d1
> os.(*file).close+0x85
> internal/poll.(*FD).close+0x45
> syscall.Close+0x25
close(7) = -1 EBADF (Bad file descriptor)
```

The close is only *damaging* once the kernel recycles the number — the lowest
free fd is handed out again, and the stale close lands on the new owner:

```
3220961 close(7<pipe:[112765507]>) = 0        ← the pipe, by its real owner
3220961 close(7)                   = -1 EBADF ← the stale finalizer, same number
```

That recycling requirement is why the flake needed a long-lived process with
other tests opening descriptors (the combined `-run` set), and why one test alone
was always green.

## Two shapes, one mechanism

① **The refused probe.** As above: the probe borrows `mergerLockInheritedFD`,
refuses it, drops the wrapper, and the finalizer closes whatever live handle owns
that number. In the measured run `fd 7` was a `TempDir` recursion's open
directory (`drwxrwxr-x`), which is exactly the `readdirent`/`unlinkat` victim.

② **The adopted handle (production).** `_, _ = mergerTakeoverLockAdopt()`
discards the returned handle on purpose — the design comment says "the
descriptor's mere existence is the guard". True, but that discarded `*os.File`
was the **only owner** of the descriptor: once collected, its finalizer closed
the singleton lock the take-over is meant to hold for the whole merge round. A
real take-over could silently lose its guard mid-round, and the freed number
would be recycled onto an unrelated handle.

## The repair

`detachInheritedFDWrapper` (`cmd/agent/merger_takeover_lock.go`):

```go
func detachInheritedFDWrapper(f *os.File) {
	runtime.SetFinalizer(f, nil)
	borrowedFDWrappers = append(borrowedFDWrappers, f)
}
```

`SetFinalizer(f, nil)` is the whole fix — the descriptor keeps its real owner.
Retention is required because a finalizer runs only on an **unreachable** object,
so a local going out of scope would re-arm the leak; the package-level slice is
bounded (one refusal on an ordinary start, plus the adopted handle on a
take-over).

Applied at **all four** exits of `mergerTakeoverLockAdoptAt`: the stat-error and
non-regular refusals, the failed-fence abort, and **the adopted path itself**
(shape ②).

On the failed-fence path the previously-armed `FD_CLOEXEC` is also cleared with
`fcntl(F_SETFD, 0)` before the detach: the code is aborting the adoption, so it
must not leave a fence behind on a descriptor it is about to stop owning, even
though that idempotent consequence is unobservable in-process.

## Guards (both red before, green after)

- `TestMergerTakeoverRefusedProbeLeavesTheFDAlive` — shape ①. Refuses a directory
  through the real code path, drops the wrapper, settles finalizers, then asks the
  **kernel** (`F_GETFD`) whether the descriptor is still open. The fixture uses a
  raw `syscall.Dup` rather than the package's `mtlDup`, because `mtlDup` retains
  an `*os.File` for the number and a retained wrapper would keep it alive by
  itself — hiding the leak under test.
- `TestMergerTakeoverAdoptedHandleSurvivesCollection` — shape ②, the production
  shape: adopt, discard the handle exactly as `main()` does, settle finalizers,
  assert the lock fd is still open.
- Cross-test reproduction (what makes shape ① observable at all in one process):

```bash
go test ./cmd/agent/ -run \
  'TestMergerTakeoverLockAdoptArmsTheFence|TestMergerTakeoverRefusedProbeLeavesTheFDAlive' -count=1
```

Measured: pristine production → the pair FAILS; with the detach → PASSES.

## Why the assertion is a syscall, not a bool

"The wrapper was released cleanly" is a claim about intent, and the unfixed code
also *intended* to stop using the wrapper — it passed on such an assertion. The
kernel's answer to `F_GETFD` (EBADF vs 0) is the observation, which is why both
tests read the descriptor's state instead of inspecting the code's bookkeeping.

## Instrument notes (reusable)

- `strace -f -z -k -e trace=close -o log ./pkg.test -test.run '…'` — `-k` gives
  the Go stack per syscall, which is how `runfinq` was identified as the closer;
  `-y` resolves fds to paths/pipes, which is how the recycling was seen.
- Timing the close against the collector: an `F_GETFD` immediately before and
  after a single `runtime.GC()` showed `0` → `EBADF`, proving the finalizer ran
  synchronously to that collection (no syscall in the test binary closed it).
- **No load generation was used** (PROJECT.md Rule 27): the whole reproduction is
  repetition of the test command plus syscall tracing.
