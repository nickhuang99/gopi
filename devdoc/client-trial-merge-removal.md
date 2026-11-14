<!-- doc-class: historical -->
# The client stopped trial-merging — the branch's own tests, then the push

Task: `01a130a12efc7369b5239f88e3a7f0d0` · branch `task/01a130a12efc7369b5239f88e3a7f0d0`

## The measurement that removed it (2026-10-12)

Three FINISHED branches were refused by `finish-cleanup --apply` in ONE batch:

| branch | how the trial-merge lane refused it |
|---|---|
| `01a1301b…` | a **published-anchor** gate, over a `COMMANDS.md` anchor that was **not inside its own diff** |
| `01a13019…` | 834 s, then refused — **no FAIL line anywhere** in its output |
| `01a1300b…` | 838 s, then refused — same, no FAIL line |

The same three were pushed **by hand in about a minute each**, and all three merged
cleanly. The lane was not measuring the delivery; it was re-deciding a question the
server already owns, and it was getting the answer wrong on branches that were fine.

## What the client was doing, and why every part of it was the server's

The close-out chain (`runFinishCleanupChainTarget`) fetched the peer's master,
merged it into the delivered branch locally, rebuilt the merged tree through the
declared build, ran a branch-scoped test run **on the merged tree**, ran a
published-anchor gate over the discovered edge, and pushed only if all of that
passed. Four acts, each of them a *second* decision about a state that belongs to
the merge:

- **the merge itself** — the merger performs it on the integration branch, with the
  real master in front of it, once. A client merging a *mirror* of that master is
  approximating the merger's act on a snapshot that is stale by the time it lands.
- **the composed tree's soundness** — a verdict about two branches together, which
  neither author's session can act on and which the merger can state once, for all
  of them, instead of N clients each paying minutes of CPU to guess it.
- **the published-anchor edge** — it was asked against a tree assembled locally, and
  it refused a branch whose diff did not contain the anchor it tripped on. The gate
  is worth having; it was in the wrong place, and it is now the merger's.
- **the skew** — a branch BEHIND the integration branch is not a defect. It was
  treated as one.

## What replaced it

Nothing, for those four. One thing survived, and it got **better**, because it is
now asked where it is decisive rather than where it is expensive:

**`finishCleanupBranchCheckVerify`** (`internal/tools/task_finish_premerge.go`) runs
the branch's **own** tests, in the branch's **own** worktree, at the branch's **own**
revision:

- packages: `smoke.TouchedPackages(<ground>, <integration ref>, HEAD)` — the branch's
  own diff, the question its author can act on;
- command: `smokegate.ScopedForPackages(...)` → `make test-budget PKGS='…'` — the
  declared scoped target, never the whole-tree run (Rule 27);
- base: a **local** ref, so nothing is fetched and nothing is merged — the lane reads
  the integration branch's objects and never writes to the branch.

It is recorded, **not blocking**. Its verdict lands on the row's own trail under one
of three names (`branch-checks-verified` / `branch-checks-unverified` /
`branch-checks-refused`), and the delivery proceeds: whether the branch merges, and
whether the composed tree is sound, is decided once by the merger.

The check exists because **`go build` compiles no `_test.go`** — the incident behind
it (`01a12ca0`: the shared master carried two red chat fixtures for ≈ 2 d 9.5 h) is
still standing, and a build cannot see it. What changed is *where* it looks.

## What the plan face now says, and why it had to change

`finish-cleanup` (dry run) is the operator's decision surface. It used to predict a
sequence — `git fetch <peer> → git merge <master> into <branch> → declared build →
push only if both succeed` — and it was already found to be under-reporting the gate
it actually ran (task `01a13017`, measured after `01a12fe6`). A plan that renders a
step the execution no longer performs is worse than no prediction: an operator reads
"would run" as "this is what happens". So the prediction was rewritten to the act
that remains, **plus its own limits** — stated in the same breath rather than left
for a reader to infer from silence: whether a directory holding the delivered
revision can be produced at all (an abstention, recorded), whether a branch behind
the integration branch is delivered anyway (it is), and which verdict the branch's
own test run returns (that verdict is the lane's, never a forecast).

## Not deleted, deliberately: the merge-check runner

`mergeCheck` / `mergeCheckOutcome` / `finishCleanupChecksIn` and the output
machinery live in `task_finish_premerge.go`. They were the trial merge's engine, and
the branch-check lane now runs on exactly that engine — one runner, one bounded
capture, one failure-extraction rule (a `--- FAIL` block for `go test`, the head for
a compiler), so the two cannot drift. `merge_build_gate.go` was **never** the trial
merge's alone: the merger's own build gate uses it, and it was left untouched.

## The naming that survives, and why it is not cosmetic debt

The `finishCleanupPreMerge*` prefix on the **spelling constants** ("rev-parse",
"--verify", "--quiet", "HEAD", "--abbrev-ref", "merge-base", "--is-ancestor") is a
vestige, kept on purpose: those strings have their ONE definition there and are
referenced from five sibling files, and Rule 14 says a shared value has exactly one
definition — moving them would churn five files for a name. What is **not** vestigial
is anything that describes a step: the step name (`branch-checks`), the markers and
the three event names say what this lane does *now*, and a report must never keep a
name whose act no longer happens.

## Files

| file | fate |
|---|---|
| `internal/tools/task_finish_gendoc.go` (+ its test) | **deleted** — the published-anchor gate, whose act was the server's |
| `internal/tools/task_finish_deliverysync.go` (+ its test) | **deleted** — the delivery-point alignment (fetch + merge + build) |
| `internal/tools/task_finish_premerge.go` | **rewritten** — the branch's own check, the runner, the trail |
| `internal/tools/task_finish_premerge_smoke.go` | **rewritten** — the branch-scoped selection and command |
| `internal/tools/task_finish_docclass.go` | **new file**, the classification step extracted whole from the deleted sync so it outlived it |
| `internal/tools/task_finish_cleanup.go` | the chain's step order, the plan's predictions, the audit standing |
| `internal/tools/task_finish_fixture_helpers_test.go` | **new** — the git fixtures that outlived the two deleted test files |
| `CONFIG.md` | the knob row re-pointed (`GOPI_FINISH_CLEANUP_BRANCH_CHECKS`) |
