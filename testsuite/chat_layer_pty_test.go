package testsuite

// chat_layer_pty_test.go — the SCREEN-TRUTH evidence for the four-layer chat
// format (task 01a0e834): one real `gopi chat` run on a pty, with the answer's
// four layers produced by a real model round (a tool call, then the answer), and
// the four layer markers read off the artifact's CELL GRID — the attrs-bearing
// dump the surface wrote about its own screen, not a string this harness kept.
//
// WHY A PTY AND NOT A UNIT TEST. A unit test calls the renderer the way the test
// author imagined it; this run drives the product's own entry point with the real
// wiring (cmd/agent's chat loop, the line editor, the band, the dump sink), so
// the layer claim is measured on the same bytes an operator sees. The three
// channels the harness keeps apart are: the artifact (the surface's own screen),
// the pty byte stream (what the terminal was actually sent) and the session
// transcript (what the ledger recorded) — a layer present in one and missing from
// another is a defect this file can SEE rather than infer.
//
// THE CHECKS, and each is about a layer's own column:
//  1. all four layers appear on the grid, each opening with the table's marker;
//  2. the REQUEST row is the turn's first body row — above the tool call the
//     model asked for, which is the draw-order claim;
//  3. no row wears two markers (a doubled glyph reads as two calls);
//  4. the run's own `chat_surface see` reading agrees, and the transcript carries
//     the same user line — the cross-channel check.
//
// The evidence (dump, pty log, ui_state reading, transcript) is left in a
// scratch directory named in the test log, never in the worktree (Rule 30).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/uiclient"
	"golang.org/x/sys/unix"
)

const (
	chatLayerPrompt = "layer evidence probe"
	chatLayerBash   = "printf 'the bash call for the tool layer\\n'"
	// chatLayerTail is the answer's own last line: the harness waits for THIS
	// marker rather than sleeping, so the row it asserts is the row the run's
	// answer ended on.
	chatLayerTail    = "LAYER-EVIDENCE-END"
	chatLayerAnswer  = "the answer body line\n```\ncode row for the family\n```\n" + chatLayerTail + "\n"
	chatLayerPTYLog  = "chat-layer-pty.log"
	chatLayerSession = "chat-layer-session.txt"
)

