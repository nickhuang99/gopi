<!-- doc-class: historical -->
# Code Review: `gopi chat` / `gopi web` / `gopi shot` interactive surface

Review branch: `review/cli-chat-web` · Base: master `fc1dfc9` · Date: 2026-08-31
Scope: `cmd/agent/cli_chat.go`, `cmd/agent/cli_web.go`, `cmd/agent/cli_shot.go`
+ engines (`internal/agent/{chat_ops,chatregister,loop,context}.go`,
`cmd/agent/commands.go`, `internal/gopilog/rawlog.go`, `internal/shot`).
Review task = docs only — no code changes.

Verification baseline: `go test ./cmd/agent/ ./internal/agent/ ./internal/shot/
./internal/gopilog/` all PASS (chat/web/shot/commands/session suites).

---

## 1. Per-verb findings

### 1.1 `gopi chat` (cli_chat.go, 500 lines)

**Design purpose.** Interactive multi-turn REPL. Each turn runs on ONE shared
persistent agent (`PersistChatSession`, created once in `runChat`) and appends
user/assistant events to ONE session JSONL + ONE raw log (single-session
consolidation, commit 4a918cc). Per-turn context: `sess.history` re-captured
from `ag.Ctx.Messages` after every turn, fed verbatim into the next turn's
`Chat`/`ChatOps` seed — free-form Q&A, never re-framed as an `[EPIC GOAL]`.
`--tools` selects the `Agent.ChatOps` bounded ReAct loop (max 40 rounds);
`/tools` flips mid-session. Slash state commands (`/quit /clear /tools /help`)
are REPL-native; all other `/…` lines resolve through the **shared command
table** (`cmd/agent/commands.go`) so chat and web expose the same
`/task /log /text2img /code-search /memory` surface.

**Context growth is bounded and shared.** Multi-turn context does NOT grow
unboundedly: `Chat`/`ChatOps` reuse the task loop's `trimContext`
(loop.go:3448) — fold old tool results → LLM compaction → `TrimToFit` fallback
— keyed on `ContextWindow`/`CompactTriggerRatio`. Plain-chat turns carry no
tool results, but both paths share the engine, and compaction is the exact
code path the task loop is battle-tested on. This is the design answer to the
review prompt "does it grow unboundedly": no — same bounded loop, no drift.

**Two dispatch implementations? No — one.** Slash state commands are the only
duplicated surface and they are deliberately REPL-native (they mutate
`chatSession`, which the shared table cannot see). Everything else goes
through `execSlashCommand` — the single shared dispatch used by BOTH
carriers (`gopi chat`'s REPL and `gopi web`'s `handleChat`). No table drift:
same registry (`slashCommands`), same parse, same help. Verified by reading
both call sites (cli_chat.go:424, cli_web.go:659).

**Flag/env parsing is hand-rolled** (`--tools` stripped by a manual loop, no
flag library) — consistent with the sibling `cli_*.go` files in this repo.
`isChatSubcommand` also handles the hidden `chat list|backfill` verbs
(chat-table retrieval/migration). One historical dead code: `chat list` was
unreachable because the original `args[2:]` dispatch stripped the verb
(commit c7bbf0f — `chatIndexArgs` now keeps it; regression test added).

