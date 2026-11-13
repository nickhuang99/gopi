package main

// task_claim_ground_test.go — the claim-time ground stamp as the WRAPPER
// reaches it (epic branch↔task linkage, item [2/5], 2026-09-16).
//
// internal/tools' own test drives the resolution (in a linked worktree of a
// fixture repo, over a temp ledger). THIS one drives the seam the wrapper
// calls, over a REAL ledger at a temp GOPI_MEMORY_DB and REAL git (a main
// checkout + two linked worktrees of it) — so the stamp is verified on a
// claimed, in_progress row rather than a synthetic blob.
//
// The two halves of the acceptance:
//
//	① the row is claimed and in_progress (not completed, branch unproven) and
//	   its deliverable already names BOTH the branch and the worktree;
//	② the directory stamped is the executor child's ACTUAL cwd (childDir,
//	   after executorChildDir's escape) — a child escaped out of a FOREIGN
//	   linked worktree must never bind its row to that tree.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nick/gopi/internal/tools"
)

const (
	claimGroundTaskID   = "t-claim-ground-seam"
	claimGroundBranch   = "feat/claim-ground"
	claimGroundOtherWT  = "feat/claim-ground-other"
	claimGroundSecondID = "t-claim-ground-second"
)

// claimGroundFixture builds the shape the seam must read: one repo (master)
// with TWO linked worktrees, and a claimed + started row whose
// deliverable.repo is that repo (the submit-time repo-only placeholder).
func claimGroundFixture(t *testing.T) (repo, wt, otherWT string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	wt = filepath.Join(root, "wt")
	otherWT = filepath.Join(root, "wt-other")
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(root, "init", repo)
	run(repo, "config", "user.email", "t@t")
	run(repo, "config", "user.name", "t")
	run(repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", "base.txt")
	run(repo, "commit", "-m", "base")
	run(repo, "worktree", "add", "-b", claimGroundBranch, wt, "master")
	run(repo, "worktree", "add", "-b", claimGroundOtherWT, otherWT, "master")
	run(repo, "checkout", "master")

	// The row: claimed and started, exactly as the wrapper leaves it before
	// the spawn seam runs.
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "claim-time ground stamp (seam)", Repo: repo, TaskID: claimGroundTaskID,
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	if err := tools.TaskClaimExplicit(claimGroundTaskID, "claimground:1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := tools.TaskStart(claimGroundTaskID, "claimground:1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	return repo, wt, otherWT
}

// claimGroundDeliverable reads the row's deliverable blob + status straight
// from the temp ledger (the same file the seam writes).
func claimGroundDeliverable(t *testing.T) (blob, status string) {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(os.Getenv("GOPI_MEMORY_DB")))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT deliverable, status FROM tasks WHERE task_id = ?`, claimGroundTaskID).Scan(&blob, &status); err != nil {
		t.Fatalf("read row: %v", err)
	}
	return strings.TrimSpace(blob), status
}

// TestClaimGroundStampNamesBranchAndWorktreeInFlight is acceptance ① through
// the wrapper's own seam.
func TestClaimGroundStampNamesBranchAndWorktreeInFlight(t *testing.T) {
	_, wt, _ := claimGroundFixture(t)

	blob, status := claimGroundDeliverable(t)
	if status != tools.TaskStatusInProgress {
		t.Fatalf("the row must be in flight for this assertion, got status %q", status)
	}
	if strings.Contains(blob, `"branch"`) {
		t.Fatalf("the claimed row must not name a branch before the stamp runs: %s", blob)
	}

	gotWT, gotBranch, written := taskClaimGroundStamp(claimGroundTaskID, wt)
	if !written || gotWT != wt || gotBranch != claimGroundBranch {
		t.Fatalf("claim stamp = (%q, %q, written=%v), want (%q, %q, true)", gotWT, gotBranch, written, wt, claimGroundBranch)
	}

	blob, status = claimGroundDeliverable(t)
	if status != tools.TaskStatusInProgress {
		t.Fatalf("the stamp must not move the row's status: got %q", status)
	}
	var d struct {
		Repo     string `json:"repo"`
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(blob), &d); err != nil {
		t.Fatalf("blob is not valid JSON (%s): %v", blob, err)
	}
	if d.Branch != claimGroundBranch || d.Worktree != wt {
		t.Fatalf("in-flight row must name branch=%q worktree=%q, got %s", claimGroundBranch, wt, blob)
	}
	if d.Repo == "" {
		t.Fatalf("the stamp must keep the repo it paired against: %s", blob)
	}
	if n := strings.Count(blob, `"worktree"`); n != 1 {
		t.Fatalf("the worktree key must appear exactly once (one spelling, Rules 13/14): %d in %s", n, blob)
	}
}

// TestClaimGroundStampRespectsWorktreeOwnership covers the two gates the
// claim-time stamp inherits from its ONE writer, over the real ledger:
//
//	(a) only a LINKED WORKTREE of the row's repo resolves at all — the
//	    repo's own main checkout and an unrelated directory stamp nothing
//	    (the resolution gate; branch master is exempt by the same rule);
//	(b) a linked worktree of the row's repo DOES bind (positive control —
//	    proves (a) is the resolution, not a broken fixture);
//	(c) a worktree ANOTHER row already names is NOT re-bound to this row:
//	    the worktree key is an ownership claim (reclaimSelfRowsFrom and the
//	    sweep's ownership arm both keep a named worktree alive), so two rows
//	    naming one directory would double-own it — the 2026-09-12 hijack.
func TestClaimGroundStampRespectsWorktreeOwnership(t *testing.T) {
	repo, _, otherWT := claimGroundFixture(t)

	if blob, _ := claimGroundDeliverable(t); strings.Contains(blob, `"worktree"`) {
		t.Fatalf("precondition: the fresh row must carry no worktree: %s", blob)
	}

	// (a) the main checkout and an unrelated dir resolve to no ground.
	for _, dir := range []string{repo, t.TempDir()} {
		if wt, branch, written := taskClaimGroundStamp(claimGroundTaskID, dir); written || wt != "" || branch != "" {
			t.Fatalf("a claim resolved at %q must stamp no ground, got (%q, %q, %v)", dir, wt, branch, written)
		}
	}
	if blob, _ := claimGroundDeliverable(t); strings.Contains(blob, `"worktree"`) {
		t.Fatalf("a ground-less claim must leave the row untouched: %s", blob)
	}

	// (b) the linked worktree itself DOES bind.
	if gotWT, gotBranch, written := taskClaimGroundStamp(claimGroundTaskID, otherWT); !written || gotWT != otherWT || gotBranch != claimGroundOtherWT {
		t.Fatalf("positive control: stamping %q must bind it, got (%q, %q, written=%v)", otherWT, gotWT, gotBranch, written)
	}
	blob, _ := claimGroundDeliverable(t)
	var d struct {
		Worktree string `json:"worktree"`
		Branch   string `json:"branch"`
	}
	if err := json.Unmarshal([]byte(blob), &d); err != nil {
		t.Fatalf("blob is not valid JSON (%s): %v", blob, err)
	}
	if d.Worktree != otherWT || d.Branch != claimGroundOtherWT {
		t.Fatalf("the control stamp must be the row's recorded ground, got %s", blob)
	}

	// (c) a SECOND row claiming through the same worktree records nothing.
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "claim-time ground stamp (second claimant)", Repo: repo, TaskID: claimGroundSecondID,
	}); err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if wt, branch, written := tools.TaskWorktreeStampClaimerFor(claimGroundSecondID, repo, otherWT); written {
		t.Fatalf("a worktree owned by another row must not be re-bound: (%q, %q)", wt, branch)
	}
	secondBlob := claimGroundBlobOf(t, claimGroundSecondID)
	if strings.Contains(secondBlob, `"worktree"`) {
		t.Fatalf("the second row must stay ground-less on a taken worktree: %s", secondBlob)
	}
	if firstBlob, _ := claimGroundDeliverable(t); firstBlob != blob {
		t.Fatalf("the first row's ground must survive a second claim:\n before=%s\n after =%s", blob, firstBlob)
	}
}

// claimGroundRepoPath reads the fixture row's deliverable.repo (the repo the
// worktrees belong to).
func claimGroundRepoPath(t *testing.T) string {
	t.Helper()
	blob, _ := claimGroundDeliverable(t)
	var d struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal([]byte(blob), &d); err != nil || d.Repo == "" {
		t.Fatalf("fixture row must name its repo: %s (%v)", blob, err)
	}
	return d.Repo
}

// claimGroundBlobOf reads ANY row's deliverable blob from the temp ledger.
func claimGroundBlobOf(t *testing.T, taskID string) string {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(os.Getenv("GOPI_MEMORY_DB")))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var blob string
	if err := db.QueryRow(`SELECT deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&blob); err != nil {
		t.Fatalf("read row %s: %v", taskID, err)
	}
	return strings.TrimSpace(blob)
}

