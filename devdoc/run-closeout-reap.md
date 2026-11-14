<!-- doc-class: historical -->
# Task Report: the run reaps its own children at close-out

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task:** `01a0d882-fe75-7d83-8a82-09088403cff1` — a finished `gopi task run` must
not leave its own child processes behind
**Branch:** `task/01a0d882fe757d838a8209088403cff1` · **Repo:** `/home/user/work/GoPi`
**Date:** 2026-09-25

## Completed

### 1. The leak, measured — and who (did not) reap it

The epic's hypothesis — a backgrounded `bash -c 'while pgrep …; do sleep; done'`
outlives its launcher, reparents to init and pins a worktree — is **confirmed on
the live tree**, with the kernel's own record rather than an inference:

```
pid 1136914  ppid=1  pgrp=1136914  sid=1136914
             cgroup=0::/user.slice/user-1000.slice/session-16.scope
             etime=10:43:34     (still cycling: the sleep pid moved 2055410 → 2111062)
             cwd=/home/user/workspace/gopi-01a0d5d286e67513b47274f72d467aaf
             env: GOPI_TASK_RUN_WRAPPER_PID=382599  (that pid is long dead)
                  GOPI_TASK_RUN_ID=01a0d5d2-86e6-7513-b472-74f72d467aaf
                  GOPI_RUN_GROUND=/home/user/workspace/gopi-01a0d5d2…   ← the pinned worktree
```

A second instance of the same class is live beside it: `pid 1503983`
(`python3 …/blackhole.py`, sid 27203, cwd `…/gopi-01a0d687…`) plus its orphaned
sibling `1525157` (`signalrec.test`, same session).

**Nothing claimed to reap it.** The only machinery that ever touched these
processes is the close-out CLASSIFIER (`finishCleanupClassifyOrphanHolder`), which
*classifies* leftovers — it can release a directory they pin (commit `40182d9e`)
but never terminates one, and its own doc block says so: "The remedy for (a)/(b)
belongs to the RUN, not to this classifier: reap the run's own children at
close-out." The previous report on the same area records the manual work that
implied: *"Two leaked trees were removed by hand while measuring."* The run layer
had no reap at all.

### 2. The reap, in the RUN close-out path

**`internal/tools/run_closeout_reap.go`** (new) — the ownership rules and the
killing; **`cmd/agent/cli_tasks.go`** — the one call, in the tail every wrapper
return path already funnels through (`taskRunReleaseOwnWorktrees`), placed
BEFORE the worktree release because a leftover standing in the directory is
exactly what makes that release refuse; **`cmd/agent/run_closeout_reap_scope.go`**
(new) — the wrapper's own facts (its pid, the row id, the process group it gave
its child).

Ownership is **measured, never assumed** — the epic's named criterion first:

