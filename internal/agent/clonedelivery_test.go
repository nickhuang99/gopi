package agent

// clonedelivery_test.go — the bare-prompt face's DELIVERY LEG
// (internal/agent/clonedelivery.go, epic raw-prompt-clone-channel-delivery-leg).
//
// WHAT IS PINNED, in the order the acceptance names it:
//
//	① a run that TOOK the clone ground ends by attempting delivery exactly
//	   ONCE, and the outcome verifiably changes the ledger (reason + trail) and
//	   the operator's own stream;
//	② blocked and broken are BOTH reported in all three places — the three-state
//	   contract is what makes "did it land?" answerable without parsing git;
//	③ a run that did NOT take the clone ground calls the leg ZERO times and
//	   prints nothing (the backward-compatibility contract, byte for byte);
//	④ the production path really reaches devworkspace.AttemptDelivery — not just
//	   an injected fake — proved by driving it against a real dirty git repo and
//	   reading back git's OWN porcelain words.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/runend"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// cloneDeliveryAgent builds an Agent whose end hook will really register, close
// and (when a ground is set) deliver: a private profile, a private ledger and a
// log buffer the assertions read.
func cloneDeliveryAgent(t *testing.T) (*Agent, *bytes.Buffer) {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(tools.TaskWrapperEnv, "")
	sl, err := NewSessionLogger("test-model", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { _ = sl.Close() }) //stylegate:ignore test teardown flush; assertions read the ledger, not log durability
	buf := &bytes.Buffer{}
	return &Agent{SessionLog: sl, Logger: log.New(buf, "", 0)}, buf
}

// TestCloneGroundRunAttemptsDeliveryOnce is acceptance ①/②: ONE attempt, and the
// BLOCKED verdict reaches the stream, the row's reason and the trail.
func TestCloneGroundRunAttemptsDeliveryOnce(t *testing.T) {
	a, buf := cloneDeliveryAgent(t)
	a.maybeRegisterSessionTask("deliver the clone ground's commits")
	rowID := a.autoTaskRowID
	if rowID == "" {
		t.Fatal("the start hook registered no row — the fixture premise is broken")
	}
	a.cloneGroundDir = t.TempDir()
	a.cloneGroundBranch = "master"

	calls := 0
	a.cloneDeliveryAttempt = func(_ context.Context, _ devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error) {
		calls++
		if dir != a.cloneGroundDir {
			t.Errorf("the attempt was given %q, want the run's clone ground %q", dir, a.cloneGroundDir)
		}
		return devworkspace.Result{Dir: dir, Branch: "master"},
			&devworkspace.Blocker{Kind: devworkspace.BlockerKindDirty, Words: `" M a.txt"`, Paths: []string{" M a.txt"}}, nil
	}

	a.finishSessionTask(0, 3, 100, 1.0)

	if calls != 1 {
		t.Fatalf("the clone ground's run must attempt delivery exactly once, got %d", calls)
	}
	out := buf.String()
	if !strings.Contains(out, "delivery BLOCKED") {
		t.Errorf("the BLOCKED verdict must reach the operator's stream:\n%s", out)
	}
	if !strings.Contains(out, "a.txt") {
		t.Errorf("the blocked line must carry git's own words:\n%s", out)
	}
	row, err := tools.TaskShow(rowID)
	if err != nil {
		t.Fatalf("task show: %v", err)
	}
	if !strings.Contains(row, "delivery did not land") {
		t.Errorf("the row's reason must carry the non-delivery (Rule 15):\n%s", row)
	}
	if !strings.Contains(row, "a.txt") {
		t.Errorf("the row's reason must quote the blocker's own words:\n%s", row)
	}
	// THE ROW MUST LAND A TERMINAL STATUS, and specifically NOT the success one.
	// This assertion used to demand `in_progress` ("a BLOCKED delivery must
	// leave the row OPEN"), which was the contract until bug 01a19639 measured
	// that nothing re-drives an open row: the lifecycle pass and the
	// `retry-delivery` verb were retired 2026-10-30, so a real row of exactly
	// this shape read `status: in_progress / verdict: dead / resumable: no` for
	// 65 minutes until an operator typed `--status failed` by hand. What the
	// premise was protecting is kept and stated explicitly — the row must NOT
	// claim a success it cannot prove — and what it missed is now required: the
	// row lands `failed`, so the dispatcher stops reading it as running,
	// `--resume` can reach it, and the reissue census lists it.
	if strings.Contains(row, "status:         "+tools.TaskStatusCompleted) {
		t.Errorf("a BLOCKED delivery must never land the success terminal — that is exactly the lie deliveryProofRefusal exists to refuse:\n%s", row)
	}
	if !strings.Contains(row, "status:         "+tools.TaskStatusFailed) {
		t.Errorf("a BLOCKED delivery must land a TERMINAL `%s` (an `%s` row with a dead holder is the shape no dispatcher, no resume and no census can act on — bug 01a19639):\n%s",
			tools.TaskStatusFailed, tools.TaskStatusInProgress, row)
	}
	// THE TRAIL is the append-only half: a reason edit is a column, the event is
	// history, and `task show` renders the newest renderable event as `result:`.
	if !strings.Contains(row, tools.DeliveryDidNotLandEventName) &&
		!strings.Contains(row, "delivery did not land") {
		t.Errorf("the trail must carry the outcome event %q:\n%s", tools.DeliveryDidNotLandEventName, row)
	}
	// THE STUTTER (fixed 2026-10-30). blk.Report() opens with the channel's own
	// prefix for EVERY Kind, and this leg used to paste it again — the reason,
	// the stream line and the trail detail each read
	// `clone channel: delivery BLOCKED — clone channel: refusing to pull: …`.
	// A Contains() assertion cannot see that; a COUNT can, and it is the
	// assertion that goes red if any future body gains a prefix.
	assertChannelPrefixOnce(t, "the operator's BLOCKED line", blockedLine(t, out))
	assertChannelPrefixOnce(t, "the row's non-delivery reason", cloneDeliveryTrailDetail(t, rowID, tools.DeliveryDidNotLandEventName))
}

