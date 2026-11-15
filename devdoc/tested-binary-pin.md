<!-- doc-class: normative -->
# Which tree answered? — the tested binary, and the fake-dead pid

**Task**: 01a1023ce3817096b3850cec3b072f16 (2026-10-03)
**Symptom filed**: `cmd/agent`'s `TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow`
is flaky — RED at `cli_tasks_worker_recycle_e2e_test.go:540` with
`the production backfill seam must stamp the row's ground: worktree="" branch=""`,
while stderr shows
`task t-worker-stale: creation-time ground stamp REFUSED — …/001/wt is already named by row t-stale-sibling`.
The filing's premise was "the same code, green three minutes earlier".

That premise is false, and the way it is false is the whole finding: **the two
runs did not run the same code, and did not even run the same test.** Two
independent defects were measured; neither is a race in the production seam.

## Measurement 1 — the run that "went green" SKIPPED, and skipped for another tree

The stream of the run the filing called green
(`~/workspace/gopi-test-scratch/queue/test-budget/run-1791038115-1452873.json`,
started 22:35) records the case's own output verbatim:

```
=== RUN   TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow
cli_tasks_worker_recycle_e2e_test.go:528: gopi binary not found at
    /home/user/workspace/gopi-01a1021483607bbd9816c29534024c5c/bin/gopi — run scripts/e2e_smoke.sh first
--- SKIP: TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow (0.05s)
```

Two things in that line. First, the "green" was a **SKIP** — a passing package
with the case never run, which is exactly the shape a flake report mis-reads.
Second, the path it could not find is **another worktree's** (`gopi-01a10214…`,
not the run's tree): `internal/testutil.GopiBin` resolved the module root by
walking up from the *test's own CWD*, and in this shared checkout that walk
reaches whichever worktree's `bin/gopi` happens to exist. `.gopi-tags` makes it
literal, so a session working in one worktree can run a case that reads or
reaches another worktree's tree.

The failing run three minutes later (`run-1791038621-1501928.json`, 22:43) had
`GOPI_BIN=/home/user/workspace/gopi-01a1021483607bbd9816c29534024c5c/bin/gopi`
— printed by that run's own `TestSweepE2EBinaryIsBuiltInTheSuiteScratchRoot`,
which exists to state this. So the case ran against **another worktree's build**.

## Measurement 2 — that other tree was genuinely broken under that test

`git show d8ce92ac:internal/tools/task_deliverable_worktree.go | grep -c taskWorktreeDeadOwnerGroundLineFmt`
→ **0**: that worktree lacked commit `1d901244` ("a DEAD ground owner is not a
borrow — reconcile the creation seam with the delivery recycle"), whose message
is `the binding is history, not an owner, so the ground IS recorded`.
`git show d8ce92ac:cmd/agent/cli_tasks_worker_recycle_e2e_test.go | grep -n "production backfill seam must stamp"`
→ **present** (line 149). `git merge-base --is-ancestor 1d901244 d8ce92ac` → **not an ancestor**.

**The tree had the TEST that demands the stamp and not the PRODUCTION that
performs it, so it failed its own test — truthfully.** Its failure was reported
into another task's ledger and read as a flake of the tree under test. The
independent check agrees: `strings -a` of the surviving artifact that run drove
(`/dev/shm/suite-cmd-agent-run-989045662/gopi-suite-bin-771865650/gopi`,
mtime 22:44:02) does **not** contain the `1d901244` message.

## Measurement 3 — the fixture's "dead" pid was in this host's LIVE range

The fixture that models a dead holder picked its pid by scanning
`3,000,000..3,999,999` for one absent from `/proc`:

```
const workerRecycleStaleSiblingPidFloor   = 3000000   // old
const workerRecycleStaleSiblingPidCeiling = 3999999
```

On this host that window **overlaps the live pid range**, so the pid it chose
was not "gone" but merely "not used at this instant":

```
$ cat /proc/sys/kernel/pid_max          → 4194304
$ cat /proc/sys/kernel/ns_last_pid      → 1648062
$ ls /proc | awk '$1>=3000000'          → 3043482 3044206 3044208     (23:15)
                                          … later: 3977341
```

The fixture probes the pid's absence **once**; the production seam reads
liveness **later** (after the child and its own subprocesses have been spawned).
A child landing on the very pid the fixture had just declared dead makes the
seam see a LIVE holder and refuse the stamp — the observed failure. This defect
would fire with no worktree mismatch at all.

## The fixes

**a. The tested binary is built by the run (hermeticity).**
`scripts/test-budget.sh` no longer exports `GOPI_BIN="$root/bin/gopi"`. It builds
the tested binary **once per run**, from the tree it is executing in, into the
run's own artifact directory under the scratch queue, and exports it as
`GOPI_TEST_BIN` (`<queue>/test-budget/run-<epoch>-<pid>-tested-bin/gopi`).
`internal/testutil.GopiBin` now resolves, and nothing else:

1. `GOPI_BIN` — a single case's own build (`sweepZombiesRequireBin`,
   `reportSeamBinary` stamp the binary they just built). Unusable stays
   **FATAL** (Rule 15: a silent substitution is the defect).
