<!-- doc-class: normative -->
# Task Report: Expand golden tests to gopi log content views

## Summary

Extended nick's golden regression-canary (real-session fixture + byte-exact
snapshots) from `gopi stats`/`gopi raw` to the `gopi log` CONTENT views.
No production code was changed — tests + snapshots only (acceptance #5).

## Completed

- **`internal/gopilog/log_golden_test.go`** — 16 renderer-level golden tests
  over the real fixture session (fc426fed, 9 rounds / 43 events) + its paired
  raw HTTP log, reusing the existing `fixturePath`/`compareGolden`/
  `GOPI_GOLDEN_UPDATE=1` mechanism (acceptance #2, no second framework):
  - default session view (`log-session.txt`)
  - `--chat` (`log-chat.txt`)
  - `--response` and `--response --text` (`log-response*.txt`)
  - `--thinking` and `--thinking --text` (`log-thinking*.txt`)
  - `--todo` (`log-todo.txt`) — real 5-item task list
  - `--memory` family filter (`log-memory-filter.txt`) — 3 real memory calls
  - `--filter write_task_area` (`log-write-task-area-filter.txt`) — 5 commits
  - `--search` empty state (`log-search-filter.txt`)
  - `--context-trim` empty state (`log-context-trim.txt`)
  - `--grep` content / scoped-to-memory / no-match (`log-grep-*.txt`)
  - `--request` verbatim system prompt (`log-request-system.txt`)
  - `--request --json` full body (`log-request-json.json`)
  - `--json` **byte-exact passthrough vs the fixture file itself**
    (not a snapshot — same pattern as TestGoldenRawRequestIsByteExact)
- **`cmd/agent/cli_log_golden_test.go`** — 15 CLI-layer tests driving
  `runLogSubcommand` through the fixture sandbox (`statsFixtureSandbox`),
  asserting against the SAME committed goldens (assertGolden). This pins the
  flag wiring / dispatch-order path in cli_log.go, not just the formatters.
- **16 golden snapshots** under `tests/fixtures/log-stats-raw/golden/log-*.txt`.
- **`tests/fixtures/log-stats-raw/README.md`** — view-coverage table + fixture
  gaps recorded (acceptance #4).

## Not Done (fixture gaps — recorded, not faked)

- Views needing events the fixture session has none of: tool families
  `--search`/`--web`/`--job`/`--vision`/`--text2img`/`--git` (no such tool
  calls) and `--context-trim` (no activity). Golden-covered only through their
  empty states. A real session that exercised those tools would be a new
  fixture (per IMPLEMENT-SPEC §4 fixtures must be REAL logs).
- `--last N` view combination and `--all --summary` already have non-golden
  coverage elsewhere (cli_log_test.go) — not re-goldened here.

## Verification

- `go vet ./internal/gopilog/ ./cmd/agent/`: OK
- `go test ./internal/gopilog/`: PASS (31s incl. full suite)
- `go test ./cmd/agent/`: PASS (57s incl. full suite)
- `GOPI_GOLDEN_UPDATE=1 go test ./internal/gopilog/ -run TestGoldenLog`: 16/16 recorded, re-run green (byte-stable)
- `go test ./cmd/agent/ -run TestGoldenCLILog`: 15/15 PASS

## Commit

- `ccfd983` test(gopilog): golden tests for gopi log content views over real fixture session
- `445bec0` docs(gopilog): log-content-golden task report (views covered + fixture gaps)

Branch: `feat/log-content-golden` (worktree /home/user/workspace/gopi-log-golden-views, shared with main repo — no push needed)
