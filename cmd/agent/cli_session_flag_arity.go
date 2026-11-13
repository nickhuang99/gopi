package main

// cli_session_flag_arity.go — ONE declaration of each session verb's flag
// ARITY, and the criterion the whole `gopi session` family obeys: a flag may
// stand on EITHER side of the positional <session-id>.
//
// THE MEASURED DEFECT (2026-10-27). `gopi session show --json <id>` worked
// while `gopi session log --json <id>` and `gopi session stats --json <id>`
// answered "missing <session-id> argument" / "pick one input: <session-id>, …"
// — sentences about an input the operator HAD typed. The id was on the command
// line; the parser ate it.
//
// THE MECHANISM. `session show` / `session replay` / `session notes` parse their
// own argv and classify a non-flag token as a positional wherever it stands.
// `session log` / `session stats` / `session raw` share the hand-rolled
// `memoryFlagSet` scanner (cli_memory.go), whose parse() has ONE default for an
// undeclared flag: a bare `--flag` takes the NEXT token as its value. That
// default is right for `--grep foo` and wrong for `--json`, which is a complete
// instruction on its own — and the very next token was the session id, so the
// flag claimed it, no positional survived, and the verb reported the id absent.
//
// THE CRITERION, and why it is expressed HERE rather than in each parser: where
// a flag's value ENDS is a property of the flag, not of the verb that happens to
// admit it. It was undeclared, so the scanner guessed, and it guessed "value".
// Every admitted flag therefore states its arity in the verb's ONE table below,
// which is ALSO the verb's admission table (`logFlagMember` and friends read it,
// so admission and arity can never disagree — Rules 13/14: one definition, every
// reader referencing it, no mirror). A boolean flag is then DECLARED boolean
// before the scan (memoryFlagSet.boolean), which is the scanner's own existing
// mechanism for "this flag never claims the next argument".
//
// WHAT THIS DOES NOT CHANGE. A value-taking flag keeps taking its value:
// `--grep foo`, `--last 3`, `--filter read`, `--http 2`, `--file PATH`,
// `--branch main`. A bare `--last` / `--http` at the end of the line still means
// "1" / "all", exactly as before — the scanner leaves the value empty when the
// next token is another flag or when there is no next token.

import (
	"fmt"
	"strings"
)

// sessionFlagArityTable maps a verb's flag name (WITHOUT the leading dashes) to
// whether that flag claims the following token as its value.
//
// It is a type, not three ad-hoc maps, so every verb's table is read by the same
// two functions (admission + arity) and a new verb cannot quietly ship a flag
// whose arity nobody stated.
type sessionFlagArityTable map[string]bool

// sessionLogFlagArity is `gopi session log`'s ONE flag table: admission and
// arity together. `true` = the flag takes a value.
//
// The moved flags --raw/--http STAY admitted on purpose (they are refused at run
// time with a pointer to the command that replaced them, cli_log.go's
// logMovedPointer): dropping them would turn a documented refusal into a generic
// "unknown flag" error.
//
//nolint:gochecknoglobals // the verb's flag table, read by its parser and by the G4 gate
var sessionLogFlagArity = sessionFlagArityTable{
	// View flags: complete instructions, never a value.
	"response":     false,
	"thinking":     false,
	"request":      false,
	"raw":          false,
	"json":         false,
	"text":         false,
	"chat":         false,
	"todo":         false,
	"memory":       false,
	"context-trim": false,
	"search":       false,
	"web":          false,
	"job":          false,
	"context":      false,
	"vision":       false,
	"text2img":     false,
	"git":          false,
	"all":          false,
	"summary":      false,
	// --full is --grep's companion (it turns the match window off): it is a
	// complete instruction of its own, never a value.
	"full": false,
	// The two whole-session TABLES (rounds vs turns): each is a complete
	// instruction — the table it prints is the whole session.
	logFlagRounds: false,
	logFlagTurns:  false,
	// Value-taking flags. --http is value-taking AND its value is optional at
	// the END of the line (a bare `--http` means "all"); the optionality is the
	// scanner's, the arity is this table's.
	"http":          true,
	"grep":          true,
	"filter":        true,
	"last":          true,
	logFlagToolCall: true,
	// The DELIVERABLE SELECTORS: address the session by the branch or worktree
	// the operator is standing in (the same two addresses `gopi task show` and
	// `gopi worker --resume` take), instead of by a <session-id> that workflow
	// does not carry.
	sessionBranchFlag:   true,
	sessionWorktreeFlag: true,
}

