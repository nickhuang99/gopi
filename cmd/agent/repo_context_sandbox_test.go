package main

// repo_context_sandbox_test.go — the hermetic REPO-CONTEXT sandbox for every
// test in this package that drives a REAL gopi process capable of running a
// lifecycle / own-worktree-release pass, plus the hazard test that proves both
// halves of the hazard.
//
// THE DEFECT THIS CLOSES (fulldry.log:522-524, task 01a0a80b, 2026-09-16):
// pinning the LEDGER (GOPI_HOME / GOPI_MEMORY_DB) is only HALF a sandbox. The
// producer pass that closes out a run frees the linked worktrees of the repos
// THAT RUN WORKED IN, and it resolves that repo set from two places —
// internal/tools.ReclaimSelfOwnRegisteredWorktrees (reclaimSelfOwnRepos):
//
//	① the ledger rows THIS process claims, and
//	② THE REPO OF THE PROCESS'S OWN CWD (`git -C <cwd> worktree list`).
//
// A test that sandboxes only ① leaves ② pointing at the LIVE repository: the
// test process's working directory IS ~/work/GoPi/cmd/agent (or a developer's
// linked worktree), so the spawned worker/merger enumerates the HOST's
// registrations, finds the ones no row of the SANDBOX ledger names, and — if
// they are clean, unheld and older than the pass's 5-minute floor — REMOVES
// them (ordinary `git worktree remove`, branch detached). Measured live, not
// inferred:
//
//	worker: own-worktree-release: own registered worktree freed: /home/nick/workspace/gopi-v1-clean
//	   (a worktree of this run's repo that no ledger row owned; branch detached untouched)
//
// and the dirty siblings were correctly `kept … left for a human`, which is why
// the loss looked random. The same pass freed the worktree of the very agent
// writing this file (2026-09-16) while a resident `gopi worker` ran on the host.
//
// WHY ALL THREE PINS AND NOT ONE: the pass's inputs are three, so the sandbox
// pins three — the settings repos table (an empty one), the LEDGER (an absent
// file = no rows this process claims) and the process's CWD (outside every git
// repository). Measured 2026-09-16: with the settings and the cwd pinned but the
// ledger left ambient, the pass resolved SIX of the host's registrations from
// the operator's own ledger — the ledger is not optional, and a fixture that
// forgets it re-opens the hazard through ①.
//
// WHY A NON-GIT DIRECTORY AND NOT A SANDBOX INSIDE THE REPO: reclaimSelfOwnRepoOf
// asks git for the cwd's repo, and a subdirectory of a repository still resolves
// to that repository — the hazard would move, not close. The pin therefore
// re-MEASURES the property it bought and fails LOUDLY when it does not hold
// (Rule 15: a hermeticity guard that silently degrades is worse than none).
//
// SCOPE — WHY THIS IS NOT IN TestMain: a suite-wide chdir would break the many
// tests in this package that legitimately read RELATIVE paths from the package
// directory (`os.ReadFile("cli_tasks.go")` source gates, `go build -o bin .`).
// The hazard is not "the suite's cwd" but "a test that HANDS the live repo to a
// spawned process", so the pin is applied exactly at those spawn sites, and
// TestSpawnersPinTheRepoContext fails the build when one of them forgets.

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
	"github.com/nick/gopi/internal/worktreeroots"

	_ "modernc.org/sqlite"
)

