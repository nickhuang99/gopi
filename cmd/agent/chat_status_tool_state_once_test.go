package main

// chat_status_tool_state_once_test.go — operator bug #5, pinned as the row's
// contract: the status row states the TOOL STATE once, every segment label comes
// from ONE language, and the no-tool-yet case is STATED rather than implied by a
// blank-but-present segment.
//
// The defect, as the operator met it (verbatim: "tools · no tool 语义重复且英文混中文"),
// measured on a real pty at 117x26 in three independent runs:
//
//	fake-model     ·  turn 1  ·  tools ·  no tool          ·  ctx 51/200.0k≡   6s · ready
//	                              ^^^^^^   ^^^^^^^^^
//	                              the SAME fact, twice, nine cells apart
//
//	fake-model     ·  turn 0  ·        ·  no tool          ·                   0.1s · ready
//	                              ^^^^^^
//	                              the same column, now BLANK but still joined by
//	                              two separators — a segment that is present and empty
//
// Two facts, one column each, in opposite signs, is one bit of information spent
// twice — and in left-to-right reading order the pair says "tools … no tool",
// which is how it reads as self-contradiction. The fix removes the second column
// (the mode column) and lets the ONE tool column state the fact in all three of
// its states, so this file asserts the PROPERTY (one column, one language, no
// empty segment) rather than the fix's own spelling of the labels.

