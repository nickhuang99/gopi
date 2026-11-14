<!-- doc-class: normative -->
# Task Report: `gopi web` — same-origin fetches now carry the per-run `?t=` token

Branch: `fix/web-query-token-selfheal` · Commit: see `## Verification`

**Rework note (task `01a0b5f5`).** The branch was recycled by a merge-run whose
recorded base was stale (`master 52090dc0`, an ancestor of the branch's real base
`a9a96cd7` — 325 commits behind the tip), so its "content conflict on
`cli_web_page_harness.js`" described a 771-file phantom diff; the real overlap was one
hunk. The payload was re-applied onto master `9e6d7844` as `e1160865` (fix) +
`cbd628e0` (docs) — same five files, same content — and the merge is now clean. The
original ref had already been pruned; the pre-prune branch diff attached to the rework
task is the durable copy of the payload.

## The defect

`gopi web` mints a fresh access token per run (`webTokenQueryKey = "t"`,
`webAccessCookieName = "gopi_web_token"`). The gate (`webTokenGate`) reads a
credential in the order query → cookie → bearer (`webRequestToken`) and, on the
FIRST authorized request, stored it in a cookie so the page's later fetches kept
working — but only when the jar held no cookie at all (`if _, err :=
r.Cookie(...); err != nil`).

The page's same-origin API fetches were bare relative URLs:

| site | before |
|---|---|
| `menuCandidatesFor` | `fetch(API_MENU+'?'+…)` |
| `replayTranscript` | `fetch('/api/transcript')` |
| `runTurn` (the chat POST) | `fetch('/api/chat',{method:'POST',…})` |

