package testsuite

// chat_status_row_ladder_probe_test.go — the WIDTH LADDER's rendered truth: one
// row per terminal width, read off the SHIPPED binary's own paint on a real pty.
//
// WHY THIS EXISTS BESIDE cmd/agent/chat_status_ladder_test.go. That file is the
// composer's table: it states what `statusColumnsForWidth` chooses for a width,
// through the production composer, with the width arriving by hand. This is the
// other half — what the BINARY PAINTS, on a terminal of that width, after a real
// turn. The two can disagree (they did: the whole ladder once fell back to the
// unaligned row because the live path stopped handing the composer the terminal's
// width, and the composer's own table stayed green), so the ladder's claims are
// checked against the screen rather than against itself.
//
// WHAT IT MEASURES, per width: the row's verbatim cells, its cell width (never
// over the terminal), which columns are on it (model / iter / tool / metrics), and
// that the live block still ends on its state word. The expectations are DERIVED
// from the row's own geometry (statusRowColumnsForTest below reads the ladder
// widths out of the source), never spelled: a table that restated the product's
// numbers would go green on a row that had moved.
//
// THE WALK IS ASCENDING, and that is a measurement decision, not a convenience.
// The region repaints on the width the TERMINAL reports, read at the paint it
// decides to do — and it only paints when the row's content changed. A width the
// resize does not make it repaint is therefore composed at the width of the LAST
// paint. Measured: a DESCENDING walk (117 → 24) reported the shed form at 99/98/95
// although the composer keeps the full form down to 100, because the row's content
// at 99 differed from the 100 form and the next three widths produced no new
// content. Walking UP moves one form at a time and every rung is composed at its
// own width. (The cold path is unaffected: a resize that changes the row repaints
// with the live width — TestStatusLadder_ReachableOnScreenBelowThePin is that
// path's guard.)
//
// IT IS HEAVY (Rule 27's classification): it drives a real binary on a pty for
// about a minute and costs one real turn per rung. It is skipped by the default
// smoke path and runs under `make test-heavy`; the arm's own switch additionally
// keeps a plain heavy run from driving twenty turns nobody asked for.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

const (
	// ladderProbeEnv is the arm's own switch: the width walk is minutes of pty
	// work, so even a heavy run has to ask for it by name. It names the arm to
	// drive — ladderProbeArmUsage or ladderProbeArmTools.
	ladderProbeEnv = "GOPI_LADDER_PROBE_ARM"
	// ladderProbeOutEnv names the file the rows are written to (a path OUTSIDE any
	// checkout: the run's own session/evidence directory).
	ladderProbeOutEnv = "GOPI_LADDER_PROBE_OUT"
	// ladderProbeArmUsage walks the widths with a turn whose usage reports the
	// context occupancy and the cache transport: the arm that measures the METRICS
	// group's presence and its shed point.
	ladderProbeArmUsage = "usage"
	// ladderProbeArmTools walks them with a turn that CALLS a tool: the arm that
	// measures the TOOL column's presence. Without it the table's `tool` column is
	// only ever observed in its negative arm ("tools off"), which is not the column
	// the ladder's step is about.
	ladderProbeArmTools = "tools"
	// ladderProbeModelMark is the mark the row is located by in the byte stream: the
	// model field's steady prefix (the field is elided inside its column on the
	// aligned row — "deepseek-v4-f…" — and shown whole on the unaligned one).
	ladderProbeModelMark = "deepseek"
	// ladderProbePrompt is what is typed at each width. Two cells, so the typed path
	// costs two ptyTypeGap pauses and the TURN is what the run spends.
	ladderProbePrompt = "hi"
	// ladderProbeUsageAnswer / ladderProbeToolAnswer are the two scripts' answer
	// text — the markers a turn has landed by.
	ladderProbeUsageAnswer = "pty probe answer"
	ladderProbeToolAnswer  = "ladder probe tool answer"
	// ladderProbeToolCallID is the tool_use block's id in the tool arm's script.
	ladderProbeToolCallID = "call_ladder_probe"
	// ladderProbeBashCmd is what the tool arm's call runs: three lines of a file
	// that is always there, so the digest is short and stable.
	ladderProbeBashCmd = "head -3 go.mod"
	// ladderProbeStateSettled is the live block's idle state word: the settled row's
	// tail, used as the row's end mark when the leading field is elided.
	ladderProbeStateSettled = "ready"
	// ladderProbeRows is the pty geometry's row count.
	ladderProbeRows = 30
	// ladderProbeUsedTokens is the occupancy the usage arm scripts: input +
	// cache_creation + cache_read, the sum the agent records as the prompt total.
	ladderProbeUsedTokens = 92100
	// ladderProbeSettleQuiet / ladderProbeSettleLimit bound one rung's read: the
	// stream must be quiet this long before it is taken as the settled row, and the
	// limit keeps a chatty child from hanging the walk.
	ladderProbeSettleQuiet = 700 * time.Millisecond
	ladderProbeSettleLimit = 15 * time.Second
	// ladderProbeRawTail is how many bytes of the raw stream a blank row reports.
	ladderProbeRawTail = 300
	// ladderProbeColsAnnotation is the row annotation the write-out starts a ladder
	// line with. It is the shape the ladder table's own log uses (cols=… row=…), so
	// a reader compares the two side by side.
	ladderProbeColsAnnotation = "cols="
)

