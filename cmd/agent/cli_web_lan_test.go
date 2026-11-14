// cli_web_lan_test.go — LAN access for `gopi web` (this Epic).
//
// The default stays loopback, but reaching the page from another device is a
// supported one-step path: bind a non-loopback address and the banner prints
// the ready-to-open LAN URL. Because such a bind is network-reachable — and
// `gopi web --tools` executes tools with no approval gate — the server mints
// a per-run access token that the banner embeds in every printed URL. These
// tests pin both halves: the ergonomics (two-token --addr, token-carrying
// URL) and the gate (red-proved: with the gate removed the very same
// unauthenticated request reaches the handler).

package main

import (
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestAddressIsLoopback pins the predicate that decides whether the access
// gate is required: only a concretely-loopback host may skip it. Anything
// unparseable, empty or wildcard is treated as reachable — the gate must fail
// CLOSED, never open.
func TestAddressIsLoopback(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8090", true},
		{"[::1]:8090", true},
		{"localhost:8090", true},
		{":8090", false},        // all interfaces
		{"0.0.0.0:8090", false}, // wildcard v4
		{"[::]:8090", false},    // wildcard v6
		{"192.0.2.6:8090", false},
		{"192.0.2.7:8090", false},
		{"not-an-address", false}, // unparseable: loopback cannot be promised
		{"", false},
	}
	for _, c := range cases {
		if got := addressIsLoopback(c.addr); got != c.want {
			t.Errorf("addressIsLoopback(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}

// TestWebParseFlags_LANBindForms: reaching the LAN must be one step — both
// the joined (--addr=0.0.0.0:8090) and the space-separated (--addr :8090)
// spellings must work, and a missing value must fail loudly instead of
// silently serving on the default loopback address.
func TestWebParseFlags_LANBindForms(t *testing.T) {
	opts, err := webParseFlags([]string{"--addr=0.0.0.0:8090"}, webDefaultAddrValue, false, false)
	if err != nil || opts.Help || opts.Tools || opts.Thinking || opts.Addr != "0.0.0.0:8090" {
		t.Fatalf("--addr=0.0.0.0:8090 → %+v (err %v), want (0.0.0.0:8090,false,false,false,nil)", opts, err)
	}

	opts, err = webParseFlags([]string{"--addr", "192.0.2.6:8090", "--tools"}, webDefaultAddrValue, false, false)
	if err != nil || opts.Help || !opts.Tools || opts.Thinking || opts.Addr != "192.0.2.6:8090" {
		t.Fatalf("--addr 192.0.2.6:8090 --tools → %+v (err %v), want the LAN addr with tools", opts, err)
	}

	if opts, err = webParseFlags([]string{"-h"}, webDefaultAddrValue, false, false); err != nil || !opts.Help {
		t.Fatalf("-h → help=%v err=%v, want help with no error", opts.Help, err)
	}

	if _, err = webParseFlags([]string{"--addr"}, webDefaultAddrValue, false, false); err == nil {
		t.Fatal("a dangling --addr must be a usage error, not a silent fallback to loopback")
	}

	// No flag: the default address survives untouched (the loopback posture).
	if opts, err = webParseFlags(nil, webDefaultAddrValue, false, false); err != nil || opts.Addr != webDefaultAddrValue {
		t.Fatalf("no flags → %q (err %v), want the loopback default %q", opts.Addr, err, webDefaultAddrValue)
	}
}

// TestWebNewAccessToken: real entropy, hex-encoded, distinct per run — no
// reused or empty secret.
func TestWebNewAccessToken(t *testing.T) {
	a, err := webNewAccessToken()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	b, err := webNewAccessToken()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	for _, tok := range []string{a, b} {
		if len(tok) != webTokenBytes*2 {
			t.Fatalf("token %q: length %d, want %d hex chars", tok, len(tok), webTokenBytes*2)
		}
		if _, err := hex.DecodeString(tok); err != nil {
			t.Fatalf("token %q is not hex: %v", tok, err)
		}
	}
	if a == b {
		t.Fatal("two minted tokens are identical — the per-run secret is not per-run")
	}
}

// TestWebTokenGate_RedProof is the load-bearing security test for the LAN
// opt-in. It proves BOTH directions on the REAL route table:
//   - the bare mux answers an unauthenticated GET / with 200 (so the 401
//     below is caused by the gate, not by the handler refusing);
//   - the gated mux refuses the same request with 401;
//   - with the token (query, cookie, or Bearer) the same request reaches the
//     page handler again.
//
// Delete webTokenGate from the server wiring and the middle assertion is the
// one that fails — that is the regression guard.
func TestWebTokenGate_RedProof(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	srv := &webServer{}

	// Half 1 of the red proof: WITHOUT the gate the request is served. This is
	// exactly the exposure a bare LAN bind used to create.
	bare := srv.webChatHandler()
	rec := httptest.NewRecorder()
	bare.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("control failed: ungated GET / = %d, want 200 (the gate must be what refuses)", rec.Code)
	}

	// localTrust=false: this test exercises the REMOTE posture (the load-bearing
	// LAN gate); the same-host trust path has its own file
	// (cli_web_local_trust_test.go).
	gated := webTokenGate(token, srv.webChatHandler(), false)

	// Half 2: with the gate, the SAME request is refused.
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("RED-PROOF FAILED: unauthenticated GET / = %d, want 401 — a LAN bind would serve this agent openly", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("401 without a WWW-Authenticate challenge — a browser cannot tell it needs the token")
	}

	// A wrong token is refused too (no prefix/substring matching).
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?t="+token[:8], nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("partial token accepted: GET /?t=<prefix> = %d, want 401", rec.Code)
	}

	// The printed URL carries the token → 200, and the page sets the cookie
	// so the page's own follow-up fetches work without re-appending it.
	req := httptest.NewRequest(http.MethodGet, "/?"+webTokenQueryKey+"="+token, nil)
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "gopi web") {
		t.Fatalf("token URL: GET /?%s=%s = %d, want 200 with the chat page", webTokenQueryKey, token, rec.Code)
	}
	cookies := rec.Result().Cookies()
	var gate *http.Cookie
	for _, c := range cookies {
		if c.Name == webAccessCookieName {
			gate = c
		}
	}
	if gate == nil {
		t.Fatalf("token page load set no %s cookie (got %v) — the page's own /api/chat fetch would 401", webAccessCookieName, cookies)
	}
	if gate.Value != token || !gate.HttpOnly {
		t.Fatalf("cookie = %+v, want HttpOnly value=%q", gate, token)
	}

	// Cookie alone (no query token) authenticates — the refresh path.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(gate)
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie-authenticated GET / = %d, want 200", rec.Code)
	}

	// Bearer header authenticates — the script/curl path.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Bearer-authenticated GET / = %d, want 200", rec.Code)
	}

	// The SSE turn route is gated as well (the LAN path is not just the page).
	rec = httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"hi"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated POST /api/chat = %d, want 401", rec.Code)
	}

	// Loopback (no token configured): unchanged, ungated local-shell trust.
	rec = httptest.NewRecorder()
	webTokenGate("", srv.webChatHandler(), false).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback (empty token) GET / = %d, want 200 — the default posture must not change", rec.Code)
	}
}

