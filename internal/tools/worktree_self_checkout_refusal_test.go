package tools

// Regression tests for the SELF-CHECKOUT refusal (epic 01a0c1eb, 2026-09-21 —
// "the cmd/agent suite deleted the worktree it was verifying").
//
// THE MEASURED DEFECT. `go test ./cmd/agent/` launched from a linked worktree
// removed a SIBLING linked worktree mid-run (measured 2026-09-21T14:12:35 in
// TestTaskLifecycleTick_ReclaimsDeadHolderWorktree — a name retired with its
// tick call site in 58b5618b; the acceptance runs as
// TestMergerPass_ReclaimsDeadHolderWorktree today, and the arm is unchanged; the
// same run watched
// /home/user/workspace/gopi-repro-3480894-decoy disappear 457s in). The
// mechanism has two halves and NEITHER half is a bug on its own:
//
//	CWD half  — worktreeSweepRepos() enrols the repo the process's CWD belongs
//	            to, not only the settings `repos` table. That is correct for the
//	            resident tick, which IS the repo it serves.
//	LEDGER half — the run's ledger is a per-run sandbox (GOPI_MEMORY_DB), so a
//	            registration the HOST's ledger owns is unowned to the sweep.
//
// clean + unowned + past the age floor + inside a declared root = reclaim, and
// the directory goes. The refusals that exist are all about the REGISTRATION
// (dirty? held? row-owned?); none of them is about the ACTOR — so the sweep
// deleted a checkout of the very repository it was standing in, and the loss
// stayed invisible until a later step called getcwd ("getcwd: no such file or
// directory").
//
// THE REFUSAL. A registration whose repo IS the process's own cwd repo is never
// freed by this pass (worktreeSweepHostObservations.SelfOwned →
// worktreeRuleSelfCheckout). The pair "my own unowned residue in my own repo"
// belongs to the producer pass, which proves ownership from the ledger's claim
// stamp (ReclaimOwnFinishedWorktrees / ReclaimSelfOwnRegisteredWorktrees).
//
// WHY THE POSITIVE HALF IS HERE TOO. A refusal that fires on everything is not
// a guard, it is a stopped sweep: an unrelated repo's registrations, and this
// repo's registrations when the process stands SOMEWHERE ELSE, must still be
// reclaimed exactly as before. Both directions are asserted below, because the
// narrow scope is the whole reason this rung is safe to add.
//
// WHY A CHILD PROCESS. The self-identity is a fact about the PROCESS's working
// directory, resolved once per process by worktreeSweepSelfOwnedRepo. A helper
// run in its own process (with cmd.Dir set) is the only honest way to present
// the "this process stands somewhere else" direction, and it keeps the test
// from chdir'ing the whole `go test` process — a global mutation that would
// make every sibling test's own cwd reasoning unreliable.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// selfCheckoutChildDirEnv / selfCheckoutChildRepoEnv hand the child helper
	// the two facts it must act on: where to stand, and which repo to judge.
	selfCheckoutChildDirEnv  = "GOPI_TEST_SELFCHECKOUT_DIR"
	selfCheckoutChildRepoEnv = "GOPI_TEST_SELFCHECKOUT_REPO"

	// selfCheckoutChildRootEnv hands the child the DECLARED ROOT the fixture's
	// repo lives under — the third fact of the same three, and the one the
	// fixture must state explicitly.
	//
	// WHY IT IS NOT LEFT TO THE CHILD'S OWN ENVIRONMENT (measured 2026-09-28 on
	// master's own run of this test). The child is a test PROCESS, so it runs
	// this package's TestMain, which pins TMPDIR to a NEW per-run suite root — a
	// SIBLING of the parent's, not the parent's. The fixture's repo and worktree
	// are created under the PARENT's t.TempDir(), so in the child they lay
	// outside every declared root the host resolves (the declared worktree root,
	// the scratch home, the CHILD's temp root, the repo tree) and the sweep
	// reported `declared-root-scope — not swept` for a registration the test then
	// required to have been reclaimed. All three subtests failed downstream of
	// that one root list. Naming the root here is the fixture's own declaration,
	// which is exactly what a declared root IS (Rule 1b/14): a fixture says where
	// its throwaway tree is, instead of hoping the ambient list happens to reach
	// it.
	selfCheckoutChildRootEnv = "GOPI_TEST_SELFCHECKOUT_ROOT"
)

