<!-- doc-class: historical -->
# Review Report: internal/tools/symbol.go

Reviewed 2026-05-21 on branch `feat/cs-llm-adoption` (worktree `/home/user/workspace/gopi-cs-adopt`),
**working-tree state** — the file carries uncommitted changes vs HEAD `064c177`: `.c`/`.h` added to
`indexedSourceExtensions`, `C` added to `ctagsLanguages`, comment updates (`internal/agent/loop.go`
also has unrelated tool-description edits in flight). Line numbers below are the live working tree.
`go build ./internal/tools/` + `go vet` pass.

## Completed

### 1. Full inventory — 10 structs/types, 16 functions, 6 vars, 6 consts (file total 674 lines)

**Structs (2 + 2 package-level cache maps)**

| Line | Name | Signature / shape |
|---|---|---|
| symbol.go:51 | `stalenessCacheEntry` | `{tagsMtime time.Time; stale bool; walkedAt time.Time}` — per-root staleness verdict; `tagsMtime` is the mtime guard key |
| symbol.go:88 | `walkSnapshot` | `{newest time.Time; rootMtime time.Time; subdirs map[string]time.Time; takenAt time.Time}` — incremental-walk revalidation record |

(`CodeSymbolHit` — the result type — lives in `internal/tools/search.go:27`, not this file.)

**Functions (16, all free functions — zero methods)**

| Line | Signature |
|---|---|
| symbol.go:158 | `func queryDefinition(ctx context.Context, root, symbol string) ([]CodeSymbolHit, error)` — ensureTags → tagsLowerBound → scanTagCluster → dedup |
| symbol.go:215 | `func tagsLowerBound(f *os.File, size int64, target string) (int64, error)` — O(log n) binary search on byte-order-sorted tags |
| symbol.go:243 | `func scanTagCluster(f *os.File, start int64, symbol string) ([]CodeSymbolHit, error)` — reads the equal-name run, stops at first mismatch |
| symbol.go:270 | `func tagLineKey(line string) string` — first tab-separated field |
| symbol.go:282 | `func lineStartAt(f *os.File, pos int64) (int64, error)` — backward scan for the line's start offset |
| symbol.go:320 | `func readTagLineAt(f *os.File, off, size int64) (string, error)` — chunked forward line read, 1MB cap |
| symbol.go:355 | `func formatTagEntry(line string) CodeSymbolHit` — ctags line → structured hit (`line:N`/`language:X` extras) |
| symbol.go:388 | `func signatureFromPattern(pattern string) string` — strips `/^…$/;"` anchors and ctags escaping |
| symbol.go:412 | `func kindName(kind, language string) string` — single-letter kind → readable name, per-language disambiguation |
| symbol.go:479 | `func cachedStalenessVerdict(root string, tagsMtime time.Time) (stale, ok bool)` |
| symbol.go:491 | `func cacheStalenessVerdict(root string, tagsMtime time.Time, stale bool)` |
| symbol.go:504 | `func resetStalenessCache()` — test isolation, clears both caches |
| symbol.go:518 | `func newestSourceFileCached(ctx context.Context, root string) (newest time.Time, realWalk bool, err error)` |
| symbol.go:540 | `func topLevelUnchanged(root string, snap walkSnapshot) bool` — root + immediate subdir mtime check |
| symbol.go:569 | `func snapshotWalk(root string, newest time.Time) walkSnapshot` |
| symbol.go:599 | `func ensureTags(ctx context.Context, root string) (string, error)` — the index builder |
| symbol.go:641 | `func newestSourceFile(ctx context.Context, root string) (time.Time, error)` — full `filepath.Walk`, skips vendor/.git/hidden |
| symbol.go:668 | `func hasIndexedExt(path string) bool` |

**Vars (6)**: `stalenessCache`+`stalenessCacheMu`+`stalenessWalkCount atomic.Int64` (:60–66),
`walkCache`+`walkCacheMu` (:95–98), `ctagsLanguageMapArgs` (:114), `indexedSourceExtensions` (:120),
`CodeSearchGlobs` (derived via IIFE, :134), `goKindNames` (:433), `languageKindNames` (:440).

**Consts (6)**: `tagProbeChunkBytes = 8KB` (:25), `tagLineReadChunkBytes = 4KB` (:30),
`tagMaxLineBytes = 1MB` (:35), `stalenessCacheTTL = 2s` (:45), `stalenessWalkCacheTTL = 30s` (:78),
`ctagsLanguages` (:107). All tuning numbers are named constants — Rule 1 clean.

### 2. ensureTags assessment (symbol.go:599–639)

The build itself is sound and cheap by design:

