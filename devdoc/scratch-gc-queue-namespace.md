<!-- doc-class: historical -->
# scratch-gc: the queue is its own NAMESPACE — non-marker queue children are now collectable

Branch `fix/scratch-gc-queue-namespace` (worktree `/home/user/workspace/gopi-queue-ns`), off master `622f4b0a`.

## Completed

- **Root-cause fix** (`internal/scratch/gc.go`): the queue **container** is the
  namespace declaration, so `queueChildCollectable(...)` — one shared predicate
  used by BOTH `queueSweepChildren` (sweep) and `queuePlanChildren` (`--dry-run`)
  — replaces per-child `IsScratchEntry(name)` marker matching. The
  `!IsScratchEntry(name)` pre-filter is gone from the `SweepHome` queue arm and
  both child loops.
- **Every safety veto kept, in the same order**: age > `StaleAge` (6h) and the
  live-holder veto (`LiveHolders` /proc cwd+exe+maps+fd scan **and** `.holder`
  pid+starttime) still run through the pre-existing `entryCollectable`. The
  namespace widens what is *eligible*, never what is *protected*.
- **The one keep the namespace adds**: a flat entry (not a directory) is not a
  run — verifier `*-verify-report.md`, `.cleanup-<pid>`, commit-msg scratch
  files are never deleted and never shield a sibling (Rule 15).
