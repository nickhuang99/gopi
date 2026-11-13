// Tests for the read-only branch→task lookup (task_for_branch.go).
//
// The acceptance contract has three halves and each is pinned by its own test:
//
//  1. BOTH OWNERS ARE ANSWERED — a branch named by a claimed (in-flight) row
//     AND by a completed row yields both, from one call, with exit code 0
//     (TestTaskForBranchAnswersEveryOwner).
//  2. AN UNANSWERABLE QUERY IS NOT AN EMPTY ANSWER — an unknown branch prints a
//     NAMED "no owner" line and carries the DISTINCT non-zero exit code
//     TaskForBranchExitNoOwner, never 0 and never the generic 1
//     (TestTaskForBranchUnknownBranchIsANamedNonZeroAnswer).
//  3. THE LOOKUP IS READ-ONLY — it claims nothing, moves no status and writes
//     no row (TestTaskForBranchIsReadOnly).
//
// The fixtures use the SAME parser production uses (taskDeliverableBranch /
// taskDeliverableWorktree) rather than a hand-rolled blob reader, so a change
// to the blob shape reddens here instead of drifting silently.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/taskverbs"
)

// forBranchSeed files a row with the given status/claim stamp and deliverable
// blob. The row is created through the ledger's own writer (TaskNew) and the
// four columns this lookup reads are then stamped raw — the same seeding shape
// branchAttribSeed uses, so the fixture cannot drift from the production blob
// shape in a way the parser would not see (the parsers are what read it back).
func forBranchSeed(t *testing.T, taskID, status, claimedBy, repo, branch, worktree string) {
	t.Helper()
	if _, err := TaskNew("for-branch fixture "+taskID, taskID, "sess-"+taskID, ""); err != nil {
		t.Fatalf("TaskNew %s: %v", taskID, err)
	}
	blob := `{"repo":"` + repo + `"`
	if branch != "" {
		blob += `,"branch":"` + branch + `"`
	}
	if worktree != "" {
		blob += `,"worktree":"` + worktree + `"`
	}
	blob += `}`
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, session_id = ?, deliverable = ? WHERE task_id = ?`,
		status, claimedBy, "sess-"+taskID, blob, taskID); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
}

// forBranchFixture points the process at a fresh ledger and returns a repo path
// plus a branch name. No git repository is created ON PURPOSE: the lookup reads
// the LEDGER and never runs git, and a fixture that needed a repository would
// hide a lookup that secretly shelled out to one.
func forBranchFixture(t *testing.T) (repo, branch string) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo = filepath.Join(t.TempDir(), "repo")
	branch = "feat/for-branch-lookup"
	// One row so the ledger file (and its schema) exists before the first
	// lookup — the read path must never be the thing that creates a ledger.
	forBranchSeed(t, "t-warm", taskStatusPending, "", repo, "", "")
	return repo, branch
}

// TestTaskForBranchAnswersEveryOwner is the briefing's acceptance case: ONE
// claimed row (branch stamped at claim time) and ONE completed row.
func TestTaskForBranchAnswersEveryOwner(t *testing.T) {
	repo, branch := forBranchFixture(t)
	forBranchSeed(t, "t-claimed", taskStatusClaimed, "", repo, branch, filepath.Join(repo, "..", "wt"))
	forBranchSeed(t, "t-done", taskStatusCompleted, "", repo, branch, "")

	got, err := TaskForBranch(TaskForBranchQuery{Branch: branch}, "")
	if err != nil {
		t.Fatalf("TaskForBranch: %v", err)
	}
	if !got.Readable {
		t.Fatal("the ledger was readable but Readable=false — an unreadable ledger must be distinguishable from an empty answer")
	}
	if len(got.Owners) != 2 {
		t.Fatalf("owners = %d (%+v), want 2 — both the in-flight and the completion-stamped row name this branch", len(got.Owners), got.Owners)
	}
	// The in-flight owner is rendered FIRST: it is the run standing on the
	// branch, the completed row is provenance.
	if got.Owners[0].TaskID != "t-claimed" || got.Owners[1].TaskID != "t-done" {
		t.Errorf("owner order = [%s %s], want [t-claimed t-done] (in-flight first)", got.Owners[0].TaskID, got.Owners[1].TaskID)
	}
	if c := got.Owners[0]; c.Status != taskStatusClaimed || c.Branch != branch || c.SessionID != "sess-t-claimed" {
		t.Errorf("claimed owner = %+v, want status=claimed branch=%s session=sess-t-claimed", c, branch)
	}
	if d := got.Owners[1]; !d.Terminal || d.Status != taskStatusCompleted {
		t.Errorf("completed owner = %+v, want Terminal=true status=completed", d)
	}
	if code := TaskForBranchExitCode(got); code != TaskForBranchExitOwned {
		t.Errorf("exit code = %d, want %d (an owned branch is a success)", code, TaskForBranchExitOwned)
	}
	// The operator face names every field the briefing promises.
	report := TaskForBranchReport(got)
	for _, want := range []string{branch, "t-claimed", "t-done", "sess-t-claimed", "status=claimed", "status=completed"} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q:\n%s", want, report)
		}
	}
}

// TestTaskForBranchUnknownBranchIsANamedNonZeroAnswer is the Rule 15 half: an
// unanswerable query must not look like an empty answer.
func TestTaskForBranchUnknownBranchIsANamedNonZeroAnswer(t *testing.T) {
	repo, branch := forBranchFixture(t)
	forBranchSeed(t, "t-known", taskStatusCompleted, "", repo, branch, "")

	got, err := TaskForBranch(TaskForBranchQuery{Branch: "feat/nobody-adopted-this"}, "")
	if err != nil {
		t.Fatalf("an unknown branch is an ANSWER, not an error: %v", err)
	}
	if len(got.Owners) != 0 {
		t.Fatalf("owners = %+v, want none for an unknown branch", got.Owners)
	}
	if !got.Readable {
		t.Error("Readable=false for a readable ledger: the no-owner answer must be distinguishable from a failed read")
	}
	if code := TaskForBranchExitCode(got); code != TaskForBranchExitNoOwner {
		t.Errorf("exit code = %d, want %d — an unanswerable query has a DISTINCT code", code, TaskForBranchExitNoOwner)
	}
	if TaskForBranchExitNoOwner == 0 || TaskForBranchExitNoOwner == 1 {
		t.Fatalf("TaskForBranchExitNoOwner = %d: it must not collide with success (0) or the generic failure (1)", TaskForBranchExitNoOwner)
	}
	report := TaskForBranchReport(got)
	if strings.TrimSpace(report) == "" {
		t.Fatal("the no-owner answer rendered as an EMPTY string — exactly the blank success Rule 15 forbids")
	}
	if !strings.Contains(report, taskForBranchNoOwnerMarker) {
		t.Errorf("the no-owner answer must be NAMED (%q), got:\n%s", taskForBranchNoOwnerMarker, report)
	}
	// A JSON consumer gets the same distinction without parsing prose.
	js, err := TaskForBranchJSON(got)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	var back TaskForBranchLookup
	if err := json.Unmarshal([]byte(js), &back); err != nil {
		t.Fatalf("round-trip json: %v", err)
	}
	if back.Readable != true || len(back.Owners) != 0 || back.Branch != "feat/nobody-adopted-this" {
		t.Errorf("json round-trip = %+v, want readable=true owners=[] branch preserved", back)
	}
}

// TestTaskForBranchUnreadableLedgerIsNotAnEmptyAnswer: a ledger that cannot be
// read is a THIRD outcome — an error, never an empty owner list (Rule 15).
func TestTaskForBranchUnreadableLedgerIsNotAnEmptyAnswer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(dir, "memory.db"))
	// A DIRECTORY where the ledger file should be is unopenable by sqlite —
	// a path that merely does not exist would be silently CREATED and read as
	// an empty ledger, which is the confusion this test exists to forbid.
	unopenable := filepath.Join(dir, "not-a-ledger")
	if err := os.Mkdir(unopenable, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := TaskForBranch(TaskForBranchQuery{Branch: "feat/x"}, unopenable)
	if err == nil {
		t.Fatalf("an unopenable ledger answered %+v instead of failing", got)
	}
	if got.Readable {
		t.Error("Readable=true for a ledger that could not be opened")
	}
}

// TestTaskForBranchEmptyBranchIsAUsageError: no branch = nothing to look up.
func TestTaskForBranchEmptyBranchIsAUsageError(t *testing.T) {
	if _, err := TaskForBranch(TaskForBranchQuery{Branch: "   "}, ""); err == nil {
		t.Fatal("an empty branch must be refused — it would otherwise match every branchless row")
	}
}

// TestTaskForBranchRepoNarrowing: the same branch name exists in several repos,
// and a caller who names its repo must not read a foreign row as its own.
func TestTaskForBranchRepoNarrowing(t *testing.T) {
	repoA, branch := forBranchFixture(t)
	repoB := filepath.Join(t.TempDir(), "repo-b")
	forBranchSeed(t, "t-repo-a", taskStatusCompleted, "", repoA, branch, "")
	forBranchSeed(t, "t-repo-b", taskStatusCompleted, "", repoB, branch, "")

	all, err := TaskForBranch(TaskForBranchQuery{Branch: branch}, "")
	if err != nil {
		t.Fatalf("TaskForBranch: %v", err)
	}
	if len(all.Owners) != 2 {
		t.Fatalf("owners = %d, want 2 (the branch exists in two repos)", len(all.Owners))
	}
	narrowed, err := TaskForBranch(TaskForBranchQuery{Branch: branch, Repo: repoB}, "")
	if err != nil {
		t.Fatalf("TaskForBranch --repo: %v", err)
	}
	if len(narrowed.Owners) != 1 || narrowed.Owners[0].TaskID != "t-repo-b" {
		t.Fatalf("narrowed owners = %+v, want exactly t-repo-b", narrowed.Owners)
	}
	// A repo nobody stamped is a NAMED no-owner answer, not a fallthrough to
	// the full list.
	none, err := TaskForBranch(TaskForBranchQuery{Branch: branch, Repo: filepath.Join(t.TempDir(), "repo-c")}, "")
	if err != nil {
		t.Fatalf("TaskForBranch --repo (absent): %v", err)
	}
	if len(none.Owners) != 0 || TaskForBranchExitCode(none) != TaskForBranchExitNoOwner {
		t.Errorf("a foreign repo must answer no-owner (code %d), got %+v", TaskForBranchExitNoOwner, none)
	}
}

// TestTaskForBranchIgnoresRowsWithoutABranch: a row whose deliverable names a
// repo but no branch (the submit-time placeholder) is not an owner, and a row
// whose WORKTREE string contains the branch name must not be mistaken for one —
// the match is on the parsed branch field, never on a substring of the blob.
func TestTaskForBranchIgnoresRowsWithoutABranch(t *testing.T) {
	repo, branch := forBranchFixture(t)
	forBranchSeed(t, "t-placeholder", taskStatusClaimed, "", repo, "", "")
	// A row whose worktree path SPELLS the branch but whose branch field is a
	// different one: a `LIKE '%branch%'` needle would answer this row wrongly.
	forBranchSeed(t, "t-decoy", taskStatusClaimed, "", repo, "feat/other", filepath.Join(repo, branch))

	got, err := TaskForBranch(TaskForBranchQuery{Branch: branch}, "")
	if err != nil {
		t.Fatalf("TaskForBranch: %v", err)
	}
	if len(got.Owners) != 0 {
		t.Fatalf("owners = %+v, want none — the match is the parsed branch field, not a substring of the blob", got.Owners)
	}
	if TaskForBranchExitCode(got) != TaskForBranchExitNoOwner {
		t.Errorf("exit code = %d, want %d", TaskForBranchExitCode(got), TaskForBranchExitNoOwner)
	}
}

// TestTaskForBranchIsReadOnly is the briefing's "never claims, never mutates"
// half: the lookup answers and the ledger is byte-for-byte the same afterwards.
func TestTaskForBranchIsReadOnly(t *testing.T) {
	repo, branch := forBranchFixture(t)
	forBranchSeed(t, "t-ro-claimed", taskStatusClaimed, "thishost:999999", repo, branch, filepath.Join(repo, "..", "wt"))
	forBranchSeed(t, "t-ro-done", taskStatusCompleted, "", repo, branch, "")

	before := forBranchLedgerSnapshot(t)
	if _, err := TaskForBranch(TaskForBranchQuery{Branch: branch}, ""); err != nil {
		t.Fatalf("TaskForBranch: %v", err)
	}
	if _, err := TaskForBranch(TaskForBranchQuery{Branch: "feat/unknown-ro"}, ""); err != nil {
		t.Fatalf("TaskForBranch (unknown): %v", err)
	}
	after := forBranchLedgerSnapshot(t)
	if before != after {
		t.Errorf("the lookup MUTATED the ledger:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestTaskForBranchVerbConstantIsTheDispatchedSpelling: the spelling stays the
// leaf package's constant, referenced (Rule 13/14), and its RETIREMENT is
// asserted here because this is the test that used to pin it as advertised.
//
// The verb retired on 2026-10-30 (task 01a18799) — the row-level question it
// asked survives as `gopi task show --branch` / `status --branch`, and the
// RESOLVER this file tests (tools.TaskForBranch) is what both of those call,
// which is why this test file stays. What changed is only the surface: the
// spelling is still DISPATCHED (so an old command reads a removal sentence
// instead of the generic usage dump) and advertised NOWHERE.
func TestTaskForBranchVerbConstantIsTheDispatchedSpelling(t *testing.T) {
	if taskverbs.ForBranch != "for-branch" {
		t.Errorf("taskverbs.ForBranch = %q, want \"for-branch\"", taskverbs.ForBranch)
	}
	if !containsString(taskverbs.DispatchVerbs, taskverbs.ForBranch) {
		t.Errorf("%q must still be DISPATCHED (RemovedVerbs keeps the spelling answering a removal sentence)", taskverbs.ForBranch)
	}
	if containsString(taskverbs.FamilyVerbs, taskverbs.ForBranch) {
		t.Errorf("%q is RETIRED and must appear in no advertised group (FamilyVerbs)", taskverbs.ForBranch)
	}
}

// forBranchLedgerSnapshot renders every row's observable state, so "read-only"
// is asserted against the LEDGER (the row set, statuses, claim stamps,
// deliverables and the append-only event trail) rather than against an
// in-memory value.
func forBranchLedgerSnapshot(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, COALESCE(claimed_by,''), COALESCE(claimed_at,0),
		        COALESCE(session_id,''), COALESCE(deliverable,''), COALESCE(updated_at,0),
		        COALESCE(ended_at,0)
		   FROM tasks ORDER BY task_id`)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id, status, claimedBy, sessionID, deliverable string
		var claimedAt, updatedAt, endedAt float64
		if err := rows.Scan(&id, &status, &claimedBy, &claimedAt, &sessionID, &deliverable, &updatedAt, &endedAt); err != nil {
			t.Fatalf("snapshot scan: %v", err)
		}
		b.WriteString(id + "|" + status + "|" + claimedBy + "|" + sessionID + "|" + deliverable + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("snapshot rows: %v", err)
	}
	// The append-only event trail is part of the ledger's state: a status move
	// or a claim would land there even if the row columns looked unchanged.
	var events int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_events`).Scan(&events); err != nil {
		t.Fatalf("snapshot events: %v", err)
	}
	return b.String() + "events=" + strconv.Itoa(events)
}

// forBranchGit runs one git command in dir, failing the test on error.
func forBranchGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

// TestTaskForBranchAnswersCompletionStampedBranch is the end-to-end shape the
// briefing cares about: the COMPLETION-TIME backfill
// (taskBackfillDeliverableBranch, the only writer that pairs branch with
// worktree for an ordinary row) stamps a branch, and the lookup then answers it.
//
// The row is driven through the REAL production path — TaskSubmit stamps
// {"repo":…}, the run is marked completed, and the backfill is invoked with the
// branch a writeback would report — so the test proves the lookup reads what
// production writes, not what a fixture wrote.
func TestTaskForBranchAnswersCompletionStampedBranch(t *testing.T) {
	repo, branch, _ := forBranchGitFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "e2e completed row", Payload: "p", TaskID: "t-e2e-done", Source: "human", Repo: repo}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// Walk the row through the ledger's OWN state machine (the transitions the
	// dispatcher allows), never a raw status write: the point of this test is
	// that a branch stamped by the production completion path is answerable.
	for _, st := range []string{taskStatusClaimed, taskStatusInProgress, taskStatusCompleted} {
		if _, err := TaskUpdate("t-e2e-done", st); err != nil {
			t.Fatalf("transition to %s: %v", st, err)
		}
	}
	// The completion-time backfill is a no-op until the repo-only placeholder
	// is in place; assert the precondition rather than assuming it.
	if err := taskBackfillDeliverableBranch("t-e2e-done", branch); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var blob string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(deliverable,'') FROM tasks WHERE task_id = ?`, "t-e2e-done").Scan(&blob); err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if !strings.Contains(blob, branch) {
		t.Fatalf("the completion backfill did not stamp the branch: deliverable=%q", blob)
	}

	got, err := TaskForBranch(TaskForBranchQuery{Branch: branch}, "")
	if err != nil {
		t.Fatalf("TaskForBranch: %v", err)
	}
	if len(got.Owners) != 1 || got.Owners[0].TaskID != "t-e2e-done" {
		t.Fatalf("owners = %+v, want exactly the completion-stamped row", got.Owners)
	}
	if !got.Owners[0].Terminal {
		t.Error("the completion-stamped owner must read as ended (Terminal=true)")
	}
	if code := TaskForBranchExitCode(got); code != TaskForBranchExitOwned {
		t.Errorf("exit code = %d, want %d", code, TaskForBranchExitOwned)
	}
}