// TestWebTokenURL: the one-click handoff — every printed URL carries the
// token, and the loopback banner stays clean.
func TestWebTokenURL(t *testing.T) {
	const token = "abc123"
	if got := webTokenURL("http://192.0.2.6:8090", token, false); got != "http://192.0.2.6:8090?t=abc123" {
		t.Fatalf("webTokenURL = %q, want the token appended", got)
	}
	if got := webTokenURL("http://127.0.0.1:8090", "", false); got != "http://127.0.0.1:8090" {
		t.Fatalf("webTokenURL with no token = %q, want the URL unchanged", got)
	}
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what
// it printed.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	fn()
	w.Close() //stylegate:ignore test captures a listening notice through an os.Pipe; the read below is the assertion, and the writer close error cannot lose buffered output
	os.Stderr = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	return string(out)
}

// TestWebPrintListening_LANBindCarriesToken: the LAN banner must hand over a
// URL that actually works — every line carries the access token, and the LAN
// addresses are listed (that is the whole point of the opt-in).
func TestWebPrintListening_LANBindCarriesToken(t *testing.T) {
	const token = "feedfacefeedface"
	ips := helperNonLoopbackIPs(t)

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().String()[strings.LastIndex(ln.Addr().String(), ":")+1:]

	out := captureStderr(t, func() { webPrintListening(":0", ln, token, false) })

	for _, want := range []string{
		"open: http://localhost:" + port + "?" + webTokenQueryKey + "=" + token,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("banner missing %q in:\n%s", want, out)
		}
	}
	for _, ip := range ips {
		want := "open: http://" + ip + ":" + port + "?" + webTokenQueryKey + "=" + token
		if !strings.Contains(out, want) {
			t.Fatalf("banner missing the token-carrying LAN URL %q in:\n%s", want, out)
		}
	}
	// A bare (token-less) line would be a dead end for the operator.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "open: ") && !strings.Contains(line, "?"+webTokenQueryKey+"=") {
			t.Fatalf("banner printed a token-less open line (%q) — it would 401 in the browser:\n%s", line, out)
		}
	}
}