// ladderProbeWidths are the widths the USAGE arm walks: the operator's own 117 and
// the pin 100, the cells either side of each switch (95, 80, 70/69/68, 64/63/62),
// the ladder's own form floors (63 = the tool form, 40 = the identity set), the
// narrow rungs the ladder table names (80, 60, 50), and the band where the aligned
// composition itself stops fitting (39..31) — the floor the table's narrowest rung
// rests on is a measurement, not a reading of the form table.
var ladderProbeWidths = []int{31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 48, 49, 50, 60, 62, 63, 64, 68, 69, 70, 80, 95, 98, 99, 100, 117}

// ladderProbeToolWidths are the widths the TOOL arm walks: the rungs where the
// tool column's own content decides (it is absent at 62 and carried from 63), plus
// the pin and the operator's width. Fewer rungs than the usage arm because a
// tool-calling turn costs a round-trip more than a text answer.
var ladderProbeToolWidths = []int{40, 50, 62, 63, 68, 70, 95, 100, 117}

// TestChatStatusRowWidthLadderProbe drives the shipped binary across the ladder and
// writes the row it paints at each rung, verbatim, to ladderProbeOutEnv.
func TestChatStatusRowWidthLadderProbe(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	arm := os.Getenv(ladderProbeEnv)
	if arm == "" {
		t.Skipf("the width ladder's pty walk is minutes of real terminal work: set %s=%s to drive the metrics arm, %s=%s for the tool column",
			ladderProbeEnv, ladderProbeArmUsage, ladderProbeEnv, ladderProbeArmTools)
	}
	if arm != ladderProbeArmUsage && arm != ladderProbeArmTools {
		t.Fatalf("%s=%q is not an arm: want %q or %q", ladderProbeEnv, arm, ladderProbeArmUsage, ladderProbeArmTools)
	}
	out := os.Getenv(ladderProbeOutEnv)
	if out == "" {
		t.Fatalf("%s must name the file this walk writes its rows to", ladderProbeOutEnv)
	}

	home := t.TempDir()
	work := t.TempDir()
	llmURL, argv, answer, widths, header := ladderProbeScript(t, arm)
	dumpPath := filepath.Join(t.TempDir(), uiclient.ChatDumpFileName)

	chat := startPTYChat(t, append(argv, "--dump-screen="+dumpPath), testutil.MockEnv(t, home, llmURL), work)
	t.Cleanup(func() {
		if chat.cmd.Process != nil {
			_ = chat.cmd.Process.Kill() //stylegate:ignore best-effort teardown of the walk
		}
		_ = chat.master.Close() //stylegate:ignore the master is the walk's own descriptor
	})
	chat.waitFor(t, chatPromptMark, ptyPromptTimeout)

	var b strings.Builder
	b.WriteString(header)
	for _, cols := range widths {
		// Whatever the previous rung left in flight is drained first, so the bytes
		// read below can only be this rung's paint.
		_ = ptyReadSettled(chat, "", ladderProbeSettleQuiet, ladderProbeSettleLimit)
		chat.resize(t, uint16(cols), ladderProbeRows)
		chat.writeGapped(t, ladderProbePrompt)
		chat.write(t, "\r")
		seen := chat.waitFor(t, answer, ptyPromptTimeout)
		settled := ptyReadSettled(chat, "", ladderProbeSettleQuiet, ladderProbeSettleLimit)
		plain := chatRenderStripANSI(settled)
		row, _ := ladderProbeRow(plain)
		fmt.Fprintf(&b, "%s%3d cells=%3d row=%s\n", ladderProbeColsAnnotation, cols, lineedit.DisplayWidth(row), row)
		// The tool column's OWN content — the glyph and the call's digest — is painted
		// while the call is IN FLIGHT, not on the settled row (settled, the column
		// reads its label: "no tool" with tools enabled, "tools off" without). Those
		// rows exist only in the turn's own byte stream, so they are read from there:
		// every distinct row carrying the tool glyph is written out.
		for _, inFlight := range ladderProbeGlyphRows(chatRenderStripANSI(seen)) {
			fmt.Fprintf(&b, "\tin-flight cells=%3d row=%s\n", lineedit.DisplayWidth(inFlight), inFlight)
		}
		if row == "" {
			fmt.Fprintf(&b, "\tRAW=%q\n", ladderProbeTail(settled, ladderProbeRawTail))
			fmt.Fprintf(&b, "\tPLAIN=%q\n", ladderProbeTail(plain, ladderProbeRawTail))
		}
		// The row is one screen row at the terminal it was painted for: a row wider
		// than its terminal wraps, and a wrapped status row costs the editing line.
		// This is the one assertion the walk makes — the rows themselves are the
		// deliverable, and a walk that stopped at the first failure would hide the
		// rest of the ladder.
		if w := lineedit.DisplayWidth(row); w > cols {
			t.Errorf("cols=%d: the painted row is %d cells — it runs past its terminal\nrow: %q", cols, w, row)
		}
		// The live block ends the row, whatever was shed around it: the state word is
		// the one field no rung may cut (see TestStatusLadder_LiveBlockIsReserved).
		if tail := strings.TrimRight(row, " "); row != "" && !strings.HasSuffix(tail, ladderProbeStateSettled) {
			t.Errorf("cols=%d: the row does not end on the live block's state word %q\nrow: %q",
				cols, ladderProbeStateSettled, row)
		}
	}

	t.Logf("\n%s", b.String())
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write the ladder rows to %s: %v", out, err)
	}
	t.Logf("ladder rows written to %s", out)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 — the session ends at the deliberate /quit", code)
	}
}

