package tools

// task_ground_announcement_test.go — the ANNOUNCEMENT's own regression guard
// (epic ground-announcement, 2026-09-25).
//
// THE DEFECT THESE TESTS PIN. The worker creates a per-task linked worktree
// ("the ground") and spawns the executor child in it — and the child works
// there. But nothing ANNOUNCED the ground, while PROJECT.md Rule 6a tells every run
// to create "a worktree for YOUR BRANCH" (~/workspace/gopi-<descriptive> on
// fix/<descriptive>). The ground's branch is task/<uuid>, which matches nothing
// in Rule 6a's literal test, so the run complied with Rule 6a and left an
// ORPHANED SPARE worktree behind: never entered, never held, named by no row,
// collected by no sweep. One task → two worktrees.
//
// WHY THE GUARD IS ON THE TEXT AND ON THE READER. The announcement is the fix,
// so the two things that can silently rot are (1) the clause losing the ground's
// DIR or BRANCH — an announcement that names no subject is not an announcement —
// and (2) the env names it is spelled with drifting away from the names the seam
// writes and the reader reads. Both are asserted against the SAME constants the
// producer uses (Rules 1/13/14), so a rename cannot pass by changing the test
// too.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskreport"
)

// TestGroundAnnouncementClauseNamesTheGroundAndForbidsASecondWorktree is the
// minimal sample (PROJECT.md Rule 10.4): a clause rendered for a concrete ground
// must name that directory AND that branch, and must carry the deferral — the
// ban on a second worktree AND the ban on renaming the branch, which are the
// two ways a run otherwise destroys the ground's identity.
//
// THE RENAME ASSERTION IS AN ABSENCE, and that is the point (2026-10-07). The
// clause used to BLESS `git branch -m <descriptive-name>`; that blessing is
// what severed the branch from the row (`task/crelease` is the measured
// specimen — the ledger cannot resolve it), so the regression is pinned on the
// blessing being GONE and on the uuid rule being present in its place. A test
// that only checked for the new sentence would pass while the old one came
// back beside it.
func TestGroundAnnouncementClauseNamesTheGroundAndForbidsASecondWorktree(t *testing.T) {
	const (
		dir    = "/home/agent/workspace/gopi-01a0ce80"
		branch = "task/01a0ce80"
	)
	clause := TaskGroundAnnouncementClause(dir, branch)
	if clause == "" {
		t.Fatalf("a ground with a directory must render a clause — the runner has something to announce")
	}
	if !strings.Contains(clause, TaskGroundAnnouncementLabel) {
		t.Errorf("the clause must carry the announcement label %q, so a reader (and the GOPI.md Rule 6a clause) can address it by name:\n%s",
			TaskGroundAnnouncementLabel, clause)
	}
	if !strings.Contains(clause, dir) {
		t.Errorf("the clause must NAME the ground directory %q — an announcement that names no subject is not an announcement:\n%s", dir, clause)
	}
	if !strings.Contains(clause, branch) {
		t.Errorf("the clause must NAME the ground's branch %q — the branch is what Rule 6a's literal test looked for and failed to find:\n%s", branch, clause)
	}
	// The instruction that makes it a DEFERRAL rather than a description: a
	// second worktree is refused (the spare-worktree defect, memory #4975), and
	// the branch is NOT renamed (the cross-comparability property).
	for _, want := range []string{"Do NOT create a second worktree", "do NOT rename the branch", "gopi task show --branch"} {
		if !strings.Contains(clause, want) {
			t.Errorf("the clause must carry %q — the deferral is what stops a compliant run from minting a spare or severing the row from the ref:\n%s", want, clause)
		}
	}
	// The REVERSAL: the rename blessing must not come back. `git branch -m`
	// must appear nowhere in the clause — not blessed, not as a suggestion.
	if strings.Contains(clause, "git branch -m") {
		t.Errorf("the clause must NOT carry the rename blessing (`git branch -m`): the branch name IS the ledger address, and a rename leaves a ref no row can be read off:\n%s", clause)
	}
}

// TestGroundEvidenceClauseNamesTheRunsAllowedDestination is the TEACHING half's
// guard: the fence the announcement states ("your worktree is the ONLY tree you
// write") is paired with a destination, and the destination is not invented here
// — it is the run's OWN session-artifacts directory, resolved through the SAME
// resolver the report contract uses (TaskReportPath → taskSessionDir). A run with
// nowhere legal to put a capture invents `<repo>/scratch/`; that is the measured
// 2026-09-26 shape (row 01a0dbe4, 161 untracked files in the HP main checkout).
func TestGroundEvidenceClauseNamesTheRunsAllowedDestination(t *testing.T) {
	const (
		row    = "t-groundevidence"
		ground = "/home/user/workspace/gopi-01a0dbec"
	)
	reportPath, err := TaskReportPath(row)
	if err != nil {
		t.Fatalf("the destination must come from the report contract's own resolver: %v", err)
	}
	want := filepath.Dir(reportPath)
	clause := TaskGroundEvidenceClause(row, ground)
	if clause == "" {
		t.Fatalf("a row with a ground must render an evidence destination — a fence with no door is what the run routes around")
	}
	if !strings.Contains(clause, TaskGroundEvidenceLabel) {
		t.Errorf("the clause must carry its label %q so a reader can address it by name:\n%s", TaskGroundEvidenceLabel, clause)
	}
	if !strings.Contains(clause, want) {
		t.Errorf("the clause must name the resolved evidence directory %q (the directory that holds %s):\n%s", want, taskreport.FileName, clause)
	}
	if !strings.Contains(clause, taskreport.PathEnv) {
		t.Errorf("the clause must name the env var the run ALREADY holds for that directory (%s) — a destination the caller can resolve a second way is one it can verify:\n%s", taskreport.PathEnv, clause)
	}
	if !strings.Contains(clause, ground+"/"+reclaimMainCheckoutScratchName) {
		t.Errorf("the clause must name the FORBIDDEN invention by its measured spelling (%s/%s) — the run's own tree is the one place a destination looks legal and is not:\n%s",
			ground, reclaimMainCheckoutScratchName, clause)
	}
	// A row with no ground renders nothing: there is no tree to distinguish the
	// destination from, and a clause naming no subject is worse than no clause.
	if got := TaskGroundEvidenceClause("", ground); got != "" {
		t.Errorf("no row ⇒ no clause, got %q", got)
	}
}

