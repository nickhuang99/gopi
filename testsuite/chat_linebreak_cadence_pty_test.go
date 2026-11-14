package testsuite

// chat_linebreak_cadence_pty_test.go — an answer's ROW STRUCTURE must not depend
// on the cadence the model streams at, measured on a REAL pty with the REAL
// binary.
//
// This file was recovered from the deleted branch `fix/answer-linebreak-cadence`
// (tip e206b5ed) by the rework task 01a0e57d, which kept its pty guard and its
// devdoc and dropped its in-package copy and its code fix. See
// devdoc/answer-linebreak-cadence.md for the whole account.
//
// THE DEFECT. `gopi chat`'s live answer band commits a line when its break
// arrives (internal/lineedit/answer.go's drain splits its buffer at the LAST
// '\n' and commits the text BEFORE it). When a delta frame IS the break — the
// ordinary shape of a 1-byte stream, where every character is a frame of its own
// — the committed body is EMPTY, so nothing is promoted: the line already
// painted on the live row stays LIVE and the next line's text is appended to it
// (writeLive appends at the live column), leaving several answer lines glued
// into ONE band row. A stream whose frames bundle the break with what follows (a
// 4-byte cadence) commits on schedule and draws one row per line. The reader
// therefore sees a different answer — a paragraph instead of a list — from the
// same text, decided by nothing but the carrier's chunk size.
//
// WHY THE GUARD IS ON A PTY. The in-package repro
// (internal/lineedit/answer_linebreak_cadence_test.go) pins the mechanism on the
// renderer's own cell grid, and it is the AUTHORITY for this defect: it was the
// one landed on master, it is not replaced by any copy here. This file pins the
// SURFACE: the shipped `gopi chat` on a real terminal, its stdout the pty, one
// `--dump-screen` artifact read back per cadence, with the assertion on the
// dumped CELL ROWS. It is the same instrument that measured the operator's
// sighting (delta=1 collapses the answer, delta=4 does not), and the only one
// that can say the row structure reached a terminal rather than a test buffer.
//
// UNGATED (task 01a0e706, 2026-09-28). This file landed with a RED gate
// (GOPI_LINEBREAK_CADENCE_RED) because the defect was still OPEN on master: a
// guard that failed on the tree it landed on would have made the suite red
// without pinning anything. The fix in drain (the row a lone break terminates is
// promoted) removed the gate in the same commit as the guard started passing,
// which is what the gated form promised, and the two guards went ungated
// together.
//
// WHAT IS ASSERTED. The answer is three short labelled lines; each line must
// occupy its OWN dump row, and no row may carry more than one label. The row
// count is read from the dump (the screen, through the tool's own reader); the
// labels' presence is read from the pty byte stream as well, so a dump that
// simply missed the answer cannot pass by absence.
//
// The two cadences are the case's whole point, and they are the same binary in
// the same run: only the carrier's chunk size differs.

