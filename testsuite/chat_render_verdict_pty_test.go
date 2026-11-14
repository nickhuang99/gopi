package testsuite

// chat_render_verdict_pty_test.go — the T1/T2 rendering family, MEASURED on a
// real pty.
//
// WHAT PROBLEM THIS SOLVES. The handoff for this family
// (devdoc/handoff-20260927-chat-render-and-infobar-defects.md, §0) says it in
// its own words: "The suite was green over every defect in this doc. They were
// found only by reading the live painted screen." Every existing test for T1/T2
// drives AnswerRenderer into a buffer — the instrument that was green over the
// defects — so a fix for one of them could be claimed, merged and marked
// complete with nothing in the tree able to say whether the SCREEN changed.
// This file is that instrument's replacement: the REAL `gopi chat` binary, on a
// REAL pty, at a NARROW geometry, driven by a scripted turn that produces each
// known trigger shape, with the `--dump-screen` artifact read back and one
// verdict per shape printed together with the build identity the verdict was
// measured against.
//
// IT MEASURES. IT DOES NOT FIX. A failing shape is reported (FAIL) with its raw
// evidence and the completed task/commit that claimed it fixed; repairing it is
// a different task with its own report. It carries no assertion that a shape
// PASSES either — on a branch whose job is verification, asserting a verdict
// would make the harness abort before printing the table, which is the whole
// deliverable. The assertions it DOES carry are about the harness's own
// sanity (the reader resolved the dump, the grid was painted, the shapes reached
// the screen), because a verdict table computed from an unread artifact is
// exactly the green-that-means-nothing this task exists to end.
//
// INVOCATION (the repo-resident, re-runnable entry point):
//
//	CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
//	go test ./testsuite/ -run TestChatRenderVerdicts_OnRealPTY -v -count=1
//
// The binary is built from THIS worktree and its identity is measured — never
// assumed (see chatRenderBuildIdentity).

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sgr"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/uiclient"
	"golang.org/x/sys/unix"
)

// The harness's own bounds. Every value is named (Rule 1); none is a threshold
// a verdict depends on (the verdicts are derived from the artifact, not from
// these numbers — they only decide how much screen the script is allowed to
// spend).
const (
	// chatRenderCols is the pty's width. NARROW on purpose: T1/T2 are the
	// wrap/scroll family, and a wrap is a property of the geometry (the
	// operator's own sighting was on a 117-column terminal; the handoff's
	// baseline row is 95 cells). 64 columns wraps both the CJK answer and the
	// long bash command several times per paragraph.
	chatRenderCols = 64
	// chatRenderRows is the pty's HEIGHT, and it is chosen for the oracle's
	// sake rather than for realism: the dump is a window the size of the grid,
	// so the harness keeps every measured row inside it (see the Shapes table —
	// the whole script is sized to fit). A 26-row window is the operator's own
	// geometry; this run asks for 34 so the report can also show the rows the
	// band itself scrolls.
	chatRenderRows = 34
	// chatRenderShapesRow is where the scripted answer prints its MARKERS: the
	// first row of the screen is the banner, and the markers are what lets the
	// verdicts find the shapes' rows instead of guessing them from coordinates.
	chatRenderShapesRow = 5
	// chatRenderTranscriptRetry bounds the wait for the session record to reach
	// the disk (the child writes it as it goes; the dump is frozen at teardown,
	// and the transcript is read after the process is gone).
	chatRenderTranscriptRetry = 60
)

// chatRenderShape is ONE known defect shape of the family, with the trigger the
// scripted turn produces for it.
type chatRenderShape struct {
	// ID is the short name this verdict is reported under (it is what a filed
	// task quotes).
	ID string
	// ClaimedFix names the completed task and the commit that claimed to fix
	// this shape — the fact a non-PASS verdict is a finding ABOUT.
	ClaimedFix string
	// Trigger is what the scripted turn does to produce the shape.
	Trigger string
	// Pass is the pass criterion in words, printed beside every verdict so a
	// reader can disagree with it on the evidence rather than on trust.
	Pass string
	// Marker is the line the scripted answer prints to locate this shape's rows
	// on the grid ("" = the shape has no marker and is read from the pty).
	Marker string
}

// chatRenderShapes is the measured family. T1 and T2 are the handoff's own two
// rows; NOTE-WRAP is the third sighting from the same screen ("`⚙ call` rows
// splitting an identifier mid-token AND truncating it"), which no unit test in
// the tree covers and which is the oldest note-path property in the file.
var chatRenderShapes = []chatRenderShape{
	{
		ID:         "T1-dup",
		ClaimedFix: "task 01a0e2f5-bc4f-7355-bf37-52ba32727b8d (T1, live-band duplication); fix 305a108b 'the band's carry reconciles one in-flight line's two spellings'",
		Trigger:    "a CJK answer paragraph that wraps: the band duplicates a wrapping/scrolling answer (partial repeat, truncated tail)",
		Pass:       "the answer's own text arrives exactly once, in transcript order, with no row carrying a repeated fragment (the transcript holds it once)",
		Marker:     chatRenderT1Open,
	},
	{
		ID:         "T2-wrap",
		ClaimedFix: "task 01a0e2f3-91b2-75b6-95ad-2204bccddeb5 (T2, wrap/emphasis commit); fixes 305a108b + b7d3f86a 'decide a wrapping line before its rows are committed' + 469adb94 'the wrap path promoted a whole row blank'",
		Trigger:    "a wrapping line with bold **…** and inline-code `…` spans straddling the wrap, one span broken mid-word",
		Pass:       "no literal ** and no stray backtick on any grid row; the text arrives with the markup removed; the styled run is not split by the wrap into a half-styled word",
		Marker:     chatRenderT2Open,
	},
	{
		ID:         "NOTE-WRAP",
		ClaimedFix: "commit 1bfd07eb 'a note row wraps at a boundary and marks its continuation' (the note path: writeNote/cutNoteRow, internal/lineedit/answer.go), superseded for the glyph half by e58009f65 'a note row's mid-token cut is lossless and draws no marker' (task 01a12669)",
		Trigger:    "a real tool call whose digest is a long bash command line with a >40-cell word in the middle (bash -c 'printf …')",
		Pass:       "the ⚙ call row wraps losslessly — reassembled, the note's rows are the command, and every continuation row carries the continuation gutter; NO row draws the cut glyph `…`, because the note path's cut is a plain break (its remainder opens the next row), so a glyph in the command's own cells is a character the reader pastes into a shell",
		Marker:     "",
	},
}

// Verdict vocabularies. Deliberately three-valued: "the artifact cannot say" is
// a fact of its own and collapsing it into either neighbour is how a
// measurement that never happened gets reported as one (Rule 15).
const (
	verdictPass              = "PASS"
	verdictFail              = "FAIL"
	verdictCouldNotDetermine = "COULD NOT DETERMINE"
)

// chatRenderVerdict is one row of the delivered table.
type chatRenderVerdict struct {
	Shape    chatRenderShape
	Verdict  string
	Evidence []string
	Reason   string
}

