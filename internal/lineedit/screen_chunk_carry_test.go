package lineedit

// screen_chunk_carry_test.go — the chunk boundary is not a fact about the
// stream.
//
// THE DEFECT THIS PINS (measured 2026-09-26, `gopi chat` over a real 117x26 pty;
// the observation dump's own artifact quoted in
// devdoc/chat-paint-stream-chunk-fidelity.md). A terminal is a byte STREAM: the
// app writes a paint frame and the OS hands it to the reader in however many
// chunks it feels like. `Screen.Feed` interpreted each chunk as if it were a
// complete message and, when a chunk ended mid-sequence, DROPPED the tail:
//
//  1. a truncated escape sequence (`ESC[24;1H` split after `ESC[24;`) — the
//     rest of the chunk was discarded, so the cursor was never positioned and
//     the cells meant for that row were written somewhere else;
//  2. a CSI whose parameters were split (`ESC[1;3r` split after `1;3`) —
//     `consumeEscape` returned -1 but had ALREADY accumulated `1` and `3` into
//     the parser's scratch, and the retry parsed the same digits again over
//     them: `11;33`. A scroll region of rows 1..3 was installed as rows 11..33,
//     which the layout then used to discard the rows it meant to keep.
//
// Both are the same user-visible failure: the answer painted on the terminal is
// NOT what the interpreter's grid says, so an agent reading the dump back (the
// self-observation channel) reads a screen nobody was shown — and cannot tell
// it apart from a real display loss. Measured on the recorded byte stream: fed
// whole, the grid held the answer and 74 lines of scrollback; fed in 64-byte
// reads, it held ZERO lines of scrollback and no answer at all.
//
// The criterion asserted here is the property itself, not a byte offset: THE
// SAME STREAM MUST PAINT THE SAME SCREEN AT EVERY READ SIZE.

import (
	"strconv"
	"strings"
	"testing"
)

// chunkFeedSizes are the read sizes the equivalence criterion is checked at.
// They are named (Rule 1) because the VALUES are the test's subject: 1 and 2
// split every sequence at every possible offset (the maximal case), while the
// larger ones model the buffer sizes a real reader uses.
var chunkFeedSizes = []int{1, 2, 3, 7, 13, 64, 333, 4096}

// chunkProbeCols/Rows is the geometry the synthetic case paints into. Small on
// purpose: a grid this size makes every row the stream writes observable
// without a long fixture.
const (
	chunkProbeCols = 12
	chunkProbeRows = 3
)

// feedInChunks runs the same bytes through a fresh Screen in `step`-byte reads.
func feedInChunks(data []byte, step int) *Screen {
	s := NewScreen(chunkProbeCols, chunkProbeRows)
	for i := 0; i < len(data); i += step {
		j := i + step
		if j > len(data) {
			j = len(data)
		}
		s.Feed(data[i:j])
	}
	return s
}

// feedWhole is the reference: the whole stream in one call, which is what every
// existing caller (and every existing assertion in this package) does.
func feedWhole(data []byte) *Screen {
	s := NewScreen(chunkProbeCols, chunkProbeRows)
	s.Feed(data)
	return s
}

// TestScreenFeedChunkBoundaryKeepsTheTail is the RED case for defect (1): a
// chunk that ends inside an escape sequence must carry the tail, not drop it.
func TestScreenFeedChunkBoundaryKeepsTheTail(t *testing.T) {
	cases := []struct{ name, stream string }{
		// CUP with the sequence split at its first byte: the cursor move is the
		// whole point of the frame, and losing it writes the text elsewhere.
		{"cup-split", "\x1b[2;5Hhere"},
		// An SGR split mid-parameters: losing it paints the rest with the wrong
		// pen (and the dump's per-cell attributes are the channel that reports it).
		{"sgr-split", "a\x1b[1;38;5;220mbold\x1b[0mc"},
		// A multi-byte rune split: the continuation bytes are in the next chunk.
		{"cjk-split", strings.Repeat("中", 6)},
		// An OSC string (bubbletea asks for the background colour this way) split
		// before its terminator.
		{"osc-split", "x\x1b]11;?\x07y"},
		// An OSC that is ABANDONED (never terminated) followed by a CSI: the ESC
		// that starts the next sequence aborts the string, so consuming it would
		// eat the CSI's own introducer and paint its digits as text. Measured:
		// the grid row read "1;1Hok" instead of "ok".
		{"osc-aborted-then-csi", "\x1b]11;?\x1b[1;1Hok"},
		// A scrolling region split mid-parameters — defect (2)'s carrier.
		{"region-split", "\x1b[1;3r1\r\n2\r\n3\r\n4"},
	}
	for _, c := range cases {
		want := feedWhole([]byte(c.stream))
		for _, step := range chunkFeedSizes {
			got := feedInChunks([]byte(c.stream), step)
			if want.Dump().Text() != got.Dump().Text() {
				t.Errorf("%s: step=%d paints a different screen\n whole:\n%s\n chunks:\n%s",
					c.name, step, want.Dump().Text(), got.Dump().Text())
			}
		}
	}
}

