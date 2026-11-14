package tools

// task_briefing_authoring_test.go — the ANCHOR half of the dispatch seam: which
// row's clock is the moment a briefing's TEXT was written.
//
// THE DEFECT THESE ARMS PIN (measured 2026-10-27, rows 01a159e5 / 01a179e7):
// the gate-assertion clause anchored every "<surface> is RED on master" claim to
// the DISPATCHED row's created_at. For a rework/resume successor (`<id>-r1`,
// tasktags.ReworkLineageTag) that is the wrong clock — its payload is the
// parent's text copied verbatim, so the claim is SIX DAYS older than the row
// that carries it. The seam then resolved the assertion to a revision the text
// was never written at and printed it as evidence:
//
//	anchored on the successor's clock   epoch 1793040588 → 0d313323, distance   8
//	the text's own writing moment       epoch 1792509551 → bf40185,  distance 381
//
// The arms drive the REAL ledger (Rule 32: the scaffold's fresh per-test DB,
// never a hand-rolled path) and the REAL reissue engine, and the last arm drives
// the seam's clause function against a REAL git repository — so the revision and
// the distance it asserts are computed the way production computes them, never
// by a test-local arithmetic that could only agree with itself.
//
// SEVEN ARMS: ① the copy is read off the lineage edge (the positive case); ② the
// ordinary row is unaffected (its own clock wins); ③ a NON-verbatim payload is
// UNMEASURED rather than anchored to either clock; ④ a lineage-marked row whose
// parent is GONE is UNMEASURED; ⑤ the bounded walk stops and says so; ⑥ an
// in-place payload revision moves the moment FORWARD; ⑦ the seam's OUTPUT — the
// printed epoch, revision and distance — follows the parent's clock, with the
// pre-fix reading asserted first as the RED PROOF that the two clocks really do
// resolve differently.