const (
	// repoCtxSandboxDirPrefix names the per-test sandbox directory inside the
	// suite's pinned scratch root. It is created with os.MkdirTemp (a fresh,
	// unpredictable name) because the pinned CWD is DELETED at test end and a
	// predictable writable path is a plant-a-symlink hazard; the scratch root
	// is measured OFF the `/` partition by testutil's own suite pin (Rule 22).
	repoCtxSandboxDirPrefix = "gopi-repo-ctx-"
	// repoCtxSettingsFileName is the sandbox settings file's name.
	repoCtxSettingsFileName = "settings.json"
	// repoCtxSettingsBody is the sandbox settings content: an EMPTY repos
	// table, i.e. NO repo for any resident pass to enumerate (the Rule 12
	// table is the merger's sweep list — an empty one is the documented
	// "nothing to sweep" state, not an error). The key comes from config, so
	// the fixture cannot drift from the product's spelling.
	repoCtxSettingsBody = `{"` + config.ReposSettingsKey + `":{}}`
	// repoCtxSettingsFilePerm is the sandbox settings file's mode.
	repoCtxSettingsFilePerm = 0o644
	// repoCtxSandboxDirPerm is the sandbox directory's mode.
	repoCtxSandboxDirPerm = 0o755
	// repoCtxReleaseKillSwitchValue turns the LEDGER-KEYED producer arm
	// report-only. The ownership-independent arm (the one this epic is about)
	// consults no switch by design — it only ever clears ground the run owns, so
	// the sandbox is its only control — but the guard runs the pass in-process,
	// so it also disarms the sibling arm rather than trusting the ledger pin
	// alone.
	repoCtxReleaseKillSwitchValue = "0"
	// envHomeKey is the environment variable the child reads its home from (the
	// ledger's default location and the scratch tree hang off it).
	envHomeKey = "HOME"
	// repoCtxSpawnOverrideValue is the value of the spawn guard's documented
	// one-off override.
	repoCtxSpawnOverrideValue = "1"
	// workerOnceFlag is the one-round worker flag: the shortest real entry point
	// that runs a close-out (and therefore the producer pass).
	workerOnceFlag = "--once"
	// repoCtxLedgerFileName is the sandbox ledger file handed to the pass
	// through config.MemoryDBEnv. It comes from config so a rename cannot leave
	// this fixture pointing at a ledger the product no longer reads.
	repoCtxLedgerFileName = config.MemoryDBFileName
)

// The git spellings this file needs, named once (Rule 1b: a meaning-carrying
// literal is never typed inline at the use site).
const (
	repoCtxWorktreePrefix = "worktree " // `git worktree list --porcelain`
	repoCtxGitdirPrefix   = "gitdir: "  // the .git backlink's prefix
	repoCtxDotGitName     = ".git"
	// repoCtxHeadName / repoCtxGitdirLinkName are git's two creation records
	// inside a registration's private git dir: HEAD and the `gitdir` backlink.
	// They are written once, at registration, and survive every routine verb —
	// which is why the pass measures age on the FIRST of them it finds rather
	// than on the git dir's own mtime (git bumps that on every status).
	repoCtxHeadName       = "HEAD"
	repoCtxGitdirLinkName = "gitdir"
)

// The hazard fixture's own spellings.
const (
	// repoCtxVictimAge is how far the planted victim is back-dated: past the
	// pass's floor (reclaimSelfOwnMinAge, 5 minutes) without the test sleeping
	// for it.
	repoCtxVictimAge = 10 * time.Minute
	// repoCtxFixtureFile is the one committed file every fixture repo carries,
	// so HEAD exists and a worktree can be added at it.
	repoCtxFixtureFile = "readme.txt"
	// repoCtxFixtureRepoName / repoCtxVictimRedName / repoCtxVictimGreenName
	// name the fixture tree's parts: the repository, the victim the RED half
	// offers the pass, and the one the GREEN half must keep.
	repoCtxFixtureRepoName = "repo"
	repoCtxVictimRedName   = "victim-red"
	repoCtxVictimGreenName = "victim-green"
	// The AUTHORITY fixture's own spelling: one ledger row naming the fixture
	// repository, which is what lets the cwd half enroll it (the cwd-authority
	// guard) so the feasibility half runs the pass's whole guard ladder. One
	// spelling each (Rule 1b) — the id is how a leaked row is attributed.
	repoCtxAuthorityRowID      = "t-repo-ctx-authority"
	repoCtxAuthorityRowSummary = "repo-context fixture: a row naming the fixture repo"
)

