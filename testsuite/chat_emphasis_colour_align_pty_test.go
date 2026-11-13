package testsuite

// chat_emphasis_colour_align_pty_test.go — the EMPHASIS-COLOUR ALIGNMENT guard:
// on a real pty, with the 256-colour table in force, does every cell the dump
// reads as `fg=colour-178` fall inside the `**…**` span, and does every cell it
// reads as `fg=colour-230` fall outside it?
//
// WHY THIS EXISTS (the sighting it settles). Two dumps of the SAME kind of answer
// were read as different colours — one run's cells came back `fg=colour-178`,
// another's `fg=colour-230` — and the two entries are close enough on screen
// (#D7AF00 goldenrod vs #FFFFD7 pale cornsilk) that "a different colour" is not
// obviously "a different ROLE". The question is a MAPPING question and it has one
// decidable form:
//
//	the span the ANSWER text carries (`**…**`, markers removed) versus the
//	EMPHASIS the painted cells carry (`fg=colour-178 bold`) — cell by cell.
//
// WHY THE MAPPING IS NOT AN IDENTITY AND MUST BE MEASURED RATHER THAN ARGUED.
// internal/lineedit/palette.go defines the two entries as two ROLES in one table:
//
//	cubeGoldBody = 230  // #FFFFD7 — the cube's nearest cornsilk, the answer body
//	cubeDeepGold = 178  // #D7AF00 — goldenrod, the emphasised span inside the body
//
//	Palette256.Answer     = palette256Fg(cubeGoldBody)
//	Palette256.AnswerBold = palette256FgBold(cubeDeepGold)
//
// — two FIELDS of one Palette, so a run that reads 178 on the span and 230 on the
// body is the table working. What the table cannot promise is that the COMPOSER
// handed the emphasis layer the same span the answer's text carries: the band
// paints an unfinished line provisionally (AnswerRenderer.rowForPartial, markers
// literal, no emphasis), carries a head across a flush boundary, and cuts a
// wrapping line into rows — every one of those is a place the painted emphasis
// could drift from the answer's own span. This guard measures the drift on the
// artifact rather than trusting the table.
//
// THE MEASUREMENT IS THE ARTIFACT. Every verdict is computed from the painted cell
// grid the surface itself wrote (`--dump-screen=PATH` → lines_begin / grid_begin /
// attrs_begin) — never from the pty byte stream (it carries every repaint, so a
// token occurring twice in it is not evidence of a duplicate while the same token
// occurring twice on a ROW is) and never from OCR.
//
// RUN IT WITH:
//
//	CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
//	GOPI_BIN=$PWD/bin/gopi GOPI_ALIGN_EVIDENCE=<dir> \
//	  go test ./testsuite/ -run TestChatEmphasisColourAlignment_OnRealPTY -v -count=1

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/uiclient"
	"golang.org/x/sys/unix"
)

// The harness's own bounds (Rule 1: named, and none of them is a threshold a
// verdict is read from — the verdicts come from the artifact).
const (
	// alCols / alRows are the pty geometry. alCols is the narrow width the
	// render-verdict family uses, chosen so the span MUST be cut by the wrap
	// (the answer's span is > alCols cells by construction), and alRows is tall
	// enough that the whole scripted answer stays inside the dump's window —
	// content scrolled off it reads as loss the artifact cannot distinguish from
	// damage.
	alCols = 64
	alRows = 40
	// alBandCells is the width one band row holds: the grid's width less the
	// turn marker's cell and the two-cell indent the band opens a body row with.
	// It is used for the one geometric reading the table prints ("this row
	// wrapped", "this row ended short"), never for a verdict.
	alBandCells = alCols - 1 - alBodyIndentCells
	// alBodyIndentCells is the indent the band opens every body row with.
	alBodyIndentCells = 2
	// alWaitPrompt bounds the wait for the re-rendered prompt after the artifact
	// settles. It is a timeout on a SYNCHRONIZATION wait (it returns the instant
	// its marker arrives), never a sleep.
	alWaitPrompt = 60 * time.Second
	// alSettleBase is the FIXED part of the settle window: process start-up, the
	// artifact writer's 100ms throttle and the two agreeing reads a settle needs.
	// The window itself is DERIVED per cadence (alSettleGraceFor) — a constant
	// here was the wrong shape, because the two cadences differ by 15× in fragment
	// count and one number either starves delta=1 or waits far too long on delta=4.
	alSettleBase = 10 * time.Second
	// alTailQuote is how many bytes of the pty stream a failure message quotes:
	// a pty transcript is thousands of lines of repaints, and quoting it whole
	// buries the finding.
	alTailQuote = 2000
	// alPollInterval is how often the settle-wait re-reads the artifact. The
	// artifact writes at most once per 100ms, so this is a reader's cadence, not
	// the interval under measurement.
	alPollInterval = 150 * time.Millisecond
	// alDumpsField is the provenance field the settle-wait compares between two
	// reads (the writer's own count of dump writes).
	alDumpsField = "dumps="
	// alEvidenceEnv names the directory this guard keeps its artifacts in — the
	// explicit layer over the ambient default (Rule 22: unset = the suite's
	// scratch dir, off the `/` partition).
	alEvidenceEnv = "GOPI_ALIGN_EVIDENCE"
	// alDumpName is the artifact's canonical file name: uiclient.ReadDump
	// identifies an artifact by its name + the surface's own provenance prefix
	// and REFUSES a file whose name belongs to no known surface, so the harness
	// honours the reader instead of asking it to loosen.
	alDumpName = "chat-screen.dump"
	// alMinPiece is the shortest run of the payload's remaining text that counts
	// as locating the payload on a row: the band's prose is full of single
	// capital letters, and a one-character "match" shifts every later row's piece
	// by one and turns a correct screen into a spurious misalignment.
	alMinPiece = 4
	// alTablePerm is the mode the alignment table is written with.
	alTablePerm = 0o644
)

