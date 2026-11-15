<!-- doc-class: normative -->
# Task Report: w3m as a gopi chat carrier — feasibility study

**Verdict: GO, with three measured constraints.** The terminal browser `w3m`
can drive a real chat round-trip against gopi's existing turn engine, and its
form field is a proper line editor (backspace + arrow keys + mid-line insert) —
which fixes the original complaint that drove this epic. The cost is that
`w3m` runs **no JavaScript**, so the existing browser UI page cannot be
reused as-is; a second, server-rendered page is required. That page is small,
because the expensive part (the turn engine) already exists.

Everything below was **run on this host**; commands and observed output are
quoted. Nothing is inferred.

---

## 1. Executive summary

| Question | Measured answer |
|---|---|
| Can w3m complete a chat round-trip? | **Yes** — form POST reached the server, server-rendered reply came back |
| Is the input field a real line editor? | **Yes** — backspace (`abcdef`→`abcxy`) and mid-line insert (`hello`+3×Left+`XY`→`heXYllo`) |
| CJK / emoji? | **Yes** — `中文测试 😀` round-tripped byte-exact |
| Can the turn engine be reused? | **Yes** — the existing `POST /api/chat` answered `PONG` headlessly |
| Token-by-token streaming? | **No** — no auto-refresh; the turn is atomic (POST → wait → full answer) |
| Can the existing web page be reused? | **No** — it is JS+SSE-driven; w3m has no JS engine |
| Multi-line input? | **No** — `<textarea>` Enter shells out to an external editor (nano/vim) |
| Is the field focused on load? | **No** — a `Tab` is mandatory before anything can be typed |
| New dependencies? | **Zero.** `/usr/bin/w3m` 0.5.3+git20210102 already installed |

**On the original hypothesis** ("we already have a web browser, borrow it"):
**confirmed with one correction.** The *turn engine* is borrowable and is the
expensive part; the *page* is not borrowable, because the value of the existing
page sits in the JavaScript the w3m cannot run. The integration is therefore
"write a second, dumb, server-rendered view over the same engine" — which is
still far cheaper than the raw-termios TUI alternative (5–8 d per
`devdoc/gopi-chat-mode-effort.md`).

---

## 2. Method (and how to reproduce)

Harness: a real TTY is required (`w3m` refuses to run without one; piping stdin
makes it exit immediately). Probes used `tmux` (installed, 3.2a) with a clean
environment, screenshots via `tmux capture-pane -p`:

```bash
tmux new-session -d -s w -x 100 -y 24 \
  "env -i HOME=/dev/shm/w3m-feas TERM=xterm-256color LANG=C.UTF-8 PATH=/usr/bin:/bin \
   w3m http://127.0.0.1:8791/#bottom"
tmux send-keys -t w Tab; tmux send-keys -t w Enter; tmux send-keys -t w -l 'hello'
tmux capture-pane -p -t w
```

A ~60-line throwaway Go probe server (JS-free page: transcript in a `<pre>`, one
form POST per turn, plus a `/_state` route proving what the server received) was
the counterpart. Scratch lived at `/dev/shm/w3m-feas` (`stat -c %d` → `28`,
distinct from `/` → `2051`, per Rule 22). No synthetic device load (Rule 27).

---

## 3. Measured compatibility matrix

| Capability | w3m | Evidence |
|---|---|---|
| Form POST round-trip | ✅ | server saw `msg=hello w3m chat`; page re-rendered with the reply |
| Backspace inside a field | ✅ | typed `abcdef`, 3×`BSpace`, typed `xy` → server got `abcxy` |
| Arrow-key cursor + mid-line insert | ✅ | typed `hello`, 3×`Left`, typed `XY` → server got `heXYllo` |
| UTF-8 / CJK / emoji | ✅ | `中文输入 + emoji 😀 test` echoed byte-exact |
| Long input (180 chars) | ✅ | 180 `L`s accepted and echoed |
| Long conversation (123-line page, 24-line screen) | ✅ | `#bottom` anchor opens pinned to the newest turn |
| Submit keeps the tail of a long page | ✅ | turn 46 appended from the tail; view stayed at the newest turn |
| Manual reload `R` | ✅ | re-fetched, re-rendered, landed at the newest turn (see caveat f) |
| Access-token gate (`?t=` + cookie) | ✅ | w3m accepts both |
| **JavaScript** | ❌ | not implemented — `webChatPage`'s JS/SSE is dead weight |
| **Auto-refresh (`<meta refresh>`)** | ❌ | counter stayed `tick=1` across 4 s of watching |
| **Token-by-token streaming** | ❌ | follows from the row above |
| **Auto-focus of a form field** | ❌ | no-Tab typing hit *global* keys (`o` opened the Option Setting Panel) |
| **`<textarea>` (multi-line input)** | ❌ | Enter on a textarea invoked an external editor (`Select an editor … nano/vim`) |
| **Draft survives reload** | ❌ | un-submitted draft was cleared by `R` |
| Inline images / file-upload attach | ❌ | no `<input type=file>` support path probed; needs a design |

---

## 4. The keystroke cost (measured, not estimated)

The field is **activated** with `Enter`, not focused on load. Accepted flows:

