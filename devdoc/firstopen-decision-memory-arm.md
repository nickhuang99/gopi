<!-- doc-class: historical -->
# Task Report: the first-open decision table's memory arm is MEASURED, not assumed

Branch `fix/firstopen-decision-memory-arm`, base `master 9ce8e159`.
Files: `internal/tools/firstopen_sync_test.go` (test), this devdoc. **No production file changed.**

## Completed

- `TestFirstOpenSyncModeDecisionTable`'s **memory-filesystem-ledger** arm no longer assumes its
  directory is memory-backed. It asks the *same* classifier production asks —
  `internal/scratch.MemoryFilesystem(memoryDir)` — about **its own** `t.TempDir()` directory
  (Rule 22: a path spelling proves nothing) and derives its behaviour from that measurement:
  - measured **memory-backed** → it asserts `ledgerSyncModeWorkingFS` ("OFF"), exactly as before;
  - measured **not** memory-backed (or unmeasurable) → it **skips**, with a literal, greppable reason
    `FIRST_OPEN_MEMORY_ARM_SKIPPED: … it is a block-backed filesystem type 0xef53, not a memory
    filesystem (measured on <dir>)` (Rule 15: the arm reports what it measured, never a silent pass).
- The **device-filesystem-ledger** and **unmeasurable-directory** arms are unchanged in intent (same
  paths, same expectations, same `why` text); only the memory arm's `why` was retitled
  "tmpfs" → "memory filesystem" to match what is now measured.
- The case is **not** deleted and `firstOpenSyncMode` is **not** weakened —
  `internal/tools/memory.go` is byte-identical to master (`md5 2621ab3346d4fc4cd892307d484982c5`).
- New named constants (Rule 1b) carry the strings: `firstOpenMemoryArmSkipReason`,
  `firstOpenMemoryArmNotMemoryFmt`, `firstOpenMemoryArmUnmeasurableFmt`.
- `//stylegate:ignore` marker on the skip, mirroring the sibling test's idiom (the reason is the
  filesystem type measured at run time — dynamic by construction). The skip's **first argument is a
  const literal**, so the stylegate test-skip rule accepts it on the literal path (the marker is
  belt-and-braces for reviewers).

## Files

- `internal/tools/firstopen_sync_test.go` — the arm's measurement + skip (+ 3 named constants, +1 import).
- `devdoc/firstopen-decision-memory-arm.md` — this report.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK; `gofmt -l` on the changed file → empty.
- Acceptance:
  `go test -count=1 -run '^TestFirstOpenSyncModeDecisionTable$' -v ./internal/tools/` → **PASS**, with
  the memory subtest `--- SKIP` naming the measured filesystem (block-backed root, the shipped host).