// TestCloneGroundRunReportsBrokenDeliveryToo — acceptance ②'s other arm: "not
// about the work at all" is a DIFFERENT shape and must not be silent either.
func TestCloneGroundRunReportsBrokenDeliveryToo(t *testing.T) {
	a, buf := cloneDeliveryAgent(t)
	a.maybeRegisterSessionTask("deliver the clone ground's commits")
	rowID := a.autoTaskRowID
	a.cloneGroundDir = t.TempDir()
	a.cloneGroundBranch = "master"
	a.cloneDeliveryAttempt = func(context.Context, devworkspace.Config, string) (devworkspace.Result, *devworkspace.Blocker, error) {
		return devworkspace.Result{}, nil, errors.New("clone channel: push failed on attempt 1 and the remote did not move (no such remote)")
	}

	a.finishSessionTask(0, 2, 50, 1.0)

	out := buf.String()
	if !strings.Contains(out, "delivery BROKEN") {
		t.Errorf("the BROKEN verdict must reach the operator's stream:\n%s", out)
	}
	row, _ := tools.TaskShow(rowID) //stylegate:ignore ledger read after the run; the compared row is the contract
	if !strings.Contains(row, "no such remote") {
		t.Errorf("the row's reason must carry the broker's own error text:\n%s", row)
	}
	// THE BROKEN ARM'S STUTTER: the error a caller hands this leg MAY already
	// open with the channel's prefix (AttemptDelivery composes most of its
	// errors that way — measured 2026-10-30: the row read
	// `clone channel: delivery did not land — clone channel: push failed …`).
	// One strip, one re-add: both surfaces carry the prefix exactly once.
	assertChannelPrefixOnce(t, "the operator's BROKEN line", brokenLine(t, out))
	assertChannelPrefixOnce(t, "the row's non-delivery reason (prefixed error body)",
		cloneDeliveryTrailDetail(t, rowID, tools.DeliveryDidNotLandEventName))
}

// TestDeliveredRunRecordsItsLanding — the happy arm: delivered writes a LANDED
// trail event (and no non-delivery reason), so a reader of the row can tell the
// three outcomes apart.
func TestDeliveredRunRecordsItsLanding(t *testing.T) {
	a, buf := cloneDeliveryAgent(t)
	a.maybeRegisterSessionTask("deliver the clone ground's commits")
	rowID := a.autoTaskRowID
	a.cloneGroundDir = t.TempDir()
	a.cloneGroundBranch = "master"
	a.cloneDeliveryAttempt = func(_ context.Context, _ devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error) {
		return devworkspace.Result{Dir: dir, Branch: "master", Pushed: true, Detail: "pushed master to origin after 1 attempt(s)"}, nil, nil
	}

	a.finishSessionTask(0, 2, 50, 1.0)

	if out := buf.String(); !strings.Contains(out, "delivery delivered") {
		t.Errorf("a delivered run must say so:\n%s", out)
	}
	row, _ := tools.TaskShow(rowID) //stylegate:ignore ledger read after the run; the compared row is the contract
	if strings.Contains(row, "delivery did not land") {
		t.Errorf("a delivered run must not carry the non-delivery reason:\n%s", row)
	}
	if !strings.Contains(row, "status:         "+tools.TaskStatusCompleted) {
		t.Errorf("a DELIVERED run must close `completed` — the delivery proof at the write choke point measures the destination the push just moved:\n%s", row)
	}
}

// TestNonCloneGroundRunNeverAttemptsDelivery is acceptance ③ — the
// backward-compatibility contract. Every other face (worktree channel, chat,
// a `task run` child) has an empty ground, and an empty ground must cost ZERO
// attempts and ZERO lines.
func TestNonCloneGroundRunNeverAttemptsDelivery(t *testing.T) {
	for _, arm := range []struct {
		name   string
		setRow bool
	}{
		{"registered row, no clone ground", true},
		{"no row at all", false},
	} {
		t.Run(arm.name, func(t *testing.T) {
			a, buf := cloneDeliveryAgent(t)
			if arm.setRow {
				a.maybeRegisterSessionTask("worktree channel run")
			}
			calls := 0
			a.cloneDeliveryAttempt = func(context.Context, devworkspace.Config, string) (devworkspace.Result, *devworkspace.Blocker, error) {
				calls++
				return devworkspace.Result{}, nil, nil
			}
			a.finishSessionTask(0, 1, 1, 1.0)
			if calls != 0 {
				t.Fatalf("a run with no clone ground must NEVER attempt delivery, got %d call(s)", calls)
			}
			if out := buf.String(); strings.Contains(out, "clone channel") {
				t.Fatalf("a run with no clone ground must print no clone-channel line:\n%s", out)
			}
		})
	}
}

