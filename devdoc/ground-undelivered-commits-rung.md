<!-- doc-class: normative -->

# The undelivered-commits rung: a ground is not residue while it is the only home of its work

**Status**: implemented on `feat/undelivered-commits-rung`
**Files**: `internal/tools/worktree_sweep_undelivered.go` (the rung),
`internal/tools/worktree_sweep_undelivered_test.go` (its proofs),
`internal/tools/run_ground_anchor.go` (its call site)

## What was missing

Every arm that frees a run's ground answered three questions — how old is it, is
its tree clean, does a live process stand in it — and never asked the one that
decides whether the act destroys WORK:

> do the commits this checkout holds exist anywhere besides this directory?

The name that decision needed is **the delivery point**: where a branch's work is
allowed to live besides the ground being removed. There are two, and the rung
asks git about both:

| delivery point | whose contract |
|---|---|
| `refs/remotes/<remote>/<branch>` | the clone channel's push (`gitPushcheck`) |
| the integration branch, local and remote-tracking | the merger's landing |

`refs/heads/<branch>` is deliberately **not** on that list: it always resolves
(the ground stands on it), so counting it would make every ground "delivered" by
construction. The measurement has to name a place the work exists OUTSIDE the
thing being removed.

## Which ground shapes can lose work — and the two corrections measurement forced

A first draft narrowed this to clone grounds and was **right about the mechanism
and wrong about the symptom**. A second draft covered only the "safest" reading
and would have left the operator's own complaint unfixed. Both corrections came
from running the thing, and both are recorded here so the next reader does not
have to rediscover them:

**Measured 2026-10-22 on this host:**

1. `git worktree remove` **refuses** a clone ground —
   `fatal: '…/fix/ground' is not a working tree`, exit 128. So the sweep's
   anchor axis cannot be the remover of a clone ground; `devworkspace.Reclaim`
   (a bare `os.RemoveAll`, `internal/devworkspace/devworkspace.go`) is.
2. `git rev-parse --verify refs/heads/task/undeliv-demo` exits **1 in the main
   repo** and **0 in the clone ground carrying it**. A clone ground's branch is
   in its OWN refs and was never fetched back — so asking `repo` answers a
   silent zero about the very grounds the rung protects.
3. Every ground standing in `/home/user/workspace` on that date was a clone
   whose HEAD was **already in master** (`ahead=0`). The loss had not happened
   there yet, which is why this rung's evidence is the mechanism plus the
   sandbox fixture — not a corpse.
4. The removal-audit sink records **735** distinct `/home/user/workspace/*`
   targets already removed: **472** `gopi-<stem>` LINKED grounds removed by
   `task-self-reclaim` / `task-self-own-sweep`. That is the shape whose delivery
   path dies with the directory.

**So both shapes are judged, and only the place the refs are read differs:**

- **CLONE GROUND** (`.git` is a DIRECTORY): the directory *is* a repository;
  `os.RemoveAll` takes objects and any ref that existed only there. Refs are read
  **in the ground**.
- **LINKED WORKTREE** (`.git` is a FILE): the branch survives in the main repo —
  and it is still refused, because the row's recorded deliverable **is the
  directory**, the delivery is a single shot taken FROM inside it, and nothing
  re-runs it. That state is the operator's own sentence,
  `[closeout-refused]: the recorded worktree directory is not there`
  (`task_finish_cleanup.go`, its ④ comment; measured on HP 2026-09-26 as
  `settled=7 refused=3`, every one that sentence). A surviving ref that nothing
  will ever push is not delivery.

## The one case this rung hands off: unrelated history

A branch that shares **no common ancestor** with the integration branch makes
`git rev-list --count` report every commit it has. That arithmetic is right and
the OWNER is wrong: `reclaim_ground_checklist.go`'s rung ② already refuses that
shape with its own named sentence, and
`TestRunGroundSweepKeepsAGroundWhoseBranchSharesNoAncestor` pins that refusal at
the sweeper's APPLY step. Answering here would preempt it at the PLAN step and
turn a pinned contract into an unpinned one — which is exactly what happened on
the first wiring of this file (measured: that test went red, `collectable`
carried the ground because the plan step had already refused it for the wrong
reason).

So the rung resolves the integration branch first, and only when it resolves does
it ask `git merge-base <integration> <branch>`; no common ancestor → this rung
says nothing and the lineage check speaks. **Resolution first, verdict second**:
a missing integration branch is not a lineage verdict, it is a missing ref, and
the rung must keep answering (unmeasurable → keep) rather than fall silent on a
repository it cannot judge.

## The predicate

```go
// one function, both removal axes
func worktreeSweepUndeliveredRefusal(ground, repo, branch string) string
```

- reads refs from the ground for a clone, from `repo` for a linked worktree;
- counts `git rev-list --count <branch> --not <every delivery point that
  resolves>`;
- **`> 0` → keep**, with a sentence naming the count, the branch and every ref
  counted against, plus the two actions that release it (push, or merge);
- **unmeasurable → keep**, in its own sentence: "I could not tell whether this is
  the only home of that work" is not a licence to delete it (Rule 15a);
