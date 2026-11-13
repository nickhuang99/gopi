package tools

// chat_policy_refusal_names_clause_test.go — the MEASURED defect of the bash arm's
// refusal text, pinned as a test (task 01a1ae0b, 2026-11-06).
//
// WHAT WAS MEASURED. A console run whose ```chat-policy fence carried
// `allow ls`, `allow cat` and `allow printf` — but no `allow echo` — answered
// `echo hello` with
//
//	Refused by the CHAT WRITE BOUNDARY (`echo hello`): the startup posture is
//	`forbidden` and no rule allows it, so it is wrong to run from this console at
//	all — there is nothing to ask.
//
// Every word of that is true and the SHAPE is wrong for the reader: the command
// writes nothing, the fence's own gap is invisible (which line is missing, and
// that a missing line is a cause at all), and a compound line that was refused
// for its strictest clause read as though its first clause had been refused.
// The read tools were never the problem, so the paragraph that promised
// "read-only bash queries at any moment, without asking anybody" is a second,
// separate over-promise in the prompt (asserted in internal/agent).
//
// THE FOUR PROPERTIES BELOW are the ones a reader needs at the moment of refusal:
//
//	1 the arm is a COMMAND boundary, not a write boundary;
//	2 the cause is named, and the two causes are DISTINGUISHABLE
//	  (a `forbidden` rule vs NO RULE AT ALL);
//	3 for a single-clause line the rule that is MISSING is named — the fence is
//	  the operator's file, and their missing line is what refused the command;
//	4 for a compound line the DECIDING CLAUSE is named, and the line says the
//	  whole line takes the strictest clause — which is why a pure read can be
//	  refused inside it while standing alone it is allowed (both halves asserted,
//	  or the test cannot tell a correct refusal from a blanket one).

import (
	"strings"
	"testing"
)

// THE ASSERTED FACTS ARE LITERALS, NOT THE RENDERER'S OWN CONSTANTS, and that is
// deliberate: a test that keyed on `chatBashRefusalMarker` could not be run
// against the code BEFORE this change at all (the constant did not exist), so the
// red half of the red→green pair would be a compile error rather than a
// measurement. These are the strings a READER must see in the tool result; the
// code names them once, in chat_policy_gate.go (Rule 1/13/14 governs production,
// not the assertion that a rendered sentence actually says the thing).
const (
	refusalCommandBoundary    = "Refused by the CHAT COMMAND BOUNDARY"
	refusalWriteBoundary      = "Refused by the CHAT WRITE BOUNDARY"
	refusalNoRuleCause        = "no rule in the project's ```chat-policy fence mentions"
	refusalFenceWord          = "```chat-policy fence"
	refusalCompoundClauseLead = "This line is a CHAIN of clauses"
)

// TestChatBashRefusalNamesTheClauseAndTheCause is the red→green pair for that
// text. The table it freezes is the operator's measured one, minus `echo`.
func TestChatBashRefusalNamesTheClauseAndTheCause(t *testing.T) {
	// The MEASURED fence: three allowed read commands, a posture that refuses
	// everything the table does not mention.
	freezeChatRuleTable(t, chatPolicyFence(
		"# ---- read-only queries ----",
		"allow ls",
		"allow cat",
		"allow printf",
	), postureNamed(t, "forbidden"))

	t.Run("an absent rule is the named cause", func(t *testing.T) {
		got := ChatBashWriteRefusal("echo hello")
		if got == "" {
			t.Fatal("`echo hello` was NOT refused under an empty-of-echo fence and a forbidden posture — " +
				"the premise of this whole task just changed; nothing below is measurable")
		}
		for _, want := range []string{
			refusalCommandBoundary, // 1 — a COMMAND boundary
			"echo hello",           // which command
			refusalNoRuleCause,     // 2 — the cause, named
			refusalFenceWord,       // …and the file whose line is missing
			"forbidden",            // the posture that answered
			"`allow echo`",         // 3 — the fence line that would admit it
		} {
			if !strings.Contains(got, want) {
				t.Errorf("the refusal does not carry %q.\nA reader cannot tell a fence gap from a boundary:\n%s", want, got)
			}
		}
		if strings.Contains(got, refusalWriteBoundary) {
			t.Errorf("a bash refusal still reports itself as the WRITE boundary — the command writes nothing:\n%s", got)
		}
		if strings.Contains(got, "it matches the rule") {
			t.Errorf("no rule matched `echo hello`, yet the text claims one did:\n%s", got)
		}
	})

	t.Run("the same line is admitted once the fence names it", func(t *testing.T) {
		// The negation: without this the test would pass on a boundary that
		// refused everything and described itself beautifully.
		if got := ChatBashWriteRefusal("ls"); got != "" {
			t.Fatalf("`ls` IS in the fence and must run, got:\n%s", got)
		}
	})

	t.Run("a compound line names the clause that decided", func(t *testing.T) {
		got := ChatBashWriteRefusal("cd /tmp && ls -l")
		if got == "" {
			t.Fatal("a compound line holding a clause the fence never mentions was allowed")
		}
		for _, want := range []string{
			refusalCommandBoundary,
			refusalCompoundClauseLead, // 4 — the clause-by-clause sentence
			"`cd /tmp`",               // the clause that decided (NOT `ls -l`)
			refusalNoRuleCause,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("the refusal does not carry %q:\n%s", want, got)
			}
		}
		// The decisive half: `ls -l` alone is allowed by this same fence, so a
		// reader who is told "refused" for the compound must be told WHICH clause
		// refused it, or the natural conclusion is "ls is not allowed either".
		if got := ChatBashWriteRefusal("ls -l"); got != "" {
			t.Fatalf("the single-clause `ls -l` must be allowed by this fence, got:\n%s", got)
		}
		if !strings.Contains(got, "`ls -l`") {
			t.Errorf("the compound refusal never mentions the allowed clause `ls -l` — the reader cannot see "+
				"that only `cd /tmp` was the problem:\n%s", got)
		}
	})

	t.Run("a forbidden rule is a different cause and says so", func(t *testing.T) {
		freezeChatRuleTable(t, chatPolicyFence("forbidden git push"), postureOnRequest())
		got := ChatBashWriteRefusal("git push origin master")
		for _, want := range []string{
			refusalCommandBoundary,
			"matches the rule", // cause 1: the operator's own `forbidden` line
			"`forbidden`",
			"test chat-policy:2", // and its provenance, so the line can be found
		} {
			if !strings.Contains(got, want) {
				t.Errorf("the forbidden-rule refusal does not carry %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, refusalNoRuleCause) {
			t.Errorf("a FORBIDDEN rule was reported as an ABSENT rule — the two causes are not distinguishable:\n%s", got)
		}
	})
}
