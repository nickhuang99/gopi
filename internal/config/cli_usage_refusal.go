package config

import (
	"fmt"
	"os"
)

// cli_usage_refusal.go — the USAGE-REFUSAL class of the pre-verb hoist, and the
// ONE definition of the status it exits with.
//
// THE DEFECT THIS FILE EXISTS FOR (measured on the built binary, 2026-10-30).
// BootstrapEnvFromArgs hoists a top-level `--flag VALUE` out of argv before the
// subcommand chain, and its peek was UNCONDITIONAL for every flag whose value is
// required: it took the next token as the value whatever that token was. So a
// flag written without its value immediately in front of its own verb ate the
// verb:
//
//	$ ./bin/gopi --chat-surface-turns chat </dev/null   # exit 0, top-level usage
//	$ ./bin/gopi --chat-ideal-geometry chat </dev/null  # exit 0, top-level usage
//
// Both published the WORD `chat` into the knob's env (where the resolver then
// reported it as an unusable value and fell through to the default — a second,
// quieter half of the same confusion) and deleted it from the dispatch
// remainder, so no verb was dispatched at all: the run fell through to the bare
// prompt path and printed the top-level usage. The operator's command had been
// rewritten into a DIFFERENT one, and the exit status said the rewrite was fine
// (Rule 15: a named refusal is the only honest answer).
//
// WHY THE REFUSAL LIVES HERE AND NOT IN THE KNOB'S RESOLVER. internal/config's
// knobs are three-channel (flag > env > settings) and their resolvers are SOFT by
// design: a value that cannot be understood is REPORTED and the next channel
// decides, because one stale settings.json line must not fail every command on
// the host (the pinned asymmetry, stated in full at
// OutputLanguageRefuseUnusablePerInvocation). A FLAG is not that channel: it is
// THIS invocation's own command line, typed by the person who can fix it on the
// same line, so the same file's precedent makes the flag channel FATAL. And the
// check has to happen at the HOIST rather than in each verb's parser, because the
// hoist is what destroys the verb: by the time a verb parser could complain there
// is no verb left to run — which is exactly why the two measured commands above
// exited 0 without saying anything.
//
// WHAT IT DOES NOT DO. It does not re-implement any knob's value rule: each row
// names the knob's OWN parser, so there is exactly one opinion of what a valid
// value is (Rule 2). It refuses nothing when no flag was given, and it touches no
// value-OPTIONAL switch: the verbBound table's conditional peek
// (`gopi --memory-prefetch chat`) keeps its behaviour byte for byte, and
// `--settings X`, `--home X`, `--tmpdir X` keep the plain peek plus the identity
// remainder contract (see BootstrapEnvFromArgs).
//
// THE TABLE IS CLOSED, one row per hoisted flag whose value is REQUIRED and whose
// SHAPE this package already owns a parser for. A flag whose value is a path or a
// free string is deliberately absent: "chat" cannot be told apart from a filename
// by shape, and refusing it would be a guess — Rule 15 forbids an invented
// refusal exactly as much as it forbids silence.

// CLIUsageExitCode is the status of a command line gopi REFUSES to carry out as
// written — the flag or its value is wrong, so NO work happens. It is the POSIX
// `EX_USAGE` class, and it is deliberately distinct from the two neighbouring
// classes a caller must be able to tell apart: 1 ("the run itself failed" — here
// nothing ran) and AgentGuardRefusalExitCode ("this launch is forbidden" — a
// different operator action).
//
// It is NOT a second literal beside the reply-language knob's
// OutputLanguagePerInvocationExitCode: that constant was here first and is cited
// by CONFIG.md, so this one ALIASES it instead of restating the number (Rules
// 1b/13/14 — a status re-typed in each place is the mirror those rules forbid).
// The direction matters for one further reason: aliasing FROM the existing
// constant leaves internal/config/output_language.go byte-identical, so no line
// of a file CONFIG.md anchors into moves because of this work (Rule 38's
// pathology — a citation that dies under an unrelated insert).
const CLIUsageExitCode = OutputLanguagePerInvocationExitCode

