// cli_web_m2b_test.go — the page-facing half of the M2 web increments.
//
// The page is an embedded Go raw string, so two kinds of check are needed and
// neither can replace the other:
//
//  1. TestWebM2B_PageHarnessRunsTheRealPage DRIVES the shipped page (no copy):
//     cli_web_page_harness.js extracts the real <script> out of cli_web.go,
//     evaluates it against a DOM shim and runs cli_web_page_scenarios.js. That
//     is the only check that can observe page BEHAVIOUR (a queued entry
//     appearing, a menu arbitrating Enter, a fence rendering as a <pre>).
//  2. The constant-agreement gates below pin the values that TWO carriers spell
//     — the Go side and the page side. A page constant is not importable, so
//     nothing but an explicit test can stop the two from drifting (Rule 1b /
//     Rule 13 applied across the Go↔JS boundary).
package main

import (
	"github.com/nick/gopi/internal/heavytest"
	"os/exec"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// pageScript returns the page's <script> body (the code the browser runs).
func pageScript(t *testing.T) string {
	t.Helper()
	const open = "<script>"
	const close = "</script>"
	i := strings.Index(webChatPage, open)
	j := strings.LastIndex(webChatPage, close)
	if i < 0 || j < 0 || j < i {
		t.Fatalf("the embedded page has no <script> block")
	}
	return webChatPage[i+len(open) : j]
}

// TestWebM2B_PageHarnessRunsTheRealPage is the B1/B2/B3/B5/B8/B9/B12 page gate:
// it runs the SHIPPED page under node and reports the scenario tally.
//
// A missing node is an environment limitation, not a pass: the test SKIPS with
// the reason spelled out (a silent skip is a deleted test wearing a green tick).
func TestWebM2B_PageHarnessRunsTheRealPage(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	node, err := exec.LookPath("node")
	if err != nil {
		node, err = exec.LookPath("nodejs")
	}
	if err != nil {
		t.Skip("skipping the page-harness gate: no `node` on PATH (the page's behaviour is then verified only by the constant-agreement tests below)")
	}
	out, runErr := exec.Command(node, "cli_web_page_harness.js").CombinedOutput()
	text := string(out)
	if !strings.Contains(text, "PASS:") || runErr != nil {
		t.Fatalf("the page harness did not pass (err=%v):\n%s", runErr, text)
	}
	t.Logf("page harness:\n%s", text)
}

// TestWebM2B_PageCarriesTheServerConstants asserts the page's copy of every
// value BOTH carriers spell. The page cannot import Go constants, so the pair is
// checked here instead: a rename on either side fails this test rather than
// silently desynchronizing the two (the drift class Rule 13 exists to stop).
func TestWebM2B_PageCarriesTheServerConstants(t *testing.T) {
	script := pageScript(t)
	cases := []struct {
		name     string
		pageDecl string // the page-side declaration the value must appear in
		server   string // the server-side value
		why      string
	}{
		{
			name:     "steering sentinel",
			pageDecl: "const STEER_PREFIX=",
			server:   webSteerPrefix,
			why:      "the page prefixes Alt+Enter messages with the sentinel the server strips (webSteerPrefix)",
		},
		{
			name:     "menu endpoint",
			pageDecl: "const API_MENU=",
			server:   webMenuPath,
			why:      "the page fetches its candidates from the one route the server registered",
		},
		{
			name:     "menu more-flag",
			pageDecl: "const MENU_MORE_FLAG=",
			server:   webMoreFlag,
			why:      "the withheld-count flag the page renders is the server's webMoreFlag",
		},
	}
	for _, tc := range cases {
		i := strings.Index(script, tc.pageDecl)
		if i < 0 {
			t.Errorf("%s: the page no longer declares %q — %s", tc.name, tc.pageDecl, tc.why)
			continue
		}
		lit := "'" + tc.server + "'"
		line := script[i:]
		if nl := strings.IndexByte(line, '\n'); nl >= 0 {
			line = line[:nl]
		}
		if !strings.Contains(line, lit) {
			t.Errorf("%s: the page declares %q carrying %q, but the server value is %s — the two carriers have drifted (%s)",
				tc.name, tc.pageDecl, line, tc.server, tc.why)
		}
	}

	// The markdown fence is the one value the page CANNOT write literally (it
	// lives inside a Go raw string, where a backtick would end the string), so
	// the page builds it from its code point. Pin that construction to the
	// server's constant: the page's tick must be webMarkdownFence's own
	// character, its fence length must be webMarkdownFence's length, and its
	// fence must actually be built by repeating that tick that many times.
	if webMarkdownFence == "" {
		t.Fatal("webMarkdownFence must not be empty")
	}
	wantRune := int(webMarkdownFence[0])
	for i := 1; i < len(webMarkdownFence); i++ {
		if int(webMarkdownFence[i]) != wantRune {
			t.Fatalf("webMarkdownFence = %q is not one character repeated — the page's code-point construction cannot express it", webMarkdownFence)
		}
	}
	decl := "const MD_TICK_CHARCODE="
	i := strings.Index(script, decl)
	if i < 0 {
		t.Fatalf("the page no longer declares %q — the markdown fence would be unspelled", decl)
	}
	line := script[i:]
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	if !strings.Contains(line, itoa(wantRune)) {
		t.Errorf("the page's %s must be %d (webMarkdownFence %q) so both carriers spell one fence; got %q",
			decl, wantRune, webMarkdownFence, line)
	}
	if !strings.Contains(line, "MD_FENCE_LEN="+itoa(len(webMarkdownFence))) &&
		!strings.Contains(script, "MD_FENCE_LEN="+itoa(len(webMarkdownFence))) {
		t.Errorf("the page must declare MD_FENCE_LEN=%d (len(webMarkdownFence)) — the fence length is one value", len(webMarkdownFence))
	}
	// …and the page must actually BUILD the fence from the tick (a code-point
	// constant nothing uses would be Rule 21's dead-knob defect in page form),
	// repeating it exactly len(webMarkdownFence) times.
	build := "const MD_FENCE=MD_TICK"
	j := strings.Index(script, build)
	if j < 0 {
		t.Fatalf("the page must build MD_FENCE from MD_TICK (the code-point spelling of webMarkdownFence)")
	}
	buildLine := script[j:]
	if nl := strings.IndexByte(buildLine, '\n'); nl >= 0 {
		buildLine = buildLine[:nl]
	}
	if got := strings.Count(buildLine, "MD_TICK"); got != len(webMarkdownFence) {
		t.Errorf("the page's MD_FENCE repeats the tick %d time(s), want %d (len(webMarkdownFence) = %q): %q",
			got, len(webMarkdownFence), webMarkdownFence, buildLine)
	}

	// The follow threshold is one number on both carriers (the M1 test pins the
	// value itself; this pins the spelling so a rename on either side fails).
	if !strings.Contains(script, "const FOLLOW_THRESHOLD="+itoa(webFollowThreshold)+";") {
		t.Errorf("the page's FOLLOW_THRESHOLD must be the server's webFollowThreshold (%d)", webFollowThreshold)
	}
}

// TestWebM2B_PageBudgetMatchesTheServerImageCeiling pins the image budget across
// the boundary: the page's MAX_B64 is DERIVED from its decoded-byte ceiling, and
// that ceiling is the server's own decoded-byte limit — one budget, expressed
// once per carrier, with the derivation itself asserted (never a second number
// typed beside the first).
func TestWebM2B_PageBudgetMatchesTheServerImageCeiling(t *testing.T) {
	script := pageScript(t)
	// The decoded ceiling is DERIVED from the server's constant, not typed as a
	// second number: recompute the product the page's declaration spells and
	// compare it to webMaxChatImageBytes.
	decl := "const MAX_IMAGE_BYTES="
	i := strings.Index(script, decl)
	if i < 0 {
		t.Fatalf("the page no longer declares %q", decl)
	}
	line := script[i+len(decl):]
	if j := strings.IndexAny(line, ",;"); j >= 0 {
		line = line[:j]
	}
	expr := strings.ReplaceAll(strings.TrimSpace(line), " ", "")
	if !strings.Contains(expr, "1024") && !strings.Contains(expr, "<<") {
		t.Errorf("the page's decoded ceiling %q is not built from the byte unit — a bare number drifts", expr)
	}
	// Evaluate the page's own expression (it is a plain arithmetic product) and
	// require it to equal the server's limit.
	if got := evalByteProduct(expr); got != webMaxChatImageBytes {
		t.Errorf("the page's decoded ceiling evaluates to %d, want the server's webMaxChatImageBytes (%d)", got, webMaxChatImageBytes)
	}
	if !strings.Contains(script, "const MAX_B64=Math.ceil(MAX_IMAGE_BYTES*BASE64_RATIO_NUM/BASE64_RATIO_DEN)+4;") {
		t.Errorf("the page's MAX_B64 must be DERIVED from MAX_IMAGE_BYTES by the base64 expansion, not typed as a second number")
	}
	if strings.Contains(script, "32*1024*1024*4/3") {
		t.Errorf("the page still carries the pre-derivation inline base64 expression — the budget must come from MAX_IMAGE_BYTES")
	}
}

// evalByteProduct evaluates the page's decoded-ceiling expression
// (`32*1024*1024` — a plain product of integer literals). It exists so the test
// reads the page's OWN arithmetic instead of a number copied into the assertion.
func evalByteProduct(expr string) int {
	total := 1
	for _, factor := range strings.Split(expr, "*") {
		n := 0
		for _, r := range factor {
			if r < '0' || r > '9' {
				return -1
			}
			n = n*10 + int(r-'0')
		}
		total *= n
	}
	return total
}

// TestWebM2B_PageKeepsTheXSSDiscipline asserts the page's structural safety
// property: the render/replay path never interpolates content into markup. The
// page's ONLY innerHTML uses are the two deliberate CLEARS (an empty string —
// clearing is not interpolation); anything else fails.
func TestWebM2B_PageKeepsTheXSSDiscipline(t *testing.T) {
	script := pageScript(t)
	for i, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "innerHTML") {
			continue
		}
		// A COMMENT naming innerHTML is prose (this page documents the rule it
		// follows); only CODE can write markup.
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			continue
		}
		if !strings.Contains(trimmed, "innerHTML='';") && !strings.Contains(trimmed, `innerHTML="";`) {
			t.Errorf("cli_web.go page script line %d writes markup through innerHTML: %q\ncontent must be rendered with textContent (B8's XSS contract)", i+1, trimmed)
		}
	}
	// The marker/render helpers the page uses for dynamic text must exist, so
	// the discipline has an implementation rather than only a rule.
	for _, needle := range []string{"function clearNode(", "function renderMarkdown(", "document.createTextNode("} {
		if !strings.Contains(script, needle) {
			t.Errorf("the page no longer carries %q — the textContent-only render path is incomplete", needle)
		}
	}
}

