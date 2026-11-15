<!-- doc-class: normative -->
# Task Report: flaky `TestT2_E3_ResumeCarriesForwardCommittedConclusions` — root cause + fix

**Branch:** `fix/write-task-area-schema-timeout` (worktree `~/workspace/gopi-wta-fix`)
**Symptom:** `taskarea_takeover_test.go:213: commit original: write_task_area: schema: context deadline exceeded`
**Epic:** flake reproduces on pristine master (`FAIL`, `FAIL`, `ok`), filed out of the
hippocampus host-binding task (memory #1731).

## Root cause (5-why, with measurements)

1. **Why does the test fail?** The first `commitViaAgent(...)` → `tools.WriteTaskArea`
   returns `write_task_area: schema: context deadline exceeded`, which the test turns
   into a fatal at line 213.
2. **Why does `WriteTaskArea` time out?** `internal/tools/writetaskarea.go` bounded the
   **whole** DB path with ONE deadline, `taskAreaDBTimeout = 5s` — and that path includes
   a **cold-DB bootstrap**: `openTasksDB` → `ensureKnowledgeBase` → `initKnowledgeBase`
   (create the file + ~40 DDL statements + FTS/triggers + a WAL-close checkpoint fsync).
   `tempAgentMemoryDB(t)` points `GOPI_MEMORY_DB` at a **fresh `t.TempDir()` file per run**,
   so **every run** pays that cold bootstrap inside the 5s budget.
3. **Why does the bootstrap exceed 5s?** `t.TempDir()` honours `TMPDIR`, whose system
   default on this host is `/tmp` = **the boot device** (`/dev/sda3`, 147G, **96% full**;
   memory #1308 measured 49–225ms fsync latency there, with multi-second stalls under
   concurrent-agent I/O). SQLite fsyncs the DB at the WAL close-time checkpoint, so a cold
   DB costs **10–17ms idle** but **5–7s+ under I/O load** (measured below).
4. **Why is the deadline a single shared 5s?** The constant's doc said "bounds each
   working-area DB round-trip" — it was sized for the *session-scoped* query work, yet it
   also covered the bootstrap, whose cost class is completely different (cold file
   creation + DDL + fsync). The read path survived the same deadline only because it does
   **not** create/schema a DB: `SessionConclusions` swallows open failures and returns
   "no conclusions" (`internal/tools/writetaskarea.go`), which is exactly why the read path
   passes and the write path fails (the briefing's suspect list had this right).
5. **Why was the error text misleading?** Both the bootstrap and the `ensureSessionConclusionsSchema`
   call used the same ctx, and every open failure was wrapped identically — so an expired
   *budget* surfaced as `"schema: context deadline exceeded"` (looks like a schema bug) or as a
   bare `"context deadline exceeded"` (looks like nothing at all). Rule 15: the failure
   misattributed itself.

**Mechanism, proven deterministically:** holding a write lock from a second connection and
calling `WriteTaskArea` reproduces the exact text
(`write_task_area: schema: context deadline exceeded`) — but the call takes **15.0s, not 5s**,
because SQLite's `busy_timeout` is `dbBusyTimeoutMSec = 15000` while the ctx is 5s. So the
`context deadline exceeded` delivered by the driver is the **busy/step timeout's** error,
not literally the 5s (the budget then bounds how long the caller *waits* for it).

### Measured evidence (same host, same commands)

| Condition | Before fix | After fix |
|---|---|---|
| 10 × consecutive `go test -count=1 -run 'TestT2_E3_ResumeCarriesForwardCommittedConclusions' ./internal/agent/` (idle) | 10 ok (flake not visible; 1.6–5.1s each) | 12/12 PASS on the committed branch tip (`629ffa0`), 0.023–0.053s each |
| 12 consecutive single-test runs, **under synthetic I/O load** (6× `dd … conv=fsync` loops) | **12/12 FAIL** (5.0–7.2s) | **12/12 PASS** (0.025–0.089s) |
| 10 consecutive runs under the same synthetic load, on the committed tip | — | 10/10 PASS (0.027–0.081s) |
| 8 consecutive runs, **unpinned** scratch root (`GOPI_TEST_TMPDIR=/tmp`) **+ load**, on the committed tip | 12/12 FAIL (pre-fix) | **8/8 PASS** — the wider bootstrap budget alone absorbs the boot-device cold-DB cost at this load; the pin is what removes the cost class (idle run time 3.5–5.1s → 0.03s) |
| Cold `openTasksDB`+schema on a fresh DB, idle, on `/dev/shm` | 10–17ms | 10–17ms |
| Single-test wall time (idle, boot-device scratch → tmpfs scratch) | 3.5–5.1s | 0.025–0.04s |

Two distinct failure texts under load prove both halves of the path are affected:
`write_task_area: schema: context deadline exceeded` (schema step) and
`write_task_area: context deadline exceeded` (the **open** step, no `schema:` prefix).

## Completed

1. **Root cause** stated above and pinned by measurement (not a raised timeout guess).
2. **Production fix — budget coherence** (`internal/tools/writetaskarea.go`):
   - `taskAreaWriteTimeout = 30s` — a **new named constant** for the **bootstrap** step
     (cold file creation + schema init + first open), documented as *not* interchangeable
     with the query budget.
   - `taskAreaDBTimeout` **stays 5s** — now genuinely bounded to the session-scoped
     insert/read (`insertTaskAreaConclusion`, `sessionConclusionCount`), where a stall
     really would mean a hung DB.
   - `GOPI_TASK_AREA_BOOTSTRAP_TIMEOUT_SEC` — Rule 2 single-read env knob
     (`taskAreaWriteBootstrapTimeout`), non-positive/invalid values ignored.
   - `classifyBootstrapFailure` — an expired budget now reports **"bootstrap budget 30s
     expired … raise GOPI_TASK_AREA_BOOTSTRAP_TIMEOUT_SEC"**, while a real open refusal
     (foreign/read-only/corrupt DB) passes through **unmodified** (Rule 15: no
     misattribution; the old code could not tell the two apart).
   - `LastInsertId` error no longer discarded (Rule 15b): unknown record number → `-1`,
     never a fabricated id and never a failure of an already-successful write.
3. **Test-side fix — the actual flake trigger** (`internal/testutil/scratchroot.go`, new):
   the suite scratch root is pinned to a memory filesystem, so a fresh scratch DB's
   WAL-close checkpoint never fsyncs to the boot device (memory #1308). The pin,
   its `GOPI_TEST_TMPDIR` override and its statfs classification used to live **only in
   `internal/tools`** (`testsuite_temproot_test.go`) — which is precisely why
   `internal/agent` kept flaking on the identical mechanism (Rule 14: one definition).
   `internal/testutil` is now the single shared home; `internal/tools` **delegates** to it
   (its guard tests are untouched and still green) and `internal/agent`'s `TestMain` calls
   `testutil.PinSuiteTempRoot()`.
4. **Regression guards** (the epic's "the flake cannot silently return"):
   - `TestWriteTaskAreaBootstrapBudgetExceedsQueryBudget` — fails if the two budgets are
     ever collapsed back into one (that collapse *is* the flake).
   - `TestWriteTaskAreaBootstrapBudgetEnvOverride` — the knob is read once; non-positive
     ignored.
   - `TestWriteTaskAreaBootstrapExpiryIsReportedAsABudget` — error classification
     (budget-expiry vs real refusal), no timing, no parked SQLite connection.
   - `TestWriteTaskAreaBootstrapBudgetIsEnvWidened` — the knob reaches the **real call
     path**, not just the resolver.
   - `TestSuiteTempRootIsPinned` (`internal/agent`) — dropping the pin from `TestMain`
     makes it red (mutation guard), plus a `PROBE_PASS=yes` tripwire so a temporary
     probe-pass can never ship.
   - Existing `TestSuiteScratchRootIsPinned` / `TestFreshDBOpenCloseIsFast` /
     `TestSuiteScratchRootIsMemoryBacked` (`internal/tools`) still pass against the shared
     implementation.

## Reproduce either half without editing code

```bash
# 1) DETERMINISTIC, idle host — unpinned scratch root + a small budget is enough
#    to red the test with a truthful message:
GOPI_TEST_TMPDIR=/tmp GOPI_TASK_AREA_BOOTSTRAP_TIMEOUT_SEC=1 \
  go test -count=1 -run 'TestT2_E3_ResumeCarriesForwardCommittedConclusions' ./internal/agent/
# → FAIL 4.06s: "write_task_area: bootstrap budget 1s expired while opening/initializing the
#   memory DB … raise GOPI_TASK_AREA_BOOTSTRAP_TIMEOUT_SEC"
# drop either env var → PASS (that is the two-factor proof: boot-device scratch root
# AND a budget that does not fit the cold-DB cost).

# 2) test-side half alone (boot-device scratch root):
GOPI_TEST_TMPDIR=/tmp go test -count=1 -run 'TestFreshDBOpenCloseIsFast' ./internal/tools/
GOPI_TEST_TMPDIR=/tmp go test -count=1 -run 'TestSuiteTempRootIsPinned'   ./internal/agent/

# 3) the pristine-master symptom (needs host I/O load, which deferred fsyncs):
for j in 1 2 3 4 5 6; do (while :; do dd if=/dev/zero of=/tmp/load-$j.bin bs=1M count=8 conv=fsync; done) & done
go test -count=1 -run 'TestT2_E3_ResumeCarriesForwardCommittedConclusions' ./internal/agent/
```

## Not Done (honest)

- **`cmd/agent` is not pinned.** Its `TestMain` (`cmd/agent/testmain_test.go`) does not call
  `testutil.PinSuiteTempRoot()`; the same cold-DB-on-boot-device mechanism applies to its
  E2E children (memory #1332 records that suite as load-flaky). It is a one-line adoption
  now that the helper is shared, but its full suite is expensive to verify (needs a built
  `bin/gopi`), so it is deliberately **out of this fix's verified surface** rather than
  changed unverified.
- The 5s→30s bootstrap budget is a **judgement call on the cost class**, not a measured
  p99: the fix that is *proven* to remove the flake is the scratch-root pin plus the
  separated/truthful budgets. 30s is deliberately generous because the write path cannot
  skip the file it must write to; the knob exists for operators who disagree.
- `busy_timeout = 15s` (`dbBusyTimeoutMSec`) vs a 5s query budget is a second, smaller
  coherence wart (a contended read reports the busy error, not the budget). Left alone:
  changing it affects the whole memory DB surface and is not needed to fix this flake.

## Verification

| Check | Result |
|---|---|
| `go build ./...` / `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | OK (`BUILD_OK`) |
| 10 × consecutive `go test -count=1 -run 'TestT2_E3_ResumeCarriesForwardCommittedConclusions' ./internal/agent/` (idle) on the committed tip | **10/10 PASS** (accepted run) |
| 10 × same test under synthetic I/O load (6× `dd … conv=fsync`), committed tip | **10/10 PASS** — the pre-fix condition that gave 12/12 FAIL |
| 12 × idle + 10 × under load during development (job logs `/tmp/gopi-job-4286411428.out`) | PASS; 8/8 also pass with `GOPI_TEST_TMPDIR=/tmp` + load (wider bootstrap budget absorbs the boot-device cost) |
| `go test -count=1 ./internal/agent/` (full package) | ok 104.856s |
| `go test -count=1 -timeout 900s ./internal/tools/ ./internal/testutil/ ./internal/agent/` (final, committed tip `f01c6fe`) | **BUILD_OK; ok 220.489s / ok 0.009s / ok 138.016s** |
| New + strengthened regression tests | 5 PASS (`go test -run 'TestWriteTaskAreaBootstrap' ./internal/tools/` ok 14.605s) |

Run log (final accepted run, `/tmp/gopi-job-1762423459.out`):

```
ACC IDLE pass=10/10      (idle, committed tip)
ACC LOAD pass=10/10      (6× dd conv=fsync load, the pre-fix 12/12-FAIL condition)
ACCTESTDONE
```
| Scratch-root guards (`internal/tools` + `internal/agent`) | PASS (root = `/dev/shm/gopi-suite-tmp-*`, tmpfs, ratio 1.31x vs 12.0x limit) |

## Files changed

- `internal/tools/writetaskarea.go` — budget split + truthful classification + env knob
- `internal/tools/writetaskarea_test.go` — 4 regression tests
- `internal/testutil/scratchroot.go` — **new**, the single shared scratch-root pin
- `internal/tools/testmain_test.go` — pin via `testutil`
- `internal/tools/testsuite_temproot_test.go` — delegates to `testutil` (no behaviour change)
- `internal/agent/testmain_isolation_test.go` — pin + `TestSuiteTempRootIsPinned`
