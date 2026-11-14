package agent

// chat_write_boundary.go — the CHAT-SURFACE WRITE BOUNDARY as a PROMPT: what a
// conversation may write, where, and why it is a rule rather than a preference.
//
// THE MEASURED DEFECT (operator ruling 2026-09-28, task 01a0e454). A chat
// session owns NO branch and NO worktree: it is started in whatever checkout the
// operator happens to be standing in (usually the MAIN checkout, master working
// area), so everything it writes lands there UNCOMMITTED — unattributed (no
// branch, no rollback point, no ledger row) and invisible to every merge/cleanup
// path, while `git add -A` / `stash` / a branch switch by anyone else can carry
// it away or destroy it. The symmetric case is worse: a chat that COMMITS
// REWRITES the tree the operator is working in, and that commit has no owner the
// ledger can name.
//
// WHY THE FIX IS A CONSTRAINT, NOT A WORKTREE. The obvious alternative — give
// chat its own worktree — was considered and rejected by the operator: worktrees
// exist so execution is DETERMINISTIC (the worker creates and owns one, and the
// merger reclaims it), while a chat's writes are model-driven, i.e.
// probabilistic; handing a probabilistic writer the worktree-lifecycle machinery
// reintroduces exactly the coupling the worker/worktree split removes, and no
// merge path owns a branch named by a conversation. The constraint is the
// cheaper and the honest half: a chat's artifacts go to an area that is provably
// OFF every git work tree — its OWN session directory, the one `[CURRENT SESSION]`
// names, which the harness already owns and prunes — and work that must be
// committed is routed to a task, where the worker's deterministic worktree
// already exists.
//
// WHAT THIS FILE RENDERS, AND WHERE IT LANDS. The text is emitted from
// chatContextRulesFor(toolsOn) (chat_context.go) — the CHAT-SPECIFIC block
// assembled by assembleChatSystemPrompt (plain path) and chatOpsSystemPrompt
// (--tools path). It is NOT in promptSections: every background task run reads
// that registry, and telling a task run "do not commit" would be the opposite of
// its contract — a worker's deliverable IS commits in its own worktree.
//
// THE TWO LIMBS ARE THE HONESTY REQUIREMENT (Rule 15; the precedent is
// chatNoToolsLimb/chatToolsLimb in chat_context.go). The plain path has NO tools
// at all: it cannot write a file, cannot run git and cannot file a task, so
// teaching it the tools-branch behaviour would be an instruction it cannot act on
// — the failure mode this block's own header names as the worst kind, "because it
// is followed". The plain limb therefore states the boundary and hands the
// operator the exact command instead; the tools limb states it AND the mechanism.
//
// RULE 1/13/14 — ONE DEFINITION, TWO READERS. There is no compiled verb list to
// read any more: the boundary is the operator's ```chat-policy fence plus this
// run's frozen posture, and the paragraph renders BOTH from the values the
// mechanism itself holds (tools.ChatPolicyFrozen, read at prompt-assembly time),
// so the sentence the model reads and the judgement the tool dispatch applies
// cannot drift apart. The `gopi task submit` route and the tool names
// come from their leaf packages (cliverbs, taskverbs, toolnames) — this file
// types none of those words, because internal/agent cannot import cmd/agent.
//
// RULE 3/35: no new tool, no new script, no new resident process. The mechanism
// half rides the EXISTING tool dispatch (the write/edit/bash arms of
// executeToolDefault).

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/devdoc"
	"github.com/nickhuang99/gopi/internal/taskverbs"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// chatAllowedWriteRoot is the tree this prompt TEACHES a chat to write into — the
