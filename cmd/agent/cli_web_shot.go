// cli_web_shot.go — POST /api/shot: the server half of the page's
// «📷 截当前页» button.
//
// WHAT IT DOES. One request captures THIS gopi web instance's own page with
// internal/shot's WEB surface (headless Chromium + CDP), validates the bytes
// through the same webChatImage contract /api/chat accepts, and returns
// {"image":{"media_type","data_b64"}} — the page pushes that into its own
// images[] and the next Enter is an ordinary vision turn. No new wire type, no
// new registry, no persistence.
//
// THE PRIVACY POSTURE IS A PROPERTY OF THE CODE SHAPE, not a default that a knob
// can widen. Three things are true by construction:
//
//  1. THE SURFACE IS A CONSTANT. The capture call below passes
//     shot.SurfaceWeb and nothing else; the request body carries exactly ONE
//     optional field, `viewport` (see webShotRequest), and it can only change
//     the picture's GEOMETRY — there is no field a client could set to reach
//     another surface or another target. internal/shot's own CaptureSurface
//     still knows about the terminal and the display — those surfaces are
//     simply not reachable from this endpoint, and this file never names them
//     (the gate TestWebShotNeverNamesANonPageSurface pins that, in code AND
//     comments).
//  2. THE TARGET IS THIS PROCESS. The address is s.addr — the value runWeb
//     bound its listener to — resolved to an address OF THIS MACHINE and
//     proved to be a live listener bound there before any browser starts
//     (webShotSelfAddr). Loopback, a LAN address and a wildcard bind are all
//     captureable, because all three are addresses this process bound; a
//     foreign host, or an address with no listener, is refused by name. A
//     client cannot influence any of it: nothing from the request reaches the
//     URL.
//  3. THE PRICING IS THE PAGE'S. A capture costs one Chromium start, so the
//     endpoint refuses without starting one when the server is shutting down,
//     and it names every failure with a status the page can render.
//
// RULE 35: no resident process, no timer, no file. The Chromium child is
// launched, used and reaped inside one request by internal/shot (its
// systemd-run --user escape and its 12 GiB RLIMIT_AS workaround are that
// package's business — see devdoc/web-page-shot.md for the measured path).
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/shot"
)

// webShotPath is the endpoint's route (Rule 1b: the mux registration and the
// refusal messages read this one name).
const webShotPath = "/api/shot"

// webShotMediaType is the media type the capture is declared as. internal/shot
// returns PNG bytes from CDP's captureScreenshot{"format":"png"}, and PNG is one
// of webAllowedImageMediaTypes, so the value is checked against the page's OWN
// validator rather than re-declared: validateWebImage (below) accepts exactly
// what /api/chat accepts.
const webShotMediaType = "image/png"

// webShotTimeout bounds one capture: a Chromium cold start plus a navigation is
// seconds, and a wedged browser must fail the request instead of hanging it
// forever (Rule 15 — a failure has to be VISIBLE, not a hung connection).
const webShotTimeout = 90 * time.Second

// webShotLoopbackHost / webShotLoopbackHost6 are the addresses a captured
// target may resolve to: the two loopback literals, never "" (which is every
// interface) and never a LAN/remote address.
const (
	webShotLoopbackHost  = "127.0.0.1"
	webShotLoopbackHost6 = "::1"
)

// webShotViewportKey is the ONE field name this endpoint accepts. Rule 1b: the
// struct tag, the parser's allow-list and the refusal message read this name.
const webShotViewportKey = "viewport"

// webShotViewport is the OPTIONAL geometry sub-object:
//
//	{"viewport":{"w":900,"h":900}}
//
// It is the page's OWN window size (the page fills it in from
// window.innerWidth/innerHeight), and it is the ONLY knob this endpoint has.
// Zero (or a missing key) means "use the default" — the same zero-means-default
// convention internal/shot's Options already uses, so there is one rule here and
// not two.
type webShotViewport struct {
	W int `json:"w"`
	H int `json:"h"`
}

// The viewport bounds. A client-supplied geometry is CLAMPED into this range
// rather than refused (Rule 15: the caller is told what it got, and a page whose
// window shrank below the floor still gets a picture); the values are named so
// the clamp, its announcement and its test read the same numbers (Rule 1).
const (
	// webShotMinViewport is the smallest dimension a capture may be asked for.
	// Below it a "screenshot" carries too few pixels to be the page at all.
	webShotMinViewport = 200
	// webShotMaxViewport is the largest. It bounds the work one unauthenticated-
	// adjacent request can ask Chromium for, and it is above
	// shot.DefaultViewportHeight so the default itself is never clamped.
	webShotMaxViewport = 4000
)