// TestCloneGroundDeliveryReachesTheProductionAttempt is acceptance ④: with NO
// injected attempt, the leg must reach devworkspace.AttemptDelivery for real.
//
// The fixture is a git repo with a MODIFIED TRACKED file and a declared build in
// the settings `repos` table, so the production clean gate is what answers — and
// its answer is git's own porcelain listing, which is what the row must carry.
// The assertion is on THAT listing appearing in the row: a fake could not
// produce it, which is the whole point of this arm.
func TestCloneGroundDeliveryReachesTheProductionAttempt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	git("init", "-q", "-b", "master")
	git("config", "receive.denyCurrentBranch", "ignore")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "a.txt")
	git("commit", "-q", "-m", "init")
	// THE DIRT the production clean gate must catch.
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\ndirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	body := `{"repos":{"` + repo + `":{"build":"true"}}}`
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)
	// The DECLARATION hangs on the source repo (the clone channel's own
	// precedence): the fixture repo IS the declared `dev_repo` here.
	t.Setenv("GOPI_DEV_REPO", repo)

	a, buf := cloneDeliveryAgent(t)
	a.maybeRegisterSessionTask("deliver from a dirty clone workspace")
	rowID := a.autoTaskRowID
	a.cloneGroundDir = repo // the REAL (dirty) repository stands in for the workspace
	a.cloneGroundBranch = "master"

	a.finishSessionTask(0, 2, 50, 1.0)

	if out := buf.String(); !strings.Contains(out, "delivery BLOCKED") {
		t.Fatalf("the production attempt must reach git's clean gate and come back blocked:\n%s", out)
	}
	row, _ := tools.TaskShow(rowID) //stylegate:ignore ledger read after the run; the compared row is the contract
	if !strings.Contains(row, "a.txt") {
		t.Fatalf("the row must carry git's own porcelain listing for a.txt (only the REAL AttemptDelivery produces it):\n%s", row)
	}
	if !strings.Contains(row, string(devworkspace.BlockerKindDirty)) {
		t.Fatalf("the row must name the blocker kind %q:\n%s", devworkspace.BlockerKindDirty, row)
	}
}

// TestCloneGroundDeliverySurvivesATruncatedRun pins that the leg is not on the
// happy path only: the deferred call runs for an engine-truncated close too
// (status=failed), and the reason write follows THAT status rather than a status
// the leg invents.
func TestCloneGroundDeliverySurvivesATruncatedRun(t *testing.T) {
	a, _ := cloneDeliveryAgent(t)
	a.maybeRegisterSessionTask("a run the engine cut short")
	rowID := a.autoTaskRowID
	a.cloneGroundDir = t.TempDir()
	a.cloneGroundBranch = "master"
	calls := 0
	a.cloneDeliveryAttempt = func(context.Context, devworkspace.Config, string) (devworkspace.Result, *devworkspace.Blocker, error) {
		calls++
		return devworkspace.Result{}, &devworkspace.Blocker{Kind: devworkspace.BlockerKindBuild, Words: "build: compile error"}, nil
	}
	a.finishSessionTask(0, 4, 200, 2.0)
	// Truncated close, same run shape.
	a.autoTaskRegistered = true
	a.cloneGroundDir = t.TempDir()
	a.finishSessionTask(0, 4, 200, 2.0) // latch already cleared: the second close is a no-op
	a.autoTaskRegistered = true
	a.finishSessionTask(0, 4, 200, 2.0)

	if calls < 1 {
		t.Fatalf("the delivery leg must run for a truncated run, calls=%d", calls)
	}
	row, _ := tools.TaskShow(rowID) //stylegate:ignore ledger read after the run; the compared row is the contract
	if !strings.Contains(row, "delivery did not land") {
		t.Fatalf("a truncated run's row must carry the non-delivery reason:\n%s", row)
	}
}

// TestCloneDeliveryRunsOnEveryEndHookPath is the placement proof: the leg is a
// DEFER on finishSessionTaskRecorded, so the early return (no registered row /
// no session log) still reaches it. That is what makes the duty a property of
// the hook rather than of the happy path.
func TestCloneDeliveryRunsOnEveryEndHookPath(t *testing.T) {
	a, buf := cloneDeliveryAgent(t)
	// No start hook: autoTaskRegistered is false, so the core body returns at its
	// gate — but the deferred leg must still run and report (with no row to carry
	// the reason, which it says out loud).
	a.cloneGroundDir = t.TempDir()
	a.cloneGroundBranch = "master"
	calls := 0
	a.cloneDeliveryAttempt = func(context.Context, devworkspace.Config, string) (devworkspace.Result, *devworkspace.Blocker, error) {
		calls++
		return devworkspace.Result{}, &devworkspace.Blocker{Kind: devworkspace.BlockerKindDirty, Words: `" M a.txt"`}, nil
	}
	a.finishSessionTaskRecorded(0, 1, 1, 1.0, nil, runend.Finished)

	if calls != 1 {
		t.Fatalf("the leg is deferred, so it must run even when the hook's body returns early; calls=%d", calls)
	}
	out := buf.String()
	if !strings.Contains(out, "delivery BLOCKED") {
		t.Fatalf("the verdict must be printed even with no row:\n%s", out)
	}
	if !strings.Contains(out, "could not be written to a row") {
		t.Fatalf("a run that closed no row must SAY so rather than silently dropping the outcome:\n%s", out)
	}
}