// TestSelfCheckoutSweepHelper is not a test: it is the child-process body of
// TestWorktreeSweepRefusesToFreeTheRepoItStandsIn. It runs the REAL decision
// path for one repo — the same injected-evidence door the unit tests use, plus
// the production self-identity resolution — and prints the judged findings,
// because the parent can neither chdir the whole test binary nor read this
// process's cwd identity.
func TestSelfCheckoutSweepHelper(t *testing.T) {
	dir := os.Getenv(selfCheckoutChildDirEnv)
	if dir == "" {
		t.Skip("helper — driven by TestWorktreeSweepRefusesToFreeTheRepoItStandsIn")
	}
	repo := os.Getenv(selfCheckoutChildRepoEnv)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("helper chdir %s: %v", dir, err)
	}
	// The identity the guard will read, printed with the findings so a failure
	// says WHICH fact was wrong (the actor's repo, its common dir, guestness)
	// rather than only that a directory survived or did not.
	selfPos := worktreeSweepSelfPosition()
	t.Logf("SELF repo=%q common=%q guest=%v", selfPos.Repo, selfPos.Common, selfPos.Guest)
	t.Logf("CANDIDATE repo=%q guard=%v", repo, worktreeSweepSelfGuestCheckout(repo))
	opts := worktreeSweepOptions{
		DryRun: false,
		MinAge: time.Nanosecond, // the age floor is not what this test is about
		Max:    reclaimScanMax,
		Now:    time.Now(),
	}
	deps := worktreeSweepDeps{
		Repos: []string{repo},
		// The roots IN FORCE are the fixture's own throwaway area, declared here:
		// the child's ambient resolve cannot reach the parent's root
		// (selfCheckoutChildRootEnv's doc carries the measurement).
		Roots: []worktreeroots.Root{{
			Path:   os.Getenv(selfCheckoutChildRootEnv),
			Role:   worktreeroots.RoleFixture,
			Source: worktreeroots.SourceTempRoot,
		}},
		// The empty-but-READ ledger is the measured condition: the run's own
		// per-run sandbox knows nothing about the host's registrations, so
		// "unowned" is its honest answer.
		LedgerRead: true,
		Holders:    scratch.HolderScan{},
	}
	res := worktreeSweepRunWith(context.Background(), opts, deps, nil)
	for _, f := range res.Findings {
		// One line per registration: disposition, rule, reason, then the path
		// (the path last because it may contain spaces).
		t.Logf("FINDING %s %s %s | %s", f.Disposition, f.Rule, f.Path, f.Reason)
	}
}

