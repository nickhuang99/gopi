package tools

// Red proof for the MAIN-CHECKOUT arm's SECOND DOOR (2026-09-26, HP main
// checkout, row 01a0dbe4-ac93-74f6-8afe-e740669c9532). This file is written
// against the PRE-FIX API ONLY — every symbol it calls (TaskSubmit / TaskStart /
// TaskRunWriteback / seedDeliverable / deadHolderReclaimFixture / gitRun /
// reclaimGitStatus / openTestTasksDB / config.GopiDirEnv) exists on master, and
// it defines its own fixture helpers rather than borrowing those of the fixed
// tree's test file, so the SAME file compiles on BOTH trees and the two runs
// differ by the fix alone:
//
//	# baseline (master, pre-fix) — copy THIS FILE ONLY; nothing else is needed
//	SCRATCH="$(bash -c 'source scripts/lib/tmp-ban.sh && gopi_scratch_mkdir maindep2-red')"
//	git worktree add --detach "$SCRATCH" master
//	cp internal/tools/zz_redproof2_main_checkout_repo_door_test.go "$SCRATCH/internal/tools/"
//	cd "$SCRATCH" && go test -count=1 -v -run 'TestZZRedProof2_' ./internal/tools/
//	--- FAIL: ... UNATTRIBUTED DEPOSIT: ... its own session recorded it as the
//	    launch directory, while its BRIEFING never names it ...
//
//	# this branch — the file is already here:
//	go test -count=1 -run 'TestZZRedProof2_' ./internal/tools/
//	--- PASS
//
//	git worktree remove --force "$SCRATCH"    # give the registration back
//
// THE PROPERTY. A run whose deliverable.repo IS the repo's main checkout, whose
// own session recorded that checkout as its launch directory, and whose BRIEFING
// is SILENT about it — the shape that actually leaked — must not be able to
// deposit untracked residue there SILENTLY: at close-out the row carries a record
// naming the checkout and the deposit. Pre-fix the arm's precondition is
// `TaskBriefingNamesDir(briefing, main)`, which is FALSE by construction for this
// shape, so the arm returns "nothing to measure" and no arm on master measures the
// shared checkout for a row whose briefing is silent.
//
// THE INVARIANT HALF (passes on BOTH trees) is the negative control: the same
// shape whose run launched in its OWN worktree must record NOTHING, even though
// the operator's file appears in the shared checkout inside the row's window. It
// is here to prove the fix ATTRIBUTES rather than merely making the arm loud —
// a time-only arm fails that control, which is exactly what the first draft of
// this change did.
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

const (
	zz2EvidenceDir   = "devdoc/evidence"
	zz2UIClientDir   = "internal/uiclient"
	zz2ScratchEntry  = "scratch/evidence-01a0dbe4.txt"
	zz2MeasuredRow   = "01a0dbe4-ac93-74f6-8afe-e740669c9532"
	zz2MeasuredCheck = "/home/nick/work/GoPi"
	zz2DepositEvent  = "completed-unclean-main-checkout-deposit"
)

