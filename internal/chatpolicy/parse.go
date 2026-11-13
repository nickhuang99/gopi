package chatpolicy

import (
	"fmt"
	"strings"
)

// fenceInfoWord is the fenced-block info string this parser recognises
// (```chat-policy). A Markdown info string may carry MORE after it
// (```chat-policy title="…"), so the test is on the first whitespace-separated
// word of the info string, never on the whole line.
const fenceInfoWord = "chat-policy"

// fenceOpenPrefix is the Markdown fence marker itself. Three backticks is what
// the operator's file uses; the parser accepts a longer run too, because
// Markdown lets a fence be as long as it needs to be to hold its content.
const fenceOpenPrefix = "```"

// commentPrefix starts a COMMENT line inside the fence. A rule table is read
// by a human as much as by the machine, and the operator's own file annotates
// its groups — a comment that the parser refused would make the file
// unwritable in practice.
const commentPrefix = "#"

// wildcardWord is the trailing wildcard: "any remaining words". It is allowed
// ONLY as the final word of a pattern, because a "skip one, then keep
// matching" wildcard is a regular expression, and a rule table that needs one
// is a rule table nobody can predict (see the error text in parseRuleLine).
const wildcardWord = "*"

// Rule is ONE parsed rule line: the decision, its command-word prefix, and
// where it came from. Line and Source are carried into the value (not just the
// error) so a consumer that reports a decision can say which line of which
// file decided it — a decision nobody can trace back to a rule is a decision
// nobody can debug.
type Rule struct {
	// Decision is what the rule answers with.
	Decision Decision
	// Pattern is the command-word prefix, in file order. A final "*" means
	// "any remaining words".
	Pattern []string
	// Line is the 1-based line number inside the source text.
	Line int
	// Source names where the rule came from (a file path, or the built-in
	// default label) — the second half of "which line decided this".
	Source string
}

// Policy is a parsed rule table in FILE ORDER. Order is meaning, not
// decoration: two rules of equal pattern length are equally long, and the one
// the operator wrote first wins (decide.go), so the slice must never be
// re-sorted or de-duplicated.
type Policy struct {
	// Rules is the table, in the order the rules were written.
	Rules []Rule
	// Source names the text the table came from.
	Source string
	// Fenced records whether the source actually carried a ```chat-policy
	// fence. False is not an error (a project may have no rules at all), but
	// it IS a fact the startup banner reports: "0 rules" and "no fence" are
	// different situations for the operator reading the log.
	Fenced bool
}

// Len reports how many rules the table holds.
func (p *Policy) Len() int {
	if p == nil {
		return 0
	}
	return len(p.Rules)
}

// ParseError is a rule-table error that KNOWS WHERE IT IS: the source name and
// the 1-based line. A parser that only says "invalid rule" makes the operator
// re-read a hundred-line fence by hand.
type ParseError struct {
	// Source names the text (file path or default label).
	Source string
	// Line is the 1-based line number of the offending line.
	Line int
	// Msg says what is wrong AND what would be right.
	Msg string
}

// Error renders "source:line: msg" — the shape every compiler uses, so a
// terminal that jumps to errors can jump to this one.
func (e *ParseError) Error() string {
	src := e.Source
	if src == "" {
		src = "<rules>"
	}
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", src, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", src, e.Msg)
}

