<!-- doc-class: historical -->
> **Retired mechanism (merge pass verify/withdraw deletion, 2026-11-01).** The
> post-pass verification and its withdrawal (`internal/tools/merge_pass_verify.go`:
> `CaptureMergePassScope` / `VerifyMergePass` / `WithdrawMergePass`, plus the
> `merge-withdrawn` notify alarm) were DELETED on 2026-11-01 with the last slice
> of the retired top-level `gopi merge` verb — its last production caller,
> `cmd/agent/merge_run.go`, was already gone. **`gopi task merge` does not verify
> the merged tree and does not withdraw a landing.** Everything below that speaks
> of the post-pass in the present tense is the record of a capability that
> existed, measured on the day each page states; it is not a promise about the
> shipped engine. `man/gopi-merge.md` carries the corrected contract.
# The mint is not wired to the live landing path — `gopi merge` lands, nobody names

## The defect

`gopi merge` (`cmd/agent/merge_run.go`) is the ONLY surviving merge path: the
resident merger was RETIRED (`cmd/agent/merger_retired.go`, 2026-09-19), its
rebuild was carried over to the foreground command, and its **mint was not**.
The mint helper `mintMergeVersionTag` existed and worked the whole time; nothing
on the live path called it.

Measured 2026-09-21/22 on the real repo (`/home/user/work/GoPi`):

```
$ git tag -l 'version/*' --sort=-creatordate | head -3
version/v20260918-1400-0317d09a
version/v20260918-1244-5cdae25e
version/v20260918-1114-812f9a60
$ git tag -l 'version/*' | wc -l
146
$ git rev-list --count --merges version/v20260918-1400-0317d09a..master
180
```

