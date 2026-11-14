<!-- doc-class: historical -->
# Task Report: peak-orphan test files + the ledger busy-timeout defect behind the cmd/agent red

**Task**: master's `internal/config` / `cmd/agent` test binaries allegedly fail to BUILD because
two orphan `*_test.go` files reference peak-hours config identifiers whose production half exists
on no branch.
**Branch**: `fix/peak-orphan-tests-doc` — a **fresh branch off master** `e52ff94e`, in the linked
worktree `/home/user/workspace/gopi-peak-orphans-doc` (Rule 6a, Rule 23: the sibling session's
`fix/peak-orphan-tests-green` worktree was left untouched).
**Commits**: `e69144a5` (the option-B record) + a second commit with the ledger-DSN fix below.

## Option taken: B — the orphan tests are deleted (and were already deleted on master)

The epic's premise ("master's test binaries do not build") is **stale, and the fix is already on
master**. `82f44223` deleted both files *after* the archive merge the epic was written from:

```
$ git merge-base --is-ancestor 82f44223 master; echo $?         → 0    (the fix IS on master)
$ git merge-base --is-ancestor 03526655 82f44223; echo $?       → 0    (deletion descends from the archive tip)
$ git ls-files internal/config/peakhours_flag_spelling_test.go cmd/agent/merger_verify_peak_child_test.go
(empty — both orphan files are absent from master's tree)
$ go test -c -o <tmp>/agent.test ./cmd/agent/                   → exit 0   (the binary DOES build)
$ go test -c -o <tmp>/config.test ./internal/config/            → exit 0
```

**Why not option A (land the production half)**: the production half was deleted *on purpose*
(`bb0c4c1a` "remove the feature, not fix it again", `devdoc/peak-guard-removal.md`), and master's
own `cmd/agent/peak_guard_removal_test.go` scans the tree for 15 removed symbols
(`ParsePeakFlags`, `PeakHoursEnv`, `IgnorePeakHoursEnv`, `PeakHoursFlag`,
`IgnorePeakHoursFlagSpelling`, …). Landing the production half would turn that gate red — option A
is not the cheap route, it is the route the operator already rejected.

Provenance chain (git-proved, not inferred): `69145a20` adds → `bb0c4c1a` deletes → `03526655`
archives the uncommitted vp-a03d residue → `c1cdf311` re-introduces it in the archive merge →
`82f44223` deletes it again **on master** → `e52ff94e` (master tip).

## Second defect, root-caused (the reason `go test ./cmd/agent/` was still red)