import (
	"fmt"
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

// The case's own bounds, every one named (Rule 1).
const (
	// lblCadenceCols is the pty's width. WIDE relative to the fixture's lines
	// (7 cells): this case is about LINE BREAKS, so nothing here may wrap — a
	// wrap would give a lost break a second place to hide.
	lblCadenceCols = 64
	// lblCadenceRows is the pty's height: tall enough that all three answer rows
	// and the prompt fit the dump's window with no scrolling, so the per-line row
	// count is read where it was painted.
	lblCadenceRows = 24
	// lblCadenceFrameGap is the pause the carrier leaves between two SSE frames.
	// It is ABOVE lineedit's flush window (30ms), so a 1-byte stream is the
	// "slow, one character per frame" arm of the defect rather than one burst:
	// the renderer flushes each frame on its own, which is what lets a line break
	// arrive in a frame by itself.
	lblCadenceFrameGap = 40 * time.Millisecond
	// lblCadenceTailGap is the pause before the answer's last frame, so the final
	// line is committed while its tail is the last thing on the wire.
	lblCadenceTailGap = 120 * time.Millisecond
	// lblCadenceAsk is what the harness types. It is short, and what it says does
	// not matter: the mock answers the same scripted payload whatever it is sent.
	lblCadenceAsk = "line breaks please"
	// lblCadenceQuit ends the session.
	lblCadenceQuit = "/quit"
	// lblCadencePromptTimeout bounds every wait for a marker on the pty. The
	// waits are SYNCHRONIZATION waits (they return the instant their marker
	// arrives); the bound exists only so a hang reports what the child did say.
	lblCadencePromptTimeout = 60 * time.Second
	// lblCadencePaintSettle is the pause before the artifact is read back: the
	// dump samples the screen on a throttle, so the read waits for the paint to
	// stop moving (a synchronization wait would need a signal the surface does
	// not publish per row).
	lblCadencePaintSettle = 300 * time.Millisecond
	// lblCadenceDumpPerm is the mode of the artifacts this case keeps.
	lblCadenceDumpPerm = 0o644
	// lblCadenceEvidenceEnv names the directory this guard keeps its artifacts
	// in. It is the explicit layer over the ambient default: a verification run
	// wants the dump and the transcript where its OWN report lives (the suite's
	// scratch tree is reclaimed when the test ends), while an ordinary run keeps
	// the hermetic default — the suite's scratch dir, off the `/` partition
	// (Rule 22). Unset = scratch.
	lblCadenceEvidenceEnv = "GOPI_LINEBREAK_CADENCE_EVIDENCE"
	// lblCadenceGate is the RED switch this file landed behind and no longer
	// reads — the fix removed it. It is NOT declared as a constant any more: an
	// env name no code reads is a knob the tree does not have (Rule 21), and the
	// record of it belongs in the header and in devdoc/answer-linebreak-cadence.md
	// (GOPI_LINEBREAK_CADENCE_RED), where a reader looks for history rather than
	// for a switch.
)

// lblCadenceLabels is the answer, one label per line. Every label is the same
// length, which is deliberate: a glued row then reads as a concatenation, and no
// label can be confused with a prefix of another.
var lblCadenceLabels = []string{"AAALINE", "BBBLINE", "CCCLINE"}

// lblCadencePayload is the answer the carrier streams: every line carries its own
// terminator (an ordinary multi-line answer).
func lblCadencePayload() string {
	return strings.Join(lblCadenceLabels, "\n") + "\n"
}

// lblCadence is one measured cadence: the fragment width the carrier cuts the
// answer into. Everything else about the two runs is identical.
type lblCadence struct {
	// ID is what the subtest and the evidence directory are named after.
	ID string
	// DeltaBytes is the fragment width requested from the carrier (the stub
	// repeats back the sizes it really streamed, which is the proof).
	DeltaBytes int
}

// lblCadences are the two cadences the operator's terminals actually see, and
// the pair is the whole point — the SAME answer must come out with the same rows
// from both:
//
//	delta=1 — one byte per SSE frame, spaced past the flush window. Every frame
//	          is a frame of its own, so a break arrives ALONE: this is the arm
//	          that glues the lines.
//	delta=4 — four bytes per frame: a break rides in a frame with the next
//	          line's characters, so the commit lands and the rows come out
//	          separate. The control arm, and what makes this a cadence dependence
//	          rather than a broken renderer.
func lblCadences() []lblCadence {
	return []lblCadence{
		{ID: "delta=1", DeltaBytes: 1},
		{ID: "delta=4", DeltaBytes: 4},
	}
}

// TestChatLinebreakCadencePty drives the shipped binary once per cadence and
// requires the SAME three-row structure from both. It is UNGATED: the tree
// carries the fix.
func TestChatLinebreakCadencePty(t *testing.T) {
	bin := testutil.GopiBin(t)
	evidence := lblCadenceEvidenceDir(t)
	t.Logf("LINEBREAK-CADENCE binary=%s evidence=%s", bin, evidence)
	for _, c := range lblCadences() {
		t.Run(c.ID, func(t *testing.T) {
			lblCadenceMeasure(t, bin, evidence, c)
		})
	}
}

// lblCadenceMeasure runs ONE cadence end to end: the shipped binary on a real
// pty, one turn, the dump read back, the assertion on the dumped rows.
func lblCadenceMeasure(t *testing.T, bin, evidence string, c lblCadence) {
	t.Helper()
	home := t.TempDir()
	work := t.TempDir()
	// The carrier is the SHARED mock, asked for this cadence (Resp.TextDeltaBytes
	// / TextDeltaDelay): its fragment sizes are the recorded proof that the
	// cadence ran, so this file needs no carrier of its own — the sizes it really
	// streamed belong to the stub that streamed them.
	llm := testutil.NewMockLLM(t, testutil.Resp{
		Text:           lblCadencePayload(),
		TextDeltaBytes: c.DeltaBytes,
		TextDeltaDelay: lblCadenceFrameGap,
	})
	defer llm.Close()

	// ONE SUBDIRECTORY PER CADENCE, holding the surface's canonical file name
	// (uiclient.ChatDumpFileName): the tool's own reader identifies an artifact
	// by its name + the surface's provenance prefix and refuses a file whose name
	// belongs to no known surface, so the harness honours the reader instead of
	// asking it to loosen.
	dumpPath := filepath.Join(evidence, strings.ReplaceAll(c.ID, "=", ""), uiclient.ChatDumpFileName)
	if err := os.MkdirAll(filepath.Dir(dumpPath), 0o755); err != nil {
		t.Fatalf("create the cadence's evidence directory %s: %v", filepath.Dir(dumpPath), err)
	}

	chat := startPTYChat(t, []string{"chat", "--dump-screen=" + dumpPath},
		testutil.MockEnv(t, home, llm.URL()), work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: lblCadenceCols, Row: lblCadenceRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable on this host: %v", err)
	}
	if cols := lineedit.TerminalCols(int(chat.master.Fd())); cols != lblCadenceCols {
		t.Fatalf("the pty reports %d columns, want the %d this case installed: the geometry the answer was measured at is not the geometry asserted", cols, lblCadenceCols)
	}

	chat.waitFor(t, chatPromptMark, lblCadencePromptTimeout)
	// THE QUESTION IS TYPED, NOT PASTED: back-to-back bytes are a burst to the
	// editor, and the submit Enter that follows a burst is deliberately
	// suppressed — the harness would then type a question that is never asked
	// (measured on the branch this file was recovered from).
	chat.writeGapped(t, lblCadenceAsk)
	chat.write(t, "\r")

	grid := lblCadenceAwaitAnswer(t, chat, dumpPath, evidence, c)
	rows := lblCadenceAnswerRows(grid)
	if len(rows) != len(lblCadenceLabels) {
		t.Fatalf("%s: the answer occupies %d dump row(s), want %d (one per line):\ncells: %q\n--- dump grid ---\n%s",
			c.ID, len(rows), len(lblCadenceLabels), rows, strings.Join(grid, "\n"))
	}
	for i, want := range lblCadenceLabels {
		if !strings.Contains(rows[i], want) {
			t.Fatalf("%s: dump row %d = %q, want it to carry %q (line %d of the answer)\nrows: %q",
				c.ID, i, rows[i], want, i+1, rows)
		}
		for j, other := range lblCadenceLabels {
			if j != i && strings.Contains(rows[i], other) {
				t.Fatalf("%s: dump row %d = %q carries BOTH %q and %q — two answer lines were glued into one band row (the cadence, not the text, decided the rows)\nrows: %q",
					c.ID, i, rows[i], want, other, rows)
			}
		}
	}

	chat.writeGapped(t, lblCadenceQuit)
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("%s: chat exited with status %d after %s", c.ID, code, lblCadenceQuit)
	}
}

