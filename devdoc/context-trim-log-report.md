<!-- doc-class: historical -->
# Task Report: gopi log --context-trim — context_trim activity view

## Completed

Added a new `--context-trim` view to the `gopi log` subcommand that renders
every `context_trim` activity in a session:

- **`gopi log <session-id> --context-trim`** — activity table
- **`gopi log --last N --context-trim`** — applied to each of the last N sessions

Each row shows:
- the **context_trim call's own tool_use id** (call_id)
- the **trimmed call** (target tool name + its tool_use id, resolved from the
  `call_id` argument)
- the **outcome** parsed from the context_trim tool_result
  (`replaced` / `removed` / `noop`)
- the **before/after byte-size comparison** — BEFORE = the full logged
  content of the trimmed result (tick prefix + `[tool_use id: …]` stamp +
  payload — exactly the string `Context.RemoveToolPair` replaces in context),
  AFTER = `len(replace_with)` for a replacement or `0` for a deletion
- the **replace_with conclusion in full** (the 防遗忘 payload) for replaced pairs

### Files changed

| File | Change |
|---|---|
| `internal/gopilog/types.go` | `ContextTrimEvent` struct (call id, target id/name, replace_with, outcome, removed messages, before/after bytes, timestamp) |
| `internal/gopilog/reader.go` | `collectContextTrims` (scans iterations for `context_trim` calls, classifies outcome from the result string, resolves target call + sizes), `findToolCallByID`, `strArg`, outcome regexes (anchored on `context_trim:` text — tolerant of the `[t+NNNs]` tick prefix and the `[tool_use id: …]` stamp header) |
| `internal/gopilog/formatter.go` | `FormatContextTrim` (table view + empty state), `contextTrimIDCol` constant |
| `internal/gopilog/context_trim_test.go` | NEW: `TestCollectContextTrims` (replaced/removed/noop events, payload size assertions), `TestFormatContextTrim` (rows + sizes + conclusion), `TestFormatContextTrimNoActivity` |
| `cmd/agent/cli_log.go` | `--context-trim` flag (member check, options, parsing, dispatch, runRead/runLast/renderSessionView wiring, usage text), `runContextTrim` |
| `cmd/agent/cli_log_test.go` | `TestParseLogArgsContextTrim` (flag parse, `--last N` combo, flag membership) |

## Not Done

- Did **not** fold `context_trim` into `--memory`: the epic allowed "`--memory`
  **or** a new flag". `context_trim` is a context-management tool, not a memory
  tool, and mixing it into the memory family would dilute the `--memory`
  view's meaning (its doc comment says it must stay the memory family). A
  dedicated `--context-trim` flag is self-documenting and composes with
  `--last N`.
- No `--text` variant for the table (consistent with `--todo` which also
  renders a structured view, not raw text).
- Pre-existing failures left untouched: `internal/deepseek`'s
  `TestResolveModel_Default/NilConfig` (stale since `b21e630`, default is now
  `deepseek-v4-flash`) and `internal/tools`' memory-search tests (network
  dependency, hang) — both fail identically on `master` and are unrelated to
  this change.

## Verification

- Build: `go build ./...` — **OK**
- Tests: `go test -count=1 ./internal/gopilog/ ./cmd/agent/ ./internal/agent/`
  — **all ok** (gopilog 0.016s, cmd/agent 7.5s, internal/agent 24.8s)
- Live smoke against a real session with 2 context_trim replaces
  (`ccc218ba-…`):

```
CONTEXT TRIM — 2 event(s)
CONTEXT_TRIM CALL                        TRIMMED CALL                             OUTCOME          BEFORE        AFTER DETAIL
──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
call_00_Sm34dOP8E1ISi4ccyzkc1871         bash call_00_Qm1cubp7vO9MHXoNhrb35503    replaced            711          122  replaced with 122B conclusion
    replace_with: 方法 A 失败：solve_a.py import 不存在的模块 nonexistent_module_xyz → ModuleNotFoundError——不要重试。

call_01_7teIFNyEFz8vlSVyEeqy1914         bash call_01_u0AEVBCi31760g6WLMX29247    replaced            356          110  replaced with 110B conclusion
    replace_with: 方法 B 失败：solve_b.py 打开不存在的 missing_data_b.json → FileNotFoundError——不要重试。
```

The 711B before-size matches the live evidence in `reports/archive/2026-08/CONTEXT-TRIM-REPORT.md`
(707-763B at request-body level — the small delta is the tick-prefix format).
Empty state verified on a session without trims: `No context_trim activity in
this session.`

- Commit: see `git log` below.
