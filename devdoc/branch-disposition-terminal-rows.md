<!-- doc-class: historical -->
# devdoc: a terminal row's branch gets a DISPOSITION (`gopi task orphans`)

**Branch:** `task/01a1494d5d487ecfa25b12477ad1f28a`
**Epic:** 01a1494d — "rework 行被 cancelled 后其分支既不合并也不归档也不删除——永久残留"

## The defect, restated as a state machine hole

A branch whose merge conflicts makes the system dispatch a `rework` row. The
rework row is then **cancelled**. After that moment the branch has no owner and
no disposition anywhere:

* not merged — the conflict is the reason the rework row existed;
* not archived — no `refs/tags/archive/<branch>`, so no decision is recorded;
* not deletable — `git branch -d` refuses an unmerged branch, by design.

The three cleanup mechanisms each look at a *different* population and the
branch is in none of them:

| mechanism | population it acts on | why a cancelled rework branch is missed |
|---|---|---|
| the merger | branches the ledger row says are live/delivered | the row is terminal, so no lane claims it |
| `finish-cleanup` | rows that END with a delivery (push + clean worktree) | a cancelled row never reaches that chain |
| `gopi task archive` | branches an operator names, or a merge gate refuses | nothing invokes it for a cancelled row |

So the branch stays in `git branch` forever and **no read-only surface prints
it**. That "no surface prints it" half is the part this change fixes: a defect
that is invisible cannot be counted, watched, or decided on.

## What was built

