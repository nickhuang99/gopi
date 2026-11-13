package main

// chat_keyboard_protocol.go — what the chat surface SAYS about the keyboard
// negotiation internal/lineedit performed when it entered raw mode.
//
// THE TWO THINGS WORTH SAYING, and they are different in kind:
//
//   - A terminal that answered nothing does not send Shift+Enter as anything
//     distinguishable from Enter — the information is destroyed in the terminal,
//     before any byte reaches this process (see internal/lineedit/keyprotocol.go).
//     The operator typing Shift+Enter and getting a SUBMITTED message would have
//     no way to learn why, so this says it once, and names the key that works on
//     every terminal: Alt+Enter.
//   - Under --verbose the SUPPORTED path is stated as well, because a
//     negotiation that silently succeeds is exactly as unobservable as one that
//     silently fails, and "did the probe run at all?" is the first question when
//     the feature misbehaves (Rule 15).
//
// The notice is emitted ONCE per run, from the REPL's own setup — not from the
// input constructor, which several tests call and which must stay silent. A
// notice printed per read, per turn or per redraw would be a console that
// fights the layout it is drawn into.

import (
	"fmt"
	"os"

	"github.com/nick/gopi/internal/lineedit"
)

// The words this run says. Named (Rule 1b) because they are a vocabulary a test
// asserts and an operator reads, and because the advice must name the SAME key
// the decoder implements (`ESC CR` → KeyNewline).
const (
	// chatKeyboardUnsupportedNotice is the ONE line printed on a terminal that
	// did not answer the keyboard-protocol query. It names the key that works
	// everywhere rather than the protocol it is missing: the operator has no use
	// for the protocol's name.
	chatKeyboardUnsupportedNotice = "[gopi] chat: this terminal does not report Shift+Enter (no reply to the keyboard-protocol query) — press Alt+Enter for a newline inside a message; Enter alone sends it.\n"
	// chatKeyboardSupportedVerbose is the SUPPORTED path's line, printed only
	// under --verbose: the probe ran, the terminal answered, the enhancement was
	// pushed.
	chatKeyboardSupportedVerbose = "[gopi] chat: keyboard protocol: %s (flags answered: %d, pushed disambiguate-escape-codes)\n"
	// chatKeyboardUnsupportedVerbose is the same fact for the degradation, also
	// under --verbose. It is distinct from the notice above: one is ADVICE for
	// the operator, this one is the measured result for a diagnostic reader.
	chatKeyboardUnsupportedVerbose = "[gopi] chat: keyboard protocol: %s (no flags answered — Shift+Enter cannot be distinguished from Enter on this terminal)\n"
)

// chatAnnounceKeyboardProtocol reports the negotiation the input layer performed,
// once, at the moment the REPL owns the screen — and RETURNS that verdict, so the
// caller can state the same measurement where it belongs: the startup screen's
// side panel, or the held-notice section (chat_panel_notice.go).
//
// IT PRINTS NO ADVICE ITSELF (the operator's report, 2026-10-03: the sentence used
// to be committed into the ANSWER BAND, where it wrapped to two rows of the area
// the operator reads as the conversation). The caller holds the sentence as a
// notice instead, and the notices machinery prints it on exactly the paths where
// no screen could state it — which is what keeps a cooked/redirected run's bytes
// what they were.
//
// The VERBOSE line stays on stderr in every case: it is startup plumbing, the
// stream every other --verbose path prints to, and a diagnostic reader greps for
// it there.
func chatAnnounceKeyboardProtocol(in *chatInput, verbose bool) lineedit.KeyboardProtocol {
	if in == nil {
		return lineedit.KeyboardProtocolUnavailable
	}
	state, flags, _ := in.keyboardProtocol()
	switch state {
	case lineedit.KeyboardProtocolSupported:
		if verbose {
			fmt.Fprintf(os.Stderr, chatKeyboardSupportedVerbose, state, flags)
		}
	case lineedit.KeyboardProtocolUnsupported:
		if verbose {
			fmt.Fprintf(os.Stderr, chatKeyboardUnsupportedVerbose, state)
		}
	default:
		// Not probed (no raw-mode terminal): nothing to say, and saying "no
		// reply" here would blame the terminal for a question never asked.
	}
	return state
}
