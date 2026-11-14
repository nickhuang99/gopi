package tools

// task_transfer.go — the two peer-side writes of a cross-ledger row transfer
// (epic task-pull, deliverable ①) plus the initiator's one-round leg.
//
// THE SHAPE, and why it is these writes and nothing else:
//
//	peer side   `--next-eligible`  → SELECT one transferable row, read-only
//	peer side   `--handover <id>`  → the two-way predicate, then CAS
//	                                 pending → cancelled (the signature)
//	initiator   `task receive --takeover` → offer, sign, existing row leg,
//	                                 then claim the row that landed
//
// NOTHING NEW IS INVENTED FOR THE MOVEMENT ITSELF. The handover is a local
// status write on the peer (a CAS in the shape the claim path already uses);
// the content crosses on the row leg that already exists (memory_sync_row.go:
// one ssh hop, one `sqlite3 -json` SELECT, SyncTaskImportDelta as the one merge
// authority); and the receiving ledger claims the row through the claim path
// that already exists (TaskClaimExplicit + TaskStart). There is no lock, no
// claim broadcast, no rollback and no second task id — the row keeps its id and
// simply continues on the other ledger.
//
// THE TWO-WAY PREDICATE IS ASKED AT THE WRITE POINT, not only when picking.
// A named handover (`--handover T`) passes no selector, so the predicate runs
// HERE, on the ledger that owns the row and its children — which is also where
// it is cheapest to be right: the answer is read from the same database the
// CAS writes.
//
// WHY THE HANDOVER IS A CANCELLATION AND NOT A DELETE. A delete would leave no
// trace that the row ever existed here, and a MOVED row is provenance an
// operator must be able to read (`task show` answers "where did it go?" from
// the reason line). Cancelled is also the only terminal status legal from
// pending (taskAllowedTransitions) and the family's existing word for "this
// row's work is carried elsewhere" (the send leg's --cancel-source).
//
// THE ADMISSION IS THREE CONDITIONS, all read off THIS ledger after the import:
// the row is `cancelled`, its stamp is at least the OFFER's stamp, and its
// reason carries the handover's own prefix. Together they say "this is the copy
// the handover just produced" without a lock and without comparing two hosts'
// clocks (the offer's stamp and the row's stamp are both the PEER's).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The peer-side FLAG NAMES of this leg — declared here, in the tools layer, and
// REFERENCED by the CLI (Rules 1b/13: the flag the initiator puts on the wire
// and the flag the peer's parser reads are one value, so "--next-eligible"
// cannot be spelled two ways). They belong to the `task sync` family, beside
// SyncRowRemoteFlagName, because both are the PEER side of a cross-ledger leg
// the initiator opened.
const (
	// SyncTransferNextFlagName is `--next-eligible [ID]`: the READ-ONLY
	// selector of ONE transferable row (one JSON document on stdout, "no
	// claimable row" included). With an id it answers about THAT row only —
	// the same predicate, a narrower question.
	SyncTransferNextFlagName = "next-eligible"
	// SyncTransferHandoverFlagName is `--handover <id>`: the handover write —
	// the two-way predicate, then CAS pending → cancelled. The id travels on
	// the command line, so the remote argv stays ONE element and no protocol
	// has to be piped through the peer's login shell.
	SyncTransferHandoverFlagName = "handover"
	// SyncTransferSkipTagsFlagName rides both legs above: the RECEIVING
	// dispatcher's tag exclusion. A row that dispatcher would refuse must never
	// be handed over, or the move strands it — cancelled at the source,
	// unclaimable at the destination (the trap this argument closes).
	SyncTransferSkipTagsFlagName = "skip-tag"
)

// --- the peer side: reading one transferable row ---------------------------

// SyncTransferEligibleRow is the projection the peer's selector answers with —
// the fields the initiator needs to (a) carry the row on and (b) judge the copy
// that landed here afterwards. It is a FIELD MAP of the row, never a judgement:
// which row to offer is TaskTransferEligible's business.
type SyncTransferEligibleRow struct {
	TaskID       string  `json:"task_id"`
	Summary      string  `json:"summary"`
	Tags         string  `json:"tags"`
	Status       string  `json:"status"`
	Source       string  `json:"source"`
	CreatedAt    float64 `json:"created_at"`
	UpdatedAt    float64 `json:"updated_at"`
	ParentTaskID string  `json:"parent_task_id"`
	// ParentTaskIDs carries parents 2..N (EPIC multi-parent). It rides this
	// answer document so a row MOVED between nodes arrives with the whole set:
	// the receiver writes it through taskParentsForWrite, so parent 1 still
	// lands in parent_task_id and the row keeps its dependency on a node whose
	// build is older than this field.
	ParentTaskIDs string `json:"parent_task_ids,omitempty"`
}

// SyncTransferEligibleDoc is the peer's answer document, in the row leg's own
// dialect: ONE line of JSON on stdout, under every outcome. `eligible:false`
// with a reason is an ANSWER, not an error — "no claimable row" is a normal
// state of a queue (the row leg's `Empty`), and an initiator must be able to
// tell it apart from a peer it could not reach at all (Rule 15).
type SyncTransferEligibleDoc struct {
	Node     string                   `json:"node"`
	Eligible bool                     `json:"eligible"`
	Row      *SyncTransferEligibleRow `json:"row,omitempty"`
	// Reason explains an ineligible outcome in the peer's own words.
	Reason string `json:"reason,omitempty"`
	// Scanned is how many candidates the pre-filter offered — the number that
	// separates "an empty queue" from "a queue whose rows all refuse".
	Scanned int `json:"scanned"`
}