`internal/tools/branch_disposition.go` computes, for every **terminal** row
(`cancelled` / `failed` / `superseded` — the statuses meaning "nobody will ever
finish this work") that the existing orphan readout already loads, ONE of five
dispositions:

| disposition | meaning | resolving command printed |
|---|---|---|
| `merged` | `git rev-list --count <integration>..<branch>` == 0 | none (nothing owed) |
| `archive-tagged` | an `archive/<branch>` tag exists — the decision is recorded, the tip preserved | `gopi task archive --reclaim` |
| **`residue`** | the branch exists, carries commits the integration branch lacks, and no archive tag records a decision | `gopi task archive <branch> --reason <code>` |
| `absent` | the row is terminal and the ref is gone (a delivered branch is deleted after it lands) | none (see the row's own verdict) |
| `unjudged` | the question could not be asked (unreadable archive namespace, git refused the count) | none — with the reason stated |

`residue` is the defect class. The measured failure mode before this change is
that the same branch rendered as *nothing at all*.

### Why this seam and not a new one (Rule 3 / Rule 35)

`gopi task orphans` is the one existing read-only surface that already:
loads exactly this row population (terminal rows carrying a deliverable
branch), reads the local ref set, reads the archive namespace, renders one line
per row with a next step. A second verb would be a second ledger scan, a second
render, and a second place for two answers to disagree. Nothing new persists: no
state file, no daemon, no sweep, no ledger write, no config knob.

`gopi task list` was **rejected** deliberately: it is the surface every
dispatcher reads and it is ledger-only by design (no git probes). A
`rev-list` per row there would cost a git process per row on the queue view.

### Cost (Rule 27 — no load generation)

One `rev-list --count` per LIVE terminal branch, plus ONE archive-namespace
read per **repository** (cached through `branchDispositionMeasurers`). The
population is bounded by "terminal rows whose branch still exists here" — on the
live ledger 15 rows, of which 1 is residue. Non-residue rows print a bounded
tail (`branchDispositionRowLimit = 40`) so the block cannot turn into a second
copy of the ledger; **the residue class is never truncated**.

### Rules obeyed

* **Rule 9** — the only resolving command ever printed is `gopi task archive`
  (the one writer). The reader never suggests `git branch -D`; an unmerged tip is
  not something a reader may decide.
* **Rule 1b/13/14** — one spelling per fact: `IntegrationClaimTargetRef` (the ONE
  integration-ref resolver), `integrationClaimCountBetween` (the ONE
  `rev-list --count` helper), `ArchiveEntriesForRepo` (the ONE archive namespace
  reader), `CanonicalBranchRetirementSpec` (the ONE branch-spelling normalizer),
  `ArchiveReasonHandMistake` (the suggested reason code, from the closed
  vocabulary). Disposition values and the exit code are named constants.
* **the reason code is a PLACEHOLDER** — a reason is a claim about *why* the
  branch is retired, and the reader knows only that the row is over. Printing a
  concrete code would put the reader's guess into the append-only tag
  `refs/gopi/archive-reason/<branch>`, where a later reviewer reads it as the
  record. The line names the command with `<code>`; the closed vocabulary is
  `gopi task archive -h`.
* **Rule 15** — a question that could not be asked is `unjudged` with its reason,
  never `merged` and never `residue`. An unreadable archive namespace does NOT
  become "no archive tag".
* **Rule 31** — the CLI was run for real, against a real ledger and a real repo
  (see the report's `## Verification`).

## The live measurement (2026-10-17)

`./bin/gopi task orphans --repo /home/user/work/GoPi` on **hosta**:

```
DISPOSITION of terminal rows' branches — every row below is over
(cancelled/failed/superseded); a branch that still exists and is neither merged
nor archive-tagged is RESIDUE and has no owner
  15 terminal row(s): 1 residue, 0 archive-tagged, 0 merged, 14 absent, 0 unjudged
  01a14774-4662-745f-bdf2-edb21cf4bb83  failed  task/01a147744662745fbdf2edb21cf4bb83  [residue]
    this row is over and the branch still carries 1 commit(s) that are on
    refs/heads/master NOWHERE, with no archive tag recording a decision
    resolve: gopi task archive task/01a147744662745fbdf2edb21cf4bb83 --reason <code>
```

Exit code **7** (`TaskBranchRefsExitResidue`).

The same binary run on **peerbox**'s ledger (233 terminal rows) reported
`0 residue, 1 merged, 232 absent` — the branches were already consumed there.
That is the honest cross-check: the disposition class is a MEASUREMENT of the
repository it is pointed at, not a status word.

## The 17 historical cancelled `rework` rows

Read-only disposition list, measured on BOTH hosts
(`artifacts/rework17-hosta.txt`, `artifacts/rework17-peerbox.txt`):

**All 17 are `ABSENT` — no local ref and no archive tag, on hosta AND on peerbox.**

That is not a disposition. It is the answer "there is nothing left to dispose
of, and no record says what happened to it". Two consequences a reviewer must
see:

1. **The residue population the epic pictured is already gone from these hosts.**
   The branches the briefing showed in `git branch` were cleaned up between the
   filing (2026-10-17) and this run; today's live residue is a different row
   (`01a14774`, `failed`, ahead=1).
2. **The absence is unrecoverable from git.** `git log --all` names the FULL id
   of none of the 17 (only 8-hex short tails appear, which are ambiguous by
   construction — two rows submitted in one batch share the timestamp prefix).
   So the work of those 17 branches is recoverable only from the peerbox ledger's
   event stream, which is exactly the "recovery depended on a briefing" shape
   `task_ref_reachability.go` was built to remove, one question over.

**Deleting or auto-archiving any of this is an OPERATOR decision and is NOT done
here** (per the briefing's budget clause). What this change guarantees is that
the NEXT one cannot vanish silently: it reports `residue` with the command that
resolves it, and exits 7.

## Files

* `internal/tools/branch_disposition.go` (new, ~330 lines) — the classifier.
* `internal/tools/branch_disposition_test.go` (new) — the minimal repro + guard.
* `internal/tools/task_ref_reachability.go` — the row/report plumbing and the
  `DISPOSITION` render section.
* `internal/tools/task_branch_refs.go` — `TaskBranchRefsExitResidue = 7`.
* `cmd/agent/cli_tasks.go` — the `gopi task orphans` usage text now advertises
  the disposition half and exit 7.
