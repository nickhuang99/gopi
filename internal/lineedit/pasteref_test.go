package lineedit

import (
	"strings"
	"testing"
)

// pasteref_test.go — the READER's half of the reference vocabulary.
//
// The producer is exercised end to end elsewhere (PasteStore.Spill + PasteRefFor
// through the real editor and through the chat carrier's boundary); what these
// tests pin is the PARSER, because the two must agree about one spelling and a
// parser that silently accepts a malformed line is how a second, drifting
// format starts.

// TestPasteRefPathsParsesTheRenderedSpelling is the round-trip gate: whatever
// PasteRefFor renders, PasteRefPaths reads back as its path and its reference.
func TestPasteRefPathsParsesTheRenderedSpelling(t *testing.T) {
	for _, path := range []string{
		"/home/user/.gopi/sessions/abc/paste/paste-20260927-204444-3.txt",
		"/tmp/p.txt",
	} {
		ref := PasteRefFor(PasteRef{Path: path, Bytes: 12, Runes: 12})
		got := PasteRefPaths(ref)
		if len(got) != 1 || got[0] != path {
			t.Errorf("PasteRefPaths(%q) = %q, want [%q]", ref, got, path)
		}
		if !HasPasteRef(ref) {
			t.Errorf("HasPasteRef(%q) = false, want true", ref)
		}
	}
}

// TestPasteRefPathsFindsAReferenceInsideALine pins the shape the editor produces:
// the reference sits on a line the user (or the editor) typed around it, so a
// parser requiring the whole string to be a reference would miss the ordinary
// case.
func TestPasteRefPathsFindsAReferenceInsideALine(t *testing.T) {
	ref := PasteRefFor(PasteRef{Path: "/s/paste-1.txt", Bytes: 3, Runes: 3})
	line := "look at this and tell me what is wrong " + ref + " thanks"
	got := PasteRefPaths(line)
	if len(got) != 1 || got[0] != "/s/paste-1.txt" {
		t.Fatalf("PasteRefPaths(%q) = %q, want the embedded path", line, got)
	}
}

// TestPasteRefPathsFindsEveryReference pins the multi-spill line: two pastes in
// one message state both, and the boundary must name both to the model.
func TestPasteRefPathsFindsEveryReference(t *testing.T) {
	a := PasteRefFor(PasteRef{Path: "/s/a.txt", Bytes: 1, Runes: 1})
	b := PasteRefFor(PasteRef{Path: "/s/b.txt", Bytes: 2, Runes: 2})
	got := PasteRefPaths(a + " and " + b)
	if len(got) != 2 || got[0] != "/s/a.txt" || got[1] != "/s/b.txt" {
		t.Fatalf("PasteRefPaths = %q, want both paths in order", got)
	}
}

// TestPasteRefPathsRefusesReferenceShapedNoise is the parser's negative half: a
// line that merely LOOKS like the marker must not produce a path. Each case is a
// shape a naive strings.Split would turn into a bogus payload path.
func TestPasteRefPathsRefusesReferenceShapedNoise(t *testing.T) {
	for _, s := range []string{
		"",
		"ordinary prose with no marker",
		"[paste",
		"[paste ]",
		"[paste abc: /p.txt]",   // no digits
		"[paste 12 /p.txt]",     // no separator
		"[paste 12: /p.txt",     // unterminated
		"[notpaste 12: /p.txt]", // wrong marker word
	} {
		if got := PasteRefPaths(s); len(got) != 0 {
			t.Errorf("PasteRefPaths(%q) = %q, want none", s, got)
		}
	}
}

// TestExceedsInputLineBoundIsTheLinesOwnCapacity pins the threshold's ONE
// definition: the predicate and the editing buffer's capacity are the same
// number, in the same unit, so a boundary that asked this question cannot
// disagree with the line that overflowed.
func TestExceedsInputLineBoundIsTheLinesOwnCapacity(t *testing.T) {
	if InputLineRuneBound != valueMaxRunesDefault {
		t.Fatalf("InputLineRuneBound = %d, want valueMaxRunesDefault = %d — the threshold must BE the line's capacity, not a second number",
			InputLineRuneBound, valueMaxRunesDefault)
	}
	if ExceedsInputLineBound(strings.Repeat("a", InputLineRuneBound)) {
		t.Error("a line exactly AT the bound is not over it")
	}
	if !ExceedsInputLineBound(strings.Repeat("a", InputLineRuneBound+1)) {
		t.Error("a line one rune past the bound must be over it")
	}
	// The unit is RUNES, not bytes: a multi-byte line at the bound is not over.
	wide := strings.Repeat("知", InputLineRuneBound)
	if len(wide) <= InputLineRuneBound {
		t.Fatalf("the fixture is not multi-byte (%d bytes for %d runes)", len(wide), InputLineRuneBound)
	}
	if ExceedsInputLineBound(wide) {
		t.Error("the bound is measured in runes — a 3-byte-per-rune line at the bound is not over it")
	}
}

// TestStripPasteRefsRemovesThePointerAndKeepsTheWords is the regression for the
// MEASURED defect this function was added for (2026-10-06, the paste half of the
// turn-boundary retrieval): a consumer that has already read the payload behind a
// reference must not spend its keyword budget on the pointer. The live run
// recorded `paste turn 5411 home nick workspace gopi sandbox` — the operator's two
// words plus the path's components and the size — for a turn whose pasted text was
// about something else entirely.
func TestStripPasteRefsRemovesThePointerAndKeepsTheWords(t *testing.T) {
	ref := PasteRefFor(PasteRef{
		Path:   "/home/user/workspace/gopi-sandbox/sandbox-x/.gopi/sessions/7349789c/paste/paste-20261006-041850-1.txt",
		Bytes:  5411,
		Runes:  5411,
		Source: "human",
	})
	for _, tc := range []struct {
		name, line, want string
	}{
		{"the pointer alone leaves nothing", ref, ""},
		{"the typed words survive", "paste turn" + ref, "paste turn"},
		{"words on both sides survive with a separator", "look at this" + ref + "thanks", "look at this thanks"},
		{"a plain line is untouched", "no reference here", "no reference here"},
		{"two references both go", "a" + ref + "b" + ref + "c", "a b c"},
	} {
		if got := StripPasteRefs(tc.line); got != tc.want {
			t.Errorf("%s: StripPasteRefs = %q, want %q", tc.name, got, tc.want)
		}
	}
	// The negative half: a pointer-shaped literal with no digits is not a
	// reference, so it is CONTENT and must survive (the same rule the parser's own
	// noise test pins).
	noise := "the editor renders [paste N: /abs/path] for one of these"
	if got := StripPasteRefs(noise); got != noise {
		t.Errorf("a reference-shaped literal was stripped: %q, want %q", got, noise)
	}
}
