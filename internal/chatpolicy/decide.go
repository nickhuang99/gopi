package chatpolicy

import "strings"

// Match is the table's answer for ONE command line.
//
// Matched is the FIRST thing to read, and it is not a courtesy field: false
// means the table has NO opinion and Decision must not be consulted at all —
// the POSTURE answers instead. That is why Decision is not "allow by default":
// a zero value that silently meant "run it" is exactly the failure mode this
// package exists to prevent, so the caller has to branch on Matched before it
// looks at anything else.
type Match struct {
	// Matched is true when a rule in the table applied to the line.
	Matched bool
	// Decision is the deciding rule's answer. Meaningful ONLY when Matched.
	Decision Decision
	// Rule is the rule that decided — its Line/Source let a report say WHICH
	// line of which file answered, which is the first thing an operator wants
	// after an unexpected "no".
	Rule Rule
	// Clauses is the per-clause answer of a compound command line, in the
	// order the clauses were written. It is nil for a single-clause line (the
	// common case) and non-nil when the line was handed over as several
	// clauses — see DecideClauses for why the whole line then takes the MOST
	// RESTRICTIVE answer.
	Clauses []Match
}

// THE PACKAGE DOES NOT TOKENISE. Every entry point below takes the WORDS, never
// a command string: the chat surface's ONE tokenizer is
// tools.ChatShellTokens (internal/tools/chat_shell_tokens.go, delegating to the
// guard's chatShellTokens — the implementation that already existed). A second
// splitter here would eventually disagree with the first about where one
// command ends, and a boundary that tokenises differently from the engine that
// judges it is a boundary with a hole in it. The rule table is a pure consumer
// of somebody else's token stream.
//
// The same holds for CLAUSES: the sentinel that marks a clause break is the
// tokenizer's (tools' chatClauseBreakWord), so this package never spells it.
// A caller that has a token stream splits it with tools' own clause splitter
// and hands the pieces to DecideClauses.

// DecideWords answers ONE clause: the rules of p, applied to the words the ONE
// tokenizer produced.
//
// THE MATCHING RULE, in three sentences:
//
//  1. A rule matches when its WORDS are a prefix of the command's words. A
//     trailing "*" matches any remaining words (and may match none), which is
//     how "this verb, whatever its arguments" is written.
//  2. The LONGEST matching prefix wins: `allow git status` beats `prompt git`
//     for `git status` — the more specific sentence is the one meant.
//  3. EQUAL LENGTH is decided by FILE ORDER, first written wins. Two equally
//     long prefixes are two readings of the same command, and the operator
//     resolved that conflict by writing one above the other; the file order is
//     the only tie-break that needs no extra syntax.
func (p *Policy) DecideWords(words []string) Match {
	return p.decideOne(words)
}

// DecideClauses answers a COMPOUND command line (`a && b`, `a; b`, `a | b`):
// each clause is decided and the LINE takes the MOST RESTRICTIVE of them
// (forbidden > prompt > allow).
//
// It is Matched only when EVERY clause matched: running `git status; rm -rf /`
// must not be judged by the clause the operator happened to write first, and
// one clause the table has no opinion about makes the whole line the posture's
// question. A line of ONE clause behaves exactly like DecideWords.
func (p *Policy) DecideClauses(clauses [][]string) Match {
	switch len(clauses) {
	case 0:
		return Match{}
	case 1:
		return p.decideOne(clauses[0])
	}
	whole := Match{Matched: true, Clauses: make([]Match, 0, len(clauses))}
	decided := false
	for _, clause := range clauses {
		m := p.decideOne(clause)
		whole.Clauses = append(whole.Clauses, m)
		if !m.Matched {
			// ONE unexplained clause makes the LINE unexplained: the posture —
			// not this table — must answer for the whole line.
			whole.Matched = false
			whole.Rule = Rule{}
			continue
		}
		// Strictly greater: the FIRST clause carrying the most restrictive
		// answer names the rule, so the report points at the most specific
		// evidence rather than at whichever clause came last.
		if !decided || m.Decision > whole.Decision {
			whole.Decision = m.Decision
			whole.Rule = m.Rule
			decided = true
		}
	}
	if !whole.Matched {
		whole.Decision = DecisionAllow // meaningless when Matched is false; zeroed for honesty
	}
	return whole
}

// decideOne applies the table to ONE clause's words.
func (p *Policy) decideOne(words []string) Match {
	if p == nil {
		return Match{}
	}
	best := -1    // index of the winning rule; -1 = nothing matched
	bestLen := -1 // its specificity (pattern length, excluding a trailing "*")
	for i := range p.Rules {
		spec, ok := p.Rules[i].matchLength(words)
		if !ok {
			continue
		}
		// STRICTLY greater: an equal-length rule LATER in the file never
		// displaces the earlier one, which is what "file order decides a tie"
		// means. `>=` here would make the LAST duplicate win.
		if spec > bestLen {
			best, bestLen = i, spec
		}
	}
	if best < 0 {
		return Match{}
	}
	return Match{Matched: true, Decision: p.Rules[best].Decision, Rule: p.Rules[best]}
}

// matchLength reports whether the rule applies to words and, if so, how
// SPECIFIC the match is (a longer prefix is a more specific sentence).
//
// A trailing "*" is worth 0 words of specificity (it constrains nothing), so
// `prompt *` loses to every other matching rule — the intended reading of a
// catch-all.
func (r Rule) matchLength(words []string) (int, bool) {
	return prefixMatchLength(r.Pattern, words)
}

// prefixMatchLength is matchLength's body over a bare pattern, shared with the
// SECOND fence's table (category.go) so both tables read a prefix identically —
// longest wins, a trailing "*" constrains nothing.
func prefixMatchLength(pattern, words []string) (int, bool) {
	if len(pattern) == 0 {
		return 0, false
	}
	wildcard := pattern[len(pattern)-1] == wildcardWord
	if wildcard {
		pattern = pattern[:len(pattern)-1]
	}
	if len(words) < len(pattern) {
		return 0, false
	}
	for i, w := range pattern {
		if words[i] != w {
			return 0, false
		}
	}
	return len(pattern), true
}

// PatternString renders the rule's prefix as the operator wrote it, for a
// report line: `allow git status` → "git status".
func (r Rule) PatternString() string {
	return strings.Join(r.Pattern, " ")
}

// String renders a match for a log line: what decided, or that nothing did.
func (m Match) String() string {
	if !m.Matched {
		return "no rule matched"
	}
	return m.Decision.String() + " (" + patternOf(m.Rule) + ")"
}

// patternOf renders the deciding rule's prefix, tolerating a zero Rule (a
// Match someone constructed by hand in a test).
func patternOf(r Rule) string {
	if len(r.Pattern) == 0 {
		return "<no rule>"
	}
	return r.PatternString()
}
