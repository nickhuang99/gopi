<!-- doc-class: historical -->
# The archived set, read side: reason-less and NOT-LANDED as named defects

Task `01a131e0-ddb9-76a0-be7e-e4f3b2dc0a5b`. Measured on hosta's real repository
`/home/user/work/GoPi` (137 archived branches). Supersedes nothing; it is the
READ half of the archive-why family whose WRITE half landed earlier
(`devdoc/archive-why-gate-recovery.md`).

## The defect, in one sentence

The project had a gate that refused to bury a finished branch without a reason,
and no reader that reported a branch buried WITH one already missing — so the
fact was in the repository, was rendered by a surface, and was not counted,
classified or actionable by anything.

The operator's own framing of the consequence: *"很多重要的bug就这样子被埋没了，
变成反复提"*. The count they worked from (231 of 235 reason-less) they had to
GREP out of the raw listing themselves, which is the tell: the surface knew the
fact and did not report it.

## What was already there (measured on this tree, before any edit)

Saying this plainly matters, because the epic's own framing ("规则未被强制") is
only half true and the other half is what this task actually closed:

| half | state before this task |
|---|---|
| one writer | `ArchiveTagReasoned` writes `refs/tags/archive/<branch>` AND the companion `refs/gopi/archive-reason/<branch>`. Lightweight tag, deliberately (an annotated tag is not a commit, and repo-wide readers treat the ref as a commit). |
| sweep-side refusal | `repoSweepArchiveBranch` applies `archiveOwnerDecided` and refuses to bury a `completed`/`verified` row's branch without an explicit override. |
| mint gate | `internal/standards/archive_mint_gate.go` fails the build on a bare `git tag`-shaped archive mint. |
| backlog check | `ArchiveReasonlessBacklog` — **report-only**, and **narrowed to decided owners**. |
| render | `gopi merge --status` renders every archived entry with a NEUTRAL `no reason recorded` label and no count. |

So the rule was enforced at the write seam. The hole was the read seam, and it
had two shapes.

## Hole ①: the render never counted and never judged

`gopi merge --status` printed

```
  137 branch(es) tagged out of the merge queue (buried: 1)
  01a113e467767eb9a4c33cb1df1cdfbb-20261007 — (none) (unknown) — no reason recorded
```

The same words `no reason recorded` appeared on an entry archived correctly with
no reason recorded by hand and on one a lane retired without one, and nothing in
the line said which was which — nor HOW MANY there were.

**Fix**, no extra git call (the field was already read):

```
  137 branch(es) tagged out of the merge queue (buried: 1, no reason recorded: 133)
  REASON-MISSING 01a113e467767eb9a4c33cb1df1cdfbb-20261007 — (none) (unknown) — no reason recorded
```

Two deliberate rendering decisions, both measured against the real output:

- **The count is APPENDED to the existing header, never folded in.** The
  substring `(buried: N)` keeps its exact shipped spelling, and the whole
  137-line body is byte-identical to the pre-change output once the
  `REASON-MISSING ` prefix is stripped (verified by `diff` on the two real runs).
  A rendering change that rewrites lines nobody asked about makes every earlier
  report's quotation of this output unverifiable.
- **The two markers are MUTUALLY EXCLUSIVE.** A reason-less entry that is also
  `BURIED` keeps `BURIED ` and nothing else: its owner row already states the
  stronger defect, and `BURIED` lines stay byte-for-byte what they were. The
  reason-less entry that is NOT buried gets `REASON-MISSING `.

## Hole ②: the backlog check could not see most of the corpus

`ArchiveReasonlessBacklog` deliberately narrows to `archiveOwnerDecided` rows
(its own doc gives the reason, which is sound: a branch whose owner says
failed/cancelled is legible from the row). The measured consequence is the rest
of the corpus being reported by NOTHING — and it is the LARGER part:

```
137 branch(es): 133 reason-less, 0 decided-owner, 6 not-landed, 0 decided-and-not-landed
```

