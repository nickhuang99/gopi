<!-- doc-class: normative -->
# The /tmp/other-repo merger message — a leaked test row, and the class fix

**Task:** `01a09d12` — "why /tmp/other-repo is displayed"
**Branch:** `fix/reclaim-fossil-converge` (worktree `~/workspace/gopi-tmp-repo-msg`)
**Date:** 2026-09-15

## The symptom

`gopi merger` printed, on every pass of its 5-minute loop, forever:

```
[task-notify] dead-holder reclaim  (t-stale-sibling) skipped: repo directory /tmp/other-repo is gone — this row cannot be reconciled here
```

The operator's question was the right one: **no gopi component ever created
`/tmp/other-repo`**, Rule 22 forbids test/dev workspaces on `/`, and there are
whole epics about the `/tmp` exhaustion. So where did the path come from?

## Root cause (proven, not inferred)

`/tmp/other-repo` is a **decoy string inside a test fixture row that leaked into
the operator's live ledger**.

1. The row `t-stale-sibling` is a **test fixture**, not a task. It is seeded by
   `workerRecycleStaleSiblingRow` in
   `cmd/agent/cli_tasks_worker_recycle_e2e_test.go`, whose deliverable blob is
   hard-coded as:

   ```go
   deliverable := `{"repo":"/tmp/other-repo","worktree":"` + wt + `"}`
   ```

   The repo path is **deliberately never created**: the test's subject is that a
   *stale sibling row* must not block a delivered row's recycle, so the row's
   repo is a decoy whose absence is the whole point.

2. The fixture reached the **live** ledger `~/.gopi/memory.db`. It was
   introduced by commit **`eb9b070a`** (2026-09-12 06:16) and made hermetic
   **22 minutes later** by **`c81d7fa6`** (06:38), which added
   `t.Setenv(memoryDBEnv, dbPath)` — the suite's own commit message names the
   incident: *"the 2026-09-12 suite hit exactly that [UNIQUE-constraint
   failure] because it wrote a row into the operator's real ledger"*.

   The leaked row's stamp proves which code wrote it: `claimed_by = peerbox:7`,
   and pid `7` is the **pre-fix fixture's hardcoded constant**
   (`workerRecycleStaleSiblingPid = 7`); post-fix code searches `/proc` for an
   absent pid in `[3000000, 3999999)`. The worktree path in the row is under
   `/dev/shm/gopi-test-scratch/suite-cmd-agent-run-…`, the suite's scratch root.

3. Why it **still** prints: the ledger is **append-only** — `task delete` is
   explicitly rejected by the CLI (`cmd/agent/cli_tasks_test.go` pins
   `{"delete", …} → "unknown subcommand"`), and no verb removes a row. So the
   refusal cannot converge: every pass re-discovered the same irreconcilable row
   and re-printed the same line.

**So: nobody created `/tmp/other-repo`.** The path is a decoy in a wrecked test
fixture, and the merger's line was the *honest* downstream symptom of a test
that wrote into the operator's ledger.

## The class-level gap (why it could happen twice)

Three isolation layers already existed for suites, and each was added after its
own incident:

| layer | pinned by | closes |
|---|---|---|
| scratch root (`TMPDIR`, off `/`) | `PinSuiteTempRoot` | boot-device fsync; the 2026-09-11 exhaustion |
| notify env + `GOPI_SETTINGS` | `PinSuiteNotifyEnv` | ~200 test task-ids in the live sink |
| gopi profile (`GOPI_HOME`) | `PinSuiteGopiProfile` (internal/tools only) | 54k-entry session-tree scans |

**The ledger was never pinned.** Measured 2026-09-15 on master: without an
explicit `GOPI_MEMORY_DB`, an in-process `cmd/agent` test resolves the *ambient*
ledger (`/home/user/.gopi/memory.db`, 1249 rows). `GOPI_HOME` is pinned by some
suites and not by `cmd/agent`, so the default `(<gopi dir>/memory.db)` was the
operator's real file — and the ledger is the one artifact where a stray write is
**permanent**.

