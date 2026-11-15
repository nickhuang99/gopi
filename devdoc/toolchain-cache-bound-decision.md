<!-- doc-class: historical -->
# The toolchain-cache bound: a report that acts on nothing, on purpose — and the decision it hands to the operator

Task `01a146ff-8c02-72cf-8713-d2b41bbbc614`. Files:
`cmd/agent/cli_scratch_gc_gocache.go` (+ `_test.go`). Leaf:
`internal/scratch/toolchain_cache.go`. Earlier work:
`devdoc/toolchain-cache-entry-grain.md`.

This document exists for ONE reader: whoever next runs `gopi tool scratch-gc
gocache --trim`, sees an over-bound cache, and asks "why did nothing act?" It is
the recorded answer, so that question does not have to be re-derived from the
source header alone.

## 1. The measured overshoot

Read-only, verbatim, on hostb-Laptop, **before** this row's changes
(`gopi tool scratch-gc gocache`):

```
scratch-gc role=cli-gocache path=/home/user/.cache/go-build device=66309 fs=ext4 free=127.1 GiB total=457.3 GiB body=78.3 GiB all=80.0 GiB files=275623 dirs=569 bound=25.0 GiB grain=cache-entry reclaimable=49.9 GiB reclaimable_entries=184489 shard_reclaimable=0 bytes shards=256 shard_candidates=0 trim=false body_after=78.3 GiB entries_removed=0
gocache-bookkeeping path=/home/user/.cache/go-build preserved=[README trim.txt]
```

- **body 78.3 GiB against a 25.0 GiB bound = 3.1×**
- 49.9 GiB reclaimable at the **cache-entry** grain, 184489 entries past the
  24h grace window (`ToolchainCacheEvictMinAge`)
- `device=66309` — the `/home` partition, measured by `stat(2)` device number,
  never by path spelling (Rule 22). `66309` is the partition that reached 100%
  on this host and made a `cmd/agent` suite die with ENOSPC.

Nothing was broken. The bound exists (`DefaultToolchainCacheMaxBytes`, 25 GiB);
the arm measures the overshoot; the grain is the toolchain's own
(`<hex>-a` + `<hex>-d` pair, not a whole two-hex shard); the grace window is
honoured; the arm printed the exact reclaim command. And the cache sat at 3.1×
its bound **across** the exhaustion.

## 2. Why the bound did not act

It is not supposed to. `internal/scratch/toolchain_cache.go`, in its own header:

> IT NEVER DELETES ANYTHING BY ITSELF, and this is a design decision rather than a
> missing feature (Rule 41): the toolchain cache is the operator's shared,
> cross-project asset, and the finder of a defect proposes the reclaim — the
> operator decides when it happens.

The reason, stated so a later reader need not infer it:

1. **The asset is shared and cross-project.** Every repo on the host compiles
   into `~/.cache/go-build`. A reclaim executed by one task's machinery spends a
   resource every other project on the machine is using; the cost lands on builds
   that are not the one that decided.