// document corpus, taken from its own definition (internal/devdoc.Root) so no
// site types the name (Rule 1/13).
//
// IT IS NO LONGER A PERMISSION LIST, and the prompt says so: since the write arm
// became RULE-DRIVEN (tools.ChatWritePathRefusal consumes the frozen
// chatpolicy table — the project's ```chat-policy fence — exactly as the bash arm
// does), whether a path may be written is the operator's sentence, not a value in
// this binary. What remains here is the DEFAULT DESTINATION the prompt recommends
// and the tree the sample fence in GOPI.md marks `allow write devdoc *`; a
// project that writes other rules gets exactly what it wrote.
func chatAllowedWriteRoot() string { return devdoc.Root }

// chatAllowedWritePath is the rendered spelling of that tree, as a path a model
// can copy into a tool call (`devdoc/<file>.md`).
func chatAllowedWritePath() string { return chatAllowedWriteRoot() + "/<name>.md" }

// chatWriteBoundaryMarker opens the boundary paragraph. It is the string the
// tests (and a reader of a raw log) key on when the question is "was this RUN
// told what it may write?" — separated from the body so PRESENCE is a
// one-substring question independent of the rules that follow.
const chatWriteBoundaryMarker = "[CHAT WRITE BOUNDARY — a conversation, not a task run]"

// chatGitCommandWord is the git command word itself, named (Rule 1) because both
// the rendered sentence and the reader in internal/tools spell it.
const chatGitCommandWord = "git"

// chatShellCommandWord is the shell the prompt's example wrapper uses (the
// payload-reading rule the guard implements). Named here so the sentence and the
// guard's own vocabulary agree by construction.
const chatShellCommandWord = "sh"

// chatGitStageVerbWord / chatGitCommitVerbWord are the two git verbs the ALLOWANCE
// in rule (2) spells out. They are PROSE, not a judgement: no code in this tree
// compares a command word against them (the bash boundary decides entirely from
// the operator's rule table — internal/tools/chat_policy_gate.go), and they are
// named here because a prompt can only teach a shape it can spell. The real
// enforcement of that shape is the write/edit path arm, not a verb test.
const (
	chatGitStageVerbWord  = "add"
	chatGitCommitVerbWord = "commit"
)

// chatPolicyBoundaryRendered states, for the model, WHICH posture this run
// resolved — read from the frozen pair the mechanism enforces
// (tools.ChatPolicyFrozen), so the sentence the model reads and the judgement it
// receives cannot disagree about the posture. The rule COUNT is reported rather
// than the rules themselves: the table is the operator's file and may be long,
// and a model does not need it quoted to know that asking is the move.
func chatPolicyBoundaryRendered() string {
	pol, posture := tools.ChatPolicyFrozen()
	return "posture `" + posture.String() + "` from " + posture.Source() + ", " + fmt.Sprint(pol.Len()) +
		" rule(s) in the fence; yolo means everything runs, on-request means you ask"
}

// chatPromptAnswerRoute is the part of the boundary's `prompt` sentence that
// depends on WHO IS ON THE OTHER END of the run.
//
// WHY IT IS A BRANCH AND NOT ONE SENTENCE (measured 2026-10-22): the boundary told
// every run the same thing — "you STOP and ask the operator in the conversation …
// retry only after they say yes" — while the SAME system prompt carries
// chatUnattendedFailureShape for a run with no reader ("ending by ASKING is the
// failure shape"). A run cannot obey both, and the interactive wording also
// disagreed with the tool result it actually receives (internal/tools'
// chat_operator_presence.go returns a non-asking text on exactly this branch).
// The prompt and the guard therefore read ONE fact — tools.ChatOperatorAbsent() —
// so a reader who is told "ask" is a run that can be answered, and a reader who is
// told "deliver" is a run that cannot.
func chatPromptAnswerRoute() string {
	if tools.ChatOperatorAbsent() {
		return "it is NOT run and NOT refused — and NO OPERATOR IS READING THIS RUN, so do not ask: deliver " +
			"what the rules allow (a read, or THIS run's own session directory), say in one clause which target " +
			"was refused and which deliverable is missing because of it, and if the deliverable genuinely needs " +
			"that target, name the authorisation the operator would have to give BEFORE a run starts (" +
			cliverbs.PolicyFlag + "=" + chatpolicy.PostureYoloWord + ", or an `allow` rule in the fence)."
	}
	return "you STOP and ask the operator in the conversation, quote the exact command, and retry only after " +
		"they say yes."
}

