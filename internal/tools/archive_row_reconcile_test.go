// archive_row_reconcile_test.go — the ledger half of the unrelated-history
// retirement, driven through the REAL surface (Rule 31): the test calls
// ReconcileUnrelatedHistoryRetirement, which is what `gopi merge`'s verdict
// calls, against a real temp repo AND a real ledger, and then asks the LEDGER
// the operator's question ("is this row still going to be dispatched?") rather
// than inspecting the writer's return value alone.
//
// The rows are seeded through the package's existing row fixture (newRow,
// repo_resolve_row_test.go), which writes the exact column set the schema
// defines; the branch membership they exercise is decided by the production
// parser (mergeBacklogFamilyBranch), never restated here — a fixture that
// spelled its own classifier would agree with a broken production rule.

package tools

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// recSeedAuditRow files the row `gopi merge` files: the merge-backlog AUDIT
// summary ("merge <branch> into master") for branch, live, carrying the
// merge-backlog tag — the exact shape the dispatch guard and the terminal guard
// classify.
func recSeedAuditRow(t *testing.T, branch, status string) string {
	t.Helper()
	return newRow(t, "rec", mergeBacklogSummaryVerbFilled(branch), status,
		mergeBacklogTag, "", "merger", repoDeliv("task/ground"), "")
}

// recSeedScanRow files the SCAN-face shape ("merge-backlog: <branch> unmerged —
// merge/archive") as a PENDING row — the second shape the family parser must
// recognise, and the one the measured defect left claimable.
func recSeedScanRow(t *testing.T, branch string) string {
	t.Helper()
	return newRow(t, "recp", mergeBacklogTag+": "+branch+" unmerged — merge/archive",
		taskStatusPending, mergeBacklogTag, "", "merger", "", "")
}

// recSeedUnrelatedRow is the FALSE-POSITIVE probe: a live row that MENTIONS the
// branch in its SUMMARY prose but is not a member of the merge-backlog family.
// It must be left alone: this reconciler acts on the row's own classification,
// never on a text resemblance.
//
// Its deliverable deliberately names its OWN ground (task/<id>), not the retired
// branch — and that is the measured shape of the defect this file fixes, not a
// convenience: the retirement's owner gate reads deliverable.branch, so a row
// that named the branch there would make the writer REFUSE outright (a live
// owner). The measured rows (01a13322 / 01a135d2) both name feat/unrun-demo only
// in their summary while their deliverable names task/<id> — `gopi task
// for-branch feat/unrun-demo` answered "no ledger row names branch" over 2164
// branch-carrying rows — which is exactly why the owner gate never fired for
// them and why the reconciliation has to classify on the summary.
func recSeedUnrelatedRow(t *testing.T, branch string) string {
	t.Helper()
	return newRow(t, "recu", "a different task that happens to discuss "+branch,
		taskStatusInProgress, "", "payload", "agent", repoDeliv("task/ground"), "")
}

// recSeedHeldRow files a member row whose CLAIM STAMP names a live local
// process (this test binary itself), i.e. a holder the host can measure. It
// must stay OPEN: the retirement does not fight a live holder.
func recSeedHeldRow(t *testing.T, branch string) string {
	t.Helper()
	id := recSeedAuditRow(t, branch, taskStatusInProgress)
	recRowWrite(t, `UPDATE tasks SET claimed_by = ? WHERE task_id = ?`,
		claimHolderOfPid(os.Getpid()), id)
	return id
}

// mergeBacklogSummaryVerbFilled renders the audit verb from its ONE source
// (mergeBacklogSummaryVerb) rather than re-typing the sentence.
func mergeBacklogSummaryVerbFilled(branch string) string {
	return strings.Replace(mergeBacklogSummaryVerb, "%s", branch, 1)
}

// recRowWrite is the fixture's one raw write door, so a column added later has
// exactly one place in this file to update.
func recRowWrite(t *testing.T, statement string, args ...any) {
	t.Helper()
	db := recOpenLedger(t)
	defer db.Close()
	if _, err := db.Exec(statement, args...); err != nil {
		t.Fatalf("fixture write %q: %v", statement, err)
	}
}