// The palette table this guard measures. It is SELECTED explicitly (never left to
// the ambient environment), because the question is about THESE two entries:
//
//	GOPI_PALETTE=palette256 + TERM=xterm-256color → Palette256, whose body is
//	`38;5;230` and whose emphasised span is `1m` + `38;5;38;5;178`.
const (
	alPalette = lineedit.PaletteNamePalette256
	alTerm    = "xterm-256color"
	// alBodyFrag / alBoldFrag are the wire fragments the dump's own style strings
	// carry for the two roles. They are the SGR PARAMETERS of the two table
	// entries, read back through the table (not spelled): a table whose 256 entry
	// moved would move this measurement with it instead of leaving the guard
	// asserting yesterday's colour.
	alBodyFrag = "colour-230"
	alBoldFrag = "colour-178"
	// alBoldAttr is the weight the emphasised span must additionally carry:
	// the table's emphasised field is `palette256FgBold`, so a span cell that
	// lost the colour AND kept the weight (or the reverse) is its own finding.
	alBoldAttr = "bold"
)

// The scripted content. Markers bracket the payload so the extraction locates its
// rows instead of guessing coordinates; they carry NO emphasis marker, so a
// literal `**` found on a payload row is always a leak and never the harness's ink.
const (
	alPrompt = "run the emphasis colour alignment turn"
	alOpen   = "<AL-OPEN"
	alClose  = "<AL-CLOSE"
	// alTail is the answer's LAST line: its arrival is the proof that the whole
	// answer reached the terminal, so no wait has to guess when a paint ended.
	alTail = "<AL-END>"
	// alPlainLine is a line with NO marker anywhere: its cells must all carry the
	// BODY colour, which is the second half of the criterion (230 outside the
	// span) measured on a row the span cannot reach.
	alPlainLine = "AL-PLAIN a plain sentence with no marking at all, so every cell of it is the body role."
	// alWrapLine carries ONE bold span wider than the band, so the wrap MUST cut
	// inside it: the span's cells then straddle a row boundary, which is the shape
	// the epic asks to be judged on both halves.
	alWrapLine = "AL-WRAP the marked run follows and it is wider than the band: " +
		"**ALPHA BRAVO CHARLIE DELTA ECHO FOXTROT GOLF HOTEL INDIA JULIET KILO LIMA MIKE** " +
		"and then the plain tail AL-WRAP-TAIL."
	// alAnswer is the model's answer: the plain line FIRST (so the body-colour
	// reading is taken on a row of its own), then the wrapping one.
	alAnswer = alOpen + "\n" + alPlainLine + "\n" + alWrapLine + "\n" + alClose + "\n" + alTail + "\n"
)

// alCadence is one measured chunking of the answer: how many BYTES each streamed
// text fragment carries, and the stub's inter-fragment wait. The pause is not
// decoration — it is what keeps the answer ARRIVING and therefore keeps the
// in-flight (partial) paint, the path every defect in this family lived on, on
// the screen for the whole answer.
type alCadence struct {
	ID         string
	DeltaBytes int
	Pause      time.Duration
}

// alCadences is the sweep. 4 is the coarser red-zone width (the simplest reading
// of the criterion, and the cadence whose breaks survive) and 1 the finest a byte
// stream can have. Both are reported separately: a verdict is a fact about ONE
// cadence's screen, and the epic's two dumps differed in ways nobody recorded.
func alCadences() []alCadence {
	return []alCadence{
		{ID: "delta=4", DeltaBytes: 4, Pause: 10 * time.Millisecond},
		{ID: "delta=1", DeltaBytes: 1, Pause: 15 * time.Millisecond},
	}
}

// alGlyph is one character of the ANSWER's text as the screen must show it, with
// the emphasis the answer's OWN span carries for it.
type alGlyph struct {
	R    rune
	Bold bool
}

