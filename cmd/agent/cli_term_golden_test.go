package main

// cli_term_golden_test.go — the GOLDEN page.
//
// The other /cli tests assert that the page CONTAINS the things a text browser
// needs (a named field, a real form action, the bottom anchor). This one pins
// the page BYTE FOR BYTE, because the carrier's failure mode is invisible in
// that shape of assertion: a tag that opens and never closes, an entity escaped
// twice, a stray script, a `<pre>` added inside another `<pre>` — every one of
// them still contains the form, and every one of them renders as garbage in a
// terminal.
//
// The golden file lives in testdata/ and is compared exactly. When the page
// changes ON PURPOSE, delete the file and re-run: the test regenerates it and
// says so — a regenerated golden is a reviewed diff, never a silently accepted
// one (the test never rewrites an existing golden, so an accidental change
// fails here instead of quietly becoming the new truth).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// termGoldenPath is the golden document, relative to this package.
const termGoldenPath = "testdata/term_page.golden"

// TestTermPageGolden pins the whole served document.
func TestTermPageGolden(t *testing.T) {
	ws, _ := termServerForTest(t, false, func(w http.ResponseWriter, r *http.Request) {})
	ws.mu.Lock()
	ws.history = []deepseek.Message{
		{Role: termRoleUser, Content: "hello <gopi> & friends"},
		{Role: termRoleAssistant, Content: "hi\nthere"},
		{Role: termRoleTool, Content: `{"stdout":"never rendered"}`},
	}
	ws.mu.Unlock()

	rec := newStreamRecorder()
	ws.handleTermPage(rec, httptest.NewRequest(http.MethodGet, termChatPath, nil))
	got := rec.body()

	golden, err := os.ReadFile(termGoldenPath)
	if os.IsNotExist(err) {
		if dir := filepath.Dir(termGoldenPath); dir != "" {
			if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
				t.Fatalf("create golden dir: %v", mkErr)
			}
		}
		if wErr := os.WriteFile(termGoldenPath, []byte(got), 0o644); wErr != nil {
			t.Fatalf("write golden: %v", wErr)
		}
		t.Logf("wrote the golden page to %s (%d bytes) — review it and commit it", termGoldenPath, len(got))
		return
	}
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(golden) {
		t.Errorf("the served page differs from %s.\n--- got (%d bytes) ---\n%s\n--- want (%d bytes) ---\n%s\n"+
			"If the change is intentional, delete %s and re-run this test to regenerate it.",
			termGoldenPath, len(got), got, len(golden), golden, termGoldenPath)
	}
}

// TestTermPageHasNoActiveContent pins the character of the page: it is TEXT.
// A text browser has no scripting and no CSS, and a carrier that quietly grows
// a <script> (a copy-paste from the browser page) would still pass every
// "contains" assertion while rendering as noise in the terminal.
func TestTermPageHasNoActiveContent(t *testing.T) {
	ws, _ := termServerForTest(t, false, func(w http.ResponseWriter, r *http.Request) {})
	rec := newStreamRecorder()
	ws.handleTermPage(rec, httptest.NewRequest(http.MethodGet, termChatPath, nil))
	page := rec.body()
	for _, forbidden := range []string{"<script", "<style", "EventSource", "fetch(", "onclick", "<textarea"} {
		if strings.Contains(page, forbidden) {
			t.Errorf("the terminal page carries %q — a text browser cannot use it:\n%s", forbidden, page)
		}
	}
	// Balanced <pre> tags: the whole carrier depends on the browser appending
	// inside a preformatted block, so an unclosed one is a rendering defect.
	if strings.Count(page, "<pre") != strings.Count(page, "</pre>") {
		t.Errorf("unbalanced <pre> tags (%d open, %d close):\n%s",
			strings.Count(page, "<pre"), strings.Count(page, "</pre>"), page)
	}
	// Exactly ONE form and ONE field: a second input would make Tab navigation
	// ambiguous in a browser that has no other way to reach a control.
	if strings.Count(page, "<form") != 1 {
		t.Errorf("the page has %d forms, want exactly 1:\n%s", strings.Count(page, "<form"), page)
	}
	if strings.Count(page, "<input") != 1 {
		t.Errorf("the page has %d inputs, want exactly 1 (Tab must land on it):\n%s", strings.Count(page, "<input"), page)
	}
}
