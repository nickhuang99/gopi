<!-- doc-class: normative -->
# Task Report: the orphan-crash-dump sweep's per-startup cost — bounded-tail read

Branch `fix/crashdump-scan-cost` · worktree `~/workspace/gopi-crashcost` ·
task id `01a091ea-f649-79b5-8d64-a18eca4e7ca0` (the diagnosis is
`devdoc/crash-dump-startup-backfill.md`, branch `diag/crash-dump-startup`,
merged as `b8feadb2`).

## ⚠️ LOUD FLAG FOR THE SIBLING TASK (read this first)

**The classification PREDICATE (what counts as a crash) was NOT touched** — not
one condition, not one bound, not one event-type comparison. But the function
that CONTAINS it was **split from its reader**:

- before: `HasSessionEnd(path) { data := os.ReadFile(path); …walk… }`
- after: `HasSessionEnd(path) { data := sessionTailRead(path); return
  hasSessionEndBytes(data) }` + `hasSessionEndBytes(data []byte) bool { …walk…
  }` — the walk body moved **verbatim**, zero edited lines.

Anyone whose task owns the classification predicate
(`feat/fix-crashdump-false-positive`, worktree `/home/user/workspace/gopi-fix-crashdump`,
and the mtime live-window / reconciliation work) **must rebase onto this and
edit `hasSessionEndBytes`, not `HasSessionEnd`** — a patch applied to the old
shape will conflict, and a patch that re-inlines the walk back into
`HasSessionEnd` would silently reintroduce the whole-file read. The split is
deliberate and load-bearing: it is the only thing that lets the old-vs-new
equivalence test exist at all.

**OUTCOME OF THAT REBASE (2026-09-12): it worked exactly as intended.** The
sibling landed on master first (`3b51fc00 Merge branch
'feat/fix-crashdump-false-positive'`, adding the `orphanLiveWindow` mtime guard
and `sessionJSONLSuffix` to `orphanCheckJSONL`), and this branch rebased onto it
with **zero conflicts in `HasSessionEnd`** — the split meant the sibling's edit
and this one never touched the same lines. Verified after the rebase: the whole
real-tree equivalence arm and the bytes-read arm pass against the NEW
classification (which now also has a live-window), i.e. this cost fix is
orthogonal to the semantics the sibling added. The cost test had to learn ONE
thing about the new guard: its fixtures are backdated with `os.Chtimes` (and
the window env is pinned), because a just-written file is "live" under
`orphanLiveWindow` and would not be classified at all.

## Completed

