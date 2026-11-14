<!-- doc-class: historical -->
# The orphan-scene root must live inside the sweep's own scope

**Task:** `01a12ced-0186-7053-a785-aba644f654c8` · **Branch:** `task/01a12ced01867053a785aba644f654c8`
**Subject:** `internal/testutil/orphansweep_test.go` — `sceneDir()`

## 1. The defect

`internal/testutil/orphansweep.go` carries the suite-start backstop: `ArmAndSweepForSuite`
→ `SweepOrphansAtSuiteStart(suite)`, called from a suite's `TestMain` before `m.Run`. It
reaps daemons an earlier run leaked (a detached, orphaned process whose only tie to the
harness is a directory it owns).

When `GOPI_TEST_ORPHAN_SWEEP_ROOT` is unset it scopes itself by

```go
where = SuiteOrphanScopePrefix(suite)          // filepath.Join(ScratchHomeDir(), scratchRunDirName(suite))
swept, err = sweepInScope(scopeOfPrefix(where)) // → "<scratch-home>/suite-<suite>-run-"
```

`sceneDir()` — the helper that builds the tree a scene's stand-in (`setsid --fork sleep 600`)
works in — built its directory in a **sibling** tree:

```go
dir := filepath.Join(ScratchRootQueue(), name+"-"+itoa(os.Getpid()))  // "<scratch-home>/queue/…"
```

The two roots do not contain one another. So a stand-in left in a scene by a case that
failed before its own sweep — or by a binary SIGKILLed between the launch and the
`t.Cleanup` — is **invisible to the very backstop that exists to reap it**, and lives out
its whole hold: a detached process plus the tmpfs its sandbox occupies, for 600 s. That is
the leak the sweep was built for, manufactured by the sweep's own test.

## 2. Evidence — controlled, only the directory differing

The sweep's discovery is path-based (`ownershipProof`: cwd → `GOPI_HOME` → open fds), so a
stand-in under any directory can be planted and a REAL suite start (`go test
./internal/testutil`, i.e. `TestMain` → `ArmAndSweepForSuite`) can be run against it. Same
moment, same process shape, three roots:

```
# planted stand-ins (identity by /proc/<pid>/cwd, never pgrep — Rule 24)
  pid=1479378 cwd=<home>/workspace/gopi-test-scratch/queue/zz-scene-KEEP2
  pid=1479381 cwd=<home>/workspace/gopi-test-scratch/suite-internal-testutil-run-ZZ2
  pid=1479384 cwd=/dev/shm/suite-internal-testutil-run-ZZ2

# the REAL suite start sweep
ok  github.com/nick/gopi/internal/testutil  0.628s

# survivors
  pid=1479378 cwd=<home>/workspace/gopi-test-scratch/queue/zz-scene-KEEP2      ← SURVIVED (the queue)
  pid=1479384 cwd=/dev/shm/suite-internal-testutil-run-ZZ2                     ← SURVIVED (memory base)
```

Raw artifact: `~/.gopi/sessions/01a12ced-0186-7053-a785-aba644f654c8/artifacts/red-placement-scope-gap.txt`.

Two facts came out of it, and the second one is why the fix is *not* simply "put the scene
under the queue's sibling":

1. **The queue is outside the scope** — the defect as reported.
2. **The run root the pin actually uses is frequently NOT under the same tree as
   `SuiteOrphanScopePrefix`.** `PinSuiteTempRoot` prefers a MEMORY-BACKED placement
   (`scratch_placement.go`, `placementMemoryBase`): on this host it pinned
   `/dev/shm/suite-internal-testutil-run-<N>`, while `SuiteOrphanScopePrefix` resolves to
   `<ScratchHomeDir()>/suite-<suite>-run-`. So `sceneDir` must not hang off
   `SuiteTempRoot()` either: a scene placed in *this run's* pinned root is invisible to the
   *next* run's sweep twice over (different random root **and** a different filesystem), and
   under the memory-base placement (the normal placement upstream) that is the default
   outcome. Scoping strictly to the home tree's run roots would be blind in that arm; scoping
   strictly to the memory base would be blind in the device arm.

## 3. The fix

`sceneDir` now builds in **the sweep's own scope namespace**:

```go
scope := SuiteOrphanScopePrefix(internalTestutilSuite)   // <scratch-home>/suite-<suite>-run-
dir := filepath.Join(scope, name+"-"+itoa(os.Getpid()))
if !scopeOfPrefix(scope)(dir) { t.Fatalf(…) }             // the guard, using the sweep's OWN predicate
```

What makes this correct rather than merely convenient:

- **One scoping function, not two spellings.** The guard calls `scopeOfPrefix` — the
  function `SweepOrphansAtSuiteStart` itself uses — so a scene that falls outside the scope
  fails *in the case that would otherwise become the sweep's blind spot*, not in a later
  review. An edit back to `ScratchRootQueue()` or `SuiteTempRoot()` now fails here.
