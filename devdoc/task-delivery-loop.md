<!-- doc-class: normative -->
# The bounded delivery loop — one hand-over, measured and re-tried, instead of one `git push`

**What this closes.** A deliverable branch reached the peer through exactly one
`git push`. When the peer said no, the lane printed "the peer rejected the push"
and the branch stayed local. That sentence hid four different situations, and
only one of them is "somebody pushed first":

| what the peer's no actually means | what it needs |
|---|---|
| its trunk/branch moved past our base | fetch, align the branch onto it, push again |
| it owns `refs/heads/<name>` at a diverging tip | fetch, align onto **that ref**, push again |
| the branches cannot be merged at all (content) | a decision — a resolver, or a human |
| it is unreachable / the push is refused outright | nothing; no number of attempts helps |

One call reported all four identically. The loop
(`internal/tools/task_delivery_loop.go`, `deliverBranchToPeer`) replaces that with
a bounded `for`: every round **re-fetches** the peer's state, **measures** which
of the four it is looking at, **aligns** the branch when the cause is one a merge
can fix, and **pushes** again — at most `pushbackDeliveryRounds` (3) times.

## The three call sites, one implementation

`deliverBranchToPeer` is the ONLY loop in the tree. Both delivery paths call it:

- **the periodic flush** (`task_sync_git_pushback.go`, `gitPushbackPlan`): a
  refused push escalates to the loop; a delivered loop records the branch as
  pushed *and* fast-forwarded, and an aligned-but-undelivered branch is recorded
  as `NotFastForwardable` (it now contains the peer's master, which is a fact the
  peer's `merge --ff-only` cares about, even though the push still did not land).
- **the close-out chain** (`task_finish_cleanup.go`, step ②): a refused push
  escalates to the same loop. The loop's `Reason` — the last round's own git
  output — is what the step reports. The old name-mismatch arm (a
  `push -- <target> <branch>:<branch>` retry plus a tip comparison) is gone:
  the loop's alignment covers the same ground and reports it in one shape.

Because both paths run the loop in the **main checkout**, whose HEAD is the
integration branch and NOT the branch being delivered, the loop may not use a
working tree at all. See the next section.

## The alignment touches no working tree, and rewrites no commit

Both callers hand the loop a directory that is *not* checked out on the branch
under delivery: the flush works in the main checkout, and the close-out push
deliberately runs *outside* the run's worktree (step ③ may already have removed
it). A `git merge` there would either refuse or merge into the wrong branch, so
the alignment is built the way git builds a merge internally:

```
merge-tree --write-tree <branch> <ref>   → the merged TREE (and, on failure,
                                            the conflicted PATH NAMES on stdout)
commit-tree <tree> -p <branch> -p <ref>  → a real two-parent merge commit
update-ref refs/heads/<branch> <commit> <oldTip>
```

Three properties follow, and each is a test:

1. **No working tree is touched** — so the loop runs wherever the caller is.
2. **Nothing is rewritten.** The commit is a *descendant* of the branch tip, so a
   branch that has already crossed the wire keeps every commit it had. (This is
   also why the push-back lane's "shares history with master" invariant still
   holds after an alignment.)
3. **A failed alignment leaves the ref untouched** — `update-ref` is the last
   step, so a failed `commit-tree` cannot half-apply.

## The refusal is measured, never assumed

After a refused push the loop asks two questions, and each answers with *a ref to
align onto*:

- **Does the peer own `refs/heads/<branch>` at a diverging tip?**
  (`gitPushbackPeerRefTip` + `gitPushbackClassifyPeerRef` — the lane's existing
  pair, not a second implementation.) Divergent → align onto **that ref**.
- **Does this branch contain the peer's master?**
  (`gitPushbackFastForwardsOntoMaster`.) No → align onto the master mirror.

Neither → the refusal is not about alignment (a server-side hook, a permission, a
`remote.pushurl` block) and the loop **stops** with git's own sentence. Spending
rounds on a constant is the failure the brake exists to prevent.

The state carried between rounds is therefore just `nextRef` — a *ref*, not a
verdict — so the next round re-reads it after **its own** fetch. A peer that
moved again is a tip the loop has not seen, which is exactly why every round
fetches first.