func recOpenLedger(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openTasksDB(t.Context(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	return db
}

// recRowState reads back the columns an operator decides on, from the DB the
// process actually wrote.
func recRowState(t *testing.T, id string) (status, reason string) {
	t.Helper()
	db := recOpenLedger(t)
	defer db.Close()
	if err := db.QueryRow(`SELECT status, reason FROM tasks WHERE task_id = ?`, id).
		Scan(&status, &reason); err != nil {
		t.Fatalf("read row %s: %v", id, err)
	}
	return status, reason
}

// recEvents returns the row's append-only trail entries under the reconciler's
// own event name — the side channel a later reader of `gopi task show` sees.
func recEvents(t *testing.T, id string) []string {
	t.Helper()
	db := recOpenLedger(t)
	defer db.Close()
	rows, err := db.Query(`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id`,
		id, archiveRetirementRowEvent)
	if err != nil {
		t.Fatalf("read events for %s: %v", id, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		out = append(out, d)
	}
	return out
}

// TestArchiveRetirementReconcilesTheOpenRow is the defect's own repro: a branch
// retired as a decided exclusion while an OPEN row named it. Asserted: the row
// is no longer offered by the dispatch funnel (the ledger's own question), its
// status is terminal, and the recorded REASON carries the decision — the branch,
// the tag and the reason code — so a reviewer never has to re-derive git state.
func TestArchiveRetirementReconcilesTheOpenRow(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/gone")
	archOrphanBranch(t, repo, "feat/gone", "unrelated root")
	auditID := recSeedAuditRow(t, "feat/gone", taskStatusInProgress)
	scanID := recSeedScanRow(t, "feat/gone")
	bystanderID := recSeedUnrelatedRow(t, "feat/gone")

	// The premise is read from the LEDGER, never from the dispatch funnel: the
	// funnel is itself a mutator here (a PENDING family row whose branch does
	// not resolve is closed lazily by the merge-backlog ghost-ref sweep, from
	// inside TaskNextFiltered — measured in this test's first run), so invoking
	// it before the reconciliation would close the pending row by another lane
	// and hide which lane did the work. The premise is therefore the one thing
	// that is true before any pass: both member rows are LIVE.
	for _, id := range []string{auditID, scanID} {
		if status, _ := recRowState(t, id); !taskLiveHolderStatus(status) {
			t.Fatalf("premise failed: row %s is %q, want a live status", id, status)
		}
	}

	rec, err := ReconcileUnrelatedHistoryRetirement(repo, "feat/gone")
	if err != nil {
		t.Fatalf("ReconcileUnrelatedHistoryRetirement: %v", err)
	}
	if rec.Branch != "feat/gone" || rec.Tag != ArchiveBranchPrefix+"feat/gone" {
		t.Fatalf("reconcile reported branch/tag %q/%q", rec.Branch, rec.Tag)
	}
	if rec.Reason != ArchiveReasonUnrelatedHistory {
		t.Fatalf("reconcile recorded reason %q, want %q", rec.Reason, ArchiveReasonUnrelatedHistory)
	}
	if len(rec.Closed) != 2 {
		t.Fatalf("reconciled %d rows (%+v), want the 2 member rows — the bystander must not be touched",
			len(rec.Closed), rec.Closed)
	}

	// THE CLASS ACT: both rows are terminal, and the decision is visible FROM
	// THE ROW (Rule 41's whole point: not only by re-deriving git state).
	for _, id := range []string{auditID, scanID} {
		status, reason := recRowState(t, id)
		if status != taskStatusCancelled {
			t.Errorf("row %s status = %q, want %q", id, status, taskStatusCancelled)
		}
		for _, want := range []string{"feat/gone", ArchiveBranchPrefix + "feat/gone", ArchiveReasonUnrelatedHistory} {
			if !strings.Contains(reason, want) {
				t.Errorf("row %s reason %q does not name %q", id, reason, want)
			}
		}
		if !strings.Contains(reason, ArchiveReasonDoc(ArchiveReasonUnrelatedHistory)) {
			t.Errorf("row %s reason %q does not gloss the reason code", id, reason)
		}
		if evs := recEvents(t, id); len(evs) == 0 {
			t.Errorf("row %s carries no %s trail entry", id, archiveRetirementRowEvent)
		}
	}
	// And the ledger's own gate agrees: the row is terminal, so no lane offers
	// it (the state the operator reads, not the funnel's transient response).
	if status, _ := recRowState(t, scanID); status != taskStatusCancelled {
		t.Errorf("row %s status = %q after the retirement", scanID, status)
	}
	if status, _ := recRowState(t, bystanderID); status != taskStatusInProgress {
		t.Errorf("bystander row %s status = %q — a row that only MENTIONS the branch must be left alone",
			bystanderID, status)
	}
	// The branch ref survives (a retirement is not a deletion) and the reason
	// record is the one the reconciler read.
	if !archRefResolves(t, repo, ArchiveBranchPrefix+"feat/gone") {
		t.Errorf("the archive tag is gone — the retirement must preserve the decision record")
	}
	if rec := strings.TrimSpace(archReasonRecord(t, repo, "feat/gone")); !strings.Contains(rec, ArchiveReasonUnrelatedHistory) {
		t.Errorf("reason record = %q, want the code the reconcile read", rec)
	}
}

// TestArchiveRetirementHoldsALiveRowAndIsIdempotent — the two disciplines the
// first test cannot show: a row whose holder this host can measure stays OPEN
// and is REPORTED, and a second pass over the same state writes no close, no
// second tag and no second row.
func TestArchiveRetirementHoldsALiveRowAndIsIdempotent(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/held")
	archOrphanBranch(t, repo, "feat/held", "unrelated root")
	heldID := recSeedHeldRow(t, "feat/held")

	rec, err := ReconcileUnrelatedHistoryRetirement(repo, "feat/held")
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if len(rec.Closed) != 0 {
		t.Fatalf("closed %+v — a live holder's row must not be closed by the retirement", rec.Closed)
	}
	if len(rec.Held) != 1 || rec.Held[0].TaskID != heldID {
		t.Fatalf("held = %+v, want exactly the live-held row %s", rec.Held, heldID)
	}
	if status, _ := recRowState(t, heldID); status != taskStatusInProgress {
		t.Errorf("held row status = %q, want it left in_progress", status)
	}
	evs := recEvents(t, heldID)
	if len(evs) != 1 || !strings.Contains(evs[0], "left OPEN") {
		t.Errorf("held row trail = %q, want ONE entry saying the row is left OPEN", evs)
	}

	// Second pass: the tag is already recorded (the writer is idempotent by
	// reason — the same blob, so no new tag) and the held row's disposition is
	// unchanged, so the summary still reports the hold and nothing is closed.
	rec2, err := ReconcileUnrelatedHistoryRetirement(repo, "feat/held")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(rec2.Closed) != 0 || len(rec2.Held) != 1 {
		t.Fatalf("second pass changed state: closed=%+v held=%+v", rec2.Closed, rec2.Held)
	}
	if got := len(recEvents(t, heldID)); got != 2 {
		t.Errorf("held row trail after a second pass = %d entries, want 2 (one per pass, no duplicated CLOSE)", got)
	}
	tags := strings.Fields(archGit(t, repo, "tag", "-l", ArchiveBranchPrefix+"feat/held"))
	if len(tags) != 1 {
		t.Errorf("tags after two passes = %q, want exactly one", tags)
	}
	db := recOpenLedger(t)
	defer db.Close()
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE summary LIKE ?`,
		"%feat/held%").Scan(&rows); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("ledger holds %d rows naming feat/held, want 1 — the retirement files no row", rows)
	}
}

// TestArchiveRetirementWithNoLedgerIsTheEmptyAnswer is the regression guard for
// the arm the durability epic's own test hit FIRST (measured 2026-10-14): on a
// host with NO ledger at the resolved path — the ordinary shape of a test run,
// a fresh sandbox profile, or a machine whose ledger has not been built yet —
// the reconciliation must answer the EMPTY SET, not a refusal.
//
// THE DEFECT IT PINS: archiveRetirementLedger answered
// `no ledger at <path>` as an ERROR, which contradicted its own documented
// contract in the same file ("An ABSENT ledger is the empty answer, never a
// refusal") AND contradicted the package's ONE read guard, ledgerMissingForRead
// (ledger_read_guard.go), which every other pure-read lookup obeys. The caller
// is what made it expensive: cmd/agent's unrelated-history verdict renders ANY
// error as "left as it is, NOT retired" — so a retirement whose TAG HAD BEEN
// WRITTEN was reported as not having happened at all, on the one reading the
// operator needs most (the durable half ran; only the ledger half had no
// subject).
//
// FOUR ASSERTIONS, and none of them is the writer's return value alone:
//  1. no error — an absent ledger is a measurement, not a failure;
//  2. the answer SAYS so (LedgerAbsent + the path it asked), because "0 rows"
//     and "no ledger" are different facts and a caller must not have to guess
//     which one it got (Rule 15);
//  3. no rows in either arm — there is no ledger, so nothing could name the
//     branch;
//  4. the DURABLE half still happened: the tag and the reason record exist, and
//     the reason code was read back from the record. This is the half that makes
//     the test a red proof of the REAL defect rather than of an error string.
//
// The ledger is pinned through the package's ONE scaffold
// (testutil.NewTestDBPath, Rule 32): a fresh per-test path that does NOT exist,
// off the `/` partition, with the env pinned, so the test cannot reach the
// operator's own append-only ledger. Nothing in the reconcile may CREATE it —
// asserted at the end, because a read path that builds its subject is the write
// ledger_read_guard.go exists to forbid.
func TestArchiveRetirementWithNoLedgerIsTheEmptyAnswer(t *testing.T) {
	// The scaffold pins GOPI_MEMORY_DB to a per-test path under the scratch
	// root: the directory exists, the FILE does not — exactly the absent-ledger
	// shape, and never a hand-rolled path (Rule 32).
	ledger := testutil.NewTestDBPath(t)
	if fileExists(ledger) {
		t.Fatalf("the scaffold left a ledger at %s — this test is about its ABSENCE", ledger)
	}
	repo := archTestRepo(t, "feat/noledger")
	archOrphanBranch(t, repo, "feat/noledger", "unrelated root")

	rec, err := ReconcileUnrelatedHistoryRetirement(repo, "feat/noledger")
	if err != nil {
		t.Fatalf("an absent ledger must be the empty answer, never a refusal (measured: the verdict "+
			"rendered this as \"left as it is, NOT retired\" over a tag that HAD been written): %v", err)
	}
	// ② THE ANSWER NAMES WHAT IT COULD NOT READ.
	if !rec.LedgerAbsent {
		t.Errorf("LedgerAbsent = false with no ledger at %s — the caller cannot tell an empty row "+
			"set from an absent ledger, and the two sentences differ (Rule 15)", ledger)
	}
	if rec.LedgerPath != ledger {
		t.Errorf("LedgerPath = %q, want the pinned %q — a \"0 rows\" answer must name the database it asked",
			rec.LedgerPath, ledger)
	}
	// ③ NO ROWS: nothing named the branch because nothing could.
	if len(rec.Closed) != 0 || len(rec.Held) != 0 {
		t.Errorf("closed=%+v held=%+v with no ledger to read — an absent ledger holds no row",
			rec.Closed, rec.Held)
	}
	// ④ THE DECISION IS STILL RECORDED. The ledger half is not a precondition of
	// the durable half: the writer runs FIRST, and the tag is the retirement.
	if rec.Reason != ArchiveReasonUnrelatedHistory {
		t.Errorf("reason = %q, want %q — the decision was read back from the record regardless of the ledger",
			rec.Reason, ArchiveReasonUnrelatedHistory)
	}
	if !archRefResolves(t, repo, ArchiveBranchPrefix+"feat/noledger") {
		t.Errorf("no %s tag: the durable half must not depend on a ledger being present",
			ArchiveBranchPrefix+"feat/noledger")
	}
	if rec := strings.TrimSpace(archReasonRecord(t, repo, "feat/noledger")); !strings.Contains(rec, ArchiveReasonUnrelatedHistory) {
		t.Errorf("reason record = %q, want the code the reconcile read", rec)
	}
	// AND THE READ DID NOT BUILD ITS SUBJECT: a reader that mints the operator's
	// ledger from a pure-read path is the write ledger_read_guard.go forbids.
	if fileExists(ledger) {
		t.Errorf("the reconcile CREATED %s — answering \"no rows\" must not bring the ledger into existence "+
			"(ledger_read_guard.go: the first open is a schema build, a write with no subject)", ledger)
	}
}

// TestArchiveRetirementReconcilesNothingWhenTheWriterRefuses — the ORDER
// property stated as a test: the reconciliation reads its decision BACK from the
// record the writer leaves, so a retirement the owner gate REFUSED (the branch's
// own ledger row says the work FINISHED) closes nothing. A reconciler that
// trusted its caller and ran first would cancel a row on a decision nobody made.
func TestArchiveRetirementReconcilesNothingWhenTheWriterRefuses(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/owned")
	archOrphanBranch(t, repo, "feat/owned", "unrelated root")
	// The OWNER row: a FINISHED deliverable of that branch — the gate's refusal.
	archSeedRow(t, "01a0rec0wn0r00000000000000000a", TaskStatusCompleted, "feat/owned")
	// The row that asked for the merge, open and claimable.
	id := recSeedScanRow(t, "feat/owned")

	if _, err := ReconcileUnrelatedHistoryRetirement(repo, "feat/owned"); err == nil {
		t.Fatalf("reconcile succeeded on a branch the owner gate refuses")
	}
	if status, _ := recRowState(t, id); status != taskStatusPending {
		t.Errorf("row status = %q after a refused reconcile, want it untouched (pending)", status)
	}
	if archRefResolves(t, repo, ArchiveBranchPrefix+"feat/owned") {
		t.Errorf("a tag was written despite the owner gate's refusal")
	}
}

// TestArchiveRetirementOnAHostWithNoLedgerIsNotARefusal is the regression guard
// for the measured mis-report of 2026-10-14: the retirement's ledger half
// answered the ABSENT-ledger case with an ERROR, and `gopi merge`'s verdict
// renders ANY error from this call as `left as it is, NOT retired` — the one
// sentence reserved for a writer that did not run — over a writer that had just
// written the tag and the reason record.
//
// The state is ordinary, not exotic: a host that has never filed a task has no
// ledger, so the mis-report was the COMMON case on a clean machine. The
// assertion is deliberately on the OUTCOME the writer produced (the tag and the
// reason record on disk, the reconcile's return), never on the absence of the
// error string alone: a test that only checked `err == nil` would pass a
// reconciler that reconciled nothing it was asked to.
//
// The mirrored half — that the ERROR arm is still reachable for a ledger that
// exists and cannot be read — is TestArchiveRetirementReconcilesWhenTheLedgerIsUnreadable.
func TestArchiveRetirementOnAHostWithNoLedgerIsNotARefusal(t *testing.T) {
	// The absent ledger, spelled the way the production reader sees it: a path
	// with no file, whose parent directory exists, i.e. "not built yet".
	absent := filepath.Join(t.TempDir(), "memory.db")
	if !ledgerMissingForRead(absent) {
		t.Fatalf("the fixture is not the absent case: ledgerMissingForRead(%q) = false", absent)
	}
	t.Setenv(memoryDBEnv, absent)

	repo := archTestRepo(t, "feat/noledger")
	archOrphanBranch(t, repo, "feat/noledger", "unrelated root")

	rec, err := ReconcileUnrelatedHistoryRetirement(repo, "feat/noledger")
	if err != nil {
		t.Fatalf("an absent ledger was reported as a failure: %v — `gopi merge` renders this as "+
			"`left as it is, NOT retired`, i.e. a refusal to the operator over a write that succeeded", err)
	}
	// NOTHING TO RECONCILE IS NOT NOTHING DONE: the durable retirement is on
	// disk. Asserting this is what separates this test from one that only
	// checks the error is nil.
	if !archRefResolves(t, repo, ArchiveBranchPrefix+"feat/noledger") {
		t.Errorf("no %s tag — the retirement's durable half must be written regardless of ledger presence",
			ArchiveBranchPrefix+"feat/noledger")
	}
	if got := ArchiveTagReason(archReasonRecord(t, repo, "feat/noledger")); got != ArchiveReasonUnrelatedHistory {
		t.Errorf("reason record = %q, want %q", got, ArchiveReasonUnrelatedHistory)
	}
	if rec.Reason != ArchiveReasonUnrelatedHistory {
		t.Errorf("reconcile read reason %q, want %q — the decision must be read BACK from the record the writer wrote",
			rec.Reason, ArchiveReasonUnrelatedHistory)
	}
	if len(rec.Closed) != 0 || len(rec.Held) != 0 {
		t.Errorf("closed=%+v held=%+v — a host with no ledger has no rows to reconcile (and none may be invented)",
			rec.Closed, rec.Held)
	}
	// And the absence is not papered over on the way out: the ledger is still
	// absent, i.e. the read path did not CREATE the subject it was asked about
	// (the ledger_read_guard rule this file obeys).
	if fileExists(absent) {
		t.Errorf("the reconciliation BUILT %s — a read path must never create the ledger it could not find", absent)
	}
}

// TestArchiveRetirementReconcilesWhenTheLedgerIsUnreadable is the MIRROR of the
// test above, and its purpose is to stop the fix from being a blanket
// erasure of the error arm: a ledger that EXISTS and cannot be opened as one is
// genuinely "I could not look" (Rule 15), and it must still reach the caller as
// an error rather than as the reassuring empty answer.
//
// The fixture is a directory at the ledger's path: existence is true (so the
// read guard does not take the absent arm) and the opener cannot read it as a
// ledger. This is the cheap, deterministic stand-in for the permission case the
// read guard's own header documents.
func TestArchiveRetirementReconcilesWhenTheLedgerIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "memory.db")
	if err := os.Mkdir(bad, 0o700); err != nil {
		t.Fatalf("build the unreadable-ledger fixture: %v", err)
	}
	if ledgerMissingForRead(bad) {
		t.Fatalf("the fixture is not the unreadable case: ledgerMissingForRead(%q) = true", bad)
	}
	t.Setenv(memoryDBEnv, bad)

	repo := archTestRepo(t, "feat/unreadable")
	archOrphanBranch(t, repo, "feat/unreadable", "unrelated root")

	// The durable half still happens — the retirement is not conditional on the
	// ledger — but the reconciliation REPORT returns the failure, because a
	// question it could not answer must not be rendered as an answer.
	if _, err := ReconcileUnrelatedHistoryRetirement(repo, "feat/unreadable"); err == nil {
		t.Error("an unreadable ledger was reported as a clean reconciliation — a scan that could not run " +
			"must be distinguishable from a scan that found nothing (Rule 15)")
	}
	if !archRefResolves(t, repo, ArchiveBranchPrefix+"feat/unreadable") {
		t.Errorf("no %s tag — the durable retirement is written before the ledger half runs",
			ArchiveBranchPrefix+"feat/unreadable")
	}
}
