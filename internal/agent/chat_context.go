package agent

// chat_context.go — the CHAT-SPECIFIC context block: what this surface is, and
// where this process is actually running.
//
// WHY IT EXISTS (the measured gap). Both chat paths were told nothing about
// themselves. The plain path (Agent.Chat, loop.go) sent three sentences of
// persona and a sentence FORBIDDING any mention of tools or internals; the
// --tools path (ChatOps, chat_ops.go) sent the full TASK assembly, which knows
// the sense verbs but believes it is a task runner. NEITHER was told which
// machine, which node role, or which repo it was in — so the one question a chat
// console cannot answer from its own transcript ("where am I, and what am I?")
// was answerable only by guessing. This block answers it.
//
// WHAT IT RENDERS, and WHY THROUGH PromptSection.Render. The three runtime
// values (hostname, node role, repo root) depend on the PROCESS, not on the
// source: a package-level Text would be evaluated once at init and freeze
// whatever the environment said then (the exact trap memory #4669 measured for
// the session budget). Render is therefore the section's form, and the
// assembly is the ONE place these values are read — the registry entry is a
// function, not a literal, and nothing mutates the prompt mid-session (the
// prompt cache's byte stability is the reason the task path assembles once per
// session; the chat path re-renders per turn exactly as the operator's
// reply-language directive does, and the string is byte-identical turn to turn
// as long as the process's identity is).
//
// RULE 1/13/14: every verb and flag the block spells comes from
// internal/cliverbs — the leaf constant package that exists because
// cmd/agent is `package main` and unimportable. Not one of the invocations
// below is a typed literal.
//
// Rule 3: no new tool. The mechanism is the existing bash tool invoking gopi's
// own CLI; on the plain (no-tools) path there is no bash at all, which is why
// the block says so in its own words instead of pretending the check is
// available (Rule 31's spirit: an instruction the agent cannot act on is worse
// than none).

import (
	"os"
	"os/exec"
	"strings"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
)

// chatContextSectionName is the registry Name of the chat-context block. It is
// a constant (Rule 1) because the tests address the section by name rather than
// by position, and because a rename must be a compile-time-visible edit on both
// sides.
const chatContextSectionName = "chatContext"

// renderPromptSection returns a section's text AT ASSEMBLY TIME: Render when it
// is set, otherwise the literal Text. It is one function rather than an inline
// branch because both prompt assemblies (the task registry in loop.go and the
// chat pair below) must render a section the same way — a second copy is how
// "Render takes precedence" would come to mean two things.
func renderPromptSection(s PromptSection) string {
	if s.Render != nil {
		return s.Render()
	}
	return s.Text
}

// chatContextHead opens the block and is the marker tests key on. It is
// separated from the body so "is this block present at all" is a one-substring
// question, independent of the runtime values that follow.
const chatContextHead = "[CHAT SURFACE — you are the console, here and now]"

// ChatSurfacePromptHead returns the opening of the chat-surface block — the block
// ONLY the chat assemblies append (assembleChatSystemPrompt, chatOpsSystemPrompt).
// It is exported so a caller OUTSIDE this package can assert "this request carried
// the chat turn" against the marker the prompt itself draws, instead of re-typing
// the wording (Rule 13/14: referenced, never mirrored — a re-worded head would
// otherwise leave the outside check passing against text nothing paints any more).
func ChatSurfacePromptHead() string { return chatContextHead }

// chatContextSep is the mid-block field separator (" · "), named so the
// rendering below stays a readable sentence with no bare literal in it.
const chatContextSep = " · "

