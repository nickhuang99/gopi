package uiclient

// sgr_turn_test.go — the turn locator's own gates.
//
// WHAT IS PINNED, and why each one is the failure that matters:
//
//  1. THE ANCHOR IS THE RENDERER'S SHAPE. The stem the locator matches is derived
//     from lineedit.TurnHeader, so the test renders a header through the SAME
//     entry point and asserts the locator finds it — and asserts it finds it in a
//     frame that also contains prose about turns (`turn 58` in a prompt), which is
//     how a bare-number matcher reports a boundary that is not one.
//  2. REPAINTS DO NOT OPEN SPANS. A frame that repaints an OLDER turn's header is
//     chrome; the span list must stay in step with the numbers, strictly ascending.
//  3. NO MARKS IS NOT AN ERROR, AND IT IS NOT A SPAN EITHER. A recording that
//     predates numbered turns yields an empty list so the CALLER can refuse by
//     name with the range that would have worked.
//  4. THE COPY IS THE SPAN. The frames ReadSgrTurnFile returns are the frames the
//     locator named — asserted by content, not by count.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
)

// sgrTurnTestHeader is the renderer's own header row for a turn — the anchor the
// locator must find. It is composed by CALLING the renderer, so a change to the
// glyph, the spacing or the word reaches this fixture without an edit.
func sgrTurnTestHeader(turn int, prompt string) string {
	return lineedit.TurnHeader(lineedit.TurnInfo{Index: turn, Prompt: prompt}, 120)
}

// sgrTurnTestStream writes n frames into a real stream file and returns its path.
func sgrTurnTestStream(t *testing.T, frames ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "turn-locator"+ScreenStreamSuffix)
	s, err := OpenSgrStream(SgrStreamConfig{Path: path, Cols: 120, Rows: 24, Revision: "test"})
	if err != nil {
		t.Fatalf("OpenSgrStream: %v", err)
	}
	for _, f := range frames {
		if werr := s.Write([]byte(f)); werr != nil {
			t.Fatalf("SgrStream.Write: %v", werr)
		}
	}
	if cerr := s.Close(); cerr != nil {
		t.Fatalf("SgrStream.Close: %v", cerr)
	}
	return path
}

// TestSgrTurnLocatorFindsTheRenderersOwnHeader is the epic's acceptance at the
// package level: the anchor is found on the bytes the renderer really draws.
func TestSgrTurnLocatorFindsTheRenderersOwnHeader(t *testing.T) {
	path := sgrTurnTestStream(t,
		"\x1b[1;1Hyou> hello\r\n",
		"\x1b[22;1H"+sgrTurnTestHeader(58, "what is on my screen")+"\x1b[0m",
		"\x1b[22;1Hthe answer body\r\n",
		"\x1b[23;1Hyou> next\r\n",
		"\x1b[22;1H"+sgrTurnTestHeader(59, "and now")+"\x1b[0m",
		"\x1b[22;1Hsecond answer\r\n",
	)
	turns, err := LocateSgrTurns(path)
	if err != nil {
		t.Fatalf("LocateSgrTurns: %v", err)
	}
	if len(turns.Spans) != 2 {
		t.Fatalf("located %d turn(s) %v, want 2 (58 and 59)", len(turns.Spans), turns.Spans)
	}
	if turns.Spans[0].Turn != 58 || turns.Spans[0].StartFrame != 1 {
		t.Errorf("turn 58's span = %+v, want turn 58 starting at frame 1 (the frame that painted its header)", turns.Spans[0])
	}
	if turns.Spans[0].EndFrame != 4 {
		t.Errorf("turn 58 ends at frame %d, want 4 — a turn runs until the NEXT header, so its answer frame and the prompt after it belong to it", turns.Spans[0].EndFrame)
	}
	if turns.Spans[1].Turn != 59 || turns.Spans[1].StartFrame != 4 {
		t.Errorf("turn 59's span = %+v, want turn 59 starting at frame 4", turns.Spans[1])
	}
	if turns.Spans[1].EndFrame != turns.StreamFrames {
		t.Errorf("the newest turn ends at frame %d, want the stream's %d frames (it is still the last painting)", turns.Spans[1].EndFrame, turns.StreamFrames)
	}
	cur, ok := turns.Current()
	if !ok || cur.Turn != 59 {
		t.Errorf("Current() = (%+v, %v), want the newest located turn, 59", cur, ok)
	}
	if got := turns.RangeWords(); got != "58..59" {
		t.Errorf("RangeWords() = %q, want \"58..59\" — a refusal has to name the turns that would work", got)
	}
}

