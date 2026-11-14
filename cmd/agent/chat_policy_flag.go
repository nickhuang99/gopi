package main

// chat_policy_flag.go — `gopi chat --policy VALUE`: the CLI channel of the chat
// posture, and the ONE startup line that reports what the run resolved.
//
// IT FREEZES THE JUDGEMENT. The rule table and the posture are resolved here — once,
// from the channels in precedence order — and then handed to
// tools.SetChatPolicy, which is the ONE holder the chat write boundary reads
// (internal/tools/chat_policy_gate.go). Nothing below re-reads a channel: the
// running session judges by this frozen pair, so an operator who set `--policy`
// can reason about the boundary for the rest of the run. The startup line still
// prints what was resolved, because "the boundary asked me" and "the boundary was
// told to ask" are one behaviour and two different facts.
//
// WHY THE FLAG IS IN THE RECORDED/VALUED LISTS. `--policy` takes a VALUE, so it
// must be in chatResumeValuedFlags (or a walk hands its value back as a
// positional) and it is READ, so it must be in chatREPLFlags (or the closed
// vocabulary refuses the flag the page advertises). It is NOT in
// chatResumeOptions: the posture is a property of THIS invocation's start, like
// the geometry, not of the session — a resumed run must not inherit a posture
// its operator no longer wants.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// chatPolicyFlag is the CLI spelling, taken from the resolver's own constant so
// the parser, the page and the env/settings ladder cannot spell it twice.
const chatPolicyFlag = agent.ChatPolicyFlag

// chatParsePolicyArg parses `--policy VALUE` and `--policy=VALUE`.
// consumed=false means the word is not this flag at all.
//
// A missing or EMPTY value is a usage error naming the accepted words: `gopi
// chat --policy` alone names no posture, and swallowing it would leave the run
// on the default while the operator believes they chose one (Rule 15).
func chatParsePolicyArg(a, next string, hasNext bool) (value string, consumed bool, err error) {
	if a != chatPolicyFlag && !strings.HasPrefix(a, chatPolicyFlag+chatFlagValueSep) {
		return "", false, nil
	}
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		inline := a[k+len(chatFlagValueSep):]
		if inline == "" {
			return "", true, fmt.Errorf("%s was given an empty value — write %s %s",
				chatPolicyFlag, chatPolicyFlag, strings.Join(chatpolicy.PostureWords(), "|"))
		}
		return inline, true, nil
	}
	if !hasNext || strings.HasPrefix(next, chatFlagPrefix) {
		return "", true, fmt.Errorf("%s needs a value — write %s %s",
			chatPolicyFlag, chatPolicyFlag, strings.Join(chatpolicy.PostureWords(), "|"))
	}
	return next, true, nil
}

// chatPolicyStartupLine is the ONE rendering of the resolved policy state: the
// number of rules the project's GOPI.md contributed, where they came from, and
// the posture that answers everything else. One function, so the startup log,
// the tests and the report quote the same bytes (Rule 2).
//
// "0 rules" and "no rules file" are DIFFERENT facts and print differently: the
// first means the file exists and declares no rules (or has no fence), the
// second means there is no GOPI.md to read at all.
func chatPolicyStartupLine(rules *chatpolicy.Policy, rulesPath string, resolved agent.ChatPolicyPosture) string {
	rulesPart := "0 rules, no project GOPI.md found"
	switch {
	case rulesPath != "" && rules != nil:
		rulesPart = fmt.Sprintf("%d rules from %s", rules.Len(), rulesPath)
		if !rules.Fenced {
			rulesPart += " (no ```" + "chat-policy fence — the posture answers every command)"
		}
	case rulesPath != "" && rules == nil:
		rulesPart = fmt.Sprintf("0 rules, %s could not be read", rulesPath)
	}
	return fmt.Sprintf("[gopi] chat: policy posture %q (from %s) — %s; the rule table and the posture are FROZEN into the write boundary for this run",
		resolved.Posture.String(), resolved.Posture.Source(), rulesPart)
}

