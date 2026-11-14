<!-- doc-class: historical -->
# Task Report: gopi log --memory reflects all memory tool calls

## Completed
- `cmd/agent/cli_log.go`:
  - `memoryToolNames` now = `memory_search, memory_store, memory_feedback, memory_update, memory_delete`
    (was missing `memory_delete`, the CLI maintenance command from round-3 memory CLI work).
  - `logUsage` help text: `--memory` line no longer claims "Only memory_search /
    memory_store calls" — now "Only memory tool calls
    (search/store/feedback/update/delete)".
  - Comment documents why memory_delete is included even though it is CLI-only
    (not an LLM loop tool): sessions may still log it.
- `internal/gopilog/formatter.go`: "No matching tool calls" hint now lists the full
  memory family (memory_feedback / memory_update / memory_delete) alongside bash/read/write/edit.
- Tests extended:
  - `cmd/agent/cli_log_test.go`: `TestResolveFilterSetMemoryAddsMemoryTools` asserts
    feedback/update/delete are added by `--memory`.
  - `internal/gopilog/reader_test.go`: `memorySession` fixture now contains
    `memory_feedback` and `memory_update` tool_use/tool_result pairs;
    `TestParseMemorySession` asserts input args (id/rating/reason, id/keywords) and
    matched results are captured for all four memory tools.
  - `internal/gopilog/formatter_test.go`: `TestFormatToolFilterMemory` renders
    feedback + update calls, header shows all four names (sorted).

## Not Done
- `memory_delete` is NOT exposed as an LLM loop tool (out of scope — it stays a CLI
  maintenance command; the log filter merely covers it if a session logs it).
- `internal/tools/memory.go` header comment ("memory_search / memory_store") left as-is;
  it is a package doc sentence, not user-facing CLI text.

## Verification
- Build: `go build ./...` OK
- Tests: `go test ./...` — cmd/agent, internal/agent, internal/config, internal/deepseek,
  internal/gopilog, internal/tools all PASS (memory DB scratch tests included, ~90s)
- Smoke: `gopi log -h` shows updated `--memory` line
- Commit: 41c8619 (branch feat/log-memory-all-tools, pushed to ~/work/GoPi)
