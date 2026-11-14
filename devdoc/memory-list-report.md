<!-- doc-class: historical -->
# Task Report: memory_list — close the memory record-list missing chain

## Problem (Epic)
memory_get / memory_update / memory_delete all require a record id, but the
only way to discover ids was memory_search — which needs a query and does not
guarantee returning any given record. There was no way to enumerate the
knowledge base. Missing chain.

## Root-cause finding
The engine already had `tools.MemoryListRecent(n)` (internal/tools/memory.go)
— but it was wired ONLY to the `/memory list` slash command inside an
interactive chat session (cmd/agent/commands.go). The `gopi` CLI — where the
entire maintenance loop (memory_get/update/delete) lives — had no
`gopi memory_list` subcommand, so a headless operator could never enumerate
ids to feed memory_get. The chain existed in the engine, not in the surface.

## Completed
- Added `gopi memory_list [--limit N] [--db PATH]` CLI subcommand in
  cmd/agent/cli_memory.go:
  - dispatch case in `isMemorySubcommand` (memory_list added to switch)
  - parse case in `parseMemoryArgs`: positional-free, `--limit N` positive
    int validation, `--db` accepted
  - `memberOf("memory_list")` allows db/limit only (cross-subcommand flags
    rejected, mirroring strict tool-arg validation)
  - run case calls shared `tools.MemoryListRecent(opts.listLimit)` (0 →
    engine default 20) — CLI behavior identical to the engine
  - memoryUsage help text documents the subcommand as the enumeration entry
    point of the maintenance loop
- Advertised everywhere the CLI surface is documented/tested:
  - internal/config/help.go: `gopi memory_list` section
  - cmd/agent/usage_test.go: added "gopi memory_list" to the guard list
  - cmd/agent/cli_log.go toolFamilies "memory": added memory_list (log
    --family memory filter + its test)
  - cmd/agent/cli_log_test.go TestResolveFilterSetMemoryAddsMemoryTools
- Unit + chain tests in cmd/agent/cli_memory_test.go:
  - TestParseMemoryListArgs (defaults, --limit, --db)
  - TestParseMemoryListErrors (bad limit, positional, cross-flag --id)
  - TestCLIMemoryListThenGet — the missing-chain regression guard: stores 3
    rows, memory_list enumerates all ids, then the listed ids round-trip
    through memory_get / memory_update / memory_delete

## Not Done
- No new LLM tool_use added (memory_list stays CLI-only, same maintenance
  status as memory_get/update/delete — the Epic's ask was the CLI enumeration
  surface, and LLM-facing tools already have memory_search).
- No engine changes: MemoryListRecent already existed and was reused.

## Verification
- Build: `go build ./...` OK
- Tests: `go test ./cmd/agent/ -count=1` ok (full suite)
  `go test ./cmd/agent/ -run 'Memory|memory'` ok
  `go test ./internal/config/ -count=1` ok
  internal/tools suite running in background (no engine change; green history)
- E2E via real binary:
  ```
  gopi memory_store ×2 → gopi memory_list
  #2 [warm] e2e chain two / #1 [warm] e2e list chain ...
  gopi memory_list --limit 1 → 1 most recent
  gopi memory_get 1 → full record (id consumed from list output)
  gopi -h | grep -c memory_list → 1
  ```
- Commit: see feat/memory-list HEAD

## Deliverable declaration
- Branch: **feat/memory-list** (worktree /home/user/workspace/gopi-memory-list)
- Recovery: `git show feat/memory-list:devdoc/memory-list-report.md`
- Commit is on the branch in the MAIN repo (~/work/GoPi) via worktree sharing
  — no push needed.

## Merge resolution (into master, 2026-09-03)
Master restructured the memory CLI into the grouped surface (`gopi memory
<sub>`, Epic cli-subcommand-groups) after this branch forked (merge-base
6bb9073). Master's grouped design is canonical, so the branch's flat
`gopi memory_list` was grafted into the group:

- `gopi memory list [--limit N] [--db PATH]` — canonical grouped subcommand
  (dispatch case in runMemoryGroup, added to memorySubcommandNames,
  memberOf("memory_list") allows db/limit, parse case in parseMemoryArgs,
  run case calls shared tools.MemoryListRecent)
- `memory_list` stays working as a legacy flat alias via memoryLegacyAliases
  (prints the one-line deprecation note, routes to the group)
- help.go / usage_test.go / memoryUsage advertise `gopi memory list` in the
  grouped form (never the flat verb — master's anti-flat-verb guard test)
- cli_log.go toolFamilies "memory" + cli_log_test.go carry memory_list
  (auto-merged from the branch, master-consistent)

Conflicts resolved: cmd/agent/cli_memory.go, cmd/agent/usage_test.go,
internal/config/help.go (3 hunks each = usage text / alias map+dispatch /
help table). Engine (tools.MemoryListRecent) needed no change — already on
master via 642e9bc. E2E verified: `gopi memory store`×2 → `gopi memory list`
enumerates ids → `gopi memory_get <id>`; flat `gopi memory_list` prints the
deprecation note then lists identically.
