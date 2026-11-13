// archive_landed_test.go — the landing instrument and the classified backlog.
//
// The red proof for these tests is an ASSERTION failure, not a compile failure:
// every one of them is written against the OLD tree's own `git` and the old
// tree's own exported backlog reader, so checking them out on the pre-change
// tree fails on the measured behaviour (the old reader reports the owned-and-
// decided rows only; nothing there asks the landing question at all). The
// campaign that produced the instrument is in the file header of
// archive_landed.go; these tests pin the three arms that review demanded.
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// archLandFixtures builds one repo carrying the three tip shapes the landing
// instrument must separate, and returns the repo path:
//
//	feat/landed  — merged into master: the tip IS an ancestor (landed, trivially)
//	feat/rebased — NOT an ancestor, but every path it added IS on master: the
//	               same work landed under a different commit (a rebase/squash),
//	               i.e. the FALSE POSITIVE the path instrument exists to avoid
//	feat/ghost   — NOT an ancestor, and the path it added is absent from master:
//	               the buried deliverable
//
// Plus fix/reasoned-ghost — the same shape as feat/ghost with a RECORDED reason,
// which the classification must still count as not-landed (a reason can say "this
// was a mistake" and still be wrong about the work being reachable).
func archLandFixtures(t *testing.T) (repo string) {
	t.Helper()
	repo = archTestRepo(t)

	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string) string {
		t.Helper()
		archGit(t, repo, "add", "-A")
		archGit(t, repo, "commit", "-q", "-m", msg)
		return strings.TrimSpace(archGit(t, repo, "rev-parse", "HEAD"))
	}

	// feat/landed — an ordinary merge into master.
	archGit(t, repo, "checkout", "-q", "-b", "feat/landed")
	write("landed.txt", "landed\n")
	commit("add landed.txt")
	archGit(t, repo, "checkout", "-q", masterBranchName)
	archGit(t, repo, "merge", "-q", "--no-ff", "-m", "merge feat/landed", "feat/landed")

	// feat/rebased — the path lands on master through a DIFFERENT commit.
	archGit(t, repo, "checkout", "-q", "-b", "feat/rebased")
	write("rebased.txt", "same content\n")
	commit("add rebased.txt (branch side)")
	archGit(t, repo, "checkout", "-q", masterBranchName)
	write("rebased.txt", "same content\n")
	commit("add rebased.txt (master side, different commit)")

	// feat/ghost and fix/reasoned-ghost — the work is nowhere.
	for _, b := range []string{"feat/ghost", "fix/reasoned-ghost"} {
		archGit(t, repo, "checkout", "-q", "-b", b, masterBranchName)
		write(strings.ReplaceAll(b, "/", "-")+".txt", "nowhere\n")
		commit("add " + b)
	}
	archGit(t, repo, "checkout", "-q", masterBranchName)

	for _, b := range []string{"feat/landed", "feat/rebased", "feat/ghost", "fix/reasoned-ghost"} {
		archGit(t, repo, "tag", archiveBranchPrefix+b, b)
	}
	return repo
}

// TestArchiveLandSeparatesLandedFromBuried — the instrument's three arms, on one
// repo, in one call.
func TestArchiveLandSeparatesLandedFromBuried(t *testing.T) {
	archLedgerFile(t)
	repo := archLandFixtures(t)

	entries, err := ArchiveEntriesForRepo(repo)
	if err != nil {
		t.Fatalf("ArchiveEntriesForRepo: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %d (%+v), want 4", len(entries), entries)
	}
	ref := IntegrationClaimTargetRef(repo)
	if strings.TrimSpace(ref) == "" {
		t.Fatal("the integration ref resolved to nothing — the landing question would be unasked")
	}
	probe := ArchiveLandProbe(repo, ref, ArchiveTipsForLandCheck(repo, ref, entries))
	got := ArchiveLandFunc(repo, ref, probe, entries)

	for branch, want := range map[string]string{
		"feat/landed":        ArchiveLandLanded,
		"feat/rebased":       ArchiveLandLanded, // THE false positive: not an ancestor, work present
		"feat/ghost":         ArchiveLandMissing,
		"fix/reasoned-ghost": ArchiveLandMissing,
	} {
		if v := got[branch]; v.Verdict != want {
			t.Errorf("%s verdict = %q, want %q (ref %s, missing %v)", branch, v.Verdict, want, v.Ref, v.MissingPaths)
		}
	}

	// The NOT-LANDED verdict carries counter-evidence, not just a word: the path
	// the branch added and the ref that lacks it.
	if m := got["feat/ghost"].MissingPaths; len(m) != 1 || m[0] != "feat-ghost.txt" {
		t.Errorf("the missing verdict must name the added path master lacks, got %v", m)
	}
	// And the LANDED-under-a-new-hash verdict must NOT claim a path is missing.
	if m := got["feat/rebased"].MissingPaths; len(m) != 0 {
		t.Errorf("a rebase-landed branch reported missing paths %v — the path instrument's whole job is to avoid this", m)
	}
}

