package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sgr"
	"github.com/nick/gopi/internal/uiclient"
)

// chat_dump_test.go — `gopi chat --dump-screen`: the flag's artifact, and the
// PER-CELL BOLD MASK the reserved answer band actually paints.
//
// WHY THE BOLD MASK IS ASSERTED HERE AND NOT ONLY IN internal/lineedit.
// internal/lineedit's emphasis_test.go pins the tokenizer and the renderer's own
// drawing. What it cannot pin is the REPL's WIRING: that the stream the chat turn
// renderer produces (chatTurnRenderer.Write → region.Delta → the editor's paint
// stream → lineedit.Screen → the dump) carries the mask a reader of the artifact
// has to trust. So the expected mask is derived from the SOURCE LINE's marker
// positions (the independent side, using lineedit.BoldMarker and the same
// arithmetic) and compared column by column against the mask the dump carries —
// only the row's offset is taken from the dump, because the turn's gutter sits
// to the left of the text and assuming column 0 would assert the layout instead
// of the emphasis.
//
// THREE MECHANISMS, because the epic names three, and they are three different
// code paths in internal/lineedit/answer.go:
//
//	half-row   — rowForPartial: an UNFINISHED line, painted with its markers
//	             intact and WITHOUT emphasis (the decision is deferred, not
//	             guessed); it is decided once its break arrives;
//	fence/diff — rowFor's composer gate: inside a fence the line is CODE, so its
//	             `**` is the literal the code block says it is and no cell is
//	             bold, while the same text after the fence is prose again;
//	multi-bold — a complete line carrying several spans, decided once on the
//	             whole line.

// chatDumpProbe is one dumped chat run: the sink, the artifact's path, the screen
// state the reader recovered from it, and the field word internal/sgr renders
// Bold under (read from that package, never typed in an assertion).
type chatDumpProbe struct {
	dumper   *uiclient.ScreenDumper
	path     string
	screen   lineedit.ScreenDump
	cand     uiclient.Candidate
	boldWord string
}

// boldFieldWord returns the word internal/sgr renders when — and only when —
// Bold is set. It is read off that package's OWN field table (the surface the
// per-cell assertion is decided on) rather than typed at a comparison, so a
// renamed attribute cannot leave these assertions silently green on an empty
// mask: the candidates are whatever sgr renders for a DEFAULT style and for the
// same style with Bold turned on, and the word whose presence tracks the flag is
// the one to match. A package that stops rendering bold as a field FAILS here by
// name instead of matching on an empty string (Rule 15b).
func boldFieldWord(t *testing.T) string {
	t.Helper()
	base := sgr.Default()
	on := base
	on.Bold = true
	off := map[string]bool{}
	for _, f := range base.Fields() {
		off[f] = true
	}
	for _, f := range on.Fields() {
		if !off[f] {
			return f
		}
	}
	t.Fatalf("internal/sgr renders no field for Bold (base %q, bold %q): the per-cell bold assertion has nothing to match on",
		base.String(), on.String())
	return ""
}

// newChatDumpProbe arms a chat dump sink at a fresh path and drives one turn of
// deltas through the REAL chat turn renderer with the editor's paint stream teed
// into it — the harness production uses, plus the artifact.
//
// The tee is chatDumpWriter(rec, dumper), exactly what newChatInputWithDumper
// installs, so every byte the layout draws reaches the sink as well as the
// recording writer the grid assertions use.
func newChatDumpProbe(t *testing.T, deltas ...string) *chatDumpProbe {
	t.Helper()
	return newChatDumpProbeWithPalette(t, lineedit.BasicPalette, deltas...)
}

// The freeze gate's knobs. chatDumpQuietWindow is how long the paint stream must
// be SILENT before the frame it produced can be frozen — it is taken from the
// renderer's OWN cadence constant (chatWordFlushWindow, how long the answer
// renderer holds a word before flushing it), doubled, so the quiet window can
// never be shorter than one producer step. chatDumpSettleBudget bounds the whole
// wait and chatDumpSettleStep is its poll interval: a budget for a GATE, never a
// sleep.
const (
	chatDumpQuietWindow  = 2 * chatWordFlushWindow
	chatDumpSettleBudget = 5 * time.Second
	chatDumpSettleStep   = 2 * time.Millisecond
)

// chatDumpFrameHolds is the gate's content criterion, read off the SINK'S OWN grid
// (never a second oracle — a replay of the same bytes into another screen turned out
// to be fed differently, measured 2026-09-26, and an oracle that disagrees with the
// artifact about bytes they were both given cannot decide when the artifact is
// right).
//
// Two legs:
//
//   - the line the turn last wrote is ON the grid, in either spelling the layout
//     legitimately draws it in (raw: an unfinished line is painted with its markers
//     intact; or resolved by lineedit's own tokenizer). This is the leg that catches
//     the "answer never reached the artifact" failures (measured 2026-09-26:
//     TestChatDump_MultiBoldAnswerMarksExactlyItsSpans and the QuotedFence case).
//   - the row the editor's PROMPT owns (anchored at column zero — the editing line
//     starts at the screen's left edge, while every answer row opens with the turn's
//     gutter) does not DUPLICATE a line already on the band. That duplicate is the
//     measured defect: the artifact frozen mid-paint painted the answer's tail a
//     second time onto the prompt's row (`you> + added **d**`), and because the
//     probe's textRowOf returns the LAST matching row, the assertion read the
//     prompt's 4 bold cells as the diff line's cells — the whole-package failure.
//     Only a DUPLICATE is refused, because a frame may legitimately paint other text
//     there (TestChatDump_QuotedFenceMarkerKeepsTheProseProse settles with
//     `you>  follows` and asserts on that very grid).
func chatDumpFrameHolds(d *uiclient.ScreenDumper, want string) bool {
	snap := d.Snapshot()
	grid := strings.Join(snap.Grid, "\n")
	if !strings.Contains(grid, want) {
		return false
	}
	prompt := strings.TrimRight(chatPrompt, " ")
	for _, row := range snap.Grid {
		if !strings.HasPrefix(row, prompt) {
			continue
		}
		text := strings.TrimSpace(row[len(prompt):])
		if text == "" {
			continue
		}
		for _, other := range snap.Grid {
			if other != row && strings.Contains(other, text) {
				return false
			}
		}
	}
	return true
}

