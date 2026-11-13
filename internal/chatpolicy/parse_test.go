package chatpolicy

import (
	"errors"
	"strings"
	"testing"
)

// fence builds a document with one chat-policy fence around body.
func fence(body string) string {
	return "# project context\n\nSome prose that is NOT a rule.\n\n```chat-policy\n" + body + "```\n\nmore prose\n"
}

// THE SEVEN PARSE CASES the item names, one test per shape. They are separate
// tests, not table rows, so a failure names the CASE rather than an index.

// 1. A legal fence: rules in file order, line numbers pointing INSIDE the
// document, prose outside the fence never read as a rule.
func TestParseLegalFenceKeepsFileOrderAndLineNumbers(t *testing.T) {
	text := fence("allow git status\nprompt git\nforbidden git push\n")
	pol, err := Parse("GOPI.md", text)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !pol.Fenced {
		t.Error("Fenced = false for a document that carries a ```chat-policy fence")
	}
	if pol.Len() != 3 {
		t.Fatalf("Len = %d, want 3 (rules: %v)", pol.Len(), pol.Rules)
	}
	want := []struct {
		d      Decision
		last   string
		lineNo int
	}{
		{DecisionAllow, "status", 6},
		{DecisionPrompt, "git", 7},
		{DecisionForbidden, "push", 8},
	}
	for i, w := range want {
		got := pol.Rules[i]
		if got.Decision != w.d {
			t.Errorf("rule %d decision = %v, want %v", i, got.Decision, w.d)
		}
		if got.Pattern[len(got.Pattern)-1] != w.last {
			t.Errorf("rule %d pattern = %v, want it to end in %q", i, got.Pattern, w.last)
		}
		if got.Line != w.lineNo {
			t.Errorf("rule %d Line = %d, want %d (the 1-based line INSIDE the document)", i, got.Line, w.lineNo)
		}
		if got.Source != "GOPI.md" {
			t.Errorf("rule %d Source = %q, want the source name", i, got.Source)
		}
	}
}

// 2. An illegal decision word: a ParseError naming the WORD and the line, never
// a silently dropped rule.
func TestParseIllegalDecisionNamesWordAndLine(t *testing.T) {
	_, err := Parse("GOPI.md", fence("allow git status\npermited git push\n"))
	if err == nil {
		t.Fatal("Parse accepted an unknown decision word")
	}
	var perr *ParseError
	if !errors.As(err, &perr) {
		t.Fatalf("error %v is not a *ParseError — a caller cannot name the line", err)
	}
	if perr.Line != 7 {
		t.Errorf("ParseError.Line = %d, want 7", perr.Line)
	}
	if !strings.Contains(err.Error(), `"permited"`) {
		t.Errorf("error %q does not name the offending word", err.Error())
	}
	if !strings.Contains(err.Error(), "GOPI.md:7:") {
		t.Errorf("error %q is not shaped source:line: msg", err.Error())
	}
	for _, w := range decisionWords {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not offer the accepted word %q", err.Error(), w)
		}
	}
}

// 3. A "*" that is NOT the last word is refused: a mid-pattern wildcard would
// make the rule a pattern nobody can predict.
func TestParseRefusesWildcardThatIsNotLast(t *testing.T) {
	_, err := Parse("GOPI.md", fence("allow git * status\n"))
	if err == nil {
		t.Fatal("Parse accepted a wildcard in the middle of a rule")
	}
	if !strings.Contains(err.Error(), "may only be the LAST word") {
		t.Errorf("error %q does not explain the wildcard rule", err.Error())
	}
	// The LAST position is legal, with no other rule present.
	pol, err := Parse("GOPI.md", fence("forbidden rm *\n"))
	if err != nil {
		t.Fatalf("Parse refused a trailing wildcard: %v", err)
	}
	if pol.Len() != 1 || pol.Rules[0].Pattern[1] != wildcardWord {
		t.Errorf("rules = %v, want one rule whose pattern ends in %q", pol.Rules, wildcardWord)
	}
}

// 4. Comments and blank lines are skipped; a rule that FOLLOWS a comment is
// still read, and the comment itself contributes nothing.
func TestParseSkipsCommentsAndBlankLines(t *testing.T) {
	pol, err := Parse("GOPI.md", fence("\n# this is not a rule\n   \nallow git status  \n\n# trailing comment\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pol.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (comments and blanks are not rules): %v", pol.Len(), pol.Rules)
	}
	if pol.Rules[0].Pattern[0] != "git" || pol.Rules[0].Line != 9 {
		t.Errorf("rule = %+v, want the git rule on line 9", pol.Rules[0])
	}
}

