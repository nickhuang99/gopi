package tools

// task_deliverable_adopt_rebind_test.go — the ADOPT arm of
// `deliverable.worktree`.
//
// THE DEFECT (measured 2026-10-29): a run SPAWNED INTO a ground adopts it
// (`GOPI_RUN_GROUND` → chdir → the shared ground stamp), but the shared stamp is
// a DETECTION seam and NEVER rebinds an existing value (the 2026-09-12
// foreign-owner hijack). So a row that already named a different directory kept
// naming it while the run worked in the adopted one — and the delivery leg's
// anchor gate then refused, correctly, with "deliverable.worktree is A, but this
// attempt publishes from B". The record is what has to move.
//
// THE ARMS:
//
//	① a row naming A, a run that adopted B → the row names B, and the line says
//	  the old value and WHY it changed (a rebind nobody can read is
//	  indistinguishable from a hijack);
//	② the row already names B → no write, no line (idempotence);
//	③ a directory that is NOT a work tree of its own (git walks up to an
//	  enclosing repository) → REFUSED with the measured head directory named; the
//	  row keeps its record. This is the guard that keeps the adopt arm from
//	  becoming the hijack it replaces.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// adoptRebindFixture is a real repo with TWO checkouts of it — the shape of "the
// row recorded A, the run adopted B".
type adoptRebindFixture struct {
	db  string
	a   string // the directory the row records
	b   string // the directory the run adopted
	row string
}