```
Form with an input AND a submit button:
  Tab  Enter  <type>  Enter  Tab  Enter      = 6 keys per turn
Form with a SINGLE input, no submit button:
  Tab  Enter  <type>  Enter                  = 4 keys per turn
```

The single-input variant is the recommended shape: w3m submits implicitly when
the form holds exactly one input. A field can never be typed into without the
leading `Tab, Enter` (verified twice).

---

## 5. The reusable seam (zero new engine, zero new deps)

`cmd/agent/cli_web.go` already owns everything the carrier needs. `runWeb`
builds the shared server (`srv := &webServer{…}`, `mux := srv.webChatHandler()`,
`webGatedHandler(addr, mux)`), and one turn runs as:

- `ag := s.agent` (the ONE persistent agent), turn serialised by `s.mu`
  (`TryLock` → `event: queued` frame → `Lock`),
- `ag.Chat(msg, seed, images...)` / `ag.ChatOps(...)`,
- live output via `ag.OnText` / `ag.OnToolEvents`,
- shutdown via `s.finalizeChatSession`, LAN exposure via `webGatedHandler`.

**Proof the engine is carrier-independent** — driven with no browser at all:

```
$ curl -sN -X POST -H 'Content-Type: application/json' \
    -d '{"message":"Reply with exactly: PONG"}' http://127.0.0.1:8799/api/chat
event: delta
data: {"text":"P"}          … {"text":"ONG"}
event: done
data: {"result":"PONG"}
```

So the w3m mode is a **second view over the existing engine**: add a
server-rendered route (e.g. `GET /cli` + `POST /cli/chat`) that renders the
transcript from server-owned history (the `handleTranscript` route is the
precedent), runs the same `ag.Chat`/`ChatOps` under the same `s.mu`
discipline, and answers with `303 → /cli#bottom`. `handleChat`'s
`TryLock`/queued-frame interlock must be mirrored, and the response must not be
written while holding the lock before the turn. A `gopi w3m` subcommand then
starts that server and `exec`s w3m at the printed URL (`os/exec` + `net/http`
— both already imported).

Note the current page is a single `<pre>`-free JS app: `webChatPage` is a
const HTML/JS blob returned verbatim by `handleRoot`, so the w3m view is a
*sibling* route, never a modification of it.

---

## 6. Honest gaps / constraints (do not read this as "no work")

1. **Streaming is gone.** The user waits for the whole answer. Tool activity can
   only be rendered as a post-turn block. This is the single biggest regression
   against the browser UI, and it is a property of w3m (no JS, no observed
   auto-refresh), not of our code — no amount of gopi-side work recovers it.
   A `--no-stream`/atomic-turn mode is inherent, not optional.
2. **single-line input only.** Copy-pasting a multi-line code prompt is
   effectively unsupported; `<textarea>` cannot be used. Long single-line input
   does work.
3. **Full-page re-render per turn.** No incremental DOM; each turn repaints the
   viewport (the `#bottom` pin handles the landing position, verified).
4. **Image attach is unprobed to completion.** The browser UI's ≤8 image
   attachments need a filename-based alternative (a text field the server
   reads) and an explicit trust decision; flagged, not designed.
5. **Presentation ceiling.** The output is terminal text; markdown/code
   highlighting is limited to whatever plain-text styling w3m renders. The
   "beautiful UI" goal is only partly served by this route.
6. **Effort estimate (day-level):** minimal viable w3m mode ≈ **1.5–3 d**
   (no-JS page + POST route + subcommand/exec wrapper + session finalize on
   exit + tests). Anchors: `devdoc/gopi-chat-mode-effort.md` (CLI REPL 2–4 d,
   TUI 5–8 d). This is a *feasibility estimate*, not a commitment.

---

## 7. Recommendation

**Proceed as a small, separate increment** — a `gopi w3m` carrier over the
existing engine, offered *alongside* `gopi web` and `gopi chat`, not as a
replacement. It is the cheapest route to "the agent in a terminal browser",
and it demonstrably fixes the line-editing complaint. It should be sold
honestly as **atomic turns, single-line input, terminal-text output**, because
those three limits are w3m's, not ours. If the streaming/agentic display
matters more than the carrier, the browser UI remains the better surface.

### Not done (honest)

- No implementation — this is the requested feasibility study (Rule 5 deliverable
  = this document).
- Image/file attach through w3m was not probed end-to-end.
- `meta_refresh` was tested only through the observed counter; the option's exact
  semantics in this build were not reverse-engineered from w3m's source.
- No A/B of `-o` option sets tuned for reading comfort (colour, columns) —
  cosmetic, deferred.

---

## Verification

- Probing host: `w3m 0.5.3+git20210102` (`/usr/bin/w3m`), `tmux 3.2a`,
  `LANG=C.UTF-8`, screen sizes 100×24 / 90×20 / 80×12.
- Carrier round-trips: 46 chat turns delivered and re-rendered; server-side
  `/_state` log corroborated every POST.
- Engine reuse: live `gopi web` server (`./bin/gopi web --addr 127.0.0.1:8799`),
  `POST /api/chat` → real model deltas → `event: done {"result":"PONG"}`.
- Doc-only change: no production code touched in this task.
