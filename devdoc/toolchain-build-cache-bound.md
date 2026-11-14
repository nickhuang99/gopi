<!-- doc-class: historical -->
# The agent-side Go toolchain cache: a bound, and the headroom question `/` cannot answer

**Task**: `01a12ff2-a6a8-7af8-a664-c1c51dc7676c` · branch `task/01a12ff2a6a87af8a664c1c51dc7676c`

## The measurement that produced this work

Read-only measurements on `hostb-Laptop` (2026-10-12), no deletion performed:

| what | value |
|---|---|
| `/home` | `/dev/nvme0n1p7`, device **66309**, 458 GiB, **19 GiB free — 96% full** |
| `/` | `/dev/nvme0n1p5`, device **66307**, 183 GiB, 131 GiB free — 25% |
| `~/.cache/go-build` | **121 GiB**, 368 605 files, device 66309, ext4 |

The partition that filled is `/home`. The cache that filled it is the Go
**toolchain** cache — the one `go` itself writes (the agent side's own compiles),
not the merge gate's per-repo cache.

## Why the existing machinery did not see it

Both existing cache checks ask the right question and both answer *"not my
problem"* here:

- `scratch.AssertGoCacheOffRootFS` (`internal/scratch/rootgocache.go`) — *is the
  cache on the same filesystem as `/`?* **No.** `/home` is a different device from
  `/`. Rule 22's criterion is a criterion **about `/`**, and `/` was at 25% the
  whole time.
- `scratch.AssertGoCacheOffShm` — *is it on RAM?* **No.** It is on ext4.
- `merge_build_gate_gocache.go` bounds the **merge gate's own** per-repo cache at
  2 GiB and never touches `~/.cache/go-build` — by an explicit decision ("a merge
  probe must not fill the user's cache"). Its bound is per repo key, not per host.

So the number that would have made the exhaustion visible — **free bytes on the
filesystem that HOLDS the cache** — was measured by nothing. That is the hole.

## What this change adds

### `internal/scratch/toolchain_cache.go` (new)

| symbol | what it does |
|---|---|
| `MeasureToolchainCache` | measures a cache: **body** (bytes across the two-hex entry shards — the number a bound compares), whole-tree totals, an age-classified shard list, which toolchain bookkeeping files are at the root, and the **filesystem that holds it** (device, type by statfs magic, total, free) |
| `CheckToolchainCache` | the criterion: body vs the **bound**, free space vs the **headroom floor**; either arm returns a sentence naming the path, the filesystem, the measured bytes and the way out |
| `EvictToolchainCache` | brings the body to a target by removing whole entry shards **oldest first** (ordering key = the shard's *newest* entry mtime) |
| `ResolveToolchainCacheMaxBytes` | the **one** reader of the bound knob (explicit value > `GOPI_BUILD_CACHE_MAX_GIB` > default) |
| `ResolveToolchainCachePath` | explicit path > `GOCACHE` (the toolchain's own precedence — never a second one) > `<user cache dir>/go-build` |
| `FilesystemTypeName`, `FilesystemCapacity`, `PathDeviceNumber` | the measurement primitives (statfs magic → name; `Blocks`/`Bavail` total & free; stat(2) device number) |

Constants: `DefaultToolchainCacheMaxBytes` = 25 GiB, `DefaultToolchainCacheHeadroomBytes`
= 5 GiB, `ToolchainCacheEvictMinAge` = 24h — each carries its measured *why* in
the source, including why the bound is deliberately loose (the merge gate's first
512 MiB bound was raised to 2 GiB because a bound that fires on healthy work makes
every build cold).

### Three invariants of the eviction (each is a way it could destroy something)

1. **Only two-hex shard DIRECTORY names are candidates.** The toolchain's
   bookkeeping (`README`, `trim.txt`, `log.txt`, `CACHEDIR.TAG`) are root-level
   **files** and unreachable by construction — removing them leaves a directory the
   toolchain no longer recognises as its cache.
2. **A 24h grace window.** A shard whose *newest* entry is younger is never a
   candidate, so a build that ran minutes ago (or was killed with warm entries)
   keeps its entries. (The toolchain's own trim waits 5 days — an operator at 96%
   cannot.)
3. **The root is never removed**, at any size or target.

### `gopi tool scratch-gc gocache` (new arm)

```
gopi tool scratch-gc gocache [--path DIR] [--trim] [--max-gib N] [--headroom-gib N]
```

- **Read-only by default.** It reports and prints the verdict to stderr; it
  deletes **nothing**. Rule 41: the toolchain cache is the operator's *shared,
  cross-project* asset, so the finder of an exhaustion proposes and the operator
  decides. `--path` is what makes the arm live-testable against a fixture instead
  of the real 121 GiB.
- **`--trim` is the explicit reclaim** — the only deleting face — and it is
  **withheld, and says so**, when the host's sweeps are disabled (the cadence
  knob): a disabled sweep must not delete on the strength of a flag.
- The before/after pair is printed **verbatim** on its own line, and the body is
  **re-measured** after the trim rather than derived by subtraction.

### The knob

`GOPI_BUILD_CACHE_MAX_GIB` (env-only; `--max-gib` outranks it; default 25) —
catalogued in `internal/config/settings.go`'s `EnvVars` table (so it has a
`gopi --help-env` face) and documented in `CONFIG.md` both in the §2c flag table and
the §k env table, each row carrying a resolving `file.go:NNN` reference the CONFIG.md
gate checks. The §2c inventory ratchet went `72 → 73` in the same change, with its
audit-trail entry — the precedent `GOPI_HOLDER_SCAN_DEADLINE_SEC` set.

## Not done, honestly

- **Nothing of the operator's 121 GiB was deleted.** The reclaim is the operator's
  call; this change ships the measurement, the criterion and the explicit verb.
  The reclaimable figure is reported (`reclaimable=` / `--dry-run`).
- **The 174 per-PID `~/.cache/gopi-tools-suite-build-cache-<pid>` directories**
  (93 MiB) are **not** covered here: they are created by the test suite's own
  `GOCACHE` sandbox and are a *tool-cache*, not the toolchain cache. They are
  filed as their own observation rather than swept silently in this change.
- No scheduling, no daemon, no cron, no timer was added (Rule 35): the arm rides
  the verb the host watchdog already executes, and the measurement is read-only.

## Verification

See `devdoc/`-adjacent task report (`$GOPI_TASK_REPORT_PATH`) for the verbatim
`Ran:`/`Result:` evidence, including the two defects the real run caught
(Rule 31): a mislabeled `freed=` field on the report line, and a device number
printed as the statfs **Fsid** — a number no external tool prints, therefore
uncheckable — which the report now prints as the stat(2) device number
(`stat -c %d`).
