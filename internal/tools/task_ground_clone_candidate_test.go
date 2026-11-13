package tools

// The advisory ground line's CLONE-CANDIDATE arm (task_ground_clone_candidate.go).
//
// THE MEASURED DEFECT (2026-10-28, 8 rows): a clone-channel run executes neither
// `git worktree add -b` nor `git checkout -b` — its clone arrives already
// checked out on the row's branch — so the SESSION arm of the reading has no
// command to parse, reconstructs "", and the line rendered
//
//	ground (resolved now): repo=… worktree=(none) branch=(none) — the row's session "…" names no worktree-add and no in-place branch creation
//
// while `refs/heads/task/<id>` was live in the ground directory on disk. These
// two tests pin BOTH directions of the fix and nothing else:
//
//	(1) candidate present  ⇒ `branch=task/<id> (exists)`, the clone answers the
//	    WORKTREE axis too, and the why NAMES THE DIRECTORY READ;
//	(2) no candidate       ⇒ the pre-fix `(none)` reading and its why are
//	    unchanged (Rule 15: an arm that does not pan out may not rewrite the
//	    honest negative, and may not silently claim it).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/toolnames"
)

// advisoryCloneNoReadingClause is the RENDERED half of the session reading's own
// why (taskGroundWhyNoReadingFmt with its %q filled) — the words that must
// survive the candidate arm unchanged. It is written out rather than sliced off
// the format string: a prefix of `…%q…` is not the sentence a reader sees.
const advisoryCloneNoReadingClause = "names no worktree-add and no in-place branch creation"

// advisoryCloneRepoBranch is the branch of the fixture's MAIN checkout's linked
// worktree. It is deliberately NOT master: newClaimCandFixture creates its
// worktree with `-b <branch>`, and `-b master` collides with the fixture's own
// initial branch (measured: "fatal: a branch named 'master' already exists").
const advisoryCloneRepoBranch = "feat/advisory-clone-repo"

// advisoryCloneGround is the clone channel's workspace shape on disk: a
// directory under the workspace root named by the FULL task id, whose `.git` is
// a DIRECTORY (the ONE discriminator, devworkspace.GroundModelOf ⇒ clone), and
// which stands on the row's own branch name.
type advisoryCloneGround struct {
	t      *testing.T
	root   string
	dir    string
	branch string
}

