package lineedit

// turn_spill_header_test.go — the gates for the SPILL FORM of the turn header:
// the header a turn gets when its input was a paste spill rather than text the
// operator typed.
//
// The four gates, one per way the specialised form can be quietly wrong:
//
//  1. TestTurnSpillHeader_IsTotalInColsAtEveryWidth — the shape's hard floor: the
//     header never occupies more cells than it was given, at every width from 0
//     up. This is the same discipline TurnHeader has always had, asserted over
//     the new body.
//
//  2. TestTurnSpillHeader_MarksEveryOmission — the rule the paste case asks for:
//     when the header cannot show all of the payload's lines, it SAYS how many it
//     left out, and the count is right (shown + dropped == the payload's own line
//     count). A silently shortened summary is the defect this leg exists to catch.
//
//  3. TestTurnSpillHeader_IsDistinguishableFromAPlainTurn — the whole point of a
//     specialised form: a reader (and a test) can tell a spilled turn's header
//     from an ordinary one's in the same band, and the spill header carries the
//     facts the ordinary one cannot (bytes, runes, the payload's line count, the
//     handle on the file) while the ordinary form keeps its own bytes.
//
//  4. TestReadSpillFacts_CountsWhatTheFileHolds — the facts the header prints are
//     READ FROM THE FILE, so their exactness is a gate of its own: bytes and runes
//     over a whole payload, the rune count independent of the read boundary, the
//     line count with and without a trailing newline, and an unreadable path
//     failing loudly rather than reporting a paste that was never verified.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// spillTestPath is the path the harness invents for a spilled payload. It is a
// REAL-shaped absolute path because that is what the store mints
// (`paste-<stamp>-<n>.txt` under the session's own paste directory), and the width
// arithmetic is only meaningful over the shape production actually writes.
const spillTestPath = "/home/user/.gopi/sessions/01a0e922-69fe-740c-8c0b-901ee777fc06/paste/paste-20260929-013000-1.txt"

// spillPathFragment is the part of that path a mid-width header can afford: the
// header never claims the WHOLE path until the band allows it, so a leg asserting
// "the header names the file" must assert the fragment that must survive.
const spillPathFragment = "/home/user/.gopi/sessions/01a0e922"

// spillHandleOnlyBudget is the widest cell budget at which spillContent must
// choose the HANDLE alone, DERIVED from the payload rather than hard-coded: it is
// one cell short of what it takes to state the size (the description's own floor)
// beside a handle the minimum width, which is exactly the boundary the production
// split tests. A leg that spelled a number here would go on passing after the
// floor moved — it is the floor it is about.
func spillHandleOnlyBudget(s TurnSpill) int {
	floor := displayWidth(s.spillSizeStatement(0) + turnSpillMoreMarker(s.Lines))
	return displayWidth(StatusSep) + floor + turnSpillHandleMinCells - 1
}

// spillTestInfo builds the turn under test: a spilled input of the given size,
// with a three-line head.
func spillTestInfo(index, bytes, runes, lines int) TurnInfo {
	ref := "[" + PasteRefKeyword + " " + strconv.Itoa(runes) + ": " + spillTestPath + "]"
	return TurnInfo{
		Index:  index,
		Prompt: ref, // what the input line actually carries
		Spill: TurnSpill{
			Spilled: true,
			Bytes:   bytes,
			Runes:   runes,
			Lines:   lines,
			Handle:  spillTestPath,
			Head:    []string{"$ git status", "On branch master", "nothing to commit, working tree clean"},
		},
	}
}

