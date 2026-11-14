<!-- doc-class: historical -->
# Devdoc: the park lane's third step + a satisfiable park briefing

Epic: **merger-park-worktree-release** (2026-09-17, HP client host).
Branch: `fix/merger-park-worktree` (fresh from `master` @17cec86b — see "Rule 23"
below; the pre-existing branch of the same name was NOT landed, it was read).

Two measured defects in the merger's park lane, both fixed here:

- **D1 — the park was SELF-TRAPPING.** `repoSweepParkBranch` archived the ref
  (`refs/tags/archive/<branch>`, which removes the branch from every merge PATH)
  and filed one ledger row, but never released the branch's linked worktree —
  and a registered checkout removes the branch from the merge CANDIDATE SET
  (`repoSweepProtectedBranches`). Doubly excluded, the branch is invisible to
  every later tick while its filed row reads as progress. The operator's
  requirement (ledger `01a0aaa6`, `source=human`) names **three** steps for a
  park: file a task, archive the blocking branch, **delete the worktree**.
  Master implemented two.
- **D2 — the filed briefing was UNSATISFIABLE as written.** `parkTaskPayloadFmt`
  told the worker to `merge <trunk> into <branch>`; the payload's acceptance and
  the merger's own predicate disagreed, so a worker could follow the briefing
  exactly and still deliver something the next tick parks again.

## What changed

| file | change |
|---|---|
| `internal/tools/repo_sweep_park_worktree.go` | **new** — the third step: registration lookup, refusal classification, `worktree remove --force`, the park/row/release fold, and the drain arm |
| `internal/tools/repo_sweep.go` | park runs all three steps; the briefings are rendered from one argument list and one command builder set; the drain arm is called; four new counters; `repoSweepActionError` (Rule 1b) |
| `internal/tools/repo_sweep_park_worktree_test.go` | **new** — the D1 and D2 regression tests |
| `internal/tools/repo_sweep_park_test.go` | the park-row reader matches the summary HEAD (see "the 200-rune cap") |
| `cmd/agent/cli_tasks.go` | the tick's log line names the third step (`park-worktree-released` / `-FAILED` / `park-drained` / `park-drain-FAILED`) |
| `devdoc/demo-park-worktree-release.sh` | the live before/after demonstration |

### D1: the three steps are ONE act

`repoSweepParkBranch` now reads:

```go
if err := repoSweepArchiveBranch(repo, branch); err != nil { … }   // ① the park ref
action, detail := repoSweepParkFileRow(repo, branch)               // ② the ledger row
return repoSweepParkFoldRelease(action, detail,
    repoSweepParkReleaseWorktree(repo, branch))                    // ③ the worktree
```

Design decisions, each load-bearing:

- **The third step runs LAST.** The ref + row pair is what *preserves* the
  branch's work. Releasing the checkout before the work is durably recorded
  would trade one trap (a branch hidden behind its own checkout) for another (a
  branch whose only copy was an uncommitted tree).
- **`repoSweepParkFileRow` is split out** so an early `return` in the row arms
  cannot skip the release. That early return is exactly how the step went
  missing in the first place.
- **`worktree remove --force`, always.** By the time the removal runs the ref
  and the row are in place, so the tree's contents are the run's residue; and a
  dirty-check-then-remove pair races whatever is still writing there. Measured
  2026-09-16: ten dead-holder branches were trapped behind dirty trees.
- **Refusals are classified, never rounded up to success** (Rule 15). Three
  refusals exist, each a named constant:
  `repoSweepParkWtNoLinkedReason` (steady state — nothing held it),
  `repoSweepParkWtMainCheckoutReason` (the branch is in the **main** checkout:
  removing that directory deletes the repository, and the branch is visible
  there to whoever put it there — a stated skip, not a failure), and
  `repoSweepParkWtLockedReason` (git's own `worktree lock`; `remove` refuses a
  locked registration, so the removal is not attempted with a double force).
  The classification's **default arm is "failure"**: a refusal the code does not
  recognise must never read as success.
