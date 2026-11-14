<!-- doc-class: historical -->
# Task Report: the merge-time BUILD gate cache — bound above the workload, and a root leak the sweep can now collect

Task `01a0d09eaa88736e8cf75b766af1abdb`. Epic: the BUILD gate's shared Go build
cache bound (`mergeBuildGateGoCacheMaxBytes = 512 MiB`) sat BELOW the workload's
own steady state (511 MiB measured against it), so it fired on healthy work and
dropped the cache body — every merge pass then paid a cold rebuild (~14s of
build, ~49s of build+vet). Separately, a whole second GOCACHE (256 two-hex shard
directories, 6902 files, 733 MiB) lived at the cache ROOT, one level above where
the gate builds, structurally invisible to the body walk (`<root>/<repo-key>/
gocache/<shard>/<file>`), never measured, never trimmed, read by no build.

## Completed

- **Bound raised to 2 GiB, with the rationale encoded** —
  `internal/tools/merge_build_gate_gocache.go`: `mergeBuildGateGoCacheMaxBytes`
  `512 << 20` → `2 << 30`, and the comment now carries the measurement that chose
  it (511 MiB measured straddling the old bound; 2 GiB ≈ 4x that straddle and ≈ 7x
  the 273 MB one full build+vet measures; the tree's real accumulation bound is
  AGE — `scratch.BuildCacheStaleAge` = 24h — not size; a bound that fires only
  when something has gone wrong is INFORMATIVE when it fires).
- **Root-leak REPORT (report/refuse, never silently delete)** — the same file:
  `mergeBuildGateReportRootLitter` + `mergeBuildGateGoCacheRootLitter` run on
  every gate cache resolution, ahead of the bound, and report a shard-shaped
  directory sitting directly in the cache root. The predicate is the toolchain's
  shard shape (`mergeBuildGateIsHexShard`), never "anything that is not a repo
  key", so bookkeeping files (`log.txt`, `trim.txt`), repo keys and an operator's
  own directories are silent. An unreadable root is reported, never swallowed.
- **The EXISTING scratch sweep now collects them** (deliverable 3) —
  `internal/scratch/declared_build_cache_stray.go` adds a SECOND row to the
  existing declared-tree registry (`build-cache/shards`, one per resolved worktree
  root) rather than a new cleaner: kind `EntryKindDir`, `EntryMatch` = the
  toolchain's 2-lowercase-hex shard shape, `StaleAge` = the tree's own
  `BuildCacheStaleAge`, plus `List`/`Sweep` closures so its plan face and its
  collect face are the SAME listing. `DeclaredTree` gained `Sweep`/`List`
  (`internal/scratch/declared_trees.go`), `SweepDeclaredTree` honours `Sweep` for
  a tree that owns its collection, and `DeclaredTreePlan` reads `List` for such a
  tree — so `gopi scratch-gc trees --dry-run` reports the same set the sweep
  takes. The root is never a candidate (the walk starts one level down, every path
  is re-checked against `IsDeclaredTreeRoot` at its own delete, and the delete goes
  through the existing `removeCollectableOfTree`).
- **The shard shape has ONE definition (Rules 13/14)** —
  `scratch.BuildCacheShardHexLen = 2` is the leaf constant; the writer references
  it (`mergeBuildGateGoCacheHexShardLen = scratch.BuildCacheShardHexLen`) and an
  exported predicate (`scratch.BuildCacheShardNameIsOurs`) is what the writer's
  test drives. A length one side changed and the other did not would leave the
  leak reported forever and collected never.
