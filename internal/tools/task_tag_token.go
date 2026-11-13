package tools

import "strings"

// taskHasTagToken reports whether a row's comma-separated tag list carries
// `want` as a WHOLE tag.
//
// It matches on the token, never on a substring: a row tagged
// `delivery-chain` must not answer yes to a probe for `chain`, and a probe for
// `bug` must not light up on `debug`. That distinction is the reason this
// helper exists at all rather than a `strings.Contains` at each call site —
// the tag vocabulary is a closed set and a caller asking about a member is
// asking about exactly that member.
//
// It lived in task_verify_closure.go until that file was deleted on
// 2026-10-25 (the operator removed the program's self-verification gate); the
// helper never belonged to that mechanism — ten other files use it for tag
// routing — so it moved here unchanged instead of being re-typed per caller
// (Rules 13/14: one definition, referenced everywhere).
func taskHasTagToken(tags, want string) bool {
	for _, t := range strings.Split(tags, ",") {
		if strings.TrimSpace(t) == want {
			return true
		}
	}
	return false
}