// The [CURRENT SESSION] runtime block — the part of this file that names THIS
// run's own transcript FILE, as opposed to the surfaces in general.
//
// WHY IT EXISTS. The block above tells the console which verbs READ a record
// (`gopi session log`/raw/stats); none of them says WHICH FILE this run is writing. So
// the one question an operator asks when the console and the log disagree —
// "quote me the file that settles what you actually sent" — was answerable only
// by guessing, and a chat that guessed denied a file that existed. The path is a
// RUNTIME fact (it embeds this run's uuid, and rawLogDirEnv relocates the tree),
// so it is RESOLVED at assembly and never frozen into a const: a const is
// evaluated once at process init and would freeze whatever this machine had
// then. That is the same reason Render — not Text — is the carrier.
const (
	// chatSessionBlockMarker opens the runtime block. It is the string a test
	// (and a reader of a raw log) greps for when the question is "was this run
	// told where its own record is?".
	chatSessionBlockMarker = "\n\n[CURRENT SESSION —"

	// chatSessionRawFileFlag is the `gopi session raw` input selector that reads a raw
	// log FILE — the flag the block pairs with the path it hands the model. It is
	// an ALIAS of the leaf constant (Rule 13/14: one definition, in the package
	// every consumer can import), because the dispatch side (cmd/agent/cli_raw.go)
	// also spells it as an alias of the same value — internal/cliverbs' drift
	// guard proves the bare WORD the parser matches and this flag still agree.
	chatSessionRawFileFlag = cliverbs.FileFlag

	// chatEntityUnknown is the fail-safe when neither the session logger nor the
	// hostname can identify the run: the block still states which run it is
	// speaking about, in the block's own vocabulary, rather than dropping the
	// field.
	chatEntityUnknown = "this run (id unavailable)"
)

// The two ABSENCE sentences, named so the rendering and every assertion about it
// share ONE definition (Rule 13): a test that re-typed the prose could keep
// passing while the prompt said something else, and the whole point of these two
// statements is that they are present exactly when they are true.
const (
	// chatSessionNoRawLogSentence is arm 2's second half: a session record with
	// no wire log. It names the switch that explains the absence, so the honest
	// answer needs no inference.
	chatSessionNoRawLogSentence = "There is NO raw HTTP log for this run"

	// chatSessionNoTranscriptSentence is arm 3: nothing on disk at all, plus the
	// instruction that matters most in this arm — say so rather than name a file.
	chatSessionNoTranscriptSentence = "No transcript of this run is on disk"
)

// chatSessionBlock renders the [CURRENT SESSION] block: the RUNTIME path of the
// record this process is writing, and the one invocation that prints it.
//
// THE THREE ARMS are the three states that REALLY occur, and each says what IS
// (Rule 15: an absent transcript is REPORTED, never left for the model to
// guess — a model handed no path must not invent one):
//
//  1. both logs live → both named with their paths;
//  2. session JSONL only → the wire log is reported ABSENT, together with the
//     switch that explains it (rawLogEnableEnv=rawLogDisabledValue);
//  3. neither → "no transcript of this run is on disk", plus the instruction to
//     say so rather than name a file.
//
// Each half is reported ON ITS OWN, which is the whole point: gating one on the
// other made the commonest real state — the raw log alone — fall through to
// "there is no transcript", a FALSE statement about a file that exists.
//
// bot-only: the id it names is the entity a transcript can be addressed by
// (SessionLog.SessionID when there is a session logger; SessionLog.TaskID when
// the run is a task child — the same two ids internal/tools resolves a session
// path from). It is an identifier, never a credential, so it is publishable.
func chatSessionBlock(a *Agent) string {
	if a == nil {
		return ""
	}
	var rawPath, sessionPath string
	if a.RawLog != nil {
		rawPath = a.RawLog.Path()
	}
	if a.SessionLog != nil {
		sessionPath = a.SessionLog.Path()
	}

	var clauses []string
	if rawPath != "" {
		clauses = append(clauses, "raw HTTP log "+rawPath+
			" — every request and response byte exactly as it went on the wire, so it is what settles "+
			"\"what did I actually send\" (`gopi "+cliverbs.Raw+" "+chatSessionRawFileFlag+" "+rawPath+
			"` prints it verbatim)")
	}
	if sessionPath != "" {
		clauses = append(clauses, "session JSONL "+sessionPath+" (this run's turns and tool calls)")
	}

	// Arm 3 — nothing on disk. Say so; do not let the model name a file it
	// cannot know.
	if len(clauses) == 0 {
		return chatSessionBlockMarker + " " + chatSessionEntity(a) +
			"] No transcript of this run is on disk (no session and no raw log is attached to this " +
			"process) — say so rather than naming a file."
	}

	block := chatSessionBlockMarker + " " + chatSessionEntity(a) +
		"] Your own transcript, written while you answer: " + strings.Join(clauses, "; ") + "."

	// Arm 2 — a session record with no wire log. The ABSENT half is stated with
	// the switch that caused it, so the honest answer is available without the
	// model having to infer a cause.
	if rawPath == "" {
		block += " There is NO raw HTTP log for this run (" + rawLogEnableEnv + "=" + rawLogDisabledValue +
			"), so do not name one — say the wire log was not written."
	}
	return block
}