// repoContextSandboxResult is what a caller needs to keep its OWN fixtures
// working after the pin moved the process's working directory, and what the
// guard needs to judge the pass afterwards.
type repoContextSandboxResult struct {
	// Dir is the sandbox directory the process's cwd was moved to.
	Dir string
	// SettingsPath is the empty-repos settings file handed to children through
	// config.SettingsEnv.
	SettingsPath string
	// LedgerPath is the sandbox ledger (an absent file = no claim rows), handed
	// to children through config.MemoryDBEnv.
	LedgerPath string
	// StartCWD is the working directory that was current when the pin ran. A
	// caller whose fixture must launch the child from a directory it has to
	// find again (its own git repo) resolves it against this instead of against
	// the moved cwd.
	StartCWD string
	// LiveRepoToplevel is the WORKING TREE the starting cwd belonged to ("" when
	// it was in no repository). It is recorded for the guard's verdict, which
	// compares against the MAIN checkout of that working tree — the string the
	// pass itself uses as a repo.
	LiveRepoToplevel string
}

// SandboxRepoContext makes the REPO CONTEXT of a spawning test hermetic, so a
// real gopi child it starts can never enumerate (let alone free) a worktree of
// the developer's live repository.
//
// It pins all three of the pass's inputs — settings, ledger, cwd — and then
// re-MEASURES the properties it bought (Rule 15: an unverifiable sandbox is a
// FAILURE, never a skip):
//
//	① writes an EMPTY settings repos table and points config.SettingsEnv at it;
//	② points config.MemoryDBEnv at a fresh (absent) ledger file, so this process
//	   claims no rows in any real database;
//	③ moves THIS process's working directory into a fresh, NON-GIT sandbox
//	   directory, and fails if that directory turns out to be inside a repository.
//
// The child must be launched from the pinned cwd (the ordinary case: exec.Cmd
// inherits the test process's cwd) — see the per-test pins for the few that must
// launch from their own fixture repo instead.
func SandboxRepoContext(t *testing.T) repoContextSandboxResult {
	t.Helper()
	dir, err := os.MkdirTemp(testutil.SuiteTempRoot(), repoCtxSandboxDirPrefix)
	if err != nil {
		t.Fatalf("repo-context sandbox: create the sandbox directory under %s: %v", testutil.SuiteTempRoot(), err)
	}
	return repoCtxPinTo(t, dir, false)
}

// SandboxRepoContextInFixtureRepo is the pin for a spawning test whose CHILD
// genuinely needs a RESOLVABLE repository — two measured cases in this package
// (2026-09-16, this epic's re-run on master):
//
//	① `gopi task submit` REFUSES from a non-repository cwd ("task_submit: repo …
//	   is not a git work tree"), so a suite that seeds its rows through the
//	   shipped CLI fails outright under the non-git sandbox, and
//	② the `task run` wrapper resolves the task's deliverable repo from its cwd,
//	   so a child handed no repository cannot exercise the path the test is about
//	   (the assertion then passes VACUOUSLY rather than failing).
//
// It pins the same three inputs — an empty settings repos table, a fresh ledger,
// and a working directory that is not the LIVE repository — with the cwd set to a
// THROWAWAY FIXTURE repository this test owns: one commit, and (re-measured, not
// assumed) exactly ONE worktree, the main checkout. A spawned pass that resolves
// this repo therefore has nothing but the main checkout to look at, and a main
// checkout is never removable — so the hazard stays closed while the child keeps a
// real repository to resolve. The settings file and the ledger live OUTSIDE the
// fixture repo, which stays pristine.
func SandboxRepoContextInFixtureRepo(t *testing.T) repoContextSandboxResult {
	t.Helper()
	dir := RepoContextFixtureRepo(t)
	if err := os.Chmod(dir, repoCtxSandboxDirPerm); err != nil {
		t.Fatalf("repo-context sandbox: chmod %s: %v", dir, err)
	}
	return repoCtxPinTo(t, dir, true)
}

