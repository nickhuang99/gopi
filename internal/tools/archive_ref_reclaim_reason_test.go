package tools

// The reclaim arm's REASON half (epic archive-reclaim-reason, 2026-10-17).
//
// THE DEFECT THESE TESTS PIN. archiveReclaimDelete removes a decided branch's
// head ref — and, before this epic, it never asked for the branch's reason
// record. So the arm consumed the archive decision and left the WHY unwritten:
// measured on this host, 11 decided heads were removed and 10 of them ended
// with no refs/gopi/archive-reason/<branch> at all, so `gopi task archive
// --check` reported the arm's own output as reason-less debt (239 → 229 only
// after a human wrote the ten records by hand).
//
// EVERY ASSERTION IS READ BACK WITH GIT, never inferred from the arm's report:
// the head ref, the tag ref's object id and type, and the reason ref's blob are
// all read out of the fixture repository after the arm ran. The RED half drives
// the SAME code path with the reason seam removed (the pre-fix behaviour) and
// requires the green assertions to FAIL there — a proof that cannot go red has
// stopped proving anything (the shape archive_ref_reclaim_audit_test.go uses).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/removalaudit"
	"github.com/nick/gopi/internal/testutil"
)

// reclaimReasonBlob reads the companion ref's BLOB through `git cat-file blob`
// — exactly the read an operator is told to make — and reports whether the ref
// is a blob at all. "" with blobOK=false means the ref does not exist.
func reclaimReasonBlob(t *testing.T, repo, branch string) (body string, blobOK bool) {
	t.Helper()
	ref := archiveReasonRefPrefix + branch
	typ, err := repoSweepGit(repo, "cat-file", "-t", ref)
	if err != nil {
		return "", false
	}
	if strings.TrimSpace(typ) != "blob" {
		t.Fatalf("%s resolves to %q, want a blob", ref, strings.TrimSpace(typ))
	}
	body, err = repoSweepGit(repo, "cat-file", "blob", ref)
	if err != nil {
		t.Fatalf("read %s as a blob: %v", ref, err)
	}
	return body, true
}

// reclaimTagObject reads the archive tag's own object id and type — the two
// facts that decide "was the durable half touched?" (Rule 9): a lightweight tag
// on a commit answers (tip, commit) and the id must not change.
func reclaimTagObject(t *testing.T, repo, branch string) (sha, typ string) {
	t.Helper()
	ref := archiveTagRefPrefix + branch
	sha, err := repoSweepGit(repo, "rev-parse", ref)
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	typ, err = repoSweepGit(repo, "cat-file", "-t", ref)
	if err != nil {
		t.Fatalf("cat-file -t %s: %v", ref, err)
	}
	return strings.TrimSpace(sha), strings.TrimSpace(typ)
}