// chatSessionEntity names WHICH session the block is speaking about: the session
// id when there is a session logger, else the task id it belongs to, else the
// explicit chatEntityUnknown. A block that names a transcript and no session
// leaves the model able to quote a path but unable to say whose it is.
func chatSessionEntity(a *Agent) string {
	if a.SessionLog == nil {
		return chatEntityUnknown
	}
	if id := a.SessionLog.SessionID(); id != "" {
		return id
	}
	if id := a.SessionLog.TaskID(); id != "" {
		return "task " + id
	}
	return chatEntityUnknown
}

// chatUnknownHost is the fail-safe hostname (the same spelling
// internal/config's node banner uses for an unreadable host): a chat that
// cannot read its hostname still gets a prompt, and says "unknown" rather than
// silently omitting the field.
const chatUnknownHost = "unknown"

// chatNodeUndeclared is what an unset node role renders as. Empty is the
// ORDINARY state on a machine that never opted into the topology (bug041:
// topology is DECLARED, never discovered), so it is rendered as an explicit
// "undeclared" rather than dropped — a missing field reads as an omission, and
// the whole point of this block is that the field is never a guess.
const chatNodeUndeclared = "undeclared"

// chatRepoUnknown is what an unresolvable repo root renders as.
const chatRepoUnknown = "(not a git repo)"

// chatHostName reads this process's hostname, fail-safe. The error is
// deliberately discarded with a reason: the hostname is a DISPLAY label in this
// block, and an unreadable one degrades to chatUnknownHost — which the sentence
// still prints. It is the same contract (and the same review comment)
// internal/config's nodeName and internal/tools' claim holder use.
func chatHostName() string {
	h, err := os.Hostname() //stylegate:ignore host name is a display label; a failure degrades to chatUnknownHost and nothing downstream breaks
	if err != nil || strings.TrimSpace(h) == "" {
		return chatUnknownHost
	}
	return h
}

// chatRepoRoot is the enclosing repository's top directory, resolved ONCE per
// assembly — never a hard-coded path and never a guess. Resolution is
// `git rev-parse --show-toplevel` from the process cwd (the same question
// internal/agent/chatregister.go asks when it records a chat row's git
// context); outside a repo the answer is the honest chatRepoUnknown rather
// than a fabricated path. The command's failure is not an error to report —
// "this is not a git repo" IS the answer, and the block says it.
func chatRepoRoot() string {
	cwd, err := os.Getwd() //stylegate:ignore cwd is the resolution ORIGIN, not the answer; an unreadable cwd falls back to the git default below
	if err != nil || cwd == "" {
		cwd = "."
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return chatRepoUnknown
	}
	if root := strings.TrimSpace(string(out)); root != "" {
		return root
	}
	return chatRepoUnknown
}

