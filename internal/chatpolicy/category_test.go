package chatpolicy

// category_test.go — the SECOND fence's own proofs.
//
// The category table answers "WHAT is this action?", and it exists as a separate
// fence so that question can never be answered by the enforcement table. These
// tests pin the properties that separation depends on, each by driving the real
// parser and matcher rather than by reading the source:
//
//   - the built-in table is REAL data that parses, and it carries no decision word
//     (so it can never be read as `allow`/`prompt`/`forbidden`);
//   - a malformed category line is REPORTED with its line number, exactly as a
//     malformed rule line is;
//   - the matching rule is the rule layer's own (longest prefix, file order for a
//     tie, trailing `*` worth nothing), plus the two shapes this table needs and
//     the enforcement table must never learn: a directory word (`internal/`) and a
//     write redirection, whose subject is not the command's first word;
//   - the two fences are DIFFERENT documents: a ```chat-policy rule is not a
//     category line and vice versa.

import (
	"strings"
	"testing"
)

// TestDefaultCategoriesParse is the shipped table's smoke test: the constant is
// real the grammar accepts, and both classes have words (an empty class would make
// the criterion's refusal name nothing).
func TestDefaultCategoriesParse(t *testing.T) {
	tab, err := DefaultCategories()
	if err != nil {
		t.Fatalf("the built-in category table does not parse: %v", err)
	}
	if !tab.Fenced || tab.Len() == 0 {
		t.Fatalf("the built-in category table parsed to %d lines (fenced=%v)", tab.Len(), tab.Fenced)
	}
	for _, class := range []string{CategoryRepoWriteWord, CategoryLongWord} {
		if len(tab.WordsOf(class)) == 0 {
			t.Errorf("the built-in table declares no %q line — that class would match nothing", class)
		}
	}
	// Operator work is the DEFAULT and names no word; if the shipped table ever
	// declares it, the default class has become a list, which is the shape the
	// deleted Go slices had.
	if w := tab.WordsOf(CategoryOperatorWorkWord); len(w) != 0 {
		t.Errorf("the built-in table declares %d %q line(s) (%v) — the default class must stay a default",
			len(w), CategoryOperatorWorkWord, w)
	}
}

// TestDefaultCategoryTextCarriesNoDecisionWord is the separation, stated as a
// property of the BYTES: a category fence can never be parsed by (or mistaken for)
// the enforcement parser, because it never spells a decision.
func TestDefaultCategoryTextCarriesNoDecisionWord(t *testing.T) {
	for _, line := range strings.Split(DefaultCategoryText(), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, commentPrefix) || strings.HasPrefix(trimmed, fenceOpenPrefix) {
			continue
		}
		first := strings.Fields(trimmed)[0]
		if _, ok := decisionNamed(first); ok {
			t.Errorf("the category fence carries the DECISION word %q as a class (%q): the two fences would "+
				"become one table", first, trimmed)
		}
		if _, ok := categoryNamed(first); !ok {
			t.Errorf("the category fence line %q does not start with a class word", trimmed)
		}
	}
	// …and the other direction: the ENFORCEMENT default text carries no class word,
	// so neither table can be read as the other.
	for _, line := range strings.Split(DefaultRulesText(), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, commentPrefix) || strings.HasPrefix(trimmed, fenceOpenPrefix) {
			continue
		}
		first := strings.Fields(trimmed)[0]
		if _, ok := categoryNamed(first); ok {
			t.Errorf("the enforcement fence carries the CLASS word %q as a decision (%q)", first, trimmed)
		}
	}
}

// TestParseCategoriesReportsTheLine is the honesty half: a rule the operator wrote
// must either take effect or be reported with its line (Rule 15).
func TestParseCategoriesReportsTheLine(t *testing.T) {
	const src = "notes\n" +
		"```chat-op-category\n" +
		"# a comment\n" +
		"repo-write git commit *\n" +
		"long * anything\n" +
		"```\n"
	_, err := ParseCategories("GOPI.md", src)
	if err == nil {
		t.Fatal("a wildcard in the middle of a category line was accepted")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected a *ParseError, got %T: %v", err, err)
	}
	if pe.Line != 5 {
		t.Errorf("the error names line %d, want 5", pe.Line)
	}
	if !strings.Contains(pe.Error(), "GOPI.md:5") {
		t.Errorf("the error does not name source:line — %q", pe.Error())
	}

	_, err = ParseCategories("GOPI.md", "```chat-op-category\nask git commit *\n```\n")
	if err == nil || !strings.Contains(err.Error(), "unknown action class") {
		t.Fatalf("an unknown class was not reported: %v", err)
	}

	// An UNCLOSED fence is reported rather than turning the rest of the document
	// into silent rules.
	if _, err := ParseCategories("GOPI.md", "```chat-op-category\nlong make test\n"); err == nil {
		t.Error("an unclosed category fence was accepted")
	}
}

