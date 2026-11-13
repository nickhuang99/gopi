package main

// term_tool_row_test.go — the tool-activity row's SHAPE and its COPY contract.
//
// The two properties this file is here to keep are the ones a reader can lose
// without noticing, because both fail SILENTLY on a screen:
//
//  1. THE COMMAND IS THE COMMAND. Row.CopyText is the event's own bytes, and the
//     cell that carries it holds those bytes unchanged — no `…`, no border, no
//     indent inserted INTO it. A pasted tool row that does not run is worse than
//     no tool row at all (the operator's own sighting: a mid-token `…` turned
//     `-maxdepth` into `-ma…xdepth`, and every line of a pasted turn died on the
//     `▌ ` gutter).
//  2. COLOUR IS ONE DECISION. With colour off the row is plain text — zero
//     escape bytes, not a pair of resets — and with colour on the call half and
//     the result half do not carry the same emphasis.
//
// The commands below are REAL ones from this project's own vocabulary (the five
// shapes an operator actually pastes: quotes, `&&`, `$VAR`, a CJK path, a long
// argument), because a fixture that is a single word proves nothing about a
// cut that only bites at a quoted space.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sgr"
)

// termToolRowRealCommands are the five command shapes the copy contract is
// pinned against. Each is a command an operator of THIS project pastes.
var termToolRowRealCommands = []struct {
	name    string
	command string
}{
	{"quotes", `git log --oneline -3 --grep "worktree at"`},
	{"and-and", `cd /home/nick/workspace/01a150dc && git status --short && git log --oneline -1`},
	{"variable", `cd "$GOPI_HOME" && ls -1 sessions | tail -5`},
	{"cjk-path", `cd /home/nick/工作区/测试 && cat 报告.txt`},
	{"long-arg", `go test ./cmd/agent -run TestTermToolRowCarriesTheCommandVerbatim -count=1 -v -timeout 900s -args -probe=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`},
}

// TestTermToolRowCarriesTheCommandVerbatim is the copy contract: for every real
// command shape, the row's CopyText is the event's summary BYTE FOR BYTE, and
// that same byte sequence appears CONTIGUOUSLY and UNCHANGED inside the rendered
// line (so selecting the command region of the screen gives the command).
func TestTermToolRowCarriesTheCommandVerbatim(t *testing.T) {
	st := termToolStylesFor(lineedit.ColourOn)
	for _, tc := range termToolRowRealCommands {
		t.Run(tc.name, func(t *testing.T) {
			ev := agent.ToolEvent{Kind: agent.ToolEventKindCall, Name: "bash", Summary: tc.command}
			row := termToolRow(ev, st)
			if row.CopyText != ev.Summary {
				t.Fatalf("CopyText = %q, want the event's own bytes %q", row.CopyText, ev.Summary)
			}
			if got := row.Text(); !strings.Contains(got, tc.command) {
				t.Fatalf("the rendered row does not carry the command contiguously:\n  row = %q\n  cmd = %q", got, tc.command)
			}
			// The command's OWN cells are the command: find the cell whose text is
			// the summary and check it is untouched.
			found := false
			for _, c := range row.Render {
				if c.Text == tc.command {
					found = true
				}
			}
			if !found {
				t.Fatalf("no single cell holds the command verbatim — the renderer cut it: %#v", row.Render)
			}
			// Nothing was appended to or removed from the payload.
			if n := strings.Count(row.Text(), tc.command); n != 1 {
				t.Fatalf("the command appears %d times in %q, want exactly 1", n, row.Text())
			}
		})
	}
}

// TestTermToolRowNeverEllipsisesALongCommand is the 200-cell case: a command far
// wider than an 80-column terminal is ONE row of cells here, with no `…` — the
// width belongs to the band's own wrap (which continues the row and marks the
// continuation), never to a cut inside the command.
func TestTermToolRowNeverEllipsisesALongCommand(t *testing.T) {
	cmd := "bash -lc 'echo " + strings.Repeat("abcdefghij", 20) + "'" // 200+ cells
	if len([]rune(cmd)) < 200 {
		t.Fatalf("the fixture is only %d runes — it must exceed 200", len([]rune(cmd)))
	}
	ev := agent.ToolEvent{Kind: agent.ToolEventKindCall, Name: "bash", Summary: cmd}
	row := termToolRow(ev, termToolStylesFor(lineedit.ColourOn))
	if row.CopyText != cmd {
		t.Fatalf("CopyText lost characters: %d runes, want %d", len([]rune(row.CopyText)), len([]rune(cmd)))
	}
	if strings.Contains(row.Text(), termToolResultEllipsis) {
		t.Fatalf("the row carries a cut glyph in a command: %q", row.Text())
	}
	if strings.Contains(row.Row(), termToolResultEllipsis) {
		t.Fatalf("the coloured bytes carry a cut glyph in a command")
	}
	if !strings.Contains(row.Text(), cmd) {
		t.Fatalf("the long command is not carried verbatim")
	}
}

