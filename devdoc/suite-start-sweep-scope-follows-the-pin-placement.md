<!-- doc-class: historical -->
# The suite-start sweep's scope and the pin's landing site are ONE resolution

**Task:** `01a12d34-fe84-7931-a9f9-b5113d863827` · **Branch:** `task/01a12d34fe847931a9f9b5113d863827`

## The defect (measured on this host, 2026-10-12)

`SuiteOrphanScopePrefix(suite)` built the backstop's aim from the scratch home:

```go
filepath.Join(ScratchHomeDir(), scratchRunDirName(suite))   // …/gopi-test-scratch/suite-<suite>-run-
```

`PinSuiteTempRoot` creates the run root under whichever parent
`scratchPlacementFor` chose (`scratchroot.go`, `os.MkdirTemp(home, pattern)`), and
under `placementMemoryBase` that parent is the host **memory base**:

```
testutil: suite "internal-testutil": scratch root /dev/shm is tmpfs (memory filesystem)
          (placement memory-base, 25.3 GiB free)
suite root   = /dev/shm/suite-internal-testutil-run-3747346477
scope prefix = /home/user/workspace/gopi-test-scratch/suite-internal-testutil-run-
in scope?    = false                                        ← the whole defect, one line
```

So the sweep's aim and the pin's landing site were **two expressions of one fact**
that had drifted apart. Measured before the fix:

```
device /dev/shm roots (all suites) : 172     (~3.7G of the tmpfs)
  of THIS suite                    :   7
home tree roots                    :   1     ← the only tree the sweep could see
```

Everything a **killed** run of this suite left in the memory base was invisible to
the backstop that exists to reap exactly that — its detached daemons no less than
a test's own stand-in. (The sibling defect — a *scene* helper building its tree in
the scratch queue — was fixed separately in `ebec3413b`.)

## The fix

One resolver, `SuiteOrphanScopeRoots(suite)` (`internal/testutil/orphansweep.go`),
returns the **set** of prefixes a run root of this suite can be created under, and
`SuiteOrphanScopePrefix` is defined as its first member (`roots[0]`), so the two
cannot differ again.

Membership (nothing invented, nothing widened):

| member | why it is in |
|---|---|
| `<ScratchHomeDir()>/suite-<suite>-run-` | the pin's default landing site |
| `ScratchPlacementDir()/suite-<suite>-run-` | the pin's OWN recorded landing site (set by `PinSuiteTempRoot`); empty before the pin runs |
| `dir(SuiteTempRoot())/suite-<suite>-run-` | the parent the pin demonstrably used — covers a caller-pinned `TMPDIR` and a memory-backed scratch home with no second channel read |
| `MemoryBase()/suite-<suite>-run-`, only when the base is memory-backed | the arm that answers for a **past** run whose placement differs from this one (the pin has no memory of them) |
| caller-pinned home arm: `<home>/suite-<suite>-run-` **+** `<home>/fixture-suite-<suite>-run-`, and **nothing else** | a caller who pinned the scratch home owns the scope; the shared memory base is deliberately NOT added (the same stand taken by `LegacyScratchBases`), and both shapes the pin produces under that home are covered |

It deliberately does **not** call `scratchPlacementFor`: that function's saturation
arm *reclaims* directories (`reclaimSaturatedCandidate` → `SweepBaseSuiteRuns`), and
a scope resolver may never delete anything.

`SweepOrphansAtSuiteStart` now sweeps the whole set:

```go
roots := SuiteOrphanScopeRoots(suite)
where = strings.Join(roots, scopeRootSeparator)
swept, err = sweepScopeRoots(roots)
```

`FindOrphansInScopeRoots` scans each member through the **same** `scopeOfPrefix`
predicate as before (a path/component comparison — no second, looser spelling of
"inside") and **de-duplicates by identity**: `sweptKey{pid, StartTime}`, because a
bare pid is not an identity (the kernel recycles it) and two members can match one
process — which must be signalled **once**. A member whose scan failed is reported
on stderr and the rest continue; only when **every** member failed is an error
returned, so "the tree is clean" can never be the answer to "nothing was scanned".

What is deliberately NOT changed: `orphanCandidate` still requires the orphan shape
(`Detached()` + `Orphan()`) **and** an ownership proof (`cwd` / `GOPI_HOME` / open
fds) inside a member. A sibling suite, the scratch queue, another program's
directory in the same base, and a live keeper all stay untouched — the widening is
in *where the scope is aimed*, never in *what counts as ours*.

## Why not the alternatives

- **Widen to the whole memory base** — kills the ownership argument: the base holds
  another suite's live run roots and other programs' directories.
- **Scope to `SuiteTempRoot()` alone** — that is *this* run's random root, so a
  previous, killed run's root is outside it by construction (the same reason the
  prefix scope exists at all).
- **Sweep nothing automatically and leave it to `gopi tool scratch-gc base`** — the
  operator's one-shot already exists, but the backstop's whole point is that a
  killed suite's leak is reaped by the *next* start of that suite; leaving it to a
  manual command is what let 172 roots accumulate.
- **Rate-limit on the tmpfs occupancy** (skip the scan when the base is comfortable)
  — a threshold that hides the roots it merely *thinks* are cheap, and the cost it
  saves is one `/proc` pass, not the sweep (measured: the sweep is `~0.2s`).

## Cost

The backstop runs once per suite, before `m.Run`, and the added work is one extra
`/proc` pass per member. Measured on this host: a suite start with the full
4-member scope, from process start to `ok`, is **2.7s** (whole-suite runs with the
added member show no regression against the pre-fix baseline).

## Verification

Red → green, plus the real-host reclaim: see `## Evidence` in the task report and
`~/.gopi/sessions/01a12d34-fe84-7931-a9f9-b5113d863827/artifacts/`.
