<!-- doc-class: historical -->
# Task Report: "gopi chat via w3m" — how much is done, is it usable, what blocks it

**Verdict: 0% implemented (nothing usable), 100% designed.** There is no `gopi w3m`
carrier, no `/cli` route, no w3m code of any kind in the tree. What exists is a
*feasibility study* whose GO verdict is trustworthy for the **engine** and for a
**throwaway JS-free page** — but whose central assumption ("the existing page can be
borrowed") is now **measured false**: the shipped page cannot even be typed into, let
alone submitted, under w3m. The blocker is a **unbuilt artifact, not a technical wall** —
and the study undersold how much of the page has to be rewritten rather than reused.

Everything below was run on this host (2026-09-18); commands and observed output are quoted.

---

## 1. What exists today (audit)

| Question | Measured answer | Evidence |
|---|---|---|
| Feasibility study? | **Yes** — `devdoc/w3m-cli-feasibility.md`, landed `69a4fed4` (2026-09-16) | `git log -1 -- devdoc/w3m-cli-feasibility.md` |
| Its verdict | GO with 3 constraints (no JS ⇒ no streaming; no auto-focus ⇒ `Tab`+`Enter`; `<textarea>` unusable ⇒ single-line) | doc §1 table |
| Any w3m code? | **None** — 0 hits in every `.go` file | `git grep -il w3m -- '*.go' \| wc -l` → `0` |
| A `gopi w3m` verb? | **No** — `cli_verbs.go` has no w3m constant/verb | `grep -n w3m cmd/agent/cli_verbs.go` → empty |
| A no-JS route? | **No** — 404 | `curl /cli` → `404`, `/cli/chat` → `404` |
| Web routes that do exist | `/` via `handleRoot`, `/api/chat` (SSE), `/api/transcript` (JSON), reset, menu | `cli_web.go:714-718` |
| Ledger rows | `01a0a4f2` cancelled; `01a0a981` completed **as the study** (doc = its Rule-5 deliverable); **no implementation row was ever created** | `gopi task list --all \| grep -i w3m` |

So: the idea was studied, judged GO, documented — and then **nothing was ever
dispatched to build it**. The row that closed (`01a0a981`) closed with a document, which
is exactly what it was asked for; no follow-up row carries the build.

## 2. Is it usable? — the empirical answer, on the REAL page

The study's own probe used a ~60-line throwaway server with a JS-free `<pre>` transcript
and one `<form>` POST. **That was never the shipped page.** Driving w3m at the *real*
`gopi web` (built from this worktree, `gopi web --addr 127.0.0.1:8857`) gives:

```
$ w3m -dump -cols 100 http://127.0.0.1:8857/
gopi web
pure-Go browser chat · SSE streaming
↓ jump to latest
📎[                    ] Send
[                    ]
```

That is the **entire** rendered page (116 bytes, 6 lines; the served HTML is 41 611
bytes). The transcript — `<div id="log">`, populated only by
`document.getElementById('log')` in the inline JS — renders as **nothing**. The page
still shows one empty input **box**, so w3m has something to point at, but:

| Probe (real page) | Observed |
|---|---|
| `Tab`, `Tab`×4 | cursor never moves; status stays `Viewing <gopi web chat>` — no focus change |
| `Tab` `Enter`, then typing `text` | the text goes **nowhere** — the screen is byte-identical to before typing |
| `Tab` `Enter` (send) | **no submission** — still `Viewing <gopi web chat>`, no navigation |
| `Enter` at load | w3m **exits** (jump-to-latest is a JS `click` handler, so no link exists) |
| `/cli`, `/cli/chat` | `404 text/plain` |

### Why it cannot be typed into (the mechanism, not an impression)

The served HTML's interactive widgets carry **no `name` attribute and no `action`**:

```html
<form id="form">                                          <!-- no action, no method -->
<textarea id="input" rows="1" placeholder="…">            <!-- no name -->
<input id="file"  type="file" accept="image/*" multiple hidden/>
<button id="jump"  type="button">  <button id="clip" type="button">  <button id="send" type="submit">
```

No `<a href>` exists on the page at all. w3m builds its form/field model from **names and
`action`s**; an unnamed control is not a submittable datum and a form with no `action`
has nothing for w3m to submit *to*. This is the precise reason the study's
`Tab Enter <type> Enter` key recipe (its §4, measured on its own probe form) **does not
transfer to the real page**: that probe's form was correctly named and actioned, the
shipped one is not.

**Conclusion: the shipped web UI is unusable under w3m — not degraded, unusable.** Not a
single round-trip is possible. (Consistent with the study's own honest caveat that the
page is not reusable — but stronger than it: the study's "third, dumb, server-rendered
view" is not "cheap because the page is reused", it is a *second page written from
scratch*, with the *engine* — the expensive part — being the only real reuse.)

## 3. What stops development (ranked, each with its evidence)

1. **Nobody dispatched the build.** The study closed as a document (correctly — a study
   is a doc), and no row, tag or backlog item carries "build the `/cli` route + `gopi w3m`
   verb". Not a technical blocker, and the cheapest one to clear.
2. **The page must be rewritten, not reused.** The value of `webChatPage` (41 KB of inline
   JS + SSE: streamed deltas, `#log` rendering, reset, menu, `/api/transcript` replay) is
   *entirely* in the JS w3m cannot run. A w3m view needs a **new server-rendered route**
   with `name`d controls and an `action` (the study's §5 sketch: `GET /cli` + `POST /cli/chat`
   → `303 → /cli#bottom`, mirroring `handleChat`'s `TryLock`/queued-frame discipline). The
   study's "1.5–3 d" is a *feasibility* estimate that this audit reads as optimistic on
   the page half and right on the engine half.
3. **Loss of streaming, permanently.** Atomic turns only (POST → wait → whole answer).
   A property of w3m (no JS; the study measured `<meta refresh>` NOT working), not of our
   code — no gopi-side work recovers it. Tool activity can only be a post-turn block.
4. **Single-line input only.** `<textarea>` is unusable (Enter shells out to nano/vim);
   a one-line `<input name=…>` is the only workable field. Multi-line prompt paste is out.
5. **No in-place keystroke cost is free.** With a correctly-named single-input form the
   study measured `Tab Enter <type> Enter` = 4 keys/turn. The real page cannot even reach
   that, because its field is unnamed (see §2).
6. **Image attach undesigned.** The browser path is `type=file` (hidden input + `#clip`
   button + `#imgstrip`) — all JS; w3m needs a filename text field plus an explicit trust
   decision. Flagged by the study, still unprobed.

**Not a blocker:** dependencies (w3m 0.5.3 already installed; `gopi web` needs nothing
new — `os/exec` + `net/http` are already imported), or the engine (proven
carrier-independent: the study drove a real `POST /api/chat` → `event: done {"result":"PONG"}`
with no browser at all).

## 4. Recommendation

The idea is **salvageable and cheap — but it is not "borrow the page", it is "write a
second, dumb page over the same engine"**. Two things are worth doing, in this order:

1. **Fix the real page's w3m-affordances first, as a 1-line-ish precondition** — give the
   form an `action`/`method` and the field a `name` (a no-JS server POST path), so the
   real page fails *informatively* instead of silently. This is the smallest sample that
   proves the integration end to end.
2. Then the carrier: `GET /cli` + `POST /cli/chat` (server-rendered transcript from
   server-owned history, `#bottom` pin) + a `gopi w3m` verb that starts the server and
   `exec`s w3m at the printed URL. Sell it honestly as **atomic turns, single-line input,
   terminal text**.

If streaming/agentic display matters more than the carrier, the browser UI stays the
better surface — that trade is the operator's, and it should be made explicitly rather
than by default.

## Not Done (honest)

- No implementation — this is a status audit, not a build. Nothing was added to the
  product code path (`git status --porcelain` in the worktree shows only this report).
- The image/file attach path through w3m was **not** probed end-to-end.
- The `action`/`name` fix in §4.1 was **not** written — deliberately: it is product code
  for a build task, and sequencing it is the operator's call.
- The 4-keys-per-turn recipe was **not** re-measured on the real page (impossible: the
  field cannot be focused) — it is quoted from the study, measured on its own probe form.

## Verification

- Build: `CGO_ENABLED=0 go build -o /dev/shm/gopi-w3m-probe ./cmd/agent/` → `BUILD_OK`
  (`-rwxrwxr-x 23762333 …/gopi-w3m-probe`).
- Live server: `…/gopi-w3m-probe web --addr 127.0.0.1:8857` → `gopi web: listening … open: http://127.0.0.1:8857`.
- Real-page render: `w3m -dump -cols 100 http://127.0.0.1:8857/` → 6 lines / 116 bytes
  (served HTML 41 611 bytes; `curl -w '%{size_download}'`).
- Interactive probes: `tmux` session + `capture-pane -p` — Tab×4 (no focus change),
  `Tab Enter` + `-l 'text'` (screen byte-identical), `Enter` at load (w3m exits).
- Route probes: `curl /cli` → `404`, `/cli/chat` → `404`, `/api/transcript` → `200 application/json`.
- Code audit: `git grep -il w3m -- '*.go' | wc -l` → `0`; `grep -n w3m cmd/agent/cli_verbs.go` → empty.
- Scratch: `/dev/shm/gopi-w3m-run` (`stat -c %d` → `28` ≠ `/` → `2051`, Rule 22). No
  synthetic device load (Rule 27).