// chatDumpFrameJustifies is the spelling-independent form of the content leg: the
// try list holds the accepted spellings of the line the turn last wrote (raw and
// marker-resolved), and the grid must carry ANY of them.
func chatDumpFrameJustifies(d *uiclient.ScreenDumper, try []string) bool {
	if len(try) == 0 {
		return true
	}
	grid := strings.Join(d.Snapshot().Grid, "\n")
	for _, want := range try {
		if want != "" && strings.Contains(grid, want) {
			return chatDumpFrameHolds(d, want)
		}
	}
	return false
}

// chatDumpFrameSettled is the wait. It requires TWO facts, both measured on the
// sink itself, because the sink is the only surface the assertions read:
//
//   - STABILITY: the sink's grid has not changed for chatDumpQuietWindow, taken
//     from the renderer's own cadence constant (chatWordFlushWindow, how long the
//     answer renderer holds a word before flushing it) and doubled, so a quiet
//     window can never be shorter than one producer step. The sink is fed from the
//     editor's read loop, so this is the fact that says that loop has finished
//     handing over the turn's paint.
//   - CONTENT: chatDumpFrameJustifies — the turn's last line is on the grid and the
//     prompt's row does not duplicate a band line.
//
// Stability alone cannot be trusted (the loop can pause longer than the window), and
// content alone cannot either (a mid-paint frame already carries the text on the
// wrong row) — which is why both are required, and why the failure names both.
func chatDumpFrameSettled(d *uiclient.ScreenDumper, try []string, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	last, quiet := "", time.Duration(0)
	for {
		if grid := strings.Join(d.Snapshot().Grid, "\n"); grid == last {
			quiet += chatDumpSettleStep
		} else {
			last, quiet = grid, 0
		}
		if quiet >= chatDumpQuietWindow && chatDumpFrameJustifies(d, try) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(chatDumpSettleStep) //stylegate:allow-sleep polling the sink's own grid, bounded by the caller's budget (a wait, not a fixed sleep)
	}
}

// WHY THIS IS A LAG TEST AND NOT AN ABSOLUTE, recorded here so a future reader does
// not "strengthen" the gate into a hang:
//
// the probe ALREADY waits for the turn's last line in the artifact and fails by
// name when it is absent ("the answer never reached the artifact"). Making this
// gate demand that same line absolutely would convert a pre-existing, separately
// rooted failure into a 5s timeout here — and there IS such a failure, measured
// 2026-09-26 at -count=80: the sink froze `▌   b c` where the band row is
// `  a b c`, i.e. two cells of the row were blanked. That is NOT a freeze-timing
// artifact, and it is NOT two writers either: the paint stream's two PAINTERS
// (the turn goroutine's answer renderer and the editor's read loop) reached the
// fan-out's two consumers — the terminal and the dump sink — in DIFFERENT
// ORDERS, so the sink interpreted a byte order the terminal never saw. That
// order defect is fixed in `chatDumpWriter` (chatPaintWriter serializes one
// paint write), and the deterministic regression for it is
// `TestChatDumpSinkSeesThePaintStreamInTheTerminalsOrder` in
// cmd/agent/chat_dump_writer_order_test.go: the artifact's grid must describe
// the same screen as a replay of the recorded stream. Read that test — and the
// devdoc's "SECOND ORDER DEFECT" section — before re-theorizing this one.
// waitForChatDumpFrame waits for that settled frame and FAILS BY NAME if it never
// arrives.
//
// The wait is not a convenience: the sink is fed from the EDITOR'S READ LOOP (the
// tee on the paint stream), so the probe's own Write/Finish calls return while
// paints are still in flight. Freezing the sink before they land captures a grid
// the stream never finished producing, and every row index an assertion then reads
// belongs to that frame — which is how this file's assertions failed only in a
// whole-package run (measured 2026-09-26; the reproduction and the frame it froze
// are in this change's report and devdoc).
//
// A budget that expires FAILS rather than proceeding (Rule 15b, no silent
// fallback).
func waitForChatDumpFrame(t *testing.T, d *uiclient.ScreenDumper, deltas []string) {
	t.Helper()
	try := chatProbeWaitAlternates(deltas...)
	if chatDumpFrameSettled(d, try, chatDumpSettleBudget) {
		return
	}
	t.Fatalf("the dump sink never settled within %s: its grid did not stay unchanged for %s, or it does not carry %s, or it duplicates a band line on the row the editor's prompt owns (the measured 2026-09-26 shape). Every row index an assertion reads would belong to a frame the turn did not draw.\n--- sink ---\n%s",
		chatDumpSettleBudget, chatDumpQuietWindow, try, d.Snapshot().Text())
}

