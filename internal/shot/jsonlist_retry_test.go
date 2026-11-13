package shot

// jsonlist_retry_test.go — the /json/list page-target REGISTRATION race.
//
// Chrome prints its "DevTools listening on ws://…" stderr line roughly 100 ms
// BEFORE it registers the page target, so the FIRST GET of /json/list
// routinely answers an EMPTY list on a perfectly healthy chrome. A single-shot
// discovery therefore aborts capture on a healthy browser (observed: 1/12
// captures failed before the fix this file guards).
//
// The tests here take the only honest instrument available for that timing
// window: a fake CDP endpoint whose discovery answer is scripted per call
// (poll 1 → empty list, later polls → the real page target), so the race is
// reproduced deterministically with no real browser and no timing luck.

import (
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Named constants (Rule 1) — no magic literals in this file.
const (
	// jsonListEmptyBody is what a healthy chrome answers on the FIRST
	// /json/list GET while the page target is still registering.
	jsonListEmptyBody = "[]"

	// jsonListPageTargetBodyTemplate is a one-entry discovery list carrying a
	// page target whose WebSocket URL points back at the answering server.
	// %s is the listener port.
	jsonListPageTargetBodyTemplate = `[{"id":"T1","type":"page","url":"about:blank",` +
		`"webSocketDebuggerUrl":"ws://127.0.0.1:%s/devtools/page/T1"}]`

	// jsonListRetryBudget is the test-side watchdog: production bounds the poll
	// with discoveryPollTimeout, so this only has to be comfortably larger.
	jsonListRetryBudget = 5 * time.Second

	// jsonListMinPolls is the number of discovery GETs that proves a retry
	// happened: one empty answer plus at least one further attempt.
	jsonListMinPolls = 2

	// jsonListNoPageTargetFragment is the discovery failure text callers and
	// tests match on (it is part of the contract, errNoPageTarget's value).
	jsonListNoPageTargetFragment = "no page target"

	// jsonListFakePNGFiller is filler appended to the PNG magic so the
	// scripted screenshot payload passes the byte-level PNG guard.
	jsonListFakePNGFiller = 64

	// jsonListRetryMinElapsed is the floor proving a transport failure was
	// RETRIED (not returned on the first attempt) — well under
	// discoveryPollTimeout, well over one discoveryPollInterval.
	jsonListRetryMinElapsed = 500 * time.Millisecond
)

// jsonListFakeCDPServer serves a fake chrome CDP endpoint on one listener,
// counting every HTTP discovery GET and letting the caller script the answer
// per poll. The WebSocket half answers CDP frames with respondShot, so a test
// can drive the pipeline past discovery or keep it discovery-only.
func jsonListFakeCDPServer(
	t *testing.T,
	polls *atomic.Int64,
	discovery func(poll int, port string) string,
	respondShot func(method string) (any, bool),
) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	//stylegate:ignore test cleanup: the fake listener is closed at teardown so the accept loop unwinds; a close error cannot leave the test with a reportable failure
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go jsonListFakeConn(conn, polls, discovery, respondShot)
		}
	}()
	return ln.Addr().String()
}

// jsonListFakeConn demuxes the discovery GET from the WebSocket upgrade on one
// connection, answering the former with the scripted body.
func jsonListFakeConn(
	conn net.Conn,
	polls *atomic.Int64,
	discovery func(poll int, port string) string,
	respondShot func(method string) (any, bool),
) {
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
		serveFakeWS(conn, respondShot)
		return
	}
	poll := int(polls.Add(1))
	_, portStr, _ := net.SplitHostPort(conn.LocalAddr().String()) //stylegate:ignore splits the fixture listener's own address; the derived port is the assertion
	body := discovery(poll, portStr)
	conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: " + //nolint:errcheck
		itoa(len(body)) + "\r\n\r\n" + body))
}

// jsonListPNGResponder answers a CDP session with a tiny valid PNG, so the
// scripted capture can run to completion.
func jsonListPNGResponder() func(method string) (any, bool) {
	png := append([]byte(testPNGMagic), make([]byte, jsonListFakePNGFiller)...)
	return func(method string) (any, bool) {
		if method == "Page.captureScreenshot" {
			return map[string]any{"data": base64Std(png)}, true
		}
		return map[string]any{}, true
	}
}

