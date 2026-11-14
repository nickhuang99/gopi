// shot.go — the `gopi tool shot` capture engine: launch (or reuse) a headless
// Chromium, navigate to a URL, wait for render, capture a PNG via raw CDP.
//
// Flow (probe-verified on HP chromium-1228, see devdoc/shot-design.md):
//  1. findChrome locates the binary.
//  2. chrome --headless=new --remote-debugging-port=0 --user-data-dir=TMP
//     prints "DevTools listening on ws://127.0.0.1:PORT/..." on stderr.
//  3. GET /json/list → the page target's webSocketDebuggerUrl.
//  4. Over WebSocket: Page.enable → Emulation.setDeviceMetricsOverride(viewport)
//     → Page.navigate(url) → wait (fixed delay and/or selector) →
//     Page.captureScreenshot{"format":"png"} → base64-decode → PNG bytes.
//  5. Chrome process is always killed (defer), tmp profile removed.
package shot

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// SelfPageProvider returns the HTML for --self captures. Injected by the
// cmd/agent wiring (the SPA lives there as an unexported const); nil with
// --self surfaces an error at resolve time. Exported so cmd/agent can set it.
var SelfPageProvider func() string

// selfPage is the safe accessor for the injected page HTML.
func selfPage() string {
	if SelfPageProvider == nil {
		return "<!DOCTYPE html><title>gopi tool shot</title><h1>gopi tool shot: page provider not wired</h1>"
	}
	return SelfPageProvider()
}

// Named constants (Rule 1) — no magic numbers below this comment block.
const (
	// DefaultViewport is the capture size (Epic suggestion: tall enough for
	// the chat UI without the 10000px+ token-truncation trap of #1136).
	DefaultViewportWidth  = 1440
	DefaultViewportHeight = 2800

	// WaitDefault seconds after navigation for SPA render.
	WaitDefault = 2

	// WaitSelectorTimeout bounds --wait-selector polling.
	WaitSelectorTimeout = 15 * time.Second

	// WaitSelectorInterval is the selector poll cadence.
	WaitSelectorInterval = 250 * time.Millisecond

	// chromeStartTimeout bounds how long we wait for the CDP endpoint.
	chromeStartTimeout = 15 * time.Second

	// chromeStartPoll is the DevTools-stderr poll cadence.
	chromeStartPoll = 100 * time.Millisecond

	// navSettle is the small settle sleep after Page.navigate before waits.
	navSettle = 500 * time.Millisecond

	// screenshotTimeout bounds the captureScreenshot call wait loop.
	screenshotTimeout = 30 * time.Second

	// tmpProfilePrefix names the throwaway --user-data-dir.
	tmpProfilePrefix = "gopi-shot-profile-"

	// navCheckWindow bounds how long waitNavigationOutcome keeps asking whether
	// Chromium answered the navigation with an error document. It covers the
	// measured error-document arrival (well under a second) with margin, and it
	// is spent ONLY when neither signal fires.
	navCheckWindow = 3 * time.Second

	// navCheckInterval is the DOM probe's cadence inside that window. The
	// errorText probe is a single call and runs before every poll, so a
	// navigation that reports its error immediately never waits an interval.
	navCheckInterval = 150 * time.Millisecond
)

// The two probes waitNavigationOutcome drives. They are named constants for the
// same reason as everything else in this package (Rule 1), and the tests drive
// the fake CDP peer with these SAME strings — a fake input is what the real
// protocol carries by construction, so a probe rename cannot leave the fake
// answering the wrong call.
//
// errorDocumentProbeExpr asks whether the document on screen is Chromium's
// error page. It reads `location.protocol`, and the choice of binding is
// MEASURED, not assumed (probe: chromium-1228, 2026-09-27, on a real page):
//
//   - `url` is NOT a global binding in this evaluation context — the natural
//     `url.protocol` spelling throws ReferenceError and the probe would have
//     been silently inert, discovered by running it against the real browser.
//   - `Object.defineProperty(window, 'location', …)` THROWS TypeError, so a
//     page cannot fake the binding.
//   - `window.location = {protocol: 'chrome-error://…'}` does not forge a
//     reading either: it makes the browser NAVIGATE to that URL, so the page
//     really does become an error document. A page can therefore only reach a
//     `chrome-error:` reading by actually showing one — which is exactly the
//     state this probe reports, and the state in which capturing "the page"
//     has already failed.
//
// The JSON.stringify wrapper keeps the caller's shape check meaningful: the
// answer is trusted only when it parses as the object this literal produces.
const errorDocumentProbeExpr = `JSON.stringify({` +
	`protocol: location.protocol, ` +
	`loaded: !!(performance && performance.getEntriesByType('navigation')[0]), ` +
	`text: ((String(document.title || '') + ' ' + String((document.body && document.body.innerText) || '')).slice(0, ` + errPageTextProbeConstant + `))})`

