package tools

// taskproc_ground_facts_test.go — the regression for the "Ground facts" sentence
// at the surface a READER actually reaches: `gopi task status`.
//
// THE DEFECT IT PINS (measured 2026-10-27, row 01a17a48). A one-line summary of
// a run's ground said, in the SAME sentence:
//
//	… is a LINKED WORKTREE (.git is a FILE), on branch master, tree clean,
//	HEAD=46a448d … No rebase in progress (no .git/rebase-merge|apply).
//
// Both clauses cannot rest on one reading. Under a `.git` FILE, `.git/rebase-*`
// NEVER EXISTS, so a hand-joined `ls .git/rebase-merge` cannot report "no
// rebase" there — it reports nothing, forever. And on the very directory the
// sentence described, `test -d .git` printed DIRECTORY: it was a CLONE. The
// sentence's shape clause was the `||` arm of a shell chain whose probe had
// exited 2 earlier in the SAME chain — a failed measurement rendered as a fact.
//
// WHY IT IS NOT COSMETIC: Rule 6c gives the two shapes OPPOSITE delivery
// instructions (a clone's commit is owed by the clone channel's delivery loop; a
// linked worktree's commit is already on the shared branch, no push needed). A
// run told the wrong one either reports finished with the commit unpublished or
// pushes what another party owns.
//
// WHAT THIS FILE DRIVES, through PRODUCTION seams only (Rule 31 — the surfaces a
// user reaches, not a shim beside them):
//
//   - `TaskSubmit` + `TaskStampDeliverableGroundForTest` build the row with a
//     recorded ground, exactly as the executor's own backfill does;
//   - `TaskProcessStatusFor` — the engine half of `gopi task status` — renders
//     the sentence, so the assertion is on the string a reader reads;
//   - the fixtures are REAL git: a real clone (`.git` a DIRECTORY) and a real
//     registered linked worktree (`.git` a FILE), each measured before it is used
//     so a fixture that silently became the other shape fails loudly.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/testutil"
)

// groundFactsTestID is the row this test cites. It is a REAL uuid-v7 spelling
// (hex and dashes) because the row id IS the ledger address and a slug would
// exercise a refusal path instead of the mechanism.
const groundFactsTestID = "01a17a48-0000-7000-8000-0fbac75c0001"

