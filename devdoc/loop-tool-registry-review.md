<!-- doc-class: historical -->
# Task Report: loop.go tool-registry & validation audit

## Completed
- **Declaration site**: `claudeToolDefs` is a package-level var at `internal/agent/loop.go:526`
  (`var claudeToolDefs = []deepseek.ClaudeToolDef{`, doc comment at loop.go:523). The slice
  spans ~lines 526–1001 and holds exactly **20 tool definitions** in Claude Messages API format.

- **Registered tools (file:line = the `Name:` field line of each def)**:
  | # | Tool | Line | # | Tool | Line |
  |---|------|------|---|------|------|
  | 1 | read | loop.go:528 | 11 | memory_store | loop.go:751 |
  | 2 | git_blame | loop.go:552 | 12 | memory_feedback | loop.go:782 |
  | 3 | write | loop.go:574 | 13 | memory_update | loop.go:809 |
  | 4 | edit | loop.go:597 | 14 | context_trim | loop.go:853 |
  | 5 | bash | loop.go:629 | 15 | job_start | loop.go:871 |
  | 6 | code_search | loop.go:657 | 16 | job_output | loop.go:885 |
  | 7 | keyword_search | loop.go:671 | 17 | job_kill | loop.go:899 |
  | 8 | web_search | loop.go:685 | 18 | todo | loop.go:913 |
  | 9 | web_fetch | loop.go:709 | 19 | vision_analyze | loop.go:922 |
  | 10 | memory_search | loop.go:728 | 20 | text2img | loop.go:940 |

- **validateToolArgs call-path assessment**:
  - Definition: `func validateToolArgs` at loop.go:3248 (doc comment loop.go:3239–3247).
  - **Sole production call: loop.go:2894**, the first statement of `executeToolDefault`
    (loop.go:2886), immediately after the nil-args guard — before the dispatch switch.
  - `executeTool` (loop.go:2877–2881) routes either to the `executeToolFn` test hook or to
    `executeToolDefault`. All task-loop execution funnels there:
    single-read fast path loop.go:2808, serial batch loop.go:2817, parallel batch loop.go:2852
    (inside `executeToolBatchParallel`).
  - Chat mode (`chat_ops.go`) builds its request with the same `claudeToolDefs` (chat_ops.go:128)
    and executes tool rounds via `a.executeToolBatch(toolCalls)` at chat_ops.go:190 → same choke point.
  - `validateToolArgs` case labels cover 19 groups = all 20 tools. `todo` (loop.go:913) has no case
    — correct, it declares `required: []string{}` (zero-arg read-only), so the switch falls through
    returning "" and validation is a no-op by design.

## Verdict
**PASS — validateToolArgs is called on every tool execution path.** There is exactly one
pre-dispatch gate (loop.go:2894) and every executor (task loop ×3 paths, chat ops) reaches it
through `executeTool → executeToolDefault`. Two caveats, both by design:
1. A test that sets `a.executeToolFn` bypasses validation — that is the documented test hook,
   never used in production wiring.
2. `todo` is absent from the validator (no required params); effective coverage is 20/20,
   explicit case labels 19/20.

## Not Done
- No code changes made or needed (read-only review).
- Deeper per-tool type-coercion audit (beyond required-param presence) out of scope.

## Verification
- Build: `go build ./...` — OK (no code touched).
- Tests: `go test ./internal/agent/ -run 'Tool|Validate' -count=1` — PASS (existing
  validator/dispatch tests in loop_test.go, memory_tools_test.go, vision_test.go,
  text2img_test.go, context_trim_test.go all exercise validateToolArgs).
- Commit: docs-only (this file).
