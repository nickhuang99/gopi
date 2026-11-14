<!-- doc-class: historical -->
# Task Report: the close-out reap called a ZOMBIE a fork/exec window, and never waited on it

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a17b72-9cd4-732b-b5c5-94d542bc2b49`, branch
`task/01a17b729cd4732bb5c594d542bc2b49`, baseline `d46f4a4`.

## The defect, in one line

A ZOMBIE's `/proc/<pid>/environ` **cannot be read at all** (the address space is
released, so the kernel refuses the read), and the reap's fork/exec hole test had
no way to tell "the block is gone" from "the block is not written yet". Every
zombie was therefore filed as a **hole in the measurement**, the bounded
re-measure that exists to close a hole could only **expire** (a zombie never
starts answering), the pid entered neither the selected set nor the reaped set,
and the close-out printed

```
run-closeout reap: pid N (…) NOT reaped — pgrp X is this run's own but its
environment block never became readable (the fork/exec window), and the bounded
re-measure expired
```

about a process that had **already finished**. Under that sat the second half:
`wait4` was only ever issued for a pid the run had **selected**, so a zombie that
*was* this run's own direct child was never waited on either — the kernel kept its
pid reserved forever, and every presence test on the host kept answering "still
there".

## The measurement that decided the fix

A zombie's environment block is refused, not empty — and the two differ in a way
the code has to act on:

```
$ python3 -c 'Popen(["sleep","300"]) → SIGKILL → read /proc/<pid>/environ'
environ read error: [Errno 13] Permission denied: '/proc/2683434/environ'
stat: 2683434 (sleep) Z 2683433 2683431 2532733 0 -1 4228108 62 0 0 0 0 0 0 0 20 0 1 0 97726710
state letter: Z
```

Both shapes reach `finishCleanupProcEnviron` as `("", false)`, and only the
**state letter** says which one it is. That predicate already existed — three
times over, in fact, and none of them was reachable from the reap:

| predicate | where | why it was not usable |
|---|---|---|
| `pidIsZombie` | `internal/tools/taskzombie.go` | reads the letter, decides only for the ledger's `pidAlive` |
| `pidAlive` / `PidAlive` | `taskzombie.go`, `service_control.go` | liveness, not the reap's selection |
| `procstat.Alive` → `VerdictGone` | `internal/procstat` | session-liveness verdicts |

So the fix is not a second spelling of "state == Z": it is **routing the one
existing predicate into the two places in the reap that never asked it**.

## What changed

- **`internal/tools/run_closeout_reap.go`** — the selection grows a THIRD
  outcome. It used to answer `(owned, unmeasured)`; it now answers
  `(owned, unmeasured, zombies)`, because "declined by every rule", "could not be
  measured" and "measured, and the kernel says it is already dead" are three
  different facts and collapsing any two of them is the silent-failure shape
  (Rule 15).
  - `runCloseoutPidShallowUnavailable` (the fork/exec hole test) now returns
    **false** for a zombie: the hole is the window where an EMPTY block is about
    to be filled, and a zombie's block will never be filled again.
  - `runCloseoutSelection` asks the zombie probe **only** of pids that already
    qualify as holes — the cheap gate first, the extra `/proc` read second. A
    zombie of this run always qualifies (unreadable block, this uid, arrival after
    this process started), and one that does not qualify is not this run's to
    report.
  - `runCloseoutZombieLines` is the DEAD arm. It **re-reads** the state (a parent
    may have waited between the table walk and the line, and a claim about a pid
    that has left the table is a false claim), then asks the kernel — in ONE
    `wait4(WNOHANG)`, because a process the kernel has already ended either IS this
    process's unreaped child (one wait releases it, measured) or is somebody
    else's, and then no polling changes the answer. Three outcomes, three lines:
    reaped, not-ours (`ECHILD` — Rule 42: this waits only on a handle it holds,
    and it signals nothing), and the failed wait (reported, never swallowed).
  - `runCloseoutReapWaitOwnChild` was split so the polling loop and the single
    attempt share ONE `wait4` and ONE reading of the kernel's three answers
    (`runCloseoutReapTryWaitChild`). The polling form keeps its own contract: only
    the kill path needs it.
  - `runCloseoutRuleZombie` (`DEAD(zombie)`) is a new rule name in the trail
    vocabulary, so an operator can grep "was this a fork/exec hole or a corpse?"
    afterwards — the two used to be the same line.
- **`internal/tools/run_closeout_reap_zombie_test.go`** — the minimal repro and
  its control (below).
- **`internal/tools/run_closeout_reap_window_test.go`** — one existing arm's
  FIXTURE had to change, and the reason is a finding of its own. The hole-scope
  guard plants a "stray": another process of this uid whose block the test then
  lies about. That stray used to be `/bin/true -h`, which **exits immediately** —
  so it stood in the process table as a **zombie**, and the arm passed *through
  the very defect being fixed*: a dead pid was counted as "another process's
  unmeasurable hole". With the zombie routed to the DEAD arm the stray stops
  entering `unmeasured` and the arm's own premise fails. The stray is now
  `sleep 120` (`runReapTestStrayLifetime`), which is the shape the arm always
  said it wanted: a live foreign process whose block cannot be read.

## The regression guard, and what makes it red

`TestRunCloseoutReapRoutesAZombieToTheDeadArm` builds the shape out of real
processes: spawn `sleep 300`, `SIGKILL` it, **do not wait** — then hand the pid to
the production selection. It asserts the pid is NOT in `unmeasured`, IS in
`zombies`, and is NOT in the selected set (a zombie carries no readable block, so
no ownership rung answered for it and it must not be signalled on one that could
not). Then it runs the exact call the wrapper's tail makes
(`RunCloseoutReapRunChildren`) and requires `reaped pid N` and that the pid has
left the process table.

`TestRunCloseoutReapKeepsALiveUnreadablePidAHole` is the CONTROL, and without it
the fix would be indistinguishable from "anything unreadable is dead" — which
would silently drop the fork/exec window the neighbouring arms exist to cover. It
holds a LIVE child's block unreadable at the ONE seam the hole test reads
(`runCloseoutReapEnviron`, scoped to that pid) and requires the pid to stay a
hole and stay OUT of the dead arm.

**Red is a one-line mutation**: delete the zombie branch in `runCloseoutSelection`
(the `if runCloseoutPidIsZombie` routing) and the zombie falls back into
`unmeasured` — which is precisely the pre-fix behaviour. Measured both ways; the
two runs are in the task's session directory.

## What was NOT done

- **The natural incident was not re-created.** Reproducing it end-to-end means
  killing a real `gopi task run` wrapper's child mid-close-out; the arm instead
  constructs the *state* (a real unreaped zombie) and drives the production
  selection and reap over it. That is the defect's own shape, not a mock of it —
  but the wrapper-level path is argued from the code, not observed.
- **A zombie older than the run is not reported.** The hole gate carries the
  existing uid + arrival bound, so the host's pre-existing zombies stay out of a
  run's close-out. That is deliberate (a report listing every foreign zombie buries
  the one that matters), and it is stated here rather than hidden.

## Verification

Build: `go build ./...` → BUILD_OK (after `go vet ./internal/tools` → VET_OK).
Tests: `make test-budget PKGS='./internal/tools'` — see the report's `Ran:` lines.
Ran / Result: see the task report (`## Verification`), which quotes the two test
runs (green, and the mutation's red) verbatim.
