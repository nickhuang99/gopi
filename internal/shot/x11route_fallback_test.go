// x11route_fallback_test.go — the route-resolution guards for the defect
// "`gopi tool act info` → connect … no such file or directory".
//
// THE DEFECT (measured on zbook, 2026-09-20): `DISPLAY=localhost:17.0` (an SSH
// forward whose forward is gone) made every X11 path fail with ENOENT — while
// the SAME HOST had live displays (:1, :98) and the same display NUMBER was
// often served locally by the durable Xvfb. The resolver mapped a `localhost`
// host to the unix socket ONLY and never looked at TCP, and a display whose
// socket was gone never looked for the same number over the other local
// transport. This file pins the corrected resolution:
//
//	1. the route ORDER (the transport the DISPLAY names first, the other local
//	   transport only as a fallback — and no fabricated route for a remote host);
//	2. the FALLBACK actually reaching a server that answers on it, through the
//	   same entry points the CLI uses (dialX11 for capture, OpenXActuator for
//	   `gopi tool act`) — a test that only checks the route TABLE would pass with a
//	   resolver that computes the right list and never dials it;
//	3. the LOUD failure when nothing answers: every route tried, named with its
//	   own error, plus this host's live displays as the remedy (Rule 15 — the
//	   bare `no such file or directory` was the whole complaint).
//
// The fixtures reuse the fake X server in cov_shot_x11_test.go (a protocol-
// level server that replays the connection-setup reply and answers the requests
// the act path makes), started on TCP instead of the unix socket so a test can
// exercise the transport the DISPLAY does NOT name.
package shot

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	// covRouteDisplayMin/Max bound the display numbers this file's fixtures may
	// claim. They sit ABOVE the fake-server range in cov_shot_x11_test.go
	// (covFakeDisplayMin..Max) so the two fixture families can never contend
	// for one display number.
	covRouteDisplayMin = 270
	covRouteDisplayMax = 299
)

// covRouteTCPAddr is the TCP address X11 uses for a display number
// (6000+N), built from the product constant — never an inline 6000.
func covRouteTCPAddr(num int) string {
	return net.JoinHostPort(hostLocalForTests, strconv.Itoa(x11TCPPortBase+num))
}

// hostLocalForTests is this machine's address as a DISPLAY host would spell it
// (the fixture binds it literally; the product's parser accepts "127.0.0.1" and
// "localhost" alike).
const hostLocalForTests = "127.0.0.1"

// covRouteFreeDisplay returns a ":N" and its bare number for which BOTH local
// transports are free: no unix socket at the protocol path, no /tmp/.XN-lock,
// and the TCP port can be bound. The TCP bind is the proof (not a guess): the
// port is closed immediately and the fixture binds it for real a moment later.
func covRouteFreeDisplay(t *testing.T) (string, int) {
	t.Helper()
	if _, err := os.Stat(x11UnixSocketDir); err != nil {
		t.Skipf("no %s directory — cannot host a fake X server: %v", x11UnixSocketDir, err)
	}
	for n := covRouteDisplayMin; n <= covRouteDisplayMax; n++ {
		num := strconv.Itoa(n)
		if _, err := os.Stat(filepath.Join(x11UnixSocketDir, x11UnixSocketPrefix+num)); err == nil {
			continue
		}
		if _, err := os.Stat(covXLockPath(num)); err == nil {
			continue
		}
		ln, err := net.Listen("tcp", covRouteTCPAddr(n))
		if err != nil {
			continue
		}
		//stylegate:ignore the probe listener is closed the moment the port is proven free; the fixture rebinds it, and a failed rebind skips VIBLY
		ln.Close()
		return ":" + num, n
	}
	t.Skipf("no display number in :%d-:%d with both local transports free — fallback fixture skipped VIBLY",
		covRouteDisplayMin, covRouteDisplayMax)
	return "", 0
}

