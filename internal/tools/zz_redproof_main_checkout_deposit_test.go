package tools

// Red proof for the MAIN-CHECKOUT arm (epic 01a0db25). This file is written
// against the PRE-FIX API ONLY — every symbol it calls (TaskSubmit / TaskStart /
// TaskRunWriteback / seedDeliverable / deadHolderReclaimFixture / gitRun /
// reclaimGitStatus / openTestTasksDB) exists on master, and it defines its own
// fixture helpers rather than borrowing those of the fixed tree's test file, so
// the SAME file compiles on BOTH trees and the two runs differ by the fix alone:
//
//	# baseline (master, pre-fix) — copy THIS FILE ONLY; nothing else is needed
//	git worktree add --detach <scratch>/maindep-red master
//	cp internal/tools/zz_redproof_main_checkout_deposit_test.go <scratch>/maindep-red/internal/tools/
//	cd <scratch>/maindep-red && go test -count=1 -v -run 'TestZZRedProof_MainCheckoutDeposit' ./internal/tools/
//	--- FAIL: ... UNATTRIBUTED DEPOSIT: the run closed out `completed` after writing
//	    devdoc/evidence/tui-two-defects/ and internal/uiclient/live_emphasis_attr_test.go
//	    into the repo's MAIN checkout …
//
//	# take <scratch> from the scratch queue, never a path you invent, and give
//	# the registration back when the measurement is done:
//	SCRATCH="$(bash -c 'source scripts/lib/tmp-ban.sh && gopi_scratch_mkdir maindep-red')"
//	git worktree remove --force "$SCRATCH"    # the recipe is reproducible; leave nothing registered
//
//	# this branch
//	go test -count=1 -run 'TestZZRedProof_MainCheckoutDeposit' ./internal/tools/
//	--- PASS
//
// THE PROPERTY: a run whose OWN briefing names the repo's MAIN checkout — the
// measured shape of live row 01a0dabe-3fa1-7cc8-b601-66f1456b8ffd — must not be
// able to deposit into that shared checkout SILENTLY: at close-out the row
// carries a record naming the checkout and the deposits. Pre-fix nothing is
// recorded anywhere (the own-worktree arm measures the row's worktree only, and
// reclaimRunUnattributableGround excludes the main checkout by construction).
//
// The fixture reproduces the shape and not the machine: the deposit parents are
// TRACKED (so git reports the deposit itself), and the payload names the
// checkout as its only absolute path while naming the deposits RELATIVELY —
// which is exactly why the path proof cannot fire on this row and the run's
// WINDOW carries the attribution.
import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	zzMainDepEvidenceDir  = "devdoc/evidence"
	zzMainDepUIClientDir  = "internal/uiclient"
	zzMainDepEvidence     = "devdoc/evidence/tui-two-defects/"
	zzMainDepTestFile     = "internal/uiclient/live_emphasis_attr_test.go"
	zzMainDepMeasuredRow  = "01a0dabe-3fa1-7cc8-b601-66f1456b8ffd"
	zzMainDepRepositoryID = "/home/nick/work/GoPi"
)

// zzMainDepFixture is the measured shape on a throwaway repo: the main checkout
// (the fixture's `repo`) with TRACKED deposit parents, plus the row's linked
// worktree.
func zzMainDepFixture(t *testing.T) (repo, wt, branch string) {
	t.Helper()
	repo, wt, branch = deadHolderReclaimFixture(t)
	for _, d := range []string{zzMainDepEvidenceDir, zzMainDepUIClientDir} {
		dir := filepath.Join(repo, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".keep"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, repo, "add", filepath.Join(zzMainDepEvidenceDir, ".keep"), filepath.Join(zzMainDepUIClientDir, ".keep"))
	gitRun(t, repo, "commit", "-m", "tracked deposit parents")
	return repo, wt, branch
}

// zzMainDepPayload copies the measured briefing's shape: the main checkout as
// the ONLY absolute path, the two deposits named relatively, and the measured
// row quoted so the reproduction is traceable from the ledger alone.
func zzMainDepPayload(repo string) string {
	return "Repo: " + repo + " (branch master at 4c802614). Re-verify every line number before relying on it.\n\n" +
		"GOAL\nFix two measured defects in the `gopi chat` terminal UI.\n\n" +
		"Write the pixel evidence into `" + zzMainDepEvidence + "` and the regression test at `" + zzMainDepTestFile + "`.\n" +
		"Reproduction of the deposit recorded on row " + zzMainDepMeasuredRow + "."
}

// zzMainDepDeposit performs the measured writes inside the SHARED checkout.
func zzMainDepDeposit(t *testing.T, repo string) {
	t.Helper()
	evidence := filepath.Join(repo, filepath.FromSlash(zzMainDepEvidence))
	if err := os.MkdirAll(evidence, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidence, "capture.txt"), []byte("pixel evidence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(repo, filepath.FromSlash(zzMainDepTestFile))
	if err := os.MkdirAll(filepath.Dir(testFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testFile, []byte("package uiclient\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestZZRedProof_MainCheckoutDepositIsRecordedForTheRowThatNamedIt(t *testing.T) {
	repo, wt, branch := zzMainDepFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "fix(chat-ui): status line + bold", Payload: zzMainDepPayload(repo),
		TaskID: "t-zz-maindep", Source: "human",
	}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-zz-maindep", repo, branch, wt)
	if _, err := TaskStart("t-zz-maindep", "worker-1"); err != nil {
		t.Fatal(err)
	}
	zzMainDepDeposit(t, repo)
	porcelain, err := reclaimGitStatus(repo)
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if !strings.Contains(porcelain, zzMainDepTestFile) {
		t.Fatalf("the fixture must reproduce the measured deposit (git must REPORT it): %q", porcelain)
	}
	if _, err := TaskRunWriteback("t-zz-maindep", TaskStatusCompleted, "done — elapsed=1s"); err != nil {
		t.Fatalf("writeback: %v", err)
	}
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	rows, err := db.QueryContext(context.Background(),
		`SELECT detail FROM task_events WHERE task_id = ? ORDER BY id`, "t-zz-maindep")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	named := 0
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, repo) {
			named++
		}
	}
	if named == 0 {
		t.Fatalf("UNATTRIBUTED DEPOSIT: the run closed out `%s` after writing %s and %s into the repo's "+
			"MAIN checkout %s (its OWN briefing named that checkout, and its payload carried no other "+
			"absolute path), and NOTHING on the row's trail says so — the close-out residue arm measures "+
			"only the row's own worktree and the shared checkout is excluded by construction "+
			"(reproduction: row %s, deposits %s + %s)",
			TaskStatusCompleted, zzMainDepEvidence, zzMainDepTestFile, repo,
			zzMainDepMeasuredRow, zzMainDepEvidence, zzMainDepTestFile)
	}
	// The arm never touches a byte of the shared checkout.
	if after, err := reclaimGitStatus(repo); err != nil || after != porcelain {
		t.Fatalf("the shared checkout must be byte-identical across the close-out (err=%v):\nbefore=%q\nafter =%q", err, porcelain, after)
	}
	_ = zzMainDepRepositoryID // the operator's own checkout, quoted for the reader of this file
}
