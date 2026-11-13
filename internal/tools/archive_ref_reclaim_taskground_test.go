package tools

// The task-ground half of the reclaim arm's guards — pinned as a TABLE over the
// two FACTS that actually protect a ground, rather than over its name.
//
// THE DEFECT THESE TESTS PIN (measured 2026-10-12, this host): guard ④ refused
// every `task/<id>` branch by NAME PREFIX, before anything was checked, with the
// sentence "branch … is the integration branch or a task-run ground — never a
// reclaim candidate". A branch whose ledger row was gone, whose worktree had
// been reclaimed and whose content was already on master therefore stayed in
// refs/heads FOREVER, held out by a sentence that was, about that branch,
// simply false. The four cells below are the four combinations of the two facts
// that can really protect a ground — (a ledger row tracks it) × (a worktree
// holds it) — and the FIRST cell is the one the old code got wrong.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// errReclaimLedgerUnreadable stands in for the ledger being unreadable — the
// shape the arm must REPORT rather than read as "no row tracks this branch".
var errReclaimLedgerUnreadable = errors.New("ledger unavailable (fixture)")

// reclaimOwnerSeam is an injected ledger answer: the arm's questions about
// ownership are answered from rows a test holds, never from a real ledger
// (Rule 32 — no test may reach the operator's profile).
func reclaimOwnerSeam(rows ...ArchiveReclaimOwnerRow) archiveReclaimOwnerLookup {
	return func(string) ([]ArchiveReclaimOwnerRow, error) { return rows, nil }
}

// reclaimTaskGroundCase is ONE cell of the table: the two facts as fixture
// inputs, and what the arm must answer.
type reclaimTaskGroundCase struct {
	name string
	// ownerRows is the ledger's answer about the branch (nil = no row tracks it).
	ownerRows []ArchiveReclaimOwnerRow
	// hold registers a real `git worktree add` on the branch.
	hold bool
	// wantDeleted — the head ref is removed (the residue case).
	wantDeleted bool
	// wantKind is the ArchiveReclaimProtected* kind the row must carry ("" when
	// the row is a deletion).
	wantKind string
	// wantIn are the FACTS the refusal sentence must name.
	wantIn []string
}