// 5. An EMPTY fence is a valid empty table — and so is an empty BODY beside an
// existing fence: absence of rules is a result, not an error.
func TestParseEmptyFenceIsAValidEmptyTable(t *testing.T) {
	pol, err := Parse("GOPI.md", fence(""))
	if err != nil {
		t.Fatalf("Parse of an empty fence: %v", err)
	}
	if !pol.Fenced {
		t.Error("Fenced = false for an empty but PRESENT fence")
	}
	if pol.Len() != 0 {
		t.Errorf("Len = %d, want 0", pol.Len())
	}
	// A document with NO fence at all is likewise valid, and NOT fenced — the
	// difference the startup line reports.
	bare, err := Parse("GOPI.md", "# just prose, no fence\n")
	if err != nil {
		t.Fatalf("Parse of a fence-less document: %v", err)
	}
	if bare.Fenced || bare.Len() != 0 {
		t.Errorf("fence-less document = {Fenced:%v Len:%d}, want {false 0}", bare.Fenced, bare.Len())
	}
}

// 6. DUPLICATE rules are legal and KEPT: two identical lines decide identically,
// and dropping one would only make the line numbers lie.
func TestParseKeepsDuplicateRules(t *testing.T) {
	pol, err := Parse("GOPI.md", fence("allow git status\nallow git status\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pol.Len() != 2 {
		t.Fatalf("Len = %d, want 2 — duplicates are kept so Line numbers stay true", pol.Len())
	}
	if pol.Rules[0].Line == pol.Rules[1].Line {
		t.Error("both duplicates report the same Line — the second line was not read as its own rule")
	}
}

// 7. AN ERROR LINE'S NUMBER: a rule with no command words is refused with ITS
// line, and an UNCLOSED fence is refused naming the end of the document (a
// missing ``` would otherwise turn the rest of the file into silent rules).
func TestParseErrorLineNumbersAreTheOperatorsLineNumbers(t *testing.T) {
	_, err := Parse("GOPI.md", fence("allow git status\nallow\n"))
	var perr *ParseError
	if !errors.As(err, &perr) {
		t.Fatalf("error %v is not a *ParseError", err)
	}
	if perr.Line != 7 {
		t.Errorf("ParseError.Line = %d, want 7 (the wordless rule on line 7)", perr.Line)
	}
	if !strings.Contains(err.Error(), "names no command words") {
		t.Errorf("error %q does not say what is wrong", err.Error())
	}

	unclosed := "# ctx\n\n```chat-policy\nallow git status\n"
	_, err = Parse("GOPI.md", unclosed)
	if !errors.As(err, &perr) {
		t.Fatalf("an unclosed fence was not refused: %v", err)
	}
	if perr.Line != 5 {
		t.Errorf("unclosed-fence Line = %d, want 5 (one past the last line)", perr.Line)
	}
	if !strings.Contains(err.Error(), "never closed") {
		t.Errorf("error %q does not name the unfolding", err.Error())
	}
}

// The built-in table is DATA parsed by the SAME parser, and it names no verb.
func TestDefaultsAreParsedByTheSameParserAndNameNoVerb(t *testing.T) {
	def, err := Defaults()
	if err != nil {
		t.Fatalf("Defaults: %v (the built-in table is malformed — a programming error)", err)
	}
	if def.Len() != 1 {
		t.Fatalf("Defaults().Len = %d, want 1 (the catch-all)", def.Len())
	}
	r := def.Rules[0]
	if r.Decision != DecisionPrompt {
		t.Errorf("default decision = %v, want prompt (an unmatched line is a question)", r.Decision)
	}
	if len(r.Pattern) != 1 || r.Pattern[0] != wildcardWord {
		t.Errorf("default pattern = %v, want [%q]", r.Pattern, wildcardWord)
	}
	if r.Source != defaultRulesLabel {
		t.Errorf("default Source = %q, want the built-in label", r.Source)
	}
	// The text is the SAME bytes the parser read (one definition, rendered).
	if _, err := Parse(defaultRulesLabel, DefaultRulesText()); err != nil {
		t.Errorf("DefaultRulesText does not re-parse: %v", err)
	}
	// It must not name a command: a default that named a verb would put policy
	// back into the binary.
	for _, forbidden := range []string{"git", "push", "rm ", "reset"} {
		if strings.Contains(DefaultRulesText(), forbidden) {
			t.Errorf("the built-in default table names %q — the default names NO command (%s)",
				forbidden, DefaultRulesText())
		}
	}
}
