// Package fullid is the ONE source of truth for the rule that a task id or a
// session id is ALWAYS used in FULL.
//
// WHY THIS PACKAGE EXISTS (measured 2026-09-23). A rendered listing printed a
// TRUNCATED id (the first 8 characters of a uuid v7) and every resolution path
// accepted it back. On this host 2409 ledger rows produce prefixes that are not
// unique: `01a0b51d` alone names 35 rows, `01a0c039` names 2, `01a0aba1` names
// 2. The flake registry's owner column is an 8-hex id, and 2 of its 5 owner
// rows resolved through such a prefix. At the same time the token
// `gopi session log --last` printed (`2ab3facd`) is a prefix of a SESSION id, so it
// resolved to no ledger row either: the printed token was neither a usable task
// id nor a usable session id, it only LOOKED like one. A prefix is therefore not
// a convenience — it is an identifier that silently names more than one thing.
//
// The rule, in one sentence: a prefix is NEVER resolved; it is refused, and the
// refusal names every full id it could have meant. Small, leaf, exported — the
// internal/toolnames pattern (Rule 14) — so gopilog, tools and cmd/agent all
// compare against the SAME predicate instead of each spelling their own.
package fullid

import (
	"fmt"
	"strings"
)

const (
	// UUIDLen is the length of a canonical 8-4-4-4-12 uuid — the shape of
	// every auto-minted task id (uuid v7) and every session id.
	UUIDLen = 36

	// ShortPrefixLen is the length of the chunk a rendered listing used to
	// print in place of the id (the first dash-delimited group). It is kept
	// ONLY so a refusal can RECOGNISE that an operand was copied out of a
	// listing — never so anything may resolve one.
	ShortPrefixLen = 8

	// uuidPattern is the canonical uuid shape, as data rather than a regexp:
	// this package stays a zero-import leaf (Rule 14) apart from fmt/strings,
	// and TruncatedUUID walks this string position by position.
	uuidPattern = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"

	// fullIDHint is the shared tail of every refusal: the operator-facing half
	// that says what to type instead. One spelling for all three shapes.
	fullIDHint = "values are always used in FULL (36 characters, e.g. 01a0ce09-6395-7e7e-9f04-6ca51716c90b)"
)

// IsUUID reports whether s is a canonical lowercase 8-4-4-4-12 uuid.
func IsUUID(s string) bool {
	return len(s) == UUIDLen && matchesUUIDPattern(s)
}

// TruncatedUUID reports whether s is the canonical uuid shape CUT SHORT:
// ShortPrefixLen..UUIDLen-1 characters that match the uuid pattern as far as
// they go. A full uuid is not "truncated" (see IsUUID), and a legacy or custom
// id (`t-1788387314`, `t-proof-wanted`) never matches, because `t` is not a hex
// digit.
//
// This is what lets a refusal say WHY the operand was rejected ("a prefix, not
// an id") instead of the misleading "no such row".
func TruncatedUUID(s string) bool {
	if len(s) < ShortPrefixLen || len(s) >= UUIDLen {
		return false
	}
	return matchesUUIDPattern(s)
}

// matchesUUIDPattern reports whether s matches the canonical uuid shape as far
// as it goes (the caller decides how long s may be).
func matchesUUIDPattern(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if uuidPattern[i] == '-' {
			if c != '-' {
				return false
			}
			continue
		}
		if !isHexDigit(c) {
			return false
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

// IDKind names the class of identifier a refusal is about, so the message an
// operator sees says "session id" or "task id" rather than "id".
type IDKind string

const (
	// KindTask is a task ledger row id.
	KindTask IDKind = "task id"
	// KindSession is a session id (the session_start event's sessionId).
	KindSession IDKind = "session id"
)

// Refusal builds the error for an operand that is a truncated id rather than a
// full one. candidates are the full ids the operand could have meant, in the
// caller's order (they are sorted here so the message is stable).
//
// The message states two things on purpose: that the operand was NOT a full id
// and every identity it is ambiguous between, plus what to type instead — a
// refusal an operator cannot act on is how the old silent pick went unnoticed.
func Refusal(kind IDKind, id string, candidates []string) error {
	sorted := append([]string(nil), candidates...)
	sortStrings(sorted)
	switch len(sorted) {
	case 0:
		return fmt.Errorf("%s %q is not a full id — %s %s; a prefix is never resolved and always refused",
			kind, id, kind, fullIDHint)
	case 1:
		return fmt.Errorf("%s %q is not a full id — it is a prefix of %s; %s %s, so re-run with the complete id",
			kind, id, sorted[0], kind, fullIDHint)
	default:
		return fmt.Errorf("%s %q is not a full id — it is ambiguous: %d identities share that prefix (%s) — %s %s, so re-run with the complete id",
			kind, id, len(sorted), strings.Join(sorted, ", "), kind, fullIDHint)
	}
}

// CanBePrefix reports whether id has the shape of a canonical id CUT SHORT
// (see TruncatedUUID). Callers that resolve ids by scanning a candidate set use
// it only to CHOOSE THE MESSAGE: a truncated operand whose candidates are known
// is refused with them, while an unrelated operand (`zzzz`, a stale id) keeps
// the caller's ordinary "not found" answer. It is never a permission to resolve
// a prefix.
func CanBePrefix(id string) bool { return TruncatedUUID(id) }

// sortStrings is a tiny insertion sort: this package is a leaf and must not
// grow an import for the handful of candidate ids a refusal names. Deterministic
// output is the only requirement.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
