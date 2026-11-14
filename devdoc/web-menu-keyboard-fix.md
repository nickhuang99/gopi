<!-- doc-class: historical -->
# Task Report: `gopi web` slash-menu keyboard navigation — the keyup that undid every arrow

## Completed

- **Root cause fixed in `cmd/agent/cli_web.go`** (page JS inside the embedded
  `webChatPage` raw string, 56 insertions / 13 deletions):
  - `menuRefresh(resetSel)` no longer rebuilds the open menu with an
    unconditional `sel:0`. It decides from the query's IDENTITY: a refresh of the
    SAME `trigger`+`query` **preserves** the highlight, a refresh for a **new**
    query (or a caller that asks for a reset) starts at 0. The preserved index is
    **clamped** with `Math.min(menu.sel, got.candidates.length-1)` so a shorter
    list can never leave the highlight past the end.
  - The **wiring is split by event**: `input` calls `menuRefresh(true)` (a new
    query resets), `click` calls `menuRefresh()` (the caret did not change the
    query). The old `['click','keyup'].forEach(... menuRefresh)` pair — the line
    whose keyup half undid every arrow press — is gone. `keyup` now carries a
    guard: `if(!MENU_NAV_KEYS.has(e.key)) menuRefresh()`, so a pure-navigation key
    issues **no** `/api/menu` round-trip.
  - `menuClose()` clears `trigger`/`query` as well as the items, so a closed menu
    cannot look like "the same open menu" to the next refresh.
  - **Named constants (Rule 1b/13):** `KEY_ARROW_DOWN`, `KEY_ARROW_UP`,
    `KEY_ENTER`, `KEY_TAB`, `KEY_ESCAPE`, `MENU_MOVE_NEXT`, `MENU_MOVE_PREV` and
    the `MENU_NAV_KEYS` set. Every `e.key==='…'` comparison now reads a constant;
    each DOM key name occurs **exactly once** in the page script (its
    declaration), asserted by the new gate below.
- **New regression gate `cmd/agent/cli_web_menu_keyboard_test.go`** (two halves,
  because neither can see the defect alone):
  - `TestWebMenu_KeydownKeyupPairKeepsTheHighlight` drives the **shipped page**
    through `cli_web_page_harness.js` with a real **keydown+keyup pair** (the old
    B9 scenario drove `menuMove()` directly — the exact call the bug's second half
    undid).
  - `TestWebMenu_NavigationKeysAreNamedConstants` is the static half: one
    declaration per key name, the guarded keyup listener present, and the three
    defect shapes (`['click','keyup'].forEach`, `items:got.candidates,sel:0,`, a
    resetless `menuRefresh`) absent for good.
- **New page scenario `B9 arrow-nav`** in `cli_web_page_scenarios.js` (28 checks):
  ArrowDown advances through the keydown/keyup pair and is never undone; ArrowUp
  decrements; **pure arrow keys spend no `/api/menu` round-trip**; a caret click
  on the same query keeps the highlight; **Enter completes the HIGHLIGHTED row**
  (never sends); a **NEW query resets to 0**; Escape closes and leaves nothing
  behind; a preserved highlight is **clamped** when the same query comes back
  shorter.
- **Harness support:** `cli_web_page_harness.js` gained `--only <substring>`
  (one increment's gate can be invoked alone; an empty match exits 1, never a
  silent pass) and exports the new key constants/set.

## Not Done

- **Nothing from the epic is deferred.** The optional CDP DOM assertion was in
  fact run (below); it was not required for acceptance.
- The pre-existing B9 scenario's use of `menuMove()`/`menuRefresh()` (direct
  calls, no key events) was **left as it is** — it still passes and still covers
  the pure detect core; the new scenario adds the paired-event coverage rather
  than rewriting the old one.
- A cross-repo `Rule 21`-style scan for "page env-name constants with no reader"
  was not attempted — out of scope for this diff.

## Verification

- Build: `CGO_ENABLED=0 go build ./cmd/agent/` → OK; `go vet ./cmd/agent/` → OK.
- Tests (green): `go test ./cmd/agent/ -run 'TestWebMenu_|TestWebM2B' -count=1` → `ok`.
- Page harness (whole suite): `node cmd/agent/cli_web_page_harness.js` → `PASS: 123/123 checks`.
- Branch-scoped smoke over everything the diff can regress:
  `go test ./cmd/agent/ -run 'Web' -count=1` → `ok github.com/nick/gopi/cmd/agent 45.151s`.