// errorPageTextExpr extracts the error page's own message for the operator.
const errorPageTextExpr = `(document.body && document.body.innerText) || ''`

// errPageTextProbeConstant is errPageTextProbeLimit's decimal spelling INSIDE
// errorDocumentProbeExpr. It is a string because it is emitted into JavaScript
// source, and it is written once here (Rule 1's companion: the number has one
// named holder per LANGUAGE, since a Go constant cannot be spliced into a JS
// literal without a conversion the compiler cannot check).
const errPageTextProbeConstant = "300"

// Navigation-outcome protocol constants (Rule 1). The strings are the wire
// vocabulary of Chromium's error document, not our invention, so they live here
// with their meaning stated once.
const (
	// errorPageURLPrefix is the URL of the error document: chrome://navigations/
	// error landed on chrome-error://chromewebdata/ (probe-verified on
	// chromium-1228), which IS the fact an error page is showing. It is checked
	// FIRST because it is the only signal that cannot be shadowed: a page's own
	// JavaScript can run a custom check that returns a fixed object — this
	// protocol check disarms exactly that.
	errorPageURLPrefix = "chrome-error:"

	// netErrorPrefix marks a net-error code in CDP's errorText / the DOM TEXT
	// check (net::ERR_FILE_NOT_FOUND, ERR_UNSAFE_PORT, …).
	netErrorPrefix = "net::ERR_"

	// expectedNetErrorPrefix is the same code WITHOUT the CDP namespace, as it
	// appears in the error document's own TEXT check ("ERR_FILE_NOT_FOUND").
	// A constant cannot call strings.TrimPrefix, so the derivation — netErrorPrefix
	// minus its "net::" prefix — is asserted by TestExpectedNetErrorPrefixIsDerived
	// instead: a machine check keeps the two from drifting.
	expectedNetErrorPrefix = "ERR_"

	// errPageTextProbeLimit caps the error-page text the probe returns, so a
	// pathological document cannot push a CDN-sized string through the CDP
	// response (Rule 1: the bound is named, not inlined).
	errPageTextProbeLimit = 300

	// minErrorPageTextLen is how much of the extracted text must survive before
	// it is worth quoting; below it the message just omits the body.
	minErrorPageTextLen = 8
)

// NavigationFailure is a navigation Chromium ANSWERED WITH AN ERROR DOCUMENT:
// the URL never loaded, and every pixel captured afterwards is Chromium's own
// error page rather than the requested page. It carries the PNG bytes anyway —
// the error page is real evidence about the request (it names the error code),
// so losing it would be worse than the failure it reports.
//
// The error text is the CONTRACT (Rule 1 + the package's "shot: …" shape):
// callers and tests match on it, and it always names the failed URL.
type NavigationFailure struct {
	URL    string // the URL that failed
	Reason string // CDP errorText ("net::ERR_FILE_NOT_FOUND"), or the
	// error-document fact when the navigation event carried no errorText
	PNG []byte // the captured error-page PNG (real bytes, never nil on this path)
}

// Error renders the one-line reason `gopi tool shot` prints. It never embeds the PNG.
func (e *NavigationFailure) Error() string {
	return fmt.Sprintf("shot: navigation to %s failed: %s — captured Chromium's error page, not the requested page", e.URL, e.Reason)
}

// wsURLPattern extracts the DevTools WebSocket endpoint from chrome's stderr.
var wsURLPattern = regexp.MustCompile(`ws://127\.0\.0\.1:(\d+)/devtools/browser/[0-9a-f-]+`)