// TestClaimGroundStampNoRepoIsANoop: a row with no deliverable.repo (an
// auto-created row, or a submit from outside a work tree) has nothing to pair
// — the seam is a silent no-op, never an error (Rule 15: the no-op is by
// contract, and the row stays exactly as its creator left it).
func TestClaimGroundStampNoRepoIsANoop(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	const repoLess = "t-claim-ground-norepo"
	if _, err := tools.TaskNew("claim-time ground stamp (repo-less)", repoLess, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if wt, branch, written := taskClaimGroundStamp(repoLess, t.TempDir()); written || wt != "" || branch != "" {
		t.Fatalf("a repo-less row must be a no-op, got wt=%q branch=%q written=%v", wt, branch, written)
	}
}

// ---------------------------------------------------------------------------
// STRUCTURAL HALF: the wrapper's call-site.
//
// The behaviour above is driven through the seam the wrapper calls, because
// the WRAPPER SEQUENCE itself cannot be driven in-process: the only hermetic
// way to run runTaskRunOnce without spawning a model-backed child is the
// taskChildFailTestEnv escape, and that escape RETURNS before `childDir` is
// resolved — i.e. before the stamp is reachable. So the placement contract is
// pinned the way this repo pins an unobservable-in-process compose (precedent:
// task_run_release_coverage_test.go's structural half, and the literal gates in
// internal/standards): the AST is the evidence.
//
// What the placement must hold, and why each clause is a real defect if lost:
//
//	(a) the stamp is called in runTaskRunOnce — the ONE spawn funnel every
//	    path shares (worker round, manual `task run`, serve tick). Called
//	    anywhere else it misses the claim it is supposed to record.
//	(b) EXACTLY one call: two call sites = two writers of the same blob from
//	    one claim, which is the double-writer the briefing forbids.
//	(c) the argument is childDir, the directory the executor child ACTUALLY
//	    starts in — after the foreign-worktree escape. Stamping execWorkdir
//	    instead binds the row to a foreign task's linked worktree (the
//	    measured 01a09250 hijack).
//	(d) the call sits AFTER `childDir := executorChildDir(...)`: before that
//	    assignment the identifier does not even hold the escaped answer.
//	    Note (d) is ALSO why the hermetic escape cannot cover this — the escape
//	    returns earlier in the same function, which is exactly the residual
//	    this half closes.
// ---------------------------------------------------------------------------

// claimGroundCallSite reports the line of runTaskRunOnce's single
// taskClaimGroundStamp call and the line of the childDir resolution, or an
// error naming what is wrong. It is a pure function over SOURCE TEXT so the
// red-proof below can run it on synthetic probes.
func claimGroundCallSite(src string) (stampLine, childDirLine int, arg string, err error) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, "cli_tasks.go", src, 0)
	if perr != nil {
		return 0, 0, "", perr
	}
	var inRun bool
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "runTaskRunOnce" || fn.Body == nil {
			continue
		}
		inRun = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			// The stamp call.
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == claimGroundFunc {
					stampLine = fset.Position(call.Pos()).Line
					if len(call.Args) == 2 {
						if a, ok := call.Args[1].(*ast.Ident); ok {
							arg = a.Name
						}
					}
				}
			}
			// the childDir resolution: `childDir := executorChildDir(...)`
			if as, ok := n.(*ast.AssignStmt); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 {
				id, okID := as.Lhs[0].(*ast.Ident)
				ce, okCall := as.Rhs[0].(*ast.CallExpr)
				if okID && okCall && id.Name == claimGroundChildDirVar {
					if inner, ok := ce.Fun.(*ast.Ident); ok && inner.Name == childDirResolverFunc {
						childDirLine = fset.Position(as.Pos()).Line
					}
				}
			}
			return true
		})
	}
	if !inRun {
		return 0, 0, "", fmt.Errorf("runTaskRunOnce not found in cli_tasks.go")
	}
	return stampLine, childDirLine, arg, nil
}

