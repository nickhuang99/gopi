<!-- doc-class: historical -->
# The merge candidate set: a branch a LIVE task owns is not a candidate

Epic: `merge-candidate-live-owner-fence` · branch `fix/merge-live-owner-fence`
· 2026-09-22

## The exposure

`gopi merge`'s per-branch loop asked every question about the **repository** —
is a worktree holding the branch, does the history relate to master, is the
diff within the ceiling, is the tree dirty, is a branch-rework ticket already
filed — and **none** about the **task**. The two facts are independent, and one
mechanism drives them apart: a run whose worktree was removed out from under it
keeps committing with its cwd on the main checkout. `mergeDetailWorktreeHeld`
then reads "nobody is using this branch" at exactly the moment somebody is.

Measured 2026-09-22: `fix/jieba-token-absorbs-search-kw` had an `in_progress`
owner row, a live worker, and no worktree — a full merge candidate, eligible to
be conflict-measured, handed to a merge gopi, and landed. A terminal row for
the same branch (`completed`) existed too, from a previous life of the branch.

## What was added

`mergeLiveOwnerParks(repo, branch)`, called **first** in the loop — before the
hand-over counter, before any conflict is measured, before any ref can move.

Three states, never two:

| state | answer | consequence |
|---|---|---|
| no owner row | `(false, "", nil)` | the branch reaches the ordinary gates, unchanged |
| a LIVE owner | `(true, detail, nil)` | skipped, detail names **branch + row + status** |
| ledger unreadable | `(true, detail, err)` | `mergeActionError`, run exits non-zero |

"It could not look" is deliberately not "nobody is there" — a wedged ledger must
never widen the candidate set by default (Rule 15, and `TaskForBranch`'s own
header states the same contract).

### Inputs reused, not reinvented

- **Loader**: `tools.TaskForBranch` — the ONE branch→row loader the ref guard
  already uses. No second loader, no hand-rolled `SELECT … LIKE`.
- **Candidate source**: still git refs. The check is decision-time only; a
  ledger name git does not have cannot be merged, compiled or fast-forwarded
  (memory #3709).
- **Live status list**: `tools.isLiveTaskStatus`, carried on the resolved row as
  `TaskForBranchOwner.Live`.

### `LIVE` is not `!Terminal`

`taskTerminalStatus` answers completed/failed; **`cancelled` and `verified` are
neither** live nor terminal-by-that-predicate. A caller reading `!Terminal` as
"live" would park a cancelled branch forever — the mirror image of the defect
this fence fixes. Hence the explicit `Live` field rather than a derived one;
both callers test `Live`.

### One resolution policy, two surfaces

`TaskForBranch` deliberately answers with **every** row that names a branch (the
in-flight run and the completed row that finished with it commonly share a
name). A fence needs **one** answer, and so does `gopi merge --status`, whose
owner column previously came from the archive face's loader — which prefers the
**decided** row, right for burial bookkeeping and wrong for "who owns it right
now". `tools.TaskForBranchResolvedRow` is that one answer, and it is a fixed
policy: live outranks any other row; with no live owner, the newest row.

`renderArchiveRows` now renders that resolved owner instead of the archive row's
own field. Where the resolver cannot answer, the archive row's field is kept as
the fallback, so a reader never loses a fact the surface used to print.

### Two deliberate carve-outs

1. **A branch-rework carrier is handed back to the pre-existing arm.** Such a
   row is live by status, but it is a *queued ticket*, filed by this command's
   own ceiling arm, and the run already has a named arm for it (the hand-over
   cap's `mergeDetailReworkLaneOwnsFmt`). Parking here would shadow that arm's
   sentence with a weaker one and would freeze the branch the moment the ceiling
   filed for it — i.e. before the hand-over the ceiling deliberately still
   allows. The predicate used is that lane's own dedup probe
   (`tools.MergeHandoverReworkLive`), not a second reading of the row.
2. **A row recording a different repo does not park.** The same branch name
   legitimately exists in two repos (two hosts, two deliverables), so
   `mergeOwnerLookupForRepo` keeps only rows that name this repo — or name
   none, because "the blob did not say" is unknown, never a mismatch (the ref
   guard's rule).

### The subtlety worth remembering: same repo, different path

`mergeOwnerLookupForRepo`'s repo test started as a path comparison and was
**wrong in the silent direction**: a worker submits from
`/home/user/workspace/gopi-<task>` (its worktree) while the settings table
sweeps `/home/user/work/GoPi` (the main checkout). Those are one repository —
they share a git dir — and string equality called them different, which would
have skipped the fence for **every row the ordinary dispatch path creates**. A
false park is a visible freeze; a missed park is an unfinished delivery on
master with no signal at all.

`tools.MergeSameRepo` now answers with `--git-common-dir` (via
`mergeExecMainRepo`), falling back to cleaned-path equality when neither side
resolves as a repository. Pinned by
`TestMergeLiveOwnerFenceSeesThroughAWorktreePath`.

## `scripts/pre-merge-safety-check.sh`

Added by `bb453e44`, dropped by `07d45184`, absent from master ever since — the
operator's pre-flight check for this exact exposure was missing. Restored, and
rewritten rather than reverted: its old verdict ("**UNSAFE** — tag the bare
branches out by hand before running `gopi merge`") is precisely what the fence
now makes unnecessary, and re-shipping that text would re-teach the workaround
for a fixed bug. It now *predicts the fence* — the parked set, with the row and
status the run will print — and reads its ledger and repo set through the same
resolution the command uses (`GOPI_MEMORY_DB`, `GOPI_SETTINGS`), so the check
cannot report on a different database or repo set than the run. Branch/ref
questions are asked with `git -C <repo>` in the repo they concern; asking them
in the script's own checkout reported every swept row as "branch already gone",
the silent all-clear this check must never give.

## Verification

See `tasks/merge-live-owner-fence-report.md` on the same branch — it carries the
red-then-green transcript, the real `gopi merge` run, and the script/command
agreement run.