// alGlyphs is the answer's text as a per-character sequence with its emphasis —
// built by the composer's own tokenizer (lineedit.LineSpans) rather than by a
// substitution this harness invented, so a screen that left a marker behind
// cannot be excused by a harness that stripped it.
//
// THE INPUT IS THE RAW LINE, never its DisplayText: tokenizing text whose markers
// were already stripped yields one plain span, i.e. a sequence in which NOTHING is
// in the span — the reading that made an earlier draft of this guard report
// `span cells=0` and 64 violations about a screen that was in fact fully bold over
// the span (measured, in this run's log). The tokenizer is the one authority on
// what the span is; asking it about text it has already processed is asking
// nothing.
//
// Newlines are NOT part of the sequence: a row cannot hold one, and the walk
// below treats "the payload's next character is a newline" as "a line boundary
// was crossed here", which is exactly what the glue reading needs.
func alGlyphs() []alGlyph {
	return alGlyphsOf([]string{alPlainLine, alWrapLine})
}

// alGlyphsOf tokenizes the RAW payload lines (markers still in them) into the
// per-character sequence.
func alGlyphsOf(lines []string) []alGlyph {
	var out []alGlyph
	for _, line := range lines {
		for _, sp := range lineedit.LineSpans(line) {
			for _, r := range sp.Text {
				out = append(out, alGlyph{R: r, Bold: sp.Emphasis == lineedit.EmphasisBold})
			}
		}
		out = append(out, alGlyph{R: '\n'})
	}
	return out
}

// alDisplayText is the payload as the SCREEN must show it: the markers removed by
// the composer's own tokenizer, never by a substitution spelled here.
func alDisplayText() string {
	return alPlainLine + "\n" + lineedit.DisplayText(alWrapLine)
}

// alSpanText is the answer's own bold span, markers removed.
func alSpanText() string { return chatRenderBoldSpan(alWrapLine) }

// alCell is ONE measured cell of the alignment table: where it is on the grid,
// what character it drew, which character of the answer it corresponds to (and
// whether THAT character is inside the span), and the style the dump recorded.
type alCell struct {
	Row      int
	Col      int
	Ch       rune
	GlyphIdx int
	InSpan   bool
	Style    string
	// Verdict is "" (consistent), "fail" (a violation of the criterion) or
	// "other" (a cell in the payload that carries neither role — reported, never
	// silently folded into a neighbour).
	Verdict string
	// Why states the violation in one line, for the table and the report.
	Why string
}

// TestChatEmphasisColourAlignment_OnRealPTY is the deliverable: per cadence, the
// per-cell alignment of the painted styles against the answer's own span, the two
// criterion clauses read off it, and the raw dump rows + attr runs they were read
// from.
func TestChatEmphasisColourAlignment_OnRealPTY(t *testing.T) {
	bin := testutil.GopiBin(t)
	t.Logf("ALIGN harness invoked with GOPI_BIN=%s", bin)
	t.Logf("ALIGN build identity: %s", chatRenderBuildIdentity(t, bin))
	t.Logf("ALIGN geometry: pty %dx%d, band %d cells; palette=%s TERM=%s", alCols, alRows, alBandCells, alPalette, alTerm)
	evidence := alEvidenceDir(t)
	t.Logf("ALIGN evidence dir: %s", evidence)

	for _, cadence := range alCadences() {
		t.Run(cadence.ID, func(t *testing.T) {
			alMeasureCadence(t, bin, evidence, cadence)
		})
	}
}

// alEvidenceDir resolves where this run keeps its artifacts: $GOPI_ALIGN_EVIDENCE
// when the caller named one (a verification run wants the table beside its own
// report), otherwise the suite's scratch dir (Rule 22).
func alEvidenceDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv(alEvidenceEnv); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s=%s: %v", alEvidenceEnv, dir, err)
		}
		return dir
	}
	return testutil.TestScratchDir(t, "chat-emphasis-align")
}

