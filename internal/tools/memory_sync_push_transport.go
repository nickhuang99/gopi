package tools

// memory_sync_push_transport.go: the WIRE of a row push -- moved verbatim out
// of internal/tools/memory_sync_claim.go in the same commit that deleted the
// cross-node claim LOCK.
//
// Why it survives the deletion it was born in: a push is not a lock round.
// The terminal push (memory_sync_terminal_push.go) and the manual
// `gopi task sync --push-only` face still ship a one-row delta patch over ssh
// and read a ClaimPushResponse back, and those two legs are OUT OF SCOPE for
// this batch -- moved, not changed. The lock protocol that used this same
// transport for its Phase-2 remote CAS is gone, so today the only callers are
// push legs.
//
// NOT IN THIS FILE, and deliberately: the PEER-side handler that used to
// answer on the far end (`gopi task sync --claim-remote`, which applied the
// patch to the peer's ledger and mirrored the lock) was Phase-2/3 machinery
// and went with the lock. Consequence, stated rather than hidden: a push from
// this build reaches only a peer still running the pre-deletion build. On
// this host the leg is inert anyway (`task_sync` off / no sync_peers), which
// is why the push code could be carved out without touching its semantics.

import (
	"fmt"
	"time"
)

// claimLockTimeout is a var (not const): tests shorten it to keep the
// unreachable-peer case fast. Rule 1 note: the production default lives in
// one place here; no production path reassigns it. It bounds ONE peer's
// protocol leg (ssh + remote CAS) and must stay below claimLockWait.
var claimLockTimeout = syncSSHTimeout

// ClaimPushResponse is the claim handler's answer document.
type ClaimPushResponse struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
	Node     string `json:"node,omitempty"`
}

// exportClaimPatch builds the one-row delta-shaped patch carrying the
// task's CURRENT row state (in_progress, mid-protocol — the peer's
// handler advances/acquires it on its side). It rides the v2 delta schema
// so the peer's merge machinery needs zero new parsing. No anchor window
// AND no anti-reflow gate: a claim patch is a lock-protocol VERB aimed at
// the row's origin — excluding live from-<peer> rows (the M4.24 reflow
// gate, c4745d9) filtered out exactly the rows a cross-node claim must
// reach, so the acquire patch came back empty ("not found in the ledger"),
// every round failed, and the runner yielded into a stuck in_progress
// zombie (the anchor-guard incident, 2026-09-02: HP claiming a from-zbook
// row could never advance it on zbook). The peer's --claim-remote handler
// owns the CAS/refusal logic; an echo row no-ops idempotently, so carrying
// the row verbatim is safe.
func exportClaimPatch(taskID, claimOwner, peer string) (*SyncTaskPatch, error) {
	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{AfterSource: 0, SkipReflowGate: true})
	if err != nil {
		return nil, err
	}
	rows := make([]SyncTaskRow, 0, 1)
	for _, r := range patch.Rows {
		if r.TaskID == taskID {
			rows = append(rows, r)
			break
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("task %s not found in the ledger (row vanished between claim and lock)", taskID)
	}
	patch.Rows = rows
	patch.ExportedAt = time.Now().Unix()
	patch.Generator = "claim-lock:" + claimOwner
	return patch, nil
}

// claimSSHExchange is the claim protocol's ssh leg: same BatchMode/connect
// timeout discipline as the session driver, but the remote verb is
// `task sync --claim-remote` (a lock handler, NOT a delta session — the
// peer answers a tiny ClaimPushResponse instead of an envelope).
func claimSSHExchange(peer string, body []byte) ([]byte, error) {
	// Peer-alias guard (option injection): the alias travels as an ssh
	// argv element below — a leading "-" would be parsed as an OPTION.
	// It runs BEFORE the test seam below on purpose: the seam replaces the
	// TRANSPORT, never the validation, so a test that injects a peer answer
	// still exercises the same alias rules as production (the structural
	// gate in peer_alias_test.go relies on this call staying here).
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}
	if claimSSHExchangeHook != nil {
		return claimSSHExchangeHook(peer, body)
	}
	// THE PRODUCTION PATH IS GONE, and this is where it used to be. The leg
	// composed `ssh <peer> gopi task sync --claim-remote`; that verb was
	// DELETED on 2026-11-05 (cmd/agent/task_sync_verb_removal.go) and answers
	// with a removal notice + exit 1, so dialling turned a removal into a
	// protocol error at the far end of a connection that cost real seconds.
	// The refusal happens HERE, before any ssh argv exists
	// (remote_leg_retired.go states the mechanism once, Rule 13/14), and the
	// alias guard + the test seam above keep running exactly as they did —
	// the seam still answers, so a test that injects a peer reply still
	// exercises the validation above it.
	return nil, remoteLegRetired("claim broadcast", peer)
}

// claimSSHExchangeHook replaces the claim protocol's ssh transport when
// non-nil (the syncSSHExchangeHook pattern in memory_sync_session.go): a test
// drives a REAL dispatch through the claim gate with the peer's answer
// injected, so no ssh leg — and no real peer — is touched. Production never
// sets it (nil → the ssh exec above).
var claimSSHExchangeHook func(peer string, body []byte) ([]byte, error)

// SetClaimSSHExchangeHookForTest installs (or, with nil, removes) the claim
// transport seam. Exported because the caller driving a real dispatch lives in
// cmd/agent.
func SetClaimSSHExchangeHookForTest(fn func(peer string, body []byte) ([]byte, error)) {
	claimSSHExchangeHook = fn
}