// Options is the parsed `gopi tool shot` invocation.
type Options struct {
	URL          string // target page (http/https/file/data)
	Self         bool   // screenshot gopi's own web chat UI
	Out          string // output PNG path ("" → shot-<timestamp>.png)
	ViewportW    int    // 0 → DefaultViewportWidth
	ViewportH    int    // 0 → DefaultViewportHeight
	WaitSeconds  int    // -1 → WaitDefault; 0 → no extra wait
	WaitSelector string // "" → none; else poll until querySelector hits
	Addr         string // --self: reuse an already-running gopi web at this addr

	// Surface selects WHAT a --self capture points at (web|chat|screen) — see
	// surface.go. Empty → DefaultSurface (the web SPA), so every existing
	// invocation keeps its behaviour.
	Surface string
	// Text carries the terminal-surface knobs (SurfaceChat): the argv to run as
	// the TUI, its geometry in CELLS, and the screen marker/exit keys.
	Text TextSurfaceOptions
	// Display is the X11 display for SurfaceScreen ("" → $DISPLAY).
	Display string
}

// CaptureSurface runs the capture selected by opts.Surface. The web surface
// keeps the historical Capture path untouched; chat and screen are separate
// engines (a pty-driven VT capture and the X11 root grab) chosen here so the CLI
// has exactly one call site.
func CaptureSurface(ctx context.Context, opts Options) ([]byte, error) {
	surface, err := NormalizeSurface(opts.Surface)
	if err != nil {
		return nil, err
	}
	switch surface {
	case SurfaceWeb:
		return Capture(ctx, opts)
	case SurfaceChat:
		return captureTextSurface(ctx, opts, opts.Text)
	case SurfaceScreen:
		return captureScreenSurface(opts.Display)
	}
	return nil, fmt.Errorf("shot: unknown surface %q (want %s)", surface, SurfacesHelp())
}

// Capture runs one screenshot session and returns the PNG bytes.
func Capture(ctx context.Context, opts Options) ([]byte, error) {
	url, cleanup, err := resolveURL(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	home, _ := os.UserHomeDir() //stylegate:ignore cache lookup; an empty home simply disables the cache path
	bin, err := findChrome(home)
	if err != nil {
		return nil, err
	}

	profile, err := scratch.MkdirTemp(scratch.RuntimeClassShot, tmpProfilePrefix)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(profile)

	proc, err := launchChrome(ctx, bin.Path, profile)
	if err != nil {
		return nil, err
	}
	defer proc.stop()

	// --self: the page was authored and served by THIS process, so the
	// navigation is known good before it is asked about — the outcome check
	// costs one DOM probe instead of a re-navigation (see prepared).
	outcome := prepared{}
	if opts.Self {
		outcome = preparedPage
	}
	return captureOverCDP(proc.port, url, opts, outcome)
}

// waitDevToolsPort reads chrome's stderr until the DevTools listening line
// appears and returns the port number. Bounded by chromeStartTimeout plus a
// per-Read deadline (a plain pipe read has no timeout; the SetReadDeadline
// error is ignored since pipes just block, but reads return promptly once
// chrome prints). A background reaper goroutine would be overkill: the loop
// exits as soon as the pattern matches, and Capture's defer kills chrome.
func waitDevToolsPort(stderr io.Reader) (string, error) {
	done := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			if m := wsURLPattern.FindStringSubmatch(sc.Text()); m != nil {
				done <- m[1]
				return
			}
		}
		done <- "" // stream ended without a match (chrome died?)
	}()
	select {
	case port := <-done:
		if port == "" {
			return "", fmt.Errorf("shot: chrome exited without printing a DevTools port (stderr consumed)")
		}
		return port, nil
	case <-time.After(chromeStartTimeout):
		return "", fmt.Errorf("shot: chrome did not report a DevTools port within %s", chromeStartTimeout)
	}
}

// prepared is a page this package NAVIGATED ITSELF (the --self static server,
// surface.go's synthetic pages) and therefore already knows is good. It exists
// so the navigation-outcome check does not have to re-ask a page the engine just
// authored — see captureOverCDP for the cost that buys.
type prepared struct {
	page bool
}

// preparedPage marks a page the engine loaded itself: a known-good navigation.
var preparedPage = prepared{page: true}

