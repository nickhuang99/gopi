package main

// Worker-path delivery recycle E2E tests (epic t-1788389175 defect fix,
// 2026-09-04): a task delivered by a WORKER on a server/single-node must
// recycle its worktree at close-out — the missing server leg of the
// finish-cleanup chain that left HOMM3's four feasibility worktrees behind
// on zpeer (PA removed them by hand so the maps merger could see the
// branches). This file drives the REAL binary through the FULL worker chain:
//
//	submit → gopi worker (claims, runs a real child via mock LLM) →
//	terminal writeback → delivery-time recycle → branch left unprotected →
//	merger pass (config-sweep) merges it into master
//
// ZERO manual intervention between worker delivery and merger merge — the
// exact scenario the HOMM3 incident exposed (4 manual worktree removals).
// Zero real API calls (scripted mock LLM); sandboxed GOPI_HOME /
// GOPI_MEMORY_DB / GOPI_SETTINGS; the git world is a throwaway temp repo +
// linked worktree.

import (
	"context"
	"github.com/nick/gopi/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// workerRecycleEnv builds the sandboxed gopi env for a worker-path E2E:
// mock LLM, throwaway GOPI_HOME/MEMORY_DB, settings.json with the role and
// the repos table the merger sweep needs, and the single-node switches that
// make a real run hermetic (no claim broadcast, auto task registration).
// The settings.json carries role=server + repos:{repo: {}} so BOTH the
// finish-cleanup mode split (IsServerNode → server arm) and the merger's
// config-driven sweep (ResolveMergerRepos → this repo) resolve against the
// SAME declared topology the incident ran in.
func workerRecycleEnv(t *testing.T, repo string, role string) []string {
	t.Helper()
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "do the deliverable work — done"})
	t.Cleanup(mock.Close)
	settingsDir := t.TempDir()
	settingsPath := filepath.Join(settingsDir, "settings.json")
	// role may be "" (the zpeer live shape: NO "role" field, no sync_peers —
	// the finish-cleanup server arm fires on the empty peer target via
	// finishCleanupServerTarget()==""), or explicit "server". Either way the
	// repos table carries the sweep repo for the merger pass.
	roleJSON := ""
	if role != "" {
		roleJSON = `"role":"` + role + `",`
	}
	body := `{` + roleJSON + `"sync_peers":[],"repos":{"` + repo + `":{}}}`
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		memoryDBEnv+"="+dbPath,
		"GOPI_SETTINGS="+settingsPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_AUTO_TASK_REGISTER=1",
	)
	return env
}

// chainGit runs one git command, failing the test on error.
func chainGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// chainWorktreeHoldsBranch reports whether repo has a linked worktree
// currently checked out on branch.
func chainWorktreeHoldsBranch(t *testing.T, repo, branch string) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git worktree list: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(strings.TrimPrefix(line, "branch ")) == "refs/heads/"+branch {
			return true
		}
	}
	return false
}

// chainCommitAncestorOfMaster reports whether commit's tip is an ancestor
// of master (i.e. already merged). Tested by HASH, not branch name: the
// adoption/merge chains delete the branch ref after a clean merge
// (branch -d), so a post-merge ancestry check keyed on the branch name
// fails not because the merge missed but because the ref is gone.
func chainCommitAncestorOfMaster(t *testing.T, repo, commit string) bool {
	t.Helper()
	err := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", commit, "master").Run()
	return err == nil
}

// chainBranchTip returns the commit hash a branch currently points at.
func chainBranchTip(t *testing.T, repo, branch string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", branch).Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", branch, err)
	}
	return strings.TrimSpace(string(out))
}

