<!-- doc-class: roadmap -->
# GUI / Chat Front-End Roadmap — borrow survey, gap analysis, effort tables

Research + design deliverable. **No production code changed** (this branch adds this
file only). Scope: `gopi chat` (CLI REPL, cooked-mode input) and `gopi web` (embedded
browser UI), against three external front-ends read on this box.

- Base: master `0dac2ae7`
- Branch: `docs/gui-chat-roadmap` (worktree `~/workspace/gopi-gui-roadmap`)
- References read in full/part (paths are absolute on this box):
  - `~/work/hermes-agent/cli.py` (16 362 lines) + `~/work/hermes-agent/hermes_cli/web_server.py` (16 926 lines)
  - `~/work/deepseek-harness` (TS/React pnpm monorepo: `packages/host/apiproxy`, `packages/client/ui-conversation`, `packages/client/ui-input-trigger`, `apps/web`)
  - `~/work/codex/codex-rs/tui` (Rust/ratatui, third reference)
- Prior gopi docs this roadmap is anchored on: `devdoc/gopi-chat-mode-effort.md`
  (the original 方案A 2–4d / 方案B 5–8d / full-TUI 3–4w estimate),
  `devdoc/chat-mode-report.md`, `devdoc/chat-ops-report.md`,
  `devdoc/chat-single-session.md`, `devdoc/web-chat-single-session.md`,
  `devdoc/gopi-web-report.md`, `devdoc/review-cli-chat-web.md`.

---

## 0. TL;DR

| question | answer |
|---|---|
| What is actually broken today? | `gopi chat`'s **input path**: cooked-mode `bufio.ReadString('\n')` (cli_chat.go:424/440). No history, no cursor editing, no multiline, no paste guard. A pasted 10-line block becomes **10 turns**, not one. |
| Is `golang.org/x/term` an option? | **No.** It is not in `go.mod`, not in `go.sum`, not in the module cache, and unreachable offline. The briefing's "golang.org/x/term indirect only" is factually wrong (see §2.3). |
| So what do we build the editor on? | **Zero new dependency**: `syscall` termios (probe-compiled green on this box) or `golang.org/x/sys/unix`, which is **already** a direct dependency (go.mod; used in `internal/daemonlifetime/binding.go:40`). Recommended (§2.4). |
| CLI effort | 6.5–8.5 days total, split into 5 independently shippable increments (Table A). |
| Browser effort | 8 quick wins ≤1d each, 5 medium items 2–5d, 3 large items 1–2w — with 2 explicitly **rejected** (Table B). |
| Sequencing | M1 (two reversible, independent tracks) → M2 → M3 explicitly deferred. §4. |
| What must NOT break | single-session consolidation + concurrent-turn mutex discipline (§5). Every increment ships a regression test. |

---

## 1. Borrow survey — three references, with file:line citations

### 1.1 Reference 1 — hermes-agent (Python, prompt_toolkit + rich TUI; FastAPI web)

**TUI shape.** A prompt_toolkit `Application` built with `full_screen=False`
(Application at `cli.py:15120`, `full_screen=False` at `cli.py:15124`), `erase_when_done=True`
(`cli.py:15144`), and
`refresh_interval=float(CLI_CONFIG.get("display", {}).get("cli_refresh_interval", 0))`
(`cli.py:15133`, with the rationale in the comment block directly above it). The inline comment is the single most transferable design note
in the whole reference: *"Set to 0 to suppress background redraws entirely — avoids
fighting terminal auto-scroll in non-fullscreen mode (Xshell, iTerm2, Windows
Terminal). See #48309."* — i.e. **do not run a timer-driven repaint against a
terminal whose scrollback the user owns**; repaint on events only.

**Input widget.** `TextArea(height=Dimension(min=1, max=8, preferred=1), multiline=True,
wrap_lines=True, read_only=Condition(...), history=FileHistory(str(self._history_file)),
completer=ThreadedCompleter(_completer), complete_while_typing=True)` at
`cli.py:14315-14332`. Three borrowable decisions:

- **History is a file, not a ring in memory**: `.hermes_history` under the app home
  (`cli.py:4127`) — *"in-memory only, never persisted across restarts"* — replayed across
  sessions by `FileHistory` (`cli.py:14322`).
- **The completer runs off the render loop**: the comment at `cli.py:14323-14329`
  explains the completer shells out to `rg`/`fd` with up to a 2 s timeout and hits
  `os.listdir/stat`, so inline completion would stall rendering on every keystroke —
  hence `ThreadedCompleter` (`cli.py:14329`). Applies directly to gopi's `/` command
  menu if it ever shells out (repo scan, file completion).
- **The input grows to 8 rows then scrolls** (`max=8`) rather than one line.

**Multiline keys.** `'escape','enter'` → `event.current_buffer.insert_text('\n')`
(`cli.py:13653-13662`), with an explicit carve-out documented in the docstring: Windows
Terminal intercepts that chord at the terminal layer, so Windows users get the newline
via `c-j` instead (bound conditionally just below, `cli.py:13665+`). The submit bindings
are centralized in `_bind_prompt_submit_keys(kb, handler)` (`cli.py:3269`, called at
`cli.py:13651`) — one place decides "Enter submits, the chord inserts a newline".