// captureOverCDP performs the CDP session against 127.0.0.1:<port>.
func captureOverCDP(port, url string, opts Options, pre prepared) ([]byte, error) {
	// Discovery polls /json/list until the page target registers: chrome's
	// "DevTools listening on …" line lands ~100 ms BEFORE the target exists, so
	// a single GET yields a page-less list on a healthy chrome.
	wsURL, err := waitPageTarget(cdpLoopbackHTTPPrefix + port)
	if err != nil {
		return nil, err
	}
	conn, err := dialWebSocket(wsURL)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	id := 0
	nextID := func() int { id++; return id }
	if _, err := conn.call(nextID(), "Page.enable", nil); err != nil {
		return nil, err
	}
	w := opts.ViewportW
	if w <= 0 {
		w = DefaultViewportWidth
	}
	h := opts.ViewportH
	if h <= 0 {
		h = DefaultViewportHeight
	}
	if _, err := conn.call(nextID(), "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": w, "height": h, "deviceScaleFactor": 1, "mobile": false,
	}); err != nil {
		return nil, err
	}
	navResult, err := conn.call(nextID(), "Page.navigate", map[string]any{"url": url})
	if err != nil {
		return nil, fmt.Errorf("shot: navigate %s: %w", url, err)
	}
	// THE RESULT IS READ, NEVER DISCARDED: when Chromium has decided the
	// navigation failed (it usually decides before this call returns) the
	// verdict rides right here as errorText, and dropping it was the defect —
	// the only channel left distinguishing success from failure was the pixels.
	navCruft := navigationErrorText(navResult)

	wait := opts.WaitSeconds
	if wait < 0 {
		wait = WaitDefault
	}
	time.Sleep(navSettle + time.Duration(wait)*time.Second)

	if navCruft != "" {
		return captureErrorPage(conn, nextID, url, navCruft)
	}

	// The navigation ran; establish whether Chromium ANSWERED it with an error
	// document. Both probes run BEFORE the --wait-selector gate: when a page
	// never loaded, waiting for a selector the error document does not contain
	// would just time out and report the wrong failure.
	//
	// A page the engine authored itself (pre) is already known good: one DOM
	// probe answers it with no re-navigation, so --self pays a single evaluate.
	if cruft := waitNavigationOutcome(conn, nextID, url, pre); cruft != "" {
		return captureErrorPage(conn, nextID, url, cruft)
	}

	if opts.WaitSelector != "" {
		if err := waitSelector(conn, &id, opts.WaitSelector); err != nil {
			return nil, err
		}
	}

	// Last guard before the shutter: a page that was still loading when the
	// outcome check gave up — or that navigated ITSELF from a loaded page into
	// an error document while we waited — is caught here rather than reported as
	// a successful capture. One evaluate on every path, including --self.
	if cruft := errorDocumentProbe(conn, nextID); cruft != "" {
		return captureErrorPage(conn, nextID, url, cruft)
	}

	png, err := capturePNG(conn, nextID)
	if err != nil {
		return nil, err
	}
	return png, nil
}

// navigationErrorText returns the navigation verdict Chromium PUT IN THE RESULT
// of Page.navigate — navErrorText when the navigation failed, "" when it was
// accepted. It is the free half of the detection: no extra round trip, and no
// second navigation whose own abort could be mistaken for the page's.
func navigationErrorText(res json.RawMessage) string {
	var nav struct {
		ErrorText string `json:"errorText"`
	}
	if strictUnmarshal(res, &nav) != nil {
		// An undecodable result is UNKNOWN, never a failure: refusing to
		// capture a page must never rest on our own parse error.
		return ""
	}
	return nav.ErrorText
}

// captureErrorPage captures the error document that is on screen and folds
// Chromium's own message into the failure, so `gopi tool shot` returns the PNG
// ALONGSIDE the error rather than instead of it — the error page is real
// evidence about the failed request (it names the error code), and the caller
// writes it out as `gopi tool shot` does for a success.
func captureErrorPage(conn *cdpConn, nextID func() int, url, cruft string) ([]byte, error) {
	png, err := capturePNG(conn, nextID)
	if err != nil {
		return nil, err
	}
	reason := cruft
	if detail := errorPageDetail(conn, nextID); detail != "" {
		reason += ": " + detail
	}
	return png, &NavigationFailure{URL: url, Reason: reason, PNG: png}
}

