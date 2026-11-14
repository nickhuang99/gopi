<!-- doc-class: historical -->
# The Go build cache reclaims at the ENTRY grain, not the shard grain

Task `01a1304317fe7003b4b4f3be07c3124c`. Files: `internal/scratch/toolchain_cache.go`,
`cmd/agent/cli_scratch_gc_gocache.go`, `CONFIG.md`.

## The defect, measured

`gopi tool scratch-gc gocache` measured `~/.cache/go-build` and could evict whole
**two-hex shard directories** on `--trim`. A shard was a candidate only when the
shard's **newest** entry was older than the 24h grace window
(`scratch.ToolchainCacheEvictMinAge`). On a live, still-being-written cache **every**
shard holds a fresh entry, so **no** shard was ever a candidate: the arm reported
`reclaimable=0` at any bound. The diagnosis was correct and the relief was zero —
and a bare `0` reads as "your cache is fine", which is the worst property a
capacity report can have.

Measured on the operator's real cache before the fix (READ-ONLY, never trimmed):

```
body=126.3 GiB all=128.6 GiB bound=25.0 GiB shards=256 shard_candidates=0
```

256 shards, not one candidate, 126 GiB held.

## The fix: the toolchain's own grain

`cmd/go/internal/cache/cmd.go` + `cache.go` reclaim at `entryName`: a cache entry is
the **`<hex>-a` index file plus the `<hex>-d` data file** (or the `<hex>-d`
directory, for an executable output) that live inside a shard, and `trimSubdir`
removes entries whose `-a` file is older than the grace window. That is the grain
this code now measures and reclaims at — the mechanism the toolchain already runs
(Rule 35: prefer what already runs over inventing a second policy).

Two grains now appear side by side, and they are intentionally different numbers
about the same cache:

| field | grain | meaning |
|---|---|---|
| `body=` | entry | bytes in the entry pairs — the number the bound compares |
| `reclaimable=` | entry | bytes past the grace window, at the entry grain — **what a trim frees** |
| `reclaimable_entries=` | entry | how many entries that is |
| `shard_reclaimable=` | shard | the old reading, kept for comparison (0 on a live cache) |
| `shard_candidates=` | shard | the shards the old grain would have removed (0 on a live cache) |
| `grain=cache-entry` | — | the NAME of the grain `reclaimable=` is counted in |

`ToolchainCacheReport.GrainName()` is the single answer to "what grain is this
figure at?" — including for a hand-built zero-value report — so a printed line can
never carry a bare number whose unit the reader has to infer.

## Safety invariants (each one a way this could destroy something the operator needs)

1. **Only cache-ENTRY files are removed.** `ToolchainCacheEntryName` accepts exactly
   a 64-hex id plus `-a`/`-d`; a 16-hex repo key, `README`, `trim.txt`, `log.txt`,
   `CACHEDIR.TAG`, an upper-case id, a foreign `*-d` file are all *not* entries and
   are never candidates.
2. **The grace window moves to the entry grain**, which is what makes the tighter
   grain *safer* than the coarse one: a shard holding one fresh entry survives while
   the cold entries beside it come back.
3. **No shard DIRECTORY is ever removed, and the root never is** — at any age, size
   or target. An emptied shard stays an empty directory.
4. **The pair is removed together** (index + data). An index without its data is a
   cache MISS the toolchain pays on every reuse; a data file without its index is
   unreachable bytes. Only the halves that exist *at that moment* are removed.
5. **The decision is re-read at the delete** (`verifyCacheEntryAtDelete`): an entry
   that has become fresh, or that is gone (the toolchain's own concurrent trim won
   the race), is skipped with a reason — never removed on the strength of a stale
   measurement.

`SignedBytes` (new, `internal/scratch/tmproot.go`) exists because the measured
change in a filesystem's free space can legitimately be negative; rendering it
through the unsigned `FormatBytes` would wrap a small deficit into an absurd
magnitude.

## The "cannot reclaim" sentence

When a cache is **over its bound** and **nothing** is past the grace window, the arm
prints (to stderr, so the logfmt line stays machine-parseable):

```
gopi scratch-gc gocache: NOTHING CAN BE RECLAIMED at the cache-entry grain — the
cache holds 1.1 GiB, over its 1.0 GiB bound, but no entry is older than the 24h0m0s
grace window, so a reclaim would free 0 bytes; the space is held by WORKING-SET
entries (a live still-being-written cache), and neither grain frees them: wait for
entries to age past the window, or lower GOPI_BUILD_CACHE_MAX_GIB deliberately
```

The decision is taken from the **report** (`reclaimable==0 && body > bound`), not
from "did the trim run", so a plain `--trim`-less invocation says it too.

## Measured evidence (in this run's session directory)

Fixture: a cache written by the **real toolchain** (`go build` of a two-file
module into a private `GOCACHE`), then aged 40h with three shards given one fresh
file each (live-style), then one cold `-d` grown to ~1.2 GiB (a single
`head -c … > file`, no loop, no `fsync`, no `oflag=direct` — Rule 27). The fixture
lived at `/home/user/workspace/gocache-live-*` (device `66309`, `/` is `66307`,
Rule 22 honoured) and was deleted afterwards.

```
BEFORE du -sb : 1396458119   avail /home: 3455938560
scratch-gc role=cli-gocache … body=1.3 GiB … grain=cache-entry
   reclaimable=1.3 GiB reclaimable_entries=387 shard_reclaimable=30.8 MiB
   shards=256 shard_candidates=255 trim=true body_after=0 bytes entries_removed=387
gocache-trim … grain=cache-entry before=1395405215 after=0 freed=1395405215 entries=387
gopi scratch-gc gocache: the reclaim freed +1.3 GiB of filesystem space (free 3.2 GiB → 4.5 GiB …)
AFTER  du≈1.0 MB   avail /home: 4852670464     (delta ≈ +1.33 GiB, off /)
```

The **same** cache through the **old** grain has `shard_candidates=255` but
`shard_reclaimable=30.8 MiB` — i.e. the old grain's figure was 1/40th of what the
entry grain frees, and on a cache with *any* fresh entry per shard it was exactly 0.

Real cache, re-measured READ-ONLY after the change:

```
body=126.3 GiB all=128.6 GiB bound=25.0 GiB grain=cache-entry
reclaimable=44.8 GiB reclaimable_entries=142694 shard_reclaimable=0 bytes shard_candidates=0
```

`shard_reclaimable=0 bytes` beside `reclaimable=44.8 GiB` on one line is the whole
defect and the whole fix in one reading.

## Not done

- The real cache was **not** trimmed (44.8 GiB is now known to be available). The
  verb only reclaims on the operator's explicit `--trim`, and this run had no
  instruction to consume 44.8 GiB of the machine's build cache.
- The **executable-entry** shape (`<hex>-d` as a *directory*, used when the
  toolchain stores an executable output) is **skipped, not measured**: the reclaim's
  promise is about files it can measure to the byte. Such a shard's bytes show up in
  `du` but not in `body=`, and they are never candidates. Named here rather than
  silently absorbed.
