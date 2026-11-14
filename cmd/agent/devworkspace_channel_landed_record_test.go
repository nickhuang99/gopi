package main

// devworkspace_channel_landed_record_test.go — THE LANDED RECORD IS WRITTEN BY
// EVERY ARM THAT KNOWS A PUSH LANDED.
//
// THE DEFECT (this row's own family, measured on 01a19bf1-ff52):
//
//	$ gopi task show 01a19bf1-ff52 | grep clone-delivery-landed   → (nothing)
//	output.log:2344  gopi worker: clone channel: nothing to deliver —
//	  .../01a19bf1-ff52 carries no commit to deliver:
//	  `rev-list --count HEAD --not origin/master` answered 0 against origin/master ...
//	  (after 3 delivery attempt(s), the workspace ... is RECLAIMED: it held no commit past master)
//
// But that ground HAD delivered: its HEAD was origin/master's tip, the push
// accepted, and no commit of it was left unpublished. What was missing was the
// row's OWN record of the push (`clone-delivery-landed`), so the reader
// `tools.TaskLandedDeliveryTip` answered "", the empty gate lost its second
// reading, and the loop spent all MaxDeliveryRounds re-measuring a fact that
// could never change.
//
// WHERE THE GAP WAS: two arms know a delivery landed. The DELIVERED arm (this
// attempt pushed) recorded it; the ALREADY-DELIVERED arm (a previous attempt —
// or a previous run — pushed it) did not, so the second arm's own exit is what
// produced the line above.
//
// WHAT THIS FILE PINS, and the direction of each assertion:
//
//	① the already-delivered arm leaves a record naming THE GROUND'S HEAD
//	   (red before the fix: no record at all, so the tip reads "");
//	② it reads the tip BEFORE the reclaim deletes the directory (the fixture
//	   ground is gone by the time the assertions run, so a record that exists
//	   can only have been written from a read that happened first);
//	③ an unreadable tip is SAID, in the run's own log (`cfg.Log`), not only on
//	   the operator's stream — the half of this defect that is not the ledger row;
//	④ ANTI-WIDENING: the record names the arm that made the claim, so it is
//	   never read as "this process pushed it".
//
// The fake loop is the one `devworkspace_delivery_loop_test.go` already uses
// (`newFakeLoop`); this file only supplies a REAL git ground and a pinned
// ledger, so the assertions are about the shipped write/read pair rather than
// about a stub (Rule 31).

import (
	"bytes"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"

	_ "modernc.org/sqlite"
)

// landedFixtureGround materialises a REAL clone-shaped ground holding ONE commit
// and returns (dir, tip). It is deliberately the smallest sample (Rule 10.4):
// one commit, whose sha is the only thing the record has to name.
func landedFixtureGround(t *testing.T) (dir, tip string) {
	t.Helper()
	dir = testutil.TestDeviceScratchDir(t, "landed-record-ground")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		// testutil.GitEnv, NOT append(os.Environ(), …): a fixture's git child must
		// not inherit this process's repository ROUTING variables (GIT_DIR and
		// friends), or the fixture silently operates on the caller's repo
		// (Rule 13/15; the gate that caught this is TestGate9GitEnvRedProof).
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("fixture dir: %v", err)
	}
	git("init", "-q", "-b", devworkspace.DefaultBranch)
	if err := os.WriteFile(filepath.Join(dir, "delivered.txt"), []byte("the work\n"), 0o644); err != nil {
		t.Fatalf("fixture file: %v", err)
	}
	git("add", "delivered.txt")
	git("commit", "-q", "-m", "feat: the work this ground delivered")
	return dir, git("rev-parse", "HEAD")
}

