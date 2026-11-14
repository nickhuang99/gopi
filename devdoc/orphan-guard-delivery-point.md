<!-- doc-class: historical -->
# Task Report: the orphan guard asks the ref the delivery just made

## Which side was wrong (the deliverable's one required sentence)

**The GUARD'S REACHABILITY SCOPE was incomplete — not the fixture and not the
assertion.** The fixture (`newFinishCleanupFixture`: bare server + client +
one linked worktree on `feat/sync-conflict-completed`) is honest: its branch
has one commit, and that commit is reachable from no ref in the client repo —
exactly the state the guard is supposed to name. The assertion (step ④
`branch` = OK) is likewise honest: the chain pushed that tip to its target in
step ②, so deleting the local ref does NOT make the commit unreachable. The
guard's question — "would deleting this branch leave its tip reachable from no
other ref?" — was answered from the LOCAL ref namespace only, so it could not
see the ref the chain had just created at its delivery point, and it refused
the chain's own healthy close-out. Evidence below.

## The red, measured at base (both sides of the fix)

Master base at `a9a96cd7` (the orphan guard landed in `feaa8c6b`):

```
--- FAIL: TestFinishCleanupChainCleanWorktree (2.29s)
    step branch ok=false want true (detail: error: The branch 'feat/sync-conflict-completed'
    is not fully merged. — its tip is reachable from no other ref (64b78ef26118) — …)
--- FAIL: TestFinishCleanupChainRepoIsWorktree (0.26s)
    step branch ok=false want true (detail: … reachable from no other ref (a65669f9156a) …)
```

Green at the commit BEFORE the guard, same fixtures, same assertions
(`feaa8c6b^` = `9ce8e159`, detached worktree `/home/user/workspace/gopi-orphan-guard-base`):
`ok github.com/nick/gopi/internal/tools 2.997s`. So the guard's arrival is what
turned these two tests red — and they were green because the pre-guard code
force-deleted the ref after its own successful push, which is CORRECT for a
delivered tip and wrong only for an undelivered one.

## Completed

- **`branchDeleteOrphanRefusalAt(repo, branch, deliveryPoint)`**
  (`internal/tools/branch_delete_guard.go`) — the guard can now be asked WITH
  the target the caller DELIVERED to. `branchDeleteOrphanRefusal(repo, branch)`
  is the unchanged no-delivery-point form (an empty point), so the adoption arm
  (`task_adoption.go`) and the existing regression test keep their exact
  semantics.
- **The delivery point is asked over the delivery's own channel**: one
  read-only `git ls-remote --heads <target> refs/heads/<branch>` — the read side
  of the push that created it. It never fetches and never writes into the repo
  under judgement. A bare path (`/tmp/x/server.git`) and an ssh alias
  (`peerbox:~/work/GoPi`) are both just repository ARGUMENTS; the git process's
  `Dir` stays the judged repo.
- **FAIL-CLOSED, and the local scan is NOT weakened**: the delivery point counts
  as holding the tip only when it lists EXACTLY the tip at risk. An unreachable
  point, an absent ref, an unparseable answer, or a DIFFERENT tip (the branch
  moved after the push) all answer "no evidence" → the local scan decides, and
  the local refusal stands. The guard is therefore never weaker than before for
  any caller that did not deliver.
- **The refusal names what it asked** (`branchDeleteDeliveryPointMarker`): when a
  delivery point was consulted and did not hold the tip, the refusal says so
  beside the local scan, so an operator reading it knows which refs were in
  scope (Rule 15 — a checked step is reported, never silent).
- **Chain wiring** (`task_finish_cleanup.go` step ④): the guard is now called as
  `branchDeleteOrphanRefusalAt(branchDir, branch, target)` — `target` is the
  step-② push target, which the chain already holds.
- **Regression test `TestBranchDeleteGuardAsksTheDeliveryPoint`**
  (`internal/tools/live_row_session_veto_test.go`), six poles on a real git
  fixture: DELIVERED (target holds the tip → not refused), UNDELIVERED (empty
  target → refused, refusal names the point), UNREACHABLE target → refused,
  MISMATCHED tip → refused, NO POINT named → refused (the old form untouched),
  LOCAL CARRIER (a local ref holds the tip → reaped — the reachability half
  unchanged). The two-pole proof is inside one test: same repo, same branch,
  the delivery point is the only thing that changes the answer.
- **Named constants (Rules 1b/13/14)**: `branchDeleteDeliveryPointMarker`,
  `branchDeleteLsRemoteVerb/HeadsFlag/FieldSep` — no new bare literals; the
  `refs/heads/` namespace keeps coming from `refsHeadsPrefix`.

## Not Done (honest)

