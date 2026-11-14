package tools

// git_commit_task_trailer.go — the LEDGER REFERENCE every commit a run makes
// carries, so `git log --grep` is the only index anyone needs.
//
// ── THE DEFECT THIS CLOSES ───────────────────────────────────────────────────
//
// A commit says nothing about the row that produced it. Measured on this
// checkout (2026-10-26, `git log -200 --format=%B | grep -c '^task:'` → 0):
// not one of the last 200 commits carries a ledger reference, so "which of
// these commits is that failed row's output?" has no machine answer. The
// ledger records `deliverable.branch`, and for a CLONE-CHANNEL ground that
// value is `master` — the INTEGRATION branch, i.e. a name shared by every row
// of the channel — so `git merge-base --is-ancestor <deliverable.branch> master`
// is trivially true for all of them and answers nothing. The association has to
// live in the commit itself, and the index must be git's own: `git log --grep`
// needs no table, no index file and no daemon (Rule 35 / Rule 5b).
//
// ── THE MECHANISM, AND WHY IT IS HERE ────────────────────────────────────────
//
// A run creates commits through the BASH TOOL (`git commit -m "<subject>"` is
// what every worker does), so the tool is the one place that can add the line
// without
// touching history and without a hook: `git commit --trailer` is a COMMAND-LINE
// spelling that needs no `prepare-commit-msg` script and no config key. Both
// facts were measured on this host's git 2.43.0 in a scratch repository:
//
//	git commit -m "subject" --trailer "task: <id>"   → body = subject\n\ntask: <id>
//	git config trailer.task.key task (no --trailer)  → body = subject   (NO trailer)
//	alias.commit = 'commit --trailer "task: <id>"'   → ignored (git refuses to
//	                                                   shadow a builtin with an alias)
//	GIT_CONFIG_* alias injection                     → same, ignored
//
// So the trailer is injected into the command TEXT, once, right after the
// `commit` word of each `git commit` invocation this command contains. Nothing
// is written to disk, nothing persists (Rule 35 satisfied by construction).
//
// ── THE SECOND REFERENCE: WHICH MACHINE MADE IT ─────────────────────────────
//
// `git log` says nothing about the HOST that produced a commit, so across the
// fleet (host-a / hp / zpeer) review and triage are blind on that axis: a
// commit is visible but its author-machine is not. The same seam carries a
// second trailer, `<HostTrailerKey>: <local hostname>`.
//
// WHY THIS SEAM AND NOT A PROSE RULE. The obvious placement is the operator's
// own system policy (`~/.gopi/GOPI.md`, "put your hostname in the commit
// message"). It is rejected, on three measured grounds:
//
//	1. IT DOES NOT TRAVEL. `~/.gopi/GOPI.md` is one host's local file, so the
//	   instruction is absent on every machine that was never configured with
//	   it — exactly the machines whose commits you cannot attribute.
//	2. IT IS ADVISORY. A prose rule is obeyed when the model reads it and
//	   forgets it when the context is long, the run is mechanical, or the model
//	   is not a gopi run at all (`git commit` typed by hand, by a script, by
//	   `make`). Nothing observes whether it was obeyed, so nothing can report
//	   that it was not (Rule 15).
//	3. IT IS UNVERIFIABLE AFTER THE FACT. A commit missing the line looks
//	   exactly like a commit whose host had no hostname — there is no witness.
//
// The bash tool, by contrast, is the ONE place every run's commits pass through
// (`git commit -m "<subject>"` is what every worker does), so the line is added
// whether
// or not anyone remembered, on every host, with no file to configure — and the
// injection is ANNOUNCED (below), so the one case it cannot cover is visible
// rather than silent. It is the same mechanism, not a second one: one scan, one
// set of exemptions, one notice channel (Rule 35 — no new dependency).
//
// On which host names are accepted: `os.Hostname()` failing or returning a
// blank is the only case where the line is skipped, because a made-up machine
// name is worse than no line. A name that cannot be spelled safely inside the
// injected single quotes is refused by the same charset guard the ledger id
// uses — an apostrophe in a hostname would otherwise close the quote and turn a
// trailer into further COMMAND.
//
// ── WHAT IT DELIBERATELY DOES NOT DO ────────────────────────────────────────
//
//   - It never rewrites an EXISTING commit (`--amend` clauses are skipped): the
//     trailer belongs to the commit being created; an amend of a commit this
//     run already made carries the line already, and `--trailer` APPENDS, so
//     injecting again would duplicate it (measured).
//   - It never injects into a QUOTED string or a HERE-DOCUMENT body. Both are
//     DATA, not commands — a `git commit` written inside a document being
//     written is the ordinary case (this repository's own briefings contain
//     those four words), and injecting there would corrupt the document. The
//     scan is therefore quote- and heredoc-aware, exactly like the ref guards
//     that read the same command text.
//   - It does not cover commit CREATION that is not `git commit` (`git merge`
//     auto-commit, `git commit-tree`, `git cherry-pick`): none of them accepts
//     `--trailer`, and covering them means building messages gopi does not
//     build. This is stated in the task report rather than papered over.
//   - It does not reach a `git commit` written inside COMMAND SUBSTITUTION
//     (`x=$(git commit -m "<subject>")`): `$`, `(` and `)` are ordinary word
//     characters to
//     this scanner, so no clause boundary falls between `$(` and the verb
//     (measured — the rewrite leaves such a command untouched). Closing that
//     means teaching the scanner subshell nesting, which is a change to a
//     scanner three guards share; it belongs in its own row.
//   - It is silent when no ledger row is known (a chat, a hand-run gopi): a run
//     with no row has nothing honest to cite, and a made-up id is worse than no
//     trailer.

