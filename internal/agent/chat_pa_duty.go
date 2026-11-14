package agent

// chat_pa_duty.go — the CHAT SURFACE'S OPERATING DUTY as a PROMPT: which work a
// console does itself, which work is a task, and which work only the operator
// may authorise.
//
// THE MEASURED DEFECT (operator ruling 2026-10-07, task
// 01a11343-0c04-7ce0-be86-2e2e831005c6). The console — the operator's PA — was
// routing its OWN housekeeping through the task queue: a check, a diagnosis, a
// `git tag`, a `git worktree remove` by a WORKER. The operator's words: "这一类
// 工作是简单的维护工作就是举手之劳，应该是 PA 的工作，不用启用一个昂贵的
// worker……pa 只是不允许实际编码 debug，其他运维的工作都是可以代劳人类用户的" —
// the routine maintenance is the PA's, a worker is expensive, and the one thing
// the PA may not do is the actual coding fix.
//
// WHY THE FIX IS A PROMPT LIMB. The duty is a routing decision, and a routing
// decision is made by the model at the moment it decides between doing a thing
// and filing a row. There is nothing to enforce at a dispatch point (Rule 3/35:
// no new tool, no new script, no resident process, no config knob — and
// deliberately NO escape-hatch flag): the console is ALREADY able to run every
// OP action through the tools it has, and the defect is that it did not know it
// was allowed — and required — to. So the paragraph states the duty, its
// boundary and its cost, on the surface that has the choice to make.
//
// WHY THE COST IS IN THE PARAGRAPH, not only in this comment. A rule the model
// cannot weigh is a sentence it will drop under pressure. The operator's reason
// is a measured price: promoting a hand's-breadth job to a task row buys one
// worktree, one model run and one merge cycle, AND HIDES the action from the
// operator, so a row that fails leaves the maintenance undone while the queue
// reads clean (measured: a run wrote an empty skeleton, the row read
// `completed`, and the task body never happened). Stating the price is what
// makes the boundary a decision rather than a slogan.
//
// THE THIRD MEASURED DEFECT — THE SCOPE WAS NARROWER THAN THE DUTY (operator
// ruling 2026-10-10, this task). The paragraph above named MAINTENANCE work as
// the console's, and the model read exactly that: research, an ordinary
// operation on the machine, and a short job the operator would otherwise have
// had to run by hand were still being treated as things to ask about or to file
// a row for. The operator's words: "user doesn't have to do all shell/scripting/
// devops … all research/operation except of coding/debugging should be duty of
// gopi chat … it should have full authority to run short job which includes
// everything except of tasks which involves programming/debugging."
//
// So clause (1) is stated as a SCOPE with a NAME for the one exclusion, rather
// than as a list of chores: the console holds the operator's authorisation for
// everything the operator would otherwise do by hand, the exclusion is CODING /
// DEBUGGING alone AS AN OWNED DELIVERABLE, and (2b) states the ONE act that
// survives as off-limits — a DESTRUCTIVE REF MOVE — so "full authority" cannot be
// read as "rewrite a branch now". (A `git push` was in this family until the
// operator's 2026-11-03 ruling made every push spelling a repository write;
// "rewrite a branch now" is exactly what it is NOT about any more — publishing a
// commit is the ordinary end of the commit path.) The asymmetry the paragraph teaches is deliberate and
// is the reason the scope can be stated so widely: READING the operator's machine
// is recoverable, and WRITING a file is recoverable too (git holds the history);
// deciding WHICH REFS EXIST is not, because that decides what other work is
// reachable at all. It widens NOTHING in the write boundary — rule (2b) is the
// boundary's own rule named as the bound of the scope, and the boundary's
// enforcement at the dispatch point is untouched.
//
// THE REPOSITORY HALF OF THAT ASYMMETRY WAS REVERSED (operator ruling
// 2026-10-24), AND THIS FILE NOW TEACHES THE REVERSAL. The paragraph used to say
// "only a change to the REPOSITORY is a task" and "(2b) the one act … is the
// commit", which was the PRE-2026-10-24 rule: a chat that cannot commit cannot
// finish anything, and on the night of the ruling the dispatched workers were the
// thing breaking the machine, so "route it to a worker" routed the work to the
// unreliable party. The guard moved first — tools.ChatOpAdmit admits a
// REPOSITORY WRITE at every posture — and the prompt kept contradicting it, which
// is the defect this file's three clauses were rewritten to close: the model read
// "this is WRONG and is a task" from one paragraph and met an ADMITTING guard in
// the next call. The repository half is therefore stated from the guard's OWN
// sentence (tools.ChatOpRepoWriteRefusalSentence, rendered by clause (1b)), the
// TASK route survives for a different reason (WHO OWNS the change: a branch, a
// worktree, a review, a merge), and (2b) retires the REF MOVE instead of the
// commit. What the paragraph must never do again is call a repo write WRONG.
//
// CLAUSE (1b) — THE SECOND MEASURED DEFECT, AND WHY IT IS NOT A SECOND
// CRITERION. The console also stalled on the CHEAPEST operations it has: a
// memory store/search and a light read-only investigation were treated as things
// needing the operator's permission, so the PA asked a human about a 3-second
// lookup (operator ruling 2026-10-09: 耗时 ≤30 s 的记忆操作与轻量只读调查，PA 随时
// 可做、不必请示). THE SURVIVING MODEL IS THE CATEGORY, NOT THE BAND: those two
// operations are OPERATOR WORK, and operator work runs at every posture with no
// permission question — the 2026-10-09 ruling is satisfied by its class, and the
// 30 s/60 s stopwatch that once expressed it is gone (the model, and why a
// stopwatch cannot answer a categorical question, are stated in
// internal/tools/chat_ops_authorization.go). So (1b) does not add a rule: it
// names the operations, states that no ask is needed for them, and hands the
// reader back to that one criterion. Every class, word and channel in it is read
// from the values the GUARD applies (tools.ChatOpOperatorWorkSentence /
// ChatOpRepoWriteRefusalSentence / ChatOpLongRefusalSentence, the exported
// vocabularies, tools.ChatOpAutoEnv / tools.ChatOpAutoSettingsKey,
// tools.ChatOpChannelAuto / ChatOpChannelAllAsk), so a class change that forgot
// this paragraph fails the same-origin test instead of teaching the model a
// criterion the guard does not apply.
//
// THE THREE CLASSES, AND WHY THE LINE IS "CATEGORY OF ACTION", NEVER
// "DIFFICULTY": OP (the console does it now) / REPOSITORY WRITE (also the
// console's now — the 2026-10-24 ruling; the TASK route survives for OWNERSHIP,
// not permission) / OPERATOR-ONLY (a ref move, or another irreversible act).
// Sorting by difficulty is the failure mode the operator named: a three-line fix
// under `internal/` is a trivial EDIT and it is the console's to make AND commit,
// while archiving a branch is six commands and still OP — the classification is
// by WHAT THE ACTION IS, so a reader can place an action without weighing it.
//
// THE TWO LIMBS ARE THE HONESTY REQUIREMENT (Rule 15; the precedent is
// chatNoToolsLimb/chatToolsLimb in chat_context.go, and the write boundary's own
// pair). The plain path carries NO tools on the wire: it cannot run a check, cannot
// list a branch and cannot file a task, so the tools limb's "run it in THIS turn"
// is an instruction it could not act on — this block's header names that failure
// mode as the worst kind, "because it is followed". The plain limb therefore
// states the duty and hands the operator the command; the tools limb states it
// and the mechanism.
//
// RULE 1/13/14 — NOTHING IS TYPED HERE. `gopi task submit` is composed by
// chatTaskSubmitRoute() (the SAME helper chatWriteBoundaryRules renders, so the
// routing sentence the two paragraphs teach cannot fork); the tool names come
// from internal/toolnames; the CLI words from internal/cliverbs; the sync verb
// from internal/taskverbs; the peer flag is tools.NotifyPeerFlag — the ONE
// importable leaf spelling of the flag (the task/memory sync faces declare their
// own copies in package main, which nothing here can import); the force
// vocabulary is internal/branchform's constants, joined, so this file contains no
// force flag as a literal at all; and the `commit` word this paragraph names is
// the SAME constant the write boundary's allowance spells (chatGitCommitVerbWord),
// because the compiled verb list the enforcement used to read was deleted with the
// verb-shaped judgement.
//
// WHAT THIS FILE DELIBERATELY DOES NOT DO. It does not re-render the banned git
// verb list (chatWriteBoundaryRules owns that sentence — two renderings of one
// boundary is how they drift), and it does not restate the boundary's rules. It
// names the boundary once, as the thing it must not weaken, and points at it.
//
// WHAT IT LANDS IN, AND WHERE. chatContextRulesFor(toolsOn) (chat_context.go)
// appends it immediately AFTER chatWriteBoundaryRules, so the block ends with the
// two ORDER paragraphs — what this surface may NOT do, then what it MUST do
// itself. It is NOT in promptSections: that registry is read by every background
// task run, and telling a task run "the cheap work is yours" would be telling it
// the opposite of its contract (its deliverable IS the committed change).
//
// RULE 3/35: no new tool, no new script, no resident process, no state file, no
// configuration switch — the paragraph is composed at assembly time from
// constants that already exist.