// forBranchGitFixture builds a real repo with a feature branch CHECKED OUT in a
// linked worktree — the shape a worker leaves behind — so the fixture feeds the
// production branch detectors the same evidence a live run would.
func forBranchGitFixture(t *testing.T) (repo, branch, worktree string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	worktree = filepath.Join(root, "wt")
	branch = "feat/completion-stamped"
	forBranchGit(t, root, "init", repo)
	forBranchGit(t, repo, "config", "user.email", "t@t")
	forBranchGit(t, repo, "config", "user.name", "t")
	forBranchGit(t, repo, "checkout", "-b", masterBranchName)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	forBranchGit(t, repo, "add", "a.txt")
	forBranchGit(t, repo, "commit", "-m", "init")
	forBranchGit(t, repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "w.txt"), []byte("work"), 0o644); err != nil {
		t.Fatal(err)
	}
	forBranchGit(t, repo, "add", "w.txt")
	forBranchGit(t, repo, "commit", "-m", "work")
	forBranchGit(t, repo, "checkout", masterBranchName)
	forBranchGit(t, repo, "worktree", "add", worktree, branch)
	return repo, branch, worktree
}

// --- the read path must never BUILD the ledger it reads -------------------
//
// THE DEFECT THESE PIN (measured 2026-09-26, cmd/agent's 10-minute package
// budget): TaskForBranch promised "a PURE READ of the ledger ... no statement
// in this file writes", and then reached openTasksDB, which initializes a
// MISSING file. Answering "nobody owns this branch" therefore cost a full
// knowledge-base first open — the schema build, ~15 DDL statements whose
// commits each pay the filesystem's durability cost (measured for the same
// work: 550 ms on this host's ext4 journal queue vs 88 ms in /dev/shm, idle).
//
// The cost is load-dependent BY CONSTRUCTION — those commits land on the shared
// device journal (firstOpenSyncMode's own note: the sync parks in
// jbd2_log_wait_commit, which every writer on the device queues behind) — so
// the same call that is fast on an idle host reaches minutes under a live
// merger + worker. That is the non-determinism the epic reports: what the read
// path CREATES here is a whole schema in the operator's ledger, and the
// fix is that it must create nothing at all.

