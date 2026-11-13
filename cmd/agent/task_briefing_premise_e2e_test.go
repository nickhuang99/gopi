package main

// task_briefing_premise_e2e_test.go — ACCEPTANCE ON THE REAL SURFACE (Rule 31):
// the dispatch-time premise refresh, measured in the briefing TEXT a child
// actually receives.
//
// WHAT THIS WALK MEASURES, and why unit tests cannot stand in for it:
//
//  ① THE STAMP REACHED THE CHILD'S FIRST MODEL REQUEST. The refresh is a
//     text mutation at the spawn seam (cmd/agent/cli_tasks.go), and a mutation
//     bound into `cmd.Stdin` is exactly the kind of code that is correct in
//     isolation and never reached: the seam appends the clause inside the
//     ground-resolution block, before the ONE binding. So the assertion is made
//     against the body the mock LLM served for request 1 — the child's first
//     round, i.e. the briefing it was handed — never against a string the test
//     composed.
//
//  ② THE POSITIVE CONTROL TRAVELS WITH IT. One briefing names BOTH a LANDED
//     revision (a stale premise) and a genuinely UNLANDED one. The child must be
//     told to verify the first and NOT the second, and the round log must carry
//     both measurements — so a guard that stamps every revision it sees fails
//     this walk, not just the unit suite.
//
//  ③ THE SEAM RECORDED IT (Rule 15). The measurement reaches the run's own
//     output log, which is the artifact an evidence reader has.
//
// WHICH PROCESS IS MEASURED: the child is found by /proc environment through the
// EXISTING helper (groundWaitForChildCwd — the process carrying
// GOPI_TASK_WRAPPER_ID=<row>; never a pgrep pattern, Rule 24's self-match trap).

import (
	"fmt"
	"github.com/nick/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// premiseE2ERow is this walk's row id — a FULL uuid, because the ground's path
// and branch stem are derived from it (internal/tools/task_run_ground.go).
const premiseE2ERow = "d1f0a2c3-5b64-4e77-9a18-2c6d4f8b0e31"

const (
	premiseE2ESideBranch = "feat/premise-e2e-unlanded"
	premiseE2ESummary    = "briefing premise refresh"
	// Each premise is its OWN BULLET, and that is load-bearing: the claim reader
	// (taskreport.claimSentences, shared with the mirror detector) joins wrapped
	// lines into one sentence and splits on ";"/"|", so two premises written as
	// one paragraph are ONE claim — and a single claim names one revision. Two
	// bullets are two claims, which is what this walk needs to assert that the
	// landed one is stamped and the open one is NOT.
	premiseE2EPayload = "# Epic: fix the read path\n\n" +
		"- Row 01a12c4a is completed but its fix (%s) never landed on %s — re-implement it here.\n" +
		"- The side-branch work (%s) is missing from %s, so pick it up in the same pass.\n"
	// premiseE2EMaxCommitTries bounds the commit-shape hunt below (Rule 27: a
	// loop that creates commits needs its own ceiling).
	premiseE2EMaxCommitTries = 8
)

// TestWorkerStampsAStaleBriefingPremiseForTheChild is the walk described in the
// file header.
func TestWorkerStampsAStaleBriefingPremiseForTheChild(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	repo := executorCwdProbeRepo(t, "briefing-premise-repo")
	landed := premiseE2ELandedCommit(t, repo)
	unlanded := premiseE2EUnlandedCommit(t, repo)

	mock := testutil.NewMockLLM(t,
		testutil.Resp{Hold: true},
		testutil.Resp{Text: "briefing premise walk done"},
	)
	t.Cleanup(mock.Close)

	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL())
	// The payload names BOTH revisions: the landed one (the stale premise, which
	// must be stamped) and the open one (the positive control, which must not).
	payload := fmt.Sprintf(premiseE2EPayload, landed, tools.IntegrationBranchName,
		unlanded, tools.IntegrationBranchName)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit", premiseE2ESummary,
		"--payload", payload, "--task-id", premiseE2ERow, "--repo", repo},
		"", env, repo); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	wrapper := exec.Command(bin, "worker", premiseE2ERow)
	wrapper.Dir = repo
	wrapper.Env = env
	var wout, werr strings.Builder
	wrapper.Stdout = &wout
	wrapper.Stderr = &werr
	if err := wrapper.Start(); err != nil {
		t.Fatalf("start `gopi worker %s`: %v", premiseE2ERow, err)
	}
	wrapperPid := wrapper.Process.Pid
	t.Cleanup(func() {
		if wrapper.ProcessState == nil {
			_ = wrapper.Process.Kill()    //stylegate:ignore kills the fixture process the test spawned
			_, _ = wrapper.Process.Wait() //stylegate:ignore reaps the fixture process the test spawned
		}
	})
	childPid, childCwd := groundWaitForChildCwd(t, premiseE2ERow, wrapperPid)
	t.Cleanup(func() { groundKill(t, childPid) })

	// The measurement is taken through the SAME function the seam uses, so the
	// expected spellings (the abbreviated sha) are not re-derived here.
	landedEv := tools.MasterClaimEvidenceAt(repo, landed)
	if !landedEv.OK() {
		t.Fatalf("fixture precondition: %s must be an ancestor of %s; evidence: %v",
			landed, tools.IntegrationBranchName, landedEv.Observations())
	}
	unlandedEv := tools.MasterClaimEvidenceAt(repo, unlanded)
	if !unlandedEv.NotOnIntegrationBranch() {
		t.Fatalf("fixture precondition: %s must NOT be an ancestor of %s; evidence: %v",
			unlanded, tools.IntegrationBranchName, unlandedEv.Observations())
	}

	groundWaitForFirstModelRequest(t, mock)
	body := mock.RequestBody(1)
	if strings.TrimSpace(body) == "" {
		t.Fatalf("the mock served no request body — the child never reached its first model round, so the briefing cannot be measured")
	}
	// The clause is APPENDED LAST, so everything from its label on is the stamp
	// (the briefing is the child's whole stdin). Scoping the control to it is not
	// a convenience: the payload itself quotes both revisions in full, and a
	// full sha CONTAINS its own abbreviation, so a whole-body search for the
	// open revision's short form matches the briefing's own text and asserts
	// nothing (measured — this assertion was wrong before the scope was added).
	stamp := ""
	if i := strings.Index(body, tools.TaskBriefingPremiseLabel); i >= 0 {
		stamp = body[i:]
	}

	// ① THE LANDED PREMISE IS STAMPED, WITH ITS MEASUREMENT.
	for _, want := range []string{
		tools.TaskBriefingPremiseLabel,
		landedEv.ShortCommit,
		tools.TaskBriefingPremiseVerifyOnly,
		"do NOT re-implement",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the child's FIRST model request must carry %q — the premise was re-measured at dispatch and the child must be told; body:\n%s",
				want, groundAnnounceBodyExcerpt(body))
		}
	}
	// ② THE POSITIVE CONTROL: the genuinely open revision carries NO stamp. The
	// clause names the landed sha and must not name the open one, or the child
	// would be told to skip work that is really missing.
	if stamp == "" {
		t.Fatalf("the stamped clause is ABSENT from the child's first request; body:\n%s", groundAnnounceBodyExcerpt(body))
	}
	if strings.Contains(stamp, unlandedEv.ShortCommit) {
		t.Errorf("the stamped clause must NOT name the genuinely unlanded revision %s — a guard that stamps everything is as useless as one that stamps nothing; clause:\n%s",
			unlandedEv.ShortCommit, stamp)
	}

	// ③ THE SEAM'S OWN RECORD: one line per claim examined, the landed arm and
	// the open arm both readable where the run's facts are read (Rule 15).
	roundLog := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, premiseE2ERow, tools.WorkerLogFileName)
	data, err := os.ReadFile(roundLog)
	if err != nil {
		t.Fatalf("read the round log %s: %v", roundLog, err)
	}
	log := string(data)
	if !strings.Contains(log, "briefing premise →") {
		t.Errorf("the seam must RECORD the premise measurement in the run log; got:\n%s", log)
	}
	if !strings.Contains(log, landedEv.ShortCommit) || !strings.Contains(log, "STAMPED") {
		t.Errorf("the record must state the landed verdict for %s; got:\n%s", landedEv.ShortCommit, log)
	}
	if !strings.Contains(log, unlandedEv.ShortCommit) || !strings.Contains(log, "is NOT an ancestor") {
		t.Errorf("the record must state the OPEN verdict for %s (the control); got:\n%s", unlandedEv.ShortCommit, log)
	}
	_ = childCwd

	mock.ReleaseHold(1)
	groundStopRun(t, wrapper, childPid, &wout, &werr)
}