// syncTransferEligibleColumns is the projection both selector queries share
// (Rule 1b/13: one column list, two WHERE clauses) and
// syncTransferEligibleFrom its single FROM — `payload` rides along because
// taskRowClaimable judges a row partly by it, so it must be selectable without
// a second query.
const (
	syncTransferEligibleColumns = `task_id, summary, COALESCE(tags,''), status,
		COALESCE(source,''), created_at, updated_at, COALESCE(payload,''), COALESCE(parent_task_id,''),
		COALESCE(` + taskParentIDsColumnName + `,'')`
	syncTransferEligibleFrom = ` FROM tasks WHERE `
)

// syncTransferCandidateScan reads a candidate page through the shared
// projection. It is the ONE place the row shape is scanned (Rule 4/14), so a
// column added to the projection is added for both queries at once.
func syncTransferCandidateScan(rows *sql.Rows) (*Task, error) {
	var t Task
	if err := rows.Scan(&t.TaskID, &t.Summary, &t.Tags, &t.Status, &t.Source,
		&t.CreatedAt, &t.UpdatedAt, &t.Payload, &t.ParentTaskID, &t.ParentTaskIDs); err != nil {
		return nil, fmt.Errorf("transfer: scan row: %w", err)
	}
	return &t, nil
}

// taskTransferRowByID reads ONE row through the candidate projection, or nil
// when this ledger does not hold it. `taskID` MUST be a row id this ledger
// really holds (a full id — see the note at TaskTransferSelectOne's named
// branch): the match is exact, so a short spelling reads as absence.
func taskTransferRowByID(ctx context.Context, db *sql.DB, taskID string) (*Task, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+syncTransferEligibleColumns+syncTransferEligibleFrom+`task_id = ?`, taskID)
	t, err := syncTransferCandidateScanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// taskTransferRowFullID resolves a TYPED task id to the row through the SAME
// face the rest of the family uses — syncRowResolveTaskID, the send/move leg's
// ONE id→row step (Rule 14: no second resolver, no second judgement about what
// an operand means).
//
// WHY THIS FILE NEEDS IT AT ALL. Both peer-side faces of a cross-ledger
// transfer take a task id from the COMMAND LINE (`--next-eligible <ID>`,
// `--handover <ID>`), and both handed it to the exact-match read above — the
// door the MOVE leg's dependency gate used to have, with the same two answers
// for one operand: a row the ledger really holds, typed short, reads as "is not
// in this ledger" here while the ledger's own id resolves it perfectly well
// everywhere else. Full id → the row; uuid-shaped prefix → refused by name
// with its candidates (internal/fullid's one sentence); anything else, or no
// such row → nil, and the caller keeps the "not in this ledger" sentence it
// already had.
//
// A refusal is returned as an error, never folded into the nil row, because the
// two facts are different: "this ledger does not hold it" is a verdict about the
// row, "that spelling names several rows" is a verdict about the OPERAND.
func taskTransferRowFullID(ctx context.Context, db *sql.DB, taskID string) (*Task, error) {
	id, err := syncRowResolveTaskID(ctx, db, taskID)
	if err != nil {
		return nil, err
	}
	if id == nil {
		return nil, nil
	}
	return taskByID(ctx, db, id.TaskID)
}

// syncTransferCandidateScanRow is the single-row face of the shared projection.
func syncTransferCandidateScanRow(row *sql.Row) (*Task, error) {
	var t Task
	if err := row.Scan(&t.TaskID, &t.Summary, &t.Tags, &t.Status, &t.Source,
		&t.CreatedAt, &t.UpdatedAt, &t.Payload, &t.ParentTaskID, &t.ParentTaskIDs); err != nil {
		return nil, err
	}
	return &t, nil
}

