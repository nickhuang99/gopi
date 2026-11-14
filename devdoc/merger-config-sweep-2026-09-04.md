<!-- doc-class: normative -->
# Architecture Fix v2 — Config-Driven Pure-Git Merger Sweep (2026-09-04)

## Summary

Reworked the GoPi merger from a task-oriented / DB-driven design into a
**pure git sweep driven by a config repo list** (nick's final 2026-09-03
design). Before, the merger's merge surfaces were woven into task-backlog
rows and ran only against a **single repo implied by the daemon's launch cwd**
(`GitSyncRepoRoot` → `~/work/GoPi`). The old sweep never reached a SECOND
repo the daemon didn't happen to launch in — proven by the HOMM3 maps
failure (feat/maps-* branches sat unmerged in `/home/user/Downloads/games/
Maps` until a manually re-launched daemon happened to live in the right
context).

Now the merger is config-drive: settings.json's existing `repos` table
**IS** the sweep list. Every tick the merger cd's to EACH configured
repo path and merges all worktree-unprotected branches.

## The design (nick's words, 2026-09-03)

> "In the config file we specify the repo.path, the merger periodically
> cd's to repo.path and merges all branches... and deletes cleaned
> branches... cleans leftover worktrees... because it happened before for
> the merger to get merged."

Key consequences:
1. **Config list drives everything** — one daemon sweeps both
   `~/work/GoPi` **and** `/home/user/Downloads/games/Maps` every tick no
   matter where the daemon starts.