// groundFactsTaskRepo builds a REAL repository with one commit — the seed every
// fixture below clones or worktrees from.
func groundFactsTaskRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "seed")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", repo, err)
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
		{"checkout", "-q", "-b", "master"},
	} {
		if out, err := groundTestGit(repo, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "base.txt"}, {"commit", "-q", "-m", "base"},
	} {
		if out, err := groundTestGit(repo, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

// groundFactsCloneDir builds the CLONE shape and PROVES it: `.git` is a
// DIRECTORY and `git rev-parse --git-common-dir` prints `.git`, which is the
// same pair of facts the incident's own evidence printed.
func groundFactsCloneDir(t *testing.T) string {
	t.Helper()
	seed := groundFactsTaskRepo(t)
	dir := filepath.Join(t.TempDir(), "ground-clone")
	if out, err := groundTestGit(filepath.Dir(dir), "clone", "-q", seed, dir); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	info, err := os.Stat(filepath.Join(dir, devworkspace.GroundGitEntryName))
	if err != nil || !info.IsDir() {
		t.Fatalf("the CLONE fixture must have .git as a DIRECTORY: err=%v", err)
	}
	if got := devworkspace.GroundModelOf(dir); got != devworkspace.GroundModelClone {
		t.Fatalf("the ONE discriminator reports %q for the CLONE fixture", got)
	}
	return dir
}

// groundFactsLinkedDir builds the LINKED shape and PROVES it: `.git` is a FILE.
func groundFactsLinkedDir(t *testing.T) string {
	t.Helper()
	seed := groundFactsTaskRepo(t)
	dir := filepath.Join(t.TempDir(), "ground-linked")
	if out, err := groundTestGit(seed, "worktree", "add", "-q", dir, "-b", "feat/gf-linked"); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	info, err := os.Stat(filepath.Join(dir, devworkspace.GroundGitEntryName))
	if err != nil || info.IsDir() {
		t.Fatalf("the LINKED fixture must have .git as a FILE: err=%v", err)
	}
	if got := devworkspace.GroundModelOf(dir); got != devworkspace.GroundModelLinkedWorktree {
		t.Fatalf("the ONE discriminator reports %q for the LINKED fixture", got)
	}
	return dir
}

// groundFactsRowFor files a row whose RECORDED ground is dir and returns the
// status view of it — the same view `gopi task status` prints.
func groundFactsRowFor(t *testing.T, taskID, dir string) *TaskProcessStatus {
	t.Helper()
	testutil.NewTestDBPath(t) // pins GOPI_MEMORY_DB at a fresh per-test ledger
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "ground-facts fixture " + taskID,
		Payload: "ground-facts fixture",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	if err := TaskStampDeliverableGroundForTest(taskID, dir); err != nil {
		t.Fatalf("stamp %s's ground: %v", taskID, err)
	}
	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	}
	return st
}

// TestTaskStatusGroundFactsNameTheMeasuredShape is the fix's acceptance: for
// BOTH ground shapes, the status view's ground facts state THAT shape and never
// the other, and the rendered report carries the line.
func TestTaskStatusGroundFactsNameTheMeasuredShape(t *testing.T) {
	for _, tc := range []struct {
		name    string
		build   func(*testing.T) string
		want    string
		notWant string
	}{
		{name: "clone", build: groundFactsCloneDir, want: "is a CLONE", notWant: "is a LINKED WORKTREE"},
		{name: "linked-worktree", build: groundFactsLinkedDir, want: "is a LINKED WORKTREE", notWant: "is a CLONE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.build(t)
			st := groundFactsRowFor(t, groundFactsTestID, dir)
			if st.Ground != dir {
				t.Fatalf("the row's recorded ground must be the fixture: got %q, want %q", st.Ground, dir)
			}
			if strings.TrimSpace(st.GroundFacts) == "" {
				t.Fatalf("the status view must carry the ground's measured facts; got an empty field.\nreport:\n%s",
					TaskProcessStatusReport(st))
			}
			if !strings.Contains(st.GroundFacts, tc.want) {
				t.Errorf("the ground facts must state %q, got:\n%s", tc.want, st.GroundFacts)
			}
			if strings.Contains(st.GroundFacts, tc.notWant) {
				t.Errorf("the ground facts must NOT claim the other shape (%q), got:\n%s", tc.notWant, st.GroundFacts)
			}
			// The REBASE half is a measurement whose evidence is the path git
			// resolved — the fact a `<dir>/.git/rebase-merge` guess does not have.
			if !strings.Contains(st.GroundFacts, "no rebase in progress") {
				t.Errorf("a clean ground must read as no rebase, got:\n%s", st.GroundFacts)
			}
			if !strings.Contains(st.GroundFacts, "git rev-parse --git-path") {
				t.Errorf("the rebase clause must name the command it ran, got:\n%s", st.GroundFacts)
			}
			// AND IT REACHES THE RENDERED REPORT — a field nothing prints is not
			// a delivered fact.
			report := TaskProcessStatusReport(st)
			if !strings.Contains(report, devworkspace.GroundFactsLabel) {
				t.Errorf("the rendered report must carry the %q line, got:\n%s", devworkspace.GroundFactsLabel, report)
			}
			if !strings.Contains(report, tc.want) {
				t.Errorf("the rendered report must state %q, got:\n%s", tc.want, report)
			}
		})
	}
}

