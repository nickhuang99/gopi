package devworkspace

import "strings"

// THE CHANNEL'S ONE PREFIX, AND THE ONE PLACE IT IS COMPOSED.
//
// Every line this channel writes about itself — the operator's log line, the
// ledger row's reason, a refusal returned as an error — opens with the SAME
// words, "clone channel: ", and an operator reads that prefix as the identity of
// the speaker (this channel) rather than part of the sentence. Rule 1/13: the
// spelling lives HERE and nowhere else; every other mention (this package, cmd/
// agent, internal/agent, internal/tools) takes the value from this constant.
//
// WHY THE COMBINATOR EXISTS (measured defect, 2026-10-30). The bodies this
// channel composes do NOT share one convention. (*Blocker).Report() opens EVERY
// Kind branch with the prefix — that is what makes a lone report readable — and
// so do most of AttemptDelivery's errors, while ctx.Err(), a settings refusal
// and a sentence this channel writes do not. A call site that pasted the
// constant itself therefore DOUBLED it for the first group:
//
//	clone channel: delivery did not land in 3 attempt(s); last blocker (dirty): clone channel: refusing to pull: …
//
// measured on cmd/agent's exhausted() as PROBE_PREFIX_COUNT=2, and on the retry
// face (internal/tools) as `delivery owed: clone channel: clone channel: …`.
//
// The cure is not "delete the prefix at the call sites" — that trades a stutter
// for a line with no speaker at all — and it is not "make every body carry it" —
// that is a convention no single site can enforce. It is ONE idempotent
// composition point: trim, strip the prefix AT MOST ONCE, add it back ONCE.
// Count("clone channel: ") == 1 is then a property of the composition, not of
// each caller's memory.
const CloneChannelPrefix = "clone channel: "

// CloneChannelReasonNothing is what a channel line says when the BODY it was
// handed is empty: the prefix over nothing is not a reason (Rule 15 — a line
// that reads "clone channel: " alone tells a reader nothing about what happened).
const CloneChannelReasonNothing = "the channel reported no reason (an empty body — the report is the defect)"

// CloneChannelBody normalises a BODY for a channel line: trimmed, with the
// channel's prefix stripped AT MOST ONCE.
//
// IT IS IDEMPOTENT ON PURPOSE, and that is the whole mechanism. A body that
// already opens with the prefix (every (*Blocker).Report() does) comes back
// without it; a body that does not (ctx.Err(), a hand-written sentence) comes
// back unchanged; either way the caller that re-adds the prefix gets exactly one.
// Stripping at most once is deliberate: a line whose body legitimately QUOTES
// another channel line keeps that inner mention, which is information, and only
// the speaker's own leading prefix is removed.
func CloneChannelBody(body string) string {
	trimmed := strings.TrimSpace(body)
	return strings.TrimSpace(strings.TrimPrefix(trimmed, CloneChannelPrefix))
}

// CloneChannelReason composes ONE channel line: the prefix exactly once, then
// the body with any prefix of its own stripped.
//
// It is the ONLY composition site (Rule 14), so "how many times does
// `clone channel: ` appear" is decided HERE rather than by each caller's memory
// of which bodies arrive pre-prefixed. A caller with a format string passes the
// template's OUTPUT through this function — never the prefix-pasted sentence.
func CloneChannelReason(body string) string {
	why := CloneChannelBody(body)
	if why == "" {
		why = CloneChannelReasonNothing
	}
	return CloneChannelPrefix + why
}
