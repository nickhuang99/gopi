<!-- doc-class: historical -->
# Devdoc: `gopi log --last N` with view flags

## The bug
`gopi log --last N --memory` (and any other view flag: `--filter`, `--chat`,
`--todo`, `--response`, `--thinking`, `--request`, `--json`) returned the same
output for every N > 1. Root cause: in `runLast` (cmd/agent/cli_log.go) the
"if flags are set" branch rendered only `files[0]` and returned — N was
silently ignored. Plain `--last N` (summaries) was always correct; only the
view-flag combination was broken.

## The fix (pattern worth repeating)
1. Extract the per-session view dispatch into a helper that takes a single
   file path: `renderSessionView(path, opts)`. It mirrors `runRead`'s
   dispatch order: `--request` → `--memory/--filter` → parse → `--json` →
   chat/todo/response/thinking. `runRead` and `runLast` now share one
   dispatch, so the two can never drift apart again.
2. `runLast` view branch loops over the last N files calling the helper.
   Print a blank-line separator between sessions EXCEPT for `--json` (raw
   JSONL stays pipeable).
3. Regression test pattern: point `HOME` at a temp dir with
   `$HOME/.gopi/sessions` fixtures (distinct modtimes via `os.Chtimes` to
   control newest-first sort), capture stdout via `os.Pipe`, and assert the
   session IDs present. Note `runLast` calls `os.Exit(1)` on directory/parse
   errors — fixtures must be valid JSONL or the test process dies.

## Pitfalls
- `os.UserHomeDir()` reads `$HOME` fresh on each call on Unix (no cache) —
  `t.Setenv("HOME", ...)` works for testing `gopilog.ResolveSessionDir`.
- `go build`/`go test` inside a git worktree may fail with "error obtaining
  VCS status" — use `-buildvcs=false` (repo VCS quirk, not a code issue).