// chatNodeIdentity renders this process's declared node identity as
// "<host> (<role>)" / "<host> (<role>→<sync_to>)", or the bare host when the
// topology is undeclared. It reads config.ResolveNodeRole — the ONE resolver
// (env > settings.json), memoised by that package — so the prompt and the
// startup banner cannot disagree about which node this is.
func chatNodeIdentity() string {
	role, syncTo := config.ResolveNodeRole()
	if role == "" {
		return chatNodeUndeclared
	}
	if syncTo != "" {
		return string(role) + "→" + syncTo
	}
	return string(role)
}

// chatNoToolsLimb and chatToolsLimb are the block's LAST sentence in its two
// forms, and the difference is not cosmetic: the same block is carried by the
// plain path (no tools on the wire at all) and by the tool-enabled path (the
// full toolset plus bash). Telling the tool-enabled path "you cannot run these"
// would be a FALSE instruction — the worst kind, because it is followed. The
// limb is therefore parameterised, and each path states only what is true of it.
const (
	chatNoToolsLimb = "This path carries no tools: you can NAME these surfaces but cannot run them — " +
		"say that plainly instead of guessing what the screen says or asking the operator to read it for you."
	chatToolsLimb = "With tools on this path, run each of these through bash yourself — never guess what the screen shows."
)

// chatContextRules renders the block in its PLAIN-path form (no tools). It is
// the section's Render, and the tool-enabled path renders its own form through
// chatContextRulesFor(true) — one block, two honest limbs.
func chatContextRules() string { return chatContextRulesFor(false) }