// TestTermToolRowNoColourEmitsNoSGR is the NO_COLOR half: the monochrome table
// renders the row with ZERO escape bytes (a reset pair is still a byte the
// operator's log or a paste has to survive, and the project's colour-off
// invariant is "no sequence at all").
func TestTermToolRowNoColourEmitsNoSGR(t *testing.T) {
	ev := agent.ToolEvent{Kind: agent.ToolEventKindCall, Name: "bash", Summary: `ls -1 "$GOPI_HOME"`}
	row := termToolRow(ev, termToolStylesFor(lineedit.ColourOff))
	if got := row.Row(); got != row.Text() {
		t.Fatalf("colour-off bytes = %q, want the plain text %q", got, row.Text())
	}
	if strings.ContainsRune(row.Row(), 0x1b) {
		t.Fatalf("colour-off row carries an escape byte: %q", row.Row())
	}
	res := termToolRow(agent.ToolEvent{Kind: agent.ToolEventKindResult, Name: "bash", Summary: "ok"}, termToolStylesFor(lineedit.ColourOff))
	if strings.ContainsRune(res.Row(), 0x1b) {
		t.Fatalf("colour-off result row carries an escape byte: %q", res.Row())
	}
	if res.Row() != res.Text() {
		t.Fatalf("colour-off result bytes = %q, want %q", res.Row(), res.Text())
	}
}

// TestTermToolRowCallAndResultAreDistinct is the "colour ON" half: the two
// halves of one activity must not be drawn identically — a call is the action
// (marker, name, payload) and a result is its echo (dim, indented, `↳`).
func TestTermToolRowCallAndResultAreDistinct(t *testing.T) {
	st := termToolStylesFor(lineedit.ColourOn)
	call := termToolRow(agent.ToolEvent{Kind: agent.ToolEventKindCall, Name: "bash", Summary: "ls"}, st)
	res := termToolRow(agent.ToolEvent{Kind: agent.ToolEventKindResult, Name: "bash", Summary: "README.md"}, st)

	// The marker cell differs in BOTH text and emphasis.
	if call.Render[0].Text == res.Render[0].Text {
		t.Fatalf("call and result share the marker glyph %q", call.Render[0].Text)
	}
	if call.Render[0].Style.Equal(res.Render[0].Style) {
		t.Fatalf("call and result share the marker emphasis (%s)", call.Render[0].Style)
	}
	// The payload of a call is not dim; the echo of a result is.
	callPayload := call.Render[len(call.Render)-1]
	resDigest := res.Render[len(res.Render)-1]
	if callPayload.Style.Dim {
		t.Errorf("the call's command is drawn dim — the payload is the row's material, not a hint")
	}
	if !resDigest.Style.Dim {
		t.Errorf("the result's digest is not dim")
	}
	if callPayload.Style.Equal(resDigest.Style) {
		t.Errorf("call and result payloads share one emphasis (%s)", callPayload.Style)
	}
	// THE CHROME IS EMPHASISED AND THE PAYLOAD IS 本色. `命令本色` is the epic's
	// own word for it: the glyph and the name are the surface's judgement and
	// carry emphasis, while the command is the operator's material and is drawn
	// in the terminal's own foreground (sgr.Default) — anything else would put
	// the interface's taste on the thing they are about to paste.
	for i, c := range call.Render {
		if strings.TrimSpace(c.Text) == "" {
			// The SEPARATORS belong to the payload side: they are the gap between
			// the chrome and the command, and they carry the command's colour so a
			// terminal that paints a background cannot draw a two-tone gap.
			if !c.Style.Equal(sgr.Default()) {
				t.Errorf("separator %d carries emphasis (%s)", i, c.Style)
			}
			continue
		}
		isPayload := i == len(call.Render)-1
		if isPayload {
			if !c.Style.Equal(sgr.Default()) {
				t.Errorf("the command cell %q is not the terminal's own colour (%s)", c.Text, c.Style)
			}
			continue
		}
		if c.Style.Equal(sgr.Default()) {
			t.Errorf("chrome cell %d (%q) carries NO emphasis in a coloured table", i, c.Text)
		}
	}
}

