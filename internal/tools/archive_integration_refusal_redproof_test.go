package tools

// THE RED PROOF of the "a refusal must precede the write" repair (measured
// defect 01a12f0d, 2026-10-12), kept as a shipping test on purpose (Rule 5/10:
// every bug fix carries a minimal repro).
//
// It is written against the PRE-EXISTING surface ONLY — `ArchiveBranch`, the
// reason vocabulary constants and the fixture helpers — so the very same file
// COMPILES on the code before the fix and on the code after it. That is what
// makes the transition a measurement rather than an assertion:
//
//	PRE-FIX  : `ArchiveBranch(repo, "master", …)` returns Recorded=true, writes
//	           refs/tags/archive/master AND refs/gopi/archive-reason/master, and
//	           the very same invocation's reclaim leg then prints
//	           "branch master is the integration branch — never a reclaim
//	           candidate". Both refs exist ⇒ these assertions FAIL.
//	POST-FIX : the gate refuses by name before the first write ⇒ they pass.
//
// The CONTROL arm is the third case (a `feat/…` branch no row owns), so a
// refusal-everything implementation cannot pass this test.

import (
	"strings"
	"testing"
)

// TestArchiveBranchRedProofRefusesTheIntegrationBranchBeforeAnyWrite is the
// minimal repro: the branch the whole system integrates INTO must be refused,
// and NOTHING may be written for it.
//
// The harm is not the self-contradicting output. refs/tags/archive/<branch> is
// the project's decided-exclusion namespace, and every merge path reads it as
// "a loser was decided here" (PROJECT.md Rule 9) — so a refused reclaim that wrote
// the tag first marked the INTEGRATION HEAD as decided-against.
func TestArchiveBranchRedProofRefusesTheIntegrationBranchBeforeAnyWrite(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/ordinary", mainBranchName)

	// The predicate is the RECLAIM ARM's own guard — asserted here so the two
	// faces cannot drift into two definitions (Rule 13): if this ever stops
	// holding, the gate below is asking a different question than the sweep.
	if !archiveReclaimProtected(masterBranchName) || !archiveReclaimProtected(mainBranchName) {
		t.Fatal("fixture/gate disagreement: the integration branches are not the reclaim guard's protected set")
	}
	if archiveReclaimProtected("feat/ordinary") {
		t.Fatal("fixture/gate disagreement: an ordinary branch must NOT be reachable by the name guard")
	}

	for _, branch := range []string{masterBranchName, mainBranchName} {
		tip := strings.TrimSpace(archGit(t, repo, "rev-parse", "refs/heads/"+branch))
		res, err := ArchiveBranch(repo, branch, ArchiveReasonHandMistake, "probe", false)
		if err != nil {
			t.Fatalf("ArchiveBranch(%s): %v", branch, err)
		}
		if res.Verdict != ArchiveVerdictRefused || res.Recorded || res.Tag != "" {
			t.Fatalf("%s: verdict=%q recorded=%v tag=%q — the integration branch must be REFUSED with nothing written",
				branch, res.Verdict, res.Recorded, res.Tag)
		}
		// The refusal names the guard's own measurement. (The exact sentence is
		// pinned by TestArchiveIntegrationRefusalReusesTheReclaimGuardsSentence,
		// which is a POST-fix test — it names the exported shared text, so it is
		// deliberately kept OUT of this file, which must compile on both sides.)
		if !strings.Contains(res.Refusal, "integration branch") {
			t.Errorf("%s: refusal does not name the integration branch:\n%s", branch, res.Refusal)
		}
		if !strings.Contains(res.Refusal, "NOTHING WAS WRITTEN") {
			t.Errorf("%s: refusal does not say the write was skipped:\n%s", branch, res.Refusal)
		}
		// (a) no archive tag …
		if archRefResolves(t, repo, archiveTagRefPrefix+branch) {
			t.Errorf("%s: refs/tags/%s%s EXISTS — a refused archive wrote the decided-exclusion ref",
				branch, archiveBranchPrefix, branch)
		}
		// (b) … no reason ref …
		if archRefResolves(t, repo, archiveReasonRefPrefix+branch) {
			t.Errorf("%s: %s%s EXISTS — a refused archive wrote the why", branch, archiveReasonRefPrefix, branch)
		}
		// (c) … and the BRANCH itself is untouched (the refused invocation must
		// be inert, not merely quiet about what it did).
		if got := strings.TrimSpace(archGit(t, repo, "rev-parse", "refs/heads/"+branch)); got != tip {
			t.Errorf("%s: the branch ref moved (%s → %s) on a refused archive", branch, tip, got)
		}
	}
	// The namespace is EMPTY — read the way the merge paths read it, not one ref
	// at a time: a tag written under any other spelling would still be a
	// decision.
	if out := strings.TrimSpace(archGit(t, repo, "for-each-ref", "--format=%(refname)", archiveTagRefPrefix)); out != "" {
		t.Fatalf("the archive namespace is not empty after the refusals:\n%s", out)
	}
	if out := strings.TrimSpace(archGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/gopi/")); out != "" {
		t.Fatalf("the reason namespace is not empty after the refusals:\n%s", out)
	}

	// ── CONTROL: the same writer still archives an ordinary branch ──────────
	// Without this arm, "refuse everything" would satisfy every assertion above.
	ctrlTip := strings.TrimSpace(archGit(t, repo, "rev-parse", "feat/ordinary"))
	res, err := ArchiveBranch(repo, "feat/ordinary", ArchiveReasonHandMistake, "probe", false)
	if err != nil {
		t.Fatalf("ArchiveBranch(feat/ordinary): %v", err)
	}
	if !res.Recorded {
		t.Fatalf("an ordinary unowned branch must still archive; refusal: %s", res.Refusal)
	}
	if got := strings.TrimSpace(archGit(t, repo, "rev-parse", archiveTagRefPrefix+"feat/ordinary")); got != ctrlTip {
		t.Fatalf("archive/feat/ordinary = %q, want the branch tip %q", got, ctrlTip)
	}
	if got := ArchiveTagReason(archReasonRecord(t, repo, "feat/ordinary")); got != ArchiveReasonUnowned {
		t.Fatalf("control reason = %q, want %q (unowned is forced, not a caller's story)", got, ArchiveReasonUnowned)
	}
}

// TestArchiveBranchIntegrationRefusalIsNotOverridable pins the half of the
// refusal a caller might otherwise assume is a flag away: `--override-completed`
// clears the OWNER gate (a row the ledger calls finished), and it must NOT clear
// the NAME guard. The predicate is a branch name; a flag cannot argue with one.
func TestArchiveBranchIntegrationRefusalIsNotOverridable(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, mainBranchName)
	res, err := ArchiveBranch(repo, masterBranchName, ArchiveReasonHandMistake, "probe", true)
	if err != nil {
		t.Fatalf("ArchiveBranch(ack): %v", err)
	}
	if res.Verdict != ArchiveVerdictRefused || res.Recorded {
		t.Fatalf("acknowledgement cleared the integration-branch refusal (verdict=%q recorded=%v)",
			res.Verdict, res.Recorded)
	}
	if archRefResolves(t, repo, archiveTagRefPrefix+masterBranchName) {
		t.Fatal("the acknowledged invocation wrote the tag anyway")
	}
}
