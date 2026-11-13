// Package tools — ledger_agreement.go: the READ-ONLY report layer behind the
// EXPLICIT ledger comparison, `gopi task diff --peer <alias>`.
//
// Every piece of cross-node machinery (claim broadcast, "is this row already
// being worked elsewhere?", the delta protocol's anchor premise) ASSUMES the
// two ledgers mirror each other. `gopi task diff --peer X` (① in
// memory_sync_reconcile.go) is how an OPERATOR answers that question, on
// demand, from the node's own settings.json declaration.
//
// WHAT WAS REMOVED AND WHY (2026-10-29, task 01a183d8-1ac2-7c54-b811-4f6c10f6ba17).
// This file also used to serve an AUTOMATIC precondition check that the worker's
// dispatch path ran on EVERY round (`announceLedgerAgreement`, wired at the claim
// gate in cmd/agent). It is gone, and the measured reason is the whole point:
// with two peers declared it cost ~0.9s of ssh per round, and its verdict was a
// CONSTANT — 235 status-mismatches that stayed mismatched for hours across 6+
// alarms and 0 operator actions. An announcement nobody can act on is a cost
// with no consumer, so the automatic leg was deleted rather than tuned, and no
// switch was left behind to schedule a thing that no longer exists (Rule 35).
// `gopi task diff --peer` and `gopi task sync` — the explicit, human-typed
// actions — are untouched: the judgement is "the worker must not carry this",
// not "this capability must not exist".
//
// What remains is ONE function, READ-ONLY by construction:
//
//		LedgerAgreementReport(peer) → *LedgerAgreement  (one peer, always fresh)
//
//	  - READ-ONLY (no auto-repair). Nothing in this path writes, merges or
//	    deletes a row — the divergence IS the evidence, and repair stays an
//	    explicit `gopi task sync --full <peer>` the operator types.
//	  - NEVER SILENT-GREEN (Rule 15). An unanswerable check (ssh down, timeout,
//	    unparsable peer reply) returns an ERROR, never a clean report: the caller
//	    must announce it loudly. A green "agreed" is only ever printed for a
//	    check that actually RAN.
//	  - BOUNDED output. A real divergence on a live pair counts in the hundreds
//	    of rows; the report lists maxLedgerDivergenceListed ids per class (with
//	    the true counts) so the report stays a one-screen signal.
//
// ONE JUDGEMENT SURVIVES: `Agreed()` compares the row SETS, which is what the
// explicit diagnostic renders. The tighter per-dispatch judgement
// (`StructuralConflict`, same-row DISAGREEING STATUSES only) went with the
// automatic leg it existed to serve.
package tools

import (
	"fmt"
	"sort"
	"strings"
)

// Ledger agreement tunables — Rule 1: every literal in one place.
const (
	// LedgerAgreementDivergedMarker is the verdict word the divergence summary
	// leads with — exported so Summary()'s own rendering and the tests that
	// assert on it use the SAME spelling (Rule 1b/14) rather than re-typing
	// "DIVERGED".
	LedgerAgreementDivergedMarker = "DIVERGED"

	// maxLedgerDivergenceListed bounds how many ids of ONE class the report
	// spells out. The COUNTS are never truncated (they are the severity
	// signal); only the id list is, and the truncation is announced ("+N
	// more") so a bounded report can never read as a complete one.
	maxLedgerDivergenceListed = 8
)

// LedgerAgreement is the answer to "do my ledger and this peer's agree?" —
// a TaskDiffStats plus the human-facing rendering the EXPLICIT diagnostic
// (`gopi task diff`, with and without `--peer`) emits. It wraps (never
// replaces) the diff's own stats so the two faces of `task diff` can never
// drift into two different notions of "divergent".
type LedgerAgreement struct {
	// Peer is the declared peer alias this answer is about.
	Peer string
	// Stats is the diff's own both-direction comparison (Rule 14: one
	// implementation of the set logic — reconCompare).
	Stats *TaskDiffStats
}

// Agreed reports whether the two ledgers mirror each other row-for-row.
func (a *LedgerAgreement) Agreed() bool { return a.Stats == nil || a.Stats.IsClean() }

// DivergentCount is the number of differing rows (the headline number). It is
// answered from the ONE definition of the total (TaskDiffStats.DiffCountTotal,
// Rule 14) rather than from the stored field, so no reader can print a total
// that disagrees with the classes beside it.
func (a *LedgerAgreement) DivergentCount() int {
	if a.Stats == nil {
		return 0
	}
	return a.Stats.DiffCountTotal()
}

// Summary renders the ONE-LINE operator-facing verdict (the line
// `gopi task diff` prints for a declared peer). It is deliberately explicit
// about all four facts the operator needs to act: what disagrees, how much,
// why it matters, and the two commands (read-only diagnostic first, explicit
// repair second — the check itself never repairs).
func (a *LedgerAgreement) Summary() string {
	if a.Agreed() {
		return fmt.Sprintf("ledger agreement: %s — ledgers agree (%d rows)", a.Peer, a.Stats.LocalRows)
	}
	s := a.Stats
	return fmt.Sprintf(
		"ledger agreement: "+LedgerAgreementDivergedMarker+" from %s — %d differing rows (missing-on-peer=%d missing-on-local=%d status-mismatch=%d; local=%d peer=%d rows). "+
			"Cross-node dispatch assumes the two ledgers mirror each other (claim broadcast, \"is this row already being worked elsewhere?\"); "+
			"this is a PRECONDITION failure, not a failure of this command. Diagnose: gopi task diff --peer %s. Repair (explicit, never automatic): gopi task sync --full %s.",
		a.Peer, a.DivergentCount(), len(s.MissingOnPeer), len(s.MissingOnLocal), len(s.StatusMismatch),
		s.LocalRows, s.PeerRows, a.Peer, a.Peer)
}