// chatTaskSubmitRoute is the ONE spelling of the routing sentence's command —
// `gopi task submit` — composed from the leaf constants, paired with the tool
// name the model actually calls when it files one.
func chatTaskSubmitRoute() string {
	return cliverbs.Task + " " + taskverbs.Submit
}

// chatWriteBoundaryOpsCategoryRule renders rule (6): the operator's ruling that
// replaced BOTH the verb-name criterion and the stopwatch that followed it — a
// console judges an operation by WHAT IT IS, not by how long it takes. The three
// classes, and the words inside them, are READ from internal/tools (the class
// predicate tools.ChatOpClassify and its two vocabularies), never typed here, so a
// prompt that taught one class while the guard applied another is not expressible
// (Rule 13/14 — the same defect class as a tool description no consumer reads).
func chatWriteBoundaryOpsCategoryRule() string {
	return "(6) AN OPERATION IS JUDGED BY ITS CATEGORY, NOT BY ITS TIME. Three classes, and a reader places a " +
		"command into one by asking what the ACTION IS: " +
		"(6a) " + tools.ChatOpOperatorWorkSentence() +
		"(6b) " + tools.ChatOpRepoWriteRefusalSentence() +
		"(6c) " + tools.ChatOpLongRefusalSentence(tools.ChatOpChannel()) +
		"You no longer declare a duration to get past this: `" + tools.ChatBashTimeoutArg + "` is the bash tool's " +
		"EXECUTION CAP (it kills the process group at that number), and it says nothing about the category of the " +
		"action — a fast call into the source tree is still a repository write, and a slow one that only READS the " +
		"operator's machine is still their own work. "
}

