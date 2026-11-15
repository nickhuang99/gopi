<!-- doc-class: normative -->
# Task Report: gopi chat mode (CLI REPL)

## Summary
Implemented the **CLI chat mode** (`gopi chat`) — the "方案 A" lightweight path
from devdoc/gopi-chat-mode-effort.md. A multi-turn interactive REPL that streams
the model's text to stdout, with conversation context preserved across turns.

## Completed
1. **`Agent.OnText` streaming callback** (`internal/agent/loop.go`) — the *only*
   core-loop change the effort study flagged as necessary. Every text delta the
   model streams is now exposed per-fragment via `OnText (func(string))`, fired
   on **both** the SSE streaming path (`chatOnceAttempt` text_delta handler) and
   the non-streaming fallback (`ClaudeChat` path). This makes already-produced
   SSE data reach stdout in real time. Original `stderr` printing is untouched
   (no behavior change to existing single-shot runs).
2. **`gopi chat` REPL subcommand** (`cmd/agent/cli_chat.go` + dispatch in
   `cmd/agent/main.go`):
   - `gopi chat [raw-log-path|session-id]` — optional checkpoint seed to continue
     an earlier session; otherwise starts empty.
   - **Free-form turns**: each turn runs on the agent's **resume path** with the
     accumulated history (no `[EPIC GOAL]` framing — chat is Q&A, not a task run).
     This sidesteps the effort study's "skip EPIC framing" risk #4 for free-form
     dialogue.
   - **Multi-turn context**: after each turn the agent's in-memory `Ctx.Messages`
     are captured and passed as the seed of the next turn's `Resume` — context
     genuinely accumulates across turns (live-verified: a codeword told in turn 1
     is recalled in turn 2).
   - **Streaming**: `ag.OnText = fmt.Print` — the answer renders to stdout token
     by token as it arrives.
   - **Slash commands**: `/quit` `/exit`, `/clear` (forget context), `/help`.
     Exit via Ctrl+D (EOF).
   - **Per-turn session logging**: each turn builds a fresh agent (mirrors
     `gopi resume`), so every turn gets its own raw log + session JSONL row —
     `gopi log --chat` shows each exchange.

## Not Done (deferred, honest)
- **Ctrl+C mid-turn interrupt**: SIGINT during a long turn is not yet intercepted
  to cancel the in-flight request (the effort study's risk #3). Ctrl+Enter/EOF on
  input works; aborting a running turn is future work.
- **TUI mode** (方案 B, zero-dep ANSI renderer, scrollback, status bar): out of
  scope for this lightweight CLI task.
- **Inline continuation to the reuse of the *same* agent/session file across
  turns**: deliberately each-turn-fresh for clean `gopi log --chat` rows.

## Verification
- Build: `go build ./...` → OK; `go vet ./...` → clean.
- Tests:
  - `go test ./internal/agent/ -run TestOnText -race` → 2/2 PASS
    (`TestOnText_StreamingFragments`, `TestOnText_FallbackPath`).
  - `go test ./cmd/agent/ -run 'TestIsChat|TestRunChat'` → PASS
    (dispatch rule, `/help` returns 0, unknown seed errors, valid seed accepted).
  - `go test ./internal/agent/... ./cmd/agent/...` → both packages PASS
    (existing suites unaffected — no regressions).
- Live REPL (built binary, piped stdin):
  - `printf 'hi\n/quit\n' | gopi chat` → greeting streamed to stdout, `/quit` → "bye." exit 0.
  - `2+2?` → "4.4." single-turn answer streamed.
  - 2-turn codeword test → "remember ZEBRA" then "what was it?" → "**ZEBRA**"
    recalled, proving context continuity across turns.
  - Each turn produced its own session JSONL + raw log (verified under ~/.gopi).
- Commit: `e408008 feat(chat): add gopi chat interactive REPL + OnText streaming callback`

## Branch & Recovery
- **Branch**: `feat/chat-mode` (linked worktree `~/workspace/gopi-chat-mode`,
  shares the main repo's refs — commit is already in the main repo).
- **Recovery**: `git show feat/chat-mode:devdoc/chat-mode-report.md`
