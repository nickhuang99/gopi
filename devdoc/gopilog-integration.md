<!-- doc-class: normative -->
# gopi log subcommand — integrated gopi-log-reader

The standalone `gopi-log-reader` tool is now built into the `gopi` binary as
the `gopi log` subcommand, so a single binary can inspect its own session logs
without an extra tool.

## Layout

- `internal/gopilog/` — the ported engine (pure logic, stdlib only):
  - `types.go` — `Session`, `Iteration`, `ToolCall`, `RawEvent`, `SessionSummary`
  - `reader.go` — `ParseFile`, `ListSessionFiles`, `LoadAllSummaries`,
    `ResolveSessionDir`, `FindSession`, `FileToID` (JSONL session parsing,
    iteration/tool-result matching, bash exit-code extraction)
  - `formatter.go` — `FormatSession` / `Chat` / `Todo` / `Response` /
    `Thinking` / `Summary` / `SummaryTable` / `ToolFilter` / `JSONL`
  - `rawlog.go` — raw HTTP log pairing (`FindRawLogForSession` by timestamp
    proximity), `ParseFirstRequest`, `FormatRequestSystem`, `FormatRequestJSON`
- `cmd/agent/cli_log.go` — the CLI layer: `logUsage`, `parseLogArgs`
  (unit-testable, mirrors `cli_memory.go`/`cli_websearch.go`), `isLogSubcommand`
  dispatch, and the `run*` commands (`runRead`, `runLast`, `runFilter`,
  `runAllSummary`, `runRequest`).
- `cmd/agent/main.go` — added `|| isLogSubcommand(args)` to the subcommand
  dispatch check (checked before the general `-h` scan, so `gopi log -h`
  prints the log usage).

## Behavior

Identical to gopi-log-reader: reads `~/.gopi/sessions/*.jsonl` (session views)
and pairs with `~/.gopi/raw/gopi-raw-*.log` for `--request`. Short-ID prefix
matching, repeatable `--filter`, `--memory` (memory_search/memory_store),
`--last [N]`, `--all --summary`, `--text` plain mode, `--json` passthrough all
ported 1:1.

## Notes / decisions

- Subcommand name is `log`: `gopi log <id> [flags]`, `gopi log --last [N]`,
  `gopi log --all --summary`.
- Reuses the existing `memoryFlagSet` hand-rolled parser from `cli_memory.go`;
  repeatable `--filter` values are collected separately in `collectLogFilters`
  because that parser keeps only the last value per flag name.
- `cmdRequest` (stdout/exiting CLI part of rawlog.go) lives in
  `cmd/agent/cli_log.go` as `runRequest`; the gopilog package keeps only pure
  functions.
- No new tool_use functions were added (Rule 3: tools stay minimal) — this is
  a CLI subcommand, not a tool.
