package shot

// capture_test.go — boundary + error-path tests for Capture (shot.go:93), the
// only exported function of internal/shot without a test (func_coverage.py
// reported 50.0%: Capture uncovered).
//
// Capture is unexported-helper glue: resolveURL → findChrome → exec chrome →
// waitDevToolsPort → captureOverCDP. To exercise it end-to-end WITHOUT a real
// browser (the host's chromium is a snap that cannot start outside the snap
// sandbox — see devdoc/cov-shot-hp.md §Environment) the tests inject:
//
//  1. GOPI_SHOT_CHROME → a generated POSIX-shell stub that impersonates
//     chrome's stderr "DevTools listening on ws://…" handshake, and
//  2. an in-process fake CDP server (HTTP /json/list + WebSocket) reused from
//     the same-package helpers.
//
// That covers Capture's real control flow (temp profile, StderrPipe, Start,
// port parse, defer kill/cleanup) while keeping the suite deterministic and
// dependency-free. No product code is modified: everything here works through
// documented seams (the env var, Options, and SelfPageProvider).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/heavytest"
)

// Named constants (Rule 1) — no magic literals in this file.
const (
	// testPNGMagic is the 8-byte PNG signature captureOverCDP validates.
	testPNGMagic = "\x89PNG\r\n\x1a\n"

	// testPNGFiller bytes follow the magic so the payload looks like a body.
	testPNGFiller = 64

	// testNonPNGData is a well-formed non-PNG image header ("GIF89a…"): the
	// payload must be rejected by the byte-check, not by a decoder error.
	testNonPNGData = "GIF89a not a png at all"

	// stubChromeUUID is the [0-9a-f-]+ segment wsURLPattern requires.
	stubChromeUUID = "00000000-0000-0000-0000-000000000000"

	// stubChromePortLine is the stderr line chrome prints once CDP is up;
	// %s is the port (the fake CDP server's port in these tests).
	stubChromePortLine = "DevTools listening on ws://127.0.0.1:%s/devtools/browser/" + stubChromeUUID

	// stubChromeArgsFile records the argv Capture launched chrome with, so a
	// test can assert the headless flag set and the temp-profile cleanup.
	stubChromeArgsFile = "stub-chrome-args.txt"

	// stubChromeHangSeconds keeps a silent stub alive well past
	// chromeStartTimeout; Capture's deferred cancel() reaps it.
	stubChromeHangSeconds = 30

	// fakeCDPMaxEntries bounds the fake server's accept loop: one HTTP
	// discovery + one WebSocket upgrade, plus headroom for reconnects.
	fakeCDPMaxEntries = 64

	// waitBoundaryTolerance absorbs scheduler jitter in elapsed-time asserts.
	waitBoundaryTolerance = 300 * time.Millisecond
)

// ---------------------------------------------------------------------------
// stub chrome binary
// ---------------------------------------------------------------------------

// stubChrome writes an executable shell stub that impersonates chrome: body is
// appended after the shebang. Returns its path (usable as GOPI_SHOT_CHROME).
func stubChrome(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stub-chrome")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// portLineBody is the stub body that reports the DevTools line for port,
// records its argv to argsFile (when non-empty), then idles until killed.
func portLineBody(port string, argsFile string) string {
	line := fmt.Sprintf(stubChromePortLine, port)
	body := ""
	if argsFile != "" {
		body += fmt.Sprintf("printf '%%s\\n' \"$@\" > '%s'\n", argsFile)
	}
	body += fmt.Sprintf("printf '%%s\\n' '%s' 1>&2\n", line)
	body += fmt.Sprintf("exec sleep %d\n", stubChromeHangSeconds)
	return body
}

// ---------------------------------------------------------------------------
// fake CDP server (records methods + params)
// ---------------------------------------------------------------------------

// cdpCall is one recorded CDP request.
type cdpCall struct {
	method string
	params map[string]any
}

// cdpRecorder is a thread-safe log of the CDP calls a fake server received,
// plus the canned results it answers with.
type cdpRecorder struct {
	mu       sync.Mutex
	calls    []cdpCall
	shot     []byte // Page.captureScreenshot payload (base64-encoded on the wire)
	selector bool   // Runtime.evaluate result value

	// navErrorText, when set, is the errorText Page.navigate answers with —
	// i.e. the navigation FAILED (probe-verified CDP shape:
	// {"loaderId":"…","errorText":"net::ERR_FILE_NOT_FOUND"}). Empty means
	// Chromium accepted the navigation.
	navErrorText string
	// probeAnswer, when set, is what the error-document probe's
	// Runtime.evaluate answers — the raw serialised value, so a test can feed
	// BOTH the error document's real shape and the shapes a page's own
	// JavaScript would produce instead. Empty → a healthy page's answer.
	probeAnswer string
	// navCount counts Page.navigate calls, so a test can assert the detection
	// did (or did not) re-issue the navigation.
	navCount int
	// pageText is the error page's own innerText (the operator-facing detail).
	pageText string
}

func newCDPRecorder(shot []byte) *cdpRecorder {
	// Default to a HEALTHY page: a test that does not care about the
	// error-document probe (the selector, viewport and happy-path tests) must
	// not accidentally answer the probe with the --wait-selector boolean, which
	// would make every capture look like a failure. A test that needs a
	// failure calls setProbeAnswer/setNavigation.
	return &cdpRecorder{shot: shot, selector: true, probeAnswer: healthyProbeAnswer}
}

// healthyProbeAnswer is what the error-document probe sees on an ordinary
// page: a non-error protocol, so the capture is never failed.
const healthyProbeAnswer = `{"protocol":"file:","loaded":true,"text":"OK"}`

// setNavigation sets the Page.navigate errorText answer ("" = success).
func (r *cdpRecorder) setNavigation(errorText string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.navErrorText = errorText
}

// setProbeAnswer sets the raw answer to the error-document probe.
func (r *cdpRecorder) setProbeAnswer(answer string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probeAnswer = answer
}

func (r *cdpRecorder) add(method string, raw json.RawMessage) {
	params := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &params) //stylegate:ignore best-effort decode of an optional field; the zero value is the documented fallback//nolint:errcheck // a malformed param is asserted via paramsOf
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, cdpCall{method: method, params: params})
}

