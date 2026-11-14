package replay

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// The shapes this test uses. They are the four alternatives of IdentShapes,
// written as literals HERE because a test that read them from the pass would
// assert nothing about the pass (and Rule 1b's concern is production code: a
// test's fixture is the thing being compared, not a second definition of a
// value the product reads).
const (
	testUUID      = "4ca8cd29-019c-4dda-a000-6dafd782bf91"
	testHex32     = "9f2c7ab41d0e5386ac1122fdeb445566"
	testHomePath  = "/home/user/.gopi/sessions"
	testSandboxID = "sandbox-build-4ca8cd29"
)

// TestMaskIsLengthPreserving pins the property the whole pass rests on: the
// substitute is the SAME RUNE COUNT as what it replaces. A shorter one would
// reflow the line it sits in, moving every glyph after it — a different picture
// rather than the same picture with one fact withheld — and it is also what makes
// masking a stream across fragment boundaries possible at all.
func TestMaskIsLengthPreserving(t *testing.T) {
	r := NewRedactorWriter(io.Discard)
	for _, tc := range []struct{ name, in string }{
		{"uuid", "session " + testUUID + " ended"},
		{"32hex", "digest " + testHex32 + " here"},
		{"home path", "read " + testHomePath + "/x"},
		{"sandbox", "profile " + testSandboxID + " ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, changed := r.Mask(tc.in)
			if !changed {
				t.Fatalf("%q was not masked at all", tc.in)
			}
			if utf8.RuneCountInString(out) != utf8.RuneCountInString(tc.in) {
				t.Fatalf("length changed: %d runes in, %d out (%q)", utf8.RuneCountInString(tc.in), utf8.RuneCountInString(out), out)
			}
			if IdentShapes.MatchString(out) {
				t.Fatalf("the masked text still carries a shape: %q", out)
			}
			if strings.Contains(out, testUUID) || strings.Contains(out, testHex32) {
				t.Fatalf("the run survived: %q", out)
			}
		})
	}
}

// TestMaskLogRowFormat pins the log verify.py reads: a header, then one row per
// substitution with `#n`, `runes` and the run itself, tab-separated. The second
// column is what makes the length-preserving claim checkable from the file alone,
// and the third is what the containment scan greps the delivered artifacts for.
func TestMaskLogRowFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), RedactionLogName)
	r, err := NewRedactor(path)
	if err != nil {
		t.Fatalf("open the log: %v", err)
	}
	if _, changed := r.Mask("a " + testUUID + " b " + testHex32); !changed {
		t.Fatal("nothing was masked")
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want a header and two rows, got %d lines: %q", len(lines), raw)
	}
	if lines[0] != strings.TrimSuffix(RedactionLogHeader, "\n") {
		t.Fatalf("header is not the shared one: %q", lines[0])
	}
	if cols := strings.Split(lines[1], "\t"); len(cols) != 3 || cols[0] != "1" || cols[1] != "36" || cols[2] != testUUID {
		t.Fatalf("row 1 is not `#n runes matched`: %q", lines[1])
	}
	if cols := strings.Split(lines[2], "\t"); len(cols) != 3 || cols[0] != "2" || cols[1] != "32" || cols[2] != testHex32 {
		t.Fatalf("row 2 is not `#n runes matched`: %q", lines[2])
	}
	if r.Masks() != 2 {
		t.Fatalf("Masks() = %d, want 2", r.Masks())
	}
}

// TestMaskFragmentsCatchesARunThatStraddlesTheSeam is the measured leak this
// exists for: a uuid split across two fragments matches NO shape in either half
// (8-4-4 is not 8-4-4-4-12), so a fragment-by-fragment masker passes both and the
// two halves together spell the identifier on screen.
func TestMaskFragmentsCatchesARunThatStraddlesTheSeam(t *testing.T) {
	r := NewRedactorWriter(io.Discard)
	first, second := testUUID[:18], testUUID[18:]
	// The premise: neither half is a shape on its own.
	if IdentShapes.MatchString(first) || IdentShapes.MatchString(second) {
		t.Fatalf("the split is not the one this test is about (%q / %q)", first, second)
	}
	out, changed := r.MaskFragments([]string{"see " + first, second + " for it"})
	if changed != 2 {
		t.Fatalf("changed = %d, want both fragments rewritten", changed)
	}
	joined := out[0] + out[1]
	if IdentShapes.MatchString(joined) || strings.Contains(joined, testUUID) {
		t.Fatalf("the identifier that crossed the seam survived: %q", joined)
	}
	if utf8.RuneCountInString(joined) != utf8.RuneCountInString("see "+first+second+" for it") {
		t.Fatalf("the fragments did not keep their length: %q", joined)
	}
	if utf8.RuneCountInString(out[0]) != utf8.RuneCountInString("see "+first) {
		t.Fatalf("fragment boundaries moved: %q", out[0])
	}
}

// TestNewRedactorRefusesAnUnopenableLog is the Rule 15 half: a pass that cannot
// record what it withheld must not run at all. The alternative — degrade to
// drawing the identifier — is the defect the pass exists to remove.
func TestNewRedactorRefusesAnUnopenableLog(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-directory", RedactionLogName)
	if _, err := NewRedactor(missing); err == nil {
		t.Fatalf("an unopenable log at %s was accepted", missing)
	}
}

// TestPendingShapeTailLenHoldsOnlyWhatCouldStillBecomeAShape pins the streaming
// hold: it must cover the head of a shape (so a delta boundary cannot leak it)
// and must release ordinary prose, punctuation and non-ASCII text immediately —
// a hold that never releases would stall the surface's own drawing.
func TestPendingShapeTailLenHoldsOnlyWhatCouldStillBecomeAShape(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", 0},
		{"the answer is ", 0},
		{"done.\n", 0},
		{"中文答案结束", 0},
		{"see 4ca8cd29", 8},                  // the head of a uuid
		{"see /home/ni", 8},                  // the head of a path
		{"profile sandbox-bu", 10},           // the head of a sandbox id
		{"x " + strings.Repeat("a", 50), 36}, // capped at one shape's length
	} {
		if got := PendingShapeTailLen(tc.in); got != tc.want {
			t.Fatalf("PendingShapeTailLen(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestPendingShapeTailLenIsSufficientForTheShapes is the PROOF of the hold's
// sufficiency, and it is the reason the number is maxIdentShapeRunes rather than
// a guess: for every shape, a split anywhere inside it holds the entire head (the
// released prefix matches no shape) and the whole run is then masked by the
// joined mask.
func TestPendingShapeTailLenIsSufficientForTheShapes(t *testing.T) {
	for _, shape := range []string{testUUID, testHex32, "/home/user", testSandboxID} {
		rs := []rune(shape)
		for cut := 1; cut < len(rs); cut++ {
			head, tail := string(rs[:cut]), string(rs[cut:])
			hold := PendingShapeTailLen(head)
			if hold < utf8.RuneCountInString(head) {
				t.Fatalf("the head %q of %q would be released (hold %d of %d runes) — the tail would complete the shape on screen",
					head, shape, hold, utf8.RuneCountInString(head))
			}
			// And the joined mask, which is what a streaming caller runs, removes it.
			joined, changed := NewRedactorWriter(io.Discard).MaskFragments([]string{head, tail})
			if changed == 0 || IdentShapes.MatchString(strings.Join(joined, "")) {
				t.Fatalf("the joined mask did not remove %q split at %d: %q", shape, cut, joined)
			}
		}
	}
}
