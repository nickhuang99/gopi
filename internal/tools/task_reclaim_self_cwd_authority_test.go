// task_reclaim_self_cwd_authority_test.go — the CWD-AUTHORITY guard on the
// ownership-independent producer pass (ReclaimSelfOwnRegisteredWorktrees).
//
// THE DEFECT (measured 2026-09-16, and re-measured on master's own tree while
// this file was written): the pass resolves its repos as
// `reclaimSelfOwnRepos()` = the repos of the rows THIS process claims ∪ the repo
// of the process CWD. When the LEDGER is not the authority for the repository
// the process stands in — the classic `gopi worker <task-id>` launched from
// ~/work/GoPi with a sandboxed home/ledger — the cwd half alone enrolled the
// LIVE repository, enumerated its registered worktrees and REMOVED every one
// that was clean, held by nobody and older than the 5-minute floor: other
// tasks' live, unmerged work (the directory goes, the branch survives, which is
// why the loss looked random).
//
// THE GUARD: a repository is enrolled by the cwd ONLY when the ledger this
// process reads demonstrably covers it — it names at least one row carrying
// that repo (`reclaimSelfOwnRepoNamed`). An empty/absent ledger names no repo,
// so a round that cannot prove ownership does not sweep.
//
// WHAT THESE TESTS PIN, and why each is a SEPARATE case:
//
//	① the PREDICATE (reclaimSelfOwnRepoNamed) — exact canonical match, and a
//	   ledger naming a DIFFERENT repo (or nothing) never authorises;
//	② the REPO SET (reclaimSelfOwnRepos) — a foreign ledger + a cwd inside a
//	   repository yields NO repo, and the refusal is NAMED (Rule 15);
//	③ THE REGRESSION (ReclaimSelfOwnRegisteredWorktrees) — a DECOY: a clean,
//	   detached, aged registration of another task's repo survives a full pass
//	   run with a sandbox ledger and the process standing in that repo. This is
//	   the epic's acceptance shape;
//	④ NO REGRESSION OF PURPOSE — the same fixture, plus a row THIS process
//	   claims that carries the repo, still enrols and frees the decoy (the
//	   pass's legitimate job, untouched).
//
// THE RED PROOF is a MUTATION, not a second code path (deliberately: the
// shipped code carries no test-only knob — a shell pass-through or a
// built-in-reverse env var would be a second behaviour to reason about, and
// this codebase already refuses "test hook" switches in release paths,
// ReclaimSelfEnv excepted because it is an OPERATOR brake).
//
// The mutation that IS the pre-fix code replaces the refusal in
// reclaimSelfOwnRepos — the `if !reclaimSelfOwnRepoNamed(named, repo) { … }`
// guard clause — with the unconditional `add(repo)` the defect had. ② and ③
// then turn RED (③ with the pass's own line "… own registered worktree freed:
// …/wt", i.e. the decoy really is deleted), and both return GREEN when the
// guard is restored. Both directions are transcribed in
// devdoc/own-release-cwd-authority.md.
//
// It must be THAT mutation and not a looser one inside the predicate: a
// predicate-only mutation (`return len(named) > 0`) leaves ③ GREEN, because
// the ledger fixture there names no repo at all — so it reddens ① only, and ①
// alone cannot show that the DEFECT frees a worktree. The mutation is
// therefore named as the guard clause above, and ① is the durable pin that a
// future editor cannot satisfy the guard with an approximate reading.
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// cwdAuthorityDecoyReason is the refusal phrase the pass must print when it
// withholds a repository it cannot prove it owns (Rule 15: the withheld sweep
// is NAMED, never silent). It is asserted as a fragment so the message may
// carry its evidence without breaking this pin.
const cwdAuthorityDecoyReason = "cwd repo"

// cwdAuthorityFixture builds the epic's fixture: a repository with ONE clean
// linked worktree registration that no ledger row owns — the decoy — declared
// root included, and aged past the pass's floor, so every rung EXCEPT the
// authority guard would free it.
//
// Returned repo is the MAIN checkout (the string the pass itself uses as a
// repo, and the string a row's deliverable.repo carries: measured on 603 host
// rows, 599 record the main checkout, never a linked worktree path).
func cwdAuthorityFixture(t *testing.T) (repo, decoy string) {
	t.Helper()
	root := t.TempDir()
	repo, decoy = worktreeRefusalFixture(t, root)
	// The fixture tree doubling as the declared worktree root is what puts the
	// decoy INSIDE the pass's containment rung — otherwise the decoy would be
	// kept for the wrong reason and the mutation proof would be vacuous.
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	cwdAuthorityAgeRegistration(t, decoy)
	return repo, decoy
}

