<!-- doc-class: historical -->
# Master-red: two `internal/tools` tests encoding the retired tmpfs scratch world

Task `01a0b1bf-5170-71b1-b4dc-5891f20c3f60` (branch `fix/master-red-scratch-skew`).
Reproduced at master tip `9ce8e159` (and `8b0011a3`), in a clean checkout, in this
worktree, and on a detached baseline worktree of master — so both reds are
**branch-independent, master's own**.

## The root cause, once, for both failures

`524156af` ("the scratch home stays on the block device — retire the tmpfs
route", operator ruling 2026-09-18) removed the memory backing from the scratch
home. Before it, `<home>/workspace/gopi-test-scratch` was (on this host) a
symlink onto `/dev/shm`, so **every** path under the suite's pinned scratch root
was on a **memory filesystem**; after it, the same tree is an ordinary directory
on the block device (`/home`, device `2052`, fs type `0xef53`).

Two tests had the old world baked into their *fixtures or their comments* rather
than into the contract they claim to test. Neither is a production regression —
`firstOpenSyncMode` and the merge build gate both behave correctly at
`9ce8e159`. They are tests that assert the wrong thing about a correct codebase,
and the shape of the wrongness is what makes them worth a devdoc.

### (1) `TestFirstOpenSyncModeDecisionTable` — a claim about the HOST dressed as a claim about the DECISION

```go
memoryDir := t.TempDir() // the suite's pinned scratch root: a memory filesystem
...
want: ledgerSyncModeWorkingFS   // "OFF"
```

`t.TempDir()` is inside the suite's pinned scratch root. The comment was true
until `524156af` and the assertion rode on it. Now the dir measures block-backed,
`firstOpenSyncMode` correctly returns `ledgerSyncMode` (`"NORMAL"`), and the arm
fails asserting `"OFF"`:

```
firstopen_sync_test.go:110: firstOpenSyncMode("…/suite-tools-run-…/memory.db")
    = "NORMAL", want "OFF" — the schema is built into a fresh file on tmpfs…
```

Two master tips give two different outcomes for the same file — `524156af`
**skipped** (its fixture failed the old pre-tmpfs premise), `8b0011a3` and later
**fail**. A `SKIP` vs `FAIL` pair on one premise is the tell that the premise,
not the code, moved.

### (2) `TestMergeBuildGate_GoCacheLeavesNoScratchCache` — the fixture seeding itself where it forbids the gate to go

```
merge_build_gate_gocache_test.go:161: the gate's cache
  …/suite-tools-run-…/TestMergeBuildGate_GoCacheLeavesNoScratchCache…/001/cache-root/<repo-key>/gocache
  is inside the scratch tree — scratch is designed to be COLLECTED,
  so a reusable cache can never live there
```

The arm's own fixture resolves the gate's cache root out of `t.TempDir()`.
While the scratch home was tmpfs that pin was refused **by accident** — the RAM
check (`AssertGoCacheOffShm`) refused it for a reason that had nothing to do with
the tree it sits in. `524156af` retired the memory backing, the accident could no
longer fire, and the same fixture became a cache root inside the collected
scratch tree: the fixture seeds its subject exactly where the arm asserts the
subject may not be.

The instruction lesson: **a placement rule that holds only because of an
unrelated filesystem choice is a rule nobody has.** The rule here is "scratch is
COLLECTED, so a reusable cache may not live in it", and it needed to be stated in
its own right.

## The fix

Production (`internal/tools/merge_build_gate_gocache.go`) — the third placement
ban, beside off-`/` and off-RAM:

```go
if scratch.IsInScratchTree(cacheDir) {
    taskNotifyLog("merger: BUILD gate ABSTAINING — "+ fmt.Sprintf(…InScratchReasonFmt, cacheDir, scratch.HomeDir(), scratch.GoCacheRootEnvName))
    return "", false
}
```

The gate **abstains loudly** (Rule 15b) — it never silently substitutes a
location, and the refusal names the measured tree, the home it measured, and the
override that moves it. It is measured through the same predicate the suite's own
arm asserts (`scratch.IsInScratchTree`), so production and test cannot disagree
about where the tree is; the check is at the **cache directory** (the deepest
spelling), so a symlinked scratch home is resolved by the predicate rather than
prefix-matched by hand.

Testing (`internal/tools/memory.go`) — the decision extracted as the total
function it already was:

```go
func firstOpenSyncMode(path string) string {
    return firstOpenLedgerSyncMode(memoryFilesystemLedger(path))
}
```

`firstOpenLedgerSyncMode(bool) string` is the mapping; `firstOpenSyncMode` is a
one-line call into it, so the two halves of the contract cannot drift. This
matters because the memory arm is otherwise **untestable on this host**: the
repo's own fixtures cannot create a tmpfs, and every path a suite can reach is
now block-backed. Asserting only reached paths would leave memory ⇒ relaxed
untested on exactly the hosts that run the suite.

Fixtures:

- `firstopen_sync_test.go`: the memory arm is **derived** — the candidates
  (`worktreeroots.ShmDir`, `/run/shm`, the suite's scratch home) are each
  classified by `statfs`, none by its name; when one measures as memory-backed
  the real path is asserted, and when none does the arm is asserted
  symbolically against `firstOpenLedgerSyncMode` and says so in its log. The
  device and unmeasurable arms no longer borrow the memory dir (they used to
  take `filepath.Join(memoryDir, "no-such-dir", …)`, i.e. they depended on it
  too).
- `merge_build_gate_gocache_test.go`: `mbgcIsDiskSafe`/`mbgcDiskDir` now
  measure the third property (`!scratch.IsInScratchTree`), so the fixture stops
  seeding itself inside the collected tree and falls back to
  `os.UserCacheDir` when `t.TempDir()` is unsafe. `…GoCacheRefusesTmpfsRoot`
  became `…GoCacheRefusesMemoryFilesystemRoot` (memory filesystem *discovered*
  by measurement, skips with its classification when none exists), and a new
  `…GoCacheRefusesScratchTreeRoot` pins the new ban — at the home itself and
  nested under it, plus the direct predicate assertion.
- `testmain_test.go`: the suite-level sandbox pin had the **same** defect —
  `toolsDirIsDiskSafe` measured only two of the three rules, and its first
  candidate is inside the scratch tree. Without the third measurement the suite
  pinned a cache root the gate now (correctly) refuses, and every gate test that
  needs a cache abstained (`TestMergeUnionGate_*`, `TestMergeBuildGate_GoDefault*`
  — observed, see below). The candidate list measures the tree now, and the
  guard's failure message names all three rules.

## Red-then-green evidence

Pre-fix files at master `9ce8e159` (`git stash`, same worktree, same host):

```
--- FAIL: TestFirstOpenSyncModeDecisionTable
    --- FAIL: TestFirstOpenSyncModeDecisionTable/memory-filesystem-ledger
        firstopen_sync_test.go:110: firstOpenSyncMode(…) = "NORMAL", want "OFF"
--- FAIL: TestMergeBuildGate_GoCacheLeavesNoScratchCache
    merge_build_gate_gocache_test.go:161: the gate's cache … is inside the scratch tree
FAIL	github.com/nick/gopi/internal/tools	0.951s
```

Post-fix, both scratch worlds (see `## Verification` in the task report for the
commands and full output).

## What this devdoc is not

No tmpfs is re-introduced anywhere: the block-backed scratch home is final, and
the fix removes the *last two* code sites that assumed otherwise. Nothing here
weakens an assertion to reach green — the decision table asserts strictly more
than before (the memory arm is now asserted on every host, via the measured
candidate or the decision function), and the placement test adds a rule it did
not have. The 10m suite-budget red (`01a0aba7`) is a different row and is not
touched.
