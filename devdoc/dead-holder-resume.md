<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Dead-Holder Resume — recovering an interrupted run whose holder is provably dead

Epic: dead-holder-resume (2026-09-16). Branch: `feat/dead-holder-resume`.
Cross-reference: session `01a0a540` (the identity epic this builds on).

## The problem, in one paragraph

The merger never has to babysit a child — but until this change it also never
**recovered** one. Everything the merger knew about a holder was an annotation:
`RepoSweepPreview.DeadHolder` says "a provably dead holder still occupies this
branch", and the reclaim arm frees the *directory* that holder named. **Nothing
continued the interrupted run.** So a branch protected by a dead holder was
either withheld from merging forever, or — after the reclaim freed its worktree
— merged as though the work were finished. The work that child had in flight
(a half-written fix, a worktree full of commits, a session checkpoint holding the
full context at the last request) was abandoned by default.

## PART 1 was already fixed on master — and that mattered

The briefing claimed the `task run` path never stamps the process-identity
triple. **That premise is false on master**, and writing code against it would
have been a second, redundant identity writer (Rule 14's exact failure).

Evidence, measured before any code was written:

- `git merge-base --is-ancestor d7474fb4 HEAD` → true. Commit **d7474fb4**
  (2026-09-13, *"persist a task's process identity (pid+starttime+cmdline) and
  verify liveness with all three"*) made `taskUpdateStampedFrom` write the triple
  **inside the same UPDATE** as the `claimed → in_progress` transition.
- The live ledger's only `in_progress` row (`01a0a553`) carried
  `proc_pid=952831`, and `/proc/952831/cmdline` was a live `gopi worker`.

Then it was **reproduced end-to-end** on a scratch ledger (`/dev/shm`, Rule 22)
with the real `gopi task run`:

| step | observed |
| --- | --- |
| real `task run` (mock LLM holding the call open) | `status=in_progress`, `claimed_by=peerbox:<pid>`, `proc_pid=<pid>`, `proc_start_ticks=<ticks>`, `proc_cmd="/…/gopi task run t-part1"`, `run_method=manual` |
| `gopi task status <id>` while alive | `verdict: running` — "pid N is alive and matches the recorded identity" |
| after `kill -9 <that pid>` | `status: failed`, the proc columns cleared on the terminal transition |

**Disposition: PART 1 is already fixed; no new writer was needed.** The only
legitimate residual was the *backfill* of pre-d7474fb4 rows (below).

Two rig gotchas worth recording: `task run` deliberately refuses to start when
`GOPI_AGENT_SESSION`/`GOPI_EXECUTOR_SESSION_ID` are inherited from an agent
session (a scratch rig needs the documented one-off `GOPI_ALLOW_AGENT_SPAWN=1`;
`testutil.MockEnv` strips `GOPI_*`, so Go tests are unaffected), and a
backgrounded mock LLM that inherits stdout keeps the bash pipe open — redirect it.

## PART 2 — the feature, as built

`internal/tools/task_holder_resume.go`. Per ledger row that names a worktree:

1. **Liveness is NOT re-implemented.** It delegates to the single existing
   implementation, `finishCleanupProcessHolderLive(procIdentity{})` (over the
   row's own recorded `proc_pid`/`proc_start_ticks`/`proc_cmd`), and to the
   existing `taskCaptureProcIdentity` when a pid must be captured. No second
   identity capture, no second start-tick reader.
2. **Three-way verdict, never a boolean** — `live` / `dead` / `unreadable`.
   Only a POSITIVE, readable answer counts as dead: `/proc/<pid>` absent, a
   zombie, or a start-tick mismatch (pid recycled). An **unreadable probe is
   never dead** — that is Rule 15, and memory #2632 is the fail-open precedent
   where the opposite assumption deleted a live session's worktree.
3. **`/proc/<pid>/cwd` is SECONDARY evidence**, consulted only after the triple
   failed to corroborate life. It resolves the one case the triple cannot: two
   ledger rows name worktree paths that both `LIKE`-match, and the recorded
   identity matches a live process standing in a *different* directory. A
   positive cwd match is proof of life; an unreadable cwd is not proof of death.
4. **The recovery is the existing verb.** `gopi resume <session-id> --task
   <task-id>`, spawned **detached** (`Setsid`). No second recovery mechanism:
   the resume verb already owns loading the session checkpoint, the identity
   gate, and the ownership handoff. The merger does **not** wait — the whole
   point of the epic is that it never waits on, babysits, or kills a child.
5. **Ownership handoff by the PARENT, atomically.** `TaskStampSelfClaim` was
   refactored onto a new `TaskStampHolder(taskID, holder, pid)` that stamps an
   *explicit* pid. The parent stamps the freshly spawned child immediately after
   `Start()`, so the ledger never passes through an "old dead pid" state — a
   window in which the reclaim arm could free the worktree out from under the
   recovery. `TaskStampHolder` keeps the Rule-15 shape: a pid with no readable
   identity records **nothing** (neither a zero pseudo-identity nor a cleared
   triple).
6. **Loop safety.** Each resume appends `dead-holder-resume:<n>`; at
   `deadHolderResumeMaxAttempts` (3) the arm STOPS and **fails the row with a
   named reason** — an unbounded restart loop would burn a model call per tick.
   The resumed run re-stamps the row with live identity, so a healthy recovery
   makes the next pass silent (this is asserted).

## The ordering is a correctness requirement, not style

`mergerPassOnce` runs the resume leg **before** the reclaim leg:

- **reclaim frees the dead holder's worktree**, and a run whose worktree has been
  removed can no longer be continued;
- so recovering after a reclaim is *impossible* — both legs would report
  success while the work was silently abandoned.

This order is asserted by a source-level wiring gate
(`cmd/agent/merger_resume_arm_wiring_test.go`), which also asserts that the leg
is reachable from the pass an operator actually runs. That gate exists because
this area was measured failing the *reachability* way before: the 2026-09-13
reclaim incident happened because the arm lived only in a throttled **worker**
pass while the component operators run is the **merger** — the capability
existed, passed its tests, and was never reached.

## The backfill (residual PART 1 work)

`internal/tools/task_holder_backfill.go` fills the triple for live rows that
name a holder only textually, **only where `/proc` proves it**:

- the claim stamp must parse as a pid **and** be marked as this host's
  (`claimHostIsLocal`) — stamping this host's pid onto a foreign `host:pid` would
  fabricate an identity, and a false identity can make a LIVE holder read as dead;
- `/proc/<pid>/comm` must carry the gopi runner prefix (the same pid-reuse guard
  the rest of the ledger trusts);
- the row must still be in a live-holder status (a terminal row has no live
  holder to describe).

**Measured against the real live ledger** (`~/.gopi/memory.db`):

```
write=false holder-identity backfill: gained=0; left: row status is terminal (no live holder to describe)=716
write=true  holder-identity backfill: gained=0; left: …=716   (no change — as expected)
```

**gained = 0, and that is the honest answer.** Every legacy row that names a
holder is terminal; there is no live holder anywhere in the ledger without a
triple, so nothing can be proven and nothing was written. A one-shot data repair
has no LLM in its path and therefore cannot satisfy Rule 11's adoption standard —
that is why this is a library function (dispatchable from a test, and it was run
against the live ledger) rather than a new CLI verb with no user.

## `gopi resume <task-id>` needs a checkpoint — and that is correct

An early e2e iteration failed with `no → REQUEST checkpoint found`. That was not
a bug in the arm: a run killed during its very first model request has never
completed a round, so there is nothing to restore. The test now scripts one
completed round and then holds, i.e. it reproduces the real case — **a run
interrupted with recoverable work in flight** — and asserts the checkpoint
exists on disk (the `→ REQUEST` marker in the raw log) *before* killing the
holder. When no checkpoint exists at all, `gopi resume` refuses by design; the
arm's job is the case where recovery is possible.

## Continuation, not restart — the evidence

`TestMergerResumesDeadHolderRunE2E` runs a **real** `gopi task run` in a real
linked worktree against a scripted mock LLM, kills the holder's process group,
calls the arm once, and observes:

```
arm outcome: resume-dead-holder: t-dh-e2e (session 78672ac9-…, attempt 1/3)
             — spawned: gopi resume 78672ac9-… --task t-dh-e2e (log: …)
the resumed run holds the row as pid 1668847
continuation evidence: resume: loaded 1 history message(s) [1 system block(s), 41857 bytes in checkpoint request]
```

The last line is the proof: the resumed run **recovered the interrupted
session's context** — it did not start the work over. The negative acceptance
(`TestResumeArmNeverTouchesALiveHolderWorktreeE2E`) asserts the other dangerous
direction: with the holder still alive the arm produces no outcome, stamps
nothing, spends no attempt, and leaves the worktree untouched.

## A note on the reverse lookup through `deliverable` (it does not work)

`ResumeDeadHolderRuns(repo)` matches a row's declared worktree against the
repo's own linked-worktree registry, keyed by the row's `deliverable.branch`.
This is a **forward** lookup only. There is **no reliable reverse lookup**: given
a path, you cannot map it back to a task — the path is a directory name, and a
directory carries no identity. The consequence, stated plainly so it is not
mistaken for a gap to close: a worktree whose row carries no `deliverable`
(older rows, or rows whose deliverable was cleared) simply produces no candidate
here. Those rows are left to the reclaim arm and to the operator, deliberately —
inventing a mapping (a worktree→task index file) would be a second, drifting
source of truth for something git already answers. The epic constraint was
explicit: no worktree→task mapping file as the primary mechanism.

## Files

| file | what |
| --- | --- |
| `internal/tools/task_holder_resume.go` | the arm: verdict ladder, cwd cross-check, detached spawn, cap |
| `internal/tools/task_holder_backfill.go` | prove-only identity backfill + honest counts |
| `internal/tools/tasks.go` | `TaskStampHolder` (explicit pid); `TaskStampSelfClaim` delegates to it |
| `cmd/agent/cli_tasks.go` | `mergerPassOnce` ladder (resume → reclaim); `mergerResumeDeadHolderLeg` |
| `internal/tools/task_holder_resume_test.go` | decision red-proofs, scope, cap, path comparison, renderer |
| `internal/tools/task_holder_resume_e2e_test.go` | real run → kill → resume (positive + negative) |
| `cmd/agent/merger_resume_arm_wiring_test.go` | reachability + ordering gate (with a mutation proof) |

## Verification

See `tasks/dead-holder-resume-report.md` for the transcript: build, full suite,
the red-proofs (each mutation fails its intended test), the wiring gate's
mutation, and the e2e output above.
