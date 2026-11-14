// Package gitargv is the ONE reader of GIT'S OWN COMMAND-LINE VOCABULARY: the
// global options that stand BETWEEN the `git` word and the subcommand, the fact
// that some of them consume the FOLLOWING word as their operand, and the wrapper
// commands (`sudo`, `env`, `xargs`, …) that may stand in front of `git` itself —
// together with the operands THOSE wrappers consume.
//
// ── WHY IT IS A PACKAGE AND NOT A FUNCTION SOMEBODY ALREADY HAS ─────────────
//
// Two independent readers needed this knowledge and had no legal way to share it:
// `internal/tools` (the ref guard, the stash guard, the commit trailer, the chat
// push legs) and `internal/chatpolicy` (the category table's prefix match, which
// `internal/tools` imports — so chatpolicy cannot import tools back). The
// alternative is what a previous round measured the cost of: the SECOND table,
// written because copying one word looked cheaper than moving it. A duplicate of
// git's option list drifts the moment either copy learns a new option, and the
// drift is invisible — both copies keep compiling and answering.
//
// So the vocabulary lives here, once, exported, in a LEAF package: no business
// logic, no project imports (Rule 14). Every consumer reads these values rather
// than restating them.
//
// ── WHAT A CALLER MAY USE THIS FOR, AND WHAT IT MUST NOT ────────────────────
//
// SubcommandPath answers "which subcommand does this clause invoke, and what
// follows it" — for a CLASSIFICATION or a RECOGNITION that must see the
// subcommand regardless of how the operator spelled the globals in front of it.
// It is NOT a general git argument parser: it does not know which options belong
// to which subcommand, and it deliberately answers NOTHING (nil) for a clause
// that does not invoke git at all, so a caller can tell "no git here" from "git
// with no subcommand".
//
// A caller whose judgement DEPENDS ON WHERE git will run (`-C <dir>`,
// `--git-dir=…`) must keep reading the ORIGINAL words for that question: the
// operand is resolved by the caller's own probe, not here. This package only
// says which words are an option and which is the subcommand.
package gitargv

import "strings"

// BinaryName is the git command word itself. Named because three packages match
// it and a literal in three places is three chances to disagree.
const BinaryName = "git"

// DashCDirOption is `git -C <dir>`'s option word — the one global option that
// re-points the invocation at a directory rather than merely skipping a word.
// Named (Rule 13) because a reader that RESOLVES it (the ref guard, the chat push
// legs) and the table that says it CONSUMES an operand must agree on one
// spelling.
const DashCDirOption = "-C"

// FlagValueSep is the separator of git's attached long-option spelling
// (`--git-dir=<path>`), which is one word on the command line but two values to a
// reader.
const FlagValueSep = "="

// GitDirOption is git's global option naming the repository. Exported because a
// caller that refuses to measure a push it cannot resolve (chat_push_fastforward)
// looks for exactly this one among the words, and it must spell it the same way
// the table below does.
const GitDirOption = "--git-dir"

// OptionsWithOperand are git's own GLOBAL options that consume the FOLLOWING word
// as their operand, so the word after one of them is NOT git's subcommand.
//
// It is a map rather than a slice because every reader asks "is this word one of
// them" per word; the write is never done (no caller mutates it) and the intent is
// a set.
//
// WHY THE LIST IS SHORT AND WHY THAT IS THE HONEST SHAPE: these are the globals
// that take a SEPARATE word. An option that takes no value (`--no-pager`, `-p`) or
// carries its value attached (`--git-dir=/p`) needs no entry — a word starting
// with `-` is already skipped by the scanner.
var OptionsWithOperand = map[string]bool{
	DashCDirOption: true,
	"-c":           true,
	GitDirOption:   true,
	"--work-tree":  true,
	"--namespace":  true,
}

// ── THE WRAPPERS, AND THE OPERANDS *THEY* CONSUME ───────────────────────────
//
// A wrapper word in front of `git` (`sudo git …`, `xargs git …`) does NOT mean
// the next word is `git`: most of them take their own operands first, and an
// operand is a plain word. `sudo -u the operator git push --force` and
// `env FOO=1 git push --force` both spell a REAL git invocation, and a scanner
// that reads every plain word before `git` as "an argument to some other
// command" answers "there is no git here" for both of them.
//
// THAT WAS A MEASURED DEFECT, not a hypothetical one (task 01a19d8a): with the
// operand-blind scan, `sudo -u the operator git push --force origin master` and
// `env FOO=1 git reset --hard HEAD~5` were classified OPERATOR WORK /
// allowed=true — the class that RUNS — while the directory-level spelling of the
// same hole (`git -C <dir> push --force`) had already been closed by 01a19d6e.
// One wrapper operand was enough to bypass the ref-move fence.
//
// THE DISCRIMINATOR IS CONSUMPTION, and it is the only one that survives:
//
//	`echo git push`       — `git` is an ARGUMENT of `echo`  ⇒ no git invocation
//	`env FOO=1 git push`  — `FOO=1` is ENV'S OWN operand     ⇒ a git invocation
//
// Both have a plain word before `git`; what separates them is whether that word
// was EATEN by the wrapper's own option. So the wrapper table says which of its
// words each wrapper consumes, and the scan skips exactly those.
//
// ── THE DIRECTION OF EVERY UNCERTAINTY IS FAIL-CLOSED ───────────────────────
//
// A word this package cannot account for ends the scan with "no git here" —
// which leaves every caller on the answer it already had. Guessing the other way
// (treating an unknown shape as a git invocation anyway) would let the
// SUBCOMMAND be read off a word that is really somebody's argument, and a
// mis-read subcommand is not harmless: a read command read as `push --force` is
// refused, i.e. the operator's own hand's-breadth work (Rule 37) stops working.
// "Cannot tell" therefore never means "assume git".

