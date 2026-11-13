package main

// cli_web_command_images_page_test.go — puts the R7 "command images" scenario
// group under `go test`.
//
// Why this file exists (the same defect shape cli_web_page_capture_test.go
// pins): the scenarios in cli_web_page_scenarios.js are only a CHECK if
// something EXECUTES them. `go test ./cmd/agent/` is that something; a scenario
// nobody drives would let the page stop staging a command's pictures — silently,
// with the strip empty and the model receiving nothing — while every Go test
// stayed green.

import (
	"strings"
	"testing"
)

// pageCommandImagesScenarioName is the substring selecting the whole R7 group
// (both arms). A CONSTANT (Rule 1/13/14): an inline literal here and in the
// scenario file could drift, and the drift would read as a pass while running
// nothing — the harness only refuses an EMPTY match.
const pageCommandImagesScenarioName = "R7 command images"

// TestPageCommandImages drives the shipped page through the R7 group: a
// `command` frame carrying pictures stages them into images[] and draws the
// thumbnails (so the NEXT Enter ships them through the ordinary /api/chat
// body), and a frame carrying more pictures than one message may hold reports
// the drop instead of swallowing it.
func TestPageCommandImages(t *testing.T) {
	node, err := lookPathNode()
	if err != nil {
		t.Skip("skipping the R7 command-images gate: no `node` on PATH (the scenario runs the page's own JS under node, so there is no way to drive it without one)")
	}
	out, runErr := runNode(t, node, pageHarnessPath, "--only", pageCommandImagesScenarioName, "--page", menuNavGoFile)
	text := string(out)
	// "FAIL" without the colon is the harness's per-check marker AND its
	// empty-match refusal, so the bare prefix catches a red check and a
	// selector that matched no scenario (which would otherwise look green).
	if runErr != nil || !strings.Contains(text, menuHarnessPass) || strings.Contains(text, menuHarnessFail) {
		t.Fatalf("the R7 command-images scenario did not pass (err=%v):\n%s", runErr, text)
	}
	t.Logf("R7 command-images scenarios:\n%s", text)
}
