package tools

// The push-back ANCESTRY gate (epic: the commit path has no ancestry check).
//
// THE INCIDENT IT CLOSES (measured 2026-09-21): a scratch repository —
// `git init`, two fake files (`a.txt`, `b.txt`), one "deliverable work"
// commit — reached the real GoPi repo as `feat/sync-conflict-completed`, a
// ref whose root (39fd968a) shares NO commit with master's root (e9538e78).
// Nothing on the way in noticed: `git merge` refuses it outright
// (`fatal: refusing to merge unrelated histories`, exit 128), so the branch
// could never be landed by anyone, and the merge-backlog generator re-filed
// the junk four times (rows 01a0c201, 01a0c2e1, 01a0c2e7, 01a0c55c).
//
// Every other gate in this lane is built on the assumption that a branch
// DESCENDS from the trunk: `merge-base --is-ancestor <branch> <mirror>`
// (already-integrated? no) and `merge-base --is-ancestor <mirror> <branch>`
// (fast-forwardable? no) both return "no" for an orphan, and the second one
// used to REFUSE it as "not fast-forwardable" — true, but it names a symptom
// and tells the operator to merge master into a history that has no master
// to merge (a remedy that cannot be performed). The ancestry probe below is a
// third, PRIOR question: do these two refs have ANY commit in common.
//
// These tests run against REAL git: an orphan ref is a git-semantics object
// (a shared root is exactly what `merge-base` measures), so a mock cannot
// prove the refusal.
//
// Run this file's tests:  go test -run TestPushbackAncestry ./internal/tools/

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pbGitMaybe is pbRun's NON-FATAL twin: it reports git's trimmed stdout and
// its exit code so a test can assert on a MEASUREMENT (an ancestry probe's
// exit status), rather than on a call that must not fail. Same deterministic
// identity env as pbRun (a commit must not depend on the host's git config).
func pbGitMaybe(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		code = -1
	}
	return strings.TrimSpace(string(out)), code
}

// pbOrphanBranchNode mints the incident's own shape INSIDE the test: a
// brand-new repository (`git init`) with two fake files and one commit —
// then fetches its master tip into `branch` of the client repo, which is
// exactly how the junk ref arrived (a push from a scratch repo; a fetch
// writes the same refs/heads entry without needing a live ssh peer).
//
// Returns the orphan tip sha so a caller can assert on it.
func pbOrphanBranchNode(t *testing.T, w *pbWorld, branch string) string {
	t.Helper()
	scratch := filepath.Join(w.root, "scratch-"+strings.ReplaceAll(branch, "/", "-"))
	w.git(w.root, "init", "-q", "-b", "master", scratch)
	w.git(scratch, "config", "user.email", "t@t")
	w.git(scratch, "config", "user.name", "t")
	pbWrite(t, scratch, "a.txt", "fake")
	w.git(scratch, "add", "a.txt")
	w.git(scratch, "commit", "-qm", "init")
	pbWrite(t, scratch, "b.txt", "deliverable")
	w.git(scratch, "add", "b.txt")
	w.git(scratch, "commit", "-qm", "deliverable work")
	tip := w.git(scratch, "rev-parse", "HEAD")
	w.git(w.client, "fetch", "-q", scratch, "master:"+branch)
	return tip
}