// TestTermToolRowResultDigestIsTheOnlyCut pins WHERE the one allowed cut lives:
// a result digest longer than the cap is capped and marked, and the command half
// never is — the same statement as the 200-cell test, made about the other half.
func TestTermToolRowResultDigestIsTheOnlyCut(t *testing.T) {
	long := strings.Repeat("r", termToolResultMaxRunes+50)
	res := termToolRow(agent.ToolEvent{Kind: agent.ToolEventKindResult, Name: "bash", Summary: long}, termToolStylesFor(lineedit.ColourOff))
	if !strings.HasSuffix(res.Text(), termToolResultEllipsis) {
		t.Fatalf("a capped result digest is not marked: %q", res.Text())
	}
	if n := len([]rune(res.Text())); n > termToolResultMaxRunes+40 {
		t.Fatalf("the capped row is %d runes — the cap did not apply", n)
	}
	// CopyText stays the UNCUT summary (the wire value is not this file's to
	// change — ToolResultDigest composed it).
	if res.CopyText != long {
		t.Fatalf("CopyText was cut: %d runes, want %d", len([]rune(res.CopyText)), len([]rune(long)))
	}
}

// TestTermToolRowShapesMatchTheHistoricLine pins that the renderer's TEXT is the
// line this surface has always printed: `⚙ call <tool> <digest>` and, for a
// result, the continuation marker instead of the glyph. The glyph is
// lineedit.ToolGlyph — one definition (Rule 14) — and the result's marker is the
// continuation marker the transcript already uses.
func TestTermToolRowShapesMatchTheHistoricLine(t *testing.T) {
	st := termToolStylesFor(lineedit.ColourOff)
	got := termToolRow(agent.ToolEvent{Kind: agent.ToolEventKindCall, Name: "read", Summary: "/etc/hostname"}, st).Text()
	want := lineedit.ToolGlyph + " call read /etc/hostname"
	if got != want {
		t.Errorf("call row = %q, want %q", got, want)
	}
	got = termToolRow(agent.ToolEvent{Kind: agent.ToolEventKindResult, Name: "bash", Summary: "ok"}, st).Text()
	want = lineedit.NoteContinuation + "result bash ok"
	if got != want {
		t.Errorf("result row = %q, want %q", got, want)
	}
	// No dangling separator for a tool with no digest.
	got = termToolRow(agent.ToolEvent{Kind: agent.ToolEventKindCall, Name: "bash"}, st).Text()
	if got != lineedit.ToolGlyph+" call bash" {
		t.Errorf("call row without a digest = %q", got)
	}
}

// TestTermToolRowRunsCarryTheCommandAsOneRun pins the BAND seam's half of the
// copy contract: the run list the band draws holds the command as one run with
// no escape byte inside it, so the band's own cutter (which measures cells and
// steps over sequences) cuts the command at a cell boundary and never inside a
// sequence — and a mask applied per run cannot reach into the middle of one.
func TestTermToolRowRunsCarryTheCommandAsOneRun(t *testing.T) {
	cmd := `cd /home/nick/workspace/01a150dc && git log --oneline -3`
	row := termToolRow(agent.ToolEvent{Kind: agent.ToolEventKindCall, Name: "bash", Summary: cmd}, termToolStylesFor(lineedit.ColourOn))
	runs := termToolRuns(row, nil)
	if got := lineedit.NoteRunText(runs); got != row.Text() {
		t.Fatalf("the runs' text = %q, want the row's text %q", got, row.Text())
	}
	found := false
	for _, r := range runs {
		if r.Text == cmd {
			found = true
			// 本色 on a colour run: the payload maps to the monochrome style, so
			// the band writes it without a sequence — which is exactly what
			// "the terminal's own foreground" costs.
			if !r.Style.Empty() {
				t.Errorf("the command run is not 本色: %q", r.Style.Open())
			}
		}
		if strings.ContainsRune(r.Text, 0x1b) {
			t.Fatalf("a run carries an escape byte: %q", r.Text)
		}
	}
	if !found {
		t.Fatalf("the command is not one contiguous run: %#v", runs)
	}
	// The mask is applied per run and reaches the command.
	masked := termToolRuns(row, func(s string) string { return strings.ReplaceAll(s, "/home/nick", "<HOME>") })
	if got := lineedit.NoteRunText(masked); strings.Contains(got, "/home/nick") {
		t.Fatalf("the mask did not reach the command: %q", got)
	}
}