// resultFor is the canned CDP result object for a method. The evaluate answers
// are keyed off the EXPRESSION the product sent (the product's own constants),
// so a test's answer lands on the probe it was written for and a probe rename
// cannot leave the fake silently answering the wrong call.
func (r *cdpRecorder) resultFor(method string, params map[string]any) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch method {
	case "Page.captureScreenshot":
		return map[string]any{"data": base64Std(r.shot)}
	case "Runtime.evaluate":
		return map[string]any{"result": map[string]any{"value": r.evaluateValue(params)}}
	case "Page.navigate":
		r.navCount++
		if r.navErrorText != "" {
			// The exact shape the probe measured over a real CDP session.
			return map[string]any{
				"frameId":   "FAKEFRAME",
				"loaderId":  "FAKELOADER",
				"errorText": r.navErrorText,
			}
		}
		return map[string]any{"frameId": "FAKEFRAME", "loaderId": "FAKELOADER"}
	default:
		return map[string]any{}
	}
}

// evaluateValue is the answer to Runtime.evaluate: the expression decides which
// seam is being asked, so the fake is what the real protocol would carry for
// that call.
func (r *cdpRecorder) evaluateValue(params map[string]any) any {
	expr, _ := params["expression"].(string)
	switch expr {
	case errorDocumentProbeExpr:
		if r.probeAnswer != "" {
			return r.probeAnswer
		}
		return healthyProbeAnswer
	case errorPageTextExpr:
		return r.pageText
	default:
		return r.selector
	}
}

// setSelector flips the Runtime.evaluate answer (call before starting a test's
// server; the field is only read from the server goroutine).
func (r *cdpRecorder) setSelector(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selector = v
}

func (r *cdpRecorder) called(method string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if c.method == method {
			return true
		}
	}
	return false
}

// paramsOf returns the params of the LAST call to method (nil if never called).
func (r *cdpRecorder) paramsOf(method string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.calls) - 1; i >= 0; i-- {
		if r.calls[i].method == method {
			return r.calls[i].params
		}
	}
	return nil
}

func (r *cdpRecorder) countOf(method string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c.method == method {
			n++
		}
	}
	return n
}

// fakeCDPServerRecorded serves the two CDP halves (HTTP /json/list + WebSocket)
// on one listener, for several sequential connections, recording every call.
func fakeCDPServerRecorded(t *testing.T, rec *cdpRecorder) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	//stylegate:ignore test cleanup: the fake CDP listener is closed at teardown so the accept loop unwinds; a close error cannot leave the test with a reportable failure
	t.Cleanup(func() { ln.Close() })
	go func() {
		for i := 0; i < fakeCDPMaxEntries; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveRecordedConn(conn, rec)
		}
	}()
	return ln.Addr().String()
}

// serveRecordedConn demuxes HTTP discovery vs WebSocket upgrade by header.
func serveRecordedConn(conn net.Conn, rec *cdpRecorder) {
	defer conn.Close()
	buf := make([]byte, 8192)
	n, _ := conn.Read(buf) //stylegate:ignore reads from the fixture connection; the bytes read are the assertion
	req := string(buf[:n])
	if strings.Contains(req, "Upgrade: websocket") {
		const marker = "Sec-WebSocket-Key: "
		i := strings.Index(req, marker)
		if i < 0 {
			return
		}
		key := req[i+len(marker):]
		if j := strings.Index(key, "\r\n"); j >= 0 {
			key = key[:j]
		}
		resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + base64Std(sha1Sum(key+wsGUID)) + "\r\n\r\n"
		conn.Write([]byte(resp)) //nolint:errcheck // best-effort test peer
		serveRecordedWS(conn, rec)
		return
	}
	_, portStr, _ := net.SplitHostPort(conn.LocalAddr().String()) //stylegate:ignore splits the fixture listener's own address; the derived port is the assertion
	body := `[{"id":"T1","type":"page","url":"about:blank","webSocketDebuggerUrl":"ws://127.0.0.1:` + portStr + `/devtools/page/T1"}]`
	conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: " + //nolint:errcheck
		itoa(len(body)) + "\r\n\r\n" + body))
}

