package lineedit

// screen_invalid_utf8_resync_test.go — a byte that can NEVER begin a rune must
// not be HELD: holding it is a livelock with a paint hole behind it.
//
// THE DEFECT THIS PINS (measured 2026-09-28, no pty, no device load — hand-built
// byte sequences straight through Screen.Feed). Feed's `default` arm read the
// tuple `utf8.DecodeRune` returns and treated ANY (RuneError, size<=1) as "a
// multi-byte rune whose continuation has not arrived yet", then held the tail:
//
//	stream "\x80\x1b[1;1Hok"      painted "<empty screen>"   (wrong: should be "ok")
//	stream       "\x1b[1;1Hok"    painted "ok"               (the reference)
//
// The two readings of that tuple are NOT the same case:
//
//   - a truncated VALID prefix (`0xE4` at a chunk end) is a rune in progress —
//     `utf8.FullRune` is false and the next read completes it. HOLD it.
//   - a byte that cannot start a sequence at all (a lone continuation byte
//     0x80..0xBF, or a lead past U+10FFFF, 0xF5..0xFF) is not in progress and
//     never will be — `utf8.FullRune` is TRUE and the decoder's verdict is
//     final. Holding it keeps the byte at the HEAD of the tail, so every later
//     Feed re-offers it, takes the same branch and re-holds: the hold never
//     completes, and every genuine paint byte queued behind it (the frame's
//     cursor moves, text, SGR) is swallowed uninterpreted until the tail passes
//     maxPendingEscape and holdPending discards the whole state. A deterministic
//     paint hole of up to ~512 bytes, and — because the byte stays — an
//     arbitrarily long one at the same cost.
//
// The criterion asserted here is the property, not a byte offset: THE HOLD MUST
// TERMINATE, and the same stream must paint the same screen at every read size.

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The bytes this file's subject is made of, named (Rule 1) because the VALUES
// are the criterion: each one can never begin a UTF-8 sequence.
const (
	loneContinuationByte byte = 0x80 // 10xxxxxx with nothing in front of it
	lastContinuationByte byte = 0xBF // the far end of the same range
	invalidLeadByte      byte = 0xF5 // past U+10FFFF: encodes no rune at all
	invalidLeadLastByte  byte = 0xFF // never valid in any UTF-8 revision
	threeByteLead        byte = 0xE4 // a VALID 3-byte lead — the contrast case
	// threeByteLeadBadFollower is a byte that cannot follow threeByteLead: the
	// sequence is invalid AND `utf8.FullRune` is already true for it, so the
	// decoder's RuneError+size=1 here is FINAL, not "wait for more".
	threeByteLeadBadFollower byte = 0xFF
)

// paintAfterNoise is the paint that sits BEHIND an invalid byte in these cases:
// an absolute cursor placement plus text, i.e. exactly the shape a chat frame
// writes. Losing it is the user-visible symptom (an empty screen where the frame
// meant to write a row).
const paintAfterNoise = "\x1b[1;1Hok"

// pendingLen is how many bytes the parser is still holding for the next Feed.
// It is the observable the livelock shows up in: the pre-fix parser holds an
// invalid byte and the paint behind it FOREVER (the count never returns to 0 no
// matter how many further Frames arrive).
func pendingLen(s *Screen) int { return len(s.pending) }

// rawByte spells one byte as the ONE-BYTE string carrying it. `string(0x80)`
// does not: an int-to-string conversion is a RUNE conversion, so it yields the
// two bytes of U+0080 — the test's own fixture would then be a VALID stream that
// can never show the defect. Written once, used by every case (a bare literal in
// an argument list would be converted and UTF-8-encoded without warning).
func rawByte(b byte) string { return string([]byte{b}) }