// TestAlreadyDeliveredArmRecordsTheLandedTip is ARM ① and ②: the record exists,
// names the ground's HEAD, and survives the reclaim that removed the directory
// it was read from.
func TestAlreadyDeliveredArmRecordsTheLandedTip(t *testing.T) {
	// RULE 32: the ledger comes from the scaffold. `TaskDeliveryRecordLanded`
	// resolves memoryDBPath(), which the pin below is what that resolves to.
	dbPath := testutil.NewTestDBPath(t)
	const rowID = "01a19c12-3c3d-7ed4-a540-0000000000e1"

	ground, tip := landedFixtureGround(t)

	f := newFakeLoop(t, nil, &devworkspace.Blocker{
		Kind:  devworkspace.BlockerKindAlreadyDelivered,
		Step:  "rev-list --count HEAD --not origin/master",
		Words: ground + " carries no commit to deliver — but this run's delivery already landed (" + tip[:8] + ")",
	}, nil)
	// THE DECLARED BRANCH IS PART OF THE FIXTURE (measured twice here, and both
	// measurements are why this line exists):
	//   · an EMPTY one makes the writer skip the record outright —
	//     `clone-delivery-landed record skipped: … branch=""`;
	//   · a FOREIGN one (`master`) makes the writer succeed and the READER
	//     refuse, because `deliveryProofRowAddresses` admits only names this row
	//     can legitimately be delivered under, and `master` is none of them.
	// Production stamps the row's own `task/<id>` here, so the fixture does too.
	f.state.cfg.TaskID = rowID
	f.state.cfg.Branch = tools.TaskGroundBranchName(rowID)
	// THE FAKE LOOP'S `Result.Branch` IS `master` AND PRODUCTION'S IS NOT.
	// `devworkspace.AttemptDelivery` stamps the branch the GROUND STANDS ON
	// (`devworkspace.go:690`, `:820`), which on a post-`90f6a96` clone ground is
	// `task/<id>`; `landedRecordBranchOf` prefers that field, and the reader
	// admits only the row's own addresses. So the fixture has to answer what
	// production would — measured both ways: with the fake's `master` the writer
	// SUCCEEDS and the reader refuses, which looks exactly like the defect and
	// is not.
	inner := f.state.attempt
	f.state.attempt = func() (devworkspace.Result, *devworkspace.Blocker, error) {
		res, blk, err := inner()
		res.Branch = tools.TaskGroundBranchName(rowID)
		return res, blk, err
	}
	f.state.dir = ground
	var runLog bytes.Buffer
	f.state.cfg.Log = &runLog

	_, err := f.state.finish(&f.log)
	if err == nil {
		t.Fatal("the already-delivered exit must still carry its reason as an error")
	}

	// ② THE GROUND IS GONE — the reclaim ran, so the tip could only have been
	// read before it. This is the ORDER the arm's own comment claims.
	if _, statErr := os.Stat(ground); statErr == nil {
		t.Fatalf("the fixture ground survived the reclaim — this arm's ordering is not being exercised:\n%s", runLog.String())
	}

	// ① THE RECORD EXISTS AND NAMES THIS TIP. Read through the SHIPPING reader,
	// not by grepping the trail: the reader is what the empty gate consults.
	got := tools.TaskLandedDeliveryTip(rowID, "")
	if got != tip {
		t.Fatalf("the already-delivered arm left no usable landed record for its own tip\n"+
			"  want tip %s\n  got      %q\n"+
			"WITHOUT the record the empty gate falls back to \"carries no commit to deliver\" — the line this row was filed on.\n"+
			"run log:\n%s", tip, got, runLog.String())
	}
	if !strings.Contains(got, tip) {
		t.Fatalf("the record must name the whole sha, got %q for tip %s", got, tip)
	}

	// There was a readable tip, so the unreadable-tip sentence must NOT appear.
	if strings.Contains(runLog.String(), "could NOT be written") {
		t.Fatalf("a readable tip must not be reported as unrecordable:\n%s", runLog.String())
	}

	// The ledger really is the pinned one (Rule 32): the record landed somewhere,
	// and that somewhere is the scratch DB, never the operator's live profile.
	if _, statErr := os.Stat(dbPath); statErr != nil {
		t.Fatalf("the pinned ledger is not on disk: %v", statErr)
	}
	testutil.RefuseOperatorProfileWrite(t, "the landed-record fixture ledger", dbPath)
}

