package tools

// git_push_writes.go — THE ONE PREDICATE that answers "does this `git push`
// write a ref?", and the flag vocabulary it reads.
//
// WHY IT IS ITS OWN FILE. Two guards ask this question and they must give the
// SAME answer:
//
//	git_publish_target_guard.go  (the clone-channel publish guard, a bash-tool
//	                              step: "is this destination one the channel
//	                              would not publish?")
//	chat_ops_authorization.go    (WAS: the chat op-category criterion, "is this a
//	                              REF MOVE?". Retired 2026-11-03, row 01a1a786)
//
// They were written apart, and the divergence is measured (task 01a17ddf,
// 01a17e4f): `3f15db556` taught the publish guard that a dry run writes nothing,
// while the category table still matched the WORDS `git push *` and classified
// `git push --dry-run origin master` as REF MOVE — refused, unconditionally, on
// the chat surface. The predicate lives here now so a second guard cannot
// re-derive it (Rule 13/14: one definition, referenced everywhere).

import (
	"strings"

	"github.com/nick/gopi/internal/gitargv"
)

// The DRY-RUN spellings (git-push(1): `-n`, `--dry-run`, and git's boolean
// assignment form `--dry-run=<bool>`). `3f15db556` introduced these as the
// publish guard's private constants; they moved here, unchanged in value and in
// meaning, so this file is their ONE definition (Rule 1/13/14).
const (
	gitPublishDryRunShort  = "-n"
	gitPublishDryRunLong   = "--dry-run"
	gitPublishDryRunAssign = gitPublishDryRunLong + "="
	// gitPublishBoolFalse is git's FALSE spelling for the boolean VALUE form.
	// MEASURED on this host (git 2.43.0, `git push -h` → `-n, --[no-]dry-run`):
	// push's `--dry-run` takes NO value, so BOTH `--dry-run=true` and
	// `--dry-run=false` are syntax errors that write nothing (`error: option
	// `dry-run' takes no value`, exit 129). The predicate still recognizes the
	// assignment form (a run may spell it), exempting any value that is not
	// `false`; the `false` spelling keeps its PREVIOUS verdict, which costs
	// nothing because that command can never write a ref. The genuinely
	// dangerous spelling on this git is the NEGATED one (`--no-dry-run`, a REAL
	// push) — it matches neither the short/long word nor the assignment prefix,
	// so it is judged exactly as before
	// (TestCloneGroundPublishTarget_DryRunSpellingsAreWhatGitAccepts pins all
	// three readings against git itself).
	gitPublishBoolFalse = "false"

	// gitPublishShortOptPrefix is the ONE dash a CLUSTERED short option starts
	// with; gitPublishShortDryRunLetter is `-n` inside such a cluster.
	gitPublishShortOptPrefix    = "-"
	gitPublishShortDryRunLetter = 'n'
	// gitPublishDoubleDashFlagPrefix is the LONG-option prefix, used to refuse a
	// word like `--dry-run` from the short-option cluster reader (`-` followed by
	// letters only is a cluster; `--x` is a long option and is read elsewhere).
	gitPublishDoubleDashFlagPrefix = "--"
)

// gitPushShortOptionsWithArgument holds push's short options that CONSUME A
// VALUE, taken from git-push(1) on this host (`-o <server-specific>` is the only
// one; `-v`, `-q`, `-n`, `-f`, `-d` take none). The letters after one belong to
// that value, not to the cluster, so a reader that kept scanning would read
// `-on` (push-option "n") as a dry run — the one false positive this table
// exists to prevent.
var gitPushShortOptionsWithArgument = map[byte]bool{'o': true}

// gitPushShortVetoLetters are the short options whose presence VETOES a dry-run
// reading of the same cluster: `-f` (--force) and `-d` (--delete), the two
// spellings the category table refuses by name. git accepts `-fn` as a real
// option cluster, and a forced/ref-deleting push is not a form this predicate
// clears on the strength of a letter that sits beside it — the conservative
// direction, matching chat_ops_authorization.go's rule that a refused class is
// never walked down by a flag it merely carries. MEASURED (2026-10-28):
// `git push -fn` is accepted by git; `git push -on …` is push-option "n".
var gitPushShortVetoLetters = map[byte]bool{'f': true, 'd': true}

