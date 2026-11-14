// cli_web_shot_bind_test.go — the BIND half of the /api/shot target proof.
//
// The endpoint's privacy posture has always been "the target is s.addr — the
// address this process bound its listener to — and nothing from the request
// reaches it". The old implementation proved that by requiring the address to
// be LOOPBACK, which is a different (and false) statement: `gopi web --addr
// <this machine's LAN IP>` — the spelling `gopi web -h` prints — binds a
// non-loopback address, so the proof refused the process's OWN listener and
// /api/shot answered 500 on every request (bug 01a1b343-b6ef).
//
// These tests pin the corrected criterion on BOTH sides of the line:
//
//   - an address THIS PROCESS is listening on is captureable, whatever its
//     interface (loopback, a LAN address, or a wildcard bind);
//   - an address this process is NOT listening on is refused BY NAME before any
//     browser starts, whether it belongs to another host or to this one but no
//     process is behind it.
//
// The second half is the one that must not rot: "it is an address of this
// machine" is NOT the criterion, and neither is "it is loopback".
package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/shot"
)

// shotServerBoundTo builds a webServer whose listen address is a REAL listener
// on addr (an explicit host:port or ":0"), so the handler's target proof runs
// against the kernel's own table instead of a fabricated string. The listener
// accepts in the background: the proof dials it, and a listener that never
// accepts would make the test hang until the dial timeout.
func shotServerBoundTo(t *testing.T, addr string) (*webServer, string) {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("cannot bind %q on this host: %v", addr, err)
	}
	t.Cleanup(func() { _ = ln.Close() }) //stylegate:ignore closing a test listener cannot fail usefully
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_ = conn.Close() //stylegate:ignore probe connections carry no data
		}
	}()
	bound := ln.Addr().String()
	return &webServer{addr: bound}, bound
}

// shotOwnLANAddr returns an interface address of THIS machine that is neither
// loopback nor link-local, or skips when this host has none (a container with
// only lo). Skipping is honest: the test's subject is a real non-loopback bind,
// and inventing one would test nothing.
func shotOwnLANAddr(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("cannot read this host's interface addresses: %v", err)
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil || ipnet.IP.IsLoopback() || ipnet.IP.IsLinkLocalUnicast() || ipnet.IP.IsLinkLocalMulticast() {
			continue
		}
		return ipnet.IP.String()
	}
	t.Skip("this host has no non-loopback IPv4 interface address — nothing to bind")
	return ""
}

// shotOwnGlobalIPv6Addr returns a GLOBAL IPv6 address of this machine, or skips
// when it has none.
//
// It exists because the bug's fix is a property of the ADDRESS, not of IPv4:
// `webHostIsLocal` reads `net.InterfaceAddrs` (which lists v6 too) and the probe
// compares `conn.LocalAddr().IP.Equal(net.ParseIP(host))` (which is
// family-agnostic), so an IPv6 bind must be captureable for the same reason a
// LAN IPv4 bind is. Link-local is excluded on purpose — the interesting case is
// an address that is NOT automatically reachable on the local link.
func shotOwnGlobalIPv6Addr(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("cannot read this host's interface addresses: %v", err)
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.To4() != nil {
			continue
		}
		if !ipnet.IP.IsGlobalUnicast() || ipnet.IP.IsLinkLocalUnicast() || ipnet.IP.IsLoopback() {
			continue
		}
		return ipnet.IP.String()
	}
	t.Skip("this host has no global IPv6 interface address — nothing to bind")
	return ""
}

