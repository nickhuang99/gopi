<!-- doc-class: historical -->
# Task Report: `gopi proxy` announces readiness only after the socket is bound

## Completed

- **`cmd/agent/cli_proxy.go` — the banner is now post-bind by construction.**
  The startup path had two mutually exclusive outcomes behind ONE unconditional
  banner. It now has exactly one emission site for the readiness claim,
  `proxyServe(ln, addr, mode, up, …)`, which **receives an already-bound
  `net.Listener`** — the function has no way to print "listening on" earlier
  than the bind, because the only address it can print comes from a listener
  that `net.Listen` already returned. `runProxySubcommand` binds first and
  routes the error before any claim:

  ```go
  ln, err := net.Listen("tcp", opts.listen)
  if err != nil { fmt.Fprintf(os.Stderr, "%s%v\n", proxyBannerPrefix, err); os.Exit(1) }
  err = proxyServe(ln, opts.listen, opts.upstream, up, opts.verbose, logger)
  ```

  Replacing `s := &http.Server{Addr: …}; s.ListenAndServe()` with
  `net.Listen` + `httpSrv.Serve(ln)` is what removes the false-positive
  ordering: `ListenAndServe` performed the bind *behind* the banner, which is
  the exact reason the claim could precede the fact.
- **The banner names the socket the kernel actually bound.** `proxyBannerAddr`
  reports the listener's own address, so `--listen 127.0.0.1:0` prints the
  chosen port instead of the literal `0` the operator typed; a wildcard bind is
  spelled `[::]`, the way `ss -ltn` spells it, so the printed line can be
  matched against the kernel table line by line.
- **Rule 1/13/14: constants referenced, no bare literals.** The banner
  vocabulary lives in one `const` block (`proxyBannerPrefix`,
  `proxyListeningPrefix`), consumed by the production emission site and by the
  regression guard — the test branches on `proxyListeningPrefix`, not on a typed
  copy of the phrase. `proxyWildcardHost`, `proxyProbeTimeout`,
  `proxyProbeExitRefused`, `proxyProbeLoopback`, `proxyProbePortRe`,
  `proxyProbePollInterval` are the other named values; the test's argv is built
  by `proxyProbeArgs`, so there is no second hand-typed flag set.
- **`cmd/agent/cli_proxy_test.go` — two regression guards**
  (the pre-existing parse tests kept, untouched):
  - `TestProxyBusyPortNeverClaimsListening` — holds a port with an ordinary
    listener, runs the real binary against it, and asserts the refusal prints
    the bind error, **zero** "listening on" lines, and no `listening` in any
    other spelling/case.
  - `TestProxyBannerNamesBoundSocket` — binds `:0`, reads the port back out of
    the banner, and POSTs a real Anthropic request to it, so the sentence is
    proven true whenever it is printed.

## Not Done

- **No pre-bind "binding …" line was added, deliberately.** The epic allowed one
  ("the pre-bind line says 'binding …' if anything"). The honest pre-bind line
  would have to name an address before the bind, which is the very class of
  unfounded claim being removed; a version keyed on "the bind is taking longer
  than N ms" would need a timer that fires *during* a blocking `net.Listen` and
  was measured as unreachable in practice (binds here return in microseconds, so
  the line would never print — dead code carrying a promise). The resulting
  surface is smaller and strictly truthful: on the success path the operator
  gets exactly one line, and it is true. If a slow-bind progress line is ever
  wanted, it needs an event loop around the bind, which is a larger change than
  this fix.
- **`TestG5ProxyRunsAsAdvertised` was not modified.** It asserts the banner
  appears and the port accepts a connection; both still hold (it passes). It is
  not a busy-port case, so it did not need the new guard.
- **`internal/stylegate` is red on master, not from this diff.** Nine
  `errorf-verb` production hits (ceiling 7) in `cli_uistate_compare.go` /
  `memory_procedural.go` and five unexplained `t.Skip` sites, all in files this
  branch does not touch. This branch's own stylegate hits are zero.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `build: OK`
  (`go build ./...` clean; `go vet ./cmd/agent/` clean).
- **Tests** (Rule 27: branch-scoped smoke, not the whole tree):
  - `go test ./cmd/agent/ -run 'TestProxyBusyPortNeverClaimsListening|TestProxyBannerNamesBoundSocket' -count=1 -v`
    → `--- PASS: TestProxyBusyPortNeverClaimsListening (4.04s)`,
    `--- PASS: TestProxyBannerNamesBoundSocket (0.04s)`, `ok … 6.909s`.
  - Neighbours, unbroken: `go test ./cmd/agent/ -run 'TestG5Proxy|TestParseProxyArgs|TestProxy' -count=1`
    → `ok github.com/nick/gopi/cmd/agent 5.871s`.
  - `go test ./internal/standards/ -count=1` → `ok … 62.204s` (Rule 16–21 gates).
  - `go test ./internal/stylegate/ -count=1` → FAIL: 9 pre-existing
    `errorf-verb` hits + 5 pre-existing `t.Skip` hits, **none in
    `cli_proxy*.go`** (`grep -c cli_proxy` on the gate output = 0). Master state,
    untouched by this diff.
