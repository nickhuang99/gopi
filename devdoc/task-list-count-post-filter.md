<!-- doc-class: normative -->
# Task Report: `task list --status <s>` header count is now POST-archive-filter

## Completed

- **Root cause**: `TaskList` (internal/tools/tasks.go) stamps the count header
  from its *SELECT's* row count (`%d task(s) with status=%s:`). The CLI's
  default (non-`--all`) path then runs `tools.FilterArchivedRows` over that
  rendered block (cmd/agent/cli_tasks.go) to drop the archived
  (`resolved`-tagged) rows. The old filter only rewrote the header when
  **every** row was dropped — a *partial* filter left the pre-filter count
  above a shorter body (the reported "header claims 68, 7 rows render").
- **Fix** (surgical, `internal/tools/tasks.go`):
  - `FilterArchivedRows` now recounts the surviving `#`-prefixed rows and
    re-stamps the header's leading integer via `rewriteTaskListCount`; a
    filter that loses every row degrades to `taskListEmptyResult`
    (`"No tasks found."`). Dead branch (`if dropped > 0` inside a
    `dropped==0` early return) removed.
  - New helpers: `countRenderedTaskListRows`, `rewriteTaskListCount`,
    `taskListCountPrefix` (parses the header back into a count + tail).
  - New constants (Rule 1b/13, no bare literals): `taskListCountSuffix`,
    `taskListStatusCountSuffix` (the ONE spelling of the header, used by
    `TaskList`), `taskListEmptyResult`, `taskListRowPrefix` (`"#"`),
    `taskListCountMarker` (`" task(s)"`). The filter still reuses
    `taskResolvedRowMarker` / `isRenderedRowArchived`; `--all` skips the
    filter entirely, so it stays unfiltered and accurate.
- **Regression tests** (Rule 10.5):
  - `internal/tools/tasks_archive_test.go`: `TestFilterArchivedRowsHeaderMatchesRows`
    (mixed archived+live → header == live-row count), `TestFilterArchivedRowsHeaderNoStaleCount`
    (fully archived → `No tasks found.`, no stale count header),
    `TestRewriteTaskListCountPreservesBody` (header-only rewrite; unexpected
    render passes through), and `TestFailedViewHidesArchived` extended to
    assert the `--all` (unfiltered render) header == 2 rows.
  - `cmd/agent/cli_tasks_test.go`: `TestTasksCLIListHeaderCountMatchesRows` —
    **real binary** end-to-end (`gopiTestBinPath`) against a seeded `--db`:
    default view header == 1 row, `--all` header == 4 rows.

## Not Done

- No change to the ledger data or to `--all` semantics (deliberate).
- The fully-archived case is covered at the unit level
  (`TestFilterArchivedRowsHeaderNoStaleCount`) and via the CLI mixed view; a
  live DB where *every* row for a status is archived still renders
  `No tasks found.` from `TaskList` itself (its SELECT already matches
  nothing) — the filter's empty degradation is the belt to that braces.
- `internal/tools` still spells a few other count headers (search results,
  reframe summaries); only the TaskList header is part of this defect and was
  centralized.

## Verification

- **Red-proof (mutation)**: reverting the single rewrite line
  (`return rewriteTaskListCount(out, rows)` → `return out`) makes
  `TestFilterArchivedRowsHeaderMatchesRows` and `TestFailedViewHidesArchived`
  FAIL with exactly the defect message — e.g.
  `header claims 4 task(s) but 1 rows render` — and restoring it goes green.
- **Real binary, before**: built `master` binary, seeded 1 live +
  3 archived failures:
  `task list --status failed` → `4 task(s) with status=failed:` above
  **1** row.
- **Real binary, after**: `1 task(s) with status=failed:` above 1 row;
  `--all` → `5 task(s) with status=failed:` above 5 rows; last row archived →
  `No tasks found.` These match the DB truth
  (`SELECT count(*) FROM tasks WHERE status='failed' AND replace(tags,' ','')
  NOT LIKE '%resolved%'` → 1; unfiltered → 5).
- **Build**: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK.
- **Tests**: `go test ./internal/tools/ ./cmd/agent/ ./internal/standards/`
  (see session log; the new tests and the archive/contract suites are green).
- **Commit**: `d350f637` on branch `feat/tasklist-header-count`, worktree
  `~/workspace/gopi-tasklist-count` (shared refs — already in the main repo).

### Independent re-verification (real ledger, read-only)

Re-run against the live `~/.gopi/memory.db` **copied** to /tmp (the original
DB was never opened for write; sha256 of `~/.gopi/memory.db` unchanged:
`cee92914caf81e1c`):

| command | master (pre-fix) | fixed |
|---|---|---|
| `task list --status failed` | header `69` above **4** rows | header `4` above **4** rows |
| `task list --status failed --all` | `69` above 69 rows | `69` above 69 rows |

DB truth for the same snapshot: `SELECT count(*) WHERE status='failed'` → 69;
`… AND replace(tags,' ','') NOT LIKE '%resolved%'` → 4. Real binary, 69-vs-4 —
the reported defect, reproduced and closed. `go test ./internal/standards/`
green (Rules 16–21 gates unaffected).