| rung | what /proc states | why it is safe |
|---|---|---|
| **R1 lineage** | the process's own `GOPI_TASK_RUN_WRAPPER_PID` (or `GOPI_TASK_RUN_ID`) names THIS run | the kernel preserves the exec-time environment block across reparenting, so this reaches an already-init-reparented leftover; a stranger has no such record |
| **R2 group** | its `pgid` is a group the run registered, **verified by the leader's incarnation ticks** | `Setpgid` on the run child makes it a group leader; the incarnation check stops a recycled number impersonating it |
| **R3 ancestry** | its parent chain reaches a verified group leader or this run's wrapper | reaches a descendant whose group a tool changed with its own `setpgid` |
| **VETO** | it LEADS a POSIX session of its own (`sid == pid`) and R1 does not hold | the daemonised-into-its-own-session case: a service, not a leftover (Rule 24 — the kernel's session record is the detachment evidence) |
| **everything unknown** | unreadable environ / stat / parent / unverified leader | **KEEP** — no rung may turn "cannot tell" into a kill (Rule 15a) |

**"Shares the worktree as cwd" is deliberately NOT a rung.** That is the epic's
trap, and the positive control below exists to keep it out.

One supporting change in the same layer: the run child is now spawned with
`Setpgid: true` (`cmd/agent/cli_tasks.go`), so the run HAS a process group a
close-out can select its own descendants by. Nothing signals that group in
production — the stop path signals the child directly and its descendants
individually (`signalRunChild`, which measures `Getpgid` first) — so the child's
own process-group semantics are unchanged; the exit-report and signal paths are
untouched.

The guards `40182d9e` added are **not weakened**: this change adds no rung to the
classifier, removes none, and its new shared reader
(`finishCleanupProcCensus`) is the same /proc read, factored out (Rules 13/14).

### 3. The red-then-green regression test, with a positive control

`internal/tools/run_closeout_reap_test.go` — three arms, every process REAL
(the defect is about what the kernel reports, so a simulated fixture would only
assert its own assumptions). The fixture is a `python3` process started with
`Setsid` (so testutil's reaper owns its whole session) and an EMPTY
`GOPI_TASK_RUN_WRAPPER_PID`, so nothing in its tree carries any run's lineage —
which is what makes it safe to kill the group wholesale at cleanup, while each
arm stamps the lineage it means onto the specific member under test:

* `TestRunCloseoutReapTerminatesTheRunsLeakedTree` — the measured shape: a member
  reparented to init (`ppid=1`), standing in the worktree, carrying the run's
  record. It is GONE afterwards (ESRCH, not merely reparented).
* `TestRunCloseoutReapKeepsAThirdPartyInTheSameWorktree` — **the positive
  control**: a live process whose cwd IS the worktree and which carries no record
  of this run survives the same reap call.
* `TestRunCloseoutReapLeavesADaemonisedDescendantAlone` — a `setsid` descendant
  with no record of this run (own session, `sid == pid`) survives.

`cmd/agent/run_closeout_reap_e2e_test.go` — **the Rule 31 arm**: the real
wrapper (`gopi worker <row>`, the entry that runs one row), a real executor child
driven by a mock model whose one tool call is `bash` leaking a background shell,
a real ledger, and a real third party started in the run's own ground. The leak
itself writes its `/proc` facts while it is alive; the assertions read `/proc`
after the wrapper exited.

## Not Done

* **A leftover that has been reparented to init AND carries no record of THIS run
  is not reachable from the run's close-out.** A bare `sleep` the run's tools
  backgrounded inside a launcher that stripped the environment has no lineage, no
  group and no placeable parent — the run is gone, so it cannot be identified as
  its own. That class stays the CLASSIFIER's (`40182d9e` releases the directory it
  pins). Stated in `run_closeout_reap.go`'s header rather than hidden.
* **A descendant that `setsid`s and carries no record of the run survives** — by
  design (the veto), and it is the case the unit test pins. A run that needs such
  a child reaped must keep its record.
* **The process-group registration covers the run child only.** A descendant the
  child itself spawns with its own `setpgid` is reached by R3 (ancestry) rather
  than by its own registered group.
* **The rule lives in the run close-out, not in the bash tool.** A tool call that
  backgrounds a process and returns still leaks until the run ends; stopping it
  at the tool boundary was out of this epic's scope (the change stays in the
  RUN/close-out layer) and is worth its own row if the operator wants it.
* **The two live worktrees measured above are still on disk** — they are other
  rows' grounds (Rule 6a/29), and both are refused by a rung this change does not
  touch (a non-mergeable branch / a live non-orphan holder). The leftovers that
  pinned them are, however, now classifiable-or-reapable rather than forever.
* **`GOPI_TASK_RUN_WRAPPER_PID` still reaches a bash child.** Two design options
  were weighed: strip the launcher record from every tool child (a wide blast
  radius on a durable identity the classifier depends on) versus make the
  process-group/ancestry rungs exact and leave the record alone. The second was
  chosen deliberately; the trade-off is recorded here.

## Verification