// layerRows is the grid with trailing blanks removed, for one artifact.
func layerRows(cand uiclient.Candidate) []string {
	out := make([]string, 0, len(cand.Dump.Lines))
	for _, l := range cand.Dump.Lines {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

// findLayerRows returns the indices of the grid rows opening with layer's marker.
func findLayerRows(rows []string, layer lineedit.AnswerLayer) []int {
	marker := lineedit.LayerMarkerForTest(layer)
	var out []int
	for i, r := range rows {
		if strings.HasPrefix(r, marker) {
			out = append(out, i)
		}
	}
	return out
}

// TestChatLayerFormat_OnRealPTY measures the four-layer format on a real chat
// run's own screen artifact. It is the screen-truth half of the task's
// acceptance: the unit tests assert the table and the render path, this asserts
// what the terminal was shown.
func TestChatLayerFormat_OnRealPTY(t *testing.T) {
	bin := testutil.GopiBin(t)
	identity := chatRenderBuildIdentity(t, bin)
	t.Logf("LAYER-EVIDENCE harness invoked with GOPI_BIN=%s", bin)

	home := t.TempDir()
	work := t.TempDir()
	evidence := testutil.TestScratchDir(t, "chat-layer-format")
	// The artifact's name is the ONE this harness family already uses
	// (chatRenderDumpName): the shared reader below resolves that name inside the
	// evidence directory, and a second spelling would be a second path for one
	// file.
	dumpPath := filepath.Join(evidence, chatRenderDumpName)
	chatRenderDumpRoot = evidence
	t.Cleanup(func() { chatRenderDumpRoot = "" })

	// ONE turn carrying three of the four layers from the model's own round: a
	// tool call (the code-tool layer's tool half — announced BEFORE the answer,
	// which is what makes this run a real draw-order test), then the answer body
	// with a fenced block (response + the code-tool layer's code half). The
	// fourth layer, the request, is the operator's own submitted line.
	llm := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_layer_evidence_01",
			Name: "bash",
			Args: map[string]interface{}{"command": chatLayerBash},
		}},
		testutil.Resp{Text: chatLayerAnswer},
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
	chat.writeGapped(t, chatLayerPrompt)
	chat.write(t, "\r")
	ptyOut := chat.waitFor(t, chatLayerTail, ptyPromptTimeout)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	code := chat.close(t)
	if code != 0 {
		t.Fatalf("chat exit = %d, want 0 (the deliberate /quit); pty output:\n%s", code, ptyOut)
	}

	// ---- channel 1: the surface's OWN screen artifact ----------------------
	dumpFile, dumpText := chatRenderDumpArtifact(t)
	cand, err := uiclient.ReadDump(dumpFile)
	if err != nil {
		t.Fatalf("the harness could not read the artifact it asked the surface for (%s): %v", dumpFile, err)
	}
	if refusal := uiclient.CompareGridUnusable(cand); refusal != "" {
		t.Fatalf("the dump cannot be read as a screen, so no verdict can be measured from it: %s", refusal)
	}
	t.Logf("LAYER-EVIDENCE dump provenance: %s", cand.Record.Line)
	t.Logf("LAYER-EVIDENCE geometry=%s cols=%d rows=%d frames=%d dumps=%d attrs_recorded=%v",
		uiclient.GeometryOf(cand), cand.Dump.Cols, cand.Dump.Rows, cand.Frames, cand.Dumps, cand.Dump.AttrsRecorded)
	rows := layerRows(cand)
	for i, r := range rows {
		t.Logf("LAYER-EVIDENCE row %03d|%s", i, r)
	}
	if first := strings.SplitN(dumpText, "\n", 2); len(first) > 0 {
		t.Logf("LAYER-EVIDENCE dump file first line: %s", first[0])
	}

	// ---- the four layers, each on its own column ---------------------------
	layers := []lineedit.AnswerLayer{
		lineedit.LayerRequestForTest, lineedit.LayerThinkingForTest,
		lineedit.LayerResponseForTest, lineedit.LayerCodeToolForTest,
	}
	at := map[lineedit.AnswerLayer][]int{}
	for _, l := range layers {
		at[l] = findLayerRows(rows, l)
		t.Logf("LAYER-EVIDENCE layer %d marker=%q rows=%v", l, lineedit.LayerMarkerForTest(l), at[l])
	}
	// (1) The layers this run's script PRODUCES must be on the grid. The
	// reasoning layer is listed with the others but is NOT required: whether the
	// mock's round carries a reasoning block is the mock's business, and a layer
	// absent upstream must not be failed for being absent downstream — the claim
	// is that whichever layers the run produced are distinguishable, and the
	// reasoning gutter is asserted by the unit tests' own four-layer turn.
	required := []lineedit.AnswerLayer{
		lineedit.LayerRequestForTest, lineedit.LayerResponseForTest, lineedit.LayerCodeToolForTest,
	}
	for _, l := range required {
		if len(at[l]) == 0 {
			t.Fatalf("layer %d (marker %q) is not on the run's own screen\n%s",
				l, lineedit.LayerMarkerForTest(l), strings.Join(rows, "\n"))
		}
	}
	t.Logf("LAYER-EVIDENCE reasoning layer rows=%v (absent when the round streams no reasoning block)", at[lineedit.LayerThinkingForTest])

	// (1b) The GUTTER CELLS are not alone: the layer's own STYLE is read off the
	// same artifact at column 0 of its rows (the dump's per-cell attr_runs, never
	// OCR). Two layers that shared a style would be one visual thing with two
	// markers, which is not a distinguishable layer.
	styleAt := map[lineedit.AnswerLayer]string{}
	for _, l := range layers {
		if len(at[l]) == 0 {
			continue
		}
		styleAt[l] = chatRenderStyleAt(cand, at[l][0], 0)
		t.Logf("LAYER-EVIDENCE layer %d gutter=%q style@col0=%q (from grid row %d)",
			l, lineedit.LayerMarkerForTest(l), styleAt[l], at[l][0])
	}
	for _, l := range required {
		if sa, ok := styleAt[l]; ok && (sa == "" || sa == "?") {
			t.Fatalf("layer %d (gutter %q) is on the grid but its column-0 cell carries no readable style (%q) — the artifact cannot answer the style half\n%s",
				l, lineedit.LayerMarkerForTest(l), sa, strings.Join(rows, "\n"))
		}
	}
	for _, a := range layers {
		sa, okA := styleAt[a]
		if !okA {
			continue
		}
		for _, b := range layers {
			if b <= a {
				continue
			}
			sb, okB := styleAt[b]
			if !okB {
				continue
			}
			if sa == sb {
				t.Errorf("layers %d and %d share the cell style %q at column 0 — two markers over one style is one visual thing\n%s",
					a, b, sa, strings.Join(rows, "\n"))
			}
		}
	}

	// (2) THE DRAW ORDER: the request row precedes the tool row the model asked
	// for — the call is the answer to the request, never its preface.
	req, tool := at[lineedit.LayerRequestForTest][0], at[lineedit.LayerCodeToolForTest][0]
	if tool < req {
		t.Fatalf("the tool row is at grid row %d, ABOVE the request row %d — the screen shows the call before the question\n%s",
			tool, req, strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[req], chatLayerPrompt) {
		t.Fatalf("the request row %d = %q does not carry the operator's own line %q\n%s",
			req, rows[req], chatLayerPrompt, strings.Join(rows, "\n"))
	}

	// (3) No row wears two markers of the same layer: the tool row's glyph is
	// the ROW's own text (cmd/agent's termToolLine), and a renderer that added a
	// second would be read as two calls.
	for _, i := range at[lineedit.LayerCodeToolForTest] {
		if strings.HasPrefix(rows[i], lineedit.ToolGutter+lineedit.ToolGutter) {
			t.Errorf("grid row %d = %q wears the tool glyph twice", i, rows[i])
		}
	}

	// (4) The ANSWER's own text reached the grid — and each half of it on the
	// layer the TABLE says it belongs to. The prose half is the response layer's
	// row; the FENCED half is the code/tool family's, which is a different column.
	//
	// THE RULING THIS TEST USED TO CONTRADICT (decided 2026-10-30 on master
	// 20b01c4, the red this assertion replaces). A body line's layer is decided by
	// the composer's verdict, and a fenced or diff line maps to LayerCodeTool:
	// `layerForLine`, internal/lineedit/layers.go:97-101. That layer's rows are the
	// reader's COPYING material, so they are drawn with NO gutter at all — a code
	// row carries `copied: true` and `answerRow.gutterGlyph` then draws no turn
	// marker (internal/lineedit/answer.go:2175-2206, the operator's paste leg:
	// select a fenced command, paste it, and the first line must not die on `▌`).
	// internal/lineedit/layers_test.go:195-205 pins the same shape at the unit
	// level ("the code half of the code/tool family carries no marker"). So the
	// fenced row is ON the grid and OFF the marker column: an assertion that looks
	// for it among the response layer's marker-bearing rows can never pass, and a
	// fenced row that DID wear a gutter would be the defect (two layers merged onto
	// one column, and a command a reader cannot paste).
	bodySeen := false
	for _, i := range at[lineedit.LayerResponseForTest] {
		if strings.Contains(rows[i], "the answer body line") {
			bodySeen = true
		}
	}
	if !bodySeen {
		t.Errorf("no response-layer row carries the answer's own text\n%s", strings.Join(rows, "\n"))
	}
	codeRow := -1
	for i, r := range rows {
		if strings.Contains(r, "code row for the family") {
			codeRow = i
			break
		}
	}
	if codeRow < 0 {
		t.Fatalf("the fenced row's own text is on no grid row: the code half of the code/tool layer never reached the screen\n%s",
			strings.Join(rows, "\n"))
	}
	// The copying half opens at COLUMN 0: no layer's marker may precede it, which
	// is the same claim (read off one table) that a `▌ ` row would violate.
	for _, l := range layers {
		if m := lineedit.LayerMarkerForTest(l); m != "" && strings.HasPrefix(rows[codeRow], m) {
			t.Fatalf("the fenced row %d = %q opens with layer %d's marker %q — a code row is the COPYING half and must open at column 0 (answerRow.gutterGlyph)\n%s",
				codeRow, rows[codeRow], l, m, strings.Join(rows, "\n"))
		}
	}
	// And it is painted as a LAYER, not as loose text at column 0: the artifact
	// must answer for its cell at the row's first column.
	if st := chatRenderStyleAt(cand, codeRow, 0); st == "" || st == "?" {
		t.Fatalf("the fenced row %d is on the grid but its column-0 cell carries no readable style (%q) — the artifact cannot say how this layer is drawn\n%s",
			codeRow, st, strings.Join(rows, "\n"))
	} else {
		t.Logf("LAYER-EVIDENCE fenced row %d (code/tool copying half, no gutter) style@col0=%q", codeRow, st)
	}

	// ---- channel 2: the run's own chat_surface reading ---------------------
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	uiOut, uiErr, uiCode := testutil.RunGopi(t,
		[]string{"chat_surface", "action=see"},
		"", testutil.MockEnv(t, home, llm.URL()), work)
	t.Logf("LAYER-EVIDENCE chat_surface see exit=%d stderr=%s", uiCode, strings.TrimSpace(uiErr))
	t.Logf("LAYER-EVIDENCE chat_surface see stdout (first %d bytes):\n%s",
		chatRenderUIStateQuoteBytes, firstBytes(uiOut, chatRenderUIStateQuoteBytes))

	// ---- channel 3: the transcript, and the kept evidence ------------------
	if err := os.WriteFile(filepath.Join(evidence, chatLayerPTYLog), []byte(ptyOut), dumpFilePerm); err != nil {
		t.Fatalf("write the pty transcript evidence: %v", err)
	}
	if err := os.WriteFile(filepath.Join(evidence, chatLayerSession), []byte(chatRenderSessionText(t, sess)), dumpFilePerm); err != nil {
		t.Fatalf("keep the session transcript beside the dump: %v", err)
	}
	t.Logf("LAYER-EVIDENCE artifacts kept under %s (dump, pty log, session transcript)", evidence)
	t.Logf("LAYER-EVIDENCE build identity: %s", identity)
	if !strings.Contains(chatRenderSessionText(t, sess), chatLayerPrompt) {
		t.Errorf("the session transcript does not carry the operator's line %q — the request layer left no record", chatLayerPrompt)
	}
	t.Logf("LAYER-EVIDENCE summary: request_row=%d tool_rows=%v response_rows=%v",
		req, at[lineedit.LayerCodeToolForTest], at[lineedit.LayerResponseForTest])
	fmt.Fprintf(os.Stderr, "")
}
