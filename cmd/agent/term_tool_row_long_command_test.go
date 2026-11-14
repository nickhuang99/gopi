package main

// term_tool_row_long_command_test.go — the COPY CONTRACT for a command that is
// wider than the digest: `>200` cells, from the tool call all the way to the
// bytes a reader pastes off an 80-column screen.
//
// THE DEFECT THIS PINS (measured 2026-10-27). agent.ToolEvent carried only
// Summary, and Summary is a DIGEST: firstLineOf caps it at
// agent's toolCallSummaryMaxChars with a `…`. A 200-cell bash command therefore
// reached termToolRow already cut, so CopyText — the row's whole reason to exist,
// "select the command and run it" — carried `…` and could not be run. The
// previous tests could not see it: they handed termToolRow a Summary the TEST had
// composed at full length (`TestTermToolRowNeverEllipsisesALongCommand` builds
// `cmd` and puts it straight into Summary), which is a state the production wire
// can never produce — the cut happens UPSTREAM, in ToolCallSummary.
//
// WHAT IS ASSERTED, and each half is a different owner's claim:
//
//  1. THE WIRE loses the bytes (Summary carries `…`, shorter than the command) —
//     the premise. If this ever stops being true the rest of this file is
//     measuring nothing, so it is asserted rather than assumed.
//  2. THE EVENT keeps them: agent.ToolCallEvent stamps Command with the very same
//     argument, uncut (no `…`, byte-equal).
//  3. THE ROW copies them: termToolRow's CopyText IS that command byte for byte,
//     and the rendered line carries it contiguously exactly once — so selecting
//     the command region of the screen is the command.
//  4. THE SCREEN holds them: on a REAL 80-column pty the row is drawn through the
//     band (the production sink, lineedit.NoteRuns → ChatEditor), and the grid's
//     rows reassembled in order equal the command byte for byte. This is the half
//     a renderer unit test cannot reach (Rule 31): a wrapped row is where a
//     truncation would actually live.
//
// NOTHING HERE CHANGES the wire's Summary: assertion 1 requires it to stay cut.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// longCmd runs to >200 cells so it cannot survive the digest cap, and carries
// spaces, quotes, slashes and a `#`, because those are the characters a wrap or a
// boundary cut would damage (`-ma…xdepth` was the measured shape). It is ONE line
// and ends in a marker the reconstruction can look for.
const longCmd = `bash -lc 'cd /home/user/work/gopi && git log --oneline --all --decorate --graph -40 -- internal/tools/task_finish_cleanup.go internal/tools/task_sync_git_pushback.go | head -60 && echo LONG_COMMAND_TAIL'`

// longCmdMinCells is the width the fixture must exceed for the case to be the
// defect rather than a coincidence of a long-ish string.
const longCmdMinCells = 200

// longCmdGeometry is the pty the screen half uses: the narrow width the report
// promises evidence at (80 columns), tall enough for a band to host the row's
// wrap without scrolling the row itself out of the grid.
const (
	longCmdPtyCols = 80
	longCmdPtyRows = 40
)

// longCmdEvidenceDir is where the dumped grid is written when the run asks for
// it (EVIDENCE_DIR), so the artifact can be committed to a session directory and
// quoted in the report. With no such variable the test still runs — the dump goes
// to t.TempDir() and only the LOG line is the evidence (a run that rejects the
// dump must still fail the case).
func longCmdEvidenceDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("EVIDENCE_DIR")
	if dir == "" {
		return t.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("evidence dir %s: %v", dir, err)
	}
	return dir
}

// longCmdCall is the tool call the model would have made. It is a REAL
// deepseek.ToolCall so the path under test starts where production starts.
func longCmdCall() deepseek.ToolCall {
	return deepseek.ToolCall{
		Name:      "bash",
		Arguments: map[string]interface{}{"command": longCmd},
	}
}