// TestDeliveredRunRecordsThePushedTIP — the landed record must name the COMMIT
// (2026-10-27, row 01a17a67). A record that names only the branch cannot be used
// to judge a commit, and the delivery proof needs exactly that when a `pull
// --rebase` RE-DERIVED the pushed content: nothing else survives to match on.
//
// The ground here is a REAL repository, so the tip is a real object name; the
// assertion is the round trip through the reader the guard uses.
func TestDeliveredRunRecordsThePushedTIP(t *testing.T) {
	a, _ := cloneDeliveryAgent(t)
	a.maybeRegisterSessionTask("deliver the clone ground's commits")
	rowID := a.autoTaskRowID
	if rowID == "" {
		t.Fatal("the start hook registered no row — the fixture premise is broken")
	}
	ground := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "master", ground},
		{"-C", ground, "config", "user.email", "tip@test"},
		{"-C", ground, "config", "user.name", "tip"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(ground, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", ground, "add", "a.txt"},
		{"-C", ground, "commit", "-q", "-m", "the run's work"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	tip := strings.TrimSpace(mustGit(t, ground, "rev-parse", "HEAD"))

	a.cloneGroundDir = ground
	a.cloneGroundBranch = "master"
	a.cloneDeliveryAttempt = func(_ context.Context, _ devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error) {
		return devworkspace.Result{Dir: dir, Branch: "master", Pushed: true, Detail: "pushed master to origin after 1 attempt(s)"}, nil, nil
	}

	a.finishSessionTask(0, 2, 50, 1.0)

	// Read the TRAIL, not `task show`: the landed record is history, and a later
	// status event legitimately owns the row's `result:` line. The event's own
	// detail is the artifact this test is about.
	record := landedRecordDetail(t, rowID)
	if !strings.Contains(record, tip) {
		t.Fatalf("the landed record must name the pushed tip %s — a record naming only the branch cannot "+
			"judge a commit, and a rebase that re-derived the content leaves nothing else to match on:\n%s", tip, record)
	}
	if !strings.HasPrefix(record, tools.DeliveryLandedRecordPrefix+"master"+tools.DeliveryLandedRecordFrom) {
		t.Fatalf("the landed record must be in the ONE documented grammar (%q + branch + %q …), got:\n%s",
			tools.DeliveryLandedRecordPrefix, tools.DeliveryLandedRecordFrom, record)
	}
	if !strings.Contains(record, tools.DeliveryLandedRecordAt+tip) {
		t.Fatalf("the tip must sit at the record's own anchor %q, got:\n%s", tools.DeliveryLandedRecordAt, record)
	}
}

// landedRecordDetail reads the newest landed record's detail from the ledger the
// fixture pinned (GOPI_MEMORY_DB — the caller owns that pin, Rule 32).
func landedRecordDetail(t *testing.T, rowID string) string {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerReadOnlyDSN(os.Getenv("GOPI_MEMORY_DB")))
	if err != nil {
		t.Fatalf("open the pinned ledger: %v", err)
	}
	defer db.Close()
	var detail string
	err = db.QueryRow(`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id DESC LIMIT 1`,
		rowID, tools.DeliveryLandedEventName).Scan(&detail)
	if err != nil {
		t.Fatalf("no %q record on row %s: %v", tools.DeliveryLandedEventName, rowID, err)
	}
	return detail
}

// cloneDeliveryTrailDetail reads the newest event's detail for a row from the
// ledger the fixture pinned (GOPI_MEMORY_DB — the caller owns that pin, Rule 32).
//
// It is the surface the STUTTER was measured on: `task show` renders a reason
// twice-ish (reason column plus the newest renderable event), so a prefix COUNT
// taken over its output is not a reading of one composed string. The event's
// detail IS that one string.
func cloneDeliveryTrailDetail(t *testing.T, rowID, event string) string {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerReadOnlyDSN(os.Getenv("GOPI_MEMORY_DB")))
	if err != nil {
		t.Fatalf("open the pinned ledger: %v", err)
	}
	defer db.Close()
	var detail string
	err = db.QueryRow(`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id DESC LIMIT 1`,
		rowID, event).Scan(&detail)
	if err != nil {
		t.Fatalf("no %q record on row %s: %v", event, rowID, err)
	}
	return detail
}

// cloneDeliveryRowReason reads a row's `reason` COLUMN from the pinned ledger —
// the surface the operator sees as `reason:` in `task show`, for the arms that
// deliberately record no trail event (the three completions).
func cloneDeliveryRowReason(t *testing.T, rowID string) string {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerReadOnlyDSN(os.Getenv("GOPI_MEMORY_DB")))
	if err != nil {
		t.Fatalf("open the pinned ledger: %v", err)
	}
	defer db.Close()
	var reason string
	if err := db.QueryRow(`SELECT reason FROM tasks WHERE task_id = ?`, rowID).Scan(&reason); err != nil {
		t.Fatalf("read row %s's reason: %v", rowID, err)
	}
	if strings.TrimSpace(reason) == "" {
		t.Fatalf("row %s carries no reason — the assertion below would pass on an empty string", rowID)
	}
	return reason
}

// streamLinesWith returns the operator-stream lines carrying marker, one entry
// per line, so an assertion reads the LINE the leg composed rather than the
// whole buffer (a whole-buffer Count would be a count over several lines).
func streamLinesWith(t *testing.T, out, marker string) []string {
	t.Helper()
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, marker) {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		t.Fatalf("the stream carries no line with %q:\n%s", marker, out)
	}
	return kept
}

// blockedLine / brokenLine name the two operator lines by their own marker.
func blockedLine(t *testing.T, out string) string {
	t.Helper()
	return strings.Join(streamLinesWith(t, out, cloneDeliveryBlockedMarker), "\n")
}

func brokenLine(t *testing.T, out string) string {
	t.Helper()
	return strings.Join(streamLinesWith(t, out, cloneDeliveryBrokenMarker), "\n")
}

// assertChannelPrefixOnce is THE assertion this defect needed and Contains()
// could not make: the channel's prefix appears exactly ONCE in the sentence.
//
// A Contains() check is satisfied by `clone channel: delivery BLOCKED — clone
// channel: …`, which is precisely the string the operator read. Counting is the
// only reading that distinguishes "the channel spoke" from "the channel spoke
// twice" (measured 2026-10-30 on both the stream and the row's reason).
func assertChannelPrefixOnce(t *testing.T, what, sentence string) {
	t.Helper()
	if n := strings.Count(sentence, cloneChannelLinePrefix); n != 1 {
		t.Fatalf("%s must carry %q exactly ONCE, got %d — a doubled prefix is the stutter defect "+
			"(a body that opens with the channel's prefix was concatenated under the same prefix):\n%s",
			what, cloneChannelLinePrefix, n, sentence)
	}
	if !strings.HasPrefix(sentence, cloneChannelLinePrefix) {
		t.Fatalf("%s must still OPEN with %q — the fix is one strip and one re-add, not a deletion:\n%s",
			what, cloneChannelLinePrefix, sentence)
	}
}

