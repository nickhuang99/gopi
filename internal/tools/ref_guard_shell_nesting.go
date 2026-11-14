package tools

import "strings"

// ref_guard_shell_nesting.go — the ref-guard family's PARSE layer: every simple
// command a shell command line will EXECUTE, including the ones the shell only
// executes as the BODY of a nesting construct.
//
// ── THE DEFECT THIS FILE EXISTS FOR (measured 2026-11-01, row 01a1964f) ─────
//
// The shared-checkout branch-create guard refused the bare spelling and let the
// wrapped one through, and git really ran:
//
//	git -C /home/user/work/gopi checkout -b task/x-legal
//	  → refused: "cannot create branch \"task/x-legal\" in /home/user/work/gopi:
//	    that directory is the declared shared checkout …"
//	OUT=$(git -C /home/user/work/gopi checkout -b task/x-legal 2>&1); echo "$OUT"
//	  → exit 0, git executed
//
// The cause is a PARSE fact, not a missing spelling. refGuardClauses split the
// line on `; & |` and newlines ONLY, so the second line produced ONE clause whose
// first word was `OUT=$(git`; refGuardGitSubcommand looks for `git` as the
// clause's command head (after wrapper words and git's own options), found none,
// and the whole clause was DROPPED — the git invocation inside the substitution
// was never a clause at all. Enumerating one more spelling (`$()`, then
// backticks, then `eval`, then `bash -c`) would have been the fifth patch on the
// same blind spot: the splitter did not model the shell's nesting.
//
// So the splitter keeps its clause boundaries and GAINS a second dimension: a
// clause now also yields the SEGMENTS that are executed by nested constructs
// inside it. Every consumer iterates segments instead of words, so the
// recogniser, the fail-closed arm and the refusal wording stay exactly where they
// were — one predicate, one sentence (Rules 13/14) — and a nesting form this file
// cannot read is REFUSED rather than skipped (see refGuardClauseSegments).

// The nesting vocabulary, named once (Rule 1: no bare literal decides a branch).
const (
	// refGuardShellSubstOpen is the two-character opener of a command
	// substitution. `$(` is spelled as a whole because the `$` alone is a
	// variable expansion (`$X`), which is a VALUE, not a command.
	refGuardShellSubstOpen = "$("
	// refGuardShellSubstOpenChar / ...CloseChar are the characters a balanced
	// substitution body is delimited by.
	refGuardShellSubstOpenChar  = '('
	refGuardShellSubstCloseChar = ')'
	// refGuardShellBacktickOpen / ...Close delimit the OTHER command-substitution
	// spelling: `` `git …` ``. Both characters are the same, which is why the
	// body scanner needs to know it is looking for a delimiter rather than a
	// depth count.
	refGuardShellBacktick = "`"
	// refGuardShellCommandFlag is the `-c` family: the flag that tells a shell
	// interpreter its command arrives as TEXT rather than as a file to source.
	// It is matched as a CONTAINS test inside a short-flag cluster (`-lc`,
	// `-ec`, `-eu -c`) because a shell accepts all of those.
	refGuardShellCommandFlag = "c"
	// refGuardShellFlagTerminator ends a shell interpreter's own option parsing.
	refGuardShellFlagTerminator = "--"
	// refGuardMaxShellNestingDepth bounds the recursion. A command line is
	// attacker-shaped text; the depth is bounded by its length, and the bound
	// makes that explicit rather than incidental.
	refGuardMaxShellNestingDepth = 8
	// refGuardShellPathSep splits a command word from its directory (`/bin/bash`
	// invokes the same interpreter as `bash`).
	refGuardShellPathSep = "/"
	// refGuardShellKshWord is `ksh`, the one interpreter this file names that
	// the console's own list (chatShellInterpreterWords) does not carry: the
	// ref guards are not console-only, so the vocabulary is stated here.
	refGuardShellKshWord = "ksh"
)

// refGuardShellCommandFlagWords are the interpreters that take their command as
// TEXT behind a `-c`-style flag: `bash -c '…'`, `sh -lc '…'`, `/bin/dash -ec …`.
//
// The spellings come from the package's existing constants (Rules 13/14: one
// spelling for each word), while MEMBERSHIP is stated here rather than inherited
// from chatShellInterpreterWords — that list also carries `eval` (which takes no
// flag, so it is judged separately) and `xargs` (which is a git-command PREAMBLE
// the recogniser already unwraps), and inheriting it would make both of those
// mean something they do not.
var refGuardShellCommandFlagWords = map[string]bool{
	shellBashCommandWord: true,
	shellShCommandWord:   true,
	shellZshCommandWord:  true,
	shellDashCommandWord: true,
	refGuardShellKshWord: true,
}

