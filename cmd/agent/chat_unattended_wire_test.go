package main

// chat_unattended_wire_test.go — the UNATTENDED context at the FACE boundary:
// which argv carries it, and what actually reaches the wire.
//
// WHY THE WIRE AND NOT THE FLAG. The epic's whole claim is that a sentence of
// context changes what the model does, so the only honest place to check the
// plumbing is the REQUEST BODY a real turn sends: a flag parsed into a field
// nobody assembles into the prompt would pass every argv-level test in this tree
// and change nothing (Rule 31's spirit — the surface must be exercised, not
// inspected).
//
// The three cases are the acceptance's own three:
//   - the one-shot face (`provided != nil`, i.e. every `gopi chat --once` run)
//     CARRIES the context — that is the default this epic introduces;
//   - `--answer-alone` on the same face does NOT (a human is reading the one
//     answer: a single question-and-answer step);
//   - an interactive run does NOT, because a human IS watching it.
//
// The harness is the production path minus the terminal: the same
// runChatWithInput the REPL, the one-shot face and the scripted-sequence face
// all call, against a mock model that records every request body.

import (
	"bytes"
	"strings"
	"testing"
)

// unattendedBodyNeedle is the ASCII head of the context's marker
// (agent.ChatUnattendedPromptHead begins "[UNATTENDED RUN — …"). The em dash in
// the full marker is a NON-ASCII rune, and a request body is JSON — the encoder
// escapes it as \u2014 — so the byte-level check keys on the marker's ASCII head
// rather than the whole string, and the assertion below states that in one
// place. The needle still moves with the marker's own wording, which is what
// keeps this check from passing against a prompt that says something else.
const unattendedBodyNeedle = "[UNATTENDED RUN"

// unattendedAbsentNeedle is the sentence that must NEVER reach an interactive
// run's wire (nor an --answer-alone one): the fact is false there, and a prompt
// that states a false fact is worse than one that omits it.
const unattendedAbsentNeedle = "No human is reading this run"

// TestChatUnattended_WireCarriesItOnlyOnTheOneShotFace drives the ONE-SHOT face
// (both directions) through the REPL and reads what each turn sent.
//
// WHY THERE IS NO INTERACTIVE CASE HERE. `runChatWithInput`'s discriminator is
// the input it is handed: a nil input means the interactive face, which reads
// os.Stdin — a test binary has no terminal, so no request would be made and the
// case would assert nothing. The interactive direction is therefore pinned twice
// where it CAN be: at the assembly (`assembleChatSystemPrompt`/`chatOpsSystemPrompt`
// with an ordinary Agent, internal/agent/chat_unattended_test.go) and against a
// REAL interactive run's raw log (the acceptance's own evidence, quoted in the
// report). What this test owns is the one-shot face's default and its decline.
func TestChatUnattended_WireCarriesItOnlyOnTheOneShotFace(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want bool
	}{
		{
			// The argv the one-shot face hands the REPL after it has stripped
			// its own words: this is `gopi chat --once --tools "…"` verbatim,
			// minus `--once` itself (which drives the walk before the REPL, see
			// runChatOnce) and with the face's own provided input.
			name: "one-shot face, default (the epic's new default)",
			argv: []string{"--no-editor", "--tools"},
			want: true,
		},
		{
			name: "one-shot face, --answer-alone declines it",
			argv: []string{"--no-editor", "--tools", chatFlagAnswerAlone},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := runChatBoundaryTurnArgv(t, "hello", c.argv)
			reqs := h.requests()
			if len(reqs) == 0 {
				t.Fatalf("no request reached the mock model — the turn did not run (exit %d)", h.code)
			}
			// EVERY request of the turn is checked, not the first: the system
			// prompt rides every round of a ReAct turn, so a context injected
			// once and dropped on the next round would still be a defect.
			for i, body := range reqs {
				has := bytes.Contains(body, []byte(unattendedBodyNeedle))
				if has != c.want {
					t.Fatalf("request %d of %d: unattended context present=%t, want %t\nbody head: %s",
						i+1, len(reqs), has, c.want, truncateForTest(string(body), 200))
				}
				// The false-fact direction: the sentence may not appear on a run
				// that did not declare itself unattended, whatever the marker
				// does.
				if !c.want && bytes.Contains(body, []byte(unattendedAbsentNeedle)) {
					t.Fatalf("request %d states a false fact to a run whose answer a human is waiting for: %s",
						i+1, truncateForTest(string(body), 200))
				}
			}
		})
	}
}

// TestChatUnattended_AnswerAloneIsAcceptedAndUnattendedIsTheDefault pins the
// FACE's contract in both directions at the argv level: the decline is a word
// the closed vocabulary accepts (a documented command line the parser refuses
// would be a trap), and the default is the unattended one — so the epic's
// "default carries it" claim is a property of the face, not of a caller who
// remembered.
func TestChatUnattended_AnswerAloneIsAcceptedAndUnattendedIsTheDefault(t *testing.T) {
	if !chatOnceIsREPLFlag(chatFlagAnswerAlone) {
		t.Fatalf("%s is not in the REPL's closed vocabulary — `gopi chat --once %s \"…\"` would be refused as an unknown flag",
			chatFlagAnswerAlone, chatFlagAnswerAlone)
	}
	// The one-shot face PARSES it (the walk consumes a bare switch without
	// consuming the prompt that follows).
	opts, err := chatOnceParseArgs([]string{"--tools", chatOnceFlag, chatFlagAnswerAlone, "hello"})
	if err != nil {
		t.Fatalf("the one-shot face refused %s: %v", chatFlagAnswerAlone, err)
	}
	if opts.prompt != "hello" {
		t.Fatalf("prompt = %q, want \"hello\" — the switch must not swallow the prompt", opts.prompt)
	}
	// And the DEFAULT — the one-shot face with no decline — is the unattended
	// reading of the same command line. This is the epic's own default-bias
	// claim, stated where it can fail.
	plain, err := chatOnceParseArgs([]string{"--tools", chatOnceFlag, "hello"})
	if err != nil {
		t.Fatalf("the plain one-shot argv was refused: %v", err)
	}
	if plain.prompt != "hello" {
		t.Fatalf("plain prompt = %q, want \"hello\"", plain.prompt)
	}
}

// TestChatUnattended_HelpNamesBothDirections asserts the operator-facing page
// documents the default AND its way out: a default that changes model behaviour
// and a decline nobody can find are the same defect as a silent failure.
func TestChatUnattended_HelpNamesBothDirections(t *testing.T) {
	if !strings.Contains(chatOnceUsage, chatFlagAnswerAlone) {
		t.Fatalf("the --once section does not name %s — the decline is undiscoverable", chatFlagAnswerAlone)
	}
	if !strings.Contains(chatOnceUsage, "UNATTENDED BY DEFAULT") {
		t.Fatalf("the --once section does not state the default it now has")
	}
}