// reclaimReasonDetailLine is the record's `detail:` line — the acceptance's
// "the detail names the reclaim arm and the tag tip".
func reclaimReasonDetailLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "detail: ") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// TestArchiveReclaimRecordsTheReasonBeforeRemovingTheHead is the epic's main
// case: a reason-LESS archive tag (the 21:04 / post-21:04 shape) whose head the
// arm reclaims must leave a companion reason ref behind, parseable to a known
// code, naming the reclaim arm and the tip — while the archive tag stays
// byte-identical at the same commit.
func TestArchiveReclaimRecordsTheReasonBeforeRemovingTheHead(t *testing.T) {
	repo := rsTestRepo(t)
	const branch = "feat/decided-without-a-why"
	rsMakeBranch(t, repo, branch)
	rsGit(t, repo, true, "tag", archiveBranchPrefix+branch, branch)
	tip := reclaimHeadTip(t, repo, branch)
	tagShaBefore, tagTypeBefore := reclaimTagObject(t, repo, branch)
	if tagTypeBefore != gitObjectTypeCommit {
		t.Fatalf("fixture: the archive tag must stay LIGHTWEIGHT on a commit, got %q", tagTypeBefore)
	}

	res := ArchiveReclaimDecidedRefs(repo)
	if res.Err != "" {
		t.Fatalf("reclaim scan failed: %s", res.Err)
	}
	if res.Deleted != 1 {
		t.Fatalf("deleted=%d, want 1 (rows: %+v)", res.Deleted, res.Rows)
	}
	// ① THE HEAD IS GONE — the half that already worked.
	if after := reclaimHeadTip(t, repo, branch); after != "" {
		t.Fatalf("%s%s still resolves to %s", refsHeadsPrefix, branch, after)
	}
	// ② THE REASON REF EXISTS, IS A BLOB, AND PARSES TO A KNOWN CODE.
	body, ok := reclaimReasonBlob(t, repo, branch)
	if !ok {
		t.Fatalf("%s%s does not exist — the arm reclaimed the head and left the decision unreasoned",
			archiveReasonRefPrefix, branch)
	}
	code := ArchiveTagReason(body)
	if code != ArchiveReasonReclaimCarried {
		t.Fatalf("record parses to reason %q, want %q (body:\n%s)", code, ArchiveReasonReclaimCarried, body)
	}
	if !strings.HasPrefix(body, archiveReasonLinePrefix) {
		t.Fatalf("the record's FIRST line must be %q, got %q", archiveReasonLinePrefix+"<code>", firstLine(body))
	}
	// ③ THE DETAIL NAMES THE ARM AND THE TIP.
	detail := reclaimReasonDetailLine(body)
	if !strings.Contains(detail, string(removalaudit.ArmArchiveReclaimBranch)) {
		t.Errorf("detail line does not name the reclaim arm %q: %q", removalaudit.ArmArchiveReclaimBranch, detail)
	}
	if !strings.Contains(detail, tip) {
		t.Errorf("detail line does not name the tag tip %s: %q", tip, detail)
	}
	// ④ THE TAG IS UNTOUCHED: same object id, same type, same commit.
	tagShaAfter, tagTypeAfter := reclaimTagObject(t, repo, branch)
	if tagShaAfter != tagShaBefore || tagTypeAfter != tagTypeBefore {
		t.Fatalf("the archive tag was rewritten: %s %s → %s %s (Rule 9: the durable half is never moved)",
			tagTypeBefore, tagShaBefore, tagTypeAfter, tagShaAfter)
	}
	if got := reclaimTagExists(t, repo, branch); got != tip {
		t.Fatalf("archive tag resolves to %q, want %q", got, tip)
	}
}