// TestArchiveLandUnmeasuredIsNeverMissing — a question that could not be asked
// answers UNKNOWN, never "missing". The direction matters: an Unknown folded into
// Missing manufactures a re-dispatch for a branch nobody measured (the false
// accusation), while Missing folded into Unknown hides the defect (Rule 15's
// silent-green direction).
func TestArchiveLandUnmeasuredIsNeverMissing(t *testing.T) {
	repo := archTestRepo(t)
	// A tip that is not a valid object name: `merge-base --is-ancestor` fails, and
	// a nil probe means the added-path read was never taken.
	if v := ArchiveLandClassify(repo, refsHeadsPrefix+masterBranchName, "0000000000000000000000000000000000000000", nil); v.Verdict != ArchiveLandUnknown {
		t.Errorf("unmeasurable tip = %q, want %q", v.Verdict, ArchiveLandUnknown)
	}
	// No resolvable integration ref: also unmeasured.
	if v := ArchiveLandClassify(repo, "", strings.TrimSpace(archGit(t, repo, "rev-parse", "HEAD")), nil); v.Verdict != ArchiveLandUnknown {
		t.Errorf("no integration ref = %q, want %q", v.Verdict, ArchiveLandUnknown)
	}
	// And the bulk instrument reports the failure as nil rather than as an empty
	// (clean) answer: an unreadable repo has no "nothing is missing".
	if p := ArchiveLandProbe(filepath.Join(repo, "does-not-exist"), refsHeadsPrefix+masterBranchName,
		[]string{strings.TrimSpace(archGit(t, repo, "rev-parse", "HEAD"))}); p != nil {
		t.Error("an unreadable repo returned a probe — the failure would render as 'nothing is missing'")
	}
}

