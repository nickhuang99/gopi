package tools

// task_submit_clone_ground_test.go — THE CLONE-GROUND ARM's regression guard
// (epic 01a160a2; measured rows 01a1604a, 01a15b96, 01a16053).
//
// The four readings, and why each is independent:
//
//	① the ARM ITSELF: a clone-channel workspace resolves to the repository it was
//	   cloned from, and the caller is told so in one line — measured after
//	   proving the OLD resolution answers identity for that same directory
//	   (mergeExecMainRepo), which is why every row born there was doomed;
//	② the SUBMIT SEAM: a task_submit whose repo IS the workspace lands on the
//	   ledger naming the SOURCE repository, and the confirmation carries the note
//	   (the model must not be told "queued" and learn the truth never);
//	③ the REFUSAL: a workspace whose origin cannot be named as a live git work
//	   tree is REFUSED at the submit — never silently stamped with the directory
//	   that is about to be deleted (that fallback IS the defect);
//	④ the BOUNDARY: a repository outside the workspace root is IDENTITY with no
//	   note, and a LINKED worktree under the root keeps its own answer (the main
//	   checkout it is a guest of) — this arm adds a case, it does not replace the
//	   rules that already existed.
//
// The fixtures are REAL repositories (git init + commit), and the workspace root
// is pinned through its ONE reader (config.WorkspaceRootEnvName), so nothing here
// can pass by reading the operator's settings file.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// cloneGroundFixture builds a SOURCE repository and a workspace root, clones the
// source into <root>/<task-id> — the exact shape internal/devworkspace.Open
// produces — and pins the ledger + the ONE root reader.
func cloneGroundFixture(t *testing.T) (source, root, clone string) {
	t.Helper()
	source, root = repairFixture(t) // real repo with one commit + a pinned root
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	clone = filepath.Join(root, "01a16033-c069-72fe-8b4e-d15d706f41ac")
	if out, err := groundTestGit(filepath.Dir(clone), "clone", "-q", source, clone); err != nil {
		t.Fatalf("git clone %s → %s: %v\n%s", source, clone, err, out)
	}
	if SameDir(source, clone) {
		t.Fatalf("fixture degenerate: the workspace %s IS the source repository", clone)
	}
	// THE INSTRUMENT FIRST (Rule 10[4]): the OLD resolution answers IDENTITY for
	// this directory — it is a "main checkout" of its own, which is exactly why
	// the defect shipped. If this ever stops holding, the arm below no longer
	// addresses the measured shape and this test must be revisited, not deleted.
	main, err := mergeExecMainRepo(clone)
	if err != nil {
		t.Fatalf("mergeExecMainRepo(%s): %v", clone, err)
	}
	if !SameDir(main, clone) {
		t.Fatalf("fixture premise broken: the linked-worktree resolution should see a clone as its own main checkout (%s), got %s", clone, main)
	}
	return source, root, clone
}

// TestDurableRepoForSubmitResolvesACloneGroundToItsSource is ①.
func TestDurableRepoForSubmitResolvesACloneGroundToItsSource(t *testing.T) {
	source, _, clone := cloneGroundFixture(t)

	got, note, err := DurableRepoForSubmitWithNote(clone)
	if err != nil {
		t.Fatalf("DurableRepoForSubmitWithNote(%s): %v", clone, err)
	}
	if !SameDir(got, source) {
		t.Errorf("a clone-channel workspace must resolve to the repository it was cloned from (%s); got %s\n"+
			"the workspace is deleted when the run's push lands, so a row naming it is stranded forever "+
			"(measured: 01a1604a, 01a15b96, 01a16053)", source, got)
	}
	if SameDir(got, clone) {
		t.Errorf("the resolution returned the WORKSPACE itself (%s) — the defect verbatim", clone)
	}
	if strings.TrimSpace(note) == "" {
		t.Errorf("a substitution must be reported in one line (Rule 15); got an empty note")
	}
	if !strings.Contains(note, clone) || !strings.Contains(note, source) {
		t.Errorf("the note must name BOTH paths so a reader can trust the substitution; got %q", note)
	}
}

// TestTaskSubmitFromACloneGroundStampsTheSourceRepo is ② — the seam the measured
// failures actually went through.
func TestTaskSubmitFromACloneGroundStampsTheSourceRepo(t *testing.T) {
	source, _, clone := cloneGroundFixture(t)

	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: "child of a clone-channel run", Payload: "do the work",
		Repo: clone, TaskID: "t-clone-ground-source",
	})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	got := taskRowDeliverableRepo("t-clone-ground-source")
	if !SameDir(got, source) {
		t.Errorf("the row's deliverable.repo must be the SOURCE repository %s; got %q", source, got)
	}
	if SameDir(got, clone) {
		t.Errorf("the row's deliverable.repo is the run's workspace %s — the directory is deleted when the "+
			"parent's push lands, and the child is then refused at spawn with \"the directory no longer "+
			"exists on disk\" (measured three times)", clone)
	}
	if !strings.Contains(out, source) {
		t.Errorf("the submit confirmation must carry the substitution note (the model reads THIS string, not stderr); got:\n%s", out)
	}
}