func newAdoptRebindFixture(t *testing.T) adoptRebindFixture {
	t.Helper()
	f := adoptRebindFixture{db: filepath.Join(t.TempDir(), "memory.db"), row: "t-adopt-rebind-1"}
	t.Setenv(memoryDBEnv, f.db)
	base := t.TempDir()
	f.a = filepath.Join(base, "ground-a")
	f.b = filepath.Join(base, "ground-b")
	for _, dir := range []string{f.a, f.b} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		deliverableWTGit(t, dir, "init", "-q", "-b", "master")
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(dir+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		deliverableWTGit(t, dir, "add", "README.md")
		deliverableWTGit(t, dir, "commit", "-q", "-m", "base in "+filepath.Base(dir))
	}
	// THE ROW RECORDS A, paired with the repo it pairs with.
	ctx := context.Background()
	conn, err := openTasksDB(ctx, f.db)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer conn.Close()
	blob := `{"repo":"` + f.a + `","branch":"master","worktree":"` + f.a + `"}`
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, created_at, updated_at)
		 VALUES (?, ?, 'in_progress', ?, 'auto', ?, ?)`,
		f.row, "a row that recorded one ground and adopted another", blob, nowStamp(), nowStamp()); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	return f
}

// adoptRebindDeliverable decodes the row's deliverable blob.
func adoptRebindDeliverable(t *testing.T, db, row string) (repo, branch, worktree string) {
	t.Helper()
	var d struct {
		Repo     string `json:"repo"`
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(deliverableWTRowBlob(t, db, row)), &d); err != nil {
		t.Fatalf("decode deliverable: %v", err)
	}
	return d.Repo, d.Branch, d.Worktree
}

// TestTaskDeliverableWorktreeAdoptMovesTheRecord is arm ① plus arm ② (the
// idempotent re-adopt) in one walk, because they are the same question asked
// twice.
func TestTaskDeliverableWorktreeAdoptMovesTheRecord(t *testing.T) {
	f := newAdoptRebindFixture(t)

	// THE LINE'S SINK, captured so the operator-facing half is asserted too.
	var lines []string
	prev := taskWorktreeStampLog
	taskWorktreeStampLog = func(format string, args ...any) {
		lines = append(lines, sprintfForTest(format, args...))
	}
	t.Cleanup(func() { taskWorktreeStampLog = prev })

	changed, err := TaskDeliverableWorktreeAdopt(f.row, f.b, "master")
	if err != nil {
		t.Fatalf("adopt the announced ground: %v", err)
	}
	if !changed {
		t.Fatalf("the record did not move although the row named %s and the run adopted %s", f.a, f.b)
	}
	repo, branch, worktree := adoptRebindDeliverable(t, f.db, f.row)
	if worktree != f.b {
		t.Fatalf("deliverable.worktree = %q, want the adopted ground %q", worktree, f.b)
	}
	if repo != f.a {
		t.Fatalf("the adopt arm must not touch deliverable.repo (repo = %q, want %q)", repo, f.a)
	}
	if branch != "master" {
		t.Fatalf("deliverable.branch = %q, want master", branch)
	}
	// THE LINE NAMES BOTH VALUES AND THE CAUSE: an operator must be able to tell
	// a deliberate rebind from the hijack the never-rebind guard exists for.
	if len(lines) != 1 {
		t.Fatalf("want exactly one stamp line, got %d: %v", len(lines), lines)
	}
	for _, want := range []string{f.row, f.a, f.b, TaskDeliverableWorktreeAdoptMoment, "ADOPTED"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the rebind line must carry %q:\n%s", want, lines[0])
		}
	}

	// ARM ② — IDEMPOTENT: the row already names the adopted ground, so a second
	// call writes nothing and says nothing (a line per round would be noise that
	// makes a real rebind unfindable in a log).
	lines = nil
	before := deliverableWTRowBlob(t, f.db, f.row)
	changed, err = TaskDeliverableWorktreeAdopt(f.row, f.b, "master")
	if err != nil {
		t.Fatalf("re-adopt: %v", err)
	}
	if changed {
		t.Fatal("a second adopt of the SAME ground wrote the record again")
	}
	if after := deliverableWTRowBlob(t, f.db, f.row); after != before {
		t.Fatalf("an idempotent adopt changed the blob:\n%s\n%s", before, after)
	}
	if len(lines) != 0 {
		t.Fatalf("an idempotent adopt must say nothing, got: %v", lines)
	}

	// AND THE SAME DIRECTORY SPELLED ANOTHER WAY IS THE SAME DIRECTORY: the
	// comparison is an identity (absolute + symlink-resolved), not a string
	// compare — otherwise every round would "rebind" to the same place.
	link := filepath.Join(t.TempDir(), "spelling-of-b")
	if err := os.Symlink(f.b, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	changed, err = TaskDeliverableWorktreeAdopt(f.row, link, "master")
	if err != nil {
		t.Fatalf("adopt through a symlink: %v", err)
	}
	if changed {
		t.Fatal("a symlinked spelling of the recorded ground was treated as a different directory")
	}
}

// TestTaskDeliverableWorktreeAdoptRefusesADirectoryThatIsNotAGround is arm ③: the
// gate that keeps this writer from becoming the hijack it replaces. A directory
// with no `.git` of its own makes `git -C` answer about the ENCLOSING repository,
// so recording it would point the row at a ground nothing can measure, deliver
// from or reclaim.
func TestTaskDeliverableWorktreeAdoptRefusesADirectoryThatIsNotAGround(t *testing.T) {
	f := newAdoptRebindFixture(t)
	notAGround := filepath.Join(f.b, "nested")
	if err := os.Mkdir(notAGround, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	before := deliverableWTRowBlob(t, f.db, f.row)
	changed, err := TaskDeliverableWorktreeAdopt(f.row, notAGround, "master")
	if err == nil {
		t.Fatalf("a directory that is not a work tree of its own was RECORDED (changed=%v) — the row would "+
			"then name a ground whose every count is about %s", changed, f.b)
	}
	// THE REFUSAL NAMES THE MEASURED HEAD DIRECTORY, because that is the fact the
	// operator needs: "this directory is inside that one".
	if !strings.Contains(err.Error(), f.b) || !strings.Contains(err.Error(), notAGround) {
		t.Fatalf("the refusal must name both the refused directory and where its HEAD actually lives:\n%v", err)
	}
	if after := deliverableWTRowBlob(t, f.db, f.row); after != before {
		t.Fatalf("a refused adopt wrote the record:\n%s\n%s", before, after)
	}
}

// sprintfForTest renders a captured log line — the capture seam takes a format
// plus args (the ONE sink's signature), so the assertion reads the same text an
// operator would.
func sprintfForTest(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
