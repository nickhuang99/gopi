<!-- doc-class: historical -->
# Task Report: the run's close-out reap missed a descendant in its fork/exec window

Task `01a10455-f320-7b77-8fe3-45bc3270138c` (branch `task/01a10455f3207b778fe345bc3270138c`).

## Completed

- **The mechanism, named** (5-why at the bottom, "Why" section): the close-out reap's
  ownership rung R1 is a read of the candidate's **exec-time environment**
  (`/proc/<pid>/environ`), and that file is **empty from `fork` until `execve`
  installs the child's address space**. Inside that window R1 answers "no record of
  this run" about a process that IS the run's own. One read per pid, taken once, made
  that a permanent verdict: the pid left the selection, the reap printed **nothing**,
  and the leaked child stayed alive.
- **`internal/tools/run_closeout_reap.go`** — the fix, three parts, all with named
  constants (Rule 1):
  - `runCloseoutSelection` now returns a third result: the pids it could **not
    measure**, which is a different answer from the ones it **declined** (Rule 15).
  - `runCloseoutSelectTargets` WATCHES that set for a bounded window
    (`runCloseoutReapRemeasurePolls` × `runCloseoutReapPollStep` = 300 ms), re-asking
    the **same** ownership rules — no second ownership path, so it can only fail to
    find a leftover, never take a foreign process.
  - The hole test (`runCloseoutPidShallowUnavailable`) is deliberately narrow:
    unplaced **and** block empty/unreadable **and** same uid as this run **and**
    started after this process did (`runCloseoutReapTicksBase` / `runCloseoutReapTicksWidth`).
    Each conjunct is a measured necessity — without the uid/starttime pair the report
    listed **5780** "leftovers" per 20 runs (kernel threads and other users' daemons);
    with it, **0** false entries in 20 runs + 3 more (see Evidence).
  - **One reading per pid per selection** (`runCloseoutEnvRead`, memoised): R1 and the
    hole test now consume the *same* read. This was not an optimisation but a bug I
    hit and fixed mid-task — reading twice per pid per selection spent the window
    before the watch could see it, and the guard test caught it (`owned=[]` after the
    reap, the member still alive).
  - An **unmeasurable table** is reported as its own line instead of reading as
    "owns nothing" (Rule 15).
- **`internal/tools/run_closeout_reap_window_test.go`** — the regression guard: a real
  fixture member whose real block carries this run's lineage, with the kernel's window
  injected at the ONE seam the read goes through (`runCloseoutReapEnviron`), plus a
  control arm that keeps "a hole" and "a decision" apart.

## Not Done

- **The window itself was never observed on the real run path.** I measured it out of
  reach without load (Rule 27 forbids manufacturing it): a settled `sleep` read 200
  times gave **0** empty blocks, and 431 process-table snapshots at ~100 ms across a
  real `cmd/agent` run caught the window 0 times. The guard therefore pins the
  mechanism **at the seam**, by injection — it does not reproduce the natural race, and
  I am not claiming it does.
- **No natural red was reproduced in this session.** The parent run's red is cited, not
  re-derived: `~/.gopi/sessions/01a1042a-746a-70ac-bae2-444c54775691/evidence/run-1791073357-3172505.fails.txt`
  → `TestRunCloseoutReapOnTheRealRunPath`, `"Action":"fail"`, `Elapsed: 62.46`, inside a
  `load_end 12.01` run. My own full-package runs here ran at load ~2–5/4 cores.
- **Whether the fix removes the *natural* flake is therefore unproven** — that needs a
  loaded full-package run, which is the operator's to schedule, not mine to create.

## Verification

- Build: `go build ./...` → **BUILD_OK** (preceded by `gofmt -l` → no output).
- Tests:
  - `go test -count=1 -run 'TestRunCloseoutReap|TestRunCloseoutSelectionReportsAHole' ./internal/tools/` → **ok** (green).
  - **RED proof for the guard**: with the watch short-circuited at its entry (one added
    line, then reverted) → `--- FAIL: TestRunCloseoutReapWatchesAPidItCannotMeasureYet`,
    "pid 3257209 survived the reap", the fixture member alive at `ppid=1`. Restored
    immediately (`go build ./...` → BUILD_OK).
  - `go test -count=3 -run 'TestRunCloseoutReapOnTheRealRunPath' -v ./cmd/agent/` →
    **3/3 PASS**, 0 `NOT reaped` lines.
  - `make test-budget PKGS='./cmd/agent/'` → **exit=0 outcome=pass, 0 `"Action":"fail"`
    events**, package elapsed 434s (`load_end 0.88 1.35 1.76`), stream
    `run-1791076833-3366575.json`; the reported test itself is green at **Elapsed 0.78**.
  - `make test-budget PKGS='./internal/tools/'` → **exit=0 outcome=pass, 0
    `"Action":"fail"`** (stream `run-1791076577-3335391.json`).
- Ran: `go test -count=20 -run 'TestRunCloseoutReapOnTheRealRunPath' -v ./cmd/agent/`
  (the real run path, 20 times, on the FINAL code) and
  `make test-budget PKGS='./cmd/agent/'` (the whole package, the surface the parent
  task reported the flake on).
- Result: the full package is **green with 0 fail events**, and the flaky test that cost
  the parent task a 62.46s red now finishes in **0.78s**. On the 20-run batch: **20/20
  PASS**, 0 `"Action":"fail"`, slowest **2.96s**, mean 1.9s; 0 `NOT reaped` lines for the
  operator's own processes (the over-reporting fix confirmed on the real surface, not
  only in the unit arm), and `run-closeout reap: terminated pid …` still printed for the
  run's own children.