// TestTaskSubmitRefusesACloneGroundWhoseOriginIsUnnameable is ③: the only other
// value available is the doomed path, so the arm refuses instead.
func TestTaskSubmitRefusesACloneGroundWhoseOriginIsUnnameable(t *testing.T) {
	_, _, clone := cloneGroundFixture(t)
	if out, err := groundTestGit(clone, "remote", "remove", "origin"); err != nil {
		t.Fatalf("git remote remove origin: %v\n%s", err, out)
	}

	_, _, err := DurableRepoForSubmitWithNote(clone)
	if err == nil {
		t.Fatalf("a workspace with no origin must be REFUSED — the alternative is stamping the path that is about to be deleted")
	}
	msg := err.Error()
	if !strings.Contains(msg, clone) {
		t.Errorf("the refusal must name the workspace it refused; got %v", err)
	}
	if !strings.Contains(msg, "gopi task update") {
		t.Errorf("the refusal must name the repair verb an operator can act on; got %v", err)
	}
	if _, serr := TaskSubmit(TaskSubmitOptions{
		Summary: "must not be queued", Payload: "nope", Repo: clone, TaskID: "t-clone-ground-refused",
	}); serr == nil {
		t.Errorf("TaskSubmit accepted a repo that can never be run — a row that never starts is worse than a refused submit")
	}
}

// TestDurableRepoForSubmitLeavesOtherReposAlone is ④: this arm adds a case, it
// does not re-decide the ones that already worked.
func TestDurableRepoForSubmitLeavesOtherReposAlone(t *testing.T) {
	source, root, _ := cloneGroundFixture(t)

	// (a) a repository OUTSIDE the workspace root, with an origin that points
	// somewhere else: identity, no note. A normal submit costs no git question.
	other := groundTestRepo(t, "outside-the-root")
	if out, err := groundTestGit(other, "remote", "add", "origin", source); err != nil {
		t.Fatalf("git remote add origin: %v\n%s", err, out)
	}
	if worktreerootsWithin(other, root) {
		t.Fatalf("fixture degenerate: %s must lie OUTSIDE the workspace root %s", other, root)
	}
	got, note, err := DurableRepoForSubmitWithNote(other)
	if err != nil {
		t.Fatalf("DurableRepoForSubmitWithNote(%s): %v", other, err)
	}
	if !SameDir(got, other) || note != "" {
		t.Errorf("a repo outside the workspace root must be IDENTITY with no note; got (%s, %q)", got, note)
	}

	// (b) a LINKED worktree under the root keeps the answer that already existed:
	// the MAIN checkout it is a guest of — its origin remote is irrelevant here.
	linked := filepath.Join(root, "linked-under-root")
	if out, err := groundTestGit(other, "worktree", "add", "-q", "-b", "feat/linked-under-root", linked, "master"); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	got, _, err = DurableRepoForSubmitWithNote(linked)
	if err != nil {
		t.Fatalf("DurableRepoForSubmitWithNote(%s): %v", linked, err)
	}
	if !SameDir(got, other) {
		t.Errorf("a LINKED worktree must keep resolving to the main checkout that registers it (%s); got %s", other, got)
	}

	// (c) a CLONE-model repository NESTED BELOW the root that is not a direct
	// child — i.e. NOT the shape the channel produces — is IDENTITY, whichever
	// origin it has. Measured defect (task 01a16218, master ebbecc7): the arm
	// accepted everything under the root, so a plain `git init` two levels down
	// was refused as "the clone channel's workspace for one run" even though no
	// run had ever created it, and `gopi task clone --repo <such a path>`
	// aborted. The workspace root is ALSO where checkouts, an operator's scratch
	// repos and the test suite's own scratch home live, so containment alone
	// cannot be the predicate — `devworkspace.Dir` makes the shape ONE level
	// deep.
	nested := filepath.Join(dirNestedUnderRoot(t, root), "override-main")
	if out, err := groundTestGit(filepath.Dir(nested), "init", "-q", nested); err != nil {
		t.Fatalf("git init %s: %v\n%s", nested, err, out)
	}
	if worktreerootsWithin(nested, root) == false {
		t.Fatalf("fixture degenerate: %s must lie UNDER the workspace root %s", nested, root)
	}
	if filepath.Dir(nested) == filepath.Clean(root) {
		t.Fatalf("fixture degenerate: %s must NOT be a direct child of the root %s", nested, root)
	}
	// It carries NO origin: if the arm fired at all, this is the REFUSAL arm —
	// which is exactly the sentence the measured failure printed.
	if st := devworkspace.ResolveGroundState(nested); st.Model != devworkspace.GroundModelClone || !st.Alive {
		t.Fatalf("fixture degenerate: %s must be a LIVE clone-model ground, got model=%s alive=%v (%s)",
			nested, st.Model, st.Alive, st.Detail)
	}
	got, note, err = DurableRepoForSubmitWithNote(nested)
	if err != nil {
		t.Errorf("a clone-model repository NESTED below the workspace root is not one of this channel's "+
			"workspaces (the channel's shape is root/<task-id>, ONE level) — it must be IDENTITY, not "+
			"refused; got error: %v", err)
	}
	if !SameDir(got, nested) || note != "" {
		t.Errorf("a repository nested below the root must be IDENTITY with no note; got (%s, %q)", got, note)
	}
}

// dirNestedUnderRoot makes a two-level directory under the workspace root (the
// channel itself only ever produces ONE level: devworkspace.Dir). It is a
// sibling of the fixture root's OWN children, never the root's, so the arm's
// shape predicate — not an accident of naming — is what the assertion below
// measures.
func dirNestedUnderRoot(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "nested-scratch-home", "queue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}

// worktreerootsWithin keeps the boundary assertion readable and anchors it on the
// SAME boundary function the arm uses (worktreeroots.Within), through the SAME
// one root reader (config.WorkspaceRootConfigured) — Rule 13/14: the test must
// not restate the rule it checks with a private spelling.
func worktreerootsWithin(path, root string) bool {
	r, _, err := config.WorkspaceRootConfigured()
	if err != nil {
		return false
	}
	if !worktreeroots.Within(root, r) || !worktreeroots.Within(path, r) {
		return false
	}
	return worktreeroots.Within(path, root)
}
