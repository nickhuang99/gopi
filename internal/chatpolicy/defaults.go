package chatpolicy

// The built-in default rule table: what the engine answers when the project's
// GOPI.md carries no ```chat-policy fence (or when the file is absent).
//
// IT NAMES NO COMMAND, ON PURPOSE. The rule DATA here is one line —
//
//	prompt *
//
// — which says "every command line is a question for the operator". That is
// the same answer the default posture gives an unmatched command, so a project
// with no rules and a project with this one rule behave identically; the
// difference is only that the table exists and can be printed.
//
// A QUESTION HERE IS ANSWERABLE: `prompt` is satisfied by the operator's own
// affirmative in the turn's message (internal/tools/chat_approval_authorization.go),
// so this default means "ask me first", not "nothing ever runs". It is worth
// saying here because the opposite reading shipped for weeks: with no
// approval channel a `prompt *` default turned every bash and write call on a
// surface with no settings.json document into a permanent refusal.
//
// WHAT MUST NEVER BE ADDED HERE: a verb, or a path. The whole point of this
// package is that "which commands are dangerous" is the OPERATOR's sentence,
// written in the file the operator already writes, not a list compiled into
// the binary (the defect this replaces: a hardcoded table of fourteen git
// verbs that needed a code change and a release to revise). A default that
// named `push` would reintroduce exactly that: code standing in for policy.
//
// The table is DATA — text in the same syntax the fence uses — so it is parsed
// by the SAME parser as an operator's file. A default table that were built as
// Go structs could drift from the syntax; one that is text cannot.
const defaultRulesText = fenceOpenPrefix + fenceInfoWord + "\n" +
	"prompt *\n" +
	fenceOpenPrefix + "\n"

// defaultRulesLabel is the SOURCE name a rule from the built-in table reports
// in its Line/Source pair. It is a name, not a path, because there is no file:
// a decision traced to "built-in" tells the operator the rule is not in their
// own file, which is the first question they ask when the engine says no.
const defaultRulesLabel = "built-in chat-policy defaults"

// Defaults returns the built-in rule table, parsed by the same parser that
// reads an operator's fence.
//
// It returns an error only if the constant above is malformed — a programming
// error, not an operator one — and the error is handed back rather than
// panicked so a caller can print it and continue with no rules at all (an
// engine that refuses to start because its own default table is broken hides
// the operator's real work behind a bug they did not cause).
func Defaults() (*Policy, error) {
	return Parse(defaultRulesLabel, defaultRulesText)
}

// DefaultRulesText is the built-in table's TEXT, exactly as parsed. It is
// exported so the startup banner and the documentation can print the same
// bytes the engine reads (Rule 2: one definition, rendered, never re-typed).
func DefaultRulesText() string {
	return defaultRulesText
}