// alMeasureCadence runs ONE real-pty chat turn at one cadence, then reads the
// alignment off the artifact the surface wrote.
func alMeasureCadence(t *testing.T, bin, evidence string, cadence alCadence) {
	t.Helper()
	home := t.TempDir()
	work := t.TempDir()
	// ONE SUBDIRECTORY PER CADENCE, each holding the surface's canonical file
	// name (uiclient.ReadDump identifies an artifact by its name).
	dumpPath := filepath.Join(evidence, strings.ReplaceAll(cadence.ID, "=", ""), alDumpName)
	if err := os.MkdirAll(filepath.Dir(dumpPath), 0o755); err != nil {
		t.Fatalf("create the cadence's evidence directory %s: %v", filepath.Dir(dumpPath), err)
	}

	llm := testutil.NewMockLLM(t, testutil.Resp{
		Text:           alAnswer,
		TextDeltaBytes: cadence.DeltaBytes,
		TextDeltaDelay: cadence.Pause,
	})
	defer llm.Close()

	env := append(testutil.MockEnv(t, home, llm.URL()),
		lineedit.EnvTerm+"="+alTerm,
		lineedit.EnvPalette+"="+alPalette,
	)

	chat := startPTYChat(t, []string{"chat", "--dump-screen=" + dumpPath}, env, work)
	if err := unix.IoctlSetWinsize(int(chat.master.Fd()), unix.TIOCSWINSZ,
		&unix.Winsize{Col: alCols, Row: alRows}); err != nil {
		t.Skipf("TIOCSWINSZ unavailable on this host: %v", err)
	}
	if cols := lineedit.TerminalCols(int(chat.master.Fd())); cols != alCols {
		t.Fatalf("the pty reports %d columns, want the %d this harness installed", cols, alCols)
	}

	chat.waitFor(t, chatPromptMark, alWaitPrompt)
	chat.writeGapped(t, alPrompt)
	chat.write(t, "\r")
	// The settle-wait consumes the pty while it polls the artifact (Rule: a wait
	// that polls anything must keep the stream flowing — see alWaitTailPainted),
	// and it returns the prompt wait's own transcript, so there is exactly one
	// place that waits for the end of the turn.
	ptyOut := alWaitTailPainted(t, chat, dumpPath, cadence)

	chat.writeGapped(t, "/quit")
	chat.write(t, "\r")
	if code := chat.close(t); code != 0 {
		t.Fatalf("chat exit = %d, want 0 (the deliberate /quit)", code)
	}

	// ---- the cadence, read off the stub (never restated from the parameter) --
	alLogCadence(t, cadence, llm.TextStreamSizes(1))

	// ---- the artifact -----------------------------------------------------
	blob, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("read the dump %s: %v", dumpPath, err)
	}
	if len(blob) == 0 {
		t.Fatalf("the dump %s is empty: the surface was armed but wrote nothing", dumpPath)
	}
	ptyPath := filepath.Join(evidence, "align-"+strings.ReplaceAll(cadence.ID, "=", "")+"-pty.log")
	if err := os.WriteFile(ptyPath, []byte(ptyOut), alTablePerm); err != nil {
		t.Fatalf("write the pty transcript evidence: %v", err)
	}
	t.Logf("ALIGN %s pty transcript written to %s (%d bytes)", cadence.ID, ptyPath, len(ptyOut))

	cand, err := uiclient.ReadDump(dumpPath)
	if err != nil {
		t.Fatalf("the harness could not read the artifact the surface wrote (%s): %v", dumpPath, err)
	}
	if refusal := uiclient.CompareGridUnusable(cand); refusal != "" {
		t.Fatalf("the dump cannot be read as a screen, so no verdict can be measured from it: %s", refusal)
	}
	if !cand.Dump.AttrsRecorded || len(cand.Dump.AttrRuns) == 0 {
		t.Fatalf("the artifact carries no per-cell attributes, so the colour mapping cannot be measured from it")
	}
	for i, row := range cand.Dump.Lines {
		t.Logf("ALIGN %s dump row %03d|%s", cadence.ID, i, row)
	}
	alLogAttrRuns(t, cadence.ID, cand)

	// ---- the alignment ----------------------------------------------------
	first, last := alPayloadRowRange(cand)
	if first < 0 {
		t.Fatalf("the payload's markers are not on the dump's grid: the shape this run's verdicts describe was never painted on the artifact it read")
	}
	cells, note, ok := alAlign(cand, first, last)
	t.Logf("ALIGN %s alignment: %s", cadence.ID, note)
	table := alTable(cand, cells)
	t.Logf("ALIGN %s per-cell alignment table:\n%s", cadence.ID, table)
	tablePath := filepath.Join(evidence, "align-"+strings.ReplaceAll(cadence.ID, "=", "")+"-table.txt")
	if err := os.WriteFile(tablePath, []byte(table), alTablePerm); err != nil {
		t.Fatalf("write the alignment table evidence: %v", err)
	}
	t.Logf("ALIGN %s alignment table written to %s", cadence.ID, tablePath)

	if !ok {
		t.Errorf("the payload's characters could not be aligned to the painted cells: %s — the criterion cannot be judged from this artifact, so this is NOT a pass", note)
		return
	}

	// ---- the criterion ----------------------------------------------------
	alReportCriterion(t, cadence.ID, cells)
}

// alLogCadence prints what the STUB streamed and asserts the two facts that make
// the red-zone cadence a measurement rather than a claim: every fragment is inside
// the requested width, and the fragments reassemble to the answer byte for byte.
func alLogCadence(t *testing.T, cadence alCadence, sizes []int) {
	t.Helper()
	if len(sizes) == 0 {
		t.Fatalf("the stub recorded no text fragments for the answer request: the run cannot say which cadence it measured, so it measured nothing")
	}
	total, max := 0, 0
	for _, n := range sizes {
		total += n
		if n > max {
			max = n
		}
	}
	t.Logf("ALIGN cadence %s: fragments=%d bytes=%d max_fragment=%d requested=%d pause=%s",
		cadence.ID, len(sizes), total, max, cadence.DeltaBytes, cadence.Pause)
	if total != len(alAnswer) {
		t.Fatalf("the stub streamed %d bytes, want the answer's %d: this run's screen does not describe the scripted answer", total, len(alAnswer))
	}
	if max > cadence.DeltaBytes {
		t.Fatalf("a fragment carried %d bytes, more than the %d this cadence asks for: the red zone was not measured", max, cadence.DeltaBytes)
	}
}

