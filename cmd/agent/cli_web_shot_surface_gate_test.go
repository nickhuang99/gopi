// cli_web_shot_surface_gate_test.go — the POSTURE gate for the page-capture
// endpoint: it may ask for exactly ONE surface, and this file is where that
// claim is machine-checked instead of promised.
//
// WHY A SOURCE GATE ON TOP OF THE BEHAVIOURAL TEST: the behavioural test
// (TestWebShotAlwaysAsksForTheWebSurfaceOfItsOwnPage) reads the options ONE
// capture call received. That catches a flipped constant on the path it drives.
// It cannot catch a SECOND path — a fallback branch, a helper added later, a
// `--surface` string handed to a child process — because such a branch only
// shows up when something drives it, and a privacy regression that hides behind
// an untested branch is exactly the kind that would ship.
//
// So the endpoint's whole implementation file is scanned for the vocabulary of
// the other two surfaces. The scan is over the FILE, comments included: a
// comment that names a surface this endpoint cannot reach is how a later reader
// learns the endpoint takes options it does not.
package main

import (
	"os"
	"strings"
	"testing"
)

// webShotGateFiles are the files that implement the endpoint and the page-side
// button. The page file (cli_web.go) is enormous and predates this feature, so
// the gate scans only the endpoint's own file — the page's half is pinned
// behaviourally by the harness scenarios (cli_web_page_scenarios.js, R6) plus
// the constant-agreement test.
var webShotGateFiles = []string{"cli_web_shot.go"}

// webShotForbiddenTokens are the spellings that would mean "somebody wired a
// second surface in". They are spelled HERE as data — the gate's own list is the
// only place they may appear (an exhaustive-gate pattern, Rule 13).
var webShotForbiddenTokens = []string{
	"SurfaceScreen",
	"SurfaceChat",
	"surface screen",
	"surface chat",
	"--surface screen",
	"--surface chat",
	"screen_capture",
	"ScreenCapture",
}

// TestWebShotNeverNamesANonPageSurface is the RED-PROOFABLE gate: introduce any
// forbidden spelling into cli_web_shot.go and this test fails, naming the token
// and the line.
func TestWebShotNeverNamesANonPageSurface(t *testing.T) {
	for _, name := range webShotGateFiles {
		raw, err := os.ReadFile(name) //nolint:gosec // a fixed file in this package
		if err != nil {
			t.Fatalf("cannot read %s: %v", name, err)
		}
		src := string(raw)
		if strings.TrimSpace(src) == "" {
			t.Fatalf("%s is empty — the gate would pass vacuously", name)
		}
		// Non-vacuity: the file MUST mention the web surface, or this gate is
		// scanning a file that does not implement the capture at all.
		if !strings.Contains(src, "shot.SurfaceWeb") {
			t.Errorf("%s does not name shot.SurfaceWeb — the gate is scanning the wrong file", name)
		}
		for i, line := range strings.Split(src, "\n") {
			for _, tok := range webShotForbiddenTokens {
				if strings.Contains(line, tok) {
					t.Errorf("%s:%d names %q — the page-capture endpoint may only ever ask for the web surface of its own page:\n\t%s",
						name, i+1, tok, strings.TrimSpace(line))
				}
			}
		}
	}
}

// TestWebShotRequestTakesOnlyTheViewport is the SSRF half of the same posture,
// asserted on the SHAPE of the endpoint's request type — and it is a REWRITE,
// not a removal.
//
// The rule it guards is unchanged: there must be no field a client could send
// that redirects the capture. What changed is the SET of permitted knobs: the
// geometry is legitimately the caller's (a phone-sized window should not get a
// 1440px screenshot), and it cannot redirect anything — ViewportW/H reach
// internal/shot's Options and not the URL, the surface or the address.
//
// So the assertion is an EXACT SHAPE rather than an emptiness: the request type
// carries exactly ONE field, `Viewport`, of the one viewport type, and that type
// carries exactly w and h. A struct that grew `URL`, `Addr`, `Surface` — or any
// second field at all — turns this red, which is the option this endpoint must
// not have. The BEHAVIOURAL half ({"url":…} / {"surface":…} still answer 400 and
// name the field) is TestWebShotRefusesAClientSuppliedTarget.
func TestWebShotRequestTakesOnlyTheViewport(t *testing.T) {
	src, body := requestTypeBody(t, "cli_web_shot.go", "type webShotRequest struct {")
	// Non-vacuity: a scan whose type is empty cannot pass by having nothing to
	// compare against.
	if strings.TrimSpace(body) == "" {
		t.Fatalf("webShotRequest has no fields at all — this gate is asserting a shape that carries no option, so it would pass over a rewritten type")
	}
	if body != webShotOnlyField {
		t.Errorf("webShotRequest is %q, want exactly %q — the capture target must not be settable from the request (SSRF/privacy), so `viewport` is the ONLY permitted knob",
			body, webShotOnlyField)
	}
	// The type of that one field, pinned the same way: a `*webShotViewport` that
	// grew a third member would be a second option hiding behind one field name.
	vpBody := requestTypeBodyOrFail(t, src, "type webShotViewport struct {")
	if vpBody != webShotViewportFields {
		t.Errorf("webShotViewport is %q, want exactly %q — the geometry object carries w and h and nothing else",
			vpBody, webShotViewportFields)
	}
}

// The exact shapes the gate above pins. Spelled as data so the assertion and the
// declaration are one string a reader can compare (Rule 13: an exhaustive gate
// keeps its expectation in one place).
const (
	webShotOnlyField      = "Viewport *webShotViewport `json:\"viewport\"`"
	webShotViewportFields = "W int `json:\"w\"`\n\tH int `json:\"h\"`"
)

// requestTypeBody returns a source file's text and the TRIMMED body of the named
// type declaration.
func requestTypeBody(t *testing.T, file, decl string) (string, string) {
	t.Helper()
	raw, err := os.ReadFile(file) //nolint:gosec // a fixed file in this package
	if err != nil {
		t.Fatalf("cannot read %s: %v", file, err)
	}
	src := string(raw)
	i := strings.Index(src, decl)
	if i < 0 {
		t.Fatalf("the type %q is gone from %s — this gate has nothing to scan", decl, file)
	}
	rest := src[i+len(decl):]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatalf("the body of %q is unterminated", decl)
	}
	return src, strings.TrimSpace(rest[:end])
}

// requestTypeBodyOrFail is requestTypeBody for a type already known to exist in
// the source read once — it re-reads nothing and fails the test if the type is
// missing.
func requestTypeBodyOrFail(t *testing.T, src, decl string) string {
	t.Helper()
	i := strings.Index(src, decl)
	if i < 0 {
		t.Fatalf("the type %q is gone — this gate has nothing to scan", decl)
	}
	rest := src[i+len(decl):]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatalf("the body of %q is unterminated", decl)
	}
	return strings.TrimSpace(rest[:end])
}
