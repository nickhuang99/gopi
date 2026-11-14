// cli_web_local_trust_test.go — the same-host trust fix for `gopi web`.
//
// The defect this file pins: a `gopi web` bind that is not concretely
// loopback (e.g. --addr=:8090) mints an access token and the gate required it
// from EVERY peer — including a browser running on the gopi host itself, which
// is the URL the banner tells the operator to open. That is the
// "unauthorized: open the token URL printed by 'gopi web'…" page a normal
// same-machine user hit, and it reads like a missing API key even though the
// model credential is untouched (the web server builds its agent exactly like
// `gopi chat` does).
//
// The posture is the local shell's: whoever can run `gopi web` can already run
// gopi with tools from that shell, so a request that demonstrably originates on
// THIS machine carries the same trust and needs no token. Remote peers are
// unchanged — they still need the token.
//
// These tests are the regression guard: they fail against the pre-fix gate,
// which had no local-peer branch at all.

package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// helperLocalPeerAddrs returns "ip:port" peer strings that ARE this machine:
// the loopback forms plus every non-loopback address on its interfaces.
func helperLocalPeerAddrs(t *testing.T) []string {
	t.Helper()
	peers := []string{"127.0.0.1:54321", "[::1]:54321", "localhost:54321"}
	for _, ip := range helperNonLoopbackIPs(t) {
		peers = append(peers, ip+":54321")
	}
	return peers
}

// webTestPeerNonLocal is a plausible off-host peer: a private address that is
// not on this machine's interfaces. Used for the "remote peers are unchanged"
// half of every assertion.
const webTestPeerNonLocal = "192.0.2.77:54321"

// TestWebPeerIsLocal_OwnAddressesAreLocal is the trust predicate's red proof: a
// peer at one of this host's OWN addresses is local (no token needed), while an
// off-host address and an unparseable one are not. The negative cases are the
// fail-closed half (Rule 15) — a wrong "true" here would serve a
// network-reachable agent with no gate at all.
func TestWebPeerIsLocal_OwnAddressesAreLocal(t *testing.T) {
	for _, peer := range helperLocalPeerAddrs(t) {
		if !webPeerIsLocal(peer) {
			t.Errorf("webPeerIsLocal(%q) = false, want true (this machine's own address)", peer)
		}
	}
	for _, peer := range []string{
		webTestPeerNonLocal,
		"192.0.2.9:1",    // TEST-NET-1: never a local address
		"",               // no peer at all
		"not-an-address", // unparseable
		"0.0.0.0:80",     // unspecified: never a peer we can point at
	} {
		if webPeerIsLocal(peer) {
			t.Errorf("webPeerIsLocal(%q) = true, want false (must fail closed)", peer)
		}
	}
}

