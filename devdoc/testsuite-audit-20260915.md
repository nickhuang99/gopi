<!-- doc-class: historical -->
# Testsuite audit 2026-09-15 — findings and fixes

Audit of the whole GoPi test tree (544 `*_test.go`: `internal` 386, `cmd` 151,
`testsuite` 5, `archive` 2), prompted by tasks that stall while their
verification suite runs. Every claim below is measured on this host, not
inferred from reading code.

## Finding 1 (FIXED here) — the scratch GC could never drain permission-blocked residue, so the tmpfs filled up and tests failed with "database or disk is full"

**Symptom.** `/dev/shm` at 100% (7.8G used, 0 free). Suites then fail with
`database or disk is full (13)` and `no space left on device` — which reads as a
slow or hanging suite but is a full tmpfs. `internal/agent`'s four slowest tests
(524s combined in the 2026-09-14 budget run) and the whole 617s package time are
a *consequence* of this, not a property of those tests.

**Root cause (proved by differential).** A test fixture drops a directory to
mode 0 (`internal/testutil/scratchroot_test.go` `TestGCUnreadableScratchEntryDoesNotPanic`,
`os.Chmod(locked, 0)`) and restores it in `t.Cleanup`. A suite killed before the
cleanup ran (the load-94 kill incident) leaves the mode-0 directory behind for
good. `internal/scratch/gc.go` deleted a collectable entry with a bare
`os.RemoveAll`, so ONE mode-0 descendant made the whole entry undeletable, every
pass, forever:

```
$ gopi scratch-gc --dry-run
would-remove /dev/shm/gopi-test-scratch/suite-internal-testutil-run-3663106447 age=17h3m7s
$ gopi scratch-gc
scratch: GC could not remove .../suite-locked: permission denied
scratch-gc role=cli removed=0
```

Minimal-sample differential (Rule 10.4): `chmod 0700` on that single directory,
nothing else changed:

```
$ gopi scratch-gc
scratch-gc role=cli removed=1
$ ls -ld .../suite-internal-testutil-run-3663106447
No such file or directory
```

**Fix.** `removeCollectable()`: after a failed `RemoveAll` on an entry the shared
collectability predicate ALREADY approved, restore owner-only access
(`RepairDirPerm`/`RepairFilePerm`) down the subtree and retry once. The authorising
probe is `entryReadable()` — "can the sweep see inside the entry" (open +
`Readdirnames(1)`), **not** `Lstat`: Lstat only needs the parent's execute bit, so
it succeeds for an entry whose own mode is 0o000, and that shape is pinned as
KEEP by `TestGCUnreadableScratchEntryDoesNotPanic` (Rule 15 — never guess about a
tree you cannot inspect). Contract preserved and newly pinned by
`TestSweepHomeKeepsAnEntryItCannotMeasure`; blast radius pinned by
`TestSweepHomeLeavesPermissionBlockedForeignResidueAlone` (repair runs only after
the collectability predicate approved the entry, so a foreign directory is never
touched, nor chmod-ed on the way past); red-proof half
`TestRemoveAllCannotDrainAPermissionBlockedEntry` pins the mechanism itself.

Red-before/GREEN-after on the same command (`-run PermissionBlocked`):
pre-fix `swept 0 entries, want 1` + `permission denied`; post-fix 5/5 PASS.
`./internal/scratch/` green; `./internal/testutil/` green except
`TestWorkerOwnershipPointerPresent`, which is **pre-existing on master** (3 devdocs
missing the worker-ownership pointer line — Finding 4).

## Finding 2 (FIXED here) — test binaries that did not COMPILE on master

`go vet ./...` and `go test -run XXXNONE` on master:

```
internal/config/peakhours_flag_spelling_test.go:24: undefined: IgnorePeakHoursFlagSpelling
cmd/agent/merger_verify_peak_child_test.go:71: undefined: config.PeakHoursEnv
```

Two packages' test binaries failed to build, so `go test ./internal/config/` and
`go test ./cmd/agent/` ran **nothing at all** (a build failure is not a skipped
test — it is an unverifiable package; the slower and larger of the two is
`cmd/agent`, 520s of test time and 151 test files).