- **A distinct action for a completed park** (`parked-worktree-released`) so a
  half-completed park is visible in the tick's log instead of looking clean. A
  release failure outranks everything except a failed row: the row failure is
  the louder defect (the branch has no carrier at all).
- **The park is still a REF.** Nothing in the new code deletes a branch; the
  branch ref and its archive tag survive, so a park stays reversible.

### The drain arm (self-healing)

`repoSweepDrainParkedWorktrees` releases the leftover checkout of a branch the
merger **already decided about** — archive ref present, worktree still
registered. No other arm can reach that state: the candidate scan skips
archive-exempt branches *before* the worktree question is asked, the
merged-worktree recycle only knows already-merged branches, and the reclaim arms
refuse a live checkout. Scope is exactly "already decided": a branch that is
merely checked out but NOT parked is untouched (that is live work).

**Known residual hazard, measured and dispatched — not solved here.** An
already-parked branch can have a worker mid-repair *inside* the worktree the
drain releases; that worker's final extraction step then loses its work (the
~42-occurrence extraction-failure shape). The release is deliberately NOT
narrowed by a dirty-tree or holder-liveness guard in this branch, for a
measured reason: the ten dead-holder branches of 2026-09-16 were trapped behind
DIRTY trees, so a dirty-guard would regress exactly the win D1 is for, and a
liveness guard is the ownership model Rule 29/C3 exists to avoid — the daemon
already has a dead-holder salvager (`internal/tools/dirty_holder_salvage.go`) for
that question. The hazard is filed as its own task
(`01a0abfc-3e3f-7923-9355-1010f117d1c7`, tags bug/merger/worktree/data-loss)
rather than folded in, per Rule 26. Until it lands, the honest statement is:
**the drain releases deeply-parked residue and a live reparking worker may lose
an unextracted deliverable.**

### D2: the briefing now asks for the deed the merger can perform

`parkTaskPayloadFmt` is rewritten. It

- states that the merger lands **only** by `merge --ff-only` (rendered from
  `parkWtGitMergeVerb` / `parkWtGitFFOnlyFlag`),
- names **rebase** as the reaching approach (`git fetch . <branch>` then
  `git rebase <trunk>` then `git rebase --continue`), each command line built by
  one function (`parkWtPayloadFetchCmd` / `parkWtPayloadRebaseCmd` /
  `parkWtPayloadRebaseContCmd`),
- carries a **runnable self-check** in the merger's own polarity —
  `git merge-base --is-ancestor <trunk> <branch> && echo FF-ABLE`, built by
  `parkWtPayloadSelfCheckCmd` from `parkWtGitMergeBaseVerb` /
  `parkWtGitIsAncestorFlag` / `parkWtPayloadFfAbleProof`,
- warns that a branch whose resolution was replayed onto a **stale** trunk tip,
  or left uncommitted, is reparked, and
- keeps the honest escape: if ff-ability is genuinely unreachable, deliver the
  best resolution **and state the non-convergence in the report** — a stated
  non-convergence is a decision somebody can act on; a silent re-park is an
  infinite loop.

The whole command list lives in **one** place (`parkWtPayloadCmds` +
`parkWtPayload*Cmd`), so the briefing the worker reads and the instructions the
regression test executes are the same strings (Rule 13).

## D2, re-measured — a correction to the epic's premise

The epic states that merging the trunk into the branch "adds a commit so it can
never become ff-able". **That is false, and it was measured** (HP host, git
2.34, `~/workspace/_tmp/fftest.*`):

```
git checkout br; git merge --no-ff master; <resolve>; git commit
git checkout master; git merge --ff-only br   → SUCCEEDS
git merge-base --is-ancestor master br        → exit 0
```

A merge commit's parents include the trunk tip, so the trunk *becomes* an
ancestor of the branch and the branch is ff-able again. What actually survives
as the loop is narrower: a resolution **replayed onto a stale trunk tip**, or a
resolution left **uncommitted / committed on the trunk** instead of on the
branch.

