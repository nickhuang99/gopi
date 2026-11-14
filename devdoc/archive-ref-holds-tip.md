<!-- doc-class: normative -->
# Task Report: an archiving/removal path must leave the commit reachable from a ref

Task: `01a12f48-7ca7-74bf-8f63-2a8fd3f8dd22` · host `hostb-Laptop` · branch
`task/01a12f487ca774bf8f632a8fd3f8dd22`

## Completed

**1. The reported state is REAL and reproducible, and the diagnosis was narrowed by
measurement ①: the observed 2026-10-11 deletion left the commit in the reflog grace
window, not in the archive namespace.**

On the peer `peer1`, for the tip the epic names (`bc8e2ea8c279c86a6682c50853cb6bbfde05f79b`):

```
$ git rev-parse bc8e2ea8^{commit}      → bc8e2ea8c279c86a6682c50853cb6bbfde05f79b
$ git for-each-ref --contains <tip>    → (empty)
$ git fsck --no-progress --unreachable --no-reflogs | grep <tip>
unreachable commit bc8e2ea8c279c86a6682c50853cb6bbfde05f79b
$ git merge-base --is-ancestor <tip> master   → rc=1
```

So the commit is present, an ancestor of **nothing** (NOT merged — the "already landed"
reading is refuted), reachable from no ref, and reported UNREACHABLE by `git fsck` once
`--no-reflogs` is passed. The filing run's `git fsck --dangling` was empty because
`--dangling` is not the reachability enquiry: what keeps the object alive right now is a
reflog entry in the grace window (`git fsck --dangling` does not list it — measured on
both reflogs on and off). **"Deleted with a grace period" is therefore not a hypothesis
for this commit: it is the measured state** (Rule 41: measurement vs inference labelled).

**2. Root cause, named by code AND by a removal record: a scratch holder counted as a
durable one, so the orphan guard allowed a delete while the tip's only company was a ref
its own writer deletes.**

The audit trail of the disposal chain on peer1 carries the decision, verbatim:

```
/ home/ nick/ .gopi/ logs/ removal-audit.log
{"version":1,"ts":"2026-10-11T22:40:16.74792688Z","kind":"branch-delete","arm":"merge-run-spent-branch",
 "op":"branch:delete","repo":"/home/user/work/GoPi","target":"task/01a12cb5eee0737fb63caaad45194e85",
 "evidence":"merge --ff-only landed the ref in master (a fast-forward cannot conflict, nothing is left to hand over)"}
```

`removalaudit.ArmMergeRunSpentBranch` was written by exactly one site,
`mergeDecideBranch`'s fast-forward arm (`cmd/agent/merge_run.go`: the record is written
immediately before `git branch -d <branch>`). *(Re-measured 2026-11-01: the arm's
CONSTANT is deleted — `merge_run.go` went with the top-level merge verb, and the
constant went with its only emit; `internal/removalaudit/audit.go:307` records the
retirement. A record naming that arm is evidence of an OLD binary. The surviving
spent-branch prune arms are `ArmTaskMergeBranch` and `ArmMergedCleanupBranch`.)*

What that record's evidence claims vs what was measured for THIS branch (two facts, each
re-measured, and they are not contradictory):

- The branch's **earlier** commit is on master — `2aeece70e docs(tasks): report for
  01a12cb5 …` with `git merge-base --is-ancestor 2aeece70e master` → rc=0 on the peer. So
  the branch really was merged once, and the fast-forward evidence was true at the time it
  was written.
- The branch's **tip** (`bc8e2ea8`, subject *"the evidence hold-out ratchet runs to its
  end — delete the two stand-ins the merge union outlived"*) is an ancestor of nothing:
  `git merge-base --is-ancestor bc8e2ea8 master` → **rc=1** on the peer. Its subject area
  was resolved on master by an equivalent, separately-committed decision
  (`e9fe0ded0 docs(standards): the evidence hold-out ratchet's end recorded on the current
  trunk — resolution accepted, not re-applied` — a reading, and the *"what it was resolved
  by"* attribution is this report's inference).

So the branch was merged as a ref and the tip carried work that was subsequently resolved
elsewhere: the deletion was defensible on its merits, and the commit was still a
deliverable of a named branch whose content a "WHY" record (Rule 9) exists to keep. It is
readable today (`git cat-file -p bc8e2ea8` on the peer) **only** because the reflog grace
has not yet lapsed — which is exactly the state the guard is supposed to refuse.

**The defect is the class of holder that count as evidence of durability**, and it lives in
`internal/tools/branch_delete_guard.go` (`branchDeleteOrphanRefusalAt`): the guard decided
"would this commit survive in a ref?" by enumerating **every** ref, and one family answers
YES without keeping anything —

```
refs/remotes/gitPushcheck/master   the pushback lane's per-pass mirror, written by a fetch
                                   and DELETED by the same pass (gitPushbackPruneMirrors)
