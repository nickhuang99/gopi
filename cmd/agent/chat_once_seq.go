package main

// chat_once_seq.go — `gopi chat --once` with REPL LINES instead of one prompt.
//
// WHY THIS FILE EXISTS. chat_once.go runs the ordinary REPL (runChat) with its
// prompt already in hand, and that is the whole design: the turn a script drives
// must be the turn a human types. The consequence nobody wrote down is that the
// REPL is a LINE LOOP, and its input is not a prompt — it is a transcript a human
// types at a `you>` prompt: `/help`, `/task list`, `/quit`, and ordinary
// sentences between them. Folding that into ONE message (chatOnceOneLine) handed
// the model a `/help` line it was never supposed to see, and running only the
// first command hung up on the message the caller wrote after it. A driver could
// not script the surface a human types into.
//
// THE RULE, and why it is this narrow. The input is read as REPL lines as soon as
// the FIRST line is a slash command (isSlashCommand — the SAME "/" predicate the
// interactive REPL and the web carrier branch on, never a second spelling). That
// is the caller saying "I am scripting a session": the lines go to the REPL's own
// reader in order and its own arms run them — the command switch, the session
// commands, the model turns — with no second command table anywhere (Rule 3; the
// R4 one-table contract in commands.go). Input that does NOT open with a command
// is untouched: a single message stays one message, and a multi-line prompt is
// still folded into ONE message (chatOnceOneLine), which is the documented
// behaviour of every `--once` run that came before this file.
//
// WHENCE the answer goes: the REPL's own flow output, exactly as a piped
// `gopi chat` writes it. This is not a second output contract — the sequence face
// IS a piped `gopi chat`, one session, so a command's result and a turn's answer
// land on the stream a script already knows how to read. The one-prompt face
// keeps its stricter "stdout is the answer alone" contract, because that face is
// still one turn with nothing else to say.
//
// WHAT THE EXIT STATUS MEANS (Rule 15 — a caller is never handed a silent 0):
//
//	0  every command ran and the session ended the caller's own way (/quit, or
//	   end of input);
//	1  something the sequence asked for FAILED: a command that could not be
//	   honoured (the REPL's own refusal, now carrying the status it deserves —
//	   before this it printed the refusal and exited 0, so a driver could not
//	   tell a scripted command that ran from one that was rejected), or a turn
//	   that failed;
//	2  usage (no input at all; reported by the caller).
//
// The failure path is observed through chatOnceFailureObserver (chat_once.go):
// the REPL's own command arms report their refusal through ONE seam, so this face
// learns about it without parsing its own stderr and without a second dispatch.

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// chatOnceLineSep is the line separator of a scripted transcript. It is the byte
// the split and the join must agree on (Rule 1b).
const chatOnceLineSep = "\n"

// chatOnceSeqLine is ONE line of the scripted transcript, with the REPL's own
// leading/trailing blank space removed — the REPL trims a line before it decides
// anything (cli_chat.go's `msg := strings.TrimSpace(line)`), so the face's own
// command test must see the same bytes the REPL will.
//
// A line is a COMMAND line iff it opens with the shared "/" trigger. Anything
// else is a message for the model.
func chatOnceSeqLine(line string) string { return strings.TrimSpace(line) }

// chatOnceSeqIsScriptedLine reports whether one scripted line belongs to a
// scripted SESSION transcript — the caller is driving the REPL rather than
// asking one question. That is every line the shared classifier does not call
// plain text: a COMMAND line (the REPL's own arms run it) or the literal-send
// ESCAPE (the REPL sends it to the model as text). Both are lines a human types
// at a `you>` prompt, so both make the input a transcript; a line that is
// neither is the one-prompt face's message, unchanged.
//
// It asks the SAME classifier the interactive carriers ask (classifyChatLine,
// commands.go) with NO vocabulary: whether a word is accepted is the REPL's
// business — a word it does not accept is still a command line, and the REPL
// refuses it in its own voice.
func chatOnceSeqIsScriptedLine(line string) bool {
	return classifyChatLine(line, nil).kind != chatLineText
}

// chatOnceSeqFromArgv reports the scripted lines a caller typed as argv: every
// element that is itself a command line, taken VERBATIM as one REPL line (so
// `/help task` is one element and stays one line). Only argv elements count —
// a command word inside a sentence is prose, which is exactly what the "/" rule
// means.
func chatOnceSeqFromArgv(argv []string) []string {
	var lines []string
	for _, a := range argv {
		if chatOnceSeqIsScriptedLine(a) {
			lines = append(lines, chatOnceSeqLine(a))
		}
	}
	return lines
}

// chatOnceSeqFromStdin splits piped text into transcript lines, dropping blanks.
func chatOnceSeqFromStdin(text string) []string {
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", chatOnceLineSep), chatOnceLineSep) {
		if s := chatOnceSeqLine(raw); s != "" {
			lines = append(lines, s)
		}
	}
	return lines
}