// workerRecycleStampGround records worktree+branch as the row's GROUND through
// the PRODUCTION backfill seam — tools.TaskDeliverableWorktreeBashForRow, the
// very function the executor's bash round calls when a `git worktree add`
// round creates a directory (internal/tools/task_deliverable_worktree.go). The
// fixture reaches the row's field the same way the worker does; it writes no
// blob of its own, so the test cannot drift from the real writer's schema.
//
// The row id is passed EXPLICITLY (the ForRow entry point) rather than through
// GOPI_TASK_WRAPPER_ID: this process is not the row's executor child, so it
// holds no wrapper env, and a global env flip to fake one would be state the
// fixture does not need to own.
func workerRecycleStampGround(t *testing.T, env []string, taskID, wt, branch string) {
	t.Helper()
	// The seam writes through the process's ledger, so the sandbox the run
	// uses must be THIS process's ledger too (t.Setenv also keeps a caller
	// that forgot the env honest — the same discipline the stale-sibling
	// seeder below follows).
	dbPath := envValue(env, memoryDBEnv)
	if dbPath == "" {
		t.Fatalf("test env carries no %s — the fixture must sandbox the ledger", memoryDBEnv)
	}
	t.Setenv(memoryDBEnv, dbPath)
	// The production detection seam: its command argument is what tells it this
	// round CREATED the directory, and its workdir argument is the round's own
	// cwd — the two facts a real bash round carries.
	tools.TaskDeliverableWorktreeBashForRow(taskID, wt, "git worktree add "+wt+" -b "+branch+" master")
	if gotWT, gotBranch := tools.TaskDeliverableWorktreeFor(taskID); gotWT != wt || gotBranch != branch {
		t.Fatalf("the production backfill seam must stamp the row's ground: worktree=%q branch=%q (want %q/%q)", gotWT, gotBranch, wt, branch)
	}
}

// workerRecycleRecordedGround reads the ground a row RECORDS back out of the
// rendered row — the same text an operator reads with `gopi task show`, so the
// assertion is about the row's deliverable, never about a path the fixture
// remembers inventing.
func workerRecycleRecordedGround(t *testing.T, env []string, taskID string) string {
	t.Helper()
	show, _, _ := runGopiMapped(t, []string{"task", "show", taskID}, "", env, "")
	for _, line := range strings.Split(show, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), tools.TaskShowDeliverableWorktreeLabel); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// mergeRunLockPath names the single-instance lock file the merge run takes
// (the same serve.lock every merge face takes) inside THIS test's sandboxed
// GOPI_HOME — the fixture's profile, resolved through the env the child is
// actually handed, never through this process's ambient profile. The path is
// returned rather than created: the run creates it, and the file's only
// content is the flock (gone with the run's process), so the test removes the
// zero-byte artifact instead of leaving it in the sandbox it reports on.
func mergeRunLockPath(t *testing.T, env []string) string {
	t.Helper()
	home := envValue(env, config.GopiDirEnv)
	if home == "" {
		t.Fatalf("test env carries no %s — the fixture must sandbox the profile", config.GopiDirEnv)
	}
	return filepath.Join(home, config.ProfileDirName, serveLockFilename)
}