// repoCtxPinTo is the shared three-part pin, with the process's cwd moved to dir,
// re-measuring every property it bought. isFixtureRepo selects the one property
// that differs: a non-git sandbox must resolve to NO repository, while a fixture
// repo must resolve EXACTLY to itself and hold exactly one worktree (the main
// one) — a fixture that silently grew a second registration would hand the
// spawned pass an unowned, freeable worktree, i.e. the hazard itself.
func repoCtxPinTo(t *testing.T, dir string, isFixtureRepo bool) repoContextSandboxResult {
	t.Helper()

	start, err := os.Getwd()
	if err != nil {
		t.Fatalf("repo-context sandbox: read the starting cwd: %v", err)
	}
	// The settings file and the ledger are written OUTSIDE dir when dir is a
	// fixture repo: the repository under test stays clean (an untracked
	// settings.json inside it would be one more thing a cleanliness rule could
	// trip on).
	stateDir := dir
	if isFixtureRepo {
		stateDir = t.TempDir()
	}
	settingsPath := filepath.Join(stateDir, repoCtxSettingsFileName)
	if err := os.WriteFile(settingsPath, []byte(repoCtxSettingsBody), repoCtxSettingsFilePerm); err != nil {
		t.Fatalf("repo-context sandbox: write %s: %v", settingsPath, err)
	}
	if err := os.Chmod(stateDir, repoCtxSandboxDirPerm); err != nil {
		t.Fatalf("repo-context sandbox: chmod %s: %v", stateDir, err)
	}
	// The LIVE working tree is read BEFORE the chdir: it is the thing the sandbox
	// must keep out of the child's reach, and the guard asserts against it after.
	res := repoContextSandboxResult{
		Dir:              dir,
		SettingsPath:     settingsPath,
		LedgerPath:       filepath.Join(dir, repoCtxLedgerFileName),
		StartCWD:         start,
		LiveRepoToplevel: repoCtxGitTopLevel(t, start),
	}

	// t.Setenv is right here rather than a loose os.Setenv: the pin is per-TEST
	// state and both variables are restored by the framework (Rule 17).
	t.Setenv(config.SettingsEnv, settingsPath)
	t.Setenv(config.MemoryDBEnv, res.LedgerPath)
	// os.Chdir + t.Cleanup, NOT t.Chdir: t.Chdir refuses to run in a test that
	// has already started a parallel subtest, and this pin is called from
	// spawners that legitimately have (the worker-daemon cases). The undo is a
	// t.Cleanup rather than a defer, because the pin's whole point is that the
	// process STAYS in the sandbox for the rest of the test — a defer would
	// restore the live repository the moment this helper returned.
	t.Cleanup(func() {
		if err := os.Chdir(start); err != nil {
			t.Errorf("repo-context sandbox: restore cwd %s: %v", start, err)
		}
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("repo-context sandbox: chdir %s: %v", dir, err)
	}

	// ③ the property, re-measured after the fact (never assumed).
	if repos, rerr := config.ResolveMergerRepos(); rerr != nil {
		t.Fatalf("repo-context sandbox: read the settings repos table back from %s: %v", settingsPath, rerr)
	} else if len(repos) != 0 {
		t.Fatalf("repo-context sandbox: %s declares %d repo(s) (%v) — a spawned pass would enumerate them",
			settingsPath, len(repos), repos)
	}
	if gitTop := repoCtxGitTopLevel(t, dir); isFixtureRepo {
		if scratch.CanonicalPath(gitTop) != scratch.CanonicalPath(dir) {
			t.Fatalf("repo-context sandbox: the cwd %s must resolve to the fixture repository itself, got %q — "+
				"a `task submit` refuses without a repository and a `task run` case would pass vacuously", dir, gitTop)
		}
		if n := len(repoCtxWorktreeRegistrations(t, dir)); n != 1 {
			t.Fatalf("repo-context sandbox: the fixture repository %s holds %d worktree registration(s), want "+
				"exactly 1 (the main checkout) — a second, unowned registration is exactly what a spawned "+
				"release pass would free", dir, n)
		}
	} else if gitTop != "" {
		t.Fatalf("repo-context sandbox: the pinned cwd %s resolves to the git repository %s — "+
			"a spawned worker/merger would enumerate (and can free) that repo's worktrees; "+
			"the sandbox directory must live outside every repository", dir, gitTop)
	}
	return res
}

// repoCtxWorktreeRegistrations lists the worktree paths git reports for repo.
func repoCtxWorktreeRegistrations(t *testing.T, repo string) []string {
	t.Helper()
	var paths []string
	for _, line := range strings.Split(repoCtxGit(t, repo, "worktree", "list", "--porcelain"), "\n") {
		if p, ok := strings.CutPrefix(line, repoCtxWorktreePrefix); ok {
			paths = append(paths, strings.TrimSpace(p))
		}
	}
	return paths
}

// assertProducerPassResolvesNoRepo is the HAZARD GUARD: it runs the product's
// OWN producer pass (internal/tools.ReclaimSelfOwnRegisteredWorktrees) under the
// pinned context and fails when the pass still reaches the live tree.
//
// It is deliberately end-to-end rather than a re-implementation of the repo
// resolution: the pass is the thing that deleted the worktrees, so the pass is
// what must be observed to be inert. The ledger-keyed sibling arm is disarmed
// first (the ownership-independent arm has no switch by design), so a failing
// guard REPORTS the hazard instead of repeating it.
func assertProducerPassResolvesNoRepo(t *testing.T, res repoContextSandboxResult) {
	t.Helper()
	t.Setenv(tools.ReclaimSelfEnv, repoCtxReleaseKillSwitchValue)
	lines := tools.ReclaimSelfOwnRegisteredWorktrees()
	main := repoCtxMainCheckout(t, res.LiveRepoToplevel)
	for _, line := range lines {
		if main != "" && strings.Contains(scratch.CanonicalPath(line), main) {
			t.Fatalf("the producer pass still names the live checkout %s although the repo context is "+
				"sandboxed — a spawned worker/merger would free these:\n  %s", main, line)
		}
	}
	if len(lines) != 0 {
		cwd, _ := os.Getwd() //stylegate:ignore the cwd is read only to explain an already-failing verdict; an empty value cannot change it
		t.Fatalf("the producer pass acted on %d registration(s) although the repo context is sandboxed — "+
			"a spawned worker/merger would free them (cwd=%s ledger=%s settings=%s):\n  %s",
			len(lines), cwd, res.LedgerPath, res.SettingsPath, strings.Join(lines, "\n  "))
	}
}

// TestRepoContextHazard_TheReleasePassFreesAnUnownedWorktreeOfTheCwdRepo is the
// epic's regression guard, and it is TWO halves on purpose:
//
//	FEASIBILITY HALF — the plant IS the shape the pass frees. It is staged
//	  through a ledger that NAMES the fixture repository, so the pass runs its
//	  whole guard ladder and frees the plant on the AUTHORITY it was given
//	  (settings ✓, ledger ✓ naming the repo, cwd inside the repo). This half
//	  exists so the guard half below cannot pass vacuously: if the plant ever
//	  stops being freeable, THIS half fails instead of the guard half quietly
//	  holding.
//	GREEN HALF — the same call, with the process standing in the repo-context
//	  sandbox and a ledger that names nothing, leaves the plant registered and
//	  resolves no repo at all.
//
// WHAT THIS TEST USED TO ASSERT, and why it changed (2026-09-16). Its red half
// used to run the pass with an EMPTY ledger and the process standing in the
// fixture repo, asserting the plant was FREED. That assertion was the defect
// written down as a requirement: a pass that frees a repository's registrations
// while covering that repository nowhere was never merely a sandbox failure —
// it is what deleted live worktrees on this host. The cwd-authority guard
// (internal/tools/task_reclaim_self.go) now refuses exactly that state, so the
// fixture survives as the FEASIBILITY half: same repo, same plant, same pass,
// with the ledger actually naming the repo. The pre-fix behaviour is still
// proven RED — by the guard's own mutation case in
// internal/tools/task_reclaim_self_cwd_authority_test.go and by the red output
// recorded in devdoc/own-release-cwd-authority.md.
//
// Nothing here touches the developer's workspace: the fixture tree, the ledger,
// the settings file and both victims live inside this test's temporary tree.
func TestRepoContextHazard_TheReleasePassFreesAnUnownedWorktreeOfTheCwdRepo(t *testing.T) {
	// ① the fixture tree: root/repo is the repository, root/victim-* the planted
	// registrations. Root doubles as the declared worktree root, so the pass's
	// containment rung accepts the victim it is meant to offer.
	root := t.TempDir()
	repo := filepath.Join(root, repoCtxFixtureRepoName)
	if err := os.MkdirAll(repo, repoCtxSandboxDirPerm); err != nil {
		t.Fatalf("create the fixture repository dir: %v", err)
	}
	repoCtxGit(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, repoCtxFixtureFile), []byte("fixture\n"), repoCtxSettingsFilePerm); err != nil {
		t.Fatalf("write the fixture file: %v", err)
	}
	repoCtxGit(t, repo, "add", repoCtxFixtureFile)
	repoCtxGit(t, repo, "commit", "-q", "-m", "fixture")

	// ② the pin FIRST, so the sandbox's own verdict is measured against the REAL
	// live repository, and so BOTH halves run with the sandbox settings + ledger
	// already in place (the difference between them is only the cwd).
	sandbox := SandboxRepoContext(t)
	// The fixture tree is the declared worktree root for this test only: the
	// override needs no settings file, and t.Setenv keeps it inside the test.
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	// The ledger-keyed sibling arm is disarmed. The ownership-independent arm
	// this epic is about consults no switch by design (it only ever clears ground
	// this process owns, so a switch there would be the operator's brake on the
	// wrong leg) — the sandbox is what must keep it inert.
	t.Setenv(tools.ReclaimSelfEnv, repoCtxReleaseKillSwitchValue)

	// ③ FEASIBILITY HALF — settings ✓, ledger ✓ naming the repo, cwd inside the
	// repo. The plant MUST be freed here, so the guard half below can never pass
	// vacuously: if the plant stops being freeable, THIS is what fails.
	freeable := filepath.Join(root, repoCtxVictimRedName)
	plantUnownedWorktree(t, repo, freeable)
	ageWorktreeForRelease(t, freeable, repoCtxVictimAge)
	if !worktreeRegistered(t, repo, freeable) {
		t.Fatalf("the planted victim %s is not registered — this test's own fixture is broken", freeable)
	}
	feasibleLines := repoCtxRunReleasePassNamingRepo(t, repo, sandbox.LedgerPath)
	if worktreeRegistered(t, repo, freeable) {
		//stylegate:ignore the failure message is a multi-line concat carrying the pass's own lines
		t.Fatalf("the plant %s was NOT freed although the ledger names the repository %s — the fixture no "+
			"longer offers the pass a freeable registration, so the guard half below would pass vacuously.\n"+
			"pass lines:\n  %s", freeable, repo, strings.Join(feasibleLines, "\n  "))
	}
	if _, serr := os.Stat(freeable); serr == nil {
		t.Errorf("the victim's registration is gone but its directory %s survives — a different mechanism "+
			"emptied the registry; this half no longer measures the pass", freeable)
	}
	if !strings.Contains(strings.Join(feasibleLines, "\n"), freeable) {
		t.Errorf("the victim was removed but no pass line named %s — this half no longer measures the "+
			"pass's own report:\n  %s", freeable, strings.Join(feasibleLines, "\n  "))
	}

	// ④ GREEN HALF — the pinned state: settings ✓, ledger ✓, cwd outside every
	// repository ✓.
	green := filepath.Join(root, repoCtxVictimGreenName)
	plantUnownedWorktree(t, repo, green)
	ageWorktreeForRelease(t, green, repoCtxVictimAge)
	greenLines := repoCtxRunReleasePass(t, sandbox.Dir)
	if !worktreeRegistered(t, repo, green) {
		t.Fatalf("the repo-context sandbox did NOT protect the planted victim %s — the pass reached the "+
			"repository at %s although the settings table is empty, the ledger claims nothing and the cwd "+
			"is not a repository:\n  %s", green, repo, strings.Join(greenLines, "\n  "))
	}
	assertProducerPassResolvesNoRepo(t, sandbox)
}

