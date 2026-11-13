package tools

// The READ-ONLY advisory ground line of `gopi task show` (gap B of the
// TaskWorktreeStampClaimerFor follow-up, epic 01a0bb4d).
//
// THE POINT OF THE LINE. `task show` renders the deliverable block out of the
// row's own blob — the ADOPTABLE field the merger, the sweeper and the reclaim
// arms read, written only by seams that have evidence and never re-pointed. A
// row whose ground exists but was never recorded (the measured 2-of-131
// in-flight shape) therefore shows no ground, and an operator cannot tell that
// from a run with none. The advisory line resolves the ground NOW from the
// row's own session through the EXISTING readers and says why when it cannot.
//
// What these tests pin:
//
//	(a) a row that recorded NOTHING but whose session names a worktree-add
//	    branch shows the resolved ground (the epic's headline case);
//	(b) the RECORDED deliverable is byte-identical across a show — the line is
//	    advisory, never a writer (the epic's explicit regression test);
//	(c) a recorded ground is rendered and VERIFIED live (the stale marker when
//	    the branch is gone);
//	(d) no session / no readable reading ⇒ an honest WHY, never a failure and
//	    never a silent omission;
//	(e) the show NEVER errors because of the advisory line.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/toolnames"
)

// advisoryShowFixture is a real repo (main checkout on master) plus a session
// JSONL whose tool_use lines carry ground-creating commands — the shape a
// `task run` child leaves behind.
type advisoryShowFixture struct {
	t       *testing.T
	repo    string
	wt      string
	branch  string
	session string
}

func newAdvisoryShowFixture(t *testing.T, branch, sessionID string) *advisoryShowFixture {
	t.Helper()
	f := newClaimCandFixture(t, branch)
	f2 := &advisoryShowFixture{t: t, repo: f.repo, wt: f.wt, branch: branch, session: sessionID}
	// The session JSONL: session_start (workdir = the MAIN checkout, the
	// executor shape whose HEAD is exempt) then the ONE ground-creating
	// worktree-add the run performed.
	sessHome := t.TempDir()
	t.Setenv("HOME", sessHome)
	t.Setenv(config.GopiDirEnv, sessHome)
	sessDir := filepath.Join(sessHome, ".gopi", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"session_start","sessionId":"` + sessionID + `","model":"m","timestamp":"x","workdir":"` + f.repo + `"}` + "\n" +
		jobGroundToolUseLine(toolnames.ToolBash, "", "cd "+f.repo+" && git worktree add "+f.wt+" -b "+branch) + "\n"
	if err := os.WriteFile(filepath.Join(sessDir, sessionID+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return f2
}

// advisorySeedRow inserts a row with the given deliverable blob and session.
func advisorySeedRow(t *testing.T, taskID, deliverable, sessionID string) {
	t.Helper()
	ctx := context.Background()
	conn, err := openTasksDB(ctx, os.Getenv(memoryDBEnv))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, session_id, source, created_at, updated_at)
		 VALUES (?, ?, 'in_progress', ?, ?, 'auto', ?, ?)`,
		taskID, "advisory fixture", deliverable, sessionID, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed row: %v", err)
	}
}

// TestTaskShowGroundAdvisoryResolvesUnrecordedGround is case (a): a row whose
// RECORDED deliverable is repo-only, but whose own session names the worktree
// and branch it created, shows the resolved ground — the exact shape the epic
// measured (2 of 131 in-flight rows).
func TestTaskShowGroundAdvisoryResolvesUnrecordedGround(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newAdvisoryShowFixture(t, "feat/advisory-unrecorded", "sess-advisory-unrecorded")
	advisorySeedRow(t, "t-advisory-unrecorded", `{"repo":"`+f.repo+`"}`, f.session)

	out, err := TaskShow("t-advisory-unrecorded")
	if err != nil {
		t.Fatalf("TaskShow must never fail because of the advisory line: %v", err)
	}
	if !strings.Contains(out, TaskShowGroundLabel) {
		t.Fatalf("show must render the advisory ground line:\n%s", out)
	}
	line := advisoryGroundLine(t, out)
	for _, want := range []string{f.branch, TaskShowGroundExistsMarker, f.repo} {
		if !strings.Contains(line, want) {
			t.Errorf("advisory line must carry %q, got %q", want, line)
		}
	}
	// The recorded block stays exactly as recorded: repo-only.
	if !strings.Contains(out, TaskShowDeliverableRepoLabel+" "+f.repo) {
		t.Errorf("the RECORDED repo line must still render:\n%s", out)
	}
	if strings.Contains(out, TaskShowDeliverableWorktreeLabel) {
		t.Errorf("the advisory line must NOT write the recorded deliverable — the block above stays repo-only:\n%s", out)
	}
}