// TestWebM2B_ResetAckAndQueuedNoticeAreSingleSourced pins the two wire strings
// the page matches on: the reset acknowledgement (B1) and the busy-wait notice
// (B2). Both are written by the server and compared by the page, so both must
// have exactly one spelling.
func TestWebM2B_ResetAckAndQueuedNoticeAreSingleSourced(t *testing.T) {
	if webResetCleared != webResetBodyMessage {
		t.Errorf("webResetCleared (%q) must BE webResetBodyMessage (%q) — one reset message, not two", webResetCleared, webResetBodyMessage)
	}
	script := pageScript(t)
	if !strings.Contains(script, "const RESET_NOTICE='"+webResetCleared+"';") {
		t.Errorf("the page must recognize the server's reset acknowledgement %q verbatim", webResetCleared)
	}
	if webBusyWaitNotice == "" {
		t.Errorf("webBusyWaitNotice must not be empty — a queued frame with no notice leaves a blank bubble")
	}
	// The page renders the server's notice when its frame carries one, and has a
	// fallback for a frame that does not (never a blank bubble).
	if !strings.Contains(script, "q.notice||") {
		t.Errorf("the page must fall back to its own text when a queued frame carries no notice")
	}
}

// TestWebM2B_PageBoldMarkerIsTheTerminalTokenizersOwn is the cross-carrier pin
// for the intra-line emphasis marker: the page cannot import Go constants, so
// `gopi web`'s spelling of `**` and `gopi ui`'s (lineedit.BoldMarker, which the
// terminal tokenizer reads) are held together here — a rename on either side
// fails this test instead of silently giving the two carriers different
// conclusions about what `**` means (Rule 14: one value, checked across the
// Go↔JS boundary).
func TestWebM2B_PageBoldMarkerIsTheTerminalTokenizersOwn(t *testing.T) {
	if webMarkdownBold != lineedit.BoldMarker {
		t.Fatalf("webMarkdownBold = %q must BE lineedit.BoldMarker (%q): one marker, two carriers",
			webMarkdownBold, lineedit.BoldMarker)
	}
	// The marker must be one character repeated, or the page's code-point
	// construction (its fence idiom) cannot express it.
	if len(webMarkdownBold) == 0 {
		t.Fatal("webMarkdownBold must not be empty")
	}
	want := int(webMarkdownBold[0])
	for i := 1; i < len(webMarkdownBold); i++ {
		if int(webMarkdownBold[i]) != want {
			t.Fatalf("webMarkdownBold = %q is not one character repeated", webMarkdownBold)
		}
	}
	script := pageScript(t)

	// The page's code point is the marker's own character…
	decl := "const MD_ASTERISK_CHARCODE="
	i := strings.Index(script, decl)
	if i < 0 {
		t.Fatalf("the page no longer declares %q — the bold marker would be unspelled", decl)
	}
	line := script[i:]
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	if !strings.Contains(line, itoa(want)) {
		t.Errorf("the page's %s must be %d (webMarkdownBold %q); got %q", decl, want, webMarkdownBold, line)
	}
	// …its length is the server's, and it is actually BUILT by repeating that
	// character (a code-point constant nothing uses would be Rule 21's dead-knob
	// defect in page form).
	if !strings.Contains(script, "MD_BOLD_LEN="+itoa(len(webMarkdownBold))) {
		t.Errorf("the page must declare MD_BOLD_LEN=%d (len(webMarkdownBold)) — the marker's length is one value",
			len(webMarkdownBold))
	}
	build := "const MD_BOLD=MD_ASTERISK"
	j := strings.Index(script, build)
	if j < 0 {
		t.Fatalf("the page must build MD_BOLD from MD_ASTERISK (the code-point spelling of webMarkdownBold)")
	}
	buildLine := script[j:]
	if nl := strings.IndexByte(buildLine, '\n'); nl >= 0 {
		buildLine = buildLine[:nl]
	}
	if got := strings.Count(buildLine, "MD_ASTERISK"); got != len(webMarkdownBold) {
		t.Errorf("the page's MD_BOLD repeats the asterisk %d time(s), want %d (len(webMarkdownBold) = %q): %q",
			got, len(webMarkdownBold), webMarkdownBold, buildLine)
	}
}