// repoCtxRunReleasePass runs the product's own producer release with the process
// standing in dir, and returns its report lines. It is the pass a worker round's
// close-out runs (cmd/agent/cli_tasks_worker.go), driven directly so the guard
// measures the pass rather than a launcher's convenience.
func repoCtxRunReleasePass(t *testing.T, dir string) []string {
	t.Helper()
	restore := repoCtxEnter(t, dir)
	defer restore()
	return tools.ReclaimSelfOwnRegisteredWorktrees()
}

// repoCtxRunReleasePassNamingRepo is repoCtxRunReleasePass with the pass's own
// AUTHORITY evidence in place: one ledger row naming repo, so the cwd half may
// enroll it (the cwd-authority guard) and the pass runs its full guard ladder
// rather than refusing at the repo set.
//
// The row deliberately belongs to ANOTHER host's claim stamp, so the
// claim-stamp half of the repo set stays empty and the CWD half is the only
// thing that can enroll repo — the feasibility half then measures exactly the
// authority path the guard half withholds.
func repoCtxRunReleasePassNamingRepo(t *testing.T, repo, ledger string) []string {
	t.Helper()
	repoCtxSeedRepoNamingRow(t, ledger, repo)
	return repoCtxRunReleasePass(t, repo)
}

// repoCtxSeedRepoNamingRow writes one row whose deliverable carries repo into
// ledger. It opens the ledger through tools.TaskSubmit FIRST — the product's own
// open path, which creates/validates the schema (a bare sql.Open on an absent
// file has no tasks table to insert into) — and then stamps the deliverable
// through tools.LedgerDSN, never a bare DSN (the shared ledger open, Rule 14).
// The row's source is the ledger's own default ported through TaskSubmit
// (Rule 1b: no provenance literal typed here).
func repoCtxSeedRepoNamingRow(t *testing.T, ledger, repo string) {
	t.Helper()
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: repoCtxAuthorityRowSummary,
		Payload: "repo-context fixture",
		TaskID:  repoCtxAuthorityRowID,
	}); err != nil {
		t.Fatalf("repo-context fixture: seed the row %s through the product's open path: %v", repoCtxAuthorityRowID, err)
	}
	db, err := sql.Open("sqlite", tools.LedgerDSN(ledger))
	if err != nil {
		t.Fatalf("repo-context fixture: open the sandbox ledger %s: %v", ledger, err)
	}
	defer db.Close() //stylegate:ignore test-fixture handle closed at test end; the OS reclaims the descriptor
	blob := `{"repo":"` + repo + `"}`
	if _, err := db.ExecContext(context.Background(),
		`UPDATE tasks SET deliverable = ?, status = ? WHERE task_id = ?`,
		blob, tools.TaskStatusCompleted, repoCtxAuthorityRowID); err != nil {
		t.Fatalf("repo-context fixture: stamp the repo onto row %s in %s: %v", repoCtxAuthorityRowID, ledger, err)
	}
}

