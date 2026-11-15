<!-- doc-class: normative -->
# The shared build cache tree — the fourth declared tree, and the merge-path decision it needed

Branch `feat/build-cache-tree-gc`, based on `feat/scratch-gc-declared-trees`
(`b448b18f`) — the branch that declared the first three trees and deliberately
left this one out. Rule 26: the incomplete fix gets its own dispatch.

## What was missing

`~/workspace/gopi-build-cache` is created and re-created by the repo's own
merge/build gate and was bounded by **nothing**. Measured at the start of this
task: **174 key directories, 80,081 files, 5.4 GB** (`du -sb`), and **181 keys
3.2 hours later** — it grows by one throwaway key per worktree per task and
deletes none of them.

## The decision, and why it is 24h

The gate's own bound (`mergeBuildGateGoCacheMaxBytes = 512 MiB`) is **PER KEY**.
Measured over 183 keys / 84,779 files / 5.21 GB:

| statistic | value |
|---|---|
| largest key | 38 MB |
| median key | 32 MB |
| keys over 100 MB | **0** |
| largest as a share of the gate's bound | **7.5%** |

So the gate's bound is **unreachable** — the toolchain's own trim keeps each key
small — and the tree grows by **accumulation** alone. That is the finding that
decides the rule: no per-key bound can bound this tree, and `age` is the only
lever that can. The window therefore *is* the bound, so it is sized from the
measured rate:

| window | keys held at the observed 1.7 GB/h | at a merge-only ~1.3 GB/day |
|---|---|---|
| 24h (**chosen**) | tens of GB (this host: continuously active) | ~1.3 GB |
| 72h (rejected) | ~120 GB — not a bound at all | ~4 GB |

What the window protects is exactly one thing: rebuilding the **same checkout**.
A worktree that has not been built in a day is a task that is done, and a cold
rebuild costs one build — against the three disk-exhaustion incidents
(2026-09-11, 2026-09-15, 2026-09-16) caused by trees nothing bounded.
`BuildCacheStaleAge` is deliberately equal to `GoTmpStaleAge`; choosing a
different number to *look* deliberate would be the opposite of a measured
decision.

## The trap: a key's own mtime is not its age

The gate creates `<key>/` once and the **Go toolchain then fills `gocache`
below it** — the key's mtime never moves again. Measured on the real tree: 172
keys, **every one of them with `gocache` strictly newer than the key holding
it** (e.g. key `1789864127`, `gocache` `1789864139`). A key backdated by a day
therefore *looks* abandoned while a build written into it seconds ago is still
inside it.

So the tree declares its own staleness oracle (`DeclaredTree.EntryStale`,
`buildCacheKeyStale`): a key is stale only when **every regular file below it**
is older than the window. That is *stricter* than the mtime reading, and it is
proved on the live tree (below).

Two supporting rules ride with it:

- **A key holding a symlink is never collected** — the link's target is not
  followed, so nothing measurable is asserted about it, and following it could
  age (then delete) bytes outside the tree.
- **An empty key falls back to its own mtime** — a key the gate created and
  never filled is exactly what its own mtime describes, and without this a
  crashed gate would leak one directory per invocation.

## The key length is ONE constant (Rules 13/14)

A bare `16` in the writer and a bare `16` in the predicate would drift silently:
the sweep would report the tree as *visited* while matching nothing, which is
precisely the accumulation defect. `scratch.BuildCacheKeyHexLen` is the single
definition; the writer references it
(`mergeBuildGateGoCacheRepoKeyHexLen = scratch.BuildCacheKeyHexLen`) and
`TestBuildCacheKeyIsInsideTheSweepsNamespace` asserts the gate's minted key is
inside the sweep's namespace through the **sweep's own predicate**
(`scratch.BuildCacheKeyNameIsOurs`), not a second hand-written hex check.

## Root, namespace, and the several-trees problem

- **The root is never a candidate** — `<worktree root>/gopi-build-cache` is
  refused in the predicate and re-checked at the delete
  (`TestBuildCacheRootSurvivesAnAgedKey`), the same invariant that exists because
  a hand sweep deleted `GOTMPDIR` itself.
- **The namespace predicate is the writer's key shape** (16 lowercase hex), so
  an operator's own directory parked in the same root is never a candidate
  (`TestBuildCacheKeyPredicateIsTheWritersKeyShape`) — the same reason the
  GOTMPDIR tree requires `go-build*`.
- **One row per resolved worktree root.** A repo whose settings entry declares
  its own `worktree_root` gets its cache tree there, which the ambient resolution
  cannot see; `internal/config` hands the enumeration over
  (`RegisterBuildCacheWorktreeRoots`) exactly as it hands over the data-root
  resolvers. The first (ambient) row keeps the plain name `build-cache` so the
  log stays diffable; additional rows are `build-cache@<root>` so one host's
  several trees stay attributable.

