package main

// cli_uistate_compare_offgrid_test.go — the THIRD exit answer, through the verb.
//
// THE DEFECT (task 01a1c05c, measured on a real chat dump): `--compare` had two
// reachable codes for a line the transcript holds — 0 and 3 — and a line the
// surface PAINTED and the terminal later pushed off the top of the viewport was
// reported with the disagreement code (3). A caller branching on 3 concluded the
// paint had lost it, from a question the observation cannot answer: the artifact
// itself carried 2,518 rows of saved history beside its 22-row window.
//
// WHAT IS ASSERTED HERE, through the VERB (never by re-deriving a verdict here):
//
//	① a line the surface really painted and the band pushed into the SAVED
//	   HISTORY exits with uiStateExitOffGrid — not 0, not 3 — and prints the
//	   outcome word `off-grid` on the header line, so a reader (or a script) gets
//	   the fact without parsing prose;
//	② the reason's FIRST WORD is `off-grid`, which is the machine-readable
//	   discriminator that replaces "read the English sentence";
//	③ the same fixture's LOSING line still exits 3 with a reason whose first word
//	   names the side — so the new answer did not become a blanket excuse.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// offGridScrollRow is handed to the band FIRST, so the band's own scroll has
// pushed it off the window by the time the fixture closes — which is where an
// off-grid answer's evidence lives (the artifact's saved history).
const offGridScrollRow = "离线前的这一行已经被终端推进了保存的历史里，视口里看不见了"

// offGridVisibleRow is handed LAST and is the band's live row.
const offGridVisibleRow = "这是最后一行，仍然留在视口里"

// offGridFillRows is how many short filler rows go between them. The band keeps
// every row it is given until it runs out of rows, and only then does the top row
// leave the window — so a fixture of two rows never scrolls at all (measured: both
// rows came back on the visible grid) and a case built on it would pass or fail for
// a reason unrelated to the off-grid answer.
const offGridFillRows = 40

// offGridPaintedRows is the fixture's paint order: the scroll row first, enough
// filler to overflow the band, the visible row last.
func offGridPaintedRows() []string {
	rows := []string{offGridScrollRow}
	for i := 0; i < offGridFillRows; i++ {
		rows = append(rows, fmt.Sprintf("filler row %02d", i))
	}
	return append(rows, offGridVisibleRow)
}

// offGridFixture paints rows through the PRODUCTION band on a REAL pty (the same
// harness auditFixture establishes: production editor, production sink, geometry
// asserted against the pty's own answer) and returns the dump path and the session
// log path.
//
// WHY NOT auditFixture: its own premise guard requires EVERY painted row back on
// the grid, which is exactly the state this case needs to break — the first row must
// have scrolled off. The guard is right for its own cases and wrong for this one, so
// this fixture keeps the same construction and asserts the OTHER premise instead: the
// LAST painted row is on the grid (the band really painted), and the first one is not
// (the band really scrolled). Reading it back through the same reader the verb uses is
// what makes that a measurement rather than a claim.
func offGridFixture(t *testing.T, painted []string, transcript []string) (dumpPath, sessionPath string) {
	t.Helper()
	gopiDir, _ := uiStateFixtureProfile(t)
	sessions := filepath.Join(gopiDir, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir the fixture session dir: %v", err)
	}
	sessionPath = filepath.Join(sessions, compareSessionID, compareSessionFileName)
	if err := os.MkdirAll(sessionlayout.RecordContainer(sessionPath), 0o755); err != nil {
		t.Fatalf("mkdir the fixture session: %v", err)
	}
	writeCompareSessionLog(t, sessionPath, transcript)

	_, slave := dumpGeomPty(t)
	if got := lineedit.TerminalCols(int(slave.Fd())); got != dumpGeomPtyCols {
		t.Fatalf("the pty reports %d columns, want %d: the case is unset rather than green", got, dumpGeomPtyCols)
	}
	dir := filepath.Join(sessionlayout.RecordContainer(sessionPath), compareDumpDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir the fixture dump dir: %v", err)
	}
	dumpPath = filepath.Join(dir, uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: dumpPath,
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              os.Getpid(),
			Target:           chatDumpTarget(false),
			TerminalColsRows: [2]int{dumpGeomPtyCols, dumpGeomPtyRows},
			Started:          uiDumpStartStamp(),
		},
	})
	if err != nil {
		t.Fatalf("arm the fixture sink: %v", err)
	}
	term, err := lineedit.Open(slave, d)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	defer func() { _ = term.Close() }() //stylegate:ignore the harness's own terminal teardown
	ed := lineedit.NewChatEditor(term, d, chatPrompt, nil, os.Stderr, NewSlashCompleter())
	defer ed.Close()
	ed.SetGeometryObserver(chatGeomObserver(d))
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%dx%d) = false: the fixture needs the reserved layout installed", dumpGeomPtyCols, dumpGeomPtyRows)
	}
	for _, row := range painted {
		if !ed.WriteBandRow(row) {
			t.Fatalf("the band refused the row %q: no reserved layout is active", row)
		}
	}
	if !ed.SettleBand() {
		t.Fatalf("the band did not settle the painted rows: the layout does not own the screen")
	}
	if err := d.Close(); err != nil {
		t.Fatalf("finalize the fixture dump: %v", err)
	}
	return dumpPath, sessionPath
}

