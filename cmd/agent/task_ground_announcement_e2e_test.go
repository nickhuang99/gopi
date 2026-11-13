package main

// task_ground_announcement_e2e_test.go — ACCEPTANCE ON THE REAL SURFACE (Rule
// 31), and the RED PROOF of the announcement (epic ground-announcement,
// 2026-09-25).
//
// WHAT THIS WALK MEASURES, and why each half is needed:
//
//  ① ONE WORKTREE — `gopi worker <id>` over a fresh repo-aware row leaves the
//     repo with exactly ONE linked worktree while the run is in flight, and it
//     is the directory the child actually stands in (read from
//     /proc/<pid>/cwd, never inferred). Pre-fix this same reading was ALREADY
//     one-or-more depending on the model; the invariant is what the fix must
//     preserve, and it is asserted so a future change cannot "fix" the spare by
//     killing the ground.
//
//  ② THE ANNOUNCEMENT REACHED THE CHILD BEFORE ITS FIRST TOOL ROUND. This is
//     the fix, and it is the half that is RED on the pre-fix tree: the child's
//     FIRST model request must carry the ground's directory and branch. The
//     mechanism is impossible to see any other way — the pre-fix child was
//     spawned in the ground (cmd.Dir, correct) and behaved correctly there, so
//     the defect was never that the child stood in the wrong place; it was that
//     nothing TOLD it, while PROJECT.md Rule 6a told it to build a worktree "for
//     YOUR BRANCH" and the ground's branch (task/<uuid>) matched nothing in
//     that test. So the child obeyed Rule 6a and left an orphaned spare.
//
// WHY A SCRIPTED CHILD CANNOT ITSELF BE THE RED PROOF, STATED HONESTLY. The
// spare is created by a MODEL obeying Rule 6a; a mock LLM cannot be made to
// "obey Rule 6a and now stop obeying it" — its script is identical on both
// trees. So this walk does not claim to reproduce the spare; it measures the
// two things the fix does own: the announcement lands in the child's first
// message (impossible pre-fix — the string does not exist yet), and the run's
// worktree accounting stays exactly-one. The elimination of the spare itself
// rests on the model reading the amended Rule 6a, and the report says so rather
// than claiming a code path that could forbid worktree creation without
// breaking the no-ground case Rule 6a exists for.
//
// WHICH PROCESS IS MEASURED: the child is found by /proc environment — it is
// the process carrying GOPI_TASK_WRAPPER_ID=<row>, which only an executor child
// of this row can have (never a pgrep pattern — Rule 24's self-match trap).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
	"github.com/nick/gopi/internal/worktreeroots"
)

// groundAnnounceTestRow is this walk's row id. It is a FULL uuid, and that is
// load-bearing now: the ground's path/branch stem is derived from it
// (gopi-<uuid with dashes removed>), so a slug row id has no derivable ground
// and the run refuses to start (internal/tools/task_run_ground.go).
const groundAnnounceTestRow = "cb4434a7-40f8-4240-872a-13cbbb74eb5e"

// TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree is the explicit
// acceptance walk described in the file header.
func TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree(t *testing.T) {
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	root := t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	repo := executorCwdProbeRepo(t, "ground-announce-repo")

	// The held response keeps the child INSIDE its first model round trip, so
	// every reading below lands mid-run (the row is in_progress) — which is the
	// only moment at which "the announcement preceded the child's first tool
	// round" is a statement about the fix rather than about some later state.
	mock := testutil.NewMockLLM(t,
		testutil.Resp{Hold: true},
		testutil.Resp{Text: "ground announcement done"},
	)
	t.Cleanup(mock.Close)

	env := executorProbeEnv(t, gopiHome, dbPath, mock.URL())
	env = append(env, worktreeroots.WorktreeRootEnvName+"="+root)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"ground announcement", "--payload", "say something", "--task-id", groundAnnounceTestRow, "--repo", repo},
		"", env, repo); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	// Precondition: the row names a repo and NO ground yet — the defect's input.
	if _, preBranch, preWorktree := groundRowDeliverable(t, dbPath, groundAnnounceTestRow); preWorktree != "" || preBranch != "" {
		t.Fatalf("fixture precondition: the row must start with NO ground; got branch=%q worktree=%q", preBranch, preWorktree)
	}

	wrapper := exec.Command(bin, "worker", groundAnnounceTestRow)
	wrapper.Dir = repo
	wrapper.Env = env
	var wout, werr strings.Builder
	wrapper.Stdout = &wout
	wrapper.Stderr = &werr
	if err := wrapper.Start(); err != nil {
		t.Fatalf("start `gopi worker %s`: %v", groundAnnounceTestRow, err)
	}
	wrapperPid := wrapper.Process.Pid
	t.Cleanup(func() {
		if wrapper.ProcessState == nil {
			_ = wrapper.Process.Kill()    //stylegate:ignore kills the fixture process the test spawned
			_, _ = wrapper.Process.Wait() //stylegate:ignore reaps the fixture process the test spawned
		}
	})

	childPid, childCwd := groundWaitForChildCwd(t, groundAnnounceTestRow, wrapperPid)
	t.Cleanup(func() { groundKill(t, childPid) })

	// ① THE CHILD STANDS IN THE GROUND — and the ground is a LINKED worktree of
	// its repo under the pinned root (the child was NOT sent to the main
	// checkout, and NOT to a foreign directory).
	if !strings.HasPrefix(childCwd, root+string(filepath.Separator)) {
		t.Fatalf("/proc/%d/cwd = %s — the child's ground must be under the pinned worktree root %s", childPid, childCwd, root)
	}
	if fi, err := os.Stat(filepath.Join(childCwd, ".git")); err != nil || fi.IsDir() {
		t.Fatalf("%s must be a LINKED worktree (.git a file); stat err=%v", childCwd, err)
	}
	if !groundRegisteredIn(t, repo, childCwd) {
		t.Fatalf("git must register the child's ground %s as a worktree of %s; listing:\n%s",
			childCwd, repo, groundWorktreeListOf(t, repo))
	}

	// ② THE ROW NAMES THE SAME DIRECTORY — the invariant the briefing states:
	// the child's cwd, the row's deliverable.worktree, and the worktree holding
	// the commits are ONE path.
	_, rowBranch, rowWorktree := groundRowDeliverable(t, dbPath, groundAnnounceTestRow)
	if rowWorktree == "" || rowBranch == "" {
		t.Fatalf("the row must name its ground during the run; got branch=%q worktree=%q", rowBranch, rowWorktree)
	}
	if !SameDirString(rowWorktree, childCwd) {
		t.Fatalf("the row's deliverable.worktree (%s) must be the child's own cwd (%s)", rowWorktree, childCwd)
	}
	if status := groundRowStatus(t, dbPath, groundAnnounceTestRow); status != "in_progress" {
		t.Errorf("the readings above must come from an IN-FLIGHT run; status=%q", status)
	}

	// ③ EXACTLY ONE LINKED WORKTREE — no spare was created for this task.
	linked := groundAnnounceLinkedWorktrees(t, repo)
	if len(linked) != 1 {
		t.Fatalf("the repo must have exactly ONE linked worktree while the run is in flight; found %d:\n%s",
			len(linked), strings.Join(linked, "\n"))
	}
	if !SameDirString(linked[0], childCwd) {
		t.Fatalf("the single linked worktree (%s) must be the one the child stands in (%s) — a second, unheld worktree is the spare this epic removes",
			linked[0], childCwd)
	}

	// ④ THE ANNOUNCEMENT REACHED THE CHILD'S FIRST REQUEST — this is the fix,
	// and it is the assertion that is RED on the pre-fix tree. Request 1 is the
	// child's FIRST model round, i.e. before any tool ran.
	//
	// The request must be WAITED for, not assumed: /proc/<pid>/cwd is readable
	// the instant the child exists, while its first model round happens after
	// registration and prompt assembly (measured: the cwd was readable ~7s
	// before the first request arrived). Bounded and loud — a request that never
	// arrives is a failure of this walk, never a silent skip.
	groundWaitForFirstModelRequest(t, mock)
	body := mock.RequestBody(1)
	if strings.TrimSpace(body) == "" {
		t.Fatalf("the mock served no request body — the child never reached its first model round, so the announcement cannot be measured")
	}
	for _, want := range []string{tools.TaskGroundAnnouncementLabel, childCwd, rowBranch} {
		if !strings.Contains(body, want) {
			t.Errorf("the child's FIRST model request must carry %q — without it the run never learns it has a ground and obeys GOPI.md Rule 6a instead, leaving a spare:\n%s",
				want, groundAnnounceBodyExcerpt(body))
		}
	}
	// ④b THE FENCE'S DOOR (2026-09-26, row 01a0dbe4 on the HP main checkout): the
	// announcement forbade the shared checkout and named no legal destination, so
	// that run invented `<repo>/scratch/` for its evidence — 161 untracked files
	// in the operator's checkout. The request must therefore carry the run's OWN
	// session-artifacts directory: the directory the child's exported
	// GOPI_TASK_REPORT_PATH points into, resolved by the seam through the SAME
	// resolver (tools.TaskReportPath). It is spelled from the CHILD's profile
	// (gopiHome), not from this test process's ambient one — the child runs on its
	// own GOPI_HOME, and a destination computed here would assert the wrong home.
	childEvidenceDir := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, groundAnnounceTestRow)
	for _, want := range []string{tools.TaskGroundEvidenceLabel, childEvidenceDir} {
		if !strings.Contains(body, want) {
			t.Errorf("the child's FIRST model request must carry %q — a fence with no legal destination is a fence the run routes around (<repo>/scratch/):\n%s",
				want, groundAnnounceBodyExcerpt(body))
		}
	}

	// ④c THE BASELINE CLAUSE (epic 01a12d66, 2026-10-12): a briefing's acceptance
	// criterion is often phrased against the integration branch («`git diff --stat
	// master...HEAD` is empty»), but a ground is cut from the PEER's tip while the
	// LOCAL refs/heads/master lags it, so that three-dot diff reports the whole
	// INHERITED population and is non-empty for a run that changed nothing. The
	// clause that names the cut point must therefore reach the child BEFORE its
	// first tool round, exactly like the announcement above — this is the half
	// that cannot be measured any other way (the row's payload text is the
	// submitter's prose and is deliberately untouched).
	//
	// The cut point is read from the CHILD'S own ground (childCwd), the same tree
	// the announcement names, and it must be the sha the reflog records — the
	// value the clause is measured against by whoever reads the briefing.
	wantCut := groundCutPointOf(t, childCwd, rowBranch)
	for _, want := range []string{tools.GroundCutPointLabel, wantCut} {
		if !strings.Contains(body, want) {
			t.Errorf("the child's FIRST model request must carry %q — a criterion phrased against the integration branch on this ground is non-empty by construction:\n%s",
				want, groundAnnounceBodyExcerpt(body))
		}
	}
	// The CORRECTED range travels with it: the clause renders `<cut>..HEAD` so
	// the correction is executable rather than a second document.
	if !strings.Contains(body, wantCut+"..HEAD") {
		t.Errorf("the child's briefing must render the corrected range `%s..HEAD` so a `master...HEAD` criterion can be re-based:\n%s",
			wantCut, groundAnnounceBodyExcerpt(body))
	}

	// ④d THE MASTER-READING CLAUSE (epic 01a13af7, 2026-10-14): the sibling above
	// re-bases a criterion that quantifies over the integration branch; this one
	// answers the question one step earlier, because the criterion in the
	// measured case said «用 master 的版本» and THREE different blobs in one tree
	// answered to that word (refs/heads/master 2502 lines, the peer mirror 2533,
	// the ground's cut commit 2540) — the deliverable's red/green flipped with
	// the ref the reviewer held. The clause must reach the child before its first
	// tool round, and it must name the refs IN FULL: a bare branch name is the
	// spelling the ambiguity lives in, so asserting only the label would pass on
	// exactly the text that produced the epic.
	for _, want := range []string{tools.MasterReadingLabel, tools.MasterReadingLocalRef(),
		tools.MasterReadingTipRef()} {
		if !strings.Contains(body, want) {
			t.Errorf("the child's FIRST model request must carry %q — a criterion phrased against \"master\" is undecidable without the ref it denotes:\n%s",
				want, groundAnnounceBodyExcerpt(body))
		}
	}

	// ⑤ THE SEAM RECORDED IT (Rule 15). The round log is the artifact an
	// evidence reader has, so "the child was told" must be readable there.
	roundLog := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, groundAnnounceTestRow, tools.WorkerLogFileName)
	if data, err := os.ReadFile(roundLog); err == nil {
		if !strings.Contains(string(data), "child ground ANNOUNCED") {
			t.Errorf("the spawn seam must RECORD the announcement in the round log; got:\n%s", groundGroundLines(string(data)))
		}
	} else {
		t.Errorf("read the round log %s: %v", roundLog, err)
	}

	t.Logf("measured: pid %d cwd %s (row branch %s), linked worktrees=%d", childPid, childCwd, rowBranch, len(linked))

	mock.ReleaseHold(1)
	groundStopRun(t, wrapper, childPid, &wout, &werr)
}

