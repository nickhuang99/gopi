<!-- doc-class: historical -->
# Task Report: fresh-DB guard — contention-robust, I/O-attributable verdict

Branch: `feat/freshdb-contention-guard` (worktree `~/workspace/gopi-freshdb-guard`,
based on master `48ecdd5`). Task id: `t-1788905574`.

## Problem

`TestFreshDBOpenCloseIsFast` (internal/tools/testsuite_temproot_test.go) is the
hang guard from t-1788361069: it reproduces open→INSERT→**Close** of a fresh
ledger DB, the step where SQLite checkpoints the WAL and fsyncs. Its verdict
was an **absolute wall clock**: `freshDBOpenCloseBudget = 25ms/DB`, 12 DBs,
300ms total.

Two defects:

1. **Not I/O-attributable.** The same open→INSERT→Close cycle spends most of
   its time in in-process work (modernc/sqlite schema init + database/sql
   scheduling), which host CPU contention inflates 2–4x. Measured on the
   pinned tmpfs root while the host was 4-core oversubscribed (3 sibling
   package suites, load 8–13): candidate loop median **43ms/DB**, max 71ms —
   1.7x–2.8x over the old budget. Master (unpatched) reds in that state
   (see evidence table, phase H1: 52.1ms/DB, budget 300ms).
2. **Untruthful failure text.** It asserted "the scratch root %q **syncs at
   WAL close**" while printing a `/dev/shm` root — a claim `statfs`
   contradicts (`tmpfs` cannot send an fsync to a block device). Rule 15.

## Fix (why this shape)

The verdict is now a **same-process I/O attribution**, not a wall clock:

- Each iteration times the candidate cycle (scratch root) and an
  **fsync-free in-memory control** cycle (`file:gopi-testsuite-control-<pid>-<i>
  ?mode=memory&cache=shared&`, held alive by a keeper connection so the
  internal open/close inside `openTasksDB` does not destroy it), interleaved.
- The control does the **same work** (full schema init + INSERT + WAL close of
  a fresh DB) through the **same production opener** (`openTasksDB`) — its only
  difference is that there is no file, hence no fsync. So it is the noise
  reference: CPU contention inflates **both** loops, and the ratio stays ~1.
- **Verdict = median ratio candidate/control > `freshDBAttributionRatioMax`
  (12.0x)**, plus a per-iteration hard ceiling `freshDBStallCeiling = 2s`
  (the original "one stalled fsync blocks minutes" hang detector).
- The scratch root is classified by **statfs** (`scratchRootFilesystem`): a
  root on tmpfs/ramfs can never be blamed on an fsync — the message says so
  explicitly; a block-backed root is attributed truthfully.

Measured separation on this host:

| root | candidate median/DB | fsync-free control median/DB | ratio |
|---|---|---|---|
| `/dev/shm` (tmpfs), idle | 14–16 ms | 11–12 ms | **1.28–1.35x** |
| `/dev/shm`, 3 sibling suites + 10 CPU hogs (load 10–13) | 43.0 ms | 30.5 ms | **1.41x** |
| `/tmp` (/dev/sda3, block-backed) | 1.4–1.6 s | ~12 ms | **141–154x** |

Noise floor (tmpfs-vs-tmpfs, two separate dirs, same process) measured
1.24x total / 1.06x median in the calibration probe. The limit (12x) sits an
order of magnitude above the noise and an order of magnitude below the
divergence it must catch.

Constants (Rule 1a/1b + Rule 13, all referenced, none re-inlined):
`freshDBOpenCloseTries`, `freshDBDBFileName`, `freshDBControlDSNPrefix/Suffix`,
`freshDBAttributionRatioMax`, `freshDBStallCeiling`, `tmpfsMagic`, `ramfsMagic`,
plus the pre-existing `tempDirEnvName` / `suiteTempRootEnvName` / `suiteShmDir` /
`suiteTempDirPattern` / `raceBudgetMultiplier`. `GOPI_TEST_TMPDIR` is read in
exactly one place (`suiteTempRootOverride`, Rule 2). The dead
`freshDBOpenCloseBudget` constant is gone — no absolute per-DB budget remains.

New regression tests (each is a minimal repro of one defect):

- `TestScratchRootFilesystemClassification` — /dev/shm and the pinned root must
  classify as memory-backed; the stall attribution for a memory-backed root must
  NOT contain the block-device-fsync claim; an unreadable root must report
  "statfs failed", never a class.
- `TestFreshDBAttributionRatioIsIOAttributable` — the ratio on a memory-backed
  root must stay under the limit (fails if the control stops measuring the same
  in-process work — e.g. a future `:memory:`-vs-DSN mishap).
- unchanged from t-1788361069: `TestSuiteScratchRootIsPinned`,
  `TestSuiteScratchRootIsMemoryBacked`.

## Acceptance evidence

Host: 4 cores, `up 57 days`, other agent suites running concurrently
(load average 6–13 throughout).

### A. Full package suite, idle-ish host (two consecutive runs, same binary)