- **Red-then-green (Go + page honest).** Because the new tests reference NEW Go
  identifiers, restoring master's `cli_web.go` would be a *build* failure rather
  than a red test; so the red run was made behavioural: master's page **plus only
  the new constant declarations** (extracted with `sed` from the fixed file, never
  retyped) was built into the worktree as its `cli_web.go`.
  - `go test ./cmd/agent/ -run 'TestWebMenu_'` → **FAIL** (both tests).
  - The scenario tally was **`FAIL: 21/28 checks`** — the seven failures are
    exactly the bug: `ArrowDown advanced the highlight … (got 0, want 1)`, `a
    second ArrowDown … (got 0, want 2)`, `ArrowUp walked … (got 0, want 1)`,
    `pure arrow keys issued NO /api/menu round-trip (got 4, want 1)`, `Enter
    completed the HIGHLIGHTED candidate … (got "/task ", want "/text2img ")`,
    `the narrower query is walkable (got 0, want 1)`, `the highlight reached the
    last row … (got 0, want 2)`.
  - Fixed page, same tests → **PASS: 28/28 checks**, `ok`.

## Evidence (self-filed)

- **Deterministic gates I ran, with the outputs I saw** (in
  `~/workspace/gopi-webmenu-fix`):
  - `go build` → `BUILD OK`; `go vet ./cmd/agent/` → `VET OK`.
  - `go test ./cmd/agent/ -run 'TestWebMenu_|TestWebM2B' -count=1` → `ok github.com/nick/gopi/cmd/agent 1.321s`.
  - `node cmd/agent/cli_web_page_harness.js` → `PASS: 123/123 checks`.
  - `node cli_web_page_harness.js --only 'B9 arrow-nav'` → `PASS: 28/28 checks`.
  - `go test ./cmd/agent/ -run 'Web' -count=1` → `ok github.com/nick/gopi/cmd/agent 45.151s`.
  - RED (master page + grafted constants): `go test -run 'TestWebMenu_'` → `--- FAIL:
    TestWebMenu_KeydownKeyupPairKeepsTheHighlight`, `--- FAIL:
    TestWebMenu_NavigationKeysAreNamedConstants`; harness `FAIL: 21/28 checks`
    with the seven quoted failures above.
- **Live CDP verification on HP (192.168.7.5) — the real browser, not the shim.**
  A throwaway `gopi web --addr 127.0.0.1:8095 --no-tools` instance (never the
  operator's 8090 server) plus headless Chrome on ports 9223/9224, driven by the
  proven zero-dep raw-WebSocket CDP client; `/api/menu` and `/api/chat` calls were
  counted by monkey-patching `window.fetch` inside the live page.
  - Fixed build, observed lines:
    - `A. menu opened for '/': {"rows":6,"selIdx":0,"selText":"code-search","api":{"menu":1,"chat":0}}`
    - `B. ArrowDown x1: {… "selIdx":1,"selText":"log" … "api":{"menu":1,…}}`
    - `C. ArrowDown x2: {… "selIdx":2,"selText":"memory" … "api":{"menu":1,…}}`
    - `D. ArrowUp x1: {… "selIdx":1,"selText":"log" … "api":{"menu":1,…}}` (no new fetch)
    - `E. typed a NEW character: {"rows":2,"selIdx":0,"selText":"task", … "api":{"menu":2,…}}`
    - `F. ArrowDown on the new query: {… "selIdx":1,"selText":"text2img" …}`
    - `G. Enter: {"rows":2,"selIdx":1,"hidden":true,"value":"/text2img ","api":{"menu":2,"chat":0}}`
    - `H/I. Escape: reopened with 6 rows, then {"hidden":true}`.
    - Script verdict: `LIVE-OK: arrows advance/skip-no-fetch, new query resets, Enter completes without sending, Escape closes`.
  - Master build (`gopi-menured`, same driver): `B. ArrowDown x1: {… "selIdx":0,
    "selText":"code-search" … "api":{"menu":2,…}}`, `C. … "selIdx":0 … "api":{"menu":3,…}`,
    `D. … "api":{"menu":4,…}` — the highlight frozen at 0 with one `/api/menu` per
    arrow press, i.e. the reported symptom reproduced on the live page, which
    proves the live instrument measures the defect rather than blessing the fix.
  - HP was left clean: both test servers and both headless Chrome profiles were
    killed and deleted (`left: []`); ports 8095/8096/9223/9224 all closed.
- **Commit**: the branch tip of `feat/fix-web-menu-keyboard`, subject
  `fix(web): the slash-menu keyup no longer undoes every arrow press` (5 files,
  +514/-16, worktree clean). The exact hash travels in the task's final message:
  a report cannot embed the hash of the commit that contains it.

## Notes for the next agent

- The scenario that could not see the bug (old B9) drove `menuMove()` directly —
  a **paired-event handler needs the pair**, or the very code you are testing is
  the half that survives.
- `pkill -f "<pattern>"` over ssh kills the ssh process that carries the pattern
  in its own cmdline (Rule 24's sibling trap). Probe with a connect/`pgrep -af |
  grep -v pgrep` instead.
- Rule 13's exhaustive-literal idea crosses the Go↔JS boundary here: "each DOM key
  name occurs exactly once in the page script" is a cheap, precise gate, and it is
  in the new test file.
