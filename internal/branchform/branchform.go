// Package branchform is the ONE definition of the question "does this git
// command line CREATE a branch, and is the spelling it uses DESTRUCTIVE?".
//
// WHY A LEAF PACKAGE (PROJECT.md Rule 14). The predicate is spoken by the shipped
// instruction surfaces (GOPI.md's branch-creation step, the strings gopi renders
// into a child's briefing) and by the gate that checks them
// (internal/standards). A predicate copied into each of those is Rule 1b's
// defect and Rule 13's drift: the day the vocabulary changes, one copy keeps the
// old spelling and a caller silently stops recognising the form it exists to
// catch. This package imports the standard library only — no project import, so
// no consumer can create a cycle (the internal/toolnames and internal/smokegate
// precedent).
//
// THE DEFECT THIS VOCABULARY NAMES (measured 2026-09-24, see
// devdoc/branch-creation-forms.md). `git worktree add <path> -b <branch>` — the
// spelling GOPI.md prescribes at three sites — REFUSES a name that already
// exists:
//
//	$ git checkout -b feat/x                 # feat/x exists
//	fatal: A branch named 'feat/x' already exists.
//
// That refusal is loud and safe. Its force siblings are neither:
//
//	$ git checkout -B feat/x                 # feat/x exists, holds another task's commits
//	Switched to and reset branch 'feat/x'
//
// The branch is RESET to the new base and the commits it held are reachable only
// through the reflog (measured: `git reflog show feat/x` → "branch: Reset to
// HEAD", the victim commit one entry below). Nothing is refused, nothing is
// reported, and the other task's ref — which the ledger, the merger and the
// worktree sweep all treat as that task's work — now points somewhere else.
//
// So the two classes are not "convenient vs strict", they are:
//
//   - REFUSED forms — `-b`, `checkout -b`, `switch -c`, `worktree add -b`:
//     git itself is the guard, because the create forms never overwrite an
//     existing ref. No package here needs to detect them.
//   - DESTRUCTIVE forms — `-B`, `checkout -B`, `switch -C`, `worktree add -B`,
//     the force-into-place pair `checkout/switch -b|-c … -f|-F|--force`, and
//     `branch -f|--force`: these exist to overwrite, that is their whole
//     semantics, and they are what this package reports.
//
// SCOPE, deliberately narrow (a gate that fires on prose gets disabled):
//
//   - A command line is examined one SEGMENT at a time, segments split on the
//     shell separators (`;`, `&&`, `||`, `|`, a backtick, a newline). A flag in
//     a later command never makes an earlier `git checkout` destructive:
//     `git checkout -b x && git checkout -f y` is two commands, one of them
//     innocent.
//   - A mention of a flag WITHOUT a verb is invisible (`-B` in prose, in a rule
//     table, in the vocabulary of this very package's docs). Only a verb plus a
//     flag in one segment is a form. That is what lets the rule file NAME the
//     banned flags without tripping its own gate.
//   - `git branch -D` / `git branch -d` (deliberate deletion, which GOPI.md
//     Rule 9 prescribes for an archived loser) is NOT a destructive form: it is
//     loud, it names its ref, and it is the documented archiving step.
//   - `git worktree remove --force` is NOT this package's subject: it removes a
//     checkout, it does not create or reset a branch.
package branchform

import "strings"

// The git verbs that can move a branch ref, named once (Rule 1b). They are
// compared against whole tokens, never searched for as substrings.
const (
	// VerbCheckout / VerbSwitch are git's two branch-moving verbs. `git
	// checkout` also serves file restores, so the CREATE flags below decide
	// whether a given line is a branch creation at all.
	VerbCheckout = "checkout"
	VerbSwitch   = "switch"
	// VerbBranch names the refs-only verb (`git branch -f x <base>` moves a ref
	// without touching the worktree).
	VerbBranch = "branch"
	// VerbWorktree is the plumbing verb GOPI.md's branch-creation step uses;
	// SubAdd is the subcommand that creates both the checkout and the branch.
	VerbWorktree = "worktree"
	// SubAdd is the only worktree subcommand that creates a branch.
	SubAdd = "add"
)

// The branch-CREATING flags, named once: git itself is the source of truth for
// their meaning, and this package is the one place they are spelled.
const (
	// FlagCreate / FlagSwitchCreate are the safe spellings — `git checkout -b
	// <name>` / `git switch -c <name>`. They REFUSE an existing name, which is
	// why they need no entry in Form: git is already the guard.
	FlagCreate       = "-b"
	FlagSwitchCreate = "-c"
	// FlagCreateForce / FlagSwitchCreateForce are the destructive pair: they
	// create the branch if it is absent and RESET it to HEAD if it is not.
	FlagCreateForce       = "-B"
	FlagSwitchCreateForce = "-C"
	// FlagBranchForce / FlagBranchForceLong move an existing ref (`git branch
	// -f <name> <base>`) — the same destruction through the refs-only verb.
	FlagBranchForce     = "-f"
	FlagBranchForceLong = "--force"
)

