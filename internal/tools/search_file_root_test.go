package tools

// search_file_root_test.go — the wrapper's two SILENT-defect regression guards,
// both proven red against the pre-fix wrapper (commit ad8dca8's parent):
//
//   - TestKeywordSearchSingleFileRootKeepsLineNumbers — a single FILE as the
//     search root lost ripgrep's filename prefix, so splitRgColumnLine shifted
//     by one field and returned the match COLUMN as the line number. The
//     wrapper is file-rooted by every session search, so the defect was live on
//     the ordinary path.
//   - TestKeywordSearchSurvivesALongLine — the output parser capped ONE line at
//     1 MiB, while a session JSONL row is a whole message (measured on this
//     host: 3,987,146 bytes). A perfectly ordinary query then failed the WHOLE
//     call with `bufio.Scanner: token too long` instead of returning hits.
//
// Both drive the PUBLIC entry point (KeywordSearch), not the parser helpers: a
// unit test on splitRgColumnLine cannot see what rg actually PRINTS, and the
// missing prefix was a property of the rg ARGUMENT list, not of the parser.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// searchFileRootFixtureName is the single-file search root both guards use.
	// The wrapper's defect was shape-dependent (a FILE root, not a directory),
	// so the fixture must BE a file.
	searchFileRootFixtureName = "fixture-single-file-root.cfg"

	// searchFileRootNeedle sits on line searchFileRootNeedleLine and, crucially,
	// at COLUMN searchFileRootNeedleColumn — the pre-fix wrapper reported the
	// column as the line, so the two numbers must differ or the guard proves
	// nothing.
	searchFileRootNeedle       = "needle-marker-single-file-root"
	searchFileRootNeedleLine   = 3
	searchFileRootNeedleColumn = 11

	// searchFileRootFixtureBody pads the needles so an off-by-one parse yields a
	// line number that is unmistakably NOT the true one (11 vs 3), rather than a
	// neighbour a lenient assertion would accept. The matched line carries a
	// COLON in its content on purpose: rg prints `LINE:COL:TEXT`, and real rows
	// (session JSONL) are JSON, so the pre-fix parser split it into four fields
	// and read the COLUMN as the line — the documented shape, not the accidental
	// one. Without a colon the same output yields only three fields and the hit
	// is dropped instead, which is a different (also silent) failure.
	searchFileRootFixtureBody = "line one is filler\n" +
		"line two is filler\n" +
		"          " + searchFileRootNeedle + `: {"kind":"cfg","colons":2}` + "\n" +
		"line four is filler\n"

	// searchLongLineCeiling is the fixture row size: 4 MiB, i.e. 4x the 1 MiB
	// parser cap the defect carried and comfortably under the 128 MiB ceiling the
	// fix names, so the guard separates "fixed" from "still 1 MiB" without
	// depending on the exact value of either.
	searchLongLineCeiling = 4 << 20

	// searchLongLineNeedle is the only match in the 4 MiB row: it sits at the very
	// END, so a parser that silently drops or truncates the row reports zero hits
	// rather than a lucky early match.
	searchLongLineNeedle = "needle-marker-long-row"

	// searchLongLineFiller is the byte a session row is padded with — arbitrary,
	// it only has to not contain the needle.
	searchLongLineFiller = "x"

	// searchLongLineFile mirrors the real shape: a JSONL file whose SECOND row
	// is one enormous message. rg prints that whole row as ONE output line, which
	// is what the parser had to hold.
	searchLongLineFile = "session-long-row.jsonl"
)

// TestKeywordSearchSingleFileRootKeepsLineNumbers pins that a FILE as the search
// root yields the real line number. Pre-fix (no -H) rg printed `line:col:text`,
// splitRgColumnLine read `line` as the path and `col` as the line, and this
// fixture answered 11 instead of 3.
func TestKeywordSearchSingleFileRootKeepsLineNumbers(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, searchFileRootFixtureName)
	if err := os.WriteFile(path, []byte(searchFileRootFixtureBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// The root IS the file: this is the shape the session scope and any
	// caller-selected file use, and the shape the defect lived in.
	hits, _, _, err := KeywordSearch(SearchScope{RootPath: path}, searchFileRootNeedle)
	if err != nil {
		t.Fatalf("KeywordSearch over a single-file root failed: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit for %q in the single-file root, got %d: %+v",
			searchFileRootNeedle, len(hits), hits)
	}
	if hits[0].LineNo != searchFileRootNeedleLine {
		t.Fatalf("line number = %d, want %d (the match COLUMN is %d — a hit reporting the column is the missing-filename-prefix defect)",
			hits[0].LineNo, searchFileRootNeedleLine, searchFileRootNeedleColumn)
	}
	if !strings.Contains(hits[0].Snippet, searchFileRootNeedle) {
		t.Errorf("snippet lost the match: %q", hits[0].Snippet)
	}
}

// TestKeywordSearchSurvivesALongLine pins that ONE long row does not fail the
// call. Pre-fix the parser's scanner was capped at 1 MiB, so this 4 MiB row made
// the whole query return `keyword_search: scan rg output: bufio.Scanner: token
// too long` and zero hits.
func TestKeywordSearchSurvivesALongLine(t *testing.T) {
	if !rgAvailable() {
		t.Skip("rg not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, searchLongLineFile)

	// Row 1 is a normal line, row 2 is the long one — so the assertion can also
	// prove the parser kept counting after the big row instead of resuming at 1.
	longRow := strings.Repeat(searchLongLineFiller, searchLongLineCeiling-len(searchLongLineNeedle)) +
		searchLongLineNeedle
	if len(longRow) != searchLongLineCeiling {
		t.Fatalf("fixture row is %d bytes, want %d", len(longRow), searchLongLineCeiling)
	}
	body := `{"type":"user","text":"filler"}` + "\n" + longRow + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, _, _, err := KeywordSearch(SearchScope{RootPath: path}, searchLongLineNeedle)
	if err != nil {
		t.Fatalf("KeywordSearch over a %d-byte row failed: %v", searchLongLineCeiling, err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit in the long row, got %d: %+v", len(hits), hits)
	}
	if hits[0].LineNo != 2 {
		t.Errorf("long-row hit line = %d, want 2", hits[0].LineNo)
	}
	// The snippet the caller sees stays small even though the parser had to hold
	// the whole row — the ceiling is a parser bound, not a delivery size.
	if runes := len([]rune(hits[0].Snippet)); runes > 120 {
		t.Errorf("snippet is %d runes, want a bounded snippet (<= 120)", runes)
	}
}
