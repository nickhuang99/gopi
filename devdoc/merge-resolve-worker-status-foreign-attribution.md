<!-- doc-class: normative -->
# Merge resolution: fix/worker-status-foreign-attribution → master

Commit `70ee4179` (merge, parents `9ab0dd18` ours + `74d28bdc` master).
Repo /home/user/work/GoPi, worktree `…/gopi-merge-resolve-3526989137/wt`.

## Conflicts (3 UU files, 5 hunks)

Two independent intents collided in `cmd/agent/cli_worker_status.go`, both
editing `workerStatusEnumerate`:

- **ours (HEAD)**: profile attribution — a detached worker-shaped process is
  attributed by ITS OWN environment (`GOPI_HOME`, else `HOME`); a differing
  profile becomes a Foreign row, never a worker line.
- **master**: the ORDER fix (measured 2026-09-14 lie) — the state-file
  (strong) half runs FIRST, the argv fallback covers the remainder; the old
  argv-first order rendered a daemon that HAD a record from the weak source.

Resolution = **master's order + our attribution on the fallback half**
(never both, never a silently dropped side):

- `workerStatusEnumerate`: dropped HEAD's fallback-first block entirely
  (keeping it would have re-introduced the argv-first bug master measured);
  the record half runs first and marks `seen`, and the single remaining
  fallback half calls `pids, foreign := fallbackWorkerPIDs(scan.Profile)`,
  appends `scan.Foreign`, then skips `seen` pids. HEAD's doc comment was
  spliced onto master's instead of duplicated.
- `internal/procname/procname.go`: disjoint consts — both kept
  (`EnvHome` from ours, `EnvStatField` from master).
- `cmd/agent/cli_worker_status_test.go`: comment-wording-only conflicts;
  master's superset wording kept.

## Merge-induced build failure (not a conflict marker)

`internal/testutil` failed to build: master added `procident.go` declaring
`procStatName` / `procCmdlineName` (package scope) while our branch declared
the same two in `glm_night_watch_test.go`. Fixed by keeping procident.go as
the single source of truth and dropping the duplicates from the test's const
block (Rule 14; verified `reapProcRoot` / `procCommName` are still used).

## Verification

- `go build ./...` → OK
- `go vet ./internal/testutil/ ./internal/procname/ ./cmd/agent/` → clean
- `go test ./cmd/agent/ -run 'TestWorkerStatus|TestWorkerState' -count=1` → ok
- `go test ./internal/testutil/ -run 'GLM|Proc|Procname' -count=1` → ok
- Completeness gate: `git merge master --no-commit --no-ff` → "Already up to
  date." (no remaining conflict), aborting left the tree clean
- `gofmt -l` on the touched files → empty; zero conflict markers

Full-suite `go test ./...` deliberately not run (known pre-existing hang; the
epic waives it).