const (
	claimGroundFunc        = "taskClaimGroundStamp"
	claimGroundChildDirVar = "childDir"
	childDirResolverFunc   = "executorChildDir"
	claimGroundSourceFile  = "cli_tasks.go"
)

// TestClaimGroundStampCallSiteIsTheSpawnFunnel pins the wrapper call-site
// structurally (see the block comment above for why this cannot be observed
// in-process) AND carries its own red-proof: the same predicate is run against
// synthetic probes that each break one clause, and every probe must be
// rejected. Without that half the assertions could pass on a source file the
// predicate no longer understands.
func TestClaimGroundStampCallSiteIsTheSpawnFunnel(t *testing.T) {
	src, rerr := os.ReadFile(claimGroundSourceFile)
	if rerr != nil {
		t.Fatalf("read %s: %v", claimGroundSourceFile, rerr)
	}
	stampLine, childDirLine, arg, err := claimGroundCallSite(string(src))
	if err != nil {
		t.Fatalf("call-site analysis: %v", err)
	}
	if stampLine == 0 {
		t.Fatalf("runTaskRunOnce does not call %s — the claim-time stamp is gone (the row names no ground while it runs)", claimGroundFunc)
	}
	if n := strings.Count(string(src), claimGroundFunc+"("); n != 1 {
		// The call lives in THIS file; the function itself is declared in
		// task_claim_ground.go. Two hits here = a second call site, i.e. two
		// writers of one blob from a single claim.
		t.Fatalf("%s must be called exactly ONCE in %s (two call sites = two writers of one blob), found %d occurrences", claimGroundFunc, claimGroundSourceFile, n)
	}
	if childDirLine == 0 {
		t.Fatalf("%s := %s(...) not found in runTaskRunOnce — the escape seam moved", claimGroundChildDirVar, childDirResolverFunc)
	}
	if arg != claimGroundChildDirVar {
		t.Fatalf("the stamp must be passed %s (the directory the child actually starts in), got %q — passing execWorkdir binds the row to a foreign task's worktree (the 01a09250 hijack)", claimGroundChildDirVar, arg)
	}
	if stampLine < childDirLine {
		t.Fatalf("the stamp (line %d) must run AFTER %s := %s(...) (line %d): before it, the escaped answer does not exist yet", stampLine, claimGroundChildDirVar, childDirResolverFunc, childDirLine)
	}

	// RED PROOFS: each probe breaks one clause; all must be rejected.
	good := "package main\n\nfunc runTaskRunOnce() error {\n\tchildDir := executorChildDir(execWorkdir, target, launchWorkdir)\n\ttaskClaimGroundStamp(target, childDir)\n\treturn nil\n}\n"
	probes := map[string]string{
		"no call at all":           "package main\n\nfunc runTaskRunOnce() error {\n\tchildDir := executorChildDir(execWorkdir, target, launchWorkdir)\n\treturn nil\n}\n",
		"stamps execWorkdir":       "package main\n\nfunc runTaskRunOnce() error {\n\tchildDir := executorChildDir(execWorkdir, target, launchWorkdir)\n\ttaskClaimGroundStamp(target, execWorkdir)\n\treturn nil\n}\n",
		"before the resolution":    "package main\n\nfunc runTaskRunOnce() error {\n\ttaskClaimGroundStamp(target, childDir)\n\tchildDir := executorChildDir(execWorkdir, target, launchWorkdir)\n\treturn nil\n}\n",
		"only in another function": "package main\n\nfunc runTaskRunOnce() error {\n\tchildDir := executorChildDir(execWorkdir, target, launchWorkdir)\n\treturn nil\n}\n\nfunc other() { taskClaimGroundStamp(target, childDir) }\n",
	}
	if s, _, a, err := claimGroundCallSite(good); err != nil || s == 0 || a != claimGroundChildDirVar {
		t.Fatalf("predicate sanity: the compliant probe must be accepted, got line=%d arg=%q err=%v", s, a, err)
	}
	for name, probe := range probes {
		s, c, a, err := claimGroundCallSite(probe)
		switch {
		case err != nil:
			continue // also a rejection
		case s == 0:
			continue // no call found = rejected
		case a != claimGroundChildDirVar:
			continue // wrong argument = rejected
		case c != 0 && s < c:
			continue // wrong order = rejected
		default:
			t.Fatalf("RED PROOF FAILED (%s): the predicate accepted a probe that violates a clause (stamp=%d childDir=%d arg=%q) — the assertions above prove nothing", name, s, c, a)
		}
	}
}