Two consequences, both landed here:

1. The rebase instruction is a **clarity verdict**, not the removal of an
   impossibility — it names the operation that keeps the branch's commits linear
   and pins the acceptance to the merger's own predicate. The briefing says so
   in those terms (the stale-tip/uncommitted shape), rather than repeating a
   claim the measurement contradicts.
2. The **polarity** of the self-check is what has to be right, and the payload
   now carries the merger's polarity. `--is-ancestor <trunk> <branch>` exits 0
   exactly when `merge --ff-only <branch>` (run from the trunk) succeeds; the
   inverted form exits 0 precisely when the branch is *not* ff-able, so a worker
   who wrote it backwards would read a successful repair as a failure. The
   regression test refuses to settle for a vacuous check: on the parked fixture
   both polarities fail, so the polarity assertion runs **after** the repair,
   where they disagree.

## Regression tests (`internal/tools/repo_sweep_park_worktree_test.go`)

| test | property |
|---|---|
| `TestRepoSweepParkReleasesWorktree` | one park = ref on the branch tip + **exactly one** row + the registered worktree **gone from disk**, with the branch ref preserved |
| `TestRepoSweepParkedBranchIsOutOfTheMergeSet` | after the park the branch is out of `repoSweepUnprotectedBranches`' view, and the exclusion it is out by is the archive tag (`repoSweepArchiveExempt` — production's own predicate) |
| `TestRepoSweepWorktreeTrapIsWhyTheStepExists` | the red half: parked + checkout held ⇒ out (for the hidden reason); tag dropped + checkout gone ⇒ candidate again, so the park is reversible |
| `TestParkPayloadCommandsFollowedLiterallyYieldFFAbleBranch` | **D2**: the briefing's commands, run verbatim (from the payload's own builders, in a shell), leave the branch ff-able; the polarity is red-proofed; and the next tick **lands** it |
| `TestRepoSweepDrainReleasesAlreadyParkedWorktree` | the drain arm releases an already-parked branch's leftover checkout **through the tick** (`ParkDrained`), files nothing, keeps ref + tag, and is a silent no-op on a second pass |
| `TestRepoSweepParkSummaryCapIsNotADuplicateRowFactory` | the ledger's 200-rune summary cap does not turn the dedup probe into a duplicate-row factory: the row is found and a re-park is a duplicate |

The park primitive is driven directly in the first two tests for a documented
reason: the tick's own candidate scan drops a worktree-held branch (that IS the
trap), so the tick reaches such a branch through the drain arm — which is what
the fifth test pins.

### The 200-rune cap was making the dedup probe a duplicate-row factory

While asking why a park row could not be read back, the same cap turned out to
have bitten the **writer's own probe**. The ledger caps a task summary at
`taskSummaryMaxRunes = 200` (`sanitizeTaskSummary`), and the park's dedup probe
(`repoSweepParkTaskLive`) compared summaries with **exact equality**. On a repo
path long enough for the clip — the suite's own scratch paths are, and so is a
real workspace — the STORED summary is a *prefix* of the derived one, so the
probe never matched its own row and **every tick re-filed the park**: a
duplicate-row factory, which is precisely what the probe exists to prevent.

Fix: the probe matches the summary's **identity head** (`parkTaskSummaryRowIdentityFmt`
= branch + repo, i.e. the part the format guarantees survives) with a byte-prefix
comparison — `substr(summary,1,length(?)) = ?`, deliberately **not** `LIKE`, since
a `%` or `_` inside a branch or repo path would become a wildcard and let a probe
for one branch match another. `repoSweepParkSummaryHeadOf` derives the head from
the full summary, so the caller keeps passing the same value the writer writes.

The regression test (`TestRepoSweepParkSummaryCapIsNotADuplicateRowFactory`)
**forces** the condition instead of hoping for it: it renames the fixture repo to
a computed path length and asserts BOTH halves — the full summary exceeds the cap
(so the clip really happens) and the head still fits inside it (so the row stays
identifiable). A fixture that quietly ran on an unclipped path would pass against
the defect.

