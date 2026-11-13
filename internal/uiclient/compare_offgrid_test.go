package uiclient

// compare_offgrid_test.go — the THREE answers a containment check can give, each
// pinned separately, at the unit the whole fix is about.
//
// THE DEFECT THIS FILE PINS (task 01a1c05c, measured on a real chat dump): the
// one-line compare had TWO reachable verdicts for a line the transcript holds and
// no grid row carries — and one of them was `mismatch`, whichever the real cause
// was. So a line the grid had painted and then pushed off the top of its 22-row
// viewport reported the SAME status as a line the paint really lost, and a caller
// branching on that status concluded the paint was broken from a question the
// observation cannot answer. The same dump carried 2,518 rows of saved history
// beside the 22-row window: the evidence that the line was painted was IN THE
// ARTIFACT and nothing asked it.
//
// What is asserted here, one arm each for the three facts a caller must be able to
// tell apart WITHOUT reading English prose:
//
//  ① a line the grid WRAPS and carries whole (200+ characters, the width the epic
//     asks for) is a MATCH whose reason names no loss;
//  ② a line the grid PAINTED and the terminal has since scrolled out of the window
//     is the third answer — CompareOffGrid, with its reason's FIRST WORD saying so
//     — and NOT a mismatch;
//  ③ a line the transcript holds and neither the window NOR the saved history
//     carries is still a MISMATCH, and its reason's first word names the side the
//     loss is on (transcript-side: the authority has it, the paint does not).

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// The epic's own width requirement: a real over-wide sample, 200+ characters
// wide, with CJK and an unbroken ASCII token so the wrap lands MID-WORD (which is
// the shape a row-exact match cannot see).
const (
	offGridWrapCJK = "自观测闭环：一条比窗口更宽的答案行必须逐字符还原，包括超长词与中文标点，绝不允许任何一层静默丢字；这是为了验证折行之后的拼接等式，并确认比较器不会把一次正确的折行判成丢失"
	offGridWrapTok = "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789"
)

// offGridCandidateCols is the fixture grid's width — the width the band wraps at
// AND the width the candidate reports, one value so the fixture cannot wrap at a
// width its own grid does not claim.
const offGridCandidateCols = 117

// offGridWrappedLine is 200+ runes and wider than the 117-cell fixture grid in
// BOTH cells and runes, so the case cannot be explained away as the wide-rune
// (cjk_wrap) class.
func offGridWrappedLine() string { return offGridWrapCJK + offGridWrapTok }

// offGridCandidate builds a candidate whose VISIBLE WINDOW and SAVED HISTORY are
// given separately — which is the whole point: the two lists are different
// observations of the same surface, and the fix is that the second is asked.
func offGridCandidate(window, history []string) Candidate {
	return Candidate{
		Path:    "/fixture/chat-screen.dump",
		Surface: SurfaceChat,
		State:   StateExited,
		Frames:  7,
		Record:  DumpRecord{Fields: map[string]string{dumpRecordGeometryField: string(GeometrySourceLayout)}},
		Dump: lineedit.ScreenDump{
			Cols: offGridCandidateCols, Rows: len(window) + 1, Lines: window, Scrollback: history,
		},
	}
}

// bandRows wraps line the way the chat band draws it: the answer gutter, then the
// text cut at the grid width on WORD boundaries (lineedit.WrapToCols). It is the
// fixture's rendering of what the terminal really produced, and it is taken from
// the production wrapper rather than re-implemented — a hand-cut wrap would test
// a wrap the band never draws.
func bandRows(t *testing.T, line string, cols int) []string {
	t.Helper()
	pieces := lineedit.WrapToCols(line, cols)
	if len(pieces) < 2 {
		t.Fatalf("the fixture line %q did not wrap at %d columns (%d piece(s)): the case would not exercise a wrap",
			line, cols, len(pieces))
	}
	rows := make([]string, 0, len(pieces))
	for i, p := range pieces {
		if i == 0 {
			rows = append(rows, lineedit.AnswerGutters()[0]+p)
			continue
		}
		rows = append(rows, p)
	}
	return rows
}

