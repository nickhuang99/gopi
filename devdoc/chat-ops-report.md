<!-- doc-class: historical -->
# Task Report: Tool-enabled ops chat (`gopi chat --tools`)

## Summary
Gopi chat mode now covers the claude-code/hermes-style "operate my machine
from a conversation" use case. A new **tool-enabled chat turn**
(`Agent.ChatOps`) runs a bounded ReAct loop with the FULL GoPi toolset inside
the chat REPL, while the existing lightweight no-tools chat path stays the
default and is untouched. Single binary, no new deps, no new top-level tools.

Design choice (Epic offered freedom): rather than re-purposing the task loop
(`Run`/`Resume` — it would drag in `[EPIC GOAL]` framing, task-list nudges,
task-ledger rows, the bug022 double-confirm exit), chat got its own bounded
loop that reuses the task loop's *machinery* but none of its *task
scaffolding*. `gopi chat` alone remains the cheap Q&A path; `gopi chat
--tools` (or `/tools` mid-session) enables operations.

## Completed
1. **`Agent.ChatOps(userInput, history)`** (`internal/agent/chat_ops.go`):
   - History seeds verbatim, user turn appended verbatim — never framed as a
     task; no task-start memory enforcement, no task-list parsing/nudges, no
     ledger registration, no GOPI.md project injection.
   - FULL `claudeToolDefs` + full `assembleSystemPrompt()` tool-routing
     prompt on the wire (the model must know the tools exist — the #996
     lesson: routing说服 requires the prompt, not just defs).
   - Bounded loop (`maxChatOpsToolRounds = 40`): request → execute batch →
     tool_result with the same `[tool_use id: …]` + tick stamping the task
     loop uses → repeat; exits on the FIRST no-tool round carrying a
     substantive answer (`minFinalAnswerChars` gate). A terse/empty no-tool
     round gets exactly ONE continuation message
     (`chatOpsEndTurnContinuation`, `Agent.continuationShot` latch) — a
     claude-code-flavored "you have tools, use them" nudge; the second
     degenerate round is accepted as-is (no infinite loop).
   - Shared machinery: `chatOnce` (streaming/thinking-budget/fallback),
     `executeToolBatch` (parallel read-only subset), `trimContext`
     (fold→compact), job-completion notices, per-round log Sync.
   - Session bookkeeping identical to `Agent.Chat`: persistent single
     session id/log across turns under `PersistChatSession` (caller closes
     via `CloseChatSession`), no `⏱` task-stats line.
2. **Agent.continuationShot field** (`internal/agent/loop.go`) — one-shot
   degenerate-round latch, reset on every tool round.
3. **CLI** (`cmd/agent/cli_chat.go`): `--tools` flag; `/tools` slash command
   toggles ops on/off mid-session; banner when enabled; seed path arg
   correctly taken after flag stripping. Turn dispatch picks `ChatOps` vs
   `Chat` per `sess.toolsEnabled`.

## Not Done (honest)
- **Ctrl+C mid-turn interrupt** — pre-existing gap (both prior chat reports);
  unchanged.
- No per-turn round counting in `finishChat` stats (CostTracker has no
  request counter; round-level detail lives in the session JSONL LogRequest
  rows).
- `/tools` toggle does not retro-actively trim tool blocks already in
  history — a plain turn after an ops turn still seeds that history (correct
  context-wise, slightly heavier on the wire).
- The chat system prompt deliberately stays out of GOPI.md/config — no
  per-project customization of ops-chat behavior yet.

## Verification
- Build: `go build ./...` OK; `go vet ./internal/agent/ ./cmd/agent/` clean.
- Tests (`-race`): `go test ./internal/agent/ ./cmd/agent/` → both PASS.
  New: `TestChatOps_ToolRoundThenAnswer` (real `tools.Read` execution,
  tool_result rides back, answer relays content),
  `TestChatOps_FullToolsetAndPrompt` (≥10 tools, full prompt, no EPIC/task
  framing, not the tiny chat prompt),
  `TestChatOps_DegenerateRoundGetsOneContinuation` (exactly one continuation
  message, second degenerate round accepted),
  `TestChatOps_HistorySeededAndContinuity` (turn-2 seed carries turn-1
  tool_use/tool_result). Pre-existing Chat/renderer/persistent-session tests
  all still pass (no regression on the no-tools path).
- **Rule 11 live adoption proof** (LLM actually used the new mode's tools):
  - `printf 'Use your tools: run bash to print /tmp/ops-demo.txt…' | ./bin/gopi chat --tools`
    → session `9e652ea0-840b…` (glm-5.3-flash): 1 `tool_use` block (`read`
    /tmp/ops-demo.txt — model chose `read` over `cat` per routing), answered
    **OPSCODE-LIVE-4242** correctly.
  - `printf 'Count how many .go files are under cmd/agent/…' | ./bin/gopi chat --tools`
    → session `589cd625-499…`: 1 `tool_use` block (`bash`
    `find cmd/agent -name '*.go' | wc -l`), answered **27** (verified
    independently: 27 files).
  - Contrast: recent plain-chat sessions (`5fc250f4…`, `47992dcb…`,
    `9266f24f…`) carry **0** tool_use blocks — the no-tools default is intact.
  - Evidence lives in `~/.gopi/sessions/<id>.jsonl` (assistant events with
    `tool_use` blocks).

## Branch & Recovery
- **Branch**: `feat/chat-ops` (linked worktree `~/workspace/gopi-chat-ops`,
  shares the main repo's refs — commit `b94b012` is already in the main repo).
- **Recovery**: `git show feat/chat-ops:devdoc/chat-ops-report.md`
- Merge handled by PA after review (safe-merge guard per Rule 9).
