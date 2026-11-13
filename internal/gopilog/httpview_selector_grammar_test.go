package gopilog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHTTPSelectorKeywordConstantsAreTheAcceptedSpellings pins the `all|last|N`
// grammar's keyword spellings to their single definitions (PROJECT.md Rule 1b/13):
// the parser must accept exactly RoundKeywordAll / RoundKeywordLast, so a
// consumer comparing against those constants can never disagree with the
// parser. Origin: the orphaned `--http` view review (commit e23d506 left the
// grammar with no production caller and the keywords as bare literals).
func TestHTTPSelectorKeywordConstantsAreTheAcceptedSpellings(t *testing.T) {
	cases := []struct {
		arg        string
		wantAll    bool
		wantDetail bool
		wantRound  int
	}{
		{"", true, false, 0}, // bare flag == the all listing (zero value)
		{"  " + RoundKeywordAll + " ", true, false, 0},
		{RoundKeywordAll, true, false, 0},
		{RoundKeywordLast, false, true, LastRound}, // resolved to the last round by HTTPDetailIndex
	}
	for _, c := range cases {
		sel, err := ParseHTTPSelector(c.arg)
		if err != nil {
			t.Errorf("ParseHTTPSelector(%q) = %v, want no error", c.arg, err)
			continue
		}
		if sel.All != c.wantAll || sel.Detail != c.wantDetail || sel.Round != c.wantRound {
			t.Errorf("ParseHTTPSelector(%q) = %+v, want {All:%v Detail:%v Round:%d}",
				c.arg, sel, c.wantAll, c.wantDetail, c.wantRound)
		}
	}

	// A near-miss spelling stays an error: the keywords are exact, never
	// case-folded, so a renamed constant cannot be silently accepted by half
	// the surfaces.
	for _, arg := range []string{"All", "ALL", "Last", "LAST", "al l"} {
		if _, err := ParseHTTPSelector(arg); err == nil {
			t.Errorf("ParseHTTPSelector(%q) accepted a non-keyword spelling", arg)
		}
	}
}

// TestParseHTTPSelectorMessageIsSingleSourced pins the WHOLE error message. The
// message is rendered from the keyword constants, so the test states the fact
// the defect broke: the text names each keyword exactly ONCE (it once read
// `all|last|last|N`) and it tells the truth about the grammar (0 and negatives
// are legal round selectors, not listed as illegal).
func TestParseHTTPSelectorMessageIsSingleSourced(t *testing.T) {
	_, err := ParseHTTPSelector("third")
	if err == nil {
		t.Fatal("ParseHTTPSelector(\"third\") = no error, want the grammar diagnostic")
	}
	want := fmt.Sprintf("--http takes %s|%s|N (a round number), got %q",
		RoundKeywordAll, RoundKeywordLast, "third")
	if got := err.Error(); got != want {
		t.Errorf("message = %q\nwant      %q", got, want)
	}
	// The duplicated-keyword shape itself, stated independently of the
	// constant rendering: the keyword appears once per occurrence in text.
	if n := strings.Count(err.Error(), RoundKeywordLast); n != 1 {
		t.Errorf("message names %q %d time(s), want exactly 1: %q", RoundKeywordLast, n, err.Error())
	}
	if !strings.Contains(err.Error(), RoundKeywordAll) {
		t.Errorf("message does not name %q: %q", RoundKeywordAll, err.Error())
	}
}

// TestHTTPDetailIndexCountsFromTheEnd is the resolver's matrix: a positive
// selector is 1-based from the start, a zero/negative one counts from the END
// (0 and -1 = last, -2 = 2nd-from-last), and a selector past either end clamps
// to that end — the documented forgiving fallback.
func TestHTTPDetailIndexCountsFromTheEnd(t *testing.T) {
	rounds := make([]HTTPRound, 3) // last index = 2
	cases := []struct {
		round int
		want  int
	}{
		{1, 0},
		{3, 2},
		{4, 2},  // past the end clamps to the last round
		{0, 2},  // zero = the last round
		{-1, 2}, // last
		{-2, 1}, // 2nd-from-last
		{-3, 0}, // 3rd-from-last = the first
		{-4, 0}, // past the first round clamps to it
	}
	for _, c := range cases {
		if got := HTTPDetailIndex(rounds, c.round); got != c.want {
			t.Errorf("HTTPDetailIndex(%d rounds, %d) = %d, want %d", len(rounds), c.round, got, c.want)
		}
	}
}

// TestHTTPSelectorGrammarLiteralGate is the Rule 13 anti-regression gate: the
// duplicated-`last` spelling and the hand-typed grammar string may live NOWHERE
// in this package's production files — the text is RENDERED from
// RoundKeywordAll/RoundKeywordLast (httpSelectorGrammar), so no copy can drift
// from the parser it documents.
func TestHTTPSelectorGrammarLiteralGate(t *testing.T) {
	// Built by concatenation so THIS file does not itself carry the literal a
	// future copy-paste could lift.
	banned := []string{
		"all" + "|last|last|N",       // the defect's exact text
		`"--http takes " + "%s|%s|N`, // a hand-typed replacement, bypassing httpSelectorGrammar
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue // production files only — a test may legitimately quote the text
		}
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		for _, bad := range banned {
			if strings.Contains(string(data), bad) {
				t.Errorf("%s carries %q — render the grammar from RoundKeywordAll/RoundKeywordLast (httpSelectorGrammar) instead", name, bad)
			}
		}
	}
	// Read-floor: a broken ReadDir/glob must fail the gate, never pass it
	// vacuously (the file IS in this directory).
	if scanned < 2 {
		t.Fatalf("scanned %d production .go files, want the package's files", scanned)
	}
}
