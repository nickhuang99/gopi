package tools

import (
	"errors"
	"fmt"
)

// The REMOTE LEGS of the cross-node ledger protocol are RETIRED, and this file
// is the ONE place that says so (Rule 13/14: the sentence is stated once, so no
// two legs can drift into describing a different mechanism).
//
// WHY THE LEGS EXIST AT ALL, and why they are not simply deleted with the verb:
// the verbs they used to speak over ssh (`gopi task sync --serve-remote`,
// `--claim-remote`, `--row-remote`, `gopi memory sync --serve-assets`) are
// DELETED (2026-11-05, cmd/agent/task_sync_verb_removal.go): the dispatcher now
// answers each of them with a removal sentence and exits 1. A leg that still
// COMPOSED one of those spellings therefore did not merely fail — it ssh'd to a
// peer, spent a connection, and got an exit 1 carrying an operator-facing
// removal notice back as if it were a protocol error. That is the defect this
// file closes: the composition point is gone, so no code path in this binary can
// build such an argv any more (Rule 43: the criterion is that the PRODUCTION
// POINT is gone, not that a symbol's name greps to zero).
//
// WHAT THE REFUSAL IS, and what it is not: a peer session has nowhere to go, so
// each leg keeps its signature, its seam and its callers, and refuses LOCALLY
// and IMMEDIATELY. It never dials, never writes, and never reports success.
// Rule 15: the refusal names the leg, the retired protocol, and the route that
// replaced it (a row that must run on another host is FILED there — Rule 28 /
// Rule 40 — and work crosses hosts through the git ref a row's deliverable
// publishes to, the clone channel's delivery leg).
const remoteLegRetiredFmt = "the cross-node ledger protocol is RETIRED (2026-11-05): %s would have opened a " +
	"session against a peer's ledger, and this binary no longer has one to open. The remote spellings it spoke " +
	"(`gopi task sync --serve-remote|--claim-remote|--row-remote`, `gopi memory sync --serve-assets`) are deleted " +
	"and answer with a removal notice. Nothing was sent to %s and nothing was written on either ledger. A row that " +
	"must run on another host is FILED there (`gopi task submit \"<briefing>\"` on that host, which stamps that " +
	"host's own repo and ledger — Rule 28 / Rule 40); work crosses hosts through the git ref a row's deliverable " +
	"publishes to, never through a second ledger"

// remoteLegRetired builds the ONE refusal every retired remote leg returns.
// `leg` names the face in the caller's own vocabulary (so an operator reading
// two different refusals can tell which path they took), and `peer` names the
// host NOT contacted — the empty string renders as "the peer" rather than an
// empty slot.
func remoteLegRetired(leg, peer string) error {
	target := peer
	if target == "" {
		target = "the peer"
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(remoteLegRetiredFmt, leg, target), errRemoteLegRetired)
}

// errRemoteLegRetired is the SENTINEL a caller tests with errors.Is, and it is
// not a convenience: the claim leg's classifier distinguishes THREE causes that
// all arrive as a Go error — a peer that REFUSED the lock, a transport that did
// not ANSWER, and this protocol being GONE — and the first two must not be
// collapsed into the third.
//
// WHY IT MATTERS (measured 2026-11-06, internal/tools, `go test -run
// 'TaskSubmit|Transfer|Claim|SyncRow|MemoryAssets'`): with the refusal returning
// a plain error, classifyClaimLockRound took its "unreachable → retry" branch,
// exhausted all three rounds and YIELDED — rolling the run's own row back out of
// in_progress. That is not a conservative failure, it is a WRONG one: with no
// cross-node protocol there is no peer to contest the lock, so the local claim
// is the whole claim and the round is UNCONTENDED. A caller must be able to tell
// "nobody answered" from "there is no longer anybody to answer".
var errRemoteLegRetired = errors.New("remote ledger leg retired")