// TestSgrTurnLocatorIgnoresProseThatNamesATurn is the false-positive gate, and it
// is the reason a bare `turn <N>` match is not the criterion: THE PROMPT IS THE
// OPERATOR'S OWN TEXT, so it may contain the very word the locator looks for.
func TestSgrTurnLocatorIgnoresProseThatNamesATurn(t *testing.T) {
	path := sgrTurnTestStream(t,
		// A frame whose PROSE carries the words a naive matcher keys on, in the
		// exact spellings this epic's briefings use.
		"\x1b[22;1Hthe file is called screen-turn58 and it holds turn 59 in it\r\n",
		"\x1b[22;3Hecho \"turn 59\" | grep -a \"turn 58\"\r\n",
		"\x1b[22;1H"+sgrTurnTestHeader(60, "turn 58 is the previous one")+"\x1b[0m",
		"\x1b[22;1Hbody\r\n",
	)
	turns, err := LocateSgrTurns(path)
	if err != nil {
		t.Fatalf("LocateSgrTurns: %v", err)
	}
	if len(turns.Spans) != 1 || turns.Spans[0].Turn != 60 {
		t.Fatalf("located %v, want exactly ONE turn (60) — prose naming a turn is not a boundary, and a matcher that thinks it is hands back the wrong segment", turns.Spans)
	}
}

// TestSgrTurnLocatorRepaintDoesNotOpenASpan pins the running-maximum rule. A
// repaint re-emits older rows (a scroll, a resize, the band rewriting itself), and
// a locator that opened a span per header frame would report one turn many times
// and cut spans that were never cut.
func TestSgrTurnLocatorRepaintDoesNotOpenASpan(t *testing.T) {
	hdr58 := "\x1b[22;1H" + sgrTurnTestHeader(58, "first") + "\x1b[0m"
	hdr59 := "\x1b[22;1H" + sgrTurnTestHeader(59, "second") + "\x1b[0m"
	path := sgrTurnTestStream(t,
		hdr58,
		"\x1b[22;1Hanswer\r\n",
		hdr58, // a repaint of the PREVIOUS turn's header — chrome, not a boundary
		hdr59,
		"\x1b[22;1Hanswer two\r\n",
		hdr58, // scrolling back repaints an old header after a newer turn opened
	)
	turns, err := LocateSgrTurns(path)
	if err != nil {
		t.Fatalf("LocateSgrTurns: %v", err)
	}
	if len(turns.Spans) != 2 {
		t.Fatalf("located %d span(s) %v, want 2 — a repainted header must not open a span", len(turns.Spans), turns.Spans)
	}
	for i := 1; i < len(turns.Spans); i++ {
		if turns.Spans[i].Turn <= turns.Spans[i-1].Turn {
			t.Errorf("spans are not strictly ascending: %v", turns.Spans)
		}
		if turns.Spans[i].StartFrame <= turns.Spans[i-1].StartFrame {
			t.Errorf("span start frames are not ascending: %v", turns.Spans)
		}
	}
}

// TestSgrTurnLocatorReportsNoMarksRatherThanGuessing is the rule this epic names
// as "取错段比取不到更糟": a recording with no numbered turn is an EMPTY result,
// not a guess at some other frame.
func TestSgrTurnLocatorReportsNoMarksRatherThanGuessing(t *testing.T) {
	path := sgrTurnTestStream(t,
		"\x1b[1;1Hyou> hello\r\n",
		"\x1b[1;40Hthe answer, with the word turn in it\r\n",
	)
	turns, err := LocateSgrTurns(path)
	if err != nil {
		t.Fatalf("a recording with no turn header is not an error: %v", err)
	}
	if len(turns.Spans) != 0 {
		t.Fatalf("located %v in a recording that carries no turn header — a guessed span is worse than none", turns.Spans)
	}
	if _, ok := turns.Current(); ok {
		t.Errorf("Current() reported a turn in a recording that carries none")
	}
	if got := turns.RangeWords(); got != "none" {
		t.Errorf("RangeWords() = %q, want \"none\" — the refusal must be able to say there is nothing to select", got)
	}
	if turns.StreamFrames != 2 {
		t.Errorf("StreamFrames = %d, want 2 (the totals are still reported)", turns.StreamFrames)
	}
}