// RepoContextFixtureRepo creates a throwaway git repository (one commit) and
// returns its path — for a test whose CHILD genuinely needs a resolvable repo
// (the `task run` wrapper resolves the task's deliverable repo from its cwd).
//
// Giving that child the FIXTURE repo is both functional and safe: the child
// resolves a repository this test owns, whose only worktree is the main one, so
// the release pass has nothing of the developer's to find. The alternative —
// letting it resolve the package's own directory — is the hazard itself.
func RepoContextFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), repoCtxFixtureRepoName)
	if err := os.MkdirAll(repo, repoCtxSandboxDirPerm); err != nil {
		t.Fatalf("repo-context fixture: mkdir %s: %v", repo, err)
	}
	repoCtxGit(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, repoCtxFixtureFile), []byte("fixture\n"), repoCtxSettingsFilePerm); err != nil {
		t.Fatalf("repo-context fixture: write the commit file: %v", err)
	}
	repoCtxGit(t, repo, "add", repoCtxFixtureFile)
	repoCtxGit(t, repo, "commit", "-q", "-m", "fixture")
	return repo
}

// plantUnownedWorktree plants a CLEAN, DETACHED worktree registration at path —
// exactly the shape the producer pass frees when nothing owns it: no branch, no
// uncommitted work, no live process inside, and (after ageWorktreeForRelease)
// old enough to clear the pass's age floor.
//
// It is the VICTIM a hazard test offers a spawned gopi: if the child was handed
// repo as its context the registration is freed; if the child was sandboxed it is
// still registered afterwards.
func plantUnownedWorktree(t *testing.T, repo, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), repoCtxSandboxDirPerm); err != nil {
		t.Fatalf("create the victim's parent dir: %v", err)
	}
	repoCtxGit(t, repo, "worktree", "add", "--detach", path, repoCtxHeadName)
}