// The face prefixes of the ONE freeze below. Named (Rule 1b/13) because the
// sentence they open is printed by whichever verb started the run, and a bare
// literal beside each call site is how two faces drift into naming one surface.
const (
	chatFacePrefixWeb = "gopi web: "
	chatFacePrefixCli = "gopi cli: "
	chatFacePrefixUI  = "gopi ui: "
)

// chatFreezeChatPolicyState is the RESOLUTION+LOAD HALF of the chat surface's
// startup: it turns the channels into the frozen pair (posture + rule table) and
// the frozen category table, printing every NOTE and every parse error, and
// returns what the caller needs to report.
//
// IT IS EXTRACTED, not duplicated, because a second face reached the same
// boundary without it (measured 2026-10-22, task 01a16225): the faces that
// EXECUTE TOOLS but own no `--policy` flag — `gopi web`, `gopi cli`, `gopi ui` —
// built their agent and served turns without ever calling
// tools.SetChatPolicy, so the write boundary judged every write/edit against the
// PACKAGE DEFAULT ITSELF (the built-in `prompt *` table with the on-request
// posture) instead of against the operator's ```chat-policy fence and the
// declared posture. The observable was a tool round whose genuine `write` came
// back `[NEEDS OPERATOR APPROVAL — command NOT run]` and never landed, on a
// surface whose operator had declared `chat_op_auto: true`.
//
// So the resolution lives in ONE function (Rule 2) and every face that can run a
// tool calls it before serving its first turn.
//
// facePrefix is the verb the notes are addressed to ("gopi chat: ", "gopi web: ",
// …): the sentence names the surface whose boundary it froze, so an operator
// reading a web log is not told a `gopi chat` run said it.
func chatFreezeChatPolicyState(facePrefix, policyFlagValue string) (agent.ChatPolicyPosture, *chatpolicy.Policy, string) {
	resolved := agent.ResolveChatPolicyPosture(policyFlagValue, os.Getenv, "")
	for _, note := range resolved.Notes {
		fmt.Fprintf(os.Stderr, "%s%s\n", facePrefix, note)
	}
	// THE TABLE IS LOADED ON EVERY RUN, not only under --verbose: it is the
	// judgement, and a boundary that ran on the default table while the operator's
	// fence sat unread would be a boundary nobody declared. A parse error is
	// REPORTED and the default table is frozen instead — the run continues, the
	// operator sees the line number, and the boundary falls back to the shipped
	// "ask" table rather than to a half-read one.
	rules, path, err := agent.LoadChatPolicyTable("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s%v\n", facePrefix, err)
		rules = nil
	}
	tools.SetChatPolicy(rules, resolved.Posture)
	// THE ACTION-CLASS TABLE, resolved through the SAME priority order CONFIG.md
	// carries: the TRACKED project file (`.gopi/chat-policy.md`, which travels
	// with the clone) first, then the client's ```chat-op-category fence in
	// GOPI.md, then the built-in table.
	//
	// THE PROJECT FILE REPLACES, IT NEVER MERGES. A merge would let a word gopi
	// happens to ship reach into a project that never declared it, so a user's own
	// `cmd/` would be refused as a repository write by a rule about somebody
	// else's repository — the defect this whole move exists to end, arriving
	// through the back door.
	//
	// A parse error is REPORTED (chatpolicy's error renders `file:line: what is
	// wrong`) and the next channel answers: a malformed table must never leave the
	// criterion with NO vocabulary, because "nothing is a repository write" is a
	// widening nobody declared. The same rule answers a file that exists but
	// carries no fence — it declared nothing, and reading that as "nothing
	// applies" would be the widening again.
	categories, catPath, catErr := agent.LoadProjectCategoryTable("")
	switch {
	case catErr != nil:
		fmt.Fprintf(os.Stderr, "%s%v\n", facePrefix, catErr)
		categories = nil
	case categories != nil && !categories.Fenced:
		fmt.Fprintf(os.Stderr, "%s%s carries no ```%s fence — the next channel answers\n",
			facePrefix, catPath, chatpolicy.CategoryFenceInfoWord())
		categories = nil
	}
	if categories == nil {
		if fromGOPI, _, gopiErr := agent.LoadChatOpCategoryTable(""); gopiErr != nil {
			fmt.Fprintf(os.Stderr, "%s%v\n", facePrefix, gopiErr)
		} else if fromGOPI != nil && fromGOPI.Fenced && fromGOPI.Len() > 0 {
			categories = fromGOPI
		}
	}
	if categories != nil {
		tools.SetChatOpCategories(categories)
	} else {
		tools.SetChatOpCategories(nil)
	}
	return resolved, rules, path
}