// TestCaptureOverCDPDiscoveryPollsUntilPageTarget is the regression guard for
// the page-target registration race: the fake answers an EMPTY discovery list
// on the first GET (exactly what a healthy chrome does for ~100 ms) and the
// capture MUST keep polling until the page target appears — i.e. it must
// SUCCEED, on more than one discovery GET.
//
// On a single-shot implementation this test fails at the very first assertion
// (the capture dies with "no page target in /json/list"), which is the
// behaviour a healthy browser must never produce.
func TestCaptureOverCDPDiscoveryPollsUntilPageTarget(t *testing.T) {
	polls := &atomic.Int64{}
	addr := jsonListFakeCDPServer(t, polls, func(poll int, port string) string {
		if poll == 1 {
			return jsonListEmptyBody // chrome launched, page target not registered yet
		}
		return fmt.Sprintf(jsonListPageTargetBodyTemplate, port)
	}, jsonListPNGResponder())

	done := make(chan error, 1)
	go func() {
		got, err := captureOverCDP(portOnly(addr), "data:text/html,<h1>x</h1>", Options{WaitSeconds: 0}, prepared{})
		if err == nil && len(got) == 0 {
			err = fmt.Errorf("capture returned an empty payload")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("an empty first /json/list must be retried, not fatal: %v", err)
		}
	case <-time.After(jsonListRetryBudget):
		t.Fatalf("capture neither finished nor failed within %s", jsonListRetryBudget)
	}
	if n := polls.Load(); n < jsonListMinPolls {
		t.Fatalf("/json/list polled %d time(s) — want at least %d: a single-shot GET is the "+
			"page-target registration race that aborts capture on a healthy chrome",
			n, jsonListMinPolls)
	}
}

// TestCaptureOverCDPPageLessListStillFailsBounded — a discovery endpoint that
// NEVER registers a page target must still fail (with the contract error text)
// and must do so within the bounded poll budget, never spin forever.
func TestCaptureOverCDPPageLessListStillFailsBounded(t *testing.T) {
	polls := &atomic.Int64{}
	addr := jsonListFakeCDPServer(t, polls, func(int, string) string { return jsonListEmptyBody },
		jsonListPNGResponder())
	done := make(chan error, 1)
	go func() {
		_, err := captureOverCDP(portOnly(addr), "data:text/html,<h1>x</h1>", Options{WaitSeconds: 0}, prepared{})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), jsonListNoPageTargetFragment) {
			t.Fatalf("a page-less discovery list must fail with %q, got %v",
				jsonListNoPageTargetFragment, err)
		}
	case <-time.After(jsonListRetryBudget):
		t.Fatalf("page-less discovery neither failed nor finished within %s", jsonListRetryBudget)
	}
}

// TestCaptureOverCDPDiscoveryFailureFailsBounded covers the OTHER failure arm of
// the poll: an endpoint that never serves at all (transport error on every
// attempt, not a served-but-page-less list). The poll must retry that too — the
// registration race and a not-yet-ready listener are the same user-visible
// shape — and it must give up at the budget with the DISCOVERY error, never the
// errNoPageTarget text and never an unbounded spin.
//
// The address is bound and immediately released, so the port is valid but
// refuses connections for the whole poll.
func TestCaptureOverCDPDiscoveryFailureFailsBounded(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = waitPageTarget(cdpLoopbackHTTPPrefix + portOnly(addr))
	if err == nil {
		t.Fatal("a never-serving discovery endpoint must fail, not return a target")
	}
	if strings.Contains(err.Error(), jsonListNoPageTargetFragment) {
		t.Fatalf("a transport failure must surface the discovery error, not %q: %v",
			jsonListNoPageTargetFragment, err)
	}
	// Bounded, not unbounded: the poll must not return before it has spent a
	// meaningful part of its budget retrying, and it must not exceed it wildly.
	if elapsed := time.Since(start); elapsed < jsonListRetryMinElapsed {
		t.Fatalf("discovery failed after %s — a transport error was not retried for the "+
			"bounded budget (%s)", elapsed, jsonListRetryMinElapsed)
	}
}
