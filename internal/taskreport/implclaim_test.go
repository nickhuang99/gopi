package taskreport

// implclaim_test.go — the TEXT half of the master-claim check: the sentences a
// report uses to say "the integration branch already has this", and the commit
// each one names. The motivating run (session 01a0f3de) wrote exactly such a
// sentence about the tip of an ARCHIVED branch and closed exit=0.
//
// The two directions are both pinned here: the claim SHAPES are found (so the
// seal can re-measure them), and the TRUTHFUL shapes — "not an ancestor",
// "never merged" — are left alone (a false record on an honest report is the
// failure this check must not make).

import "testing"

const implClaimTestBranch = "master"

func TestImplClaimsFindsTheMotivatingClaimAndItsCommit(t *testing.T) {
	body := `# Task Report: salvage review

## Completed
- the listing feature is a genuine gap

## Verification
- ` + "`internal/shot/x11list.go` is already implemented on `master`" + `
  (the archived tip ` + "`e1701ac8`" + ` is how master does it)
- build: OK

## Files
- internal/shot/x11list.go
`
	got := ImplClaimsIn(body, implClaimTestBranch)
	if len(got.Claims) != 1 {
		t.Fatalf("claims = %d (%+v), want exactly the one implementation claim", len(got.Claims), got.Claims)
	}
	c := got.Claims[0]
	if c.Commit != "e1701ac8" {
		t.Fatalf("commit = %q, want e1701ac8 — the commit the claim rests on", c.Commit)
	}
	if c.Branch != implClaimTestBranch {
		t.Fatalf("branch = %q, want %q", c.Branch, implClaimTestBranch)
	}
	if !contains(c.Sentence, "already") {
		t.Errorf("sentence = %q, want the claim quoted verbatim", c.Sentence)
	}
	if got.Section != VerificationHeading() {
		t.Errorf("section = %q, want the verification heading", got.Section)
	}
}

// The evidence section wins when a report declares one — the same three-way
// choice GateClaimVerdict makes, so the two records cannot disagree about where
// a report's results live.
func TestImplClaimsPrefersTheEvidenceSection(t *testing.T) {
	body := `# Task Report: x

## Verification
- ` + "`deadbeef1`" + ` already landed on ` + implClaimTestBranch + `

` + SectionEvidence + `
- nothing to add
`
	got := ImplClaimsIn(body, implClaimTestBranch)
	if got.Section != SectionEvidence {
		t.Fatalf("section = %q, want the evidence heading", got.Section)
	}
	if len(got.Claims) != 0 {
		t.Fatalf("claims = %+v, want none: the evidence section declares no claim", got.Claims)
	}
}

// The TRUTHFUL shapes must stay expressible: a report that CORRECTLY says the
// work never landed is not a finding.
func TestImplClaimsLeavesTheTruthfulShapesAlone(t *testing.T) {
	for _, sentence := range []string{
		"`e1701ac8` is NOT an ancestor of `master` (merge-base --is-ancestor exit=1)",
		"the branch was never merged into master",
		"`master` does not contain `e1701ac8`",
		"the file is missing on master",
		"e1701ac8 不在 master 上",
	} {
		body := "## Verification\n- " + sentence + "\n"
		if got := ImplClaimsIn(body, implClaimTestBranch); len(got.Claims) != 0 {
			t.Errorf("sentence %q was flagged as an implementation claim: %+v", sentence, got.Claims)
		}
	}
}

// A claim that names NO commit is still a claim — and it is the one nobody can
// re-measure, which is why the record must carry it (Commit == "").
func TestImplClaimsRecordsAClaimThatNamesNoCommit(t *testing.T) {
	body := SectionEvidence + "\n- the feature already exists on master (see the audit)\n"
	got := ImplClaimsIn(body, implClaimTestBranch)
	if len(got.Claims) != 1 {
		t.Fatalf("claims = %+v, want one nameless claim", got.Claims)
	}
	if got.Claims[0].Commit != "" {
		t.Fatalf("commit = %q, want empty — the sentence names no revision", got.Claims[0].Commit)
	}
}

// The commit extractor's SHAPE test: a revision needs both a letter and a digit,
// which is what keeps English words ("defaced"), bare counts ("12345678") and
// hash-like prose out of the record.
func TestImplClaimCommitShape(t *testing.T) {
	cases := []struct {
		sentence string
		want     string
	}{
		{"already implemented on master per `e1701ac8`", "e1701ac8"},
		{"already implemented on master per e1701ac8", "e1701ac8"},
		{"already on master — see `491b5ba7`", "491b5ba7"},
		{"already on master per `12345678`", ""},         // digits only: a count
		{"already on master per `deadbeef`", ""},         // letters only: a word
		{"already on master per `deadbeefdeadbeef`", ""}, // still letters only
		{"already on master per `a1b2c3d4e5f6`", "a1b2c3d4e5f6"},
		{"already on master (no revision named)", ""},
	}
	for _, c := range cases {
		body := "## Verification\n- " + c.sentence + "\n"
		got := ImplClaimsIn(body, implClaimTestBranch)
		if len(got.Claims) != 1 {
			t.Fatalf("sentence %q: claims = %+v, want one", c.sentence, got.Claims)
		}
		if got.Claims[0].Commit != c.want {
			t.Errorf("sentence %q: commit = %q, want %q", c.sentence, got.Claims[0].Commit, c.want)
		}
	}
}

// The branch name is a PARAMETER (Rule 14): the leaf never mirrors the literal,
// so a different integration branch works with no edit here.
func TestImplClaimsUsesTheBranchItIsGiven(t *testing.T) {
	body := "## Verification\n- the feature already landed on trunk per `a1b2c3d4`\n"
	if got := ImplClaimsIn(body, "trunk"); len(got.Claims) != 1 {
		t.Fatalf("claims = %+v, want one for branch trunk", got.Claims)
	}
	if got := ImplClaimsIn(body, implClaimTestBranch); len(got.Claims) != 0 {
		t.Fatalf("claims = %+v, want none: the sentence names trunk, not %s", got.Claims, implClaimTestBranch)
	}
	if got := ImplClaimsIn(body, ""); len(got.Claims) != 0 {
		t.Fatalf("an empty branch name must yield no claims, got %+v", got.Claims)
	}
}

// contains is a local spelling of strings.Contains, kept so this test file needs
// no import beyond testing (the leaf's own contract: stdlib only, and the test
// reads as the shape it pins).
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
