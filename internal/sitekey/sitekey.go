// Package sitekey renders a source SITE as a baseline key that survives an edit
// ABOVE it.
//
// WHY THIS PACKAGE EXISTS. The machine-checkable gates in internal/standards
// carry baselines — allow-lists of already-known violations — and until
// 2026-10-16 every entry was a bare `file:line` key. That key is a function of
// the LINE NUMBER, so any edit above a baselined site rots its entry: the site
// is still the same violation, but its key names a line that now holds
// something else. One defect is then reported TWICE by one gate — a NEW
// violation at the site's new line and a STALE baseline entry at its old one —
// and the remedy available at that moment (re-key `:155` to `:166`) is
// per-instance, so the same shape reappears at the next edit. Measured
// twice in one day: internal/docclass/anchor_number_material_test.go moved
// :155 -> :166, and the moment that was repaired
// internal/tools/git_child_locale_test.go moved :243 -> :248 (see
// devdoc/standards-baseline-verdict-content-key.md).
//
// THE KEY. A site is identified by WHAT IS ON IT, not by where it is:
//
//	<path> \t <normalized line text> \t #<ordinal>
//
//   - path — repo-relative, slash-separated: the same string the scan and the
//     baseline always used;
//   - normalized line text — the offending line with every whitespace run
//     collapsed to one space and the ends trimmed (Normalize). Indentation and
//     gofmt's own column alignment are therefore NOT part of the identity, so a
//     de-indent or a re-aligned comment does not rot the entry;
//   - ordinal — 1-based, the position of this line among the lines of the SAME
//     file whose normalized text is identical. It exists because two identical
//     lines in one file (a repeated fixture line, a table row) must get two
//     DIFFERENT keys, or one permit would silently cover both.
//
// The line NUMBER is deliberately absent from the key, which is the whole
// point: the key is a function of the file's CONTENT at the site, so an
// insertion or a deletion anywhere above it leaves the key unchanged.
//
// WHAT THE KEY STILL ROTS ON — stated so a reader does not mistake the key for
// immunity it does not have:
//
//   - the site's own text is EDITED (the line itself changes) — the key moves,
//     the entry reads STALE, and re-keying is a content change that a reviewer
//     reads;
//   - an EARLIER identical sibling line is deleted, which renumbers this line's
//     ordinal (#2 -> #1);
//   - an earlier identical sibling is INSERTED, which pushes this one down
//     (#1 -> #2).
//
// All three are content edits at or beside the site, and all three report
// LOUDLY as a STALE entry plus a NEW violation — never as a silent pass. The
// only thing the key makes invisible is a line-number shift, which was never
// the site's identity in the first place.
//
// The key is DATA, produced by exactly one function (Key) and read back by
// exactly two accessors (File, Text), so the gate that writes a permit and the
// generator that re-keys a baseline cannot disagree about its shape
// (PROJECT.md Rule 14: a value shared across packages has one definition, in a
// small leaf package every consumer can import).
package sitekey

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// FieldSep separates the key's three fields. A TAB cannot occur inside a
	// field: the path is slash-separated and Normalize collapses every run of
	// whitespace (TAB included) to a single space.
	FieldSep = "\t"

	// OrdinalPrefix marks the ordinal field's payload, so a reader can tell
	// `#2` apart from a line of source that happens to end in a number.
	OrdinalPrefix = "#"

	// fieldCount is how many fields a well-formed key carries — path, text,
	// ordinal — and it is asserted, because a key of the wrong arity is a
	// malformed permit and must be reported rather than silently unmatched
	// (PROJECT.md Rule 15: no silent failure).
	fieldCount = 3

	// FirstOrdinal is the ordinal of a site whose normalized text occurs once
	// in its file.
	FirstOrdinal = 1

	// KeyCreatedBy names the one command that produces keys, spelled here so
	// the generator and every error message share it (Rule 14).
	KeyCreatedBy = "go run internal/standards/testdata/baseline-rekey.go"

	// whitespace is the run Normalize collapses: it is the space the fields of
	// a normalized line are joined with.
	whitespace = " "
)

