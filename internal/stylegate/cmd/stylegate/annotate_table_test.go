package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/stylegate"
)

// TestAnnotateRefusesGroupReasonKeys is the annotator's half of the per-SITE
// rule: the reason table is keyed `file:line` and NOTHING else, so the two
// GROUP shapes that used to clear a site — a bare/qualified callee and a whole
// file — are REFUSED BY NAME before a single byte is written.
//
// Why this is a refusal and not a filter: dropping a group key silently leaves
// its author believing the justification applies, which is the same failure the
// old lookup produced — a site cleared by a statement about a group.
//
// The table is driven through the real annotate() entry point, on a fixture
// module the test owns (Rule 6). The propositions:
//
//	GROUP-CALLEE  `strconv.Atoi` (and its `file::callee` spelling) is refused,
//	              and the message names the key and the defect.
//	GROUP-FILE    `fixture.go` (a whole-file exemption) is refused the same way.
//	SITE          the same fixture with `fixture.go:<line>` is ACCEPTED — the
//	              proof must not pass merely because everything is refused.
func TestAnnotateRefusesGroupReasonKeys(t *testing.T) {
	const siteLine = 6 // the `_ = boom()` line in the fixture below

	// The scanner resolves a root through RelativeTo, but the ANNOTATOR reads and
	// writes paths relative to the PROCESS cwd (report entries and file names are
	// both repo-relative). So the arm runs inside the fixture: t.Chdir registers
	// its own restore, which is what the stylegate's own RuleTestChdir arm asks
	// for.
	dir := t.TempDir()
	t.Chdir(dir)
	src := "package fixture\n" + // 1
		"\n" + // 2
		"import \"fmt\"\n" + // 3
		"\n" + // 4
		"func site() {\n" + // 5
		"\t_ = boom()\n" + // 6
		"}\n" + // 7
		"\n" + // 8
		"func boom() error { return fmt.Errorf(\"boom\") }\n"
	if err := os.WriteFile("fixture.go", []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile("go.mod", []byte("module fixture\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// The report the annotator consumes, produced by the REAL scanner over the
	// fixture — a hand-written report would let this arm pass on a shape the
	// tool can never actually meet.
	rep, err := stylegate.Scan(stylegate.Options{Root: dir, IncludeTests: true})
	if err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	reportPath := "report.json"
	body, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("encode report: %v", err)
	}
	if err := os.WriteFile(reportPath, body, 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}

	writeTable := func(name string, entries map[string]string) string {
		t.Helper()
		p := name
		b, err := json.Marshal(entries)
		if err != nil {
			t.Fatalf("encode table %s: %v", name, err)
		}
		if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
			t.Fatalf("write table %s: %v", name, err)
		}
		return p
	}

	refusals := []struct {
		name    string
		entries map[string]string
		wantKey string
		wantWhy string
	}{
		{
			name:    "bare callee",
			entries: map[string]string{"boom": "a group reason for every call to boom"},
			wantKey: `"boom"`,
			wantWhy: "names a GROUP",
		},
		{
			name:    "file::callee",
			entries: map[string]string{"fixture.go::boom": "the spelling the old middle fallback used"},
			wantKey: `"fixture.go::boom"`,
			wantWhy: "names a GROUP",
		},
		{
			name:    "whole file",
			entries: map[string]string{"fixture.go": "a whole-file exemption covering every discard here"},
			wantKey: `"fixture.go"`,
			wantWhy: "names a whole FILE",
		},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			table := writeTable(strings.ReplaceAll(tc.name, " ", "_")+".json", tc.entries)
			n, err := annotate(dir, reportPath, table, stylegate.RuleIgnoredError)
			if err == nil {
				t.Fatalf("a table keyed %q was ACCEPTED and annotated %d site(s) — a group reason cleared a "+
					"site, which is the regression this rule exists to prevent", tc.name, n)
			}
			if n != 0 {
				t.Errorf("a refused table still reported %d annotation(s) — nothing may be written on a refusal", n)
			}
			if !strings.Contains(err.Error(), tc.wantKey) {
				t.Errorf("the refusal does not name the offending key %s: %v", tc.wantKey, err)
			}
			if !strings.Contains(err.Error(), tc.wantWhy) {
				t.Errorf("the refusal does not say WHY %s is refused (want %q): %v", tc.wantKey, tc.wantWhy, err)
			}
			// The refusal must be free of side effects: the source cannot carry a
			// marker for a reason that was never accepted.
			got, rerr := os.ReadFile("fixture.go")
			if rerr != nil {
				t.Fatalf("read back fixture: %v", rerr)
			}
			if strings.Contains(string(got), stylegate.IgnoreMarker) {
				t.Fatalf("the refused table still wrote a marker into the fixture:\n%s", got)
			}
		})
	}

	// CONTROL: the SAME site, keyed per site, is annotated. Without this half the
	// arm above would pass if the tool simply refused everything.
	good := writeTable("site.json", map[string]string{
		stylegate.SiteKey("fixture.go", siteLine): "the fixture's own setup boom; the test asserts on the marker below",
	})
	n, err := annotate(dir, reportPath, good, stylegate.RuleIgnoredError)
	if err != nil {
		t.Fatalf("a per-site table was refused: %v", err)
	}
	if n != 1 {
		t.Fatalf("a per-site table annotated %d site(s), want 1", n)
	}
	got, err := os.ReadFile("fixture.go")
	if err != nil {
		t.Fatalf("read back fixture: %v", err)
	}
	if !strings.Contains(string(got), stylegate.IgnoreMarker) {
		t.Fatalf("the accepted table wrote no marker:\n%s", got)
	}
	t.Logf("per-site table accepted; fixture now carries:\n%s", got)
}
