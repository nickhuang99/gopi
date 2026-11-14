<!-- doc-class: historical -->
# Task Report: Simplified chat path — no tools, no task scaffolding + human-friendly buffered text

## Summary
Refined the existing `gopi chat` mode (devdoc/chat-mode-report.md, merged in
`e408008`) to match the Epic's two asks:
1. **Differentiate no-task/chat mode from a full task run** — added a dedicated
   `Agent.Chat` path that skips the entire ReAct loop: no tools, no task
   system prompt, no `[EPIC GOAL]` framing, no task-list nudges / task-ledger
   registration / memory-enforcement checkpoints, no iteration bookkeeping.
2. **Human-friendly text appearance** — replaced the raw `fmt.Print(delta)`
   token stream with a buffered `chatRenderer` that "pops in" the answer as
   coherent reading units (whole lines + whole words), never dropping the tail.

## What changed

### internal/agent/loop.go
- **`chatSystemPrompt` constant** — a ~1-line minimal chat-assistant system
  prompt replacing the ~4K GoPi tool-routing/task system prompt. Small = the
  model gives direct answers AND chat turns cost far fewer input tokens.
- **`Agent.Chat(userInput, history) (string, error)`** — the simplified single-
  turn path. Seeds the accumulated history verbatim (mirrors Resume), appends
  the user turn with **no EPIC framing**, builds the request with **no tools**
  (`nil` tool defs), uses `chatSystemPrompt` (no project/GOPI.md context), and
  does **one** `chatOnce` round-trip. Records the assistant answer into
  `Ctx.Messages` (so the caller captures the next turn's seed) and closes the
  turn's raw/session logs cleanly (fresh-session-per-turn, no `⏱` task stats
  line, no task-ledger row).
- **`Agent.ChatMode` field** — suppresses the raw stderr text print inside
  `chatOnce` (both the SSE `text_delta` path and the non-streaming fallback).
  Before this, chat text appeared on BOTH stderr (from `chatOnce`) and stdout
  (via the `OnText` callback) — a double print. Thinking stays on stderr, so
  the user still sees the model "work" while the answer streams cleanly to
  stdout. Reset on `Chat` exit so a reused agent still runs the normal loop.

### cmd/agent/cli_chat.go
- **`chatRenderer`** (`newChatRenderer` / `write` / `flush`) — buffers streamed
  deltas and flushes in human-friendly chunks: a whole line flushes immediately
  (natural reading pause); a completed word (trailing space) flushes once its
  `chatWordFlushWindow` (30ms) elapses; an oversized buffer flushes to keep
  visible progress; `flush()` at turn end never drops the answer tail.
- **`runChat`** now calls `ag.Chat(...)` instead of `ag.Resume(...)` and feeds
  the renderer via `ag.OnText`. The answer appears once, on stdout, in
  readable pop-in bursts.

### Tests (Rule 5: every fix ships a repro test)
- `internal/agent/chat_test.go`:
  - `TestAgentChat_SingleRoundNoTools` — exactly 1 API call (not the ReAct
    loop's confirmation rounds), no `tools` on the wire, no tool_use/tool_result
    content blocks, single correct answer, and the answer recorded in
    `Ctx.Messages` for next-turn seeding.
  - `TestAgentChat_SeedsHistory` — seeded history + user turn reach the wire,
    and NO `[EPIC GOAL]` framing.
- `cmd/agent/cli_chat_test.go`:
  - `TestChatRenderer_BufferedPopIn` — partial words buffer (not printed raw),
    newlines flush immediately, oversized buffers flush, word-window flush, and
    `flush()` never drops the tail.

## Not Done (honest)
- **Ctrl+C mid-turn interrupt** (still from the prior report): SIGINT during a
  long chat turn is not yet wired to cancel the in-flight request. Unchanged.
- **TUI mode** (方案 B): out of scope — the buffered renderer improves the CLI
  readability without a full ANSI TUI.
- Caching of the `chatSystemPrompt`: chat prompts are tiny so prompt-caching
  gains would be marginal; not added.

## Verification
- Build: `go build ./...` → OK; `go vet ./cmd/agent/ ./internal/agent/` → clean.
- Tests (all with `-race`): `go test ./internal/agent/ ./cmd/agent/ -race` →
  both packages PASS (no regressions; new Chat + renderer tests included).
  - `TestAgentChat_SingleRoundNoTools` / `TestAgentChat_SeedsHistory` PASS
  - `TestChatRenderer_BufferedPopIn` PASS
- Live: built `bin/gopi`, ran a piped 2-turn chat (codeword continuity across
  turns) — see the job/session log evidence below.

## Branch & Recovery
- Worked directly on `master` in `/home/user/work/GoPi` (solo agent, main repo —
  no worktree clone per Rule 6a's solo-agent allowance).
- Commit: `8300a57` `feat(chat): simplified Agent.Chat path (no tools/task scaffolding) + buffered human-friendly renderer`