// gitPublishIsDryRun reports whether this `git push` invocation is a DRY RUN.
//
// WHY A DRY RUN IS NOT JUDGED (the harm both callers exist for cannot occur):
// `--dry-run` makes git report what it WOULD do and then stop — no
// receive-pack is run, no object is sent, no ref on the server is created,
// moved or deleted. Every harm the two guards name therefore needs a REAL push:
// the publish guard's stray ref (`HEAD:<some name>`) is never created and the
// server's integration branch is never moved; the category criterion's "a
// published ref rewritten is not undone" never happens. So exempting a dry run
// cannot weaken either guard by one case: it is the SAME judgement applied to
// the set of invocations that write nothing.
//
// And it must be exempted, because this is precisely the command PROJECT.md
// Rule 6c tells a run to use to inspect its own publish addressing ("does this
// destination resolve to the branch my ground stands on?") without writing
// anything. Refusing it taught runs to stop checking and push blind, which is
// the opposite of what either guard is for (measured 2026-10-27, row 01a17ddf:
// a `git push --dry-run origin HEAD:refs/heads/rule31-probe` was refused with
// the sentence "Nothing was pushed." — true, and exactly why the refusal was
// wrong; and again on the chat surface, row 01a17e4f).
//
// The check is CLAUSE-LOCAL (the caller passes only the words of ONE push
// clause), so a dry run earlier on the line never opens a real push beside it.
func gitPublishIsDryRun(argv []string) bool {
	// ONLY THE PUSH'S OWN WORDS ARE SCANNED. A wrapper's option is not push's:
	// `nice -n 10 git push --force origin master` carries nice's `-n`, and reading
	// it as push's `--dry-run` demoted a REAL force-push to REPOSITORY WRITE /
	// allowed=true (measured by this row's probe, task 01a19d8a). The head is read
	// through the leaf package's ONE scan, so the wrapper operands that shifted
	// into view when the head walk was repaired are skipped here too.
	//
	// When the clause does not read as a git invocation the words are used as
	// given: that is the caller's previous verdict, and this predicate must never
	// widen on a shape it cannot resolve (fail-closed).
	if path := gitargv.SubcommandPath(argv); path != nil {
		argv = path[2:]
	}
	for _, w := range argv {
		switch w {
		case gitPublishDryRunShort, gitPublishDryRunLong:
			return true
		}
		// A CLUSTERED short option: git accepts `-nv` (`-n -v`) and `-fn`
		// (`-f -n`) as one word, and BOTH are commands a run writes. Judged by
		// walking the cluster letter by letter, so the reading is "does `-n`
		// appear as an OPTION of this clause", not "does the word contain the
		// letter n" — `origin/master`, `-1` and a refspec are not clusters.
		//
		// MEASURED (2026-10-28, a real bare server + a real clone, via the
		// repository's own fixture): `git push -nv origin HEAD:<branch>` wrote NO
		// ref (the server's tip was byte-identical before and after) and
		// `git push -n -v` is the same command — while the pre-fix predicate read
		// the cluster as a real push and the chat criterion refused it as REF MOVE.
		// The unit-literal test could not see it because it walked only the whole
		// spellings.
		//
		// The value-consuming options are the reason this is a table and not a
		// `strings.Contains`: `-on abc` sets push-option "abc", and reading the
		// `n` after `-o` as a dry run would clear a command that may do anything
		// the server's hook chooses. A veto letter (`-f`, `-d`) anywhere before
		// the scan ends keeps the conservative verdict.
		if dry, ok := gitPushShortClusterIsDryRun(w); ok {
			if dry {
				return true
			}
			continue
		}
		// git's boolean option syntax: a value may be spelled explicitly. MEASURED
		// (git 2.43.0, `git push -h` → `-n, --[no-]dry-run`): push's `--dry-run`
		// takes NO value, so BOTH `--dry-run=true` and `--dry-run=false` are
		// syntax errors that write nothing. The value form is still recognized
		// here (a run may spell it, and a future git may accept it), and the
		// `false` value keeps its PREVIOUS verdict rather than being exempted on
		// a theory. The dangerous spelling is the NEGATED one (`--no-dry-run`, a
		// REAL push): it matches neither the short/long word nor the assignment
		// prefix, so the guard still refuses it — which is exactly why this
		// predicate must not grow a substring test for "dry-run".
		// TestCloneGroundPublishTarget_DryRunSpellingsAreWhatGitAccepts pins all
		// three readings against git itself.
		if strings.HasPrefix(w, gitPublishDryRunAssign) {
			if !strings.EqualFold(strings.TrimPrefix(w, gitPublishDryRunAssign), gitPublishBoolFalse) {
				return true
			}
		}
	}
	return false
}

// gitPushShortClusterIsDryRun answers, for ONE word, whether it is a CLUSTER of
// short options (`ok`) and what it means for the dry-run question (`dry`). A
// word that is not a cluster at all answers ok=false, so the caller keeps its
// other readings.
//
// A cluster is one dash followed by at least one letter and NO other character
// class: `-nv`, `-fn`, `-qnv` are clusters; `-`, `--dry-run`, `-1`, `-.` and
// `origin/master` are not. Walking stops at the first value-consuming option
// (its operand is the REST of the word, per git's own short-option convention
// `-oVALUE`), and a veto letter anywhere before that stop keeps the verdict
// false — the conservative direction, so a clearance always needs the cluster
// to be READ, never merely to contain the letter.
func gitPushShortClusterIsDryRun(word string) (dry, ok bool) {
	if len(word) < 2 || !strings.HasPrefix(word, gitPublishShortOptPrefix) ||
		strings.HasPrefix(word, gitPublishDoubleDashFlagPrefix) {
		return false, false
	}
	for i := 1; i < len(word); i++ {
		c := word[i]
		if c < 'a' || c > 'z' {
			return false, false
		}
	}
	for i := 1; i < len(word); i++ {
		c := word[i]
		if gitPushShortVetoLetters[c] {
			return false, true
		}
		if gitPushShortOptionsWithArgument[c] {
			return false, true
		}
		if c == gitPublishShortDryRunLetter {
			return true, true
		}
	}
	return false, true
}

// THE SECOND CALLER IS GONE (2026-11-03, row 01a1a786), AND SO IS ITS QUESTION.
//
// This file used to end with `gitPushClauseWritesRef` — "is this clause a `git
// push` at all, and does it write a ref?" — because the chat op-category
// criterion needed to clear a push that wrote nothing. With every `git push`
// SPELLING now REPOSITORY WORK, no caller asks that composite question: the
// publish guard judges its own clauses (`gitPublishSegmentRefusal`, clause-local,
// asking `gitPublishIsDryRun` directly), so the predicate is deleted rather than
// left as an unused definition (Rule 43: a mechanism's leftovers are removed with
// it, and a green `go build` says nothing about a function nobody calls).
//
// What remains here is what a guard still needs: the DRY-RUN vocabulary and the
// predicate that reads it.
