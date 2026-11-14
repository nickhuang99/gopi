package tools

// memory_sync_row.go — the SINGLE-ROW send/receive legs of the task-ledger
// sync family.
//
// THE DEFECT THIS FILE CLOSES (measured 2026-09-21 on peer1, re-confirmed by
// this task's own probe):
//
//	$ gopi task sync --push-only --task-id <row> --peer hp
//	push hp: accepted (1 row(s))
//	$ ssh hp 'gopi task show <row>'
//	gopi task show error: task: no task with task_id="<row>"
//
// The manual single-row face rode `--claim-remote`, a CLAIM-BROADCAST handler:
// it mirrors the row's lock state and answers an `accepted/refused` VERDICT.
// It never stores the row, so "accepted" was true about the LOCK and false
// about the ROW — the operator was told a row travelled that the peer's ledger
// had never seen (Rule 15b: prove the effect, never the receipt).
//
// THE TWO LEGS, AND WHAT EACH REUSES (Rule 14 — no second engine):
//
//	send    — export ONE row (SyncTaskExportDelta), ssh it to the peer's
//	          `task sync --row-remote` handler, and report what the peer's
//	          ledger DID with it (stored / already / refused).
//	receive — SELECT the ONE row off the peer's ledger (the harvest
//	          ssh+SQL shape, quoted with harvestShellQuote) and apply it
//	          through SyncTaskImportDelta — the SAME merge/de-dup/
//	          forward-only/no-overwrite authority every other leg uses.
//
// NOTHING here re-decides a merge. The send leg's peer handler calls
// SyncTaskImportDelta verbatim and reports its counts; the receive leg calls
// it verbatim after a one-row SELECT. There is no row-shaped rule in this
// file — only a transport, a projection, and a receipt.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/fullid"
)

// --- constants (Rule 1: every literal in one place) -------------------------