// chatContextRulesFor renders the chat-context block at ASSEMBLY time. It is
// the section's Render function (never its Text): the host, the node and the
// repo are properties of the running process, and the whole defect this block
// closes is that they were absent.
//
// The block states these things, in the order a reader needs them:
//  1. WHERE — host · node · repo, rendered, not typed.
//  2. WHAT — the long-lived interactive console, operator watching, the answer
//     body is the artifact (and NOT a task run: no epic framing). The `task`
//     CHECKLIST is a different thing from an epic — it is how this surface
//     reports which of its own steps are still open, and the harness READS it
//     (loop's trustedCompletion: TaskState.hasList && AllCompleted). WHAT THAT
//     READ DECIDES IS ONE TURN, NOT A PERIOD: the list is how this surface says
//     "this turn's work is finished", so a no-open-items list lets the round
//     exit early and a list with open items keeps it working. It does NOT carry
//     the console past a period boundary, and no mechanism does that from a
//     checklist — the period is opened by a line this surface did not write
//     (cmd/agent/cli_chat.go's selfDriven := false seam), because a budget the
//     model can raise by editing its own list is not a bound. The claim this
//     comment used to make named a self-drive seam READER that has never existed
//     anywhere in the tree — it is deleted rather than implemented, so the
//     comment now states what the code does. (The dead spelling is not repeated
//     here on purpose: this file's prose names only symbols that exist, which is
//     what `TestChatPromptNamesOnlySymbolsThePackageDeclares` enforces. The
//     defect and its measured evidence are recorded in that test's header and in
//     task 01a1e125.) The other
//     half — carrying the console past a period — is UNWIRED, not ruled out:
//     the period accounting and its wiring have their own owner (task
//     01a1e101), so a later reader who wants that capability must go there
//     rather than conclude this surface does not want it.
//  3. HOW TO SEE MYSELF HERE — arm, read, compare, and the authoritative
//     transcript, each spelled through cliverbs.
//  4. THE OPERATOR'S RULE — no pixel snapshot on this surface.
//  5. WHEN A CHECK IS WORTH THE TOKENS — plus the honest limb for the path at
//     hand (chatNoToolsLimb / chatToolsLimb). An instruction the agent cannot
//     act on is worse than none, so the no-tools form says the check is
//     unavailable instead of teaching an invocation with no execution.
//  6. WHEN TO REMEMBER (chatMemoryTimingRules, in chat_memory_timing.go) — the
//     operator's instruction is the trigger, and it fires at once; NOT a task
//     run's timing, because there is no task end here to defer to. It carries
//     the same two-limb parameterisation, since only the tool-enabled path can
//     actually call the memory tool.
//     6b. WHEN A VAGUE PHRASE IS A RETRIEVAL REQUEST (chatRecallRules, in
//     recall_triggers.go) — 「你还记得…」「你忘了…」「我们之前讨论过…」 and their
//     English twins are a SEARCH the console owes, not a question it answers from
//     the window; the query is the model's own translation of the topic, and the
//     paragraph carries the floor that keeps "search" from becoming the reflex
//     answer to any mention of the past. Same two-limb parameterisation.
//  7. WHEN THE WORK IS ALREADY DONE BY SOMEONE ELSE (industryPracticeRules, in
//     industry_practice.go) — search prior art before building: the industry's
//     practice first, then this repo, and reuse it. It rides here rather than in
//     a kernel task block because a chat path carries the tool routing rules
//     only when tools are on the wire, and this principle must reach the chat
//     reader either way — under its own two limbs, since the plain path cannot
//     search anything.
//  8. WHAT THIS SURFACE MAY WRITE, AND WHERE (chatWriteBoundaryRules, in
//     chat_write_boundary.go) — no git mutation, no artifact into a tracked
//     path, this run's own session directory for what the operator asks to keep,
//     a task for anything that must be committed. It is an ORDER rather than an
//     observation, and it carries the same two-limb parameterisation.
//  9. WHICH WORK IS THIS SURFACE'S OWN (chatPADutyRules, in chat_pa_duty.go) —
//     the console holds the operator's authorisation for EVERYTHING they would
//     otherwise do by hand (research, operation, a short job, and — since the
//     2026-10-24 ruling — a change to the REPOSITORY too, commit included); the
//     ONE named exclusion is coding/debugging as an OWNED deliverable, and the
//     one act still retired is a REF MOVE, because a ref decides what other work
//     is reachable. The irreversible calls belong to the operator. LAST, after
//     the write boundary, because the two are read together as one order — what
//     this surface may NOT do, then what it MUST do itself — and (9) names (8) as
//     the bound of its own scope. It carries the same two-limb parameterisation.
//  10. WHAT AN ANSWER BODY MAY BE MADE OF (chatReplyGlyphRules, in
//     chat_reply_glyphs.go) — the screen cell is copied out and used as typed,
//     so nothing in a reply may be elided with an ellipsis. LAST, after the
//     paragraphs about what this surface DOES: it is a rule about the characters
//     of the answer those paragraphs are instructions to produce, and it is one
//     sentence with one limb (no tool is involved in how text is written).
func chatContextRulesFor(toolsOn bool) string {
	var b strings.Builder
	b.WriteString(chatContextHead)
	b.WriteString(" Where: host ")
	b.WriteString(chatHostName())
	b.WriteString(chatContextSep)
	b.WriteString("node ")
	b.WriteString(chatNodeIdentity())
	b.WriteString(chatContextSep)
	b.WriteString("repo ")
	b.WriteString(chatRepoRoot())
	b.WriteString(". What: the long-lived interactive `gopi " + cliverbs.Chat +
		"` console — the operator is watching this screen and the ANSWER BODY is the artifact: a turn is conversation, not a task run (no epic framing, no roadmap ceremony — but `task`'s own CHECKLIST is this surface's tool and the operator WANTS it used: a multi-step job gets task(action=view) plus a [TASK_LIST_BEGIN] block, and the list is what tells the harness whether THIS turn's work is finished — it does not extend the turn budget, which is opened only by a line you did not write). ")
	b.WriteString("Seeing THIS surface: `gopi " + cliverbs.Chat + " " + cliverbs.DumpScreen +
		"` arms the run so its own dump is written (an UNARMED chat writes none — `gopi " + cliverbs.UIState +
		"` then exits 1); `gopi " + cliverbs.UIState + " " + cliverbs.TargetFlag + " " + cliverbs.Chat +
		"` reads that dump back as a CELL GRID carrying attrs per cell-range, no pixels and no vision; `gopi " + cliverbs.UIState +
		" " + cliverbs.Compare + " \"<line>\" " + cliverbs.SessionFlag + " <id> " + cliverbs.TargetFlag + " " + cliverbs.Chat +
		"` names which side lost a line (exit 0 = on the grid AND in the transcript, 3 = they disagree, 1 = the check could not run — never \"they agree\"); `gopi " +
		cliverbs.Log + "`/" + cliverbs.Raw + "/" + cliverbs.Stats + " = the authoritative transcript. ")
	b.WriteString("Operator's rule for this surface: NO pixel snapshot (" + cliverbs.ScreenCapture +
		"/" + cliverbs.Capture + "+vision cost tokens the cell grid answers for free). ")
	b.WriteString("Spend a check when the presence, width or wrap of a line IS the question; otherwise just answer. ")
	if toolsOn {
		b.WriteString(chatToolsLimb)
	} else {
		b.WriteString(chatNoToolsLimb)
	}
	b.WriteString(chatMemoryTimingRules(toolsOn))
	b.WriteString(chatRecallRules(toolsOn))
	b.WriteString(industryPracticeRules(toolsOn))
	b.WriteString(chatWriteBoundaryRules(toolsOn))
	b.WriteString(chatPADutyRules(toolsOn))
	b.WriteString(chatTurnClockRules())
	b.WriteString(chatReplyGlyphRules())
	return b.String()
}

