package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// agentguard_shortprompt.go — the SECOND screen on the bare-prompt face: a
// prompt too SHORT to be a prompt.
//
// THE DEFECT THIS CLOSES (operator's ruling, 2026-11-17). The neighbouring
// guard (§1c, GuardBarePrompt in agentguard.go) answers only ONE of the two
// ways `gopi <word>` wastes a real run: the RECURSION one, and only when an
// agent signature is in the environment. On the operator's OWN terminal it is
// deliberately a no-op — the documented `gopi "<prompt>"` usage lives there —
// so `gopi submit` typed by hand (a leaf verb of `gopi task submit`, written
// without its family word, or two words in the wrong order) is indistinguishable
// from a prompt and buys a full paid run: a session directory, an `[auto]
// submit` ledger row and a clone ground. Measured in this ledger: 917 `[auto]`
// rows, of which 5 are `[auto] submit` plus one each of show/list/status.
//
// WHY LENGTH IS THE RIGHT SECOND AXIS, AND WHY IT IS NOT THE FIRST ONE.
// `exec("/bin/gopi", ["gopi","submit"])`, `exec("/bin/gopi", ["gopi","submit"])`
// and `gopi "submit"` from a shell hand the process BYTE-IDENTICAL argv — the
// quotes are the shell's syntax and do not survive the exec boundary — so
// "did they mean a command or a one-word prompt" is not decidable from the
// command line. Two signals are decidable, and they are complements:
//
//	① WORD OWNERSHIP — is the word this binary's own surface word?
//	   That is config.PositionalVerbs / config.commandHintWords' question, and
//	   it answers `gopi submit` but cannot answer `gopi task submitt`.
//	② LENGTH — could this text possibly BE an instruction?
//	   A one-token input never carries a complete meaning; it answers nothing,
//	   asks nothing, and is far more likely a mistyped command than a request.
//	   That is THIS file's question, and it answers the typo case ① misses.
//
// THE FLOOR IS 2 TOKENS, and the number is MEASURED, not chosen. Counted
// through this repository's own tokenizer — the same DeepSeek-V4.1 BPE the
// model is charged by (`internal/tokenizer`, CountDefault) — on the built
// binary, 2026-11-17:
//
//	$ ./bin/gopi tool tokens --file <one line, no trailing newline>
//	  你好                              1 token
//	  好的                              1 token
//	  version                           1 token
//	  submit                            1 token
//	  help                              1 token
//	  about                             1 token
//	  你好吗                            2 tokens   ← the operator's own hello-world
//	  hello world                       2 tokens
//	  submit-x                          2 tokens
//	  hello there                       2 tokens
//	  who are you?                      4 tokens
//	  how are you?                      4 tokens
//	  where is the capital of China?    7 tokens
//	  analyze this                      2 tokens
//	  run the tests                     3 tokens
//
// Every complete question — the operator's `你好吗` hello-world included, and
// the English `hi`/`ok` shapes he rejected as unusable — costs AT LEAST two
// tokens; every one-token line is a bare WORD. The floor therefore admits
// every real prompt measured and refuses exactly the class the defect is made
// of. It is a floor, not a quality bar: `gopi 你好吗` still starts a run, and
// that is correct — it is a sentence.
//
// WHAT THIS GUARD DOES NOT DO.
//   - It does not touch the agent-recursion case: that shape is refused by §1c
//     BEFORE this floor is consulted, with a message that names the queue and
//     the chat door. One surface must not produce two verdicts for one argv.
//   - It does not touch an EXPLICIT prompt channel (`--prompt X`, the settings
//     "prompt" key) or a PIPED stdin body: those are a deliberate statement
//     that the text IS input, which is exactly the answer the refusal hands
//     the operator. Only a positional that would BECOME the prompt is
//     screened — the same predicate §1c uses (BarePromptWord), so the two
//     screens cannot disagree about which invocations are on this face.
//   - It does not refuse an IMAGE-only run: an attached image is a complete
//     instruction with no text at all.

// BarePromptMinTokens is the length floor, in tokens of the model's own
// vocabulary. See the file comment for the measured ladder it comes from.
//
// It is a named constant because the decision, the refusal message and the
// acceptance test all spell the same threshold, and a refuse-at-3 / claim-2
// drift would make the message lie about the rule it just applied
// (Rules 1/13/14).
const BarePromptMinTokens = 2

// ShortBarePromptFace is the surface name the refusal quotes.
const ShortBarePromptFace = "the bare-prompt length floor"

// BarePromptCounter counts model tokens in a prompt text. It is INJECTED
// rather than imported so this leaf policy package keeps its dependency set
// (`internal/tokenizer` embeds a 6.3 MB vocabulary image; a package every
// command imports is the last place to drag it into) — and so the decision is
// testable without the real encoder. `internal/config` already receives
// cmd/agent's measurements the same way (AgentSpawnSignatureNow is produced by
// the caller's process facts, not guessed here).
type BarePromptCounter func(text string) (int, error)