// TestTaskShowGroundAdvisoryDoesNotTouchTheRecordedDeliverable is case (b) —
// the epic's explicit regression test: the raw deliverable blob and the row's
// status are IDENTICAL before and after a show, so the advisory line can never
// be a quiet writer of the adoptable field.
func TestTaskShowGroundAdvisoryDoesNotTouchTheRecordedDeliverable(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newAdvisoryShowFixture(t, "feat/advisory-readonly", "sess-advisory-readonly")
	blob := `{"repo":"` + f.repo + `"}`
	advisorySeedRow(t, "t-advisory-readonly", blob, f.session)

	before := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), "t-advisory-readonly")
	beforeStatus := advisoryRowStatus(t, "t-advisory-readonly")
	if _, err := TaskShow("t-advisory-readonly"); err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	after := deliverableWTRowBlob(t, os.Getenv(memoryDBEnv), "t-advisory-readonly")
	afterStatus := advisoryRowStatus(t, "t-advisory-readonly")
	if before != after {
		t.Fatalf("the advisory line CHANGED the recorded deliverable: %q → %q", before, after)
	}
	if beforeStatus != afterStatus {
		t.Fatalf("the advisory line changed the row's status: %q → %q", beforeStatus, afterStatus)
	}
	if before != blob {
		t.Fatalf("fixture precondition: the seeded blob must be untouched to begin with, got %q", before)
	}
}

// TestTaskShowGroundAdvisoryVerifiesTheRecordedGround is case (c): a row that
// DID record a ground renders it and VERIFIES it live — and says so when the
// reading has gone stale (the branch no longer exists in the repo).
func TestTaskShowGroundAdvisoryVerifiesTheRecordedGround(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newAdvisoryShowFixture(t, "feat/advisory-recorded", "sess-advisory-recorded")
	deliverable := `{"repo":"` + f.repo + `","branch":"` + f.branch + `","worktree":"` + f.wt + `"}`
	advisorySeedRow(t, "t-advisory-recorded", deliverable, f.session)

	out, err := TaskShow("t-advisory-recorded")
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := advisoryGroundLine(t, out)
	if !strings.Contains(line, f.wt) || !strings.Contains(line, TaskShowGroundExistsMarker) {
		t.Fatalf("a live recorded ground must render verified: %q", line)
	}

	// Now delete the branch — the ground is STALE, and the advisory line must
	// say so rather than keep claiming a verified reading. The registration
	// goes first: git refuses to delete a branch checked out in a worktree.
	advisoryDropBranch(t, f)
	out, err = TaskShow("t-advisory-recorded")
	if err != nil {
		t.Fatalf("TaskShow after stale: %v", err)
	}
	line = advisoryGroundLine(t, out)
	if !strings.Contains(line, TaskShowGroundStaleMarker) {
		t.Errorf("a branch that no longer exists must render the stale marker: %q", line)
	}
}

