<!-- doc-class: historical -->
# Devdoc: the merger BUILD gate's Go build cache moves to disk (bounded, shared)

Branch: `fix/merger-build-gate-disk-gocache`
Files: `internal/tools/merge_build_gate_gocache.go` (new), `internal/tools/merge_build_gate.go`,
`internal/scratch/rootgocache.go` (new), `internal/worktreeroots/roots.go`,
`internal/testutil/scratchroot.go`

## The defect (measured)

The merge-time BUILD gate (`mergeBuildGateGoCache`) pointed `GOCACHE` at a
**per-PROCESS** directory inside the tmpfs scratch root:

```
<scratch>/queue/queue-runtime-merge-build-<pid>/gocache
```

A Go build cache is an inherently **reusable, cross-task** asset; a per-PID
directory is the one placement that guarantees it is never reused. Every gate
call wrote a full build's worth of cache into RAM-backed tmpfs and then threw it
away:

| measurement (this host) | value |
|---|---|
| per-process cache reached | 726M-1.14G (briefing) — 170M for this repo's `go build ./cmd/agent/` |
| dead per-PID copies counted 2026-09-15 | 54 MB x 100+ (`find … -name gocache -type d` = 62 dirs, 4.47 GiB) |
| /dev/shm | **100%** on 2026-09-15; 6.3G used of a 7.8G cap when this change was written |
| toolchain cost of a cold cache | **84.4s** (measured below) |

Two failures in one: the gate paid the **cold** compile cost on *every* revision
(so a branch build and, on failure, the master build each recompiled the world),
and it spent RAM-backed tmpfs on a cache nothing could read again.

## The fix — one shared, disk-backed, BOUNDED location per repo

```
<worktree root>/gopi-build-cache/<repo-key>/gocache
```

* **Default root** = `<home>/workspace` (`worktreeroots.DefaultRoot`) — the same
  declared-per-repo root a worktree uses. A build cache is reusable *across
  tasks* but **not across repos** (a cache keyed by one repo's package graph is
  dead weight for another), so the per-repo root is where it belongs.
* **One resolver** (Rule 14): `config.ResolveRepoWorktreeRoot` →
  `worktreeroots.Resolve`, i.e. settings.json `repos.<repo>.worktree_root`, with
  `GOPI_WORKTREE_ROOT` winning when a sandbox pins it. No second root
  definition; an operator who declares a repo's root moves the cache with it.
* **Per-repo key** = first 16 hex chars of `sha256(absolute repo path)` — never
  the basename (two repos routinely share a leaf; a shared leaf would silently
  share one cache across projects).
* **Override**: `GOPI_MERGE_BUILD_GATE_GOCACHE_ROOT` (constant in
  `internal/scratch`, the ONE reader in the gate — Rule 2).
* **Leaf name**: `worktreeroots.SharedBuildCacheDirName = "gopi-build-cache"`;
  `internal/scratch` renders it as an alias pinned by
  `TestSharedBuildCacheLeafMatchesWorktreeRoots`.

### Placement is MEASURED, never a spelling (Rule 22)

`scratch.AssertGoCacheOffRootFS` refuses a cache root sharing the device of `/`
(the 2026-09-11 partition) and `scratch.AssertGoCacheOffShm` refuses one on a
memory filesystem. The second check is not redundant: **/dev/shm is its own
mount, not on `/`**, so a device comparison alone cannot see the tmpfs case —
and the *suite's own scratch root is tmpfs on this host*, which is legitimate
for a fixture and fatal for a cache holding a build's worth of bytes. Both
refusals name the measured classification, the incident, and the lever.

**Verification recipe (reusable)**: a placement test must assert by
`statfs` type + device id, and its fixture must live where the *fixed* code
puts the cache — a suite's `t.TempDir()` is inside the scratch home and will
therefore be tmpfs. `mbgcDiskDir` uses `t.TempDir()` only when it measures
disk-safe, else `os.UserCacheDir()`.

### The bound (and why it is the toolchain's own trim plus a report)

The toolchain already trims a GOCACHE: `Trim` runs when the cache is closed at
the end of a `go` invocation — at most once per `trimInterval` (24h) — and
deletes entries unused for `trimLimit` (5 days); measured in
`GOROOT/src/cmd/go/internal/cache/cache.go:337-406` (go1.24.5).

The gate's *old* cache was self-emptying (it died with its PID), so a bound came
for free. A **shared** cache has no such luck: without a bound it is a slow disk
leak. So the gate adds the two halves the toolchain cannot:

1. a **size report** at every gate call (a bound nobody can see is a bound
   nobody can check) — `merger: BUILD gate shared build cache … holds X (bound Y)`;
