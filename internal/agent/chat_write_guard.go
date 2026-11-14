package agent

// chat_write_guard.go — the CHAT WRITE BOUNDARY's MECHANISM half, agent-side:
// the four-line hook that turns the tools package's predicate (internal/tools/
// chat_write_guard.go) into a refusal at the moment a chat turn's tool call would
// mutate git state.
//
// WHY A MECHANISM AT ALL (the prompt is not enough). The boundary is written into
// the chat system prompt (chat_write_boundary.go) and that is the primary
// constraint — but a prompt is followed PROBABILISTICALLY, and the failure it
// guards against is the one with no recovery: a `git commit` / `checkout` /
// `stash` from a chat rewrites the tree the operator is standing in, leaves no
// branch and no rollback point, and — the stash shape — can take ANOTHER live
// run's uncommitted work with it. The precedent is this repo's own shared-stash
// guard (internal/tools/shared_stash_guard.go), which refuses one verb family for
// the same reason; the boundary widens that idea to the chat surface.
//
// WHERE THE HOOK RUNS — THE TOOL DISPATCH, NOT THE SHELL. It is called from
// Agent.executeToolDefault, the ONE place a chat's `bash` / `write` call turns
// into an action, and it fires only while a.ChatMode is set — which Agent.Chat
// and Agent.ChatOps set for the duration of a chat turn and NO task run ever
// sets. Three consequences, each the reason the other door was rejected:
//
//   - The worker path is untouched BY CONSTRUCTION: `git add`/`commit`/`push` in
//     the row's own worktree is its deliverable, and it never enters this
//     function. The tests assert both sides.
//   - No new tool, no new script, no new resident process (Rule 3/35): the
//     refusal is RETURNED IN PLACE of the call's result, in the same
//     error-shaped prose the dispatch's other refusals already produce, so the
//     model reads why and self-corrects. It removes no tool and asks the operator
//     nothing.
//   - It is not a shell filter: the command is judged by subcommand word through
//     the tools package's clause reader (which reuses the shared-stash guard's
//     preamble/option tables), so `git log --grep=commit` reads as a log, and a
//     quoted argument naming `git add` reads as prose.

