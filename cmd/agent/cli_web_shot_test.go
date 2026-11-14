// cli_web_shot_test.go — the endpoint half of the «📷 截当前页» increment.
//
// Three kinds of check live here and none can replace another:
//
//  1. BEHAVIOUR over the real mux (`webChatHandler`), with the capture seam
//     recorded — the handler's whole path (method gate, body refusal, self-
//     address proof, validation, base64, error surfacing) without a browser.
//  2. THE PRIVACY PROPERTY, asserted on the OPTIONS the endpoint actually asks
//     internal/shot for: Self, the web surface, this process's own loopback
//     address. That is the constraint "never the desktop", expressed as a fact
//     about the call rather than a sentence in a comment.
//  3. THE SSRF BOUNDARY: a client-supplied target is refused, and the refusal
//     NAMES the field — a capture that silently ignored {"url":…} would be a
//     jump host with a shrug.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nickhuang99/gopi/internal/shot"
)

// webShotTestPNG is a minimal byte string standing in for the capture: the
// handler never inspects PNG structure (it validates the WIRE contract), so a
// short payload keeps the test's assertions about base64 exact.
var webShotTestPNG = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4}

// withShotSeam installs a capture stub for the duration of a test and restores
// the real one afterwards. A test that forgets the restore would leave a stub
// installed for its siblings — the restore is why this is one helper.
func withShotSeam(t *testing.T, png []byte, err error) *shotCallRecord {
	t.Helper()
	rec := &shotCallRecord{}
	prev := webShotRun
	webShotRun = func(_ context.Context, opts shot.Options) ([]byte, error) {
		rec.calls.Add(1)
		rec.opts = opts
		return png, err
	}
	t.Cleanup(func() { webShotRun = prev })
	return rec
}

// shotCallRecord is what the seam saw. calls is atomic because the server may
// serve concurrently in a future test; the options are read after the request
// returns.
type shotCallRecord struct {
	calls atomic.Int32
	opts  shot.Options
}

// shotTestServer builds a webServer whose listen address is a REAL loopback
// listener (the handler proves s.addr is one of this process's listeners, so a
// fabricated address would be refused before the capture — correctly). The
// listener is closed at cleanup; the address string is what the handler reads.
func shotTestServer(t *testing.T) (*webServer, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("cannot bind a loopback listener for the test: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() }) //stylegate:ignore closing a test listener cannot fail usefully
	// Accept in the background: the handler's self-probe dials this address and
	// a listener that never accepts would make every test time out.
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_ = conn.Close() //stylegate:ignore probe connections carry no data
		}
	}()
	addr := ln.Addr().String()
	return &webServer{addr: addr}, addr
}

// TestWebShotCapturesThisPagesRendering is the endpoint's happy path: a POST
// with no body answers 200 with {"image":{"media_type":"image/png",
// "data_b64":…}} whose payload is EXACTLY the captured bytes, base64 encoded.
func TestWebShotCapturesThisPagesRendering(t *testing.T) {
	rec := withShotSeam(t, webShotTestPNG, nil)
	ws, addr := shotTestServer(t)
	srv := httptest.NewServer(ws.webChatHandler())
	defer srv.Close()

	res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST %s: %v", webShotPath, err)
	}
	defer res.Body.Close()          //nolint:errcheck // test read
	body, _ := io.ReadAll(res.Body) //stylegate:ignore bounded test read of a small JSON body

	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200 — body: %s", webShotPath, res.StatusCode, body)
	}
	var got webShotResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("the response is not the documented JSON shape: %v — body: %s", err, body)
	}
	if got.Image.MediaType != webShotMediaType {
		t.Errorf("media_type = %q, want %q", got.Image.MediaType, webShotMediaType)
	}
	want := base64.StdEncoding.EncodeToString(webShotTestPNG)
	if got.Image.DataB64 != want {
		t.Errorf("data_b64 = %q, want the base64 of the captured bytes %q", got.Image.DataB64, want)
	}
	if rec.calls.Load() != 1 {
		t.Errorf("the capture ran %d time(s), want exactly 1", rec.calls.Load())
	}
	// The payload must satisfy the SAME validator /api/chat applies — otherwise
	// the page would stage a picture the next turn refuses.
	if _, err := validateWebImage(got.Image); err != nil {
		t.Errorf("the returned image does not satisfy validateWebImage: %v", err)
	}
	if rec.opts.Addr != addr {
		t.Errorf("the capture targeted %q, want this server's own address %q", rec.opts.Addr, addr)
	}
}

