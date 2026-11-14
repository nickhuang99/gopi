<!-- doc-class: normative -->
# The memory-sync child's working directory — a producer obligation (Rule 34's `cmd.Dir` shape)

> **PARTLY REVERSED 2026-11-06 — the command this page names is BACK, minus one flag.**
> `gopi memory sync` was restored the day after its deletion, as a MANUAL door:
> one invocation = one session, exit. `--peer` / `--push` / `--pull` / `--dry-run` /
> `--serve-assets` / `--timeout` all run again; the runnable shape and its measured
> two-way session are in `devdoc/memory-sync-manual-door-restored.md`. **What is
> still gone is the `--every <dur>` cadence flag**, whose reader and loop went in
> batch 5 — so a line below that carries `--every` is a RECORD, not a command to
> paste (it exits 1 with `unknown flag`). The one-way copy `gopi memory backup`
> is the other manual cross-host door and its guarantees differ: it REPLACES the
> peer ledger instead of merging it.

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Row `01a16272-9ce6-7c54-904f-ef3301a6cf58`. Source: `cmd/agent/memory_sync_tick.go`.

## The defect, as measured

A gopi run's own `gopi memory sync` child inherited the run's working directory
(its task ground), so the ground's own machine vetoed the ground's own reclaim:

```
$ gopi task status 01a161f3-67e5-7889-adec-8f8b01509299-r1
status:      in_progress
ground_now:  IN USE by live process pid 3333196 (gopi) argv="…/bin/gopi memory sync --peer peerbox --push …" ppid=3294965 — cwd /home/user/workspace/01a161f3-…-r1
ground_why:  worktree in use by live process pid 3333196 … cwd /home/user/workspace/01a161f3-…-r1
```