- **Regression test** (`internal/scratch/gc_queue_subtree_test.go`): the repro
  `TestSweepHomeCollectsUnmarkedQueueChildren` over the three measured shapes,
  plus keeps for flat artifacts / a held child / a fresh child; one pre-existing
  fixture corrected (its "foreign entry" was a *directory*, which the namespace
  deliberately cannot call someone else's).
- **Report + commit** on a fresh branch in a linked worktree, added
  file-by-file (Rule 8).

## The defect (root cause, not a missing marker)

`internal/scratch`'s ownership predicate is `IsScratchEntry(name)`: an entry is
this system's throwaway only if its **name** carries `suite-`, `gopi-suite-tmp-`
or `queue-`. That is the right rule for the scratch **home**, which is shared
with neighbours the sweep must never touch.

It is the wrong rule for `<home>/queue`. The queue (`scratch.QueueDirName`) is
this system's **own declared container**, and it is swept one level down
(`queueSweepChildren` / `queuePlanChildren`, merged in `57386b4c`/`51312e68`).
Inside it the predicate kept asking each **child** to re-declare the namespace
in its own name:

```go
if !IsScratchEntry(entry.Name()) {
    continue // the hole: "not ours"
}
```

Any queue child produced **without** the marker matched nothing, so it was
invisible to the sweep **and to `--dry-run`** — never planned, never deleted.
Measured shapes on this host (2026-09-14): `verify-01a0a248-modcache-manual`
(415 MB), `accept`, `test-budget`. Adding one more marker string per producer
would leave the hole open for the next producer; the namespace declaration
closes it for all of them, because a new child is in-namespace by construction.

## The fix

`internal/scratch/gc.go`: one predicate, two faces.

```go
func queueChildCollectable(path string, isDir bool, now time.Time, olderThan time.Duration, holders []string) bool {
	if !isDir {
		return false // an artifact, not a run: kept (and never a shield)
	}
	return entryCollectable(path, now, olderThan, holders)
}
```

- `queueSweepChildren` and `queuePlanChildren` both call it — the plan can never
  promise what the sweep refuses, and vice versa.
- The **namespace widens what is eligible, never whether it is protected**: the
  age veto (`StaleAge`, 6h) and the **live-holder veto** (`LiveHolders` /proc
  cwd+exe+maps+fd scan **and** the `.holder` pid+starttime registration) are
  unchanged, in the same order, through the pre-existing `entryCollectable`.
- The ONE keep the namespace adds: a **flat** entry (not a directory) is not a
  run. The queue is also a shared drop point for artifacts with no run semantics
  that cannot be attributed to one — a verifier's `<id>-verify-report.md`,
  `scripts/lib/tmp-ban.sh`'s `.cleanup-<pid>` registry, a commit-message scratch
  file. None of them can be attributed to a reclaimable run, so none is deleted
  (Rule 15: what cannot be attributed is left alone, not guessed at). Such a
  flat entry also never **shields** a sibling, and cannot grow without bound on
  its own — the bytes live in directories, which do age out.
- Sweeping stays **per child** (`51312e68`'s contract): one fresh or held run
  never shields stale siblings.

Docs corrected in the same change: `SweepHome`'s queue arm and `Collectable`'s
①B comment still described the queue as a marker ("the in-home ones that carry
the queue namespace in their name"), which is no longer how its children are
judged.

## Commit

`a60ea9fb` — `fix(scratch-gc): the queue is its own NAMESPACE, not a name marker`
(the fix commit: `internal/scratch/gc.go` + `internal/scratch/gc_queue_subtree_test.go`
+ this report; added file-by-file per Rule 8, on fresh branch
`fix/scratch-gc-queue-namespace` off master `622f4b0a`, in the linked worktree
`/home/user/workspace/gopi-queue-ns`).

## Files

- `internal/scratch/gc.go` — `queueChildCollectable` (new, the shared
  predicate), `queueSweepChildren` / `queuePlanChildren` rewired to it, two
  stale comments corrected.
- `internal/scratch/gc_queue_subtree_test.go` — 5 new tests; 1 existing test
  updated (its "foreign entry" fixture was a *directory*, see below).
- `devdoc/scratch-gc-queue-namespace.md` — this report.

## Verification

### Build / vet / format

```
$ go build ./...                      → BUILD_EXIT=0
$ gofmt -l internal/scratch/          → (empty)
$ go vet ./internal/scratch/          → clean
```

### The regression test, RED before / GREEN after

`go test ./internal/scratch/ -run TestSweepHomeCollectsUnmarkedQueueChildren -count=1`

**Before (master, 622f4b0a)** — every measured shape is invisible:

```
--- FAIL: TestSweepHomeCollectsUnmarkedQueueChildren (1.23s)
    --- FAIL: TestSweepHomeCollectsUnmarkedQueueChildren/verify-01a0a248-modcache-manual
        gc_queue_subtree_test.go:285: dry-run plan = [], want exactly [.../queue/verify-01a0a248-modcache-manual]
            — an unmarked queue child is INVISIBLE to the plan (the reported hole)
    --- FAIL: .../accept        dry-run plan = [], want exactly [.../queue/accept]
    --- FAIL: .../test-budget   dry-run plan = [], want exactly [.../queue/test-budget]
FAIL	github.com/nick/gopi/internal/scratch	4.162s
```

**After (this branch)**:

```
$ go test ./internal/scratch/ -count=1
ok  	github.com/nick/gopi/internal/scratch	12.530s
```

**Red-proof re-run against the CURRENT master tip** (master advanced again to
`eb802d64` after the branch was cut at `622f4b0a`; the hole is unchanged). The
post-fix test file is dropped onto a clean master tree and fails there, which
pins the test to the FIX rather than to a stale base:

```
$ git worktree add --detach /home/user/workspace/tmp-verify/redproof-$$ master   # tip eb802d64
$ cp <branch>/internal/scratch/gc_queue_subtree_test.go internal/scratch/       # test only, master gc.go
$ go test ./internal/scratch/ -run TestSweepHomeCollectsUnmarkedQueueChildren -count=1
--- FAIL: TestSweepHomeCollectsUnmarkedQueueChildren (1.56s)
    --- FAIL: .../verify-01a0a248-modcache-manual
        gc_queue_subtree_test.go:285: dry-run plan = [], want exactly [.../queue/verify-01a0a248-modcache-manual]
            — an unmarked queue child is INVISIBLE to the plan (the reported hole)
    --- FAIL: .../accept        dry-run plan = [], want exactly [.../queue/accept]
    --- FAIL: .../test-budget   dry-run plan = [], want exactly [.../queue/test-budget]
FAIL	github.com/nick/gopi/internal/scratch	2.649s
```

The same file on this branch: all 3 sub-cases **PASS** (see "After" above and the
final run in the task report).

New tests: `TestSweepHomeCollectsUnmarkedQueueChildren` (the repro, 3 measured
sub-cases, plan **and** sweep), `TestSweepHomeKeepsUnmarkedQueueArtifacts`
(flat report / `.cleanup-*` / commit-msg survive, plan empty),
`TestSweepHomeKeepsAHeldUnmarkedQueueChild`, `TestSweepHomeKeepsAFreshUnmarkedQueueChild`,
plus `TestSweepHomeKeepsAnEmptyQueueDirectory` updated.

Related packages: `go test ./internal/testutil/` (it re-exports the sweep) — see
the "Tests" section below for the run status.

### The real root: `--dry-run` plan, before vs after (fixture = the measured shape)

The 415 MB-era data is no longer on disk (master advanced to `622f4b0a`;
`/dev/shm` was at 15%), so the shape is **reproduced as a fixture** on the real
scratch root (`/dev/shm/gopi-test-scratch/queue`, reached via the
`~/workspace/gopi-test-scratch` symlink), aged past `StaleAge` so only the
namespace (never the age veto) can explain the decision:

```
411M  queue/verify-01a0a248-modcache-manual      (410 MB gocache blob)
4.0M  queue/accept
2.0M  queue/test-budget
```

Two binaries from the same tree — `gopi-prefix` (master) and `gopi-postfix`
(this branch):

```
$ ./gopi-prefix scratch-gc --dry-run      # PRE-FIX
scratch-gc role=cli home=/dev/shm/gopi-test-scratch removed=0 stale=6h0m0s disabled=false dry-run=true

$ ./gopi-postfix scratch-gc --dry-run     # POST-FIX
scratch-gc role=cli home=/dev/shm/gopi-test-scratch removed=3 stale=6h0m0s disabled=false dry-run=true
would-remove /dev/shm/gopi-test-scratch/queue/accept age=37h15m6s
would-remove /dev/shm/gopi-test-scratch/queue/test-budget age=37h15m6s
would-remove /dev/shm/gopi-test-scratch/queue/verify-01a0a248-modcache-manual age=37h15m6s
```

Pre-fix: **0 planned** (the whole 417 MB invisible). Post-fix: **3 planned**.

### The real sweep: `df -h /dev/shm` before / after

```
$ df -h | grep shm                                    # BEFORE
tmpfs           7.8G  2.2G  5.7G  28% /dev/shm

$ ./gopi-postfix scratch-gc
scratch-gc role=cli home=/dev/shm/gopi-test-scratch removed=3 stale=6h0m0s disabled=false

$ df -h | grep shm                                    # AFTER
tmpfs           7.8G  1.8G  6.1G  23% /dev/shm
```

**Reclaimed ~417 MB (0.4 GB) in one sweep** — the exact bytes the pre-fix sweep
could not see. (The epic's "≥400 MB" bar is met with the fixture; on a host
carrying the original residue the same predicate would have taken the 415 MB
child itself.)

### The live child and the in-flight artifacts are still shielded

`queue-runtime-merge-build-2712346` (1.2 GB) and `queue-runtime-merge-resolve-2712346`
survived both the dry-run and the real sweep:

```
$ du -sh queue/queue-runtime-merge-build-2712346
1.2G	/dev/shm/gopi-test-scratch/queue/queue-runtime-merge-build-2712346
```

**Honest correction to the briefing's premise:** the briefing calls this child
"live merger-held (pid 2712346 = gopi merger)". On this host **pid 2712346 no
longer exists** (`ps -p 2712346` is empty; `/proc/2712346` is gone), and no
process holds that directory (`cwd`/`fd`/`maps` scans found none). It is
currently shielded by **age**: its mtime is 39 min old, well under `StaleAge`
(6h) — which is why the sweep left it alone. The veto that matters was proven
separately, on a child that *is* held.

### The hard rule, proven on a real LIVE holder (unmarked queue child)

A stale, unmarked queue child with a live process's cwd inside it:

```
holder pid=426952 cwd=/dev/shm/gopi-test-scratch/queue/verify-live-holder-realroot
=== dry-run with a LIVE holder (must NOT list the child) ===
scratch-gc ... removed=0 ... dry-run=true          ← not even planned
=== real sweep with a LIVE holder ===
scratch-gc ... removed=0 ...                       ← not deleted
=== held child after the sweep ===
PRESENT (correct)   8.0M
=== after the holder is gone: the SAME child must now be planned ===
scratch-gc ... removed=1 ... dry-run=true
would-remove .../queue/verify-live-holder-realroot age=37h15m36s
```

The child survives exactly while the holder lives and is collectable the moment
it dies — the namespace changed what is ELIGIBLE, not what is PROTECTED.

### Blast radius: non-queue foreign residue untouched, queue dir survives

```
foreign (stale, unmarked, OUTSIDE the queue) present before: yes
scratch-gc role=cli home=/dev/shm/gopi-test-scratch removed=0 stale=6h0m0s disabled=false
foreign present after the sweep: YES (correct)
queue directory after the sweep: PRESENT (correct)
flat artifacts after the sweep:
  .cleanup-3776529
  .cleanup-3809854
  .cleanup-3824711
  .cleanup-3876503
  01a09fdf-verify-report.md
  daemon-cwd-commit-msg.txt
```

## One existing test had to change (and why that is correct)

`TestSweepHomeKeepsAnEmptyQueueDirectory` used a foreign **directory**
(`someone-elses-loose-file`) as its "an idle queue holds nothing collectable"
fixture. Under the namespace rule **every directory in the queue is a run** and
ages out — there is deliberately no name-based way left to call a directory
inside the queue "someone else's" (that was the hole). The fixture is now a flat
**file**, which is the keep the namespace actually declares, and the comment
records why. This is a fixture following the corrected contract, not a
weakened assertion.

## Final re-verification at the branch tip (after the report commit)

Re-run from a freshly built binary of the branch tip `e0b96625`, fixture
re-created (the earlier one had been reclaimed), on the real scratch root:

```
$ bash bin/live-reverify.sh
=== df BEFORE ===
tmpfs           7.8G  2.3G  5.5G  30% /dev/shm
=== dry-run (post-fix) ===
scratch-gc role=cli home=/dev/shm/gopi-test-scratch removed=3 stale=6h0m0s disabled=false dry-run=true
would-remove /dev/shm/gopi-test-scratch/queue/verify-01a0a248-modcache-manual age=37h0m0s
=== real sweep ===
scratch-gc role=cli home=/dev/shm/gopi-test-scratch removed=3 stale=6h0m0s disabled=false
=== df AFTER ===
tmpfs           7.8G  1.9G  5.9G  24% /dev/shm
=== SHIELDED child (live merge tree) ===
1.2G	/dev/shm/gopi-test-scratch/queue/queue-runtime-merge-build-2712346
=== flat in-flight artifacts surviving ===
01a09fdf-verify-report.md
daemon-cwd-commit-msg.txt
=== queue dir itself ===
/dev/shm/gopi-test-scratch/queue
=== fixture reclaimed? (expect: No such file) ===
ls: cannot access '.../queue/verify-01a0a248-modcache-manual': No such file or directory
ls: cannot access '.../queue/accept': No such file or directory
ls: cannot access '.../queue/test-budget': No such file or directory
```

`removed=3`, ~417 MB reclaimed (`2.3G → 1.9G`), the merger's 1.2 GB tree and
every flat in-flight artifact still present, queue directory intact.

## Not Done / bounds (honest)

- **What the namespace does NOT collect:** flat entries (regular files,
  symlinks) directly in the queue are never collected — by design (Rule 15: not
  attributable to a run). A queue that accumulated unbounded *flat* files would
  not be bounded by this change, but flat files carry no gocache/gomodcache and
  are not the measured leak; the bytes live in directories.
- **The 6h `StaleAge` was not touched.** master has since gained a briefing
  (`eb802d64`, `briefings/brief-scratch-gc-stale-window.md`) arguing the bare 6h
  constant's premise is false. That is a separate, explicitly-filed work item —
  this fix makes no claim about the window's value and stays orthogonal to it.
- **No daemon/timer change:** the resident tick and the one-shot CLI both call
  `SweepHome`, so both faces inherit the fix with no code change.
- The measured 415 MB residue was no longer on disk when this branch was cut
  (master advanced, `/dev/shm` at 15%); the reclaim is therefore proven against
  a fixture reproducing the exact measured shape, aged past `StaleAge`.

## Verification summary (Build / Test / Commit)

- **Build**: `go build ./...` → exit 0; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK;
  `gofmt -l internal/scratch/` empty; `go vet ./internal/scratch/` clean.
- **Tests**: `go test ./internal/scratch/` → **ok** (18.7s full package, 9.7s at the final re-run);
  repro `TestSweepHomeCollectsUnmarkedQueueChildren` → **FAIL on master `eb802d64`**
  (3/3 sub-cases, "dry-run plan = []") / **PASS on this branch** (3/3).
  `./internal/testutil/` ok (122.7s); `cmd/agent -run Scratch` ok.
  `internal/standards` + `internal/stylegate` fail **identically on master** and flag
  `internal/scratch` 0 times — pre-existing, not introduced here.
- **Commit**: `a60ea9fb` (fix) + `e0b96625` (report hash line) on branch
  `fix/scratch-gc-queue-namespace`, worktree `/home/user/workspace/gopi-queue-ns`,
  cut off master `622f4b0a`.