- **`task_adoption.go`'s force-prune keeps the local-only form.** Its arm does
  not push — it prunes a ref whose merge staged nothing after asking the
  tracker's own merge probe — so it has no delivery point to name, and giving it
  one would be a guess. If an adoption-style arm ever delivers before pruning,
  it must pass its target.
- **The guard still does not consult a REMOTE it did not push to** (e.g. a
  peer's copy of the branch that this host never pushed). That is out of the
  question's scope: the predicate asks "would this delete orphan the tip", and a
  ref this host never observed is not evidence it may rely on.
- Pre-existing master-side redness in `internal/standards` (Rule 16 gate) and in
  `cmd/agent`'s `TestTaskRun*E2E` family is untouched by this branch — both
  reproduce on a detached master worktree.

## Verification

See `## Evidence (self-filed)` for the measured outputs. Commands run in
`/home/user/workspace/gopi-orphan-guard-scope` (branch `fix/orphan-guard-scope`,
a linked worktree off master).

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0, no output).
- `gofmt -l` on the three touched files → no output; `go vet ./internal/tools/` → OK.
- Tests: `go test -count=1 -run 'TestFinishCleanup|TestBranchDeleteGuard|TestWorktreeOwnership|TestDeadLocalHolder' ./internal/tools/`
  → see Evidence (the epic's two tests plus their sibling chain tests).
- Red proof: with the delivery-point argument neutered, the new test's DELIVERED
  pole must fail — see Evidence.

## Evidence (self-filed)

Every line below is a command this run ran and its observed output.

**1. The red, at master base (`a9a96cd7`), in this worktree before the fix.**
`go test -count=1 -run 'TestFinishCleanup' ./internal/tools/`:

```
--- FAIL: TestFinishCleanupChainCleanWorktree (2.29s)
--- FAIL: TestFinishCleanupChainRepoIsWorktree (0.26s)
    step branch ok=false want true (detail: error: The branch 'feat/sync-conflict-completed'
    is not fully merged. — its tip is reachable from no other ref (64b78ef26118) — the commit
    would survive in no ref; archive it (`git tag archive/…`)
```

**2. The green, one commit earlier (`feaa8c6b^` = `9ce8e159`).** Same fixtures,
same assertions, in a detached worktree at that commit
(`/home/user/workspace/gopi-orphan-guard-base`): `ok  github.com/nick/gopi/internal/tools  2.997s`.
The guard's arrival (`feaa8c6b`) is what turned the two tests red — the
pre-guard code force-deleted the ref after its own successful push, which is
right for a delivered tip and wrong only for an undelivered one.

**3. Two fixture bugs the new regression test caught in ITSELF (both fixed in
the test, evidence that the poles are load-bearing).** First run:

```
--- FAIL: TestBranchDeleteGuardAsksTheDeliveryPoint (0.39s)
    live_row_session_veto_test.go:294: a delivery point holding a DIFFERENT tip was accepted as evidence
```

Cause: the MISMATCHED pole committed on `master` instead of on
`feat/delivered`, so the branch never moved and both tips were equal — the pole
proved nothing. Fixed by committing on the branch and asserting the two tips
differ before asking. Second run, the pole then exposed its follow-on:

```
--- FAIL: TestBranchDeleteGuardAsksTheDeliveryPoint (0.44s)
    live_row_session_veto_test.go:310: a tip the LOCAL repo still reaches was refused: its tip is
    reachable from no other ref (54a28d418df5) — … — the delivery point …/gone.git did not hold
    refs/heads/feat/delivered at that tip (unreadable or a different tip)
```

Cause: the LOCAL CARRIER pole minted its carrier at the pre-move `tip`, so after
the branch moved the tip under test was still unreferenced. Fixed by minting the
carrier at the branch's CURRENT tip (`moved`). Third run green:

```
--- PASS: TestBranchDeleteGuardRefusesAnOrphaningDelete (1.22s)
--- PASS: TestBranchDeleteGuardAsksTheDeliveryPoint (0.92s)
ok  	github.com/nick/gopi/internal/tools	3.399s
```

**4. Acceptance observed** (this branch, after the fix, final run):
`go test -count=1 -run 'TestFinishCleanup' ./internal/tools/` →
`ok  github.com/nick/gopi/internal/tools  30.053s` (the two epic tests plus every
sibling chain test in the same package run). Sibling guard/worktree tests in the
same batch:

```
--- PASS: TestBranchDeleteGuardRefusesAnOrphaningDelete (1.22s)
--- PASS: TestBranchDeleteGuardAsksTheDeliveryPoint (0.92s)
ok  	github.com/nick/gopi/internal/tools	3.399s
```

`TestWorktreeOwnership*` (6) + `TestDeadLocalHolder*` (2) all PASS in the same
batch. `CGO_ENABLED=0 go build ./...` → exit 0, no output; `gofmt -l` on the
three touched files → no output; `go vet ./internal/tools/` → exit 0.
