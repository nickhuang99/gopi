<!-- doc-class: normative -->
# Task Report: Web Chat Single-Session Consolidation

## Epic Goal
> gopi web chat has a similar issue as gopi chat mode which is a cli such that
> multiple rounds creates multiple session and log. It should mimic gopi chat mode
> to use single session and log file, and following chat rounds append to same log
> file. Also gopi task subcommand should also reflect its current status. Check gopi
> chat development, possibly consolidate the common thing — chat, cli or web is just
> an interactive and displaying level, the foundation below should be similar.

## What This Task Delivers
Gopi web chat now uses **ONE session id + ONE session log + ONE raw log** across
all turns — the same single-session foundation `gopi chat` already used after
commit 4a918cc. The browser (web) vs stdin REPL (cli) is only the
*interactive/display layer*; the *foundation* below is now consolidated:
both `gopi chat` and `gopi web` create **one persistent agent**
(`PersistChatSession=true`) up front and reuse it across every turn, appending
each turn's user/assistant/request events to the same JSONL, and write the
single `session_end` at exit (shutdown).

## Completed

### 1. Web server holds ONE persistent agent (cmd/agent/cli_web.go)
- Replaced `webServer.newAgentForTurn` (built a **fresh agent per** `/api/chat`
  request → a fresh session id + session log + raw log per turn, the pre-Epic
  per-turn bug) with:
  - `webServer.agent *agent.Agent` — the single persistent agent, created once
    in `runWeb` at startup with `PersistChatSession = true`.
  - `webServer.newWebAgent func() (*agent.Agent, error)` — the factory (tests
    inject a stub pointing at a mock SSE server).
- `handleChat` now **reuses `s.agent`** instead of building a new one per
  request. Every turn's events append to the SAME session id + session JSONL +
  raw log. Multi-turn context still comes from the browser-shipped `history`,
  but the persistence layer is a single conversation — identical to `gopi chat`.
- Added `webServer.finalizeChatSession(graceful)` — the direct mirror of
  `runChat`'s `finishChat`: called once when `http.Serve` returns, it writes
  the **single** `session_end` (via `CloseChatSession`) and flips the
  task-ledger row to completed (via `FinishSessionTask`).

### 2. Task subcommand reflects current status for web chat
- Mirrors `gopi chat`'s task-ledger registration: the **first successful** web
  turn calls `ag.RegisterSessionTask(msg)` (task_id = session id, summary =
  first message with `[auto]` prefix), leaving an `in_progress` row joignable
  against the one session JSONL.
- `finalizeChatSession` closes that row to `completed` with run stats
  (elapsed/iter/tokens), exactly like chat's finishChat.
- Empty web chats (server started, no turns) register nothing — no orphan rows.

### 3. Concurrency safety for the shared agent (web-specific hardening)
- `Agent.Chat` mutates unsynchronized agent state (`Ctx.Messages`, `Cost`). The
  chat CLI is strictly sequential (one stdin loop), but a web server can receive
  concurrent `/api/chat` requests (multiple tabs). Added `webServer.mu
  sync.Mutex`, held for the **whole turn including SSE streaming** — turns queue
  and never interleave on one session, preserving the single-session discipline.

### 4. Tests (cmd/agent/cli_web_test.go)
- `TestWebChat_SinglePersistentSession` — TWO turns through `handleChat` on one
  `webServer`: same session id + path, one `session_start`, two user + two
  assistant appended to ONE file, NO per-turn `session_end`, and exactly ONE
  `session_end` after `finalizeChatSession`. (Mirrors
  `TestChat_PersistentSession_SingleIDAcrossTurns`.)
- `TestWebChat_TaskLedgerReflectsStatus` — after a successful web turn the row
  is `in_progress` with the `[auto]` first-message summary; a second turn still
  uses the SAME row (one conversation, not per-turn rows); `finalizeChatSession`
  flips it to `completed`. (Mirrors `TestChatTaskRegistration_Wrappers`.)
- `TestWebChat_ConcurrentTurnsSingleSession` — 5 concurrent `/api/chat` turns
  on one webServer; runs green under `-race` and yields 5 user + 5 assistant on
  the one session file (proves the mutex serialization).

## Not Done
- No change to `internal/agent` — the single-session machinery already shipped
  in 4a918cc (`PersistChatSession`, `Agent.Chat` append-vs-close, `CloseChatSession`,
  `RegisterSessionTask`/`FinishSessionTask` wrappers). This task only wired the
  **web** front-end onto that same foundation.
- `finalizeChatSession` uses exit code 0 always (a web server crash on the last
  in-flight turn isn't propagated to the session_end/ledger as "failed"). For a
  local single-user chat this is acceptable; a future refinement could track a
  per-server failure latch. Currently `CloseChatSession(0, ...)` on any halt.

## Verification
- Build: `go build ./...` → OK
- Tests: `go test ./cmd/agent/ ./internal/agent/ -count=1` → `ok` (both), plus
  new web tests under `-race` green.
- Commit: `8dc5c67` on branch `feat/web-chat-single-session`

## The Consolidation (foundation shared by cli + web)
```
        gopi chat (CLI REPL)          gopi web (HTTP + SSE)
        ──────────────────            ────────────────────
        ONE agent, created once       ONE agent, created once in runWeb
        PersistChatSession=true       PersistChatSession=true
        reused every turn             reused every /api/chat turn
        ONE session id + JSONL + raw  ONE session id + JSONL + raw
        per-turn append               per-turn append
        /quit | Ctrl+D → close        http.Serve returns → finalize
        single session_end            single session_end
        task row: in_progress→done    task row: in_progress→done
```
Only the interactive/display layer differs (`bufio` stdin + `chatRenderer` vs
`net/http` + SSE + embedded HTML). The persistence/agent foundation is shared
verbatim through `Agent.Chat` + `PersistChatSession` + the task-registration
wrappers. This is the "foundation below should be similar" consolidation the
Epic asked for.
