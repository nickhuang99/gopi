<!-- doc-class: historical -->
# Task Report: Fix `gopi log --last N` showing empty rows

## Completed

**Bug**: `gopi log --last 10` printed rows with all-empty fields:
```
f0e71f65   test-model  0.0s  0 iter  0 tok  OK
160d8bde   test-model  0.0s  0 iter  0 tok  OK
...
```

**Root cause** (verified against real data): `~/.gopi/sessions/` contained 187
degenerate JSONL files whose ONLY line is a `session_start` event — no `user`,
`request`, `assistant`, or `session_end`. These are agent `SessionLogger`s
opened and then abandoned: unit tests calling `NewAgent("test-model")` without
GOPI_HOME isolation (in-repo `internal/agent` tests + many worktree clones),
and runs that died before their first turn. The parser was correct —
`FormatSummary` faithfully rendered what was in the file (model, 0.0s, 0 iter,
0 tok). The display had no filter, so `--last N` let these stubs (often the
newest, mtime-wise) crowd out real sessions.

**Fix — summary listings now skip sessions with zero activity**:
- `internal/gopilog/reader.go`: new `SessionHasActivity(path)` — scans the
  JSONL for any event beyond `session_start` (user/request/assistant/
  session_end). A bare session_start-only file is the degenerate fingerprint.
- `cmd/agent/cli_log.go` `runLast`: filters files through `SessionHasActivity`
  before applying N, so `--last N` shows the N most recent REAL sessions.
- `internal/gopilog/reader.go` `LoadAllSummaries` (used by `gopi log --all
  --summary` and the `/log` interactive views): tracks `sawActivity` while
  scanning and drops degenerate rows the same way.
- `internal/agent/writetaskarea_test.go`: `newTaskAreaTestAgent` now calls
  `newTestAgentEnv(t)` — the in-repo leak source. It opened real session
  loggers under the developer's `~/.gopi`. Verified: before/after running the
  affected agent tests, sessions count stayed 1161 (no growth).

Degenerate sessions remain reachable by full id (`gopi log <full-id>`) and
are still crash-dumped by `BackfillOrphanCrashDumps` — only the summary
listing hides them ("what did my agents actually do" must not show no-ops).

## Not Done
- Existing polluted files in `~/.gopi/sessions` were NOT deleted (out of
  scope; they now just don't surface in listings). A future `gopi log gc`
  could remove session_start-only files whose dump.json already exists.
- The `<id>.dump.json` files are picked up by `FindSession` prefix matching
  (an 8-char prefix matching both `X.jsonl` and `X.dump.json` reports
  ambiguity). Pre-existing behavior, untouched.

## Verification
- Build: `CGO_ENABLED=0 go build ./...` — OK
- Tests:
  - `TestRunLastSkipsDegenerateSessions` (new, cmd/agent) — PASS
  - `TestRunLastViewHonorsN` — PASS (existing N-honoring regression)
  - `TestSessionHasActivity` / `TestLoadAllSummariesSkipsDegenerateSessions`
    (new, internal/gopilog) — PASS
  - `go test ./internal/gopilog/ ./cmd/agent/ ./internal/agent/` — all ok
  - Full `go test ./...` — run detached (job-8), package results below
- Real-data repro: freshly built binary `log --last 10` now shows 10 real
  sessions (task/iter/tok populated); before the fix it interleaved junk
  `test-model 0.0s` rows. `log --all --summary` likewise clean.
- All-degenerate dir edge: `--last 5` prints "No sessions found".

## Commit
- Branch: `feat/fix-log-last-degenerate`
- Commit: `2e4eadc` fix(log): skip session_start-only degenerate sessions in --last N and summaries
