<!-- doc-class: historical -->
# A pid is announced before it is the process it will be — the detach read in the run-closeout reap fixture

Task `01a130ff-8b70-7d62-872b-0fe61667d773` (bug flow). File:
`internal/tools/run_closeout_reap_detach_guard_test.go` (new),
`internal/tools/run_closeout_reap_test.go` (fix).

## The symptom, and it is load-dependent

`TestRunCloseoutReapLeavesADaemonisedDescendantAlone` is green alone (4/4) and red
on some whole-package runs of `make test-budget PKGS='./internal/tools'`. Verbatim
from the run stream that reproduces it
(`/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791822236-2682861.json`,
whole-package scope `github.com/nick/gopi/internal/tools`, `0.10s`):

```
=== RUN   TestRunCloseoutReapLeavesADaemonisedDescendantAlone
    run_closeout_reap_test.go:659: daemon member is not a session leader
        (sid=2706314 ok=true, pid=2706316) — the fixture did not detach it
--- FAIL: TestRunCloseoutReapLeavesADaemonisedDescendantAlone (0.10s)
```

`sid=2706314` is the **fixture's own session**: the fixture is started with
`Setsid`, so it is a session leader and every member it spawns is born into its
session. The assertion was not asking for something wrong — it was asking
**too early**.

## The mechanism

The fixture writes a member's pid to the pid file the instant
`subprocess.Popen(['setsid','sleep','300'])` **returns**:

```python
p = subprocess.Popen(argv, env=e, cwd=cwd, ...)   # ← announces p.pid here
with open(pidfile, 'a') as f: f.write(str(p.pid) + "\n")
```

Between that return and the member's own `exec` there is a window in which the
member **exists, answers `kill(pid,0)`, is runnable (`state=R`), and still
carries the session it inherited by `fork`** — `setsid(2)` is the child's own
work, done at exec time. The test took **one** session read in that window.

Measured with the fixture's own spawn shape — 400 **sequential** short-lived
children, each killed and waited for before the next iteration, **no load
generated** (Rule 27) — reading `/proc/<pid>/stat` field 6 through the same
instrument (parse after the last `')'`):

```
iterations=400  iterations with >=1 pre-detach read = 399   (round 2: 400)
window measured (pid announced -> session==pid):
    min=25.7us p50=258us p90=281us max=618us
pre-detach reads per caught iteration: min=1 p50=17 max=46
```

So the read's outcome is decided by whether it lands before or after ~260µs of
work the fixture's own child does. That is a **sample, not an observation**, and
under whole-package load the sample lands on the wrong side — which is the entire
difference between the isolated run (green 4/4) and the package run.

**Why the fixture's OTHER read of the same window did not flake the same way**:
the cleanup's ownership read is `/proc/<pid>/environ`, and it WAITS for the block
to be published (`runReapTestMemberSettleTimeout`, polled). The session read had no
such wait — it was taken immediately after `kill(pid,0)` first succeeded, which is
*satisfied at fork*. Measured through the same window (200 sequential children):
`/proc/<pid>/environ` is EMPTY in **24/200** reads taken with no slack, and in
**0/200** reads taken with the 3ms of slack the existing pid-file poll gives — the
same window, closed by a wait instead of sampled.

## The fix — wait on the kernel's own answer

`runReapTestAwaitSessionLeader(pid, read)` returns the instant **`session == pid`**,
a state only the member's own `setsid(2)` can produce:

- it is **not** a `sleep`: it ends the microsecond the kernel says so, so it is
  load-independent, and its budget (`runReapTestDetachTimeout`) is a **bound the
  passing path never spends**;
- the **assertion is unchanged** and is still asserted on the *value* it waited
  for (`sid != daemonPid` fails), so a helper that answered `true` without saying
  what this arm needs cannot hide behind its own boolean;
- a member that never detaches **fails loudly** naming pid, last reading, budget
  and the fixture pid — never a skip (Rule 15).

Nothing in production changed: `runCloseoutPidOwned`'s VETO asks the same
question (`sid == pid`) and is unaffected.

## The guard — the window HELD OPEN, not sampled

`internal/tools/run_closeout_reap_detach_guard_test.go`. Asserting the window
would reintroduce the load sensitivity being removed, so the guard reproduces the
state **on demand**: the probe child stops itself before its exec

