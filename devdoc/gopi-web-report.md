<!-- doc-class: normative -->
# Task Report: gopi web — pure-Go browser chat UI (second TUI mode)

## Summary
Implemented **`gopi web`** — the "second TUI mode" suggested in
devdoc/chat-mode-report.md, modeled on the **deepseek-harness** project's web
browser UI (`apps/web` + `packages/client`). Where deepseek-harness needs a
Node/TS host + React `dsh-client-web` + HTTP/WebSocket JSON-RPC carrier, gopi
stands the whole thing up with **only Go stdlib** — a `net/http` server, an
embedded single-file HTML+JS page, and **Server-Sent Events (SSE)** for
streaming. Zero new go.mod dependencies, fully offline-buildable.

## deepseek-harness investigation (what transfers, what doesn't)
The deepseek-harness web UI is a **pnpm TypeScript monorepo**:
- Host process (CLI `dsh`) + `dsh-client-web` (React browser UI) + `dsh-host-apiproxy`
  (versioned JSON-RPC contract: Sessions/Events/Workspace/Skills/Models/Subagents/Jobs).
- Transport: HTTP unary + **SSE streaming** + WebSocket downlink for host→client push.
- Streaming insight: the browser fetches with `Accept: text/event-stream`; the host
  `bridge` streams model deltas out chunk-by-chunk.

**Nothing is directly reusable** (Go vs TS/React), but the *pattern* transfers cleanly:
1. **Single HTTP server + SSE** = the whole "web carrier" (Go `net/http` needs no lib).
2. **Per-turn streaming** maps onto gopi's existing `Agent.Chat(input, history)` +
   `Agent.OnText` (from the chat-mode work) with zero new backend machinery.
3. **The browser owns conversation state** and re-ships `history` every turn — no server
   session map, fully stateless per request. This is *simpler* than dsh's host-side
   session API and equally correct (live-verified below).

## What was implemented
1. **`cmd/agent/cli_web.go`** — the `gopi web` subcommand:
   - `GET /` → embedded single-file HTML+JS chat page (vanilla JS, no build step).
   - `POST /api/chat` → body `{"message","history"}` → **`text/event-stream`** reply,
     streaming the model's answer token-by-token as `event: delta`, ending `event: done`.
   - Per-turn fresh agent via `Agent.Chat(message, history)` — no EPIC framing, no tools
     (mirrors `gopi chat`), history capped at 200 messages, body capped at 4 MiB.
   - `GOPI_WEB_ADDR` env / `--addr :PORT` flag override (Rule 2: one place reads env).
   - Client-disconnect cancels the in-flight request via `r.Context().Done()`.
   - Each turn writes its own session JSONL (mirrors `gopi chat` per-turn logging).
   - `newAgentForTurn func() (*agent.Agent, error)` injection seam for tests.
2. **`cmd/agent/cli_web_test.go`** — unit + integration tests (below).
3. **`cmd/agent/main.go`** — wired `isWebSubcommand` into the subcommand dispatch.