// TestChatRenderVerdicts_OnRealPTY is the deliverable. It prints, in order: the
// build identity the verdicts were measured against, the raw evidence, and one
// verdict per known shape.
func TestChatRenderVerdicts_OnRealPTY(t *testing.T) {
	bin := testutil.GopiBin(t)
	identity := chatRenderBuildIdentity(t, bin)
	t.Logf("RENDER-VERDICT harness invoked with GOPI_BIN=%s", bin)

	home := t.TempDir()
	work := t.TempDir()

	// The evidence directory: the artifacts this run leaves for a reader — the
	// dump, the transcript beside it, the pty's byte stream and the verb's own
	// read of the screen. It is a SCRATCH directory (off the `/` partition and
	// outside the worktree, so the deliverable stays committed files only —
	// Rules 22, 30), never a task directory: a repo-resident test that wrote to
	// one task's session dir would fail (or litter) for the next runner, which
	// is why an earlier draft of this file was changed here.
	evidence := testutil.TestScratchDir(t, "chat-render-verdict")
	dumpPath := filepath.Join(evidence, chatRenderDumpName)
	// The reader helper below is package-level (it is shared with the verifier),
	// so the path it reads is handed over the same way this file hands over the
	// comparator's reading under test.
	chatRenderDumpRoot = evidence
	t.Cleanup(func() { chatRenderDumpRoot = "" })

	// ---- the scripted turn -------------------------------------------------
	// TWO model rounds, and the shape of the turn is the harness's own lesson
	// (measured 2026-09-28, first run of this file): a mock text round ends the
	// turn (`stop_reason=end_turn`), so a third scripted round only arrives on
	// the NEXT user message. The whole measurement therefore lives in ONE turn:
	//   round 1 — the tool call whose digest is the long bash command (the
	//             NOTE-WRAP trigger; the call fires the OnToolEvents callback
	//             BEFORE execution, so the note row is painted while the tool
	//             actually runs);
	//   round 2 — the whole answer, T1/CJK through T2/mixed spans, ENDING with
	//             the tail marker so the harness can wait for the last row
	//             instead of guessing when the paint finished.
	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_render_verdict_01",
			Name: "bash",
			Args: map[string]interface{}{"command": chatRenderBashCommand},
		}},
		testutil.Resp{Text: chatRenderAnswer},
	)
	defer llm.Close()

	chat := startPTYChat(t, []string{"chat", "--tools", "--dump-screen=" + dumpPath},
		testutil.MockEnv(t, home, llm.URL()), work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: chatRenderCols, Row: chatRenderRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable on this host: %v", err)
	}
	if cols := lineedit.TerminalCols(int(chat.master.Fd())); cols != chatRenderCols {
		t.Fatalf("the pty reports %d columns, want the %d this harness installed", cols, chatRenderCols)
	}

	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)
	chat.writeGapped(t, chatRenderPrompt)
	chat.write(t, "\r")

	// WAIT FOR THE LAST ROW, THEN FOR THE TURN TO END. The dump is a SAMPLE
	// (100ms throttle) and the pty carries every frame, so waiting on the first
	// fragment would quit mid-answer and then report the band as having lost
	// the rest. The answer's tail marker is its last line; the SECOND wait is
	// for the prompt to be re-rendered, which is the moment the turn is over and
	// its rows are COMMITTED (the family is about committed rows — a provisional
	// paint may legitimately be re-drawn). No sleep is involved: both waits
	// return the instant their marker arrives.
	ptyOut := chat.waitFor(t, chatRenderTail, ptyPromptTimeout)
	ptyOut += chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	code := chat.close(t)
	if code != 0 {
		t.Fatalf("chat exit = %d, want 0 (the deliberate /quit); pty output:\n%s", code, ptyOut)
	}

	// ---- the raw evidence --------------------------------------------------
	dumpPath, dumpText := chatRenderDumpArtifact(t)
	cand, err := uiclient.ReadDump(dumpPath)
	if err != nil {
		t.Fatalf("the harness could not read the artifact it asked the surface for (%s): %v", dumpPath, err)
	}
	if refusal := uiclient.CompareGridUnusable(cand); refusal != "" {
		t.Fatalf("the dump cannot be read as a screen, so no verdict can be measured from it: %s", refusal)
	}
	grid := cand.Dump.Grid
	for i, row := range cand.Dump.Lines {
		t.Logf("dump row %03d|%s", i, row)
	}
	t.Logf("dump provenance: %s", cand.Record.Line)
	t.Logf("dump geometry=%s cols=%d rows=%d frames=%d dumps=%d attrs_recorded=%v", uiclient.GeometryOf(cand), cand.Dump.Cols, cand.Dump.Rows, cand.Frames, cand.Dumps, cand.Dump.AttrsRecorded)
	if len(grid) != len(cand.Dump.Lines) {
		t.Fatalf("the artifact's two renderings disagree about the row count (grid %d, lines %d): a verdict read from one of them would describe a different screen", len(grid), len(cand.Dump.Lines))
	}
	t.Logf("transcript-candidate path=%s", filepath.Join(home, ".gopi", "sessions"))
	if first := strings.SplitN(dumpText, "\n", 2); len(first) > 0 {
		t.Logf("dump file first line: %s", first[0])
	}

	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)

	// The third channel: what the pty actually emitted. Kept in the evidence
	// directory (never in the worktree — Rule 30) and quoted in the report.
	ptyPath := filepath.Join(evidence, chatRenderPTYLogName)
	if err := os.WriteFile(ptyPath, []byte(ptyOut), dumpFilePerm); err != nil {
		t.Fatalf("write the pty transcript evidence: %v", err)
	}
	t.Logf("pty transcript (third channel) written to %s (%d bytes)", ptyPath, len(ptyOut))

	// Keep the artifacts a reader (and the ui_state verb) needs after the
	// suite's own temporary home is gone: the dump (the surface wrote it to the
	// same directory), the transcript beside it, and the pty's byte stream.
	if err := os.WriteFile(filepath.Join(evidence, chatRenderSessionName),
		[]byte(chatRenderSessionText(t, sess)), dumpFilePerm); err != nil {
		t.Fatalf("keep the session transcript beside the dump: %v", err)
	}
	t.Logf("transcript kept beside the dump at %s", filepath.Join(evidence, chatRenderSessionName))

	// The verb's OWN read of the same artifact, so the report carries
	// ui_state's words and not only this harness's interpretation. The --all
	// ledger is asked too: its matched/disagreed counters beside its own list
	// are the oracle's self-check, and on this screen they contradict each
	// other (matched=6, two mismatches listed) — quoted verbatim, and kept in
	// the evidence directory so the reading is never a transcription.
	uiOut, uiErr, uiCode := testutil.RunGopi(t,
		[]string{"ui_state", "--target", "chat", "--json"},
		"", testutil.MockEnv(t, home, llm.URL()), work)
	t.Logf("ui_state --target chat --json exit=%d stderr=%s", uiCode, strings.TrimSpace(uiErr))
	t.Logf("ui_state stdout (first %d bytes):\n%s", chatRenderUIStateQuoteBytes, firstBytes(uiOut, chatRenderUIStateQuoteBytes))
	if err := os.WriteFile(filepath.Join(evidence, chatRenderUIStateName), []byte(uiOut), dumpFilePerm); err != nil {
		t.Fatalf("write the ui_state evidence: %v", err)
	}
	allOut, allErr, allCode := testutil.RunGopi(t,
		[]string{"ui_state", "--target", "chat", "--all", "--json",
			"--session", filepath.Join(evidence, chatRenderSessionName)},
		"", testutil.MockEnv(t, home, llm.URL()), work)
	t.Logf("ui_state --all exit=%d stderr=%s stdout_bytes=%d", allCode, strings.TrimSpace(allErr), len(allOut))
	t.Logf("ui_state --all stdout (first %d bytes): %s", chatRenderUIStateQuoteBytes, firstBytes(allOut, chatRenderUIStateQuoteBytes))
	t.Logf("ui_state --all ledger, verbatim (basis of filed oracle defect 01a0e4f5-f9cc): %s", chatRenderLedgerLine(allOut, allCode))
	if err := os.WriteFile(filepath.Join(evidence, chatRenderUIStateAllName), []byte(allOut), dumpFilePerm); err != nil {
		t.Fatalf("write the ui_state --all evidence: %v", err)
	}

	// ---- the verdicts ------------------------------------------------------
	t.Logf("RENDER-VERDICT build identity: %s", identity)
	t.Logf("RENDER-VERDICT geometry: cols=%d rows=%d (pty %dx%d)", cand.Dump.Cols, cand.Dump.Rows, chatRenderCols, chatRenderRows)
	verdicts := chatRenderVerdicts(cand, sess, ptyOut)
	for _, v := range verdicts {
		t.Logf("RENDER-VERDICT %s: %s — %s", v.Shape.ID, v.Verdict, v.Reason)
		t.Logf("RENDER-VERDICT %s pass criterion: %s", v.Shape.ID, v.Shape.Pass)
		t.Logf("RENDER-VERDICT %s trigger: %s", v.Shape.ID, v.Shape.Trigger)
		for _, e := range v.Evidence {
			t.Logf("RENDER-VERDICT %s evidence: %s", v.Shape.ID, e)
		}
	}
	t.Logf("RENDER-VERDICT table:")
	for _, v := range verdicts {
		t.Logf("  | %-9s | %-19s | claimed by %s |", v.Shape.ID, v.Verdict, v.Shape.ClaimedFix)
	}
	t.Logf("RENDER-VERDICT harness cannot see: %s", strings.Join(chatRenderBlindSpots, " | "))
}

