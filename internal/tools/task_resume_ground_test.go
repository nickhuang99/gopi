// Tests for the RESOLVE-ONLY ground resolver behind `gopi worker --resume`
// (task_resume_ground.go, devdoc/worker-resume-ground.md).
//
// THE PROPERTY UNDER TEST is not "does it find the right directory" — it is
// "does it OWN the answer without TOUCHING anything": the two halves of the
// acceptance bar that a green happy path cannot show are asserted here, because
// both are failures that LOOK like success:
//
//   - a resolver that CREATES the worktree it was asked about answers the same
//     question the right way and leaves the operator with ground they did not
//     have (TestResumeGroundNeverCreatesGround);
//   - a resolver that falls back to "some directory" for an unknown branch
//     resumes SOMETHING and reports success (every negative case below).
//
// The positive cases run against a real git repo with a real linked worktree
// (the resolver's job includes `git worktree list`, so a fixture without git
// could not tell a working resolver from one that reads the ledger alone), and
// every ledger read is pinned to a fresh per-test database (Rule 32).

package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/devworkspace"
)

// resumeGroundRepo is a throwaway repository with one linked worktree on a
// feature branch — the shape the resolver must find. It returns the repo root,
// the worktree path and the branch.
func resumeGroundRepo(t *testing.T, branch string) (repo, wt string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	wt = filepath.Join(root, "ground")
	gitIn := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
		}
	}
	gitIn(root, "init", repo)
	gitIn(repo, "config", "user.email", "t@t")
	gitIn(repo, "config", "user.name", "t")
	gitIn(repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(repo, "add", "a.txt")
	gitIn(repo, "commit", "-m", "init")
	gitIn(repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "wip.txt"), []byte("deliverable"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(repo, "add", "wip.txt")
	gitIn(repo, "commit", "-m", "deliverable work")
	gitIn(repo, "checkout", "master")
	gitIn(repo, "worktree", "add", wt, branch)
	return repo, wt
}

// resumeGroundFixture pins a fresh ledger and returns the repo + ground for one
// branch plus a row that names them.
func resumeGroundFixture(t *testing.T, branch string) (repo, wt, taskID string) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt = resumeGroundRepo(t, branch)
	taskID = "t-resume-ground"
	forBranchSeed(t, "t-warm", taskStatusPending, "", repo, "", "")
	forBranchSeed(t, taskID, taskStatusInProgress, "", repo, branch, wt)
	return repo, wt, taskID
}

// TestResumeGroundByBranchResolvesTheLiveWorktreeAndItsRow is the `--branch`
// spelling's acceptance: the branch's registered worktree is found, the repo is
// derived from the ledger's own rows (the operator is not asked which host
// holds it), and the row comes back named.
func TestResumeGroundByBranchResolvesTheLiveWorktreeAndItsRow(t *testing.T) {
	branch := "feat/resume-ground-branch"
	repo, wt, taskID := resumeGroundFixture(t, branch)

	got, err := TaskResumeGroundFor(branch, "")
	if err != nil {
		t.Fatalf("TaskResumeGroundFor(branch): %v", err)
	}
	if !SameDir(got.Dir, wt) {
		t.Errorf("Dir = %q, want the registered worktree %q", got.Dir, wt)
	}
	if got.Branch != branch {
		t.Errorf("Branch = %q, want %q", got.Branch, branch)
	}
	if !SameDir(got.Repo, repo) {
		t.Errorf("Repo = %q, want %q", got.Repo, repo)
	}
	if got.TaskID != taskID {
		t.Errorf("TaskID = %q, want %q (the row that names the ground)", got.TaskID, taskID)
	}
	if got.Selector != TaskResumeGroundBranchKind {
		t.Errorf("Selector = %q, want %q — the answer must name its own question", got.Selector, TaskResumeGroundBranchKind)
	}
	// The report must be re-derivable: it carries the dir, branch, repo and row.
	rep := got.Report()
	for _, want := range []string{wt, branch, repo, taskID} {
		if !strings.Contains(rep, want) {
			t.Errorf("Report() must name %q, got:\n%s", want, rep)
		}
	}
}

// TestResumeGroundByWorktreeResolvesTheSameAnswer: the two spellings are ONE
// address. The path spelling additionally proves the resolver accepts the path
// identity rules the rest of the package uses (a trailing separator, an
// interior `./`) rather than string equality.
func TestResumeGroundByWorktreeResolvesTheSameAnswer(t *testing.T) {
	branch := "feat/resume-ground-wt"
	repo, wt, taskID := resumeGroundFixture(t, branch)

	byPath, err := TaskResumeGroundFor("", wt)
	if err != nil {
		t.Fatalf("TaskResumeGroundFor(worktree): %v", err)
	}
	byBranch, err := TaskResumeGroundFor(branch, "")
	if err != nil {
		t.Fatalf("TaskResumeGroundFor(branch): %v", err)
	}
	if byPath.Dir != byBranch.Dir || byPath.Branch != byBranch.Branch || byPath.TaskID != byBranch.TaskID || byPath.Repo != byBranch.Repo {
		t.Fatalf("the two spellings must answer ONE ground:\nby path:   %+v\nby branch: %+v", byPath, byBranch)
	}
	if byPath.Selector != TaskResumeGroundWorktreeKind {
		t.Errorf("Selector = %q, want %q", byPath.Selector, TaskResumeGroundWorktreeKind)
	}

	// Path IDENTITY, not string equality: `…/ground/` and `…/ground/./` are the
	// same directory.
	for _, spelling := range []string{wt + string(os.PathSeparator), filepath.Join(wt, ".")} {
		got, err := TaskResumeGroundFor("", spelling)
		if err != nil {
			t.Fatalf("TaskResumeGroundFor(%q): %v", spelling, err)
		}
		if got.TaskID != taskID {
			t.Errorf("spelling %q must resolve to row %s, got %q", spelling, taskID, got.TaskID)
		}
	}
	_ = repo
}