**Findings (P1/P2):**
- [P1] Chat exit on a mid-session API error loses the session row's end
  stamp. In `runChat` a turn error path calls `finishChat(…, 1)` which closes
  the session with exitCode=1 → chats-row `end_reason="interrupt"` even when
  the user could have continued. Worse, **the whole `gopi chat` process exits
  (return 1) on the first failing turn**, so a transient API/network error
  kills the REPL. An interactive REPL should surface the error and keep
  reading lines (or offer `/retry`). Note `runChat` has no `signal.Notify`
  at all (grep = 0 hits): Ctrl+C mid-turn kills the process; the session log
  ends up WITHOUT `session_end` (the JSONL is left open, no `CloseChatSession`)
  and the chats-table row stays `ended_at IS NULL` forever (in_progress
  forever). `gopi web` fixed exactly this in bug003; `gopi chat` never got
  the same fix. Also `RegisterChatSession` is called ONLY on success
  (`runErr == nil && ag.Cost != nil`) — so an API failure on turn 1 creates
  NO chats-table row; acceptable by design ("first-turn API failure leaves no
  row"), but combined with the above the chat table shows chat runs that
  never end.
- [P2] `chatRegistered bool` (cli_chat.go:302) is dead weight — comment says
  "kept for struct-shape parity with webServer"; the latch actually lives on
  the agent (`Agent.chatRegistered`). Same for `webServer.chatRegistered`
  (cli_web.go:303). Remove or repurpose.
- [P2] `continuedFrom` is stored but never surfaced after the seed line.
- [P2] `/clear` drops `sess.history`, and the agent's `Ctx.Messages` is reset on
  the next turn because `Chat`/`ChatOps` replace it with the passed history
  (`a.Ctx.Messages = append([]deepseek.Message{}, history...)`) — so `/clear`
  DOES work. Verified. Residual: `/clear` between turns only takes effect at
  the next model turn (an intervening `/tools` or `/help` line does not
  consult history — irrelevant). No bug.
- [P2] `iterCount++` counts only successful turns? No — it runs after
  `Chat`/`ChatOps` regardless of `runErr`, fine. But the `totalTokens`
  update runs BEFORE the `runErr != nil` early return... actually after, on
  the success path only — inconsistent stat on failed turns (minor).
- [P2] `--tools` state is per-session only; there's no env toggle for chat
  (web has `GOPI_WEB_TOOLS`). Asymmetric but probably fine — chat is
  interactive so a flag is natural.

**ChatOps drift check (vs the task ReAct loop):** ChatOps is a deliberately
lighter twin of runEngine — no EPIC framing, no project GOPI.md injection, no
task-list nudges, no task-ledger registration, no task-start memory_search,
and it exits on the FIRST no-tool round with a substantive answer
(`minFinalAnswerChars` gate + one-shot `continuationShot` degenerate guard).
Shared machinery (chatOnce/executeToolBatch/trimContext/job notices/per-round
Sync) is reused by reference, not copy. **No drift observed** — the two loops
only differ where the design says they must.

**Tool-safety notes:** tool execution in ChatOps runs with NO approval gate
and NO user confirmation inside the loop — every round just executes. This
matches claude-code-style ops chat and is explicitly documented (chat-ops
report + help banner), but worth restating: a `--tools` chat session is
local-shell trust with no per-call confirm. `GOPI_ALLOW_DANGEROUS` still
guards `rm -rf` etc. (bash tool level, not an approval UI).

### 1.2 `gopi web` (cli_web.go, 1080 lines)

**Design purpose.** Pure-Go zero-dependency browser chat UI: GET / serves the
embedded single-file HTML+JS page; POST /api/chat streams SSE
(`event: delta` per token → `event: done` with the full result); R2 tool turns
stream `event: tool` frames via `Agent.OnToolEvents`; R4 slash commands answer
with one `event: command` frame; R5 image uploads (≤8, ≤32 MiB decoded).
Since web-align P0, the **server owns the authoritative history**
(`s.history`, captured from `ag.Ctx.Messages` after every turn); the browser
POSTs only `{message}`; the legacy `history` field is decoded-but-ignored for
back-compat. One persistent agent for the whole run → ONE session id/JSONL/
raw log → ONE `session_end` at shutdown.

**History ownership (R1) is the right call.** The pre-P0 design (browser
re-shipped `history` every turn) was lossy (tool_use/tool_result dropped) and
body-capped (4 MiB); the transcript can legitimately exceed that once tool
results ride in. Server-side capture of `ag.Ctx.Messages` is exactly what
`gopi chat` does with `sess.history` — the two carriers now share the same
context semantics. The browser is a pure display, and refresh recovery is
served by GET /api/transcript (no pixels re-shipped for image turns).
`webChatMessage` (the legacy field type) is retained for decode compat.

**SSE correctness.** Frames are hand-written with correct
`event: X\ndata: …\n\n` framing; deltas are JSON-encoded so embedded newlines
survive; headers are set before the flush (`text/event-stream`, no-cache,
keep-alive, `X-Accel-Buffering: no`); a flusher check aborts cleanly on
non-HTTP writers; a terminal `event: done` is ALWAYS emitted (success or
error), and the JS `frames()` parser handles a mid-frame `__done__` after
stream end. The browser JS is a textbook SSE-over-fetch reader (buffer split
on `\n\n`).

**The one-`close-of-closed-channel` pitfall was root-caused and removed**
(comment at cli_web.go:~560): an earlier cancel-on-client-disconnect signal
channel that did `defer close(ch)` AND a goroutine closing on
`r.Context().Done()` panicked on "close of closed channel"; the channel was
dead code (nothing consumed it), so it was deleted — per-turn boundedness is
left to Agent.Chat's own internal Timeout, and SSE writes to a dead client
are harmlessly ignored. (memory #1084 corroborates.)

**Graceful shutdown (bug003)** is properly wired for web: SIGINT/SIGTERM →
`shuttingDown` flag (503 interlock) → `http.Srv.Shutdown(5s grace)` →
drain → `finalizeChatSession` → one `session_end` + chats-row close. The
finalizer takes `s.mu` so it can never race an in-flight turn. SIGKILL
remains a documented best-effort-impossible case.

**Findings (P1/P2):**
- [P1] `gopi web --tools` executes tools with NO approval gate (same as
  `gopi chat --tools`). Security posture: loopback-only DEFAULT
  (`127.0.0.1:8090` — commit 712ccd2 made loopback the default after a
  self-review major), opt-in LAN exposure via `--addr 0.0.0.0`. Documented in
  three places (help, banner, code comments) — the review prompt asks "no
  approval gate — documented?" YES, thoroughly, and the loopback default is
  the compensating control. Residual: anyone with loopback access (any local
  process / user) can drive shell-equivalent tools with zero auth. An
  approval gate is P3 per web-align-p0; consider at least a `--tools` +
  non-loopback bind warning (the banner exists; maybe refuse non-loopback
  `--tools` without an explicit `--allow-remote-tools`).
- [P1] `handleChat` runs the FULL model turn + SSE stream while holding
  `s.mu`, and there is NO per-request timeout — Agent.Timeout default is 0
  (unlimited). A stuck model request blocks every later /api/chat turn AND
  the graceful-shutdown finalizer (which takes the same mutex; `http.Shutdown`
  gives up after 5s and force-closes, but the in-flight `s.mu` holder keeps
  running until Agent.Chat's own context cancels — which is never, with
  Timeout=0). In practice the deepseek client's internal HTTP timeouts
  (ResponseHeaderTimeout 120s etc.) bound each round, so a hung turn
  eventually unwinds, but a 120s+ stall on a shared single-agent mutex is a
  real availability gap. Suggest: web turns should set a per-turn
  `Agent.Timeout` (env-overridable) before `Chat`/`ChatOps`, distinct from
  the task-loop default.
- [P1] **`/clear` equivalent does not exist in web** — there is no way to
  reset the server-owned conversation (browser reload replays via
  /api/transcript, but the transcript is the server's `s.history`; a reload
  does NOT clear it). `gopi chat` has `/clear`; web has none (slash commands
  only run the shared table). A "new conversation" button/command would
  require a server-side reset of `s.history` under `s.mu`.
- [P2] `handleChat`'s SSE streaming holds `s.mu` across the whole turn —
  correct for single-agent serialization, but two browser tabs POSTing
  concurrently queue serially; there is no feedback that turn 2 is queued
  (the 200+SSE headers are not sent until the lock is acquired). A turn that
  takes 60s leaves tab 2's request hanging with no bytes. Minor UX; the
  concurrency is safe (mu), just not visible.
- [P2] Image-size validation mismatch: server caps DECODED size at 32 MiB
  (`webMaxChatImageBytes`) while the browser's JS `MAX_B64` caps the BASE64
  string at `32*1024*1024*4/3+4` — which is the base64 of 32 MiB (the client
  limit is the ENCODED size, i.e. ~24 MiB decoded). So the client rejects
  images the server would accept (24–32 MiB decoded). Harmless direction
  (client more strict), but the two "32 MiB" claims are not the same budget —
  comment says "≤32MiB decoded" in JS yet computes encoded. P2 cosmetic.
- [P2] `handleTranscript` replays only `Content`; tool-call arguments and
  results are NOT re-shipped (only `tool_calls`/`tool_call_id`/`has_image`
  markers), so a --tools transcript after refresh shows bare "tool →" lines
  (JS renders `m.content` only) — tool activity context is lost on replay.
  Display-only (model context unaffected), but the replay UX for tool turns
  is a stub.
- [P2] `webServer.agent` mutated on every image turn (`ag.Model =
  s.visionModel` then restore) — safe under s.mu, but if the restore defer
  ever ran outside the lock domain it would race; currently inside. Fine.
- [P2] `maxWebHistoryMessages = 200` dead const (pre-R1 documentation), same
  for the dead `chatRegistered` field. Dead code cleanup candidates.
- [P2] `handleChat` slash commands do NOT touch `s.history` (by design — R4
  "command results are display, not conversation") — but the browser's
  replayTranscript also skips them, so after a reload the command history is
  gone from the display. Consistent with design, minor UX note.
- [P3] `json.Marshal` errors on the delta/tool/done payloads are silently
  dropped (`_ =` / `if err != nil { return }`), which would truncate the
  stream silently; deltas of pure text never fail in practice (strings are
  always JSON-encodable), so this is theoretical.

**XSS check:** model deltas, command outputs and tool summaries are all
inserted via `textContent`/`createTextNode`-equivalent (`div.textContent=…`),
never `innerHTML` — user/model content cannot inject markup. The only
`innerHTML` use is `renderStrip()` building `<img>` thumbs from
`im.media_type`/`data_b64` — but both are constrained by the server's
`validateWebImages` allowlist (media type must be in the 4-image set; base64
must decode), and the browser builds `img.src` as a data URI from that
base64. `media_type` flows into `data:` URI prefix — an attacker-controlled
`media_type` value could... no: server rejects anything not in the allowlist,
so the src prefix is one of 4 known types. OK. (Still, prefer building the
img via `document.createElement` + `img.src` — it already does — rather than
innerHTML string concat; the current code is safe but fragile.)

**Web --tools state flip mid-session:** `--tools` is a startup flag, NOT
flippable mid-session (web has no `/tools`). `s.tools` is read per turn under
no lock but is set once at startup — no race. OK.

### 1.3 `gopi shot` (cli_shot.go, 199 lines; engine internal/shot)

**Design purpose.** Drive a local headless Chromium over raw CDP (zero Go deps)
to PNG a URL or gopi's own embedded chat page (`--self`), feeding the vision
debug loop (`shot → vision_analyze → fix`). Chrome found via GOPI_SHOT_CHROME
→ playwright cache → PATH. `--self` starts a temporary static server hosting
the SAME `webChatPage` const (via `shot.SelfPageProvider`), or reuses a
running `gopi web` with `--addr`.

**Flag/parse:** hand-rolled `memoryFlagSet`, `shotFlagMembers` allowlist,
mutual-exclusion checks (`--url` vs `--self`), `--self` bare-flag swallowing a
trailing positional is caught, `parseShotArgs` is unit-testable (no os.Exit
in the parser). Matches the text2img CLI pattern.

**CDP engine** (internal/shot): named constants for all timeouts
(chromeStart 15s, screenshot 30s, wsHandshake 10s, WaitSelector 15s/250ms
interval) — Rule 1 compliant. Chrome process is always killed (defer), tmp
profile removed. flow: find chrome → `--headless=new
--remote-debugging-port=0` → parse "DevTools listening on ws://…" from stderr
→ GET /json/list → WebSocket Page.enable → setDeviceMetricsOverride →
navigate → wait (fixed + selector poll) → captureScreenshot → base64 → PNG.

**Findings (P1/P2):**
- [P1] CDP "listening" line is parsed from chrome's STDERR with a regex —
  brittle across Chrome versions/locales (Chrome may print localized or
  versioned prefixes; `--remote-debugging-port=0` + ws URL format changed
  across Chrome 100–130 era). If the line is missed the capture fails with a
  confusing error. Robustness suggestion: also probe `http://127.0.0.1:<port>/json/version` on the bound port. (There's a `--remote-debugging-pipe` alternative but that's a bigger rewrite.)
- [P1] **`--addr` reuse + graceful shutdown hole:** when shooting a live
  `gopi web` (`--self --addr`), shot takes the screenshot but the web server
  keeps running — fine. BUT shot's OWN temp server for plain `--self` binds
  `127.0.0.1:<ephemeral>`; the web page itself POSTs to `/api/chat` which the
  static temp server does NOT implement (404) — only the static HTML is
  served. For a screenshot that's fine (no interaction), but the page's JS
  will fire a failed POST on load if the model is auto... no, it only POSTs
  on user submit. OK — no issue.
- [P2] `runShot` wires `shot.SelfPageProvider` as a one-time global
  assignment at CLI level — a test running `runShot` twice with different
  pages would be fine (idempotent assignment of the same closure); only a
  problem if a second page provider were needed in-process. Minor.
- [P2] `--wait` and `--wait-selector` are both honored but there is no
  `--timeout` for the whole capture — the fixed waits can stack to
  2+15 = 17s+ per shot (bounded, fine, just not configurable).
- [P2] Chrome stderr parsing: if chrome prints the ws line with a different
  scheme or the `--headless=new` flag is not recognized by the found chrome
  (older/playwright builds), the whole flow fails — no fallback to
  `--headless` old mode. Suggest logging the chrome stderr tail on failure
  for debuggability (the current error may be opaque).
- [P3] Default viewport 1440x2800 is a magic-ish number but it IS a named
  constant pair with a doc comment (and the --self page is ~2800 tall), fine.

### 1.4 Cross-cutting: history & session semantics

**`gopi chat` seed (`resolveResumeLogPath`/`LoadCheckpoint`)** — chat reuses
the resume path's session→raw-log resolution. In `gopi chat <session-id>` the
given UUID resolves via `gopilog.SessionRawLogPath` (session_start's recorded
field — authoritative) else timestamp pairing (`FindRawLogForSession`:
nearest raw-log start ≤ `maxRawLogMatchDelta` 2 min, ambiguity = error).
`SessionStartTime` reads the session JSONL's `session_start` timestamp.
Sound. Residual: `gopi chat <path-to-raw-log>` and `<session-id>` are both
accepted — ambiguity between a file that exists and an id is resolved by
`os.Stat` first (a path wins). A 36-char UUID that happens to be a filename
would seed the file — acceptable.

**Chat vs web divergence in context-capture failure path:** chat captures
`sess.history` AFTER an error too (unconditional at cli_chat.go:~485) — no
wait, it's after the `runErr != nil` early return, so a FAILED turn's
`ag.Ctx.Messages` (which ChatOps may have partially filled with tool rounds
before the error) is NOT captured — next turn re-seeds the pre-turn history.
Web DOES capture after error (unconditional in handleChat). Behavioral
asymmetry, both defensible; chat's is arguably safer (a failed turn's partial
tool context doesn't leak into the next), web's keeps the transcript
continuous. Worth one comment each; not a bug.

**Chats-table end-reason vocabulary:** `CloseChatRow` maps exit!=0 →
"interrupt". For web, `finalizeChatSession` is called with graceful=true on
normal AND signal shutdown (both write exitCode 0 → "normal"); a web run
killed by SIGKILL writes no session_end at all (documented). The chats row
semantics are: ended_at NULL = never closed (crash/kill). Consistent with
memory #1098's finding that kill/Ctrl+C historically leaked in_progress rows —
bug003 fixed web's signal path; chat has no signal handler at all (see P1
above), so `gopi chat` Ctrl+C STILL leaks a session_end-less JSONL and a
never-ended chats row. This is the single most user-visible gap between the
two carriers.

**Rule 1 / Rule 2 compliance:**
- Magic numbers: all literals are named consts with doc comments
  (`chatWordFlushWindow`, `chatMaxBufferedBytes`, `webMaxChatBodyBytes` 4 MiB,
  `webMaxChatImageBytes` 32 MiB, `webShutdownGrace` 5s, `webMaxImagesPerRequest`
  8, shot's full timeout set). The one wart: JS `MAX_B64` inline math
  (base64-of-32MiB) is an encoded-size cap while the Go side is decoded-size —
  semantic mismatch, see web P2.
- Single source of truth: `webDefaultAddrValue` is the one loopback default
  const, read in exactly one place (`webDefaultAddr`); `GOPI_WEB_ADDR`/
  `GOPI_WEB_TOOLS` each read once. Timeout default `0 * time.Second` in
  NewAgent (agent loop) with `GOPI_TIMEOUT_SEC` read once. Chat uses the same
  agent defaults. `chatRegisterEnv` read once. Good.

---

## 2. Prioritized issues

### P0
(none found — the web SSE/history/session machinery is correct and
regression-tested; no data-loss or crash bug verified in the review scope.)

### P1
1. **`gopi chat` has no graceful-exit path** (cli_chat.go has zero
   `signal.Notify`): Ctrl+C mid-turn leaves the session JSONL without
   `session_end` and the chats-table row `ended_at IS NULL` forever. `gopi
   web` fixed this in bug003 (signal → Shutdown → finalizer); `gopi chat`
   never received the equivalent. Also a mid-session API error EXITS the
   whole REPL (return 1) instead of letting the user continue — and stamps
   the chats row "interrupt". Suggest: wire the bug003 signal/shutdown pattern
   into `runChat` (interrupt the in-flight turn via ctx cancel, then
   `finishChat`), and on turn error continue the REPL (print the error, keep
   reading).
2. **`gopi web` turns hold `s.mu` with no per-turn timeout** (Agent.Timeout
   defaults to 0/unlimited): a hung model request blocks all later turns AND
   the shutdown finalizer up to the client's internal 120s header timeout.
   Suggest a web-only per-turn `Agent.Timeout` (env-overridable).
3. **`gopi web` has no conversation-reset** — server-owned history
   (`s.history`) grows for the server's lifetime and there is no `/clear`
   equivalent (browser reload replays, does not clear). A /api/chat turn can
   never start fresh; add a reset command/endpoint under s.mu (mirror chat's
   `/clear`).
4. **CDP stderr-parsing brittleness in `internal/shot`**: chrome's
   "DevTools listening on ws://…" line is regex-parsed; a missed/unexpected
   line fails the whole capture with an opaque error. Suggest probing
   `/json/version` on the bound port as a robust fallback, and logging the
   chrome stderr tail on failure.

### P2
5. ~~`/clear` in `gopi chat`~~ — reviewed: `/clear` works (Chat/ChatOps
   replace `Ctx.Messages` with the passed history each turn, so nil history
   = fresh start). No issue.
6. Dead code: `chatSession.chatRegistered`, `webServer.chatRegistered`
   (latch moved to the agent), `maxWebHistoryMessages` const, and (mostly)
   `chatSession.continuedFrom`. Remove.
7. Web client/server image-size caps disagree on the 32 MiB budget (client
   caps base64 ENCODED ≈ 24 MiB decoded; server caps 32 MiB decoded) — align
   the client constant to decoded bytes so "≤32 MiB" means one thing.
8. `handleTranscript` replay of --tools turns renders bare "tool →" stubs
   (arguments/results not re-shipped) — display-only, but the tool activity
   context is lost after a refresh. Consider shipping one-line tool
   summaries (mirroring ToolEvent.Summary) in the transcript.
9. `gopi web` with two browser tabs queues serially on `s.mu` with no
   feedback (headers not sent until the lock is acquired) — a long turn
   leaves the second tab hanging byte-less. Minor UX; consider sending the
   200 + a "queued" SSE frame before acquiring the lock.
10. Web `--tools` + explicit non-loopback bind (`--addr 0.0.0.0`) is
    currently allowed with only a banner warning. Consider refusing (or
    requiring an explicit `--allow-remote` flag) — loopback is the documented
    security posture.
11. shot: whole-capture timeout not configurable (fixed waits stack);
    `--headless=new` has no old-mode fallback for older Chromes; chrome
    stderr tail should surface on failure.
12. Chat fails to stamp chats-row/`session_end` on turn-1 API failure by
    design ("no row at all") — the design comment says so, but a user who
    hits a transient API error in a fresh chat sees NO trace in the chats
    table or sessions dir... (session JSONL is created at NewAgent, so a
    session file exists but with no session_end — the crash-dump orphan
    backfill (BackfillOrphanCrashDumps) covers this on the NEXT run.)

### P3
13. `json.Marshal` errors silently dropped in SSE payload writes
    (theoretical — text is always encodable).
14. `--addr :PORT` two-token form refused with a hint; `--addr=:PORT` only —
    consistent but slightly un-flag-like; fine as documented.
15. `gopi chat --tools` has no env toggle (web has GOPI_WEB_TOOLS) —
    asymmetry only.

---

## 3. Design-history notes (git log / blame)

- chat-mode → web evolution is a coherent arc, not a reversal: chat REPL
  (e408008) → single-session consolidation (4a918cc) → ops chat `--tools`
  (b94b012) → web UI mirroring chat (f57ba0c) → web single-session
  (8dc5c67) → web-align P0 (9fe3d48: server-owned history R1 + --tools R2 +
  graceful shutdown bug003) → R4/R5 shared slash table + images (642e9bc) →
  chats table + chat/web split from task ledger (a4ba5cc) → shot (032e9f1).
  The one genuine design reversal is the web history ownership (browser
  re-shipped → server-owned), which was a deliberate P0 fix with documented
  lossiness/body-cap rationale — and chat and web now agree on the same
  capture-from-Ctx pattern, so the two carriers converged rather than
  drifted.
- Dead-flag archaeology: `chat list`/`backfill` unreachability bug (c7bbf0f)
  is the only "accumulated patch" smell; otherwise the flags added over time
  (--tools, --addr, --viewport, --wait-selector, image caps) are all live.
- The `finishChat`/`finalizeChatSession`/`CloseChatSession`/`CloseChatRow`
  chain is duplicated between cli_chat.go and cli_web.go with near-identical
  bodies (elapsed/iter/tokens bookkeeping). Acceptable at 2 sites, but a
  future third carrier should factor a shared `chatRun` struct.

## 4. Verdict

Well-engineered surface overall: the shared-command-table unification (R4)
eliminated the dispatch duplication the review prompt asked about, the
server-owned history (R1) is the correct architecture and is mirrored across
both carriers, SSE framing and shutdown paths are regression-tested and
correct, and XSS posture is sound (textContent everywhere user/model content
flows). The three P1s that matter are (1) `gopi chat`'s missing graceful-exit
(web got the bug003 fix, chat did not), (2) web's unbounded per-turn hold on
the single-agent mutex, and (3) web's lack of any conversation-reset. No P0
data-loss/crash bug found within scope.
