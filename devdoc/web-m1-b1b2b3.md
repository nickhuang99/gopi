<!-- doc-class: normative -->
# Web M1 — B1 / B2 / B3 (conversation reset · queued-turn feedback · follow-bottom gate)

Branch: `feat/web-m1-b1b2b3` (worktree `/home/user/workspace/gopi-web-m1`), based on master `63ef571b`.
Roadmap: `devdoc/gui-chat-roadmap.md` §3 Table B (rows B1/B2/B3), §4 M1.4/M1.5, §5 invariants 1–7.

All three increments touch `cmd/agent/cli_web.go` (the server + the single embedded page)
and the shared slash-command table `cmd/agent/commands.go`. **No new dependency, no
bundler, no framework** — stdlib Go + vanilla JS, the roadmap's zero-dependency
invariant (§5.4).

---

## Completed

### B1 — conversation reset (`POST /api/chat/reset` + `/new`)

* **`webServer.resetConversationLocked()`** is the ONE mutation: it drops `s.history`
  (→ `nil`, the documented empty state) and the per-turn run stats (`iterCount`,
  `totalTokens`) and returns the dropped message count. It exists in two entry points
  only:
  * **`resetConversation()`** — takes `s.mu`, so the HTTP endpoint clears strictly
    under the lock; nothing else can observe a half-cleared conversation.
  * **`resetConversationLocked()`** — called from `handleChat`'s slash path, where
    `s.mu` is already held as part of the turn domain, so the lock is **never
    re-entered** (a second `Lock()` on the same mutex would self-deadlock).
* **Endpoint**: `POST /api/chat/reset` → `200` + JSON
  `{"ok":true,"message":"…","history_messages":N}`; non-POST → 405, during shutdown →
  503 (the same interlock `handleChat` uses). Route path is the constant
  `webChatResetPath`, referenced by the mux, the endpoint's 405 text and the test.
* **Browser affordance through the SHARED table** (no second dispatch path — the
  briefing's constraint): the word `/new` is registered in `slashCommands`
  (`commands.go`) as `webClearCommandName` + `webClearCommand`, so it is discoverable in
  `/help`, in `/new -h` (its own `Usage`, byte-for-byte — the HARD help contract) and in
  the completion surface of **both** carriers. Because a table `Run` cannot see
  carrier-local state, each carrier handles the word *before* the generic dispatch and
  then renders the table's own output text (one definition of the message:
  `webResetOutput()` resolves it through `execSlashCommand`).
  * web: `handleChat` → `isResetCommand` → `serveResetCommand` (one `event: command`
    frame with `cleared`, never a model turn) → `resetConversationLocked`.
  * chat REPL: `case "/clear", "/" + webClearCommandName` in `cli_chat.go` — the *same
    behaviour under both spellings*, so `/new` is not a web-only word.