// TestResumeGroundNeverCreatesGround is the SHAPE test of the whole file: a
// selector that names ground the world does not contain must be REFUSED, and
// the refusal must leave the filesystem exactly as it found it. A resolver that
// silently ran `git worktree add` would pass a happy-path test and be the worst
// possible outcome of this mode (the operator asked to RESUME, and got empty
// ground plus a lost branch association).
func TestResumeGroundNeverCreatesGround(t *testing.T) {
	branch := "feat/resume-ground-absent"
	repo, wt, _ := resumeGroundFixture(t, branch)
	// The branch the ledger names, with NO worktree behind it: the caller must
	// be refused by name, not handed a freshly minted checkout.
	ghost := filepath.Join(filepath.Dir(wt), "never-created")

	before := resumeGroundWorktreeList(t, repo)
	beforeBranches := resumeGroundBranchList(t, repo)

	cases := []struct {
		name     string
		branch   string
		worktree string
		want     string
	}{
		{"unknown branch, no row at all", "feat/does-not-exist", "", "no ground to resume"},
		{"nonexistent path", "", filepath.Join(t.TempDir(), "nope"), "is not a work tree"},
		{"path inside the main checkout", "", repo, "MAIN checkout"},
		{"a directory that is not a worktree of the repo", "", t.TempDir(), "is not a work tree"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TaskResumeGroundFor(tc.branch, tc.worktree)
			if err == nil {
				t.Fatalf("must REFUSE, got %+v", got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal must name the reason %q, got: %v", tc.want, err)
			}
		})
	}

	// The ledger KNOWS the branch but no repo has it checked out: still a
	// refusal, and it reports what was tried.
	forBranchSeed(t, "t-ghost", taskStatusCompleted, "", repo, "feat/ledger-only", ghost)
	if _, err := TaskResumeGroundFor("feat/ledger-only", ""); err == nil {
		t.Fatal("a branch the ledger knows with NO live worktree must be refused (the mode never creates ground)")
	} else if !strings.Contains(err.Error(), "no ground to resume") {
		t.Errorf("refusal must say there is no ground, got: %v", err)
	}

	// NOTHING was created: the registrations and the branches are untouched.
	if got, want := resumeGroundWorktreeList(t, repo), before; got != want {
		t.Errorf("git worktree list changed:\nbefore:\n%s\nafter:\n%s", want, got)
	}
	if got, want := resumeGroundBranchList(t, repo), beforeBranches; got != want {
		t.Errorf("git branch changed:\nbefore:\n%s\nafter:\n%s", want, got)
	}
	if _, err := os.Stat(ghost); !os.IsNotExist(err) {
		t.Errorf("the resolver created %s — it must never create ground", ghost)
	}
}

// TestResumeGroundRefusesBothSelectorsAndNeither: the selectors are one
// address, not a filter pair — so BOTH and NEITHER are usage errors, each with
// its own sentence (an operator who passed both made a different mistake from
// one who passed none).
func TestResumeGroundRefusesBothSelectorsAndNeither(t *testing.T) {
	if _, err := TaskResumeGroundFor("", ""); err == nil {
		t.Fatal("neither selector must be refused")
	} else if !strings.Contains(err.Error(), "selector is required") {
		t.Errorf("neither-selector refusal must say a selector is required, got: %v", err)
	}
	if _, err := TaskResumeGroundFor("feat/x", "/tmp/somewhere"); err == nil {
		t.Fatal("both selectors must be refused")
	} else if !strings.Contains(err.Error(), "not a filter pair") {
		t.Errorf("both-selector refusal must say they are one ground, got: %v", err)
	}
}

// TestResumeGroundRefusesAWorktreeNoRowNames: the ground is real and registered
// — and NO row claims it. That is a refusal, because resuming it would mean
// inventing an owner (the ledger is the only thing that can say whose work it
// is; a directory alone cannot).
func TestResumeGroundRefusesAWorktreeNoRowNames(t *testing.T) {
	branch := "feat/resume-ground-unowned"
	repo, wt, _ := resumeGroundFixture(t, branch)
	// A second registration no row names.
	orphan := filepath.Join(filepath.Dir(wt), "orphan")
	resumeGroundGit(t, repo, "worktree", "add", orphan, "-b", "feat/resume-ground-orphan")

	got, err := TaskResumeGroundFor("", orphan)
	if err == nil {
		t.Fatalf("a registered worktree no row names must be refused, got %+v", got)
	}
	if !strings.Contains(err.Error(), "no ledger row names") {
		t.Errorf("refusal must name the missing row, got: %v", err)
	}
	// The registration SURVIVES the refusal (a refusal is a read, not a sweep).
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("the refusal must not remove the directory: %v", err)
	}
}

