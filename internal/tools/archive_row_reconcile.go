// archive_row_reconcile.go — the LEDGER half of `gopi merge`'s unrelated-history
// verdict. The durable retirement (archive tag + reason record, archive_reason.go)
// writes the DECISION into git; this file makes that decision VISIBLE FROM THE
// ROW that asked for the merge, so a reviewer is never dispatched onto a branch
// the retirement already took out of every merge lane.
//
// THE MEASURED DEFECT (Rule 41, task 01a1383b, 2026-10-14). The retirement wrote
// a tag and a reason blob and NO ledger state. Rows 01a13322 (in_progress) and
// 01a135d2 (pending) both named feat/unrun-demo; the branch had been retired at
// 2026-10-13T10:49:59 and the FILER half was already fixed (archiveTagAncestry /
// archiveTipReachable, task_merge_backlog.go) — so no NEW row was filed — while
// the rows already in the ledger stayed live and kept being claimed. The row's
// disposition was only derivable by re-running git by hand; nothing on the row
// said it.
//
// WHAT THIS FILE ADDS, AND WHAT IT DELIBERATELY DOES NOT:
//
//   - It writes NO tag and creates NO row: the decision is read back from the
//     record the retirement just wrote (the reason record's blob), and only the
//     rows that decision voids are reconciled.
//   - It closes a row ONLY when the row's own summary carries the branch through
//     the ONE merge-backlog family parser (mergeBacklogFamilyBranch — the same
//     membership rule the dispatch guard and the terminal guard read, Rule 14).
//     A row that merely MENTIONS the branch in prose is not decidable from the
//     row, so it is left alone: cancelling unrelated work on a text resemblance
//     is the false positive Rule 15's default-deny direction forbids.
//   - A row whose recorded holder is LIVE is left open and REPORTED. A live
//     holder's disposition belongs to the reclaim / dead-holder lane (which
//     measures the runner's incarnation); this reconciler never fights one.

package tools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Reconcile tunables (Rule 1: every literal in one place).
const (
	// archiveRetirementRowEvent is the append-only trail entry recorded on each
	// reconciled row — the same side-channel discipline the sibling guards use
	// (mergeBacklogGhostEvent), so the decision rides the ROW and not only the
	// process log a later reader never sees.
	archiveRetirementRowEvent = "archive-retirement-reconciled"

	// archiveRowCloseReasonFmt is the ONE spelling of the reason written onto a
	// reconciled row. It names the decision (the tag), its recorded why (the
	// reason code, glossed by the vocabulary's own doc) and the undo, because an
	// operator reading a `cancelled` row must be able to see that a DECISION
	// closed it rather than an abandoned attempt. It carries no timestamp: two
	// passes over the same state must write the same sentence (the write is
	// idempotent, and a row's reason is read as a fact about the branch).
	archiveRowCloseReasonFmt = "unrelated-history retirement — branch %s is a decided exclusion in git " +
		"(tag %s, reason %s: %s). No merge lane can land it (git merges such a branch only with " +
		"--allow-unrelated-histories), so this row's premise is void. Closed as %s: %s. " +
		"The branch ref is PRESERVED — delete the tag to re-arm the branch."

	// archiveRowHeldReasonFmt is what a row left open carries instead: the
	// holder this host can still see, and who owns that disposition.
	archiveRowHeldReasonFmt = "unrelated-history retirement — branch %s is a decided exclusion in git (tag %s), " +
		"but this row is left OPEN: %s. A live holder's disposition belongs to the reclaim / dead-holder lane, " +
		"which measures the recorded runner's incarnation; this reconciliation does not fight one."

	// archiveRowClaimHolder is the claimed_by stamp the reconciler writes when it
	// advances a PENDING row to a terminal (the pending → claimed → cancelled
	// walk every closer in this package uses). It names the reconciler, so the
	// claim trail shows no worker ran the row.
	archiveRowClaimHolder = "archive-retirement-reconciler"
)