**Streaming into a live prompt.** This is the part gopi's CLI will collide with the
moment a line editor owns the terminal. hermes imports `patch_stdout`
(`cli.py:61`), and its `_cprint` docstring states the trap verbatim: *"Raw ANSI escapes
written via print() are swallowed by patch_stdout's StdoutProxy. Routing through
print_formatted_text(ANSI(...)) lets prompt_toolkit parse the escapes and render real
colors."* (`cli.py:2689-2695`). Consequently model text is **line-buffered** before
emission (`_stream_delta`, `cli.py:5776-5800`), reasoning is streamed into a dim box
(`_stream_reasoning_delta`, `cli.py:5722-5726`), and a `ChatConsole` Rich adapter
(`cli.py:3500-3511`) routes rendered output through the same renderer.

**Terminal state restore.** hermes keeps a *full* termios snapshot, not a bit mask —
the comment (`cli.py:938-951`) says hand-OR-ing `ECHO|ICANON|ISIG` back cannot cover
what raw mode clears (`IXON, ICRNL, BRKINT, OPOST, IEXTEN, VMIN/VTIME`), and it
snapshots precisely because SSH multi-PTY makes the library's own restore unreliable.
The snapshot is taken with `termios.tcgetattr(sys.stdin.fileno())` (`cli.py:955`).

**Web side.** `FastAPI(title=..., lifespan=_lifespan)` at `web_server.py:255`.
Transport is **not** SSE: `/api/ws` is a JSON-RPC WebSocket (`web_server.py:15279`),
`/api/events` is a second WebSocket for dashboard→browser push (`web_server.py:15338`),
and `/api/pty` (`web_server.py:13831-13836`) spawns *the same* `hermes --tui` binary
behind a POSIX pty and forwards bytes + resize escapes to `xterm.js`
(`web/src/pages/ChatPage.tsx`). **Borrowable as a concept, rejected as an
implementation for gopi** — see Table B, T3b: it needs a JS dependency (xterm.js) the
offline, no-bundler box cannot fetch.

**Busy-input policy as a settings row (borrowable, and it names the exact gap in §2.2).**
hermes exposes `display.busy_input_mode` as a *select* with options
`interrupt | queue | steer` (`web_server.py:661-663`), plus `display.resume_display`
(`minimal|full|off`, `:657-660`) — i.e. "what happens to input typed while a turn runs"
and "how much history a resumed session shows" are both first-class user preferences,
declared in one settings table and rendered by the dashboard. gopi web's W3 is exactly
the `queue` case with none of the plumbing; dsh's `QueuedInboxItem.placement` (§1.2) is
the same idea carried on the wire.

### 1.2 Reference 2 — deepseek-harness (`dsh`, Node/TS + React)
**The contract, not the code, is the asset.** `packages/host/apiproxy/README.md` (83
lines) describes a four-quadrant wire union — who initiates × request/response —
decoupled from the carrier: `ClientRequest` = `POST /api/<method>` body, `ServerResponse`
= that POST's response body, `ServerRequest` = SSE frame, `ClientResponse` =
`POST /api/respond` body (README.md:21). Responses echo `rpcId` and never mint a new
one; business errors ride the result's error branch while **HTTP status expresses only
the carrier**; and every `/api` POST must declare `application/json`, anything else
refused with **415 before dispatch** so cross-site "simple" requests can never execute a
side-effectful method blind (README.md:21). The typed shapes are
`RpcRequest<P>`/`RpcResponse<T>` (`packages/host/apiproxy/src/api/rpc.ts:131-140`) with
a **closed** error-code union derived from `RpcErrorDetailsMap`
(`src/api/rpc.ts:32-36`, `:98-106`).

**Streaming events.** `packages/host/apiproxy/src/api/events.ts`: `session/event` frames
carry an optional host-computed render intent (`view?: ToolEventView`, `:32`, `:70`),
and `session/queue` frames carry `QueuedInboxItem[]` with an explicit
`placement: 'queued' | 'steering' | 'context'` (`:37-41`, `:84`). **Two ideas worth
taking even without the RPC layer**: (a) the host may attach a *render intent* to a tool
event so the client does not re-derive it; (b) a message that arrives while a turn runs
has a **typed placement** — the UI can show it as *queued* rather than silently
swallowing it (exactly the gap `review-cli-chat-web.md:180` records for gopi web).

**Composer UX.** `packages/client/ui-conversation/src/client/skeleton/InputBar.tsx`
(763 lines) is the concrete checklist of browser-input edge cases:

- `onKeyDown` (`:280`); **IME guard** — a composition-closing Enter picks a candidate
  and must not send (`:104`, `:282`);
- **Shift+Enter is an unconditional native newline, decided *before* the IME guard**
  (`:291-293`) so a composition-closing Shift+Enter still breaks the line;
- Enter with a menu open is *arbitrated* (highlight wins, `:341-343`);
- `if (e.repeat) return` — *"held-down Enter must not machine-gun sends"* (`:351`);
- an empty-draft accelerated Enter acts on the **queue** instead of submitting (`:354`).

**Submission policy as a first-class object.**
`input/submission-policy.ts:22-77`: `ComposerSubmissionPolicy.resolve(running, gesture,
steeringAvailable)` (`:48`) turns a keystroke into a delivery mode (queue vs steer) from
a live user preference, and the *same* store backs the Settings row.