// TestTurnSpillHeader_IsTotalInColsAtEveryWidth is the shape's hard floor.
func TestTurnSpillHeader_IsTotalInColsAtEveryWidth(t *testing.T) {
	info := spillTestInfo(3, 65536, 65536, 11)
	for cols := 0; cols <= 240; cols++ {
		got := TurnHeader(info, cols)
		if w := displayWidth(got); w > cols {
			t.Fatalf("cols=%d: the spill header occupies %d cells — not total in cols\n%q", cols, w, got)
		}
		// A header with cells to spend and nothing rendered is the other half of
		// the failure: the renderer must not silently drop the turn's chrome.
		if cols >= turnHeaderMinCells && got == "" {
			t.Fatalf("cols=%d: the spill header is empty although the width is drawable", cols)
		}
		// THE NARROW LADDER'S OWN CONTRACT: below turnHeaderMinCells the band is
		// too small for the rule chrome, and the header must be the body — never a
		// bare rule with no label (that would be chrome for nothing).
		if cols > 0 && cols < turnHeaderMinCells {
			if strings.Trim(got, TurnRuleGlyph) == "" {
				t.Fatalf("cols=%d: the header degraded to a bare rule (%q), which names no spill", cols, got)
			}
		}
	}
}

// TestTurnSpillHeader_MarksEveryOmission is the "never silently omit" gate, and
// it is asserted against the DELIVERED summary (spillSummaryFit), not against a
// re-implementation: whatever the header shows, the count it prints must account
// for the lines it did not show.
func TestTurnSpillHeader_MarksEveryOmission(t *testing.T) {
	const payloadLines = 11
	info := spillTestInfo(3, 65536, 65536, payloadLines)

	t.Run("a band narrow enough to drop head lines still says the size", func(t *testing.T) {
		cols := turnLabelHalfCells * 7
		got := TurnHeader(info, cols)
		for _, want := range []string{TurnSpillBytesMark, TurnSpillRunesMark, TurnSpillLinesMark, "more line"} {
			if !strings.Contains(got, want) {
				t.Fatalf("the header at %d cols does not carry %q: %q", cols, want, got)
			}
		}
	})

	t.Run("a band too narrow for the summary keeps the handle on the bytes", func(t *testing.T) {
		// Below the summary's own floor the body states the FILE rather than a
		// description: the handle is the field with no substitute — the payload is
		// one open away and this header is the only place the operator sees where
		// it landed — so the handle is the whole body, cut with the marker rather
		// than shortened silently. Asserted on spillContent, where the cell budget
		// IS the input rather than a consequence of the rule chrome around it, and
		// at a budget DERIVED from the payload (one cell short of holding both), so
		// the leg cannot go on passing on a width where its branch is never taken.
		got := spillContent(info.Spill, spillHandleOnlyBudget(info.Spill))
		if !strings.Contains(got, spillPathFragment) {
			t.Fatalf("the body %q spends a budget that cannot hold the summary on something other than the path's own leading cells", got)
		}
		if !strings.HasSuffix(got, StatusEllipsis) {
			t.Fatalf("the body %q cut the handle without marking it, so the path reads as complete", got)
		}
		if strings.Contains(got, TurnSpillBytesMark) {
			t.Fatalf("the body %q claims a size it had no room to verify beside the handle", got)
		}
	})

	t.Run("the marker counts the lines the header did not show", func(t *testing.T) {
		got := spillContent(info.Spill, 220)
		if !strings.Contains(got, "more line") {
			t.Fatalf("the summary dropped lines without marking it (Rule 15): %q", got)
		}
		fields := strings.Fields(got)
		dropped := -1
		for _, f := range fields {
			if !strings.HasPrefix(f, "(+") {
				continue
			}
			n, err := strconv.Atoi(strings.TrimPrefix(f, "(+"))
			if err != nil {
				t.Fatalf("the elision marker is not machine-readable: %q", got)
			}
			dropped = n
		}
		if dropped < 0 {
			t.Fatalf("the elision marker carries no count: %q", got)
		}
		if dropped > payloadLines {
			t.Fatalf("the marker claims %d of %d lines were dropped — more than the payload holds", dropped, payloadLines)
		}
		// THE COUNT IS EXACT: shown + dropped == the payload's own line count.
		shown := 0
		for i, f := range fields {
			if f == TurnSpillHeadMark && i+1 < len(fields) {
				n, err := strconv.Atoi(fields[i+1])
				if err != nil {
					t.Fatalf("the head count is not a number in %q", got)
				}
				shown = n
			}
		}
		if shown+dropped != payloadLines {
			t.Fatalf("shown=%d + dropped=%d != %d payload lines — the header's account does not add up: %q",
				shown, dropped, payloadLines, got)
		}
	})

	t.Run("the marker's spelling and empty case", func(t *testing.T) {
		// Asserted at the function that OWNS the spelling, because a real band
		// rarely reaches the singular: a marker reading "(+1 more lines)" would be
		// visible breakage on the one band that does.
		if got := turnSpillMoreMarker(0); got != "" {
			t.Errorf("turnSpillMoreMarker(0) = %q, want no marker: a complete summary must not claim an omission", got)
		}
		if got := turnSpillMoreMarker(1); got != turnSpillMoreOne {
			t.Errorf("turnSpillMoreMarker(1) = %q, want %q", got, turnSpillMoreOne)
		}
		if got := turnSpillMoreMarker(2); !strings.Contains(got, "2 more lines") {
			t.Errorf("turnSpillMoreMarker(2) = %q, want the plural spelling", got)
		}
	})

	t.Run("a summary that fits everything carries no marker", func(t *testing.T) {
		small := TurnSpill{Spilled: true, Bytes: 5, Runes: 5, Lines: 1, Head: []string{"hello"}, Handle: "/tmp/p.txt"}
		got := small.spillSummaryFit(200)
		if strings.Contains(got, "more line") {
			t.Fatalf("a fully shown payload was marked as truncated: %q", got)
		}
		if !strings.Contains(got, "hello") {
			t.Fatalf("the head line the band could afford is missing: %q", got)
		}
	})

	t.Run("a spill with no handle still states its size", func(t *testing.T) {
		// The caller that could not read the payload hands a handle and no
		// counts, and the caller that could not even name a path hands nothing:
		// the summary is then the whole body and still accounts for the lines it
		// did not draw.
		got := spillContent(TurnSpill{Spilled: true, Bytes: 65536, Runes: 40000, Lines: 900}, 120)
		for _, want := range []string{"65536" + TurnSpillBytesMark, "40000" + TurnSpillRunesMark, "900" + TurnSpillLinesMark, "more line"} {
			if !strings.Contains(got, want) {
				t.Fatalf("a handle-less spill summary does not carry %q: %q", want, got)
			}
		}
	})
}