2. **The cost of over-reclaiming is measured and large.** A cold build of this
   repo's `./cmd/agent` was measured at 84.4s against a warm 3.7s — **22.6×** a
   warm build (recorded 2026-10-12, memory #9800). That is why the bound is set
   where it is (25 GiB, ~12× the merge gate's per-repo bound): a bound that fires
   on healthy work makes every build cold.
3. **Eviction is irreversible and the operator owns the window.** Which entries
   are safe to lose depends on what the machine is about to build — information
   the arm does not have.
4. **Rule 41 in one line**: the finder of a defect files evidence; it does not
   delete the operator's cache. Rule 35's corollary is the same shape — do not
   wrap a bound that already exists in something that acts on its own (no cron,
   no daemon, no timer; the arm rides the verb the host watchdog already runs).

So an over-bound reading is a **finding plus a proposal**, never a failure of the
arm to act. That was previously legible only from the source; §5 makes it legible
from the output.

## 3. The decision

Exactly one of two arms is to be taken. Both are the operator's.

### Arm (a) — reclaim now, on this host

```bash
gopi tool scratch-gc gocache                 # 1. measure (read-only) — the numbers in §1
gopi tool scratch-gc gocache --trim          # 2. reclaim: entries past the 24h window, oldest first, down to the bound
gopi tool scratch-gc gocache                 # 3. re-measure — the row's before/after pair
du -sb ~/.cache/go-build                     # 4. the filesystem's own witness
df -h /home                                  # 5. free space on device 66309
```

What `--trim` does and does not touch, so this is a decision about a known
object:

- **removes**: cache ENTRY files (`<64-hex>-a` / `<64-hex>-d` pairs, together)
  whose newest half is older than the 24h grace window, oldest first, stopping at
  the bound;
- **never removes**: the toolchain's bookkeeping leaves at the cache root
  (`README`, `trim.txt`, `log.txt`, `CACHEDIR.TAG`), any shard DIRECTORY (an
  emptied shard stays an empty directory), the cache root, or a foreign file
  (`<16-hex>-*` repo-key entries, upper-case ids);
- **withholds itself** if the operator has disabled the sweeps (GC cadence knob
  `0`) and says so, rather than deleting on the strength of a flag.

Variants: `--max-gib N` sets this invocation's target; `GOPI_BUILD_CACHE_MAX_GIB=N`
moves the bound persistently (`CONFIG.md`); `--headroom-gib N` moves the
free-space floor.

Do it when the machine is otherwise idle if build warmth matters: 49.9 GiB of
working-set entries is what a warm build reuses.

### Arm (b) — keep it bounded-but-untrimmed, and record the ruling

No command. The ruling to record is:

> A bounded-but-untrimmed cache is ACCEPTABLE: the arm stays report-only, the
> bound stays REPORTED, and no machinery ever reclaims on its own — because the
> cache is a shared cross-project asset whose reclaim is the operator's decision,
> and because the measured cost of an unnecessary cold build is 22.6× a warm one.

This document is the reason, written down. Arm (b) is satisfied by that sentence
being findable next to the arm — which is why §5's sentence names this file.

## 4. Where this leaves the reader

- An over-bound cache is the arm **working**: it measured, judged, named the
  reclaim and stepped back. It is not a defect to be patched by making the trim
  automatic; that change is a separate ruling with its own review.
- The number `reclaimable_entries` × the 24h window is the honest capacity story
  for a live cache: the arm's `reclaimable=` is what a trim WOULD free now, and
  `shard_reclaimable=0 bytes` beside it is the older, coarser grain's reading
  (see `devdoc/toolchain-cache-entry-grain.md`).

## 5. What this row changed (text only, no behaviour change)

The arm printed the numbers and left the reader to infer whose move it was. It now
also prints, on the **report** face, when the cache is over its bound:

```
gopi scratch-gc gocache: the cache is OVER its bound and bringing it back is the
OPERATOR's decision — this arm never reclaims on its own, by design: the toolchain
cache is a shared cross-project asset (every repo on this host compiles into it)
and the eviction is deliberately never automatic. Run `gopi scratch-gc gocache
--trim` when you choose to reclaim it, or lower GOPI_BUILD_CACHE_MAX_GIB
deliberately; the reason the arm stays report-only is recorded in
internal/scratch/toolchain_cache.go and devdoc/toolchain-cache-bound-decision.md
```

Properties that keep the sentence honest, each pinned by a test in
`cmd/agent/cli_scratch_gc_gocache_test.go`:

- printed only when **over the bound** — a cache inside its bound is not a
  decision, and a sentence urging a reclaim there is noise an operator learns to
  skip;
- printed only on the **report** face — a `--trim` invocation IS the operator
  taking the decision, so the arm does not urge the flag that was just run;
- names the **exact command** from the arm's own constants (Rule 1b), so output
  and docs cannot drift;
- names **where the reason lives** (source + this document), so the next reader
  does not re-derive it;
- states the **reason itself** (`shared cross-project asset`, `never automatic`),
  not merely its address;
- carries **no behaviour change**: the reclaim path, the bound, the grain, the
  grace window and the exit contract are untouched — `trim=false`,
  `entries_removed=0` on the read-only face, and the report stays read-only.

## Not done (honest)

- **No trim was run and no cache byte was deleted.** 78.3 GiB of cache (49.9 GiB
  reclaimable) is still on disk at the end of this row. The reclaim is arm (a),
  the operator's; this row had no instruction to consume a shared 49.9 GiB, and
  Rule 41 forbids the finder deleting it to "prove" the bound.
- **Neither arm is closed by this document.** It records the brief for both, with
  the reason, so the ruling can be taken and read back. A ruling is the
  operator's to make and is not manufactured here.
- The **executable-entry** shape (`<hex>-d` as a directory) remains skipped, not
  measured — unchanged by this row, named in
  `devdoc/toolchain-cache-entry-grain.md`.