// TestWebShotAlwaysAsksForTheWebSurfaceOfItsOwnPage is the PRIVACY gate: the
// capture options this endpoint asks for are constants — Self, the web surface,
// this process's loopback address — and nothing from the request reaches them.
//
// The RED PROOF: flip webShotCapture's Surface to any other value (or drop
// Self, or pass the client's bytes as an address) and this test fails, naming
// the option that moved. That is the difference between "we do not call the
// desktop capture" and "this endpoint cannot ask for it".
func TestWebShotAlwaysAsksForTheWebSurfaceOfItsOwnPage(t *testing.T) {
	rec := withShotSeam(t, webShotTestPNG, nil)
	ws, addr := shotTestServer(t)
	srv := httptest.NewServer(ws.webChatHandler())
	defer srv.Close()

	// A body that TRIES to redirect the capture. It is refused (see the
	// SSRF test), so drive the accepted path first: an empty body.
	res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST %s: %v", webShotPath, err)
	}
	defer res.Body.Close() //nolint:errcheck // test read
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST with an empty object = %d, want 200", res.StatusCode)
	}

	if !rec.opts.Self {
		t.Error("the capture options did not set Self — the endpoint must capture THIS instance's page, not an arbitrary URL")
	}
	if rec.opts.Surface != shot.SurfaceWeb {
		t.Errorf("the capture surface is %q, want %q — the endpoint may only ever ask for the page", rec.opts.Surface, shot.SurfaceWeb)
	}
	if rec.opts.Addr != addr {
		t.Errorf("the capture targeted %q, want this server's own address %q", rec.opts.Addr, addr)
	}
	if !webShotHostIsLoopback(mustHost(t, rec.opts.Addr)) {
		t.Errorf("the capture target %q is not a loopback address", rec.opts.Addr)
	}
	// The URL field belongs to the OTHER shot surface (an arbitrary URL). The
	// endpoint must leave it empty: a non-empty URL would be a target nothing
	// on this page controls.
	if rec.opts.URL != "" {
		t.Errorf("the capture options carry URL=%q — the endpoint must never capture an arbitrary URL", rec.opts.URL)
	}
}

// TestWebShotRefusesAClientSuppliedTarget is the SSRF gate: every attempt to
// point the capture somewhere is refused with 400 and a message naming the
// field, and NO capture runs (a refusal that still launched Chromium would be
// an oracle an attacker can use to probe hosts).
func TestWebShotRefusesAClientSuppliedTarget(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "url", body: `{"url":"http://example.com/"}`, want: "url"},
		{name: "addr", body: `{"addr":"192.0.2.9:8090"}`, want: "addr"},
		{name: "surface", body: `{"surface":"screen"}`, want: "surface"},
		{name: "several", body: `{"url":"http://x/","surface":"screen"}`, want: "surface"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := withShotSeam(t, webShotTestPNG, nil)
			ws, _ := shotTestServer(t)
			srv := httptest.NewServer(ws.webChatHandler())
			defer srv.Close()

			res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("POST %s: %v", webShotPath, err)
			}
			defer res.Body.Close()          //nolint:errcheck // test read
			body, _ := io.ReadAll(res.Body) //stylegate:ignore bounded test read of a small error body

			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("POST with %s = %d, want 400 (body: %s)", tc.body, res.StatusCode, body)
			}
			if !strings.Contains(string(body), tc.want) {
				t.Errorf("the refusal does not name the offending field %q — body: %s", tc.want, body)
			}
			if got := rec.calls.Load(); got != 0 {
				t.Errorf("a refused request still ran the capture %d time(s) — a refusal must cost no browser", got)
			}
		})
	}
}

// TestWebShotMethodGate pins the verb: GET /api/shot is a 405, never a capture.
// (A GET that captured would be triggerable by an <img src> on any page.)
func TestWebShotMethodGate(t *testing.T) {
	rec := withShotSeam(t, webShotTestPNG, nil)
	ws, _ := shotTestServer(t)
	srv := httptest.NewServer(ws.webChatHandler())
	defer srv.Close()

	res, err := http.Get(srv.URL + webShotPath)
	if err != nil {
		t.Fatalf("GET %s: %v", webShotPath, err)
	}
	defer res.Body.Close() //nolint:errcheck // test read
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET %s = %d, want 405", webShotPath, res.StatusCode)
	}
	if got := rec.calls.Load(); got != 0 {
		t.Errorf("GET ran the capture %d time(s), want 0", got)
	}
}