// premiseE2ELandedCommit returns a commit that IS an ancestor of the
// integration branch, choosing one whose sha is commit-SHAPED (a letter and a
// digit) so the detector reads it as a revision rather than as a word.
func premiseE2ELandedCommit(t *testing.T, repo string) string {
	t.Helper()
	for i := 0; i < premiseE2EMaxCommitTries; i++ {
		sha := premiseE2EGit(t, repo, "rev-parse", "HEAD")
		if premiseE2EShaShaped(sha) {
			return sha
		}
		premiseE2EGit(t, repo, "commit", "--allow-empty", "-q", "-m", "shape")
	}
	t.Fatalf("no commit-shaped sha among %d attempts on HEAD", premiseE2EMaxCommitTries)
	return ""
}

// premiseE2EUnlandedCommit creates (and returns) a side-branch tip that the
// integration branch does not contain — the walk's positive control.
func premiseE2EUnlandedCommit(t *testing.T, repo string) string {
	t.Helper()
	premiseE2EGit(t, repo, "checkout", "-q", "-b", premiseE2ESideBranch)
	var sha string
	for i := 0; i < premiseE2EMaxCommitTries; i++ {
		premiseE2EGit(t, repo, "commit", "--allow-empty", "-q", "-m", "unlanded shape")
		sha = premiseE2EGit(t, repo, "rev-parse", "HEAD")
		if premiseE2EShaShaped(sha) {
			break
		}
	}
	if !premiseE2EShaShaped(sha) {
		t.Fatalf("no commit-shaped sha among %d attempts on the side branch", premiseE2EMaxCommitTries)
	}
	premiseE2EGit(t, repo, "checkout", "-q", tools.IntegrationBranchName)
	return sha
}

// premiseE2EGit runs git in repo, failing the test on error (never a silent
// zero — Rule 15).
func premiseE2EGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, repo, err, out)
	}
	return strings.TrimSpace(string(out))
}

// premiseE2EShaShaped is the fixture's own copy of the DETECTOR's shape rule
// (a hex run carrying both a letter and a digit): the fixture must not hand the
// walk a sha the detector would not read as a revision, which would make the
// red proof pass for a fixture reason instead of a code reason.
func premiseE2EShaShaped(sha string) bool {
	letter, digit := false, false
	for _, r := range sha {
		if r >= 'a' && r <= 'f' {
			letter = true
		}
		if r >= '0' && r <= '9' {
			digit = true
		}
	}
	return letter && digit
}