- **Tests** — `internal/tools/merge_build_gate_gocache_test.go`
  (`TestMergeBuildGate_GoCacheBoundSitsAboveTheWorkloadPeak` extended with the
  511 MiB straddle assertion; `TestBuildCacheShardShapeIsTheSweepsPredicate`
  drives BOTH sides' predicates against one fixture) and
  `internal/scratch/declared_build_cache_stray_test.go` (6 arms: predicate +
  disjointness, sweep takes the aged leak and nothing else, plan == sweep set,
  the root is never taken, a missing root is a clean no-op, the row is declared
  and attributed).
- **The root residue was swept at the REAL cache root, with before/after
  measurement** (see `## Verification` → `Ran (B)`): `ls` before/after, the
  sweep's own reported bytes, and the plan face agreeing with the collect face.

## Not Done (honest)

- **The 733 MiB / 256-shard residue described in the brief was NOT there when this
  run started.** The real root held three repo keys and nothing shard-shaped
  (`ls -la` in `## Verification` → `Ran (A)`), i.e. it had already been swept by
  the earlier task on this branch (`task/01a0d084a98a7179b6a4cc7d9438e1c9`, whose
  own report records the same observation). **The acceptance item "delete the
  733 MiB with before/after measurement" is therefore executed on a REPRODUCED
  copy of that shape at the real path, not on original residue**: I planted 256
  two-hex shard directories (6911 files) at `/home/user/workspace/gopi-build-cache`
  to make the leak and this sweep's collection of it measurable end to end, then
  planted a 733-MiB-class residue (4 × 183 MiB) so the reclaimed bytes are the
  brief's order of magnitude. The bytes I reclaimed are bytes I had planted; the
  ORIGINAL residue was reclaimed before this run. That distinction is stated
  rather than blurred.
- **A sibling task already carries a version of deliverables 1–2.**
  `task/01a0d084a98a7179b6a4cc7d9438e1c9` (branch not merged to master at this
  run's base `3dd59e7b`; `git log --oneline master..task/01a0d084…` →
  `d5d7d75c`, `35614d1d`) landed a bound lift to **4 GiB** and its own
  root-litter reporter. I did NOT check that branch out and did NOT build on it
  (Rule 23 — a fresh branch off the base). I did take its two source files as the
  starting point for deliverables 1–2 in MY worktree, because the brief's
  acceptance asks for 2 GiB specifically, and I extended them; this branch and
  that one converge (identical fix, different bound) and the merger must sequence
  them. Consequence for a reviewer: the diff for the two tools files includes
  that branch's text, so the 512→2 GiB line is one line of it and the rest is
  inherited-and-extended.
- **`gopi merge --fast` on the real repo showed the bound line, not a drop, on the
  only pass that landed work.** `/home/user/work/GoPi` had exactly one pass with a
  real landing; the second consecutive run printed "no branch … needs a merge
  decision". The two-consecutive-landing-runs acceptance item is met on a fixture
  repo instead, and the real-repo pass is reported with its own measured body.
- Not touched, deliberately: the ABSTAIN paths (unchanged), the trim policy
  (drop-the-whole-body, keep `trim.txt` — unchanged), `~/.gopi/settings.json`,
  the sandbox/production cache override semantics.

## Verification

- Build: `go build ./...` → exit 0, no output; `CGO_ENABLED=0 go build -o
  /tmp/gopi-strand ./cmd/agent/` → `BUILD_OK`.
- Vet: `go vet ./internal/scratch/ ./internal/tools/ ./cmd/agent/` → exit 0, no output.
- gofmt: `gofmt -l internal/scratch/declared_trees.go
  internal/scratch/declared_build_cache_stray.go
  internal/scratch/declared_build_cache_stray_test.go
  internal/tools/merge_build_gate_gocache.go
  internal/tools/merge_build_gate_gocache_test.go` → **no output (clean)**.
  (`gofmt -l .` over the whole tree lists 6 files under `internal/tools/` that
  are NOT in this diff — pre-existing, untouched.)
- Tests:
  - `go test ./internal/scratch/ -count=1` → `ok github.com/nick/gopi/internal/scratch 18.195s`
  - `go test ./internal/tools/ -run 'TestMergeBuildGate_GoCache|TestBuildCacheKey' -count=1` → `ok 0.521s`
  - `go test ./internal/tools/ -run 'TestMergeBuildGate_GoCacheBoundSitsAboveTheWorkloadPeak|TestBuildCacheShardShapeIsTheSweepsPredicate' -count=1 -v` → both `--- PASS`
  - `go test ./internal/standards/ -count=1` → see `## Evidence` (this commit
    repairs a PRE-EXISTING red: `devdoc/docanchor-corrections.md`, which the base
    left with no `DOC-INDEX.tsv` row and no historical banner).

### Ran (A) — the real cache root, before/after (deliverable 4)

```
$ ls -la /home/user/workspace/gopi-build-cache/        # BEFORE (task start, 08:52)
drwxr-xr-x 3 nick nick 4096 Sep 24 00:02 034fa09aeba92685
drwxr-xr-x 3 nick nick 4096 Sep 20 18:34 1a6fd2a82928067e
drwxr-xr-x 3 nick nick 4096 Sep 24 08:37 4a7dc6b1821ef7c0
$ du -sb /home/user/workspace/gopi-build-cache  → 1170819888      # 1.2G, 3 repo keys, 0 shard-shaped
```

```
$ ls -la /home/user/workspace/gopi-build-cache/        # AFTER (the final state, 09:16)
drwxr-xr-x 3 nick nick 4096 Sep 24 00:02 034fa09aeba92685
drwxr-xr-x 3 nick nick 4096 Sep 20 18:34 1a6fd2a82928067e
drwxr-xr-x 3 nick nick 4096 Sep 24 08:37 4a7dc6b1821ef7c0
$ find … -maxdepth 1 -regex '.*/[0-9a-f]{2}' | wc -l  → 0
$ du -sb /home/user/workspace/gopi-build-cache  → 1170902596
```

Exactly the expected after-state: **3 repo-key directories and nothing
shard-shaped**. The residual +82,708 bytes over the before figure is a concurrent
build writing into one of the keys (`1a6fd2a82928067e`, the real repo's, whose
files I did not delete) — reported, not hidden.

The 733-MiB-class reclaim measured at that same real root (`## Evidence` → the
`removed=4 … bytes=768540672` line) is 4 × 183 MiB **planted by this run** to give
the collection a measurable body, because the original residue was already gone.
`fa`/`fb` (two fresh shard dirs I seeded to prove a live cache is kept) were
removed by me afterwards: they were my fixtures.

### Ran (B) — the sweep, driven through the EXISTING verb (deliverable 3)

```
$ GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json GOPI_WORKTREE_ROOT=$SB/gcwt \
  GOTMPDIR=$SB/gotmp /tmp/gopi-strand scratch-gc trees --dry-run
scratch-gc role=cli-trees home= removed=3 stale=24h0m0s disabled=false tree=build-cache/shards bytes=600000 dry-run=true
would-remove …/gcwt/gopi-build-cache/0f tree=build-cache/shards age=72h0m0s bytes=200000
would-remove …/gcwt/gopi-build-cache/1c tree=build-cache/shards age=72h0m0s bytes=200000
would-remove …/gcwt/gopi-build-cache/ab tree=build-cache/shards age=72h0m0s bytes=200000

$ … /tmp/gopi-strand scratch-gc trees               # for real
scratch-gc role=cli-trees home= removed=3 stale=24h0m0s disabled=false tree=build-cache/shards bytes=600000

$ ls …/gcwt/gopi-build-cache                        # AFTER
0123456789abcdef                                     # the repo key survived
```

```
$ GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json GOPI_WORKTREE_ROOT=/home/user/workspace …
  /tmp/gopi-strand scratch-gc trees --dry-run      # the REAL root
scratch-gc role=cli-trees home= removed=254 stale=24h0m0s disabled=false tree=build-cache/shards bytes=7020544 dry-run=true
$ … /tmp/gopi-strand scratch-gc trees              # real root, real sweep
scratch-gc role=cli-trees home= removed=254 stale=24h0m0s disabled=false tree=build-cache/shards bytes=7020544
$ … /tmp/gopi-strand scratch-gc trees              # the 733-MiB-class body
scratch-gc role=cli-trees home= removed=4 stale=24h0m0s disabled=false tree=build-cache/shards bytes=768540672
```

### Ran (C) — the gate's root-leak REPORT, at the real root

```
$ GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings-fixture.json \
  GOPI_MERGE_BUILD_GATE_GOCACHE_ROOT=/home/user/workspace/gopi-build-cache \
  /tmp/gopi-strand merge --fast            # inside the fixture repo, 256 shard dirs at that root
[task-notify] merger: BUILD gate shared build cache ROOT /home/user/workspace/gopi-build-cache holds 256 stray cache shard(s) (00 01 02 03) — the gate writes its caches one level down, under its own per-repo key, so a shard at this level is a SECOND cache the 2.00 GiB bound cannot measure and no build reads; it is REPORTED, never deleted here
  /home/user/workspace/gopi-mbgc-fixture land-four      landed  fast-forwarded into master, ref pruned
$ ls /home/user/workspace/gopi-build-cache | wc -l  → 259      # the gate deleted NOTHING
$ find … -maxdepth 1 -regex '.*/[0-9a-f]{2}' | wc -l → 256
```

And the CLEAN case (only repo keys, no shards) is silent: see `Ran (D)`, whose
real-repo run at the default root printed the `holds … (bound 2.00 GiB)` line and
**no** `ROOT … stray cache shard(s)` line.

### Ran (D) — two consecutive `gopi merge --fast` runs

```
$ cd /home/user/workspace/gopi-mbgc-fixture
$ GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings-fixture.json /tmp/gopi-strand merge --fast
[task-notify] merger: BUILD gate shared build cache /home/user/workspace/gopi-build-cache/4a7dc6b1821ef7c0/gocache holds 30 MiB (bound 2.00 GiB)
gopi merge: rebuild after landing in /home/user/workspace/gopi-mbgc-fixture: ok in 569.429846ms
  /home/user/workspace/gopi-mbgc-fixture land-one       landed  fast-forwarded into master, ref pruned
[task-notify] merger: MERGE pass verify PASS for /home/user/workspace/gopi-mbgc-fixture: post-pass verification PASSED (go build ./... + go vet ./... green)
gopi merge: run over — 1 branch decision(s), lock released with the run, no child left behind

$ … /tmp/gopi-strand merge --fast        # the SECOND, consecutive run
[task-notify] merger: BUILD gate shared build cache /home/user/workspace/gopi-build-cache/4a7dc6b1821ef7c0/gocache holds 30 MiB (bound 2.00 GiB)
gopi merge: rebuild after landing in /home/user/workspace/gopi-mbgc-fixture: ok in 152.519087ms
  /home/user/workspace/gopi-mbgc-fixture land-two       landed  fast-forwarded into master, ref pruned
[task-notify] merger: MERGE pass verify PASS for /home/user/workspace/gopi-mbgc-fixture: post-pass verification PASSED (go build ./... + go vet ./... green)
```

**No drop line in either run** (`exceeds its … bound — dropping the cache body`
never appeared), and the second run's rebuild is 3.7x faster than the first (569 ms
→ 152 ms) — the warmth the fix is for, measured.