// TestWorkerServerDeliveryRecyclesThenMergerMerges is the epic's full-chain
// acceptance (QC #1 + #5): a task delivered by a WORKER on a server node
// (worktree mode) → completes → ITS WORKTREE IS AUTOMATICALLY REMOVED →
// the branch is unprotected → the NEXT merger pass merges it into master.
// Zero manual intervention — the exact HOMM3 scenario (4 worktrees needed
// manual cleanup on zpeer).
func TestWorkerServerDeliveryRecyclesThenMergerMerges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	feat := "feat/worker-chain-e2e"
	chainGit(t, root, "init", repo)
	chainGit(t, repo, "config", "user.email", "t@t")
	chainGit(t, repo, "config", "user.name", "t")
	chainGit(t, repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	chainGit(t, repo, "add", "a.txt")
	chainGit(t, repo, "commit", "-m", "init")
	// The HOMM3 feasibility shape: the dispatcher pre-created the branch +
	// linked worktree with the deliverable work committed (PROJECT.md Rule 6a).
	chainGit(t, repo, "checkout", "-b", feat)
	if err := os.WriteFile(filepath.Join(repo, "b.txt"), []byte("deliverable work"), 0o644); err != nil {
		t.Fatal(err)
	}
	chainGit(t, repo, "add", "b.txt")
	chainGit(t, repo, "commit", "-m", "feature work")
	chainGit(t, repo, "checkout", "master")
	chainGit(t, repo, "worktree", "add", wt, feat)
	if !chainWorktreeHoldsBranch(t, repo, feat) {
		t.Fatal("fixture: feat must be checked out in the linked worktree")
	}

	env := workerRecycleEnv(t, repo, "server")

	// Submit the pending row (the worker will claim it).
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"worker chain e2e", "--payload", "do the deliverable work", "--task-id", "89e80315-0535-59d4-9f79-ffa8658bda24",
		"--repo", repo},
		"", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	// THE ROW'S OWN GROUND (ground epic a3e2d9f0): stamp the fixture's linked
	// worktree onto the row through the production backfill seam, so the row
	// RECORDS this directory as its deliverable.worktree.
	//
	// WHY THIS STEP IS THE FIX AND NOT A CONVENIENCE. Before the ground epic
	// the executor's bash round was the seam that paired the row with the
	// directory it worked in, and this fixture supplied that pairing by simply
	// running the worker with its cwd inside `wt`. The epic moved the pairing
	// to the PRODUCER: the worker now resolves-or-CREATES the row's ground
	// before the spawn (internal/tools/task_run_ground.go) and the row ends up
	// owning THAT directory — a sibling of `wt`, named gopi-<stem>. A `wt` that
	// no row records is nobody's deliverable, so the release arms keep it on
	// purpose (ownerless; younger than the 5m0s floor). Stamping it makes the
	// directory under assertion the ROW'S OWN GROUND: the same relationship the
	// pre-epic fixture had by accident, now stated explicitly and through the
	// same writer production uses.
	workerRecycleStampGround(t, env, "89e80315-0535-59d4-9f79-ffa8658bda24", wt, feat)

	// The WORKER runs RESIDENT IN THE MAIN CHECKOUT — the documented server
	// shape, and the shape the release arms are built for: the actor that owns
	// the repository is the actor allowed to free a row's ground (a process
	// standing inside a linked worktree of it is a GUEST and is refused by
	// design — guestRowOwnRefusal). Explicit role=server here; the dirty test
	// below exercises the REAL zpeer shape (no role field, no peers → empty
	// peer target fires the same server arm).
	stdout, stderr, code := runGopiMapped(t, []string{"worker", "--once"},
		"", env, repo)
	if code != 0 {
		t.Fatalf("worker --once must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// Acceptance ①: the worker's terminal delivery AUTO-REMOVED the ground the
	// row RECORDS as its own (the missing server leg — zero manual
	// intervention). The assertion reads the row's recorded ground back first,
	// so a fixture whose stamping seam silently stopped working fails HERE with
	// that fact named, instead of asserting the survival of a directory no row
	// ever owned.
	if ground := workerRecycleRecordedGround(t, env, "89e80315-0535-59d4-9f79-ffa8658bda24"); ground != wt {
		t.Fatalf("fixture: the row must record its ground as %s, but deliverable.worktree reads %q", wt, ground)
	}
	if chainWorktreeHoldsBranch(t, repo, feat) {
		t.Errorf("worker delivery left its worktree behind: %s still holds %s", wt, feat)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree dir %s still exists after worker delivery", wt)
	}
	// Acceptance ② (QC #3): the branch SURVIVES the recycle — it is the
	// merge run's input, never deleted server-side.
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", feat).Run(); err != nil {
		t.Errorf("branch %s must survive server-side recycle (merge run input)", feat)
	}
	// The wrapper row is terminal completed. (The branch backfill into the
	// deliverable is proven indirectly by the merge outcome below: the
	// fixture's `feat` is NOT an orphan commit — it is master plus the
	// worker's own commit, authored in this repo's own worktree — so an
	// ancestry check on its tip is the end-to-end statement that the very
	// work the worker delivered reached master.)
	show, _, _ := runGopiMapped(t, []string{"task", "show", "89e80315-0535-59d4-9f79-ffa8658bda24"}, "", env, "")
	if !strings.Contains(show, "completed") {
		t.Errorf("wrapper row must end completed:\n%s", show)
	}

	// Acceptance ③ (QC #5 — merger starvation): with the recycle in place,
	// the NEXT MERGE RUN merges the now-unprotected branch — no manual
	// worktree removal in between.
	//
	// THE FACE (2026-09-21, test-side migration). The resident merger daemon
	// was retired by entry-point closure (commit f9ff1a89): both live entry
	// points of `gopi merger` and its `gopi task merger` alias now exit 1 with
	// a retirement sentence naming `gopi merge`. The PRODUCT retirement is
	// deliberate, so the STALE side is this test: driving `--once` here would
	// assert a face that is gone.
	//
	// The shipped replacement is EXACTLY the one that sentence points at,
	// `gopi merge` (cmd/agent/merge_run.go) — ONE foreground run of the same
	// pass, over the same config-driven repo set (settings.json "repos" →
	// config.ResolveMergerRepos, so this fixture's single-repo table is the
	// whole sweep), deciding every branch it can with git ALONE and handing
	// the rest to a merge gopi. What the assertion needs from it is the half
	// that needs NO model: a fast-forwardable branch is LANDED
	// (merge --ff-only) and its ref pruned. This fixture's branch IS
	// fast-forwardable — `feat` is master plus one commit, and the worker
	// chain above never moved master — so the run lands it with no hand-over
	// at all, and the run's exit 0 is the strongest form of its contract:
	// "every branch reached a terminal decision". A run that did NOT land the
	// branch would either hand it over (running a real agent — the expensive
	// path this fixture must never take) or report a refusal, and the
	// assertion below fails on the landing either way.
	//
	// The old face's vocabulary is gone with it: `adopted=1`/`merged=1` were
	// counters of the resident sweep's take-over, printed by a dispatcher
	// that no longer exists. The equivalent observable outcome on the new
	// face is the report's `landed` action for THIS branch. The assertion
	// below is therefore unchanged in MEANING (the recycled branch reaches
	// master with zero manual intervention) and re-keyed on the one spelling
	// the shipped command prints (Rule 1b: the test reads the constant, never
	// a re-typed literal).
	//
	// Capture the branch tip BEFORE the merge run — the landing is followed
	// by `branch -d`, so the post-run ancestry check must be by hash (a
	// branch-name check would fail because the ref is gone, not because the
	// merge missed).
	tip := chainBranchTip(t, repo, feat)
	merr := mergeRunLockPath(t, env)
	// THE VERB WORD, NOT THE CONSTANT: `CliVerbMerge` was deleted with the
	// dispatch (epic merge-removal, item 2). This test drives the merge ENGINE
	// through the argv the engine's own tests have always used, which is why the
	// word is spelled here as a local constant rather than reached through a
	// verb constant that no longer exists — the point of the assertion is the
	// landing, and the engine is unchanged.
	// THE BRANCH IS NAMED, and naming it is the other half of the argv change:
	// `gopi merge` with no argument swept every repo in the settings table, while
	// the atom's no-argument form sweeps the SAME table — neither decides a branch
	// this fixture owns, and the fixture's repo is not in that table (measured:
	// a bare `task merge` here reports "merged=0" and leaves the branch alone).
	// The atom's `<branch>...` form is the one that decides ONLY the named
	// branches, which is the operation this test has always performed.
	mout, mstderr, mcode := runGopiMapped(t, []string{CliVerbTask, mergeEngineVerbWord, feat},
		"", env, repo)
	if mcode != 0 {
		t.Fatalf("merge must succeed: code=%d (a merge run whose lock is contested prints %q and holds no lock to clean up)\nstdout:\n%s\nstderr:\n%s",
			mcode, serveLockFilename, mout, mstderr)
	}
	// The equivalent of the retired sweep's "it landed work" line: the
	// command's own report names THIS branch as landed. The containment
	// check alone would be satisfied by the fast-forward half, so both are
	// read — the report (what the command says it did) and git (what the repo
	// actually holds).
	// THE SECOND HALF OF THIS ASSERTION USED TO READ `mergeActionLanded`, and the
	// atom does not print that word: the standalone face reported one line per
	// branch ("merge <b> … landed"), while `gopi task merge <branch>...` reports
	// the per-branch rows AND a final tally whose own counters are
	// `merged=… deleted=…`. Asserting the old word would therefore have been an
	// assertion about WHICH FACE ran, not about whether the branch landed — which
	// is what this test is for. So the report half now asks the question the
	// face-independent way: the branch is NAMED by the run and the run's own
	// tally says it merged something. The landing itself is verified against git
	// on the next assertion, which is the measurement that cannot be talked into
	// agreement by a printed line.
	if !strings.Contains(mout, feat) || !strings.Contains(mout, mergeTallyMergedKey) {
		t.Errorf("merge run reported no landing for %s — starvation (the recycle should have made it a candidate)\nmerge output:\n%s", feat, mout)
	}
	t.Logf("merge output:\n%s", mout)
	if !chainCommitAncestorOfMaster(t, repo, tip) {
		t.Errorf("merge run did NOT merge %s into master (starvation — the recycle should have made it a merge candidate)\nmerge output:\n%s", feat, mout)
	}
	if err := os.Remove(merr); err != nil && !os.IsNotExist(err) {
		t.Errorf("remove the merge run's lock file %s: %v", merr, err)
	}
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", feat).Run(); err == nil {
		t.Logf("note: %s still present after the landing (the ref was kept: the run reports the deletion failure it hit)", feat)
	} else {
		t.Logf("branch %s deleted by the merge run after landing (branch -d)", feat)
	}
}

