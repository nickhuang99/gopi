package devdoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The parser and the banner reader are the whole package, and both are read by
// RUNNING code (the merge lane's union, the write path's hook), so each gets a
// direct test here rather than only through its caller: a defect in either one
// shows up as a merge lane reading a fragment of a manifest or a document
// landing with a banner its own reader cannot parse.

func TestParseManifestLinesAcceptsTheShippedShape(t *testing.T) {
	body := []byte(strings.Join([]string{
		"# a comment above the column line is tolerated",
		"",
		ManifestHeader,
		"devdoc/a.md\tnormative\ta standing claim\tpath devdoc/a.md",
		"devdoc/b.md\thistorical\ta past record\t",
	}, "\n") + "\n")
	rows, err := ParseManifestLines(body)
	if err != nil {
		t.Fatalf("the shipped shape does not parse: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("parsed %d row(s), want 2: %+v", len(rows), rows)
	}
	if rows[0] != (Row{Path: "devdoc/a.md", Class: ClassNormative, Reason: "a standing claim",
		Evidence: "path devdoc/a.md", Line: 4}) {
		t.Fatalf("row 0 = %+v", rows[0])
	}
	if rows[1].Class != ClassHistorical || rows[1].Evidence != "" {
		t.Fatalf("row 1 = %+v — a historical row records no spot-check", rows[1])
	}
}

// TestParseManifestLinesRefusesRatherThanSkipping is the property the deleted
// reader was rebuilt around: a malformed line is an ERROR with its line number,
// never a skipped row — a half-read manifest silently shrinks its caller's scope
// to whatever happened to parse (Rule 15).
func TestParseManifestLinesRefusesRatherThanSkipping(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"wrong header", "path\tclass\treason\n", "unexpected header"},
		{"too few cells", ManifestHeader + "\ndevdoc/a.md\tnormative\n", "2 field(s), want 4"},
		{"too many cells", ManifestHeader + "\ndevdoc/a.md\tnormative\tr\tev\tx\n", "5 field(s), want 4"},
		{"blank row line", ManifestHeader + "\ndevdoc/a.md\tnormative\treason\tev\n\n", "blank line"},
		{"unknown class", ManifestHeader + "\ndevdoc/a.md\topinion\tr\tev\n", "unknown class"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseManifestLines([]byte(c.body))
			if err == nil {
				t.Fatalf("a %s manifest parsed cleanly — it must be refused", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("refusal %q does not carry %q", err, c.want)
			}
		})
	}
}

func TestParseBannerAndHeaderClassAgree(t *testing.T) {
	if got := ParseBanner(Banner(ClassRoadmap)); got != ClassRoadmap {
		t.Fatalf("ParseBanner(Banner(roadmap)) = %q", got)
	}
	if got := ParseBanner("<!-- doc-class: historical — classified in x.md -->"); got != "" {
		t.Fatalf("a banner carrying extra prose is %q — the grammar is exact, so commentary must be NO banner rather than a near match", got)
	}
	if got := ParseBanner("<!-- doc-class: opinion -->"); got != "" {
		t.Fatalf("an unknown class parsed as %q", got)
	}

	class, err := HeaderClass(Banner(ClassHistorical) + "\r")
	if err != nil {
		t.Fatalf("a banner with a CRLF ending was refused: %v", err)
	}
	if class != ClassHistorical {
		t.Fatalf("class = %q", class)
	}
	// The three refusals the write hook depends on, each named in its own words:
	// missing, malformed, and an illegal value.
	for _, c := range []struct{ line, want string }{
		{"", "first line is empty"},
		{"# Title\n", "must be"},
		{"<!-- doc-class: historical", "does not close"},
		{"<!-- doc-class: opinion -->", "illegal"},
	} {
		_, herr := HeaderClass(c.line)
		if herr == nil || !strings.Contains(herr.Error(), c.want) {
			t.Fatalf("HeaderClass(%q) error = %v; want a refusal naming %q", c.line, herr, c.want)
		}
	}
}

func TestReadHeaderClassSeparatesUnreadableFromUnclassified(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.md")
	if err := os.WriteFile(good, []byte(Banner(ClassNormative)+"\n\n# Title\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	class, err := ReadHeaderClass(good)
	if err != nil || class != ClassNormative {
		t.Fatalf("ReadHeaderClass(good) = %q, %v", class, err)
	}
	// An UNREADABLE file is its own error — never reported as "unclassified",
	// which would send a reader looking for a header on a file nobody can open.
	if _, err := ReadHeaderClass(filepath.Join(dir, "absent.md")); err == nil ||
		!strings.Contains(err.Error(), "read ") {
		t.Fatalf("an absent file's error = %v; want a read failure", err)
	}
}

func TestIsProseMatchesOnlyMarkdown(t *testing.T) {
	if !IsProse(Root + "/a.md") {
		t.Fatal("a markdown doc under the corpus must be prose")
	}
	for _, rel := range []string{Root + "/a.tsv", Root + "/a.png", "docs/a.md"} {
		if IsProse(rel) {
			t.Fatalf("%q counted as corpus prose", rel)
		}
	}
}

// TestPathComposesFromItsHalves keeps the composed constant honest: a rename of
// either half moves Path with it, which is the whole reason it is composed.
func TestPathComposesFromItsHalves(t *testing.T) {
	if Path != Root+"/"+ManifestName {
		t.Fatalf("Path = %q, want %q composed from its halves", Path, Root+"/"+ManifestName)
	}
	if ManifestHeader != strings.Join([]string{"path", "class", "reason", "evidence"}, "\t") {
		t.Fatalf("ManifestHeader = %q does not carry the four documented columns tab-separated", ManifestHeader)
	}
}
