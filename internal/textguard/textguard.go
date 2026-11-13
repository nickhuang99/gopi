// Package textguard is the ONE decision point for the question "is this
// string text?" and for the sanitizer that folds a string that is NOT text
// into something a terminal can print.
//
// WHY A SHARED LEAF PACKAGE (Rule 14): the same question is asked by two
// packages that must not depend on each other — internal/tools (the task
// ledger's summary/payload columns: `gopi task list`, `task show`) and
// internal/gopilog (the session renderers: `gopi session log --session/--todo/--last`,
// which print s.Task, the session's first user row = the accepted prompt).
// internal/gopilog already imports internal/agent, and internal/tools imports
// nothing from gopilog, so putting the predicate in tools would force a cycle;
// a leaf package (stdlib imports only, no project imports) is what both can
// share (the internal/toolnames precedent).
//
// THE DEFECT THIS CLOSES (measured, epic binary-summary-guard + its gopilog
// sibling): a bare-prompt run whose positional named an existing non-image
// file made that file's bytes the prompt (readInput reads such a positional as
// prompt TEXT). For `gopi gopi help` run inside a directory holding the built
// ./gopi, the prompt WAS an ELF executable. The bytes then reached every
// renderer that prints captured prompt bytes:
//
//   - the ledger: `gopi task list --status failed` emitted `data` (a row whose
//     summary held \x7fELF…);
//   - the session views: `gopi session log <id> --todo` / `--session` exit 0 and print
//     s.Task verbatim, so the same 15 NULs land on the terminal, and
//     `file -` calls the output "data".
//
// CONTRACT (oracle-safe — the sanitizer, never the renderer, decides):
//
//   - IDENTITY for ordinary text: CJK, emoji, punctuation, quotes pass
//     byte-for-byte, so a legitimate briefing is never rewritten;
//   - a maximal run of non-text bytes collapses to ONE placeholder carrying the
//     run's leading bytes ("[bin \x7fELF\x02\x01…]"): the output stays readable
//     AND the evidence stays visible. The magic bytes are the one honest trace
//     that a binary was captured — a silent strip would destroy the only clue
//     an operator has for finding the upstream cause (Rule 15, no silent
//     failure);
//   - the result is always valid UTF-8, NUL-free and (when a ceiling is given)
//     within it.
package textguard

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// Marker labels an escaped non-text run in a rendered line ("[bin …]").
	Marker = "bin"

	// HeadBytes is how many leading bytes of a non-text run are rendered
	// verbatim (as \xNN). Enough to identify a magic number (\x7fELF, %PDF,
	// \x89PNG) and short enough that a megabyte of binary cannot bloat the
	// rendered line.
	HeadBytes = 8

	// DominancePercent is the share of non-text bytes at or above which a
	// string is treated as a CAPTURED BLOB rather than as text with corruption
	// in it. Below it, each run is escaped IN PLACE (the escaped form keeps the
	// text's shape — "…resolution\x00steps…"); at or above it, a binary read as
	// a whole (an executable, a core dump) would shatter into dozens of markers
	// interleaved with its own byte fragments, which is noise, not a headline.
	// The blob form then reports ONE marker carrying the magic head.
	DominancePercent = 50

	// percentScale is the denominator of the dominance test above (one
	// definition, so the percentage can never be read as a fraction).
	percentScale = 100

	// printableLow/printableHigh bound the ASCII range whose bytes the blob
	// marker prints as themselves, so a signature stays recognisable
	// ("\x7fELF", "%PDF") instead of becoming unreadable \xNN pairs.
	printableLow  = 0x20
	printableHigh = 0x7e

	// blobCollapseMinBytes is the size at or above which a SPAN-dominant string
	// is reported as a blob (see blobParts). Below it the in-place escape runs,
	// preserving every byte of text — the fidelity the ledger's summary and
	// payload columns depend on. 1 MiB is comfortably above any text briefing a
	// human reads on a terminal and below the measured 21 MB captured-binary
	// briefing whose in-place render was 72 MB of markers.
	blobCollapseMinBytes = 1 << 20

	// Ellipsis marks BOTH a truncated line and a non-text run longer than the
	// rendered head — the standard "there was more" mark.
	Ellipsis = "…"
)

