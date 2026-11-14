// cli_web_webtoken_test.go — the regression tests for the "the page loads, but
// every send 401s" defect.
//
// The shape of the bug: the page's same-origin API fetches used RELATIVE URLs
// carrying no ?t= token, so they authenticated through the gate cookie ALONE.
// The access token is re-minted per run, so a browser holding a cookie from an
// EARLIER run presented a stale token → ConstantTimeCompare failed → every send
// 401'd ("still unauthorized from this peer") while the page load — which
// carried the fresh ?t= — succeeded.
//
// These tests drive the REAL handler over a REAL listener on a throwaway port
// (never httptest.NewRequest alone: the defect is about what a BROWSER sends
// after a real page load, cookies and all), and the URL the chat POST uses is
// built by the page's OWN JS — extracted from the source and executed under
// node — never by a hand-written one. A page that stops appending the token
// fails here, which is the property no substring check could establish.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

// webTokenTestAddr is the throwaway listener the auth tests use: port 0 lets
// the kernel pick, and the tests dial the address the kernel returned — the
// port itself is never asserted, so a busy machine cannot make these flake.
const webTokenTestAddr = "127.0.0.1:0"

// webTokenTestBind is the address handed to webGatedHandler purely to select
// the GATED branch: a non-loopback bind is what mints a token, while the
// listener stays on loopback so the test can dial it. That is exactly the
// posture under test — a gated server reached by a client that is not the host
// itself (localTrust is off in every harness here).
const webTokenTestBind = "192.0.2.6:8090"

// jsonContentType is the turn body's media type, set so the request is exactly
// what the page's fetch sends (handleChat parses the body either way).
const jsonContentType = "application/json"

// webStaleTokenForTests is a token from "an earlier run": never the run's own,
// and not a prefix/suffix of it (a constant-time compare has no prefix
// semantics, and the test should not imply otherwise).
const webStaleTokenForTests = "0000000000000000000000000000dead"

// webTokenForTests is the token THIS run mints — a value a browser could only
// learn from this run's banner URL (?t=…), never from an older visit.
const webTokenForTests = "cafef00dcafef00d"

// webTokenHarness is one gated server on a real listener.
type webTokenHarness struct {
	base  string // http://127.0.0.1:<port>
	token string
}

// newWebTokenHarness serves the handler runWeb would serve for a non-loopback
// bind, on a real listener. localTrust is OFF, so the tests exercise the remote
// posture: a loopback client would otherwise be served without a token and the
// whole defect would stay invisible.
func newWebTokenHarness(t *testing.T, token string) *webTokenHarness {
	t.Helper()
	t.Setenv(webTokenEnv, token)
	ws, mockSrv := webServerForTest(t, "pong")
	t.Cleanup(mockSrv.Close)

	handler, gotToken, err := webGatedHandler(webTokenTestBind, ws.webChatHandler(), false)
	if err != nil {
		t.Fatalf("webGatedHandler: %v", err)
	}
	if gotToken != token {
		t.Fatalf("gate token = %q, want the pinned %q", gotToken, token)
	}
	ln, err := net.Listen("tcp", webTokenTestAddr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: handler} //stylegate:ignore the throwaway test listener is closed below
	go func() { _ = srv.Serve(ln) }()     //stylegate:ignore serving a throwaway test listener; the error is that listener's own close
	t.Cleanup(func() { _ = srv.Close() }) //stylegate:ignore closing a throwaway test listener
	return &webTokenHarness{base: "http://" + ln.Addr().String(), token: token}
}

// chatBody is the turn body the page sends (the page's own JSON shape).
func chatBody(t *testing.T, message string) io.Reader {
	t.Helper()
	b, err := json.Marshal(webChatRequest{Message: message}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return bytes.NewReader(b)
}

// do issues one request over the REAL listener, with an optional raw Cookie
// header (a jar would hide what the test means to send), and returns the
// response plus its body.
func do(t *testing.T, method, rawURL, cookieHeader string, body io.Reader) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		t.Fatalf("new request %s: %v", rawURL, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", jsonContentType)
	}
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawURL, err)
	}
	defer res.Body.Close() //stylegate:ignore closing a body read immediately below
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res, string(b)
}

// staleCookie builds the Cookie header of a browser that holds the token of a
// PREVIOUS run — the exact client state that produced every 401.
func staleCookie() string {
	return webAccessCookieName + "=" + webStaleTokenForTests
}

