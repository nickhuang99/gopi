// archive_landed_content_test.go — the CONTENT arm of the archived-branch
// landing verdict, pinned on the shapes PATH PRESENCE is blind to.
//
// THE MEASURED DEFECT THIS FILE REPAIRS (epic 01a17e06, 2026-10-28). The path
// instrument answers "which ADDS does the integration ref lack?" and says
// nothing about MODIFIED lines. So a branch whose entire change is a different
// VALUE IN A LINE THE INTEGRATION BRANCH ALREADY CARRIES adds no path at all,
// the missing set comes back empty, and the verdict reads LANDED for a
// deliverable whose work is on the integration ref NOWHERE. Two measured shapes
// below, both of them ordinary:
//
//	feat/modified-only          the integration branch never got the change at all
//	feat/other-change-same-path the integration branch got a DIFFERENT value in
//	                            the very same line
//
// Against the pre-repair tree BOTH of those read LANDED (see the red proof
// recorded with this change: `git checkout HEAD -- internal/tools/archive_landed.go`
// then this file → the two cases fail with "verdict = landed, want missing").
//
// THE OTHER DIRECTION IS EQUALLY LOAD-BEARING, and it is the reason the arm
// could not simply be made stricter: a REBASE lands the same work under a new
// hash, so the branch is not an ancestor and its patch-id does not match, and a
// verdict of MISSING there manufactures a re-dispatch for work that is present.
// feat/rebase-same-content is that case, and it must read LANDED.
//
// THE FOURTH CASE KEEPS THE TWO ARMS SEPARATED: feat/adds-path introduces a file
// the integration ref has never had. The path arm decides it (MISSING, with the
// path named), and the content arm is never asked — a verdict that named a LINE
// there would be evidence of the wrong instrument answering.
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture's own spellings, named once (Rule 1/13): the test asserts against
// the branch names and the marker values its assertions quote, so a rename
// anywhere is one edit rather than a silent mismatch.
const (
	alcFlagPath = "flags.go"

	alcBranchModifiedOnly   = "feat/modified-only"
	alcBranchRebasedContent = "feat/rebase-same-content"
	alcBranchOtherSamePath  = "feat/other-change-same-path"
	alcBranchAddsPath       = "feat/adds-path"

	// The integration ref's own final value, plus the two values only the
	// branches carry. Distinct on purpose: a shared value would let a
	// destination-side coincidence pass as a content match.
	alcValueSeed            = "seed"
	alcValueLandedOnMaster  = "landed-value"
	alcValueOnlyOnModified  = "only-on-modified-only"
	alcValueOnlyOnOtherPath = "only-on-other-change"

	alcAddedPathName = "only-this-branch.txt"

	alcFlagsTemplate = "package alc\n\nconst flag = %q\n"
)