// TestGroundAnnouncementClauseIsEmptyWithoutAGround pins the OTHER direction:
// a row that resolved no ground announces NOTHING. An empty dir rendered into a
// clause would be a record naming no subject — the shape Rule 15 refuses — and
// the seam relies on this "" to bind nothing into the briefing.
func TestGroundAnnouncementClauseIsEmptyWithoutAGround(t *testing.T) {
	for _, tc := range []struct{ name, dir, branch string }{
		{"no dir", "", "task/x"},
		{"blank dir", "   ", "task/x"},
		{"nothing at all", "", ""},
	} {
		if got := TaskGroundAnnouncementClause(tc.dir, tc.branch); got != "" {
			t.Errorf("%s: a row with no ground must render no clause; got:\n%s", tc.name, got)
		}
	}
}

// TestGroundAnnouncementClauseNamesBranchNoneRatherThanDroppingIt pins the
// branch's zero-value rendering: the pair travels together (the seam exports
// both names or neither), so a ground whose branch could not be read says
// "none" instead of rendering a clause with a hole in it.
func TestGroundAnnouncementClauseNamesBranchNoneRatherThanDroppingIt(t *testing.T) {
	clause := TaskGroundAnnouncementClause("/tmp/ground", "")
	if !strings.Contains(clause, groundBranchOrDash("")) {
		t.Errorf("a ground with no resolvable branch must still announce the branch axis (%q), never omit it:\n%s",
			groundBranchOrDash(""), clause)
	}
	if GroundBranchLabel("  ") != groundBranchOrDash("") {
		t.Errorf("GroundBranchLabel must read the ONE spelling of the empty-branch label (%q)", groundBranchOrDash(""))
	}
}

// TestTaskGroundAnnouncedReadsTheSeamPair is the READER's guard (Rule 21: the
// env-name constants must have a real consumer). The reader answers with what
// the runner STATED — never with a path derived from the process cwd, because a
// ground nobody made for this process is not this process's ground.
//
// Rule 17: t.Setenv (not os.Setenv) for the set case (restored automatically).
// The unset case is asserted with the pair EXPLICITLY absent — the spawn seam's
// strip means an executor that was not handed a ground must read ("", ""), so
// absence is a case this reader has to get right, not a detail.
func TestTaskGroundAnnouncedReadsTheSeamPair(t *testing.T) {
	t.Setenv(TaskGroundDirEnv, "/home/agent/workspace/gopi-01a0ce80")
	t.Setenv(TaskGroundBranchEnv, "task/01a0ce80")
	dir, branch := TaskGroundAnnounced()
	if dir != "/home/agent/workspace/gopi-01a0ce80" || branch != "task/01a0ce80" {
		t.Fatalf("TaskGroundAnnounced must read the pair the spawn seam writes; got (%q, %q)", dir, branch)
	}

	// The absent case, in a subtest whose env is its own (t.Setenv restores at
	// the end of the SUBTEST, so the pair cannot leak into a sibling).
	t.Run("absent", func(t *testing.T) {
		t.Setenv(TaskGroundDirEnv, "")
		t.Setenv(TaskGroundBranchEnv, "")
		if dir, branch := TaskGroundAnnounced(); dir != "" || branch != "" {
			t.Fatalf("a process the seam did not spawn must read no ground; got (%q, %q) — an announced ground is a fact the RUNNER stated, never one derived here", dir, branch)
		}
	})
}

// TestGroundAnnouncementEnvNamesAreNamespaced pins the wire spelling itself: the
// names are read by a child process and quoted in logs, so they must stay in the
// GOPI_ namespace rather than becoming a private convention (Rule 1b — the
// spelling has exactly one home, and this asserts that home's shape).
func TestGroundAnnouncementEnvNamesAreNamespaced(t *testing.T) {
	for _, name := range []string{TaskGroundDirEnv, TaskGroundBranchEnv} {
		if !strings.HasPrefix(name, "GOPI_") {
			t.Errorf("the announcement env name %q must live in the GOPI_ namespace", name)
		}
	}
	if TaskGroundDirEnv == TaskGroundBranchEnv {
		t.Errorf("the two axes must have two names; both are %q", TaskGroundDirEnv)
	}
}