2. `GOPI_TEST_BIN` — the suite pin, and the resolution every e2e case must get.
   `pinSuiteTestedBin` builds from the tree under test when the runner supplied
   none, publishes the result into the environment (so a **child** the suite
   spawns drives the same revision) and does it at most once per test binary
   (`sync.Once`) — one build, every case.
3. `<module root>/bin/gopi` — last resort, correct **only** for the one-click
   entry (`scripts/e2e_smoke.sh`), whose whole job is to build exactly that file
   from this tree.

The build directory is a `MkdirTemp` under the suite's pinned scratch root
(Rule 22), never a predictable name in a shared writable directory, and it is
already inside the scratch GC's scope (Rule 34/35: no new lifecycle).

**b. The fake-dead pid is derived from the kernel (stability).**
`workerRecycleAbsentPid` reads `/proc/sys/kernel/pid_max` and returns a pid
**above** it, where no process can be allocated, so its absence verdict is
stable rather than merely current. It fails the case if the knob is unreadable
(Rule 15). The reachable production branch is unchanged: the seam's predicate is
a `/proc` existence check either way.

## The RED→GREEN of the stale-claim mechanism (measured, one mutant)

The production seam cannot be starved by waiting, and no stress may be built to
force it (Rule 27). The measureable form of "the fixture declares a pid dead that
is not dead" is therefore a **mutation of the fixture's own premise**, run twice
on the fixed tree:

```
# RED — the mutant hands the seam a pid that IS live (os.Getpid()):
$ GOPI_REDPROOF_LIVE_PID=1 GOPI_TEST_BIN= \
    go test ./cmd/agent/ -run TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow -count=1
[task-notify] task t-worker-stale: creation-time ground stamp REFUSED — …/001/wt is
  already named by row t-stale-sibling (a directory or branch another row already
  names is that row's; a borrowed ground is never recorded)
--- FAIL: TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow (1.24s)
    cli_tasks_worker_recycle_e2e_test.go:593: the production backfill seam must
  stamp the row's ground: worktree="" branch="" (want "…/001/wt"/"feat/worker-stale-sibling")
FAIL	github.com/the operator/gopi/cmd/agent	1.441s      WALL 16.97 s

# GREEN — mutant reverted (git status --porcelain prints nothing):
$ GOPI_TEST_BIN= go test ./cmd/agent/ -run TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow -count=1
ok  	github.com/the operator/gopi/cmd/agent	3.528s          WALL 5.82 s
```