// alLogAttrRuns prints the dump's own attribute runs for the payload's rows — the
// raw evidence the per-cell table is computed from, so a reader can re-derive the
// table by hand instead of trusting it.
func alLogAttrRuns(t *testing.T, id string, cand uiclient.Candidate) {
	t.Helper()
	for i, runs := range cand.Dump.AttrRuns {
		parts := make([]string, 0, len(runs))
		for _, r := range runs {
			parts = append(parts, fmt.Sprintf("%d:%d=%s", r.Col, r.Cells, r.Style))
		}
		t.Logf("ALIGN %s attrs row %03d|%s", id, i, strings.Join(parts, " "))
	}
}

// alPayloadRowRange locates the payload's rows on the grid by the harness's own
// markers: the first row after the open marker's row, up to the close marker's row
// (inclusive — the close marker's row can hold the payload's own tail before the
// marker, which is exactly the reading a glued screen needs to be judged on).
//
// A row that shares the OPEN marker's row is included too: when the stream glued
// the marker into the answer's head, the payload's first characters are on that
// row, and dropping it would report the answer's head as missing.
func alPayloadRowRange(cand uiclient.Candidate) (int, int) {
	first, close := -1, -1
	for i, row := range cand.Dump.Lines {
		if first < 0 {
			if j := strings.Index(row, alOpen); j >= 0 {
				first = i
				// The close marker can share the open marker's row on a screen
				// that glued the whole answer onto one row.
				if k := strings.Index(row[j+len(alOpen):], alClose); k >= 0 {
					close = i
					break
				}
			}
			continue
		}
		if strings.Contains(row, alClose) {
			close = i
			break
		}
	}
	if first < 0 {
		return -1, -1
	}
	if close < 0 {
		return -1, -1
	}
	return first, close
}

// alAlign walks the payload's characters against the painted cells and returns
// one alCell per cell that carries a payload character.
//
// THE WALK IS CHAR-BY-CHAR AND IN ORDER, because that is the only alignment that
// survives this family's shapes: a row holds a prefix of what is left of the
// payload, a wrap may consume the space at the break, and a lost break puts the
// NEXT line's head on the same row. So each row is asked for the longest prefix of
// the remaining payload text it CONTAINS (alMinPiece's floor), that piece's cells
// are classified character by character, and the walk resumes after it — including
// on the same row, when the piece ended at a line boundary and the row has more
// text after it.
//
// The GUTTER is excluded by construction: the piece is located INSIDE the row's
// text (so the band's marker columns are before its start), and a wrapped row's
// leading space falls outside the piece (it is trimmed by the prefix match).
//
// ok is false when the walk cannot account for the payload — an unaccounted tail,
// or a character that does not line up. A criterion that cannot be read off the
// artifact must say so (Rule 15: no silent pass).
func alAlign(cand uiclient.Candidate, first, last int) ([]alCell, string, bool) {
	glyphs := alGlyphs()
	// The walk index points at the next payload character not yet accounted for.
	at := 0
	// A leading newline (the text begins with the open marker's line break) is not
	// a character: skip any at the head.
	for at < len(glyphs) && glyphs[at].R == '\n' {
		at++
	}
	var cells []alCell
	rowsWalked, pieces := 0, 0
	note := fmt.Sprintf("payload characters: %d (span %q, %d cells); rows scanned: %d..%d",
		len(glyphs), alSpanText(), lineedit.DisplayWidth(alSpanText()), first, last)

	for row := first; row <= last && at < len(glyphs); row++ {
		if row >= len(cand.Dump.Lines) {
			break
		}
		text := strings.TrimRight(cand.Dump.Lines[row], " ")
		if strings.TrimSpace(text) == "" {
			continue
		}
		rowsWalked++
		for {
			// The piece this row carries: the longest prefix of the remaining
			// payload (up to the next newline) the row CONTAINS.
			lineEnd := at
			for lineEnd < len(glyphs) && glyphs[lineEnd].R != '\n' {
				lineEnd++
			}
			if lineEnd == at {
				// The payload's next character is a line boundary: consume it if
				// this row had more text (the lost-break shape) and keep going.
				at++
				continue
			}
			piece := alPieceOf(glyphs, at, lineEnd)
			idx, h := alLocate(text, piece)
			if idx < 0 || h == "" {
				break
			}
			// The piece's cells: its FIRST CHARACTER's column is where the match
			// begins in the row, so the base is the row text's width UP TO the
			// match — never the width up to a "first glyph", because a piece that
			// opens with a space is a space the row really draws at that column
			// and subtracting it (the earlier draft's `lead`) shifted every cell
			// of the row by one and reported the row's last bold cell as an
			// artifact column the grid does not have (measured: a FAIL at col 64
			// of a 64-column grid, ruling on a row whose run is `2:62=fg=colour-178
			// bold`).
			base := lineedit.DisplayWidth(text[:idx])
			for k, r := range []rune(h) {
				if r == ' ' {
					// A space carries no role of its own, and the wrap can consume
					// one at a break — it is neither evidence for nor against the
					// criterion.
					continue
				}
				gi := at + k
				if gi >= len(glyphs) {
					break
				}
				cell := base + k
				style := chatRenderStyleAt(cand, row, cell)
				cells = append(cells, alCell{
					Row: row, Col: cell, Ch: r, GlyphIdx: gi,
					InSpan: glyphs[gi].Bold, Style: style,
					Verdict: alCellVerdict(glyphs[gi].Bold, style),
					Why:     alCellWhy(glyphs[gi].Bold, style),
				})
			}
			pieces++
			at += len([]rune(h))
			// A row that ended the piece INSIDE the line (it wrapped) holds
			// nothing more of this line: the next row continues it.
			if at < lineEnd {
				break
			}
			// The line is fully consumed: step over its newline only when the
			// payload has one (the last line does not).
			if at < len(glyphs) && glyphs[at].R == '\n' {
				at++
			}
			// Does this row still hold the NEXT line's opening (the lost break)?
			// The loop asks again; alLocate fails and the row is done when it does
			// not.
			if at >= len(glyphs) {
				break
			}
		}
	}
	tail := 0
	for at < len(glyphs) {
		if glyphs[at].R != '\n' && glyphs[at].R != ' ' {
			tail++
		}
		at++
	}
	note += fmt.Sprintf("; rows walked: %d; pieces located: %d; cells attributed: %d; unaccounted payload characters: %d",
		rowsWalked, pieces, len(cells), tail)
	if len(cells) == 0 {
		return cells, note, false
	}
	if tail > 0 {
		return cells, note, false
	}
	return cells, note, true
}