// newChatDumpProbeWithPalette is the probe with the colour table chosen by the
// caller: the red proof installs the MONOCHROME table, so the same text is drawn
// with no emphasis at all and the mask collapses — which is what shows the
// per-cell assertion is sensitive to the emphasis rather than to the characters.
func newChatDumpProbeWithPalette(t *testing.T, pal lineedit.Palette, deltas ...string) *chatDumpProbe {
	t.Helper()
	path := filepath.Join(t.TempDir(), uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: path,
		Cols: completionCols,
		Rows: completionRows,
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              os.Getpid(),
			Target:           chatDumpTarget(false),
			ColsRows:         [2]int{completionCols, completionRows},
			TerminalColsRows: [2]int{completionCols, completionRows},
			Started:          time.Now(),
		},
	})
	boldWord := boldFieldWord(t)
	if err != nil {
		t.Fatalf("arm the chat dump sink: %v", err)
	}
	h := newStreamHarnessDumped(t, d, pal)
	if band := h.in.Answer(); band == nil {
		t.Fatal("no answer band on a terminal that hosts the layout")
	}
	renderer := newChatTurnRenderer(h.in)
	if !renderer.Reserved() {
		t.Fatal("the turn renderer did not take the reserved path")
	}
	renderer.BeginTurn(1, "a question")
	for _, delta := range deltas {
		renderer.Write(delta)
	}
	renderer.Finish()

	// The artifact is synchronized by Close, not by a poll: the sink's refresh is
	// throttled (100ms) and its frames are written from the editor's read loop, so
	// the only moment the file is guaranteed to hold the last painted frame is the
	// freeze — which is exactly what a run's teardown does. Waiting on the file
	// first would be waiting for a write that Close is about to make anyway.
	//
	// The producer must be STOPPED before the freeze, and "stopped" has to be a
	// fact the harness measures rather than a duration the test waits out.
	// stopReadLoop ends the editor's read loop and reports whether it returned.
	//
	// A quiet-window wait alone cannot establish this, and the measurement is
	// unambiguous: this harness drives the answer renderer off a test clock whose
	// word-flush window is 2s (chatTestEpoch + chatWordFlushWindow), so a 60ms
	// quiet window is satisfied over and over while the band is still being
	// painted — a whole-package stress run then froze `▌ a b` (the turn's last
	// line, half written) and reported "the dumped grid does not carry a b c —
	// the answer never reached the artifact", or held a grid that never went quiet
	// at all. Waiting cannot fix a moving target; stopping the producer can.
	if !h.stopReadLoop() {
		t.Fatal("the editor's read loop never stopped, so the artifact cannot be frozen deterministically")
	}

	// The FREEZE is what makes Close correct, so it must not be taken while that
	// read loop is still painting: a frame captured mid-paint is a grid the layout
	// never drew. waitForChatDumpFrame is that wait — it refuses such a frame by
	// name instead of handing it to an assertion.
	waitForChatDumpFrame(t, d, deltas)
	if err := d.Close(); err != nil {
		t.Fatalf("close the chat dump sink: %v", err)
	}
	cand, err := uiclient.ReadDump(path)
	if err != nil {
		t.Fatalf("read back the chat dump %s: %v", path, err)
	}
	if cand.Frames <= uiclient.UnpaintedFrames {
		t.Fatalf("the dump recorded frames=%d: the layout's paint stream never reached the sink", cand.Frames)
	}
	// The callers' own wait: the LAST line of the stream must be in the artifact
	// before a test asserts on it, so the probe refuses to hand back a dump that
	// is one frame behind the turn it describes.
	if want := chatProbeWaitText(deltas...); want != "" && !strings.Contains(strings.Join(cand.Dump.Grid, "\n"), want) {
		t.Fatalf("the dumped grid does not carry %q — the answer never reached the artifact:\n%s",
			want, strings.Join(cand.Dump.Grid, "\n"))
	}
	return &chatDumpProbe{dumper: d, path: path, screen: cand.Dump, cand: cand, boldWord: boldWord}
}

// grid renders the artifact's grid for a failure message.
func (p *chatDumpProbe) grid() string { return strings.Join(p.screen.Grid, "\n") }

// textRowOf finds the dumped row whose grid text contains want, or -1. The row is
// located FROM THE DUMP (never from re-derived layout arithmetic): a reader of the
// artifact asks the artifact where things are.
func (p *chatDumpProbe) textRowOf(want string) int {
	for i, row := range p.screen.Grid {
		if strings.Contains(row, want) {
			return i
		}
	}
	return -1
}

// rowsContaining returns every dumped row index whose text contains want.
func (p *chatDumpProbe) rowsContaining(want string) []int {
	var out []int
	for i, row := range p.screen.Grid {
		if strings.Contains(row, want) {
			out = append(out, i)
		}
	}
	return out
}

// chatProbeWaitAlternates is the accepted spellings of the line the turn last wrote:
// the line with its emphasis resolved by lineedit's own tokenizer, plus the RAW line,
// because an unfinished line is painted with its markers intact (rowForPartial's
// documented behaviour, asserted by TestChatDump_HalfRowPaintsItsMarkersLiterally).
// A wait that demanded only the resolved spelling would hang on a legitimate
// provisional frame (measured 2026-09-26).
func chatProbeWaitAlternates(deltas ...string) []string {
	var raw string
	for _, line := range strings.Split(strings.Join(deltas, ""), "\n") {
		if s := strings.TrimRight(line, "\r"); strings.TrimSpace(s) != "" {
			raw = s
		}
	}
	if raw == "" {
		return nil
	}
	return []string{chatProbeWaitText(raw + "\n"), raw}
}

