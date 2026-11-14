<!-- doc-class: normative -->
# Task Report: the GC's declared trees — the GOTMPDIR tree, the raw logs, the session raw.log

**Scope**: `internal/scratch` (the declared-tree registry), `cmd/agent` (the
`scratch-gc trees` arm), `scripts/scratch-gc-watchdog.sh` (the installed
watchdog), plus the two producers that now reference the registry.
**Branch**: `feat/scratch-gc-declared-trees`
**Measured on**: this host (peer1), 2026-09-20, `/home` = `/dev/sda4` 766G.

## Completed

- **A declared-tree registry** (`internal/scratch/declared_trees.go`). Three
  trees, each with its OWN age constant, its path resolver, and its namespace
  predicate:
  | tree | path | constant | namespace |
  |---|---|---|---|
  | `go-tmp` | `$GOTMPDIR` (env, else `~/.config/go/env`) | `GoTmpStaleAge` = 24h | `go-build*` dirs |
  | `raw` | `<gopi dir>/raw` | `RawLogStaleAge` = 72h | `gopi-raw-*.log` |
  | `session-raw` | `<gopi dir>/sessions` | `SessionRawLogStaleAge` = 336h (14d) + `SessionRawLogDirStaleAge` = 360h for the empty container | `*.raw.log` |

  Each value is declared once and referenced (Rules 1/13/14): the writers build
  their names from `scratch.SessionRawLogFileSuffix` / `scratch.RawLogNamePrefix`,
  and `internal/config` resolves the raw/session roots through the registry's
  function seam (one path spelling each, no config→scratch cycle).
- **The sweeps reuse the existing hard rule**, not a copy: `ScratchEntryStale` +
  `LiveHolders`/`heldSet` + `unheldRegistered` (the `.holder` registration). The
  **root of a declared tree is never a candidate at any age**, checked in the
  predicate AND re-checked at the delete (`removeCollectableOfTree`) — the
  invariant behind `briefings/brief-declared-paths-survive-sweep.md`.
- **`gopi scratch-gc trees`** drives them; each line reports that tree's own
  window and the reclaimed **bytes** on the existing logfmt shape (composed from
  `scratchGCLogFmt`, so the two faces cannot drift), e.g.
  `scratch-gc role=cli-trees home= removed=26 stale=72h0m0s disabled=false tree=raw bytes=1553432404`.
- **The watchdog drives both faces** behind the existing single flock, and
  **degrades loudly** when the binary predates the new arm (skips that face,
  keeps sweeping the home — verified against master's `bin/gopi`, whose argv word
  would otherwise have been an agent prompt).
- **The mechanical gate** (`TestDeclaredTreesCoverRepoWriters`): every tree this
  repository's own writers create must be declared, and each writer's own file
  must still carry the evidence string the row pins — so a writer that moves
  fails the gate instead of silently leaving its tree unbounded.
- **Two defects the real tree exposed, fixed with regression guards**:
  1. the walk reused `ScanLimit` (10,000, one scratch DIRECTORY) as the bound for
     a whole declared TREE — the sessions tree has **74,994** top-level entries,
     so the entire 40 GB tree was SKIPPED and reported `bytes=0`
     (`TestDeclaredTreeWalksPastTheScratchScanLimit`);
  2. per-candidate `filepath.EvalSymlinks` plus an O(candidates × holders)
     comparison made the dry run exceed **120s** — now **3.3s**
     (`TestHeldSetIndexesTreeAncestors`, which caught an out-of-tree holder being
     indexed).

## Not Done (honest)

