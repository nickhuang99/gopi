package main

// Regression (epic resume-session-target-latch): `gopi session resume <session-id>`
// must NOT adopt a ledger row keyed by the session uuid.
//
// THE DEFECT, reproduced through the REAL binary before the fix. A bare
// session's run directory is named by its own session uuid
// (<gopi dir>/sessions/<session-uuid>/session.jsonl plus the co-located raw
// log), so a SESSION-UUID target resolves through the run-directory branch of
// resolveResumeTarget — a filesystem fact that says nothing about the ledger.
// The branch nevertheless reported "this is a task id", runResume adopted the
// uuid as a row, Agent.AdoptLedgerRow reported success, the start hook's
// latchWrapperRow() then returned true and suppressed the run's own auto row,
// and the run leaked exactly three [gopi] stderr lines over a row that cannot
// exist:
//
//	[gopi] task ledger: adopted row <sid> for this run (…)
//	[gopi] resume: warning: task linkage failed (<sid> ← <new-sid>): task: no task with task_id="<sid>"
//	[gopi] task ledger: close failed (continuing): task: no task with task_id="<sid>"
//
// and ended with NO ledger record for the resumed run at all — the adopted row
// is the session uuid (which never exists) and the auto mirror was suppressed.
//
// THE FIX: adoption is gated on resolution through the ROW (task-id) view, and
// a non-resolving row is reported as a DECISION instead of latched.
//
// RED BEFORE THE FIX: rebuild bin/gopi at the parent commit (or revert the
// resumeTargetRowID gate in resolveResumeTarget) and
// TestResumeSessionIDTargetAdoptsNoRow fails on the three leaked lines; the
// task-id half (TestResumeTaskIDTargetStillAdopts) passes on both sides, which
// is what makes this a regression guard rather than a change of behaviour.
// A STALE binary makes it pass falsely — rebuild first (the trap memory #2988
// records for this area).
//
// Zero real API calls: the mock LLM answers each round with plain text. All
// fixtures are sandboxed (temp GOPI_HOME + temp ledger, off the / partition —
// Rule 22).

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// resumeLatchSessionID is the uuid the operator's report resumes: a
	// SESSION id, never a task id. Its run directory is what its own session
	// created, so it resolves from the filesystem alone.
	resumeLatchSessionID = "da892a10-ab50-4c21-8295-b1291f31cd00"

	// resumeLatchRowID is the fixture ROW's id (a task id) — the shape the
	// task-target case resumes.
	resumeLatchRowID = "t-resume-latch-row"

	// The three stderr lines the pre-fix binary leaks for a session-only
	// target. Each is matched by the CONSTANT fragment the emitter owns, so a
	// rewording of the surrounding prose does not silently disarm this test.
	resumeLatchLeakAdopted = "task ledger: adopted row"
	resumeLatchLeakLinkage = "task linkage failed"
	resumeLatchLeakClose   = "task ledger: close failed"

	// resumeLatchNoRowNote is the honest note the fix emits instead: the
	// absence of a row is a DECISION, reported, never an error.
	resumeLatchNoRowNote = "names no ledger row"

	// resumeLatchContinuation is the continuation text handed to the resume —
	// any non-empty instruction; the mock answers it.
	resumeLatchContinuation = "continue: report the status\n"
)

// resumeLatchSandbox is a sandboxed profile for one case: a temp GOPI_HOME
// whose OWN ledger (the profile's memory.db — the file the child resolves
// through GOPI_HOME) is the ledger both sides use, plus a mock LLM.
//
// ONE LEDGER, TWO PINNED SIDES: the test process writes the fixture row
// through GOPI_MEMORY_DB, the child resolves it through GOPI_HOME, and both
// name the same file. Pinning only one side is the trap the neighbouring
// resume fixtures document — the writer lands in the suite's shared ledger
// while the child reads the sandbox's, and the row view fails for a reason
// that has nothing to do with the code under test.
func resumeLatchSandbox(t *testing.T) (gopiHome string, env []string) {
	t.Helper()
	gopiHome = t.TempDir()
	dbPath := filepath.Join(gopiHome, config.ProfileDirName, config.MemoryDBFileName)
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "reply ok"})
	t.Cleanup(mock.Close)
	t.Setenv(config.MemoryDBEnv, dbPath)
	env = testutil.MockEnv(t, gopiHome, mock.URL())
	return gopiHome, append(env, config.MemoryDBEnv+"="+dbPath)
}