// TestAlreadyDeliveredArmSaysWhenTheTipCannotBeRead is ARM ③: a delivery that
// landed but whose tip cannot be read must leave a NAMED reason in the RUN'S OWN
// log (`cfg.Log`). Before the fix that sentence existed only on stderr, and
// stderr of the close-out is not the run's output.log — so the run left nothing
// a later reader could diagnose.
func TestAlreadyDeliveredArmSaysWhenTheTipCannotBeRead(t *testing.T) {
	testutil.NewTestDBPath(t)
	const rowID = "01a19c12-3c3d-7ed4-a540-0000000000e2"

	// A directory that is NOT a git repository: CloneDeliveryTip answers "".
	notARepo := testutil.TestDeviceScratchDir(t, "landed-record-noground")

	f := newFakeLoop(t, nil, &devworkspace.Blocker{
		Kind:  devworkspace.BlockerKindAlreadyDelivered,
		Step:  "rev-list --count HEAD --not origin/master",
		Words: notARepo + " carries no commit to deliver",
	}, nil)
	// THE DECLARED BRANCH IS PART OF THE FIXTURE (measured twice here, and both
	// measurements are why this line exists):
	//   · an EMPTY one makes the writer skip the record outright —
	//     `clone-delivery-landed record skipped: … branch=""`;
	//   · a FOREIGN one (`master`) makes the writer succeed and the READER
	//     refuse, because `deliveryProofRowAddresses` admits only names this row
	//     can legitimately be delivered under, and `master` is none of them.
	// Production stamps the row's own `task/<id>` here, so the fixture does too.
	f.state.cfg.TaskID = rowID
	f.state.cfg.Branch = tools.TaskGroundBranchName(rowID)
	// THE FAKE LOOP'S `Result.Branch` IS `master` AND PRODUCTION'S IS NOT.
	// `devworkspace.AttemptDelivery` stamps the branch the GROUND STANDS ON
	// (`devworkspace.go:690`, `:820`), which on a post-`90f6a96` clone ground is
	// `task/<id>`; `landedRecordBranchOf` prefers that field, and the reader
	// admits only the row's own addresses. So the fixture has to answer what
	// production would — measured both ways: with the fake's `master` the writer
	// SUCCEEDS and the reader refuses, which looks exactly like the defect and
	// is not.
	inner := f.state.attempt
	f.state.attempt = func() (devworkspace.Result, *devworkspace.Blocker, error) {
		res, blk, err := inner()
		res.Branch = tools.TaskGroundBranchName(rowID)
		return res, blk, err
	}
	f.state.dir = notARepo
	var runLog, operatorStream bytes.Buffer
	f.state.cfg.Log = &runLog

	if _, err := f.state.finish(&operatorStream); err == nil {
		t.Fatal("the already-delivered exit must still carry its reason as an error")
	}

	// THE RUN'S OWN LOG CARRIES IT (this is the assertion the fix adds).
	if !strings.Contains(runLog.String(), "could NOT be written") {
		t.Fatalf("the run's own log must name the unwritable record, and it does not:\n%s", runLog.String())
	}
	// It names the ARM, the ROW and the DIRECTORY — the three things a later
	// reader needs (Rule 15: a silent or anonymous failure is the defect).
	for _, want := range []string{rowID, cloneLandedArmAlreadyDelivered, notARepo} {
		if !strings.Contains(runLog.String(), want) {
			t.Fatalf("the sentence must name %q, got:\n%s", want, runLog.String())
		}
	}
	// AND NOTHING WAS RECORDED — an unreadable tip must never be filed as a
	// delivery of some other commit.
	if got := tools.TaskLandedDeliveryTip(rowID, ""); got != "" {
		t.Fatalf("an unreadable tip must record NOTHING, got %q", got)
	}
}

// TestLandedRecordNamesTheArmThatMadeTheClaim is ARM ④, the anti-widening half.
// The already-delivered arm pushed nothing in this process, so its record must
// not wear the delivered arm's words ("pushed to origin after N attempt(s)"),
// which the writer substitutes for an empty detail. A record that claimed a push
// this arm did not make is the same class of defect as a status claim with no
// observation behind it (Rule 5).
func TestLandedRecordNamesTheArmThatMadeTheClaim(t *testing.T) {
	testutil.NewTestDBPath(t)
	const rowID = "01a19c12-3c3d-7ed4-a540-0000000000e3"

	ground, tip := landedFixtureGround(t)

	// Drive the ONE method the arm calls, through the arm's own arm label, so
	// this arm cannot pass by the delivered arm's detail.
	state := &cloneRunState{
		cfg: devworkspace.Config{TaskID: rowID, Branch: tools.TaskGroundBranchName(rowID)},
		dir: ground,
	}
	var log bytes.Buffer
	state.recordLandedDelivery(&log, devworkspace.Result{}, cloneLandedArmAlreadyDelivered)

	if got := tools.TaskLandedDeliveryTip(rowID, ""); !strings.Contains(got, tip) {
		t.Fatalf("the record must carry the ground's tip %s, got %q", tip, got)
	}
	// The trail's detail column names the arm. Read it back through the SQL the
	// writer used, so the assertion is about bytes on the row, not about a
	// sentence this test happened to build.
	detail := landedRecordDetailForTest(t, rowID)
	if !strings.Contains(detail, cloneLandedArmAlreadyDelivered) {
		t.Fatalf("the record's detail must name the arm that made the claim (%q), got %q",
			cloneLandedArmAlreadyDelivered, detail)
	}
	if strings.Contains(detail, "pushed to origin after") {
		t.Fatalf("the already-delivered arm must not claim a push it did not make: %q", detail)
	}
}

// landedRecordDetailForTest reads the row's landed record detail straight out of
// the pinned ledger — the bytes the reader parses, not a re-rendering.
func landedRecordDetailForTest(t *testing.T, taskID string) string {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(os.Getenv(config.MemoryDBEnv)))
	if err != nil {
		t.Fatalf("open pinned ledger: %v", err)
	}
	defer db.Close()
	var detail string
	if err := db.QueryRow(
		`SELECT detail FROM task_events WHERE task_id=? AND event=? ORDER BY id DESC LIMIT 1`,
		taskID, tools.DeliveryLandedEventName).Scan(&detail); err != nil {
		t.Fatalf("no %s record on %s: %v", tools.DeliveryLandedEventName, taskID, err)
	}
	return detail
}