// refGuardShellNestedBodies returns the command TEXT a simple command executes
// through a nesting construct: every `$( … )` substitution, every “ `…` “
// substitution, and every shell interpreter's `-c` body.
//
// It reads the words the splitter produced (their quoting already reconciled) and
// answers with the INNER command text, unparsed — turning text back into commands
// is refGuardClauses' job, and doing it twice is how two splitters drift apart
// (Rule 13/14).
//
// WHAT IT DELIBERATELY DOES NOT DO — stated so a reader does not mistake it for
// coverage: a body a FILE supplies (`bash script.sh`, `source /tmp/x`, an alias,
// a function defined elsewhere) is out of reach of any text-level guard, and this
// one does not pretend otherwise.
func refGuardShellNestedBodies(words []string) (bodies []string, complete bool) {
	if len(words) == 0 {
		return nil, true
	}
	complete = true
	// (a) Command substitution and backticks: they can appear anywhere in the
	// words — a whole argument, a prefix (`OUT=$(git …)`), a suffix
	// (`git … $(date)`), or inside a redirect target.
	subBodies, subComplete := refGuardShellSubstitutions(strings.Join(words, refGuardWordJoin))
	bodies = append(bodies, subBodies...)
	complete = complete && subComplete
	// (a2) A bare `( … )` GROUP: it substitutes nothing, but its body is a
	// sequence of its own — `( cd /shared && git … )` runs that git in the
	// group's subshell. The splitter keeps the whole group in ONE clause (so its
	// separators cannot truncate it), which leaves the recogniser looking at a
	// clause whose head word is `(`; the body handed back here is what makes the
	// invocation visible again (measured, task 01a1964e: the group body was
	// DROPPED — no bodies, no segments, exit 0).
	if body, ok := refGuardGroupBody(words); ok {
		bodies = append(bodies, body)
	}
	// (b) A shell interpreter handed its command as text.
	for i, w := range words {
		head := refGuardShellWordBase(w)
		if head == "" || i+1 >= len(words) {
			continue
		}
		if head == commandWordEval {
			// `eval "<command text>"`: no flag, the body is the first operand.
			if body := refGuardInterpreterBody(words[i+1:], false); body != "" {
				bodies = append(bodies, body)
			}
			continue
		}
		if refGuardShellCommandFlagWords[head] {
			if body := refGuardInterpreterBody(words[i+1:], true); body != "" {
				bodies = append(bodies, body)
			}
		}
	}
	return bodies, complete
}

// refGuardGroupBody returns the text INSIDE a bare `( … )` subshell group, when
// the clause's word list is exactly that group.
//
// The shape this reads: the splitter keeps a group (and everything inside it) in
// ONE clause, so the words are `["(", "cd", "/shared", "&&", "git", "-C", …,
// ")"]` — the openers and closers are their own words because `(` and `)` are
// word boundaries in the shell. The body is the words between them, re-joined:
// the segment layer then parses that as the sequence it is, and a `cd` inside
// stays inside (the group is a SUBshell, which refGuardClauseSegmentsAt marks).
//
// A clause that opens a group without closing it — or closes one it never
// opened — answers false: an UNTERMINATED group is unreadable text, and the
// fail-closed path in refGuardShellSubstitutions/refGuardClauseSegmentsAt is
// where "unreadable" is already answered (Rule 15: never a silent pass).
func refGuardGroupBody(words []string) (string, bool) {
	if len(words) < 2 {
		return "", false
	}
	head := refGuardTrimOuterQuotes(words[0])
	if !strings.HasPrefix(head, string(refGuardShellSubstOpenChar)) {
		return "", false
	}
	// The opener shares its word with whatever followed it on the line
	// (`(git …` is the ordinary no-space spelling), so the first body word is
	// the rest of that word.
	body := []string{}
	if rest := strings.TrimPrefix(head, string(refGuardShellSubstOpenChar)); rest != "" {
		body = append(body, rest)
	}
	last := len(words) - 1
	tail := refGuardTrimOuterQuotes(words[last])
	if last == 0 || !strings.HasSuffix(tail, string(refGuardShellSubstCloseChar)) {
		return "", false
	}
	body = append(body, words[1:last]...)
	if trimmed := strings.TrimSuffix(tail, string(refGuardShellSubstCloseChar)); trimmed != "" {
		body = append(body, trimmed)
	}
	joined := strings.Join(body, refGuardWordJoin)
	if strings.TrimSpace(joined) == "" {
		return "", false
	}
	return joined, true
}

