// Tests for the deliverable selector → ONE row resolution
// (task_deliverable_selector.go) and its worktree sibling (task_for_worktree.go).
//
// The selector is the CLI's `--branch B` / `--worktree PATH` on `gopi task
// show`: the operator standing in a task worktree holds `git branch
// --show-current` or `pwd`, never the row id. Each acceptance half is pinned
// here:
//
//   - the branch selector resolves to the SAME row the live-first policy picks
//     among several owners, and the plurality is REPORTED, not hidden
//     (TestResolveByDeliverablePicksTheLiveOwnerAndReportsTheRest);
//   - the worktree selector resolves from a path spelled another way
//     (`…/wt/` vs `…/wt`, `…/./wt`) — one worktree, not three
//     (TestResolveByWorktreeMatchesOnPathIdentity);
//   - the two selectors are one address, not a filter pair, and neither being
//     set is a usage error (TestResolveByDeliverableRefusesBothAndNeither);
//   - a selector no row records is a NAMED error, never an empty row
//     (TestResolveByDeliverableUnknownSelectorIsAnError);
//   - the resolution claims nothing and writes nothing
//     (TestResolveByDeliverableIsReadOnly).

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deliverableSelectorFixture points the process at a fresh ledger and returns a
// repo path plus a worktree path. No git repository is created ON PURPOSE: the
// resolution reads the LEDGER and never runs git, and a fixture that needed a
// repository would hide a resolver that secretly shelled out to one.
func deliverableSelectorFixture(t *testing.T) (repo, worktree string) {
	t.Helper()
	// Rule 32: a fresh per-test path from the repo's OWN scaffold (its sibling
	// forBranchFixture convention), which pins GOPI_MEMORY_DB at it — every
	// ledger read in this file resolves to THIS file, never the operator's.
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo = filepath.Join(t.TempDir(), "repo")
	worktree = filepath.Join(t.TempDir(), "workspace", "gopi-selector")
	// One row so the ledger file (and its schema) exists before the first
	// lookup — the read path must never be the thing that creates a ledger.
	forBranchSeed(t, "t-warm", taskStatusPending, "", repo, "", "")
	return repo, worktree
}

// TestResolveByDeliverablePicksTheLiveOwnerAndReportsTheRest pins the singular
// answer on the shape the live ledger actually has (8 rows on one branch was
// measured): the IN-FLIGHT owner wins, the terminal owners are named, and the
// choice is never silent.
func TestResolveByDeliverablePicksTheLiveOwnerAndReportsTheRest(t *testing.T) {
	repo, _ := deliverableSelectorFixture(t)
	branch := "feat/selector-shared"
	forBranchSeed(t, "t-done", taskStatusCompleted, "", repo, branch, "")
	forBranchSeed(t, "t-live", taskStatusClaimed, "", repo, branch, "")

	res, err := ResolveTaskIDByDeliverable(DeliverableSelector{Branch: branch}, "")
	if err != nil {
		t.Fatalf("ResolveTaskIDByDeliverable: %v", err)
	}
	if res.TaskID != "t-live" {
		t.Errorf("resolved = %q, want t-live — the live owner outranks the decided one (the SAME policy the merge fence uses)", res.TaskID)
	}
	if len(res.Owners) != 2 {
		t.Fatalf("owners = %d, want 2 (both rows named the branch)", len(res.Owners))
	}
	note := DeliverableResolutionNote(res, DeliverableSelector{Branch: branch})
	if !strings.Contains(note, "2 rows name branch "+branch) || !strings.Contains(note, "t-done") {
		t.Errorf("the note must name the other owner and the count:\n%s", note)
	}
	if strings.Contains(note, "t-live (") {
		t.Errorf("the note must not list the SHOWN row as an other-owner: %s", note)
	}

	// A single-owner selector has no note at all (a one-row answer is not
	// ambiguous, and an empty line above the digest would be noise).
	forBranchSeed(t, "t-solo", taskStatusCompleted, "", repo, "feat/selector-solo", "")
	solo, err := ResolveTaskIDByDeliverable(DeliverableSelector{Branch: "feat/selector-solo"}, "")
	if err != nil {
		t.Fatalf("solo resolve: %v", err)
	}
	if n := DeliverableResolutionNote(solo, DeliverableSelector{Branch: "feat/selector-solo"}); n != "" {
		t.Errorf("a single-owner selector must produce no note, got %q", n)
	}
}