// TestPushbackAncestryRefusesOrphanAndPassesDescendant is the red-proof case:
// an unrelated-history branch is REFUSED with its own reason (never pushed),
// and an ordinary master-descended branch in the SAME flush still goes
// through — so the gate cannot be satisfied by refusing everything.
func TestPushbackAncestryRefusesOrphanAndPassesDescendant(t *testing.T) {
	w := newPBWorld(t)
	c := w.client

	// The junk ref, in the incident's shape.
	orphanTip := pbOrphanBranchNode(t, w, "feat/orphan")

	// The control: a normal branch cut from the trunk and containing the
	// peer's master tip — the shape this lane exists to deliver.
	w.git(c, "checkout", "-q", "-b", "feat/legit", w.seedCommitA)
	pbWrite(t, c, "legit.txt", "work")
	w.git(c, "add", "legit.txt")
	w.git(c, "commit", "-qm", "legit work")
	w.git(c, "checkout", "-q", "master")

	// Precondition, measured (not assumed): the two refs really share no root.
	// (Frozen here as the documented `merge-base --all` predicate's basis —
	// exit 1 = "no common ancestor".)
	roots, code := pbGitMaybe(t, c, "rev-list", "--max-parents=0", "master", "feat/orphan")
	if code != 0 {
		t.Fatalf("fixture: rev-list roots exit=%d: %s", code, roots)
	}
	if n := len(strings.Fields(roots)); n != 2 {
		t.Fatalf("fixture: master and feat/orphan must have two distinct roots, got %d: %s", n, roots)
	}
	if out, _ := pbGitMaybe(t, c, "merge-base", "--all", "master", "feat/orphan"); out != "" {
		t.Fatalf("fixture: master and feat/orphan already share %q — not an orphan", out)
	}

	flush := gitPushbackPlan(context.Background(), c, w.server)

	// ① the orphan is refused, BY NAME, with the ancestry reason.
	var refusal string
	for _, f := range flush.Failed {
		if strings.HasPrefix(f, "feat/orphan:") {
			refusal = f
		}
	}
	if refusal == "" {
		t.Fatalf("an unrelated-history branch was NOT refused: failed=%v pushed=%v", flush.Failed, flush.Pushed)
	}
	if !strings.Contains(refusal, "no common ancestor") {
		t.Errorf("refusal does not state the ancestry reason: %q", refusal)
	}
	// The refusal must name the trunk AND route the discard through the ONE
	// archive writer: the old text told the operator to run the raw deletion,
	// which is a removal that leaves no audit record (the reason the removal
	// census gained its shell half — see devdoc/removal-audit-shell-census.md).
	for _, want := range []string{masterBranchName, "gopi task archive feat/orphan --reason " + ArchiveReasonUnrelatedHistory} {
		if !strings.Contains(refusal, want) {
			t.Errorf("refusal is missing %q — an operator must be told which trunk it is unrelated to and how to discard it: %q", want, refusal)
		}
	}
	// ② nothing crossed the wire: the peer's ref namespace is untouched.
	if w.serverRefExists("feat/orphan") {
		t.Fatal("the orphan branch reached the peer's refs — the refusal must PREVENT the push, not report it after")
	}
	// ③ the local branch is left exactly as it was (never deleted, never
	// rewritten by a gate that is only allowed to refuse).
	if got := w.git(c, "rev-parse", "feat/orphan"); got != orphanTip {
		t.Errorf("the refused branch's tip moved: got %s want %s", got, orphanTip)
	}
	// ④ the control passed the SAME flush — the gate is not a blanket refusal.
	if !pbContains(flush.Pushed, "feat/legit") {
		t.Errorf("a master-descended branch was not pushed: pushed=%v failed=%v", flush.Pushed, flush.Failed)
	}
	if !w.serverRefExists("feat/legit") {
		t.Error("the master-descended branch did not reach the peer")
	}
	if why := pbFailureFor(flush.Failed, "feat/legit"); why != "" {
		t.Errorf("the master-descended branch carried a refusal: %q", why)
	}
}

// TestPushbackAncestryReasonIsDistinctFromNotFastForwardable pins the
// diagnostic distinction the incident was made of: an orphan satisfies "not
// fast-forwardable onto the mirror" as well, but that reason names a REMEDY
// THAT CANNOT BE PERFORMED (`merge master into it` — there is no master in
// its history to merge). The ancestry gate runs FIRST, so the operator reads
// the true cause.
func TestPushbackAncestryReasonIsDistinctFromNotFastForwardable(t *testing.T) {
	w := newPBWorld(t)
	pbOrphanBranchNode(t, w, "feat/junk")

	flush := gitPushbackPlan(context.Background(), w.client, w.server)
	why := pbFailureFor(flush.Failed, "feat/junk")
	if why == "" {
		t.Fatalf("the orphan branch was not refused: %v", flush.Failed)
	}
	if strings.Contains(why, "not fast-forwardable") {
		t.Errorf("orphan reported as merely not-ff-able — the remedy on that line (merge %s into it) cannot be performed on an unrelated history: %q",
			masterBranchName, why)
	}
}

func pbContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func pbFailureFor(failed []string, branch string) string {
	for _, f := range failed {
		if strings.HasPrefix(f, branch+":") {
			return f
		}
	}
	return ""
}