180 merge rounds past the newest name, 0 new names. A **pre-fix binary** run
against a clone of that repo confirms the mechanism end to end (not a
deduction — the run is in the report's evidence):

```
--- tags before the pre-fix landing: 146 ---
gopi merge: rebuild after landing in /home/user/scratch/mint-real: ok in 13.450959011s
  .../mint-real fix/probe-prefix-landing landed  fast-forwarded into master, ref pruned
--- tags after the pre-fix landing: 146 ---
$ ./bin/gopi version
v20260918-1400-0317d09a+689 unminted (873ab8937789)
```

A landing that minted nothing, from the binary that ships today.

## The fix (the acceptance's shape: reuse, do not invent)

Two call sites, ONE mint helper, no new mechanism.

### 1. The per-landing mint — `mergeSweepRepo`'s landing arm

In `cmd/agent/merge_run.go`, `if landed { ... }` now mints BEFORE
`tools.RebuildAfterMerge`:

```go
mintMergeVersionTag(repo, landedBranchNames(out, repo))
res := tools.RebuildAfterMerge(repo)
```

- `mintMergeVersionTag` was the helper that landed this (then at
  `cli_tasks_rebuild.go:162`) → `internal/version.MintTag`. The epic forbids a
  second mint function and there is none. **SUPERSEDED (epic version-identity,
  2026-09-24)**: the helper and the per-landing mint are GONE from master — the
  binary's identity is `internal/version.Version`, compiled in, so nothing is
  minted before the build reads it (`cli_tasks_rebuild.go:71` is the tombstone
  that records the removal). This doc is a landed record and keeps its
  measurement; the anchor cell now names the live line.
- The idempotence guard is untouched: the helper's first act is
  `gopiversion.TagAt(repo)`, which refuses to re-name a position that already
  carries a name (a re-run, a sibling face, an operator's hand-cut `v1.0.0`) and
  says so under the existing `mergerVersionTagLog` wording.
- `landedBranchNames` is new and reads the run's own OUTCOMES for this repo
  (`mergeActionLanded` + `mergeActionUnionResolved` — a union-resolved branch IS
  a landing, the same reason it counts for the post-pass verification). It does
  not re-derive "what landed" from git, which would also claim positions another
  actor moved.
- ORDERING (why before the rebuild): the binary that the rebuild leaves behind
  introduces itself with the name of the position it was built from, so the name
  must exist before the build reads it — the ordering the retired
  `serveRebuildAfterMerge` documents at length.

### 2. The run close-out mint — `mintAdvancedPositionAtRunClose` (the hole the per-landing mint cannot see)

**A branch the hand-over CHILD resolves is merged by the child**, inside
`mergeHandOver`, AFTER every per-repo sweep has returned. That merge is in
**no outcome** — `mergeHandOver` records *failures* (`mergeRecordHandOverFailures`),
never landings — so the per-landing mint never runs for it and the run would end
with work on master that no name covers: the operator's complaint, one lane
further along. (`cmd/agent/merge_prompt.md` §"The rebuild is part of landed":
"*If YOU merge (a conflict branch you resolved yourself)*…"— the child is the
second lander on this face and it runs while the command is blocked on it.)

```go
if !verification.withdrawn {
    for _, repo := range repos {
        mintAdvancedPositionAtRunClose(repo, snapshots[repo], opts)
    }
}
```

It is **guarded on a proven movement**, never unconditional: a tag is a side
effect, and an unchanged repo must not grow one. Every refusal is a fact the run
cannot prove —

| refusal | why |
|---|---|
| `opts.noVerify` | no snapshot was taken, so there is no pre-pass tip to compare against |
| `!snap.Captured()` | the capture failed — same absence of a measurement |
| HEAD unreadable, or `HEAD == snap.PreTip` | this run did not move the repo |
| `gopiversion.TagAt(repo) != ""` | the position is already named (per-landing mint or an operator's release tag) |
| `verification.withdrawn` | the batch was reset back to the pre-pass tip — the name the per-landing mint cut before that reset is the "already-named position" the next run must not stack onto |

It mints with an **empty branch list**: "moved" is proven, "which branch moved
it" is not (the child's verdicts are not this run's outcomes), so the tag message
claims nothing this run cannot vouch for.

## The revert-handle question (asked explicitly by the briefing)

**Does the live path need the retired path's revert-handle tag drop? NO — and the
reason is structural, not an omission.**

`serveRebuildAfterMerge` stashes the name on `tools.FailedMergeRevert.MintedTag`
because **mechanism 2** auto-reverts a landing whose rebuild FAILED
(`RestoreMaster` step ③ drops the name with the merge it described). The live
path has no revert on that axis: `tools.RebuildAfterMerge(repo)` is called with
no handle and never unwinds a merge it does not own — this face's own comment
says the same thing ("No revert handle here: this face has no auto-revert").

Its only rollback is the **post-pass withdrawal** (`tools.WithdrawMergePass`,
opt-out `--no-verify`), which resets to the pre-pass tip *after* verification
measured red. A withdrawal unwinds the WHOLE batch, so the name left on the
unwound position degrades to a point no branch carries: a `version/*` name is
DERIVED from a position, not a release claim about a live tip, and the next run
re-names whatever survives. Wiring the drop would mean threading a revert handle
through `tools.RebuildAfterMerge`'s signature — i.e. adding STATE to a command
this epic documents as NO LOCK, NO DAEMON, NO STATE. So the handle is
deliberately not carried. The close-out mint's `verification.withdrawn` guard is
the one place the withdrawal's meaning touches naming at all.

## Reachability audit of the other mint call sites

Verdicts and caller chains: see the report (`tasks/01a0c36d-report.md`,
§"Reachability"). Summary — **all three surviving sites are on the RETIRED
merger/serve chain and none can fire today**; the fourth mint call site (the
`merge-execute` facade `mergerCloseLoopAfterLanding`) *can* fire, and it already
reaches the mint through `serveRebuildAfterMerge`.

## Ticket #4's preserved branch

Verdict: **superseded for the fix**, kept as a defect exhibit; the deciding diff
and the one idea adopted from it (its run close-out mint) are in the report. No
rework of that branch: `rework-backup/fix/version-name-follows-the-landing`
(`46d06b66`) stays as the audit trail.

## Files

- `cmd/agent/merge_run.go` — the two mint call sites + `landedBranchNames` +
  `mintAdvancedPositionAtRunClose`.
- `cmd/agent/merge_run_test.go` — 5 regression tests (2 per-landing, 2
  close-out, 1 reuse-arm pin), all red-proofed.