// IsPrintableRune reports whether r may appear in rendered captured text.
// Tab/CR/LF ARE printable here and are folded to a space by the single-line
// sanitizer (they are whitespace, not corruption); every other control, format
// (Cf), surrogate and unassigned rune is not text.
func IsPrintableRune(r rune) bool {
	switch r {
	case '\t', '\n', '\r':
		return true
	}
	// A SPACE SEPARATOR (unicode.Zs) is one cell of whitespace, so it is text:
	// U+00A0, U+2000..U+200A, U+202F, U+205F, U+3000.
	//
	// WHY THIS IS NOT FOLDED INTO unicode.IsPrint (measured 2026-11-03, task
	// 01a1a0ed): unicode.IsPrint answers false for EVERY Zs rune except the
	// ASCII space, so this package used to call a no-break space binary
	// corruption. That was not academic — gopi's own chat status row separates
	// its state word from its animation with U+00A0 on purpose
	// (cmd/agent/chat_stream.go statusLiveInFlightGap: the row's separator is
	// U+0020, so a field value carrying that byte would be cut in half), so
	// pasting one status row into a prompt made the ledger render
	// "resp[bin \xc2 \xa0]" where the operator had simply written "resp ".
	// The same held for the IDEOGRAPHIC SPACE U+3000 that a Chinese IME
	// produces. Identity for ordinary text is this package's first contract
	// clause, and a space is ordinary text.
	//
	// The other unicode.IsSpace classes stay NON-text on purpose: U+0085 (Cc),
	// U+2028/U+2029 (Zl/Zp) and the C0 controls \v/\f are LINE STRUCTURE, and a
	// line separator inside a single line would shear the geometry a renderer
	// laid out. Zs is the class that occupies a cell without ending a line.
	if unicode.Is(unicode.Zs, r) {
		return true
	}
	// unicode.IsPrint is the single definition of "printable" this package
	// uses: C0/C1 controls, DEL and the invisible format classes are excluded,
	// while letters, marks, numbers, punctuation, symbols and spaces pass.
	return unicode.IsPrint(r)
}

// IsText reports whether s is text a renderer may print as-is: it decodes as
// valid UTF-8 with no invalid byte and no rune IsPrintableRune rejects. The
// empty string is text. This is the fast, allocation-free face of the guard —
// a renderer asks it first and only pays for the sanitizer when it says no.
func IsText(s string) bool {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		// A single-byte RuneError IS an invalid byte (DecodeRuneInString
		// reports the width, so a genuine U+FFFD in valid text — width 3 — is
		// not mistaken for corruption).
		if (r == utf8.RuneError && size == 1) || !IsPrintableRune(r) {
			return false
		}
		i += size
	}
	return true
}

// Sanitize folds any string into rendered text.
//
//   - maxRunes > 0 caps the rune count; 0 means unlimited.
//   - singleLine folds \n/\t/\r to a space; false PRESERVES line structure
//     (only non-text bytes are escaped, so an indented/YAML/JSON block survives
//     intact).
//
// Non-text bytes are escaped in maximal runs, in place, so the escaped form
// keeps its position in the text (the ELF marker stays where the binary
// started — the line still reads "[auto] [bin \x7fELF…]").
func Sanitize(s string, maxRunes int, singleLine bool) string {
	if s == "" {
		return s
	}
	if prefix, head, count, suffix, dominant := blobParts(s); dominant {
		// Predominantly non-text: the string IS a captured blob (the measured
		// case — an ELF image read as a text input file). Report it as one
		// marker instead of interleaving dozens of fragments.
		return joinBounded([]string{string(prefix), escapeBlob(head, count), suffix}, maxRunes, singleLine)
	}
	var b strings.Builder
	b.Grow(len(s) + HeadBytes)
	head := make([]byte, 0, HeadBytes)
	runBytes := 0
	flushRun := func() {
		if runBytes == 0 {
			return
		}
		b.WriteString(escapeRun(head, runBytes))
		head = head[:0]
		runBytes = 0
	}
	runes := 0
	truncated := false
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isNonTextRune(r, size) {
			if len(head) < HeadBytes {
				head = append(head, s[i:i+min(size, HeadBytes-len(head))]...)
			}
			runBytes += size
			i += size
			continue
		}
		flushRun()
		if maxRunes > 0 && runes >= maxRunes {
			truncated = true
			break
		}
		if singleLine && (r == '\n' || r == '\r' || r == '\t') {
			b.WriteByte(' ')
		} else {
			b.WriteString(s[i : i+size])
		}
		runes++
		i += size
	}
	flushRun()
	if truncated {
		b.WriteString(Ellipsis)
	}
	return strings.TrimSpace(b.String())
}

