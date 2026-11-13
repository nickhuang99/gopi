package tools

// The POST-fix half of the integration-branch refusal: the properties that can
// only be asserted against the symbols the repair INTRODUCED, kept in their own
// file so the red proof next door stays compilable on the pre-fix tree (the
// discipline archive_ref_reclaim_taskground_redproof_test.go documents: a red
// proof that does not COMPILE on the old code measures a build error, not the
// defect).

import (
	"strings"
	"testing"
)

// TestArchiveIntegrationRefusalReusesTheReclaimGuardsSentence is the "ONE
// definition" half (Rule 13): the veto the archive GATE applies and the veto the
// reclaim SWEEP prints are the same predicate and the same measurement, so a
// reader who saw `--reclaim` skip master and a reader who saw `task archive
// master` refuse it cannot be reading two different facts.
//
// Asserted three ways, so neither half can drift alone:
//
//	① the predicate is the reclaim arm's own (archiveReclaimProtected);
//	② the sweep's guard-④ row and the exported shared text are one sentence;
//	③ the gate's refusal EMBEDS that sentence and adds the two things only an
//	   archive caller needs — that nothing was written, and that no flag clears it.
func TestArchiveIntegrationRefusalReusesTheReclaimGuardsSentence(t *testing.T) {
	// ① the ONE predicate, asked directly.
	if !archiveReclaimProtected(masterBranchName) || !archiveReclaimProtected(mainBranchName) {
		t.Fatal("the integration branches must be the reclaim guard's protected set")
	}
	if archiveReclaimProtected("feat/ordinary") || archiveReclaimProtected(TaskGroundBranchPrefix+"01a12f0d") {
		t.Fatal("the name guard must reach ONLY the integration branches (a task ground is protected by FACTS, not by its name)")
	}

	// ② the sweep's row and the exported text are the same sentence.
	for _, branch := range []string{masterBranchName, mainBranchName} {
		kind, reason := archiveReclaimProtectedStatement(branch)
		if kind != ArchiveReclaimProtectedIntegrationBranch {
			t.Fatalf("%s: sweep row kind = %q, want %q", branch, kind, ArchiveReclaimProtectedIntegrationBranch)
		}
		if reason != ArchiveProtectedRefusalText(branch) {
			t.Fatalf("%s: the sweep's guard row and the shared text disagree:\n  row:  %s\n  text: %s",
				branch, reason, ArchiveProtectedRefusalText(branch))
		}
	}

	// ③ the gate's refusal carries it, plus the two archive-specific facts.
	archLedgerFile(t)
	repo := archTestRepo(t, mainBranchName)
	res, err := ArchiveBranch(repo, masterBranchName, ArchiveReasonHandMistake, "probe", false)
	if err != nil {
		t.Fatalf("ArchiveBranch: %v", err)
	}
	if res.Verdict != ArchiveVerdictRefused {
		t.Fatalf("verdict = %q, want %q", res.Verdict, ArchiveVerdictRefused)
	}
	if !strings.Contains(res.Refusal, ArchiveProtectedRefusalText(masterBranchName)) {
		t.Fatalf("the gate's refusal does not embed the guard's own sentence:\n%s", res.Refusal)
	}
	if !strings.Contains(res.Refusal, "NOTHING WAS WRITTEN") {
		t.Fatalf("the refusal must state that no ref was written:\n%s", res.Refusal)
	}
	if !strings.Contains(res.Refusal, archiveTagRefPrefix+masterBranchName) ||
		!strings.Contains(res.Refusal, archiveReasonRefPrefix+masterBranchName) {
		t.Fatalf("the refusal must NAME the two refs that were not written (Rule 15: the operator reads the address):\n%s", res.Refusal)
	}
}