// resumeLatchRunDir is the SESSION's own run directory under the sandbox
// profile — the address the run path mints and the resolver reads
// (internal/agent/session.go sessionDirForID for a bare session).
func resumeLatchRunDir(gopiHome, runID string) string {
	return filepath.Join(gopiHome, config.ProfileDirName, config.SessionsDirName, runID)
}

// seedSessionRunLog materialises a run directory the way the run path does:
// the raw log (the CHECKPOINT) plus the session.jsonl that RECORDS it. Both
// halves matter and for different reasons — the record is what makes the
// ledger's row view (tools.TaskResumeTarget → rundir.WithCheckpoint) able to
// answer for the row, and the raw log is what a resume actually loads. A
// fixture with only the raw log exercises the filesystem branch alone (the
// session-only case); one with both exercises the ROW branch.
func seedSessionRunLog(t *testing.T, gopiHome, runID string) {
	t.Helper()
	dir := resumeLatchRunDir(gopiHome, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	raw := filepath.Join(dir, runID+scratch.SessionRawLogFileSuffix)
	if err := os.WriteFile(raw, []byte("→ REQUEST\n"+checkpointBody()+"\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal raw path: %v", err)
	}
	lines := []string{
		`{"type":"session_start","sessionId":"` + runID + `","model":"deepseek-v4-flash","rawLogPath":` + string(rawJSON) + `,"timestamp":"2026-09-01T00:00:00Z"}`,
		`{"type":"session_end","elapsed_sec":1.0,"total_iterations":2,"total_tokens":20,"exit_code":0}`,
	}
	if err := os.WriteFile(filepath.Join(dir, runID+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write session log: %v", err)
	}
}

// resumeLatchContinuationFile writes the continuation file the resume reads.
func resumeLatchContinuationFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cont.txt")
	if err := os.WriteFile(p, []byte(resumeLatchContinuation), 0o644); err != nil {
		t.Fatalf("write continuation: %v", err)
	}
	return p
}

// resumeLatchFixtureRepo creates the fixture's OWN deliverable repo: a throwaway
// work tree under this test's temp dir, carrying one commit on `master`.
//
// WHY IT EXISTS: the row's deliverable.repo is what the resumed child's close-out
// resolves as the MAIN CHECKOUT to restore, and the restore's whole job is
// `git checkout <integration>` in it (FinishCleanupMainCheckoutRestore). A row
// seeded with no repo takes the submitter's cwd — i.e. the checkout this suite is
// running in — and the close-out then moves THAT. One commit on master is what
// makes the fixture a real target for that step rather than a directory git
// refuses, so the close-out path stays exercised end to end.
func resumeLatchFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	testGit(t, repo, "init", "-q", "-b", masterBranchName)
	testGit(t, repo, "config", "user.email", resumeLatchFixtureGitEmail)
	testGit(t, repo, "config", "user.name", resumeLatchFixtureGitName)
	if err := os.WriteFile(filepath.Join(repo, resumeLatchFixtureRepoFile), []byte(resumeLatchFixtureRepoBody), 0o644); err != nil {
		t.Fatalf("write the fixture ground's file: %v", err)
	}
	testGit(t, repo, "add", resumeLatchFixtureRepoFile)
	testGit(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-m", resumeLatchFixtureRepoSubject)
	return repo
}

const (
	// resumeLatchFixtureRepoFile / resumeLatchFixtureRepoBody are the fixture
	// ground's only content: the fixture's value is that NOBODY ELSE writes
	// there, and this file is what carries that one commit.
	resumeLatchFixtureRepoFile    = "ground.txt"
	resumeLatchFixtureRepoBody    = "resume-latch fixture ground — never the tree under test (Rule 6)\n"
	resumeLatchFixtureRepoSubject = "fixture ground for the resume-latch row"
	// resumeLatchFixtureGitName / resumeLatchFixtureGitEmail are the identity the
	// fixture's commit uses: a fresh temp repo has none, and an inherited global
	// one is not guaranteed (a commit that fails for that reason would read as a
	// different defect).
	resumeLatchFixtureGitName  = "gopi-resume-fixture"
	resumeLatchFixtureGitEmail = "resume-fixture@gopi.invalid"
	// masterBranchName is the integration branch the fixture declares, so the
	// close-out's restore has a real destination.
	masterBranchName = "master"
)

// TestResumeCloseoutDoesNotMoveTheTreeUnderTest is the Rule 29 regression for the
// defect this file's seed helper was fixed for (measured 2026-10-28): a resumed
// child's close-out moved the SUITE'S OWN checkout off its branch.
//
// THE MEASURED SHAPE, before the pin: the row carried the submitter's cwd as its
// deliverable.repo (the clone ground a worker drives this suite in — or, for an
// operator, `~/work/gopi`), FinishCleanupRestoreMainCheckout resolved that as the
// main checkout to restore, and the child ran `git checkout master` in it. The
// ground's reflog recorded `checkout: moving from task/<id> to master` three
// times across two runs (trapped by a post-checkout hook that named
// `gopi session resume` as the ancestor of the `git checkout`).
//
// The assertion is the CHECKOUT IT RUNS IN, not the fixture: the tree under test
// must still be on the branch it was on when the case started. It is deliberately
// read BEFORE and AFTER the same resume the neighbouring cases drive, so a fix
// that merely changed an assertion elsewhere cannot pass it.
func TestResumeCloseoutDoesNotMoveTheTreeUnderTest(t *testing.T) {
	checkout := resumeSuiteCheckout(t)
	branchBefore := resumeCheckoutBranch(t, checkout)
	headBefore := resumeCheckoutHead(t, checkout)

	gopiHome, env := resumeLatchSandbox(t)
	seedRowIntoProfile(t, gopiHome, resumeLatchRowID)
	seedSessionRunLog(t, gopiHome, resumeLatchRowID)

	if _, stderr, code := runGopiMapped(t,
		[]string{CliVerbSession, CliVerbResume, resumeLatchRowID, resumeLatchContinuationFile(t)}, "", env, ""); code != 0 {
		t.Fatalf("a task-id resume must succeed: code=%d\nstderr:\n%s", code, stderr)
	}

	if got := resumeCheckoutBranch(t, checkout); got != branchBefore {
		t.Fatalf("the resume's close-out moved the tree under test from %q to %q — a resumed child restored the "+
			"MAIN CHECKOUT of a repo the fixture never owned (Rule 29). The row's deliverable.repo must be the "+
			"fixture's own repo (seedRowIntoProfile's pin), never the submitter's cwd.", branchBefore, got)
	}
	if got := resumeCheckoutHead(t, checkout); got != headBefore {
		t.Fatalf("the tree under test's HEAD moved %q → %q during the resume", headBefore, got)
	}
}

// resumeSuiteCheckout is the checkout this suite is running in: the top of the
// work tree the test binary's own source tree belongs to. It is resolved from
// the package directory so the reading does not depend on the process cwd of a
// child; "" (and a SKIP) when the suite is not running inside a git work tree
// (a `git archive` extract has no checkout to move).
func resumeSuiteCheckout(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "-C", ".", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("this suite is not running inside a git checkout, so there is no tree to keep still: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// resumeCheckoutBranch / resumeCheckoutHead read the two state facts the
// regression compares, through the same `-C <dir>` anchor so neither can drift
// onto the process cwd.
func resumeCheckoutBranch(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(resumeCheckoutGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"))
}

func resumeCheckoutHead(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(resumeCheckoutGit(t, dir, "rev-parse", "HEAD"))
}

func resumeCheckoutGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return string(out)
}

// TestResumeSessionIDTargetAdoptsNoRow is the epic's headline acceptance: a
// SESSION-UUID target resolves its checkpoint, the resume runs and exits 0, and
// NONE of the three leaked lines is printed — the run is not silently
// ledger-less, it is honestly session-only.
func TestResumeSessionIDTargetAdoptsNoRow(t *testing.T) {
	gopiHome, env := resumeLatchSandbox(t)
	seedSessionRunLog(t, gopiHome, resumeLatchSessionID)

	_, stderr, code := runGopiMapped(t,
		[]string{CliVerbSession, CliVerbResume, resumeLatchSessionID, resumeLatchContinuationFile(t)}, "", env, "")
	if code != 0 {
		t.Fatalf("a session-only resume must succeed: code=%d\nstderr:\n%s", code, stderr)
	}
	for _, leak := range []string{resumeLatchLeakAdopted, resumeLatchLeakLinkage, resumeLatchLeakClose} {
		if strings.Contains(stderr, leak) {
			t.Errorf("session-id target must not latch a ledger row: stderr carries %q\nstderr:\n%s", leak, stderr)
		}
	}
	// The other half of Rule 15: the absence is REPORTED, so the operator can
	// see the run is session-only rather than wonder why the ledger is empty.
	if !strings.Contains(stderr, resumeLatchNoRowNote) {
		t.Errorf("the session-only decision must be reported (%q missing)\nstderr:\n%s", resumeLatchNoRowNote, stderr)
	}
	if !strings.Contains(stderr, resumeLatchSessionID) {
		t.Errorf("the note must name the target that has no row\nstderr:\n%s", stderr)
	}
}

// TestResumeTaskIDTargetStillAdopts is the non-regression half: a target that
// genuinely IS a row still resolves through the row view, still adopts it, and
// still reports the adoption — the behaviour memory #4684's live rig cases
// depend on. Without this half a "fix" that simply never adopts would pass the
// headline test while breaking task-id recovery entirely.
func TestResumeTaskIDTargetStillAdopts(t *testing.T) {
	gopiHome, env := resumeLatchSandbox(t)
	seedRowIntoProfile(t, gopiHome, resumeLatchRowID)
	seedSessionRunLog(t, gopiHome, resumeLatchRowID)

	_, stderr, code := runGopiMapped(t,
		[]string{CliVerbSession, CliVerbResume, resumeLatchRowID, resumeLatchContinuationFile(t)}, "", env, "")
	if code != 0 {
		t.Fatalf("a task-id resume must succeed: code=%d\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, resumeLatchLeakAdopted) {
		t.Errorf("a task-id target that IS a row must still be adopted\nstderr:\n%s", stderr)
	}
	if strings.Contains(stderr, resumeLatchLeakLinkage) || strings.Contains(stderr, resumeLatchLeakClose) {
		t.Errorf("an adopted row must link and close cleanly\nstderr:\n%s", stderr)
	}
	if strings.Contains(stderr, resumeLatchNoRowNote) {
		t.Errorf("a row-resolved target must not be reported session-only\nstderr:\n%s", stderr)
	}
}

// seedRowIntoProfile writes the fixture row into the SANDBOX profile's own
// ledger (the file the child resolves through GOPI_HOME). The pin is the
// double pin the suite's other resume fixtures document: GOPI_HOME +
// config.MemoryDBEnv for the writer, GOPI_HOME for the reader — and here they
// name the SAME file, which is what makes the row visible to the child.
//
// THE ROW'S DELIVERABLE REPO IS PINNED (Rule 29 — measured defect 2026-10-28).
// A submit with no Repo takes THIS PROCESS'S CWD as the row's
// deliverable.repo, and this suite runs from inside a checkout — the run's own
// clone ground when a worker drives it, `~/work/gopi` when an operator does.
// The resumed child's close-out then resolves that repo as the MAIN CHECKOUT to
// restore and runs `git checkout master` IN IT: the ground's own reflog carried
// `checkout: moving from task/<id> to master` three times over two runs, and the
// live run was switched off its task branch under its own feet. The 96a853d5f
// clone-ground guard does not catch it because MockEnv strips every GOPI_* var
// — including GOPI_RUN_GROUND — so a child of this suite never sees an
// announcement to decline on. The fixture therefore OWNS its ground: nobody
// else writes this directory and the session's real checkout is never named.
func seedRowIntoProfile(t *testing.T, gopiHome, taskID string) {
	t.Helper()
	repo := resumeLatchFixtureRepo(t)
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "resume-latch fixture row",
		Payload: "the row a task-id resume continues",
		TaskID:  taskID,
		Source:  "human",
		Repo:    repo,
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	// THE PIN IS ASSERTED, never assumed: a submit that silently fell back to
	// the cwd would rearm the defect while this helper still looked fixed, and
	// the damage lands in somebody else's checkout (Rule 15).
	if show, err := tools.TaskShow(taskID); err != nil {
		t.Fatalf("task show %s: %v", taskID, err)
	} else if got, ok := reportSeamShowField(show, tools.TaskShowDeliverableRepoLabel); !ok {
		t.Fatalf("the fixture row carries no %q line, so its deliverable repo cannot be proven:\n%s",
			tools.TaskShowDeliverableRepoLabel, show)
	} else if !reportSeamSamePath(got, repo) {
		t.Fatalf("the fixture row's deliverable.repo is %q, not the fixture's own repo %q — a row pointing at the "+
			"tree under test makes the resumed child's close-out run `git checkout master` in a LIVE checkout (Rule 29)",
			got, repo)
	}
	// The row must be READABLE through the same view the resume path asks, and
	// it must be in the state a killed run leaves behind — claimed work, i.e.
	// in_progress — because that is the state a resumed run closes from (the
	// ledger refuses a pending → completed transition: a row nobody started
	// has no run to close out). Both halves are asserted here so a fixture
	// that tests only its own writer fails loudly.
	if _, err := tools.TaskUpdate(taskID, tools.TaskStatusInProgress); err != nil {
		t.Fatalf("claim fixture row: %v", err)
	}
	if st, err := tools.TaskStatusOf(taskID); err != nil || st != tools.TaskStatusInProgress {
		t.Fatalf("fixture row not readable as %s through the ledger view (GOPI_MEMORY_DB=%s): st=%q err=%v",
			tools.TaskStatusInProgress, os.Getenv(config.MemoryDBEnv), st, err)
	}
}

// resumeRowGateUnknownErr is the fixture's "the ledger does not hold this row"
// error — the same shape tools.TaskStatusOf returns for an unknown id, so the
// test drives the guard's decision without asserting on the ledger's wording.
var resumeRowGateUnknownErr = fmt.Errorf("task status: no task with task_id=%q", resumeLatchRowID)

// TestResumeRowGateReportsANonResolvingAdopt pins the SECOND half of the fix at
// its own seam: an adopted row that does not resolve must REPORT, never latch.
//
// It drives adoptResumeLedgerRow directly — the function whose wiring is the
// defect — with the ledger read replaced by a seam, so both arms are observable
// without a live ledger. The REPORT half is read off the adoption itself: a
// non-resolving row leaves the agent with NO row (AdoptLedgerRow is the only
// thing that could set one, and the next test proves it still does).
func TestResumeRowGateReportsANonResolvingAdopt(t *testing.T) {
	real := resumeRowExists
	t.Cleanup(func() { resumeRowExists = real })

	// ARM 1 — the row does not resolve: NO adoption, and the reason reported on
	// stderr (the same single spelling the session-only path uses).
	resumeRowExists = func(string) (string, error) { return "", resumeRowGateUnknownErr }
	var adopted *agent.Agent
	var resolved *agent.Agent
	printed := captureStderr(t, func() {
		adopted = &agent.Agent{}
		adoptResumeLedgerRow(adopted, resumeLatchRowID)
	})
	if rowIDOf(t, adopted) != "" {
		t.Fatalf("a non-resolving row must NOT be adopted, got %q", rowIDOf(t, adopted))
	}
	if !strings.Contains(printed, resumeLatchNoRowNote) || !strings.Contains(printed, resumeLatchRowID) {
		t.Fatalf("a non-resolving adopt must REPORT the decision (naming the row), got:\n%s", printed)
	}

	// ARM 2 — the row resolves: adopted exactly as before (the gate is a gate,
	// not a removal).
	resumeRowExists = func(string) (string, error) { return tools.TaskStatusInProgress, nil }
	printed = captureStderr(t, func() {
		resolved = &agent.Agent{}
		adoptResumeLedgerRow(resolved, resumeLatchRowID)
	})
	if rowIDOf(t, resolved) != resumeLatchRowID {
		t.Fatalf("a resolving row must be adopted, got %q", rowIDOf(t, resolved))
	}
	if strings.Contains(printed, resumeLatchNoRowNote) {
		t.Fatalf("a resolving adopt must not report a missing row, got:\n%s", printed)
	}
}

// rowIDOf reads back the row an Agent carries, through the agent's own
// accessor — the same value the start hook's latch and the end hook's close-out
// key on. A nil agent has no row (and AdoptLedgerRow itself refuses nil).
func rowIDOf(t *testing.T, ag *agent.Agent) string {
	t.Helper()
	if ag == nil {
		t.Fatal("agent was never built — the arm under test did not run")
	}
	return ag.AdoptedRowID()
}