const (
	// SyncRowRemoteFlagName is the ONE spelling of the peer-side handler flag
	// (`gopi task sync --row-remote`). Exported because the CLI's admission
	// table (cmd/agent/cli_tasks.go, taskFlagMember) must admit the SAME
	// value this file puts on the wire — one definition, referenced twice
	// (Rules 1b/13/14). It is spelled here rather than in cmd/agent because
	// THIS file is the side that must agree with the remote argv.
	SyncRowRemoteFlagName = "row-remote"

	// syncFlagPrefixRow is the argv spelling of a flag name.
	syncFlagPrefixRow = "--"

	// syncRemoteHomePrefix is the shell prefix that binds the PEER-side
	// handler to an isolated profile (Rule 34). It is spelled once here and
	// applies to the remote argv only — a LOCAL export would move this
	// process's own ledger, which is the opposite of the intent.
	syncRemoteHomePrefix = "GOPI_HOME="
	// syncRemotePathPrefix binds the PEER-side handler's BINARY the same way
	// the profile binds its ledger: an isolated sandbox is an execution
	// environment, so a probe carries its own build and the peer's installed
	// copy is left untouched. The bin directory INSIDE the profile is
	// prepended, so a profile without one falls through to the peer's own
	// PATH copy (and a deployment that ships gopi normally is unaffected).
	syncRemotePathPrefix = " PATH="
	// syncRemoteBinDir is the profile-relative directory the private build
	// lives in, and syncRemotePathTail keeps the peer's own PATH reachable
	// behind it.
	syncRemoteBinDir   = "/bin"
	syncRemotePathTail = ":$PATH "

	// syncRowWireMaxRows is the row count a one-row leg carries: EXACTLY ONE.
	// The one-row send side ships one row by construction (its export is
	// narrowed to the requested id) — a window arriving there means the
	// SENDER's selection is wrong (the 2026-09-21 whole-window incident,
	// cli_tasks_sync_pushonly_rowfilter_test.go).
	//
	// THE PEER'S HANDLER NO LONGER REFUSES MORE THAN THIS (it admits
	// 1..syncRowPatchMaxRows, the wire bound below): a multi-row patch ships N
	// rows in ONE patch by design, and the property worth keeping is "the
	// sender says how many travel and the handler applies exactly what
	// arrived", which the receipt's per-row counts still prove. The constant
	// stays as the ONE-ROW leg's own count, which is what the wire comment meant.
	syncRowWireMaxRows = 1

	// syncRowPatchMaxRows bounds ONE patch the peer's handler will settle
	// (Rule 1: a named limit, never a literal at the call site). It is a WIRE
	// bound, and it is a defence: the handler holds the whole patch in memory
	// and settles it in two passes, so an unbounded patch is an unbounded
	// transaction on someone else's ledger.
	//
	// WHERE THIS CONSTANT CAME FROM, because it moved and its old name is
	// dead: it was `chainTransferMaxRows`, defined in the chain-transfer
	// mechanism (memory_sync_row_chain.go / task_chain_transfer.go). That
	// mechanism was DELETED (its only entry point, `task send --chain`, went
	// with the cross-node sync family — taskverbs.RemovedVerbs), and this
	// constant's only readers were THIS file's handler and its test — i.e.
	// outside the deleted mechanism — so Rule 43 says MOVE it here rather than
	// delete it with the mechanism, in the same commit. The NAME changed with
	// the move: `chainTransfer…` named a mechanism this tree no longer has,
	// while `syncRowPatch` names the thing it actually bounds.
	//
	// The VALUE is unchanged (64), and that is a deliberate decision, not an
	// omission: after the chain leg's deletion NO production sender emits more
	// than one row (the one-row leg ships exactly one by construction), so the
	// bound is now a pure wire defence against a malformed or hostile sender.
	// Relaxing it to "<1 only" was considered and refused — it would remove the
	// defence and buy nothing, and the refusal arm it drives (a patch past the
	// bound is REFUSED BY NAME, never silently truncated) is measured by
	// TestRowRemoteArityIsTheChainBound.
	syncRowPatchMaxRows = 64

	// syncRowGenerator tags the patch's provenance (informational; the peer's
	// import never gates on it — SyncTaskPatch.Generator's own contract).
	syncRowGenerator = "row-send:"

	// syncRowRemoteSelect fetches ONE row off the peer's ledger by id — the
	// harvest SELECT's shape (memory_sync_task.go owns the anchor-gated
	// sibling), narrowed to a task_id predicate and quote-free for the same
	// reason: the command travels through the peer's shell as one quoted
	// string, so the ID is interpolated through harvestShellQuote, never
	// inlined. `sqlite3 -json` emits JSON null for SQL NULL, which unmarshals
	// into Go zero values, so no COALESCE is needed on the nullable columns
	// beyond the ones harvest already coalesces.
	syncRowRemoteSelect = `SELECT task_id, summary, payload, tags, source,
		model, status, created_at, updated_at, ended_at, ` + syncRemoteCoalescedProjection + `
		FROM tasks WHERE task_id = %s`

	// syncRemoteSQLiteBinary is the peer-side reader the transport executes.
	// It is a NAME, not a path: the remote login shell resolves it from the
	// peer's PATH (a test on this host resolves it the same way), and Rule 1b
	// keeps this one spelling instead of the three literals that had drifted
	// into the queries.
	syncRemoteSQLiteBinary = "sqlite3"

	// syncRemoteCoalescedProjection is the ONE spelling of the peer-side
	// projection's COALESCE'd tail (Rule 1b/13: the row leg and the anchor
	// window carry the same five columns, and a second spelling is what let
	// them drift apart).
	//
	// THE `AS <name>` IS LOAD-BEARING, and dropping it is a SILENT data loss
	// measured on 2026-10-07 (`sqlite3 -json` names such an output column with
	// its WHOLE SOURCE TEXT — "COALESCE(reason,'')" — so the JSON key never
	// matches the wire struct's json tag and the field arrives EMPTY). It is
	// the root cause of the documented "five chain links landed unparented"
	// defect: parent_task_id, reason, deliverable, run_method and run_cmd all
	// travelled as "" and every reader of them saw a row with no edge, no why
	// and no provenance. A NULLABLE column keeps its name and needs nothing;
	// an EXPRESSION does.
	syncRemoteCoalescedProjection = `COALESCE(deliverable,'')` + syncColumnAliasKeyword + `deliverable,
		COALESCE(run_method,'')` + syncColumnAliasKeyword + `run_method, COALESCE(run_cmd,'')` + syncColumnAliasKeyword + `run_cmd,
		COALESCE(reason,'')` + syncColumnAliasKeyword + `reason, ` + syncParentColumnNamed + `, ` + syncParentIDsColumnNamed

	// --- the STATUS GATE (this leg's one precondition) -------------------
	//
	// Only a `pending` row travels: it is the one state that means "queued
	// work nobody is running yet". Every other status is refused BY NAME,
	// with the current status and the reason — a silent skip is the defect
	// this gate exists to prevent (Rule 15). It is a gate on the SEND, never
	// a re-decision about the merge: the peer's apply is the ordinary import
	// authority and is untouched by it.
	syncRowStatusGateFormat = "row send: task %s is %s — only a %s row may be sent: %s"

	// --- the DEPENDENCY GATE (this leg's third precondition) -------------
	//
	// The status gate above answers "is this row queued?"; this one answers
	// "can this row LEAVE without breaking the chain it belongs to?" — the
	// question no reader of the row alone can answer, because the dependency
	// edge is written on the CHILD (task_transfer_eligibility.go owns both
	// halves of the criterion and every sentence). This format is only the
	// PREFIX: the reason it carries comes from
	// TaskTransferEligible — master's ONE predicate, asked here a third time
	// (Rule 14) — and names the blocking rows by id.
	syncRowTransferGateFormat = "row send: task %s may not leave this ledger yet: %s"

	// syncRowIDRefusalFormat frames the ONE refusal a truncated task id gets on
	// this leg (Rule 14: internal/fullid owns the sentence — this only adds what
	// THIS leg can promise). The promise matters: the id is resolved before any
	// peer is contacted, so a refused send is provably a no-op on BOTH ledgers
	// — the fact that was missing when the operand silently matched nothing.
	syncRowIDRefusalFormat = "row send: %s — the id is resolved against THIS ledger before any peer is contacted, so nothing was sent and nothing was cancelled"

	// syncRowWhyInProgress is the sharpest arm. An `in_progress` row has a
	// leg running HERE; sending it hands the peer's dispatcher work that is
	// already in flight — one row, two legs.
	syncRowWhyInProgress = "a leg is RUNNING on this row here — sending it would hand the peer's dispatcher work already in flight (two legs, one row); finish or cancel the local run first"
	// syncRowWhyClaimed is the same hazard one step earlier: a holder has
	// been stamped and the child is about to start.
	syncRowWhyClaimed = "the row is CLAIMED (a holder is stamped and the leg is about to start) — a second ledger holding it is how one row gets two legs; release it here first"
	// syncRowWhyHusk is the completion arm: the copy carries no work.
	syncRowWhyHusk = "the row is already FINISHED here — a copy carries no work (an empty husk the peer's dispatcher skips); send moves QUEUED work, and `gopi task receive` is the leg for a row's outcome"
	// syncRowWhyTerminal: failed/cancelled ended without a deliverable.
	syncRowWhyTerminal = "the row is TERMINAL here — its run is over, so a copy cannot re-run it and the verdict is already on this ledger"
	// syncRowWhyNotQueued: the status is outside the set this family knows —
	// the honest answer is "not the queued state", never a guess.
	syncRowWhyNotQueued = "the row is not in the queued state, so it is not work this leg may move"

	// --- the opt-in source cancellation (--cancel-source) ----------------
	//
	// THE ORDER IS THE WHOLE SAFETY ARGUMENT: the source row is cancelled
	// AFTER a peer answers a PERSISTED receipt (stored / migrated / already)
	// and NEVER on a refusal, so a refused or unreachable peer leaves the row
	// exactly where it was — still pending here, still dispatchable. The
	// manual three-step migration handed that ordering to the operator; the
	// flag encodes it.
	syncRowCancelReasonFormat  = "sent to peer %s by `gopi task send --cancel-source` (receipt: %s, row %s) — cancelled HERE so this host does not dispatch it a second time; undo with `gopi task update %s --status %s --reason WHY`"
	syncRowCancelRefusedFormat = "refused by peer %s: %s"
	syncRowCancelLineFormat    = "cancel-source: %s cancelled here (peer %s holds it: %s)"
	syncRowCancelKeptFormat    = "cancel-source: %s LEFT PENDING here — no peer persisted the row (%s), so nothing moved and the source is UNCHANGED (a move cancels the source only once a peer's ledger holds the row)"
	syncRowCancelNoReceipt     = "the send produced no receipt to cancel on"

	// syncRowHandoffStampFailSuffix is appended to the migration's confirmation
	// line when the row's own migration marker could not be written. It names the
	// CONSEQUENCE (the cancel stands, only the marker is missing) because that is
	// what an operator has to act on: the row is cancelled here and the peer holds
	// it, so nothing about the move is in doubt — only the record other lanes read
	// to know the work is elsewhere.
	syncRowHandoffStampFailSuffix = " — NOTE: the row's migration marker could not be written (%v); the migration itself stands (cancelled here, held by the peer), but a lane reading the row as a plain cancellation could re-issue its work"

	// syncRowPrefix is the refusal prefix of this leg (one spelling).
	syncRowPrefix = "row send: "

	// syncRowRemoteRowCountFormat is the peer handler's arity refusal. ONE
	// spelling: the sentence states the RANGE the handler admits and the count
	// that arrived, because the handler cannot tell a mis-selection from a
	// hostile sender — and both are refused the same way, by name.
	syncRowRemoteRowCountFormat = "row-remote: want 1..%d row(s), got %d — this leg carries at most one row per selection and the handler settles the whole patch it is given; an empty or oversized patch means the sender's selection is wrong"

	// syncRowEffectStored / Migrated / Already are the receipt vocabulary
	// this leg reports in — ONE spelling each, shared by the receipt line and
	// by the cancellation reason (Rule 1b/13: the reason quotes the effect a
	// reader saw on stdout).
	syncRowEffectStored   = "stored"
	syncRowEffectMigrated = "migrated"
	syncRowEffectAlready  = "already current"

	// --- the sender-side EDGE gate (this leg's second precondition) ------
	//
	// A one-row leg ships the row and NOTHING ELSE, so the ROW'S OWN EDGE is
	// the only thing that can carry its dependency across — and the peer
	// refuses an edge it cannot resolve (syncParentUnresolvable), which turns
	// a chain sent child-first into a refusal instead of a permanently parked
	// row. This gate is the SENDER's half of the same judgement, asked BEFORE
	// any peer is contacted: it names what is wrong and what to do about it
	// here, where the operator still has the whole chain in front of them.
	//
	// The chain preamble an operator actually needs, in one sentence:
	// send the PARENT first (in chain order), and the child's own edge then
	// resolves in the peer's ledger the moment the parent's row lands.
	syncRowParentMissingFormat    = "row send: task %s depends on parent %s, which is not in the peer %s ledger — a one-row leg ships only this row, so the child would arrive with a dangling edge (Rule 33 parks such a row permanently: %s releases a child, and no other status does); send the PARENT row first and re-run this send, or use the window legs so parent and child travel together"
	syncRowParentTerminalFormat   = "row send: task %s depends on parent %s, which is %s in the peer %s ledger — that verdict does not release a child (Rule 33: %s releases), so the child would arrive parked forever; the repair is a REISSUE of the parent on the peer (its successor carries the work), never a copy of its child"
	syncRowParentUnreadableFormat = "row send: task %s depends on parent %s, but the peer %s ledger could not be read to resolve it — an unresolvable dependency is never assumed satisfied (send refused before any row left this host)"
)

// --- the wire receipt -------------------------------------------------------