// newAdvisoryCloneGround builds that ground for taskID under root.
func newAdvisoryCloneGround(t *testing.T, root, taskID string) *advisoryCloneGround {
	t.Helper()
	branch := TaskGroundBranchName(taskID)
	if branch == "" {
		t.Fatalf("fixture precondition: task %q must have a ground branch name", taskID)
	}
	dir := devworkspace.Dir(root, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	deliverableWTGit(t, dir, "init", "-q")
	deliverableWTGit(t, dir, "config", "user.email", "t@t")
	deliverableWTGit(t, dir, "config", "user.name", "t")
	deliverableWTGit(t, dir, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("clone ground"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliverableWTGit(t, dir, "add", "a.txt")
	deliverableWTGit(t, dir, "commit", "-q", "-m", "clone ground work")
	if got := devworkspace.GroundModelOf(dir); got != devworkspace.GroundModelClone {
		t.Fatalf("fixture shipped the wrong ground model at %s: %q (want %q)", dir, got, devworkspace.GroundModelClone)
	}
	return &advisoryCloneGround{t: t, root: root, dir: dir, branch: branch}
}

// advisoryCloneSession writes the row's run session with a LAUNCH DIRECTORY (so
// the session reading gets past its first gates) and a tool_use line that
// creates NO ground — the exact evidence shape a clone-channel run leaves.
func advisoryCloneSession(t *testing.T, repo, sessionID string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.GopiDirEnv, home)
	sessDir := filepath.Join(home, ".gopi", config.SessionsDirName)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"session_start","sessionId":"` + sessionID + `","model":"m","timestamp":"x","workdir":"` + repo + `"}` + "\n" +
		jobGroundToolUseLine(toolnames.ToolBash, "", "cd "+repo+" && "+claimCandNoGroundCommand) + "\n"
	if err := os.WriteFile(filepath.Join(sessDir, sessionID+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTaskShowGroundAdvisoryFallsBackToTheCloneGround is direction (1): the
// session names no branch, the candidate ground on disk holds the row's ref, and
// the advisory answers BOTH axes from it — naming the directory it read.
func TestTaskShowGroundAdvisoryFallsBackToTheCloneGround(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	const taskID = "01a17df2-1d64-7624-b08e-f6ba187631b2"
	f := newClaimCandFixture(t, advisoryCloneRepoBranch)
	advisoryCloneSession(t, f.repo, "sess-clone-candidate")
	root := t.TempDir()
	t.Setenv(config.WorkspaceRootEnvName, root)
	ground := newAdvisoryCloneGround(t, root, taskID)
	advisorySeedRow(t, taskID, `{"repo":"`+f.repo+`"}`, "sess-clone-candidate")

	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow must never fail because of the advisory line: %v", err)
	}
	line := advisoryGroundLine(t, out)
	// THE BRANCH AXIS, with the live-verification marker.
	if !strings.Contains(line, taskGroundKeyBranch+"="+ground.branch+" "+TaskShowGroundExistsMarker) {
		t.Errorf("the branch axis must be answered from the candidate ground (%q with %q), got:\n%s",
			ground.branch, TaskShowGroundExistsMarker, line)
	}
	// THE WORKTREE AXIS: a CLONE is a directory of its own, so the candidate IS
	// the ground. A linked worktree would answer this through the injected
	// resolver instead, and this arm must not decide it a second way.
	if !strings.Contains(line, taskGroundKeyWorktree+"="+ground.dir+" "+TaskShowGroundExistsMarker) {
		t.Errorf("a clone candidate must also answer the worktree axis with %q, got:\n%s", ground.dir, line)
	}
	// RULE 15 half one: a hit SAYS WHERE IT READ.
	if !strings.Contains(line, ground.dir) {
		t.Errorf("the why must name the directory that was read (%q), got:\n%s", ground.dir, line)
	}
	// THE DELIVERY HALF, stated honestly: this fixture's clone ground is its own
	// repository, so its commit is in NO other tree — the measured 2-of-8 shape.
	// The reading must say so beside the live ref, not instead of it.
	if !strings.Contains(line, "NOT in the recorded repo") {
		t.Errorf("a tip absent from the recorded repo must be stated (branch=%q is live, the commit is not delivered), got:\n%s", ground.branch, line)
	}
	// The explicit absence marker must be gone from the branch axis — the whole
	// defect was `branch=(none)` beside a live ref.
	if strings.Contains(line, taskGroundKeyBranch+"="+taskGroundKeyAbsent) {
		t.Errorf("the branch axis still renders the absence marker:\n%s", line)
	}
	// RULE 37/READ-ONLY: the recorded deliverable stays exactly as recorded.
	if !strings.Contains(out, TaskShowDeliverableRepoLabel+" "+f.repo) {
		t.Errorf("the RECORDED repo line must still render:\n%s", out)
	}
	if strings.Contains(out, TaskShowDeliverableWorktreeLabel) {
		t.Errorf("the advisory line must NOT write the recorded deliverable:\n%s", out)
	}
}

// TestTaskShowGroundAdvisoryWithoutACloneGroundKeepsTheHonestAbsence is direction
// (2): the workspace root holds no ground for this row, so NOTHING is claimed.
// The pre-fix `branch=(none)` reading and the session's own why survive, and the
// arm states what it looked for (Rule 15: an arm that does not pan out leaves the
// existing why intact and never dresses the absence up as a resolution).
func TestTaskShowGroundAdvisoryWithoutACloneGroundKeepsTheHonestAbsence(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	const taskID = "01a17df3-0000-7000-8000-00000000none"
	f := newClaimCandFixture(t, advisoryCloneRepoBranch)
	advisoryCloneSession(t, f.repo, "sess-clone-absent")
	root := t.TempDir()
	t.Setenv(config.WorkspaceRootEnvName, root)
	advisorySeedRow(t, taskID, `{"repo":"`+f.repo+`"}`, "sess-clone-absent")

	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow must never fail because of the advisory line: %v", err)
	}
	line := advisoryGroundLine(t, out)
	if !strings.Contains(line, taskGroundKeyBranch+"="+taskGroundKeyAbsent) {
		t.Errorf("with no candidate ground the branch axis must stay the honest absence marker, got:\n%s", line)
	}
	if !strings.Contains(line, advisoryCloneNoReadingClause) {
		t.Errorf("the session reading's own why must survive the arm, got:\n%s", line)
	}
	// The negative names WHAT was looked for — the row's own ground directory —
	// so it is distinguishable from "nothing was asked".
	if !strings.Contains(line, devworkspace.Dir(root, taskID)) {
		t.Errorf("the negative must name the candidate directory it asked (%q), got:\n%s", devworkspace.Dir(root, taskID), line)
	}
}
