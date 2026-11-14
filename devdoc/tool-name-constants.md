<!-- doc-class: historical -->
# Task Report: 工具名常量化 — Tool-name constants (single source of truth)

Epic: briefings/brief-tool-name-constants.md · Branch: feat/tool-name-constants

## Completed

- **New package `internal/toolnames/names.go`**: Go constants for all 23 real
  tool names (ToolBash … ToolText2img) — the single source of truth. Renaming
  or merging a tool is now a one-line change in this file (Rule 1).
- **`internal/agent/loop.go`**: replaced every bare tool-name string with the
  constants in:
  - `claudeToolDefs` schema `Name:` fields (23 sites),
  - `executeToolDefault` dispatch `switch tc.Name` (~23 case labels),
  - `validateToolArgs` `switch name` (~23 case labels incl. grouped cases
    like `case ToolRead, ToolWrite, ToolEdit`),
  - the three identity maps: `parallelSafeTools`, `pollingTools`,
    `contentReturningTools`,
  - context-slim / trim logic: `currentMemorySearchIDs`,
    `whitelistContextSlim`, `usefulVotedRecordIDs`,
    `trimCommittedTaskArea` (`write_task_area` match).
- **`internal/agent/compact.go`**: `protectedStoreRounds` memory_store match.
- **`internal/gopilog/formatter.go`**: `formatToolCall` +
  `formatChatToolCall` switch cases (read/write/edit/bash/git_blame and the
  grouped search-family case) + `searchToolQuery`.
- **`internal/gopilog/reader.go`**: `matchToolResult` bash ExitCode match +
  `collectContextTrims` context_trim filter.
- **`internal/tools/write.go` / `edit.go`**: `resolveSymlink` op labels.
- **`internal/tools/task_deliverable_branch.go`**: the parsed-record bash
  tool-name check (tool identity on parsed session records).
- **Guarding comments** at intentional-literal sites (see Not Done).

101 constant references in production code; zero remaining bare tool-name
identity strings in `internal/` (verified by grep).

## Not Done (deliberate — documented, zero behavior change)

- **cmd/agent CLI surfaces left as literals**: every remaining bare tool-name
  string lives in cmd/agent and is user-facing CLI syntax, not agent tool
  identity — shell names (`cli_completion.go` "bash"/"zsh"/"fish"),
  log family flags (`cli_log.go` "text2img"), CLI verbs / legacy flat
  subcommands (`cli_memory.go` memory_search/store/…, `cli_vision.go` …).
  These are public argv/flag surfaces analyzed separately in
  briefings/brief-hardcoding-review.md (ff68cbf); binding them to the tool
  constants would couple the CLI contract to the rename constant.
- **`internal/tools/memory.go` metaIngestionQueryPats**: content pattern over
  LLM prose ("memory_search" + human-phrase "memory search" as a deliberate
  pair) — constantizing would fragment the pair and wrongly track a renamed
  tool. Comment added explaining why.
- **`internal/tools/tasks.go` memory_feedback tableExists**: DB schema table
  name, not the tool — renaming the tool must not migrate the DB table.
  Comment added.
- **`internal/tools/task_deliverable_branch.go` raw-byte scan**: the
  `[]byte(`"bash"`)` fast path scans serialized JSONL wire bytes, not Go
  identity (the parsed check beside it uses ToolBash). Comment added.
- **`exec.Command("bash", "-c")`** (tools/bash.go, tools/job.go): the shell
  binary, not the tool name — must never follow a tool rename.

## Verification

- Build: `go build ./...` OK (all packages)
- gofmt: all changed files clean (ran `gofmt -w` only on changed files;
  restored 21 files a stray directory-wide gofmt had comment-reformatted)
- Tests: `go test ./...` — agent ok (cached), gopilog ok, tools ok
  (330.2s), toolnames [no test files]; full-suite job-2 green (see below)
- Grep: `case "…toolname"` / `.Name == "…"` identity bare strings in
  internal/ = 0 (all cmd/agent hits verified as CLI syntax)
- Behavior: tool-name string VALUES unchanged everywhere — pure refactor.

## Commit

04d1bce feat(toolnames): constantize all 23 tool names — single source of truth (Rule 1)