// startCovFakeXServerTCP serves the fake X server on TCP (127.0.0.1:6000+N)
// instead of the unix socket, so a test can prove the resolver reaches a
// display whose NAMED transport (the local socket) is dead.
func startCovFakeXServerTCP(t *testing.T, display string, num int) *covFakeXServer {
	t.Helper()
	ln, err := net.Listen("tcp", covRouteTCPAddr(num))
	if err != nil {
		t.Skipf("cannot bind the fake TCP X server at %s: %v", covRouteTCPAddr(num), err)
	}
	s := &covFakeXServer{
		t:        t,
		ln:       ln,
		display:  display,
		layout:   covUsFakeLayout(),
		shutdown: make(chan struct{}),
	}
	t.Cleanup(func() {
		close(s.shutdown)
		s.ln.Close() //stylegate:ignore test cleanup: the fake server's listener is closed after its shutdown channel, which unwinds the accept loop
		s.wg.Wait()
	})
	s.wg.Add(1)
	go s.serve()
	return s
}

// TestX11RoutesPutsTheNamedTransportFirst pins the resolution TABLE: the
// transport the DISPLAY spells is the primary route (and no fallback is
// fabricated for a remote host).
func TestX11RoutesPutsTheNamedTransportFirst(t *testing.T) {
	cases := []struct {
		display     string
		wantPrimary string
		wantRoutes  int
	}{
		{":31", "unix", 2},
		{"unix:31", "unix", 2},
		{"localhost:31.0", "tcp", 2},
		{"127.0.0.1:31", "tcp", 2},
		{"xhost.example:31", "tcp", 1}, // a REMOTE host: no invented local route
	}
	for _, tc := range cases {
		d, err := parseX11Display(tc.display)
		if err != nil {
			t.Fatalf("parseX11Display(%q): %v", tc.display, err)
		}
		routes := x11Routes(d)
		if len(routes) != tc.wantRoutes {
			t.Errorf("x11Routes(%q) = %d route(s), want %d: %+v", tc.display, len(routes), tc.wantRoutes, routes)
			continue
		}
		if routes[0].network != tc.wantPrimary {
			t.Errorf("x11Routes(%q) primary = %q, want %q", tc.display, routes[0].network, tc.wantPrimary)
		}
		if routes[0].fallback {
			t.Errorf("x11Routes(%q): the named transport must NOT be marked a fallback", tc.display)
		}
		for _, r := range routes[1:] {
			if !r.fallback {
				t.Errorf("x11Routes(%q): route %+v is not the named one and is not marked a fallback", tc.display, r)
			}
		}
	}
}

// TestDialX11FallsBackToTCPWhenTheLocalSocketIsGone is the regression guard for
// the epic's everyday shape: `:N` (or a dead `localhost:N`) whose unix socket is
// gone while the TCP transport answers. Before the fix this dial failed with
// "no such file or directory" and the display's live server was never looked
// for; after it, the fallback route is taken AND the substitution is visible in
// the name the client echoes.
func TestDialX11FallsBackToTCPWhenTheLocalSocketIsGone(t *testing.T) {
	display, num := covRouteFreeDisplay(t)
	startCovFakeXServerTCP(t, display, num)

	c, err := dialX11(display)
	if err != nil {
		t.Fatalf("dialX11(%q) did not fall back to the TCP route: %v", display, err)
	}
	defer c.close()
	if want := display + x11RouteLabelFallback; c.connectedDisplay != want {
		t.Errorf("connectedDisplay = %q, want %q — a fallback route must be VISIBLE, never echoed as the display the caller asked for",
			c.connectedDisplay, want)
	}
}

// TestDialX11FallsBackToTheLocalSocketForALocalhostDisplay is the other half:
// the DISPLAY names `localhost:N` (TCP over a forward that has died) while the
// same display NUMBER is served locally on the unix socket. That is the zbook
// case the worker's display preflight reports as an unreachable GUI, and the
// local server is the display the operator meant.
func TestDialX11FallsBackToTheLocalSocketForALocalhostDisplay(t *testing.T) {
	srv := startCovFakeXServer(t, covUsFakeLayout()) // binds the unix socket at :N
	num := strings.TrimPrefix(srv.display, ":")
	requested := x11HostLocalhost + ":" + num

	c, err := dialX11(requested)
	if err != nil {
		t.Fatalf("dialX11(%q) did not fall back to the local unix socket: %v", requested, err)
	}
	defer c.close()
	if want := requested + x11RouteLabelFallback; c.connectedDisplay != want {
		t.Errorf("connectedDisplay = %q, want %q", c.connectedDisplay, want)
	}
}

