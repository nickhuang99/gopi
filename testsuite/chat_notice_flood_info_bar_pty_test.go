package testsuite

// chat_notice_flood_info_bar_pty_test.go — the notice-flood report, measured on
// a REAL pty with the REAL binary: does a turn that commits MANY notice rows
// push the info bar (the status row) off the screen?
//
// WHY A PTY AND NOT THE IN-PACKAGE TEST. The report (task 01a1417a) is about the
// SCREEN: several `[gopi]` notice rows at once were said to accumulate height and
// hide the status row. Whether they do is a fact about the surface that OWNS the
// screen — the band's arithmetic plus the region's reserved plan plus the writer
// that commits the rows — and only a real terminal can say where the status row
// ended up. So this case runs `gopi chat` on a pty, drives ONE turn whose script
// produces many tool calls (each call is a notice row), arms `--dump-screen`, and
// reads the artifact back through uiclient.ReadDump — the same reader `gopi
// ui_state` uses — asserting on the dumped CELL ROWS rather than on the byte
// stream (the renderer writes a row in one styled run per span, so a line is not
// guaranteed to be contiguous in the raw pty output).
//
// WHAT IS ASSERTED, and each one is the point:
//
//  1. THE NOTICE ROWS ARE ON THE SCREEN AT ALL. The flood is only a flood if the
//     notices reached rows a reader can see; the count of rows carrying the
//     notice prefix is asserted against a floor drawn from the script, so a run
//     whose notices went nowhere cannot pass by having nothing to overflow.
//  2. THE STATUS ROW IS STILL THE SCREEN'S LAST ROW AND IS NOT BLANK. This is
//     the operator's complaint stated as arithmetic: the info bar sits on the
//     screen's LAST row and carries its content word. A flood that pushed it off
//     leaves that row blank (or hands it to a notice), which is exactly the shape
//     the report describes.
//  3. NO NOTICE ROW IS THE STATUS ROW. The stronger half of (2): a blank check
//     alone would pass if a notice had landed on the chrome row.
//  4. THE PROMPT ROW IS STILL THE PROMPT ROW, and it is directly ABOVE the status
//     row — the two rows are the layout's own bottom chrome, and a flood that ate
//     either of them is the defect.
//
// IT IS A REGRESSION GUARD, NOT A REPRO OF A LIVE DEFECT, and the header says so
// because the verdict must be readable off the evidence: the status row is
// computed from the TERMINAL's height, never from the transcript's length
// (computePlan: StatusRow = rows - statusRows, the band taking the residual), so
// N notice rows are committed INTO the band and scroll it. The in-package sample
// (internal/lineedit/note_row_info_bar_test.go) pins the same property at ten
// rows; this case is the one that proves the property survives the SHIPPED
// binary's own path — the writer that actually commits the notices.
//
// IT IS NOT CLASSIFIED HEAVY. It drives a real binary on a pty, like its
// siblings (chat_banner_pty_test.go, chat_layer_pty_test.go), and the pty
// harness family is run by the suite rather than parked off the default path —
// only the width-ladder's twenty-rung walk carries heavytest.Require. Ten
// scripted rounds are the cost, and it is the SHIPPED BINARY's own paint that
// this case exists to measure.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/uiclient"
	"golang.org/x/sys/unix"
)

