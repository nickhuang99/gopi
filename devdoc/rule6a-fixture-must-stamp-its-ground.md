<!-- doc-class: historical -->
# A Rule-6a fixture's ground must be the ROW's ground

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Scope.** `cmd/agent/cwd_release_composition_test.go` (epic
cwd-release-composition) went RED the moment it was run from a linked worktree —
the ground Rule 6a *requires* a run to develop in. The finding generalizes, so
this note is the reusable half.

## What was measured (2026-09-26)

The fixture drives the REAL binary as a `gopi worker` whose cwd is the fixture's
own linked worktree, because the composition it pins needs the round's cwd to be
INSIDE the directory the round removes (the profile root `GOPI_HOME` is placed
inside that worktree deliberately). Since epic 01a0c2b5 (2026-09-21), that actor
position IS a guest of the repository:

```
own worktree release refused: task/… (t-…) — guest-row-reconcile refusal
(arm guestRowOwnReleaseRefusal): this process stands in the linked worktree
…/001/wt, not in the main checkout …/001/repo of that repository … → wt STILL PRESENT
```

The round's removal and its cwd release are BOTH refused, so the worktree
survives, the process is never pinned, and the fixture observes the ABSENCE of
its defect (readings: `wt → wt/.gopi/sessions/<id> → /`; `pinned=0`).

## Two facts that make the obvious fix wrong

1. **Reverting the launch directory alone does not help.** With the worker
   launched from the fixture's repo (resident, so not a guest), the fixture's
   worktree is still kept, now by the age floor:
   `own registered worktree kept: …/001/wt — younger than the 5m0s floor (3s)`.
   A fixture's hand-made worktree is seconds old by construction
   (`reclaimSelfOwnMinAge`, and the same family as memory #5683/#5684).
2. **The worker creates its own ground** (epic worker-creates-child-ground):
   `gopi worker: child ground → …/004/gopi-<stem>`. That directory — not the
   fixture's — becomes the row's recorded ground at claim time, and it is freed
   by finish-cleanup independently of the fixture. Assertions aimed at "the
   worktree" must say WHICH one they mean.

## The shape that works (test-side only, no guard touched)

The guards already carry the exemption the fixture needs. The producer close-out
publishes a mark — `internal/tools/reclaim_own_ground.go`
(`SelfGroundReleaseArm*`) — holding exactly the grounds the pass is closing out,
and every rung that would refuse a process standing in its own closing ground
skips on MEMBERSHIP in that set: the guest receiver arm
(`guestRowOwnRefusal`), the own-ground rung (`guestRowOwnCheckoutRefusal`) and
the age floor all read it. The guest arm's skip is keyed on the **row's recorded
ground** (`r.Worktree` in `reclaimSelfRelease`) — hence:

- **stamp the fixture's directory as the ROW's ground** through the production
  backfill seam `tools.TaskDeliverableWorktreeBashForRow` (the call the
  executor's bash round makes), with a read-back through
  `tools.TaskDeliverableWorktreeFor`; the seam writes through the process ledger,
  so pin the ledger env with `t.Setenv` for the duration of the call;
- **keep the launch shape** (the worker still starts inside the worktree) — that
  premise is the whole point of the fixture, and the mark is what makes it
  legitimate rather than refused.

Measured result of that shape (the same fixture, same binary):

```
readings=…/001/wt → …/wt/.gopi/sessions/t-… → …/sessions/t-… (deleted) → /
session-inside=true; pinned=174; released=true; exited=false
finish-cleanup: … worktree-freed=…/001/wt        (no refusal line anywhere)
gopi worker: child ground → …/001/wt (task …: reused the recorded ground …/001/wt)
```

So the composition runs end to end — including the shape the fixture exists for:
the release is the ONLY thing that unpins a process standing in a directory that
no longer exists.

## Why this is worth remembering

`devdoc/rule-6a-worktree-verification.md` states the rule this task is an
instance of: **a rule must be verified inside the environment where the rule
holds — a symmetric environment, not a mirrored one.** Rule 6a's environment is
a linked worktree. A fixture built on the assumption that it can spawn its
subject from wherever the suite happens to run will be RED exactly there, and it
will be RED for a reason unrelated to what it claims to test. Two habits:

- **spawn the subject from where the claim is testable, and bind the claim to a
  LEDGER/engine fact, never to the suite's own cwd** (the production seam is the
  binding);
- when a test is red only under one launch shape, **check which GUARD is
  speaking before changing the test's world** — the refusal line names its arm
  and the evidence that decided it, which is what turns "the test is red" into
  "the test's premise contradicts a guard".

Not covered here (dispatchable): a gate that refuses a Rule-6a-shaped fixture
whose subject is spawned with a foreign cwd, and whether the fixture should
exercise the `guestRowOwnCheckoutRefusal` arm explicitly rather than relying on
the close-out mark's membership to bypass it.