// TestWebShotCapturesThisProcessesOwnNonLoopbackListener is the bug's regression
// test: `gopi web --addr 192.0.2.12:PORT` must be captureable.
//
// It binds a REAL listener on this machine's own LAN address (that is exactly
// what `gopi web` does), drives the endpoint over the real mux, and asserts both
// halves of the privacy property: 200 with the captured bytes, and the capture
// seam received THAT address — not a loopback substitute, not anything the
// request could have named.
//
// RED PROOF (measured, before the fix): 500 with
// `/api/shot: refusing to capture 192.0.2.12:<port> — only a loopback address
// can be this instance's own page`, and the capture never ran.
func TestWebShotCapturesThisProcessesOwnNonLoopbackListener(t *testing.T) {
	lanIP := shotOwnLANAddr(t)
	rec := withShotSeam(t, webShotTestPNG, nil)
	ws, addr := shotServerBoundTo(t, net.JoinHostPort(lanIP, "0"))
	srv := httptest.NewServer(ws.webChatHandler())
	defer srv.Close()

	res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST %s: %v", webShotPath, err)
	}
	defer res.Body.Close()          //nolint:errcheck // test read
	body, _ := io.ReadAll(res.Body) //stylegate:ignore bounded test read of a small JSON body

	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST %s on this process's own non-loopback listener %s = %d, want 200 — body: %s",
			webShotPath, addr, res.StatusCode, body)
	}
	if got := rec.calls.Load(); got != 1 {
		t.Errorf("the capture ran %d time(s), want exactly 1", got)
	}
	if rec.opts.Addr != addr {
		t.Errorf("the capture targeted %q, want this server's own bound address %q", rec.opts.Addr, addr)
	}
	if host := mustHost(t, rec.opts.Addr); host != lanIP {
		t.Errorf("the capture targeted host %q, want the process's own bound host %q", host, lanIP)
	}
	if !rec.opts.Self || rec.opts.Surface != shotSurfaceWebForTest {
		t.Errorf("the capture options moved: Self=%v Surface=%q (must stay this instance's page)",
			rec.opts.Self, rec.opts.Surface)
	}
}

// TestWebShotStillCapturesAWildcardBindAndLoopback pins the two spellings that
// already worked, so the fix cannot trade one budget for another: a wildcard
// bind (`0.0.0.0:PORT`, `:PORT`) is captured through the loopback spelling of
// the SAME listener, and an explicit loopback bind is captured as itself.
func TestWebShotStillCapturesAWildcardBindAndLoopback(t *testing.T) {
	cases := []struct {
		name    string
		bind    string
		wantDst string // "" = whatever the listener reported, but loopback
	}{
		{name: "explicit-loopback", bind: "127.0.0.1:0"},
		{name: "wildcard-v4", bind: "0.0.0.0:0", wantDst: webShotLoopbackHost},
		// The host NAME that is loopback by definition. It worked before this
		// endpoint's target proof changed, and the proof must not depend on a
		// DNS resolution it never asked for.
		{name: "localhost-name", bind: net.JoinHostPort(webLoopbackHostName, "0"), wantDst: webShotLoopbackHost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := withShotSeam(t, webShotTestPNG, nil)
			ws, addr := shotServerBoundTo(t, tc.bind)
			srv := httptest.NewServer(ws.webChatHandler())
			defer srv.Close()

			res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader(""))
			if err != nil {
				t.Fatalf("POST %s: %v", webShotPath, err)
			}
			defer res.Body.Close() //nolint:errcheck // test read
			if res.StatusCode != http.StatusOK {
				t.Fatalf("s.addr=%q = %d, want 200 (this spelling worked before the fix and must keep working)", addr, res.StatusCode)
			}
			if got := rec.calls.Load(); got != 1 {
				t.Errorf("the capture ran %d time(s), want exactly 1", got)
			}
			gotHost := mustHost(t, rec.opts.Addr)
			if tc.wantDst != "" && gotHost != tc.wantDst {
				t.Errorf("a wildcard bind captured through %q, want the loopback spelling %q", gotHost, tc.wantDst)
			}
			if !webHostIsLocal(gotHost) {
				t.Errorf("the capture target host %q is not this machine", gotHost)
			}
		})
	}
}

// TestWebShotRefusesAnAddressThisProcessIsNotListeningOn is the other side of
// the criterion, and it is the one a "just drop the loopback check" fix would
// break: the target must be THIS PROCESS's listener.
//
// Three shapes, all of which must fail WITHOUT running a browser:
//
//   - another machine's address (a literal that is not one of this host's) —
//     refused before the probe;
//   - this host's own address on a port nothing listens on — the probe fails;
//   - a well-formed but unbound address where the connection cannot complete.
func TestWebShotRefusesAnAddressThisProcessIsNotListeningOn(t *testing.T) {
	lanIP := shotOwnLANAddr(t)
	closedPort := shotClosedPort(t, lanIP)

	cases := []struct{ name, addr string }{
		{name: "another-machines-address", addr: net.JoinHostPort(webShotForeignTestHost, "8090")},
		{name: "own-address-no-listener", addr: net.JoinHostPort(lanIP, closedPort)},
		{name: "loopback-no-listener", addr: net.JoinHostPort(webShotLoopbackHost, closedPort)},
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
			defer res.Body.Close()          //nolint:errcheck // test read
			body, _ := io.ReadAll(res.Body) //stylegate:ignore bounded test read of a small error body

			if res.StatusCode == http.StatusOK {
				t.Fatalf("s.addr=%q was accepted (%d), want a refusal — the endpoint captures only this process's own listener. body: %s",
					tc.addr, res.StatusCode, body)
			}
			if got := rec.calls.Load(); got != 0 {
				t.Errorf("s.addr=%q still ran the capture %d time(s) — a refusal must cost no browser", tc.addr, got)
			}
			// Rule 15: the refusal speaks. It must name the address it refused,
			// so an operator reading a 500 learns WHICH target was rejected.
			if !strings.Contains(string(body), tc.addr) {
				t.Errorf("the refusal does not name the refused address %q — body: %s", tc.addr, body)
			}
		})
	}
}