// TaskTransferSelectOne picks ONE transferable row in the peer's OWN claim order
// (taskClaimOrderBy — the same clause `task next` and the pending view's [next]
// marker read, Rules 13/14) and answers a leaf document: the row, or the reason
// nothing executable can travel right now. `only` narrows the question to one
// named row (the named-handover half).
//
// "NO CLAIMABLE ROW" IS THE HONEST ANSWER, and it is deliberately not dressed
// up as "wait and re-run": a ledger whose pending rows all refuse by their own
// fields is a ledger whose queue is empty FOR A DISPATCHER, and the initiator's
// correct move is the next peer, not a sleep.
func TaskTransferSelectOne(ctx context.Context, db *sql.DB, skipSet map[string]bool, only string) (*SyncTransferEligibleDoc, error) {
	doc := &SyncTransferEligibleDoc{Node: claimBroadcastNodeName()}
	if id := strings.TrimSpace(only); id != "" {
		// THE NAMED ROW IS RESOLVED, NOT ASSUMED FULL. `--next-eligible <ID>` and
		// `--handover <ID>` take a task id typed on a command line, and the
		// family's rule about a typed id is the project's, not this file's: a
		// full id is looked up, a uuid-shaped PREFIX is refused with the
		// candidates it names (internal/fullid), and only "no such row" keeps
		// this face's own sentence. Reading the operand with an exact match
		// instead made a short spelling report "is not in this ledger" about a
		// row the ledger holds — the same silent-skip the send leg's dependency
		// gate had (defect 01a117082d5473c7a8ed247ec357795d).
		t, err := taskTransferRowFullID(ctx, db, id)
		if err != nil {
			return nil, err
		}
		doc.Scanned = 1
		if t == nil {
			doc.Reason = fmt.Sprintf("row %s is not in this ledger", id)
			return doc, nil
		}
		return taskTransferAnswer(ctx, db, doc, t, skipSet)
	}

	// The pre-filter narrows the walk to rows that can POSSIBLY travel — the
	// QUEUED status and the parentless edge — and leaves every judgement to
	// TaskTransferEligible (Rule 14: one predicate, two callers; these clauses
	// are an index, never a verdict).
	rows, err := db.QueryContext(ctx,
		`SELECT `+syncTransferEligibleColumns+syncTransferEligibleFrom+`status = ? AND `+taskTransferParentlessSQL+taskClaimOrderBy,
		taskStatusPending)
	if err != nil {
		return nil, fmt.Errorf("transfer: select: %w", err)
	}
	// The walk is fully read BEFORE any judgement: the ledger openers pin a
	// SINGLE connection, so a query issued while this cursor is open would block
	// on it (see the deadlock note in TaskNextFiltered) — and the reverse half
	// of the predicate is itself a query.
	var cands []*Task
	for rows.Next() {
		t, serr := syncTransferCandidateScan(rows)
		if serr != nil {
			//stylegate:ignore the cursor ends here and the scan error is returned to the caller on the next line
			rows.Close()
			return nil, serr
		}
		cands = append(cands, t)
	}
	if err := rows.Err(); err != nil {
		//stylegate:ignore the cursor is finished and the reported error is the rows.Err() checked on the line above
		rows.Close()
		return nil, fmt.Errorf("transfer: select: %w", err)
	}
	//stylegate:ignore read-only cursor of a single-connection ledger, closed before the child scans; the close error has no reporter
	rows.Close()

	doc.Scanned = len(cands)
	refusal := ""
	for _, t := range cands {
		dep, derr := TaskTransferLiveChild(ctx, db, t.TaskID)
		if derr != nil {
			return nil, derr
		}
		if ok, why := TaskTransferEligible(t, dep, skipSet); !ok {
			// A refusal is NOT "wait and re-run" work (no amount of waiting
			// removes a live child), so the loop walks on to the next
			// candidate — but the FIRST refusal is remembered: it is the row an
			// operator would ask about.
			if refusal == "" {
				refusal = fmt.Sprintf("row %s: %s", t.TaskID, why)
			}
			continue
		}
		return taskTransferAnswer(ctx, db, doc, t, skipSet)
	}
	doc.Reason = taskTransferNoRowReason(refusal)
	return doc, nil
}

// taskTransferAnswer renders ONE row's verdict into the document — the shared
// tail of both selector shapes.
func taskTransferAnswer(ctx context.Context, db *sql.DB, doc *SyncTransferEligibleDoc, t *Task, skipSet map[string]bool) (*SyncTransferEligibleDoc, error) {
	dep, err := TaskTransferLiveChild(ctx, db, t.TaskID)
	if err != nil {
		return nil, err
	}
	ok, why := TaskTransferEligible(t, dep, skipSet)
	if !ok {
		doc.Reason = fmt.Sprintf("row %s: %s", t.TaskID, why)
		return doc, nil
	}
	doc.Eligible = true
	doc.Row = &SyncTransferEligibleRow{
		TaskID: t.TaskID, Summary: t.Summary, Tags: t.Tags, Status: t.Status,
		Source: t.Source, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		ParentTaskID: t.ParentTaskID, ParentTaskIDs: t.ParentTaskIDs,
	}
	return doc, nil
}

// taskTransferNoRowReason renders the peer's "nothing transferable" line: the
// empty-queue sentence when there was nothing to look at, else the first
// candidate's own refusal (which is what makes the answer actionable — a queue
// of parked rows and a queue blocked on a live child are two different facts,
// and only the reason distinguishes them).
func taskTransferNoRowReason(refusal string) string {
	if refusal == "" {
		return TaskTransferNoRow
	}
	return TaskTransferNoRow + " (" + refusal + ")"
}

// TaskTransferNoRow is the ONE spelling of the empty answer — the literal token
// an initiator (or a test) greps for across implementations, so "the peer had
// nothing" cannot be spelled two ways in two callers.
const TaskTransferNoRow = "no claimable row"