// webShotRequest is the request body: ONE optional field, and nothing else.
//
// What it deliberately does NOT accept is the privacy/SSRF boundary itself. An
// endpoint taking a URL is a jump host that navigates and screenshots on the
// caller's behalf; one taking a surface is a way to ask for the desktop. So
// there is no URL field, no addr field, no surface field — the capture target is
// this process's own listener, resolved from s.addr alone — and the parser
// REFUSES any other key by name instead of ignoring it (Rule 15), so a caller
// that tried to aim the capture learns it was refused, not that it worked.
//
// Viewport is a POINTER so "absent" and `{"w":0,"h":0}` are distinguishable in
// the type, even though both resolve to the default.
type webShotRequest struct {
	Viewport *webShotViewport `json:"viewport"`
}

// webShotMaxBodyBytes bounds the request body read. The legitimate body is "{}",
// empty, or {"viewport":{"w":N,"h":N}}; anything larger is a refusal, not a
// buffer.
const webShotMaxBodyBytes = 1024

// webShotResponse is the success payload — and it reuses webChatImage VERBATIM
// rather than declaring a parallel struct. That is the point: the page pushes
// the returned object into images[] and /api/chat validates it with no
// translation step, so "what this endpoint returns" and "what /api/chat
// accepts" cannot drift apart (a second struct would be a second contract).
//
// The one addition is CAPTURE — the geometry the server actually used, echoed
// so a caller that asked for something clamped LEARNS what it got instead of
// measuring the PNG (Rule 15). It is a sibling of "image", never a field inside
// it: webChatImage is /api/chat's input contract and must stay byte-identical.
// A test (TestWebShotEchoesTheClampedGeometry) pins both halves.
type webShotResponse struct {
	Image   webChatImage `json:"image"`
	Capture captureShape `json:"capture"`
}

// captureShape is the echoed geometry: the width/height the picture actually
// has, and — when the request asked for something else — the size it asked for
// and whether the answer was clamped. Omitted entirely when nothing was clamped
// and nothing was asked for, so an old caller's response body gains only
// "capture":{"w":1440,"h":2800}, which is additive JSON a caller ignores.
type captureShape struct {
	W       int  `json:"w"`
	H       int  `json:"h"`
	AskedW  int  `json:"asked_w,omitempty"`
	AskedH  int  `json:"asked_h,omitempty"`
	Clamped bool `json:"clamped,omitempty"`
}