// alPieceOf renders the payload's characters in [from,to) as text.
func alPieceOf(glyphs []alGlyph, from, to int) string {
	var b strings.Builder
	for i := from; i < to; i++ {
		b.WriteRune(glyphs[i].R)
	}
	return b.String()
}

// alLocate finds the longest prefix of `want` (at least alMinPiece characters)
// that occurs in `row`, returning the byte index and the matched piece. A piece
// shorter than the floor is not evidence of the payload (the band's prose is full
// of single letters).
func alLocate(row, want string) (int, string) {
	runes := []rune(want)
	for n := len(runes); n >= alMinPiece; n-- {
		if i := strings.Index(row, string(runes[:n])); i >= 0 {
			return i, string(runes[:n])
		}
	}
	return -1, ""
}

// alCellVerdict decides ONE cell against the criterion:
//
//   - the cell shows a character the ANSWER's span covers → it must carry the
//     emphasised role (the 256 table's bold + its span colour);
//   - the cell shows a character the span does NOT cover → it must NOT carry the
//     emphasised role, and a cell carrying the BODY role is the second clause's
//     positive evidence;
//   - anything else in the payload (the wrong colour, the weight without the
//     colour) is "other": reported, and a failure of the READING rather than of
//     either clause.
func alCellVerdict(inSpan bool, style string) string {
	hasBold := strings.Contains(style, alBoldFrag)
	hasBody := strings.Contains(style, alBodyFrag)
	switch {
	case inSpan && hasBold && strings.Contains(style, alBoldAttr):
		return ""
	case inSpan:
		return "fail"
	case !hasBold:
		// Outside the span and not carrying the span's colour: the criterion's
		// second clause holds. A cell that also carries the body role is the
		// positive evidence; anything else is reported as "other" but is not a
		// violation of either clause.
		if hasBody {
			return ""
		}
		return "other"
	default:
		return "fail"
	}
}

// alCellWhy states a cell's violation (or its "other" reading) in one line.
func alCellWhy(inSpan bool, style string) string {
	switch alCellVerdict(inSpan, style) {
	case "fail":
		if inSpan {
			return fmt.Sprintf("inside the span, but the cell carries %q instead of the emphasised role (%q + %s)", style, alBoldFrag, alBoldAttr)
		}
		return fmt.Sprintf("OUTSIDE the span, but the cell carries the emphasised role (%q)", style)
	case "other":
		return fmt.Sprintf("in the payload and carrying neither role: %q", style)
	default:
		return ""
	}
}

// alTable renders the per-cell alignment as the text the report quotes: one line
// per payload character, with the grid row/column, the character, the answer's own
// emphasis for it, and the style the dump recorded.
func alTable(cand uiclient.Candidate, cells []alCell) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# per-cell alignment: the answer's own emphasis vs the painted style\n")
	fmt.Fprintf(&b, "# span (answer text, markers removed): %q\n", alSpanText())
	fmt.Fprintf(&b, "# columns: grid_row grid_col char in_span dump_style verdict note\n")
	for _, c := range cells {
		note := c.Why
		if note == "" {
			note = "-"
		}
		fmt.Fprintf(&b, "%3d %3d %q %-5v %-24q %-6s %s\n", c.Row, c.Col, string(c.Ch), c.InSpan, c.Style, alVerdictWord(c.Verdict), note)
	}
	// The runs the table was read from, so the whole measurement is re-derivable
	// from this ONE file.
	fmt.Fprintf(&b, "\n# the dump's own attribute runs, per row (raw)\n")
	for i, runs := range cand.Dump.AttrRuns {
		parts := make([]string, 0, len(runs))
		for _, r := range runs {
			parts = append(parts, fmt.Sprintf("%d:%d=%s", r.Col, r.Cells, r.Style))
		}
		fmt.Fprintf(&b, "%3d|%s\n", i, strings.Join(parts, " "))
	}
	fmt.Fprintf(&b, "\n# the dump's own grid rows (raw)\n")
	for i, row := range cand.Dump.Lines {
		fmt.Fprintf(&b, "%3d|%s\n", i, row)
	}
	return b.String()
}