// TestResumeGroundPicksTheSameRowAsTheSharedPolicy: several rows name one
// ground — the LIVE owner wins, exactly as TaskForBranchResolvedRow decides it
// everywhere else, and the count of the passed-over rows is REPORTED rather
// than hidden.
func TestResumeGroundPicksTheSameRowAsTheSharedPolicy(t *testing.T) {
	branch := "feat/resume-ground-shared"
	repo, wt, _ := resumeGroundFixture(t, branch)
	forBranchSeed(t, "t-old", taskStatusCompleted, "", repo, branch, wt)

	got, err := TaskResumeGroundFor("", wt)
	if err != nil {
		t.Fatalf("TaskResumeGroundFor: %v", err)
	}
	// The fixture's live row is t-resume-ground; t-old is terminal.
	if got.TaskID != "t-resume-ground" {
		t.Errorf("TaskID = %q, want the LIVE owner t-resume-ground", got.TaskID)
	}
	if !strings.Contains(got.Detail, "2 row(s) name it") {
		t.Errorf("the detail must report the plurality it decided among, got: %q", got.Detail)
	}
}

// TestResumeGroundisAReadOnlyResolution pins the resolver's contract with the
// ledger: the row's status and deliverable are unchanged by any successful or
// refused resolution (no claim, no stamp, no write — the mode's whole point is
// that the GROUND is resolved, not that a row is touched).
func TestResumeGroundisAReadOnlyResolution(t *testing.T) {
	branch := "feat/resume-ground-readonly"
	_, wt, taskID := resumeGroundFixture(t, branch)

	beforeStatus, beforeDeliverable := resumeGroundRowState(t, taskID)
	if _, err := TaskResumeGroundFor("", wt); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := TaskResumeGroundFor("feat/no-such", ""); err == nil {
		t.Fatal("expected a refusal")
	}
	afterStatus, afterDeliverable := resumeGroundRowState(t, taskID)
	if beforeStatus != afterStatus || beforeDeliverable != afterDeliverable {
		t.Errorf("the resolution WROTE to the row: status %q→%q, deliverable %q→%q",
			beforeStatus, afterStatus, beforeDeliverable, afterDeliverable)
	}
}