// TestUIStateCompare_OffTheWindowExitsFourNotThree is ① and ②.
//
// RED BEFORE THE FIX: the check had no off-grid answer at all, so this line fell
// through to the loss classes and printed `outcome=disagreed ... damage=paint_loss`
// with exit 3 — the code that means the two sides contradict each other.
func TestUIStateCompare_OffTheWindowExitsFourNotThree(t *testing.T) {
	painted := offGridPaintedRows()
	dump, session := offGridFixture(t, painted, painted)

	// THE FIXTURE'S OWN PREMISE, read back through the reader the verb uses: the
	// line is NOT on the visible window, and IS in the saved history. A fixture
	// whose band happened to keep the row would make this case pass for the wrong
	// reason (a window match), so the premise is asserted rather than assumed.
	cand, err := uiclient.ReadDump(dump)
	if err != nil {
		t.Fatalf("the fixture dump does not read back: %v", err)
	}
	if lineInRows(cand.Dump.Lines, offGridScrollRow) {
		t.Fatalf("the fixture still shows %q on the visible window: the case does not exercise an off-window line\nwindow:\n%s",
			offGridScrollRow, strings.Join(cand.Dump.Lines, "\n"))
	}
	if !lineInRows(cand.Dump.Scrollback, offGridScrollRow) {
		t.Fatalf("the fixture's saved history does not hold %q, so there is no off-grid evidence to find\nhistory (%d rows):\n%s",
			offGridScrollRow, len(cand.Dump.Scrollback), strings.Join(cand.Dump.Scrollback, "\n"))
	}

	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, offGridScrollRow,
		uiStateFlagSession, session,
		uiStateFlagTarget, string(uiclient.SurfaceChat),
	})
	if code == uiStateExitCompareMismatch {
		t.Fatalf("exit = %d (the DISAGREEMENT code) for a line the saved history holds: a script branching on it concludes the paint lost the line — the measured defect\noutput:\n%s",
			code, out)
	}
	if code != uiStateExitOffGrid {
		t.Fatalf("exit = %d, want %d (off the observed window)\noutput:\n%s", code, uiStateExitOffGrid, out)
	}
	if code == 0 {
		t.Fatalf("exit = 0 for a line the VISIBLE window does not contain — nothing about the current frame was verified:\n%s", out)
	}
	header := compareHeaderLine(t, out, "#gopi-ui-state-compare")
	if got := compareHeaderField(t, header, "outcome"); got != uiStateAuditWordOffGrid {
		t.Fatalf("outcome=%q, want %q — the word is the contract\nheader:\n%s", got, uiStateAuditWordOffGrid, header)
	}
	// ② THE FIRST WORD OF THE REASON is the machine-readable discriminator.
	if !strings.Contains(out, string(uiclient.CompareOffGrid)) {
		t.Fatalf("the verdict line does not carry the %q status:\n%s", uiclient.CompareOffGrid, out)
	}
	if !strings.Contains(out, uiclient.CompareReasonSideOffGrid) {
		t.Fatalf("the reason does not open with %q, so a caller must read the sentence to tell this from a loss:\n%s",
			uiclient.CompareReasonSideOffGrid, out)
	}
}

