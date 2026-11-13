// cli_ui_remote_test.go — the remote-access half of `gopi ui`.
//
// THE DEFECT THIS PINS. `gopi ui` starts the same web engine as `gopi web` and
// gate-wraps it exactly the same way, but it printed NO banner: a non-loopback
// `--addr` minted a per-run token that appeared NOWHERE, so a second machine had
// no URL to open and remote browser access was impossible in practice (a browser
// on the same host worked only because local trust admits it without a token).
// These tests fail on the pre-fix code — the built binary prints a bare
// "listening" with no URL at all.
package main

import (
	"net"
	"strings"
	"testing"
)

// uiTestListener binds a TCP listener on the loopback interface and returns it
// with its resolved port. Its ADDRESS is not what the handoff is printed for —
// the caller passes the DISPLAY address (the LAN address the operator chose) —
// but the listener is what supplies the BOUND PORT.
func uiTestListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	//stylegate:ignore closes the fixture listener the test owns
	t.Cleanup(func() { ln.Close() })
	return ln
}

// TestUIRemoteHandoff_PrintsTheTokenBearingLANURL is the epic's acceptance in
// one assertion: on a non-loopback bind `gopi ui` must hand over a URL a REMOTE
// browser can open as-is — i.e. an interface-address URL carrying the per-run
// access token — and it must warn about the exposure in its OWN name (the
// warning text is gopi web's, the carrier prefix is not).
func TestUIRemoteHandoff_PrintsTheTokenBearingLANURL(t *testing.T) {
	const token = "cafebabecafebabecafebabecafebabe"
	ln := uiTestListener(t)
	port := strings.TrimPrefix(ln.Addr().String(), "127.0.0.1:")

	out := captureStderr(t, func() { uiPrintRemoteHandoff("0.0.0.0:0", ln, token, false, true) })

	// The banner must speak as `gopi ui`, never as `gopi web` (a false name is a
	// false instruction about which surface the operator is looking at).
	if !strings.Contains(out, webCarrierUI+": listening") {
		t.Fatalf("banner does not announce %s:\n%s", webCarrierUI, out)
	}
	if strings.Contains(out, webCarrierWeb+": listening") {
		t.Fatalf("banner announces the wrong carrier (%s):\n%s", webCarrierWeb, out)
	}

	// Every interface address the machine has must be offered WITH the token —
	// that URL is what another machine opens.
	for _, u := range webDisplayURLs(webWildcardHost4, port) {
		want := "open: " + u + "?" + webTokenPair(token)
		if !strings.Contains(out, want) {
			t.Fatalf("banner is missing the token-bearing URL %q:\n%s", want, out)
		}
	}

	// The loopback URL of a WILDCARD bind is itself a token-less line only when
	// local trust is on; here trust is off, so it carries the token too — and
	// either way the LAN URLs above are present.
	if !strings.Contains(out, "LAN-REACHABLE") {
		t.Fatalf("banner omits the LAN exposure warning:\n%s", out)
	}
	if !strings.Contains(out, "remote tool execution") {
		t.Fatalf("banner omits the tools-on-LAN escalation:\n%s", out)
	}
}

// TestUIRemoteHandoff_LocalTrustExplainsWhyTheLocalURLHasNoToken: with local
// trust ON, this host's own browser needs no token — printing one there would be
// a false instruction, and printing NOTHING about it is what once made a
// same-host browser look like an "apikey" failure.
func TestUIRemoteHandoff_LocalTrustExplainsWhyTheLocalURLHasNoToken(t *testing.T) {
	const token = "feedfacefeedfacefeedfacefeedface"
	ln := uiTestListener(t)

	out := captureStderr(t, func() { uiPrintRemoteHandoff("0.0.0.0:0", ln, token, true, false) })
	if !strings.Contains(out, "skip the token") {
		t.Fatalf("banner does not explain the local-trust posture:\n%s", out)
	}
	if strings.Contains(out, "remote tool execution") {
		t.Fatalf("tools were off but the tools escalation was printed:\n%s", out)
	}
}

// TestUIRemoteHandoff_LoopbackBindPrintsNoRemoteBlock: the loopback default has
// no handoff to make. Its absence is asserted because a remote banner on a
// loopback bind would be a FALSE security statement — the exact mistake
// webPrintListening was fixed for on the browser carrier.
func TestUIRemoteHandoff_IsNotCalledForLoopback(t *testing.T) {
	// The decision lives at the call site (uiStartServer calls the handoff only
	// under !addressIsLoopback). Pin the predicate itself, so a change to
	// addressIsLoopback's fail-closed rule cannot silently start printing a
	// remote handoff (or silently stop printing one).
	if !addressIsLoopback(webDefaultAddr()) {
		t.Fatalf("the default address %s no longer reads as loopback", webDefaultAddr())
	}
	if addressIsLoopback("0.0.0.0:8090") {
		t.Fatal("a wildcard bind reads as loopback — the gate would be skipped")
	}
}

// TestUIIdentityURL_CarriesTheRouteAndTheToken: the identity URL printed to the
// operator must be the one that WORKS from where they are — origin + route, and
// the token whenever the bind demands one. A URL assembled anywhere else could
// disagree with the gate.
func TestUIIdentityURL_CarriesTheRouteAndTheToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	ln := uiTestListener(t)
	port := strings.TrimPrefix(ln.Addr().String(), "127.0.0.1:")

	got := uiIdentityURL("0.0.0.0:0", ln, token, false)
	// The ROUTE must precede the query: the gate reads the token from the URL's
	// query string, so `…/api/identity?t=<token>` authenticates the path while
	// `…?t=<token>/api/identity` puts the route inside the token's VALUE and
	// 401s (measured live — this assertion is that regression's guard).
	wantTail := webIdentityPath + "?" + webTokenPair(token)
	if !strings.HasSuffix(got, wantTail) {
		t.Fatalf("identity URL %q does not end in %q — the route must precede the query", got, wantTail)
	}
	if !strings.Contains(got, ":"+port) {
		t.Fatalf("identity URL %q does not name the BOUND port %s (a `:0` bind would print an unusable URL)", got, port)
	}
	// A wildcard bind offers BOTH this host's loopback and its interface
	// addresses: the identity line must hand out an address ANOTHER machine can
	// open, not the `localhost` line that only works where it was printed.
	if webURLIsLoopback(got) && len(webDisplayURLs(webWildcardHost4, port)) > 1 {
		t.Fatalf("identity URL %q is loopback-only while interface addresses exist — a remote operator could not open it", got)
	}

	// A loopback bind mints nothing: the URL stays token-free, exactly like the
	// loopback banner.
	got = uiIdentityURL(webDefaultAddr(), ln, "", true)
	if strings.Contains(got, "?") {
		t.Fatalf("loopback identity URL carries a query string: %q", got)
	}
	if got != "http://127.0.0.1:"+port+webIdentityPath {
		t.Fatalf("loopback identity URL = %q, want the loopback origin + route", got)
	}
}