// SyncRowReceipt is the peer handler's answer document — what the peer's
// LEDGER did with the row, counted by SyncTaskImportDelta. It is a RECEIPT,
// not a verdict: every field is copied from the import's own stats, so a
// caller can never be told "accepted" about a row that did not land.
type SyncRowReceipt struct {
	// Stored/Already/Migrated/Refused are the import's counts, one row each
	// (the wire carries ONE row, so at most one of them is 1).
	Stored   int `json:"stored,omitempty"`
	Already  int `json:"already,omitempty"`
	Migrated int `json:"migrated,omitempty"`
	Refused  int `json:"refused,omitempty"`
	// Status is the peer ledger's row state AFTER the merge ("" = the row is
	// not in that ledger and was refused).
	Status string `json:"status,omitempty"`
	// Node identifies the answering node (provenance for a two-node probe).
	Node string `json:"node,omitempty"`
	// Reason carries the import's refusal text verbatim (Rule 15b: the
	// failure names itself, never an empty "refused").
	Reason string `json:"reason,omitempty"`
	// Unreachable is set LOCALLY (never by the peer) when no answer arrived
	// at all — the transport failed or the response did not parse. It exists
	// so an absent answer is a NAMED outcome in the same vocabulary as a
	// refusal, instead of an empty receipt a caller could mistake for "the
	// row is already there" (Rule 15).
	Unreachable bool `json:"unreachable,omitempty"`
}

// Effect is the receipt's ONE-WORD verdict — what the peer's ledger did with
// the row — and it is the value the send line prints and the cancellation
// reason quotes. A REFUSAL is its own effect: `refused` is a verdict about
// the row NOT landing, and every caller that must not act on a failed send
// asks THIS method rather than re-interpreting the counts (Rule 14: one
// reading of the receipt, shared by the printer and the canceller).
func (r SyncRowReceipt) Effect() string {
	switch {
	case r.Refused > 0 || r.Unreachable:
		return syncRowEffectRefused
	case r.Stored > 0:
		return syncRowEffectStored
	case r.Migrated > 0:
		return syncRowEffectMigrated
	default:
		return syncRowEffectAlready
	}
}

// Persisted reports whether the peer's ledger actually HOLDS the row after the
// exchange — the gate the opt-in source cancellation stands on. True only for
// the three persisted effects; a refusal (or a leg that never reached a peer)
// is false, so the source row is left untouched.
func (r SyncRowReceipt) Persisted() bool {
	switch r.Effect() {
	case syncRowEffectStored, syncRowEffectMigrated, syncRowEffectAlready:
		return true
	default:
		return false
	}
}

// SyncRowSendResult is the outcome of ONE row send: the receipt lines a caller
// prints, plus — for each peer — the receipt the peer answered. The receipts
// are RETURNED rather than folded into a printed string because the opt-in
// source cancellation must be decided on the RECEIPT (was the row persisted?
// by whom?), never on the text a human reads (Rule 15b: prove the effect).
type SyncRowSendResult struct {
	// Lines are the per-peer receipt lines (one per peer, in peer order).
	Lines []string
	// Receipts are the same answers in structured form. A peer that could not
	// be reached, or whose answer did not parse, contributes a receipt with
	// Unreachable set — a NON-persisting outcome, so it can never authorise a
	// cancellation.
	Receipts []SyncRowPeerReceipt
	// Disposition is what happened to the SOURCE row — the move's second half.
	// It is filled only when the caller asked for a move (CancelSource), and it
	// is the whole answer INCLUDING "nothing happened": a send with no
	// persisting receipt leaves it here with Cancelled=false and a Note saying
	// why, so the caller never has to infer the outcome from silence (Rule 15).
	// nil = no move was asked for (the primitive produced no disposition).
	Disposition *SyncRowSourceDisposition
}

// SyncRowPeerReceipt is one peer's answer together with the peer it came from.
type SyncRowPeerReceipt struct {
	Peer    string
	Receipt SyncRowReceipt
}

// syncRowResolveTaskID resolves a TYPED task id to this ledger's row — the ONE
// id→row step of the send/move leg (Rule 14), taking the ledger handle the
// caller already owns (so it costs no second connection).
//
// WHY IT EXISTS (measured 2026-10-07, defect
// 01a117082d5473c7a8ed247ec357795d). Every reader on this leg handed the raw
// operand to `task_id = ?`. A 12-character id copied out of a listing therefore
// read as "this ledger holds no such row" — and on the MOVE leg that answer is
// not an error but a SILENT SKIP: syncRowTransferGate treats an absent row as
// the export's business, so the dependency criterion was never asked, the
// command exited 0, and the operator saw "the row has no live child" when in
// truth nothing had looked. The result is the same shape for ANY shorter
// spelling of a row id that exists.
//
// THE RULE IT OBEYS IS NOT INVENTED HERE. internal/fullid owns the project's
// single spelling of "a task/session id is always used in FULL; a prefix is
// never resolved, it is refused, and the refusal names every full id it could
// have meant" (the ledger's ids are uuid v7; measured on this host, `01a0b51d`
// named 35 rows). That rule was reachable only on the SESSION paths; a prefix
// reaching a LEDGER read was reported as absence — the same operand answered
// two ways depending on which door it used. This function closes that gap
// rather than opening a second resolver: it does not look the prefix up and
// guess, it names the candidates and stops.
//
// An ABSENT row stays absent: a full id (or any operand that is not the uuid
// shape cut short — `t-<epoch>`, a test's probe id) is looked up exactly as
// before and answered by the leg's own "not in this ledger" sentence, because
// two refusals for one fact would be two places to keep in step.
func syncRowResolveTaskID(ctx context.Context, q QueryExecer, raw string) (*Task, error) {
	id := taskIDNormalize(raw)
	if id == "" || !fullid.CanBePrefix(id) {
		return taskByIDOn(ctx, q, id)
	}
	cands, err := taskIDPrefixCandidates(ctx, q, id)
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf(syncRowIDRefusalFormat, fullid.Refusal(fullid.KindTask, id, cands))
}

// syncRowResolveOnLedger is syncRowResolveTaskID against the ledger the caller
// NAMED, opened and closed for the duration (the same pin the gates use, so
// `--db PATH` means the same ledger here as everywhere else on the verdict).
func syncRowResolveOnLedger(dbPath, taskID string) (*Task, error) {
	var row *Task
	if err := syncRowWithLedger(dbPath, func() error {
		db, derr := openTasksDB(context.Background(), memoryDBPath())
		if derr != nil {
			return derr
		}
		defer db.Close() //dead-end:ignore the handle belongs to this read; its verdict travels back with the row
		var rerr error
		row, rerr = syncRowResolveTaskID(context.Background(), db, taskID)
		return rerr
	}); err != nil {
		return nil, err
	}
	return row, nil
}

// PersistedPeers names the peers whose ledger HOLDS the row (the persisted
// effects). An empty answer is a fact worth reporting — it is what leaves the
// source row where it was.
func (res SyncRowSendResult) PersistedPeers() []string {
	out := make([]string, 0, len(res.Receipts))
	for _, pr := range res.Receipts {
		if pr.Receipt.Persisted() {
			out = append(out, pr.Peer)
		}
	}
	return out
}

// syncRowReceiptFromStats projects the import's stats into the receipt.
// status is the row's state in the PEER ledger after the merge (read back by
// the handler); "" when the import refused the row.
func syncRowReceiptFromStats(stats *SyncTaskImportStats, status string) SyncRowReceipt {
	r := SyncRowReceipt{
		Stored:   stats.RowsImported,
		Already:  stats.RowsAlready,
		Migrated: stats.RowsMigrated,
		Status:   status,
		Node:     claimBroadcastNodeName(),
	}
	if stats.RowsRefused > 0 {
		r.Refused = stats.RowsRefused
		r.Reason = strings.Join(stats.Errors, "; ")
	}
	if r.Stored+r.Already+r.Migrated+r.Refused == 0 {
		// The import ran and moved nothing: the row was an idempotent echo
		// that the ladder absorbed under a different counter (tag union,
		// divergence, terminal-tier tie). "Already" is the honest word for
		// "your row is present and current" — never a silent zero.
		r.Already = 1
	}
	return r
}

// --- the send leg -----------------------------------------------------------

