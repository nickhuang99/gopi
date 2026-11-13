package tools

// chat_shell_heredoc.go — the HERE-DOCUMENT half of the chat surface's ONE
// tokenizer (chatShellTokens, chat_write_guard.go).
//
// THE DEFECT THIS REPAIRS (task 01a17e03-12f0, measured 2026-10-28). The
// tokenizer cut a command line into clauses by shell separators, and a
// HERE-DOCUMENT BODY carried those separators too — so every line of a message
// file written with `cat > reply.md <<'EOF' … EOF` became a clause of its own and
// was CLASSIFIED as if the shell were about to run it. A reply that merely QUOTED
// a command (`git branch -f main HEAD~3` inside an explanation, which is the ordinary
// shape of the console's own answers) therefore classified the whole line as
// REF-MOVE, and the console's one legal command —
//
//	cat > reply.md <<'EOF' … EOF
//	./bin/gopi notify --session <dir> --message "$(cat reply.md)"
//
// — was refused as "this moves a ref". Measured before the fix:
//
//	CLASS=REF MOVE
//	  clause=["cat" ">" "…/reply.md" "<<EOF"]
//	  clause=["##" "Q2"]
//	  clause=["git" "branch" "-f" "main" "HEAD~3" "#" "…"]
//	  clause=["EOF"]
//	  clause=["./bin/gopi" "notify" …]
//
// THE SHELL FACT, and it is the same one the ref guard already honours
// (shared_integration_ref_guard.go's clause scanner): a here-document body is
// DATA. The shell reads it as bytes for a command's stdin; it never parses it as
// commands. So a classified verb that appears only inside a body is not an
// invocation.
//
// BUT NOT ALWAYS — THE THREE CASES THAT KEEP THE BODY, and they are the shell's
// own switch, not a heuristic this reader invented.
//
//  1. THE COMMAND READING THE BODY IS A SHELL INTERPRETER
//     (`bash <<'EOF' … git push … EOF`): the body is precisely the script that
//     runs, so it MUST stay in the classified stream. Same for a line that pipes
//     into one (`cat <<'EOF' | bash`): the body reaches an interpreter through
//     the pipe.
//  2. THE DELIMITER IS UNQUOTED **AND** THE BODY EXPANDS. A here-document whose
//     delimiter bash did not have to quote is an EXPANDED body: backticks and
//     `$( … )` inside it are command substitutions the shell RUNS while it reads
//     the body, before the command that owns the heredoc ever sees a byte.
//     Measured (`bash -c`, git stubbed so a run is visible):
//
//	bash -c 'cat <<EOF
//	MARKER `git push origin master` TAIL
//	EOF'
//	  MARKER REAL-GIT-RAN: push origin master TAIL          ← it ran
//
//	bash -c 'cat <<EOF
//	$(git push origin master)
//	EOF'
//	  REAL-GIT-RAN: push origin master                      ← it ran
//
//     The SAME body under `<<'EOF'`, `<<"EOF"`, `<<\EOF` and the partially
//     quoted `<<E"OF"` prints the text and runs nothing — which is why the
//     delimiter's own quoting, and not the body's content, is the discriminator.
//     `<<E"OF"` is worth naming: ANY quote (or backslash escape) anywhere in the
//     delimiter makes the whole body literal for bash, so the operator struct
//     records "was this delimiter quoted at all", never "is it fully quoted".
//  3. THE TERMINATOR LINE CARRIES A PIPE. bash never accepts `EOF | bash` as a
//     terminator — measured: `cat <<'EOF' … EOF | cat -A` prints the `EOF | cat
//     -A` line as BODY TEXT and warns `here-document at line 1 delimited by
//     end-of-file`. This reader therefore refuses to treat such a line as a
//     safe terminator: a line inside the body that carries the delimiter, does
//     not strictly terminate, and carries an unquoted pipe keeps the body in the
//     classified stream.
//
// Every one of the three is fail-CLOSED — an unrecognised head, an unterminated
// body, an unquoted delimiter whose body expands, or SEVERAL here-documents of
// which one is kept, all keep the body. So the fix can make a line LESS
// restrictive only in the case that was measured: a body that reaches NO
// interpreter, through a QUOTED delimiter, on a line with no pipe.
//
// NOT A SECOND TOKENIZER. This file adds no scanner of its own: the delimiter
// extraction and the terminator comparison are the ref guard's
// (refGuardHeredocDelimiters / refGuardHeredocTerminates — Rule 14, one
// definition two consumers), and the caller is the single tokenizing loop in
// chat_write_guard.go.
//
// WHY THE DELIMITER IS READ AT THE CHARACTER LEVEL, and not from the parsed
// words. The tokenizer DROPS quote characters, so after tokenising `echo "<<EOF"`
// the word is `<<EOF` — indistinguishable from a real opener, and reading it as
// one would skip the following lines (a WIDENING of exactly the kind this fix
// must not create). The opener is therefore recognised where the information
// still exists: in the raw text, outside quotes, which is the same place the
// shell recognises it.

