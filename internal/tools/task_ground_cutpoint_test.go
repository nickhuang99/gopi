package tools

// task_ground_cutpoint_test.go — the cut-point clause's regression guard (epic
// 01a12d66, measured 2026-10-12).
//
// THE DEFECT THESE TESTS PIN. A task briefing's acceptance criterion is often
// phrased against the INTEGRATION BRANCH («`git diff --stat master...HEAD --
// internal/` 为空»). A ground is cut from the PEER's integration-branch tip while
// the LOCAL refs/heads/master lags it — so that three-dot diff reports the
// population the ground INHERITED from the peer and is non-empty for a run that
// changed nothing. Measured twice: row 01a12d24 (11 files / 1654 insertions on a
// docs-only run) and this epic's own ground (19 files / 3952 insertions before
// its first commit).
//
// WHAT IS ASSERTED, and why each half is needed:
//
//  ① THE BASELINE IS THE RIGHT COMMIT — on a real repository shaped exactly like
//     a ground (a peer-tip branch cut behind the LOCAL master, by the same
//     command the ground uses: `git worktree add -b`), the clause names the
//     commit the branch was cut at, and the command it renders really does print
//     nothing for a branch that changed no file under the path — the epic's
//     acceptance ("docs-only ⇒ empty diff at the cut point"). The FALSE spelling
//     is asserted NON-empty on the SAME tree in the SAME test: a test that only
//     showed the corrected form empty would pass on a tree where both were.
//
//  ② AN UNREADABLE ORIGIN IS STATED, NEVER SUBSTITUTED — a branch whose reflog
//     cannot be read renders the unmeasured clause, and that clause must offer NO
//     baseline command (Rule 15: a record that cannot name its subject says so;
//     the integration branch is the exact substitution that produced this epic).
//
//  ③ THE RECORD IS NOT A PASS — the second return value carries the ONE git
//     invocation and its exit status, and the unmeasured arm's verdict says
//     UNMEASURED rather than an empty cut point.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// cutPointFixture is the repository the tests measure against.
type cutPointFixture struct {
	repo string // the main checkout (what a ground is a worktree OF)
	cut  string // the commit the ground branch was cut at (the peer's tip)
	dir  string // the ground worktree
	br   string // the ground branch
}

// cutPointTestRepo builds a real repository shaped like a host: the LOCAL
// integration branch sits BEHIND the commit a ground is cut from, which is what
// makes `master...HEAD` non-empty for a run that changed nothing.
//
// The shape, and why each step is there:
//   - `master` is the local integration branch and stops early (that is the
//     measured host condition: refs/heads/master lags by 55/83 commits).
//   - `peer` carries the commits the ground INHERITS (other rows' merged work,
//     under internal/ so the criterion's path filter sees them).
//   - the ground branch is created by the GROUND'S OWN creation command,
//     `git worktree add -b <branch> <dir> <peer tip>` — the same call
//     taskGroundCreateFrom makes, and the one that writes the reflog entry the
//     cut-point read depends on.
func cutPointTestRepo(t *testing.T) cutPointFixture {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(t.TempDir(), "ground")
	br := "task/cutpoint-fixture"
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=cutpoint", "GIT_AUTHOR_EMAIL=cutpoint@test",
			"GIT_COMMITTER_NAME=cutpoint", "GIT_COMMITTER_EMAIL=cutpoint@test",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	run("init", "-q", "-b", IntegrationBranchName)
	write("README.md", "base\n")
	run("add", "README.md")
	run("commit", "-q", "-m", "base")
	// The peer's tip carries the INHERITED population: two commits under
	// internal/, authored by nobody in this run.
	run("checkout", "-q", "-b", "peer")
	for _, c := range []struct{ path, body string }{
		{"internal/other/one.go", "package other\n"},
		{"internal/other/two.go", "package other\n\n// two\n"},
	} {
		write(c.path, c.body)
		run("add", c.path)
		run("commit", "-q", "-m", "inherited: "+c.path)
	}
	cut := run("rev-parse", "HEAD")
	// The ground: a linked worktree on a new branch, cut from the PEER's tip —
	// leaving the LOCAL integration branch behind it, exactly as on a client
	// host.
	run("worktree", "add", "-q", "-b", br, dir, cut)
	run("checkout", "-q", IntegrationBranchName)
	return cutPointFixture{repo: repo, cut: cut, dir: dir, br: br}
}

