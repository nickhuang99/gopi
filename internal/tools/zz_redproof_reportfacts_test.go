package tools

// zz_redproof_reportfacts_test.go — the A/B red proof for the harness
// skeleton's facts, written against the PRE-FIX API ONLY (SealTaskReport + the
// row's deliverable stamping helpers as they already existed; nothing this
// change adds), so the very same file compiles and runs on both trees:
//
//	pre-fix tree: FAILS — the skeleton resolved branch/commit from the child
//	  session ENV alone, so a row whose LEDGER deliverable names the branch and
//	  whose branch ref is reachable in the shared repo still printed
//	  `branch: (unknown)` / `commit: (unknown)`. Measured on row
//	  01a12de6-c052-7346-9efc-bbd885387489 (deliverable.branch
//	  task/01a12de6c05273469efcbbd885387489 @ b8c566ea2, skeleton "(unknown)").
//	post-fix tree: PASSES — the same row's skeleton states the branch the
//	  ledger records and the tip of that ref.
//
// Reproduce:
//
//	git worktree add --detach <scratch> master
//	cp internal/tools/zz_redproof_reportfacts_test.go <scratch>/internal/tools/
//	cd <scratch> && go test -run TestZZRedProofSkeletonFactsAgreeWithTheLedger ./internal/tools/
//
// The asserted prefixes are LITERALS here on purpose: this file must compile
// against the pre-fix tree, where no constant for the fact line exists.
// Production code never uses the literal — it renders through
// taskreport.FormatFact (Rules 1b/13).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskreport"
)

// zzRedProofBranch is the branch the fixture ground is created on.
const zzRedProofBranch = "feat/redproof-report-facts"

// zzRedProofGit runs one git command in dir and fails the test on error — a
// missing git must never read as a pass.
func zzRedProofGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=redproof", "GIT_AUTHOR_EMAIL=redproof@gopi",
		"GIT_COMMITTER_NAME=redproof", "GIT_COMMITTER_EMAIL=redproof@gopi")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// zzRedProofGround creates a repo with one commit on zzRedProofBranch checked
// out in a linked worktree, returning (repo, worktree, short tip).
func zzRedProofGround(t *testing.T) (repo, wt, tip string) {
	t.Helper()
	repo = t.TempDir()
	zzRedProofGit(t, repo, "init", "-q", "-b", "master")
	zzRedProofGit(t, repo, "commit", "-q", "--allow-empty", "-m", "root")
	wt = filepath.Join(t.TempDir(), "wt")
	zzRedProofGit(t, repo, "worktree", "add", "-q", "-b", zzRedProofBranch, wt, "master")
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("delivered\n"), 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}
	zzRedProofGit(t, wt, "add", "work.txt")
	zzRedProofGit(t, wt, "commit", "-q", "-m", "feat: the delivery")
	return repo, wt, zzRedProofGit(t, wt, "rev-parse", "--short", "HEAD")
}

// TestZZRedProofSkeletonFactsAgreeWithTheLedger is the motivating shape: the
// run delivered on a branch the LEDGER records, while no child-session env
// reached the close-out (the wrapper resolves it from a session JSONL that a
// pruned sessions tree no longer holds). The harness's own fallback report must
// still agree with the harness's own ledger.
func TestZZRedProofSkeletonFactsAgreeWithTheLedger(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	// The env channel is EMPTY — the shape the defect was measured in.
	t.Setenv(TaskChildSessionEnv, "")
	t.Setenv(TaskExecutorSessionEnv, "")

	repo, wt, tip := zzRedProofGround(t)

	const taskID = "t-zz-redproof-report-facts"
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "skeleton facts agree with the ledger", Repo: repo, TaskID: taskID})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	if got := adoptionTaskID(t, out); got != taskID {
		t.Fatalf("submitted row id = %q, want %q", got, taskID)
	}
	// The row's RECORDED delivery, exactly as a real claim/completion stamps
	// it (both helpers predate this change).
	if err := taskBackfillDeliverableWorktree(taskID, wt, zzRedProofBranch); err != nil {
		t.Fatalf("stamp the recorded ground: %v", err)
	}

	line, err := SealTaskReport(taskID)
	if err != nil {
		t.Fatalf("SealTaskReport: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(home, ".gopi", "sessions", taskID, taskreport.FileName))
	if err != nil {
		t.Fatalf("read skeleton: %v", err)
	}
	got := string(body)

	if !strings.Contains(got, "- branch: "+zzRedProofBranch) {
		t.Errorf("UNRECORDED FACT: the ledger records branch %q on this row and the harness's own report does not state it:\n%s",
			zzRedProofBranch, got)
	}
	if !strings.Contains(got, "- commit: "+tip) {
		t.Errorf("UNRECORDED FACT: the branch ref resolves to %s in the shared repo and the report does not state it:\n%s",
			tip, got)
	}
	// The digest still names the report (the index that outlives the file).
	if !strings.Contains(line, taskreport.DigestLinePrefix) {
		t.Errorf("digest line must name the report: %q", line)
	}
}
