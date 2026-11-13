// cli_notify_subverb_help_test.go — the group page's own promise, EXECUTED.
//
// THE DEFECT THIS FILE PINS. `gopi notify -h` states, on the page itself:
//
//	Every sub-verb answers its OWN page: gopi notify <sub> -h prints that verb's
//	flags and examples — this page only says which of the three routes you want.
//
// Measured on 7c72007f, that sentence was false in the quietest possible way:
// all four invocations printed the SAME 75 lines and exited 0 —
//
//	$ for v in "" events failed sink; do ./bin/gopi notify $v -h | md5sum; done
//	3c3a1c06e9c54402bca1c0e009e799ca   (group)
//	3c3a1c06e9c54402bca1c0e009e799ca   (events)
//	3c3a1c06e9c54402bca1c0e009e799ca   (failed)
//	3c3a1c06e9c54402bca1c0e009e799ca   (sink)
//
// A refusal would have been a bug an operator notices. Four identical pages are
// not: nothing errors, nothing warns, and the reader who asked "what does
// --cursor do on failed?" is handed an index that never mentions it.
//
// WHY THE ASSERTION IS "≠ THE GROUP PAGE" and not "non-empty" or "exit 0".
// The sibling row 01a1aa6d measured that `gopi frobnicatezzz -h` prints the
// top-level page and exits 0 — so neither output-exists nor exit-status is
// evidence that a verb HAS a page. The discriminator that survives both shapes
// is BYTES: the sub-verb's body must not be the group's body. That is the whole
// predicate below, and it is a pure function so the red proofs run the same code
// the gate runs.
//
// THE REVERSE ARM IS PART OF THE CONTRACT, not an afterthought: an UNKNOWN word
// must still fall back to the group page (and still refuse, exit 1). A repair
// that made every word print "its own page" by inventing one would break the
// fallback that tells a reader their typo is a typo.
package main

import (
	"strings"
	"testing"
)

const (
	// nshMinSubVerbs is the non-vacuity floor: the family advertises three
	// reading sub-verbs (events/failed/sink), so a walk that inspected fewer is
	// blind and its green would prove nothing (Rule 15: a gate that measured
	// nothing must not pass).
	nshMinSubVerbs = 3
	// nshUnknownWord is a word the dispatcher does NOT accept. It exists so the
	// fallback can be exercised: the reverse arm asserts that it still reaches
	// the group page WITH a refusal.
	nshUnknownWord = "no-such-notify-verb"
	// nshIdenticalOffense is the predicate's own sentence for the defect this
	// file exists for. It is a constant so the red proof and the gate assert the
	// SAME wording (a red proof that matched a paraphrase would prove the wrong
	// thing).
	nshIdenticalOffense = "prints the GROUP page byte for byte"
)

// nshExceptionPages is the declared exception list the briefing allows: a
// sub-verb that legitimately shares the group page. It is EMPTY, deliberately —
// the three verbs have three different flag sets, so none of them has a reason
// to answer the index — and a permit written here must carry its reason or it
// fails (an unexplained exception is the divergence left in place).
var nshExceptionPages = map[string]string{}

// nshDivergenceOffenses is THE predicate for one sub-verb: given its help body,
// the group page's body, and the declared exception table, it returns every way
// the sub-verb failed to answer its own page. It is a pure function of those
// three inputs so the red proofs below exercise the shipped predicate instead of
// a re-implementation.
func nshDivergenceOffenses(verb, body, group string, exceptions map[string]string) []string {
	var out []string
	if strings.TrimSpace(body) == "" {
		out = append(out, "printed NOTHING — a page that measured nothing must not pass as a page of its own")
		return out
	}
	if strings.TrimSpace(body) == strings.TrimSpace(group) {
		if reason, ok := exceptions[verb]; ok {
			if strings.TrimSpace(reason) == "" {
				out = append(out, nshIdenticalOffense+" and its exception carries no reason — an unexplained exception is the divergence itself")
			}
			return out
		}
		out = append(out, nshIdenticalOffense+" — the group page promises `gopi notify <sub> -h prints that verb's flags and examples`, so either the sub-verb gets its own page or that promise leaves the page")
	}
	return out
}