That is the filed symptom verbatim — `creation-time ground stamp REFUSED … already
named by row t-stale-sibling`, then `worktree="" branch=""` — reproduced from a
LIVE holder, which is exactly what an allocatable pid inside the fixture's old
window could become between the fixture's probe and the seam's liveness read. It
also shows both fixes are needed on this tree: the mutant runs the case WITHOUT
any binary mismatch (`GOPI_TEST_BIN=` empty, the pin names this run's own build)
and still fails, so the pid defect is a second, independent cause of the same
message; the binary defect is what made a *different* tree's refusal look like
this tree's flake.

What is still **not** claimed: a capture of an *accidental* pid collision. The
collision is a window (probe → spawn → read), and a window is not measurable by
waiting without manufacturing load (Rule 27); it is proven by the shape above
plus the host measurement that the old window was allocatable.

## What is NOT claimed

- **The stale-binary replay did not reproduce RED.** Driving the case with
  `GOPI_TEST_BIN=<the old artifact>` **passed**: `d8ce92ac` contains all three
  earlier construction-seam commits (`b16e7318`, `8d3432f5`, `6a4f556c` — 4
  matches each), so the tree was self-consistent *except* for the one test whose
  production half it lacked. The cross-worktree resolution is therefore proven
  **hermeticity** damage (a run answering for a tree it does not own, and a
  hidden SKIP), and Measurement 2 is what proves the filed failure came from
  there. The pid window is proven as a live defect **by the measured state of
  this host** (`pid_max`, `ns_last_pid`, live pids inside the old window), not
  by a captured red.
- Neither fix is a claimed RED→GREEN of the filed symptom. No failing run of
  *this* tree was observed; the honest statement is: **the flake report was
  traced to two defects and both are fixed, with the one reproduction attempt
  that could have falsified the binary story reported above as a non-result.**

## Evidence

- Streams (verbatim run records, each with its own `GOPI_BIN`):
  `~/workspace/gopi-test-scratch/queue/test-budget/run-1791038*.json`.
- `git show d8ce92ac:<path> | grep -c taskWorktreeDeadOwnerGroundLineFmt` → 0;
  `git merge-base --is-ancestor 1d901244 d8ce92ac` → false.
- `strings -a <the run's suite binary> | grep -F "<the 1d901244 message>"` → no match.
- `/proc/sys/kernel/pid_max`, `/proc/sys/kernel/ns_last_pid`, live `/proc` pids: quoted above.
- **a. The pid window (this section)**: red proof above — mutant → the seam's
  refusal verbatim, reverted → ok, same invocation.
- **b. The binary pin**: `go test ./internal/testutil/ -run TestGopiBin` —
  precedence, child publication, fatal-on-unusable-pin (subprocess fixture).
- `go test ./cmd/agent/ -run TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow`
  prints `testutil: tested binary pinned to <scratch>/suite-tested-bin-*/gopi`.
- The loaded two-package run (`make test-budget PKGS='./internal/tools ./cmd/agent'`,
  stream `run-1791040417-1610786.json`): 2 packages finished, 0 with a failure,
  `TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow` PASS (2.35s), and the
  cmd/agent stream carries 0 `"Action":"fail"` records.

## Guidance for the next agent

- **Read the run's own stream before believing a flake report.** It carries
  `GOPI_BIN`, the package scope and each case's verbatim output — including a
  SKIP that a "green" run silently swallowed.
- **A passing package is not a passing case.** `--- SKIP:` is not evidence about
  the tree; a case that cannot resolve its binary must not decide a verdict.
- **Never reintroduce a resolution that reaches a tree's `bin/gopi` by
  accident.** Take `GopiBin(t)` and let the pin decide; if you build a binary
  yourself, stamp `GOPI_BIN` and clean the directory in `t.Cleanup`.
- **A pid chosen for "absence" must be outside the allocatable range.** An
  absence observed once is not an absence at verdict time: derive it from
  `/proc/sys/kernel/pid_max`, never from a hand-picked window.
