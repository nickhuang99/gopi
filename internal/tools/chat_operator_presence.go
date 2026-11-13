package tools

// chat_operator_presence.go — WHO is on the other end of a `prompt` decision.
//
// THE DEFECT THIS FILE EXISTS TO REMOVE (measured 2026-10-22, unattended chat
// runs): a `prompt` decision's text ends "DO THIS NOW: ask the operator in the
// conversation … and wait for their answer", and on a run that declared NO
// OPERATOR (`gopi chat --once`, agent.Agent.Unattended) that instruction is
// unsatisfiable — nobody will answer, so the model either waits for a turn that
// never comes or spends its remaining turns re-spelling a call that meets the
// same rule. The same run's own prompt already forbids exactly this ("do NOT ask
// the operator — nobody will", chat_unattended.go), so the surface was
// contradicting itself in two places a model reads in the same turn.
//
// It is worse than a deadlock, because the SAME message tells the model to keep
// its artifacts in THIS run's own session directory (Rule 37: chat artifacts are
// not committed; they live under [CURRENT SESSION]) — and that directory sits
// OUTSIDE every git work tree, so no `write <tree>` rule mentions it, so the
// POSTURE answers, and the default posture is `prompt *`. A run told "deliver to
// your session directory" was refused by a rule that says "ask me first" to a
// human who does not exist.
//
// THE TWO ANSWERS, and each is stated where it is decided:
//
//	1. A run's OWN SESSION DIRECTORY is exempt from a PROMPT decision
//	   (chatWriteTargetIsOwnSessionArtifact). It is still refused when the rule
//	   says `forbidden` — a forbidden rule is not a question, and this exemption
//	   is not a hole in it. The directory is not the operator's file, not the
//	   project's tree, and Rule 37 already made it THE place a chat's artifacts
//	   belong, so "ask me first about MY OWN transcript directory" was never the
//	   sentence the operator wrote.
//
//	2. When NO OPERATOR is on the run, a prompt decision is answered
//	   DETERMINISTICALLY and does not ask (chatPromptWithoutOperator): the text
//	   says the wait cannot end, names what to deliver instead, and names the
//	   authorisation that would have to exist BEFORE the run started. Asking is
//	   still what an ATTENDED console does — there a human really is there to
//	   answer, and the approval channel (chat_approval_authorization.go) really
//	   is that answer.
//
// WHY PRESENCE IS IN-PROCESS STATE AND NOT AN ENV VAR (Rule 35): the fact is
// already known in-process — cmd/agent reads it from the run's own contract
// (`--once` declares it; cli_chat.go sets agent.Agent.Unattended from it) — so a
// second channel would be a persistent artifact invented to carry a value the
// process already holds, and an ambient one at that, which a child process could
// inherit after the run it described had ended. The setter is called by the ONE
// place that knows the contract, and the zero value is the ATTENDED reading: a
// process that never declares the absence asks a human, which is what every
// surface did before this file existed.

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/nick/gopi/internal/chatpolicy"
	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/signalrec"
)

// chatOperatorAbsent is the process-wide PRESENCE flag. It is a value under a
// mutex rather than a plain bool so a test that flips it and a tool call that
// reads it cannot race; the DEFAULT is false (a human is there), so the
// unattended answer can only be reached by a caller that declared it.
var chatOperatorAbsent struct {
	mu     sync.RWMutex
	absent bool
}

// SetChatOperatorAbsent declares whether THIS process's chat run has an operator
// on the other end. It is called by cmd/agent from the run's own contract (the
// `--once` face sets agent.Agent.Unattended; cli_chat.go publishes the same fact
// here) and by tests. Passing false restores the attended default, which is also
// the zero value — there is no third state and nothing to leave enabled.
func SetChatOperatorAbsent(absent bool) {
	chatOperatorAbsent.mu.Lock()
	defer chatOperatorAbsent.mu.Unlock()
	chatOperatorAbsent.absent = absent
}

// ChatOperatorAbsent reports whether a `prompt` decision on this run has NOBODY
// to answer it. False means an attended console: the approval channel is the
// operator's own next message, and the approval text says so.
func ChatOperatorAbsent() bool {
	chatOperatorAbsent.mu.RLock()
	defer chatOperatorAbsent.mu.RUnlock()
	return chatOperatorAbsent.absent
}

