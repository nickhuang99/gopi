<!-- doc-class: normative -->
# Task Report: Post-merge rebuild — the dirty-tree guard must judge TRACKED changes only

**Epic**: post-merge rebuild's dirty-tree guard used an untracked-inclusive
predicate, so the Rule-12 close loop (merge → cleanup → REBUILD) was skipped
forever in a busy task field.
**Branch**: `feat/rebuild-guard-tracked-only`
**Date**: 2026-09-12

## Completed

### 1. The predicate split (`internal/tools/task_adoption.go`)

`gitStatusTrackedOnlyFlag = "--untracked-files=no"` — Rule 1b: the flag is a
meaningful literal, named once.

| helper | predicate | contract |
|---|---|---|
| `AdoptionGitStatus` | `git status --porcelain` | **UNCHANGED** — untracked-INCLUSIVE; the adoption chain's strict pristine-tree guard (probe merges / ③a commits must not entangle with untracked in-flight work) |
| `AdoptionGitStatusTracked` | `git status --porcelain --untracked-files=no` | **NEW** — tracked-ONLY; the rebuild guard |

`AdoptionGitStatus` was left byte-for-byte intact, so
`cmd/agent/cli_tasks_adopt.go:57` (`adoptionCleanTree`) keeps its semantics.
The two contracts are now explicit at both call sites instead of one helper
serving two different questions.

### 2. The rebuild guard switched (`cmd/agent/cli_tasks_rebuild.go`)

Guard → `tools.AdoptionGitStatusTracked`, with the file header's dirty-tree
paragraph rewritten: untracked files no longer defer; **tracked dirt still
does**. The guard is split, not deleted.

### 3. The second half — the build must not stamp itself `-dirty` (`internal/tools/rebuild.go`)

**This was not in the original epic text, and it is required for the fix to
work at all.** See "Root cause" below.