With no `?t=` in the URL, such a fetch authenticates through the COOKIE ALONE.
A browser that holds the cookie of an EARLIER run presents a stale token →
`subtle.ConstantTimeCompare` fails → every send 401s ("still unauthorized from
this peer"), while the page load — which carried the fresh `?t=` the banner
printed — succeeded. So the page looked freshly loaded and simultaneously could
not send.

Two further gaps made that state undiagnosable and sticky:

- the gate **logged nothing** on a refusal: the 401 existed only on the wire;
- a stale cookie was never replaced, so accepting a fresh `?t=` did not repair
  the page — the NEXT cookie-only fetch 401'd again.

## Completed

1. **`cmd/agent/cli_web.go` — page JS carries the token on every fetch.**
   Page-side constants (Rule 1b — the page is a Go raw string and cannot import
   Go constants): `TOKEN_ARG`, `QUERY_SEP`, `API_CHAT`, `API_TRANSCRIPT`
   (`API_MENU` already existed). New helpers:

   ```js
   function locationToken(){ … new URLSearchParams((window.location&&window.location.search)||'').get(TOKEN_ARG) … }
   function withToken(url){ const t=locationToken(); if(!t)return url; return url+((url.indexOf('?')<0)?'?':QUERY_SEP)+TOKEN_ARG+'='+encodeURIComponent(t); }
   ```

   `locationToken` re-reads `window.location.search` on EVERY call, so a fresh
   `?t=` pasted into the same tab self-heals without a reload. All three fetch
   sites now route through `withToken(...)`.

2. **`cmd/agent/cli_web.go` — named constants for every value both sides spell.**
   `webChatPath` (`"/api/chat"`), `webTranscriptPath` (`"/api/transcript"`),
   `webTokenQuerySep` (`"&"` — the page's `QUERY_SEP`), `webTokenPair(token)`.
   The mux registrations, the banner URL builder and the tests all reference
   these; the two literal paths that were typed into the mux are gone (Rule 1b).

3. **Self-heal, server side.** The cookie is (re)set whenever the presented
   cookie is missing **or differs from the run token**:

   ```go
   if c, err := r.Cookie(webAccessCookieName); err != nil || c.Value != token { … http.SetCookie(…) }
   ```

   Authorization still comes from the carrier `webRequestToken` already picked
   (query first), so a fresh `?t=` beside a stale cookie is accepted AND the jar
   is repaired — the follow-up cookie-only fetch then succeeds.

4. **Refusal is recorded (Rule 15).** `logWebTokenRefusal` prints one fixed-shape
   line on stderr, built from constants (`webRefusalLogPrefix`,
   `webRefusalLogFormat`, `webRedactedTokenMarker`):

   ```
   [gopi web] token gate: refused 2026-09-17T…Z peer=192.0.2.6:54321 POST /api/chat query=absent cookie=present reason=wrong cookie token (the request carried no ?t= — the page did not append the access token)
   ```

   Presence only — never token material — and the `reason` distinguishes the four
   carrier states, including the reported one ("query token and cookie DISAGREE").

5. **Regression tests — `cmd/agent/cli_web_webtoken_test.go`** (13 tests) plus
   `cmd/agent/cli_web_page_url.js` and a `window.location` shim added to
   `cmd/agent/cli_web_page_harness.js`. They drive the REAL handler over a REAL
   listener (`net.Listen("tcp","127.0.0.1:0")`, kernel-picked port) with
   `localTrust=false`, and the chat URL is built by the **shipped page's own JS**
   executed under node — never re-implemented in Go, because a Go copy would
   prove nothing about what the browser sends.

## Not Done (honest)

- **No real-browser end-to-end run.** The page URL is built by the page's own JS
  under node (the harness the repo already uses for page scenarios), not by a
  headless Chromium session. `gopi shot --url` renders but does not POST a turn,
  so a full browser-cookie-jar run was not automated here. What IS covered: the
  exact URL the page builds, over a real listener, with the real handler.
- **No manual "stale cookie in a live browser" reproduction.** The stale-jar
  state is reproduced at the HTTP layer (raw `Cookie` header), which is the same
  wire input a browser jar produces.
- **Pre-existing, unrelated failures** remain on this branch's base and are
  unchanged by the diff (see Verification): `internal/stylegate`'s `Errorf`-verb
  ceiling (8 > 7) and two `internal/standards` gates (`Gate2`, `DataReport`).
  They are not caused by, and not fixed by, this task.

## Verification

- Build: `go build ./...` → OK (exit 0).
- Focused tests: `go test ./cmd/agent/ -run 'WebToken|WebPageToken|ChatAuth' -count=1`
  → `ok github.com/nick/gopi/cmd/agent` (16 tests PASS, 0 FAIL).
- Red-then-green: with master's `cli_web.go` restored (and a shim test supplying
  the new Go identifiers so the package compiles), the SAME test run gives
  **8 FAIL / 8 PASS**:
  FAIL — `TestWebPageToken_PageLoadThenSendCarriesTheToken`,
  `…_TranscriptAndMenuAlsoCarryTheToken`, `…_TokenlessPageSendsABareURL`,
  `TestWebTokenGate_SelfHealsFromAStaleCookie`, `…_RefusalIsLogged`,
  `…_RefusalLineNamesBothCarrierStates`, `…_EveryFetchUsesTheTokenBuilder`,
  `…_PageCarriesTheServerConstants`.
  PASS — the posture tests (`…_StaleCookieAloneIsStillRefused`,
  `…_WrongQueryTokenIsStillRefused`, `…_NoCredentialRemoteIsRefused`,
  `…_LoopbackDefaultUnchanged`) and the two pre-existing `TestWebTokenURL*`, i.e.
  the tests that must be red ARE red, and the ones asserting preserved posture
  are green on both sides.
- Commit: `d9e2f0fe fix(web): page fetches append the per-run ?t= token; gate logs refusals and self-heals a stale cookie`.

## Evidence (self-filed)

- `go build ./...` → `BUILD OK` (exit 0), run from `/home/user/workspace/gopi-webtoken`.
- Green: `go test ./cmd/agent/ -run 'WebToken|WebPageToken|ChatAuth' -count=1`
  → `ok  	github.com/nick/gopi/cmd/agent	0.899s`; `-v` shows all 16
  `--- PASS` lines, including `--- PASS: TestWebTokenGate_SelfHealsFromAStaleCookie`,
  `--- PASS: TestWebTokenGate_RefusalIsLogged`,
  `--- PASS: TestWebPageToken_PageLoadThenSendCarriesTheToken`.
- Red proof (same command, master `cmd/agent/cli_web.go`, shim file
  `cmd/agent/zz_redshim_test.go` declaring only
  `webChatPath`/`webTranscriptPath`/`webTokenQuerySep`/`webRefusalLogPrefix`/`webRedactedTokenMarker`/`webTokenPair`
  so the test file compiles): `FAIL github.com/nick/gopi/cmd/agent 0.950s` with
  the 9 `--- FAIL` lines quoted above. The shim was deleted and the fixed file
  restored (`cp` from `~/.gopi/sessions/evidence/red-kept-cli_web.go`) before the
  commit; `git status --porcelain` is empty afterwards.
- Posture preserved: `TestWebTokenGate_NoCredentialRemoteIsRefused` asserts 401
  for `GET /`, `GET /api/transcript`, `GET /api/menu`, `POST /api/chat` with no
  credential; `TestWebTokenGate_LoopbackDefaultUnchanged` asserts 200 on `GET /`
  and NO `Set-Cookie` when no token is configured.
- Self-heal: `TestWebTokenGate_SelfHealsFromAStaleCookie` asserts 200 for
  `GET /?t=<run token>` + `Cookie: gopi_web_token=<stale>`, a `Set-Cookie`
  carrying THIS run's token with `HttpOnly`, and 200 for the follow-up
  cookie-only `GET /`.
- Log line: `TestWebTokenGate_RefusalIsLogged` captures stderr around a real
  cookie-only `POST /api/chat` and asserts it contains `webRefusalLogPrefix`,
  `POST`, `webChatPath`, `cookie=present`, `query=absent`, the peer `127.0.0.1`,
  exactly ONE line, and NEITHER token value.
- No device load (Rule 27): every test is HTTP over loopback + in-process
  handlers; the node harness only parses and evaluates the page script.