// serveRecordedWS answers CDP frames, recording method + params.
func serveRecordedWS(conn net.Conn, rec *cdpRecorder) {
	c := &cdpConn{conn: conn, br: newBufio(conn)}
	for {
		opcode, payload, err := c.readFrame()
		if err != nil {
			return
		}
		if opcode != wsOpcodeText {
			continue
		}
		var msg struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if jsonUnmarshal(payload, &msg) != nil || msg.ID == 0 {
			continue
		}
		rec.add(msg.Method, msg.Params)
		var params map[string]any
		_ = jsonUnmarshal(msg.Params, &params) //stylegate:ignore best-effort decode for the fake's answer lookup; the zero value selects the default answer
		resp, _ := jsonMarshal(map[string]any{"id": msg.ID, "result": rec.resultFor(msg.Method, params)})
		if c.writeTextFrame(resp) != nil {
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Capture end-to-end through an injected chrome stub
// ---------------------------------------------------------------------------

// stubPNG returns a magic-prefixed fake PNG body.
func stubPNG() []byte { return append([]byte(testPNGMagic), make([]byte, testPNGFiller)...) }

// TestCaptureHappyPathStubChrome drives Capture's full control flow with a
// stubbed chrome binary: resolveURL → findChrome (env) → temp profile →
// StderrPipe/Start → waitDevToolsPort → CDP capture → PNG, plus the deferred
// process kill and temp-profile removal.
func TestCaptureHappyPathStubChrome(t *testing.T) {
	png := stubPNG()
	rec := newCDPRecorder(png)
	port := portOnly(fakeCDPServerRecorded(t, rec))

	argsFile := filepath.Join(t.TempDir(), stubChromeArgsFile)
	t.Setenv(chromeEnvVar, stubChrome(t, portLineBody(port, argsFile)))

	got, err := Capture(contextBackground(), Options{URL: "data:text/html,<h1>stub</h1>", WaitSeconds: 0})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if !bytes.Equal(got, png) {
		t.Fatalf("Capture returned %d bytes, want the %d-byte stub PNG", len(got), len(png))
	}
	for _, method := range []string{"Page.enable", "Emulation.setDeviceMetricsOverride", "Page.navigate", "Page.captureScreenshot"} {
		if !rec.called(method) {
			t.Errorf("CDP method %s never reached the server", method)
		}
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read recorded argv: %v", err)
	}
	argv := string(raw)
	wantFlags := []string{
		"--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
		"--remote-debugging-port=0", "--user-data-dir=", "about:blank",
	}
	for _, want := range wantFlags {
		if !strings.Contains(argv, want) {
			t.Errorf("chrome argv missing %q\nargv: %s", want, argv)
		}
	}
	// The throwaway profile must be gone (defer os.RemoveAll(profile)).
	for _, line := range strings.Split(argv, "\n") {
		const prefix = "--user-data-dir="
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if _, err := os.Stat(strings.TrimPrefix(line, prefix)); !os.IsNotExist(err) {
			t.Errorf("temp profile %q survived Capture (stat err=%v)", line, err)
		}
	}
}

// TestCaptureSelfWithProvider — the --self branch resolved INSIDE Capture: a
// temporary static server hosts the injected page and its URL is what chrome
// is told to navigate to.
func TestCaptureSelfWithProvider(t *testing.T) {
	saved := SelfPageProvider
	SelfPageProvider = func() string { return "<html>SELF-STUB-PAGE</html>" }
	defer func() { SelfPageProvider = saved }()

	rec := newCDPRecorder(stubPNG())
	port := portOnly(fakeCDPServerRecorded(t, rec))
	t.Setenv(chromeEnvVar, stubChrome(t, portLineBody(port, "")))

	if _, err := Capture(contextBackground(), Options{Self: true, WaitSeconds: 0}); err != nil {
		t.Fatalf("Capture(--self): %v", err)
	}
	nav := rec.paramsOf("Page.navigate")
	if nav == nil {
		t.Fatal("Page.navigate never called")
	}
	gotURL, _ := nav["url"].(string)
	if !strings.HasPrefix(gotURL, "http://127.0.0.1:") {
		t.Fatalf("--self navigate url = %q, want the temp 127.0.0.1 server", gotURL)
	}
}

// TestCaptureEmptyURL — nothing to shoot is a local error: Capture returns
// before chrome discovery (no stub needed).
func TestCaptureEmptyURL(t *testing.T) {
	_, err := Capture(contextBackground(), Options{})
	if err == nil || !strings.Contains(err.Error(), "nothing to shoot") {
		t.Fatalf("empty Options: want the nothing-to-shoot error, got %v", err)
	}
}

// TestCaptureSelfMissingProvider — --self without the wiring errors at
// resolve time, again before chrome discovery.
func TestCaptureSelfMissingProvider(t *testing.T) {
	saved := SelfPageProvider
	SelfPageProvider = nil
	defer func() { SelfPageProvider = saved }()
	_, err := Capture(contextBackground(), Options{Self: true})
	if err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("--self without provider: got %v", err)
	}
}

// TestCaptureMissingChrome — both flavours of "no chrome": an explicit bogus
// override, and a host where the whole detection chain comes up empty.
func TestCaptureMissingChrome(t *testing.T) {
	t.Run("bogus override", func(t *testing.T) {
		t.Setenv(chromeEnvVar, filepath.Join(t.TempDir(), "no-such-chrome"))
		_, err := Capture(contextBackground(), Options{URL: "example.com"})
		if err == nil || !strings.Contains(err.Error(), chromeEnvVar) {
			t.Fatalf("want an error naming %s, got %v", chromeEnvVar, err)
		}
	})
	t.Run("override is a directory", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(chromeEnvVar, dir)
		_, err := Capture(contextBackground(), Options{URL: "example.com"})
		want := fmt.Sprintf(errNotExecutableFile, chromeEnvVar, dir)
		if err == nil || err.Error() != want {
			t.Fatalf("directory override: err = %v, want %q", err, want)
		}
	})
	t.Run("chain exhausted", func(t *testing.T) {
		t.Setenv(chromeEnvVar, "")
		t.Setenv("PATH", "")          // no PATH candidates
		t.Setenv("HOME", t.TempDir()) // no playwright cache
		_, err := Capture(contextBackground(), Options{URL: "example.com"})
		if err == nil || !strings.Contains(err.Error(), msgNoChromeFound) {
			t.Fatalf("want the no-headless-chrome error, got %v", err)
		}
	})
}

// TestCaptureChromeStartFailure — the original bug repro (bug 01a08c89): an
// override that exists but carries no execute bit (chmod 644) is rejected AT
// DISCOVERY with the actionable "is not an executable file" message. It must
// NOT reach chrome start, where the failure was opaque
// ("shot: start …: permission denied", devdoc/cov-shot-hp.md §7 finding 1).
func TestCaptureChromeStartFailure(t *testing.T) {
	notExec := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(notExec, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(chromeEnvVar, notExec)
	_, err := Capture(contextBackground(), Options{URL: "example.com"})
	want := fmt.Sprintf(errNotExecutableFile, chromeEnvVar, notExec)
	if err == nil {
		t.Fatalf("0644 override accepted; want %q", want)
	}
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err, want)
	}
	if strings.Contains(err.Error(), "shot: start") {
		t.Fatalf("failure deferred to chrome start instead of discovery: %v", err)
	}
}

