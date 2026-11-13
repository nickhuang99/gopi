package main

// task_completion_witness_seam_test.go — the CLOSE-OUT SEAM's own arm.
//
// The criterion lives in internal/tools/task_completion_witness.go and is
// covered there. What THIS file pins is the half only cmd/agent can answer:
//
//	① the write-back really calls the arm — a row closed `completed` by
//	   writebackOutcome leaves the witnessless record on its trail, and the
//	   status it was written with still stands (the arm annotates, never gates);
//	② taskRunConclusion — the clone channel's own measured conclusion — is what
//	   makes the third verdict reachable, so a nothing-to-deliver completion is
//	   NOT recorded as the defect.
//
// It drives the PRODUCTION door (writebackOutcome), not the arm directly: a
// seam that is correct in isolation and never called is exactly the defect
// Rule 31 is about.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// cwSeamGit runs one git command in dir, failing the test on error.
func cwSeamGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

// cwSeamRow submits a row and stamps a deliverable naming a branch that does not
// exist in the given repo — the witnessless shape (no ref, no trailer, no
// landed record).
func cwSeamRow(t *testing.T, dbPath, taskID, repo, branch string) {
	t.Helper()
	if _, err := tools.TaskNew("completion witness seam", taskID, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	blob, err := json.Marshal(map[string]string{
		tools.DeliverableRepoKey:     repo,
		tools.DeliverableBranchKey:   branch,
		tools.DeliverableWorktreeKey: filepath.Join(repo, ".gone-ground"),
	})
	if err != nil {
		t.Fatalf("deliverable: %v", err)
	}
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, string(blob), taskID); err != nil {
		t.Fatalf("deliverable update: %v", err)
	}
}

// cwSeamEventDetail returns the row's newest event of the given name ("" when
// none was filed).
func cwSeamEventDetail(t *testing.T, dbPath, taskID, event string) string {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	var detail string
	err = db.QueryRowContext(context.Background(),
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id DESC LIMIT 1`,
		taskID, event).Scan(&detail)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("read %s: %v", event, err)
	}
	return detail
}

// cwSeamFixture makes a real git repo whose only branch is master, so a
// deliverable naming `task/<id>` resolves nowhere.
func cwSeamFixture(t *testing.T) (repo, dbPath string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dbPath = filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	t.Setenv(tools.TaskWrapperEnv, "")
	repo = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	cwSeamGit(t, repo, "init", "-q", "-b", "master")
	cwSeamGit(t, repo, "config", "user.email", "cw@test")
	cwSeamGit(t, repo, "config", "user.name", "cw")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwSeamGit(t, repo, "add", "a.txt")
	cwSeamGit(t, repo, "commit", "-q", "-m", "base")
	return repo, dbPath
}

// TestWritebackOutcomeRecordsAWitnesslessCompletion is ①: the production door
// leaves the record, and the `completed` claim stands.
func TestWritebackOutcomeRecordsAWitnesslessCompletion(t *testing.T) {
	repo, dbPath := cwSeamFixture(t)
	const row = "01a182bf-0000-7000-8000-00000000seam1"
	branch := "task/01a182bf00007000800000000000seam1"
	cwSeamRow(t, dbPath, row, repo, branch)
	taskRunConclusion = ""

	writebackOutcome(row, tools.TaskStatusCompleted, "gopi run completed", "", "")

	st, err := tools.TaskStatusOf(row)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st != tools.TaskStatusCompleted {
		t.Fatalf("status = %q — the witness arm RECORDS and never rewrites the claim (%q)", st, tools.TaskStatusCompleted)
	}
	detail := cwSeamEventDetail(t, dbPath, row, tools.CompletionWitnesslessEvent)
	if detail == "" {
		t.Fatalf("SILENT CLAIM: %s reads %s, branch %s resolves to no ref, no reachable commit names it, and the write-back filed nothing",
			row, tools.TaskStatusCompleted, branch)
	}
	for _, want := range []string{tools.CompletionWitnesslessMarker, row, branch} {
		if !strings.Contains(detail, want) {
			t.Errorf("the record must name %q — got %q", want, detail)
		}
	}
}

// TestWritebackOutcomeConcludedCompletionIsNotTheDefect is ②: the same
// witnessless state, but THIS run completed on the clone channel's measured
// conclusion — so the seam hands the arm `concluded` and no defect record is
// filed.
func TestWritebackOutcomeConcludedCompletionIsNotTheDefect(t *testing.T) {
	repo, dbPath := cwSeamFixture(t)
	const row = "01a182bf-0000-7000-8000-00000000seam2"
	cwSeamRow(t, dbPath, row, repo, "task/01a182bf00007000800000000000seam2")
	// The latch the nothing-to-deliver exit sets in the close-out.
	taskRunConclusion = "clone channel: nothing to deliver — the ground carries no commit to deliver: `git rev-list --count HEAD --not origin/master` answered 0"
	defer func() { taskRunConclusion = "" }()

	writebackOutcome(row, tools.TaskStatusCompleted, taskRunConclusion, "", "")

	if detail := cwSeamEventDetail(t, dbPath, row, tools.CompletionWitnesslessEvent); detail != "" {
		t.Fatalf("a CONCLUDED completion must not be recorded as the defect — got %q", detail)
	}
	st, err := tools.TaskStatusOf(row)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st != tools.TaskStatusCompleted {
		t.Fatalf("status = %q, want %q", st, tools.TaskStatusCompleted)
	}
}

// TestWritebackOutcomeFailedRowIsNotJudged: the seam judges DELIVERY claims, so
// a `failed` write files nothing even when every witness is absent.
func TestWritebackOutcomeFailedRowIsNotJudged(t *testing.T) {
	repo, dbPath := cwSeamFixture(t)
	const row = "01a182bf-0000-7000-8000-00000000seam3"
	cwSeamRow(t, dbPath, row, repo, "task/01a182bf00007000800000000000seam3")
	taskRunConclusion = ""

	writebackOutcome(row, tools.TaskStatusFailed, "the run failed", "", "")

	if detail := cwSeamEventDetail(t, dbPath, row, tools.CompletionWitnesslessEvent); detail != "" {
		t.Fatalf("a failed row claims no delivery and must not be judged — got %q", detail)
	}
}