// mustGit runs one read-only git command in dir and returns its stdout.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", full, err)
	}
	return string(out)
}

// TestEmptyGroundIsNothingToDeliverNotAnUndeliveredRun is the repro for bug
// 01a182a8: THE SECOND DELIVERY LEG did not look at blk.Kind, so the same
// `BlockerKindEmpty` the channel's first leg had already split off
// (cmd/agent/devworkspace_channel.go's nothingToDeliver) was folded here into
// tools.DeliveryDidNotLandReasonPref + blk.Report(). The end hook's
// `undelivered` predicate then wrote the run's OWN auto row back to
// `in_progress` and printed LEFT OPEN — one fact ("the ground carries no commit
// past the integration branch"), two opposite sentences, and a completed run
// left hanging.
//
// The premise is the measured one: an empty blocker whose OWN words already say
// "carries no commit to deliver". The assertion is on the row and the stream,
// because those are the two surfaces an operator reads.
func TestEmptyGroundIsNothingToDeliverNotAnUndeliveredRun(t *testing.T) {
	a, buf := cloneDeliveryAgent(t)
	a.maybeRegisterSessionTask("produce no change; verify the count is zero")
	rowID := a.autoTaskRowID
	if rowID == "" {
		t.Fatal("the start hook registered no row — the fixture premise is broken")
	}
	a.cloneGroundDir = t.TempDir()
	a.cloneGroundBranch = "master"
	a.cloneDeliveryAttempt = func(_ context.Context, _ devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error) {
		return devworkspace.Result{Dir: dir, Branch: "master"},
			&devworkspace.Blocker{
				Kind:  devworkspace.BlockerKindEmpty,
				Step:  "rev-list --count HEAD --not origin/master",
				Words: "carries no commit to deliver: `rev-list --count HEAD --not origin/master` answered 0 against origin/master",
			}, nil
	}

	a.finishSessionTask(0, 2, 50, 1.0)

	out := buf.String()
	if strings.Contains(out, "LEFT OPEN") {
		t.Fatalf("an empty ground must not leave the row open — nothing was owed:\n%s", out)
	}
	if strings.Contains(out, "delivery did not land") {
		t.Fatalf("an empty ground is a CONCLUSION; it must not wear the refused-delivery sentence:\n%s", out)
	}
	if !strings.Contains(out, cloneNothingToDeliverToken) {
		t.Fatalf("the run must report the outcome by name (%q):\n%s", cloneNothingToDeliverToken, out)
	}
	row, err := tools.TaskShow(rowID)
	if err != nil {
		t.Fatalf("task show: %v", err)
	}
	if !strings.Contains(row, "status:         "+tools.TaskStatusCompleted) {
		t.Fatalf("a run with nothing to deliver must stay `%s` — it produced the change it was asked for (none):\n%s",
			tools.TaskStatusCompleted, row)
	}
	if strings.Contains(row, "delivery did not land") {
		t.Fatalf("the row must NOT wear the refused-delivery sentence:\n%s", row)
	}
	if !strings.Contains(row, cloneNothingToDeliverToken) {
		t.Fatalf("the row's reason must say %q (Rule 15 — a verdict only on stderr is invisible to the next reader):\n%s",
			cloneNothingToDeliverToken, row)
	}
	// THE STUTTER'S SECOND BODY SOURCE: this sentence is formatted with
	// blk.Report(), which opens with the channel's prefix for EVERY Kind — so
	// before the fix the row read `clone channel: nothing to deliver —
	// clone channel: carries no commit …`. One prefix, counted (2026-10-30).
	// Read from the REASON COLUMN, not the trail: an empty ground records no
	// non-delivery event on purpose (reportCloneNothingToDeliver's note).
	assertChannelPrefixOnce(t, "the empty ground's row reason", cloneDeliveryRowReason(t, rowID))
}

