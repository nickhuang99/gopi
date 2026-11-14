<!-- doc-class: historical -->
# devdoc: `gopi merge` takes the single-instance lock (epic merge-lock, 2026-09-19)

> **DISPOSITION (2026-11-01, merge-removal epic).** Classified `historical`
> rather than `normative`: the subject of this page — the top-level `gopi merge`
> command's lock behaviour — is a command that no longer exists, so nothing here
> is a standing claim a reader can act on. `./bin/gopi merge` now exits **1**
> with ``gopi: `merge` is RETIRED — use `gopi task merge` ``.
>
> **The MECHANISM is live and this page is still its account.** The single-
> instance lock (`mergerWorkLock`, `cmd/agent/merger_work_lock.go`) was carved
> out of the retired daemon and is held by the surviving face, `gopi task merge`
> — same lock, same serialization between the command and the merger's tick. So
> the defect analysis, the SIGKILL-holder trap and the exit-code contract below
> are true of the lock as it runs today; only the spelling that takes it has
> changed. A reader re-deriving any of this should re-measure against
> `gopi task merge`, and `man/gopi-merge.md` is the engine's contract page.

## The defect, in one measured sentence

`gopi merge` (cmd/agent/merge_run.go) walks every repo in the settings.json
`repos` table, lands fast-forwards, hands conflicted branches to a short-lived
gopi, deletes merged branches and rebuilds repos — **while holding nothing**.
Two such runs at once, or one of them beside the resident `gopi merger --every D`
loop, or beside the git post-receive hook's `gopi task merge`, write the same
refs with no serialisation: a double-merge.

The asymmetry is what made it a defect rather than a design: every OTHER merge
face already takes the single-instance flock (merger_work_lock.go →
acquireServeLock). The foreground command, which does the same work and has the
strongest claim to the guard (it lands refs itself, without parking anything),
was the one face outside it.

## The fix is an ADOPTION, not a second lock

`cmd/agent/merge_lock.go` adds one CALLER of the existing seam. Nothing about
the mechanism is re-implemented, so everything that makes the existing lock hard
to defeat comes along for free:

- the **stray-holder refusal** (a holder whose lock path was unlinked underneath
  it keeps a guard nobody can see — the 2026-09-12 double-merge),
- the **identity re-verification** (flock lives on the INODE, not the path),
- the **ancestor-admission rule** (`mergerLockHeldByAncestor`): a face the live
  holder is WAITING ON must not be refused its own ancestor's lock, or the guard
  becomes a deadlock.

Two measured facts that are easy to get wrong, and are the reason this file
exists rather than a one-line `acquireServeLock()` call at the top of runMerge:

1. **The lock path is `<home>/.gopi/serve.lock`, not `<home>/serve.lock`** — the
   data root is `GOPI_HOME/.gopi` (`config.ProfileDirName`). A caller that
   derives the path itself gets a lock nobody else contends on.
2. **The merge gopi runs IN THIS PROCESS.** `mergeHandOver` calls `ag.Run`; it
   does not exec a binary. So the tools the run spawns (bash, git, a `gopi task
   merge` a report decides to make) are children of THIS process and read
   `os.Environ()`. The admission marker must therefore be EXPORTED into this
   process's own environment (`mergeExportLockMarker` → `os.Setenv`), not handed
   to a hypothetical spawned child — a child-only env would reach nobody.

## Why release needs no code: "even if merge crashes"

The briefing asks that the flock be released even if the merge crashes. That is
satisfied by the MECHANISM, not by a cleanup path: flock lives on the **open file
description**, and the kernel drops it when the last descriptor copy closes —
which for this run means process death, whether normal, on a panic, or on
SIGKILL. The `defer mergeReleaseLock(lock)` in runMerge is the tidy half (it
releases a fraction of a second earlier and reports a close failure); nothing may
be built on the assumption that it ran.

Consequence worth stating because its absence is a design choice: there is **no
pid file, no state file, no heartbeat, no stale-lock sweeper**. The descriptor
and the kernel's own tables are the only evidence.

## Where the lock is taken, and why exactly there

In `runMerge`, AFTER the pure handling of `-h` / bad flags / an empty repo table
(a usage query must not create the lock file) and BEFORE the first repo is
walked. It is held for the whole rest of the run **including the hand-over child**:
that gopi writes refs too, and a lock that lapsed at the hand-over would guard
exactly the part that needs it least, while the parent is blocked on it.

A refusal is **fatal and prints the no-op consequence** — non-zero, no repo
walked, no ref written, no branch deleted, no build run. "Continue anyway" is not
a degraded mode; it is the defect. The refusal REASON text is acquireServeLock's
own, printed verbatim; only the consequence line is the command's (Rule 2).

Exit status is named (`mergeLockExitStatus = 1`) because it is a contract: a
script firing `gopi merge` from a hook must distinguish "decided everything" (0)
from "somebody else is merging" (1).

## The child-admission marker

After winning the lock the run stamps its environment with the ancestor marker
(`mergerLockOwnerEnv = <holder pid>:merge`). Without it, a `gopi task merge` the
merge gopi itself calls would be REFUSED by a lock its own ancestor holds — a
self-deadlock. The marker is evidence, never authority: the kernel half of
`mergerLockHeldByAncestor` still has to find a live ancestor holding the flock.
It is set on the ACQUIRE arm only — an ADMITTED run already carries its
ancestor's marker, and re-stamping it with a pid that holds nothing would make
the diagnostic lie.

## Tests, and why each layer is needed

- **`merge_lock_test.go`** — the shared fixtures. The holder is a REAL separate
  process (python3 + `fcntl.flock`), not an in-process flock: flock does not
  conflict between two descriptors of the SAME process, and a self-pid holder
  proves the exemption hole rather than the fix (memory #2043's testing pitfall,
  applied here deliberately).
- **`merge_run_test.go`** (in-process, three scenarios replacing
  `TestMergeTakesNoLockAndLeavesNothingRunning`): a free lock is TAKEN (the
  stamped pid is the run's own); a HELD lock REFUSES before deciding anything
  (asserted on the repository, not just the exit code); the run's own release
  admits the next run.
- **`merge_lock_e2e_test.go`** (REAL binary, process boundaries): a second
  `gopi merge` is refused while a live holder runs; **a SIGKILLed holder leaves
  no lock** (the briefing's requirement, measured across processes); `-h` still
  touches no lock file.

The E2E parks a run deterministically with a **declared repo build** that blocks
on a FIFO — a Rule 12 contract a real repo can declare, not a test-only knob — so
the crash is a `kill -9` of a run that has already reached the lock, never a race
against start-up. Every wait is bounded by a named constant; a regression fails
instead of hanging.

`-h` and `merger --status` remain lock-free and read-only: a usage query must not
write to the operator's profile.
