package taskreport

// unlandedclaim_test.go — the MIRROR detector's polarity proof, and the record
// of the case it was built from (row 01a12c7b: a briefing rendered from a
// snapshot asserted a completed row's fix was unlanded while the fix was an
// ancestor of the integration branch).
//
// There is no git here and there must not be: this file decides only WHICH
// SENTENCES are claims. Whether the claim is still true is an exit status, and
// it is taken by internal/tools (task_briefing_premise_test.go, real git).

import "testing"

const (
	unlandedTestBranch = "master"
	unlandedTestRev    = "9867ed461"
)

func TestUnlandedClaimsIn_FindsTheStalePremise(t *testing.T) {
	body := "# Epic\n\n" +
		"The row 01a12c4a is completed but its fix (" + unlandedTestRev + ") never landed on " +
		unlandedTestBranch + " — re-implement it in this run's worktree.\n"
	v := UnlandedClaimsIn(body, unlandedTestBranch)
	if len(v.Claims) != 1 {
		t.Fatalf("claims = %v, want exactly the stale premise", v.Claims)
	}
	if v.Claims[0].Commit != unlandedTestRev {
		t.Errorf("the sentence names %s; the detector read %q", unlandedTestRev, v.Claims[0].Commit)
	}
	if v.Claims[0].Sentence == "" || v.Claims[0].Branch != unlandedTestBranch {
		t.Errorf("a claim must carry the author's own words and the branch it named; got %+v", v.Claims[0])
	}
}

func TestUnlandedClaimsIn_LeavesTheTruthfulShapeAlone(t *testing.T) {
	// The truthful shape this detector must NEVER stamp: the sentence is about a
	// report's own run, and the work really is absent. Both forms are the
	// negation of the claim class ("not an ancestor" / "已落地" as a marker).
	for name, body := range map[string]string{
		"an explicit landed marker": "# Report\n\n- " + unlandedTestRev + " has already landed on " + unlandedTestBranch + "\n",
		"a Chinese landed marker":   "# Report\n\n- " + unlandedTestRev + " 已落地到 " + unlandedTestBranch + "\n",
		"no branch named":           "# Report\n\n- " + unlandedTestRev + " never landed anywhere\n",
	} {
		if got := UnlandedClaimsIn(body, unlandedTestBranch).Claims; len(got) != 0 {
			t.Errorf("%s: read as a stale premise: %v", name, got)
		}
	}
}

func TestUnlandedClaimsIn_NamesNoRevisionIsStillAClaim(t *testing.T) {
	body := "# Epic\n\nThe read-path fix is missing from " + unlandedTestBranch + " — land it.\n"
	v := UnlandedClaimsIn(body, unlandedTestBranch)
	if len(v.Claims) != 1 {
		t.Fatalf("claims = %v, want the premise that names no revision", v.Claims)
	}
	if v.Claims[0].Commit != "" {
		t.Errorf("the sentence names no commit-shaped token; got %q", v.Claims[0].Commit)
	}
}

func TestUnlandedClaimsIn_EmptyBranchIsNoVerdict(t *testing.T) {
	if got := UnlandedClaimsIn("never landed on master", "").Claims; len(got) != 0 {
		t.Errorf("with no branch named by the caller there is nothing to read; got %v", got)
	}
}

func TestUnlandedClaimSentenceExcerpt_BoundsAQuotedSentence(t *testing.T) {
	long := ""
	for i := 0; i < unlandedClaimSentenceMaxRunes+50; i++ {
		long += "x"
	}
	got := UnlandedClaimSentenceExcerpt(long)
	if []rune(got)[len([]rune(got))-1] != []rune(unlandedClaimEllipsis)[0] {
		t.Fatalf("a sentence past the cap must be marked as truncated; got %q", got)
	}
	if len([]rune(got)) != unlandedClaimSentenceMaxRunes+1 {
		t.Fatalf("excerpt length = %d runes, want %d + the ellipsis", len([]rune(got)), unlandedClaimSentenceMaxRunes)
	}
}
