<!-- doc-class: historical -->
# The bound case's ctrl-c closeout: a wall clock replaced by the run's own progress

Task: `01a10a45-f3b1-7cdb-9d5d-b237dab80dc2`. Worktree
`/home/user/workspace/gopi-01a10a45f3b17cdb9d5db237dab80dc2`, branch
`task/01a10a45f3b17cdb9d5db237dab80dc2`. Host peerbox, 4 cores; every load figure
below is `/proc/loadavg` read at the start and end of the run that produced it.

## What was wrong

`exitAfterInterrupt` (`cmd/agent/chat_surface_bound_e2e_test.go`) waited
`boundWait = 20 * time.Second` for the REPL to leave after the injected ctrl-c,
whatever the run was doing. Inside a whole `./cmd/agent` package run under
contention it expired while the REPL was still up, and the case failed at
22.06s (2026-10-05, `run-1791172666-79126.json`, load 15.18 14.81 10.74 → 8.50
11.04 11.40, cores 4).

## The diagnosis: the 20s went nowhere, and the run's own log says so

The failing run's own progress lines (same stream, the case's log tail):

```
:1274: ctrl-c closeout: the run has not left yet (2.003s of 20s elapsed; … — 45 model round(s) served so far)
:1274: ctrl-c closeout: the run has not left yet (4.007s of 20s elapsed; … — 45 model round(s) served so far)
…
:1274: ctrl-c closeout: the run has not left yet (18.022s of 20s elapsed; … — 45 model round(s) served so far)
:1274: the REPL did not finish within 20s after the injected ctrl-c; the goroutine is still running and the mock served 45 model round(s)
```

**Two readings 16 seconds apart carry the SAME counter (45).** The mock serves a
round only when the REPL asks it for one, so 45 frozen rounds is the run not
asking for anything: the 20 seconds were not spent on work, they were spent
parked. The injected key's own delivery was measured green at the same instant
(`ctrl-c injected: accepted=true readByEditor=true pending=0 took=2ms`), i.e. the
read loop TOOK the key and then the run neither finished the turn it claimed nor
returned to a read.

That is a real defect being *masked by the wrong criterion*: a wall clock cannot
tell "slow because the host is starved" from "stopped", so the case reported the
second as the first. The same package legitimately takes 910s quiet and
1053–1154s at load 33/4 cores, so no fixed value of `boundWait` is right for both
— widening it (the explicitly rejected move) buys green and keeps the blindness.

What the wait can see instead is the two counters it already had in hand:
`rounds()` (model rounds served) and `s.paint.painted()` (bytes painted).

## The change

`cmd/agent/chat_surface_bound_e2e_test.go`:

- `exitAfterInterrupt` now waits on **progress**, not on elapsed time. It polls
  `rounds()` and `paint.painted()`; a run that keeps moving keeps waiting, a run
  whose **both** counters are frozen for `boundStallWindow` (5s) FAILS with
  "the run STOPPED MAKING PROGRESS", and `boundCloseoutCeiling` (3min) is a
  **separate** failure sentence for a run that never stops moving at all (a loop
  that does not terminate) — never merged with a stall.
- The decision is one function, `boundCloseoutVerdict(stillFor, waited)`
  (Rule 14: one definition, the wait and its guard read the same rule), and the
  stall is decided BEFORE the ceiling, so a frozen run is never reported as
  "still progressing".
- Named constants, no bare literals (Rule 1): `boundStallWindow`,
  `boundStallNoteInterval`, `boundCloseoutCeiling`. `boundExitNoteEvery` is gone
  (its pacing role is now a constant interval, so it could no longer drift with a
  window). `boundWait` survives as the **ceiling of the console-paint waits**
  (notices, periods, announcements), with its comment saying so.
- The progress line now reports both counters and how long they have been still,
  so the log shows the stall forming instead of meeting it only at the failure.
- Nothing was loosened: no assertion removed, no bound widened, no sleep added,
  no skip. The failure conditions are strictly richer (stall, ceiling) than the
  single wall clock they replace.

**Guard**: `TestBoundCloseoutVerdictIsProgressNotAClock` — the red-proof case is
"counters frozen for the stall window while the wait has barely begun ⇒ STALLED",
which the pre-fix wall-clock-only logic answers "keep waiting" (it had no other
input), so reverting the rework turns the guard red.

## Measured

**Control** (`evidence/repro1/`, 2026-10-05, no synthetic load — Rule 27: two
whole-package `make test-budget PKGS='./cmd/agent'` runs + four concurrent
`-run <case> -count=12` lanes, all real test work):

- load_start 33.03 20.86 14.46 / 33.82 20.81 14.41; load_end 7.79 10.75 12.52 /
  14.08 11.99 12.77 (cores 4).
- The case itself was green in all four lanes (48/48) and was **not** in either
  whole-package failure list; the two whole-package failures were
  `TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree` (two suite runs
  colliding through `/dev/shm/suite-cmd-agent-run-<N>`) and
  `TestTaskRunSpawnsTheChildInsideItsOwnLinkedWorktree` /
  `TestDaemonWorkerKeepsTheTaskTextOutOfItsOwnStdio` — a known
  concurrent-suite-run collision family, NOT this case.
- Streams: `run-1791174579-389647.json`, `run-1791174579-389640.json`.

**After** (`evidence/after/`, `evidence/final/`): the case green alone 3× and
10× per lane × 4 concurrent lanes, and the whole `./cmd/agent` package at
`make test-budget` green, with the closeout reporting `the REPL left 0s … (36
model round(s) served, 40764 byte(s) painted)`. Full numbers and stream paths are
in the task report.

## Honest scope (the counter-finding, not this fix)

The frozen-counter stall is what the criteria CAN see, and it is what the case now
reports. The production question behind it — WHERE a key that `readByEditor=true`
went, between the read loop taking it and the session ending — is a `cmd/agent`
read-loop question, not a test-wait question, and it is **not** fixed here: this
change makes the defect VISIBLE and cheap to catch (5s, both counters printed)
instead of masking it as a slow host.
