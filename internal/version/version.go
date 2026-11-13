// Package version — report parsing: the shapes a gopi binary answers
// `--version` with, and the two predicates a consumer needs from them.
//
// WHAT THIS FILE IS FOR, after the epic that made the version internal
// (devdoc/version-identity.md). `gopi version` now prints the compiled-in
// version (self.go) followed by the build metadata the Go toolchain embedded
// in the artifact. The post-merge rebuild verifier (internal/tools/rebuild.go)
// asked a freshly built binary `--version` and must compare the REVISION it
// reports against the master tip — so it needs (a) the revision out of
// whatever shape answers, and (b) the dirty witness, because a binary built
// from a dirty tree does not represent the committed tip and must be refused.
// Both live here, in ONE place, because a producer and a reader that disagree
// about a report format is how a correct build got reported as a version
// mismatch and a good merge got auto-reverted (epic 2026-09-13).
//
// TOLERANCE IS DELIBERATE, and each tolerated shape has a live caller: a
// STALE binary in the repo is exactly what the verifier is invoked on, so the
// older shapes cannot simply be dropped.
//
//	"revision: <sha>"        ← this package's producer today (ReportRevisionKey)
//	"<sha>"                  ← a bare revision, the pre-report fallback
//	"v20260918-0932 (sha12)" ← a display an OLDER binary still prints
//	"<sha>-dirty"            ← the older dirty witness
//	"modified: true"         ← this package's dirty witness today
//
// What is NOT here any more — and the reason is that nothing in the live tree
// read them: the minted-name machinery (a `version/*` tag minted on every
// merge round), the name-borrowing decision (a binary taking its name from the
// checkout it stands in), the `describe` parsers that fed both, and the
// multi-line DESCRIPTION renderer those produced. They were deleted whole
// rather than kept behind a flag, because a dormant mint is the coupling this
// epic exists to remove. The 183 `version/*` tags those rounds created are
// untouched history; Namespace below is what a fixture uses to write one.
package version

import "strings"

// Namespace is the git tag namespace the retired mint wrote version tags
// under ("version/"). It is kept — and exported — because the readonly tag
// fixtures that pin the display/report readers tag their positions with it
// (internal/tools, cmd/agent), and a fixture that spelled the namespace by
// hand would be a second source of truth for a value this package names.
//
// NOTHING IN PRODUCTION WRITES IT: the mint is deleted, so the 183 existing
// `version/*` tags are history a repo owner may keep, retag or delete as
// their own release decision (see devdoc/version-identity.md § the 183).
const Namespace = "version/"

// DirtySuffix marks a tree that had uncommitted tracked modifications when
// the binary was built — as a suffix on a DISPLAY ("<rev>-dirty", the shape
// older binaries print). It is exported because the rebuild verifier's tests
// compose producer-shaped displays with it and cannot import a private
// constant to do it. The CURRENT producer states the same fact as its own
// report line (MetadataModifiedKey); both are witnessed by IsDirty.
const DirtySuffix = "-dirty"

// MetadataModifiedKey labels the report line witnessing a dirty build, and
// MetadataModifiedTrue is the value that makes it a claim. They are exported
// because each side of the wire is written in a different package (cmd/agent
// composes the report, this package parses it) and a producer/reader
// disagreement silently loses the witness (Rule 13/14).
const (
	MetadataModifiedKey  = "modified:"
	MetadataModifiedTrue = "true"
)

// ReportRevisionKey labels the line carrying a revision. It is the key the
// rebuild verifier recovers provenance from, so it is the one report key with
// a production reader on both sides; cmd/agent composes the line and
// RevisionOfDisplay reads it.
const ReportRevisionKey = "revision:"

// ShortRevisionLenText is the revision width gopi's displays carry, as the
// decimal string a NON-Go producer needs on the wire (a test fixture's
// `git rev-parse --short=<n>`, a shell probe). The Go-side width lives in
// cmd/agent; this constant exists so the fixture and the probe cannot frame a
// revision differently from the display they are imitating.
const ShortRevisionLenText = "12"

const (
	// shortRevisionLen is how much of a 40-hex revision a display carries.
	shortRevisionLen = 12
	// minUniqueRevisionLen is git's own abbreviation floor (`--short` never
	// returns fewer than 7 hex chars, because fewer are not unique).
	minUniqueRevisionLen = 7
	// fullRevisionLen is a complete SHA-1 revision.
	fullRevisionLen = 40
	// displayOpen/displayClose frame the revision group in the OLDER display
	// shape: "v20260913-0830 (177375b5a6be)".
	displayOpen  = " ("
	displayClose = ")"
)

