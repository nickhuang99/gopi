package tools

// Legacy holder-identity BACKFILL (epic dead-holder-resume, 2026-09-16).
//
// THE GAP. `task run` has stamped the process-identity triple since d7474fb4
// (2026-09-13), so every run started after that commit is judgeable: its record
// can be compared against /proc and answered live/dead. A row from BEFORE it
// (or one whose capture failed at stamp time) names its holder only as TEXT —
// `claimed_by = "host:pid"` — and carries proc_pid = NULL. Such a row is not
// judgeable by the triple; the older textual layers decide it, and the merger's
// resume arm is right to treat an unprovable row as unreadable rather than
// guess (Rule 15).
//
// THE FIX IS A PROOF, NOT A GUESS. This backfill fills the triple ONLY where
// /proc PROVES the claim stamp's pid is the same live process the stamp meant:
//
//   - the claim stamp must parse as a pid AND be marked as THIS host's
//     (claimHostIsLocal) — a foreign node's "otherhost:1234" says nothing about
//     any process here, and stamping this host's pid 1234 onto it would be a
//     fabricated identity (the exact Rule-15 failure: a false identity can make
//     a LIVE holder read as dead, and a resume would then double-run it);
//   - /proc/<pid>/comm must begin with the gopi runner prefix
//     (taskAliveProcessNamePrefix) — the SAME pid-reuse guard the rest of the
//     ledger trusts, so a recycled pid belonging to an unrelated process is
//     never adopted as the holder;
//   - the row must be in a status that can still carry a live holder
//     (taskLiveHolderStatus) — a terminal row's holder is historical, and
//     writing a triple onto it would invent a live-looking holder for work that
//     is over.
//
// Everything else is LEFT, deliberately: the counts this returns are the
// honest report (gained vs left-with-reason), because a backfill that silently
// filled unprovable rows would poison exactly the judgement it exists to make
// possible.
//
// WHY NOT A NEW CLI VERB (Rule 3 / Rule 11). A one-shot data repair has no LLM
// in its path and cannot be adopted by one: Rule 11 requires a new tool/verb to
// demonstrate real LLM invocation, so a verb here would be a surface with no
// user. The function is the deliverable; it is dispatchable from a test (see
// TestBackfillLegacyHolderIdentityProvesBeforeWriting) and was RUN against the
// live ledger, with the counts recorded in the task report.

import (
	"context"
	"fmt"
	"strconv"
)

// backfillReason names WHY a row was left without a triple, so the operator
// report says which of the guards refused rather than a bare total.
type backfillReason string

const (
	// backfillGained: the triple was proven from /proc and written.
	backfillGained backfillReason = "gained"
	// backfillLeftForeign: the claim stamp is not this host's.
	backfillLeftForeign backfillReason = "left: claim stamp is not this host's (a foreign pid is unknowable here)"
	// backfillLeftUnparsable: the claim stamp carries no pid at all.
	backfillLeftUnparsable backfillReason = "left: claim stamp carries no pid"
	// backfillLeftNotRunner: /proc/<pid> is absent, or its comm is not a gopi
	// runner (a recycled pid belongs to something else).
	backfillLeftNotRunner backfillReason = "left: /proc does not prove a live gopi runner on that pid"
	// backfillLeftTerminal: the row's status is over; there is no live holder
	// to describe.
	backfillLeftTerminal backfillReason = "left: row status is terminal (no live holder to describe)"
	// backfillLeftAlreadyStamped: the row already carries a triple.
	backfillLeftAlreadyStamped backfillReason = "left: row already carries a recorded identity"
)

// BackfillCounts is the honest tally of one backfill run.
type BackfillCounts struct {
	Gained int
	Left   map[backfillReason]int
}

// Line renders the tally for the operator report, in a stable order.
func (c BackfillCounts) Line() string {
	out := fmt.Sprintf("holder-identity backfill: gained=%d", c.Gained)
	for _, r := range []backfillReason{
		backfillLeftAlreadyStamped, backfillLeftForeign, backfillLeftUnparsable,
		backfillLeftNotRunner, backfillLeftTerminal,
	} {
		if n := c.Left[r]; n > 0 {
			out += fmt.Sprintf("; %s=%d", r, n)
		}
	}
	return out
}

// BackfillLegacyHolderIdentity fills the identity triple for live rows whose
// holder is named only textually, where /proc PROVES the stamp. Dry-run
// (write=false) reports what WOULD change without touching the ledger — the
// form to use on a foreign/operator ledger.
func BackfillLegacyHolderIdentity(write bool) (BackfillCounts, error) {
	counts := BackfillCounts{Left: map[backfillReason]int{}}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return counts, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, COALESCE(claimed_by,'') FROM tasks
		 WHERE COALESCE(claimed_by,'') <> ''
		   AND (COALESCE(`+tasksProcPidColumnName+`,0) = 0 OR COALESCE(`+tasksProcStartColumnName+`,0) = 0)`)
	if err != nil {
		return counts, err
	}
	type cand struct{ id, status, claimedBy string }
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.status, &c.claimedBy); err != nil {
			//stylegate:ignore the scan error IS the error being returned; a cursor that just failed to scan has no state left to report
			rows.Close()
			return counts, err
		}
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		//stylegate:ignore rows.Err() already reports the iteration failure; Close() on a failed cursor adds nothing and would mask it
		rows.Close()
		return counts, err
	}
	//stylegate:ignore read-only cursor drained at end of iteration; the error carries no state to recover, and the connection is needed by the per-row writes below
	rows.Close()
	for _, c := range cands {
		reason := backfillClassify(c.status, c.claimedBy)
		if reason != backfillGained {
			counts.Left[reason]++
			continue
		}
		pid := claimPidOf(c.claimedBy)
		if write {
			ident, ok := taskCaptureProcIdentity(pid)
			if !ok {
				counts.Left[backfillLeftNotRunner]++
				continue
			}
			if _, werr := db.ExecContext(ctx,
				`UPDATE tasks SET `+tasksProcPidColumnName+` = ?, `+tasksProcStartColumnName+` = ?, `+tasksProcCmdColumnName+` = ? WHERE task_id = ?`,
				ident.Pid, ident.Start, ident.Cmd, c.id); werr != nil {
				return counts, werr
			}
		}
		counts.Gained++
	}
	return counts, nil
}

// backfillClassify returns backfillGained when the row's textual stamp is
// PROVEN to name a live gopi runner on this host, else the reason it is left.
func backfillClassify(status, claimedBy string) backfillReason {
	if !taskLiveHolderStatus(status) {
		return backfillLeftTerminal
	}
	if claimPidOf(claimedBy) == 0 {
		return backfillLeftUnparsable
	}
	if !claimHostIsLocal(claimedBy) {
		return backfillLeftForeign
	}
	// The pid is this host's and parses; the last proof is that the process it
	// names is still the RUNNER (never a recycled pid owned by something else).
	if !taskPidIsRunner(claimPidOf(claimedBy)) {
		return backfillLeftNotRunner
	}
	return backfillGained
}

// backfillHolderStamped reports whether a row already carries a usable triple —
// the predicate the ledger query above expresses in SQL, kept here as the
// readable spelling for tests and reports.
func backfillHolderStamped(pid int, startTicks int64) bool {
	return pid > 0 && startTicks > 0
}

// backfillClaimPidDisplay is the report's rendering of a claim stamp's pid
// (0 renders as "-", never as a pid nobody proved).
func backfillClaimPidDisplay(claimedBy string) string {
	if pid := claimPidOf(claimedBy); pid > 0 {
		return strconv.Itoa(pid)
	}
	return "-"
}
