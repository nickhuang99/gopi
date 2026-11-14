<!-- doc-class: historical -->
# finish-cleanup can now wait for the lock — and says NOT EXECUTED when waiting cannot help

Epic `01a14845` (branch `task/01a14845193f707593d8770e6fef4d88`, 2026-10-17).
Three defects, one surface: the refusal `gopi task finish-cleanup --apply` printed
when another merge face held the single-instance lock.

## The defect, as measured

`runTaskFinishCleanup`'s APPLY arm called `mergerWorkLock(prog)`, which does a
**non-blocking** `flock` on `<gopi dir>/serve.lock` and `os.Exit(1)` on
`EWOULDBLOCK`. A resident `gopi merge --every D` loop holds that flock for its
**entire life** (it is the same one-funnel acquisition, taken once and released only
when the loop's last descriptor closes — memory #3494). So while any loop ran, the
face that exists to reclaim residue worktrees could never run at all: a permanent
refusal wearing a safety check's clothes.

Two secondary defects lived in the very message the refusal printed:

1. `lock-kernel-records: … | live pids holding an fd on that inode: 3496987, …`
   listed every pid whose `/proc/<pid>/fd` directory could be **read**, with no
   liveness proof. Measured: pid 3496987 was reported as a *live* holder while
   `/proc/3496987` was GONE. The label asserted "live"; the scan only proved
   "readable".
2. `(scan incomplete)` — the admission that the scan could not judge every
   process — was **appended to a list that still asserted holders**. Unknown was
   rendered as present. Rule 15 says the opposite: "I cannot say" and "these are
   they" are different answers, and only one of them may wear one label.

## The fix

### ① Bounded wait — `cmd/agent/merge_lock_wait.go`

`serveLockAcquireBounded(prog, budget, poll, readArgv)` tries the **same**
`acquireServeLock` seam once, and if the refusal is the contention sentinel
(`errServeLockHeld`) it retries until a budget expires. The flock is never removed,
never stolen, never double-taken: merge mutual exclusion is exactly what it was
(constraint 1 — the correct shape is WAIT, not break). No new lock, no pid file, no
state file, no daemon: the acquisition funnel stays the single owner (Rules 2/13/35).

- `mergeLockWaitDefault = 20s` — a **bound**, not a hope (Rule 1).
- `GOPI_MERGE_LOCK_WAIT` — the override, read in **exactly one place**
  (`mergeLockWaitBudget`, Rule 2); a bad value is reported and the default used
  (never a silent fallback).
- Every outcome is announced: the wait's start (budget), its success (elapsed), a
  rejected override, the spent budget, and the resident-loop verdict.

### ② Resident-loop verdict — NOT EXECUTED, with a next step

Before **every** sleep, `serveLockLoopHolder` asks `tools.LockEvidences` who holds
the lock and reads each live holder's `/proc/<pid>/cmdline`; if a whole argv token
equals `mergeEveryFlag` (`--every`, the parser's own constant — no second spelling)
the holder is a resident loop and the run stops right there instead of burning the
budget:

```
gopi task finish-cleanup: NOT EXECUTED — the single-instance lock is held by a
long-lived resident loop (pid 1576945, argv: flock -x …/serve.lock bash -c … --every 5m)
after 66.931ms of looking.
a resident loop holds the flock for its ENTIRE life (it is released only when the
loop's last descriptor closes), so no bounded wait can ever succeed — this run did
not wait out its budget for it.
next step: stop the loop with `gopi merge --stop`, then re-run `gopi task
finish-cleanup`. Do NOT delete serve.lock and do NOT kill the loop mid-pass: the
flock is the merge guard, and removing it would let two passes write the same refs.
```

The token comparison (not a substring search) matters: a path or commit message
containing the letters `--every` inside a longer word is not the flag, and mistaking
one would turn a waitable one-shot face into a "permanent" verdict.

The sentinel `errServeLockNotExecuted` (`"NOT EXECUTED"`) is what lets a caller — and
a test — separate *ran nothing* from *ran and failed* with `errors.Is`, so the
wording cannot be matched by prose.

### ③ fd-holder liveness + unknown-is-unknown

- `serveLockFdHolders` now filters every candidate through `tools.PidAlive` — the
  one liveness predicate in the CLI (signal-0, zombie counts as gone) — so a pid
  with no `/proc` entry can never be printed as a live holder.
- The scan is `serveLockFdHoldersAt(root, ino, alive)`: the same code the tests
  drive over a fixture procfs (Rules 2/13 — no copy of it in the test).
- The **label** now follows the answer (`serveLockFdHoldersLine`): a complete scan
  says `live pids holding an fd on that inode: …`; an incomplete one says
  `pids PROVEN to hold an fd on that inode: … (a lower bound — … this list is NOT
  the whole answer)`; a scan that judged nothing says `unknown (… this is NOT
  'none', and it is NOT a list of holders)`.

## Why not a blocking flock

`flock(2)` without `LOCK_NB` would block indefinitely — turning a refusal into a
hang, with no budget and no report. A bounded poll is the honest shape: it can be
reported, measured, and overridden, and it fails with a verdict instead of silence.

## Verification (what was actually run)

```
$ go build ./...                                             # OK
$ make test-budget PKGS='./internal/tools ./cmd/agent'        # see the task report
$ go test ./cmd/agent/ -run 'TestFinishCleanupWaitsForAOneShotHolder|
      TestFinishCleanupReportsAResidentLoopWithoutSpendingTheBudget|
      TestFdHoldersExcludeDeadPidsAndRenderUnknownAsUnknown' -count=1   # ok
```

Red proofs (each mutation reverted immediately; `git diff HEAD -- cmd/agent/`
sha256 identical before and after, `merge_lock_wait.go` sha256
`c29095ef…` restored byte-identical):

| repro | mutation | red |
|---|---|---|
| ① a one-shot holder must not block the face | `serveLockAcquireBounded` returns the refusal instead of waiting | `must acquire the lock once the holder goes away … got: serve lock held by a live daemon` |
| ③a dead pid is not a live holder | drop the `alive(pid)` filter | `pid 3496987 has NO /proc entry and must never be reported as a live fd holder (got [4242 3496987])` |
| ③b unknown is not present | one label for both answers | `an incomplete scan must not use the 'live pids holding an fd' label` + the two missing words |

Real invocations (sandbox profile `sandbox-lockproof-b073437e`, so the operator's
live `~/.gopi/serve.lock` was never touched):

- loop-shaped holder (`flock … bash -c '… while :; do sleep 2; done' --every 5m`):
  `gopi task finish-cleanup --apply` → NOT EXECUTED in **0.112s**, naming pid
  1576945 and its argv, i.e. the budget was NOT spent.
- one-shot holder (`flock … bash -c '… sleep 3'`): `… --apply` → `waiting up to 20s
  … acquired after waiting 1.508s` then `finish-cleanup (APPLY): candidates=0 …`.
- the same refusal's evidence lines now read `pids PROVEN to hold an fd on that
  inode: … (a lower bound …)` — no dead pid, and the incomplete scan no longer
  wears the "live pids" label.

`git branch --list | wc -l` was 4 before the loop run and 4 after it, and every
non-dry run there happened in the sandbox profile; the real profile's branches were
never touched by this task (Rule 30: the worktree closes clean).

## What this does NOT do

- It does not give finish-cleanup a second path around the lock, and it does not
  make the loop stoppable mid-pass: `gopi merge --stop` is still the operator's lever.
- Other faces that call `mergerWorkLock` (`task merge`, `task adopt`,
  `merged-cleanup`, `merge-execute`, `sweep-worktrees`, `task merge` hook) keep the
  instant refusal. They are script/hook triggers whose semantics are "skip and let
  the next pass land it"; waiting there would be a behaviour change with its own
  measurements. The wait is opt-in per face (`mergerWorkLockWaiting`) and only
  finish-cleanup uses it today.