import (
	"os"
	"strings"

	"github.com/nick/gopi/internal/gitcommit"
)

// The trailer's TWO spellings have one home each (Rules 1/13/14): the key and
// the line format. A reader that greps for the association, a test that asserts
// it, and the injection itself all reference these, never a typed literal.
const (
	// TaskTrailerKey / TaskTrailerLineFmt / HostTrailerKey / HostTrailerLineFmt
	// are ALIASES, not copies (Rule 13/14): their ONE definition is in
	// internal/gitcommit, because the trailer is written through two doors —
	// this command-text injection and gitcommit.Commit, which gopi's own Go
	// callers use — and two copies of the spelling is exactly the drift the
	// single-source rule forbids. A literal typed here would be a bare string
	// the seam gate (git_commit_seam_gate_test.go) fails on.
	TaskTrailerKey     = gitcommit.TaskTrailerKey
	TaskTrailerLineFmt = gitcommit.TaskTrailerLineFmt
	HostTrailerKey     = gitcommit.HostTrailerKey
	HostTrailerLineFmt = gitcommit.HostTrailerLineFmt

	// gitCommitVerb is git's commit-creating verb. It is the ONE subcommand the
	// injection recognises, and it is read from this constant everywhere.
	gitCommitVerb = "commit"

	// gitCommitTrailerFlag is the command-line spelling that appends a trailer
	// (git >= 2.32). It is spelled once so the injection and the "a caller
	// already took control" check cannot drift.
	gitCommitTrailerFlag = "--trailer"

	// gitCommitAmendFlag is the flag that REWRITES an existing commit rather
	// than creating one — see the header: amend clauses are skipped.
	gitCommitAmendFlag = "--amend"

	// trailerValueQuote is the quoting every injected value gets — both the
	// ledger id and the host name. They are uuid-shaped and DNS-label shaped
	// respectively, so single quotes are both sufficient and literal-safe in
	// every POSIX shell (and both pass trailerValueSpellable, which is what
	// makes that claim true rather than hopeful).
	trailerValueQuote = "'"

	// gitCommitTrailerNoticeFmt announces an injection on stderr, naming every
	// line that was added. The tool never silently executes a command other
	// than the one the model wrote — the same Rule 15 discipline
	// checkoutHomeStripRecord states for a stripped variable. It carries the
	// WHOLE set of lines because there are two of them now: announcing one and
	// injecting two would be exactly the silent half Rule 15 is about.
	gitCommitTrailerNoticeFmt = "[commit trailer added: %s]\n"
)

// TaskTrailerLine renders the ledger reference for a task id: `<key>: <id>`.
// It returns "" for a blank id — a reference to nothing is not a reference.
func TaskTrailerLine(taskID string) string {
	return gitcommit.TaskTrailerLine(taskID)
}

// HostTrailerLine renders the machine reference for a host name:
// `<HostTrailerKey>: <name>`. It returns "" when the host cannot be named — an
// empty or unspellable name has no honest line to add, and a guessed machine is
// worse than a missing one (Rule 15 / Rule 41: an unmeasured fact is never
// reported as a measured one).
func HostTrailerLine(host string) string {
	// The RAW value is validated, never a trimmed one: padding is exactly the
	// shape that a shell word-split would silently normalise, and "it looked
	// fine after I trimmed it" is how a value that never appeared in a commit
	// gets reported as one that did. The validation itself is gitcommit's, so
	// the VALUE is judged by one function for both doors.
	return gitcommit.HostTrailerLine(host)
}

