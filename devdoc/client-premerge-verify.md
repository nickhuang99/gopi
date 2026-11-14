<!-- doc-class: normative -->
# Client pre-merge verification before push (the close-out chain's step ①b)

Task: `01a0ab18` — *client push must pre-merge server master and verify buildable
BEFORE pushing the branch*.
Branch: `feat/client-premerge-verify` (worktree `/home/user/workspace/gopi-premerge`).

## The defect this closes

The close-out chain (`internal/tools/task_finish_cleanup.go`) pushed a branch and
only then let the server discover whether that branch was any good. A branch that
could not merge — or that merged into something which did not compile — still
travelled to the server, where it occupied a merge slot, produced a conflict
resolution task, and only then reported a problem the client was in a position to
find for itself in seconds. The ordinary developer action (fetch upstream, merge
it locally, rebuild, and only then hand the work over) was simply absent from the
chain.

## Where the gate sits, and why there

`runFinishCleanupChainTarget`'s **CLIENT arm**, between the dirty guard and the
push:

```
① guard (terminal row + clean worktree)     ← unchanged
①b pre-merge verify  ← NEW (this task)      ← refuses here ⇒ NO push
② push
③ worktree / ④ branch
```

- **After the dirty guard**, deliberately: a dirty tree is a live Bond still
  writing there, and refusing *there* is cheaper and gives a better reason than
  running a merge inside a tree someone else is editing. (The neighbouring task
  `01a0aab8` — completed 2026-09-16 — decoupled the *push* from the dirty guard;
  this gate keeps the honest part of that ordering: it never merges into a tree
  that is still being written.)
- **Immediately before the push**, because that is the last point from which "do
  not push" can still be obeyed. A gate anywhere downstream (the merger) cannot
  act on it; a gate per-caller would be bypassed by every other caller of the
  chain.
- **Client arm only.** On the server arm the branch is already local, and the
  merger's own pre-merge build gate + post-merge rebuild own that lane.

## What runs, per attempt

All of it inside the session worktree the run owns (`task_finish_premerge.go`):

| step | mechanism | on failure |
|---|---|---|
| ① fetch | `git fetch --force --no-tags <peer> +refs/heads/master:<mirror>` into the SAME reserved mirror ref the push-back core uses | **ABSTAIN** — push, and record `premerge-unverified` |
| ② up-to-date | `merge-base --is-ancestor <mirror> HEAD` | — (this IS the cheap path) |
| ③ trial merge | `git merge --no-edit <mirror>` | **REFUSE** — `merge --abort`, no push, `premerge-refused` |
| ④ rebuild | the repo's DECLARED build (`config.ResolveRepoBuild`, Rule 12) in a detached scratch worktree | **REFUSE** — unwind, no push, `premerge-unverified` |
| ⑤ unwind | `merge --abort` if a merge actually stopped, else `reset --hard` to the recorded pre-tip | recorded; if reset itself fails the refusal names it |

### Why the durable mirror ref and not `FETCH_HEAD`

`FETCH_HEAD` is per-checkout state and `git fetch` writes it in the *current*
worktree. The gate merges a peer-tracking object, so it must live at a location
the gate itself names — a durable `refs/…` mirror. (The push-back core already
reserves exactly such a ref for the peer's master; the gate reuses it rather than
inventing a second namespace.)

### Why merge and not rebase

- The deliverable is a **delivered branch**: rebasing rewrites its commits and
  invalidates every tip already recorded against it (the ledger's deliverable,
  the row's session log, another agent's `git show`).
- "Branches are preserved" is an invariant of this repo, not a preference; a
  rebase here would rewrite already-recorded history.
- Rebase converts an honest "these two changes disagree" into a sequence of
  per-commit conflicts the author resolves blind; a merge's failure *is* the
  finding.
- The server will itself merge — the client doing the same thing is verifying the
  operation that will actually happen, not an operation of its own invention.

### Why the real `git merge` and not `git merge-tree`

`merge-tree` answers the textual-conflict question without touching the worktree,
but only a real merge exercises the index, rename and directory-rename semantics
that conflicts actually come from. And a *clean* real merge is then **reused** as
the verified state that gets pushed, so it costs nothing extra.