// chatProbeWaitText is the text the wait looks for in the artifact: the last
// non-empty line of the WHOLE delta stream with its emphasis resolved the way the
// tokenizer resolves it — which is what the grid draws. The whole stream, not the
// last delta: a marker pair may close ACROSS two deltas (the half-row's `**bo` +
// `ld**`), and a wait keyed on the last delta alone would look for a marker that
// the deciding frame consumed.
//
// It uses lineedit's own tokenizer (LineSpans/SpanText), never a second marker
// parser (Rule 14). An UNCLOSED marker stays literal there, which is exactly what
// the renderer draws for an unfinished line — so both frame shapes are covered by
// one rule.
func chatProbeWaitText(deltas ...string) string {
	stream := strings.Join(deltas, "")
	var last string
	for _, line := range strings.Split(stream, "\n") {
		if s := strings.TrimRight(line, "\r"); strings.TrimSpace(s) != "" {
			last = s
		}
	}
	if last == "" {
		return ""
	}
	if strings.Contains(last, lineedit.BoldMarker) {
		if text := lineedit.SpanText(lineedit.LineSpans(last)); text != "" {
			return text
		}
	}
	return last
}

// boldMask is the per-cell bold mask of one dumped row, read from the dump's OWN
// attribute runs — the structure a reader of the file consumes (the text dump's
// attrs_begin section, the JSON twin's attr_runs). A cell no run covers is not
// bold: it carries the default style.
func (p *chatDumpProbe) boldMask(row int) []bool {
	mask := make([]bool, p.screen.Cols)
	if row < 0 || row >= len(p.screen.AttrRuns) {
		return mask
	}
	for _, run := range p.screen.AttrRuns[row] {
		if !strings.Contains(run.Style, p.boldWord) {
			continue
		}
		for c := run.Col; c < run.Col+run.Cells && c < len(mask); c++ {
			mask[c] = true
		}
	}
	return mask
}

// expectBoldMask derives the mask a line MUST paint from the marker positions in
// the line itself: each CLOSED pair of lineedit.BoldMarker spans exactly the cells
// between them, and an unclosed marker is literal (it occupies cells). This is the
// independent side of the assertion — it reads the SOURCE text, not the renderer's
// output.
func expectBoldMask(text string, width int) []bool {
	mask := make([]bool, width)
	marker := lineedit.BoldMarker
	col := 0
	for i := 0; i < len(text); {
		if strings.HasPrefix(text[i:], marker) {
			end := strings.Index(text[i+len(marker):], marker)
			if end < 0 {
				col += len([]rune(marker))
				i += len(marker)
				continue
			}
			span := []rune(text[i+len(marker) : i+len(marker)+end])
			for j := range span {
				if col+j < width {
					mask[col+j] = true
				}
			}
			col += len(span)
			i += len(marker) + end + len(marker)
			continue
		}
		col++
		i++
	}
	return mask
}

// requireMarkedSpans is the per-cell assertion in the ROW's own coordinates: it
// locates the drawn text inside the dumped row, then compares the dumped mask with
// the marker-derived one column by column.
//
// sourceLine is the line as the model sent it (markers included); drawnText is what
// a reader must see on the grid (a consumed marker is not a cell).
func requireMarkedSpans(t *testing.T, what string, p *chatDumpProbe, row int, sourceLine, drawnText string) {
	t.Helper()
	rowText := p.screen.Grid[row]
	off := runeIndex(rowText, drawnText)
	if off < 0 {
		t.Fatalf("%s: the drawn text %q is not on row %d = %q\n--- dump ---\n%s", what, drawnText, row, rowText, p.grid())
	}
	want := expectBoldMask(sourceLine, p.screen.Cols)
	got := p.boldMask(row)
	for c := 0; c < p.screen.Cols; c++ {
		w := c-off >= 0 && c-off < len(want) && want[c-off]
		if got[c] != w {
			t.Fatalf("%s: column %d bold = %t, want %t (the text starts at column %d)\nrow %d = %q\nsource line = %q\nattrs row %d = %+v",
				what, c, got[c], w, off, row, rowText, sourceLine, row, p.screen.AttrRuns[row])
		}
	}
}

// runeIndex returns the RUNE index of needle in haystack, or -1. A byte index
// would be wrong here: the turn's gutter glyph is three bytes and one cell, and a
// column IS a rune index (the grid's own contract).
func runeIndex(haystack, needle string) int {
	h, n := []rune(haystack), []rune(needle)
	if len(n) == 0 || len(n) > len(h) {
		return -1
	}
	for i := 0; i+len(n) <= len(h); i++ {
		if string(h[i:i+len(n)]) == needle {
			return i
		}
	}
	return -1
}

// countTrue counts the set cells of a mask.
func countTrue(mask []bool) int {
	n := 0
	for _, v := range mask {
		if v {
			n++
		}
	}
	return n
}