// cwdAuthorityAgeRegistration back-dates the decoy's OWN creation record past
// the pass's age floor, so nothing in these tests waits 5 real minutes. It
// reuses ageRegistrationRecord (worktree_refusal_arms_test.go) — ONE aging
// implementation — and reads the gitdir backlink git wrote at registration.
func cwdAuthorityAgeRegistration(t *testing.T, worktree string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(worktree, ".git"))
	if err != nil {
		t.Fatalf("age %s: read its gitdir backlink: %v", worktree, err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), "gitdir:"))
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(worktree, gitdir)
	}
	ageRegistrationRecord(t, filepath.Clean(gitdir), time.Now().Add(-2*reclaimSelfOwnMinAge))
}

// cwdAuthorityForeignLedger points the process at a FRESH (absent) ledger — the
// sandbox state whose rows name nothing, hence the state the defect needed.
func cwdAuthorityForeignLedger(t *testing.T) {
	t.Helper()
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), memoryDefaultDBFile))
}

// cwdAuthorityNamesRepo reports whether repos names repo, compared the way the
// production guard compares (canonical paths), so this helper cannot pass on a
// spelling the product would refuse.
func cwdAuthorityNamesRepo(t *testing.T, repos []string, repo string) bool {
	t.Helper()
	want := scratch.CanonicalPath(repo)
	for _, got := range repos {
		if scratch.CanonicalPath(got) == want {
			return true
		}
	}
	return false
}

// TestReclaimSelfOwnRepoNamedRequiresAnExactCanonicalMatch is ①: the guard's
// predicate. A ledger that names nothing never authorises a sweep, and a ledger
// that names a DIFFERENT repository never authorises THIS one — the two ways an
// approximate reading (a prefix, a length check, a non-empty check) would
// re-open the hazard.
func TestReclaimSelfOwnRepoNamedRequiresAnExactCanonicalMatch(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	other := filepath.Join(root, "other")
	if !reclaimSelfOwnRepoNamed([]string{other, repo}, repo) {
		t.Fatalf("a ledger naming the repo must authorise it")
	}
	if reclaimSelfOwnRepoNamed(nil, repo) {
		t.Fatalf("an EMPTY ledger must never authorise a repo (%s) — this is the sandbox case", repo)
	}
	if reclaimSelfOwnRepoNamed([]string{other}, repo) {
		t.Fatalf("a ledger naming only %s must not authorise %s", other, repo)
	}
	if reclaimSelfOwnRepoNamed([]string{root}, repo) {
		t.Fatalf("a ledger naming a PARENT directory must not authorise %s (no prefix reading)", repo)
	}
	if reclaimSelfOwnRepoNamed([]string{""}, repo) {
		t.Fatalf("a blank named repo must not authorise %s", repo)
	}
}

// TestReclaimSelfOwnReposRefusesTheCwdRepoOfAForeignLedger is ②: the repo set.
// The process stands in a repository whose worktrees it cannot vet (the ledger
// names nothing), so the cwd half enrols NOTHING — and says so, with the
// evidence, instead of silently sweeping.
func TestReclaimSelfOwnReposRefusesTheCwdRepoOfAForeignLedger(t *testing.T) {
	repo, _ := cwdAuthorityFixture(t)
	cwdAuthorityForeignLedger(t)
	t.Chdir(repo)

	var got []string
	stderr := captureStderrDuring(t, func() { got = reclaimSelfOwnRepos() })
	if cwdAuthorityNamesRepo(t, got, repo) {
		t.Fatalf("the cwd repo %s was enrolled by a ledger that names it nowhere (%v) — "+
			"a spawned round would free every clean worktree of the live repository", repo, got)
	}
	if len(got) != 0 {
		t.Fatalf("a foreign ledger with a cwd inside a repository must yield NO repo, got %v", got)
	}
	if !strings.Contains(stderr, cwdAuthorityDecoyReason) || !strings.Contains(stderr, repo) {
		t.Fatalf("the refusal must NAME the withheld repo and why (Rule 15), stderr=%q", stderr)
	}
}