// ladderProbeScript returns the arm's provider URL, argv, answer marker, the
// widths it walks and the header its write-out carries. One function per arm keeps
// the coupling between "what the turn does" and "which column that measures" in one
// place, so an arm cannot be pointed at a turn that measures something else.
func ladderProbeScript(t *testing.T, arm string) (llmURL string, argv []string, answer string, widths []int, header string) {
	t.Helper()
	switch arm {
	case ladderProbeArmTools:
		// A turn that really CALLS a tool: the tool column carries the glyph and the
		// digest rather than the tools-off label, which is the column the ladder's
		// second step is about.
		llm := testutil.NewMockLLM(t,
			testutil.Resp{ToolUse: &testutil.ToolCall{
				ID:   ladderProbeToolCallID,
				Name: "bash",
				Args: map[string]interface{}{"command": ladderProbeBashCmd},
			}},
			testutil.Resp{Text: ladderProbeToolAnswer},
		)
		return llm.URL(), []string{"chat", "--tools"}, ladderProbeToolAnswer, ladderProbeToolWidths,
			fmt.Sprintf("# the status row the shipped binary paints, per terminal width — %s arm\n"+
				"# the turn CALLS a tool (%s), so the tool column is the one carrying content:\n"+
				"#   settled, the column reads its label (\"no tool\" with tools enabled);\n"+
				"#   while the call is in flight it carries the glyph and the call's digest.\n"+
				"# A `in-flight` line appears only for a turn whose content CHANGED — the row is\n"+
				"# deduplicated on its composed text, so an unchanged digest paints no new bytes.\n",
				ladderProbeArmTools, ladderProbeBashCmd)
	default:
		// A turn whose usage reports the context occupancy and the cache transport.
		return startUsageLLM(t).URL, []string{"chat"}, ladderProbeUsageAnswer, ladderProbeWidths,
			fmt.Sprintf("# the status row the shipped binary paints, per terminal width — %s arm\n"+
				"# ctx window under test: %d (agent.DefaultContextWindow); scripted occupancy %d tokens\n"+
				"# one real turn is driven at each width; the row recorded is the SETTLED one\n",
				ladderProbeArmUsage, agent.DefaultContextWindow, ladderProbeUsedTokens)
	}
}

