<!-- doc-class: historical -->
# Task Report: chat-mode single session id + log append + task entry status

## Summary
The Epic asked for two fixes to `gopi chat` (multi-turn CLI):

1. **One single session id + one log file** — chat should be ONE conversation
   appending to ONE session JSONL (and raw log), not a fresh session id + log
   file per turn.
2. **Correct task entry status via DB** — chat turns should create/close a
   task-ledger row with the correct status, exactly like a full task run.

## Root Cause (5-why)
- **Symptom**: each chat turn printed its own `/gopi/raw` + `/gopi/sessions` path.
- **Mechanism**: `runChat` (cmd/agent/cli_chat.go) called `newAgent("")` on
  EVERY turn → `agent.NewAgent` → `NewSessionLogger` generated a brand-new UUID
  + a brand-new JSONL file per turn. `Agent.Chat` then closed that session log
  at each turn's end (`LogSessionEnd` + `Close`).
- **Norm violated**: a chat session is conceptually one conversation → one
  session id, one log (append). The task-ledger start/end hooks
  (`maybeRegisterSessionTask`/`finishSessionTask`) live only on the full-run
  `runEngine` path, so the Chat path created no DB task row.

## Completed
- **`internal/agent/loop.go`**:
  - New `Agent.PersistChatSession bool` field. When true, `Agent.Chat` does NOT
    close the session/raw log and does NOT write a per-turn `session_end` —
    it `Sync()`s after each turn instead (durable append; a crash loses at most
    the current turn, never the whole conversation). Default false preserves the
    original per-turn close (out-of-the-box behavior unchanged).
  - New `Agent.CloseChatSession(exitCode, iterations, tokens, elapsedSec)` —
    writes the SINGLE session_end at chat exit (accumulated cost across turns)
    and closes both logs.
- **`internal/agent/taskregister.go`**:
  - Exported `RegisterSessionTask(briefing)` and `FinishSessionTask(...)`
    wrappers around the existing unexported auto-registration hooks, so
    `package main` (the chat CLI) can create/close a ledger row. Reuses the
    exact same DB/summary/stats machinery as full runs.
- **`cmd/agent/cli_chat.go`** (`runChat`):
  - Creates **ONE agent** up front and reuses it for every turn
    (`PersistChatSession = true`) → ONE session id + ONE session JSONL + ONE
    raw log; each turn appends its user/assistant/request events.
  - `RegisterSessionTask` after the first successful turn (task_id = session id,
    summary = first turn's input); `CloseChatSession` + `FinishSessionTask` at
    exit (`/quit` / Ctrl+D / error) — ledger row flips in_progress → completed/
    failed and gets the accumulated run stats.
  - Empty chats (/quit before any turn) register nothing — no orphan rows.

## Tests
- `internal/agent/chat_session_test.go` (new):
  - `TestChat_PersistentSession_SingleIDAcrossTurns` — two `Chat` calls on one
    agent keep the SAME session id + path; one session JSONL contains 1
    session_start, 2 user + 2 assistant events, and **0 per-turn session_end**;
    `CloseChatSession` appends exactly ONE session_end.
  - `TestChat_Default_PerTurnClose` — default (no PersistChatSession) still
    closes per turn (1 session_end) → no regression.
- `internal/agent/taskregister_test.go` (new):
  - `TestChatTaskRegistration_Wrappers` / `_WrapperFailedStatus` — the exported
    wrappers create an in_progress row then flip to completed (exit 0) / failed
    (exit 1) with stats appended.
  - `TestChatTaskRegistration_NoRowWhenUnregistered` — FinishSessionTask with no
    prior registration is a no-op (no orphan rows).
- **Live E2E (Rule 11-style, mocked SSE server)**:
  - Piped `hi there` → `what was the code?` → `/quit` into `./bin/gopi chat`
    with `ANTHROPIC_BASE_URL` pointing at a local fake Claude-Messages SSE
    endpoint and an isolated `GOPI_HOME`.
  - Verified: ONE session id printed, ONE `.jsonl` session file containing
    `session_start` → 2×(user, assistant, request) → `session_end(exit=0, iter=2)`;
    `gopi task list` (same GOPI_HOME) shows the row as
    `[completed] [auto] hi there — elapsed=1.8s iter=2 tokens=0` with
    `task_id == session_id` and the model persisted.

## Not Done
- Ctrl+C mid-turn SIGINT interruption (pre-existing, out of scope).
- No CLI flag to opt chat *out* of task registration (auto-registration follows
  the existing `GOPI_AUTO_TASK_REGISTER` kill switch).

## Verification
- Build: `go build ./...` OK; `go build -o bin/gopi ./cmd/agent/` OK.
- Tests: `go test ./internal/agent/ ./cmd/agent/` PASS; `-race` PASS (both suites
  green, job-3).
- Commit: see final git hash on `feat/chat-single-session`.