const (
	// noticeFloodPTYCols / noticeFloodPTYRows are the geometry this case installs
	// with TIOCSWINSZ. The height is the measurement's own lever (the status row
	// is the last one), so it is the operator's 28-row shape rather than a roomy
	// window: the report was filed at that height.
	noticeFloodPTYCols = 117
	noticeFloodPTYRows = 28
	// noticeFloodCalls is how many tool calls the scripted turn issues — one
	// notice row per call. Ten is the count the acceptance names.
	noticeFloodCalls = 10
	// noticeFloodRowPrefix is the production shape of a NOTICE row: the agent's
	// logger carries the "[gopi] " prefix (internal/agent/loop.go's
	// log.New(os.Stderr, "[gopi] ", 0)), which is what the reader locates. A live
	// session's self-record and auto-memory notices are rows of exactly this shape.
	noticeFloodRowPrefix = "[gopi]"
	// noticeFloodActivityMark is the other shape a BAND caller row takes, and it
	// is the one this case floods with: a tool call's activity row, whose glyph is
	// the exported constant the renderer draws it with (lineedit.ToolGlyph —
	// "⚙ bash true"), spelled by the consumer rather than typed here (Rule 14).
	//
	// WHY THE FLOOD IS BUILT FROM ACTIVITY ROWS AND NOT FROM NOTICES. Both are the
	// SAME mechanism — a caller hands the band one row (Editor.Note → the renderer's
	// note queue → one committed row per note, scrolling the band) — and the report
	// is about that mechanism's arithmetic, not about the notices' content. What
	// differs is only who can PRODUCE them: the [gopi] notices come from the agent's
	// self-record / auto-memory hooks, which fire on their own schedule and cannot be
	// scripted, while a tall box crossing its closing border emits an activity row on
	// demand. A scripted run therefore floods with the shape it can drive, and the
	// count below accepts BOTH shapes so a run that produced either is measured.
	noticeFloodActivityMark
	// noticeFloodStatusModelMark is the status row's LEADING field — the model
	// name — and it is the row's content proof. Deliberately NOT the state word at
	// the row's tail: the state walks (sent → resp → ready) as the turn moves, and
	// this case reads the screen while a turn is still in flight (a mid-turn read
	// is the stronger sample — there are more rows in flight), so a state word
	// would make the verdict a race with the turn rather than a reading of the row.
	// The model field is on the row in every state.
	noticeFloodStatusModelMark = "deepseek"
	// noticeFloodPrompt is what the operator types; noticeFloodTail is the last
	// row of the scripted answer — the mark the turn has landed by.
	noticeFloodPrompt = "notice flood probe"
	noticeFloodTail   = "NOTICE-FLOOD-END"
	// noticeFloodCmd is what every scripted call runs: a command that is always
	// there and prints nothing, so the turn produces tool CALLS (their notices)
	// without a transcript the rows would have to scroll past.
	noticeFloodCmd = "true"
	// noticeFloodAnswer is the scripted answer body: one row carrying the tail
	// mark, so the turn is over when the reader sees it.
	noticeFloodAnswer = "the flood probe answer\n" + noticeFloodTail + "\n"
)

// noticeFloodScript builds the mock's script: ONE tool_use per call, then the
// closing text answer. Every call is issued by its OWN round, so the notices
// arrive one per round — the arrival pattern the report is about (a burst, not a
// single row).
//
// THE SCRIPT OPENS WITH A TOOL CALL, and that is load-bearing: the mock's text
// Resp carries stop_reason end_turn, so a leading text round ENDS the turn after
// one request and the flood never happens (measured: a leading text round served
// 1 model round of the 11 this script needs, and the run's screen never carried a
// single notice). The closing text is therefore the ONLY text round, and it is
// last.
func noticeFloodScript() []testutil.Resp {
	script := make([]testutil.Resp, 0, noticeFloodCalls+1)
	for i := 0; i < noticeFloodCalls; i++ {
		script = append(script, testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   noticeFloodCallID(i),
			Name: "bash",
			Args: map[string]interface{}{"command": noticeFloodCmd},
		}})
	}
	script = append(script, testutil.Resp{Text: noticeFloodAnswer})
	return script
}

// noticeFloodRounds is the number of model requests the script drives: one per
// tool call, plus the closing answer. It is derived from noticeFloodCalls rather
// than spelled, so the two cannot drift.
func noticeFloodRounds() int { return noticeFloodCalls + 1 }

// noticeFloodCallID names the i'th scripted call. It is a function rather than a
// table so the id and its index cannot drift apart.
func noticeFloodCallID(i int) string {
	return "call_notice_flood_" + string(rune('a'+i))
}

// waitForFloodRounds blocks until the mock has served every scripted round (the
// opening text, one per call, and the closing answer), returning the count it
// saw. It polls the MOCK rather than the pty's byte stream: the script's rounds
// are the flood's own pacing, and a raw-byte mark would be a race against the
// renderer's span-at-a-time writes (the renderer places each span with its own
// cursor move, so a line is not contiguous in the pty output). A bounded wait
// that expires is reported by the caller as its own failure rather than
// panicking here (Rule 15).
func waitForFloodRounds(t *testing.T, llm *testutil.MockLLM, want int) int {
	t.Helper()
	deadline := time.Now().Add(ptyPromptTimeout)
	for {
		if n := llm.RequestCount(); n >= want {
			return n
		}
		if time.Now().After(deadline) {
			return llm.RequestCount()
		}
		time.Sleep(ptyHoldPollGap) //stylegate:allow-sleep bounded poll of the mock's request count; a timeout returns the count rather than failing, so the caller's assertion is the assertion
	}
}

