package tools

// Regression tests for the three worktree-removal refusal arms (epic
// 01a0a6ff, 2026-09-16). Each test is the minimal repro of one measured
// refusal, written so a future re-inline fails here rather than silently in
// production:
//
//	case 1 — guard ② must NAME untracked-only residue apart from tracked
//	         uncommitted work (the actionable half of a dirty refusal);
//	case 2 — the producer pass must free a linked worktree THIS process created
//	         that no ledger row owns;
//	case 3 — the verify path must be told where the delivered work already lives.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// ── case 1: the dirty refusal speaks two states ──────────────────────────────

// TestReclaimDirtyReasonNamesUntrackedResidueApartFromWork is the RED test for
// case 1: before the split, EVERY non-empty porcelain answered
// reclaimSkipDirtyReason, so a run's own untracked residue was reported as if
// it were somebody's uncommitted work (and the salvage lane had nothing to
// capture while the residue named no hygiene debt anywhere).
//
// MERGE NOTE (resolve merge feat/worktree-refusal-arms-v2 into master): the two
// sides disagreed on ONE point — whether the refusal also QUOTES the offending
// entries back. Master returned the named sentence alone; the branch appends the
// raw porcelain body (Rule 15: the refusal names the files that caused it) and
// the finish-cleanup arm of master already quotes it the same way
// (finishCleanupWorktreeDirtyReason appends the trimmed body to its prefix).
// The merge kept the QUOTING behaviour — one spelling, shared with arm 3's
// gate — so the assertions below check the NAMED opening sentence, which is
// exactly what master's test is about (the two states must not be conflated),
// and additionally pin the quoted body rather than dropping it.
func TestReclaimDirtyReasonNamesUntrackedResidueApartFromWork(t *testing.T) {
	cases := []struct {
		name      string
		porcelain string
		want      string
	}{
		{"clean", "", ""},
		{"untracked only", "?? build.out\n?? scratch/probe.txt\n", reclaimUntrackedResidueMarker},
		{"tracked modification", " M internal/tools/task_reclaim.go\n", reclaimSkipDirtyReason},
		{"tracked and untracked", "?? build.out\n M internal/tools/task_reclaim.go\n", reclaimSkipDirtyReason},
		{"staged add", "A  internal/tools/new.go\n", reclaimSkipDirtyReason},
		{"staged deletion", "D  internal/tools/old.go\n", reclaimSkipDirtyReason},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reclaimDirtyReason(tc.porcelain)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("reclaimDirtyReason(%q) = %q, want the clean verdict", tc.porcelain, got)
				}
				return
			}
			if !strings.HasPrefix(got, tc.want) {
				t.Fatalf("reclaimDirtyReason(%q) = %q, want it to open with %q", tc.porcelain, got, tc.want)
			}
			// Rule 15: the refusal also names the entries that caused it — the
			// half of the branch's classifier this merge kept.
			if body := strings.TrimSpace(tc.porcelain); !strings.Contains(got, strings.Split(body, "\n")[0]) {
				t.Fatalf("reclaimDirtyReason(%q) = %q, want the offending entry quoted back", tc.porcelain, got)
			}
		})
	}
}

// TestFinishCleanupWorktreeDirtyReasonMatchesReclaim pins the ONE reading both
// arms share (Rule 14): the finish-cleanup gate and the reclaim gate must not
// drift on what "dirty" means.
func TestFinishCleanupWorktreeDirtyReasonMatchesReclaim(t *testing.T) {
	rep := &FinishCleanupReport{}
	if got := finishCleanupWorktreeDirtyReason(rep, finishCleanupNoOutput); got != "" {
		t.Fatalf("the clean sentinel must not be a refusal, got %q", got)
	}
	// Same classification, both directions: an untracked-only body refuses
	// with the untracked prefix, a tracked body with the tracked prefix.
	if got := finishCleanupWorktreeDirtyReason(rep, "?? x\n"); !strings.HasPrefix(got, finishCleanupDirtyUntrackedPrefix) {
		t.Fatalf("untracked-only body = %q, want the untracked prefix", got)
	}
	if got := finishCleanupWorktreeDirtyReason(rep, " M y\n"); !strings.HasPrefix(got, finishCleanupDirtyTrackedPrefix) {
		t.Fatalf("tracked body = %q, want the tracked prefix", got)
	}
	if len(rep.Steps) == 0 || !strings.Contains(rep.Steps[len(rep.Steps)-1].Detail, finishCleanupUntrackedResidueNote) {
		t.Fatalf("the untracked refusal must leave a durable step record, steps=%+v", rep.Steps)
	}
}