Cause, established from history: `bb0c4c1a remove(peak-guard): delete the
peak-hour cost guard and its configuration surface` deleted the API; commit
`03526655` ("archive(vp-a03d probe): preserve the two staged test files the
verification run left uncommitted") then checked in two stray test files that
reference the DELETED API. Their intended replacements
(`internal/config/peak_guard_removed_test.go`, `cmd/agent/peak_guard_removal_test.go`)
already exist and assert the opposite — the symbols are absent, by exhaustive
scan, with the two strays named by path in that scan's allow-list.

**Fix.** Both stray files deleted (the removed API they exercise is gone by
decision, and the removal's own absence proofs already cover the surface they
were written for). Green-after: `go vet ./internal/config/ ./cmd/agent/` clean,
`go test ./internal/config/` ok, and both removal proofs
(`TestPeakGuardSymbolsAreAbsentFromTheTree`,
`TestPeakGuardWireTokensAreAbsentOutsideHistoryAndBackwardTolerance`) now
actually RUN and PASS instead of being unreachable behind a build failure.

**Measured before/after (the payoff).** With the strays gone, `cmd/agent` — the
package that previously could not build its test binary at all — now RUNS and
passes: `go test -count=1 -timeout 1500s ./cmd/agent/` →
`ok github.com/nick/gopi/cmd/agent 183.367s`, wall clock 3m04s, 31% CPU
(`/usr/bin/time -v`: 45.05s user + 12.39s system against 184s elapsed — i.e. even
this package is mostly WAITING, the same starved-not-slow signature as the rest
of the tree). So the ~520s previously attributed to this package by the budget
artifacts was the compile-and-fail cycle of an unverifiable package, not work:
151 test files were producing zero verified assertions, and after the fix the
suite both runs and fits the budget.

The files remain in history (`git show 03526655:<path>`), which is where the
Rule-6a-era work that produced them is preserved.

## Finding 3 (REPORTED) — worktree / branch / ad-hoc tmpfs residue

- **9 linked worktree registrations** outside my own. The sweeper is not the
  problem — the DECLARATION is. `gopi task sweep-worktrees` (dry-run) for the
  whole repo answered `repos=2 reclaimed=0 would-reclaim=1`:

  ```
  report   /dev/shm/gopi-verify-01a0a36a/master-base (outside every declared root — not swept; declare the repo's worktree_root to bring it into policy)
  report   /dev/shm/gopi-verify-01a0a37d-wt        (outside every declared root — …)
  keep     /home/user/workspace/gopi-ab-base       (younger than the threshold (2h1m0s < 24h0m0s))
  reclaim  /home/user/workspace/gopi-wt-residue    (task 01a09d0a-… is completed, no live holder, clean, age 3h20m0s)
  ```

  So 8 of 9 are one of two ordinary classes — "merged into master, nobody owns
  it, still inside the 24h age window" (7) and "reclaimed by the sweeper once
  its task row went terminal" (1) — while the **/dev/shm ones are UNGOVERNABLE
  BY CONSTRUCTION**: they sit outside every declared root, and `worktree_root`
  (settings.json `repos.<repo>.worktree_root`, default `<home>/workspace`) is
  ONE path per repo, so it cannot also cover the memory base that
  `internal/testutil` itself picks (`ShmDir=/dev/shm`; the suites create linked
  worktrees there for tmpfs speed because a block-backed scratch root measured
  a 2.5x wall-clock tax). The memory-base sweeper
  (`internal/testutil/legacy_residue.go` → `scratch.GCLegacyScratchResidue`)
  matches only names with BOTH a `gopi-` prefix AND a `suite-tmp-` infix and
  only DIRECTORIES, so `gopi-verify-<task-id>`/`-wt`/`-base`/`-prefix` (a
  worktree REGISTRATION, not a scratch entry) is claimed by neither sweeper.
  Fix shape (deferred, see "Not fixed" below): extend the memory-base residue
  namespace to the verification-worktree family, or treat the resolved memory
  base as an implicit declared root when the scratch home resolves onto it —
  coordinated with `worktree_sweep`'s ownership rule (a dead registration's
  branch deletion must be deferred until AFTER `git worktree prune`).
- **`/dev/shm` ad-hoc residue outside the scratch namespace is likewise
  permanent**: `gocache-01a098a1` (308M, 2 days), `mprobe.*` (69M, 3 days),
  `orphan-e2e.JBGyyK` (42M), plus dozens of ad-hoc logs/backups/db snapshots
  written directly into `/dev/shm` by verification runs. Measured and reclaimed
  during this audit: ~614MB, which took `/dev/shm` from 100% to 93%.
- **30 `archive/*` tags** with 1 remaining unmerged branch
  (`fix/worktree-residue-closeout` — Rule 9 step 3 not completed).

## Not fixed (honest) — deliberately left for a fresh task

Two items in this audit are narrow, self-contained and NOT done here:

1. **The ungovernable-worktree family above** (Finding 3's /dev/shm class). It
   needs a settings/policy change plus a sweeper-namespace change and its own
   regression test; it touches `internal/tools/worktree_sweep.go` (which has an
   ownership rule around deferred branch deletion) and is not a change to make
   inside an audit branch at the end of a session.
2. **The temp-root is still the ambient `/tmp` on the `/` partition.** The
   measured `/tmp` residue above is the *symptom*; the mechanism is that the
   settings.json `tmpdir` key is not set, so children inherit `os.TempDir()`.
   Because the disk-pressure half of "testsuites run forever" is exactly this,
   the fix is Rule 22's explicit layer, not `rm -rf /tmp`: set `tmpdir` to the
   drained scratch tree, verify OFF-`/` by device id (`stat -c %d` vs `/`),
   and add a regression test that runs a suite with `tmpdir` set and asserts
   no new file appears under the `os.TempDir()` fallback. Left undone here
   because it is a settings/policy change whose acceptance is a leak
   measurement on a host I must not reconfigure mid-audit (Rules 22, 25).

3. **`TestDaemonLifetimeBoundChildSurvivesWhileItsHolderLives`** — observed red
   once in this worktree's `internal/testutil` run and NOT reproduced (green on
   the immediately following run). It is a daemon-lifetime test, i.e. exactly the
   class that flips under host load (memory #2071: `exit=-1` with no
   `session_end` = SIGTERM handling losing to load, load 10+). Recorded as a
   flake candidate, never as a defect, and never diagnosed by assumption.

## Finding 4 (FIXED here) — guard red on master

`TestWorkerOwnershipPointerPresent` failed on master: `devdoc/binary-summary-guard.md`,
`devdoc/merger-build-gate-module-download-abstain.md`, `devdoc/task-liveness-ticktock.md`
teach a worker-launch command without the `WORKER-OWNERSHIP: OPERATOR-ONLY`
pointer line (they quote `gopi worker` / `gopi task serve` shapes, so the gate
requires the pointer; none of the three is in `workerOwnershipDismissed`).

**Fix.** The gate's own prescribed form inserted in the form each file's
language needs — markdown comment, first line:
`<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->`.
Same one-line class as the gate's own message; no dismissal entry added, so the
three files stay AUDITED rather than quietly un-gated. Green-after:
`go test -run TestWorkerOwnership ./internal/testutil/` ok.

## Finding 5 (REPORTED) — leftover diagnostic test files, 380 `t.Skip` sites

- `internal/tools/wip_worktree_repro_test.go`, `zz_redproof_reclaim_promise_test.go`,
  `zz_wtrecycle_diag_test.go` (env-gated live diagnostic, documented as kept
  deliberately), `cov_*_test.go` — the `cov_`/`wip_`/`zz_` naming is
  audit-artifact naming (15 files repo-wide), not a package convention.
- 380 `t.Skip` sites; the large majority are legitimate capability probes
  (`ctags`/`git`/`procfs`/`root`), and `internal/stylegate` already gates that
  every skip carries a literal reason. The load-bearing risk is the skip that
  SKIPS A GUARD: a missing capability turns a regression guard green-vacuous
  (documented at `devdoc/test-outtxt-leak.md`, where an e2e skips without
  `bin/gopi`). `scripts/test-budget.sh` already fails rather than skips when the
  binary is absent — that is the right shape, and it is not universal.

## Finding 7 (MEASURED, nothing to fix) — the named e2e suites are FAST and leak NOTHING

The epic names "especially those long running, e2e testsuites, those using system
resource and not releasing after running, even including creating git worktree or
branches". Measured, that class is clean here — which is the point of measuring it
before "fixing" it:

- `go test ./testsuite/` (the three-mode E2E smoke suite: task / chat / web, each
  driving the REAL `bin/gopi` subprocess against a scripted mock LLM):
  **1.888s, 12 tests, 0 skips, all PASS**. Not a slow suite by any reading.
- `go test ./internal/tools/ -run 'Worktree|WorktreeSweep|DeadRegistration|Cleanup|Reclaim|OffPolicy'`
  — the tests that create linked worktrees and branches — **8.632s**, with
  before/after counts IDENTICAL: `git worktree list` 13→13, `git branch` 4→4,
  `find /tmp -maxdepth 1` 1988→1988. So the worktree-creating tests do release
  what they create; the residue in Findings 3/6 is NOT produced by the suites'
  own bookkeeping but by ad-hoc runs outside any namespace.