// waitNavigationOutcome establishes whether Chromium is SHOWING AN ERROR
// DOCUMENT. It returns "" for a healthy page, or the cruft as it came from the
// wire — Chromium's own words are the evidence here, so they are quoted, never
// paraphrased.
//
// THE ORDER IS THE COST CONTROL. The DOM probe runs FIRST, and a LOADED
// non-error document ends the wait immediately — so the ordinary healthy page
// pays one evaluate and no waiting, and an unreachable URL is caught on the same
// first probe (probe-verified: the error document is in place with
// location.protocol == chrome-error: within 150 ms and reports loaded=true, so
// "loaded" alone is NOT health — the protocol decides). Only a document that has
// not loaded yet makes the loop wait, and only such a page spends
// navCheckWindow. `pre` (a page this engine authored, --self) skips the wait
// entirely: one probe is the whole answer.
//
// No second Page.navigate is ever issued. It was considered (to re-read
// errorText after the fact) and rejected on measurement: a repeat navigate to a
// still-loading URL ABORTS the navigation in flight, so the probe would report
// its own artifact ("net::ERR_ABORTED") as the page's verdict, and a URL that
// completed would be re-fetched. The first navigate's errorText is read instead
// (navigationErrorText), and the error document itself supplies the error code.
func waitNavigationOutcome(conn *cdpConn, nextID func() int, url string, pre prepared) string {
	if pre.page {
		return errorDocumentProbe(conn, nextID)
	}
	deadline := time.Now().Add(navCheckWindow)
	for {
		probe := probeErrorDocument(conn, nextID)
		if probe.fact != "" {
			return probe.fact
		}
		if probe.loaded {
			// A load committed — and it was NOT the error document, or fact
			// would be set. `loaded` is true on the error page too, which is
			// exactly why the protocol decides and this flag only says "stop
			// waiting": a document that has finished loading will not become a
			// different one on its own, and anything after this point is the
			// pre-capture guard's business.
			return ""
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(navCheckInterval)
	}
}

// domOutcome is one error-document probe's answer: the failure fact (a non-empty
// string means an error document is on screen) plus whether a document is
// loaded at all — the two facts that between them decide the wait early-stop.
type domOutcome struct {
	fact   string
	loaded bool
}

// errorDocumentProbe is probeErrorDocument reduced to the fact, for the callers
// that only need the verdict (the pre-capture guard and the --self path).
func errorDocumentProbe(conn *cdpConn, nextID func() int) string {
	return probeErrorDocument(conn, nextID).fact
}

// probeErrorDocument performs ONE check. An evaluate that fails, throws, or
// answers a shape we did not ask for is UNKNOWN — no fact and not loaded — never
// a failure: a wrong call here would refuse to capture healthy pages.
func probeErrorDocument(conn *cdpConn, nextID func() int) domOutcome {
	res, err := conn.call(nextID(), "Runtime.evaluate", map[string]any{
		"expression": errorDocumentProbeExpr, "returnByValue": true,
	})
	if err != nil {
		return domOutcome{}
	}
	var r struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if strictUnmarshal(res, &r) != nil {
		return domOutcome{}
	}
	// The answer is trusted only when it parses as the object this probe asked
	// for. Anything else — a bare string a page supplied instead, a missing
	// value, a truncated document — is UNKNOWN, and unknown never fails a
	// capture. That is the whole shape check: there is no answer a page can give
	// that both passes it and hides an error document that is actually showing.
	var body struct {
		Protocol string `json:"protocol"`
		Text     string `json:"text"`
	}
	if json.Unmarshal([]byte(r.Result.Value), &body) != nil || body.Protocol == "" {
		return domOutcome{}
	}
	if !strings.HasPrefix(body.Protocol, errorPageURLPrefix) {
		return domOutcome{loaded: true}
	}
	fact := fmt.Sprintf("Chromium served its error page (%s)", body.Protocol)
	// The error code is the most useful word on screen, but it is extracted
	// only when it was actually found: no code, no invented one (Rule 15).
	if ref := firstNetErrorRef(body.Text); ref != "" {
		fact += " — " + ref
	}
	return domOutcome{fact: fact, loaded: true}
}

// firstNetErrorRef returns the first ERR_* code in an error page's own text, or
// "" when the text carries none.
func firstNetErrorRef(text string) string {
	for _, field := range strings.Fields(text) {
		if strings.HasPrefix(field, expectedNetErrorPrefix) {
			return strings.Trim(field, ".,;:()[]")
		}
	}
	return ""
}

// errorDocumentDetail extracts the error page's own message for the operator
// (best effort, bounded): "" when nothing readable can be had, since the
// failure is already fully described by the fact itself.
func errorPageDetail(conn *cdpConn, nextID func() int) string {
	res, err := conn.call(nextID(), "Runtime.evaluate", map[string]any{
		"expression": errorPageTextExpr, "returnByValue": true,
	})
	if err != nil {
		return ""
	}
	var r struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if strictUnmarshal(res, &r) != nil {
		return ""
	}
	text := strings.Join(strings.Fields(r.Result.Value), " ")
	if len(text) < minErrorPageTextLen {
		return ""
	}
	if len(text) > errPageTextProbeLimit {
		text = text[:errPageTextProbeLimit]
	}
	return text
}

// capturePNG runs Page.captureScreenshot and returns the PNG bytes, failing
// loudly on a payload that is not a PNG (the #1136 empty-shell guard).
func capturePNG(conn *cdpConn, nextID func() int) ([]byte, error) {
	res, err := conn.call(nextID(), "Page.captureScreenshot", map[string]any{"format": "png"})
	if err != nil {
		return nil, err
	}
	var shot struct {
		Data string `json:"data"`
	}
	if err := strictUnmarshal(res, &shot); err != nil {
		return nil, err
	}
	png, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return nil, fmt.Errorf("shot: screenshot base64 decode: %w", err)
	}
	if len(png) < 8 || string(png[:8]) != "\x89PNG\r\n\x1a\n" {
		return nil, fmt.Errorf("shot: captured %d bytes, not a PNG — page likely failed to render", len(png))
	}
	return png, nil
}

