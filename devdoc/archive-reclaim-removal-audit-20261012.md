<!-- doc-class: historical -->
# The archive-reclaim branch deletion had no removal-audit record (2026-10-12)

Status: FIXED on branch `task/01a12de329b97921856d5e95db647a54`.
Probes: `internal/standards/removal_audit_gate_test.go`
(`TestRemovalAuditGateSequelVocabularyIsLoadBearing`, `EveryRemovalSiteEmits`,
`DataReport`), `internal/tools/archive_ref_reclaim_audit_test.go`
(`TestArchiveReclaimBranchDeleteIsAudited`).

## The observation that started it

`refs/heads` in `/home/user/work/GoPi` went from 16 to 5 between 09:41 and 10:00
CST 2026-10-12, and **not one** of the eleven had a `kind=branch-delete` line in
`~/.gopi/logs/removal-audit.log`. A delivered branch's name
(`task/01a12d22dac07bdea9f895926cdc64ea`) was among the eleven — the incident
that opened the investigation.

**The window the refs went in holds zero lines of any kind.** Records are
stamped UTC; the drop is 09:44–09:50 CST = 01:44–01:50Z, and that window
carries **0** records (log read at 10:24:56 CST, 16294 lines, mtime stamped
with the read). For scale on the same log: 3902 `branch-delete` records in
total, 96 of them stamped `2026-10-12`, 684 in the CST-day 2026-10-12.

**The eleven are named in that log, but never as a branch deletion.** Each of
the eleven appears in exactly **two** records, both of the *worktree* kinds —
`kind=worktree-ground` (arm `task-ground-anchor`) and `kind=worktree-removal`
(arm `task-self-own-sweep` / `task-self-reclaim`) — and **zero** records of
`kind=branch-delete` (per-name `grep -c` → 0 for all eleven; measured
2026-10-12 10:41 CST). Those worktree records are timestamped 2026-10-11
23:07Z–2026-10-12 00:02Z — the run's OWN teardown of its own ground, earlier
and unrelated to the 09:45 ref removal, so they neither audit it nor explain
it. The contrast stands on its own: elsewhere in that same log the worktree
lifecycle leaves a record, and the ref removal left none.

**Nothing was lost.** `git merge-base --is-ancestor <tagged tip> master` is
rc=0 for **all eleven** archived tips (`e4870cb8c`, `f0ecc06bc`, `1881e2b11`,
`df301b1a4`, `e5a2435f0`, `9c52546ad`, `c789c501d`, `3db654fb7`, `c3e5fc399`,
`76b5da36e`, `406a3ef4d`) — measured, not argued from the reason record, which
exists for only one of the eleven.

## What removed it — measured, not inferred

`gopi task archive <branch> --reason superseded`, run on this host at
**09:45:09 CST**, through its automatic reclaim leg:

```
gopi task archive <branch>            cmd/agent/cli_archive.go
  -> tools.ArchiveBranchCwd           ArchiveDecide (gate) -> ArchiveTagReasoned
       writes refs/tags/archive/<branch>     (the decision, durable)
  -> runArchiveReclaimAfterArchive    cli_archive.go:328
     -> tools.ArchiveReclaimDecidedRefs       archive_ref_reclaim.go
        -> archiveReclaimDelete -> `git update-ref -d refs/heads/<branch>`
```

Three refs on the repo carry the proof, and the third is why the owner is this
machine and not an external deleter:

| ref | content |
|---|---|
| `refs/gopi/archive-reason/task/01a12d22…` | `reason: superseded`, `repo: /home/user/work/GoPi`, `actor: user@host-HP-Laptop`, `at: 2026-10-12T09:45:09+08:00`, detail: the delivered tip is contained in peerbox/master `e07d222dd` (IN-MASTER measured) |
| `refs/tags/archive/task/01a12d22…` | lightweight, at `e4870cb8c` — the branch's own tip |
| `refs/heads/task/01a12d22…` | absent |

**The discriminator.** The reclaim arm refuses any branch with no archive tag
(guard ①) and any tag/head commit mismatch (guard ②). A foreign deletion —
a manual `git branch -D`, a peer, a `gc` — would leave the surviving tag at
some commit and no head to compare; it cannot leave the two refs this repo
holds in *agreement at the branch's own tip*. They agree exactly, so the ref was
removed by the arm that mints that tag. `git merge-base --is-ancestor e4870cb8c
master` → rc=0, so nothing was lost: the tip is in master.

The eleven are one batch: ten sibling tags carry the `-20261012` suffix this
repo assigned to hand-run mass-archive scripting on 2026-08-28, and the victim
carries this file's own suffix-less spelling. All eleven have a tag, none has a
`kind=branch-delete` record.