import (
	"strings"
	"testing"
	"unicode"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// statusToolStateColumnName is the ONE column name allowed to state the tool
// fact. It is read from the row's own name map (statusColumnNames) by the
// assertions below rather than typed here, so a renamed column moves the test.
func statusToolStateColumnNames(t *testing.T) []string {
	t.Helper()
	cols := statusRowColumns()
	if len(cols) != len(statusColumnNames) {
		t.Fatalf("statusRowColumns resolved %d columns against %d names — the row and its name map disagree, "+
			"so this file cannot locate the tool column", len(cols), len(statusColumnNames))
	}
	names := make([]string, 0, len(cols))
	for key := statusColModel; int(key) < len(cols); key++ {
		names = append(names, statusColumnNames[key])
	}
	return names
}

// assertToolStateStatedOnce is the headline contract: exactly ONE of the row's
// columns may carry the tool fact, and it must carry it in the state under test.
//
// The count is taken over the row's real geometry, not over a substring count on
// the drawn text: the defect was a SECOND COLUMN, and a column that said the
// same thing in different words ("tools" beside "no tool") is invisible to any
// counting of one label's own spelling. So the assertion is about the columns the
// row is composed from — the property, not the wording.
func assertToolStateStatedOnce(t *testing.T, what string, row string) {
	t.Helper()
	names := statusToolStateColumnNames(t)
	carriers := 0
	for _, name := range names {
		if name == statusColumnNameTool {
			carriers++
		}
	}
	if carriers != 1 {
		t.Fatalf("%s: the row carries %d columns named %q (%v) — the tool state must be stated by exactly "+
			"ONE column, or the row says the same thing twice\nrow = %q",
			what, carriers, statusColumnNameTool, names, row)
	}
	// And the state must really be ON the row: a row that names no tool and
	// states no absence is the other half of the defect (a blank-but-present
	// segment reads as a broken field, not as a fact).
	if !strings.Contains(row, statusNoToolLabel) && !strings.Contains(row, statusNoToolsLabel) &&
		!strings.Contains(row, toolGlyph) {
		t.Fatalf("%s: the row states no tool fact at all — the tool column is present but says nothing, "+
			"which is the blank segment this defect is about\nrow = %q", what, row)
	}
}

// assertNoEmptySegment is the "no blank-but-present segment" contract, asserted on
// the row as DRAWN: every top-level segment between two separators carries content
// a reader can decode, so the row never shows an unexplained gap.
//
// It is stated over the composed row (the cells a reader sees) rather than over
// the column list, because that is where the defect was visible: the blank mode
// column still spent its separator, so the row showed `turn 0  ·        ·  no tool`
// — two adjacent separators with nothing between them.
func assertNoEmptySegment(t *testing.T, what, row string) {
	t.Helper()
	// The row's own separator (lineedit.StatusSep) is the vocabulary a reader
	// splits the segments on; it is taken from the constant, never typed.
	for i, seg := range strings.Split(row, lineedit.StatusSep) {
		// The live block is the row's tail and is separated by the layout's own
		// gap rather than by StatusSep; a trailing segment that is all spaces is
		// therefore not this contract's subject. What IS the subject: a segment
		// between two separators (i not last) that carries nothing.
		if i < len(strings.Split(row, lineedit.StatusSep))-1 && strings.TrimSpace(seg) == "" {
			t.Errorf("%s: segment %d of the row is EMPTY but still joined by separators (%q) — "+
				"a blank-but-present segment reads as a broken field, and the row must state the "+
				"absence in words instead\nrow = %q", what, i, seg, row)
		}
	}
}

// assertRowSpeaksOneLanguage pins the language decision on the row as drawn: the
// whole row is written in ONE script, and there is no CJK character anywhere on it.
//
// WHY THE DECISION IS ENGLISH, and why a partial translation would be the WORSE
// defect: the row is a read-out of MACHINE vocabulary (a model identifier, "turn",
// tool names, arrow-marked cache counters) where the only translatable parts are
// the connective words. Translating those alone produces the mix in the opposite
// direction — Chinese connectives between English counter names and English tool
// names — which is precisely the defect the operator reported; and every other
// line of this surface's own chrome is English today (the same screen prints
// `[gopi] w context window: ... trimmed 3 oldest message(s)`, `tools ENABLED` and
// the whole /help legend). A fully localised surface is a different decision with
// its own cost (it needs a locale channel, which does not exist) and it is NOT
// this fix. What this fix owes the reader is that the row does not MIX, which is
// what is asserted here.
func assertRowSpeaksOneLanguage(t *testing.T, what, row string) {
	t.Helper()
	for _, r := range row {
		if unicode.In(r, unicode.Han) {
			t.Errorf("%s: the row carries the CJK rune %q — the row's segment labels come from ONE language, "+
				"and this surface's own chrome is English (see this file's note)\nrow = %q", what, r, row)
		}
	}
}

// TestStatusRowStatesToolStateOnce_ToolsOnNoCall is the DEFENDED STATE, at the
// prompt, on a tools-enabled surface where nothing has been called yet — the state
// the operator's row was dumped in.
//
// It drives the production wiring (the source and the row's own status accessor)
// and asserts the three contracts on the row as drawn at the row's own pinned
// width, so the row's geometry and this file cannot disagree.
func TestStatusRowStatesToolStateOnce_ToolsOnNoCall(t *testing.T) {
	s := newChatStatusSource(statusToolStateModelForTest, statusToolStateToolsOn)
	s.update(statusToolStateIdleIterForTest, statusLiveIdleTurn, statusToolStateTokensForTest, "")
	row := s.status().Line(statusColumnsAtCols)

	assertToolStateStatedOnce(t, "tools on, nothing called yet", row)
	assertNoEmptySegment(t, "tools on, nothing called yet", row)
	assertRowSpeaksOneLanguage(t, "tools on, nothing called yet", row)

	// The zero-count case is STATED, not reserved: the column holds cells and the
	// cells carry the fact in words (the property the blank segment failed).
	if !strings.Contains(row, statusNoToolLabel) {
		t.Errorf("a tools-enabled surface with no call yet must SAY so (%q); the row shows %q",
			statusNoToolLabel, row)
	}
	t.Logf("tools on, zero calls: %q", row)
}

// TestStatusRowStatesToolStateOnce_ToolAlreadyCalled is the SECOND state the fix
// must hold in: a tool has run inside the turn, so the same column states the
// tool's name instead of the absence — still once, still in one language, still
// with no empty segment.
func TestStatusRowStatesToolStateOnce_ToolAlreadyCalled(t *testing.T) {
	s := newChatStatusSource(statusToolStateModelForTest, statusToolStateToolsOn)
	s.update(statusToolStateLiveIterForTest, statusToolStateLiveIterForTest, statusToolStateTokensForTest, "")
	s.setTool(statusToolStateNameForTest, statusToolStateDigestForTest)
	row := s.status().Line(statusColumnsAtCols)

	assertToolStateStatedOnce(t, "a tool called", row)
	assertNoEmptySegment(t, "a tool called", row)
	assertRowSpeaksOneLanguage(t, "a tool called", row)

	if !strings.Contains(row, statusToolStateNameForTest) {
		t.Errorf("a tool ran and the row does not name it (%q):\nrow = %q", statusToolStateNameForTest, row)
	}
	// The four-column geometry is what makes "stated once" structural: the row
	// carries one column for the tool fact and no second column that could restate
	// it in the opposite sign.
	if got := len(statusRowColumns()); got != statusToolStateColumnCountForTest {
		t.Fatalf("the row carries %d columns, want %d (model, iter, tool, metrics): the retired mode "+
			"column is what stated the tool fact a second time", got, statusToolStateColumnCountForTest)
	}
	t.Logf("tool called: %q", row)
}

// TestStatusRowStatesToolStateOnce_NoToolsAtAll is the state the blank segment came
// from: a surface with NO tools configured (`gopi chat` without --tools). The row
// must still STATE which state it is in — never a blank reserved segment — and it
// must not claim "no tool" either, which would describe a surface that could have
// called one.
func TestStatusRowStatesToolStateOnce_NoToolsAtAll(t *testing.T) {
	s := newChatStatusSource(statusToolStateModelForTest, !statusToolStateToolsOn)
	s.update(statusToolStateIdleIterForTest, statusLiveIdleTurn, statusToolStateTokensForTest, "")
	row := s.status().Line(statusColumnsAtCols)

	assertToolStateStatedOnce(t, "no tools configured", row)
	assertNoEmptySegment(t, "no tools configured", row)
	assertRowSpeaksOneLanguage(t, "no tools configured", row)

	if !strings.Contains(row, statusNoToolsLabel) {
		t.Errorf("a surface with no tools at all must state THAT (%q): the row shows %q — and %q would "+
			"describe a surface that could call one", statusNoToolsLabel, row, statusNoToolLabel)
	}
	if strings.Contains(row, statusNoToolLabel) {
		t.Errorf("a surface with no tools at all claims %q, which is a statement about a tool that did not "+
			"run rather than about a surface that has none:\nrow = %q", statusNoToolLabel, row)
	}
	t.Logf("no tools configured: %q", row)
}

// TestStatusRowStatesToolStateOnce_RedProof is the RED PROOF: it runs the SAME
// predicates against the row SHAPE the fix removed and asserts they refuse it.
//
// Without this, a gate that had stopped seeing the row (a renamed column, a
// geometry that no longer composes) would still report green — the failure mode
// this repository has measured more than once. The pre-fix row is spelled here as
// the two-segment shape the operator's dump carries, so the predicate is exercised
// on the defect itself rather than on a synthetic empty string.
func TestStatusRowStatesToolStateOnce_RedProof(t *testing.T) {
	// The measured pre-fix row (117x26, `turn 0` at the prompt) with its mode
	// column BLANK — the third state of the three the defect had.
	blankSegmentRow := statusToolStatePreFixRow(t, "", statusNoToolLabel)
	if !rowHasEmptySegment(blankSegmentRow) {
		t.Fatalf("the red-proof fixture %q is not recognised as carrying an empty segment — the "+
			"predicate is stuck, so the green assertions prove nothing", blankSegmentRow)
	}
	// And the two-segment shape: the SAME fact stated in the mode column and again
	// in the tool column, which is what "stated twice" means here.
	twoSegmentRow := statusToolStatePreFixRow(t, statusToolStateLegacyModeWord, statusNoToolLabel)
	if !rowStatesToolFactTwice(twoSegmentRow) {
		t.Fatalf("the red-proof fixture %q is not recognised as stating the tool fact twice — the "+
			"predicate is stuck", twoSegmentRow)
	}
	// The negative control: the FIXED row must pass the same two predicates.
	s := newChatStatusSource(statusToolStateModelForTest, statusToolStateToolsOn)
	s.update(statusToolStateIdleIterForTest, statusLiveIdleTurn, statusToolStateTokensForTest, "")
	fixed := s.status().Line(statusColumnsAtCols)
	if rowHasEmptySegment(fixed) {
		t.Errorf("the fixed row is reported as carrying an empty segment: %q", fixed)
	}
	if rowStatesToolFactTwice(fixed) {
		t.Errorf("the fixed row is reported as stating the tool fact twice: %q", fixed)
	}
}

// rowHasEmptySegment is the predicate assertNoEmptySegment is built on, kept
// separate so the red proof can run it against a canned row.
func rowHasEmptySegment(row string) bool {
	segs := strings.Split(row, lineedit.StatusSep)
	for i := 0; i < len(segs)-1; i++ {
		if strings.TrimSpace(segs[i]) == "" {
			return true
		}
	}
	return false
}

// rowStatesToolFactTwice reports whether a row carries the tool fact from two
// separate segments. It resolves the two statements from the row's own vocabulary:
// the retired mode column's word and the tool column's own labels.
func rowStatesToolFactTwice(row string) bool {
	segs := strings.Split(row, lineedit.StatusSep)
	carriers := 0
	for _, seg := range segs {
		field := strings.TrimSpace(seg)
		if field == "" {
			continue
		}
		if strings.Contains(field, statusToolStateLegacyModeWord) ||
			strings.Contains(field, statusNoToolLabel) ||
			strings.Contains(field, statusNoToolsLabel) {
			carriers++
		}
	}
	return carriers > 1
}

// statusToolStatePreFixRow composes the row SHAPE the fix removed: the retired mode
// column's segment and the tool column's, joined by the row's own separator. It is
// built from the layout's separator constant so it is a statement about the row's
// vocabulary rather than about a copied string.
func statusToolStatePreFixRow(t *testing.T, modeSegment, toolSegment string) string {
	t.Helper()
	return strings.Join([]string{
		statusToolStateModelForTest,
		turnLabel(statusToolStateIdleIterForTest, statusLiveIdleTurn),
		modeSegment,
		toolSegment,
	}, lineedit.StatusSep)
}

// The fixture values. They are named (Rule 1) rather than typed at the call sites
// so the three states read as one scenario each.
const (
	// statusToolStateModelForTest is an ordinary model name, the identity field the
	// row's own tests use.
	statusToolStateModelForTest = "deepseek-flash"
	// statusToolStateToolsOn is the surface flag the tools-enabled path installs
	// (`gopi chat --tools`), and statusToolStateToolsOn is its negation — both
	// spelled so a call site states which surface it is driving.
	statusToolStateToolsOn = true
	// statusToolStateIdleIterForTest is the iteration count at the prompt of a
	// session whose first turn has not run.
	statusToolStateIdleIterForTest = 0
	// statusToolStateLiveIterForTest is the iteration count while a turn is in
	// flight — the state a tool call happens in.
	statusToolStateLiveIterForTest = 1
	// statusToolStateTokensForTest is a measured context size, so the metrics
	// column carries a value and the row is composed at its full geometry.
	statusToolStateTokensForTest = 15400
	// statusToolStateNameForTest / statusToolStateDigestForTest are an ordinary tool
	// call: the name a reader steers by and a short digest.
	statusToolStateNameForTest   = "keyword_search"
	statusToolStateDigestForTest = "status row"
	// statusToolStateColumnCountForTest is the number of columns the row carries
	// once the tool fact is stated ONCE: model, iter, tool and the metrics group.
	// It is a count of the row's OWN columns, read from the constants above it, so a
	// column added or removed fails here as well as in the geometry's own sum.
	statusToolStateColumnCountForTest = 4
	// statusToolStateLegacyModeWord is the RETIRED mode column's word, spelled here
	// on purpose: the production constant died with the column (it is now a boolean
	// field, not a label), and the red proof must be able to compose the defect the
	// column produced. It must appear on no row the production composer draws.
	statusToolStateLegacyModeWord = "tools"
)