// TestGroundCutPointClauseNamesTheBaselineTheRunIsMeasuredAgainst is ① above.
func TestGroundCutPointClauseNamesTheBaselineTheRunIsMeasuredAgainst(t *testing.T) {
	f := cutPointTestRepo(t)
	clause, record := TaskGroundCutPointClause(f.dir, f.br)
	if clause == "" {
		t.Fatalf("a ground with a branch must render a cut-point clause")
	}
	if !strings.Contains(clause, GroundCutPointLabel) {
		t.Errorf("the clause must carry its label %q so a reader can address it by name:\n%s", GroundCutPointLabel, clause)
	}
	if !strings.Contains(clause, f.br) {
		t.Errorf("the clause must name the branch it speaks about (%q):\n%s", f.br, clause)
	}
	if !strings.Contains(clause, shortSha(f.cut)) {
		t.Errorf("the clause must name the commit the branch was CUT at (%s), not the local integration branch's:\n%s", shortSha(f.cut), clause)
	}
	// The corrected spellings, runnable as rendered.
	for _, want := range []string{
		shortSha(f.cut) + "..HEAD",
		"git diff --stat -- <path>",
		"git show --stat",
	} {
		if !strings.Contains(clause, want) {
			t.Errorf("the clause must render the runnable baseline %q — a correction a run cannot execute is a second document:\n%s", want, clause)
		}
	}
	// The FALSE spelling is named as the thing to re-base, so the run meets its
	// own criterion already flagged.
	mismatch := fmt.Sprintf(groundCutPointMismatchFmt, IntegrationBranchName)
	if !strings.Contains(clause, mismatch) {
		t.Errorf("the clause must quote the criterion spelling it defuses (%q):\n%s", mismatch, clause)
	}

	// THE MEASUREMENT, not the argument: on this tree the corrected spelling is
	// EMPTY and the criterion's own spelling is NON-EMPTY. Both readings are
	// taken here, on one repository, so neither can be a coincidence of a tree
	// where both are empty.
	if got := strings.TrimSpace(cutPointGit(t, f.dir, "diff", "--stat", f.cut+"..HEAD", "--", "internal/")); got != "" {
		t.Errorf("the cut-point baseline must be EMPTY on a ground whose run committed nothing; got:\n%s", got)
	}
	wrong := strings.TrimSpace(cutPointGit(t, f.dir, "diff", "--stat", IntegrationBranchName+"...HEAD", "--", "internal/"))
	if wrong == "" {
		t.Fatalf("FIXTURE: this repository must reproduce the defect (a non-empty `%s...HEAD` on a run that changed nothing), or the test proves nothing", IntegrationBranchName)
	}
	t.Logf("measured on the fixture: `%s` = %q ; `%s..HEAD` = (empty)", mismatch, cutPointSummary(wrong), shortSha(f.cut))

	if !strings.Contains(record, shortSha(f.cut)) || !strings.Contains(record, "reflog show") {
		t.Errorf("the record must carry the measurement (the cut commit and the invocation); got:\n%s", record)
	}
}

// TestGroundCutPointClauseNamesNoBaselineWhenTheOriginIsUnreadable is ② above: an
// origin that cannot be read is STATED, and no baseline is offered in its place.
func TestGroundCutPointClauseNamesNoBaselineWhenTheOriginIsUnreadable(t *testing.T) {
	// A directory that is not a repository at all: git fails, so nothing about
	// this branch's origin was measured (Rule 15 — an unread origin is NOT an
	// empty cut point).
	clause, record := TaskGroundCutPointClause(t.TempDir(), "task/nonexistent")
	if clause == "" {
		t.Fatalf("an unreadable origin must still render a clause — the run must be told what was NOT measured")
	}
	if strings.Contains(clause, "Your branch was cut at") {
		t.Errorf("the unmeasured clause must render NO baseline — a base it could not read would be an invented number:\n%s", clause)
	}
	if !strings.Contains(clause, "could NOT be read") {
		t.Errorf("the unmeasured clause must say the cut point was not read:\n%s", clause)
	}
	if !strings.HasPrefix(clause, GroundCutPointLabel) {
		t.Errorf("the unmeasured clause must still carry its label:\n%s", clause)
	}
	if !strings.Contains(record, "UNMEASURED") {
		t.Errorf("the record must never let an unread origin print as an empty cut point; got:\n%s", record)
	}

	// A row with no branch renders NOTHING: there is no subject to name.
	if c, r := TaskGroundCutPointClause("/repo", "  "); c != "" || r != "" {
		t.Errorf("no branch ⇒ no clause and no record, got clause=%q record=%q", c, r)
	}

	// A branch that exists but has NO reflog entry is the same class: stated,
	// never read as "no changes". (git's reflog is per-ref; with
	// core.logAllRefUpdates=false a branch created after that setting has none.)
	repo := t.TempDir()
	cutPointGitEnv(t, repo, "init", "-q", "-b", IntegrationBranchName)
	cutPointGitEnv(t, repo, "config", "core.logAllRefUpdates", "false")
	cutPointGitEnv(t, repo, "checkout", "-q", "-b", cutPointNoReflogBranch)
	read := GroundCutPoint(repo, cutPointNoReflogBranch)
	if read.Measured() {
		t.Fatalf("a branch with no reflog entry must NOT read as a measured cut point; got sha=%q", read.SHA)
	}
	if read.Note == "" || strings.TrimSpace(read.Evidence) == "" {
		t.Errorf("an unreadable origin must be STATED with its evidence; got note=%q evidence=%q", read.Note, read.Evidence)
	}
}

// cutPointNoReflogBranch is the branch the fixture creates AFTER turning ref
// reflogs off — a ref git can resolve but whose origin is unrecorded.
const cutPointNoReflogBranch = "no-reflog-fixture"

// cutPointGit runs ONE git query in dir and returns its stdout.
func cutPointGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return cutPointGitEnv(t, dir, args...)
}

// cutPointGitEnv is cutPointGit with the fixture's identity/config isolation.
func cutPointGitEnv(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=cutpoint", "GIT_AUTHOR_EMAIL=cutpoint@test",
		"GIT_COMMITTER_NAME=cutpoint", "GIT_COMMITTER_EMAIL=cutpoint@test",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return string(out)
}

// cutPointSummary keeps a `--stat` body's last line for a readable message.
func cutPointSummary(stat string) string {
	lines := strings.Split(strings.TrimSpace(stat), "\n")
	return lines[len(lines)-1]
}