With the build question settled, the acceptance command still failed. Reproduced on my branch AND
on a **control clone of pristine master at the same sha** (differential discipline, memory #2553) —
so it is master-preexisting, not branch-attributable:

```
--- FAIL: TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound
    task_lifecycle_tick_test.go:527: backdate t-zd-confirmed: database is locked (5) (SQLITE_BUSY)
```

**Root cause (5-why)**: a test fixture opens the ledger with a bare `sql.Open("sqlite", path)`.
Production opens its ledger through `openDB` (`path+"?_txlock=immediate"` + `PRAGMA
busy_timeout = dbBusyTimeoutMSec`) and puts the file in WAL (`enableWAL`). A bare DSN carries **no
busy timeout** and **no WAL**, so the fixture's write gets an immediate `SQLITE_BUSY` instead of
queueing behind the resident merger daemon the same test starts. The test's *logic* is fine — its
*connection contract* is not production's.

**Repro matrix** (same test, isolated run, 3 runs per DSN variant — the bare DSN is not a "flake"
in the sense of being random noise; it is a defect whose hit rate is load-dependent):

| DSN on the test readers | runs 1/2/3 |
|---|---|
| bare `path` | LOCKED / ok / LOCKED |
| `busy_timeout(15000)` only | ok / LOCKED / ok |
| `journal_mode(WAL)` only | LOCKED / LOCKED / ok |
| `busy_timeout(15000)` + `journal_mode(WAL)` | ok / ok / ok |

Note the **writer** side is what failed under WAL-only/partial variants (`backdate …`), which is why
the timeout alone is not sufficient: the fixture both seeds and reads the row.

**Fix (Rule 14 — one definition, referenced, never re-typed)**:
`internal/tools/ledger_dsn.go` exports `LedgerDSN(path)`, built from the SAME constant production
uses (`dbBusyTimeoutMSec`) and stating production's WAL mode. The measured-red readers reference it:

```go
db, err := sql.Open("sqlite", tools.LedgerDSN(path))   // cmd/agent/cli_tasks_zombie_dispose_test.go (2 sites)
```

## Verification

```
$ go build ./...                                  → exit 0
$ go vet ./internal/tools/ ./cmd/agent/           → exit 0 (no "undefined:")
$ gofmt -l internal/tools/ledger_dsn.go cmd/agent/cli_tasks_zombie_dispose_test.go → (clean)
$ go test ./cmd/agent/ -count=1 -run '^TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound$'
    fixed: 5/5 ok        (bare DSN before the fix: 2 of 3 locked)
$ go test ./cmd/agent/ -count=1 -run 'TestZombie|TestKilledWorkerDaemon|TestMergerTick|TestDispose'
    → ok  github.com/nick/gopi/cmd/agent  11.173s   (FAM_EXIT=0)
```

**THE ACCEPTANCE GATE — green, and A/B-proved.** Run on a clone of the branch tip
(`667b272c`, gitignored `bin/gopi` rebuilt in-tree), scratch root `GOPI_TEST_TMPDIR` on the
`/home` partition, daemon triple stripped:

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
$ GOPI_TEST_TMPDIR=<off-/> go test ./internal/config/ ./cmd/agent/ -count=1
ok  	github.com/nick/gopi/internal/config	0.119s
ok  	github.com/nick/gopi/cmd/agent	357.672s
ACC2_EXIT=0                        ← the acceptance command exits 0, both packages
```

The **control clone of pristine master at the same sha `e52ff94e`** (no fix), run under equal
load on the same host, failed with exactly the defect this branch fixes:

```
--- FAIL: TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound (2.80s)
    task_lifecycle_tick_test.go:557: status t-zd-confirmed: database is locked (5) (SQLITE_BUSY)
--- FAIL: TestMergerDaemon_DisposesZombieWithZeroWorkerProcesses (4.45s)
FAIL	github.com/nick/gopi/cmd/agent	525.364s   CTL_EXIT=1
```

So the delta is measured, not asserted: **branch green, master control red on the same tests.**

### Trap met while measuring (worth recording)

The first branch-vs-control run showed **9 branch-only FAILs and 2 control FAILs** — and all 9
were the suite's own staleness guard talking, not a regression:

```
.../bin/gopi (mtime 17:35:23) is OLDER than .../internal/tools/ledger_dsn.go (mtime 17:53:03) —
this test drives the BUILT binary … Rebuild it … and re-run.
```

My own edit made the source newer than the in-tree binary. Rebuilding turned all 9 green; none of
them was a code failure. (`bin/gopi` is gitignored, so a fresh worktree has no binary at all and
this same family **skips** instead — the mirror-image false green.)

## Not Done (honest)

- **Six more bare-DSN test readers remain** in `cmd/agent` (`cli_resume_worktree_test.go`,
  `cli_serve_stale_test.go`, `cli_tasks_attach_index_test.go`, `cli_tasks_singleton_guard_test.go`,
  `cli_tasks_worker_writeback_test.go`, `cli_version_test.go`). They pass today; converting them is
  the same one-line reference but each needs the `internal/tools` import. Reported as a dispatchable
  follow-up (Rule 26), not silently fixed here.
- I did **not** change how the ledger is created (`journal_mode` is production's own persistent
  header decision — a test must not mutate it; the fix is connection-local on the reader side).
- No worktree cleanup performed; the sibling `fix/peak-orphan-tests-green` worktree was not touched.
- **`internal/standards` is red, and it is pre-existing on master** — measured on BOTH the branch
  and the pristine-master control with identical output (`NEW=2`, the same stale `Rule 16`
  baseline entry `cmd/agent/main.go:954`). The branch's names do not appear in either gate report
  (`grep -c 'ledger_dsn\|cli_tasks_zombie_dispose'` → 0), so the branch adds no finding. Reported,
  not fixed (a separate task per Rule 26).

## Declaration (Rule 6d)

- Branch: `fix/peak-orphan-tests-doc` (fresh off master `e52ff94e`)
- Recovery: `git show fix/peak-orphan-tests-doc:devdoc/peak-orphan-tests-and-ledger-busy-dsn.md`
- The commits are in the main repo (the linked worktree shares refs — no push needed); own branch
  not merged.