import (
	"context"
	"database/sql"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tasktags"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// The two clocks the measured defect was made of, both NAMED (Rule 1) so the
// assertions compare constants whose relationship is the whole point rather than
// re-typing numbers.
const (
	briefingAuthoringParentEpoch    int64 = 1700000000
	briefingAuthoringSuccessorStep  int64 = 6 * 24 * 60 * 60 // SIX DAYS: the measured gap
	briefingAuthoringSuccessorEpoch       = briefingAuthoringParentEpoch + briefingAuthoringSuccessorStep
	// briefingAuthoringBetweenCommits is how many commits sit between the text's
	// moment and the successor row's: the false stamp's distance came from
	// exactly this shape (measured: 8 from the successor's clock, 381 from the
	// text's).
	briefingAuthoringBetweenCommits = 8
	// briefingAuthoringTailCommits are the commits that landed AFTER the
	// successor row was minted, so the branch tip is later than BOTH clocks and
	// neither reading degenerates into the "authored at the tip, nothing to
	// stamp" positive control.
	briefingAuthoringTailCommits = 5
	// briefingAuthoringTextDistance / ...RowDistance are the two distances the
	// fixture must produce, derived from the counts above rather than typed.
	briefingAuthoringTextDistance = briefingAuthoringBetweenCommits + 1 + briefingAuthoringTailCommits
	briefingAuthoringRowDistance  = briefingAuthoringTailCommits
	briefingAuthoringBriefingText = "`go test -run TestDocClassBannerMatchesManifest ./internal/standards/` is RED on master, in a file this branch does not touch:\n"
	// briefingAuthoringRowTag is the tag set the fixtures' rows carry: the
	// reserved bug-flow tag, so nothing in these arms depends on a bespoke tag.
	briefingAuthoringRowTag = tasktags.BugTag
)

// briefingAuthoringLedger pins this test's OWN fresh ledger (Rule 32: the
// scaffold's writer shape, never a hand-rolled path) and returns a handle whose
// schema was built by the package's own opener — so the fixture writes through
// the SAME DDL, triggers and FTS maintenance production does (Rules 8/13).
func briefingAuthoringLedger(t *testing.T) *sql.DB {
	t.Helper()
	testutil.NewTestDBPath(t)
	return openTasksDBForTest(t)
}

// openTasksDBForTest opens the ledger the scaffold pinned, for the arms that need
// a WRITER handle. It is the package's own opener (one reading, Rule 8), so the
// fixture never builds a schema of its own.
func openTasksDBForTest(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	t.Cleanup(func() { db.Close() }) //stylegate:ignore the fixture's own handle
	return db
}

// briefingAuthoringSeedRow inserts one row with a chosen payload, tags and
// parent edge. The CLOCK is set separately (briefingAuthoringSetClock): this
// file's whole subject is two moments days apart, and the ledger's insert stamps
// "now".
func briefingAuthoringSeedRow(t *testing.T, db *sql.DB, f taskCreateFields) {
	t.Helper()
	if _, err := insertTaskRow(context.Background(), db, f); err != nil {
		t.Fatalf("insert row %s: %v", f.TaskID, err)
	}
}

// briefingAuthoringSetClock moves one row's created_at to a KNOWN epoch. It is a
// direct UPDATE on a row this test just created: no production door lets a
// caller choose a row's creation time (correctly), and that clock is this arm's
// INPUT.
func briefingAuthoringSetClock(t *testing.T, db *sql.DB, id string, at int64) {
	t.Helper()
	if _, err := db.Exec(`UPDATE tasks SET created_at = ? WHERE task_id = ?`, float64(at), id); err != nil {
		t.Fatalf("set created_at of %s: %v", id, err)
	}
}

// briefingAuthoringSuccessorOf reads the successor back OFF THE LEDGER — the
// only honest way to name it since a clone mints a FRESH uuid (operator ruling
// 2026-11-05): there is no suffix to derive from the parent id.
func briefingAuthoringSuccessorOf(t *testing.T, db *sql.DB, parentID string) string {
	t.Helper()
	var succ string
	if err := db.QueryRowContext(context.Background(),
		`SELECT task_id FROM tasks WHERE parent_task_id = ? ORDER BY id DESC LIMIT 1`, parentID).Scan(&succ); err != nil {
		t.Fatalf("read the successor of %s back off the ledger: %v", parentID, err)
	}
	return succ
}

// TestBriefingAuthoring_ACopiedPayloadAnchorsAtItsParentsClock is arm ① — the
// defect's own shape, seeded through the REAL reissue engine so the lineage
// mark, the parent edge and the payload copy are exactly what production writes.
func TestBriefingAuthoring_ACopiedPayloadAnchorsAtItsParentsClock(t *testing.T) {
	db := briefingAuthoringLedger(t)
	const parentID = "01a159e5-0000-7000-8000-00000000dead"
	briefingAuthoringSeedRow(t, db, taskCreateFields{
		TaskID:  parentID,
		Summary: "row whose briefing carries the assertion",
		Status:  taskStatusFailed,
		Tags:    briefingAuthoringRowTag,
		Payload: briefingAuthoringBriefingText,
		Source:  "auto",
	})
	briefingAuthoringSetClock(t, db, parentID, briefingAuthoringParentEpoch)

	out, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{
		TaskID: parentID,
		Reason: "the run never produced a verdict",
	})
	if err != nil {
		t.Fatalf("reissue %s: %v", parentID, err)
	}
	succ := briefingAuthoringSuccessorOf(t, db, parentID)
	if !strings.Contains(out, succ) {
		t.Fatalf("reissue reported %q — it must name the row the ledger holds (%q)", out, succ)
	}
	briefingAuthoringSetClock(t, db, succ, briefingAuthoringSuccessorEpoch)

	got, has, record, err := TaskBriefingAuthoringAt(succ)
	if err != nil {
		t.Fatalf("TaskBriefingAuthoringAt(%s): %v", succ, err)
	}
	if !has {
		t.Fatalf("the successor's payload IS its parent's text, so its writing moment is established; got UNMEASURED with record %v", record)
	}
	if int64(got) != briefingAuthoringParentEpoch {
		t.Fatalf("the anchor must be the moment the TEXT was written (%d), not the successor row's own clock (%d); got %d",
			briefingAuthoringParentEpoch, briefingAuthoringSuccessorEpoch, int64(got))
	}
	// The record must show BOTH clocks, so a reader can see which one produced
	// the anchor — a single number would be indistinguishable from the defect.
	joined := strings.Join(record, "\n")
	for _, want := range []string{
		strconv.FormatInt(briefingAuthoringParentEpoch, 10),
		strconv.FormatInt(briefingAuthoringSuccessorEpoch, 10),
		parentID,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the derivation record must carry %q; record:\n%s", want, joined)
		}
	}
	// The copy is a LEDGER FACT, not a rule re-applied from code that ran once:
	// the reissue wrote it down, and this reads it back through the ledger.
	var copies int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND event = ?`,
		succ, BriefingPayloadCopyEventName).Scan(&copies); err != nil {
		t.Fatalf("read the copy trail of %s: %v", succ, err)
	}
	if copies != 1 {
		t.Fatalf("the reissue must record exactly ONE %q event on the successor (that is what makes the copy answerable later); got %d",
			BriefingPayloadCopyEventName, copies)
	}
}

// TestBriefingAuthoring_AnOrdinaryRowKeepsItsOwnClock is arm ②'s positive
// control: a row that OWNS its text is anchored at its own created_at. A fix
// that shifted every anchor backwards would be a second defect.
func TestBriefingAuthoring_AnOrdinaryRowKeepsItsOwnClock(t *testing.T) {
	db := briefingAuthoringLedger(t)
	const id = "01a179e7-0000-7000-8000-00000000beef"
	briefingAuthoringSeedRow(t, db, taskCreateFields{
		TaskID: id, Summary: "ordinary row", Status: taskStatusPending,
		Tags: briefingAuthoringRowTag, Payload: briefingAuthoringBriefingText, Source: "auto",
	})
	briefingAuthoringSetClock(t, db, id, briefingAuthoringSuccessorEpoch)

	got, has, record, err := TaskBriefingAuthoringAt(id)
	if err != nil {
		t.Fatalf("TaskBriefingAuthoringAt(%s): %v", id, err)
	}
	if !has || int64(got) != briefingAuthoringSuccessorEpoch {
		t.Fatalf("a row whose payload is its own text must anchor at created_at (%d); got value=%d present=%v record=%v",
			briefingAuthoringSuccessorEpoch, int64(got), has, record)
	}
	if len(record) == 0 {
		t.Fatal("the derivation must be RECORDED, never silent (Rule 15)")
	}
}

// TestBriefingAuthoring_ANonVerbatimCopyIsUnmeasured is arm ③, and it is the arm
// that keeps this reader honest: the lineage tag CLAIMS a copy, and a row
// carrying the tag whose payload does NOT contain its parent's has broken that
// claim — so neither clock can be called the text's writing moment, and the
// answer is UNMEASURED with its reason, never a pick.
func TestBriefingAuthoring_ANonVerbatimCopyIsUnmeasured(t *testing.T) {
	db := briefingAuthoringLedger(t)
	const parentID = "01a159e5-0000-7000-8000-00000000cafe"
	const succID = "01a159e5-0000-7000-8000-00000000caff"
	briefingAuthoringSeedRow(t, db, taskCreateFields{
		TaskID: parentID, Summary: "parent", Status: taskStatusFailed,
		Tags: briefingAuthoringRowTag, Payload: "the parent's own briefing text", Source: "auto",
	})
	briefingAuthoringSetClock(t, db, parentID, briefingAuthoringParentEpoch)
	briefingAuthoringSeedRow(t, db, taskCreateFields{
		TaskID: succID, Summary: "rework with an EDITED payload", Status: taskStatusPending,
		ParentTaskID: parentID, Tags: briefingAuthoringRowTag + "," + tasktags.ReworkLineageTag,
		Payload: "a briefing the successor wrote itself, sharing no bytes with the parent",
		Source:  "auto",
	})
	briefingAuthoringSetClock(t, db, succID, briefingAuthoringSuccessorEpoch)

	got, has, record, err := TaskBriefingAuthoringAt(succID)
	if err != nil {
		t.Fatalf("TaskBriefingAuthoringAt(%s): %v", succID, err)
	}
	if has || got != 0 {
		t.Fatalf("a lineage-marked row whose payload is NOT the parent's text has no provable writing moment; got value=%d present=%v (anchoring it to either clock would be a guess)", int64(got), has)
	}
	if len(record) != 1 || !strings.Contains(record[0], "UNMEASURED") {
		t.Fatalf("the refusal must be STATED, never a silent pass; record: %v", record)
	}
}

// TestBriefingAuthoring_AMissingParentIsUnmeasured is arm ④: the edge names a
// row the ledger does not hold, so the text cannot be read back and no clock can
// be attributed to it.
func TestBriefingAuthoring_AMissingParentIsUnmeasured(t *testing.T) {
	db := briefingAuthoringLedger(t)
	const succID = "01a159e5-0000-7000-8000-00000000feed"
	briefingAuthoringSeedRow(t, db, taskCreateFields{
		TaskID: succID, Summary: "orphan successor", Status: taskStatusPending,
		Tags:    briefingAuthoringRowTag + "," + tasktags.ReworkLineageTag,
		Payload: "copied briefing text", Source: "auto",
	})
	// The edge is set AFTER the insert: this arm needs an edge to a row that does
	// not exist, which no production door would write.
	if _, err := db.Exec(`UPDATE tasks SET parent_task_id = ? WHERE task_id = ?`,
		"01a159e5-0000-8000-8000-000000000000", succID); err != nil {
		t.Fatalf("set a dangling parent edge: %v", err)
	}
	got, has, record, err := TaskBriefingAuthoringAt(succID)
	if err != nil {
		t.Fatalf("TaskBriefingAuthoringAt(%s): %v", succID, err)
	}
	if has || got != 0 {
		t.Fatalf("a dangling parent edge cannot supply a writing moment; got value=%d present=%v", int64(got), has)
	}
	if len(record) != 1 || !strings.Contains(record[0], "UNMEASURED") {
		t.Fatalf("the refusal must be STATED and name the missing parent; record: %v", record)
	}
}

// TestBriefingAuthoring_ARunawayLineageStopsAtItsBound is arm ⑤: a chain longer
// than the walk's ceiling is UNMEASURED with the bound and the row it stopped at
// — a walk that ran out of budget must never mint an anchor where it gave up, and
// a cycle must not become one either.
func TestBriefingAuthoring_ARunawayLineageStopsAtItsBound(t *testing.T) {
	db := briefingAuthoringLedger(t)
	const rootID = "01a159e5-0000-7000-8000-0000000a0000"
	const rootPayload = "root briefing text"
	briefingAuthoringSeedRow(t, db, taskCreateFields{
		TaskID: rootID, Summary: "root", Status: taskStatusPending,
		Tags: briefingAuthoringRowTag, Payload: rootPayload, Source: "auto",
	})
	prev := rootID
	// Each link copies the previous payload VERBATIM, so every copy is provable
	// and only the BOUND can end the walk.
	for i := 0; i <= taskBriefingLineageMaxRows+1; i++ {
		next := "01a159e5-0000-7000-8000-0000000a0" + strconv.Itoa(100+i)
		briefingAuthoringSeedRow(t, db, taskCreateFields{
			TaskID: next, Summary: "link " + strconv.Itoa(i), Status: taskStatusPending,
			ParentTaskID: prev, Tags: briefingAuthoringRowTag + "," + tasktags.ReworkLineageTag,
			Payload: rootPayload, Source: "auto",
		})
		prev = next
	}
	got, has, record, err := TaskBriefingAuthoringAt(prev)
	if err != nil {
		t.Fatalf("TaskBriefingAuthoringAt(%s): %v", prev, err)
	}
	if has || got != 0 {
		t.Fatalf("a chain deeper than the walk's ceiling (%d rows) must NOT produce an anchor; got value=%d present=%v",
			taskBriefingLineageMaxRows, int64(got), has)
	}
	if len(record) != 1 || !strings.Contains(record[0], "UNMEASURED") ||
		!strings.Contains(record[0], strconv.Itoa(taskBriefingLineageMaxRows+1)) {
		t.Fatalf("the refusal must STATE the bound it hit and the row it stopped at; record: %v", record)
	}
}

// TestBriefingAuthoring_AnInPlaceRevisionMovesTheMomentForward is arm ⑥: a
// briefing rewritten through the ledger's own door (TaskSetPayload) after its row
// was created has a LATER writing moment, recorded in the revision trail — the
// only place that moment exists.
func TestBriefingAuthoring_AnInPlaceRevisionMovesTheMomentForward(t *testing.T) {
	db := briefingAuthoringLedger(t)
	const id = "01a179e7-0000-7000-8000-00000000d00d"
	briefingAuthoringSeedRow(t, db, taskCreateFields{
		TaskID: id, Summary: "row whose briefing was rewritten", Status: taskStatusPending,
		Tags: briefingAuthoringRowTag, Payload: briefingAuthoringBriefingText, Source: "auto",
	})
	briefingAuthoringSetClock(t, db, id, briefingAuthoringParentEpoch)
	// The revision is written through the ledger's OWN event vocabulary
	// (PayloadRevisionEventName, the name TaskSetPayload uses) at a KNOWN later
	// epoch: the trial is the reader's arithmetic, not the writer's.
	if _, err := db.Exec(`INSERT INTO task_events (task_id, event, detail, created_at) VALUES (?, ?, ?, ?)`,
		id, PayloadRevisionEventName, "reason: rewritten briefing", float64(briefingAuthoringSuccessorEpoch)); err != nil {
		t.Fatalf("append a payload revision: %v", err)
	}
	got, has, record, err := TaskBriefingAuthoringAt(id)
	if err != nil {
		t.Fatalf("TaskBriefingAuthoringAt(%s): %v", id, err)
	}
	if !has || int64(got) != briefingAuthoringSuccessorEpoch {
		t.Fatalf("an in-place revision at %d is a LATER writing moment than created_at %d, so it must win; got value=%d present=%v record=%v",
			briefingAuthoringSuccessorEpoch, briefingAuthoringParentEpoch, int64(got), has, record)
	}
}

// TestBriefingGateAssert_AReworkRowAnchorsAtItsPayloadsClock is arm ⑦ — THE
// DEFECT, reproduced end-to-end. It seeds the two-clock shape through the real
// reissue engine, resolves the anchor through the ledger accessor, then runs the
// seam's clause function against a REAL repository whose commits sit at those
// same epochs. The reading it asserts is the one the two paying runs read: the
// epoch printed, the revision it resolves to, and the distance — all must follow
// the PARENT's clock, and the successor's clock must appear nowhere as the
// anchor. The pre-fix reading is measured FIRST, so this fixture is proven able
// to tell the two clocks apart.
func TestBriefingGateAssert_AReworkRowAnchorsAtItsPayloadsClock(t *testing.T) {
	repo := briefingAuthoringClockRepo(t)
	db := briefingAuthoringLedger(t)
	const parentID = "01a159e5-0000-7000-8000-0000000b0000"
	briefingAuthoringSeedRow(t, db, taskCreateFields{
		TaskID: parentID, Summary: "the row that wrote the assertion", Status: taskStatusFailed,
		Tags: briefingAuthoringRowTag, Payload: briefingAuthoringBriefingText, Source: "auto",
	})
	briefingAuthoringSetClock(t, db, parentID, briefingAuthoringParentEpoch)
	if _, err := TaskCloneWithOptions(context.Background(), TaskCloneOptions{
		TaskID: parentID,
		Reason: "the run never produced a verdict",
	}); err != nil {
		t.Fatalf("reissue %s: %v", parentID, err)
	}
	succ := briefingAuthoringSuccessorOf(t, db, parentID)
	// The successor's OWN clock is days later — the clock the seam used to read,
	// and the one that produced the false revision.
	briefingAuthoringSetClock(t, db, succ, briefingAuthoringSuccessorEpoch)

	at, has, record, err := TaskBriefingAuthoringAt(succ)
	if err != nil || !has {
		t.Fatalf("the anchor must be established for a proven copy; err=%v has=%v record=%v", err, has, record)
	}
	// The two readings the defect is made of, computed through the seam's OWN
	// resolver (Rules 8/13), so the assertions cannot pass by agreeing with a
	// test-local arithmetic.
	wantRev := briefingAuthoringRevAt(t, repo, briefingAuthoringParentEpoch)
	wrongRev := briefingAuthoringRevAt(t, repo, briefingAuthoringSuccessorEpoch)
	if wantRev == "" || wrongRev == "" || wantRev == wrongRev {
		t.Fatalf("fixture: the two clocks must resolve to DIFFERENT revisions or this arm proves nothing (parent→%q successor→%q)", wantRev, wrongRev)
	}

	// THE RED PROOF: the pre-fix anchor (the successor's own clock) must really
	// produce the OTHER revision and the OTHER distance — the short one, which is
	// why the false stamp looked precise.
	oldClause, _ := TaskBriefingGateAssertRefresh(repo, float64(briefingAuthoringSuccessorEpoch), briefingAuthoringBriefingText)
	if !strings.Contains(oldClause, masterClaimShortSha(wrongRev)) {
		t.Fatalf("RED PROOF FAILED: the pre-fix anchor (successor clock %d) must resolve to %s — that is the defect; clause:\n%s",
			briefingAuthoringSuccessorEpoch, masterClaimShortSha(wrongRev), oldClause)
	}
	if strings.Contains(oldClause, masterClaimShortSha(wantRev)) {
		t.Fatalf("RED PROOF FAILED: the pre-fix anchor already named the text's own revision (%s), so this fixture cannot tell the two clocks apart; clause:\n%s",
			masterClaimShortSha(wantRev), oldClause)
	}
	if !strings.Contains(oldClause, strconv.Itoa(briefingAuthoringRowDistance)) {
		t.Fatalf("RED PROOF FAILED: the pre-fix anchor must report the SHORT distance (%d commits) — the number that made the false stamp look precise; clause:\n%s",
			briefingAuthoringRowDistance, oldClause)
	}

	// THE GREEN: the shipped anchor (the text's clock) names the other revision.
	clause, gateRecord := TaskBriefingGateAssertRefresh(repo, at, briefingAuthoringBriefingText)
	if clause == "" {
		t.Fatalf("the briefing asserts a surface RED on master and its text is old, so it must be stamped; record: %v", gateRecord)
	}
	if !strings.Contains(clause, strconv.FormatInt(briefingAuthoringParentEpoch, 10)) {
		t.Errorf("the clause's epoch must be the TEXT's writing moment (%d); clause:\n%s", briefingAuthoringParentEpoch, clause)
	}
	if strings.Contains(clause, strconv.FormatInt(briefingAuthoringSuccessorEpoch, 10)) {
		t.Errorf("the clause must NOT print the successor ROW's clock (%d) as the anchor — precise evidence pointing at the wrong revision is this defect; clause:\n%s",
			briefingAuthoringSuccessorEpoch, clause)
	}
	if !strings.Contains(clause, masterClaimShortSha(wantRev)) {
		t.Errorf("the clause must name the revision the TEXT's moment resolves to (%s); clause:\n%s", masterClaimShortSha(wantRev), clause)
	}
	if strings.Contains(clause, masterClaimShortSha(wrongRev)) {
		t.Errorf("the clause must NOT name the revision the successor's own clock resolves to (%s); clause:\n%s", masterClaimShortSha(wrongRev), clause)
	}
	// The DISTANCE must be the text's, not the successor row's: the whole point
	// of the anchor is which stretch of history it measures.
	if !strings.Contains(clause, strconv.Itoa(briefingAuthoringTextDistance)) {
		t.Errorf("the clause must state the distance measured from the TEXT's moment (%d commits); clause:\n%s", briefingAuthoringTextDistance, clause)
	}
}

// briefingAuthoringRevAt resolves the revision the fixture's integration branch
// stood at, through the PRODUCTION resolver the seam itself uses.
func briefingAuthoringRevAt(t *testing.T, repo string, epoch int64) string {
	t.Helper()
	rev, err := gateAssertRevisionAt(repo, IntegrationClaimTargetRef(repo), epoch)
	if err != nil {
		t.Fatalf("gateAssertRevisionAt(%s, %d): %v", repo, epoch, err)
	}
	return rev
}

// briefingAuthoringClockRepo builds a repository whose integration branch
// carries commits at the two epochs this file is about, so `git rev-list
// --before` — the seam's own anchor — resolves them to DIFFERENT revisions with a
// non-trivial distance. The commits are empty on purpose: these arms measure
// which REVISION a moment resolves to, not what the tree contains.
func briefingAuthoringClockRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git := func(at int64, args ...string) string {
		t.Helper()
		stamp := strconv.FormatInt(at, 10) + " +0000"
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		// testutil.GitEnv, not os.Environ(): a fixture's git child must not inherit
		// the caller's repository routing (Rule 13/15's git-env gate — the class
		// that once wrote 12 commits onto a real integration branch).
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp,
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), repo, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(briefingAuthoringParentEpoch, "init", "-q", "-b", IntegrationBranchName)
	git(briefingAuthoringParentEpoch, "commit", "--allow-empty", "-q", "-m", "the revision the text was written at")
	for i := 1; i <= briefingAuthoringBetweenCommits; i++ {
		git(briefingAuthoringParentEpoch+int64(i), "commit", "--allow-empty", "-q", "-m", "later work "+strconv.Itoa(i))
	}
	git(briefingAuthoringSuccessorEpoch, "commit", "--allow-empty", "-q", "-m", "the revision the successor ROW's clock resolves to")
	// The branch keeps moving after the row was minted — the state the defect was
	// found in (a row written days ago, dispatched against a much later tip).
	for i := 1; i <= briefingAuthoringTailCommits; i++ {
		git(briefingAuthoringSuccessorEpoch+int64(i), "commit", "--allow-empty", "-q", "-m", "work after the row was minted "+strconv.Itoa(i))
	}
	return repo
}