`runBuild` now runs the declared build command with
`GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=status.showUntrackedFiles
GIT_CONFIG_VALUE_0=no` (named consts `gitConfig*` / `gitConfigShowUntracked*`),
so the go toolchain's own VCS stamping counts tracked changes only. The env is
built by `buildVCSStatusEnv()`, which **drops any caller-supplied `GIT_CONFIG_*`
first** (a duplicate `GIT_CONFIG_COUNT` would leave which value wins to
getenv's first-match rule — Rule 15a).

## Root cause (why the epic’s stated premise needed correcting)

The epic states: *"buildVersion's `-dirty` suffix comes from tracked-file
changes."* **Measured false on this toolchain (go1.24.5).**

`cmd/go` stamps `vcs.modified` from its own `gitStatus`, which runs plain
`git status --porcelain` — `GOROOT/src/cmd/go/internal/vcs/vcs.go:334`.
The first `git status` in that function is tracked-aware, but the dirty flag
comes from the second one, which is not. Measured with a throwaway module:

| tree state | `vcs.modified` |
|---|---|
| clean | `false` |
| **one untracked file, zero tracked changes** | **`true`** |
| same untracked file, built under `status.showUntrackedFiles=no` | `false` |
| one tracked modification | `true` |

**Consequence if only the guard had been changed**: the guard would stop
deferring on an untracked-only tree, the build would run, produce a `-dirty`
binary, `versionMatches` would reject it (correctly — a `-dirty` bin does not
represent the committed tip), and the round would end in a **false
`build-failed` alarm + Mechanism-2 auto-revert of a perfectly good merge**.
That is strictly worse than the silent skip: it notifies and unwinds merges on
every pass.

Real-world confirmation in the affected field (`~/work/GoPi`):
tracked porcelain is **empty** (0 lines) yet `bin/gopi --version` reports
`49ff408258b6-dirty`, and there are 29 untracked entries. The `-dirty` suffix
is produced entirely by untracked files — the guard and the stamp disagree
about what "dirty" means, and both had to be aligned to *tracked*.

The two halves now answer the same question the same way: **untracked scratch
does not defer the rebuild, and does not stamp the binary.**

## Not Done

- No change to `RebuildAfterMerge`'s version-assertion logic: `-dirty` must
  keep being rejected. The fix is on the *producing* side, not by loosening
  the assertion.
- The epic's literal deliverable 1 example name (`tools.AdoptionGitStatusTracked`)
  and deliverable 2 were implemented as stated; deliverable 3's test list is
  implemented and extended with the two engine-level stamp tests the corrected
  root cause requires.
- No comdat/lint cleanup beyond the touched files.

## Verification

Build + tests (worktree `~/workspace/gopi-rebuildguard`):

```
go build ./...                                                  → OK (exit 0)
go test ./internal/tools/ -run 'TestAdoptionGitStatusTracked|TestBuildVCSStatusEnv|TestRunBuildStamps' -v
  TestAdoptionGitStatusTrackedIgnoresUntracked                   PASS
  TestAdoptionGitStatusTrackedReportsTrackedDirt                 PASS
  TestBuildVCSStatusEnvDropsCallerConfig                         PASS
  TestRunBuildStampsCleanUnderUntrackedOnly                      PASS
  TestRunBuildStampsDirtyUnderTrackedDirt                        PASS
go test ./cmd/agent/ -run 'TestServeRebuild|TestAdoptionCleanTreeStaysStrict' -v
  TestServeRebuildUntrackedOnlyRunsBuild                         PASS
  TestServeRebuildTrackedDirtStillSkips                          PASS
  TestServeRebuildTrackedAndUntrackedSkips                       PASS
  TestAdoptionCleanTreeStaysStrict                               PASS
  TestServeRebuildDirtyTreeSkips                                 PASS (fixture corrected)
  TestServeRebuildOKLogs / AutoRevertOnFailure / NoRevertWithoutSnapshot / NoContractSilentSkip  PASS
```

### Red-proof (the new tests fail on the pre-fix predicate)

- `TestServeRebuildUntrackedOnlyRunsBuild` dirties the tree with three
  **untracked** files and asserts the build RUNS. Under the pre-fix guard
  (`AdoptionGitStatus`) it would log `worktree dirty` and fail.
- `TestRunBuildStampsCleanUnderUntrackedOnly` builds a real Go module over an
  untracked-only tree and asserts `vcs.modified = false`. Without
  `buildVCSStatusEnv` it prints `true` — verified directly by the standalone
  probe (probe B, table above; the same probe with the env applied = probe C).
- One pre-existing test **had encoded the buggy premise**:
  `TestServeRebuildDirtyTreeSkips` created an *untracked* `dirty.txt` and
  expected a skip. It now dirties a **tracked** file (`f.txt`) and its comment
  records why — otherwise the suite would pin the bug.

### Red-proof A/B (both halves fail on the pre-fix code)

Captured by running the new tests with the fix temporarily reverted, then
restoring it:

```
A) guard reverted to tools.AdoptionGitStatus  (empty tracked diff, 3 untracked files)
   --- FAIL: TestServeRebuildUntrackedOnlyRunsBuild
       untracked-only tree must NOT defer the rebuild (pre-fix predicate):
       [merger] [rebuild] skip: worktree dirty — rebuild deferred to a clean tree (bin stays on the last clean master)

B) build env reverted to cmd.Env = os.Environ()
   --- FAIL: TestRebuildE2EUntrackedOnlyBinFollowsMasterAfterMerge
       untracked-only tree must NOT fail the rebuild
       (err="version mismatch: expected 86e15c7, binary reports \"86e15c7e1341-dirty\"")

both restored → PASS
```

Probe B is the finding that reshaped the fix: with the guard fixed but the
stamp unfixed, the round reaches the build, gets a `-dirty` binary, and lands
in `version mismatch` → false `build-failed` notify + Mechanism-2 auto-revert.

Real captured log lines (both from this session's runs):

```
[merger] [rebuild] skip: worktree dirty — rebuild deferred to a clean tree (bin stays on the last clean master)
[merger] [rebuild] ok: /dev/shm/gopi-test-scratch/…/repo built in 2.212587ms ()
```

### Real-world before/after (`~/work/GoPi`, the affected task field)

```
BEFORE
  master tip                         49ff4082 (2026-09-12 09:00:27 +0800)
  bin/gopi --version                 49ff408258b6-dirty     ← -dirty, from untracked files only
  git status --porcelain             29 lines (all ?? untracked)
  git status --porcelain -uno         0 lines
  pre-fix guard  AdoptionGitStatus   → non-empty ⇒ [rebuild] skip: worktree dirty …  EVERY pass
  post-fix guard AdoptionGitStatusTracked → "" ⇒ the guard no longer defers
```

Log lines the epic asks for:

- deferred (tracked dirt): `[merger] [rebuild] skip: worktree dirty — rebuild deferred to a clean tree (bin stays on the last clean master)`
- runs: `[merger] [rebuild] ok: <repo> built in <d> (<version>)`

Pinned by `TestServeRebuildTrackedDirtStillSkips` (skip line) and
`TestServeRebuildUntrackedOnlyRunsBuild` (ok line) respectively.

## Commit

`feat(rebuild): the post-merge dirty-tree guard judges TRACKED changes only`
(branch `feat/rebuild-guard-tracked-only`, worktree
`~/workspace/gopi-rebuildguard` — commit is shared with the main repo, no push
needed per Rule 6c).