// TestRealDeliveryFailuresLandTerminalFailed pins the OTHER half of the fork:
// the six kinds that really are refused deliveries land as a TERMINAL `failed`
// row carrying the non-delivery sentence — never as `completed` (the claim the
// proof would have refused), and never parked at `in_progress`.
//
// WHY THE CONTRACT IS NOT "LEAVE THE ROW OPEN" ANY MORE (bug 01a19639, measured
// 2026-11-01). The old arm wrote `in_progress` and returned, on the premise that
// a downstream pass would re-drive the delivery. MEASURED: there is no such
// pass. The lifecycle pass was retired 2026-10-30 and the `retry-delivery` verb
// with it (taskverbs.RemovedVerbs); `deliveryRetryOwedIDs` — the scan that would
// select exactly these rows — carries its own note "NO PRODUCTION CALLER".
// The observed consequence, on a real row:
//
//	$ gopi task status 01a19613-9019-71bc-80bf-0f450ec97ce5
//	status:      in_progress
//	verdict:     dead
//	evidence:    pid 1453624 is gone — the run recorded a clean end (exit 0,
//	             reason complete); the WORK may have finished and the row was
//	             never closed
//	resumable:   no
//
// `dead` + `resumable: no` + `in_progress` is the one combination nobody can act
// on: the dispatcher reads it running, `--resume` refuses it, and the reissue
// census lists `failed` rows only. It took a hand-typed
// `gopi task update <id> --status failed` 65 minutes later.
//
// `failed` is what the rest of the system already agrees with — not a delivery
// claim (so no proof arm disputes it), resumable (so the recovery route the open
// row preserved survives), and inside the reissue census. The non-delivery
// reason still rides the reason column, so nothing the operator needed is lost.
func TestRealDeliveryFailuresLandTerminalFailed(t *testing.T) {
	for _, kind := range []devworkspace.BlockerKind{
		devworkspace.BlockerKindDirty,
		devworkspace.BlockerKindPull,
		devworkspace.BlockerKindBuild,
		devworkspace.BlockerKindPush,
		devworkspace.BlockerKindMan,
		// THE DISK KIND IS A REFUSED DELIVERY TOO, and this is the assertion
		// that keeps it there: a full filesystem is not a code failure but it is
		// also not a delivery — the row must land `failed` carrying the
		// non-delivery sentence, exactly like the other refusals. It must never
		// drift into the nothing-to-deliver fork above, whose sentence would tell
		// the next reader the run had nothing to publish.
		devworkspace.BlockerKindDiskExhaustion,
	} {
		t.Run(string(kind), func(t *testing.T) {
			a, buf := cloneDeliveryAgent(t)
			a.maybeRegisterSessionTask("deliver the clone ground's commits")
			rowID := a.autoTaskRowID
			a.cloneGroundDir = t.TempDir()
			a.cloneGroundBranch = "master"
			a.cloneDeliveryAttempt = func(_ context.Context, _ devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error) {
				return devworkspace.Result{Dir: dir, Branch: "master"},
					&devworkspace.Blocker{Kind: kind, Words: "a step refused the delivery"}, nil
			}

			a.finishSessionTask(0, 2, 50, 1.0)

			out := buf.String()
			if !strings.Contains(out, "LEFT OPEN") {
				t.Fatalf("a %q refusal must SAY the claim was not written (the operator line is where the WHY lives):\n%s", kind, out)
			}
			if !strings.Contains(out, tools.TaskStatusFailed) {
				t.Fatalf("a %q refusal must land the row %s, not leave it parked:\n%s", kind, tools.TaskStatusFailed, out)
			}
			if !strings.Contains(out, tools.DeliveryDidNotLandReasonPref) {
				t.Fatalf("a %q refusal must carry the non-delivery sentence:\n%s", kind, out)
			}
			if strings.Contains(out, cloneNothingToDeliverToken) {
				t.Fatalf("a %q refusal must NOT be reported as nothing-to-deliver — that would relax a real failure:\n%s", kind, out)
			}
			row, _ := tools.TaskShow(rowID) //stylegate:ignore ledger read after the run; the compared row is the contract
			if !strings.Contains(row, "status:         "+tools.TaskStatusFailed) {
				t.Fatalf("a %q refusal must land the row %s — an `in_progress` row with a dead holder is the shape no dispatcher, no resume and no census can act on (bug 01a19639):\n%s",
					kind, tools.TaskStatusFailed, row)
			}
			if !strings.Contains(row, tools.DeliveryDidNotLandReasonPref) {
				t.Fatalf("the row must carry the non-delivery reason for %q:\n%s", kind, row)
			}
			// The terminal-write WITNESS must exist for these rows too. The old
			// arm `return`ed before recordTaskRowClosed, so a session in which the
			// hook decided "leave it open" and a session in which the hook never
			// ran were INDISTINGUISHABLE from the transcript — measured on the real
			// row: `grep -c task_row_closed sessions/659b645b-…/session.jsonl` = 0.
			if got := countSessionEvents(a, "task_row_closed"); got == 0 {
				t.Fatalf("a %q refusal must still record the terminal-write witness (a non-close with no witness cannot be told apart from a hook that never ran):\n%s", kind, out)
			}
			// EVERY KIND'S BODY, ONE PREFIX (fixed 2026-10-30): blk.Report()
			// opens each of these kinds differently — dirty/pull/build/push/man
			// with `clone channel: the <step> step refused`, disk-exhaustion with
			// its own sentence — and every one of them used to be concatenated
			// under a second copy of the same prefix.
			assertChannelPrefixOnce(t, "the operator's BLOCKED line for "+string(kind), blockedLine(t, out))
			assertChannelPrefixOnce(t, "the row's non-delivery reason for "+string(kind),
				cloneDeliveryTrailDetail(t, rowID, tools.DeliveryDidNotLandEventName))
		})
	}
}

