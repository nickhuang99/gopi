<!-- doc-class: normative -->
# Devdoc: gopi log tool-family gap fill (`--search`/`--web`/`--job`/`--context`/`--grep`)

## Problem

The agent loop (`internal/agent/loop.go`) exposes 17 tools. The `gopi log`
subcommand (integrated gopi-log-reader) only had a dedicated `--memory`
shortcut for the memory family and a generic `--filter TOOL` for everything
else. The search tools (`code_search`, `keyword_search`) and web tools
(`web_search`, `web_fetch`) — the newest toolcalls — had no first-class
support, and there was no way to *search* a session's content from the log
subcommand (the log-side analogue of the agent's code/keyword search).

## Gap table (17 agent tools vs gopi log before this change)

| Family | Tools | Before | After |
|---|---|---|---|
| file | read/write/edit | `--filter <name>` | unchanged |
| bash | bash | `--filter bash` | unchanged |
| memory | memory_search/store/feedback/update/delete | `--memory` | unchanged |
| **search** | code_search, keyword_search | `--filter <name>` only | **`--search`** |
| **web** | web_search, web_fetch | `--filter <name>` only | **`--web`** |
| **job** | job_start, job_output, job_kill | `--filter <name>` only | **`--job`** |
| **context** | context_trim | `--filter <name>` only | **`--context`** |
| todo | todo | `--todo` (view) | unchanged |
| — (search capability) | keyword_search analogue | none | **`--grep TEXT`** |

## Design

1. **`toolFamilies` registry** (`cmd/agent/cli_log.go`): one map from
   shortcut flag → tool names, mirroring `internal/agent/loop.go`. Adding a
   toolcall to the agent loop = one line here. `familyOrder` keeps the
   expansion/display order deterministic. `filterActive`/`resolveFilterSet`
   were reworked from `(flagMemory bool, filterNames []string)` to take
   `*logOptions` so every family composes uniformly (families + explicit
   `--filter` names union into one set).
2. **`--grep TEXT`** (`internal/gopilog/formatter.go` `FormatGrep`):
   case-insensitive substring search over tool-call name/input/result, text
   responses and thinking blocks. Empty tool set = search all content;
   non-empty set (from family/`--filter` flags) = search only tool calls in
   that set. `gopi log <id> --grep x --job` = "job tool calls mentioning x".
   Wired into `runLogSubcommand` dispatch, `runLast` (applies per session)
   and `renderSessionView`, so `--last N --grep` works.
3. **Chat view for search tools** (`formatChatToolCall`): `code_search`
   shows the `symbol`, `keyword_search`/`web_search` show `query`,
   `web_fetch` shows `url`, plus a 120-char first-line result summary.
   `searchToolQuery` falls back to `%v` of Args when the field is missing.

## Pitfalls encountered

- The shared hand-rolled `memoryFlagSet` keeps only the *last* value per
  flag name, so `--grep` is single-valued by design (like `--last`); only
  `--filter` is repeatable (its own collector pass exists already).
- A boolean shortcut flag with a following bare token is consumed as the
  flag's value (`--search x` silently drops `x`) — inherited from the
  shared flagset; not special-cased to avoid diverging from `--memory`.
- `grep -c` over the filter view counts the *header* (`Filter: web_fetch,
  web_search`) — smoke tests must count `TOOL `/`TEXT `/`THINKING ` lines
  or check tool_use_id presence, not raw name matches.

## Verification

- `go build ./...` OK; `go test ./cmd/agent/ ./internal/gopilog/` pass.
- `internal/deepseek` + `internal/tools` test failures are pre-existing on
  master (env model default + SQLite migration race) — unrelated.
- Live smoke on real sessions: `--search`, `--web`, `--job`, `--context`,
  `--grep`, `--grep x --search` (scoped), `--last 2 --grep`, `--last 2
  --web`, `--chat` search-tool rendering, `gopi log -h` — all verified.
