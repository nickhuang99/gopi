package tools

// Shared git-fixture helpers for the close-out suites.
//
// They live here because they OUTLIVED the two files that first declared them
// (task_finish_premerge_test.go and task_finish_deliverysync_test.go): the
// trial-merge gate and the delivery-point sync were REMOVED on 2026-10-12 (their
// own tests went with them), while half a dozen other suites still stand on the
// real two-node git worlds these helpers build. Deleting the tests that pinned
// the removed behaviour must not delete the tools every other test uses, so the
// survivors are collected in one place and named for what they are.
//
// Every fixture here is REAL git (no mocks): the questions the close-out asks —
// does this ref exist on the peer, is this commit an ancestor of that one, is
// the worktree still on disk — are git's own semantics, and a mock would answer
// a question nobody asked.
import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// --- the declared smoke entry point, staged into a fixture tree ---
//
// pmsScriptsDir / pmsHeavyInventory are the fixture tree's copy of the ONE
// declared smoke entry point and the heavy inventory its Makefile target reads —
// staged from this checkout so a fixture check runs THE declared command (Rule
// 14: the entry point is never re-spelled here).
const (
	pmsScriptsDir     = "scripts"
	pmsEntryPoint     = "scripts/test-budget.sh"
	pmsHeavyInventory = "testdata/heavy-tests.tsv"
	pmsMakefile       = "Makefile"
	// pmsClosureMentionRe finds a repo-relative path to another file under the
	// script tree, as the scripts THEMSELVES spell it. The closure below starts
	// from these mentions instead of from a hand-written file list (Rule 14):
	// a list would silently degrade into under-staging the moment the entry
	// point gains a sourced library, and the fixture would then refuse a branch
	// for a reason that has nothing to do with the branch.
	pmsClosureMentionRe = `scripts/[A-Za-z0-9_./-]+`
	// pmsClosureVarRe finds a `$root/<name>` (or `$root/${name}`) reference —
	// the shape the entry point uses to source a library whose path it holds in
	// a variable, so the path itself never appears as a literal mention.
	pmsClosureVarRe = `\$root/(?:\$\{)?([A-Za-z_][A-Za-z0-9_]*)(?:\})?`
	// pmsClosureAssignRe finds a shell variable assignment, which is where the
	// value of a `$root/$name` reference is declared.
	pmsClosureAssignRe = `^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)=["']?([^"'\s$]+)["']?`
	// pmsClosureMentionTrailingCut trims the punctuation a path can abut in
	// prose or in a shell word.
	pmsClosureMentionTrailingCut = `.,;:)"'`
	// pmsClosureCap bounds the walk: a mention graph that does not converge is a
	// bug in this fixture, not a slow test.
	pmsClosureCap = 128
	// pmsGoExt is the suffix that decides which staged files the Go tool would
	// try to build inside the fixture module (see pmsStagedGoFiles).
	pmsGoExt = ".go"
)

// pmsRepoRoot is the checkout this test file lives in (the script tree and the
// heavy inventory are read from it).
func pmsRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(pmsScriptsDir))); err != nil {
		t.Fatalf("no %s under %s: %v — the arm cannot stage the declared entry point", pmsScriptsDir, root, err)
	}
	return root
}