// groundCutPointOf reads the commit the row's branch was cut at, the way the
// seam does (tools.GroundCutPoint over the ground worktree) — the value the
// clause is asserted against is therefore the MEASUREMENT, not a re-derivation
// that could agree with a broken clause.
func groundCutPointOf(t *testing.T, ground, branch string) string {
	t.Helper()
	read := tools.GroundCutPoint(ground, branch)
	if !read.Measured() {
		t.Fatalf("the fixture's ground must have a readable cut point (%s): %s", read.Evidence, read.Note)
	}
	return read.SHA[:groundCutShownLen]
}

// groundCutShownLen is the abbreviation the clause renders (tools-side
// shortSha's length): the test compares what a reader SEES, so a change to the
// abbreviation cannot leave this assertion measuring a full sha.
const groundCutShownLen = 7

// groundWaitForFirstModelRequest blocks (bounded) until the mock has served one
// request — the child's first model round, which is the moment the announcement
// must already have reached it. Reported on timeout, never silently skipped.
func groundWaitForFirstModelRequest(t *testing.T, mock *testutil.MockLLM) {
	t.Helper()
	deadline := time.Now().Add(groundE2EProbeWait)
	for {
		if mock.RequestCount() >= 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the executor child made no model request within %s — the announcement cannot be measured on a run that never reached its first round",
				groundE2EProbeWait)
		}
		time.Sleep(groundE2EProbePeriod) //stylegate:allow-sleep bounded wait for a just-spawned child's first model round (no event fires when it lands)
	}
}

// groundAnnounceLinkedWorktrees returns every LINKED worktree of repo (the main// checkout excluded) from the porcelain listing — the population a spare would
// join.
func groundAnnounceLinkedWorktrees(t *testing.T, repo string) []string {
	t.Helper()
	var linked []string
	for _, rec := range strings.Split(groundWorktreeListOf(t, repo), "\n\n") {
		var path string
		for _, line := range strings.Split(rec, "\n") {
			if p, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok {
				path = filepath.Clean(p)
			}
		}
		if path == "" || SameDirString(path, repo) {
			continue // the main checkout is not a linked worktree
		}
		linked = append(linked, path)
	}
	return linked
}

// groundAnnounceBodyExcerpt bounds a request body for an error message (a full
// briefing is thousands of bytes and an assertion message must stay readable).
func groundAnnounceBodyExcerpt(body string) string {
	const limit = 1200
	if len(body) <= limit {
		return body
	}
	return body[:limit] + "… (truncated)"
}