// joinBounded assembles the blob form's parts and applies the same
// ceiling/whitespace contract as the run form.
func joinBounded(parts []string, maxRunes int, singleLine bool) string {
	joined := strings.Join(parts, "")
	if singleLine {
		joined = strings.Join(strings.Fields(strings.ReplaceAll(joined, "\n", " ")), " ")
	}
	joined = strings.TrimSpace(joined)
	if maxRunes <= 0 {
		return joined
	}
	r := []rune(joined)
	if len(r) > maxRunes {
		return strings.TrimSpace(string(r[:maxRunes])) + Ellipsis
	}
	return joined
}

// blobParts measures a candidate string and reports the blob form's pieces:
// the leading text before the first non-text byte, the escaped binary's magic
// head and byte count, the trailing text after the LAST non-text byte, and
// whether non-text bytes DOMINATE the string (the DominancePercent test — by
// count or by span, see below). The measurement reads the string once and is
// pure.
func blobParts(s string) (prefix, head []byte, count int, suffix string, dominant bool) {
	firstBad, lastBad := -1, -1
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isNonTextRune(r, size) {
			if firstBad < 0 {
				firstBad = i
			}
			lastBad = i + size
			count += size
			if len(head) < HeadBytes {
				head = append(head, s[i:i+size]...)
			}
		}
		i += size
	}
	if firstBad < 0 {
		return nil, nil, 0, "", false
	}
	// Two ways a string IS a captured blob rather than text with corruption in
	// it, both measured on the same read:
	//   - the non-text bytes are a large SHARE of the string (count), and
	//   - the non-text bytes SPAN most of the string AND the string is large
	//     (span), which is the shape a binary read as prompt text has: the
	//     executable's printable ASCII interleaved with its NULs, so the count
	//     can stay far below the share while the corruption covers everything.
	//     Measured case (2026-09-15): the real briefing of the 87 MB session is
	//     21,690,123 bytes holding 3,372,545 NULs (15%) spanning the whole
	//     string; escaped in place it rendered 72 MB of markers (proven on the
	//     real session), collapsed as a blob it is one line.
	// The size floor on the span rule is deliberate: below it the in-place
	// escape is affordable, so EVERY byte of text is preserved and a legitimate
	// payload that merely starts and ends with one corrupt byte is not gutted
	// (summary/payload fidelity is what makes this guard safe for the ledger's
	// `task show`).
	spanPercent := (lastBad - firstBad) * percentScale / len(s)
	if count*percentScale < len(s)*DominancePercent &&
		!(spanPercent >= DominancePercent && len(s) >= blobCollapseMinBytes) {
		return nil, nil, 0, "", false
	}
	prefix = []byte(s[:firstBad])
	// The blob's head is the RAW leading bytes of the binary span (not only its
	// non-text ones): that is what carries the magic number a reader identifies
	// the format by (\x7fELF, %PDF, \x89PNG) — the in-place RUN escape collects
	// non-text bytes only, but here the printable bytes of the signature are
	// the whole point.
	blobEnd := min(firstBad+HeadBytes, lastBad)
	head = []byte(s[firstBad:blobEnd])
	if lastBad < len(s) {
		suffix = s[lastBad:]
	}
	return prefix, head, count, suffix, true
}

// isNonTextRune reports whether one decoded rune is corruption for rendered
// text: an INVALID byte (a single-byte RuneError — a genuine U+FFFD has width
// 3 and is not mistaken for one) or a rune IsPrintableRune rejects.
func isNonTextRune(r rune, size int) bool {
	return (r == utf8.RuneError && size == 1) || !IsPrintableRune(r)
}

// escapeBlob renders the blob form's marker: the magic head, e.g.
// "[bin \x7fELF\x02\x01…]". A printable ASCII byte of the signature prints as
// itself (that is how "ELF" is recognisable); every other byte prints as \xNN.
func escapeBlob(head []byte, count int) string {
	var b strings.Builder
	b.WriteString("[")
	b.WriteString(Marker)
	b.WriteString(" ")
	for _, c := range head {
		if c >= printableLow && c <= printableHigh {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "\\x%02x", c)
	}
	if count > len(head) {
		b.WriteString(Ellipsis)
	}
	b.WriteString("]")
	return b.String()
}

// escapeRun renders ONE maximal non-text run as its placeholder:
// "[bin \x7f\x45\x4c\x46\x02\x01\x01\x00…]" — the marker, the run's leading
// bytes (up to HeadBytes) as \xNN, and an ellipsis when the run continued past
// the head.
func escapeRun(head []byte, total int) string {
	var b strings.Builder
	b.WriteString("[")
	b.WriteString(Marker)
	for _, c := range head {
		fmt.Fprintf(&b, " \\x%02x", c)
	}
	if total > len(head) {
		b.WriteString(Ellipsis)
	}
	b.WriteString("]")
	return b.String()
}
