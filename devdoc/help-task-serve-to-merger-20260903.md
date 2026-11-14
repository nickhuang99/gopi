<!-- doc-class: historical -->
# Task Report: gopi -h still shows 'task serve' — replace with 'task merger'

## Completed
- Root cause: `internal/config/help.go` FormatUsage() (the top-level `gopi -h`
  output) still carried the M4.5-era (c84d9f0, 2026-08-29) `gopi task serve`
  block describing a **task-executing daemon** (`Daemon loop: run next →
  writeback → sleep … --skip-tag …`), plus a pre-split M4.20-A worker blurb.
  The serve/worker-split (2026-09-03, epic serve-worker-split) renamed the
  merge daemon verb `task serve` → `task merger` and moved task execution to
  `gopi worker`; `cmd/agent/cli_tasks.go`'s tasksUsage was rewritten then, but
  help.go's top-level block drifted (not covered by any test).
- Replaced the stale block in `internal/config/help.go` with the canonical
  semantics:
  - `gopi task merger [--every 5m] [--once] [--repo PATH] [--no-branch-scan] [--db PATH]`
    — MERGE DAEMON (server-only, old 'task serve' renamed, still an alias):
    branch merge + conflict resolve ONLY, never claims/runs tasks, six-sweep
    merge surface, --once single pass, flock single-instance.
  - `gopi worker [--once]` — TASK-MANAGEMENT LOOP (client AND server), the
    ONLY process that claims and runs tasks; drained queue waits
    (GOPI_WORKER_IDLE_SEC, default 30s); --once drain-exit.
- Added regression guard in `cmd/agent/usage_test.go`
  `TestTopLevelUsageShowsTaskLedger`: now asserts `gopi task merger` and
  `gopi worker` are advertised AND rejects any top-level `gopi task serve [`
  verb block (deprecated alias must never be re-advertised as current).

## Not Done
- Other pre-split-era "task serve" references in comments/docstrings
  (cli_tasks.go lock messages, devdoc history, loop.go tool descriptions)
  left as-is: they correctly refer to the deprecated-alias/runtime meaning.
- No runtime code change: the `task serve` alias itself still works (kept,
  per serve-worker-split design — merger and serve never double-merge).

## Verification
- Build: `CGO_ENABLED=0 go build ./...` OK
- Tests: `go test ./cmd/agent/ -run TestTopLevelUsage -count=1` OK;
  `go test ./internal/config/ -count=1` OK; go vet clean on both packages.
- Live render: `go run ./cmd/agent/ -h` shows the new `gopi task merger`
  block and worker text between `task run` and `task update`.
- Commit: `9c222d3` on branch `feat/help-task-serve-to-merger` (worktree
  /home/user/workspace/gopi-help-merger-fix — commit already in the shared
  main repo via worktree refs; no push needed).
