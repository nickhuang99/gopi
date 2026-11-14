<!-- doc-class: normative -->
# The WIP snapshot leg — an interrupted run's uncommitted work survives it

## The defect

A run that is interrupted — a signal, an OOM kill, a power cut — leaves its
uncommitted work in exactly one place: the run's own worktree. The moment the row
is written `failed` that directory becomes **reclaimable**: the worktree sweep is
entitled to remove it.

Measured incident (2026-10-26, epic row `01a17a8b`): 1097 uncommitted lines
survived only because a human happened to be awake at that second and copied them
out by hand into that session's own directory (`wip-snapshot/` plus
`tracked-0703.patch`). Nothing in the ledger could do it, and nothing told the
relayer where the salvaged work was — the successor `${id}-r1` started on a fresh
ground with a clean tree and no knowledge that 1097 lines existed.

## The shape

Three legs over one primitive:

| leg | seam | when |
|---|---|---|
| ① primitive | `SnapshotUncommittedWork(taskID, trigger)` (`internal/tools/task_wip_snapshot.go`) | on demand |
| ② `failed` writeback | `taskUpdateStampedCore` (`internal/tools/tasks.go`), before every refusal and before the UPDATE | the last chance |
| ③ periodic | `SnapshotLiveRunsPeriodic()`, called from `taskLifecyclePassOnce` (`cmd/agent/task_lifecycle_tick.go`) | while the run is alive |
| ④ pointer | `taskSuccessorWIPAxis` (`internal/tools/task_successor_wip_pointer.go`), from `TaskCloneWithOptions` | when a successor is minted |

The deposit lives at `~/.gopi/sessions/<task-id>/wip/`:

```
status.porcelain             the raw `git status --porcelain` body measured
tracked.patch                `git diff HEAD --binary` — every tracked change
untracked/<relative path>    one copy per untracked file (symlinks as links)
untracked.list               exactly which paths were copied
HANDOFF-FIRST-READ-ME.md     machine-generated: what was measured, what could
                             NOT be carried, and the pasteable restore recipe
```

## The three boundaries (each is a rule, not a preference)

**Rule 30 — the leg only READS the worktree.** Nothing in this file commits,
stashes, cleans, restores or deletes a byte of the run's tree. Every git verb it
runs is a read (`status`, `diff`); every file it writes goes under the session
dir. Measured: the ground's `git status --porcelain` body is byte-identical
before and after a snapshot (`TestWIPSnapshotDirtyGroundIsRestorable`).

**Rule 37 — the deposit is in the run's session directory**, which is never a
git-tracked path. The ground is therefore empty of this file's output *by
construction*, not by cleanup.

**Rule 35 — no new resident anything.** Leg ③ is a function called from the tick
both resident components already fire. There is no process, no timer, no script,
and no state file: the per-row throttle is an in-process map, because a persisted
one would be a new state artifact whose only job is saving a few bytes of copy.

## Zero write when there is nothing to save

The clean path returns **before** `os.MkdirAll`, so a tidy run leaves no `wip/`
directory at all — not an empty one. Asserting "the directory is absent" rather
than "it is empty" is the point: an empty directory is still a write, and it would
put a trace in every clean run's profile.

Measured on the real binary: a clean ground + `gopi task reconcile` printed
`wip snapshot (periodic): … holds no uncommitted work — nothing written`, and
`ls -a ~/.gopi/sessions/<id>` printed only `.` and `..`.

## The measured pitfall: a patch's bytes must never pass through a parser

The first draft captured `git diff HEAD --binary` through the package's
parsed-read helper (`finishCleanupGitParsedStdout`) and re-wrote the returned
string. The test caught it immediately:

```
git apply --binary …/wip/tracked.patch
error: corrupt patch at line 7
```

Root cause: that helper `strings.TrimSpace`s its result, and a patch's last
context line is terminated **by** its newline. A value whose bytes *are* the
deliverable cannot go through any reader that parses, trims or re-encodes it.

The fix is `taskWIPGitToFile`: `exec.CommandContext` with `cmd.Stdout` set to an
already-opened `*os.File`, stderr captured separately for the error sentence, and
`f.Close()`'s error reported when the command itself succeeded (a patch that never
reached the disk is the failure a caller most needs to hear about). No parsing, no
trimming, no `CombinedOutput` — which would also merge git's diagnostics into the
bytes.

The same trap applies to every artifact of this kind in this repo: a bundle, a
diff, a binary, a signature.

## The pointer, and what it deliberately does not do

`taskReopenWIPAxis` reads the **disk**, not the ledger: the deposit's existence
is the fact, and a ledger event claiming a snapshot exists while the directory has
since been removed must not put a dead path in a successor's briefing. A directory
without `HANDOFF-FIRST-READ-ME.md` is a partial write from a crash mid-copy, and
yields no pointer.

Found → the path, the patch name and the restore commands are appended to the
successor's **payload** (the payload *is* the briefing — the only text the relayer
reads), and the reissue line carries `wip=<path>`. Absent → the line carries
`carries no WIP snapshot (…)`: "no snapshot" and "the mechanism never ran" are
different facts, and Rule 15 forbids letting a reader confuse them.

It does **not** copy the deposit into the successor's directory (a second copy is
a second thing to lose track of) and it does **not** restore it automatically (an
automatic `git apply` onto a fresh ground is exactly the tree mutation Rule 30
forbids this epic from making). Restoring is the relayer's deliberate act, and the
hand-over note gives it the commands.

## What a snapshot is NOT

- Not a backup of committed work — git already holds that.
- Not a replacement for the run committing its own work, and not a reason to
  relax Rule 30's dirty close-out gate. A snapshot makes an interruption
  survivable; it never makes an uncommitted delivery acceptable.
- Not a guarantee: the work is only safe once it is on a branch, and the note
  says so in its own words.

## Bounds, and why every omission is named

`taskWIPMaxFileBytes` 8 MiB per file, `taskWIPMaxTotalBytes` 64 MiB per snapshot,
`taskWIPMaxUntrackedEntries` 5000. A path over a bound, a directory, a vanished
file, a symlink that could not be read — each is **named** in
`HANDOFF-FIRST-READ-ME.md` under `## NOT carried by this deposit`. A deposit that
silently omitted the one file that mattered would be worse than no deposit:
it would look complete.

## Checks

```bash
go test ./internal/tools/ -run 'TestWIP|TestFailedWritebackSnapshotsFirst|TestReopenBriefingNamesTheSnapshotPath'
```

- `TestWIPSnapshotDirtyGroundIsRestorable` — a real `git init` repo, a real
  `git clone` ground (the clone channel's shape, Rule 6c): the tracked half
  applies to a clean clone and reproduces the content, the untracked half is
  byte-identical, and the ground's porcelain is unchanged by the snapshot.
- `TestWIPSnapshotCleanGroundWritesNothing` — asserts the `wip/` directory is
  **absent**, not empty.
- `TestFailedWritebackSnapshotsFirst` — the `failed` writeback itself produces the
  deposit and the row's trail names it.
- `TestReopenBriefingNamesTheSnapshotPath` — both directions: the path is in the
  successor's payload; the absence is stated on the line.
- `TestPeriodicSnapshotLegIsCappedAndReported` — the leg files the CLEAN event on
  a tidy tree and reports a preserved one on a dirty tree.

Real-binary evidence, the restore transcript and the reopen transcript are in the
run's own `task-report.md`.