// TestWorkerServerDeliveryDirtyWorktreeKeepsBranch is epic QC #4: a worker
// whose worktree has UNCOMMITTED changes must NOT have it removed — the
// recycle fails soft and never destroys work. The dirty file simulates a
// live bond still writing (or an unfinished run) at terminal delivery.
func TestWorkerServerDeliveryDirtyWorktreeKeepsBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	feat := "feat/worker-dirty-e2e"
	chainGit(t, root, "init", repo)
	chainGit(t, repo, "config", "user.email", "t@t")
	chainGit(t, repo, "config", "user.name", "t")
	chainGit(t, repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	chainGit(t, repo, "add", "a.txt")
	chainGit(t, repo, "commit", "-m", "init")
	chainGit(t, repo, "checkout", "-b", feat)
	chainGit(t, repo, "checkout", "master")
	chainGit(t, repo, "worktree", "add", wt, feat)
	// Uncommitted work in the worktree = live bond protection.
	if err := os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("live bond writing"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := workerRecycleEnv(t, repo, "") // the REAL zpeer shape: no role field, no peers
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"worker dirty e2e", "--payload", "do the deliverable work", "--task-id", "1085f792-7c16-573d-bc39-dbf503a9ca14",
		"--repo", repo},
		"", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	stdout, stderr, code := runGopiMapped(t, []string{"worker", "--once"},
		"", env, wt)
	if code != 0 {
		t.Fatalf("worker --once must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	// The dirty worktree MUST survive (fail-soft, never destroy work).
	if !chainWorktreeHoldsBranch(t, repo, feat) {
		t.Errorf("dirty worktree was removed — the recycle must fail soft on uncommitted work")
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree dir %s must survive a dirty skip: %v", wt, err)
	}
	if _, err := os.Stat(filepath.Join(wt, "wip.txt")); err != nil {
		t.Errorf("uncommitted work must survive: %v", err)
	}
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", feat).Run(); err != nil {
		t.Errorf("branch %s must survive a dirty skip", feat)
	}
}

// workerRecycleStaleSiblingRow seeds a TERMINAL sibling row that also names
// worktree the way the live ledger did on 2026-09-12 (row 01a08cfd completed,
// pinned behind CANCELLED sibling 01a08cd5 on the same directory): status
// cancelled, a claim stamped by THIS host with a pid that is provably gone.
// Before the fix that sibling was treated as the directory's legitimate owner
// and blocked the delivered row's recycle for good.
//
// THE CLAIM STAMP'S HOST IS THIS TEST HOST, NOT A FIXTURE-ONLY HOSTNAME
// (2026-10-03). The "dead owner" disposition is a LOCAL verdict by
// construction — claimHolder resolves a stamp against this host
// (finishCleanupDeadLocalHolder: "no stamp / foreign host: unknowable here —
// never dead"). A fixture that stamps an INVENTED host name therefore does not
// model a stale sibling at all: the real arm would call that row's holder
// unknowable and keep it blocking, while the CREATE half of the rule
// (taskCreationGroundBorrowed) uses the same predicate and — before this test
// did — could not tell the difference either. The stamp is spelled the way the
// production claim seam spells it (claimHolderOfPid: hostname ":" pid), so the
// dead-owner verdict under test is the one the machinery actually reaches.
const (
	workerRecycleStaleSiblingID = "t-stale-sibling"

	// mergeEngineVerbWord is the argv this test hands the binary to run the merge
	// engine. It is a NAMED constant rather than a bare literal (Rule 1) and
	// rather than the `CliVerbMerge` constant, which was DELETED with the
	// dispatch (epic merge-removal, item 2).
	//
	// IT IS THE SECOND WORD OF A TWO-WORD ADDRESS NOW, and that is a
	// measurement rather than a preference: the standalone `merge` word refuses
	// (measured — `gopi merge` exits 1 with the retirement sentence), so an argv
	// of `[]string{"merge"}` would assert a landing the binary can no longer
	// perform. The engine's live address is the FAMILY spelling `gopi task
	// merge`, which runs the same engine under the same single-instance lock —
	// and this test's subject is the ENGINE's landing, not the address it is
	// reached through. The caller composes `{CliVerbTask, mergeEngineVerbWord}`.
	mergeEngineVerbWord = "merge"

	// mergeTallyMergedKey is the counter the atom's summary line names when it
	// merged a branch. It is the face-independent half of "it landed": the word
	// the standalone face printed per branch (`landed`) is not printed here, so
	// reading that one would have measured which face ran rather than whether
	// the work landed.
	mergeTallyMergedKey = "merged=1"

	// workerRecycleAbsentPidSearchSpan bounds how far above the kernel's
	// pid_max the fixture looks for a provably-absent pid before it gives up.
	workerRecycleAbsentPidSearchSpan = 64
	// kernelPidMaxPath is the host knob the search floor is DERIVED from: a
	// pid above this cannot be allocated, so an absence verdict on it is
	// stable. Never a bare literal (Rule 1) — the host states its own limit.
	kernelPidMaxPath = "/proc/sys/kernel/pid_max"
)

// workerRecycleAbsentPid returns a pid that is PROVABLY absent on this host —
// one ABOVE the kernel's pid_max, where no process can be allocated. It fails
// the test rather than guessing (Rule 15): an "alive" pid here would make this
// test assert the OPPOSITE of what it means to (live holders legitimately
// block).
//
// Why above pid_max rather than "any pid not currently in /proc" (measured
// 2026-10-03 on zpeer, this test's own flake): the old search scanned
// 3,000,000..3,999,999, a window that OVERLAPS the live pid range — live pids
// read 3043482/3044206/3044208 and later 3977341 while ns_last_pid showed
// 1,648,062, i.e. the allocator hands out free pids from the low end and had
// already climbed past 3,000,000. The fixture observes an absence once and the
// production seam reads liveness LATER, so a child spawned in between could
// take exactly the pid the fixture had just declared dead; the seam then saw a
// LIVE holder, refused the stamp, and the case failed for a reason that has
// nothing to do with the code under test.
func workerRecycleAbsentPid(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(kernelPidMaxPath)
	if err != nil {
		t.Fatalf("cannot read %s to derive a provably-absent pid: %v", kernelPidMaxPath, err)
	}
	pidMax, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pidMax <= 0 {
		t.Fatalf("%s holds %q, which is not a pid limit", kernelPidMaxPath, strings.TrimSpace(string(raw)))
	}
	for pid := pidMax + 1; pid <= pidMax+workerRecycleAbsentPidSearchSpan; pid++ {
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); os.IsNotExist(err) {
			return pid
		}
	}
	t.Fatalf("no absent pid found in (%d, %d] — cannot model a dead holder", pidMax, pidMax+workerRecycleAbsentPidSearchSpan)
	return 0
}

