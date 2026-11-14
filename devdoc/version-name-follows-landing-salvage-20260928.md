<!-- doc-class: historical -->
# Salvage Measurement: `salvage/version-name-follows-landing-20260928` — every intent item RETIRED by `ddbc3df1`

**Verdict: nothing to re-implement.** The archived branch (tip `88a0ae76`,
2026-09-21 16:42) asked for three things, and master decided against all three
four days later: `ddbc3df1 feat(version): the binary's identity is its own —
internal, constant, checkout-independent` (2026-09-24 09:26) **deleted the whole
version-name lane on purpose**. Re-implementing any of the three would be a
regression against that commit, not a salvage. This document is the salvage
record; **no production code change accompanies it**, and none is warranted.

Task: `01a0e922-7b29-7b81-9eb8-45e834295849` — rework of a deleted ref whose
only surviving copy is `git diff merge-base..tip`
(`salvage_version-name-follows-landing-20260928.diff.gz`, read for INTENT only,
never applied — Rule 23).

## 0. What was archived, and why it could not merge

| field | value |
|---|---|
| branch | `salvage/version-name-follows-landing-20260928` |
| tip | `88a0ae7648b5182fc7a5c29c461443bada916948` (2026-09-21 16:42:40 +0800) |
| tip subject | `fix(version): the merge face names the position it lands, and the report dates the name` |
| merge-base | `d2703f1f5665bb23735f6fc27a108dfc3adcf3b6` (2026-09-21 16:12:03 +0800) |
| master at archive | `cb89fff6bc636427741bfdec0f22abb981a9edc6` |
| master measured | `590d5f334fa67a7749cfb3ff1f22c2d9cea7dc00` (2026-09-29 03:4x), re-verified at `a80a9f8b` (§6) |
| archive reason | non-ff salvage branch; substantive conflict / unrelated history with master |
| diff size | 725 lines, 6 files (`cmd/agent/cli_tasks_rebuild.go`, `cmd/agent/merge_run.go`, `cmd/agent/merge_version_name.go` new, `cmd/agent/merge_version_name_test.go` new, `internal/version/version.go`, `internal/version/version_test.go`) |

The tip is still resolvable and is **not** in master — so it was a real, never-landed
change set, not a fixture:

```bash
$ git cat-file -t 88a0ae7648b5182fc7a5c29c461443bada916948
commit
$ git merge-base --is-ancestor 88a0ae7648b5182fc7a5c29c461443bada916948 master; echo $?
1
$ git merge-base --is-ancestor d2703f1f5665bb23735f6fc27a108dfc3adcf3b6 master; echo $?
0            # the merge-base IS master history: the fork point was on master
```

## 1. The intent, recovered from the diff (never applied)

Three interlocking items, each stated in its own doc comments:

- **I1 — the merge face must NAME the position it lands.** `mintPositionVersionName`
  (`cmd/agent/merge_version_name.go`, new) is the ONE mint every landing face calls;
  the foreground `gopi merge` was to call it in its sweep (name first, then rebuild)
  and in its pass close-out. The diff's own rationale: the resident merger minted on
  every round but was RETIRED (`merger_retired.go`) and replaced by the foreground
  `gopi merge` without the mint being re-attached, so 158 merge rounds landed
  unnamed and `gopi version` introduced the binary by a name three days old.
- **I2 — the pass close-out names an unnamed position without a landing.**
  `nameUnnamedPosition` + `handedOverBranchNames` + `mergeTipMovedPast` +
  `mergeRepoErrored` (`merge_run.go`): a repo the run walked and left with no name
  at all is named outright, a repo whose tip did not move is skipped (an unprovable
  landing is not named), and a repo whose refs could not be read is excluded.
- **I3 — the report must say how far the NAME lags the code.**
  `Info.RevisionCommitDate` + `AgeText()` + `ReportAgeKey` + `roundAge`
  (`internal/version/version.go`): the DESCRIPTION renderer gains an `age:` line
  comparing the tag's committer date with HEAD's, because the operator judged the
  version stale from the name's date alone and the report offered nothing to compare
  it with.

The diff carries nothing else: no parser change, no new parser test, and no
fixture for the `-g`-inside-a-minted-name hazard (that hazard belongs to the
*sibling* salvage `salvage/version-identity-20260928`, and master landed its
remedy at `2cf202cb` — see §5).

## 2. Measurement against current master — the item-by-item table

The measurement order decides the verdict. The branch's merge-base `d2703f1f`
(2026-09-21) is an **ancestor** of master, and the retirement `ddbc3df1`
(2026-09-24) is dated **after** the fork — so master did not restate this intent,
it removed the machinery the intent was improving.