// TestChatRenderComparatorSanity guards the harness's OWN arithmetic, because
// the first run of this file proved it needs guarding: the join inserted a space
// at every wrap and reported two correctly painted screens as duplicated text.
// A comparator that can only say PASS measures nothing, so this test drives it
// with two canned screens — one as the band really draws a wrapped answer, one
// carrying the T1 duplication — and requires a PASS and a FAIL respectively. It
// touches no pty and no binary, so it runs everywhere.
func TestChatRenderComparatorSanity(t *testing.T) {
	// Rows as the dump renders them (gutter stripped by the harness): the band
	// cut mid-word here, which is what the band does.
	wrapped := func() uiclient.Candidate {
		return chatRenderCannedCandidate([]string{
			chatRenderT1Open,
			"这一段中文答案用来检查换行之后的列是否对齐，中间夹着 ASCII tok",
			"en 和 code 片段，然后再来一段中文以确保换行发生多次。自观测闭",
			"环的验证必须在同一个屏幕上完成，否则读者无法把两边的证据对齐。",
			chatRenderT1Close,
		})
	}
	if v, ev, why := chatRenderVerdictT1(wrapped()); v != verdictPass {
		t.Fatalf("a correctly wrapped answer was reported %s (%s)\nevidence:\n%s", v, why, strings.Join(ev, "\n"))
	}

	// The duplication: the last body row carries the tail of the line above it
	// again — the operator's own baseline shape.
	dup := chatRenderCannedCandidate([]string{
		chatRenderT1Open,
		"这一段中文答案用来检查换行之后的列是否对齐，中间夹着 ASCII tok",
		"环的验证必须在同一个屏幕上完成，否则读者无法把两边的证据对齐。",
		"环的验证必须在同一个屏幕上完成，否则读者无法把两边的证据对齐。",
		chatRenderT1Close,
	})
	if v, _, _ := chatRenderVerdictT1(dup); v != verdictFail {
		t.Fatalf("a duplicated row was reported %s — the comparator cannot see the defect it exists to measure", v)
	}

	// A shape whose marker never reached the screen is NOT a pass: an absent
	// trigger is a measurement that did not happen.
	absent := chatRenderCannedCandidate([]string{"nothing here", "and nothing here"})
	if v, _, _ := chatRenderVerdictT1(absent); v != verdictCouldNotDetermine {
		t.Fatalf("an absent trigger was reported %s, want %s", v, verdictCouldNotDetermine)
	}

	// THE EMPHASIS HALF, both ways. The same rows with the bold run's cells
	// stripped of their bold must FAIL — that is the operator's second damage
	// class, and a harness that reported it PASS would be reading the text and
	// calling it the emphasis.
	bold := chatRenderCannedCandidate(mixedRows)
	bold.Dump.AttrsRecorded = true
	bold.Dump.AttrRuns = cannedRuns(true)
	if v, _, why := chatRenderVerdictT2(bold); v != verdictPass {
		t.Fatalf("a correctly emphasised span was reported %s (%s)", v, why)
	}
	flat := chatRenderCannedCandidate(mixedRows)
	flat.Dump.AttrsRecorded = true
	flat.Dump.AttrRuns = cannedRuns(false)
	if v, _, _ := chatRenderVerdictT2(flat); v != verdictFail {
		t.Fatalf("a span whose cells are NOT bold was reported %s — the emphasis half is not being measured", v)
	}

	// THE NOTE HALF, both ways, on the contract the NOTE PATH was fixed to
	// (commit e58009f65, task 01a12669): a note row cut inside a token is
	// LOSSLESS and carries NO cut glyph, so the canned screens are
	//   (a) the lossless shape with the renderer's own continuation gutter → PASS;
	//   (b) the same shape with a `…` drawn in one of the command's OWN content
	//       cells → FAIL.
	//
	// THE ASSERTIONS ARE FLIPPED FROM THE VERSION THIS TEST SHIPPED WITH, and the
	// flip is the point rather than a tidy-up: the old canned pair drove the
	// verdict with a MID-TOKEN row that filled the band and no marker (→ FAIL) and
	// the same row carrying `lineedit.StatusEllipsis` (→ PASS) — the note path's
	// pre-01a12669 contract, under which the glyph was drawn INSIDE the command's
	// own cells and stood for nothing (the tail was on the next row all along). A
	// reader who selected those rows pasted `-ma…xdepth` into a shell. So the two
	// screens below are driven in the opposite direction on purpose: if the
	// second one is reported PASS the harness is still green over the defect the
	// previous contract called correct, and the test fails loudly.
	//
	// The note, cut the way the band cuts one: a head that did NOT fill the row
	// (so it ended its own text), a MIDDLE row filled to the band's edge and
	// therefore cut inside a token — where the note path takes a plain break and
	// marks nothing — and a tail that finishes the command.
	prefix := lineedit.ToolGlyph + " " + chatRenderNoteKind + " "
	cmd := "probe " + strings.Repeat("abcdefghij", 8)
	headText := cmd[:20]
	midText := cmd[20 : 20+chatRenderBandWidth()]
	tailText := cmd[20+chatRenderBandWidth():]
	head := prefix + headText
	lossless := chatRenderCannedCandidate([]string{head, lineedit.NoteContinuation + midText, lineedit.NoteContinuation + tailText})
	if v, ev, why := chatRenderVerdictNote(head, lossless, cmd); v != verdictPass {
		t.Fatalf("a lossless, unmarked mid-token cut was reported %s (%s) — the note half is measuring the OLD contract (a cut that draws no ellipsis is not a defect: the tail continues on the next row)\nevidence:\n%s", v, why, strings.Join(ev, "\n"))
	}
	// ONE cell of the command replaced by the cut glyph — the shape the fix
	// removed: `midText`'s last character is gone from the cells and the `…`
	// stands in its place, so a reader who selects the two rows copies a command
	// that does not run. It must be reported FAIL.
	marked := chatRenderCannedCandidate([]string{
		head,
		lineedit.NoteContinuation + strings.TrimSuffix(midText, midText[len(midText)-1:]) + lineedit.StatusEllipsis,
		lineedit.NoteContinuation + tailText,
	})
	if v, _, _ := chatRenderVerdictNote(head, marked, cmd); v != verdictFail {
		t.Fatalf("a mid-token cut carrying the cut glyph %q in the command's OWN cells was reported %s — the harness is green over the defect this contract exists to catch (a selected row copies the glyph into the command)",
			lineedit.StatusEllipsis, v)
	}
}

// mixedRows is one correctly wrapped T2 block, as the band draws it (gutter
// stripped) — the canned screen the emphasis red-proof is taken on.
var mixedRows = []string{
	chatRenderT2Open,
	"Live band probe: the row is partially emphasised and then cont",
	"inues into `rowForPartial` and finishes with plain text so the",
	"wrap lands inside the styled run.",
	chatRenderT2Close,
}

// cannedRuns builds attribute runs for mixedRows with (or without) bold on the
// cells the span occupies. The geometry mirrors the measured dump: the bold run
// starts 30 cells into the first body row and continues into the second.
func cannedRuns(withBold bool) [][]lineedit.DumpAttrRun {
	plain := lineedit.DumpAttrRun{Col: 0, Cells: chatRenderCols + 2, Style: "default"}
	if !withBold {
		runs := make([][]lineedit.DumpAttrRun, len(mixedRows))
		for i := range runs {
			runs[i] = []lineedit.DumpAttrRun{plain}
		}
		return runs
	}
	runs := make([][]lineedit.DumpAttrRun, len(mixedRows))
	for i := range runs {
		runs[i] = []lineedit.DumpAttrRun{plain}
	}
	bold := lineedit.DumpAttrRun{Col: 28, Cells: 34, Style: "bold"}
	runs[1] = []lineedit.DumpAttrRun{{Col: 0, Cells: 28, Style: "default"}, bold}
	runs[2] = []lineedit.DumpAttrRun{{Col: 0, Cells: 7, Style: "bold"}, {Col: 7, Cells: 60, Style: "default"}}
	return runs
}

// chatRenderCannedCandidate builds a Candidate whose Lines are the given rows,
// for the comparator sanity test (the only caller — a canned grid is not a
// measurement of anything).
func chatRenderCannedCandidate(lines []string) uiclient.Candidate {
	return uiclient.Candidate{Dump: lineedit.ScreenDump{
		Lines:   lines,
		Grid:    append([]string(nil), lines...),
		Cols:    chatRenderCols,
		Rows:    len(lines),
		Version: lineedit.DumpVersion,
	}}
}