// TestWebShotRefusesWithoutTheToken is the GATE-PARITY check: a non-loopback
// bind is served behind webTokenGate, and the SAME wrapper must refuse
// /api/shot — otherwise the new route would be a token bypass in front of a
// tool-executing agent.
//
// The wrapper is the one runWeb installs: webTokenGate(token, mux, localTrust).
// A request carrying no token is refused with 401 and the capture never runs.
func TestWebShotRefusesWithoutTheToken(t *testing.T) {
	rec := withShotSeam(t, webShotTestPNG, nil)
	ws, _ := shotTestServer(t)
	const token = "test-token-0123456789"
	// localTrust=false: the request is treated as remote, which is what a LAN
	// peer is — the strictest reading of the gate.
	srv := httptest.NewServer(webTokenGate(token, ws.webChatHandler(), false))
	defer srv.Close()

	res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST %s without a token: %v", webShotPath, err)
	}
	defer res.Body.Close()          //nolint:errcheck // test read
	body, _ := io.ReadAll(res.Body) //stylegate:ignore bounded test read of a small error body
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST %s without a token = %d, want 401 — body: %s", webShotPath, res.StatusCode, body)
	}
	if got := rec.calls.Load(); got != 0 {
		t.Errorf("an unauthenticated request ran the capture %d time(s), want 0", got)
	}

	// WITH the token the same request is served — the gate is a gate, not a
	// closed door (so this test also proves the route is reachable when
	// authorized, which is what makes the 401 meaningful).
	req, err := http.NewRequest(http.MethodPost, srv.URL+webShotPath, strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("building the authorized request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	ok, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s with a token: %v", webShotPath, err)
	}
	defer ok.Body.Close() //nolint:errcheck // test read
	if ok.StatusCode != http.StatusOK {
		t.Errorf("POST %s with a valid token = %d, want 200", webShotPath, ok.StatusCode)
	}
	if got := rec.calls.Load(); got != 1 {
		t.Errorf("the authorized capture ran %d time(s), want 1", got)
	}
}

// TestWebShotSurfacesACaptureFailure is Rule 15 on this route: a capture that
// fails answers a NON-2xx with the reason in the body — never an empty 200 the
// page would render as a broken thumbnail.
func TestWebShotSurfacesACaptureFailure(t *testing.T) {
	const boom = "shot: navigation to http://127.0.0.1:1/ failed: net::ERR_CONNECTION_REFUSED"
	recorded := withShotSeam(t, nil, errShotStub{msg: boom})
	ws, _ := shotTestServer(t)
	srv := httptest.NewServer(ws.webChatHandler())
	defer srv.Close()

	res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST %s: %v", webShotPath, err)
	}
	defer res.Body.Close()          //nolint:errcheck // test read
	body, _ := io.ReadAll(res.Body) //stylegate:ignore bounded test read of a small error body
	if res.StatusCode == http.StatusOK {
		t.Fatalf("a failed capture answered 200 — body: %s", body)
	}
	if !strings.Contains(string(body), "ERR_CONNECTION_REFUSED") {
		t.Errorf("the failure body does not carry the capture's own reason — body: %s", body)
	}
	if recorded.calls.Load() != 1 {
		t.Errorf("the capture ran %d time(s), want 1", recorded.calls.Load())
	}

	// An EMPTY capture (no error) is a failure too, not a zero-byte picture.
	withShotSeam(t, nil, nil)
	res2, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST %s: %v", webShotPath, err)
	}
	defer res2.Body.Close() //nolint:errcheck // test read
	if res2.StatusCode == http.StatusOK {
		t.Errorf("a zero-length capture answered 200, want a loud failure (Rule 15)")
	}
}