## Cost on the real host

A full `gopi scratch-gc trees --dry-run` against the live tree (192 keys /
~85k files) takes **9.0s** wall — the walk is bounded by `TreeEntryScanLimit`
and the per-key freshness read is one `WalkDir` per key. The watchdog runs this
face every 30 minutes behind an flock, so the cost is paid well inside its tick.

## Not Done (honest)

- **A merge-time "build in flight" veto is NOT added.** The measured rule is
  *conservative* instead: a key is collectable only when every file inside it is
  older than 24h, so a build running now is never touched (proved below). A veto
  based on a marker file would be strictly weaker evidence than the write times
  themselves. What remains unaddressed at the *envelope* level is the
  hypothetical host simultaneously (a) running a 24h+ build and (b) sweeping
  under an overridden window — recorded here rather than papered over.
- **The pre-existing `internal/tools` suite is slow on this host** (~minutes;
  see the gate run below). Its `TestBuildCacheKeyIsInsideTheSweepsNamespace` is
  not the cost — the package's own fixtures are.
- **The window is not adaptive.** A host whose worktrees live longer than 24h
  pays a cold rebuild; the lever is the tree's constant (and the CLI's
  `--scratch-gc-stale-min` override), not a heuristic.

## Verification

### 1. A planted aged key is removed by a REAL `gopi scratch-gc trees` run

Planted inside the live tree: `aaaaaaaaaaaaaaaa/` (valid key shape) holding a
64 MiB cache body backdated 3 days, beside two controls — `bbbbbbbbbbbbbbbb/`
with a **fresh** body, and `operator-notes-not-a-key/` with an aged foreign
file.

```
scratch-gc role=cli-trees home= removed=0 stale=24h0m0s disabled=false tree=go-tmp bytes=0
scratch-gc role=cli-trees home= removed=0 stale=72h0m0s disabled=false tree=raw bytes=0
scratch-gc role=cli-trees home= removed=0 stale=336h0m0s disabled=false tree=session-raw bytes=0
scratch-gc role=cli-trees home= removed=1 stale=24h0m0s disabled=false tree=build-cache bytes=67108864
gopi_exit=0

SURVIVES /home/user/workspace/gopi-build-cache        <- the ROOT
REMOVED  /home/user/workspace/gopi-build-cache/aaaaaaaaaaaaaaaa
SURVIVES /home/user/workspace/gopi-build-cache/bbbbbbbbbbbbbbbb
SURVIVES /home/user/workspace/gopi-build-cache/operator-notes-not-a-key
```

`bytes=67108864` is exactly the planted body — the reclaimed bytes are on the
existing logfmt line, as the epic asks.

### 2. The freshness rule, proved on the live tree (the discriminating case)

`cccccccccccccccc/` planted with its **key directory backdated 4 days** and a
**fresh** 8 MiB body inside:

```
key DIR backdated 4 days: .../cccccccccccccccc 2026-09-16 10:55:44
file inside is FRESH:     .../gocache/00/planted-fresh-body 2026-09-20 10:55:44
scratch-gc ... tree=build-cache bytes=0
SURVIVES (correct: a key with a fresh file inside is never collected —
           mtime-of-key alone would have taken it)
```

### 3. `df -h /home` and `du -sb` before/after

```
BEFORE  /dev/sda4  766G  478G  250G  66% /home     du -sb = 5423542307
AFTER   /dev/sda4  766G  478G  250G  66% /home     du -sb = 5431947299
```

The after-reading is **equal-or-larger** and that is the honest result: the real
run happened while this host's merge/build gate was creating keys (a new key per
worktree per task — the very growth this task bounds), so `du` moved by more than
the 64 MiB reclaimed. The **reclaimed** amount is the authoritative number and it
is reported by the sweep itself (`bytes=67108864`), not inferred from a `du`
delta on a live tree.

### 4. Gate, tests, build

- `TestDeclaredTreesCoverRepoWriters` — six new anchor rows cover the build-cache
  writers (`merge_build_gate_gocache.go` × 3, `declared_build_cache.go`,
  `config/scratch_trees.go`), and `TestGateCatchesUndeclaredTree` gained the
  red-proof case for `build-cache`.
- `TestDeclaredTreesAreWiredAndNamed` now pins the *name* rather than the value
  (a duration-keyed map could not hold two windows that legitimately share one)
  and asserts every named window is referenced by a tree.
- `TestBuildCache*` (9 cases), `TestBuildCacheKeyIsInsideTheSweepsNamespace`,
  and the three `TestScratchGCCLITrees_*` e2e cases (which now plant an entry in
  the fourth tree and cannot touch the developer's real tree — everything runs in
  a sandbox under `t.TempDir()`).

See the task report for the exact command output and the commit hash.