// TestCaptureChromeExitsWithoutPort — a chrome that dies silently must surface
// the "exited without printing a DevTools port" error instead of blocking.
func TestCaptureChromeExitsWithoutPort(t *testing.T) {
	t.Setenv(chromeEnvVar, stubChrome(t, "exit 1\n"))
	_, err := Capture(contextBackground(), Options{URL: "example.com"})
	if err == nil || !strings.Contains(err.Error(), "without printing a DevTools port") {
		t.Fatalf("want the exited-without-port error, got %v", err)
	}
}

// TestCaptureChromeNeverPrintsPort — the other half of that boundary: a stub
// that stays alive but never prints the line must hit chromeStartTimeout.
// Slow by construction (the timeout IS the boundary); skipped by -short.
func TestCaptureChromeNeverPrintsPort(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	if testing.Short() {
		t.Skip("waits out chromeStartTimeout")
	}
	t.Setenv(chromeEnvVar, stubChrome(t, fmt.Sprintf("exec sleep %d\n", stubChromeHangSeconds)))
	start := time.Now()
	_, err := Capture(contextBackground(), Options{URL: "example.com"})
	if err == nil || !strings.Contains(err.Error(), "did not report a DevTools port") {
		t.Fatalf("want the port-timeout error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < chromeStartTimeout {
		t.Fatalf("returned after %s, before chromeStartTimeout=%s", elapsed, chromeStartTimeout)
	}
}

// TestCaptureNonPNGResponse — a non-PNG screenshot payload must fail the
// capture loudly (the #1136 empty-shell guard), through Capture.
func TestCaptureNonPNGResponse(t *testing.T) {
	rec := newCDPRecorder([]byte(testNonPNGData))
	port := portOnly(fakeCDPServerRecorded(t, rec))
	t.Setenv(chromeEnvVar, stubChrome(t, portLineBody(port, "")))

	_, err := Capture(contextBackground(), Options{URL: "http://example/", WaitSeconds: 0})
	if err == nil || !strings.Contains(err.Error(), "not a PNG") {
		t.Fatalf("want the not-a-PNG error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// captureOverCDP boundaries (no chrome process involved)
// ---------------------------------------------------------------------------

// TestCaptureOverCDPNoPageTarget — a discovery endpoint with no page target is
// a hard error, not an empty screenshot.
func TestCaptureOverCDPNoPageTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "[]") //nolint:errcheck // test handler
	}))
	defer srv.Close()
	port := portOnly(strings.TrimPrefix(srv.URL, "http://"))

	_, err := captureOverCDP(port, "http://example/", Options{WaitSeconds: 0}, prepared{})
	if err == nil || !strings.Contains(err.Error(), "no page target") {
		t.Fatalf("want the no-page-target error, got %v", err)
	}
}

