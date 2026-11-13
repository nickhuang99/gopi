package lineedit

import "strings"

// styled.go — the small text utilities the emphasis layer needs. It deliberately
// holds no renderer state: an emphasis is applied to one ROW PIECE at a time
// (Style.Wrap), so nothing has to be tracked across a row change, a scroll, or a
// flush boundary — the terminal's own attribute handling does that work, and a
// renderer that tried to model it would be one more place to get wrong.
//
// The two properties this file makes checkable are the acceptance's own:
//
//   - stripSGR(styled) == the plain text, byte for byte: emphasis is ADDITIVE.
//     Nothing the user reads depends on it (the monochrome rule).
//   - hasSGR(capture) is the byte-exact predicate: false means the layer emitted
//     NOTHING, which is what NO_COLOR / TERM=dumb / --no-color / a non-TTY must
//     produce.

// SGRStrip removes every SGR sequence from s, restoring the characters as the
// user sees them. It is what "emphasis is additive" is asserted with, and what a
// caller measures display cells against when it holds styled text.
func stripSGR(s string) string {
	if !strings.Contains(s, escCSI) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !isCSIFinal(s[j]) {
				j++
			}
			if j < len(s) {
				j++ // consume the final byte
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// isCSIFinal reports whether c ends a CSI sequence (0x40..0x7e).
func isCSIFinal(c byte) bool { return c >= 0x40 && c <= 0x7e }

// hasSGR reports whether s contains any SGR sequence at all. It scans for the
// sequence rather than searching a list of known spellings, so a palette entry
// added later cannot slip past the zero-SGR assertion — the acceptance is about
// ANSI bytes on the wire, not about the constants this file happens to know.
func hasSGR(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] != 0x1b || s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && !isCSIFinal(s[j]) {
			j++
		}
		if j < len(s) && s[j] == 'm' {
			return true
		}
		i = j
	}
	return false
}

// lineSegments cuts text into segments that END at a line boundary (the newline
// stays with the line it ends). The final segment may be unterminated — the line
// the model has not finished — and the caller decides what an unfinished line
// means for its emphasis.
//
// Segments are the unit of COMPOSITION (one line = one decision), and they are
// why an emphasis never begins or ends mid-line: every sequence is emitted at a
// boundary the format itself defines.
func lineSegments(text string) []string {
	if text == "" {
		return nil
	}
	segs := make([]string, 0, 8)
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			segs = append(segs, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		segs = append(segs, text[start:])
	}
	return segs
}

// lineEndingSplit returns the segment without its terminator and whether it had
// one. The composer classifies the line's CONTENT (a fence marker is "```", not
// "```\n"), while the terminator is what tells a complete line from an
// unfinished one.
func lineEndingSplit(seg string) (content string, terminated bool) {
	if strings.HasSuffix(seg, escNewline) {
		return strings.TrimSuffix(seg, escNewline), true
	}
	return seg, false
}