// The refusal's vocabulary. Constants for the usual reason: the test that drives
// the REAL binary matches the SAME words the process prints (Rule 14 — a test
// holding its own copy keeps passing after the message is reworded), and one
// entry point words every row.
const (
	// HoistedFlagRefusalPrefix starts every hoist refusal, so a caller scanning
	// a transcript can tell gopi's own diagnosis from the verb's output.
	HoistedFlagRefusalPrefix = "gopi: "
	// HoistedFlagMissingValuePhrase is what a flag with no value at all is
	// refused as (`--chat-surface-turns chat`, `--chat-surface-turns`).
	HoistedFlagMissingValuePhrase = "needs a value"
	// HoistedFlagUnusableValuePhrase is what a value the knob's own parser
	// rejects is refused as (`--chat-surface-turns banana`).
	HoistedFlagUnusableValuePhrase = "is not a usable value"
	// HoistedFlagNothingRanClaim is the second line of BOTH refusals: it states
	// the thing the exit status alone cannot — that NO work happened, so there is
	// nothing to clean up and nothing to retry.
	HoistedFlagNothingRanClaim = "nothing was run"
	// HoistedFlagNextWordKeptClaim is appended to the MISSING-value refusal, and
	// it is the diagnosis rather than a decoration: the token after the flag is
	// usually the operator's verb, and the defect this file closes was that it was
	// silently taken as the value and deleted from argv.
	HoistedFlagNextWordKeptClaim = "the word after the flag was not taken as its value"
	// HoistedFlagNextWordIsCommandFmt names the case the defect was made of: the
	// token after the flag is one of THIS binary's own command words, so it was
	// never a candidate for the value. Spelling that out (rather than the generic
	// "not a usable value") is what tells the operator which half of their command
	// line is missing.
	HoistedFlagNextWordIsCommandFmt = "the next word `%s` is one of gopi's own commands, not a value"
	// HoistedFlagWriteFmt is the copy-pasteable remedy line, built from the same
	// flag name the refusal names.
	HoistedFlagWriteFmt = "write `gopi --%s VALUE %s`"
	// hoistedFlagVerbPlaceholder stands where the operator's own verb belongs in
	// the example line: the hoist runs BEFORE dispatch, so it has no idea which
	// verb was meant — and inventing one would be the guess this file refuses.
	hoistedFlagVerbPlaceholder = "VERB"
)

// The composed message shapes (Rule 13: one spelling per sentence, built from
// the constants above rather than re-typed at the print site).
const (
	// The three shapes differ only in the CLAIM they make about the token, which
	// is the whole point: "you forgot the value", "the word you put there is a
	// command", and "the value you wrote is not one this knob accepts" are three
	// different mistakes with three different remedies.
	hoistedFlagNoTokenFmt = "gopi: --%s " + HoistedFlagMissingValuePhrase +
		" (%s) — " + HoistedFlagWriteFmt + "\n  %s"
	hoistedFlagVerbWordFmt = "gopi: --%s " + HoistedFlagMissingValuePhrase +
		" — " + HoistedFlagNextWordIsCommandFmt + "; " + HoistedFlagWriteFmt + "\n  %s"
	hoistedFlagUnusableValueFmt = "gopi: --%s %q " + HoistedFlagUnusableValuePhrase +
		" (%s) — " + HoistedFlagWriteFmt + "\n  %s"
)

// hoistedValuedFlag is one closed-table row: the value SHAPE the operator is
// shown, and the parser that decides it — the knob's OWN parser, never a second
// opinion (Rule 2).
type hoistedValuedFlag struct {
	// shape is the value as the operator must write it ("a whole number of
	// turns", "COLSxROWS"), taken from the knob's own vocabulary constant.
	shape string
	// parses reports whether a value is usable for this knob.
	parses func(string) bool
}

// hoistedValuedFlags is the closed table. A row is what makes a hoisted flag's
// value REFUSED by name instead of being taken from whatever token happened to
// sit next to it.
//
// Built once, from the knobs' own exported names, vocabulary constants and
// parsers (Rules 1b/13/14): no knob name, no shape sentence and no value rule is
// spelled a second time here.
var hoistedValuedFlags = map[string]hoistedValuedFlag{
	// --chat-surface-turns N — the flag whose missing value was measured eating
	// its own verb (this file's header).
	ChatSurfaceTurnsFlagName: {
		shape: chatSurfaceTurnsShapeClaim,
		parses: func(v string) bool {
			_, ok := ParseChatSurfaceTurns(v)
			return ok
		},
	},
	// --chat-ideal-geometry COLSxROWS — the sibling the same way: "chat" is not a
	// geometry, and before this table it was published as one.
	ChatIdealGeometryFlagName: {
		shape: chatIdealGeometryShapeClaim,
		parses: func(v string) bool {
			_, _, ok := parseChatIdealGeometry(v)
			return ok
		},
	},
}