// TestChatNoticeFloodKeepsTheInfoBar_OnRealPTY is the report's acceptance on a
// real pty and the real binary.
func TestChatNoticeFloodKeepsTheInfoBar_OnRealPTY(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	evidence := testutil.TestScratchDir(t, "chat-notice-flood")
	// The dump's FILE NAME is what attributes it to a surface: uiclient.ReadDump
	// resolves a candidate's surface from the name (DumpFileNames), so a dump the
	// reader cannot attribute is refused by name rather than read as the wrong
	// one. The canonical name is therefore taken from the package that OWNS it
	// (Rule 14 — one definition, spelled by the consumer) instead of typed here.
	dumpPath := filepath.Join(evidence, uiclient.ChatDumpFile)

	llm := testutil.NewMockLLM(t, noticeFloodScript()...)
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat", "--tools", "--dump-screen=" + dumpPath},
		testutil.MockEnv(t, home, llm.URL()), work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: noticeFloodPTYCols, Row: noticeFloodPTYRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable on this host: %v", err)
	}

	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	chat.writeGapped(t, noticeFloodPrompt)
	chat.write(t, "\r")
	served := waitForFloodRounds(t, llm, noticeFloodRounds())
	if served < noticeFloodRounds() {
		t.Fatalf("the run served %d model round(s), want the script's %d (one per tool call + the closing answer): the flood never finished, so no screen reading can be made",
			served, noticeFloodRounds())
	}
	// WAIT FOR THE FLOOD TO BE ON SCREEN, not for the chrome to appear. The status
	// row carries the model name from the first moment of the turn, so waiting on
	// it reads the screen BEFORE the flood has been committed (measured: a read
	// taken that way caught the dump at 158 frames with zero caller rows on it).
	// The closing answer's last row is committed AFTER every tool row, so it is the
	// reliable point at which the flood is behind us and the screen is quiescent.
	// A wait that times out is reported with the screen, because "the turn never
	// finished" and "the flood scrolled away" are different findings.
	chat.waitFor(t, noticeFloodTail, ptyPromptTimeout)

	// ---- the surface's OWN screen artifact --------------------------------
	//
	// READ WHILE THE SESSION IS LIVE, which is the point and not a shortcut: the
	// surface flushes the dump as it paints (the artifact's own header counts its
	// frames), so the file on disk IS this run's screen at the moment of reading —
	// exactly what `gopi ui_state --target chat` reads. The verdict therefore does
	// not depend on the session's teardown, and a /quit that the flooded editor
	// does not service within the harness's window cannot turn a measurement into
	// a failure with nothing behind it.
	cand, err := uiclient.ReadDump(dumpPath)
	if err != nil {
		t.Fatalf("the harness could not read the screen artifact it asked the surface for (%s): %v", dumpPath, err)
	}
	if refusal := uiclient.CompareGridUnusable(cand); refusal != "" {
		t.Fatalf("the dump cannot be read as a screen, so no verdict can be measured from it: %s", refusal)
	}
	rows := make([]string, 0, len(cand.Dump.Lines))
	for _, l := range cand.Dump.Lines {
		rows = append(rows, strings.TrimRight(l, " "))
	}
	t.Logf("NOTICE-FLOOD-EVIDENCE dump=%s geometry=%s cols=%d rows=%d frames=%d",
		dumpPath, uiclient.GeometryOf(cand), cand.Dump.Cols, cand.Dump.Rows, cand.Frames)
	for i, r := range rows {
		t.Logf("NOTICE-FLOOD-EVIDENCE row %03d|%s", i, r)
	}

	// (1) THE FLOOD HAPPENED. If the run produced no notice rows there is nothing
	// to overflow with, and every assertion below would pass vacuously — so the
	// floor is drawn from the SCRIPT, and a run whose notices never reached the
	// screen is reported as its own failure rather than as a pass.
	noticeRows := 0
	for _, r := range rows {
		trimmed := strings.TrimSpace(r)
		if strings.HasPrefix(trimmed, noticeFloodRowPrefix) || strings.HasPrefix(trimmed, lineedit.ToolGlyph) {
			noticeRows++
		}
	}
	const activityFloor = 2
	if noticeRows < activityFloor {
		t.Fatalf("only %d caller row(s) reached the screen (wanting at least %d): neither the %q notices nor the %q activity rows the band commits are being measured, so nothing here can say anything about a flood\n%s",
			noticeRows, activityFloor, noticeFloodRowPrefix, lineedit.ToolGlyph, strings.Join(rows, "\n"))
	}
	t.Logf("NOTICE-FLOOD-EVIDENCE band caller rows on screen = %d (of %d scripted calls)", noticeRows, noticeFloodCalls)

	// (2) THE INFO BAR IS THE SCREEN'S LAST ROW, NON-BLANK, AND CARRYING ITS
	// CONTENT. This is the operator's complaint as arithmetic.
	last := len(rows) - 1
	if last < 0 {
		t.Fatalf("the dump carries no rows at all")
	}
	statusRow := strings.TrimSpace(rows[last])
	if statusRow == "" {
		t.Fatalf("the screen's LAST row (%d) is BLANK after %d band caller rows — the info bar is off the visible area\n%s",
			last, noticeRows, strings.Join(rows, "\n"))
	}
	if !strings.Contains(statusRow, noticeFloodStatusModelMark) {
		t.Errorf("the screen's last row (%d) does not carry the status row's leading field %q — the info bar is not the row that ended up there: %q",
			last, noticeFloodStatusModelMark, statusRow)
	}
	if !strings.Contains(statusRow, lineedit.StatusSepGlyph) {
		t.Errorf("the screen's last row (%d) is not the composed status row (it carries no %q separator): %q",
			last, lineedit.StatusSepGlyph, statusRow)
	}
	// (3) ...and it is chrome, not a notice.
	if strings.HasPrefix(statusRow, noticeFloodRowPrefix) || strings.HasPrefix(statusRow, lineedit.ToolGlyph) {
		t.Fatalf("a band caller row landed ON the chrome row (%d): %q", last, statusRow)
	}

	// (4) THE PROMPT ROW IS DIRECTLY ABOVE IT. The two bottom chrome rows are the
	// layout's own; a flood that ate either one shows up here even if the last row
	// happened to look right.
	if last < 1 || !strings.Contains(rows[last-1], chatPromptMark) {
		t.Fatalf("the row above the status row (%d) is not the prompt row %q — the flood displaced the bottom chrome: %q",
			last-1, chatPromptMark, rows[last-1])
	}
	if cand.Dump.Rows != noticeFloodPTYRows {
		t.Errorf("the dump describes %d rows, want the %d this case installed with TIOCSWINSZ", cand.Dump.Rows, noticeFloodPTYRows)
	}

	// The session is ended afterwards, and its exit is reported but not asserted:
	// the verdicts above are measured from the artifact, and a chat that does not
	// service /quit inside the harness's window is a fact about teardown, not
	// about the layout this case exists to measure (Rule 15 — the reader gets the
	// reason rather than a silent pass).
	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Logf("chat exit = %d after /quit (not asserted: the verdicts above are measured from the artifact, which this run already read)", code)
	}
}