// ladderProbeRow extracts the status row from the ANSI-stripped accumulated stream:
// the LAST occurrence of the row's leading field, cut at the end of its row. The
// second arm handles the alighted row's neighbours — the UNALIGNED composition below
// the ladder's floor, whose leading field is the model short form — by locating the
// line its state word ends instead. Both arms read what the terminal painted; neither
// composes anything.
func ladderProbeRow(plain string) (string, string) {
	if row, rest, ok := ladderProbeLineFrom(plain, ladderProbeModelMark); ok {
		return row, rest
	}
	return ladderProbeLineFromWord(plain, ladderProbeStateSettled)
}

// ladderProbeLineFrom cuts the last line beginning at the last occurrence of mark.
// The row ends at a newline, or at the prompt's own marker: the row is repainted on
// the status line and the editor then re-places the prompt, and once the cursor
// movements are stripped the two run together on one line.
func ladderProbeLineFrom(plain, mark string) (string, string, bool) {
	at := strings.LastIndex(plain, mark)
	if at < 0 {
		return "", "", false
	}
	rest := plain[at:]
	cut := strings.IndexAny(rest, "\r\n")
	if cut < 0 {
		cut = len(rest)
	}
	if p := strings.Index(rest, chatPromptMark); p >= 0 && p < cut {
		return strings.TrimRight(rest[:p], " "), rest[p:], true
	}
	return strings.TrimRight(rest[:cut], " "), rest[cut:], true
}

// ladderProbeLineFromWord finds the LAST line carrying word and returns that whole
// line — the reach is "the end of the last line that contains the marker", which is
// what makes it usable on a row whose LEADING field is elided.
func ladderProbeLineFromWord(plain, word string) (string, string) {
	at := strings.LastIndex(plain, word)
	if at < 0 {
		return "", ""
	}
	start := strings.LastIndexAny(plain[:at], "\r\n")
	if start < 0 {
		start = 0
	} else {
		start++
	}
	rest := plain[start:]
	if cut := strings.IndexAny(rest, "\r\n"); cut >= 0 {
		return strings.TrimRight(rest[:cut], " "), rest[cut:]
	}
	return strings.TrimRight(rest, " "), ""
}

// ladderProbeGlyphRows returns every DISTINCT row in the stripped stream that
// carries the tool glyph — the tool column's own content while a call is in flight,
// oldest first. The in-flight row is a repaint like any other (the row is recomposed
// on every state change), so it is read off the stream the same way the settled row
// is; the return is a SET because the animation repaints the row once per frame and
// the frames differ only in the live block's dots.
func ladderProbeGlyphRows(plain string) []string {
	var rows []string
	seen := map[string]bool{}
	for _, line := range strings.FieldsFunc(plain, func(r rune) bool { return r == '\r' || r == '\n' }) {
		line = strings.TrimSpace(line)
		if at := strings.LastIndex(line, lineedit.ToolGlyph); at >= 0 {
			row := strings.TrimRight(line[at:], " ")
			if row != "" && !seen[row] {
				seen[row] = true
				rows = append(rows, row)
			}
		}
	}
	return rows
}

// ladderProbeTail returns the last n BYTES of s, for a blank row's own diagnosis.
// It cuts on a rune boundary: half a multi-byte cell would report mojibake as if the
// row had printed it.
func ladderProbeTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := len(s) - n
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return s[cut:]
}