// chatOnceSeqAppend extends a scripted sequence by the lines a stdin read carried
// (argv first, then what was piped in — the order the caller wrote them in).
func chatOnceSeqAppend(seq []string, stdinText string) []string {
	out := append([]string{}, seq...)
	return append(out, chatOnceSeqFromStdin(stdinText)...)
}

// runChatOnceSequence drives the ordinary REPL over a scripted transcript and
// returns this face's exit status (see the file header for the contract).
//
// The whole transcript is handed to the REPL's reader, so the lines are read in
// order by the REPL's own loop and every line reaches the arm it names. The run
// ends the way an interactive session ends: a `/quit` line, or end of input after
// the last line.
func runChatOnceSequence(rest []string, lines []string) int {
	sequence := strings.Join(lines, chatOnceLineSep) + chatOnceLineSep

	// The input is built through the SAME cooked constructor the one-prompt face
	// and a piped `gopi chat` use, so the bytes cannot differ: the transcript is
	// what a caller would have typed, and the REPL reads it with its own reader.
	answer := &chatOnceAnswer{}
	input, closeInput := newChatInputCooked(strings.NewReader(sequence), io.MultiWriter(os.Stdout, answer))
	defer closeInput()

	prevActivity := chatOnceToolActivityOut
	chatOnceToolActivityOut = os.Stderr
	defer func() { chatOnceToolActivityOut = prevActivity }()

	// Arm the failure seam for the duration of this run: the REPL's own arms
	// report a refusal or a failed turn here, so a scripted line that could not be
	// honoured is not lost behind a 0 (Rule 15). Restored on the way out, so the
	// interactive face keeps its historic bytes.
	var failErr error
	restoreFailures, _ := chatOnceObserveFailures(&failErr)
	defer restoreFailures()

	code := runChatWithInput(rest, input)

	// A CLEAN QUIT IS SUCCESS. `/quit` is the caller's own end-of-session, not a
	// failure: the REPL leaves through its leaveChat path, whose status describes
	// the SESSION TEARDOWN (an interrupt is a non-zero there), while the caller
	// asked for exactly this end. A scripted session that ran every line and quit
	// on purpose is a 0 — that is the honest exit contract of this face.
	if code != chatOnceExitOK && !chatOnceSeqQuitTheSession(lines, failErr) {
		return code
	}
	if failErr != nil {
		// The REPL's own notice already said why. This is the STATUS the caller
		// acts on, and it names the class so a driver can tell a refused command
		// from a failed turn.
		fmt.Fprintf(os.Stderr, "%sa scripted line was not honoured: %v\n", chatOncePrefix, failErr)
		return chatOnceExitTurnFailed
	}
	return chatOnceExitOK
}

// chatOnceSeqQuitTheSession reports whether the caller ended this session with
// their OWN command word (/quit or /exit) and nothing failed — the one case where
// the REPL's teardown status is not the run's verdict.
//
// It is deliberately strict: ANY reported failure means the status stands, so a
// script that quit after a refused command is still a failure.
func chatOnceSeqQuitTheSession(lines []string, failErr error) bool {
	if failErr != nil {
		return false
	}
	for _, line := range lines {
		if word := chatOnceSeqWord(line); word == quitCommandName || word == exitCommandName {
			return true
		}
	}
	return false
}

// chatOnceSeqWord is a scripted line's command word, WITHOUT its slash — the
// spelling the REPL's own switch compares against (Rule 1b: the words come from
// the shared constants, never typed twice).
func chatOnceSeqWord(line string) string {
	fields := strings.Fields(chatOnceSeqLine(line))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(fields[0], chatSlashTrigger))
}

// chatOnceSeqRest is the REPL's own argv for a scripted run: the argv tail with
// the command words removed. They are not REPL words — their place is the
// transcript — so leaving them in argv would make the REPL reject a scripted run
// with "unknown flag" for a line it is about to read anyway.
func chatOnceSeqRest(argv []string) []string {
	var rest []string
	for _, a := range argv {
		if chatOnceSeqIsScriptedLine(a) {
			continue
		}
		rest = append(rest, a)
	}
	return rest
}

// chatOnceSeqFirstLine is the first scripted line of this invocation, resolved
// from the two sources in the caller's own order (argv commands, then piped
// lines). Its KIND decides whether this run is a scripted session or one prompt.
func chatOnceSeqFirstLine(argvLines []string, stdinText string) string {
	if len(argvLines) > 0 {
		return argvLines[0]
	}
	if lines := chatOnceSeqFromStdin(stdinText); len(lines) > 0 {
		return lines[0]
	}
	return ""
}

// chatOnceSeqPick resolves the text of a ONE-PROMPT run from its two sources:
// -p/--prompt/positional wins when given, otherwise the piped stdin — the
// precedence this face has always documented. The loser is not merged in: a
// caller that passed both meant the explicit prompt.
func chatOnceSeqPick(prompt, stdinText string) string {
	if strings.TrimSpace(prompt) != "" {
		return prompt
	}
	return stdinText
}