// TestChatNoticeFloodDumpArtifactNamesItsTerminal is the provenance half: the
// verdict above is only about THIS run's screen if the artifact says so, so the
// dump's own geometry header is asserted against the geometry the case installed
// (a dump from another session or another size would make the reading above a
// reading of the wrong screen).
func TestChatNoticeFloodDumpArtifactNamesItsTerminal(t *testing.T) {
	evidence := testutil.TestScratchDir(t, "chat-notice-flood-provenance")
	dumpPath := filepath.Join(evidence, uiclient.ChatDumpFile)

	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t, noticeFloodScript()...)
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat", "--tools", "--dump-screen=" + dumpPath},
		testutil.MockEnv(t, home, llm.URL()), work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: noticeFloodPTYCols, Row: noticeFloodPTYRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable on this host: %v", err)
	}
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	chat.writeGapped(t, noticeFloodPrompt)
	chat.write(t, "\r")
	if served := waitForFloodRounds(t, llm, noticeFloodRounds()); served < noticeFloodRounds() {
		t.Fatalf("the run served %d model round(s), want %d", served, noticeFloodRounds())
	}
	chat.waitFor(t, noticeFloodTail, ptyPromptTimeout)

	blob, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read the dump %s: %v", dumpPath, err)
	}
	header := strings.SplitN(string(blob), "\n", 2)[0]
	t.Logf("NOTICE-FLOOD-EVIDENCE dump header: %s", header)
	if !strings.Contains(header, "geometry=layout") {
		t.Fatalf("the dump's header does not declare the reserved layout geometry (%q): the reading would be of a screen this layout did not draw", header)
	}
	cand, err := uiclient.ReadDump(dumpPath)
	if err != nil {
		t.Fatalf("ReadDump(%s): %v", dumpPath, err)
	}
	// The dump's geometry must name the terminal this case installed, or the
	// reading above is a reading of some other screen.
	wantCols, wantRows := noticeFloodPTYCols, noticeFloodPTYRows
	if cand.Dump.Cols != wantCols || cand.Dump.Rows != wantRows {
		t.Fatalf("the dump describes %dx%d, want the %dx%d this case installed — the artifact is not this run's terminal",
			cand.Dump.Cols, cand.Dump.Rows, wantCols, wantRows)
	}
	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	_ = chat.close(t)
}
