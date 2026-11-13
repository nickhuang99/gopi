package tools

// search_parse_failure_test.go — the SILENT-DROP guard for the rg output parser.
//
// The defect: keywordSearchViaRipgrep parsed each rg output line with
// splitRgColumnLine and, on ok=false, answered `continue`. A line the parser
// could not read was therefore DROPPED with no trace, and a search whose
// matches were all unparseable came back as `hits=0, err=nil` — the exact
// answer "nothing matched" gives. The caller cannot tell the two apart, and
// the second one is a wrapper defect, not an empty tree (Rule 15).
//
// The trigger below is a REAL one, not a synthetic string: rg prints
// `path:line:col:text`, which is ambiguous the moment a PATH contains a colon.
// Measured on this host (ripgrep 13.0.0):
//
//	$ rg -H -n --no-heading --color never --column -- 'needle-x' <dir>
//	<dir>/colon:name.cfg:2:5:bbb needle-x ccc: ddd
//
// splitRgColumnLine splits on the FIRST THREE colons, so parts[1] becomes
// "name.cfg", Atoi fails, and the hit is thrown away — while a sibling file
// with a colon-free name in the same directory parses fine. That asymmetry is
// what makes the pre-fix answer so dangerous: one match arrives, one silently
// vanishes, and the caller sees a clean, plausible, PARTIAL list.
//
// Both arms drive the PUBLIC entry point (KeywordSearch) rather than the
// parser: a unit test on splitRgColumnLine cannot see what rg actually PRINTS
// (the same lesson search_file_root_test.go records for the -H defect), and the
// call that must stop lying is the one the tool layer makes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// searchColonNeedle is the only match in both fixtures. It is long and
	// unlikely to occur by accident anywhere the search root might reach.
	searchColonNeedle = "needle-marker-colon-path"

	// searchColonNamedFile carries a COLON in its path — the shape that makes
	// rg's own output ambiguous. This is the ordinary, not the pathological,
	// case: git checkouts, backups and downloaded files are full of them.
	searchColonNamedFile = "colon:name.cfg"

	// searchPlainFile is the control: a colon-free name in the SAME directory,
	// so an arm can prove the call still works on the trees this defect does
	// not touch (a guard that fired on every search would be its own bug).
	searchPlainFile = "plain.cfg"

	// searchColonFixtureBody puts the needle on line 2, so the pre-fix parser's
	// mis-split is reproducible and the fixture reads like a real config file.
	searchColonFixtureBody = "aaa\n" + searchColonNeedle + " here\n"
)

// writeColonFixture writes both fixtures into a fresh directory and returns it.
func writeColonFixture(t *testing.T, withPlain bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, searchColonNamedFile), []byte(searchColonFixtureBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if withPlain {
		if err := os.WriteFile(filepath.Join(dir, searchPlainFile), []byte(searchColonFixtureBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestKeywordSearchFailsWhenNoMatchLineParses is the sharpest form of the
// defect: every match is unreadable, so the pre-fix call answered the same
// thing an empty tree answers.
//
// Pre-fix: hits=0, dropped=0, err=nil  → indistinguishable from "no matches".
// Fixed:   err names the unreadable line and the count.
func TestKeywordSearchFailsWhenNoMatchLineParses(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	dir := writeColonFixture(t, false)

	hits, dropped, _, err := KeywordSearch(SearchScope{RootPath: dir}, searchColonNeedle)
	if err == nil {
		t.Fatalf("KeywordSearch over a colon-named match returned err=nil with hits=%d dropped=%d: "+
			"the match existed and the parser threw it away, so this is a FAILURE a caller must see, not an empty result",
			len(hits), dropped)
	}
	if len(hits) != 0 || dropped != 0 {
		t.Errorf("a failing call must return no hits and no drop count, got hits=%d dropped=%d", len(hits), dropped)
	}
	msg := err.Error()
	for _, want := range []string{searchColonNamedFile, "1 line(s) unreadable"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error must name %q so a reader can find the offending line; got: %s", want, msg)
		}
	}
}

// TestKeywordSearchFailsWhenAMatchLineIsDroppedBesideGoodOnes is the PARTIAL
// case, and the one that actually reaches users: a directory holding one
// colon-named file and one ordinary file. Pre-fix the ordinary hit came back
// with a clean err — a plausible, incomplete answer nobody could question.
func TestKeywordSearchFailsWhenAMatchLineIsDroppedBesideGoodOnes(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	dir := writeColonFixture(t, true)

	hits, dropped, _, err := KeywordSearch(SearchScope{RootPath: dir}, searchColonNeedle)
	if err == nil {
		t.Fatalf("KeywordSearch silently returned %d hit(s) (dropped=%d) while one match was unparseable: "+
			"a partial answer that hides which matches were lost is the same defect at a smaller dose", len(hits), dropped)
	}
	if !strings.Contains(err.Error(), "1 line(s) unreadable, 1 parsed into hits") {
		t.Errorf("the failure must report BOTH counts (what was thrown away, what survived); got: %v", err)
	}
}

// TestKeywordSearchStillAnswersOnAColonFreeTree is the control arm: the fix
// must not turn every search into a failure. Same needle, same shape, no colon
// in any path → the hit arrives with err=nil.
func TestKeywordSearchStillAnswersOnAColonFreeTree(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, searchPlainFile), []byte(searchColonFixtureBody), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, _, _, err := KeywordSearch(SearchScope{RootPath: dir}, searchColonNeedle)
	if err != nil {
		t.Fatalf("a colon-free tree must still answer: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
	}
	if hits[0].LineNo != 2 {
		t.Errorf("hit line = %d, want 2", hits[0].LineNo)
	}
}

// TestSearchParseFailureBoundsTheEchoedLine pins Rule 1's bound: the offending
// line is echoed into the error, and the line being described is frequently a
// whole session message (megabytes). The echo must be truncated AND marked, so
// a reader never mistakes a cut line for the line that was printed.
func TestSearchParseFailureBoundsTheEchoedLine(t *testing.T) {
	long := strings.Repeat("y", searchParseFailureLineCap*3)
	err := searchParseFailure(long, 2, 5)
	msg := err.Error()
	if len(msg) > searchParseFailureLineCap*2 {
		t.Fatalf("the error echoed %d bytes of a %d-byte line — the diagnostic must stay bounded", len(msg), len(long))
	}
	if !strings.Contains(msg, searchParseFailureEllipsis) {
		t.Error("a truncated echo must be marked as such")
	}
	if !strings.Contains(msg, "2 line(s) unreadable, 5 parsed into hits") {
		t.Errorf("counts missing from the message: %s", msg)
	}
}