// ArchiveRowReconciliation is what ONE ledger row's reconciliation did (or
// deliberately did not do). From/To are the statuses the write moved it
// between (To is "" when the row was left open) and Held names the live holder
// in that case — a reporter never has to re-derive which arm fired.
type ArchiveRowReconciliation struct {
	TaskID string
	From   string
	To     string
	Held   string
}

// ArchiveRetirementReconcile is the WHOLE ledger half of one unrelated-history
// retirement: which branch and decision it read, and the two arms of rows —
// Closed (the premise was void and nobody held it) and Held (left open, with
// the holder named).
type ArchiveRetirementReconcile struct {
	Branch string
	Tag    string
	Reason string
	Closed []ArchiveRowReconciliation
	Held   []ArchiveRowReconciliation
	// LedgerPath is the ledger this reconciliation ASKED for rows, carried out
	// with the answer: "reconciled 0 rows" is a claim about ONE database, and
	// the reader has to be able to tell which one answered (Rule 15) — the
	// sibling lookup carries its ledger out the same way (TaskOwnerAxes).
	LedgerPath string
	// LedgerAbsent records that there was NO ledger at LedgerPath, which is the
	// reason the row arms are empty. It is a field rather than something the
	// caller re-derives, because the two readings of an empty row set — "the
	// ledger held no row naming this branch" and "there was no ledger" — are
	// DIFFERENT facts, and a report that collapsed them would claim a
	// reconciliation that never had a subject (Rule 15).
	LedgerAbsent bool
}

// ReconcileUnrelatedHistoryRetirement is the ONE entry point of the ledger half:
// it writes the durable retirement (idempotent — repoSweepArchiveBranch) and
// then reconciles the ledger rows that decision voids.
//
// ORDER IS LOAD-BEARING. The reconciliation runs AFTER the writer, because it
// reads the decision back from the record the writer leaves: a reconciler that
// ran first, or that trusted the caller's word, would cancel rows on a decision
// that was REFUSED (the owner gate refusing a finished deliverable's branch is
// the case the whole gate exists for). So a refusal is an error out of here and
// NOTHING is reconciled.
//
// It is a NEW name rather than a change to RetireUnrelatedHistoryBranch, which
// stays the pure record-writer its own callers and tests pin.
func ReconcileUnrelatedHistoryRetirement(repo, branch string) (ArchiveRetirementReconcile, error) {
	res := ArchiveRetirementReconcile{Branch: strings.TrimSpace(branch),
		Tag: ArchiveBranchPrefix + strings.TrimSpace(branch)}
	if res.Branch == "" {
		return res, fmt.Errorf("archive retirement reconcile: branch must not be empty")
	}
	if err := RetireUnrelatedHistoryBranch(repo, res.Branch); err != nil {
		return res, err
	}
	code, err := archiveReasonRecordedFor(repo, res.Branch)
	if err != nil {
		return res, err
	}
	res.Reason = code
	rows, ledgerPath, ledgerAbsent, err := archiveFamilyRowsForBranch(res.Branch)
	if err != nil {
		return res, err
	}
	res.LedgerPath = ledgerPath
	res.LedgerAbsent = ledgerAbsent
	for _, row := range rows {
		if rowHasLiveHolder(row) {
			held := archiveRowHolderClause(row)
			archiveReconcileEvent(row.TaskID, fmt.Sprintf(archiveRowHeldReasonFmt, res.Branch, res.Tag, held))
			res.Held = append(res.Held, ArchiveRowReconciliation{TaskID: row.TaskID, From: row.Status, Held: held})
			continue
		}
		closed, err := archiveReconcileOneRow(row, res)
		if err != nil {
			// Rule 15: a row that could not be written is REPORTED, never
			// dropped — the caller decides whether the verdict's report is
			// still truthful. The rows after this one are still attempted, so
			// one failure cannot hide the rest of the residue.
			res.Held = append(res.Held, ArchiveRowReconciliation{TaskID: row.TaskID, From: row.Status,
				Held: "the terminal write failed: " + err.Error()})
			continue
		}
		res.Closed = append(res.Closed, closed)
	}
	return res, nil
}

