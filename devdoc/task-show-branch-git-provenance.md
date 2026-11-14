<!-- doc-class: historical -->
# devdoc: `gopi task show --branch` when the LEDGER has no row — the git half

**Status:** implemented. **Branch:** `fix/task-show-branch-git-provenance`.
**Origin:** task `01a0cc9b-8d22-7da2-88df-4d68e88ddcc7` (tag `bug`, source human).

## The report

```
$ gopi task show --branch fix/memory-escape-decode
gopi task show error: task: no ledger row names branch "fix/memory-escape-decode" — …
[exit 1]
```

The operator's question: *"basic function of 'gopi task show --branch …' not
working at all, how can this is done without basic testing?"*

## What was actually true (measured, not assumed)

| axis | reading |
|---|---|
| git | the branch EXISTS — tip `ccdd5f1e`, checked out in the linked worktree `/home/user/workspace/gopi-memescape`, which held uncommitted work (`M internal/tools/memory.go`, `?? internal/tools/memory_escape.go`) |
| ledger | `0` of **1019** rows carrying a branch named it (1021 by the fix's live run — the ledger grows) (`sqlite3 ~/.gopi/memory.db`) — the row id never entered the ledger at all |

Both halves were correct and only one was stated. The verb answered "no owner"
— which is exactly what it should say about the *ledger* — but the way it said
it ("no ledger row names branch X", exit 1, nothing else) reads to the operator
standing in that worktree as **"this branch does not exist"**.

## Why the ledger legitimately had no row

This is not a lost record — it is the documented boundary of the whole
branch↔task linkage epic (`devdoc/branch-task-linkage.md` §2): a row records its
deliverable only when the run goes **through the dispatch path** (claim-time
stamp / creation seam / terminal writeback). A worktree an operator or another
agent creates **by hand** never passes through any of those seams, so it has no
row — and per the epic's own §7 item [5/5], the lookup "should say *which*
channel answered, and … must not be silently merged into a ledger answer".

So the defect is not "the ledger is wrong". It is that the selector's PRIMARY
use case — *"I am standing in a worktree; which row is this?"* — collapses into
a refusal that is indistinguishable from "the branch does not exist", for
exactly the work the linkage epic exists to make visible.

## What shipped

**`internal/tools/task_deliverable_git.go` (new)** — the git half:

```go
func DeliverableGitProvenanceAnswer(ctx context.Context, sel DeliverableSelector, res DeliverableResolution) string
```

It renders a **labelled** block:

```
git provenance (read from the git repository now — NOT recorded in the ledger):
  branch "fix/memory-escape-decode" EXISTS in /home/user/work/GoPi at ccdd5f1
  checked out at /home/user/workspace/gopi-memescape
  that worktree HAS uncommitted work: M internal/tools/memory.go | ?? internal/tools/memory_escape.go | …
  ledger: the ledger was read: 1021 row(s) carry a branch and none names branch "fix/memory-escape-decode"
```

**`internal/tools/task_deliverable_selector.go`** — `DeliverableResolution` now
carries `Readable` + `RowsWithBranch` out of the resolver even when it resolves
no row (so the ledger half is *stated*, never guessed), and the no-owner failure
is classified by a sentinel, `ErrDeliverableNoOwner`, so a caller can tell it
from a usage error or an unreadable ledger **without matching on prose**.

**`cmd/agent/cli_tasks_show.go`** — on `ErrDeliverableNoOwner` the verb returns
the provenance block *as well as* the error.

**`cmd/agent/cli_tasks.go`** — the show case prints a non-empty result before
reporting an error, so the block reaches stdout while the exit code stays 1.

## The five design decisions worth keeping

### 1. The exit code is NOT weakened

Nothing was resolved, so the command still fails: exit 1, the same refusal on
stderr, byte-identical to before. A script that gates on `$?` sees the same
answer it always did. The change is *additive*: the row question still fails,
and the operator additionally gets the fact the failure was hiding.

### 2. The two channels are labelled, never merged

`TaskGitProvenanceLabel` opens the block and says, in the operator's own reading
order, that these lines come from git **now** and are **not** a ledger record.
The ledger half follows as its own `ledger:` line. This is
`branch-task-linkage.md` §7 [5/5] enforced in the output shape: a git-only
answer is never dressed as a ledger answer.

### 3. "No owner" and "unreadable ledger" stay distinguishable

`res.Readable == false` renders *"the ledger could NOT be read, so whether a row
claims it is UNKNOWN"* — never "no row names it". Collapsing the two would turn
a broken read into a missing dispatch, which is the one mistake that would make
the operator re-dispatch work that already exists.

### 4. The git half REUSES the existing readers (Rules 13/14)

No new git spelling was invented:

- ref presence → `repoSweepRefPresent` (the sweep's own predicate, which already
  separates git's named absence from an inconclusive probe);
- tip → `repoSweepReadTip`;
- checkout → `finishCleanupBranchWorktree` (the ONE `git worktree list
  --porcelain` reader);
- dirt → `AdoptionGitStatus`;
- runner → `repoSweepGit` (one timeout, one error shape).

A second spelling of any of them would be free to disagree with the sweep about
the same repository.

### 5. A linked worktree is not a second repository

**Found by running the real command** (Rule 31), not by reasoning: candidates
come from the ledger's `deliverable.repo`, the declared `repos` table, and the
process's working directory. The operator's own task worktree is a *linked
worktree of the same repository*, so the first live run printed *"2 declared
repositories contain …"* about **one** repository — a phantom ambiguity.

Fixed by `taskGitRepoIdentityKey` (`git rev-parse --git-common-dir`, the `.git`
directory every checkout of a repository shares) collapsing same-repository
paths to one candidate, **and reporting the collapse as a note** so it is
auditable rather than silent. The collapse is real work, not cosmetics: without
it, every task worktree added inside a listed repo would look like an ambiguous
second repo.

## Verification

Rule 31 — the operator's own invocation, against the live ledger:

```
$ ./bin/gopi task show --branch fix/memory-escape-decode
git provenance (read from the git repository now — NOT recorded in the ledger):
  branch "fix/memory-escape-decode" EXISTS in /home/user/work/GoPi at ccdd5f1
  checked out at /home/user/workspace/gopi-memescape
  that worktree HAS uncommitted work: M internal/tools/memory.go | ?? internal/tools/memory_escape.go | ?? internal/tools/memory_escape_test.go
  ledger: the ledger was read: 1021 row(s) carry a branch and none names branch "fix/memory-escape-decode"
  note: /home/user/workspace/gopi-showprovenance is the SAME repository as /home/user/work/GoPi — one repository, reached by two paths, read once
[exit 1, the unchanged refusal on stderr]
```

The negative controls (a branch nobody has; a resolved selector) and the test
inventory are in `tasks/01a0cc9b-8d22-7da2-88df-4d68e88ddcc7-report.md`.

## The "how can this be done without basic testing" half — answered honestly

The feature's own report (`tasks/<id>-report.md`, feature commit `f922b184`)
DID run the real binary — but every case it ran was on a selector the ledger
already knew (the author's own row). "No ledger row names it" was tested only at
the **unit** level, where the assertion was *"this must be an error that names
the selector"* — which it was, and still is. Nothing asked what the OPERATOR
gets, so nothing caught that the answer for a hand-made worktree is a refusal
that reads like a wrong answer.

The gap was therefore not a missing `go test ./...`; it was a missing **case in
the invocation matrix**: the same face, with the axis (ledger-known /
git-only / nowhere) crossed instead of only the first value exercised. The
regression tests added here are that matrix, and the one that matters —
`TestTaskShowBranchNoRowAnswersWithLedgerAndGitFacts` — reproduces the operator's
situation end to end.
