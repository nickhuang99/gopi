<!-- doc-class: historical -->
# Task Report: merge-conflict resolution — feat/rebuild-guard-tracked-only into master

## Completed
- Reproduced the merge in this worktree with `git merge master --no-commit --no-ff`
  (head 85d744e6, master 59455092). Exactly **one** conflicted file / one hunk:
  `internal/testutil/glm_night_watch_test.go` (~line 395, inside the GLM night-watch
  shell-probe test's `t.Skipf` reason). No other unmerged paths.
- Resolved the hunk and removed every marker.

### The hunk and why master's side wins
- **ours (feat/rebuild-guard-tracked-only)**: split the skip reason into two adjacent
  string literals and put `//stylegate:ignore` on the split line — i.e. our side tried
  to annotate the skip *inside* the concatenation.
- **theirs (master, f6608c2e)**: collapsed each skip reason back into a **single
  literal**, deliberately, with a comment stating the test-skip rule requires the
  reason be one literal (the stylegate annotation did not land in the site's accepted
  line span, so the multi-literal form stayed red).
- Ours and theirs are not additive here: they are two competing fixes for the SAME
  site, and master's is the later, superseding decision (it explains why the split
  form — our approach — does not pass the gate). Resolution = master's single-literal
  form. Nothing of ours is lost: our side's other, unrelated work in this file is
  preserved (see below), and master's second single-literal skip
  (`opPathWorkerBin`) merged cleanly without a conflict.

### Preserved from our side (merged cleanly, verified in the committed tree)
- `//stylegate:allow-sleep` annotation on `time.Sleep(probePollInterval)` (line 617).
- `//stylegate:ignore` annotation on the `filepath.WalkDir` in `listTree` (line 814).
Ground truth: `git diff 59455092 HEAD -- internal/testutil/glm_night_watch_test.go`
shows ONLY those two annotation lines — i.e. the file is master's version plus
exactly our two unrelated annotations. No third-party change was dropped.

## Not Done
- No full-suite (`go test ./...`) run — explicitly out of scope per the task
  (known pre-existing full-suite hang); bounded checks only.
- Did not merge/tag/clean any branch or worktree: that is the merger's job. No push.

## Verification
- Conflict markers: `git grep -nE '^(<<<<<<<|=======$|>>>>>>>)'` → **zero hits**.
- Unmerged paths: `git diff --name-only --diff-filter=U` → **empty**; `git status --porcelain` → clean.
- Commit: **d264a18d** "resolve merge feat/rebuild-guard-tracked-only into master",
  parents 85d744e6 (ours) + 59455092 (master), on branch
  `feat/rebuild-guard-tracked-only`.
- Build: `go build ./...` → **exit 0**.
- Touched-package tests: `go test ./internal/testutil/ -run 'Night|Skip|GLM|Glm' -count=1`
  → **ok 42.007s (exit 0)**; `go test ./internal/stylegate/ -count=1`
  → **ok 14.394s (exit 0)** (the stylegate package validates the annotations this
  hunk is about).
- No post-commit fix was needed; the commit was not amended.

## Recovery
Branch `feat/rebuild-guard-tracked-only` in /home/user/work/GoPi (worktree-shared refs,
no push needed): `git show d264a18d` / `git show feat/rebuild-guard-tracked-only:devdoc/merge-resolve-rebuild-guard-tracked-only.md`.