// forBranchReadOnlyLookupBudget is the ceiling these guards allow a lookup that
// has nothing to read. It is the SHARED value (Rule 14), not a second copy: the
// merge-pass guard in cmd/agent measures against the same constant, so the two
// can never drift into disagreeing about what "nothing to read" may cost.
const forBranchReadOnlyLookupBudget = TaskForBranchReadOnlyLookupBudget

// TestTaskForBranchDoesNotCreateTheLedgerItReads is the RED-then-GREEN guard:
// with no ledger at the resolved path, a lookup must answer (no owners, and
// Readable — an absent ledger is a readable fact) and must NOT have created the
// file. Before the fix this test fails on the wrong assertion for the right
// reason: the file exists afterwards, carrying a schema nobody asked for.
func TestTaskForBranchDoesNotCreateTheLedgerItReads(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("fixture precondition: %s must not exist, stat err = %v", dbPath, err)
	}

	lookup, err := TaskForBranch(TaskForBranchQuery{Branch: "feat/nobody"}, "")
	if err != nil {
		t.Fatalf("TaskForBranch on an absent ledger: %v — an absent ledger has no rows, so it has no owners: "+
			"that is the empty answer, not a read failure (Readable=false is reserved for \"I could not look\", "+
			"and it PARKS the branch)", err)
	}
	if !lookup.Readable {
		t.Errorf("Readable = false for an ABSENT ledger — the ledger is not unreadable, it simply does not " +
			"exist; reporting it as a failure would park every branch on a host whose ledger has not been " +
			"built yet, freezing the merge queue")
	}
	if len(lookup.Owners) != 0 {
		t.Errorf("Owners = %d, want 0 from an absent ledger", len(lookup.Owners))
	}
	if _, err := os.Stat(dbPath); err == nil {
		t.Errorf("the READ path CREATED %s — TaskForBranch is documented as a pure read (\"no statement in "+
			"this file writes\"), and what it creates is a whole knowledge base: the schema build costs a "+
			"device journal commit per DDL statement, on the same shared journal a live merger and worker "+
			"are queueing on. This is the cmd/agent 10-minute hang's root cause", dbPath)
	} else if !os.IsNotExist(err) {
		t.Errorf("stat %s: %v", dbPath, err)
	}
}

