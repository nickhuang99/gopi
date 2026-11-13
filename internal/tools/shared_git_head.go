package tools

// shared_git_head.go — the ONE walk that finds the `git` COMMAND WORD in a
// clause, for the readers that do not classify but RECOGNISE (`refGuardGitSubcommand`,
// `gitCommitInPreamble`, `stashWordIsInvocation`, the chat shell guards).
//
// ── WHY THE SHARED WALK EXISTS AT ALL ───────────────────────────────────────
//
// The caller asks a question about a shell word that may be a SHELL INTERPRETER
// (`bash`, `sh`, …) rather than about git. `bash` is in chatGitPreambleWords
// because `bash -c 'git commit …'` is a real spelling, but it is NOT in
// gitargv.OperatorWords: to gitargv it is a plain word and therefore "the git
// token is somebody's argument". So a `git`-only vocabulary would answer "no git
// here" for `bash -c '…'`, and the callers must keep their own answer for that
// shape.
//
// Hence ONE walk, parameterised by the wrapper vocabulary it walks over: git's
// own consumers pass gitargv.OperatorWords and get the wrapper→operand rule the
// leaf package publishes; the shell guards pass their wider table and keep
// reading `bash -c` the way they did. The important part is that the WALK is
// written once — four copies of "which word is git" is four chances for one of
// them to keep the operand-blind shape this family was repaired of (task
// 01a19d8a).
//
// THE RULE ITSELF IS gitargv.GitHead's, unmodified: gitargv.OperatorWords yields
// gitargv.GitHead exactly (the assignment-word and leading-`-` skips are the same
// two steps). This function only substitutes the table.

import "github.com/nick/gopi/internal/gitargv"

// gitCommandHeadIdx reports the index of the `git` command word in words when
// every word before it is a wrapper from wrappers, one of the wrapper's own
// OPERANDS (gitargv.OperandWords — `sudo -u nick git …`), one of the wrapper's
// own option words, or a shell assignment (`FOO=1`). ok=false means the git
// token is an argument to some other command (`echo git push`).
func gitCommandHeadIdx(words []string, wrappers map[string]bool) (int, bool) {
	return commandHeadIdx(words, wrappers, func(w string) bool { return w == gitBinaryName })
}

// commandHeadIdx is the walk with the HEAD PREDICATE supplied, because the chat
// shell guards ask the same question about a different head (`bash`, `sh`, …).
// Two callers, one walk: the wrapper→operand rule is then identical for `sudo -u
// nick git push` and for `sudo -u nick sh -c '…'`, and cannot be repaired in one
// and forgotten in the other (task 01a19d8a).
func commandHeadIdx(words []string, wrappers map[string]bool, isHead func(string) bool) (int, bool) {
	for i := 0; i < len(words); {
		w := words[i]
		if isHead(w) {
			return i, true
		}
		if !wrappers[w] && !gitargv.AssignmentWord(w) && !isOptionWord(w) {
			return 0, false
		}
		if consumes := gitargv.OperandWords[w]; wrappers[w] && consumes != nil &&
			i+1 < len(words) && consumes[words[i+1]] {
			i += 3 // the wrapper, its option, and the operand that belongs to it
			continue
		}
		i++
	}
	return 0, false
}

// isOptionWord reports whether w is a command-line option rather than a plain
// word: a leading `-` with something after it. Named because every head walk
// spells it, and one spelling is the difference between skipping an option and
// refusing a command. A lone `-` (stdin) is NOT an option here — it is left to
// the caller's refusal, the fail-closed side.
func isOptionWord(w string) bool {
	return len(w) > 1 && w[0] == '-'
}
