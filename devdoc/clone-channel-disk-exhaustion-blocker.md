<!-- doc-class: normative -->
# A full disk is not a code failure — the clone channel's own blocker kind for ENOSPC

## 1. The defect, measured

Every clone-channel run compiles inside its ground under `/home/user/workspace`
(`/dev/nvme1n1p1`, 3.6 TiB). On 2026-10-31 that filesystem had **2.3 GiB free**,
and the declared build died with the kernel's own text:

```
/usr/local/go/pkg/tool/linux_amd64/link: writing /home/user/workspace/x/a.out: no space left on device
```

The channel classified it as `BlockerKindBuild` — the same kind, and the same
sentence, as `undefined: foo`. So the row said *the build step refused*, the next
round re-ran the same build on the same bytes and got the same refusal, and the
agent went looking for a defect in a revision **that had never been linked**.
Nothing proposed reclaiming anything, because no sentence named a filesystem.

## 2. What actually holds the space (the two facts, separated)

Measured in this run (raw dump: `evidence/01-facts.txt` in the run's session
directory):

```
$ df -B1 / /home/user/workspace
Filesystem         1B-blocks          Used   Available Use% Mounted on
/dev/nvme0n1p2 1967317549056 1820485279744 46822449152  98% /
/dev/nvme1n1p1 3936818806784 3674492653568 62270124032  99% /home/user/workspace

$ stat -c '%d %n' / /home/user/workspace
66308 /
66305 /home/user/workspace

$ go env GOCACHE
/home/user/workspace/.gocache

$ gopi tool scratch-gc gocache
… holds 560.2 GiB, over its 25.0 GiB bound (436.3 GiB of that is reclaimable now
at the cache-entry grain, and 1173233 of its entries are past the 24h0m0s grace window)
… path=/home/user/workspace/.gocache device=66305 fs=ext4 free=57.9 GiB
   total=3666.4 GiB body=560.2 GiB all=569.9 GiB … bound=25.0 GiB grain=cache-entry
   reclaimable=436.3 GiB reclaimable_entries=1173233
```

**The two facts do not share a number.** `/` and `/home/user/workspace` are two
devices (`66308` vs `66305`); the task grounds live on the second one and are a
rounding error beside the cache — the ten largest directories under the workspace
root in the same session were `llvm-project` 256 G, `doubao_dialog` 63 G,
`_tmp` 21 G, and then individual grounds at 139–317 M each. The Go **toolchain
cache alone is 560.2 GiB of a 3.6 TiB filesystem**, and 436.3 GiB of it is
reclaimable *right now* at the cache-entry grain. Reclaiming task grounds is not
a disk-space remedy; bringing the cache back under its bound is.

## 3. The fix: one kind, one sentence, one copy-pasteable command

`BlockerKindDiskExhaustion` (`internal/devworkspace/disk_exhaustion.go`), decided
in `buildBlocker` — the ONE place the build step's two outcomes are separated:

- **vocabulary is NOT restated.** `internal/scratch` already owns
  `DiskExhaustionMarkers` and `DiskExhaustionDiagnosticShape` (the merge-time
  build gate's classification). This file imports them (Rule 43/14): a second
  copy would drift, and the surface that stopped matching would report a full
  disk as a code failure again.
- **the diagnostic shape wins.** An output carrying `file:line:col:` was
  *compiled*, so it stays `BlockerKindBuild` whatever else it mentions — a repo
  whose declared build runs a test suite can print the disk phrase inside a
  failing test's message while the compile error is the real defect. Guarded by
  `TestBuildBlockerKeepsTheCodeClassWhenADiagnosticIsPresent`.
- **the report measures at render time.** `diskExhaustionReport` stats the
  ground's filesystem (read-only, no subprocess — Rule 35) so the reader sees the
  figures for the path the build really ran in, never a stale absolute and never
  a healthy `/` beside a full `/home` (the merge gate's 2026-10-12 lesson).
- **the decisive line is the marker.** `kindDecisivePatterns[BlockerKindDiskExhaustion]`
  is DERIVED from `scratch.DiskExhaustionMarkers`, so the 200-rune ledger summary
  carries `no space left on device` rather than the channel's preamble.
- **the round is told the right fact.** `deliveryRetryBriefing` gives this kind
  its own opening: the revision was never linked, the reclaim command, and
  「不要去找代码里的缺陷」.

The reclaim itself is the operator's act, by design: the cache is a shared
cross-project asset and the arm is report-only. This change never trims, never
deletes, and never changes a byte outside its own ground (Rule 42).

## 4. The command that reclaims it (operator decision)

```
gopi tool scratch-gc gocache            # report first: body, bound, reclaimable
gopi tool scratch-gc gocache --trim     # evicts whole cache ENTRIES oldest first,
                                        # keeping any shard whose newest entry is
                                        # inside the 24h grace window
```

**Consequences, measured — read before running it.** On today's cache the trim
would free up to **436.3 GiB** (1 173 233 entries past the 24 h grace window) and
would take `/home/user/workspace` from 99 % back to roughly 82 % used. It is
irreversible in the sense that matters: a Go build cache entry is a *derived*
artifact — the next compile of the same package re-computes it (slower, not
broken) — but there is no undo, so a cache trimmed on a busy day costs the
following builds their warm path. Nothing else reads that directory.

## 5. What this does NOT claim

It does not shrink the cache automatically, does not change `GOPI_BUILD_CACHE_MAX_GIB`
(the 25 GiB bound stays), and does not decide that task grounds should be swept.
It only makes an ENOSPC refusal *say what it is*.