// chatContextSection is the chat-context block as a PromptSection — the SAME
// type the task registry uses, so the two prompt paths cannot grow two shapes
// for "a block of the system prompt". Render is set and Text is empty: a Text
// here would freeze the process's identity at init.
var chatContextSection = PromptSection{
	Name:   chatContextSectionName,
	Order:  chatContextSectionOrder,
	Render: chatContextRules,
}

// chatContextSectionOrder places the chat block after the persona body and
// before anything else a chat prompt may later carry.
const chatContextSectionOrder = 10

// assembleChatSystemPrompt renders the CHAT system prompt: the persona body,
// then the chat-context block, joined with the registry's own separator and
// passed through the operator's reply-language directive exactly as the task
// path is. It is the ONE composition point for the plain chat path, so a test
// asserting "the chat prompt carries the block" asserts it against the same
// function the request is built from (not against a copy of the text).
//
// The block is read through renderPromptSection — the SAME render step
// assembleSystemPrompt applies to a task section — so "a section's text is
// Render()-or-Text" has exactly one definition, and dropping the block (an
// empty section) drops it from the prompt rather than leaving a stray separator.
//
// The AGENT is a parameter because half the block is per-RUN state: the path of
// this process's own transcript (chatSessionBlock) lives on the Agent, not in
// module scope, and a package-level spelling could only render the static half.
// It is appended through the same join, so the emptied-section contract above
// still holds (an empty section takes the runtime half with it — the block is
// one block).
func assembleChatSystemPrompt(a *Agent) string {
	block := renderPromptSection(chatContextSection)
	// The parts are collected rather than concatenated in place, because the
	// unattended block (chat_unattended.go) is OPTIONAL and the emptied-section
	// contract above still has to hold: a run that carries neither block sends
	// the persona alone, with no trailing separator left behind by a block it
	// did not have. Order is persona → chat surface → unattended: the surface is
	// where the run is, the unattended sentence is what it may do there.
	parts := []string{chatSystemPrompt}
	if block != "" {
		parts = append(parts, block+chatSessionBlock(a)+chatSurfaceFactRules(a)+chatSurfaceBudgetLine(a))
	}
	if extra := chatUnattendedBlockOpen(a); extra != "" {
		parts = append(parts, extra)
	}
	if len(parts) == 1 {
		return maybeAppendChatSelfRecordRule(systemPromptWithOutputLanguage(chatSystemPrompt))
	}
	return maybeAppendChatSelfRecordRule(systemPromptWithOutputLanguage(strings.Join(parts, promptSectionSeparator)))
}