// TestArchiveReclaimTaskGroundRefusalIsAFactNotAName drives the four-quadrant
// table. Every cell reads the refs back with git, so the arm's own report is
// never mistaken for the ref state; and every refusal is asserted to NAME its
// fact (the task id with its status, or the worktree path), because a refusal
// that does not say what it saw is a wall rather than a gate (Rule 15).
func TestArchiveReclaimTaskGroundRefusalIsAFactNotAName(t *testing.T) {
	const (
		branchUnowned = TaskGroundBranchPrefix + "01a0f44c2d0f7a82bde8f76445daa30d"
		branchLiveRow = TaskGroundBranchPrefix + "01a12c9f5a1b2c3d4e5f60718293a4b5"
		branchHeld    = TaskGroundBranchPrefix + "01a12cbe00000000000000000000abcd"
		branchBoth    = TaskGroundBranchPrefix + "01a12cbe00000000000000000000abce"
	)
	const liveTaskID = "01a12c9f5a1b2c3d4e5f60718293a4b5"
	cases := []reclaimTaskGroundCase{
		{
			// THE RED CELL: no row, no worktree — the archive decision is the only
			// thing this ref still carries, and the decision says "never merged".
			// The old code refused it by prefix, so this cell failed before the
			// fix with the head ref still present.
			name:        "unowned and unheld — the residue the archive decision was about is reclaimed",
			wantDeleted: true,
		},
		{
			name:      "a non-terminal ledger row keeps the ground and the refusal names the row",
			ownerRows: []ArchiveReclaimOwnerRow{{TaskID: liveTaskID, Status: TaskStatusInProgress}},
			wantKind:  ArchiveReclaimProtectedLiveRow,
			wantIn:    []string{liveTaskID, TaskStatusInProgress},
		},
		{
			name:     "a registered worktree keeps the ground and the refusal names the tree",
			hold:     true,
			wantKind: ArchiveReclaimProtectedWorktree,
		},
		{
			name:      "both facts hold — the worktree guard answers first",
			hold:      true,
			ownerRows: []ArchiveReclaimOwnerRow{{TaskID: liveTaskID, Status: TaskStatusInProgress}},
			wantKind:  ArchiveReclaimProtectedWorktree,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := rsTestRepo(t)
			var branch string
			switch {
			case tc.ownerRows != nil && tc.hold:
				branch = branchBoth
			case tc.hold:
				branch = branchHeld
			case tc.ownerRows != nil:
				branch = branchLiveRow
			default:
				branch = branchUnowned
			}
			rsMakeBranch(t, repo, branch)
			rsGit(t, repo, true, "tag", archiveBranchPrefix+branch, branch)
			tip := reclaimHeadTip(t, repo, branch)
			if tip == "" || reclaimTagExists(t, repo, branch) != tip {
				t.Fatalf("fixture: %s must exist with a lightweight archive tag at its tip", branch)
			}
			worktree := ""
			if tc.hold {
				worktree = filepath.Join(t.TempDir(), "wt")
				rsGit(t, repo, true, "worktree", "add", "-q", worktree, branch)
			}

			res := ArchiveReclaimDecidedRefsWith(repo, reclaimOwnerSeam(tc.ownerRows...))
			if res.Err != "" {
				t.Fatalf("reclaim scan failed: %s", res.Err)
			}
			if len(res.Rows) != 1 {
				t.Fatalf("want exactly one row for %s, got %+v", branch, res.Rows)
			}
			row := res.Rows[0]
			if row.Branch != branch {
				t.Fatalf("row names %q, want %q", row.Branch, branch)
			}
			if tc.wantDeleted {
				if row.Verdict != ArchiveReclaimDeleted || res.Deleted != 1 || res.Skipped != 0 {
					t.Fatalf("want a deletion, got %+v (%+v)", row, res)
				}
				if after := reclaimHeadTip(t, repo, branch); after != "" {
					t.Fatalf("%s%s still resolves to %s — the residue was not reclaimed",
						refsHeadsPrefix, branch, after)
				}
				if got := reclaimTagExists(t, repo, branch); got != tip {
					t.Fatalf("the archive tag moved/vanished: %q, want %q", got, tip)
				}
				if !DecidedRefReclaimed(repo, branch) {
					t.Fatal("DecidedRefReclaimed must report the state the arm just produced")
				}
				return
			}
			if row.Verdict != ArchiveReclaimSkipped || res.Deleted != 0 {
				t.Fatalf("want a refusal, got %+v (%+v)", row, res)
			}
			if row.Kind != tc.wantKind {
				t.Fatalf("refusal kind = %q, want %q (%s)", row.Kind, tc.wantKind, row.Reason)
			}
			for _, want := range tc.wantIn {
				if !strings.Contains(row.Reason, want) {
					t.Fatalf("the refusal must name %q, got: %s", want, row.Reason)
				}
			}
			if tc.hold {
				if !strings.Contains(row.Reason, worktree) {
					t.Fatalf("the worktree refusal must name the tree %q, got: %s", worktree, row.Reason)
				}
			}
			// The ground is UNTOUCHED: both refs, and the branch still checked out
			// where the peer left it (Rule 29).
			if got := reclaimHeadTip(t, repo, branch); got != tip {
				t.Fatalf("%s%s moved: %q → %q", refsHeadsPrefix, branch, tip, got)
			}
			if got := reclaimTagExists(t, repo, branch); got != tip {
				t.Fatalf("the archive tag moved: %q, want %q", got, tip)
			}
		})
	}
}

// TestArchiveReclaimStillRefusesTheIntegrationBranchByName keeps the ONE name
// test that survives: master and main are the head everything integrates INTO,
// and no fact about them makes them a reclaim candidate.
func TestArchiveReclaimStillRefusesTheIntegrationBranchByName(t *testing.T) {
	repo := rsTestRepo(t)
	rsGit(t, repo, true, "branch", mainBranchName)
	before := map[string]string{}
	for _, b := range []string{masterBranchName, mainBranchName} {
		rsGit(t, repo, true, "tag", "-f", archiveBranchPrefix+b, b)
		before[b] = reclaimHeadTip(t, repo, b)
	}
	res := ArchiveReclaimDecidedRefsWith(repo, reclaimOwnerSeam())
	if res.Err != "" {
		t.Fatalf("reclaim scan failed: %s", res.Err)
	}
	if res.Deleted != 0 {
		t.Fatalf("the arm deleted %d protected ref(s): %+v", res.Deleted, res.Rows)
	}
	for _, b := range []string{masterBranchName, mainBranchName} {
		if got := reclaimHeadTip(t, repo, b); got != before[b] {
			t.Fatalf("%s was moved: %s → %q", b, before[b], got)
		}
	}
	var kinds []string
	for _, r := range res.Rows {
		kinds = append(kinds, r.Kind)
	}
	if strings.Join(kinds, ",") != ArchiveReclaimProtectedIntegrationBranch+","+ArchiveReclaimProtectedIntegrationBranch {
		t.Fatalf("both rows must carry the integration-branch kind, got %v (%+v)", kinds, res.Rows)
	}
}