// pmsStageEntryPoint copies the repo's OWN declared smoke contract — the
// Makefile that defines the scoped target, the entry point script, the scripts
// that entry point actually sources, and the heavy inventory that target reads —
// into the fixture, so the check runs THE declared command, resolved inside the
// checkout, exactly as the production lane will. The copy is a fixture MIRROR of
// the tree, never a mutation of it (Rule 6).
//
// IT STAGES THE ENTRY POINT'S OWN SOURCE CLOSURE, NOT THE WHOLE scripts/ TREE.
// The whole tree carries real Go packages of its own (scripts/lib/ledgersql is
// one), and the fixture module is stdlib-only on purpose — Rule 35: giving it a
// `require` would make this gate depend on a warm module cache and on a large
// artifact cache. A staged Go package whose third-party import cannot resolve
// therefore fails AT BUILD, on every run, and that permanently-red package is
// what an operator then reads in the branch-check refusal:
//
//	FAIL pmcfixture/scripts/lib/ledgersql [setup failed]
//
// — a package nobody asked about, inside a 12-line quote that has to carry the
// ACTUAL failing test. The fixture needs none of it, so the STAGED CODE gives
// way, exactly as the sibling fixture at internal/testutil does.
//
// THE CLOSURE IS DERIVED FROM THE SCRIPTS' OWN TEXT (Rule 14) — every
// `scripts/…` path a staged file spells, plus the value of every variable it
// sources through `$root/$name`. A hand-written file list would silently
// degrade into UNDER-STAGING the moment the entry point gains a sourced
// library, and the fixture would then refuse a branch for a reason that has
// nothing to do with the branch.
func pmsStageEntryPoint(t *testing.T, repoRoot, dir string) {
	t.Helper()
	mk, err := os.ReadFile(filepath.Join(repoRoot, pmsMakefile))
	if err != nil {
		t.Fatalf("read the repo's %s: %v", pmsMakefile, err)
	}
	if err := os.WriteFile(filepath.Join(dir, pmsMakefile), mk, 0o644); err != nil {
		t.Fatal(err)
	}
	pmsStageScriptClosure(t, repoRoot, dir)
	invSrc := filepath.Join(repoRoot, filepath.FromSlash(pmsHeavyInventory))
	invDst := filepath.Join(dir, filepath.FromSlash(pmsHeavyInventory))
	if err := os.MkdirAll(filepath.Dir(invDst), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(invSrc)
	if err != nil {
		t.Fatalf("read %s: %v", pmsHeavyInventory, err)
	}
	if err := os.WriteFile(invDst, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// pmsStageScriptClosure stages <dir>/scripts with exactly the files the entry
// point's own text reaches: the entry point, every repo-relative `scripts/…`
// path any staged file spells, and every shell variable a staged file sources
// through `$root/<name>` whose assignment names such a path. Files keep their
// permission bits, so an executable stays executable.
func pmsStageScriptClosure(t *testing.T, repoRoot, dir string) {
	t.Helper()
	mention := regexp.MustCompile(pmsClosureMentionRe)
	rootVar := regexp.MustCompile(pmsClosureVarRe)
	assign := regexp.MustCompile(pmsClosureAssignRe)
	if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(pmsEntryPoint))); err != nil {
		t.Fatalf("the repo carries no %s: %v — the fixture cannot stage an entry point that is not there",
			pmsEntryPoint, err)
	}
	queued := []string{pmsEntryPoint}
	staged := map[string]bool{}
	for len(queued) > 0 {
		if len(staged) >= pmsClosureCap {
			t.Fatalf("the entry point's staged closure exceeded %d files — the mention graph must "+
				"converge, and a walk that does not is a bug in this fixture", pmsClosureCap)
		}
		rel := queued[0]
		queued = queued[1:]
		if staged[rel] || !strings.HasPrefix(rel, pmsScriptsDir+"/") {
			continue
		}
		src := filepath.Join(repoRoot, filepath.FromSlash(rel))
		info, serr := os.Stat(src)
		if serr != nil || !info.Mode().IsRegular() {
			// A prose mention of a path that is not a regular file is not a
			// dependency of the closure.
			continue
		}
		raw, rerr := os.ReadFile(src)
		if rerr != nil {
			t.Fatalf("read %s out of the repo's closure: %v", rel, rerr)
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, raw, info.Mode().Perm()); err != nil {
			t.Fatalf("stage %s: %v", rel, err)
		}
		staged[rel] = true
		text := string(raw)
		for _, m := range mention.FindAllString(text, -1) {
			queued = append(queued, strings.TrimRight(m, pmsClosureMentionTrailingCut))
		}
		values := map[string]string{}
		for _, m := range assign.FindAllStringSubmatch(text, -1) {
			values[m[1]] = strings.TrimRight(m[2], pmsClosureMentionTrailingCut)
		}
		for _, m := range rootVar.FindAllStringSubmatch(text, -1) {
			if v, ok := values[m[1]]; ok {
				queued = append(queued, v)
			}
		}
	}
	if len(staged) == 0 {
		t.Fatalf("staged 0 files from the entry point's closure — a fixture with no scripts must be a " +
			"loud failure, not a vacuous pass")
	}
	if !staged[pmsEntryPoint] {
		t.Fatalf("the staged closure carries no %s (it carries %d file(s)) — the entry point must be "+
			"the repo's own file", pmsEntryPoint, len(staged))
	}
}

// pmsStagedGoFiles counts the .go files the stage put under <dir>/scripts — the
// population the Go tool resolves against a fixture module that declares no
// `require`.
func pmsStagedGoFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	for _, rel := range pmsStagedScriptFiles(t, dir) {
		if filepath.Ext(rel) == pmsGoExt {
			n++
		}
	}
	return n
}