// SyncTransferEligible answers the peer-side selector: resolve the peer's own
// ledger, run the predicate, marshal the document. `skipTags` is the comma list
// the initiator sent ("" = no exclusions).
func SyncTransferEligible(dbPath, only, skipTags string) ([]byte, error) {
	var doc *SyncTransferEligibleDoc
	err := syncRowWithLedger(dbPath, func() error {
		db, closeDB, oerr := taskSyncDB()
		if oerr != nil {
			return oerr
		}
		defer closeDB()
		var serr error
		doc, serr = TaskTransferSelectOne(context.Background(), db, taskTransferSkipSet(skipTags), only)
		return serr
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// taskTransferSkipSet parses the wire spelling of a skip set (the CLI's own
// comma list, unchanged in transit). An empty string is the empty set, never a
// set containing "".
func taskTransferSkipSet(csv string) map[string]bool {
	set := map[string]bool{}
	for _, tg := range strings.Split(csv, ",") {
		if tg = strings.TrimSpace(tg); tg != "" {
			set[tg] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// --- the peer side: the handover ------------------------------------------

// TaskTransferCASRefusal is a handover that lost its CAS: the row was NOT
// pending when the conditional UPDATE ran. It is its own type so a caller can
// distinguish "somebody else moved the row" (the ordinary race — report it by
// name and walk to the next row) from a transport or ledger failure. The state
// the CAS actually found is carried, never a bare "refused".
type SyncTransferCASRefusal struct {
	TaskID string
	Status string
}

// Error names the row, the state it is really in, and what that means — with
// the ONE sentence the shared conditional write uses (task_cancel_pending.go's
// taskCancelPendingMissFormat), so the pull leg and the push leg word a lost CAS
// identically (Rule 15b).
func (e *SyncTransferCASRefusal) Error() string {
	status := e.Status
	if status == "" {
		status = taskCancelPendingAbsent
	}
	return fmt.Sprintf("transfer: "+taskCancelPendingMissFormat, e.TaskID, status, taskStatusPending)
}

// TaskTransferIneligible is a handover the two-way predicate refused — the row
// itself cannot travel, so no signature was written and the peer is untouched.
type SyncTransferIneligible struct {
	TaskID string
	Why    string
}

func (e *SyncTransferIneligible) Error() string {
	return fmt.Sprintf("transfer: task %s cannot be handed over: %s", e.TaskID, e.Why)
}

// taskTransferHandoverReasonPrefix is the ONE spelling of the reason a
// handed-over row carries (Rule 1b). It travels WITH the row, so the receiving
// ledger can recognise the copy this handover produced (see TaskTransferAdmit)
// — and it names the peer, the same-id continuation and the undo, because the
// row's own trail is the only place an operator standing on the SOURCE ledger
// can learn where its work went.
const taskTransferHandoverReasonPrefix = "handed over to peer "

// taskTransferHandoverReason renders the trail line: peer, the same-id
// continuation, and the undo that is actually possible (a cancelled row
// releases nothing, so its children — if it ever gets them — stay parked until
// it is reissued).
func taskTransferHandoverReason(peer, taskID string) string {
	return fmt.Sprintf("%s%s by `gopi task sync --%s` (the SAME task id %s continues on that ledger, claimed there); cancelled HERE so this ledger does not dispatch it a second time — undo with `gopi task update %s --status %s --reason WHY`",
		taskTransferHandoverReasonPrefix, peer, SyncTransferHandoverFlagName, taskID, taskID, taskStatusPending)
}

// TaskTransferHandover performs the handover signature: the two-way predicate
// on the row's OWN ledger, then pending → cancelled as a CAS, in ONE ledger.
//
// WHY A CAS AND NOT A STATUS WRITE. A status write re-reads the row and refuses
// an illegal TRANSITION, which is not the same guarantee: two handovers (or a
// handover racing a claim) can both read `pending`, both pass the transition
// gate, and then two ledgers each believe they own the row while only one can
// run it. The conditional UPDATE makes exactly one caller win, and the loser is
// told WHICH status beat it so the initiator walks on instead of guessing.
//
// The ledger is taken through syncRowWithLedger, so `--db PATH` means the same
// ledger here as on every other half of the row leg.
func TaskTransferHandover(taskID, peer, dbPath, skipTags string) error {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return fmt.Errorf("transfer: a task id is required (--%s)", SyncTransferHandoverFlagName)
	}
	ctx := context.Background()
	var refusal error
	err := syncRowWithLedger(dbPath, func() error {
		db, dbErr := openTasksDB(ctx, memoryDBPath())
		if dbErr != nil {
			return dbErr
		}
		defer db.Close()

		// ① THE TWO-WAY PREDICATE runs HERE, at the write point, on the ledger
		// that owns the row and its children. A named handover never passed the
		// selector, so this is the only place the reverse edge is seen. The id is
		// resolved through the family's ONE id→row face (a typed short id is
		// refused by name here too, not read as "no such row" — see
		// taskTransferRowFullID), and the row it yields is the ledger's OWN
		// spelling, which is what the CAS and the trail line below must use.
		row, rerr := taskTransferRowFullID(ctx, db, taskID)
		if rerr != nil {
			return rerr
		}
		if row == nil {
			refusal = &SyncTransferIneligible{TaskID: taskID, Why: taskTransferMissingStatus}
			return nil
		}
		taskID = row.TaskID
		// The STATUS arm is asked HERE in the race's own words, before the
		// general predicate: "it is claimed, not pending — another writer moved
		// it" is what an initiator needs to hear (it walks on), while the
		// predicate's version of the same fact is the SELECTOR's phrasing. The
		// predicate still owns every OTHER arm below (Rule 14: one predicate,
		// two callers) — this is one arm stated at the point that can say it
		// more precisely.
		if row.Status != taskStatusPending {
			refusal = &SyncTransferCASRefusal{TaskID: taskID, Status: row.Status}
			return nil
		}
		dep, derr := TaskTransferLiveChild(ctx, db, taskID)
		if derr != nil {
			return derr
		}
		if ok, why := TaskTransferEligible(row, dep, taskTransferSkipSet(skipTags)); !ok {
			refusal = &SyncTransferIneligible{TaskID: taskID, Why: why}
			return nil
		}
		// ①b THE RETIREMENT ARM — asked at the WIRE, before the signature, and
		// asked only for a row whose branch the discipline of the merge-backlog
		// family names (mergeBacklogGuardBranch: deliverable.branch first, the
		// summary's own branch otherwise — the SAME resolver the dispatch guard
		// and the terminal guard read, Rule 14). A retired branch must NEVER
		// travel: the receiving ledger would claim it and spend a session on a
		// premise git has already refused, which is exactly the measured defect
		// (see archived_branch_retirement.go for the run and the row). The
		// adjudication is the ONE predicate's answer, read from the durable
		// record on the SENDING side, where the decision was made.
		if rec, retired := RowBranchRetirement(mergeBacklogGuardRepo(row), row.Summary, row.Tags, row.Deliverable); retired {
			refusal = &SyncTransferIneligible{TaskID: taskID, Why: ArchivedBranchExclusionText(rec)}
			return nil
		}

		// ② THE SIGNATURE — the ONE pending→cancelled conditional write of the
		// whole MOVE family (task_cancel_pending.go): the send leg's source
		// cancellation calls the same function, so "of two writers exactly one
		// wins, and the loser is told which state beat it" is stated in ONE
		// place. `reason` is overwritten on purpose: the row carries its own
		// provenance out of here.
		out, cerr := taskCancelPendingCAS(ctx, db, taskID, taskTransferHandoverReason(peer, taskID))
		if cerr != nil {
			return cerr
		}
		if out.Cancelled {
			return nil
		}
		refusal = &SyncTransferCASRefusal{TaskID: taskID, Status: out.Observed}
		return nil
	})
	if err != nil {
		return err
	}
	return refusal
}

// SyncTransferHandoverDoc is the handover's answer document — the same one-line
// JSON discipline as the selector, and the same rule that a REFUSAL is an
// answer rather than a transport error.
type SyncTransferHandoverDoc struct {
	TaskID string `json:"task_id"`
	Signed bool   `json:"signed"`
	// Observed is what the ledger actually showed when the signature did not
	// land: the status that beat the CAS, or the predicate's own reason. The
	// initiator walks on from a named refusal, never from a guess.
	Observed string `json:"observed,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// SyncTransferHandoverFlag answers the peer-side handover: sign the row over
// (or refuse it by name), always as a document on stdout.
func SyncTransferHandoverFlag(value, dbPath, skipTags, peer string) ([]byte, error) {
	id := taskIDNormalize(value)
	if id == "" {
		return nil, fmt.Errorf("transfer: --%s requires a task id (the row to hand over)", SyncTransferHandoverFlagName)
	}
	err := TaskTransferHandover(id, peer, dbPath, skipTags)
	doc := &SyncTransferHandoverDoc{TaskID: id}
	if err == nil {
		doc.Signed = true
		return json.Marshal(doc)
	}
	var cas *SyncTransferCASRefusal
	if errors.As(err, &cas) {
		doc.Observed = cas.Status
		doc.Reason = cas.Error()
		return json.Marshal(doc)
	}
	var inel *SyncTransferIneligible
	if errors.As(err, &inel) {
		doc.Observed = inel.Why
		doc.Reason = inel.Error()
		return json.Marshal(doc)
	}
	return nil, err
}

// SyncTransferPeerUnknown is the peer named in the reason when the handover
// runs as a bare peer-side handler: the PEER cannot know which alias its caller
// reached it by (the alias lives in the INITIATOR's ssh config), and inventing
// one would be a provenance claim nothing measured. It is a sentence, not a
// placeholder, so an operator reading the row's trail is told exactly that.
const SyncTransferPeerUnknown = "(this peer cannot see the alias you reached it by)"

// --- the initiator: the post-import admission -----------------------------

// taskTransferMissingStatus is the word a handover refusal uses when the row is
// not in this ledger at all (Rule 1b: one spelling, so the sentence never
// renders an empty status as if it were a state).
const taskTransferMissingStatus = "absent from this ledger"

// taskTransferOfferGap is the slack the admission allows between the OFFER's
// stamp and the LANDED row's stamp: both are the PEER's `updated_at` for the
// same row, so the second is the first plus whatever wall-clock seconds the
// exchange consumed. The guard's job is to reject a cancelled row that predates
// the offer, so it only has to be non-negative — the slack is there because a
// peer whose clock steps BACK must not turn a good handover into a refusal.
const taskTransferOfferGap = 0.0

// TaskTransferAdmit claims the row that the handover produced, and ONLY that
// row. It runs after the import, on THIS ledger, and it is the whole admission:
//
//	① status == cancelled        — a pending/cancelled import never happens: the
//	                               shipped row IS cancelled, so anything else
//	                               means the import did not apply it.
//	② reason carries the handover prefix — the signature the handover writes and
//	                               nothing else does.
//	③ updated_at >= the OFFER's stamp — both values are the PEER's, so this
//	                               comparison stays in ONE clock domain (a
//	                               two-host comparison would not).
//
// Failing ② or ③ means this ledger already held a cancellation of the same id
// from an earlier exchange: the import correctly refused to overwrite a
// TERMINAL row (the row leg's never-regress rule), and claiming it here would
// be stealing a row this transfer did not move. It is reported, not taken.
func TaskTransferAdmit(shipped SyncTaskRow, offeredAt float64) (string, error) {
	taskID := taskIDNormalize(shipped.TaskID)
	if taskID == "" {
		return "", fmt.Errorf("transfer: the landed row has no task id")
	}
	row, err := taskStatusDirect(taskID)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", fmt.Errorf("transfer: row %s is not in this ledger after the handover — the row leg reported an import it did not make", taskID)
	}
	if row.Status != taskStatusCancelled {
		return fmt.Sprintf("transfer: row %s landed as %s — the handover ships a %s row, so this is not the copy the handover produced; NOT claimed here",
			taskID, row.Status, taskStatusCancelled), nil
	}
	if !strings.HasPrefix(strings.TrimSpace(row.Reason), taskTransferHandoverReasonPrefix) {
		return fmt.Sprintf("transfer: row %s is %s here but was NOT cancelled by a handover (its reason is not the handover's) — this ledger's own copy; NOT claimed here",
			taskID, taskStatusCancelled), nil
	}
	if row.UpdatedAt+taskTransferOfferGap < float64(shipped.UpdatedAt) {
		return fmt.Sprintf("transfer: row %s is %s here but its cancellation (%0.f) PREDATES the shipped copy (%d) — a stale local row, not this handover's; NOT claimed here",
			taskID, taskStatusCancelled, row.UpdatedAt, shipped.UpdatedAt), nil
	}
	if err := taskClaimFromCancelled(taskID); err != nil {
		return "", err
	}
	// THE HANOVER LANDS `claimed`, NOT `in_progress` (epic 01a1a87b). This leg
	// exec's nothing — it adopts a row another host handed over — and since the
	// operator's ruling `in_progress` means exactly "a child process was exec'd
	// HERE and is being waited on". Writing it here would recreate the defect
	// the epic removes, on the one path where it is most tempting: the adopting
	// host would claim a process it does not have, and the question "is this row
	// live on THIS machine?" would again have a false answer.
	//
	// What the handover DOES establish is complete: the row is claimed here
	// (taskClaimFromCancelled's CAS) and the double lock is settled by the
	// exchange itself, which is what lock_phase=both records. The runner that
	// actually execs the row advances it — with its child's pid.
	if err := TaskStampLockPhase(taskID, TaskLockPhaseBoth); err != nil {
		return "", fmt.Errorf("transfer: stamp the handover's lock phase on %s: %w", taskID, err)
	}
	return fmt.Sprintf("transfer: %s claimed HERE (the handover's copy); status stays %s with lock_phase=%s — "+
		"the runner that execs it records %s", taskID, taskStatusClaimed, TaskLockPhaseBoth, taskStatusInProgress), nil
}

// taskClaimFromCancelled is the SECOND-STEP CAS of a handed-over row: the same
// conditional UPDATE TaskClaimExplicit performs, relaxed by exactly one
// expectation — the row was cancelled by the handover rather than being pending
// — and its WHERE carries the cancelled status, so it can only ever win on a
// row still in the state this transfer wrote. It lives HERE rather than in the
// claim file on purpose: the relaxation is the transfer's, and it must not be
// reachable by an ordinary claim.
func taskClaimFromCancelled(taskID string) error {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	holder := taskDefaultClaimHolder()
	now := float64(time.Now().Unix())
	identSet, identArgs := taskRunClaimIdentitySet(holder)
	args := []any{taskStatusClaimed, holder, now, now}
	args = append(args, identArgs...)
	args = append(args, taskID, taskStatusCancelled)
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, updated_at = ?, `+identSet+
			` WHERE task_id = ? AND status = ?`, args...)
	if err != nil {
		return fmt.Errorf("transfer: claim: %w", err)
	}
	//stylegate:ignore n!=1 is the meaningful branch and a driver error yields the same 0, so the CAS-miss error below is raised either way
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("transfer: claim of handed-over row %s lost its CAS — the row is no longer the %s this transfer read", taskID, taskStatusCancelled)
	}
	taskNotifyFlushStatusMove(taskID, taskStatusCancelled, taskStatusClaimed)
	return nil
}

// --- the initiator: the one-round leg -------------------------------------

// SyncTransferTakeOverOptions is ONE initiator round: ask a peer for a
// transferable row, sign it over, carry it here through the ordinary row leg,
// and claim what landed.
type SyncTransferTakeOverOptions struct {
	// Peer is the source alias (required; must be declared).
	Peer string
	// TaskID narrows the round to ONE named row. "" = the peer picks (its own
	// claim order, its own judgement).
	TaskID string
	// RemoteDB / RemoteHome / DBPath are the row leg's isolation channels,
	// passed through unchanged (Rule 34: a probe runs in a private profile).
	RemoteDB   string
	RemoteHome string
	DBPath     string
	// SkipTags is the RECEIVING dispatcher's exclusion list (the worker's own
	// set): a row it would refuse must never be handed over.
	SkipTags string
}

// SyncTransferTakeOverResult is what ONE round did — every outcome is a result,
// never a silent nothing.
type SyncTransferTakeOverResult struct {
	// Peer is the alias the round asked.
	Peer string
	// TaskID is the row it moved ("" when nothing did).
	TaskID string
	// Empty reports the peer's honest "no claimable row" — the caller's cue to
	// try the next peer, NOT an error.
	Empty bool
	// Reason explains an empty or refused round in the peer's own words.
	Reason string
	// Status is the row's state in THIS ledger after the round.
	Status string
	// Stats is the import's own verdict (the merge authority's counts).
	Stats *SyncTaskImportStats
	// Message is the post-import admission's report (claimed / not claimed,
	// with its reason).
	Message string
}

// SyncTransferTakeOver runs ONE round of the transfer, in the order the
// hazards require:
//
//  1. OFFER   — the peer names one transferable row, or says it has none
//  2. SIGN    — the peer CASes that row pending → cancelled (refused by name
//     when somebody else moved it in between: walk to the next row,
//     never retry blindly)
//  3. FETCH   — the EXISTING row leg reads the row and merges it here
//  4. ADMIT   — this ledger claims the copy the handover produced, and only it
//
// A failure at any step is returned as its own error, and NOTHING is undone:
// there is no rollback to write, because the only write on the source ledger
// (step 2) is the handover itself — a row that is cancelled there and never
// claimed here is an operator-visible parked row with its own trail line, not
// a silent loss (Rule 15: a failure names itself rather than hiding in a
// compensation path).
func SyncTransferTakeOver(opts SyncTransferTakeOverOptions) (*SyncTransferTakeOverResult, error) {
	peer, err := ValidateSSHPeerAlias(opts.Peer)
	if err != nil {
		return nil, fmt.Errorf("transfer: %w", err)
	}
	if err := RequireDeclaredPeer(peer, nil); err != nil {
		return nil, fmt.Errorf("transfer: %w", err)
	}
	res := &SyncTransferTakeOverResult{Peer: peer, TaskID: taskIDNormalize(opts.TaskID)}

	// THE SELF-PEER GATE, asked BEFORE the offer and therefore before every
	// contact of this round (peer_self_host.go). This round's SECOND step (SIGN)
	// is a PEER-SIDE WRITE — the row is CASed pending → cancelled on the peer's
	// ledger — so a self-peer alias would run that write in whatever profile the
	// local login shell resolves. Refusing here keeps the whole round a
	// provable no-op; the OFFER alone would have been harmless, but a round that
	// starts reading and dies at the sign is worse for the caller than one that
	// never started (Rule 15: the refusal names the leg and the way out).
	if err := selfPeerWriteRefusal(peer, opts.RemoteHome, memoryDBPathOr(opts.DBPath), "transfer"); err != nil {
		return nil, fmt.Errorf("transfer: %w", err)
	}

	offeredAt := float64(0)
	if res.TaskID == "" {
		// ① OFFER
		pick, perr := SyncTransferPickRow(peer, opts.SkipTags, opts.RemoteHome, opts.RemoteDB)
		if perr != nil {
			return nil, perr
		}
		if pick == nil || !pick.Eligible || pick.Row == nil {
			res.Empty = true
			if pick != nil {
				res.Reason = pick.Reason
			}
			if res.Reason == "" {
				res.Reason = TaskTransferNoRow
			}
			return res, nil
		}
		res.TaskID = taskIDNormalize(pick.Row.TaskID)
		offeredAt = pick.Row.UpdatedAt
	}
	if res.TaskID == "" {
		return nil, fmt.Errorf("transfer: peer %s offered a row with no task id", peer)
	}

	// ② SIGN — and ③ FETCH + ④ ADMIT, in the row leg's fixed order (handover,
	// read, import, message).
	stats, status, rerr := SyncRowReceive(SyncRowReceiveOptions{
		TaskID:   res.TaskID,
		Peer:     peer,
		RemoteDB: opts.RemoteDB,
		DBPath:   opts.DBPath,
		Handover: func(id string) error {
			doc, herr := syncTransferSignOver(peer, id, opts.SkipTags, opts.RemoteHome, opts.RemoteDB)
			if herr != nil {
				return herr
			}
			if !doc.Signed {
				return fmt.Errorf("transfer: peer %s refused to hand over %s: %s", peer, id, doc.Reason)
			}
			return nil
		},
		Message: func(id string) (string, error) {
			ships, ferr := syncTransferReadLocal(id)
			if ferr != nil {
				return "", ferr
			}
			return TaskTransferAdmit(ships, offeredAt)
		},
	})
	if rerr != nil {
		return nil, rerr
	}
	res.Stats, res.Status = stats, status
	if stats != nil && len(stats.Errors) > 0 {
		res.Message = strings.Join(stats.Errors, "; ")
	}
	return res, nil
}

// syncTransferReadLocal reads the row THIS ledger holds through the leg's own
// row shape, so the admission compares like with like (the stamps it judges are
// the ones the import wrote).
func syncTransferReadLocal(taskID string) (SyncTaskRow, error) {
	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{SkipReflowGate: true})
	if err != nil {
		return SyncTaskRow{}, err
	}
	for _, r := range patch.Rows {
		if r.TaskID == taskID {
			return r, nil
		}
	}
	return SyncTaskRow{}, fmt.Errorf("transfer: row %s is not in this ledger after the handover", taskID)
}

// syncTransferSignOver runs the peer-side handover over ssh and parses its
// document. Like every other peer face, the refusal is a DOCUMENT on stdout (a
// moved row, a predicate refusal) while a transport failure is an error — the
// two are never conflated, because the initiator's next move is different for
// each (walk on vs back off).
func syncTransferSignOver(peer, taskID, skipTags, remoteHome, remoteDB string) (*SyncTransferHandoverDoc, error) {
	// THE PRODUCTION PATH IS GONE, and this is where it used to be: the argv
	// was `gopi task sync --row-handover …`, and `task sync` is a DELETED verb
	// (2026-11-05) that answers with a removal notice + exit 1. The argv
	// RENDERER went with it (Rule 43: its only referrer was this leg), so
	// nothing can compose that spelling any more. The refusal happens before a
	// connection exists; remote_leg_retired.go states the mechanism once.
	_, _, _, _ = taskID, skipTags, remoteHome, remoteDB
	return nil, remoteLegRetired("transfer handover", peer)
}

// parseTransferHandoverDoc reads the handover's answer, refusing a peer that
// answered something else by naming the version fact rather than pretending the
// network broke (an older binary answers an unknown flag with usage text).
func parseTransferHandoverDoc(peer string, stdout []byte) (*SyncTransferHandoverDoc, error) {
	trimmed := strings.TrimSpace(string(stdout))
	if trimmed == "" {
		return nil, fmt.Errorf("transfer: peer %s answered nothing to `--%s` (no document on stdout)", peer, SyncTransferHandoverFlagName)
	}
	doc := &SyncTransferHandoverDoc{}
	if err := json.Unmarshal([]byte(trimmed), doc); err != nil {
		return nil, fmt.Errorf("transfer: peer %s did not answer the `--%s` protocol: %s — the peer's gopi may predate this leg (its stdout was not a document): %w",
			peer, SyncTransferHandoverFlagName, transferSnippet(trimmed), err)
	}
	return doc, nil
}

// --- the initiator: asking ONE peer for a row ------------------------------

// SyncTransferPick asks ONE peer to name a transferable row (the read-only
// selector leg) — the initiator's half of the worker's trigger: a worker with
// an EMPTY local queue asks each declared peer in turn, and the first peer with
// a row wins.
func SyncTransferPickRow(peer, skipTags, remoteHome, remoteDB string) (*SyncTransferEligibleDoc, error) {
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("transfer: %w", err)
	}
	if err := RequireDeclaredPeer(peer, nil); err != nil {
		return nil, fmt.Errorf("transfer: %w", err)
	}
	// THE PRODUCTION PATH IS GONE (see syncTransferSignOver above): the argv
	// would have been `gopi task sync --row-next …`, a DELETED verb, so its
	// renderer went with it (Rule 43). A peer that answers nothing is no
	// longer a protocol question, because nothing asks.
	_, _, _ = skipTags, remoteHome, remoteDB
	return nil, remoteLegRetired("transfer selector", peer)
}

// unusedTransferSelectorReply is the UNREACHABLE remainder of the retired
// selector leg (the parse of a peer's eligible-row document). It is left in
// place, not deleted, because the document TYPE it parses is shared with the
// LOCAL selector (TaskTransferSelectOne); only the ssh round-trip is gone.
func unusedTransferSelectorReply(peer string, stdout []byte) (*SyncTransferEligibleDoc, error) {
	trimmed := strings.TrimSpace(string(stdout))
	if trimmed == "" {
		return nil, fmt.Errorf("transfer: peer %s answered nothing to `--%s` (no document on stdout) — an empty answer is not `%s`",
			peer, SyncTransferNextFlagName, TaskTransferNoRow)
	}
	doc := &SyncTransferEligibleDoc{}
	if err := json.Unmarshal([]byte(trimmed), doc); err != nil {
		return nil, fmt.Errorf("transfer: peer %s did not answer the `--%s` protocol: %s — the peer's gopi may predate this leg (its stdout was not a document): %w",
			peer, SyncTransferNextFlagName, transferSnippet(trimmed), err)
	}
	return doc, nil
}

// transferSnippetMax bounds a peer's stray stdout in a refusal line (Rule 1: a
// named cap, so a peer that answers with a kilobyte of usage text cannot bury
// the sentence that explains what happened).
const transferSnippetMax = 200

func transferSnippet(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= transferSnippetMax {
		return s
	}
	return s[:transferSnippetMax] + "…"
}

// syncTransferSkipSuffix is the flag tail both peer-side faces carry: the
// receiving dispatcher's exclusion list and the peer-side ledger override
// (`--remote-db`, the harvest flag the row leg already reuses).
func syncTransferSkipSuffix(skipTags, remoteDB string) string {
	tail := ""
	if tags := strings.TrimSpace(skipTags); tags != "" {
		tail += " " + syncFlagPrefixRow + SyncTransferSkipTagsFlagName + "=" + harvestShellQuote(tags)
	}
	if db := strings.TrimSpace(remoteDB); db != "" {
		tail += " " + syncFlagPrefixRow + syncRemoteDBFlagForPeer + " " + harvestShellQuote(db)
	}
	return tail
}

// syncRemoteDBFlagForPeer is the PEER-side spelling of the ledger override: on
// the wire the initiator's `--remote-db PATH` ("the ledger over there") becomes
// the peer's OWN `--db PATH` ("my ledger"), because a peer-side face resolves
// ITS ledger, not somebody else's. Named once because the two builders below
// and the peer's own parser must agree (Rules 1b/13).
const syncRemoteDBFlagForPeer = "db"

// --- the peer side: the handler -------------------------------------------

// SyncTransferPeerFaceOptions is ONE peer-side invocation of the transfer leg:
// resolve THIS ledger (the peer's own), answer a document on stdout.
//
// ONE FLAG HERE MEANS SOMETHING DIFFERENT, and it is written down because it
// would otherwise read as a mistake: CallerAlias (`--peer <alias>`) is NOT a
// dial target on these faces — a peer-side handler dials nobody. It is the
// alias the CALLER reached this peer by, carried so the row's own trail can
// name where its work went ("handed over to peer peerbox…"). The peer cannot
// derive it (the alias lives in the INITIATOR's ssh config), so when it is
// absent the reason says exactly that instead of inventing a name.
type SyncTransferPeerFaceOptions struct {
	// SelectorBare is `--next-eligible` with NO value: offer one row.
	SelectorBare bool
	// SelectorID is `--next-eligible <ID>`: the same question about one row.
	SelectorID string
	// HandoverID is `--handover <ID>`: sign that row over.
	HandoverID string
	// SkipTags is the receiving dispatcher's exclusion list (comma list).
	SkipTags string
	// DBPath overrides THIS ledger ("" = this process's own profile ledger).
	DBPath string
	// CallerAlias names the caller for the row's provenance line ("" = the
	// peer cannot see it, and says so).
	CallerAlias string
}

// SyncTransferPeerFace dispatches the peer-side selector or handover and
// answers the document to print. Exactly one face per invocation is the
// caller's contract (the parser admits either, and the handover wins if both
// were somehow given, because it is the one that WRITES — an ambiguous
// invocation must never silently become the harmless one).
func SyncTransferPeerFace(opts SyncTransferPeerFaceOptions) ([]byte, error) {
	if strings.TrimSpace(opts.HandoverID) != "" {
		peer := strings.TrimSpace(opts.CallerAlias)
		if peer == "" {
			peer = SyncTransferPeerUnknown
		}
		return SyncTransferHandoverFlag(opts.HandoverID, opts.DBPath, opts.SkipTags, peer)
	}
	return SyncTransferEligible(opts.DBPath, opts.SelectorID, opts.SkipTags)
}