- **Red → green (the guard actually catches the defect)**: the new test was run
  against the **unmodified** `cli_proxy.go` (HEAD), with only the constant
  replaced by the literal phrase so the old tree compiles:

  ```
  --- FAIL: TestProxyBusyPortNeverClaimsListening (7.59s)
      cli_proxy_test.go:79: the proxy claimed readiness for a port it never bound ("127.0.0.1:35151") — the banner precedes the fact:
          gopi proxy: listening on 127.0.0.1:35151 (upstream=openai base=http://127.0.0.1:1/v1 model=stub-model)
          gopi proxy: listen tcp 127.0.0.1:35151: bind: address already in use
  FAIL	github.com/nick/gopi/cmd/agent	8.970s
  ```

  With the fix in place the same test passes (above). The banner-then-bind
  ordering is reproduced by the guard, not merely inferred.
- **Ran (Rule 31 — the real binary, real ports, verbatim)**
  `bash /home/user/workspace/proxy-verify.sh`, which rebuilds `bin/gopi` and
  drives `gopi proxy --upstream openai --openai-base-url http://127.0.0.1:1/v1
  --openai-model stub-model --openai-key stub-key`:

  - **case A — busy port** (held by an ordinary Python listener; `ss -ltn
    'sport = :45481'` → `LISTEN 0 1 127.0.0.1:45481 0.0.0.0:*`):

    ```
    $ bin/gopi proxy --listen 127.0.0.1:45481 <flags>
    exit=1
    gopi proxy: listen tcp 127.0.0.1:45481: bind: address already in use
    listening-on lines: 0
    bind lines: 1
    ```

  - **case B — free port** (`--listen 127.0.0.1:18131`), banner vs kernel table
    vs a real request:

    ```
    banner:  gopi proxy: listening on 127.0.0.1:18131 (upstream=openai base=http://127.0.0.1:1/v1 model=stub-model)
    ss -ltn 'sport = :18131' →  LISTEN 0 4096 127.0.0.1:18131 0.0.0.0:*
    POST /v1/messages         →  http_status=502
      {"error":{"message":"upstream request failed: Post \"http://127.0.0.1:1/v1/chat/completions\": dial tcp 127.0.0.1:1: connect: connection refused", …}}
    ```

    The 502 is the intended answer: the listener is gopi's and the upstream is
    deliberately unreachable, so the socket accepted, parsed and forwarded the
    request — the banner's promise kept. `bind: address already in use` comes
    only after a real bind attempt, and no "listening on" precedes it.
- **Rule 27**: no load generator anywhere — the busy port is one ordinary bound
  listener, and the free-port case does one POST.
- **Rule 28**: `cmd/agent/cli_proxy.go` + its test only; no scheduler, merger,
  ledger, worktree or standards-gate file is touched (`git status --porcelain`
  lists exactly those two files).
- **Commit**: `917f5cd5 fix(proxy): announce readiness only after the socket is
  bound` (+ `dcf1c0e3 docs(index): register devdoc/proxy-banner-truthful.md`) on
  the task branch; files `cmd/agent/cli_proxy.go`,
  `cmd/agent/cli_proxy_test.go`, `devdoc/proxy-banner-truthful.md`,
  `devdoc/DOC-INDEX.tsv`.

## Evidence (self-filed)

- `go test ./cmd/agent/ -run TestProxyBusyPortNeverClaimsListening -count=1`
  against HEAD's `cli_proxy.go` → `--- FAIL … the banner precedes the fact:
  gopi proxy: listening on 127.0.0.1:35151 …` (exit 1); against the fix →
  `--- PASS … (4.04s)`.
- `bash /home/user/workspace/proxy-verify.sh` case A → `exit=1`,
  `listening-on lines: 0`, `bind lines: 1`; case B → `ss -ltn` shows
  `LISTEN … 127.0.0.1:18131` and the same address answers
  `http_status=502` to a real POST.
- `go test ./internal/stylegate/ -count=1 2>&1 | grep -c cli_proxy` → `0`
  (the gate's red is master's, in files this diff does not touch).
- `go test ./internal/standards/ -count=1` → `ok … 62.204s`.