// TestCaptureViewportOverride — viewport 0 (and a negative) fall back to the
// documented defaults; an explicit size is passed through verbatim.
func TestCaptureViewportOverride(t *testing.T) {
	cases := []struct {
		name         string
		w, h         int
		wantW, wantH float64
	}{
		{"zero uses defaults", 0, 0, float64(DefaultViewportWidth), float64(DefaultViewportHeight)},
		{"negative uses defaults", -5, -1, float64(DefaultViewportWidth), float64(DefaultViewportHeight)},
		{"explicit passes through", 800, 600, 800, 600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newCDPRecorder(stubPNG())
			port := portOnly(fakeCDPServerRecorded(t, rec))
			if _, err := captureOverCDP(port, "http://example/", Options{
				ViewportW: tc.w, ViewportH: tc.h, WaitSeconds: 0,
			}, prepared{}); err != nil {
				t.Fatalf("captureOverCDP: %v", err)
			}
			p := rec.paramsOf("Emulation.setDeviceMetricsOverride")
			if p == nil {
				t.Fatal("setDeviceMetricsOverride never called")
			}
			if gotW, _ := p["width"].(float64); gotW != tc.wantW {
				t.Errorf("width = %v, want %v", p["width"], tc.wantW)
			}
			if gotH, _ := p["height"].(float64); gotH != tc.wantH {
				t.Errorf("height = %v, want %v", p["height"], tc.wantH)
			}
		})
	}
}

// TestCaptureWaitSecondsBoundaries — WaitSeconds -1 means the documented
// default wait; 0 means "navigate + settle only". The elapsed time is the only
// observable difference, so it is the assertion.
func TestCaptureWaitSecondsBoundaries(t *testing.T) {
	defaultWait := navSettle + time.Duration(WaitDefault)*time.Second
	t.Run("minus one uses WaitDefault", func(t *testing.T) {
		rec := newCDPRecorder(stubPNG())
		port := portOnly(fakeCDPServerRecorded(t, rec))
		start := time.Now()
		if _, err := captureOverCDP(port, "http://example/", Options{WaitSeconds: -1}, prepared{}); err != nil {
			t.Fatalf("captureOverCDP: %v", err)
		}
		if elapsed := time.Since(start); elapsed < defaultWait-waitBoundaryTolerance {
			t.Fatalf("elapsed %s < default wait %s — WaitSeconds=-1 did not wait", elapsed, defaultWait)
		}
	})
	t.Run("zero skips the extra wait", func(t *testing.T) {
		rec := newCDPRecorder(stubPNG())
		port := portOnly(fakeCDPServerRecorded(t, rec))
		start := time.Now()
		if _, err := captureOverCDP(port, "http://example/", Options{WaitSeconds: 0}, prepared{}); err != nil {
			t.Fatalf("captureOverCDP: %v", err)
		}
		if elapsed := time.Since(start); elapsed >= defaultWait {
			t.Fatalf("elapsed %s >= default wait %s — WaitSeconds=0 waited anyway", elapsed, defaultWait)
		}
	})
}

// TestCaptureWaitSelector — the selector gate both ways: a matching selector
// returns immediately, a never-matching one times out with the named error.
func TestCaptureWaitSelector(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	t.Run("matches", func(t *testing.T) {
		rec := newCDPRecorder(stubPNG())
		rec.setSelector(true)
		port := portOnly(fakeCDPServerRecorded(t, rec))
		if _, err := captureOverCDP(port, "http://example/", Options{
			WaitSeconds: 0, WaitSelector: "#chat-log",
		}, prepared{}); err != nil {
			t.Fatalf("captureOverCDP with a matching selector: %v", err)
		}
		if rec.countOf("Runtime.evaluate") == 0 {
			t.Error("Runtime.evaluate never polled")
		}
		if !rec.called("Page.captureScreenshot") {
			t.Error("capture did not proceed after the selector matched")
		}
	})
	t.Run("never matches", func(t *testing.T) {
		if testing.Short() {
			t.Skip("waits out WaitSelectorTimeout")
		}
		rec := newCDPRecorder(stubPNG())
		rec.setSelector(false)
		port := portOnly(fakeCDPServerRecorded(t, rec))
		start := time.Now()
		_, err := captureOverCDP(port, "http://example/", Options{
			WaitSeconds: 0, WaitSelector: "#never-appears",
		}, prepared{})
		if err == nil || !strings.Contains(err.Error(), "not found within") {
			t.Fatalf("want the selector-timeout error, got %v", err)
		}
		if elapsed := time.Since(start); elapsed < WaitSelectorTimeout {
			t.Fatalf("timed out after %s, before WaitSelectorTimeout=%s", elapsed, WaitSelectorTimeout)
		}
		if rec.called("Page.captureScreenshot") {
			t.Error("capture proceeded despite the selector timeout")
		}
	})
}

