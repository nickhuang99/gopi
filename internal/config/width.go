package config

import "unicode"

// HelpWrapCol is the right margin every help block is wrapped to, measured in
// DISPLAY columns — a CJK glyph occupies two terminal cells, so counting
// bytes (or runes) under-measures a line by half and lets it overflow the
// terminal it is printed to. It is EXPORTED because the subcommand usage
// constants live in cmd/agent and their guard must measure against the same
// budget the renderer wraps to: one budget, one definition (Rule 14 — a
// second copy in cmd/agent would drift).
const HelpWrapCol = 88

// IsWideRune reports whether r occupies two terminal columns: the East Asian
// Wide/Fullwidth ranges (CJK ideographs, kana, Hangul, fullwidth forms) and the
// emoji pictograph blocks. Ambiguous-width characters count as one column, the
// common terminal default.
func IsWideRune(r rune) bool {
	switch {
	case r < 0x1100:
		// Fast path: everything below Hangul Jamo is narrow.
		return false
	case unicode.Is(unicode.Han, r),
		unicode.Is(unicode.Hangul, r),
		unicode.Is(unicode.Hiragana, r),
		unicode.Is(unicode.Katakana, r):
		return true
	case r <= 0x115F: // Hangul Jamo initial consonants
		return true
	case r >= 0x2E80 && r <= 0x303E: // CJK radicals, Kangxi, CJK symbols
		return true
	case r >= 0xFF00 && r <= 0xFF60: // Fullwidth forms
		return true
	case r >= 0xFFE0 && r <= 0xFFE6: // Fullwidth signs
		return true
	case r >= 0x1F300 && r <= 0x1FAFF: // emoji and pictographs
		return true
	}
	return false
}

// DisplayWidth is the number of terminal columns s occupies — the width the
// help wrap budget is spent in. It is the measure every help guard must use:
// len(s) counts bytes (a CJK line reads as three times its real width) and
// utf8.RuneCountInString counts glyphs (a CJK line reads as half).
func DisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		if IsWideRune(r) {
			w += 2
			continue
		}
		w++
	}
	return w
}