- **The scope arm that covers it is explicit.** `scopeOfPrefix` has two arms:
  `PathWithin(prefix, p)` (a real run root, `…/suite-x-run-1234`) **or**
  `strings.HasPrefix(p, prefix)` (a path in that namespace). The scene directory
  `<scope>/orphan-scene-<pid>` is covered **by the second arm**, and that is intended: the
  scope's declared semantics are "a run root of THIS suite" plus "the run-root namespace of
  this suite", and a scene is an artifact of this suite's run that must be collectable by
  this suite's backstop. The scene is therefore *owned* by the suite, and is named by pid so
  concurrent binaries of the same suite cannot collide.
- **The queue keeps its job.** `ScratchRootQueue()` remains the right home for artifacts
  that carry **no process identity** (`TestScratchDir` / `TestDeviceScratchDir`, large
  files). A scene's entire meaning is the process standing in it, which is exactly the thing
  that must be inside the reapable root.
- **Rule 22 holds.** `<scratch-home>` is the tree the suite's own block-device assertion has
  already judged; `os.TempDir()` and `/` are never used.

### Why not a correct-looking alternative: put the scene under this run's pinned root

Rejected in §2.2: it reads as "inside the suite's own tree", but the pin mints a fresh
random root per run, so a scene left by a killed run is invisible to the run that follows —
which is the *only* run that could ever reap it. The stable, suite-wide namespace is the
one the backstop searches.

### The negative half is preserved, and pinned

`TestOrphanSweepLeavesAForeignProcessAlone` already proves the sweep does not act outside
one root. The new regression adds the scope-level version of the same claim: the queue scene
is asserted **outside** `scopeOfPrefix(SuiteOrphanScopePrefix(suite))`, and a stand-in there
is asserted **alive and unreported** after the real suite-start sweep. `queueSceneDir()`
fails loudly if the queue ever came inside the scope, so the negative half cannot silently
stop proving anything.

## 4. Regression test

`TestSuiteStartSweepSeesItsOwnScopeScenes` (`orphansweep_test.go`) — minimal repro, driving
**the real entry point** `SweepOrphansAtSuiteStart(internalTestutilSuite)`, never a
re-creation of its scope:

1. `t.Setenv(OrphanSweepRootEnv, "")` (Rule 17 — the empty value *is* "no override").
2. two scenes: `sceneDir` (in scope) and `queueSceneDir` (outside), each with a real
   `setsid --fork sleep 600` stand-in in it.
3. (a) the placement claim through the sweep's OWN predicate; (b) the real sweep reaps the
   in-scope stand-in and leaves the queue stand-in alone and alive; (c) the queue stand-in is
   reclaimed through the sweep of its own root.

Observed (real run, kept as evidence):

```
testutil: internal-testutil: swept orphan daemon pid=1565867 starttime=312751390 cmdline="sleep 600"
  (cwd /home/user/workspace/gopi-test-scratch/suite-internal-testutil-run-/sweep-scope-scene-1565733) — terminated
--- PASS: TestSuiteStartSweepSeesItsOwnScopeScenes (0.09s)
```

## 5. Verification

```
go build ./...                                             : OK (no output)
go vet ./internal/testutil/                                : OK
go test ./internal/testutil/ -run 'Orphan|Scene|SuiteStartSweep' -count=1 -v  : ok (see evidence artifact)
```

**Two failures in this package are PRE-EXISTING and untouched by this change** — measured in
a pristine worktree at the master tip (`b8287d5c6`, branch `base/01a12ced-check`, no change
of mine), raw output in
`~/.gopi/sessions/01a12ced-0186-7053-a785-aba644f654c8/artifacts/preexisting-gate-failures.txt`:

- `TestPinCoverageTable` — `internal/smokegate` creates scratch (`t.TempDir()`) but no
  `TestMain` routes the pin through `internal/testutil`.
- `TestEveryTempUsingSuiteIsPinned` — the same finding from the static gate.

Both are the same single cause (`internal/smokegate` has no `TestMain`), neither is in this
change's blast radius, and neither is repaired here (Rule 26/41: filed as its own bug row,
not smuggled into a fix that would then be two changes).

## 6. Honest gaps

- The sweep's scope and the pin's placement remain two different trees when the placement is
  memory-backed (§2.2). This fix makes the *scene* land in the sweep's namespace; it does
  not unify the two namespaces, and a leaked scene under the memory-base arm of the
  placement is still out of the sweep's reach. That is a separate, larger question
  (`SuiteTempRoot` vs `SuiteOrphanScopePrefix` resolution) and is left as its own finding
  rather than widened into this change.
- **Division of labour (Rule 28):** this file is the system layer (`internal/testutil`
  gates/sweeps), which is peerbox's half. The row ran on this host in its own worktree; the
  change is deliberately minimal (one test file), touches no production code, and the fact
  is recorded here rather than hidden.