// strictUnmarshal decodes a CDP result payload.
func strictUnmarshal(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("shot: cdp result decode: %w", err)
	}
	return nil
}

// waitSelector polls Runtime.evaluate until querySelector matches, bounded by
// WaitSelectorTimeout. (id is shared with the caller's counter.)
func waitSelector(conn *cdpConn, id *int, sel string) error {
	deadline := time.Now().Add(WaitSelectorTimeout)
	expr := fmt.Sprintf("document.querySelector(%q) !== null", sel)
	for time.Now().Before(deadline) {
		*id++
		res, err := conn.call(*id, "Runtime.evaluate", map[string]any{
			"expression": expr, "returnByValue": true,
		})
		if err == nil {
			var r struct {
				Result struct {
					Value bool `json:"value"`
				} `json:"result"`
			}
			if strictUnmarshal(res, &r) == nil && r.Result.Value {
				return nil
			}
		}
		time.Sleep(WaitSelectorInterval)
	}
	return fmt.Errorf("shot: selector %q not found within %s", sel, WaitSelectorTimeout)
}

// resolveURL returns the page URL to shoot plus a cleanup func. --self starts
// a temporary static server hosting the embedded webChatPage (the capture
// needs only the page's HTML/CSS/JS render — no agent/session machinery), or
// reuses an already-running instance at opts.Addr when reachable.
func resolveURL(ctx context.Context, opts Options) (string, func(), error) {
	if opts.Self {
		if SelfPageProvider == nil {
			return "", nil, errors.New("shot: --self unavailable (page provider not wired)")
		}
		if opts.Addr != "" {
			if reachable(opts.Addr) {
				return "http://" + opts.Addr + "/", func() {}, nil
			}
			return "", nil, fmt.Errorf("shot: --addr %s is not reachable — start `gopi web --addr %s` first", opts.Addr, opts.Addr)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", nil, err
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, selfPage()) //nolint:errcheck // client gone mid-write is fine
		})
		srv := &http.Server{Handler: mux}
		//stylegate:ignore Shutdown/Close terminates Serve — ErrServerClosed is the normal exit path
		go func() { _ = srv.Serve(ln) }()
		url := "http://" + ln.Addr().String() + "/"
		//stylegate:ignore deferred shutdown of the local CDP server after the caller returned
		return url, func() { _ = srv.Shutdown(context.Background()) }, nil
	}
	if opts.URL == "" {
		return "", nil, errors.New("shot: nothing to shoot — pass --url URL or --self")
	}
	if !strings.Contains(opts.URL, "://") && !strings.HasPrefix(opts.URL, "data:") {
		// Bare hostname → http://; data:/https: etc. are already absolute.
		opts.URL = "http://" + opts.URL
	}
	return opts.URL, func() {}, nil
}

// reachable reports whether a TCP addr accepts a connection quickly.
func reachable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, wsHandshakeTimeout)
	if err != nil {
		return false
	}
	conn.Close() //stylegate:ignore transport teardown on a connection already reported as failed
	return true
}
