package tools

// THE OWED REASON CARRIES THE CHANNEL'S PREFIX EXACTLY ONCE — whoever composed
// the body.
//
// THE DEFECT (measured 2026-10-30 on a live retry, row 01a174c3's log):
//
//	delivery owed: clone channel: clone channel: push failed on attempt 1 and
//	the remote did not move (…)
//
// `devworkspace.Blocker.Report()` already opens every one of its Kind branches
// with `clone channel: ` (devworkspace.go's Report switch), and
// `devworkspace.AttemptDelivery`'s own errors do too (`clone channel: no build
// command is declared …`, the anchor gate's sentences, the push failure). The
// repair face's blocked arms then composed `cloneChannelFailurePrefix + body`
// on top — so every reason whose body came from the channel was prefixed
// TWICE, and a reader saw two sentences where one failure had happened.
//
// WHY THE FIX IS A COMPOSER AND NOT "DELETE THE CONSTANT AT THE blk.Report()
// SITES": the five blocked arms do not share a body source. Two paste
// `blk.Report()`, two paste `err.Error()` (whose returns are MOSTLY prefixed but
// not uniformly — `ctx.Err()` arrives bare), and three paste a sentence this
// file composes itself (which nobody prefixes). Deleting the constant at one
// group would leave that group bare and the others doubled — the two failure
// shapes trading places. So the ONE composer makes the prefix idempotent, and
// the invariant the tests below pin is the property a reader cares about: the
// prefix occurs EXACTLY ONCE.
//
// Rule 1/13: the prefix is the constant at its one definition site; no literal
// is typed here either.

import (
	"errors"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
)

// The three BODY SOURCES the five blocked arms actually feed the composer, each
// named so a red run says which source regressed.
func TestDeliveryRetryChannelReasonCarriesThePrefixExactlyOnce(t *testing.T) {
	// A devworkspace blocker: its Report() open every kind's sentence with the
	// prefix already (the source of the doubling).
	emptyBlocker := (&devworkspace.Blocker{Kind: devworkspace.BlockerKindEmpty}).Report()
	// The channel's own error: `AttemptDelivery`'s returns are composed with the
	// prefix at their own call sites, which is how the measured live line read.
	channelErr := errors.New(cloneChannelFailurePrefix + "push failed on attempt 1").Error()
	// A sentence this file composes: nothing upstream prefixes it.
	ownSentence := "the attempt reported neither a push nor a reason (detail: x)"
	// The declaration refusal: `deliveryRetryCloneConfig` composes
	// `<settings key> unreadable: …` with no channel wording of its own.
	declaration := config.DevRepoSettingsKey + " unreadable: boom"

	cases := []struct {
		name string
		body string
	}{
		{"a blocker's Report() (already prefixed)", emptyBlocker},
		{"the channel's own error (already prefixed)", channelErr},
		{"this file's own sentence (bare)", ownSentence},
		{"the declaration refusal (bare)", declaration},
	}
	for _, c := range cases {
		got := deliveryRetryChannelReason(c.body)
		if n := strings.Count(got, cloneChannelFailurePrefix); n != 1 {
			t.Errorf("%s: the channel prefix must occur exactly once, count=%d: %q", c.name, n, got)
		}
		if !strings.HasPrefix(got, deliveryRetryOwedMarker) {
			t.Errorf("%s: the reason must still open with the owed marker %q: %q",
				c.name, deliveryRetryOwedMarker, got)
		}
		if !strings.HasPrefix(got, deliveryRetryOwedMarker+cloneChannelFailurePrefix) {
			t.Errorf("%s: the prefix must sit directly under the owed marker: %q", c.name, got)
		}
		// The body itself survives intact: the composer adds at most one prefix
		// and never trims what the channel said.
		trimmed := strings.TrimSpace(c.body)
		body := strings.TrimPrefix(trimmed, cloneChannelFailurePrefix)
		if !strings.HasSuffix(got, strings.TrimSpace(body)) {
			t.Errorf("%s: the body was not carried verbatim: %q", c.name, got)
		}
	}
}

// A nil/empty body still yields a usable reason rather than a bare marker glued
// to the prefix with nothing under it (Rule 15: an empty reason is silence).
func TestDeliveryRetryChannelReasonOfAnEmptyBodyIsStillAReason(t *testing.T) {
	got := deliveryRetryChannelReason("   ")
	if n := strings.Count(got, cloneChannelFailurePrefix); n != 1 {
		t.Fatalf("count=%d want 1: %q", n, got)
	}
	if strings.TrimSpace(strings.TrimPrefix(got, deliveryRetryOwedMarker+cloneChannelFailurePrefix)) == "" {
		t.Fatalf("an empty body left a reason with nothing under the prefix: %q", got)
	}
}