- **Build:** `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK.
- **Tests (branch-scoped to the diff):**
  - `go test -count=1 -run TestRunCloseoutReap ./internal/tools/` → `ok` (0.6–0.9s; the R1 rung, the ownership rules, the census)
  - `GOPI_BIN=$PWD/bin/gopi go test -count=1 -run 'RunCloseoutReap|WorkerStop|ReportSeam|TaskRunSeam|CwdRelease|OwnRelease|FinishCleanup|Ground|SelfRelease|Residue' ./cmd/agent/` → `ok github.com/nick/gopi/cmd/agent 35.6s`
  - `go test -count=1 -run 'RunCloseoutReap|Orphan|FinishCleanup|Reclaim|Worktree|SelfGround|OwnRelease|SelfTree' ./internal/tools/` → `ok 113.3s` (the close-out classifier's own suite, unchanged and still green)
  - `go test -count=1 -run 'Rule1|Rule13|NamedLimits|Gate1ErrorWrap|TestGOPIRule3' ./internal/tools/ ./internal/standards/` → both `ok`
  - `go test -count=1 -run TestTestDBScaffoldIsUsedOrTheLedgerEnvIsPinned ./internal/standards/` → `ok`
  - `go test -count=1 ./internal/stylegate/` → this branch's files contribute **no** finding (the run's own files are absent from the report; the gate's pre-existing hits are in other files)
- **Ran:** `GOPI_BIN=$PWD/bin/gopi go test -count=1 -v -run TestRunCloseoutReapOnTheRealRunPath ./cmd/agent/` — the wrapper's real close-out, a real executor child driven by a mock model whose one tool call is `bash`, a real ledger, and a third party standing in the run's ground.
- **Result (the /proc observations, quoted from that run):**

  ```
  # the leak, written by the LEAK ITSELF while alive:
  pid=2136618 ppid=1 pgrp=2136617 sid=23130
             cwd=…/gopi-truncloseoutreape2e            ← the run's own ground
             wrapper=2136586 runid=t-run-closeout-reap-e2e
  ```

  — `ppid=1` (reparented to init, the launcher is gone), its process group is the
  run's own, and the run's launcher records are still in its preserved
  environment: exactly the measured shape, reproduced end to end.

  ```
  # after `gopi worker t-run-closeout-reap-e2e` returned (exit 0):
  the run's leaked child: no longer in /proc (the wait for it to disappear passed)
  the third-party control: pid 2136574 pgrp=2136574 sid=2136574 … — STILL ALIVE
  the wrapper's stdout carried its reap lines
  ```

  **RED→GREEN, and the red is measured, not asserted.** The pre-fix state is the
  leak itself: with the reap disabled (mutation 1) the unit arm's leaked member
  is still respawning **10 s later** — well past the 5 s window the assertion
  waits through — which is the same behaviour the live 10h43m leftover shows.
  Two mutations were run against the real code and both went RED where they
  should:

  | mutation | result |
  |---|---|
  | R1 (lineage rung) disabled | `TestRunCloseoutReapTerminatesTheRunsLeakedTree` FAILS (10.08s) — the leftover is never selected |
  | ownership by cwd instead of lineage | `TestRunCloseoutReapKeepsAThirdPartyInTheSameWorktree` FAILS — the positive control is killed, which is the defect the trap names |
  | (restored) | both arms PASS |

- **Commit:** `e052cc0e` (fix) · `d44fd9e7` (manifest row) · `d3274237` (master merged in: DOC-INDEX union + the doc's class banner)
- **Delivery-point sync / baseline honesty:** master was merged in at `d3274237` (the merge itself was blocked by the append-only DOC-INDEX union lane — resolved as master's file plus this run's one row, verified by `diff <(sort …)` showing exactly that delta). `internal/standards` is RED identically on master (measured in a detached worktree at `95dc93b2`: the same 6 failing tests, none of them this run's), and byte-identical to master in every gate input file.

## Evidence (self-filed)

- `go build ./...` → exit 0, no output.
- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0; the binary was rebuilt AFTER the last source edit (`stat -c %Y` on `bin/gopi` = 1790341165 > `internal/tools/run_closeout_reap.go` = 1790341039) so the Rule 31 run exercised the entity I changed, not an earlier one.
- `go test -count=1 -run TestRunCloseoutReap ./internal/tools/` → `ok github.com/nick/gopi/internal/tools 0.610s`.
- `go test -count=1 -run 'TestRunCloseoutReap' -v ./internal/tools/` → each arm logged its own `/proc` proof, e.g. `RED state (the leak, before the reap): pid 2134570 ppid=1 pgrp=2134569 sid=2134569 wrapper="2134569" cwd=…/001` → `PASS`; the control arm logged `control state (must survive): … wrapper="999999998" …` → `PASS`.
- The live leak measured at the task's start: `/proc/1136914/{stat,environ,cgroup}` → `ppid=1 pgrp=1136914 sid=1136914`, `etime 10:43:34`, `GOPI_TASK_RUN_WRAPPER_PID=382599`, cwd `…/gopi-01a0d5d2…`; the sleep under it moved `2055410 → 2111062`, i.e. the loop is still cycling.
- `grep -rn "GOPI_TASK_RUN_WRAPPER_PID"` over the tree → 2 hits (`cmd/agent` writer, classifier reader) before this change; the value is set from `os.Getpid()` of the wrapper, which is why the reap's R1 compares against the wrapper's own pid.
- MUTATION table above: the commands were the same `go test -run TestRunCloseoutReap ./internal/tools/`, with the named edit applied to `run_closeout_reap.go` and reverted from a copy afterwards.
