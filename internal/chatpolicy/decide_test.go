package chatpolicy

import "testing"

// table builds a Policy the way the parser would, from rule lines — so a
// decision test exercises the SAME Rule values the parser produces (Line
// numbers included) rather than hand-built structs that could drift from it.
func table(t *testing.T, lines ...string) *Policy {
	t.Helper()
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	pol, err := Parse("test-fence", fence(body))
	if err != nil {
		t.Fatalf("Parse(%v): %v", lines, err)
	}
	return pol
}

// words tokenises through the ONE tokenizer's contract: the rule table consumes
// WORDS, so the tests hand it words. The tokenizer itself is internal/tools'
// (its own tests live there); these tests are about the MATCHER.
func words(s ...string) []string { return s }

// THE FOUR DECISION CASES the item names.

// 1. LONGEST PREFIX WINS: `allow git status` beats `prompt git` for `git status`.
func TestDecideLongestPrefixWins(t *testing.T) {
	pol := table(t, "prompt git", "allow git status")
	m := pol.DecideWords(words("git", "status"))
	if !m.Matched {
		t.Fatal("Matched = false for a command two rules apply to")
	}
	if m.Decision != DecisionAllow {
		t.Errorf("Decision = %v, want allow — the LONGER prefix (git status) is the more specific sentence", m.Decision)
	}
	if got := m.Rule.PatternString(); got != "git status" {
		t.Errorf("deciding rule = %q, want %q", got, "git status")
	}
	// The shorter rule still answers the commands the longer one does not cover.
	if m := pol.DecideWords(words("git", "push")); m.Decision != DecisionPrompt {
		t.Errorf("`git push` Decision = %v, want prompt (only `prompt git` applies)", m.Decision)
	}
}

// 2. EQUAL LENGTH IS DECIDED BY FILE ORDER — first written wins, in BOTH
// directions (so the test cannot pass by accident of which decision sorts
// higher).
func TestDecideEqualLengthFirstWrittenWins(t *testing.T) {
	firstAllow := table(t, "allow git push", "forbidden git push")
	if m := firstAllow.DecideWords(words("git", "push")); m.Decision != DecisionAllow {
		t.Errorf("allow-then-forbidden: Decision = %v, want allow (the first line wins the tie)", m.Decision)
	}
	firstForbidden := table(t, "forbidden git push", "allow git push")
	if m := firstForbidden.DecideWords(words("git", "push")); m.Decision != DecisionForbidden {
		t.Errorf("forbidden-then-allow: Decision = %v, want forbidden (the first line wins the tie)", m.Decision)
	}
	// The deciding RULE is the first one too, so a report names the right line.
	if firstForbidden.DecideWords(words("git", "push")).Rule.Line != firstForbidden.Rules[0].Line {
		t.Error("the reported rule is not the one the file order chose")
	}
}

// 3. THE TRAILING "*" MATCHES ANY REMAINING WORDS — and none at all — while it
// constrains nothing, so ANY more specific rule beats it.
func TestDecideTrailingWildcard(t *testing.T) {
	pol := table(t, "prompt *", "forbidden git push")
	for _, cmd := range [][]string{
		words("git", "push"),
		words("git", "push", "--force", "--no-verify"),
		words("rm", "-rf", "/"),
		words("ls"),
	} {
		m := pol.DecideWords(cmd)
		if !m.Matched {
			t.Errorf("%v: Matched = false — the catch-all must match every command", cmd)
			continue
		}
		if cmd[0] == "git" && cmd[1] == "push" {
			if m.Decision != DecisionForbidden {
				t.Errorf("%v: Decision = %v, want forbidden — the specific rule beats `prompt *`", cmd, m.Decision)
			}
			continue
		}
		if m.Decision != DecisionPrompt {
			t.Errorf("%v: Decision = %v, want prompt (the catch-all)", cmd, m.Decision)
		}
	}
	// A wildcard rule with NO remaining words still matches (`*` may match none).
	bare := table(t, "allow git status *")
	if m := bare.DecideWords(words("git", "status")); !m.Matched || m.Decision != DecisionAllow {
		t.Errorf("`allow git status *` on `git status` = %+v, want a match with allow (the wildcard may match nothing)", m)
	}
	// ONE word is not a prefix match for a two-word pattern.
	short := table(t, "allow git status")
	if m := short.DecideWords(words("git")); m.Matched {
		t.Errorf("a two-word rule matched ONE word (%+v) — a prefix match cannot invent words", m)
	}
}

// 4. NO RULE MATCHED means Matched=false and Decision MEANS NOTHING: the caller
// must branch on Matched, and the zero Decision must not be read as "allow".
func TestDecideUnmatchedReportsNoOpinion(t *testing.T) {
	pol := table(t, "allow git status")
	m := pol.DecideWords(words("rm", "-rf", "/"))
	if m.Matched {
		t.Fatalf("Matched = true for a command no rule covers: %+v", m)
	}
	if m.String() != "no rule matched" {
		t.Errorf("String() = %q, want the explicit no-opinion line", m.String())
	}
	// An EMPTY table (and a nil one) has no opinion either — never a silent allow.
	if m := (&Policy{}).DecideWords(words("git", "status")); m.Matched {
		t.Errorf("an empty table matched %+v", m)
	}
	var nilPol *Policy
	if m := nilPol.DecideWords(words("git", "status")); m.Matched {
		t.Errorf("a nil table matched %+v", m)
	}
}

// A COMPOUND line takes the MOST RESTRICTIVE clause answer, and is Matched only
// when EVERY clause matched.
func TestDecideClausesTakeTheMostRestrictiveAnswer(t *testing.T) {
	pol := table(t, "allow git status", "forbidden git push", "prompt *")
	m := pol.DecideClauses([][]string{
		words("git", "status"),
		words("git", "push", "--force"),
	})
	if !m.Matched {
		t.Fatal("Matched = false for a compound line every clause covers")
	}
	if m.Decision != DecisionForbidden {
		t.Errorf("Decision = %v, want forbidden — the line is as restrictive as its worst clause", m.Decision)
	}
	if len(m.Clauses) != 2 {
		t.Fatalf("Clauses = %d, want one answer per clause", len(m.Clauses))
	}
	if m.Clauses[0].Decision != DecisionAllow || m.Clauses[1].Decision != DecisionForbidden {
		t.Errorf("per-clause answers = %v/%v, want allow/forbidden", m.Clauses[0].Decision, m.Clauses[1].Decision)
	}

	// ONE unexplained clause makes the WHOLE line unexplained: the posture, not
	// this table, must answer.
	narrow := table(t, "allow git status")
	partial := narrow.DecideClauses([][]string{
		words("git", "status"),
		words("rm", "-rf", "/"),
	})
	if partial.Matched {
		t.Error("Matched = true although one clause has no rule — `git status; rm -rf /` must not be judged by its first clause")
	}
}

// String renders a decision for a log line, and an out-of-range value is NAMED
// rather than printed as "allow".
func TestDecisionStringNamesUnknownValues(t *testing.T) {
	if got := DecisionAllow.String(); got != "allow" {
		t.Errorf("DecisionAllow.String() = %q", got)
	}
	if got := DecisionPrompt.String(); got != "prompt" {
		t.Errorf("DecisionPrompt.String() = %q", got)
	}
	if got := DecisionForbidden.String(); got != "forbidden" {
		t.Errorf("DecisionForbidden.String() = %q", got)
	}
	if got := Decision(42).String(); got != "decision(42)" {
		t.Errorf("Decision(42).String() = %q — an unknown value must be named, never printed as a decision", got)
	}
}