// TestArchiveReclaimNeverRewritesAnExistingReason pins the IDEMPOTENCE half: a
// branch whose decision was recorded by another lane (the park lane, a compile
// gate, `gopi task archive`) keeps THAT record — a reclaim must not restamp it
// with its own code, because that record is the only copy of why the branch
// lost.
func TestArchiveReclaimNeverRewritesAnExistingReason(t *testing.T) {
	repo := rsTestRepo(t)
	const branch = "feat/parked-with-a-why"
	rsMakeBranch(t, repo, branch)
	// The lane that DECIDED writes both halves through the one writer.
	if _, err := ArchiveTagReasoned(repo, branch, ArchiveReasonNotFFAblePark, "parked by the merger (fixture)"); err != nil {
		t.Fatalf("fixture archive: %v", err)
	}
	before, ok := reclaimReasonBlob(t, repo, branch)
	if !ok || ArchiveTagReason(before) != ArchiveReasonNotFFAblePark {
		t.Fatalf("fixture: the lane's record must exist and parse, got %q", before)
	}
	tagShaBefore, _ := reclaimTagObject(t, repo, branch)

	res := ArchiveReclaimDecidedRefs(repo)
	if res.Err != "" || res.Deleted != 1 {
		t.Fatalf("reclaim: err=%q deleted=%d (%+v)", res.Err, res.Deleted, res.Rows)
	}
	after, ok := reclaimReasonBlob(t, repo, branch)
	if !ok {
		t.Fatalf("the record vanished during the reclaim")
	}
	if after != before {
		t.Fatalf("an existing record was REWRITTEN by the reclaim:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if code := ArchiveTagReason(after); code != ArchiveReasonNotFFAblePark {
		t.Fatalf("record now parses to %q, want the author's code %q", code, ArchiveReasonNotFFAblePark)
	}
	if tagShaAfter, _ := reclaimTagObject(t, repo, branch); tagShaAfter != tagShaBefore {
		t.Fatalf("tag moved: %s → %s", tagShaBefore, tagShaAfter)
	}
	// And a SECOND reclaim of the same (now head-less) branch writes nothing.
	again := ArchiveReclaimDecidedRefs(repo)
	if again.Deleted != 0 || again.Err != "" {
		t.Fatalf("second pass must be a no-op, got %+v", again)
	}
	if still, _ := reclaimReasonBlob(t, repo, branch); still != before {
		t.Fatalf("the idempotent pass rewrote the record")
	}
}

// TestArchiveReclaimKeepsTheHeadWhenTheReasonCannotBeWritten is the Rule 15 half
// of the order: RECORD BEFORE REMOVE. Two arms are driven through the seam —
//
//	① a recorder that checks the HEAD REF STILL RESOLVES at the moment it is
//	   asked to write (the order the epic demands; a delete-first arm fails
//	   here), and
//	② a recorder that FAILS, whose failure must leave the head ref exactly
//	   where it was with the reason reported.
func TestArchiveReclaimKeepsTheHeadWhenTheReasonCannotBeWritten(t *testing.T) {
	// ① THE ORDER.
	repo := rsTestRepo(t)
	const branch = "feat/order-probe"
	rsMakeBranch(t, repo, branch)
	rsGit(t, repo, true, "tag", archiveBranchPrefix+branch, branch)
	tip := reclaimHeadTip(t, repo, branch)
	var seenWhileRecording string
	recorder := func(r, b, tagTip string) error {
		seenWhileRecording = reclaimHeadTip(t, r, b)
		if tagTip != tip {
			return fmt.Errorf("seam called with tip %q, want %q", tagTip, tip)
		}
		return archiveReclaimReasonRecorder(r, b, tagTip)
	}
	verdict, _, _, reason := archiveReclaimOneWith(repo, branch, archiveBranchPrefix+branch, recorder)
	if verdict != ArchiveReclaimDeleted {
		t.Fatalf("verdict = %q (%s), want %q", verdict, reason, ArchiveReclaimDeleted)
	}
	if seenWhileRecording != tip {
		t.Fatalf("the reason was written with the head already %q (want %q) — the arm removed the ref before recording",
			seenWhileRecording, tip)
	}

	// ② THE FAILURE.
	repo2 := rsTestRepo(t)
	const branch2 = "feat/record-fails"
	rsMakeBranch(t, repo2, branch2)
	rsGit(t, repo2, true, "tag", archiveBranchPrefix+branch2, branch2)
	tip2 := reclaimHeadTip(t, repo2, branch2)
	failing := func(string, string, string) error { return errReclaimReasonUnwritable }
	verdict2, _, _, reason2 := archiveReclaimOneWith(repo2, branch2, archiveBranchPrefix+branch2, failing)
	if verdict2 != ArchiveReclaimSkipped {
		t.Fatalf("verdict = %q, want %q when the record cannot be written", verdict2, ArchiveReclaimSkipped)
	}
	if got := reclaimHeadTip(t, repo2, branch2); got != tip2 {
		t.Fatalf("%s%s = %q, want the ref KEPT at %q — deleting it would deepen the reason-less backlog",
			refsHeadsPrefix, branch2, got, tip2)
	}
	if !strings.Contains(reason2, errReclaimReasonUnwritable.Error()) {
		t.Errorf("the refusal must carry the writer's failure, got %q", reason2)
	}
	if _, ok := reclaimReasonBlob(t, repo2, branch2); ok {
		t.Errorf("no record was written, yet %s%s exists", archiveReasonRefPrefix, branch2)
	}
	// The tag survives, at the same commit: a refused reclaim is not a decision.
	if got := reclaimTagExists(t, repo2, branch2); got != tip2 {
		t.Errorf("archive tag = %q, want %q", got, tip2)
	}
}

// errReclaimReasonUnwritable stands in for a reason write that git refused.
var errReclaimReasonUnwritable = errors.New("reason ref unwritable (fixture)")

// TestArchiveReclaimStopsTheReasonlessBacklogGrowing is the ACCEPTANCE
// measurement, on a fixture repo (never the operator's): a branch whose owning
// row is completed and whose tag carries no record IS a backlog row; after the
// reclaim it must NOT be — the record the arm wrote closes it.
//
// The RED half is the pre-fix behaviour driven through the same entry point with
// the reason seam removed: the head still goes, the record is still absent, and
// the backlog still counts the branch. That is what makes the assertion above a
// measurement of the record rather than of the arm's willingness to delete.
func TestArchiveReclaimStopsTheReasonlessBacklogGrowing(t *testing.T) {
	const branch = "feat/completed-and-unreasoned"

	// GREEN: the shipped arm.
	repo := rsTestRepo(t)
	seedCompletedBranchRow(t, branch)
	rsMakeBranch(t, repo, branch)
	rsGit(t, repo, true, "tag", archiveBranchPrefix+branch, branch)
	if n := reclaimBacklogCount(t, repo); n != 1 {
		t.Fatalf("fixture: a reason-less tag over a completed row must be 1 backlog row, got %d", n)
	}
	if res := ArchiveReclaimDecidedRefs(repo); res.Err != "" || res.Deleted != 1 {
		t.Fatalf("reclaim: err=%q deleted=%d (%+v)", res.Err, res.Deleted, res.Rows)
	}
	if n := reclaimBacklogCount(t, repo); n != 0 {
		t.Fatalf("after the reclaim the backlog is %d, want 0 — the arm's own output became debt again", n)
	}

	// RED: the pre-fix behaviour (no reason seam at all).
	repo2 := rsTestRepo(t)
	seedCompletedBranchRow(t, branch)
	rsMakeBranch(t, repo2, branch)
	rsGit(t, repo2, true, "tag", archiveBranchPrefix+branch, branch)
	if n := reclaimBacklogCount(t, repo2); n != 1 {
		t.Fatalf("RED fixture: backlog = %d, want 1", n)
	}
	verdict, _, _, _ := archiveReclaimOneWith(repo2, branch, archiveBranchPrefix+branch, nil)
	if verdict != ArchiveReclaimDeleted {
		t.Fatalf("RED half: verdict = %q, want %q (the pre-fix arm deletes without recording)", verdict, ArchiveReclaimDeleted)
	}
	if got := reclaimHeadTip(t, repo2, branch); got != "" {
		t.Fatalf("RED half: head ref = %q, want it removed", got)
	}
	if _, ok := reclaimReasonBlob(t, repo2, branch); ok {
		t.Fatal("RED half: a record exists without the seam — the green half is not measuring the record")
	}
	if n := reclaimBacklogCount(t, repo2); n != 1 {
		t.Fatalf("RED half: the pre-fix arm must leave the branch in the backlog (got %d) — "+
			"otherwise the green assertion proves nothing about the record", n)
	}
}

// seedCompletedBranchRow writes ONE ledger row naming branch, with a terminal
// (`completed`) status — the backlog check's own subject. The database comes
// from the scaffold (Rule 32): pinned scratch root, env contract, never the
// operator's live profile.
func seedCompletedBranchRow(t *testing.T, branch string) {
	t.Helper()
	ctx := context.Background()
	conn, err := openTasksDB(ctx, testutil.NewTestDBPath(t))
	if err != nil {
		t.Fatalf("open the fixture ledger: %v", err)
	}
	defer conn.Close()
	deliverable := `{"repo":"/tmp/reclaim-fixture","branch":"` + branch + `"}`
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, claimed_by, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, ?, '', ?, 'auto', ?, ?)`,
		"t-reclaim-reason", "fixture row for "+branch, TaskStatusCompleted, deliverable,
		nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed the completed row: %v", err)
	}
}

// reclaimBacklogCount is the operator-facing figure (`gopi task archive
// --check`'s first line, "N reason-less archive tag(s) over a completed/verified
// task row") read through the SAME function the verb calls.
func reclaimBacklogCount(t *testing.T, repo string) int {
	t.Helper()
	rows, err := ArchiveReasonlessBacklog(repo)
	if err != nil {
		t.Fatalf("backlog scan: %v", err)
	}
	return len(rows)
}