// refGuardShellWordBase reduces a command word to the interpreter name it
// invokes: `/bin/bash` → `bash`, `bash` → `bash`, `'bash'` → `bash`. A word that
// is not one of the interpreters answers "" without any map lookup, so a long
// argument cannot be mistaken for one.
func refGuardShellWordBase(word string) string {
	word = refGuardTrimOuterQuotes(word)
	if slash := strings.LastIndex(word, refGuardShellPathSep); slash >= 0 {
		word = word[slash+1:]
	}
	if refGuardShellCommandFlagWords[word] || word == commandWordEval {
		return word
	}
	return ""
}

// refGuardInterpreterBody returns the command text an interpreter's operand list
// carries, or "" when the operand list names a FILE instead.
//
// needsFlag distinguishes the two shapes: `bash` only executes TEXT behind its
// `-c` family (`bash -c 'git …'`, `bash -lc 'git …'`), while `eval` always
// executes text. Everything else about the operand list is skipped — the shell's
// own flags (`-e`, `-u`, `-x`, `--norc`) carry no command.
func refGuardInterpreterBody(rest []string, needsFlag bool) string {
	flagged := !needsFlag
	for idx, w := range rest {
		if w == refGuardShellFlagTerminator {
			flagged = flagged || needsFlag
			continue
		}
		if strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") {
			// A short-flag cluster. `-c` is what makes the NEXT word a command;
			// `-lc`/`-ec`/`-xc` are the ordinary combinations.
			if strings.Contains(w, refGuardShellCommandFlag) {
				flagged = true
			}
			continue
		}
		if strings.HasPrefix(w, "--") {
			continue // a long flag of the interpreter, never the command text
		}
		if !flagged {
			// `bash script.sh`, `bash /tmp/probe.sh`: a FILE, not text. No body.
			return ""
		}
		return refGuardJoinShellQuoted(rest[idx:])
	}
	return ""
}

// refGuardTrimOuterQuotes removes ONE matching pair of surrounding quotes from a
// shell word, leaving the inner text — quotes and all — intact. It is not
// stripQuotes (which removes every quote and is right for a fail-closed TOKEN
// scan): here the inner text is handed back to the splitter, which has its own
// quote model.
func refGuardTrimOuterQuotes(word string) string {
	for _, q := range []string{refGuardSingleQuote, refGuardDoubleQuote} {
		if len(word) >= 2 && strings.HasPrefix(word, q) && strings.HasSuffix(word, q) {
			return word[1 : len(word)-1]
		}
	}
	return word
}

// refGuardShellSubstitutions returns the bodies of every command substitution in
// a piece of shell text, in the order they appear.
//
// The scan is quote-aware in the one direction that matters: a SINGLE-quoted
// region performs no substitution at all, while a double-quoted region does
// (`"$(git rev-parse HEAD)"` is the ordinary idiom). An escaped character is
// data. A body's own quotes restart the scan, because the substitution inside
// `$( … )` is a fresh shell context.
func refGuardShellSubstitutions(text string) (out []string, complete bool) {
	complete = true
	const (
		unquoted = iota
		inSingle
		inDouble
	)
	state := unquoted
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch state {
		case inSingle:
			if string(c) == refGuardSingleQuote {
				state = unquoted
			}
		case inDouble:
			switch {
			case string(c) == refGuardDoubleQuote:
				state = unquoted
			case string(c) == refGuardEscapeChar:
				i++ // the escaped character is data
			case strings.HasPrefix(text[i:], refGuardShellSubstOpen):
				body, end, ok := refGuardBalancedSubstitution(text, i+len(refGuardShellSubstOpen))
				out = append(out, body)
				complete = complete && ok
				i = end
			case strings.HasPrefix(text[i:], refGuardShellBacktick):
				body, end, ok := refGuardBacktickBody(text, i+len(refGuardShellBacktick))
				out = append(out, body)
				complete = complete && ok
				i = end
			}
		default: // unquoted
			switch {
			case string(c) == refGuardSingleQuote:
				state = inSingle
			case string(c) == refGuardDoubleQuote:
				state = inDouble
			case string(c) == refGuardEscapeChar:
				i++
			case strings.HasPrefix(text[i:], refGuardShellSubstOpen):
				body, end, ok := refGuardBalancedSubstitution(text, i+len(refGuardShellSubstOpen))
				out = append(out, body)
				complete = complete && ok
				i = end
			case strings.HasPrefix(text[i:], refGuardShellBacktick):
				body, end, ok := refGuardBacktickBody(text, i+len(refGuardShellBacktick))
				out = append(out, body)
				complete = complete && ok
				i = end
			}
		}
	}
	return out, complete
}

