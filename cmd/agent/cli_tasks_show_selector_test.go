// The `gopi task show` DELIVERABLE SELECTORS at the CLI entry: `--branch B` /
// `--worktree PATH` address the row an operator standing in a task worktree
// actually has (`git branch --show-current` or `pwd`), instead of the row id
// that workflow never carries.
//
// The resolution itself is unit-tested in internal/tools
// (task_deliverable_selector_test.go); what this file pins is the WIRING, at the
// three points the operator actually touches:
//
//  1. ADMISSION — both flags are accepted on `show` and refused on a verb that
//     has no use for them (the cross-verb gate stays intact);
//  2. THE THREE ADDRESSES — the positional task_id, --branch and --worktree all
//     land on the SAME digest (runTaskShow, the function the verb calls);
//  3. THE REFUSALS — no address at all, and two addresses at once, are named
//     usage errors (never a blank line, never a silent preference).

package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/taskverbs"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// showSelectorSeed files one ledger row whose deliverable blob records branch
// and worktree — the shape every row created through the dispatch path carries.
//
// THE LEDGER IS THE SCAFFOLD'S (Rule 32): testutil.NewTestDBPath hands the test
// a fresh, off-`/` database and pins GOPI_MEMORY_DB at it, so EVERY ledger read
// in this file (runTaskShow's, the resolver's) lands on this file. The row is
// created through the tools layer's own writer (TaskNew) and the two columns
// this lookup reads are then stamped raw, exactly as internal/tools'
// forBranchSeed does — the fixture cannot drift from the production blob shape
// in a way the parsers would not see, and the schema stays owned by the code
// under test (a hand-rolled CREATE/INSERT would pin a shape nobody ships).
func showSelectorSeed(t *testing.T, dbPath, taskID, status, repo, branch, worktree string) {
	t.Helper()
	if _, err := tools.TaskNew("show selector fixture "+taskID, taskID, "sess-"+taskID, ""); err != nil {
		t.Fatalf("TaskNew %s: %v", taskID, err)
	}
	blob := `{"repo":"` + repo + `","branch":"` + branch + `","worktree":"` + worktree + `"}`
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET status = ?, deliverable = ? WHERE task_id = ?`, status, blob, taskID); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
}

// showSelectorDigestClockRE matches the ONE part of a `task show` digest that is
// a reading of the WALL CLOCK rather than of the row: the liveness line stamps
// the instant it measured against (`(clock 09:35:16)`, from tools'
// taskShowTickTockLine — the operator's own clock may differ from the host's,
// so the instant is part of the answer that line gives).
//
// It is MASKED for an equality assertion between two shows, and the masking is
// the repair of a measured flake (2026-10-03): an unmasked byte comparison of
// two digests is a comparison of TWO READINGS TAKEN AT TWO INSTANTS, so it
// fails whenever the second boundary falls between them — `-count=1` green and
// `-count=5` red, on a pristine master worktree, with no row difference at all.
// MEASURED: the two digests differed on exactly ONE line, the liveness clock
// (`09:35:15` vs `09:35:16`), 1708 bytes vs 1708 bytes.
//
// Masking the clock does NOT weaken what this file asserts: the subject of the
// headline test is WHICH ROW a selector resolves to, and the resolved row's
// bytes are all still compared. It is not a silenced assertion either — the
// second-boundary regression below (TestTaskShowDigestIsAFunctionOfTheRowNotTheClock)
// requires the UNMASKED digests to differ, so a mask that hid a real difference
// (or a renderer that stopped carrying the clock) fails there.
var showSelectorDigestClockRE = regexp.MustCompile(`\(clock \d{2}:\d{2}:\d{2}\)`)

// showSelectorDigestState renders a digest in its clock-independent form: the
// row's own rendering, with the wall-clock instant of the liveness line masked
// to a fixed token. The label inside the parentheses is kept, so the line stays
// visible in a failure diff (`(clock <masked>)`) instead of vanishing.
func showSelectorDigestState(digest string) string {
	return showSelectorDigestClockRE.ReplaceAllString(digest, showSelectorDigestClockMask)
}

// showSelectorDigestClockMask is the token the mask writes (Rule 1: the masked
// form itself is spelled once, so the comparison and any reader of it agree).
const showSelectorDigestClockMask = "(clock <masked>)"

// showSelectorDigestClockStamp returns the wall-clock reading a digest carries
// (the `(clock HH:MM:SS)` stamp of its liveness line), or "" when it carries
// none — an absence with a consequence: the mask above would then be hiding
// nothing, and the comparison it serves could not be told from a real one
// (Rule 15).
func showSelectorDigestClockStamp(digest string) string {
	return showSelectorDigestClockRE.FindString(digest)
}

// showSelectorDigestRereadWait bounds how long a second READING of the same row
// is polled for a digest stamped at a different instant.
const showSelectorDigestRereadWait = 10 * time.Second

// showSelectorDigestDifferingClock re-reads one row until its digest carries a
// DIFFERENT wall-clock stamp than `first`, and returns that digest. It is the
// non-vacuity precondition of the headline comparison: a pair of readings that
// happened to share a second would make "the mask hides only the clock" true
// without ever exercising it, so the test waits for the external event that
// separates them (≤1s away — a REAL clock crossing, not a scheduler guess) and
// fails loudly if it never arrives.
func showSelectorDigestDifferingClock(t *testing.T, opts *tasksOptions, first string) string {
	t.Helper()
	stamp := showSelectorDigestClockStamp(first)
	if stamp == "" {
		t.Fatalf("the digest carries no liveness clock stamp at all, so the mask this file asserts against hides nothing:\n%s", first)
	}
	deadline := time.Now().Add(showSelectorDigestRereadWait)
	for time.Now().Before(deadline) {
		again, err := runTaskShow(opts)
		if err != nil {
			t.Fatalf("re-read the row: %v", err)
		}
		if showSelectorDigestClockStamp(again) != stamp {
			return again
		}
	}
	t.Fatalf("no second reading of %+v was stamped at a different instant within %s — the clock line cannot be measured", opts, showSelectorDigestRereadWait)
	return ""
}

// showSelectorDigestDifference classifies HOW two digests of the SAME row
// differ, so "the clock is the only volatile line" is an assertion and not a
// claim about the mask. It returns showSelectorDigestClockOnly, or a sentence
// naming the first difference that is not the clock.
func showSelectorDigestDifference(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(al) != len(bl) {
		return fmt.Sprintf("a LINE-COUNT difference (%d vs %d lines) — not the clock", len(al), len(bl))
	}
	for i := range al {
		if al[i] == bl[i] {
			continue
		}
		if showSelectorDigestState(al[i]) != showSelectorDigestState(bl[i]) {
			return fmt.Sprintf("a non-clock line %d: %q vs %q", i+1, al[i], bl[i])
		}
	}
	return showSelectorDigestClockOnly
}

// showSelectorDigestClockOnly is showSelectorDigestDifference's single clean
// verdict: every differing line is the liveness clock reading.
const showSelectorDigestClockOnly = "the liveness clock line only"

// TestTaskShowAdmitsTheDeliverableSelectors: admission is verb-scoped — the two
// flags are legal on `show` and rejected elsewhere, so the CLI's cross-verb gate
// still catches a flag passed to the wrong verb.
func TestTaskShowAdmitsTheDeliverableSelectors(t *testing.T) {
	for _, args := range [][]string{
		{"show", "--branch", "feat/x"},
		{"show", "--worktree", "/tmp/wt"},
		{"show", "01a0c86f-ced2-73dc-8837-751ce0bc820d", "--branch", "feat/x"},
	} {
		if _, err := parseTasksArgs("show", args[1:]); err != nil {
			t.Errorf("show %v must be admitted: %v", args[1:], err)
		}
	}
	// The same flags on a verb that cannot use them stay rejected.
	for _, verb := range []string{"list", "new", "for-branch"} {
		if _, err := parseTasksArgs(verb, []string{"--branch", "feat/x"}); err == nil {
			t.Errorf("%s --branch must be rejected as an unknown flag for that verb", verb)
		}
		if _, err := parseTasksArgs(verb, []string{"--worktree", "/tmp/wt"}); err == nil {
			t.Errorf("%s --worktree must be rejected as an unknown flag for that verb", verb)
		}
	}
	// The parse carries the two selectors into their own fields.
	opts, err := parseTasksArgs("show", []string{"--branch", "feat/x"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.selBranch != "feat/x" || opts.selWorktree != "" {
		t.Errorf("parsed selectors = (%q, %q), want (feat/x, \"\")", opts.selBranch, opts.selWorktree)
	}
	// …and the RETIRED plural verb (for-branch, task 01a18799) carries neither
	// selector into a field: its parse arm validates the shape only, because the
	// dispatcher refuses the spelling before anything is read. Asserted so the
	// two namespaces cannot re-merge by accident — a retired spelling that
	// silently started filling `selBranch` would be a resurrection nobody sees.
	fb, err := parseTasksArgs(taskverbs.ForBranch, []string{"feat/x"})
	if err != nil {
		t.Fatalf("for-branch parse: %v", err)
	}
	if fb.selBranch != "" || fb.selWorktree != "" {
		t.Errorf("for-branch fields = (selBranch %q, selWorktree %q), want both empty: the verb is retired and reads nothing",
			fb.selBranch, fb.selWorktree)
	}
}

// TestTaskShowRefusesNoAddressAndTwoAddresses: a show that names no row, and a
// show that names two, are both usage errors raised at PARSE time (before any
// ledger read) and they name the alternatives.
func TestTaskShowRefusesNoAddressAndTwoAddresses(t *testing.T) {
	if _, err := parseTasksArgs("show", nil); err == nil {
		t.Fatal("show with no address must be refused")
	} else if !strings.Contains(err.Error(), "--branch") || !strings.Contains(err.Error(), "--worktree") {
		t.Errorf("the empty-address error must name both alternate addresses: %v", err)
	}
	// Two ADDRESSES (a positional plus a selector) is left to the resolver: the
	// positional is checked first, so this shape is not an error — but the two
	// SELECTORS together are, and that refusal lives in the resolver's ONE
	// definition of it (asserted here through the verb so the wiring is pinned).
	opts, err := parseTasksArgs("show", []string{"--branch", "a", "--worktree", "b"})
	if err != nil {
		t.Fatalf("parse admits both selectors; the resolver refuses them: %v", err)
	}
	if _, err := runTaskShow(opts); err == nil {
		t.Fatal("--branch together with --worktree must be refused by the resolver")
	}
}

// TestShowSelectorDigestDifferenceNamesEveryNonClockChange is the NON-VACUITY
// proof for the mask the headline test compares through: the mask hides the
// liveness clock, so if it also swallowed a row difference the headline would go
// green over a real regression. The three arms below are the three ways two
// digests of one row can move — the clock, another line's text, the line count —
// and only the first may be reported as the clock.
func TestShowSelectorDigestDifferenceNamesEveryNonClockChange(t *testing.T) {
	head := "task_id:        t-x\n"
	tail := "ended_at:       (not ended)\n"

	// 1. The clock moves alone: THE expected verdict.
	clockA := head + "liveness:       last_toolcall_at=(none — unknown)  (clock 09:35:15)\n" + tail
	clockB := head + "liveness:       last_toolcall_at=(none — unknown)  (clock 09:35:16)\n" + tail
	if got := showSelectorDigestDifference(clockA, clockB); got != showSelectorDigestClockOnly {
		t.Errorf("a clock-only change must be reported as the clock (got %q)", got)
	}
	// …and the mask itself must leave the line readable in a diff.
	if !strings.Contains(showSelectorDigestState(clockA), showSelectorDigestClockMask) {
		t.Errorf("the mask must render the stamp as %q:\n%s", showSelectorDigestClockMask, showSelectorDigestState(clockA))
	}
	if !strings.Contains(showSelectorDigestState(clockA), "liveness:") {
		t.Errorf("the mask must not erase the line it masks:\n%s", showSelectorDigestState(clockA))
	}

	// 2. A BODY line changes (this is what a selector resolving the wrong row
	//    looks like): the guard must name it, never report the clock.
	bodyA := head + "summary:        first\n" + tail
	bodyB := head + "summary:        second\n" + tail
	if got := showSelectorDigestDifference(bodyA, bodyB); got == showSelectorDigestClockOnly {
		t.Errorf("a changed body line was reported as the clock — the mask hides a row difference")
	} else if !strings.Contains(got, "summary") {
		t.Errorf("the report must name the line that moved (got %q)", got)
	}

	// 3. A line appears or vanishes: also not the clock.
	if got := showSelectorDigestDifference(bodyA, bodyA+tail); got == showSelectorDigestClockOnly {
		t.Errorf("a line-count change was reported as the clock — the mask hides a row difference")
	}
}

// TestTaskShowRendersTheSameDigestForAllThreeAddresses is the epic's headline:
// the digest an operator gets from the branch (or the worktree) they are
// standing in is the digest the row id gives.
func TestTaskShowRendersTheSameDigestForAllThreeAddresses(t *testing.T) {
	dbPath := testutil.NewTestDBPath(t)
	repo := filepath.Join(t.TempDir(), "repo")
	wt := filepath.Join(t.TempDir(), "workspace", "gopi-show-selector")
	branch := "feat/show-selector-address"
	taskID := "t-show-selector"
	showSelectorSeed(t, dbPath, taskID, tools.TaskStatusInProgress, repo, branch, wt)

	byID, err := runTaskShow(&tasksOptions{taskID: taskID})
	if err != nil {
		t.Fatalf("show by id: %v", err)
	}
	byBranch, err := runTaskShow(&tasksOptions{selBranch: branch})
	if err != nil {
		t.Fatalf("show by branch: %v", err)
	}
	byWorktree, err := runTaskShow(&tasksOptions{selWorktree: wt})
	if err != nil {
		t.Fatalf("show by worktree: %v", err)
	}
	for name, got := range map[string]string{"--branch": byBranch, "--worktree": byWorktree} {
		if showSelectorDigestState(got) != showSelectorDigestState(byID) {
			t.Errorf("the digest reached by %s differs from the one reached by task_id:\n--- by id ---\n%s\n--- by %s ---\n%s",
				name, byID, name, got)
		}
		if !strings.Contains(got, taskID) {
			t.Errorf("the digest reached by %s must name the resolved row %s:\n%s", name, taskID, got)
		}
	}

	// The masking above is a comparison of the ROW, not of the clock, and this
	// is where that is pinned: two shows of the same row at two instants differ
	// in the clock line and in NOTHING ELSE. Without this case a mask that also
	// hid the resolved row's bytes would leave the headline test green
	// (non-vacuity — the mask must hide ONE thing, and the test must be able to
	// say which).
	sameSecond := showSelectorDigestDifferingClock(t, &tasksOptions{taskID: taskID}, byID)
	if diff := showSelectorDigestDifference(byID, sameSecond); diff != showSelectorDigestClockOnly {
		t.Errorf("two shows of the SAME row must differ in the liveness clock line and nothing else; the difference is %s:\n--- first ---\n%s\n--- second ---\n%s",
			diff, byID, sameSecond)
	}

	// A selector with no owner fails the verb by name (Rule 15) — and the
	// failure is the RESOLVER's, not an empty digest.
	if _, err := runTaskShow(&tasksOptions{selBranch: "feat/show-selector-nobody"}); err == nil {
		t.Error("an unknown branch must fail the show, not render a blank digest")
	} else if !strings.Contains(err.Error(), "feat/show-selector-nobody") {
		t.Errorf("the failure must name the selector: %v", err)
	}
}

// TestTaskShowSelectorNoteGoesToStderrAndDigestStaysOnStdout: a SHARED selector
// is resolved to the live owner and the other rows are NAMED — but on stderr, so
// stdout stays exactly the digest a script already parses.
func TestTaskShowSelectorNoteGoesToStderrAndDigestStaysOnStdout(t *testing.T) {
	dbPath := testutil.NewTestDBPath(t)
	repo := filepath.Join(t.TempDir(), "repo")
	branch := "feat/show-selector-shared"
	showSelectorSeed(t, dbPath, "t-shared-live", tools.TaskStatusInProgress, repo, branch, "")
	showSelectorSeed(t, dbPath, "t-shared-done", tools.TaskStatusCompleted, repo, branch, "")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	out, runErr := runTaskShow(&tasksOptions{selBranch: branch})
	os.Stderr = origStderr
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("close stderr pipe: %v", closeErr)
	}
	if runErr != nil {
		t.Fatalf("show by shared branch: %v", runErr)
	}
	note := make([]byte, 4096)
	n, readErr := r.Read(note)
	if readErr != nil && n == 0 {
		t.Fatalf("read stderr note: %v", readErr)
	}
	_ = r.Close() //stylegate:ignore closes the fixture read end
	gotNote := string(note[:n])

	if !strings.Contains(gotNote, "2 rows name branch "+branch) {
		t.Errorf("stderr must carry the plurality note:\n%q", gotNote)
	}
	if !strings.Contains(gotNote, "t-shared-done") {
		t.Errorf("the note must name the other owner:\n%q", gotNote)
	}
	// The digest itself: the live owner, and no note text on stdout.
	if !strings.Contains(out, "t-shared-live") {
		t.Errorf("the resolved row must be the LIVE owner:\n%s", out)
	}
	if strings.Contains(out, "rows name branch") {
		t.Errorf("the note must not ride stdout — scripts parse that as a field:\n%s", out)
	}
}