- The one e2e correctness gap worth naming — and it is an ASYMMETRY, not an
  oversight: `testsuite/` SKIPS itself when `bin/gopi` is absent
  (`internal/testutil.GopiBin`: `t.Skipf("gopi binary not found at %s — run
  scripts/e2e_smoke.sh first")`, and the package doc states it: "a plain
  `go test ./...` skips these tests when bin/gopi is absent"). `bin/` is
  UNTRACKED (`git ls-files bin/` is empty), so on a fresh checkout the whole
  three-mode E2E reports green `ok` while executing ZERO subprocess assertions.
  The same harness is NOT uniform about this: `cmd/agent`'s sweep-zombies E2E
  FAILS on purpose (`t.Fatalf("sweep-zombies E2E needs a gopi binary: set
  GOPI_BIN …")`, with the comment "a skipped E2E reads as a [pass]"),
  `scripts/test-budget.sh` fails rather than skips, and `scripts/e2e_smoke.sh`
  builds the binary first. So the load-bearing entry points are guarded and only
  the bare `go test ./testsuite/` path is not — recorded here as the shape to
  copy (fail, don't skip, when the missing capability is the thing under test),
  not as a change worth making inside an audit branch.

## Finding 6 (REPORTED) — ledger-DB copies parked in /tmp (ad-hoc investigation residue)

`/tmp` is NOT a tmpfs on this host (`findmnt` reports ext2/ext3, device 2051 =
the `/` partition), so it cannot exhaust RAM. It is nevertheless 4.0G of the
86%-full boot device, and the shape is test/verification residue:

- age histogram: 127 entries <1d (438MB), **1,310 entries 1–7d (2,429MB)**, 548
  entries >7d (21MB) — i.e. ~2.4G of week-old, never-reaped files.
- the largest are all 80MB copies of the scratch LEDGER database
  (`tallarch.db`, `probe-ledger.db`, `livecopy.db`, `led.db`, `gtc-e2e.db`,
  `dryrun-verdict.db`, `lp-proof.db`, `memory.db.bak-060349`, 76M
  `tab-rework-check-xHInynmz`, 82M `tmp.DWLw7pU4q2`) plus hundreds of
  `*_test.go` / `*.bak` scratch copies of source files.

Nothing reaps `/tmp`: the scratch age GC is scoped to the scratch home by
construction (Rule 22's "namespace-limited, never a global delete"), and these
paths are not in that namespace. Rule 22 governs this class — a temp workspace
must be provably off the `/` partition, and the settings table has a `tmpdir`
key that gopi injects into its OWN environment so children inherit it — so the
actionable half is to configure `tmpdir` onto the (now drained) scratch tree,
not to add a `/tmp` reaper.

## Budget/uptime context (why "tasks wait for testsuites")

From the 2026-09-14 budget artifacts
(`<scratch>/queue/test-budget/stamps.log`, `last.json`): 3,994 tests, 2,569s of
test time, per-package sums `internal/agent` 617s, `internal/testutil` 532s,
`cmd/agent` 520s, `internal/tools` 426s, against a per-package budget of 25m and
a documented quiet-host suite time of 189–291s that degrades to >1123s at load
42–63. So the wall-clock a task waits for is dominated by host load plus the
full-tmpfs failures above, not by any single pathological test.

## How to re-run this audit (the cheap gates, ~5 minutes)

Run these BEFORE hunting for a "slow test" — on this host the first two have
historically produced the largest, least-obvious defects, and both are seconds
cheap:

```bash
# 1. Dead packages: a test binary that does not COMPILE is not a skipped test
#    (it is an unverifiable package, and it reports green in the aggregate).
go vet ./... 2>&1 | grep -v '^#'
go test -run XXXNONE ./... 2>&1 | grep -iE 'FAIL|undefined|build failed'

# 2. Disk, not CPU — the failure mode that LOOKS like a slow suite.
df -h /dev/shm / ; findmnt -no FSTYPE,SOURCE /tmp   # is /tmp on / or its own fs?

# 3. Is the scratch GC actually draining, or silently reclaiming zero?
gopi scratch-gc --dry-run | tail -5 ; gopi scratch-gc | tail -3   # want removed=N>0

# 4. Resource residue: worktrees, archive tags, stray branches, temp files.
git worktree list | wc -l ; git tag -l 'archive/*' | wc -l
gopi task sweep-worktrees --dry-run | tail -20      # names every unswept root

# 5. Budget artifacts — separate STARVED from SLOW by measurement.
tail -20 <scratch>/queue/test-budget/stamps.log     # elapsed + load before/after
```

Reading rule for the last one: a package whose tests SUM to a couple of minutes
but whose wall clock is ten is starved by host load, and its `FAIL … 600.3xx s`
verdict is a load detector, not an assertion failure (Rule 15).