// TestResolveByWorktreeMatchesOnPathIdentity: the operator's `pwd` and the
// row's recorded worktree are the same directory spelled two ways — they must
// be one answer, not two.
func TestResolveByWorktreeMatchesOnPathIdentity(t *testing.T) {
	repo, wt := deliverableSelectorFixture(t)
	forBranchSeed(t, "t-wt", taskStatusClaimed, "", repo, "feat/selector-wt", wt)

	for _, spelling := range []string{wt, wt + string(filepath.Separator), filepath.Join(wt, "."), "  " + wt + "  "} {
		res, err := ResolveTaskIDByDeliverable(DeliverableSelector{Worktree: spelling}, "")
		if err != nil {
			t.Fatalf("worktree %q: %v", spelling, err)
		}
		if res.TaskID != "t-wt" {
			t.Errorf("worktree %q resolved to %q, want t-wt", spelling, res.TaskID)
		}
	}
	// A DIFFERENT worktree does not match (the key is the path, not a prefix).
	if _, err := ResolveTaskIDByDeliverable(DeliverableSelector{Worktree: filepath.Join(wt, "nested")}, ""); err == nil {
		t.Error("a nested path under the worktree must not resolve to the worktree's row")
	}
	// The note names the WORKTREE, never the branch stored in the same row.
	note := DeliverableResolutionNote(
		DeliverableResolution{TaskID: "t-wt", Owners: []TaskForBranchOwner{{TaskID: "t-wt"}, {TaskID: "t-other", Status: taskStatusCompleted}}},
		DeliverableSelector{Worktree: wt})
	if !strings.Contains(note, "worktree "+wt) {
		t.Errorf("a worktree resolution's note must say so:\n%s", note)
	}
}

// TestResolveByDeliverableRefusesBothAndNeither: the two selectors are two
// spellings of ONE address. Setting both is a usage error (never a silent
// preference), and setting neither names no row at all.
func TestResolveByDeliverableRefusesBothAndNeither(t *testing.T) {
	deliverableSelectorFixture(t)
	if _, err := ResolveTaskIDByDeliverable(DeliverableSelector{}, ""); err == nil {
		t.Error("neither selector set must be refused by name")
	}
	if _, err := ResolveTaskIDByDeliverable(DeliverableSelector{Branch: "b", Worktree: "/w"}, ""); err == nil {
		t.Error("both selectors set must be refused by name (they are not a filter pair)")
	}
	if _, err := ResolveTaskIDByDeliverable(DeliverableSelector{Worktree: "   "}, ""); err == nil {
		t.Error("a blank worktree must be refused — it names no row")
	}
}

// TestResolveByDeliverableUnknownSelectorIsAnError is the Rule 15 half: a
// selector no row records must fail loudly and name the selector, so the
// operator cannot mistake it for a rendered row.
func TestResolveByDeliverableUnknownSelectorIsAnError(t *testing.T) {
	repo, _ := deliverableSelectorFixture(t)
	forBranchSeed(t, "t-known", taskStatusCompleted, "", repo, "feat/selector-known", "")

	_, err := ResolveTaskIDByDeliverable(DeliverableSelector{Branch: "feat/selector-nobody-has"}, "")
	if err == nil {
		t.Fatal("an unknown branch must be an error at the show face, not an empty digest")
	}
	if !strings.Contains(err.Error(), "feat/selector-nobody-has") {
		t.Errorf("the error must name the selector it could not resolve: %v", err)
	}
	_, err = ResolveTaskIDByDeliverable(DeliverableSelector{Worktree: filepath.Join(t.TempDir(), "no-such-wt")}, "")
	if err == nil || !strings.Contains(err.Error(), "worktree") {
		t.Errorf("an unknown worktree must be an error naming the worktree: %v", err)
	}
}

// TestResolveByDeliverableIsReadOnly: the resolution claims nothing, moves no
// status and writes no row — it is a pure read of the ledger.
func TestResolveByDeliverableIsReadOnly(t *testing.T) {
	repo, wt := deliverableSelectorFixture(t)
	branch := "feat/selector-readonly"
	forBranchSeed(t, "t-ro", taskStatusClaimed, "", repo, branch, wt)

	before := deliverableSelectorSnapshot(t)
	if _, err := ResolveTaskIDByDeliverable(DeliverableSelector{Branch: branch}, ""); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := ResolveTaskIDByDeliverable(DeliverableSelector{Worktree: wt}, ""); err != nil {
		t.Fatalf("resolve worktree: %v", err)
	}
	if after := deliverableSelectorSnapshot(t); after != before {
		t.Errorf("the ledger changed across two resolutions — a lookup must not write:\nbefore %s\nafter  %s", before, after)
	}
}

