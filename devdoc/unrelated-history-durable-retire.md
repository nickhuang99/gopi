<!-- doc-class: historical -->
# Task Report: the unrelated-history verdict leaves a durable, idempotent retirement

Epic: `gopi merge` detects an unrelated-history branch and refuses it, but the
refusal was terminal for ONE RUN ONLY — the branch was left exactly as it was, so
the auto merge-backlog generator re-filed `merge feat/<x>` on every pass
(`feat/sync-conflict-completed` was filed 4×). This change makes the verdict
leave a durable, idempotent retirement behind, through the EXISTING archive
mechanism, without weakening the refusal.

## Completed

- **The verdict retires the branch** (`cmd/agent/merge_run.go`,
  `mergeDecideBranch` → `mergeUnrelatedHistoryOutcome`): the arm that used to
  return a report-and-leave-alone outcome now writes the archive retirement and
  reports branch + reason + both roots + the action taken.
- **The mechanism is the existing one, not a new file or knob**
  (`internal/tools`): the new exported name
  `RetireUnrelatedHistoryBranch(repo, branch)` delegates to
  `repoSweepArchiveBranch` (→ `ArchiveTagReasoned`), which writes
  `refs/tags/archive/<branch>` plus a reason record. That namespace is already
  hidden from every merge path by `repoSweepArchiveExempt` — `mergeCandidateBranches`,
  `taskMergeExempt`, `safe-merge.sh`, the branch-scan face — and the
  merge-backlog generator filters on it through `archiveTagAncestry`.
- **A new reason code**, `tools.ArchiveReasonUnrelatedHistory =
  "unrelated-history"` (`internal/tools/archive_reason.go`), added to the
  `ArchiveReasons` vocabulary (so `gopi archive -h` and the refusal message list
  it) with its `ArchiveReasonDoc` gloss and its `repoSweepArchiveDetail` body —
  one definition, every reader references it (Rules 1b/13/14).
- **Idempotence is now structural**: `repoSweepArchiveBranch` probes
  `repoSweepArchiveExempt` BEFORE the owner gate, so an archive already recorded
  by an earlier pass is a success even if a ledger row later appears over that
  branch (the standing reason is kept, never restamped). The writer would
  otherwise fail on the SECOND pass of a branch whose row the epic's own
  generator filed — mid-implementation I found that shape and fixed the order,
  rather than papering over it in the caller.
- **The refusal is NOT weakened**: no `--allow-unrelated-histories` lane was
  added, and the owner gate still refuses to retire a branch whose ledger row
  says the work FINISHED (`completed`/`verified`) — that branch keeps its place
  in the queue, the writer's own refusal sentence travels into the report, and
  the run prints it on stderr (Rule 15b, never a swallow).
- **The ref is preserved**: a tag, never `branch -d`. The detail line names the
  undo ("delete that tag to re-arm it").
- **The report names everything the epic asks for**: the verdict word, the
  reason code, both roots as `refs/heads/<name> <tip>` pairs, and the action.
- **Generator-side audit (deliverable 4) — see the finding below.**
- **Tests**: the existing `cmd/agent` gate was rewritten to the new contract plus
  a new second-pass gate, and the mechanism got its own gate in `internal/tools`.
- **devdoc**: `devdoc/merge-unrelated-history-diagnosis.md` gained a
  "Revision (2026-09-22)" section recording the inversion, and the
  assertion-totality allow-list permit that quoted the OLD contract verbatim was
  re-reasoned (a stale permit is itself a failure of that gate).

### Generator-side audit (deliverable 4)

**Does the merge-backlog filer skip archive-tagged branches? YES** — and it is
the same mechanism this change leans on, asked one level up.

`internal/tools/task_merge_backlog.go`: `AuditMergeBacklog` (the face behind
`gopi task audit-merges --submit`, the post-receive hook's stage-1 discovery and
`tools.MergeBacklogGenerator`) calls `archiveTagAncestry` BEFORE the branch+tip
dedup. `archiveTagAncestry` lists `refs/tags/archive/*` and drops every branch
whose tip is reachable from any archive tag (`merge-base --is-ancestor`), with a
deliberate degrade-don't-go-blind fallback when the listing fails. Measured on
the real repo with a REAL unrelated-history branch:

```
before retirement:  merge-backlog audit (submit): scanned=1 submitted=0 duped=1 skipped=0 failed=0
after  retirement:  merge-backlog audit (submit): scanned=0 submitted=0 duped=0 skipped=0 failed=0
```

`scanned` going 1 → 0 is the branch leaving the candidate set — the generator no
longer even considers it, so no row is owed. The branch+tip dedup is the second
net (a live `pending` row for the same branch is counted `duped`), but that net
alone was what the epic caught broken: it dedups the ROW, not the DECISION, so a
row that later reaches a terminal state lets the next pass re-file. The archive
tag is the durable memory the dedup lacked.