// TestCaptureBareHostnameGetsScheme — Capture (not just resolveURL) hands
// chrome an http:// URL for a bare hostname.
func TestCaptureBareHostnameGetsScheme(t *testing.T) {
	rec := newCDPRecorder(stubPNG())
	port := portOnly(fakeCDPServerRecorded(t, rec))
	t.Setenv(chromeEnvVar, stubChrome(t, portLineBody(port, "")))

	if _, err := Capture(contextBackground(), Options{URL: "example.com", WaitSeconds: 0}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	nav := rec.paramsOf("Page.navigate")
	if u, _ := nav["url"].(string); u != "http://example.com" {
		t.Fatalf("navigate url = %q, want http://example.com", u)
	}
}

// ---------------------------------------------------------------------------
// navigation-failure detection (the F2+F4 fix)
//
// THE DEFECT these tests hold down: `gopi tool shot --url <unreachable>` exited 0 and
// wrote a PNG of Chromium's error page, so the ONLY channel distinguishing a
// successful capture from a failed navigation was the pixels. captureOverCDP
// discarded Page.navigate's result (which carries CDP's errorText) and
// byte-checked only the PNG magic, so an error document was indistinguishable
// from the requested page on rc/stdout/PNG-shape.
//
// TWO failure shapes, because the error document is assembled asynchronously and
// EITHER can be what an operator actually hits:
//   A. Page.navigate reports errorText directly (measured: net::ERR_FILE_NOT_FOUND),
//      while the DOM probe still sees a healthy-looking document.
//   B. Page.navigate reports nothing, and only the DOM probe finds the error
//      document (the late-arriving case).
//
// Every assertion here is about the CONTRACT (typed-error class, named URL, the
// PNG still returned), never "a PNG was written" — that observation passed
// BEFORE the fix and is exactly what hid the defect.
// ---------------------------------------------------------------------------

// navFailureCases are the two detected failure shapes plus the page-side shapes
// that must NOT be read as failures.
const (
	// errTextFileNotFound is the measured CDP errorText for an unreachable
	// file:// URL (probe: chromium-1228, 2026-09-27).
	errTextFileNotFound = "net::ERR_FILE_NOT_FOUND"

	// errorDocProbeAnswer is the measured answer on an error page:
	// chrome-error://chromewebdata/ for location.protocol.
	errorDocProbeAnswer = `{"protocol":"chrome-error://chromewebdata/","loaded":true,"text":"无法访问您的文件 ERR_FILE_NOT_FOUND"}`

	// notOurObjectProbeAnswer is an answer that is NOT the object the probe's
	// expression produces — a bare string, e.g. a page supplying a verdict of
	// its own instead of the probe's. If it were trusted, a page could
	// suppress its own failure report; it must be refused as UNKNOWN.
	notOurObjectProbeAnswer = `"file:"`

	// otherNonObjectProbeAnswer is the same refusal for another non-object
	// shape (a number). Unknown, never a verdict.
	otherNonObjectProbeAnswer = `7`

	// errorPageTitleAndBody is the error document's own wording, which the
	// failure line quotes back to the operator.
	errorPageTitleAndBody = "无法访问您的文件 ERR_FILE_NOT_FOUND"

	// errorTextFieldNotFound is the reason the typed error must name when the
	// errorText probe is the one that fired.
	errorTextFieldNotFound = errTextFileNotFound
)

// TestCaptureOverCDPNavigationFailureIsTyped is the regression test for the
// defect: an error document must come back as a typed failure naming the URL
// and the error, WITH the PNG bytes still delivered.
func TestCaptureOverCDPNavigationFailureIsTyped(t *testing.T) {
	cases := []struct {
		name string
		// navError is the Page.navigate errorText ("" = success).
		navError string
		// probe is the raw answer to the error-document probe.
		probe string
		// wantReason is a substring the failure MUST name ("" = expect success).
		wantReason string
	}{
		{
			name:       "errorText shape — CDP reports the failure directly",
			navError:   errTextFileNotFound,
			probe:      healthyProbeAnswer,
			wantReason: errorTextFieldNotFound,
		},
		{
			name:       "a real error document is reported even without errorText",
			navError:   "",
			probe:      errorDocProbeAnswer,
			wantReason: errorPageURLPrefix,
		},
		{
			name:       "healthy page — nothing to report",
			navError:   "",
			probe:      healthyProbeAnswer,
			wantReason: "",
		},
		{
			name:       "an answer that is not our object is unknown, not a verdict",
			navError:   "",
			probe:      notOurObjectProbeAnswer,
			wantReason: "",
		},
		{
			name:       "a non-object answer is unknown too",
			navError:   "",
			probe:      otherNonObjectProbeAnswer,
			wantReason: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newCDPRecorder(stubPNG())
			rec.setNavigation(tc.navError)
			rec.setProbeAnswer(tc.probe)
			rec.pageText = errorPageTitleAndBody
			port := portOnly(fakeCDPServerRecorded(t, rec))

			got, err := captureOverCDP(port, "file:///definitely/not/here.html", Options{WaitSeconds: 0}, prepared{})

			if tc.wantReason == "" {
				if err != nil {
					t.Fatalf("healthy page reported as a failure: %v", err)
				}
				if !bytes.Equal(got, stubPNG()) {
					t.Fatalf("healthy capture returned %d bytes, want the stub PNG", len(got))
				}
				return
			}

			var navErr *NavigationFailure
			if !errors.As(err, &navErr) {
				t.Fatalf("failure returned as %T (%v), want *NavigationFailure — an error document must be "+
					"distinguishable on the control channel, not only in the pixels", err, err)
			}
			if navErr.URL != "file:///definitely/not/here.html" {
				t.Errorf("typed error names %q, want the failed URL", navErr.URL)
			}
			if !strings.Contains(err.Error(), "file:///definitely/not/here.html") {
				t.Errorf("error text %q does not name the URL", err.Error())
			}
			if !strings.Contains(navErr.Reason, tc.wantReason) {
				t.Errorf("reason %q does not carry %q", navErr.Reason, tc.wantReason)
			}
			// The PNG is NOT lost: the error page is evidence about the request.
			if !bytes.Equal(navErr.PNG, stubPNG()) {
				t.Errorf("typed error carries %d PNG bytes, want the captured %d", len(navErr.PNG), len(stubPNG()))
			}
			if !bytes.Equal(got, stubPNG()) {
				t.Errorf("capture returned %d bytes alongside the error, want the captured PNG", len(got))
			}
			if !rec.called("Page.captureScreenshot") {
				t.Error("the error page was never captured — the pixel evidence was lost")
			}
		})
	}
}