// Errors returned by Key. They are named (not formatted inline) so a caller can
// branch on them and so a test can assert the exact refusal.
var (
	// ErrLineOutOfRange means the site names a line the file does not have —
	// the file changed under the caller, or the permit was written by hand.
	ErrLineOutOfRange = errors.New("sitekey: line is outside the file")

	// ErrLineBlank means the site's line has no text once whitespace is
	// collapsed. A blank line carries no identity, so a hit reported on one is
	// a defect of the PREDICATE that reported it, not of the baseline: it must
	// fail loudly instead of minting a key every blank line in the file would
	// share.
	ErrLineBlank = errors.New("sitekey: the line is blank — a blank line carries no site identity")
)

// Normalize renders one source line as the key's identity: every run of
// whitespace collapsed to a single space, both ends trimmed.
//
// Collapsing (rather than trimming alone) is what makes the key immune to the
// two cosmetic edits that are common above and AT a site: a de-indent, and the
// column alignment gofmt applies to a run of trailing comments. The line's
// TOKENS are untouched — an edit that changes what the line says changes the
// key, which is intended.
func Normalize(line string) string {
	return strings.Join(strings.Fields(line), whitespace)
}

// Key renders the site at src's 1-based line as its baseline key.
//
// The ordinal is the 1-based position of this line among the file's lines whose
// Normalize equals this line's, in file order — so two identical lines get two
// different keys, and a site whose text is unique gets FirstOrdinal. Blank
// lines are never counted (they have no identity), and a site ON a blank line
// is refused with ErrLineBlank.
//
// src may be the whole file (the normal case) or any prefix that contains the
// line; only content UP TO AND INCLUDING the site decides the ordinal, so a
// caller that reads lazily still gets the same key the full-file scan mints.
func Key(path string, line int, src []byte) (string, error) {
	lines := strings.Split(string(src), "\n")
	if line < FirstOrdinal || line > len(lines) {
		return "", fmt.Errorf("%w: %s:%d (the file has %d line(s))",
			ErrLineOutOfRange, path, line, len(lines))
	}
	text := Normalize(lines[line-FirstOrdinal])
	if text == "" {
		return "", fmt.Errorf("%w: %s:%d", ErrLineBlank, path, line)
	}
	ordinal := 0
	for i := 0; i < line; i++ {
		if Normalize(lines[i]) == text {
			ordinal++
		}
	}
	return path + FieldSep + text + FieldSep + OrdinalPrefix + strconv.Itoa(ordinal), nil
}

// File returns the repo-relative path a key names, or "" when key is not a
// well-formed key (the caller decides whether that is a defect of the data or
// of its own parsing).
func File(key string) string {
	fields := strings.Split(key, FieldSep)
	if len(fields) != fieldCount {
		return ""
	}
	return fields[0]
}

// Text returns the normalized line text a key names, or "" when key is not a
// well-formed key.
func Text(key string) string {
	fields := strings.Split(key, FieldSep)
	if len(fields) != fieldCount {
		return ""
	}
	return fields[1]
}

// Ordinal returns the ordinal a key names, or 0 when key is not a well-formed
// key.
func Ordinal(key string) int {
	fields := strings.Split(key, FieldSep)
	if len(fields) != fieldCount {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimPrefix(fields[2], OrdinalPrefix))
	if err != nil {
		return 0
	}
	return n
}

// WellFormed reports whether key has the shape Key produces: a path, a
// non-empty text, and a positive ordinal. A malformed entry in a baseline is a
// permit that can never match — it must be reported, never left to read as a
// quiet exemption (Rule 15).
func WellFormed(key string) bool {
	return File(key) != "" && Text(key) != "" && Ordinal(key) >= FirstOrdinal
}

// Locate returns the 1-based line on which src still carries key's text, or 0
// when it carries none.
//
// It exists for the STALE half of a gate's report: when an entry matches no
// hit, the useful sentence is not "this permit is stale" but "the text this
// permit names is still at <path>:<line>, and the gate no longer fires there" —
// which is the difference between "the site was repaired" and "the predicate
// stopped seeing it". The ordinal is honoured when src carries a line whose text
// equals key's and whose own generated key equals key exactly; otherwise the
// first line carrying the text is reported, because the exact ordinal moved and
// that IS the diagnosis.
func Locate(src []byte, key string) int {
	text := Text(key)
	if text == "" {
		return 0
	}
	first := 0
	lines := strings.Split(string(src), "\n")
	for i := 0; i < len(lines); i++ {
		if Normalize(lines[i]) != text {
			continue
		}
		line := i + FirstOrdinal
		if first == 0 {
			first = line
		}
		if k, err := Key(File(key), line, src); err == nil && k == key {
			return line
		}
	}
	return first
}