## Why there was no record (the defect, reproduced)

`archiveRefReclaim` deletes through `git update-ref -d`, not
`git branch -d/-D` — deliberately: git's merged-only check is the wrong question
for a branch the merge path will never take (see that file's header). And the
removal-audit census classified a removal site from git's porcelain pair alone:

```go
if has("branch") && (has("-d") || has("-D") || has("-r")) { return branch-delete }
```

So the arm's deletion was not classified as an **unrecorded** removal — it was
not classified **at all**. The gate that "makes every removal attributable" had
no opinion about the one arm in the tree whose removals left no trace, and
stayed green. Reproduced by running the real verb against a fixture repo
(`reclaim-probe2-…`): the ref was deleted, `audit.jsonl` was never created. The
pre-fix data report read `total=24 … kinds=map[branch-delete:14
worktree-removal:10]`, with the archive-reclaim site in no bucket.

**The trigger is the automatic leg.** `runArchiveReclaimAfterArchive` sweeps the
whole repo after every single `gopi task archive`, so one archive decision means
one silent ref removal per decided branch — which is exactly how eleven of them
went in one minute.

## The fix

Two ends, because either alone leaves the hole.

**Producer** (`internal/tools/archive_ref_reclaim.go`): `archiveReclaimDelete`
emits `removalaudit.BranchDeleteAt(ArmArchiveReclaimBranch, repo, branch, "",
tip, "", …)` **before** the `update-ref -d`, carrying the deleted tip. Emitting
first is the package's existing convention (a record written after the mutation
explains nothing about a crash inside it); carrying the tip is what lets a
reader answer "which commit left, and did it reach anywhere else?" — only the
caller can capture it, because a moment later the ref is gone.

**Census** (`internal/standards/removal_audit_gate_test.go`): the classification
gained the PLUMBING vocabulary, so a `update-ref -d` deletion is a
branch-delete site that must be covered. The verb and flag are
`removalaudit.RefDeleteVerb` / `RefDeleteFlag`, referenced by both the arm and
the census — one definition, so the spelling the arm runs and the spelling the
census matches cannot drift (Rule 14).

**The widened vocabulary also surfaced a second site**:
`gitPushbackPruneMirrors` prunes `refs/remotes/<lane>/…` in the CLIENT repo
(never a `refs/heads` branch, never a delivery fact, re-fetched on the next
pass). It carries a reviewed `removal-audit-ok` escape naming exactly that.
The escape marker sits on the call line as well as in the comment block: the
verdict reads the escape from a window of comment lines before the site, and
this statement's own trailing comment moves the needle.

## The probes, and why each direction exists

`TestRemovalAuditGateSequelVocabularyIsLoadBearing` — on the **real tree**, in
three parts: (1) the arm's `update-ref -d` call IS classified branch-delete, so
the record it now emits satisfies it; (2) deleting the ACTUAL emit statement
from the file turns that site RED, so the fix is load-bearing rather than
merely present; (3) with the plumbing vocabulary switched OFF — the pre-fix
census, **driven** rather than described — the same tree reports NOTHING, which
is the blindness itself, measured.

`TestArchiveReclaimBranchDeleteIsAudited` — the real entry point against a real
git repo: the ref's absence is read back **with git**, never taken from the
arm's own report; the record is read from the emitter's own sink; the RED half
disables the emit and requires the same assertion to fail. The RED half takes a
**fresh sink**, because the GREEN half's record is still in the file — probing
the shared sink passed and proved nothing (caught by running it).

## Two probe repairs the change forced (both honest, both verified)

1. `TestRemovalAuditGateSameKindSiblingIsLoadBearing` derived its expected caller
   count from ONE file. The archive-reclaim arm is now a third caller of
   `removalaudit.BranchDeleteAt`, so the derivation was moved to the tree
   (`removalAuditSiblingCallers`): the probe is about the sibling's callers
   wherever they live, and a probe pinned to one file fails on a change that
   makes the gate STRONGER. The file-local count is still used for the topology
   assertion, which mutilates that one file.
2. `removalAuditIsForwardingCall` gained the `-C <dir>` arm. With the plumbing
   verb matched, `mergeRevertGit(repo, "update-ref", ref, tip)` — a real
   invocation through a runner (`removalAuditGitPrefixes` includes `"git",
   "-C"`) — would have been read as a forwarding call and would have silently
   stopped counting for a record. The arm is lowest-precedence and its ambiguity
   is stated in the function's own comment.
