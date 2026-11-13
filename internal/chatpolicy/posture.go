package chatpolicy

// posture.go — the STARTUP POSTURE: the answer the chat surface gives when the
// rule table has NO opinion about a command line.
//
// WHY IT IS NOT IN decide.go. The table and the posture answer two different
// questions, and conflating them is what makes a policy change a code change:
//
//	the table  : "what do the operator's RULES say about THIS line?"
//	the posture: "and when they say nothing?"
//
// The posture is resolved ONCE at process start (internal/agent/
// chat_policy_posture.go reads the channels) and then FROZEN: it is a value,
// not a global, so nothing running inside the session — a model, a tool, a
// later decision — can widen it. A posture that a running process could revise
// is a posture the operator cannot reason about when they set it.

// Posture is the FROZEN default answer, resolved once at startup.
//
// THE ZERO VALUE IS "ON-REQUEST", NOT "ALLOW". That is deliberate and it is the
// safety property of this type: a Posture a caller forgot to resolve (or built
// as Posture{}) asks rather than runs. `decision` being Go's zero Decision
// would otherwise have meant Allow — the exact silent widening this package
// exists to prevent — so the zero value is intercepted in Decision() instead of
// being trusted.
type Posture struct {
	// decision is the answer this posture gives an unmatched command line.
	decision Decision
	// set records that a value was RESOLVED (by name or by the resolver).
	// Unset means "nobody said", which reads as on-request.
	set bool
	// source names the CHANNEL that decided, in the words a startup log prints
	// ("--policy", "GOPI_CHAT_POLICY", `settings.json "chat_policy"`, …). It is
	// not decoration: "ask because that is the default" and "ask because the
	// operator's file says so" are one value and two different facts.
	source string
	// note carries a compatibility remark the resolver wants printed once (the
	// chat_op_auto mapping). Empty when there is nothing to say.
	note string
}

// THE POSTURE SPELLINGS. The canonical three are the same words a rule line
// uses (decisionAllowWord / decisionPromptWord / decisionForbiddenWord): an
// operator who has read one fence has read this vocabulary too. The aliases
// exist because each of them is a word already in circulation for the same
// idea, and refusing them would only make the knob harder to set.
//
//	allow      | yolo | auto          -> run it (yolo: EVERY command, no branch)
//	on-request | ask  | prompt        -> ask the operator (the DEFAULT)
//	forbidden  | deny | never         -> refuse without asking
const (
	// PostureDefaultWord is the documented default, spelled as the operator
	// reads it on the startup line.
	PostureDefaultWord = "on-request"

	// PostureYoloWord is the ALLOW spelling an operator most often types. It is
	// an alias, not a separate mode: there is no code path that behaves
	// differently for "yolo" than for "allow" (a second mode is how a
	// "yolo but still refuse push --force" exception grows back in).
	PostureYoloWord = "yolo"

	// PostureAskWord / PostureDenyWord are the conversational aliases.
	PostureAskWord  = "ask"
	PostureDenyWord = "deny"

	// PostureUnlessTrustedWord is the STRICTEST spelling: a line runs only when a
	// rule says `allow` for it, and a line the table is SILENT about is refused
	// rather than asked. It exists for a console whose operator is not watching —
	// an approval request nobody will read allows nothing anyway, so the posture
	// says so outright instead of producing text with no reader.
	PostureUnlessTrustedWord = "unless-trusted"
)

// postureAliases maps every accepted spelling onto the ONE canonical decision.
// One table (Rule 1/13): the resolver, the startup log and the error message all
// read it, so the accepted vocabulary cannot drift between them.
var postureAliases = []struct {
	word     string
	decision Decision
}{
	{decisionAllowWord, DecisionAllow},
	{PostureYoloWord, DecisionAllow},
	{"auto", DecisionAllow},
	{PostureDefaultWord, DecisionPrompt},
	{PostureAskWord, DecisionPrompt},
	{decisionPromptWord, DecisionPrompt},
	{decisionForbiddenWord, DecisionForbidden},
	{PostureDenyWord, DecisionForbidden},
	{"never", DecisionForbidden},
	{PostureUnlessTrustedWord, DecisionForbidden},
}