// refreshedCookie returns the named cookie from the response's Set-Cookie
// headers, or nil.
func refreshedCookie(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// (a) the reported failure, end to end: the page load carries ?t=, and the
// page's own JS then builds a chat POST that authenticates by the URL — with NO
// cookie at all.
// ---------------------------------------------------------------------------

// TestWebPageToken_PageLoadThenSendCarriesTheToken is the reported bug's exact
// shape: open the page with ?t=<token> (200), then send the URL the page's OWN
// helper builds, with no cookie (a browser that never stored it — a private
// window, a cookie-clearing policy, a first visit). Authentication must come
// from the URL, the same route the page load used, so the turn must be 200 —
// never "still unauthorized".
func TestWebPageToken_PageLoadThenSendCarriesTheToken(t *testing.T) {
	h := newWebTokenHarness(t, webTokenForTests)

	pageURL := h.base + "/?" + webTokenPair(h.token)
	res, body := do(t, http.MethodGet, pageURL, "", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("page load %s = %d, want 200 (body: %s)", pageURL, res.StatusCode, body)
	}
	if !strings.Contains(body, webChatPath) {
		t.Fatalf("the served page does not carry the chat route %q — the page under test is not the shipped one", webChatPath)
	}

	// The page's own builder produces the URL the browser will actually fetch.
	sendURL := h.base + jsPageFetchURL(t, pageFetchChat, "?"+webTokenPair(h.token))
	if !strings.Contains(sendURL, webTokenPair(h.token)) {
		t.Fatalf("the page's JS built %q — it does not append the access token, which IS the defect", sendURL)
	}
	res, body = do(t, http.MethodPost, sendURL, "", chatBody(t, "ping"))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST %s with NO cookie = %d, want 200 — the page's fetch must authenticate by the ?%s= URL (body: %s)",
			sendURL, res.StatusCode, webTokenQueryKey, body)
	}
	if !strings.Contains(body, "event: ") {
		t.Fatalf("the turn did not stream: body = %q", body)
	}
}

// TestWebPageToken_TranscriptAndMenuAlsoCarryTheToken: every same-origin fetch
// the page makes (not just the chat POST) must be authenticated the same way —
// the defect was present on all three routes, and a fix on one is not a fix.
func TestWebPageToken_TranscriptAndMenuAlsoCarryTheToken(t *testing.T) {
	h := newWebTokenHarness(t, webTokenForTests)
	search := "?" + webTokenPair(h.token)
	for _, tc := range []struct {
		name   string
		kind   string
		path   string
		method string
	}{
		{"chat", pageFetchChat, webChatPath, http.MethodPost},
		{"transcript", pageFetchTranscript, webTranscriptPath, http.MethodGet},
		{"menu", pageFetchMenu, webMenuPath, http.MethodGet},
	} {
		url := h.base + jsPageFetchURL(t, tc.kind, search)
		if !strings.Contains(url, tc.path) {
			t.Fatalf("%s: the page built %q, which is not the route %q", tc.name, url, tc.path)
		}
		if !strings.Contains(url, webTokenPair(h.token)) {
			t.Fatalf("%s: the page built %q without the access token — a cookie-only fetch 401s after a re-mint", tc.name, url)
		}
		var body io.Reader
		if tc.method == http.MethodPost {
			body = chatBody(t, "ping")
		}
		res, got := do(t, tc.method, url, "", body)
		if res.StatusCode == http.StatusUnauthorized {
			t.Fatalf("%s: %s %s was refused WITH the token in the URL (body: %s)", tc.name, tc.method, url, got)
		}
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %s %s = %d, want 200 (body: %s)", tc.name, tc.method, url, res.StatusCode, got)
		}
	}
}