// TestUIStateCompare_LostLineStillNamesTheSide is ③: the anti-widening control at
// the verb. The new answer must not swallow a real loss.
//
// THE FIXTURE IS THE REAL-LOSS SHAPE, not a typo: the band is handed only the visible
// row, and the LOG holds one more line the paint never reached — so the authority has
// it and no layer of the screen does. That is a genuine paint loss, it still exits 3,
// and its reason OPENS with `transcript-side`: the side the finding is attributed to,
// available without reading the sentence.
func TestUIStateCompare_LostLineStillNamesTheSide(t *testing.T) {
	lostLine := "这条日志里有、而屏幕的任何一层都没有画过的行"
	// The log carries BOTH lines; the band is handed only the visible one. auditFixture
	// asserts every PAINTED row reached the grid, which holds here — the loss is on the
	// transcript side, so it is exactly the row it was never given.
	dump, session := offGridFixture(t, []string{offGridVisibleRow}, []string{offGridVisibleRow, lostLine})

	// The premise, read back through the reader the verb uses: the line is in the
	// transcript and on neither list of the screen. Without this the case could pass
	// because the band happened to paint it.
	cand, err := uiclient.ReadDump(dump)
	if err != nil {
		t.Fatalf("the fixture dump does not read back: %v", err)
	}
	if lineInRows(cand.Dump.Lines, lostLine) || lineInRows(cand.Dump.Scrollback, lostLine) {
		t.Fatalf("the fixture painted the line it means to lose: the case proves nothing\nwindow:\n%s",
			strings.Join(cand.Dump.Lines, "\n"))
	}

	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, lostLine,
		uiStateFlagSession, session,
		uiStateFlagTarget, string(uiclient.SurfaceChat),
	})
	if code != uiStateExitCompareMismatch {
		t.Fatalf("exit = %d, want %d for a line the log holds and the paint never reached\noutput:\n%s",
			code, uiStateExitCompareMismatch, out)
	}
	if strings.Contains(out, uiclient.CompareReasonSideOffGrid) {
		t.Fatalf("a lost line was reported off-grid — the new answer became a blanket excuse:\n%s", out)
	}
	if !strings.Contains(out, uiclient.CompareReasonSideTranscript) {
		t.Fatalf("the reason does not name the side (%q), so a caller must read the sentence to learn where the loss is:\n%s",
			uiclient.CompareReasonSideTranscript, out)
	}
	if !strings.Contains(out, "in_transcript=true") || !strings.Contains(out, "in_grid=false") {
		t.Fatalf("the verdict does not report both sides of the disagreement:\n%s", out)
	}
}

// lineInRows reports whether any row of the list carries text verbatim.
func lineInRows(rows []string, text string) bool {
	for _, r := range rows {
		if strings.Contains(r, text) {
			return true
		}
	}
	return false
}

// offGridWideLine is the epic's own width requirement — 200+ characters — with CJK
// and a long unbroken ASCII token, so the band's wrap lands MID-WORD.
const (
	offGridWideCJK = "自观测闭环：一条比窗口更宽的答案行必须逐字符还原，包括超长词与中文标点，绝不允许任何一层静默丢字；这段文字是为了让真实样本超过两百个字符，并确认比较器在折行、超长词与中日韩标点混排的条件下仍然能判出正确的到达，不允许截断"
	offGridWideTok = "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789"
)

// TestUIStateCompare_WideWrappedLineExitsZero is the epic's first deliverable at the
// VERB: a 200+ character line the band painted verbatim across a wrap comes back as
// an ARRIVAL — exit 0, outcome=agreed, match=true — and carries no loss class.
//
// RED BEFORE THE FIX (task 01a0e4f5's measurement, reproduced here at 200+ runes):
// the row-exact rule cannot see a wrap, so the line fell through to the loss classes
// and printed `outcome=disagreed ... prefix=<first row>` with exit 3, beside a
// whole-answer audit that counted the same line arrived.
func TestUIStateCompare_WideWrappedLineExitsZero(t *testing.T) {
	line := offGridWideCJK + offGridWideTok
	if n := len([]rune(line)); n < 200 {
		t.Fatalf("the fixture line is %d runes: the epic's case is a 200+ character line", n)
	}
	dump, session := offGridFixture(t, []string{line}, []string{line})

	// The premise, read back through the reader the verb uses: the line IS wider than
	// the grid and NO single row carries it — otherwise the case would pass on the
	// row-exact rule and prove nothing about the wrap join.
	cand, err := uiclient.ReadDump(dump)
	if err != nil {
		t.Fatalf("the fixture dump does not read back: %v", err)
	}
	if w := lineedit.DisplayWidth(line); w <= cand.Dump.Cols {
		t.Fatalf("the fixture line is %d cells at a %d-cell grid: it cannot wrap", w, cand.Dump.Cols)
	}
	for _, row := range cand.Dump.Lines {
		if strings.Contains(row, line) {
			t.Fatalf("a single grid row carries the whole line, so the case does not exercise the wrap: %q", row)
		}
	}

	out, code := uiStateRun(t, []string{
		uiStateFlagCompare, line,
		uiStateFlagSession, session,
		uiStateFlagTarget, string(uiclient.SurfaceChat),
	})
	if code != uiStateExitAgreed {
		t.Fatalf("exit = %d, want %d: the grid painted this line verbatim across a wrap\noutput:\n%s",
			code, uiStateExitAgreed, out)
	}
	for _, want := range []string{
		"outcome=" + uiStateAuditWordAgreed,
		"match=true",
		string(uiclient.CompareMatch),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the verdict does not carry %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, string(uiclient.DamagePaintLoss)) || strings.Contains(out, string(uiclient.DamageWrapArtifactOrLoss)) {
		t.Errorf("the verdict carries a loss class for a line the grid painted whole:\n%s", out)
	}
}
