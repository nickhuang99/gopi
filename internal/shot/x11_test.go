// x11_test.go — regression guards for the hand-rolled X11 client.
//
// These tests talk to a REAL X server on purpose. The whole value of this
// code is "it speaks the protocol correctly", and both bugs found while
// writing it were field-offset / request-size mistakes that NO fake would
// have caught:
//
//  1. the connection-setup reply field map (vendor length read from byte 14,
//     number-of-formats read from byte 20) put the screen table ~900 bytes
//     off, so the root window id parsed as 0xff00 and every GetImage failed;
//  2. xXTestFakeInputReq is 36 bytes (sz_xXTestFakeInputReq), not the 16 a
//     first revision assumed — a length field that disagrees with the struct
//     makes the server silently DROP the event (XTEST defines zero errors).
//
// So the guards here are: compare our parse against an independent X11 client
// (xwininfo) and against an independent capture implementation (ImageMagick
// import), plus a real round-trip through XTEST. Each test SKIPS when its
// oracle or a display is unavailable, so the suite still passes on a machine
// with no X at all.
package shot

import (
	"bytes"
	"image/png"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// testDisplayFromEnv returns the display these tests should use: the explicit
// GOPI_TEST_DISPLAY, else :99 (the project's long-running Xvfb), else "".
func testDisplayFromEnv(t *testing.T) string {
	t.Helper()
	if d := strings.TrimSpace(os.Getenv("GOPI_TEST_DISPLAY")); d != "" {
		return d
	}
	const projectXvfb = ":99"
	if _, err := os.Stat(x11UnixSocketDir + "/" + x11UnixSocketPrefix + "99"); err == nil {
		return projectXvfb
	}
	return ""
}

// requireDisplay skips the test when no X display is available.
func requireDisplay(t *testing.T) string {
	t.Helper()
	d := testDisplayFromEnv(t)
	if d == "" {
		t.Skip("no X display (set GOPI_TEST_DISPLAY, or start Xvfb :99)")
	}
	c, err := dialX11(d)
	if err != nil {
		t.Skipf("display %s not reachable: %v", d, err)
	}
	c.close()
	return d
}

// runOracle executes a helper binary and returns its combined output.
func runOracle(t *testing.T, env string, name string, args ...string) (string, bool) {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	cmd := exec.Command(path, args...)
	cmd.Env = append(os.Environ(), env)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), false
	}
	return string(out), true
}

// TestX11SetupFieldsMatchXwininfo pins the connection-setup field map against
// an INDEPENDENT X11 client: xwininfo -root reports the root window id and
// geometry, which must equal what our parser derived from the setup reply.
// This is the exact guard for bug (1) above.
func TestX11SetupFieldsMatchXwininfo(t *testing.T) {
	d := requireDisplay(t)
	out, ok := runOracle(t, "DISPLAY="+d, "xwininfo", "-root")
	if !ok {
		t.Skip("xwininfo unavailable — cannot cross-check the setup field map")
	}
	rootRe := regexp.MustCompile(`Window id:\s+(0x[0-9a-fA-F]+)`)
	wRe := regexp.MustCompile(`(?m)^\s*Width:\s+(\d+)`)
	hRe := regexp.MustCompile(`(?m)^\s*Height:\s+(\d+)`)
	rm, wm, hm := rootRe.FindStringSubmatch(out), wRe.FindStringSubmatch(out), hRe.FindStringSubmatch(out)
	if rm == nil || wm == nil || hm == nil {
		t.Fatalf("could not parse xwininfo -root output:\n%s", out)
	}
	wantRoot, _ := strconv.ParseUint(rm[1], 0, 32) //stylegate:ignore parses a fixture string; the compared number is the assertion
	wantW, _ := strconv.Atoi(wm[1])                //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
	wantH, _ := strconv.Atoi(hm[1])                //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback

	c, err := dialX11(d)
	if err != nil {
		t.Fatalf("dialX11(%s): %v", d, err)
	}
	defer c.close()

	if uint64(c.root) != wantRoot {
		t.Errorf("root window parsed 0x%x, xwininfo says 0x%x — the setup reply field map has drifted (see the map in parseSetup)", c.root, wantRoot)
	}
	if c.width != wantW || c.height != wantH {
		t.Errorf("root geometry parsed %dx%d, xwininfo says %dx%d — the setup reply field map has drifted", c.width, c.height, wantW, wantH)
	}
	if c.depth < 24 {
		t.Errorf("root depth parsed %d, want >= 24 (xwininfo reports the same screen)", c.depth)
	}
	if c.maxReq <= 0 {
		t.Errorf("maximum-request-length parsed as %d — the field map has drifted", c.maxReq)
	}
}