// TestCaptureErrorPageDetailIsQuoted — the error page's own wording reaches the
// operator, bounded, and an unreadable page costs nothing.
func TestCaptureErrorPageDetailIsQuoted(t *testing.T) {
	rec := newCDPRecorder(stubPNG())
	rec.setNavigation(errTextFileNotFound)
	rec.setProbeAnswer(errorDocProbeAnswer)
	rec.pageText = errorPageTitleAndBody
	port := portOnly(fakeCDPServerRecorded(t, rec))

	_, err := captureOverCDP(port, "file:///definitely/not/here.html", Options{WaitSeconds: 0}, prepared{})
	var navErr *NavigationFailure
	if !errors.As(err, &navErr) {
		t.Fatalf("want a typed navigation failure, got %v", err)
	}
	if !strings.Contains(navErr.Reason, expectedNetErrorPrefix) {
		t.Errorf("reason %q does not quote the error page's own ERR_* code", navErr.Reason)
	}

	t.Run("no readable text still reports the failure", func(t *testing.T) {
		rec2 := newCDPRecorder(stubPNG())
		rec2.setProbeAnswer(errorDocProbeAnswer)
		port2 := portOnly(fakeCDPServerRecorded(t, rec2))
		_, err := captureOverCDP(port2, "file:///x.html", Options{WaitSeconds: 0}, prepared{})
		var e2 *NavigationFailure
		if !errors.As(err, &e2) {
			t.Fatalf("want a typed navigation failure, got %v", err)
		}
		if !strings.Contains(e2.Reason, errorPageURLPrefix) {
			t.Errorf("reason %q must still name the error-page fact", e2.Reason)
		}
	})
}

// TestHealthyPageCostsOneNavigationAndOneProbe pins the COST contract the
// detection has to keep: a healthy page is navigated ONCE and established with
// a SINGLE probe. Both halves matter — a repeat navigate would abort a page that
// is still loading (its artifact, not the page's verdict), and a probe that does
// not stop on a loaded document would spend the whole navCheckWindow on every
// ordinary capture.
func TestHealthyPageCostsOneNavigationAndOneProbe(t *testing.T) {
	rec := newCDPRecorder(stubPNG())
	port := portOnly(fakeCDPServerRecorded(t, rec))

	start := time.Now()
	if _, err := captureOverCDP(port, "http://example/", Options{WaitSeconds: 0}, prepared{}); err != nil {
		t.Fatalf("captureOverCDP: %v", err)
	}
	if got := rec.countOf("Page.navigate"); got != 1 {
		t.Errorf("Page.navigate called %d times, want exactly 1 — the verdict rides in the FIRST "+
			"result, and a repeat navigate aborts a page still loading", got)
	}
	if got := rec.countOf("Runtime.evaluate"); got != 2 {
		t.Errorf("Runtime.evaluate called %d times, want 2 (the loaded-document probe that ends the "+
			"wait, plus the pre-capture guard)", got)
	}
	if elapsed := time.Since(start); elapsed >= navCheckWindow {
		t.Errorf("a loaded healthy page took %s — the wait must end on the first probe, not spend "+
			"navCheckWindow=%s", elapsed, navCheckWindow)
	}
	if !rec.called("Page.captureScreenshot") {
		t.Error("capture did not proceed")
	}
}