- an **absent branch** is an ANSWER (`measured, ahead=0`), not a failure —
  otherwise every cleaned-up registration would park behind a phantom keep.

Placement in `runGroundAnchorLiveness` is load-bearing: the rung sits **after**
both the launch ladder and the ledger rung, at the single exit that produces
`collectable`. A future rung added in front of it cannot accidentally bypass the
collection decision.

## What the live run shows (the `Ran:` evidence)

Sandbox fixture: a clone ground `fix/ground` off `fix/main`, one commit on
`task/undeliv-demo`, an anchor record aged 72h whose holder pid is dead.

```
$ gopi task sweep-worktrees --dry-run
run-ground anchors: collectable=0 kept=1
  keep  [run-ground-anchor] …/fix/ground  kept: the ground's branch task/undeliv-demo
        holds 1 commit that no delivery point has (refs/remotes/origin/master,
        refs/heads/master) — this directory is that work's only home: push the
        branch or merge it into master, then the ground is collectable

$ git -C fix/ground push -q origin task/undeliv-demo
$ gopi task sweep-worktrees --dry-run
run-ground anchors: collectable=1 kept=0
  collectable [run-ground-anchor] …/fix/ground  the run that took this ground is
        gone (pid 1 wears a different incarnation) …
```

Same ground, one push apart: that is the rung measuring DELIVERY rather than
"a branch exists".

## Two defects the FIRST REAL RUN of the rung exposed (2026-10-23)

Both were found by running `gopi task sweep-worktrees --dry-run` against this
host's live anchor sink — not by reading the code, and not by the suite, which
was green through both of them.

### ① The count was clamped to one

The probe's argv carried a stray `-1` (git's "limit the number of commits
shown") beside `--count`, so git answered with the LIMIT rather than the count.
Measured in `/home/user/work/gopi`:

```
git rev-list --count feat/suite-closeout-sweep-gate --not refs/remotes/origin/master refs/heads/master -1  → 1
git rev-list --count feat/suite-closeout-sweep-gate --not refs/remotes/origin/master refs/heads/master     → 9
```

The rung printed "holds **1 commit** that no delivery point has" for a branch
holding nine. It survived a green suite because every existing case commits
EXACTLY ONE commit, where a clamped count and a correct one are
indistinguishable — so the regression case
`TestUndeliveredCountsEveryCommitNotJustTheFirst` builds three and asserts the
number exactly, in the struct and in the operator-facing sentence, then delivers
all three and checks the rung flips to collectable.

### ② A directory that no longer exists was called the work's "only home"

The same run printed:

```
keep [run-ground-anchor] /home/user/workspace/gopi-closeout-sweep  kept: the ground's
branch feat/suite-closeout-sweep-gate holds 9 commits that no delivery point has (…) —
this directory is that work's only home: push the branch or merge it into master, then
the ground is collectable
```

while that directory had not existed for a day. The removal sink records it:

```
{"ts":"2026-10-22T21:13:48.776365076Z","kind":"worktree-removal","arm":"task-self-own-sweep",
 "target":"/home/user/workspace/gopi-closeout-sweep","branch":"feat/suite-closeout-sweep-gate"}
```

Two defects in one sentence, and the second is the expensive one:

- the sentence is **false** — it describes a directory that is not there;
- the keep is **permanent**. Every other rung on this axis relaxes: the age
  floor elapses, the holder scan finds no live process. A commit count is a
  fixed fact about a ref, so a registration whose directory is gone would be
  parked forever. Measured on the same run: **16 of 31 keeps** named a directory
  `os.Stat` could not see.

The rung's own question is "would REMOVING THIS DIRECTORY destroy work?", which a
directory that is already removed answers vacuously: no removal can destroy what
a previous removal took, and a loss that already happened is not undone by
refusing later. The shape belongs to the dead-registration rung, which already
owns "the anchor's directory is gone". Confirmed on this host that nothing is
lost by the silence: the nine commits survive in the main repo
(`/home/user/work/gopi`: branch resolves, `rev-list --count … → 9`).

**The red proof failed to go red on the first attempt, and that is recorded
here because the failure mode is general.** The first draft of the case removed a
CLONE ground — so `repo` and `ground` were the SAME vanished path, the probe
short-circuited on the pre-existing "the branch does not resolve here" rung
(Measured=true, ahead=0), and the new guard was never reached: the case PASSED
with the guard disabled. What an operator actually sees is the opposite shape —
the MAIN REPO IS ALIVE, only the ground directory is gone — so the fixture is a
linked worktree whose removal leaves a resolvable branch. With that shape the
guard's removal turns the case red, and the message names the false sentence.

## Deliberately NOT done

- **The rule is not duplicated.** `reclaim_ground_checklist.go` gained no call:
  that predicate's call sites include the ordinary close-out release, where the
  branch is normally merged already and a rung there would be a second copy of
  this one asked in a place with a different job.
- **`gopi task archive` / the branch-delete guards are untouched.** They already
  ask their own delivery question; this rung is about the DIRECTORY.
- **No new dependency, no new verb, no scheduled sweep** (Rule 35). The rung runs
  inside the sweep that already ticks.