// ShortBarePromptAddresses are the three ways out, spelled once so the message
// and the tests cannot drift (Rule 14: a refusal that hides the remedy is the
// dead end Rule 15 bans).
var ShortBarePromptAddresses = struct {
	// Sentence is the ordinary shape: a complete request as one argument.
	Sentence string
	// Explicit is the unambiguous channel: the flag states "this is input",
	// which is exactly what a one-word prompt cannot state.
	Explicit string
	// VerbHelp is how a reader who meant a COMMAND finds the real spelling.
	VerbHelp string
}{
	Sentence: cliverbs.Program + ` "<the request, as a sentence>"`,
	Explicit: cliverbs.Program + ` --prompt "<text>"`,
	VerbHelp: cliverbs.Program + ` <verb> ` + AgentSpawnGuardHelpAnchor,
}

// GuardShortBarePrompt decides whether `text` is long enough to be a prompt.
//
// The second return reports that the count could NOT be taken (the injected
// counter is nil, or the encoder failed). That is NOT a refusal: an
// unmeasurable length must not turn into "everyone is blocked", which would
// make a broken vocabulary file look like a strict guard. The caller prints the
// reason and lets the run proceed — dropped in silence it would be Rule 15's
// silent failure, and a refusal on it would be an invented one.
func GuardShortBarePrompt(text string, count BarePromptCounter) (AgentSpawnDecision, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		// Not this face's question: an empty prompt never reaches the run path
		// (cmd/agent refuses `No input provided` before anything is spent).
		return AgentSpawnDecision{Allowed: true}, nil
	}
	if count == nil {
		return AgentSpawnDecision{Allowed: true}, fmt.Errorf("%s: no token counter was supplied, so the length floor could not be applied", ShortBarePromptFace)
	}
	n, err := count(trimmed)
	if err != nil {
		return AgentSpawnDecision{Allowed: true}, fmt.Errorf("%s: token count failed: %w", ShortBarePromptFace, err)
	}
	if n >= BarePromptMinTokens {
		return AgentSpawnDecision{Allowed: true}, nil
	}
	return AgentSpawnDecision{Allowed: false, Reason: shortBarePromptReason(trimmed, n)}, nil
}

// shortBarePromptReason renders the refusal. Every fact in it was measured:
// the token count comes from the same encoder the model is charged by, and the
// exit code is the one RefuseShortBarePrompt exits with.
func shortBarePromptReason(text string, tokens int) string {
	return fmt.Sprintf(
		"refusing to start (%s): %q is %d token(s) — below the %d-token floor this binary\n"+
			"  accepts as a prompt. A word this short cannot carry a request; in practice it is\n"+
			"  a COMMAND typed wrong — a leaf verb without its family word (`gopi submit` for\n"+
			"  `gopi task submit`), or two words in the wrong order — and the command line cannot\n"+
			"  tell that apart from a prompt: a shell's quotes never reach argv, so `gopi submit`\n"+
			"  and `gopi \"submit\"` arrive as the same three bytes.\n"+
			"  Say what you want, as a sentence:\n"+
			"    %s\n"+
			"  Or state explicitly that the text is input (the flag disarms this floor):\n"+
			"    %s\n"+
			"  Or, if you meant a command, ask what the real spelling is:\n"+
			"    %s\n"+
			"  %s%d — this refusal started nothing: no session, no ledger row, no clone ground,\n"+
			"  no tokens. Measured through this binary's own tokenizer: a complete question costs\n"+
			"  %d tokens or more (`你好吗` = 2, `who are you?` = 4, `where is the capital of China?` = 7).",
		ShortBarePromptFace, text, tokens, BarePromptMinTokens,
		ShortBarePromptAddresses.Sentence,
		ShortBarePromptAddresses.Explicit,
		ShortBarePromptAddresses.VerbHelp,
		AgentSpawnGuardExitCodeLabel, CLIUsageExitCode,
		BarePromptMinTokens)
}

// RefuseShortBarePrompt prints the refusal to stderr and exits with
// CLIUsageExitCode.
//
// The code is the POSIX USAGE class (2), deliberately NOT
// AgentGuardRefusalExitCode (3): 3 says "this launch is forbidden" (a rule
// about who may run what), while this refusal says "the command line is
// incomplete — write the request out and re-issue it". A caller scripting gopi
// must be able to tell the two apart, and the operator's remedy differs.
func RefuseShortBarePrompt(d AgentSpawnDecision) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", cliverbs.Program, d.Reason)
	os.Exit(CLIUsageExitCode)
}