// TestNotifySubVerbHelpDivergesFromTheGroupPage is the GATE. It derives its
// subject from notifySubcommandNames — the same registry the dispatcher and the
// completion set read — so a sub-verb added later cannot be born without a page.
func TestNotifySubVerbHelpDivergesFromTheGroupPage(t *testing.T) {
	if len(notifySubcommandNames) < nshMinSubVerbs {
		t.Fatalf("notifySubcommandNames holds %d words, below the floor of %d — this walk would be vacuous",
			len(notifySubcommandNames), nshMinSubVerbs)
	}
	group, groupErr, err := runNotifyBinary(t, HelpFlag)
	if err != nil {
		t.Fatalf("gopi notify -h: %v\nstderr: %s", err, groupErr)
	}
	if strings.TrimSpace(group) == "" {
		t.Fatalf("gopi notify -h printed nothing — the comparison below would have no baseline")
	}

	seen := map[string]string{notifyGroupPageName: group}
	for _, sub := range notifySubcommandNames {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			body, stderr, err := runNotifyBinary(t, sub, HelpFlag)
			if err != nil {
				t.Fatalf("gopi notify %s -h: %v\nstderr: %s", sub, err, stderr)
			}
			for _, offense := range nshDivergenceOffenses(sub, body, group, nshExceptionPages) {
				t.Errorf("`gopi notify %s -h` %s", sub, offense)
			}
			// A page of its own NAMES ITSELF: a page that opened with another
			// verb's fingerprint would be one page wearing three hats.
			head, ok := notifySubVerbHeads[sub]
			if !ok {
				t.Fatalf("sub-verb %q has no entry in notifySubVerbHeads — it has no page of its own", sub)
			}
			if !strings.HasPrefix(body, head) {
				t.Errorf("`gopi notify %s -h` does not open with its own fingerprint %q:\n%s", sub, head, body)
			}
			// And it must NOT carry another verb's fingerprint: the shared page
			// this repair removed was exactly a body every verb could claim.
			for other, otherHead := range notifySubVerbHeads {
				if other == sub {
					continue
				}
				if strings.Contains(body, otherHead) {
					t.Errorf("`gopi notify %s -h` carries %s's fingerprint %q — two verbs are sharing one page again", sub, other, otherHead)
				}
			}
			seen[sub] = body
		})
	}

	// The four bodies must be four DIFFERENT bodies. This is the md5 assertion
	// from the epic's acceptance criterion, expressed without a shell: a
	// duplicate pair means two verbs landed on one page and the per-verb checks
	// above somehow agreed with it.
	for a, bodyA := range seen {
		for b, bodyB := range seen {
			if a >= b {
				continue
			}
			if strings.TrimSpace(bodyA) == strings.TrimSpace(bodyB) {
				t.Errorf("`gopi notify %s -h` and `gopi notify %s -h` printed the same page", a, b)
			}
		}
	}
}

// notifyGroupPageName is how the group page is labelled in failure messages and
// in the duplicate check above — it is not a sub-verb and never reaches the
// dispatcher.
const notifyGroupPageName = "‹group›"

// TestNotifySubVerbPagesNameTheirOwnFlags is the half that makes a divergent
// page USEFUL rather than merely different: each page must state the flags its
// own verb actually accepts, spelled with the SAME constants the parser reads
// (Rule 1/13 — a page that typed `--cursor` by hand could drift from the parser
// while still being "different from the group page").
func TestNotifySubVerbPagesNameTheirOwnFlags(t *testing.T) {
	// Measured on the real parser (parseNotifyArgs): `--event`/`--task`/`--limit`
	// apply to the two read verbs, `--follow` is refused by name on anything but
	// events, and `sink` answers without reading the stream at all.
	for _, c := range []struct {
		verb  string
		flags []string
		why   string
	}{
		{verb: notifyEventsVerb,
			flags: []string{notifyFollowFlag, notifyCursorFlag, notifyEventFlag, notifyTaskFlag, notifySinceFlag, notifyLimitFlag, notifyJSONFlag, notifySinkFlag},
			why:   "the general reader owns the whole flag set, including the foreground loop"},
		{verb: notifyFailedVerb,
			flags: []string{notifyCursorFlag, notifyJSONFlag, notifySinceFlag, notifyLimitFlag, notifyEventFlag, notifyTaskFlag},
			why:   "the debt report owns the filters and the machine-readable form"},
		{verb: notifySinkVerb,
			flags: []string{notifySinkFlag},
			why:   "the sink answer owns only the path that makes it authoritative"},
	} {
		page, ok := notifySubVerbUsage[c.verb]
		if !ok {
			t.Fatalf("sub-verb %q has no page in notifySubVerbUsage", c.verb)
		}
		for _, flag := range c.flags {
			if !strings.Contains(page, flag) {
				t.Errorf("`gopi notify %s -h` does not name %s, which its own parser accepts (%s)", c.verb, flag, c.why)
			}
		}
	}

	// The sink page must SAY that the shared parser accepts flags it ignores —
	// silently accepting them is the kind of quiet no-object that Rule 15 bans,
	// and the page is where a reader learns it.
	sinkPage := notifySubVerbUsage[notifySinkVerb]
	if !strings.Contains(sinkPage, notifyJSONFlag) {
		t.Errorf("`gopi notify %s -h` does not mention %s at all — a reader whose parser accepts it learns nothing about why it does nothing",
			notifySinkVerb, notifyJSONFlag)
	}
	if !strings.Contains(sinkPage, notifyEventsVerb) {
		t.Errorf("`gopi notify %s -h` does not route the reader to `gopi notify %s` for the stream itself", notifySinkVerb, notifyEventsVerb)
	}
}