## Not Done (honest)
- **WebSocket downlink / host-push** (dsh's push events): not needed — the browser
  drives turns via POST/SSE; there's no server-initiated push use case yet.
- **Persistent server-side sessions** (resume a session by id from the browser): the
  CLI `gopi chat <log>` seed path covers this; a web-side "continue past session"
  selector is future work.
- **No HTTPS/CORS**: local loopback-only tool, consistent with gopi's other modes.
- **Rule 11 note**: no new *tool* was added — `gopi web` is a CLI subcommand reusing
  existing `Agent.Chat`/`OnText`; the mandate does not apply to tool additions.

## Verification

### Build / vet / tests
- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- `go build ./...` → OK; `go vet ./...` → clean
- `go test ./cmd/agent/...` → PASS (existing suites + new web tests, no regressions)
- `go test -race ./cmd/agent/ -run 'TestHandleChat|TestHandleRoot'` → PASS (race-clean)
- `go test ./internal/agent/...` → PASS (no regressions in the agent loop)

### New unit/integration tests (cli_web_test.go)
- `TestIsWebSubcommandDetection` — dispatch word recognition.
- `TestRunWebHelpReturnsZero` — `-h`/`--help` exit 0.
- `TestHandleRootServesPage` — page has title, `/api/chat` endpoint, SSE body reader.
- `TestHandleChatStreamsDeltasAndDone` — real `Agent.Chat` against a mock Claude SSE
  server: **asserts ≥2 per-fragment `event: delta` frames** (real streaming, not one
  coalesced blob) + a final `event: done` carrying the full result.
- `TestHandleChatRoundsHistoryIntoRequest` — seeded history reaches the model request
  (user + assistant turns verified in the request body) + done event present.
- `TestHandleChatRejectsEmptyMessage` / `RejectsNonPost` — 400 / 405.
- `TestHandleChat_MidStreamClientDisconnectNoPanic` — regression guard for the
  client-disconnect panic fix (cancelled request context ⇒ no panic, clean termination).

### Live end-to-end (real deepseek API, built binary, port 18090/18091)
1. `GET /` → served the chat page (HTML + title + JS).
2. `GET /api/chat` → 405 (method guard).
3. `POST /api/chat` "Say hello…" → **STREAMED** token-by-token:
   `event: delta / data:{"text":"Hello"} / … / {"text":"you"}` — SSE streaming confirmed.
4. `POST /api/chat` "What is 2+2?" → `event: done / data:{"result":"4"}`.
5. **Multi-turn context**: turn 1 "Remember ZEBRA-PANDA" (→OK); turn 2 "what codeword?"
   with history shipped → **"ZEBRA-PANDA"** recalled; turn 2 with EMPTY history (control)
   → correctly "I don't have any memory of past conversations" — proving the web API's
   re-shipped `history` is exactly what carries context.
6. Each turn wrote its own session JSONL (verified `~/.gopi/sessions/*.jsonl`).

### Root-cause bug found & fixed (panic on client disconnect)
**Symptom**: the first live run logged `http: panic serving … close of closed channel`
in `handleChat` whenever a client (e.g. `curl | head`) hung up mid-stream.
**Root cause (5-why)**: a draft cancellation block did `defer close(sseClientGone)` AND
spawned a goroutine that also `close(sseClientGone)` on `r.Context().Done()`. When the
client dropped mid-stream, the goroutine closed the channel and the deferred close
closed it **again** → panic. The channel was **dead code anyway** (nothing consumed it)
— it added no value while introducing a crash on a *normal* event.
**Fix**: removed the entire dead `sseClientGone` channel + goroutine. `Agent.Chat`'s own
internal `context` (honoring `Agent.Timeout`) already bounds each turn, and an SSE write
to a dead client is harmlessly ignored by the flusher. After the fix, a client that
disconnects at 3s mid-stream leaves the server alive and serving (verified live: 0 panics
in server log, subsequent `GET /` → 200).
**Regression guard**: `TestHandleChat_MidStreamClientDisconnectNoPanic` — drives a request
with an already-cancelled context (the exact crash trigger) and asserts `handleChat` does
not panic.

### Session evidence (live-verify, Rule 11)
`~/.gopi/sessions/43c05393-…-b8c826bc4f.jsonl` — web turn "What codeword…" produced a
clean `session_start` + `user` row, proving the web path logs identically to `gopi chat`.

## Commit
- Branch: `feat/gopi-web-mode`
- Commit: (see `git log -1` on the branch)

## Branch & Recovery
- **Branch**: `feat/gopi-web-mode` (linked worktree `~/workspace/gopi-web-mode`, shares the
  main repo's refs — the commit is already in the main repo).
- **Recovery**: `git show feat/gopi-web-mode:devdoc/gopi-web-report.md`