2. **Worktree = merge-time protection signal.** A branch checked out in a
   linked worktree is ACTIVE bond work — never swept. MergedCleanupSweep
   earlier removed its worktree when the tip was provably in master. The
   empirical git fact (verified 2026-09-04): `git merge` happily lands a
   worktree-checked-out branch (only `branch -d` refuses), so the sweep's
   **own occupancy guard is load-bearing** — a bug in the adoption/merge
   path merged active work earlier (memory #1344).
3. **Conflicts dispatch onto the normal task queue** for a worker/LLM to
   resolve. The merger ONLY merges what is clean and NEVER blocks.
4. **The merger never queries task status** for the merge decision. Merging
   is decoupled from the task ledger by construction (git is the arbiter).

## Empirical fact (load-bearing)

The whole worktree-protection model rests on one git behavior, verified in
real git 2026-09-04:

```
$ git merge feat/x        # worktree-checked-out branch → SUCCEEDS (lands in master)
$ git branch -d feat/x    # but → error: Cannot delete branch checked out at <wt>
```

So the sweep MUST carry its own worktree filters — git gives no protection
at merge time. `MergerRepoSweep` computes the protected-branch set (from
`git worktree list --porcelain`) and skips those branches for merging.

## Core engine: `MergerRepoSweep` (internal/tools/repo_sweep.go)

Per configured repo, one sweep turn:
1. **Discover unprotected merge candidates**: local branches whose tip is
   NOT already in master AND not checked out in a linked worktree. Skips
   master/main, `hp-unique-*` and archive-tagged (`refs/tags/archive/x`)
   loser branches.
2. **TaskMerge the candidates** (`TaskMergeOptions{Repo:..., Submit:true}`):
   clean merge lands (git auto-commit, branch -d); conflict aborts with
   master untouched and dispatches ONE merge-conflict task onto the normal
   queue; already-merged unprotected branches are pruned (branch -d).
3. **Tidy merged worktrees** (`MergedCleanupSweep`) every turn: prune
   merged-branch worktrees + branches already in master (the recycle).
4. **Report** merged/deleted/protected/dispatched tallies so the merger
   can decide the rebuild.

Fail-soft per repo: any git error degrades to that repo's report row; the
daemon never dies and continues to the next configured repo / tick.

## New merger pass (`mergerPassOnce` / `mergerSweepRepo`, cmd/agent)

- Iterates `config.ResolveMergerRepos()` (config-go) = sorted keys of the
  settings `repos` table. No repos table = log "nothing to sweep", continue.
- Cross-node git-sync + adoption channels STILL run ONCE per pass — they
  are the peer-delivery merge face (client push-back) and no-op when no
  sync peer is declared.
- **Retired from the pass**: merge-backlog row pipeline (branch-scan →
  merge-backlog audit → merge-backlog executor). Its job — discover +
  land branches on the one cwd-derived repo — is now done per configured
  repo by the pure-git sweep. No task rows mediate a clean local merge.
- **NO `--repo` flag change / no cwd assumption**: the daemon's own cwd is
  irrelevant to which repos it sweeps.
- Single-instance flock preserved.

## Rebuild-after-merge, per repo

`mergerSweepRepo` calls `serveRebuildAfterMerge(repo)` when the turn
landed OR pruned work. A repos-table entry with an EMPTY build is now a
valid PURE MERGE TARGET: swept but not rebuilt, `found=false`, never an
error (ResolveRepoBuild semantics relaxed). The close-loop (merge →
cleanup → REBUILD) is preserved per repo that declares a build.

## Config

```jsonc
// ~/.gopi/.ds_settings.json (the real machine config — now updated)
"repos": {
  "/home/user/work/GoPi": { "build": "CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/", "build_timeout_sec": 600 },
  "/home/user/Downloads/games/Maps": {}   // merge-only (no build contract)
}
```

## Files

- `internal/config/config.go`: `ResolveMergerRepos()` (sorted repos-table
  keys) + relaxed `ResolveRepoBuild` (empty-build entry = merge-only).
- `internal/config/config_repos_test.go`: new ResolveMergerRepos tests +
  merge-only ResolveRepoBuild test.
- `internal/tools/repo_sweep.go`: `MergerRepoSweep` engine.
- `internal/tools/repo_sweep_test.go`: real-git tests (merge unprotected,
  skip worktree-protected, prune already-merged).
- `cmd/agent/cli_tasks.go`: config-driven `mergerPassOnce` +
  `mergerSweepRepo` per-repo rebuild.
- `GOPI.md` Rule 12: repos table = sweep list + merge-only entries.

## E2E verification (real)

Setup: temp **real** unprotected branch in the Maps repo
(feat/merger-config-e2e-2), isolated GOPI_HOME so the flock doesn't collide
with the live daemon, cwd=/tmp (arbitrary — proving cwd irrelevance):

```
$ cd /tmp && ... bin/gopi task merger --once
[merger] [/home/user/Downloads/games/Maps] merged feat/merger-config-e2e-2 (merged)
[merger] [/home/user/Downloads/games/Maps] merged=1
[merger] [rebuild] skip: no build contract for .../Maps ...   ← silent skip (merge-only)
[merger] [/home/user/work/GoPi] merged=0 protected=2          ← live worktrees untouched
```

Both configured repos swept in ONE tick from an arbitrary cwd; Maps branch
landed + deleted; live GoPi bond worktree branches (protected=2) untouched.

## Not done / notes

- The merge-backlog row pipeline functions (`serveBranchScan`,
  `serveMergeBacklogAudit`, `serveMergeExecSweep`) remain DEFINED as CLI
  faces + their unit tests but are no longer invoked in the merger pass.
  Full removal / CLI-plumbing cleanup deferred (PA may drop them or keep
  as manual affordances).
- The real machine's settings.json was updated to add Maps but **did not
  restart the live daemon** (still running the OLD binary pre-self-heal).
  The new config-driven merger takes effect on the daemon's next clean
  restart with the rebuilt binary.
- During development the live daemon (old binary) auto-merged this feature
  branch into master via its adoption/cleanup pass (no worktree guard in
  that path) — this was actually the self-inflicted incident that validated
  why the guard matters. Feature commits all landed safely on master.

## Verification recap

- Build (this branch + go build ./...): OK.
- Unit: `go test ./internal/tools/ -run TestMergerRepoSweep` PASS;
  `go test ./internal/config/ -run TestResolveMergerRepos|TestResolveRepoBuild` PASS.
- Real E2E (single `--once` pass, cwd=/tmp, isolated GOPI_HOME): both
  configured repos swept; Maps unprotected branch merged+deleted; GoPi
  protected (live-worktree) branches untouched; Maps rebuild correctly
  skipped (merge-only).
- Full `go test ./...` suite: PASS (all packages green).