// handleShot answers POST /api/shot: capture this instance's own page and return
// it as base64 PNG.
//
// The token gate is NOT here — it is the mux-level gate runWeb installs
// (webTokenGate over webChatHandler), the SAME wrapper that guards /api/chat, so
// this route can never be a bypass: a gated bind refuses an unauthenticated
// POST before this function runs (TestWebShotRefusesWithoutTheToken).
func (s *webServer) handleShot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (POST "+webShotPath+")", http.StatusMethodNotAllowed)
		return
	}
	// Same shutdown interlock as /api/chat: once the finalizer is closing the
	// session, a capture would start a Chromium for a page nobody will read.
	if s.shuttingDown {
		http.Error(w, `{"error":"server shutting down"}`, http.StatusServiceUnavailable)
		return
	}
	// THE BODY IS PARSED, AND ANY FIELD BUT `viewport` IS REFUSED. A caller that
	// ships {"url":…}, {"addr":…} or {"surface":…} is asking for something this
	// endpoint cannot do, and silently dropping the field would let the caller
	// believe the capture went where it asked (Rule 15).
	req, err := webShotParseBody(r)
	if err != nil {
		http.Error(w, `{"error":`+jsonString(err.Error())+`}`, http.StatusBadRequest)
		return
	}

	addr, err := webShotSelfAddr(s.addr)
	if err != nil {
		http.Error(w, `{"error":`+jsonString(err.Error())+`}`, http.StatusInternalServerError)
		return
	}
	// A clamped geometry is ANNOUNCED (Rule 15): the caller asked for something
	// it did not get, so the run leaves a trace of the substitution instead of
	// rewriting it silently.
	vp := req.viewport()
	if note := webShotClampNote(req, vp); note != "" {
		fmt.Fprintf(os.Stderr, "%s\n", note)
	}

	ctx, cancel := context.WithTimeout(r.Context(), webShotTimeout)
	defer cancel()

	// THE ONE CAPTURE CALL. Self (this instance's page) + the web surface +
	// this process's own bound address — the request may set the GEOMETRY and
	// nothing else.
	png, err := webShotCapture(ctx, addr, vp)
	if err != nil {
		http.Error(w, `{"error":`+jsonString(err.Error())+`}`, http.StatusBadGateway)
		return
	}

	// The capture is judged by the PAGE'S OWN validator: a capture that comes
	// back empty, truncated or (bug) not an allowed media type is refused here
	// rather than shipped to a browser that would show a broken thumbnail and
	// only fail at send time.
	b64 := base64.StdEncoding.EncodeToString(png)
	captured := webChatImage{MediaType: webShotMediaType, DataB64: b64}
	if _, err := validateWebImage(captured); err != nil {
		http.Error(w, `{"error":`+jsonString("capture rejected by "+webShotPath+": "+err.Error())+`}`, http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(webShotResponse{Image: captured, Capture: req.captureShape(vp)})
}

// webShotCapture is the seam the tests replace: the real implementation asks
// internal/shot for the page surface anchored at addr, at the requested
// geometry. Kept as a package variable (one assignment point, one reader) so a
// unit test can drive the handler's whole path — validation, base64, error
// surfacing — without a browser, while the real run exercises the browser
// itself (Rule 31).
var webShotCapture = func(ctx context.Context, addr string, vp shotViewport) ([]byte, error) {
	// SelfPageProvider is wired by the shot CLI's init path; wire it here too so
	// the capture works even if `gopi tool shot` was never invoked in this
	// process (the provider returns the SAME embedded page this server serves).
	if shot.SelfPageProvider == nil {
		shot.SelfPageProvider = func() string { return webChatPage }
	}
	// THE CAPTURE OPTIONS ARE BUILT HERE, NOT PARSED FROM ANYWHERE. There is no
	// target input: Self is a constant, Surface is a constant, and Addr is the
	// address webShotSelfAddr proved belongs to this process. The ONE thing a
	// request may influence is the GEOMETRY, and it arrives already clamped.
	// A test (TestWebShotAlwaysAsksForTheWebSurfaceOfItsOwnPage) records what
	// this call received, so flipping the surface or the target turns it red.
	png, err := webShotRun(ctx, shot.Options{
		Self:      true,
		Addr:      addr,
		Surface:   shot.SurfaceWeb,
		ViewportW: vp.W,
		ViewportH: vp.H,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: capture failed: %w", webShotPath, err)
	}
	if len(png) == 0 {
		// A zero-length capture is a failure, never an empty picture (Rule 15).
		return nil, fmt.Errorf("%s: capture returned no bytes", webShotPath)
	}
	return png, nil
}

// webShotRun is the seam the tests record through: the ONE call into
// internal/shot. A test replaces it to assert the capture OPTIONS this endpoint
// asks for (the privacy property), while a run replaces it — via the real
// implementation — with the browser itself (Rule 31).
var webShotRun = shot.CaptureSurface

// shotViewport is the geometry this endpoint resolved for one capture: what the
// page asked for, clamped into [webShotMinViewport, webShotMaxViewport]. It is
// a named type rather than two ints so the seam's signature cannot be filled
// with a width and a height in the wrong order without a type error.
type shotViewport struct {
	W int
	H int
}

// viewport resolves the parsed body into the geometry to capture at. An absent
// sub-object is the default (zero → internal/shot's own defaults), which is why
// every pre-existing caller — one that POSTs "{}" or nothing at all — keeps the
// 1440x2800 geometry it always got.
func (req webShotRequest) viewport() shotViewport {
	in := webShotViewport{}
	if req.Viewport != nil {
		in = *req.Viewport
	}
	return shotViewport{
		W: clampViewport(in.W, shot.DefaultViewportWidth),
		H: clampViewport(in.H, shot.DefaultViewportHeight),
	}
}

// clampViewport bounds ONE dimension. Zero or negative means "unset" and keeps
// internal/shot's own default (the convention its Options documents, so there is
// one rule); any other value is CLAMPED into the allowed range rather than
// refused, and the clamp is announced by webShotClampNote when it fires.
func clampViewport(v, def int) int {
	if v <= 0 {
		return def
	}
	if v < webShotMinViewport {
		return webShotMinViewport
	}
	if v > webShotMaxViewport {
		return webShotMaxViewport
	}
	return v
}

// captureShape is what the response echoes back: the geometry the picture
// actually has, plus — only when the request asked for something else — the
// size it asked for and the fact that the answer was clamped.
//
// This is Rule 15's shape for a CLAMP rather than a refusal: the caller is not
// turned away (a page whose window shrank still gets its picture), but it is
// TOLD what it got, in the same response, so nothing has to be inferred from
// the PNG's bytes by a human reading a log afterwards.
func (req webShotRequest) captureShape(vp shotViewport) captureShape {
	out := captureShape{W: vp.W, H: vp.H}
	if req.Viewport == nil {
		return out
	}
	// `asked` is only meaningful for a POSITIVE request: w=0 means "unset", the
	// same convention clampViewport uses.
	if req.Viewport.W > 0 && req.Viewport.W != vp.W {
		out.AskedW, out.Clamped = req.Viewport.W, true
	}
	if req.Viewport.H > 0 && req.Viewport.H != vp.H {
		out.AskedH, out.Clamped = req.Viewport.H, true
	}
	return out
}

// webShotClampNote is the SERVER-SIDE half of the same announcement: a clamp
// this request suffered is written to stderr, so a run's log shows it even for
// a caller that never reads the response body.
func webShotClampNote(req webShotRequest, vp shotViewport) string {
	got := req.captureShape(vp)
	if !got.Clamped {
		return ""
	}
	asked := fmt.Sprintf("w %d, h %d", req.Viewport.W, req.Viewport.H)
	return fmt.Sprintf("%s: clamped viewport (%s) into [%d,%d] → %dx%d",
		webShotPath, asked, webShotMinViewport, webShotMaxViewport, got.W, got.H)
}

// webShotParseBody decodes the request body: empty, "{}", or exactly one key,
// `viewport`, carrying optional w/h. An EMPTY body and "{}" are both fine (a
// plain POST needs no payload).
//
// Every other key is REFUSED BY NAME. This is the SSRF/privacy boundary
// speaking: a caller that tried to point the capture at another URL, address or
// surface must read that it was refused, not that it was honoured.
func webShotParseBody(r *http.Request) (webShotRequest, error) {
	var out webShotRequest
	raw, err := io.ReadAll(io.LimitReader(r.Body, webShotMaxBodyBytes+1))
	if err != nil {
		return out, fmt.Errorf("read request body: %w", err)
	}
	if len(raw) > webShotMaxBodyBytes {
		return out, fmt.Errorf("%s takes at most a %d-byte body ({\"viewport\":{\"w\":N,\"h\":N}})", webShotPath, webShotMaxBodyBytes)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "{}" {
		return out, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return out, fmt.Errorf("%s takes an optional {%q:{\"w\":N,\"h\":N}} — the body must be empty, {} or that object: %w", webShotPath, webShotViewportKey, err)
	}
	unknown := make([]string, 0, len(fields))
	for name := range fields {
		if name != webShotViewportKey {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		// The refusal NAMES the fields: a caller that tried to aim the capture
		// somewhere must read that it was refused.
		return out, fmt.Errorf("%s takes no parameter but %q — refusing %s; the capture target is always THIS server's own page",
			webShotPath, webShotViewportKey, strings.Join(sortedStrings(unknown), ", "))
	}
	if vpRaw, ok := fields[webShotViewportKey]; ok {
		if err := webShotParseViewport(vpRaw, &out); err != nil {
			return out, err
		}
	}
	return out, nil
}

// webShotParseViewport decodes the viewport sub-object into out. A wrong JSON
// TYPE is refused by name (Rule 15) rather than defaulted silently: a caller
// that sent "viewport":"900x900" asked for a geometry and must learn it was not
// understood.
func webShotParseViewport(raw json.RawMessage, out *webShotRequest) error {
	var vp webShotViewport
	if err := json.Unmarshal(raw, &vp); err != nil {
		return fmt.Errorf("%s: %q must be an object {\"w\":N,\"h\":N} of whole numbers: %w",
			webShotPath, webShotViewportKey, err)
	}
	out.Viewport = &vp
	return nil
}

// sortedStrings returns a stable order so the refusal line is deterministic (a
// map's iteration order is not).
func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// webShotSelfAddr turns the server's listen address into the address a capture
// may navigate to, and PROVES it belongs to this process.
//
// The proof is not a string comparison, and it is NOT "the address is loopback".
// That second reading was the bug (01a1b343-b6ef): `--addr <this machine's LAN
// IP>` is a spelling the usage text prints, it binds a real listener on a
// non-loopback address, and a loopback precondition refused the process's OWN
// listener — so every /api/shot on such a bind answered 500.
//
// What must actually be true is narrower, and it holds for every bind:
//
//  1. the host is an address OF THIS MACHINE (loopback, or one of this host's
//     own interface addresses) — so the capture can never name a foreign host,
//     and a LAN bind is captureable exactly because that address is still ours;
//  2. a connection to host:port completes AND leaves this host through that
//     same address — so a live listener answers at the very address parity
//     `gopi web` bound, not at a name that resolved somewhere else.
//
// A wildcard bind (":8090", "0.0.0.0:8090") is resolved to the loopback
// spelling: the listener accepts there too, and capturing through loopback
// keeps a wildcard bind's capture traffic off the wire.
//
// Nothing from the request reaches any of this: the input is s.addr alone.
func webShotSelfAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", fmt.Errorf("%s: cannot read this server's listen address %q: %w", webShotPath, addr, err)
	}
	if port == "" {
		return "", fmt.Errorf("%s: this server's listen address %q has no port", webShotPath, addr)
	}
	target := host
	switch {
	case target == "" || target == webShotWildcardHost4 || target == webShotWildcardHost6:
		// A wildcard bind listens on every interface, loopback included — the
		// capture uses the loopback spelling, so nothing leaves this host.
		target = webShotLoopbackHost
	case strings.EqualFold(target, webLoopbackHostName):
		// The one host NAME that is loopback by definition. Normalised to its
		// literal here so the self-probe can compare IPs directly: a name that
		// stays a name is a resolution this code never wants to depend on, and
		// `--addr localhost:PORT` worked before this endpoint changed.
		target = webShotLoopbackHost
	}
	self, err := webShotIsOwnListener(target, port)
	if err != nil {
		return "", err
	}
	if !self {
		return "", fmt.Errorf("%s: refusing to capture %s — it is not an address this process is listening on (the endpoint captures only this gopi web instance)",
			webShotPath, net.JoinHostPort(target, port))
	}
	return net.JoinHostPort(target, port), nil
}

// The two wildcard spellings a bind address may carry, named once so the
// resolution above and its test read the same values.
const (
	webShotWildcardHost4 = "0.0.0.0"
	webShotWildcardHost6 = "::"
)

// webShotIsOwnListener reports whether this process holds a listening socket on
// host:port, and whether reaching it stays on this machine.
//
// The criterion is CUSTODY, not a spelling: the host must be an address of this
// machine (webHostIsLocal — loopback or one of our own interface addresses),
// and a probe connection must (a) complete and (b) report a local address equal
// to host:port. Together that says "a listener is bound at exactly the address
// this server bound, reachable only through our own interface" — which is the
// privacy property (the capture targets THIS instance and nothing else), while
// refusing to confuse it with the narrower, false statement "the target must be
// loopback".
//
// Refusing a FOREIGN host here (rather than trusting the probe alone) keeps the
// failure cheap and named: a stale s.addr naming another machine is refused
// before any dial, so the endpoint can never be used to probe a remote host,
// and Chromium is never started for a target this process does not own.
func webShotIsOwnListener(host, port string) (bool, error) {
	joined := net.JoinHostPort(host, port)
	if !webHostIsLocal(host) {
		return false, fmt.Errorf("%s: refusing to capture %s — it is not an address of this machine, and the endpoint captures only this gopi web instance's own page",
			webShotPath, joined)
	}
	conn, err := net.DialTimeout("tcp", joined, webShotDialTimeout)
	if err != nil {
		return false, fmt.Errorf("%s: this server is not reachable at %s: %w", webShotPath, joined, err)
	}
	defer conn.Close() //nolint:errcheck // closing a probe connection cannot fail usefully
	local, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok || local == nil {
		return false, fmt.Errorf("%s: probe of %s reported no local address", webShotPath, joined)
	}
	// The kernel's own answer, not a resolution: a probe whose SOURCE address is
	// the address we aimed at proves the connection never left this host and
	// that the listener sits exactly where the bind put it.
	if !local.IP.Equal(net.ParseIP(host)) {
		return false, fmt.Errorf("%s: refusing to capture %s — the probe left this host through %s instead, so that address is not this instance's own listener",
			webShotPath, joined, local.IP)
	}
	return true, nil
}

// webShotDialTimeout bounds the self-probe. A listener that does not accept a
// connection in this window is not this instance, and starting a browser to
// find out is not worth it.
const webShotDialTimeout = 2 * time.Second

// webShotHostIsLoopback reports whether host is one of the two loopback
// literals (or the localhost name). Named separately so the wildcard resolution
// and the self-probe agree (Rule 13).
func webShotHostIsLoopback(host string) bool {
	return host == webShotLoopbackHost || host == webShotLoopbackHost6 || host == webLoopbackHostName
}
