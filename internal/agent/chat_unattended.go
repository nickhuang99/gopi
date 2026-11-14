package agent

// chat_unattended.go — the UNATTENDED RUN's context block: the one sentence a
// self-driving run cannot guess for itself.
//
// THE MEASURED GAP (operator's own A/B, 2026-10-04). One model, one toolset, one
// machine; the ONLY difference between two runs was a sentence of context — "no
// one is going to answer you; pick the conventions yourself". In the run WITHOUT
// it the model asked a human a question and stopped after one turn; in the run
// WITH it the model chose the conventions itself and drove the work to the end.
// The mechanism to keep going already existed (`--once --turns -1
// --max-seconds`, chat_surface action=type, the end_turn confirmation round);
// what was missing was that the MODEL knew it was allowed to use it. That is
// what this block states, and it is a prompt change only — no new tool, no new
// mechanism, no relaxed bound (Rule 3/35).
//
// WHY IT IS A BLOCK AND NOT A DEFAULT IN THE PERSONA. A question addressed to
// the operator is the CORRECT behaviour of an interactive console: the operator
// is right there and answering is cheap. The same sentence on an interactive
// surface would be a lie (a human IS reading it) and would degrade the console
// the operator uses every day. So the block is carried ONLY by a run that
// declares itself unattended — `Agent.Unattended`, set by the one-shot face that
// has no human on the other end (`gopi chat --once`, cmd/agent/chat_once.go) —
// and every other path renders the prompt it rendered before. The switch exists
// in both directions: a `--once` run that DOES have a human in the loop (a
// single question-and-answer step, a scripted run a person is watching) turns
// the context off with the face's own flag.
//
// WHY <= 3 LINES, MEASURED. The block rides the system prompt of EVERY turn of
// an unattended run, so its cost is paid per turn (and re-paid at every prefill,
// since an append-only history is resent each round — memory #7815's O(n^2)
// note). Three sentences is the shortest form that carries the three distinct
// facts the A/B showed were needed: (1) nobody will answer, so asking ends the
// run; (2) you are AUTHORISED to choose; (3) ending by asking is the FAILURE
// SHAPE, so the answer is the artifact. A fourth sentence would be commentary,
// and commentary in a per-turn prompt is the expensive kind of prose.
//
// RULE 1/13/14: the block's own text is composed from NAMED constants — the
// marker (the one substring a test greps for presence), the three sentences, and
// the failure-shape vocabulary — so "is this block present?" and "what does it
// say about asking?" are answered against one definition each. Nothing here is
// spelled twice.

import (
	"strings"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// chatUnattendedMarker opens the block, and it is the ONE substring a caller
// outside this package asserts presence against — separated from the body so
// "was this run told it is unattended?" is a one-substring question that does
// not move when the sentences below are reworded. Exported through
// ChatUnattendedPromptHead for the same reason ChatSurfacePromptHead is: a
// check outside this package must key on the prompt's own words, never on a
// re-typed copy of them (Rule 13/14).
const chatUnattendedMarker = "[UNATTENDED RUN — nobody will answer you]"

// ChatUnattendedPromptHead returns the block's opening marker: the string a
// caller OUTSIDE this package (cmd/agent's own acceptance, a report's evidence
// step) greps for when the question is "did this request carry the unattended
// context?". Same contract as ChatSurfacePromptHead: presence is asserted
// against the marker the prompt itself draws.
func ChatUnattendedPromptHead() string { return chatUnattendedMarker }

// The three sentences, named (Rule 1). Sentence 1 says WHY a question is fatal
// rather than merely unhelpful; sentence 2 is the AUTHORISATION the A/B showed
// was the whole difference; sentence 3 NAMES the failure shape, so "I stopped to
// ask" is a shape the model can recognise in its own draft rather than a habit
// nobody described to it.
const (
	// chatUnattendedFact states the fact a self-driving run cannot discover:
	// there is no reader, and a question is therefore not a question.
	chatUnattendedFact = "No human is reading this run and no reply will ever come, so a question you ask is not a question — it is where the run ends."

	// chatUnattendedAuthorisation is the permission the measured A/B isolated:
	// the model may choose a convention the task left open, provided it says
	// which one it chose. "Say so in one clause" is deliberate: the choice must
	// be reviewable, and a report that hides its own convention is not.
	chatUnattendedAuthorisation = "You are AUTHORISED to choose the conventions yourself: where a parameter, a period, a sample or a source is left undecided, pick a defensible value, state the choice in one clause, and continue — never wait for a human's confirmation."

	// chatUnattendedFailureShape names the ending this context exists to
	// prevent. It is stated as the observable shape ("ends by asking") rather
	// than as a prohibition, because the shape is what a later reviewer can find
	// in the transcript — and the last clause says what to end with instead.
	chatUnattendedFailureShape = "The FAILURE shape of this run is ending by ASKING instead of delivering (\"which do you want?\", \"shall I proceed?\", \"waiting for confirmation\") — deliver the work, then end your answer with the one next step you would have taken."
)

// chatUnattendedContextRules renders the block. It is a function rather than a
// const because the ASSEMBLY decides whether it is carried at all — the block
// itself has no runtime values, and keeping it as a rendering site (never a
// registry Text) is what lets the two chat assemblies append it conditionally
// without a second prompt-shape.
//
// The sentences are joined by the package's own separator (promptSectionSeparator
// is the blank line BETWEEN blocks; inside one block the sentences are one
// paragraph each, joined by a single space) so the block is exactly three
// sentences on ONE rendered line — which is what makes the "<= 3 lines" ceiling
// measurable with a line counter rather than an opinion.
func chatUnattendedContextRules() string {
	return strings.Join([]string{
		chatUnattendedMarker + " " + chatUnattendedFact,
		chatUnattendedAuthorisation,
		chatUnattendedFailureShape,
	}, " ")
}

// chatUnattendedPromptLineCount is the ceiling the block must not exceed, in
// RENDERED LINES of the whole chat system prompt — the unit the operator's
// constraint is stated in. It is a constant so the acceptance check reads the
// same number the block's own documentation claims (Rule 1/13: a bound nothing
// references is not a bound).
const chatUnattendedPromptLineCount = 3

// chatUnattendedBlockOpen is the small entry point both chat assemblies call:
// it returns the block for an agent that declared itself unattended, and "" for
// every other agent (nil included). Keeping the CONDITION here — rather than
// repeating `a != nil && a.Unattended` at two call sites — is what stops the two
// assemblies from ever disagreeing about which runs carry the context, which
// would make the same run behave differently depending on whether tools were on
// the wire.
func chatUnattendedBlockOpen(a *Agent) string {
	if a == nil || !a.Unattended {
		return ""
	}
	return chatUnattendedContextRules()
}

// ChatUnattendedArmedBy names the VERB that arms this context (the one-shot
// carrier `gopi chat --once`) for any reader of the prompt: composed from the
// leaf constant package, so the sentence and the parser cannot drift (Rule
// 1/13 — internal/agent cannot import cmd/agent, and the face's own FLAG
// spelling lives in cmd/agent's vocabulary, where its parser reads it).
func ChatUnattendedArmedBy() string { return cliverbs.Chat }
