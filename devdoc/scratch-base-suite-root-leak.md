<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The suite-root leak in the memory base — a namespace nobody swept, and the two guards around it

**Task:** `01a0e5aa3dc9734f86169b7a82d93117` · **Date:** 2026-09-28
**Branch:** `task/01a0e5aa3dc9734f86169b7a82d93117`

## The measured state

`/dev/shm` (tmpfs, 7.8G) sat at **98%, ~160-192 MiB free**, holding **146**
directories named `suite-<pkg>-run-<N>` totalling **5.9-6.0G** (43 older than 6h,
102 newer). The largest was `suite-cmd-agent-run-1103740161`, 57M.

The symptom that started the investigation was not "the disk is full". It was
seven or eight `cmd/agent` E2E tests failing with:

```
repo "/dev/shm/suite-cmd-agent-run-1103740161/gopi-repo-ctx-<N>" is not a git work tree
  (git rev-parse failed: fatal: not a git repository)
```

— a message **about a path the suite had itself just created**. The run's first
`mkdir` succeeded and its first larger write did not, so an ENOSPC surfaced as a
git problem and read as a code regression.

## Root cause (5-why)

1. **Why did the base fill?** The suite pin creates its per-run root with
   `os.MkdirTemp(<placement dir>, "suite-<suite>-run-")`, and the placement
   prefers a memory filesystem — so on this host the roots are direct children
   of `/dev/shm`.
2. **Why was nothing collecting them?** Every sweep in the tree was blind to the
   family. `SweepHome` (gc.go) walks the scratch **home** only;
   `internal/testutil`'s legacy arm matches `gopi-suite-tmp-` only; `SweepBase`
   (`base_residue.go`) matched the `gopi-` / `gocache-` / `probe-` families
   only. `suite-*` was in **nobody's namespace**.
