<!-- doc-class: normative -->
# devdoc: `gopi web` LAN access (one step from another device, without opening the box)

Task: epic "gopi web should allow access from IP" (session `01a0a4c7`).
Branch: `feat/web-lan-access` (worktree `~/workspace/gopi-web-ip`).

## The ask, and what it collided with

The operator's complaint (verbatim, from the briefing attachment): `gopi web`
only listens on `127.0.0.1`, "but this … is a kind of inconvenient, as using
current IP like 192.0.2.6 is just one step away, why don't do it?"

Just one step away — that was true and false at the same time:

- **True** — the code already supported it: `--addr=0.0.0.0:8090` (or
  `GOPI_WEB_ADDR`) binds every interface, and the banner (fix `fb9de6b`) prints
  the reachable URLs. Nothing *blocks* a LAN bind.
- **False, and dangerous** — the step was blocked by THREE separate papercuts,
  and the destination was the real problem: `gopi web --tools` turns run the
  full gopi toolset **with no approval gate** (`cli_web.go` R2). Opening that
  to the LAN means every device on the network can run `bash` on this host.
  The loopback default was not an oversight; it was the security decision
  recorded in `webDefaultAddrValue`'s comment.

So "just do it" (make LAN the default, or just answer "you already can") was
not an acceptable outcome. The deliverable had to make the convenience REAL
while keeping remote tool execution behind a secret.

### The three papercuts (each one a lost round-trip)

1. `--addr :8097` (space-separated, the form **every other gopi subcommand**
   takes) was REJECTED: `use --addr=:PORT (single token)`. The only valid
   spelling was the joined one.
2. The banner for a wildcard bind printed `http://localhost:8097` plus the LAN
   IPs — fine, but nothing told the operator which one is reachable from the
   other machine, or that the bind was now network-exposed.
3. The `--tools` notice said "loopback only, local-shell trust" **while the
   server was listening on every interface** — an actively false security
   statement on the exact path that made it false.

## What was built

### 1. The step is now one step (`webParseFlags`)

Both spellings work: `--addr 0.0.0.0:8097` and `--addr=0.0.0.0:8097`. A
dangling `--addr` (no value) is a hard usage error — a silent fallback to the
loopback default would be worse than an error. Flag parsing moved out of
`runWeb` into a pure function so the CLI contract is testable without binding
a socket.

### 2. A non-loopback bind is never served bare (`webTokenGate`)

`webGatedHandler(addr, bare)` is the single decision point (called once by
`runWeb`):

| listen address | handler served | token |
|---|---|---|
| loopback (`127.0.0.1`, `[::1]`, `localhost`) | bare | none — posture unchanged |
| anything else (wildcard, LAN IP, unparseable) | `webTokenGate(token, bare)` | `GOPI_WEB_TOKEN` if pinned, else 16 random bytes (hex) |

- The predicate `addressIsLoopback` fails **closed**: an unparseable address is
  treated as reachable, because loopback cannot be promised for something we do
  not understand.
- Token comparison is `crypto/subtle.ConstantTimeCompare`.
- The token travels by **query param** (`?t=…`, the one-click printed URL),
  **cookie** (`gopi_web_token`, HttpOnly, set on the first token-bearing page
  load so the page's own `/api/chat` + `/api/transcript` fetches and later
  refreshes keep working), or **`Authorization: Bearer`** (curl/scripts).
- If minting fails, `runWeb` **refuses to start** — never serve an unguarded
  network-reachable agent.
- The loopback path configures NO token and passes straight through: the
  local-shell-trust posture the previous epic deliberately established is
  untouched (`TestWebGatedHandler_WiringIsLoadBearing` asserts both sides).

### 3. The banner tells the truth (`webPrintListening(addr, ln, token)`)

Every printed URL of a gated bind carries `?t=<token>` — the LAN URL the
operator copies opens the page as-is, no second step. Plus two loud notices:

```
gopi web: LAN-REACHABLE bind — every URL below carries the per-run access token; anyone with that URL can drive this agent
gopi web: tools ENABLED on a LAN-REACHABLE bind — remote tool execution is possible for anyone holding the token URL
```

The second one fires only for `--tools` + non-loopback, and it REPLACES the
old "loopback only, local-shell trust" sentence (which would now be a lie).

## Verification

Unit tests (all 10 in `cmd/agent/cli_web_lan_test.go`, plus signature/helper
updates in `cli_web_test.go` and `cli_web_p1_test.go`):

- `TestAddressIsLoopback` — 10 address forms incl. `:8090`, `0.0.0.0`, `[::]`,
  an unparseable host; the gate must fail closed.
- `TestWebParseFlags_LANBindForms` — both `--addr` spellings, `--tools`,
  `-h`, dangling `--addr` → error, no flags → the loopback default intact.
- `TestWebNewAccessToken` — 32 hex chars, decodes, two mints differ.
- `TestWebTokenGate_RedProof` — **the load-bearing test**: on the REAL route
  table, the bare mux answers an unauthenticated `GET /` with 200 (the
  control), the gated mux refuses the *same* request with 401, a token prefix
  is refused, and query/cookie/Bearer all reach the page. `/api/chat` is gated
  too. Loopback (empty token) stays 200.
- `TestWebGatedHandler_WiringIsLoadBearing` — the WIRING red proof: a
  non-loopback address must yield a token and a 401 on the bare request; the
  pinned `GOPI_WEB_TOKEN` is used verbatim; loopback yields no token.
- `TestWebPrintListening_LANBindCarriesToken` / `_LoopbackUnaffected` — no
  `open:` line of a gated bind may lack `?t=`; the loopback banner has no
  query string at all.
- `TestWebUsageDocumentsLANAccess`, `TestWebLANWarningsNameTheRisk` — the help
  documents the path (a feature nobody can find is not delivered) and the
  warnings name the risk.

Live proof against the real binary (see the task report for the exact
transcript): `ss -ltn` shows `*:8097` for the LAN bind and
`127.0.0.1:8090` for the default; `curl http://192.0.2.6:8097/` → 401,
`…/?t=<token>` → 200, `…/api/chat` unauthenticated → 401, Bearer → 200; with
the loopback default the LAN IP is refused entirely.

Repo-wide controls (so the pre-existing red is not mistaken for this change):
`go test ./cmd/agent/` as a whole FAILs on the daemon/cgroup suite
(`TestWorkerDaemon_*`, `TestMergerRestart_*`, `TestUserBusSeam_*`, … — 18
cases) and pristine master `807dda98` fails the same 18 plus 3 more, so the
branch's set is a strict subset (pre-existing, environment-bound).
`internal/standards` Rule 16 fails identically on pristine master
(`NEW=2` at `cli_voice_client.go:811` / `cmd/agent/main.go:965` + a STALE baseline at
`cmd/agent/main.go:954`); neither file is in this diff.

## Decisions worth remembering

- **Loopback default kept.** The operator asked for convenience, not for a
  network-reachable unauthenticated tool runner. The default is unchanged; the
  opt-in is one step and self-explaining.
- **Gate on the bind, not on a `--lan` flag.** Any non-loopback address is
  network-reachable *by definition* — tying the gate to the address (rather
  than to a flag the operator might not pass) means there is no spelling that
  exposes the server bare.
- **`GOPI_WEB_TOKEN` exists** so the URL survives a restart (a minted token
  changes every run). One env reader (`webPinnedToken`), Rule 2.
- **The cookie is `SameSite=Lax` on purpose**: a cross-site page cannot make
  the browser attach it to a POST, so the cookie that makes the page's own
  fetches work does not also hand a hostile web page a CSRF channel into
  `/api/chat`.
- The usage string grew to 12 format verbs; both the printer and the tests go
  through ONE builder (`helperWebUsage`) so `go vet`'s printf check and the
  help assertions can never drift.