// deliverableSelectorSnapshot renders every row's mutable columns as one line,
// so a write anywhere in the ledger is visible to the read-only test.
func deliverableSelectorSnapshot(t *testing.T) string {
	t.Helper()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	rows, err := db.QueryContext(context.Background(),
		`SELECT task_id, status, COALESCE(claimed_by,''), COALESCE(session_id,''), COALESCE(deliverable,'') FROM tasks ORDER BY task_id`)
	if err != nil {
		t.Fatalf("snapshot query: %v", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id, status, claimedBy, sessionID, blob string
		if err := rows.Scan(&id, &status, &claimedBy, &sessionID, &blob); err != nil {
			t.Fatalf("snapshot scan: %v", err)
		}
		b.WriteString(strings.Join([]string{id, status, claimedBy, sessionID, blob}, "|"))
		b.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("snapshot rows: %v", err)
	}
	return b.String()
}

// TestTaskForWorktreeAnswersThePathItWasAskedAbout pins the sibling lookup
// directly: the plural answer is the worktree's owners, ordered live-first,
// and a row with no branch is not an owner (the shared scan's unit).
func TestTaskForWorktreeAnswersThePathItWasAskedAbout(t *testing.T) {
	repo, wt := deliverableSelectorFixture(t)
	forBranchSeed(t, "t-wt-live", taskStatusClaimed, "", repo, "feat/wt-live", wt)
	forBranchSeed(t, "t-wt-done", taskStatusCompleted, "", repo, "feat/wt-done", wt)

	got, err := TaskForWorktree(TaskForWorktreeQuery{Worktree: wt}, "")
	if err != nil {
		t.Fatalf("TaskForWorktree: %v", err)
	}
	if !got.Readable {
		t.Error("Readable=false for a readable ledger")
	}
	if len(got.Owners) != 2 {
		t.Fatalf("owners = %d (%+v), want 2", len(got.Owners), got.Owners)
	}
	if got.Owners[0].TaskID != "t-wt-live" || got.Owners[1].TaskID != "t-wt-done" {
		t.Errorf("owner order = [%s %s], want the live owner first", got.Owners[0].TaskID, got.Owners[1].TaskID)
	}
	// The refusal half: an empty worktree is a usage error, never a match-all.
	if _, err := TaskForWorktree(TaskForWorktreeQuery{Worktree: "   "}, ""); err == nil {
		t.Error("an empty worktree must be refused, not treated as a wildcard")
	}
	// The report's header names the WORKTREE question (Rule 15: an answer that
	// mislabels what was asked is worse than no answer).
	if report := TaskForBranchReport(got); !strings.Contains(report, "worktree "+wt) {
		t.Errorf("the report must name the worktree it answered about:\n%s", report)
	}
}

// TestTaskWorktreePathKeyIsSingleValued pins the comparison key itself — one
// directory, many spellings, no symlink resolution (which would make the answer
// depend on the filesystem state at query time).
func TestTaskWorktreePathKeyIsSingleValued(t *testing.T) {
	base := t.TempDir()
	want := taskWorktreePathKey(base)
	if want == "" {
		t.Fatal("a real directory must produce a non-empty key")
	}
	for _, spelling := range []string{base + "/", base + "/.", "  " + base + "  ", filepath.Join(base, "sub", "..")} {
		if got := taskWorktreePathKey(spelling); got != want {
			t.Errorf("taskWorktreePathKey(%q) = %q, want %q (one directory, one key)", spelling, got, want)
		}
	}
	if got := taskWorktreePathKey("   "); got != "" {
		t.Errorf("a blank path must key to %q, got %q", "", got)
	}
	if got := taskWorktreePathKey(filepath.Join(base, "sub")); got == want {
		t.Error("a different directory must not share the worktree's key")
	}
	// A symlinked spelling is NOT resolved: the ledger records what the
	// dispatch path recorded.
	link := filepath.Join(base, "link")
	if err := os.Symlink(base, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if taskWorktreePathKey(link) == taskWorktreePathKey(base) {
		t.Error("the key must not resolve symlinks — the comparison must not depend on query-time filesystem state")
	}
}