// chatRenderVerdicts computes one verdict per shape from the artifact.
func chatRenderVerdicts(cand uiclient.Candidate, sess *testutil.Session, ptyOut string) []chatRenderVerdict {
	out := make([]chatRenderVerdict, 0, len(chatRenderShapes))
	for _, shape := range chatRenderShapes {
		v := chatRenderVerdict{Shape: shape}
		switch shape.ID {
		case "T1-dup":
			v.Verdict, v.Evidence, v.Reason = chatRenderVerdictT1(cand)
		case "T2-wrap":
			v.Verdict, v.Evidence, v.Reason = chatRenderVerdictT2(cand)
		case "NOTE-WRAP":
			v.Verdict, v.Evidence, v.Reason = chatRenderVerdictNote(ptyOut, cand, chatRenderBashCommand)
		default:
			v.Verdict = verdictCouldNotDetermine
			v.Reason = "the harness has no measurement for this shape id — a verdict it cannot compute is not a verdict"
		}
		out = append(out, v)
	}
	// The transcript channel is quoted for the first grid-measured shape: a
	// reader must be able to see the authority the rows are measured against
	// (the log holds the answer once).
	if len(sess.Events) > 0 {
		out[0].Evidence = append(out[0].Evidence, fmt.Sprintf("transcript event types: %v", sess.Types()))
	}
	return out
}

// chatRenderVerdictT1 measures the duplication shape.
//
// THE MEASUREMENT, and why it is not a string search for a literal. T1's
// signature is a row that carries a line and THEN carries a fragment of that
// line's head again (the band's carry/re-commit race), and the operator's
// baseline row is exactly that. So the test is: reassemble the answer's
// paragraph from the dump's rows and require the result to EQUAL the answer's
// text. A duplicated tail, a dropped tail or a reordered paragraph all break
// that equality, and all three are the family.
//
// TWO READINGS, ONE VERDICT — and the reader must not confuse them (measured:
// this harness's first draft did). `Grid` is cell-exact, one rune per CELL, so a
// wide rune leaves its continuation column holding a SPACE; joining Grid rows
// therefore inserts a phantom space per wide rune and would report a perfect
// screen as "fragment duplicated or dropped". `Lines` is the readable rendering
// with the continuation columns SKIPPED, which is what a human reads. So the
// comparison uses Lines (and strips the band's OWN gutter, which the transcript
// never carries), and the Grid's cell count is used only to prove the two
// renderings agree about the row count.
func chatRenderVerdictT1(cand uiclient.Candidate) (string, []string, string) {
	rows, ok := chatRenderRowsBetween(cand, chatRenderT1Open, chatRenderT1Close)
	if !ok {
		return verdictCouldNotDetermine, nil, "the T1 marker rows are not on the dump's grid: the shape was never painted on the artifact this run read (the answer may have scrolled out of the window, or the dump froze before the paint landed) — a verdict measured from an absent trigger is not a verdict"
	}
	evidence := make([]string, 0, len(rows)+3)
	for i, r := range rows {
		evidence = append(evidence, fmt.Sprintf("row %d: %q", i, r))
	}
	gridText := chatRenderJoinWrapped(rows)
	evidence = append(evidence, fmt.Sprintf("dump rows reassembled: %q", gridText))
	evidence = append(evidence, fmt.Sprintf("answer text:          %q", chatRenderCJKParagraph))
	// The comparison is WHITESPACE-INSENSITIVE, and that is a limit of the
	// instrument stated rather than a convenience: the CJK body's wide runes and
	// the wrap's boundaries make "where exactly the cells fell" a property of the
	// cutter, and what the equality proves is the T1 claim itself — the answer's
	// TEXT arrives once, in order, with nothing repeated, dropped or re-ordered.
	// A defect that only duplicated a SPACE cell would pass; both readings are
	// quoted above so a reader can see what is being compared.
	if chatRenderNormalize(gridText) != chatRenderNormalize(chatRenderCJKParagraph) {
		return verdictFail, evidence,
			"the band's rows do not reassemble to the answer's text: a fragment is duplicated, dropped or re-ordered — the T1 duplication signature"
	}
	return verdictPass, evidence, "the answer's text arrives exactly once across the wrapped rows; no row carries a repeated fragment"
}

// chatRenderVerdictT2 measures the wrap/emphasis shape.
//
// The claim has two halves and BOTH are measured: (a) the TEXT — no literal
// markup may reach the screen and the rows must reassemble to the answer with
// its markers removed; (b) the EMPHASIS — a bold span must be bold ON ITS OWN
// CELLS, including across the row boundary the wrap put inside it (the dump
// records SGR per cell, so this is a measured fact rather than an inference
// from the bytes).
func chatRenderVerdictT2(cand uiclient.Candidate) (string, []string, string) {
	rows, ok := chatRenderRowsBetween(cand, chatRenderT2Open, chatRenderT2Close)
	if !ok {
		return verdictCouldNotDetermine, nil, "the T2 marker rows are not on the dump's grid: the shape was never painted on the artifact this run read — a verdict measured from an absent trigger is not a verdict"
	}
	// The dump's OWN row numbers, so the emphasis half can read the same rows'
	// attribute runs. Rendering the gutters away must not lose the row index.
	first, _ := chatRenderMarkerRows(cand, chatRenderT2Open, chatRenderT2Close)
	evidence := make([]string, 0, len(rows)+5)
	for i, r := range rows {
		evidence = append(evidence, fmt.Sprintf("row %d: %q", i, r))
	}
	gridText := chatRenderJoinWrapped(rows)
	want := chatRenderMixedPlain()
	evidence = append(evidence, fmt.Sprintf("dump rows, joined                       : %q", gridText))
	evidence = append(evidence, fmt.Sprintf("answer text, by lineedit.DisplayText    : %q", want))
	// The comparison is WHITESPACE-INSENSITIVE, and that is a deliberate limit
	// of the instrument rather than a convenience: where an answer's line broke
	// is a property of the band's cutter, and a break that spends the space
	// between two words leaves the rows indistinguishable from a break that did
	// not. What the comparison therefore proves is exactly T1's claim — the
	// answer's CHARACTERS arrive once, in order — while a defect that only
	// duplicated a SPACE cell would pass. Both readings are quoted above so a
	// reader can see the difference rather than take it on trust.
	if chatRenderNormalize(gridText) != chatRenderNormalize(want) {
		return verdictFail, evidence, "the grid's text is not the answer's text (markers removed): a span was split, duplicated or lost at the wrap"
	}
	// The rows the markers BRACKET (the marker's own row is not the payload —
	// see chatRenderRowsBetween), whose attribute runs the emphasis half reads.
	em := chatRenderEmphasis(cand, first+1, len(rows))
	evidence = append(evidence, em.evidence...)
	if em.fail != "" {
		return verdictFail, evidence, em.fail
	}
	return verdictPass, evidence, "no literal markup on any row, the text reassembles to the answer, and the bold span is bold on every cell the wrap split it across"
}