## Refusal is not destruction

A refused delivery keeps the branch, the worktree and the commits — the gate only
*unwinds its own trial merge*. Consequences, all asserted physically in tests:

- the server does **not** hold the ref (`refs/heads/<branch>` absent),
- the local branch ref still exists, the worktree still exists,
- the worktree is back at its pre-merge tip (`status --porcelain` empty — a
  conflicted index must never ride the push).

## Visibility (Rule 15 — no silent failure)

Every non-OK verdict and every abstention writes a `task_events` row on the
row's own trail — `premerge-refused` / `premerge-unverified` — trimmed to the
last `finishCleanupPreMergeEventKeep` (3) events per row so a delivery that keeps
failing cannot grow the ledger without bound. `gopi task show <id>` therefore
explains why the branch is still on the client host.

The gate distinguishes three outcomes, and report lines say which one happened:

- `pre-merge ok=true "merged peer master locally and rebuilt — the branch is mergeable and compiles"`
- `pre-merge ok=true "branch already contains peer master (fast-forward at the peer) — nothing to merge, no rebuild"`
- `pre-merge ok=false "<marker>: <git's own output> merge unwound — the worktree is back at the pre-merge tip"`

## The cheap path (the epic's "no-op when already up to date")

One `merge-base --is-ancestor` call decides it: the branch already contains the
peer's master ⇒ **no trial merge, no merge commit, no rebuild**. Measured: the
declared build in that scenario is deliberately `exit 9` — and it never runs,
because there is nothing to verify. The pushed tip is the branch's own tip.

## Conflict classes caught

Anything `git merge` refuses, which the verdict names explicitly: **content**
(both sides edited the same lines), **modify/delete**, **rename** and
**binary**. The `merge --abort` is the `--abort` *flag* — the bare `merge abort`
spelling is parsed as "merge the revision named abort" and was measured leaving
the conflicted index in place (recorded in the constant's comment so it is not
re-invented).

## Abstentions (honest, never a silent pass)

A client that cannot judge must not impersonate a verdict. The gate **pushes and
says so** when it:

- has no peer master resolvable (`GOPI_PEER`/peer settings absent),
- cannot fetch the peer master (offline, auth, unknown remote),
- has no branch resolved,
- has **no ground**: the delivered ground must be checked out ON the delivered
  branch. A main checkout, a detached HEAD, or another branch ⇒ abstain. (This
  is the Rule 29 guard: the gate never merges into a shared checkout whose tip is
  someone else's.)
- has **no declared build** (Rule 12): a repo with no `repos.<path>.build` entry
  is merged and the abstention is named — the gate never invents a command for
  someone else's project. The `go build ./...` default belongs to the merger's
  lane and stays there.

Each abstention writes `premerge-unverified` with the reason.

## Escape hatch

`GOPI_FINISH_CLEANUP_PREMERGE` (`"0"/"off"/"no"/"false"/"disabled"` turns the
gate off), read in exactly one place. With it off the chain behaves exactly as it
did before the step existed — asserted.

## Coordination with task `01a0aab8`

`01a0aab8` (*client push must not be gated by the worktree dirty guard*) is
**completed** (2026-09-16 22:57), and its concern is orthogonal and preserved:

- it moved the push off the *destructive-step* guard;
- this gate does not reintroduce that coupling — it sits after the dirty guard
  but **before** the push, and the push itself is unchanged;
- the two touch the same function (`runFinishCleanupChainTarget`), so
  `master` was merged into this branch and the package rebuilt before the work
  was committed: the seam was verified against the current function body, not
  against the briefing's snapshot. The new code is additive — a new sibling file
  plus ~48 lines in the chain (the plan entry, the gate call, and a doc-comment
  note), with one pre-existing assertion narrowed (a run that already has a
  clean pre-merge step now legitimately reports it in `done`).

## Rule 11 (LLM adoption) — why not applicable

This is not a tool the LLM chooses to call; it is a step inside the close-out
chain, entered by the chain itself. There is no prompt-routing decision to prove
and no wire name for the model to emit, so the adoption-proof standard does not
apply. The equivalent evidence here is behavioural: the three scenarios below,
each asserted against a real two-node git world.

## Scope honestly stated

- One push path is covered: the close-out chain's client arm
  (`finishCleanupGit(…, finishCleanupPushVerb, …)`). Two other places run a
  `git push` that this gate does not intercept — `internal/tools/task_sync_git.go`
  (the periodic sync pass) and `cmd/agent/cli_tasks.go` (a worker-side push). Both
  were left alone deliberately: the sync pass pushes whatever it finds and is
  idempotent, and neither is the delivery-of-a-task path this epic names. They are
  named here so the next reader does not mistake the gate for universal, and are
  the natural follow-up if the push authority is ever consolidated.
- The rebuild runs the repo's *declared* build only. A repo declaring no build is
  merged and abstains — never silently passed.

## Verification (measured)

```
go build ./...                     → OK
go vet ./internal/tools/           → OK
go test -run 'PreMerge|FinishCleanup' ./internal/tools/  → ok (all PASS)
```

Scenario output (the operator-visible report lines, printed from a real run):

```
[①CONFLICT]  test branch=feat/sync-conflict-completed done=[] failed=[pre-merge: branch does NOT merge into peer master (conflict classes: content, modify/delete, rename, binary): Auto-merging a.txt merge unwound — the worktree is back at the pre-merge tip]
[①CONFLICT]  server has ref: false
[②CLEAN]     test branch=… done=[pre-merge,push,worktree,branch]
[②CLEAN-STEP] pre-merge ok=true :: merged peer master locally and rebuilt — the branch is mergeable and compiles
[②CLEAN-STEP] push      ok=true :: * [new branch]      feat/sync-conflict-completed -> feat/sync-conflict-completed
[②CLEAN]     declared build ran: true
[②CLEAN]     server has ref: true
[③UPTODATE]  test branch=… done=[pre-merge,push,worktree,branch]
[③UPTODATE-STEP] pre-merge ok=true :: branch already contains peer master (fast-forward at the peer) — nothing to merge, no rebuild
[③UPTODATE]  server has ref: true
[④ROW]       event="premerge-refused" detail="branch does NOT merge into peer master (…): Auto-merging a.txt merge unwound — the worktree is back at the pre-merge tip"
```

Tests (`internal/tools/task_finish_premerge_test.go`), each over a real bare
"server" + client repo + linked worktree:

- `TestPreMergeConflictBlocksThePush` — refused, no server ref, branch+worktree
  survive, worktree clean (trial merge unwound).
- `TestPreMergeConflictIsRecordedOnTheRow` — the refusal reaches `task_events`.
- `TestPreMergeCleanMergeRebuildsThenPushes` — the declared build really ran
  (marker file), push landed, and what landed **contains the peer's master tip**
  (`merge-base --is-ancestor`), with the merge commit visible on the server.
- `TestPreMergeFailingBuildBlocksThePush` — clean merge, failing build ⇒ no push,
  clean worktree.
- `TestPreMergeUpToDateIsAZeroCostNoOp` — cheap path: no merge commit, pushed tip
  is the branch's own tip, and the (deliberately failing) build never ran.
- `TestPreMergeAbstainsWithoutADeclaredBuild` — merged and abstained, named.
- `TestPreMergeIsOffByTheKillSwitch` — off ⇒ old behaviour.
- `TestPreMergeGroundMustBeTheDeliveredBranch` — main checkout / detached HEAD /
  another branch are never accepted as ground; end-to-end abstention + push.

## Rules honoured

Rule 1/1b/13 (every literal and git spelling is a named constant), Rule 5
(build+test+report), Rule 6 (worktree `gopi-premerge`, own branch), Rule 8 (files
added one at a time), Rule 12 (the declared build contract is the single source),
Rule 14 (the build timeout is `mergeBuildGateDefaultTimeoutSec`, not a third
copy), Rule 15 (no silent failure — every non-OK and every abstention is
recorded), Rule 22 (the rebuild's scratch worktree comes from the verify-class
scratch root, off `/`), Rule 27 (no device load anywhere in this work).