// refGuardBalancedSubstitution reads a `$( … )` body starting just after the
// opener, honouring nested parentheses, quotes and escapes. It returns the body
// (the text BETWEEN the delimiters), the index of the closing delimiter, and
// whether the body was closed at all.
func refGuardBalancedSubstitution(text string, start int) (string, int, bool) {
	depth := 1
	quote := byte(0)
	for i := start; i < len(text); i++ {
		c := text[i]
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			if c == refGuardEscapeChar[0] && quote == refGuardDoubleQuote[0] {
				i++
			}
			continue
		}
		switch c {
		case refGuardSingleQuote[0], refGuardDoubleQuote[0]:
			quote = c
		case refGuardEscapeChar[0]:
			i++
		case refGuardShellSubstOpenChar:
			depth++
		case refGuardShellSubstCloseChar:
			depth--
			if depth == 0 {
				return text[start:i], i, true
			}
		}
	}
	// UNTERMINATED. The body is still returned: a command line the shell would
	// itself reject is still text a reader meant, and dropping it is the
	// silence this whole file exists to remove — the caller's own consumer
	// decides what an unterminated nesting means (refGuardClauseSegments
	// refuses it).
	return text[start:], len(text), false
}

// refGuardBacktickBody reads a “ `…` “ body starting just after the opener.
func refGuardBacktickBody(text string, start int) (string, int, bool) {
	for i := start; i < len(text); i++ {
		c := text[i]
		if c == refGuardEscapeChar[0] {
			i++
			continue
		}
		if string(c) == refGuardShellBacktick {
			return text[start:i], i, true
		}
	}
	return text[start:], len(text), false
}

// refGuardClauseSegment is ONE simple command the shell will execute: the words,
// the directory the shell has SELECTED for it, and whether it runs in a SUBSHELL
// (a `$( … )`, a backtick body, or an interpreter's `-c` body — every construct
// whose `cd` does not escape into the caller's shell).
type refGuardClauseSegment struct {
	runDir   string
	words    []string
	subshell bool
}

// refGuardClauseSegments flattens a clause into every simple command it executes
// — the clause's own, plus each nesting body's, recursively.
//
// A clause that CONTAINS a nesting construct this file cannot read is answered
// with ONE segment whose words are the clause's own plus a fail-closed marker
// verb: the splitter's blind spot was never the missing spelling, it was that
// "I did not understand this" read as "there is nothing here". A consumer that
// asks refGuardGitSubcommand for the segment's verb gets a verb no vocabulary
// knows, so the fail-closed arm refuses it BY NAME instead of dropping it.
//
// The bound: recursion depth is capped at refGuardMaxShellNestingDepth. A command
// line deeper than that is not a command anyone writes by hand — it is the shape
// that exists to make a guard lose track, and it is refused as unread (Rule 15: a
// scan that could not finish must not read as clean).
func refGuardClauseSegments(clause refGuardClause) []refGuardClauseSegment {
	return refGuardClauseSegmentsAt(clause, 0, nil)
}