// OperatorWords are the wrapper commands that may legitimately precede the `git`
// command word (`sudo git stash pop`, `xargs git stash drop`). Any OTHER plain
// word before `git` — one no wrapper's operand rule below accounts for — means
// the git token is an argument to some other command (`echo git stash`) and the
// clause holds no git invocation at all.
var OperatorWords = map[string]bool{
	"sudo": true, "doas": true, "env": true, "time": true,
	"nice": true, "nohup": true, "command": true, "xargs": true, "exec": true,
}

// OperandWords are each wrapper's OWN options that consume the FOLLOWING word as
// their operand, so that word is neither `git`'s command word nor a foreign
// command's argument — it belongs to the wrapper.
//
// It is keyed by the wrapper and the value is a per-wrapper SET, for the same
// reason OptionsWithOperand is a set: the question is always "does THIS wrapper
// consume on THIS word". A wrapper that appears in OperatorWords with NO row here
// (`nohup`) takes no operand at all, which is a fact worth stating by absence
// rather than by an empty map a reader would have to interpret.
//
// THE LIST IS THE OPERANDS THAT OCCUR IN FRONT OF A COMMAND, not an exhaustive
// option catalogue of each tool: an option missing here is one that takes no
// separate operand (a `-E`, `-H`, `-r` flag), and the leading-`-` rule already
// skips those. Adding a row is how a new measured spelling gets read.
var OperandWords = map[string]map[string]bool{
	// `sudo -u <user> git …`, `-g <group>`, `-p <prompt>`, `-C <n>`, `-h <host>`,
	// `-r <role>`, `-t <type>`, `-U <user>`, `-D <dir>`, `-T <timeout>`.
	"sudo": {
		"-u": true, "-g": true, "-p": true, "-C": true, "-h": true,
		"-r": true, "-t": true, "-U": true, "-D": true, "-T": true,
	},
	// `doas -u <user> git …`, `-C <n>`.
	"doas": {"-u": true, "-C": true},
	// `env -u <name> git …`, `-C <dir>`, `-S <string>`; the BARE `NAME=VALUE` form
	// is env's operand by SHAPE rather than by flag — see AssignmentWord, which the
	// scan applies after these.
	"env": {"-u": true, "-C": true, "-S": true},
	// `nice -n <num> git …`.
	"nice": {"-n": true},
	// `time -f <format> git …`, `-o <file>`.
	"time": {"-f": true, "-o": true},
	// `xargs -n <n> git …`, `-I <replace>`, `-P`, `-L`, `-s`, `-E`, `-d`, `-a`.
	"xargs": {
		"-n": true, "-I": true, "-P": true, "-L": true,
		"-s": true, "-E": true, "-d": true, "-a": true,
	},
	// `command -v <name>`, `exec -a <name>`. Both wrappers otherwise take no
	// operand; `nohup` takes none at all (no row).
	"command": {"-v": true, "-V": true},
	"exec":    {"-a": true},
}

// AssignmentWord reports whether w is a shell VARIABLE ASSIGNMENT word
// (`FOO=1`, `LC_ALL=C`, `PATH=/usr/bin:/bin`): a leading identifier followed by
// `=`.
//
// It is exported because two shapes consume it and both readers must agree:
//
//   - `env FOO=1 git …` — the assignment is env's own operand;
//   - `FOO=1 git …`     — a bare assignment prefix is not a command at all; the
//     shell strips it and runs `git`.
//
// The identifier rule is the shell's: a letter or `_`, then letters, digits or
// `_`; the FIRST `=` ends it. So `FOO=1` and `_A9=2` are assignments, while
// `a/b=c` (a path carrying an `=`) and `=x` (nothing before it) are not.
func AssignmentWord(w string) bool {
	for i := 0; i < len(w); i++ {
		c := w[i]
		if c == FlagValueSep[0] {
			return i > 0
		}
		letter := c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		digit := c >= '0' && c <= '9'
		if !letter && !(digit && i > 0) {
			return false
		}
	}
	return false
}

