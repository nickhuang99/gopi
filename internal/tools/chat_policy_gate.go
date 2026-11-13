package tools

// chat_policy_gate.go — the CHAT WRITE BOUNDARY's DECISION half: the frozen
// (policy, posture) pair this process judges bash commands by, and the three-way
// mapping from chatpolicy.Decision to what a tool call actually gets back.
//
// WHAT THIS REPLACES, AND WHY IT HAD TO GO. Until this file existed the guard
// decided by READING git's grammar: a hardcoded table of fourteen git
// subcommands, a partly-allowed pair judged by pathspec predicates, a read-only
// sub-table, and — the growth that made the shape untenable — a rule
// that refused every line whose references it could not resolve (`ssh -o …`,
// `$FILES`, `~`, a glob, a redirection): "unreadable, therefore refused". Every
// one of those was the OPERATOR's judgement compiled into the binary, so
// "may this chat push?" was a code change and a release, and a shape the reader
// had never been taught failed closed even when the operator would have said yes.
//
// The judgement now comes from the operator's own rule table
// (internal/chatpolicy: a ```chat-policy fence in the project's GOPI.md) and, for
// a line the table has no opinion about, from the STARTUP POSTURE (--policy /
// GOPI_CHAT_POLICY / settings.json "chat_policy", resolved once by
// internal/agent and frozen into this package at process start).
//
// THE SPLIT OF WORK, stated so nobody re-grows a rule here:
//
//	internal/chatpolicy  : fence text -> Policy; words -> Match (pure, no I/O)
//	this package         : the ONE tokenizer + clause splitter, and the mapping
//	                       below from a Decision to the text a tool call returns
//	internal/agent       : the CHANNELS, resolved once, frozen through
//	                       SetChatPolicy
//
// NOTHING IN THIS FILE NAMES A COMMAND. The absence is the point: the only
// vocabulary here is the three decisions. A verb word appearing in the prose
// below is an EXAMPLE of what a rule might say — never a comparison, never a
// special case (Rule 6 of this subtask's brief: no verb-shaped judgement may
// remain, and prose may only illustrate).

import (
	"strings"
	"sync"

	"github.com/nick/gopi/internal/chatpolicy"
	"github.com/nick/gopi/internal/toolnames"
)

// chatWriteApprovalPrefix opens the text a PROMPT decision returns.
//
// THE PROMPT SEMANTICS, FIXED HERE IN CODE SO THEY CANNOT DRIFT:
//
//	A PROMPT DECISION IS NOT AN ALLOW AND NOT A REFUSAL. The dispatch can only
//	return text in place of a tool call's result, so "prompt" must mean exactly
//	this: the command WAS NOT RUN, the operator is the only one who can authorize
//	it, and the MODEL's move is to ask its human in conversation and wait for a
//	yes before calling bash again. It is deliberately not a refusal: a refusal
//	tells the model the action is wrong (and here it may be perfectly right), so
//	collapsing prompt into forbidden would teach the model to stop asking. And
//	it is emphatically not an allow: the command did not run.
//
//	THE ANSWER HAS A ROUTE: the operator's own next message
//	(chat_approval_authorization.go). It is not an interactive prompt and not a
//	stored grant — the guard reads the turn's text, so a turn that says nothing
//	approves nothing and no state can be left open.
//
// The prefix is a named constant because it is the ONE string a caller (and the
// tests) can key on to tell an approval request from a refusal — the two must be
// distinguishable without parsing prose.
const chatWriteApprovalPrefix = "[NEEDS OPERATOR APPROVAL — command NOT run]"

// ChatWriteApprovalPrefix is that prefix, EXPORTED for readers OUTSIDE this
// package: a dispatch-level test (internal/agent) and any surface that must tell
// the three answers apart keys on the same string. It is an alias, not a second
// literal (Rule 13/14) — changing the wording moves both readers.
const ChatWriteApprovalPrefix = chatWriteApprovalPrefix