// TestWorktreeSweepRefusesEvictionOfAPeerCheckout is the RED test for the
// measured defect, and it pins BOTH directions of the criterion — because a
// guest-only refusal is exactly as narrow as it must be, and a broader "same
// repo" refusal would silently strip the resident of its mandate (the very pass
// that reaps a repo's own dead registrations):
//
//	(a) GUEST in the repo — the process stands in a LINKED worktree of it, so a
//	    peer task's clean/unowned/aged registration is a peer's checkout: KEPT,
//	    and the refusal NAMES the arm (Rule 15).
//	(b) RESIDENT in the repo — the process stands in its MAIN checkout: the same
//	    registration is still RECLAIMED (the mandate the criterion must not take
//	    away from the pass that owns this repo).
//	(c) FOREIGN repo — the process stands elsewhere entirely: that repo's
//	    registrations are reclaimed exactly as before (the guard is about the
//	    ACTOR's position, not about which repo was named).
func TestWorktreeSweepRefusesEvictionOfAPeerCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	selfRoot := filepath.Join(root, "self")
	otherRoot := filepath.Join(root, "other")
	for _, d := range []string{selfRoot, otherRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	selfRepo, selfWT := worktreeRefusalFixture(t, selfRoot)
	_, otherWT := worktreeRefusalFixture(t, otherRoot)
	for _, wt := range []string{selfWT, otherWT} {
		ageRegistrationRecord(t, worktreeFullGitdir(t, wt), time.Now().Add(-2*reclaimSelfOwnMinAge))
	}
	for _, tc := range []struct {
		name     string
		stand    string
		repo     string
		wt       string
		wantGone bool
		why      string
	}{
		{
			name: "guest in the repo, peer checkout", stand: selfWT, repo: selfRepo, wt: selfWT,
			wantGone: false,
			why:      "a process standing in a linked worktree must never evict a peer checkout of the same repository",
		},
		{
			name: "resident in the repo (main checkout)", stand: selfRepo, repo: selfRepo, wt: selfWT,
			wantGone: true,
			why:      "the resident in the main checkout keeps its mandate over its own repo's unowned registrations",
		},
		{
			name: "foreign repo", stand: otherWT, repo: selfRepo, wt: selfWT,
			wantGone: true,
			why:      "standing in another repository, this pass may still reclaim an unowned registration",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Each case re-creates its registration: a case that removes one
			// must not decide the next case's fixture.
			restoreWorktreeRegistration(t, tc.repo, tc.wt)
			out := runSelfCheckoutChild(t, tc.stand, tc.repo, root)
			_, err := os.Stat(tc.wt)
			gone := os.IsNotExist(err)
			if gone != tc.wantGone {
				t.Fatalf("%s\nregistration %s: gone=%v, want gone=%v\nfindings:\n%s",
					tc.why, tc.wt, gone, tc.wantGone, out)
			}
			// Rule 15: the refusal is NAMED, not merely silent. The kept case
			// must carry the policy arm and the reason that claims the actor
			// fact — a refusal nobody can read is indistinguishable from a bug.
			if !tc.wantGone {
				if !strings.Contains(out, string(worktreeRuleSelfCheckout)) ||
					!strings.Contains(out, worktreeSweepSelfCheckoutPrefix) {
					t.Fatalf("the refusal must name the %q arm and its reason.\nfindings:\n%s",
						worktreeRuleSelfCheckout, out)
				}
			}
		})
	}
}