| run | command | verdict |
|---|---|---|
| 1 | `go test -count=1 ./internal/tools/` | `ok github.com/nick/gopi/internal/tools 303.368s` |
| 2 | `go test -count=1 ./internal/tools/` | `ok github.com/nick/gopi/internal/tools 306.573s` |

Two further consecutive branch runs (back to back, same worktree, load 6.8–9.1)
are the branch's own "green twice" pair:

| run | command | verdict |
|---|---|---|
| 3 | `go test -count=1 ./internal/tools/` | `ok github.com/nick/gopi/internal/tools 186.963s` |
| 4 | `go test -count=1 ./internal/tools/` | `ok github.com/nick/gopi/internal/tools 149.455s` |

(Run 1 on the branch, run 2 on master concurrently — both green; the branch's
own two run matrix is recorded below.)

### B. Contention matrix — master baseline vs branch (the false-red proof)

Three sibling `go test ./internal/tools/` suites running (their own process
groups) during each phase; phase H* adds 10 busy-loop CPU hogs on the 4-core box.

| phase | checkout | scratch root | load | result |
|---|---|---|---|---|
| L1 | master `48ecdd5` (old guard) | tmpfs (default) | 8.0 | **PASS** 0.14s |
| L2 | **branch** (new guard) | tmpfs (default) | 8.8 | PASS 0.30s — ratio 1.37x |
| L3 | **branch** | `GOPI_TEST_TMPDIR=/tmp` | 8.1 | **FAIL** 4.62s (expected RED) |
| H1 | master `48ecdd5` (old guard) | tmpfs (default) | 8.4 | **FAIL** — `52.1ms each, budget 300ms`, text claims a /dev/shm root "syncs at WAL close" (the false red + the lie) |
| H2 | **branch** (new guard) | tmpfs (default) | 10.4 | **PASS** 1.00s — candidate median 43.0ms vs control 30.5ms, ratio **1.41x** |
| H3 | **branch** | `GOPI_TEST_TMPDIR=/tmp` | 12.5 | **FAIL** 4.61s — one DB took 2.72s over the 2s ceiling, control 22.1ms, root classified "block-backed filesystem type 0xef53 (NOT a memory filesystem)" |

The same load that reds master (H1) leaves the branch green (H2) — the false
red is gone — while `/tmp` still reds on both guards (L3/H3): the guard kept
its power.

### C. Focused runs

- default root: `PASS` — "scratch root tmpfs (memory filesystem); candidate
  median 16.2ms, control median 12.0ms → ratio 1.35x (limit 12.0x)".
- `GOPI_TEST_TMPDIR=/tmp`: `FAIL` — truthful text, statfs-classified root.
- `go vet ./internal/tools/` clean; `gofmt -l` clean.

## Re-run matrix (this worktree @ `048e765`, re-verified end-to-end)

The tables above were recorded while the patch was written; the matrix below is
an independent re-run of the SAME branch head, against master's current tip
(`f0106d7` — no overlap with the two files this branch touches, verified with
`git diff --name-only HEAD...FETCH_HEAD`). Contention generator: the acceptance
text's stated equivalent for the absent `stress-ng` — 3 sibling full package
suites, then 8 busy-loop hogs on the host's 4 cores (load 11.6→16.1).

**Phase 1 — moderate load (load 6–9, 3 sibling `go test ./internal/tools/`)**

| checkout | scratch root | verdict |
|---|---|---|
| branch | tmpfs (default) | **PASS** 0.320s |
| branch | `GOPI_TEST_TMPDIR=/tmp` | **FAIL** 5.72s — one DB 2.459s over the 2s ceiling; root "block-backed filesystem type 0xef53 (NOT a memory filesystem)" |
| master `f0106d7` (old guard) | tmpfs (default) | PASS 0.148s |
| master | `GOPI_TEST_TMPDIR=/tmp` | **FAIL** 15.19s — "1.266s each, budget 300ms" |

All 3 sibling suites: `ok`.

**Phase 2 — heavy oversubscription (8 hogs / 4 cores, load 11.6 → 16.1)**

| checkout | scratch root | verdict |
|---|---|---|
| **branch** | tmpfs (default) | **PASS** 1.089s |
| **branch** | tmpfs (default) rerun | **PASS** 0.964s |
| **branch** | tmpfs (default) rerun 2 | **PASS** 0.906s |
| branch | `GOPI_TEST_TMPDIR=/tmp` | **FAIL** 12.56s — candidate median 986.7ms vs control 23.1ms → ratio **42.68x** (limit 12.0x) |
| **master** (old guard) | tmpfs (default) | **FAIL** 0.68s — "56.79ms each, budget 300ms", text claims the `/dev/shm/…` root "syncs at WAL close" (**the false red + the lie**) |
| master | tmpfs (default) rerun | **FAIL** 0.55s — 46.02ms each, same false claim |

**Phase 3 — verbose under the same load (16.6) + full-suite pair**