```sh
kill -STOP $$; sleep 1s; exec setsid sleep 20
```

A stopped process cannot change its session, so the pre-exec state is held for as
long as the guard likes, on any load. The `1s` is **derived**, not typed twice:
it is `runReapTestDetachTimeout / runReapTestProbeDelayDivisor`, and
`TestRunCloseoutReapProbeDelayMatchesTheBound` fails if the shell spelling and the
Go bound ever drift apart — the arm's claim ("a wait that merely READS cannot
succeed inside this delay") is only as strong as that number.

The probe is started through an **inner shell** (`sh -c '<probe>' & wait $!`), so
the process that stops itself is a *grandchild*: its own `setsid` takes it out of
every process group this test owns, and neither testutil's reaper nor
`kill(-pgid)` can reach it. The test therefore reaps it **by the kernel's parent
field** (SIGCONT then SIGKILL, polled, and reported by pid if it refuses to leave,
Rule 15) — Rule 6's creator-owns-cleanup, in the one shape where the creator is
the only possible owner.

| arm | what it pins |
|---|---|
| `…ClosesTheForkExecWindow` | the child is measured carrying **this process's** session while stopped (the flake's own observation), then the wait must walk out of it; a single read answers with the inherited session and reddens |
| `…NeverClaimsADetachItDidNotSee` | negative control on a **real** child parked in the window for the whole arm: the wait refuses, and the refusal carries the last reading |
| `…DecisionTable` | the rule over an **injected** reader: first reading that names the pid ends the wait; a refusal reads the table through and stops at its budget |
| `…BudgetIsABoundNotADuration` | an immediate answer must return far inside the bound — an order of magnitude, never a deadline |
| `…ProbeDelayMatchesTheBound` | the probe's shell-spelled delay and the Go bound are the same duration, and the probe that is actually started carries it (Rule 13: one spelling of one value) |

The guard's delay lives in the **probe child** (`sleep 0.3` before its exec), never
in the guard: no test here sleeps a fixed time to make a race likely.

## Red proofs (both observed, then restored)

- **Mutation A — revert the fix at its call site** (one read, the pre-fix shape):
  the live arms still **PASS** — recorded, not hidden. Measured twice: one run of
  `TestRunCloseoutReapLeavesADaemonisedDescendantAlone` (PASS), then
  `-count=20` over both live arms (**ok 30.1s, 20/20**). The reason is arithmetic,
  not luck: each run is a fresh fixture whose members are polled into existence, so
  the announcement-to-read gap is milliseconds there, while the ~260µs window is
  what a *whole-package* run collides with (~1 failing run in 50 measured over
  three days of `test-budget` streams). A single live run is therefore **not** a
  reliable detector of this defect — which is exactly why the red burden sits on
  the arms below, and why this document says so instead of implying the live arm
  reddens.
- **Mutation B — disable the wait in the helper** (`if true { return last, false }`,
  i.e. one read):
  - `…ClosesTheForkExecWindow` → `--- FAIL`, `the detach wait answered
    (sid=2011861, false) for a child that DOES detach — it sampled the fork/exec
    window instead of waiting for the kernel's own answer`;
  - `…DecisionTable/detached_after_the_window_closes` → `wait = (sid 1000, ok
    false), want (sid 4242, ok true)`;
  - `…DecisionTable/never_detaches` and `/never_readable` → `1 reading(s),
    want 3..6 / 4..7`.
- **Restore proven by content, not by an index-relative diff** (Rule 5): the file
  was snapshot to the session dir before mutation and
  `sha256sum` is identical after (`8e73cdf6849b926684b4c31d61ad7a6d49adf482f142db908126bee5d5c51ea3`),
  with `grep -c MUTATION` → `0`.

## Not done (honest)

- **The live arm cannot be made red-on-revert by a single run** and this document
  says so instead of implying otherwise: measured above, mutation A passed the
  unmuted live arm once. The deterministic red lives in the guard's held-open
  window and its injected-reader table.
- The same *sample-don't-wait* shape exists in principle wherever a test reads a
  child's identity right after `kill(0)` succeeds; this change fixes the one arm
  that was measurably red and does not sweep the package.