// THE TWO REFUSAL MARKERS, and why there are exactly two rather than one.
//
// A refusal used to open with ONE string — "Refused by the CHAT WRITE BOUNDARY" —
// on all three arms (bash, write/edit, the cleanup face's `--apply`). That is
// accurate for two of them and WRONG for the first, and the cost was measured
// (task 01a1ae0b, 2026-11-06): `echo hello` was refused because the operator's
// fence had `allow ls` / `allow cat` / `allow printf` and no `allow echo`, and the
// text a reader received said a WRITE boundary had stopped it. A command that
// writes nothing cannot be refused by a write boundary, so the reader drew the
// only conclusion the words allowed — "this console may not run read commands" —
// and stopped taking evidence it was entitled to take.
//
// The two markers therefore name the two different questions a refusal answers:
//
//	chatBashRefusalMarker          a COMMAND boundary: may this line RUN?
//	chatWriteBoundaryRefusalMarker a DEPOSIT boundary: may this console WRITE there?
//
// They are constants (Rule 1/13/14) because tests on both sides of the package
// boundary — and the internal/agent dispatch tests — must tell the answers apart
// without parsing prose.
const (
	chatBashRefusalMarker          = "Refused by the CHAT COMMAND BOUNDARY"
	chatWriteBoundaryRefusalMarker = "Refused by the CHAT WRITE BOUNDARY"
)

// chatPolicyFenceWord names the operator's rule fence in a refusal. It is the FILE
// whose missing line is the cause when nothing matched, and spelling it here once
// keeps every place that mentions it in step (Rule 13/14).
const chatPolicyFenceWord = "```chat-policy fence"

// chatBashNoRuleCause is the FIRST of the two causes a bash refusal can have: the
// fence is SILENT about the line, so the startup posture answered. Its sibling
// ("it matches the rule …") is the other cause — the operator wrote a line saying
// no. A reader must be able to tell them apart, because one is fixed by adding a
// line to the fence and the other by removing one.
const chatBashNoRuleCause = "no rule in the project's " + chatPolicyFenceWord + " mentions"

// chatBashCompoundClauseLead opens the per-clause accounting of a CHAIN
// (`a && b`, `a; b`, `a | b`). It exists because the composition rule — the WHOLE
// line takes the strictest clause (chatpolicy.Policy.DecideClauses) — is invisible
// to a reader who sees only the line and one refusal: a pure read inside a chain is
// refused because of its NEIGHBOUR, and a text that does not say so teaches
// "ls is not allowed" about a fence that says `allow ls`.
const chatBashCompoundClauseLead = "This line is a CHAIN of clauses and the WHOLE line takes the strictest " +
	"of them, so the clause named below is the one that refused it — the other clauses did not:"

// chatWriteApprovalBoard is the sentence the approval text carries, kept as its
// own constant so the four facts below are stated once: nothing ran, a human is
// the only authority, the model must ask IN CONVERSATION, and a plain "yes" in
// the operator's next message is what authorizes the retry.
//
// IT STATES A ROUTE THAT EXISTS. The sentence used to end at "this surface has no
// approval channel", which was true and also meant the retry it told the model to
// make could never succeed: the same rule answered the same way forever
// (chat_approval_authorization.go states the measured deadlock). The channel is
// the operator's OWN NEXT MESSAGE — `ChatTurnApprovesTheToolQuestion` reads it —
// so the text names that as the way through, and the model's move is unchanged:
// ask, then retry THIS SAME call once the operator has said yes.
const chatWriteApprovalBoard = "This command matches a rule the operator marked `" +
	"prompt`, so it is neither allowed nor forbidden — it needs a HUMAN decision, and the human's decision is the " +
	"approval channel on this surface: the operator's own next message. The command did NOT run.\nDO THIS NOW: ask " +
	"the operator in the conversation — quote the exact command, say what it would do — and wait for their answer. " +
	"If they say yes (any plain affirmative in their message: \"yes\", \"go ahead\", \"可以\", \"跑吧\" …), call bash " +
	"with the SAME command again: their words in that turn run it. If they say no, use the narrower alternative " +
	"they approve of. Do not retry before they answer, and do not rephrase the command to slip past the rule."

// chatBashPolicyText renders the approval request for one decided command line,
// naming the rule that raised the question so the operator is told WHY the model
// stopped rather than being handed a bare "may I?".
func chatBashPolicyText(command string, m chatpolicy.Match) string {
	var b strings.Builder
	b.WriteString(chatWriteApprovalPrefix)
	b.WriteString(" `")
	b.WriteString(command)
	b.WriteString("`")
	if rule := chatPolicyRuleRendered(m); rule != "" {
		b.WriteString(" — matched rule `")
		b.WriteString(rule)
		b.WriteString("`")
	}
	b.WriteString(".\n")
	if ChatOperatorAbsent() {
		b.WriteString(chatUnattendedPromptBody())
		return b.String()
	}
	b.WriteString(chatWriteApprovalBoard)
	return b.String()
}

