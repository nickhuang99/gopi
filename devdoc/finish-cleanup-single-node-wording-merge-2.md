<!-- doc-class: historical -->
# Task Report: re-un-park `fix/finish-cleanup-single-node-wording` (2nd cycle — landed again by FF)

**Branch:** `fix/finish-cleanup-single-node-wording` @ `96b68ce5` (== master)
**Park tags found this cycle:** `-045443`, `-045650`, `-050556` (all tip `7264cc5a`)
**Master at landing:** `d2b1e217` → `916dffca` → `96b68ce5` (fast-forward, 2 commits:
the wording fix + this record)
**Landing worktree:** `/home/user/workspace/gopi-fc-verify` (Rule 6)

## Completed

### 1. Cycle 1's landing had been undone — and the re-park was a RE-TAG, not new work

`master` was back at `d2b1e217` (cycle 1's `916dffca` landing gone; reflog shows
`916dffca master@{0}: merge … Fast-forward` then a later reset). The branch ref
was still `916dffca`, whose **parent is `d2b1e217`** — so `branch..master` is
EMPTY and `git merge --ff-only` lands it. The conflict-resolution work had been
done in a prior session; nothing textual was left to resolve.

**The parked tip was stale, and the tag was misleading.** All three park tags
pointed at `7264cc5a`, which is **NOT** an ancestor of the branch (its 5
commits — `5ee57565` wording fix, `749c5732`/`7264cc5a` devdocs, `2349e059`
settings-key refactor, `0863c310` git-sync test — sit on a base `6a105a48` that
master passed long ago). The parked *content* is nonetheless fully on master:
the 5 commits' work exists in `916dffca` as amended/squashed equivalents
(`noServerTargetSkipReason` + `finishCleanupNoPeerDeclaredNote`, all four new
tests, `config.SyncPeersSettingsKey`). Verified file-by-file:
`git diff 7264cc5a 916dffca` on the four touched paths shows only **master-ahead
additions** (the pre-merge gate ①b text, and the pre-merge-aware
`TestFinishCleanupChainPushFailKeepsBranch` assertion) — the 3 "deletions" are
superseded lines *inside* those additions. No unique history in the tag.

### 2. Both sides' intent preserved in the landed tree

- master's pre-merge gate: `finishCleanupPreMergeStep` /
  `internal/tools/task_finish_premerge.go`, referenced from the chain arm.
- branch's two-cause wording: `finishCleanupNoPeerDeclaredNote` (`"single-node: "
  + config.SyncPeersSettingsKey + …`) and `noServerTargetSkipReason`, shared by
  the chain arm (`task_finish_cleanup.go:1057+`), the recycle arm and the
  git-sync face (`task_sync_git.go:200`).

### 3. Park tags dropped (briefing's gate: only after the branch lands)

The parked tip is preserved as `backup/finish-cleanup-single-node-wording-parktip`
(`7264cc5a`) before the drop, so the archive is a ref rename, never a delete:

```
git tag -d archive/fix/finish-cleanup-single-node-wording-045443 \
           archive/fix/finish-cleanup-single-node-wording-045650 \
           archive/fix/finish-cleanup-single-node-wording-050556
```

`git tag -l 'archive/fix/finish-cleanup*'` → empty. Class-attribution residue
(not deletions): branch refs `fix/…-045443` / `-045650` / `-050556` and the
`hp/*` remote-tracking refs still point at `7264cc5a` (a merger bug: the park
consumes `git branch -m` output as an archive *name*, creating those branch
refs) — left untouched, they are other rows' objects.

## Not Done

- **The park tag is re-creatable by the merger**: the refs above mean a stale
  `7264cc5a` keeps being offered to the merger, which will re-park and re-tag it
  (this is the 2nd cycle). A durable fix is a merger/worker change (system
  layer, Rule 28 → peerbox): park-classify should not mint an `archive/…` tag for
  a tip whose content is already an ancestor-by-content, and `git branch -m`
  output must not be reused as a tag name. **Not fixed here** — out of scope for
  a conflict-resolution task; would need its own dispatched row.
- No whole-tree `go test ./...` (`cmd/agent` exceeds Go's 10m package budget on
  this host); coverage = build-all + the full owning package (cycle 1, 497.9s
  green) + focused wording tests on the landed tip this cycle.
- Worktree `/home/user/workspace/gopi-fc-verify` left for the merger sweep
  (branch tip == master, tree clean, nothing uncommitted left behind).
- This cycle's landing is the doc record only (`96b68ce5`); the *code* half
  landed as `916dffca` and is unchanged.

## Verification (at `916dffca`)

- FF-ability: `git merge-base --is-ancestor fix/finish-cleanup-single-node-wording master`
  → exit 0 (branch is an ancestor of master ⇒ landed).
- Landing: `git reflog show master` → `96b68ce5 master@{0}: merge fix/finish-cleanup-single-node-wording: Fast-forward`
  (and `916dffca master@{1}:` the same for the first half).
- Build: `CGO_ENABLED=0 go build ./...` on master `96b68ce5` → exit 0.
- Tests (landed master `96b68ce5`, this cycle):
  `go test -count=1 -run 'FinishCleanup|SyncGit' ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools 7.780s`; plus the narrower
  `-run 'FinishCleanupNoServerTarget|FinishCleanupGoneWorktree|FinishCleanupNoPeerWordingLiteralGate|TestGitSyncClientPassNoServerNamesTheCondition|FinishCleanupChainPushFailKeepsBranch'`
  → `ok … 6.241s`.
- Park-tag hygiene: `git rev-parse backup/…-parktip` → `7264cc5a`; the archive tags → empty.
- No force-push: `git reflog show fix/finish-cleanup-single-node-wording` shows
  only the local `commit (amend)`; the branch was never rewritten by this cycle.

## Deliverable / recovery (Rule 6d)

- Branch `fix/finish-cleanup-single-node-wording` @ `916dffca` **is master** — the
  commit lives in the shared main repo `/home/user/work/GoPi`. No push/clone holds
  the only copy.
- Recovery: `git show master:devdoc/finish-cleanup-single-node-wording-merge-2.md`.
- Superseded park tip kept at tag `backup/finish-cleanup-single-node-wording-parktip` (`7264cc5a`).