- Neighbours: `go test -count=1 -run 'TestFirstOpen|TestTasksSchemaBuildIsEquivalentAcrossPaths' ./internal/tools/` → `ok … 79.652s`.
- Commit: `8b9c6bda fix(tools): measure the memory-filesystem arm of the first-open decision table`
  (+ this devdoc's commit).

## Evidence (self-filed)

**1. The host's scratch geometry (Rule 22 measurement, not spelling).**

```
$ stat -f -c '%T %d' /dev/shm /dev/shm/gopi-test-scratch /tmp /home/user/workspace/gopi-test-scratch
tmpfs 2011180
tmpfs 2011180
ext2/ext3 9305603
ext2/ext3 47566947
```
`/home/user/workspace/gopi-test-scratch` (the suite's scratch home) is `ext2/ext3` = **0xef53**,
device 47566947. `TMPDIR=/dev/shm/gopi-test-scratch/tmp`, `GOPI_TEST_TMPDIR` unset. The suite itself
says so on stderr: `the ledger sandbox …/suite-ledger/memory.db is on block-backed filesystem type
0xef53 (device-backed)`.

**2. RED before the change** — the same worktree, pristine master 9ce8e159
(`go test -count=1 -run '^TestFirstOpenSyncModeDecisionTable$' -v ./internal/tools/`):

```
=== RUN   TestFirstOpenSyncModeDecisionTable/memory-filesystem-ledger
    firstopen_sync_test.go:110: firstOpenSyncMode("…/gopi-test-scratch/suite-tools-run-3058865635/
        TestFirstOpenSyncModeDecisionTable2389982594/001/memory.db") = "NORMAL", want "OFF" —
        the schema is built into a fresh file on tmpfs, where a sync protects nothing
--- FAIL: TestFirstOpenSyncModeDecisionTable (0.00s)
    --- FAIL: TestFirstOpenSyncModeDecisionTable/memory-filesystem-ledger (0.00s)
    --- PASS: TestFirstOpenSyncModeDecisionTable/device-filesystem-ledger (0.00s)
    --- PASS: TestFirstOpenSyncModeDecisionTable/unmeasurable-directory (0.00s)
FAIL	github.com/nick/gopi/internal/tools	0.768s
```

**3. GREEN after the change** — same command, committed tree, same block-backed scratch root:

```
=== RUN   TestFirstOpenSyncModeDecisionTable/memory-filesystem-ledger
    firstopen_sync_test.go:152: FIRST_OPEN_MEMORY_ARM_SKIPPED: this host cannot show the
        memory-filesystem arm of the decision: it is a block-backed filesystem type 0xef53,
        not a memory filesystem (measured on …/gopi-test-scratch/suite-tools-run-678215060/
        TestFirstOpenSyncModeDecisionTable118265773/001) — the ledger there keeps production's
        durable mode, so the relaxed expectation cannot be asserted here
--- PASS: TestFirstOpenSyncModeDecisionTable (0.00s)
    --- SKIP: TestFirstOpenSyncModeDecisionTable/memory-filesystem-ledger (0.00s)
    --- PASS: TestFirstOpenSyncModeDecisionTable/device-filesystem-ledger (0.00s)
    --- PASS: TestFirstOpenSyncModeDecisionTable/unmeasurable-directory (0.00s)
PASS	github.com/nick/gopi/internal/tools	0.496s
```

**4. The arm is NOT vacuous — where a memory filesystem exists it ASSERTS, does not skip.**
A tmpfs scratch home was constructed (the suite honours `GOPI_TEST_TMPDIR`, the documented override):

```
$ mkdir -p /dev/shm/gopi-memarm-probe && stat -f -c '%T' /dev/shm/gopi-memarm-probe
tmpfs
$ GOPI_TEST_TMPDIR=/dev/shm/gopi-memarm-probe go test -count=1 -run '^TestFirstOpenSyncModeDecisionTable$' -v ./internal/tools/
    --- PASS: TestFirstOpenSyncModeDecisionTable/memory-filesystem-ledger (0.00s)   ← asserted OFF, no SKIP
    --- PASS: TestFirstOpenSyncModeDecisionTable/device-filesystem-ledger (0.00s)
    --- PASS: TestFirstOpenSyncModeDecisionTable/unmeasurable-directory (0.00s)
PASS	github.com/nick/gopi/internal/tools	0.246s
```

**5. Mutation red-proof — the assertion still catches the regression it defends.**
With the production classifier disabled (`memoryFilesystemLedger` → `… return err == nil && mem && false`,
a temporary mutation removed with `cp` from a backup), on the tmpfs scratch home:

```
--- FAIL: TestFirstOpenSyncModeDecisionTable/memory-filesystem-ledger (0.00s)
    firstopen_sync_test.go:156: firstOpenSyncMode("/dev/shm/gopi-memarm-probe/…/001/memory.db") =
        "NORMAL", want "OFF" — the schema is built into a fresh file on a memory filesystem,
        where a sync protects nothing
FAIL	github.com/nick/gopi/internal/tools	0.020s
```
`memoryFilesystemLedger` was restored byte-exact afterwards (`md5sum internal/tools/memory.go` =
`md5sum` of `git show master:internal/tools/memory.go` = `2621ab3346d4fc4cd892307d484982c5`).

**6. Rule 3 / acceptance 3 — only test+doc files in the diff.**
`git diff --name-only master...HEAD` → `internal/tools/firstopen_sync_test.go` (+ this devdoc).

**7. Acceptance 4 — stylegate, no new hits, measured against master.**
`go run ./internal/stylegate/cmd/stylegate -list` (branch vs master):

```
branch {'errorf-verb': 8, 'ignored-error': 205, 'test-skip': 1, 'test-sleep': 5}
master {'errorf-verb': 8, 'ignored-error': 205, 'test-skip': 1, 'test-sleep': 5}
```
Identical — 0 new findings anywhere. `findings in firstopen_sync_test.go: []` on both (the JSON report
was filtered per file). `-rule test-skip` prints the same 1 site in both trees (not this file).

**8. Worktree state.** `git status --porcelain` in the run's worktree → empty.

## Not Done

- **Not fixed here (out of scope, still red on master):** `internal/stylegate`'s own pre-existing
  findings — 8 `errorf-verb` + 1 `test-skip` — reported in §7's identical counts so nobody reads my
  change as having touched them. Also still red/out of scope:
  `TestMergeBuildGate_GoCacheLeavesNoScratchCache` (the same scratch-home skew, a different subject).
- The memory arm **cannot assert** on this host's default scratch root: the operator's scratch home is
  block-backed by decision (524156af / `devdoc/scratch-shm-role-split.md` §7), so the "OFF wherever a
  memory ledger is measured" half of the decision is exercised only via the `GOPI_TEST_TMPDIR` tmpfs
  run in §4 — not by the default invocation. That is the environment's limit, not the test's.
- The skip's reason is dynamic (it quotes the measured filesystem), so it carries a
  `//stylegate:ignore`. If stylegate gains a structured "skip reason is a const format string" form,
  this site could drop the marker.

## Superseded (2026-09-18, branch `fix/master-red-scratch-skew`)

The **skip** this report designed is superseded: measure-on-a-fixture-and-skip left the
memory→relaxed half of the decision UNTESTED on every block-backed host (this host
included — the skip fired here). The arm is now **derived**: `firstOpenMemoryScratch()`
measures a real memory filesystem from a candidate list (`/dev/shm`, `/run/shm`, the
scratch home) and the arm is asserted on it when one exists; when none exists the mapping
is pinned symbolically against `firstOpenLedgerSyncMode(memoryBacked bool)` (the decision,
split out of `firstOpenSyncMode` in `internal/tools/memory.go`). Measured here:
`the memory arm is measured on /dev/shm (tmpfs (memory filesystem))` — asserted, not
skipped. `firstOpenMemoryArmSkipReason` / `firstOpenMemoryArmNotMemoryFmt` /
`firstOpenMemoryArmUnmeasurableFmt` and the `skip` field/`t.Skipf` path this report added
are therefore REMOVED, not carried. Full account: `devdoc/scratch-home-block-backed-red.md`.
