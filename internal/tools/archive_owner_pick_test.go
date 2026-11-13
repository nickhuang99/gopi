package tools

// Regression guard for the third arm of the 2026-09-21/22 incident: the status
// surface described a branch by a row that was not its state.
//
// `gopi merge --status` printed
//
//	BURIED fix/stash-guard-compound-chain — 01a0c545-… (completed) — no reason recorded
//
// for a branch TWO `in_progress` rows (01a0c61a / 01a0c61b) also named. The
// ledger held both facts; the report picked the decided one, so the reader was
// handed `(completed)` for a branch that was still being built on — and the
// branch's ground was removed minutes later on the strength of that reading.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// TestArchiveBranchOwnersLetsAnOpenRowOutweighADecidedOne pins the pick: when
// two rows name one branch, the OPEN row's status is what the branch is
// described by.
func TestArchiveBranchOwnersLetsAnOpenRowOutweighADecidedOne(t *testing.T) {
	ctx := context.Background()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)

	const branch = "fix/owners-pick"
	// The decided row FIRST, so the open one must actively supersede it rather
	// than merely arriving later (the map's insert order would otherwise decide).
	seedArchiveOwnerRow(t, ctx, "t-owner-decided", taskStatusCompleted, branch)
	seedArchiveOwnerRow(t, ctx, "t-owner-open", taskStatusInProgress, branch)

	owners, err := archiveBranchOwners()
	if err != nil {
		t.Fatalf("archiveBranchOwners: %v", err)
	}
	got, ok := owners[branch]
	if !ok {
		t.Fatalf("the branch must be owned by some row, got %+v", owners)
	}
	if got.status != taskStatusInProgress {
		t.Errorf("an OPEN row must decide the branch's reported status: got %q for row %s", got.status, got.taskID)
	}
	if got.taskID != "t-owner-open" {
		t.Errorf("the reported owner must be the OPEN row, got %s", got.taskID)
	}
}

// TestArchiveBranchOwnersKeepsADecidedRowWhenNothingIsOpen is the negative half:
// with no open row the decided row stands, so the pick is a precedence and not
// a blanket preference for nothing.
func TestArchiveBranchOwnersKeepsADecidedRowWhenNothingIsOpen(t *testing.T) {
	ctx := context.Background()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)

	const branch = "fix/owners-decided-only"
	seedArchiveOwnerRow(t, ctx, "t-owner-only-decided", taskStatusCompleted, branch)

	owners, err := archiveBranchOwners()
	if err != nil {
		t.Fatalf("archiveBranchOwners: %v", err)
	}
	got, ok := owners[branch]
	if !ok {
		t.Fatal("the branch must be owned")
	}
	if got.status != taskStatusCompleted || got.taskID != "t-owner-only-decided" {
		t.Errorf("with no open row the decided one must stand, got %+v", got)
	}
}

// TestArchiveBranchOwnersInventsNothingForAnUnnamedBranch pins the third half:
// a branch NO row names stays absent from the map, so the renderer prints the
// absence word (unknown) rather than a status the ledger never held. This is the
// `(unowned)` defect: the status slot was fed a word that is not a status.
func TestArchiveBranchOwnersInventsNothingForAnUnnamedBranch(t *testing.T) {
	ctx := context.Background()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)

	seedArchiveOwnerRow(t, ctx, "t-owner-elsewhere", taskStatusCompleted, "fix/owners-elsewhere")

	owners, err := archiveBranchOwners()
	if err != nil {
		t.Fatalf("archiveBranchOwners: %v", err)
	}
	if _, ok := owners["fix/owners-never-filed"]; ok {
		t.Fatalf("a branch no row names must not appear in the map, got %+v", owners["fix/owners-never-filed"])
	}
	if got, ok := owners["fix/owners-elsewhere"]; !ok || got.status != taskStatusCompleted {
		t.Fatalf("a named branch must appear with its ledger status, got %+v (ok=%v)", got, ok)
	}
}