// refGuardClauseSegmentsAt is the depth-carrying half; `walked` records the
// nesting texts already expanded so a construct that re-states its own body
// cannot multiply the walk.
func refGuardClauseSegmentsAt(clause refGuardClause, depth int, walked []string) []refGuardClauseSegment {
	out := []refGuardClauseSegment{{runDir: clause.runDir, words: clause.words}}
	if depth >= refGuardMaxShellNestingDepth {
		// Too deep to read: refuse rather than pass. The marker is a segment of
		// its own so the plain clause still gets judged on its own words.
		return append(out, refGuardUnreadNestingSegment(clause.runDir))
	}
	bodies, complete := refGuardShellNestedBodies(clause.words)
	for _, body := range bodies {
		if strings.TrimSpace(body) == "" {
			continue
		}
		if refGuardContainsString(walked, body) {
			continue // this exact body was already expanded from another opener
		}
		// A `cd` INSIDE a body belongs to the SUBSHELL and never reaches the
		// caller's shell, so the body's own clauses resolve their directories
		// from the base the outer clause already holds — never from the outer
		// `cd` carry, which is refGuardClauses' own business (untouched here).
		for _, inner := range refGuardClauses(body, clause.runDir) {
			for _, seg := range refGuardClauseSegmentsAt(inner, depth+1, append(walked, body)) {
				seg.subshell = true
				out = append(out, seg)
			}
		}
	}
	if !complete {
		// An UNTERMINATED nesting is text this file could not read to the end.
		// Silence here is exactly the defect this layer removes, so it becomes a
		// segment that the fail-closed arm refuses by name.
		out = append(out, refGuardUnreadNestingSegment(clause.runDir))
	}
	return out
}

// refGuardUnreadNestingSegment builds the fail-closed marker segment.
func refGuardUnreadNestingSegment(dir string) refGuardClauseSegment {
	return refGuardClauseSegment{
		runDir:   dir,
		words:    []string{refGuardUnreadNestingMarker},
		subshell: true,
	}
}

// refGuardUnreadNestingMarker is the fail-closed MARKER: a word that is no git
// verb and no wrapper, standing for "this clause nests a construct the parse
// layer could not read". A consumer asks refGuardGitSubcommand for the segment's
// verb and is told the marker as the verb, so the fail-closed arm refuses it BY
// NAME instead of the clause being dropped — the shape that let the wrapped
// spelling run.
const refGuardUnreadNestingMarker = "gopi-ref-guard-unread-nesting"

// refGuardSegmentUnread reports whether a segment carries the marker, i.e. the
// clause it came from nests something unreadable.
func refGuardSegmentUnread(words []string) bool {
	for _, w := range words {
		if refGuardTrimOuterQuotes(w) == refGuardUnreadNestingMarker {
			return true
		}
	}
	return false
}

// refGuardContainsString reports whether the list already holds s.
func refGuardContainsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// refGuardWordJoin is the separator a clause's words are re-joined with when the
// nesting scan needs the text back. Whitespace was the only thing the splitter
// consumed, so one space restores exactly what the scan reads.
const refGuardWordJoin = " "

// refGuardUnwrapShellHead normalises a clause whose FIRST WORD wraps the real
// command inside a nesting construct, WITHOUT changing the word count.
//
// `["OUT=$(git", "-C", "<shared>", "checkout", "-b", "x", "2>&1)"]` becomes
// `["git", "-C", "<shared>", "checkout", "-b", "x", "2>&1"]`.
//
// ── WHY THIS IS A PARSE FIX AND NOT ONE MORE SPELLING ───────────────────────
//
// The measured bypass was exactly that word shape, and it worked for a structural
// reason: the splitter cuts simple commands on `; & |`/newline only, so a
// substitution never became a clause of its own, and the recogniser — which reads
// a clause's COMMAND HEAD — saw `OUT=$(git`, found neither `git` nor a wrapper
// word it knew, and answered "this is not a git invocation", so the clause was
// dropped while git really ran in the shared checkout. Enumerating `OUT=`,
// `${X}=`, `2>` or `then` would each have been the fifth patch on that one blind
// spot; the head-opener unwrap is stated as a property of the word instead.
//
// THE WORD COUNT IS PRESERVED (Rule 14's readers depend on it): the head word
// loses its `NAME=$( ` prefix, and the LAST word surrenders exactly as many
// closing delimiters as the head opened (`x)` → `x`). A caller that computes a
// verb index as len(words)-len(rest)-1 therefore stays correct.
func refGuardUnwrapShellHead(words []string) []string {
	if len(words) == 0 {
		return words
	}
	head := refGuardTrimOuterQuotes(words[0])
	openers := refGuardShellOpenerCount(head)
	if openers == 0 {
		return words
	}
	at, width := -1, 0
	if i := strings.LastIndex(head, refGuardShellSubstOpen); i > at {
		at, width = i, len(refGuardShellSubstOpen)
	}
	if i := strings.LastIndex(head, refGuardShellBacktick); i > at {
		at, width = i, len(refGuardShellBacktick)
	}
	if at < 0 {
		return words
	}
	out := make([]string, len(words))
	copy(out, words)
	if rest := strings.TrimSpace(head[at+width:]); rest != "" {
		out[0] = rest
	}
	out[len(out)-1] = refGuardTrimCloseDelims(out[len(out)-1], openers)
	return out
}