2. a **reachable bound**. `mergeBuildGateGoCacheMaxBytes = 512 MiB` — below the
   726M-1.14G the per-process cache reached, so it is genuinely reachable. When
   the measured body exceeds it, the gate drops the entry shards (leaving
   `trim.txt`/`log.txt` so the toolchain still recognises its cache) and the next
   build repopulates it: bounded disk, warm again within one build, never a
   failure. A shard that cannot be dropped is reported, never swallowed.

Why `Trim` alone was not enough: the gate's build can be killed by its timeout,
and a killed `go` invocation never reaches `Trim` — that is exactly how the
per-process cache grew to 1.14G in the first place.

**Deferred finding (recorded, not fixed here)**: `internal/scratch.StaleAge = 6h`
(`gc.go:60`) age-collects runtime scratch entries — including the merger's
per-call build checkouts and any *leftovers* of the old cache placement. It does
NOT collect the new cache (the new location is deliberately outside the scratch
tree, which is the point), so the 6h window now governs only genuinely
throwaway data. Two consequences worth a separate task: (a) the per-call
worktree scratch still pays a cold `worktree add`; (b) a cache placed under
`~/workspace` can be swept by *nothing* — which is why the report-then-trim
bound above is not optional.

## What is deliberately unchanged

* **GOMODCACHE stays the shared READ-ONLY operator cache** (455daa77):
  `mergeBuildGateSharedModCache` still resolves `GOPI_MERGE_BUILD_GATE_GOMODCACHE`
  → `go env GOMODCACHE`, validates existence + `cache/download` subtree, and the
  gate never writes there. Re-verified green in
  `TestMergeBuildGate_SharedModCacheResolvesWithProxyOff` (real build,
  `GOPROXY=off`, no `go: downloading`) — the only change in that file is the
  build helper's new signature and a `t.Cleanup` that removes the cache the
  helper creates.
* **Non-Go-repo fallback unchanged**: a repo whose declared build cannot read
  modules still gets the gate's own scratch module cache
  (`mergeBuildGateScratchModCache` → `<class>/gomodcache`).
* **The operator's `~/.cache/go-build` is still never used** (a merge probe must
  not fill or depend on the user's build cache).
* No new dependency (stdlib `crypto/sha256`, `encoding/hex`), no privilege
  (Rule 25).

## Measurements (commands + output)

### Warmth at the NEW location — cold vs warm, through the gate's own code

Harness: temporary in-package test (deleted before commit) calling
`mergeBuildGateGoCache` + `mergeBuildGateBuild`, fresh cache root, build =
`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`:

```
BEFORE-CENSUS:
  class dir: /dev/shm/gopi-test-scratch/queue/queue-runtime-merge-build-1247363
  62  (gocache dirs under the tmpfs scratch root)
  6.2G /dev/shm/gopi-test-scratch
  4474496 KiB in gocache dirs
du(cache before)=4.0K  /home/user/workspace/gopi-measure-cache-1247363/44df950bbe9a5942/gocache
COLD build: status=PASS elapsed=1m24.387s   du(cache after cold)=170M
WARM build: status=PASS elapsed=3.733s      du(cache after warm)=170M
RATIO cold/warm = 22.6x
```

* cold **84.4s** vs warm **3.7s** → the 68s/~2s reference is reproduced (my
  figures are a little higher because this harness runs inside a loaded test
  process). **The second build at the same location is measurably warm.**
* the cache is **populated on disk**: `du -sh` = **170M** (vs 4.0K before).
* **no cache-shaped directory is created under the tmpfs scratch class** by a
  gate call (census unchanged by the two builds, modulo the suite's own
  fixtures).

### tmpfs cost: before vs after (the OLD placement, executed)

Same tree, same build command, `GOCACHE` pointed at the OLD location
(`<scratch>/queue/queue-runtime-merge-build-<pid>/gocache`), module cache shared:

```
tmpfs before: 6.3G        gocache before: 0
cold build wall=100.38s   gocache after cold build: 169M
tmpfs after: 6.5G         (+169M on /dev/shm, RAM-backed)
warm build wall=2.97s
cleaned my own measure dir; tmpfs now: 6.3G
```

| | old placement (tmpfs) | new placement (disk) |
|---|---|---|
| cache after one cold build | **169M on /dev/shm** | 170M on disk |
| cold build wall | 100.38s | 84.4s |
| warm build wall | 2.97s (same process/dir only) | 3.73s |
| what the next gate call gets | **nothing** (dir died with the PID) → cold again | the same warm cache |
| per-build tmpfs cost | 169M-1.14G | **0** |

A gate run now adds **0 bytes** to `/dev/shm` by construction, and
`TestMergeBuildGate_GoCacheLeavesNoScratchCache` asserts nothing cache-shaped
appears under the merge-build scratch class.

### Suite pollution: found and fixed while measuring