// ageWorktreeForRelease back-dates a planted worktree's CREATION record past the
// pass's age floor, so a hazard test does not have to sleep for the real
// (5-minute) floor. It writes the records the pass actually reads — the
// registration's OWN git dir, never a shared parent (the repository-age trap:
// the main repo's commondir carries the REPOSITORY's age, so ageing it would not
// make a fresh registration old).
func ageWorktreeForRelease(t *testing.T, worktree string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	backLink := filepath.Join(worktree, repoCtxDotGitName)
	raw, err := os.ReadFile(backLink)
	if err != nil {
		t.Fatalf("age %s: read %s: %v", worktree, backLink, err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), repoCtxGitdirPrefix))
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(worktree, gitdir)
	}
	// The pass reads these in a FIXED order and takes the FIRST hit (`gitdir`,
	// then `HEAD`, then the git dir itself), so every present record is aged —
	// and never the git dir alone, whose mtime `git status` bumps on every look.
	for _, name := range []string{repoCtxGitdirLinkName, repoCtxHeadName, ""} {
		target := gitdir
		if name != "" {
			target = filepath.Join(gitdir, name)
		}
		if _, serr := os.Stat(target); serr != nil {
			continue // the pass falls through to the next creation record
		}
		if err := os.Chtimes(target, when, when); err != nil {
			t.Fatalf("age %s: chtimes %s: %v", worktree, target, err)
		}
	}
}