// refGuardShellOpenerCount counts the nesting openers in one word's text.
func refGuardShellOpenerCount(word string) int {
	bodies, _ := refGuardShellSubstitutions(word)
	if len(bodies) > 0 {
		// The count that matters is how many delimiters the LAST word must
		// surrender, so the openers are counted directly rather than inferred.
		return strings.Count(word, refGuardShellSubstOpen) + strings.Count(word, refGuardShellBacktick)/2
	}
	return 0
}

// refGuardTrimCloseDelims removes up to n closing command-substitution delimiters
// from the end of a word.
func refGuardTrimCloseDelims(word string, n int) string {
	for n > 0 {
		switch {
		case strings.HasSuffix(word, string(refGuardShellSubstCloseChar)):
			word = word[:len(word)-1]
		case strings.HasSuffix(word, refGuardShellBacktick):
			word = word[:len(word)-len(refGuardShellBacktick)]
		default:
			return word
		}
		n--
	}
	return word
}

// ── TWO SHELL FACTS THE SPLITTER HAD TO LEARN ───────────────────────────────

// refGuardWordCarriesNestingOpener reports whether a shell word's VALUE opens a
// nesting construct — `OUT=$(git`, `X=\`git`, `B="$(git`.
//
// It exists for the splitter's environment-prefix rule. That rule is right (`env
// FOO=1 git …` is a git invocation, and the prefix is not the command head), but
// it was applied to a word whose value is a COMMAND SUBSTITUTION, and the
// assignment was consumed along with the command it runs. Measured on row
// 01a1964f: `OUT=$(git -C <shared> checkout -b X)` lost its head word before the
// clause was even built, so the recogniser saw `-C <shared> checkout -b X`.
func refGuardWordCarriesNestingOpener(word string) bool {
	if !envAssignPattern.MatchString(word) {
		return false
	}
	_, assign, found := strings.Cut(word, envAssignSep)
	if !found {
		return false
	}
	return strings.Contains(assign, refGuardShellSubstOpen) || strings.Contains(assign, refGuardShellBacktick)
}

// refGuardRedirectionAmpersands are the `&` SPELLINGS that belong to a
// REDIRECTION rather than to a clause boundary: `2>&1`, `>&2`, `&>file`.
//
// The splitter cut on every `&`, which turned `git … 2>&1` into two clauses and
// left the second one holding `1)`. Harmless on its own — until the first clause
// is the one a guard has to read, and the tail `1)` is what the refusal would
// quote. Naming the forms here keeps the cut rule in ONE place (Rule 14) instead
// of teaching every consumer to skip the debris.
var refGuardRedirectionAmpersands = map[string]bool{
	refGuardAmpGreaterDigit: true, // `>&1`
	refGuardAmpGreater:      true, // `&>file`
}

// The redirection spellings, named once (Rule 1).
const (
	refGuardAmpGreaterDigit = ">&"
	refGuardAmpGreater      = "&>"
	refGuardGreaterThan     = ">"
	envAssignSep            = "="
)

// refGuardIsClauseSeparator reports whether the character at this position CUTS a
// simple command. It reads the buffer so far, because an `&` is a clause boundary
// only when it is not part of a redirection operator (`2>&1`) — the buffer's last
// byte distinguishes the two: in `2>` the `>` is what precedes the `&`.
//
// `$(` and backticks are NOT separators for this reason: a command substitution is
// a nested command, which refGuardClauseSegments walks as its own segment. Cutting
// on them would produce a body fragment the recogniser cannot read AND destroy the
// `OUT=$(git` word shape that the wrapper unwrap needs.
func refGuardIsClauseSeparator(c rune, buf []byte) bool {
	lineSep := rune(refGuardLineSep[0])
	if c == lineSep {
		return !strings.ContainsRune(refGuardInLineSeparators, c)
	}
	if !strings.ContainsRune(refGuardInLineSeparators, c) {
		return false
	}
	if c != rune(refGuardAmpersand[0]) {
		return true
	}
	// An `&` after `>` (or after the buffer's `2>`) is a redirection, not a cut.
	if len(buf) > 0 && buf[len(buf)-1] == refGuardGreaterThan[0] {
		return false
	}
	return true
}