// TestNotifyUnknownWordStillFallsBackToTheGroupPage is the REVERSE ARM: the
// repair must not delete the fallback. A word the dispatcher does not accept is
// a TYPO, and the honest answer is the group page plus the refusal that names
// the accepted words — never an invented page for a verb that does not exist.
func TestNotifyUnknownWordStillFallsBackToTheGroupPage(t *testing.T) {
	// runNotifyBinary returns (stdout, stderr, err) — the refusal travels on
	// stderr, so the two are read in that order deliberately.
	_, stderr, err := runNotifyBinary(t, nshUnknownWord, HelpFlag)
	if err == nil {
		t.Fatalf("`gopi notify %s -h` exited 0 — an unknown word must refuse", nshUnknownWord)
	}
	if !strings.Contains(stderr, notifyUsage) {
		t.Errorf("`gopi notify %s -h` does not print the group page — the fallback was deleted instead of kept:\n%s", nshUnknownWord, stderr)
	}
	// And no sub-verb page may be reachable through a word nobody dispatches.
	for sub, page := range notifySubVerbUsage {
		if strings.Contains(stderr, page) {
			t.Errorf("`gopi notify %s -h` printed the %s page — an unknown word must never be answered by a known verb's page", nshUnknownWord, sub)
		}
	}
	if !strings.Contains(stderr, notifyEventsVerb) || !strings.Contains(stderr, notifySinkVerb) {
		t.Errorf("the refusal does not name the accepted words — a typo is repaired by being told the valid spellings:\n%s", stderr)
	}
}

// TestNotifySubVerbHelpPredicateIsNotBlind is the RED PROOF (Rule 10.4/10.5):
// the predicate must reject the shape the defect had, and accept the shape the
// repair produces, on the SAME code the gate runs above.
func TestNotifySubVerbHelpPredicateIsNotBlind(t *testing.T) {
	const group = "gopi notify — index page\n"

	// RED: the measured defect — a sub-verb answering the group page byte for
	// byte, with no exception declared.
	if got := nshDivergenceOffenses(notifyEventsVerb, group, group, nil); len(got) == 0 {
		t.Errorf("the predicate passed a sub-verb that printed the group page — it is blind to the defect it exists for")
	}

	// RED: an empty body is not a page either (a gate measuring nothing).
	if got := nshDivergenceOffenses(notifyEventsVerb, "  \n", group, nil); len(got) == 0 {
		t.Errorf("the predicate passed an empty help body")
	}

	// RED: an exception with no reason is the divergence left in place.
	if got := nshDivergenceOffenses(notifyEventsVerb, group, group, map[string]string{notifyEventsVerb: " "}); len(got) == 0 {
		t.Errorf("the predicate accepted an un-reasoned exception")
	}

	// GREEN control: a declared, reasoned exception is admitted.
	if got := nshDivergenceOffenses(notifyEventsVerb, group, group, map[string]string{notifyEventsVerb: "measured: this verb's flags are the group's"}); len(got) != 0 {
		t.Errorf("the predicate refused a reasoned exception: %v", got)
	}

	// GREEN control: a page of its own passes.
	if got := nshDivergenceOffenses(notifyEventsVerb, "gopi notify events — …\n", group, nil); len(got) != 0 {
		t.Errorf("the predicate refused a divergent page: %v", got)
	}
}