func workerRecycleStaleSiblingRow(t *testing.T, env []string, wt string) {
	t.Helper()
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skipf("hostname unavailable: %v", err)
	}
	// The stamp must be the form the PRODUCTION claim seam writes (see the
	// const block above) — same host spelling, so the dead-owner verdict this
	// test depends on is reachable on the host running the test.
	claimedBy := host + ":" + strconv.Itoa(workerRecycleAbsentPid(t)) // red-proof: change the host here and the seam refuses again
	// Seed into the SANDBOXED ledger the run itself uses (the env's
	// GOPI_MEMORY_DB), never the ambient one: a test that writes a row into
	// the operator's real ledger both leaks state and turns a repeated run
	// into a UNIQUE-constraint failure (the 2026-09-12 suite hit exactly
	// that). t.Setenv also keeps a caller that forgot the env honest.
	dbPath := envValue(env, memoryDBEnv)
	if dbPath == "" {
		t.Fatalf("test env carries no %s — the fixture must sandbox the ledger", memoryDBEnv)
	}
	t.Setenv(memoryDBEnv, dbPath)
	db, err := tools.OpenTasksDBForSweep(context.Background())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	deliverable := `{"repo":"/tmp/other-repo","worktree":"` + wt + `"}`
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO tasks (task_id, summary, status, claimed_by, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'auto', ?, ?)`,
		workerRecycleStaleSiblingID, "stale sibling", tools.TaskStatusCancelled,
		claimedBy, deliverable, 1.0, 1.0); err != nil {
		t.Fatalf("seed stale sibling row: %v", err)
	}
}

// envValue reads one KEY=VALUE entry out of a testutil env slice (the sandbox
// variables a helper must honour rather than re-derive).
func envValue(env []string, key string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

// TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow is the epic's
// foreign-owner negative case measured on the DELIVERY path (not the sweep):
// a task's worktree is released at delivery even though another row names the
// same directory — because that other row's life is OVER and its holder pid is
// GONE. Before the fix this exact shape stranded /home/user/workspace/gopi-cov-agent
// (completed row 01a08cfd blocked by cancelled sibling 01a08cd5) and the
// delivered row's chain reported "worktree ownership mismatch".
//
// THE PRECONDITION the test depends on is that the sibling is a DEAD owner and
// not merely a non-live row: the seam asks the SAME holder predicate the
// delivery arm asks (finishCleanupOwnerRowLive), so a sibling stamped by a LIVE
// pid — or by a foreign host, which this host cannot measure — keeps REFUSING
// the stamp and this fixture's read-back guard fails. Reachability of the
// opposite branch is established by construction, not by a second stub: the
// seeding helper's own contract (workerRecycleStaleSiblingRow) is "a pid that
// is PROVABLY absent on this host", and the package's unit fixtures
// (TestCreationSeamRefusesABorrowedWorktree, TestCreationSeamStillRecordsAnUnownedGround)
// drive the refusal and the unowned arms directly.
func TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	feat := "feat/worker-stale-sibling"
	chainGit(t, root, "init", repo)
	chainGit(t, repo, "config", "user.email", "t@t")
	chainGit(t, repo, "config", "user.name", "t")
	chainGit(t, repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	chainGit(t, repo, "add", "a.txt")
	chainGit(t, repo, "commit", "-m", "init")
	chainGit(t, repo, "checkout", "-b", feat)
	if err := os.WriteFile(filepath.Join(repo, "b.txt"), []byte("deliverable work"), 0o644); err != nil {
		t.Fatal(err)
	}
	chainGit(t, repo, "add", "b.txt")
	chainGit(t, repo, "commit", "-m", "feature work")
	chainGit(t, repo, "checkout", "master")
	chainGit(t, repo, "worktree", "add", wt, feat)

	env := workerRecycleEnv(t, repo, "server")
	workerRecycleStaleSiblingRow(t, env, wt) // ← the blocked-forever shape

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"worker stale-sibling recycle", "--payload", "do the deliverable work", "--task-id", "837d2422-cf6d-5e7c-8903-288414525c1b",
		"--repo", repo},
		"", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}
	// The SAME production seam as the chain test above: the delivered row
	// RECORDS this directory as its ground, so the stale sibling and the
	// delivered row name the SAME path and the release pass must tell the dead
	// owner from the live one. Without the stamp the stale row would be
	// claiming a directory no live row owns, and the rejection under test
	// (foreign owner) would never be exercised.
	workerRecycleStampGround(t, env, "837d2422-cf6d-5e7c-8903-288414525c1b", wt, feat)
	stdout, stderr, code := runGopiMapped(t, []string{"worker", "--once"}, "", env, repo)
	if code != 0 {
		t.Fatalf("worker --once must succeed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if ground := workerRecycleRecordedGround(t, env, "837d2422-cf6d-5e7c-8903-288414525c1b"); ground != wt {
		t.Fatalf("fixture: the row must record its ground as %s, but deliverable.worktree reads %q", wt, ground)
	}
	if chainWorktreeHoldsBranch(t, repo, feat) {
		t.Errorf("a DEAD sibling row must not block the delivery recycle: %s still holds %s", wt, feat)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree dir %s must be recycled despite the stale sibling row", wt)
	}
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", feat).Run(); err != nil {
		t.Errorf("branch %s must survive (merger input): %v", feat, err)
	}
}

// (the ledger-path variable is the package-level memoryDBEnv defined in
// main_test.go — Rule 14: one definition, so this fixture names it nowhere
// else; the const that used to live here was a second copy.)