// TestReclaimSelfOwnRegisteredWorktreesKeepsTheDecoyOfAForeignLedger is ③ — the
// epic's regression test: a decoy clean, detached, aged registration belonging
// to work this ledger never recorded must SURVIVE a full pass run whose process
// stands in that repository.
func TestReclaimSelfOwnRegisteredWorktreesKeepsTheDecoyOfAForeignLedger(t *testing.T) {
	repo, decoy := cwdAuthorityFixture(t)
	cwdAuthorityForeignLedger(t)
	t.Chdir(repo)

	// Precondition — the decoy is EXACTLY the shape the pass frees: registered,
	// clean and (for the register of trust) not named by any row of this ledger.
	if !worktreeListed(t, repo, decoy) {
		t.Fatalf("fixture broken: the decoy %s is not registered before the pass", decoy)
	}
	if entries := strings.TrimSpace(gitOut(t, decoy, "status", "--porcelain")); entries != "" {
		t.Fatalf("fixture broken: the decoy is dirty (%q) — a dirty registration would be kept for the wrong reason", entries)
	}

	var lines []string
	stderr := captureStderrDuring(t, func() { lines = ReclaimSelfOwnRegisteredWorktrees() })
	joined := strings.Join(lines, "\n")

	if !worktreeListed(t, repo, decoy) {
		t.Fatalf("the DECOY %s was freed by a pass that cannot prove it owns %s — this is the defect "+
			"(the pre-fix code does exactly this; pass lines: %v)", decoy, repo, lines)
	}
	if _, err := os.Stat(decoy); err != nil {
		t.Fatalf("the decoy's directory %s is gone (%v) although the pass reported %v", decoy, err, lines)
	}
	if strings.Contains(joined, decoy) && strings.Contains(joined, reclaimSelfOwnFreedMarker) {
		t.Fatalf("the pass FOUND the decoy without authority — pass lines: %v", lines)
	}
	if !strings.Contains(stderr, cwdAuthorityDecoyReason) {
		t.Fatalf("the withheld repository must be NAMED on the operator sink (Rule 15), stderr=%q lines=%v", stderr, lines)
	}
}

// TestReclaimSelfOwnRegisteredWorktreesFreesTheDecoyWhenTheLedgerNamesTheRepo is
// ④: the guard must not cost the pass its legitimate purpose. The same fixture
// and the same decoy, with ONE difference — a row THIS process claims that
// carries the repo — and the decoy is enrolled and freed exactly as before.
//
// The row deliberately names a SIBLING worktree path, not the decoy: naming the
// decoy would make it OWNED, and rung ④'s live-owner veto would keep it for a
// reason that has nothing to do with the authority guard under test (the
// assertion would then pass vacuously in both directions).
func TestReclaimSelfOwnRegisteredWorktreesFreesTheDecoyWhenTheLedgerNamesTheRepo(t *testing.T) {
	repo, decoy := cwdAuthorityFixture(t)
	db := filepath.Join(t.TempDir(), memoryDefaultDBFile)
	t.Setenv(memoryDBEnv, db)
	t.Chdir(repo)

	// The ledger evidence: a row NAMING the repo that this process does NOT
	// claim — the claim stamp belongs to another host, so the claim-stamp half of
	// the repo set contributes nothing and the CWD half is the only thing that
	// can enrol the repo. That is what makes this case measure the AUTHORITY path
	// itself rather than the ledger half it runs alongside.
	const taskID = "t-cwd-authority-owned"
	seedLiveStatusRow(t, taskID, repo, "feat/own", filepath.Join(filepath.Dir(repo), "run-own-wt"), "otherhost:1")

	if repos := reclaimSelfOwnRepos(); !cwdAuthorityNamesRepo(t, repos, repo) {
		t.Fatalf("a ledger naming %s must still enrol it (repos=%v) — the guard must not break the pass's own purpose", repo, repos)
	}
	lines := ReclaimSelfOwnRegisteredWorktrees()
	if worktreeListed(t, repo, decoy) {
		t.Fatalf("the pass must free the unowned clean decoy %s once its own ledger names %s; lines=%v", decoy, repo, lines)
	}
	if _, err := os.Stat(decoy); !os.IsNotExist(err) {
		t.Fatalf("the decoy's directory %s must be gone after the release (stat err=%v)", decoy, err)
	}
}