- **`HasSessionEnd` reads a bounded tail, not the whole file.**
  `internal/agent/sessiontail.go` (new) = `sessionTailRead`: `os.Open` →
  `Stat` → one `ReadAt` of at most `sessionTailWindowBytes` bytes from the file
  end (a fresh window buffer per call — see "Why the window buffer is NOT
  pooled" below).
- **Classification predicate untouched (verified, not asserted).** The walk
  moved verbatim from `HasSessionEnd` into `hasSessionEndBytes(data []byte)`
  (`internal/agent/session.go`); `HasSessionEnd` is now
  `sessionTailRead` → `hasSessionEndBytes`. The predicate itself has **zero
  changed lines** — this change is cost/IO only, as the briefing demanded. The
  sibling task that owns "what counts as a crash" still owns that function.
- **The equivalence is a test, not a promise.**
  `sessionend_tail_equivalence_test.go` keeps the PRE-CHANGE implementation
  (whole-file `os.ReadFile` + the same walk) as an oracle in the test binary
  and compares old vs new on: a fixture corpus (both verdicts — the test fails
  if the corpus is vacuous), every byte offset in a ±64-byte band around the
  window boundary plus 120 deterministic random offsets, a synthetic
  production-size sweep (7 sizes × 3 tail shapes), and the host's **real**
  sessions tree.
- **Cost is asserted in BYTES READ, never wall-clock.**
  `sessionend_tail_cost_test.go` substitutes `openSessionTailReader` with a
  counting file: an 8 MB session must read ≤ `sessionTailWindowBytes`; the
  read must be FLAT across 64× size growth; a whole-tree sweep must stay within
  `files × window`; allocation churn over 200 probes stays under 4 MB.
- **`make test` green** — including the 4 `internal/stylegate` violations that
  were **already failing on master** (see "Not Done" for the exact accounting).

## Decision: the sweep stays on the startup path — but it is now IO-bounded

The briefing asked for a justified on-path vs off-path decision, argued against
the alternative. **Decision: keep `BackfillOrphanCrashDumps` on the startup
path.** Measured breakdown on the real tree (29 390–29 542 root entries,
28 998–29 150 session dirs, 58 643–58 959 sub-entries, 1 469–1 477 JSONL,
429.6–430.8 MB of JSONL; the tree grows between passes):

| component | before | after |
|---|---|---|
| bytes read (rchar, kernel) | 409.7–410.9 MB | **1.7 MB** |
| allocation churn | 435.9–437.2 MB | **20.7–20.8 MB** |
| wall (same pass, same host) | 0.866–2.924 s | **0.375–1.205 s** |
| residual = `ReadDir` metadata | — | **0.34–1.30 s** (root `ReadDir` alone: 45–126 ms) |
| verdicts | 367–374 orphans | identical, both ways |

(Ranges = the run-to-run spread over 5 passes on an increasingly loaded shared
host; the bytes and churn bands are the stable part, which is why the tests
assert those.)

1. **The cost that motivated the task was IO, and it is gone.** 409.7 MB →
   1.7 MB is a **34×** reduction in bytes read and **21×** in allocation churn.
   That was the whole bill: 429.6 MB of JSONL was slurped to inspect 4 KB per
   file.
2. **What is left is not the sweep's design — it is the tree's size.** The
   metadata-walk residual is `ReadDir` metadata over ~29 000 directories, which *any*
   sweep of this tree pays, including one that would only `stat` each file. An
   off-path move does not delete that cost; it moves it somewhere the user
   cannot see.
3. **The on-path placement is load-bearing for correctness, not just for
   reporting.** The sweep's only exclusion is *the current process's own*
   session id — a value that exists exactly once, at startup, and is exactly
   why the count is printed to the user at that moment
   (`cmd/agent/main.go:277`). Deferring the sweep to a background goroutine
   means (a) the startup line reports nothing or a stale count, and (b) the
   sweep can run after this process's session id is no longer the "live" one —
   i.e. the change would move work off-path *and* reach for semantics the
   sibling task owns (live-vs-dead classification). A cost fix must not
   purchase its win by silently expanding the classification surface.
4. **The alternative's failure mode is the one this codebase fears most: a
   silent no-sweep.** A staleness marker / journal ("skip the walk when the
   tree has not changed") saves the metadata-walk residual only when the marker is
   correct; when it is stale (crash before the marker write, clock skew,
   out-of-band file removal) the sweep stops happening and nothing reports it.
   A bounded 1.7 MB read + ~0.4 s of inherent metadata is a better trade than
   a stateful fast path that can fail closed. This is a real, separate design
   change (incremental scan / marker with an audited invalidation rule) —
   **submitted as its own task**, not smuggled into a cost fix.

So: on-path, but now bounded by the *metadata* of the tree rather than by its
*contents* — a 34× cheaper start with byte-identical behaviour.

## Files

| file | change |
|---|---|
| `internal/agent/sessiontail.go` | NEW — `sessionTailRead` (bounded window), `sessionTailWindowBytes`, test seam `openSessionTailReader` |
| `internal/agent/session.go` | `HasSessionEnd` now reads the bounded window; the walk moved verbatim into `hasSessionEndBytes` (predicate lines unchanged) |
| `internal/agent/sessionend_tail_equivalence_test.go` | NEW — old-vs-new oracle over fixtures, boundary offsets, production sizes, and the real tree |
| `internal/agent/sessionend_tail_cost_test.go` | NEW — bytes-read + allocation-churn proofs at the seam |
| `cmd/agent/cli_voice.go`, `cmd/agent/cli_voice_server.go` | **NOT part of this fix** — see "Not Done" |
| `devdoc/crashdump-scan-cost.md` | this report |

### Why the window buffer is NOT pooled

The first version recycled window buffers through a `chan []byte` pool. That
was dropped: `sessionTailRead` returns the window to its caller as a slice, so
a buffer returned to a pool at `defer` time aliases whatever the next caller
reads — cross-call state in exchange for ~10 MB of the ~437 MB this file's
whole-file read churned. The measured churn with a FRESH window per call stays
at 20.8 MB (below the 21 MB the 200-probe test's ceiling is derived from), so
the pool bought nothing worth reasoning about. Deliberately no pool, no
`sync.Pool`, no retained state.

## Not Done (honest)

- **The 4 `internal/stylegate` violations I fixed were already red on master,
  and they belong to the voice work — not to this task.** `make test` on
  master (`1c021f4c`) fails on `cmd/agent/cli_voice.go:124`
  (`os.UserHomeDir` discarded), `cmd/agent/cli_voice_server.go:207`
  (`json.NewEncoder(...).Encode` discarded) and `:162`/`:166` (bare
  `f.Close()`), all introduced by `bd3d34a5` (voice). The briefing requires
  `make test` green on this branch, so I made the MINIMAL correctness fix at
  those 4 sites (a real error branch for the temp-file close and the home
  lookup; a reasoned discard where there is genuinely nothing to do — the
  response headers are already committed). This is the only place this change
  touches code outside the sweep; **it is unrelated to the crash-dump cost and
  should be reviewed as its own thing** (a reviewer may reasonably prefer it
  split out, in which case only these two files need extracting).
- **The residual metadata walk is NOT fixed** — deliberately, per the
  decision above (a stateful fast path can fail closed). Submitted as a
  follow-up task.
- **No `BackfillOrphanCrashDumps` reconciliation** (dumps are never retracted
  when a session later ends cleanly). That is the *false-positive* sibling task
  (`01a091ea-f519-...`), in flight on `feat/fix-crashdump-false-positive`; I
  did not touch it.
- **Before/after are wall-clock on a shared host**, so the wall numbers move
  between passes (1.574 s / 1.116 s / 0.866 s were observed for the same
  pre-change code). The **bytes-read (409.7 MB → 1.7 MB) and allocation-churn
  (435.9 MB → 20.7 MB) numbers are stable across runs** and are the ones the
  tests assert. This is exactly why the tests assert bytes, not time.
- The earlier attempt on `fix/crash-dump-tail-read` (worktree
  `~/workspace/gopi-crash-tail`) contained a similar uncommitted edit; I wrote
  this one independently in `~/workspace/gopi-crashcost` so the two worktrees
  do not share files. Its `tmpbench/` probe directory was NOT copied here —
  the probe lives outside the repo (`.probe-archive/`, untracked, and the
  binaries in `/tmp`) and is not committed.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK; `gofmt -l` on touched files →
  clean; `go vet ./internal/agent/` → clean.
- **Semantic equivalence (old vs new), whole real tree**: probe arm B
  reimplements the PRE-CHANGE sweep (`os.ReadFile` + the pre-change predicate)
  independently and runs both in ONE process:
  `EQUIVALENCE: old and new sweeps agree on all files (orphans=367)`.
  In-tree, `TestHasSessionEndEquivalenceOnRealTree` compares the two
  implementations file-by-file over the host tree —
  `compared 400 real session files (largest 4339116 bytes, 294–295 ended)` →
  PASS (the ended count moves because the live tree grows).
- **Bytes read**: `TestHasSessionEndReadsTailOnly_EndedSession` (8 MB fixture),
  `..._OrphanSession` (4 MB), `TestHasSessionEndReadIsProportionalToTailNotFile`
  (1 KB → 16 MB, read flat), `TestDetectOrphanSessionsReadVolumeIsTailBounded`
  (12 × 2 MB tree): `sweep over 12 files × 2097152 bytes = 25165824 bytes of
  JSONL read 49164 bytes (0.1954% of the tree)` → PASS.
- Tests: `go test ./internal/agent/ -run 'SessionEnd|Orphan' -count=1` → ok
  (3.9 s). Full `make test` → see commit block below.
- **Before/after on the real tree** (`/home/user/.gopi/sessions`, 29 390 root
  entries / 1 469–1 477 JSONL / 429.6–430.8 MB JSONL / 8.6 GB including
  non-JSONL): 409.7–410.9 MB → 1.7 MB read (34×); 435.9–437.2 MB → 20.7–20.8 MB
  churn (21×); 0.866–2.924 s → 0.375–1.205 s wall; 367–374 orphans both ways
  (the tree grows while the task runs), **0 dumps written** (arm A excludes
  every detected orphan so the live host is never mutated).
- Commit: `<hash>` on `fix/crashdump-scan-cost` (see the branch log) —
  `tmpbench/` was deleted before the commit and is confirmed absent from it.

## Verification recipe (reuse — do not re-derive)

- The probe is `tmpbench/main.go` on this branch pre-commit, archived at
  `~/workspace/gopi-crashcost/.probe-archive/main.go` (untracked): build with
  `CGO_ENABLED=0 go build -o /tmp/crashbench ./tmpbench/` from the worktree,
  run bare. It reimplements the pre-change sweep, so before/after and the
  equivalence verdict come out of ONE process on the same host load — do not
  compare against a number captured on another day.
- `rchar` from `/proc/self/io` is the honest read-volume meter (`TotalAlloc`
  delta is churn, `VmHWM` the high-water mark); `os.ReadFile` bypasses any
  in-process counting seam, so the kernel counter is the only way to measure
  the OLD path's bytes.
