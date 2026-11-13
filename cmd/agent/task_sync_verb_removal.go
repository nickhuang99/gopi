package main

import "github.com/nick/gopi/internal/taskverbs"

// The cross-node sync family's REMOVAL SENTENCES, in one place (Rule 13/14: a
// spelling is stated once; every face that refuses it renders the same words,
// so the `-h` scan and the pre-parse gate can never drift).
//
// WHY THERE IS NO REPLACEMENT VERB, said plainly because a refusal that hides
// this would send every reader hunting one: the mechanism is DELETED, not
// re-homed. What each spelling did, and what an operator does instead:
//
//   - `sync` / `sync-pass` / `harvest` / `push` / `diff` spoke to a PEER's
//     ledger over ssh. This binary no longer opens such a session: work crosses
//     hosts through the GIT ref a row's deliverable publishes to (the clone
//     channel's own delivery leg), not through a second ledger protocol. A row
//     that must be worked on another host is FILED there —
//     `gopi task submit "<briefing>"` on that host, which is also what Rule 28
//     and Rule 40 require of the host that owns the work.
//   - `send` / `receive` / `pull` moved ONE row between two live ledgers. Same
//     answer: there is no second ledger to hand a row to.
//
// The one capability the family carried that was never a peer protocol — the
// twin-row audit (`memsync --dedup-scan`) — is a LEDGER verb now and is named
// in the sentence, because that is the only part of the removal a reader can
// actually go and use.
//
// WHAT IS NOT SAID HERE, deliberately: no duration, no token cost, no
// "temporarily". A removal sentence is a statement about the surface, and every
// clause it carries must be something the reader can check with `-h`.

// syncVerbRemovalSpellings is the deleted family. It is a LIST rather than seven
// separate `if`s so a spelling added to or dropped from the family is edited in
// ONE place, and so the tests can iterate exactly what the dispatcher accepts.
var syncVerbRemovalSpellings = []string{
	taskverbs.Sync, taskverbs.SyncPass, taskverbs.Harvest, taskverbs.Diff,
	taskverbs.Send, taskverbs.Receive, taskverbs.Pull, taskverbs.Push,
}

// syncVerbRemovalRowLegs is the subset that moved a ROW rather than opened a
// peer session. The sentence differs because the consequence differs: a row
// that used to be sent somewhere has a place to be filed instead, while a peer
// session has nowhere to go at all.
var syncVerbRemovalRowLegs = []string{
	taskverbs.Send, taskverbs.Receive, taskverbs.Pull,
}

// syncVerbRemovalSentence answers the sentence for a deleted sync spelling, or
// the empty string when the verb is not one of them (the caller's ordinary
// path). Returning "" rather than a default is what keeps every OTHER removal
// on its own advice: an unexpected verb must not inherit a sync sentence.
func syncVerbRemovalSentence(verb string) string {
	if !isSyncVerbRemoval(verb) {
		return ""
	}
	if isSyncVerbRemovalRowLeg(verb) {
		return "the cross-node row legs are DELETED (2026-11-05) — this binary no longer hands a row to another " +
			"node's ledger, because there is no second ledger to hand it to. Work that must run on another host is " +
			"FILED there: `gopi task submit \"<briefing>\"` on that host, which stamps that host's own repo and " +
			"ledger (Rule 28 / Rule 40). Nothing on this host reads, waits for or merges the row you meant to send"
	}
	return "the cross-node sync surface is DELETED (2026-11-05) — no verb in this binary opens a session against a " +
		"peer's ledger any more. Work crosses hosts through the GIT ref a row's deliverable publishes to (the " +
		"clone channel's delivery leg), never through a second ledger protocol. The twin-row audit that lived in " +
		"this family is a ledger verb now: `gopi task dedup-scan`"
}

// isSyncVerbRemoval reports whether verb is one of the deleted spellings.
func isSyncVerbRemoval(verb string) bool {
	for _, v := range syncVerbRemovalSpellings {
		if v == verb {
			return true
		}
	}
	return false
}

// isSyncVerbRemovalRowLeg reports whether verb is one of the ROW legs.
func isSyncVerbRemovalRowLeg(verb string) bool {
	for _, v := range syncVerbRemovalRowLegs {
		if v == verb {
			return true
		}
	}
	return false
}