// chatPolicyRuleRendered renders the deciding rule for a reader: its line, the
// pattern as the operator wrote it, and where it came from. Empty when the
// decision came from the posture rather than from a rule (the posture names
// itself instead — see chatPolicySourceRendered).
func chatPolicyRuleRendered(m chatpolicy.Match) string {
	if m.Rule.Source == "" || len(m.Rule.Pattern) == 0 {
		return ""
	}
	where := m.Rule.Source
	if m.Rule.Line > 0 {
		where += ":" + itoaRuleLine(m.Rule.Line)
	}
	return m.Rule.PatternString() + "` (" + where + ")"
}

// itoaRuleLine renders a rule's 1-based line number without pulling strconv into
// this file's imports for one call site.
func itoaRuleLine(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
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

// chatBashPolicyForbiddenRefusal renders the refusal for a FORBIDDEN decision.
//
// IT ANSWERS A QUESTION ABOUT A COMMAND, NOT ABOUT A DEPOSIT (see the two markers
// above), and it names BOTH of the things a reader needs: WHICH CLAUSE of the line
// decided, and WHICH OF THE TWO CAUSES produced the answer —
//
//	the operator's fence says `forbidden` for it (a line to remove), or
//	the fence says NOTHING about it and the startup posture answered (a line to
//	  add, and the postured answer is the one to weigh).
//
// Without the first, a CHAIN (`cd /tmp && ls -l`) reads as though every clause were
// refused — and a reader concludes `ls` is not allowed while the fence says it is.
// Without the second, "no rule mentions this" is indistinguishable from "the
// operator forbade it", and the only repair a reader can imagine is the wrong one.
//
// The single-clause cause sentence names the rule that WOULD admit the line (its
// command word), because the fence is the operator's file: their missing line is
// the fact, and a reader who cannot name it cannot report it (Rule 15).
func chatBashPolicyForbiddenRefusal(command string, m chatpolicy.Match) string {
	var b strings.Builder
	b.WriteString(chatBashRefusalMarker + " (`")
	b.WriteString(command)
	b.WriteString("`): ")
	if rule := chatPolicyRuleRendered(m); rule != "" {
		b.WriteString("this command matches the rule `")
		b.WriteString(rule)
		b.WriteString(", which the operator marked `")
		b.WriteString(chatpolicy.DecisionForbidden.String())
		b.WriteString("`, so it is not runnable from this console and there is nothing to ask.\n")
	} else {
		b.WriteString("this command is not runnable from this console, and NOT because anyone forbade it: ")
		b.WriteString(chatBashNoRuleCause)
		b.WriteString(" it, so the startup posture — `")
		b.WriteString(chatpolicy.DecisionForbidden.String())
		b.WriteString("` from ")
		b.WriteString(chatPolicySourceRendered(m))
		b.WriteString(" — answered. Nothing was asked because there is nothing to ask. ")
		b.WriteString(chatBashMissingRuleHint(command))
		b.WriteString("\n")
	}
	b.WriteString(chatBashRefusalClauseAccount(command, m))
	b.WriteString("DO THIS INSTEAD: use a narrower command the fence DOES list (")
	b.WriteString(chatBashReadToolsRoute())
	b.WriteString("), keep artifacts in THIS run's own session directory (the one named by [CURRENT SESSION] in " +
		"your prompt), or — for anything that must be committed or that moves a ref — file a task with `gopi task " +
		"submit` and report its task id: the worker gets a deterministic worktree of its own. Widening the boundary " +
		"is the operator's sentence to write in their " + chatPolicyFenceWord + ", never something to route " +
		"around.")
	return b.String()
}

// chatPolicySourceRendered names the CHANNEL that answered when no rule did. It
// reads the source out of the Match the posture already stamped, and falls back to
// a sentence rather than to an empty string: a refusal that says "the posture — “
// — answered" tells a reader nothing about where to look (Rule 15).
func chatPolicySourceRendered(m chatpolicy.Match) string {
	if src := strings.TrimSpace(m.Rule.Source); src != "" {
		return src
	}
	return "the process's startup channel"
}

// chatBashMissingRuleHint names the fence line that WOULD admit a single-clause
// line — the command word of its first clause — because "no rule mentions it" is
// only actionable once the reader can see what the missing sentence looks like.
// It is empty for a chain (its account already names the decisive clause, and a
// proposed line for a chain would be a line nobody would write).
func chatBashMissingRuleHint(command string) string {
	clauses := chatShellClauses(chatShellTokens(command))
	if len(clauses) != 1 || len(clauses[0]) == 0 {
		return ""
	}
	return "The fence line that would admit this command is `" + chatpolicy.DecisionAllow.String() + " " +
		clauses[0][0] + "` — its absence is the whole cause, and adding it is the operator's sentence to write."
}

// chatBashReadToolsRoute names the TOOLS a reader can still use when bash is
// refused. It is the measured replacement for a promise the prompt used to make on
// the boundary's behalf ("read-only bash queries … at any moment, without asking
// anybody"), which the fence governs and the tools do not: a read tool answers
// whatever the fence says about bash. Composed from the tool-name constants
// (Rule 1/13/14), never typed.
func chatBashReadToolsRoute() string {
	return "e.g. `" + toolnames.ToolRead + "`, `" + toolnames.ToolCodeSearch + "`, `" +
		toolnames.ToolKeywordSearch + "`, `" + toolnames.ToolMemory + "` — the read and memory tools are not gated " +
		"by the fence at all"
}

// chatBashRefusalClauseAccount renders the per-clause accounting of a CHAIN, and
// NOTHING for a single-clause line (a one-clause line needs no accounting: the
// whole line IS the clause).
//
// WHY IT RECOMPUTES INSTEAD OF READING m. The decision handed in is the strictest
// answer over the clauses (chatpolicy.Policy.DecideClauses's rule, applied by
// chatBashPolicyMatch), and that Match does not carry WHICH clause produced it —
// the composition only keeps the winner. The clause list is therefore derived here
// through the SAME tokenizer and the SAME predicate, so this account cannot
// disagree with the decision it explains, and the assertion is on the rendered
// text rather than on a second judgement (Rule 13/14: no second criterion).
//
// The clause carrying the winning decision, the FIRST one, is named as decisive —
// the same tie-break chatBashPolicyMatch uses — so a chain with two bad clauses
// points at the one a reader should look at first.
func chatBashRefusalClauseAccount(command string, m chatpolicy.Match) string {
	clauses := chatShellClauses(chatShellTokens(command))
	if len(clauses) < 2 {
		return ""
	}
	pol, posture := ChatPolicyFrozen()
	rendered := make([]string, 0, len(clauses))
	decider, deciderText := -1, ""
	for i, clause := range clauses {
		text := strings.Join(clause, " ")
		own := posture.Resolve(pol, clause)
		if decider < 0 && own.Decision == m.Decision {
			decider, deciderText = i, text
		}
		rendered = append(rendered, "`"+text+"` → `"+own.Decision.String()+"`")
	}
	var b strings.Builder
	b.WriteString(chatBashCompoundClauseLead)
	b.WriteString(" ")
	b.WriteString(strings.Join(rendered, "; "))
	b.WriteString(". ")
	if decider >= 0 {
		b.WriteString("The clause that decided is `")
		b.WriteString(deciderText)
		b.WriteString("` (clause ")
		b.WriteString(itoaRuleLine(decider + 1))
		b.WriteString(" of ")
		b.WriteString(itoaRuleLine(len(clauses)))
		b.WriteString(").\n")
	}
	return b.String()
}

// --- the frozen holder ------------------------------------------------------

// chatPolicyFrozen is the PROCESS-WIDE frozen pair. A pointer under a mutex rather
// than two globals because the two values are ONE state: a policy read beside a
// posture from a different write is a decision nobody declared.
//
// FREEZE, NOT CONFIGURATION: SetChatPolicy is called ONCE by the face that
// resolved the channels (cmd/agent, at startup, and by the chat entry points that
// own the run). Nothing inside a session is expected to revise it; a running
// process that could widen its own boundary is a boundary the operator cannot
// reason about.
var chatPolicyFrozen = struct {
	mu   sync.RWMutex
	pol  *chatpolicy.Policy
	post chatpolicy.Posture
}{pol: defaultChatPolicy(), post: chatpolicy.NewPosture(chatpolicy.DecisionPrompt, chatPolicyDefaultSource)}

// chatPolicyDefaultSource names the channel when nothing declared a posture: the
// shipped default (on-request), the same word internal/agent's resolver prints.
const chatPolicyDefaultSource = "default"

// defaultChatPolicy parses the built-in table. A parse failure of a constant
// embedded in the binary is a programming error; the guard must not then judge by
// a half-read table, so it falls back to NO rules — which makes every command the
// POSTURE's question, the safe reading.
func defaultChatPolicy() *chatpolicy.Policy {
	pol, err := chatpolicy.Defaults()
	if err != nil {
		return &chatpolicy.Policy{Source: "built-in chat-policy defaults (unparseable)"}
	}
	return pol
}

// SetChatPolicy freezes the resolved rule table and posture for this process. A
// nil policy is the project having no rules; it is replaced by the built-in
// default table so the judgement never runs against nil (which would silently turn
// every command into the posture's question with no trace of why).
func SetChatPolicy(pol *chatpolicy.Policy, posture chatpolicy.Posture) {
	if pol == nil {
		pol = defaultChatPolicy()
	}
	chatPolicyFrozen.mu.Lock()
	chatPolicyFrozen.pol = pol
	chatPolicyFrozen.post = posture
	chatPolicyFrozen.mu.Unlock()
}

// chatOpCategoryFrozen holds the frozen CATEGORY table — the ```chat-op-category
// fence of the same GOPI.md — beside the pair above and under its OWN lock, so a
// caller can read one without the other (the same-origin test does) and so the
// category freeze can never be silently skipped by a caller that only meant to set
// the policy.
//
// IT IS A SEPARATE HOLDER FROM chatPolicyFrozen ON PURPOSE: the two tables answer
// two different questions ("may this line run?" vs "what is this action?"), and a
// single holder would invite a reader to treat one as the other — the exact
// conflation internal/chatpolicy/category.go exists to prevent.
var chatOpCategoryFrozen = struct {
	mu  sync.RWMutex
	tab *chatpolicy.CategoryTable
}{}

// SetChatOpCategories freezes the resolved category table for this process. Nil
// restores the built-in defaults (see defaultChatOpCategories): the criterion never
// runs against "no categories", because "nothing is a repository write" is a
// WIDENING the operator did not declare.
func SetChatOpCategories(tab *chatpolicy.CategoryTable) {
	if tab == nil {
		tab = defaultChatOpCategories()
	}
	chatOpCategoryFrozen.mu.Lock()
	chatOpCategoryFrozen.tab = tab
	chatOpCategoryFrozen.mu.Unlock()
}

// ChatOpCategoryFrozen returns the frozen category table (read-only).
func ChatOpCategoryFrozen() *chatpolicy.CategoryTable {
	chatOpCategoryFrozen.mu.RLock()
	defer chatOpCategoryFrozen.mu.RUnlock()
	return chatOpCategoryFrozen.tab
}

// ResetChatOpCategoriesToDefaults restores the shipped category table. Like
// ResetChatPolicyToDefaults it exists for the tests that must prove the DEFAULT
// behaviour, not as a runtime knob.
func ResetChatOpCategoriesToDefaults() { SetChatOpCategories(nil) }

// ChatPolicyFrozen returns the frozen pair (read-only; the values are immutable
// once published).
func ChatPolicyFrozen() (*chatpolicy.Policy, chatpolicy.Posture) {
	chatPolicyFrozen.mu.RLock()
	defer chatPolicyFrozen.mu.RUnlock()
	return chatPolicyFrozen.pol, chatPolicyFrozen.post
}

// ResetChatPolicyToDefaults restores the shipped pair. It exists for the tests
// that must prove the DEFAULT behaviour (on-request) and for a process that never
// resolved any channel — not as a runtime knob.
func ResetChatPolicyToDefaults() {
	SetChatPolicy(defaultChatPolicy(), chatpolicy.NewPosture(chatpolicy.DecisionPrompt, chatPolicyDefaultSource))
}

// --- the decision -----------------------------------------------------------

// chatBashPolicyMatch decides a WHOLE command line: tokenize (the ONE tokenizer),
// split into clauses, resolve each, and take the MOST RESTRICTIVE answer.
//
// THE COMPOSITION RULE is chatpolicy's own (forbidden > prompt > allow), applied
// here over the clauses the tokenizer produced — `git status; rm -rf build` must
// not be judged by the clause written first. The first clause carrying the winning
// decision names it, so the text the model reads points at the sharpest evidence.
func chatBashPolicyMatch(command string) chatpolicy.Match {
	clauses := chatShellClauses(chatShellTokens(command))
	if len(clauses) == 0 {
		// A blank command line carries no judgement: the tool itself refuses an
		// empty command, and inventing an answer here would make the boundary
		// claim an opinion about nothing.
		return chatpolicy.Match{}
	}
	pol, posture := ChatPolicyFrozen()
	best := chatpolicy.Match{}
	seen := false
	for _, clause := range clauses {
		m := posture.Resolve(pol, clause)
		if !seen || m.Decision > best.Decision {
			best = m
			seen = true
		}
	}
	return best
}