// TestWorktreeSweepSelfOwnedRepoResolvesTheProcessRepo pins the identity half
// in-process (no child): inside a repo the answer is that repo, and the
// comparison is canonical — a spelling difference (a symlink, or a trailing
// component) must not let a registration slip past the refusal.
// TestWorktreeSweepSelfGuestCheckoutResolvesTheProcessRepo pins the two facts
// the identity resolves to (the process's own repo, and whether it is a GUEST
// there), plus the canonical comparison — a spelling difference (a trailing
// component, a symlink) must not let a registration slip past the refusal.
func TestWorktreeSweepSelfGuestCheckoutResolvesTheProcessRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo, _ := worktreeRefusalFixture(t, t.TempDir())
	// The guard's positive half is scoped by BOTH facts, so this test pins each
	// one where it can see it.
	self := worktreeSweepSelfOwnedRepo()
	if self == "" {
		t.Fatalf("this test process runs inside a git repo; the self-identity must resolve, not stay empty")
	}
	// The module root where this test binary's cwd sits is a MAIN checkout, so
	// the process is NOT a guest there: the resident's mandate over its own
	// repo's registrations is exactly what a guest-only criterion preserves.
	// Whether THIS checkout is a main one or a linked worktree is DERIVED, not
	// assumed: gopi's own developers work in linked worktrees (the very reason
	// this guard exists), so a hardcoded expectation would be wrong on half the
	// hosts this test runs on and would be "fixed" by weakening it.
	wantGuest := selfCheckoutIsGuestCheckout(t)
	if pos := worktreeSweepSelfPosition(); pos.Guest != wantGuest {
		t.Fatalf("guest=%v for cwd %s, want %v (a linked worktree of a repo reads as a guest; its main checkout does not)",
			pos.Guest, self, wantGuest)
	}
	// The two facts are consumed together, so pin the compound predicate too.
	if guest := worktreeSweepSelfGuestCheckout(self); guest != wantGuest {
		t.Fatalf("guard for the process's own repo (%s) = %v, want %v", self, guest, wantGuest)
	}
	// A foreign repo is never the process's own, guest or not.
	if worktreeSweepSelfGuestCheckout(repo) {
		t.Fatalf("a fixture repo must not be mistaken for the process's own repo (%s)", repo)
	}
	// Canonical comparison: an unclean spelling of a foreign repo is still not
	// self, and an empty string cannot conjure selfness either.
	if worktreeSweepSelfGuestCheckout(repo + string(filepath.Separator) + ".") {
		t.Fatalf("a path that merely RESOLVES under a foreign repo must not read as self")
	}
	if worktreeSweepSelfGuestCheckout("") {
		t.Fatalf("an empty repo is not the process's own repo")
	}
	if scratch.CanonicalPath(self) != self {
		t.Fatalf("the memoized self-identity must already be canonical: %s", self)
	}
}

// selfCheckoutIsGuestCheckout derives, independently of the production helper,
// whether THIS process's cwd is a linked worktree rather than a main checkout:
// a linked worktree's `.git` is a FILE (a gitdir backlink), a main checkout's is
// a DIRECTORY. Deriving it keeps the assertion honest on both shapes of host
// (gopi's own developers work in linked worktrees).
func selfCheckoutIsGuestCheckout(t *testing.T) bool {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, rerr := repoRootOf(cwd)
	if rerr != nil {
		t.Fatalf("cwd %s is not in a repository: %v", cwd, rerr)
	}
	info, serr := os.Lstat(filepath.Join(root, ".git"))
	if serr != nil {
		t.Fatalf("stat %s/.git: %v", root, serr)
	}
	return !info.IsDir()
}

// runSelfCheckoutChild runs the helper in its own process standing in stand.
func runSelfCheckoutChild(t *testing.T, stand, repo, root string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSelfCheckoutSweepHelper$", "-test.v")
	cmd.Dir = stand
	cmd.Env = append(os.Environ(),
		selfCheckoutChildDirEnv+"="+stand,
		selfCheckoutChildRepoEnv+"="+repo,
		selfCheckoutChildRootEnv+"="+root,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper in %s: %v\n%s", stand, err, out)
	}
	return string(out)
}

// worktreeFullGitdir returns the worktree's private git dir (the directory the
// .git FILE points at), which is where the registration's creation record lives.
func worktreeFullGitdir(t *testing.T, wt string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(wt, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(string(raw), "gitdir:"))
	if gitdir == "" {
		t.Fatalf("no gitdir backlink in %s/.git (raw=%q)", wt, raw)
	}
	return gitdir
}

// restoreWorktreeRegistration re-creates the linked worktree at wt if a previous
// subtest removed it, so every subtest starts from the same world.
func restoreWorktreeRegistration(t *testing.T, repo, wt string) {
	t.Helper()
	if _, err := os.Stat(wt); err == nil {
		return
	}
	branch := "feat/x"
	cmd := exec.Command("git", "worktree", "add", wt, branch)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restore worktree %s: %v: %s", wt, err, out)
	}
	ageRegistrationRecord(t, worktreeFullGitdir(t, wt), time.Now().Add(-2*reclaimSelfOwnMinAge))
}
