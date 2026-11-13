package chatpolicy

// Decision is the THREE-VALUED answer a rule gives to a command line.
//
// It is deliberately not a bool. The middle value — Prompt — is the one a bool
// cannot express: "this is not obviously safe, and it is not obviously
// forbidden either; a human has to say yes." Collapsing Prompt into Allow
// loses the question; collapsing it into Forbidden makes an over-broad rule
// indistinguishable from a real ban, which is how a rule table stops being
// read (precedent: codex-rs execpolicy's Decision::Allow/Prompt/Forbidden,
// hermes tools/approval.py's three-way detect_dangerous_command answer).
type Decision int

const (
	// DecisionAllow runs the command without asking.
	DecisionAllow Decision = iota
	// DecisionPrompt stops and asks the operator. It is NOT a refusal: the
	// consumer's job is to route "may I run this?" to the human (on a face with
	// nobody to ask, that routing itself declines — the caller's decision, not
	// this package's).
	DecisionPrompt
	// DecisionForbidden refuses without asking. A rule that says this means the
	// command is wrong to run at all, so there is nothing to ask about.
	DecisionForbidden
)

// decisionAllowWord / decisionPromptWord / decisionForbiddenWord are the exact
// spellings a rule line uses AND the exact strings String prints. Declared
// once (Rule 1/13): a rule file, the parser, the error message and the
// documentation all reference these names, so the vocabulary cannot drift
// between what an operator writes and what the code accepts.
const (
	decisionAllowWord     = "allow"
	decisionPromptWord    = "prompt"
	decisionForbiddenWord = "forbidden"
)

// decisionWords is the accepted vocabulary in DOCUMENTATION order — the order
// the usage/error message lists, from least to most restrictive. A message
// that lists them in another order invites the reader to guess at a severity
// ladder the code does not have.
var decisionWords = []string{decisionAllowWord, decisionPromptWord, decisionForbiddenWord}

// String renders the decision as the word a rule line would have used.
// An out-of-range value is named rather than silently printed as "allow": a
// bare zero value that meant Allow would make a decode bug look like a pass.
func (d Decision) String() string {
	switch d {
	case DecisionAllow:
		return decisionAllowWord
	case DecisionPrompt:
		return decisionPromptWord
	case DecisionForbidden:
		return decisionForbiddenWord
	default:
		return "decision(" + itoa(int(d)) + ")"
	}
}

// decisionNamed reports whether s is the spelling of one of the three
// decisions and returns it. Case-SENSITIVE on purpose: a rule file is read by
// a human and the machine reads the same bytes, so "Allow" is not the word —
// it is a typo the parser names with its line number rather than a synonym.
func decisionNamed(s string) (Decision, bool) {
	switch s {
	case decisionAllowWord:
		return DecisionAllow, true
	case decisionPromptWord:
		return DecisionPrompt, true
	case decisionForbiddenWord:
		return DecisionForbidden, true
	}
	return 0, false
}

// itoa is strconv.Itoa spelled locally so decision.go's String stays total
// without pulling the strconv documentation surface into the reader's eye; the
// only values that reach it are negative or > 2 (a decode bug), which is
// exactly the case that must still print something.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