// TestWebPageToken_TokenlessPageSendsABareURL: without a ?t= (the loopback
// default, and every page opened before the fix) the page must stay unchanged —
// no stray "?t=" and no "&t=" appended to a URL that carries no token, which
// would be a false instruction on a bind that has no gate.
func TestWebPageToken_TokenlessPageSendsABareURL(t *testing.T) {
	for _, tc := range []struct{ name, kind string }{
		{"chat", pageFetchChat},
		{"transcript", pageFetchTranscript},
		{"menu", pageFetchMenu},
	} {
		if got, want := jsPageFetchURL(t, tc.kind, ""), tc.kind; got != want {
			t.Errorf("%s: tokenless page built %q, want %q (no token, no query added)", tc.name, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// (b) self-heal: a fresh ?t= wins over a stale cookie.
// ---------------------------------------------------------------------------

// TestWebTokenGate_SelfHealsFromAStaleCookie is the "browser held a cookie from
// a previous run" case: the request carries the RIGHT query token AND the WRONG
// cookie. The gate must accept it (the query token is the credential
// webRequestToken already prefers) and must REFRESH the cookie, so the
// cookie-only fetches that follow stop failing — accepting the request without
// repairing the cookie would leave the next fetch 401ing again.
func TestWebTokenGate_SelfHealsFromAStaleCookie(t *testing.T) {
	h := newWebTokenHarness(t, webTokenForTests)

	res, body := do(t, http.MethodGet, h.base+"/?"+webTokenPair(h.token), staleCookie(), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("fresh ?%s= + stale cookie = %d, want 200 (self-heal) — body: %s", webTokenQueryKey, res.StatusCode, body)
	}
	cookie := refreshedCookie(res, webAccessCookieName)
	if cookie == nil {
		t.Fatalf("the gate accepted a stale cookie but sent no Set-Cookie — the next cookie-only fetch would 401 again")
	}
	if cookie.Value != h.token {
		t.Fatalf("refreshed cookie = %q, want this run's token (a stale cookie left in place re-breaks the page)", cookie.Value)
	}
	if !cookie.HttpOnly {
		t.Errorf("the refreshed cookie is not HttpOnly (the first-load cookie is)")
	}

	// The healed cookie alone now authenticates (no query token) — the exact
	// follow-up request that used to 401.
	res, body = do(t, http.MethodGet, h.base+"/", webAccessCookieName+"="+cookie.Value, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("cookie-only request after the heal = %d, want 200 (body: %s)", res.StatusCode, body)
	}
}

// TestWebTokenGate_StaleCookieAloneIsStillRefused: the self-heal must not
// become a blanket accept. A request carrying ONLY the stale cookie presents a
// credential that does not match — it is refused, exactly as before.
func TestWebTokenGate_StaleCookieAloneIsStillRefused(t *testing.T) {
	h := newWebTokenHarness(t, webTokenForTests)
	res, body := do(t, http.MethodPost, h.base+webChatPath, staleCookie(), chatBody(t, "ping"))
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stale cookie, no query token = %d, want 401 (body: %s)", res.StatusCode, body)
	}
}

// TestWebTokenGate_WrongQueryTokenIsStillRefused: a fresh-looking but wrong
// query token is not a credential either — the healing accepts what MATCHED,
// never what merely arrived.
func TestWebTokenGate_WrongQueryTokenIsStillRefused(t *testing.T) {
	h := newWebTokenHarness(t, webTokenForTests)
	res, body := do(t, http.MethodGet, h.base+"/?"+webTokenPair(webStaleTokenForTests), "", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong query token = %d, want 401 (body: %s)", res.StatusCode, body)
	}
}

// ---------------------------------------------------------------------------
// (c) the posture is preserved: no credential → 401, and the loopback default
// keeps serving bare.
// ---------------------------------------------------------------------------

// TestWebTokenGate_NoCredentialRemoteIsRefused: the fix must not weaken the
// gate. A remote peer with NO token — no query, no cookie, no bearer — gets 401
// on the page and on every API route.
func TestWebTokenGate_NoCredentialRemoteIsRefused(t *testing.T) {
	h := newWebTokenHarness(t, webTokenForTests)
	for _, tc := range []struct {
		method string
		url    string
		body   io.Reader
	}{
		{http.MethodGet, h.base + "/", nil},
		{http.MethodGet, h.base + webTranscriptPath, nil},
		{http.MethodGet, h.base + webMenuPath, nil},
		{http.MethodPost, h.base + webChatPath, chatBody(t, "ping")},
	} {
		res, body := do(t, tc.method, tc.url, "", tc.body)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s with no credential = %d, want 401 (body: %s)", tc.method, tc.url, res.StatusCode, body)
		}
	}
}

// TestWebTokenGate_LoopbackDefaultUnchanged: with no token configured (the
// loopback default) the gate is a pass-through — no auth, no cookie. This is
// the posture the local-shell-trust decision protects, and it must survive the
// fix untouched.
func TestWebTokenGate_LoopbackDefaultUnchanged(t *testing.T) {
	ws, mockSrv := webServerForTest(t, "pong")
	t.Cleanup(mockSrv.Close)
	handler, token, err := webGatedHandler("127.0.0.1:8090", ws.webChatHandler(), false)
	if err != nil || token != "" {
		t.Fatalf("loopback bind: token=%q err=%v, want no token and no error", token, err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback GET / = %d, want 200 (no gate on the local path)", rec.Code)
	}
	if cookie := refreshedCookie(rec.Result(), webAccessCookieName); cookie != nil {
		t.Errorf("the ungated path set %s=%q — a tokenless bind must not hand out cookies", webAccessCookieName, cookie.Value)
	}
}

// ---------------------------------------------------------------------------
// (d) the refusal is RECORDED: a 401 leaves a host-side line naming who, what
// and why (Rule 15 — the gate previously logged nothing, so "still unauthorized
// from this peer" was undiagnosable from the host).
// ---------------------------------------------------------------------------

// TestWebTokenGate_RefusalIsLogged drives the refusal through the real listener
// and asserts the diagnostic line carries what an operator needs: the peer, the
// method+path, which carriers arrived, and a reason — and that it never leaks
// token material.
func TestWebTokenGate_RefusalIsLogged(t *testing.T) {
	h := newWebTokenHarness(t, webTokenForTests)
	var res *http.Response
	var body string
	out := captureStderr(t, func() {
		res, body = do(t, http.MethodPost, h.base+webChatPath, staleCookie(), chatBody(t, "ping"))
	})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cookie-only POST = %d, want 401 (body: %s)", res.StatusCode, body)
	}
	for _, want := range []string{webRefusalLogPrefix, http.MethodPost, webChatPath, "cookie=present", "query=absent"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal line does not carry %q — a 401 with no record is the undiagnosable case this closes.\nlog: %s", want, out)
		}
	}
	if !strings.Contains(out, "127.0.0.1") {
		t.Errorf("the refusal line does not name the peer address (RemoteAddr).\nlog: %s", out)
	}
	if strings.Contains(out, h.token) || strings.Contains(out, webStaleTokenForTests) {
		t.Errorf("the refusal line leaks token material — log presence, never secrets.\nlog: %s", out)
	}
	if got := strings.Count(out, webRefusalLogPrefix); got != 1 {
		t.Errorf("want exactly one refusal line, got %d:\n%s", got, out)
	}
	// The line is a fixed shape with every field present (timestamp, peer,
	// method, path, query, cookie, reason) — the format constant is the single
	// spelling, so a field cannot quietly disappear.
	for _, field := range []string{"peer=", " " + http.MethodPost + " ", "query=", "cookie=", "reason="} {
		if !strings.Contains(out, field) {
			t.Errorf("the refusal line is missing the %q field:\n%s", field, out)
		}
	}
}

// TestWebTokenGate_RefusalLineNamesBothCarrierStates: the diagnostic must
// distinguish the shapes an operator faces — a request with a cookie and no
// query token (the reported defect) from one with neither.
func TestWebTokenGate_RefusalLineNamesBothCarrierStates(t *testing.T) {
	h := newWebTokenHarness(t, webTokenForTests)

	withCookie := captureStderr(t, func() {
		do(t, http.MethodGet, h.base+"/", staleCookie(), nil)
	})
	if !strings.Contains(withCookie, "cookie="+webRedactedTokenMarker) || !strings.Contains(withCookie, "query=absent") {
		t.Errorf("a stale-cookie refusal must report query=absent cookie=%s (that pair IS the reported bug):\n%s",
			webRedactedTokenMarker, withCookie)
	}

	bare := captureStderr(t, func() {
		do(t, http.MethodGet, h.base+"/", "", nil)
	})
	if !strings.Contains(bare, "query=absent") || !strings.Contains(bare, "cookie=absent") {
		t.Errorf("a credential-less refusal must report both carriers absent:\n%s", bare)
	}
	if !strings.Contains(bare, "no credential presented") {
		t.Errorf("a credential-less refusal must say so in the reason field:\n%s", bare)
	}
}

// ---------------------------------------------------------------------------
// the page-side half: the builder exists, EVERY fetch uses it, and it agrees
// with the server's constants.
// ---------------------------------------------------------------------------

// pageFetchChat / pageFetchTranscript / pageFetchMenu name the three
// same-origin fetches the page makes, for the JS-driven builder test.
const (
	pageFetchChat       = webChatPath
	pageFetchTranscript = webTranscriptPath
	pageFetchMenu       = webMenuPath + "?trigger=%2F&q=a"
)

// jsPageFetchURL runs the SHIPPED page (extracted from cli_web.go, never a
// copy) under node and returns the URL its OWN fetch helper builds for the
// named endpoint with the given location.search. The page's URL building is
// browser-side code: only the page's own JS knows how the request is spelled,
// and re-implementing it in Go would prove nothing about the page.
func jsPageFetchURL(t *testing.T, endpoint, search string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		if node, err = exec.LookPath("nodejs"); err != nil {
			t.Skip("skipping the page-URL check: no `node` on PATH (the constant-agreement test below still pins the token parameter)")
		}
	}
	out, runErr := exec.Command(node, "cli_web_page_url.js", endpoint, search).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if runErr != nil {
		t.Fatalf("the page URL harness failed (err=%v):\n%s", runErr, text)
	}
	return text
}