// TestSgrTurnCopyIsTheSpanItNames pins the copier against the locator: the frames
// it returns are the ones the span named, by CONTENT.
func TestSgrTurnCopyIsTheSpanItNames(t *testing.T) {
	path := sgrTurnTestStream(t,
		"\x1b[1;1Hyou> opener\r\n",
		"\x1b[22;1H"+sgrTurnTestHeader(58, "first")+"\x1b[0m",
		"\x1b[22;1HTURN-58-BODY\r\n",
		"\x1b[22;1H"+sgrTurnTestHeader(59, "second")+"\x1b[0m",
		"\x1b[22;1HTURN-59-BODY\r\n",
	)
	turns, err := LocateSgrTurns(path)
	if err != nil {
		t.Fatalf("LocateSgrTurns: %v", err)
	}
	span, ok := turns.Span(58)
	if !ok {
		t.Fatalf("turn 58 was not located: %v", turns.Spans)
	}
	rng, frames, err := ReadSgrTurnFile(path, span)
	if err != nil {
		t.Fatalf("ReadSgrTurnFile: %v", err)
	}
	if rng.Frames != span.Frames() || len(frames) != span.Frames() {
		t.Fatalf("copied %d frame(s) (%d reported), the span holds %d", len(frames), rng.Frames, span.Frames())
	}
	joined := ""
	for _, f := range frames {
		joined += string(f)
	}
	if !strings.Contains(joined, "TURN-58-BODY") {
		t.Errorf("the copy does not contain turn 58's own body:\n%q", joined)
	}
	if strings.Contains(joined, "TURN-59-BODY") {
		t.Errorf("the copy contains the NEXT turn's body — the segment is one frame too long:\n%q", joined)
	}
	if !rng.Truncated {
		t.Errorf("a span that starts after frame 0 must report Truncated so a reader knows older frames exist")
	}
	// An empty span is refused rather than written as an empty artifact (Rule 15).
	if _, _, eerr := ReadSgrTurnFile(path, SgrTurnSpan{Turn: 58, StartFrame: 3, EndFrame: 3}); eerr == nil {
		t.Errorf("an empty span was accepted")
	}
}

// TestSgrTurnAnchorStemComesFromTheRenderer pins the derivation itself: the stem
// is the renderer's, and it is followed by the number.
func TestSgrTurnAnchorStemComesFromTheRenderer(t *testing.T) {
	stem, err := sgrTurnAnchorStem()
	if err != nil {
		t.Fatalf("sgrTurnAnchorStem: %v", err)
	}
	if !strings.HasSuffix(stem, "turn ") {
		t.Errorf("the stem %q does not end with the renderer's turn word — the locator would match prose", stem)
	}
	if !strings.Contains(stem, lineedit.TurnRuleGlyph+lineedit.TurnRuleGlyph) {
		t.Errorf("the stem %q carries no rule run — a bare \"turn \" prefix is what a prompt can forge", stem)
	}
	for _, turn := range []int{1, 58, 999} {
		want := lineedit.TurnHeader(lineedit.TurnInfo{Index: turn}, sgrTurnProbeCols)
		if !strings.HasPrefix(want, stem) {
			t.Errorf("the renderer's header for turn %d (%q) does not start with the derived stem %q", turn, want, stem)
			continue
		}
		rest := want[len(stem):]
		if !strings.HasPrefix(rest, sgrItoa(turn)) {
			t.Errorf("the stem is not followed by the number for turn %d: %q", turn, rest)
		}
	}
}

// sgrItoa spells a decimal without importing strconv into the assertion.
func sgrItoa(n int) string { return strconvItoa(n) }