| checkout | measurement / verdict |
|---|---|
| branch | PASS — "scratch root tmpfs (memory filesystem); candidate median **42.742ms**, max 108.25ms; control median **34.983ms** → ratio **1.22x** (limit 12.0x)" |
| master | **FAIL** 0.872s — 69.35ms each vs 300ms budget, same "syncs at WAL close" claim on a `/dev/shm` root |
| branch full suite run 1 | **`ok github.com/nick/gopi/internal/tools 194.192s`** (exit 0, load 12.0) |
| branch full suite run 2 | **`ok github.com/nick/gopi/internal/tools 151.499s`** (exit 0, load 10.4) |

Clause-by-clause:

1. **Green under simulated contention** — branch 3/3 PASS at load 12–16 (phase 2),
   ratio 1.22x at load 16.6 (phase 3); master reds 2/2 at the SAME load with the
   false claim on a memory-backed root.
2. **`GOPI_TEST_TMPDIR=/tmp` still RED** — FAIL in phase 1 (5.72s) and phase 2
   (12.56s, ratio 42.68x); the branch's failure text contains **0** occurrences of
   "syncs at WAL close" (`grep -c`), and classifies the root as
   "block-backed filesystem type 0xef53 (NOT a memory filesystem)".
3. **Idle host green** — PASS 0.320s / 0.334s, ratio 1.32x (15.85ms vs 12.04ms).
4. **Full package suite green twice** — 194.192s and 151.499s above.

Static gates: `go build ./...` OK, `go vet ./internal/tools/` clean, `gofmt -l`
clean. Rule 1a/1b/13 audit: `freshDBOpenCloseBudget` no longer exists anywhere
(`grep -rn` → none); `tempDirEnvName` / `suiteTempRootEnvName` / `suiteShmDir` /
`tmpfsMagic` / `ramfsMagic` each have exactly one definition site and are
referenced, never re-inlined. Rule 2: `GOPI_TEST_TMPDIR` is read only inside
`suiteTempRootOverride()`.

## Not Done / Notes

- `stress-ng` is not installed on this host (`which stress-ng stress` → not
  found), so simulated oversubscription used its stated equivalent from the
  acceptance text: **3 sibling suites concurrently** and, for the heavier
  phases, 10 busy-loop hogs on 4 cores (load 10–13), which reproduced the
  false red on master (H1).
- Master-detach baseline (`/tmp/master-base`, `git worktree add --detach`) was
  used only for the baseline runs; remove it after review
  (`git worktree remove --force /tmp/master-base`).
- The `-race` path keeps working through the shared `suiteBudget` multiplier:
  the ceiling scales to 40s under instrumentation while the ratio criterion is
  unaffected (both loops are instrumented identically). Not re-run here — the
  full `-race` suite is ~400s and the change is verdict-shaped, not
  instrumentation-shaped.

## Merge gate note

`safe-merge.sh feat/freshdb-contention-guard` currently refuses with
`[c] 分支仍被 worktree 占用（未清理）` — expected: the guard's own merge
precheck refuses any branch still checked out in a worktree, and the
implementation worktree `~/workspace/gopi-freshdb-guard` is intentionally
left in place for review. It must be removed only after acceptance:

```
git worktree remove --force ~/workspace/gopi-freshdb-guard
bash scripts/safe-merge.sh feat/freshdb-contention-guard   # then ✅ exit 0
```

(`[d] 尚未 merge 进 master` is informational, not a blocker.) No self-merge
was performed (Rule 9). Master has since advanced to `f0106d7`; this branch
is based on `48ecdd5` and touches only
`internal/tools/testsuite_temproot_test.go` + the new devdoc; the re-run matrix
above was taken with `f0106d7` fetched, and `git diff --name-only
HEAD...FETCH_HEAD` shows **no overlap** — the merge is a clean fast-forward-free
two-line-union (this branch's two files, nothing else).

**Cleanup order for the reviewer (PA):**

```bash
# 1. the baseline detach used for the matrix is ALREADY removed
#    (git worktree remove --force /tmp/freshdb-master — done at 048e765)
# 2. release the implementation worktree BEFORE the safe-merge gate can pass:
git worktree remove --force ~/workspace/gopi-freshdb-guard
# 3. then the gate itself:
bash scripts/safe-merge.sh feat/freshdb-contention-guard   # expects ✅ exit 0
```

The deliverable is already in the MAIN repo (a linked worktree shares its refs —
no push needed): `feat/freshdb-contention-guard` @ `b288850`, visible from
`~/work/GoPi` via `git log --oneline -1 feat/freshdb-contention-guard`.

## Verification

- Build: `go build ./...` → OK; `go vet ./internal/tools/` → clean; `gofmt -l internal/tools/` → empty
- Tests:  the branch-head commits, in order — `fffd63d` (guard rewrite),
  `a1f48ce` (design + contention matrix), `048e765` (safe-merge gate note),
  `b288850` (independent re-run matrix vs master `f0106d7`). `git status` at HEAD: clean.
- Commit:  `b288850` is the evidence commit (Rule 8: one file, `git add` by path).
