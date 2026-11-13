package main

// The one-shot face's VALUED REPL FLAGS carry their value in the next argv slot —
// the minimal repro of the defect this file pins.
//
// THE DEFECT (measured on the shipped binary, task 01a1adab). The help page
// advertises the SPACED spelling —
//
//	--resume SESSION   continue a session the seed argument below could name …
//	                   The joined form --resume=<SESSION> is accepted too.
//
// — and `gopi chat --once` shares that page and that command line. But the
// one-shot parser handed only the FLAG word to the REPL and read the word in its
// slot as the POSITIONAL PROMPT, so the documented command line was refused:
//
//	$ gopi chat --once --resume 01a1adab-284f-73bf-bf71-f6bc4d03d437 -p hi
//	gopi chat: more than one prompt given
//	  ("01a1adab-284f-73bf-bf71-f6bc4d03d437" and "hi") — one turn takes ONE prompt
//
// while `--resume=<id>` worked. A reader who follows the page writes a command
// line that cannot run — the state this run's epic forbids.
//
// WHY THE TEST SITS AT THE PARSER: chatOnceParseArgs is the seam that dropped the
// word, and it is pure — the REPL's own argv walk is pinned elsewhere
// (chat_argv_test.go). The whole-package binary is exercised for the same claim by
// the epic's live run (Rule 31), which this arm cannot replace and does not try to.

import (
	"strings"
	"testing"
)

// TestChatOnceValuedREPLFlagCarriesItsValueInTheNextSlot is the regression guard:
// every valued flag of the REPL face keeps its slot when the one-shot face hands
// the argv on — in BOTH spellings the page documents — and the slot's word is
// never read as the prompt.
//
// It reads chatResumeValuedFlags (the parser's own list, Rule 13), so a flag that
// gains a value slot joins this contract by construction rather than by being
// named here a second time.
func TestChatOnceValuedREPLFlagCarriesItsValueInTheNextSlot(t *testing.T) {
	for _, flag := range chatResumeValuedFlags() {
		value := chatValuedFlagProbeValue(flag)
		t.Run(flag, func(t *testing.T) {
			// The SPACED spelling — the one the page advertises and the one that
			// was broken.
			spaced, err := chatOnceParseArgs([]string{flag, value, chatOncePromptFlag, "hi"})
			if err != nil {
				t.Fatalf("chatOnceParseArgs([%s %s %s hi]) = %v, want no error — the spaced spelling is on the help page",
					flag, value, chatOncePromptFlag, err)
			}
			if spaced.prompt != "hi" {
				t.Errorf("chatOnceParseArgs([%s %s %s hi]).prompt = %q, want %q — the flag's VALUE was read as the prompt",
					flag, value, chatOncePromptFlag, spaced.prompt, "hi")
			}
			want := []string{flag, value}
			if strings.Join(spaced.rest, " ") != strings.Join(want, " ") {
				t.Errorf("chatOnceParseArgs([%s %s %s hi]).rest = %v, want %v — the REPL never sees the flag's value, so it cannot consume it",
					flag, value, chatOncePromptFlag, spaced.rest, want)
			}

			// The JOINED spelling keeps working: the fix must not be bought by
			// breaking the form that already worked.
			joined, err := chatOnceParseArgs([]string{flag + chatFlagValueSep + value, chatOncePromptFlag, "hi"})
			if err != nil {
				t.Fatalf("chatOnceParseArgs([%s=%s %s hi]) = %v, want no error", flag, value, chatOncePromptFlag, err)
			}
			if joined.prompt != "hi" {
				t.Errorf("chatOnceParseArgs([%s=%s %s hi]).prompt = %q, want %q", flag, value, chatOncePromptFlag, joined.prompt, "hi")
			}
			if strings.Join(joined.rest, " ") != flag+chatFlagValueSep+value {
				t.Errorf("chatOnceParseArgs([%s=%s %s hi]).rest = %v, want [%s=%s]",
					flag, value, chatOncePromptFlag, joined.rest, flag, value)
			}

			// A SLOT HOLDING A FLAG IS NOT A VALUE. The flag's own parsers treat a
			// `-`-word as "no value given", so the walk must leave that word for
			// the next iteration: `--once <flag> -h` still reaches the page rather
			// than resuming a session named "-h".
			bare, err := chatOnceParseArgs([]string{flag, HelpFlag, chatOncePromptFlag, "hi"})
			if err != nil {
				t.Fatalf("chatOnceParseArgs([%s %s %s hi]) = %v, want no error", flag, HelpFlag, chatOncePromptFlag, err)
			}
			wantBare := []string{flag, HelpFlag}
			if strings.Join(bare.rest, " ") != strings.Join(wantBare, " ") {
				t.Errorf("chatOnceParseArgs([%s %s %s hi]).rest = %v, want %v — a flag in the value slot was consumed as a value",
					flag, HelpFlag, chatOncePromptFlag, bare.rest, wantBare)
			}
		})
	}
}

// TestChatOncePromptIsStillTheBareWord: the arm above must not have turned every
// word into a flag's value. A bare word with no valued flag before it is still the
// prompt, and two prompts are still refused by name (Rule 15) — the contract
// TestChatOnceParseArgs_RefusesTwoPrompts pins, re-asserted beside the new arm
// because that arm now consumes words the old parser did not.
func TestChatOncePromptIsStillTheBareWord(t *testing.T) {
	got, err := chatOnceParseArgs([]string{chatOnceFlag, "hi"})
	if err != nil {
		t.Fatalf("chatOnceParseArgs([--once hi]) = %v, want no error", err)
	}
	if got.prompt != "hi" {
		t.Errorf("chatOnceParseArgs([--once hi]).prompt = %q, want %q", got.prompt, "hi")
	}
	if _, err := chatOnceParseArgs([]string{chatOnceFlag, "a", "b"}); err == nil {
		t.Error("chatOnceParseArgs([--once a b]) = nil error, want the two-prompts refusal")
	}
}