import (
	"strings"
)

// chatShellPipeSeparator is the pipe, spelled once here because two readers need
// it: the clause splitter (chatGitClauseSeparators carries it) and this file's
// fail-closed question "does a body written by a non-interpreter still reach an
// interpreter through a pipe?".
const chatShellPipeSeparator = '|'

// chatShellInterpreterWords are the command words whose ARGUMENTS are themselves
// commands. A body fed to one of them is a script, so it stays classified.
//
// The set is deliberately small and closed: it holds the shell interpreters the
// project's own guards already name (shellBashCommandWord … shellDashCommandWord,
// commandWordEval, xargsCommandWord) and nothing else, because every addition
// here removes protection from a body nobody re-reads.
var chatShellInterpreterWords = map[string]bool{
	shellBashCommandWord: true,
	shellShCommandWord:   true,
	shellZshCommandWord:  true,
	shellDashCommandWord: true,
	commandWordEval:      true,
	xargsCommandWord:     true,
}

// chatShellClauseIsInterpreter reports whether ONE simple command's words invoke
// a shell interpreter: the head (after any VAR=VAL prefix and any wrapper word —
// `sudo bash`, `env FOO=1 sh`) is one of chatShellInterpreterWords, spelled bare or
// with a path (`/bin/bash`).
func chatShellClauseIsInterpreter(words []string) bool {
	i, ok := commandHeadIdx(words, gitCommandPreambleWords, func(w string) bool {
		if slash := strings.LastIndexByte(w, '/'); slash >= 0 {
			w = w[slash+1:]
		}
		return chatShellInterpreterWords[w]
	})
	if !ok || i >= len(words) {
		return false
	}
	head := words[i]
	if slash := strings.LastIndexByte(head, '/'); slash >= 0 {
		head = head[slash+1:]
	}
	return chatShellInterpreterWords[head]
}

// chatShellHeredocDelimAt reports the delimiter word of a here-document operator
// that begins at index i in command (which MUST be the first `<` of an unquoted
// `<<`), the index just past the delimiter, and whether i begins one at all.
//
// The three operators are told apart the way bash tells them apart: `<<<` is the
// here-STRING (it feeds the next word on the SAME line and opens no body), `<<-`
// allows a tab-stripped body, and `<<` is the ordinary one. A single `<` is a
// plain redirection and opens nothing.
//
// The delimiter itself may be QUOTED (`<<'EOF'`, `<<"EOF"`) or backslash-escaped;
// the quotes are not part of it, so they are skipped here rather than in the
// terminator comparison — but WHETHER ONE APPEARED is reported, because that
// single fact decides whether the body is literal or EXPANDED (the file header
// states the measurement: any quote or backslash anywhere in the delimiter makes
// the whole body literal for bash, and an unquoted delimiter makes every backtick
// and `$( … )` in the body a command the shell RUNS).
func chatShellHeredocDelimAt(command string, i int) (delimWord string, end int, quoted, ok bool) {
	if i+1 >= len(command) || command[i] != '<' || command[i+1] != '<' {
		return "", i, false, false
	}
	j := i + 2
	if j < len(command) && command[j] == '<' {
		return "", i, false, false // the here-STRING operator: no body follows
	}
	if j < len(command) && command[j] == '-' {
		j++
	}
	for j < len(command) && (command[j] == ' ' || command[j] == '\t') {
		j++
	}
	var delim strings.Builder
	var quote byte
	for ; j < len(command); j++ {
		c := command[j]
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			delim.WriteByte(c)
			continue
		}
		switch {
		case c == '\'' || c == '"':
			quote = c
			quoted = true
		case c == '\\' && j+1 < len(command):
			j++
			delim.WriteByte(command[j])
			quoted = true
		case c == ' ' || c == '\t':
			return delim.String(), j, quoted, delim.Len() > 0
		case strings.IndexByte(chatGitClauseSeparators, c) >= 0:
			return delim.String(), j, quoted, delim.Len() > 0
		default:
			delim.WriteByte(c)
		}
	}
	return delim.String(), j, quoted, delim.Len() > 0
}