// SyncRowSendOptions selects ONE row and the peers it is sent to.
type SyncRowSendOptions struct {
	// TaskID is the row to send (required: a row leg has no window).
	TaskID string
	// Peers are the destination aliases (already resolved by the caller; each
	// one is re-validated against the DECLARED topology here).
	Peers []string
	// DBPath overrides the local ledger ("" = the default path).
	DBPath string
	// RemoteHome, when set, is exported as GOPI_HOME in the peer-side argv —
	// the ISOLATION channel (Rule 34). The peer resolves its own ledger from
	// its own environment, so without this a probe writes into the peer
	// OPERATOR's profile; with it the peer's handler runs against a private
	// profile. "" = the peer's own default (the production shape).
	RemoteHome string
	// Exchange replaces the ssh transport (tests inject a peer answer).
	Exchange func(peer string, body []byte) ([]byte, error)
	// CancelSource says the caller will cancel the source row HERE once a peer
	// has persisted it (`--cancel-source`) — i.e. this leg is a MOVE, not a
	// copy. It is what decides how much of the dependency criterion applies: a
	// COPY changes nothing on this ledger (the source stays, so every edge
	// around it resolves as before and the receiver prices the edge it is
	// handed), while a MOVE removes the row from this ledger and therefore has
	// to answer for the rows that depend on it. See syncRowTransferGate.
	CancelSource bool
}

// SyncRowSend ships ONE row to every named peer and returns the per-peer
// RECEIPT lines. The structured form of the same answer rides
// SyncRowSendEx — that is the door the opt-in source cancellation reads, and
// this one stays the leg's ordinary, text-only face.
func SyncRowSend(opts SyncRowSendOptions) ([]string, error) {
	res, err := SyncRowSendEx(opts)
	if err != nil {
		return nil, err
	}
	return res.Lines, nil
}

// SyncRowSendEx is SyncRowSend with the RECEIPTS kept.
//
// TWO THINGS HAPPEN HERE THAT DID NOT BEFORE, and both are gates rather than
// new machinery:
//
//  1. THE STATUS GATE (syncRowStatusGate) — the row must be pending when the
//     send starts. Every other status is refused BY NAME, with the current
//     status, BEFORE any peer is contacted (Rule 15: a refused send says why,
//     and it is provable that nothing left this host).
//  2. THE RECEIPTS — every outcome, including "no answer at all", is recorded
//     as a SyncRowPeerReceipt, so a caller can ask what the peer's LEDGER did
//     without parsing the line it printed (Rule 15b: prove the effect).
func SyncRowSendEx(opts SyncRowSendOptions) (*SyncRowSendResult, error) {
	taskID := taskIDNormalize(opts.TaskID)
	if taskID == "" {
		// The FLAG spelling belongs to the CLI (cmd/agent owns the family's
		// flag constants); this layer names the missing VALUE, and the parse
		// case adds the spelling the operator typed (Rule 14: one owner per
		// vocabulary).
		return nil, fmt.Errorf(syncRowPrefix + "a task id is required (a row leg carries no delta window)")
	}
	if len(opts.Peers) == 0 {
		return nil, fmt.Errorf(syncRowPrefix+"no peers to send to (declare sync_peers in settings.json %q / %s)",
			config.SyncPeersSettingsKey, config.SyncPeersEnv)
	}
	// THE ID ITSELF IS RESOLVED FIRST, before a peer is chosen, before the
	// status gate and before the dependency gate — because an operand that
	// matches NO row is not a verdict on the row, it is the absence of one, and
	// every gate below reads "no such row" as "nothing to judge". A truncated
	// id is a refusal HERE (fullid's sentence, resolved against this ledger);
	// an id this ledger genuinely does not hold keeps the leg's own missing-row
	// answer further down, unchanged. See syncRowResolveTaskID.
	// It runs whether or not --cancel-source was given: the same command must
	// not answer one way for the COPY that was typed and another for the MOVE
	// the operator meant.
	if _, err := syncRowResolveOnLedger(opts.DBPath, taskID); err != nil {
		return nil, err
	}
	// THE MOVE-ONLY GATE — a send IS a move. It runs before the status gate,
	// before the export and before ANY peer is contacted, so a refused send is
	// provably a no-op (Rule 15: nothing left this host).
	//
	// WHY IT IS NOT OPTIONAL ANY MORE. `send` used to degrade into a COPY when
	// the caller omitted --cancel-source: the row stayed dispatchable HERE while
	// a copy was dispatchable on the peer, so one task could be claimed and run
	// TWICE — two live copies of one row, and every later reconciliation (the
	// sync layer's "newer wins", its status-mismatch family) compares two
	// ledgers that both believe they own the work. "One row globally" is
	// therefore not a property of the system while it remains a parameter of
	// the CALLER. The optionality is what this refuses; the two legal shapes are
	// named in the refusal so the operator's next keystroke is written out.
	if err := syncRowMoveOnlyGate(opts.CancelSource); err != nil {
		return nil, err
	}
	// THE STATUS GATE runs before the export and before any contact: sending a
	// row that is not queued is the defect this leg refuses, and a refusal that
	// had already written the row onto a peer would be no refusal at all.
	if err := syncRowStatusGate(taskID, opts.DBPath); err != nil {
		return nil, err
	}
	// THE DEPENDENCY GATE — the leg's THIRD precondition, and the one the
	// other two cannot see (Rule 33 + Rule 15). It asks THE exported
	// criterion (TaskTransferEligible) both halves of the dependency question
	// this ledger can answer: the candidate's OWN parent must be RELEASED
	// here, and NO live row may depend on the candidate.
	//
	// IT APPLIED TO A MOVE ONLY while `--cancel-source` was optional: a COPY
	// left the source row here, so the graph this ledger holds was unchanged
	// and a child's edge rode with it. A SEND IS NOW ALWAYS A MOVE (the
	// move-only gate above), so there is no copy arm left to exempt: every
	// send cancels the source once a peer persists the row, and a child left
	// behind would hold an edge to a status Rule 33 never releases — parked
	// forever. Asked BEFORE any peer is contacted, so a refused send is
	// provably a no-op.
	if err := syncRowTransferGate(taskID, opts.DBPath); err != nil {
		return nil, err
	}
	exchange := opts.Exchange
	if exchange == nil {
		exchange = func(peer string, body []byte) ([]byte, error) {
			return syncRowSSHExchange(peer, body, opts.RemoteHome)
		}
	}
	res := &SyncRowSendResult{
		Lines:    make([]string, 0, len(opts.Peers)),
		Receipts: make([]SyncRowPeerReceipt, 0, len(opts.Peers)),
	}
	for _, peer := range opts.Peers {
		// The declared-topology gate, asked before any contact (Rule 14: ONE
		// sentence for the family, owned by RequireDeclaredPeer).
		if err := RequireDeclaredPeer(peer, nil); err != nil {
			return nil, fmt.Errorf(syncRowPrefix+"%w", err)
		}
		// THE SELF-PEER GATE — the push half of the write-leg criterion
		// (peer_self_host.go). This leg SHIPS A ROW, so it is the strongest
		// case of "a write whose profile the caller did not bind": with no
		// --remote-home and an alias that resolves to THIS host, the peer-side
		// handler runs in the profile the local login shell resolves, which is
		// how four fixture rows reached the operator's ledger in real work
		// (measured 2026-10-08). Asked BEFORE any contact, exactly like the
		// topology gate above it, so a refusal here provably wrote nothing.
		if err := selfPeerWriteRefusal(peer, opts.RemoteHome, memoryDBPathOr(opts.DBPath), "row send"); err != nil {
			return nil, fmt.Errorf(syncRowPrefix+"%w", err)
		}
		line, receipt := syncRowSendToOne(peer, taskID, opts.DBPath, exchange)
		res.Lines = append(res.Lines, line)
		res.Receipts = append(res.Receipts, SyncRowPeerReceipt{Peer: peer, Receipt: receipt})
	}
	// THE MOVE'S OTHER HALF RUNS HERE, and this is the only place it can run:
	// a send is a MOVE (syncRowMoveOnlyGate refuses anything else), so the
	// source row must be signed off once a peer's ledger persisted it, and the
	// post-condition may only be read AFTER that write. Splitting the two would
	// mean a caller holding a result whose row is still pending here for the
	// whole window between "the peer stored it" and "this host cancelled it" —
	// two live copies, which is the defect the leg exists to prevent. So the
	// primitive performs both effects and keeps the disposition it produced.
	//
	// A peer that persisted NOTHING leaves the source untouched (the safe order,
	// not a failed post-condition), so no verdict is read against it: the note
	// in Disposition says in words that nothing moved.
	if opts.CancelSource {
		disp := SyncRowCancelSource(SyncRowCancelSourceOptions{
			TaskID: taskID,
			Result: res,
			DBPath: opts.DBPath,
		})
		res.Disposition = &disp
		if !disp.Cancelled {
			return res, nil
		}
		// ④ THE POST-CONDITION OF A MOVE, taken from the ledger AFTER the
		// cancellation: a row that is still pending/in_progress here while a
		// peer holds it is exactly the second copy, so it is an ERROR naming
		// the peer (Rule 15: never a green exit over two live copies).
		if _, verr := TaskMoveVerdict(taskID, disp.At, opts.DBPath); verr != nil {
			return res, verr
		}
	}
	return res, nil
}