// chatRenderVerdictNote measures the note-row (tool call) wrap shape.
//
// THE CRITERION, and why the earlier versions of it were wrong. The first version
// searched the pty's byte stream for the command's whole words and reported FAIL
// because the stream is not a screen (the words it looked for were split across
// rows there). The second took the measurement from the dump's own note rows but
// asked the PRE-01a12669 question — "does every mid-token cut carry the
// continuation ellipsis?" — so it reported the FIXED shape as a failure. The
// guarantee the note path actually carries (commit e58009f65, task 01a12669;
// internal/lineedit/note_row_wrap_test.go's own wording) is the opposite one, and
// this function now asks exactly that:
//
//		004|⚙ call bash bash -
//		005|↳ c 'echo 0123456789abcdef0123456789abcdef0123456789abcdef012345
//		006|↳ 6789abcdefghij'
//
//	 1. NOTHING IS LOST — the note's rows, reassembled, are the command, character
//	    for character (`…012345` + `6789…` = the token in full, measured above).
//	    The continue-rows' gutter (`↳ `) is the block's own column, not content, so
//	    it is stripped before the rebuild — the same thing a reader does when they
//	    select the rows.
//	 2. EVERY CONTINUATION IS MARKED — each row after the first opens with the
//	    continuation gutter, so a reader can tell a wrapped call from a second one.
//	 3. NO CUT GLYPH IS ON ANY ROW — the note path draws no `…` at all, and a row
//	    that carries one has put it INSIDE the command's own cells: select the
//	    rows, paste, and the shell gets `…` it cannot run (`-ma…xdepth` is the
//	    operator's own sighting). A glyph there is a defect, never a fix.
func chatRenderVerdictNote(ptyOut string, cand uiclient.Candidate, want string) (string, []string, string) {
	plain := chatRenderStripANSI(ptyOut)
	// The prefix includes the separator that follows the tool name: the row reads
	// `⚙ call bash <the call's argument>`, and the space between them belongs to
	// the label rather than to the command — leaving it out of the prefix would
	// put a phantom leading cell into the reassembly and report a correct screen
	// as a mismatch.
	prefix := lineedit.ToolGlyph + " " + chatRenderNoteKind + " "
	evidence := make([]string, 0, 7)
	if idx := strings.Index(plain, strings.TrimRight(prefix, " ")); idx >= 0 {
		evidence = append(evidence, fmt.Sprintf("the pty's byte stream carries %q at byte %d (the row was painted, not merely recorded)", strings.TrimRight(prefix, " "), idx))
	} else {
		return verdictCouldNotDetermine, nil, fmt.Sprintf("no %q row is in the pty stream: the tool call never painted its note row (was the turn tool-enabled? did the call reach OnToolEvents?) — a verdict measured from an absent trigger is not a verdict", strings.TrimRight(prefix, " "))
	}

	head, cont, ok := chatRenderNoteRows(cand)
	if !ok {
		return verdictCouldNotDetermine, evidence, fmt.Sprintf("no note row beginning %q is on the dump's grid: the shape was never painted on the artifact this run read", strings.TrimRight(prefix, " "))
	}
	evidence = append(evidence, fmt.Sprintf("dump note rows: head %q, continuations %v", head, cont))
	if len(cont) == 0 {
		return verdictCouldNotDetermine, evidence, "the note is on ONE dump row: the measured command did not wrap at this geometry, so no row boundary — and therefore no cut — was exercised. A verdict about cuts read off a screen that has none is not a verdict."
	}
	for i, row := range cont {
		if strings.HasPrefix(strings.TrimRight(row, " "), lineedit.NoteContinuation) {
			continue
		}
		return verdictFail, evidence, fmt.Sprintf("continuation row %d (%q) does not open with the continuation gutter %q: a wrapped call reads as a second one", i+1, row, lineedit.NoteContinuation)
	}
	evidence = append(evidence, fmt.Sprintf("every continuation row carries the gutter %q (the block's own column, stripped before the rebuild)", lineedit.NoteContinuation))

	rebuilt, marked := chatRenderNoteRebuild(prefix, head, cont)
	// The join's row boundaries: every continuation row is one, and each may have
	// consumed at most one space.
	byteExact, matched, skips := chatRenderNoteMatches(rebuilt, want, len(cont))
	evidence = append(evidence, fmt.Sprintf("note reassembled: %q", rebuilt))
	evidence = append(evidence, fmt.Sprintf("command:          %q", want))
	evidence = append(evidence, fmt.Sprintf("reassembled vs command: byte-exact=%v (spaces the row boundaries consumed: %d of at most %d)", byteExact, skips, len(cont)))
	if len(marked) > 0 {
		evidence = append(evidence, fmt.Sprintf("rows carrying the cut glyph %q in the command's own cells: %v", lineedit.StatusEllipsis, marked))
		return verdictFail, evidence, "a note row carries the cut glyph " + fmt.Sprintf("%q", lineedit.StatusEllipsis) + " inside the command's own cells: the tail continues on the next row, so the glyph stands for nothing and a reader who selects the rows copies it into a command no shell runs"
	}
	if !matched {
		return verdictFail, evidence, "the note's rows do not reassemble to the call's command: part of it was lost or repeated on the way to the screen"
	}
	if skips == 0 {
		return verdictPass, evidence, "the call's command reaches the screen in full — the rows joined, gutter stripped, are the command BYTE FOR BYTE — every continuation row carries the gutter, and no row draws a cut glyph"
	}
	return verdictPass, evidence, fmt.Sprintf("the call's command reaches the screen character for character (the %d space(s) a boundary break consumed are the join's only difference), every continuation row carries the gutter, and no row draws a cut glyph", skips)
}

// chatRenderNoteMatches compares a reassembled note against the call's command
// the way a paste does, and reports WHICH of the two comparisons it took: byte
// equality, or equality after the spaces that row boundaries consumed.
//
// The distinction is reported rather than smoothed away. A note whose wrap broke
// inside a token — the shape this family's cmd/agent fixture produces at a narrow
// geometry, and the shape the fix is about — reassembles BYTE FOR BYTE: nothing
// was spent at the break, which is exactly the property the operator's sighting
// asks for. A wrap that broke at a SPACE legitimately consumed it (the head ends
// the line there and the tail opens the next one), so at most one space per row
// boundary may be missing from the reassembly; the count is bounded by the number
// of boundaries, and each skip must be a space the command itself carries at
// exactly the join position — never a wildcard.
func chatRenderNoteMatches(rebuilt, want string, boundaries int) (byteExact, matched bool, skips int) {
	if rebuilt == want {
		return true, true, 0
	}
	i, j := 0, 0
	for i < len(rebuilt) && j < len(want) {
		if rebuilt[i] == want[j] {
			i++
			j++
			continue
		}
		if want[j] == ' ' && skips < boundaries && j+1 < len(want) && rebuilt[i] == want[j+1] {
			j++
			skips++
			continue
		}
		return false, false, skips
	}
	return false, i == len(rebuilt) && j == len(want), skips
}

// chatRenderNoteRows finds the note on the dump: the row carrying the tool glyph
// and the continuation rows that follow it (the note is over at the first row
// that is not a continuation).
func chatRenderNoteRows(cand uiclient.Candidate) (head string, cont []string, ok bool) {
	for i, row := range cand.Dump.Lines {
		if !strings.HasPrefix(row, lineedit.ToolGlyph) {
			continue
		}
		head = row
		for j := i + 1; j < len(cand.Dump.Lines); j++ {
			next := cand.Dump.Lines[j]
			if !strings.HasPrefix(next, lineedit.NoteContinuation) {
				break
			}
			cont = append(cont, next)
		}
		return head, cont, true
	}
	return "", nil, false
}

// chatRenderNoteRebuild reassembles a note's rows into the text they carry, and
// reports the rows that draw the cut glyph inside the command's own cells.
//
// THE DIRECTION OF THE SECOND RETURN IS THE NEW CONTRACT (commit e58009f65, task
// 01a12669). It used to be `unmarked` — the rows cut mid-token WITHOUT an
// ellipsis, i.e. the PRE-fix defect, and a shape the note path produces on every
// wrapped long token BY DESIGN. The defect now is a row that DRAWS the glyph: the
// tail continues on the next row, so `…` stands for nothing while occupying a
// cell of the command the reader copies, and selecting the rows and pasting them
// yields a command no shell runs (`-ma…xdepth`). The glyph is therefore not
// stripped-and-ignored any more: it is reported, and the caller fails on it —
// stripping it from the rebuild as well would hide the very cell the finding is
// about.
func chatRenderNoteRebuild(prefix, head string, cont []string) (rebuilt string, marked []string) {
	pieces := []string{strings.TrimPrefix(head, prefix)}
	for _, row := range cont {
		// The continuation gutter is the block's own COLUMN (documented in
		// NoteRowLegend), not content: it is stripped before the rebuild, exactly
		// as a reader's selection of the rows does, and it is not a cut glyph.
		pieces = append(pieces, strings.TrimPrefix(row, lineedit.NoteContinuation))
	}
	var b strings.Builder
	for _, piece := range pieces {
		text := strings.TrimRight(piece, " ")
		if strings.Contains(text, lineedit.StatusEllipsis) {
			marked = append(marked, fmt.Sprintf("%q", text))
		}
		b.WriteString(text)
	}
	return b.String(), marked
}

