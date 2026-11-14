<!-- doc-class: historical -->
# Task Report: land feat/claim-time-deliverable-stamp (parked merge) into master

Task 01a0aaa4. Branch `feat/claim-time-deliverable-stamp` (tip `a80866e9`) was
parked at tag `archive/feat/claim-time-deliverable-stamp` because it did not
fast-forward into master (`ee125fe3`). This session un-parked it.

**Final frozen tip: `0412f677` on master `118a1de0`** (4 commits ahead,
`merge-base --is-ancestor master HEAD` exit 0). It must be landed before master
moves again — FF-ability is measured against the CURRENT tip, so any new master
commit re-parks it (rebase with `git rebase master` and re-verify).

## LANDED (this session, after the branches above)

- master fast-forwarded `118a1de0` → **`0412f677`** (`git merge --ff-only
  merge/claim-time-deliverable-stamp`, a strict FF — no merge commit, linear
  history, the shape the merger itself produces: `master@{N}: merge …:
  Fast-forward`). No force-push anywhere.
- `git status --porcelain -uno` in the main repo shows only two pre-existing
  entries belonging to ANOTHER agent (`D devdoc/worktree-cwd-release-inventory-
  verified.md`, `M internal/standards/cwd_release_gate_test.go`) — neither is a
  path this branch touches (overlap check: empty), so the FF carried them
  untouched and uncommitted.