// maskEqual reports whether two masks agree over their common length.
func maskEqual(a, b []bool) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestChatDump_ArtifactCarriesTheBandAndItsIdentity is the flag's own acceptance
// at the artifact level: the sink writes `chat-screen.dump` (the name
// `gopi ui_state` scans for) with the CHAT provenance prefix and this process's
// own target, the reader recovers the chat surface from that name, and the grid
// carries the layout's rows.
func TestChatDump_ArtifactCarriesTheBandAndItsIdentity(t *testing.T) {
	p := newChatDumpProbe(t, "the answer body\n")

	if got := filepath.Base(p.path); got != uiclient.ChatDumpFileName {
		t.Fatalf("dump file = %q, want %q (the name gopi ui_state scans for)", got, uiclient.ChatDumpFileName)
	}
	blob, err := os.ReadFile(p.path)
	if err != nil {
		t.Fatalf("read %s: %v", p.path, err)
	}
	first := strings.SplitN(string(blob), "\n", 2)[0]
	if !strings.HasPrefix(first, uiclient.ChatDumpRecordPrefix) {
		t.Fatalf("the dump's provenance line is %q, want the chat marker %q", first, uiclient.ChatDumpRecordPrefix)
	}
	if !strings.Contains(first, "target="+chatTargetSelf) {
		t.Fatalf("the provenance line %q does not name this process's own target (%q)", first, chatTargetSelf)
	}
	if p.cand.Surface != uiclient.SurfaceChat {
		t.Fatalf("the reader recovered surface %q from %s, want %q", p.cand.Surface, p.path, uiclient.SurfaceChat)
	}
	if len(p.screen.AttrRuns) != completionRows {
		t.Fatalf("the dump carries %d attribute rows, want %d (the grid's height, one style run list per row)",
			len(p.screen.AttrRuns), completionRows)
	}
	if len(p.screen.BlindSpots) != len(p.screen.BlindSpotIDs()) || len(p.screen.BlindSpots) == 0 {
		t.Fatalf("the dump carries %d blind spot(s): the reader's own bounds must be listed", len(p.screen.BlindSpots))
	}
	if row := p.textRowOf("the answer body"); row < 0 {
		t.Fatalf("the dumped grid does not carry the answer:\n%s", p.grid())
	}
	if _, err := os.Stat(p.dumper.JSONPath()); err != nil {
		t.Fatalf("the machine-checkable twin is missing: %v", err)
	}
}

// TestChatDump_HalfRowPaintsItsMarkersLiterally drives an UNFINISHED line — the
// rowForPartial mechanism — and asserts on the dumped grid that NO cell is bold
// and the markers are still drawn: the decision is deferred, not guessed, because
// text a reader can see may not change its letters when the next frame arrives.
func TestChatDump_HalfRowPaintsItsMarkersLiterally(t *testing.T) {
	const half = "a " + lineedit.BoldMarker + "bo"
	p := newChatDumpProbe(t, half)
	row := p.textRowOf(half)
	if row < 0 {
		t.Fatalf("the unfinished line %q is not on the dumped grid:\n%s", half, p.grid())
	}
	if mask := p.boldMask(row); countTrue(mask) != 0 {
		t.Fatalf("the half-row marks %d bold cell(s), want 0 (an unfinished line is painted with its markers and no emphasis)\nrow %d = %q",
			countTrue(mask), row, p.screen.Grid[row])
	}
}

// TestChatDump_HalfRowDecidesWhenTheBreakArrives is the other half of the same
// mechanism, and it is stated as a comparison of the TWO frames the artifact
// recorded: the unfinished line is drawn WITH its markers and no emphasis (the
// first probe), and the instant its break arrives the same line is drawn with the
// markers CONSUMED — they occupy no column — and exactly the span's cells bold
// (the second probe). The letters the reader saw first are still there; only the
// markers stop being cells, which is why the decision cannot change a visible
// letter.
func TestChatDump_HalfRowDecidesWhenTheBreakArrives(t *testing.T) {
	const (
		half   = "a " + lineedit.BoldMarker + "bo"
		source = "a " + lineedit.BoldMarker + "bold" + lineedit.BoldMarker
		drawn  = "a bold"
	)
	// Frame 1 — unfinished: the markers are on the grid, no cell is bold.
	open := newChatDumpProbe(t, half)
	openRow := open.textRowOf(half)
	if openRow < 0 {
		t.Fatalf("the unfinished line %q is not on the dumped grid:\n%s", half, open.grid())
	}
	if mask := open.boldMask(openRow); countTrue(mask) != 0 {
		t.Fatalf("the unfinished frame marks %d bold cell(s), want 0\nrow %d = %q", countTrue(mask), openRow, open.screen.Grid[openRow])
	}

	// Frame 2 — decided: one row, no marker, exactly the span bold.
	p := newChatDumpProbe(t, half, "ld"+lineedit.BoldMarker+"\n")
	rows := p.rowsContaining(drawn)
	if len(rows) != 1 {
		t.Fatalf("the decided line must be drawn once; found on rows %v:\n%s", rows, p.grid())
	}
	row := rows[0]
	if strings.Contains(p.screen.Grid[row], lineedit.BoldMarker) {
		t.Fatalf("the decided row still draws a marker: %q (a consumed marker occupies no column)", p.screen.Grid[row])
	}
	want := expectBoldMask(source, p.screen.Cols)
	if got := countTrue(want); got != len("bold") {
		t.Fatalf("the expectation itself is wrong: %d bold cells for %q", got, source)
	}
	requireMarkedSpans(t, "decided half-row", p, row, source, drawn)
	// The line's own letters survive the decision: the decided frame's body is the
	// unfinished frame's body with the two marker pairs resolved away.
	if got, wantText := runeIndex(p.screen.Grid[row], drawn), "a bold"; got < 0 {
		t.Fatalf("the decided row %q does not carry %q", p.screen.Grid[row], wantText)
	}
}