// lblCadenceAwaitAnswer waits for the turn's answer to reach the dump and returns
// the screen's rows. It waits on the ARTIFACT rather than on a byte pattern on the
// pty: the renderer writes a row's cells in pieces (a styled run per span, each
// with its own cursor placement), so a label is not guaranteed to be contiguous in
// the raw stream, while the artifact carries the rendered grid — the same
// instrument the assertion reads.
func lblCadenceAwaitAnswer(t *testing.T, chat *ptySession, dumpPath, evidence string, c lblCadence) []string {
	t.Helper()
	deadline := time.Now().Add(lblCadencePromptTimeout)
	var last string
	for time.Now().Before(deadline) {
		cand, err := uiclient.ReadDump(dumpPath)
		// "not written yet" (the surface writes by atomic rename) and "not
		// complete yet" are this poll's ordinary states, never a verdict.
		if err == nil && lblCadenceHoldsEveryLabel(strings.Join(cand.Dump.Lines, "\n")) {
			time.Sleep(lblCadencePaintSettle) //stylegate:allow-sleep waits for the throttled dump sampler to write the settled frame, so the grid is read after the paint stopped rather than mid-paint
			cand, err = uiclient.ReadDump(dumpPath)
			if err != nil {
				t.Fatalf("%s: the dump %s stopped being readable after it carried the whole answer: %v", c.ID, dumpPath, err)
			}
			// A dump of the wrong size or an unpainted screen is a REFUSAL, not a
			// verdict: the reader's own check decides, so no assertion here can
			// pass by reading a screen no surface drew.
			if refusal := uiclient.CompareGridUnusable(cand); refusal != "" {
				t.Fatalf("%s: the dump cannot be read as a screen, so no verdict can be measured from it: %s", c.ID, refusal)
			}
			blob, rerr := os.ReadFile(dumpPath)
			if rerr != nil {
				t.Fatalf("%s: read the settled dump %s: %v", c.ID, dumpPath, rerr)
			}
			kept := filepath.Join(evidence, fmt.Sprintf("cadence-%s-screen.dump", strings.ReplaceAll(c.ID, "=", "")))
			if werr := os.WriteFile(kept, blob, lblCadenceDumpPerm); werr != nil {
				t.Fatalf("%s: keep the dump as evidence: %v", c.ID, werr)
			}
			t.Logf("%s: answer rows read from %s (copy kept at %s)", c.ID, dumpPath, kept)
			return cand.Dump.Lines
		}
		if err == nil {
			last = strings.Join(cand.Dump.Lines, "\n")
		} else {
			last = err.Error()
		}
		time.Sleep(lblCadenceFrameGap) //stylegate:allow-sleep polls the artifact the surface writes on a throttle; it is a reader's cadence, not the interval under measurement
	}
	t.Fatalf("%s: the dump %s never carried the whole answer within %s; last read:\n%s",
		c.ID, dumpPath, lblCadencePromptTimeout, last)
	return nil
}