// TestScreenFeedByteThatCannotBeginARuneIsResynchronized is the RED case. Each
// stream is (invalid byte(s)) + (a real paint), fed at every chunk size; the
// expected screen is the SAME paint with the invalid bytes removed, because a
// byte that can never begin a rune commands no cell — it is stream noise, on the
// same footing as the control bytes Feed already skips (`b < printableMin`).
//
// Pre-fix, every case fails twice over: the paint is GONE (the screen is empty)
// and the hold has not terminated (pendingLen != 0).
func TestScreenFeedByteThatCannotBeginARuneIsResynchronized(t *testing.T) {
	cases := []struct {
		name   string
		stream string
		paint  string
	}{
		{"lone-continuation", rawByte(loneContinuationByte) + paintAfterNoise, paintAfterNoise},
		{"last-continuation", rawByte(lastContinuationByte) + paintAfterNoise, paintAfterNoise},
		{"invalid-lead", rawByte(invalidLeadByte) + paintAfterNoise, paintAfterNoise},
		{"invalid-lead-0xff", rawByte(invalidLeadLastByte) + paintAfterNoise, paintAfterNoise},
		{"run-of-noise", strings.Repeat(rawByte(loneContinuationByte), 8) + paintAfterNoise, paintAfterNoise},
		{"lead-with-an-impossible-follower",
			rawByte(threeByteLead) + rawByte(threeByteLeadBadFollower) + paintAfterNoise, paintAfterNoise},
		{"noise-then-plain-text", rawByte(loneContinuationByte) + "hello", "hello"},
	}
	for _, c := range cases {
		stream := []byte(c.stream)
		// The reference is the SAME stream with its invalid bytes taken out: the
		// paint must land exactly where it lands without them.
		reference := feedWhole([]byte(c.paint))
		if !strings.Contains(reference.Contents(), "ok") && strings.Contains(c.paint, "ok") {
			t.Fatalf("%s: the test's own reference does not paint %q:\n%s", c.name, c.paint, reference.Contents())
		}
		for _, step := range chunkFeedSizes {
			got := feedInChunks(stream, step)
			if got.Contents() != reference.Contents() {
				t.Errorf("%s: step=%d painted %q, want %q (the paint behind the un-startable byte was swallowed)\n%s",
					c.name, step, got.Contents(), reference.Contents(), got.Contents())
			}
			if n := pendingLen(got); n != 0 {
				t.Errorf("%s: step=%d left %d byte(s) held for the next Feed — the hold did not terminate at the un-startable byte",
					c.name, step, n)
			}
		}
	}
}

// TestScreenFeedUnstartableByteIsSkippedInTheSameChunk is the one-shot half of
// the case above: invalid byte and paint arriving in ONE Feed (the shape a real
// reader produces when the frame fits in its buffer). Asserted separately
// because it is the shape every in-tree caller uses, and because a fix that only
// resynchronized at the NEXT chunk boundary would pass the chunked case by
// accident for step>1 while still swallowing a whole one-shot frame.
func TestScreenFeedUnstartableByteIsSkippedInTheSameChunk(t *testing.T) {
	s := NewScreen(chunkProbeCols, chunkProbeRows)
	s.Feed([]byte(rawByte(loneContinuationByte) + paintAfterNoise))
	if got, want := s.Contents(), feedWhole([]byte(paintAfterNoise)).Contents(); got != want {
		t.Fatalf("one-shot feed painted %q, want %q", got, want)
	}
	if n := pendingLen(s); n != 0 {
		t.Fatalf("one-shot feed left %d byte(s) held, want 0", n)
	}
}

// TestScreenFeedUnstartableByteThenPaintInASeparateFeed is the same defect with
// the boundary BETWEEN the two (chunk N = the invalid byte alone, chunk N+1 =
// the paint). This is the livelock at its purest: pre-fix, chunk N+1's paint is
// appended to the held byte, the head is still un-startable, and the whole
// chunk is re-held.
func TestScreenFeedUnstartableByteThenPaintInASeparateFeed(t *testing.T) {
	s := NewScreen(chunkProbeCols, chunkProbeRows)
	s.Feed([]byte{loneContinuationByte})
	if n := pendingLen(s); n != 0 {
		t.Errorf("a lone continuation byte was held (%d byte(s)): it begins no rune, so there is nothing to wait for", n)
	}
	s.Feed([]byte(paintAfterNoise))
	if got, want := s.Contents(), feedWhole([]byte(paintAfterNoise)).Contents(); got != want {
		t.Fatalf("the paint in the following feed painted %q, want %q", got, want)
	}
	if n := pendingLen(s); n != 0 {
		t.Fatalf("the hold did not terminate: %d byte(s) still held after the paint arrived", n)
	}
}

// TestScreenFeedRepeatedNoiseDoesNotGrowTheHold is the "no unbounded hold"
// property stated on its own: a stream of nothing but un-startable bytes must
// terminate with an EMPTY carry every time, however long it is. Pre-fix the tail
// grows to maxPendingEscape and is then dropped wholesale (the paint hole the
// epic measures); post-fix it never grows at all.
func TestScreenFeedRepeatedNoiseDoesNotGrowTheHold(t *testing.T) {
	s := NewScreen(chunkProbeCols, chunkProbeRows)
	for i := 0; i < maxPendingEscape*2; i++ {
		s.Feed([]byte{loneContinuationByte})
		if n := pendingLen(s); n != 0 {
			t.Fatalf("iteration %d left %d byte(s) held — a byte that begins no rune must be consumed, not carried", i, n)
		}
	}
}