import (
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// chatToolWriteGuard returns the refusal for a chat turn's tool call, or "" when
// the call is allowed. operatorTurn is THIS turn's own user message, passed down
// rather than read from the guard, so the cleanup faces' destructive half can be
// opened by the operator's words and by nothing else (it is a parameter, not
// state: a caller that has no turn to offer passes "", which holds the half
// closed). The two arms are the two ways a chat can touch git state
// with the tools it has: a bash command that runs a banned git subcommand, and a
// write/edit whose target is a git-tracked tree.
//
// The repo root is resolved through chatRepoRoot — the SAME resolver the chat
// block renders into the prompt — so the guard and the sentence the model reads
// cannot disagree about which checkout this run is standing in (one answer, one
// place). It is read only for the write/edit arms: a bash command carries its own
// repository, and probing git for a call that is not a write would be a process
// spawned per tool call.
func chatToolWriteGuard(toolName string, args map[string]interface{}, operatorTurn, carrier string) string {
	switch toolName {
	case toolnames.ToolBash:
		command, _ := args["command"].(string)
		workdir, _ := args[chatBashWorkdirArg].(string)
		if workdir == "" {
			workdir = chatRepoRoot()
		}
		// The operator's own turn message is what may open the cleanup faces'
		// destructive half; the guard holds it CLOSED for every other caller
		// (chat_cleanup_authorization.go states why it cannot become a switch).
		if refusal := tools.ChatBashWriteRefusalForCarrier(carrier, command, workdir, operatorTurn); refusal != "" {
			return refusal
		}
		// THE CATEGORY CRITERION, which is the half that replaced the verb list:
		// an operation is judged by WHAT IT IS, never by a stopwatch. OPERATOR WORK
		// (ops, DevOps, the network, a script, a document, a query, a diagnosis, a
		// bug row, housekeeping) runs with no permission question; a REPOSITORY
		// WRITE is refused with the task route; only a LONG operation (whole-tree
		// suite, full build, package install, merge) is a question for the operator.
		// It is asked LAST, after the write boundary answered "does this MUTATE?" —
		// a refused mutation needs no category question, and the mutation refusal is
		// the sharper one. The call's own `timeout_sec` is NOT read here any more:
		// it is an execution cap the bash tool enforces, never a declaration of what
		// the action IS (chat_ops_authorization.go states the defect that made it the
		// criterion, and why a cap cannot answer a categorical question).
		// The WORKDIR is passed down (ChatOpRefusalIn) because the criterion it
		// asks carries the invocation's directory: a `git push` is a repository
		// write since the operator's 2026-11-03 ruling (row 01a1a786), and every
		// other reading of the line is the same in both spellings. Without the
		// directory the guard would answer for the tree the process happens to
		// stand in whenever the tool call names another one.
		return tools.ChatOpRefusalIn(command, workdir, operatorTurn)
	case toolnames.ToolWrite, toolnames.ToolEdit:
		path, _ := args["path"].(string)
		// BOTH openings, because they answer DIFFERENT questions and each one
		// alone leaves a face stuck. The CARRIER arm answers "this face has no
		// approval channel, so its `prompt` is a question with no reader"
		// (internal/tools/chat_policy_carrier.go). The TURN arm answers "a `prompt`
		// decision is the operator asking to be asked, and this turn is their
		// answer" (chat_approval_authorization.go). Measured: with only the carrier
		// arm the console's own approval deadlocks; with only the turn arm
		// `gopi web --tools` asks a question nobody can answer.
		//
		// An EMPTY carrier is the console's own behaviour (publishChatCarrier's
		// restore value), so it takes the turn arm.
		if carrier != "" {
			return tools.ChatWritePathRefusalForCarrier(carrier, path, chatRepoRoot(), operatorTurn)
		}
		return tools.ChatWritePathRefusalAuthorized(path, chatRepoRoot(), operatorTurn)
	default:
		return ""
	}
}

// chatBashWorkdirArg is the bash tool's optional working-directory argument, named
// here because this hook reads it (Rule 1: an argument key is a value, and the
// dispatch below reads the same spelling). It is what makes the pathspec judgement
// resolve against the repository the command will actually run in.
const chatBashWorkdirArg = "workdir"

// publishChatTurn publishes THIS chat turn's own user message into the write
// boundary's authorization channel and returns the restore for it — the one
// spelling both chat entry points (Agent.Chat and Agent.ChatOps) use, so the
// channel cannot be left open by one of them forgetting to clear it. The restore
// puts back whatever was there before (nothing, in the console path), so a nested
// or reused agent does not end up holding a turn that has ended.
//
// It is a PAIR on purpose: `defer a.publishChatTurn(msg)()` is the only shape, and
// the property the tests pin — an authorizing turn followed by a silent one is
// REFUSED — is a property of this pair (chat_cleanup_authorization.go: the gate
// reads a function of one turn's text and holds no state a later turn inherits).
// publishChatCarrier publishes the FACE this turn is answered on and returns its
// restore — the same pair shape publishChatTurn uses, so a carrier can never be
// left set for a later turn (an empty restore value is the console's own
// behaviour, which is the safe direction for a turn nobody declared).
func (a *Agent) publishChatCarrier(carrier string) func() {
	previous := a.ChatCarrier
	a.ChatCarrier = carrier
	return func() { a.ChatCarrier = previous }
}

func (a *Agent) publishChatTurn(userInput string) func() {
	previous := a.chatTurnInput
	a.chatTurnInput = userInput
	return func() { a.chatTurnInput = previous }
}

// chatModeWriteRefusal is the dispatch's ONE entry point: it applies the guard
// only for a chat turn, so the task path (whose deliverable is exactly the git
// mutations this refuses) is unaffected by construction rather than by a second
// predicate.
func (a *Agent) chatModeWriteRefusal(toolName string, args map[string]interface{}) string {
	if a == nil || !a.ChatMode {
		return ""
	}
	return chatToolWriteGuard(toolName, args, a.chatTurnInput, a.ChatCarrier)
}