### One shared reader fixed on the way

The ledger caps a task summary at `taskSummaryMaxRunes = 200`
(`sanitizeTaskSummary`). A park summary is
`resolve merge conflict for <branch> in <repo>, parked at archive/<branch>`, so
on a long repo path the `parked at archive/…` tail is **cut off** — and a reader
that matched the whole summary reported `sql.ErrNoRows` for a row that *was*
filed (this is how the cap was discovered: the D2 test could not read back the
payload the park had just written). The tag separator is now a single constant
(`parkTaskSummaryTagSep`) that both renders the full summary and locates the head
inside it, the shared head is `parkTaskSummaryHeadFmt`, and every row reader
matches on the identity rather than on the clipped whole. Without this, a test
would have reported "no park row at all" for a correctly filed park — a silent
failure in the test itself, and Rule 15's exact shape.

## Verification

See the task report for the full command/output transcript. Summary:

- `go build ./...` — clean.
- `go test ./internal/tools/ -count=1` — see the report (whole package).
- `go test ./cmd/agent/ -run 'TestMergerTickSpawnsNothingAndWaitsOnNothing'` —
  the C3 no-child gate: **green**, unchanged (the tick still spawns nothing,
  waits on nothing and runs no LLM; the third step is two bounded git calls).
- `go test ./internal/standards/` — green.
- **Live before/after** (`devdoc/demo-park-worktree-release.sh`): the `before`
  binary prints `protected=1` and leaves the worktree on disk; the `after`
  binary prints
  `branch feat/trapped (park-drained): archive/feat/trapped released — the parked branch is out of the merge set AND out of the worktree-protected set`,
  the worktree is **gone from `git worktree list`**, the branch ref and the park
  tag survive, and **no ledger row** is filed by the drain.
- **Defect shape observed on the live host**:
  `docs/requirements-task-ledger` is worktree-held at
  `/home/user/workspace/gopi-req-ledger` *and* carries
  `archive/docs/requirements-task-ledger`, with a **pending** park row
  (`01a0ab70`) — the D1 trap, live.

## The drain arm's data-loss hazard (investigated, dispatched, NOT solved here)

An already-parked branch can have a worker mid-repair in its worktree. The drain
arm removes that checkout with `--force`, and the worker's *extraction* then
fails (~42 occurrences of that pattern were reachable historically). This is a
**real residual of this change** and it is stated rather than hidden.

Why it is not narrow-guarded here: a dirty-tree guard would regress D1's win
(the 2026-09-16 trapped branches were dirty), and a live-holder guard is an
ownership model the daemon must not grow (C3; Rule 29's one-task-one-worktree
discipline is the operator's side of it). The repository already has the right
machinery in `internal/tools/dirty_holder_salvage.go` (dead-holder patch +
untracked capture) and the residue-reclamation `/tmp` fallback.

Dispatched as its own task: **`01a0abfc-3e3f-7923-9355-1010f117d1c7`** (tags
`bug`, `merger`, `worktree`, `data-loss`) — Rule 26: an incomplete fix is
repaired by dispatching again, and a fix that overstates its coverage closes the
ticket while hiding the defect.

## Rule 23 note — the pre-existing branch

`remotes/peerbox/fix/merger-park-worktree-release` (10 commits, `ccf62985` +
follow-ups, its ledger row reads `completed`) was **read, not landed and not
committed onto**: `git log master..<branch>`, `git diff master...<branch>`, and
`git show <branch>:<file>` were used to understand the earlier attempt, and the
implementation here was written fresh on `fix/merger-park-worktree` from
`master`. The earlier branch is 10 commits behind a moving master and carries
extra arms (a `cli_tasks.go` log path, red-proof fixtures, its own demo scripts)
whose behaviour was re-derived rather than copied — including the D2 correction
above, which that branch's payload does not carry.