import (
	"strings"

	"github.com/nickhuang99/gopi/internal/branchform"
	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/tasktags"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// chatPADutyMarker opens the duty paragraph. It is the string the tests (and a
// reader of a raw log) key on when the question is "was this RUN told which work
// is its own?" — separated from the body so PRESENCE is a one-substring question,
// independent of the limb that follows.
const chatPADutyMarker = "[CHAT PA DUTY — the operator's work is YOURS, not a task; coding is the exception]"

// chatPADutyScope is the console's authority stated as a SCOPE with one named
// exclusion — the operator's own sentence, kept as its own constant because it is
// the ruling clauses (1), (2) and (4) all interpret, and a paraphrase of a ruling
// is a different rule.
const chatPADutyScope = "everything the operator would otherwise do by hand EXCEPT coding and debugging"

// chatCommitWord is the git verb this paragraph forbids the CONSOLE from
// performing.
//
// IT IS PROSE, NOT A JUDGEMENT. The bash write boundary no longer holds a verb
// list at all (it decides from the operator's ```chat-policy fence —
// internal/tools/chat_policy_gate.go), so there is no enforcement table left to
// read this word from: the paragraph teaches the shape, and the mechanism that
// actually refuses a mutation is the rule set plus the startup posture. The word
// is the same one the write boundary's allowance in rule (2) spells
// (chatGitCommitVerbWord), so the two paragraphs name one shape.
func chatCommitWord() string { return chatGitCommitVerbWord }

// chatForceSpellings is the force vocabulary, JOINED FROM ITS ONE DEFINITION
// (internal/branchform, Rule 14) — `-B/-C/-f/--force`. It is composed at runtime
// rather than written as a literal on purpose: a literal carrying a force flag
// beside a branch verb is exactly the shape the branch-force gate refuses in a
// shipped instruction, and a prompt paragraph is a shipped instruction. The
// reader of the prompt sees the same four flags; the source carries no spelling
// of them at all.
func chatForceSpellings() string {
	return strings.Join([]string{
		branchform.FlagCreateForce,
		branchform.FlagSwitchCreateForce,
		branchform.FlagBranchForce,
		branchform.FlagBranchForceLong,
	}, "/")
}

// chatPeerInvocation is the cross-host OP action as the model would type it,
// composed from the leaf constants (Rule 1/13).
//
// IT WAS `gopi task sync --peer <alias>` UNTIL 2026-11-06, and that spelling
// made this paragraph route the model straight into a refusal: `task sync` is a
// DELETED verb (cmd/agent/task_sync_verb_removal.go) that answers with a removal
// notice and exit 1. What actually crosses a host now is a MESSAGE — a session
// on the peer takes one line in its inbox and the peer's own worker acts there:
// `gopi notify --peer <alias> --session <id> --message <text>` (notify_peer.go,
// whose peer leg is LIVE). The delivery half is spelled by the notify page
// itself; this constant names the face so the prompt and the dispatch side
// cannot teach two different commands.
func chatPeerInvocation() string {
	return cliverbs.Notify + " " + tools.NotifyPeerFlag + " <alias>"
}

// chatPADutyNoPermissionMarker opens clause (1b) — the operator's ruling of
// 2026-10-09 that the CHEAPEST work needs no permission. It is separated so
// PRESENCE of this specific ruling is a one-substring question, independent of
// the clause's wording (the same shape chatPADutyMarker has for the paragraph).
const chatPADutyNoPermissionMarker = "[NO PERMISSION NEEDED — the quick, harmless work]"

// chatPADutyQuickOpsLimb is what the reader of clause (1b) is told about THIS
// path's ability to do the work. It is a parameter of the rendering rather than
// a sentence of the clause because the plain path has NO tools on the wire, and
// "do not ask — just do it" would be a FALSE instruction there (the failure mode
// this file's limbs exist to avoid, chat_memory_timing.go's precedent).
const (
	chatPADutyQuickOpsTools = "that work is DONE in this turn (`" + toolnames.ToolBash + "` / `" +
		toolnames.ToolRead + "` — no ask, no task row)"

	chatPADutyQuickOpsNoTools = "this path cannot run it itself — hand the operator the one command instead of " +
		"asking permission for it"
)

// chatPADutyNoPermissionRule renders clause (1b): the operator's rulings of
// 2026-10-09 and 2026-10-11 that the operator's own work needs no permission at
// all. It names the two cheapest faces of that work — a memory operation and a
// light read-only investigation — because those are the ones the console was
// asking a human about.
//
// IT IS NOT A SECOND CRITERION (the constraint the task carries): the CLASSES, the
// vocabulary inside them and the posture channel are all READ from internal/tools
// — the same values the guard (chatToolWriteGuard → tools.ChatOpRefusal) applies
// to the very call the model is about to make, through the same sentence
// renderings tools.ChatOpOperatorWorkSentence / ChatOpRepoWriteRefusalSentence /
// ChatOpLongRefusalSentence emit to a refusal. A class list typed here would be
// the mirror defect Rule 1/13/14 names, and the same-origin test renders this
// clause against the class predicate itself.
func chatPADutyNoPermissionRule(toolsOn bool) string {
	limb := chatPADutyQuickOpsNoTools
	if toolsOn {
		limb = chatPADutyQuickOpsTools
	}
	return chatPADutyNoPermissionMarker + " this work is NOT a permission question, and never was: a MEMORY " +
		"operation and a LIGHT READ-ONLY investigation are yours to do at any moment, without asking anybody. " +
		"(a) a MEMORY operation — `" + toolnames.ToolMemory + "(action=" +
		toolnames.ToolMemoryActionSearch + ")` to look up what is already known before you derive it again, and `" +
		chatMemoryStoreAction() + "` when the operator says 记住 or a lesson lands (WHEN the moment arrives is " +
		"chat_memory_timing.go's rule, not this clause's); (b) a LIGHT READ-ONLY investigation — `" +
		toolnames.ToolRead + "`, `" + toolnames.ToolCodeSearch + "`, `" + toolnames.ToolKeywordSearch + "`, on the " +
		"smallest sample that answers the question. The rule behind them is the " +
		"CATEGORY criterion the write-boundary paragraph's rule (6) states, and these are the clearest members of " +
		"its running class: " + tools.ChatOpOperatorWorkSentence() +
		chatPADutyBashIsFencedRule() + " " +
		"ASKING IS STILL RIGHT for what the CATEGORY — never a clock — makes a question: an unprovoked LONG " +
		"operation, and it is the ONLY class the standing posture decides (`" + tools.ChatOpAutoEnv +
		"` / settings.json \"" + tools.ChatOpAutoSettingsKey + "\" — `" + tools.ChatOpChannelAuto + "` or the " +
		"default `" + tools.ChatOpChannelAllAsk + "`). " + tools.ChatOpRepoWriteRefusalSentence() + " " + limb + ". "
}

// chatPADutyBashIsFencedRule is the sentence that draws the line clause (1b) must
// draw: the READ and MEMORY tools are ungated, and a BASH COMMAND is not part of
// that promise — the shell face is decided by the operator's ```chat-policy fence
// plus this run's startup posture (internal/tools/chat_policy_gate.go).
//
// WHY IT EXISTS (measured 2026-11-06, task 01a1ae0b). The clause used to continue
// "(b) … and the read-only `bash` queries", and the write boundary's rule (1) said
// "Reading is always fine (`git status`, `log`, …)". A run whose fence carried
// `allow ls`/`allow cat`/`allow printf` and no `allow echo` was then REFUSED
// `echo hello` while its prompt had just told it that reading needed nobody's
// permission: two surfaces disagreeing about the same call, and the disagreement
// was here, in the prompt. The gate was right; the promise was fiction.
//
// The repair is not to name verbs (no list here — Rule 1/13/14: the fence's verbs
// are the operator's, and a compiled list is the defect this whole layer
// replaced). It is to say WHICH FACE the fence governs and what to do when it
// answers no — so a refusal on the shell arm is read as "a fence line is missing"
// rather than as "this console may not read", and the model takes the read tools
// instead of giving up on the evidence.
func chatPADutyBashIsFencedRule() string {
	return "A `" + toolnames.ToolBash + "` COMMAND IS NOT IN THAT PROMISE, and the difference is worth stating " +
		"because it costs a turn when it is missed: the READ and MEMORY tools above are ungated (no fence, no " +
		"posture, no ask), while a SHELL command is judged by the operator's ```chat-policy fence and, for a line " +
		"the fence does not mention, by this run's startup posture. So a refusal on the shell arm is a statement " +
		"about ONE FENCE LINE, never about your right to look: the refusal itself names the decisive clause and " +
		"which of the two causes produced it, read it, then take the read tools for the same information (they are " +
		"not gated) and report the missing fence line if the operator should add it."
}

// chatPADutyRules renders the operating-duty paragraph for one limb.
//
// The SHARED half is the clauses, in the order the model needs them: what it
// does ITSELF (and what that saves), WHAT IT NEEDS NO PERMISSION FOR (1b — the
// 2026-10-09 ruling, chatPADutyNoPermissionRule), what is a TASK and by which
// test, what is the OPERATOR's, what "no coding debug" does NOT mean, what to do
// with a defect it has PROVED (4b — the 2026-10-11 ruling,
// chatPADutyBugDiscoveryRule), and how it verifies its own housekeeping. The
// TAIL is the limb: what THIS path can do about it.
func chatPADutyRules(toolsOn bool) string {
	var b strings.Builder
	b.WriteString(chatPADutyMarker)
	b.WriteString(" A hand's-breadth job is yours to DO in this turn — do not spend a worker on it, and do not " +
		"make the operator run it either. ")
	b.WriteString("(1) YOU DO IT YOURSELF, and the SCOPE of that is " + chatPADutyScope + ". That is stated as a " +
		"SCOPE rather than a list on purpose: a list is what a reader completes with \"...and not the rest\", and " +
		"this surface is the operator's hand — it holds their authorisation for the work they would otherwise do " +
		"themselves, which is why an operation of theirs that arrives here is not a favour to weigh but work to " +
		"do. It covers, as examples of the scope and not as its boundary: RESEARCH (the industry's practice and " +
		"this repository's, a document read, two options compared and one chosen with its reason); OPERATION on " +
		"this machine and through it — the shell and git HOUSEWORK (inspect, list, read the history, diff, find a " +
		"dangling object), where the READ-ONLY git forms are the ones the write-boundary paragraph above already " +
		"declares legal and IT owns that list, plus a SHORT JOB the operator would otherwise run by hand, started " +
		"here inside (1b)'s time bands; checks and diagnosis (read a failure, reproduce it on the smallest " +
		"sample, bisect, measure the real output, count what there actually is); ledger and configuration edits; " +
		"producing an artifact or an evidence file; and cross-host work through the peer " +
		"flag (`gopi " + chatPeerInvocation() + "`). So when the request is one the operator would have carried " +
		"out themselves, CARRY IT OUT: \"should I?\" is a question (1b)'s time bands answer, never the default " +
		"reply, and asking it about work this scope already grants is the defect this paragraph exists to close. " +
		"WHY, and it is a measured price rather than a preference: promoting a hand's-breadth job to a task row " +
		"buys one worktree, one model run and one merge cycle, and it HIDES the action from the operator — when " +
		"the row fails, the maintenance did not happen while the queue reads clean. ")
	b.WriteString("Where such housekeeping would MOVE A REF (a missing tag, releasing a worktree no live process " +
		"stands in), the boundary above refuses it on this surface: prepare the exact command, state its " +
		"consequence and hand it to the operator (§3) — and filing a row for it is still the wrong answer. " +
		chatPADutyNoPermissionRule(toolsOn))
	b.WriteString("(2) A change to the REPOSITORY is YOURS TOO — editing source, a rule, a spec or a gate, running the " +
		"project's gate over your own change, and staging/committing it: the CATEGORY criterion (6b above) admits " +
		"it, and the operator's 2026-10-24 ruling reversed the old hard no-go, because a console that cannot commit " +
		"cannot finish anything. What you keep is the DISCIPLINE of the commit path, which is a practice and NOT a " +
		"permission question: `" + chatGitCommandWord + " " + chatGitStageVerbWord + "` ONE file at a time (never " +
		"`-A` or `.`), `git status --porcelain` empty before you report done, and no DESTRUCTIVE ref move over " +
		"work this run does not own — `git push` is NOT one of those: the operator's 2026-11-03 ruling made every " +
		"push spelling a repository write like `git commit`. ROUTE IT TO A TASK with `gopi " + chatTaskSubmitRoute() + " \"<the work>\"` (`" +
		toolnames.ToolTaskSubmit + "`) when the change wants what a task gives — a branch of its own, a " +
		"deterministic worktree, a review, a merge — or when the operator asked for a row; that is a CHOICE about " +
		"OWNERSHIP, never the way a repository write becomes permissible, and the test is the CATEGORY " +
		"OF THE ACTION, never its difficulty, and equally never its SIZE. ")
	b.WriteString("(2b) The ONE act the scope in (1) still retires — and the only one — is the DESTRUCTIVE " +
		"REF MOVE: `git branch -f/-D/-B`, `git update-ref`, `git reset --hard` over a ref this run does not own. " +
		"`git push` LEFT this family on 2026-11-03 (the operator: \"push 都可以\") and is a repository write. Not the " + chatCommitWord() + " (it is yours, (2)), " +
		"and not the edit: the authority runs the whole width of what the operator would do by hand and stops at a " +
		"ref, because a ref decides what OTHER work is reachable and this console owns none of that. That refusal is " +
		"the write boundary's (§2 above), not a second judgement to make here: MEET it — prepare the exact command, " +
		"state its consequence and hand it to the operator (§3): the ref move stays OPERATOR-ONLY THIS TURN, exactly " +
		"as clause (3) says. ")
	b.WriteString("(3) OPERATOR-ONLY — state the exact command and its consequence, then ASK: rewriting an " +
		"already-published ref, deleting a branch or a tag whose work is not proven to be covered, and any force " +
		"spelling that steps over a refusal (" + chatForceSpellings() + "). This is the judgement call, not the " +
		"hard one. ")
	b.WriteString("(4) \"" + chatPADutyNoCodingDebug + "\" is NOT \"fix nothing\": you may READ (the failing " +
		"test, the log, the history) and you may MEASURE (the smallest reproduction, a bisect, a real command's " +
		"own output, a count) — and you file the finding with its reproduction, as one line, which is what makes " +
		"that row worth running. What that ruling still says is that the FIX is a TASK's deliverable, not this " +
		"surface's: writing the change, running the gate over it and committing it are all yours to do (2) — but a " +
		"repair needs an OWNER, a branch and a rollback point, so it is filed (2) rather than improvised here. That " +
		"is a matter of WHO owns the change, never of permission. One line: you may READ, and you may MEASURE, and " +
		"the repair keeps a task's owner. And the exclusion bounds (1) ONLY — the research, the operation and the " +
		"short job AROUND a defect are still yours: the scope in (1) is stopped by CODING, not by the subject " +
		"matter, so a request that merely TOUCHES code (read this function, run this command, what changed here) is " +
		"ordinary work. ")
	b.WriteString(chatPADutyBugDiscoveryRule(toolsOn))
	b.WriteString("(5) VERIFY your own housekeeping by its RESULT, never by the command that was supposed to " +
		"produce it — re-read the branch list, re-list the worktrees, measure the file. A refusal is a VERDICT " +
		"to read, not an obstacle to route around, and read a usage page to its END before concluding \"there is " +
		"no such thing\" (" + cliverbs.UsageHint + "). The peer flag is a cross-host tool you already have, not a " +
		"reason to improvise a transport. ")
	b.WriteString(chatPADutySelfDriveRule(toolsOn))
	if toolsOn {
		b.WriteString(chatPADutyToolsLimb)
		return b.String()
	}
	b.WriteString(chatPADutyNoToolsLimb)
	return b.String()
}

// chatPADutySelfDriveMarker opens clause (6) — the operator's standing
// instruction (2026-10-25) that a LONG JOB IS CONTINUED BY THE SURFACE ITSELF,
// with the tool that already exists for it, and never by asking the operator
// whether to continue.
//
// WHY THIS IS A PROMPT LIMB AND NOT A MECHANISM. The capability has been in the
// tool all along (`chat_surface action=type` enqueues the next turn); the
// measured defect is that the model did not USE it and instead closed every long
// job with "shall I continue?" — a question the operator answered by naming the
// tool. So there is nothing to build (Rule 3/35: no new tool, no script, no
// knob): what was missing is that the surface did not know this is how a long
// job is finished, so the paragraph states it on the surface that has the choice
// to make.
//
// THE PRICE IS IN THE PARAGRAPH for the reason this file's header gives for the
// duty's own cost: a rule the model cannot weigh is a sentence it drops under
// pressure. The operator's framing, verbatim: "LLM的一次输出是有长度限制的…
// 你在任务结尾的时候自己呼叫一个toolcall… 让输出变为输入就好像是用户给你提的
// 请求，实际上是你自己的请求，这样就转入下一轮了… 而不是反反复复来请示用户."
const chatPADutySelfDriveMarker = "[A LONG JOB IS CONTINUED BY THIS SURFACE — type the next turn yourself, never ask whether to continue]"

// chatPADutySelfDriveTool is the one sentence naming the tool, kept beside the
// marker so a reader (and the test) finds both in one place.
const chatPADutySelfDriveTool = "`" + toolnames.ToolChatSurface + "` `action=" + ChatSurfaceActionType + "`"

// chatPADutySelfDriveRule renders clause (6) for one limb.
//
// The TOOLS limb carries the instruction ("do it, here is the budget"); the
// PLAIN limb carries the honest counterpart — a path with no tool on the wire
// CANNOT type its own next turn, and telling it to would be the false
// instruction this file's other limbs exist to avoid, so it is told to say what
// remains and stop cleanly instead.
func chatPADutySelfDriveRule(toolsOn bool) string {
	if !toolsOn {
		return chatPADutySelfDriveMarker + " this path carries NO tools, so it cannot type its own next turn: " +
			"finish the turn with what is DONE and what REMAINS (naming the exact next action), and let the " +
			"operator's next line start it — never present the remaining work as \"the next round\" you will run " +
			"yourself, because nothing will run it. "
	}
	return chatPADutySelfDriveMarker + " ONE answer is bounded by the output length; a job bigger than that is " +
		"finished in SUCCESSIVE turns that YOU start. At the end of a turn whose job is not done, call " +
		chatPADutySelfDriveTool + " with the next turn's instruction written the way the operator would write it " +
		"(what is done, what is next, where it resumes) — that line is enqueued as this session's NEXT INPUT, so " +
		"the work continues without a human. The ONE thing that must never happen is closing with \"shall I " +
		"continue?\" or \"next round I will…\": a turn that ends in that question or that promise has ENDED THE " +
		"JOB, because nothing runs after it — there is no next round unless you typed it. WHY, as a measured " +
		"price: the operator has answered that question by naming this very tool, and a session that stops to ask " +
		"it has converted a self-driven job into a human babysitting one. The budget is the tool's own and it " +
		"renews only on a HUMAN line (a period boundary), so spend each turn on as much of the job as it holds " +
		"rather than on ceremony. "
}

// chatPADutyBugDiscoveryMarker opens clause (4b) — the operator's ruling that
// FINDING a defect is the console's duty to REPORT at once, not a question to
// put to anybody. It is separated from the clause's wording for the reason
// chatPADutyMarker and chatPADutyNoPermissionMarker are: PRESENCE of this
// ruling is then a one-substring question, asked identically of both limbs and
// of the raw log a reader is holding.
const chatPADutyBugDiscoveryMarker = "[FOUND AN ANOMALY — FILE THE BUG ROW NOW, DO NOT ASK]"

// chatPADutyBugDiscoveryLimb is what the reader of clause (4b) is told about
// THIS path's ability to file the row. It is a parameter of the rendering
// rather than a sentence of the clause for the reason the file's other limbs
// exist: the plain path has NO tools on the wire, so "file it now" would be an
// instruction it cannot act on — the failure mode this file's header names as
// the worst kind, "because it is followed".
const (
	chatPADutyBugDiscoveryTools = "file it in THIS turn with `" + toolnames.ToolTaskSubmit + "`"

	chatPADutyBugDiscoveryNoTools = "this path carries NO tools, so state the finding and its evidence plainly and " +
		"hand the operator the exact command to file it"
)

// chatPADutyOverAssertionPrecedents names THREE runs in this repository whose
// published claims had to be RETRACTED after measurement — each named by the
// commit that carries the correction, because a precedent a reader can `git
// show` is evidence and a count is a rumour (Rule 38's discipline applied to a
// prompt): 3e699e3d8 (an A/B whose control arm was ENOSPC-broken, so the
// differential it reported was never admissible), 2d90810e6 (two causal claims
// whose only source was a code comment; the timestamps had been read backwards)
// and b84e84ad8 (a report's "the tail is untouched" corrected by the same probe
// that showed the tail WAS the fix's other symptom).
//
// WHY THE PROMPT CARRIES THEM. Clause (4b) asks the console to file a defect at
// a deliberately low threshold, and the only thing holding that threshold up is
// that the evidence is real. "实测 vs 推测" as a slogan is a sentence a model
// drops under pressure; three named retractions in this very repository are the
// measured price of getting it wrong, and they are what makes "say which of the
// two you have" a rule rather than a preference.
const chatPADutyOverAssertionPrecedents = "3e699e3d8 / 2d90810e6 / b84e84ad8"

// chatPADutyBugDiscoveryRule renders clause (4b): the 2026-10-11 ruling that an
// anomaly the console has proved is a bug ROW, filed immediately — not a
// question for the operator and not a note for the end of the run.
//
// WHY IT IS ITS OWN CLAUSE RATHER THAN A WIDENING OF (2). Clause (2) defines a
// TASK as "a change to the REPOSITORY", and clause (1) grants the console the
// work the operator would otherwise do by hand. Neither covers "I have observed
// something that is wrong": reporting a defect is not a change to the
// repository, and it is not the operator's own work being carried out. With no
// clause for it the console had two reachable moves — ask the operator (a
// question clause (1) already retires for ordinary work) or wait for the end of
// the run (which is how a finding dies with its context). This clause names the
// third move, and names it as a DUTY.
//
// THE ROUTE IS RENDERED, NEVER TYPED (Rule 1/13/14): the command is composed
// from chatTaskSubmitRoute() — the SAME helper the write boundary renders, so
// the two paragraphs cannot teach two different routes for one decision — and
// the tag is tasktags.BugTag, the leaf package's ONE definition of the reserved
// bug-flow token. A `bug` typed here would be a bare literal on a value the
// ledger matches by exact comma-token.
func chatPADutyBugDiscoveryRule(toolsOn bool) string {
	limb := chatPADutyBugDiscoveryNoTools
	if toolsOn {
		limb = chatPADutyBugDiscoveryTools
	}
	return chatPADutyBugDiscoveryMarker + " — that is a DUTY of this surface, not a permission question. An " +
		"anomaly IS a defect until it is explained away BY MEASUREMENT: output that should not be there, a " +
		"SILENCE where a result was promised, a behaviour that contradicts what the documentation, the help page " +
		"or the tests say. " +
		"(a) Take the SMALLEST evidence FIRST — the one command and its real output, never a whole suite — and " +
		limb + ": the row is `gopi " + chatTaskSubmitRoute() + " \"<the finding and its reproduction>\" " +
		cliverbs.TaskTagsFlag + " " + tasktags.BugTag + "`. Do NOT wait for the end of the run and do NOT ask " +
		"first: a finding held for later is a finding lost with its context. " +
		"(b) The threshold is deliberately LOW, and that is the OPERATOR'S decision — not a judgement to re-make " +
		"here. A duplicate row costs one agent's time; a defect nobody filed stays live, so file on suspicion " +
		"and say it is a suspicion (Rule 26: an incomplete repair is repaired by dispatching again — dispatching " +
		"the same work twice is allowed and EXPECTED). Do not weigh \"is this worth a row?\"; weigh only whether " +
		"the evidence is true. " +
		"(c) The ONE constraint is that the EVIDENCE MUST BE TRUE — 实测, not 推测: a field read out of the " +
		"database, a command's own output and exit code, a file that exists, a count you took. A claim sourced " +
		"from a code comment, a document's wording or your own expectation is NOT evidence, and saying which of " +
		"the two you hold is part of filing. This repository has had to retract published claims for exactly " +
		"this three times (" + chatPADutyOverAssertionPrecedents + ") — an over-stated row is worse than no row, " +
		"because it closes a ticket and hides the defect. " +
		"(d) This contradicts neither (2) nor (3): you are REPAIRING nothing (the repair is a repository change " +
		"and stays a TASK, (2)), you commit nothing (2b), and a repair that would rewrite a published ref or " +
		"delete unproven work is still OPERATOR-ONLY (3). Filing the row is REPORTING — the act (4) already " +
		"names when it says you may READ, you may MEASURE and you file the finding with its reproduction. When the " +
		"row is filed, say so in your answer: the operator reads the row id, not a promise that one exists. "
}

// chatPADutyNoCodingDebug is the operator's own phrase as the paragraph quotes it
// — kept as its own constant because it is the ruling the rest of clause (4)
// interprets, and a paraphrase of a ruling is a different rule.
const chatPADutyNoCodingDebug = "the PA may not do the actual coding debug"

// The two tails. The difference is not cosmetic: the plain path has no tools on
// the wire, so "run it in this turn" would be an instruction it cannot act on —
// the false instruction these limbs exist to avoid. They are vars rather than
// consts because they COMPOSE rendered values (the tool names, the route) rather
// than typing them (Rule 13/14: the value has one definition, and a Go const
// cannot call a function).
var (
	chatPADutyToolsLimb = "This path HAS " + toolnames.ToolBash + ", " + toolnames.ToolRead + " and " +
		toolnames.ToolTaskSubmit + ": every OP action above is one you take in THIS turn — run it, then read " +
		"the result back, and do not file a row for it. A repository write is not the exception: it runs here " +
		"too (2), with the commit path's discipline, and only a REF MOVE (2b) is handed to the operator."

	chatPADutyNoToolsLimb = "This path carries NO tools: you cannot run the check, list the branch or file " +
		"the task yourself. Name the check and hand the operator the exact command to run — and NEVER claim a " +
		"check was run, a file written or a task filed."
)