// TestChatDump_FenceAndDiffMarkersStayLiteral is the composer gate through the
// real chat renderer: inside a fence `**` is the literal the code block says it is
// (no cell bold, fence and diff lines alike), and the SAME text after the fence is
// prose again — where the pair marks exactly its span.
func TestChatDump_FenceAndDiffMarkersStayLiteral(t *testing.T) {
	fence := lineedit.FenceMarker
	body := "a " + lineedit.BoldMarker + "b" + lineedit.BoldMarker + " c"
	added := "+ added " + lineedit.BoldMarker + "d" + lineedit.BoldMarker
	p := newChatDumpProbe(t, fence+"\n", body+"\n", added+"\n", fence+"\n", body+"\n")

	// The fenced line: the markers are still there as TEXT (they are the literal
	// the code block says they are) and no cell is bold.
	fencedRow := p.textRowOf(body)
	if fencedRow < 0 {
		t.Fatalf("the fenced line is not on the dumped grid:\n%s", p.grid())
	}
	if mask := p.boldMask(fencedRow); countTrue(mask) != 0 {
		t.Fatalf("a fenced line marks %d bold cell(s), want 0 (inside a fence the marker is the literal the code block says it is)\nrow %d = %q\nattrs row %d = %+v",
			countTrue(mask), fencedRow, p.screen.Grid[fencedRow], fencedRow, p.screen.AttrRuns[fencedRow])
	}
	addRow := p.textRowOf("+ added")
	if addRow < 0 {
		t.Fatalf("the diff line is not on the dumped grid:\n%s", p.grid())
	}
	if mask := p.boldMask(addRow); countTrue(mask) != 0 {
		t.Fatalf("a diff line inside a fence marks %d bold cell(s), want 0\nrow %d = %q",
			countTrue(mask), addRow, p.screen.Grid[addRow])
	}
	// The prose row: the SAME text after the closing fence, drawn without its
	// markers and with exactly the marked span bold. It is located by the drawn
	// text (the fenced row cannot carry it — its markers occupy those cells).
	const drawn = "a b c"
	proseRow := p.textRowOf(drawn)
	if proseRow < 0 || proseRow == fencedRow {
		t.Fatalf("the prose line after the fence is not on the dumped grid (found row %d):\n%s", proseRow, p.grid())
	}
	want := expectBoldMask(body, p.screen.Cols)
	if got := countTrue(want); got != 1 {
		t.Fatalf("the expectation itself is wrong: %d bold cells for %q", got, body)
	}
	requireMarkedSpans(t, "prose line after a fence", p, proseRow, body, drawn)
}

// TestChatDump_MultiBoldAnswerMarksExactlyItsSpans is the full multi-bold answer
// through the same path: several spans in one line, a line with no marker at all
// (zero bold cells), and the per-column comparison on the finished grid.
func TestChatDump_MultiBoldAnswerMarksExactlyItsSpans(t *testing.T) {
	const plain = "no marker on this line"
	multi := lineedit.BoldMarker + "alpha" + lineedit.BoldMarker + " and " +
		lineedit.BoldMarker + "beta" + lineedit.BoldMarker + " and " +
		lineedit.BoldMarker + "gamma" + lineedit.BoldMarker
	p := newChatDumpProbe(t, plain+"\n", multi+"\n")

	plainRow := p.textRowOf(plain)
	if plainRow < 0 {
		t.Fatalf("the markerless line is not on the dumped grid:\n%s", p.grid())
	}
	if mask := p.boldMask(plainRow); countTrue(mask) != 0 {
		t.Fatalf("a markerless line marks %d bold cell(s), want 0\nrow %d = %q",
			countTrue(mask), plainRow, p.screen.Grid[plainRow])
	}

	const drawn = "alpha and beta and gamma"
	multiRow := p.textRowOf(drawn)
	if multiRow < 0 {
		t.Fatalf("the multi-bold line is not on the dumped grid:\n%s", p.grid())
	}
	if strings.Contains(p.screen.Grid[multiRow], lineedit.BoldMarker) {
		t.Fatalf("the multi-bold row still draws markers: %q", p.screen.Grid[multiRow])
	}
	want := expectBoldMask(multi, p.screen.Cols)
	if got := countTrue(want); got != len("alpha")+len("beta")+len("gamma") {
		t.Fatalf("the expectation itself is wrong: %d bold cells for %q", got, multi)
	}
	requireMarkedSpans(t, "multi-bold answer", p, multiRow, multi, drawn)
	// The marker cells are ZERO cells: a consumed marker occupies no column, so
	// the drawn BODY is shorter than the source line by exactly the markers that
	// were consumed — and no marker character survives on the row at all.
	rowText := strings.TrimRight(p.screen.Grid[multiRow], " ")
	if n := strings.Count(rowText, lineedit.BoldMarker); n != 0 {
		t.Fatalf("the multi-bold row draws %d marker character(s): %q", n, p.screen.Grid[multiRow])
	}
	off := runeIndex(p.screen.Grid[multiRow], drawn)
	body := len([]rune(rowText)) - off
	src := len([]rune(multi))
	if body != len([]rune(drawn)) {
		t.Fatalf("the multi-bold row's body is %d cells for %q (%d runes), want %d",
			body, drawn, len([]rune(drawn)), len([]rune(drawn)))
	}
	if consumed := src - body; consumed != 3*2*len([]rune(lineedit.BoldMarker)) {
		t.Fatalf("the source line is %d runes and its drawn body %d: %d rune(s) consumed, want %d (three pairs of zero-width markers)",
			src, body, consumed, 3*2*len([]rune(lineedit.BoldMarker)))
	}
}