// forceFlagNames is the force vocabulary a checkout/switch line must not combine
// with a create flag: each of these means "do not stop for anything", which is
// exactly the choice being taken away from the task that owns the ref. It is
// ordered as the report should quote them.
var forceFlagNames = []string{FlagBranchForce, "-F", FlagBranchForceLong, "--discard-changes"}

// shellSeparators are the characters that end one simple command and begin
// another. A backtick is included because the shipped surfaces quote commands in
// markdown code spans: the span delimiter is not part of the command.
const shellSeparators = ";&|`\n"

// Form is one destructive branch-creation spelling found in a line.
type Form struct {
	// Verb is the git verb the form was found under (VerbCheckout, VerbSwitch,
	// VerbBranch, VerbWorktree), so a report can name the command rather than
	// the line.
	Verb string
	// Flag is the destructive flag that was found: FlagCreateForce,
	// FlagSwitchCreateForce, FlagBranchForce, or a force flag combined with a
	// create flag.
	Flag string
	// Base is the safe create flag it was found beside, when the destruction
	// comes from a force flag combined with a create flag (`checkout -b x -f`).
	// Empty for the self-sufficient force-create spellings.
	Base string
	// Text is the segment the form was found in, trimmed — the evidence a
	// report quotes.
	Text string
}

// Destructive reports every destructive branch-creation form on one LINE of
// text. It is the package's whole surface: pure and deterministic, so a gate can
// call it per line of a file and a test can call it per fixture line.
//
// The input is a LINE, never a whole file: the shipped surfaces are instructions
// (markdown prose and code spans, Go string literals a briefing renders), and an
// instruction is one command per line in every one of them.
func Destructive(line string) []Form {
	var out []Form
	for _, seg := range segments(line) {
		out = append(out, destructiveInSegment(seg)...)
	}
	return out
}

// segments splits one line into simple commands on the shell separators.
func segments(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		return strings.ContainsRune(shellSeparators, r)
	})
}

// destructiveInSegment reports the destructive forms of ONE simple command.
func destructiveInSegment(seg string) []Form {
	toks := strings.Fields(seg)
	if len(toks) == 0 {
		return nil
	}
	// Locate the verb that governs this segment: the LAST of our verbs, because
	// a command line spells the binary and its options apart (`git -C <dir>
	// worktree add …`) and the flags belong to the innermost verb.
	vi, verb := -1, ""
	for i, t := range toks {
		switch trimGlue(t) {
		case VerbCheckout, VerbSwitch, VerbBranch, VerbWorktree:
			vi, verb = i, t
		}
	}
	if vi < 0 {
		return nil
	}
	rest := toks[vi+1:]

	// A token may carry punctuation that Fields cannot split (a markdown comma
	// that is not a separator, a `>` redirect glued on). Flags are compared
	// after trimming that glue, so `-B,` is still `-B`.
	hasFlag := func(flag string) bool {
		for _, t := range rest {
			if trimGlue(t) == flag {
				return true
			}
		}
		return false
	}

	switch verb {
	case VerbWorktree:
		// Only `worktree add` creates a branch, and only -B makes it a reset.
		if !hasFlag(SubAdd) {
			return nil
		}
		if hasFlag(FlagCreateForce) {
			return []Form{{Verb: verb, Flag: FlagCreateForce, Text: strings.TrimSpace(seg)}}
		}
		return nil
	case VerbBranch:
		// `git branch -f <name> <base>` moves an existing ref. `-D`/`-d` are
		// deliberate deletions and are out of scope (see the package doc).
		for _, f := range []string{FlagBranchForce, FlagBranchForceLong} {
			if hasFlag(f) {
				return []Form{{Verb: verb, Flag: f, Text: strings.TrimSpace(seg)}}
			}
		}
		return nil
	}

	// checkout / switch.
	forceCreate := ""
	switch {
	case hasFlag(FlagCreateForce):
		forceCreate = FlagCreateForce
	case hasFlag(FlagSwitchCreateForce):
		forceCreate = FlagSwitchCreateForce
	}
	if forceCreate != "" {
		return []Form{{Verb: verb, Flag: forceCreate, Text: strings.TrimSpace(seg)}}
	}
	// The force-into-place pair: a create flag AND a force flag on the same
	// command. Neither is destructive alone (a create with no force refuses an
	// existing name; a force with no create restores files).
	base := ""
	switch {
	case hasFlag(FlagCreate):
		base = FlagCreate
	case hasFlag(FlagSwitchCreate):
		base = FlagSwitchCreate
	}
	if base == "" {
		return nil
	}
	for _, f := range forceFlagNames {
		if hasFlag(f) {
			return []Form{{Verb: verb, Flag: f, Base: base, Text: strings.TrimSpace(seg)}}
		}
	}
	return nil
}

// trimGlue strips the punctuation a shell line glues onto a token without making
// it a separator of its own: a markdown comma, a redirect, quotes, a trailing
// backslash. It never removes a leading dash, so `-B` stays `-B`.
func trimGlue(tok string) string {
	return strings.Trim(tok, `,.)"'\\>`)
}