// GitHead reports the index of the `git` command word in words, or ok=false when
// the clause invokes no git at all — the ONE scan every consumer shares, so the
// ref guard, the stash guard, the commit trailer, the chat guards and the
// category table cannot disagree about what a git invocation looks like.
//
// The walk, in order:
//
//  1. `git`                    → report its index;
//  2. a wrapper's consuming option (`sudo -u <user>`, `env -u <name>`) → skip
//     the wrapper, the option AND the operand;
//  3. a wrapper word with no consuming option here (`nohup`, `sudo -E`) → skip
//     the word;
//  4. an assignment word (`env FOO=1`, a bare `FOO=1` prefix) or any other
//     option word (leading `-`) → skip it;
//  5. any other plain word → it is an argument to another command: no git.
//
// Step 2 is the whole point of this row. Before it, steps 3-5 read `-u`'s
// operand `the operator` as a plain foreign word and answered "no git here".
//
// A wrapper ALREADY KNOWN but asked with an option this table does not carry
// falls to step 5's plain-word refusal rather than to a guess — the fail-closed
// direction. That is deliberate: the alternative (assume the next plain word is
// git's command word) would swallow a real operand and read a subcommand off it.
func GitHead(words []string) (int, bool) {
	for i := 0; i < len(words); {
		w := words[i]
		if w == BinaryName {
			return i, true
		}
		// A WRAPPER IS ONE OF OperatorWords, whether or not it has an operand row:
		// `nohup` takes no operand at all, and asking OperandWords alone made it a
		// plain foreign word — `nohup git push --force` then read as "no git here"
		// (measured by this row's own probe, before the table was consulted in this
		// order).
		if OperatorWords[w] {
			if consumes := OperandWords[w]; consumes != nil && i+1 < len(words) && consumes[words[i+1]] {
				i += 3 // the wrapper, its option, and the operand that belongs to it
				continue
			}
			i++ // the wrapper word itself; its own flag words are skipped below
			continue
		}
		if AssignmentWord(w) || strings.HasPrefix(w, "-") {
			i++
			continue
		}
		return 0, false
	}
	return 0, false
}

// SubcommandPath returns the clause as `git <subcommand> <words after it>`, with
// git's own global options, any wrapper words AND THE OPERANDS THOSE WRAPPERS
// CONSUME removed — or nil when the clause's command head is not `git`.
//
// nil means one of two things and the caller must not merge them: no git word at
// all (`echo git push`), or a git invocation with NO subcommand (`git -C`, an
// option whose operand is missing — a line git itself rejects). Both are "there
// is no subcommand to read", which is all a matcher needs to know; anything that
// needs to distinguish them reads the words it already has.
//
// Both spellings of an operand-taking option are honoured: `--git-dir <p>` and
// `--git-dir=<p>`; `-C <dir>` and the attached `-Cdir`. An option that carries no
// operand is skipped by its leading `-`, so an option this table does not know
// cannot be mistaken for the subcommand — the fail-closed direction: an unknown
// global is SKIPPED, and the real subcommand behind it is still found.
func SubcommandPath(words []string) []string {
	head, ok := GitHead(words)
	if !ok {
		return nil
	}
	for i := head + 1; i < len(words); i++ {
		w := words[i]
		if name, attached := optionName(w); attached && OptionsWithOperand[name] {
			// The operand is inside this word (`--git-dir=/p`).
			continue
		}
		if OptionsWithOperand[w] {
			// The next word is this option's operand, not the subcommand.
			i++
			continue
		}
		if strings.HasPrefix(w, "-") {
			continue
		}
		path := make([]string, 0, len(words)-i+1)
		path = append(path, BinaryName, w)
		return append(path, words[i+1:]...)
	}
	return nil
}

// Subcommand returns the git SUBCOMMAND the clause invokes, the words after it,
// and whether the clause invokes git at all — the same reading
// `SubcommandPath` builds, in the shape a guard that answers about ONE verb
// (`refGuardGitSubcommand`, `gitCommitInPreamble`) wants to consume it.
//
// It exists so those guards share the scan rather than each walking the words
// again: four copies of "which word is git" is four chances for one of them to
// keep the operand-blind shape this package was just repaired of.
func Subcommand(words []string) (verb string, rest []string, ok bool) {
	path := SubcommandPath(words)
	if path == nil {
		return "", nil, false
	}
	return path[1], path[2:], true
}

// optionName splits an attached option spelling at FlagValueSep and reports
// whether the separator was there at all. `--git-dir=/p` → ("--git-dir", true);
// `--no-pager` → ("", false) — and an empty name is never in the table, so the
// caller's `attached` test needs no second guard.
func optionName(word string) (string, bool) {
	name, _, found := strings.Cut(word, FlagValueSep)
	if !found {
		return "", false
	}
	return name, true
}