// syncRowSendToOne runs ONE peer's leg and answers both faces of its outcome:
// the line a human reads, and the receipt a caller decides on. Every arm that
// could not read the peer's ledger answers a NON-PERSISTING receipt
// (Unreachable), so "the transport failed" can never be mistaken for "the peer
// already had it" — the confusion that would let a --cancel-source send cancel
// a row no peer holds (Rule 15).
func syncRowSendToOne(peer, taskID, dbPath string, exchange func(string, []byte) ([]byte, error)) (string, SyncRowReceipt) {
	patch, err := syncRowExportOne(taskID, peer, dbPath)
	if err != nil {
		return fmt.Sprintf("send %s: %v", peer, err), SyncRowReceipt{Unreachable: true, Reason: err.Error()}
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return fmt.Sprintf("send %s: marshal (fail-soft): %v", peer, err), SyncRowReceipt{Unreachable: true, Reason: err.Error()}
	}
	stdout, err := exchange(peer, body)
	if err != nil {
		return fmt.Sprintf("send %s: unreachable (fail-soft): %v", peer, err), SyncRowReceipt{Unreachable: true, Reason: err.Error()}
	}
	var receipt SyncRowReceipt
	if err := json.Unmarshal(stdout, &receipt); err != nil {
		return fmt.Sprintf("send %s: response: %v", peer, err), SyncRowReceipt{Unreachable: true, Reason: err.Error()}
	}
	return syncRowSendLine(peer, taskID, receipt), receipt
}

// syncRowMoveOnlyRefusal is the ONE sentence a send without --cancel-source
// gets (Rules 1b/15): it names the cost the flag used to be optional about and
// writes out BOTH legal shapes, so the operator's next keystroke is in the
// message rather than in the manual.
const syncRowMoveOnlyRefusal = "%sa task send is a MOVE, and --%s is what makes it one: without it the row stays PENDING here while the peer's ledger queues a copy, so one task is dispatchable on two machines and can run twice (two live copies of one row; the sync layer's status-mismatch family downstream). Spell the move — `gopi task send --task-id T --peer ALIAS --%s` (push), or take the row from its own side — `gopi task receive --task-id T --peer ALIAS --%s` (pull; the peer signs it off its queue first, pending → cancelled)"

// syncRowMoveOnlyGate refuses a send that would be a COPY. It is the ONE
// decision of "is this invocation a move" on the push leg, asked before any
// peer contact — see SyncRowSendEx's call site for the argument.
func syncRowMoveOnlyGate(moves bool) error {
	if moves {
		return nil
	}
	return fmt.Errorf(syncRowMoveOnlyRefusal, syncRowPrefix,
		CancelSourceFlagSpelling, CancelSourceFlagSpelling, TakeOverFlagSpelling)
}

// The two flag spellings the refusal names. They live in the tools layer
// because the refusal is THIS layer's text (Rules 1b/13): the CLI's own
// constants are built from them, so the sentence a refusal prints and the flag
// the parser admits are one value and cannot drift.
const (
	// CancelSourceFlagSpelling is `--cancel-source`: the push leg's half
	// of a move. It is NO LONGER OPTIONAL — a send without it is refused by
	// syncRowMoveOnlyGate, because an optional move is a copy by default.
	CancelSourceFlagSpelling = "cancel-source"
)

// syncRowMoveVerdict is THE post-condition of a move, shared by both legs
// (deliverable ④): after a move the row must no longer be LIVE on THIS ledger.
// The send's own cancellation already proves the effect it performed, but "the
// row is not dispatchable here any more" is the fact every downstream reader
// stands on — a pending or in_progress row here is a second copy a dispatcher
// can claim — and it is cheap to ask once at the end. It answers the ledger's
// own status (syncRowMoveVerdictAbsent when the row is not here at all, which
// is as moved as a row gets), or a refusal naming what the row still is and
// which peer holds it.
func syncRowMoveVerdict(taskID, peer, dbPath string) (string, error) {
	status, err := syncRowStatusOn(dbPath, taskID)
	if err != nil {
		return "", fmt.Errorf(syncRowPrefix+"the move could not be verified: task %s's status on THIS ledger could not be read: %w", taskID, err)
	}
	if status == "" {
		return syncRowMoveVerdictAbsent, nil
	}
	// A terminal status is the post-condition. `reopened` is NOT terminal (the
	// row is queue work again), so it is refused like a live one.
	if !terminalTaskStatus(status) {
		return status, fmt.Errorf(syncRowMoveVerdictFormat, syncRowPrefix, taskID, status, peer, taskID, taskStatusPending)
	}
	return status, nil
}

// TaskMoveVerdict is the exported face of the same post-condition, for the CLI's
// send leg (deliverable ④). The pull leg asks the same question through
// taskClaimFromCancelled's CAS: both legs land on "the source ledger no longer
// holds a live row", one by re-reading, one by the claim that can only win on a
// cancelled row.
func TaskMoveVerdict(taskID, peer, dbPath string) (string, error) {
	return syncRowMoveVerdict(taskIDNormalize(taskID), peer, dbPath)
}

const (
	// syncRowMoveVerdictFormat names the row, the state it is still in and the
	// peer the move deposited it on, so the operator can act on either side.
	syncRowMoveVerdictFormat = "%sthe move did NOT land: task %s is still %s on THIS ledger although peer %s reported it stored the row — two live copies of one task, either can be dispatched; cancel it here (`gopi task update %s --status %s --reason WHY`) or cancel the peer's copy"
	// syncRowMoveVerdictAbsent is the other honest terminal answer: the row is
	// not on this ledger at all, which is as moved as a row gets.
	syncRowMoveVerdictAbsent = "absent from this ledger"
)

// THIS ledger's state for the row and answers ONE refusal naming the current
// status and the reason that status is refused.
func syncRowStatusGate(taskID, dbPath string) error {
	status, err := syncRowStatusOn(dbPath, taskID)
	if err != nil {
		return fmt.Errorf(syncRowPrefix+"status of task %s could not be read from this ledger: %w", taskID, err)
	}
	if status == "" || status == TaskStatusPending {
		// "" = this ledger holds no such row. The GATE says nothing about
		// absence: the export that runs next is the authority on what this
		// ledger holds, and it names the missing id itself. Two refusals for
		// one fact would be two places to keep in step.
		return nil
	}
	return fmt.Errorf(syncRowStatusGateFormat, taskID, status, TaskStatusPending, syncRowWhyNotPending(status))
}

