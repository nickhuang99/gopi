<!-- doc-class: normative -->
# Cross-host branch visibility — a local ref is not a delivery

**Status**: measured 2026-10-12 on hosta (HP, client) against peerbox (peer),
while recovering the stranded fix of task `01a12d6b` (row
`01a12d6b-b00a-7d1d-9477-1be79b5a8fe7`, commit `04fda64e2`).

**One sentence**: gopi's worktree habit makes a committed branch visible in the
MAIN REPO of the machine that committed it, and that is a statement about ONE
machine — it says nothing about the other host, so a branch that never crossed a
transport is invisible to the peer while every local check reports success.

## The two layers, and why "it is in the main repo" is the wrong instrument

Rule 6c is correct and this document does not weaken it:

> A worktree shares the main repo's refs — committing in the worktree already
> updates your branch in the main repo. NO push is needed.

That sentence answers **"is my commit lost when my worktree is removed?"** — and
the answer is no, because `refs/heads/<branch>` lives in the shared object store.
The instrument that proves it is local: `git -C <main-checkout> log <branch>`.

There is a SECOND host, and it is a different question. The peer holds no refs of
ours and no objects of ours beyond what a transport (`git fetch`/`git push`,
the push-back lane, the merge/adoption chain) put there. On HP — whose ledger
carries `task_sync: false` and whose `remote.peerbox.pushurl` is absent — a branch
created and committed locally is therefore **invisible to peerbox no matter how it
was committed**, and no local command can tell you otherwise.

So the honest check for "is my deliverable visible to the peer" is a read at the
FAR SIDE, never an exit code or a local `git log` (memory #1830 item 2):

```bash
ssh <peer> 'cd <peer-repo> && git cat-file -e <sha> && echo PEER-HAS-OBJECT || echo PEER-LACKS-OBJECT'
ssh <peer> 'cd <peer-repo> && git for-each-ref | grep <branch-or-id>'
```

## The measured case (narrative evidence, this run)

The stranded row `01a12d6b` closed out with this sentence in its own final
message (`.../sessions/01a12d6b-.../output.log`):

> **分支**：`task/01a12d6bb00a7d1d94771be79b5a8fe7`，commit `04fda64e2`，已在**主仓库**共享对象库中（worktree 共享 refs，无需 push）。

and a probe from the peer two hours later:

```
ssh peerbox 'cd /home/user/work/GoPi && git cat-file -e 04fda64e2'
→ PEER-LACKS-OBJECT                     # the object is absent from peerbox entirely
ssh peerbox '... git for-each-ref | grep 01a12d6b'
→ (nothing)                             # and no ref carries it either
```

Both statements are true at once. The mechanism is visible in the same repo,
because the two lanes are directly comparable:

| commit | made by | ancestor of the peer's master? | on the peer? |
|---|---|---|---|
| `acbce8e2a` (the stranded branch's base) | an HP merge lane that pushed | **yes** | yes |
| `611a3082d`, `02476ccde` (HP master line, 08:38/08:55) | HP lanes that pushed | **yes** (`6fa57c7ec` contains them) | yes |
| `04fda64e2` (the fix) | a worktree commit at 08:18 | **no** (its parent is `acbce8e2a`, but *it* is carried by `refs/heads/task/01a12d6b…` only, locally) | **no** |

A commit crosses to the peer when SOME ref the peer fetches contains it. A lane
that pushes ends up in the peer's master; a lane that only commits leaves an
object nothing at the far end asks for.

**Price of the invisibility, measured**: the flake the stranded commit fixes
(`internal/testutil TestProcIdentityRefusesASignalledRecycledPID`, ~1-in-2) kept
redding acceptance for OTHER rows — a later one-line `COMMANDS.md` regeneration
failed its acceptance on run #1 for exactly this test. The peer had no
`internal/testutil/procargvpublish_test.go` at all, and its
`internal/testutil/procident.go` was byte-identical to the pre-fix blob
(`8317924c6`, checked against both the peer master and the fix's parent).

## What is NOT a defect in this

- `git worktree remove` + `git branch -d` (the finish-cleanup chain ③④) did not
  lose anything: they run only after a push step, and the module's own contract
  is "a removed worktree with an unpushed branch = work loss".
- The row's cleanup did not run at all (the run was left open for the stale
  sweep by the close-out writeback), which is why the branch and the worktree
  still existed hours later and the work was recoverable by name.
- Rule 6c is untouched: inside one machine, a worktree commit is a delivery.

The line that needs care is the one a run writes in its OWN final message: "no
push is needed" is a statement about the local ref, and it must not be read as
"the peer can see this". A branch whose purpose is to be landed by the peer
needs a transport that carries it, and the recovery of a stranded branch costs
exactly one cherry-pick **because the commit message carries the reasoning**.

## Recovery recipe (used here, ~6 minutes)

```bash
# 0. prove it is stranded, from the peer's side (never from an exit code)
ssh <peer> 'cd <peer-repo> && git cat-file -e <sha> || echo PEER-LACKS-OBJECT'

# 1. the old ref still exists locally while its row/ground does — read it there
git log --oneline <stranded-branch>
git branch -a --contains <sha>          # reachable from which ref, exactly

# 2. re-apply the CONTENT, not the branch (Rule 23: never commit on someone
#    else's branch): cherry-pick onto your own branch, which already contains
#    the peer's master so the result is fast-forwardable there
git cherry-pick <sha>                   # clean = the trees had not diverged

# 3. prove the resulting TREE is byte-identical to the measured one, so the
#    measurements taken on the old tree still describe this one
git rev-parse <old-merge-tip>^{tree}     # == HEAD^{tree}  → same tree object
```

Step 3 is what lets a `Ran:` line taken before the recovery still be an honest
`Ran:` line after it: `73706dee9` was the tree under which the green loop ran,
and `ce02ab85b` carries the same tree.

## Not to be confused with

- **The flake's own root cause** — the `/proc/<pid>/cmdline` publication window
  after `Start()` — which lives in the recovered commit's message and in
  `internal/testutil/procident.go`.
- **The peer's ahead-ness** (its master is not an ancestor of a client branch
  cut earlier): that is ordinary divergence, sampled with `git log --oneline
  <mine>..<peer-master>` and merged (`git merge <peer-master> <branch>`) — and
  it matters here because the peer lands with `merge --ff-only`.