// chatWriteBoundaryRules renders the write-boundary paragraph for one limb.
// The SHARED half states the boundary — five rules, in the order the model needs
// them: what it may not run, WHICH TREE IT MAY (the allowance, and the exact
// shape of a legal staging+commit), what it may not write into, where an
// artifact that is not a document goes, and what a code change is. The TAIL is
// the limb: what THIS path can actually do about it.
func chatWriteBoundaryRules(toolsOn bool) string {
	var b strings.Builder
	b.WriteString(chatWriteBoundaryMarker)
	b.WriteString(" This console owns no branch and no worktree — it stands in whatever checkout it was started in, " +
		"shared with the operator and with other runs. Therefore: ")
	b.WriteString("(1) a git MUTATION is not yours to decide — it is governed by the operator's own rule table " +
		"(the ```chat-policy fence in this project's GOPI.md) and, for anything the table does not mention, by " +
		"this run's startup posture (" + chatPolicyBoundaryRendered() + "). Three answers are possible and they " +
		"are DIFFERENT: `allow` means run it; `forbidden` means it is wrong to run from a console at all and there " +
		"is nothing to ask; `prompt` means it is NOT run and NOT refused — " + chatPromptAnswerRoute() + " " +
		"And read this boundary for what it is: it governs a DEPOSIT and a REF, so the way to obtain information " +
		"is the read tools (`" + toolnames.ToolRead + "`, `" + toolnames.ToolCodeSearch + "`, `" +
		toolnames.ToolKeywordSearch + "`, `" + toolnames.ToolMemory + "`), which no fence and no posture gates at " +
		"all. A SHELL command is the fence's question even when it only reads — a refusal there names the clause " +
		"and the cause, and it says a fence line is missing, never that you may not look. ")
	b.WriteString("(2) THE ALLOWANCE — you MAY write, stage and commit ONE tree, `" + chatAllowedWriteRoot() + "/`, " +
		"the operator's office area, and nothing else. The exact shape, and it is narrow on purpose: " +
		"`" + toolnames.ToolWrite + "` the document to `" + chatAllowedWritePath() + "`, then `" + chatGitCommandWord +
		" " + chatGitStageVerbWord + " " + chatAllowedWritePath() + "` naming THAT FILE (no option, no `-A`, no `.`, " +
		"no directory), then `" + chatGitCommandWord + " " + chatGitCommitVerbWord + " " + tools.ChatGitMessageFlag +
		" \"<message>\"`. Staging is per-file, so this cannot carry another run's work; a branch, a ref move or a " +
		"push from here is still the operator's decision (see (1)). ")
	b.WriteString("(3) write NO artifact into any OTHER git-tracked path (any file `" + chatGitCommandWord +
		" ls-files` lists outside `" + chatAllowedWriteRoot() + "/`): a note, an evidence file or a source edit is " +
		"not a chat's to author in the repo, and it lands there untracked, unattributed and unstoppable by any " +
		"merge path. ")
	b.WriteString("(4) any OTHER artifact the operator asks you to KEEP goes to THIS run's OWN session directory — " +
		"the directory holding the transcript `[CURRENT SESSION]` names — e.g. `<that directory>/" +
		tools.ChatSessionArtifactsDirName + "/<name>.md` (that tree is outside every git work tree, named by this " +
		"run, and swept with the session): hand the operator that path. ")
	b.WriteString("(5) a CODE change is a task and not an edit: file it with `gopi " + chatTaskSubmitRoute() +
		" \"<the work>\"` (`" + toolnames.ToolTaskSubmit + "`) and report the task id — the worker gets a " +
		"deterministic worktree of its own, which is the whole reason the ledger exists. It is also ALWAYS a task " +
		"when the work is a DEBUG fix: editing a source file to make a symptom go away is the same act whether it " +
		"takes ten seconds or ten minutes. ")
	b.WriteString(chatWriteBoundaryOpsCategoryRule())
	b.WriteString("(7) a finished run's ground is COLLECTED through the repository's own cleanup faces — " +
		strings.Join(tools.ChatCleanupFaceInvocations(), ", ") + " — never by removing a directory yourself: a BARE run is a PLAN " +
		"(it reports and removes nothing), and the `" + tools.ChatTaskApplyFlag + "` half — which removes the " +
		"worktree and deletes its branch — runs only when the OPERATOR asked for cleanup in THIS turn's own " +
		"message. So: run the plan, show it, and let the operator's next message open the gate. ")
	if toolsOn {
		b.WriteString(chatWriteBoundaryToolsLimb)
		return b.String()
	}
	b.WriteString(chatWriteBoundaryNoToolsLimb)
	return b.String()
}

// The two tails. The difference is not cosmetic: the same paragraph is carried by
// the plain path (no tools on the wire) and by the tool-enabled path (write, edit
// and bash are on the wire), and telling the plain path to "write the file" would
// teach an invocation it cannot run.
const (
	chatWriteBoundaryToolsLimb = "This path HAS " + toolnames.ToolWrite + ", " + toolnames.ToolEdit + " and " +
		toolnames.ToolBash + ", so the boundary constrains HOW you use them, not what you can do: " +
		toolnames.ToolWrite + " the artifact to that session path (or a document to the allowance in (2)) and commit " +
		"it there if the operator asked you to, and reach `gopi " + cliverbs.Task + " " + taskverbs.Submit + "` / " +
		"`gopi " + cliverbs.Task + " " + taskverbs.Show + "` through " + toolnames.ToolBash + ". Calls that cross " +
		"the boundary are REFUSED with the reason and the alternative — read the refusal and retry inside the " +
		"boundary instead of rephrasing."

	chatWriteBoundaryNoToolsLimb = "This path carries NO tools: you cannot write that file, run git or file that " +
		"task yourself. Say what the artifact should contain, hand the operator the command to run or the text to " +
		"save, and NEVER claim a file was written, a task filed or a commit made."
)