- ~~**The build cache tree is NOT declared**~~ — **CLOSED** by
  `feat/build-cache-tree-gc` (Rule 26's own dispatch): the tree now has a
  declared row with its own `BuildCacheStaleAge = 24h` (the window was *measured*
  — the gate's per-key bound is unreachable at 7.5%, so the tree grows by
  accumulation and age is the only bound that can exist), a key-shape namespace
  predicate, a staleness oracle that reads the newest file BELOW each key (a key's
  own mtime never moves after the gate creates it), and a resolver through the
  worktree-root seam. The gate's anchor list carries its writers. See
  `devdoc/build-cache-tree-gc.md`.
- **74,981 non-empty session directories are not swept as a class.** Only their
  `*.raw.log` files and provably EMpty containers are candidates. Mass-pruning
  session directories needs a keep-rule (the resume/side-file contract) that the
  epic deliberately left out of scope; using the raw-log window as a directory
  keep-window was rejected as a guess.
- **`internal/gopilog`'s reader regexp still spells the raw-log name shape
  inside the pattern** (a regexp cannot take a constant without being rewritten).
  Writer/sweeper and reader are held together by
  `gopilog.TestRawLogDirNameMatchesScratchTree` instead.
- The watchdog's *installed* copy was refreshed (20 lines added), but master's
  `bin/gopi` does not carry the arm until the merge rebuild (Rule 12) — until
  then the watchdog logs the skip and sweeps the home face only, by design.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK.
- Tests (branch-scoped): `go test ./internal/scratch/ ./internal/config/ ./internal/gopilog/ ./cmd/agent/` — all **ok** (scratch 34.4s, config 2.5s, gopilog 6.2s, cmd/agent 23.0s).
  `./internal/standards/` FAILS on `devdoc/DOC-INDEX.tsv` for **six files that are
  not mine** (`cli-ui-feasibility-study.md`, `report-seam-hermetic.md`,
  `requirements/12-verification-and-gates.md`, `task-notify-study-20260920.md`,
  `taskid-vs-sessionid-verdict.md`, `tools-master-reds-90e664e0.md`) plus
  `gopi-statistics-20260920.md` (missing banner). Measured pre-existing: the same
  failures reproduce in the untouched main repo (`git status --short` there: 0
  entries).
- Real run: `./bin/gopi scratch-gc trees` → exit 0.
- Watchdog: `bash scripts/scratch-gc-watchdog.sh` → exit 0, both faces logged.

### Evidence (self-filed)

**Before** (measured, 2026-09-20 08:2x):
```
du -sb ~/workspace/gopi-go-tmp          → 514412205      (18 go-build* dirs)
du -sb ~/.gopi/raw                      → 1553564061     (31 files)
find ~/.gopi/sessions -maxdepth 2 -name '*.raw.log' -printf '%s\n' | awk   → 40355681297  (877 files)
df -h /home                             → /dev/sda4 766G 502G used 226G avail 70%
```

**Planted proof entries** (aged past each tree's own constant; `P` = shell pid):
```
~/workspace/gopi-go-tmp/go-build-gcsweep-P/payload.bin   3,000,000 B, mtime -3d
~/.gopi/raw/gopi-raw-20260101-010101.log                 mtime -5d
~/.gopi/sessions/gcsweep-proof-P/gcsweep-proof-P.raw.log mtime -20d   (dir only that file)
~/.gopi/sessions/gcsweep-keep-P/{gcsweep-keep-P.raw.log, session.jsonl}  mtime -20d  ← the control
```

**Real sweep** (`./bin/gopi scratch-gc trees`, 1.2s):
```
scratch-gc role=cli-trees home= removed=1 stale=24h0m0s disabled=false tree=go-tmp bytes=3000000
scratch-gc role=cli-trees home= removed=26 stale=72h0m0s disabled=false tree=raw bytes=1553432404
scratch-gc role=cli-trees home= removed=3 stale=336h0m0s disabled=false tree=session-raw bytes=38
```
Post-run checks (all observed):
```
ls -d ~/workspace/gopi-go-tmp/go-build-gcsweep-*   → "No such file or directory"   (removed)
ls ~/.gopi/raw/gopi-raw-20260101-010101.log        → "No such file or directory"   (removed)
ls -d ~/.gopi/sessions/gcsweep-*                   → only gcsweep-keep-3907085      (proof dir + its .raw.log gone; control dir KEPT)
ls -la /home/user/.gopi/sessions/gcsweep-keep-3907085/      → session.jsonl present, the control's .raw.log GONE
  (honest reading: the control's raw log was aged 20d — past the tree's own 14d window — so the FILE
   verdict was correct. What the control proves is the CONTAINER rule: its directory SURVIVED because a
   container is only emptied when every child is a candidate, and session.jsonl never is.)
~/workspace/gopi-go-tmp  PRESENT   ~/.gopi/raw  PRESENT   ~/.gopi/sessions  PRESENT   ← every ROOT survived
GOFLAGS= go build -o …/gc-sweep-build-probe ./cmd/agent/  → BUILD_AFTER_SWEEP_OK   ← a real build AFTER the sweep (the incident's test)
```
**After**:
```
du -sb ~/workspace/gopi-go-tmp  → 512496871      (the 3 MB proof + its own go-build dir reclaimed)
du -sb ~/.gopi/raw              → 131681
df -h /home                     → 501G used, 226G avail, 69%
```
(The raw tree's 1.55 GB verdict against a 131 KB landing size is exact:
`ls /home/user/.gopi/raw/gopi-raw-*.log` AFTER the run prints nothing — all 26 of ours were
past the 72h window and all 26 went. The surviving entries are the ones the
namespace predicate refuses: 7 non-`gopi-raw-*.log` items (a `verify-*` directory
and a human's own `*-test.log` dumps), which a pure SUFFIX rule would have swept —
that is why this tree declares a name PREFIX, and what the fixture's
`fmt-detail.log` control pins. The session tree's 40.4 GB is its 74,586 session
directories (879 carry a raw log, none stale); the ~45 MB growth is sessions
written during this run.)

**Dry run** (`--dry-run`, 3.3s; was >120s before the two fixes):
```
scratch-gc role=cli-trees home= removed=1  stale=24h0m0s  tree=go-tmp      bytes=3000000    dry-run=true
scratch-gc role=cli-trees home= removed=26 stale=72h0m0s  tree=raw         bytes=1553432404 dry-run=true
scratch-gc role=cli-trees home= removed=3  stale=336h0m0s tree=session-raw bytes=56         dry-run=true
would-remove /home/user/.gopi/sessions/gcsweep-proof-3907085 …
```
(The dry run counts the session container's bytes while its file is still inside;
the real run measures the container after its children are gone — the accounting
is conservative, never overstated.)

**Watchdog log line** (appended by the installed script to
`~/.gopi/logs/scratch-gc-watchdog.log`, run with `GOPI_SCRATCH_GC_BIN=<branch bin>`):
```
2026-09-20T08:38:16+08:00 scratch-gc-watchdog scratch-gc role=cli home=/home/user/workspace/gopi-test-scratch removed=0 stale=6h0m0s disabled=false
2026-09-20T08:38:20+08:00 scratch-gc-watchdog scratch-gc role=cli-trees home= removed=0 stale=24h0m0s disabled=false tree=go-tmp bytes=0
2026-09-20T08:38:20+08:00 scratch-gc-watchdog scratch-gc role=cli-trees home= removed=0 stale=72h0m0s disabled=false tree=raw bytes=0
2026-09-20T08:38:20+08:00 scratch-gc-watchdog scratch-gc role=cli-trees home= removed=0 stale=336h0m0s disabled=false tree=session-raw bytes=0
```
Degradation path (master's binary, arm absent):
```
FAIL /home/user/work/GoPi/bin/gopi does not carry the scratch-gc trees arm … that face is SKIPPED … The scratch-home face below still runs.
scratch-gc role=cli home=/home/user/workspace/gopi-test-scratch removed=0 stale=6h0m0s disabled=false
```

**RED-PROOF of the gate** (the `session-raw` row deleted from `DeclaredTrees()`,
then reverted):
```
--- FAIL: TestDeclaredTreesCoverRepoWriters (0.00s)
    UNDECLARED TREE: internal/agent/rawlog.go — the repository's own commands create and never clean a tree (internal/agent/rawlog.go) that the GC's declared set does not cover. Declare it in internal/scratch.DeclaredTrees (or remove the writer).
    UNDECLARED TREE: internal/config/scratch_trees.go — …
--- FAIL: TestGateCatchesUndeclaredTree (0.00s)
    gate fires on the real declared set: [{writer:internal/agent/rawlog.go covers:session-raw …} …]
FAIL	github.com/the operator/gopi/internal/scratch	0.490s
```

## Files

- `internal/scratch/declared_trees.go` (new) — the registry, the resolvers, the
  plan/sweep faces, the heldSet index.
- `internal/scratch/declared_trees_test.go` (new) — the gate, its red-proof, the
  root invariant, the hard rule, the two regression guards.
- `internal/scratch/scratch.go` — `CachedCanonicalPath` (the hot-path memo the
  measured 100s per-candidate symlink cost motivated).
- `internal/config/scratch_trees.go` (new) — the resolver seam (one path spelling).
- `internal/gopilog/scratch_tree_pin_test.go` (new) — the raw-dir/name-shape pin.
- `internal/agent/rawlog.go` — the writer builds its names from the registry's
  constants.
- `cmd/agent/cli_scratch_gc.go` — the `trees` arm + the bytes-bearing line + usage.
- `cmd/agent/cli_scratch_gc_trees_test.go` (new) — the arm's end-to-end contract on
  a sandbox (`GOTMPDIR` + `GOPI_HOME` inside a temp dir; nothing touches the real
  trees).
- `scripts/scratch-gc-watchdog.sh` — drives both faces, one lock, loud degradation.
- Commit: `f8d63783` (implementation) + the docs commit that follows.
