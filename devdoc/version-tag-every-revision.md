<!-- doc-class: historical -->
# Task Report: the merger names EVERY position it lands

Epic: "merger should tag for each revision — whenever merger merges some new
feat or bugfixing, it should simply tag the commit when merging successful, so
gopi version would use the meaningful tag name as version name."

Most of the mechanism already existed (`internal/version.MintTag` + the mint in
`serveRebuildAfterMerge`). This task found and closed the places where a
**successful landing produced no name**, measured on the real repo.

## Completed

### 1. The measured hole: a kept gated (bugfix) landing was left unnamed

Production evidence, measured on master (not synthesised):

```
$ git log --first-parent --merges -3 master
30d1a6f7  Merge branch 'feat/guard-test-daemon-env-isolation'   tag=version/v20260913-1421-30d1a6f7
8f4042b3  Merge branch 'fix/session-record-classification'      tag=version/v20260913-1411-8f4042b3
e8e94ca0  Merge branch 'fix/cmd-agent-inherited-contract-reds'  tag=            ← NO NAME
```

`e8e94ca0` was a **kept bugfix landing** (merger log 21:49:47: `merge-verify
PASS: branch fix/cmd-agent-inherited-contract-reds kept on master (b84eb68d8a36
→ e8e94ca0688b)`). It carried no version tag, and the binary on master still
reported the SWEEP's older name `v20260913-1411-8f4042b3`.

**Root cause (5-why).** The verify gate lands the gated branch itself
(`tools.TaskMerge` in `merger_verify_child.go`), then calls
`serveRebuildAfterMerge(repo, gatedRevert(...))`. `gatedRevert` returned a
handle with `MergedBranches: []string{}` — and the mint was gated on the handle
being non-nil AND the name was taken from the handle's branch list. So the
landing leg reached the mint either not at all (nil handle, the revert case) or
with a stale/empty branch list naming whatever position the sweep had left.
The mint was coupled to the SWEEP's bookkeeping instead of to the repo's
position.

### 2. Fix — the mint follows the POSITION, not the caller's handle

`cmd/agent/cli_tasks_rebuild.go`:
- `serveRebuildAfterMerge` now mints **unconditionally** (nil handle included):
  within one merger pass the sweep and the gate land one after the other, and
  each position must be named. `mergedBranchesOf(revert)` reads the optional
  handle (nil-safe, one place).
- `mintMergeVersionTag` refuses to re-name a position that already has a name:
  `gopiversion.TagAt(repo)` is the single "is this revision named" predicate,
  and the refusal is **logged** (`already named by %s — no new name minted for
  this round`) rather than silent. This is what keeps the second landing leg
  from stacking a second timestamped label on the first leg's revision.
- The name still rides the handle when there IS one, so `RestoreMaster` step ③
  still drops a name whose position the failed-build auto-revert unwound.

`cmd/agent/cli_tasks.go`:
- `gatedRevert(repo, capture, reverted, gatedBranch)` now carries the gated
  branch, so the gate's landing names itself with the branch that got it there
  (`git show <tag>` → "what does this version entail").

### 3. The MANUAL merge face minted nothing at all

`runTaskMergeRebuild` (the `gopi task merge` tail) rebuilt the binary but never
named the position — the one landing path with no mint, so a binary built by
the operator's own merge introduced itself with a bare revision. It now mints
first (before the build reads the display), with the landed branch names taken
from `tools.MergedBranchNames(counts.Outcomes)` — the same function the merger
sweep uses, now exported so the two faces cannot disagree about "landed".

### 4. The adoption leg minted nothing

`serveAdoptionSweep` (in the merger tick) COMMITS push-back branches into
master (`AdoptionSweep` ③a) — a real landing. It now mints for the adopted
branches via `mintAdoptionVersionTag`, after the report line.

### 5. Tests (all new, all in `cmd/agent/cli_tasks_rebuild_test.go`)

- `TestServeRebuildMintsForAPositionItDidNotLand` — the acceptance for the
  hole: sweep leg mints, gate leg (no handle) lands a NEW position and that
  position comes out named, the sweep's own name untouched, and
  `gopiversion.Describe` reads the gate's name.
- `TestServeRebuildDoesNotRenameANamedPosition` — idempotence at the seam: an
  operator's hand-cut `v9.9.9-handcut` keeps the position to exactly ONE name.
- `TestRunTaskMergeRebuildMintsTheName` — the manual face names its landing
  (with the branch in the message) and lands-nothing mints nothing.

## Acceptance walk-through (MEASURED on the real binary, not synthesised)

Scratch repo `/dev/shm/gopi-vtag-accept-5ecP/repo`, built binary
`bin/gopi` from THIS branch, settings `{"repos":{}}` (no build contract).

```
$ gopi task merge --repo $R fix/one-line-bugfix
merge fix/one-line-bugfix   merged   clean merge, branch deleted
task merge (no-submit): merged=1 deleted=0 …
[merger] [version] minted version/v20260913-1446-114cd88 (names the position this round landed; landed: 1 [fix/one-line-bugfix])
$ git tag -l
version/v20260913-1446-114cd88
$ git tag -l -n99 version/v20260913-1446-114cd88
gopi version tag — post-merge position 114cd88
    merged branches:
      fix/one-line-bugfix

$ gopi task merge --repo $R fix/second           # a second round, a NEW position
[merger] [version] minted version/v20260913-1447-307f098 (… landed: 1 [fix/second])
$ git tag -l
version/v20260913-1446-114cd88
version/v20260913-1447-307f098

$ gopi task merge --repo $R dup-already-merged   # lands at an ALREADY-named position
merge dup-already-merged   deleted   already merged into master — branch -d
[merger] [version] already named by version/v20260913-1447-1c48b05 — no new name minted for this round
$ git tag -l --points-at HEAD
version/v20260913-1447-1c48b05                  # exactly one name — never doubled
```

