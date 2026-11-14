<!-- doc-class: historical -->
# The `scratch-gc deps` origin guard: a plant that stays measurable under load

## What was wrong

`TestScratchGCDepsOriginReadsExeNotArgv0` (`cmd/agent/cli_scratch_gc_deps_test.go`)
plants a real process, gives it a Chromium-shaped `argv[0]` — ONE field holding
the whole command line, whose `filepath.Base` matches the gopi name regex — and
then asserts that `depsResidentOrigin` answers `foreign`. Under the pre-fix
classifier (argv[0]) the answer was `gopi`, which is the regression the guard
exists to catch. It was therefore a useful guard with a broken fixture:

```go
cmd := exec.Command(sleepBin, "30")
cmd.Args = []string{plantArgv0}   // argv[1] is gone
```

The kernel received the lying string as `argv[0]` and **nothing as `argv[1]`**,
so `sleep` had no duration to parse, printed its usage and exited immediately.
`depsResidentOrigin` then read `/proc/<pid>/exe` of a process that was over and
answered `unknown` — correctly, per Rule 15 — where the test demanded `foreign`.

### Root cause, measured

A probe (400 spawns per cell, `cmd.Args = []string{lie}`) settles the mechanism
exactly:

| read delay after `Start()` | readlink failed | state of the failure |
|---|---|---|
| 0 | 0/400 | the exec window had not opened yet |
| 50 µs | 397/400 | `state=Z` (exited, unreaped) |
| 2 ms | 400/400 | `state=Z` |
| 50 ms | 400/400 | `state=Z` |

So the child was **dead within ~50 µs**, and the only reason the case usually
passed was that the readlink happened to run inside the sub-50-µs window between
`fork`/`exec` and the child's exit. A loaded host loses that race — which is the
whole flake. It is not a `/proc` publication-order race (that class is about
records arriving out of order for a live child); here the process is simply
gone, and **`Signal(0)` cannot see it**: a zombie satisfies `Signal(0)`, and a
zombie is exactly what the failures were (`state=Z`). On this host, before the
fix: **21 of 300 runs red** at load ≈ 8, all of them `= "unknown", want
"foreign"`.

## The fix

1. **A real duration on `argv[1]`** (`cmd.Args = []string{plantArgv0, plantSleepSeconds}`):
   argv[0] is still the lie, and the program behind it is now alive for 30 s —
   liveness independent of the host's load. Nothing waits on that duration, so
   it cannot expire a loaded run.
2. **The plant is asserted to be measurable, and an unmeasurable plant FAILS
   LOUDLY** (never "unknown" as a verdict):
   - *structural*: the argv arity is `plantArgvLen` and `argv[1]` matches a
     duration the program accepts — decided without spawning anything, so the
     old broken shape is red in every run rather than probabilistically;
   - *measured*: `plantWaitExeReadable` polls until `Signal(0)` says the child
     runs **and** `/proc/<pid>/exe` reads back the executable we started, with
     the state field of `/proc/<pid>/stat` checked too, so `Z`/`X` is a fixture
     failure rather than a silent `unknown`;
   - *after the call*: `plantAssertAlive` re-checks the pid the classifier read.
3. **The assertion is unchanged**: `depsResidentOrigin(...) == foreign`. Nothing
   was relaxed to accept `unknown`; the failure modes moved into `Fatalf`s that
   name the fixture.

The argv handed to the classifier is now taken from `cmd.Args` (the kernel's own
argv) instead of a retyped constant, so the trap check cannot pass against a
string that no longer describes the plant.

## Why each piece could not be omitted (they were each tried)

| candidate | verdict |
|---|---|
| “just add `sleep "30"` as argv[1]” | fixes the process, but leaves the fixture's liveness **unasserted** — a future edit could remove it again and the case would go back to measuring corpses under load. Kept *and* asserted. |
| “assert liveness with `Signal(0)` only” | **insufficient, measured**: the failures were `state=Z` zombies, which `Signal(0)` reports as alive. This is why the state field is read. |
| “retry the assertion until it is not `unknown`” | a guarantee free of consequence — the guard would pass on a corpse by luck. Rejected. |
| “assert `unknown` is impossible and stop there” | tautology: it turns the case red on the old classifier too, but says nothing about the property. The argv[0]-vs-exe assertion stays the only assertion. |

## Evidence (this run)

- flake before: `go test -count=300 -run TestScratchGCDepsOriginReadsExeNotArgv0 ./cmd/agent/`
  → 21 failures, every one `= "unknown", want "foreign"`.
- flake after: same command, `-count=400` → 0 failures.
- red-proof (the guard's purpose): `depsResidentOrigin` reverted to
  `depsOriginFromProgram(filepath.Base(parts[0]), parts)` → the case fails with
  `= "gopi", want "foreign"`, i.e. **not** `unknown` — the plant stayed
  measurable and the classifier was caught.
- fixture sanity (the precondition can fire): plant variants
  `{lie}`, `{lie, ""}`, `{lie, "abc"}` each produce a `Fatalf` naming the
  fixture, so the plant can never quietly become unmeasurable.