The first targeted run of the new tests wrote **110M into the operator's real
`~/workspace/gopi-build-cache/<repo-key>/`** — one throwaway key per test, 13
keys in minutes. A test run must never populate the operator's cache (same rule
as the notify sink and the ledger). Fixed with a suite-wide sandbox:
`TestMain` pins `GOPI_MERGE_BUILD_GATE_GOCACHE_ROOT` at a throwaway DISK-backed
directory (the suite's own scratch root is tmpfs on this host, so candidates are
MEASURED and a RAM one is skipped), removes it after `m.Run`, and
`TestSuiteBuildCacheSandboxIsPinned` guards the pin. The pollution was removed.

The same pin closed a **pre-existing** suite failure on master (found while
measuring, repaired here): `TestMergeBuildGate_SharedModCacheResolvesWithProxyOff`'s
cold-cache negative control asserted `mergeBuildGateStatusFail` + a
`go: downloading ` line, but the classification half of the same epic
(`mergeBuildGateUnresolvedModules`, master `1c320ddc`) now maps exactly that
output to an **ABSTAIN**. The control could therefore never pass on master; it
now asserts the classification the gate actually reports (still proving the cold
cache cannot resolve, which is the control's job), and pins a cold build cache so
the shared warm one cannot answer the compilation before module resolution is
reached.

## Red-proof (the regression test fails against the old placement)

Temporarily restoring the OLD code path (per-process tmpfs scratch cache) inside
`mergeBuildGateGoCache`:

```
--- FAIL: TestMergeBuildGate_GoCacheLeavesNoScratchCache
    the gate created /dev/shm/gopi-test-scratch/queue/queue-runtime-merge-build-<pid>/gocache
    under the tmpfs scratch class — the per-process tmpfs cache (726M-1.14G per
    build) is back
--- FAIL: TestMergeBuildGate_GoCacheIsOffShmAndOffRootFS
    the gate's cache …/queue-runtime-merge-build-<pid>/gocache is under /dev/shm —
    the whole point of this change is that it is on DISK
FAIL
```

Restoring the fix → both green. The regression is pinned.

## Tests added

`internal/tools/merge_build_gate_gocache_test.go`

| test | pins |
|---|---|
| `…GoCacheLeavesNoScratchCache` | nothing cache-shaped under `RuntimeScratchDir(MergeBuild)`; cache not inside the scratch tree (fails on the old placement) |
| `…GoCacheIsOffShmAndOffRootFS` | measured: not on a memory filesystem, not on `/`'s device |
| `…GoCacheRefusesMemoryFilesystemRoot` | a RAM cache root ⇒ ABSTAIN (mirror of the `/` arm; the memory filesystem is DISCOVERED by measurement, so the arm skips with its classification on a host that has none) |
| `…GoCacheRefusesScratchTreeRoot` | a cache root inside the collected scratch tree ⇒ ABSTAIN, at the home itself and nested under it (added 2026-09-18 — see `devdoc/scratch-home-block-backed-red.md`) |
| `…GoCacheIsSharedAcrossProcesses` | two calls resolve ONE path, and the path carries no per-PID scratch shape |
| `…GoCacheSeparatesRepos` | same basename in two repos ⇒ two caches |
| `…GoCacheBoundIsEnforced` | boundary is exact (`> bound`), trim drops the body, `log.txt`/`trim.txt` survive |
| `…GoCacheAbsentRootAbstains` | uncreatable / relative / unresolvable-home / on-`/` roots ⇒ ABSTAIN with a log line, never a substituted path |
| `…GoCacheUnderTheWorktreeRoot` | default placement is `<worktree root>/gopi-build-cache/<repo-key>/gocache` |

`internal/tools/testmain_test.go`: `TestSuiteBuildCacheSandboxIsPinned` — a
gate build inside this suite must never fall back to the operator's real
`~/workspace/gopi-build-cache` (the same shape as the suite's notify/profile
hermeticity guards).

`internal/scratch/rootgocache_test.go`: the leaf alias equality with
`worktreeroots`, the `/` refusal naming its remedy, the positive control (a disk
path off `/` is allowed), the unmeasurable-path refusal, and the tmpfs refusal.

## Rule audit

* **Rule 22** (temp workspaces off `/`, criterion not value): placement is
  decided by measured `statfs` device/type, and the refusals quote them.
* **Rules 1/13/14** (no bare literals; one source of truth): env name
  (`scratch.GoCacheRootEnvName`), leaf name
  (`worktreeroots.SharedBuildCacheDirName` + pinned alias), magic numbers
  (`aligns` … all named consts; statfs magics exported once from
  `internal/scratch`), the bound, the hex-shard predicate, the human-byte units.
* **Rule 15b** (no silent failure): an uncreatable/unmeasurable/on-`/`/on-tmpfs
  root ABSTAINS with the path and reason in the merger log; the size report,
  trim, un-droppable shard and unmeasurable cache are all logged.
* **Rule 25** (no privilege): nothing here needs one.
* **Dependency preflight**: stdlib only (`crypto/sha256`, `encoding/hex`,
  `syscall` already in use in this package).