// TestExpectedNetErrorPrefixIsDerived is the machine check behind the two
// spellings of the SAME error code: CDP's errorText carries "net::ERR_…", and
// the error document's own text carries "ERR_…". A constant cannot call
// strings.TrimPrefix (Rule 1), so the derivation is asserted here — the code
// below is what a constant expression cannot express.
func TestExpectedNetErrorPrefixIsDerived(t *testing.T) {
	if want := strings.TrimPrefix(netErrorPrefix, "net::"); expectedNetErrorPrefix != want {
		t.Errorf("expectedNetErrorPrefix = %q, want %q (derived from netErrorPrefix=%q)",
			expectedNetErrorPrefix, want, netErrorPrefix)
	}
	if got := firstNetErrorRef("无法访问 ERR_FILE_NOT_FOUND, 请重试"); got != "ERR_FILE_NOT_FOUND" {
		t.Errorf("firstNetErrorRef = %q, want ERR_FILE_NOT_FOUND", got)
	}
	if got := firstNetErrorRef("nothing to see here"); got != "" {
		t.Errorf("firstNetErrorRef invented %q from text with no code", got)
	}
}

// TestNavigationFailureErrorShape pins the ONE-LINE contract the CLI prints.
func TestNavigationFailureErrorShape(t *testing.T) {
	e := &NavigationFailure{URL: "file:///nope.html", Reason: errTextFileNotFound, PNG: stubPNG()}
	msg := e.Error()
	if strings.ContainsAny(msg, "\n\r") {
		t.Errorf("the failure must render as ONE stderr line, got %q", msg)
	}
	for _, want := range []string{"shot: navigation to file:///nope.html failed", errTextFileNotFound, "error page"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, missing %q", msg, want)
		}
	}
}

// ---------------------------------------------------------------------------
// the REAL browser leg
// ---------------------------------------------------------------------------

// localFileURL spells a file:// URL for an absolute local path. It is the one
// place this suite builds one, so the URL shape cannot drift between the
// existing and unreachable cases.
func localFileURL(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return "file://" + abs
}

// TestCaptureRealChromeUnreachableURL is the regression test for the DEFECT on
// a REAL browser: `gopi tool shot --url <unreachable>` must come back as a typed
// failure carrying the error page's PNG, and the control (an existing file)
// must still capture normally.
//
// WHY THE REAL BROWSER IS REQUIRED HERE: the defect was invisible precisely
// because the fake-CDP tests above assert the PROTOCOL (errorText present, the
// probe answering chrome-error:). What a real Chromium decides to do with an
// unreachable URL — that it produces an error document at all, that
// location.protocol reads chrome-error:, that the PNG is a real capture — is a
// property of the browser, not of our client, and only this leg can observe it.
//
// It drives a real browser end to end, so it is HEAVY per Rule 27 and lives in
// testdata/heavy-tests.tsv: off the merge/verify path, run via `make test-heavy`.
func TestCaptureRealChromeUnreachableURL(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)

	bin, err := findChrome(homeDirOrEmpty())
	if err != nil {
		t.Skipf("no chrome on this host: %v", err)
	}
	_ = bin // findChrome's result is asserted by the capture itself

	t.Run("unreachable URL is a typed failure that still carries the PNG", func(t *testing.T) {
		home := t.TempDir()
		missing := filepath.Join(home, "definitely-not-here.html")
		if _, statErr := os.Stat(missing); statErr == nil {
			t.Fatalf("test precondition broken: %s exists", missing)
		}
		png, err := Capture(contextBackground(), Options{
			URL: localFileURL(t, missing), WaitSeconds: 0, Out: filepath.Join(home, "unreachable.png"),
		})
		var navErr *NavigationFailure
		if !errors.As(err, &navErr) {
			t.Fatalf("capturing an unreachable URL returned %T (%v), want *NavigationFailure — "+
				"the whole defect was that this looked like success", err, err)
		}
		if !strings.Contains(navErr.URL, filepath.Base(missing)) {
			t.Errorf("typed error names %q, want the requested URL", navErr.URL)
		}
		if !bytes.Equal(navErr.PNG, png) {
			t.Errorf("the failed capture returned %d bytes but the error carries %d — the pixel "+
				"evidence must survive the failure", len(png), len(navErr.PNG))
		}
		if !strings.HasPrefix(string(png), testPNGMagic) {
			t.Errorf("error page capture is not a PNG (%d bytes)", len(png))
		}
		if strings.Contains(navErr.Reason, errorPageURLPrefix) || strings.Contains(navErr.Reason, expectedNetErrorPrefix) {
			t.Logf("failure reason (measured on this browser): %s", navErr.Reason)
		} else {
			t.Errorf("reason %q names neither the error-page protocol (%s) nor an ERR_* code (%s)",
				navErr.Reason, errorPageURLPrefix, expectedNetErrorPrefix)
		}
	})

	t.Run("control — a page that exists still captures", func(t *testing.T) {
		home := t.TempDir()
		page := filepath.Join(home, "real-page.html")
		if err := os.WriteFile(page, []byte("<html><body><h1>REAL PAGE</h1></body></html>"), 0o644); err != nil {
			t.Fatal(err)
		}
		png, err := Capture(contextBackground(), Options{
			URL: localFileURL(t, page), WaitSeconds: 0, Out: filepath.Join(home, "real.png"),
		})
		if err != nil {
			t.Fatalf("capturing an EXISTING page failed: %v", err)
		}
		if !strings.HasPrefix(string(png), testPNGMagic) {
			t.Errorf("control capture is not a PNG (%d bytes)", len(png))
		}
	})
}