// chatRenderRowCutMidToken was DELETED here with the contract flip (commit
// e58009f65, task 01a12669): it asked "is this row's last cell inside a token?"
// in order to demand a cut glyph for that shape, so it encodes the PRE-fix
// ontology and has no consumer once the glyph became the defect. The note path
// takes a plain break there and the remainder opens the next row, so the
// question the harness asks is no longer "was this row cut?" but "does any row
// draw the glyph, and do the rows reassemble to the command?" — both answered in
// chatRenderNoteRebuild / chatRenderVerdictNote.

// chatRenderEmphasisResult is the emphasis half's outcome.
type chatRenderEmphasisResult struct {
	fail     string
	evidence []string
}

// chatRenderEmphasis reads the attribute runs of the rows a shape occupied and
// requires the answer's bold span to be BOLD on the cells that carry its text —
// including the cells on the far side of a wrap.
//
// It measures the span by its TEXT, not by a column, and it walks the ROWS
// because the wrap may put half of the span on the next one: for each row it
// takes the longest prefix of the span that still remains, finds where that
// prefix sits (the row already carries the band's gutter, so the display column
// comes from the text to its left), and asks the artifact for the style of that
// cell.
func chatRenderEmphasis(cand uiclient.Candidate, firstRow, rowCount int) chatRenderEmphasisResult {
	if !cand.Dump.AttrsRecorded || len(cand.Dump.AttrRuns) == 0 {
		return chatRenderEmphasisResult{
			fail:     "the artifact carries no per-cell attributes (attrs_recorded=false), so the emphasis claim cannot be measured from it",
			evidence: []string{"emphasis: attrs_recorded=false — the artifact cannot answer this half"},
		}
	}
	remaining := chatRenderBoldSpan(chatRenderMixedParagraph)
	if remaining == "" {
		return chatRenderEmphasisResult{
			fail:     "the harness could not locate the bold span in the answer's own text, so it cannot ask whether its cells are bold — the trigger and the criterion drifted apart",
			evidence: []string{"emphasis: no bold span in the answer's own text"},
		}
	}
	var evidence []string
	seen := 0
	for dr := firstRow; dr < firstRow+rowCount && dr < len(cand.Dump.AttrRuns) && remaining != ""; dr++ {
		row := cand.Dump.Lines[dr]
		head, col := chatRenderLongestPrefixAt(row, remaining)
		if head == "" {
			continue
		}
		style := chatRenderStyleAt(cand, dr, lineedit.DisplayWidth(row[:col]))
		evidence = append(evidence, fmt.Sprintf("emphasis: dump row %d col %d carries %q → style %q", dr, lineedit.DisplayWidth(row[:col]), head, style))
		seen++
		if !strings.Contains(style, chatRenderBoldStyle) {
			return chatRenderEmphasisResult{
				fail:     fmt.Sprintf("the bold span's text is on the screen but its cells are NOT bold (row %d, style %q): the wrap/commit broke the emphasis, which is the operator's second damage class", dr, style),
				evidence: evidence,
			}
		}
		remaining = remaining[len(head):]
	}
	if seen == 0 {
		return chatRenderEmphasisResult{
			fail:     "the bold span's cells could not be located in the rows the markers bracket, so the emphasis half is unmeasured",
			evidence: evidence,
		}
	}
	if remaining != "" {
		evidence = append(evidence, fmt.Sprintf("emphasis: the span is split across rows and the remainder %q was not located in any of them", remaining))
		return chatRenderEmphasisResult{
			fail:     "only part of the bold span's text could be located on the rows the markers bracket: the rest of it is not on the screen where the wrap should have put it",
			evidence: evidence,
		}
	}
	return chatRenderEmphasisResult{evidence: evidence}
}

// chatRenderLongestPrefixAt finds the longest prefix of want that occurs in row,
// and where it starts. It exists because a span that straddles a wrap is only
// PARTLY on the row: the row carries the span's head, the next row its tail.
func chatRenderLongestPrefixAt(row, want string) (prefix string, byteIdx int) {
	for n := len(want); n > 0; n-- {
		if idx := strings.Index(row, want[:n]); idx >= 0 {
			return want[:n], idx
		}
	}
	return "", -1
}

// chatRenderStyleAt returns the style string of the run covering a cell, or
// "?" when no run covers it (never silently "default": a missing run is a fact
// about the artifact, and reporting it as unstyled would invert the verdict).
func chatRenderStyleAt(cand uiclient.Candidate, row, col int) string {
	if row < 0 || row >= len(cand.Dump.AttrRuns) {
		return "?"
	}
	for _, run := range cand.Dump.AttrRuns[row] {
		if col >= run.Col && col < run.Col+run.Cells {
			return run.Style
		}
	}
	return "?"
}

// chatRenderBoldSpan returns the text of the answer's bold span, markers
// removed — read from the answer itself (not from the screen), so the harness
// asks about the text it scripted rather than about whatever got painted.
func chatRenderBoldSpan(plain string) string {
	open := strings.Index(plain, lineedit.BoldMarker)
	if open < 0 {
		return ""
	}
	rest := plain[open+len(lineedit.BoldMarker):]
	closeIdx := strings.Index(rest, lineedit.BoldMarker)
	if closeIdx < 0 {
		return ""
	}
	return rest[:closeIdx]
}

// chatRenderBoldHead is the part of the bold span that appears at the START of
// a row (the span continues from the row above it).
func chatRenderBoldHead(row string) string {
	span := chatRenderBoldSpan(chatRenderMixedParagraph)
	for n := len(span); n > 0; n-- {
		if strings.HasPrefix(row, span[:n]) {
			return span[:n]
		}
	}
	return ""
}

// chatRenderMarkerRows returns the dump rows that carry two markers (the second
// exclusive), so a caller can keep the dump's own row numbering.
func chatRenderMarkerRows(cand uiclient.Candidate, from, to string) (int, int) {
	start, end := -1, -1
	for i, row := range cand.Dump.Lines {
		if start < 0 && strings.Contains(row, from) {
			start = i
			continue
		}
		if start >= 0 && strings.Contains(row, to) {
			end = i
			break
		}
	}
	return start, end
}

// chatRenderRowsByPrefix returns the dump rows whose text begins with a glyph
// (the band's gutter stripped first).
func chatRenderRowsByPrefix(cand uiclient.Candidate, glyph string) []string {
	var out []string
	for _, row := range cand.Dump.Lines {
		if s := chatRenderStripGutter(row); strings.HasPrefix(s, glyph) {
			out = append(out, s)
		}
	}
	return out
}

// chatRenderRowsBetween returns the dump's rows STRICTLY between the one
// carrying `from` and the one carrying `to` — the payload rows, with the band's
// gutters stripped and the markers' own rows excluded. The markers are the
// harness's own handwriting: leaving them in the compared text would append
// "<T1-DUP" to the answer and report a correctly painted screen as a mismatch
// (the first run of this file did exactly that, which is why the comparator has a
// sanity test of its own).
func chatRenderRowsBetween(cand uiclient.Candidate, from, to string) ([]string, bool) {
	start, end := chatRenderMarkerRows(cand, from, to)
	if start < 0 {
		return nil, false
	}
	if end < 0 {
		end = len(cand.Dump.Lines)
	}
	if end <= start+1 {
		return nil, true
	}
	out := make([]string, 0, end-start-1)
	for _, row := range cand.Dump.Lines[start+1 : end] {
		out = append(out, chatRenderStripGutter(row))
	}
	return out, true
}

// chatRenderStripGutter removes the band's own marker columns from a dump row:
// the turn bullet, the continuation gutter, and the two-cell indent that opens
// every body row. The transcript never carries them, so leaving them in would
// report every line as a mismatch (the diagnosis inverted — the same trap
// internal/uiclient's compare gutters exist for).
func chatRenderStripGutter(row string) string {
	s := strings.TrimRight(row, " ")
	for {
		switch {
		case strings.HasPrefix(s, lineedit.TurnMarker):
			s = s[len(lineedit.TurnMarker):]
		case strings.HasPrefix(s, lineedit.TurnMarkerSplit):
			s = s[len(lineedit.TurnMarkerSplit):]
		case strings.HasPrefix(s, lineedit.NoteContinuation):
			s = s[len(lineedit.NoteContinuation):]
		default:
			return strings.TrimLeft(s, " ")
		}
	}
}

