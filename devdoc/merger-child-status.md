<!-- doc-class: historical -->
# `merger --status`: reporting the child merging process (epic row 01a0a79b)

## The question that had no answer

`gopi merger --status` answered two questions: *is a merger running?* (a
non-destructive `flock` probe) and *what would the next sweep merge?* (per-repo
protected/pending branch counts). The operator's third question — **the merger
has been alive for an hour, what is it waiting for?** — had no line at all. A
merger blocked in `waitpid` on a conflict-resolving child rendered exactly like
a merger that had just booted.

## What the status now says

```
in flight:   pass started 2026-09-16T13:19:50+08:00 (elapsed 9m0s), repo /repo/wedged, branch feat/conflicted, stage resolve
  children:  1 in flight (the merger is blocked on these)
    - resolve-child feat/conflicted (pid 3901144, started ..., elapsed 8m0s, ALIVE)
```

Three states the block can report, kept deliberately distinct:

| record | render |
|---|---|
| a child that is still running | the line above (`ALIVE`) |
| a child whose process ended (SIGKILLed writer, dead child) | `children: none running …` + an `ended:` list with `process GONE` |
| a record that cannot be read | `in flight: unknown — <the reason>` |

## Design decisions worth knowing

**1. `Children` became a struct, not a rendered sentence.** The old wire shape
was `Children []string` plus a parallel `ChildPIDs []int` — a liveness check had
to re-parse a human sentence for the pid, and the sentence could not carry the
child's *kind* or *start time* at all. `MergerChild{Kind,Label,PID,Group,Started}`
carries all four; `Elapsed()` is derived at read time (a stored duration is only
true at write time); the human line is produced by one renderer
(`MergerChild.String`) so the state note, the stop-wait messages and the
`--status` line cannot drift.

**2. Liveness is measured, and "cannot tell" is not "gone".** A state file
outlives a SIGKILLed writer: the last tick keeps whatever it said, including pids
that ended with the process. The reader therefore measures every pid (signal 0)
— but an entry that names *no* pid counts as live and renders `pid unknown`,
because a missing pid is missing evidence (Rule 15), not an absence. The dead
ones are not deleted either: they are filed under `ended:`, which is how an
operator tells "still resolving" from "died mid-resolve".

**3. A pid-less reservation, taken before `Start()`.** Registration can only
happen after a successful `Start` (an entry for a child that never ran would
make every graceful stop wait for a process that does not exist), but the window
between "we decided to spawn" and "the child exists" is exactly the window an
operator is asking about — a resolve child spends its first seconds in worktree /
marker / model start-up. `registerPendingPassChild` reserves a **negative** key
(pids are positive, so it can never collide with a real child) carrying kind and
label but no pid; `stampChild(pid, group)` replaces it in one transition. A
spawn that fails drops it via `unregister`; both halves are idempotent.

**4. The pass is published even with no children.** The other way a merge
pipeline stalls is a pass wedged in its own git/build work, so the block names
the pass (start + elapsed) with an empty child list rather than vanishing.
`mergerSetPassStarted` records and publishes the instant, and clearing it clears
the block — so a finished pass cannot keep rendering as in-flight work.

**5. The block is rendered on evidence, not on a word.** The "unknown" note is
printed only when there is a reason to ask (a lock holder, or a state file for a
LIVE pid — `MergerStateWriterLive`); an in-flight block backed by *named*
children prints whenever it exists, including under a dead merger (a live child
left behind is the finding). A line that always appears is a line an operator
learns to ignore.

**6. Read-only, still.** `MergerStatusReport(lockPath, stateDir)` takes the
control directory from the caller; the CLI resolves it with
`MergerGracefulDirReadOnly()` (never `mkdir`). `TestMergerChildStatus_ReadOnlyAgain`
runs the real binary against a fresh profile and asserts no directory was
created — creating the thing you are reporting on would also make every later
query claim a merger had published state there.

## Reusable seams introduced

- `tools.MergerStateFiles(dir)` / `MergerNewestStateFile(dir)` — the unfiltered
  records (their writer may be gone: the post-mortem view), vs
  `MergerLiveStateFiles` (live writers only: the stop/restart view).
- `tools.MergerChild.String()` / `MergerInterrupt.PassLine()` — one spelling of
  each half of the in-flight fact.
- `tools.StatusAliveWord`, `statusChildGone`, `statusChildPidUnknown` — the
  liveness words as constants (Rule 1b: tests and operators read the same
  spelling).

## Deferred

- The **rebuild child** is not reported: the post-merge rebuild runs
  synchronously inside `runBuild` and has no registry entry, so there is no
  process to name. Naming it needs a registration at that call site.
- No end-to-end run against a merger *actually blocked* on a conflict resolve
  (needs a real conflict plus a live daemon on a shared repo). The real command
  was exercised against a planted record with a live child
  (`.evid/demo-merger-child-status.sh`); the producer half is covered by the
  registry unit tests.