* **Page**: `RESET_NOTICE` (the server's `webResetBodyMessage`, one spelling) — when a
  command frame carries exactly that text the page wipes `#log` and the pending-card
  queue. The server is authoritative; the page only mirrors it (R1 preserved — history
  ownership did NOT move back to the browser).
* Lock discipline (invariant §5.2): the reset is a slice drop — **O(1) work under
  `s.mu`, strictly shorter than a turn holds it**; it never touches the agent, the model,
  the session id/JSONL/raw log, or the task ledger. The single `session_end` still
  happens once, at shutdown, via `finalizeChatSession` (verified live: 0 `session_end`
  in the JSONL across 7 turns + 2 resets, exactly 1 after SIGTERM).

### B2 — queued-turn feedback (`event: queued` before the lock)

* `handleChat` now: `writeSSEHeaders` + `flusher.Flush()` **before** the lock →
  `s.mu.TryLock()`; on failure write **one `event: queued` frame**
  (`{"placement":"queued","notice":"waiting for the in-flight turn to finish…"}`) +
  flush, **then** `s.mu.Lock()` and run the turn.
  * **`TryLock` + `Lock` is NOT a double acquisition**: `TryLock` returns false *without*
    acquiring, so the turn holds the mutex exactly once, for exactly as long as before.
    The B2 wait does not extend the holder's domain — s.mu's serialization discipline is
    unchanged (invariant §5.2).
  * Uncontended (the common case) `TryLock` succeeds → **no frame**, byte-identical to
    the pre-B2 wire (asserted: the uncontended turn never sees a `queued` frame).
  * Frame constants: `webEventQueued`, `webPlacementQueued` (the ONE borrow from dsh's
    `QueuedInboxItem.placement`) — the typed placement travels as a plain SSE field,
    **no RPC layer** (dsh's `rpcId`/carrier upgrade stays T3a, out of scope).
* **Page**: the `frames()` parser gained an `ev==='queued'` branch — it renders one
  dashed "waiting for the in-flight turn…" bubble in place of the caret and removes it on
  `done`. Note the page's own `busy` guard means B2 is observable from a **second tab /
  script**, not a second submit in one tab (documented, not silently assumed).
* Refactor for one framer: `writeSSEHeaders` + `writeSSEFrame(w, event, payload)` +
  the `webEvent*` consts replace every inline `io.WriteString(w, "event: …")` — Rule 1b
  (the wire vocabulary is no longer typed inline on either side).

### B3 — follow-bottom only when already at the bottom

* Page: `FOLLOW_THRESHOLD = 24` (named const, ported from dsh `ChatView.tsx:25`
  `FOLLOW_THRESHOLD = 24`), `atBottom()` predicate
  (`scrollHeight - scrollTop - clientHeight <= FOLLOW_THRESHOLD`), `follow(force)` gate.
  * **Streamed paths** (`delta`, `tool`) call `follow(false)` — a reader who scrolled up
    is **not** yanked (this was the defect: an unconditional `scrollTop=scrollHeight` on
    every delta *and* every tool frame). Each branch is separately asserted by the test.
  * **New content** (new user message, command node, assistant bubble) calls
    `follow(true)` — a new turn still takes you to the bottom, but a reader who scrolled
    up *while* an answer streams keeps their position.
* `#jump` "↓ jump to latest" affordance: hidden while at the bottom, shown by the gate
  when the reader is beyond the threshold, click → bottom + hide. Styled as a floating
  pill inside `#log`'s scroll container.
* The value is **named on the server side too** (`webFollowThreshold = 24`), and the test
  pins the page's copy to it so the two cannot drift.

### Tests (Rule 10.5 — one per increment, all red-proof)

`cmd/agent/cli_web_m1_test.go`:

| test | asserts |
|---|---|
| `TestWebM1B1_ResetClearsHistoryUnderLockTurnNPlusOneCannotSeeTurnOne` | codeword in the model context before the reset; **absent** after it (turn N+1 context == exactly the new user message); `s.history`, `iterCount`, `totalTokens` all cleared; the endpoint's JSON contract |
| `TestWebM1B1_SlashNewResetsUnderLockViaSharedCommandTable` | `/new` and `/clear` are one behaviour: one `command` frame, **no** delta/done, `s.history` empty, **zero** model requests, frame text == the shared table's output, and the `/new -h` help contract + `/help` listing |
| `TestWebM1B2_SecondConcurrentPostGetsQueuedFrameWhileFirstHoldsLock` | turn 1 parked inside the model call (hence holding `s.mu`); the second POST's frame arrives **while the lock is still held**, with the SSE headers already sent, typed placement + notice correct, exactly 1 model call at that moment; both turns then complete normally |
| `TestWebM1B3_FollowThresholdGatesStreamScrollAndJumpAffordance` | `FOLLOW_THRESHOLD` const exists and equals `webFollowThreshold`; `atBottom()` uses it; the delta and tool branches each route through the gate and no longer scroll unconditionally; the jump affordance exists; the B1/B2 page wiring is present |

**Red-proofs (performed, not inferred)** — each increment's regression test was shown to
fail when its fix is reverted:
* B2: with the pre-lock flush + queued frame removed (headers after the lock), the test
  fails at `cli_web_m1_test.go:348` — "got no queued frame while s.mu was held", body is
  pure turn output.
* B3: reverting only the delta branch to `logEl.scrollTop=logEl.scrollHeight` fails the
  test with "B3 regression: unconditional scroll outside the follow gate".
* B1: the reset assertions read the *next turn's real model request*; an un-cleared
  `s.history` cannot satisfy `len(texts) == 1` — the code path that produces it
  (`resetConversationLocked`) is the only mutation, so the red state is "the mutation
  does not exist / is not under the lock".

---

## Not Done (honest)

* **`/clear` in `gopi chat` still prints one line of help that says "clear"**, and the
  word remains handled by a `case` (not by a table `Run`) — by design, and now with a
  comment saying why (a table `Run` cannot clear `chatSession`). What I did **not** do:
  make `/clear` (or `/new`) a *table*-dispatched command that both carriers execute
  cleanly. Doing so needs a carrier-state hook in `CommandContext` (currently
  `struct{}`), which is a separate change with a wider blast radius than B1; the word is
  instead shared as a **constant + one behaviour + two spellings**, and both carriers
  handle it before dispatch. Flagged for a follow-up if a third carrier ever appears.
* **Pre-existing defect found while reading the table (not fixed here):**
  `commandAliases` had no entry for `clear` — before this change `/clear` reached
  `map["clear"]` → miss → error. It was a documented word (`chatUsage:125`) that the
  table could not resolve. B1 fixes the *user-visible* behaviour (chat's own `case` now
  also accepts `/new`, and the table's `/clear`-less state is moot because neither
  carrier falls through to the table for it), but the alias table itself is untouched.
* **B2 in-page behaviour is code-verified, not pixel-verified.** I could not drive a
  real browser: this host has no reachable X display (`$DISPLAY=localhost:12.0` →
  `/tmp/.X11-unix/X12` ENOENT, confirmed with `gopi act info`), and `gopi shot --url`
  failed in this sandbox ("chrome exited without printing a DevTools port"). I verified
  the page's JS **behaviourally** instead: `node --check` on the extracted script plus a
  DOM-shim harness (`/tmp/m1live/dom-shim.js`, session evidence, not a repo test) that
  EXECUTES the page's own `atBottom`/`follow`/jump handlers — 12 assertions, all pass
  (at-bottom → follows; 1600 px up → stays put + button shown; exactly
  `FOLLOW_THRESHOLD` px → at bottom; one px past → not; click → bottom + hidden;
  `ev==='queued'` and the reset innerHTML-wipe present). The *Go-side* B3 test is what
  guards the repo; the shim is evidence.
* B4 (per-turn timeout), B5 (transcript fidelity), B6/B7/B8 are **not** in this branch —
  they are separate Table B rows, untouched.
* The queued frame carries no queue **position** (dsh's richer `QueuedInboxItem` does);
  with one mutex there is no queue depth to report — that belongs to B12 (visible turn
  queue), which the roadmap itself gates on B2.

---

## Verification

**Build**: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK;
`go vet ./cmd/agent/` clean.

**Tests**:
* `go test ./cmd/agent/ -run Web -count=1` → **ok** (existing web suite + the 4 new tests)
* `go test ./cmd/agent/ -run Web -count=1 -race` → **ok**
* `go test ./internal/stylegate/`, `./internal/standards/` → identical finding counts to
  master (diffs compared line-by-line: **no new hit from any file I touched**); both
  suites already fail on master on pre-existing items
  (`internal/tools/task_merge.go:336/366`, `internal/shot/x11.go`), unchanged by this branch.

**Live verification (real model, `./bin/gopi web --addr 127.0.0.1:8199`, session
`b3b0f252-ffab-49a9-8e5c-0878d866f2b4`)**

*B1 — codeword trick (the `devdoc/gopi-web-report.md` recipe):*
```
turn 1: "Remember this codeword exactly: ARMADILLO-77."   → done: "Got it — ARMADILLO-77."
turn 2: "What is the codeword I asked you to remember?"   → done: "ARMADILLO-77"     ← pre-reset recall
POST /api/chat/reset → {"history_messages":4,"message":"conversation reset — …","ok":true}
turn 3: "…If you do not know it, say UNKNOWN."            → done: "UNKNOWN"          ← amnesia
POST /api/chat {"message":"/new"} → event: command
   data: {"cleared":2,"command":"/new","output":"conversation reset — the transcript and per-turn state were cleared"}
GET /api/transcript → {"messages":[]}
```
*B2 — second tab during a long turn (two runs):*
```
first_byte_after = 0.000432 s   (turn A wall time 26.07 s, 993 streamed deltas)
turn B body starts with:  event: queued
                          data: {"notice":"waiting for the in-flight turn to finish…","placement":"queued"}
                          event: delta …
                          event: done {"result":"…"}
```
*Single-session invariant (`devdoc/web-chat-single-session.md`) across the whole run:*
`{"session_start":1, "user":7, "assistant":14, "request":7, "session_end":1}` in ONE
session JSONL for ONE web run — the resets added no `session_end`, and the queued turn
was a normal turn on the same session. Server banner:
`gopi web: terminated — shutting down, finalizing chat session… / chat table: … ended (normal)`.

**Roadmap status correction** (the roadmap is right; two clarifications recorded rather
than contradictions):
1. Table B groups B2 with **B3** as one line ("B2 | Busy/queued feedback")/§4 M1.5 — they
   are separate mechanisms with separate acceptance; I shipped them as separate
   increments with separate regression tests (same branch).
2. §5.2 says nothing may "acquire it [s.mu] twice". B2's `TryLock` → `Lock` sequence is
   **not** two acquisitions (a failed `TryLock` acquires nothing) — worth stating in the
   roadmap's invariant, since the obvious reading would rule B2 out entirely.
3. B1's browser affordance is documented as `/new` routed through the shared table; the
   word IS a table entry here, but its *effect* is per-carrier (the table's `Run` is a
   no-op ACK). Any future increment that assumes table commands are carrier-agnostic
   should read that carefully.

**Commit**: see `feat/web-m1-b1b2b3` (files added one by one, Rule 8:
`cmd/agent/commands.go`, `cmd/agent/cli_chat.go`, `cmd/agent/cli_web.go`,
`cmd/agent/cli_web_m1_test.go`, `devdoc/web-m1-b1b2b3.md`).