| intent item | the diff's shape | master today | deciding commit | status |
|---|---|---|---|---|
| **I1** `mintPositionVersionName` re-attached to `gopi merge` / the rebuild close loop | new `merge_version_name.go`; calls in `mergeSweepRepo` and `runMerge` | `internal/version.MintTag` and `TagAt` **do not exist** (`git grep -F MintTag master -- '*.go'` → three COMMENT lines, two of them naming `MintTag`, and no call site); the package doc names the deletion ("deleted whole rather than kept behind a flag, because a dormant mint is the coupling this epic exists to remove") | `ddbc3df1`, 2026-09-24 | **RETIRED** |
| **I2** pass close-out names an unnamed position (no landing) | `nameUnnamedPosition`, `handedOverBranchNames`, `mergeTipMovedPast`, `mergeRepoErrored` | 0 hits each on master; the `runMerge` close-out no longer mints anything, and the adoption face says the same in its own comment (`cli_tasks_adopt.go:128`: "NO VERSION TAG IS MINTED FOR WHAT THIS PASS ADOPTED") | `ddbc3df1`, 2026-09-24 | **RETIRED** |
| **I3** `AgeText` / `ReportAgeKey` — how far the name lags the code | `Info.RevisionCommitDate`, `AgeText()`, `roundAge`, `age:` line in `Description()` | `AgeText`, `ReportAgeKey`, `RevisionCommitDate`, `roundAge`, `landingText`, `TagCommitDate`, `UnnamedLandings` — **0 hits each** in `internal/version/`; the whole multi-line DESCRIPTION renderer went with the lane (`internal/version/version.go` shrank to **229** lines — `ddbc3df1`'s numstat is `106 879`, and `name.go` 370 / `tag.go` 147 / `git.go` 67 / `version_test.go` 546 were deleted outright); today `gopi version` prints the compiled-in `internal/version.Version` plus artifact metadata (`cmd/agent/cli_version.go`), and there is **no name to age** | `ddbc3df1`, 2026-09-24 | **RETIRED** |
| **the lane's own residue** 184 `version/*` tags + exported `Namespace` | (n/a — predates the branch) | kept as history; `internal/version/Namespace`'s doc says *"NOTHING IN PRODUCTION WRITES IT"*, so it is a fixture spelling, not a socket for a new mint | `ddbc3df1`, 2026-09-24 | **LANDED (as residue, deliberately)** |

### The commands, verbatim, with their observed output

Every figure above is reproducible from these (read-only git plumbing; no checkout,
no `git apply` — Rule 23 / Rule 29). The full run is
`~/.gopi/sessions/01a0e922-7b29-7b81-9eb8-45e834295849/evidence/measure.log` and
`…/evidence/measure2.log`; below is the deciding subset.

```bash
$ git merge-base --is-ancestor d2703f1f5665bb23735f6fc27a108dfc3adcf3b6 master; echo $?
0                                    # the fork point is master history
$ git merge-base --is-ancestor 88a0ae7648b5182fc7a5c29c461443bada916948 master; echo $?
1                                    # the archived tip never landed (real change set)
$ git rev-parse master
590d5f334fa67a7749cfb3ff1f22c2d9cea7dc00

# I1/I2 — is any of the machinery master's?
$ for s in mintPositionVersionName nameUnnamedPosition handedOverBranchNames \
           mergeTipMovedPast landedBranchNames mergeRepoErrored TagAt; do
>   printf '%-30s hits=%s\n' "$s" "$(git grep -F "$s" master -- '*.go' | wc -l)"; done
mintPositionVersionName        hits=0
nameUnnamedPosition            hits=0
handedOverBranchNames          hits=0
mergeTipMovedPast              hits=0
landedBranchNames              hits=0
mergeRepoErrored               hits=0
TagAt                          hits=0
$ git grep -n -F -e mintMergeVersionTag -e MintTag -e BorrowedName master -- '*.go' | grep -v '_test\.go'
master:cmd/agent/cli_tasks_rebuild.go:72:	// close loop used to call mintMergeVersionTag → internal/version.MintTag,
master:cmd/agent/merge_run.go:1889:		// to call mintMergeVersionTag → internal/version.MintTag before
# ↑ two COMMENT hits and no call site: the mint is gone, not moved.
#   (At 590d5f33 there were three — a third comment in merge_run.go turned over
#   with the delivery-point sync; the measured COUNT moves with master, the
#   verdict does not. Re-run in §6.)

# I3 — is the age machinery master's? (describe/landing renderers, and the name machinery they fed)
$ for s in Describe LandingText AgeText ReportAgeKey RevisionCommitDate roundAge TagCommitDate UnnamedLandings BorrowedName; do
>   printf '%-24s hits=%s\n' "$s" "$(git grep -F "$s" master -- internal/version/ | wc -l)"; done
Describe                 hits=0
LandingText              hits=0
AgeText                  hits=0
ReportAgeKey             hits=0
RevisionCommitDate       hits=0
roundAge                 hits=0
TagCommitDate            hits=0
UnnamedLandings          hits=0
BorrowedName             hits=0
# ↑ Describe and LandingText both 0: the whole DESCRIPTION renderer the archived
#   diff extended is gone, not merely missing the age line.

# WHO retired it, and is that commit master history?
$ git show -s --format='%H %ci %s' ddbc3df1
ddbc3df13bd3b6e0e564b7bd4a138a1bd63d868b 2026-09-24 09:26:23 +0800 feat(version): the binary's identity is its own — internal, constant, checkout-independent
$ git merge-base --is-ancestor ddbc3df1 master; echo $?
0                                    # the retirement IS master history
$ git show --stat --format='' ddbc3df1 -- internal/version/ | grep -E 'internal/version/'
 internal/version/describe_parse_test.go       | 136 ----
 internal/version/git.go                       |  67 --
 internal/version/name.go                      | 370 ----------
 internal/version/name_test.go                 | 288 --------
 internal/version/self.go                      | 107 +++
 internal/version/tag.go                       | 147 ----
 internal/version/version.go                   | 985 +++-----------------------
 internal/version/version_report_probe_test.go | 172 ++---
 internal/version/version_test.go              | 546 --------------

# the lane's residue — the tags the retired mint left behind, and their namespace
$ git tag -l 'version/*' | wc -l
184
$ git for-each-ref --sort=-creatordate --format='%(refname:short) %(creatordate:iso8601)' refs/tags/version/ | head -3
version/v20260924-0142-report-date-intermediate-snapshot-so-reads-one-e4bf1e19 2026-09-24 09:42:41 +0800
version/v20260924-0105-delivery-point-sync-master-delivered-before-536eb373 2026-09-24 09:05:45 +0800
version/v20260924-0053-report-record-two-pre-existing-run-truncation-8f96fcf0 2026-09-24 08:53:25 +0800
# ↑ the newest name is 2026-09-24 09:42, i.e. the last moments of the mint's life.
#   Nothing has minted since: the lane's last producer is ddbc3df1 (09:26) plus
#   its own tail, and the archived branch's whole complaint (a name three days
#   behind the code) is moot when no name is produced or read at all.
```

## 3. Why re-implementing would be a regression

`ddbc3df1` is not an accidental deletion: it is an epic with its own doc
(`devdoc/version-identity.md`, 300 lines), its own gates, and a stated reason. The
live tree carries the reasoning at both old call sites — `cmd/agent/merge_run.go`
already says it, verbatim, where the archived diff wanted the mint:

> *"That lane is deleted in full — the call, the revert-handle tag drop it fed
> (`FailedMergeRevert.MintedTag`, `RestoreMaster` step ③) and the helper itself.
> Merging is git work; cutting a release tag into a repository gopi merely merged
> is the repo owner's decision, and gopi's own identity is now the compiled-in
> `internal/version.Version`, which needs no name cut before the build runs."*

and `cmd/agent/cli_tasks_rebuild.go` names the cost of the lane: *"183 `version/*`
tags accumulated this way … writing a tag into a repository gopi does not own is
the repo owner's release decision, never a side effect of a merge."*

Two anti-regression gates now hold that decision, and re-implementing I1–I3 would
break both — they are the "stronger guard for the same behaviour" whose absence is
the signal a salvage is still LIVE:

- `cmd/agent/version_identity_test.go: TestVersionDoesNotBorrowANameTheCheckoutCarries`
  (`ddbc3df1`) — the identity must be byte-identical whatever `version/*` tag the
  checkout carries. **I1/I2 are exactly that borrowing**: a repo whose position gets
  a name is a repo whose `gopi version` changes with its checkout.
- `internal/version/version.go`'s package doc names the deletion as the design:
  *"What is NOT here any more — and the reason is that nothing in the live tree read
  them: the minted-name machinery … They were deleted whole rather than kept behind
  a flag, because a dormant mint is the coupling this epic exists to remove."*

The one place the retired lane is still half-alive is history, not code: 184
`version/*` tags and the exported `internal/version.Namespace` constant, kept only
because the readonly fixtures that pin the display/report readers tag positions
with it (`Namespace`'s own doc says *"NOTHING IN PRODUCTION WRITES IT"*). That is
a deliberate, documented residue — not a socket for a new mint.

## 4. Honest gaps

- **The archived diff was read, not executed.** Its 169-line test file
  (`merge_version_name_test.go`) has no counterpart to run against master: its
  fixtures (`mergeTestEnv`, `mergeFastSeams`, `captureMergeStdout`,
  `integrationBranchForTest`) live in master's `merge_run_test.go`, but the
  behaviour under test — a *minted tag* — is the retired lane, so there is no
  honest way to re-run it here. Rule 31's "run the surface you changed" is
  answered by the measured commands above (this deliverable changes no surface).
- **I did not verify the epic brief's "master moved 8 days past it" framing**:
  the measured distance is the branch's own merge-base on master (`d2703f1f`,
  2026-09-21) with master at `590d5f33` — the *retirement*, not the distance, is
  what kills the intent.
- **`cde8b4df` (2026-09-28, "salvage: uncommitted workspace content") also shows up
  under `git log -S 'func MintTag' --all`** — it is an unrelated-history rescue
  snapshot that re-adds the deleted file, not a revival on master; `master` has no
  `MintTag`, which the production grep above proves.
- **No production code, and therefore no new test, is added by this document**:
  a test asserting "the mint does not come back" already exists, and it is
  `TestVersionDoesNotBorrowANameTheCheckoutCarries`.

## 5. References

- retirement: `ddbc3df1` (2026-09-24), `devdoc/version-identity.md`, `devdoc/version-meaningful.md`
- the lane's own history: `3f6df7b7` (2026-09-13 created), `22951fe8`, `a106b98f`, `e3be5af1`, `2cf202cb` (2026-09-21), deleted by `ddbc3df1`
- the archived change set: `88a0ae76` (2026-09-21), merge-base `d2703f1f`
- sibling salvage of the same lane, landed the day before this one:
  `devdoc/version-identity-salvage-20260928.md`
- evidence for every figure above:
  `~/.gopi/sessions/01a0e922-7b29-7b81-9eb8-45e834295849/evidence/measure.log`,
  `…/measure2.log`

## 6. Re-measurement at the merge base — master moved during this run

The delivery-point sync pulled master forward while this document was being
written (`590d5f33` → `a80a9f8b`, "delivery-point sync: merge master into the
delivered branch before close-out"), so every deciding figure was re-run against
the tip the branch would actually land on, and the MERGE_PROBE was repeated there:

```bash
$ git rev-parse --short master
a80a9f8b
$ git merge-base --is-ancestor 590d5f33 master; echo $?     # this branch's base
0
$ git merge-base --is-ancestor ddbc3df1 master; echo $?
0                       # the retirement is still master history at the new tip

$ for s in mintPositionVersionName nameUnnamedPosition handedOverBranchNames \
           mergeTipMovedPast landedBranchNames mergeRepoErrored TagAt; do
>   printf '%-30s hits=%s\n' "$s" "$(git grep -F "$s" master -- '*.go' | wc -l)"; done
mintPositionVersionName        hits=0
nameUnnamedPosition            hits=0
handedOverBranchNames          hits=0
mergeTipMovedPast              hits=0
landedBranchNames              hits=0
mergeRepoErrored               hits=0
TagAt                          hits=0

$ for s in Describe LandingText AgeText ReportAgeKey RevisionCommitDate roundAge \
           TagCommitDate UnnamedLandings BorrowedName; do
>   printf '%-24s hits=%s\n' "$s" "$(git grep -F "$s" master -- internal/version/ | wc -l)"; done
Describe                 hits=0
LandingText              hits=0
AgeText                  hits=0
ReportAgeKey             hits=0
RevisionCommitDate       hits=0
roundAge                 hits=0
TagCommitDate            hits=0
UnnamedLandings          hits=0
BorrowedName             hits=0

$ git merge-base --is-ancestor 88a0ae7648b5182fc7a5c29c461443bada916948 master; echo $?
1                       # the archived tip is still not in master

# MERGE_PROBE repeated on the NEW tip (detached master worktree)
$ git worktree add --detach <probe> master && cd <probe>
$ git merge --no-commit --no-ff rework/version-name-follows-landing-salvage
Auto-merging devdoc/DOC-INDEX.tsv
Automatic merge went well; stopped before committing as requested   # merge rc=0
unmerged paths: 0
staged: devdoc/DOC-INDEX.tsv
        devdoc/version-name-follows-landing-salvage-20260928.md
$ git merge --abort; echo $?   # 0
$ git rev-parse -q --verify MERGE_HEAD || echo "MERGE_HEAD cleared"   # MERGE_HEAD cleared
probe porcelain: 0     worktree removed
```

Nothing in the verdict moved: same three RETIRED verdicts, same deciding commit
`ddbc3df1`, and the merge of this branch into the current tip is a clean one with
no unmerged path.