// TestLongCommandSurvivesTheWireIntoTheCopyText is assertions 1–3: the digest
// loses the bytes, the event keeps them, the row copies them.
//
// It opens with the PREMISE as a positive assertion, which doubles as the red
// proof of the fix: strip ToolCallEvent's Command line and assertion 2 fails
// (`Command = ""`), while assertion 3 fails on the `…` — so the test cannot go
// green on a wire that still cuts, and cannot go green on a renderer that ignores
// the field.
func TestLongCommandSurvivesTheWireIntoTheCopyText(t *testing.T) {
	if n := lineedit.DisplayWidth(longCmd); n < longCmdMinCells {
		t.Fatalf("the fixture is %d cells, want >%d — below the digest cap this case measures nothing", n, longCmdMinCells)
	}
	tc := longCmdCall()
	ev := agent.ToolCallEvent(tc)

	// 1. THE PREMISE: the digest is the cut rendering of the same argument.
	if !strings.Contains(ev.Summary, "…") {
		t.Fatalf("Summary = %q carries no cut glyph — the digest cap moved or was removed, so this file's premise no longer holds", ev.Summary)
	}
	if len(ev.Summary) >= len(longCmd) {
		t.Fatalf("Summary (%d bytes) is not shorter than the command (%d) — the wire did not digest", len(ev.Summary), len(longCmd))
	}

	// 2. THE EVENT KEEPS THE BYTES: same parse, no cut.
	if ev.Command != longCmd {
		t.Fatalf("ToolEvent.Command is not the call's command bytes:\n got %q\nwant %q", ev.Command, longCmd)
	}
	if strings.Contains(ev.Command, "…") {
		t.Fatalf("ToolEvent.Command carries a cut glyph: %q", ev.Command)
	}

	// 3. THE ROW COPIES THEM.
	row := termToolRow(ev, termToolStylesFor(lineedit.ColourOn))
	if row.CopyText != longCmd {
		t.Fatalf("CopyText is not the command byte for byte (%d vs %d bytes):\n got %q\nwant %q",
			len(row.CopyText), len(longCmd), row.CopyText, longCmd)
	}
	if strings.Contains(row.CopyText, termToolResultEllipsis) {
		t.Fatalf("CopyText carries the cut glyph `%s` — a paste of this row is not runnable: %q", termToolResultEllipsis, row.CopyText)
	}
	got := row.Text()
	if !strings.Contains(got, longCmd) {
		t.Fatalf("the rendered row does not carry the command contiguously: %q", got)
	}
	if n := strings.Count(got, longCmd); n != 1 {
		t.Fatalf("the command appears %d time(s) in the row, want exactly 1: %q", n, got)
	}
	if strings.Contains(got, termToolResultEllipsis) {
		t.Fatalf("the rendered row carries a cut glyph in a command: %q", got)
	}
	// The COLOURED bytes carry the command too (a paste from a colour terminal is
	// the same paste): the SGR between cells must never land inside it.
	if plain := stripSGRForTest(row.Row()); !strings.Contains(plain, longCmd) {
		t.Fatalf("the coloured row's plain text does not carry the command contiguously: %q", plain)
	}

	// THE NEGATIVE CONTROL, in-process: the SAME event with Command cleared is
	// what every other tool still produces, and it must fall back to the digest —
	// so the assertions above are measuring the field and not a coincidence of
	// Summary's length.
	fallback := termToolRow(agent.ToolEvent{Kind: ev.Kind, Name: ev.Name, Summary: ev.Summary}, termToolStylesFor(lineedit.ColourOn))
	if fallback.CopyText != ev.Summary {
		t.Fatalf("a call with no Command must copy its Summary, got %q", fallback.CopyText)
	}
	if fallback.CopyText == longCmd {
		t.Fatal("the fallback arm reproduced the full command from a cut Summary — the digest is not cut, so this file measures nothing")
	}

	t.Logf("COPYTEXT BYTE-EXACT: %d cells / %d bytes, no cut glyph; digest was %d bytes and carried `…`",
		lineedit.DisplayWidth(row.CopyText), len(row.CopyText), len(ev.Summary))
}

