<!-- doc-class: normative -->
# The run-close-out hole assertion measured the HOST, not the run

**Task** `01a114f7-fe0d-7a30-bfb6-5253bed80daf` · 2026-10-07 · branch `task/01a114f7fe0d7a30bfb65253bed80daf`
**File** `internal/tools/run_closeout_reap_window_test.go`
**Class** test-harness hermeticity (a test asserting on a shared, global fact)

## The defect, in one sentence

`TestRunCloseoutSelectionReportsAHoleAndNotAMiss` asserted `len(unmeasured) > 1`,
but `unmeasured` is not "the pids this test planted" — it is a slice of the
**host's** process table, so the arm's verdict was a function of what every other
process on the machine happened to be doing while it ran.

## Why the count could never have been about this test

`runCloseoutSelection` walks `/proc` through the production reader and, for every
pid it cannot place, asks `runCloseoutPidShallowUnavailable` whether that pid is a
*hole in the measurement*. Four halves, all of them about the machine:

| half | fact | who else satisfies it |
|---|---|---|
| `no block` | `/proc/<pid>/environ` reads EMPTY **or unreadable** | every same-uid process between `fork` and `execve`; a zombie |
| `in our uid` | `/proc/<pid>` is owned by the test's uid | every process the operator runs |
| `newer` | starttime (stat field 22) > this process's | anything started after `go test` |
| `unplaced` | no rung placed it | any process carrying no record of this run |

So `unmeasured` legitimately contains foreign pids; asserting its SIZE was
asserting on the state of the host.

## Reproduction (measured, not argued)

Two carriers of the same class, in the task's session artifacts
(`~/.gopi/sessions/01a114f7-fe0d-7a30-bfb6-5253bed80daf/artifacts/`):

- **`holeprobe/`** — a standalone measurement of the predicate against the live
  process table. Quiet host: `holes_seen=0` over 300 sweeps. While a rotating
  flock of same-uid fork/exec/exit processes runs: **7038 hole observations over
  21 distinct foreign pids** (`python3`, `vet`, `bash`), including the pid of a
  concurrent `go vet`/`go build` — i.e. exactly the "another test-budget run"
  shape the epic names.
- **`churn-zombies.sh`** — the deterministic carrier. Each helper forks a child
  that exits at once and the parent *never waits*: the pid stays in the table as
  a zombie, whose `/proc/<pid>/environ` reads **EACCES** (measured: a zombie
  `python3`'s block is readable→4036 bytes while running, then
  `ERROR Permission denied`). Empty/unreadable + same uid + newer incarnation is
  the whole of the `no block` half, so a zombie flock is a *guaranteed* source of
  foreign holes. **No load is generated (Rule 27): these processes do nothing.**

Results, binary built from the pre-fix tree (`tools.test.pre-fix`, built from
`git show master:internal/tools/run_closeout_reap_window_test.go`), against the
identical flock as its post-fix twin:

```
$ bash artifacts/churn-zombies.sh artifacts/tools.test.pre-fix 25 artifacts/pre-fix-zombie.txt
exit status: 1
FAIL lines: 24
PASS lines: 1

run_closeout_reap_window_test.go:172: more holes than the one injected read:
  [1929303 1929310 1929308 1929422 1929304 1929307 1929309 1929302 1929306]
  — the discriminator is loose

$ bash artifacts/churn-zombies.sh artifacts/tools.test 25 artifacts/post-fix-zombie.txt
exit status: 0
FAIL lines: 0
PASS lines: 25
```

(A first run of the same A/B, before the pre-fix binary was rebuilt from master's
test, measured 22/25 FAIL — the rate is high but not 100%, which is why the
reproduction is quoted as "N of M" and never as a single red.)

Every foreign pid in that list is a process the arm never planted; the injected
`ownedPid` is not even in it.

## The fix — the assertion speaks only about this test's own pids

The rule the arm now states: **among the pids this arm owns, exactly one is a
hole, and it is the injected pid.** The old global count and the two
per-pid assertions (the injected pid IS a hole; the readable control is NOT) are
unchanged — the injected hole is still reported and the measured control is still
not reported as a hole (the epic's explicit constraint).

The pid set is built by a new helper, `runReapTestOurPIDs`, from the same census
the selection reads (Rules 13/14 — no second walker): the fixture's three
announced members plus every descendant of them. Everything outside that set is
another process on the host, and the arm neither asserts on it nor fails because
of it.

## The regression guard, and why it is deterministic

Racing the kernel cannot be aimed at — the live fork/exec window measured
**0 holes / 200 sweeps** on this host even under a 6-wide `env -i sleep` flock,
while the zombie carrier hits on essentially every sweep. So the guard
`TestRunCloseoutSelectionDoesNotCountAnotherProcessAsItsOwnHole` reproduces the
**shape** instead of the odds:

1. it lies about a real pid the test spawned **outside the fixture tree** — `/bin/true`
   started by the test itself, which is exactly what a sibling run's child is to
   this arm — in addition to the injected fixture pid;
2. it evaluates **the old assertion on purpose** (`len(unmeasured) > 1`) and
   **fatals when it is FALSE** — so this guard is RED the moment the old global
   count is restored: it pins the defect, not the fix;
3. it asserts the **scoped** rule (no pid of its own tree other than the injected
   one is a hole) — green on that same measurement.

Measured log line, `-count=2`:

```
stray pid 1925427 is a hole for the host's census (2 holes) and NOT for this arm's scope
  (map[1925424:true 1925425:true 1925426:true])
```

## What was deliberately NOT changed

The production rule (`runCloseoutPidShallowUnavailable`, `runCloseoutSelection`)
is untouched: narrowing *it* would either lose the leftover the epic exists to
reap or put an unreadable pid into the kill path, and no planted hole in a test
can reproduce the real race to prove otherwise. The defect is a test asserting a
global fact; the edit is where the defect is. (An honest partial: hardening the
rule against zombie pids — a zombie is gone, not unmeasured — is a follow-up of
its own, and the guard's stray does NOT depend on it.)

## Verification (Rule 5 / Rule 31)

- Build: `go build ./...` → OK; `go vet ./internal/tools/` → clean.
- Tests: the `RunCloseout` tests in `internal/tools` passed here (`ok … 4.334s`);
  `make test-budget PKGS='./internal/tools'` → see the task report.
  <!-- Retired mechanism (2026-11-05): those tests were DELETED with the
       close-out reap itself, so the selector above no longer names anything.
       This line records what was measured on 2026-10-31; it is not a command
       a reader should re-run — today it would exit 0 with `[no tests to run]`. -->
- Ran (the surface changed is a test, so the invocation IS the test binary):
  - `bash artifacts/churn-zombies.sh artifacts/tools.test.pre-fix 25 artifacts/pre-fix-zombie.txt`
    → **exit 1, 22 FAIL / 3 PASS** — the reproduction.
  - `bash artifacts/churn-zombies.sh artifacts/tools.test 25 artifacts/post-fix-zombie.txt`
    → **exit 0, 0 FAIL / 25 PASS** — same flock, fixed arm.
  - `bash artifacts/churn-zombies.sh artifacts/tools.test 20 … '^TestRunCloseoutSelectionDoesNotCountAnotherProcessAsItsOwnHole$'`
    → **20/20 PASS**; `… 6 … '^TestRunCloseout'` → 0 FAIL under the same churn.
- Result: the failure mode is reproducible on the pre-fix binary and absent on the
  fixed one; the guard passes and is red by construction if the global count
  returns.
