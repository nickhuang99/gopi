<!-- doc-class: historical -->
# The release that vetoed itself — an own-worktree release kept forever by its own run tree

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: *worktree not released* (task `01a0d554-8d63-7cde-8685-3409345eb4bc`), 2026-09-25.

## The symptom

A finished `gopi worker` round kept its own ground, pass after pass, with:

```
worker: own-worktree-release: own registered worktree kept: /home/user/workspace/gopi-01a0d500… — held by a live process (cwd/fd/exe/maps)
```

Two of the four registrations in the report were refused this way (the other two
were merely younger than the 5m age floor). Nothing in the message named a
process, so the operator had no way to check the claim.

## The hypothesis in the briefing was wrong — and measuring it took one command

The briefing proposed that the worker `chdir`s into the worktree it spawns and
then cannot delete its own cwd. Measured, it does not:

* `gopi worker <id>` runs with **cwd = ~/.gopi/sessions/<task>**
  (`taskWorkdirEnter` / `taskWorkdirRestore`);
* the executor **child** is spawned with `cmd.Dir = <worktree>`
  (`cmd/agent/cli_tasks.go:2173`); the worker never follows it.

`ps -o pid,ppid,sid` on a live round confirmed the picture: worker 492455 → its
executor child 492481, both in session 17262, the child's cwd inside the ground.
**The "live process" the release kept naming was the asker's own child.**

## Root cause: a self-veto, in two layers

**Layer 1 — one arm never asked the question its sibling asks.**
`ReclaimSelfOwnRegisteredWorktrees` (the producer-side pass, whose whole job is
freeing what nothing owns) vetoed on the raw holder scan
(`internal/tools/task_reclaim_self.go`, rung ②) and stopped. Its ledger-keyed
sibling one file over (`task_reclaim.go`, guard ③) consults
`finishCleanupOrphanCwdInheritorReason` on refusal — "is EVERY live cwd-holder
here an orphaned cwd-inheritor nobody owns?" — so the same directory was
releasable by one arm and permanently pinned by the other.

**Layer 2 — the ladder's last rung counted the asker's own run.**
`finishCleanupClassifyOrphanHolder` rung 5 asks
`finishCleanupSessionHasLiveGopi(sid)`: "does a live gopi process still stand in
this holder's session?" The release pass runs in the session it SHARES with its
own executor child, so the answer was always yes, for its own child. The same
self-match had already been fixed once for the sweep
(`finishCleanupSessionHasLiveGopiExcept(sid, os.Getpid())`, 2026-09-23 — "a
questioner that answers itself is not evidence"); an earlier unmerged branch
(`0d500…`) had gone further with `…ExceptTree`.

**The live specimen that proves the two layers are the whole story:**
pid 221331, a leaked `gopi web`, `ppid=1`, `cwd=<worktree> (deleted)`, its
recorded launcher `GOPI_TASK_RUN_WRAPPER_PID=217683` **dead**, and its session
23345 holding nothing but itself. Measured on the fixed tree:

```
DIAG ladder                 pidMsg="" why="" (release=false)   ← still refused
DIAG bareCensus(sid 23345)  = true                             ← counted itself
DIAG exceptSubject(23345,221331) = false                       ← with the exclusion
```

Rung 5 was answering "a live gopi is in this session" **with the holder itself
as the witness**, so the release rung could never fire for a gopi-commanded
leftover. (That specimen is a `gopi web` whose cwd is a *deleted* directory,
so `HeldByProcesses` does not even see it — the two-layer reading holds for the
kept-with-"held by a live process" class, which is what the worker reported.)

## The fix (three files, no new subsystem)

1. `internal/tools/task_finish_cleanup.go` — **new** `finishCleanupSelfTree()`:
   `finishCleanupSelfChain` widened downward. Ancestors are EXCLUDED, the asker
   and its own transitive descendants are ATTRIBUTED to it. PID 1 and the
   measured init are never members.
2. `internal/tools/task_finish_cleanup_holder_orphan.go` — the session census is
   now one implementation, `finishCleanupSessionHasLiveGopiExcluding(sid, exclude)`;
   the old `Except(sid, except)` stays a thin wrapper. Rung 5 calls
   `finishCleanupSessionHasLiveGopiExceptSubject(sid, holderPid)` — the holder
   itself and the asker's tree are not evidence.
3. `internal/tools/task_reclaim_self.go` — rung ② falls through to
   `finishCleanupOrphanCwdInheritorReason` on a raw-holder refusal, exactly like
   its ledger-keyed sibling.

## The over-exclusion I made first, and why it is a guard now

The first version expanded the closure from **every pid already in the set**,
ancestors included. Every process's ancestry ends at init, so the seed contained
pid 1 and the closure reached **the whole host**: 65 pids on this box, unrelated
`gopi worker` rounds and a sandboxed `gopi ui` among them.

That is not a narrower veto — it is **no veto**: the set feeds the census that
decides "is a live run still in there", so excluding the host makes that rung
answer "nothing is live" for registrations a real third party is standing in,
and the release would tear down a bystander's ground. The corrected shape
excludes the ancestors (a launcher chain is not a third party) but **attributes
nothing to them**; only the asker's own descendants are its business.

Guard: `TestFinishCleanupSelfTreeSparesThirdParties` asserts the property per
pid (every member is the asker, an ancestor of it, or a descendant of it) AND
against a real stand-in third party (a process reparented to init in its own
session), and it was proven RED by re-introducing the old expansion.

## Verification

* Unit: `internal/tools` — `TestReclaimSelfOwnReleasesOrphanHeldRegistration`
  (RED before, freeing an orphan-pinned registration), the positive control
  `TestReclaimSelfOwnKeepsLiveChildHeldRegistration`, and the over-exclusion
  guard. All three PASS.
* Live (Rule 31): the real binary's `gopi worker <id>` round-end release driven
  against a fixture repo whose one registration is pinned by a leaked orphaned
  cwd-inheritor (`setsid`, `env -i`, dead `GOPI_TASK_RUN_WRAPPER_PID`):
  **before** → `own registered worktree kept … held by a live process`, directory
  present; **after** → `own registered worktree freed`, directory GONE, branch
  kept. The positive control (the same fixture with a LIVE recorded launcher)
  still reports `kept … held by a live process`.
* One thing the fix deliberately does NOT do: it never frees a directory a live
  process stands in. An orphan that is still sitting in a deleted directory keeps
  sitting there — its cwd is a stale inode, and the release is about the
  registration, not about reaping a stray process. Killing leaked children is a
  different defect with a different owner.
