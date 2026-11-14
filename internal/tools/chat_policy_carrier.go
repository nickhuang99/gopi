package tools

// chat_policy_carrier.go — WHICH FACE a turn is answered on, and the ONE place
// that face's own declaration is allowed to change an `prompt` answer.
//
// THE MEASURED DEFECT (task 01a159ad, epic on
// TestWebChat_R2_ToolsFlagRoutesThroughChatOpsAndStreamsToolFrames).
// `Agent.ChatOps` sets `a.ChatMode = true` for the length of a turn, and the chat
// write boundary reads exactly that flag (internal/agent/chat_write_guard.go). The
// flag is set by the CONSOLE and by every OTHER carrier that drives the same
// engine — `gopi web --tools`, `gopi ui --tools`, `gopi cli --tools` — so a
// browser turn asking for "write a file" was answered with
//
//	[NEEDS OPERATOR APPROVAL — command NOT run]
//
// and the file was never written.
//
// The refusal is not wrong BY ITSELF: the shipped posture is `on-request`, the
// built-in table is `prompt *`, so the line is a question for a human. It is the
// wrong answer FOR THAT CARRIER, and the carrier says so in its own words: the
// `gopi web --tools` banner, its page and its help text all state "executes tools
// on this host with no approval gate — loopback, local-shell-level trust". Over
// HTTP the operator IS the one who typed the line, so `prompt`'s instruction
// ("ask the operator in the conversation and wait") loops the question back to
// the only person who can answer it: the turn dead-ends, and the face's own
// declared contract is contradicted by its own tool dispatch.
//
// WHAT THIS FILE DECIDES — a DEGRADATION, not a widening invented here.
//
//   - `forbidden` ALWAYS WINS on every carrier. Nothing here can run a line the
//     operator forbade; that is the one answer a no-approval face must still obey.
//   - `allow` is unchanged on every carrier.
//   - `prompt` is the ONLY answer this file touches, and only on the carriers that
//     have NO interactive approval channel: there the decision has no reader, so
//     it degrades to what the face already declared before the turn started. On
//     the console (`terminal`) it is left exactly as it was — a human is in the
//     turn, so `prompt` really does mean "ask the operator" (the semantics
//     chat_policy_gate.go fixes in code).
//
// The direction of that degradation is the one the face DECLARES, never a default
// invented here: a carrier that has not declared an answer (an unknown name) gets
// the frozen behaviour back unchanged, which is the safe direction.
//
// RULE 1/13/14: the carrier names, the declaration table and the degradation live
// HERE, so a second face cannot answer the same question differently. The
// console's own pair (`ChatPolicyFrozen`) is read, never rewritten — this file
// returns a pair, it does not freeze one.
//
// RULE 35: no new configuration channel. The carrier is a value the caller already
// knows (it is the face it is serving), passed down as an argument; there is
// nothing to set, nothing to remember and nothing to leave enabled.

import (
	"github.com/nickhuang99/gopi/internal/chatpolicy"
)

// The carriers that drive Agent.Chat / Agent.ChatOps. VALUES (Rule 1), not prose
// typed at each call site: the guard reads them, the startup notices name them and
// the tests address them by name.
const (
	// ChatPolicyCarrierTerminal is the console carriers (`gopi chat`, the bare
	// prompt, the multi-line editor): a human is IN the turn, so the frozen
	// operator posture governs untouched.
	ChatPolicyCarrierTerminal = "terminal"
	// ChatPolicyCarrierWeb is `gopi web` (HTTP + SSE, browser page).
	ChatPolicyCarrierWeb = "web"
	// ChatPolicyCarrierUI is `gopi ui` (the loopback display engine).
	ChatPolicyCarrierUI = "ui"
	// ChatPolicyCarrierCLI is `gopi cli` (the text-browser carrier over the same
	// web engine).
	ChatPolicyCarrierCLI = "cli"
)

// chatPolicyCarrierDeclaresNoApprovalChannel is the ONE declaration table: the
// faces whose own banner/page/help states that tool turns run with no approval
// gate. Membership is the whole of the fact; a face that does not appear here gets
// the frozen behaviour back unchanged (the safe direction — an undeclared carrier
// is a console, not a silently widened one).
var chatPolicyCarrierDeclaresNoApprovalChannel = map[string]bool{
	// "turns run the full gopi toolset with no approval gate (loopback only,
	// local-shell trust)" — cli_web.go's startup line and help page.
	ChatPolicyCarrierWeb: true,
	// the same text rendered in the ui carrier's name (uiToolsNoticeFormat).
	ChatPolicyCarrierUI: true,
	// cli_term.go's termToolsNotice: "turns run the full gopi toolset with no
	// approval gate (treat as local-shell trust)".
	ChatPolicyCarrierCLI: true,
}

// CarrierHasApprovalChannel reports whether a carrier can ASK a human mid-turn.
// The console can (the operator is typing); the HTTP/display carriers cannot —
// the question renders as text the asker is already reading.
func CarrierHasApprovalChannel(carrier string) bool {
	return !chatPolicyCarrierDeclaresNoApprovalChannel[carrier]
}

// CarrierDecision degrades one decision for a carrier — the three-sentence rule
// above, as code, so the two arms (bash, write/edit) cannot disagree about it.
func CarrierDecision(carrier string, d chatpolicy.Decision) chatpolicy.Decision {
	if d != chatpolicy.DecisionPrompt || CarrierHasApprovalChannel(carrier) {
		return d
	}
	return chatpolicy.DecisionAllow
}

// ForCarrier resolves the (policy, posture) pair a CARRIER's turn is judged by:
// the frozen operator pair, with the posture degraded through CarrierDecision. The
// rule table is returned UNCHANGED — `forbidden` and `allow` rules keep deciding,
// which is the half that makes this a degradation rather than a widening.
func ForCarrier(carrier string) (*chatpolicy.Policy, chatpolicy.Posture) {
	pol, posture := ChatPolicyFrozen()
	if CarrierHasApprovalChannel(carrier) {
		return pol, posture
	}
	return pol, posture.WithDecision(CarrierDecision(carrier, posture.Decision()))
}

// ChatWritePathRefusalForCarrier judges one write/edit target the way a CARRIER's
// turn must be judged: the same predicate `ChatWritePathRefusal` applies, with the
// carrier's degradation applied to whatever the frozen pair answered.
func ChatWritePathRefusalForCarrier(carrier, path, repoRoot, operatorTurn string) string {
	pol, posture := ChatPolicyFrozen()
	m := posture.Resolve(pol, chatWriteRuleWords(path, repoRoot))
	m.Decision = CarrierDecision(carrier, m.Decision)
	return chatWritePathRefusalMatch(path, operatorTurn, m)
}

// ChatBashWriteRefusalForCarrier is the bash arm's carrier entry point: the same
// clause reader arriving at the same degradation.
func ChatBashWriteRefusalForCarrier(carrier, command, workdir, operatorTurn string) string {
	m := chatBashPolicyMatch(command)
	m.Decision = CarrierDecision(carrier, m.Decision)
	return chatBashWriteRefusalMatch(command, m, operatorTurn)
}