// TestArchiveReclaimSeamIsConsultedAndItsDefaultIsTheProductionLookup pins the
// two halves of the seam: the scan ASKS it, and the shipped entry point hands it
// the ledger reader (deliverable ② — a seam wired to nothing is a decoration).
func TestArchiveReclaimSeamIsConsultedAndItsDefaultIsTheProductionLookup(t *testing.T) {
	var asked []string
	repo := rsTestRepo(t)
	const branch = TaskGroundBranchPrefix + "01a0f44c2d0f7a82bde8f76445daa30e"
	rsMakeBranch(t, repo, branch)
	rsGit(t, repo, true, "tag", archiveBranchPrefix+branch, branch)

	res := ArchiveReclaimDecidedRefsWith(repo, func(b string) ([]ArchiveReclaimOwnerRow, error) {
		asked = append(asked, b)
		return nil, nil
	})
	if res.Err != "" || len(asked) != 1 || asked[0] != branch {
		t.Fatalf("the ledger seam must be asked about %q exactly once, got %v (err=%q)", branch, asked, res.Err)
	}
	if res.Deleted != 1 {
		t.Fatalf("with no owning row the arm must reclaim, got %+v", res.Rows)
	}
	// An ordinary feat/… branch never reaches the ledger at all: the question is
	// asked only where a ground's name makes ownership the thing to check.
	featRepo := rsTestRepo(t)
	rsMakeBranch(t, featRepo, "feat/ordinary")
	asked = nil
	ArchiveReclaimDecidedRefsWith(featRepo, func(string) ([]ArchiveReclaimOwnerRow, error) {
		asked = append(asked, "unexpected")
		return nil, nil
	})
	if len(asked) != 0 {
		t.Fatalf("a feat/… branch must not consult the ledger, got %v", asked)
	}
	// The production entry point really uses the ledger reader.
	if reflect.ValueOf(archiveReclaimOwnerLookupDefault()).Pointer() !=
		reflect.ValueOf(archiveReclaimOwnersFromLedger).Pointer() {
		t.Fatal("ArchiveReclaimDecidedRefs must default to archiveReclaimOwnersFromLedger")
	}
	if got := reflect.ValueOf(archiveReclaimOwnerLookupDefault()).Pointer(); got == 0 {
		t.Fatal("the default owner lookup must not be nil")
	}
}

// TestArchiveReclaimStopsWhenTheLedgerCannotBeRead is the Rule 15 half: a ledger
// that could not be read is NOT "nothing tracks this branch". Reading it that
// way would delete a running deliverable's ground, so the arm stops and reports.
func TestArchiveReclaimStopsWhenTheLedgerCannotBeRead(t *testing.T) {
	repo := rsTestRepo(t)
	const branch = TaskGroundBranchPrefix + "01a0f44c2d0f7a82bde8f76445daa30f"
	rsMakeBranch(t, repo, branch)
	rsGit(t, repo, true, "tag", archiveBranchPrefix+branch, branch)
	tip := reclaimHeadTip(t, repo, branch)

	res := ArchiveReclaimDecidedRefsWith(repo, func(string) ([]ArchiveReclaimOwnerRow, error) {
		return nil, errReclaimLedgerUnreadable
	})
	if res.Err == "" || !strings.Contains(res.Err, errReclaimLedgerUnreadable.Error()) {
		t.Fatalf("an unreadable ledger must be REPORTED, got err=%q", res.Err)
	}
	if res.Deleted != 0 || len(res.Rows) != 0 {
		t.Fatalf("nothing may be decided from an unreadable ledger, got %+v", res)
	}
	if got := reclaimHeadTip(t, repo, branch); got != tip {
		t.Fatalf("the ref was moved on an unreadable ledger: %q", got)
	}
	// A nil seam is a programming error, never a silent "no owner anywhere".
	if null := ArchiveReclaimDecidedRefsWith(repo, nil); null.Err == "" {
		t.Fatal("a nil owner lookup must be refused by name, not treated as an empty ledger")
	}
}