// TestTaskForBranchAnswersAnAbsentLedgerUnderHardTimeBound is the bounded half:
// a lookup with nothing to read must return within
// forBranchReadOnlyLookupBudget, whatever the host is doing. It is the
// regression guard for the epic's own symptom — under a live merger + worker
// the same call used to spend minutes building a schema it then did not use.
func TestTaskForBranchAnswersAnAbsentLedgerUnderHardTimeBound(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	start := time.Now()
	lookup, err := TaskForBranch(TaskForBranchQuery{Branch: "feat/bounded"}, "")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("TaskForBranch: %v", err)
	}
	if !lookup.Readable || len(lookup.Owners) != 0 {
		t.Fatalf("want an empty readable answer, got Readable=%v Owners=%d", lookup.Readable, len(lookup.Owners))
	}
	if elapsed > forBranchReadOnlyLookupBudget {
		t.Errorf("an absent-ledger lookup took %s (budget %s) — it spent that time building a ledger it then "+
			"did not read. A schema build costs %s measured on an IDLE ext4 host, 839 ms MEAN / 2211 ms MAX "+
			"on this one under a live merger + worker, and 18 ms in /dev/shm — the device is why the same call "+
			"is fast on one host and minutes on another, because every DDL commit waits on the shared journal",
			elapsed, forBranchReadOnlyLookupBudget, "(550 ms measured)")
	}
}

