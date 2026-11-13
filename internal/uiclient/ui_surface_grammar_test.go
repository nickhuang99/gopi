package uiclient

// ui_surface_grammar_test.go — the `gopi ui` surface's rows are compared under
// ITS OWN grammar, and a line the surface painted faithfully is never reported
// as a paint loss.
//
// THE MEASURED DEFECT (task 01a0e62d, 2026-09-28, a real 110x40 pty). The grid
// of a live `gopi ui --dump-screen` carried
//
//	003|gopi> UI-AUTH-OK-8842 qwerty
//
// and the session record beside the dump carried the transcript line
// `UI-AUTH-OK-8842 qwerty`, i.e. in_transcript was TRUE. The compare stripped
// only the CHAT surface's band markers (▌ / ·), which left the row reading
// `gopi> UI-AUTH-OK-8842 qwerty`, so no row carried the line:
//
//	outcome=disagreed damage=paint_loss prefix=0 reason="…the paint lost it…"
//
// on a screen that had painted the line perfectly. The line was on the grid, the
// line was in the log, and the reader was told the paint lost it — the exact
// inversion these verdicts exist to prevent.
//
// THE FIX UNDER TEST is that the row grammar is read from the surface that WROTE
// the row (CompareGutters(surface)). The cases below are the two directions —
// the ui row matches once its own label is removed, and the chat row keeps
// matching — plus the honesty control: the ui labels are the UI surface's
// grammar, so they are NOT stripped away from a chat row.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// uiGrammarLine is the model's line as the AUTHORITY holds it — the text, which
// never carries a surface's row label. This is the pair a live run measured.
const uiGrammarLine = "UI-AUTH-OK-8842 qwerty"

// uiGrammarRow is the SAME line as the ui surface draws it: its own row label in
// front of the text (internal/uiclient/model.go's wrapEntry).
const uiGrammarRow = "gopi> " + uiGrammarLine

// uiGrammarCandidate builds a readable, painted candidate at a REAL geometry
// whose grid carries the given rows, for the given surface.
func uiGrammarCandidate(surface Surface, rows ...string) Candidate {
	return Candidate{
		Path:    "/fixture/" + DumpFileNames()[surface],
		Surface: surface,
		State:   StateExited,
		Frames:  7,
		Record:  DumpRecord{Fields: map[string]string{dumpRecordGeometryField: string(GeometrySourceLayout)}},
		Dump:    lineedit.ScreenDump{Cols: 80, Rows: 10, Lines: rows},
	}
}

// TestUIRowIsComparedUnderTheUISurfaceGrammar is the RED PROOF of the measured
// defect: the transcript line IS on the ui surface's grid, and the compare must
// say so. Before the fix this answered CompareMismatch with damage paint_loss.
func TestUIRowIsComparedUnderTheUISurfaceGrammar(t *testing.T) {
	cand := uiGrammarCandidate(SurfaceUI, "header", uiGrammarRow)
	v := CompareTranscriptLine(uiGrammarLine, cand, []string{uiGrammarLine})

	if v.Status != CompareMatch {
		t.Fatalf("status=%s damage=%s prefix=%d in_grid=%t in_transcript=%t\n"+
			"want %s: the ui surface PAINTED this line (its row carries it behind the "+
			"surface's own label %q) and the authority holds it — reporting a paint "+
			"loss here is the diagnosis inverted",
			v.Status, v.Damage, v.GridPrefixRunes, v.InGrid, v.InTranscript, CompareMatch, uiGrammarRow)
	}
	if !v.InGrid {
		t.Fatalf("in_grid=false for a line the ui surface's own row draws: %q", uiGrammarRow)
	}
	if v.Damage != DamageNone {
		t.Fatalf("damage=%s for an intact arrival — an arrival must carry no loss class", v.Damage)
	}
}

// TestEveryUIRowLabelIsStripped is the same case for every label the ui surface
// draws, so the fix cannot cover the assistant row alone.
func TestEveryUIRowLabelIsStripped(t *testing.T) {
	for _, prefix := range uiRowPrefixes() {
		cand := uiGrammarCandidate(SurfaceUI, prefix+uiGrammarLine)
		v := CompareTranscriptLine(uiGrammarLine, cand, []string{uiGrammarLine})
		if v.Status != CompareMatch {
			t.Fatalf("status=%s for a ui row labelled %q (%q): every label the surface draws must be its grammar",
				v.Status, prefix, prefix+uiGrammarLine)
		}
	}
}

// TestChatRowGrammarIsUnchangedByTheUISurfaceWidening is the anti-regression
// control: the chat surface's band markers still strip, and a chat row still
// matches. Widening the marker set must not have moved the chat surface's case.
func TestChatRowGrammarIsUnchangedByTheUISurfaceWidening(t *testing.T) {
	cand := uiGrammarCandidate(SurfaceChat, lineedit.TurnMarker+uiGrammarLine)
	v := CompareTranscriptLine(uiGrammarLine, cand, []string{uiGrammarLine})
	if v.Status != CompareMatch {
		t.Fatalf("status=%s for a CHAT row under the chat grammar: %q", v.Status, cand.Dump.Lines)
	}
}