// TestArchiveBacklogClassifiesEveryBranchAndNamesTheBuriedSet — the face the
// epic asks for: EVERY archived branch classified, and the decided-but-not-landed
// set pulled out as the thing a re-dispatch acts on.
//
// It also pins the two asymmetry controls the review demanded: an entry with NO
// owner row is classified (it is reported by NOTHING today), and a RECORDED
// reason never removes a row from the buried set (the reason can be about the
// DECISION, while the landing axis is about the WORK).
func TestArchiveBacklogClassifiesEveryBranchAndNamesTheBuriedSet(t *testing.T) {
	archLedgerFile(t)
	repo := archLandFixtures(t)
	// Owners: landed+rebased+ghost are FINISHED rows; reasoned-ghost carries a
	// recorded reason; fix/unowned gets NO row at all.
	archSeedRow(t, "t-landed", TaskStatusCompleted, "feat/landed")
	archSeedRow(t, "t-rebased", TaskStatusVerified, "feat/rebased")
	archSeedRow(t, "t-ghost", TaskStatusCompleted, "feat/ghost")
	archSeedRow(t, "t-reasoned", TaskStatusCompleted, "fix/reasoned-ghost")
	if _, err := ArchiveTagReasoned(repo, "fix/reasoned-ghost", ArchiveReasonSuperseded, "intent landed elsewhere"); err != nil {
		t.Fatalf("reasoned fixture: %v", err)
	}
	archGit(t, repo, "tag", archiveBranchPrefix+"fix/unowned", "feat/ghost")

	cls, err := ClassifyArchiveBacklog(repo)
	if err != nil {
		t.Fatalf("ClassifyArchiveBacklog: %v", err)
	}
	if cls.LandErr != "" {
		t.Fatalf("landing question failed: %s", cls.LandErr)
	}
	// EVERY branch is in the table — including the one no ledger row names.
	if len(cls.Rows) != 5 {
		t.Fatalf("classified %d branch(es), want all 5 (rows: %+v)", len(cls.Rows), cls.Rows)
	}
	byBranch := map[string]ArchiveRowClass{}
	for _, r := range cls.Rows {
		byBranch[r.Branch] = r
	}
	u, ok := byBranch["fix/unowned"]
	if !ok {
		t.Fatal("the unowned branch is missing from the classification — it is exactly the corpus the narrowed backlog never reported")
	}
	if !u.OwnerAbsent() || u.OwnerCell() != archiveRowNoOwnerLabel {
		t.Errorf("unowned row = %+v, want an explicit owner-absent cell", u)
	}

	// The re-dispatchable set: decided owner + not-landed. feat/landed (landed)
	// and feat/rebased (landed under a new hash) are excluded — calling them
	// buried is the false accusation the path instrument exists to prevent.
	buried := cls.BuriedUnlanded()
	got := map[string]bool{}
	for _, r := range buried {
		got[r.Branch] = true
	}
	if len(buried) != 2 || !got["feat/ghost"] || !got["fix/reasoned-ghost"] {
		t.Fatalf("buried set = %v, want exactly {feat/ghost, fix/reasoned-ghost}", got)
	}
	if got["feat/landed"] || got["feat/rebased"] {
		t.Errorf("a landed branch was reported as a buried deliverable: %v", got)
	}
	// The reason-less count is the figure the operator used to grep out by hand.
	if n := cls.ReasonLessCount(); n != 4 {
		t.Errorf("reason-less count = %d, want 4 (only fix/reasoned-ghost carries a record)", n)
	}

	// The report renders the set AND the one command that re-arms a branch.
	report, n, err := ArchiveBacklogClassReport(repo)
	if err != nil {
		t.Fatalf("ArchiveBacklogClassReport: %v", err)
	}
	if n != len(buried) {
		t.Errorf("report finding count = %d, want %d", n, len(buried))
	}
	for _, want := range []string{
		"feat/ghost", ArchiveLandMissingLabel, "feat-ghost.txt",
		"fix/unowned", ArchiveReasonMissingLabel,
		"git tag -d archive/feat/ghost",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("classification report missing %q:\n%s", want, report)
		}
	}
}

// TestArchiveBacklogClassificationSeesWhatTheNarrowedBacklogCannot — the
// regression the epic rests on, stated as the DIFFERENCE between the two faces
// on ONE repo: the shipped backlog check reports the decided-and-reason-less
// rows, this classification additionally accounts for the branches no ledger row
// owns.
func TestArchiveBacklogClassificationSeesWhatTheNarrowedBacklogCannot(t *testing.T) {
	archLedgerFile(t)
	repo := archLandFixtures(t)
	archSeedRow(t, "t-ghost", TaskStatusCompleted, "feat/ghost")
	archGit(t, repo, "tag", archiveBranchPrefix+"docs/no-owner", "feat/ghost")

	narrow, err := ArchiveReasonlessBacklog(repo)
	if err != nil {
		t.Fatalf("ArchiveReasonlessBacklog: %v", err)
	}
	cls, err := ClassifyArchiveBacklog(repo)
	if err != nil {
		t.Fatalf("ClassifyArchiveBacklog: %v", err)
	}
	if len(cls.Rows) <= len(narrow) {
		t.Fatalf("classification accounted for %d rows, the narrowed backlog %d — the unowned corpus is still invisible",
			len(cls.Rows), len(narrow))
	}
	for _, r := range narrow {
		if r.Branch != "feat/ghost" {
			t.Errorf("the narrowed backlog changed shape: reported %s", r.Branch)
		}
	}
	seen := false
	for _, r := range cls.Rows {
		if r.Branch == "docs/no-owner" {
			seen = true
			if !r.OwnerAbsent() {
				t.Errorf("docs/no-owner should have no owner row, got %+v", r)
			}
		}
	}
	if !seen {
		t.Error("the classification did not account for the unowned branch")
	}
}
