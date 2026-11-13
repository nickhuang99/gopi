package taskreport

import "testing"

// TestCountClaimsInMeasuredShapes pins the FIRING arm on the shapes this
// predicate exists for — the incident's own sentence, quoted verbatim from the
// briefing row 01a136ef received (epic 01a13781), plus the error line its
// reproduction block carried.
func TestCountClaimsInMeasuredShapes(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		count int
		unit  string
		file  string
	}{
		{
			name:  "the incident's own sentence (bold figure, file in the same clause)",
			body:  "So: the table `internal/docclass/testdata/anchor-number-baseline.tsv` carries **168 rows** and the tree now produces **167** incoherent anchor rows\n",
			count: 168, unit: "rows", file: "internal/docclass/testdata/anchor-number-baseline.tsv",
		},
		{
			name:  "the incident's gate-test transcript line",
			body:  "    anchor_number_gate_test.go:129: 1 permit(s) in internal/docclass/testdata/anchor-number-baseline.tsv\n",
			count: 1, unit: "permit", file: "internal/docclass/testdata/anchor-number-baseline.tsv",
		},
		{
			name:  "the census line, plural spelling",
			body:  "`scanned 251 anchor row(s), 168 incoherent; table carries 167 row(s) in devdoc/DOC-INDEX.tsv`\n",
			count: 167, unit: "row", file: "devdoc/DOC-INDEX.tsv",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CountClaimsIn(tc.body)
			if len(got) != 1 {
				t.Fatalf("CountClaimsIn() = %d claims (%+v), want exactly 1", len(got), got)
			}
			if got[0].Count != tc.count || got[0].Unit != tc.unit || got[0].File != tc.file {
				t.Errorf("claim = {count:%d unit:%q file:%q}, want {count:%d unit:%q file:%q}",
					got[0].Count, got[0].Unit, got[0].File, tc.count, tc.unit, tc.file)
			}
		})
	}
}

// TestCountClaimsInStaysSilent is the POSITIVE CONTROL, and it is the arm this
// predicate's whole shape rests on: a scanner that fires on an honest briefing is
// worse than no scanner, because the stamp it writes costs the child's budget on
// a surface that had nothing wrong with it.
//
// Every silent shape below was MEASURED — either in the 400-payload probe this
// predicate's bounds were tuned against (2026-10-14, the operator's ledger) or in
// the incident's own text. The probe went 8 firings → 5 → 3 → 1 as the bounds
// landed; this test is what keeps them from being relaxed back.
func TestCountClaimsInStaysSilent(t *testing.T) {
	silent := []struct {
		why  string
		body string
	}{
		// Rule 1: no count-digit-adjacent unit phrase.
		{"prose with numbers but no unit", "the fix landed after 3 attempts, 41 commits behind master\n"},
		{"a unit with no count beside it", "the table carries rows while the tree now produces fewer rows\n"},
		// Rule 2: past-tense reporting — the base clause owns this reading.
		{"reporting the past", "the table carried 168 rows before the repair\n"},
		{"a past scan figure", "anchor-number-census scanned 252 anchor row(s), 167 incoherent\n"},
		// Rule 3 refined by measurement: a SEPARATOR is not a file.
		{"wrap-tail separator (measured on the incident payload)",
			"`scanned 251 anchor row(s), 168 incoherent; table carries 167 row(s)` /\n"},
		{"no file token on the line", "the table carries 168 rows and the tree now produces 167 anchor rows\n"},
		// Rule 2 refined by measurement: deltas and plans, the shapes that made
		// up 7 of the 8 first-pass firings over the real corpus.
		{"a delta (the census writer's own line)",
			"the write's own delta: 0 permit(s) added, 168 permit(s) removed → wrote .../anchor-number-baseline.tsv\n"},
		{"a plan", "(stderr: would write 1 row(s): ['devdoc/proc-scan-cost-and-test-budget.md'])\n"},
		{"a signed delta", "- devdoc/DOC-INDEX.tsv (+1 row) and devdoc/x.md (+69 new file).\n"},
		{"a zero figure", "2. `scripts/g4-help-surface.sh` — a `derive` row for it scans 0 rows in the doc\n"},
		{"a bare byte figure is not a population",
			"**而文件已经完整落盘**（`-rw-r--r-- 4585 bytes`，sha256=8ece56a9…）。\n"},
		// Ordinary briefings that merely mention a path.
		{"a path with no figure", "the file internal/taskreport/baseclaim.go needs a new scanner\n"},
	}
	for _, tc := range silent {
		t.Run(tc.why, func(t *testing.T) {
			if got := CountClaimsIn(tc.body); len(got) != 0 {
				t.Errorf("CountClaimsIn() fired on a shape that must stay silent: %+v\n  body: %s", got, tc.body)
			}
		})
	}
}

// TestCountClaimsInIsBoundedAndDeduplicated pins the two budget guards: a
// briefing with a systemic problem cannot push the child's prompt past what a
// reader reads, and the same file+figure repeated across a transcript is ONE
// claim (the incident's text repeats its figure in prose AND in a test
// transcript).
func TestCountClaimsInIsBoundedAndDeduplicated(t *testing.T) {
	body := "the table internal/docclass/testdata/anchor-number-baseline.tsv carries 168 rows\n" +
		"the table internal/docclass/testdata/anchor-number-baseline.tsv carries 168 rows\n"
	if got := CountClaimsIn(body); len(got) != 1 {
		t.Errorf("repeated file+figure = %d claims, want 1", len(got))
	}
	many := ""
	for i := 0; i < 20; i++ {
		many += itoa(i) + " rows in devdoc/doc" + itoa(i) + ".md\n"
	}
	if got := CountClaimsIn(many); len(got) != countClaimMaxClaims {
		t.Errorf("a 20-claim briefing = %d claims, want the cap %d", len(got), countClaimMaxClaims)
	}
}

// TestCountClaimLineExcerptIsBounded pins the record's budget: a quoted line can
// be a paragraph and a record has to stay a record.
func TestCountClaimLineExcerptIsBounded(t *testing.T) {
	long := ""
	for i := 0; i < countClaimLineMaxRunes*2; i++ {
		long += "x"
	}
	got := CountClaimLineExcerpt(long)
	if len([]rune(got)) != countClaimLineMaxRunes+1 {
		t.Errorf("excerpt runes = %d, want %d + the ellipsis", len([]rune(got)), countClaimLineMaxRunes)
	}
	if short := CountClaimLineExcerpt("  ok  "); short != "ok" {
		t.Errorf("short excerpt = %q, want %q", short, "ok")
	}
}