// TestScreenFeedSplitCSIParametersAreNotDoubled is defect (2) alone, asserted on
// the OBSERVABLE it corrupted rather than on the scratch buffer: the scroll
// region a caller installs must be the one the stream spelled.
//
// RED on the pre-fix parser: `ESC[1;3r` split after `1;3` installs region 11..33
// on a 12x3 grid (clamped to the last row), so the assertion reads `2,2` where
// the stream said `0,2`.
func TestScreenFeedSplitCSIParametersAreNotDoubled(t *testing.T) {
	const stream = "\x1b[1;3r"
	wantTop, wantBottom := 0, 2
	whole := feedWhole([]byte(stream))
	if top, bottom := whole.ScrollRegion(); top != wantTop || bottom != wantBottom {
		t.Fatalf("one-shot feed installed region %d..%d, want %d..%d", top, bottom, wantTop, wantBottom)
	}
	for step := 1; step <= len(stream); step++ {
		got := feedInChunks([]byte(stream), step)
		if top, bottom := got.ScrollRegion(); top != wantTop || bottom != wantBottom {
			t.Errorf("step=%d installed region %d..%d, want %d..%d (split parameters were accumulated twice)",
				step, top, bottom, wantTop, wantBottom)
		}
	}
}

// TestScreenFeedOneFramePerChunkIsUnchanged is the non-regression floor: a
// caller that feeds whole frames (what every in-tree caller does, and what the
// existing suite asserts) sees exactly the same screen as before the carry
// existed.
func TestScreenFeedOneFramePerChunkIsUnchanged(t *testing.T) {
	frame := "\x1b[1;3r\x1b[2;3H中文 abc\x1b[0m"
	want := feedWhole([]byte(frame))
	got := NewScreen(chunkProbeCols, chunkProbeRows)
	got.Feed([]byte(frame))
	if want.Dump().Text() != got.Dump().Text() {
		t.Errorf("a single whole-frame feed changed:\n before:\n%s\n after:\n%s",
			want.Dump().Text(), got.Dump().Text())
	}
}

// TestScreenFeedChunkSizeIsIrrelevantOnARecordedFrame drives the criterion on a
// stream SHAPED like the chat layout's own traffic — an absolute placement plus
// a confined scroll region plus wide text per frame — rather than on isolated
// sequences: the defect above only became visible once a layout scrolled its
// band, which is exactly what these frames do.
func TestScreenFeedChunkSizeIsIrrelevantOnARecordedFrame(t *testing.T) {
	var b strings.Builder
	for frame := 0; frame < 8; frame++ {
		b.WriteString("\x1b[1;1H\x1b[2K")              // park and clear the band's row
		b.WriteString("\x1b[1;3r")                     // the region the layout confines scrolling to
		b.WriteString("\x1b[1;1H▌ 第")                  // the turn marker plus a wide rune
		b.WriteString(strconv.Itoa(frame) + " 帧 line") // ASCII body
		b.WriteString("\x1b[2;1Hnext row text")
		b.WriteString("\x1b[3;1Hscrolling now\r\n") // push the band up by one
		b.WriteString("\x1b[1;3r\x1b[0m")
	}
	stream := []byte(b.String())
	want := feedWhole(stream)
	for _, step := range chunkFeedSizes {
		got := feedInChunks(stream, step)
		if want.Dump().Text() != got.Dump().Text() {
			t.Fatalf("step=%d paints a different screen\n whole:\n%s\n chunks:\n%s",
				step, want.Dump().Text(), got.Dump().Text())
		}
		if want.ScrollbackText() != got.ScrollbackText() {
			t.Fatalf("step=%d keeps different scrollback (%d lines vs %d)",
				step, len(got.Scrollback()), len(want.Scrollback()))
		}
	}
}

// TestScreenFeedCarryBufferIsBounded pins the bound on the carry: a stream that
// never completes a sequence must not grow the parser's state without limit
// (Rule 1's bound, and the reason holdPending truncates rather than appends).
func TestScreenFeedCarryBufferIsBounded(t *testing.T) {
	s := NewScreen(chunkProbeCols, chunkProbeRows)
	for i := 0; i < maxPendingEscape*4; i++ {
		s.Feed([]byte{escByte}) // an ESC that never becomes a sequence
	}
	if len(s.pending) > maxPendingEscape {
		t.Fatalf("carry buffer grew to %d bytes, bound is %d", len(s.pending), maxPendingEscape)
	}
}