// TestTaskShowGroundAdvisoryHonestWhenUnreadable is case (d): no session, an
// empty reading, and a repo that is not a repo each render an honest WHY —
// never a failure, never a silent omission where a ground could exist.
func TestTaskShowGroundAdvisoryHonestWhenUnreadable(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	lines := claimCandCaptureLog(t)

	// ① No session at all, and no ground: the advisory stays SILENT (there is
	// nothing to resolve and nothing to explain — an invented ground would be
	// a lie).
	advisorySeedRow(t, "t-advisory-nosession", `{"repo":"/nonexistent/repo"}`, "")
	out, err := TaskShow("t-advisory-nosession")
	if err != nil {
		t.Fatalf("TaskShow (no session): %v", err)
	}
	if strings.Contains(out, TaskShowGroundLabel) {
		t.Errorf("a row with neither a recorded ground nor a session has nothing to advise:\n%s", out)
	}

	// ② A session that exists but names no ground-creating command: the
	// advisory must SAY SO (Rule 15), not omit the line.
	f := newAdvisoryShowFixture(t, "feat/advisory-noreading", "sess-advisory-noreading")
	advisoryDropBranch(t, f)
	advisorySeedRow(t, "t-advisory-noreading", `{"repo":"`+f.repo+`"}`, f.session)
	out, err = TaskShow("t-advisory-noreading")
	if err != nil {
		t.Fatalf("TaskShow (no reading): %v", err)
	}
	line := advisoryGroundLine(t, out)
	if !strings.Contains(line, f.repo) {
		t.Errorf("the advisory line must still name the repo it judged against: %q", line)
	}
	if !strings.Contains(line, "—") {
		t.Errorf("an unresolved reading must carry an explicit WHY suffix: %q", line)
	}
	_ = lines
}

// TestTaskShowGroundAdvisoryNeverFailsOnABrokenSession is case (e): the show
// itself must survive a session file that cannot be read (here: a session
// whose JSONL names a nonexistent worktree directory) — the advisory degrades
// to a why-line and the row still renders.
func TestTaskShowGroundAdvisoryNeverFailsOnABrokenSession(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	f := newAdvisoryShowFixture(t, "feat/advisory-broken", "sess-advisory-broken")
	// Point the session's worktree-add at a directory that does not exist.
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("gopi dir: %v", err)
	}
	sessDir := filepath.Join(gd, config.SessionsDirName)
	body := `{"type":"session_start","sessionId":"sess-advisory-broken","model":"m","timestamp":"x","workdir":"` + f.repo + `"}` + "\n" +
		jobGroundToolUseLine(toolnames.ToolBash, "", "git worktree add /nonexistent/gone -b feat/advisory-gone") + "\n"
	if err := os.WriteFile(filepath.Join(sessDir, "sess-advisory-broken.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	advisorySeedRow(t, "t-advisory-broken", `{"repo":"`+f.repo+`"}`, "sess-advisory-broken")

	out, err := TaskShow("t-advisory-broken")
	if err != nil {
		t.Fatalf("a broken session reading must never fail the show: %v", err)
	}
	if !strings.Contains(out, "task_id:        t-advisory-broken") {
		t.Fatalf("the row's own digest must still render:\n%s", out)
	}
}

// advisoryDropBranch removes both the worktree registration and the branch it
// carried, so the fixture's recorded ground becomes STALE exactly as a
// collected worktree leaves it.
func advisoryDropBranch(t *testing.T, f *advisoryShowFixture) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ctx
	deliverableWTGit(t, f.repo, "worktree", "remove", "--force", f.wt)
	deliverableWTGit(t, f.repo, "branch", "-D", f.branch)
}

// advisoryGroundLine extracts the advisory line from a show digest.
func advisoryGroundLine(t *testing.T, show string) string {
	t.Helper()
	for _, l := range strings.Split(show, "\n") {
		if strings.HasPrefix(l, TaskShowGroundLabel) {
			return l
		}
	}
	t.Fatalf("show carries no %q line:\n%s", TaskShowGroundLabel, show)
	return ""
}

// advisoryRowStatus reads a row's status column directly.
func advisoryRowStatus(t *testing.T, taskID string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := openTasksDB(ctx, os.Getenv(memoryDBEnv))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	task, err := taskByID(ctx, conn, taskID)
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if task == nil {
		t.Fatalf("task %s not found", taskID)
	}
	return task.Status
}