// chatShellHeredocOpener is ONE here-document opened on a line: the delimiter
// bash looks for, and whether that delimiter was QUOTED (or backslash-escaped) —
// the one fact that decides whether the body is LITERAL or EXPANDED (the file
// header carries the measurement; an unquoted delimiter makes every backtick and
// `$( … )` in the body a command the shell runs).
type chatShellHeredocOpener struct {
	delim  string
	quoted bool
}

// chatShellHeredocSkip reports where the tokenizer must resume after a line whose
// here-documents open bodies that are DATA, so those bodies never become tokens.
// It returns next == start (nothing to skip) when the bodies must stay classified.
//
// A body stays classified for FOUR reasons now, all of them fail-closed:
//
//	keptByPipe        the line carries an unquoted pipe (the body may reach an
//	                  interpreter that way — `cat <<'EOF' | bash`).
//	keptByInterpreter the command reading the body IS a shell interpreter
//	                  (`bash <<'EOF' … EOF`).
//	an UNQUOTED delimiter — bash EXPANDS such a body, so its backticks and
//	                  `$( … )` are commands that run before the owner sees a
//	                  byte. Measured in the file header.
//	a line that CARRIES the delimiter and more (`EOF | bash`) — bash accepts no
//	                  such line as a terminator, so the reader must not silently
//	                  swallow whatever follows on it.
//
// Deciding per-command would need a finer reading than this scanner has, and the
// wrong answer is a widening, so ONE kept here-document keeps EVERY body opened
// on the line.
func chatShellHeredocSkip(command string, start int, openers []chatShellHeredocOpener, keptByPipe, keptByInterpreter bool) int {
	if len(openers) == 0 || keptByPipe || keptByInterpreter {
		return start
	}
	for _, o := range openers {
		if !o.quoted {
			return start
		}
	}
	next, ok := chatShellSkipHeredocBodies(command, start, openers)
	if !ok {
		return start
	}
	return next
}

// chatShellSkipHeredocBodies consumes the body lines of the here-documents opened
// on the line that just ended, starting at index start (the character AFTER that
// line's newline). It returns the index of the first character that is not body
// data — the start of the next line after the last terminator, or len(command)
// when a body never terminates (the shell reads to EOF too, and so does this
// reader, which is the reading that keeps the bodies' text out of the stream).
//
// ok is FALSE when the body must not be skipped after all: a line that carries
// the delimiter AND MORE (`EOF | bash`) is not a terminator bash would accept —
// measured, `cat <<'EOF' … EOF | cat -A` prints that whole line as body text and
// warns `delimited by end-of-file` — so whatever is written after the delimiter
// still has to be classified. The caller then keeps the body.
//
// The delimiters are consumed in the order they were opened and each body ends at
// ITS OWN terminator, which is what bash does with several here-documents on one
// line. The comparison is refGuardHeredocTerminates, which tolerates the `<<-`
// tab strip and a trailing CR.
func chatShellSkipHeredocBodies(command string, start int, openers []chatShellHeredocOpener) (int, bool) {
	i := start
	for _, o := range openers {
		terminated := false
		for i < len(command) {
			nl := strings.IndexByte(command[i:], refGuardLineSep[0])
			var line string
			if nl >= 0 {
				line = command[i : i+nl]
				i += nl + 1
			} else {
				line = command[i:]
				i = len(command)
			}
			if refGuardHeredocTerminates(line, o.delim) {
				terminated = true
				break
			}
			if chatShellHeredocLineCarriesTail(line, o.delim) {
				return start, false
			}
		}
		if !terminated {
			return i, true
		}
	}
	return i, true
}

// chatShellHeredocLineCarriesTail reports whether line starts with the delimiter
// and then carries something ELSE — the `EOF | bash` shape, which bash does not
// accept as a terminator. The prefix test is done on the same trimmed form the
// terminator comparison uses (a `<<-` body's leading tabs, a trailing CR), so a
// line that already terminates never reaches here.
func chatShellHeredocLineCarriesTail(line, delim string) bool {
	if delim == "" {
		return false
	}
	trimmed := strings.TrimRight(strings.TrimLeft(line, refGuardHeredocTabTrim), "\r ")
	rest, ok := strings.CutPrefix(trimmed, delim)
	if !ok || rest == "" {
		return false
	}
	return strings.TrimSpace(rest) != ""
}