// TestWebShotRefusesAnAddressThatIsNotItsOwn is the target-proof gate: an s.addr
// that is not loopback (or no longer listening) is refused BY NAME before any
// browser starts — the endpoint never navigates to a host it does not run.
func TestWebShotRefusesAnAddressThatIsNotItsOwn(t *testing.T) {
	cases := []struct{ name, addr string }{
		{name: "lan-address", addr: "192.0.2.9:8090"},
		{name: "no-port", addr: "127.0.0.1"},
		{name: "closed-loopback-port", addr: "127.0.0.1:1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := withShotSeam(t, webShotTestPNG, nil)
			ws := &webServer{addr: tc.addr}
			srv := httptest.NewServer(ws.webChatHandler())
			defer srv.Close()

			res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("POST %s: %v", webShotPath, err)
			}
			defer res.Body.Close() //nolint:errcheck // test read
			if res.StatusCode == http.StatusOK {
				t.Errorf("s.addr=%q was accepted, want a refusal (the endpoint captures only its own loopback listener)", tc.addr)
			}
			if got := rec.calls.Load(); got != 0 {
				t.Errorf("s.addr=%q still ran the capture %d time(s), want 0", tc.addr, got)
			}
		})
	}
}

// errShotStub is a capture error the test controls.
type errShotStub struct{ msg string }

func (e errShotStub) Error() string { return e.msg }

// mustHost splits a host:port and fails the test when it cannot.
func mustHost(t *testing.T, addr string) string {
	t.Helper()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("addr %q is not host:port: %v", addr, err)
	}
	return host
}

// TestWebShotViewportReachesTheCapture is the GEOMETRY half of the endpoint:
// {"viewport":{"w":900,"h":900}} must reach internal/shot's Options unchanged,
// because that pair is what makes the picture match the operator's window
// instead of the 1440x2800 default.
func TestWebShotViewportReachesTheCapture(t *testing.T) {
	cases := []struct {
		name string
		body string
		w, h int
	}{
		// The pre-existing caller: no body at all. The endpoint RESOLVES the
		// default here rather than passing 0 through — the value it hands
		// internal/shot is exactly what shot.Options would have defaulted to, so
		// the picture is unchanged (1440x2800) and the response's echo can report
		// a real number instead of "0 means whatever the library decided".
		{name: "no-body", body: "", w: shot.DefaultViewportWidth, h: shot.DefaultViewportHeight},
		{name: "empty-object", body: `{}`, w: shot.DefaultViewportWidth, h: shot.DefaultViewportHeight},
		{name: "absent-viewport-means-default", body: `{"viewport":{}}`, w: shot.DefaultViewportWidth, h: shot.DefaultViewportHeight},
		// The feature: a real window size passes straight through.
		{name: "square", body: `{"viewport":{"w":900,"h":900}}`, w: 900, h: 900},
		{name: "wide", body: `{"viewport":{"w":1440,"h":900}}`, w: 1440, h: 900},
		// CLAMPED, not refused (Rule 15: the caller is told, in the response).
		{name: "too-small", body: `{"viewport":{"w":10,"h":10}}`, w: webShotMinViewport, h: webShotMinViewport},
		{name: "too-large", body: `{"viewport":{"w":999999,"h":999999}}`, w: webShotMaxViewport, h: webShotMaxViewport},
		{name: "mixed", body: `{"viewport":{"w":50,"h":999999}}`, w: webShotMinViewport, h: webShotMaxViewport},
		// One dimension requested, the other left to the default.
		{name: "half-specified", body: `{"viewport":{"w":900}}`, w: 900, h: shot.DefaultViewportHeight},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := withShotSeam(t, webShotTestPNG, nil)
			ws, _ := shotTestServer(t)
			srv := httptest.NewServer(ws.webChatHandler())
			defer srv.Close()

			res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("POST %s: %v", webShotPath, err)
			}
			defer res.Body.Close()          //nolint:errcheck // test read
			body, _ := io.ReadAll(res.Body) //stylegate:ignore bounded test read
			if res.StatusCode != http.StatusOK {
				t.Fatalf("POST with %s = %d, want 200 — body: %s", tc.body, res.StatusCode, body)
			}
			if rec.opts.ViewportW != tc.w || rec.opts.ViewportH != tc.h {
				t.Errorf("the capture asked for %dx%d, want %dx%d (body %s)",
					rec.opts.ViewportW, rec.opts.ViewportH, tc.w, tc.h, tc.body)
			}
			// The boundary is unchanged no matter what the geometry says.
			if !rec.opts.Self || rec.opts.Surface != shot.SurfaceWeb || rec.opts.URL != "" {
				t.Errorf("a viewport request moved the TARGET (self=%v surface=%q url=%q) — geometry must never affect what is captured",
					rec.opts.Self, rec.opts.Surface, rec.opts.URL)
			}
		})
	}
}