On this repo **not one** archived branch has a decided owner row under the
`deliverable.branch` axis, so the narrow check reported `clean` while 133 entries
sat there unclassified. On the peer (peerbox) the same class was measured at 108.

**Fix**: `ArchiveBacklogClassReport` (`gopi task archive --check`, second
section) classifies EVERY archived branch on three axes —

```
reason (recorded / absent)
  × owner row (decided / open / none)
  × landing (landed / NOT LANDED / un-measured)
```

— and pulls out the set an operator acts on: **`decided owner × NOT LANDED`**.

### Why the landing axis, and which instrument

An owner row saying `completed` is a statement about the LEDGER. Whether the work
is reachable is a statement about the REPOSITORY, and the two faces rendered them
as one. The landing axis is what separates "finished and landed" from "finished
and nowhere".

The instrument is chosen against three measured false-positive modes
(`devdoc/archive-tag-sweep-88-20260916.md`, memory #4972):

| instrument | verdict |
|---|---|
| `rev-list` / "commits ahead of master" | labels almost EVERY archive tag — an archive tag is by construction a branch that never merged |
| `git cherry` / patch-ids | a rebase or squash lands the same patch under a NEW hash and reads "+" (unlanded) |
| a NAME test ("does master define this symbol?") | reads UNLANDED whenever the file that held it was DELETED on master. Called 42 rows unlanded; path presence cut that to 33, a blob check to 6 |
| **PATH PRESENCE** ✅ | the paths the branch ADDED, tested for presence on the integration ref |

Order of the two probes, cheapest first:

1. `git merge-base --is-ancestor <tag tip> <ref>` — one call per entry. Exit 0 =
   `landed`, final.
2. only for the misses, ONE `git log --no-walk --diff-filter=A --name-only`
   for the WHOLE miss-set (not one per entry — 137 branches would otherwise be
   137 subprocesses), then `git cat-file -e <ref>:<path>` per added path. No
   absent path = the work landed under a different commit = `landed`.

**A nil probe answers `unknown`, never `missing`.** The two directions are not
symmetrical: folding `unknown` into `missing` manufactures a re-dispatch for a
branch nobody measured; folding `missing` into `unknown` hides the defect. The
first is a false accusation and the second is the silent green Rule 15 forbids —
so the verdict is TRI-state and the third value is printed as `land-unmeasured`.

### The integration ref is not spelled `master`

The question is asked of `IntegrationClaimTargetRef(repo)`, which prefers the
delivery lane's forced mirror when it is strictly ahead of the local branch
(`internal/tools/integration_claim_target.go`). Measured on this repo the
resolver answers `refs/remotes/gitPushcheck/master`, and the report prints the
ref it used: asking a STALE local `master` returns "not landed" for work that
landed on the peer, which is exactly the false negative that cost a whole run in
`devdoc/commit-path-ancestry-gate.md`.

### Measured result on the real repo

```
137 branch(es): 133 reason-less, 0 decided-owner, 6 not-landed, 0 decided-and-not-landed
  ref for the landing question: refs/remotes/gitPushcheck/master
```

All 137 rows are classified — including the ~130 that NO surface had ever
reported, because they have no owner row (`(none)`). Runs in **2.0s**.

### Cost discipline (Rule 27)

`gopi merge --status` runs on every merger tick, so the landing instrument is NOT
wired into it. It lives only in the explicit `gopi task archive --check` face.
The render fix (hole ①) costs ZERO extra git calls because it re-reads fields the
row already carries.

## BURIED — the semantics, in writing (the epic asked for a definite answer)

**Who writes it.** Nobody. `BURIED` is not a stored field, a tag, a ref, a
ledger status or a database column. It is a MARKER the render adds.

**On what predicate.** `archiveOwnerDecided(ownerStatus)` — the owner row's
status is exactly `completed` or `verified` (`internal/tools/archive_reason.go`).
The SAME predicate is read by three sites, so they cannot disagree about what
"buried" means (Rule 13): the sweep-side refusal gate (`repoSweepArchiveBranch`),
`ArchiveReasonlessBacklog`, and the render's counting loop.

**Where it is rendered.** Two faces, one predicate:
- `gopi merge --status` → `BURIED ` prefix on the entry, and the buried count in
  the header.
- `Repo…DescribeArchived` (the merger's sweep line) → `(BURIED: N [names])`.

**Can a buried terminal row be re-dispatched? Yes — and it needs no permission
from the ledger.** A row whose status is `completed`/`verified` is terminal for
its own run; it is NOT a claim on the branch and NOT a fence against new work.
`internal/tools/task_for_branch.go` states the policy: a LIVE row outranks a
terminal one, and "the archive face's preference for the decided row is the
opposite question … and stays where it is". A `pending` row that names the same
branch is claimable immediately.

**The revival path is exactly ONE, and it does not touch the tag's commit:**

```bash
git tag -d archive/<branch>      # re-arms the branch; the tag is the decision record
```

Removing the archive tag is what un-arms a branch for a future merge — the
namespace's documented contract (`archive_reason.go`), and the same single
command `task_merge.go`'s build-gate refusal spells as element 6 ("REVIVAL").
Two routes to the buried work, in the order that actually works:

1. **File a new task** against the buried work and build it anew off current
   master. This is the route that works, because the branch failed a gate once
   and will fail it again unchanged.
2. **Re-arm the old branch** with `git tag -d` — the branch returns to the merge
   queue and will be refused again unless it also builds.

**What is NOT a revival path**: editing the archived branch in place, pushing a
"regenerate" commit onto it, or deleting/rewriting the `archive/*` tag or its
`refs/gopi/archive-reason/*` record. The tag is the DECISION and the reason ref
is its WHY; both are evidence and neither is garbage (Rule 9/23).

## A measured divergence this task did NOT fix (filed as its own bug)

> **Retired mechanism (merge verb removal, 2026-11-01).** The `gopi merge --status`
> line below is the RECORD of what this run typed, kept verbatim; the top-level
> verb was removed afterwards and answers **exit 1** today (`gopi task merge` is the
> surviving family spelling, `man/gopi-merge.md`). The divergence measured here is
> in the ledger reads, which is unchanged.

Found by running the two faces against the same ledger, which is why Rule 31's
"execute the surface you changed" matters — the two reads disagree on the SAME
branch:

```
$ gopi merge --status
  BURIED base-peermaster-01a12b54 — 01a12b54-e636-796f-a97e-5cfccd3ebb06 (completed) — unowned

$ gopi task archive --check
    base-peermaster-01a12b54 — unowned — (none) — landed
```

Root cause, measured to the field: `merge --status` resolves an owner through
`TaskForBranch` (+ `TaskForBranchResolvedRow`), whose matching axis includes
`deliverable.grounds`; the archive namespace matches `deliverable.branch` ONLY
(`archiveReclaimAllOwners`' `LIKE '%"branch":"%'` + `deliverableBranchOf`). That
row's `deliverable.branch` is `task/01a12b54e636796fa97e5cfccd3ebb06` — a
different name entirely — while `base-peermaster-01a12b54` occurs only in
`deliverable.grounds`. So one resolver recognises the branch and the other does
not, and "is this branch buried?" depends on which face you ask.

That is a difference in the MEMBERSHIP of the owner candidate set, not the
documented difference in ORDERING preference. Filed as task
`01a13214-1d89-7127-8391-fa7a97fb89c0` with the raw output attached; fixing it
would change the input BURIED and the gates read, which is not this task's scope.

## Files

- `internal/tools/archive_landed.go` — the landing instrument (path presence,
  bulk read, tri-state verdict).
- `internal/tools/archive_backlog_classify.go` — the classification, the
  re-dispatchable set, the report.
- `internal/tools/archive_landed_test.go` — the three arms + the two asymmetry
  controls.
- `cmd/agent/merge_run.go` — the render fix (count + defect marker) and the
  `gopi merge -h` BURIED/re-dispatch paragraph.
- `cmd/agent/cli_archive.go` — `--check` runs both sections; usage updated.
- `cmd/agent/cli_archive_test.go` — the marker/byte-identity contracts.