// syncRowTransferGate is the DEPENDENCY precondition of the send leg — and it
// runs on EVERY send, because a send no longer has a non-moving arm. It asks
// THE exported criterion (TaskTransferEligible — the ONE judgment master's
// transfer legs already share) whether this row may LEAVE this ledger, and
// turns a refusal into the leg's own error text. It runs BEFORE any peer is
// contacted, so a refused send has demonstrably touched nothing.
//
// IT ANSWERS ONLY FOR A MOVE (`moves` = the caller will cancel the source here
// once a peer has it, i.e. `--cancel-source`). A send without that flag is a
// COPY: the source row stays, so every edge around it still resolves exactly
// as before, and a child travelling with its parent's edge is the DESIGNED
// flow of a chain migration (the edge rides each child; see
// memory_sync_parent_chain_test.go). The receiver is what prices a travelling
// edge — syncParentVerdict refuses one it cannot resolve — and that is where
// the cost of a copy belongs. `--cancel-source` is the shape that can destroy
// work: the row is cancelled HERE as soon as a peer persists it, so the rows
// that waited on it would hold an edge to a status Rule 33 never releases and
// be parked forever. Measured harm, measured guard.
//
// The row is read by id, so an id this ledger does not hold is left to the
// EXPORT to name (the same division syncRowStatusGate already follows: the
// gate judges what the ledger shows, the export is the authority on absence —
// two refusals for one fact would be two places to keep in step).
// The row is resolved through syncRowResolveTaskID — the LEG's ONE id→row step
// for a typed operand — and then judged exactly as before. That resolution is
// not a convenience: under `task_id = ?` a truncated id read as "this ledger
// holds no such row", which THIS function deliberately treats as "not mine to
// judge" (the export is the authority on absence, below). So the criterion was
// never asked, and the command exited 0 — the operator was shown the answer
// "no live child" by a run that had looked at nothing. An id that IS this
// ledger's row and is typed short is now refused by name (fullid's sentence,
// every candidate listed) instead of silently skipping the gate; an id this
// ledger does not hold still falls through to the export, unchanged.
func syncRowTransferGate(taskID, dbPath string) error {
	var row *Task
	var dep TaskTransferDependency
	if err := syncRowWithLedger(dbPath, func() error {
		db, err := openTasksDB(context.Background(), memoryDBPath())
		if err != nil {
			return err
		}
		defer db.Close() //dead-end:ignore the handle belongs to this read; every error below is returned to the caller
		// The candidate and its live dependents are read in ORDER, one query
		// at a time: the ledger openers pin a SINGLE connection, so a cursor
		// left open would block the next read (the same rule
		// TaskTransferSelectOne follows).
		row, err = syncRowResolveTaskID(context.Background(), db, taskID)
		if err != nil {
			return err
		}
		if row == nil {
			return nil
		}
		dep, err = TaskTransferLiveChild(context.Background(), db, row.TaskID)
		return err
	}); err != nil {
		// A resolver refusal (a truncated id) travels out with this frame's
		// prefix but FULLID's OWN sentence appended — it is a verdict about the
		// OPERAND, and its candidates are the only thing that makes it
		// actionable. It is deliberately not rewritten into the "dependencies
		// could not be read" sentence below: nothing was read, nothing failed,
		// and the operator typed an id the ledger cannot resolve.
		if fullid.CanBePrefix(taskIDNormalize(taskID)) {
			return fmt.Errorf(syncRowPrefix+"%w", err)
		}
		return fmt.Errorf(syncRowPrefix+"the dependencies of task %s could not be read from this ledger: %w", taskID, err)
	}
	if row == nil {
		return nil
	}
	// taskTransferSkipSet("") is "this ledger excludes nothing" — the leg's
	// own question is about the CHAIN, and the send's destination exclusion
	// (the receiving dispatcher's skip set) is the export's to price.
	if ok, why := TaskTransferEligible(row, dep, taskTransferSkipSet("")); !ok {
		return fmt.Errorf(syncRowTransferGateFormat, row.TaskID, why)
	}
	return nil
}

// syncRowWhyNotPending is the REASON half of the gate: one sentence per
// status, each naming what sending the row would actually cost.
func syncRowWhyNotPending(status string) string {
	switch status {
	case TaskStatusInProgress:
		return syncRowWhyInProgress
	case TaskStatusClaimed:
		return syncRowWhyClaimed
	case TaskStatusCompleted, TaskStatusVerified:
		return syncRowWhyHusk
	case TaskStatusFailed, TaskStatusCancelled, TaskStatusSuperseded:
		return syncRowWhyTerminal
	default:
		return syncRowWhyNotQueued
	}
}

// syncRowSendLine renders ONE peer's receipt. Every arm names the EFFECT
// (stored / already current / REFUSED) and the row, so a reader never has to
// interpret an "accepted" that means nothing about the ledger.
func syncRowSendLine(peer, taskID string, r SyncRowReceipt) string {
	switch {
	case r.Refused > 0:
		return fmt.Sprintf("send %s: REFUSED: %s", peer, r.Reason)
	case r.Stored > 0:
		return fmt.Sprintf("send %s: stored (row %s, status=%s)", peer, taskID, r.Status)
	case r.Migrated > 0:
		return fmt.Sprintf("send %s: migrated (row %s, status=%s)", peer, taskID, r.Status)
	default:
		return fmt.Sprintf("send %s: already current (row %s, status=%s)", peer, taskID, r.Status)
	}
}

// syncRowExportOne exports exactly the named row as a one-row patch. The
// reflow gate is skipped on purpose, for the same reason exportClaimPatch
// skips it (memory_sync_claim.go): this is a MANUAL re-send aimed at a row the
// operator named, and a live `from-<peer>` row is exactly the row a re-send
// exists to carry — the gate would hide it and the leg would answer "not found
// in the ledger" about a row sitting in the local ledger.
//
// THE ROW'S COLUMNS COME FROM THE LEDGER, NOT FROM THE PATCH. The patch is the
// export's selection, and its `task_id` is matched exactly; on a COPY the
// operand that arrives here is already a resolved row id (SyncRowSendEx refuses
// a truncated one up front, and MOVE's gate resolves it too), but this face is
// also driven directly by tests and by callers that skip those heads, and the
// id a PEER is told to look for must be the ledger's spelling in every case.
// So the exact match stays (it is what extracts the one-row patch) and a
// single resolve supplies the canonical id for the wire.
func syncRowExportOne(taskID, peer, dbPath string) (*SyncTaskPatch, error) {
	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{AfterSource: 0, SkipReflowGate: true, Peer: peer, DBPath: dbPath})
	if err != nil {
		return nil, fmt.Errorf("row send: export: %w", err)
	}
	for i := range patch.Rows {
		if patch.Rows[i].TaskID != taskID {
			continue
		}
		// ONE resolve, so a caller that reached this face with a short (or
		// padded) spelling still puts the ROW'S id on the wire — the peer's
		// handler looks rows up by exact id, and a truncated one would land
		// there as a second, phantom row.
		row, rerr := syncRowResolveOnLedger(dbPath, taskID)
		if rerr != nil {
			return nil, rerr
		}
		if row == nil {
			return nil, fmt.Errorf("row send: task %s not found in this ledger", taskID)
		}
		patch.Rows = []SyncTaskRow{patch.Rows[i]}
		patch.Rows[0].TaskID = row.TaskID
		patch.ExportedAt = time.Now().Unix()
		patch.Generator = syncRowGenerator + claimBroadcastNodeName()
		return patch, nil
	}
	return nil, fmt.Errorf("row send: task %s not found in this ledger", taskID)
}

// syncRowSSHExchange is the send leg's transport: pipe the one-row patch to
// `ssh <peer> gopi task sync --row-remote` and return the peer's receipt.
// The remote command is ONE argv element (syncRemoteGopi's own contract:
// separate elements make the peer's login shell re-join them and gopi can read
// the whole thing as a positional prompt).
func syncRowSSHExchange(peer string, body []byte, remoteHome string) ([]byte, error) {
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("row send: %w", err)
	}
	// THE PRODUCTION PATH IS GONE, and this is where it used to be: the leg
	// composed `ssh <peer> gopi task sync --row-remote`, a DELETED verb
	// (2026-11-05) that now answers with a removal notice + exit 1. The argv
	// RENDERER went with it — it had no reader left once nothing handed the
	// string to ssh (Rule 43: a symbol whose only referrer was the mechanism
	// is deleted with it, never kept as a decorative constant). The refusal
	// happens before a connection exists; remote_leg_retired.go states the
	// mechanism once, and `remoteHome` no longer has an argv to prefix.
	_ = remoteHome
	return nil, remoteLegRetired("row send", peer)
}