// TestParseCategoriesIgnoresTheOtherFence is the separation at the PARSER: a
// ```chat-policy block in the same document contributes no category line, and a
// ```chat-op-category block contributes no rule.
func TestParseCategoriesIgnoresTheOtherFence(t *testing.T) {
	const doc = "```chat-policy\nprompt *\n```\n" +
		"```chat-op-category\nlong make test *\n```\n"
	tab, err := ParseCategories("GOPI.md", doc)
	if err != nil {
		t.Fatalf("ParseCategories: %v", err)
	}
	if tab.Len() != 1 || tab.Rules[0].Category != CategoryLongWord {
		t.Fatalf("the category table read %+v — it must hold exactly the category line", tab.Rules)
	}
	pol, err := Parse("GOPI.md", doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pol.Len() != 1 || pol.Rules[0].Decision != DecisionPrompt {
		t.Fatalf("the rule table read %+v — it must hold exactly the policy line", pol.Rules)
	}
}

// TestCategoryMatchIsPrefixBased pins the matching rule on a table written for the
// test, so the assertions do not move when the shipped vocabulary is edited.
func TestCategoryMatchIsPrefixBased(t *testing.T) {
	tab, err := ParseCategories("test", "```chat-op-category\n"+
		"repo-write git commit *\n"+
		"long git commit --amend *\n"+
		"repo-write > internal/\n"+
		"long make test *\n"+
		"```\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := []struct {
		name  string
		words []string
		want  string
		ok    bool
	}{
		// LONGEST PREFIX WINS — the more specific sentence is the one meant.
		{"longer beats shorter", []string{"git", "commit", "--amend", "x"}, CategoryLongWord, true},
		{"the shorter still applies", []string{"git", "commit", "-m", "x"}, CategoryRepoWriteWord, true},
		// A prefix may not be a SUBSTRING: the old predicate was `strings.Contains`.
		{"no substring match", []string{"xmake", "test"}, "", false},
		{"no partial-word match", []string{"git", "committed"}, "", false},
		// A directory word matches a longer word under it.
		{"directory prefix", []string{"echo", "x", ">", "internal/agent/y.go"}, CategoryRepoWriteWord, true},
		// …and a redirection flush against its target is the same act.
		{"flushed redirection", []string{"echo", "x", ">>internal/agent/y.go"}, CategoryRepoWriteWord, true},
		// …but NOT the session directory the write boundary mandates.
		{"session dir is not the source tree", []string{"echo", "x", ">", "internalish/y.go"}, "", false},
		// An exact word matches, with or without arguments.
		{"exact word", []string{"make", "test"}, CategoryLongWord, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rule, ok := tab.MatchCategory(c.words)
			if ok != c.ok {
				t.Fatalf("matched=%v, want %v (rule %+v)", ok, c.ok, rule)
			}
			if ok && rule.Category != c.want {
				t.Errorf("class %q, want %q", rule.Category, c.want)
			}
		})
	}
}

// TestCategoryFileOrderDecidesATie is the tie-break, shared with the rule layer:
// two equally long prefixes are two readings of one command, and the operator
// resolved that by writing one above the other.
func TestCategoryFileOrderDecidesATie(t *testing.T) {
	tab, err := ParseCategories("test", "```chat-op-category\n"+
		"long git push *\n"+
		"repo-write git push *\n"+
		"```\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rule, ok := tab.MatchCategory([]string{"git", "push", "origin", "master"})
	if !ok || rule.Category != CategoryLongWord {
		t.Fatalf("the tie was decided as %+v — the FIRST written line must win", rule)
	}
}

// TestCategoryTableIsReadOnlyOverItsWords: WordsOf hands out a COPY-like slice, so
// a renderer that mutates what it printed cannot disarm the table.
func TestCategoryTableIsReadOnlyOverItsWords(t *testing.T) {
	tab, err := DefaultCategories()
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	words := tab.WordsOf(CategoryLongWord)
	if len(words) == 0 {
		t.Fatal("no long words")
	}
	before := len(tab.WordsOf(CategoryRepoWriteWord))
	words[0] = "mutated"
	if after := len(tab.WordsOf(CategoryRepoWriteWord)); after != before {
		t.Fatalf("WordsOf aliased the table (%d -> %d)", before, after)
	}
	if got := tab.WordsOf(CategoryLongWord)[0]; got == "mutated" {
		t.Error("WordsOf handed out a slice into the table's own rules")
	}
}