// TestTaskStatusGroundFactsSeeARebaseOnBothShapes is the arm a hand-joined probe
// CANNOT pass on the linked shape: there the rebase marker lives under the MAIN
// checkout, not under `<dir>/.git/`. The marker is created at the path GIT
// resolves (the only honest way to stage a rebase without running one).
func TestTaskStatusGroundFactsSeeARebaseOnBothShapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(*testing.T) string
	}{
		{name: "clone", build: groundFactsCloneDir},
		{name: "linked-worktree", build: groundFactsLinkedDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.build(t)
			probe := devworkspace.GroundRebaseProbe(dir)
			if probe.State != devworkspace.RebaseStateNone {
				t.Fatalf("the fixture must start with no rebase; probe = %+v", probe)
			}
			if err := os.MkdirAll(probe.MergePath, 0o755); err != nil {
				t.Fatalf("materialise the rebase marker %s: %v", probe.MergePath, err)
			}
			//stylegate:ignore best-effort temp cleanup; the OS/harness sweeps leftovers too
			t.Cleanup(func() { _ = os.RemoveAll(probe.MergePath) })

			st := groundFactsRowFor(t, groundFactsTestID, dir)
			if !strings.Contains(st.GroundFacts, "A REBASE IS IN PROGRESS") {
				t.Errorf("an unfinished rebase must be reported, got:\n%s", st.GroundFacts)
			}
			if !strings.Contains(st.GroundFacts, probe.MergePath) {
				t.Errorf("the clause must name the marker it found (%q), got:\n%s", probe.MergePath, st.GroundFacts)
			}
		})
	}
}

// TestTaskStatusGroundFactsSayUndeterminedRatherThanNoRebase is Rule 15 at the
// reader's surface: a ground git cannot answer about must NEVER render the
// reassuring "no rebase". The directory exists (so the row records it) but is
// not a repository and has no repository above it — the scratch root is not a
// git work tree.
func TestTaskStatusGroundFactsSayUndeterminedRatherThanNoRebase(t *testing.T) {
	dir := t.TempDir() // under the suite scratch root: no .git, no enclosing repo
	if _, err := os.Stat(filepath.Join(dir, devworkspace.GroundGitEntryName)); err == nil {
		t.Fatalf("the fixture must have no .git entry, %s has one", dir)
	}
	st := groundFactsRowFor(t, groundFactsTestID, dir)
	if strings.TrimSpace(st.GroundFacts) == "" {
		t.Fatalf("an undecidable ground must still be REPORTED as undecided, not omitted")
	}
	if !strings.Contains(st.GroundFacts, "rebase state UNDETERMINED") {
		t.Errorf("an unmeasured rebase state must render as UNDETERMINED, got:\n%s", st.GroundFacts)
	}
	if strings.Contains(st.GroundFacts, "no rebase in progress") {
		t.Errorf("an unmeasured rebase state must NOT render as 'no rebase', got:\n%s", st.GroundFacts)
	}
	// The SHAPE is undecided too, and the sentence says so rather than picking a
	// side — which is what the delivery instruction depends on.
	if !strings.Contains(st.GroundFacts, "UNDETERMINED shape") {
		t.Errorf("a directory with no .git entry must render an undetermined SHAPE, got:\n%s", st.GroundFacts)
	}
}

// TestTaskStatusGroundFactsAreAbsentWhenNoGroundIsRecorded keeps the field
// honest at its zero value: a row that records NO ground has nothing to
// summarise, and must not be handed a sentence about whatever directory the
// command happened to run in.
func TestTaskStatusGroundFactsAreAbsentWhenNoGroundIsRecorded(t *testing.T) {
	testutil.NewTestDBPath(t)
	const id = "01a17a48-0000-7000-8000-0fbac75c0002"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "ground-facts fixture without a ground",
		Payload: "ground-facts fixture",
		TaskID:  id,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", id, err)
	}
	st, err := TaskProcessStatusFor(id)
	if err != nil {
		t.Fatalf("status %s: %v", id, err)
	}
	if st.Ground != "" {
		t.Fatalf("the fixture must record no ground, got %q", st.Ground)
	}
	if st.GroundFacts != "" {
		t.Errorf("a row with no recorded ground must carry no ground facts, got %q", st.GroundFacts)
	}
	if strings.Contains(TaskProcessStatusReport(st), devworkspace.GroundFactsLabel) {
		t.Errorf("no ground must render no ground-facts line, got:\n%s", TaskProcessStatusReport(st))
	}
}