// seedArchiveOwnerRow files one row whose deliverable names branch.
func seedArchiveOwnerRow(t *testing.T, ctx context.Context, taskID, status, branch string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "owner " + taskID, Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, deliverable = ? WHERE task_id = ?`,
		status, `{"repo":"/repo","branch":"`+branch+`"}`, taskID); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
}

// newArchiveOwnerLedger points the process at a FRESH temp ledger + settings,
// returning the context every seed call takes.
func newArchiveOwnerLedger(t *testing.T) context.Context {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	return context.Background()
}

// TestArchiveBranchOwnersPrefersTheLiveRowOverAnArchivableOne is the regression
// guard for the four-row incident (measured 2026-10-07, zbook, run 01a115c3,
// branch task/01a113e467767eb9a4c33cb1df1cdfbb).
//
// That branch was named by FOUR rows — a `cancelled` one, a `completed` one and
// an `in_progress` one among them. The pre-fix pick asked only "is an OPEN row
// present?", where "open" meant "not completed/verified", so the CANCELLED row
// counted as the open side and won whenever the ledger returned it first. The
// gate read its status through archiveOwnerArchivable → Proceed, minted
// `archive/<branch>` with NO override, and armed the archive-reclaim arm's
// head-deleting branch — over a run that was still building.
//
// The fix is a RANK over the readings (live > decided > archivable), so the
// assertion is expressed on a live row the archivable one must lose to. Both
// orderings are driven, because the defect was ORDER-SENSITIVE: the seeded
// sequence is the only thing that changes between the two subtests, and a
// ranking that depended on insert order would pass one and fail the other.
func TestArchiveBranchOwnersPrefersTheLiveRowOverAnArchivableOne(t *testing.T) {
	const branch = "task/01a113e467767eb9a4c33cb1df1cdfbb"
	cases := []struct {
		name       string
		archivable string
		live       string
	}{
		// The measured shape: the archivable row comes FIRST, and its id also
		// sorts first, so neither scan order nor the tie-break could rescue a
		// rank-blind pick.
		{"archivable row seeded first", "01a114aa-eb19-cancelled", "01a113e4-6799-live"},
		{"live row seeded first", "zzzz-cancelled", "aaaa-live"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newArchiveOwnerLedger(t)
			// Seed order is the ONLY difference between the two cases.
			seedArchiveOwnerRow(t, ctx, tc.archivable, taskStatusCancelled, branch)
			seedArchiveOwnerRow(t, ctx, tc.live, taskStatusInProgress, branch)

			owners, err := archiveBranchOwners()
			if err != nil {
				t.Fatalf("archiveBranchOwners: %v", err)
			}
			got, ok := owners[branch]
			if !ok {
				t.Fatalf("the branch must be owned, got %+v", owners)
			}
			if got.status != taskStatusInProgress || got.taskID != tc.live {
				t.Fatalf("the LIVE row must decide the branch (a cancelled sibling is not permission to delete a "+
					"running deliverable): got %q for row %s, want %q for %s",
					got.status, got.taskID, taskStatusInProgress, tc.live)
			}

			// THE GATE'S HALF, through the real decision function: the incident
			// was not "the row was misreported", it was `gopi task archive`
			// exiting 0. A bare reason (no override) must be REFUSED here, and
			// the refusal must name the live row — not the cancelled one.
			dec, err := ArchiveDecide(branch, ArchiveReasonHandMistake, false)
			if err != nil {
				t.Fatalf("ArchiveDecide: %v", err)
			}
			if dec.Verdict != ArchiveVerdictRefused {
				t.Fatalf("verdict = %q, want %q — a cancelled sibling row must not license archiving a live run's "+
					"ground (this is the measured 01a115c3 exit-0)", dec.Verdict, ArchiveVerdictRefused)
			}
			if !strings.Contains(dec.Refusal, "IN FLIGHT") {
				t.Fatalf("refusal does not say the work is in flight: %s", dec.Refusal)
			}
			if dec.OwnerTaskID != tc.live || dec.OwnerStatus != taskStatusInProgress {
				t.Fatalf("the refusal names %s (%s), want the live row %s (%s)",
					dec.OwnerTaskID, dec.OwnerStatus, tc.live, taskStatusInProgress)
			}
			// An override does NOT clear this one: --override-completed is the
			// acknowledgement for a FINISHED owner, and there is no flag that
			// authorises burying work in flight.
			if dec, err := ArchiveDecide(branch, ArchiveReasonHandMistake, true); err != nil {
				t.Fatalf("ArchiveDecide(override): %v", err)
			} else if dec.Verdict != ArchiveVerdictRefused {
				t.Fatalf("--override-completed cleared a LIVE owner (verdict=%q): nothing may authorise burying "+
					"work in flight", dec.Verdict)
			}
		})
	}
}

// TestArchiveBranchOwnersBreaksTiesByTaskID pins the DETERMINISM half: two rows
// of the SAME rank must resolve to the same row every run, whatever order the
// ledger returns them in. The pre-fix pick was order-sensitive by construction
// (it swapped on "is this one open and that one not", which answers differently
// depending on which row arrived first), and an archive decision that varies
// between two identical runs is not a decision.
func TestArchiveBranchOwnersBreaksTiesByTaskID(t *testing.T) {
	const branch = "fix/owners-tie"
	ctx := newArchiveOwnerLedger(t)
	// Both ARCHIVABLE (same rank), archiving side seeded the ONE ordering that
	// would win if insert order decided.
	seedArchiveOwnerRow(t, ctx, "t-tie-zzz", taskStatusSuperseded, branch)
	seedArchiveOwnerRow(t, ctx, "t-tie-aaa", taskStatusCancelled, branch)

	first, err := archiveBranchOwners()
	if err != nil {
		t.Fatalf("archiveBranchOwners: %v", err)
	}
	second, err := archiveBranchOwners()
	if err != nil {
		t.Fatalf("archiveBranchOwners (rerun): %v", err)
	}
	if first[branch] != second[branch] {
		t.Fatalf("two identical reads disagreed: %+v vs %+v", first[branch], second[branch])
	}
	if first[branch].taskID != "t-tie-aaa" {
		t.Fatalf("the tie-break must be the task id, got %+v (want t-tie-aaa — the lexicographically smaller)",
			first[branch])
	}
}
