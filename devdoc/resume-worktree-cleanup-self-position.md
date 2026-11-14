<!-- doc-class: historical -->
# Why `worker --resume` left the worktree — and the two defects that kept it

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic `01a0d500`, 2026-09-25. Design notes for the next agent; the reports are
`tasks/01a0d500-resume-worktree-cleanup-report.md` and (the sibling leak)
`devdoc/worktree-orphan-holder-and-run-ground-anchor.md`.

## The shape everyone should know: resume MOVES the process, and that is by design

`gopi resume <row>` (and `gopi worker --resume`) does three things in an order
that looks wrong until you see why:

1. `resumeChdirToWorktree` — **chdir INTO** the row's deliverable worktree
   (the work has to happen there).
2. the round runs, the row is completed;
3. `worktreerelease.ReleaseReporting` — **hand the cwd BACK** to the session dir
   or `/`, and only THEN run finish-cleanup.

Step 3 exists so the directory is removable: a finish-cleanup that finds its own
process standing in the candidate **refuses on purpose** (freeing it would rip
the ground out from under a live run). So the release is the precondition of the
cleanup — and any memo of "where am I standing?" that outlives step 3 turns that
deliberate order into a permanent refusal. That is exactly what
`worktreeSweepSelfOwnedRepo` did: one `sync.Once` per process, with a comment
asserting *"the pass chdir'ing mid-run is not a shape this code has"*. It was,
twice, in this flow.

**The rule that came out of it:** an actor-position memo is keyed on the CWD,
never on the process. A process's identity as an actor is a function of where it
stands *now*; if the process can move, the memo must be re-measured on the move.
`worktreeSweepSelfPosition` returns `{Repo, Common, Guest}` measured together
(three facts from one moment), and both consumers — `guestRowRepoVerdict` (cache
key `(repo, cwd)`) and `guestRowOwnCheckoutRefusal` (reads live) — take it from
there. Cost: one `git rev-parse` pair per **distinct directory** the process
visits, which for nearly every process is the same pair as before.

## The orphan-holder ladder, and the self-match that made rung 5 unpassable

`task_finish_cleanup_holder_orphan.go` releases a directory held by a process
that is provably a leftover of a *finished* run. The rungs: reparented to init,
gopi launcher records present, those records DEAD, and finally "is any live gopi
still in the holder's session?" — that last one is what vetoes a genuine live
run.

The measured defect (pid 221331, a leaked `gopi web` verification server of a
completed run): the census asked "live gopi in session 23345?" and found **the
asker's own worker chain** (the `gopi worker` pass and its agent child shared
that session). The veto could therefore never clear — the process asking the
question was its own counter-example. The fix is the one already applied to the
run-ground anchor (`run_ground_anchor.go:241`): **exclude the asker's own
process tree** (ancestry ∪ descendants) and the holder itself from the census.

Two traps in that fix, both hit and both caught by the new test:

- **Descent must start at the asker ALONE.** Expanding from the whole ancestry
  makes every process the kernel reparents onto init — i.e. most of the machine —
  a "descendant", turning the exclusion into an invented permission.
- **A census that cannot be read is not a release.** An unreadable ancestor or a
  truncated `/proc` listing can only ever yield *fewer* excluded pids, which
  keeps the veto. Fail-safe direction: measure again, never keep an older answer.

Session leadership is deliberately **not** a rung. A real daemon is its own
session leader (`setsid`), and the leftover class this ladder exists for is
*setsid-orphaned* — a leader-veto would re-break the case it was written for.

## `GOPI_TASK_RUN_OUTCOME` is NOT run-finished evidence

Measured: `cli_tasks.go:2217` stamps `GOPI_TASK_RUN_OUTCOME="completed"` on a
launched run **before spawning it** (it is the outcome slot the wrapper fills,
pre-seeded). Do not use it as a release rung; it is true of a run that has not
started. Recorded here so the next agent does not re-derive it.

## Verification recipe (reuse, don't redo)

- **Headline E2E:** `go test ./cmd/agent/ -run TestResumeReclaimsTheWorktreeItFinishedIn -count=1`
  — a real `gopi resume` against a throwaway git world, mocked LLM, and the
  assertion that the worktree is GONE. This is the epic's acceptance in one
  command; it went FAIL 1.895s → ok 2.315s.
- **The stale-identity guard:** `internal/tools/worktree_self_position_test.go`
  re-execs a CHILD process (cwd is process-global — you cannot present "this
  process moved" any other way without corrupting sibling tests), chdirs it
  worktree → main checkout, and reads the position twice.
- **Live proof on the real ledger:** `gopi task finish-cleanup --task <id> --apply`
  prints `removed=N refused=N`; then check the branch still resolves while the
  directory is gone.
- **The suite trap:** `go test ./internal/tools/` **times out at 7m on pristine
  master** of this clone — pre-existing, not this diff. Verify branch-scoped
  (`-run …`) rather than concluding your change broke the package.
