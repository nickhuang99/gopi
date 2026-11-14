<!-- doc-class: historical -->
# Task Report: merge conflict resolution — feat/worktree-live-owner-guard into master

## Completed
- Reproduced the merge in the assigned linked worktree
  (`/dev/shm/gopi-test-scratch/queue/queue-runtime-merge-resolve-1070633/gopi-merge-resolve-3186892445/wt`)
  with `git merge master --no-commit --no-ff`.
- **Exactly one conflicted file**: `internal/tools/task_merged_cleanup.go` (status `UU`);
  every other path auto-merged cleanly.
- Resolved the single hunk (markers at lines 130/162/163) and committed the merge:

  **commit `2529ead9` — "resolve merge feat/worktree-live-owner-guard into master"**
  (parents `0d092e8e` = branch tip, `a7584fc1` = master tip ⇒ a real two-parent merge commit).

### The resolution and why
The conflict was a **duplicate symbol**, not two competing implementations. HEAD's
block introduced three live-status helpers into this file; master independently
grew its own canonical live-status plumbing in the same package:

| symbol | HEAD (this branch) | master | resolution |
|---|---|---|---|
| `liveTaskStatuses` | `task_merged_cleanup.go` | — | **keep HEAD** (nothing on master replaces it; the TS-vs-master tree has no other definition) |
| `isLiveTaskStatus` | `task_merged_cleanup.go` | — (referenced only by HEAD's own `task_finish_cleanup.go:1421` `taskLoadLiveClaims` predicate) | **keep HEAD** |
| `taskLiveStatusSQLList` | `task_merged_cleanup.go` | `tasks.go:169` — same three constants (`taskStatusPending/Claimed/InProgress`) rendered as an SQL IN literal | **drop this file's copy** — two definitions in one package is a redeclaration (compile error) *and* a second source of truth for one set (Rule 14) |

Both sides' intent survives: the live-status set and its Go predicate are HEAD's
(the branch added them, master does not have them anywhere), while the SQL-list
rendering — the newer, canonical home — comes from master. A `merge-note:`
comment at the resolution site records this so a future reader does not
"re-add" the dropped copy. Neither side's intent was silently dropped: the only
thing removed is a byte-for-byte semantic duplicate.

### Self-corrected during verification (honest note)
My **first** resolution dropped all three symbols (mis-attributing
`task_finish_cleanup.go`'s `isLiveTaskStatus` caller to master). `go build ./...`
caught it immediately (`undefined: isLiveTaskStatus`), I restored HEAD's list +
predicate, and amended the merge commit (`118e5115` → `2529ead9`). The final
commit is the corrected one.

## Not Done
- `go test ./internal/tools/ -run 'TestMergedCleanup|…'` was started as the
  touched-package check and was **killed after ~90 s with zero output** — this
  package's test binary is the known pre-existing hang in this repo (memory
  #1970). The epic explicitly waives it ("a hanging check never blocks your
  exit"); it is not a signal about this resolution. `go vet ./internal/tools/`
  was run in its place as the compile-level check on the touched package.
- No attempt to fix the pre-existing hang — out of scope for a resolver.

## Verification
- Merge reproduced: **yes** — 1 `UU` file, `git status --porcelain` before the commit.
- Conflict markers: `grep -rn -E '^(<<<<<<<|=======|>>>>>>>)' --include=*.go .` → **0 hits**, exit 1.
- Duplicate gone: `git grep -c "func taskLiveStatusSQLList"` → **1** (`internal/tools/tasks.go` only).
- Build: `go build ./...` → **OK** (no output; the object is a merge candidate, no `isLiveTaskStatus` error).
- Vet (touched package): `go vet ./internal/tools/` → **OK**.
- Format/diff: `gofmt -l internal/tools/task_merged_cleanup.go` → clean; `git diff --check` → clean.
- Commit: `2529ead9 0d092e8e a7584fc1` ⇒ **two parents**; `MERGE_HEAD` absent;
  `git status --porcelain` → **clean**.
- Repo isolation: `git --git-dir=/home/user/work/GoPi/.git rev-parse feat/worktree-live-owner-guard`
  → `2529ead9` (the commit **is** in the main repo's object store, via the shared
  worktree refs); `git -C /home/user/work/GoPi status --porcelain -- internal/tools/task_merged_cleanup.go`
  → empty (the main checkout was **never** touched). No push, no `branch -d`.

## Deliverable declaration (Rule 6d)
1. **Branch**: `feat/worktree-live-owner-guard` (commit `2529ead9417d06383f8f327e884010db5f7c98e4`).
2. **Recovery command**:
   `git -C /home/user/work/GoPi show feat/worktree-live-owner-guard:internal/tools/task_merged_cleanup.go`
3. **Confirmed in the main repo**: yes — `git --git-dir=/home/user/work/GoPi/.git rev-parse`
   resolves the branch to that commit (worktree shares the object store / refs; no push needed).