// zz2Fixture is the measured shape on a throwaway repo: the main checkout (the
// fixture's `repo`) with TRACKED deposit parents, plus the row's own linked
// worktree.
func zz2Fixture(t *testing.T) (repo, wt, branch string) {
	t.Helper()
	repo, wt, branch = deadHolderReclaimFixture(t)
	for _, d := range []string{zz2EvidenceDir, zz2UIClientDir} {
		dir := filepath.Join(repo, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".keep"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, repo, "add", filepath.Join(zz2EvidenceDir, ".keep"), filepath.Join(zz2UIClientDir, ".keep"))
	gitRun(t, repo, "commit", "-m", "tracked deposit parents")
	return repo, wt, branch
}

// zz2SeedSession makes the row's run REAL: a session record whose
// session_start.workdir is launchDir, written into the pinned gopi root's
// sessions tree, with the row's session_id pointed at it. This is the lineage
// witness the fix reads — and it is written with pre-fix API only.
func zz2SeedSession(t *testing.T, taskID, launchDir string) {
	t.Helper()
	sessHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sessHome, ".gopi", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.GopiDirEnv, sessHome)
	sessID := "sess-" + taskID
	line := fmt.Sprintf(`{"type":"session_start","sessionId":%q,"model":"m","timestamp":"x","workdir":%q}`, sessID, launchDir)
	if err := os.WriteFile(filepath.Join(sessHome, ".gopi", "sessions", sessID+".jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `UPDATE tasks SET session_id = ? WHERE task_id = ?`, sessID, taskID); err != nil {
		t.Fatal(err)
	}
}

// zz2Events returns every event detail on the row's trail naming the checkout.
func zz2Events(t *testing.T, taskID, repo string) []string {
	t.Helper()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	rows, err := db.QueryContext(context.Background(),
		`SELECT detail FROM task_events WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, repo) {
			out = append(out, detail)
		}
	}
	return out
}

// TestZZRedProof2_RepoDoorRecordsTheRowWhoseDeliverableIsTheSharedCheckout is the
// RED half: pre-fix, nothing is recorded for this shape.
func TestZZRedProof2_RepoDoorRecordsTheRowWhoseDeliverableIsTheSharedCheckout(t *testing.T) {
	repo, wt, branch := zz2Fixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "evidence-heavy work with a silent briefing",
		Payload: "Collect the pixel evidence and the dumps for this round; land nothing else.",
		TaskID:  "t-zz2-repo", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-zz2-repo", repo, branch, wt)
	if _, err := TaskStart("t-zz2-repo", "worker-1"); err != nil {
		t.Fatal(err)
	}
	zz2SeedSession(t, "t-zz2-repo", repo) // the run stood IN the shared checkout
	deposit := filepath.Join(repo, filepath.FromSlash(zz2ScratchEntry))
	if err := os.MkdirAll(filepath.Dir(deposit), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deposit, []byte("dump\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := reclaimGitStatus(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before, "scratch/") {
		t.Fatalf("the fixture must reproduce the measured deposit (git must REPORT it): %q", before)
	}
	if _, err := TaskRunWriteback("t-zz2-repo", TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	if events := zz2Events(t, "t-zz2-repo", repo); len(events) == 0 {
		t.Fatalf("UNATTRIBUTED DEPOSIT: the run closed out `%s` after writing %s into the repo's MAIN checkout %s — "+
			"the row's deliverable.repo IS that checkout AND its own session recorded it as the launch directory, "+
			"while its BRIEFING never names it, so no arm on master measures that shared tree and NOTHING on the "+
			"row's trail says so (reproduction: row %s on %s, which invented <repo>/scratch/ — 161 files)",
			TaskStatusCompleted, zz2ScratchEntry, repo, zz2MeasuredRow, zz2MeasuredCheck)
	}
	// The arm never touches a byte.
	if after, err := reclaimGitStatus(repo); err != nil || after != before {
		t.Fatalf("the shared checkout must be byte-identical across the close-out (err=%v):\nbefore=%q\nafter =%q", err, before, after)
	}
}

// TestZZRedProof2_RepoDoorStaysSilentForARunThatDidNotLaunchThere is the
// INVARIANT half: master passes it too, and it is what forbids the fix from
// degenerating into "measure any untracked entry inside the window".
func TestZZRedProof2_RepoDoorStaysSilentForARunThatDidNotLaunchThere(t *testing.T) {
	repo, wt, branch := zz2Fixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "unrelated work", Payload: "work only inside " + wt,
		TaskID: "t-zz2-foreign", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-zz2-foreign", repo, branch, wt)
	if _, err := TaskStart("t-zz2-foreign", "worker-1"); err != nil {
		t.Fatal(err)
	}
	zz2SeedSession(t, "t-zz2-foreign", wt) // launched in its OWN worktree
	if err := os.WriteFile(filepath.Join(repo, "operator-note.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback("t-zz2-foreign", TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	if events := zz2Events(t, "t-zz2-foreign", repo); len(events) != 0 {
		t.Fatalf("the arm must NOT attribute untracked dirt to a run that did not launch in the shared checkout; "+
			"got %d record(s):\n%s", len(events), strings.Join(events, "\n"))
	}
}