// archiveReasonRecordedFor reads the DECISION back from the reason record the
// retirement writes (refs/gopi/archive-reason/<branch>'s blob) and returns the
// reason CODE it carries.
//
// A tag with no record, or a record whose code is not in the closed vocabulary,
// is NOT a decision this reconciler may act on (Rule 15: no credit for a typo),
// so it is an error rather than an empty string a caller might read as "act".
// The record is read through the branch's own ref, never through the tag's
// message: git's folded ref-metadata view is not the body the writers own.
func archiveReasonRecordedFor(repo, branch string) (string, error) {
	out, err := repoSweepGit(repo, "cat-file", "blob", archiveReasonRefPrefix+branch)
	if err != nil {
		return "", fmt.Errorf("archive retirement reconcile: no decision record for %s (ref %s): %w",
			branch, archiveReasonRefPrefix+branch, err)
	}
	code := ArchiveTagReason(out)
	if code == "" {
		return "", fmt.Errorf("archive retirement reconcile: %s carries no reason from the closed vocabulary (record %s) — "+
			"nothing is reconciled on an unrecorded decision", branch, archiveReasonRefPrefix+branch)
	}
	return code, nil
}

// archiveFamilyRowsForBranch loads every LIVE row whose summary names branch
// through the merge-backlog family parser — the row that ASKED for the merge.
//
// The SQL narrows to the live statuses only (a terminal row is already decided
// and is never rewritten: the ledger is append-only), and the family judgement
// is the Go predicate's, not a LIKE: the summary is the durable carrier of the
// branch (the cross-node delta union replaces tags wholesale), so the parser
// that the dispatch guard and the terminal guard already read is the ONE
// membership rule here too (Rule 14).
//
// It returns the ledger it asked (and whether there was one) beside the rows:
// "0 rows" is only interpretable together with WHICH database answered, and an
// ABSENT ledger is the empty answer rather than a refusal — the same pure-read
// guard taskOwnerAxesAll obeys (ledger_read_guard.go). Building a knowledge
// base to answer "no rows" is the write that guard exists to forbid, and
// refusing instead would make `gopi merge` report a retirement it DID perform
// as "NOT retired" on any host whose ledger is not built yet — the measured
// defect this arm carries (cmd/agent's TestMergeUnrelatedHistory… at :156).
func archiveFamilyRowsForBranch(branch string) (rows []*Task, ledgerPath string, ledgerAbsent bool, err error) {
	ctx := context.Background()
	path := memoryDBPath()
	// AN ABSENT LEDGER IS THE EMPTY ANSWER, NEVER A REFUSAL (Rule 15) — the
	// doctrine this package's ONE read guard states, applied where it decides
	// something. Measured 2026-10-14 (task 01a139ec): without this arm the
	// caller's error travelled out of ReconcileUnrelatedHistoryRetirement and
	// `gopi merge` rendered the retirement it had ALREADY written as
	// "left as it is, NOT retired: … no ledger at …" — a false negative about
	// the durable half, over the state the merge pass's own contract calls the
	// ordinary no-owner answer. A machine with no ledger has no rows to
	// reconcile, and the ORDER property is untouched: the WRITER still ran
	// first, and a refusal from it (the owner gate) still travels out as an
	// error.
	if ledgerMissingForRead(path) {
		return nil, path, true, nil
	}
	db, err := archiveRetirementLedger(ctx)
	if err != nil {
		return nil, path, false, err
	}
	defer db.Close()
	res, err := db.QueryContext(ctx,
		`SELECT `+taskSelectColumns+` FROM tasks WHERE status IN (`+taskLiveStatusSQLList()+`)`)
	if err != nil {
		return nil, path, false, fmt.Errorf("archive retirement reconcile: read live rows: %w", err)
	}
	defer res.Close()
	for res.Next() {
		row, err := scanTask(res)
		if err != nil {
			return nil, path, false, fmt.Errorf("archive retirement reconcile: scan row: %w", err)
		}
		// THE MEMBERSHIP TEST IS THE CANONICAL SPEC'S, not a string equality:
		// the retirement is asked about the branch the DECIDING pass named
		// ("task/<id>" from the merge verdict) while a row may spell the same
		// branch as its ground ("<id>"), and a verbatim comparison would leave
		// exactly that row live — the measured shape (task 01a1446e's bug row:
		// the row that survived a retirement and was handed over cross-host).
		// ONE canonicalization is read here, the same one the exclusion
		// predicate applies (Rule 14).
		if named, family := mergeBacklogFamilyBranch(row.Summary, row.Tags); family &&
			CanonicalBranchRetirementSpec(named) == CanonicalBranchRetirementSpec(branch) {
			rows = append(rows, row)
		}
	}
	return rows, path, false, res.Err()
}