// TestWebLocalHostNeedsNoToken is THE regression test for the reported bug: on
// a gated (non-loopback) bind, a browser ON this host opens the page without
// the token, while a remote peer is still refused. Both directions run on the
// REAL chat route table, so the gate is proved to be what decides.
func TestWebLocalHostNeedsNoToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	srv := &webServer{}
	bare := srv.webChatHandler()

	// The gate as runWeb wires it for a wildcard bind (local trust on).
	gated := webTokenGate(token, bare, true)

	// (1) LOCAL peer, no token: served. Pre-fix this was 401 — the bug.
	for _, peer := range helperLocalPeerAddrs(t) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = peer
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("REGRESSION: local peer %s got %d, want 200 — a browser on the gopi host must not need the token (body: %q)",
				peer, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}

	// (2) LOCAL peer on the SSE turn route too — the fix is not page-only.
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"hi"}`))
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("local POST /api/chat = 401, want it served (the turn route is gated the same way)")
	}

	// (3) REMOTE peer, no token: still 401, and the body must not send the
	// operator hunting for an API key (the reported confusion) — it names the
	// banner token and says a request from this host needs no token.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = webTestPeerNonLocal
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("RED-PROOF FAILED: remote peer without the token got %d, want 401 — local trust must not weaken remote peers", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, webTokenQueryKey+"=<token>") {
		t.Errorf("401 body does not point at the banner token URL: %q", body)
	}
	if !strings.Contains(body, "NOT the model API key") {
		t.Errorf("401 body fails to rule out the API-key misreading (the reported confusion): %q", body)
	}

	// (4) REMOTE peer WITH the printed token URL: served — the LAN one-click
	// handoff the token exists for is untouched.
	req = httptest.NewRequest(http.MethodGet, "/?"+webTokenQueryKey+"="+token, nil)
	req.RemoteAddr = webTestPeerNonLocal
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("remote peer with the printed token URL got %d, want 200 (the LAN handoff broke)", rec.Code)
	}
}

// TestWebLocalTrustEscapeHatch: GOPI_WEB_LOCAL_TRUST=0 restores
// token-for-everyone — the operator's switch when the gopi host itself is
// shared. Local trust is the default (no flag needed), so one operator on one
// machine is the ordinary case.
func TestWebLocalTrustEscapeHatch(t *testing.T) {
	if !webLocalTrust(":8090") {
		t.Fatal("local trust is off by default — the ordinary same-host browser would be asked for a token again")
	}
	t.Setenv(webLocalTrustEnvName, webLocalTrustOffValue)
	if webLocalTrust(":8090") {
		t.Fatalf("%s=%s must disable local trust", webLocalTrustEnvName, webLocalTrustOffValue)
	}

	// With local trust off, the gate demands the token from a local peer too.
	gated := webTokenGate("secret-token", (&webServer{}).webChatHandler(), webLocalTrust(":8090"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("local peer with local trust disabled got %d, want 401", rec.Code)
	}
}

// TestWebTokenURL_SkipsTokenForLocalURLs: the banner instruction must be true.
// A URL pointing at THIS machine works as printed, so appending a token would
// be a false instruction (and put a secret in a URL the operator may paste
// elsewhere); a URL for another machine keeps its token.
func TestWebTokenURL_SkipsTokenForLocalURLs(t *testing.T) {
	const token = "abc123"
	local := []string{
		"http://localhost:8090",
		"http://127.0.0.1:8090",
		"http://[::1]:8090",
	}
	for _, ip := range helperNonLoopbackIPs(t) {
		local = append(local, "http://"+ip+":8090")
	}
	for _, u := range local {
		// A LOOPBACK url goes bare under local trust; an interface-address URL
		// keeps its token (another machine opens it and needs the token) — in
		// both cases the printed URL is true.
		wantBare := strings.Contains(u, "localhost") || strings.Contains(u, "127.0.0.1") || strings.Contains(u, "::1")
		if got := webTokenURL(u, token, true); (got == u) != wantBare {
			t.Errorf("webTokenURL(%q, local trust) = %q, want bare=%v", u, got, wantBare)
		}
		if got := webTokenURL(u, token, false); got == u {
			t.Errorf("webTokenURL(%q, local trust OFF) = %q, want the token appended", u, got)
		}
	}
	if got := webTokenURL("http://"+strings.TrimSuffix(webTestPeerNonLocal, ":54321")+":8090", token, true); got != "http://192.0.2.77:8090?"+webTokenQueryKey+"="+token {
		t.Errorf("remote URL = %q, want the token appended", got)
	}
}

// TestWebPrintListening_LocalTrustSkipsTokenForOwnURLs: on a gated bind with
// local trust on, the banner prints the host's own URLs bare and stays true —
// every URL it prints opens without a token, which is exactly what the operator
// does. With local trust off the token comes back on every line.
func TestWebPrintListening_LocalTrustSkipsTokenForOwnURLs(t *testing.T) {
	const token = "feedfacefeedface"
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()
	port := addr[strings.LastIndex(addr, ":")+1:]

	out := captureStderr(t, func() { webPrintListening(":0", ln, token, true) })
	if strings.Contains(out, "localhost:"+port+"?"+webTokenQueryKey) {
		t.Fatalf("banner put a token on this host's own URL (it works without one):\n%s", out)
	}
	if !strings.Contains(out, "open: http://localhost:"+port+"\n") {
		t.Fatalf("banner is missing the plain local URL:\n%s", out)
	}

	out = captureStderr(t, func() { webPrintListening(":0", ln, token, false) })
	if !strings.Contains(out, "open: http://localhost:"+port+"?"+webTokenQueryKey+"="+token) {
		t.Fatalf("with local trust off the banner must carry the token:\n%s", out)
	}
}