// chatRenderJoinWrapped reassembles the rows one wrapped line was drawn on: a
// row that FILLED the band is continued on the next row, so the join puts the
// next row's text directly against it; a row that did not fill the band ended
// its line, so the join breaks there.
//
// NO SEPARATOR IS INSERTED BETWEEN WRAPPED ROWS, and the first run of this
// harness is the measurement that settled it: the band cuts a long line mid-word
// (`…ASCII tok` / `en 和…`, `…then cont` / `inues into…`) without spending or
// losing a character, so inserting the space a wrap "consumed" GLUED an extra
// cell into every cut word and reported two perfectly painted screens as
// duplicated/dropped text. The comparison that follows is whitespace-insensitive
// for the mirror-image reason (a break that DID consume a space leaves the two
// sides differing only there), and the limit that admits is stated at each use.
func chatRenderJoinWrapped(rows []string) string {
	var b strings.Builder
	for i, row := range rows {
		if i > 0 && !chatRenderRowIsWrappedASCII(rows[i-1]) {
			b.WriteString(chatRenderJoinNewline)
		}
		b.WriteString(row)
	}
	return b.String()
}

// chatRenderRowIsWrappedASCII reports whether a row is FULL — the property that
// makes a mid-token end a cut rather than the end of the text.
func chatRenderRowIsWrappedASCII(row string) bool {
	return lineedit.DisplayWidth(strings.TrimRight(row, " ")) >= chatRenderBandWidth()
}

// chatRenderBandWidth is the width the band's rows are cut at: the grid's own
// width minus the column the band indents its text by. Both numbers come from
// the layout (the grid's width is measured; the indent is the turn marker's).
func chatRenderBandWidth() int { return chatRenderCols - lineedit.DisplayWidth(lineedit.TurnMarker) }

// chatRenderMixedPlain is the T2 answer's text as the SCREEN must show it: the
// markers removed by the composer's own tokenizer (lineedit.DisplayText), never
// by a substitution this harness invented — a harness that stripped markers with
// its own `strings.Replace` would pass a screen that left one behind.
func chatRenderMixedPlain() string { return lineedit.DisplayText(chatRenderMixedParagraph) }

// chatRenderNormalize compares text the way a screen can be compared: a wide
// rune's continuation column is not part of the text, and a wrap that spends the
// space between two words is a cell the renderer consumed rather than a
// character it lost. Both sides are therefore compared with all whitespace
// removed. The limit this admits is stated where it is used (see the T2 verdict).
func chatRenderNormalize(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// chatRenderJoinNewline is the separator the join uses between two rows that are
// NOT one wrapped line.
const chatRenderJoinNewline = "\n"

// chatRenderStripANSI removes CSI/OSC sequences so the pty stream can be
// searched as text.
func chatRenderStripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if n := sgr.EscapeSeqLen(s[i:]); n > 0 {
			i += n
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// chatRenderNoteWords is the set of whole words of the tool call's command,
// longest first — the units a mid-token cut or a truncation destroys. Any word
// longer than one band row is excluded (no rule can keep it whole, and its
// absence would be a false FAIL).
func chatRenderNoteWords(command string) []string {
	var out []string
	for _, w := range strings.Fields(command) {
		if lineedit.DisplayWidth(w) >= chatRenderBandWidth() {
			continue
		}
		out = append(out, w)
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// firstBytes trims a captured stream to a printable quote.
func firstBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Cut on a rune boundary: a report is read by people, and half a rune in a
	// quote reads as corruption in the artifact rather than in the quote.
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max] + "\n… [truncated by the harness's quote bound]"
}

// chatRenderBuildIdentity measures WHICH BINARY the verdicts describe.
//
// A verdict from an unknown build is not a verdict (the handoff's own words),
// so the identity is measured two ways and both are reported:
//
//  1. the artifact's own `go version -m` — the toolchain's stamp inside the
//     binary (absent for a linked-worktree build: the Go toolchain embeds no
//     VCS settings there, which `gopi version` already reports honestly);
//  2. the working tree the binary was built from (this worktree): HEAD, the
//     branch and whether the tree was dirty — the identity the run actually
//     controls, printed beside the artifact's own words so a reader can see
//     which of the two is doing the vouching.
func chatRenderBuildIdentity(t *testing.T, bin string) string {
	t.Helper()
	var parts []string
	if out, err := exec.Command("go", "version", "-m", bin).CombinedOutput(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "vcs.") || strings.HasPrefix(line, "mod\t") {
				parts = append(parts, "artifact: "+line)
			}
		}
	} else {
		parts = append(parts, fmt.Sprintf("artifact: go version -m failed: %v", err))
	}
	if out, err := exec.Command("git", "-C", chatRenderRepoRoot(t), "rev-parse", "HEAD").Output(); err == nil {
		parts = append(parts, "worktree HEAD: "+strings.TrimSpace(string(out)))
	} else {
		parts = append(parts, fmt.Sprintf("worktree HEAD: unreadable: %v", err))
	}
	if out, err := exec.Command("git", "-C", chatRenderRepoRoot(t), "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		parts = append(parts, "worktree branch: "+strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("git", "-C", chatRenderRepoRoot(t), "status", "--porcelain").Output(); err == nil {
		dirty := strings.TrimSpace(string(out))
		if dirty == "" {
			parts = append(parts, "worktree dirty: no")
			return strings.Join(parts, " · ")
		}
		parts = append(parts, "worktree dirty: "+strings.ReplaceAll(dirty, "\n", "; "))
		return strings.Join(parts, " · ")
	}
	// The git probes are the harness's own honesty, never its subject: a
	// failure to run git is REPORTED (Rule 15) and the run continues with the
	// identity it has, rather than aborting a measurement over metadata.
	parts = append(parts, "worktree dirty: unreadable")
	return strings.Join(parts, " · ")
}

// chatRenderRepoRoot is the worktree this run's binary was built from.
func chatRenderRepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Logf("git rev-parse --show-toplevel failed (%v): the build identity cannot name the worktree", err)
		return "."
	}
	return strings.TrimSpace(string(out))
}

// chatRenderDumpRoot is the evidence directory the RUNNING test handed to the
// package-level reader below. It is written by the test itself (the test is the
// only thing that knows its scratch dir) and cleared in t.Cleanup, so a reader
// that runs outside a test — or after it — fails loudly instead of reading a
// stale screen.
var chatRenderDumpRoot string

// chatRenderDumpArtifact reads the dump file the surface was asked to write,
// and insists that it exists: an artifact the harness cannot find is a
// measurement that did not happen, never an empty screen.
func chatRenderDumpArtifact(t *testing.T) (string, string) {
	t.Helper()
	path, err := chatRenderDumpFile()
	if err != nil {
		t.Fatalf("%v", err)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the dump %s: %v", path, err)
	}
	return path, string(blob)
}

// chatRenderDumpFile resolves the dump's path from the running test's evidence
// directory and refuses to guess: with no directory in force the harness has no
// artifact to describe, and a default path would make the reader describe a
// screen produced by some other run (Rule 15 — never a silent fallback).
func chatRenderDumpFile() (string, error) {
	if chatRenderDumpRoot == "" {
		return "", fmt.Errorf("no evidence directory is in force (chatRenderDumpRoot is empty): %s is set by TestChatRenderVerdicts_OnRealPTY for the duration of its run and cleared afterwards", chatRenderDumpName)
	}
	return filepath.Join(chatRenderDumpRoot, chatRenderDumpName), nil
}

// chatRenderLedgerLine picks the oracle's own ledger verdict out of `ui_state
// --all --json` and renders it as ONE line, so the run's log carries the tool's
// claim and not the harness's paraphrase of it. It reads the counters from the
// JSON rather than scraping the pretty form, and when the shape is not what it
// expects it says so — an absent ledger is a fact about the verb's answer here,
// never an empty ledger (Rule 15).
func chatRenderLedgerLine(out string, code int) string {
	var doc struct {
		Ledger *struct {
			Rows            int `json:"rows"`
			Matched         int `json:"matched"`
			Disagreed       int `json:"disagreed"`
			EmphasisChecked int `json:"emphasis_checked"`
			Lines           []struct {
				Verdict struct {
					Matched bool `json:"matched"`
				} `json:"verdict"`
			} `json:"lines"`
		} `json:"ledger"`
	}
	if err := json.Unmarshal([]byte(firstBytes(out, chatRenderUIStateQuoteBytes*8)), &doc); err != nil {
		return fmt.Sprintf("(the verb's output could not be parsed: %v) raw head: %s", err, firstBytes(out, chatRenderUIStateQuoteBytes))
	}
	if doc.Ledger == nil {
		return fmt.Sprintf("NO LEDGER in the verb's answer (exit=%d): the run inside this harness has no live screen of its own to audit — the ledger quoted in the report comes from the same verb against the session copy this run kept (--session <evidence>/%s)", code, chatRenderSessionName)
	}
	mismatches := 0
	for _, ln := range doc.Ledger.Lines {
		if !ln.Verdict.Matched {
			mismatches++
		}
	}
	return fmt.Sprintf("rows=%d matched=%d disagreed=%d emphasis_checked=%d mismatchEntriesListed=%d",
		doc.Ledger.Rows, doc.Ledger.Matched, doc.Ledger.Disagreed, doc.Ledger.EmphasisChecked, mismatches)
}