// decisionForPostureWord resolves a posture spelling.
func decisionForPostureWord(word string) (Decision, bool) {
	for _, a := range postureAliases {
		if a.word == word {
			return a.decision, true
		}
	}
	return DecisionPrompt, false
}

// PostureWords lists the accepted posture spellings in DOCUMENTATION order —
// least to most restrictive, aliases beside their canonical word — for usage
// pages and refusal messages. It is built from postureAliases so the message
// cannot list a word the resolver refuses.
func PostureWords() []string {
	words := make([]string, 0, len(postureAliases))
	for _, a := range postureAliases {
		words = append(words, a.word)
	}
	return words
}

// NewPosture returns the frozen posture for a decision, naming the channel that
// decided. It is the constructor every channel-specific branch goes through, so
// no caller can build a Posture with an unset `set` flag and get the silent
// "allow" the zero value must never mean.
func NewPosture(d Decision, source string) Posture {
	return Posture{decision: d, set: true, source: source}
}

// PostureNamed resolves a posture SPELLING (the CLI value or an env value).
//
// The returned bool is false for an unknown word, and the WORD is returned
// unchanged by the caller's error message: a typo must be reported, never
// silently read as the default (Rule 15 — a knob whose off state is
// indistinguishable from its default has no off state).
func PostureNamed(word, source string) (Posture, bool) {
	d, ok := decisionForPostureWord(word)
	if !ok {
		return Posture{}, false
	}
	return NewPosture(d, source), true
}

// WithDecision returns a copy carrying a DIFFERENT answer for an unmatched line,
// keeping the source that decided. It is the ONE constructor a CARRIER uses when
// its own surface cannot honor the operator's answer (internal/tools/
// chat_policy_carrier.go: `prompt` has no reader on a face with no approval
// channel), so the degradation is a value transformation rather than a second
// decision path — the rule table it composes with is untouched, and `forbidden`
// still wins.
func (p Posture) WithDecision(d Decision) Posture {
	p.decision = d
	p.set = true
	return p
}

// WithNote returns a copy carrying a one-time compatibility remark. It returns a
// VALUE, so the frozen posture it came from is untouched (a mutating setter here
// would put a writable posture back into a running process).
func (p Posture) WithNote(note string) Posture {
	p.note = note
	return p
}

// Decision answers an UNMATCHED command line. An unresolved Posture (the zero
// value) asks — see the type comment for why that is the safe reading.
func (p Posture) Decision() Decision {
	if !p.set {
		return DecisionPrompt
	}
	return p.decision
}

// Source names the channel that decided, or PostureDefaultWord's channel when
// nothing was declared.
func (p Posture) Source() string {
	if p.source == "" {
		return "default"
	}
	return p.source
}

// Note reports the one-time compatibility remark ("" when there is none).
func (p Posture) Note() string { return p.note }

// AllowsEverything reports whether this posture answers ALLOW to every
// unmatched line — the "yolo" reading. It is the ONE predicate a caller asks
// when it wants to know whether the rules can still refuse something, so the
// answer cannot be re-derived from Decision() in two places and drift.
func (p Posture) AllowsEverything() bool { return p.Decision() == DecisionAllow }

// String renders the posture for the startup line: the canonical word, never an
// alias, so two runs that mean the same thing log the same bytes.
func (p Posture) String() string { return p.Decision().String() }

// Resolve is the LAYER the rules and the posture are combined through — the one
// function a call site will use once wiring lands (NOT wired yet, by design).
//
// It encodes the relationship the two layers have:
//
//   - a rule that matched decides, whatever the posture is — EXCEPT under a
//     posture that allows everything, where the rule table is not consulted at
//     all: "yolo" means every command runs, with no exception branch, which is
//     exactly why no verb or path is named anywhere in this package.
//   - an unmatched line takes the posture's answer.
func (p Posture) Resolve(pol *Policy, words []string) Match {
	if p.AllowsEverything() {
		return Match{Matched: true, Decision: DecisionAllow, Rule: Rule{Source: p.Source()}}
	}
	if pol == nil {
		return Match{Matched: true, Decision: p.Decision(), Rule: Rule{Source: p.Source()}}
	}
	m := pol.DecideWords(words)
	if m.Matched {
		return m
	}
	m.Matched = true
	m.Decision = p.Decision()
	m.Rule = Rule{Source: p.Source()}
	return m
}