// strconvItoa is a tiny local spelling so the fixture helpers stay dependency-free
// of the test file's imports.
func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// TestSgrTurnLocatorOnARealRecording replays a RECORDED stream when the caller
// names one, and skips with its reason when it does not: the fixture streams above
// are written by the same writer, but a real session is the only evidence that the
// renderer's own repaints, escapes and chinese text do not defeat the anchor.
//
//	GOPI_REPLAY_SGR_TURN=<path to a .sgr.stream> go test ./internal/uiclient/ -run TestSgrTurnLocatorOnARealRecording -v
func TestSgrTurnLocatorOnARealRecording(t *testing.T) {
	path := os.Getenv("GOPI_REPLAY_SGR_TURN")
	if path == "" {
		t.Skip("GOPI_REPLAY_SGR_TURN is unset — no recorded stream to replay")
	}
	turns, err := LocateSgrTurns(path)
	var contradiction *SgrStreamContradictionError
	if errors.As(err, &contradiction) {
		// THE FILE'S OWN DEFECT, NAMED AS SUCH. This branch exists because its
		// absence is the defect this test was written to catch: a stream whose
		// records contradict its own length used to reach the assertion below,
		// which then said "the anchor does not match the real renderer" and sent
		// its reader to inspect the renderer. The refusal carries the offsets and
		// the sizes, and the test refuses to restate it as anything else.
		t.Fatalf("LocateSgrTurns(%s): the RECORDING contradicts itself, which is not a statement about the anchor: %v", path, err)
	}
	if err != nil {
		t.Fatalf("LocateSgrTurns(%s): %v", path, err)
	}
	if len(turns.Spans) == 0 {
		t.Fatalf("no turn was located in %s (%d frames) — the anchor does not match the real renderer", path, turns.StreamFrames)
	}
	for i, s := range turns.Spans {
		if s.Frames() <= 0 {
			t.Errorf("span %d (%+v) is empty", i, s)
		}
		if i > 0 {
			if s.Turn <= turns.Spans[i-1].Turn {
				t.Errorf("spans are not strictly ascending at %d: %v", i, turns.Spans)
			}
			if s.StartFrame != turns.Spans[i-1].EndFrame {
				t.Errorf("span %d starts at %d but the previous ended at %d — the spans must tile the stream", i, s.StartFrame, turns.Spans[i-1].EndFrame)
			}
		}
	}
	t.Logf("located %d turn(s) %s in %d frames", len(turns.Spans), turns.RangeWords(), turns.StreamFrames)
}

// TestSgrTurnLocatorRejectsACopyThatCarriesTheEscapeText is the SECOND false
// positive measured in a real recording, and the one the stem alone cannot catch:
// a frame that prints the ESCAPE'S OWN SPELLING in front of a header.
//
// The bytes `\x1b[22;1H` appear here literally — four characters in the payload —
// after a genuine cursor move. A locator keyed on the stem alone reports the copy
// as a boundary, and `/screen N` then cuts a paragraph of prose.
func TestSgrTurnLocatorRejectsACopyThatCarriesTheEscapeText(t *testing.T) {
	header60 := sgrTurnTestHeader(60, "the real one")
	path := sgrTurnTestStream(t,
		"\x1b[22;1H"+header60+"\x1b[0m",
		// A frame whose payload prints the cursor's escape spelling and then a
		// header — the copy. Note the REAL CUP at the start (every frame has one)
		// and the TEXT `\x1b[22;1H` the copy carries.
		"\x1b[22;1Hthe briefing quoted \\x1b[22;1H"+sgrTurnTestHeader(61, "not a turn")+"\x1b[0m",
	)
	turns, err := LocateSgrTurns(path)
	if err != nil {
		t.Fatalf("LocateSgrTurns: %v", err)
	}
	if len(turns.Spans) != 1 || turns.Spans[0].Turn != 60 {
		t.Fatalf("located %v, want exactly ONE turn (60) — a header preceded by the ESCAPE'S TEXT is a quoted copy, not a boundary", turns.Spans)
	}
}

// TestSgrTurnLocatorRejectsAHeaderDrawnMidLine is the FIRST false positive: the
// stem must be what the cursor sequence draws FIRST. A quoted header is drawn
// after other text, so the bytes in front of it are that text.
func TestSgrTurnLocatorRejectsAHeaderDrawnMidLine(t *testing.T) {
	path := sgrTurnTestStream(t,
		"\x1b[22;1H"+sgrTurnTestHeader(58, "real")+"\x1b[0m",
		"\x1b[22;1Hthe diagnostic said "+sgrTurnTestHeader(59, "quoted")+"\x1b[0m",
	)
	turns, err := LocateSgrTurns(path)
	if err != nil {
		t.Fatalf("LocateSgrTurns: %v", err)
	}
	if len(turns.Spans) != 1 || turns.Spans[0].Turn != 58 {
		t.Fatalf("located %v, want exactly ONE turn (58) — a header that is not the first thing the cursor drew is prose, not a boundary", turns.Spans)
	}
}