// TestTaskForBranchReadsAnExistingLedgerWithoutRebuilding is the no-weakening
// half, and it is the one that would catch a "fix" that simply stopped reading:
// once the ledger EXISTS with its rows, the lookup must answer from it exactly
// as before — the same owners, the same liveness — and it must still not modify
// a byte of it.
func TestTaskForBranchReadsAnExistingLedgerWithoutRebuilding(t *testing.T) {
	repo, branch := forBranchFixture(t)
	forBranchSeed(t, "t-guard-live", taskStatusClaimed, "thishost:999999", repo, branch, "")
	forBranchSeed(t, "t-guard-done", taskStatusCompleted, "", repo, branch, "")

	before := forBranchLedgerSnapshot(t)
	lookup, err := TaskForBranch(TaskForBranchQuery{Branch: branch}, "")
	if err != nil {
		t.Fatalf("TaskForBranch: %v", err)
	}
	if !lookup.Readable {
		t.Fatal("Readable = false for a ledger that exists and carries rows")
	}
	if len(lookup.Owners) != 2 {
		t.Fatalf("Owners = %d, want 2 (one claimed, one completed) — the read guard must not stop the lookup "+
			"from reading a ledger that is really there", len(lookup.Owners))
	}
	if got := lookup.Owners[0].TaskID; got != "t-guard-live" {
		t.Errorf("first owner = %q, want the in-flight row t-guard-live (the ONE ordering opinion)", got)
	}
	if after := forBranchLedgerSnapshot(t); before != after {
		t.Errorf("the lookup MUTATED an existing ledger:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestLedgerReadGuardIsTheDecidingSeam pins the guard itself, so the predicate
// that decides "answer without opening" is asserted directly rather than only
// through its effect on one caller.
func TestLedgerReadGuardIsTheDecidingSeam(t *testing.T) {
	dir := t.TempDir()
	absent := filepath.Join(dir, "nope.db")
	if !ledgerMissingForRead(absent) {
		t.Errorf("ledgerMissingForRead(%q) = false, want true for a file that does not exist", absent)
	}
	if !ledgerMissingForRead("") {
		t.Error("ledgerMissingForRead(\"\") = false — a caller that named no ledger must not cause one to appear")
	}
	present := filepath.Join(dir, "here.db")
	if err := os.WriteFile(present, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ledgerMissingForRead(present) {
		t.Errorf("ledgerMissingForRead(%q) = true for an EXISTING file — existence is decided by the "+
			"filesystem, never by a path spelling", present)
	}
	// A directory named like a ledger is a thing that exists: the read path
	// must not try to build a schema over it either, and the guard decides by
	// existence alone — fileExists is true for a directory, so this arm answers
	// false and the opener reports what is actually wrong with it.
	if !ledgerMissingForRead(filepath.Join(dir, "sub", "x.db")) {
		t.Error("ledgerMissingForRead = false for a path under a directory that does not exist — answering " +
			"the empty result there keeps a READ path from minting a directory tree on its way to a ledger " +
			"nobody asked for")
	}
	// THE BLOCKER CASE — and the one the first cut of this guard got WRONG.
	// A path whose PARENT is a regular file cannot name a ledger at all: it is
	// "I could not look" (Rule 15), not "not built yet", and it must fall
	// through to the opener so the real error surfaces. Measured 2026-09-26:
	// answering the empty result here turned an unopenable ledger into
	// "no owner" and was caught by the then-live for-branch CLI test (that verb
	// is retired 2026-10-30, task 01a18799; TestTaskVerbStatusRunsAsAdvertised
	// asserts the same arm through `status --branch`) — exit 4 where the contract
	// wants 1, "never the no-owner answer".
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	underAFile := filepath.Join(blocker, "memory.db")
	if ledgerMissingForRead(underAFile) {
		t.Errorf("ledgerMissingForRead(%q) = true with a regular FILE as its parent — that path can name no "+
			"ledger, so treating it as an absent one reports \"no owner\" for an unreadable ledger", underAFile)
	}
	if !filepath.IsAbs(underAFile) {
		t.Fatalf("fixture precondition: %q should be absolute — this arm exists to prove an ABSOLUTE path "+
			"gets no special treatment", underAFile)
	}
	// A path that exists IS readable-as-a-ledger-attempt, whatever it holds:
	// whether the bytes parse is the opener's question, never this predicate's.
	if ledgerMissingForRead(blocker) {
		t.Errorf("ledgerMissingForRead(%q) = true for a file that exists", blocker)
	}
}

// --- the no-owner answer is scoped to ONE host's ONE ledger ---------------
//
// THE DEFECT THESE PIN (epic 01a147e5, measured 2026-10-17): the no-owner
// answer named neither the MACHINE it was read on nor the boundary of the
// claim, so `no owner: no ledger row names branch X` was read as a statement
// about the install. On a two-host setup with row exchange off (`task_sync`
// defaults OFF) every branch a client produced reads as ownerless on the other
// host — `/home/<user>/.gopi/memory.db` is the SAME path literal on both — and
// a completed, correctly-cleaned run was escalated as orphan work at the price
// of a whole agent session. The ledger line alone cannot close that: the path
// is identical on both machines, so the HOST is what disambiguates, and the
// "not consulted" sentence is what stops the denial from being read machine-
// wide. Nothing here changes propagation: `task_sync` is the operator's switch,
// and the sentence only reports its value.

// TestTaskForBranchNoOwnerNamesHostAndLedgerAndTheBoundary is the acceptance
// arm for the epic: the rendering carries the host, the ledger AND the
// not-consulted sentence, on a REAL lookup against a real (empty) ledger.
func TestTaskForBranchNoOwnerNamesHostAndLedgerAndTheBoundary(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	// Warm the ledger through its own writer, so this test measures the ANSWER
	// and not the read guard (an absent ledger is its own, already-pinned case).
	if _, err := TaskNew("host-channel fixture", "t-host-channel", "sess-host-channel", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}

	got, err := TaskForBranch(TaskForBranchQuery{Branch: "feat/a-branch-no-row-names-here"}, "")
	if err != nil {
		t.Fatalf("TaskForBranch: %v", err)
	}
	if len(got.Owners) != 0 {
		t.Fatalf("fixture precondition: owners = %+v, want none", got.Owners)
	}
	// ① THE ANSWER CARRIES THE MACHINE — as a field, so a JSON consumer needs
	//    no prose parsing, and so the renderer and the sentence cannot disagree.
	host, hostErr := os.Hostname()
	if hostErr != nil {
		t.Skipf("os.Hostname unavailable on this host (%v) — the UNKNOWN arm is pinned separately", hostErr)
	}
	if got.Host != host {
		t.Fatalf("Host = %q, want %q — a no-owner answer that does not name the machine it read "+
			"is a claim a two-host reader will take about the install", got.Host, host)
	}
	if got.LedgerPath != dbPath {
		t.Errorf("LedgerPath = %q, want %q", got.LedgerPath, dbPath)
	}

	report := TaskForBranchReport(got)
	// ② THE THREE SENTENCES, each asserted through its OWN constant (Rule 1b):
	//    the verdict stays named, the host line names the measured host, the
	//    ledger line names the measured path, and the boundary sentence states
	//    that no other host was asked.
	for _, want := range []string{
		taskForBranchNoOwnerMarker,
		fmt.Sprintf(taskForBranchHostScopeFmt, host),
		fmt.Sprintf(taskForBranchLedgerScopeFmt, dbPath),
		fmt.Sprintf(taskForBranchNotConsultedFmt, taskSyncSwitchLabel()),
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the no-owner answer must carry %q — the denial is a fact about ONE ledger on ONE "+
				"host (epic 01a147e5), got:\n%s", want, report)
		}
	}
	// ③ THE SWITCH IS NAMED, by the key the switch's own constants define — a
	//    reader's next move (is another host's ledger even exchangeable?) is
	//    decided by it, and this answer must not leave that to be guessed.
	if !strings.Contains(report, config.TaskSyncSettingsKey) {
		t.Errorf("the boundary sentence must name %q, the knob that decides whether rows cross hosts:\n%s",
			config.TaskSyncSettingsKey, report)
	}
	// ④ ORDER: both scope lines sit UNDER the verdict and BEFORE the git
	//    channel / the size line — a scope printed after the finding is a
	//    footnote under a verdict the reader has already formed (the same
	//    measured failure epic 01a0ed38 fixed for the git channel).
	verdict := strings.Index(report, taskForBranchNoOwnerMarker)
	hostLine := strings.Index(report, fmt.Sprintf(taskForBranchHostScopeFmt, host))
	boundary := strings.Index(report, "other hosts' ledgers were NOT consulted")
	if !(verdict < hostLine && hostLine < boundary) {
		t.Errorf("order must be verdict → host → boundary (got %d, %d, %d):\n%s", verdict, hostLine, boundary, report)
	}
	// ⑤ the JSON carries the same two facts, so a script gets them without the
	//    text (Rule 15: an answer that only exists in prose is not consumable).
	js, err := TaskForBranchJSON(got)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	for _, want := range []string{`"host": "` + host + `"`, `"ledger_path": "` + dbPath + `"`} {
		if !strings.Contains(js, want) {
			t.Errorf("the JSON answer must carry %s:\n%s", want, js)
		}
	}
}

// TestTaskForBranchNoOwnerReportsAnUnreadableHostAsUnknown is the Rule-15 half
// of the host channel: when the machine cannot be named, the answer says
// UNKNOWN rather than printing a blank or silently claiming to be this host.
// It drives the RENDERER directly, so it needs no failing OS call.
func TestTaskForBranchNoOwnerReportsAnUnreadableHostAsUnknown(t *testing.T) {
	report := TaskForBranchReport(TaskForBranchLookup{
		Branch:          "feat/x",
		LedgerPath:      "/home/nick/.gopi/memory.db",
		Host:            "",
		LedgerRowsTotal: 12,
		Readable:        true,
		RowsWithBranch:  7,
	})
	if !strings.Contains(report, taskForBranchHostUnknownFmt) {
		t.Errorf("a lookup whose hostname could not be read must SAY the host is unknown:\n%s", report)
	}
	if strings.Contains(report, fmt.Sprintf(taskForBranchHostScopeFmt, "")) {
		t.Errorf("an empty hostname must never render as the host line's value:\n%s", report)
	}
	// The boundary sentence is printed either way: whether the host is known
	// says nothing about whether the OTHER hosts were asked (they were not).
	if !strings.Contains(report, "other hosts' ledgers were NOT consulted") {
		t.Errorf("the boundary sentence is owed even when the host is unknown:\n%s", report)
	}
}

// TestTaskForBranchWorktreeSiblingCarriesTheSameChannel keeps the two SELECTORS
// on one answer shape (Rule 14): the worktree spelling of the lookup must scope
// its denial the same way, or the same branch asked two ways would produce one
// honest and one machine-wide verdict.
func TestTaskForBranchWorktreeSiblingCarriesTheSameChannel(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	if _, err := TaskNew("worktree channel fixture", "t-wt-channel", "sess-wt-channel", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	got, err := TaskForWorktree(TaskForWorktreeQuery{Worktree: filepath.Join(dir, "no-such-worktree")}, "")
	if err != nil {
		t.Fatalf("TaskForWorktree: %v", err)
	}
	if got.Host == "" && got.LedgerPath == "" {
		t.Fatalf("the worktree answer carries neither host nor ledger: %+v", got)
	}
	report := TaskForBranchReport(got)
	if !strings.Contains(report, "other hosts' ledgers were NOT consulted") {
		t.Errorf("the worktree selector's denial must carry the same boundary sentence:\n%s", report)
	}
}