**Residual, reported rather than hidden**: `notifyBranchPushed`
(`cmd/agent/cli_tasks.go`) files a separate `branch-pushed` hint row with no
durable memory of a refusal. It is bounded — it fires once per successful
push-back, not once per pass — so it is not the amplification this epic is about;
it is NOT widened here (that would be a new lane, outside this change's
authority). Named in `## Not Done`.

## Not Done

- **The `branch-pushed` hint row's own "no durable memory" shape is NOT fixed.**
  It is one row per push event (bounded by a human/machine push), not one per
  generator pass, so it does not reproduce the epic's amplification. Fixing it
  would mean giving that face a notion of "already decided" — a different lane.
- **The pre-existing `pending` row for the branch is left in the ledger.** The
  retirement stops FUTURE filing (the epic's acceptance (d) is "does not
  increase"), and this change deliberately does not touch an operator's ledger:
  closing another face's row is a decision, not a repair. The row's own executor
  now reports `status=skipped` on it (`gopi task merge-execute 3061` →
  `{"status":"skipped","reason":"no such task row"}` in this environment's
  ledger view), i.e. it cannot land anything.
- **No unit test was added for the `--fast` arm** interacting with the verdict:
  the unrelated-history arm is reached BEFORE `if opts.fast`, which is unchanged
  behaviour, so the retirement happens in `--fast` runs too. I judged that
  correct (the fact it records is a property of the repository, not of the arm)
  and did not add a gate for it.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0 (no output)
- Vet: `go vet ./...` → exit 0 (no output)
- Tests: `go test -run 'TestAssertionTotalityAuditGate|TestArchive|TestRepoResolveRow|TestRetireUnrelatedHistory|TestMergeBacklog' ./internal/tools/` → `ok github.com/nick/gopi/internal/tools 139.185s`; `go test -run 'TestMergeUnrelatedHistory|TestMergeHasCommonAncestor' ./cmd/agent/` → `ok 11.463s`; whole-`TestMerge` suite compared against master (see below).
- gofmt: `gofmt -l cmd/agent/merge_run.go cmd/agent/merge_unrelated_history_test.go internal/tools/archive_reason.go internal/tools/archive_reason_test.go internal/tools/repo_resolve_row.go internal/tools/assertion_totality_allow_test.go` → no output (all formatted)
- Ran: `~/workspace/gopi-unrel-bin/gopi merge feat/e2e-orphan-probe1` — TWICE, on the REAL repo `/home/user/work/GoPi`, with a freshly manufactured unrelated-history branch (`git init -b main` in `~/workspace/gopi-test-scratch/e2e-orphan-src/orphan-src`, one file `only.txt`, one commit `a587ee76…`, pushed in with `git push /home/user/work/GoPi HEAD:refs/heads/feat/e2e-orphan-probe1`; `git merge-base master feat/e2e-orphan-probe1` → exit 1 = no common ancestor).
- Ran (generator): `~/workspace/gopi-unrel-bin/gopi task audit-merges --repo /home/user/work/GoPi --submit` before and after the retirement.
- Result: <the four observations below>
- Commit: `12aa2673 fix(merge): retire an unrelated-history branch durably (idempotent archive tag)` (the report edit itself lands in the follow-up commit)

### E2E observations (the acceptance letters)

**(a) the first pass refuses + retires + reports** — exit 0, verbatim stdout line:

```
  /home/user/work/GoPi feat/e2e-orphan-probe1 unrelated-history  shares no commit with master (git merges it only with --allow-unrelated-histories) — not a conflict any merge gopi could resolve; roots refs/heads/master 2fd34b427f9b459ffb38004c6503d33f308fc460, refs/heads/feat/e2e-orphan-probe1 a587ee763a4775a624502e55ffcbd88e1802013f; retired as a decided exclusion (tag archive/feat/e2e-orphan-probe1, reason unrelated-history) — the branch ref is preserved; delete that tag to re-arm it
gopi merge: run over — 1 branch decision(s), lock released with the run, no child left behind
```

The reason record exists in the mechanism's own namespace:

```
$ git cat-file blob refs/gopi/archive-reason/feat/e2e-orphan-probe1
reason: unrelated-history

branch: feat/e2e-orphan-probe1
repo:   /home/user/work/GoPi
actor:  nick@peerbox
task:   (none)
at:     2026-09-22T06:26:47+08:00
detail: `gopi merge` cannot merge this branch by construction: it shares no commit with master (git merges such a branch only with --allow-unrelated-histories), so no merge lane may ever land it; retired as a decided exclusion, ref preserved
```

> **Retired mechanism (merge verb removal, 2026-11-01).** The `$ gopi merge
> feat/e2e-orphan-probe1` line below (and the `gopi merge:` output it printed) are
> the RECORD of what this run executed, kept verbatim; the top-level verb was
> removed afterwards and answers **exit 1** today (`gopi task merge` runs the same
> engine). The retirement verdict the page records is unaffected.

**(b) the SECOND pass is empty** — the branch is filtered before it is decided:

```
$ gopi merge feat/e2e-orphan-probe1     # second run
gopi merge: branch selection feat/e2e-orphan-probe1 — 1 of 1 repo(s) hold one
gopi merge: holding the single-instance lock; 1 repo(s), ceiling 120000 estimated tokens / 25 conflicted files per branch
gopi merge: exported GOPI_MERGER_LOCK_OWNER=1839364:merge so the tools this run spawns are admitted under this run's lock instead of deadlocking on it
gopi merge: hand-over budget 45 minute(s) published to the child's GOPI_TIMEOUT_SEC
no branch in /home/user/work/GoPi needs a merge decision
gopi merge: run over — 0 branch decision(s), lock released with the run, no child left behind
```

Tags `377 → 377`; the tag's tip is unchanged (no re-point, no second write).

**(c) the branch still exists (tagged, not deleted)**:

```
$ git rev-parse feat/e2e-orphan-probe1   → a587ee763a4775a624502e55ffcbd88e1802013f
$ git branch --list feat/e2e-orphan-probe1 →   feat/e2e-orphan-probe1
$ git tag -l archive/feat/e2e-orphan-probe1; git rev-parse archive/feat/e2e-orphan-probe1
archive/feat/e2e-orphan-probe1
a587ee763a4775a624502e55ffcbd88e1802013f          ← the tag points at the branch tip
```

**(d) the `merge-backlog,merge-feat` row count does not increase** — the real
post-receive hook filed the row when the fixture was pushed (the defect live,
before my change), and no pass adds another:

```
$ sqlite3 ~/.gopi/memory.db "select id,status,tags,summary from tasks where summary like '%feat/e2e-orphan-probe1%';"
3061|pending|merge-backlog,merge-feat|merge feat/e2e-orphan-probe1 into master
$ sqlite3 ~/.gopi/memory.db "select 'tagged-count='||count(*) from tasks where summary like '%feat/e2e-orphan-probe1%' and tags like '%merge-backlog%';"
tagged-count=1
```

…measured identically before pass 1, after pass 1 and after pass 2, and after a
post-retirement `audit-merges --submit` (`scanned=0`).

**(e) the named test is RED on master and GREEN after** — see
`## Evidence (self-filed)`.

**(f) `go build ./...` and `go vet ./...` both exit 0** — see above.

**Pre-existing gate failures, compared against master (not merely asserted).** A
whole-package `go test ./internal/standards/` run still fails two doc-class gates
— and it fails them on master too, with the SAME failure set. Measured by running
the two gates in this worktree and in the master worktree and diffing the
findings:

```
$ diff mine.txt master.txt
2c2
<  docclass_gate_test.go:130: devdoc/DOC-INDEX.tsv: 2195 rows cover 2196 files under devdoc/ (classes: map[historical:1727 …])
---
>  docclass_gate_test.go:130: devdoc/DOC-INDEX.tsv: 2194 rows cover 2195 files under devdoc/ (classes: map[historical:1726 …])
<  --- FAIL: TestDocClassBannerMatchesManifest (0.01s)
<  --- FAIL: TestDocManifestCoversEveryDevdocFile (0.21s)
---
>  --- FAIL: TestDocClassBannerMatchesManifest (0.04s)
>  --- FAIL: TestDocManifestCoversEveryDevdocFile (0.35s)
```

The only content difference is my own added row (2194→2195 rows, 1726→1727
historical). Every named offender is identical on both trees:
`devdoc/assertion-totality-gate-master-red.md` (unclassified),
`devdoc/bill-analysis-deepseek-20260922.md`, `devdoc/handoff-20260922-0503.md`,
`devdoc/handoff-20260922-bill-analysis.md`, `devdoc/session-budget-prompt.md`
(missing banners) — all five are `git ls-tree master devdoc/` paths, and one is
literally a coverage floor (`735 markdown docs` vs the gate's floor), i.e. a
budget consumed by other lanes' docs, not by this change. **My change adds no
gate failure.**

## Evidence (self-filed)

**RED proof on master** — the SAME test file copied onto a master worktree
(`git worktree add -f --detach /home/user/workspace/gopi-unrel-red master`, at
`2fd34b42`) and run there. The only file differing from master in that worktree
was the copied test (`git status --porcelain` → ` M
cmd/agent/merge_unrelated_history_test.go`), so the failure below is the
assertion, not a build error.

The strongest form of the proof is the WHOLE-`TestMerge`-suite comparison — same
suite, same host, both trees, failing test names only:

```
$ diff merge-mine.txt merge-master.txt
2,3c2,5
< FAIL	github.com/nick/gopi/cmd/agent	145.059s
< --- FAIL: TestMergeUnionRowCheckOnTheRealManifest (0.64s)
---
> FAIL	github.com/nick/gopi/cmd/agent	160.864s
> --- FAIL: TestMergeUnionRowCheckOnTheRealManifest (3.76s)
> --- FAIL: TestMergeUnrelatedHistoryBranchIsDiagnosedNotAnError (2.42s)
> --- FAIL: TestMergeUnrelatedHistoryRetirementIsIdempotentAcrossPasses (2.88s)
```

Both of my tests are RED on master and GREEN in my tree;
`TestMergeUnionRowCheckOnTheRealManifest` fails on BOTH trees (pre-existing — it
reads the same devdoc manifest drift recorded in the section above, and it is not
my file). The focused run, with the assertion messages:

```
$ cd /home/user/workspace/gopi-unrel-red && go test -run 'TestMergeUnrelatedHistoryBranchIsDiagnosedNotAnError|TestMergeUnrelatedHistoryRetirementIsIdempotentAcrossPasses' -v ./cmd/agent/
=== RUN   TestMergeUnrelatedHistoryBranchIsDiagnosedNotAnError
    merge_unrelated_history_test.go:126: feat/orphan-TestMergeUnrelatedHistoryBranchIsDiagnosedNotAnError carries no retirement tag — the verdict is terminal for this run only, which is the defect
--- FAIL: TestMergeUnrelatedHistoryBranchIsDiagnosedNotAnError (1.57s)
=== RUN   TestMergeUnrelatedHistoryRetirementIsIdempotentAcrossPasses
    merge_unrelated_history_test.go:206: the first pass left no archive/feat/orphan-TestMergeUnrelatedHistoryRetirementIsIdempotentAcrossPasses tag: git rev-parse refs/tags/archive/feat/orphan-…: exit status 128: … unknown revision or path not in the working tree.
--- FAIL: TestMergeUnrelatedHistoryRetirementIsIdempotentAcrossPasses (0.84s)
FAIL	github.com/nick/gopi/cmd/agent	3.523s
```

**(g) the fixture was removed afterwards and both worktrees are clean**:

```
$ git branch -D feat/e2e-orphan-probe1        → Deleted branch feat/e2e-orphan-probe1 (was a587ee76).
$ git tag -d archive/feat/e2e-orphan-probe1   → Deleted tag 'archive/feat/e2e-orphan-probe1' (was a587ee76)
$ git update-ref -d refs/gopi/archive-reason/feat/e2e-orphan-probe1
$ git branch --list feat/e2e-orphan-probe1 ; git tag -l 'archive/feat/e2e-orphan-probe1'   → (both empty)
$ git rev-parse --verify --quiet refs/gopi/archive-reason/feat/e2e-orphan-probe1 → reason ref removed
$ git status --porcelain     (main repo)   → (empty)
$ git status --porcelain     (worktree)    → (empty)
```

**GREEN after** (branch worktree):

```
$ go test -run 'TestMergeUnrelatedHistory|TestMergeHasCommonAncestor' -v ./cmd/agent/
--- PASS: TestMergeUnrelatedHistoryBranchIsDiagnosedNotAnError (3.95s)
--- PASS: TestMergeUnrelatedHistoryRetirementIsIdempotentAcrossPasses (4.18s)
--- PASS: TestMergeHasCommonAncestorTellsOrphanFromRelated (2.64s)
ok  	github.com/nick/gopi/cmd/agent	15.118s

$ go test -run 'TestRetireUnrelatedHistoryBranchIsDurableAndObeysTheOwnerGate' -v ./internal/tools/
--- PASS: TestRetireUnrelatedHistoryBranchIsDurableAndObeysTheOwnerGate (13.10s)
ok  	github.com/nick/gopi/internal/tools	13.625s
```

**A fixture that lied, and the fix** (recorded because it is the trap in this
area): the first draft of the `internal/tools` gate created ordinary branches off
a shared seed commit, so the arm-④ tag sat on that commit and was therefore an
ANCESTOR of every tip — the ancestry filter legitimately dropped all of them and
the control arm could not tell a working filter from a blind one. The fixture now
builds true orphan roots (`archOrphanBranch`, `git commit-tree` with no parent),
which is both the production shape and the only shape that makes the filter
discriminating.

**Scratch/Rule 22**: the E2E fixture source repo lives under the pinned scratch
root (`~/workspace/gopi-test-scratch/e2e-orphan-src`); the built binary lives in
`~/workspace/gopi-unrel-bin/` (outside every worktree, so Rule 30 is unaffected).
No `/tmp` path is used and no `.db` or binary artifact is committed.