// refGuardAmpersand is `&`'s own spelling, named because two readers ask about it.
const refGuardAmpersand = "&"

// refGuardJoinShellQuoted restores the TEXT of an interpreter's body operand from
// the splitter's words.
//
// The splitter's words are whitespace-separated (strings.Fields), so a quoted body
// arrives in pieces — `sh -lc 'git -C /s checkout -b x'` is
// `["'git", "-C", "/s", "checkout", "-b", "x'"]`. Reading only the first piece
// (`'git`) produced a body the recogniser could not read, which is why the
// fail-closed arm refused nothing: the parse layer had reported success on a
// fragment. Rejoining until the opening quote CLOSES restores the body the shell
// will actually run.
func refGuardJoinShellQuoted(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	first := parts[0]
	if len(first) == 0 || (first[0] != refGuardSingleQuote[0] && first[0] != refGuardDoubleQuote[0]) {
		// An UNQUOTED body. Returning the first word alone is what made
		// `sh -c git -C <shared> update-ref refs/heads/x deadbeef` (the shape a
		// preceding stripQuotes produces from the quoted spelling) hand the
		// recogniser a one-word body `git`, which no verb reader can judge: the
		// ref face answered ZERO targets and the write went unjudged (measured,
		// task 01a1964e). The whole remaining text is returned instead.
		//
		// Reading MORE than the shell would is the fail-closed direction: the
		// words all come from the SAME clause, so nothing outside the command the
		// caller actually spelled can become a target.
		return strings.Join(parts, refGuardWordJoin)
	}
	quote := first[0]
	joined := first
	if refGuardQuoteCloses(joined, quote) {
		return refGuardTrimOuterQuotes(joined)
	}
	for _, next := range parts[1:] {
		joined += refGuardWordJoin + next
		if refGuardQuoteCloses(joined, quote) {
			return refGuardTrimOuterQuotes(joined)
		}
	}
	// Never closed: the text IS the body (the caller's completeness check is what
	// decides whether that is a refusal).
	return refGuardTrimOuterQuotes(joined)
}

// refGuardQuoteCloses reports whether the quote character opening the text also
// closes it — an ODD number of unescaped occurrences past the first is unbalanced.
func refGuardQuoteCloses(text string, quote byte) bool {
	count := 0
	for i := 0; i < len(text); i++ {
		if text[i] == refGuardEscapeChar[0] {
			i++
			continue
		}
		if text[i] == quote {
			count++
		}
	}
	return count >= 2 && count%2 == 0
}

// refGuardShellCompoundKeywords are the shell's clause-initial keywords. They
// begin a compound command and are never the command itself, so a recogniser
// reading the clause head sees a keyword instead of the git invocation behind it
// — measured on `if true; then git -C <shared> checkout -b X; fi`, where the
// clause `then git -C …` was dropped before this set existed.
//
// The set is the POSIX reserved words that can PRECEDE a command. The closers
// (`fi`, `done`, `esac`, `in`) are deliberately absent: they terminate a construct
// and no command follows them on the same clause, so skipping them would move the
// head past the end for no gain.
var refGuardShellCompoundKeywords = map[string]bool{
	shellKeywordIf:    true,
	shellKeywordThen:  true,
	shellKeywordElse:  true,
	shellKeywordElif:  true,
	shellKeywordWhile: true,
	shellKeywordUntil: true,
	shellKeywordDo:    true,
	shellKeywordNot:   true,
	shellKeywordBrace: true,
	shellKeywordExec:  true,
}

// The keyword spellings, named once (Rule 1).
const (
	shellKeywordIf    = "if"
	shellKeywordThen  = "then"
	shellKeywordElse  = "else"
	shellKeywordElif  = "elif"
	shellKeywordWhile = "while"
	shellKeywordUntil = "until"
	shellKeywordDo    = "do"
	shellKeywordNot   = "!"
	shellKeywordBrace = "{"
	shellKeywordExec  = "exec"
)
