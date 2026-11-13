package tokenizer

// bytes.go — the byte↔rune alphabet the vocabulary is written in, and the two
// rune helpers the rest of the package shares (one implementation each, Rule 14).

import "unicode/utf8"

// byteRuneTable is the byte↔rune table the vocab is keyed on: the printable
// bytes keep their own rune, and the remaining 68 values are lifted above U+0100
// in ascending order. It is the table every merge string in tokenizer.json is
// spelled in — "Ġt" is space + "t", not a literal two-character word.
func byteRuneTable() [256]rune {
	var t [256]rune
	var lifted rune
	for b := 0; b < 256; b++ {
		if isPrintableByte(byte(b)) {
			t[b] = rune(b)
			continue
		}
		t[b] = 256 + lifted
		lifted++
	}
	return t
}

// isPrintableByte reports the three windows the table leaves untouched:
// '!'..'~', 0xA1..0xAC, 0xAE..0xFF.
func isPrintableByte(b byte) bool {
	switch {
	case b >= '!' && b <= '~':
		return true
	case b >= 0xA1 && b <= 0xAC:
		return true
	case b >= 0xAE && b <= 0xFF:
		return true
	}
	return false
}

// appendRune appends r's UTF-8 encoding to dst.
func appendRune(dst []byte, r rune) []byte {
	var b [utf8.UTFMax]byte
	n := utf8.EncodeRune(b[:], r)
	return append(dst, b[:n]...)
}

// decodeMapped splits a mapped byte slice into runes.
func decodeMapped(b []byte) []rune {
	out := make([]rune, 0, len(b))
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		out = append(out, r)
		i += size
	}
	return out
}

// runeLen is the UTF-8 width of r, used when a rune-slice index is turned back
// into a byte offset.
//
// IT IS THE WIDTH OF THE VALUE, NOT OF ANY PARTICULAR ENCODING — which is why
// the pre-tokenizer must NOT use it to walk a string: an INVALID byte decodes to
// U+FFFD (width 3) while occupying ONE byte, so a table summed from these widths
// runs past len(s). runeBoundaries (below) is the one that walks a string; this
// answers a different question ("how wide is this rune value"), and the only
// caller that wanted it answers it for well-formed runes alone.
func runeLen(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r < 0x800:
		return 2
	case r < 0x10000:
		return 3
	}
	return 4
}

// runeBoundaries splits s into its runes and, beside them, the byte offset at
// which each one BEGINS, plus len(s) as a final entry: offs[k]..offs[k+1] is the
// byte span of rune k.
//
// THE WIDTH COMES FROM THE ENCODING, NEVER FROM THE RUNE'S VALUE.
// utf8.DecodeRuneInString answers both at once, and for an invalid byte it
// answers (RuneError, 1) — ONE byte consumed — which is exactly what makes the
// table end at len(s). A table summed from runeLen instead is wrong precisely
// where the two disagree: an invalid byte occupies 1 byte but reads back as
// U+FFFD, whose value is 3 wide, so each one pushes the rest of the table two
// bytes to the right. Stage 3 then slices past the end of the string — the
// measured crash `slice bounds out of range [:141] with length 140` in
// splitGrouping, which took down two worker runs from a request whose content
// carried a non-UTF-8 byte (a tool result holding a file's raw bytes).
func runeBoundaries(s string) ([]rune, []int) {
	rs := make([]rune, 0, len(s))
	offs := make([]int, 1, len(s)+1)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		rs = append(rs, r)
		i += size
		offs = append(offs, i)
	}
	return rs, offs
}