Measured 2026-10-22T15:09:48+08:00 on hosta: the launcher was worker pid
3294965 (`gopi`), the child pid 3333196 (`gopi memory sync --peer peerbox --push`),
and **three** processes stood in that ground (worker, sync child, and the sync
child's own `ssh` leg). The clone channel had already deleted the workspace
(`devworkspace.Reclaim` checks no holders at all), so the two arms gave opposite
answers about one directory.

`exec.Command` with no `Dir` hands the child the CALLER's cwd. The resident
memory-sync driver runs inside a `gopi worker` whose cwd is the ground it was
started in (the worker never chdir's away), so the child pins the ground for its
whole lifetime if its launcher dies first.

## The fix, and why it is shaped this way

Two measurements decided the shape, and neither is a preference:

1. **the generic daemon resolver is wrong for THIS child on THIS host.** On
   2026-10-22 `daemoncwd.Resolve(<ground>, "")` answered `/proc`: the scratch
   home (`/home/user/workspace/gopi-test-scratch`) lived under the default
   worktree root (`/home/user/workspace`) until that day, the package relies on
   `worktreeroots.DefaultRoot()` as its declared root, and it therefore refused
   even `<scratch>/runtime/process`. `/proc` is a fine cwd for a daemon that
   opens no relative path; it is not a place to run a sync verb. So the
   memory-sync child gets the **scratch tree itself**, measured off `/` by
   device id (66305 vs 66308 on this host) — its own root, decided by its own
   needs.
2. **an unconditional move would be theatre.** A worker whose cwd is `~` or `/`
   vetoes nothing. The escape therefore happens **only when a live process
   stands in a ground** — the very condition guard ③ later reads.

```
memorySyncTickChildCwd()        (dir, inherited, why)
  ├─ inherited == ""  → keep, why=no-readable-cwd
  ├─ memorySyncTickInheritedGround(inherited) == ""
  │      → keep, why="no live process stands in a task ground there"     [the common case]
  └─ escape needed → memorySyncTickProcessRoot(inherited)
         ├─ error → keep inherited + report the measured refusal          [Rule 22/15]
         └─ ok    → child starts there; the move is reported
```

`memorySyncTickSpawnChild(argv, childDir)` applies it (`cmd.Dir = childDir`),
and the ONE call site (`memorySyncTickRun`) decides it once per fire, not once
per peer.

### The guard is untouched, deliberately

`memorySyncTickInheritedGround` asks `tools.TaskGroundInUse` — the SAME
predicate guard ③ (`finishCleanupWorktreeInUse`, via `TaskGroundInUse`) uses —
of the inherited directory and of `GOPI_RUN_GROUND`. So "an escape was needed"
and "the row would read IN USE" cannot disagree (Rules 13/14: one reading).
`reclaimSkipDirtyReason` and `finishCleanupWorktreeInUse` are not modified by
this change; the fix is entirely producer-side.

`memorySyncTickProcessRoot` only ever REFUSES: a candidate that is inside the
ground it must escape, or inside a declared worktree root, or on the same
filesystem as `/`, is never handed to the child — the inherited directory and a
named refusal beat a path nobody measured (Rule 15).

## Not adopted, and why (so nobody re-litigates it)

- **A git-repository-boundary invariant for the escape root.** Considered and
  dropped: `grep -rn '"git"' internal/tools/memory_sync*.go` finds no git
  spawn in the sync legs, so "which repository does a git leg answer about" is
  not a property of this child. Adding the check made the escape refuse on a
  host whose scratch home is in a different repository from the ground — i.e.
  it would have re-introduced exactly the veto it removes.
- **A `chdir` in the worker instead of a `cmd.Dir` on the child.** It moves the
  worker's own ground detection (branch backfill, project context) out from
  under it — the trade the daemon-context-isolation epic already rejected.
- **Weakening the holder scan** (`finishCleanupWorktreeInUse`) or adding an
  exemption for processes named `gopi memory sync`. The scan is right about what
  it measures; what was wrong is that this child was ever spawned into a ground.

## Verification recipe (reusable)

```
# 1. the decision, from inside a REAL linked worktree
go test ./cmd/agent/ -run TestMemorySyncTickSpawnsChildOutsideGround -count=1 -v
#    the helper prints DECISION / MEASURED / SEAM lines
# 2. red proof: revert the cwd assignment (or hand the seam "") → the same test
#    fails with "the driver handed the seam an EMPTY cwd … pre-fix behaviour"
# 3. the ground really reads free afterwards:
cd <ground> && gopi task status <that row> > /dev/shm/out.txt   # NOT through a pipe
grep ground_now /dev/shm/out.txt      # expect: no live process stands in it
```

**The pipe mattered — and the guard now knows it** (update, 2026-10-27, row
`01a17d23`). Until then, a piped read (`gopi task status <row> | grep <pattern>`)
reported the pipeline's own `grep`/shell as the holder, because the scan is the
asker's own cwd too (filed as row `01a16343-2b5d-7d11-8662-e6a03d85f4be`; evidence under
that run's session dir); the workaround was to redirect to a file first and read
it. The guard's criterion now excludes the caller's own side of the pipeline
(`finishCleanupAskFamily`, `internal/tools/task_finish_cleanup.go`): same process
group, plus either the opposite end of a pipe the asker holds or a shared
ancestor with a start tick at or after the asker's. Measured after that change,
from inside a ground: `cd <ground> && gopi task status --worktree . | grep
ground_now` → `ground_now:  no live process stands in it`, while a third party
`sleep` standing in the same ground still reads `IN USE`. The redirect form still
works and is still the more robust one to teach (it measures the guard, not the
reader's own plumbing).

## Files

- `cmd/agent/memory_sync_tick.go` — the cwd decision, its report lines and the
  named reasons (Rule 1: every literal is a constant; Rule 15: every keep is
  reported with its reason).
- `cmd/agent/memory_sync_tick_cwd_test.go` — the /proc-level regression.
- `cmd/agent/memory_sync_tick_test.go` — the spawn-seam stub takes the cwd.