// chatAnnounceChatPolicy is the `gopi chat` startup call: freeze the pair, then
// print the ONE line under --verbose (or whenever the operator NAMED a posture —
// someone who typed `--policy yolo` must be able to see what it resolved to
// without knowing a second flag).
//
// st is the run's system voice, resolved ONCE by runChat and handed down
// (termToolStylesFor(chatCap.Colour())). The line this call prints is the
// surface reporting its OWN posture, so it wears that voice as a termRow; the
// NOTES and PARSE ERRORS the resolution emits (inside chatFreezeChatPolicyState,
// shared with `gopi web`/`gopi cli`/`gopi ui`, which resolve no chat capability)
// stay plain diagnostics — a warning is not the surface talking about itself,
// and dimming it would be the emphasis the operator's ruling forbids on a
// failure. No capability resolution happens here or below: that would be the
// second decision this whole change exists to avoid.
func chatAnnounceChatPolicy(policyFlagValue string, verbose bool, st termToolStyles) agent.ChatPolicyPosture {
	resolved, rules, path := chatFreezeChatPolicyState(chatErrorPrefix, policyFlagValue)
	if verbose || policyFlagValue != "" {
		termWriteRows(os.Stderr, termNoteRows(chatPolicyStartupLine(rules, path, resolved), st))
	}
	return resolved
}

// chatPolicyWordsHelp renders the accepted spellings for the page, beside the
// default and the channel that supplies it.
//
// IT WRAPS. The page is a fixed-width block (config.HelpWrapCol, Rule 14), and
// the posture vocabulary is DATA that grows — nine words already run to 92
// columns on one indented line, past the margin the rest of the block respects
// and into a ragged right edge that reads as an error. Wrapping here, once,
// means every future word (and a long one) inherits the margin instead of
// re-breaking the page: the width is measured with config.DisplayWidth, the same
// measure the help guard uses, so the two cannot disagree about what "too wide"
// means.
func chatPolicyWordsHelp() string {
	return wrapHelpWords(chatpolicy.PostureWords(), chatWordsSep)
}

// chatWordsSep joins the posture spellings on one line — the separator a reader
// reads as "or", spelled once (Rule 1).
const chatWordsSep = "|"

// wrapHelpWords joins words with sep and breaks the joined text so no line runs
// past config.HelpWrapCol. The FIRST line continues the caller's row (so it
// carries no indent of its own); every continuation line starts at
// chatHelpContIndent, the column this page's prose bodies line up under.
func wrapHelpWords(words []string, sep string) string {
	var b strings.Builder
	line := ""
	first := true
	for _, w := range words {
		if line == "" {
			if first {
				line = w
			} else {
				line = chatHelpContIndent + w
			}
			continue
		}
		cand := line + sep + w
		// The caller's row already left the cursor at chatHelpContIndent, so the
		// FIRST line spends that much of the margin too — measuring it without
		// the indent would let the first line run exactly that far past the
		// margin the rest of the page respects.
		if config.DisplayWidth(cand)+config.DisplayWidth(chatHelpContIndent) > config.HelpWrapCol {
			b.WriteString(line + sep + "\n")
			line = chatHelpContIndent + w
			first = false
			continue
		}
		line = cand
	}
	b.WriteString(line)
	return b.String()
}

// chatHelpContIndent is the column every continuation line of this page's prose
// starts at: the flag rows are this wide, so a body lines up under them (Rule 1
// — the width is named once, and the wrap above measures against it).
const chatHelpContIndent = "                   "

// chatPolicyDefaultNote names the shipped default for the page.
func chatPolicyDefaultNote() string { return chatpolicy.PostureDefaultWord }