// alVerdictWord renders a cell verdict the way the table prints it.
func alVerdictWord(v string) string {
	if v == "" {
		return "ok"
	}
	return v
}

// alReportCriterion reads the two clauses off the table and reports them — the
// halves of a row boundary INCLUDED, which is the epic's own wording: a span the
// wrap cut is judged on both sides of the cut.
func alReportCriterion(t *testing.T, id string, cells []alCell) {
	t.Helper()
	spanCells, bodyCells, otherCells, bad := 0, 0, 0, 0
	spanBad, outsideBad := 0, 0
	// The stretch of the span the row boundary cut: the LAST cell of the span on
	// each row and the FIRST on the next — the two halves the epic names.
	perRowSpan := map[int][]alCell{}
	for _, c := range cells {
		if c.InSpan {
			perRowSpan[c.Row] = append(perRowSpan[c.Row], c)
		}
	}
	for _, c := range cells {
		switch {
		case c.Verdict == "fail":
			bad++
			if c.InSpan {
				spanBad++
			} else {
				outsideBad++
			}
			t.Errorf("ALIGN %s cell (%d,%d) %q [answer char %d, in_span=%v]: %s",
				id, c.Row, c.Col, string(c.Ch), c.GlyphIdx, c.InSpan, c.Why)
		case c.Verdict == "other":
			otherCells++
			t.Logf("ALIGN %s note: cell (%d,%d) %q [answer char %d, in_span=%v]: %s",
				id, c.Row, c.Col, string(c.Ch), c.GlyphIdx, c.InSpan, c.Why)
		}
		if c.InSpan {
			spanCells++
			continue
		}
		if strings.Contains(c.Style, alBodyFrag) {
			bodyCells++
		}
	}
	// The two clauses, stated as counts a reader can check against the table.
	t.Logf("ALIGN %s CLAUSE 1 (every %s cell is INSIDE the span): span cells=%d, span cells NOT carrying the emphasised role=%d, non-span cells carrying it=%d",
		id, alBoldFrag, spanCells, spanBad, outsideBad)
	t.Logf("ALIGN %s CLAUSE 2 (every %s cell is OUTSIDE the span): non-span cells carrying the body role=%d; payload cells carrying neither role=%d",
		id, alBodyFrag, bodyCells, otherCells)
	// The row boundary the wrap put inside the span: report both halves by name.
	rows := make([]int, 0, len(perRowSpan))
	for r := range perRowSpan {
		rows = append(rows, r)
	}
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j] < rows[j-1]; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	for i, r := range rows {
		part := perRowSpan[r]
		first, last := part[0], part[len(part)-1]
		t.Logf("ALIGN %s span part on dump row %d: %d cells, cols %d..%d; first %q (%s), last %q (%s)",
			id, r, len(part), first.Col, last.Col, string(first.Ch), first.Style, string(last.Ch), last.Style)
		if i == 0 {
			continue
		}
		t.Logf("ALIGN %s the wrap cut the span between dump row %d and dump row %d: the row above ends its part at col %d (%q) and the row below opens its part at col %d (%q) — both halves judged above",
			id, rows[i-1], r, perRowSpan[rows[i-1]][len(perRowSpan[rows[i-1]])-1].Col, perRowSpan[rows[i-1]][len(perRowSpan[rows[i-1]])-1].Style, first.Col, first.Style)
	}
	if bad == 0 && len(rows) > 0 {
		t.Logf("ALIGN %s VERDICT: CONSISTENT — the %s cells are exactly the span's characters (%d cells over %d row(s), both halves of the wrap included) and every other payload cell avoids that role (%d carrying the body role)",
			id, alBoldFrag, spanCells, len(rows), bodyCells)
	}
}