// chatWriteTargetIsOwnSessionArtifact reports whether path lies inside THIS
// process's own session directory — the one `[CURRENT SESSION]` names and Rule 37
// puts a chat's artifacts in. It is the exemption's ONLY predicate, and it is
// deliberately narrow:
//
//   - The directory comes from signalrec.SessionDir(), the SAME resolver that
//     records the run's own handoff and that the prompt's [CURRENT SESSION] block
//     is built from — so the guard and the sentence the model reads cannot
//     disagree about where its directory is (Rule 2: one reader).
//   - An empty resolver answer is NOT a wildcard: with no session directory of
//     its own this process exempts nothing (the defect a "path under X" test
//     usually ships — an unset root matching every path).
//   - Containment is measured with pathWithin (cleaned segments and stat
//     identity), never a string prefix: a sibling directory whose name merely
//     starts with the session directory's name is not inside it.
//
// The caller applies it ONLY on the prompt branch: a `forbidden` rule still
// forbids the directory, and the allow branch never needed the exemption.
func chatWriteTargetIsOwnSessionArtifact(path string) bool {
	dir := strings.TrimSpace(signalrec.SessionDir())
	target := strings.TrimSpace(path)
	if dir == "" || target == "" {
		return false
	}
	if !filepath.IsAbs(target) {
		// A relative target is measured against the process's own working
		// directory, which is how the write tool resolves it. Its absolute
		// spelling is therefore the only comparable form.
		abs, err := filepath.Abs(target)
		if err != nil {
			return false
		}
		target = abs
	}
	return pathWithin(target, dir)
}

// chatUnattendedPromptBody is the TAIL both `prompt`-decision arms append on a
// run that declared no operator: the part that would otherwise say "ask the
// operator and wait". It keeps the ONE prefix the arms already wrote (still a
// human decision, still nothing run/written) and replaces only the asking
// sentence with what is actually true here.
//
// WHAT IT MUST NEVER SAY is the point of the function: "ask the operator", "wait
// for their answer", "retry once they say yes" are instructions a run with no
// operator cannot carry out, and a model told to do the impossible spends its
// turns trying (measured: a `--once` run re-issued the same refused call until its
// turn budget ran out). So the text NAMES the dead end, hands over the two routes
// that exist on THIS run, and names the authorisation that is decided BEFORE the
// run starts.
func chatUnattendedPromptBody() string {
	return "On this run NO OPERATOR IS READING: the process declared an unattended run, so there is nobody to " +
		"answer a question and NOTHING IN THIS RUN WILL APPROVE IT — asking, waiting for a next turn, and " +
		"re-issuing the same call all reach the same answer. It did NOT run.\n" +
		"DO THIS INSTEAD, in this order: (1) deliver what the rules DO allow — a read, a target an `allow` rule names, " +
		"or THIS run's own session directory (the one `[CURRENT SESSION]` names in your prompt), which is exempt from " +
		"a `" + decisionPromptWord() + "` decision, so your report and your evidence go there and WILL be written. " +
		"(2) State in your answer, in one clause each, which target was refused and which deliverable is missing " +
		"because of it, so the operator acts on a real fact instead of a stall. (3) If the deliverable genuinely " +
		"requires that target, the authorisation is decided BEFORE a run starts and is the operator's to give: an " +
		"`allow` rule for it in the project's chat-policy fence, or a run started with `" + chatPolicyRerunSpelling() +
		"`. Do not re-spell the target to slip past the rule."
}

// chatPolicyRerunSpelling is the flag spelling the unattended text hands its
// reader: the ONE authorisation a run cannot give itself. It is composed from the
// leaf constant (internal/cliverbs.PolicyFlag) so this text and the parser that
// reads the flag cannot drift (Rule 1/13/14), and it names the widest posture
// rather than inventing a bespoke approval verb for these two refusals.
func chatPolicyRerunSpelling() string {
	return cliverbs.Chat + " " + cliverbs.PolicyFlag + "=" + chatPolicyWidestPostureWord()
}

// chatPolicyWidestPostureWord is the posture word that runs everything, taken
// from the posture vocabulary rather than typed (Rule 14) so a rename moves the
// refusals with the parser that reads the flag.
func chatPolicyWidestPostureWord() string { return chatpolicy.PostureYoloWord }

// decisionPromptWord is the decision word the rules use, from the decision's own
// String() so the text and the matcher cannot drift.
func decisionPromptWord() string { return chatpolicy.DecisionPrompt.String() }