// TestTheUILabelIsNotStrippedFromAChatRow is the honesty control on the union
// itself: the ui labels are the UI surface's grammar, and a chat row whose text
// genuinely BEGINS with `gopi> ` must keep it — a strip that removed it would
// invent an arrival for text the chat surface never had.
func TestTheUILabelIsNotStrippedFromAChatRow(t *testing.T) {
	const literal = "gopi> is the ui surface's label"
	if got := stripCompareGutters(lineedit.TurnMarker+literal, SurfaceChat); got != literal {
		t.Fatalf("stripCompareGutters(chat)=%q, want %q — the ui labels must not be removed from a chat row", got, literal)
	}
	if got := stripCompareGutters(uiGrammarRow, SurfaceUI); got != uiGrammarLine {
		t.Fatalf("stripCompareGutters(ui)=%q, want %q", got, uiGrammarLine)
	}
}

// TestUIRowPrefixesMatchTheRenderer pins the list the READER strips against the
// values the RENDERER draws (model.go's wrapEntry selects one per entry kind).
// The two are separate declarations on purpose — one is the grammar, the other is
// the switch that paints — so a prefix added to the renderer and forgotten in the
// grammar is exactly the drift that produced the measured defect.
func TestUIRowPrefixesMatchTheRenderer(t *testing.T) {
	m := model{}
	cases := []struct {
		kind entryKind
		want string
	}{
		{entryUser, userPrefix},
		{entryAssistant, assistantPrefix},
		{entryTool, toolPrefix},
		{entryError, errorPrefix},
		{entryNotice, noticePrefix},
		{entryReasoning, reasoningPrefix},
	}
	known := map[string]bool{}
	for _, p := range uiRowPrefixes() {
		known[p] = true
	}
	for _, tc := range cases {
		rendered := m.wrapEntry(entry{kind: tc.kind, text: uiGrammarLine}, 80)
		if len(rendered) == 0 {
			t.Fatalf("the renderer produced no row for entry kind %v", tc.kind)
		}
		if !strings.HasPrefix(rendered[0], tc.want) {
			t.Fatalf("the renderer draws %q but the reader strips %q — the grammar has drifted from the paint",
				rendered[0], tc.want)
		}
		if !known[tc.want] {
			t.Fatalf("the renderer draws the prefix %q and uiRowPrefixes does not carry it: the reader would leave it in the row", tc.want)
		}
	}
}

// TestNoAuthorityIsItsOwnStatusAndNotice pins the vocabulary the fix adds: an
// artifact with no derivable authority is reported as `no-authoritative-side`,
// a DIFFERENT word from `no-transcript` (a transcript that was READ and held
// nothing). A reader who cannot tell them apart cannot tell "the log is empty"
// from "this surface has no log" — the measured complaint.
func TestNoAuthorityIsItsOwnStatusAndNotice(t *testing.T) {
	if CompareNoAuthority == CompareNoTranscript {
		t.Fatalf("no-authoritative-side and no-transcript collapsed into one word (%q)", CompareNoAuthority)
	}
	if AuditNoAuthority == AuditNoTranscript {
		t.Fatalf("the audit's two absences collapsed into one word (%q)", AuditNoAuthority)
	}
	if string(CompareNoAuthority) != string(AuditNoAuthority) {
		t.Fatalf("the two faces spell the same fact two ways: %q vs %q", CompareNoAuthority, AuditNoAuthority)
	}
	for _, want := range []string{string(CompareNoAuthority), "--session", "nothing was compared"} {
		if !strings.Contains(NoAuthoritativeSideNotice, want) {
			t.Fatalf("the notice does not carry %q: %q", want, NoAuthoritativeSideNotice)
		}
	}
}

// TestCompareGuttersIsPerSurface pins the mechanism directly: the two surfaces'
// grammars differ, the ui grammar carries the chat markers (a dump read at the
// wrong marker must not be answered with a false loss), and the chat grammar does
// NOT carry the ui labels (see TestTheUILabelIsNotStrippedFromAChatRow).
func TestCompareGuttersIsPerSurface(t *testing.T) {
	chatOnly := map[string]bool{}
	for _, g := range CompareGutters(SurfaceChat) {
		chatOnly[g] = true
	}
	ui := map[string]bool{}
	for _, g := range CompareGutters(SurfaceUI) {
		ui[g] = true
	}
	shared := map[string]bool{}
	for _, g := range uiSharedRowPrefixes() {
		shared[g] = true
	}
	for g := range chatOnly {
		if !ui[g] {
			t.Fatalf("the ui grammar dropped the chat marker %q: a dump read at the wrong surface marker would be answered with a false paint loss", g)
		}
	}
	for _, g := range uiRowPrefixes() {
		if chatOnly[g] && !shared[g] {
			t.Fatalf("the chat grammar carries the ui-only label %q: a chat row's own text starting with it would be stripped away", g)
		}
	}
	// The declared sharing must be TRUE, not a permission anyone can widen: a
	// prefix listed as shared while the chat grammar does not carry it is a
	// stale exception, and it would silence this pin for a label that is really
	// ui-only.
	for _, g := range uiSharedRowPrefixes() {
		if !chatOnly[g] {
			t.Fatalf("%q is declared shared but the chat grammar does not carry it — either the sharing ended (delete the entry) or the label is ui-only (the ban above applies)", g)
		}
	}
	// And the sharing that MOTIVATED the list is pinned by name, so a future
	// edit that gives the reasoning row a second spelling has to say so here.
	if !shared[reasoningPrefix] {
		t.Fatalf("the reasoning gutter %q is not declared shared — the two carriers mark the same role with one glyph (lineedit.ThinkingGutter)", reasoningPrefix)
	}
}