```

Measured on this host after this fix, pre-fix code, in the incident's own fixture shape:

```
--- FAIL: TestOrphanGuardRefusesATipOnlyAScratchMirrorHolds
    the guard ALLOWED a delete whose only other container is refs/remotes/gitPushcheck/master
    (deleted by its own lane when the pass ends) — the tip <sha> would survive in no ref
--- FAIL: TestRemovalPathLeavesNoUnreachableTip
    a removal path deleted fix/removal-path while refs/remotes/gitPushcheck/master was its only
    other container: no ref contains <sha> afterwards; after `git gc --prune=now` the object is
    still present=true
```

A removal path that asks the guard, is told "another ref holds it", deletes the ref, and
lets the owning lane prune its own mirror reproduces the incident exactly. (The mirror is
also why the disposal looked benign in every report: `git for-each-ref --contains` answered
NON-empty at that instant, and empty one pass later.)

**3. The hole was already NAMED and the report arm fixed — the decision was not (epic
01a12af7).** `branchDeleteScratchOnlyHolders` / `branchDeleteMirrorOnlyNote` from
2026-10-11 declared `refs/remotes/gitPushcheck/` non-durable and amended the **report
sentence**; its own test asserted *"the GUARD'S DECISION IS UNCHANGED — the same fixture
that is reaped before the change is reaped after it"*. That boundary is what left the
decision open; the fix below moves the same, already-declared non-durable notion into the
decision, and that test is amended with the measurement that falsified its premise.

**4. The fix** (`internal/tools/branch_delete_guard.go`): the orphan guard decides on
**durable** holders. A tip whose every remaining container is a non-durable mirror takes the
SAME refusal as a tip with no container, with the holder named and the recovery command in
the sentence. Positive controls unchanged: any tag, any branch, another remote's tracking
ref still silences the guard, and a caller's DELIVERY POINT still authorises the delete
(`branchDeleteOrphanRefusalAt`'s existing arm). The guard is still read-only — it deletes
nothing (asserted).

**5. Regression test (red-first), `internal/tools/archive_ref_holds_tip_test.go`:** the
archive writer holds the tip and it survives two pruners; the guard refuses a scratch-only
holder; the end-to-end removal shape leaves the tip contained by the archive ref. Plus the
amended `TestBranchDeleteGuardRefusesAScratchOnlyHolder`.

**5b. What the trail can and cannot answer (measured, and it bounds this fix).** Every
disposal writes a removal-audit record, and on this host `…/logs/removal-audit.log` holds
**4336** `branch-delete` records: `finish-cleanup-branch` 2143, `finish-cleanup-branch-force`
2142, `merge-run-spent-branch` 51 (36 of them the `--ff-only landed` evidence, 14
`--is-ancestor`). The two close-out arms' records do not carry the tip, so "did THIS deletion
orphan the commit" is not answerable from the trail for them — the reading must be taken
from the repo, which is exactly why the guard (not the log) is where the decision is fixed.

**5b-2. Which removal sites ASK the guard (so the fix's coverage is measurable, not
claimed).** The census (`ref-removal-site-census.txt`, this run's session dir) enumerates
every ref-removing site in `internal/` + `cmd/` and the guard's callers:

- **Ask the orphan guard** (the two FORCE-delete arms): `task_finish_cleanup.go:4750`
  (`branchDeleteOrphanRefusalAt`, with the chain's delivery point) and
  `task_adoption.go:414` (`branchDeleteOrphanRefusal`). Both are now closed against a
  scratch-only holder.
- **`git branch -d` elsewhere** (`task_merge.go:691/710`, `task_merge_executor.go:306/348`,
  `task_merged_cleanup.go:613`, `merge_run.go:2039/2052/2145`): these rely on git's own
  merged-only lock. **Read, not measured**: `-d` refuses unless the branch is merged into its
  UPSTREAM, so "cannot orphan the tip" holds when the upstream is a durable ref (normally
  master) and is NOT established when a `refs/remotes/gitPushcheck/*` mirror is what the
  upstream resolves to — which is exactly the family the 2026-10-11 record came from. Rather
  than assert the semantics, this run drove neither: no guard was added there, and the
  construction that would settle it is filed (see `## Not Done`).
- **`archive_ref_reclaim.go:600`** (`update-ref -d`, the reclaim arm): it may only act when
  `refs/tags/archive/<branch>` names the same commit (guards ①–⑥ in that file), which the
  first test in this file measures directly (`TestArchiveTagHoldsTheBranchTipAndSurvivesGC`).
- **`task_sync_git_pushback.go:866`**: deletes only this lane's own scratch namespace
  (carries its own `removal-audit-ok` reasoning).

**5c. The companion namespace was ALREADY guarded — this fix does not duplicate it.** A
reason record whose tag is gone is reported by `ArchiveNamespaceOrphans`
(`internal/tools/archive_reason.go`, pinned by
`TestArchiveReasonRecordDoesNotOutliveItsTag`): reported, never auto-deleted, because
dropping the tag is the documented way to un-archive. The REF-holds-the-commit half had no
such reader — that is the half this task adds, at the decision point rather than at the
report point.

**5c-2. The reclaim arm re-verified against the tag's OWN commit (read, not assumed).**
`archive_ref_reclaim.go`'s delete is gated by `tagTip != headTip` (line ~559: the tag must
resolve to the same commit the head ref points at) and re-checks the tip immediately before
the `update-ref -d`, so the durable half is the tag by construction. That is the same
property this task's first test measures directly — the inference ("the tag still names it")
does not have to be taken on faith.

**5d. Deliverable declaration (Rule 6d).** Branch
`task/01a12f487ca774bf8f632a8fd3f8dd22` in the shared object store of the main repo;
recovery of this report: `git show task/01a12f487ca774bf8f632a8fd3f8dd22:devdoc/archive-ref-holds-tip.md`.
The commit is on that branch (it descends from master `6613b7d5e`); nothing was merged —
the archive/merge machinery is the system layer (Rule 28), so the branch is the deliverable.

**6. Rule 31 real run of the surface** — `gopi task archive` in a scratch repo, with the
reachability enquiry and the gc survival (verbatim output in `## Verification`): the archive
ref holds the tip, `git gc --prune=now` does not take it, and the SAME shape without an
archive decision is measured unrecoverable once the reflog grace expires.

- **Delivery-point sync, then the drift the fix itself caused.** Master was merged into this
  branch (`git merge --no-edit master`) before hand-over. That merge surfaced one thing this
  fix OWNS: `branch_delete_guard.go` grew 23 lines above the refusal sentence that
  `devdoc/archive-mint-outside-writer.md` anchors in `devdoc/DOC-INDEX.tsv`, so the manifest
  row's recorded number (`:241`) drifted to `:264`. Regenerated with the table's own named
  generator, never by hand (Rule 36; Rule 38 — the figure names its generator):

  ```bash
  go run internal/docclass/testdata/anchor-number-census.go . \
      internal/docclass/testdata/anchor-number-baseline.tsv
  ```

  Same run drops the `devdoc/rule17-gate-report.md` permit — a STALE permit is an error, and
  that file is now 238 lines so its recorded `:43` is inside the file and the row no longer
  drifts. `TestAnchorNumberGateOnTheRealTree`: RED before the regeneration, green after.
  The other three red tests in `./internal/docclass` (`TestProsePathLine*`) are **RED ON
  MASTER TOO** with identical output, and are already an in-flight row
  (`01a12f4f-6cd2-7c69-a87c-8ca62a9322de`) — not this branch's.

## Not Done (honest)

- **The 2026-10-11 disposal's own object was not recovered, and cannot be made durable
  retroactively**: it is reachable from no ref on the peer and held only by the reflog grace
  window. This report does not mint a ref for it (that is an operator's call on the peer,
  and the object is already readable: `git cat-file -p <tip>`).
- **The reachability enquiry the filing run used was the wrong one, and that cost the
  case its precision**: `git fsck --dangling` is not "unreachable" — measured on the peer,
  `git fsck --unreachable --no-reflogs` REPORTS the tip (`unreachable commit bc8e2ea8…`)
  while `--dangling` prints nothing for it, with reflogs on or off. So the object is not
  somehow "special"; it is in the ordinary unreachable set, and only the reflog grace keeps
  it. This matters for every later reader of that bug report: the same command on a peer
  after the grace expires returns NOTHING and reads as "the object was never there".
- **1679 other commits sit in the same unreachable state on the peer** (`git fsck --no-progress
  --unreachable --no-reflogs | grep -c "^unreachable commit"` → 1679; `… "^unreachable tag"` →
  0, peer master `45b54d81f` at the moment of counting).
  They are a POPULATION, not a single incident, and this run did not enumerate them: the
  class is now refused at the guard, but the backlog is a separate, operator-visible job
  (`scripts/archive-tag-census.sh` classifies archive tags; an unreachable-commit census does
  not exist). Filed as a bug row rather than fixed here (Rule 41): **`01a12f65-c286-793d-8de9-27fbf5243148`**
  (tags=bug), carrying the exact generator command and the count (Rule 38), and naming the
  object store that held them (master `45b54d81f` on the peer at the moment of counting).
- **The guard is not wired into every removal path, and §5b-2 says which ones and why.** The
  two FORCE-delete arms ask it (`task_finish_cleanup.go:4750`, `task_adoption.go:414`); the
  merge/cleanup arms use `git branch -d`, whose merged-only lock is itself the argument that
  the tip has a durable ancestor. What this run did NOT do is drive the arm that wrote the
  2026-10-11 record (`mergeDecideBranch`'s `--ff-only` arm, `removalaudit.
  ArmMergeRunSpentBranch`) end to end, with a `refs/remotes/gitPushcheck/*` mirror standing as
  the tip's only other container — that is the one construction under which a `-d` arm could
  still orphan a tip, and it needs a diverged peer/master pair plus a finished pushback pass
  to arrange. **This is the honest gap in the fix**, and it is dispatchable as it stands: a
  fixture repo, a master that has since moved, a mirror planted at the branch tip, then the
  merge arm — if the arm deletes, the guard must be wired there too. Filed as a bug row from
  this run rather than left as a note (Rule 41): **`01a12f68-a72c-71ae-a5b0-52e5c18b79c6`**
  (tags=bug), whose briefing is the construction above, step by step.
- **Rule 41 rows filed by this run (both on THIS host's ledger, per the local policy):
  `01a12f65-c286-793d-8de9-27fbf5243148`** (the 1679-commit unreachable backlog) and
  **`01a12f68-a72c-71ae-a5b0-52e5c18b79c6`** (the un-driven `-d` arm). Their payloads carry
  the verbatim commands and the raw-output paths; the observations are in
  `archive-proof-output.txt` and `ref-removal-site-census.txt` in the session directory.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (binary 17:12);
  `go build ./...` → OK.
- **Tests**: `make test-budget PKGS='./internal/tools'` → see `## Evidence` (the run below).
- **Ran** (verbatim, on this branch's tree):
  - `bash /home/user/.gopi/sessions/01a12f48-7ca7-74bf-8f63-2a8fd3f8dd22/run-archive-proof.sh`
  - inside it, the surface under test:
    `GOPI_HOME=$SB /home/user/workspace/gopi-01a12f487ca774bf8f632a8fd3f8dd22/bin/gopi task archive task/01a12f48demo-branch --reason unowned --detail "…"`
- **Result** (copied from `archive-proof-output.txt`; repo is the scratch repo the run
  prints, tip `b3a89fc816d9ea24c0808f171aba3ef32166f615`):

```
archived task/01a12f48demo-branch as archive/task/01a12f48demo-branch
  reason: unowned
  owner:  (no task row tracked this branch)
  read back: git cat-file blob refs/gopi/archive-reason/task/01a12f48demo-branch
  deleted          archive/task/01a12f48demo-branch
archive exit=0

-- git for-each-ref --contains $TIP --
refs/tags/archive/task/01a12f48demo-branch commit          ← acceptance (a): NON-EMPTY, names the archive ref

-- after the branch ref went (the state an archive decision leaves) --
refs/tags/archive/task/01a12f48demo-branch commit

-- git gc --prune=now --
commit                                                     ← the object is still there
refs/tags/archive/task/01a12f48demo-branch commit          ← acceptance (b): the ref still contains it after gc

OBSERVATION (recorded because it is easy to misread): the script's own `git branch -D $BR`
answered `error: branch 'task/01a12f48demo-branch' not found.` — because `gopi task archive`
deleted the head ref itself, in the same invocation, via its automatic post-archive reclaim
leg (`ArchiveReclaimDecidedRefs`, `cmd/agent/cli_archive.go:331`; the line the command printed
as `deleted  …`). That leg may only fire when the archive tag names the same commit
(`archiveRefReclaim` guards ①–⑥, and the delete re-verifies the tip immediately before
`update-ref -d`), which is why the tip is STILL contained by the tag afterwards — and it is
the sequence measured above, not a second, unattended deletion.

NEGATIVE CONTROL (same shape, NO archive decision):
-- after the lane prunes its mirror --                     (empty)
-- gc WITHOUT reflog expiry: --            commit          (the grace period)
-- reflog expire --expire=now --all + gc --  fatal: git cat-file: could not get object info
GONE: the unarchived commit is unrecoverable
```

- **Red-first (acceptance (c))**: with the production hunk reverted in place
  (`git checkout -- internal/tools/branch_delete_guard.go`, new tests kept; patch preserved
  at `fix-branch-delete-guard.patch` and re-applied): 3 FAIL
  (`TestOrphanGuardRefusesATipOnlyAScratchMirrorHolds`,
  `TestRemovalPathLeavesNoUnreachableTip`,
  `TestBranchDeleteGuardRefusesAScratchOnlyHolder`); after re-applying the fix: `ok
  github.com/nick/gopi/internal/tools`. Transcript: `red-proof-prefix.txt`.
- **Commit**: `500a37d38` — `fix(tools): the branch-delete orphan guard decides on DURABLE
  holders, not scratch mirrors` (the production hunk + tests + this doc). Report refinements
  ride `29c01be60 … 148c9927b`; the branch is
  `task/01a12f487ca774bf8f632a8fd3f8dd22` (worktree `/home/user/workspace/gopi-01a12f487ca774bf8f632a8fd3f8dd22`).

## Evidence (self-filed)

- `git rev-parse bc8e2ea8^{commit}` on peer1 → `bc8e2ea8c279c86a6682c50853cb6bbfde05f79b`;
  `git for-each-ref --contains <tip>` → empty; `git fsck --unreachable --no-reflogs | grep
  <tip>` → `unreachable commit bc8e2ea8…`; `git merge-base --is-ancestor <tip> master` → rc=1.
- `grep -h 01a12cb5eee0737fb63caaad45194e85 /home/user/.gopi/logs/removal-audit.log`
  (on peer1, `…/logs/` is the gopi data root's) → the one `arm":"merge-run-spent-branch"`
  record quoted in §2.
- The pre-fix guard's behaviour, measured in this run's fixture: quoted in §2 (the FAIL text
  is the observation; the fixture is `branchGuardFixtureRepo` + one `git update-ref`).
- Counts from the proof run (mine to report, the framework's are not): `for-each-ref
  --contains` named exactly ONE ref in §6 (`refs/tags/archive/…`), and the negative control
  answered empty both before and after the mirror was pruned.
- Raw outputs: `archive-proof-output.txt`, `red-proof-prefix.txt`,
  `scratch/archive-ref-repo` (the scratch repo left in place so a reviewer can re-run the
  enquiry), all under
  `/home/user/.gopi/sessions/01a12f48-7ca7-74bf-8f63-2a8fd3f8dd22/`.