// rowHasLiveHolder reports whether a live row is still HELD — "" is not a
// possible answer, and the three sources are asked WEAKEST LAST, so the
// strongest evidence on the row decides:
//
//  1. the row's OWN record of its runner (proc_pid + start ticks + cmdline),
//     compared against the live process table by the package's ONE ladder
//     (finishCleanupProcessHolderLive → procname.RunnerIdentity) — a recycled
//     pid is caught here and nowhere else;
//  2. the claim stamp, when a local pid is resolvable and alive;
//  3. a stamp this host cannot measure (another host, or an unparseable one) —
//     held, because the write this gates is TERMINAL and Rule 15's default-deny
//     direction forbids resting one on an unmeasured factor.
//
// An ABSENT stamp is the one case that answers "nobody here holds it": 31 of the
// 36 live rows on this host are pending with no stamp at all, and treating that
// as a hold would make the reconciliation unreachable for exactly the rows it
// was written for. (The worktree veto's classifier, finishCleanupOwnerRowLive,
// answers the OPPOSITE question — "any reason to keep this ground?" — and
// therefore reads an absent stamp as a hold; borrowing it here was measured as a
// defect in this file's own first test run, which reconciled 0 of 2 rows.)
func rowHasLiveHolder(row *Task) bool {
	if row == nil {
		return true
	}
	if row.HasProcIdentity && finishCleanupProcessHolderLive(procIdentity{
		Pid: row.ProcPid, StartTicks: row.ProcStartTicks, Cmd: row.ProcCmd}) {
		return true
	}
	if strings.TrimSpace(row.ClaimedBy) == "" {
		return false
	}
	_, pid, known := claimHolder(row.ClaimedBy)
	if !known {
		return true // another host, or a stamp this build cannot parse — unmeasured
	}
	return pidAlive(pid)
}

// archiveRowHolderClause describes what this host can see holding (or not
// holding) the row — the ONE spelling of the holder state, read by both arms
// (the held arm's reason and the close arm's clause). A factor this host could
// not measure is not rendered as "nobody": a foreign-host stamp, a live local
// pid and an absent stamp are three DIFFERENT measurements, and Rule 15 says a
// sentence must not collapse them.
func archiveRowHolderClause(row *Task) string {
	if row == nil {
		return "the row could not be read"
	}
	if rowHasLiveHolder(row) {
		if host := claimHostOf(row.ClaimedBy); host != "" && host != hostnameOrEmpty() {
			return "it is claimed on " + host + " — this host cannot measure that holder"
		}
		if row.ClaimedBy != "" {
			return "its claim stamp " + row.ClaimedBy + " names a process this host measured alive"
		}
		return "its recorded runner is still alive"
	}
	if strings.TrimSpace(row.ClaimedBy) != "" {
		return "its claim stamp " + row.ClaimedBy + " names a process this host measured as gone"
	}
	return "no claim holder was recorded and none was measured"
}