// worktreeRegistered reports whether git still holds a registration for path
// (the question a hazard test asks after the child exited). A registration whose
// directory was REMOVED no longer appears, so this is the same reading
// `git worktree list` gives — and the same one the merger's safe-merge gate [c]
// makes.
func worktreeRegistered(t *testing.T, repo, path string) bool {
	t.Helper()
	out := repoCtxGit(t, repo, "worktree", "list", "--porcelain")
	want := scratch.CanonicalPath(path)
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), repoCtxWorktreePrefix) {
			continue
		}
		got := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), repoCtxWorktreePrefix))
		if scratch.CanonicalPath(got) == want {
			return true
		}
	}
	return false
}

// repoCtxEnter moves the process into dir and returns the restore func. It is
// the deliberate inverse of SandboxRepoContext: a test that must stand IN a
// repository to reproduce the hazard states that (Rule 15: the red half is
// demonstrated, not assumed).
func repoCtxEnter(t *testing.T, dir string) func() {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("read cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	return func() {
		if err := os.Chdir(prev); err != nil {
			t.Errorf("restore cwd %s: %v", prev, err)
		}
	}
}

// repoCtxEnterInto runs fn with the process standing in dir and restores the
// previous working directory afterwards. The measurement must happen WHILE the
// cwd is pinned: the producer pass resolves its repo from the cwd at the moment
// it runs.
func repoCtxEnterInto(t *testing.T, dir string, fn func()) {
	t.Helper()
	restore := repoCtxEnter(t, dir)
	defer restore()
	fn()
}

// repoCtxMainCheckout answers the MAIN checkout of a working tree: the string the
// producer pass itself uses as a repo (`git worktree list` returns the main
// checkout as its first porcelain record, never the linked worktree the process
// happens to stand in). "" when the input names no working tree — the guard then
// has nothing to compare against.
func repoCtxMainCheckout(t *testing.T, worktree string) string {
	t.Helper()
	if strings.TrimSpace(worktree) == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", worktree, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	common := scratch.CanonicalPath(strings.TrimSpace(string(out)))
	return strings.TrimSuffix(common, repoCtxDotGitName)
}

// repoCtxGitTopLevel returns the git repository a directory belongs to, or ""
// when it is in no repository — the question (and the binary) the producer pass
// itself uses, with the noise of a non-repo directory suppressed.
func repoCtxGitTopLevel(t *testing.T, dir string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("repo-context sandbox: git is required to verify the pinned cwd is outside a repository: %v", err)
	}
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "" // not a repository — the property this pin buys
	}
	return strings.TrimSpace(string(out))
}

// repoCtxGit runs one git command in dir with a hermetic identity (the host's
// global config must not leak into the fixture).
func repoCtxGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}