// Detail renders the multi-line evidence block: per-class counts (NEVER
// truncated — they are the severity signal) plus a bounded sample of the
// offending ids, so the alarm is actionable without being a wall of text.
func (a *LedgerAgreement) Detail() string {
	if a.Agreed() {
		return ""
	}
	s := a.Stats
	var b strings.Builder
	fmt.Fprintf(&b, "  missing-on-peer  (%d): %s\n",
		len(s.MissingOnPeer), ledgerDivergenceSample(s.MissingOnPeer))
	fmt.Fprintf(&b, "  missing-on-local (%d): %s\n",
		len(s.MissingOnLocal), ledgerDivergenceSample(s.MissingOnLocal))
	fmt.Fprintf(&b, "  status-mismatch  (%d): %s\n",
		len(s.StatusMismatch), ledgerDivergenceSample(s.StatusMismatch))
	return strings.TrimRight(b.String(), "\n")
}

// ledgerDivergenceSample renders at most maxLedgerDivergenceListed ids of
// one class, sorted (deterministic output — a test can assert the line),
// with the truncation announced.
func ledgerDivergenceSample(ids []string) string {
	if len(ids) == 0 {
		return "(none)"
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	shown := sorted
	over := 0
	if len(shown) > maxLedgerDivergenceListed {
		over = len(shown) - maxLedgerDivergenceListed
		shown = shown[:maxLedgerDivergenceListed]
	}
	out := strings.Join(shown, ", ")
	if over > 0 {
		out = fmt.Sprintf("%s (+%d more)", out, over)
	}
	return out
}

// LedgerAgreementPeerRows is the peer-catalog answer one row wide (the
// exported shape of reconRowSet, so a test in ANOTHER package can supply the
// peer's answer through the seam below).
type LedgerAgreementPeerRows struct {
	TaskID string
	Status string
}

// ledgerAgreementPeerRowsHook replaces the peer-side READ of the check when
// non-nil (the reconRemotePendingSetHook pattern in
// memory_sync_reconcile.go): tests inject the peer's catalog in-process so the
// real local read + the real reconCompare run, with only the ssh byte pipe
// replaced. Production never sets it (nil → the real ssh read path).
var ledgerAgreementPeerRowsHook func(peer string) ([]LedgerAgreementPeerRows, error)

// SetLedgerAgreementPeerRowsForTest installs (or, with nil, removes) the
// peer-catalog seam. Exported because the red-proof test drives the CLI from
// cmd/agent.
func SetLedgerAgreementPeerRowsForTest(fn func(peer string) ([]LedgerAgreementPeerRows, error)) {
	ledgerAgreementPeerRowsHook = fn
}

// peerRowSets reads the peer's catalog through the seam when one is installed,
// else over ssh (the diff's own channel).
func peerRowSets(peer string) ([]reconRowSet, error) {
	if ledgerAgreementPeerRowsHook != nil {
		rows, err := ledgerAgreementPeerRowsHook(peer)
		if err != nil {
			return nil, err
		}
		out := make([]reconRowSet, 0, len(rows))
		for _, r := range rows {
			out = append(out, reconRowSet{TaskID: r.TaskID, Status: r.Status})
		}
		return out, nil
	}
	return reconRemoteRowSets(peer, reconRemoteDB)
}

// LedgerAgreementReport answers "do my ledger and peer's agree?" with the
// diff's OWN read path (peerRowSets + reconLocalRowSets + reconCompare — the
// ① reconciliation, reused verbatim, never rebuilt) and wraps the answer for
// display. Read-only by construction: the only two operations are a local
// SELECT and one peer-side SELECT over ssh.
//
// An unanswerable check returns an ERROR (Rule 15) — the caller announces
// it; nothing here degrades to a silent "agreed".
//
// THIS IS THE ONLY PATH (`gopi task diff --peer <alias>`): always fresh, never
// cached. The peer-catalog cache that used to brake the automatic leg's ssh
// cost was deleted with it — an explicit diagnostic is asked for a handful of
// times a session, so paying the round trip is the honest price of an answer
// that is exact at the moment it is printed.
func LedgerAgreementReport(peer string) (*LedgerAgreement, error) {
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("ledger agreement: %w", err)
	}
	remote, err := peerRowSets(peer)
	if err != nil {
		// Never a clean-looking report: the caller must say "could not
		// check", which is a DIFFERENT operator action than "diverged".
		return nil, fmt.Errorf("ledger agreement: %w", err)
	}
	local, err := reconLocalRowSets()
	if err != nil {
		return nil, fmt.Errorf("ledger agreement: %w", err)
	}
	return &LedgerAgreement{Peer: peer, Stats: reconCompare(peer, local, remote)}, nil
}
