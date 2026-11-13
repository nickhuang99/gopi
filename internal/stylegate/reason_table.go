package stylegate

import (
	"fmt"
	"sort"
	"strings"
)

// reason_table.go owns the SPELLING of a per-site reason key, and the refusal of
// every GROUP shape the annotate tool used to accept.
//
// It lives in the package rather than in the tool because the vocabulary has two
// readers: the annotator (which writes markers from it) and the arms in
// per_site_reason_redproof_test.go (which pin that a callee-wide or whole-file
// key cannot clear a site). One definition, two readers — Rule 14.
//
// The mechanism it retires: the table fell back from `file:line` to
// `file::callee` to a bare `callee`, so one JSON line silenced EVERY call to that
// function. On the ground this change was measured at, 48 of 64 sites were
// cleared by ~30 callee keys, which is why "justify this discard" degraded into
// "name the callee once". A group reason is not a reason.

// SiteKeySep separates a table key's file from its line. A table whose keys are
// not `path:line` is refused outright, so the separator is one decision.
const SiteKeySep = ":"

// SiteKey is the ONLY key shape the reason table accepts: one site, by file and
// line. Named rather than formatted at each call site so the table and the
// lookup cannot drift apart (Rule 1/13).
func SiteKey(file string, line int) string {
	return fmt.Sprintf("%s%s%d", file, SiteKeySep, line)
}

// refusals for the two GROUP shapes the table used to accept. They are the
// diagnosis half of "a group reason is not a reason": the message names the key
// and what it would have silenced, because a bare "invalid key" leaves the
// author to guess which rule they hit.
const (
	// errTableCalleeKey refuses `callee` and `file::callee`: both name a GROUP.
	errTableCalleeKey = "reason table key %q names a GROUP, not a site: a " +
		"callee-wide justification would clear every call to that function — " +
		"the shape this table retired; write one key per site as `file.go:LINE`"
	// errTableFileKey refuses `path/to/file.go` with no line: a whole-file
	// exemption is the same defect one level up.
	errTableFileKey = "reason table key %q names a whole FILE, not a site: " +
		"an exemption that covers every discard in a file cannot say why any " +
		"one of them is deliberate; write one key per site as `file.go:LINE`"
	// errTableLineOnly refuses a bare line number, which has no file to be a
	// site IN.
	errTableLineOnly = "reason table key %q names a line but no file; write one " +
		"key per site as `file.go:LINE`"
)

// IsSiteKey matches a key that ENDS in `:<digits>` — the only shape
// SiteKey produces. It is a predicate over the spelling, so a table entry that
// merely contains a colon (`file::callee`) is not mistaken for a site.
func IsSiteKey(k string) bool {
	i := strings.LastIndex(k, SiteKeySep)
	if i <= 0 || i == len(k)-1 {
		return false
	}
	for _, r := range k[i+1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isFileKey reports whether a rejected key names a FILE (a path with a
// separator, or a `.go` suffix). It decides which refusal message fits, never
// whether the key is accepted — that is IsSiteKey's single question.
func isFileKey(k string) bool {
	return strings.Contains(k, "/") || strings.HasSuffix(k, goSourceSuffix)
}

// goSourceSuffix is the suffix that makes a key look like a source file.
const goSourceSuffix = ".go"

// isLineNumberKey reports whether a rejected key is digits only: a line with no
// file to be a site in.
func isLineNumberKey(k string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ValidateReasonTable REFUSES a table carrying any key that is not a site key.
// It returns the group keys it found, so the caller can name them all rather
// than the first one.
//
// The check is a REFUSAL and not a filter: dropping the bad keys silently would
// leave the author believing their justification applied, which is the same
// silence-by-wrong-mechanism this rule exists to remove.
func ValidateReasonTable(table map[string]string) error {
	var bad []string
	for k := range table {
		if !IsSiteKey(k) {
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	msgs := make([]string, 0, len(bad))
	for _, k := range bad {
		switch {
		case isFileKey(k):
			msgs = append(msgs, fmt.Sprintf(errTableFileKey, k))
		case isLineNumberKey(k):
			msgs = append(msgs, fmt.Sprintf(errTableLineOnly, k))
		default:
			// Everything else names a CALLEE: a bare `Atoi`, a qualified
			// `strconv.Atoi`, or the `file::callee` spelling. They are one
			// class — a GROUP — and one message.
			msgs = append(msgs, fmt.Sprintf(errTableCalleeKey, k))
		}
	}
	return fmt.Errorf("reason table carries %d non-site key(s):\n  %s",
		len(bad), strings.Join(msgs, "\n  "))
}