**Input as a pure state machine.**
`input/machine.ts` (604 lines) — "Events in, effects out; zero React / DOM / cordis /
ambient clock", draft truth in one string, one transaction per mutation, a bounded undo
log (`LOG_LIMIT = 100`, `:42`). The reason this matters: the editor's behaviour is
testable without a terminal.

**Scroll and render economics.** `chat/ChatView.tsx`: `FOLLOW_THRESHOLD = 24` (`:25`),
bottom-follow only when the reader *is* at the bottom (`isAtBottom` at `:271-275`),
prepend anchoring that preserves the read position when older rows load (`:291`).
`chat/use-throttled-visual-update.ts` (`:4`, `DEFAULT_INTERVAL_FRAMES = 3`,
`:12`) coalesces non-essential visual alignment over a frame interval — used by
`chat/ReasoningRow.tsx:31` for streaming reasoning.

**Menus.** `packages/client/ui-input-trigger/` splits a pure `core/detect.ts` decision
from a `client/MenuView.tsx` render; the same model serves `/` commands and `@`
references.

### 1.3 Reference 3 — codex TUI (Rust / ratatui)

Chosen as the third reference because it solves the problem gopi's CLI will have
*before* it has a TUI: **where does finalized output live while an editor owns the
bottom of the screen?**

- Module docstring (`tui/src/insert_history.rs:1-4`): *"Codex uses the terminal
  scrollback itself for finalized chat history, so inserting a history cell is an
  escape-sequence operation rather than a normal ratatui render."* The mechanism:
  `SetScrollRegion(range)` writes `\x1b[<t>;<b>r` (`insert_history.rs:343-352`), used at
  `:173`/`:203`, always paired with `ResetScrollRegion` = `\x1b[r` (`:363-372`, `:178`/`:215`),
  with the cursor restored afterwards (`MoveTo(last_cursor_pos)`, `:154`/`:216`) and an explicit
  note that `MoveTo` is used instead of `set_cursor_position` to keep the terminal's
  tracked cursor position valid (`insert_history.rs:205-207`). The layout concept is a
  *viewport area* (`custom_terminal.rs:73`, set via `set_viewport_area`, `:305`).
- `live_wrap.rs` is the "when to commit" policy: `RowBuilder::drain_commit_ready(max_keep)`
  (`:94-108`) hands back the oldest rows once the live region exceeds `max_keep`, so
  streaming output is *promoted into scrollback* instead of being perpetually repainted;
  a row that ends exactly on a width boundary keeps an `explicit_break` empty row so
  fragmentation is invariant (`flush_current_line`, `:108-129`).
- History recall is a real subsystem: `bottom_pane/chat_composer_history.rs`
  `navigate_up`/`navigate_down` (`:404`/`:430`), Ctrl+R search (`:566`), 12 in-file tests.
- **Paste burst** is the same bug class gopi has: `PASTE_BURST_MIN_CHARS = 3` (`:157`),
  `PASTE_ENTER_SUPPRESS_WINDOW = 120 ms` (`:158`), `PASTE_BURST_CHAR_INTERVAL = 8 ms`
  (`:161`), and a documented API `direct_insert_newline_should_insert` (`:38`) that the
  Enter handler must consult — i.e. **a newline that arrived inside a paste must not
  send**. `insert_newline` is a first-class editor action in the keymap
  (`keymap/bindings.rs:254`).
- **Verification recipe (Rule 10 "渲染真值"):** codex asserts *rendered screen content*,
  not callbacks — `src/test_backend.rs:14` documents the wrapper and `:22`/`:34` build a
  `CrosstermBackend<vt100::Parser>`, re-exported to tests by the 4-line `tests/test_backend.rs:1-4`,
  and `tests/suite/vt100_history.rs:51`/`:98` assert
  `scenario.term.backend().vt100().screen().contents()` contains the inserted lines. No pty
  is needed. This is directly reusable for gopi:
  a vt100-style 80×24 emulator fed by our writer gives byte-truth for scrollback/redraw
  behaviour that a "did we call the callback" test cannot.

### 1.4 What is NOT borrowable

- hermes' and codex's *widget toolkits* (prompt_toolkit / ratatui+crossterm) — both are
  large third-party trees; the offline box cannot fetch them and Rule 10.7 prefers
  stdlib. Only the *decisions* transfer.
- dsh's carrier (JSON-RPC + Zod + React) — it presumes a build toolchain.
- hermes' `/api/pty` + xterm.js bridge — needs a JS dependency. Recorded as rejected
  (§3, Table B T3b) so it is not re-proposed.

---

## 2. Current-state gap analysis (gopi, master `0dac2ae7`)

### 2.1 `gopi chat` — the input path is the whole gap

`cmd/agent/cli_chat.go` = 598 lines. Verified anchors:

| concern | file:line | state |
|---|---|---|
| reader | `cli_chat.go:424` `reader := bufio.NewReader(os.Stdin)` | cooked mode |
| read loop | `cli_chat.go:437` `lineCh := make(chan lineRead, 1)` + `:440` `reader.ReadString('\n')` in a feeder goroutine | **one line = one turn** |
| prompt | `cli_chat.go:469` `fmt.Fprint(os.Stderr, "you> ")` | no editor behind it |
| signal path | `cli_chat.go:404` `sigCh := make(chan os.Signal, chatSignalBuf)` (buf const `:40`) | **present** — the old review `:54` (mid-session exit loses the session row's end) is FIXED on master (`session_end` is written on every exit path) |
| streaming renderer | `cli_chat.go:534` `renderer := newChatRenderer(os.Stdout)`; consts `chatWordFlushWindow = 30ms` (`:32`), `chatMaxBufferedBytes = 400` (`:36`) | word/line flush policy, good |
| turn | `cli_chat.go:550` `ag.ChatOps(...)` / `:552` `ag.Chat(...)` | single shared agent |
| history capture | `cli_chat.go:581` `sess.history = append([]deepseek.Message{}, ag.Ctx.Messages...)` | single-session consolidation — **must not break** |

What the user feels:

1. **No history.** Up arrow writes `^[[A` into the line buffer — the terminal does not
   own a history for a program reading cooked lines.
2. **No in-line editing.** `bufio.ReadString` hands back a completed line; left/right
   arrows arrive as literal escape bytes in that line, and there is no cursor model to
   apply them to.
3. **Long lines wrap ambiguously** — the tty wraps them visually with no knowledge of
   what gopi will do with the characters.
4. **Multiline is impossible, and worse than impossible — it is silently destructive.**
   The feeder (`cli_chat.go:437-455`) emits one `lineRead` per `\n`, and the loop
   `TrimSpace`s + `continue`s on empty input. Pasting a 10-line prompt therefore runs
   **10 separate model turns**, each with its own history append. There is no
   paste-burst window, no bracketed-paste handling, no "swallow the newline" guard
   (codex' `PASTE_ENTER_SUPPRESS_WINDOW`, §1.3).
5. **No typing while a turn runs** — the feeder blocks on a 1-buffer channel, so at most
   one line is held; anything else waits.

Repo-wide check: `grep -rn 'MakeRaw\|termios\|TCGETS\|isatty' --include='*.go' internal/ cmd/`
returns **zero hits**. The only TTY detection in the tree is
`cmd/agent/main.go`'s `isTerminal` (a `ModeCharDevice` heuristic). There is no raw-mode
code to extend — this is greenfield.

### 2.2 `gopi web` — a competent SSE UI with a known, enumerated gap list

`cmd/agent/cli_web.go` = 1384 lines. Routes: `/` (`:562`), `/api/chat` (`:563`, SSE),
`/api/transcript` (`:564`). Frames: `event: delta` (`:848`), `event: tool` (`:861`),
`event: done` (`:900`). The whole turn runs under `s.mu.Lock()` (`:783`), and
`finalizeChatSession` (`:936`, taking `s.mu` at `:944`) is the single-session closeout.
The page is one embedded const (`webChatPage`, `:1102`) with vanilla JS:
`addMsg` (`:1169`), `addCommand` (`:1181`), `stageFiles` (`:1197`), `renderStrip` (`:1211`),
`replayTranscript` (`:1239`), `addToolCard` (`:1255`), form submit (`:1274`),
SSE `frames()` parser (`:1314`), Enter/Shift+Enter (`:1377-1378`).

Gaps, each cross-checked against `devdoc/review-cli-chat-web.md`:

| # | gap | anchor | review ref |
|---|---|---|---|
| W1 | **No conversation reset.** Only two routes exist; `s.history` (server-owned since R1) lives for the server's lifetime, and a reload *replays* rather than clears. `gopi chat` has `/clear` (`cli_chat.go:504`); web has no equivalent. | `:562-564`, `:1050` | `:174` |
| W2 | **No per-turn timeout.** `Agent.Timeout` default 0 (unlimited) while a turn holds `s.mu`, so a hung model call blocks every later turn *and* the shutdown finalizer. | `:783`, `:944` | `:162` |
| W3 | **Queued turns are invisible.** The 200 + SSE headers are not written until the lock is acquired, so tab 2 hangs byte-less. dsh solves this with a typed `placement` (`events.ts:41`). | `:783` | `:180` |
| W4 | **Transcript replay loses tool activity.** `handleTranscript` ships only `Content`; tool args/results are not re-shipped, so a `--tools` refresh renders bare stubs. | `:1050` | `:193` |
| W5 | **No markdown.** Assistant text is appended with `bubble.textContent+=JSON.parse(data).text` — code fences render as literal backticks. | `:1336` (inside `handle('delta')`, `:1334`) | — |
| W6 | **Unconditional scroll jump.** Every delta does `logEl.scrollTop=logEl.scrollHeight`; a reader scrolled up is yanked back. dsh follows only when `isAtBottom` (`ChatView.tsx:271-275`, threshold 24). | `:1337` (delta), `:1354` (tool) | — |
| W7 | **Dead code + a budget mismatch.** `maxWebHistoryMessages = 200` (`:708`) is documentation-only; the client's `MAX_B64` (`:1167`) caps base64-*encoded* bytes while the server caps *decoded* 32 MiB (`webMaxChatBodyBytes`, `:1084`) — two different budgets under one "32 MiB" claim. | `:708`, `:1084`, `:1167` | `:186`, `:202` |
| W8 | **No composer ergonomics**: no autogrow, no `e.repeat` guard (dsh `InputBar.tsx:351`), no drag-drop/paste-burst discipline beyond the file input. | `:1153`, `:1377` | — |
| W9 | **Security posture (not a UX item, but it gates remote use):** loopback default (`webDefaultAddrValue`, `:423`) + token gate (`webGatedHandler`, `:543`), but `--tools` on a non-loopback bind is only *warned*. | `:423`, `:543` | `:151` |

### 2.3 The `golang.org/x/term` question (measured, not assumed)

The briefing asserted "go.mod = modernc.org/sqlite + x/term indirect only". **That is
false on this box:**

```
$ grep -c 'golang.org/x/term' go.sum go.mod     # → 0 and 0
$ ls ~/go/pkg/mod/golang.org/x/                     # arch, exp, image, mod, sync, sys, text, tools — NO term@*
$ GOPROXY=off go list -m golang.org/x/term          # → not a known dependency
```

Consequence: `x/term` cannot be added by an offline build. Adding it means either
network access (unavailable) or hand-vendoring a foreign tree (`go.sum` entries cannot
be synthesized offline either). **Any plan that begins "use x/term" is dead on arrival
here.** What *is* available, measured with a compiled probe:

```
$ cat probe.go  # syscall.Termios, syscall.TCGETS/TCSETS, syscall.ECHO|ICANON|ISIG, t.Cc[VMIN]/[VTIME]
$ GOPROXY=off go run probe.go     # → ok 0 0 0
```

`golang.org/x/sys` **is** already a direct dependency (`go.mod`: `golang.org/x/sys v0.37.0`,
present in the module cache) and is used in production code today
(`internal/daemonlifetime/binding.go:40`). `golang.org/x/sys/unix` gives the same
`Termios`/`IoctlGetTermios` surface as `x/term` with no new module.

### 2.4 Line-editor options, effort, recommendation

| option | availability on this box | effort | verdict |
|---|---|---|---|
| **`golang.org/x/term`** (`term.MakeRaw`/`Restore`, `GetSize`) | **unavailable** — not in go.mod/go.sum/module cache (§2.3) | n/a | **rejected**: cannot be fetched offline, cannot be vendored honestly |
| **Zero-dep termios editor** — `syscall.Termios` (+`TCGETS`/`TCSETS`) or `golang.org/x/sys/unix` (`x/sys` already a dep), behind a tiny `lineEditor` interface; Linux/Unix build tag with a cooked-mode fallback | **verified compilable and runnable** | **1.5–2.5 d** for the editor core (raw enter/restore via a *full* termios snapshot, rune-cursor buffer, insert/delete/word motions/kill+yank, history ring + `~/.gopi/chat_history`, multiline continuation, paste-burst guard, SIGWINCH redraw) + 1 d for tests | **RECOMMENDED** |
| **Vendored linenoise** (or any copied line-editor source tree) | not in the module cache; would mean committing third-party source into the repo | 2–4 d of integration + permanent maintenance/attribution surface | **rejected**: Rule 10.7 (dependency preflight, stdlib first), and the offline box can never update it |

**Recommendation.** Zero new dependency, `x/sys/unix` (already present) as the syscall
layer rather than raw `syscall`, because `x/sys` is the maintained, build-tagged surface
for exactly this — and it costs nothing since it is already in `go.mod` and already used
in `internal/daemonlifetime`. Follow hermes' **full-snapshot restore** discipline
(`cli.py:938-951`) rather than a bit mask, and follow codex's **paste-burst window**
(§1.3) rather than trying to detect pastes by timing alone.

**Non-negotiable fallback:** the editor must be entered *only* when stdin is a TTY and
must degrade to today's cooked `ReadString` path otherwise — the testsuite drives
`gopi chat` with piped stdin (`testsuite/chat_test.go`) and that path is load-bearing.

---

## 3. Effort tables

Scale anchor: `devdoc/gopi-chat-mode-effort.md` priced CLI chat (方案 A) at **2–4 days**
and an interactive TUI (方案 B) at **5–8 days**; the CLI chat itself has since shipped
(`devdoc/chat-mode-report.md`). Everything below is *new* work living in that same
scale — 方案 B's "terminal rendering layer ≈1–2 d" is refined into A1–A7.

### Table A — CLI workstream (line editing / history / multiline / scrollback-safe streaming)

| id | increment | effort | deliverable | regression test |
|---|---|---|---|---|
| **A1** | Raw-mode line editor core: enter/leave raw mode via a **full** termios snapshot; rune-cursor buffer (insert, backspace, delete, left/right, home/end, word motions, kill-to-EOL + yank); redraw on width change (SIGWINCH); non-TTY fallback untouched | **1.5–2.5 d** | `internal/lineedit` (or `cmd/agent/lineedit_*.go`) behind an interface; `cli_chat.go:424-455` becomes "ask the editor for a line" | vt100-screen test (codex `vt100_history.rs` recipe, §1.3): feed keystrokes, assert the rendered line; `-run TestLineEditor` |
| **A2** | History: up/down ring + persistence to a per-home history file (hermes `FileHistory` precedent, `cli.py:4127`), dedup-adjacent, bounded | **0.5 d** | history file + recall bindings | `-run TestLineEditorHistory` (recall order, persistence round-trip, bound respected) |
| **A3** | Multiline + paste guard: Alt+Enter/Shift+Enter inserts a newline; bracketed paste; a newline arriving inside a paste **never** submits (codex `PASTE_ENTER_SUPPRESS_WINDOW = 120ms`, `paste_burst.rs:157-161`); all constants named (Rule 1) | **1 d** | multiline turn input; kills the "pasted block = N turns" defect | `-run TestChatPasteBurst` — paste 3 lines in one burst, assert **1** model turn and a 3-line user message |
| **A4** | Trapdoor + pipe-path parity + degradation: TTY detection, `--no-editor`, cooked fallback, and re-verification that the piped testsuite path is byte-identical | **1 d** | guarded entry; documented fallback | `testsuite/chat_test.go` stays green; new `-run TestChatCookedFallback` |
| **A5** | Scrollback-safe streaming: stop fighting auto-scroll (hermes `refresh_interval=0` rationale, `cli.py:15133`); commit *finalized* lines to scrollback and keep only a live region for the in-flight answer (codex viewport + `drain_commit_ready`, `live_wrap.rs:94-108`); one-line status (model / iter / tokens — data already exists in `cli_chat.go`'s counters) | **1–1.5 d** | non-flickering output under a live editor | vt100 screen-content assertions + a no-flicker check (count repaints for a fixed delta stream) |
| **A6** | Interrupt & typed-ahead discipline: Ctrl+C at the prompt (discard the line) vs mid-turn (cancel the turn, keep the session) — reconciled with the existing `interruptCh` path (`cli_chat.go:404-419`); a line typed during a turn is preserved, not lost | **0.5–1 d** | two distinct interrupt semantics | `-run TestChatInterrupt` (prompt-interrupt does not end the session; mid-turn interrupt writes `session_end` once) |
| **A7** | Optional: `/` and file completion menu below the line, computed off the render loop (hermes `ThreadedCompleter` rationale, `cli.py:14323-14329`) | **1–1.5 d** | completion popup | `-run TestChatCompletion` against a stub completer |

**Table A total: 6.5–8.5 days**, in 5 shippable increments (A1+A4 first; A2, A3, A5, A6
each land independently; A7 optional last). Nothing here touches session consolidation
or the agent loop — the change is confined to *how a line is obtained*.

### Table B — Browser UI workstream, tiered

Tier 1 — quick wins (≤1 day each)

| id | item | effort | anchor / borrow |
|---|---|---|---|
| **B1** | **Conversation reset** — a `/new` (shared command table) + `POST /api/chat/reset` that clears `s.history` under `s.mu`; browser clears `#log` | **0.5 d** | gap W1; `review-cli-chat-web.md:174` [P1]; mirrors `cli_chat.go:504` |
| **B2** | **Busy/queued feedback** — write the 200 + SSE headers and a `queued` frame *before* acquiring `s.mu`, and render it | **0.5 d** | gap W3; dsh `placement: 'queued'` (`events.ts:41`) |
| **B3** | **Follow-bottom only when at the bottom** — a threshold constant (dsh `FOLLOW_THRESHOLD = 24`, `ChatView.tsx:25`) and a "jump to latest" affordance | **0.25–0.5 d** | gap W6 |
| **B4** | **Per-turn timeout** — `Agent.Timeout` set before `Chat`/`ChatOps` for web turns, env-overridable, read in exactly one place (Rule 2) | **0.5 d** | gap W2; `review-cli-chat-web.md:162` [P1] |
| **B5** | **Transcript fidelity** — ship tool `name`/`summary` (the `ToolEvent.Summary` the SSE path already has) in `/api/transcript`; render tool rows on replay | **0.5 d** | gap W4; review `:193` [P2] |
| **B6** | **Dead code + budget alignment** — delete `maxWebHistoryMessages`, align the client cap to *decoded* bytes so "32 MiB" means one thing | **0.25 d** | gap W7; review `:186`/`:202` [P2] |
| **B7** | **Composer guards** — autogrow, `e.repeat` guard (dsh `InputBar.tsx:351`), IME-safe Enter | **0.5 d** | gap W8 |
| **B8** | **Minimal markdown** — fenced code blocks + inline code only (no dependency: a small tokenizer, `textContent`-based like today's XSS-safe rendering) | **0.5–1 d** | gap W5 |

Tier 1 total: **3.5–4.5 d.**

Tier 2 — medium (2–5 days each)

| id | item | effort | borrow |
|---|---|---|---|
| **B9** | `/` + `@` **input-trigger menus** driven by one detect→menu model | **3–4 d** | dsh `ui-input-trigger/core/detect.ts` + `MenuView.tsx` |
| **B10** | **Session browser** — list chats from the chats table, resume by id (the CLI's `gopi chat <session-id>` seed path already exists) | **3–5 d** | gopi's own `resolveResumeLogPath`; hermes' dashboard-side session list |
| **B11** | **Tool-card depth** — args + result folding, per-tool render intents (bash/read/diff) | **3–4 d** | dsh host-computed `ToolEventView` (`events.ts:32`) + `packages/client/ui-tool/**/toolviews/` |
| **B12** | **Visible turn queue + steering** — type while a turn runs, see the queue, optionally steer | **4–5 d** | dsh `QueuedInboxItem.placement` (`events.ts:41`) + `submission-policy.ts:48`. Depends on B2 |
| **B13** | **Reasoning/thinking stream** in a collapsible dim block, frame-throttled | **2–3 d** | hermes `_stream_reasoning_delta` (`cli.py:5722-5726`); dsh `use-throttled-visual-update.ts:4`, `ReasoningRow.tsx:31` |

Tier 2 total: **15–21 d.**

Tier 3 — large (1–2 weeks each) — **planned, not scheduled in M1/M2**

| id | item | effort | note |
|---|---|---|---|
| **T3a** | Typed RPC carrier upgrade (rpcId echo, closed error-code union, media-type guard) replacing ad-hoc SSE frames | **1–2 w** | dsh `rpc.ts:131-140`, README.md:21. Only worth it once the frame set outgrows `delta`/`tool`/`done`/`command`/`error` |
| **T3b** | ~~PTY + xterm.js bridge~~ | — | **REJECTED**: needs a JS dependency and a bundler; the box is offline with no build tooling (Rule 10.7) |
| **T3c** | Multi-session server (per-session agents instead of one agent + `s.mu`) | **1–2 w** | **DEFERRED**: it is the opposite direction from the single-session consolidation (§5). Revisit only with an explicit product decision |

---

## 4. Sequenced roadmap (M1 / M2)

Each increment is independently shippable and revertible, names its acceptance check,
and **does not depend on a later one**. Ordering inside a milestone is preference, not
dependency.

### M1 — fix what the user is hitting now (≈3–5 days total)

1. **M1.1 — A1 + A4: raw-mode line editor core with cooked fallback.** *Acceptance:*
   in a real PTY, backspace/arrows edit the line and a long line wraps correctly; with
   piped stdin, `testsuite/chat_test.go` is unchanged and green; `-run TestLineEditor`
   asserts rendered content through a vt100 emulator.
   **Caveat to state honestly:** raw mode is Linux/Unix-only in this shape; the fallback
   path is what Windows/odd terminals get.
2. **M1.2 — A2: history.** *Acceptance:* up/down recall within and across sessions;
   history file is bounded and written atomically.
3. **M1.3 — A3: multiline + paste guard.** *Acceptance:* a 3-line paste is **one** turn
   with a 3-line user message (`-run TestChatPasteBurst`). This one closes a real defect,
   not a polish item.
4. **M1.4 — B1: web conversation reset.** *Acceptance:* after reset, turn N+1 has no
   knowledge of turn 1 (live-verify with the codeword trick used in
   `devdoc/gopi-web-report.md`), and `s.history` is empty under the lock.
   **SHIPPED** (branch `feat/web-m1-b1b2b3`, `devdoc/web-m1-b1b2b3.md`): the endpoint is
   `POST /api/chat/reset`, the browser affordance is the shared table's `/new` (its
   *effect* is per-carrier — the table's `Run` is a no-op ACK, since `CommandContext`
   cannot see `s.history`), and live verification is in that report.
5. **M1.5 — B2 + B3: queued feedback + scroll pinning.** *Acceptance:* a second tab
   POSTing during a long turn receives a `queued` frame promptly; a reader scrolled up
   is not yanked by streamed deltas.
   **SHIPPED, as two increments** (same branch/report): B2 writes the 200 + SSE headers
   and one `event: queued` frame before taking `s.mu` (`TryLock` → frame → `Lock`); B3
   gates the page's follow on `FOLLOW_THRESHOLD = 24`. Live: second POST's first byte at
   0.0004 s during a 26 s turn.
6. **M1.6 — B6 + B7: dead code, budget alignment, composer guards.** *Acceptance:*
   `go vet`-clean; a 25 MiB decoded image is accepted by both sides; held Enter sends
   once.

### M2 — depth (≈10–14 days total)

7. **M2.1 — A5: scrollback-safe streaming + status line.** *Acceptance:* no repaint storm
   for a streamed answer (bounded repaint count asserted), finalized lines are promoted
   out of the live region, status line shows model/iter/tokens.
8. **M2.2 — A7: slash completion menu** (off the render loop). *Acceptance:*
   `-run TestChatCompletion`; typing `/` opens a menu, Tab completes, Esc closes.
9. **M2.3 — B5 + B8: transcript fidelity + minimal markdown.** *Acceptance:* a `--tools`
   turn replays with tool rows after refresh; fenced code renders as a `<pre>` built from
   `textContent` (no `innerHTML`), verified by a test that feeds `<script>` in a delta.
10. **M2.4 — B9: input-trigger menus.** *Acceptance:* `/` lists commands from the shared
    table; `@` lists files; keyboard navigation; no send while a menu is open.
11. **M2.5 — B10: session browser.** *Acceptance:* a listed chat resumed from the browser
    continues the same session id (single-session invariant preserved — see §5).
12. **M2.6 — B12: visible turn queue + steering.** *Acceptance:* a message typed during a
    turn is visibly queued and delivered, not dropped.
13. **M2.7 — A6: interrupt semantics.** *Acceptance:* Ctrl+C at the prompt keeps the
    session; Ctrl+C mid-turn cancels the turn and writes exactly one `session_end`.

### M3 — explicitly deferred

T3a (typed carrier), T3c (multi-session). T3b is rejected. Not scheduled; revisit with a
product decision, not by drift.

---

## 5. Non-negotiable invariants for every increment

1. **Single-session consolidation stays.** One agent, one session id, one JSONL, one raw
   log per chat run, one `session_end` at exit — `devdoc/chat-single-session.md`,
   `devdoc/web-chat-single-session.md`; web parity per `devdoc/review-cli-chat-web.md`
   §1.1/§1.2. A line editor must not create a session per line and a web queue must not
   create a second agent. **T3c (multi-session) is the only item that would change this,
   which is why it is deferred rather than scheduled.**
2. **Concurrent-turn mutex discipline stays.** `s.mu` serializes web turns; nothing in
   Table B may hold it longer, acquire it twice, or bypass it. B1/B2/B4 all touch this
   and must say so in their own devdoc.
   **Clarification (B1/B2 shipped, `devdoc/web-m1-b1b2b3.md`):** B2's
   `TryLock()` → `queued` frame → `Lock()` is **not** a double acquisition — a failed
   `TryLock` acquires nothing, so the turn holds `s.mu` exactly once and for no longer
   than before; and B1's reset is O(1) work on the same mutex (a slice drop), strictly
   shorter than a turn's tenure.
3. **History ownership is unchanged.** Chat: `sess.history` captured from `ag.Ctx.Messages`
   (`cli_chat.go:581`). Web: server-owned `s.history`, captured in `handleChat`,
   replayed by `/api/transcript`. B5 adds fields to the *replay*, it does not move
   ownership back to the browser.
4. **Zero new dependencies.** No `x/term` (§2.3), no JS framework, no bundler. If an
   increment cannot be built from stdlib + `x/sys` + `modernc.org/sqlite`, it is not in
   this roadmap.
5. **Every increment ships a regression test**, and every bug-class fix ships a minimal
   repro: Rule 10.5. The gates on master (`internal/stylegate`, `internal/standards`
   Rules 16–21) apply — notably `test-sleep`, `test-setenv`, `test-skip` and the
   `%w`/panic/accessor rules — so tests must be added with that discipline, not around it.
6. **Rule 1 applies to the new code**: every paste window, follow threshold, history
   bound and repaint interval is a named constant (the references above are quoted with
   their values precisely so the constants can be named after them).
7. **Verification recipe to reuse (Rule 10.2):** for anything that draws, assert
   *rendered screen content* through a vt100-style emulator (codex `vt100_history.rs`
   recipe) or pixel-check with `text2img`+IoU; never assert "the callback fired".

---

## 6. What I did not verify (honest)

- I did not build or run hermes, dsh, or codex — the citations are readings of source
  files, not observations of those programs running. Where a comment states a rationale
  (e.g. hermes' `refresh_interval` note), I am quoting the source's claim, not measuring
  the behaviour.
- Effort numbers are estimates anchored on `devdoc/gopi-chat-mode-effort.md`'s scale and
  on the *shape* of the reference implementations, not on a spike. A1 in particular has a
  wide band (1.5–2.5 d) because the redraw/cursor-position work under a live stream is the
  part that can surprise (codex needed a whole `custom_terminal.rs` + `insert_history.rs`,
  1 170 lines, for it — but that is a full-screen TUI, and M1.1 deliberately does not
  attempt that).
- The vt100-emulator test recipe (codex' `test_backend.rs` wraps the Rust `vt100` crate)
  would need a **Go** vt100 emulator or a hand-rolled 80×24 cell model; I did not verify
  that such a component exists in the module cache. If it does not, M1.1's test tier is
  either a minimal cell-grid renderer written in-repo or a PTY-driven transcript diff —
  that choice is left to the implementing task and is called out as a risk.
- Windows/ConPTY behaviour is out of scope: the recommended editor is Unix-termios-based
  and the fallback is today's cooked path.

---

## 7. Follow-up implementation items

Filed as `task_submit` items (this branch: research only — no production code changed):

| task id | scope |
| --- | --- |
| `01a0a946-47b8-7f36-b799-d973078723ea` | CLI M1 — raw-mode line editor + history + multiline/paste guard (A1–A4) |
| `01a0a946-613b-79a2-8fea-c19049ea26a3` | Web M1/M2 — reset, queued-turn feedback, scroll pinning, per-turn timeout, transcript fidelity |
| `01a0a948-e837-7ec0-a5bc-3b5bbbb770c4` | M1 web — conversation reset + queued-turn feedback + scroll pinning (B1, B2, B3) |
| `01a0a948-e9b2-7ae2-bfab-b067e00a1d54` | M2 CLI — scrollback-safe streaming + status line + slash completion (A5, A7) |
| `01a0a948-eaf0-727c-b02f-0f5dadd365cc` | M2 web — input-trigger menus + transcript fidelity + minimal markdown + visible turn queue (B9, B5, B8, B12) |

Each carries its own acceptance check from §4 and the invariants from §5.

**On the two web rows overlapping** (Project Rule 26): `01a0a946-…1ea3` and the
`01a0a948-e8…` row both cover `gopi web` increments. That is deliberate, not a
filing error — the two B-row sets were filed in the same round from two reads of
Table B (one grouped as "M1/M2 web", one split per increment), and Rule 26 says a
duplicate dispatch costs one agent's time while a stalled repair leaves the defect
live. The executor judges overlap from this doc; the row that lands first should
have the other closed as a duplicate rather than re-implemented.