// TestLongCommandReachesThe80ColumnBandGrid is assertion 4: the production band,
// a real 80-column pty, the production dump reader.
//
// THE ROW WRAPS, and that is the point rather than an inconvenience: at 80 cells
// a 200-cell command occupies four rows of the grid, and the continuation rows are
// exactly where a truncation would show up. The assertion REASSEMBLES them in
// order (what a reader's selection does) and requires the result to be the command
// byte for byte — with no `…` anywhere in the rows it was built from.
func TestLongCommandReachesThe80ColumnBandGrid(t *testing.T) {
	// THE PRODUCTION INPUT LAYER over a REAL pty — `newChatInputWithSource` is the
	// constructor the REPL uses, and it is the piece this test must not fake: a
	// note published while NOBODY IS READING is never painted (measured: an
	// Editor.NoteRuns with the read loop not started leaves the grid all-blank,
	// because the band's single writer IS the read loop), so a case that skipped
	// the loop would assert about a screen nothing ever drew on.
	pair := chatOpenPtyPair(t, longCmdPtyCols, longCmdPtyRows)
	dir := longCmdEvidenceDir(t)
	sink, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path:     filepath.Join(dir, uiclient.ChatDumpFileName),
		Cols:     longCmdPtyCols,
		Rows:     longCmdPtyRows,
		Geometry: uiclient.GeometrySourceTerminal,
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              os.Getpid(),
			Target:           chatDumpTarget(false),
			TerminalColsRows: [2]int{0, 0},
			Started:          uiDumpStartStamp(),
		},
	})
	if err != nil {
		t.Fatalf("arm the dump sink: %v", err)
	}
	term, err := lineedit.Open(pair.Slave, sink)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	in := newChatInputWithSource(term, sink, chatPrompt)
	in.editor.SetPalette(lineedit.BasicPalette)
	in.editor.SetGeometryObserver(chatGeomObserver(sink))
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the case needs the real chat layout installed", longCmdPtyCols, longCmdPtyRows)
	}
	if got := lineedit.TerminalCols(int(pair.Slave.Fd())); got != longCmdPtyCols {
		t.Fatalf("TerminalCols = %d, want %d — the evidence claims 80 columns", got, longCmdPtyCols)
	}
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		_, _ = in.editor.Run() //stylegate:ignore the read loop's return is joined below; a real failure surfaces as the bounded grid wait that dumps the screen
	}()
	t.Cleanup(func() {
		// The terminal's stop path FIRST, then the join (the order chat's own
		// REPL fixtures use): closing the editor releases the layout, closing the
		// terminal is what ends ReadEvent.
		in.editor.Close() //stylegate:ignore the harness's own terminal teardown
		_ = term.Close()  //stylegate:ignore the harness's own terminal teardown
		chatJoinReadLoop(t, exited, func() string { return runState(exited) })
		_ = pair.Master.Close() //stylegate:ignore pty master teardown of the fixture; the grid was asserted from the captured dump
	})

	// THE PRODUCTION PATH, verbatim: cmd/agent's chat callback builds the row with
	// termToolRow, converts it to note runs with termToolRuns (mask nil — a plain
	// chat has no redaction pass) and hands them to the band through the input
	// layer.
	ev := agent.ToolCallEvent(longCmdCall())
	row := termToolRow(ev, termToolStylesFor(lineedit.ColourOn))
	if !in.WriteToolRowRuns(termToolRuns(row, nil)) {
		t.Fatal("the band refused the tool row (BandActive said otherwise) — nothing to measure")
	}

	// THE OBSERVABLE: the command's LAST cell appears in the grid. `head -60 && echo
	// LONG_COMMAND_TAIL` puts a unique token at the tail, so a row cut anywhere
	// upstream cannot satisfy this — it is the tail, not the head, that separates
	// "wrapped" from "truncated".
	if !waitForGridToken(t, sink, "LONG_COMMAND_TAIL", longCmdGridWait) {
		t.Fatalf("the command's tail never reached the grid within %v — the row was cut on the way to the screen:\n%s",
			longCmdGridWait, sink.Snapshot().Text())
	}
	// Freeze the sink on its last painted frame BEFORE the editor's teardown
	// disables (and erases) the region.
	if err := sink.Close(); err != nil {
		t.Fatalf("close dump: %v", err)
	}
	cand, err := uiclient.ReadDump(filepath.Join(dir, uiclient.ChatDumpFileName))
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	lines := cand.Dump.Lines

	// The rows the command occupies: from the row carrying the call's marker to
	// the last row before the next row that opens with the marker again. Both
	// markers are asked of internal/lineedit rather than spelled here.
	start, end := -1, -1
	for i, ln := range lines {
		body := strings.TrimSpace(ln)
		if start < 0 {
			if strings.HasPrefix(body, lineedit.ToolGlyph) {
				start = i
				end = i
			}
			continue
		}
		if strings.HasPrefix(body, lineedit.ToolGlyph) || body == "" {
			break
		}
		end = i
	}
	if start < 0 {
		t.Fatalf("no `%s call` row is in the dumped grid:\n%s", lineedit.ToolGlyph, cand.Dump.Text())
	}
	// The block's text with every continuation gutter REMOVED, joined in order:
	// what a reader's selection of these rows produces once the band's own marker
	// cells are dropped (the marker is chrome, the command is the payload — the
	// same reason termRow separates CopyText from Render).
	// The block's rows with every continuation gutter REMOVED, in order.
	rows := make([]string, 0, end-start+1)
	for i := start; i <= end; i++ {
		ln := strings.TrimRight(lines[i], " ")
		if i > start {
			ln = strings.TrimPrefix(ln, lineedit.NoteContinuation)
		}
		rows = append(rows, ln)
	}
	// Row 0 opens with the band's CHROME — `⚙ call bash ` — before the command.
	// The prefix is taken from the renderer's own text (the row's bytes minus the
	// command it carries) rather than spelled here, so a change to the markers
	// moves this with it instead of failing a test that agreed with itself.
	chrome := strings.TrimSuffix(row.Text(), longCmd)
	if chrome == row.Text() {
		t.Fatalf("the rendered row does not end with the command, so its chrome cannot be separated: %q", row.Text())
	}
	if !strings.HasPrefix(rows[0], chrome) {
		t.Fatalf("the screen's first command row %q does not open with the row's own chrome %q", rows[0], chrome)
	}
	rows[0] = strings.TrimPrefix(rows[0], chrome)
	pasted := strings.Join(rows, "")
	if strings.Contains(pasted, termToolResultEllipsis) {
		t.Errorf("the command's rows on the 80-column screen carry a cut glyph `%s` — the paste is not runnable:\n%q",
			termToolResultEllipsis, pasted)
	}
	// THE RECONSTRUCTION, and why it is not a plain string comparison. The band's
	// wrap breaks a note at a boundary and CONSUMES that boundary character when it
	// was a space (answer.go's noteTail: "the break IS the space between two
	// words"), so a reader's selection across a wrapped row loses one space per
	// wrap — a documented property of the band, not of this row. The claim that
	// matters is that NOTHING ELSE is lost: every remaining byte of the command is
	// on the screen, in order, unchanged. gridBlockIsCommand proves exactly that —
	// it walks the command and the rows together and permits only the boundary
	// space to be absent.
	ok, why := gridBlockIsCommand(rows, longCmd)
	if !ok {
		t.Errorf("the block read off the 80-column screen is not the command: %s\n  screen rows %d..%d = %q\n  want command    = %q\n%s",
			why, start, end, pasted, longCmd, cand.Dump.Text())
	} else {
		t.Logf("GRID COPY: %d row(s) %d..%d, %d cells; command %d cells — every byte on screen, in order, no cut glyph",
			end-start+1, start, end, lineedit.DisplayWidth(pasted), lineedit.DisplayWidth(longCmd))
	}
	t.Logf("EVIDENCE dump: %s", filepath.Join(dir, uiclient.ChatDumpFileName))
	for i := start; i <= end; i++ {
		t.Logf("grid row %2d %q", i, strings.TrimRight(lines[i], " "))
	}
}