// localHostTrailerLine is the host line for THIS machine. It is the ONE reader
// of the OS hostname for this mechanism (Rule 2's spirit — one place), so the
// injection, its notice and its test all ask the same function rather than
// each calling os.Hostname and disagreeing on the failure shape.
func localHostTrailerLine() string {
	return gitcommit.LocalHostTrailerLine()
}

// gitCommitTrailerLines returns the trailer LINES every commit this process
// creates should carry, in injection order: the ledger row first (when one was
// published) and the host name second (when the machine can be named).
//
// The two are INDEPENDENT on purpose. A ledger row is published only to a
// dispatched run, so a chat or a hand-run gopi has none — but it is still a
// machine making commits, and "which host wrote this?" is a question that must
// have an answer for those commits too. Requiring both would put the host
// trailer behind the ledger seam for exactly the runs that need attribution
// most.
func gitCommitTrailerLines() []string {
	// The line SET (which trailers exist, their order, and that the host line
	// does not depend on the ledger row) is gitcommit's, so the two doors that
	// write a trailer cannot drift apart. What is local is WHERE the row comes
	// from: the environment seam that publishes it to every child.
	return gitcommit.Trailers(os.Getenv(TaskRunIDEnv))
}

// gitCommitTaskTrailerRewrite adds a `--trailer "<line>"` for every line
// gitCommitTrailerLines reports to every `git commit` invocation in command
// that does not already carry one, and reports what it added.
//
// The ledger row comes from the environment seam that already publishes it to
// every child (TaskRunIDEnv, written once by the spawn seam — Rule 2). An unset
// or unusable row means "nothing was published": that line is skipped and the
// host line still goes in. A command with no commit at all is returned
// unchanged, whatever lines are available.
func gitCommitTaskTrailerRewrite(command string) (string, []string, bool) {
	lines := gitCommitTrailerLines()
	if len(lines) == 0 {
		return command, nil, false
	}
	ends := gitCommitInsertionPoints(command)
	if len(ends) == 0 {
		return command, nil, false
	}
	// ONE `--trailer <value>` PER LINE. The tempting shorter spelling — a
	// single `--trailer "task: X host: Y"` with the lines space-joined — was
	// measured on git 2.43.0 and REJECTED: git writes that as ONE body line
	// (`task: X host: Y`), so the host becomes part of the ledger value and
	// neither `git log --grep="task: X"` nor `--grep="host: Y"` matches a
	// trailer of its own. Two flags produce two lines (measured), which is
	// what the two axes require.
	injection := ""
	for _, line := range lines {
		injection += " " + gitCommitTrailerFlag + " " + trailerValueQuote + line + trailerValueQuote
	}
	// Build the rewritten text right to left so the earlier offsets stay valid.
	out := command
	changed := false
	for i := len(ends) - 1; i >= 0; i-- {
		end := ends[i]
		if end < 0 || end > len(out) {
			continue
		}
		out = out[:end] + injection + out[end:]
		changed = true
	}
	if !changed {
		return command, nil, false
	}
	return out, lines, true
}

// trailerValueSpellable is the guard that keeps a hostile or malformed value
// out of a shell single-quoted injection: an apostrophe in the value would
// close the quote and turn the trailer into further COMMAND. It is applied to
// BOTH trailer values — the ledger id (uuid-shaped: hex and '-') and the host
// name (DNS-label shaped: letters, digits, '-', '.', '_') — so a value that
// could break out is refused and its line is not injected at all; the command
// then runs exactly as the caller wrote it. An id is data, and data never
// becomes command.
func trailerValueSpellable(value string) bool {
	return gitcommit.Spellable(value)
}