// TestWebPrintListening_LoopbackUnaffected: no token configured → the banner
// is exactly what it was (no query string, no noise).
func TestWebPrintListening_LoopbackUnaffected(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	out := captureStderr(t, func() { webPrintListening(addr, ln, "", false) })
	if !strings.Contains(out, "open: http://"+addr+"\n") {
		t.Fatalf("loopback banner = %q, want the plain URL %q", out, "open: http://"+addr)
	}
	if strings.Contains(out, "?") {
		t.Fatalf("loopback banner carries a query string: %q", out)
	}
}

// TestWebLANWarningsNameTheRisk: the exposure notices are the operator's only
// warning, so they must name the token URL and the tool-execution risk.
func TestWebLANWarningsNameTheRisk(t *testing.T) {
	for _, want := range []string{"LAN-REACHABLE", "token"} {
		if !strings.Contains(webLANWarning, want) {
			t.Errorf("webLANWarning missing %q: %s", want, webLANWarning)
		}
	}
	for _, want := range []string{"tools ENABLED", "LAN-REACHABLE", "remote tool execution"} {
		if !strings.Contains(webToolsLANWarning, want) {
			t.Errorf("webToolsLANWarning missing %q: %s", want, webToolsLANWarning)
		}
	}
}

// TestWebGatedHandler_WiringIsLoadBearing is the red proof at the WIRING
// level (not just the gate in isolation): the handler runWeb actually serves
// must refuse an unauthenticated request for a LAN bind, while the loopback
// bind keeps serving bare. If the wiring ever returns the bare handler for a
// non-loopback address, the first assertion fails with the exposure it names.
func TestWebGatedHandler_WiringIsLoadBearing(t *testing.T) {
	bare := (&webServer{}).webChatHandler()

	lan, token, err := webGatedHandler("0.0.0.0:8090", bare, false)
	if err != nil {
		t.Fatalf("LAN bind: %v", err)
	}
	if token == "" {
		t.Fatal("RED-PROOF FAILED: a non-loopback bind produced NO token — the LAN server would be served open")
	}
	rec := httptest.NewRecorder()
	lan.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("RED-PROOF FAILED: LAN bind served an unauthenticated GET / with %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	lan.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?"+webTokenQueryKey+"="+token, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("LAN bind rejected its OWN printed token URL: %d", rec.Code)
	}

	// A pinned token is used instead of a minted one (operator-controlled
	// secret), and it is the one in the URL.
	t.Setenv(webTokenEnv, "pinned-secret-for-tests")
	lan, token, err = webGatedHandler("192.0.2.6:8090", bare, false)
	if err != nil {
		t.Fatalf("pinned: %v", err)
	}
	if token != "pinned-secret-for-tests" {
		t.Fatalf("token = %q, want the GOPI_WEB_TOKEN pin", token)
	}

	// Loopback: bare handler, no token (the default posture is untouched).
	lo, token, err := webGatedHandler("127.0.0.1:8090", bare, false)
	if err != nil || token != "" {
		t.Fatalf("loopback: token=%q err=%v, want no token and no error", token, err)
	}
	rec = httptest.NewRecorder()
	lo.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback GET / = %d, want 200 (no gate on the local path)", rec.Code)
	}
}

// TestWebUsageDocumentsLANAccess: `gopi web -h` must teach the LAN path (a
// feature nobody can find is not delivered).
func TestWebUsageDocumentsLANAccess(t *testing.T) {
	out := helperWebUsage()
	for _, want := range []string{"LAN access", "0.0.0.0", "per-run access token", webTokenEnv} {
		if !strings.Contains(out, want) {
			t.Errorf("gopi web -h missing %q:\n%s", want, out)
		}
	}
}