// chatRenderSessionName is what the transcript is called beside the dump: the
// session reader derives it from the dump's own session directory.
const chatRenderSessionName = "session.jsonl"

// chatRenderSessionText re-reads the session's JSONL from the suite's own home,
// so the copy that survives the test is the CHILD's record (the authority the
// dump is compared against), not a transcription of it.
func chatRenderSessionText(t *testing.T, sess *testutil.Session) string {
	t.Helper()
	blob, err := os.ReadFile(sess.Path)
	if err != nil {
		t.Fatalf("read the session record %s: %v", sess.Path, err)
	}
	return string(blob)
}

// The names of the non-branch evidence the harness leaves in its scratch
// directory (Rule 30: the worktree carries committed files only).
const (
	// chatRenderPTYLogName is the pty's byte stream as captured by the session.
	chatRenderPTYLogName = "render-verdict-pty.log"
	// chatRenderUIStateName is `gopi ui_state --target chat --json`'s output.
	chatRenderUIStateName = "render-verdict-ui-state.json"
	// chatRenderUIStateAllName is `gopi ui_state --target chat --all --json`'s
	// output — the oracle's own ledger, kept so the reading in the report can be
	// re-read from the file rather than trusted from the quote.
	chatRenderUIStateAllName = "render-verdict-ui-state-all.json"
)

// The harness's file vocabulary (Rule 1: no bare literals at a decision).
const (
	// chatRenderDumpName is the artifact's FILE NAME inside the run's evidence
	// directory; the directory itself is resolved at run time (it is a scratch
	// dir the test owns), so no absolute path can go stale here.
	chatRenderDumpName = "chat-screen.dump"
	// dumpFilePerm is the mode of the harness's own evidence files.
	dumpFilePerm = 0o644
	// chatRenderUIStateQuoteBytes bounds the ui_state quote in the report, so a
	// whole grid does not drown the verdict table.
	chatRenderUIStateQuoteBytes = 1500
	// chatRenderNoteKind is the tool-event kind a bash call paints ("⚙ call …").
	chatRenderNoteKind = "call bash"
	// chatRenderBoldStyle is how sgr renders the emphasis the band's bold span
	// must carry. A literal, because it is the sgr package's OUTPUT vocabulary
	// (the style-string format the dump's attr_runs are written in): the reader
	// compares against the string the artifact records, not against a constant
	// the artifact's writer owns.
	chatRenderBoldStyle = "bold"
	// chatRenderPrompt is what the harness types.
	chatRenderPrompt = "run the render-verdict turn"
)

// The scripted content. Kept as constants so the harness's trigger and the
// report's quote of it cannot drift apart.
const (
	// chatRenderBashCommand is the tool call's command: one long bash -c line
	// whose middle carries a >40-cell word, which is the ONLY break a note row
	// may not take silently (see cutAtBoundary / noteBreakInsideWord). It also
	// runs fast and produces one short line, so the answer is not drowned by
	// the tool result.
	chatRenderBashCommand = `bash -c 'echo 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdefghij'`
	// chatRenderAnswer is the model's answer. The markers are on their own
	// lines because the scripted answer is plain text (the assistant's own text
	// blocks are not drawn with the band's gutters, so markers must not share a
	// line with the text they bracket).
	chatRenderAnswer = chatRenderT1Open + "\n" + chatRenderCJKParagraph + "\n" +
		chatRenderT1Close + "\n" +
		chatRenderT2Open + "\n" + chatRenderMixedParagraph + "\n" +
		chatRenderT2Close + "\n" +
		chatRenderTail + "\n"
	// The four markers the script surrounds each shape with, plus the tail. They
	// are constants so the harness's trigger, its row lookup and the report's
	// quote of it cannot drift apart.
	chatRenderT1Open  = "<T1-DUP"
	chatRenderT1Close = "<T1-END"
	chatRenderT2Open  = "<T2-WRAP"
	chatRenderT2Close = "<T2-END"
	// chatRenderTail is the answer's LAST line: its arrival is the harness's
	// proof that the whole answer reached the terminal, so the waits never have
	// to guess when a paint finished (the rows of a long answer look alike).
	chatRenderTail = "<RENDER-VERDICT-END>"
	// chatRenderCJKParagraph is the T1 trigger: a CJK paragraph long enough to
	// wrap several times at 64 columns, with no ASCII word to break at (CJK
	// text has no spaces — every rune is its own break opportunity).
	chatRenderCJKParagraph = "这一段中文答案用来检查换行之后的列是否对齐，中间夹着 ASCII token 和 code 片段，" +
		"然后再来一段中文以确保换行发生多次。自观测闭环的验证必须在同一个屏幕上完成，否则读者无法把两边的证据对齐。"
	// chatRenderMixedParagraph is the T2 trigger: bold and inline-code spans
	// straddling the wrap, one of them broken mid-word.
	chatRenderMixedParagraph = "Live band probe: the row is **partially emphasised and then continues** " +
		"into `rowForPartial` and finishes with plain text so the wrap lands inside the styled run."
)

// chatRenderBlindSpots is what this harness CANNOT see, and how it handles each
// limit. It is printed with the table and repeated verbatim in the report: a
// verdict that does not state its own blind spots is a claim about the whole
// screen from a window.
var chatRenderBlindSpots = []string{
	"26-row window: the dump is a window the size of the grid, so content scrolled off it reads in_grid=false with damage=paint_loss/cjk_wrap — that is SCROLLING, not loss. Handled by keeping the whole measured script inside the window (the trigger rows are located by their own markers) and by quoting the marker rows rather than asserting on their coordinates.",
	"throttled sampling: the dump is written at most once per 100ms, so frames and dumps diverge — the artifact is the state at the LAST write, not the screen at every instant. Handled by waiting for the script's tail marker before quitting, and by reporting the sink's own frames/dumps counters.",
	"freeze-on-close: the sink freezes when the run tears its layout down, so the dump is the last painted screen rather than a live one; a shape whose evidence the exit repaint would destroy is measured from the pty transcript instead (the third channel the report quotes).",
	"an EMPTY GOPI_CHAT_DUMP means 'say nothing' rather than a path: the dump is ON BY DEFAULT now and an unset variable is what a default run looks like, so the harness never relies on the env switch at all — it passes the path with --dump-screen=PATH, which is the spelling that both arms the surface and names where the artifact goes.",
	"the harness drives a MOCK model (testutil.MockLLM): the scripted content is a trigger, not the operator's live traffic. Its answer is streamed in the stub's LEGACY shape — two halves, `TextSSE` — so no run of THIS file has ever seen a fragment smaller than half the answer, and the payload's every line is single long line that wraps. Both gaps are closed by the sibling guard chat_render_redzone_pty_test.go (Resp.TextDeltaBytes/TextDeltaDelay + a non-wrapping line in the same answer), which measures the same family at 1 and 4 bytes per fragment; this file's verdicts are unchanged and remain the wide-cadence reading.",
	"emphasis is measured from the artifact, not by the ui_state audit: the bold half's style string comes from the dump's per-cell attr_runs (attrs_recorded=true), read by chatRenderStyleAt. The cross-check that would use `gopi ui_state --target chat --all --json` is NOT available: that audit reported emphasis_checked=0 on this run, and its ledger contradicts itself (matched=6 while listing two mismatches) — filed as task 01a0e4f5-f9cc",
	"one geometry, one payload: T1/T2 are geometry- and content-dependent, so a PASS here is a PASS at 64x34 with this script — the harness is re-runnable at another geometry by changing the constants, and nothing claims more than it measured.",
}