// gitCommitInsertionPoints returns the byte offsets (in command) just past the
// `commit` word of every `git commit` invocation that still needs the trailer.
//
// WHY A SECOND SCAN rather than offsets from refGuardClauses: that scanner
// judges a QUOTE-STRIPPED copy of the command (its callers pass
// stripQuotes(command)), so its offsets address a different string. The VALUES
// this scan dispatches on — the quote characters, the separators, the preamble
// words, git's options-with-operand table, the git word itself — are the shared
// constants of the guard family (Rule 14), referenced and never re-typed; what
// is local is the word/offset walk, which has to run on the caller's own text.
func gitCommitInsertionPoints(command string) []int {
	var ends []int
	var word []byte
	var wordEnd int
	var quote byte
	var pendingDelims []string
	var clauseWords []string
	// pendingTrailer defers the decision to the END of the simple command: a
	// clause that turns out to carry its own --trailer or --amend loses it.
	pendingTrailer := -1

	// flushWord closes the word accumulated in word.
	flushWord := func() {
		if len(word) == 0 {
			return
		}
		w := string(word)
		word = word[:0]
		clauseWords = append(clauseWords, w)
		if w == gitCommitVerb && gitCommitInPreamble(clauseWords) {
			pendingTrailer = wordEnd
		}
		if w == gitCommitTrailerFlag || strings.HasPrefix(w, gitCommitTrailerFlag+"=") {
			pendingTrailer = -1
		}
		if w == gitCommitAmendFlag {
			pendingTrailer = -1
		}
	}

	// flushClause closes the simple command: its here-document delimiters start
	// NOTHING in a comment-worthy sense until the next newline, and a deferred
	// trailer is committed here.
	flushClause := func() {
		flushWord()
		if pendingTrailer >= 0 {
			ends = append(ends, pendingTrailer)
			pendingTrailer = -1
		}
		pendingDelims = append(pendingDelims, refGuardHeredocDelimiters(clauseWords)...)
		clauseWords = clauseWords[:0]
	}

	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote == 0 && len(pendingDelims) > 0 {
			// Inside a here-document body the rest of this LINE is DATA.
			if nl := strings.IndexByte(command[i:], refGuardLineSep[0]); nl >= 0 {
				line := command[i : i+nl]
				if refGuardHeredocTerminates(line, pendingDelims[0]) {
					pendingDelims = pendingDelims[1:]
				}
				i += nl
				continue
			}
			break
		}
		if quote == 0 {
			// A `#` that BEGINS a word starts a comment: the rest of the line is
			// text, and text is not a command.
			if c == refGuardCommentPrefix[0] && len(word) == 0 {
				if nl := strings.IndexByte(command[i:], refGuardLineSep[0]); nl >= 0 {
					i += nl - 1
					continue
				}
				break
			}
			if c == refGuardSingleQuote[0] || c == refGuardDoubleQuote[0] {
				quote = c
				word = append(word, c)
				continue
			}
			if c == refGuardEscapeChar[0] {
				if i+1 < len(command) {
					word = append(word, command[i+1])
					wordEnd = i + 2
					i++
				}
				continue
			}
			if strings.ContainsRune(refGuardInLineSeparators+refGuardLineSep, rune(c)) ||
				strings.ContainsRune(refGuardWordGap, rune(c)) {
				flushWord()
				if strings.ContainsRune(refGuardInLineSeparators+refGuardLineSep, rune(c)) {
					flushClause()
				}
				continue
			}
			word = append(word, c)
			wordEnd = i + 1
			continue
		}
		// Inside a quote: everything is data.
		if c == quote {
			quote = 0
			word = append(word, c)
			wordEnd = i + 1
			continue
		}
		if c == refGuardEscapeChar[0] && quote == refGuardDoubleQuote[0] && i+1 < len(command) {
			word = append(word, command[i+1])
			wordEnd = i + 2
			i++
			continue
		}
		word = append(word, c)
		wordEnd = i + 1
	}
	flushClause()
	return ends
}

// gitCommitInPreamble reports whether the words collected so far in this simple
// command reach the given word as a GIT SUBCOMMAND — i.e. a `git` word precedes
// it, separated only by git's own global options and their operands
// (`git -C <dir> commit`, `sudo git -C <dir> commit`). It reuses the guard
// family's preamble table and options-with-operand map rather than deciding
// afresh what a git invocation looks like.
func gitCommitInPreamble(words []string) bool {
	// The last word IS the verb under judgement; everything before it is the
	// preamble.
	pre := words[:len(words)-1]
	// Leading process wrappers (`sudo`, `env`, `nohup`), their own flag words AND
	// the operands those flags consume (`sudo -u nick git …`) — the shared walk, so
	// this reader cannot keep the operand-blind shape the ref guard was repaired of
	// (task 01a19d8a).
	head, ok := gitCommandHeadIdx(pre, gitCommandPreambleWords)
	if !ok {
		return false
	}
	i := head + 1
	// Between `git` and the subcommand only git's own global options and their
	// operands may appear.
	for i < len(pre) {
		w := pre[i]
		if gitOptionsWithOperand[w] {
			i += 2
			continue
		}
		if strings.HasPrefix(w, "-") {
			i++
			continue
		}
		// A non-option word that is not the last preamble word is not a
		// subcommand separator: this is not a `git <opts> commit` shape.
		if i != len(pre)-1 {
			return false
		}
		return false
	}
	// `git` directly followed by the verb.
	return i == len(pre)
}