// TestTurnSpillHeader_IsDistinguishableFromAPlainTurn is the gate the epic asks
// for by name: in one band, a spilled turn and a typed turn must not look alike,
// and the spilled one must carry facts the typed one cannot.
func TestTurnSpillHeader_IsDistinguishableFromAPlainTurn(t *testing.T) {
	const cols = 160
	info := spillTestInfo(3, 65536, 65536, 11)
	spill := TurnHeader(info, cols)
	// The ordinary prompt deliberately avoids every word of the spill vocabulary:
	// the assertion below is that the two forms are separable, and a prompt that
	// happened to contain the word would test the fixture instead.
	plain := TurnHeader(TurnInfo{Index: 4, Prompt: "read the git log for me and tell me what changed"}, cols)

	if spill == plain {
		t.Fatalf("a spilled header and a typed header rendered identically:\n%s", spill)
	}
	// The spill's own vocabulary, on the spill only.
	for _, want := range []string{TurnSpillBytesMark, TurnSpillRunesMark, TurnSpillLinesMark, TurnSpillLabelName, spillPathFragment} {
		if !strings.Contains(spill, want) {
			t.Errorf("the spill header does not carry %q:\n%s", want, spill)
		}
		if strings.Contains(plain, want) {
			t.Errorf("an ordinary header carries the spill vocabulary %q:\n%s", want, plain)
		}
	}
	// THE WRONG NUMBER MUST NOT APPEAR: the stale size a reference carries is what
	// this form replaces, so a header stating it beside the verified count is the
	// defect wearing the fix's clothes.
	stale := "65536" + TurnSpillRunesMark
	if strings.Count(spill, stale) != 1 {
		t.Errorf("the header states the payload's rune count %d times, want once:\n%s", strings.Count(spill, stale), spill)
	}
	if strings.Contains(spill, "["+PasteRefKeyword) {
		t.Errorf("the spill header reprints the reference line (which carries its own stale size):\n%s", spill)
	}
	// The whole handle survives when the band can afford it: the absolute path is
	// the one field a reader must be able to use, so it is asserted at a width that
	// has room for it (the narrower widths show it cut, with the marker).
	wide := TurnHeader(info, 320)
	if !strings.Contains(wide, spillTestPath) {
		t.Errorf("the spill header never shows the whole path, even at 320 cols:\n%s", wide)
	}
	if !strings.Contains(wide, TurnSpillLabelName) {
		t.Errorf("the spill header does not name its form (%q):\n%s", TurnSpillLabelName, wide)
	}
	// The ordinary header keeps its own identity, untouched.
	if !strings.Contains(plain, turnLabelTurn) || !strings.Contains(plain, "read the git log for me") {
		t.Errorf("the ordinary header no longer names its turn and prompt:\n%s", plain)
	}
	// Both forms stay inside the band and both are drawn between the rules.
	for _, s := range []string{spill, plain} {
		if w := displayWidth(s); w != cols {
			t.Errorf("a header occupies %d cells at cols=%d:\n%q", w, cols, s)
		}
		if !strings.HasPrefix(s, strings.Repeat(TurnRuleGlyph, turnRuleMinCells)) {
			t.Errorf("a header does not open with its lead rule:\n%q", s)
		}
	}
	// The two forms differ where a reader looks FIRST: the words before the first
	// separator.
	head := func(s string) string {
		s = strings.TrimLeft(s, TurnRuleGlyph+turnLabelLead)
		return strings.SplitN(s, StatusSep, 2)[0]
	}
	if head(spill) == head(plain) {
		t.Errorf("both forms open with the same words: %q", head(spill))
	}
	if head(spill) != TurnSpillLabelName+turnSpillHeadAir+"3" {
		t.Errorf("the spill header opens with %q, want the form's own identity", head(spill))
	}
	if head(plain) != turnLabelTurn+"4" {
		t.Errorf("the ordinary header's opening words changed: %q", head(plain))
	}
}