// TestOpenXActuatorReachesAFallbackRoute is the guard at the seam `gopi tool act`
// actually uses (OpenXActuator), not just at the dial helper: the fallback must
// carry through to a usable actuator — XTEST queried over the fallback route,
// and a request answered on it.
func TestOpenXActuatorReachesAFallbackRoute(t *testing.T) {
	display, num := covRouteFreeDisplay(t)
	startCovFakeXServerTCP(t, display, num)

	a, err := OpenXActuator(display)
	if err != nil {
		t.Fatalf("OpenXActuator(%q) did not fall back to the TCP route: %v", display, err)
	}
	defer a.Close()
	if want := display + x11RouteLabelFallback; a.Display() != want {
		t.Errorf("OpenXActuator(%q).Display() = %q, want %q", display, a.Display(), want)
	}
	// A request answered over the fallback route proves the connection is not
	// merely open (the fake answers GetInputFocus; an unimplemented request
	// would be an X11 error, i.e. a non-nil err here).
	if _, err := a.Focus(); err != nil {
		t.Errorf("a request over the fallback route failed: %v", err)
	}
}

// TestDialX11ErrorNamesEveryRouteAndTheRemedy is the Rule 15 guard on the
// failure path: with nothing answering, the error must name EVERY route it
// tried with its own cause (a dead socket reads differently from a live server
// refusing) and, when this host has live displays, name them as the remedy. The
// reported defect was precisely that this message said only "no such file or
// directory".
func TestDialX11ErrorNamesEveryRouteAndTheRemedy(t *testing.T) {
	display, num := covRouteFreeDisplay(t)
	_, err := dialX11(display)
	if err == nil {
		t.Fatalf("dialX11(%q) succeeded with no server on either local transport", display)
	}
	msg := err.Error()
	for _, want := range []string{
		"no route answered",
		filepath.Join(x11UnixSocketDir, x11UnixSocketPrefix+strconv.Itoa(num)),
		covRouteTCPAddr(num),
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not name %q:\n%s", want, msg)
		}
	}
	// The remedy half, when this host has a reachable display. The hint is
	// recomputed here (the same function the error used) so the assertion is
	// about the shipped wording, not a hardcoded display number.
	if hint := x11LiveDisplayHint(); hint != "" && !strings.Contains(msg, hint) {
		t.Errorf("error omits this host's live display(s) %q:\n%s", hint, msg)
	}
}

// TestX11LiveDisplayHintNamesOnlyReachableDisplays pins the hint's contract:
// everything it names answers an X11 handshake, the list is capped, and it
// never speaks when the host has no display. (The hint is a REMEDY appended to
// an error that is already decided — a wrong name in it would send the reader
// into a second failure.)
func TestX11LiveDisplayHintNamesOnlyReachableDisplays(t *testing.T) {
	hint := x11LiveDisplayHint()
	if hint == "" {
		t.Skip("no live X11 display on this host — the hint's positive direction is not applicable (the empty-answer direction is pinned by TestDialX11ErrorNamesEveryRouteAndTheRemedy)")
	}
	names := strings.Split(hint, ",")
	if len(names) > x11HintMaxDisplays {
		t.Errorf("hint named %d displays, the cap is %d: %q", len(names), x11HintMaxDisplays, hint)
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, ":") {
			t.Errorf("hint named %q, which is not a local display name", name)
			continue
		}
		c, err := dialX11Routes(name, false)
		if err != nil {
			t.Errorf("hint named %q, which does not answer: %v", name, err)
			continue
		}
		c.close()
	}
}
