<!-- doc-class: historical -->
# Task Report: gopi log --last N broken output (empty fields)

## Completed

`gopi log --last 10` printed rows like `f0e71f65  test-model  0.0s  0 iter  0 tok  OK` with
all fields empty. Root cause and fix:

1. **Root cause** — degenerate session files. `~/.gopi/sessions/*.jsonl` contained ~168
   one-line files holding only a `session_start` event (no user/assistant/request/
   session_end). `gopi log` rendered *every* file, and a session_start-only file has no
   task/summary/duration content, so it rendered as the empty `test-model 0.0s 0 iter
   0 tok OK` row. The parser was correct; the display had no filter for degenerate
   sessions. (Pollution source: agent unit tests — `NewAgent("test-model")` writes a
   real session file without GOPI_HOME isolation. In-repo leaker was
   `internal/agent/writetaskarea_test.go`'s `newTaskAreaTestAgent`.)

2. **Filter degenerate sessions** (`internal/gopilog/reader.go`):
   - New `SessionHasActivity(path string) (bool, error)` — true only if the session
     stream contains a `user` / `assistant` / `request` / `session_end` event line
     (i.e. real activity, not just a session_start).
   - `LoadAllSummaries` now skips degenerate sessions (central fix — every log caller
     benefits, not only `--last`).

3. **Apply filter before N** (`cmd/agent/cli_log.go` runLast): files are pre-filtered
   through `SessionHasActivity` before the newest-N window is selected, so N junk
   files can no longer crowd out real sessions.

4. **Stop the pollution at the source** (`internal/agent/writetaskarea_test.go`):
   `newTaskAreaTestAgent` now calls `newTestAgentEnv(t)` so the test agent gets an
   isolated GOPI_HOME. Verified: real profile session count stayed flat during the
   test run (1161 → 1161).

5. **Regression tests** (all pass):
   - `cmd/agent`: `TestRunLastSkipsDegenerateSessions` — a session_start-only file in
     a fake GOPI_HOME is not shown by `log --last N`.
   - `internal/gopilog`: `TestSessionHasActivity` + `TestLoadAllSummariesSkipsDegenerateSessions`.

6. **Real-data repro**: freshly built binary — `gopi log --last 10` on the polluted
   profile now lists only real sessions (previous junk rows gone). Note: the user's
   installed `bin/gopi` was stale (predates session-schema changes); rebuild required
   to see the fix.

## Not Done

- No migration/cleanup of the ~168 already-polluting files (outside repo scope). A
  one-off `rm ~/.gopi/sessions/<those 168 ids>.jsonl` or a future housekeeping sweep
  can remove them; the display fix makes them harmless either way.
- Full `go test ./... -count=1`: ALL packages pass except ONE pre-existing,
  environment-drift failure unrelated to this fix —
  `testsuite.TestWebFetchContract_DeployedScriptSync` (deployed
  `~/.gopi/plugins/websearch/scripts/arxiv_search.py` stale vs repo copy; sync
  needed, out of scope here).

## Verification

- Build: `go build ./...` OK (job ran in worktree).
- Tests (affected): internal/gopilog, cmd/agent, internal/agent — full package suites
  PASS incl. the three new regression tests. Full `go test ./...` otherwise green.
- Real-data repro: `gopi log --last 10` shows only real sessions with the fixed binary.
- Commit: `9f70039` (fix) + `24d3b7b` (report) on branch `feat/fix-log-last-degenerate`
  (worktree `/home/user/workspace/gopi-log-last-fix`, shared with main repo — commit
  already on the branch in `/home/user/work/GoPi`).

## Recover

- Branch: `feat/fix-log-last-degenerate`
- Fetch: `git show feat/fix-log-last-degenerate:devdoc/log-last-degenerate-fix.md`