## Who resolves a content conflict: the existing session channel

`pushbackConflictResolver` is a declared seam, not a new subsystem (Rule 3). It
receives the branch, the peer's tip and **git's verbatim conflict output**, and
answers whether a resolution now exists.

- A caller with a session (the production close-out, the flush) supplies a hook
  that hands the decision to that session.
- A caller without one supplies `nil`, and the loop **stops** on the first
  conflicted round with the paths quoted — rather than looping on a merge that
  will conflict identically.
- The hook's answer is **re-measured**: after it returns `true` the loop runs
  `merge-tree` again. A hook that claimed more than it did surfaces as git's own
  refusal, not as a silent claim.

## The reason a human reads

`Reason` is the **last round's own measurement** plus the round accounting:

```
<branch>: the peer rejected the push — … — git said: … (delivery loop: round 2/3 measured name-collision)
```

The measurement comes **first** and the accounting is the tail, because these
lines are appended to the lane's existing refusals and a reader (or a grep for a
branch name) must not have to parse past a prefix. `pushbackDeliveryReason` is the
one composition, so every exit accounts for its round the same way.

## Verification

```bash
go test ./internal/tools/ -run 'TestDeliveryLoop' -count=1    # the four properties + the named limit
go test ./internal/tools/ -run 'TestPushback|TestFinishCleanup|TestPeerRemote' -count=1
```

The loop's tests drive **real git** — a bare peer and real clones — because every
question here ("does this branch contain that tip", "is this merge clean", "does
the peer accept this refspec") is a git-semantics question a mock cannot answer.
The refusal fixture is the shape that actually refuses: the peer **already owns
the branch name** at a diverging tip. (A push of a name the peer has never seen
always succeeds — measured while writing these tests: the loop delivered in round
1 and the fixture's premise was wrong. Rule 31 in miniature: a test that was never
forced to observe the refusal never tested it.)

## A defect found while building it

`gitPushbackGitRun`/`gitPeerRun` return **stderr** on failure and **stdout** on
success. `merge-tree` prints the conflicted **path names on stdout**, so wrapping
it in the shared runner silently dropped exactly the fact the reason exists to
carry: the line said `content-conflict` with no path on it. The trial merge now
reads the process's own two streams. Nothing else needed to change — this was a
call site choosing the wrong reader, not a defect in the runner.

## The consequence for every existing "the push must fail" fixture

Four tests in this package pinned the delivery-failure paths by making the
peer's **same-named branch diverge** (`task_finish_cleanup_test.go`,
`task_finish_push_contract_test.go` ×2,
`task_finish_cleanup_gone_delivery_claim_test.go`). Under one-shot push that
was a refusal; under the loop it is a **successful delivery** — the loop merges
the peer's divergent ref and pushes. The tests failed with `step push ok=true
want false`, and the fix is in the FIXTURES, not in the loop:

> A diverged peer branch is not a failed delivery any more. It is a delivery the
> loop is supposed to make.

The semantics those four tests actually pin — the chain STOPS at the push, the
worktree and branch SURVIVE, and the reason NAMES the directory that was not
collected — still hold, but they need a push that really cannot go through. So
each fixture now makes the peer rewrite **the same file our branch adds**
(`b.txt`, different content). That is a content conflict: with no resolver
declared (the production close-out passes `nil`), the loop stops on round 1 and
quotes git's own conflict output.

This is the general lesson for anyone writing a delivery-failure fixture from
here on: **a cause the loop can repair is not a cause**. Ask which of the six
`pushbackDeliveryCause` values the fixture actually produces, and pick one no
round can change (`content-conflict` with no resolver, `unrelated-history`, a
name collision the peer owns and we do not want, a dead target).

Measured (before → after, `make test-budget PKGS='./internal/tools'`):
`5 FAIL` → the four above PASS in 2.062s; the fifth,
`TestMemorySearchNonBlocking`, is a pre-existing concurrent-vtable flake
(`database schema has changed: vtable constructor failed: distilled_qa_fts`)
that passes when run alone and is unrelated to delivery.