## The fix (two legs)

### Leg 1 — the class: a suite ledger pin (`internal/testutil/ledgerpin.go`)

`PinSuiteLedgerEnv(suite)` points `GOPI_MEMORY_DB` at a **blank** per-run file
inside the pinned scratch root and **masks** the legacy alias `GOPI_HIPPO_DB`
(which is the same override under an older name and would smuggle the real
ledger back in through `memoryDBPath`'s fallback chain). It is installed by the
**shared entry point** `PinSuiteForTestMain`, so every suite that pins scratch
isolation gets ledger isolation too — with one line of new code per suite (a
guard test), not one per test.

Blank, not a copy: a test that wants rows seeds them into its own sandbox (every
in-process fixture already does); copying the live ledger would re-import the
read cost the profile pin removed and would put the operator's task history into
every test process.

Guard: `testutil.AssertSuiteLedgerIsPinned` — asserted by
`internal/tools` and `cmd/agent` (the CLI-driving suite that leaked). Red proof
in `internal/testutil/ledgerpin_test.go`: a re-exec child running the **pre-fix**
shape (scratch rooted, ledger ambient) must fail the anchor naming the ambient
file and the fix; the fixed shape must pass (positive control, so the guard
cannot be satisfied by a test that never runs).

### The pin's second door — fixtures that UNSET it (found by running the guard)

The guard is what exposed the rest of the hole, and it is worth stating because
it is order-dependent and therefore invisible in a single-test run:

```
go test ./internal/tools/ -run 'TestProceduralSearchEmpty|TestMigrateDefaultTriggers|TestSuiteLedgerIsSandboxed'
→ --- FAIL: TestSuiteLedgerIsSandboxed: GOPI_MEMORY_DB is unset …
```

Two `internal/tools` fixtures took the pin back down for the **whole rest of the
binary**:

| site | shape | why it leaks |
|---|---|---|
| `memory_procedural_test.go:288` | `_ = os.Unsetenv("GOPI_MEMORY_DB")` at the END of the test | erases the suite-wide pin; every later test resolves the operator's real ledger |
| `memory_polish_test.go:184` | `os.Setenv(pin); defer os.Unsetenv(pin)` | the `defer` restores **""**, not the pin — same effect |

The irony is that an `Unsetenv` reads like cleanup. It is the exact opposite: the
pin is a **suite-wide safety invariant**, and a test that wants a different
ledger must *narrow* it (`t.Setenv`, whose undo restores the pin), never remove
it. Both sites now use `t.Setenv` — legitimate under Rule 17 because these are
plain restore sites, not per-call closure flips (which need the
`os.Setenv` + `defer testutil.SnapshotEnv(t, key)()` pair so the flip undoes when
the call returns). The three baseline permits that existed only to allow the old
shape were removed, so the allow-list ratchets down.

### The pin's second iteration — protection beats a choice, but never overrules one

The first pin was **unconditional**, and running it in context turned a green
test red — which is the guard earning its keep:

```
go test ./internal/tools/ -run 'TestSyncImportTwoProcessConcurrent'
→ FAIL (the children imported into an empty sandbox while the parent counted rows in its own)
```

`syncTestDB` names a ledger of its own and **hands it to a re-exec child**: the
child is the same test binary with the parent's DB in its environment, and the
point of the test is that two PROCESSES import into THE SAME file. A pin that
overwrites that choice breaks a legitimate contract. The rule the pin now
encodes:

> A suite pin stops the **fallback** to the operator's file. It never overrules
> a ledger a test **deliberately named** — except when that choice *is* the
> operator's live file, where protection beats choice.

```go
func honoursInheritedLedger(c ledgerChoice) bool {
    return c.named() && !ledgerIsAmbient(c.path())
}
```

Two details that are easy to get wrong, both measured:

* the inherited choice is captured ONCE, **before** this process pins anything
  (`inheritedLedgerOverride`, a `sync.Once`) — otherwise a second pin call
  mistakes its own sandbox for a parent's choice;
* the ambient-candidate set has **two** entries: the resolved `GOPI_HOME`
  default *and* the `$HOME`-derived `~/.gopi/memory.db`. The harness pin
  rewrites `GOPI_HOME` but never `HOME`, so a suite whose profile is sandboxed
  would otherwise call the operator's real file "not a candidate" and bless an
  `GOPI_MEMORY_DB` that points straight at it.

`AssertSuiteLedgerIsPinned` follows the same reasoning: its rule is **"never the
operator's REAL ledger"**, not "must be inside this suite's scratch root" — the
old wording failed a test whose sandbox was legitimately elsewhere. The legacy
alias is checked FIRST (an ambient `GOPI_HIPPO_DB` smuggles the real ledger back
in through the fallback chain, so no `GOPI_MEMORY_DB` value can make that shape
safe).

### Leg 2 — the existing fossil: convergence (`internal/tools/task_reclaim.go`)

The reclaim's own doc already promised it: *"a row whose ground is unreadable is
reported ONCE, not as a fresh failure burst every pass"* — but only the
empty-reason case honoured that. Every non-empty refusal was re-printed on every
pass.

`reclaimRefuse(row, why)` memoizes reported refusals per process, **keyed by
row+reason**:

* first occurrence → **reported** (Rule 15: the refusal is disclosed, never
  swallowed);
* same row, same reason → **silent** (the convergence the doc promises);
* same row, **new** reason → reported again (a fresh diagnosis must not hide
  behind the verdict it replaced);
* a second fossil row → reported in its own right (no global "warned" flag).

This is the same bargain `path_policy.go`'s `settingsWarnOnce` strikes: one loud
line names a condition nobody can act on again, instead of it becoming the
steady state of the sweep.

### Rule 14 cleanup (rides along)

`GOPI_MEMORY_DB` / `GOPI_HIPPO_DB` / `memory.db` were mirrored: an unexported
`memoryDBEnv` in `internal/tools` plus re-stated copies in
`internal/agent/loop_test.go` and `cmd/agent/main_test.go`. They now have ONE
definition in `internal/config/memory_env.go` (`MemoryDBEnv`,
`MemoryDBEnvLegacy`, `MemoryDBFileName`), which is what lets the suite pin name
the file at all. `internal/tools` keeps its unexported names as **aliases**.

## Verification

* `go build ./...` — OK
* `go test ./internal/tools/ -run 'TestDeadHolderReclaimReports|TestReclaimRefusal'` — PASS
* **Red proof (leg 2):** reverting `reclaimRefuse` to the pre-fix unconditional
  `taskNotifyLog` makes `TestDeadHolderReclaimReportsAnIrreconcilableRowOnce`
  FAIL on the second pass — the test measures the real defect, not a re-creation.
* `go test ./internal/testutil/ -run 'TestSuiteLedgerPin|TestAmbientLedgerPath'` — PASS (red/green pair)
* `go test ./internal/tools/ ./cmd/agent/` — see the task report's Verification section for the final run.

## Not done / honest limits

* The `t-stale-sibling` row **stays in the live ledger**. It cannot be deleted —
  the ledger is append-only by design, and adding a delete verb to clean up one
  bad row would be the wrong trade. With leg 2 the merger reports it **once per
  process start** and then stays quiet.
* The legacy alias masking is a *suite* concern; a production invocation may
  still set `GOPI_HIPPO_DB` (that is the documented fallback).

## Deliverable

Branch `fix/reclaim-fossil-converge` in the shared repo (worktree
`/home/user/workspace/gopi-tmp-repo-msg`). Recover any file with
`git show fix/reclaim-fossil-converge:<path>`.