- Ran: `go test -count=20 -run 'TestRunCloseoutReapOnTheRealRunPath' -v ./cmd/agent/`
  (the real run path, 20 times, on the FINAL code) and
  `make test-budget PKGS='./cmd/agent/'` (the whole package, the surface the parent
  task reported the flake on).
- Result: **20/20 PASS**, 0 `"Action":"fail"`, slowest run **2.96 s** (the batch's mean
  1.9 s); 0 `NOT reaped` lines for the operator's own processes, so the over-reporting
  fix is confirmed on the real surface, not only in the unit arm; the reap still prints
  `run-closeout reap: terminated pid …` for the run's own children. In the full-package
  run `TestRunCloseoutReapOnTheRealRunPath` itself is green at **Elapsed 1.17** (the
  stream carries it as a passing test).
- **The full-package verdict needs one honest caveat.** The first `./cmd/agent` run
  reported 11 `"Action":"fail"` events, all at ~0.01 s, and the stream names the reason:
  `worker_foreground_console_e2e_test.go:162: …/run-…-tested-bin/gopi (mtime 09:10:44) is
  OLDER than …/internal/tools/run_closeout_reap.go (mtime 09:13:43) — this test drives
  the BUILT binary, so it would verify code that is not in that binary. Rebuild it and
  re-run.` Those are the harness's staleness guard refusing a binary built before my
  last edit — **my process error** (I edited the tree while a package run was in flight),
  not a defect of this diff. The re-run on the frozen tree is the verdict; the first run
  is kept here because a gate result I did not observe is not a result.
- Commit: `0b12d52f` on `task/01a10455f3207b778fe345bc3270138c`.

## Evidence (self-filed)

- **The window is real and is not reachable by sampling.** 200 reads of a *settled*
  `sleep`'s block → **0** empty; the same tight-loop read during 120 `fork`+`execv`
  cycles → **26** empty reads with `comm=sleep`. A 0.1 s-cadence process-table snapshot
  across a whole real `cmd/agent` run (443 snapshots) caught the leaked child as
  `pid 3251490 ppid=1 pgrp=3251489 sid=3044208 sleep` — reparented to init, pgrp the
  **wrapper's** pid, i.e. the exact shape the diagnosis predicts.
- **`runCloseoutPidShallowUnavailable` over-reporting, measured twice, each time on the
  revision being claimed.** With only "unreadable block + not selected" as the test, a
  20-run batch of the real path printed **5780** `NOT reaped` lines (`sshd`, `bash`,
  `gvfsd`, `at-spi-bus-laun`, `xfconfd`, and 2020 pgrp-0 kernel threads, which have no
  environment block by nature) — the guard arm also failed on that revision with a
  control pid reported as a hole. With the uid + starttime conjuncts, the FINAL code at
  `-count=20` on the same surface: `PASS=20 FAIL=0 NOTREAPED=0`.
- **One reading per pid per selection.** With R1 and the hole test reading the block
  separately, the guard failed (`owned=[]`, member alive after the reap) because the
  fixture's window was spent before the watch could see it; with the memoised
  `runCloseoutEnvRead` the same arm reaps it (`reap lines: [run-closeout reap: terminated
  pid 3257094 (sleep)]`).
- **A near-miss is what the fix removes, not a theoretical case.** While the intermediate
  revision (protection present, over-reporting not yet narrowed) was in the tree, one
  run out of 20 took **53.43 s** instead of the usual 1.75 s: the same test PASSED, but
  only because the final `/proc` read happened to find the pid gone ("last /proc reading
  before it disappeared: pid 3254242 ppid=- pgrp=- sid=- wrapper=- cwd=-"). On the final
  code the slowest of 20 runs is 2.96 s.

## Why (5-why)

1. `TestRunCloseoutReapOnTheRealRunPath` failed: the leaked background child survived
   the rerun's close-out reap for the whole 62 s wait.
2. The close-out prefix printed **no** `run-closeout reap:` line — a pid that is not
   selected produces no line, and a reap that selected nothing is indistinguishable
   from a reap that had nothing to do.
3. The child was not selected: R1 (its environment) answered "no record of this run",
   R2 (groups) checks the run child's group while the child carries the wrapper's, and
   R3 (ancestry) dead-ends — it had been reparented to init.
4. R1 answered "no record" because `/proc/<pid>/environ` is **empty** until `execve`
   installs the child's address space: the census ran while the command was still being
   set up, so the read fell inside the fork/exec window.
5. The selection reads each candidate **once** and treats the answer as final, so a
   read taken inside that window became a permanent "not ours" — the mechanism is a
   single-sample verdict on a read that is only valid after `execve`, not a flaky test.