// RevisionOf decomposes a single-line version display back into the revision
// it carries, whether the tree was dirty, and whether a revision was
// recognized at all. Both historically produced shapes are accepted:
//
//	"v20260913-0830 (177375b5a6be)"  → revision group
//	"177375b5a6be-dirty"             → the legacy bare shape
//
// It is the inverse of the formatting an older gopi printed, and it is kept
// in this package so that FORMAT has exactly one owner (Rule 13: no consumer
// re-parses a layout).
func RevisionOf(display string) (rev string, dirty bool, ok bool) {
	if display == "" {
		return "", false, false
	}
	body := display
	if open := strings.LastIndex(body, displayOpen); open >= 0 && strings.HasSuffix(body, displayClose) {
		// Tagged shape: the revision is the parenthesised group, and the
		// dirty marker decorates the NAME (left of the open paren) — it can
		// not be read off the string's tail: a minted tag name itself ends
		// in the short revision, so the name half is the ONLY trustworthy
		// witness that this string is dirty.
		dirty = nameHalfIsDirty(display[:open])
		body = body[open+len(displayOpen) : len(body)-len(displayClose)]
	} else {
		// Legacy bare shape: the whole string is the revision, so the
		// marker is its suffix.
		dirty = strings.HasSuffix(display, DirtySuffix)
		body = strings.TrimSuffix(body, DirtySuffix)
	}
	if !isRevision(body) {
		return "", dirty, false
	}
	return body, dirty, true
}

// nameHalfIsDirty reports whether a display's NAME half (the text left of the
// revision group) carries the dirty marker. The marker is the token ending in
// DirtySuffix, not a suffix of the whole name: a producer appended an unminted
// marker after it ("v1.0.0+2-dirty unminted"), so a position-based test is the
// wrong question — the producer's own token is what is asked.
func nameHalfIsDirty(nameHalf string) bool {
	for _, f := range strings.FieldsFunc(nameHalf, func(r rune) bool {
		return r == ' ' || r == '(' || r == ')'
	}) {
		if strings.HasSuffix(f, DirtySuffix) {
			return true
		}
	}
	return false
}

// IsDirty reports whether a DISPLAY or REPORT says the binary was built from
// a dirty tree — the single owner of the dirty witness (Rule 13/14), so a
// consumer that must reject a dirty answer never re-derives the predicate
// from a string tail it guessed at.
//
// Both witness shapes are checked because the answer may come from either
// producer generation:
//
//	"v20260918-0932-8380ce9caeef-dirty (8380ce9caeef)"   ← name, not the tail
//	"8380ce9caeef-dirty"                                 ← legacy bare shape
//	"modified: true"                                     ← the report line today
//
// A tail-only HasSuffix check reads the FIRST string as clean — the name half
// ends in the short revision it borrows, so the tail is ")" — and the rebuild
// verifier then accepted a dirty build's revision as the committed tip.
func IsDirty(displayOrReport string) bool {
	for _, line := range strings.Split(displayOrReport, "\n") {
		if strings.Contains(line, DirtySuffix) {
			return true
		}
		if key, value, found := strings.Cut(line, ":"); found &&
			strings.TrimSpace(key)+":" == MetadataModifiedKey &&
			strings.TrimSpace(value) == MetadataModifiedTrue {
			return true
		}
	}
	return false
}

// RevisionOfDisplay recovers the REVISION a version answer carries, in any of
// the shapes this file accepts (see RevisionOf) or a bare revision. It is the
// consumer-facing half of RevisionOf: a caller comparing two answers compares
// PROVENANCE, so it needs to know whether the string it holds carries a
// revision at all — RevisionOfDisplay(""), a name with no revision group and
// a prose line all answer ok=false, while "revision: <sha>", the display of an
// older binary and a bare "<sha>" answer the revision itself.
//
// The LIVE caller is internal/tools/rebuild.go's binaryVersion: it probes a
// freshly built artifact with `--version` and must compare what comes back
// against the integration branch's tip, whatever generation of gopi wrote
// that answer. Handing a WHOLE report to the single-line RevisionOf is the
// defect this closes: that parser read one line only, recovered no revision
// from a multi-line answer, and a perfectly correct build was reported as a
// version mismatch (epic 2026-09-13, the post-merge rebuild auto-reverting
// every tagged merge).
func RevisionOfDisplay(report string) (rev string, ok bool) {
	if report == "" {
		return "", false
	}
	for _, line := range strings.Split(report, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		body := line
		if key, value, found := strings.Cut(line, ":"); found &&
			strings.TrimSpace(key) == strings.TrimSuffix(ReportRevisionKey, ":") {
			body = strings.TrimSpace(value)
		}
		if rev, _, ok := RevisionOf(body); ok {
			return rev, true
		}
		if isRevision(body) {
			return body, true
		}
	}
	return "", false
}

// isRevision reports whether s looks like a git revision (abbreviated or
// full SHA-1). It is the guard that keeps RevisionOf from mistaking a stamp
// or a human-authored name for a revision.
func isRevision(s string) bool {
	if len(s) < minUniqueRevisionLen || len(s) > fullRevisionLen {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