// gridBlockIsCommand reports whether the screen's rows, joined, reconstruct the
// command — permitting ONLY the one thing the band's wrap legitimately removes: a
// single SPACE at a row boundary (answer.go's noteTail consumes the boundary
// character the break used, and that character is a space when the break landed
// between two words).
//
// It walks the rows in order against the command rather than comparing strings,
// so a row that is merely a SUBSTRING of the command cannot satisfy it: a dropped
// character, an added cell or a reordered run makes the walk fail and is named in
// the returned reason.
func gridBlockIsCommand(rows []string, command string) (bool, string) {
	cursor := 0
	for idx, row := range rows {
		if idx > 0 {
			// A continuation row may be missing its boundary space. Take the space
			// only when the row does NOT match at the current cursor — the "kept"
			// boundary case (the break landed on one of cutNoteBoundaries, which
			// stays in the text) must not be double-counted.
			if !strings.HasPrefix(command[cursor:], row) && cursor < len(command) && command[cursor] == ' ' {
				cursor++
			}
		}
		if !strings.HasPrefix(command[cursor:], row) {
			return false, fmt.Sprintf("row %d %q does not continue the command at byte %d (command there: %q)",
				idx, row, cursor, tailForReason(command[cursor:]))
		}
		cursor += len(row)
	}
	if cursor != len(command) {
		return false, fmt.Sprintf("the screen stops at byte %d of %d (missing: %q)",
			cursor, len(command), tailForReason(command[cursor:]))
	}
	return true, ""
}

// tailForReason shortens a command tail for a failure message.
func tailForReason(s string) string {
	const max = 40
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// longCmdGridWait bounds the wait for the command's tail to reach the grid. The
// band commits a note before NoteRuns returns (the editor's own read loop is the
// only writer and it is already parked on the next read), so the first pass
// normally sees it; the bound exists so a genuinely dropped row FAILS here with
// its own name instead of surfacing as a confusing "no row" assertion later.
const longCmdGridWait = 5 * time.Second

// waitForGridToken polls a snapshot for a token in any row. It is a pure
// in-memory read of the sink's grid (no sleep-driven progress, no device), so it
// neither races the dump's throttle nor passes by waiting long enough.
func waitForGridToken(t *testing.T, d *uiclient.ScreenDumper, token string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for {
		for _, ln := range d.Snapshot().Lines {
			if strings.Contains(ln, token) {
				return true
			}
		}
		if !time.Now().Before(deadline) {
			return false
		}
		runtime.Gosched()
	}
}