// TestArchiveReclaimRowLivenessIsDefaultDeny pins the status reading the
// task-ground guard applies: only the statuses that name a FINISHED or
// decided-against row release a ground. Everything else — including the statuses
// the ledger's own set carries that this package classifies nowhere (reopened),
// and a row with no status recorded at all — KEEPS the ref (Rule 15 default-deny:
// a status nobody reasoned about is not a permission to delete a ref).
func TestArchiveReclaimRowLivenessIsDefaultDeny(t *testing.T) {
	released := []string{TaskStatusCompleted, TaskStatusVerified, TaskStatusFailed,
		TaskStatusCancelled, TaskStatusSuperseded}
	for _, s := range released {
		if archiveReclaimRowLiveness(s) {
			t.Errorf("status %q names a concluded row and must release the ground", s)
		}
	}
	kept := []string{TaskStatusPending, TaskStatusClaimed, TaskStatusInProgress,
		TaskStatusReopened, "", "a status nobody has invented yet"}
	for _, s := range kept {
		if !archiveReclaimRowLiveness(s) {
			t.Errorf("status %q must KEEP the ground", s)
		}
	}
	// The one-row pick names the row whose sentence is printed, and it agrees
	// with the predicate above.
	rows := []ArchiveReclaimOwnerRow{
		{TaskID: "t-done", Status: TaskStatusCompleted},
		{TaskID: "t-live", Status: TaskStatusInProgress},
	}
	if live, ok := archiveReclaimLiveRow(rows); !ok || live.TaskID != "t-live" {
		t.Fatalf("the live row must be the one named, got %+v ok=%v", live, ok)
	}
	if _, ok := archiveReclaimLiveRow([]ArchiveReclaimOwnerRow{{TaskID: "t-done", Status: TaskStatusCancelled}}); ok {
		t.Fatal("a cancelled row must not be reported as live")
	}
}

// TestArchiveReclaimProductionPathReadsTheLedger is the END-TO-END half of
// deliverable ②, with the production entry point (no seam): a `task/<id>` branch
// whose ledger row is in_progress is refused BY THE LEDGER'S ANSWER, and a
// `task/<id>` branch no row tracks is reclaimed. The ledger is a fresh temp
// database pinned through the test scaffold, never the operator's (Rule 32).
func TestArchiveReclaimProductionPathReadsTheLedger(t *testing.T) {
	ctx := context.Background()
	repo := rsTestRepo(t)
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)

	const (
		trackedID     = "01a12cbe00000000000000000000ee01"
		trackedBranch = TaskGroundBranchPrefix + trackedID
		orphanBranch  = TaskGroundBranchPrefix + "01a12cbe00000000000000000000ee02"
	)
	seedArchiveOwnerRow(t, ctx, trackedID, TaskStatusInProgress, trackedBranch)
	for _, b := range []string{trackedBranch, orphanBranch} {
		rsMakeBranch(t, repo, b)
		rsGit(t, repo, true, "tag", archiveBranchPrefix+b, b)
	}

	res := ArchiveReclaimDecidedRefs(repo)
	if res.Err != "" {
		t.Fatalf("reclaim scan failed: %s", res.Err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("want one row per tagged task ground, got %+v", res.Rows)
	}
	byBranch := map[string]ArchiveReclaimRow{}
	for _, r := range res.Rows {
		byBranch[r.Branch] = r
	}
	tracked := byBranch[trackedBranch]
	if tracked.Verdict != ArchiveReclaimSkipped || tracked.Kind != ArchiveReclaimProtectedLiveRow {
		t.Fatalf("%s must be kept by its ledger row, got %+v", trackedBranch, tracked)
	}
	if !strings.Contains(tracked.Reason, trackedID) || !strings.Contains(tracked.Reason, TaskStatusInProgress) {
		t.Fatalf("the refusal must name the row and its status, got: %s", tracked.Reason)
	}
	if reclaimHeadTip(t, repo, trackedBranch) == "" {
		t.Fatalf("%s was reclaimed despite a live ledger row", trackedBranch)
	}
	orphan := byBranch[orphanBranch]
	if orphan.Verdict != ArchiveReclaimDeleted {
		t.Fatalf("%s is tracked by no row and must be reclaimed, got %+v", orphanBranch, orphan)
	}
	if reclaimHeadTip(t, repo, orphanBranch) != "" {
		t.Fatalf("%s survived although nothing tracks it", orphanBranch)
	}
	if reclaimTagExists(t, repo, orphanBranch) == "" {
		t.Fatalf("%s lost its archive tag — the durable half must stay", orphanBranch)
	}
}

// TestArchiveReclaimGroundNameComesFromTheSharedPrefix pins that the namespace
// this arm asks about is the SHARED spelling (Rule 13/14): a rename of
// TaskGroundBranchPrefix would otherwise leave the reclaim arm asking about a
// namespace nobody mints branches in.
func TestArchiveReclaimGroundNameComesFromTheSharedPrefix(t *testing.T) {
	if !archiveReclaimIsTaskGround(TaskGroundBranchPrefix + "01a12cbe") {
		t.Fatal("a minted ground branch must be recognised as a task ground")
	}
	if archiveReclaimIsTaskGround("feat/not-a-ground") {
		t.Fatal("a feature branch must not be read as a task ground")
	}
	if !archiveReclaimProtected(masterBranchName) || !archiveReclaimProtected(mainBranchName) {
		t.Fatal("master/main must stay protected by name")
	}
	if archiveReclaimProtected(TaskGroundBranchPrefix + "01a12cbe") {
		t.Fatal("a task ground must NOT be protected by name any more — that was the defect")
	}
}
