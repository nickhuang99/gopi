<!-- doc-class: historical -->
# The legacy bare-key build-cache family: `/dev/shm/<16-hex>/gocache`

**Task:** `01a0f4f6-1ec3-7bd2-9559-4bb6af7f35a5` · **Date:** 2026-10-01
**Verdict:** the writer was creating the tree it then refused; the family is now
collected by the EXISTING base sweep, through the existing namespace predicate.

---

## Completed

### 1. Root cause, with measured evidence (deliverable 1)

`/dev/shm/<16-hex>/gocache` is written by the **merge-time BUILD gate's own
PLACEMENT-REFUSAL test**, through `mergeBuildGateGoCache`
(`internal/tools/merge_build_gate_gocache.go`). The mechanism is the **order of
two statements**:

```go
cacheDir = filepath.Join(root, mergeBuildGateGoCacheRepoKey(repo), mergeBuildGateCacheDirName)
os.MkdirAll(cacheDir, …)      // ← created FIRST
…AssertGoCacheOffRootFS(root) // ← then measured: refuse, and return ok=false
…AssertGoCacheOffShm(cacheDir)
…IsInScratchTree(cacheDir)
```

Everything needed is already in that tree:

| element | value / source |
|---|---|
| `<16-hex>` | `mergeBuildGateGoCacheRepoKey` = first 16 hex of SHA-256 over the repo's **ABSOLUTE** path — i.e. `scratch.BuildCacheKeyHexLen` |
| `gocache` leaf | `mergeBuildGateCacheDirName` (now `scratch.BuildCacheGoCacheLeafName`) |
| the parent | `GOPI_MERGE_BUILD_GATE_GOCACHE_ROOT` (`scratch.GoCacheRootEnvName`) |
| the producer | `TestMergeBuildGate_GoCacheRefusesMemoryFilesystemRoot`, which **pins the cache root AT the memory base on purpose** (`t.Setenv(GoCacheRootEnvName, ramRoot)`) so it can assert the "never a cache on RAM" ban |

So the arm whose entire purpose is to refuse a RAM placement placed the cache
root at `/dev/shm`, and the gate created `<root>/<key>/gocache` before refusing
it. The key is a digest of the **absolute** path, and that fixture's repo is
`mbgcFixtureRepo` → `filepath.Join(mbgcDiskDir(t), …)` → **`t.TempDir()`, a fresh
path on every run** ⇒ a **fresh key every run** ⇒ **one new directory per run**,
each holding a single empty `gocache` (no build ever runs, because the gate
abstains). That is the whole family, and it explains every measured property:
171 instances, all tiny, all empty, all minted at test-suite rate rather than at
merge rate.

**Reproduced on demand** (this is the `Ran:` evidence below): one run of that
single test against unmodified `HEAD` left `/dev/shm/59b7322e7cf5476b/gocache`
behind on a base that had been measured empty immediately before.

### 2. The single-place fix + red→green test (deliverable 2)

**Writer** — `internal/tools/merge_build_gate_gocache.go`: the three placement
bans moved **before** the `os.MkdirAll`, into one function
(`mergeBuildGateGoCachePlacement`) so a moved check is not a re-spelled check.
The bans still return the same refusal, and the gate still ABSTAINS (no new
permission).

Making that a real fix (rather than a relocation) needed one change in the leaf,
`internal/scratch/rootgocache.go`: both device bans now judge the path by the
filesystem that would **hold** it (`existingAncestor(clean)`) instead of the
path itself. For a path that already exists the nearest existing ancestor is the
path, so **no existing verdict changes**; for a path that does not exist yet the
question becomes answerable *before* creation. This is not a new rule — it is the
rule `IsTempRootOffRootFS` already applies to a not-yet-created sandbox
(`internal/scratch/tmproot.go:~595`), reused (Rule 8).