// pmsStagedScriptFiles lists the files a closure stage put under <dir>/scripts,
// as repo-relative slash paths. It is the reader the regression arm uses to say
// what the fixture actually carries.
func pmsStagedScriptFiles(t *testing.T, dir string) []string {
	t.Helper()
	root := filepath.Join(dir, filepath.FromSlash(pmsScriptsDir))
	var got []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk the staged %s tree: %v", pmsScriptsDir, err)
	}
	return got
}

// --- primitives ---

// preMergeGit runs one git command in dir with a deterministic identity and no
// ambient configuration (the finishCleanupFixture convention), and returns the
// combined output.
func preMergeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// preMergeGitErr is the failure-tolerant twin (used where a non-zero exit IS
// the observation, e.g. an unresolvable ref).
func preMergeGitErr(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	return cmd.Run()
}

// serverHasRefRuns asserts the peer holds refs/heads/<branch> — the physical
// statement "a push happened".
func serverHasRefRuns(t *testing.T, serverDir, branch string) bool {
	t.Helper()
	return preMergeGitErr(serverDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch) == nil
}

// advanceServerMaster commits one change to the PEER's master, through a
// scratch clone rewritten to have no remote of its own (so nothing else can be
// pushed by accident), and returns the new peer tip. It is how a test makes the
// peer move ahead of a branch — the skew the delivery lane no longer resolves
// and the merger now owns.
func (f *finishCleanupFixture) advanceServerMaster(t *testing.T, file, content, message string) string {
	t.Helper()
	clone := filepath.Join(filepath.Dir(f.serverDir), "server-advance")
	preMergeGit(t, filepath.Dir(f.serverDir), "clone", f.serverDir, clone)
	preMergeGit(t, clone, "checkout", masterBranchName)
	if err := os.WriteFile(filepath.Join(clone, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	preMergeGit(t, clone, "add", file)
	preMergeGit(t, clone, "commit", "-m", message)
	preMergeGit(t, clone, "push", "origin", masterBranchName)
	return preMergeGit(t, f.serverDir, "rev-parse", masterBranchName)
}

// countCommits counts the commits reachable from rev.
func countCommits(t *testing.T, dir, rev string) int {
	t.Helper()
	out := preMergeGit(t, dir, "rev-list", "--count", rev)
	n := 0
	for _, r := range strings.TrimSpace(out) {
		if r < '0' || r > '9' {
			t.Fatalf("rev-list --count %s: unparsable %q", rev, out)
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// --- settings / ledger seams ---

// withDeclaredBuild points the repo's DECLARED contracts at commands the test
// chooses (Rule 12's source of truth is the settings table; the resolver is
// bound to one file, so the fixture offers a directory as the repo path).
//
// The contract itself is written once per test into a sandbox settings file and
// read through the production resolver (config.ResolveRepoBuild) — a lane is
// never handed a command directly, so what is exercised is the real lookup.
func withDeclaredBuild(t *testing.T, repo, buildCmd string) {
	t.Helper()
	settings := filepath.Join(t.TempDir(), "settings.json")
	body := `{"repos":{"` + repo + `":{"build":"` + buildCmd + `"}}}`
	if err := os.WriteFile(settings, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settings)
}

// preMergeSeedIsolation points the ledger at a sandbox file so a lane's own
// audit write never touches the ambient ledger, and returns the path.
func preMergeSeedIsolation(t *testing.T) string {
	t.Helper()
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	return db
}

// preMergeLaneEvent reads back what a lane recorded on the row's own trail: the
// newest task_events row whose event name carries the marker, with its detail.
// It is the reader half of the audit assertion — the writer is
// finishCleanupBranchCheckReason.
func preMergeLaneEvent(t *testing.T, db *sql.DB, taskID, marker string) (event, detail string) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT event, detail FROM task_events WHERE task_id = ? ORDER BY id DESC`, taskID)
	if err != nil {
		t.Fatalf("read the row's trail: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ev, det string
		if err := rows.Scan(&ev, &det); err != nil {
			t.Fatalf("scan the row's trail: %v", err)
		}
		if strings.Contains(det, marker) {
			return ev, det
		}
	}
	t.Fatalf("no trail event for %s carried %q", taskID, marker)
	return "", ""
}

// preMergeSeedRow inserts one terminal row carrying the fixture's deliverable,
// so a chain's own ledger lookup resolves it (the ownershipSeedRow shape).
func preMergeSeedRow(t *testing.T, conn *sql.DB, taskID, deliverable string) {
	t.Helper()
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'auto', ?, ?)`,
		taskID, "close-out "+taskID, TaskStatusCompleted, deliverable, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed row %s: %v", taskID, err)
	}
}

// --- one repo, master + a feat branch checked out in a linked worktree ---

// localSyncFixture is a single-node world: a main checkout sitting on the
// integration branch, and ONE feature branch parked in a linked worktree — the
// shape a finished worker leaves behind on a single-node host.
type localSyncFixture struct {
	t          *testing.T
	repoDir    string // main checkout (on master)
	wtDir      string // linked worktree on the feat branch
	featBranch string
}

// newLocalSyncFixture builds: master (one commit) → feat branch (one commit) →
// the feat branch checked out in a linked worktree, master back in the main
// checkout.
func newLocalSyncFixture(t *testing.T) *localSyncFixture {
	t.Helper()
	root := t.TempDir()
	f := &localSyncFixture{
		t:          t,
		repoDir:    filepath.Join(root, "repo"),
		wtDir:      filepath.Join(root, "wt"),
		featBranch: "feat/delivery-sync",
	}
	preMergeGit(t, root, "init", "-q", "-b", masterBranchName, f.repoDir)
	preMergeGit(t, f.repoDir, "config", "user.email", "t@t")
	preMergeGit(t, f.repoDir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(f.repoDir, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	preMergeGit(t, f.repoDir, "add", "a.txt")
	preMergeGit(t, f.repoDir, "commit", "-q", "-m", "init")
	preMergeGit(t, f.repoDir, "checkout", "-q", "-b", f.featBranch)
	if err := os.WriteFile(filepath.Join(f.repoDir, "b.txt"), []byte("deliverable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	preMergeGit(t, f.repoDir, "add", "b.txt")
	preMergeGit(t, f.repoDir, "commit", "-q", "-m", "deliverable work")
	preMergeGit(t, f.repoDir, "checkout", "-q", masterBranchName)
	preMergeGit(t, f.repoDir, "worktree", "add", "-q", f.wtDir, f.featBranch)
	return f
}

// advanceMaster commits one change to the integration branch IN THE MAIN
// checkout (which stays on master for the whole fixture — the shape of a
// single-node host: the worker's branch is parked in a linked worktree while the
// main checkout's master moves on). Returns the new master tip.
func (f *localSyncFixture) advanceMaster(t *testing.T, file, content, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.repoDir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	preMergeGit(t, f.repoDir, "add", file)
	preMergeGit(t, f.repoDir, "commit", "-q", "-m", message)
	return preMergeGit(t, f.repoDir, "rev-parse", masterBranchName)
}

// commitInWorktree commits one file inside the delivered branch's worktree.
func (f *localSyncFixture) commitInWorktree(t *testing.T, file, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.wtDir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	preMergeGit(t, f.wtDir, "add", file)
	preMergeGit(t, f.wtDir, "commit", "-q", "-m", message)
}

// runServerRecycle runs the LOCAL arm (the one this host's deployment takes: no
// sync peer declared) with the fixture's deliverable.
func (f *localSyncFixture) runServerRecycle() *FinishCleanupReport {
	f.t.Helper()
	f.t.Setenv(finishCleanupExemptEnv, "")
	rep, err := finishCleanupServerRecycle(context.Background(),
		&FinishCleanupReport{TaskID: "t-local-sync", Repo: f.repoDir},
		taskFinishDeliverable{Repo: f.repoDir, Branch: f.featBranch, Worktree: f.wtDir}, false)
	if err != nil {
		f.t.Fatalf("server recycle: %v", err)
	}
	return rep
}