// syncRowRemotePrefix renders the isolation prefix for the PEER-side argv
// from ONE directory:
//
//	GOPI_HOME='<dir>' PATH='<dir>/bin':$PATH
//
// TWO bindings, one directory, because a sandbox is an EXECUTION ENVIRONMENT
// (Rule 34), not just a path: GOPI_HOME decides which ledger the handler
// writes, and the profile's own bin/ decides which BUILD does the writing —
// so a probe runs its own binary against its own ledger and the peer's
// installed copy and operator profile are both untouched. The tail keeps the
// peer's PATH reachable, so a sandbox without a bin/ is the ordinary
// deployment (the peer's own gopi answers).
//
// harvestShellQuote owns the single-quoting of both values — they travel
// through the peer's login shell, and `$PATH` must stay OUTSIDE the quotes so
// the peer expands it.
func syncRowRemotePrefix(home string) string {
	return syncRemoteHomePrefix + harvestShellQuote(home) +
		syncRemotePathPrefix + harvestShellQuote(home+syncRemoteBinDir) + syncRemotePathTail
}

// --- the peer side ----------------------------------------------------------

// SyncRowRemote is the PEER side of a row send (`gopi task sync --row-remote`):
// read the one-row patch from stdin, apply it through the SAME merge authority
// every other leg uses, read the row's resulting state back out of THIS
// ledger, and answer the receipt. One handler, no new judgement.
func SyncRowRemote(stdin []byte) ([]byte, error) {
	var patch SyncTaskPatch
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(stdin))), &patch); err != nil {
		return nil, fmt.Errorf("row-remote: parse stdin: %w", err)
	}
	if err := syncCheckPatchVersion("row-remote", patch.Version, syncPatchVersionDelta); err != nil {
		return nil, err
	}
	// A MULTI-ROW PATCH ARRIVES AS ONE PATCH (max syncRowPatchMaxRows rows) and
	// a one-row send arrives as one. Both are the same import; only the arity
	// bound differs, and it is stated by the caller's own selection. An empty
	// patch is still refused by name: "the sender sent nothing" is a selection
	// defect, never a no-op to merge.
	if len(patch.Rows) < 1 || len(patch.Rows) > syncRowPatchMaxRows {
		return nil, fmt.Errorf(syncRowRemoteRowCountFormat, syncRowPatchMaxRows, len(patch.Rows))
	}
	stats, err := SyncTaskImportDelta(&patch)
	if err != nil {
		return nil, err
	}
	// The receipt's status is read BACK from the ledger (never assumed from
	// the patch): it answers "what does this node hold now".
	status := ""
	if row, rerr := taskStatusDirect(patch.Rows[0].TaskID); rerr == nil && row != nil {
		status = row.Status
	}
	return json.Marshal(syncRowReceiptFromStats(stats, status))
}

// --- the receive leg --------------------------------------------------------

// syncRowArrivalGateFormat is the ONE spelling of the arrival criterion's
// refusal prefix. Only the PREFIX lives here; the sentence it carries is the
// criterion's (task_transfer_eligibility.go owns the reason vocabulary for
// both directions, so a push refusal and an arrival refusal cannot drift).
const syncRowArrivalGateFormat = "row receive: task %s may not land on this ledger yet: %s"

// SyncRowReceiveOptions selects ONE row to pull off a peer's ledger.
type SyncRowReceiveOptions struct {
	// TaskID is the row to pull (required).
	TaskID string
	// Peer is the source alias (required; validated against the declared
	// topology).
	Peer string
	// RemoteDB overrides the peer-side ledger path ("" = harvestRemoteDBPath).
	// It is the ISOLATION channel of this leg, on the harvest precedent:
	// a probe reads a sandbox ledger instead of the peer's own.
	RemoteDB string
	// DBPath overrides the LOCAL ledger ("" = default).
	DBPath string
	// Handover, when set, is the TRANSFER's first half: the peer is asked to
	// sign the row over (pending → cancelled) BEFORE a byte of its content is
	// read. A refusal (somebody else moved the row) stops the leg here — no
	// fetch, no import, local ledger untouched. nil = the ordinary pull, which
	// writes nothing on the peer.
	Handover func(taskID string) error
	// Message, when set, is the TRANSFER's post-import half: it claims the row
	// that landed here. It carries the epoch bound itself (the CLI closes over
	// the stamp the peer's OFFER carried), so this layer stays the transport
	// and the ORDER — handover, read, import, message — and nothing more.
	// nil = the ordinary pull, which claims nothing.
	Message func(taskID string) (string, error)
	// Fetch replaces the ssh+SQL transport (tests inject a row).
	Fetch func(peer, taskID string) ([]SyncTaskRow, error)
}

// SyncRowReceive pulls ONE row off a peer and applies it through
// SyncTaskImportDelta — the same merge authority the session, harvest and
// reconcile legs use, so "newer wins", "never regress a terminal row" and
// "never overwrite content" hold here for free rather than being re-stated.
// It returns the import's counts and the row's state in THIS ledger after the
// merge.
//
// NO ANCHOR IS TOUCHED. A single-row pull is a targeted read, not a window
// advance: moving the harvest anchor would silently skip every row between it
// and this one (the anchor's premise is "everything before it is identical on
// both sides" — one row proves nothing about that).
func SyncRowReceive(opts SyncRowReceiveOptions) (*SyncTaskImportStats, string, error) {
	taskID := taskIDNormalize(opts.TaskID)
	if taskID == "" {
		return nil, "", fmt.Errorf("row receive: a task id is required (a row leg carries no delta window)")
	}
	peer := strings.TrimSpace(opts.Peer)
	if peer == "" {
		return nil, "", fmt.Errorf("row receive: --peer is required (the ssh alias to pull from)")
	}
	// THE DECLARED-TOPOLOGY GATE, ASKED BEFORE ANY CONTACT — the pull half's
	// missing half of the send leg's precondition (memory_sync_row.go's
	// SyncRowSendEx asks it per peer, four lines above its own first contact).
	// The rule is the family's, in one line: sync topology is DECLARED, never
	// discovered — `task diff`, `task harvest`, `task sync` and `task send` all
	// refuse an alias that is not in settings.json "sync_peers" /
	// GOPI_SYNC_PEERS, and a one-off read of an undeclared alias is not a
	// supported shape for the same reason it is not for a session: the alias is
	// how a peer was AGREED to be addressed, so a pull of something else is not
	// a narrower operation, it is a different one. Asked BEFORE the fetch
	// closure runs, so a refusal here provably reached no peer.
	if err := RequireDeclaredPeer(peer, nil); err != nil {
		return nil, "", fmt.Errorf("row receive: %w", err)
	}
	fetch := opts.Fetch
	if fetch == nil {
		fetch = func(p, id string) ([]SyncTaskRow, error) {
			return syncRowFetchRemote(p, id, opts.RemoteDB)
		}
	}
	// THE HANDOVER RUNS FIRST, AFTER THE TOPOLOGY GATE AND BEFORE ANY READ: the
	// row is SIGNED OVER on the peer, and only then is its content fetched, so
	// the reading half can never race a peer dispatcher that took the row in
	// between — the two-legs-one-row hazard, closed by ORDER rather than by a
	// lock. A refused signature (somebody else moved the row) stops here: no
	// fetch, no import, and the local ledger is untouched.
	if opts.Handover != nil {
		if herr := opts.Handover(taskID); herr != nil {
			return nil, "", herr
		}
	}
	rows, err := fetch(peer, taskID)
	if err != nil {
		return nil, "", err
	}
	// THE ARRIVAL DEPENDENCY GATE — the pull half's second precondition, and
	// the one the import's own refusals cannot state as a sentence about THIS
	// ledger (Rule 15 + Rule 33). It asks the ONE exported criterion
	// (TaskTransferArrivalEligible, task_transfer_eligibility.go) whether the
	// row's edge can be honoured HERE, BEFORE the merge runs.
	//
	// WHY IT IS NOT OPTIONAL WORK DUPLICATING THE IMPORT. The import already
	// refuses an unresolvable edge — by ROLLING THE ROW BACK. A rolled-back
	// import is not a landed row: the counters on the way out describe work that
	// did not happen, and re-running the same pull reads the PEER's copy again,
	// which carries no backfill, so asking again cannot close the gap. Refusing
	// HERE says it instead, names the parent, and changes nothing.
	//
	// THE DECLARED-TOPOLOGY GATE ABOVE STAYS FIRST ON PURPOSE: an undeclared
	// alias is refused before any contact, and a criterion that read the ledger
	// ahead of it would put a ledger read in front of a transport refusal.
	if len(rows) > 0 {
		if ok, why := TaskTransferArrivalEligible(rows[0], NewTaskLedgerDependencyView(opts.DBPath)); !ok {
			return nil, "", fmt.Errorf(syncRowArrivalGateFormat, taskID, why)
		}
	}
	node := peer
	patch := &SyncTaskPatch{Version: syncPatchVersionDelta, Node: node, ExportedAt: time.Now().Unix(), Rows: rows}
	stats, err := SyncTaskImportDelta(patch)
	if err != nil {
		return nil, "", err
	}
	// THE MESSAGE (the transfer's post-import half) runs LAST, on the row that
	// did land. Its FAILURE is recorded in the stats, never returned as an
	// import failure: the row is here either way, and the caller's own next
	// step (the row's state in this ledger) already says what it is (Rule 15:
	// recorded, not swallowed — and not misreported).
	if opts.Message != nil {
		msg, merr := opts.Message(taskID)
		if merr != nil {
			msg = fmt.Sprintf("transfer: post-import step failed: %v", merr)
		}
		if msg != "" {
			stats.Errors = append(stats.Errors, msg)
		}
	}
	// THE STATE IS READ AFTER THE MESSAGE, and the order is load-bearing
	// (measured 2026-10-07 on the cross-host probe): the message IS the
	// transfer's admission, so it is what moves the row from the handed-over
	// `cancelled` to `in_progress`. Reading the status before it reported
	// `cancelled` for a row this very call had just claimed — and the worker's
	// trigger compares that value to decide whether it has a row to run, so a
	// taken-over row would have been announced and then silently dropped.
	status := ""
	if row, rerr := taskStatusDirect(taskID); rerr == nil && row != nil {
		status = row.Status
	}
	return stats, status, nil
}