// TestChatDump_BoldMaskRedProofs turns the per-cell bold assertion RED on purpose,
// by breaking exactly the thing it claims to measure. With the MONOCHROME table
// installed (the one NO_COLOR selects — internal/lineedit's zero Palette,
// referenced, never re-spelled) the same text is drawn with identical characters
// and no SGR at all, so the dumped mask collapses to all-false while the
// marker-derived expectation still says "these columns are bold". If the assertion
// above could not see that difference it would be a satisfied assertion about
// nothing.
func TestChatDump_BoldMaskRedProofs(t *testing.T) {
	const (
		source = lineedit.BoldMarker + "alpha" + lineedit.BoldMarker + " plain"
		drawn  = "alpha plain"
	)
	want := expectBoldMask(source, completionCols)
	if countTrue(want) != len("alpha") {
		t.Fatalf("the red proof's own expectation is wrong: %d bold cells", countTrue(want))
	}

	// GREEN: the shipped palette emits bold, so the dumped mask equals `want`.
	green := newChatDumpProbe(t, source+"\n")
	greenRow := green.textRowOf(drawn)
	if greenRow < 0 {
		t.Fatalf("the bold line is not on the dumped grid:\n%s", green.grid())
	}
	requireMarkedSpans(t, "green: the shipped palette", green, greenRow, source, drawn)

	// RED: the same text, the monochrome table.
	red := newChatDumpProbeWithPalette(t, lineedit.MonoPalette, source+"\n")
	redRow := red.textRowOf(drawn)
	if redRow < 0 {
		t.Fatalf("the bold line is not on the red grid:\n%s", red.grid())
	}
	if got := strings.TrimRight(red.screen.Grid[redRow], " "); !strings.HasSuffix(got, drawn) {
		t.Fatalf("the red run drew %q, want the same characters as the green one (%q): the red proof would be comparing different text",
			got, drawn)
	}
	if maskEqual(red.boldMask(redRow), want) {
		t.Fatalf("RED PROOF FAILED: with colour off the dumped mask still equals the marker-derived expectation — the per-cell bold assertion is vacuous\nrow %d = %q",
			redRow, red.screen.Grid[redRow])
	}
	if got := countTrue(red.boldMask(redRow)); got != 0 {
		t.Fatalf("with colour off the dumped mask marks %d cell(s), want 0", got)
	}
}