// sessionStatsFlagArity is `gopi session stats`'s ONE flag table.
//
//nolint:gochecknoglobals // the verb's flag table, read by its parser and by the G4 gate
var sessionStatsFlagArity = sessionFlagArityTable{
	"rounds": false,
	"tools":  false,
	"bytes":  false,
	"json":   false,
	"all":    false,
	"cost":   false,
	"bill":   false,
	"last":   true,
	// The DELIVERABLE SELECTORS: address the session by the branch or
	// worktree the operator is standing in, instead of by a <session-id>
	// that workflow does not carry.
	sessionBranchFlag:   true,
	sessionWorktreeFlag: true,
}

// sessionRawFlagArity is `gopi session raw`'s ONE flag table. --json/--text are
// absent on purpose: raw prints verbatim bytes and refuses them BY NAME before
// the scan (rejectedRawFlags, cli_raw.go).
//
//nolint:gochecknoglobals // the verb's flag table, read by its parser and by the G4 gate
var sessionRawFlagArity = sessionFlagArityTable{
	rawFlagRounds:     false,
	rawFlagSysprompt:  false,
	rawFlagUserprompt: false,
	rawFlagFile:       true,
	rawFlagLines:      true,
	rawFlagLast:       true,
	rawFlagBranch:     true,
	rawFlagWorktree:   true,
}

// logFlagArity reports whether `gopi session log` admits flag, and whether that
// flag claims the following token as its value. Admission and arity are ONE
// answer from ONE table: a flag cannot be admitted here and unclassified there.
func logFlagArity(flag string) (takesValue, admitted bool) {
	v, ok := sessionLogFlagArity[flag]
	return v, ok
}

// statsFlagArity is logFlagArity for `gopi session stats`.
func statsFlagArity(flag string) (takesValue, admitted bool) {
	v, ok := sessionStatsFlagArity[flag]
	return v, ok
}

// rawFlagArity is logFlagArity for `gopi session raw`.
func rawFlagArity(flag string) (takesValue, admitted bool) {
	v, ok := sessionRawFlagArity[flag]
	return v, ok
}

// sessionFlagNames returns the FLAG NAME (without the leading dashes, and with
// any attached `=value` cut) of every `--name` / `--name=value` token in args,
// in order. It answers "which flags were typed", never "which tokens are
// values" — deciding that is the scanner's job, and the whole point of the
// declaration pass below is to hand it the answer for the flags that do NOT
// take one.
func sessionFlagNames(args []string) []string {
	names := make([]string, 0, len(args))
	for _, a := range args {
		if len(a) < len(argvFlagPrefix) || a[:len(argvFlagPrefix)] != argvFlagPrefix {
			continue
		}
		name := a[len(argvFlagPrefix):]
		if eq := strings.Index(name, "="); eq >= 0 {
			name = name[:eq]
		}
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// sessionFlagDeclare is the ONE application of a verb's arity table, run BEFORE
// the scan: it refuses a flag the table does not admit (by name, Rule 15) and
// declares every admitted VALUELESS flag boolean, which is the scanner's own
// mechanism for "this flag never claims the next argument".
//
// WHY THE DECLARATION MUST PRECEDE parse(): memoryFlagSet.boolean is consulted
// inside parse() as the token is consumed. A valueless flag declared after the
// fact has already eaten its neighbour — that is precisely how the session id
// disappeared into `--json`.
//
// WHY IT REFUSES HERE RATHER THAN AFTER THE SCAN: the post-scan loop it replaces
// (`for _, fname := range fs.order { if !member(fname) { … } }`) walked the flags
// the SCANNER had recorded, which is a different set from the flags that were
// TYPED whenever a value happened to look like a flag. Refusing on the typed set
// is the honest reading of "unknown flag", and it is the same set the arity
// declaration needs.
func sessionFlagDeclare(fs *memoryFlagSet, args []string, arity func(string) (bool, bool)) error {
	for _, name := range sessionFlagNames(args) {
		takesValue, admitted := arity(name)
		if !admitted {
			return fmt.Errorf("unknown flag --%s", name)
		}
		if !takesValue {
			fs.boolean(name)
		}
	}
	return nil
}

// sessionFlagArityTables is the FAMILY's declaration, keyed by verb: every verb
// whose parser shares the memoryFlagSet scanner. It exists so the criterion can
// be checked over the family rather than verb by verb — a fourth verb joining
// the scanner without a table is a flag whose arity nobody stated, which is the
// defect's exact shape.
func sessionFlagArityTables() map[string]func(string) (bool, bool) {
	return map[string]func(string) (bool, bool){
		CliVerbLog:   logFlagArity,
		CliVerbStats: statsFlagArity,
		CliVerbRaw:   rawFlagArity,
	}
}
