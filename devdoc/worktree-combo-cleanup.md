<!-- doc-class: historical -->
# Task Report: abandoned worktree `~/workspace/gopi-combo` — verify, abort, remove

Branch: `docs/gopi-combo-worktree-cleanup` (worktree `~/workspace/gopi-combo-cleanup`)

## Summary

`~/workspace/gopi-combo` was a linked worktree parked in a never-finished merge:
detached HEAD `fcbb4742`, `MERGE_HEAD 58314968`, `UU cmd/agent/main.go`, six
staged A/M entries and an untracked `tmpbench/`. Both merge sides were
independently re-verified as already ancestors of `master` (stale-base no-op
replay), so `git merge --abort` + `git worktree remove --force` could not lose
any work. The worktree is gone; `master` and every ref are byte-identical.

## Completed

1. **Preflight ancestry re-verification (independent of prior evidence)** — from
   `/home/user/work/GoPi`:
   - `git merge-base --is-ancestor fcbb4742 master` → **exit 0**
   - `git merge-base --is-ancestor 58314968 master` → **exit 0**
   - Both sides of the recorded merge are ancestors of `master` ⇒ replaying it
     would add nothing; `--abort` discards only a no-op.
   - Worktree `git log --oneline -3` (fcbb4742 / 132d2161 / 79d6fe52) and the
     reflog (`merge fix/crash-dump-tail-read: Fast-forward`) show the checkout sat
     at the tip of an already-landed fast-forward chain.
2. **Deliverable-content proof (ancestry is not just a graph fact)** — every file
   the staged index carried resolves against `master`:
   ```
   devdoc/crash-dump-orphan-false-positive.md      master-blob=e73811a39a2d…
   internal/agent/crashdump_orphanwindow_test.go   master-blob=17c2740236bf…
   internal/agent/crashdump.go                     master-blob=5c796dcaeb44…
   internal/agent/envparse.go                      master-blob=04c5be98e238…
   internal/agent/session.go                       master-blob=90291c392f97…
   internal/agent/sessionend_tail_test.go          master-blob=26bc4364a253…
   ```
   `git log master -- internal/agent/crashdump.go devdoc/crash-dump-orphan-false-positive.md`
   shows the crash-dump deliverable landed via `b33dda6e … 6e154d69 resolve merge
   fix/crashdump-classify into master`.
3. **Liveness preflight** — `/proc/*/cwd` scan for a live holder of
   `~/workspace/gopi-combo`:
   ```
   == live cwd holders (real, excluding this probe) ==
   (end)
   ```
   Zero real holders (the only hit was the scanning shell's own `/proc/<pid>/cwd`).
   Re-run was performed **immediately before** removal. Merger daemon
   `pid 1284455 ./bin/gopi task merger --every 5m` was left running (still alive,
   elapsed 09:49:58) — it holds no cwd in the worktree; the merger's own last
   line about `[adopt] skipped: worktree dirty` refers to *other* trees.
4. **`git merge --abort` with cwd inside the worktree** —
   `cd ~/workspace/gopi-combo && git merge --abort` → **exit 0**; `MERGE_HEAD`
   gone; `git status --porcelain` reduced to only `?? tmpbench/`.
5. **Untracked `tmpbench/` preserved before removal** — it is an uncommitted
   ad-hoc probe (`tmpbench/main.go`, 2199 bytes, header: *"Probe (UNCOMMITTED —
   do not git add): measures the orphan-sweep startup cost"*). Copied to
   `/tmp/gopi-combo-tmpbench-backup/tmpbench/main.go` so removal loses nothing.
   **Its measurements are already published in master**:
   `devdoc/crash-dump-startup-backfill.md` records
   `DetectOrphanSessions elapsed=647.9ms`, `BackfillOrphanCrashDumps = 668 ms`,
   `alloc churn 430.9 MB`, `VmHWM 36.7 MB` — same instrument, same figures
   (see also `devdoc/crash-dump-startup-backfill-gate.md`,
   `devdoc/crashdump-scan-cost.md`). Nothing was orphaned by the removal.
6. **Removal** — `git worktree remove --force /home/user/workspace/gopi-combo`
   → **exit 0**. Afterwards:
   - `git worktree list` no longer lists `gopi-combo` (main + 5 other worktrees + 2 in `/tmp/gwv` untouched);
   - `.git/worktrees/gopi-combo` admin dir: `No such file or directory`;
   - checkout dir `/home/user/workspace/gopi-combo`: `No such file or directory`.
7. **No-ref-change / no-residue proof** (snapshots taken before and after):
   - `git for-each-ref … | sort` before vs after → `diff` empty → **REFS-IDENTICAL** (64 refs);
   - `git rev-parse master` before = after = `c0211ad470e91e2c72b0d7cd7f956958445198e9`;
   - `git status --porcelain` in the main repo: 32 lines before, 32 after, no line matching `combo|tmpbench`.
8. **This report committed on `docs/gopi-combo-worktree-cleanup`** (worktree at `/home/user/workspace/gopi-combo-cleanup`), per Rule 6 (never develop in the main repo; worktree commit is already in the shared repo, no push needed).

## Not Done / Deliberately Not Done

- **No branch deleted** — the abandoned checkout was a *detached HEAD*; there was
  no branch under it to delete or archive.
- **Nothing merged** (Rule 6). No merge-backlog task filed: both sides were
  already ancestors of `master`, so there is nothing left to land.
- **Other worktrees untouched** (`gopi-mergerverify`, `gopi-verify-01a09435`,
  `gopi-verify-glmwatch`, `gopi-verify-prefix`, `gopi-verify-rpt`, `/tmp/gwv/*`).
- **Ledger rows not touched.** `01a09464…` (this cleanup, `[in_progress]`)
  belongs to the harness session that claimed it, and `01a09463…` is already
  `[completed]` (`resolve merge conflict: fix/merger-status`, auto). Both were
  read-only observed; closing them is the claiming session's job, not this
  report's. `gopi task show 01a09463`/`01a09464` returned
  `no task with task_id=…` (the verb wants the full UUID), which is why the
  short ids were read from `gopi task list` instead — no ledger write attempted.
- **No repo-wide `go test ./...`** — the change is a deletion of an abandoned
  checkout, not a source edit; the targeted build + standards gate + crash-dump
  package tests below are the relevant gate for the deliverable area.

## Verification

- Build: `CGO_ENABLED=0 go build -o /tmp/gopi-cleanupcheck ./cmd/agent/` → **build OK** (in the report worktree, at master tip `c0211ad4`)
- Tests:
  - `go test ./internal/standards/ -count=1` → `ok github.com/nick/gopi/internal/standards 2.607s`
  - `go test ./internal/agent/ -run 'Crashdump|Orphan|SessionEnd' -count=1` → `ok github.com/nick/gopi/internal/agent 1.775s`
- Ancestry (before abort): `git merge-base --is-ancestor fcbb4742 master` → exit 0; `git merge-base --is-ancestor 58314968 master` → exit 0
- Removal: `git worktree remove --force` → exit 0; admin dir + checkout dir both gone
- Refs: `diff` of 64-ref snapshots → empty; `master` = `c0211ad470e91e2c72b0d7cd7f956958445198e9` before and after
- Commit: `<see git log of docs/gopi-combo-worktree-cleanup>`