**Collector** — `internal/scratch/base_residue.go` + `declared_build_cache.go`:
the family is now a namespace of the **existing** base sweep. Nothing new was
built: `buildCacheKeyName` (`BuildCacheKeyHexLen`, the writer's own key shape),
`buildCacheShardName` (`BuildCacheShardHexLen`) and `staleUnheldDirectory` (the
base sweep's age+holder rule, shared with the suite-run arm) are all reused, and
the `gocache` leaf became ONE constant, `scratch.BuildCacheGoCacheLeafName`, that
the writer (`mergeBuildGateCacheDirName`) and the sweeper both reference
(Rules 13/14).

**Red→green**, both halves:

* writer — `TestMergeBuildGate_GoCache_AbstainLeavesNoDirectory` (new): pins that
  a refused placement leaves **no** `<root>/<key>` and no `<root>/<key>/gocache`,
  for each of the three bans. **RED** on `HEAD` (three failures, one per ban,
  naming the leftover); **GREEN** with the fix.
* collector — `internal/scratch/base_legacy_gocache_test.go` (new, 3 arms):
  the shape table (what is ours / what is kept), plan↔sweep↔verdict agreement
  through `SweepBase`, and the hard "a symlink is never a candidate" rule.
  **RED** on `HEAD`, measured directly: `IsBaseResidueName("8a28affa4f85ba27")
  = false` and `SweepBase` `REMOVED=0` against a stale, unheld instance.
* **The arm this change RETIRED, and why that is not a regression.**
  `TestAssertGoCacheOffRootFSUnmeasurableIsRefused` asserted that a
  not-yet-created cache root is *unmeasurable* and therefore refused — the
  contract the fix deliberately replaced. It is worth naming as a defect rather
  than a loss: a check that can only judge a path that **already exists** forces
  its caller to create the directory first, so every refusal leaves the refused
  tree behind — which is precisely the `<16-hex>/gocache` family this task
  removes. The arm is now
  `TestAssertGoCacheOffRootFSIsAnswerableBeforeCreation`, pinning both
  directions (a not-yet-created root under a disk ancestor is MEASURED and the
  check must not create it; one under `/` resolves to `/` and is refused; an
  empty root is still never assumed safe) — commit `c872f27a`.

### 3. The ONE-place collection mechanism (deliverable 3) — implemented

The family lives in the **base sweep that already exists** —
`scratch.SweepBase`, reached by the operator's one-shot `gopi scratch-gc base` —
and its predicate is the existing shape rule. No second sweep, no second
namespace policy, and **`SweepBase` was not widened to blunt-prefix matching**:
the arm is a separate shape predicate (`isLegacyBareKeyGoCacheName` = the
writer's key shape **and no dash**), and it is disjoint from the generic
namespace walk by construction (a `gocache-<token>` name carries the family
prefix and a dash and is matched by `IsBaseResidueName` instead).

It is **explicit-only on purpose**, matching the suite-run arm's precedent: the
base is the SHARED `/dev/shm`, and a resident pass cannot tell another user's
live tree from an abandoned one. The plan face (`--dry-run`) and the `--explain`
face both reach the family, so the removal count is never a zero an operator
cannot read.

**A second defect, found by running the verb rather than the unit test.** With
the writer fixed and the collector wired, `scratch-gc base --dry-run` against a
fixture base reported `removed=0` **with no plan**, while `scratch-gc base` on
the same fixture removed one entry — the plan and the sweep disagreed about every
non-default base. `SweepBase` runs the legacy arm for whatever base it is handed,
but `BaseResiduePlan` added that arm's plan only inside the `default-base` branch.
Fixed in a second commit: the plan moved out of that branch, beside the suite-run
plan, where the sweep's own scoping puts it. This is the failure mode the task
exists to remove (a zero nothing can explain), and it was invisible to the unit
tests — `TestLegacyBareKeyGoCache_PlanSweepAndVerdictsAgree` calls
`BaseResidueLegacyGoCachePlan` directly, so it never saw the wiring (Rule 31).

### 4. The family's measured shape (deliverable 4)

**Before (briefing's measurement, 2026-10-01):** 171 key directories, ~2.7 G of
`/dev/shm` residue, 0 held (nothing resides in a key a `go` invocation has
already exited).

**On this host, measured at 14:04 by this run:**

```bash
ls -A /dev/shm | grep -cE '^[0-9a-f]{16}$'   # → 0
find /dev/shm -maxdepth 2 -name gocache -type d | wc -l   # → 0
```

**After: 0, by the same commands.** The host was **wiped at 13:10:30**, roughly
80 seconds before this run's first measurement — `/dev/shm` was recreated empty
(`stat -c '%y' /dev/shm` → `2026-10-01 13:10:30`, `df` → 0 used; `/dev/shm` mtime
read 13:10:30 at `t+19s` and the current contents are 13:26 suite roots). See
**Not Done** for what that costs the before/after claim, and how it is answered
instead.

---

## Not Done (honest)

* **The 171-instance backlog could not be swept, because it no longer exists.**
  `/dev/shm` was wiped at 13:10:30, before this run measured it, so the
  "before/after count of the family" is **0 → 0** and is *not* evidence that the
  collector works. The collector's evidence is therefore taken on a **fixture
  base** (10 instances of the same shape, driven through the real CLI verb) and
  the writer's evidence is the **RED reproduction** — the unmodified code
  minting exactly `/dev/shm/59b7322e7cf5476b/gocache` on a base measured empty
  one command earlier. Both are quoted with their commands below.
* **The collector arm runs off the explicit CLI verb, not the resident tick.**
  This is deliberate (the shared-base reasoning above), but it means an
  unattended host accumulates this family until the operator runs
  `gopi scratch-gc base`. If the ~2.7 G/171-instance rate recurs, the fix is a
  *different* decision (a residency-safe ownership marker), not a wider
  predicate — and that is a new task, not this one.
* **The pre-existing master failures were FILED, not fixed** (Rule 26): the two
  `cmd/agent` arms that fail on master alone are now task
  `01a0f603-76ea-73a0-b79a-769abc30a15e` (tags `bug`), with the exact
  reproduction, the verbatim failure text and the acceptance bar. They are
  master's defects; this branch's diff in that package is `cli_scratch_gc.go`
  +12 lines.
* **The pre-existing-shape degradation was not fixed**: for a cache ROOT that
  does not yet exist, `filepath.EvalSymlinks` does not resolve the tail, so the
  device probe falls back to the unresolved path. This is also why the
  `/`-device pre-existing case *reports on an existing directory* instead. Not a
  consequence of this change (it cannot turn a refusal into a pass), and out of
  scope.

---

## Verification

* **Build:** `go build ./...` → no output, exit 0 (`BUILD_OK`);
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0.
* **Tests:** — the FINAL run, at the branch tip `28e095dc`:

  `make test-budget PKGS='./internal/scratch ./internal/tools ./cmd/agent'`
  → 3 packages, **496s wall, exit 1, 1 with a failure** (`cmd/agent`; stream
  `…/queue/test-budget/run-1790833172-269025.json`).

  - `internal/scratch` — **ok** (the RED probe is gone from the branch; the new
    contract is pinned by `TestAssertGoCacheOffRootFSIsAnswerableBeforeCreation`
    and the three `TestLegacyBareKeyGoCache_*` arms).
  - `internal/tools` — **ok** in this run, including the writer's own
    `TestMergeBuildGate_GoCache_AbstainLeavesNoDirectory` and the whole
    `TestMergeBuildGate_GoCache*` family. (An earlier run of this same command
    also reported `internal/tools` FAILED on `TestTaskSyncImportConcurrentSamePatch`;
    that test passes in isolation — `go test -count=1 -run
    'TestTaskSyncImportConcurrentSamePatch' ./internal/tools/` → `ok … 0.199s` —
    and it is untouched by this diff, so it is the load-sensitive flake the
    25m-per-package budget run exposes, not this branch.)
  - `cmd/agent` — 4 failures, **pre-existing and proven so by a control run at
    master**. The branch's entire diff in that package is `cli_scratch_gc.go`
    +12 lines. Control: a DETACHED worktree at master `4926e4b9`
    (`git worktree add --detach …/gopi-mastercheck-28e095dc 4926e4b9`), same
    four tests, same machine, same hour →

    ```
    --- FAIL: TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer (0.01s)
    --- FAIL: TestSlashMenuOneCandidateStatesWhyTheArrowCannotMove (0.00s)
        slash_menu_test.go:258: the production completer offers 2 candidates for "/q"
                               (want exactly 1, the one-item case this contract is about)
    ```

    i.e. two of the four fail on master with **no part of this branch present**
    — master's own `/queue` verb and its chat system prompt are what those two
    arms disagree with. The other two (`TestPrefetchGoroutineSettleGapDetectsALiveLeak`,
    `TestChatSurfaceBound_…`) **pass on that same detached master worktree in
    isolation** (`go test -count=1 -run '<the two>' ./cmd/agent/` → `ok … 1.026s`),
    so they are the load-sensitive timing arms the concurrent budget run exposes,
    not master failures. **Confirmed one step further at close-out**: with the
    isolated selection
    `go test -count=1 -run 'TestPrefetchGoroutineSettleGapDetectsALiveLeak|TestScratchGCDepsOriginReadsExeNotArgv0|TestMergeEveryKilledLoopLeavesNoResidueInTheProfile'
    ./cmd/agent/` → **master `4926e4b9` `ok … 24.686s` AND branch tip `634fa0a4`
    `ok … 22.512s`** — the same three arms that fail under the concurrent
    three-package run pass on both trees when run alone, so their failure is the
    run's concurrency, not either tree's content. **Reported and filed, not
    adopted** — repairing these is a different task (`01a0f603-76ea-73a0-b79a-769abc30a15e`),
    and this branch does not touch a line of them.
* **Ran** (exact invocations, verbatim):

  ```bash
  # (a) THE CHANGED SURFACE, driven for real: the operator's one-shot verb,
  #     against a fixture base, on the smallest off-/ sample (2 instances).
  FIX=/home/user/workspace/_tmp/legacy-b6m24vc8     # fixture base, off /, off /dev/shm
  GOPI_SCRATCH_BASE="$FIX" ./bin/gopi scratch-gc base --dry-run --explain
  GOPI_SCRATCH_BASE="$FIX" ./bin/gopi scratch-gc base

  # (a2) THE PLAN FACE, which is where the second defect showed: the dry-run on
  #      a NON-default base must name what the sweep would remove.
  GOPI_SCRATCH_BASE="$FIX2" ./bin/gopi scratch-gc base --dry-run     # after: removed=1 + the plan line
  GOPI_SCRATCH_BASE="$FIX2" ./bin/gopi scratch-gc base

  # (b) THE WRITER, reproducing the family against unmodified HEAD:
  git checkout -- internal/tools/merge_build_gate_gocache.go
  go test -count=1 -run 'TestMergeBuildGate_GoCache_AbstainLeavesNoDirectory' ./internal/tools/
  ls -A /dev/shm

  # (c) THE COLLECTOR, red on HEAD:
  go test -count=1 -v -run 'TestZZRedProbeLegacyBareKey' ./internal/scratch/
  ```

* **Result:**

  **(a) the real verb — fixture base, before → after (10 instances):**

  ```
  scratch-verdict …/0123456789abcdef verdict=keep       age=2s younger than the stale window (6h0m0s) [kept]
  scratch-verdict …/8a28affa4f85ba27 verdict=collectable age=8842h19m49s collectable: no owner record …
  scratch-verdict …/deadbeefdeadbeef verdict=keep       age=8842h19m49s the name is our key shape but the tree
                                                                         inside is not a build cache we wrote — kept [kept]
  gopi scratch-gc base: 3 candidate(s) judged, 1 collectable

  scratch-gc role=cli-base base=… removed=1 stale=6h0m0s disabled=false
  ```

  After the sweep the fixture holds **only** `deadbeefdeadbeef/mine.txt` (the
  hex-named foreign tree the shape rule KEEPS) and
  `0123456789abcdef/gocache` (fresh, inside the window). The stale instance
  `8a28affa4f85ba27/` is gone, `gocache` and all. The dry-run named exactly the
  entry the sweep then removed.

  **(a2) the plan face, after the second fix — plan and sweep agree:**

  ```
  $ GOPI_SCRATCH_BASE=<fixture2> ./bin/gopi scratch-gc base --dry-run
  scratch-gc role=cli-base base=…/legacy2-b_29xban removed=1 stale=6h0m0s disabled=false dry-run=true
  would-remove …/legacy2-b_29xban/8a28affa4f85ba27 age=8842h23m26s
  $ find <fixture2>          # unchanged: a dry-run removes nothing
  <base>  <base>/0123456789abcdef  <base>/0123456789abcdef/gocache
  <base>/8a28affa4f85ba27  <base>/8a28affa4f85ba27/gocache  <base>/deadbeefdeadbeef

  $ GOPI_SCRATCH_BASE=<fixture2> ./bin/gopi scratch-gc base
  scratch-gc role=cli-base base=…/legacy2-b_29xban removed=1 stale=6h0m0s disabled=false
  $ find <fixture2>          # the named entry is gone, the other two survive
  <base>  <base>/0123456789abcdef  <base>/0123456789abcdef/gocache  <base>/deadbeefdeadbeef
  ```

  **(a3) re-run at the branch tip, with the finished binary** (`bin/gopi` rebuilt
  from `9b86a364`; fixture under the session directory, on `/home` — a different
  filesystem from `/`, so nothing here is an assertion about the host's tmpfs):

  ```
  $ GOPI_SCRATCH_BASE=<fixture3> ./bin/gopi scratch-gc base --dry-run
  scratch-gc role=cli-base base=…/verify/legacy-b1 removed=1 stale=6h0m0s disabled=false dry-run=true
  would-remove …/verify/legacy-b1/8a28affa4f85ba27 age=267h31m43s
  $ find <fixture3>          # unchanged by the dry-run
  <base>  <base>/0123456789abcdef/gocache  <base>/8a28affa4f85ba27/gocache  <base>/deadbeefdeadbeef

  $ GOPI_SCRATCH_BASE=<fixture3> ./bin/gopi scratch-gc base
  scratch-gc role=cli-base base=…/verify/legacy-b1 removed=1 stale=6h0m0s disabled=false
  $ find <fixture3>          # the named entry is gone; the fresh key and the
  <base>  <base>/0123456789abcdef/gocache  <base>/deadbeefdeadbeef
                             # foreign hex-named tree both survive
  ```

  **(b) the writer, RED — the unmodified code minting the family:**

  ```
  --- FAIL: TestMergeBuildGate_GoCache_AbstainLeavesNoDirectory (0.00s)
      … left /home/user/workspace/gopi-test-scratch/gocache-abstain-probe/59b7322e7cf5476b behind …
      … left /home/user/workspace/gopi-test-scratch/gocache-abstain-probe/59b7322e7cf5476b/gocache behind …
      … left /tmp/gocache-abstain-probe/59b7322e7cf5476b behind …
      … left /tmp/gocache-abstain-probe/59b7322e7cf5476b/gocache behind …
      … left /dev/shm/59b7322e7cf5476b behind …
      … left /dev/shm/59b7322e7cf5476b/gocache behind …
  --- /dev/shm AFTER the red run, verbatim ---
  59b7322e7cf5476b
  ```

  Six failures, one pair per ban, and the directory the pre-fix gate left **is
  the family**. Restored from a file copy (not `git stash` — this repo's
  `refs/stash` is shared by 5 linked worktrees and the guard refuses it), the
  same test is **green** and `/dev/shm` is back to 0 entries.

  **(c) the collector, RED on HEAD:**

  ```
  zz_redprobe_test.go:18: IsBaseResidueName("8a28affa4f85ba27") = false
  zz_redprobe_test.go:23: SweepBase REMOVED=0 (stale+unheld instance of <16hex>/gocache)
  zz_redprobe_test.go:27: RED CONFIRMED: the family is claimed by no sweep
  ```

* **The family on this host, before/after, with the exact command:**

  ```bash
  $ ls -A /dev/shm | grep -cE '^[0-9a-f]{16}$'          # before → 0
  $ find /dev/shm -maxdepth 2 -name gocache -type d | wc -l   # before → 0
  $ ls -A /dev/shm | grep -cE '^[0-9a-f]{16}$'          # after  → 0
  $ find /dev/shm -maxdepth 2 -name gocache -type d | wc -l   # after  → 0
  ```

  Both 0 because the host was wiped at 13:10:30 (see **Not Done**); stated as
  measured rather than dressed up as a fix effect.

* **Commit:** see the commit on `task/01a0f4f6-1ec3-7bd2-9559-4bb6af7f35a5`.

---

## Evidence (self-filed)

* **The writer defect, reproduced against unmodified HEAD.**
  `go test -count=1 -run 'TestMergeBuildGate_GoCache_AbstainLeavesNoDirectory'
  ./internal/tools/` → `--- FAIL` with six `left … behind` lines, and
  `ls -A /dev/shm` → `59b7322e7cf5476b`. The same command with the fix restored →
  `ok github.com/nick/gopi/internal/tools 0.417s` and `ls -A /dev/shm` → 0
  entries.
* **The collector defect, reproduced against unmodified HEAD.**
  `go test -count=1 -v -run 'TestZZRedProbeLegacyBareKey' ./internal/scratch/` →
  `IsBaseResidueName("8a28affa4f85ba27") = false`, `SweepBase REMOVED=0`.
* **The changed surface executed for real — re-run at the branch tip
  `28e095dc`, with `bin/gopi` built from it.** Fixture base
  `/home/user/workspace/_tmp/legacy-tip-28e095dc` (on `/home`: off `/`, off
  RAM), three instances, one stale (`8a28affa4f85ba27`, mtime forced to
  2025-01-01), one fresh key, one foreign hex-named tree:

  ```
  $ GOPI_SCRATCH_BASE=<fixture> ./bin/gopi scratch-gc base --dry-run
  scratch-gc role=cli-base base=…/legacy-tip-28e095dc removed=1 stale=6h0m0s disabled=false dry-run=true
  would-remove …/legacy-tip-28e095dc/8a28affa4f85ba27 age=15325h41m48s

  $ GOPI_SCRATCH_BASE=<fixture> ./bin/gopi scratch-gc base
  scratch-gc role=cli-base base=…/legacy-tip-28e095dc removed=1 stale=6h0m0s disabled=false
  ```

  `find` after the dry-run → unchanged (6 entries, the plan removed nothing);
  after the real sweep → 4 entries: the stale `8a28affa4f85ba27/` is gone with
  its `gocache`, while `0123456789abcdef/gocache` (fresh) and `deadbeefdeadbeef/`
  (foreign `mine.txt`) both survive. The plan named exactly the entry the sweep
  removed.
* **The pre-existing `cmd/agent` failures, attributed by a master control run.**
  `git worktree add --detach ~/workspace/gopi-mastercheck-28e095dc 4926e4b9` →
  `go test -count=1 -run
  'TestTermOnceTurnDrivesTheEngineAndStreamsTheAnswer|TestSlashMenuOneCandidateStatesWhyTheArrowCannotMove'
  ./cmd/agent/` → both `--- FAIL` with master alone in the tree; the other two
  arms `ok … 1.026s` on that same worktree.
* **The same arms, run alone, on BOTH trees — the concurrency verdict.**
  `go test -count=1 -run
  'TestPrefetchGoroutineSettleGapDetectsALiveLeak|TestScratchGCDepsOriginReadsExeNotArgv0|TestMergeEveryKilledLoopLeavesNoResidueInTheProfile'
  ./cmd/agent/` → master `4926e4b9`: `ok github.com/nick/gopi/cmd/agent 24.686s`;
  branch tip `634fa0a4`: `ok github.com/nick/gopi/cmd/agent 22.512s`. The arms
  that fail inside the concurrent three-package budget run pass on both trees
  when run alone, so the variable is the run's concurrency, not the tree.
  The second control worktree was removed afterwards
  (`git worktree remove --force /home/user/workspace/gopi-mastercheck-2`) and
  `git worktree list` shows no `mastercheck-*` entry.
* **The producer arm's own suite leaves nothing behind.**
  `find /dev/shm -maxdepth 2 -name gocache -type d | wc -l` → `0` before and `0`
  after `go test -count=1 -run 'TestMergeBuildGate_GoCache' ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools 1.155s`. Under the pre-fix gate the
  same arm minted one `<16hex>/gocache` per run (quote (b)).
* **Rule 30.** `git status --porcelain` in this worktree → see the commit; the
  run's own close-out prints the worktree's state.
* **The host wipe, measured.** `stat -c '%y' /dev/shm` → `2026-10-01 13:10:30`;
  `df -h /dev/shm` → `0` used at `t+19s`; the current entries are
  `suite-cmd-agent-run-129620960` / `suite-scratch-run-1024172618` from 13:26.
  This run's first measurement was at 13:11:50.
* **No load was generated** (Rule 27): every number above comes from a directory
  listing, a `find`, or a single targeted test. No `dd`, no stress helper, no
  parallel workers.
* **Rule 6.** Every fixture is a plain directory under `t.TempDir()` or
  `/home/user/workspace/_tmp`; nothing under `/dev/shm` was created or removed by
  a fixture. The only `/dev/shm` writes in this run are the two the RED
  reproduction required, both removed by absolute path immediately afterwards
  (`python3 -c "shutil.rmtree(…)"`, quoted under `## Result` (b)).
