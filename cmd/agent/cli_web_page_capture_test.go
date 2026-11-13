// cli_web_page_capture_test.go — the R6 page-capture gate: puts the 📷
// capture scenarios under `go test`.
//
// Why this file exists (the defect it pins): commit e1c344e2a fixed
// `capturePage()`'s failure branch, which used to clear the ENTIRE strip
// (`strip.innerHTML=''`) to drop a previous `.shoterr` line — and therefore also
// erased the thumbnails of pictures the operator had ALREADY staged, while
// images[] kept them: the pictures vanished from the screen and the next Enter
// still shipped them. The fix removes the failure line only
// (strip.querySelectorAll('.shoterr').forEach(n=>n.remove())).
//
// The fix landed with its scenario in cli_web_page_scenarios.js and that
// scenario passed 31/31 — but NOTHING in Go drove it. `go test ./cmd/agent/`
// never ran it, so putting `strip.innerHTML=''` back would have stayed green:
// a scenario nobody executes is a check that cannot fail. This test is the
// driver, so the R6 group is part of the suite.

package main

import (
	"strings"
	"testing"
)

// pageCaptureScenarioName is the scenario-name substring the harness is asked
// to run alone. The harness matches it against `name.indexOf(...)`, so this one
// constant selects the whole R6 page-capture group — including the scenario the
// e1c344e2a fix added ('R6 page capture failure keeps the staged thumbnails').
// It is a CONSTANT (Rule 1/13/14): an inline string here and in the scenario
// file could drift apart silently, and the drift would read as "the gate
// passes" while running nothing — the harness only refuses an EMPTY match, and
// a stale-but-matching prefix would run the wrong scenarios.
const pageCaptureScenarioName = "R6 page capture"

// TestPageCaptureFailure drives the shipped page through the harness's R6
// page-capture group and requires it to pass.
//
// The scenarios assert, against the page's REAL script: 📷 POSTs /api/shot with
// no body; the returned picture is staged in images[] and drawn as a data: URI
// thumbnail; a REFUSED capture stages nothing but IS rendered with the server's
// own status and message (never a button that silently does nothing); and — the
// e1c344e2a fix — a failed capture keeps the ALREADY-STAGED thumbnails, removes
// only the failure line, and replaces a previous failure line instead of piling
// them up.
func TestPageCaptureFailure(t *testing.T) {
	node, err := lookPathNode()
	if err != nil {
		t.Skip("skipping the R6 page-capture gate: no `node` on PATH (the capture scenario runs the page's own JS under node, so there is no way to drive it without one)")
	}
	out, runErr := runNode(t, node, pageHarnessPath, "--only", pageCaptureScenarioName, "--page", menuNavGoFile)
	text := string(out)
	// "FAIL" without the colon is the harness's own per-check marker and its
	// empty-match refusal, so the bare prefix catches both a red check and a
	// selector that matched no scenario (which would otherwise look green).
	if runErr != nil || !strings.Contains(text, menuHarnessPass) || strings.Contains(text, menuHarnessFail) {
		t.Fatalf("the R6 page-capture scenario did not pass (err=%v):\n%s", runErr, text)
	}
	t.Logf("R6 page-capture scenario:\n%s", text)
}