// --- the pull leg's per-row OUTCOME ----------------------------------------

// syncRowPullStoredFormat / Migrated / Refused / Already are the ONE spelling
// each of a pull's per-row verdict. They are deliberately the SAME vocabulary
// the send leg's receipt reports in (`syncRowEffect*`, memory_sync_row_cancel.go)
// because they answer the same question about the same row from the other side
// of the wire: "what did THIS ledger do with it". A pull that answered a bare
// count — or a bare "OK" — would leave the operator to infer whether the row
// arrived, which is the accepted-and-uninformative defect class this family
// already records twice (Rule 15).
const (
	syncRowPullStoredFormat   = "pull %s: stored (row %s, status=%s)"
	syncRowPullMigratedFormat = "pull %s: migrated (row %s, status=%s)"
	syncRowPullAlreadyFormat  = "pull %s: already current (row %s, status=%s)"
	syncRowPullRefusedFormat  = "pull %s: REFUSED (row %s): %s"
)

// SyncRowPullLine renders ONE row's pull outcome from the import's own stats.
//
// IT PROJECTS THE RECEIPT AND READS ITS ONE-WORD VERDICT rather than
// re-interpreting the counters at the call site (Rule 14: one reading of the
// receipt, shared by the printer and every future caller). That projection is
// what gives the all-zero case its honest word: an import that ran and moved
// nothing counted the row under a counter of its own ladder (tag union,
// divergence, terminal-tier tie), and `already current` is what the send leg
// already answers for exactly that shape.
func SyncRowPullLine(peer, taskID string, stats *SyncTaskImportStats, status string) string {
	r := syncRowReceiptFromStats(stats, status)
	switch r.Effect() {
	case syncRowEffectRefused:
		return fmt.Sprintf(syncRowPullRefusedFormat, peer, taskID, r.Reason)
	case syncRowEffectStored:
		return fmt.Sprintf(syncRowPullStoredFormat, peer, taskID, status)
	case syncRowEffectMigrated:
		return fmt.Sprintf(syncRowPullMigratedFormat, peer, taskID, status)
	default:
		return fmt.Sprintf(syncRowPullAlreadyFormat, peer, taskID, status)
	}
}

// syncRowFetchRemote SSHes to the peer and runs the one-row SELECT, returning
// the row projected into the delta wire shape. It is the harvest leg's shape
// (harvestRemoteTasks) with the anchor predicate replaced by an id predicate —
// same exec discipline (no shell on the initiator side, BatchMode forbids an
// interactive prompt), same quoting helper, same "nothing on stdout = no row".
func syncRowFetchRemote(peer, taskID, remoteDB string) ([]SyncTaskRow, error) {
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("row receive: %w", err)
	}
	if remoteDB == "" {
		remoteDB = harvestRemoteDBPath
	}
	ctx, cancel := context.WithTimeout(context.Background(), harvestSSHTimeout)
	defer cancel()
	query := fmt.Sprintf("%s -json %s %s", syncRemoteSQLiteBinary, remoteDB,
		harvestShellQuote(fmt.Sprintf(syncRowRemoteSelect, harvestShellQuote(taskID))))
	out, err := exec.CommandContext(ctx, "ssh",
		"-o", fmt.Sprintf("ConnectTimeout=%d", harvestSSHConnectTimeout),
		"-o", "BatchMode=yes", peer, query).Output()
	if err != nil {
		return nil, fmt.Errorf("row receive: ssh %s: %w", peer, err)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		// 0 rows: sqlite3 -json prints nothing for an empty result set. The
		// absence is REPORTED (the caller prints "not found"), never padded
		// into an empty patch that would silently import nothing.
		return nil, fmt.Errorf("row receive: task %s not found in the peer %s ledger (%s)", taskID, peer, remoteDB)
	}
	var remote []harvestRemoteRow
	if err := json.Unmarshal([]byte(trimmed), &remote); err != nil {
		return nil, fmt.Errorf("row receive: peer %s returned unparsable JSON: %w", peer, err)
	}
	rows := make([]SyncTaskRow, 0, len(remote))
	for _, r := range remote {
		rows = append(rows, syncRowFromHarvest(r))
	}
	return rows, nil
}

// syncRowFromHarvest projects the peer-side SELECT's row into the delta wire
// row. It is a FIELD MAPPING and nothing more: every judgement about what to
// do with the row belongs to SyncTaskImportDelta, which is where the caller
// hands it. Tags arrive as the ledger's comma string and use the same
// splitter the exporter uses (splitSources), so a projection can never
// disagree with the exporter about what a tag list is.
func syncRowFromHarvest(r harvestRemoteRow) SyncTaskRow {
	return SyncTaskRow{
		TaskID:      r.TaskID,
		Summary:     r.Summary,
		Payload:     r.Payload,
		Tags:        splitSources(r.Tags),
		Source:      r.Source,
		CreatedAt:   int64(r.CreatedAt),
		Status:      r.Status,
		UpdatedAt:   int64(r.UpdatedAt),
		EndedAt:     int64(r.EndedAt),
		Deliverable: r.Deliverable,
		RunMethod:   r.RunMethod,
		RunCmd:      r.RunCmd,
		Reason:      r.Reason,
		// The row's dependency EDGE, so `task receive` brings back a row that
		// still waits for its parent instead of one that claims to be free
		// work (Rule 33).
		ParentTaskID: r.ParentTaskID, ParentTaskIDs: r.ParentTaskIDs,
	}
}