// webShotForeignTestHost is an address this machine does not own, used to spell
// "somebody else's host". A TEST-ONLY literal: the production file must not
// carry one (the endpoint never names a destination — it reads s.addr).
const webShotForeignTestHost = "192.0.2.9" // RFC 5737 TEST-NET-1: never a real peer

// shotSurfaceWebForTest re-exports the surface the endpoint must ask for, so
// this file's assertions read the same constant the implementation does.
const shotSurfaceWebForTest = shot.SurfaceWeb

// shotClosedPort returns a port on host that nothing is listening on: bind one,
// read it, close it. The window between close and use is a test-local race with
// this run's own processes only; a live listener would fail the assertion, not
// silently pass it.
func shotClosedPort(t *testing.T, host string) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatalf("cannot probe for a closed port on %q: %v", host, err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("cannot read the probe listener's address: %v", err)
	}
	if cerr := ln.Close(); cerr != nil {
		t.Fatalf("cannot close the probe listener: %v", cerr)
	}
	if _, aerr := strconv.Atoi(port); aerr != nil {
		t.Fatalf("the probe port %q is not numeric: %v", port, aerr)
	}
	return port
}

// TestWebShotCapturesThisProcessesOwnGlobalIPv6Listener pins that the fix is a
// property of the ADDRESS, not of IPv4: `gopi web --addr [<global v6>]:PORT` is
// the same posture as a LAN IPv4 bind — an address of this machine that this
// process bound — so it must be captured too.
//
// MEASURED before this test existed (real run, binary at HEAD e2b42a23):
// `./bin/gopi web --addr [240e:37c:1a16:d100:fd4a:9e0c:94b4:b1f3]:8194`,
// `curl -X POST http://[240e:…:b1f3]:8194/api/shot` → status=200, 53342 PNG
// bytes, magic 89504e470d0a1a0a. It is a test rather than only a report line so
// a later change that starts assuming IPv4 (a To4() filter, an IPv4-only probe)
// turns red instead of silently refusing a legitimate bind.
func TestWebShotCapturesThisProcessesOwnGlobalIPv6Listener(t *testing.T) {
	v6 := shotOwnGlobalIPv6Addr(t)
	rec := withShotSeam(t, webShotTestPNG, nil)
	ws, addr := shotServerBoundTo(t, net.JoinHostPort(v6, "0"))
	srv := httptest.NewServer(ws.webChatHandler())
	defer srv.Close()

	res, err := http.Post(srv.URL+webShotPath, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST %s: %v", webShotPath, err)
	}
	defer res.Body.Close()          //nolint:errcheck // test read
	body, _ := io.ReadAll(res.Body) //stylegate:ignore bounded test read of a small JSON body

	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST %s on this process's own global IPv6 listener %s = %d, want 200 — body: %s",
			webShotPath, addr, res.StatusCode, body)
	}
	if got := rec.calls.Load(); got != 1 {
		t.Errorf("the capture ran %d time(s), want exactly 1", got)
	}
	if host := mustHost(t, rec.opts.Addr); host != v6 {
		t.Errorf("the capture targeted host %q, want the process's own bound host %q", host, v6)
	}
	if !rec.opts.Self || rec.opts.Surface != shotSurfaceWebForTest {
		t.Errorf("the capture options moved: Self=%v Surface=%q (must stay this instance's page)",
			rec.opts.Self, rec.opts.Surface)
	}
}
