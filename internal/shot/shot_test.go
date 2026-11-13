package shot

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPlaywrightVersion extracts the numeric segment from cache paths.
func TestPlaywrightVersion(t *testing.T) {
	cases := map[string]string{
		"/home/x/.cache/ms-playwright/chromium-1228/chrome-linux64/chrome": "1228",
		"/home/x/.cache/ms-playwright/chromium-9/chrome-linux/chrome":      "9",
		"/usr/bin/chromium-browser":                                        "",
	}
	for in, want := range cases {
		if got := playwrightVersion(in); got != want {
			t.Errorf("playwrightVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// compareSign collapses an int to its -1/0/+1 sign.
func compareSign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	default:
		return 0
	}
}

// TestComparePlaywrightVersions covers the ORDERING contract findChrome's cache
// sort depends on: comparePlaywrightVersions is a NUMERIC comparison, so a
// shorter version number is the OLDER one even when it is lexically greater
// ("9" > "1228" as strings, but 9 < 1228 as versions). Every row is a real
// playwrightVersion result (a digit string, or "" for a non-matching path).
func TestComparePlaywrightVersions(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want int // the sign comparePlaywrightVersions(a, b) must return
	}{
		{"shorter number is older", "9", "1228", -1},
		{"newer number wins", "1228", "9", 1},
		{"equal numbers", "1228", "1228", 0},
		{"leading zeros carry no weight", "0900", "1228", -1},
		{"same value, different padding", "0900", "900", 0},
		{"digit count beats lexical order", "99", "100", -1},
		{"same digit count compares lexically", "1228", "1229", -1},
		{"unmatched path is the weakest candidate", "", "1", -1},
		{"real version beats unmatched path", "1", "", 1},
		{"two unmatched paths rank equal", "", "", 0},
	}
	for _, tc := range cases {
		got := comparePlaywrightVersions(tc.a, tc.b)
		want := compareSign(tc.want)
		if got != want {
			t.Errorf("%s: comparePlaywrightVersions(%q, %q) = %d, want sign %d",
				tc.name, tc.a, tc.b, got, want)
		}
		// Antisymmetry: the reverse comparison must mirror the result.
		if rev := comparePlaywrightVersions(tc.b, tc.a); rev != -want {
			t.Errorf("%s: comparePlaywrightVersions(%q, %q) = %d, want %d (antisymmetry)",
				tc.name, tc.b, tc.a, rev, -want)
		}
	}
}

// writeCacheChrome materialises an executable fake chrome at the playwright
// cache path for version v (.../.cache/ms-playwright/chromium-<v>/chrome-linux/
// chrome) and returns that path.
func writeCacheChrome(t *testing.T, home, v string) string {
	t.Helper()
	p := filepath.Join(home, ".cache", "ms-playwright", "chromium-"+v, "chrome-linux", "chrome")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// TestFindChromeCacheOrderingIsNumeric — regression guard for the chromium-9 >
// chromium-1228 ordering defect: glob returns the cache hits in lexical order,
// so the version segments must be compared NUMERICALLY. A stale chromium-9
// sitting next to chromium-1228 must never win, even though "9" > "1228" when
// compared as strings. Verified RED against the raw string compare.
func TestFindChromeCacheOrderingIsNumeric(t *testing.T) {
	home := t.TempDir()
	stale := writeCacheChrome(t, home, "9")     // shorter number: lexically GREATER
	newest := writeCacheChrome(t, home, "1228") // HP's verified cache version
	t.Setenv(chromeEnvVar, "")
	t.Setenv("PATH", t.TempDir())

	bin, err := findChrome(home)
	if err != nil {
		t.Fatalf("cache candidate not found: %v", err)
	}
	if bin.Path != newest {
		t.Fatalf("findChrome picked %s, want the newest %s — the cache sort is "+
			"comparing version strings again (chromium-9 beats chromium-1228; stale=%s)",
			bin.Path, newest, stale)
	}
}

// TestFindChromeEnvOverride — GOPI_SHOT_CHROME wins over everything and is
// validated AT DISCOVERY: a path that does not exist, or that exists without an
// execute bit, errors naming the var — never a silent fallback, and never a
// failure deferred to chrome start (bug 01a08c89).
func TestFindChromeEnvOverride(t *testing.T) {
	// Each override subtest is host-independent: a non-empty override
	// short-circuits the discovery chain, so PATH/cache state cannot leak in.
	t.Run("non-executable regular file (0644)", func(t *testing.T) {
		notExec := filepath.Join(t.TempDir(), "not-executable")
		if err := os.WriteFile(notExec, []byte("#!/bin/sh\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv(chromeEnvVar, notExec)
		_, err := findChrome(t.TempDir())
		want := fmt.Sprintf(errNotExecutableFile, chromeEnvVar, notExec)
		if err == nil || err.Error() != want {
			t.Fatalf("0644 override: err = %v, want %q", err, want)
		}
	})

	t.Run("directory override", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(chromeEnvVar, dir)
		_, err := findChrome(t.TempDir())
		want := fmt.Sprintf(errNotExecutableFile, chromeEnvVar, dir)
		if err == nil || err.Error() != want {
			t.Fatalf("directory override: err = %v, want %q", err, want)
		}
	})

	t.Run("nonexistent override", func(t *testing.T) {
		bogus := filepath.Join(t.TempDir(), "no-such-chrome")
		t.Setenv(chromeEnvVar, bogus)
		_, err := findChrome(t.TempDir())
		if err == nil || !strings.Contains(err.Error(), chromeEnvVar) {
			t.Fatalf("bogus override should error naming the var, got: %v", err)
		}
	})

	t.Run("executable override honored", func(t *testing.T) {
		dir := t.TempDir()
		fake := filepath.Join(dir, "mychrome")
		if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv(chromeEnvVar, fake)
		bin, err := findChrome(t.TempDir())
		if err != nil || bin.Path != fake || bin.Source != chromeEnvVar {
			t.Fatalf("override not honored: bin=%+v err=%v", bin, err)
		}
	})

	// Empty env → falls through to the chain. Host-dependent: either a chrome
	// is found or the full attempt list is reported — never the override error.
	t.Run("empty override falls through", func(t *testing.T) {
		t.Setenv(chromeEnvVar, "")
		bin, err := findChrome(t.TempDir())
		if err == nil {
			if bin.Source == chromeEnvVar {
				t.Fatalf("empty override resolved to an env source: %+v", bin)
			}
			return
		}
		if !strings.Contains(err.Error(), msgNoChromeFound) {
			t.Fatalf("empty override should report the attempt list, got: %v", err)
		}
	})
}

// TestIsExecutableFile — the shared predicate behind all three discovery
// branches (Rule 13: one definition, referenced, not a bare mode mask).
func TestIsExecutableFile(t *testing.T) {
	dir := t.TempDir()
	exec := filepath.Join(dir, "exec")
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(exec, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"executable regular file", exec, true},
		{"non-executable regular file", plain, false},
		{"directory (even mode 0755)", dir, false},
		{"missing path", filepath.Join(dir, "absent"), false},
	}
	for _, tc := range cases {
		if got := isExecutableFile(tc.path); got != tc.want {
			t.Errorf("isExecutableFile(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// fakeCDPServer serves the two CDP halves over one listener: HTTP /json/list
// on the first request path, then a WebSocket upgrade on the same port
// (chrome uses one port for both; we route by Upgrade header).
func fakeCDPServer(t *testing.T, respond func(method string) (any, bool)) string {
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
			go handleFakeConn(conn, respond)
		}
	}()
	return ln.Addr().String()
}

// handleFakeConn demuxes HTTP vs WebSocket on one raw connection.
func handleFakeConn(conn net.Conn, respond func(string) (any, bool)) {
	defer conn.Close()
	buf := make([]byte, 8192)
	n, _ := conn.Read(buf) //stylegate:ignore reads from the fixture connection; the bytes read are the assertion
	req := string(buf[:n])
	if strings.Contains(req, "Upgrade: websocket") {
		const marker = "Sec-WebSocket-Key: "
		i := strings.Index(req, marker)
		if i < 0 {
			return // not a valid upgrade request
		}
		key := req[i+len(marker):]
		if j := strings.Index(key, "\r\n"); j >= 0 {
			key = key[:j]
		}
		accept := base64Std(sha1Sum(key + wsGUID))
		resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
		conn.Write([]byte(resp)) //nolint:errcheck
		serveFakeWS(conn, respond)
		return
	}
	// HTTP discovery: one page target pointing back at ourselves.
	_, portStr, _ := net.SplitHostPort(conn.LocalAddr().String()) //stylegate:ignore splits the fixture listener's own address; the derived port is the assertion
	body := `[{"id":"T1","type":"page","url":"about:blank","webSocketDebuggerUrl":"ws://127.0.0.1:` + portStr + `/devtools/page/T1"}]`
	resp := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: " +
		itoa(len(body)) + "\r\n\r\n" + body
	conn.Write([]byte(resp)) //nolint:errcheck
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func serveFakeWS(conn net.Conn, respond func(string) (any, bool)) {
	c := &cdpConn{conn: conn, br: newBufio(conn)}
	idSeq := 0
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
		idSeq++
		result, ok := respond(msg.Method)
		if !ok {
			result = map[string]any{}
		}
		resp, _ := jsonMarshal(map[string]any{"id": msg.ID, "result": result}) //stylegate:ignore marshals a fixture value; the stub response is not asserted for its own error
		if c.writeTextFrame(resp) != nil {
			return
		}
	}
}

// TestCaptureOverCDPFullPipeline drives captureOverCDP against a fake CDP
// server: Page.enable, setDeviceMetricsOverride, navigate, screenshot with a
// tiny valid PNG payload.
func TestCaptureOverCDPFullPipeline(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...) // magic + filler
	seen := map[string]bool{}
	addr := fakeCDPServer(t, func(method string) (any, bool) {
		seen[method] = true
		switch method {
		case "Page.captureScreenshot":
			return map[string]any{"data": base64Std(png)}, true
		default:
			return map[string]any{}, true
		}
	})
	// Point discovery at the fake server: captureOverCDP dials
	// http://127.0.0.1:<port> — the fake serves /json/list on any path.
	got, err := captureOverCDP(portOnly(addr), "data:text/html,<h1>x</h1>", Options{WaitSeconds: 0}, prepared{})
	if err != nil {
		t.Fatalf("captureOverCDP: %v", err)
	}
	if string(got) != string(png) {
		t.Fatalf("roundtrip mismatch: %d vs %d bytes", len(got), len(png))
	}
	for _, m := range []string{"Page.enable", "Emulation.setDeviceMetricsOverride", "Page.navigate", "Page.captureScreenshot"} {
		if !seen[m] {
			t.Errorf("method %s never called", m)
		}
	}
}

// TestCaptureOverCDPNotPNG — a non-PNG payload must be rejected (the #1136
// empty-shell guard, byte-checked not size-guessed).
func TestCaptureOverCDPNotPNG(t *testing.T) {
	addr := fakeCDPServer(t, func(string) (any, bool) {
		return map[string]any{"data": base64Std([]byte("GIF89a not a png at all"))}, true
	})
	if _, err := captureOverCDP(portOnly(addr), "http://x/", Options{WaitSeconds: 0}, prepared{}); err == nil || !strings.Contains(err.Error(), "not a PNG") {
		t.Fatalf("want not-a-PNG error, got %v", err)
	}
}

// TestCDPFrameRoundtrip masks/encodes a frame and decodes it back.
func TestCDPFrameRoundtrip(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	w := &cdpConn{conn: c1, br: newBufio(c1)}
	r := &cdpConn{conn: c2, br: newBufio(c2)}
	payload := []byte(`{"id":1,"method":"Page.enable"}`)
	done := make(chan struct{})
	go func() {
		defer close(done)
		op, got, err := r.readFrame()
		if err != nil || op != wsOpcodeText || string(got) != string(payload) {
			t.Errorf("roundtrip: op=%d err=%v payload=%q", op, err, got)
		}
	}()
	if err := w.writeTextFrame(payload); err != nil {
		t.Fatalf("writeTextFrame: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("roundtrip timeout")
	}
}

// TestCDPFrameLargePayload exercises the 16-bit extended length path.
func TestCDPFrameLargePayload(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	w := &cdpConn{conn: c1, br: newBufio(c1)}
	r := &cdpConn{conn: c2, br: newBufio(c2)}
	payload := strings.Repeat("x", 70000) // > 65535 → 64-bit length path
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, got, err := r.readFrame()
		if err != nil || string(got) != payload {
			t.Errorf("large frame: err=%v len=%d want=%d", err, len(got), len(payload))
		}
	}()
	if err := w.writeTextFrame([]byte(payload)); err != nil {
		t.Fatalf("writeTextFrame: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("large frame timeout")
	}
	_ = binary.BigEndian // keep import if unused elsewhere
}

// TestResolveURLSelfMissingProvider — --self without wiring errors clearly.
func TestResolveURLSelfMissingProvider(t *testing.T) {
	saved := SelfPageProvider
	SelfPageProvider = nil
	defer func() { SelfPageProvider = saved }()
	if _, _, err := resolveURL(contextBackground(), Options{Self: true}); err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("want provider error, got %v", err)
	}
	// With a provider, --self serves the page from a temp listener.
	SelfPageProvider = func() string { return "<html>SELF-TEST-PAGE</html>" }
	url, cleanup, err := resolveURL(contextBackground(), Options{Self: true})
	if err != nil {
		t.Fatalf("--self: %v", err)
	}
	defer cleanup()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf) //stylegate:ignore reads a bounded fixture body; the compared bytes are the assertion
	if !strings.Contains(string(buf[:n]), "SELF-TEST-PAGE") {
		t.Fatalf("self page not served: %q", string(buf[:n]))
	}
}

// TestResolveURLAddsScheme — bare hostnames get http:// prepended.
func TestResolveURLAddsScheme(t *testing.T) {
	url, cleanup, err := resolveURL(contextBackground(), Options{URL: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if url != "http://example.com" {
		t.Fatalf("url = %q", url)
	}
}

// Silence the unused-helper lint in constrained builds.
var _ = httptest.NewServer