// archiveReconcileOneRow writes ONE row's reconciliation: the legal walk from
// its live status to the terminal, through the SAME exported transitions every
// other closer uses (claim, then the shared mutation core), so the claim trail
// records who closed it and the state machine cannot be bypassed.
//
// Two steps for a pending row (pending → claimed → cancelled) and one for a row
// already claimed or in_progress. The REASON column is the whole operator-facing
// content of the close (Rule 15: a close whose reason does not say WHY is a
// silent close), and the judgement also rides the row's append-only trail.
func archiveReconcileOneRow(row *Task, res ArchiveRetirementReconcile) (ArchiveRowReconciliation, error) {
	reason := fmt.Sprintf(archiveRowCloseReasonFmt, res.Branch, res.Tag, res.Reason,
		ArchiveReasonDoc(res.Reason), taskStatusCancelled, archiveRowHolderClause(row))
	from := row.Status
	if from == taskStatusPending {
		if err := TaskClaimExplicit(row.TaskID, archiveRowClaimHolder); err != nil {
			return ArchiveRowReconciliation{}, fmt.Errorf("claim %s: %w", row.TaskID, err)
		}
	}
	if _, err := taskUpdate(row.TaskID, taskStatusCancelled, "", reason); err != nil {
		return ArchiveRowReconciliation{}, fmt.Errorf("cancel %s: %w", row.TaskID, err)
	}
	taskEventWrite(context.Background(), retiredBranchReconcileDB(), row.TaskID, archiveRetirementRowEvent, reason)
	return ArchiveRowReconciliation{TaskID: row.TaskID, From: from, To: taskStatusCancelled}, nil
}

// archiveReconcileEvent appends the judgement to ONE row's append-only trail —
// the same channel the sibling guards use, so a reader of `task show` sees the
// decision without the process log (best-effort by design: Rule 15b, an
// annotation failure is logged and never overturns a close the ledger accepted).
func archiveReconcileEvent(taskID, detail string) {
	taskEventWrite(context.Background(), retiredBranchReconcileDB(), taskID, archiveRetirementRowEvent, detail)
}

// retiredBranchReconcileDB opens the process's ledger for the trail write — a
// separate opener from the read above because the write happens per row while
// the read's handle is already closed, and the single-connection ledger pins one
// writer at a time. An unavailable ledger yields the nil handle the event writer
// reports to the log (Rule 15b), never a silent drop — an ABSENT ledger is the
// same skip and NOT a refusal: there is no ledger to hold the trail, so there is
// nothing to write (the same reading archiveFamilyRowsForBranch takes, and the
// reason the guard is asked here rather than left to the opener, whose job is to
// CREATE the file).
func retiredBranchReconcileDB() QueryExecer {
	ctx := context.Background()
	if path := memoryDBPath(); ledgerMissingForRead(path) {
		taskNotifyLog("archive retirement reconcile: trail write skipped, no ledger at %s", path)
		return nil
	}
	db, err := archiveRetirementLedger(ctx)
	if err != nil {
		taskNotifyLog("archive retirement reconcile: trail write skipped, ledger unavailable: %v", err)
		return nil
	}
	return db
}

// archiveRetirementLedger opens the process's ledger for the reconciliation. It
// is the OPENER, not the guard: the "is there a ledger to answer from at all?"
// question belongs to the caller through the package's ONE read guard
// (ledgerMissingForRead, ledger_read_guard.go), because the two callers read an
// absent ledger differently and one of them must not be reached with no file —
// the row read answers the EMPTY SET (a machine with no ledger has no rows to
// reconcile), while the trail write SKIPS with a log line (Rule 15b).
//
// An ABSENT ledger is therefore never a refusal, and this function no longer
// answers one: it reports only what it can still report — a ledger that exists
// and cannot be opened. That distinction is the whole difference between "I
// could not look" (Rule 15) and "there is nothing to look at", and the refusal
// belongs to the first alone.
func archiveRetirementLedger(ctx context.Context) (*sql.DB, error) {
	path := memoryDBPath()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("archive retirement reconcile: open ledger %s: %w", path, err)
	}
	return db, nil
}
