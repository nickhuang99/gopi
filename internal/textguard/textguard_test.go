package textguard

// Tests for the shared text predicate/sanitizer. The fixture below is the REAL
// byte prefix of the offending ledger row (epic binary-summary-guard): the
// head of a Linux ELF64 executable, not a synthetic "\x00\x01" stand-in — so
// the test fails if any renderer starts letting those bytes through
// (PROJECT.md Rule 10: verify on the smallest REAL sample).

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// elfHeader is the ELF64 magic + the first header words of the binary that
// became a prompt (`gopi gopi help` in a directory holding the built ./gopi).
var elfHeader = []byte{
	0x7f, 'E', 'L', 'F',
	0x02, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x02, 0x00, 0x3e, 0x00, 0x01, 0x00,
}

func elfBlobFixture(n int) string {
	var b []byte
	for len(b) < n {
		b = append(b, elfHeader...)
	}
	return string(b[:n])
}

func TestIsTextIdentity(t *testing.T) {
	for _, s := range []string{
		"",
		"resolve merge conflict: fix/cwd-and-lifetime",
		"[auto] 背景（nick：harvest 时是否也 push HP 分支回 zpeer？）",
		"emoji 🙂 and symbols → ✓ § ¶",
		"tabs\tand\nnewlines are whitespace, not corruption",
	} {
		if !IsText(s) {
			t.Errorf("IsText(%q) = false, want true — ordinary text must pass", s)
		}
		if got := Sanitize(s, 0, false); got != s {
			t.Errorf("Sanitize(%q) = %q, want identity", s, got)
		}
	}
}

// TestSpaceSeparatorsAreText is the regression guard for the measured
// 2026-11-03 defect (task 01a1a0ed): the operator pasted gopi's OWN chat status
// row into a task briefing, and the briefing came back reading
//
//	1m13s · resp[bin \xc2 \xa0]
//
// — the row's field gap is a NO-BREAK SPACE on purpose
// (cmd/agent/chat_stream.go statusLiveInFlightGap, so the row's own U+0020
// separator can never cut a field in half), and this package called every Zs
// rune except the ASCII space binary corruption. Ordinary text must pass
// byte-for-byte (the package's first contract clause); a space is ordinary
// text.
func TestSpaceSeparatorsAreText(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		// INTERIOR positions: the whole contract, identity byte-for-byte.
		{"the chat status row's own field gap (U+00A0)", "1m13s · resp\u00a0· 161k"},
		{"ideographic space from a Chinese IME (U+3000)", "中文\u3000输入"},
		{"en quad (U+2000)", "a\u2000b"},
		{"thin space (U+2009)", "a\u2009b"},
		{"narrow no-break space (U+202F)", "10\u202f000"},
		{"medium mathematical space (U+205F)", "a\u205fb"},
		{"figure space (U+2007)", "turn\u2007591"},
	} {
		if !IsText(tc.in) {
			t.Errorf("IsText(%q) = false, want true — %s is a SPACE, not corruption", tc.in, tc.name)
		}
		if got := Sanitize(tc.in, 0, false); got != tc.in {
			t.Errorf("Sanitize(%q) = %q, want identity — %s must not be escaped", tc.in, got, tc.name)
		}
		if got := Sanitize(tc.in, 0, true); got != tc.in {
			t.Errorf("Sanitize(%q, singleLine) = %q, want identity — %s must not be escaped", tc.in, got, tc.name)
		}
	}
	// The exact string the ledger rendered, asserted end to end: no marker. A
	// LEADING/TRAILING Zs is TRIMMED — Sanitize ends with strings.TrimSpace,
	// whose unicode.IsSpace already covers every Zs rune — and that trim is
	// pre-existing, documented behaviour for surrounding whitespace, not a
	// corruption escape. What must never appear is the binary marker.
	if got := Sanitize("resp\u00a0", 0, true); got != "resp" {
		t.Fatalf("Sanitize(%q) = %q, want %q (trailing whitespace trimmed, NOT escaped)", "resp\u00a0", got, "resp")
	}
	if got := Sanitize("resp\u00a0", 0, false); strings.Contains(got, "["+Marker) {
		t.Fatalf("Sanitize(%q) = %q — a no-break space must never produce a binary marker", "resp\u00a0", got)
	}
}