// TestChatDump_FreezeHappensBeforeTheLayoutIsTakenDown is the regression guard
// for the defect the first LIVE pty run of this flag exposed: the dump was frozen
// by the REPL's deferred finalize, which runs AFTER leaveChat has closed the
// input — and closing the input takes the layout down (region.Reset, the
// scroll-region reset, the cursor show). Those are bytes of the EXIT, so the
// artifact described a blank terminal instead of the screen the operator read
// (measured: a real run wrote a grid of 24 empty rows, frames>0 and dumps>0, so
// every "the dump exists" check passed while the artifact said nothing).
//
// The fix is an ORDER, and this test asserts the order rather than the
// mechanism: after the close-out, the dump still carries the answer row. It is
// the smallest sample that separates the two orders — an equivalent full-REPL
// test would exercise the same freeze point through several more layers.
func TestChatDump_FreezeHappensBeforeTheLayoutIsTakenDown(t *testing.T) {
	const answer = "the answer survives the teardown"
	path := filepath.Join(t.TempDir(), uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: path, Cols: completionCols, Rows: completionRows,
		Identity: uiclient.DumpIdentity{
			Surface: uiclient.SurfaceChat, PID: os.Getpid(), Target: chatDumpTarget(false),
			ColsRows: [2]int{completionCols, completionRows}, TerminalColsRows: [2]int{completionCols, completionRows},
			Started: time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("arm the chat dump sink: %v", err)
	}
	// The sink is handed to the INPUT (that is what CloseOut freezes), so this
	// test drives the same object the run does.
	h := newStreamHarnessDumped(t, d, lineedit.BasicPalette)
	renderer := newChatTurnRenderer(h.in)
	renderer.BeginTurn(1, "a question")
	renderer.Write(answer + "\n")
	renderer.Finish()
	h.waitFor(t, "the answer reached the screen", func() bool {
		h.replay()
		return strings.Contains(h.screen.Contents(), answer)
	})

	// The REPL's close-out, in the REPL's order: the input is closed FIRST (that
	// is leaveChat's first act), the deferred dump finalize would come after.
	h.in.CloseOut()

	cand, err := uiclient.ReadDump(path)
	if err != nil {
		t.Fatalf("read back the chat dump %s: %v", path, err)
	}
	grid := strings.Join(cand.Dump.Grid, "\n")
	if !strings.Contains(grid, answer) {
		t.Fatalf("the frozen dump does not carry the answer — the artifact describes the TEARDOWN, not the screen:\n%s", grid)
	}
	if cand.Frames <= uiclient.UnpaintedFrames {
		t.Fatalf("the frozen dump reports frames=%d, want the painted stream", cand.Frames)
	}
}

// TestChatDump_QuotedFenceMarkerKeepsTheProseProse is the SAME composer gate on
// the shape the defect actually appeared in: an answer that quotes a screen
// capture, so the capture's own fence marker sits INDENTED inside the fence that
// quotes it (session a8553c96, 2026-09-26). The quoted marker is content, not the
// quoting block's delimiter — so the answer's closing paragraph is PROSE, drawn
// with its markers consumed and exactly its span's cells bold.
//
// It is the REPL's wiring that this asserts (chatTurnRenderer.Write → the editor's
// paint stream → the dump), on the real chat surface, with the mask read from the
// artifact a reader consumes rather than from a callback.
func TestChatDump_QuotedFenceMarkerKeepsTheProseProse(t *testing.T) {
	const (
		fence = "```"
		// quoted is the capture's own marker: the same three characters, indented
		// six cells inside the block that quotes them.
		quoted = "      " + fence
		// prose is the answer's closing paragraph: after the quoting block closes,
		// it must be drawn as prose with its emphasis applied.
		prose = "the **bold conclusion** follows"
		drawn = "the bold conclusion follows"
	)
	p := newChatDumpProbe(t,
		"here is what I saw:\n",
		fence+"\n",
		"you> head -3 go.mod\n",
		quoted+"\n",
		"module github.com/nick/gopi\n",
		fence+"\n",
		"\n",
		prose+"\n",
	)
	rows := p.rowsContaining(drawn)
	if len(rows) != 1 {
		t.Fatalf("the closing paragraph must be drawn once, without its markers; found rows %v\n--- dump ---\n%s", rows, p.grid())
	}
	row := rows[0]
	if strings.Contains(p.screen.Grid[row], lineedit.BoldMarker) {
		t.Fatalf("the closing paragraph still draws its markers: %q — the line after a quoted fence marker was classified as code",
			p.screen.Grid[row])
	}
	requireMarkedSpans(t, "the paragraph after a quoted fence marker", p, row, prose, drawn)
	// The quoted marker itself is CONTENT of the block, not a delimiter: the
	// capture's line is inside the fence and carries no emphasis.
	if q := p.textRowOf(quoted); q >= 0 {
		if mask := p.boldMask(q); countTrue(mask) != 0 {
			t.Fatalf("the quoted capture line marks %d bold cell(s), want 0\nrow %d = %q", countTrue(mask), q, p.screen.Grid[q])
		}
	}
}

// TestChatDumpFrameGateRefusesAMidPaintGrid is the regression for the ORDER
// DEPENDENCY a whole-package run measured (2026-09-26). The artifact was frozen
// while the editor's read loop was still painting, and the answer's tail was
// painted a second time onto the row the EDITOR's prompt owns ("you> +
// added **d**"). The probe's own `textRowOf` returns the LAST matching row, so the
// assertion measured the prompt's 4 bold cells as the diff line's bold cells — the
// whole-package failure verbatim.
//
// Both legs are exercised on a REAL sink, built the way the probe builds one:
//
//   - a grid that does not carry the turn's last line is refused;
//   - a grid that carries it but duplicates a band line on the prompt's row is
//     refused (the measured shape);
//   - a grid that carries it with a clean prompt row is ACCEPTED, so the gate is
//     not a blanket refusal that would break every case in this file.
func TestChatDumpFrameGateRefusesAMidPaintGrid(t *testing.T) {
	fence := lineedit.FenceMarker
	body := "a " + lineedit.BoldMarker + "b" + lineedit.BoldMarker + " c"
	added := "+ added " + lineedit.BoldMarker + "d" + lineedit.BoldMarker
	deltas := []string{fence + "\n", body + "\n", added + "\n", fence + "\n", body + "\n"}
	try := chatProbeWaitAlternates(deltas...)
	// The two accepted spellings are the DRAWN form of the stream's last line (the
	// markers consumed) and its RAW source form — for `a **b** c` those are
	// "a b c" and "a **b** c", and pinning them here is what stops a future edit
	// from quietly narrowing the gate to one spelling.
	if len(try) != 2 || try[0] != "a b c" || try[1] != body {
		t.Fatalf("the gate's accepted spellings are %q, want the drawn form %q plus its raw form %q", try, "a b c", body)
	}
	// A budget short enough to be INSIDE the quiet window: with the production
	// budget a grid would settle, and a refusal would be a refusal of a frame the
	// sink had already left.
	const withinTheWindow = 4 * time.Millisecond

	newSink := func(t *testing.T) *uiclient.ScreenDumper {
		t.Helper()
		d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
			Path: filepath.Join(t.TempDir(), uiclient.ChatDumpFileName),
			Cols: completionCols,
			Rows: completionRows,
			Identity: uiclient.DumpIdentity{
				Surface:          uiclient.SurfaceChat,
				PID:              os.Getpid(),
				Target:           chatDumpTarget(false),
				ColsRows:         [2]int{completionCols, completionRows},
				TerminalColsRows: [2]int{completionCols, completionRows},
				Started:          time.Now(),
			},
		})
		if err != nil {
			t.Fatalf("arm the sink: %v", err)
		}
		t.Cleanup(func() { _ = d.Close() }) //stylegate:ignore the regression's own sink teardown
		return d
	}

	// Leg 1 — the turn's last line is not on the grid.
	partial := newSink(t)
	if _, err := partial.Write([]byte(fence + "\r\n\r\n" + chatPrompt)); err != nil {
		t.Fatalf("feed the partial frame: %v", err)
	}
	if chatDumpFrameJustifies(partial, try) {
		t.Fatalf("the gate accepted a grid that does not carry the turn's last line %q:\n%s", body, partial.Snapshot().Text())
	}

	// Leg 2 — the measured shape: the band carries the line AND the prompt's row
	// duplicates it.
	split := newSink(t)
	if _, err := split.Write([]byte(body + "\r\n" + chatPrompt + body)); err != nil {
		t.Fatalf("feed the duplicated frame: %v", err)
	}
	if chatDumpFrameJustifies(split, try) {
		t.Fatalf("the gate accepted a grid duplicating a band line on the prompt's own row — the 2026-09-26 order dependency:\n%s", split.Snapshot().Text())
	}

	// Leg 3 — the settled frame is accepted, so the gate is not a blanket refusal.
	ok := newSink(t)
	if _, err := ok.Write([]byte(body + "\r\n" + chatPrompt)); err != nil {
		t.Fatalf("feed the settled frame: %v", err)
	}
	if !chatDumpFrameJustifies(ok, try) {
		t.Fatalf("the gate refused a settled grid, so every case in this file would fail on it:\n%s", ok.Snapshot().Text())
	}
	// And the STABILITY leg is what makes the gate a wait rather than a snapshot:
	// a grid that is still changing must not be accepted even though it already
	// carries the line.
	live := newSink(t)
	if _, err := live.Write([]byte(body + "\r\n" + chatPrompt)); err != nil {
		t.Fatalf("feed the live frame: %v", err)
	}
	if chatDumpFrameSettled(live, try, withinTheWindow) {
		t.Fatal("the gate accepted a grid before it had been quiet for the window")
	}
}