// TestScreenFeedTruncatedValidPrefixIsStillHeld is the GUARD against
// over-correcting: a prefix that could still become a valid rune MUST be held
// (dropping or skipping it would lose a real rune — the carry buffer's whole
// reason to exist). Every PROPER prefix of a valid 2-, 3- and 4-byte rune is
// asserted to be held, and then completed into the rune the whole-stream feed
// paints.
func TestScreenFeedTruncatedValidPrefixIsStillHeld(t *testing.T) {
	runes := []string{"é", "中", "𝄞"} // 2-, 3- and 4-byte encodings
	for _, r := range runes {
		raw := []byte(r)
		for cut := 1; cut < len(raw); cut++ {
			s := NewScreen(chunkProbeCols, chunkProbeRows)
			s.Feed(raw[:cut])
			if n := pendingLen(s); n != cut {
				t.Errorf("%q cut after %d byte(s): held %d, want %d — a truncated VALID prefix must be kept for the next read",
					r, cut, n, cut)
			}
			if got := s.Contents(); strings.TrimSpace(got) != "" {
				t.Errorf("%q cut after %d byte(s): a half rune was painted (%q)", r, cut, got)
			}
			s.Feed(raw[cut:])
			if n := pendingLen(s); n != 0 {
				t.Errorf("%q cut after %d byte(s): %d byte(s) still held after the rune completed", r, cut, n)
			}
			if got, want := s.Contents(), feedWhole(raw).Contents(); got != want {
				t.Errorf("%q cut after %d byte(s): completed rune painted %q, want %q", r, cut, got, want)
			}
		}
	}
}

// TestScreenFeedInvalidThreeByteLeadPaintsTheFollowingText is the case the epic
// names explicitly (0xE4 + 'A'): a three-byte lead whose follower cannot be a
// continuation. Pre-fix this case is red for the same reason as its siblings —
// `utf8.FullRune` is already true for `E4 41`, so the decoder's verdict
// (RuneError, 1) is FINAL, yet the old arm read it as "wait for the rest" and
// held the whole remaining frame.
//
// The post-fix contract asserted here: the invalid LEAD occupies at most one
// cell — it decodes as U+FFFD, the replacement glyph the utf8 decoder's own
// contract names — and everything after it (`A`, the cursor placement, the text)
// is painted normally. The hold terminates.
func TestScreenFeedInvalidThreeByteLeadPaintsTheFollowingText(t *testing.T) {
	// The paint goes to row 2 (absolute placement at 2;1), so row 0 KEEPS the
	// sequence under test: the invalid lead's cell and the `A` behind it are not
	// overwritten, and both halves of the contract are readable at once.
	const paint = "\x1b[2;1Hok"
	stream := []byte(rawByte(threeByteLead) + "A" + paint)
	reference := feedWhole(stream)
	row0 := reference.Line(0)
	if !strings.ContainsRune(row0, utf8.RuneError) || !strings.Contains(row0, "A") {
		t.Fatalf("the test's own reference row 0 is %q, want the replacement glyph followed by %q", row0, "A")
	}
	for _, step := range chunkFeedSizes {
		got := feedInChunks(stream, step)
		if got.Contents() != reference.Contents() {
			t.Errorf("step=%d painted %q, want %q (the text after an invalid lead was swallowed)",
				step, got.Contents(), reference.Contents())
		}
		if n := pendingLen(got); n != 0 {
			t.Errorf("step=%d left %d byte(s) held, want 0", step, n)
		}
	}
}

// TestScreenFeedChunkingIsIrrelevantOnMixedNoiseAndPaint drives the same
// criterion the carry-buffer suite asserts (the chunk boundary is the READER's
// artifact), now on a stream that MIXES the shapes: valid wide runes, SGR, a
// truncated-at-the-boundary prefix, and un-startable bytes. A fix that
// resynchronized only at some boundaries would show up here as a step for which
// the screen differs.
func TestScreenFeedChunkingIsIrrelevantOnMixedNoiseAndPaint(t *testing.T) {
	stream := []byte(strings.Join([]string{
		rawByte(loneContinuationByte), "中",
		"\x1b[1;1H中文",
		rawByte(invalidLeadByte),
		rawByte(threeByteLead), // a prefix whose continuation is a LATER byte
		"A",
		"\x1b[0m", rawByte(loneContinuationByte), "tail",
	}, ""))
	want := feedWhole(stream)
	if top, bottom := want.ScrollRegion(); top != 0 || bottom != chunkProbeRows-1 {
		t.Logf("note: the mixed stream installed region %d..%d", top, bottom)
	}
	for _, step := range chunkFeedSizes {
		got := feedInChunks(stream, step)
		if got.Contents() != want.Contents() {
			t.Fatalf("step=%d paints a different screen\n whole:\n%s\n chunks:\n%s",
				step, want.Contents(), got.Contents())
		}
		if n := pendingLen(got); n != 0 {
			t.Fatalf("step=%d left %d byte(s) held at the end of a complete stream", step, n)
		}
	}
}
