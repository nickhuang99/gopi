<!-- doc-class: historical -->
# Task Report: gopi web browser-access fix (http://:8090)

## Summary
`sgoopi web` bound correctly to all interfaces, but printed the banner line
`gopi web: listening on http://:8090` — an **empty hostname** no browser can
navigate to. The user pasted the printed URL into a browser and it failed
(`http://:8090` is unresolvable: curl → `000`), so the service looked "not
ready" even though it was actually reachable via `http://localhost:8090` and
`http://<lan-ip>:8090`.

## Root cause (5-why)
- **Symptom**: `gopi web` prints `listening on http://:8090`; browser cannot open it.
- **Why**: Go's all-interfaces address is `:8090` (empty host). The banner does
  `"listening on http://%s"` with that raw `addr` string → `http://:8090`.
- **Why**: `http://:8090` has an empty host — the HTTP URL grammar needs a host;
  browsers refuse to navigate there (curl returns `000`).
- **Why**: the code never resolved the empty host into a concrete reachable
  address (loopback / machine IPs).
- **Root cause**: `runWeb` printed the raw listen string instead of browser-
  navigable URLs. No IP/hostname surfaced ⇒ user had no clue what to type.

## Completed
- **`webPrintListening(addr, ln)`** — new banner printer. Splits the listen
  addr into host+port; if the host is concrete it prints that single URL
  verbatim; if the host is empty/all-interfaces it prints the loopback URL
  plus the machine's non-loopback IPv4 addresses (from `net.InterfaceAddrs`),
  so a browser on another LAN host can reach it.
- **`webDisplayURLs(host, port) []string`** — pure helper (unit-testable) that
  returns the browser-navigable URL list for a host+port.
- Help text (`webUsage`) and endpoints unchanged; server bind behavior unchanged.

## Not Done
- No HTTPS/CORS/authentication — out of scope (local tool, consistent with gopi's
  other modes).
- Only IPv4 non-loopback addresses are listed; IPv6 link-local URLs are not
  printed (rarely needed for a LAN browser tool; loopback + IPv4 covers the
  common case).

## Verification
- Build: `CGO_ENABLED=0 go build ./...` → OK; `go vet ./cmd/agent/` → clean.
- Tests: `go test ./cmd/agent/...` → PASS (existing suites + 4 new regression
  tests, no regressions). `go test -race ./cmd/agent/ -run 'TestWebDisplayURLs|TestWebPrintListening'` → PASS.
- New regression tests:
  - `TestWebDisplayURLs_ExplicitHost` — concrete host printed verbatim.
  - `TestWebDisplayURLs_EmptyHost` — empty/0.0.0.0/:: never emit `http://:PORT`;
    always include `http://localhost:PORT` + the machine's LAN IPs.
  - `TestWebPrintListening_EmptyHostPrintsReachableURLs` — banner prints
    `open:` lines with loopback + real LAN IPs (never `http://:`).
  - `TestWebPrintListening_ExplicitHostPrintsVerbatum` — `--addr=127.0.0.1:PORT`
    prints that exact URL only.
- Live end-to-end (built binary, `gopi web` default): banner printed
  ```
  gopi web: listening (Ctrl+C to stop)
    open: http://localhost:8090
    open: http://10.0.0.6:8090
    open: http://10.0.0.11:8090
  ```
  All three printed URLs verified reachable via curl (`200` each); `GET /api/chat`
  → `405` method guard intact; the broken `http://:8090` form no longer appears.

## Commit
- Branch: `feat/gopi-web-url-fix`
- Commit: `fb9de6b` fix(web): print reachable URLs, not broken http://:8090
- Worktree: `~/workspace/gopi-web-fix` (shares the main repo's refs — the commit
  is already in the main repo).

## Branch & Recovery
- **Branch**: `feat/gopi-web-url-fix`
- **Recovery**: `git show feat/gopi-web-url-fix:devdoc/gopi-web-url-fix-report.md`