// ① THE WRAP IS NOT A LOSS, at 200+ characters.
//
// The grid drew the line across several rows, the transcript has it once, and the
// join instrument reassembles it exactly — so the verdict is a MATCH, no loss class
// is carried, and the verdict says the text arrived. This is the arm the epic's
// first deliverable asks for ("折行行 rc=0 的可运行证据"); the exit CODE for it is
// asserted through the verb in cmd/agent, and this arm asserts the verdict the exit
// code is derived from.
func TestCompareTranscriptLine_WrappedWideLineIsAMatch(t *testing.T) {
	line := offGridWrappedLine()
	if n := len([]rune(line)); n < 200 {
		t.Fatalf("the fixture line is %d runes: the epic's case is a 200+ character line", n)
	}
	window := bandRows(t, line, offGridCandidateCols)
	for _, row := range window {
		if strings.Contains(row, line) {
			t.Fatalf("a single fixture row carries the whole line, so the case does not exercise the wrap: %q", row)
		}
	}
	cand := offGridCandidate(window, nil)

	v := CompareTranscriptLine(line, cand, []string{line})
	if v.Status != CompareMatch {
		t.Fatalf("status = %q, want %q (%s): the grid painted this line whole across %d rows",
			v.Status, CompareMatch, v.Reason, len(window))
	}
	if !v.InGrid || !v.Rejoined {
		t.Fatalf("in_grid=%t rejoined=%t, want both true — the arrival came from the join, and the verdict must say so",
			v.InGrid, v.Rejoined)
	}
	if want := len([]rune(line)); v.GridPrefixRunes != want {
		t.Fatalf("grid_prefix_runes = %d, want %d (the line's own rune length — a FIRST-ROW length here is the bug that sent a reader to a truncation that never happened)",
			v.GridPrefixRunes, want)
	}
	if v.Damage != DamageNone {
		t.Fatalf("damage = %q, want none: an arrival carries no loss class", v.Damage)
	}
	for _, loss := range []AuditDamage{DamagePaintLoss, DamageWrapArtifactOrLoss, DamageCJKWrap} {
		if strings.Contains(v.Reason, string(loss)) {
			t.Fatalf("the reason carries the loss class %q for a line the grid painted whole: %s", loss, v.Reason)
		}
	}
}

// ② OFF THE WINDOW IS NOT A DISAGREEMENT — the third answer.
//
// The line is NOT on the visible grid (the terminal pushed its rows into the saved
// history), the transcript holds it, and the SAME join instrument reassembles it
// from the history. The verdict is CompareOffGrid: the paint is not accused of
// losing anything, and the check does not claim an agreement it did not verify.
//
// The reason's FIRST WORD is the contract — that is what a caller greps instead of
// branching on an English sentence, and before this fix the only machine-readable
// discriminator was an exit code that said "disagreed" for both facts.
func TestCompareTranscriptLine_LineInSavedHistoryIsOffGridNotMismatch(t *testing.T) {
	line := "这一行早先画在屏上，之后被终端推进了保存的历史里，视口里已经看不见它了"
	history := append([]string{"older row"}, line)
	// The window is a LATER frame of the same surface: the line is simply not in
	// it — that is what "off the window" means, and a fixture that left the line in
	// the window would be testing the match path instead.
	window := []string{"a later row of the same screen", "status: idle"}
	cand := offGridCandidate(window, history)

	v := CompareTranscriptLine(line, cand, []string{line})
	if v.Status != CompareOffGrid {
		t.Fatalf("status = %q, want %q (%s): this line is in the artifact's saved history and not in its visible window",
			v.Status, CompareOffGrid, v.Reason)
	}
	if v.Status == CompareMismatch {
		t.Fatalf("the line was reported as a DISAGREEMENT while the history holds it: this is the measured defect")
	}
	if !v.OffGrid {
		t.Fatalf("off_grid = false on an off-grid verdict")
	}
	if want := 1; v.ScrollbackRow != want {
		t.Fatalf("scrollback_row = %d, want %d (the row the line was reassembled from)", v.ScrollbackRow, want)
	}
	if v.Damage != DamageNone {
		t.Fatalf("damage = %q, want none: nothing was lost", v.Damage)
	}
	if !strings.HasPrefix(v.Reason, CompareReasonSideOffGrid) {
		t.Fatalf("reason = %q, want it to OPEN with %q — the first word is the contract a caller branches on",
			v.Reason, CompareReasonSideOffGrid)
	}
}

