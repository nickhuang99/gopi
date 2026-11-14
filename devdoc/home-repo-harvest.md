<!-- doc-class: historical -->
# Task Report: Harvest mistaken ~/.git commits into the correct repos

Epic: gopi mistakenly committed assets at `~/.git` (a repo rooted at $HOME) that
belonged in the current GoPi project or a remote peerbox repo. Fix by harvesting
those assets into the correct git repo in cwd (~/work/GoPi).

## Audit facts (what ~/.git actually was)

`~/.git` = a non-bare git repo rooted at $HOME, master @ `fa5ddb9`, 3 commits,
1 tracked file (`work/claude/evennia-island-init.sh`). `~/work/gopi-review2b/`
and `~/work/claude/` were NOT repos — just untracked dirs inside HOME's repo
scope (this is why `~/work/evennia` "resolved" to `~/.git`).

| # | Commit | Content | Verdict |
|---|--------|---------|---------|
| 1 | 128579f | evennia-island-init.sh (Evennia MUD island init, 6434B) | Wrong repo entirely (evennia banned in GoPi per task-repo-guard). Real home = `~/work/claude/` scratch (un-versioned). NOT pushed anywhere. |
| 2 | 5a44b0b | review2-cli-config-options.md (236L variant, path `work/gopi-review2b/devdoc/`) | Duplicate patch of GoPi's own f91f76a/5db899a review, BUT contains unique P1-C/P1-D findings missing from master's 247L canonical. |
| 3 | fa5ddb9 | deletes the 5a44b0b file | part of the mis-commit churn |

Also verified: HOME repo had no other branches, no stashes, no tags; untracked
`work/gopi-review2b/devdoc/review2-log-hp.md` = byte-identical to GoPi's
rescue/hp-review2-log content (no separate harvest needed).

## Completed

- **Audit** of ~/.git lineage (3 commits, tracked/untracked assets, absence of
  branches/stash/tags) — facts above.
- **Harvest** commit 2's unique 236L variant as
  `devdoc/review2-cli-config-options-variant-20260903.md` → commit `3d6c0a4`
  on GoPi master (worktree-shared; canonical 247L doc untouched; another
  agent's live edits to cli_tasks.go/.test stashed→restored intact).
- **Residual bugs verified live on master and filed as bug tasks**:
  - P1-C `t-1788773479`: settings.json `"compact": "off"` (string) fails the
    whole typed parse (Compact is `*bool`, config.go:141).
  - P1-D `t-1788773479-1`: malformed settings.json → base=nil → ApplyEnv
    nil-panic (main.go:193-196 prints sErr but still passes nil base).
- **Push to remote peerbox**: `master` push was refused (branch checked out on
  peerbox, receive.denyCurrentBranch — normal). Delivered instead as
  `harvest/home-repo-harvest` branch on peerbox @ 3d6c0a4 (verified via
  ls-remote).
- **Neutralized the mis-commit vector**: `~/.git` → `~/.git-harvest-backup-20260904`
  (full forensic backup, all 3 commits verified intact inside). `$HOME` is no
  longer a git repo (`git -C ~ rev-parse` → "not a git repository"). Future
  accidental commits at HOME now fail loudly instead of silently accumulating.
- evennia-island-init.sh left intact on disk at `~/work/claude/` (6434B,
  untracked).

## Not Done (deliberate)

- Merging `harvest/home-repo-harvest` into peerbox's master — merge is PA's job
  (Rule: do not merge own branch; safe-merge guard applies).
- The 3 mis-commits were NOT rewritten/replayed anywhere else; the backup
  preserves the forensic lineage.
- No new home repo was invented for evennia-island-init.sh — the on-disk
  evennia projects (evennia-pipeline-v3, evennia-wagent) are separate
  initiatives; claiming one for this scratch script without operator input is
  scope creep. Documented instead.

## Verification

- Build: N/A — zero code changed in GoPi (docs harvest + git ops only).
- Tests: N/A — same; bug tasks carry the regression-test acceptance criteria.
- Commit: `3d6c0a4` on GoPi master (= tip of peerbox `harvest/home-repo-harvest`).
- Neutralization check: `git -C ~ rev-parse --git-dir` → "fatal: not a git
  repository"; backup log shows fa5ddb9/5a44b0b/128579f.
- ls-remote peerbox: `refs/heads/harvest/home-repo-harvest` → 3d6c0a4.