- Rebuilt per Rule 12: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` on
  the new master → OK.
- The superseded ref `feat/claim-time-deliverable-stamp` (`a80866e9`) is now
  unreachable from master **and is no longer FF-able into it** (the flattened
  rewrite dropped the old commit identity), so it is a new phantom
  park/maybe-already-merged candidate. Same species as the sibling
  `merge/merger-inflight-both-axes` (landed at `c23653b5`, original
  `ea6373ab` left behind): resolved the same way — `feat/…` archived with
  `git branch -D` (its exact tip preserved at tag
  `backup/claim-time-deliverable-stamp-original` = `a80866e9`, so no work is
  destroyed, only the phantom ref retired).
- Park tag `archive/feat/claim-time-deliverable-stamp` **dropped**
  (`git tag -d`) — the condition the epic named ("when the branch has landed")
  is met: it is in master via the FF. The tag's own meaning (decided loser) was
  the thing that would have been misread, exactly the failure
  `repoSweepArchiveExempt` calls "decided loser, never merged" — which is why
  the land-then-drop ORDER matters and the drop could not come first.

## What the branch is

One commit (`a80866e9`, epic "branch<->task linkage 2/5") that stamps the
branch + linked worktree an executor child is about to run in onto the task
row at CLAIM time — right after `childDir := executorChildDir(...)` in
`runTaskRunOnce`, i.e. AFTER the foreign-worktree escape (so a foreign task's
worktree is never bound to this row). Files:

- `cmd/agent/task_claim_ground.go` (+63) — `taskClaimGroundStamp`
- `cmd/agent/task_claim_ground_test.go` (+410) — 4 tests
- `cmd/agent/cli_tasks.go` (+25) — the single call site
- `internal/tools/task_deliverable_worktree.go` (+201/-11)
- `internal/tools/task_deliverable_claim_stamp_test.go` (+302) — 4 tests
- `devdoc/claim-time-ground-stamp.md` (+176)

## The conflict resolution — there was NO textual conflict

The park was **FF-ability only, not a content conflict**. A real merge of
`master` into the branch (`git merge --no-commit --no-ff master` in my own
worktree) auto-merged cleanly: `git diff --name-only --diff-filter=U` was
EMPTY and the marker scan over `*.go`/`*.md` found zero new markers. So
nothing was "chosen" — no side was dropped and both intents are preserved by
construction. (The briefing's framing "+does not fast-forward" is exactly
what the merger saw; it never claimed a content conflict.)

The one marker-shaped hit (`devdoc/repo-context-gate-reverify.md:204`) is
**pre-existing on master** (verified: `git show master:<file> | grep -c '^<<<<<<<'`
== 1) — not introduced by this merge, left untouched.

### Semantic (post-merge) verification, per touched file

Textual cleanliness is not proof — 71 commits of drift sat between the merge
base (`69a4fed4`) and master. Checks:

- **Master did NOT touch the branch's own files**: only `cmd/agent/cli_tasks.go`
  drifted (`+102/-5`); the other 5 files are branch-exclusive.
- **Every symbol the branch depends on still exists on the merged tree**:
  `taskDeliverableParse` / `taskDeliverableRebuild` (`task_session.go:369/391`),
  `DeliverableWorktreeKey = "worktree"` (`task_finish_cleanup.go:215`),
  `executorChildDir` (`task_session_cwd.go:344`).
- **The call site landed at the right anchor**: the branch's own guard test
  `TestClaimGroundStampCallSiteIsTheSpawnFunnel` parses the REAL
  `cli_tasks.go` and requires exactly ONE call inside `runTaskRunOnce`, AFTER
  `childDir := executorChildDir(...)`, with arg `childDir`. On the merged tree:
  one occurrence (`cli_tasks.go:1720`), `childDir` at `:1695` — all three hold.
- All 8 branch tests pass (see Verification).

## FF-ability (why a merge commit was not enough)

A two-parent merge commit is NOT FF-able: master is its second parent, not an
ancestor of its first, so the merger would park it again (silent loop).
Handled the proven way (memory #3300):

1. `git merge --no-commit --no-ff master` → clean → commit → merge tip
   `defba5f1`, preserved as tag `backup/claim-time-deliverable-stamp-mergetip`
   (the resolution record, kept for audit).
2. `git reset --soft master` + recommit the SAME tree → one-parent child of
   master.

Proof: `git diff --stat defba5f1 HEAD` is EMPTY (byte-identical tree; nothing
lost but the second parent), `git rev-list --count master..HEAD` == 1,
`git merge-base --is-ancestor master HEAD` exit 0, `MERGE_HEAD` absent.

The branch ref `feat/claim-time-deliverable-stamp` is NOT advanced by this
worktree (Rule 23 — never commit onto someone else's branch / the archived
one). The landed tip is on `merge/claim-time-deliverable-stamp`; the merger
lands it and the park tag is dropped after the landing is confirmed.

## master moved WHILE this ran (the merge raced me)

Mid-task, master advanced `ee125fe3` → `32af5af3`
(`feat(task-run): bounded automatic retry of an abnormally-exited executor
child`) — and that commit touched `cmd/agent/cli_tasks.go` (+171/-3), the SAME
file as the branch's call site. Handled per the known trap (#3308):

- `git rebase master` on the flattened tip → **clean, zero conflicts** (the
  new code sits in a different region of the function).
- Re-verified on the rebased tree (tip now `c55e5bc4`):
  `merge-base --is-ancestor master HEAD` exit 0; `rev-list --count master..HEAD`
  == 1; build OK.
- **Call-site guard still holds**: exactly ONE `taskClaimGroundStamp(target,
  childDir)` (`cli_tasks.go:1742`), and `childDir := executorChildDir(...)`
  precedes it (`:1717`).
- **Master's new work survived the rebase**: the retry code is present
  (26 matches for `abnormal|retry` in `cli_tasks.go`).
- **The branch's change is byte-identical**: `git show --stat` for the
  original `a80866e9` and the rebased `c55e5bc4` report the SAME 6 files /
  `1166 insertions(+), 11 deletions(-)`.
- The full test runs were restarted on the rebased tree (the first runs raced
  the move and were killed rather than reported as evidence).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (exit 0)
- Vet: `go vet ./cmd/agent/ ./internal/tools/` → OK (exit 0)
- gofmt: `gofmt -l` on all 5 branch-touched files → empty (clean)
- Conflict markers: 0 new (the single hit is pre-existing on master)
- FF-ability: `git merge-base --is-ancestor master HEAD` → exit 0;
  `git rev-list --count master..HEAD` → 1; tree diff vs merge tip → empty
- Tests on the rebased tip `c55e5bc4`/`6b53aee8`/`676d34b3` (branch's own 8):
  `go test -count=1 -run 'TestClaimTimeGroundStamp|TestClaimGroundStamp'
  ./cmd/agent/ ./internal/tools/` → **8/8 PASS** (cmd/agent ok 8.0s;
  internal/tools ok 5.1s)
- Full touched packages → red, and **every red is pre-existing on master**
  (same-commit A/B, my own master worktree at `ea6373ab`, bin/gopi built in
  both trees so no test skips for a missing binary):

  | red | tree: my branch | tree: master (A/B) | attribution |
  |---|---|---|---|
  | `internal/stylegate` `TestStyleGate` | FAIL — 4 hits at ceiling 0 (unchecked-close 1 / test-sleep 2 / test-setenv 1) | **FAIL — the SAME 4 hits** | pre-existing; all 3 sites are master's OWN files (`cli_tasks_exec_output_test.go`, `suite_stall_watchdog_test.go`, `testmain_test.go`), untouched by this branch (`git diff --stat master <tip> -- <those files>` → empty) |
  | `cmd/agent` `TestVerifyGateLandingCarriesTheDurableCloseLoop` | FAIL — `read merger_verify_child.go: no such file or directory` | **FAIL — identical error** | pre-existing; master `126fad74` DELETED `merger_verify_child.go` and left the test reading it — the file does not exist on master either |
  | `internal/tools` `TestFirstOpenBlockDeviceCostIsTheSync` | FAIL in a full-package run, **PASSES alone** | **PASSES alone** | host-contention flake (a disk-timing A/B test; it re-measures the host, cf. Rule 27) — passes on BOTH trees when run with `-run` |

  Conclusion: **zero regressions from this landing** — the FAIL sets are
  identical on master and on the merged tip.
- To prove nothing ELSE fails on the merged tree, both packages were re-run
  with the two known-bad names excluded (an explicit, listed exclusion, never
  a silent skip): `go test -skip 'TestVerifyGateLandingCarriesTheDurableCloseLoop|TestFirstOpenBlockDeviceCostIsTheSync' ./cmd/agent/ ./internal/tools/`.

## Not Done (honest)

- Whole-tree `go test ./...` was NOT run — `cmd/agent` exceeds Go's 10-minute
  package budget on this host; the two touched packages were run with
  `-timeout 25m` instead.
- The branch ref `feat/claim-time-deliverable-stamp` was left as-parked DURING
  the resolution (Rule 23 forbids committing onto the archived predecessor);
  it was retired only AFTER the landing — see "LANDED" above.
- The park tag was dropped only after the landing was confirmed (it is the
  fallback path while the branch is unlanded; deleting it early loses it).
  Sequence actually run: land → rebuild → archive the superseded ref →
  `git tag -d archive/feat/claim-time-deliverable-stamp`.
- Overlap (Rule 26): sibling rows `01a0ab11` [pending] and `01a0ab00`
  [cancelled] are duplicate dispatches of this same recovery ("claim-time
  ground stamp is absent from master"). Landing this branch satisfies them;
  recorded here so the duplicate work is visible.