// ③ A LINE THE PAINT REALLY LOST IS STILL REPORTED, AND THE REASON NAMES THE SIDE.
//
// This is the ANTI-WIDENING control, and it is why the off-grid answer is a
// MEASUREMENT of the saved history rather than a blanket excuse: a line the history
// does NOT hold stays a disagreement. Its reason opens with `transcript-side`,
// which says the authority has the line and the paint does not — the finding a
// reader acts on, without parsing the rest of the sentence.
func TestCompareTranscriptLine_RealLossStaysMismatchAndNamesTheSide(t *testing.T) {
	got := "屏幕上一层丢了这一行的后半段，历史里也没有它"
	cand := offGridCandidate([]string{"unrelated shell output"}, []string{"another old row"})

	v := CompareTranscriptLine(got, cand, []string{got})
	if v.Status != CompareMismatch {
		t.Fatalf("status = %q, want %q: neither the window nor the saved history carries this line",
			v.Status, CompareMismatch)
	}
	if v.OffGrid {
		t.Fatalf("off_grid = true for a line the saved history does not hold")
	}
	if !strings.HasPrefix(v.Reason, CompareReasonSideTranscript) {
		t.Fatalf("reason = %q, want it to OPEN with %q — the loss is on the paint side, and the authority has the line",
			v.Reason, CompareReasonSideTranscript)
	}
	if v.Damage.DamageArrived() {
		t.Fatalf("damage = %q is an ARRIVAL class for a line neither list carries", v.Damage)
	}
}

// ④ THE HISTORY NEVER OUTRANKS THE WINDOW: a line that IS on the visible grid is a
// match even when the saved history also holds it (a re-paint of the same text).
// Without this the off-grid probe could turn a verified arrival into an
// "undecidable" one, which would be the same class of bug in the other direction.
func TestCompareTranscriptLine_VisibleGridOutranksSavedHistory(t *testing.T) {
	line := "这一行同时在视口与历史里"
	window := []string{line, "status: idle"}
	cand := offGridCandidate(window, []string{line})

	v := CompareTranscriptLine(line, cand, []string{line})
	if v.Status != CompareMatch {
		t.Fatalf("status = %q, want %q: the line is ON the visible grid, so nothing is undecidable about it",
			v.Status, CompareMatch)
	}
	if v.OffGrid {
		t.Fatalf("off_grid = true for a line the visible grid carries")
	}
}

// ⑤ THE WHOLE-ANSWER LEDGER SAYS THE SAME THING ABOUT THE SAME LINES.
//
// This is the arm that keeps the TWO faces from drifting apart — the defect family
// this whole file's subject belongs to (task 01a0e4f5 was one face saying "paint
// loss" while the other counted the same line arrived). The audit's status must not
// claim `agreed` over a line the visible grid never showed: it says `off-grid`, and
// its counters close arithmetically (audited = matched + disagreed + off_grid) so a
// line nobody could see never inflates `matched`.
func TestAuditAnswer_OffWindowLinesAreCountedNotMatched(t *testing.T) {
	onScreen := "这一行在视口里"
	scrolledOff := "这一行已经滚出视口，只在保存的历史里"
	// The window is a LATER frame: one line still visible, the other already history.
	cand := offGridCandidate([]string{onScreen}, []string{scrolledOff})

	led := AuditAnswer(cand, []string{scrolledOff, onScreen})
	if led.Status != AuditOffGrid {
		t.Fatalf("status = %q, want %q: one audited line was not in the visible window, so the audit cannot claim agreement",
			led.Status, AuditOffGrid)
	}
	if led.OffGrid != 1 {
		t.Fatalf("off_grid = %d, want 1", led.OffGrid)
	}
	if led.LostText != 0 || led.Disagreed != 0 {
		t.Fatalf("lost_text=%d disagreed=%d, want 0 and 0: nothing was lost and nothing contradicts", led.LostText, led.Disagreed)
	}
	if want := led.Audited - led.Disagreed - led.OffGrid; led.Matched != want {
		t.Fatalf("matched = %d, want %d (audited %d − disagreed %d − off_grid %d): a line nobody could see must not count as matched",
			led.Matched, want, led.Audited, led.Disagreed, led.OffGrid)
	}
}

// ⑥ THE LEDGER'S THIRD STATE DOES NOT SWALLOW A LOSS: with a genuinely lost line
// present, `disagreed` still wins — an off-window line is an observation limit, and
// a real loss outranks it.
func TestAuditAnswer_LostTextOutranksOffGrid(t *testing.T) {
	lost := "日志里有，屏幕的任何一层都没有"
	cand := offGridCandidate([]string{"unrelated"}, []string{"another old row"})
	led := AuditAnswer(cand, []string{lost})
	if led.Status != AuditDisagreed {
		t.Fatalf("status = %q, want %q: a lost line decides the status", led.Status, AuditDisagreed)
	}
	if led.OffGrid != 0 {
		t.Fatalf("off_grid = %d, want 0", led.OffGrid)
	}
}