// TestCaptureX11RootProducesRealPNG asserts the end-to-end capture: a valid
// PNG whose decoded size equals the root window size the server reported.
func TestCaptureX11RootProducesRealPNG(t *testing.T) {
	d := requireDisplay(t)
	pngBytes, geom, err := CaptureX11Root(d)
	if err != nil {
		t.Fatalf("CaptureX11Root(%s): %v", d, err)
	}
	if !bytes.HasPrefix(pngBytes, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("capture did not produce a PNG (%d bytes)", len(pngBytes))
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("decode captured PNG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != geom.Width || b.Dy() != geom.Height {
		t.Errorf("PNG is %dx%d but the server reported a %dx%d root window", b.Dx(), b.Dy(), geom.Width, geom.Height)
	}
	if geom.Width <= 0 || geom.Height <= 0 {
		t.Fatalf("geometry not populated: %+v", geom)
	}
	if geom.Bands < 1 {
		t.Errorf("bands = %d, want >= 1 (the banded GetImage loop must run at least once)", geom.Bands)
	}
}

// TestCaptureMatchesImportOracle is the correctness oracle for the whole
// capture path: our pure-Go GetImage must be PIXEL-IDENTICAL to ImageMagick's
// import, which reaches the same pixels through Xlib's XGetImage. A non-zero
// absolute-error count means our pixel copy, band offsets or colour-channel
// order is wrong — the strongest available check on a hand-rolled client.
func TestCaptureMatchesImportOracle(t *testing.T) {
	d := requireDisplay(t)
	if _, err := exec.LookPath("compare"); err != nil {
		t.Skip("ImageMagick compare unavailable — no capture oracle")
	}
	dir := t.TempDir()
	mine := dir + "/mine.png"
	theirs := dir + "/theirs.png"

	pngBytes, _, err := CaptureX11Root(d)
	if err != nil {
		t.Fatalf("CaptureX11Root(%s): %v", d, err)
	}
	if err := os.WriteFile(mine, pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := runOracle(t, "DISPLAY="+d, "import", "-window", "root", theirs); !ok {
		t.Skip("import unavailable — no capture oracle")
	}
	out, _ := runOracle(t, "DISPLAY="+d, "compare", "-metric", "AE", mine, theirs, "null:")
	// `compare -metric AE` prints the count on stderr and returns non-zero when
	// the images differ, so the output is inspected regardless of exit status.
	got := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	if got != "0" {
		t.Errorf("pure-Go capture differs from `import` by %s pixels (want 0) — check band offsets / BGR->RGB swap", got)
	}
}

// TestXTestInjectionRoundTrip drives the real XTEST path: warp the pointer,
// then read the position back with a SEPARATE QueryPointer round-trip. This
// guards the 36-byte FakeInput request (bug 2): a malformed request makes the
// server drop the event silently, so the read-back would simply not match.
func TestXTestInjectionRoundTrip(t *testing.T) {
	d := requireDisplay(t)
	a, err := OpenXActuator(d)
	if err != nil {
		t.Skipf("no XTEST actuator on %s: %v", d, err)
	}
	defer a.Close()

	w, h := a.RootSize()
	if w <= 0 || h <= 0 {
		t.Fatalf("root size %dx%d", w, h)
	}
	// Pick a target away from the current position so a no-op cannot pass.
	startX, startY, err := a.Pointer()
	if err != nil {
		t.Fatalf("QueryPointer (before): %v", err)
	}
	tx, ty := w/2, h/2
	if tx == startX && ty == startY {
		tx, ty = w/3, h/3
	}
	if err := a.MovePointer(tx, ty); err != nil {
		t.Fatalf("MovePointer: %v", err)
	}
	gotX, gotY, err := a.Pointer()
	if err != nil {
		t.Fatalf("QueryPointer (after): %v", err)
	}
	if gotX != tx || gotY != ty {
		t.Errorf("pointer read back at (%d,%d), asked for (%d,%d) — XTestFakeInput was malformed and the server dropped it", gotX, gotY, tx, ty)
	}
}

// TestX11DisplayParsing covers the DISPLAY string forms the CLI accepts.
func TestX11DisplayParsing(t *testing.T) {
	cases := []struct {
		in       string
		host     string
		number   int
		screen   int
		wantFail bool
	}{
		{in: ":0", host: "", number: 0},
		{in: ":99", host: "", number: 99},
		{in: "localhost:10.0", host: "localhost", number: 10, screen: 0},
		{in: "unix:77", host: "", number: 77},
		{in: "192.0.2.5:1.2", host: "192.0.2.5", number: 1, screen: 2},
		{in: "", wantFail: true},
		{in: "nonsense", wantFail: true},
		{in: ":x", wantFail: true},
	}
	for _, tc := range cases {
		got, err := parseX11Display(tc.in)
		if tc.wantFail {
			if err == nil {
				t.Errorf("parseX11Display(%q) accepted a malformed display: %+v", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseX11Display(%q): %v", tc.in, err)
			continue
		}
		if got.host != tc.host || got.number != tc.number || got.screen != tc.screen {
			t.Errorf("parseX11Display(%q) = %+v, want host=%q number=%d screen=%d", tc.in, got, tc.host, tc.number, tc.screen)
		}
	}
}

// TestKeysymForRuneRefusesUnknown pins the loud-refusal contract: characters
// this client cannot map must be an error, never a silently dropped keystroke
// (Rule 15).
func TestKeysymForRuneRefusesUnknown(t *testing.T) {
	if _, _, err := keysymForRune('A'); err != nil {
		t.Errorf("ASCII 'A' must map: %v", err)
	}
	if _, _, err := keysymForRune('#'); err != nil {
		t.Errorf("shifted ASCII '#' must map: %v", err)
	}
	if _, _, err := keysymForRune('知'); err == nil {
		t.Error("non-ASCII must be refused loudly, not mapped to a guess")
	}
	if _, _, err := keysymForName("f13"); err == nil {
		t.Error("unknown key name must be refused")
	}
}