// TestWebShotEchoesTheClampedGeometry pins the Rule 15 half of a clamp: a request
// that asked for a size it did not get is TOLD, in its own response, instead of
// having to be diagnosed from the PNG afterwards.
func TestWebShotEchoesTheClampedGeometry(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		w, h        int
		askedW      int
		askedH      int
		wantClamped bool
	}{
		{name: "exact", body: `{"viewport":{"w":900,"h":900}}`, w: 900, h: 900},
		{name: "clamped-low", body: `{"viewport":{"w":10,"h":10}}`, w: webShotMinViewport, h: webShotMinViewport, askedW: 10, askedH: 10, wantClamped: true},
		{name: "clamped-high", body: `{"viewport":{"w":99999,"h":900}}`, w: webShotMaxViewport, h: 900, askedW: 99999, wantClamped: true},
		{name: "no-body", body: "", w: shot.DefaultViewportWidth, h: shot.DefaultViewportHeight},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withShotSeam(t, webShotTestPNG, nil)
			ws, _ := shotTestServer(t)
			srv := httptest.NewServer(ws.webChatHandler())
			defer srv.Close()

			res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("POST %s: %v", webShotPath, err)
			}
			defer res.Body.Close()          //nolint:errcheck // test read
			body, _ := io.ReadAll(res.Body) //stylegate:ignore bounded test read
			var got webShotResponse
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("response is not the documented shape: %v — %s", err, body)
			}
			if got.Capture.W != tc.w || got.Capture.H != tc.h {
				t.Errorf("capture echo = %dx%d, want %dx%d", got.Capture.W, got.Capture.H, tc.w, tc.h)
			}
			if got.Capture.Clamped != tc.wantClamped {
				t.Errorf("capture.clamped = %v, want %v (body %s)", got.Capture.Clamped, tc.wantClamped, tc.body)
			}
			if got.Capture.AskedW != tc.askedW || got.Capture.AskedH != tc.askedH {
				t.Errorf("capture asked = %dx%d, want %dx%d", got.Capture.AskedW, got.Capture.AskedH, tc.askedW, tc.askedH)
			}
			// The image half must stay byte-identical to /api/chat's contract.
			if _, err := validateWebImage(got.Image); err != nil {
				t.Errorf("the echoed image stopped satisfying validateWebImage: %v", err)
			}
		})
	}
}

// TestWebShotRefusesAMalformedViewport is the Rule 15 half of the parser: a
// geometry the endpoint cannot read is REFUSED by name, never silently defaulted
// (a caller that sent "900x900" asked for a size and must learn it was not
// understood, not get a 1440x2800 picture believing it got what it asked for).
func TestWebShotRefusesAMalformedViewport(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{name: "string", body: `{"viewport":"900x900"}`, want: "viewport"},
		{name: "number", body: `{"viewport":900}`, want: "viewport"},
		{name: "fractional", body: `{"viewport":{"w":900.5,"h":900}}`, want: "viewport"},
		{name: "null-viewport-ok-is-refused-by-type", body: `{"viewport":[1,2]}`, want: "viewport"},
		{name: "extra-key", body: `{"viewport":{"w":900,"h":900},"url":"http://example.com/"}`, want: "url"},
		{name: "surface-still-refused", body: `{"surface":"screen"}`, want: "surface"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := withShotSeam(t, webShotTestPNG, nil)
			ws, _ := shotTestServer(t)
			srv := httptest.NewServer(ws.webChatHandler())
			defer srv.Close()

			res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("POST %s: %v", webShotPath, err)
			}
			defer res.Body.Close()          //nolint:errcheck // test read
			body, _ := io.ReadAll(res.Body) //stylegate:ignore bounded test read
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("POST %s = %d, want 400 — body: %s", tc.body, res.StatusCode, body)
			}
			if !strings.Contains(string(body), tc.want) {
				t.Errorf("the refusal does not name %q — body: %s", tc.want, body)
			}
			if got := rec.calls.Load(); got != 0 {
				t.Errorf("a malformed body still ran the capture %d time(s) — a refusal must cost no browser", got)
			}
		})
	}
}