// resumeGroundRowState reads one row's status and deliverable through a raw
// open (the test's own path, independent of the tools-layer accessors it is
// checking).
func resumeGroundRowState(t *testing.T, taskID string) (status, deliverable string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore process-lifetime handle closed at the end of the fixture
	if err := db.QueryRowContext(ctx,
		`SELECT status, deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&status, &deliverable); err != nil {
		t.Fatalf("read row %s: %v", taskID, err)
	}
	return status, deliverable
}

// worktreeList / branchList / runGit are the raw-git readings the "nothing was
// created" assertions compare before and after (the test's own instrument, not
// the resolver's).
func resumeGroundWorktreeList(t *testing.T, repo string) string {
	t.Helper()
	return resumeGroundGitOut(t, repo, "worktree", "list")
}

func resumeGroundBranchList(t *testing.T, repo string) string {
	t.Helper()
	return resumeGroundGitOut(t, repo, "branch", "--format=%(refname:short)")
}

func resumeGroundGitOut(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, repo, err, out)
	}
	return string(out)
}

func resumeGroundGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, repo, err, out)
	}
}

// ---------------------------------------------------------------------------
// THE DEFAULT SPELLING: the ROW is the input, the ground is derived from it.
//
// These pin the three properties that make `gopi worker --resume <task-id>` a
// first-class spelling rather than a convenience wrapper: it answers the SAME
// ground the two flags answer (one address, three spellings), it refuses by
// NAME when the row records no ground (never by creating one), and every
// failure is a sentence with a distinct repair.
// ---------------------------------------------------------------------------

// TestResumeGroundFromTaskIsTheSameGroundAsTheFlags is the headline property:
// naming the ROW and naming its GROUND must reach one answer. The row's
// deliverable already records both facts, so an operator holding the task id
// must not have to re-derive them from git.
func TestResumeGroundFromTaskIsTheSameGroundAsTheFlags(t *testing.T) {
	branch := "feat/resume-ground-task"
	repo, wt, taskID := resumeGroundFixture(t, branch)

	byTask, err := TaskResumeGroundForTask(taskID)
	if err != nil {
		t.Fatalf("TaskResumeGroundForTask(%s): %v", taskID, err)
	}
	if !SameDir(byTask.Dir, wt) {
		t.Errorf("Dir = %q, want the row's recorded worktree %q", byTask.Dir, wt)
	}
	if byTask.Branch != branch {
		t.Errorf("Branch = %q, want %q", byTask.Branch, branch)
	}
	if !SameDir(byTask.Repo, repo) {
		t.Errorf("Repo = %q, want the row's deliverable repo %q", byTask.Repo, repo)
	}
	if byTask.TaskID != taskID {
		t.Errorf("TaskID = %q, want %q (the row is the input here, not the answer)", byTask.TaskID, taskID)
	}
	if byTask.Selector != TaskResumeGroundTaskKind {
		t.Errorf("Selector = %q, want %q — the answer must name its own question", byTask.Selector, TaskResumeGroundTaskKind)
	}
	// ONE ADDRESS: the flag spellings must resolve to the very same ground, so a
	// caller can pick whichever it holds without the guards seeing two places.
	byBranch, err := TaskResumeGroundFor(branch, "")
	if err != nil {
		t.Fatalf("TaskResumeGroundFor(branch): %v", err)
	}
	byWorktree, err := TaskResumeGroundFor("", wt)
	if err != nil {
		t.Fatalf("TaskResumeGroundFor(worktree): %v", err)
	}
	for _, other := range []TaskResumeGround{byBranch, byWorktree} {
		if !SameDir(other.Dir, byTask.Dir) {
			t.Errorf("the spellings must answer ONE ground: task %q vs %s %q", byTask.Dir, other.Selector, other.Dir)
		}
		if other.Branch != byTask.Branch || !SameDir(other.Repo, byTask.Repo) || other.TaskID != byTask.TaskID {
			t.Errorf("the spellings must answer ONE ground:\nby task:     %+v\nby %s: %+v", byTask, other.Selector, other)
		}
	}
	// The report is re-derivable by a reader who was not here: the row, its
	// status, the directory, the branch and the repo all ride the line.
	rep := byTask.Report()
	for _, want := range []string{wt, branch, repo, taskID, taskStatusInProgress} {
		if !strings.Contains(rep, want) {
			t.Errorf("Report() must name %q, got:\n%s", want, rep)
		}
	}
}

// TestResumeGroundFromTaskRefusesWithANamedReason is the negative half. Each
// shape has its own repair, so each assertion checks the SENTENCE names it —
// and the last two prove the mode created nothing on the way to refusing (the
// resolve-only contract, Rule 15b).
func TestResumeGroundFromTaskRefusesWithANamedReason(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := resumeGroundRepo(t, "feat/resume-ground-task-refusals")
	const (
		withGround    = "t-resume-task-with-ground"
		withoutGround = "t-resume-task-no-ground"
		ghostWorktree = "t-resume-task-ghost-worktree"
	)
	forBranchSeed(t, withGround, taskStatusInProgress, "", repo, "feat/resume-ground-task-refusals", wt)
	forBranchSeed(t, withoutGround, taskStatusPending, "", repo, "", "")
	forBranchSeed(t, ghostWorktree, taskStatusInProgress, "", repo, "",
		filepath.Join(filepath.Dir(wt), "the-ground-that-was-swept"))

	// A row that records NEITHER axis: there is nothing to resume where the work
	// sits, and the refusal says so rather than falling back to a created ground.
	_, err := TaskResumeGroundForTask(withoutGround)
	if err == nil {
		t.Fatalf("a row naming no ground must be refused (got a resolution)")
	}
	if !strings.Contains(err.Error(), "names no ground") {
		t.Errorf("the refusal must say the row names no ground, got: %v", err)
	}

	// A row whose recorded worktree is gone (merged and swept): the mode never
	// mints a replacement — and the sentence is the ONE ground judge's
	// (devworkspace.ResolveGroundState) verdict, not a second opinion: it names
	// WHICH model the directory would have had and WHAT was checked.
	_, err = TaskResumeGroundForTask(ghostWorktree)
	if err == nil {
		t.Fatalf("a row whose recorded worktree is gone must be refused (got a resolution)")
	}
	if !strings.Contains(err.Error(), "NOT a live ground") {
		t.Errorf("the refusal must say the ground is gone, got: %v", err)
	}
	for _, want := range []string{"model missing", "has no .git entry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must carry the resolver's own detail (%q missing): %v", want, err)
		}
	}

	// A row that does not exist: distinct from "the row exists and records
	// nothing", because the repairs differ (Rule 15).
	_, err = TaskResumeGroundForTask("t-no-such-row-anywhere")
	if err == nil {
		t.Fatal("an unknown row must be refused")
	}
	if !strings.Contains(err.Error(), "no ledger row to resume") {
		t.Errorf("the refusal must say there is no such row, got: %v", err)
	}

	// An EMPTY id is its own sentence (the mode's default spelling was not
	// supplied at all) and must not be reported as a lookup failure.
	_, err = TaskResumeGroundForTask("   ")
	if err == nil {
		t.Fatal("an empty task id must be refused")
	}
	if !strings.Contains(err.Error(), "task id is required") {
		t.Errorf("the empty-id refusal must ask for the id, got: %v", err)
	}

	// NOTHING WAS CREATED, anywhere: the ghost directory must not exist and the
	// registrations must be untouched (the property the whole face rests on).
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(wt), "the-ground-that-was-swept")); statErr == nil {
		t.Error("the resolver created a worktree — it must never create ground")
	}
	if got := resumeGroundWorktreeList(t, repo); !strings.Contains(got, wt) {
		t.Errorf("the fixture's registration must be untouched, git worktree list says:\n%s", got)
	}

	// And the HAPPY path still resolves after all those refusals — a refusal
	// that damaged the resolver would show up here.
	got, err := TaskResumeGroundForTask(withGround)
	if err != nil {
		t.Fatalf("the resumable row must still resolve: %v", err)
	}
	if !SameDir(got.Dir, wt) {
		t.Errorf("Dir = %q, want %q", got.Dir, wt)
	}
}

// TestResumeGroundFromTaskResolvesByBranchWhenNoWorktreeIsRecorded: a row whose
// deliverable carries a BRANCH and no worktree is the ordinary in-flight shape
// (the worktree is backfilled by the first git-touching round). The default
// spelling must reach the same live ground the `--branch` spelling reaches
// rather than refusing on a field that was simply not stamped yet.
func TestResumeGroundFromTaskResolvesByBranchWhenNoWorktreeIsRecorded(t *testing.T) {
	const branch = "feat/resume-ground-task-branch-only"
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := resumeGroundRepo(t, branch)
	const taskID = "t-resume-task-branch-only"
	forBranchSeed(t, taskID, taskStatusInProgress, "", repo, branch, "")

	got, err := TaskResumeGroundForTask(taskID)
	if err != nil {
		t.Fatalf("TaskResumeGroundForTask (branch-only row): %v", err)
	}
	if !SameDir(got.Dir, wt) {
		t.Errorf("Dir = %q, want the branch's registered worktree %q", got.Dir, wt)
	}
	if got.Branch != branch {
		t.Errorf("Branch = %q, want %q", got.Branch, branch)
	}
	// The report must render the ABSENT axis explicitly rather than leave a gap
	// a reader reads as "we did not look" (the package's absence marker).
	if !strings.Contains(got.Report(), taskGroundKeyAbsent) {
		t.Errorf("the report must render the missing worktree axis as %q, got:\n%s",
			taskGroundKeyAbsent, got.Report())
	}
}

// TestResumeGroundFromTaskFallsBackToTheRegistrationBranch: a row whose blob
// carries a WORKTREE and no branch (an older stamp) still yields a complete
// answer, because the registration itself knows which branch is checked out
// there. Without this the caller would be handed an empty branch axis and the
// child resume — which reports the branch it resumed — would have nothing to
// name.
func TestResumeGroundFromTaskFallsBackToTheRegistrationBranch(t *testing.T) {
	const branch = "feat/resume-ground-task-wt-only"
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := resumeGroundRepo(t, branch)
	const taskID = "t-resume-task-wt-only"
	forBranchSeed(t, taskID, taskStatusInProgress, "", repo, "", wt)

	got, err := TaskResumeGroundForTask(taskID)
	if err != nil {
		t.Fatalf("TaskResumeGroundForTask (worktree-only row): %v", err)
	}
	if !SameDir(got.Dir, wt) {
		t.Errorf("Dir = %q, want %q", got.Dir, wt)
	}
	if got.Branch != branch {
		t.Errorf("Branch = %q, want the registration's own branch %q", got.Branch, branch)
	}
}

// resumeGroundDeclareRepo writes a settings.json declaring `repo` in the repos
// table (Rule 12's contract) and points GOPI_SETTINGS at it — the channel a
// branch with no ledger row is ordinarily reachable through, and the one
// TaskGitProvenanceFor reads (taskGitProvenanceRepos candidate ②).
func resumeGroundDeclareRepo(t *testing.T, repo string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	body := `{"repos":{"` + repo + `":{"build":""}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv("GOPI_SETTINGS", path)
}

// resumeGroundProbeGit answers one question about a repository without failing
// the test — the negative cases below need git's refusal as a RESULT.
func resumeGroundProbeGit(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// TestResumeGroundNoRowReportsTheLiveGroundItMeasures is the reported defect
// (measured 2026-09-27 on branch fix/pushback-rule31-evidence): the branch has
// NO ledger row and a LIVE registered worktree. The pre-fix sentence —
// "no ground to resume — no ledger row names --branch B" — asserted a fact
// about the FILESYSTEM from a fact about the LEDGER, and the two disagreed; the
// operator went looking for a ground that was standing right there.
//
// The refusal must now name what was MEASURED: the repository, the worktree
// directory, the tip, and the reason a row-less ground cannot be resumed (the
// checkpoint lives in the row's run directory) — never "there is no ground".
func TestResumeGroundNoRowReportsTheLiveGroundItMeasures(t *testing.T) {
	const branch = "fix/resume-ground-no-row"
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := resumeGroundRepo(t, branch)
	resumeGroundDeclareRepo(t, repo)
	tip, err := resumeGroundProbeGit(t, repo, "rev-parse", "--short", branch)
	if err != nil || tip == "" {
		t.Fatalf("fixture: rev-parse %s: %v: %s", branch, err, tip)
	}

	got, rerr := TaskResumeGroundFor(branch, "")
	if rerr == nil {
		t.Fatalf("a row-less ground must still be REFUSED (resume dies at the checkpoint), got %+v", got)
	}
	msg := rerr.Error()
	if strings.Contains(msg, "no ground to resume") {
		t.Errorf("the ground EXISTS — the refusal must not claim there is none: %v", rerr)
	}
	for _, want := range []string{"no ledger row names", wt, repo, tip, "cannot be resumed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal must name what it measured (%q): %v", want, rerr)
		}
	}
	// The measurement is a READ: the ground it names is untouched.
	if _, statErr := os.Stat(wt); statErr != nil {
		t.Errorf("the refusal must not remove the ground it found: %v", statErr)
	}
}

// TestResumeGroundNoRowRefusesTheMainCheckoutAsGround: the same no-row case
// where the branch is checked out in the repository's MAIN checkout. The ground
// is NOT a deliverable ground, and the sentence must say which of the two
// shapes it found — an operator sent looking for a worktree that never existed
// is the failure this distinction removes.
func TestResumeGroundNoRowRefusesTheMainCheckoutAsGround(t *testing.T) {
	const branch = "fix/resume-ground-main-checkout"
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo := t.TempDir()
	resumeGroundGit(t, repo, "init", "-q", "-b", "master")
	resumeGroundGit(t, repo, "config", "user.email", "t@t")
	resumeGroundGit(t, repo, "config", "user.name", "t")
	resumeGroundGit(t, repo, "commit", "-q", "--allow-empty", "-m", "seed")
	resumeGroundGit(t, repo, "checkout", "-q", "-b", branch)
	resumeGroundDeclareRepo(t, repo)

	if _, err := TaskResumeGroundFor(branch, ""); err == nil {
		t.Fatal("the main checkout is not a deliverable ground — must be refused")
	} else {
		msg := err.Error()
		if !strings.Contains(msg, "MAIN checkout") {
			t.Errorf("the refusal must name the main checkout: %v", err)
		}
		if strings.Contains(msg, "no ground to resume") {
			t.Errorf("the branch IS checked out — the refusal must not say there is no ground: %v", err)
		}
	}
}

// TestResumeGroundNoRowDistinguishesAStaleRegistration: git still REGISTERS a
// worktree on the branch and the directory is GONE. That is neither "the ground
// is live" nor "no such branch", and the refusal says so — the third measured
// state, so an operator can tell a swept ground from a missing ref.
func TestResumeGroundNoRowDistinguishesAStaleRegistration(t *testing.T) {
	const branch = "fix/resume-ground-stale"
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := resumeGroundRepo(t, branch)
	resumeGroundDeclareRepo(t, repo)
	if err := os.RemoveAll(wt); err != nil {
		t.Fatalf("remove the ground: %v", err)
	}

	if _, err := TaskResumeGroundFor(branch, ""); err == nil {
		t.Fatal("a registration whose directory is gone is not a resumable ground")
	} else {
		msg := err.Error()
		if !strings.Contains(msg, "directory is gone") || !strings.Contains(msg, wt) {
			t.Errorf("the refusal must name the stale registration and its path: %v", err)
		}
	}
}

// TestResumeGroundNoRowNamesAMissingRef: no row and no ground — the branch
// itself does not exist in the repository the declared table names. The refusal
// keeps its original force here, and names the repository it asked.
func TestResumeGroundNoRowNamesAMissingRef(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, _ := resumeGroundRepo(t, "fix/resume-ground-other")
	resumeGroundDeclareRepo(t, repo)

	const branch = "fix/resume-ground-never-existed"
	_, err := TaskResumeGroundFor(branch, "")
	if err == nil {
		t.Fatal("a branch nobody has must be refused")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no ground to resume") || !strings.Contains(msg, repo) {
		t.Errorf("the refusal must say there is no ground and name the repo it read: %v", err)
	}
	// AND IT MUST SAY WHERE THAT REPO CAME FROM. `--branch` takes no repo
	// input, so the repo in this sentence is a candidate the LEDGER named; an
	// unqualified path reads as a verdict on the BRANCH itself (measured
	// 2026-10-07, row 01a112e3: the branch was present in the repo the operator
	// could see and absent only from the inferred candidate). Removing the
	// qualifier is the regression this assertion exists to catch — the failure
	// is silent otherwise, because a bare path is still a plausible sentence.
	if !strings.Contains(msg, "inferred") {
		t.Errorf("the repo axis is inferred — the refusal must name the derivation, not print a bare path "+
			"(otherwise it reads as a verdict on the branch): %v", err)
	}
}

// TestResumeGroundTakesACloneWorkspaceAndTheAdvisoryAgrees is THE regression this
// epic exists for (measured 2026-10-19, row 01a150b3): the CLONE channel's
// workspace — a standalone clone, `.git` a DIRECTORY, registered in NO worktree
// list — was refused by `gopi worker --resume` ("NOT a live linked worktree …
// the ground is gone") in the same minute the ledger's `ground (resolved now)`
// line called the same directory "verified live".
//
// One directory, one row, TWO faces, and the faces must agree — because they now
// ask the SAME function (devworkspace.ResolveGroundState). The test pins the
// agreement, not just the two verdicts: a future reader that re-derives
// liveness locally makes this fail.
func TestResumeGroundTakesACloneWorkspaceAndTheAdvisoryAgrees(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := resumeGroundRepo(t, "feat/resume-clone-workspace")
	// The CLONE model: a standalone clone of the fixture repo, unregistered.
	clone := filepath.Join(filepath.Dir(wt), "clone-workspace")
	deliverableWTGit(t, filepath.Dir(clone), "clone", "-q", repo, clone)
	if info, err := os.Stat(filepath.Join(clone, ".git")); err != nil || !info.IsDir() {
		t.Fatalf("the fixture must be a CLONE (.git a directory): err=%v", err)
	}
	const row = "t-resume-clone-workspace"
	forBranchSeed(t, row, taskStatusInProgress, "", repo, "", clone)

	// ① the RESUME face: a clone workspace is a ground the worker may take over.
	ground, err := TaskResumeGroundForTask(row)
	if err != nil {
		t.Fatalf("a CLONE workspace must be resumable — a clone is registered nowhere and needs no "+
			"registration; got: %v", err)
	}
	if !SameDir(ground.Dir, clone) {
		t.Errorf("resolved dir = %q, want the clone workspace %q", ground.Dir, clone)
	}
	if ground.Branch == "" {
		t.Errorf("a clone's branch must be resolved, got an empty one: %+v", ground)
	}

	// ② the LEDGER face, on the SAME row: the advisory verifies the same
	// directory live (no stale marker, no "NOT a live ground").
	show, err := TaskShow(row)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := advisoryGroundLine(t, show)
	if !strings.Contains(line, TaskShowGroundExistsMarker) {
		t.Errorf("the ledger line must verify the clone workspace LIVE (the resume face just did): %q", line)
	}
	if strings.Contains(line, TaskShowGroundStaleMarker) || strings.Contains(line, "NOT a live ground") {
		t.Errorf("the ledger line must not call the clone workspace stale: %q", line)
	}

	// ③ THE AGREEMENT, stated as the rule rather than as two outputs: the
	// ledger's verdict and the resume's verdict are the SAME function's answer.
	if st := devworkspace.ResolveGroundState(clone); !st.Alive || st.Model != devworkspace.GroundModelClone {
		t.Fatalf("ResolveGroundState must call a clone workspace alive/clone, got %+v", st)
	}
}

// TestResumeGroundRefusesASweptLinkedWorktreeAndTheAdvisoryAgrees is the OTHER
// direction, and it is the one the advisory got wrong: a linked worktree whose
// registration was swept still EXISTS on disk (so "does the path exist" said
// verified live) while the resume face correctly said gone. Both faces now say
// gone, from the same reading.
func TestResumeGroundRefusesASweptLinkedWorktreeAndTheAdvisoryAgrees(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := resumeGroundRepo(t, "feat/resume-swept-linked")
	const row = "t-resume-swept-linked"
	forBranchSeed(t, row, taskStatusInProgress, "", repo, "feat/resume-swept-linked", wt)
	// Sweep the registration without touching the directory: `worktree remove`
	// would take the directory with it, and the shape under test is a directory
	// that is still there with nothing registering it.
	reg := filepath.Join(repo, ".git", "worktrees")
	entries, err := os.ReadDir(reg)
	if err != nil {
		t.Fatalf("read the registrations: %v", err)
	}
	for _, e := range entries {
		_ = os.Remove(filepath.Join(reg, e.Name(), "gitdir")) //stylegate:ignore best-effort break of an abandoned test lock; the lock is advisory
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the fixture's directory must survive the sweep: %v", err)
	}

	if _, err := TaskResumeGroundForTask(row); err == nil {
		t.Fatalf("a swept linked worktree must be refused by the resume face")
	} else if !strings.Contains(err.Error(), "NOT a live ground") {
		t.Errorf("the refusal must come from the ONE judge, got: %v", err)
	}

	show, err := TaskShow(row)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	line := advisoryGroundLine(t, show)
	if !strings.Contains(line, TaskShowGroundStaleMarker) {
		t.Errorf("the ledger line must now report the swept ground STALE (it exists on disk but nothing "+
			"registers it — the reading `os.Stat` alone could not see): %q", line)
	}
}

// TestResumeGroundByWorktreeTakesACloneWorkspace is the other half of
// TestResumeGroundTakesACloneWorkspaceAndTheAdvisoryAgrees, and it is the
// spelling that stayed broken (measured 2026-10-27, this epic's own ground):
//
//	$ gopi worker --resume --worktree /home/nick/workspace/01a17aba-…
//	gopi worker: no ground to resume: --worktree /home/nick/workspace/01a17aba-… is the
//	MAIN checkout of /home/nick/workspace/01a17aba-… — a repo-aware row works in a
//	LINKED worktree, and the main checkout is not a deliverable ground
//	exit=1
//	$ gopi worker --resume 01a17aba-…          # the SAME directory, the default spelling
//	resume ground: dir=/home/nick/workspace/01a17aba-… branch=master … model clone
//
// ONE directory, TWO spellings, OPPOSITE answers — and the refused one is the
// shape this epic keeps. The mechanism was structural, not a wording slip: in the
// CLONE model `git rev-parse --git-common-dir` answers `<clone>/.git`, so
// `worktreeSweepMainCheckoutOf` answers the clone ITSELF and the main-checkout
// guard fired on every clone.
//
// The test pins BOTH directions, because the repair must not become a hole: a
// clone workspace a row names RESOLVES, and a clone-model directory that SERVES
// linked worktrees (i.e. is a shared checkout) is still refused with the same
// sentence.
func TestResumeGroundByWorktreeTakesACloneWorkspace(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := resumeGroundRepo(t, "feat/resume-clone-by-path")
	// The CLONE model: a standalone clone, registered in no worktree list.
	clone := filepath.Join(filepath.Dir(wt), "clone-by-path")
	deliverableWTGit(t, filepath.Dir(clone), "clone", "-q", repo, clone)
	if info, err := os.Stat(filepath.Join(clone, ".git")); err != nil || !info.IsDir() {
		t.Fatalf("the fixture must be a CLONE (.git a directory): err=%v", err)
	}
	const row = "t-resume-clone-by-path"
	// The row records the SOURCE repository as its repo (that is what the clone
	// channel stamps) and the clone as its worktree.
	forBranchSeed(t, row, taskStatusInProgress, "", repo, "master", clone)

	// THE REPAIR: the path spelling accepts a clone workspace.
	got, err := TaskResumeGroundFor("", clone)
	if err != nil {
		t.Fatalf("--worktree must accept a CLONE workspace (the shape the clone channel makes): %v", err)
	}
	if !SameDir(got.Dir, clone) {
		t.Errorf("Dir = %q, want the clone %q", got.Dir, clone)
	}
	if got.Branch == "" {
		t.Errorf("the clone's branch must be resolved, got %+v", got)
	}
	if got.TaskID != row {
		t.Errorf("TaskID = %q, want the row that names the ground %q", got.TaskID, row)
	}
	// ONE ADDRESS: the two spellings must agree about ONE directory. This is what
	// the pre-fix code violated — not "the message was unclear".
	byTask, err := TaskResumeGroundForTask(row)
	if err != nil {
		t.Fatalf("the default spelling must resolve the same ground: %v", err)
	}
	if !SameDir(byTask.Dir, got.Dir) || byTask.Branch != got.Branch {
		t.Errorf("the two spellings disagree about ONE ground:\n--worktree: %+v\n--task id:  %+v", got, byTask)
	}

	// THE GUARD THAT MUST SURVIVE: a clone-model directory that SERVES linked
	// worktrees is a shared checkout, and a resume that closes a row out would
	// remove the tree every other run hangs off. The fixture's `repo` is exactly
	// that shape (it registered `wt` above).
	serves, why := taskResumeGroundSharedCheckoutReason(repo)
	if !serves {
		t.Fatalf("the fixture repo serves a linked worktree — the shared-checkout reading must say so")
	}
	if !strings.Contains(why, "linked worktree") {
		t.Errorf("the reason must name the measured evidence, got: %q", why)
	}
	if _, err := TaskResumeGroundFor("", repo); err == nil {
		t.Fatal("a clone-model directory that serves linked worktrees is a SHARED checkout and must be refused")
	} else if !strings.Contains(err.Error(), "MAIN checkout") {
		t.Errorf("the refusal must keep the sentence an operator already knows, got: %v", err)
	}
}

// TestResumeGroundByWorktreeRefusesADeclaredRepo: the SECOND instrument of the
// clone arm. A path the settings `repos` table declares is the host's shared
// repository even when nothing currently hangs off it — the declaration IS the
// operator's statement that it is shared. Without this arm the guard would be
// "does anything happen to be registered right now", which is a fact about
// timing, not about ownership.
func TestResumeGroundByWorktreeRefusesADeclaredRepo(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := resumeGroundRepo(t, "feat/resume-declared-repo")
	declared := filepath.Join(filepath.Dir(wt), "declared")
	deliverableWTGit(t, filepath.Dir(declared), "clone", "-q", repo, declared)
	resumeGroundDeclareRepo(t, declared)

	if _, err := TaskResumeGroundFor("", declared); err == nil {
		t.Fatal("a DECLARED repository is a shared checkout and must be refused as one")
	} else {
		msg := err.Error()
		if !strings.Contains(msg, "MAIN checkout") || !strings.Contains(msg, "repos") {
			t.Errorf("the refusal must name the main checkout and the table it read, got: %v", err)
		}
	}
	// A clone of the SAME repo that nothing declares is still a ground: the
	// declaration is scoped to the path, never to the repository's identity.
	workspace := filepath.Join(filepath.Dir(wt), "undeclared-workspace")
	deliverableWTGit(t, filepath.Dir(workspace), "clone", "-q", repo, workspace)
	const row = "t-resume-undeclared-workspace"
	forBranchSeed(t, row, taskStatusInProgress, "", repo, "master", workspace)
	if _, err := TaskResumeGroundFor("", workspace); err != nil {
		t.Fatalf("an UNDECLARED clone workspace must stay resumable: %v", err)
	}
}