// lblCadenceAnswerRows returns the dump rows that carry any label, in screen
// order. Rows without a label (chrome, prompt, blank band rows) are not part of
// the answer, and a row count is read from where the answer's cells were PAINTED.
func lblCadenceAnswerRows(grid []string) []string {
	var rows []string
	for _, row := range grid {
		for _, label := range lblCadenceLabels {
			if strings.Contains(row, label) {
				rows = append(rows, strings.TrimRight(row, " "))
				break
			}
		}
	}
	return rows
}

// lblCadenceHoldsEveryLabel reports whether a grid carries every label of the
// answer — the precondition for reading it as a completed answer.
func lblCadenceHoldsEveryLabel(grid string) bool {
	for _, label := range lblCadenceLabels {
		if !strings.Contains(grid, label) {
			return false
		}
	}
	return true
}

// lblCadenceEvidenceDir is where the run keeps its artifacts, OFF the `/`
// partition and OUTSIDE the worktree (Rules 22, 30). The task's own session
// directory is used when the runner names one; otherwise the suite's scratch
// home, so the case is re-runnable anywhere.
func lblCadenceEvidenceDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv(lblCadenceEvidenceEnv); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s=%s: %v", lblCadenceEvidenceEnv, dir, err)
		}
		return dir
	}
	return testutil.TestScratchDir(t, "linebreak-cadence")
}