// alcWriteFlag sets the fixture's one source file to carry value.
func alcWriteFlag(t *testing.T, repo, value string) {
	t.Helper()
	body := strings.Join([]string{"package alc", "", "const flag = \"" + value + "\""}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(repo, alcFlagPath), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// alcLandFixtures builds one repo carrying the four shapes above and returns it
// plus the tip of each branch. Every branch is cut from the SAME seed commit, so
// the only difference between them is what each one changed.
func alcLandFixtures(t *testing.T) (repo string, tips map[string]string) {
	t.Helper()
	repo = archTestRepo(t)
	commit := func(msg string) string {
		t.Helper()
		archGit(t, repo, "add", "-A")
		archGit(t, repo, "commit", "-q", "-m", msg)
		return strings.TrimSpace(archGit(t, repo, "rev-parse", "HEAD"))
	}
	cut := func(branch, value string) string {
		t.Helper()
		archGit(t, repo, "checkout", "-q", "-b", branch, masterBranchName)
		alcWriteFlag(t, repo, value)
		return commit("change the flag to " + value + " on " + branch)
	}

	alcWriteFlag(t, repo, alcValueSeed)
	commit("seed")

	tips = map[string]string{}
	// ① the integration branch NEVER gets this change.
	tips[alcBranchModifiedOnly] = cut(alcBranchModifiedOnly, alcValueOnlyOnModified)
	// ② the integration branch gets the SAME change under a different commit —
	// the rebase shape, which must stay LANDED.
	tips[alcBranchRebasedContent] = cut(alcBranchRebasedContent, alcValueLandedOnMaster)
	// ③ the integration branch gets a DIFFERENT value in the very same line.
	tips[alcBranchOtherSamePath] = cut(alcBranchOtherSamePath, alcValueOnlyOnOtherPath)
	// ④ an ADDED path — the arm path presence already decides.
	archGit(t, repo, "checkout", "-q", "-b", alcBranchAddsPath, masterBranchName)
	if err := os.WriteFile(filepath.Join(repo, alcAddedPathName), []byte("added\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tips[alcBranchAddsPath] = commit("add " + alcAddedPathName + " on " + alcBranchAddsPath)

	// master's own commit carrying ②'s value — a DIFFERENT commit, which is what
	// makes the rebase shape real rather than an ancestor relation.
	archGit(t, repo, "checkout", "-q", masterBranchName)
	alcWriteFlag(t, repo, alcValueLandedOnMaster)
	commit("master carries the same change under its own commit")

	for _, b := range []string{alcBranchModifiedOnly, alcBranchRebasedContent, alcBranchOtherSamePath, alcBranchAddsPath} {
		archGit(t, repo, "tag", archiveBranchPrefix+b, b)
	}
	return repo, tips
}

// alcClassify runs the SHIPPED predicate over the fixture, through the shipped
// instrument pair — never a hand-built probe, so the test measures what the
// report face measures.
func alcClassify(t *testing.T, repo string) map[string]ArchiveLandVerdict {
	t.Helper()
	entries, err := ArchiveEntriesForRepo(repo)
	if err != nil {
		t.Fatalf("ArchiveEntriesForRepo: %v", err)
	}
	ref := refsHeadsPrefix + masterBranchName
	probe := ArchiveLandProbe(repo, ref, ArchiveTipsForLandCheck(repo, ref, entries))
	return ArchiveLandFunc(repo, ref, probe, entries)
}

// TestArchiveLandContent_ModifiedOnlyBranchIsNotLanded — the defect this change
// repairs, in its two measured shapes.
func TestArchiveLandContent_ModifiedOnlyBranchIsNotLanded(t *testing.T) {
	repo, _ := alcLandFixtures(t)
	got := alcClassify(t, repo)

	for _, tc := range []struct{ branch, why string }{
		{alcBranchModifiedOnly, "the integration branch never got the change at all"},
		{alcBranchOtherSamePath, "the integration branch carries a DIFFERENT value in the same line"},
	} {
		v := got[tc.branch]
		if v.Verdict != ArchiveLandMissing {
			t.Errorf("%s verdict = %q, want %q — %s (missing paths %v, missing line %q from %q)",
				tc.branch, v.Verdict, ArchiveLandMissing, tc.why, v.MissingPaths, v.MissingLines, v.MissingLinePath)
			continue
		}
		// The verdict must carry COUNTER-EVIDENCE, not just the word: the path
		// whose work is absent and the line that proves it. A MISSING with an
		// empty evidence set is the accusation without the proof.
		if v.MissingLinePath != alcFlagPath {
			t.Errorf("%s verdict names path %q, want %q", tc.branch, v.MissingLinePath, alcFlagPath)
		}
		if len(v.MissingLines) == 0 {
			t.Errorf("%s verdict carries no missing line — the content arm's whole job is to say WHICH line", tc.branch)
		}
		// The path arm must NOT have decided this: no path was added, so a
		// non-empty MissingPaths would mean the wrong instrument spoke.
		if len(v.MissingPaths) != 0 {
			t.Errorf("%s verdict names missing PATHS %v — no path was added, so the content arm is the one that must decide",
				tc.branch, v.MissingPaths)
		}
	}
}

// TestArchiveLandContent_RebasedBranchStaysLanded — the direction that stops the
// repair from turning "different hash" into an accusation.
func TestArchiveLandContent_RebasedBranchStaysLanded(t *testing.T) {
	repo, tips := alcLandFixtures(t)
	got := alcClassify(t, repo)

	// First: prove the fixture really IS the rebase shape — the branch tip is not
	// an ancestor of master, so the verdict below cannot be the ancestry arm's.
	ref := refsHeadsPrefix + masterBranchName
	if _, err := repoSweepGit(repo, archiveLandMergeBaseVerb, archiveLandIsAncestorFlag,
		tips[alcBranchRebasedContent], ref); err == nil {
		t.Fatalf("%s is an ancestor of %s — the fixture collapsed into the trivial case and proves nothing",
			alcBranchRebasedContent, ref)
	}
	v := got[alcBranchRebasedContent]
	if v.Verdict != ArchiveLandLanded {
		t.Fatalf("%s verdict = %q, want %q — the same content landed under a different commit is a DELIVERY, "+
			"and calling it missing manufactures a re-dispatch for work that is present (missing line %q from %q)",
			alcBranchRebasedContent, v.Verdict, ArchiveLandLanded, v.MissingLines, v.MissingLinePath)
	}
	if len(v.MissingPaths) != 0 || len(v.MissingLines) != 0 {
		t.Errorf("a landed verdict must carry no counter-evidence, got paths %v lines %v", v.MissingPaths, v.MissingLines)
	}
}

// TestArchiveLandContent_AddedPathIsStillThePathArm — the two arms must not
// collapse into one: an ADDED file the integration ref lacks is decided by path
// presence, and the verdict names the path rather than a line.
func TestArchiveLandContent_AddedPathIsStillThePathArm(t *testing.T) {
	repo, _ := alcLandFixtures(t)
	v := alcClassify(t, repo)[alcBranchAddsPath]
	if v.Verdict != ArchiveLandMissing {
		t.Fatalf("%s verdict = %q, want %q", alcBranchAddsPath, v.Verdict, ArchiveLandMissing)
	}
	if len(v.MissingPaths) != 1 || v.MissingPaths[0] != alcAddedPathName {
		t.Errorf("the missing verdict must name the added path %q, got %v", alcAddedPathName, v.MissingPaths)
	}
	if len(v.MissingLines) != 0 {
		t.Errorf("a path-arm verdict must not quote a line (%q) — the content arm was not the one that decided", v.MissingLines)
	}
}

// TestArchiveLandContent_UnanswerableReadIsNeverMissing — arm ③'s honest empty
// answer. An unrelated history has no merge base to range over, so the content
// question cannot be asked; the verdict must then be UNKNOWN rather than either
// terminal answer. Measured shape: a branch whose history shares no commit with
// the integration ref (the shape `gopi merge` calls unrelated-history).
func TestArchiveLandContent_UnanswerableReadIsNeverMissing(t *testing.T) {
	repo := archTestRepo(t)
	archGit(t, repo, "checkout", "-q", "--orphan", "unrelated")
	archGit(t, repo, "commit", "-q", "--allow-empty", "-m", "unrelated root")
	tip := strings.TrimSpace(archGit(t, repo, "rev-parse", "HEAD"))
	archGit(t, repo, "checkout", "-q", masterBranchName)

	// A probe that answers "no added path is absent" forces the decision to arm ③,
	// whose merge-base read cannot succeed across unrelated histories.
	probe := func(string) []string { return nil }
	v := ArchiveLandClassify(repo, refsHeadsPrefix+masterBranchName, tip, probe)
	if v.Verdict != ArchiveLandUnknown {
		t.Fatalf("an unanswerable content read = %q, want %q — neither terminal answer is measured here "+
			"(missing paths %v, missing lines %v)", v.Verdict, ArchiveLandUnknown, v.MissingPaths, v.MissingLines)
	}
}

// TestArchiveLandContent_NormaliserIsTheDeliveryProofs — the arm must read the
// CONTENT through the ONE normaliser (Rule 14), so a rebase's prose drift (a
// changed comment, a blank line, a `measured-at:` row) can never manufacture a
// MISSING. This pins the reuse by behaviour rather than by import: the same
// added-line set, once with each of those three shapes injected into the
// destination, must still be judged present.
func TestArchiveLandContent_NormaliserIsTheDeliveryProofs(t *testing.T) {
	added := []string{"const flag = \"x\""}
	dest := strings.Join([]string{
		"// a comment the rebase reworded",
		"",
		"const flag = \"x\"",
		"// measured-at: 2026-10-28T00:00:00Z",
	}, "\n")
	if missing := deliveryProofLinesMissingAt(dest, added); len(missing) != 0 {
		t.Fatalf("the shared containment rule reported %v missing from a destination that carries the work line — "+
			"the arm is not reading through deliveryProofNormalizeLines", missing)
	}
}

// TestArchiveLandContent_VerdictRendersItsEvidence — the verdict's proof must
// reach the reader. A NOT LANDED whose evidence lives only in a struct field
// prints as a bare accusation on every face, which is how a modified-only
// branch used to print as "landed": the reader had nothing to check it against.
func TestArchiveLandContent_VerdictRendersItsEvidence(t *testing.T) {
	v := ArchiveLandVerdict{
		Verdict:         ArchiveLandMissing,
		Ref:             refsHeadsPrefix + masterBranchName,
		MissingLinePath: alcFlagPath,
		MissingLines:    []string{`const flag = "only-on-modified-only"`},
	}
	out := renderArchiveLandEvidence(v, archiveLandEvidenceIndent)
	if !strings.Contains(out, alcFlagPath) {
		t.Errorf("the rendered evidence does not name the path %q:\n%s", alcFlagPath, out)
	}
	if !strings.Contains(out, "only-on-modified-only") {
		t.Errorf("the rendered evidence does not quote the missing line:\n%s", out)
	}
	// A verdict with no counter-evidence renders NOTHING, so no caller has to
	// pre-test the verdict string before printing.
	if out := renderArchiveLandEvidence(ArchiveLandVerdict{Verdict: ArchiveLandLanded}, archiveLandEvidenceIndent); out != "" {
		t.Errorf("a landed verdict rendered evidence %q, want the empty string", out)
	}
}