// hoistRefusalReason is WHY a hoisted flag cannot take the token the
// unconditional peek would have given it. The three are distinct diagnoses with
// distinct remedies, so an enum keeps them from being flattened into one message.
type hoistRefusalReason int

const (
	// hoistOK — the token is the flag's value (or the flag is not shape-checked
	// here at all, in which case the caller keeps the plain peek).
	hoistOK hoistRefusalReason = iota
	// hoistNoToken — nothing follows the flag.
	hoistNoToken
	// hoistVerbWord — the token that follows is one of THIS binary's command
	// words. It was never a candidate for the value; this is the shape the
	// measured defect was made of (`--chat-surface-turns chat`).
	hoistVerbWord
	// hoistUnusable — a token is there, it is not a command word, and the knob's
	// own parser rejects it (`--chat-surface-turns banana`).
	hoistUnusable
)

// hoistedFlagVerdict is the ONE decision (Rule 2): for a hoisted flag name and
// the token the unconditional peek WOULD have taken, it says whether the value is
// acceptable and, if not, WHY. Flags absent from the table (paths, free strings,
// value-optional switches) are never refused — the caller keeps the plain peek for
// them, because "the next token is the value" is the only answer available there
// and guessing would be worse than the convention.
//
// The verb-word test uses the guard's OWN membership index (positionalVerbSet,
// the vocabulary of top-level command words this binary dispatches and refuses in
// positional form) rather than a second list: `chat` is a command because that
// table says so, not because this file spelled the word (Rules 1b/13/14).
//
// It returns rather than exiting so the DECISION is unit-testable in this package;
// the fatal site is refuseHoistedFlagValue below, and the status it exits with is
// pinned by driving the real binary (Rule 31).
func hoistedFlagVerdict(name, value string, haveToken bool) hoistRefusalReason {
	row, ok := hoistedValuedFlags[name]
	if !ok {
		return hoistOK
	}
	if !haveToken {
		return hoistNoToken
	}
	if row.parses(value) {
		return hoistOK
	}
	if positionalVerbSet[value] {
		return hoistVerbWord
	}
	return hoistUnusable
}

// refuseHoistedFlagValue is the ONE fatal site of the hoist: it names the flag,
// says what it needed, states that the next word was NOT consumed, and exits with
// CLIUsageExitCode.
//
// It is FATAL (os.Exit, not an error return) for the reason the sibling
// per-invocation refusal states: the policy is a process decision, and it MUST
// fire before the subcommand chain — the chain is where the operator's verb would
// otherwise be dispatched, and past that point the wrong-command rewrite has
// already happened and there is nothing left to report it.
func refuseHoistedFlagValue(name, value string, reason hoistRefusalReason) {
	row, _ := hoistedValuedFlags[name]
	switch reason {
	case hoistNoToken:
		fmt.Fprintf(os.Stderr, hoistedFlagNoTokenFmt+"\n",
			name, row.shape, name, hoistedFlagVerbPlaceholder,
			HoistedFlagNothingRanClaim+": "+HoistedFlagNextWordKeptClaim)
	case hoistVerbWord:
		fmt.Fprintf(os.Stderr, hoistedFlagVerbWordFmt+"\n",
			name, value, name, hoistedFlagVerbPlaceholder,
			HoistedFlagNothingRanClaim+": "+HoistedFlagNextWordKeptClaim)
	case hoistUnusable:
		fmt.Fprintf(os.Stderr, hoistedFlagUnusableValueFmt+"\n",
			name, value, row.shape, name, hoistedFlagVerbPlaceholder, HoistedFlagNothingRanClaim)
	case hoistOK:
		// Not reachable from this file's callers — a caller only refuses on a
		// non-OK verdict. Exiting here anyway would invent a refusal, so the
		// honest answer is to return: no diagnosis, no status, nothing claimed.
		return
	}
	os.Exit(CLIUsageExitCode)
}