// TestWebPageToken_EveryFetchUsesTheTokenBuilder is the source-level half of
// the JS check: EVERY fetch in the page goes through the token builder. A
// future fetch added with a bare relative URL would silently reintroduce the
// defect on that route, and no behaviour test would notice until it 401'd.
func TestWebPageToken_EveryFetchUsesTheTokenBuilder(t *testing.T) {
	script := pageScript(t)
	all := strings.Count(script, "fetch(")
	withToken := strings.Count(script, "fetch(withToken(")
	if all == 0 {
		t.Fatal("the page has no fetch at all — this check would pass vacuously")
	}
	if all != withToken {
		t.Errorf("%d of %d fetch sites do not route through withToken — a same-origin fetch without the ?%s= token depends on the per-run cookie and breaks on a re-mint",
			all-withToken, all, webTokenQueryKey)
	}
	for _, want := range []string{"function withToken(", "function locationToken(", "window.location"} {
		if !strings.Contains(script, want) {
			t.Errorf("the page no longer carries %q — the token cannot be re-read from the URL", want)
		}
	}
}

// TestWebPageToken_PageCarriesTheServerConstants pins every value BOTH carriers
// spell: the token query parameter, the query separator, and the three routes.
// The page cannot import Go constants, so this test is the only thing standing
// between a rename on one side and a silent desynchronization (Rule 13).
func TestWebPageToken_PageCarriesTheServerConstants(t *testing.T) {
	script := pageScript(t)
	cases := []struct {
		name     string
		pageDecl string
		server   string
		why      string
	}{
		{"token query parameter", "const TOKEN_ARG=", webTokenQueryKey,
			"the parameter the page appends is the one the gate reads (webTokenQueryKey)"},
		{"query separator", "const QUERY_SEP=", webTokenQuerySep,
			"the separator between an existing query and the token pair"},
		{"chat route", "const API_CHAT=", webChatPath,
			"the chat POST target is the route the mux registers"},
		{"transcript route", "const API_TRANSCRIPT=", webTranscriptPath,
			"the replay target is the route the mux registers"},
		{"menu route", "const API_MENU=", webMenuPath,
			"the candidate target is the route the mux registers"},
	}
	for _, tc := range cases {
		i := strings.Index(script, tc.pageDecl)
		if i < 0 {
			t.Errorf("%s: the page no longer declares %q — %s", tc.name, tc.pageDecl, tc.why)
			continue
		}
		line := script[i:]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		if !strings.Contains(line, "'"+tc.server+"'") {
			t.Errorf("%s: the page declares %q carrying %q, but the server value is %q — the two carriers have drifted (%s)",
				tc.name, tc.pageDecl, line, tc.server, tc.why)
		}
	}
}

// TestWebTokenPairIsTheSingleSpelling: the token pair is built in ONE place and
// the banner URL uses it, so a change to the parameter name cannot reach the JS
// while missing the printed URL.
func TestWebTokenPairIsTheSingleSpelling(t *testing.T) {
	want := webTokenQueryKey + "=" + "abc"
	if got := webTokenPair("abc"); got != want {
		t.Fatalf("webTokenPair = %q, want %q", got, want)
	}
	if u := webTokenURL("http://192.0.2.6:8090", "abc", false); u != "http://192.0.2.6:8090?"+want {
		t.Fatalf("webTokenURL = %q, want the pair built by webTokenPair", u)
	}
	if webTokenQuerySep != "&" {
		t.Fatalf("webTokenQuerySep = %q — the page's QUERY_SEP is pinned to this value by the constant-agreement test", webTokenQuerySep)
	}
}