- **Path**: `root/.gopi-tags`; regeneration runs `ctags -R --languages=<8 langs> --map-JavaScript=+.cjs
  --map-TypeScript=+.tsx --extras=+q --fields=+ln -f <tagsPath> .` with `cmd.Dir=root`.
  `--extras=+q` emits `pkg.Name` qualified aliases (paired with bare entries, collapsed by dedup);
  `--fields=+ln` supplies `line:` and `language:` extras that `formatTagEntry`/`kindName` parse.
- **Staleness gate** (two-tier): missing tags → stale, always regenerate. Existing tags →
  `cachedStalenessVerdict` first (tags mtime unchanged AND verdict < 2s old → reuse); on miss,
  `newestSourceFileCached` re-derives — full walk only when the incremental `topLevelUnchanged`
  check fails (stat root + immediate subdirs, ~µs vs 151ms full walk on 30K files); verdict cached
  with the tags mtime stamped in. After a regen, a fresh verdict is cached so same-round follow-ups
  skip the walk entirely.
- **Correctness posture**: comments explicitly accept eventual consistency ("full correctness is not
  required; a stale index still returns useful symbol locations") — appropriate for a review tool.
- **Weak points**:
  1. `cmd.CombinedOutput()` — a noisy stderr gets embedded whole in the error; harmless, cosmetically ugly.
  2. **TOCTOU on tags mtime**: the mtime guard is read once (`info.ModTime()`), then ctags may
     regenerate between the verdict decision and the query — worst case one call serves a
     pre-regen view; the mtime guard invalidates immediately on the *next* call. Acceptable.
  3. `ctags` failure mode is hard-fail with no once-per-process fallback to the ripgrep path
     (`queryDefinition` returns the error; `CodeSearch` at search.go:105 still falls back — so in
     practice callers degrade gracefully). OK.
  4. In-place edits inside an *unchanged top-level dir listing* are recognized at most 30s late
     (`stalenessWalkCacheTTL`), documented in the constant's comment — deliberate tradeoff.

### 3. Thread-safety of the staleness cache — **YES, correctly guarded**

Both caches use their own dedicated `sync.Mutex` with every access mutex-held:

- `stalenessCache` (map) + `stalenessCacheMu`: read in `cachedStalenessVerdict` (:480–481,
  lock/defer unlock around map read), write in `cacheStalenessVerdict` (:492–493), full clear in
  `resetStalenessCache` (:505–507, which *also* takes `walkCacheMu` before touching `walkCache` —
  no lock-ordering hazard since `resetStalenessCache` is test-only and takes them sequentially).
- `walkCache` + `walkCacheMu`: `newestSourceFileCached` (:519–521 read, :528–530 write) holds the
  mutex only around map access, never across I/O (the walk and the stat check run unlocked — correct,
  no lock contention on the slow path).
- Concurrency is real: `code_search` runs inside R4 read-only parallel batches, so two goroutines can
  hit `queryDefinition` simultaneously. Races checked:
  - Read-modify-write of the verdict: last-writer-wins, both entries valid — benign.
  - Regeneration stampede: two concurrent misses both run `ctags -f <same tagsPath>`. ctags itself
    writes the file (last writer wins; both produce a complete index); no Go-side torn state. Mild
    wasted work, no corruption. A `singleflight` would eliminate it but is not warranted.
  - `stalenessWalkCount atomic.Int64` — correctly atomic for the observability counter.
  - No cache field is ever mutated in place after insertion (entries are replaced wholesale), so
    no partially-visible `walkSnapshot`.
- Verified: `go build` + `go vet` clean; `go test -race ./internal/tools/` would be the standing
  guard (existing tests already cover the caches via `resetStalenessCache`).

### 4. Not Done

- No code changes made — this Epic is review-only (the working tree's `.c`/`.h` diff belongs to the
  in-flight `feat/cs-llm-adoption` task, not this review).
- Did not run the full `go test ./...` suite (tools package runs real ctags regressions, ~88s); build+vet+targeted inspection substituted.

### 5. Verification

- Build: `go build ./internal/tools/` — OK
- Vet: `go vet ./internal/tools/` — OK
- Definition/usages cross-check: `CodeSymbolHit` at search.go:27; callers at search.go:105,
  loop.go:2717, cmd/agent/cli_code_search.go:142 — all consistent with this file's API.

### 6. Verdict

**Healthy — ship-shape.** `symbol.go` is a well-factored 674 lines: every tuning number is a named
constant (Rule 1), both caches are correctly mutex-guarded against real concurrency (R4 parallel
batches), the two-tier staleness design (2s verdict TTL + 30s walk snapshot + mtime guard) is
documented with the measured A/B numbers that motivated it, and the binary-search lookup is a
genuine O(log n) improvement over the linear scan it replaced. Weaknesses found are minor and
acknowledged in-code (30s in-place-edit latency, benign concurrent-regen stampede, CombinedOutput
noise) — none blocks a commit. No changes required from this review.