// countSessionEvents reports how many events of the given type the agent's own
// session JSONL carries. It exists for the terminal-write witness assertion
// above: a session log is the ONLY place that distinguishes "the end hook
// decided not to close the row" from "the end hook never reached the ledger".
//
// It SYNCS first: the logger batches events and flushes at a round boundary /
// close (session.go's log + Sync), so a test that read the file directly would
// measure the flush schedule rather than the hook — the same class of mistake as
// reading a buffered pipe.
func countSessionEvents(a *Agent, eventType string) int {
	if a == nil || a.SessionLog == nil {
		return 0
	}
	// The failure is the answer: an unsyncable log carries no readable witness,
	// so a failed flush reports 0 (what the caller treats as a missing witness)
	// instead of being discarded and then read as if the buffer had landed.
	if err := a.SessionLog.Sync(); err != nil {
		return 0
	}
	data, err := os.ReadFile(a.SessionLog.Path())
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"`+eventType+`"`) {
			n++
		}
	}
	return n
}

// TestAlreadyDeliveredGroundIsItsOwnCompletion is the SECOND face's half of row
// 01a182df: the same `rev-list --count` == 0, the opposite fact. This leg used
// to fold any non-empty blocker into tools.DeliveryDidNotLandReasonPref, so a
// run whose OWN push had already moved the integration base onto its tip wore
// the refused-delivery sentence — the end hook's `undelivered` predicate then
// left the row open, and the operator read "the delivery did not land" about
// work sitting on the integration branch.
//
// The premise is the measured one: the blocker's OWN words say the count is 0
// BECAUSE the delivery landed (`BlockerKindAlreadyDelivered`). Assertions are on
// the row and the stream, the two surfaces an operator reads, plus the ABSENCE
// of both opposite sentences (Rule 15: the two zeroes never share a wording).
func TestAlreadyDeliveredGroundIsItsOwnCompletion(t *testing.T) {
	a, buf := cloneDeliveryAgent(t)
	a.maybeRegisterSessionTask("deliver the work")
	rowID := a.autoTaskRowID
	if rowID == "" {
		t.Fatal("the start hook registered no row — the fixture premise is broken")
	}
	a.cloneGroundDir = t.TempDir()
	a.cloneGroundBranch = "master"
	a.cloneDeliveryAttempt = func(_ context.Context, _ devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error) {
		return devworkspace.Result{Dir: dir, Branch: "master"},
			&devworkspace.Blocker{
				Kind:  devworkspace.BlockerKindAlreadyDelivered,
				Step:  "rev-list --count HEAD --not origin/master",
				Words: "carries no commit to deliver — but this run's delivery already landed (abc1234): the tip this run pushed is reachable from master",
			}, nil
	}

	a.finishSessionTask(0, 2, 50, 1.0)

	out := buf.String()
	if strings.Contains(out, "LEFT OPEN") {
		t.Fatalf("an already-delivered ground must not leave the row open — nothing was owed:\n%s", out)
	}
	if strings.Contains(out, "delivery did not land") {
		t.Fatalf("the delivery DID land; it must not wear the refused-delivery sentence:\n%s", out)
	}
	if !strings.Contains(out, cloneAlreadyDeliveredToken) {
		t.Fatalf("the run must report the outcome by its own name (%q):\n%s", cloneAlreadyDeliveredToken, out)
	}
	row, err := tools.TaskShow(rowID)
	if err != nil {
		t.Fatalf("task show: %v", err)
	}
	if !strings.Contains(row, "status:         "+tools.TaskStatusCompleted) {
		t.Fatalf("a run whose delivery already landed must stay `%s`:\n%s", tools.TaskStatusCompleted, row)
	}
	if strings.Contains(row, "delivery did not land") {
		t.Fatalf("the row must NOT wear the refused-delivery sentence:\n%s", row)
	}
	if strings.Contains(row, cloneNothingToDeliverToken) {
		t.Fatalf("the row must NOT claim the run produced nothing — it produced the delivered work (Rule 15):\n%s", row)
	}
	if !strings.Contains(row, cloneAlreadyDeliveredToken) {
		t.Fatalf("the row's reason must say %q (Rule 15 — a verdict only on stderr is invisible to the next reader):\n%s",
			cloneAlreadyDeliveredToken, row)
	}
	// THE STUTTER, FIFTH ARM: a body from blk.Report() — prefixed by every Kind —
	// must appear behind this sentence's prefix exactly once (2026-10-30).
	assertChannelPrefixOnce(t, "the already-delivered row reason", cloneDeliveryRowReason(t, rowID))
}

// TestInheritedDeliveryIsItsOwnOutcomeNotAnUndeliveredRun pins the THIRD meaning
// of the same `count == 0` on the SECOND delivery leg (epic: inherited
// delivery): the integration branch already CONTAINS this ground's HEAD, so the
// content this run was handed is on the base. The pre-fix leg had no case for
// it — `case blk != nil` folded it into the refused delivery, the end hook wrote
// the row back to `in_progress` with the "delivery did not land" prefix and
// printed LEFT OPEN.
//
// The assertions are the three surfaces an operator reads (the stream, the row's
// status, the row's reason), plus the one thing that must NOT appear: the
// already-delivered token, because THIS run pushed nothing.
func TestInheritedDeliveryIsItsOwnOutcomeNotAnUndeliveredRun(t *testing.T) {
	a, buf := cloneDeliveryAgent(t)
	a.maybeRegisterSessionTask("continue the work the integration branch already contains")
	rowID := a.autoTaskRowID
	if rowID == "" {
		t.Fatal("the start hook registered no row — the fixture premise is broken")
	}
	a.cloneGroundDir = t.TempDir()
	a.cloneGroundBranch = "master"
	a.cloneDeliveryAttempt = func(_ context.Context, _ devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error) {
		return devworkspace.Result{Dir: dir, Branch: "master"},
			&devworkspace.Blocker{
				Kind: devworkspace.BlockerKindInheritedDelivery,
				Step: "rev-list --count HEAD --not origin/master",
				Words: "carries no commit to deliver — 39bd44a is contained by origin/master, so " +
					"the content this run was handed is already on the integration branch",
			}, nil
	}

	a.finishSessionTask(0, 2, 50, 1.0)

	out := buf.String()
	if strings.Contains(out, "LEFT OPEN") {
		t.Fatalf("an inherited delivery must not leave the row open — nothing was owed:\n%s", out)
	}
	if strings.Contains(out, tools.DeliveryDidNotLandReasonPref) {
		t.Fatalf("the content IS on the integration branch — the refused-delivery prefix is a false claim:\n%s", out)
	}
	if !strings.Contains(out, cloneInheritedDeliveryToken) {
		t.Fatalf("the run must report the outcome by name (%q):\n%s", cloneInheritedDeliveryToken, out)
	}
	if strings.Contains(out, cloneAlreadyDeliveredToken) {
		t.Fatalf("this run pushed nothing, so it must not claim its own push landed:\n%s", out)
	}

	row, err := tools.TaskShow(rowID)
	if err != nil {
		t.Fatalf("task show: %v", err)
	}
	if !strings.Contains(row, "status:         "+tools.TaskStatusCompleted) {
		t.Fatalf("a run whose content is already on the base is `%s`:\n%s", tools.TaskStatusCompleted, row)
	}
	if strings.Contains(row, tools.DeliveryDidNotLandReasonPref) {
		t.Fatalf("the row must NOT wear the refused-delivery sentence:\n%s", row)
	}
	if !strings.Contains(row, cloneInheritedDeliveryToken) {
		t.Fatalf("the row's reason must say %q (Rule 15 — a verdict only on stderr is invisible to the next reader):\n%s",
			cloneInheritedDeliveryToken, row)
	}
	// THE STUTTER, SIXTH ARM (2026-10-30): same body source, same one-prefix rule.
	assertChannelPrefixOnce(t, "the inherited-delivery row reason", cloneDeliveryRowReason(t, rowID))
}

// TestAlreadyDeliveredArmRecordsTheLandedTip pins the ONE face this package owns
// of the defect cmd/agent's loop was filed on: the ALREADY-DELIVERED arm knows a
// push of this ground landed, so the row must carry that push's tip.
//
// IT DRIVES THE REAL BRANCH, NOT THE HELPER. The first version of this test
// called `recordCloneLandedAlreadyDelivered` directly and therefore stayed GREEN
// when the CALL was deleted from the arm — a test that exercises a function
// nobody reaches proves nothing about the product (measured: call disabled, test
// still `ok`). So the delivery attempt is stubbed (`a.cloneDeliveryAttempt`, the
// seam the other end-hook tests already use) to return the already-delivered
// blocker, and the assertion is on what the SHIPPING reader reports afterwards.
//
// RED WHEN THE CALL IS REMOVED: `tools.TaskLandedDeliveryTip` answers "" that
// way, which is exactly the state that makes the empty gate fall back to
// "carries no commit to deliver" for a ground that DID deliver.
func TestAlreadyDeliveredArmRecordsTheLandedTip(t *testing.T) {
	a, _ := cloneDeliveryAgent(t)
	a.maybeRegisterSessionTask("deliver the work")
	rowID := a.autoTaskRowID
	if rowID == "" {
		t.Fatal("the start hook registered no row — the fixture premise is broken")
	}

	// A REAL ground holding ONE commit: the smallest sample (Rule 10.4).
	ground := testutil.TestDeviceScratchDir(t, "already-delivered-ground")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", ground}, args...)...)
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(ground, 0o755); err != nil {
		t.Fatalf("ground: %v", err)
	}
	git("init", "-q", "-b", devworkspace.DefaultBranch)
	if err := os.WriteFile(filepath.Join(ground, "delivered.txt"), []byte("the work\n"), 0o644); err != nil {
		t.Fatalf("ground file: %v", err)
	}
	git("add", "delivered.txt")
	git("commit", "-q", "-m", "feat: the work this ground delivered")
	tip := git("rev-parse", "HEAD")

	// PRODUCTION'S SHAPE: the ground stands on the row's own address and that
	// address is what the record names (`devworkspace.go:690`/`:820` stamp the
	// ground's branch, and the reader admits only the row's own addresses —
	// measured: a `master` branch makes the writer succeed and the READER refuse).
	a.cloneGroundDir = ground
	a.cloneGroundBranch = tools.TaskGroundBranchName(rowID)
	a.cloneDeliveryAttempt = func(_ context.Context, _ devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error) {
		return devworkspace.Result{Dir: dir, Branch: tools.TaskGroundBranchName(rowID)},
			&devworkspace.Blocker{
				Kind:  devworkspace.BlockerKindAlreadyDelivered,
				Step:  "rev-list --count HEAD --not origin/master",
				Words: ground + " carries no commit to deliver — but this run's delivery already landed (" + tip[:8] + ")",
			}, nil
	}

	a.finishSessionTask(0, 2, 50, 1.0)

	// THE ASSERTION IS ON THE SHIPPING READER — what the NEXT judge of this row
	// will consult, possibly in another process.
	if got := tools.TaskLandedDeliveryTip(rowID, ""); got != tip {
		t.Fatalf("the already-delivered arm left no usable landed record for its own tip\n"+
			"  want tip %s\n  got      %q\n"+
			"WITHOUT it the empty gate falls back to \"carries no commit to deliver\" for a ground that DID deliver.",
			tip, got)
	}
}

// TestAlreadyDeliveredArmSaysWhenTheTipCannotBeRead is the Rule 15 half, driven
// through the same real branch: a delivery that landed but whose tip is
// unreadable must leave a NAMED sentence in the run's own log rather than
// silently recording nothing.
func TestAlreadyDeliveredArmSaysWhenTheTipCannotBeRead(t *testing.T) {
	a, buf := cloneDeliveryAgent(t)
	a.maybeRegisterSessionTask("deliver the work")
	rowID := a.autoTaskRowID
	if rowID == "" {
		t.Fatal("the start hook registered no row — the fixture premise is broken")
	}
	// A directory that is NOT a git repository: CloneDeliveryTip answers "".
	notARepo := testutil.TestDeviceScratchDir(t, "already-delivered-noground")
	a.cloneGroundDir = notARepo
	a.cloneGroundBranch = tools.TaskGroundBranchName(rowID)
	a.cloneDeliveryAttempt = func(_ context.Context, _ devworkspace.Config, dir string) (devworkspace.Result, *devworkspace.Blocker, error) {
		return devworkspace.Result{Dir: dir, Branch: tools.TaskGroundBranchName(rowID)},
			&devworkspace.Blocker{
				Kind:  devworkspace.BlockerKindAlreadyDelivered,
				Step:  "rev-list --count HEAD --not origin/master",
				Words: notARepo + " carries no commit to deliver",
			}, nil
	}

	a.finishSessionTask(0, 2, 50, 1.0)

	out := buf.String()
	// IT NAMES THE DIRECTORY IT COULD NOT READ (the run's own log, not stderr).
	if !strings.Contains(out, notARepo) {
		t.Fatalf("an unreadable tip must be said out loud, naming the directory:\n%s", out)
	}
	// AND IT RECORDED NOTHING — an unreadable tip must never be filed as a
	// delivery of some other commit.
	if got := tools.TaskLandedDeliveryTip(rowID, ""); got != "" {
		t.Fatalf("an unreadable tip must record NOTHING, got %q", got)
	}
}