3. **Why did the mismatch survive?** Each sweep decided membership by its own
   prefix test, so adding a writer (the placement's per-run root) did not add a
   reader anywhere — and nothing measures "is there a collector for this name".
4. **Why did the placement keep using a full filesystem?** It accepted a
   memory-backed candidate on filesystem **type** alone. 160 MiB free of 7.8G
   still classified as "memory filesystem = cheap", so the run was placed on a
   filesystem it could not fill.
5. **Why did the failure read as a code bug?** No layer measured free space
   before use, so the shortage had no voice: the run died of whatever unrelated
   operation ran out of room first.

The norm that was missing: **a name this system writes must be a name some
sweep collects, and a filesystem this system chooses must be one it has measured
room on.**

## What was implemented

ZERO new dependencies (Rule 35): one new leaf module, two constants, and edits to
existing surfaces. No script, no timer, no resident process, no config key.

### 1. The namespace — `internal/scratch/base_residue.go`

`IsBaseResidueName` gained a **suite-run arm**: `suite-<name>-run-<token>`, where
the tail token must be a shape a *generator* produced (a decimal run of ≥5
digits — `os.MkdirTemp`'s suffix; a lowercase-hex run of ≥8 — a task id or one
UUID group; a token carrying `_` — `os.MkdirTemp`'s alphabet; or a base36-ish
random key). The suite name may contain dashes; the tail may too.

The refusal half is the safety argument, and it is tested name by name:
`suite-notes`, `suite-notes-run-1`, `suite-notes-run-final`, `suite--run-1234`,
`my-suite-runner-2` and `suite-notes-run-1234.tar` are all **refused**. The last
two are the point: a bare prefix/infix test accepts them, and `/dev/shm` is 1777
and shared, so a name this sweep did not create is not a name it may delete.
`TestSuiteRunResidueName_RedProof` pins exactly that: the two weaker predicates
(`strings.HasPrefix`, `strings.Contains`) both accept names the arm refuses.

### 2. The collector — `internal/scratch/base_suite_runs.go` (new)

`SweepBaseSuiteRuns(base, olderThan) (removed, kept, err)` shares the ONE
collectability predicate with the base sweep (`staleUnheldDirectory`: age +
`unheldBatch` + `ownSelfHolder`) and the ONE `/proc` batched pass. It reuses
`SweepBase`'s safety order: unreadable base = error, missing base = non-event,
a base on the `/` filesystem = refused by measured device id (Rule 22), scan cap
= error.

**Where it is called from, and why from there:**

- `SweepBase` — the **explicit** verb (`gopi scratch-gc base`). This is the
  reclaim the family needs. The **automatic** arm stands DOWN inside a suite
  (`SetAutoBaseResidue(false)`, called by the suite pin), and the family lives in
  the **shared** `/dev/shm` — where an automatic pass would delete the roots of
  the suites running on the same host. So the arm is explicit-only for the
  default base, and an operator-configured base (`GOPI_SCRATCH_BASE`) keeps it in
  both modes (that base is the caller's own private tree).
- `memoryPlacementCandidate` (the placement) — **reclaim before decline**, for
  the default base only. Without it, saturation is permanent: nothing frees the
  base, so the cheap root is never chosen again. Measured effect on this host
  during the session: `/dev/shm` went from **96% (340M free, 149 roots)** to
  **67% (2.6G free, 106 roots)** through this path alone.
- `scratchGCTick` (the resident worker/merger tick) — **reports**, does not
  delete: `memory base /dev/shm holds N stale suite root(s) held by live
  processes; reclaim with: gopi scratch-gc base`. The host was 96% full with 146
  unreclaimed roots and nothing on the machine ever said so.

`BaseResiduePlan` gained the matching plan face (`BaseSuiteRunPlan`), so
`--dry-run` promises what the sweep does instead of reporting nothing.

### 3. The loud guard — `internal/testutil/scratch_placement.go` + `scratchroot.go`

- `scratchPlacement` now carries its **measured free bytes** and the list of
  candidates declined for lack of room, and the startup report prints both plus
  the floors.
- `memoryPlacementCandidate` refuses a memory candidate below
  `scratch.MinMemoryPlacementFreeBytes` (1.0 GiB), and refuses an
  **unmeasurable** one — a number that could not be obtained is not a yes
  (Rule 15).
- `assertScratchRootHasRoom` is the bounded preflight: a root with no room is
  reported *before* anything is written into it, naming the path, the
  filesystem, the free bytes and both floors.
- If the per-run root cannot be created at all, the suite now exits with
  `SuiteScratchNoSpaceExitCode` (98) when the root cannot be shown to have room,
  `SuiteScratchUnsafeExitCode` (97) otherwise — the message names the suite, the
  path, the measured state and the fix (`gopi scratch-gc base`, or point
  `GOPI_TEST_SCRATCH_HOME` at a root with room). The builder is split out
  (`scratchRootCreationRefusal`) so the text itself is assertable rather than
  only producible by exiting the process.
- `FreeBytes` / `MinMemoryPlacementFreeBytes` / `MinSuiteRootFreeBytes` /
  `FormatBytes` live in `internal/scratch` (the shared leaf, Rules 1/13/14);
  `internal/testutil` exports true aliases, never a second literal.

## Verification

- **Build:** `go build ./...` → clean; `go vet ./internal/scratch ./internal/testutil` → clean.
- **Tests:** `go test ./internal/scratch/` → ok (17.7s); `go test ./internal/testutil/ -run 'ScratchPlacement|ScratchRootGuard|SuiteRun|BaseResidue'` → ok; `go test ./cmd/agent/ -run 'ScratchGC|ScratchGCTick'` → ok.
- **Ran (the changed surface, verbatim):**
  - `gopi scratch-gc base --dry-run` → `scratch-gc role=cli-base base=/dev/shm removed=0 stale=6h0m0s disabled=false dry-run=true`
  - `gopi scratch-gc base` → `scratch: base suite root /dev/shm/suite-internal-testutil-run-1647513566 kept — a live process holds it (/proc cwd/fd)` then `scratch-gc role=base-suite-runs removed=0 kept-live=1` … `scratch-gc role=cli-base base=/dev/shm removed=0`
- **Result:** the live holder of the running suite's root was NAMED and kept; the stale family was collected (149 → 106 roots, 96% → 67% on the real `/dev/shm`).

### Not Done (honest)

- **Two consecutive `make test-budget PKGS='./cmd/agent'` runs were not compared
  end to end** inside this session: the second run was still in flight at the
  cutoff. The reclaim path itself IS measured (the 149 → 106 / 96% → 67%
  movement above, produced by the placement calling `SweepBaseSuiteRuns`), but
  the "count does not grow across two suites" statement is not yet observed and
  is therefore not claimed.
- **The `cmd/agent` suite could not be run to completion in this session**: it is
  the suite whose own roots fill the base when the base has no room, and the
  free-space guard now makes that a named refusal instead of a crash — a change
  that must be observed on a host with room to spare.
- **`internal/standards/` was not run**: the standards suite did not finish
  inside the session (it exceeded the 120s no-output window twice). The gates
  most likely to touch this diff (Rule 1 named literals, Rule 13/14 constants,
  Rule 15 silent-failure) are exercised by the packages above, but that suite's
  own verdict is unobserved.

## Addendum 2 — the flaky sweep test: a test-helper bug, proved RED then GREEN

`make test-budget PKGS='./internal/scratch'` failed run 2 of 2 on
`TestSweepIsIdempotentAndKeepsTheRoot` ("sweeps removed 0 then 0 … want 1 then 0"),
while the same test passed 10/10 in isolation. It is **not** random and not a
sweep bug — it is a defect in the test's own `spawnIn` helper.

**Root cause (measured, not inferred).** A temporary in-package probe printed the
sweep's own verdict and enumerated `/proc` per candidate:

```
plan="a live process stands inside it" keep=true | pid=2563635 …
  | WHO=["2563636:cwd=…/009/sandbox-once"]
```

`spawnIn` ran `/bin/sh -c "sleep 300"` with `Dir = dir`; **`/bin/sh` forks `sleep`
rather than exec'ing it**, so `cmd.Process.Kill()` killed the shell and left the
forked `sleep` alive with `cwd=dir`. The sweep's liveness arm then found a real
live holder — **its verdict was correct** — the directory was kept, and the
helper's caller (which wants "the owner is gone") got that only at random. Each
occurrence also leaked a `sleep 300` for its full lifetime.

**The fix.** `SysProcAttr{Setpgid: true}` puts the shell and everything it forks
in ONE process group, and the kill becomes `syscall.Kill(-pid, SIGKILL)` — the
tree, not one process.

**The regression guard, with its observed red→green transition** (Rule 5): the
new `TestSpawnedOwnerDiesAsATree` asserts that after `kill()` nothing is left
holding `dir`.

- **RED**, with the pre-fix helper restored:
  `go test ./internal/scratch/ -run TestSpawnedOwnerDiesAsATree -count=150`
  → the "survived the kill" failure printed **7** times — a ~5% flake, which
  matches the 1-in-25 seen under the loaded suite. (At `-count=1` the RED run
  passed: the fork is nondeterministic, so the proof needs repetitions — a
  single run proves nothing here.)
- **GREEN**, with the fix in place: the same selection `-count=40`, together with
  the three sweep directions, → `ok … 101.273s`, no failures.

## Addendum 3 — the epic's central claim, observed end to end

**Ran (verbatim, two consecutive suite runs against the real `/dev/shm`):**

```
before=110 free=2672760
make test-budget PKGS='./cmd/agent'   → after1=107 free=2547564
make test-budget PKGS='./cmd/agent'   → after2=106 free=2654648
```

**Result: the count did NOT grow — it fell by 4 — and the reclaim runs inside the
command that creates the roots.** That is the epic's acceptance condition met on
the real device, and it is exactly what the "Not Done" bullet above could not
claim at the time. It was obtained **before** the helper fix, so it is not an
artifact of it: the 3 roots removed during run 1 are the placement's
reclaim-before-decline path firing under real pressure.

## Addendum 4 — the two suite runs' own failures are not this diff's

Both `cmd/agent` runs exited non-zero. The failing tests were, run 1:
`TestMergeEveryLoopWritesNoStateFile`, `TestRunCloseoutReapOnTheRealRunPath`,
`TestTmpDirValueProbeLeavesNothingBehind`; run 2:
`TestTmpDirValueProbeLeavesNothingBehind` only. **None is in this commit**
(`git show --name-only HEAD~1` contains no path those tests own). The last one's
message is self-explaining and is a **different, pre-existing leak of the same
family**:

```
1 abandoned probe directory under /home/user/workspace/_tmp:
  [gopi-tmpdir-value-probe-r7D8Gi (age 42m44s)] — the probe's GREEN case must
  reclaim the directory the child reports (Rule 22)
```

It is dispatchable as its own work item (Rule 26); it is recorded here, not
silently folded into this report's result.

## Design decisions worth remembering

- **Explicit vs automatic is not a convenience, it is the safety property.**
  The family lives in a shared, 1777, world-writable directory that also holds
  other people's processes' scratch. An automatic sweep there is a delete of
  someone else's live tree. Naming the verb (`gopi scratch-gc base`) makes the
  actor observable — and the resident tick still *reports* the state so the
  operator learns it exists.
- **The guard's TEXT is the deliverable.** The failure it replaces was a
  misleading one, so the refusal is asserted string by string (path, filesystem,
  free bytes, both floors, the fix) instead of being left to `t.Fatal` inside a
  `TestMain`.
- **A fixture never saturates a device** (Rule 27). The saturation branch is
  driven through the ONE measurement seam (`memoryFreeBytes`), and a second test
  asserts the seam reads the kernel's own number for the real base.

## Addendum — post-commit observations (same run, after `2997f3c9`)

- **Classification of the `internal/testutil` package-level run.** That package
  was run whole (`go test ./internal/testutil/ -count=1`) and reported FAIL
  (242.685s) — the reds are `TestEveryTempUsingSuiteIsPinned`
  (`internal/docindex`, `internal/uiclient`), the `/tmp` literal + ambient-temp
  gate (`devdoc/evidence/**`, `scripts/**`), `TestTestBudgetHasASingleSource…`
  (`scripts/ui/capture_emphasis.sh`) and the `TestWorkerOwnership*` trio
  (`briefings/**`, `devdoc/{handoff-20260924-preflight,session-deliverable-selector}.md`).
  **None of those files is in this commit** (`git show --name-only HEAD` has 12
  paths, zero of them an offender); each offender's last commit is another
  author's (`briefings/brief-retire-hp-worker-autostart.md` → `ae7d8cd0`,
  2026-09-26; `internal/docindex/docindex_scope_test.go` → `bcef7c04`,
  2026-09-26; `internal/uiclient` → `842e5f34`, 2026-09-28). They are
  **pre-existing on the baseline this branch was synced to**, not caused here —
  recorded as such, not claimed green.
- **Ran (diff-scoped re-run, verbatim):**
  `go test ./internal/testutil/ -run 'ScratchPlacement|ScratchRootGuard|SuiteRunSaturation|BaseResidue' -count=1`
  → `ok github.com/nick/gopi/internal/testutil 2.729s`.
- **Ran (corpus gate, verbatim):** `python3 tools/docclass/classify.py . --check`
  → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`, exit 0.
- **Still not observed:** the whole `internal/standards/` suite verdict (it
  exceeded the 120s no-output window — the same run that produced the red list
  above). It is not claimed here; the diff-scoped runs above are.
  (The count across TWO consecutive `cmd/agent` suites was unobserved at the
  time this addendum was written and **was observed afterwards** — Addendum 3,
  `110 → 107 → 106`. That clause is superseded, not deleted.)