// TestReclaimDispositionUntrackedResidueStillRefuses: the refusal itself is
// UNCHANGED — only its name. A worktree with untracked files is still kept.
func TestReclaimDispositionUntrackedResidueStillRefuses(t *testing.T) {
	root := t.TempDir()
	repo, wt := worktreeRefusalFixture(t, root)
	if err := os.WriteFile(filepath.Join(wt, "build.out"), []byte("residue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	row := reclaimDeadHolderRow{TaskID: "t-untracked", Repo: repo, Branch: "feat/x", Worktree: wt}
	disp, why := reclaimDisposition(row, newReclaimPrunePlan())
	if disp != reclaimSkip {
		t.Fatalf("untracked residue must still be REFUSED (kept), got disp=%v why=%q", disp, why)
	}
	if !strings.HasPrefix(why, reclaimUntrackedResidueMarker) {
		t.Fatalf("refusal reason = %q, want it to open with the named untracked-residue marker", why)
	}
	// Rule 15: the refusal names the file that holds the tree.
	if !strings.Contains(why, "build.out") {
		t.Fatalf("refusal reason = %q, want the untracked file named", why)
	}
	if _, err := os.Stat(filepath.Join(wt, "build.out")); err != nil {
		t.Fatalf("the residue must not be deleted by a refusal: %v", err)
	}
}

// ── case 2: the unowned registration this process created ────────────────────

// worktreeRefusalFixture builds a repo with one linked worktree (branch
// feat/x) and returns (repo, worktree).
func worktreeRefusalFixture(t *testing.T, root string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	git := func(dir string, args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(root, "init", "-q", repo)
	git(repo, "checkout", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", "base.txt")
	git(repo, "commit", "-q", "-m", "base")
	git(repo, "worktree", "add", "-q", wt, "-b", "feat/x")
	return repo, wt
}

// TestReclaimSelfOwnWhyYoungerThanFloorIsKept: a registration younger than the
// floor is never freed — a concurrent round of the same process may be creating
// it right now (the anti-race guard that makes an ownership-independent pass
// safe).
func TestReclaimSelfOwnWhyYoungerThanFloorIsKept(t *testing.T) {
	repo, wt := worktreeRefusalFixture(t, t.TempDir())
	why := reclaimSelfOwnWhy(worktreeRegistration{Path: wt, Branch: "feat/x"}, repo, scratch.ScanHolders())
	if !strings.Contains(why, "younger than the") {
		t.Fatalf("a fresh registration must be kept by the age floor, got %q", why)
	}
}

// ageRegistrationRecord backdates the registration's OWN creation record: the
// worktree-private git dir and the `gitdir` backlink inside it — the two records
// worktreeSweepCreatedAt reads. NOT the main repo's commondir (2026-09-16): for a
// worktree of the main repository that file carries the REPOSITORY's age, so
// aging it would not make this registration old (and reading it made a fresh
// worktree look ancient).
func ageRegistrationRecord(t *testing.T, gitdir string, old time.Time) {
	t.Helper()
	if err := os.Chtimes(gitdir, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(gitdir, worktreeSweepGitdirFileName), old, old); err != nil {
		t.Fatal(err)
	}
}

// TestReclaimSelfOwnReclaimsUnownedAgedRegistration is the RED test for case 2:
// a linked worktree this process created, named by NO ledger row, clean, aged
// past the floor and inside a declared root, must be FREED — that is the leak
// every ledger-keyed arm is structurally blind to.
func TestReclaimSelfOwnReclaimsUnownedAgedRegistration(t *testing.T) {
	root := t.TempDir()
	repo, wt := worktreeRefusalFixture(t, root)
	_ = repo
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db")) // empty ledger: nobody owns it
	// Age the registration past the floor by backdating the creation record git
	// keeps (the same file the sweeper measures age on).
	gitdirFile := filepath.Join(wt, ".git")
	raw, err := os.ReadFile(gitdirFile)
	if err != nil {
		t.Fatal(err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(string(raw), "gitdir:"))
	old := time.Now().Add(-2 * reclaimSelfOwnMinAge)
	ageRegistrationRecord(t, gitdir, old)

	lines := reclaimSelfOwnRepo(repo, nil)
	var freed bool
	for _, l := range lines {
		if strings.Contains(l, reclaimSelfOwnFreedMarker) && strings.Contains(l, wt) {
			freed = true
		}
	}
	if !freed {
		t.Fatalf("the unowned, aged, clean registration must be freed; lines=%v", lines)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Fatalf("the worktree directory must be gone after the release")
	}
	// The BRANCH is never touched — it is the deliverable.
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "feat/x").Output(); err != nil || len(out) == 0 {
		t.Fatalf("the branch must survive the directory's release: %v", err)
	}
}

// TestReclaimSelfOwnKeepsDirtyAndLiveGround: the two safety guards are unchanged
// by the wider ownership rule — a dirty registration and one a live process
// holds are both KEPT, each with its own named reason.
func TestReclaimSelfOwnKeepsDirtyAndLiveGround(t *testing.T) {
	root := t.TempDir()
	repo, wt := worktreeRefusalFixture(t, root)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	gitdirRaw, err := os.ReadFile(filepath.Join(wt, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(string(gitdirRaw), "gitdir:"))
	old := time.Now().Add(-2 * reclaimSelfOwnMinAge)
	ageRegistrationRecord(t, gitdir, old)
	// A TRACKED modification pins it (real work).
	if err := os.WriteFile(filepath.Join(wt, "base.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	why := reclaimSelfOwnWhy(worktreeRegistration{Path: wt, Branch: "feat/x"}, repo, scratch.ScanHolders())
	// MERGE NOTE: the refusal also quotes the offending entry back (Rule 15,
	// kept from the branch's classifier), so the opening sentence is what is
	// pinned here.
	if !strings.HasPrefix(why, reclaimSkipDirtyReason) {
		t.Fatalf("a tracked change must keep the registration, got %q", why)
	}
}

// TestReclaimSelfOwnKeepsLiveSiblingsGround is the SAFETY test for the widened
// rule: a live row naming the registration as its worktree keeps it, even when
// there is no live PROCESS inside (a session idles between rounds: its build
// runs in the background, its agent is thinking). Without this veto an
// ownership-independent pass would free a sibling session's ground — the one
// failure mode that makes this pass worse than the leak it closes.
func TestReclaimSelfOwnKeepsLiveSiblingsGround(t *testing.T) {
	root := t.TempDir()
	repo, wt := worktreeRefusalFixture(t, root)
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	seedLiveStatusRow(t, "t-sibling-live", repo, "feat/x", wt, "otherhost:1")
	// Age the registration past the floor so ONLY the ownership veto can keep it.
	gitdirRaw, err := os.ReadFile(filepath.Join(wt, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(string(gitdirRaw), "gitdir:"))
	old := time.Now().Add(-2 * reclaimSelfOwnMinAge)
	ageRegistrationRecord(t, gitdir, old)

	lines := reclaimSelfOwnRepo(repo, nil)
	for _, l := range lines {
		if strings.Contains(l, reclaimSelfOwnFreedMarker) {
			t.Fatalf("a live sibling session's worktree must never be freed: %v", lines)
		}
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the live-owned worktree must survive the pass: %v", err)
	}
	var sawOwner bool
	for _, l := range lines {
		if strings.Contains(l, "owned by live task t-sibling-live") {
			sawOwner = true
		}
	}
	if !sawOwner {
		t.Fatalf("the keep must NAME the live owner (Rule 15), lines=%v", lines)
	}
}

// ── case 3: the verifier is told where the work lives ────────────────────────