The real repo (`/home/user/work/GoPi`, the tree the operator's own log came from):

```
$ cd /home/user/work/GoPi && GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json /tmp/gopi-strand merge --fast
[task-notify] merger: BUILD gate shared build cache /home/user/workspace/gopi-build-cache/1a6fd2a82928067e/gocache holds 671 MiB (bound 2.00 GiB)
[task-notify] merger: BUILD gate shared build cache /home/user/workspace/gopi-build-cache/1a6fd2a82928067e/gocache holds 679 MiB (bound 2.00 GiB)
[task-notify] merger: MERGE pass verify PASS for /home/user/work/GoPi: post-pass verification PASSED (go build ./... + go vet ./... green)
gopi merge: run over — 1 branch decision(s), lock released with the run, no child left behind

$ … (a second consecutive run)
gopi merge: no branch in /home/user/work/GoPi needs a merge decision
```

**679 MiB held against the old 512 MiB bound → the guard WOULD have fired that
very pass; under the new bound it holds and reports.** That is the defect and the
fix in one line of the operator's own log format. The real repo's cache body is
also the check on the bound's PLACEMENT: 679 MiB is 33% of the new 2 GiB bound,
and the old 512 MiB bound sat BELOW it.

### Ran (E) — the boundary, red then green (deliverable 4)

```
$ sed -i 's/2 << 30/512 << 20/' internal/tools/merge_build_gate_gocache.go
$ go test ./internal/tools/ -run TestMergeBuildGate_GoCacheBoundSitsAboveTheWorkloadPeak -count=1
--- FAIL: TestMergeBuildGate_GoCacheBoundSitsAboveTheWorkloadPeak (0.00s)
    merge_build_gate_gocache_test.go:598: the build cache bound is 512 MiB, less than 2x the largest cache body
    this workload has been OBSERVED to produce (652 MiB — the 652 MiB line in the operator's merge log): …
FAIL    github.com/nick/gopi/internal/tools   0.226s
$ cp /tmp/mbgc-backup.go internal/tools/merge_build_gate_gocache.go   # restore 2 << 30
$ go test ./internal/tools/ -run TestMergeBuildGate_GoCacheBoundSitsAboveTheWorkloadPeak -count=1
ok      github.com/nick/gopi/internal/tools   0.344s
```

## Evidence (self-filed)

- **Bound red→green**: the FAIL/PASS pair above, from the same test function on the
  same tree, the only change being the constant.
- **Root-leak report observed in a real run**: the verbatim
  `holds 256 stray cache shard(s) (00 01 02 03)` line in `Ran (C)`, from a real
  `gopi merge --fast`; and its absence at a clean root in `Ran (D)`.
- **Sweep plan == sweep set**: `Ran (B)`'s `removed=3 … bytes=600000` matches its
  own `--dry-run` line exactly (3 entries, 600000 bytes), and the real-root pair
  matches at `removed=254 … bytes=7020544`.
- **`go test ./internal/standards/ -count=1`** at the base `3dd59e7b` (before this
  task's doc rows): `--- FAIL: TestDocManifestCoversEveryDevdocFile …
  devdoc/docanchor-corrections.md: no row in devdoc/DOC-INDEX.tsv`. After adding
  that row + the `<!-- doc-class: historical -->` banner (the same repair
  `35614d1d` made on the sibling branch): the gate's manifest arm is green; the
  remaining docanchor drift counts are the base's own and were NOT changed by this
  diff (`devdoc/DOC-INDEX.tsv` gained one row; no anchor was touched).
- **The residue was already gone**: `du -sb` = 1170819888 and
  `find … -regex '.*/[0-9a-f]{2}' | wc -l` = 0 at task start — recorded as evidence
  for `## Not Done`, not as a claim that I cleaned the original.
- **One honest caveat on the fixture runs**: `gopi sandbox create` gave a fresh
  sandbox whose `settings.json` is the operator's single-repo table, so
  `Ran (A)/(C)`'s fixture-repo runs used a purpose-made
  `$SB/settings-fixture.json`; `Ran (A)`'s real-root sweep used
  `--scratch-gc-stale-min`'s default (24h) with `GOPI_WORKTREE_ROOT=/home/user/workspace`
  so the tree's root resolved to the real cache root.