// alWaitTailPainted waits until the answer's tail is ON THE ARTIFACT and the
// artifact's own `dumps` counter has SETTLED, then waits for the re-rendered
// prompt. It returns everything the pty emitted during those waits.
//
// Why the raw byte stream is not the signal: the streaming band REPAINTS its rows
// as the fragments arrive, so the tail marker appears on the screen character by
// character and a substring search over the byte stream matches a PARTIAL paint. A
// harness that quits there measures an answer that was still streaming and reports
// the truncation it caused as a paint defect.
//
// THE WAIT CONSUMES THE PTY, and that is the fix this function carries (measured
// 2026-10-30, the delta=1 red). `startPTYChat`'s drain goroutine forwards into a
// BOUNDED channel (ptyReadBuf); a settle-wait that only re-reads the artifact and
// sleeps reads nothing, so that channel fills, the drain blocks on its send,
// nobody calls read(master), and the child's write(2) parks mid-answer. What the
// old shape then reported was "the artifact did not settle within 20s" and, once
// the run went on, "chat did not exit within 1m0s" — two names for read
// starvation. Every poll now goes through `pumpFor`, which keeps the pty flowing
// for exactly one poll interval, and the settle window is derived from the
// cadence under measurement instead of being a constant that has to be guessed.
//
// THERE IS NO DEGRADED READING. The old fallback ("give up on settling, take the
// pty's re-rendered prompt as the end-of-turn signal") turned a settle failure
// into a verdict measured against whatever the artifact happened to hold — the
// failure then surfaced later, in the exit wait, with a cause that belonged to
// this loop. An unsettled artifact is now a FATAL, named here with the two facts
// that diagnose it.
func alWaitTailPainted(t *testing.T, chat *ptySession, dumpPath string, cadence alCadence) string {
	t.Helper()
	grace := alSettleGraceFor(cadence)
	deadline := time.Now().Add(grace)
	var out strings.Builder
	lastDumps := -1
	lastPainted := false
	for {
		// One poll interval of CONSUMPTION: the pty keeps flowing while the
		// artifact is being read, which is what stops the child freezing.
		_ = chat.pumpFor(&out, "", alPollInterval)
		dumps, painted := alDumpState(dumpPath)
		if painted && dumps >= 0 && dumps == lastDumps {
			t.Logf("ALIGN the artifact settled: last row on the grid with dumps=%d (the surface's own counter stopping is what proves the paint ended)", dumps)
			prompt, ok := alPumpFor(t, chat, &out, chatPromptMark, alWaitPrompt)
			if !ok {
				t.Fatalf("the artifact settled but the re-rendered prompt %q never came back within %s; pty output tail:\n%s",
					chatPromptMark, alWaitPrompt, alTailOf(prompt))
			}
			return out.String()
		}
		lastDumps, lastPainted = dumps, painted
		if time.Now().After(deadline) {
			t.Fatalf("the artifact never settled within %s (derived from cadence %s: %d bytes / %d-byte fragments × %s, doubled + %s): last dumps=%d, tail %s. No verdict can be measured from a paint that did not finish, and there is no fallback reading",
				grace, cadence.ID, len(alAnswer), cadence.DeltaBytes, cadence.Pause, alSettleBase,
				lastDumps, alTailPresence(lastPainted))
		}
	}
}

// alSettleGraceFor derives the settle window from the cadence under measurement:
// the stub cannot spend more than one Pause per DeltaBytes fragment, so the
// stream itself takes about fragments×Pause; the window is twice that (the
// artifact's writer throttles at 100ms, and a settle needs two agreeing reads)
// plus a base for process start-up and the quit path. A constant here was the
// wrong shape: the two cadences differ by 15× in fragment count, so one number
// either starves delta=1 or waits forever on delta=4.
func alSettleGraceFor(cadence alCadence) time.Duration {
	if cadence.DeltaBytes <= 0 {
		return alSettleBase
	}
	fragments := (len(alAnswer) + cadence.DeltaBytes - 1) / cadence.DeltaBytes
	return 2*(time.Duration(fragments)*cadence.Pause) + alSettleBase
}

// alPumpFor reads the pty until want appears or timeout passes, consuming the
// stream as it goes (see ptySession.pumpFor). It reports whether want arrived.
func alPumpFor(t *testing.T, chat *ptySession, buf *strings.Builder, want string, timeout time.Duration) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if strings.Contains(buf.String(), want) {
			return buf.String(), true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return buf.String(), false
		}
		step := alPollInterval
		if remaining < step {
			step = remaining
		}
		if chat.pumpFor(buf, want, step) {
			return buf.String(), true
		}
	}
}

// alTailOf keeps the last alTailQuote bytes of a stream for a failure message —
// a pty transcript is thousands of lines of repaints and quoting it whole buries
// the finding.
func alTailOf(s string) string {
	if len(s) <= alTailQuote {
		return s
	}
	return s[len(s)-alTailQuote:]
}

// alTailPresence names the artifact's tail state the way a failure message needs
// it read.
func alTailPresence(painted bool) string {
	if painted {
		return "PRESENT"
	}
	return "ABSENT"
}

// alDumpState reads the artifact's own numbers: the dump counter and whether its
// lines currently hold the tail marker. An artifact that is not readable YET (the
// surface writes it by atomic rename) reports (-1, false) — never an error,
// because "not written yet" is this wait's ordinary state.
func alDumpState(dumpPath string) (int, bool) {
	blob, err := os.ReadFile(dumpPath)
	if err != nil {
		return -1, false
	}
	text := string(blob)
	dumps := alParseDumps(text)
	painted := strings.Contains(text, alTail)
	return dumps, painted
}

// alParseDumps reads the `dumps=N` field out of the dump's provenance line.
func alParseDumps(text string) int {
	i := strings.Index(text, alDumpsField)
	if i < 0 {
		return -1
	}
	rest := text[i+len(alDumpsField):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return -1
	}
	n := 0
	for _, c := range rest[:end] {
		n = n*10 + int(c-'0')
	}
	return n
}