// TestLineStructureStaysNonText pins the other half of the Zs decision: the
// classes that END A LINE stay outside the text contract, because a line
// separator inside a single rendered line would shear the geometry a renderer
// laid out. Only Zs occupies a cell without ending a line.
func TestLineStructureStaysNonText(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"line separator (U+2028, Zl)", "a\u2028b"},
		{"paragraph separator (U+2029, Zp)", "a\u2029b"},
		{"next line (U+0085, Cc)", "a\u0085b"},
		{"vertical tab", "a\vb"},
		{"form feed", "a\fb"},
		{"zero width space (U+200B, Cf)", "a\u200bb"},
		{"zero width no-break space / BOM (U+FEFF, Cf)", "a\ufeffb"},
		{"soft hyphen (U+00AD, Cf)", "a\u00adb"},
	} {
		if IsText(tc.in) {
			t.Errorf("IsText(%q) = true, want false — %s is line structure or invisible formatting", tc.in, tc.name)
		}
		if got := Sanitize(tc.in, 0, false); !strings.Contains(got, "["+Marker) {
			t.Errorf("Sanitize(%q) = %q — %s must still be escaped visibly", tc.in, got, tc.name)
		}
	}
}

func TestIsTextRejectsBinary(t *testing.T) {
	for _, s := range []string{
		string(elfHeader),
		"prefix\x00suffix",
		"caf\xe9 中文",              // invalid UTF-8 byte
		"bell\x07 and escape\x1b", // C0 controls
	} {
		if IsText(s) {
			t.Errorf("IsText(%q) = true, want false — control/invalid bytes are not text", s)
		}
	}
}

// TestSanitizeKeepsMagicBytesVisible is the honest-escape contract: the mask
// must not destroy the evidence. A reader looking at the rendered line can
// still tell WHICH binary was captured.
func TestSanitizeKeepsMagicBytesVisible(t *testing.T) {
	got := Sanitize("check this\x00 file", 0, false)
	if strings.ContainsRune(got, 0) {
		t.Fatalf("NUL survived: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("not valid UTF-8: %q", got)
	}
	if !strings.Contains(got, "[bin \\x00]") {
		t.Errorf("escape must be visible in place, got %q", got)
	}
	if !strings.Contains(got, "check this[bin \\x00] file") {
		t.Errorf("in-place escape must keep the surrounding text, got %q", got)
	}
}

func TestSanitizeElfBlob(t *testing.T) {
	got := Sanitize("[auto] "+elfBlobFixture(200)+" help", 200, true)
	if strings.ContainsRune(got, 0) || !utf8.ValidString(got) {
		t.Fatalf("sanitized blob is not text: %q", got)
	}
	if n := strings.Count(got, "["+Marker+" "); n != 1 {
		t.Errorf("a blob-dominant string renders exactly ONE marker, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "[bin \\x7fELF\\x02\\x01\\x01\\x00") {
		t.Errorf("marker must carry the magic head recognizable as ELF, got %q", got)
	}
	if !strings.HasPrefix(got, "[auto] [bin ") {
		t.Errorf("leading text must survive, got %q", got)
	}
}

func TestSanitizeBoundsRunesAndLines(t *testing.T) {
	long := Sanitize(strings.Repeat("知", 500), 200, true)
	if n := utf8.RuneCountInString(long); n > 201 { // ceiling + ellipsis
		t.Errorf("rune count = %d, want <= 201", n)
	}
	if !strings.HasSuffix(long, Ellipsis) {
		t.Errorf("truncation must be marked: %q", long)
	}
	if got := Sanitize("a\nb\tc\rd", 0, true); got != "a b c d" {
		t.Errorf("single-line fold = %q, want %q", got, "a b c d")
	}
	if got := Sanitize("a\nb\tc", 0, false); got != "a\nb\tc" {
		t.Errorf("multi-line mode must preserve line structure, got %q", got)
	}
}