// Parse reads a project-context document (GOPI.md) and returns the rule table
// its ```chat-policy fence(s) declare.
//
// WHAT IT ACCEPTS, and why each choice is the operator-facing one:
//
//   - Only text INSIDE a ```chat-policy fence is rules. Prose around the fence
//     is the human's explanation of the rules and must never be parsed as one;
//     that is the whole point of using the file the operator already writes.
//   - A file with NO such fence is a VALID empty table (Fenced=false), not an
//     error: absence of rules is a result (the same convention the project
//     context loader already uses for a missing file), and the posture then
//     answers every command.
//   - SEVERAL fences are legal and are concatenated in file order. A document
//     may group its rules; the ORDER is still the reading order, which is what
//     Decide's tie-break means.
//   - Blank lines and `#` comments inside a fence are skipped. An EMPTY fence
//     is a valid empty table.
//   - Duplicate rules are legal and kept. Two identical lines decide
//     identically, so dropping one would only make Line numbers lie.
//   - Every malformed line is a *ParseError naming its line — never a silently
//     dropped rule (Rule 15: a rule the operator wrote must either take effect
//     or be reported; a rule that quietly does nothing is the defect class
//     this whole parser exists to end).
func Parse(source, text string) (*Policy, error) {
	pol := &Policy{Source: source}
	var (
		inFence  bool
		fenceLen int
	)
	for i, raw := range strings.Split(text, "\n") {
		lineNo := i + 1
		trimmed := strings.TrimSpace(raw)
		if !inFence {
			if open, ok := fenceOpen(trimmed); ok {
				inFence = true
				fenceLen = open
				pol.Fenced = true
			}
			continue
		}
		if closeFence(trimmed, fenceLen) {
			inFence = false
			fenceLen = 0
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, commentPrefix) {
			continue
		}
		rule, err := parseRuleLine(source, lineNo, trimmed)
		if err != nil {
			return nil, err
		}
		pol.Rules = append(pol.Rules, rule)
	}
	// An UNCLOSED fence is reported, not tolerated: everything after the
	// opening marker was read as rules, so a missing ``` turns the rest of the
	// operator's document into silent rules (or silent errors). Naming the
	// unfolding is the only honest answer.
	if inFence {
		return nil, &ParseError{
			Source: source,
			Line:   strings.Count(text, "\n") + 1,
			Msg:    "```" + fenceInfoWord + " fence is never closed — add a line containing only " + fenceOpenPrefix,
		}
	}
	return pol, nil
}

// fenceOpen reports whether a trimmed line opens a chat-policy fence and, if
// so, how many backticks the opening run had (a closing fence must be at least
// as long — the CommonMark rule, kept so the file stays a legal Markdown
// document in any renderer).
func fenceOpen(trimmed string) (int, bool) { return fenceOpenAs(trimmed, fenceInfoWord) }

// fenceOpenAs is fenceOpen generalised over the info word, so the SECOND fence
// (```chat-op-category, category.go) opens with the same Markdown reading —
// including "a longer backtick run is still a fence" and "the info string may
// carry more after its first word" — rather than with a copy of this logic.
func fenceOpenAs(trimmed, infoWord string) (int, bool) {
	if !strings.HasPrefix(trimmed, fenceOpenPrefix) {
		return 0, false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == '`' {
		n++
	}
	if n < len(fenceOpenPrefix) {
		return 0, false
	}
	info := strings.TrimSpace(trimmed[n:])
	if info == "" {
		return 0, false
	}
	fields := strings.Fields(info)
	if len(fields) == 0 || fields[0] != infoWord {
		return 0, false
	}
	return n, true
}

// closeFence reports whether a trimmed line closes the open fence.
func closeFence(trimmed string, openLen int) bool {
	if len(trimmed) < openLen || len(trimmed) < len(fenceOpenPrefix) {
		return false
	}
	if strings.Trim(trimmed, "`") != "" {
		return false
	}
	return true
}

// parseRuleLine turns one non-blank, non-comment fence line into a Rule:
//
//	<decision> <word> <word> … [*]
//
// Every rejection carries the rule the operator needs to fix it.
func parseRuleLine(source string, lineNo int, trimmed string) (Rule, error) {
	fields := strings.Fields(trimmed)
	decision, ok := decisionNamed(fields[0])
	if !ok {
		return Rule{}, &ParseError{
			Source: source,
			Line:   lineNo,
			Msg: fmt.Sprintf("unknown decision %q — a rule line starts with one of %s",
				fields[0], strings.Join(decisionWords, "|")),
		}
	}
	pattern := fields[1:]
	if len(pattern) == 0 {
		return Rule{}, &ParseError{
			Source: source,
			Line:   lineNo,
			Msg:    fmt.Sprintf("%s rule names no command words — write the prefix it applies to, e.g. %q", decision, decisionAllowWord+" some command"),
		}
	}
	if err := checkWildcard(pattern); err != nil {
		return Rule{}, &ParseError{Source: source, Line: lineNo, Msg: err.Error()}
	}
	return Rule{Decision: decision, Pattern: pattern, Line: lineNo, Source: source}, nil
}

// checkWildcard enforces the ONE placement rule both fences share: "*" is the
// last word or it is not allowed at all, because a "skip one, then keep matching"
// wildcard is a regular expression and a table that needs one is a table nobody
// can predict. Exported to category.go, not to callers: one rule, one reader.
func checkWildcard(pattern []string) error {
	for i, w := range pattern {
		if w == wildcardWord && i != len(pattern)-1 {
			return fmt.Errorf("%q may only be the LAST word of a rule (%s matches any remaining words): a wildcard in the middle would make the rule a pattern nobody can predict",
				wildcardWord, wildcardWord)
		}
	}
	return nil
}