// TestReadSpillFacts_CountsWhatTheFileHolds pins the reader whose numbers the
// header prints: the counts must be the FILE's, and the rune count must not depend
// on where the read boundary fell.
func TestReadSpillFacts_CountsWhatTheFileHolds(t *testing.T) {
	dir := t.TempDir()

	t.Run("bytes runes and lines over an ASCII payload", func(t *testing.T) {
		path := filepath.Join(dir, "ascii.txt")
		body := "$ git status\nOn branch master\nnothing to commit\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write the payload: %v", err)
		}
		facts, err := ReadSpillFacts(path)
		if err != nil {
			t.Fatalf("ReadSpillFacts: %v", err)
		}
		if facts.Bytes != len(body) {
			t.Errorf("Bytes = %d, want %d", facts.Bytes, len(body))
		}
		if want := utf8.RuneCountInString(body); facts.Runes != want {
			t.Errorf("Runes = %d, want %d", facts.Runes, want)
		}
		if facts.Lines != 3 {
			t.Errorf("Lines = %d, want 3 (the trailing newline does not open a fourth)", facts.Lines)
		}
		if len(facts.Head) != 3 || facts.Head[0] != "$ git status" {
			t.Errorf("Head = %q, want the payload's own three lines", facts.Head)
		}
	})

	t.Run("a payload with no trailing newline still ends a line", func(t *testing.T) {
		path := filepath.Join(dir, "noeol.txt")
		if err := os.WriteFile(path, []byte("one line, no newline"), 0o600); err != nil {
			t.Fatalf("write the payload: %v", err)
		}
		facts, err := ReadSpillFacts(path)
		if err != nil {
			t.Fatalf("ReadSpillFacts: %v", err)
		}
		if facts.Lines != 1 {
			t.Errorf("Lines = %d, want 1: a paste with no final newline is not zero lines long", facts.Lines)
		}
		if len(facts.Head) != 1 || facts.Head[0] != "one line, no newline" {
			t.Errorf("Head = %q, want the whole (newline-less) line", facts.Head)
		}
	})

	t.Run("CJK is counted in runes, not bytes", func(t *testing.T) {
		path := filepath.Join(dir, "cjk.txt")
		body := strings.Repeat("知知知知知\n", 3)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write the payload: %v", err)
		}
		facts, err := ReadSpillFacts(path)
		if err != nil {
			t.Fatalf("ReadSpillFacts: %v", err)
		}
		if want := utf8.RuneCountInString(body); facts.Runes != want {
			t.Errorf("Runes = %d, want %d — the count must be runes, and not the byte length (%d)", facts.Runes, want, len(body))
		}
		if facts.Bytes != len(body) {
			t.Errorf("Bytes = %d, want %d", facts.Bytes, len(body))
		}
		if facts.Lines != 3 {
			t.Errorf("Lines = %d, want 3", facts.Lines)
		}
	})

	t.Run("the rune count does not depend on the read boundary", func(t *testing.T) {
		// A payload far larger than the reader's buffer, with CJK text throughout,
		// so a boundary lands mid-rune many times over. If the count used the raw
		// chunk it would inflate by one per boundary; if it dropped a partial rune
		// it would deflate.
		path := filepath.Join(dir, "big-cjk.txt")
		const groups = 40000
		const groupLines = 8
		var b strings.Builder
		for i := 0; i < groups; i++ {
			b.WriteString("知")
			if i%groupLines == groupLines-1 {
				b.WriteString("\n")
			}
		}
		body := b.String()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write the payload: %v", err)
		}
		facts, err := ReadSpillFacts(path)
		if err != nil {
			t.Fatalf("ReadSpillFacts: %v", err)
		}
		if want := utf8.RuneCountInString(body); facts.Runes != want {
			t.Errorf("Runes = %d, want %d over a %d-byte payload (the read boundary must not change it)", facts.Runes, want, len(body))
		}
		if facts.Bytes != len(body) {
			t.Errorf("Bytes = %d, want %d", facts.Bytes, len(body))
		}
		if want := groups / groupLines; facts.Lines != want {
			t.Errorf("Lines = %d, want %d", facts.Lines, want)
		}
		// The reader is STREAMING, not loading: the payload is far past the
		// buffer, and only the head's few lines were captured.
		if len(body) <= spillFactsBufBytes {
			t.Fatalf("the fixture is %d bytes, not past the reader's %d-byte buffer — it cannot exercise a boundary", len(body), spillFactsBufBytes)
		}
		if len(facts.Head) != turnSpillHeadMaxLines {
			t.Errorf("Head carries %d line(s), want the bounded %d", len(facts.Head), turnSpillHeadMaxLines)
		}
	})

	t.Run("a head line past the capture bound is marked truncated", func(t *testing.T) {
		path := filepath.Join(dir, "longline.txt")
		body := strings.Repeat("x", spillHeadLineCapture*2) + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write the payload: %v", err)
		}
		facts, err := ReadSpillFacts(path)
		if err != nil {
			t.Fatalf("ReadSpillFacts: %v", err)
		}
		if !facts.HeadTruncated {
			t.Error("a head line past the capture bound was not marked as captured in part")
		}
		if facts.Bytes != len(body) {
			t.Errorf("Bytes = %d, want the whole file's %d — the capture bound must not shorten the COUNT", facts.Bytes, len(body))
		}
	})

	t.Run("an unreadable path fails loudly", func(t *testing.T) {
		if _, err := ReadSpillFacts(filepath.Join(dir, "does-not-exist.txt")); err == nil {
			t.Fatal("reading a missing payload reported success (Rule 15)")
		}
	})
}