The last line is the idempotence guard as a user meets it: a round that lands
at a position which already carries a name reports the refusal and leaves ONE
name on that revision.

## Measurement pitfall (worth a future agent's time)

Every entry in `~/.gopi/logs/merger.log` carries a wall-clock stamp
(`[merger] pass start 21:46:19`), but the file is written by SEVERAL live
merger processes across the day, so its lines INTERLEAVE — reading "forward"
from a mint line lands you in a different run's pass and inverts the
conclusion (this happened: the gate landing looked as if it had minted a stale
name; the timestamp brackets show the round that landed e8e94ca0 minted
nothing). Bracket log reading by TIMESTAMP, and cross-check the git side
(`git log --first-parent --merges master` + `git tag --points-at <sha>`),
which is append-only and cannot interleave.

## Environment note (not a defect, but it bit this task)

This task's worktree at `/home/user/workspace/gopi-version-tag-every-revision`
was **deleted twice while the task was running** (a resident merger's
dead-holder / finish-cleanup reclaim — no merger.log line names it, so the
reclaimer is the worktree-sweep arm). The first deletion landed DURING a full
`go test ./cmd/agent/` run and produced 37 failures, every one of them a
fixture-path read error (`open ../../tests/…: no such file or directory`) or
`getwd: no such file or directory` — i.e. a cwd that no longer existed, not a
code defect. Both deletions were recovered with `git worktree add <path>
feat/version-tag-every-revision` because **the commit was already on the
branch**: the deliverable survived, only the working copy vanished (the Rule
23 / "branch vanished — check merged, not destroyed" lesson). Re-running with
the directory present gives a clean result.


## Not Done

- No change to the release path (`-ldflags`/hand-cut tags): `TagAt` honours a
  `v*` release tag exactly as `Describe` already did, so a position the
  operator named by hand is left alone.
- Pre-existing, recorded not changed: `MintTag` builds the name from the wall
  clock, so a re-run at the same position with no existing tag would mint a
  different stamp. Idempotence (and now the re-name refusal) is what makes this
  invisible in practice — the first name wins and is never replaced.
- The verify-gate landing does not mint its OWN pre-build name before the
  verify child judges; naming happens at the pass's close loop. A FAILED
  verdict restores master to the pre-merge tip, so naming earlier would create
  a name that must then be dropped — naming at the close loop keeps name and
  position bound to the same, final, state.
- Branches landed by `git push` into a remote-tracking ref are named by the
  adoption leg only when `AdoptionSweep` reports them adopted (its ① gate
  requires a COMPLETED ledger task paired with the branch); a push-back branch
  with no ledger row is still never merged, so it is not a landing to name.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — OK; `gofmt -l` clean on every
  touched file; `go vet ./cmd/agent/ ./internal/version/ ./internal/tools/`.
- Tests: `go test ./cmd/agent/ -run 'TestServeRebuild|TestRunTaskMergeRebuild'`
  — 15/15 PASS (12 pre-existing + the 3 new ones).
- Tests: `go test ./cmd/agent/ -run 'MergeVerify|Version|TaskMerge'` — ok
  (22.3s).
- Tests: `go test ./internal/version/ ./internal/tools/` — ok (1.5s / 109.2s).
- Tests: `go test ./internal/standards/ ./internal/stylegate/` — ok (the
  machine-checkable gates, Rules 16-21).
- **Red-proof (the guard really guards).** Restoring the pre-fix shape in
  `serveRebuildAfterMerge` (`if revert != nil { revert.MintedTag =
  mintMergeVersionTag(repo, revert.MergedBranches) }`) makes the new acceptance
  test FAIL:

  ```
  --- FAIL: TestServeRebuildMintsForAPositionItDidNotLand (0.06s)
      cli_tasks_rebuild_test.go:402: a landing reached with no handle must still
      be NAMED (this is the e8e94ca0 hole); out:
          [merger] [rebuild] ok: …/repo already current at 2c7313b (no rebuild needed)
  ```

  With the fix restored the same test passes. The out-line shows the exact
  old behaviour: the round rebuilds and reports, and never names the position.

## Files

- `cmd/agent/cli_tasks_rebuild.go` — mint follows the position; idempotence
  refusal; `mergedBranchesOf`; the manual face mints.
- `cmd/agent/cli_tasks.go` — `gatedRevert` carries the gated branch.
- `cmd/agent/cli_tasks_merge.go` — hands the landed branch names to the tail.
- `cmd/agent/cli_tasks_adopt.go` — the adoption landing mint.
- `internal/version/tag.go` — `TagAt` (the one "already named" predicate).
- `internal/tools/repo_sweep.go` — `MergedBranchNames` exported (one definition
  of "landed" for every face).
- `cmd/agent/cli_tasks_rebuild_test.go` — the three new tests.
